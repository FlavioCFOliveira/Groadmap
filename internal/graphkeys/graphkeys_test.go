package graphkeys

// Tests for step 2 of the node-key uniqueness audit
// (SPEC/GRAPH.md § Node Key Uniqueness, § Auditing the convention; acceptance
// criterion 61), the regression guard for rmp task #310.
//
// WHAT THE DEFECT WAS. knowledge-model.md claimed every node's key was globally
// unique and nothing in the product held that up. Two Unicode normalisations of
// one key are two nodes that render identically, and the byte-wise duplicate audit
// the project already runs groups on the STORED BYTES, so such a pair shows up as
// two groups of one and the violation is invisible. The owner chose to keep the
// invariant a convention and to make the breach detectable instead, normalising
// for COMPARISON only. This file is the detection, tested.
//
// WHY THESE TESTS CANNOT GO GREEN ON THE DRIFT THEY CHASE. Each one states its
// witness keys as explicit code points and asserts they differ in BYTES before
// asserting anything about the audit, so a typo that made two witnesses identical
// fails loudly instead of proving nothing. And the set is chosen so that the four
// plausible wrong comparisons each fail a different test:
//
//   - comparing bytes fails TestAuditReportsOneKeySpelledTwoWays,
//   - comparing case-insensitively, or after a fold, fails
//     TestAuditDoesNotTreatCaseAsANormalisation,
//   - reporting every group fails TestAuditIsSilentOnKeysThatDifferUnderNFC,
//   - reporting only multi-spelling groups fails
//     TestAuditReportsAByteIdenticalDuplicate.
//
// A comparison that satisfies all four is NFC.

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The canonical witness of the defect: one repository-relative documentation path
// spelled two ways that Unicode calls the same text.
const (
	// cafePrecomposed carries U+00C9 LATIN CAPITAL LETTER E WITH ACUTE.
	cafePrecomposed = "docs/CAFÉ.md"
	// cafeDecomposed carries U+0045 U+0301, E then COMBINING ACUTE ACCENT.
	cafeDecomposed = "docs/CAFÉ.md"
)

// A Korean path, whose two spellings are the same text by UAX #15's ALGORITHMIC
// Hangul composition rather than by any table. It is here because that arithmetic
// is a separate code path from the primary-composite lookup, and the composed-form
// assertion below is what reaches it: grouping the pair needs only decomposition,
// so a test that checked the grouping alone would pass with the arithmetic gone.
const (
	// hangulPrecomposed carries the syllables U+D55C U+AE00.
	hangulPrecomposed = "docs/한글.md"
	// hangulJamo carries the same two syllables as leading, vowel and trailing
	// jamo: U+1112 U+1161 U+11AB and U+1100 U+1173 U+11AF.
	hangulJamo = "docs/한글.md"
)

// A Vietnamese path whose two spellings carry the SAME combining marks in
// different order. Canonical ordering sorts a run of marks by combining class —
// U+0323 is class 220 and U+0302 is class 230 — so both orders reach one NFC form.
// It is here because reordering is a part of the rule that a naive
// "decompose and compare" would get right but a "compose pairs left to right"
// would not.
const (
	// vietPrecomposed carries U+1EC7 LATIN SMALL LETTER E WITH CIRCUMFLEX AND DOT BELOW.
	vietPrecomposed = "docs/hệ-thong.md"
	// vietMarksReordered carries e, COMBINING CIRCUMFLEX ACCENT, COMBINING DOT
	// BELOW — the marks in the opposite order to canonical.
	vietMarksReordered = "docs/hệ-thong.md"
)

// Ordinary keys of the shape the project's own graph uses, none of which is a
// violation of anything.
var distinctKeys = []string{
	"SPEC/GRAPH.md",
	"internal/commands/graph.go",
	"internal/web/fold.go",
	"internal/unicodenorm",
	"release-1.4.0",
}

// rowsOf builds one audit row per key, with the ids and labels step 1 would have
// returned.
func rowsOf(keys ...string) []Row {
	rows := make([]Row, 0, len(keys))
	for i, k := range keys {
		rows = append(rows, Row{Key: k, Labels: []string{"Doc"}, ID: int64(100 + i)})
	}
	return rows
}

// requireDifferentBytes fails when two witnesses are the same string, which would
// make the test that uses them prove nothing.
func requireDifferentBytes(t *testing.T, a, b string) {
	t.Helper()
	if a == b {
		t.Fatalf("the two witness spellings are byte-identical (%q); the test would be vacuous", a)
	}
}

// TestAuditReportsOneKeySpelledTwoWays is the condition the two-step audit exists
// for: two nodes whose keys are equal under NFC and different in bytes.
//
// An audit comparing stored bytes reports nothing here, which is exactly why the
// byte-wise duplicate audit cannot see this pair.
func TestAuditReportsOneKeySpelledTwoWays(t *testing.T) {
	for _, c := range []struct {
		// name identifies the pair; first and second are the two stored
		// spellings; composed is the NFC form both must reach, which is the
		// precomposed spelling in every one of these pairs.
		name, first, second, composed string
	}{
		{"latin precomposed against decomposed", cafePrecomposed, cafeDecomposed, cafePrecomposed},
		{"hangul syllables against jamo", hangulPrecomposed, hangulJamo, hangulPrecomposed},
		{"combining marks in either order", vietPrecomposed, vietMarksReordered, vietPrecomposed},
	} {
		t.Run(c.name, func(t *testing.T) {
			requireDifferentBytes(t, c.first, c.second)

			got := Audit(rowsOf(c.first, c.second))
			if len(got) != 1 {
				t.Fatalf("Audit reported %d violations for one key spelled two ways; want exactly 1", len(got))
			}
			v := got[0]
			if v.Kind != KindNormalisation {
				t.Errorf("Kind is %v; a group holding two distinct byte sequences is %v", v.Kind, KindNormalisation)
			}
			if len(v.Rows) != 2 {
				t.Errorf("the violation names %d nodes; both nodes carrying the key must be named", len(v.Rows))
			}
			if len(v.Spellings) != 2 {
				t.Errorf("the violation lists %d spellings (%q); both stored spellings must be shown, "+
					"since only the caller knows which one the artefact was meant to carry", len(v.Spellings), v.Spellings)
			}
			for _, want := range []string{c.first, c.second} {
				if !containsString(v.Spellings, want) {
					t.Errorf("the violation does not list the stored spelling %q", want)
				}
			}
			// The grouping key must be the COMPOSED form, not merely some form
			// the two spellings happen to share.
			//
			// This is the assertion that makes the pair non-vacuous. Grouping
			// alone does not need composition at all: if the rule only ever
			// decomposed, both spellings would still land together and the
			// counts above would still pass. Requiring the shared form to be the
			// precomposed spelling is what holds the composition half of NFC in
			// place — remove the Hangul arithmetic from unicodenorm.Compose and
			// the Hangul row fails here, where nothing else in this file would
			// have noticed.
			if v.NFC != c.composed {
				t.Errorf("the group's NFC form is %q; NFC composes, so both spellings must reach %q",
					v.NFC, c.composed)
			}
		})
	}
}

// TestAuditIsSilentOnKeysThatDifferUnderNFC is the other half of the rule: an
// audit that reported groups indiscriminately would drown the real finding.
func TestAuditIsSilentOnKeysThatDifferUnderNFC(t *testing.T) {
	if got := Audit(rowsOf(distinctKeys...)); len(got) != 0 {
		t.Errorf("Audit reported %d violations over %d distinct keys; want none: %+v",
			len(got), len(distinctKeys), got)
	}

	// A key written in a decomposed form is not a violation on its own. Only a
	// COLLISION is, and a lone spelling collides with nothing.
	lone := append([]string{cafeDecomposed}, distinctKeys...)
	if got := Audit(rowsOf(lone...)); len(got) != 0 {
		t.Errorf("Audit reported %d violations for a lone decomposed key; a spelling with no twin is not a collision: %+v",
			len(got), got)
	}

	if got := Audit(nil); len(got) != 0 {
		t.Errorf("Audit over no rows reported %d violations; want none", len(got))
	}
}

// TestAuditDoesNotTreatCaseAsANormalisation is the test that fixes WHICH
// comparison the audit makes.
//
// `SPEC/GRAPH.md` and `SPEC/graph.md` are two keys for two different files. They
// are equal under case folding and under lower-casing, and NOT equal under NFC,
// which is Unicode's canonical equivalence and says nothing about case. Without
// this test the comparison could be loosened into a fold — the board search folds
// as well as normalises, so a fold is one copied line away — and the audit would
// start reporting violations where the convention is intact.
func TestAuditDoesNotTreatCaseAsANormalisation(t *testing.T) {
	const upper = "SPEC/GRAPH.md"
	const lower = "SPEC/graph.md"
	requireDifferentBytes(t, upper, lower)

	// The witnesses must actually be a case pair, or the test would pass for the
	// wrong reason.
	if !strings.EqualFold(upper, lower) {
		t.Fatalf("%q and %q are not the same text under case folding; the test does not pin what it claims to", upper, lower)
	}

	if got := Audit(rowsOf(upper, lower)); len(got) != 0 {
		t.Errorf("Audit reported %d violations for two keys that differ only in case; the comparison is NFC, "+
			"which is canonical equivalence and not a fold: %+v", len(got), got)
	}

	// The same holds for a Turkish dotted capital I, which is where a fold and a
	// normalisation visibly disagree: U+0130 decomposes to U+0049 U+0307 under
	// NFD but recomposes to U+0130, and folds to U+0069.
	const dotted = "docs/İstanbul.md"
	const folded = "docs/i̇stanbul.md"
	requireDifferentBytes(t, dotted, folded)
	if got := Audit(rowsOf(dotted, folded)); len(got) != 0 {
		t.Errorf("Audit reported %d violations for U+0130 against its folded spelling; folding is not the audit's "+
			"comparison: %+v", len(got), got)
	}
}

// TestAuditReportsAByteIdenticalDuplicate covers the trivial case of the same
// invariant: two nodes carrying one key, byte for byte.
//
// It is reported rather than filtered out. The audit has already computed the
// group, the invariant is equally broken, and staying silent would mean returning
// a strict subset of what was found. Kind separates it from the normalisation case
// so a report can say which audit would also have seen it.
func TestAuditReportsAByteIdenticalDuplicate(t *testing.T) {
	got := Audit(rowsOf("SPEC/GRAPH.md", "internal/web/fold.go", "SPEC/GRAPH.md"))
	if len(got) != 1 {
		t.Fatalf("Audit reported %d violations for one key carried by two nodes; want exactly 1", len(got))
	}
	v := got[0]
	if v.Kind != KindIdentical {
		t.Errorf("Kind is %v; a group holding one byte sequence and several nodes is %v", v.Kind, KindIdentical)
	}
	if len(v.Spellings) != 1 {
		t.Errorf("the violation lists %d spellings; a byte-identical duplicate has exactly one", len(v.Spellings))
	}
	if len(v.Rows) != 2 {
		t.Fatalf("the violation names %d nodes; both must be named", len(v.Rows))
	}
	if v.Rows[0].ID == v.Rows[1].ID {
		t.Error("the violation names one node twice; the ids must tell the nodes of a group apart")
	}
}

// TestAuditReportsEveryGroupAndKeepsItsOrderStable pins that a graph carrying more
// than one violation reports all of them, deterministically. An audit whose output
// moved between runs could not be compared across runs, which is what step 1's
// ORDER BY exists to make possible.
func TestAuditReportsEveryGroupAndKeepsItsOrderStable(t *testing.T) {
	keys := []string{
		cafePrecomposed,
		"SPEC/GRAPH.md",
		hangulPrecomposed,
		cafeDecomposed,
		"internal/commands/graph.go",
		hangulJamo,
	}

	first := Audit(rowsOf(keys...))
	if len(first) != 2 {
		t.Fatalf("Audit reported %d violations over two colliding pairs among six keys; want 2", len(first))
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].NFC >= first[i].NFC {
			t.Errorf("violations are not ordered by NFC form: %q then %q", first[i-1].NFC, first[i].NFC)
		}
	}

	second := Audit(rowsOf(keys...))
	if len(second) != len(first) {
		t.Fatalf("two runs over one input reported %d and %d violations", len(first), len(second))
	}
	for i := range first {
		if first[i].NFC != second[i].NFC || first[i].Kind != second[i].Kind {
			t.Errorf("violation %d differs between runs: %v/%v then %v/%v",
				i, first[i].NFC, first[i].Kind, second[i].NFC, second[i].Kind)
		}
	}

	// Rows keep the order step 1 returned them in, which is what lets a reader
	// match a reported node against the query output.
	for _, v := range first {
		for i := 1; i < len(v.Rows); i++ {
			if v.Rows[i-1].ID >= v.Rows[i].ID {
				t.Errorf("the rows of a violation are not in the order they arrived: id %d before id %d",
					v.Rows[i-1].ID, v.Rows[i].ID)
			}
		}
	}
}

// TestRowsFromResolvesColumnsByName holds the adapter between the two steps to
// resolving by column NAME. Reading by position would let a reordered RETURN list
// audit the label column, and an audit run over the wrong column is worse than one
// that did not run.
func TestRowsFromResolvesColumnsByName(t *testing.T) {
	columns := []string{"labels", "key", "id"}
	raw := [][]any{
		{[]any{"Doc"}, cafePrecomposed, int64(41)},
		{[]string{"Doc", "Spec"}, cafeDecomposed, float64(42)},
	}

	rows, err := RowsFrom(columns, raw)
	if err != nil {
		t.Fatalf("RowsFrom over a reordered result: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("RowsFrom returned %d rows for 2", len(rows))
	}
	if rows[0].Key != cafePrecomposed || rows[1].Key != cafeDecomposed {
		t.Errorf("RowsFrom read the keys by position: got %q and %q", rows[0].Key, rows[1].Key)
	}
	if rows[0].ID != 41 || rows[1].ID != 42 {
		t.Errorf("RowsFrom read the ids as %d and %d; want 41 and 42", rows[0].ID, rows[1].ID)
	}
	if len(rows[1].Labels) != 2 {
		t.Errorf("RowsFrom dropped labels: %q", rows[1].Labels)
	}

	if got := Audit(rows); len(got) != 1 {
		t.Errorf("the adapted rows produced %d violations; the pair must survive the adaptation", len(got))
	}
}

// TestRowsFromRefusesAResultItCannotAudit pins that a result missing a column is
// an error rather than a partial audit, and that the refusal is carried by the
// sentinel ARCHITECTURE.md § Sentinel Error Catalogue requires. A bare
// non-nil check would pass for an error the CLI could not classify.
func TestRowsFromRefusesAResultItCannotAudit(t *testing.T) {
	for _, c := range []struct {
		name    string
		columns []string
		rows    [][]any
	}{
		{"no key column", []string{"id", "labels"}, [][]any{{int64(1), []any{"Doc"}}}},
		{"no id column", []string{"labels", "key"}, [][]any{{[]any{"Doc"}, "SPEC/GRAPH.md"}}},
		{"no labels column", []string{"id", "key"}, [][]any{{int64(1), "SPEC/GRAPH.md"}}},
		{"row shorter than the columns", []string{"id", "labels", "key"}, [][]any{{int64(1), []any{"Doc"}}}},
		{"key is not a string", []string{"id", "labels", "key"}, [][]any{{int64(1), []any{"Doc"}, 7}}},
		{"id is not a number", []string{"id", "labels", "key"}, [][]any{{"one", []any{"Doc"}, "SPEC/GRAPH.md"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := RowsFrom(c.columns, c.rows)
			if err == nil {
				t.Fatalf("RowsFrom accepted a result it cannot audit")
			}
			if !errors.Is(err, utils.ErrInvalidInput) {
				t.Errorf("the refusal is %v; it must originate from utils.ErrInvalidInput", err)
			}
		})
	}
}

// containsString reports whether haystack holds needle.
func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
