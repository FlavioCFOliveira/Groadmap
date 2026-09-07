// Regression fence for SPEC/HELP.md § Graph family help specifics, item 10:
// the exhausted serialisation conflict is named in the exit-code-1 entry of
// `rmp graph execute --help` and of `rmp graph client --help`.
//
// The defect this closes. SPEC/COMMANDS.md § Execute Exit Codes and
// § Client Exit Codes have carried the cause since task #384 published the line
// graphWriteConflict prints, but the two helps enumerated the causes of exit 1
// without it. A caller who reads the block — which is where a caller goes to
// learn why a command exited 1 — found "a Cypher parse or execution error" and
// the statement time budget, and had no way to learn that a valid statement
// against a healthy store can exit 1 on contention alone, nor that this is the
// one cause whose remedy is to run the SAME statement again. Two published
// surfaces disagreed, and no gate saw it: help_content_test.go asserts that an
// exit-codes block EXISTS and carries code 0, tests/test_61 parses the CODES out
// of it, and neither reads a word of what a code's entry says.
//
// Why the assertions are scoped to the exit-1 entry rather than to the help.
// The `client` help already carried a true prose paragraph about contention
// ("A serialisation conflict is retried, not reported"), so a check for the
// phrase anywhere in that help passed BEFORE the clause existed and would have
// gone on passing if the clause were deleted. Scoping to the entry is what makes
// the assertion about the obligation item 10 actually places, and it is why
// DECISION #423 declined to rely on that paragraph.
//
// Why both helps. `client` always reaches a server and `execute` reaches one
// whenever the roadmap is served, so both subcommands have the cause. Naming it
// in one was the second option DECISION #423 declined, on the ground that two
// sibling helps disagreeing about a cause both commands have is the asymmetry
// item 4 exists to prevent. A table over both is what fences that.
//
// The two helps state it in different registers on purpose — `execute` chains
// "Also …" clauses, `client` chains semicolon-separated "or …" clauses — so the
// fragments asserted here are the ones item 10 and the published error line fix
// between them, not a transcription of either help's prose.
package commands

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
)

// conflictHelpSurfaces are the two helps item 10 binds, each reached the way a
// user reaches it — through the registry's dispatch, so a subcommand rewired to
// a different printer is followed rather than missed.
var conflictHelpSurfaces = []struct {
	label string
	argv  []string
}{
	{"rmp graph execute --help", []string{"execute", "--help"}},
	{"rmp graph client --help", []string{"client", "--help"}},
}

// conflictClauseFragments are the words item 10 fixes for both registers.
//
//   - the cause, in item 10's own terms ("every attempt of the retry policy lost
//     a serialisation conflict against a server"), stopping before the article
//     so that "against a server" and "against the server" both satisfy it;
//   - the remedy, in the published error line's terms, both halves of it: run the
//     SAME statement again, and spread concurrent writes across distinct nodes.
//
// The fact that nothing was written is asserted separately, by
// conflictNothingWritten, because the two helps are in different tenses.
var conflictClauseFragments = []string{
	"every attempt of the retry policy lost a serialisation conflict against",
	"run the same statement again",
	"spread concurrent writes across distinct nodes",
}

// conflictNothingWritten matches the claim item 10 requires the entry to make —
// a fact rather than a hope, the conflict being detected before anything is
// applied — in either tense, `execute` reporting it beside a transaction that
// commits nothing and `client` beside a statement that was sent.
var conflictNothingWritten = regexp.MustCompile(`nothing (is|was) written`)

// exitCodeEntryLine matches the first line of an entry in an `Exit codes:`
// block: exactly two leading spaces, the code, then the description. It is the
// same shape tests/test_61_family_help_dispatch_exit_code.py parses, and the
// same shape every help printer in the CLI emits.
var exitCodeEntryLine = regexp.MustCompile(`^ {2}(\d+) {2,}(\S.*)$`)

// exitCodeEntry returns the text of the entry for code want inside the
// `Exit codes:` block of a help output, with its continuation lines joined and
// whitespace normalised. It returns "" when the block or the entry is absent,
// which the callers treat as a failure rather than as a pass.
func exitCodeEntry(help, want string) string {
	lines := strings.Split(help, "\n")
	start := -1
	for i, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), "Exit codes:") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}

	var collected []string
	collecting := false
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			if collecting {
				break // the entry, and the block, end at the blank line
			}
			continue
		}
		if m := exitCodeEntryLine.FindStringSubmatch(line); m != nil {
			if collecting {
				break // the next code's entry begins
			}
			if m[1] == want {
				collecting = true
				collected = append(collected, m[2])
			}
			continue
		}
		if collecting {
			collected = append(collected, strings.TrimSpace(line))
		}
	}
	return normalizeHelpText(strings.Join(collected, " "))
}

// TestGraphHelps_ExitCode1NamesTheExhaustedConflict is the content half of the
// fence: both helps must name the cause, claim that nothing was written, and
// give both halves of the remedy, INSIDE the entry for code 1.
func TestGraphHelps_ExitCode1NamesTheExhaustedConflict(t *testing.T) {
	graphCmd := AppRegistry().FindCommand("graph")
	if graphCmd == nil {
		t.Fatal("the graph command is missing from the registry")
	}

	for _, surface := range conflictHelpSurfaces {
		t.Run(surface.label, func(t *testing.T) {
			help := captureStdout(t, func() {
				_ = graphCmd.DispatchFamily(surface.argv)
			})

			entry := exitCodeEntry(help, "1")
			if entry == "" {
				t.Fatalf("%s: no entry for exit code 1 was found in the 'Exit codes:' block. "+
					"Every assertion below is about that entry, so a block this parser cannot "+
					"read makes the whole gate vacuous:\n%s", surface.label, help)
			}

			// The parse is anchored, so a change to the block's layout that made
			// exitCodeEntry swallow the whole help would fail here rather than
			// turn the fragment assertions into a search of the entire text.
			for _, foreign := range []string{"No roadmap selected", "Roadmap not found"} {
				if strings.Contains(entry, foreign) {
					t.Fatalf("%s: the parsed exit-1 entry reaches into another code's entry "+
						"(it contains %q), so the assertions below are no longer scoped to "+
						"exit 1:\n%s", surface.label, foreign, entry)
				}
			}

			for _, fragment := range conflictClauseFragments {
				if !strings.Contains(entry, fragment) {
					t.Errorf("%s: the exit-code-1 entry does not carry %q.\n"+
						"SPEC/HELP.md § Graph family help specifics item 10 requires this entry "+
						"to name the exhausted serialisation conflict and to give the remedy in "+
						"the published error line's own terms; a caller reads this block, not "+
						"the prose above it, to learn why the command exited 1.\nentry: %s",
						surface.label, fragment, entry)
				}
			}
			if !conflictNothingWritten.MatchString(entry) {
				t.Errorf("%s: the exit-code-1 entry does not state that nothing was written.\n"+
					"That claim is what makes 'run it again' safe advice, and it is true rather "+
					"than optimistic: the conflict is detected before anything is applied "+
					"(SPEC/GRAPH.md § Concurrency Inside the Server, rule 5).\nentry: %s",
					surface.label, entry)
			}
		})
	}
}

// conflictHelpSources are the files holding the two help literals, keyed by the
// printer whose body carries the text.
var conflictHelpSources = map[string]string{
	"printGraphExecuteHelp": "graph.go",
	"printGraphClientHelp":  "graph_client.go",
}

// TestGraphHelps_DoNotWriteTheRetryBudgetFigure is the prohibition half.
//
// Item 10 forbids the helps from writing the retry budget's figure into their own
// text: graphWriteConflict renders it from backoff.Total() precisely so that one
// quantity keeps one expression, and a figure spelled out in a help string would
// be a second expression of it that disagreed with the policy silently the moment
// the policy changed. That is the defect task #386 fixed for the log timestamp
// format, applied to a duration.
//
// The check is on the SOURCE LITERAL rather than on the printed output, because
// that is the distinction item 10 draws. A help that rendered the figure from the
// same policy the error line reads would be lawful and would print the figure;
// only a figure written into the text is forbidden, and only a literal can be
// that.
func TestGraphHelps_DoNotWriteTheRetryBudgetFigure(t *testing.T) {
	figure := backoff.Total().String()

	// The help legitimately writes the statement time budget's figure, item 4
	// requiring it to name the 5-second budget. If the retry policy ever rendered
	// the same characters the two would be indistinguishable by this check, and
	// saying so loudly is better than a gate that silently starts passing or
	// falsely failing.
	if figure == graphStatementBudget().String() {
		t.Fatalf("backoff.Total() renders %q, the same characters as the statement time budget "+
			"the help names by item 4. This gate cannot tell the forbidden figure from the "+
			"permitted one while that holds", figure)
	}

	for printer, file := range conflictHelpSources {
		t.Run(printer, func(t *testing.T) {
			literals := stringLiteralsOfFunc(t, file, printer)
			if len(literals) == 0 {
				t.Fatalf("no string literal was found in %s (%s), so this gate is reading "+
					"nothing", printer, file)
			}

			anchored := false
			for _, lit := range literals {
				if strings.Contains(lit, "Exit codes:") {
					anchored = true
				}
				if strings.Contains(lit, figure) {
					t.Errorf("%s writes the retry budget's figure %q into its own text.\n"+
						"SPEC/HELP.md § Graph family help specifics item 10 forbids it: "+
						"graphWriteConflict renders that quantity from backoff.Total() so it "+
						"keeps one expression, and a literal here is a second one that would "+
						"disagree with the policy the moment the policy moved. Name the cause "+
						"and the remedy without the figure, or render it from the same policy.",
						printer, figure)
				}
			}
			if !anchored {
				t.Errorf("no literal of %s carries an 'Exit codes:' block, so this gate is not "+
					"reading the help text it was written to read", printer)
			}
		})
	}
}

// stringLiteralsOfFunc parses file and returns the unquoted value of every string
// literal in the body of the function named fn.
func stringLiteralsOfFunc(t *testing.T, file, fn string) []string {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}

	var literals []string
	for _, decl := range parsed.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != fn || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				t.Fatalf("unquoting a string literal of %s in %s: %v", fn, file, uerr)
			}
			literals = append(literals, value)
			return true
		})
		return literals
	}

	t.Fatalf("no function named %s was found in %s. This gate is keyed on that name, so a "+
		"rename must move the key rather than leave the help unwatched", fn, file)
	return nil
}
