#!/usr/bin/env bash
# Smoke test for the guards, run by the hooks-smoke verify step: each guard gets a deny and a near
# miss, and every hook config present must parse and wire its guard. Configs and guards for agents
# this project doesn't target are absent and skipped. Not wired as a hook.
#
# The force-push text and the key are assembled from pieces, so this file itself trips no guard.
# No pipefail: grep -q exits at its first match, and the writer it leaves behind would fail the pipe.
set -eu
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

h=.claude/hooks
fail() {
  echo "hooks-smoke: $*" >&2
  exit 1
}
out() { printf '%s' "$1" | bash "$2"; }
# denies|allows <what> <payload> <hook>
denies() { out "$2" "$3" | grep -q '"permission\(Decision\)\{0,1\}":"deny"' || fail "$3 should deny $1"; }
allows() { test -z "$(out "$2" "$3")" || fail "$3 should allow $1"; }

# shellcheck disable=SC2026 # split on purpose, see the header
f='git push --'force
denies "a force push" "{\"tool_input\":{\"command\":\"$f origin main\"}}" $h/block-destructive.sh
allows "a commit message naming a force push" "{\"tool_input\":{\"command\":\"git commit -m 'docs: never $f'\"}}" $h/block-destructive.sh
denies "a .env write" '{"tool_input":{"file_path":".env","content":"A=1"}}' $h/detect-secrets.sh
k=AKIA
k="${k}QQQQABCDEFGHIJKL"
denies "an AWS access key" "{\"tool_input\":{\"file_path\":\"src/k.ts\",\"content\":\"const k = '$k';\"}}" $h/detect-secrets.sh
allows "plain code" '{"tool_input":{"file_path":"src/k.ts","content":"export const k = 1;"}}' $h/detect-secrets.sh
if [ -f .codex/hooks/check-patch.sh ]; then
  denies "a patch adding an AWS access key" "{\"tool_input\":{\"command\":\"*** Begin Patch\\n*** Add File: src/k.ts\\n+const k = '$k';\\n*** End Patch\"}}" .codex/hooks/check-patch.sh
fi
if [ -f .cursor/hooks/guard.sh ]; then
  denies "a force push" "{\"tool_name\":\"Shell\",\"tool_input\":{\"command\":\"$f\"}}" .cursor/hooks/guard.sh
fi

# parses <file>: JSON with node, else python3; with neither, nothing to check with.
parses() {
  if command -v node >/dev/null 2>&1; then
    node -e 'JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))' "$1"
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c 'import json, sys; json.load(open(sys.argv[1]))' "$1"
  fi
}
for c in .claude/settings.json .codex/hooks.json .cursor/hooks.json opencode.json; do
  if [ -f "$c" ]; then parses "$c" || fail "$c isn't valid JSON"; fi
done
# wires <config> <guard>
wires() {
  if [ -f "$1" ]; then grep -q "$2" "$1" || fail "$1 doesn't wire $2"; fi
}
wires .claude/settings.json hooks/block-destructive.sh
wires .codex/hooks.json hooks/check-patch.sh
wires .cursor/hooks.json hooks/guard.sh
echo "hooks-smoke: guards and hook configs OK"
