#!/usr/bin/env bash
# Records the rr output in the Agent session: the runs an agent makes while fixing
# a failing test in the rr repository itself.
#
#   scripts/capture-agent.sh
#
# In a temporary detached worktree it applies fixtures/agent/break.patch (itoa
# loses its negative-number branch, so TestItoa fails), then runs what the agent
# runs, in JSON mode as an agent sees it:
#   1. rr run "go test ./internal/lock/... -run TestItoa"   (fails)
#   2. the fix (the patch reverted), then the same filtered run   (passes)
#   3. rr test, the full suite
# Raw output goes to fixtures/raw/agent/; scrubbed copies to fixtures/agent/.
set -euo pipefail

site=$(cd "$(dirname "$0")/.." && pwd)
repo=$(cd "$site/.." && pwd)
raw="$site/fixtures/raw/agent"
out="$site/fixtures/agent"
rm -rf "$raw"
mkdir -p "$raw" "$out"

tree=$(mktemp -d)/rr
git -C "$repo" worktree add --detach --quiet "$tree" HEAD
trap 'git -C "$repo" worktree remove --force "$tree"' EXIT
cd "$tree"

filtered='rr run "go test ./internal/lock/... -run TestItoa"'

step() {
  local name=$1
  shift
  echo "\$ $*"
  set +e
  bash -c "$*" >"$raw/$name.stdout" 2>"$raw/$name.jsonl"
  echo "  exit $?"
  set -e
}

git apply "$site/fixtures/agent/break.patch"
step fail "$filtered"
git apply -R "$site/fixtures/agent/break.patch"
git diff --exit-code --quiet || { echo "worktree not clean after the fix" >&2; exit 1; }
step pass "$filtered"
step full "rr test"

if grep -q '"reason":"local_fallback"' "$raw"/*.jsonl; then
  echo "rr fell back to local execution; not a remote run. Fixtures not written." >&2
  exit 1
fi

pairs=()
for name in fail pass full; do
  pairs+=("$raw/$name.jsonl" "$out/$name.jsonl" "$raw/$name.stdout" "$out/$name.stdout.txt")
done
node "$site/scripts/scrub-text.mjs" "${pairs[@]}"
