.PHONY: build test fmt vet lint security check clean

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

# Remove build artifacts
clean:
	rm -f ./bin/rmp
