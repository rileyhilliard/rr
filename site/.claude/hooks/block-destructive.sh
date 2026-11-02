#!/usr/bin/env bash
# PreToolUse (Bash): deny commands that destroy work or data without a way back
# (rm of /, ~ or ., git reset --hard, force push, DROP TABLE, publishing...).
# The patterns live in _destructive-patterns.sh.
#
# A deny is returned as JSON with exit 0, which the hooks contract honors, and
# the reason is shown to the agent so it can ask the user instead. Commands that
# don't match produce no output, and the normal permission flow applies.
#
# The worst of these are also listed under permissions.deny in settings.json, a
# second layer that holds even when hooks are turned off.
set -euo pipefail

hook_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=_lib.sh
source "$hook_dir/_lib.sh"
# shellcheck source=_destructive-patterns.sh
source "$hook_dir/_destructive-patterns.sh"

input=$(cat)
# Prefer lexed commands, resolving from the hook even when the tool runs elsewhere.
if [ -f "$hook_dir/guard-commands.ts" ] && command -v bun >/dev/null 2>&1; then
  export CLAUDE_PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$(cd "$hook_dir/../.." && pwd -P)}"
  if output=$(printf '%s' "$input" | bun "$hook_dir/guard-commands.ts" 2>/dev/null); then
    [ -z "$output" ] || printf '%s\n' "$output"
  else
    hook_deny "BLOCKED: The command guard failed to run. Ask the user first. Never work around this block."
  fi
  exit 0
fi

command=$(hook_get "$input" tool_input.command) || exit 0
[ -z "$command" ] && exit 0
found=$(is_destructive "$command") || exit 0
reason=${found%%$'\t'*}
suggestion=${found#*$'\t'}
hook_deny "BLOCKED: $reason"$'\n\n'"$suggestion"$'\n'"Never work around this block."
exit 0
