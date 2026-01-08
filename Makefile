.PHONY: build test lint fmt clean test-integration

build:
	go build -o rr ./cmd/rr

test:
	go test ./...

test-integration:
	RR_TEST_SSH_HOST=localhost go test ./tests/integration/... -v

lint:
	golangci-lint run

fmt:
	go fmt ./...

clean:
	rm -f rr
	rm -rf dist/
