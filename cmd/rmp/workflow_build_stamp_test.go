package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// specDeployPath locates SPEC/DEPLOY.md from this package's directory, which is
// where `go test` sets the working directory.
const specDeployPath = "../../SPEC/DEPLOY.md"

// stampSectionHeading is the SPEC/DEPLOY.md section that specifies the stamp, the
// -buildvcs=true flag and the stamp check.
const stampSectionHeading = "### How a Released Binary Carries Its Commit"

// stampBinaryPlaceholder opens the line the stamp check publishes. It stands for
// the path the step passed to `go version -m`.
const stampBinaryPlaceholder = "{binary}"

// TestWorkflowBuildJobsStampTheBinary is the regression gate for rmp task #415.
// A binary could not report which commit it was built from; it now reads the
// version-control stamp the Go toolchain records, and the two workflows guard
// that stamp twice: every build job builds with -buildvcs=true, so a build that
// finds the repository but cannot stamp the binary fails, and a stamp-check step
// after the build — and, in the release workflow, before the upload; the CI
// workflow's build job uploads nothing — fails the job when the binary carries
// no vcs.revision, whatever the cause (SPEC/DEPLOY.md § How a Released Binary
// Carries Its Commit, conditions 2 and 4). Neither workflow passes a -X linker
// flag, because the version is a constant and the commit is the toolchain's.
//
// SPEC/BUILD.md and SPEC/DEPLOY.md phrase these criteria as what reading the
// workflows shows. This test holds them, because removing either guard changes
// nothing on an ordinary run — the toolchain stamps a checkout-built binary with
// or without them — so the regression would ship unnoticed until the one run the
// guards exist for.
//
// The published line is read out of SPEC/DEPLOY.md rather than repeated here.
func TestWorkflowBuildJobsStampTheBinary(t *testing.T) {
	suffix := parseStampCheckSuffix(t)

	for _, p := range pipelines() {
		t.Run(filepath.Base(p.path), func(t *testing.T) {
			assertBuildStamp(t, p, suffix)
		})
	}
}

// parseStampCheckSuffix reads the line the stamp check publishes in
// SPEC/DEPLOY.md § How a Released Binary Carries Its Commit and returns what
// follows its {binary} placeholder.
func parseStampCheckSuffix(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(specDeployPath))
	if err != nil {
		t.Fatalf("reading the deployment specification: %v", err)
	}
	spec := string(raw)

	start := strings.Index(spec, "\n"+stampSectionHeading+"\n")
	if start < 0 {
		t.Fatalf("SPEC/DEPLOY.md has no %q section, so this gate cannot read the line the stamp check "+
			"publishes", stampSectionHeading)
	}
	section := spec[start+len(stampSectionHeading)+2:]
	if end := strings.Index(section, "\n#"); end >= 0 {
		section = section[:end]
	}

	_, stampCheck, found := strings.Cut(section, "**The stamp check.**")
	if !found {
		t.Fatalf("SPEC/DEPLOY.md § How a Released Binary Carries Its Commit no longer has a \"The stamp " +
			"check.\" paragraph; this gate cannot read the line the step must write")
	}
	block, ok := fencedBlock(stampCheck)
	if !ok {
		t.Fatalf("SPEC/DEPLOY.md § How a Released Binary Carries Its Commit no longer publishes the stamp " +
			"check's line in a fenced block after \"The stamp check.\"")
	}

	lines := make([]string, 0, 1)
	for line := range strings.Lines(block) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 || !strings.HasPrefix(lines[0], stampBinaryPlaceholder) ||
		len(lines[0]) == len(stampBinaryPlaceholder) {
		t.Fatalf("SPEC/DEPLOY.md publishes %q as the stamp check's output; this gate expects exactly one line "+
			"beginning with %s. Fix the reader rather than letting the step go unchecked.",
			lines, stampBinaryPlaceholder)
	}
	return strings.TrimPrefix(lines[0], stampBinaryPlaceholder)
}

// assertBuildStamp holds one workflow to the three guards: no -X linker flag
// anywhere in the file, -buildvcs=true on every `go build` of its build job, and
// the stamp-check step placed after the build. In the release workflow the step
// must also precede the upload; in the CI workflow, which publishes nothing, the
// build job must upload nothing at all.
func assertBuildStamp(t *testing.T, p pipeline, suffix string) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Clean(p.path))
	if err != nil {
		t.Fatalf("reading the workflow %s: %v", p.rel(), err)
	}
	for number, line := range strings.Split(string(raw), "\n") {
		if text := workflowCodeText(line); isLinkerXFlag(text) {
			t.Errorf("%s:%d passes a -X linker flag: %q. SPEC/BUILD.md § GitHub Actions Workflow and "+
				"SPEC/DEPLOY.md § How a Released Binary Carries Its Commit forbid every -X flag: the version is "+
				"the constant in cmd/rmp/main.go and the commit is the toolchain's vcs.revision stamp, so no "+
				"flag carries either.", p.rel(), number+1, text)
		}
	}

	wf := parseWorkflow(t, p.path)
	build := mustJob(t, wf, p.buildJob)

	buildStep := -1
	for _, cmd := range jobCommands(&build) {
		words := shellWords(cmd.text)
		at := goBuildAt(words)
		if at < 0 {
			continue
		}
		buildStep = max(buildStep, cmd.step)
		if !buildsWithVCSStamp(words[at+2:]) {
			t.Errorf("%s: job %q runs %q, which does not build with -buildvcs=true as its last -buildvcs "+
				"setting. Without it a build that finds the repository but cannot record the stamp writes a "+
				"binary reporting \"(commit unknown)\" instead of failing (SPEC/DEPLOY.md § How a Released "+
				"Binary Carries Its Commit, condition 2).", p.rel(), p.buildJob, cmd.text)
		}
	}
	if buildStep < 0 {
		t.Errorf("%s: job %q runs no `go build` command, so neither -buildvcs=true nor the stamp check's "+
			"placement can be verified. SPEC/BUILD.md § GitHub Actions Workflow makes this job the build gate.",
			p.rel(), p.buildJob)
		return
	}

	upload := slices.IndexFunc(build.steps, func(step wfStep) bool {
		return strings.HasPrefix(step.uses, "actions/upload-artifact@")
	})
	switch {
	case p.publishes() && upload < 0:
		t.Errorf("%s: job %q has no step that uses actions/upload-artifact, so the stamp check cannot be shown "+
			"to run before the binary leaves the job (SPEC/DEPLOY.md § How a Released Binary Carries Its "+
			"Commit, condition 4).", p.rel(), p.buildJob)
	case !p.publishes() && upload >= 0:
		t.Errorf("%s: job %q uploads an artefact in step %q, but the CI workflow's build job uploads none: "+
			"it runs the `go build` and then the stamp check, and the binary does not leave the job "+
			"(SPEC/BUILD.md § CI Workflow; SPEC/DEPLOY.md § How a Released Binary Carries Its Commit, "+
			"condition 4).", p.rel(), p.buildJob, build.steps[upload].name)
	}

	// Candidates are the steps with a command, not a comment, that runs
	// `go version -m`.
	candidates := make([]int, 0, 1)
	for _, cmd := range jobCommands(&build) {
		if strings.Contains(cmd.text, "go version -m") && !slices.Contains(candidates, cmd.step) {
			candidates = append(candidates, cmd.step)
		}
	}
	if len(candidates) == 0 {
		t.Errorf("%s: job %q has no step that runs `go version -m` on the built binary, so a binary that "+
			"carries no vcs.revision passes the job. SPEC/DEPLOY.md § How a Released Binary Carries Its "+
			"Commit (condition 4) requires the stamp check after the build and, in the release workflow, "+
			"before the upload.",
			p.rel(), p.buildJob)
		return
	}

	reports := make([]string, 0, len(candidates))
	for _, index := range candidates {
		step := build.steps[index]
		problems := stampStepProblems(step, suffix)
		if index <= buildStep {
			problems = append(problems, fmt.Sprintf("it runs before the step that runs `go build` (%q), so "+
				"it reads a binary the job has not built yet", build.steps[buildStep].name))
		}
		if upload >= 0 && index >= upload {
			problems = append(problems, fmt.Sprintf("it runs after the upload step %q, so an unstamped "+
				"binary has already left the job", build.steps[upload].name))
		}
		if len(problems) == 0 {
			return
		}
		reports = append(reports, fmt.Sprintf("step %q: %s", step.name, strings.Join(problems, "; ")))
	}
	t.Errorf("%s: job %q runs `go version -m`, but not as the stamp check SPEC/DEPLOY.md § How a Released "+
		"Binary Carries Its Commit (condition 4) specifies — after the step that runs `go build` (and, in the "+
		"release workflow, before the upload), deciding on the command's output, and writing the published "+
		"line to standard error with exit status 1:\n  %s", p.rel(), p.buildJob, strings.Join(reports, "\n  "))
}

// stampExit matches a shell command that exits with status 1.
var stampExit = regexp.MustCompile(`(?:^|[\s;&|])exit\s+1(?:$|[\s;])`)

// stampStepProblems describes how a step that runs `go version -m` falls short
// of the stamp check, or returns nothing when it is one.
func stampStepProblems(step wfStep, suffix string) []string {
	problems := make([]string, 0, 4)

	// messageBefore is what precedes the published line's suffix on the line
	// that writes it; its last shell word is what the step substitutes for
	// {binary}.
	var target, messageLine, messageBefore string
	captured, decides, exits := false, false, false
	for line := range strings.Lines(step.run) {
		code := strings.TrimSpace(line)
		if code == "" || strings.HasPrefix(code, "#") {
			continue
		}
		// The published line itself names vcs.revision, so it is set aside
		// before the step is searched for the command that examines it.
		if at := strings.Index(code, suffix); at >= 0 {
			messageLine, messageBefore = code, code[:at]
			code = code[:at] + code[at+len(suffix):]
		}
		if at := strings.Index(code, "go version -m"); at >= 0 {
			if words := shellWords(code[at:]); len(words) > 3 {
				target = words[3]
			}
			captured = strings.Contains(code[:at], "$(") || strings.Contains(code[at:], "|")
		}
		if strings.Contains(code, "vcs.revision") || strings.Contains(code, `vcs\.revision`) {
			decides = true
		}
		if stampExit.MatchString(code) {
			exits = true
		}
	}

	switch {
	case target == "":
		problems = append(problems, "no binary path could be read from its `go version -m` command")
	case !captured:
		problems = append(problems, "it does not read the output of `go version -m` (the command is neither "+
			"captured with $( ) nor piped), and that command exits 0 for a binary that carries no stamp")
	}
	if !decides {
		problems = append(problems, "no command of the step examines vcs.revision, so it cannot tell a stamped "+
			"binary from an unstamped one")
	}
	if messageLine == "" {
		problems = append(problems, fmt.Sprintf("it does not write the line SPEC/DEPLOY.md publishes, %q",
			stampBinaryPlaceholder+suffix))
	} else {
		if named := messageBefore[strings.LastIndexAny(messageBefore, "\"' ")+1:]; named != target {
			problems = append(problems, fmt.Sprintf("the published line names %q where %s must be the path "+
				"passed to `go version -m`, %q", named, stampBinaryPlaceholder, target))
		}
		if !strings.Contains(messageLine, ">&2") {
			problems = append(problems, "it does not write the published line to standard error")
		}
	}
	if !exits {
		problems = append(problems, "it never exits with status 1")
	}
	return problems
}

// workflowCodeText returns a workflow line with its YAML or shell comment
// removed, or the empty string for a whole-line comment.
func workflowCodeText(line string) string {
	text := strings.TrimSpace(line)
	if strings.HasPrefix(text, "#") {
		return ""
	}
	if cut := wfCommentIndex(text); cut >= 0 {
		text = strings.TrimSpace(text[:cut])
	}
	return text
}

var (
	// dashXToken matches -X as a flag of its own, in the forms `-X sym=v`,
	// `-X=sym=v`, `-ldflags="-X ...` and `-ldflags=-X ...`.
	dashXToken = regexp.MustCompile(`(?:^|[\s"'=])-X(?:[\s=]|$)`)
	// linkerContext matches a line that hands flags to the Go linker.
	linkerContext = regexp.MustCompile(`(?i)ldflags|GOFLAGS|\bgo\s+(?:build|install|run|test)\b|\bgo\s+tool\s+link\b`)
)

// isLinkerXFlag reports whether a line of a workflow passes a -X linker flag:
// one that assigns a package symbol (ldflagSymbol, declared beside
// TestWorkflowsDoNotSetConstantsWithLdflags), or a bare -X on a line that hands
// flags to the linker. A -X that is no linker flag, such as `curl -X POST`, is
// not reported.
func isLinkerXFlag(text string) bool {
	if text == "" {
		return false
	}
	return ldflagSymbol.MatchString(text) || (dashXToken.MatchString(text) && linkerContext.MatchString(text))
}

// goBuildAt returns the index of the word `go` that begins a `go build` command,
// or -1.
func goBuildAt(words []string) int {
	for i := 0; i+1 < len(words); i++ {
		if words[i] == "go" && words[i+1] == "build" {
			return i
		}
	}
	return -1
}

// buildsWithVCSStamp reports whether a `go build` command's arguments end its
// -buildvcs settings on -buildvcs=true: the last setting of a flag is the one
// the command applies.
func buildsWithVCSStamp(arguments []string) bool {
	last := ""
	for _, word := range arguments {
		if strings.HasPrefix(word, "--") {
			word = word[1:]
		}
		if word == "-buildvcs" || strings.HasPrefix(word, "-buildvcs=") {
			last = word
		}
	}
	return last == "-buildvcs=true"
}
