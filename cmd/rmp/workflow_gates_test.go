package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The tests in this file pin the two GitHub Actions workflows, and the
// Makefile's `check` target, to SPEC/BUILD.md and to the tool pins the Makefile
// holds. They exist because the release workflow once ran only `fmt`, `vet` and
// the tests: neither the linter nor the security scan ran anywhere in the
// release pipeline, so a `v*` tag could publish binaries that no
// `golangci-lint` and no `gosec` run had ever inspected. Nothing detected the
// gap, and ten consecutive releases recorded the security gate as "skipped, per
// project policy" — a policy SPEC/BUILD.md § A Missing Tool Is a Failure, Never
// a Skip explicitly forbids and that never existed.
//
// The specified state has since been restored in both YAML files. These tests
// are the gate that keeps it there. Every expectation the specification states
// is read out of SPEC/BUILD.md rather than repeated here: the gate set and its
// commands, the order and no-skip rules, and the form of the command that
// installs gosec. The tool versions are the exception, because the
// specification does not state them. SPEC/BUILD.md § Static Analysis makes two
// Makefile variables, GOLANGCI_LINT_VERSION and GOSEC_VERSION, the
// authoritative pins and each workflow a holder of copies, so the pins are read
// out of the Makefile: a pin raised in the Makefile alone, or in one workflow
// alone, fails the suite. The pin on the golangci-lint action itself is written
// only in the two workflows, so they are held to each other. This mirrors
// TestSupportedBuildTargetsMatchSpec in build_targets_test.go, which pins the
// build matrix to the same document.

// ciWorkflowPath and releaseWorkflowPath locate the two workflow files from
// this package's directory, which is where `go test` sets the working
// directory. specBuildPath, declared in build_targets_test.go, locates the
// specification the same way.
const (
	ciWorkflowPath      = "../../.github/workflows/ci.yml"
	releaseWorkflowPath = "../../.github/workflows/release.yml"
	makefilePath        = "../../Makefile"
)

// pipeline names the jobs SPEC/BUILD.md § GitHub Actions Workflow declares for
// one workflow: the job that runs the gates, the job that is the `build` gate,
// and the job that publishes the release. The job that runs the end-to-end
// suite has the same ID in both workflows, e2eJobID. publishJob is empty for
// the CI workflow, which publishes nothing: the release workflow is the only
// workflow that publishes.
type pipeline struct {
	path       string
	gateJob    string
	buildJob   string
	publishJob string
}

// e2eJobID is the ID SPEC/BUILD.md § The End-to-End Suite Is a Required
// Pipeline Job gives the end-to-end job in each workflow.
const e2eJobID = "e2e"

// pipelines returns the two workflows under test, described in the terms
// SPEC/BUILD.md § GitHub Actions Workflow uses for each.
func pipelines() []pipeline {
	return []pipeline{
		{path: ciWorkflowPath, gateJob: "test", buildJob: "build"},
		{path: releaseWorkflowPath, gateJob: "test", buildJob: "build", publishJob: "release"},
	}
}

// rel is the repository-relative path of the workflow, for failure messages.
func (p pipeline) rel() string { return strings.TrimPrefix(p.path, "../../") }

// publishes reports whether the workflow publishes a release, and so whether
// its build job hands its artefacts on to a publishing job.
func (p pipeline) publishes() bool { return p.publishJob != "" }

// TestWorkflowsRunTheCompleteGateSet is the core regression gate: both
// workflows MUST run every gate of SPEC/BUILD.md § Validation Gates other than
// `build` in their gate job, with the linter and the security scan among them.
func TestWorkflowsRunTheCompleteGateSet(t *testing.T) {
	facts := loadSpecGates(t)

	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			gate := loadGateJob(t, p)
			gate.resolveGates(t, facts)

			// SPEC/BUILD.md § Permitted Differences Between the Three
			// Pipelines, difference 1: the workflows must not silently
			// reformat the tree inside the runner, so `go fmt ./...` is
			// followed by `git diff --exit-code`. The two commands need not
			// share a step, but the order matters.
			formatAt := gate.find(func(cmd string) bool { return cmd == facts.commands[gateFmt] })
			diffAt := gate.find(func(cmd string) bool {
				return strings.HasPrefix(cmd, "git diff") && strings.Contains(cmd, "--exit-code")
			})
			switch {
			case diffAt < 0:
				t.Errorf("%s: job %q runs %q but never checks the result with `git diff --exit-code`, "+
					"so unformatted source would be rewritten inside the runner and the job would still pass. "+
					"SPEC/BUILD.md § Permitted Differences Between the Three Pipelines (difference 1) requires both.",
					p.rel(), p.gateJob, facts.commands[gateFmt])
			case formatAt >= 0 && diffAt < formatAt:
				t.Errorf("%s: job %q runs `git diff --exit-code` (%s) before %q (%s), "+
					"so the check cannot see what the formatter changed. "+
					"SPEC/BUILD.md § Permitted Differences Between the Three Pipelines (difference 1) requires "+
					"`go fmt ./...` first.",
					p.rel(), p.gateJob, gate.where(diffAt), facts.commands[gateFmt], gate.where(formatAt))
			}
		})
	}
}

// TestWorkflowToolPinsMatchMakefile proves the rule of SPEC/BUILD.md § Static
// Analysis, "Where the pins live, and how they change", for the two workflows:
// each holds a copy of both tool pins — the `version` input it passes to the
// golangci-lint action, and the version in the command that installs gosec —
// and each copy MUST equal the value the Makefile assigns to
// GOLANGCI_LINT_VERSION or GOSEC_VERSION. The expected values are read out of
// the Makefile, so raising a pin there alone fails this test, which is the
// point: a gate whose tool version differs between two pipelines is not the
// same gate in both. TestWorkflowPinCheckRejectsEveryDivergentCopy proves each
// rejection this test relies on.
func TestWorkflowToolPinsMatchMakefile(t *testing.T) {
	spec := loadSpecGates(t)
	pins := loadMakefilePins(t)

	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			gate := loadGateJob(t, p)
			for _, problem := range workflowPinProblems(p.rel(), &gate.job, pins, spec.gosecInstall) {
				t.Error(problem)
			}
		})
	}
}

// TestMakefileAssignsEachToolPinOnce holds the Makefile to what SPEC/BUILD.md §
// Static Analysis requires of the place that holds the tool pins: each pin
// variable is assigned exactly once, as `override <VAR> := <version>`, and that
// value is the one the version check of § Local Tool Resolution compares a
// resolved binary against. The specification requires the `test` gate to fail
// when either variable is assigned any other number of times. Without this, a
// second assignment could replace the pin the workflows are compared against,
// and a `?=` could let the environment disable the version check.
// TestMakefilePinReaderRejectsMalformedAssignments proves each rejection this
// test relies on.
func TestMakefileAssignsEachToolPinOnce(t *testing.T) {
	makefile := readMakefile(t)

	for _, variable := range []string{lintPinVariable, gosecPinVariable} {
		t.Run(variable, func(t *testing.T) {
			if _, err := makefilePin(makefile, variable); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestWorkflowsNameTheSameLintActionPin holds the pin on the golangci-lint
// action itself, which SPEC/BUILD.md § Static Analysis keeps apart from the
// linter's: the Makefile does not hold it, it is written only in the two
// workflows, and both MUST name the same one. The action is the code that
// installs and runs the linter, so two different action pins would make the
// lint gate differ between the pipelines even with the linter's own version
// equal. TestLintActionPinCheckRejectsDivergentWorkflows proves each rejection
// this test relies on.
func TestWorkflowsNameTheSameLintActionPin(t *testing.T) {
	sites := make([]actionPinSite, 0, 2)
	for _, p := range pipelines() {
		gate := loadGateJob(t, p)
		sites = append(sites, actionPinSite{rel: p.rel(), pins: lintActionPins(&gate.job)})
	}

	for _, problem := range lintActionPinProblems(sites) {
		t.Error(problem)
	}
}

// TestMakefilePinReaderRejectsMalformedAssignments proves that makefilePin,
// which TestMakefileAssignsEachToolPinOnce and every workflow pin comparison
// rely on, accepts the one form SPEC/BUILD.md § Static Analysis requires and
// rejects every other: no assignment, a second assignment in any form, the
// wrong operator or modifiers, a value that is not an exact version, and a pin
// no recipe reads.
func TestMakefilePinReaderRejectsMalformedAssignments(t *testing.T) {
	// reader is a rule whose recipe reads the pin, as the security gate does.
	const reader = "\nsecurity:\n\t@check-version gosec $(GOSEC_VERSION)\n"

	cases := []struct {
		name     string
		makefile string
		value    string // the pin read, when the Makefile is accepted
		reject   string // a fragment of the error, when it is rejected
	}{
		{
			name:     "one override assignment is accepted",
			makefile: "override GOSEC_VERSION := v2.21.4" + reader,
			value:    "v2.21.4",
		},
		{
			name:     "a pre-release version is accepted",
			makefile: "override GOSEC_VERSION := v2.22.0-rc.1" + reader,
			value:    "v2.22.0-rc.1",
		},
		{
			name: "a recipe line setting a shell variable of the same name is not an assignment",
			makefile: "override GOSEC_VERSION := v2.21.4\nscan:\n\tGOSEC_VERSION=v2.20.0 ./scripts/scan.sh\n" +
				reader,
			value: "v2.21.4",
		},
		{
			name:     "no assignment",
			makefile: "GOSEC ?= $(GOBIN)/gosec" + reader,
			reject:   "assigns GOSEC_VERSION 0 times",
		},
		{
			name:     "two override assignments",
			makefile: "override GOSEC_VERSION := v2.21.4\noverride GOSEC_VERSION := v2.22.0" + reader,
			reject:   "assigns GOSEC_VERSION 2 times",
		},
		{
			name:     "a second, conditional assignment",
			makefile: "override GOSEC_VERSION := v2.21.4\nGOSEC_VERSION ?= v2.22.0" + reader,
			reject:   "assigns GOSEC_VERSION 2 times",
		},
		{
			name:     "a second, appending assignment",
			makefile: "override GOSEC_VERSION := v2.21.4\nGOSEC_VERSION += -rc.1" + reader,
			reject:   "assigns GOSEC_VERSION 2 times",
		},
		{
			name:     "a second, target-specific assignment",
			makefile: "override GOSEC_VERSION := v2.21.4\nsecurity: GOSEC_VERSION := v2.22.0" + reader,
			reject:   "assigns GOSEC_VERSION 2 times",
		},
		{
			name:     "a second assignment by define",
			makefile: "override GOSEC_VERSION := v2.21.4\ndefine GOSEC_VERSION\nv2.22.0\nendef" + reader,
			reject:   "assigns GOSEC_VERSION 2 times",
		},
		{
			name:     "the only assignment is a define",
			makefile: "define GOSEC_VERSION\nv2.21.4\nendef" + reader,
			reject:   "with `define`",
		},
		{
			name:     "no override",
			makefile: "GOSEC_VERSION := v2.21.4" + reader,
			reject:   "requires the form `override GOSEC_VERSION := <version>`",
		},
		{
			name:     "a conditional assignment",
			makefile: "override GOSEC_VERSION ?= v2.21.4" + reader,
			reject:   "requires the form `override GOSEC_VERSION := <version>`",
		},
		{
			name:     "a recursive assignment",
			makefile: "override GOSEC_VERSION = v2.21.4" + reader,
			reject:   "requires the form `override GOSEC_VERSION := <version>`",
		},
		{
			name:     "an exported assignment",
			makefile: "export override GOSEC_VERSION := v2.21.4" + reader,
			reject:   "requires the form `override GOSEC_VERSION := <version>`",
		},
		{
			name:     "a trailing comment",
			makefile: "override GOSEC_VERSION := v2.21.4 # the scanner pin" + reader,
			reject:   "not an exact version",
		},
		{
			name:     "a version prefix",
			makefile: "override GOSEC_VERSION := v2" + reader,
			reject:   "not an exact version",
		},
		{
			name:     "no leading v",
			makefile: "override GOSEC_VERSION := 2.21.4" + reader,
			reject:   "not an exact version",
		},
		{
			name:     "an empty value",
			makefile: "override GOSEC_VERSION :=" + reader,
			reject:   "not an exact version",
		},
		{
			name:     "never read",
			makefile: "override GOSEC_VERSION := v2.21.4\nsecurity:\n\tgosec ./...\n",
			reject:   "never reads it",
		},
		{
			name: "read only by a comment",
			makefile: "# install: go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)\n" +
				"override GOSEC_VERSION := v2.21.4\nsecurity:\n\tgosec ./...\n",
			reject: "never reads it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := makefilePin(tc.makefile, gosecPinVariable)
			if tc.reject == "" {
				if err != nil || got != tc.value {
					t.Fatalf("makefilePin = %q, %v; want %q, nil\nMakefile:\n%s", got, err, tc.value, tc.makefile)
				}
				return
			}
			if err == nil {
				t.Fatalf("makefilePin accepted the Makefile and read %q; want an error containing %q\nMakefile:\n%s",
					got, tc.reject, tc.makefile)
			}
			if !strings.Contains(err.Error(), tc.reject) {
				t.Fatalf("makefilePin rejected the Makefile with %q; want an error containing %q", err, tc.reject)
			}
			if got != "" {
				t.Fatalf("makefilePin returned the pin %q alongside its error; a rejected pin must not be used", got)
			}
		})
	}
}

// TestWorkflowPinCheckRejectsEveryDivergentCopy proves that
// workflowPinProblems, which TestWorkflowToolPinsMatchMakefile relies on,
// passes a gate job whose copies equal the Makefile's pins and reports each way
// a copy can diverge. The gosec install form is the one the specification
// states, so the fixtures follow the specification rather than restating it.
func TestWorkflowPinCheckRejectsEveryDivergentCopy(t *testing.T) {
	form := loadSpecGates(t).gosecInstall
	pins := toolPins{lint: "v2.4.0", gosec: "v2.21.4"}
	install := form.with(pins.gosec)

	cases := []struct {
		name      string
		actionPin string   // empty: the job has no golangci-lint action step
		version   string   // the action's `version` input; empty: the input is absent
		commands  []string // the job's other commands, one step each
		problems  []string // a fragment of each problem expected, in order
	}{
		{
			name:      "copies equal to the Makefile's pins",
			actionPin: "v9.3.0",
			version:   pins.lint,
			commands:  []string{"go vet ./...", install, "gosec -exclude-dir=.claude/worktrees ./..."},
		},
		{
			name:      "a linter version other than the Makefile's",
			actionPin: "v9.3.0",
			version:   "v2.3.0",
			commands:  []string{install},
			problems:  []string{`passes version "v2.3.0" to the golangci-lint action, but the Makefile pins golangci-lint to v2.4.0`},
		},
		{
			name:      "no version input on the action",
			actionPin: "v9.3.0",
			commands:  []string{install},
			problems:  []string{`passes version "" to the golangci-lint action`},
		},
		{
			name:     "no golangci-lint action step",
			commands: []string{"golangci-lint run ./...", install},
			problems: []string{"has no step that uses the golangci-lint action"},
		},
		{
			name:      "a gosec version other than the Makefile's",
			actionPin: "v9.3.0",
			version:   pins.lint,
			commands:  []string{form.with("v2.20.0")},
			problems:  []string{"@v2.20.0\", but SPEC/BUILD.md § Security Scan: gosec requires"},
		},
		{
			name:      "a floating gosec version",
			actionPin: "v9.3.0",
			version:   pins.lint,
			commands:  []string{form.with("latest")},
			problems:  []string{"@latest\", but SPEC/BUILD.md § Security Scan: gosec requires"},
		},
		{
			name:      "gosec installed a second time at another version",
			actionPin: "v9.3.0",
			version:   pins.lint,
			commands:  []string{install, form.with("v2.22.0")},
			problems:  []string{"@v2.22.0\", but SPEC/BUILD.md § Security Scan: gosec requires"},
		},
		{
			name:      "gosec never installed",
			actionPin: "v9.3.0",
			version:   pins.lint,
			commands:  []string{"gosec -exclude-dir=.claude/worktrees ./..."},
			problems:  []string{"does not install gosec"},
		},
		{
			name:     "both copies diverge at once",
			commands: []string{"go install github.com/securego/gosec/v2/cmd/gosec@main"},
			problems: []string{"has no step that uses the golangci-lint action", "@main\", but"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job := fixtureGateJob(t, tc.actionPin, tc.version, tc.commands...)
			got := workflowPinProblems("fixture.yml", &job, pins, form)
			assertProblems(t, got, tc.problems)
		})
	}
}

// TestLintActionPinCheckRejectsDivergentWorkflows proves that
// lintActionPinProblems, which TestWorkflowsNameTheSameLintActionPin relies
// on, passes two workflows that name the same exact action pin and reports
// every other combination. The pins are read from parsed fixture jobs, so the
// path from a step's `uses` value to the comparison is exercised too.
func TestLintActionPinCheckRejectsDivergentWorkflows(t *testing.T) {
	cases := []struct {
		name     string
		ci       []string // the action pin of each lint step in the CI fixture
		release  []string // the same, in the release fixture
		problems []string
	}{
		{
			name:    "the same exact pin in both",
			ci:      []string{"v9.3.0"},
			release: []string{"v9.3.0"},
		},
		{
			name:    "different pins",
			ci:      []string{"v9.3.0"},
			release: []string{"v9.2.0"},
			problems: []string{"release.yml uses golangci/golangci-lint-action@v9.2.0, but ci.yml uses " +
				"golangci/golangci-lint-action@v9.3.0"},
		},
		{
			name:    "the same floating pin in both",
			ci:      []string{"v9"},
			release: []string{"v9"},
			problems: []string{"ci.yml: uses golangci/golangci-lint-action@v9, which is not an exact version",
				"release.yml: uses golangci/golangci-lint-action@v9, which is not an exact version"},
		},
		{
			name:     "no action in one workflow",
			ci:       []string{"v9.3.0"},
			problems: []string{"release.yml: no step uses golangci/golangci-lint-action@<version>"},
		},
		{
			name:    "two action steps with different pins in one workflow",
			ci:      []string{"v9.3.0", "v9.2.0"},
			release: []string{"v9.3.0"},
			problems: []string{"ci.yml uses golangci/golangci-lint-action@v9.2.0, but ci.yml uses " +
				"golangci/golangci-lint-action@v9.3.0"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites := []actionPinSite{
				{rel: "ci.yml", pins: fixtureActionPins(t, tc.ci)},
				{rel: "release.yml", pins: fixtureActionPins(t, tc.release)},
			}
			assertProblems(t, lintActionPinProblems(sites), tc.problems)
		})
	}
}

// fixtureActionPins parses a gate job with one golangci-lint action step per
// pin and returns the pins lintActionPins reads back from it.
func fixtureActionPins(t *testing.T, pins []string) []string {
	t.Helper()

	var src strings.Builder
	src.WriteString("jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - name: Run go vet\n        run: go vet ./...\n")
	for i, pin := range pins {
		fmt.Fprintf(&src, "      - name: Run golangci-lint %d\n        uses: %s%s\n        with:\n          version: v2.4.0\n",
			i+1, lintActionPrefix, pin)
	}
	job := mustJob(t, parseWorkflowSource(src.String(), "fixture.yml"), "test")
	return lintActionPins(&job)
}

// fixtureGateJob parses a gate job that runs the golangci-lint action at
// actionPin, passing version as its `version` input, and then one step per
// command. An empty actionPin leaves the action step out, and an empty version
// leaves the input out.
func fixtureGateJob(t *testing.T, actionPin, version string, commands ...string) wfJob {
	t.Helper()

	var src strings.Builder
	src.WriteString("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n")
	if actionPin != "" {
		fmt.Fprintf(&src, "      - name: Run golangci-lint\n        uses: %s%s\n        with:\n", lintActionPrefix, actionPin)
		if version != "" {
			fmt.Fprintf(&src, "          version: %s\n", version)
		}
		src.WriteString("          args: --timeout=5m\n")
	}
	for i, command := range commands {
		fmt.Fprintf(&src, "      - name: Step %d\n        run: %s\n", i+1, command)
	}
	return mustJob(t, parseWorkflowSource(src.String(), "fixture.yml"), "test")
}

// assertProblems requires exactly one reported problem per expected fragment,
// each containing its fragment, in order.
func assertProblems(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d problems, want %d.\ngot:  %q\nwant: %q", len(got), len(want), got, want)
	}
	for i := range want {
		if !strings.Contains(got[i], want[i]) {
			t.Errorf("problem %d is %q; want it to contain %q", i, got[i], want[i])
		}
	}
}

// Tool pin variables: the two Makefile variables SPEC/BUILD.md § Static
// Analysis names as the authoritative pins. loadSpecGates requires the
// specification to name exactly these, so a renamed variable fails the suite
// until these tests are taught the new name.
const (
	lintPinVariable  = "GOLANGCI_LINT_VERSION"
	gosecPinVariable = "GOSEC_VERSION"
)

// lintActionPrefix is how a workflow step names the golangci-lint action; the
// rest of its `uses` value is the action pin.
const lintActionPrefix = "golangci/golangci-lint-action@"

// exactVersion matches an exact semantic version with its leading v, the only
// form SPEC/BUILD.md admits for a pin ("Both tools are pinned to an exact
// version"; "Both pins are exact"). A prefix such as `v2` is a moving target to
// `go install` and to GitHub alike, so it is refused.
var exactVersion = regexp.MustCompile(
	`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// toolPins holds the value the Makefile assigns to each tool pin variable.
type toolPins struct {
	lint  string // GOLANGCI_LINT_VERSION
	gosec string // GOSEC_VERSION
}

// readMakefile returns the repository's Makefile.
func readMakefile(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(makefilePath))
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}
	return string(raw)
}

// loadMakefilePins reads both tool pins out of the Makefile, failing the test
// when either is not held the way SPEC/BUILD.md § Static Analysis requires.
func loadMakefilePins(t *testing.T) toolPins {
	t.Helper()

	makefile := readMakefile(t)
	lint, lintErr := makefilePin(makefile, lintPinVariable)
	gosec, gosecErr := makefilePin(makefile, gosecPinVariable)
	if err := errors.Join(lintErr, gosecErr); err != nil {
		t.Fatalf("the Makefile does not hold the tool pins the workflows are compared against:\n%v", err)
	}
	return toolPins{lint: lint, gosec: gosec}
}

// makefilePin returns the value the Makefile assigns to a tool pin variable. It
// fails unless the Makefile holds the pin as SPEC/BUILD.md § Static Analysis
// requires: assigned exactly once, as `override <VAR> := <version>` with an
// exact version and nothing after it, and read as $(<VAR>) on a line that is
// not a comment.
func makefilePin(makefile, variable string) (string, error) {
	assignments := makefileAssignment(variable).FindAllStringSubmatch(makefile, -1)
	defines := len(makefileDefine(variable).FindAllStringIndex(makefile, -1))
	if count := len(assignments) + defines; count != 1 {
		return "", fmt.Errorf("the Makefile assigns %s %d times, but SPEC/BUILD.md § Static Analysis requires "+
			"exactly one assignment, `override %s := <version>`: the value the version check of § Local Tool "+
			"Resolution compares against, and the value both workflows copy", variable, count, variable)
	}
	if defines == 1 {
		return "", fmt.Errorf("the Makefile assigns %s with `define`, but SPEC/BUILD.md § Static Analysis "+
			"requires the form `override %s := <version>`", variable, variable)
	}

	match := assignments[0]
	target, modifiers, operator, value := match[1], strings.Fields(match[2]), match[3], match[4]
	if target != "" || !slices.Equal(modifiers, []string{"override"}) || operator != ":=" {
		return "", fmt.Errorf("the Makefile assigns %s as %q, but SPEC/BUILD.md § Static Analysis requires "+
			"the form `override %s := <version>`: `override` keeps a value given on the make command line or "+
			"in the environment from replacing the pin, and nothing may disable the version check",
			variable, strings.TrimSpace(match[0]), variable)
	}
	if !exactVersion.MatchString(value) {
		return "", fmt.Errorf("the Makefile assigns %s the value %q, which is not an exact version with its "+
			"leading v (such as v1.2.3) and nothing after it. SPEC/BUILD.md § Static Analysis pins each tool to "+
			"an exact version; a trailing comment is not allowed either, because make keeps the blanks before "+
			"the `#` in the value", variable, value)
	}
	if !makefileReads(makefile, variable) {
		return "", fmt.Errorf("the Makefile assigns %s but never reads it as $(%s) outside a comment, so no "+
			"version check compares a binary against that pin. SPEC/BUILD.md § Local Tool Resolution makes the "+
			"value of %s the one the gate's version check compares against", variable, variable, variable)
	}
	return value, nil
}

// makefileAssignment matches one assignment of a Makefile variable in any form
// GNU make accepts at the start of a line: every assignment operator (`=`,
// `:=`, `::=`, `:::=`, `?=`, `+=`, `!=`), the `override`, `export` and
// `private` modifiers, and a target-specific prefix (`lint: VAR := ...`).
// Counting every form is what makes "assigned exactly once" mean what it says,
// because a second assignment of any kind can replace the pin or let the
// environment replace it. A line that begins with a tab is a recipe line, where
// the same text is a shell assignment, and is not matched. makefileDefine
// covers the one remaining form, `define`.
//
// Groups: 1 the target prefix, 2 the modifiers, 3 the operator, 4 the value.
func makefileAssignment(variable string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^ *([^\s:#=][^:#=\n]*:[ \t]*)?((?:(?:override|export|private)[ \t]+)*)` +
		regexp.QuoteMeta(variable) + `[ \t]*(:::=|::=|:=|\?=|\+=|!=|=)[ \t]*(.*?)[ \t]*$`)
}

// makefileDefine matches a multi-line `define` of a Makefile variable.
func makefileDefine(variable string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^ *(?:(?:override|export|private)[ \t]+)*define[ \t]+` +
		regexp.QuoteMeta(variable) + `(?:[ \t]|$)`)
}

// makefileReads reports whether the Makefile reads a variable as $(VAR) on a
// line that is not a comment. A comment that spells the reference — the install
// hints above the pins do — proves nothing about what a gate compares against.
func makefileReads(makefile, variable string) bool {
	reference := "$(" + variable + ")"
	for line := range strings.Lines(makefile) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, reference) {
			return true
		}
	}
	return false
}

// workflowPinProblems returns every way one workflow's gate job departs from
// the Makefile's tool pins, as SPEC/BUILD.md § Static Analysis requires the
// workflows to copy them: each golangci-lint action step passes the Makefile's
// GOLANGCI_LINT_VERSION as its `version` input, and each command that installs
// gosec is the specification's install form with the Makefile's GOSEC_VERSION
// in place of its placeholder. A job that holds no copy of a pin — no action
// step, or no gosec install — is reported too.
func workflowPinProblems(rel string, job *wfJob, pins toolPins, form installForm) []string {
	problems := make([]string, 0, 2)

	lintSteps := 0
	for _, step := range job.steps {
		if !strings.HasPrefix(step.uses, lintActionPrefix) {
			continue
		}
		lintSteps++
		if got := step.with["version"]; got != pins.lint {
			problems = append(problems, fmt.Sprintf("%s: step %q passes version %q to the golangci-lint action, "+
				"but the Makefile pins golangci-lint to %s (%s). SPEC/BUILD.md § Static Analysis requires each "+
				"workflow's copy of the pin to equal the Makefile's value.",
				rel, step.name, got, pins.lint, lintPinVariable))
		}
	}
	if lintSteps == 0 {
		problems = append(problems, fmt.Sprintf("%s: job %q has no step that uses the golangci-lint action, so "+
			"it holds no copy of the linter pin. SPEC/BUILD.md § Static Analysis makes the action's `version` "+
			"input that copy, equal to the Makefile's %s (%s).",
			rel, job.id, lintPinVariable, pins.lint))
	}

	want := form.with(pins.gosec)
	installs := 0
	for _, cmd := range jobCommands(job) {
		if !strings.HasPrefix(cmd.text, form.prefix()) {
			continue
		}
		installs++
		if cmd.text != want {
			problems = append(problems, fmt.Sprintf("%s: step %q runs %q, but SPEC/BUILD.md § Security Scan: "+
				"gosec requires each workflow to install gosec with exactly %q: its install command with the "+
				"Makefile's %s (%s) in place of %s.",
				rel, job.steps[cmd.step].name, cmd.text, want, gosecPinVariable, pins.gosec, form.placeholder))
		}
	}
	if installs == 0 {
		problems = append(problems, fmt.Sprintf("%s: job %q does not install gosec with %q. SPEC/BUILD.md "+
			"§ Security Scan: gosec gives that command, with the Makefile's %s in place of %s, and § A Missing "+
			"Tool Is a Failure, Never a Skip requires each workflow to install the tool in the job that runs "+
			"the gate.", rel, job.id, want, gosecPinVariable, form.placeholder))
	}
	return problems
}

// actionPinSite is where one workflow names the golangci-lint action.
type actionPinSite struct {
	rel  string
	pins []string // the pin of each step that uses the action, in step order
}

// lintActionPins returns the pin of every step of a job that uses the
// golangci-lint action, in step order.
func lintActionPins(job *wfJob) []string {
	pins := make([]string, 0, 1)
	for _, step := range job.steps {
		if pin, ok := strings.CutPrefix(step.uses, lintActionPrefix); ok {
			pins = append(pins, pin)
		}
	}
	return pins
}

// lintActionPinProblems returns every way the workflows depart from SPEC/BUILD.md
// on the pin of the golangci-lint action itself. That pin is written only in
// the workflows, so each MUST name one, as an exact version (§ Linter:
// golangci-lint), and all of them MUST be the same (§ Static Analysis).
func lintActionPinProblems(sites []actionPinSite) []string {
	problems := make([]string, 0, 2)

	first, firstRel := "", ""
	for _, site := range sites {
		if len(site.pins) == 0 {
			problems = append(problems, fmt.Sprintf("%s: no step uses %s<version>, so the workflow names no "+
				"golangci-lint action pin. SPEC/BUILD.md § Static Analysis requires both workflows to name the "+
				"same one.", site.rel, lintActionPrefix))
		}
		for _, pin := range site.pins {
			if !exactVersion.MatchString(pin) {
				problems = append(problems, fmt.Sprintf("%s: uses %s%s, which is not an exact version. "+
					"SPEC/BUILD.md § Linter: golangci-lint requires the action's own pin to be exact, as the "+
					"linter's is.", site.rel, lintActionPrefix, pin))
			}
			if firstRel == "" {
				first, firstRel = pin, site.rel
				continue
			}
			if pin != first {
				problems = append(problems, fmt.Sprintf("%s uses %s%s, but %s uses %s%s. SPEC/BUILD.md "+
					"§ Static Analysis writes the action pin only in the two workflows and requires both to name "+
					"the same one.", site.rel, lintActionPrefix, pin, firstRel, lintActionPrefix, first))
			}
		}
	}
	return problems
}

// TestWorkflowGateStepsCannotBeSkipped enforces SPEC/BUILD.md § A Missing Tool
// Is a Failure, Never a Skip: no gate step may fail without failing its job,
// and no step may test whether a tool is present and continue without it.
func TestWorkflowGateStepsCannotBeSkipped(t *testing.T) {
	facts := loadSpecGates(t)

	// Each token below is a way of turning a gate into a no-op: the first four
	// test whether a tool is on PATH, the last three swallow a non-zero exit.
	// SPEC/BUILD.md § A Missing Tool Is a Failure, Never a Skip forbids both
	// shapes outright, so none of them belongs in a gate job.
	skipTokens := []string{"command -v", "which ", "hash ", "type -p", "|| true", "|| :", "set +e"}

	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			gate := loadGateJob(t, p)

			for name, index := range gate.resolveGates(t, facts) {
				if step := gate.job.steps[index]; step.continueOnError {
					t.Errorf("%s: the %s gate step %q carries `continue-on-error`, so the job passes when the "+
						"gate fails. SPEC/BUILD.md § A Missing Tool Is a Failure, Never a Skip forbids "+
						"allowing a gate step to fail without failing its job.",
						p.rel(), name, step.name)
				}
			}

			for position, cmd := range gate.commands {
				for _, token := range skipTokens {
					if !strings.Contains(cmd.text, token) {
						continue
					}
					t.Errorf("%s: job %q runs %q (%s), which contains %q. "+
						"SPEC/BUILD.md § A Missing Tool Is a Failure, Never a Skip forbids testing whether a "+
						"tool is present and continuing without it, and forbids swallowing a gate's failure: "+
						"if a tool cannot be installed, the job fails.",
						p.rel(), p.gateJob, cmd.text, gate.where(position), token)
				}
			}
		})
	}
}

// TestWorkflowJobsDependOnTheGates enforces the `needs:` chain of
// SPEC/BUILD.md § Where the Gate Set Is Enforced: nothing is built or published
// in parallel with the gates or the end-to-end job, or independently of them.
// Only the release workflow has a publishing job to hold to the chain.
func TestWorkflowJobsDependOnTheGates(t *testing.T) {
	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			wf := parseWorkflow(t, p.path)
			build := mustJob(t, wf, p.buildJob)

			if !slices.Contains(build.needs, p.gateJob) {
				t.Errorf("%s: the build job %q does not declare `needs: %s`, so it can build artefacts in "+
					"parallel with the gates (it declares needs: %v). SPEC/BUILD.md § Where the Gate Set Is "+
					"Enforced requires the build job to declare `needs:` on the gate job.",
					p.rel(), p.buildJob, p.gateJob, build.needs)
			}
			if !slices.Contains(build.needs, e2eJobID) {
				t.Errorf("%s: the build job %q does not declare `needs: %s`, so it can build artefacts from a "+
					"commit whose end-to-end suite did not pass (it declares needs: %v). SPEC/BUILD.md § Where the "+
					"Gate Set Is Enforced requires the build job to declare `needs:` on the gate job and on the "+
					"end-to-end job.",
					p.rel(), p.buildJob, e2eJobID, build.needs)
			}
			if !p.publishes() {
				return
			}
			if publish := mustJob(t, wf, p.publishJob); !slices.Contains(publish.needs, p.buildJob) {
				t.Errorf("%s: the publishing job %q does not declare `needs: %s`, so it can publish artefacts "+
					"independently of the gates (it declares needs: %v). SPEC/BUILD.md § Where the Gate Set Is "+
					"Enforced requires the publishing job to declare `needs:` on the build job.",
					p.rel(), p.publishJob, p.buildJob, publish.needs)
			}
		})
	}
}

// TestWorkflowPermissionsAreLeastPrivilege enforces SPEC/BUILD.md § GitHub
// Actions Workflow and the matching acceptance criterion: each workflow grants
// `contents: read`; exactly one job of the release workflow — `release`, the
// one that publishes — raises that to `contents: write`, and no job of the CI
// workflow raises it.
func TestWorkflowPermissionsAreLeastPrivilege(t *testing.T) {
	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			wf := parseWorkflow(t, p.path)

			if got := wf.permissions["contents"]; got != "read" {
				t.Errorf("%s: the workflow-level permission for `contents` is %q, not \"read\". "+
					"SPEC/BUILD.md § GitHub Actions Workflow requires both workflows to grant `contents: read` "+
					"at workflow level.",
					p.rel(), got)
			}
			for scope, level := range wf.permissions {
				if level == "write" {
					t.Errorf("%s: the workflow-level permissions grant `%s: write`, so every job inherits it. "+
						"SPEC/BUILD.md § GitHub Actions Workflow requires least privilege at workflow level, "+
						"with only the publishing job raising its own permission.",
						p.rel(), scope)
				}
			}

			writers := make([]string, 0, len(wf.jobOrder))
			for _, id := range wf.jobOrder {
				if wf.jobs[id].permissions["contents"] == "write" {
					writers = append(writers, id)
				}
			}
			switch {
			case !p.publishes() && len(writers) != 0:
				t.Errorf("%s: the jobs holding `contents: write` are %v, but SPEC/BUILD.md § GitHub Actions "+
					"Workflow allows none: the CI workflow publishes nothing, and no job of it raises the "+
					"workflow-level `contents: read`.",
					p.rel(), writers)
			case p.publishes() && (len(writers) != 1 || writers[0] != p.publishJob):
				t.Errorf("%s: the jobs holding `contents: write` are %v, but SPEC/BUILD.md § GitHub Actions "+
					"Workflow allows exactly one — %q, the job that publishes.",
					p.rel(), writers, p.publishJob)
			}

			for _, id := range []string{p.gateJob, e2eJobID, p.buildJob} {
				for scope, level := range mustJob(t, wf, id).permissions {
					if level == "write" {
						t.Errorf("%s: job %q grants itself `%s: write`. SPEC/BUILD.md § GitHub Actions Workflow "+
							"states that the gate job, the `e2e` job and the build job read; none of them may write.",
							p.rel(), id, scope)
					}
				}
			}
		})
	}
}

// ciJobs are the jobs SPEC/BUILD.md § CI Workflow declares for the CI workflow,
// which declares no other.
var ciJobs = []string{"test", e2eJobID, "build"}

// publishingActions are the `uses` prefixes of the actions that hand a build
// artefact out of a job or write a release: the artefact upload the release
// workflow's build job runs, and the actions that create a GitHub release or
// attach assets to one.
var publishingActions = []string{
	"actions/upload-artifact@",
	"actions/upload-release-asset@",
	"actions/create-release@",
	"softprops/action-gh-release@",
	"ncipollo/release-action@",
	"svenstaro/upload-release-action@",
}

// checksumTools are the commands that generate a checksum file.
var checksumTools = []string{"sha256sum", "sha512sum", "sha1sum", "shasum", "md5sum", "b2sum"}

// TestCIWorkflowPublishesNothing is the regression gate for the rolling `dev`
// pre-release the CI workflow used to publish. SPEC/BUILD.md § GitHub Actions
// Workflow makes the release workflow the only workflow that publishes anything,
// and the matching acceptance criterion states what reading
// .github/workflows/ci.yml must show: the jobs `test`, `e2e` and `build` and no
// other, no step that packs an archive, generates a checksum, uploads a build
// artefact, or creates or deletes a release or a tag, and no job that raises the
// workflow-level permission. TestCIPublishingCheckRejectsEveryPublishingStep
// proves each rejection this test relies on.
func TestCIWorkflowPublishesNothing(t *testing.T) {
	for _, problem := range ciPublishingProblems(parseWorkflow(t, ciWorkflowPath)) {
		t.Error(problem)
	}
}

// ciPublishingProblems describes every way a parsed CI workflow departs from
// "publishes nothing", or returns nothing when it publishes nothing. A command
// is matched on its shell words wherever they stand in it, so the check errs on
// the side of reporting: a CI step has no reason to name a packing, checksum,
// release or tag command at all.
func ciPublishingProblems(wf wfWorkflow) []string {
	rel := strings.TrimPrefix(wf.path, "../../")
	problems := make([]string, 0, 4)

	if got, want := slices.Sorted(slices.Values(wf.jobOrder)), slices.Sorted(slices.Values(ciJobs)); !slices.Equal(got, want) {
		problems = append(problems, fmt.Sprintf("%s: declares the jobs %v, but SPEC/BUILD.md § CI Workflow "+
			"declares exactly %v and no other", rel, wf.jobOrder, ciJobs))
	}

	for _, id := range wf.jobOrder {
		job := wf.jobs[id]
		for _, scope := range sortedKeys(scopesAt(job.permissions, "write")) {
			problems = append(problems, fmt.Sprintf("%s: job %q raises its permission to `%s: write`, but "+
				"SPEC/BUILD.md § CI Workflow grants `contents: read` at workflow level and no job raises it",
				rel, id, scope))
		}
		for _, step := range job.steps {
			for _, prefix := range publishingActions {
				if strings.HasPrefix(step.uses, prefix) {
					problems = append(problems, fmt.Sprintf("%s: job %q, step %q uses %s, which publishes out of "+
						"the job. SPEC/BUILD.md § GitHub Actions Workflow: the CI workflow publishes nothing — "+
						"no release, no pre-release, and no build artefact", rel, id, step.name, step.uses))
				}
			}
		}
		for _, cmd := range jobCommands(&job) {
			if what := publishingCommand(shellWords(cmd.text)); what != "" {
				problems = append(problems, fmt.Sprintf("%s: job %q, step %q runs %q, which %s. SPEC/BUILD.md § "+
					"GitHub Actions Workflow: the CI workflow publishes nothing, and no step of it packs an archive, "+
					"generates a checksum, uploads a build artefact, or creates or deletes a release or a tag",
					rel, id, job.steps[cmd.step].name, cmd.text, what))
			}
		}
	}
	return problems
}

// scopesAt returns the permission scopes granted at the given level.
func scopesAt(permissions map[string]string, level string) map[string]bool {
	scopes := make(map[string]bool, len(permissions))
	for scope, granted := range permissions {
		if granted == level {
			scopes[scope] = true
		}
	}
	return scopes
}

// publishingCommand describes what a shell command does towards publishing —
// packing an archive, generating a checksum, or writing a release or a tag — or
// returns the empty string when it does none of these.
func publishingCommand(words []string) string {
	for i, word := range words {
		next := ""
		if i+1 < len(words) {
			next = words[i+1]
		}
		switch {
		case word == "tar" && tarCreates(words[i+1:]):
			return "packs an archive"
		case word == "zip":
			return "packs an archive"
		case slices.Contains(checksumTools, word):
			return "generates a checksum"
		case word == "gh" && next == "release":
			return "creates or deletes a release"
		case word == "git" && (next == "tag" || next == "push"):
			return "creates, deletes or pushes a tag"
		}
	}
	return ""
}

// tarCreates reports whether the arguments of a `tar` command select its create
// mode: `--create`, a short-flag cluster that holds `c` (`-czf`), or the
// dash-less cluster of the traditional form (`tar czf`) as its first argument.
func tarCreates(arguments []string) bool {
	for i, argument := range arguments {
		switch {
		case argument == "--create":
			return true
		case strings.HasPrefix(argument, "--"):
		case strings.HasPrefix(argument, "-"):
			if strings.Contains(argument, "c") {
				return true
			}
		case i == 0 && strings.Contains(argument, "c") && strings.Trim(argument, "abcdfhjklmoprtuvwxzABCGJMOPSTUVWZ") == "":
			return true
		}
	}
	return false
}

// TestCIPublishingCheckRejectsEveryPublishingStep proves that
// ciPublishingProblems, which TestCIWorkflowPublishesNothing relies on, passes a
// CI workflow that publishes nothing and reports each shape the rolling `dev`
// pre-release took, and each other way of publishing out of the workflow. The
// fixtures are parsed by the same reader as the real file.
func TestCIPublishingCheckRejectsEveryPublishingStep(t *testing.T) {
	// clean is a CI workflow of the specified shape: three jobs, a build job that
	// compiles and checks the stamp, and a coverage upload that is reporting, not
	// a build artefact.
	const clean = "on: push\npermissions:\n  contents: read\njobs:\n" +
		"  test:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Run tests\n        run: go test ./...\n" +
		"      - name: Upload coverage\n        uses: codecov/codecov-action@v7.0.0\n" +
		"  e2e:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Run end-to-end tests\n        run: python3 -u tests/run_tests.py\n" +
		"  build:\n    runs-on: ubuntu-latest\n    needs: [test, e2e]\n    steps:\n" +
		"      - name: Build binary\n        run: go build -buildvcs=true -o dist/rmp ./cmd/rmp\n" +
		"      - name: Check build stamp\n        run: go version -m dist/rmp\n"

	// step appends one step to the build job of the clean workflow.
	step := func(body string) string { return clean + "      - name: Extra\n" + body }

	cases := []struct {
		name     string
		src      string
		problems []string
	}{
		{name: "the specified workflow", src: clean},
		{
			name: "a publishing job",
			src: clean + "  dev-release:\n    needs: build\n    runs-on: ubuntu-latest\n" +
				"    permissions:\n      contents: write\n    steps:\n" +
				"      - name: Create dev release\n        uses: softprops/action-gh-release@v3.0.2\n",
			problems: []string{
				"declares the jobs [test e2e build dev-release]",
				"job \"dev-release\" raises its permission to `contents: write`",
				"job \"dev-release\", step \"Create dev release\" uses softprops/action-gh-release@v3.0.2",
			},
		},
		{
			name: "a missing job",
			src: "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" +
				"      - name: Run tests\n        run: go test ./...\n",
			problems: []string{"declares the jobs [test]"},
		},
		{
			name:     "a tar archive",
			src:      step("        run: tar -czf dist/rmp-dev.tar.gz -C dist rmp LICENSE README.md\n"),
			problems: []string{"which packs an archive"},
		},
		{
			name:     "a traditional-form tar archive",
			src:      step("        run: tar czf rmp.tar.gz rmp\n"),
			problems: []string{"which packs an archive"},
		},
		{
			name:     "a zip archive",
			src:      step("        run: (cd dist && zip -q rmp.zip rmp.exe)\n"),
			problems: []string{"which packs an archive"},
		},
		{
			name:     "a checksum",
			src:      step("        run: sha256sum rmp.tar.gz > rmp.tar.gz.sha256\n"),
			problems: []string{"which generates a checksum"},
		},
		{
			name:     "an artefact upload",
			src:      step("        uses: actions/upload-artifact@v7.0.1\n"),
			problems: []string{"uses actions/upload-artifact@v7.0.1"},
		},
		{
			name:     "a release deleted from the command line",
			src:      step("        run: gh release delete dev --yes\n"),
			problems: []string{"which creates or deletes a release"},
		},
		{
			name:     "a tag deleted from the command line",
			src:      step("        run: git push --delete origin dev\n"),
			problems: []string{"which creates, deletes or pushes a tag"},
		},
		{
			name:     "a tag created from the command line",
			src:      step("        run: git tag -f dev\n"),
			problems: []string{"which creates, deletes or pushes a tag"},
		},
		{
			name: "a raised permission on a job that publishes nothing",
			src: strings.Replace(clean, "    needs: [test, e2e]\n",
				"    needs: [test, e2e]\n    permissions:\n      id-token: write\n", 1),
			problems: []string{"job \"build\" raises its permission to `id-token: write`"},
		},
		{
			name:     "a tar listing is not packing",
			src:      step("        run: tar -tzf rmp.tar.gz\n"),
			problems: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertProblems(t, ciPublishingProblems(parseWorkflowSource(tc.src, "fixture.yml")), tc.problems)
		})
	}
}

// TestMakefileCheckRunsTheSameGateSet enforces the acceptance criterion that
// the gate set in each workflow matches the `check` target of the Makefile gate
// for gate: no gate may be present in one and absent from the other. The
// workflows are held to the same table by the tests above, so checking the
// Makefile against it closes the loop.
func TestMakefileCheckRunsTheSameGateSet(t *testing.T) {
	facts := loadSpecGates(t)

	prerequisites := makefileCheckTarget(t, readMakefile(t))
	for gate := range facts.commands {
		if !slices.Contains(prerequisites, gate) {
			t.Errorf("the Makefile's `check` target runs %v, which omits the %q gate. "+
				"SPEC/BUILD.md § Validation Gates defines one gate set for all three places that enforce it, "+
				"and the acceptance criteria require the Makefile and both workflows to match gate for gate.",
				prerequisites, gate)
		}
	}
	for _, gate := range prerequisites {
		if facts.commands[gate] == "" {
			t.Errorf("the Makefile's `check` target runs %q, which SPEC/BUILD.md § Validation Gates does not "+
				"list as a gate. The gate set is defined there and nowhere else: either the specification "+
				"or the Makefile is wrong.", gate)
		}
	}
}

// makefileCheckTarget returns the prerequisites of the Makefile's `check`
// target, following line continuations.
func makefileCheckTarget(t *testing.T, makefile string) []string {
	t.Helper()

	for line := range strings.Lines(makefile) {
		if !strings.HasPrefix(line, "check:") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "check:"))
		for strings.HasSuffix(body, `\`) {
			rest := makefile[strings.Index(makefile, line)+len(line):]
			next, _, _ := strings.Cut(rest, "\n")
			body = strings.TrimSuffix(body, `\`) + " " + strings.TrimSpace(next)
			line = next + "\n"
		}
		return strings.Fields(body)
	}

	t.Fatalf("the Makefile declares no `check` target; SPEC/BUILD.md § Validation Gates names `make check` " +
		"as the aggregate command that runs the six gates locally")
	return nil
}

// TestWorkflowTriggersMatchSpec pins the events each workflow runs on, so the
// release pipeline cannot stop reacting to a `v*` tag, and CI cannot stop
// running on pull requests, without this test saying so.
func TestWorkflowTriggersMatchSpec(t *testing.T) {
	// SPEC/BUILD.md § Release Workflow and § CI Workflow state these triggers
	// in prose ("Push of tags matching `v*`"; "Push to the `main` branch, and
	// pull requests targeting `main`"). They are written out here rather than
	// parsed, because the prose has no structure to parse honestly.
	cases := []struct {
		events map[string]map[string][]string
		path   string
	}{
		{path: ciWorkflowPath, events: map[string]map[string][]string{
			"push":         {"branches": {"main"}},
			"pull_request": {"branches": {"main"}},
		}},
		{path: releaseWorkflowPath, events: map[string]map[string][]string{
			"push": {"tags": {"v*"}},
		}},
	}

	for _, testCase := range cases {
		t.Run(filepath.Base(testCase.path), func(t *testing.T) {
			wf := parseWorkflow(t, testCase.path)
			rel := strings.TrimPrefix(testCase.path, "../../")

			if len(wf.triggers) != len(testCase.events) {
				t.Fatalf("%s: triggers on %d event(s) %v, but SPEC/BUILD.md § GitHub Actions Workflow "+
					"specifies %v", rel, len(wf.triggers), wf.triggers, testCase.events)
			}
			for event, filters := range testCase.events {
				for filter, want := range filters {
					if got := wf.triggers[event][filter]; !slices.Equal(got, want) {
						t.Errorf("%s: the %s trigger filters %s on %v, but SPEC/BUILD.md § GitHub Actions "+
							"Workflow specifies %v", rel, event, filter, got, want)
					}
				}
			}
		})
	}
}

// TestWorkflowBuildMatricesMatchSpec pins the `build` gate's coverage in both
// workflows: the release workflow ships every Primary Platform of
// SPEC/BUILD.md § Supported Build Targets, in the same order, and CI builds the
// four-target fast-feedback subset that § Permitted Differences Between the
// Three Pipelines names. The expected sets are read out of the specification —
// the release matrix from the same table TestSupportedBuildTargetsMatchSpec
// pins, so the two tests cannot disagree.
func TestWorkflowBuildMatricesMatchSpec(t *testing.T) {
	release := parseWorkflow(t, releaseWorkflowPath)
	releaseBuild := mustJob(t, release, "build")
	built := matrixTargets(t, &releaseBuild)
	if len(built) != len(supportedBuildTargets) {
		t.Fatalf(".github/workflows/release.yml: the build matrix has %d targets %v, but SPEC/BUILD.md "+
			"§ Supported Build Targets lists %d Primary Platforms. § Release Workflow requires the release "+
			"to build all of them.", len(built), built, len(supportedBuildTargets))
	}
	for i, want := range supportedBuildTargets {
		if got := built[i]; got.goos != want.goos || got.goarch != want.goarch || got.goarm != want.goarm {
			t.Errorf(".github/workflows/release.yml: build matrix entry %d is %s/%s (GOARM %q), but "+
				"SPEC/BUILD.md § Supported Build Targets has %s there. § Release Workflow requires the "+
				"nine Primary Platforms in the same order.",
				i, got.goos, got.goarch, got.goarm, want.name)
		}
	}

	ci := parseWorkflow(t, ciWorkflowPath)
	ciBuild := mustJob(t, ci, "build")
	subset := matrixTargets(t, &ciBuild)
	want := parseFastFeedbackSubset(t)
	if len(subset) != len(want) {
		t.Fatalf(".github/workflows/ci.yml: the build matrix has %d targets %v, but SPEC/BUILD.md "+
			"§ Permitted Differences Between the Three Pipelines (difference 3) names the subset %v",
			len(subset), subset, want)
	}
	for i, target := range subset {
		if got := target.goos + "/" + target.goarch; got != want[i] {
			t.Errorf(".github/workflows/ci.yml: build matrix entry %d is %s, but SPEC/BUILD.md "+
				"§ Permitted Differences Between the Three Pipelines (difference 3) names %s there. "+
				"The subset is a statement about feedback speed, not about portability, and it is specified.",
				i, got, want[i])
		}
	}
}

// specSubsetTarget matches a GOOS/GOARCH pair as the specification writes one
// in prose.
var specSubsetTarget = regexp.MustCompile("`([a-z0-9]+/[a-z0-9]+)`")

// parseFastFeedbackSubset reads the CI build subset out of SPEC/BUILD.md §
// Permitted Differences Between the Three Pipelines.
func parseFastFeedbackSubset(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specBuildPath))
	if err != nil {
		t.Fatalf("reading the build specification: %v", err)
	}

	section := specSection(t, string(raw), "### Permitted Differences Between the Three Pipelines")
	matches := specSubsetTarget.FindAllStringSubmatch(section, -1)
	targets := make([]string, 0, len(matches))
	for _, match := range matches {
		targets = append(targets, match[1])
	}
	if len(targets) == 0 {
		t.Fatalf("SPEC/BUILD.md § Permitted Differences Between the Three Pipelines no longer names the " +
			"CI build subset as `goos/goarch` pairs; this test cannot check the CI matrix against it")
	}
	return targets
}

// matrixTargets reads the GOOS/GOARCH/GOARM triples out of a build job's
// `strategy.matrix.include` list.
func matrixTargets(t *testing.T, job *wfJob) []buildTarget {
	t.Helper()

	if len(job.matrix) == 0 {
		t.Fatalf("job %q declares no `strategy.matrix.include` entries; the `build` gate's coverage cannot "+
			"be checked against SPEC/BUILD.md § Supported Build Targets", job.id)
	}
	targets := make([]buildTarget, 0, len(job.matrix))
	for _, entry := range job.matrix {
		targets = append(targets, buildTarget{
			goos:   entry["goos"],
			goarch: entry["goarch"],
			goarm:  entry["goarm"],
		})
	}
	return targets
}

// TestPublishedArchivesPackTheSpecifiedStructure is the regression gate for the
// archives that shipped without the project's licence. SPEC/BUILD.md §
// Artifact Structure has always listed three entries per archive — the binary,
// LICENSE and README.md — while the workflows packed the binary alone, so every
// published archive of every target omitted the licence file and nothing
// noticed.
//
// The section governs every published archive, and the release workflow is the
// only workflow that publishes one, so only its build job is checked here; that
// the CI workflow packs no archive at all is TestCIWorkflowPublishesNothing's
// concern. Everything expected is parsed out of the specification and never
// restated here: an entry added to its drawing, or a binary name changed in its
// Target OS table, fails this test until the release workflow ships it.
func TestPublishedArchivesPackTheSpecifiedStructure(t *testing.T) {
	spec := parseArtifactStructure(t)

	for _, p := range pipelines() {
		if !p.publishes() {
			continue
		}
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			wf := parseWorkflow(t, p.path)
			build := mustJob(t, wf, p.buildJob)
			commands := jobCommands(&build)

			// Which archive forms the workflow must produce follows from the
			// targets its build matrix covers: a Windows target owes a .zip
			// holding rmp.exe, and every other target a .tar.gz holding rmp
			// (SPEC/BUILD.md § Artifact Structure).
			packSteps := make([]int, 0, len(spec.forms))
			for _, form := range requiredForms(t, spec, &build, p.rel()) {
				pack := packCommand(t, commands, packTool(t, form.format), p.rel())
				assertArchiveMembers(t, pack, append([]string{form.binary}, spec.extras...), form.format, p.rel())
				assertBinaryBuilt(t, commands, pack.step, form.binary, p.rel())
				if !slices.Contains(packSteps, pack.step) {
					packSteps = append(packSteps, pack.step)
				}
			}

			// Measured behaviour: with a listed member absent, `tar` exits 2
			// and fails the job, whereas `zip -q` exits 0 and silently writes
			// an archive without it. The pack list alone is therefore not
			// evidence that an entry ships, so the entries must also be staged
			// into the directory the archive is built from.
			for _, step := range packSteps {
				assertStaged(t, commands, step, spec.extras, p.rel())
			}
		})
	}
}

// artifactSpec is what SPEC/BUILD.md § Artifact Structure requires of every
// published archive: the entries beside the binary, and the form the binary and
// the archive take for each target operating system.
type artifactSpec struct {
	forms  []archiveForm
	extras []string
}

// archiveForm is one row of the Target OS table in SPEC/BUILD.md § Artifact
// Structure. An empty os is the row covering every other target OS.
type archiveForm struct {
	os     string
	binary string
	format string
}

// formFor returns the archive form a target operating system takes, falling
// back to the row that covers every other OS.
func (s artifactSpec) formFor(goos string) (archiveForm, bool) {
	fallback, found := archiveForm{}, false
	for _, form := range s.forms {
		if form.os == goos {
			return form, true
		}
		if form.os == "" {
			fallback, found = form, true
		}
	}
	return fallback, found
}

// requiredForms returns the distinct archive forms a build job must produce,
// derived from the target operating systems in its matrix.
func requiredForms(t *testing.T, spec artifactSpec, job *wfJob, rel string) []archiveForm {
	t.Helper()

	forms := make([]archiveForm, 0, len(spec.forms))
	for _, target := range matrixTargets(t, job) {
		form, ok := spec.formFor(target.goos)
		if !ok {
			t.Fatalf("%s: the build job targets %s, and SPEC/BUILD.md § Artifact Structure gives no archive "+
				"form for it — its Target OS table names no row covering every other target OS",
				rel, target.goos)
		}
		if !slices.Contains(forms, form) {
			forms = append(forms, form)
		}
	}
	return forms
}

// parseArtifactStructure reads SPEC/BUILD.md § Artifact Structure: the entries
// the drawing lists, and the Target OS table that gives the binary entry's name
// and the archive format for each operating system.
//
// The section deliberately carries no "###" subheading, because specSection
// stops at the first line beginning with "#". Nothing here may introduce one.
func parseArtifactStructure(t *testing.T) artifactSpec {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specBuildPath))
	if err != nil {
		t.Fatalf("reading the build specification: %v", err)
	}
	section := specSection(t, string(raw), "## Artifact Structure")

	drawnBinary, extras := parseArtifactDrawing(t, section)
	forms := parseArchiveForms(t, section)

	// The drawing and the table are two statements about the same thing, so
	// they must agree: the drawing shows the form every target uses except
	// Windows, which is the table's fallback row.
	fallback, ok := artifactSpec{forms: forms}.formFor("")
	if !ok {
		t.Fatalf("SPEC/BUILD.md § Artifact Structure: the Target OS table has no row covering every other " +
			"target OS, so this test cannot tell what a non-Windows archive must hold")
	}
	if fallback.binary != drawnBinary {
		t.Fatalf("SPEC/BUILD.md § Artifact Structure contradicts itself: the drawing marks %q as the binary "+
			"entry while the Target OS table gives %q for every target OS but Windows. Repair the "+
			"specification before this gate can hold the workflows to it.",
			drawnBinary, fallback.binary)
	}

	return artifactSpec{forms: forms, extras: extras}
}

// parseArtifactDrawing reads the tree the section draws, returning the entry it
// marks as the binary and every other entry, in the order the drawing lists
// them.
func parseArtifactDrawing(t *testing.T, section string) (string, []string) {
	t.Helper()

	block, ok := fencedBlock(section)
	if !ok {
		t.Fatalf("SPEC/BUILD.md § Artifact Structure no longer holds a fenced block drawing the archive " +
			"layout; this test cannot tell what a published archive must contain")
	}

	binaries := make([]string, 0, 1)
	extras := make([]string, 0, 4)
	for line := range strings.Lines(block) {
		line = strings.TrimRight(line, "\r\n")

		// An entry is a line drawn under a tree connector. The block's first
		// line is the archive's own file name and carries none, so it is not
		// an entry.
		trimmed := strings.TrimLeft(line, treeDrawing)
		if !strings.ContainsAny(line[:len(line)-len(trimmed)], treeConnectors) {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}

		annotation := ""
		if _, after, found := strings.Cut(trimmed, "#"); found {
			annotation = strings.ToLower(strings.TrimSpace(after))
		}
		if strings.Contains(annotation, "binary") {
			binaries = append(binaries, fields[0])
			continue
		}
		extras = append(extras, fields[0])
	}

	if len(binaries) != 1 {
		t.Fatalf("SPEC/BUILD.md § Artifact Structure marks %d entries as the binary (%v), so this test cannot "+
			"tell which archive member is the binary. It matters because Windows ships that member under "+
			"another name, and the rest of the entries are compared literally.",
			len(binaries), binaries)
	}
	if len(extras) == 0 {
		t.Fatalf("SPEC/BUILD.md § Artifact Structure lists no entry besides the binary %q; the archive-contents "+
			"gate would have nothing to enforce, so the drawing's format has changed and this reader must be "+
			"fixed rather than the workflows", binaries[0])
	}
	return binaries[0], extras
}

// archiveFormHeader is the header row of the table that gives the binary
// entry's name and the archive format per operating system. The section carries
// a second table listing the published archives, so the right one is selected
// by its header rather than by position.
var archiveFormHeader = []string{"Target OS", "Binary entry", "Archive format"}

// parseArchiveForms reads the Target OS table of SPEC/BUILD.md § Artifact
// Structure. A cell naming a literal value does so in backticks; a cell of
// prose ("Every other target OS") marks the fallback row.
func parseArchiveForms(t *testing.T, section string) []archiveForm {
	t.Helper()

	forms := make([]archiveForm, 0, 2)
	inTable := false
	for line := range strings.Lines(section) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			if inTable {
				break // the table ended
			}
			continue
		}

		cells := splitTableRow(line)
		if !inTable {
			inTable = len(cells) >= len(archiveFormHeader) &&
				slices.Equal(cells[:len(archiveFormHeader)], archiveFormHeader)
			continue
		}
		if len(cells) < 3 || strings.HasPrefix(cells[0], "---") {
			continue // separator row
		}

		if !backquotedToken(cells[1]) || !backquotedToken(cells[2]) {
			t.Fatalf("SPEC/BUILD.md § Artifact Structure: the Target OS row %q does not name the binary entry "+
				"and the archive format as literal values in backticks, so this test cannot read them "+
				"unambiguously. Repair the table or this reader rather than letting the archive go unchecked.",
				line)
		}
		form := archiveForm{binary: strings.Trim(cells[1], "`"), format: strings.Trim(cells[2], "`")}
		if backquotedToken(cells[0]) {
			form.os = strings.Trim(cells[0], "`")
		}
		forms = append(forms, form)
	}

	if len(forms) < 2 {
		t.Fatalf("SPEC/BUILD.md § Artifact Structure: found %d rows in a table headed %v, expected at least "+
			"one operating system and one fallback row. Without them this test cannot tell what each "+
			"archive must hold.", len(forms), archiveFormHeader)
	}
	fallbacks := 0
	for _, form := range forms {
		if form.os == "" {
			fallbacks++
		}
	}
	if fallbacks != 1 {
		t.Fatalf("SPEC/BUILD.md § Artifact Structure: the Target OS table has %d rows covering every other "+
			"target OS (%v), expected exactly one. The archive form of an unlisted OS would be ambiguous.",
			fallbacks, forms)
	}
	return forms
}

// splitTableRow returns a markdown table row's cells, trimmed of spaces.
func splitTableRow(line string) []string {
	cells := strings.Split(strings.Trim(line, "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// backquotedToken reports whether a table cell names a literal value — a single
// token in backticks — rather than prose such as "Every other target OS".
func backquotedToken(cell string) bool {
	if len(cell) < 3 || cell[0] != '`' || cell[len(cell)-1] != '`' {
		return false
	}
	inner := cell[1 : len(cell)-1]
	return inner != "" && !strings.ContainsAny(inner, " `")
}

// packTool names the command that builds an archive of the given format.
func packTool(t *testing.T, format string) string {
	t.Helper()

	switch {
	case strings.Contains(format, "tar"):
		return "tar"
	case strings.Contains(format, "zip"):
		return "zip"
	}
	t.Fatalf("SPEC/BUILD.md § Artifact Structure requires a %s archive, and this test does not know which "+
		"command builds that format. Teach it the format rather than leaving the archive unchecked.", format)
	return ""
}

// treeDrawing is everything that can precede an entry's name in the
// specification's tree, and treeConnectors is the subset that proves a line is
// an entry rather than an indented continuation.
const (
	treeDrawing    = "├└│─|\\+`- \t"
	treeConnectors = "├└│─|\\+`"
)

// fencedBlock returns the body of the first fenced code block in a section.
func fencedBlock(section string) (string, bool) {
	_, after, found := strings.Cut(section, "```")
	if !found {
		return "", false
	}
	if _, after, found = strings.Cut(after, "\n"); !found {
		return "", false
	}
	body, _, found := strings.Cut(after, "```")
	return body, found
}

// artifactPack is one `tar` or `zip` invocation that builds an archive.
type artifactPack struct {
	members []string
	tool    string
	text    string
	step    int
}

// packCommand finds the single invocation of a packing tool in a build job.
func packCommand(t *testing.T, commands []wfCommand, tool, rel string) artifactPack {
	t.Helper()

	found := make([]artifactPack, 0, 2)
	texts := make([]string, 0, 2)
	for _, cmd := range commands {
		words := shellWords(cmd.text)
		if !slices.Contains(words, tool) {
			continue
		}
		found = append(found, artifactPack{
			tool: tool, text: cmd.text, step: cmd.step, members: packMembers(tool, words),
		})
		texts = append(texts, cmd.text)
	}

	if len(found) != 1 {
		t.Fatalf("%s: the build job runs %d %s commands %q, but this test expects exactly one that packs the "+
			"archive SPEC/BUILD.md § Artifact Structure requires for the targets in its matrix.",
			rel, len(found), tool, texts)
	}
	if len(found[0].members) == 0 {
		t.Fatalf("%s: no archive members could be read out of %q, so this test can no longer tell what the "+
			"%s archive ships. The command's form changed: fix this reader rather than the workflow.",
			rel, found[0].text, tool)
	}
	return found[0]
}

// packMembers returns the files a `tar` or `zip` command adds to an archive,
// skipping the flags and the operands those flags consume.
func packMembers(tool string, words []string) []string {
	start := slices.Index(words, tool)
	if start < 0 {
		return nil
	}

	members := make([]string, 0, len(words))
	// `tar` names its archive with -f, so its first bare operand is already a
	// member; `zip` names its archive as the first bare operand.
	archiveNamed := tool == "tar"
	for i := start + 1; i < len(words); i++ {
		word := words[i]
		switch {
		case strings.HasPrefix(word, "--"):
			if word == "--file" || word == "--directory" {
				i++ // long flag with a separate operand; the "--flag=value" form carries its own
			}
		case strings.HasPrefix(word, "-"):
			if tool == "tar" && (word == "-C" || strings.Contains(word, "f")) {
				i++ // the archive path (-f) or the directory to pack from (-C)
			}
		case !archiveNamed:
			archiveNamed = true
		default:
			members = append(members, word)
		}
	}
	return members
}

// assertArchiveMembers compares what a pack command ships against what the
// specification requires.
func assertArchiveMembers(t *testing.T, pack artifactPack, want []string, format, rel string) {
	t.Helper()

	missing := make([]string, 0, len(want))
	for _, entry := range want {
		if !slices.Contains(pack.members, entry) {
			missing = append(missing, entry)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s: the %s archive is packed with %q and omits %q. SPEC/BUILD.md § Artifact Structure "+
			"requires %q in every published archive. Shipping without them is the defect this test exists "+
			"to prevent: every published archive once omitted the licence file exactly this way. Command: %s",
			rel, format, pack.members, missing, want, pack.text)
	}

	unexpected := make([]string, 0, len(pack.members))
	for _, member := range pack.members {
		if !slices.Contains(want, member) {
			unexpected = append(unexpected, member)
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("%s: the %s archive also packs %q, which SPEC/BUILD.md § Artifact Structure does not list — "+
			"it requires exactly %q and nothing else. Either the specification or the workflow is wrong. "+
			"Command: %s", rel, format, unexpected, want, pack.text)
	}
}

// assertBinaryBuilt requires the job to produce, outside the packing step
// itself, the binary file name the archive lists. `zip` exits 0 when a listed
// member is missing, so a build that stopped producing rmp.exe while the .zip
// still listed it would publish an archive holding no binary at all and stay
// green.
//
// What this proves is that the name appears in the job's other commands, which
// is exact for a name like `rmp.exe` that can occur nowhere else. For the plain
// binary name it is weaker, because the build command's package operand
// (./cmd/rmp) ends in that name too — but that is the case `tar` already fails
// loudly on, so nothing silent hides behind the looser half.
func assertBinaryBuilt(t *testing.T, commands []wfCommand, packStep int, binary, rel string) {
	t.Helper()

	mention := regexp.MustCompile(`(^|[\s"'=/])` + regexp.QuoteMeta(binary) + `($|[\s"'])`)
	for _, cmd := range commands {
		if cmd.step != packStep && mention.MatchString(cmd.text) {
			return
		}
	}
	t.Errorf("%s: the archive lists %q, but no command in the build job outside the packing step names that "+
		"file, so nothing in the job produces it. SPEC/BUILD.md § Artifact Structure requires the binary in "+
		"every published archive, and `zip` would write one without it and still exit 0.", rel, binary)
}

// stagingTools are the commands that can put a file into the directory an
// archive is packed from.
var stagingTools = []string{"cp", "install", "ln", "mv", "rsync"}

// assertStaged requires every specified entry to be placed into the directory
// the archive is packed from.
func assertStaged(t *testing.T, commands []wfCommand, step int, extras []string, rel string) {
	t.Helper()

	staged := make([]string, 0, len(commands))
	for _, cmd := range commands {
		if cmd.step != step {
			continue
		}
		words := shellWords(cmd.text)
		if len(words) > 1 && slices.Contains(stagingTools, words[0]) {
			staged = append(staged, words[1:]...)
		}
	}

	for _, entry := range extras {
		if slices.Contains(staged, entry) {
			continue
		}
		t.Errorf("%s: the archive step packs %q but never stages it into the directory the archive is built "+
			"from (it stages %q). `zip` exits 0 and silently writes an archive without a member that does "+
			"not exist, so a .zip would ship without %q while the job stayed green. SPEC/BUILD.md "+
			"§ Artifact Structure requires it in every published archive.",
			rel, entry, staged, entry)
	}
}

// TestWorkflowsDoNotSetConstantsWithLdflags is the regression gate for rmp task
// #157. Both workflows used to build with `-X main.version=<tag>`, but
// cmd/rmp/main.go declares version in a const block and the linker's -X writes
// only to a package-level var, so the flag was silently ignored: a build that
// claimed to stamp the released version stamped nothing, and `rmp --version`
// reported the constant regardless of the tag.
//
// The check is the general form of that defect rather than the single flag:
// cmd/rmp/main.go is parsed with go/parser — standard library, so no dependency
// is added — and any -X naming a symbol that file declares as a const fails the
// test. Because SPEC/VERSION.md § Application Version requires `version` to be
// that constant (asserted below), this subsumes the narrow criterion that
// neither workflow may pass `-X main.version`.
func TestWorkflowsDoNotSetConstantsWithLdflags(t *testing.T) {
	constants := mainPackageConstants(t)

	if !constants["version"] {
		t.Fatalf("cmd/rmp/main.go does not declare `version` as a const, but SPEC/VERSION.md § Application "+
			"Version specifies `const version = \"X.Y.Z\"` there as the single source of the application "+
			"version. The consts it does declare are %v.", sortedKeys(constants))
	}

	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			wf := parseWorkflow(t, p.path)
			for _, id := range wf.jobOrder {
				job := wf.jobs[id]
				for _, cmd := range jobCommands(&job) {
					for _, match := range ldflagSymbol.FindAllStringSubmatch(cmd.text, -1) {
						pkg, symbol := match[1], match[2]
						if !isMainPackage(pkg) || !constants[symbol] {
							continue
						}
						t.Errorf("%s: job %q passes `-X %s.%s=` to the linker, but cmd/rmp/main.go declares "+
							"%s as a const. The linker writes -X only into a package-level var, so the flag "+
							"is silently ignored and the build stamps nothing while claiming to. "+
							"SPEC/VERSION.md § Application Version makes that constant the single source of "+
							"the version. Command: %s",
							p.rel(), id, pkg, symbol, symbol, cmd.text)
					}
				}
			}
		})
	}
}

// ldflagSymbol matches a linker -X flag that assigns a package-level symbol,
// in the forms `-X main.version=`, `-X=main.version=` and `-X 'main.version=`.
// The trailing "=" is required, so prose mentioning the flag — such as the
// comment in each workflow explaining why it is absent — does not match.
var ldflagSymbol = regexp.MustCompile(`-X[ =]['"]?([A-Za-z0-9_./-]+)\.([A-Za-z_][A-Za-z0-9_]*)=`)

// mainSourcePath is cmd/rmp/main.go. This test package lives in that same
// directory, which is where `go test` sets the working directory.
const mainSourcePath = "main.go"

// mainPackageConstants returns the names cmd/rmp/main.go declares as constants.
func mainPackageConstants(t *testing.T) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), filepath.Clean(mainSourcePath), nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", mainSourcePath, err)
	}

	constants := make(map[string]bool, 16)
	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range values.Names {
				constants[name.Name] = true
			}
		}
	}

	if len(constants) == 0 {
		t.Fatalf("%s declares no constants, which this test cannot be right about; fix the reader rather "+
			"than the source", mainSourcePath)
	}
	return constants
}

// isMainPackage reports whether a -X flag's package part names the released
// binary's package, which the linker accepts either as `main` or by its full
// import path.
func isMainPackage(pkg string) bool {
	return pkg == "main" || strings.HasSuffix(pkg, "/cmd/rmp")
}

// sortedKeys returns a map's keys in a stable order, for failure messages.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// shellWords splits a shell command into words, dropping the grouping
// parentheses and the quotes around an argument.
func shellWords(command string) []string {
	words := strings.Fields(strings.NewReplacer("(", " ", ")", " ").Replace(command))
	for i, word := range words {
		words[i] = strings.Trim(word, `"'`)
	}
	return words
}

// -----------------------------------------------------------------------------
// What the specification says
// -----------------------------------------------------------------------------

// Gate identifiers used as keys in the map resolveGates returns. The first five
// are the gate targets SPEC/BUILD.md § Validation Gates names; gateGosecInstall
// is the install step § A Missing Tool Is a Failure, Never a Skip requires
// alongside the security gate, located whatever version it installs.
const (
	gateFmt          = "fmt"
	gateVet          = "vet"
	gateLint         = "lint"
	gateTest         = "test"
	gateSecurity     = "security"
	gateGosecInstall = "gosec install"
)

// specGates is everything these tests read out of SPEC/BUILD.md. The tool
// versions are not part of it: the specification does not state them, and
// loadMakefilePins reads them out of the Makefile, which the specification
// names as their holder.
type specGates struct {
	commands     map[string]string // gate target -> command, from § Validation Gates
	gosecInstall installForm       // the install command § Security Scan: gosec documents
}

// installForm is a command the specification writes with a placeholder for a
// Makefile pin, such as
// `go install github.com/securego/gosec/v2/cmd/gosec@<GOSEC_VERSION>`.
type installForm struct {
	text        string // the command exactly as the specification writes it
	placeholder string // the placeholder it carries, angle brackets included
}

// with returns the command with its placeholder replaced by a version.
func (f installForm) with(version string) string {
	return strings.Replace(f.text, f.placeholder, version, 1)
}

// prefix returns the part of the command before its placeholder, which every
// installation of the tool shares whatever version it names.
func (f installForm) prefix() string {
	before, _, _ := strings.Cut(f.text, f.placeholder)
	return before
}

// pinVariableName matches the name of a Makefile pin variable as SPEC/BUILD.md
// writes one.
const pinVariableName = `([A-Z][A-Z0-9_]*)`

var (
	specLintPreamble  = regexp.MustCompile("`" + pinVariableName + "`\\s+holds the\\s+`golangci-lint`\\s+pin")
	specGosecPreamble = regexp.MustCompile("`" + pinVariableName + "`\\s+holds the\\s+`gosec`\\s+pin")
	specPinVariable   = regexp.MustCompile("The pinned version is the value of\\s+`" + pinVariableName +
		"`\\s+in the\\s+`Makefile`")
	specInstallForm = regexp.MustCompile(`(?m)^go install [^\s@]+@<` + pinVariableName + `>$`)
	specLintInput   = regexp.MustCompile("`version: <" + pinVariableName + ">`")
)

// loadSpecGates reads the gate set and the gosec install form out of
// SPEC/BUILD.md. It also checks that every statement of the specification that
// names a tool pin variable names the one these tests read from the Makefile:
// if the specification renames a variable, or names two different ones for the
// same tool, there is no single pin to hold the workflows to.
func loadSpecGates(t *testing.T) specGates {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specBuildPath))
	if err != nil {
		t.Fatalf("reading the build specification: %v", err)
	}
	spec := string(raw)

	preamble := specSection(t, spec, "## Static Analysis")
	gosecSection := specSection(t, spec, "### Security Scan: gosec")
	lintSection := specSection(t, spec, "### Linter: golangci-lint")

	names := []struct {
		pattern *regexp.Regexp
		section string
		name    string
		want    string
	}{
		{specLintPreamble, preamble, "§ Static Analysis (the golangci-lint pin variable)", lintPinVariable},
		{specGosecPreamble, preamble, "§ Static Analysis (the gosec pin variable)", gosecPinVariable},
		{specPinVariable, lintSection, "§ Linter: golangci-lint (the pinned version)", lintPinVariable},
		{specInstallForm, lintSection, "§ Linter: golangci-lint (the install command)", lintPinVariable},
		{specLintInput, lintSection, "§ Linter: golangci-lint (the action's version input)", lintPinVariable},
		{specPinVariable, gosecSection, "§ Security Scan: gosec (the pinned version)", gosecPinVariable},
		{specInstallForm, gosecSection, "§ Security Scan: gosec (the install command)", gosecPinVariable},
	}
	for _, check := range names {
		if got := specCapture(t, check.section, check.name, check.pattern); got != check.want {
			t.Fatalf("SPEC/BUILD.md %s names the Makefile variable %s, but these tests read that pin from %s. "+
				"§ Static Analysis makes one Makefile variable per tool the authoritative pin: if the variable "+
				"was renamed, rename it in these tests too.", check.name, got, check.want)
		}
	}

	// specCapture has already required exactly this form in the section.
	install := specInstallForm.FindStringSubmatch(gosecSection)
	return specGates{
		commands:     parseValidationGates(t, spec),
		gosecInstall: installForm{text: install[0], placeholder: "<" + install[1] + ">"},
	}
}

// parseValidationGates reads the gate table of SPEC/BUILD.md § Validation
// Gates. The table is the authoritative definition of the gate set, so this
// function refuses to continue if the set is no longer the six gates these
// tests know how to check: a seventh gate must be asserted here too, not
// silently ignored.
func parseValidationGates(t *testing.T, spec string) map[string]string {
	t.Helper()

	section := specSection(t, spec, "## Validation Gates")
	commands := make(map[string]string, 6)
	for line := range strings.Lines(section) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 2 {
			continue
		}
		target := strings.Trim(strings.TrimSpace(cells[0]), "`")
		if target == "Target" || strings.HasPrefix(target, "---") {
			continue // header or separator row
		}
		commands[target] = strings.Trim(strings.TrimSpace(cells[1]), "`")
	}

	known := []string{gateFmt, gateVet, gateTest, "build", gateLint, gateSecurity}
	if len(commands) != len(known) {
		t.Fatalf("SPEC/BUILD.md § Validation Gates now lists %d gates (%v), not the %d this test checks. "+
			"The gate set changed: extend these tests so the new set is enforced in both workflows.",
			len(commands), commands, len(known))
	}
	for _, gate := range known {
		if commands[gate] == "" {
			t.Fatalf("SPEC/BUILD.md § Validation Gates no longer defines a command for the %q gate; "+
				"parsed table: %v", gate, commands)
		}
	}
	return commands
}

// specSection returns the body of a SPEC/BUILD.md section: the text between the
// given heading and the next heading of any level.
func specSection(t *testing.T, spec, heading string) string {
	t.Helper()

	start := strings.Index(spec, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("SPEC/BUILD.md has no %q section, so the workflow gate tests have nothing to verify "+
			"the workflows against", heading)
	}

	var body strings.Builder
	for line := range strings.Lines(spec[start+len(heading)+2:]) {
		if strings.HasPrefix(line, "#") {
			break
		}
		body.WriteString(line)
	}
	return body.String()
}

// specCapture extracts the value a pattern captures from a section of the
// specification, and requires every occurrence of the pattern in that section
// to capture the same one.
func specCapture(t *testing.T, section, name string, pattern *regexp.Regexp) string {
	t.Helper()

	matches := pattern.FindAllStringSubmatch(section, -1)
	if len(matches) == 0 {
		t.Fatalf("SPEC/BUILD.md %s no longer states anything matching %s; "+
			"these tests cannot hold the workflows to a statement the specification does not make",
			name, pattern)
	}
	value := matches[0][1]
	for _, match := range matches[1:] {
		if match[1] != value {
			t.Fatalf("SPEC/BUILD.md %s names two different values, %s and %s; "+
				"§ Static Analysis requires one pin variable per tool", name, value, match[1])
		}
	}
	return value
}

// -----------------------------------------------------------------------------
// The gate job under test
// -----------------------------------------------------------------------------

// gateJob is a workflow's gate job, flattened into the shell commands it runs
// in execution order.
type gateJob struct {
	commands []wfCommand
	job      wfJob
	pipeline pipeline
}

// wfCommand is one shell command of a job, with the step it belongs to.
type wfCommand struct {
	text string
	step int
}

// loadGateJob parses a workflow and returns its gate job.
func loadGateJob(t *testing.T, p pipeline) *gateJob {
	t.Helper()

	wf := parseWorkflow(t, p.path)
	job := mustJob(t, wf, p.gateJob)
	if len(job.steps) == 0 {
		t.Fatalf("%s: job %q declares no steps; the workflow layout changed and these tests can no longer "+
			"see which gates run", p.rel(), p.gateJob)
	}

	return &gateJob{job: job, pipeline: p, commands: jobCommands(&job)}
}

// jobCommands flattens a job's run steps into the shell commands it executes,
// in order, dropping blank lines and comments.
func jobCommands(job *wfJob) []wfCommand {
	commands := make([]wfCommand, 0, len(job.steps))
	for index, step := range job.steps {
		for line := range strings.Lines(step.run) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			commands = append(commands, wfCommand{text: line, step: index})
		}
	}
	return commands
}

// find returns the position of the first command satisfying match, or -1.
func (g *gateJob) find(match func(string) bool) int {
	for i, cmd := range g.commands {
		if match(cmd.text) {
			return i
		}
	}
	return -1
}

// where describes a command's position for a failure message.
func (g *gateJob) where(index int) string {
	if index < 0 || index >= len(g.commands) {
		return "no step"
	}
	return "step " + g.job.steps[g.commands[index].step].name
}

// resolveGates locates the step that carries each gate of SPEC/BUILD.md §
// Validation Gates in the gate job, reporting every gate it cannot find. The
// returned map is keyed by gate identifier and holds the index of the step that
// runs it, so the caller can inspect that step further.
func (g *gateJob) resolveGates(t *testing.T, facts specGates) map[string]int {
	t.Helper()

	found := make(map[string]int, 6)

	// fmt, vet and security run one exact command each. Comparing against the
	// command the specification states — rather than a looser pattern — is what
	// keeps the scope of a gate identical in every pipeline: `gosec` scanning a
	// different tree, or with different `#nosec` suppressions in force, is not
	// the same gate.
	exact := []struct{ gate, command string }{
		{gateFmt, facts.commands[gateFmt]},
		{gateVet, facts.commands[gateVet]},
		{gateSecurity, facts.commands[gateSecurity]},
	}
	for _, want := range exact {
		wanted := want.command
		if at := g.find(func(cmd string) bool { return cmd == wanted }); at >= 0 {
			found[want.gate] = g.commands[at].step
			continue
		}
		t.Errorf("%s: the %s gate is missing from job %q: no step runs %q. "+
			"SPEC/BUILD.md § Validation Gates defines the six-gate set and § Where the Gate Set Is Enforced "+
			"admits no per-pipeline exception — a `v*` tag must not publish a release unless every gate ran. "+
			"Publishing a release with any gate absent is the defect this test exists to prevent.",
			g.pipeline.rel(), want.gate, g.pipeline.gateJob, wanted)
	}

	// The gosec install step is located by the part of the specified command
	// before its version placeholder, so it is found whatever version it names.
	// TestWorkflowToolPinsMatchMakefile compares that version with the
	// Makefile's pin, and reports a missing install.
	installPrefix := facts.gosecInstall.prefix()
	if at := g.find(func(cmd string) bool { return strings.HasPrefix(cmd, installPrefix) }); at >= 0 {
		found[gateGosecInstall] = g.commands[at].step
	}

	// The test gate runs the local command widened with the race detector, and
	// in CI with a coverage profile as well (§ Permitted Differences Between
	// the Three Pipelines, difference 2). Match on that shape rather than on a
	// fixed string, so an added flag does not fail the test but a narrowed
	// scope or a dropped `-race` does.
	local := strings.Fields(facts.commands[gateTest])
	if at := g.find(func(cmd string) bool {
		fields := strings.Fields(cmd)
		if len(fields) < len(local) || fields[0] != local[0] || fields[1] != local[1] {
			return false
		}
		return fields[len(fields)-1] == local[len(local)-1] && slices.Contains(fields, "-race")
	}); at >= 0 {
		found[gateTest] = g.commands[at].step
	} else {
		t.Errorf("%s: the test gate is missing from job %q: no step runs %q over %q with `-race`. "+
			"SPEC/BUILD.md § Validation Gates requires the gate, and § Permitted Differences Between the "+
			"Three Pipelines (difference 2) requires the workflows to run it with the race detector.",
			g.pipeline.rel(), g.pipeline.gateJob,
			strings.Join(local[:2], " "), local[len(local)-1])
	}

	// The lint gate may run through the official action, which installs the
	// pinned linter and runs `golangci-lint run ./...` itself (§ Permitted
	// Differences Between the Three Pipelines).
	for index, step := range g.job.steps {
		if strings.HasPrefix(step.uses, lintActionPrefix) {
			found[gateLint] = index
			break
		}
	}
	if _, ok := found[gateLint]; !ok {
		if at := g.find(func(cmd string) bool { return cmd == facts.commands[gateLint] }); at >= 0 {
			found[gateLint] = g.commands[at].step
		} else {
			t.Errorf("%s: the lint gate is missing from job %q: no step uses the golangci-lint action and "+
				"no step runs %q. SPEC/BUILD.md § Validation Gates requires the gate in every pipeline; "+
				"a release built without the linter having run is exactly what this test prevents.",
				g.pipeline.rel(), g.pipeline.gateJob, facts.commands[gateLint])
		}
	}

	// A gate that runs over a narrowed scope is not the gate the specification
	// defines, so the arguments handed to the action must be flags only: the
	// timeout is an execution limit, a path is a change of scope.
	if index, ok := found[gateLint]; ok {
		step := g.job.steps[index]
		for _, arg := range strings.Fields(step.with["args"]) {
			if !strings.HasPrefix(arg, "-") {
				t.Errorf("%s: step %q passes %q to the golangci-lint action, narrowing the scope of the lint "+
					"gate. SPEC/BUILD.md § Permitted Differences Between the Three Pipelines allows a timeout "+
					"— an execution limit — but requires `%s` over the same scope in all three places.",
					g.pipeline.rel(), step.name, arg, facts.commands[gateLint])
			}
		}
	}

	return found
}

// TestWorkflowReaderEndsABlockScalarAtALessIndentedLine is the regression test
// for the reader reading a comment that follows a `run: |` script into the
// script. A comment indented less than the script — the one that introduces the
// next step — sits inside the step's body, because a step's body runs to the
// next sequence item, and wfValue used to keep every line of that body. It then
// cut the comment at the script's column, so "# Then run the tests." became the
// command "en run the tests.": a line no workflow runs, which a gate test
// matching commands could count or be misled by. The fixture also carries a
// shell comment and a blank line at the script's own indentation, which are
// part of the script and must stay in it.
func TestWorkflowReaderEndsABlockScalarAtALessIndentedLine(t *testing.T) {
	src := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - name: Build and vet\n" +
		"        run: |\n" +
		"          # Build first.\n" +
		"          go build ./...\n" +
		"\n" +
		"          go vet ./...\n" +
		"\n" +
		"      # Then run the tests.\n" +
		"      - name: Run tests\n" +
		"        run: |\n" +
		"          go test ./...\n" +
		"    # The last comment of the job.\n" +
		"  build:\n    runs-on: ubuntu-latest\n    steps:\n      - name: Build\n        run: go build ./...\n"

	job := mustJob(t, parseWorkflowSource(src, "fixture.yml"), "test")
	if len(job.steps) != 2 {
		t.Fatalf("parsed %d steps, want 2", len(job.steps))
	}
	for i, want := range []string{
		"# Build first.\ngo build ./...\n\ngo vet ./...",
		"go test ./...",
	} {
		if got := job.steps[i].run; got != want {
			t.Errorf("step %d (%q): run is %q, want %q. A block scalar ends at the first non-blank line "+
				"indented less than its content (YAML 1.2 § 8.1.1.1), a comment included.",
				i+1, job.steps[i].name, got, want)
		}
	}

	listed := jobCommands(&job)
	commands := make([]string, 0, len(listed))
	for _, cmd := range listed {
		commands = append(commands, cmd.text)
	}
	if want := []string{"go build ./...", "go vet ./...", "go test ./..."}; !slices.Equal(commands, want) {
		t.Errorf("the job's commands are %q, want %q", commands, want)
	}
}

// -----------------------------------------------------------------------------
// A very small YAML reader
// -----------------------------------------------------------------------------
//
// SPEC/BUILD.md § External Dependencies governs go.mod, and a YAML library is
// not among the dependencies it admits, so the workflows are read with the
// purpose-built scanner below. It understands only the subset these files use —
// nested mappings, sequences of mappings, and block scalars — and it derives
// every block boundary from the indentation actually present rather than from a
// fixed column, so reindenting a file, renaming a step or reordering the keys
// inside one does not disturb it. Removing a gate does.
//
// Where the scanner cannot see something honestly it does not pretend to: it
// reads a step's `run:` script as a list of physical lines, which is enough to
// match a gate command exactly but would not follow a command split across a
// line continuation. Every gate command in these workflows is a single line.

// wfLine is one physical line of a workflow file.
type wfLine struct {
	text string
	num  int
}

// wfEntry is one `key: value` entry of a mapping, together with the lines
// nested underneath it.
type wfEntry struct {
	body  []wfLine
	key   string
	value string
	num   int
}

// wfStep is one entry of a job's `steps:` sequence.
type wfStep struct {
	with            map[string]string
	name            string
	uses            string
	run             string
	num             int
	continueOnError bool
}

// wfJob is one entry of a workflow's `jobs:` mapping.
type wfJob struct {
	needs           []string
	steps           []wfStep
	matrix          []map[string]string // the entries of `strategy.matrix.include`
	permissions     map[string]string
	id              string
	name            string
	runsOn          string
	timeoutMinutes  string
	continueOnError string // the job-level `continue-on-error` value, empty when absent
	num             int
}

// wfWorkflow is a parsed workflow file.
type wfWorkflow struct {
	jobOrder    []string
	jobs        map[string]wfJob
	permissions map[string]string
	triggers    map[string]map[string][]string // event -> filter -> values
	path        string
}

var wfKeyPattern = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*):(?:[ \t](.*))?$`)

// parseWorkflow reads and parses a workflow file.
func parseWorkflow(t *testing.T, path string) wfWorkflow {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading the workflow %s: %v", strings.TrimPrefix(path, "../../"), err)
	}

	wf := parseWorkflowSource(string(raw), path)
	if len(wf.jobs) == 0 {
		t.Fatalf("%s: no jobs parsed. The workflow's layout changed in a way this reader does not "+
			"understand, so it can no longer prove the gates run; fix the reader rather than the workflow",
			strings.TrimPrefix(path, "../../"))
	}
	return wf
}

// parseWorkflowSource parses the text of a workflow; path names it in failure
// messages. It is separate from parseWorkflow so that fixtures can be parsed by
// the same reader as the real files.
func parseWorkflowSource(src, path string) wfWorkflow {
	wf := wfWorkflow{path: path, jobs: make(map[string]wfJob)}
	for _, entry := range wfMapping(wfSplitLines(src)) {
		switch entry.key {
		case "permissions":
			wf.permissions = wfScalarMap(entry.body)
		case "on":
			events := wfMapping(entry.body)
			wf.triggers = make(map[string]map[string][]string, len(events))
			for _, event := range events {
				filters := wfMapping(event.body)
				values := make(map[string][]string, len(filters))
				for _, filter := range filters {
					values[filter.key] = wfStringList(filter)
				}
				wf.triggers[event.key] = values
			}
		case "jobs":
			jobs := wfMapping(entry.body)
			wf.jobOrder = make([]string, 0, len(jobs))
			for _, job := range jobs {
				parsed := wfParseJob(job)
				wf.jobs[parsed.id] = parsed
				wf.jobOrder = append(wf.jobOrder, parsed.id)
			}
		}
	}
	return wf
}

// mustJob returns a job by its identifier, failing when the workflow no longer
// declares it.
func mustJob(t *testing.T, wf wfWorkflow, id string) wfJob {
	t.Helper()

	job, ok := wf.jobs[id]
	if !ok {
		t.Fatalf("%s: no job %q; the workflow declares %v. SPEC/BUILD.md § GitHub Actions Workflow names "+
			"the jobs each workflow must declare.",
			strings.TrimPrefix(wf.path, "../../"), id, wf.jobOrder)
	}
	return job
}

func wfParseJob(entry wfEntry) wfJob {
	job := wfJob{id: entry.key, num: entry.num}
	for _, field := range wfMapping(entry.body) {
		switch field.key {
		case "name":
			job.name = field.value
		case "runs-on":
			job.runsOn = field.value
		case "timeout-minutes":
			job.timeoutMinutes = field.value
		case "continue-on-error":
			job.continueOnError = field.value
		case "needs":
			job.needs = wfStringList(field)
		case "permissions":
			job.permissions = wfScalarMap(field.body)
		case "steps":
			items := wfSequence(field.body)
			job.steps = make([]wfStep, 0, len(items))
			for _, item := range items {
				job.steps = append(job.steps, wfParseStep(item))
			}
		case "strategy":
			job.matrix = wfParseMatrix(field.body)
		}
	}
	return job
}

// wfParseMatrix reads the entries of a job's `strategy.matrix.include` list.
func wfParseMatrix(block []wfLine) []map[string]string {
	for _, field := range wfMapping(block) {
		if field.key != "matrix" {
			continue
		}
		for _, dimension := range wfMapping(field.body) {
			if dimension.key != "include" {
				continue
			}
			items := wfSequence(dimension.body)
			entries := make([]map[string]string, 0, len(items))
			for _, item := range items {
				entries = append(entries, wfScalarMap(item))
			}
			return entries
		}
	}
	return nil
}

func wfParseStep(item []wfLine) wfStep {
	step := wfStep{with: make(map[string]string)}
	if len(item) > 0 {
		step.num = item[0].num
	}
	for _, field := range wfMapping(item) {
		switch field.key {
		case "name":
			step.name = field.value
		case "uses":
			step.uses = field.value
		case "run":
			step.run = wfValue(field)
		case "with":
			step.with = wfScalarMap(field.body)
		case "continue-on-error":
			step.continueOnError = strings.EqualFold(field.value, "true")
		}
	}
	return step
}

// wfScalarMap reads a block of `key: value` entries into a map.
func wfScalarMap(block []wfLine) map[string]string {
	entries := wfMapping(block)
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		values[entry.key] = wfValue(entry)
	}
	return values
}

// wfStringList reads a value that YAML may write either inline (`needs: test`,
// `needs: [test, build]`) or as a block sequence.
func wfStringList(entry wfEntry) []string {
	if entry.value != "" {
		parts := strings.Split(strings.Trim(entry.value, "[]"), ",")
		items := make([]string, 0, len(parts))
		for _, part := range parts {
			if item := wfScalarText(part); item != "" {
				items = append(items, item)
			}
		}
		return items
	}

	items := make([]string, 0, len(entry.body))
	for _, line := range entry.body {
		trimmed := strings.TrimSpace(line.text)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		if item := wfScalarText(strings.TrimPrefix(trimmed, "- ")); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func wfSplitLines(src string) []wfLine {
	raw := strings.Split(src, "\n")
	lines := make([]wfLine, 0, len(raw))
	for i, text := range raw {
		lines = append(lines, wfLine{text: strings.TrimRight(text, "\r"), num: i + 1})
	}
	return lines
}

// wfStructural reports whether a line carries structure. Blank lines and
// whole-line comments belong to whichever block surrounds them.
func wfStructural(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed != "" && !strings.HasPrefix(trimmed, "#")
}

func wfIndent(text string) int {
	return len(text) - len(strings.TrimLeft(text, " "))
}

// wfBaseIndent is the indentation of the block's own entries: the smallest
// indentation among its structural lines. Everything deeper belongs to an
// entry's body.
func wfBaseIndent(block []wfLine) int {
	base := -1
	for _, line := range block {
		if !wfStructural(line.text) {
			continue
		}
		if indent := wfIndent(line.text); base < 0 || indent < base {
			base = indent
		}
	}
	return base
}

// wfMapping splits a block into its `key: value` entries.
func wfMapping(block []wfLine) []wfEntry {
	base := wfBaseIndent(block)
	if base < 0 {
		return nil
	}

	starts := make([]int, 0, len(block))
	entries := make([]wfEntry, 0, len(block))
	for i, line := range block {
		if !wfStructural(line.text) || wfIndent(line.text) != base {
			continue
		}
		match := wfKeyPattern.FindStringSubmatch(strings.TrimSpace(line.text))
		if match == nil {
			continue
		}
		starts = append(starts, i)
		entries = append(entries, wfEntry{key: match[1], value: wfScalarText(match[2]), num: line.num})
	}

	for i := range entries {
		end := len(block)
		if i+1 < len(entries) {
			end = starts[i+1]
		}
		entries[i].body = block[starts[i]+1 : end]
	}
	return entries
}

// wfSequence splits a block into the items of a YAML sequence. Each item is
// returned with its leading "- " rewritten as indentation, so the item's body
// reads as a plain mapping.
func wfSequence(block []wfLine) [][]wfLine {
	base := wfBaseIndent(block)
	if base < 0 {
		return nil
	}

	normalised := make([]wfLine, len(block))
	copy(normalised, block)

	items := make([][]wfLine, 0, len(block))
	start := -1
	for i, line := range block {
		if !wfStructural(line.text) || wfIndent(line.text) != base ||
			!strings.HasPrefix(strings.TrimSpace(line.text), "- ") {
			continue
		}
		normalised[i] = wfLine{text: strings.Replace(line.text, "- ", "  ", 1), num: line.num}
		if start >= 0 {
			items = append(items, normalised[start:i])
		}
		start = i
	}
	if start >= 0 {
		items = append(items, normalised[start:])
	}
	return items
}

// wfValue returns an entry's value, flattening a block scalar into its text.
//
// A block scalar's content indentation is the indentation of its first
// non-blank line, and the scalar ends at the first non-blank line indented less
// than that, as YAML 1.2 § 8.1.1.1 defines. A comment is such a line too: one
// indented less than the content — the comment that introduces the next step,
// say — is not part of the script, while a `#` line at the content's indentation
// is shell text inside it. The body of an entry runs to the next key of its
// mapping, so without that end a less-indented comment following the scalar
// was read into it, cut at the content's column into text that is neither a
// comment nor a command.
func wfValue(entry wfEntry) string {
	if entry.value == "" || (entry.value[0] != '|' && entry.value[0] != '>') {
		return entry.value
	}

	base := -1
	lines := make([]string, 0, len(entry.body))
	for _, line := range entry.body {
		if strings.TrimSpace(line.text) == "" {
			lines = append(lines, "")
			continue
		}
		indent := wfIndent(line.text)
		if base < 0 {
			base = indent
		}
		if indent < base {
			break
		}
		lines = append(lines, line.text[base:])
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// wfScalarText normalises an inline scalar: it drops a trailing comment and
// removes surrounding quotes.
func wfScalarText(value string) string {
	value = strings.TrimSpace(value)
	if cut := wfCommentIndex(value); cut >= 0 {
		value = strings.TrimSpace(value[:cut])
	}

	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			value = value[1 : len(value)-1]
		}
	}
	return value
}

// wfCommentIndex returns the offset of a trailing YAML comment, or -1. A "#"
// opens a comment only when it follows whitespace and lies outside quotes.
func wfCommentIndex(value string) int {
	inSingle, inDouble := false, false
	for i := range len(value) {
		switch value[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && i > 0 && (value[i-1] == ' ' || value[i-1] == '\t') {
				return i
			}
		}
	}
	return -1
}
