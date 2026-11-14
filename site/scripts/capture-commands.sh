#!/usr/bin/env bash
# Records the Commands section: each command runs for real against the rr
# repository's own hosts with --pretty, and its output becomes a fixture.
#
#   scripts/capture-commands.sh
#
# `rr init` runs in an empty temp project with HOME pointed at a copy of ~/.rr, so
# it can't touch your real global config. Raw output goes to fixtures/raw/commands/
# (gitignored); scrub-text.mjs writes fixtures/commands/<name>.txt.
set -euo pipefail

site=$(cd "$(dirname "$0")/.." && pwd)
repo=$(cd "$site/.." && pwd)
raw="$site/fixtures/raw/commands"
out="$site/fixtures/commands"
rm -rf "$raw"
mkdir -p "$raw" "$out"

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT

# name|directory|command (the command is what the page shows after "$ ").
commands=(
  "init|$scratch/project|rr init --non-interactive --skip-probe --host m4-mini"
  "doctor|$repo|rr doctor"
  "status|$repo|rr status"
  "run|$repo|rr run \"go test ./internal/lock/...\""
  "test|$repo|rr test-all"
  "logs|$repo|rr logs"
)

mkdir -p "$scratch/project" "$scratch/home"
cp -R "$HOME/.rr" "$scratch/home/.rr"
[ -d "$HOME/.ssh" ] && ln -s "$HOME/.ssh" "$scratch/home/.ssh"

pairs=()
manifest="["
for entry in "${commands[@]}"; do
  IFS='|' read -r name dir command <<<"$entry"
  echo "\$ $command"
  pretty=${command/#rr /rr --pretty }
  env_home=$HOME
  [ "$name" = init ] && env_home="$scratch/home"
  set +e
  (cd "$dir" && HOME="$env_home" bash -c "$pretty") >"$raw/$name.ansi" 2>&1
  status=$?
  set -e
  echo "  exit $status"
  pairs+=("$raw/$name.ansi" "$out/$name.txt")
  manifest+=$(jq -cn --arg name "$name" --arg command "$command" --argjson exit "$status" \
    '{name: $name, command: $command, exit_code: $exit}'),
done
manifest="${manifest%,}]"
jq . <<<"$manifest" >"$out/manifest.json"

node "$site/scripts/scrub-text.mjs" "${pairs[@]}"
