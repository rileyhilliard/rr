#!/usr/bin/env bash
# shellcheck disable=SC2064  # We intentionally expand trap variables at definition time
#
# End-to-end CLI test script for rr
# Tests all CLI commands and flags against real hosts
#
# Usage:
#   ./scripts/e2e-test.sh          # Build and run all tests
#   ./scripts/e2e-test.sh --quick  # Skip slow tests (sync, remote execution)
#   ./scripts/e2e-test.sh --help   # Show help
#

# Note: We intentionally don't use 'set -e' because test functions return 1 on
# failure, and we want to continue running all tests to get a complete summary.
set -uo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
PASSED=0
FAILED=0
SKIPPED=0

# Options
QUICK_MODE=false
VERBOSE=false

# Binary path
RR_BIN=""

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --quick    Skip slow tests (sync, remote execution)"
    echo "  --verbose  Show command output"
    echo "  --help     Show this help"
    echo ""
    echo "This script tests all rr CLI commands and flags."
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
    PASSED=$((PASSED + 1))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    FAILED=$((FAILED + 1))
}

log_skip() {
    echo -e "${YELLOW}[SKIP]${NC} $1"
    SKIPPED=$((SKIPPED + 1))
}

# Run a test command and check exit code
run_test() {
    local name="$1"
    local expected_exit="${2:-0}"
    shift 2
    local cmd=("$@")

    if $VERBOSE; then
        echo -e "${BLUE}[TEST]${NC} $name"
        echo "  Command: ${cmd[*]}"
    fi

    local output
    local exit_code=0
    output=$("${cmd[@]}" 2>&1) || exit_code=$?

    if [[ "$exit_code" -eq "$expected_exit" ]]; then
        log_pass "$name"
        if $VERBOSE && [[ -n "$output" ]]; then
            echo "$output" | head -5 | sed 's/^/  /'
        fi
        return 0
    else
        log_fail "$name (expected exit $expected_exit, got $exit_code)"
        if [[ -n "$output" ]]; then
            echo "$output" | head -10 | sed 's/^/  /'
        fi
        return 1
    fi
}

# Run a test and check output contains expected string
# Also verifies the command exits with expected code (default 0)
run_test_contains() {
    local name="$1"
    local expected="$2"
    local expected_exit="${3:-0}"
    shift 3
    local cmd=("$@")

    local output
    local exit_code=0
    output=$("${cmd[@]}" 2>&1) || exit_code=$?

    # Check both exit code and output content
    if [[ "$exit_code" -ne "$expected_exit" ]]; then
        log_fail "$name (expected exit $expected_exit, got $exit_code)"
        echo "$output" | head -5 | sed 's/^/  /'
        return 1
    fi

    # Use grep -F for fixed string matching and -- to prevent pattern as option
    if echo "$output" | grep -qF -- "$expected"; then
        log_pass "$name"
        return 0
    else
        log_fail "$name (output doesn't contain '$expected')"
        echo "$output" | head -5 | sed 's/^/  /'
        return 1
    fi
}

# Build the binary
build_binary() {
    log_info "Building rr binary..."
    RR_BIN="$(mktemp -d)/rr"
    if go build -o "$RR_BIN" ./cmd/rr; then
        log_pass "Build successful"
    else
        log_fail "Build failed"
        exit 1
    fi
}

# Test basic commands
test_basic_commands() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Basic Commands"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    run_test "version" 0 "$RR_BIN" version
    run_test "help" 0 "$RR_BIN" --help
    run_test "help (short)" 0 "$RR_BIN" -h
    run_test_contains "version output" "rr" 0 "$RR_BIN" version
}

# Test global flags
test_global_flags() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Global Flags"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    run_test "--quiet flag" 0 "$RR_BIN" --quiet tasks
    run_test "-q flag (short)" 0 "$RR_BIN" -q tasks
    run_test "--no-color flag" 0 "$RR_BIN" --no-color tasks
    run_test "--verbose flag" 0 "$RR_BIN" --verbose tasks
    run_test "-v flag (short)" 0 "$RR_BIN" -v tasks
    run_test "--machine flag" 0 "$RR_BIN" --machine tasks
    run_test "-m flag (short)" 0 "$RR_BIN" -m tasks
}

# Test tasks command
test_tasks_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Tasks Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    run_test "tasks" 0 "$RR_BIN" tasks
    run_test_contains "tasks lists test" "test" 0 "$RR_BIN" tasks
    run_test_contains "tasks lists build" "build" 0 "$RR_BIN" tasks
    run_test "tasks --machine" 0 "$RR_BIN" tasks --machine
}

# Test doctor command
test_doctor_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Doctor Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    run_test "doctor" 0 "$RR_BIN" doctor
    run_test "doctor --machine" 0 "$RR_BIN" doctor --machine
    run_test_contains "doctor shows CONFIG" "CONFIG" 0 "$RR_BIN" doctor
    run_test_contains "doctor shows SSH" "SSH" 0 "$RR_BIN" doctor
    run_test_contains "doctor shows HOSTS" "HOSTS" 0 "$RR_BIN" doctor
}

# Test status command
test_status_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Status Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    run_test "status" 0 "$RR_BIN" status
    run_test "status --machine" 0 "$RR_BIN" status --machine
    run_test_contains "status shows HOST" "HOST" 0 "$RR_BIN" status
}

# Test host command
test_host_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Host Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    run_test "host list" 0 "$RR_BIN" host list
    run_test "host list --machine" 0 "$RR_BIN" host list --machine
    run_test_contains "host list shows config path" "Config:" 0 "$RR_BIN" host list
}

# Test logs command
test_logs_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Logs Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    run_test "logs list" 0 "$RR_BIN" logs list
    run_test "logs list --machine" 0 "$RR_BIN" logs list --machine
}

# Test completion command
test_completion_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Completion Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    run_test "completion bash" 0 "$RR_BIN" completion bash
    run_test "completion zsh" 0 "$RR_BIN" completion zsh
    run_test "completion fish" 0 "$RR_BIN" completion fish
    run_test "completion powershell" 0 "$RR_BIN" completion powershell
}

# Test init command
test_init_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Init Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN

    # Test --help
    run_test_contains "init --help" "Creates a .rr.yaml" 0 "$RR_BIN" init --help

    # Test init in empty directory with --non-interactive (succeeds, creates config without hosts)
    run_test "init non-interactive without host" 0 \
        env -C "$test_dir" RR_NON_INTERACTIVE=true "$RR_BIN" init --skip-probe

    # Test init with existing config and no --force (should fail)
    echo "version: 1" > "$test_dir/.rr.yaml"
    run_test "init existing config no force (should fail)" 1 \
        env -C "$test_dir" "$RR_BIN" init --non-interactive --host testhost --skip-probe

    # Test init with --force overwrites existing config
    run_test "init with --force overwrites" 0 \
        env -C "$test_dir" HOME="$test_dir" "$RR_BIN" init --non-interactive --host testhost --skip-probe --force
}

# Test host add command (non-interactive)
test_host_add_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Host Add Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN
    mkdir -p "$test_dir/.rr"

    # Test host add --help
    run_test_contains "host add --help" "skip-probe" 0 "$RR_BIN" host add --help

    # Test non-interactive host add with all flags
    # shellcheck disable=SC2088  # Tilde is intentionally literal for remote expansion
    run_test "host add non-interactive" 0 \
        env HOME="$test_dir" "$RR_BIN" host add --name testhost --ssh "user@testserver" --dir "~/projects" --skip-probe

    # Test host add with duplicate name (should fail)
    run_test "host add duplicate (should fail)" 1 \
        env HOME="$test_dir" "$RR_BIN" host add --name testhost --ssh "user@other" --skip-probe

    # Verify host was added to config
    run_test_contains "host add verify in list" "testhost" 0 \
        env HOME="$test_dir" "$RR_BIN" host list

    # Test host add with tags and env
    run_test "host add with tags and env" 0 \
        env HOME="$test_dir" "$RR_BIN" host add --name taggedhost --ssh "tagged@server" --tag gpu --tag fast --env "CUDA_VISIBLE_DEVICES=0" --skip-probe
}

# Test unlock command
test_unlock_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Unlock Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Test unlock --help
    run_test_contains "unlock --help" "--all" 0 "$RR_BIN" unlock --help

    # Test unlock with invalid host (should fail)
    run_test "unlock invalid host (should fail)" 1 "$RR_BIN" unlock nonexistenthost
}

# Test logs clean command
test_logs_clean_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Logs Clean Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN
    mkdir -p "$test_dir/.rr/logs"

    # Test logs clean --help
    run_test_contains "logs clean --help" "--older" 0 "$RR_BIN" logs clean --help
    run_test_contains "logs clean --help shows --all" "--all" 0 "$RR_BIN" logs clean --help

    # Test logs clean with no logs (should succeed with no-op)
    run_test "logs clean empty" 0 env HOME="$test_dir" "$RR_BIN" logs clean

    # Test logs clean --all on empty
    run_test "logs clean --all empty" 0 env HOME="$test_dir" "$RR_BIN" logs clean --all

    # Test logs clean --older with valid duration
    run_test "logs clean --older 7d" 0 env HOME="$test_dir" "$RR_BIN" logs clean --older 7d

    # Test logs clean --older with invalid duration (should fail)
    run_test "logs clean --older invalid (should fail)" 1 "$RR_BIN" logs clean --older "badvalue"
}

# Test setup command (basic help only - requires SSH for full test)
test_setup_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Setup Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Test setup --help
    run_test_contains "setup --help" "SSH" 0 "$RR_BIN" setup --help

    # Test setup with no args (should fail - requires host)
    run_test "setup no args (should fail)" 1 "$RR_BIN" setup
}

# Test JSON output modes
test_json_output() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing JSON Output Modes"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Test tasks --json
    run_test_contains "tasks --json" "\"tasks\"" 0 "$RR_BIN" tasks --json

    # Test host list --json (use temp home with hosts)
    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN
    mkdir -p "$test_dir/.rr"

    # Add a host first
    env HOME="$test_dir" "$RR_BIN" host add --name jsontest --ssh "test@server" --skip-probe >/dev/null 2>&1

    run_test_contains "host list --json" "\"hosts\"" 0 \
        env HOME="$test_dir" "$RR_BIN" host list --json

    run_test_contains "host list --json has name" "jsontest" 0 \
        env HOME="$test_dir" "$RR_BIN" host list --json
}

# Test --config flag
test_config_flag() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing --config Flag"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN

    # Create a custom config as .rr.yaml in test dir (tasks command looks for .rr.yaml in cwd)
    cat > "$test_dir/.rr.yaml" << 'EOF'
version: 1
local_fallback: true
tasks:
  custom-task:
    description: Custom task from custom config
    run: echo custom-output
EOF

    # Test tasks lists custom task when in directory with .rr.yaml
    run_test_contains "config: custom config tasks" "custom-task" 0 \
        env -C "$test_dir" "$RR_BIN" tasks

    # Test --config with invalid path from empty dir (should fail)
    local empty_dir
    empty_dir=$(mktemp -d)
    run_test "config: invalid path (should fail)" 1 \
        env -C "$empty_dir" "$RR_BIN" --config "/nonexistent/path.yaml" tasks
    rm -rf "$empty_dir"
}

# Test --local flag with tasks
test_local_flag() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing --local Flag"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN

    cat > "$test_dir/.rr.yaml" << 'EOF'
version: 1
local_fallback: false
tasks:
  local-test:
    run: echo local-test-output
EOF

    # Test --local forces local execution even when local_fallback is false
    run_test_contains "local: --local flag works" "local-test-output" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" local-test --local
}

# Test sync command
test_sync_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Sync Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    if $QUICK_MODE; then
        log_skip "sync (quick mode)"
        log_skip "sync --dry-run (quick mode)"
        return
    fi

    run_test "sync --dry-run" 0 "$RR_BIN" sync --dry-run
    run_test "sync" 0 "$RR_BIN" sync
}

# Test exec command
test_exec_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Exec Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    if $QUICK_MODE; then
        log_skip "exec (quick mode)"
        return
    fi

    run_test "exec echo test" 0 "$RR_BIN" exec "echo test"
    run_test_contains "exec output" "test" 0 "$RR_BIN" exec "echo test"
    run_test "exec --quiet" 0 "$RR_BIN" exec --quiet "echo test"
}

# Test run command
test_run_command() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Run Command"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    if $QUICK_MODE; then
        log_skip "run (quick mode)"
        return
    fi

    run_test "run echo test" 0 "$RR_BIN" run "echo test"
    run_test "run --local" 0 "$RR_BIN" run --local "echo test"
    run_test "run --quiet" 0 "$RR_BIN" run --quiet "echo test"
}

# Test task execution
test_task_execution() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Task Execution"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    if $QUICK_MODE; then
        log_skip "task: test (quick mode)"
        log_skip "task: lint (quick mode)"
        log_skip "task: vet (quick mode)"
        return
    fi

    run_test "task: vet" 0 "$RR_BIN" vet
    run_test "task: lint" 0 "$RR_BIN" lint
    run_test "task: test" 0 "$RR_BIN" test
}

# Test parallel execution
test_parallel_execution() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Parallel Execution"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    if $QUICK_MODE; then
        log_skip "parallel: quick-check (quick mode)"
        return
    fi

    run_test "parallel: quick-check" 0 "$RR_BIN" quick-check
}

# Test parallel task flags
test_parallel_flags() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Parallel Task Flags"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Create temp config with parallel task
    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN

    cat > "$test_dir/.rr.yaml" << 'EOF'
version: 1
local_fallback: true
tasks:
  task1:
    run: echo task1-done
  task2:
    run: echo task2-done
  parallel-test:
    description: Test parallel execution
    parallel:
      - task1
      - task2
    fail_fast: false
EOF

    # Test --dry-run flag (works without execution)
    run_test_contains "parallel: --dry-run" "Dry run" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" parallel-test --dry-run

    # Test --dry-run shows tasks
    run_test_contains "parallel: --dry-run shows tasks" "task1" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" parallel-test --dry-run

    if $QUICK_MODE; then
        log_skip "parallel: --local (quick mode)"
        log_skip "parallel: --fail-fast (quick mode)"
        log_skip "parallel: --max-parallel (quick mode)"
        log_skip "parallel: --no-logs (quick mode)"
        log_skip "parallel: --stream (quick mode)"
        return
    fi

    # Test --local flag with parallel tasks (check summary shows success)
    run_test_contains "parallel: --local" "passed" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" parallel-test --local

    # Test --fail-fast flag
    run_test "parallel: --fail-fast" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" parallel-test --local --fail-fast

    # Test --max-parallel flag
    run_test "parallel: --max-parallel 1" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" parallel-test --local --max-parallel 1

    # Test --no-logs flag
    run_test "parallel: --no-logs" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" parallel-test --local --no-logs

    # Test --stream flag (check for task output in stream mode)
    run_test_contains "parallel: --stream" "task1-done" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" parallel-test --local --stream
}

# Test task dependencies
test_task_dependencies() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Task Dependencies"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Create a temp directory for dependency test config
    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN

    # Create test config with dependent tasks
    cat > "$test_dir/.rr.yaml" << 'EOF'
version: 1
local_fallback: true
tasks:
  step1:
    run: echo step1-output
  step2:
    depends:
      - step1
    run: echo step2-output
  pipeline:
    depends:
      - step1
      - step2
    description: Run the full pipeline
EOF

    # Test --local flag with dependencies (should work without remote hosts)
    run_test_contains "deps: linear chain" "step1-output" 0 "$RR_BIN" --config "$test_dir/.rr.yaml" step2 --local
    run_test_contains "deps: pipeline" "step2-output" 0 "$RR_BIN" --config "$test_dir/.rr.yaml" pipeline --local

    # Test --skip-deps flag
    run_test_contains "deps: --skip-deps" "step2-output" 0 "$RR_BIN" --config "$test_dir/.rr.yaml" step2 --local --skip-deps

    # Verify --skip-deps actually skips dependencies (step1 output should NOT appear)
    local output
    output=$("$RR_BIN" --config "$test_dir/.rr.yaml" step2 --local --skip-deps 2>&1)
    if [[ "$output" == *"step1-output"* ]]; then
        log_fail "deps: --skip-deps should not run step1"
    else
        log_pass "deps: --skip-deps correctly skips dependencies"
    fi

    # Test help shows dependency info
    run_test_contains "deps: help shows dependencies" "Dependencies:" 0 "$RR_BIN" --config "$test_dir/.rr.yaml" step2 --help
    run_test_contains "deps: help shows --skip-deps flag" "--skip-deps" 0 "$RR_BIN" --config "$test_dir/.rr.yaml" step2 --help

    # Test parallel dependencies
    cat > "$test_dir/.rr.yaml" << 'EOF'
version: 1
local_fallback: true
tasks:
  lint:
    run: echo lint-output
  typecheck:
    run: echo typecheck-output
  test:
    run: echo test-output
  ci:
    depends:
      - parallel: [lint, typecheck]
      - test
    description: Run CI pipeline
EOF

    run_test_contains "deps: parallel group" "lint-output" 0 "$RR_BIN" --config "$test_dir/.rr.yaml" ci --local
    run_test_contains "deps: parallel group (typecheck)" "typecheck-output" 0 "$RR_BIN" --config "$test_dir/.rr.yaml" ci --local
    run_test_contains "deps: parallel group (test)" "test-output" 0 "$RR_BIN" --config "$test_dir/.rr.yaml" ci --local
}

# Test --from flag for dependencies
test_from_flag() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing --from Flag for Dependencies"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN

    cat > "$test_dir/.rr.yaml" << 'EOF'
version: 1
local_fallback: true
tasks:
  step1:
    run: echo step1-output
  step2:
    depends:
      - step1
    run: echo step2-output
  step3:
    depends:
      - step2
    run: echo step3-output
EOF

    # Test --from flag starts from specified task
    local output
    output=$("$RR_BIN" --config "$test_dir/.rr.yaml" step3 --local --from step2 2>&1)

    # Should NOT contain step1-output (skipped)
    if [[ "$output" == *"step1-output"* ]]; then
        log_fail "from: --from step2 should skip step1"
    else
        log_pass "from: --from step2 skips step1"
    fi

    # Should contain step2-output
    if [[ "$output" == *"step2-output"* ]]; then
        log_pass "from: --from step2 runs step2"
    else
        log_fail "from: --from step2 should run step2"
    fi

    # Test --from help is shown
    run_test_contains "from: help shows --from flag" "--from" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" step3 --help
}

# Test multi-step tasks
test_multi_step_tasks() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Multi-Step Tasks"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN

    cat > "$test_dir/.rr.yaml" << 'EOF'
version: 1
local_fallback: true
tasks:
  multi-step:
    description: Multi-step task
    steps:
      - name: Step 1
        run: echo step-one
      - name: Step 2
        run: echo step-two
      - name: Step 3
        run: echo step-three
EOF

    # Test multi-step task execution
    local output
    output=$("$RR_BIN" --config "$test_dir/.rr.yaml" multi-step --local 2>&1)

    if [[ "$output" == *"step-one"* ]] && [[ "$output" == *"step-two"* ]] && [[ "$output" == *"step-three"* ]]; then
        log_pass "multi-step: all steps execute"
    else
        log_fail "multi-step: all steps should execute"
        echo "$output" | head -10 | sed 's/^/  /'
    fi

    # Test help shows steps
    run_test_contains "multi-step: help shows description" "Multi-step task" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" multi-step --help
}

# Test task argument passing
test_task_arguments() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Task Argument Passing"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    local test_dir
    test_dir=$(mktemp -d)
    trap "rm -rf '$test_dir'" RETURN

    cat > "$test_dir/.rr.yaml" << 'EOF'
version: 1
local_fallback: true
tasks:
  echo-args:
    description: Echo arguments
    run: echo
EOF

    # Test passing arguments to tasks
    run_test_contains "args: pass extra args" "extra-arg" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" echo-args --local "extra-arg"

    run_test_contains "args: pass multiple args" "arg2" 0 \
        "$RR_BIN" --config "$test_dir/.rr.yaml" echo-args --local "arg1" "arg2"
}

# Test error cases
test_error_cases() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Testing Error Cases"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    run_test "run with no args (should fail)" 1 "$RR_BIN" run
    run_test "exec with no args (should fail)" 1 "$RR_BIN" exec
    run_test "unknown command (should fail)" 1 "$RR_BIN" unknowncommand
    run_test "invalid task (should fail)" 1 "$RR_BIN" nonexistenttask
}

# Print summary
print_summary() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Test Summary"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo -e "  ${GREEN}Passed:${NC}  $PASSED"
    echo -e "  ${RED}Failed:${NC}  $FAILED"
    echo -e "  ${YELLOW}Skipped:${NC} $SKIPPED"
    echo ""

    local total=$((PASSED + FAILED))
    if [[ $FAILED -eq 0 ]]; then
        echo -e "${GREEN}All $total tests passed!${NC}"
        return 0
    else
        echo -e "${RED}$FAILED of $total tests failed${NC}"
        return 1
    fi
}

# Cleanup
cleanup() {
    if [[ -n "$RR_BIN" && -f "$RR_BIN" ]]; then
        rm -f "$RR_BIN"
        rmdir "$(dirname "$RR_BIN")" 2>/dev/null || true
    fi
}

# Main
main() {
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --quick)
                QUICK_MODE=true
                shift
                ;;
            --verbose)
                VERBOSE=true
                shift
                ;;
            --help|-h)
                usage
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done

    trap cleanup EXIT

    echo ""
    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║              rr End-to-End CLI Test Suite                        ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    echo ""

    if $QUICK_MODE; then
        log_info "Running in quick mode (skipping slow tests)"
    fi

    # Change to project root
    cd "$(dirname "$0")/.." || exit 1

    # Build and run tests
    build_binary

    test_basic_commands
    test_global_flags
    test_tasks_command
    test_doctor_command
    test_status_command
    test_host_command
    test_logs_command
    test_completion_command
    test_init_command
    test_host_add_command
    test_unlock_command
    test_logs_clean_command
    test_setup_command
    test_json_output
    test_config_flag
    test_local_flag
    test_sync_command
    test_exec_command
    test_run_command
    test_task_execution
    test_parallel_execution
    test_parallel_flags
    test_task_dependencies
    test_from_flag
    test_multi_step_tasks
    test_task_arguments
    test_error_cases

    print_summary
}

main "$@"
