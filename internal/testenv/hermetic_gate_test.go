package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file is the class-closing gate for task #187.
//
// The defect it prevents is not "these particular tests wrote to the real
// ~/.roadmaps" but "a package whose tests touch the roadmap home has no
// hermetic TestMain". Fixing only the instances would let the class return the
// next time someone adds a package, or deletes a TestMain, or converts a test
// helper to reach the home directory a new way. TestPackagesTouchingTheHomeAreHermetic
// fails in all three cases.

// dbOpeners are the internal/db constructors that resolve
// ~/.roadmaps/<name>/project.db and create it on demand.
var dbOpeners = map[string]bool{
	"Open":         true,
	"OpenExisting": true,
	"OpenReadOnly": true,
}

// pathFuncs are the internal/utils entry points that resolve, create or
// enumerate paths under ~/.roadmaps.
var pathFuncs = map[string]bool{
	"GetDataDir":       true,
	"GetRoadmapDir":    true,
	"GetRoadmapPath":   true,
	"EnsureDataDir":    true,
	"EnsureRoadmapDir": true,
	"ListRoadmaps":     true,
	"RoadmapExists":    true,
}

// skipDirs are directories that hold no first-party Go packages of this module.
var skipDirs = map[string]bool{
	".git":         true,
	".claude":      true,
	"bin":          true,
	"coverage":     true,
	"node_modules": true,
}

// testPackage is what the scan records about one directory's test files.
type testPackage struct {
	dir              string
	homeTouching     []string // "file:line: expression" evidence, sorted
	hasTestMain      bool
	usesHermeticHome bool
}

// TestPackagesTouchingTheHomeAreHermetic asserts that every package whose test
// files call a roadmap-home API declares a TestMain wired to
// testenv.HermeticHome.
//
// The scan is deliberately syntactic rather than textual: it inspects call
// expressions in the AST, so the identifier lists above — which appear in this
// file only as string literals — cannot make the gate flag itself, and a
// mention of Open in a comment cannot either. It distinguishes db.Open from
// sql.Open by the selector's qualifier, and recognises the unqualified calls
// that internal/db and internal/utils make to their own functions.
//
// Known limit, stated rather than papered over: the gate detects a package that
// reaches the home directory through these APIs directly. A package that opened
// a roadmap only indirectly — by invoking a command handler that opens one, say
// — would not be flagged. Closing that hole would mean flagging every package
// that imports internal/utils, which is nearly all of them, and the resulting
// noise would make the gate worthless. The empirical check that catches the
// remainder is the one in the task's acceptance criteria: run the suite under a
// scratch HOME and assert nothing appears in it.
func TestPackagesTouchingTheHomeAreHermetic(t *testing.T) {
	root := repoRoot(t)
	packages := scanTestPackages(t, root)

	if len(packages) == 0 {
		t.Fatal("the scan found no test packages at all; the gate is not " +
			"looking where it thinks it is, and would pass no matter what the tests did")
	}

	// The scan must see the packages this task fixed. Without this the gate
	// could silently degrade to inspecting nothing of consequence and still
	// report success.
	mustBeHomeTouching := []string{"internal/db", "internal/commands", "internal/utils", "internal/web"}
	for _, want := range mustBeHomeTouching {
		pkg, ok := packages[want]
		if !ok {
			t.Errorf("package %s was not scanned; it holds tests that open roadmaps, "+
				"so either it moved or the scan is broken", want)
			continue
		}
		if len(pkg.homeTouching) == 0 {
			t.Errorf("package %s was scanned but no roadmap-home call was detected in it; "+
				"the detection rules have drifted away from the code they are meant to watch", want)
		}
	}

	for _, dir := range sortedKeys(packages) {
		pkg := packages[dir]
		if len(pkg.homeTouching) == 0 {
			continue
		}
		if !pkg.hasTestMain || !pkg.usesHermeticHome {
			t.Errorf("package %s reaches the roadmap home directory in its tests but is not hermetic:\n"+
				"  TestMain declared:              %t\n"+
				"  wired to testenv.HermeticHome:  %t\n"+
				"  first calls that reach the home directory:\n%s\n"+
				"  Without a hermetic TestMain, `go test ./...` writes roadmaps into the real\n"+
				"  ~/.roadmaps of whoever runs it. Add a main_test.go declaring:\n"+
				"      func TestMain(m *testing.M) { os.Exit(runTests(m)) }\n"+
				"      func runTests(m *testing.M) int {\n"+
				"          restore := testenv.HermeticHome()\n"+
				"          defer restore()\n"+
				"          return m.Run()\n"+
				"      }",
				dir, pkg.hasTestMain, pkg.usesHermeticHome, indentEvidence(pkg.homeTouching))
		}
	}
}

// repoRoot returns the module root. `go test` runs a package's tests with the
// working directory set to that package's directory, so the root is two levels
// up from internal/testenv. The go.mod check makes a wrong answer loud instead
// of turning the gate into a no-op.
func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s, so the repository root is not where this gate assumes: %v", root, err)
	}
	return root
}

// scanTestPackages parses every _test.go file in the module and returns one
// entry per directory that contains at least one, keyed by slash-separated path
// relative to the module root.
func scanTestPackages(t *testing.T, root string) map[string]*testPackage {
	t.Helper()

	packages := make(map[string]*testPackage)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(rel)

		pkg := packages[dir]
		if pkg == nil {
			pkg = &testPackage{dir: dir}
			packages[dir] = pkg
		}
		inspectTestFile(t, fset, path, root, pkg)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
	return packages
}

// inspectTestFile folds one test file's findings into its package's record.
func inspectTestFile(t *testing.T, fset *token.FileSet, path, root string, pkg *testPackage) {
	t.Helper()

	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncDecl:
			if n.Recv == nil && n.Name.Name == "TestMain" {
				pkg.hasTestMain = true
			}
		case *ast.SelectorExpr:
			// testenv.HermeticHome is what wires a TestMain to the shared
			// mechanism. It is matched wherever it appears in the package's
			// test files, since TestMain is free to delegate.
			if ident, ok := n.X.(*ast.Ident); ok &&
				ident.Name == "testenv" && n.Sel.Name == "HermeticHome" {
				pkg.usesHermeticHome = true
			}
		case *ast.CallExpr:
			if name, ok := homeTouchingCall(n); ok {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				position := fset.Position(n.Pos())
				pkg.homeTouching = append(pkg.homeTouching,
					filepath.ToSlash(rel)+":"+itoa(position.Line)+": "+name)
			}
		}
		return true
	})
}

// homeTouchingCall reports whether a call resolves a path under ~/.roadmaps,
// and returns the expression as written for use in the failure message.
func homeTouchingCall(call *ast.CallExpr) (string, bool) {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		// A qualified call: only db.Open* and utils.<path func> count. The
		// qualifier is what keeps sql.Open, which opens no roadmap, out.
		qualifier, ok := fun.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		name := qualifier.Name + "." + fun.Sel.Name
		if qualifier.Name == "db" && dbOpeners[fun.Sel.Name] {
			return name, true
		}
		if qualifier.Name == "utils" && pathFuncs[fun.Sel.Name] {
			return name, true
		}
		return "", false
	case *ast.Ident:
		// An unqualified call: internal/db and internal/utils call their own
		// functions this way. Another package declaring a function with one of
		// these names would be flagged too; that bias is deliberate, since the
		// cost is one TestMain and the cost of the opposite bias is a polluted
		// home directory.
		if dbOpeners[fun.Name] || pathFuncs[fun.Name] {
			return fun.Name, true
		}
		return "", false
	default:
		return "", false
	}
}

// indentEvidence renders up to five call sites, so a failure names concrete
// lines instead of only a package.
func indentEvidence(evidence []string) string {
	const max = 5

	shown := evidence
	if len(shown) > max {
		shown = shown[:max]
	}

	var b strings.Builder
	for _, line := range shown {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(evidence) > max {
		b.WriteString("    ... and ")
		b.WriteString(itoa(len(evidence) - max))
		b.WriteString(" more\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// itoa avoids importing strconv for a handful of small line numbers.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	negative := n < 0
	if negative {
		n = -n
	}

	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

// sortedKeys returns the map's keys in a stable order so failures are
// reproducible rather than dependent on map iteration. It is generic in the
// value type because the gates in this package key several different maps by
// name and each one wants the same ordering guarantee.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
