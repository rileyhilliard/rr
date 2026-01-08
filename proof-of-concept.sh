#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Description: Syncs code to remote runner and executes tests.
# Usage: ./scripts/test_remote.sh [pytest_args]

# Examples:
#   1. Run ALL tests (OpenData + Backend) in parallel (quiet mode):
#      ./scripts/test_remote.sh
#
#   2. Run ONLY OpenData tests (all):
#      ./scripts/test_remote.sh opendata
#
#   3. Run ONLY Backend tests (all):
#      ./scripts/test_remote.sh backend
#
#   4. Run specific OpenData test file:
#      ./scripts/test_remote.sh tests/test_ingestion.py
#
#   5. Run specific Backend test file:
#      ./scripts/test_remote.sh backend/tests/api/test_users.py
#
#   6. Run matching tests by keyword (in OpenData):
#      ./scripts/test_remote.sh "-k ingestion"
#
#   7. Run matching tests by keyword (in Backend):
#      ./scripts/test_remote.sh "backend/ -k auth"

set -eu
set -o pipefail

# Colors - semantic palette
RED='\033[0;31m'
RED_BOLD='\033[1;31m'
GREEN='\033[0;32m'
GREEN_BOLD='\033[1;32m'
YELLOW='\033[0;33m'
YELLOW_BOLD='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
GRAY='\033[0;90m'
DIM='\033[2m'
BOLD='\033[1m'
UNDERLINE='\033[4m'
NC='\033[0m'

# Symbols - minimal, meaningful
SYM_FAIL='✗'
SYM_PASS='✓'
SYM_ARROW='→'
SYM_DOT='·'
SYM_BLOCK='█'

ARGS="${1:-}"

# Initialize log variables early for safe cleanup
OD_LOG=""
BE_LOG=""

# Check connectivity to remote hosts in order of preference:
# 1. mini-local (LAN - fastest)
# 2. mini (Tailscale - fallback)
# 3. Local execution (last resort)
USE_LOCAL=""
REMOTE_HOST=""

CONN_START=$(date +%s.%N)
echo -e "${YELLOW}Checking connectivity to remote hosts...${NC}"

if ssh -q -o BatchMode=yes -o ConnectTimeout=2 mini-local exit 2>/dev/null; then
    REMOTE_HOST="mini-local"
    CONN_END=$(date +%s.%N)
    CONN_TIME=$(echo "$CONN_END - $CONN_START" | bc)
    echo -e "${GREEN}✓ Connected via LAN (mini-local) (${CONN_TIME}s)${NC}"
elif ssh -q -o BatchMode=yes -o ConnectTimeout=2 mini exit 2>/dev/null; then
    REMOTE_HOST="mini"
    CONN_END=$(date +%s.%N)
    CONN_TIME=$(echo "$CONN_END - $CONN_START" | bc)
    echo -e "${GREEN}✓ Connected via Tailscale (mini) (${CONN_TIME}s)${NC}"
else
    echo -e "${YELLOW}⚠️  No remote hosts reachable. Falling back to ${RED}LOCAL${YELLOW} execution.${NC}"
    USE_LOCAL="1"
fi

# Default cleanup function (will be overridden with lock cleanup if using remote)
cleanup() {
    # Remove temp logs if they were created
    [ -n "$OD_LOG" ] && rm -f "$OD_LOG"
    [ -n "$BE_LOG" ] && rm -f "$BE_LOG"
}
trap cleanup EXIT

# ─────────────────────────────────────────────────────────────────────────────
# Failure Report Functions
# ─────────────────────────────────────────────────────────────────────────────

# Extract failed test info from pytest output
# Returns: file:line::test_name for each failure
extract_failed_tests() {
    local log_file="$1"
    local prefix="$2"
    grep -E "^FAILED |^ERROR " "$log_file" 2>/dev/null | \
        sed "s|tests/|${prefix}tests/|g" | \
        sed 's/^FAILED //' | \
        sed 's/^ERROR //' | \
        sed 's/ - .*$//' || true
}

# Extract short error reason from FAILED line
extract_error_reason() {
    local log_file="$1"
    grep -E "^FAILED |^ERROR " "$log_file" 2>/dev/null | \
        sed 's/^FAILED [^ ]* - //' | \
        sed 's/^ERROR [^ ]* - //' || true
}

# Extract failure tracebacks
extract_failure_details() {
    local log_file="$1"
    local prefix="$2"

    if grep -q "^=* FAILURES =*$" "$log_file" 2>/dev/null; then
        awk '/^=* FAILURES =*$/,/^=* short test summary/' "$log_file" | \
            grep -v "^=* short test summary" | \
            sed "s|tests/|${prefix}tests/|g"
    elif grep -q "^=* ERRORS =*$" "$log_file" 2>/dev/null; then
        awk '/^=* ERRORS =*$/,/^=* short test summary/' "$log_file" | \
            grep -v "^=* short test summary" | \
            sed "s|tests/|${prefix}tests/|g"
    fi
}

# Count failures in a log file
count_failures() {
    local log_file="$1"
    grep -cE "^FAILED |^ERROR " "$log_file" 2>/dev/null || echo "0"
}

# Print a divider line
print_divider() {
    echo -e "${GRAY}────────────────────────────────────────────────────────────────────────────${NC}"
}

# Print suite failure section
print_suite_failures() {
    local suite_name="$1"
    local log_file="$2"
    local prefix="$3"
    local exit_code="$4"

    [ "$exit_code" -eq 0 ] && return

    local fail_count
    fail_count=$(count_failures "$log_file")

    echo ""
    print_divider
    echo ""
    echo -e "  ${RED_BOLD}${SYM_FAIL} ${suite_name}${NC}  ${GRAY}${SYM_DOT}${NC}  ${RED}${fail_count} failed${NC}"
    echo ""

    # Failed tests with clickable paths
    local failed_tests
    failed_tests=$(extract_failed_tests "$log_file" "$prefix")
    local error_reasons
    error_reasons=$(extract_error_reason "$log_file")

    if [ -n "$failed_tests" ]; then
        local i=1
        echo "$failed_tests" | while IFS= read -r test; do
            if [ -n "$test" ]; then
                # Extract just the test function name for display
                local test_name
                test_name=$(echo "$test" | sed 's/.*:://' | sed 's/\[.*\]//')
                local test_path
                test_path=$(echo "$test" | sed 's/::[^:]*$//')

                # Get corresponding error reason
                local reason
                reason=$(echo "$error_reasons" | sed -n "${i}p" | cut -c1-60)

                echo -e "     ${CYAN}${test_path}${NC}"
                echo -e "     ${WHITE}${SYM_ARROW} ${test_name}${NC}"
                if [ -n "$reason" ]; then
                    echo -e "       ${GRAY}${reason}${NC}"
                fi
                echo ""
                ((i++)) || true
            fi
        done
    fi

    # Traceback details (collapsed by default feel - dimmed)
    local details
    details=$(extract_failure_details "$log_file" "$prefix")

    if [ -n "$details" ]; then
        echo -e "  ${YELLOW_BOLD}Traceback${NC}"
        echo ""
        echo "$details" | while IFS= read -r line; do
            # Highlight assertion errors and important lines
            if echo "$line" | grep -qE "^E |AssertionError|Error:|Exception:"; then
                echo -e "  ${RED}${line}${NC}"
            elif echo "$line" | grep -qE "^>|^    >"; then
                echo -e "  ${WHITE}${line}${NC}"
            elif echo "$line" | grep -qE "^_+.*_+$"; then
                echo -e "  ${GRAY}${line}${NC}"
            else
                echo -e "  ${DIM}${line}${NC}"
            fi
        done
    else
        echo -e "  ${YELLOW_BOLD}Output${NC} ${GRAY}(last 20 lines)${NC}"
        echo ""
        tail -n 20 "$log_file" | sed "s|tests/|${prefix}tests/|g" | while IFS= read -r line; do
            echo -e "  ${DIM}${line}${NC}"
        done
    fi
}

# Print consolidated failure report (both suites)
print_consolidated_report() {
    local od_exit="$1"
    local be_exit="$2"
    local od_log="$3"
    local be_log="$4"

    # Count totals
    local od_count=0 be_count=0 total=0
    [ "$od_exit" -ne 0 ] && od_count=$(count_failures "$od_log")
    [ "$be_exit" -ne 0 ] && be_count=$(count_failures "$be_log")
    total=$((od_count + be_count))

    # Header
    echo ""
    echo ""
    echo -e "${RED_BOLD}${SYM_BLOCK}${SYM_BLOCK}${SYM_BLOCK} TESTS FAILED${NC}"
    echo ""
    echo -e "  ${WHITE}${total} test(s) failed${NC}  ${GRAY}${SYM_DOT}  scroll up for full output${NC}"

    # Quick summary
    echo ""
    if [ "$od_exit" -ne 0 ]; then
        echo -e "  ${RED}${SYM_FAIL}${NC} OpenData  ${GRAY}${od_count} failed${NC}"
    else
        echo -e "  ${GREEN}${SYM_PASS}${NC} OpenData  ${GRAY}passed${NC}"
    fi
    if [ "$be_exit" -ne 0 ]; then
        echo -e "  ${RED}${SYM_FAIL}${NC} Backend   ${GRAY}${be_count} failed${NC}"
    else
        echo -e "  ${GREEN}${SYM_PASS}${NC} Backend   ${GRAY}passed${NC}"
    fi

    # Detailed sections
    [ "$od_exit" -ne 0 ] && print_suite_failures "OpenData" "$od_log" "opendata/" "$od_exit"
    [ "$be_exit" -ne 0 ] && print_suite_failures "Backend" "$be_log" "backend/" "$be_exit"

    echo ""
    print_divider
    echo ""
}

# Print single-suite report
print_single_suite_report() {
    local suite_name="$1"
    local log_file="$2"
    local prefix="$3"
    local exit_code="$4"

    if [ "$exit_code" -eq 0 ]; then
        echo ""
        echo -e "${GREEN_BOLD}${SYM_PASS} ${suite_name} tests passed${NC}"
        echo ""
        return
    fi

    local fail_count
    fail_count=$(count_failures "$log_file")

    # Header
    echo ""
    echo ""
    echo -e "${RED_BOLD}${SYM_BLOCK}${SYM_BLOCK}${SYM_BLOCK} TESTS FAILED${NC}"
    echo ""
    echo -e "  ${WHITE}${fail_count} test(s) failed${NC}  ${GRAY}${SYM_DOT}  scroll up for full output${NC}"

    # Details
    print_suite_failures "$suite_name" "$log_file" "$prefix" "$exit_code"

    echo ""
    print_divider
    echo ""
}

if [ -z "$USE_LOCAL" ]; then
    # 1. Acquire lock (blocking with timeout)
    # Uses mkdir as atomic lock primitive (works on macOS and Linux)
    # Lock directory contains info file with holder details
    REMOTE_LOCK_DIR="/tmp/opendata-test.lock"
    REMOTE_LOCK_INFO="/tmp/opendata-test.lock/info"

    echo -e "${YELLOW}[REMOTE] Acquiring lock...${NC}"

    LOCK_WAIT_START=$(date +%s)
    LOCK_ACQUIRED=""

    while [ -z "$LOCK_ACQUIRED" ]; do
        # Check for stale lock (older than 5 minutes - tests typically complete in ~2 min)
        if ssh "$REMOTE_HOST" "[ -d $REMOTE_LOCK_DIR ] && [ -f $REMOTE_LOCK_INFO ] && find $REMOTE_LOCK_INFO -mmin +5 | grep -q ." 2>/dev/null; then
            echo -e "${YELLOW}⚠️  Found stale lock (>5m old). Removing...${NC}"
            ssh "$REMOTE_HOST" "rm -rf $REMOTE_LOCK_DIR" 2>/dev/null || true
        fi

        # Try to acquire lock (mkdir is atomic)
        if ssh "$REMOTE_HOST" "mkdir $REMOTE_LOCK_DIR 2>/dev/null && echo 'Locked by $USER@$(hostname) at $(date)' > $REMOTE_LOCK_INFO"; then
            LOCK_ACQUIRED="1"
        else
            # Lock is held by another process - show waiting message with holder info
            ELAPSED=$(($(date +%s) - LOCK_WAIT_START))
            if [ $ELAPSED -gt 300 ]; then
                echo -e "${RED}ERROR: Timed out waiting for lock after 5 minutes${NC}" >&2
                ssh "$REMOTE_HOST" "cat $REMOTE_LOCK_INFO 2>/dev/null" >&2 || true
                exit 1
            fi

            HOLDER=$(ssh "$REMOTE_HOST" "cat $REMOTE_LOCK_INFO 2>/dev/null" || echo "unknown")
            echo -e "${YELLOW}[REMOTE] Lock held: ${HOLDER}. Waiting... (${ELAPSED}s)${NC}"
            sleep 3
        fi
    done
    echo -e "${GREEN}[REMOTE] Lock acquired.${NC}"

    # Update cleanup to release the lock
    cleanup() {
        # Remove temp logs if they were created
        [ -n "$OD_LOG" ] && rm -f "$OD_LOG"
        [ -n "$BE_LOG" ] && rm -f "$BE_LOG"

        # Release the lock
        if [ -z "$USE_LOCAL" ]; then
            ssh "$REMOTE_HOST" "rm -rf $REMOTE_LOCK_DIR" 2>/dev/null || true
        fi
    }
    trap cleanup EXIT

    # 2. Sync files
    SYNC_START=$(date +%s.%N)
    echo -e "${YELLOW}[REMOTE] Syncing files...${NC}"
    # WARNING: --delete removes files on remote not present locally
    # Use --filter='P' (protect) to preserve .venv directories on remote
    # Note: Source path is assumed to be project root. This script should be run from project root.
    rsync -az --delete --force \
        --filter='P .venv' \
        --filter='P **/.venv' \
        --filter='P .git' \
        --filter='P **/data/' \
        --filter='P frontend/node_modules' \
        --exclude '.git' \
        --exclude '__pycache__' \
        --exclude '*.pyc' \
        --exclude '.venv' \
        --exclude '**/data/' \
        --exclude '.mypy_cache' \
        --exclude '.pytest_cache' \
        --exclude '.ruff_cache' \
        --exclude '.coverage' \
        --exclude 'htmlcov' \
        --exclude '.hypothesis' \
        --exclude 'frontend/node_modules' \
        --exclude 'frontend/.react-router' \
        --exclude 'frontend/build' \
        --exclude 'frontend/dist' \
        --exclude '.DS_Store' \
        ./ "${REMOTE_HOST}:~/opendata/"
    SYNC_END=$(date +%s.%N)
    SYNC_TIME=$(echo "$SYNC_END - $SYNC_START" | bc)
    echo -e "${GREEN}[REMOTE] Sync complete (${SYNC_TIME}s)${NC}"
fi

# 3. Run tests
echo -e "${YELLOW}Running tests...${NC}"

# Temp files for logs
OD_LOG=$(mktemp)
BE_LOG=$(mktemp)
# Trap is already set above to clean these up

run_opendata() {
    echo -e "${YELLOW}=== OpenData Tests ===${NC}"
    if [ -n "$USE_LOCAL" ]; then
        echo -e "${YELLOW}[LOCAL] Executing...${NC}"
        # Local execution - sync first, then run tests
        SYNC_START=$(date +%s.%N)
        (cd opendata && uv sync --quiet)
        SYNC_END=$(date +%s.%N)
        SYNC_TIME=$(echo "$SYNC_END - $SYNC_START" | bc)
        echo -e "${GREEN}[uv sync: ${SYNC_TIME}s]${NC}"

        TEST_START=$(date +%s.%N)
        if [ -n "$1" ]; then
             (cd opendata && uv run --no-sync pytest "$1") 2>&1 | tee "$OD_LOG"
        else
             (cd opendata && uv run --no-sync pytest -n auto -q --tb=short) 2>&1 | tee "$OD_LOG"
        fi
    else
        echo -e "${YELLOW}[REMOTE] Executing...${NC}"
        # Remote execution - sync first, then run tests
        SYNC_START=$(date +%s.%N)
        ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/opendata && uv sync --quiet"
        SYNC_END=$(date +%s.%N)
        SYNC_TIME=$(echo "$SYNC_END - $SYNC_START" | bc)
        echo -e "${GREEN}[uv sync: ${SYNC_TIME}s]${NC}"

        TEST_START=$(date +%s.%N)
        if [ -n "$1" ]; then
             ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/opendata && uv run --no-sync pytest $1" 2>&1 | tee "$OD_LOG"
        else
             ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/opendata && uv run --no-sync pytest -n auto -q --tb=short" 2>&1 | tee "$OD_LOG"
        fi
    fi
    local exit_code=${PIPESTATUS[0]}
    TEST_END=$(date +%s.%N)
    TEST_TIME=$(echo "$TEST_END - $TEST_START" | bc)
    echo -e "${GREEN}[pytest: ${TEST_TIME}s]${NC}"
    return $exit_code
}

run_backend() {
    echo ""
    echo -e "${YELLOW}=== Backend Tests ===${NC}"
    if [ -n "$USE_LOCAL" ]; then
        echo -e "${YELLOW}[LOCAL] Executing...${NC}"
        # Local execution - sync first, then run tests
        SYNC_START=$(date +%s.%N)
        (cd backend && uv sync --quiet)
        SYNC_END=$(date +%s.%N)
        SYNC_TIME=$(echo "$SYNC_END - $SYNC_START" | bc)
        echo -e "${GREEN}[uv sync: ${SYNC_TIME}s]${NC}"

        TEST_START=$(date +%s.%N)
        if [ -n "$1" ]; then
             (cd backend && uv run --no-sync pytest "$1") 2>&1 | tee "$BE_LOG"
        else
             (cd backend && uv run --no-sync pytest -n auto -q --tb=short) 2>&1 | tee "$BE_LOG"
        fi
    else
        echo -e "${YELLOW}[REMOTE] Executing...${NC}"
        # Remote execution - sync first, then run tests
        SYNC_START=$(date +%s.%N)
        ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/backend && uv sync --quiet"
        SYNC_END=$(date +%s.%N)
        SYNC_TIME=$(echo "$SYNC_END - $SYNC_START" | bc)
        echo -e "${GREEN}[uv sync: ${SYNC_TIME}s]${NC}"

        TEST_START=$(date +%s.%N)
        if [ -n "$1" ]; then
             ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/backend && uv run --no-sync pytest $1" 2>&1 | tee "$BE_LOG"
        else
             ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/backend && uv run --no-sync pytest -n auto -q --tb=short" 2>&1 | tee "$BE_LOG"
        fi
    fi
    local exit_code=${PIPESTATUS[0]}
    TEST_END=$(date +%s.%N)
    TEST_TIME=$(echo "$TEST_END - $TEST_START" | bc)
    echo -e "${GREEN}[pytest: ${TEST_TIME}s]${NC}"
    return $exit_code
}

if [ -z "$ARGS" ]; then
    # Default: Run ALL tests sequentially, but keep going on failure
    OD_EXIT=0
    run_opendata "" || OD_EXIT=$?

    BE_EXIT=0
    run_backend "" || BE_EXIT=$?

    # Consolidated Report
    if [ $OD_EXIT -ne 0 ] || [ $BE_EXIT -ne 0 ]; then
        print_consolidated_report "$OD_EXIT" "$BE_EXIT" "$OD_LOG" "$BE_LOG"
        exit 1
    else
        echo ""
        echo -e "${GREEN}${BOLD}✓ ALL TESTS PASSED${NC}"
        exit 0
    fi

elif [ "$ARGS" == "opendata" ]; then
    # OpenData tests only (ALL)
    OD_EXIT=0
    run_opendata "" || OD_EXIT=$?
    print_single_suite_report "OpenData" "$OD_LOG" "opendata/" "$OD_EXIT"
    exit $OD_EXIT

elif [ "$ARGS" == "backend" ]; then
    # Backend tests only (ALL)
    BE_EXIT=0
    run_backend "" || BE_EXIT=$?
    print_single_suite_report "Backend" "$BE_LOG" "backend/" "$BE_EXIT"
    exit $BE_EXIT

elif echo "$ARGS" | grep -q "^backend/"; then
    # Backend tests only
    BACKEND_ARGS=$(echo "$ARGS" | sed 's|^backend/||')

    if [ -n "$USE_LOCAL" ]; then
        echo -e "${YELLOW}[LOCAL] Backend subset${NC}"
        SYNC_START=$(date +%s.%N)
        (cd backend && uv sync --quiet)
        SYNC_END=$(date +%s.%N)
        SYNC_TIME=$(echo "$SYNC_END - $SYNC_START" | bc)
        echo -e "${GREEN}[uv sync: ${SYNC_TIME}s]${NC}"

        TEST_START=$(date +%s.%N)
        if echo "$BACKEND_ARGS" | grep -qE "\.py|tests/"; then
            (cd backend && uv run --no-sync pytest $BACKEND_ARGS)
        else
            (cd backend && uv run --no-sync pytest -n auto $BACKEND_ARGS)
        fi
        TEST_END=$(date +%s.%N)
        TEST_TIME=$(echo "$TEST_END - $TEST_START" | bc)
        echo -e "${GREEN}[pytest: ${TEST_TIME}s]${NC}"
    else
        echo -e "${YELLOW}[REMOTE] Backend subset${NC}"
        SYNC_START=$(date +%s.%N)
        ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/backend && uv sync --quiet"
        SYNC_END=$(date +%s.%N)
        SYNC_TIME=$(echo "$SYNC_END - $SYNC_START" | bc)
        echo -e "${GREEN}[uv sync: ${SYNC_TIME}s]${NC}"

        TEST_START=$(date +%s.%N)
        if echo "$BACKEND_ARGS" | grep -qE "\.py|tests/"; then
            ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/backend && uv run --no-sync pytest $BACKEND_ARGS"
        else
            ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/backend && uv run --no-sync pytest -n auto $BACKEND_ARGS"
        fi
        TEST_END=$(date +%s.%N)
        TEST_TIME=$(echo "$TEST_END - $TEST_START" | bc)
        echo -e "${GREEN}[pytest: ${TEST_TIME}s]${NC}"
    fi

else
    # OpenData tests only
    if [ -n "$USE_LOCAL" ]; then
        echo -e "${YELLOW}[LOCAL] OpenData subset${NC}"
        SYNC_START=$(date +%s.%N)
        (cd opendata && uv sync --quiet)
        SYNC_END=$(date +%s.%N)
        SYNC_TIME=$(echo "$SYNC_END - $SYNC_START" | bc)
        echo -e "${GREEN}[uv sync: ${SYNC_TIME}s]${NC}"

        TEST_START=$(date +%s.%N)
        if echo "$ARGS" | grep -qE "\.py|tests/"; then
            (cd opendata && uv run --no-sync pytest $ARGS)
        else
            (cd opendata && uv run --no-sync pytest -n auto $ARGS)
        fi
        TEST_END=$(date +%s.%N)
        TEST_TIME=$(echo "$TEST_END - $TEST_START" | bc)
        echo -e "${GREEN}[pytest: ${TEST_TIME}s]${NC}"
    else
        echo -e "${YELLOW}[REMOTE] OpenData subset${NC}"
        SYNC_START=$(date +%s.%N)
        ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/opendata && uv sync --quiet"
        SYNC_END=$(date +%s.%N)
        SYNC_TIME=$(echo "$SYNC_END - $SYNC_START" | bc)
        echo -e "${GREEN}[uv sync: ${SYNC_TIME}s]${NC}"

        TEST_START=$(date +%s.%N)
        if echo "$ARGS" | grep -qE "\.py|tests/"; then
            ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/opendata && uv run --no-sync pytest $ARGS"
        else
            ssh "$REMOTE_HOST" "source ~/.local/bin/env && cd ~/opendata/opendata && uv run --no-sync pytest -n auto $ARGS"
        fi
        TEST_END=$(date +%s.%N)
        TEST_TIME=$(echo "$TEST_END - $TEST_START" | bc)
        echo -e "${GREEN}[pytest: ${TEST_TIME}s]${NC}"
    fi
fi
