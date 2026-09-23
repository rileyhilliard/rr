#!/bin/bash
set -e

# Generate shell completion scripts for rr

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

mkdir -p completions

# rr registers the tasks from any .rr.yaml it finds as commands, and the
# generated scripts would list them. Build and run from an empty temp dir so
# only rr's own commands end up in the completions.
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

echo "Building rr..."
go build -o "$WORK_DIR/rr" ./cmd/rr

echo "Generating completions..."
(cd "$WORK_DIR" && ./rr completion bash) > completions/rr.bash
(cd "$WORK_DIR" && ./rr completion zsh) > completions/_rr
(cd "$WORK_DIR" && ./rr completion fish) > completions/rr.fish
(cd "$WORK_DIR" && ./rr completion powershell) > completions/rr.ps1

echo "Generated completions in completions/"
ls -la completions/
