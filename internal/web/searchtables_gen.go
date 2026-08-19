//go:build ignore

// Command searchtables_gen writes the two halves of the board search's term
// normalisation into the script the binary serves: the run-encoded FOLD_TABLE
// carrying the folding rule's mapping, and the run-encoded SPACE_TABLE carrying
// the trim rule's whitespace set, both in static/task-search.js.
//
// The client MUST NOT normalise a term with the JavaScript platform's own
// functions. Its case conversion is Unicode's Default Case Conversion rather than
// the simple mapping the folding rule fixes, and its trimming removes a different
// set from the White_Space property the trim rule fixes — it keeps U+0085, which
// carries the property, and removes U+FEFF, which does not. Both functions read
// tables of whatever Unicode version the browser ships. Shipping the server's own
// mapping and the server's own set removes the platform and the browser's Unicode
// version from the answer on both counts (SPEC/WEB.md § Roadmap Tasks Page, One
// rule, and only one implementation of it; The trim rule).
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
	"strings"
	"unicode"
	"unicode/utf8"
)

// scriptPath is the served asset the tables are written into, relative to
// internal/web/ — the directory `go generate` runs this from.
const scriptPath = "static/task-search.js"

// maxCodePoint is the last code point of Unicode. Surrogates (U+D800-U+DFFF) are
// not scalar values, carry no case mapping and no property, so no span may cover
// them.
const maxCodePoint = 0x10FFFF

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
	if updated == string(script) {
		fmt.Printf("%s is already up to date (%d fold runs, %d whitespace spans)\n",
			scriptPath, len(folds), len(spaces))
		return
	}
	if err := os.WriteFile(scriptPath, []byte(updated), 0o600); err != nil {
		log.Fatalf("writing %s: %v", scriptPath, err)
	}
	fmt.Printf("%s: wrote %d fold runs covering %d code points, and %d whitespace spans "+
		"covering %d\n", scriptPath, len(folds), coveredRuns(folds), len(spaces), coveredSpans(spaces))
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
