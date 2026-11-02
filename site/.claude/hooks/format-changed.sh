#!/usr/bin/env bash
# Stop: format the files this session wrote (Edit/Write) at the end of each turn,
# so what the agent leaves behind is formatted even if nobody runs the formatter
# by hand. lint-on-write.sh records each written path in a per-session list
# outside the repo (see session_file_list in _lib.sh); this hook formats the ones
# that still exist inside the project, then clears the list. Files other agents
# or you are editing in the same checkout are left alone. No session_id or no
# list: nothing is formatted.
#
# Only formatters the project has opted into run, and only on the files they own:
#
#   *.py              ruff format + safe ruff fixes (unused imports kept), when ruff is configured or
#                     installed in the project's .venv
#   JS/TS/JSON/CSS    biome when a biome config exists, else prettier when a
#                     prettier config exists (project node_modules binaries)
#   *.go              gofmt
#   *.rs              rustfmt with the edition `cargo fmt` would pass, read from
#                     the nearest Cargo.toml; files with no Cargo.toml above them
#                     are left alone
#   *.java            Spotless through the project's Maven wrapper, once for all
#                     of the session's files under that wrapper; files with no
#                     executable mvnw and pom.xml above them are left alone
#
# Silent by design: no output, always exit 0. A Stop hook that prints errors can
# trap the session in a feedback loop.
set -euo pipefail
# Any failure the code below doesn't handle still ends the hook with 0.
trap 'exit 0' ERR

hook_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=_lib.sh
source "$hook_dir/_lib.sh"

input=$(cat)
list=$(session_file_list "$input") || exit 0
[ -s "$list" ] || exit 0
cwd=$(hook_get "$input" cwd | head -n 1) || cwd=""
if [ -n "$cwd" ]; then cwd=$(cd "$cwd" 2>/dev/null && pwd -P) || cwd=""; fi

root=$(project_root)
root=$(cd "$root" 2>/dev/null && pwd -P) || exit 0
cd "$root" || exit 0

plan=$(mktemp "${TMPDIR:-/tmp}/claude-format.XXXXXX") || exit 0
# Claim the list before reading it (mv is atomic), so a path recorded while this
# hook runs lands in a fresh list for the next Stop instead of being dropped.
claimed="$plan.files"
mv "$list" "$claimed" 2>/dev/null || { rm -f "$plan"; exit 0; }
trap 'rm -f "$plan" "$claimed"' EXIT

# prettier_for <dir>: prettier, when the project configured it and installed it.
prettier_for() {
  local cfg
  cfg=$(find_up "$1" .prettierrc .prettierrc.json .prettierrc.yaml .prettierrc.yml .prettierrc.js \
    .prettierrc.cjs .prettierrc.mjs .prettierrc.toml prettier.config.js prettier.config.cjs \
    prettier.config.mjs prettier.config.ts) || return 1
  [ -x "$cfg/node_modules/.bin/prettier" ] || return 1
  printf 'prettier\t%s\n' "$cfg"
}

# manifest_edition <Cargo.toml>: print "<package> <workspace> <has-workspace>".
# <package> is the [package] edition, "inherit" for edition.workspace = true,
# "none" when [package] sets no edition and "-" with no [package]; <workspace>
# is the [workspace.package] edition or "-"; <has-workspace> is 1 when the file
# has a [workspace] table. Only plain year values count as an edition.
manifest_edition() {
  LC_ALL=C awk '
    function year(s) {
      if (match(s, /["\047][0-9]+["\047]/)) return substr(s, RSTART + 1, RLENGTH - 2)
      return ""
    }
    { line = $0; sub(/^[ \t]+/, "", line) }
    line ~ /^\[\[/ { sec = ""; next }
    line ~ /^\[/ {
      sec = line; sub(/^\[[ \t]*/, "", sec); sub(/[ \t]*\].*$/, "", sec)
      if (sec == "package") pkg_seen = 1
      if (sec == "workspace" || sec ~ /^workspace\./) has_ws = 1
      next
    }
    sec == "package" && line ~ /^edition[ \t]*\.[ \t]*workspace[ \t]*=[ \t]*true/ { pkg = "inherit"; next }
    sec == "package" && line ~ /^edition[ \t]*=[ \t]*\{.*workspace[ \t]*=[ \t]*true/ { pkg = "inherit"; next }
    sec == "package" && line ~ /^edition[ \t]*=/ { y = year(line); if (y != "") pkg = y; next }
    sec == "workspace.package" && line ~ /^edition[ \t]*=/ { y = year(line); if (y != "") ws = y; next }
    END {
      if (!pkg_seen) pkg = "-"; else if (pkg == "") pkg = "none"
      if (ws == "") ws = "-"
      printf "%s %s %d\n", pkg, ws, has_ws
    }' "$1" 2>/dev/null
}

# cargo_edition <dir>: the edition for a Rust file in <dir>. The nearest
# Cargo.toml's [package] edition; when that is inherited, or the manifest has
# no [package], the [workspace.package] edition of the nearest manifest with a
# [workspace] table (the same file or an ancestor). A package that sets no
# edition is 2015, Cargo's default. Returns 1 when no manifest decides it.
cargo_edition() {
  local dir pkg ws has_ws
  dir=$(find_up "$1" Cargo.toml) || return 1
  read -r pkg ws has_ws <<<"$(manifest_edition "$dir/Cargo.toml")" || return 1
  case "$pkg" in
  none) printf '2015\n'; return 0 ;;
  inherit | -) ;;
  *) printf '%s\n' "$pkg"; return 0 ;;
  esac
  while :; do
    if [ "$has_ws" = 1 ]; then
      [ "$ws" != - ] || return 1
      printf '%s\n' "$ws"
      return 0
    fi
    [ "$dir" != / ] || return 1
    dir=$(find_up "$(dirname "$dir")" Cargo.toml) || return 1
    read -r pkg ws has_ws <<<"$(manifest_edition "$dir/Cargo.toml")" || return 1
  done
}

# rustfmt_file <edition> <file>: format one file through stdin, because rustfmt
# given a path also rewrites the out-of-line modules that file declares, which
# may be someone else's. It runs from the file's directory so rustfmt.toml is
# still found, and the file is rewritten only when rustfmt succeeded and changed it.
rustfmt_file() {
  local out
  out=$(mktemp "${TMPDIR:-/tmp}/claude-rustfmt.XXXXXX") || return 0
  # shellcheck disable=SC2016 # the inner sh expands its own arguments
  if (cd "$(dirname "$2")" &&
    with_timeout 20 sh -c 'exec rustfmt --edition "$1" --emit stdout <"$2" >"$3" 2>/dev/null' \
      sh "$1" "$2" "$out") >/dev/null 2>&1 &&
    [ -s "$out" ] && ! cmp -s "$out" "$2"; then
    cat "$out" >"$2" || true
  fi
  rm -f "$out"
}

# java_home_for <dir>: the JDK the project's Makefile would pick when JAVA_HOME
# is unset: the major in <dir>/.java-version through /usr/libexec/java_home,
# then Homebrew's openjdk@<major>. Prints nothing when neither has it, and
# mvnw then falls back to the java on PATH.
java_home_for() {
  local major prefix
  [ -z "${JAVA_HOME:-}" ] || { printf '%s\n' "$JAVA_HOME"; return 0; }
  [ -f "$1/.java-version" ] || return 0
  major=$(tr -d '[:space:]' <"$1/.java-version" 2>/dev/null) || return 0
  [ -n "$major" ] || return 0
  if [ -x /usr/libexec/java_home ] && /usr/libexec/java_home -v "$major" 2>/dev/null; then return 0; fi
  prefix=$(brew --prefix "openjdk@$major" 2>/dev/null) || return 0
  [ -d "$prefix/libexec/openjdk.jdk/Contents/Home" ] && printf '%s\n' "$prefix/libexec/openjdk.jdk/Contents/Home"
  return 0
}

# spotless_files <dir> <file>...: one `spotless:apply` for these files. Each
# path becomes a literal \Q...\E pattern in -DspotlessFiles, which Spotless
# matches against the absolute path, so only the session's files are touched.
spotless_files() {
  local dir=$1 f patterns="" jdk
  shift
  for f in "$@"; do patterns="${patterns:+$patterns,}\\Q$f\\E"; done
  jdk=$(java_home_for "$dir")
  (cd "$dir" && JAVA_HOME="$jdk" with_timeout 40 ./mvnw -q -B spotless:apply "-DspotlessFiles=$patterns") >/dev/null 2>&1 || true
}

# formatter_for <abs-path>: print "<tool>\t<workdir>" or return 1.
formatter_for() {
  local f=$1 dir cfg edition
  dir=$(dirname "$f")
  case "$f" in
  */node_modules/* | */.venv/* | */venv/* | */vendor/* | */dist/* | */build/* | */target/*) return 1 ;;
  *.py | *.pyi)
    cfg=$(find_up "$dir" ruff.toml .ruff.toml pyproject.toml) || return 1
    if [ -x "$cfg/.venv/bin/ruff" ]; then
      printf 'ruff-venv\t%s\n' "$cfg"
    elif command -v ruff >/dev/null 2>&1 &&
      { [ -f "$cfg/ruff.toml" ] || [ -f "$cfg/.ruff.toml" ] || grep -q '^\[tool\.ruff' "$cfg/pyproject.toml" 2>/dev/null; }; then
      printf 'ruff\t%s\n' "$cfg"
    else
      return 1
    fi
    ;;
  *.ts | *.tsx | *.js | *.jsx | *.mjs | *.cjs | *.mts | *.cts | *.json | *.jsonc | *.css)
    # Biome first: when a project has both, biome is the one it chose last.
    if cfg=$(find_up "$dir" biome.json biome.jsonc) && [ -x "$cfg/node_modules/.bin/biome" ]; then
      printf 'biome\t%s\n' "$cfg"
    else
      prettier_for "$dir"
    fi
    ;;
  *.scss | *.html | *.vue | *.svelte | *.astro | *.md | *.mdx | *.yaml | *.yml)
    prettier_for "$dir"
    ;;
  *.go)
    command -v gofmt >/dev/null 2>&1 || return 1
    printf 'gofmt\t%s\n' "$root"
    ;;
  *.rs)
    command -v rustfmt >/dev/null 2>&1 || return 1
    edition=$(cargo_edition "$dir") || return 1
    printf 'rustfmt:%s\t%s\n' "$edition" "$dir"
    ;;
  *.java)
    # A comma would split the -DspotlessFiles list and \E would end the quote.
    case "$f" in *,* | *'\E'*) return 1 ;; esac
    cfg=$(find_up "$dir" mvnw) || return 1
    [ -x "$cfg/mvnw" ] && [ -f "$cfg/pom.xml" ] || return 1
    # Never run a wrapper from above the project (or the agent's worktree).
    case "$cfg/" in
    "$root"/*) ;;
    *) [ -n "$cwd" ] && case "$cfg/" in "$cwd"/*) true ;; *) false ;; esac || return 1 ;;
    esac
    printf 'spotless\t%s\n' "$cfg"
    ;;
  *) return 1 ;;
  esac
}

# lint-on-write.sh recorded physical paths; one per line, repeats possible.
while IFS= read -r f; do
  # The plan file is tab- and newline-separated.
  case "$f" in *$'\t'*) continue ;; esac
  [ -n "$f" ] && [ -f "$f" ] || continue
  case "$f" in
  "$root"/*) ;;
  *)
    # A worktree the agent works in, as lint-on-write.sh allows.
    [ -n "$cwd" ] || continue
    case "$f" in "$cwd"/*) ;; *) continue ;; esac
    ;;
  esac
  key=$(formatter_for "$f") || continue
  printf '%s\t%s\n' "$key" "$f" >>"$plan"
done < <(sort -u "$claimed")

[ -s "$plan" ] || exit 0

# One invocation per (tool, workdir), with all of that group's files.
cut -f1,2 "$plan" | sort -u | while IFS=$'\t' read -r tool dir; do
  set --
  while IFS= read -r f; do
    set -- "$@" "$f"
  done < <(awk -F'\t' -v t="$tool" -v d="$dir" '$1 == t && $2 == d { print $3 }' "$plan")
  [ "$#" -gt 0 ] || continue

  case "$tool" in
  # F401 stays unfixed: an import written this turn may be used next turn.
  ruff-venv)
    (cd "$dir" && with_timeout 20 .venv/bin/ruff format --quiet "$@" && with_timeout 20 .venv/bin/ruff check --fix --unfixable F401 --quiet "$@") >/dev/null 2>&1 || true
    ;;
  ruff)
    (cd "$dir" && with_timeout 20 ruff format --quiet "$@" && with_timeout 20 ruff check --fix --unfixable F401 --quiet "$@") >/dev/null 2>&1 || true
    ;;
  biome)
    (cd "$dir" && with_timeout 30 node_modules/.bin/biome check --write "$@") >/dev/null 2>&1 || true
    ;;
  prettier)
    (cd "$dir" && with_timeout 30 node_modules/.bin/prettier --write --ignore-unknown "$@") >/dev/null 2>&1 || true
    ;;
  gofmt)
    with_timeout 20 gofmt -w "$@" >/dev/null 2>&1 || true
    ;;
  rustfmt:*)
    for f in "$@"; do rustfmt_file "${tool#rustfmt:}" "$f"; done
    ;;
  spotless)
    spotless_files "$dir" "$@"
    ;;
  esac
done

exit 0
