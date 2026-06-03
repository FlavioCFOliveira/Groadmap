.PHONY: build test fmt vet lint security check clean cover cover-full

# Build the binary for the current platform
build:
	go build -o ./bin/rmp ./cmd/rmp

# Run all unit tests
test:
	go test ./...

# Format source code
fmt:
	go fmt ./...

# Run static analysis
vet:
	go vet ./...

# Run golangci-lint (install: brew install golangci-lint)
lint:
	golangci-lint run ./...

# Run gosec security scan (install: go install github.com/securego/gosec/v2/cmd/gosec@latest)
security:
	gosec -exclude-dir=.claude/worktrees ./...

# Run all validation gates (matches CLAUDE.md requirements)
check: fmt vet test build lint security

# Unit-test statement coverage (fast). Writes coverage.out and prints the total.
cover:
	go test -coverpkg=./... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1
	@echo "HTML report: go tool cover -html=coverage.out"

# Full measured coverage of the command surface: an instrumented binary driven by
# the E2E suite, merged with unit-test coverage. Prints per-package and total
# statement coverage so '100% of operations' claims are measured, not asserted.
cover-full:
	@rm -rf ./coverage && mkdir -p ./coverage/e2e ./coverage/unit
	go build -cover -coverpkg=./... -o ./bin/rmp ./cmd/rmp
	GOCOVERDIR=$(CURDIR)/coverage/e2e python3 tests/run_tests.py
	go test -cover -coverpkg=./... ./... -args -test.gocoverdir=$(CURDIR)/coverage/unit
	@echo "=== Merged coverage (E2E + unit), per package ==="
	go tool covdata percent -i=./coverage/e2e,./coverage/unit
	@echo "=== Merged total ==="
	go tool covdata func -i=./coverage/e2e,./coverage/unit | tail -1
	go build -o ./bin/rmp ./cmd/rmp

# Remove build artifacts
clean:
	rm -f ./bin/rmp coverage.out
	rm -rf ./coverage
