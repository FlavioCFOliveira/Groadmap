// Regression fence for the field-length refusal on `rmp graph client`
// (rmp task #413, written against `rmp graph execute` before it was withdrawn).
//
// Before this class existed, a statement writing a label or a property key
// longer than the engine could carry reached the caller on the SAME line as a
// Cypher syntax error:
//
//	Error: graph engine error: graph query failed: <engine diagnostic>
//
// The two conditions were separated only by that diagnostic tail, which
// SPEC/GRAPH.md § Error Handling and Exit Codes, rule 2, deliberately declines to
// specify and which a caller therefore cannot lawfully match. A caller had to
// parse English to learn whether to correct the statement's syntax or to shorten
// one of its values — two opposite actions behind one line.
//
// The pinned engine's server gives the refusal a failure code of its own — one
// for an over-long token, one for any other field a durable format cannot carry —
// and the shared Bolt client recognises the class by those two codes and by
// nothing else (SPEC/GRAPH.md § Field Length Limits, rules 3 and 13). This file
// fences the wording of the line those codes select, and drives a real over-long
// field through a real server so the codes are proven to arrive. The
// classification by code, and the refusal of every wider match, are fenced where
// the classification lives: internal/graphclient's
// TestSend_ClassifiesAFieldTooLongByItsCodesAlone.
package commands

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// publishedFieldTooLongHead is rmp's own half of the line SPEC/COMMANDS.md
// § Graph Management publishes for a field the engine refuses, minus the
// "Error: " prefix the top-level error printer adds:
//
//	Error: graph engine error: graph field too long; nothing was written.
//	Shorten the field the engine names: <engine diagnostic>
//
// It is a HEAD and not the whole line, and that is the point of the row: the
// engine's diagnostic ends it, because it names which of the statement's fields
// is at fault and by how much, and no text rmp could write would know that
// (SPEC/GRAPH.md § Field Length Limits, rule 4). Everything up to and including
// the trailing space is rmp's and is asserted character for character here; the
// tail is the engine's and is asserted only to be present and unaltered.
//
// The characters are ASCII throughout: unlike the budget line beside it, this one
// carries no em dash.
const publishedFieldTooLongHead = "graph engine error: graph field too long; nothing was " +
	"written. Shorten the field the engine names: "

// engineFieldDiagnostic stands in for the diagnostic the engine's server
// forwards with the refusal: the field kind and both figures.
func engineFieldDiagnostic(kind string) string {
	return "cypher: " + kind + " is 65536 bytes, maximum 65535: token too long"
}

// TestGraphFieldTooLong_IsThePublishedLine is the positive half: the class is
// worded as published, ends in the engine's diagnostic unchanged, and carries the
// sentinel and exit code the ordinary engine failure carries.
func TestGraphFieldTooLong_IsThePublishedLine(t *testing.T) {
	diagnostic := engineFieldDiagnostic("node label")

	err := graphFieldTooLong(diagnostic)
	if !errors.Is(err, utils.ErrGraphEngine) {
		t.Errorf("err = %v, want it to wrap utils.ErrGraphEngine (exit code 1)", err)
	}
	if got, want := err.Error(), publishedFieldTooLongHead+diagnostic; got != want {
		t.Errorf("published line\n got:  %q\n want: %q", got, want)
	}

	t.Run("the field kind reaches the caller whatever it is", func(t *testing.T) {
		// rmp's half names no field kind and must not: it defers to the engine's,
		// which is the only one that knows (rule 4).
		for _, kind := range []string{"node label", "edge label", "node property key", "edge property key", "property value"} {
			if err := graphFieldTooLong(engineFieldDiagnostic(kind)); !strings.Contains(err.Error(), kind) {
				t.Errorf("field kind %q did not reach the caller: %q", kind, err.Error())
			}
		}
		if strings.Contains(publishedFieldTooLongHead, "label") ||
			strings.Contains(publishedFieldTooLongHead, "property") {
			t.Errorf("rmp's half %q names a field kind of its own; it must defer to the "+
				"engine's (rule 4)", publishedFieldTooLongHead)
		}
	})
}

// TestGraphServerFailure_SelectsTheFieldLineByKindAlone is the negative half at
// this layer: the same diagnostic classified as an ordinary statement failure —
// what an earlier engine's argument-error code produced — keeps the ordinary
// line, so nothing here re-derives the class from the text (rule 3).
func TestGraphServerFailure_SelectsTheFieldLineByKindAlone(t *testing.T) {
	const socket = "/home/user/.roadmaps/backend-platform/graph.sock"
	diagnostic := engineFieldDiagnostic("node label")

	field := graphServerFailure(socket, &graphclient.SendError{
		Kind: graphclient.FailureFieldTooLong, Socket: socket,
		Code: "Neo.ClientError.Data.DataUnsupportedByStoreFormat", Diagnostic: diagnostic,
	})
	if got, want := field.Error(), publishedFieldTooLongHead+diagnostic; got != want {
		t.Errorf("a refusal classified as too long\n got:  %q\n want: %q", got, want)
	}

	ordinary := graphServerFailure(socket, &graphclient.SendError{
		Kind: graphclient.FailureStatement, Socket: socket,
		Code: "Neo.ClientError.Statement.ArgumentError", Diagnostic: diagnostic,
	})
	if got, want := ordinary.Error(), "graph engine error: graph query failed: "+diagnostic; got != want {
		t.Errorf("the same words classified as an ordinary failure\n got:  %q\n want: %q", got, want)
	}
	if !errors.Is(field, utils.ErrGraphEngine) || !errors.Is(ordinary, utils.ErrGraphEngine) {
		t.Errorf("the two lines must share utils.ErrGraphEngine (exit code 1): %v / %v", field, ordinary)
	}
}

// TestGraphStatementError_BudgetLineIsUntouched fences the one classification
// graphStatementError still makes against the field-length line: a deadline
// selects the budget line, and nothing else does.
func TestGraphStatementError_BudgetLineIsUntouched(t *testing.T) {
	err := graphStatementError(150*time.Millisecond, "graph query failed", budgetSelector())
	if got, want := err.Error(), wantBudgetLine(150*time.Millisecond); got != want {
		t.Errorf("a deadline must select the budget line\n got:  %q\n want: %q", got, want)
	}
	if strings.Contains(err.Error(), "graph field too long") {
		t.Errorf("the budget line was reported as a field-length refusal: %q", err.Error())
	}
}

// The SECOND half of the condition — a field that commits and is then refused by
// every CHECKPOINT — is no longer this package's to report, and the test that
// used to assert it here is retired rather than adapted.
//
// `rmp graph client` opens no store and takes no checkpoint, so there is no
// checkpoint error for it to hold and rule 8 of SPEC/GRAPH.md § Field Length
// Limits says in as many words that a surface which does not hold the error MUST
// NOT pretend to. graphCheckpointWarning went with the store open it belonged to.
//
// Its coverage is accounted for in two places and neither is a weakening:
//
//   - internal/graphstore's TestFieldTooLongCheckpointDiagnostic asserts all four
//     of rule 9's contents against FieldTooLongCheckpointDiagnostic, which is now
//     the ONE wording of this diagnostic and the one every holder of the error
//     reports through — the same assertions this test made, made against the
//     shared producer instead of against a copy of it;
//   - internal/graphserve's TestShutdownCheckpointMessage asserts that the
//     surface which DOES still hold the error — the server's shutdown checkpoint
//     — selects that wording for a refused field and the general wording for
//     everything else.
//
// What no longer exists anywhere, because the condition no longer exists, is the
// short-lived invocation's per-write repetition of the diagnostic.

// walMaximumPattern reads the maximum the engine reports back out of its own
// refusal. The figure is the ENGINE's, a version bump may move it, and a test
// pinned to a literal would confirm a stale figure instead of failing on it
// (SPEC/GRAPH.md § Acceptance Criteria, 68).
var walMaximumPattern = regexp.MustCompile(`maximum (\d+)`)

// overLongLabelStatement writes a well-formed element FIRST and the over-long
// label second, in one pass. The order is the point: an implementation that
// committed the statement's well-formed prefix and refused only its tail would
// pass a check that only looked at the write which follows the refusal
// (SPEC/GRAPH.md § Acceptance Criteria, 69).
func overLongLabelStatement(n int) string {
	return "CREATE (n:FieldLengthFence {name:'kept'}) CREATE (m:`" + strings.Repeat("L", n) + "`)"
}

// TestGraphClient_FieldTooLongAgainstTheRealEngine drives a REAL over-long field
// through a real server and the CLI, and it is the one test in this file that can
// fail on something the classified cases cannot see: that the engine's server
// still gives the refusal one of the two codes the client matches.
//
// The bound is read off the engine's own refusal — its diagnostic names the
// maximum in force — and never written here as a literal, because the figure is
// the engine's and a version bump may move it (SPEC/GRAPH.md § Acceptance
// Criteria, 68). At that bound: one byte over is refused with the published
// field-length line and exit code 1, the refused statement leaves nothing behind —
// not even the well-formed element it created before the over-long label — the
// store stays usable, and a label of exactly the maximum is accepted, which is
// what makes the refusal a statement about the BOUND rather than about "a long
// label". A genuine syntax error still writes the ordinary line.
//
// It is cheap because the reachable bound is small: 65536 bytes of label fits
// comfortably inside the 1 MiB maximum query length. The bounds that govern a
// property value are NOT reachable this way and are not attempted here
// (SPEC/GRAPH.md § Field Length Limits, rule 12).
func TestGraphClient_FieldTooLongAgainstTheRealEngine(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const name = "field-length-fence"
	t.Cleanup(setupTestGraphRoadmap(t, name))
	defer serveGraph(t, name)()

	execute := func(query string) (string, string, error) {
		t.Helper()
		var err error
		stdout, stderr := captureStdStreams(t, func() {
			err = runGraphClient([]string{"-r", name, "--query", query})
		})
		return stdout, stderr, err
	}

	// Step 1: a label grossly over any plausible bound is refused with the
	// published line, and the engine's half names the maximum in force.
	_, _, gross := execute(overLongLabelStatement(70000))
	if gross == nil {
		t.Fatal("a 70000-byte label was accepted; the engine's bound has moved beyond what this " +
			"test provokes and the figures below cannot be measured")
	}
	if !strings.HasPrefix(gross.Error(), publishedFieldTooLongHead) {
		t.Fatalf("the refusal does not write the published field-length line: %q. The engine's "+
			"server must give it one of the two codes SPEC/GRAPH.md § Field Length Limits, rule 3, "+
			"names", gross.Error())
	}
	match := walMaximumPattern.FindStringSubmatch(strings.TrimPrefix(gross.Error(), publishedFieldTooLongHead))
	if match == nil {
		t.Fatalf("the engine's half reports no maximum, so a caller could not learn what to shorten "+
			"to: %q", gross.Error())
	}
	maxLen, convErr := strconv.Atoi(match[1])
	if convErr != nil || maxLen <= 0 {
		t.Fatalf("the reported maximum %q is not a length", match[1])
	}

	// Step 2: one byte over is refused, with the class and the exit code the
	// specification fixes.
	_, _, refused := execute(overLongLabelStatement(maxLen + 1))
	if refused == nil {
		t.Fatalf("a label of %d bytes was accepted against a measured maximum of %d", maxLen+1, maxLen)
	}
	if !errors.Is(refused, utils.ErrGraphEngine) {
		t.Errorf("err = %v, want it to wrap utils.ErrGraphEngine (exit code 1)", refused)
	}
	if !strings.HasPrefix(refused.Error(), publishedFieldTooLongHead) {
		t.Errorf("one byte over the bound does not write the field-length line: %q", refused.Error())
	}

	// Step 3: nothing was written — not even the well-formed element the refused
	// statements created BEFORE the over-long label.
	stdout, _, countErr := execute("MATCH (n:FieldLengthFence) RETURN count(n) AS c")
	if countErr != nil {
		t.Fatalf("counting after the refusal failed: %v", countErr)
	}
	if !strings.Contains(stdout, `"c"`) || !strings.Contains(stdout, "0") ||
		strings.Contains(stdout, "kept") {
		t.Errorf("the refused statement left something behind: %s", stdout)
	}

	// Step 4: the store is usable. An ordinary write succeeds and is found.
	okOut, okErr, writeErr := execute("CREATE (n:FieldLengthFence {name:'after'})")
	if writeErr != nil {
		t.Fatalf("an ordinary write after the refusal failed: %v", writeErr)
	}
	if !strings.Contains(okOut, `"ok": true`) {
		t.Errorf("an ordinary write after the refusal did not report success: %s", okOut)
	}
	if okErr != "" {
		t.Errorf("the healthy write wrote to stderr: %q", okErr)
	}
	countOut, _, countErr2 := execute("MATCH (n:FieldLengthFence) RETURN count(n) AS c")
	if countErr2 != nil {
		t.Fatalf("counting after the ordinary write failed: %v", countErr2)
	}
	if !strings.Contains(countOut, "1") {
		t.Errorf("the ordinary write was not counted: %s", countOut)
	}

	// Step 5: exactly AT the maximum is accepted, so the fence is on the bound
	// and not merely on "a long label".
	_, atErr, atLimit := execute("CREATE (n:`" + strings.Repeat("A", maxLen) + "`)")
	if atLimit != nil {
		t.Errorf("a label of exactly %d bytes was refused: %v", maxLen, atLimit)
	}
	if atErr != "" {
		t.Errorf("the at-the-limit write wrote to stderr: %q", atErr)
	}

	// Step 6: a genuine syntax error still writes the parse/execution line WITH
	// the engine's own diagnostic, and not the field-length line.
	_, _, syntaxErr := execute("CREATE (n:Broken")
	if syntaxErr == nil {
		t.Fatal("a malformed statement was accepted")
	}
	if !strings.HasPrefix(syntaxErr.Error(), "graph engine error: graph query failed: ") {
		t.Errorf("a syntax error no longer writes the parse/execution line: %q", syntaxErr.Error())
	}
	if strings.Contains(syntaxErr.Error(), "graph field too long") {
		t.Errorf("a syntax error was reported as a field-length refusal: %q", syntaxErr.Error())
	}
}
