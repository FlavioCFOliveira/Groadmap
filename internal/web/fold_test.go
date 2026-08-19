package web

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The whole of Unicode, which is what the guard below sweeps.
//
// unicodeScalarValues is not a magic number: it is 0x110000 code points less the
// 2048 surrogates, which are not scalar values, carry no case mapping, and can
// therefore appear in no run. Stating it as a constant and counting the sweep
// against it is what makes "every code point, not a sample" checkable rather than
// merely claimed (SPEC/WEB.md Acceptance Criterion 119).
const (
	unicodeMaxCodePoint = 0x10FFFF
	surrogateFirst      = 0xD800
	surrogateLast       = 0xDFFF
	unicodeScalarValues = (unicodeMaxCodePoint + 1) - (surrogateLast - surrogateFirst + 1)
)

// maxReportedFoldMismatches caps the failure output: a rule that moved wholesale
// would otherwise print a million lines and say less than a dozen would.
const maxReportedFoldMismatches = 12

// foldRun is one span of the shipped mapping: every code point in
// [start, start+length) folds to itself plus delta.
type foldRun struct {
	start  int
	length int
	delta  int
}

// TestTaskSearchScript_FoldTableIsTheServerFold is the gate for Acceptance
// Criterion 119, and the whole reason the client stopped consulting the browser's
// case tables.
//
// The client no longer folds a term with the JavaScript platform's case
// conversion — which is Unicode's Default Case Conversion, differing from the
// folding rule on U+0130 and U+03A3, and whose tables are of whatever Unicode
// version the browser ships. It folds the term with the mapping the server ships
// it. That mapping is a second carrier of the server's rule, and two carriers
// drift unless something compares them. This is that something, and it compares:
//
//   - the SHIPPED table, extracted from the script the binary actually serves,
//     never a copy of it kept in the test;
//   - against the SERVER'S OWN foldSearch, never against unicode.ToLower directly
//     and never against a stored table of expected results — a stored copy can be
//     updated to match a fold that changed, and would then prove nothing, so a
//     server that changes its rule MUST fail here;
//   - over EVERY code point of Unicode, all unicodeScalarValues of them, applied
//     through the same binary search the script performs, so a table that is
//     mis-ordered, overlapping, truncated or corrupt cannot pass;
//   - including when a toolchain upgrade moves a mapping: Go's unicode tables are
//     of the toolchain's Unicode version, so a bump changes foldSearch, this test
//     names the code points that moved, and `go generate ./internal/web/` is then
//     the fix rather than the detection.
func TestTaskSearchScript_FoldTableIsTheServerFold(t *testing.T) {
	script := readEmbeddedAsset(t, "static/task-search.js")

	// AC 119, the absence: the served script calls no case conversion of the
	// platform at all. Asserted on the RAW asset, comments included, the way
	// Acceptance Criterion 97 asserts the modal script's markup sinks — a
	// comment that still named one would mean the file had drifted back.
	for _, conversion := range []string{"toLowerCase", "toLocaleLowerCase"} {
		if strings.Contains(script, conversion) {
			t.Errorf("the served script names %s; the client must fold the term with the "+
				"server's shipped mapping and consult no case table of the browser's", conversion)
		}
	}

	// AC 118, the second absence: NOTHING is rewritten after the mapping. A
	// U+03C2 the user typed is already lower case and stays U+03C2, so a term of
	// "οδός" keeps finding a task titled "οδός" — a post-fold rewrite of U+03C2
	// to U+03C3, or any other fixup, would lose it. The table check below cannot
	// see such a rewrite, because a rewrite lives in the script rather than in
	// the table, so it is asserted here as an absence: the narrowing script
	// performs no string replacement, no Unicode normalisation, and no
	// locale-aware comparison of any kind.
	for _, rewrite := range []string{".replace(", ".replaceAll(", ".normalize(", "Intl.", "localeCompare"} {
		if strings.Contains(script, rewrite) {
			t.Errorf("the served script uses %s; nothing may be rewritten after the mapping, "+
				"and no locale may enter the fold", rewrite)
		}
	}

	shipped := scriptFoldTable(t, script)

	// The structure the script's binary search depends on: ordered, disjoint,
	// non-empty spans that move a code point somewhere and never cover a
	// surrogate.
	for i, run := range shipped {
		switch {
		case run.length < 1:
			t.Errorf("run %d spans %d code points, want at least 1: %+v", i, run.length, run)
		case run.delta == 0:
			t.Errorf("run %d folds its span to itself, which needs no entry: %+v", i, run)
		case run.start < 0 || run.start+run.length > unicodeMaxCodePoint+1:
			t.Errorf("run %d leaves Unicode: %+v", i, run)
		case run.start < surrogateLast+1 && run.start+run.length > surrogateFirst:
			t.Errorf("run %d covers surrogates, which are not scalar values: %+v", i, run)
		}
		if i > 0 && shipped[i-1].start+shipped[i-1].length > run.start {
			t.Fatalf("runs %d and %d are out of order or overlap: %+v then %+v; the script's "+
				"binary search would answer for whichever half it landed in",
				i-1, i, shipped[i-1], run)
		}
	}

	// EVERY code point, folded through the shipped table exactly as the script
	// folds it, against the server's own function.
	swept, mismatches := 0, 0
	for cp := 0; cp <= unicodeMaxCodePoint; cp++ {
		if cp >= surrogateFirst && cp <= surrogateLast {
			continue
		}
		swept++
		got := string(rune(applyFoldTable(shipped, cp)))
		want := foldSearch(string(rune(cp)))
		if got == want {
			continue
		}
		mismatches++
		if mismatches <= maxReportedFoldMismatches {
			t.Errorf("U+%04X: the shipped table folds to %q, the server's foldSearch to %q",
				cp, got, want)
		}
	}
	if mismatches > maxReportedFoldMismatches {
		t.Errorf("%d code points fold differently on the two sides; the first %d are above",
			mismatches, maxReportedFoldMismatches)
	}
	if swept != unicodeScalarValues {
		t.Errorf("the sweep covered %d code points, want the whole of Unicode: %d",
			swept, unicodeScalarValues)
	}

	// The table's SIZE and shape, derived from the server rather than stored: the
	// run encoding of foldSearch is canonical, so a table with a run too few, a
	// run too many, or a truncated tail fails here even where the sweep above
	// happened to agree.
	canonical := serverFoldRuns()
	if len(shipped) != len(canonical) {
		t.Fatalf("the shipped table has %d runs, want the %d the server's fold run-encodes to",
			len(shipped), len(canonical))
	}
	for i := range canonical {
		if shipped[i] != canonical[i] {
			t.Errorf("run %d is %+v, want %+v", i, shipped[i], canonical[i])
		}
	}
	if got, want := foldRunsCoverage(shipped), foldRunsCoverage(canonical); got != want {
		t.Errorf("the shipped table maps %d code points, want %d", got, want)
	}
}

// applyFoldTable folds one code point through the shipped table by binary search,
// mirroring static/task-search.js's foldCodePoint step for step. Mirroring it
// rather than scanning linearly is deliberate: a mis-ordered table then produces
// the wrong answer here exactly as it would in the browser.
func applyFoldTable(runs []foldRun, cp int) int {
	lo, hi := 0, len(runs)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case cp < runs[mid].start:
			hi = mid - 1
		case cp >= runs[mid].start+runs[mid].length:
			lo = mid + 1
		default:
			return cp + runs[mid].delta
		}
	}
	return cp
}

// serverFoldRuns run-encodes the SERVER's folding rule over the whole of Unicode,
// by asking foldSearch itself. It is the expectation the shipped table's size and
// shape are held to, and it is computed rather than stored precisely so that a
// change to foldSearch moves it.
func serverFoldRuns() []foldRun {
	var runs []foldRun
	for cp := 0; cp <= unicodeMaxCodePoint; cp++ {
		if cp >= surrogateFirst && cp <= surrogateLast {
			continue
		}
		folded := []rune(foldSearch(string(rune(cp))))
		if len(folded) != 1 || int(folded[0]) == cp {
			continue
		}
		delta := int(folded[0]) - cp
		if n := len(runs); n > 0 && runs[n-1].delta == delta && runs[n-1].start+runs[n-1].length == cp {
			runs[n-1].length++
			continue
		}
		runs = append(runs, foldRun{start: cp, length: 1, delta: delta})
	}
	return runs
}

// foldRunsCoverage totals the code points a run list maps.
func foldRunsCoverage(runs []foldRun) int {
	total := 0
	for _, run := range runs {
		total += run.length
	}
	return total
}

// foldTableNumber matches one signed integer of the table literal.
var foldTableNumber = regexp.MustCompile(`-?\d+`)

// scriptFoldTable extracts `var FOLD_TABLE = [ start, length, delta, ... ];` from
// the served script, the way scriptArrayTable extracts the badge tables from the
// modal script: the table is read out of the asset the binary ships, so what is
// checked is what a browser would run.
func scriptFoldTable(t *testing.T, script string) []foldRun {
	t.Helper()

	block := scriptBlock(t, script, "FOLD_TABLE", "[", "]")
	numbers := foldTableNumber.FindAllString(block, -1)
	if len(numbers) == 0 {
		t.Fatalf("the script's FOLD_TABLE is empty; the client would fold nothing")
	}
	if len(numbers)%3 != 0 {
		t.Fatalf("the script's FOLD_TABLE holds %d numbers, which is not whole triples of "+
			"start, length, delta; the table is truncated or corrupt", len(numbers))
	}
	runs := make([]foldRun, 0, len(numbers)/3)
	for i := 0; i < len(numbers); i += 3 {
		values := [3]int{}
		for j := range values {
			value, err := strconv.Atoi(numbers[i+j])
			if err != nil {
				t.Fatalf("the script's FOLD_TABLE holds the unparseable number %q: %v",
					numbers[i+j], err)
			}
			values[j] = value
		}
		runs = append(runs, foldRun{start: values[0], length: values[1], delta: values[2]})
	}
	return runs
}

// ==================== THE TWO DIVERGENT CODE POINTS ====================

// Titles seeded for the divergence fixture. Two of them are the whole reason the
// folding rule is stated rather than merely required to be locale-independent:
//
//   - greekUpperTitle carries a WORD-FINAL U+03A3. The folding rule folds it to
//     U+03C3 in every position; Unicode's Default Case Conversion, which a
//     platform's own lower-case function may implement, folds it to U+03C2 here.
//   - greekFinalTitle carries a literal U+03C2 the author typed. It is already
//     lower case and folds to itself, which is why nothing may be rewritten after
//     the mapping: a client that rewrote U+03C2 to U+03C3 in a term would stop
//     finding this task.
//   - dottedCapitalTitle carries U+0130. The folding rule folds it to U+0069
//     alone; the full conversion produces the TWO code points U+0069 U+0307.
//   - desereteTitle carries astral code points, which a term must be folded by
//     CODE POINT to match: walking a term by UTF-16 code unit would fold each
//     surrogate half, no run covers a surrogate, and the term would be left
//     unfolded.
const (
	greekUpperTitle    = "ΟΔΟΣ ΠΛΗΡΩΜΩΝ: επανασχεδιασμός δρομολόγησης"
	greekFinalTitle    = "Χαρτογράφηση οδός πληρωμών ανά πάροχο"
	dottedCapitalTitle = "İSTANBUL nightly settlement reconciliation"
	plainLatinTitle    = "Istanbul acquirer report parser"
	deseretTitle       = "𐐀𐐁 Deseret glossary pilot"
	controlTitle       = "Rotate the payment gateway signing keys"
)

// TestTaskSearch_DivergentCodePointsSelectTheSameCardsOnBothPaths is the gate for
// Acceptance Criterion 118 and for the identity Acceptance Criterion 104 requires
// of EVERY term, the two divergent code points included.
//
// The two paths are compared as the two paths, not as one: the server's board is
// the page it renders for ?q=<term>, and the client's board is computed by folding
// the term through the table EXTRACTED FROM THE SERVED SCRIPT and matching it
// against the corpus the server folded into the cards — which is exactly what the
// browser does. Only the fold is under test here; the surrounding whitespace strip
// is not what these terms exercise.
func TestTaskSearch_DivergentCodePointsSelectTheSameCardsOnBothPaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedFoldFixture(t, "multilingual-settlement")
	mux := buildMux()

	shipped := scriptFoldTable(t, readEmbeddedAsset(t, "static/task-search.js"))
	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})

	for _, c := range []struct {
		name string
		term string
		want []int
	}{
		{
			// The defect itself: typing the word in capitals found nothing,
			// while opening the URL that carries it found the card.
			name: "word-final capital sigma folds to U+03C3",
			term: "ΟΔΟΣ",
			want: []int{f.greekUpper},
		},
		{
			name: "the same word already in lower case",
			term: "οδοσ",
			want: []int{f.greekUpper},
		},
		{
			// The post-fold fixup this rule forbids: rewriting U+03C2 to
			// U+03C3 after folding would lose this card.
			name: "a literal final sigma stays a final sigma",
			term: "οδός",
			want: []int{f.greekFinal},
		},
		{
			name: "a phrase of capitals, spaces and all",
			term: "ΟΔΟΣ ΠΛΗΡΩΜΩΝ",
			want: []int{f.greekUpper},
		},
		{
			// U+0130 folds to U+0069 ALONE. The full conversion's
			// U+0069 U+0307 would match neither title.
			name: "dotted capital I folds to a bare i",
			term: "İSTANBUL",
			want: []int{f.dotted, f.plain},
		},
		{
			name: "and the plain spelling finds the same two",
			term: "istanbul",
			want: []int{f.dotted, f.plain},
		},
		{
			// Folded by code point: a UTF-16 code-unit walk would leave this
			// term unfolded and match nothing.
			name: "an astral pair folds as one character",
			term: "𐐀𐐁",
			want: []int{f.deseret},
		},
		{
			name: "an unrelated ASCII term still narrows normally",
			term: "SIGNING",
			want: []int{f.control},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, _ := servedBoard(t, mux, f.name, clientControls{Term: c.term})
			if got := shownBoardIDs(server); !equalIDSets(got, c.want) {
				t.Errorf("the SERVER shows %v for %q, want %v", got, c.term, c.want)
			}
			client := clientShownIDs(unnarrowed, shipped, c.term)
			if !equalIDSets(client, c.want) {
				t.Errorf("the CLIENT shows %v for %q, want %v", client, c.term, c.want)
			}
			if got := shownBoardIDs(server); !equalIDSets(got, client) {
				t.Errorf("the two paths disagree on %q: the server shows %v, the browser %v",
					c.term, got, client)
			}
		})
	}
}

// clientShownIDs is what the browser would show for a term: the term folded
// through the SHIPPED table, then matched as a substring against the corpus the
// server folded into each card, or against the card's "#<id>" reference — the
// script's own matchesTerm, over the script's own table.
func clientShownIDs(board boardState, runs []foldRun, raw string) []int {
	var folded strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		folded.WriteRune(rune(applyFoldTable(runs, int(r))))
	}
	term := folded.String()

	shown := []int{}
	for _, column := range board.columns {
		for _, card := range column.cards {
			if term == "" ||
				strings.Contains(card.search, term) ||
				strings.Contains("#"+strconv.Itoa(card.id), term) {
				shown = append(shown, card.id)
			}
		}
	}
	return shown
}

// shownBoardIDs lists every id a served board is showing, across its columns.
func shownBoardIDs(board boardState) []int {
	shown := []int{}
	for _, column := range board.columns {
		for _, card := range column.cards {
			if card.shown {
				shown = append(shown, card.id)
			}
		}
	}
	return shown
}

// equalIDSets compares two id lists as sets of the same size, so a column ordering
// difference is not mistaken for a matching difference.
func equalIDSets(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[int]int, len(got))
	for _, id := range got {
		seen[id]++
	}
	for _, id := range want {
		seen[id]--
		if seen[id] < 0 {
			return false
		}
	}
	return true
}

// foldFixture names the tasks seedFoldFixture created, so the expectations above
// bind to ids rather than to insertion order.
type foldFixture struct {
	name       string
	greekUpper int
	greekFinal int
	dotted     int
	plain      int
	deseret    int
	control    int
}

// seedFoldFixture builds a roadmap whose titles carry the code points on which a
// platform's own case conversion differs from the folding rule, plus an ordinary
// ASCII control so a term that narrows nothing at all cannot pass unnoticed.
func seedFoldFixture(t *testing.T, name string) foldFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	created := 0
	newTask := func(title string) int {
		t.Helper()
		created++
		id, cerr := database.CreateTask(ctx, &models.Task{
			Title:                  title,
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			Priority:               5,
			Severity:               3,
			FunctionalRequirements: "Operators must find this task by typing its title into the board search.",
			TechnicalRequirements:  "The title is folded once by the server into the card's search corpus.",
			AcceptanceCriteria:     "The term selects the same cards typed as it does carried in the URL.",
			CreatedAt:              fmt.Sprintf("2026-03-%02dT09:00:00Z", created),
		})
		if cerr != nil {
			t.Fatalf("creating task %q: %v", title, cerr)
		}
		return id
	}

	return foldFixture{
		name:       name,
		greekUpper: newTask(greekUpperTitle),
		greekFinal: newTask(greekFinalTitle),
		dotted:     newTask(dottedCapitalTitle),
		plain:      newTask(plainLatinTitle),
		deseret:    newTask(deseretTitle),
		control:    newTask(controlTitle),
	}
}
