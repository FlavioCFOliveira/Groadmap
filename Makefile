.PHONY: build test fmt vet lint check clean

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

# Run all validation gates (matches CLAUDE.md requirements)
check: fmt vet test build lint

# Remove build artifacts
clean:
	rm -f ./bin/rmp
