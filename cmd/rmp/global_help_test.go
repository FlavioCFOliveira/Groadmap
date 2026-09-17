package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
)

// cmd/rmp — regression suite for the opening lines of the helps this package
// owns or joins together, per SPEC/HELP.md § AI agent banner, § Help structure
// template and § Recovery help after a dispatch failure:
//
//	title        the global help opens with
//	             `Groadmap v<version> - A CLI tool for managing technical roadmaps`,
//	             <version> being the application version constant;
//	banner       every stdout help carries the AI-agent banner on the line
//	             after its single Usage: line, followed by the blank line that
//	             used to follow Usage:;
//	recovery     the help written to stderr after a dispatch failure is the
//	             stdout help with exactly the banner line removed;
//	no banner    the version line and the AI Agent Contract carry none.

// globalUsageLine is the Usage: line of the global help.
const globalUsageLine = "Usage: rmp [command] [subcommand] [arguments] [options]"

// titleVersion extracts <version> from a global help title line.
var titleVersion = regexp.MustCompile(`^Groadmap v(\S+) - A CLI tool for managing technical roadmaps$`)

// versionLineVersion extracts <version> from the line `rmp --version` writes.
var versionLineVersion = regexp.MustCompile(`^Groadmap version (\S+) \(`)

// usageLines returns the zero-based index of every line of s that begins with
// "Usage:".
func usageLines(s string) []int {
	var idx []int
	for i, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "Usage:") {
			idx = append(idx, i)
		}
	}
	return idx
}

// assertBannerFollowsUsage checks the placement rule on one stdout help:
// exactly one Usage: line, the banner on the next line, a blank line after the
// banner, the banner present once, and the banner never the first line.
func assertBannerFollowsUsage(t *testing.T, label, out string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	usage := usageLines(out)
	if len(usage) != 1 {
		t.Errorf("%s: want exactly one line beginning with \"Usage:\", got %d at %v; stdout:\n%s",
			label, len(usage), usage, truncate(out))
		return
	}
	u := usage[0]
	if u+2 >= len(lines) {
		t.Errorf("%s: stdout ends before the banner and its blank line:\n%s", label, truncate(out))
		return
	}
	if lines[u+1] != commands.AIBannerLine {
		t.Errorf("%s: the line after Usage: is %q, want the AI-agent banner %q",
			label, lines[u+1], commands.AIBannerLine)
	}
	if lines[u+2] != "" {
		t.Errorf("%s: the line after the banner is %q, want a blank line", label, lines[u+2])
	}
	if lines[0] == commands.AIBannerLine {
		t.Errorf("%s: the banner is the first line of the help; it must follow the Usage: line", label)
	}
	if n := strings.Count(out, commands.AIBannerLine); n != 1 {
		t.Errorf("%s: the banner appears %d times on stdout, want exactly 1", label, n)
	}
}

// captureGlobalHelp returns what printHelp writes to stdout and stderr,
// failing the test if it reports an error.
func captureGlobalHelp(t *testing.T) capturedStreams {
	t.Helper()
	var err error
	streams := captureStreams(t, func() { err = printHelp() })
	if err != nil {
		t.Fatalf("printHelp returned %v, want nil", err)
	}
	return streams
}

// TestGlobalHelp_OpeningLines pins the first six lines of `rmp --help`
// exactly: the versioned title, a blank line, the Usage: line, the banner, a
// blank line, and the first line of the command list, which did not move.
func TestGlobalHelp_OpeningLines(t *testing.T) {
	streams := captureGlobalHelp(t)
	if streams.stderr != "" {
		t.Errorf("global help wrote %d bytes to stderr, want zero: %q", len(streams.stderr), streams.stderr)
	}
	want := []string{
		"Groadmap v" + version + " - A CLI tool for managing technical roadmaps",
		"",
		globalUsageLine,
		commands.AIBannerLine,
		"",
		"Commands:",
	}
	lines := strings.Split(streams.stdout, "\n")
	if len(lines) < len(want) {
		t.Fatalf("global help has %d lines, want at least %d:\n%s", len(lines), len(want), streams.stdout)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("global help line %d = %q, want %q", i+1, lines[i], w)
		}
	}
	assertBannerFollowsUsage(t, "rmp --help", streams.stdout)
}

// TestGlobalHelp_TitleVersionIsTheVersionConstant ties the title's version to
// its single source by three independent readings of the same constant: the
// constant itself, the version `rmp --version` prints (without its build
// identification), and the tool.binary_version the AI Agent Contract
// publishes. A title that carried a literal of its own, or the build
// identification, would disagree with at least one of them.
func TestGlobalHelp_TitleVersionIsTheVersionConstant(t *testing.T) {
	title := firstLine(captureGlobalHelp(t).stdout)
	m := titleVersion.FindStringSubmatch(title)
	if m == nil {
		t.Fatalf("global help title %q does not have the shape "+
			"`Groadmap v<version> - A CLI tool for managing technical roadmaps`", title)
	}
	got := m[1]

	if got != version {
		t.Errorf("title version = %q, want the version constant %q", got, version)
	}
	if strings.Contains(title, "commit") {
		t.Errorf("title %q carries the build identification; it must carry the version alone", title)
	}

	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: buildSettingRevision, Value: "9f3c2a1b7d4e5f60718293a4b5c6d7e8f9012345"},
		{Key: buildSettingModified, Value: "false"},
	}}
	vm := versionLineVersion.FindStringSubmatch(versionLine(info, true))
	if vm == nil {
		t.Fatalf("version line %q does not have the shape `Groadmap version <version> (...)`", versionLine(info, true))
	}
	if got != vm[1] {
		t.Errorf("title version = %q, but `rmp --version` prints %q", got, vm[1])
	}

	handled, code, stdout, _ := runWiring(t, []string{"--ai-help"})
	if !handled || code != ExitSuccess {
		t.Fatalf("--ai-help: handled=%v code=%d, want true/0", handled, code)
	}
	var doc struct {
		Tool struct {
			BinaryVersion string `json:"binary_version"`
		} `json:"tool"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("--ai-help emitted invalid JSON: %v", err)
	}
	if got != doc.Tool.BinaryVersion {
		t.Errorf("title version = %q, but the contract publishes tool.binary_version %q",
			got, doc.Tool.BinaryVersion)
	}
}

// recoveryHelpOf extracts the recovery help from the stderr of a dispatch
// failure: what lies between "<error line>\n\n" and "\n<hint>\n\n".
func recoveryHelpOf(t *testing.T, label, errLine, stderr string) string {
	t.Helper()
	head := errLine + "\n\n"
	tail := "\n" + commands.AIBannerLine + "\n\n"
	if !strings.HasPrefix(stderr, head) || !strings.HasSuffix(stderr, tail) || len(stderr) < len(head)+len(tail) {
		t.Fatalf("%s: stderr is not `error line, blank, help, blank, hint`:\n%s", label, truncate(stderr))
	}
	return stderr[len(head) : len(stderr)-len(tail)]
}

// removeBannerLine removes the one line of help that is exactly the banner,
// together with its newline, and nothing else.
func removeBannerLine(help string) string {
	return strings.Replace(help, commands.AIBannerLine+"\n", "", 1)
}

// TestDispatchFailure_RecoveryHelpIsStdoutHelpMinusBanner pins SPEC/HELP.md
// § Recovery help after a dispatch failure byte for byte: the recovery help
// is the stdout help of the same level with exactly one line removed, the
// banner. For an unresolved command it therefore opens with the global
// title, and at both levels the line after Usage: is the blank line that
// followed the banner on stdout.
func TestDispatchFailure_RecoveryHelpIsStdoutHelpMinusBanner(t *testing.T) {
	stdoutHelp := captureGlobalHelp(t).stdout
	_, streams := probeCommandMiss(t)
	label := "rmp " + unresolvedName
	recovery := recoveryHelpOf(t, label, "Error: unknown command: "+unresolvedName, streams.stderr)
	if want := removeBannerLine(stdoutHelp); recovery != want {
		t.Errorf("%s: the recovery help is not the global help minus the banner line\n got: %.300q\nwant: %.300q",
			label, recovery, want)
	}
	if got, want := firstLine(recovery), "Groadmap v"+version+" - A CLI tool for managing technical roadmaps"; got != want {
		t.Errorf("%s: the recovery help opens with %q, want the global title %q", label, got, want)
	}
	assertUsageThenBlank(t, label, recovery)

	for _, family := range dispatchFamilies {
		cmd := commands.AppRegistry().FindCommand(family)
		if cmd == nil {
			t.Fatalf("family %q missing from the registry", family)
		}
		var err error
		helpStreams := captureStreams(t, func() { err = cmd.DispatchFamily([]string{"--help"}) })
		if err != nil {
			t.Fatalf("rmp %s --help: returned %v", family, err)
		}
		_, miss := probeSubcommandMiss(t, family)
		label := "rmp " + family + " " + unresolvedName
		recovery := recoveryHelpOf(t, label, "Error: unknown "+family+" subcommand: "+unresolvedName, miss.stderr)
		if want := removeBannerLine(helpStreams.stdout); recovery != want {
			t.Errorf("%s: the recovery help is not `rmp %s --help` minus the banner line\n got: %.300q\nwant: %.300q",
				label, family, recovery, want)
		}
		if !strings.HasPrefix(recovery, "Usage: rmp "+family+" ") {
			t.Errorf("%s: the recovery help opens with %q, want the family's Usage: line", label, firstLine(recovery))
		}
		assertUsageThenBlank(t, label, recovery)
	}
}

// assertUsageThenBlank checks that a recovery help has one Usage: line
// followed by a blank line, and no banner.
func assertUsageThenBlank(t *testing.T, label, recovery string) {
	t.Helper()
	usage := usageLines(recovery)
	if len(usage) != 1 {
		t.Errorf("%s: the recovery help has %d Usage: lines, want 1", label, len(usage))
		return
	}
	lines := strings.Split(recovery, "\n")
	if u := usage[0]; u+1 >= len(lines) || lines[u+1] != "" {
		t.Errorf("%s: the line after Usage: in the recovery help must be blank", label)
	}
	if strings.Contains(recovery, commands.AIBannerLine) {
		t.Errorf("%s: the recovery help carries the banner", label)
	}
}

// TestVersionAndContract_CarryNoBanner pins the two outputs the banner must
// stay out of (SPEC/COMMANDS.md § AI Help, rule 1): the version line, in each
// of its three build-identification displays, and the AI Agent Contract in
// each of its four invocation forms.
func TestVersionAndContract_CarryNoBanner(t *testing.T) {
	infos := []struct {
		info *debug.BuildInfo
		ok   bool
	}{
		{nil, false},
		{&debug.BuildInfo{Settings: []debug.BuildSetting{{Key: buildSettingRevision, Value: "4b1d9e0c2a7f8e6d5c4b3a291807f6e5d4c3b2a1"}}}, true},
		{&debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: buildSettingRevision, Value: "4b1d9e0c2a7f8e6d5c4b3a291807f6e5d4c3b2a1"},
			{Key: buildSettingModified, Value: "true"},
		}}, true},
	}
	for _, v := range infos {
		line := versionLine(v.info, v.ok)
		if strings.Contains(line, commands.AIBannerLine) || strings.Contains(line, "AI agents") {
			t.Errorf("version line %q carries the AI-agent banner", line)
		}
		if strings.Contains(line, "\n") {
			t.Errorf("version line %q spans more than one line", line)
		}
	}

	for _, args := range [][]string{
		{"--ai-help"},
		{"ai-help"},
		{"sprint", "--ai-help"},
		{"sprint", "close", "--ai-help"},
	} {
		handled, code, stdout, stderr := runWiring(t, args)
		if !handled || code != ExitSuccess {
			t.Fatalf("%v: handled=%v code=%d, want true/0", args, handled, code)
		}
		if bytes.Contains(stdout, []byte(commands.AIBannerLine)) || bytes.Contains(stderr, []byte(commands.AIBannerLine)) {
			t.Errorf("%v: the AI Agent Contract path wrote the banner", args)
		}
		if !json.Valid(stdout) {
			t.Errorf("%v: the contract is not valid JSON", args)
		}
	}
}
