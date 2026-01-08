.PHONY: build test lint fmt clean test-integration test-integration-ssh test-integration-no-ssh completions
.PHONY: setup setup-hooks verify check

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
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Linting and formatting
lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

fmt:
	go fmt ./...
	goimports -w .

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
setup: setup-hooks
	@command -v golangci-lint >/dev/null 2>&1 || { echo "Installing golangci-lint..."; go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; }
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
