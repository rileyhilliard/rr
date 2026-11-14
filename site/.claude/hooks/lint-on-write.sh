#!/usr/bin/env bash
# PostToolUse (Edit|Write): lint the file that was just written and hand any
# diagnostics back to the agent as additionalContext, so it fixes them now
# instead of finding out at `make lint` time or in CI.
#
# Advisory and read-only: never blocks, never rewrites the file. It also records
# the path in this session's list (see session_file_list in _lib.sh), which
# format-changed.sh formats at the end of the turn. The linter is picked by file extension and
# only runs when it is installed for this project:
#
#   *.py              ruff (project .venv first, then PATH)
#   *.ts *.tsx *.js…  biome if a biome config is present, else eslint if an
#                     eslint config is present (project node_modules first)
#   *.json *.css      biome, when configured
#   *.go              golangci-lint when configured, else gofmt syntax check
#   *.sh *.bash       shellcheck
#   *.rs              nothing: clippy checks a whole crate, too slow per write,
#                     so it stays in the project's lint target (rustfmt still
#                     runs at Stop)
#   *.java            nothing: compiling through Maven starts a JVM, seconds per
#                     write, so it stays in the project's compile and lint
#                     targets (Spotless still formats at Stop)
#
# Nothing installed, file outside the project, or linter crashed or timed out:
# exit 0 with no output. A broken linter must not turn into fake diagnostics.
set -euo pipefail

hook_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=_lib.sh
source "$hook_dir/_lib.sh"

input=$(cat)
file_path=$(hook_get "$input" tool_input.file_path | head -n 1) || exit 0
cwd=$(hook_get "$input" cwd | head -n 1) || cwd=""
[ -n "$file_path" ] && [ -f "$file_path" ] || exit 0

# Compare physical paths: the project dir, the cwd, and the file path can reach
# the same place through different symlinks (on macOS /tmp is /private/tmp).
file_dir=$(cd "$(dirname "$file_path")" && pwd -P) || exit 0
file_path="$file_dir/$(basename "$file_path")"
if [ -n "$cwd" ]; then cwd=$(cd "$cwd" 2>/dev/null && pwd -P) || cwd=""; fi

# Only files in this project (or the worktree the agent is working in).
root=$(project_root)
root=$(cd "$root" && pwd -P) || exit 0
case "$file_path" in
"$root"/*) ;;
*)
  if [ -z "$cwd" ]; then exit 0; fi
  case "$file_path" in "$cwd"/*) ;; *) exit 0 ;; esac
  ;;
esac

# Dependencies, build output, and VCS internals are not ours to lint.
case "$file_path" in
*/node_modules/* | */.venv/* | */venv/* | */vendor/* | */dist/* | */build/* | */.git/* | */target/*) exit 0 ;;
esac

# Record the path for format-changed.sh, which formats only what this session
# wrote so it never rewrites a file another agent in the same checkout is editing.
# Best effort: a failed write here only means the file isn't formatted at Stop.
case "$file_path" in
*$'\n'*) ;;
*)
  if list=$(session_file_list "$input"); then
    printf '%s\n' "$file_path" >>"$list" 2>/dev/null || true
  fi
  ;;
esac

file_dir=$(dirname "$file_path")
out=""
rc=0
tool=""

# node_bin <name>: a project-local node binary, nearest node_modules/.bin first.
node_bin() {
  local dir
  dir=$(find_up "$file_dir" "node_modules/.bin/$1") || return 1
  printf '%s\n' "$dir/node_modules/.bin/$1"
}

lint_python() {
  local dir ruff=""
  dir=$(find_up "$file_dir" pyproject.toml ruff.toml .ruff.toml) || dir=$file_dir
  if [ -x "$dir/.venv/bin/ruff" ]; then
    ruff="$dir/.venv/bin/ruff"
  else
    ruff=$(command -v ruff 2>/dev/null) || return 0
  fi
  tool="ruff check"
  # ruff exits 1 for violations, 2 for its own errors (bad config, crash).
  out=$(cd "$dir" && with_timeout 20 "$ruff" check --no-fix --quiet --output-format=concise "$file_path") || rc=$?
  [ "$rc" -eq 1 ] || out=""
}

lint_biome() {
  local cfg bin
  cfg=$(find_up "$file_dir" biome.json biome.jsonc) || return 1
  bin=$(node_bin biome) || bin=$(command -v biome 2>/dev/null) || return 0
  # lint, not check: formatting is fixed at Stop by format-changed.sh, so a
  # formatter diff here would only cost the agent a turn.
  tool="biome lint"
  out=$(cd "$cfg" && with_timeout 20 "$bin" lint --colors=off --reporter=concise "$file_path") || rc=$?
  # Clean, timed out, or the file is outside biome's configured includes.
  if [ "$rc" -eq 0 ] || [ "$rc" -ge 124 ] || grep -q "No files were processed" <<<"$out"; then
    out=""
  fi
  # Keep the one-line diagnostics (path:line:col: rule: message), not the summary.
  local lines
  lines=$(grep -E ':[0-9]+:[0-9]+: ' <<<"$out" || true)
  [ -z "$lines" ] || out=$lines
  return 0
}

lint_eslint() {
  local cfg bin
  cfg=$(find_up "$file_dir" eslint.config.js eslint.config.mjs eslint.config.cjs \
    eslint.config.ts eslint.config.mts eslint.config.cts \
    .eslintrc.js .eslintrc.cjs .eslintrc.json .eslintrc.yml .eslintrc.yaml .eslintrc) || return 0
  bin=$(node_bin eslint) || return 0
  tool="eslint"
  # eslint exits 1 for lint errors, 2 for config or crash.
  out=$(cd "$cfg" && with_timeout 30 "$bin" --no-color "$file_path") || rc=$?
  [ "$rc" -eq 1 ] || out=""
}

lint_go() {
  local mod rel
  mod=$(find_up "$file_dir" go.mod) || return 0
  if command -v golangci-lint >/dev/null 2>&1 &&
    find_up "$file_dir" .golangci.yml .golangci.yaml .golangci.toml .golangci.json >/dev/null; then
    tool="golangci-lint"
    rel=${file_dir#"$mod"}
    out=$(cd "$mod" && with_timeout 45 golangci-lint run "./${rel#/}") || rc=$?
    [ "$rc" -eq 1 ] || out=""
    return 0
  fi
  command -v gofmt >/dev/null 2>&1 || return 0
  # gofmt -e reports syntax errors; formatting itself is fixed on Stop.
  tool="gofmt (syntax)"
  out=$(with_timeout 10 gofmt -e -l "$file_path") || rc=$?
  [ "$rc" -ne 0 ] && [ "$rc" -lt 124 ] || out=""
}

lint_shell() {
  command -v shellcheck >/dev/null 2>&1 || return 0
  tool="shellcheck"
  out=$(with_timeout 15 shellcheck -x --source-path=SCRIPTDIR --color=never --format=gcc "$file_path") || rc=$?
  [ "$rc" -eq 1 ] || out=""
}

case "$file_path" in
*.py | *.pyi) lint_python ;;
*.ts | *.tsx | *.js | *.jsx | *.mjs | *.cjs | *.mts | *.cts) lint_biome || lint_eslint ;;
*.json | *.jsonc | *.css) lint_biome || true ;;
*.go) lint_go ;;
*.sh | *.bash) lint_shell ;;
*.rs | *.java) exit 0 ;;
*) exit 0 ;;
esac

out=$(printf '%s' "$out" | strip_control)
# Project-relative paths read better and cost fewer tokens.
# The prefix goes through a variable: bash 3.2 treats a "/" inside a quoted
# pattern as the ${var//pattern/replacement} delimiter.
prefix="$root/"
out=${out//"$prefix"/}
[ -z "${out//[[:space:]]/}" ] && exit 0

# Keep the payload small. Cut by line, not byte, so multi-byte characters survive.
max_lines=60
if [ "$(wc -l <<<"$out" | tr -d ' ')" -gt "$max_lines" ]; then
  out="$(head -n "$max_lines" <<<"$out")"$'\n'"... (truncated; run the linter on this file for the rest)"
fi

rel_path=${file_path#"$root"/}
hook_context PostToolUse "$tool found issues in $rel_path. Fix them before moving on:"$'\n'"$out"
exit 0
