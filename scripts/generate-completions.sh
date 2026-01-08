#!/bin/bash
set -e

# Generate shell completion scripts for rr

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

mkdir -p completions

echo "Building rr..."
go build -o rr ./cmd/rr

echo "Generating completions..."
./rr completion bash > completions/rr.bash
./rr completion zsh > completions/_rr
./rr completion fish > completions/rr.fish
./rr completion powershell > completions/rr.ps1

echo "Generated completions in completions/"
ls -la completions/
