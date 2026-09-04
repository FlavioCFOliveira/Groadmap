package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file is the enforcement SPEC/DATA_FORMATS.md § One Realisation of the
// Mapping requires: internal/testenv fails the build if a second realisation of
// the graph value mapping appears in production source, in the way it already
// fails the build for a second engine construction (engine_constructor_gate_test.go)
// and a second snapshot write (snapshot_writer_gate_test.go).
//
// THE DEFECT IT CLOSES. One mapping from an engine value to published JSON was
// written twice, in two packages, with nothing that failed when they disagreed:
// internal/commands.serializeValue served `rmp graph execute` and
// `rmp graph client`, and internal/web.serializeGraphValue served the graph data
// endpoint. Every side a test normally watches stayed quiet. Both copies
// compiled, because neither called the other. Both passed, because each was
// exercised against itself where it was exercised at all — and only ONE of them
// was: the CLI's copy, which publishes every top-level result cell a caller
// parses, had no direct test of any kind, while the copy whose element arms could
// not be reached from an HTTP request had a full suite (rmp task #394, FINDING
// #452). The divergence would have become visible only to a reader holding a CLI
// row and a web node's properties side by side.
//
// WHY A STATIC GATE AND NOT A TEST COMPARING THE TWO. A comparison test is the
// obvious instrument and it is the wrong one here. Once the second copy is gone
// there is nothing left to compare, and what remains to be prevented is a THIRD
// copy appearing later — which is a fact about the shape of the source, not about
// the behaviour of a function. internal/graphclient's package comment records
// that the third copy was very nearly written and declined for exactly this
// reason. A gate makes a second copy impossible rather than merely detectable,
// and that is the difference between the identity being a property of the code
// and being an assertion in a test.
//
// WHAT IS ENFORCED, IN THREE RULES.
//
//  1. The property-type mapping: the three renderings that turn a temporal into
//     its published text exist in one package. A second realisation cannot avoid
//     them — they ARE the mapping's observable output — so they identify one
//     wherever it is written.
//  2. The element mapping: the Node and Relationship objects are built in one
//     package, and the Path object — which is NOT shared, because only the CLI
//     publishes one — is built in exactly one other. A map literal carrying an
//     element's key set is an element object whatever it is called.
//  3. Every value kind the pinned engine can produce has a home: a row in the
//     shared realisation, or a declared owner that really does carry it. This is
//     the rule that reads BOTH sides — the engine's own source in the module
//     cache, and ours — so a kind added upstream fails the build here instead of
//     being published as a Go-rendered string that no specification fixes, and a
//     kind renamed upstream fails loudly instead of emptying the rule.
//
// KNOWN LIMITS, STATED RATHER THAN PAPERED OVER.
//
//   - The sweeps are syntactic. They read call expressions, composite literals
//     and struct tags, so a rendering assembled by string concatenation, or an
//     element object built field by field into a map[string]any across several
//     statements, is not seen. Nothing in this project writes one that way, and
//     rule 3 still requires whatever does exist to have a home.
//   - They see production files only. A test may express a published shape
//     deliberately — the suites in internal/graphjson and internal/commands do
//     exactly that, as the assertions they compare against — and holding test
//     files to these rules would delete the fences.
//   - Rules 1 and 2 say only that one package renders. That the CALL SITES route
//     through it is what the byte-identity of the three published surfaces
//     establishes, and what SPEC/DATA_FORMATS.md § Graph Client Result requires.

// graphJSONPkgDir is the one package allowed to render an engine value as
// published JSON: the home of the project's single realisation of the mapping.
const graphJSONPkgDir = "internal/graphjson"

// pathRowPkgDir is the one package allowed to build the Path object. The Path row
// is deliberately NOT in graphJSONPkgDir: the graph data endpoint decomposes a
// path into the elements it contains and publishes no path object at all, so the
// row already has one realisation by having one producer, and it stays with the
// surface that produces it (SPEC/DATA_FORMATS.md § One Realisation of the
// Mapping, "Only the CLI publishes a path").
const pathRowPkgDir = "internal/commands"

// exprImportPath is the pinned engine's value model: the domain of the mapping
// this file governs.
const exprImportPath = goGraphModulePath + "/cypher/expr"

// exprKindPrefix is what makes an exported constant of that package a value kind.
// The gate asserts below that the pinned engine declares at least one, so a
// rename upstream fails loudly instead of emptying the sweep.
const exprKindPrefix = "Kind"

// publishedRenderings are the temporal renderings SPEC/DATA_FORMATS.md
// § Property-Type Mapping fixes, keyed by the layout as it is written in Go. A
// production file that formats a value through one of them is either the single
// realisation or a second opinion about it.
//
// The three are the whole of the mapping's *layout* surface: the other temporal
// rows delegate to the engine's own String method, and the scalar rows have no
// layout at all. A second realisation that reproduced only those would publish
// nothing this project chose, so there would be nothing to keep in one place.
var publishedRenderings = map[string]string{
	`"2006-01-02"`:                    "the Date row - a calendar date as YYYY-MM-DD",
	`"2006-01-02T15:04:05.999999999"`: "the LocalDateTime row - a zoneless timestamp",
	"time.RFC3339Nano":                "the DateTime row - an instant rendered in UTC",
}

// elementKeySets are the discriminating JSON keys of each element object of
// SPEC/DATA_FORMATS.md § Graph element mapping, and the package that owns each.
//
// The keys are discriminating rather than complete on purpose: a literal that
// carries them plus more is still an element object, and a near-copy with one
// extra field is exactly the second realisation this gate exists against.
var elementKeySets = []struct {
	row  string
	dir  string
	keys []string
}{
	{row: "Node", dir: graphJSONPkgDir, keys: []string{"labels", "properties"}},
	{row: "Relationship", dir: graphJSONPkgDir, keys: []string{"startId", "endId"}},
	{row: "Path", dir: pathRowPkgDir, keys: []string{"nodes", "relationships"}},
}

// kindsOwnedElsewhere are the engine value kinds the shared realisation
// deliberately carries no row for, and the package that owns each instead.
//
// An entry is a claim about the code, in both directions, and the test asserts
// both: the named package must really carry the kind, and the shared realisation
// must really NOT carry it. An entry whose kind has since moved into the shared
// realisation would otherwise leave the coverage rule satisfied by an owner that
// no longer owns anything.
var kindsOwnedElsewhere = map[string]string{
	"KindPath": pathRowPkgDir,
}

// renderingSite is one call that formats a value through a published rendering.
type renderingSite struct {
	file      string // repository-relative
	dir       string // repository-relative directory
	function  string // enclosing top-level function
	rendering string // the layout, as written in Go
	line      int
}

// elementSite is one composite literal or struct declaration carrying an element
// object's key set.
type elementSite struct {
	file string
	dir  string
	keys string // the matched key set, joined, for the evidence line
	line int
}

// TestOneRealisationOfThePropertyTypeMapping asserts that the temporal
// renderings SPEC/DATA_FORMATS.md § Property-Type Mapping publishes are written
// in one package, and that they are still written there at all.
func TestOneRealisationOfThePropertyTypeMapping(t *testing.T) {
	sites := scanPublishedRenderings(t, repoRoot(t))

	// The anchor. Without it the rule passes vacuously the day the sweep stops
	// matching — a layout edited, a helper interposed — which is the drift that
	// would matter most, because the gate would then admit anything.
	rendered := make(map[string]bool, len(publishedRenderings))
	for _, site := range sites {
		if site.dir == graphJSONPkgDir {
			rendered[site.rendering] = true
		}
	}
	missing := make([]string, 0, len(publishedRenderings))
	for rendering := range publishedRenderings {
		if !rendered[rendering] {
			missing = append(missing, rendering+" ("+publishedRenderings[rendering]+")")
		}
	}
	sort.Strings(missing)
	for _, rendering := range missing {
		t.Errorf("no production file in %s formats a value through %s.\n"+
			"  That rendering is published by SPEC/DATA_FORMATS.md § Property-Type Mapping, so it is "+
			"written somewhere; this sweep can no longer see where, and every other rule in this test "+
			"would now pass whatever the code did. If the realisation moved, move this gate with it.",
			graphJSONPkgDir, rendering)
	}

	offenders := make([]string, 0, len(sites))
	for _, site := range sites {
		if site.dir == graphJSONPkgDir {
			continue
		}
		offenders = append(offenders,
			site.file+":"+itoa(site.line)+" ("+site.function+"): "+site.rendering+
				" - "+publishedRenderings[site.rendering])
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("%d production site(s) outside %s render an engine value into published JSON:\n%s\n"+
			"    SPEC/DATA_FORMATS.md § One Realisation of the Mapping allows exactly one, and every\n"+
			"    surface that publishes a graph value calls it. Two expressions of one mapping is how\n"+
			"    two surfaces come to answer one question differently: one is corrected and the other\n"+
			"    is not, both keep compiling because neither calls the other, and both keep passing\n"+
			"    because each is exercised against itself. That is rmp task #394, which removed the\n"+
			"    second copy this rule now keeps out. Call the %s package instead.",
			len(offenders), graphJSONPkgDir, indentEvidence(offenders), graphJSONPkgDir)
	}
}

// TestOneRealisationOfTheElementMapping asserts that each element object of
// SPEC/DATA_FORMATS.md § Graph element mapping is built in exactly one package:
// the Node and Relationship rows in the shared realisation, and the Path row in
// the single surface that publishes one.
func TestOneRealisationOfTheElementMapping(t *testing.T) {
	sites := scanElementObjects(t, repoRoot(t))

	for _, want := range elementKeySets {
		key := strings.Join(want.keys, "+")
		found := sites[key]

		// The anchor, per row: the object must still be built where this gate
		// says it is, or the row's rule below is checking nothing.
		built := false
		offenders := make([]string, 0, len(found))
		for _, site := range found {
			if site.dir == want.dir {
				built = true
				continue
			}
			offenders = append(offenders, site.file+":"+itoa(site.line)+": {"+site.keys+"}")
		}
		if !built {
			t.Errorf("no production file in %s builds the %s object (a literal carrying %s).\n"+
				"  SPEC/DATA_FORMATS.md § Graph element mapping publishes that object, so something "+
				"builds it; this sweep can no longer see what, and the rule below would now admit a "+
				"second copy silently. If the realisation moved, move this gate with it.",
				want.dir, want.row, key)
		}

		sort.Strings(offenders)
		if len(offenders) > 0 {
			t.Errorf("%d production site(s) outside %s build the %s element object:\n%s\n"+
				"    SPEC/DATA_FORMATS.md § One Realisation of the Mapping allows one realisation of\n"+
				"    the Node and Relationship rows, in %s, and leaves the Path row with %s because\n"+
				"    that is the only surface which publishes a path at all. The element mapping was\n"+
				"    written THREE times before rmp task #394 — the CLI's arms, the web's unreachable\n"+
				"    arms, and the web collector's own node and edge objects — and nothing compared\n"+
				"    any two of them. Call the shared builder instead.",
				len(offenders), want.dir, want.row, indentEvidence(offenders),
				graphJSONPkgDir, pathRowPkgDir)
		}
	}
}

// TestEveryEngineValueKindHasOneHome asserts that every value kind the pinned
// engine can produce is mapped somewhere deliberate.
//
// This is the rule that reads both sides. The kinds are enumerated from the
// engine's own source in the module cache rather than from a list kept here, so
// the rule cannot go stale in the direction that matters: a kind added upstream
// arrives as a FAILURE, naming itself, instead of falling through to the shared
// realisation's fallback and being published as whatever the engine's String
// method happens to produce — a shape no specification fixes, and precisely the
// class of defect that a reader of rmp task #394 first mistook the Path arm for.
func TestEveryEngineValueKindHasOneHome(t *testing.T) {
	kinds := engineValueKinds(t)
	if len(kinds) == 0 {
		t.Fatalf("GoGraph %s declares no exported %s* constant in %s. Either the value model was "+
			"renamed upstream and this gate outlived it, or the prefix is wrong — and a wrong prefix "+
			"here is a rule that can never be violated", goGraphVersion(t), exprKindPrefix, exprImportPath)
	}

	shared := kindsSwitchedOn(t, filepath.Join(repoRoot(t), filepath.FromSlash(graphJSONPkgDir)))
	if len(shared) == 0 {
		t.Fatalf("no production file in %s switches on an engine value kind. The single realisation "+
			"of the mapping lives there and cannot map anything without doing so, and every check "+
			"below would now pass vacuously", graphJSONPkgDir)
	}

	// The declared exceptions, asserted in both directions before they are used
	// to excuse anything.
	owners := make(map[string]map[string]bool, len(kindsOwnedElsewhere))
	for kind, dir := range kindsOwnedElsewhere {
		if !kinds[kind] {
			t.Errorf("%q is declared as owned by %s, but GoGraph %s declares no such kind. An "+
				"exception naming a kind that does not exist excuses nothing and hides the fact that "+
				"the kind was renamed", kind, dir, goGraphVersion(t))
			continue
		}
		if shared[kind] {
			t.Errorf("%q is declared as owned by %s, but %s carries a row for it too. Two homes for "+
				"one kind is the duplication this file exists against; delete whichever row is not "+
				"the published one, and correct this entry", kind, dir, graphJSONPkgDir)
			continue
		}
		if owners[kind] == nil {
			owners[kind] = kindsSwitchedOn(t, filepath.Join(repoRoot(t), filepath.FromSlash(dir)))
		}
		if !owners[kind][kind] {
			t.Errorf("%q is declared as owned by %s, but no production file there switches on it. An "+
				"owner that does not carry the kind leaves it published by the shared realisation's "+
				"fallback — the engine's String form — which no specification fixes", kind, dir)
		}
	}

	homeless := make([]string, 0, len(kinds))
	for kind := range kinds {
		if shared[kind] {
			continue
		}
		if _, declared := kindsOwnedElsewhere[kind]; declared {
			continue
		}
		homeless = append(homeless, kind)
	}
	sort.Strings(homeless)
	for _, kind := range homeless {
		t.Errorf("GoGraph %s declares expr.%s and nothing maps it.\n"+
			"  A value of that kind reaches the shared realisation's fallback and is published as the "+
			"engine's own String form, on all three surfaces at once, in a shape SPEC/DATA_FORMATS.md "+
			"does not describe.\n"+
			"  Add a row for it in %s, or — if only one surface can publish it, as is true of a path — "+
			"give it an owner there and declare that here.",
			goGraphVersion(t), kind, graphJSONPkgDir)
	}
}

// engineValueKinds returns every exported value-kind constant the pinned engine
// declares, read from the upstream source in the module cache rather than from a
// list kept here.
func engineValueKinds(t *testing.T) map[string]bool {
	t.Helper()

	dir := filepath.Join(goGraphModuleDir(t), "cypher", "expr")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the GoGraph value model at %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	kinds := make(map[string]bool, 16)
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
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, isValue := spec.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				for _, ident := range value.Names {
					if ident.IsExported() && strings.HasPrefix(ident.Name, exprKindPrefix) {
						kinds[ident.Name] = true
					}
				}
			}
		}
	}
	return kinds
}

// kindsSwitchedOn returns every engine value kind that appears as a case
// expression in a production file of dir.
//
// A case expression is the shape a mapping takes, and reading it rather than any
// mention of the identifier is what keeps a doc comment or an error message from
// standing in for a row that does not exist.
func kindsSwitchedOn(t *testing.T, dir string) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	kinds := make(map[string]bool, 16)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", path, parseErr)
		}
		local := importLocalName(file, exprImportPath)
		if local == "" {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expression := range clause.List {
				if kind, matched := engineKindSelector(expression, local); matched {
					kinds[kind] = true
				}
			}
			return true
		})
	}
	return kinds
}

// engineKindSelector reports whether expression is <local>.Kind*, and the kind's
// name when it is.
func engineKindSelector(expression ast.Expr, local string) (string, bool) {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok || qualifier.Name != local {
		return "", false
	}
	if !strings.HasPrefix(selector.Sel.Name, exprKindPrefix) {
		return "", false
	}
	return selector.Sel.Name, true
}

// scanPublishedRenderings walks the repository's production Go files and returns
// every call that formats a value through one of the published temporal
// renderings.
func scanPublishedRenderings(t *testing.T, root string) []*renderingSite {
	t.Helper()

	var sites []*renderingSite
	fset := token.NewFileSet()
	walkProductionFiles(t, root, func(rel, dir string, file *ast.File) {
		timeName, imported := timeImportName(file)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall || len(call.Args) != 1 {
					return true
				}
				selector, isSelector := call.Fun.(*ast.SelectorExpr)
				if !isSelector || selector.Sel.Name != "Format" {
					return true
				}
				rendering, matched := layoutOf(call.Args[0], timeName, imported)
				if !matched {
					return true
				}
				if _, published := publishedRenderings[rendering]; !published {
					return true
				}
				sites = append(sites, &renderingSite{
					file:      rel,
					dir:       dir,
					function:  fn.Name.Name,
					rendering: rendering,
					line:      fset.Position(call.Pos()).Line,
				})
				return true
			})
		}
	}, fset)

	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].line < sites[j].line
	})
	return sites
}

// layoutOf renders a Format call's layout argument in the form
// publishedRenderings is keyed by: a normalised string literal, or the dotted
// name of a time-package constant reached through the file's own import name.
func layoutOf(argument ast.Expr, timeName string, timeImported bool) (string, bool) {
	switch a := argument.(type) {
	case *ast.BasicLit:
		if a.Kind != token.STRING {
			return "", false
		}
		unquoted, err := strconv.Unquote(a.Value)
		if err != nil {
			return "", false
		}
		return strconv.Quote(unquoted), true
	case *ast.SelectorExpr:
		if !timeImported {
			return "", false
		}
		qualifier, ok := a.X.(*ast.Ident)
		if !ok || qualifier.Name != timeName {
			return "", false
		}
		return "time." + a.Sel.Name, true
	default:
		return "", false
	}
}

// scanElementObjects walks the repository's production Go files and returns every
// site that builds an object carrying one of the element key sets, keyed by that
// set.
//
// Both routes to a JSON object are read: a map literal whose keys are string
// literals, and a struct whose fields carry `json:"..."` tags. The second is not
// hypothetical tidiness — it is the obvious way to rewrite the first, and a rule
// that saw only map literals would wave the rewrite through.
func scanElementObjects(t *testing.T, root string) map[string][]*elementSite {
	t.Helper()

	sites := make(map[string][]*elementSite, len(elementKeySets))
	record := func(rel, dir string, line int, keys map[string]bool) {
		for _, want := range elementKeySets {
			all := true
			for _, key := range want.keys {
				if !keys[key] {
					all = false
					break
				}
			}
			if !all {
				continue
			}
			key := strings.Join(want.keys, "+")
			sites[key] = append(sites[key], &elementSite{
				file: rel,
				dir:  dir,
				keys: key,
				line: line,
			})
		}
	}

	fset := token.NewFileSet()
	walkProductionFiles(t, root, func(rel, dir string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CompositeLit:
				keys := make(map[string]bool, len(node.Elts))
				for _, element := range node.Elts {
					pair, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					literal, ok := pair.Key.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					if unquoted, err := strconv.Unquote(literal.Value); err == nil {
						keys[unquoted] = true
					}
				}
				record(rel, dir, fset.Position(node.Pos()).Line, keys)
			case *ast.StructType:
				if node.Fields == nil {
					return true
				}
				keys := make(map[string]bool, len(node.Fields.List))
				for _, field := range node.Fields.List {
					if field.Tag == nil {
						continue
					}
					tag, err := strconv.Unquote(field.Tag.Value)
					if err != nil {
						continue
					}
					name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
					if name != "" {
						keys[name] = true
					}
				}
				record(rel, dir, fset.Position(node.Pos()).Line, keys)
			}
			return true
		})
	}, fset)

	for key := range sites {
		found := sites[key]
		sort.Slice(found, func(i, j int) bool {
			if found[i].file != found[j].file {
				return found[i].file < found[j].file
			}
			return found[i].line < found[j].line
		})
	}
	return sites
}
