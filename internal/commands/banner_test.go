// Package commands — tests for the SPEC-mandated AI-agent banner.
//
// These tests enforce the contract in SPEC/HELP.md § AI agent banner:
//
//  1. The banner string is the verbatim SPEC literal (backticks included, no
//     surrounding decoration).
//  2. Every plain-text help reachable through the registry dispatch path
//     carries the banner on the line immediately after its single `Usage:`
//     line, with the blank line that followed `Usage:` now following the
//     banner, and no other line moved: the stdout help is the printer's body
//     with exactly one line inserted.
//  3. Every registered help printer writes exactly one `Usage:` line, so the
//     banner always has its place. A printer without one fails here, not in a
//     user's terminal.
//  4. A body without a `Usage:` line is refused by the insertion point, and
//     nothing is written.
//
// The banner is NOT emitted on the AI Agent Contract path or by the version
// line; both are owned by cmd/rmp and covered there and by the E2E suite.
//
// The walks use the registry as the single source of truth for the list of
// help paths. Adding a new subcommand automatically extends the coverage; no
// second list of names exists.

package commands

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout during fn, returning the bytes
// written. Mirrors the helper in integration_test.go but kept local to
// avoid coupling this test file to that file's test-ordering.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	os.Stdout = old
	<-done
	return buf.String()
}

// mustInvokeHelpPrinter returns what invokeHelpPrinter writes to stdout for
// printer, failing the test if the invocation reports an error.
func mustInvokeHelpPrinter(t *testing.T, printer func()) string {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = invokeHelpPrinter(printer) })
	if err != nil {
		t.Fatalf("invokeHelpPrinter returned an error: %v", err)
	}
	return out
}

// renderHelpBody returns the body printer writes, without the banner: the
// text the recovery help after a dispatch failure carries.
func renderHelpBody(printer func()) string {
	var buf bytes.Buffer
	WriteHelpBodyTo(&buf, printer)
	return buf.String()
}

// removeAIBannerLine returns help without the line that is exactly the
// AI-agent banner. It removes the line and its newline and nothing else, so
// applied to a stdout help it yields the body the printer wrote.
func removeAIBannerLine(help string) string {
	return strings.Replace(help, AIBannerLine+"\n", "", 1)
}

// usageLineIndexes returns the zero-based index of every line of s that
// begins with "Usage:".
func usageLineIndexes(s string) []int {
	var idx []int
	for i, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "Usage:") {
			idx = append(idx, i)
		}
	}
	return idx
}

// TestAIBanner_StringMatchesSpec is a guard against accidental edits
// to the banner literal. Any drift from the SPEC text would mean LLM
// agents looking for the documented banner string would miss it.
func TestAIBanner_StringMatchesSpec(t *testing.T) {
	const wantSpecLiteral = "AI agents usage: run `rmp --ai-help` for a machine-readable command contract."
	if AIBannerLine != wantSpecLiteral {
		t.Fatalf("AIBannerLine drifted from SPEC/HELP.md § AI agent banner:\n  got:  %q\n  want: %q", AIBannerLine, wantSpecLiteral)
	}
}

// TestInsertAIBanner_Placement pins the insertion rule on the shapes the SPEC
// names and on the edges of the line matcher: the banner lands on the line
// after the FIRST line that begins with "Usage:", and every other byte keeps
// its order.
func TestInsertAIBanner_Placement(t *testing.T) {
	const b = AIBannerLine
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "family help opens with its Usage line",
			body: "Usage: rmp task [command] [arguments] [options]\n\nValid status values (for --status filter and 'stat' setter):\n",
			want: "Usage: rmp task [command] [arguments] [options]\n" + b + "\n\nValid status values (for --status filter and 'stat' setter):\n",
		},
		{
			name: "global help carries a title and a blank line before Usage",
			body: "Groadmap v1.17.2 - A CLI tool for managing technical roadmaps\n\nUsage: rmp [command] [subcommand] [arguments] [options]\n\nCommands:\n",
			want: "Groadmap v1.17.2 - A CLI tool for managing technical roadmaps\n\nUsage: rmp [command] [subcommand] [arguments] [options]\n" + b + "\n\nCommands:\n",
		},
		{
			name: "a Usage line followed by text keeps that text on the next line",
			body: "Usage: rmp sprint stats -r <roadmap> <sprint-id>\nReport the progress of one sprint.\n",
			want: "Usage: rmp sprint stats -r <roadmap> <sprint-id>\n" + b + "\nReport the progress of one sprint.\n",
		},
		{
			name: "a Usage line with no trailing newline is the last line",
			body: "Usage: rmp stats -r <roadmap>",
			want: "Usage: rmp stats -r <roadmap>\n" + b,
		},
		{
			name: "only the first Usage line receives the banner",
			body: "Usage: rmp backlog list -r <roadmap>\n\nUsage: rmp backlog show-next -r <roadmap>\n",
			want: "Usage: rmp backlog list -r <roadmap>\n" + b + "\n\nUsage: rmp backlog show-next -r <roadmap>\n",
		},
		{
			name: "an indented or mid-line Usage does not match",
			body: "Release checklist\n  Usage: nested example\nSee Usage: below\nUsage: rmp roadmap list\n\nExamples:\n",
			want: "Release checklist\n  Usage: nested example\nSee Usage: below\nUsage: rmp roadmap list\n" + b + "\n\nExamples:\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := insertAIBanner([]byte(tc.body))
			if err != nil {
				t.Fatalf("insertAIBanner returned an error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("insertAIBanner output mismatch:\n  got:  %q\n  want: %q", got, tc.want)
			}
			if back := removeAIBannerLine(string(got)); tc.body[len(tc.body)-1] == '\n' && back != tc.body {
				t.Errorf("removing the banner line does not give the body back:\n  got:  %q\n  want: %q", back, tc.body)
			}
		})
	}
}

// TestInsertAIBanner_NoUsageLineIsRefused covers the defect the insertion
// point exists to surface: a body with no line beginning with "Usage:" has no
// place for the banner, so it is refused and no output is produced.
func TestInsertAIBanner_NoUsageLineIsRefused(t *testing.T) {
	for _, body := range []string{
		"",
		"\n",
		"Sprint velocity report\n\nOptions:\n",
		"  Usage: rmp task list -r <roadmap>\n",
		"usage: rmp task list -r <roadmap>\n",
		"Synopsis: rmp task list -r <roadmap>\n",
	} {
		got, err := insertAIBanner([]byte(body))
		if err == nil {
			t.Errorf("body %q: want an error, got output %q", body, got)
			continue
		}
		if got != nil {
			t.Errorf("body %q: an error must come with no output, got %q", body, got)
		}
		if !errors.Is(err, errHelpWithoutUsageLine) || !strings.Contains(err.Error(), `"Usage:"`) {
			t.Errorf("body %q: error %q is not the missing-Usage: refusal", body, err)
		}
	}
}

// TestWriteHelpWithAIBanner_WritesNothingOnRefusal pins the write side of the
// refusal: the body is rendered into a buffer first, so the writer receives
// zero bytes rather than a help without its banner.
func TestWriteHelpWithAIBanner_WritesNothingOnRefusal(t *testing.T) {
	var w bytes.Buffer
	err := WriteHelpWithAIBanner(&w, func(dst io.Writer) {
		fmt.Fprint(dst, "Sprint velocity report\n\nOptions:\n  -h, --help\n")
	})
	if err == nil {
		t.Fatal("a body without a Usage: line was accepted")
	}
	if w.Len() != 0 {
		t.Errorf("the refused help wrote %d bytes: %q", w.Len(), w.String())
	}

	// The control: the same writer path with a Usage: line writes the body
	// with the banner inserted, in full.
	w.Reset()
	body := "Usage: rmp sprint stats -r <roadmap> <sprint-id>\n\nOptions:\n"
	if err := WriteHelpWithAIBanner(&w, func(dst io.Writer) { fmt.Fprint(dst, body) }); err != nil {
		t.Fatalf("a body with a Usage: line was refused: %v", err)
	}
	want := "Usage: rmp sprint stats -r <roadmap> <sprint-id>\n" + AIBannerLine + "\n\nOptions:\n"
	if w.String() != want {
		t.Errorf("WriteHelpWithAIBanner output mismatch:\n  got:  %q\n  want: %q", w.String(), want)
	}
}

// TestInvokeHelpPrinter_PrinterWithoutUsageLineFailsTheInvocation proves the
// dispatch path surfaces the refusal: a printer that writes no Usage: line
// makes invokeHelpPrinter return an error and leaves stdout empty, so the
// defect cannot ship as a help silently missing its banner.
func TestInvokeHelpPrinter_PrinterWithoutUsageLineFailsTheInvocation(t *testing.T) {
	printer := func() {
		fmt.Fprintln(helpDst(), "Roadmap statistics\n\nOptions:\n  -h, --help    Show this help message")
	}
	var err error
	out := captureStdout(t, func() { err = invokeHelpPrinter(printer) })
	if !errors.Is(err, errHelpWithoutUsageLine) {
		t.Fatalf("invokeHelpPrinter returned %v for a printer that writes no Usage: line, want the missing-Usage: refusal", err)
	}
	if out != "" {
		t.Errorf("the refused help wrote %d bytes to stdout: %q", len(out), out)
	}
	if helpOut != nil {
		t.Error("the help destination was not restored after the refused render")
	}
}

// helpInvocation is one way the registry can print a help on stdout, paired
// with the printer whose body that help must carry.
type helpInvocation struct {
	label   string
	invoke  func() error
	printer func()
}

// registryHelpInvocations lists every stdout help the registry dispatches:
// the family helps under all four family-help forms, every subcommand help,
// and the leaf helps (`stats`, `web`) through their own handlers.
//
// `ai-help` is the one registered command left out. Its help printer is
// unreachable: cmd/rmp intercepts every `ai-help` token before dispatch and
// emits the contract instead (cmd/rmp/aihelp_wiring.go, covered by
// TestDetectAIHelpInvocation_* there), so no invocation writes that text.
func registryHelpInvocations(t *testing.T) []helpInvocation {
	t.Helper()
	var list []helpInvocation
	reg := AppRegistry()
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		if cmd.Name == "ai-help" {
			continue
		}
		if !cmd.HasSubcommand {
			if len(cmd.Subcommands) != 1 {
				t.Fatalf("leaf command %q must have exactly one Subcommands entry", cmd.Name)
			}
			for _, tok := range []string{"--help", "-h", "help"} {
				list = append(list, helpInvocation{
					label:   "rmp " + cmd.Name + " " + tok,
					invoke:  func() error { return cmd.DispatchFamily([]string{tok}) },
					printer: cmd.Subcommands[0].HelpPrinter,
				})
			}
			continue
		}
		for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"help"}} {
			list = append(list, helpInvocation{
				label:   strings.TrimSpace("rmp " + cmd.Name + " " + strings.Join(args, " ")),
				invoke:  func() error { return cmd.DispatchFamily(args) },
				printer: cmd.HelpPrinter,
			})
		}
		for j := range cmd.Subcommands {
			sub := &cmd.Subcommands[j]
			if sub.Name == "" {
				continue
			}
			list = append(list, helpInvocation{
				label:   "rmp " + cmd.Name + " " + sub.Name + " --help",
				invoke:  func() error { return cmd.DispatchFamily([]string{sub.Name, "--help"}) },
				printer: sub.HelpPrinter,
			})
		}
	}
	return list
}

// TestAIBanner_EveryRegistryHelpCarriesBannerAfterUsage is the contract guard
// for SPEC/COMMANDS.md § AI Help "Discoverability requirements" rule 1 at the
// family, subcommand and leaf levels. For every help the registry dispatches:
//
//   - the help has exactly one line beginning with "Usage:", and it is the
//     first line (family and subcommand helps carry no title);
//   - the next line is the banner, which is therefore the second line and
//     never the first, and the line after the banner is blank;
//   - the banner appears exactly once;
//   - removing that one line gives back, byte for byte, the body the printer
//     writes — the recovery help — so nothing else was added or moved.
func TestAIBanner_EveryRegistryHelpCarriesBannerAfterUsage(t *testing.T) {
	invocations := registryHelpInvocations(t)
	if len(invocations) < 60 {
		t.Fatalf("the registry walk produced only %d help invocations; the walk no longer "+
			"reaches the registry and this guard is proving nothing", len(invocations))
	}
	for _, inv := range invocations {
		t.Run(inv.label, func(t *testing.T) {
			if inv.printer == nil {
				t.Fatalf("%s: no help printer is registered", inv.label)
			}
			var err error
			out := captureStdout(t, func() { err = inv.invoke() })
			if err != nil {
				t.Fatalf("%s: returned an error: %v", inv.label, err)
			}
			assertBannerAfterUsage(t, inv.label, out, 0)

			body := renderHelpBody(inv.printer)
			if got := removeAIBannerLine(out); got != body {
				t.Errorf("%s: the help minus the banner line is not the printer's body\n  got:  %.200q\n  want: %.200q",
					inv.label, got, body)
			}
		})
	}
}

// assertBannerAfterUsage checks the placement rule on one stdout help:
// exactly one "Usage:" line at wantUsage, the banner on the next line, a blank
// line after the banner, and the banner present once and never first.
func assertBannerAfterUsage(t *testing.T, label, out string, wantUsage int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	usage := usageLineIndexes(out)
	if len(usage) != 1 {
		t.Fatalf("%s: want exactly one line beginning with \"Usage:\", got %d at %v:\n%.300s",
			label, len(usage), usage, out)
	}
	u := usage[0]
	if u != wantUsage {
		t.Errorf("%s: the Usage: line is line %d, want line %d", label, u+1, wantUsage+1)
	}
	if u+2 >= len(lines) {
		t.Fatalf("%s: output ends before the banner and its blank line:\n%s", label, out)
	}
	if lines[u+1] != AIBannerLine {
		t.Errorf("%s: the line after Usage: is not the banner\n  got:  %q\n  want: %q", label, lines[u+1], AIBannerLine)
	}
	if lines[u+2] != "" {
		t.Errorf("%s: the line after the banner must be blank, got %q", label, lines[u+2])
	}
	if lines[0] == AIBannerLine {
		t.Errorf("%s: the banner is the first line; it must follow the Usage: line", label)
	}
	if n := strings.Count(out, AIBannerLine); n != 1 {
		t.Errorf("%s: the banner appears %d times, want exactly 1", label, n)
	}
}

// TestAIBanner_EveryRegisteredPrinterWritesOneUsageLine is the guard that
// fails loudly for a printer the insertion point could not serve. It walks
// every HelpPrinter the registry stores — family, leaf and subcommand — and
// requires each body to open with a single one-line `Usage: rmp ...` line and
// to carry no banner of its own. Every command and subcommand except
// `ai-help` must have a printer, so the walk cannot pass by skipping entries.
func TestAIBanner_EveryRegisteredPrinterWritesOneUsageLine(t *testing.T) {
	checked := 0
	check := func(label string, printer func()) {
		t.Helper()
		if printer == nil {
			t.Errorf("%s: no help printer is registered", label)
			return
		}
		checked++
		body := renderHelpBody(printer)
		usage := usageLineIndexes(body)
		if len(usage) != 1 || usage[0] != 0 {
			t.Errorf("%s: the body must open with its single Usage: line; Usage: lines at %v:\n%.300s",
				label, usage, body)
			return
		}
		if !strings.HasPrefix(body, "Usage: rmp ") {
			t.Errorf("%s: the Usage: line must be written on one line, as `Usage: rmp ...`; got %q",
				label, strings.SplitN(body, "\n", 2)[0])
		}
		if strings.Contains(body, AIBannerLine) {
			t.Errorf("%s: the printer writes the banner itself; only the insertion point may", label)
		}
		if _, err := insertAIBanner([]byte(body)); err != nil {
			t.Errorf("%s: the insertion point refuses the body: %v", label, err)
		}
	}

	reg := AppRegistry()
	entries := 0
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		if cmd.Name == "ai-help" {
			continue
		}
		entries++
		check("rmp "+cmd.Name, cmd.HelpPrinter)
		for j := range cmd.Subcommands {
			sub := &cmd.Subcommands[j]
			entries++
			check(strings.TrimSpace("rmp "+cmd.Name+" "+sub.Name), sub.HelpPrinter)
		}
		if !cmd.HasSubcommand && len(cmd.Subcommands) == 1 && cmd.HelpPrinter != nil && cmd.Subcommands[0].HelpPrinter != nil {
			// A leaf command stores its printer twice; the handler prints the
			// subcommand entry's. Both must be the same function, or the
			// family-level entry would describe a help nobody prints.
			if reflect.ValueOf(cmd.HelpPrinter).Pointer() != reflect.ValueOf(cmd.Subcommands[0].HelpPrinter).Pointer() {
				t.Errorf("rmp %s: the command and its single subcommand entry register different help printers", cmd.Name)
			}
		}
	}
	if checked != entries || checked < 60 {
		t.Fatalf("checked %d printers over %d registry entries; every entry must carry one", checked, entries)
	}
}
