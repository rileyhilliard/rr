.PHONY: build test lint fmt clean test-integration test-integration-ssh test-integration-no-ssh completions
.PHONY: setup setup-hooks verify check ci coverage-check coverage-merged coverage-ci fmt-check install-linter demos demos-mock
.PHONY: test-local lint-local verify-local test-all verify-all

# Read golangci-lint version from file (shared with CI)
GOLANGCI_LINT_VERSION := $(shell cat .golangci-version 2>/dev/null || echo "2.8.0")

# =============================================================================
# Primary targets (use rr for remote execution)
# =============================================================================

# Build (always local - no need to sync just to compile)
build:
	go build -o rr ./cmd/rr

# Testing via rr (syncs to remote, runs there)
test:
	@if command -v rr >/dev/null 2>&1; then \
		rr test; \
	else \
		echo "rr not found, running locally..."; \
		go test ./...; \
	fi

test-integration:
	@if command -v rr >/dev/null 2>&1; then \
		rr test-integration; \
	else \
		echo "rr not found, running locally..."; \
		go test ./tests/integration/... -v; \
	fi

# Run unit and integration tests in parallel across hosts
test-all:
	@if command -v rr >/dev/null 2>&1; then \
		rr test-all; \
	else \
		echo "rr not found, running sequentially locally..."; \
		go test ./... && go test ./tests/integration/... -v; \
	fi

# Linting (always local - no need for remote execution)
lint: lint-local

# Full verification in parallel (lint + unit + integration)
verify-all:
	@if command -v rr >/dev/null 2>&1; then \
		rr verify-all; \
	else \
		echo "rr not found, running sequentially locally..."; \
		$(MAKE) lint-local && go test ./... && go test ./tests/integration/... -v; \
	fi

# Quick verify (lint + unit tests)
verify:
	@if command -v rr >/dev/null 2>&1; then \
		rr verify; \
	else \
		echo "rr not found, running locally..."; \
		$(MAKE) lint-local && go test ./...; \
	fi

check: verify

# =============================================================================
# Local-only targets (run directly without rr)
# =============================================================================

test-local:
	go test ./...

test-integration-local:
	go test ./tests/integration/... -v

test-integration-ssh:
	RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v

test-integration-no-ssh:
	RR_TEST_SKIP_SSH=1 go test ./tests/integration/... -v

test-coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

coverage-check:
	@go test -race -coverprofile=coverage.out -covermode=atomic ./... > /dev/null 2>&1
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Total coverage: $$COVERAGE%"; \
	if [ $$(echo "$$COVERAGE < 50" | bc -l) -eq 1 ]; then \
		echo "Coverage $$COVERAGE% is below 50% minimum"; \
		exit 1; \
	fi

# Coverage with merged unit + integration tests (mirrors CI behavior)
# Integration tests skip if SSH env vars aren't set - use coverage-ci for full coverage
coverage-merged:
	@echo "Running unit tests with coverage..."
	@go test -race -coverprofile=coverage-unit.out -covermode=atomic ./... > /dev/null 2>&1
	@echo "Running integration tests with coverage (tracking all packages)..."
	@go test -race -coverprofile=coverage-integration.out -covermode=atomic -coverpkg=./... ./tests/integration/... ./pkg/sshutil/... > /dev/null 2>&1 || true
	@echo "Merging coverage reports..."
	@if [ ! -f coverage-integration.out ]; then echo "mode: atomic" > coverage-integration.out; fi
	@go run github.com/wadey/gocovmerge@latest coverage-unit.out coverage-integration.out > coverage-merged.out
	@COVERAGE=$$(go tool cover -func=coverage-merged.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Total merged coverage: $$COVERAGE%"; \
	if [ $$(echo "$$COVERAGE < 60" | bc -l) -eq 1 ]; then \
		echo "Coverage $$COVERAGE% is below 60% minimum"; \
		exit 1; \
	fi
	@rm -f coverage-unit.out coverage-integration.out

# Full CI-like coverage with Docker SSH server (replicates CI exactly)
coverage-ci:
	@echo "Starting Docker SSH server..."
	@./scripts/ci-ssh-server.sh start
	@echo ""
	@set -e; trap './scripts/ci-ssh-server.sh stop' EXIT; \
	echo "Running unit tests with coverage..."; \
	go test -race -coverprofile=coverage-unit.out -covermode=atomic ./... > /dev/null 2>&1; \
	echo "Running integration tests with coverage (with SSH)..."; \
	RR_TEST_SSH_HOST=localhost:2222 \
	RR_TEST_SSH_KEY=$${TMPDIR:-/tmp}/rr-ci-ssh-keys/id_ed25519 \
	RR_TEST_SSH_USER=testuser \
	go test -race -coverprofile=coverage-integration.out -covermode=atomic -coverpkg=./... ./tests/integration/... ./pkg/sshutil/... > /dev/null 2>&1; \
	echo "Merging coverage reports..."; \
	if [ ! -f coverage-integration.out ]; then echo "mode: atomic" > coverage-integration.out; fi; \
	go run github.com/wadey/gocovmerge@latest coverage-unit.out coverage-integration.out > coverage-merged.out; \
	COVERAGE=$$(go tool cover -func=coverage-merged.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Total merged coverage: $$COVERAGE%"; \
	if [ $$(echo "$$COVERAGE < 60" | bc -l) -eq 1 ]; then \
		echo "Coverage $$COVERAGE% is below 60% minimum"; \
		exit 1; \
	fi; \
	rm -f coverage-unit.out coverage-integration.out

lint-local: install-linter
	golangci-lint run

lint-fix: install-linter
	golangci-lint run --fix

verify-local: lint-local test-local
	@echo "All local checks passed"

# Install golangci-lint at the pinned version (from .golangci-version)
install-linter:
	@CURRENT=$$(golangci-lint version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$CURRENT" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "Installing golangci-lint v$(GOLANGCI_LINT_VERSION) (current: $${CURRENT:-none})..."; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION); \
	fi

fmt:
	go fmt ./...
	goimports -w .

fmt-check:
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "Files not formatted:"; \
		gofmt -l .; \
		exit 1; \
	fi

# CI - run the full CI suite locally
ci: fmt-check lint coverage-check build
	@echo "All CI checks passed"

# Git hooks (lefthook with fallback warning)
setup-hooks:
	@# Configure git to use our hooks directory (provides fallback warnings)
	@git config core.hooksPath scripts/git-hooks
	@# Install lefthook if available, otherwise just use fallback hooks
	@if command -v lefthook >/dev/null 2>&1; then \
		lefthook install; \
		echo "Lefthook git hooks installed"; \
	else \
		echo "Lefthook not found - fallback hooks installed (will warn on commit)"; \
		echo "Run 'brew install lefthook && lefthook install' for full hook support"; \
	fi

hooks-run:
	@if command -v lefthook >/dev/null 2>&1; then \
		lefthook run pre-commit; \
	else \
		echo "Lefthook not installed. Run: brew install lefthook && lefthook install"; \
	fi

# Development setup (run once after cloning)
setup: setup-hooks install-linter
	@command -v goimports >/dev/null 2>&1 || { echo "Installing goimports..."; go install golang.org/x/tools/cmd/goimports@latest; }
	go mod download
	@echo "Development environment ready"

# Cleanup
clean:
	rm -f rr
	rm -rf dist/
	rm -f coverage.out coverage.html

# Shell completions
completions:
	@./scripts/generate-completions.sh

# VHS demo recordings (uses real rr - requires working SSH setup)
demos:
	@command -v vhs >/dev/null 2>&1 || { echo "VHS not found. Install: brew install charmbracelet/tap/vhs"; exit 1; }
	@echo "Recording demo tapes (using real rr)..."
	@for tape in tapes/*.tape; do \
		echo "Recording $$tape..."; \
		vhs "$$tape"; \
	done
	@echo "Demo recordings complete. GIFs saved to tapes/"

# VHS demo recordings with mock (deterministic output, no SSH needed)
# Note: demo-monitor.tape requires real rr (TUI too complex to mock)
demos-mock:
	@command -v vhs >/dev/null 2>&1 || { echo "VHS not found. Install: brew install charmbracelet/tap/vhs"; exit 1; }
	@echo "Recording demo tapes (using mock)..."
	@for tape in tapes/*.tape; do \
		case "$$tape" in \
			tapes/demo-monitor.tape|tapes/demo-init.tape) \
				echo "Skipping $$tape (requires real rr)..."; \
				continue ;; \
		esac; \
		echo "Recording $$tape..."; \
		PATH="$(CURDIR)/tapes/mock:$$PATH" vhs "$$tape"; \
	done
	@echo "Demo recordings complete. GIFs saved to tapes/"
	@echo "Note: Run 'make demos' with real rr for demo-monitor.tape and demo-init.tape"
