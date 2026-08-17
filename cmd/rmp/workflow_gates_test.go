package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The tests in this file pin the two GitHub Actions workflows to
// SPEC/BUILD.md. They exist because the release workflow once ran only `fmt`,
// `vet` and the tests: neither the linter nor the security scan ran anywhere in
// the release pipeline, so a `v*` tag could publish binaries that no
// `golangci-lint` and no `gosec` run had ever inspected. Nothing detected the
// gap, and ten consecutive releases recorded the security gate as "skipped, per
// project policy" — a policy SPEC/BUILD.md § A Missing Tool Is a Failure, Never
// a Skip explicitly forbids and that never existed.
//
// The specified state has since been restored in both YAML files. These tests
// are the gate that keeps it there: every assertion below reads its expected
// value out of SPEC/BUILD.md rather than repeating it, so the specification
// stays the single authority and a pin raised in the specification alone fails
// the suite. This mirrors TestSupportedBuildTargetsMatchSpec in
// build_targets_test.go, which pins the build matrix to the same document.

// ciWorkflowPath and releaseWorkflowPath locate the two workflow files from
// this package's directory, which is where `go test` sets the working
// directory. specBuildPath, declared in build_targets_test.go, locates the
// specification the same way.
const (
	ciWorkflowPath      = "../../.github/workflows/ci.yml"
	releaseWorkflowPath = "../../.github/workflows/release.yml"
	makefilePath        = "../../Makefile"
)

// pipeline names the three jobs SPEC/BUILD.md § GitHub Actions Workflow
// declares for one workflow: the job that runs the gates, the job that is the
// `build` gate, and the job that publishes artefacts.
type pipeline struct {
	path       string
	gateJob    string
	buildJob   string
	publishJob string
}

// pipelines returns the two workflows under test, described in the terms
// SPEC/BUILD.md § GitHub Actions Workflow uses for each.
func pipelines() []pipeline {
	return []pipeline{
		{path: ciWorkflowPath, gateJob: "test", buildJob: "build", publishJob: "dev-release"},
		{path: releaseWorkflowPath, gateJob: "test", buildJob: "build", publishJob: "release"},
	}
}

// rel is the repository-relative path of the workflow, for failure messages.
func (p pipeline) rel() string { return strings.TrimPrefix(p.path, "../../") }

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

// TestWorkflowToolPinsMatchSpec proves the third rule of SPEC/BUILD.md §
// Static Analysis: each tool's version appears in exactly three places — its
// own section of the specification and the two workflows — and all three MUST
// name the same version. The expected versions are read out of the
// specification, so raising a pin there alone fails this test, which is the
// point: a gate whose tool version differs between two pipelines is not the
// same gate in both.
func TestWorkflowToolPinsMatchSpec(t *testing.T) {
	facts := loadSpecGates(t)

	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			gate := loadGateJob(t, p)
			sites := gate.resolveGates(t, facts)

			// gosec: the workflow must install the pinned scanner with the
			// exact command SPEC/BUILD.md § Security Scan: gosec documents.
			if _, ok := sites[gateGosecInstall]; !ok {
				t.Errorf("%s: job %q does not install gosec at the pinned version. "+
					"SPEC/BUILD.md § Security Scan: gosec pins it to %s and documents the install command %q; "+
					"§ A Missing Tool Is a Failure, Never a Skip requires each workflow to install the tool "+
					"in the job that runs the gate.",
					p.rel(), p.gateJob, facts.gosecVersion, facts.gosecInstall)
			}

			// golangci-lint: the pinned linter version is the `version` input
			// passed to the action, a pin distinct from the action's own.
			lintStep, ok := sites[gateLint]
			if !ok {
				return // resolveGates has already reported the missing lint gate.
			}
			step := gate.job.steps[lintStep]
			if got := step.with["version"]; got != facts.lintVersion {
				t.Errorf("%s: step %q passes version %q to the golangci-lint action, but SPEC/BUILD.md "+
					"§ Linter: golangci-lint pins the linter to %s. All three places that name this version "+
					"— the specification and both workflows — MUST agree (§ Static Analysis, "+
					"\"Where the pins live, and how they change\").",
					p.rel(), step.name, got, facts.lintVersion)
			}
			if want := "golangci/golangci-lint-action@" + facts.lintActionPin; step.uses != want {
				t.Errorf("%s: step %q uses %q, but SPEC/BUILD.md § Linter: golangci-lint names the action pin %q. "+
					"The action's version is a separate exact pin from the linter's, and neither substitutes "+
					"for the other.",
					p.rel(), step.name, step.uses, want)
			}
		})
	}
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
// in parallel with the gates, or independently of them.
func TestWorkflowJobsDependOnTheGates(t *testing.T) {
	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			wf := parseWorkflow(t, p.path)
			build := mustJob(t, wf, p.buildJob)
			publish := mustJob(t, wf, p.publishJob)

			if !slices.Contains(build.needs, p.gateJob) {
				t.Errorf("%s: the build job %q does not declare `needs: %s`, so it can build artefacts in "+
					"parallel with the gates (it declares needs: %v). SPEC/BUILD.md § Where the Gate Set Is "+
					"Enforced requires the build job to declare `needs:` on the gate job.",
					p.rel(), p.buildJob, p.gateJob, build.needs)
			}
			if !slices.Contains(publish.needs, p.buildJob) {
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
// `contents: read`, and exactly one job — the one that publishes — raises that
// to `contents: write`.
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
			if len(writers) != 1 || writers[0] != p.publishJob {
				t.Errorf("%s: the jobs holding `contents: write` are %v, but SPEC/BUILD.md § GitHub Actions "+
					"Workflow allows exactly one — %q, the job that publishes.",
					p.rel(), writers, p.publishJob)
			}

			for _, id := range []string{p.gateJob, p.buildJob} {
				for scope, level := range mustJob(t, wf, id).permissions {
					if level == "write" {
						t.Errorf("%s: job %q grants itself `%s: write`. SPEC/BUILD.md § GitHub Actions Workflow "+
							"states that the gate job and the build job read; neither may write.",
							p.rel(), id, scope)
					}
				}
			}
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

	raw, err := os.ReadFile(filepath.Clean(makefilePath))
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	prerequisites := makefileCheckTarget(t, string(raw))
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
				"eleven Primary Platforms in the same order.",
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

// -----------------------------------------------------------------------------
// What the specification says
// -----------------------------------------------------------------------------

// Gate identifiers used as keys in the map resolveGates returns. The first five
// are the gate targets SPEC/BUILD.md § Validation Gates names; gateGosecInstall
// is the install step § A Missing Tool Is a Failure, Never a Skip requires
// alongside the security gate.
const (
	gateFmt          = "fmt"
	gateVet          = "vet"
	gateLint         = "lint"
	gateTest         = "test"
	gateSecurity     = "security"
	gateGosecInstall = "gosec install"
)

// specGates is everything these tests read out of SPEC/BUILD.md. Nothing here
// is duplicated in the test source: the specification is the only place any of
// these values is written down.
type specGates struct {
	commands      map[string]string // gate target -> command, from § Validation Gates
	gosecInstall  string            // the install command § Security Scan: gosec documents
	gosecVersion  string            // the gosec pin
	lintVersion   string            // the golangci-lint pin
	lintActionPin string            // the pin on the golangci-lint action itself
}

// versionPattern matches a pinned version as SPEC/BUILD.md writes one.
const versionPattern = `v[0-9][0-9A-Za-z.+-]*`

var (
	specPinnedVersion = regexp.MustCompile(`The pinned version is \*\*(` + versionPattern + `)\*\*`)
	specGosecInstall  = regexp.MustCompile(`(?m)^go install github\.com/securego/gosec/v2/cmd/gosec@(` + versionPattern + `)$`)
	specLintInstall   = regexp.MustCompile(`(?m)^go install github\.com/golangci/golangci-lint/v2/cmd/golangci-lint@(` + versionPattern + `)$`)
	specGosecPreamble = regexp.MustCompile("`gosec (" + versionPattern + ")`")
	specLintPreamble  = regexp.MustCompile("`golangci-lint (" + versionPattern + ")`")
	specLintInput     = regexp.MustCompile("`version: (" + versionPattern + ")`")
	specLintAction    = regexp.MustCompile(`golangci/golangci-lint-action@(` + versionPattern + `)`)
)

// loadSpecGates reads the gate set and both tool pins out of SPEC/BUILD.md. It
// also checks the specification against itself: a version is named in several
// places within the document, and every one of them must agree before the
// workflows can be measured against it.
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

	install := specGosecInstall.FindStringSubmatch(gosecSection)
	if install == nil {
		t.Fatalf("SPEC/BUILD.md § Security Scan: gosec no longer documents a `go install ...@version` command; " +
			"this test cannot check that the workflows install the pinned scanner")
	}

	facts := specGates{
		commands:     parseValidationGates(t, spec),
		gosecInstall: install[0],
		gosecVersion: specVersion(t, gosecSection, "§ Security Scan: gosec", specPinnedVersion),
		lintVersion:  specVersion(t, lintSection, "§ Linter: golangci-lint", specPinnedVersion),
		lintActionPin: specVersion(t, lintSection, "§ Linter: golangci-lint (the action's own pin)",
			specLintAction),
	}

	// The specification names each version more than once. If those statements
	// ever disagree, there is no single expected value to hold the workflows
	// to, and the specification must be repaired before this test can run.
	agree := []struct {
		pattern *regexp.Regexp
		section string
		name    string
		want    string
	}{
		{specGosecInstall, gosecSection, "§ Security Scan: gosec, install command", facts.gosecVersion},
		{specGosecPreamble, preamble, "§ Static Analysis preamble (gosec)", facts.gosecVersion},
		{specLintInstall, lintSection, "§ Linter: golangci-lint, install command", facts.lintVersion},
		{specLintPreamble, preamble, "§ Static Analysis preamble (golangci-lint)", facts.lintVersion},
		{specLintInput, lintSection, "§ Linter: golangci-lint, action input", facts.lintVersion},
	}
	for _, check := range agree {
		if got := specVersion(t, check.section, check.name, check.pattern); got != check.want {
			t.Fatalf("SPEC/BUILD.md contradicts itself: %s names version %s, but the section's pinned version "+
				"is %s. § Static Analysis requires one version per tool everywhere it appears.",
				check.name, got, check.want)
		}
	}

	return facts
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

// specVersion extracts a version from a section of the specification and
// requires every occurrence of the pattern in that section to name the same
// one.
func specVersion(t *testing.T, section, name string, pattern *regexp.Regexp) string {
	t.Helper()

	matches := pattern.FindAllStringSubmatch(section, -1)
	if len(matches) == 0 {
		t.Fatalf("SPEC/BUILD.md %s no longer names a version matching %s; "+
			"this test cannot hold the workflows to a pin the specification does not state",
			name, pattern)
	}
	version := matches[0][1]
	for _, match := range matches[1:] {
		if match[1] != version {
			t.Fatalf("SPEC/BUILD.md %s names two different versions, %s and %s; "+
				"§ Static Analysis requires one version per tool", name, version, match[1])
		}
	}
	return version
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

	gate := &gateJob{job: job, pipeline: p}
	for index, step := range job.steps {
		for line := range strings.Lines(step.run) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			gate.commands = append(gate.commands, wfCommand{text: line, step: index})
		}
	}
	return gate
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
		{gateGosecInstall, facts.gosecInstall},
	}
	for _, want := range exact {
		wanted := want.command
		if at := g.find(func(cmd string) bool { return cmd == wanted }); at >= 0 {
			found[want.gate] = g.commands[at].step
			continue
		}
		if want.gate == gateGosecInstall {
			continue // reported by the caller that cares about the pin
		}
		t.Errorf("%s: the %s gate is missing from job %q: no step runs %q. "+
			"SPEC/BUILD.md § Validation Gates defines the six-gate set and § Where the Gate Set Is Enforced "+
			"admits no per-pipeline exception — a `v*` tag must not publish a release unless every gate ran. "+
			"Publishing a release with any gate absent is the defect this test exists to prevent.",
			g.pipeline.rel(), want.gate, g.pipeline.gateJob, wanted)
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
		if strings.HasPrefix(step.uses, "golangci/golangci-lint-action@") {
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
	needs       []string
	steps       []wfStep
	matrix      []map[string]string // the entries of `strategy.matrix.include`
	permissions map[string]string
	id          string
	num         int
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

	wf := wfWorkflow{path: path, jobs: make(map[string]wfJob)}
	for _, entry := range wfMapping(wfSplitLines(string(raw))) {
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

	if len(wf.jobs) == 0 {
		t.Fatalf("%s: no jobs parsed. The workflow's layout changed in a way this reader does not "+
			"understand, so it can no longer prove the gates run; fix the reader rather than the workflow",
			strings.TrimPrefix(path, "../../"))
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
func wfValue(entry wfEntry) string {
	if entry.value == "" || (entry.value[0] != '|' && entry.value[0] != '>') {
		return entry.value
	}

	base := wfBaseIndent(entry.body)
	if base < 0 {
		return ""
	}
	lines := make([]string, 0, len(entry.body))
	for _, line := range entry.body {
		if len(line.text) > base {
			lines = append(lines, line.text[base:])
			continue
		}
		lines = append(lines, "")
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
