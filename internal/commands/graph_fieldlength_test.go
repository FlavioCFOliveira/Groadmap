// Regression fence for the field-length refusal on `rmp graph execute`
// (rmp task #413).
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
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	ggsnap "github.com/FlavioCFOliveira/GoGraph/store/snapshot"
	ggtxn "github.com/FlavioCFOliveira/GoGraph/store/txn"
	ggwal "github.com/FlavioCFOliveira/GoGraph/store/wal"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
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

// TestGraphCheckpointWarning fences the SECOND half of the condition: a field
// that commits and is then refused by every checkpoint.
//
// It reaches the caller as a diagnostic beside exit code 0 rather than as an
// error, so no literal for it is published anywhere and what is fixed is the
// CONTENT (SPEC/GRAPH.md § Field Length Limits, rule 9). The four things it must
// carry are asserted below by what they say, not by their wording.
func TestGraphCheckpointWarning(t *testing.T) {
	t.Run("an ordinary checkpoint failure keeps the general warning", func(t *testing.T) {
		err := errors.New("snapshot write: mkdir /roadmaps/x/graph/snapshot.tmp: no space left on device")
		got := graphCheckpointWarning(err)
		want := "Warning: graph checkpoint failed: " + err.Error()
		if got != want {
			t.Errorf("\n got:  %q\n want: %q", got, want)
		}
	})

	t.Run("a refused field is reported as the condition that cannot heal", func(t *testing.T) {
		err := fmt.Errorf("snapshot write: %w: property value is 2147483648 bytes, maximum 1073741824",
			ggsnap.ErrFieldTooLong)
		got := graphCheckpointWarning(err)

		if !strings.HasPrefix(got, "Warning: ") {
			t.Errorf("the diagnostic lost its stderr prefix: %q", got)
		}
		// Rule 9's four contents. Each is asserted by a phrase that carries the
		// MEANING; a rewording that kept the meaning keeps these passing, and a
		// rewrite that dropped one of the four does not.
		for _, required := range []struct{ why, fragment string }{
			{"the commits are still durable", "still durable in the write-ahead log"},
			{"recovery still restores them", "the next open recovers it"},
			{"the log was not folded", "was not folded into the snapshot"},
			{"the log therefore grows", "keeps growing"},
			{"the next open replays more of it", "replays more of it"},
			{"the condition persists through every later checkpoint", "every later checkpoint"},
			{"the remedy is to shorten or remove the field", "shortens or removes the field"},
		} {
			if !strings.Contains(got, required.fragment) {
				t.Errorf("the diagnostic does not say %s (looked for %q):\n%s",
					required.why, required.fragment, got)
			}
		}
		// The general warning's promise is the one thing it must NOT make.
		if strings.Contains(got, "graph checkpoint failed") {
			t.Errorf("the unhealable condition was reported as the general failure: %q", got)
		}
		// The engine's own error ends it, because it names which field.
		if !strings.HasSuffix(got, err.Error()) {
			t.Errorf("the diagnostic does not end in the engine's own error:\n%s", got)
		}
	})
}

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

// TestGraphExecute_FieldTooLongAgainstTheRealEngine drives the whole path — the
// engine, the wrapping every layer between it and the classifier applies, the
// published line, and the store afterwards — with a REAL over-long field.
//
// Every other test in this file fabricates the engine's error, which is what
// makes them cheap and what makes them blind to the one thing they cannot
// fabricate: whether the engine still wraps the sentinel at all. A version bump
// that stopped wrapping it, or a layer that re-wrapped it with %v instead of %w,
// would leave every fabricated case passing and silently return the condition to
// the parse/execution line it was separated from. This is the test that fails
// then.
//
// It is cheap because the reachable bound is small: 65536 bytes of label fits
// comfortably inside the 1 MiB maximum query length, and the whole case costs
// milliseconds. The bounds that govern a property value are NOT reachable this
// way and are not attempted here — a gigabyte of literal does not fit in a
// statement, and a test that tried would measure the machine
// (SPEC/GRAPH.md § Field Length Limits, rule 12).
func TestGraphExecute_FieldTooLongAgainstTheRealEngine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const name = "field-length-fence"
	t.Cleanup(setupTestGraphRoadmap(t, name))

	execute := func(query string) (string, string, error) {
		t.Helper()
		var err error
		stdout, stderr := captureStdStreams(t, func() {
			err = runGraphExecute([]string{"-r", name, "--query", query})
		})
		return stdout, stderr, err
	}

	// Step 1: provoke a refusal with a label grossly over any plausible bound,
	// and read the maximum the engine itself reports. Nothing below is a literal.
	_, _, provoked := execute(overLongLabelStatement(70000))
	if provoked == nil {
		t.Fatal("a 70000-byte label was accepted; the engine's bound has moved beyond what " +
			"this test provokes and the figures below cannot be derived")
	}
	match := walMaximumPattern.FindStringSubmatch(provoked.Error())
	if match == nil {
		t.Fatalf("the refusal reports no maximum, so a caller cannot learn what to shorten "+
			"to: %q", provoked.Error())
	}
	maxLen, convErr := strconv.Atoi(match[1])
	if convErr != nil || maxLen <= 0 {
		t.Fatalf("the reported maximum %q is not a length", match[1])
	}

	// Step 2: one byte over. The published line, the sentinel, and the engine's
	// own figures.
	_, _, refused := execute(overLongLabelStatement(maxLen + 1))
	if refused == nil {
		t.Fatalf("a label of %d bytes was accepted against a reported maximum of %d",
			maxLen+1, maxLen)
	}
	if !errors.Is(refused, utils.ErrGraphEngine) {
		t.Errorf("err = %v, want it to wrap utils.ErrGraphEngine (exit code 1)", refused)
	}
	if !strings.HasPrefix(refused.Error(), publishedFieldTooLongHead) {
		t.Fatalf("the real engine's refusal does not produce the published line\n got:  %q\n"+
			" want prefix: %q", refused.Error(), publishedFieldTooLongHead)
	}
	tail := strings.TrimPrefix(refused.Error(), publishedFieldTooLongHead)
	for _, fragment := range []string{
		"label",                  // the field kind, which rmp's half deliberately does not name
		strconv.Itoa(maxLen + 1), // the length the field occupies
		strconv.Itoa(maxLen),     // the maximum in force
	} {
		if !strings.Contains(tail, fragment) {
			t.Errorf("the engine's diagnostic lost %q: %q", fragment, tail)
		}
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

	// Step 6: a genuine syntax error still writes the parse/execution line. An
	// implementation that routed every engine failure to the new line would pass
	// every check above and fail this one.
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
