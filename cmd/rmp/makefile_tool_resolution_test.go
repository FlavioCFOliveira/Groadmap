package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestMakefileResolvesAndChecksThePinnedTools runs the Makefile's `lint` and
// `security` targets against fake tools and proves the rules of SPEC/BUILD.md §
// Local Tool Resolution by what each target runs and writes:
//
//   - the copy `go install` wrote is preferred to any copy on PATH, so a
//     shadowing copy of another release that PATH finds first is never run
//     while that copy exists, and a preferred copy of another release fails
//     the gate rather than giving way to a pinned copy on PATH;
//   - with no such copy, the first copy on PATH is the binary, and it is
//     version-checked exactly like the preferred one, so a shadow of another
//     release fails the gate even when the pinned version comes later on PATH;
//   - an override names the binary and is never resolved, and an empty one
//     names none;
//   - a failed check writes the three-part report: the mismatch or
//     no-readable-version line, every copy of the tool on PATH in PATH order
//     (or the line stating there is none), and the install command with the pin
//     in place of its placeholder.
//
// Every fake records each invocation that is not a version query, so "runs the
// pinned copy" and "does not run the tool" are read from what actually ran. The
// fake linter answers `version --short` as golangci-lint does. The fake scanner
// is a Go program built from a repository tagged with the version it must
// report, so `go version -m` reads that version from the `mod` line of its build
// information, as it does for a gosec built by `go install`; it answers nothing
// to `--version`, so a gate that asked it would find no version. The expected
// pins, variable names and install commands are read from the Makefile and from
// SPEC/BUILD.md, never restated here.
//
// make runs in a temporary directory with a PATH made only of the directories a
// case places tools in, plus one holding `go` and `awk`, so no tool installed on
// the machine running the test can take part.
func TestMakefileResolvesAndChecksThePinnedTools(t *testing.T) {
	pins := loadMakefilePins(t)
	installs := specToolInstallForms(t)
	lab := newToolLab(t, pins)

	lint := toolUnderTest{
		tool: "golangci-lint", variable: "GOLANGCI_LINT", target: "lint", pin: pins.lint,
		install: installs["golangci-lint"].with(pins.lint), runArgs: "run ./...",
	}
	gosec := toolUnderTest{
		tool: "gosec", variable: "GOSEC", target: "security", pin: pins.gosec,
		install: installs["gosec"].with(pins.gosec), runArgs: "-exclude-dir=.claude/worktrees ./...",
	}

	for _, tut := range []toolUnderTest{lint, gosec} {
		pinned, other := lab.fake(tut.tool, fakePinned), lab.fake(tut.tool, fakeOther)
		noVersion, notExecutable := lab.fake(tut.tool, fakeNoVersion), lab.fake(tut.tool, fakeNotExecutable)
		otherVersion := lab.version(tut.tool, fakeOther)

		cases := []resolutionCase{
			{
				name:   "the go install copy runs, not a failing shadow of another release first on PATH",
				gobin:  pinned,
				path:   []string{other, pinned},
				expect: ran(slotGOBIN),
			},
			{
				name:   "with no go install copy, the pinned first copy on PATH runs",
				path:   []string{pinned, other},
				expect: ran(slotPath(0)),
			},
			{
				name:   "with no go install copy, a first copy on PATH of another release fails, a pinned later one notwithstanding",
				path:   []string{other, pinned},
				expect: mismatch(slotPath(0), otherVersion, slotPath(0), slotPath(1)),
			},
			{
				name:   "a go install copy of another release fails and does not give way to a pinned copy on PATH",
				gobin:  other,
				path:   []string{pinned},
				expect: mismatch(slotGOBIN, otherVersion, slotPath(0)),
			},
			{
				name:   "a go install copy that cannot be run fails and does not give way to a pinned copy on PATH",
				gobin:  notExecutable,
				path:   []string{pinned},
				expect: unreadableAt(slotGOBIN, slotPath(0)),
			},
			{
				name:   "the go install directory on PATH is listed among the copies",
				gobin:  other,
				path:   []string{pinned, gobinOnPath},
				expect: mismatch(slotGOBIN, otherVersion, slotPath(0), slotGOBIN),
			},
			{
				name:   "no copy anywhere names the path the install command would write",
				expect: unreadableAt(slotGOBIN),
			},
			{
				name:   "a first copy on PATH with no readable version fails, a pinned later one notwithstanding",
				path:   []string{noVersion, pinned},
				expect: unreadableAt(slotPath(0), slotPath(0), slotPath(1)),
			},
			{
				name:   "with GOBIN empty, the bin directory of the first GOPATH entry is the go install directory",
				gopath: []string{pinned, other},
				path:   []string{other},
				expect: ran(slotGOPATH(0)),
			},
			{
				name:   "with neither GOBIN nor GOPATH, the first copy on PATH is still checked and run",
				noGo:   true,
				path:   []string{pinned},
				expect: ran(slotPath(0)),
			},
			{
				name:   "with neither GOBIN nor GOPATH and no copy on PATH, the variable is empty",
				noGo:   true,
				expect: unreadableAt(slotEmpty),
			},
			{
				name:     "a command-line override naming no file fails and does not fall back",
				gobin:    pinned,
				path:     []string{pinned},
				override: overrideArg(slotMissing),
				expect:   unreadableAt(slotMissing, slotPath(0)),
			},
			{
				name:     "an override set in the environment is checked",
				gobin:    pinned,
				path:     []string{other},
				override: overrideEnv(slotPath(0)),
				expect:   mismatch(slotPath(0), otherVersion, slotPath(0)),
			},
			{
				name:     "a command-line override takes precedence over the environment",
				gobin:    other,
				path:     []string{pinned, other},
				override: overrideBoth(slotPath(0), slotPath(1)),
				expect:   ran(slotPath(0)),
			},
			{
				name:     "an empty override names no binary",
				gobin:    pinned,
				path:     []string{pinned},
				override: overrideArg(slotEmpty),
				expect:   unreadableAt(slotEmpty, slotPath(0)),
			},
		}

		for _, tc := range cases {
			t.Run(tut.target+"/"+tc.name, func(t *testing.T) {
				runResolutionCase(t, lab, &tut, &tc)
			})
		}
	}
}

// toolUnderTest is one of the two gates the resolution rule governs.
type toolUnderTest struct {
	tool     string // the executable name
	variable string // the Makefile variable that names the binary
	target   string // the make target that runs the gate
	pin      string // the Makefile's pin
	install  string // the specification's install command, with the pin in place
	runArgs  string // the arguments the target passes to the tool
}

// fakeKind selects one of the fake binaries of a tool.
type fakeKind int

const (
	fakePinned        fakeKind = iota // reports the pin, and exits 0 when run
	fakeOther                         // reports another release, and fails whenever it is run
	fakeNoVersion                     // an executable file from which no readable version can be obtained
	fakeNotExecutable                 // the pinned fake without its execute permission
)

// gobinOnPath, placed in a case's PATH, stands for the go install directory
// itself rather than for a directory holding a copy of the tool.
const gobinOnPath = "<gobin>"

// slot names a path in a case's layout, resolved once the case's temporary
// directory exists.
type slot struct {
	kind  string // "gobin", "gopath", "path", "missing" or "empty"
	index int
}

var (
	slotGOBIN   = slot{kind: "gobin"}
	slotMissing = slot{kind: "missing"}
	slotEmpty   = slot{kind: "empty"}
)

func slotPath(i int) slot   { return slot{kind: "path", index: i} }
func slotGOPATH(i int) slot { return slot{kind: "gopath", index: i} }

// expectation is what a case must observe.
type expectation struct {
	checked   slot   // the binary named in the report's first line
	found     string // the version the mismatch line names; empty for no readable version
	ran       slot   // the binary that must run, when the gate passes
	listed    []slot // the copies the report lists, in PATH order
	passes    bool
	mismatchL bool // the first line is the mismatch line rather than the no-readable-version line
}

func ran(s slot) expectation { return expectation{passes: true, ran: s} }

func mismatch(checked slot, found string, listed ...slot) expectation {
	return expectation{checked: checked, found: found, listed: listed, mismatchL: true}
}

func unreadableAt(checked slot, listed ...slot) expectation {
	return expectation{checked: checked, listed: listed}
}

// override is how a case names the binary, when it does.
type override struct {
	arg, env       slot
	hasArg, hasEnv bool
}

func overrideArg(s slot) override { return override{arg: s, hasArg: true} }
func overrideEnv(s slot) override { return override{env: s, hasEnv: true} }
func overrideBoth(arg, env slot) override {
	return override{arg: arg, env: env, hasArg: true, hasEnv: true}
}

// resolutionCase is one machine layout and what the gate must do on it.
type resolutionCase struct {
	name     string
	gobin    string   // the fake installed in GOBIN; empty for none
	gopath   []string // with GOBIN empty, the fake installed in each GOPATH entry's bin
	path     []string // the fake in each PATH directory, in order (or gobinOnPath)
	override override
	expect   expectation
	noGo     bool // go reports neither a GOBIN nor a GOPATH
}

// layout is a case's directories, from which slots resolve to paths.
type layout struct {
	gobin    string
	gopaths  []string
	pathDirs []string
	missing  string
	tool     string
}

func (l *layout) resolve(s slot) string {
	switch s.kind {
	case "gobin":
		return filepath.Join(l.gobin, l.tool)
	case "gopath":
		return filepath.Join(l.gopaths[s.index], "bin", l.tool)
	case "path":
		return filepath.Join(l.pathDirs[s.index], l.tool)
	case "missing":
		return l.missing
	}
	return ""
}

// runResolutionCase lays out a case's fake tools, runs its target and checks
// the outcome.
func runResolutionCase(t *testing.T, lab *toolLab, tut *toolUnderTest, tc *resolutionCase) {
	t.Helper()

	root := t.TempDir()
	l := &layout{tool: tut.tool, missing: filepath.Join(root, "missing", tut.tool)}

	env := []string{
		"GOENV=" + filepath.Join(root, "goenv"),
		"GOTOOLCHAIN=local",
		"GOWORK=off",
		"GOFLAGS=",
		"RMP_FAKE_TOOL_LOG=" + filepath.Join(root, "ran.log"),
	}
	switch {
	case tc.noGo:
		env = append(env, "GOBIN=", "GOPATH=")
	case len(tc.gopath) > 0:
		for i, fake := range tc.gopath {
			dir := filepath.Join(root, "gopath"+strconv.Itoa(i))
			l.gopaths = append(l.gopaths, dir)
			installFake(t, fake, filepath.Join(dir, "bin", tut.tool))
		}
		env = append(env, "GOBIN=", "GOPATH="+strings.Join(l.gopaths, string(os.PathListSeparator)),
			"HOME="+filepath.Join(root, "home"))
	default:
		l.gobin = filepath.Join(root, "gobin")
		if err := os.MkdirAll(l.gobin, 0o700); err != nil {
			t.Fatal(err)
		}
		if tc.gobin != "" {
			installFake(t, tc.gobin, filepath.Join(l.gobin, tut.tool))
		}
		env = append(env, "GOBIN="+l.gobin, "GOPATH="+filepath.Join(root, "gopath"),
			"HOME="+filepath.Join(root, "home"))
	}

	pathDirs := make([]string, 0, len(tc.path)+1)
	for i, fake := range tc.path {
		if fake == gobinOnPath {
			l.pathDirs = append(l.pathDirs, l.gobin)
			pathDirs = append(pathDirs, l.gobin)
			continue
		}
		dir := filepath.Join(root, "path"+strconv.Itoa(i))
		installFake(t, fake, filepath.Join(dir, tut.tool))
		l.pathDirs = append(l.pathDirs, dir)
		pathDirs = append(pathDirs, dir)
	}
	pathDirs = append(pathDirs, lab.support)
	env = append(env, "PATH="+strings.Join(pathDirs, string(os.PathListSeparator)))

	args := []string{"--no-print-directory", "-f", lab.makefile, tut.target}
	if tc.override.hasArg {
		args = append(args, tut.variable+"="+l.resolve(tc.override.arg))
	}
	if tc.override.hasEnv {
		env = append(env, tut.variable+"="+l.resolve(tc.override.env))
	}

	cmd := exec.Command(lab.make, args...)
	cmd.Dir = root
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		t.Fatalf("running make: %v", runErr)
	}

	logged, err := os.ReadFile(filepath.Join(root, "ran.log"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	runs := strings.Split(strings.TrimRight(string(logged), "\n"), "\n")
	if len(logged) == 0 {
		runs = nil
	}
	transcript := fmt.Sprintf("make %s\nstdout:\n%s\nstderr:\n%s\ntools run: %q",
		strings.Join(args, " "), stdout.String(), stderr.String(), runs)

	if tc.expect.passes {
		if runErr != nil {
			t.Fatalf("`make %s` failed, but must pass running %s.\n%s",
				tut.target, l.resolve(tc.expect.ran), transcript)
		}
		want := []string{l.resolve(tc.expect.ran) + " " + tut.runArgs}
		if !slices.Equal(runs, want) {
			t.Fatalf("`make %s` ran %q, but must run exactly %q: the binary SPEC/BUILD.md § Local Tool "+
				"Resolution resolves, and no other.\n%s", tut.target, runs, want, transcript)
		}
		return
	}

	if runErr == nil {
		t.Fatalf("`make %s` passed, but must fail its version check.\n%s", tut.target, transcript)
	}
	if len(runs) != 0 {
		t.Fatalf("`make %s` ran %q, but a failed version check must stop the gate before any tool runs.\n%s",
			tut.target, runs, transcript)
	}
	want := expectedReport(tut, &tc.expect, l)
	if got := reportLines(stderr.String()); !slices.Equal(got, want) {
		t.Fatalf("`make %s` wrote the report\n  %s\nbut SPEC/BUILD.md § Local Tool Resolution requires\n  %s\n%s",
			tut.target, strings.Join(got, "\n  "), strings.Join(want, "\n  "), transcript)
	}
}

// expectedReport builds the report SPEC/BUILD.md § Local Tool Resolution
// publishes for a failed check.
func expectedReport(tut *toolUnderTest, e *expectation, l *layout) []string {
	path := l.resolve(e.checked)
	tail := fmt.Sprintf(", but the Makefile pins %s. Install %s %s, or name a binary of it with %s=<path>.",
		tut.pin, tut.tool, tut.pin, tut.variable)
	report := make([]string, 0, len(e.listed)+3)
	if e.mismatchL {
		report = append(report, fmt.Sprintf("%s at %s is version %s%s", tut.tool, path, e.found, tail))
	} else {
		report = append(report, fmt.Sprintf("%s at %s has no readable version%s", tut.tool, path, tail))
	}
	if len(e.listed) == 0 {
		report = append(report, "PATH holds no copy of "+tut.tool+".")
	} else {
		report = append(report, "PATH holds these copies of "+tut.tool+", in PATH order:")
		for _, s := range e.listed {
			report = append(report, "  "+l.resolve(s))
		}
	}
	return append(report, "Install the pinned version with: "+tut.install)
}

// reportLines returns what the version check wrote: the lines of standard
// error before make's own account of the failed target.
func reportLines(stderr string) []string {
	lines := make([]string, 0, 4)
	for line := range strings.Lines(stderr) {
		line = strings.TrimRight(line, "\n")
		if strings.HasPrefix(line, "make: ") || strings.HasPrefix(line, "make[") {
			break
		}
		lines = append(lines, line)
	}
	return lines
}

// installFake copies a fake binary to dst, keeping its mode.
func installFake(t *testing.T, src, dst string) {
	t.Helper()

	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, content, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

// specToolInstallForms reads the install command of each tool's own section of
// SPEC/BUILD.md, which the report's last line repeats.
func specToolInstallForms(t *testing.T) map[string]installForm {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specBuildPath))
	if err != nil {
		t.Fatalf("reading the build specification: %v", err)
	}
	forms := make(map[string]installForm, 2)
	for tool, heading := range map[string]string{
		"golangci-lint": "### Linter: golangci-lint",
		"gosec":         "### Security Scan: gosec",
	} {
		match := specInstallForm.FindStringSubmatch(specSection(t, string(raw), heading))
		if match == nil {
			t.Fatalf("SPEC/BUILD.md %s states no install command of the form %s", heading, specInstallForm)
		}
		forms[tool] = installForm{text: match[0], placeholder: "<" + match[1] + ">"}
	}
	return forms
}

// toolLab holds the fake binaries every case copies from, and the tools make
// needs.
type toolLab struct {
	fakes    map[string]map[fakeKind]string // tool -> kind -> path
	versions map[string]map[fakeKind]string // tool -> kind -> the version the report names
	make     string
	makefile string
	support  string // the directory holding go and awk, last on every case's PATH
}

func (l *toolLab) fake(tool string, kind fakeKind) string    { return l.fakes[tool][kind] }
func (l *toolLab) version(tool string, kind fakeKind) string { return l.versions[tool][kind] }

// newToolLab builds the fakes of both tools once, for every case.
func newToolLab(t *testing.T, pins toolPins) *toolLab {
	t.Helper()

	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("the Makefile's gates cannot be exercised without make on PATH: %v", err)
	}
	makefile, err := filepath.Abs(makefilePath)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	support := filepath.Join(dir, "support")
	if err := os.MkdirAll(support, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go", "awk"} {
		target, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("the Makefile's version checks need %s on PATH: %v", name, err)
		}
		if err := os.Symlink(target, filepath.Join(support, name)); err != nil {
			t.Fatal(err)
		}
	}

	lab := &toolLab{
		make: makePath, makefile: makefile, support: support,
		fakes:    map[string]map[fakeKind]string{},
		versions: map[string]map[fakeKind]string{},
	}

	// The linter reports its version itself, as golangci-lint's
	// `version --short` does: the bare number, with no leading v.
	otherLint := otherRelease(t, pins.lint)
	lab.fakes["golangci-lint"] = map[fakeKind]string{
		fakePinned:        writeFakeLinter(t, dir, "lint-pinned", strings.TrimPrefix(pins.lint, "v"), 0, 0o755),
		fakeOther:         writeFakeLinter(t, dir, "lint-other", strings.TrimPrefix(otherLint, "v"), 1, 0o755),
		fakeNoVersion:     writeFakeLinter(t, dir, "lint-no-version", "dev", 0, 0o755),
		fakeNotExecutable: writeFakeLinter(t, dir, "lint-not-executable", strings.TrimPrefix(pins.lint, "v"), 0, 0o644),
	}
	lab.versions["golangci-lint"] = map[fakeKind]string{fakeOther: otherLint}

	// The scanner's version is read from its build information, so its fakes
	// are Go programs whose main module carries the version; a shell script
	// has no build information and so no readable version. The copy without
	// its execute permission still carries the pinned version in its build
	// information: it fails because it cannot be run, not for lack of one.
	otherGosec := otherRelease(t, pins.gosec)
	built := buildFakeScanners(t, dir, pins.gosec, otherGosec)
	notExecutable := filepath.Join(dir, "gosec-not-executable")
	installFake(t, built[0], notExecutable)
	if err := os.Chmod(notExecutable, 0o644); err != nil {
		t.Fatal(err)
	}
	lab.fakes["gosec"] = map[fakeKind]string{
		fakePinned:        built[0],
		fakeOther:         built[1],
		fakeNoVersion:     writeFakeLinter(t, dir, "gosec-no-version", strings.TrimPrefix(pins.gosec, "v"), 0, 0o755),
		fakeNotExecutable: notExecutable,
	}
	lab.versions["gosec"] = map[fakeKind]string{fakeOther: otherGosec}
	return lab
}

// otherRelease returns a release of the pinned tool's major version that is
// not the pin: the next minor release.
func otherRelease(t *testing.T, pin string) string {
	t.Helper()

	parts := strings.SplitN(strings.TrimPrefix(pin, "v"), ".", 3)
	if len(parts) < 2 {
		t.Fatalf("the pin %q is not a semantic version", pin)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("the pin %q is not a semantic version: %v", pin, err)
	}
	return fmt.Sprintf("v%s.%d.0", parts[0], minor+1)
}

// writeFakeLinter writes a shell script that answers `version --short` with
// version, records every other invocation, and then exits with status.
func writeFakeLinter(t *testing.T, dir, name, version string, status int, mode os.FileMode) string {
	t.Helper()

	script := fmt.Sprintf("#!/bin/sh\n"+
		"if [ \"$1\" = version ]; then printf '%%s\\n' '%s'; exit 0; fi\n"+
		"printf '%%s\\n' \"$0 $*\" >> \"$RMP_FAKE_TOOL_LOG\"\n"+
		"exit %d\n", version, status)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeScannerSource records every invocation and exits with exitStatus, which
// the build of the release that must fail sets to 1.
const fakeScannerSource = `package main

import (
	"os"
	"strings"
)

var exitStatus = "0"

func main() {
	if log := os.Getenv("RMP_FAKE_TOOL_LOG"); log != "" {
		file, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = file.WriteString(strings.Join(os.Args, " ") + "\n")
			_ = file.Close()
		}
	}
	if exitStatus != "0" {
		os.Exit(1)
	}
}
`

// buildFakeScanners builds the fake scanner twice from a git repository: once
// with HEAD tagged pin, exiting 0, and once with HEAD tagged other, exiting 1.
// Go stamps a clean checkout's tag into the main module's version, which is the
// field `go version -m` prints on the `mod` line. The module path carries the
// major-version suffix the tags require.
func buildFakeScanners(t *testing.T, dir, pin, other string) [2]string {
	t.Helper()

	major := regexp.MustCompile(`^v([0-9]+)\.`).FindStringSubmatch(pin)
	if major == nil {
		t.Fatalf("the pin %q is not a semantic version", pin)
	}
	module := "example.com/fakegosec"
	if n, _ := strconv.Atoi(major[1]); n >= 2 {
		module += "/v" + major[1]
	}

	src := filepath.Join(dir, "fakegosec")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":  "module " + module + "\n\ngo 1.24\n",
		"main.go": fakeScannerSource,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	env := append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=-buildvcs=true")
	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = src
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
	}
	git := func(args ...string) {
		t.Helper()
		run("git", append([]string{"-c", "user.name=Fake Scanner", "-c", "user.email=fake@example.com",
			"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	}

	var built [2]string
	git("init", "-q", ".")
	for i, release := range []struct {
		tag, status string
	}{{pin, "0"}, {other, "1"}} {
		if i > 0 {
			if err := os.WriteFile(filepath.Join(src, "release.txt"), []byte(release.tag+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		git("add", "-A")
		git("commit", "-q", "-m", "release "+release.tag)
		git("tag", release.tag)
		built[i] = filepath.Join(dir, "gosec-"+release.tag)
		run("go", "build", "-ldflags=-X main.exitStatus="+release.status, "-o", built[i], ".")

		out, err := exec.Command("go", "version", "-m", built[i]).Output()
		if err != nil || !regexp.MustCompile(`(?m)^\s+mod\s+\S+\s+`+regexp.QuoteMeta(release.tag)+`\s`).Match(out) {
			t.Fatalf("the fake scanner built at tag %s does not report that version on its mod line (%v):\n%s",
				release.tag, err, out)
		}
	}
	return built
}
