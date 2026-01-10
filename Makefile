.PHONY: build test lint fmt clean test-integration test-integration-ssh test-integration-no-ssh completions
.PHONY: setup setup-hooks verify check ci coverage-check fmt-check install-linter

# Read golangci-lint version from file (shared with CI)
GOLANGCI_LINT_VERSION := $(shell cat .golangci-version 2>/dev/null || echo "2.8.0")

# Build
build:
	go build -o rr ./cmd/rr

# Testing
test:
	go test ./...

test-integration:
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

# Linting and formatting
lint: install-linter
	golangci-lint run

lint-fix: install-linter
	golangci-lint run --fix

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

# Full verification (run before commits/PRs)
verify: lint test
	@echo "All checks passed"

check: verify

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
