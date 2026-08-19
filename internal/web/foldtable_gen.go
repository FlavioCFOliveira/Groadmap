//go:build ignore

// Command foldtable_gen writes the board search's folding rule into the script
// the binary serves, as the run-encoded FOLD_TABLE literal in
// static/task-search.js.
//
// The client MUST NOT fold a search term with the JavaScript platform's own case
// conversion: that conversion is Unicode's Default Case Conversion rather than
// the simple mapping the rule fixes, and its tables are of whatever Unicode
// version the browser ships. Shipping the server's own mapping removes both the
// conversion and the browser's Unicode version from the answer (SPEC/WEB.md
// § Roadmap Tasks Page, One rule, and only one implementation of it).
//
// Run it with `go generate ./internal/web/` and commit the result: the table is a
// generated but COMMITTED artefact, so `go build` stays a plain Go build with no
// code-generation step. Regeneration is not what keeps the two sides equal —
// TestTaskSearchScript_FoldTableIsTheServerFold is. That test compares the
// SHIPPED table against the server's own foldSearch over every code point of
// Unicode on every `go test ./...`, so a toolchain upgrade that moves a mapping
// fails the build gates and names the code point that moved; re-running this
// generator is then the fix, not the detection.
package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"unicode/utf8"
)

// scriptPath is the served asset the table is written into, relative to
// internal/web/ — the directory `go generate` runs this from.
const scriptPath = "static/task-search.js"

// The generated region is delimited by these two markers, which are hand-written
// in the script and are never emitted by this program: everything between them is
// replaced, everything outside is left exactly as it was.
const (
	beginMarker = "  /* BEGIN GENERATED FOLD TABLE */"
	endMarker   = "  /* END GENERATED FOLD TABLE */"
)

// maxCodePoint is the last code point of Unicode. Surrogates (U+D800-U+DFFF) are
// not scalar values and carry no case mapping, so no run may cover them.
const maxCodePoint = 0x10FFFF

// lineWidth caps the emitted table's line length so the asset stays reviewable in
// an ordinary diff.
const lineWidth = 92

// run is a maximal span of consecutive code points that all fold by the same
// delta: code point c in [start, start+length) folds to c+delta.
type run struct {
	start  int
	length int
	delta  int
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("foldtable_gen: ")

	runs := foldRuns()
	if len(runs) == 0 {
		log.Fatal("the folding rule maps nothing; the table would be empty")
	}

	script, err := os.ReadFile(scriptPath)
	if err != nil {
		log.Fatalf("reading %s: %v", scriptPath, err)
	}
	updated, err := replaceRegion(string(script), emit(runs))
	if err != nil {
		log.Fatalf("rewriting %s: %v", scriptPath, err)
	}
	if updated == string(script) {
		fmt.Printf("%s is already up to date (%d runs)\n", scriptPath, len(runs))
		return
	}
	if err := os.WriteFile(scriptPath, []byte(updated), 0o600); err != nil {
		log.Fatalf("writing %s: %v", scriptPath, err)
	}
	fmt.Printf("%s: wrote %d runs covering %d code points\n", scriptPath, len(runs), covered(runs))
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
		if cp >= 0xD800 && cp <= 0xDFFF {
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
		runs = append(runs, run{start: cp, length: 1, delta: delta})
	}
	return runs
}

// covered totals the code points the runs map.
func covered(runs []run) int {
	total := 0
	for _, r := range runs {
		total += r.length
	}
	return total
}

// emit renders the runs as the JavaScript literal the script carries: one flat
// array of start, length, delta triples, ordered by start, which the script binary
// searches.
func emit(runs []run) string {
	var out strings.Builder
	out.WriteString("  var FOLD_TABLE = [\n")

	line := "   "
	for i, r := range runs {
		triple := fmt.Sprintf(" %d,%d,%d", r.start, r.length, r.delta)
		if i < len(runs)-1 {
			triple += ","
		}
		if len(line)+len(triple) > lineWidth {
			out.WriteString(line + "\n")
			line = "   "
		}
		line += triple
	}
	out.WriteString(line + "\n")
	out.WriteString("  ];\n")
	return out.String()
}

// replaceRegion swaps the text between the two markers for the generated table,
// leaving the markers and everything around them untouched.
func replaceRegion(script, table string) (string, error) {
	begin := strings.Index(script, beginMarker)
	if begin < 0 {
		return "", fmt.Errorf("no %q marker", strings.TrimSpace(beginMarker))
	}
	end := strings.Index(script, endMarker)
	if end < 0 {
		return "", fmt.Errorf("no %q marker", strings.TrimSpace(endMarker))
	}
	if end < begin {
		return "", fmt.Errorf("the end marker precedes the begin marker")
	}
	return script[:begin] + beginMarker + "\n" + table + script[end:], nil
}
