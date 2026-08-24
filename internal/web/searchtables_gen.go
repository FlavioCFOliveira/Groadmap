//go:build ignore

// Command searchtables_gen writes everything a term's preparation is made of into
// the script the binary serves, all of it in static/task-search.js: the
// run-encoded FOLD_TABLE carrying the folding rule's mapping, the run-encoded
// SPACE_TABLE carrying the trim rule's whitespace set, and the three tables the
// normalisation rule is made of — DECOMP_TABLE, the full canonical
// decompositions; CCC_TABLE, the canonical combining classes; and COMPOSE_TABLE,
// the primary composites.
//
// The client MUST NOT prepare a term with the JavaScript platform's own
// functions. Its case conversion is Unicode's Default Case Conversion rather than
// the simple mapping the folding rule fixes; its trimming removes a different set
// from the White_Space property the trim rule fixes — it keeps U+0085, which
// carries the property, and removes U+FEFF, which does not; and its
// String.prototype.normalize reads normalisation tables of its own. All three read
// tables of whatever Unicode version the browser ships. Shipping the server's own
// mapping, the server's own set and the server's own normalisation data removes
// the platform and the browser's Unicode version from the answer on all three
// counts (SPEC/WEB.md § Roadmap Tasks Page, One rule, and only one implementation
// of it; The trim rule; The normalisation rule).
//
// Hangul is deliberately NOT tabulated: UAX #15 decomposes and composes the 11,172
// Hangul syllables arithmetically, so both sides compute them and DECOMP_TABLE
// holds 2,061 entries rather than 13,233 and COMPOSE_TABLE 941 rather than 12,113.
//
// Run it with `go generate ./internal/web/` and commit the result: the tables are
// generated but COMMITTED artefacts, so `go build` stays a plain Go build with no
// code-generation step. Regeneration is not what keeps the two sides equal —
// TestTaskSearchScript_ShippedRuleIsTheServerRule is. That test compares BOTH
// SHIPPED tables against the server's own foldSearch and isSearchSpace over every
// code point of Unicode on every `go test ./...`, so a toolchain upgrade that
// moves a mapping or changes which code points carry White_Space fails the build
// gates and names what moved; re-running this generator is then the fix, not the
// detection.
package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// scriptPath is the served asset the tables are written into, relative to
// internal/web/ — the directory `go generate` runs this from.
const scriptPath = "static/task-search.js"

// maxCodePoint is the last code point of Unicode. Surrogates (U+D800-U+DFFF) are
// not scalar values, carry no case mapping and no property, so no span may cover
// them.
const maxCodePoint = 0x10FFFF

// The Hangul syllables and jamo of UAX #15's algorithmic decomposition and
// composition, which no table below holds a single entry for.
const (
	hangulSBase  = 0xAC00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11A7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulSCount = hangulLCount * hangulNCount
)

// maxDecomposition is the longest full canonical decomposition in Unicode, which
// DECOMP_TABLE's fixed entry width is built from: U+1F82 decomposes to the four
// code points U+03B1 U+0313 U+0300 U+0345. A fixed width is what lets the client
// binary-search the table by index, exactly as it searches the other four; the
// unused slots of a shorter decomposition carry 0, which no decomposition
// element can be.
const maxDecomposition = 4

// lineWidth caps the emitted lines' length so the asset stays reviewable in an
// ordinary diff.
const lineWidth = 92

// span is a maximal range of consecutive code points that share something: for
// SPACE_TABLE, the White_Space property.
type span struct {
	start  int
	length int
}

// run is a span whose code points all fold by the same delta: code point c in
// [start, start+length) folds to c+delta.
type run struct {
	span
	delta int
}

// region is one generated block of the script: the two hand-written markers that
// delimit it, and the literal written between them. The markers are never emitted
// by this program — everything between them is replaced, everything outside is
// left exactly as it was.
type region struct {
	begin string
	end   string
	body  string
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("searchtables_gen: ")

	folds := foldRuns()
	if len(folds) == 0 {
		log.Fatal("the folding rule maps nothing; FOLD_TABLE would be empty")
	}
	spaces := spaceSpans()
	if len(spaces) == 0 {
		log.Fatal("the trim rule matches nothing; SPACE_TABLE would be empty")
	}

	decompositions := decompositionEntries()
	if len(decompositions) == 0 {
		log.Fatal("nothing decomposes; DECOMP_TABLE would be empty")
	}
	classes := combiningClassSpans()
	if len(classes) == 0 {
		log.Fatal("no code point carries a combining class; CCC_TABLE would be empty")
	}
	compositions := compositionEntries(decompositions)
	if len(compositions) == 0 {
		log.Fatal("nothing composes; COMPOSE_TABLE would be empty")
	}

	regions := []region{
		{
			begin: "  /* BEGIN GENERATED FOLD TABLE */",
			end:   "  /* END GENERATED FOLD TABLE */",
			body:  emit("FOLD_TABLE", foldNumbers(folds), 3),
		},
		{
			begin: "  /* BEGIN GENERATED SPACE TABLE */",
			end:   "  /* END GENERATED SPACE TABLE */",
			body:  emit("SPACE_TABLE", spaceNumbers(spaces), 2),
		},
		{
			begin: "  /* BEGIN GENERATED DECOMP TABLE */",
			end:   "  /* END GENERATED DECOMP TABLE */",
			body:  emit("DECOMP_TABLE", decompositionNumbers(decompositions), 1+maxDecomposition),
		},
		{
			begin: "  /* BEGIN GENERATED CCC TABLE */",
			end:   "  /* END GENERATED CCC TABLE */",
			body:  emit("CCC_TABLE", combiningClassNumbers(classes), 3),
		},
		{
			begin: "  /* BEGIN GENERATED COMPOSE TABLE */",
			end:   "  /* END GENERATED COMPOSE TABLE */",
			body:  emit("COMPOSE_TABLE", compositionNumbers(compositions), 3),
		},
	}

	script, err := os.ReadFile(scriptPath)
	if err != nil {
		log.Fatalf("reading %s: %v", scriptPath, err)
	}
	updated := string(script)
	for _, r := range regions {
		updated, err = replaceRegion(updated, r)
		if err != nil {
			log.Fatalf("rewriting %s: %v", scriptPath, err)
		}
	}
	summary := fmt.Sprintf("%d fold runs covering %d code points, %d whitespace spans covering "+
		"%d, %d decompositions, %d combining-class spans, %d primary composites",
		len(folds), coveredRuns(folds), len(spaces), coveredSpans(spaces),
		len(decompositions), len(classes), len(compositions))
	if updated == string(script) {
		fmt.Printf("%s is already up to date (%s)\n", scriptPath, summary)
		return
	}
	if err := os.WriteFile(scriptPath, []byte(updated), 0o600); err != nil {
		log.Fatalf("writing %s: %v", scriptPath, err)
	}
	fmt.Printf("%s: wrote %s\n", scriptPath, summary)
}

// foldRuns run-encodes the folding rule over every code point of Unicode.
//
// The rule is applied here exactly as internal/web/fold.go's foldSearch applies
// it — strings.ToLower of the single code point — so the generator and the server
// express one rule. That the two expressions AGREE is not assumed: the guard test
// proves it against foldSearch itself, over every code point, on every test run.
func foldRuns() []run {
	var runs []run
	for cp := 0; cp <= maxCodePoint; cp++ {
		if isSurrogate(cp) {
			continue // not a scalar value; it has no mapping and gets no run
		}
		folded := strings.ToLower(string(rune(cp)))
		if utf8.RuneCountInString(folded) != 1 {
			// The simple mapping is one code point in, one code point out. A
			// rule that ever produced two could not be run-encoded as a delta,
			// so it is a hard error rather than a silently dropped entry.
			log.Fatalf("U+%04X folds to %d code points; the rule must fold to exactly one",
				cp, utf8.RuneCountInString(folded))
		}
		lower := int([]rune(folded)[0])
		if lower == cp {
			continue // folds to itself: no run needed, the client's default
		}
		delta := lower - cp
		if n := len(runs); n > 0 && runs[n-1].delta == delta && runs[n-1].start+runs[n-1].length == cp {
			runs[n-1].length++
			continue
		}
		runs = append(runs, run{span: span{start: cp, length: 1}, delta: delta})
	}
	return runs
}

// spaceSpans run-encodes the trim rule's whitespace set over every code point of
// Unicode.
//
// The set is applied here exactly as internal/web/fold.go's isSearchSpace applies
// it — unicode.IsSpace, which is Unicode's White_Space property — for the same
// reason foldRuns re-expresses the fold, and with the same guarantee: the guard
// test proves the emitted spans equal isSearchSpace itself, code point by code
// point, on every test run.
func spaceSpans() []span {
	var spans []span
	for cp := 0; cp <= maxCodePoint; cp++ {
		if isSurrogate(cp) || !unicode.IsSpace(rune(cp)) {
			continue
		}
		if n := len(spans); n > 0 && spans[n-1].start+spans[n-1].length == cp {
			spans[n-1].length++
			continue
		}
		spans = append(spans, span{start: cp, length: 1})
	}
	return spans
}

// isSurrogate reports whether cp is one of the 2048 surrogate code points, which
// are not scalar values.
func isSurrogate(cp int) bool {
	return cp >= 0xD800 && cp <= 0xDFFF
}

// foldNumbers flattens the fold runs into the start, length, delta triples the
// script binary searches.
func foldNumbers(runs []run) []int {
	numbers := make([]int, 0, 3*len(runs))
	for _, r := range runs {
		numbers = append(numbers, r.start, r.length, r.delta)
	}
	return numbers
}

// spaceNumbers flattens the whitespace spans into the start, length pairs the
// script binary searches. There is no delta: membership is the whole question a
// trim asks, so the pair is the entire entry.
func spaceNumbers(spans []span) []int {
	numbers := make([]int, 0, 2*len(spans))
	for _, s := range spans {
		numbers = append(numbers, s.start, s.length)
	}
	return numbers
}

// coveredRuns totals the code points the fold runs map.
func coveredRuns(runs []run) int {
	total := 0
	for _, r := range runs {
		total += r.length
	}
	return total
}

// coveredSpans totals the code points the whitespace spans hold.
func coveredSpans(spans []span) int {
	total := 0
	for _, s := range spans {
		total += s.length
	}
	return total
}

// emit renders a flat number list as the JavaScript array literal the script
// carries: one entry per line group of arity numbers, entries separated by a
// space and the lines wrapped at lineWidth. It knows nothing about what the
// numbers mean, which is why both tables are laid out identically — the arity is
// the only difference between a fold run and a whitespace span on the page.
func emit(name string, numbers []int, arity int) string {
	if arity < 1 || len(numbers)%arity != 0 {
		log.Fatalf("%s holds %d numbers, which is not whole entries of %d", name, len(numbers), arity)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "  var %s = [\n", name)

	line := "   "
	for i := 0; i < len(numbers); i += arity {
		entry := make([]string, arity)
		for j := range entry {
			entry[j] = fmt.Sprint(numbers[i+j])
		}
		text := " " + strings.Join(entry, ",")
		if i+arity < len(numbers) {
			text += ","
		}
		if len(line)+len(text) > lineWidth {
			out.WriteString(line + "\n")
			line = "   "
		}
		line += text
	}
	out.WriteString(line + "\n")
	out.WriteString("  ];\n")
	return out.String()
}

// replaceRegion swaps the text between a region's two markers for its generated
// body, leaving the markers and everything around them untouched.
func replaceRegion(script string, r region) (string, error) {
	begin := strings.Index(script, r.begin)
	if begin < 0 {
		return "", fmt.Errorf("no %q marker", strings.TrimSpace(r.begin))
	}
	end := strings.Index(script, r.end)
	if end < 0 {
		return "", fmt.Errorf("no %q marker", strings.TrimSpace(r.end))
	}
	if end < begin {
		return "", fmt.Errorf("the %q marker precedes %q", strings.TrimSpace(r.end),
			strings.TrimSpace(r.begin))
	}
	return script[:begin] + r.begin + "\n" + r.body + script[end:], nil
}

// decomposition is one entry of DECOMP_TABLE: a code point and its FULL canonical
// decomposition, canonically ordered, at most maxDecomposition code points long.
type decomposition struct {
	codePoint int
	runes     []rune
}

// composition is one entry of COMPOSE_TABLE: the primary composite two code
// points make.
type composition struct {
	lead      int
	trail     int
	composite int
}

// classSpan is one entry of CCC_TABLE: a maximal range of consecutive code points
// that all carry the same NON-ZERO canonical combining class. A code point in no
// span carries class 0, which is the default and needs no entry.
type classSpan struct {
	span
	class int
}

// decompositionEntries tabulates the FULL canonical decomposition of every code
// point that has one, over the whole of Unicode.
//
// The data is applied here exactly as internal/web/fold.go's searchDecompose
// applies it — norm.NFD of the single code point — so the generator and the
// server express one rule, and the guard test proves the two agree against
// searchDecompose itself, over every code point, on every test run.
//
// The 11,172 Hangul syllables are skipped: UAX #15 decomposes them
// arithmetically, both sides compute them, and the guard sweeps them too — so an
// arithmetic that disagreed with the server would fail there rather than pass
// unnoticed for want of an entry.
func decompositionEntries() []decomposition {
	var entries []decomposition
	for cp := 0; cp <= maxCodePoint; cp++ {
		if isSurrogate(cp) || isHangulSyllable(cp) {
			continue
		}
		runes := []rune(norm.NFD.String(string(rune(cp))))
		if len(runes) == 1 && runes[0] == rune(cp) {
			continue // decomposes to itself: the client's default, and no entry
		}
		if len(runes) > maxDecomposition {
			log.Fatalf("U+%04X decomposes to %d code points; DECOMP_TABLE holds at most %d",
				cp, len(runes), maxDecomposition)
		}
		entries = append(entries, decomposition{codePoint: cp, runes: runes})
	}
	return entries
}

// combiningClassSpans run-encodes the canonical combining classes over the whole
// of Unicode, one span per maximal range sharing a class.
//
// The data is applied here exactly as internal/web/fold.go's
// searchCombiningClass applies it, for the same reason foldRuns re-expresses the
// fold and with the same guarantee.
func combiningClassSpans() []classSpan {
	var spans []classSpan
	for cp := 0; cp <= maxCodePoint; cp++ {
		if isSurrogate(cp) {
			continue
		}
		class := int(combiningClass(rune(cp)))
		if class == 0 {
			continue // the default, and no entry
		}
		if n := len(spans); n > 0 && spans[n-1].class == class &&
			spans[n-1].start+spans[n-1].length == cp {
			spans[n-1].length++
			continue
		}
		spans = append(spans, classSpan{span: span{start: cp, length: 1}, class: class})
	}
	return spans
}

// compositionEntries derives the primary composites from the decompositions,
// which is what a primary composite IS: a code point whose canonical
// decomposition is two characters, the first of them a starter, that Unicode does
// not exclude from composition.
//
// It re-expresses internal/web/fold.go's buildSearchComposition step for step,
// down to reading the exclusion from the NFC_Quick_Check property — which is
// DATA and not the composing transform — and to entering only COMPOSABLE code
// points into the prefix lookup, so that U+01FA pairs with U+00C5 rather than
// with the canonically equivalent but excluded U+212B. The reason the module's
// own composition is not used at all is in that function's comment: at the pinned
// version it composes a supplementary starter as though the starter were its low
// 16 bits.
//
// Hangul is skipped here as it is in decompositionEntries, and for the same
// reason.
func compositionEntries(decompositions []decomposition) []composition {
	composable := make([]decomposition, 0, len(decompositions))
	byDecomposition := make(map[string]int, len(decompositions))
	for _, d := range decompositions {
		if len(d.runes) < 2 || combiningClass(d.runes[0]) != 0 || isCompositionExcluded(rune(d.codePoint)) {
			continue
		}
		composable = append(composable, d)
		byDecomposition[string(d.runes)] = d.codePoint
	}

	entries := make([]composition, 0, len(composable))
	for _, d := range composable {
		lead := int(d.runes[0])
		if len(d.runes) > 2 {
			prefix, ok := byDecomposition[string(d.runes[:len(d.runes)-1])]
			if !ok {
				continue // no composable prefix: no pair can reach this code point
			}
			lead = prefix
		}
		entries = append(entries, composition{
			lead:      lead,
			trail:     int(d.runes[len(d.runes)-1]),
			composite: d.codePoint,
		})
	}
	// Ordered by the PAIR, because the pair is what the client binary-searches.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].lead != entries[j].lead {
			return entries[i].lead < entries[j].lead
		}
		return entries[i].trail < entries[j].trail
	})
	return entries
}

// combiningClass returns the canonical combining class of one code point.
func combiningClass(r rune) uint8 {
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return norm.NFD.Properties(buf[:n]).CCC()
}

// isCompositionExcluded reports whether a code point carrying a canonical
// decomposition is excluded from composition, by reading its NFC_Quick_Check
// property. QuickSpanString answers from the property table alone and composes
// nothing.
func isCompositionExcluded(r rune) bool {
	s := string(r)
	return norm.NFC.QuickSpanString(s) != len(s)
}

// isHangulSyllable reports whether cp is one of the 11,172 Hangul syllables,
// which no generated table holds an entry for.
func isHangulSyllable(cp int) bool {
	return cp >= hangulSBase && cp < hangulSBase+hangulSCount
}

// decompositionNumbers flattens the decompositions into the fixed-width entries
// the script binary searches: the code point followed by maxDecomposition slots,
// the unused ones carrying 0.
func decompositionNumbers(entries []decomposition) []int {
	numbers := make([]int, 0, (1+maxDecomposition)*len(entries))
	for _, e := range entries {
		numbers = append(numbers, e.codePoint)
		for i := 0; i < maxDecomposition; i++ {
			if i < len(e.runes) {
				numbers = append(numbers, int(e.runes[i]))
				continue
			}
			numbers = append(numbers, 0)
		}
	}
	return numbers
}

// combiningClassNumbers flattens the class spans into the start, length, class
// triples the script binary searches.
func combiningClassNumbers(spans []classSpan) []int {
	numbers := make([]int, 0, 3*len(spans))
	for _, s := range spans {
		numbers = append(numbers, s.start, s.length, s.class)
	}
	return numbers
}

// compositionNumbers flattens the primary composites into the lead, trail,
// composite triples the script binary searches.
func compositionNumbers(entries []composition) []int {
	numbers := make([]int, 0, 3*len(entries))
	for _, e := range entries {
		numbers = append(numbers, e.lead, e.trail, e.composite)
	}
	return numbers
}
