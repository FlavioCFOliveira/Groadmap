package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file is the regression gate that acceptance criterion 38 of
// SPEC/GRAPH.md requires (task #149).
//
// The defect it closes: SPEC/GRAPH.md § Engine Construction and Lifecycle used
// to state, in absolute terms, that every `rmp graph` invocation constructs the
// Cypher engine over a store, and it named cypher.NewEngine as a constructor the
// project does not use for persisted graphs. Both read paths — the CLI read
// subcommands and the web server — had always used exactly that constructor.
// Nothing failed, because nothing compared the sentence with the code; the two
// drifted for as long as they were written apart. The decision on #149 was to
// correct the specification, and § Engine Constructor by Path is now the single
// authoritative statement of which constructor each path uses.
//
// A gate for that is worth only what it reads. The two tests below therefore
// read BOTH sides and compare them; neither one repeats the answer:
//
//  1. the specification side is parsed out of SPEC/GRAPH.md — the constructor
//     table, the Engine path column of § Per-Subcommand Validation Rules that
//     the table's path names come from, and the paragraph naming the
//     constructors the project does not use;
//  2. the implementation side is swept out of the source tree with go/ast, so
//     the set of constructions is whatever the code actually contains, not a
//     list somebody remembered to keep up to date here.
//
// A test that merely restated the table in Go would pin a copy of the
// specification rather than the correspondence between the specification and the
// code, and would stay green through exactly the drift that produced #149.
//
// Why the file lives in internal/testenv. The gate spans two packages
// (internal/commands and internal/web) plus a SPEC file, so no single audited
// package is its home, and internal/testenv already holds the module-wide AST
// gate (hermetic_gate_test.go), whose repository walk, skip list and formatting
// helpers this file reuses instead of duplicating. The choice also buys a real
// property: internal/testenv has no first-party dependencies, so this audit
// compiles and runs against a tree in which the audited packages do not — which
// is precisely the tree an incorrect edit produces.
//
// Known limits, stated rather than papered over:
//
//   - The sweep is syntactic. It matches calls on the local name of the GoGraph
//     cypher import, so an aliased import is followed, but an engine reached
//     through a function value or through reflection is not seen. Nothing in
//     this project constructs one that way.
//   - A construction is attributed to the graph subcommands whose dispatch
//     reaches its enclosing function with a literal name. A construction placed
//     where that attribution cannot reach it is reported as a path the table
//     does not list. That bias is deliberate: the opposite one would let a new
//     construction through unexamined, which is the failure this gate exists to
//     prevent.

// Paths and section headings the two tests read, relative to the module root.
const (
	specGraphRelPath = "SPEC/GRAPH.md"

	constructorTableHeading = "### Engine Constructor by Path"
	perSubcommandHeading    = "### Per-Subcommand Validation Rules"
)

// noOtherConstructorMarker opens the paragraph of § Engine Constructor by Path
// that names the constructors Groadmap does not use. The scan for those names is
// anchored on it and stops at the end of that paragraph, so a constructor named
// anywhere else in the section — in the rationale that follows it, say — is not
// silently read as a claim about usage. A reworded marker fails the test rather
// than quietly narrowing what it inspects.
const noOtherConstructorMarker = "Groadmap constructs an engine through no other constructor."

// The GoGraph packages the sweep recognises. Import paths rather than package
// names, so an aliased import is followed by resolving the alias per file.
const (
	goGraphModulePath = "github.com/FlavioCFOliveira/GoGraph"
	cypherImportPath  = goGraphModulePath + "/cypher"
	walImportPath     = goGraphModulePath + "/store/wal"
	txnImportPath     = goGraphModulePath + "/store/txn"
)

const (
	// engineConstructorPrefix is what makes a cypher package function an engine
	// constructor. TestGraphEngineConstructorInventoryMatchesGoGraph proves the
	// prefix covers every constructor the pinned engine exposes, so the sweep
	// can rely on it and needs no hard-coded list of names.
	engineConstructorPrefix = "NewEngine"

	// txnStoreConstructorPrefix is what makes a txn package function a
	// transactional store constructor (txn.NewStore, txn.NewStoreWithOptions).
	txnStoreConstructorPrefix = "NewStore"

	// engineTypeName is the type an engine constructor returns.
	engineTypeName = "Engine"

	// webPackageName is the package that serves the web graph page and the web
	// graph data endpoint, which is the surface the table's second row names.
	webPackageName = "web"
)

// storeExpectation is the fourth column of § Engine Constructor by Path:
// whether the path opens a transactional store and a write-ahead-log writer.
type storeExpectation int

const (
	// The zero value is deliberately not an expectation: a cell this gate
	// cannot read is fatal, never silently "no requirement".
	_ storeExpectation = iota
	expectNeither
	expectBoth
)

func (e storeExpectation) String() string {
	switch e {
	case expectNeither:
		return "neither a transactional store nor a write-ahead-log writer"
	case expectBoth:
		return "both a transactional store and a write-ahead-log writer"
	default:
		return "unreadable"
	}
}

// specConstructorRow is one data row of § Engine Constructor by Path.
type specConstructorRow struct {
	path        string   // the Path cell, a name of § Per-Subcommand Validation Rules
	surface     string   // the Surface cell, verbatim
	constructor string   // the single constructor the row names, qualified
	subcommands []string // the `graph X` subcommands the Surface cell names, sorted
	expect      storeExpectation
	line        int
	claimed     int // how many constructions in the tree matched this row
}

// constructionSite is one call to a Cypher engine constructor in production
// source.
type constructionSite struct {
	file        string   // repository-relative
	pkg         string   // package name as declared
	dir         string   // repository-relative directory
	function    string   // enclosing top-level function
	constructor string   // as written, normalised to the cypher package name
	subcommands []string // graph subcommands whose dispatch reaches function
	line        int
	opensStore  bool
	opensWAL    bool
}

// where renders the site for a failure message.
func (s *constructionSite) where() string {
	return s.file + ":" + itoa(s.line) + " (" + s.function + ")"
}

// mustConstructIn are the directories that MUST hold at least one engine
// construction. Without this anchor the sweep could stop matching — a renamed
// import, a changed AST shape — and the gate would report success over an empty
// set, which is how a guard quietly becomes decoration.
var mustConstructIn = []string{"internal/commands", "internal/web"}

// Cell scanners. Backticked spans carry every name the table states, so all
// three read the same span and differ only in what they accept inside it.
var (
	backtickedSpan     = regexp.MustCompile("`([^`]+)`")
	graphSubcommandRef = regexp.MustCompile(`^graph ([a-z]+)$`)
	constructorRef     = regexp.MustCompile(`^(?:cypher\.)?(NewEngine[A-Za-z]*)$`)
	tableSeparatorCell = regexp.MustCompile(`^:?-{3,}:?$`)
)

// ---------------------------------------------------------------------------
// 1. The inventory: the specification accounts for every constructor the pinned
//    engine exposes, and puts each one on exactly one side of the line.
// ---------------------------------------------------------------------------

// TestGraphEngineConstructorInventoryMatchesGoGraph reconciles the two lists
// SPEC/GRAPH.md § Engine Constructor by Path keeps — the constructors the table
// says Groadmap uses, and the ones the paragraph after it says Groadmap does not
// — with the constructors the pinned GoGraph release actually exposes.
//
// Its value is the case the second test cannot see: an upstream release adding a
// seventh constructor. The specification would then be making a closed claim
// about a set it no longer enumerates, and the next person to reach for the new
// constructor would find nothing in the SPEC forbidding it.
func TestGraphEngineConstructorInventoryMatchesGoGraph(t *testing.T) {
	spec := readRepoFileAt(t, specGraphRelPath)
	rows := parseConstructorTable(t, spec)
	unused := parseUnusedConstructors(t, spec)
	universe := goGraphEngineConstructors(t)

	used := make(map[string]bool, len(rows))
	for _, row := range rows {
		used[strings.TrimPrefix(row.constructor, "cypher.")] = true
	}
	if len(used) == 0 {
		t.Fatalf("%s § Engine Constructor by Path names no constructor at all", specGraphRelPath)
	}

	// The sweep in TestGraphEngineConstructionsMatchSpec recognises a
	// constructor by its name prefix rather than by a list. That is sound only
	// while every constructor the engine exposes carries the prefix, so the
	// assumption is checked against the upstream source instead of assumed.
	for _, name := range universe {
		if !strings.HasPrefix(name, engineConstructorPrefix) {
			t.Errorf("GoGraph's cypher package exposes the engine constructor %s, which does not start "+
				"with %q. The source sweep in this file finds constructions by that prefix, so it cannot "+
				"see a call to this one: teach scanEngineConstructions the new shape before relying on "+
				"either test again", name, engineConstructorPrefix)
		}
	}

	for _, name := range unused {
		if used[name] {
			t.Errorf("%s § Engine Constructor by Path both uses %s in its table and lists it among the "+
				"constructors Groadmap does not use. The section contradicts itself, so neither statement "+
				"can be relied on", specGraphRelPath, name)
		}
	}

	accounted := make(map[string]bool, len(used)+len(unused))
	for name := range used {
		accounted[name] = true
	}
	for _, name := range unused {
		accounted[name] = true
	}

	for _, name := range universe {
		if !accounted[name] {
			t.Errorf("GoGraph %s exposes cypher.%s, which %s § Engine Constructor by Path neither uses in "+
				"its table nor lists among the constructors Groadmap does not use. That section claims to "+
				"cover every engine Groadmap constructs and to name the alternatives it declines, so it is "+
				"now incomplete: add %s to the paragraph that begins %q, or add a row for it",
				goGraphVersion(t), name, specGraphRelPath, name, noOtherConstructorMarker)
		}
	}

	inUniverse := make(map[string]bool, len(universe))
	for _, name := range universe {
		inUniverse[name] = true
	}
	for name := range accounted {
		if !inUniverse[name] {
			t.Errorf("%s § Engine Constructor by Path names cypher.%s, which GoGraph %s does not expose. "+
				"Either the constructor was renamed or removed upstream and the specification outlived it, "+
				"or the name is misspelt — and a misspelt name in the table is a row this gate can never "+
				"match against the code", specGraphRelPath, name, goGraphVersion(t))
		}
	}
}

// ---------------------------------------------------------------------------
// 2. The gate criterion 38 defines: every engine the implementation constructs
//    is constructed through the constructor the table gives for its path.
// ---------------------------------------------------------------------------

// TestGraphEngineConstructionsMatchSpec enumerates every Cypher engine the
// implementation constructs to serve a graph subcommand or a web graph request,
// and fails if any of them is constructed through a constructor other than the
// one SPEC/GRAPH.md § Engine Constructor by Path gives for that path, or if an
// engine is constructed on a path that table does not list.
//
// Each construction is attributed to a row of the table by its surface, which is
// read off the code: the graph subcommands whose dispatch reaches the enclosing
// function with a literal name, or — for the row whose surface names no
// subcommand — the web package. The attribution is deliberately independent of
// the constructor being checked, so swapping only the constructor cannot move a
// construction to a row that would accept it.
func TestGraphEngineConstructionsMatchSpec(t *testing.T) {
	spec := readRepoFileAt(t, specGraphRelPath)
	rows := parseConstructorTable(t, spec)
	pathBySubcommand := parseEnginePathColumn(t, spec)
	sites := scanEngineConstructions(t, repoRoot(t))

	if len(sites) == 0 {
		t.Fatal("the sweep found no engine construction anywhere in the production source. " +
			"The implementation certainly constructs one, so the sweep has stopped matching and this " +
			"gate would now pass whatever the code did")
	}
	for _, dir := range mustConstructIn {
		if !anyConstructionIn(sites, dir) {
			t.Errorf("no engine construction was found in %s, which serves one of the surfaces "+
				"%s § Engine Constructor by Path lists. Either the construction moved — in which case "+
				"the table's Surface column needs amending — or the sweep no longer recognises it",
				dir, specGraphRelPath)
		}
	}

	for _, site := range sites {
		row := matchRow(t, rows, site)
		if row == nil {
			continue // matchRow has already reported why
		}
		row.claimed++

		if site.constructor != row.constructor {
			t.Errorf("%s constructs the engine through %s, but %s § Engine Constructor by Path gives %s "+
				"for the %s path serving %s.\n"+
				"  That table is the single authoritative statement of the constructor each path uses, so "+
				"one of the two is wrong.\n"+
				"  If the code is right, amend the table first: the section requires the change to be made "+
				"there before it is made here.\n"+
				"  If the table is right, restore the construction to %s.",
				site.where(), site.constructor, specGraphRelPath, row.constructor,
				row.path, row.surface, row.constructor)
		}

		checkStoreExpectation(t, row, site)
		checkPathAgainstSubcommands(t, row, site, pathBySubcommand)
	}

	for _, row := range rows {
		if row.claimed == 0 {
			t.Errorf("%s:%d lists a %s path serving %s, and the sweep found no construction on it. "+
				"The table claims to cover every Cypher engine Groadmap constructs; a row nothing "+
				"matches means the surface was removed, renamed, or moved out of reach of the sweep, "+
				"and until it is reconciled this gate is checking one path fewer than it reports",
				specGraphRelPath, row.line, row.path, row.surface)
		}
	}
}

// checkStoreExpectation compares the fourth column of the table with what the
// construction's enclosing function actually opens.
func checkStoreExpectation(t *testing.T, row *specConstructorRow, site *constructionSite) {
	t.Helper()

	opens := expectNeither
	if site.opensStore && site.opensWAL {
		opens = expectBoth
	}
	if site.opensStore != site.opensWAL {
		opened, missing := "a transactional store", "a write-ahead-log writer"
		if site.opensWAL {
			opened, missing = missing, opened
		}
		t.Errorf("%s opens %s but not %s. %s § Engine Constructor by Path knows only two shapes — both "+
			"opened, or neither — because a transactional store is constructed over a write-ahead-log "+
			"writer. A construction with one and not the other matches no row",
			site.where(), opened, missing, specGraphRelPath)
		return
	}
	if opens != row.expect {
		t.Errorf("%s opens %s, and %s:%d says the %s path serving %s opens %s.\n"+
			"  The fourth column is the reason the third one reads as it does: a read is given a plain "+
			"engine precisely because it opens no write-side resource. A construction that disagrees with "+
			"it has changed the path, not just the call",
			site.where(), opens, specGraphRelPath, row.line, row.path, row.surface, row.expect)
	}
}

// checkPathAgainstSubcommands ties the two tables together: the Path cell of
// § Engine Constructor by Path must be the Engine path § Per-Subcommand
// Validation Rules gives for every subcommand the construction serves.
func checkPathAgainstSubcommands(t *testing.T, row *specConstructorRow, site *constructionSite, pathBySubcommand map[string]string) {
	t.Helper()

	for _, subcommand := range site.subcommands {
		specPath, ok := pathBySubcommand[subcommand]
		if !ok {
			t.Errorf("%s serves `graph %s`, which %s § Per-Subcommand Validation Rules does not list. "+
				"§ Engine Constructor by Path takes its path names from that table, so a subcommand "+
				"missing there has no path and cannot be checked against any row",
				site.where(), subcommand, specGraphRelPath)
			continue
		}
		if !strings.EqualFold(specPath, row.path) {
			t.Errorf("%s serves `graph %s`, which %s § Per-Subcommand Validation Rules puts on the %s "+
				"path, but the construction matched the %s row of § Engine Constructor by Path (%s:%d). "+
				"The two tables disagree about which path this subcommand runs on",
				site.where(), subcommand, specGraphRelPath, specPath, row.path, specGraphRelPath, row.line)
		}
	}
}

// matchRow attributes a construction to the row of § Engine Constructor by Path
// whose surface it serves, and reports a construction on a path the table does
// not list. The attribution never looks at the constructor: a row is chosen by
// what the construction serves, so the constructor check that follows is a real
// comparison and not a tautology.
func matchRow(t *testing.T, rows []*specConstructorRow, site *constructionSite) *specConstructorRow {
	t.Helper()

	if len(site.subcommands) > 0 {
		matched := make([]*specConstructorRow, 0, 1)
		for _, row := range rows {
			if equalStrings(row.subcommands, site.subcommands) {
				matched = append(matched, row)
			}
		}
		switch len(matched) {
		case 1:
			return matched[0]
		case 0:
			t.Errorf("%s constructs an engine serving %s, and no row of %s § Engine Constructor by Path "+
				"names that set of subcommands. Either a construction was added on a path the table does "+
				"not list, or a subcommand was added to an existing path without extending the row's "+
				"Surface cell. The table is required to cover every Cypher engine Groadmap constructs",
				site.where(), quotedSubcommands(site.subcommands), specGraphRelPath)
		default:
			t.Errorf("%s constructs an engine serving %s, and %d rows of %s § Engine Constructor by Path "+
				"name that same set of subcommands. A surface on two rows makes the constructor for it "+
				"ambiguous", site.where(), quotedSubcommands(site.subcommands), len(matched), specGraphRelPath)
		}
		return nil
	}

	// No subcommand reaches this construction, so the only surface the table
	// describes that it can be is the web one — the row that names no
	// subcommand at all.
	matched := make([]*specConstructorRow, 0, 1)
	for _, row := range rows {
		if len(row.subcommands) == 0 {
			matched = append(matched, row)
		}
	}
	if len(matched) != 1 {
		t.Errorf("%s constructs an engine that no graph subcommand reaches, and %d rows of %s "+
			"§ Engine Constructor by Path name no subcommand. Exactly one row is expected to describe "+
			"the web surface", site.where(), len(matched), specGraphRelPath)
		return nil
	}
	row := matched[0]
	if !strings.Contains(strings.ToLower(row.surface), webPackageName) {
		t.Errorf("%s:%d is the only row of § Engine Constructor by Path naming no graph subcommand, so "+
			"it is the row the web surface must match, but its Surface cell (%q) does not mention the "+
			"web at all. The table's shape has changed and this gate can no longer attribute the web "+
			"construction", specGraphRelPath, row.line, row.surface)
		return nil
	}
	if site.pkg != webPackageName {
		t.Errorf("%s constructs an engine in package %s that no graph subcommand reaches. %s § Engine "+
			"Constructor by Path lists three surfaces — the CLI read subcommands, the CLI write "+
			"subcommands, and the web interface — and this construction is on none of them, so it is a "+
			"path the table does not list", site.where(), site.pkg, specGraphRelPath)
		return nil
	}
	return row
}

// ---------------------------------------------------------------------------
// The specification side.
// ---------------------------------------------------------------------------

// parseConstructorTable reads the table of § Engine Constructor by Path.
func parseConstructorTable(t *testing.T, spec string) []*specConstructorRow {
	t.Helper()

	section, bodyLine := specSection(t, spec, constructorTableHeading)
	headers := []string{"Path", "Surface", "GoGraph constructor", "Transactional store and write-ahead-log writer"}
	table := specTable(t, section, bodyLine, constructorTableHeading, headers)

	rows := make([]*specConstructorRow, 0, len(table))
	for _, raw := range table {
		row := &specConstructorRow{
			path:        raw.cells[0],
			surface:     raw.cells[1],
			subcommands: graphSubcommandsIn(raw.cells[1]),
			line:        raw.line,
		}

		names := constructorsIn(raw.cells[2])
		if len(names) != 1 {
			t.Fatalf("%s:%d names %d constructors in its GoGraph constructor cell (%q); each row must "+
				"name exactly one, or the path it describes has no single answer",
				specGraphRelPath, raw.line, len(names), raw.cells[2])
		}
		row.constructor = "cypher." + names[0]

		switch cell := raw.cells[3]; {
		case strings.HasPrefix(cell, "Neither"):
			row.expect = expectNeither
		case strings.HasPrefix(cell, "Both"):
			row.expect = expectBoth
		default:
			t.Fatalf("%s:%d has a transactional-store cell this gate cannot read (%q). It recognises "+
				"cells beginning \"Neither\" and \"Both\"; the wording changed, and a gate that cannot "+
				"read the column must not report that the code agrees with it",
				specGraphRelPath, raw.line, cell)
		}

		rows = append(rows, row)
	}

	if len(rows) < 2 {
		t.Fatalf("%s § Engine Constructor by Path parsed only %d rows; the table covers at least a read "+
			"path and a write path, so the parse has drifted from the table's shape",
			specGraphRelPath, len(rows))
	}
	return rows
}

// parseEnginePathColumn reads the Engine path column of § Per-Subcommand
// Validation Rules, which is where the path names used by § Engine Constructor
// by Path are defined.
func parseEnginePathColumn(t *testing.T, spec string) map[string]string {
	t.Helper()

	section, bodyLine := specSection(t, spec, perSubcommandHeading)
	headers := []string{"Subcommand", "Accepts", "Rejects", "Engine path"}
	table := specTable(t, section, bodyLine, perSubcommandHeading, headers)

	paths := make(map[string]string, len(table))
	for _, raw := range table {
		names := graphSubcommandsIn(raw.cells[0])
		if len(names) != 1 {
			t.Fatalf("%s:%d names %d subcommands in its Subcommand cell (%q); each row describes exactly "+
				"one", specGraphRelPath, raw.line, len(names), raw.cells[0])
		}
		if raw.cells[3] == "" {
			t.Fatalf("%s:%d has an empty Engine path cell for `graph %s`", specGraphRelPath, raw.line, names[0])
		}
		paths[names[0]] = raw.cells[3]
	}

	if len(paths) == 0 {
		t.Fatalf("%s § Per-Subcommand Validation Rules yielded no subcommand at all", specGraphRelPath)
	}
	return paths
}

// parseUnusedConstructors reads the constructors § Engine Constructor by Path
// says Groadmap does not use, from the paragraph that begins with
// noOtherConstructorMarker.
func parseUnusedConstructors(t *testing.T, spec string) []string {
	t.Helper()

	section, bodyLine := specSection(t, spec, constructorTableHeading)
	start := strings.Index(section, noOtherConstructorMarker)
	if start < 0 {
		t.Fatalf("%s § Engine Constructor by Path (from line %d) no longer contains the sentence %q, "+
			"which is where this gate reads the constructors Groadmap declines to use. Either the "+
			"paragraph was reworded or it is gone; in both cases the gate is no longer reading the "+
			"claim it reports on", specGraphRelPath, bodyLine, noOtherConstructorMarker)
	}

	paragraph := section[start:]
	if end := strings.Index(paragraph, "\n\n"); end >= 0 {
		paragraph = paragraph[:end]
	}

	names := constructorsIn(paragraph)
	if len(names) == 0 {
		t.Fatalf("%s § Engine Constructor by Path states that Groadmap constructs an engine through no "+
			"other constructor, but names none in that paragraph:\n%s\nThe paragraph is what closes the "+
			"set; without names in it, nothing forbids reaching for another constructor",
			specGraphRelPath, paragraph)
	}
	return names
}

// specSection returns the body of a "### Heading" section and the file line its
// FIRST body line sits on, so a line number derived from an offset into the body
// names the line a reader will find. The heading is matched as a whole line, so
// a table-of-contents entry or a cross-reference cannot redirect the scan.
func specSection(t *testing.T, spec, heading string) (body string, bodyLine int) {
	t.Helper()

	anchor := "\n" + heading + "\n"
	start := strings.Index(spec, anchor)
	if start < 0 {
		t.Fatalf("%s has no %q section; this gate cannot verify itself", specGraphRelPath, heading)
	}
	headingLine := 2 + strings.Count(spec[:start], "\n")

	body = spec[start+len(anchor):]
	if end := strings.Index(body, "\n#"); end >= 0 {
		body = body[:end]
	}
	return body, headingLine + 1
}

// specTableRow is one parsed row of a markdown table, with the file line it came
// from so failures can name real lines.
type specTableRow struct {
	cells []string
	line  int
}

// specTable extracts the first markdown table of a section and returns its data
// rows. The header row is compared with the expected column names, so a
// reordered or renamed column fails loudly instead of silently shifting every
// cell this gate reads. Only the first contiguous run of table lines is taken,
// so a section holding two tables cannot have them merged into one.
func specTable(t *testing.T, section string, bodyLine int, heading string, headers []string) []specTableRow {
	t.Helper()

	lines := strings.Split(section, "\n")
	rows := make([]specTableRow, 0, len(lines))
	started := false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "|") {
			if started {
				break
			}
			continue
		}
		started = true
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		rows = append(rows, specTableRow{cells: cells, line: bodyLine + i})
	}

	if len(rows) < 3 {
		t.Fatalf("%s %s has no table with data rows (found %d table lines from line %d); the section's "+
			"shape has changed and nothing below is measuring the specification any more",
			specGraphRelPath, heading, len(rows), bodyLine)
	}

	if !equalStrings(rows[0].cells, headers) {
		t.Fatalf("%s:%d has the columns %q, and this gate reads the columns %q. A renamed or reordered "+
			"column means every cell it reads is the wrong one",
			specGraphRelPath, rows[0].line, rows[0].cells, headers)
	}
	for _, cell := range rows[1].cells {
		if !tableSeparatorCell.MatchString(cell) {
			t.Fatalf("%s:%d is not the separator row of the %s table (%q); the table is malformed",
				specGraphRelPath, rows[1].line, heading, rows[1].cells)
		}
	}

	data := rows[2:]
	for _, row := range data {
		if len(row.cells) != len(headers) {
			t.Fatalf("%s:%d has %d cells and the %s table has %d columns; the row is malformed and the "+
				"cells this gate reads are not the ones it names",
				specGraphRelPath, row.line, len(row.cells), heading, len(headers))
		}
	}
	return data
}

// graphSubcommandsIn returns the graph subcommands a cell names in backticks,
// sorted and deduplicated. A backticked span that is not exactly "graph <name>"
// — a SPEC cross-reference, a clause name — is not one.
func graphSubcommandsIn(cell string) []string {
	found := make(map[string]bool)
	for _, span := range backtickedSpan.FindAllStringSubmatch(cell, -1) {
		if match := graphSubcommandRef.FindStringSubmatch(span[1]); match != nil {
			found[match[1]] = true
		}
	}
	return sortedSet(found)
}

// constructorsIn returns the engine constructors a passage names in backticks,
// as bare names, sorted and deduplicated. The table qualifies them and the
// paragraph does not, so both spellings are accepted.
func constructorsIn(passage string) []string {
	found := make(map[string]bool)
	for _, span := range backtickedSpan.FindAllStringSubmatch(passage, -1) {
		if match := constructorRef.FindStringSubmatch(span[1]); match != nil {
			found[match[1]] = true
		}
	}
	return sortedSet(found)
}

// ---------------------------------------------------------------------------
// The implementation side.
// ---------------------------------------------------------------------------

// packageScan accumulates one directory's production facts. Together the maps
// make the surface of a construction derivable from the code rather than
// declared here: which literal names reach a function, which functions it calls,
// and which functions open a write-side resource.
type packageScan struct {
	dir         string
	literalArgs map[string]map[string]bool // callee -> first string-literal arguments
	calls       map[string]map[string]bool // caller -> same-package callees
	directWAL   map[string]bool            // functions calling the wal package
	directStore map[string]bool            // functions calling txn.NewStore*
	sites       []*constructionSite
}

// scanEngineConstructions parses every production Go file in the module and
// returns each Cypher engine construction it finds, with the surface it serves
// and the write-side resources its enclosing function opens.
func scanEngineConstructions(t *testing.T, root string) []*constructionSite {
	t.Helper()

	scans := make(map[string]*packageScan)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && skipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		dir := filepath.ToSlash(filepath.Dir(rel))

		scan := scans[dir]
		if scan == nil {
			scan = &packageScan{
				dir:         dir,
				literalArgs: make(map[string]map[string]bool),
				calls:       make(map[string]map[string]bool),
				directWAL:   make(map[string]bool),
				directStore: make(map[string]bool),
			}
			scans[dir] = scan
		}
		inspectProductionFile(t, fset, path, filepath.ToSlash(rel), scan)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}

	sites := make([]*constructionSite, 0, 4)
	for _, dir := range sortedScanKeys(scans) {
		scan := scans[dir]
		if len(scan.sites) == 0 {
			continue
		}
		opensWAL := closeOverCalls(scan.directWAL, scan.calls)
		opensStore := closeOverCalls(scan.directStore, scan.calls)
		for _, site := range scan.sites {
			site.subcommands = sortedSet(scan.literalArgs[site.function])
			site.opensWAL = opensWAL[site.function]
			site.opensStore = opensStore[site.function]
			sites = append(sites, site)
		}
	}
	return sites
}

// inspectProductionFile folds one file's findings into its package's record.
func inspectProductionFile(t *testing.T, fset *token.FileSet, path, rel string, scan *packageScan) {
	t.Helper()

	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", rel, err)
	}

	cypherName := importLocalName(file, cypherImportPath)
	walName := importLocalName(file, walImportPath)
	txnName := importLocalName(file, txnImportPath)

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		enclosing := funcKey(fn)

		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := unwrapGenericCall(call.Fun).(type) {
			case *ast.SelectorExpr:
				qualifier, ok := fun.X.(*ast.Ident)
				if !ok {
					return true
				}
				switch {
				case cypherName != "" && qualifier.Name == cypherName &&
					strings.HasPrefix(fun.Sel.Name, engineConstructorPrefix):
					scan.sites = append(scan.sites, &constructionSite{
						file:        rel,
						pkg:         file.Name.Name,
						dir:         scan.dir,
						function:    enclosing,
						constructor: "cypher." + fun.Sel.Name,
						line:        fset.Position(call.Pos()).Line,
					})
				case walName != "" && qualifier.Name == walName:
					scan.directWAL[enclosing] = true
				case txnName != "" && qualifier.Name == txnName &&
					strings.HasPrefix(fun.Sel.Name, txnStoreConstructorPrefix):
					scan.directStore[enclosing] = true
				}
			case *ast.Ident:
				// A call to a function of the same package. Both the edge and
				// any literal first argument are recorded: the edge carries the
				// write-side resources a helper opens back to its caller, and
				// the argument is how a subcommand name reaches the shared
				// handler that constructs the engine.
				addEdge(scan.calls, enclosing, fun.Name)
				if literal, ok := firstStringArgument(call); ok {
					addEdge(scan.literalArgs, fun.Name, literal)
				}
			}
			return true
		})
	}
}

// funcKey names a declaration for the call graph. Methods are keyed with their
// receiver type so two same-named methods do not merge; they take no part in the
// unqualified call graph, since a method call is a selector.
func funcKey(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return receiverTypeName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

// receiverTypeName renders a receiver type, pointer and generics stripped.
func receiverTypeName(expr ast.Expr) string {
	switch typ := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(typ.X)
	case *ast.IndexExpr:
		return receiverTypeName(typ.X)
	case *ast.IndexListExpr:
		return receiverTypeName(typ.X)
	case *ast.Ident:
		return typ.Name
	default:
		return "?"
	}
}

// unwrapGenericCall strips the explicit type arguments of a generic call, so
// txn.NewStoreWithOptions[string, float64](...) is seen as a call on the txn
// package and not as an index expression.
func unwrapGenericCall(fun ast.Expr) ast.Expr {
	switch typed := fun.(type) {
	case *ast.IndexExpr:
		return typed.X
	case *ast.IndexListExpr:
		return typed.X
	default:
		return fun
	}
}

// importLocalName returns the name a file refers to an import by — its alias
// when it has one — or "" when the file does not import it. Blank and dot
// imports yield "", since neither can qualify a call.
func importLocalName(file *ast.File, path string) string {
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil || value != path {
			continue
		}
		if spec.Name == nil {
			return path[strings.LastIndex(path, "/")+1:]
		}
		if spec.Name.Name == "_" || spec.Name.Name == "." {
			return ""
		}
		return spec.Name.Name
	}
	return ""
}

// firstStringArgument returns the value of a call's first argument when it is an
// untyped string literal.
func firstStringArgument(call *ast.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// addEdge records key -> value in a map of sets.
func addEdge(sets map[string]map[string]bool, key, value string) {
	set := sets[key]
	if set == nil {
		set = make(map[string]bool)
		sets[key] = set
	}
	set[value] = true
}

// closeOverCalls propagates a property from a function to its callers until the
// result stops changing, so a caller that opens a write-side resource through a
// helper is treated as opening it. Recursion terminates because the closure only
// ever grows and is bounded by the number of functions.
func closeOverCalls(direct map[string]bool, calls map[string]map[string]bool) map[string]bool {
	closure := make(map[string]bool, len(direct))
	for name := range direct {
		closure[name] = true
	}
	for changed := true; changed; {
		changed = false
		for caller, callees := range calls {
			if closure[caller] {
				continue
			}
			for callee := range callees {
				if closure[callee] {
					closure[caller] = true
					changed = true
					break
				}
			}
		}
	}
	return closure
}

// goGraphEngineConstructors returns the names of every function the pinned
// GoGraph release's cypher package exposes that returns an *Engine. It reads the
// upstream source in the module cache, so the inventory is the dependency's, not
// a copy of it kept here.
func goGraphEngineConstructors(t *testing.T) []string {
	t.Helper()

	dir := goGraphPackageDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the GoGraph cypher package at %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	names := make(map[string]bool)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || !returnsEnginePointer(fn) {
				continue
			}
			names[fn.Name.Name] = true
		}
	}

	constructors := sortedSet(names)
	if len(constructors) == 0 {
		t.Fatalf("no engine constructor was found in the GoGraph cypher package at %s; the upstream "+
			"package moved or changed shape, and an empty inventory would let this gate approve any "+
			"specification at all", dir)
	}
	return constructors
}

// returnsEnginePointer reports whether a function returns exactly one *Engine.
func returnsEnginePointer(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	pointer, ok := fn.Type.Results.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := pointer.X.(*ast.Ident)
	return ok && ident.Name == engineTypeName
}

// goGraphPackageDir locates the pinned GoGraph cypher package in the module
// cache. `go list -m` is asked rather than the cache path being reconstructed,
// so the answer is the one the build itself uses.
func goGraphPackageDir(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", goGraphModulePath)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("locating the %s module: %v\n%s", goGraphModulePath, err, out)
	}

	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatalf("the %s module has no directory in the module cache; run `go mod download`", goGraphModulePath)
	}
	return filepath.Join(dir, "cypher")
}

// goGraphVersion reports the pinned GoGraph version, for failure messages that
// name the release whose surface was inspected.
func goGraphVersion(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}}", goGraphModulePath)
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		return "(version unknown)"
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------------------
// Small shared helpers.
// ---------------------------------------------------------------------------

// readRepoFileAt reads a file given by its module-root-relative path.
func readRepoFileAt(t *testing.T, rel string) string {
	t.Helper()

	path := filepath.Join(repoRoot(t), filepath.FromSlash(rel))
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(content)
}

// anyConstructionIn reports whether any construction was found in a directory.
func anyConstructionIn(sites []*constructionSite, dir string) bool {
	for _, site := range sites {
		if site.dir == dir {
			return true
		}
	}
	return false
}

// quotedSubcommands renders a surface as the specification writes it.
func quotedSubcommands(subcommands []string) string {
	quoted := make([]string, 0, len(subcommands))
	for _, name := range subcommands {
		quoted = append(quoted, "`graph "+name+"`")
	}
	return strings.Join(quoted, ", ")
}

// sortedSet returns a set's members in a stable order.
func sortedSet(set map[string]bool) []string {
	members := make([]string, 0, len(set))
	for member := range set {
		members = append(members, member)
	}
	sort.Strings(members)
	return members
}

// sortedScanKeys returns the scanned directories in a stable order, so failures
// are reproducible rather than dependent on map iteration.
func sortedScanKeys(scans map[string]*packageScan) []string {
	keys := make([]string, 0, len(scans))
	for key := range scans {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// equalStrings compares two ordered string slices.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
