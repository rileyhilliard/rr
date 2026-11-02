#!/usr/bin/env bash
# Records one real rr run for the landing page's Monitor stage.
#
#   scripts/capture-demo.sh <project-dir> <host,host,host> [task]
#
# While `rr test` (or [task]) runs in <project-dir>, `rr monitor --once --json`
# samples the hosts back to back (each snapshot spans about two seconds). Then the
# same task runs again with --pretty for the human transcript. Raw output lands in
# fixtures/raw/ (gitignored); scripts/scrub-fixtures.mjs turns it into the
# committed fixtures and prints what it replaced, for review before committing.
#
# Fails, without writing fixtures, when a host is offline or rr reports a local
# fallback, so a capture taken with hosts unreachable can't pass for a fleet run.
set -euo pipefail

if [ $# -lt 2 ]; then
  sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 2
fi

project=$(cd "$1" && pwd)
hosts=$2
task=${3:-test}
site=$(cd "$(dirname "$0")/.." && pwd)
raw="$site/fixtures/raw"
command -v jq >/dev/null || { echo "jq is required (brew install jq)" >&2; exit 1; }

rm -rf "$raw"
mkdir -p "$raw"
cd "$project"

want=$(tr ',' '\n' <<<"$hosts" | grep -c .)
online=$(rr monitor --once --json --hosts "$hosts" | jq '[.data.hosts[] | select(.online)] | length')
if [ "$online" -ne "$want" ]; then
  echo "Only $online of $want hosts are online; bring them up and rerun." >&2
  exit 1
fi

sample() {
  local failures=0
  while [ ! -f "$raw/.done" ]; do
    if snap=$(rr monitor --once --json --hosts "$hosts" 2>>"$raw/monitor.stderr"); then
      jq -c '.data' <<<"$snap" >>"$raw/monitor.jsonl"
    else
      failures=$((failures + 1))
      echo "monitor sample failed ($failures)" >&2
    fi
  done
  echo "$failures" >"$raw/monitor.failures"
}

echo "Sampling $hosts; idle lead-in..."
sample &
sampler=$!
trap 'touch "$raw/.done"; wait "$sampler" 2>/dev/null || true' EXIT
sleep 6

echo "Running rr $task (JSON) in $project..."
set +e
rr "$task" >"$raw/run.stdout" 2>"$raw/run.jsonl"
status=$?
set -e
echo "rr $task exited $status"
sleep 6
touch "$raw/.done"
wait "$sampler"
trap - EXIT

if grep -q '"reason":"local_fallback"\|"local_fallback":true' "$raw/run.jsonl"; then
  echo "rr fell back to local execution; not a fleet run. Fixtures not written." >&2
  exit 1
fi

echo "Running rr --pretty $task for the human transcript..."
set +e
rr --pretty "$task" >"$raw/run.pretty.ansi" 2>&1
set -e

echo "$status" >"$raw/run.status"
node "$site/scripts/scrub-fixtures.mjs" "$raw" "$site/fixtures"
