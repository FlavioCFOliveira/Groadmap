package testenv

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This file is the regression gate for defect #295.
//
// The defect: SPEC/ARCHITECTURE.md carried a section called "Error Code Mapping"
// that published nineteen symbolic error identifiers — INVALID_INPUT,
// ROADMAP_NOT_FOUND, UNKNOWN_COMMAND and sixteen more — each mapped to an exit
// code. Not one of the nineteen existed anywhere in the Go source, under that
// name or any other. The project's real error vocabulary is the sentinel VALUES
// in internal/utils/errors.go, which the same file documents correctly in
// § Sentinel Error Catalogue. So an invented vocabulary sat beside the real one,
// close enough in spelling to be mistaken for it by a reader or by an agent
// generating code from the specification.
//
// Why a gate and not just the removal. Removing the section fixes today's tree
// and nothing else. The defect class is "the specification names a symbol the
// code does not have", and that class reappears every time someone writes a
// plausible-looking constant into a table. Nothing in `make check` read a
// symbolic name out of the SPEC before this file, so nothing would have noticed.
//
// Why it lives here and not in /tests. CLAUDE.md § 12 scopes the Python suite in
// /tests to tests that execute commands against the compiled binary at bin/rmp.
// This gate runs no command: it reconciles SPEC text against the Go source tree,
// which is what the invariant gates in this package already do.
//
// The rule, measured against the tree before it was written.
//
// A "symbolic identifier" is taken to be a SCREAMING_SNAKE_CASE token that fills
// an entire inline code span in a SPEC file: `UNKNOWN_COMMAND`, `EXIT_SUCCESS`,
// `TASK_STATUS_BACKLOG`. The shape is deliberate on both sides:
//
//   - At least one underscore is required. All-caps tokens without one are SQL
//     and Cypher keywords (NOT NULL, PRIMARY KEY, MATCH, RETURN) and prose
//     emphasis, none of which claim to be a project symbol.
//   - The token must be the WHOLE span. `TEXT NOT NULL` and
//     `ALTER TABLE ADD COLUMN` are spans containing several all-caps words; they
//     name SQL syntax, not an identifier, and matching inside a span would drag
//     every one of them in.
//
// Resolution is whole-word presence anywhere in the module's Go source. That is
// permissive on purpose: the claim being policed is existence, not location. Of
// the tokens that resolve today, TASK_STATUS_BACKLOG resolves only through a
// comment in internal/commands/task_mutate.go and VERIFIED_BY only through
// Cypher fixture strings in the graph tests (internal/commands and
// internal/web). Demanding a declared Go identifier would fail both, and neither
// is the defect. A name that appears nowhere at all is.
//
// The limitation that comes with that choice, stated rather than hidden: if some
// future test file happens to spell a re-published SPEC identifier in a fixture,
// the gate would resolve it. The register below and the floors keep the common
// case honest; the rare one is a trade against failing the tree for correct
// references today.
//
// Self-exclusion. This gate's own file is excluded from the Go index it builds,
// because it spells the nineteen removed identifiers in its reproduction fixture
// below. Without the exclusion the gate would index its own fixture and hand a
// clean bill of health to exactly the table it exists to reject.
// TestTheGateDoesNotResolveIdentifiersOutOfItsOwnFixture pins that.
//
// Two guards stop the gate from quietly becoming a no-op:
//
//  1. Floors. The number of SPEC documents scanned, the number of distinct
//     identifiers found, and the number that resolve must each clear a floor. A
//     scanner that stops matching finds nothing and passes vacuously; the floors
//     turn that into a failure.
//  2. Register currency. Every entry in the exemption register must still be
//     published by the SPEC and must still fail to resolve in Go. An entry that
//     no longer applies is deleted rather than left as a standing excuse.

// specSymbolSpanRe matches an inline code span whose entire content is a
// SCREAMING_SNAKE_CASE token.
var specSymbolSpanRe = regexp.MustCompile("`([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)`")

// specSymbolGateFileRelPath is this file, excluded from the Go index. See the
// "Self-exclusion" paragraph above.
const specSymbolGateFileRelPath = "internal/testenv/spec_symbol_gate_test.go"

// Floors. Measured after defect #295 was fixed: 14 SPEC documents publish 72
// distinct identifiers, 69 of which resolve in the Go source. Each floor sits
// far below its measurement, because its job is to catch a scanner that has
// stopped working, not to track the corpus.
const (
	minSpecDocumentsScanned = 8
	minDistinctSpecSymbols  = 50
	minResolvedSpecSymbols  = 45
)

// nonGoVocabulary is the register of identifiers the SPEC publishes that belong
// to a vocabulary other than this module's Go source. Each entry states which
// vocabulary, because "it is fine" is not a reason.
//
// An entry earns its place only while the SPEC still publishes the token AND the
// token still fails to resolve in Go; TestNonGoVocabularyRegisterIsCurrent
// enforces both directions.
var nonGoVocabulary = map[string]string{
	"DECIDED_BY": "Cypher edge type offered as an example of knowledge-graph modelling in " +
		"SPEC/GRAPH.md, which labels the list illustrative and not mandatory. Edge types are " +
		"authored at runtime by whoever writes the query; the binary declares none of them.",
	"REQUIRED_BY": "Cypher edge type from the same illustrative list in SPEC/GRAPH.md, for the " +
		"same reason.",
	"SQLITE_OPEN_URI": "Open flag from SQLite's own C API, named in SPEC/IMPLEMENTATION.md when " +
		"describing what the driver passes to SQLite. It belongs to SQLite's surface, not to this " +
		"module's Go source.",
}

// specSymbolCitation is one place a SPEC file publishes an identifier.
type specSymbolCitation struct {
	file string // slash-separated, relative to the module root
	line int
}

func (c specSymbolCitation) String() string {
	return c.file + ":" + itoa(c.line)
}

// scanSpecSymbols extracts every symbolic identifier from one document's text,
// keyed by token, with the 1-based line of each occurrence.
func scanSpecSymbols(relPath, text string) map[string][]specSymbolCitation {
	out := make(map[string][]specSymbolCitation)
	for i, line := range strings.Split(text, "\n") {
		for _, m := range specSymbolSpanRe.FindAllStringSubmatch(line, -1) {
			out[m[1]] = append(out[m[1]], specSymbolCitation{file: relPath, line: i + 1})
		}
	}
	return out
}

// collectSpecSymbols scans every *.md file in SPEC/ and returns the union, plus
// the number of documents read.
func collectSpecSymbols(t *testing.T, root string) (map[string][]specSymbolCitation, int) {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, "SPEC"))
	if err != nil {
		t.Fatalf("reading SPEC/: %v", err)
	}

	all := make(map[string][]specSymbolCitation)
	docs := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		docs++
		rel := "SPEC/" + e.Name()
		data, err := os.ReadFile(filepath.Join(root, "SPEC", e.Name()))
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		for tok, cites := range scanSpecSymbols(rel, string(data)) {
			all[tok] = append(all[tok], cites...)
		}
	}
	return all, docs
}

// goSourceText concatenates every .go file in the module except this gate, and
// reports whether the exclusion actually matched a file on disk.
func goSourceText(t *testing.T, root string) (string, bool) {
	t.Helper()

	excluded := filepath.Join(root, filepath.FromSlash(specSymbolGateFileRelPath))
	excludedSeen := false

	var b strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// .git holds packed objects, and .claude/worktrees holds whole second
			// checkouts of this repository; neither is this module's source.
			switch info.Name() {
			case ".git", "worktrees", "bin", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if path == excluded {
			excludedSeen = true
			return nil
		}
		// filepath.Clean keeps this read off the G304 taint path, which is how the
		// sibling gates in this package read a walked file too.
		data, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			return readErr
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module for Go sources: %v", err)
	}
	return b.String(), excludedSeen
}

// resolvesInGo reports whether tok appears as a whole word in the Go source.
func resolvesInGo(goText, tok string) bool {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(tok) + `\b`)
	return re.MatchString(goText)
}

// TestSpecSymbolicIdentifiersExistInTheCode is the gate proper. Every
// SCREAMING_SNAKE_CASE identifier the SPEC publishes in an inline code span must
// exist in the module's Go source, or be registered as belonging to another
// vocabulary with a stated reason.
func TestSpecSymbolicIdentifiersExistInTheCode(t *testing.T) {
	root := repoRoot(t)

	symbols, docs := collectSpecSymbols(t, root)
	goText, excludedSeen := goSourceText(t, root)

	if !excludedSeen {
		t.Fatalf("this gate excludes %s from the Go index so that its own reproduction fixture "+
			"cannot resolve the identifiers it rejects, but no such file was walked. The gate "+
			"has been renamed or moved without updating specSymbolGateFileRelPath, and it is "+
			"now indexing its own fixture", specSymbolGateFileRelPath)
	}

	var unresolved []string
	resolved := 0
	for _, tok := range sortedKeys(symbols) {
		if resolvesInGo(goText, tok) {
			resolved++
			continue
		}
		if _, registered := nonGoVocabulary[tok]; registered {
			continue
		}
		var where []string
		for _, c := range symbols[tok] {
			where = append(where, c.String())
		}
		unresolved = append(unresolved, tok+" published at "+strings.Join(where, ", "))
	}

	if len(unresolved) > 0 {
		t.Errorf("the specification publishes %d symbolic identifier(s) that exist nowhere in the "+
			"module's Go source:\n  %s\n\nThis is defect #295. A table of symbolic error names that "+
			"the binary does not have is read as the project's vocabulary and is not one. Either the "+
			"identifier exists in the code, or the specification does not name it. If it belongs to "+
			"another vocabulary altogether — a Cypher edge type, a SQLite C flag — add it to "+
			"nonGoVocabulary with the reason.",
			len(unresolved), strings.Join(unresolved, "\n  "))
	}

	// Floors: a scanner that has stopped matching finds nothing and passes.
	if docs < minSpecDocumentsScanned {
		t.Errorf("scanned %d SPEC document(s), floor is %d: the corpus walk is not reaching the "+
			"specification, so nothing below it means anything", docs, minSpecDocumentsScanned)
	}
	if len(symbols) < minDistinctSpecSymbols {
		t.Errorf("found %d distinct symbolic identifier(s) in %d SPEC document(s), floor is %d: "+
			"the span scanner has stopped recognising them, and a gate that recognises nothing "+
			"rejects nothing", len(symbols), docs, minDistinctSpecSymbols)
	}
	if resolved < minResolvedSpecSymbols {
		t.Errorf("only %d identifier(s) resolved against the Go source, floor is %d: the resolver "+
			"has stopped matching, which would make every correct identifier in the SPEC look like "+
			"defect #295", resolved, minResolvedSpecSymbols)
	}

	t.Logf("SPEC documents scanned: %d; distinct symbolic identifiers: %d; resolved in Go: %d; "+
		"registered as another vocabulary: %d", docs, len(symbols), resolved, len(nonGoVocabulary))
}

// TestNonGoVocabularyRegisterIsCurrent keeps the exemption register from rotting
// into a list of standing excuses. An entry must still be published by the SPEC,
// and must still fail to resolve in Go.
func TestNonGoVocabularyRegisterIsCurrent(t *testing.T) {
	root := repoRoot(t)

	symbols, _ := collectSpecSymbols(t, root)
	goText, _ := goSourceText(t, root)

	for _, tok := range sortedKeys(nonGoVocabulary) {
		if strings.TrimSpace(nonGoVocabulary[tok]) == "" {
			t.Errorf("%s is registered with no reason; an exemption without a stated vocabulary "+
				"is indistinguishable from an oversight", tok)
		}
		if _, published := symbols[tok]; !published {
			t.Errorf("%s is registered as belonging to another vocabulary, but no SPEC document "+
				"publishes it any more. Delete the entry: a register that outlives its subject "+
				"quietly exempts a name nobody is watching", tok)
		}
		if resolvesInGo(goText, tok) {
			t.Errorf("%s is registered as absent from the Go source, but it now resolves there. "+
				"Delete the entry so the identifier is checked like every other", tok)
		}
	}
}

// removedErrorCodeMappingIdentifiers is defect #295's own fixture: the nineteen
// identifiers the deleted "Error Code Mapping" table published. They are kept
// here, in the one file excluded from the Go index, so the gate can be run
// against the defect instead of only against the fixed tree.
var removedErrorCodeMappingIdentifiers = []string{
	"INVALID_INPUT",
	"INVALID_DATE",
	"INVALID_DATE_RANGE",
	"INVALID_PRIORITY",
	"INVALID_SEVERITY",
	"INVALID_STATUS_TRANSITION",
	"ROADMAP_NOT_FOUND",
	"ROADMAP_EXISTS",
	"TASK_NOT_FOUND",
	"TASKS_NOT_FOUND",
	"SOME_TASKS_NOT_FOUND",
	"SPRINT_NOT_FOUND",
	"NO_ROADMAP",
	"DB_ERROR",
	"SYSTEM_ERROR",
	"UNKNOWN_SUBCOMMAND",
	"UNKNOWN_COMMAND",
	"UPDATE_FAILED",
	"DELETE_FAILED",
}

// TestTheRemovedErrorCodeMappingWouldBeRejected runs the defect forwards. It
// rebuilds the deleted table as a SPEC document and requires the gate's rule to
// reject every row. Without this, "the gate passes" would only mean the tree is
// clean today, not that the gate would catch the table coming back.
func TestTheRemovedErrorCodeMappingWouldBeRejected(t *testing.T) {
	root := repoRoot(t)
	goText, _ := goSourceText(t, root)

	var b strings.Builder
	b.WriteString("### Error Code Mapping\n\n")
	b.WriteString("| Error Code | Exit Code | Meaning |\n")
	b.WriteString("|------------|-----------|---------|\n")
	for _, id := range removedErrorCodeMappingIdentifiers {
		b.WriteString("| `" + id + "` | 1 | reinstated by mistake |\n")
	}

	found := scanSpecSymbols("SPEC/ARCHITECTURE.md", b.String())
	if len(found) != len(removedErrorCodeMappingIdentifiers) {
		t.Fatalf("the span scanner extracted %d identifier(s) from the reinstated table, want %d: "+
			"the scanner no longer recognises the shape the defect was written in",
			len(found), len(removedErrorCodeMappingIdentifiers))
	}

	for _, id := range removedErrorCodeMappingIdentifiers {
		if _, registered := nonGoVocabulary[id]; registered {
			t.Errorf("%s is exempted by nonGoVocabulary, which would let defect #295 back in "+
				"through the register", id)
			continue
		}
		if resolvesInGo(goText, id) {
			t.Errorf("%s resolves against the Go source, so reinstating the Error Code Mapping "+
				"table would pass this gate. Defect #295 was that none of these names exists; if "+
				"one now does, the SPEC may name it and this fixture entry must go", id)
		}
	}
}

// TestTheGateDoesNotResolveIdentifiersOutOfItsOwnFixture pins the self-exclusion.
// This file spells all nineteen removed identifiers a few lines above; if the Go
// index included it, every one of them would resolve and the gate would approve
// the table it exists to reject.
func TestTheGateDoesNotResolveIdentifiersOutOfItsOwnFixture(t *testing.T) {
	root := repoRoot(t)

	own, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(specSymbolGateFileRelPath)))
	if err != nil {
		t.Fatalf("reading this gate's own file at %s: %v", specSymbolGateFileRelPath, err)
	}
	if !resolvesInGo(string(own), "UNKNOWN_COMMAND") {
		t.Fatalf("this gate's own file no longer spells the fixture identifiers, so the "+
			"self-exclusion check below proves nothing. Restore %s or delete the exclusion",
			"removedErrorCodeMappingIdentifiers")
	}

	goText, excludedSeen := goSourceText(t, root)
	if !excludedSeen {
		t.Fatalf("the exclusion of %s never matched a file, so the index is built from a tree "+
			"this gate does not recognise", specSymbolGateFileRelPath)
	}
	if resolvesInGo(goText, "UNKNOWN_COMMAND") {
		t.Errorf("the Go index resolves UNKNOWN_COMMAND, which exists in the module only inside " +
			"this gate's own fixture. The self-exclusion is not working, and the gate would pass " +
			"a reinstated Error Code Mapping table")
	}
}
