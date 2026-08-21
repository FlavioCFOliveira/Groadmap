package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// helpPrinterSignature matches the opening line of a plain-text help
// printer. Anchored at the start of a line so a mention inside a comment
// or a string literal cannot start a false body scan.
var helpPrinterSignature = regexp.MustCompile(`^func (print\w*Help)\(\) \{`)

// directPrintCall matches a write that goes straight to the process
// stdout rather than through helpDst().
var directPrintCall = regexp.MustCompile(`fmt\.Print(f|ln)?\(`)

// minHelpPrinters is a floor on how many printers the scan must find. A
// scan that matched nothing would pass silently and prove nothing; this
// makes the guard non-vacuous. The package had 64 printers when the rule
// was introduced, and the floor is set below that so adding or removing
// one help page does not fail the test for the wrong reason.
const minHelpPrinters = 50

// TestHelpPrinters_WriteThroughHelpDst pins the invariant that makes the
// recovery help possible: every print*Help function in this package
// writes its body through helpDst(), never through fmt.Printf and
// friends.
//
// Why a source scan rather than a behavioural assertion. A printer that
// called fmt.Printf directly would still look correct on the ordinary
// `--help` path — helpDst() returns os.Stdout there, so the two are
// indistinguishable. The difference only shows on the recovery-help
// path, where the body must land on stderr (SPEC/HELP.md § Recovery help
// after a dispatch failure). If that printer's family were ever the one
// whose subcommand failed to resolve, its help would be written to
// stdout in the middle of a failing invocation that owes stdout zero
// bytes. Scanning the source catches the drift at the moment it is
// introduced, for every printer at once, including the ones no dispatch
// failure reaches today.
func TestHelpPrinters_WriteThroughHelpDst(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}

	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name) // #nosec G304 -- fixed package directory listing, no external input
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		lines := strings.Split(string(src), "\n")
		for i := 0; i < len(lines); i++ {
			m := helpPrinterSignature.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			scanned++
			// The body runs to the first line that starts a closing
			// brace in column zero, which is how gofmt renders the end
			// of a top-level function.
			for j := i + 1; j < len(lines) && !strings.HasPrefix(lines[j], "}"); j++ {
				if directPrintCall.MatchString(lines[j]) {
					t.Errorf("%s:%d: %s writes with %s; help printers must write through "+
						"helpDst() so the body can be redirected to stderr as recovery help "+
						"(see help_output.go)", name, j+1, m[1], strings.TrimSpace(lines[j]))
				}
				i = j
			}
		}
	}

	if scanned < minHelpPrinters {
		t.Fatalf("the scan found only %d help printers, want at least %d; the pattern no longer "+
			"matches the package and this guard is proving nothing", scanned, minHelpPrinters)
	}
}

// TestCommandWriteHelpBody_DivertsEveryDispatchingFamily is the
// behavioural companion to the source scan above, over exactly the set
// that matters: the families for which an unresolved subcommand — and
// therefore a recovery help — can arise.
//
// The source scan covers this package. It cannot see a HelpPrinter that
// lives in another package, and one does: the `web` command's printer is
// internal/web.PrintHelp, which writes straight to stdout. `web` takes no
// subcommand, so no dispatch failure reaches it and its printer is never
// used as recovery help. This test pins that reasoning to the registry
// rather than to a comment: if `web` (or any other family whose printer
// bypasses helpDst) ever gained subcommands, the diversion would fail
// here instead of leaking a help body onto the stdout of a failing
// invocation.
func TestCommandWriteHelpBody_DivertsEveryDispatchingFamily(t *testing.T) {
	for i := range AppRegistry().Commands {
		cmd := &AppRegistry().Commands[i]
		if !cmd.HasSubcommand {
			continue
		}
		var diverted bytes.Buffer
		leaked := captureStdout(t, func() { cmd.WriteHelpBody(&diverted) })

		if diverted.Len() == 0 {
			t.Errorf("%s: WriteHelpBody wrote nothing to the supplied writer", cmd.Name)
		}
		if leaked != "" {
			t.Errorf("%s: WriteHelpBody leaked %d bytes to stdout; the recovery help must reach "+
				"only the writer it was given, because a failing invocation owes stdout zero bytes "+
				"(SPEC/HELP.md § Stdout silence on failure). Leaked prefix: %.120q",
				cmd.Name, len(leaked), leaked)
		}
		if strings.Contains(diverted.String(), AIBannerLine) {
			t.Errorf("%s: the diverted help body carries the AI-agent banner; the recovery help "+
				"must omit it so the hint is not written twice on stderr", cmd.Name)
		}
	}
}
