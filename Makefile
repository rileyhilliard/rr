.PHONY: build test lint fmt clean test-integration test-integration-ssh test-integration-no-ssh

build:
	go build -o rr ./cmd/rr

test:
	go test ./...

test-integration:
	go test ./tests/integration/... -v

test-integration-ssh:
	RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v

test-integration-no-ssh:
	RR_TEST_SKIP_SSH=1 go test ./tests/integration/... -v

lint:
	golangci-lint run

fmt:
	go fmt ./...

clean:
	rm -f rr
	rm -rf dist/
