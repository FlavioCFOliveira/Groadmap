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
# The lint and security gates never run their tool by its bare name. Each
# resolves its tool to one binary -- the copy `go install` wrote when there is
# one, and only otherwise the first copy PATH finds -- and runs that binary by
# its path, only after checking its version against the pin below. A copy of
# another release that PATH finds first, a snap in /snap/bin say, therefore
# cannot pass a gate: it is not run when the go install copy exists, and it
# fails the version check when it does not.
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
override GOLANGCI_LINT_VERSION := v2.14.0
override GOSEC_VERSION := v2.29.0

# The install command of each tool's own section of SPEC/BUILD.md, with the pin
# in place of its placeholder. A failed version check prints it. The linter's
# module path needs the /v2 suffix -- a v1 binary cannot read .golangci.yml
# (version: "2").
golangci_lint_install = go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
gosec_install = go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)

# shell_quote renders a make value as one single-quoted shell word.
shell_quote = '$(subst ','\'',$(1))'

# tool_path renders a tool's path as the shell word that runs exactly that file:
# a relative path gains a leading ./, so the shell neither searches PATH for it
# nor reads it as an option.
tool_path = $(call shell_quote,$(if $(patsubst /%,,$(firstword $(1))),./$(1),$(1)))

# for_each_path_copy is shell code that runs the command $(2) once for every
# copy of the tool $(1) on PATH, in PATH order, with $$copy holding its path:
# every executable file of that name in a directory of PATH, the list
# `which -a` prints. An empty PATH entry names the current directory, as it
# does for the shell and for `which`.
for_each_path_copy = ( set -f; IFS=:; for dir in $$PATH; do \
	[ -n "$$dir" ] || dir=.; copy="$$dir/$(1)"; \
	if [ -f "$$copy" ] && [ -x "$$copy" ]; then $(2); fi; \
done )

# The directory `go install` writes executables to: `go env GOBIN` when it is
# not empty, otherwise the bin directory of the FIRST entry of `go env GOPATH`,
# the only entry `go install` writes to. Empty when go reports neither.
GO_INSTALL_DIR := $(shell dir=$$(go env GOBIN 2>/dev/null); \
	if [ -z "$$dir" ]; then \
		gopath=$$(go env GOPATH 2>/dev/null); gopath=$${gopath%%:*}; \
		if [ -n "$$gopath" ]; then dir="$$gopath/bin"; fi; \
	fi; \
	printf '%s' "$$dir")

# resolve_tool is the default binary of the tool $(1): the copy in
# GO_INSTALL_DIR when that path names an existing file, whether or not it can be
# run; otherwise the first copy on PATH; otherwise the path the install command
# would write, so the version check reports that nothing is installed. That
# path, and so the result, is empty when GO_INSTALL_DIR is.
resolve_tool = $(shell dir=$(call shell_quote,$(GO_INSTALL_DIR)); \
	if [ -n "$$dir" ] && [ -e "$$dir/$(1)" ]; then printf '%s' "$$dir/$(1)"; exit 0; fi; \
	first=$$( $(call for_each_path_copy,$(1),printf '%s' "$$copy"; exit 0)); \
	if [ -n "$$first" ]; then printf '%s' "$$first"; exit 0; fi; \
	if [ -n "$$dir" ]; then printf '%s' "$$dir/$(1)"; fi)

# The binary each gate runs. A caller names another one on the make command
# line (make lint GOLANGCI_LINT=<path>) or in the environment. An override is
# never resolved: the binary it names is the one the version check reads,
# present or not, and an empty value names no binary. The resolution runs only
# when the caller set nothing, and once, so every recipe line sees one binary.
ifeq ($(origin GOLANGCI_LINT),undefined)
GOLANGCI_LINT := $(call resolve_tool,golangci-lint)
endif
ifeq ($(origin GOSEC),undefined)
GOSEC := $(call resolve_tool,gosec)
endif

# How each gate reads its binary's version, from the file named by $$bin. The
# linter reports its own; gosec prints "dev" for a go install build, so its
# version is the version field of the `mod` line of its Go build information.
read_golangci_lint_version = "$$bin" version --short 2>/dev/null | awk 'NR == 1 { print $$1; exit }'
read_gosec_version = go version -m "$$bin" 2>/dev/null | awk '$$1 == "mod" { print $$3; exit }'

# check_tool_version fails the gate, with the three-part report SPEC/BUILD.md
# § Local Tool Resolution publishes, unless the binary's version matches the pin.
#   $(1) tool name   $(2) variable name   $(3) pin   $(4) name of the reader
#   $(5) install command
# A version is readable when, after one optional leading v, it begins with a
# decimal digit; two readable versions match when they are equal after one
# leading v is removed from each. The report is the line that states the
# failure, every copy of the tool on PATH, and the command that installs the
# pinned version.
define check_tool_version
bin=$(call tool_path,$($(2))); path=$(call shell_quote,$($(2))); pin='$(3)'; found=''; \
if [ -f "$$bin" ] && [ -x "$$bin" ]; then found=$$($($(4))); fi; \
case "$${found#v}" in \
	[[:digit:]]*) \
		if [ "$${found#v}" = "$${pin#v}" ]; then exit 0; fi; \
		case "$$found" in v*) ;; *) found="v$$found" ;; esac; \
		line="$(1) at $$path is version $$found, but the Makefile pins $$pin." ;; \
	*) \
		line="$(1) at $$path has no readable version, but the Makefile pins $$pin." ;; \
esac; \
{ \
	printf '%s\n' "$$line Install $(1) $$pin, or name a binary of it with $(2)=<path>."; \
	copies=$$( $(call for_each_path_copy,$(1),printf '  %s\n' "$$copy")); \
	if [ -n "$$copies" ]; then \
		printf '%s\n%s\n' "PATH holds these copies of $(1), in PATH order:" "$$copies"; \
	else \
		printf '%s\n' "PATH holds no copy of $(1)."; \
	fi; \
	printf '%s\n' "Install the pinned version with: $(5)"; \
} >&2; \
exit 1
endef

# Run golangci-lint through the resolved, version-checked binary.
lint:
	@$(call check_tool_version,golangci-lint,GOLANGCI_LINT,$(GOLANGCI_LINT_VERSION),read_golangci_lint_version,$(golangci_lint_install))
	$(call tool_path,$(GOLANGCI_LINT)) run ./...

# Run the gosec security scan through the resolved, version-checked binary.
security:
	@$(call check_tool_version,gosec,GOSEC,$(GOSEC_VERSION),read_gosec_version,$(gosec_install))
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
