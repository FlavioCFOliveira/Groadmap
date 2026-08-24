package web

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ==================== THE TWO SPELLINGS OF ONE TITLE ====================

// Titles seeded for the normalisation fixture. Each PAIR renders identically to
// every reader and carries two different byte sequences, which is the whole of
// the defect this rule closes: the search compared bytes, so a term typed in one
// spelling found one of the pair and silently missed the other.
//
// The decomposed spellings are written as explicit ESCAPES rather than as literal
// accented characters, because an editor is free to normalise a file it saves and
// would then quietly turn each pair into two copies of one spelling — which would
// leave every assertion below passing while proving nothing. The fixture asserts
// that it has not happened before it asserts anything else.
const (
	precomposedTitle = "Caf\u00e9 Lisboa merchant onboarding"     // e-acute as U+00E9
	decomposedTitle  = "Cafe\u0301 Porto merchant onboarding"     // e-acute as U+0065 U+0301
	dottedTitle      = "\u0130stanbul acquirer settlement"        // dotted I as U+0130
	dottedSequence   = "I\u0307stanbul acquirer reconciliation"   // dotted I as U+0049 U+0307
	unaccentedTitle  = "Cafeteria supplier payment schedule"      // holds "cafe", accent-free
	aereaTitle       = "A\u00e9rea cargo terminal handover"       // A followed by e-acute
	asciiControl     = "Rotate the acquirer signing certificates" // nothing above ASCII
)

// The combining marks the decomposed spellings are built from, named so that the
// fixture's own self-check reads as the question it asks.
const (
	combiningAcute    = "\u0301"
	combiningDotAbove = "\u0307"
)

// The two canonical spellings of the terms a user types, which must find one board.
const (
	precomposedTerm = "caf\u00e9"
	decomposedTerm  = "cafe\u0301"
	upperPreTerm    = "CAF\u00c9"
	upperDecomTerm  = "CAFE\u0301"
	dottedTerm      = "\u0130stanbul"
	dottedSeqTerm   = "I\u0307stanbul"
)

// normalisationFixture names the tasks seedNormalisationFixture created, so the
// expectations below bind to ids rather than to insertion order.
type normalisationFixture struct {
	name        string
	precomposed int
	decomposed  int
	dotted      int
	dottedSeq   int
	unaccented  int
	aerea       int
	control     int
}

// all lists every id the fixture seeded, which is what a term that is no term at
// all must show.
func (f normalisationFixture) all() []int {
	return []int{f.precomposed, f.decomposed, f.dotted, f.dottedSeq, f.unaccented, f.aerea, f.control}
}

// seedNormalisationFixture builds a roadmap holding two visibly identical titles
// in the two canonical spellings, the same again for U+0130, and the controls that
// keep a rule which found nothing at all from passing unnoticed.
func seedNormalisationFixture(t *testing.T, name string) normalisationFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	created := 0
	newTask := func(title string) int {
		t.Helper()
		created++
		id, cerr := seedTask(database, &models.Task{
			Title:                  title,
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			Priority:               5,
			Severity:               3,
			FunctionalRequirements: "Operators must find this task by typing its title into the board search.",
			TechnicalRequirements:  "The title is normalised and folded once by the server into the card corpus.",
			AcceptanceCriteria:     "Either canonical spelling of the term selects the task.",
			CreatedAt:              fmt.Sprintf("2026-04-%02dT09:00:00Z", created),
		})
		if cerr != nil {
			t.Fatalf("creating task %q: %v", title, cerr)
		}
		return id
	}

	return normalisationFixture{
		name:        name,
		precomposed: newTask(precomposedTitle),
		decomposed:  newTask(decomposedTitle),
		dotted:      newTask(dottedTitle),
		dottedSeq:   newTask(dottedSequence),
		unaccented:  newTask(unaccentedTitle),
		aerea:       newTask(aereaTitle),
		control:     newTask(asciiControl),
	}
}

// assertFixtureStillHoldsTwoSpellings refuses to run on a fixture an editor has
// normalised: each pair must carry its combining mark in exactly one of its two
// titles, and the two titles must differ in the word the terms are typed as.
func assertFixtureStillHoldsTwoSpellings(t *testing.T) {
	t.Helper()

	for _, pair := range []struct{ name, precomposed, decomposed, mark string }{
		{"e-acute", precomposedTitle, decomposedTitle, combiningAcute},
		{"dotted I", dottedTitle, dottedSequence, combiningDotAbove},
	} {
		if strings.Contains(pair.precomposed, pair.mark) {
			t.Fatalf("the %s pair's precomposed title carries the combining mark: the fixture "+
				"has been normalised in the source and proves nothing", pair.name)
		}
		if !strings.Contains(pair.decomposed, pair.mark) {
			t.Fatalf("the %s pair's decomposed title carries no combining mark: the fixture "+
				"has been normalised in the source and proves nothing", pair.name)
		}
		if first, second := firstWord(pair.precomposed), firstWord(pair.decomposed); first == second {
			t.Fatalf("the %s pair's two titles begin with the same bytes, %q: the fixture holds "+
				"one spelling twice", pair.name, first)
		}
	}
}

// firstWord is the word a fixture pair differs in, which is the word the terms
// below are typed as.
func firstWord(title string) string {
	return strings.SplitN(title, " ", 2)[0]
}

// TestTaskSearch_EitherStoredSpellingIsFoundByEitherTypedSpelling is the gate for
// Acceptance Criterion 153, and it is the defect this task exists to close.
//
// Two visibly identical titles can sit on one board, one stored precomposed and
// one stored decomposed, and before this rule a term typed in one spelling found
// one of them and missed the other with nothing on the page revealing the
// omission. Both paths missed it TOGETHER, so it was never the disagreement
// Acceptance Criterion 104 governs: it was a second and independent way the search
// failed to find a task that visibly contains the term.
//
// All FOUR combinations are asserted rather than a sample — decomposed title with
// decomposed term, decomposed title with precomposed term, precomposed title with
// decomposed term, precomposed title with precomposed term — and each of them on
// BOTH paths: the server's board for ?q=<term>, and the board the browser would
// compute from the term prepared through the TABLES EXTRACTED FROM THE SERVED
// SCRIPT. Acceptance Criterion 104's identity therefore survives normalisation
// rather than being weakened by it.
func TestTaskSearch_EitherStoredSpellingIsFoundByEitherTypedSpelling(t *testing.T) {
	assertFixtureStillHoldsTwoSpellings(t)

	t.Setenv("HOME", t.TempDir())
	f := seedNormalisationFixture(t, "canonical-settlement")
	mux := buildMux()

	rule := readShippedRule(t)
	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})

	for _, c := range []struct {
		name string
		term string
		want []int
	}{
		{
			name: "a precomposed term finds the precomposed title and the decomposed one alike",
			term: precomposedTerm,
			want: []int{f.precomposed, f.decomposed},
		},
		{
			name: "and so does the decomposed term",
			term: decomposedTerm,
			want: []int{f.precomposed, f.decomposed},
		},
		{
			name: "the same term in capitals, precomposed",
			term: upperPreTerm,
			want: []int{f.precomposed, f.decomposed},
		},
		{
			name: "and in capitals, decomposed",
			term: upperDecomTerm,
			want: []int{f.precomposed, f.decomposed},
		},
		{
			// U+0130's canonical decomposition is U+0049 U+0307, so the two
			// titles are the same text by Unicode's own definition and now carry
			// ONE searchable text. Acceptance Criterion 104's verdict on U+0130
			// is EXTENDED by this and not amended: "istanbul" goes on finding
			// what it always found, and U+0130 goes on folding to U+0069 alone.
			name: "a term spelled with U+0130 finds both spellings of the title",
			term: dottedTerm,
			want: []int{f.dotted, f.dottedSeq},
		},
		{
			name: "and a term spelled U+0049 U+0307 finds the same two",
			term: dottedSeqTerm,
			want: []int{f.dotted, f.dottedSeq},
		},
		{
			name: "and the plain spelling still finds them, as it always did",
			term: "istanbul",
			want: []int{f.dotted, f.dottedSeq},
		},
		{
			// The control: an ASCII term narrows exactly as it did before this
			// rule existed, so a rule that showed every card cannot pass here.
			name: "an unrelated ASCII term still narrows normally",
			term: "certificates",
			want: []int{f.control},
		},
		{
			// And a term that is no term at all still shows everything, so the
			// fixture's own size is asserted rather than assumed.
			name: "an empty term shows every task",
			term: "",
			want: f.all(),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, _ := servedBoard(t, mux, f.name, clientControls{Term: c.term})
			if got := shownBoardIDs(server); !equalIDSets(got, c.want) {
				t.Errorf("the SERVER shows %v for %v, want %v", got, []rune(c.term), c.want)
			}
			client := clientShownIDs(unnarrowed, rule, c.term)
			if !equalIDSets(client, c.want) {
				t.Errorf("the CLIENT shows %v for %v, want %v", client, []rune(c.term), c.want)
			}
			if got := shownBoardIDs(server); !equalIDSets(got, client) {
				t.Errorf("the two paths disagree on %v: the server shows %v, the browser %v",
					[]rune(c.term), got, client)
			}
		})
	}

	// And each pair carries ONE searchable text, which is what makes every case
	// above hold by construction rather than by these terms happening to work.
	for _, pair := range []struct{ name, precomposed, decomposed string }{
		{"e-acute", precomposedTitle, decomposedTitle},
		{"dotted I", dottedTitle, dottedSequence},
	} {
		first := firstWord(searchableText(pair.precomposed))
		second := firstWord(searchableText(pair.decomposed))
		if first != second {
			t.Errorf("the %s pair carries two searchable words, %v and %v; canonically "+
				"equivalent titles must carry one", pair.name, []rune(first), []rune(second))
		}
	}
}

// ==================== FOR COMPARISON ONLY ====================

// TestSearchNormalisation_TouchesNoStoredOrRenderedByte is the gate for the half
// of Acceptance Criterion 152 that says what this rule does NOT do.
//
// Normalisation is for comparison only. The bytes rmp stores stay exactly the
// bytes it was given, and the card renders the title the roadmap actually holds,
// so a decomposed title read back out of the database is byte for byte what was
// written and appears byte for byte in the page. Only the DERIVED searchable text
// is normalised, and it is asserted to be a DIFFERENT string from the folded
// title, because two assertions that a title was left alone would otherwise be
// satisfied by a rule that normalised nothing anywhere.
func TestSearchNormalisation_TouchesNoStoredOrRenderedByte(t *testing.T) {
	assertFixtureStillHoldsTwoSpellings(t)

	t.Setenv("HOME", t.TempDir())
	f := seedNormalisationFixture(t, "stored-verbatim")

	database, err := db.Open(f.name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", f.name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	stored, err := database.GetTask(context.Background(), f.decomposed)
	if err != nil {
		t.Fatalf("reading task %d: %v", f.decomposed, err)
	}
	if stored.Title != decomposedTitle {
		t.Errorf("the roadmap stores %v, want the bytes it was given, %v",
			[]rune(stored.Title), []rune(decomposedTitle))
	}

	mux := buildMux()
	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	for _, title := range []string{decomposedTitle, precomposedTitle, dottedTitle, dottedSequence} {
		if !strings.Contains(body, title) {
			t.Errorf("the page does not render the stored title %v; normalisation has reached "+
				"the display", []rune(title))
		}
	}

	// The derived form IS normalised, which is what makes the assertions above a
	// statement about WHERE the rule applies rather than that it applies nowhere.
	view := &taskView{Task: *stored}
	if view.SearchText() == foldSearch(decomposedTitle) {
		t.Errorf("the searchable text is the untouched title folded; the corpus is not being " +
			"normalised at all")
	}
	if firstWord(view.SearchText()) != firstWord(searchableText(precomposedTitle)) {
		t.Errorf("the searchable text of the decomposed title is %v, want the precomposed "+
			"title's %v", []rune(firstWord(view.SearchText())),
			[]rune(firstWord(searchableText(precomposedTitle))))
	}
}

// TestSearchNormalisation_IsTwoPassesInThisOrder is the gate for the half of
// Acceptance Criterion 152 that fixes the pipeline: trim, then NFC, then fold,
// then NFC.
//
// Both halves of that shape are PROVEN over a real domain rather than asserted:
// the sequences of a folding code point followed by a non-starter, which is every
// sequence on which either question can arise, and which is swept in full.
//
//   - THE SECOND PASS IS REQUIRED. The fold can produce a sequence that composes
//     where the unfolded one did not, so one pass leaves the result outside NFC on
//     some of those sequences while two passes leave it inside on all of them. A
//     third would change nothing, which is why there is no third — and that is
//     asserted, not assumed, on every sequence swept.
//   - NORMALISING FIRST IS REQUIRED. The two orders differ on none of Unicode's
//     single code points and on some of those sequences, and folding first gives a
//     title spelled U+0130 and a title spelled U+0049 U+0307 two different
//     searchable texts, leaving the defect open exactly where this task closes it.
//
// The counts are compared against the size of the swept domain rather than against
// stored numbers, so this cannot pass by sweeping nothing.
func TestSearchNormalisation_IsTwoPassesInThisOrder(t *testing.T) {
	var folding, nonStarters []rune
	for cp := rune(0); cp <= unicodeMaxCodePoint; cp++ {
		if isSurrogateRune(cp) {
			continue
		}
		if foldSearch(string(cp)) != string(cp) {
			folding = append(folding, cp)
		}
		if searchCombiningClass(cp) != 0 {
			nonStarters = append(nonStarters, cp)
		}
	}
	if len(folding) == 0 || len(nonStarters) == 0 {
		t.Fatalf("the sweep found %d folding code points and %d non-starters; it would prove "+
			"nothing", len(folding), len(nonStarters))
	}

	swept, onePassOutside, orderDiffers := 0, 0, 0
	orderLeads := make(map[rune]bool)
	var firstOnePass, firstOrder string
	for _, lead := range folding {
		for _, mark := range nonStarters {
			swept++
			sequence := string(lead) + string(mark)

			// ONE pass: normalise, then fold, and stop.
			onePass := foldSearch(searchNFC(sequence))
			if searchNFC(onePass) != onePass {
				onePassOutside++
				if firstOnePass == "" {
					firstOnePass = fmt.Sprintf("U+%04X U+%04X folds to %v, which composes to %v",
						lead, mark, []rune(onePass), []rune(searchNFC(onePass)))
				}
			}
			// TWO passes, the rule as it stands: always in NFC, so no third.
			twoPasses := searchableText(sequence)
			if searchNFC(twoPasses) != twoPasses {
				t.Fatalf("U+%04X U+%04X prepares to %v, which is not in NFC: a third pass "+
					"would be needed and the rule says there is none",
					lead, mark, []rune(twoPasses))
			}
			// THE OTHER ORDER, one normalisation each way.
			if foldSearch(searchNFC(sequence)) != searchNFC(foldSearch(sequence)) {
				orderDiffers++
				orderLeads[lead] = true
				if firstOrder == "" {
					firstOrder = fmt.Sprintf("U+%04X U+%04X gives %v normalising first and %v "+
						"folding first", lead, mark, []rune(foldSearch(searchNFC(sequence))),
						[]rune(searchNFC(foldSearch(sequence))))
				}
			}
		}
	}
	if swept != len(folding)*len(nonStarters) {
		t.Fatalf("the sweep covered %d sequences, want %d", swept, len(folding)*len(nonStarters))
	}
	if onePassOutside == 0 {
		t.Errorf("one pass left every one of the %d sequences in NFC, so this sweep no longer "+
			"shows why the second pass exists: the domain or the fold has changed", swept)
	} else {
		t.Logf("one pass leaves %d of %d sequences outside NFC (%s); two passes leave all %d "+
			"inside", onePassOutside, swept, firstOnePass, swept)
	}
	if orderDiffers == 0 {
		t.Errorf("the two orders agreed on all %d sequences, so this sweep no longer shows why "+
			"the order is fixed", swept)
	} else {
		t.Logf("the two orders differ on %d of %d sequences, over %d leading code points (%s)",
			orderDiffers, swept, len(orderLeads), firstOrder)
	}

	// The order is NOT observable on a single code point, which is why the sweep
	// above has to be over sequences at all.
	for cp := rune(0); cp <= unicodeMaxCodePoint; cp++ {
		if isSurrogateRune(cp) {
			continue
		}
		single := string(cp)
		if foldSearch(searchNFC(single)) != searchNFC(foldSearch(single)) {
			t.Errorf("U+%04X: the two orders differ on a single code point, which the rule's "+
				"reasoning says they do not", cp)
		}
	}

	// And the case the order exists for: folding first leaves the two canonically
	// equivalent spellings of the dotted I with two different searchable texts.
	if got, want := firstWord(searchableText(dottedTitle)),
		firstWord(searchableText(dottedSequence)); got != want {
		t.Errorf("the two spellings of the dotted I prepare to %v and %v; normalising first "+
			"must give them one text", []rune(got), []rune(want))
	}
	if firstWord(foldSearch(dottedTitle)) == firstWord(foldSearch(dottedSequence)) {
		t.Errorf("folding without normalising already gave the two spellings one text, so this " +
			"fixture no longer shows what the order is for")
	}

	// The whole pipeline, spelled out on the witnesses the rule names.
	for _, c := range []struct{ name, raw, want string }{
		{"the second pass composes what the fold made composable", "H\u0331", "\u1e96"},
		{"and the precomposed spelling of it prepares to the same", "\u1e96", "\u1e96"},
		{"U+0130 prepares to a bare i", "\u0130", "i"},
		{"and so does U+0049 U+0307", "I\u0307", "i"},
		{"the trim still comes first", "  CafE\u0301", "caf\u00e9"},
		{"and whitespace inside the term still survives it", "caf\u00e9 lisboa", "caf\u00e9 lisboa"},
	} {
		if got := foldSearchTerm(c.raw); got != c.want {
			t.Errorf("%s: foldSearchTerm(%v) = %v, want %v", c.name, []rune(c.raw), []rune(got),
				[]rune(c.want))
		}
	}
}

// ==================== AND NOTHING ELSE ====================

// TestSearchNormalisation_ChangesNothingElse is the gate for Acceptance
// Criterion 154: the form is NFC, and NFC changes nothing beyond making two
// spellings of one text one text.
//
// THE FORM MATTERS BECAUSE THE COMPARISON IS SUBSTRING CONTAINMENT. Under NFD a
// title of "Café Lisboa" would carry the four ASCII letters of "cafe" followed by
// a combining acute, so the term "cafe" would return it and the term "ae" would
// return a title of "Aérea". Both are false positives and both are the mirror
// image of the defect this rule exists to close. Under NFC an accented letter
// stays one code point and neither term matches — asserted here on BOTH paths,
// against a board that DOES hold a task the term "cafe" legitimately finds, so a
// rule that simply found nothing cannot pass.
//
// AND ASCII IS UNTOUCHED. Swept over the whole of Unicode, the code points whose
// searchable text this rule moves are the canonical singletons and the composition
// exclusions, and not one of them is ASCII — which is what keeps every ASCII term
// and every ASCII title selecting exactly what it selected before.
func TestSearchNormalisation_ChangesNothingElse(t *testing.T) {
	assertFixtureStillHoldsTwoSpellings(t)

	t.Setenv("HOME", t.TempDir())
	f := seedNormalisationFixture(t, "no-false-positives")
	mux := buildMux()

	rule := readShippedRule(t)
	unnarrowed, _ := servedBoard(t, mux, f.name, clientControls{})

	for _, c := range []struct {
		name string
		term string
		want []int
	}{
		{
			// NOT the accented titles: under NFD both would match.
			name: "an accent-free term does not match an accented word",
			term: "cafe",
			want: []int{f.unaccented},
		},
		{
			name: "and a two-letter term does not match across an accent",
			term: "ae",
			want: []int{},
		},
		{
			// The same term with its accent finds exactly the accented pair,
			// which is what makes the two cases above a statement about NFC
			// rather than about a term that matches nothing at all.
			name: "the accented term finds the accented titles",
			term: precomposedTerm,
			want: []int{f.precomposed, f.decomposed},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, _ := servedBoard(t, mux, f.name, clientControls{Term: c.term})
			if got := shownBoardIDs(server); !equalIDSets(got, c.want) {
				t.Errorf("the SERVER shows %v for %q, want %v", got, c.term, c.want)
			}
			client := clientShownIDs(unnarrowed, rule, c.term)
			if !equalIDSets(client, c.want) {
				t.Errorf("the CLIENT shows %v for %q, want %v", client, c.term, c.want)
			}
		})
	}

	// The delta over the whole of Unicode, measured rather than quoted: which code
	// points prepare differently under this rule than under the fold alone.
	swept, changed, asciiChanged := 0, 0, 0
	for cp := rune(0); cp <= unicodeMaxCodePoint; cp++ {
		if isSurrogateRune(cp) {
			continue
		}
		swept++
		single := string(cp)
		if searchableText(single) == foldSearch(single) {
			continue
		}
		changed++
		if cp < utf8.RuneSelf {
			asciiChanged++
			if asciiChanged <= maxReportedMismatches {
				t.Errorf("U+%04X is ASCII and its searchable text moved, from %v to %v; no "+
					"ASCII code point may move under this rule", cp, []rune(foldSearch(single)),
					[]rune(searchableText(single)))
			}
		}
	}
	if swept != unicodeScalarValues {
		t.Errorf("the sweep covered %d code points, want the whole of Unicode: %d",
			swept, unicodeScalarValues)
	}
	if changed == 0 {
		t.Errorf("no code point of Unicode prepares differently under this rule than under the " +
			"fold alone, so the corpus is not being normalised at all")
	}
	t.Logf("%d of %d code points prepare differently under this rule than under the fold alone, "+
		"%d of them ASCII", changed, swept, asciiChanged)
}
