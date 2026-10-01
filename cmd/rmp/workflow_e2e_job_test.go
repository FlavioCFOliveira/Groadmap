package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The tests in this file hold both workflows to SPEC/BUILD.md § The End-to-End
// Suite Is a Required Pipeline Job. The suite is not a validation gate, so the
// gate tests of workflow_gates_test.go do not see it; without these tests a
// workflow could drop the job, run it after the build, let it fail without
// failing the run, or test a binary it did not build, and every gate test would
// stay green.
//
// e2eJobProblems states every rule once, and
// TestEndToEndJobCheckRejectsEveryDivergence proves that each rule rejects the
// departure it exists for, so the tests over the real workflows below cannot
// pass vacuously.

// The steps of the `e2e` job, as SPEC/BUILD.md § The End-to-End Suite Is a
// Required Pipeline Job lists them, in its order.
const (
	e2eJobName        = "End-to-End Tests"
	e2eTimeoutMinutes = "60"
	e2eCheckoutAction = "actions/checkout@"
	e2eSetupGoAction  = "actions/setup-go@"
	e2ePythonAction   = "actions/setup-python@"
	e2eBuildCommand   = "go build -o ./bin/rmp ./cmd/rmp"
	e2eSuiteCommand   = "python3 -u tests/run_tests.py"
	e2eStepCount      = 5
)

// e2eMinimumPython is the oldest interpreter the suite compiles under: its
// sources use the f-string syntax Python 3.12 introduced.
var e2eMinimumPython = [2]int{3, 12}

// pythonVersionForm matches a `python-version` input that names one release,
// such as 3.12 or 3.12.4. A range, a wildcard or an alias names no release.
var pythonVersionForm = regexp.MustCompile(`^([0-9]+)\.([0-9]+)(?:\.[0-9]+)?$`)

// TestWorkflowsRunTheEndToEndJob holds each workflow's `e2e` job to its
// specified shape.
func TestWorkflowsRunTheEndToEndJob(t *testing.T) {
	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			wf := parseWorkflow(t, p.path)
			for _, problem := range e2eJobProblems(p.rel(), &wf, p.gateJob, e2eJobID) {
				t.Error(problem)
			}
		})
	}
}

// TestWorkflowsSetUpTheSamePython holds the `python-version` input of the `e2e`
// job to the rule that it is written only in the two workflows and is the same
// in both, so the suite runs under one interpreter in CI and at release.
func TestWorkflowsSetUpTheSamePython(t *testing.T) {
	sites := make([]pythonSite, 0, 2)
	for _, p := range pipelines() {
		wf := parseWorkflow(t, p.path)
		sites = append(sites, pythonSite{rel: p.rel(), version: e2ePythonVersion(&wf, e2eJobID)})
	}
	for _, problem := range pythonVersionProblems(sites) {
		t.Error(problem)
	}
}

// e2eJobProblems returns every way the job e2eID of a workflow departs from
// SPEC/BUILD.md § The End-to-End Suite Is a Required Pipeline Job. gateID names
// the workflow's gate job, whose runner image the end-to-end job shares.
func e2eJobProblems(rel string, wf *wfWorkflow, gateID, e2eID string) []string {
	const section = "SPEC/BUILD.md § The End-to-End Suite Is a Required Pipeline Job"

	job, ok := wf.jobs[e2eID]
	if !ok {
		return []string{fmt.Sprintf("%s: no job %q; the workflow declares %v. %s requires each workflow to "+
			"declare a job with that ID, which runs the end-to-end suite and which the build job needs.",
			rel, e2eID, wf.jobOrder, section)}
	}

	problems := make([]string, 0, 4)
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("%s: job %q ", rel, e2eID)+fmt.Sprintf(format, args...))
	}

	if job.name != e2eJobName {
		report("is named %q, but %s names it %q.", job.name, section, e2eJobName)
	}
	if !strings.HasPrefix(job.runsOn, "ubuntu-") {
		report("runs on %q, which is not a Linux runner. %s requires the Linux runner image of the gate job.",
			job.runsOn, section)
	}
	if gate, ok := wf.jobs[gateID]; ok && job.runsOn != gate.runsOn {
		report("runs on %q, but the gate job %q runs on %q. %s requires the same runner image.",
			job.runsOn, gateID, gate.runsOn, section)
	}
	if job.timeoutMinutes != e2eTimeoutMinutes {
		report("declares timeout-minutes %q, but %s requires `timeout-minutes: %s`, so a module that hangs "+
			"ends the job within the hour instead of holding it for the platform's six-hour limit.",
			job.timeoutMinutes, section, e2eTimeoutMinutes)
	}
	if len(job.needs) != 0 {
		report("declares needs: %v. %s requires it to declare none, so it runs beside the gate job rather "+
			"than after it.", job.needs, section)
	}
	if job.continueOnError != "" && !strings.EqualFold(job.continueOnError, "false") {
		report("carries `continue-on-error: %s`, so the run passes when the suite fails. %s forbids it.",
			job.continueOnError, section)
	}
	for _, scope := range sortedKeys(permissionScopes(job.permissions)) {
		level := job.permissions[scope]
		if level == "none" || (scope == "contents" && level == "read") {
			continue
		}
		report("grants itself `%s: %s`. %s allows no permission above the workflow-level `contents: read`.",
			scope, level, section)
	}

	problems = append(problems, e2eStepProblems(rel, e2eID, job.steps, section)...)
	return problems
}

// e2eStepProblems checks the job's steps: exactly the five the specification
// lists, in its order, none of which may fail without failing the job.
func e2eStepProblems(rel, e2eID string, steps []wfStep, section string) []string {
	problems := make([]string, 0, 2)
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("%s: job %q ", rel, e2eID)+fmt.Sprintf(format, args...))
	}

	for _, step := range steps {
		if step.continueOnError {
			report("step %q carries `continue-on-error`, so the job passes when that step fails. %s forbids it "+
				"on every step of the job.", step.name, section)
		}
	}

	if len(steps) != e2eStepCount {
		described := make([]string, 0, len(steps))
		for _, step := range steps {
			described = append(described, e2eDescribeStep(step))
		}
		report("takes %d steps (%s), but %s lists exactly %d, in this order: check out the repository, set "+
			"up Go from go.mod, set up Python, run %q, and run %q. A step beyond them installs, tests for or "+
			"restores something the job must not; one missing leaves the suite unrun or run on a binary "+
			"this run did not build.",
			len(steps), strings.Join(described, "; "), section, e2eStepCount, e2eBuildCommand, e2eSuiteCommand)
		return problems
	}

	checkout, goSetup, pythonSetup, build, suite := steps[0], steps[1], steps[2], steps[3], steps[4]
	if !strings.HasPrefix(checkout.uses, e2eCheckoutAction) {
		report("step 1 is %s, but %s makes step 1 the checkout (%s<version>).",
			e2eDescribeStep(checkout), section, e2eCheckoutAction)
	}
	if !strings.HasPrefix(goSetup.uses, e2eSetupGoAction) || goSetup.with["go-version-file"] != "go.mod" ||
		goSetup.with["go-version"] != "" {
		report("step 2 is %s with go-version-file %q, but %s makes step 2 the Go set-up from go.mod "+
			"(%s<version> with `go-version-file: go.mod` and no other version).",
			e2eDescribeStep(goSetup), goSetup.with["go-version-file"], section, e2eSetupGoAction)
	}
	if pin, ok := strings.CutPrefix(pythonSetup.uses, e2ePythonAction); !ok || !exactVersion.MatchString(pin) {
		report("step 3 is %s, but %s makes step 3 the Python set-up through %s pinned to an exact version, "+
			"like every action the workflows use.", e2eDescribeStep(pythonSetup), section, e2ePythonAction)
	}
	if version := pythonSetup.with["python-version"]; !pythonVersionAtLeastMinimum(version) {
		report("step 3 passes python-version %q, but %s requires an input that names Python %d.%d or a later "+
			"release: the suite's sources do not compile under an earlier interpreter.",
			version, section, e2eMinimumPython[0], e2eMinimumPython[1])
	}
	if strings.TrimSpace(build.run) != e2eBuildCommand || build.uses != "" {
		report("step 4 is %s, but %s makes step 4 the build of the tested binary, `%s`, in a step of its own "+
			"before the suite.", e2eDescribeStep(build), section, e2eBuildCommand)
	}
	if strings.TrimSpace(suite.run) != e2eSuiteCommand || suite.uses != "" {
		report("step 5 is %s, but %s makes step 5 `%s`: the default module set, unbuffered.",
			e2eDescribeStep(suite), section, e2eSuiteCommand)
	}
	return problems
}

// e2eDescribeStep renders a step for a failure message: the action it uses, or
// the script it runs.
func e2eDescribeStep(step wfStep) string {
	if step.uses != "" {
		return fmt.Sprintf("%q (uses %s)", step.name, step.uses)
	}
	return fmt.Sprintf("%q (runs %q)", step.name, strings.TrimSpace(step.run))
}

// permissionScopes returns the scopes of a permissions map as a set, for
// sortedKeys.
func permissionScopes(permissions map[string]string) map[string]bool {
	scopes := make(map[string]bool, len(permissions))
	for scope := range permissions {
		scopes[scope] = true
	}
	return scopes
}

// pythonVersionAtLeastMinimum reports whether a `python-version` input names
// one release no older than e2eMinimumPython.
func pythonVersionAtLeastMinimum(version string) bool {
	match := pythonVersionForm.FindStringSubmatch(version)
	if match == nil {
		return false
	}
	major, errMajor := strconv.Atoi(match[1])
	minor, errMinor := strconv.Atoi(match[2])
	if errMajor != nil || errMinor != nil {
		return false
	}
	return major > e2eMinimumPython[0] || (major == e2eMinimumPython[0] && minor >= e2eMinimumPython[1])
}

// e2ePythonVersion returns the `python-version` input of every step of the job
// that uses the setup-python action, joined, so that a job with no such step,
// or with two, is visible to the comparison.
func e2ePythonVersion(wf *wfWorkflow, e2eID string) []string {
	versions := make([]string, 0, 1)
	for _, step := range wf.jobs[e2eID].steps {
		if strings.HasPrefix(step.uses, e2ePythonAction) {
			versions = append(versions, step.with["python-version"])
		}
	}
	return versions
}

// pythonSite is where one workflow names the interpreter of its `e2e` job.
type pythonSite struct {
	rel     string
	version []string // the python-version input of each setup-python step
}

// pythonVersionProblems returns every way the workflows depart from the rule
// that both name the same one `python-version`.
func pythonVersionProblems(sites []pythonSite) []string {
	const section = "SPEC/BUILD.md § The End-to-End Suite Is a Required Pipeline Job"

	problems := make([]string, 0, 1)
	first, firstRel := "", ""
	for _, site := range sites {
		if len(site.version) != 1 {
			problems = append(problems, fmt.Sprintf("%s: the e2e job sets up Python %d times (python-version %q), "+
				"but %s requires it once, with the python-version input both workflows share.",
				site.rel, len(site.version), site.version, section))
			continue
		}
		if firstRel == "" {
			first, firstRel = site.version[0], site.rel
			continue
		}
		if site.version[0] != first {
			problems = append(problems, fmt.Sprintf("%s passes python-version %q to the e2e job, but %s passes "+
				"%q. %s writes that value only in the two workflows and requires both to name the same one, so "+
				"the suite runs under the same interpreter in CI and at release.",
				site.rel, site.version[0], firstRel, first, section))
		}
	}
	return problems
}

// e2eFixture describes a workflow with a gate job and an `e2e` job, which
// e2eFixtureSource renders as YAML. Each case of
// TestEndToEndJobCheckRejectsEveryDivergence departs from the conforming
// fixture in one respect.
type e2eFixture struct {
	permissions     string // the job's permissions block, one `scope: level` per line
	name            string
	runsOn          string
	timeout         string
	needs           string
	continueOnError string
	steps           []string // each step's YAML, its lines indented as a sequence item's body
	omitJob         bool
}

// conformingE2EFixture returns the fixture every rule accepts: the job the
// specification describes.
func conformingE2EFixture() e2eFixture {
	return e2eFixture{
		name:    e2eJobName,
		runsOn:  "ubuntu-latest",
		timeout: e2eTimeoutMinutes,
		steps: []string{
			e2eFixtureCheckout,
			e2eFixtureSetupGo,
			e2eFixtureSetupPython("v7.0.0", "'3.12'"),
			e2eFixtureRun("Build rmp", e2eBuildCommand),
			e2eFixtureRun("Run end-to-end tests", e2eSuiteCommand),
		},
	}
}

const (
	e2eFixtureCheckout = "name: Checkout code\nuses: actions/checkout@v7.0.1"
	e2eFixtureSetupGo  = "name: Set up Go\nuses: actions/setup-go@v7.0.0\nwith:\n  go-version-file: go.mod"
)

func e2eFixtureSetupPython(pin, version string) string {
	return "name: Set up Python\nuses: " + e2ePythonAction + pin + "\nwith:\n  python-version: " + version
}

func e2eFixtureRun(name, command string) string {
	return "name: " + name + "\nrun: " + command
}

// e2eFixtureSource renders the fixture as a workflow.
func e2eFixtureSource(f *e2eFixture) string {
	var src strings.Builder
	src.WriteString("on: push\npermissions:\n  contents: read\njobs:\n")
	src.WriteString("  test:\n    name: Test\n    runs-on: ubuntu-latest\n    steps:\n      - name: Run go vet\n" +
		"        run: go vet ./...\n")
	if f.omitJob {
		return src.String()
	}

	src.WriteString("  e2e:\n")
	for _, field := range []struct{ key, value string }{
		{"name", f.name},
		{"runs-on", f.runsOn},
		{"timeout-minutes", f.timeout},
		{"needs", f.needs},
		{"continue-on-error", f.continueOnError},
	} {
		if field.value != "" {
			fmt.Fprintf(&src, "    %s: %s\n", field.key, field.value)
		}
	}
	if f.permissions != "" {
		src.WriteString("    permissions:\n")
		for line := range strings.Lines(f.permissions) {
			src.WriteString("      " + line)
		}
		src.WriteString("\n")
	}
	src.WriteString("    steps:\n")
	for _, step := range f.steps {
		for i, line := range strings.Split(step, "\n") {
			prefix := "        "
			if i == 0 {
				prefix = "      - "
			}
			src.WriteString(prefix + line + "\n")
		}
		src.WriteString("\n")
	}
	return src.String()
}

// TestEndToEndJobCheckRejectsEveryDivergence proves that e2eJobProblems, which
// TestWorkflowsRunTheEndToEndJob relies on, accepts the specified job and
// rejects each departure from it.
func TestEndToEndJobCheckRejectsEveryDivergence(t *testing.T) {
	type mutation func(*e2eFixture)
	withStep := func(index int, step string) mutation {
		return func(f *e2eFixture) { f.steps[index] = step }
	}

	cases := []struct {
		mutate   mutation
		name     string
		problems []string // a fragment of each problem expected, in order
	}{
		{name: "the specified job", mutate: func(*e2eFixture) {}},
		{
			name:     "no e2e job",
			mutate:   func(f *e2eFixture) { f.omitJob = true },
			problems: []string{`no job "e2e"`},
		},
		{
			name:     "another name",
			mutate:   func(f *e2eFixture) { f.name = "E2E" },
			problems: []string{`is named "E2E", but`},
		},
		{
			name:   "a runner that is not Linux",
			mutate: func(f *e2eFixture) { f.runsOn = "macos-latest" },
			problems: []string{`runs on "macos-latest", which is not a Linux runner`,
				`runs on "macos-latest", but the gate job "test" runs on "ubuntu-latest"`},
		},
		{
			name:     "a Linux runner other than the gate job's",
			mutate:   func(f *e2eFixture) { f.runsOn = "ubuntu-22.04" },
			problems: []string{`runs on "ubuntu-22.04", but the gate job "test" runs on "ubuntu-latest"`},
		},
		{
			name:     "no timeout",
			mutate:   func(f *e2eFixture) { f.timeout = "" },
			problems: []string{`declares timeout-minutes "", but`},
		},
		{
			name:     "another timeout",
			mutate:   func(f *e2eFixture) { f.timeout = "360" },
			problems: []string{`declares timeout-minutes "360", but`},
		},
		{
			name:     "a job that waits for the gates",
			mutate:   func(f *e2eFixture) { f.needs = "test" },
			problems: []string{"declares needs: [test]"},
		},
		{
			name:     "a job allowed to fail",
			mutate:   func(f *e2eFixture) { f.continueOnError = "true" },
			problems: []string{"carries `continue-on-error: true`"},
		},
		{
			name:     "a write permission",
			mutate:   func(f *e2eFixture) { f.permissions = "contents: write" },
			problems: []string{"grants itself `contents: write`"},
		},
		{
			name:     "a permission beyond contents",
			mutate:   func(f *e2eFixture) { f.permissions = "contents: read\nissues: read" },
			problems: []string{"grants itself `issues: read`"},
		},
		{
			name: "a suite step allowed to fail",
			mutate: withStep(4, e2eFixtureRun("Run end-to-end tests", e2eSuiteCommand)+
				"\ncontinue-on-error: true"),
			problems: []string{`step "Run end-to-end tests" carries ` + "`continue-on-error`"},
		},
		{
			name:     "a floating setup-python pin",
			mutate:   withStep(2, e2eFixtureSetupPython("v7", "'3.12'")),
			problems: []string{"through actions/setup-python@ pinned to an exact version"},
		},
		{
			name:     "a Python older than 3.12",
			mutate:   withStep(2, e2eFixtureSetupPython("v7.0.0", "'3.11'")),
			problems: []string{`passes python-version "3.11"`},
		},
		{
			name:     "a Python range instead of a release",
			mutate:   withStep(2, e2eFixtureSetupPython("v7.0.0", "'3.x'")),
			problems: []string{`passes python-version "3.x"`},
		},
		{
			name:     "no python-version input",
			mutate:   withStep(2, "name: Set up Python\nuses: actions/setup-python@v7.0.0"),
			problems: []string{`passes python-version ""`},
		},
		{
			name:     "Go set up from a literal version",
			mutate:   withStep(1, "name: Set up Go\nuses: actions/setup-go@v7.0.0\nwith:\n  go-version: '1.27'"),
			problems: []string{`with go-version-file ""`},
		},
		{
			name:     "no checkout",
			mutate:   withStep(0, "name: Show the runner\nrun: uname -a"),
			problems: []string{"step 1 is \"Show the runner\""},
		},
		{
			name:     "a build step that builds something else",
			mutate:   withStep(3, e2eFixtureRun("Build rmp", "make build")),
			problems: []string{`step 4 is "Build rmp" (runs "make build")`},
		},
		{
			name:     "the binary restored from an artefact instead of built",
			mutate:   withStep(3, "name: Download rmp\nuses: actions/download-artifact@v8.0.1\nwith:\n  name: rmp"),
			problems: []string{`step 4 is "Download rmp" (uses actions/download-artifact@v8.0.1)`},
		},
		{
			name:     "the suite run buffered",
			mutate:   withStep(4, e2eFixtureRun("Run end-to-end tests", "python3 tests/run_tests.py")),
			problems: []string{`step 5 is "Run end-to-end tests" (runs "python3 tests/run_tests.py")`},
		},
		{
			name:     "the stress modules selected",
			mutate:   withStep(4, e2eFixtureRun("Run end-to-end tests", e2eSuiteCommand+" --all")),
			problems: []string{`(runs "python3 -u tests/run_tests.py --all")`},
		},
		{
			name: "the suite skipped when Python is absent",
			mutate: withStep(4, e2eFixtureRun("Run end-to-end tests",
				"command -v python3 && "+e2eSuiteCommand+" || true")),
			problems: []string{`step 5 is "Run end-to-end tests"`},
		},
		{
			name: "build and suite in the wrong order",
			mutate: func(f *e2eFixture) {
				f.steps[3], f.steps[4] = f.steps[4], f.steps[3]
			},
			problems: []string{`step 4 is "Run end-to-end tests"`, `step 5 is "Build rmp"`},
		},
		{
			name: "no build step",
			mutate: func(f *e2eFixture) {
				f.steps = append(f.steps[:3], f.steps[4])
			},
			problems: []string{"takes 4 steps"},
		},
		{
			name: "a cache restored before the suite",
			mutate: func(f *e2eFixture) {
				cache := "name: Restore rmp\nuses: actions/cache@v5.0.0\nwith:\n  path: bin/rmp\n  key: rmp"
				f.steps = append(f.steps[:4], append([]string{cache}, f.steps[4:]...)...)
			},
			problems: []string{"takes 6 steps"},
		},
		{
			name: "a tool installed beside the suite",
			mutate: func(f *e2eFixture) {
				f.steps = append(f.steps, e2eFixtureRun("Install jq", "sudo apt-get install -y jq"))
			},
			problems: []string{"takes 6 steps"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := conformingE2EFixture()
			tc.mutate(&fixture)
			src := e2eFixtureSource(&fixture)
			wf := parseWorkflowSource(src, "fixture.yml")
			got := e2eJobProblems("fixture.yml", &wf, "test", "e2e")
			if len(got) != len(tc.problems) {
				t.Logf("fixture:\n%s", src)
			}
			assertProblems(t, got, tc.problems)
		})
	}
}

// TestPythonVersionCheckRejectsDivergentWorkflows proves that
// pythonVersionProblems, which TestWorkflowsSetUpTheSamePython relies on,
// accepts two workflows that name the same interpreter and rejects the rest.
// The versions are read back from parsed fixtures, so the path from the
// `python-version` input to the comparison is exercised too.
func TestPythonVersionCheckRejectsDivergentWorkflows(t *testing.T) {
	cases := []struct {
		name     string
		ci       []string // the python-version of each setup-python step in the CI fixture
		release  []string // the same, in the release fixture
		problems []string
	}{
		{name: "the same version in both", ci: []string{"'3.12'"}, release: []string{"'3.12'"}},
		{
			name:     "different versions",
			ci:       []string{"'3.12'"},
			release:  []string{"'3.13'"},
			problems: []string{`release.yml passes python-version "3.13" to the e2e job, but ci.yml passes "3.12"`},
		},
		{
			name:     "the same release written differently",
			ci:       []string{"'3.12'"},
			release:  []string{"'3.12.4'"},
			problems: []string{`release.yml passes python-version "3.12.4" to the e2e job, but ci.yml passes "3.12"`},
		},
		{
			name:     "no Python set up in one workflow",
			ci:       []string{"'3.12'"},
			problems: []string{"release.yml: the e2e job sets up Python 0 times"},
		},
		{
			name:     "Python set up twice in one workflow",
			ci:       []string{"'3.12'", "'3.13'"},
			release:  []string{"'3.12'"},
			problems: []string{"ci.yml: the e2e job sets up Python 2 times"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sites := make([]pythonSite, 0, 2)
			for _, site := range []struct {
				rel      string
				versions []string
			}{{"ci.yml", tc.ci}, {"release.yml", tc.release}} {
				fixture := conformingE2EFixture()
				fixture.steps = fixture.steps[:2]
				for _, version := range site.versions {
					fixture.steps = append(fixture.steps, e2eFixtureSetupPython("v7.0.0", version))
				}
				wf := parseWorkflowSource(e2eFixtureSource(&fixture), site.rel)
				sites = append(sites, pythonSite{rel: site.rel, version: e2ePythonVersion(&wf, "e2e")})
			}
			assertProblems(t, pythonVersionProblems(sites), tc.problems)
		})
	}
}
