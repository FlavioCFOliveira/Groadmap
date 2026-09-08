// Regression fence for the field-length refusal on `rmp graph client`
// (rmp task #413, written against `rmp graph execute` before it was withdrawn).
//
// Before this class existed, a statement writing a label or a property key
// longer than the write-ahead log's length prefix could carry reached the caller
// on the SAME line as a Cypher syntax error:
//
//	Error: graph engine error: graph query failed: <engine diagnostic>
//
// The two conditions were separated only by that diagnostic tail, which
// SPEC/GRAPH.md § Error Handling and Exit Codes, rule 2, deliberately declines to
// specify and which a caller therefore cannot lawfully match. A caller had to
// parse English to learn whether to correct the statement's syntax or to shorten
// one of its values — two opposite actions behind one line.
//
// This file fences the class in both directions, because only one of the two
// directions is obvious. Reporting the new line for the new condition is the
// easy half; NOT reporting it for the neighbouring conditions is the half an
// implementation gets wrong, and SPEC/GRAPH.md § Field Length Limits, rule 10,
// names three of them explicitly:
//
//   - a Cypher statement the engine simply refuses,
//   - an over-long node KEY, refused by the engine's node-key codec under no
//     sentinel at all,
//   - an assembled log FRAME over the log's frame ceiling, refused under
//     store/wal.ErrFrameTooLarge.
//
// Each is a length refusal in spirit and none is this class, because the class is
// the sentinel a caller matches and not the shape of the complaint. The
// fabricated-error cases below are what keep them apart, and they are fabricated
// deliberately: a test that drove the real conditions would need a 4 GiB node key
// and a 1 GiB frame, and would measure the machine rather than the product
// (SPEC/GRAPH.md § Field Length Limits, rule 12).
package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	ggtxn "github.com/FlavioCFOliveira/GoGraph/store/txn"
	ggwal "github.com/FlavioCFOliveira/GoGraph/store/wal"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
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

// walFieldRefusal fabricates what the engine returns for a field the write-ahead
// log refuses, in the shape it actually arrives in: the guard's own message,
// wrapped by the commit path ("cypher: commit WAL: %w"). Measured against the
// pinned engine with a 65536-byte label, the real string is
//
//	cypher: commit WAL: txn: field too long for its WAL length prefix: label is 65536 bytes, maximum 65535
//
// and the value below is that string with the sentinel wrapped rather than
// spelled, so errors.Is finds it exactly as it does in production.
func walFieldRefusal(what string, n, max int) error {
	return fmt.Errorf("cypher: commit WAL: %w: %s is %d bytes, maximum %d",
		ggtxn.ErrFieldTooLong, what, n, max)
}

// TestGraphStatementError_FieldTooLong is the positive half: the class is
// recognised, worded as published, and carries the sentinel and exit code the
// ordinary engine failure carries.
func TestGraphStatementError_FieldTooLong(t *testing.T) {
	refusal := walFieldRefusal("label", 65536, 65535)

	// Every arrival point classifies identically. The refusal reaches
	// graphStatementError from the engine call in production — RunAny routes a
	// writing statement to RunInTx, which commits before it returns — but a
	// refusal that landed on the commit instead is the same failure and must not
	// be worded as a different one.
	for _, stage := range []string{"graph query failed", "graph commit failed"} {
		t.Run("a refusal at "+stage, func(t *testing.T) {
			err := graphStatementError(graphlock.DefaultStatementBudget, stage, refusal)

			if !errors.Is(err, utils.ErrGraphEngine) {
				t.Errorf("err = %v, want it to wrap utils.ErrGraphEngine (exit code 1)", err)
			}
			want := publishedFieldTooLongHead + refusal.Error()
			if got := err.Error(); got != want {
				t.Errorf("published line\n got:  %q\n want: %q", got, want)
			}
			if strings.Contains(err.Error(), stage) {
				t.Errorf("err = %q: the refusal must not be reported as %q, which says "+
					"nothing about the field", err.Error(), stage)
			}
		})
	}

	t.Run("the engine's diagnostic ends the line, unchanged and untrimmed", func(t *testing.T) {
		err := graphStatementError(graphlock.DefaultStatementBudget, "graph query failed", refusal)
		if !strings.HasSuffix(err.Error(), refusal.Error()) {
			t.Fatalf("err = %q, want it to END in the engine's own diagnostic %q",
				err.Error(), refusal.Error())
		}
		// The half rmp owns is everything before it, and nothing else.
		head := strings.TrimSuffix(err.Error(), refusal.Error())
		if head != publishedFieldTooLongHead {
			t.Errorf("rmp's half of the line\n got:  %q\n want: %q", head, publishedFieldTooLongHead)
		}
		// The echo is deliberate: both halves say the field is too long, and the
		// specification forbids tidying it away, because trimming the engine's
		// half would mean parsing it (rule 4).
		if !strings.Contains(refusal.Error(), "too long") {
			t.Fatalf("the fabricated engine diagnostic %q no longer resembles the engine's, "+
				"so this case no longer fences the echo", refusal.Error())
		}
	})

	t.Run("the field kind reaches the caller whatever it is", func(t *testing.T) {
		// The engine names four kinds a Cypher statement can drive here: a node
		// or edge label and a node or edge property key (rule 11). rmp's half
		// names none of them and must not: it defers to the engine's, which is
		// the only one that knows.
		for _, kind := range []string{"label", "node property key", "edge label", "edge property key"} {
			r := walFieldRefusal(kind, 65536, 65535)
			err := graphStatementError(graphlock.DefaultStatementBudget, "graph query failed", r)
			if !strings.Contains(err.Error(), kind) {
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

// TestGraphStatementError_NeighbouringRefusalsAreNotFolded is the negative half,
// and it is the one that fails on a text-matching implementation.
//
// Each case below is a failure that IS about a length and is NOT this class. An
// implementation that recognised the condition by searching the engine's message
// for "too long" — which SPEC/GRAPH.md § Field Length Limits, rule 3, forbids —
// passes every assertion of the positive test above and fails these.
func TestGraphStatementError_NeighbouringRefusalsAreNotFolded(t *testing.T) {
	cases := []struct {
		err  error
		name string
		why  string
	}{
		{
			name: "an over-long node key",
			// The engine's node-key codec refuses a key over the log's unsigned
			// 32-bit prefix with a plain error that wraps no sentinel at all.
			// The message is deliberately shaped like the guard's, so a text
			// match would swallow it (rule 10).
			err: errors.New("cypher: commit WAL: stringCodec: key too long: 4294967296 bytes, maximum 4294967295"),
			why: "it wraps no sentinel, so a caller matching the class would be told to " +
				"shorten a field the class does not cover",
		},
		{
			name: "an assembled frame over the log's frame ceiling",
			// Reachable from a list property whose elements are each
			// individually legal, so it is not a field-length condition at all
			// even though every part of it is a length (rule 10).
			err: fmt.Errorf("cypher: commit WAL: %w: 1073741825 bytes", ggwal.ErrFrameTooLarge),
			why: "the sentinel is the log framer's, not the field guard's",
		},
		{
			name: "an ordinary parse failure",
			err:  errors.New("cypher: parse: parse error at 1:16, expected one of {'CALL', ...}"),
			why:  "it is the condition the class exists to be distinguishable FROM",
		},
		{
			name: "the engine's own words with no sentinel behind them",
			// The exact production text, carrying no sentinel. It exists to
			// prove the recognition is errors.Is and not strings.Contains: this
			// case is indistinguishable from the real refusal by text alone.
			err: errors.New("cypher: commit WAL: txn: field too long for its WAL length prefix: " +
				"label is 65536 bytes, maximum 65535"),
			why: "recognition is by sentinel, never by the engine's wording (rule 3)",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := graphStatementError(graphlock.DefaultStatementBudget, "graph query failed", c.err)
			if !errors.Is(err, utils.ErrGraphEngine) {
				t.Errorf("err = %v, want it to wrap utils.ErrGraphEngine", err)
			}
			want := "graph engine error: graph query failed: " + c.err.Error()
			if got := err.Error(); got != want {
				t.Errorf("%s must keep the ordinary parse/execution line (%s)\n got:  %q\n want: %q",
					c.name, c.why, got, want)
			}
			if strings.Contains(err.Error(), "graph field too long") {
				t.Errorf("%s was folded into the field-length class (%s): %q", c.name, c.why, err.Error())
			}
		})
	}
}

// TestGraphStatementError_BudgetStillWinsOverEverything fences the ORDER of the
// two classifications against each other.
//
// A cut statement and a refused field are different actions for the caller —
// narrow the statement, or shorten a value — and the budget's line is checked
// first. Nothing in the engine produces both at once, so this is a fence against
// a future edit reordering the branches rather than a live condition.
func TestGraphStatementError_BudgetStillWinsOverEverything(t *testing.T) {
	both := fmt.Errorf("cypher: %w: and %w", context.DeadlineExceeded, ggtxn.ErrFieldTooLong)
	err := graphStatementError(150*time.Millisecond, "graph query failed", both)
	if got, want := err.Error(), wantBudgetLine(150*time.Millisecond); got != want {
		t.Errorf("a deadline must still select the budget line\n got:  %q\n want: %q", got, want)
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
// against the real engine, in the two places the condition is now observable,
// and it is the one test in this file that can fail on something the fabricated
// cases cannot see.
//
// # What the served path can and cannot report, MEASURED
//
// The engine's Bolt server does not carry this refusal's diagnostic across the
// protocol. Measured against the pinned engine through a running server, a
// 70000-byte label comes back as
//
//	graph engine error: graph query failed: An internal error occurred. See server logs for details (session: <id>).
//
// while an ordinary parse failure and an ordinary execution failure both cross
// with their full text — `cypher: parse: parse error at 1:16, expected one of
// {...}` and `exec: DropIndex "nope": index: no index by that name: "nope"`. The
// server classifies this one as an internal error and replaces its message.
//
// Two things follow, and both are stated rather than worked around. The published
// field-length line has NO PRODUCER on the served path: the sentinel does not
// cross a protocol, and neither does the engine's diagnostic that names the field
// and the two figures. And what a caller sees is the ordinary parse-or-execution
// line, carrying nothing it can act on beyond "the statement failed". The
// specification retains the line (SPEC/GRAPH.md § Field Length Limits, rules 2 to
// 5); nothing produces it while the only route to a graph is a server that
// replaces the message. This is recorded here, in
// tests/test_55_error_string_parity.py's exemption for the same line, and nowhere
// else — matching the engine's replacement text to recover the class is exactly
// what rule 3 forbids.
//
// # What is therefore asserted, and in which of the two places
//
// The bound is measured IN PROCESS, against a store this test opens itself, which
// is the one place the sentinel and the diagnostic both survive. That half is not
// a convenience: it is the live check that the engine still wraps
// store/txn.ErrFieldTooLong at all, which every fabricated case in this file
// assumes and none can verify. A version bump that stopped wrapping it, or a layer
// that re-wrapped it with %v, would leave those cases passing and this one failing.
//
// The BEHAVIOUR is then asserted through the CLI, against a server, at the
// measured bound: one byte over is refused with exit code 1, the refused statement
// leaves nothing behind — not even the well-formed element it created before the
// over-long label — the store stays usable, and a label of exactly the maximum is
// accepted. The last of those is what makes the refusal a statement about the
// BOUND rather than about "a long label".
//
// It is cheap because the reachable bound is small: 65536 bytes of label fits
// comfortably inside the 1 MiB maximum query length, and the whole case costs
// milliseconds. The bounds that govern a property value are NOT reachable this way
// and are not attempted here — a gigabyte of literal does not fit in a statement,
// and a test that tried would measure the machine
// (SPEC/GRAPH.md § Field Length Limits, rule 12).
func TestGraphClient_FieldTooLongAgainstTheRealEngine(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const name = "field-length-fence"
	t.Cleanup(setupTestGraphRoadmap(t, name))

	// Step 1, in process and before any server exists: provoke a refusal with a
	// label grossly over any plausible bound, confirm the engine still carries
	// the sentinel, and read the maximum the engine itself reports. Nothing below
	// is a literal.
	maxLen := measureFieldLengthBound(t, name)

	// Everything from here runs through the command, against a server over the
	// same store.
	defer serveGraph(t, name)()

	execute := func(query string) (string, string, error) {
		t.Helper()
		var err error
		stdout, stderr := captureStdStreams(t, func() {
			err = runGraphClient([]string{"-r", name, "--query", query})
		})
		return stdout, stderr, err
	}

	// Step 2: one byte over is refused, with the class and the exit code the
	// specification fixes.
	_, _, refused := execute(overLongLabelStatement(maxLen + 1))
	if refused == nil {
		t.Fatalf("a label of %d bytes was accepted against a measured maximum of %d",
			maxLen+1, maxLen)
	}
	if !errors.Is(refused, utils.ErrGraphEngine) {
		t.Errorf("err = %v, want it to wrap utils.ErrGraphEngine (exit code 1)", refused)
	}
	// The line a caller actually gets. It is the ordinary parse-or-execution one,
	// for the reason this test's documentation measures; asserting it is what
	// makes the loss visible rather than merely absent.
	if !strings.HasPrefix(refused.Error(), "graph engine error: graph query failed: ") {
		t.Errorf("the refusal does not write the parse/execution line: %q", refused.Error())
	}

	// Step 3: nothing was written — not even the well-formed element the refused
	// statement created BEFORE the over-long label.
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
	// the engine's own diagnostic. This is the non-vacuity control for the
	// measurement above: it establishes that the protocol carries an engine
	// diagnostic in general, so the field-length refusal's replacement message is
	// a property of THAT class and not of every failure.
	_, _, syntaxErr := execute("CREATE (n:Broken")
	if syntaxErr == nil {
		t.Fatal("a malformed statement was accepted")
	}
	if !strings.HasPrefix(syntaxErr.Error(), "graph engine error: graph query failed: ") {
		t.Errorf("a syntax error no longer writes the parse/execution line: %q", syntaxErr.Error())
	}
	if !strings.Contains(syntaxErr.Error(), "parse") {
		t.Errorf("the engine's own parse diagnostic no longer crosses the protocol: %q. If EVERY "+
			"engine failure is now replaced by the server, the measurement this test's "+
			"documentation records is stale and the field-length line's loss is no longer specific "+
			"to that class", syntaxErr.Error())
	}
	if strings.Contains(syntaxErr.Error(), "graph field too long") {
		t.Errorf("a syntax error was reported as a field-length refusal: %q", syntaxErr.Error())
	}
}

// measureFieldLengthBound opens the roadmap's store in this process, provokes the
// write-ahead log's length refusal, and returns the maximum the engine reported.
//
// It is the only place left where the whole of the condition is observable: the
// engine wraps store/txn.ErrFieldTooLong around the refusal and formats the field
// kind and both figures into its message, and neither the sentinel nor the message
// survives the Bolt server (see the caller's documentation for the measurement).
// Opening the store directly is lawful here for one reason and only that reason:
// no server is running yet — the caller starts one afterwards, over the store this
// leaves behind.
//
// Three things are asserted on the way, and each is a live check the fabricated
// cases in this file cannot make: that the engine still refuses a grossly
// over-long label at all, that graphstore.CommitRefusedFieldTooLong still
// recognises the real refusal, and that the message still names a maximum a caller
// could shorten to.
func measureFieldLengthBound(t *testing.T, roadmap string) int {
	t.Helper()

	graphDir := graphDirOf(t, roadmap)
	if err := os.MkdirAll(graphDir, 0700); err != nil {
		t.Fatalf("creating %s: %v", graphDir, err)
	}
	st, err := graphstore.Open(graphDir)
	if err != nil {
		t.Fatalf("opening the graph store at %s: %v", graphDir, err)
	}
	defer st.Close() //nolint:errcheck // the measurement is what matters; the close releases the hold

	result, runErr := st.Engine().RunInTx(context.Background(), overLongLabelStatement(70000), nil)
	if runErr == nil {
		for result.Next() { //nolint:revive // drain so Close performs the commit that is refused
		}
		runErr = result.Err()
		if runErr == nil {
			runErr = result.Close()
		}
	}
	if runErr == nil {
		t.Fatal("a 70000-byte label was accepted; the engine's bound has moved beyond what this " +
			"test provokes and the figures below cannot be measured")
	}
	if !graphstore.CommitRefusedFieldTooLong(runErr) {
		t.Fatalf("the real engine's refusal is no longer recognised by "+
			"graphstore.CommitRefusedFieldTooLong: %v. Every fabricated case in this file assumes "+
			"the engine wraps store/txn.ErrFieldTooLong, and this is the only test that checks it",
			runErr)
	}

	match := walMaximumPattern.FindStringSubmatch(runErr.Error())
	if match == nil {
		t.Fatalf("the refusal reports no maximum, so a caller could not learn what to shorten "+
			"to: %q", runErr.Error())
	}
	maxLen, convErr := strconv.Atoi(match[1])
	if convErr != nil || maxLen <= 0 {
		t.Fatalf("the reported maximum %q is not a length", match[1])
	}
	return maxLen
}
