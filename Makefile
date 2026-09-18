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

# -----------------------------------------------------------------------------
# Static-analysis tools (SPEC/BUILD.md § Static Analysis, § Local Tool Resolution)
# -----------------------------------------------------------------------------
#
# The lint and security gates never run their tool by its bare name, so a
# different copy that PATH finds first -- a snap in /snap/bin, say -- plays no
# part. Each gate runs the binary its variable names, and only after checking
# that binary's version against the pin below.
#
# These two variables are the authoritative tool pins SPEC/BUILD.md § Static
# Analysis names; the specification does not restate their values. Each is
# assigned exactly once, here. .github/workflows/ci.yml and
# .github/workflows/release.yml hold copies -- the golangci-lint action's
# `version` input and the version in the gosec install command -- and every copy
# MUST equal the value below: TestWorkflowToolPinsMatchMakefile in cmd/rmp fails
# the test gate when one differs, and TestMakefileAssignsEachToolPinOnce fails it
# when a variable is not assigned exactly once. Raising a pin therefore updates
# this file and both workflows in the same commit. `override` keeps a value given
# on the make command line or in the environment from replacing a pin, because
# nothing may disable the check.
#
# install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
# install: go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
# The linter's module path needs the /v2 suffix -- a v1 binary cannot read
# .golangci.yml (version: "2").
override GOLANGCI_LINT_VERSION := v2.13.1
override GOSEC_VERSION := v2.28.0

# The directory `go install` writes executables to: `go env GOBIN` when it is
# not empty, otherwise the bin directory of the FIRST entry of `go env GOPATH`,
# the only entry `go install` writes to. Empty when go reports neither.
GO_INSTALL_DIR := $(shell dir=$$(go env GOBIN 2>/dev/null); \
	if [ -z "$$dir" ]; then \
		gopath=$$(go env GOPATH 2>/dev/null); gopath=$${gopath%%:*}; \
		if [ -n "$$gopath" ]; then dir="$$gopath/bin"; fi; \
	fi; \
	printf '%s' "$$dir")

# The binary each gate runs. A caller names another one on the make command
# line (make lint GOLANGCI_LINT=<path>) or in the environment, and the version
# check applies to it all the same; an empty value names no binary.
GOLANGCI_LINT ?= $(if $(GO_INSTALL_DIR),$(GO_INSTALL_DIR)/golangci-lint)
GOSEC ?= $(if $(GO_INSTALL_DIR),$(GO_INSTALL_DIR)/gosec)

# shell_quote renders a make value as one single-quoted shell word.
shell_quote = '$(subst ','\'',$(1))'

# tool_path renders a tool's path as the shell word that runs exactly that file:
# a relative path gains a leading ./, so the shell neither searches PATH for it
# nor reads it as an option.
tool_path = $(call shell_quote,$(if $(patsubst /%,,$(firstword $(1))),./$(1),$(1)))

# How each gate reads its binary's version, from the file named by $$bin. The
# linter reports its own; gosec prints "dev" for a go install build, so its
# version is the version field of the `mod` line of its Go build information.
read_golangci_lint_version = "$$bin" version --short 2>/dev/null | awk 'NR == 1 { print $$1; exit }'
read_gosec_version = go version -m "$$bin" 2>/dev/null | awk '$$1 == "mod" { print $$3; exit }'

# check_tool_version fails the gate, with the one line SPEC/BUILD.md § Local Tool
# Resolution publishes, unless the binary's version matches the pin.
#   $(1) tool name   $(2) variable name   $(3) pin   $(4) name of the reader
# A version is readable when, after one optional leading v, it begins with a
# decimal digit; two readable versions match when they are equal after one
# leading v is removed from each.
define check_tool_version
bin=$(call tool_path,$($(2))); path=$(call shell_quote,$($(2))); pin='$(3)'; found=''; \
if [ -f "$$bin" ] && [ -x "$$bin" ]; then found=$$($($(4))); fi; \
case "$${found#v}" in \
	[[:digit:]]*) \
		if [ "$${found#v}" != "$${pin#v}" ]; then \
			case "$$found" in v*) ;; *) found="v$$found" ;; esac; \
			printf '%s\n' "$(1) at $$path is version $$found, but the Makefile pins $$pin. Install $(1) $$pin, or name a binary of it with $(2)=<path>." >&2; \
			exit 1; \
		fi ;; \
	*) \
		printf '%s\n' "$(1) at $$path has no readable version, but the Makefile pins $$pin. Install $(1) $$pin, or name a binary of it with $(2)=<path>." >&2; \
		exit 1 ;; \
esac
endef

# Run golangci-lint through the resolved, version-checked binary.
lint:
	@$(call check_tool_version,golangci-lint,GOLANGCI_LINT,$(GOLANGCI_LINT_VERSION),read_golangci_lint_version)
	$(call tool_path,$(GOLANGCI_LINT)) run ./...

# Run the gosec security scan through the resolved, version-checked binary.
security:
	@$(call check_tool_version,gosec,GOSEC,$(GOSEC_VERSION),read_gosec_version)
	$(call tool_path,$(GOSEC)) -exclude-dir=.claude/worktrees ./...

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
