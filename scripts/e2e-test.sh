#!/usr/bin/env bash
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

    if echo "$output" | grep -q "$expected"; then
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
    test_sync_command
    test_exec_command
    test_run_command
    test_task_execution
    test_parallel_execution
    test_error_cases

    print_summary
}

main "$@"
