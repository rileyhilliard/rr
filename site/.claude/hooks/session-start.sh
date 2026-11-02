#!/usr/bin/env bash
# SessionStart: a few lines of live state that AGENTS.md can't carry because it
# is static: the branch, how dirty the tree is, whether dependencies are
# installed, whether the services declared in the compose file are running, and
# whether a generated project still has its brief to follow.
#
# An example env file does not imply .env is required; the app may use defaults.
#
# Plain-text stdout from a SessionStart hook is added to the agent's context, so
# this hook needs no jq. Cheap: a few git calls and at most two `docker compose`
# calls, each behind a timeout so a stuck Docker daemon can't stall startup.
# Add project-specific checks at the end (a missing symlink, a required login).
set -euo pipefail

hook_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=_lib.sh
source "$hook_dir/_lib.sh"

cat >/dev/null 2>&1 || true # payload unused

root=$(project_root)
cd "$root" 2>/dev/null || exit 0

lines=()
say() { lines+=("$1"); }

if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  # `branch --show-current` rather than `rev-parse --abbrev-ref HEAD`, which
  # prints "HEAD" before the first commit.
  branch=$(git branch --show-current 2>/dev/null || true)
  [ -z "$branch" ] && branch="(detached HEAD)"
  dirty=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ') || dirty="?"
  say "Repo state: branch \`$branch\`, $dirty changed file(s)."
  # No nag before the first commit: a new project's initial commit goes on main. Repos that
  # work on main by design opt out with `git config stacks.allowMain true`.
  if git rev-parse -q --verify HEAD >/dev/null 2>&1 &&
    [ "$(git config --bool stacks.allowMain 2>/dev/null || true)" != "true" ]; then
    case "$branch" in
    main | master) say "You're on \`$branch\`: create a branch before committing non-trivial work." ;;
    esac
  fi
fi

# A project stacks generated and the agent hasn't finished: point at the brief.
if [ -f .stacks/BRIEF.md ] && { [ ! -s AGENTS.md ] || grep -q 'stacks:fill' AGENTS.md 2>/dev/null; }; then
  say "This project is still being built: read .stacks/BRIEF.md before starting. AGENTS.md is a skeleton until its stacks:fill markers are replaced."
fi

# Dependencies: a package.json without node_modules/ or a pyproject.toml without
# .venv/, at the root or one directory down (backend/ + frontend/ monorepos).
# lint-on-write and format-changed only use project-local tools, so without an
# install they go quiet. No recursive walk: the root plus its immediate subdirs.
node_install_cmd() {
  local d=$1 lock
  for lock in "$d" .; do
    if [ -f "$lock/bun.lock" ] || [ -f "$lock/bun.lockb" ]; then echo "bun install"; return; fi
    if [ -f "$lock/pnpm-lock.yaml" ]; then echo "pnpm install"; return; fi
    if [ -f "$lock/yarn.lock" ]; then echo "yarn install"; return; fi
    if [ -f "$lock/package-lock.json" ]; then echo "npm install"; return; fi
  done
  echo "npm install"
}
# Workspace members install into the root node_modules.
node_workspace=false
if [ -d node_modules ] && { [ -f pnpm-workspace.yaml ] || grep -q '"workspaces"' package.json 2>/dev/null; }; then
  node_workspace=true
fi
missing=""
hints=""
for d in . */; do
  d=${d%/}
  [ -d "$d" ] || continue
  case "$d" in node_modules | .venv | venv | vendor | dist | build | target) continue ;; esac
  label="" prefix=""
  [ "$d" = "." ] || { label="$d/"; prefix="cd $d && "; }
  if [ -f "$d/package.json" ] && [ ! -d "$d/node_modules" ] &&
    { [ "$d" = "." ] || [ "$node_workspace" = false ]; }; then
    missing="${missing:+$missing, }${label}node_modules"
    hints="${hints:+$hints; }\`$prefix$(node_install_cmd "$d")\`"
  fi
  if [ -f "$d/pyproject.toml" ] && [ ! -d "$d/.venv" ]; then
    missing="${missing:+$missing, }${label}.venv"
    hints="${hints:+$hints; }\`${prefix}uv sync\`"
  fi
done
if [ -n "$missing" ]; then
  if [ -f Makefile ] && grep -qE '^install[[:space:]]*:' Makefile; then
    hints="\`make install\`"
  fi
  say "Dependencies not installed (missing $missing), so lint-on-write and format-changed skip those files. Install with $hints."
fi

compose_file=""
for f in compose.yaml compose.yml docker-compose.yaml docker-compose.yml; do
  if [ -f "$f" ]; then
    compose_file=$f
    break
  fi
done
# `config` reads only the file (no daemon), so its failure says nothing about Docker. A file that
# needs variables not available in this session declares no known local
# services; any other failure is a file that doesn't parse. Only `ps`, which asks the daemon, can
# find Docker not responding.
if [ -n "$compose_file" ] && command -v docker >/dev/null 2>&1; then
  config_rc=0
  declared=$(with_timeout 5 docker compose -f "$compose_file" config --services) || config_rc=$?
  rc=0
  if [ "$config_rc" -eq 0 ]; then
    running=$(with_timeout 5 docker compose -f "$compose_file" ps --services --status running) || rc=$?
  fi
  if [ "$config_rc" -ne 0 ]; then
    if ! grep -qE 'required variable .* is missing a value' <<<"$declared"; then
      say "docker compose can't read $compose_file, so its services can't be checked. Run docker compose -f $compose_file config to see why."
    fi
  elif [ "$rc" -ne 0 ]; then
    say "Docker isn't responding, so the services in $compose_file can't be checked. Tests that need them will fail until Docker is running."
  else
    down=""
    while IFS= read -r svc; do
      # with_timeout merges stderr; compose warnings are not service names.
      grep -qE '^[A-Za-z0-9._-]+$' <<<"$svc" || continue
      grep -qxF -e "$svc" <<<"$running" || down="${down:+$down, }$svc"
    done <<<"$declared"
    if [ -n "$down" ]; then
      say "Services declared in $compose_file but not running: $down. Start them with the project's service command (see Commands in AGENTS.md) before running tests that need them."
    fi
  fi
fi

[ "${#lines[@]}" -eq 0 ] && exit 0
# At most 15 lines, whatever checks a project adds above.
shown=0
for line in "${lines[@]}"; do
  [ "$shown" -ge 15 ] && break
  printf '%s\n' "$line"
  shown=$((shown + 1))
done
exit 0
