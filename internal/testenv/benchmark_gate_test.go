package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file is the module-wide gate for SPEC/BUILD.md § No Benchmarks and No
// Performance-Measurement Tests.
//
// The rule: this project keeps no benchmarks. Rule 1 of that section forbids a
// `Benchmark` function "whether or not any gate runs it", and gives the reason —
// the gate set runs `go test ./...`, which does not execute benchmarks, so an
// unrun benchmark is dead weight that no gate could ever defend. A benchmark is
// therefore not a test that happens to be slow: it is code that no pipeline
// compiles into a verdict, and it decays silently.
//
// Why a gate at all, when the specification says the rule "is not a gate".
// That paragraph is about the GATE SET — the six targets `make check` runs and
// the three pipelines that run them. It says the rule adds no target to that
// table and changes no command any pipeline runs, and this file adds neither: it
// is an ordinary test, in a package the existing `test` target already covers,
// exactly like the module-wide AST gates beside it. What it removes is the need
// for the rule to be "enforced by review", which is the weakest enforcement this
// project has anywhere, on a rule whose violations are by construction invisible
// to every other check: a `Benchmark` function compiles, vets clean, lints clean
// and is never run.
//
// Why here. The sweep spans every package in the module, so no audited package is
// its home, and internal/testenv already hosts the module-wide AST gates
// (hermetic_gate_test.go, backoff_singleton_gate_test.go,
// engine_constructor_gate_test.go) whose repository walk, skip list and
// formatting helpers this file reuses rather than duplicating.
//
// What it detects, and why it is two checks rather than one:
//
//   - a function whose name begins with Benchmark and which takes a *testing.B.
//     That is the signature `go test` dispatches on, so it is what a benchmark
//     IS;
//   - any *testing.B in any signature at all — a helper, a table driver, an
//     interface method, a struct field's function type. A benchmark deleted down
//     to its helpers leaves exactly this behind, and a helper that takes a
//     *testing.B exists for nothing else.
//
// Neither check alone closes the class. The first misses the stranded helper the
// second catches; the second misses a `func BenchmarkX(t *testing.T)`, which is
// not dispatched as a benchmark but is a benchmark by intent and by name, and
// which the first catches.
//
// Known limits, stated rather than papered over:
//
//   - the sweep is syntactic. It follows an aliased import of the testing
//     package, and it matches `testing.B` by the file's own local name for that
//     package, but a type reached through a type alias declared elsewhere is not
//     seen. Nothing in this module aliases it.
//   - it reads _test.go files and production files alike. A `Benchmark` function
//     cannot live in a production file and still be dispatched, but the rule is
//     about what the repository CONTAINS, and a production file importing
//     testing is a defect of its own.
//   - it says nothing about a test that measures a duration without being a
//     benchmark. Rule 2 of the same section forbids those too, and no static
//     check distinguishes a clock read that asserts from one that logs. What
//     closes rule 2 is the observable each converted test now asserts instead.

// TestNoBenchmarkExistsAnywhereInTheModule asserts that no package in the module
// declares a `Benchmark*` function or mentions `*testing.B` in any signature.
//
// A failure is not a judgement call to be exempted, unlike the delay gate beside
// it: SPEC/BUILD.md § No Benchmarks and No Performance-Measurement Tests admits
// no benchmark under any condition, so this gate carries no exemption list and
// the remedy for a failure is to delete the code it names.
func TestNoBenchmarkExistsAnywhereInTheModule(t *testing.T) {
	root := repoRoot(t)

	findings, scanned := scanForBenchmarks(t, root, nil)

	// Non-vacuity: the walk must have parsed something. Without this the gate
	// passes the day the skip list, the root or the suffix filter breaks, which
	// is the most consequential drift of all.
	if scanned == 0 {
		t.Fatalf("the sweep parsed no Go file under %s; the gate below would pass whatever the "+
			"module contained", root)
	}

	if len(findings) == 0 {
		return
	}
	sort.Strings(findings)
	t.Errorf("the module declares %d benchmark artefact(s), and it may declare none:\n%s\n"+
		"    SPEC/BUILD.md § No Benchmarks and No Performance-Measurement Tests forbids a\n"+
		"    Benchmark function in every package and every pipeline, whether or not any gate\n"+
		"    runs it: the gate set runs `go test ./...`, which does not execute benchmarks, so\n"+
		"    an unrun benchmark is dead weight no gate can defend. A helper that takes a\n"+
		"    *testing.B is the residue of one and exists for nothing else. Delete them.",
		len(findings), indentEvidence(findings))
}

// TestNoBenchmarkGateDetectsABenchmark is the gate's own proof, and it is what
// makes the check above a gate rather than a statement.
//
// A sweep that matched nothing — a wrong selector, a filter that excluded every
// file, a walk that returned early — would pass silently on a module full of
// benchmarks. So the same scan is run over a directory containing one benchmark
// of each shape the rule forbids, and each shape is required to be reported.
//
// The fixture is written to a temporary directory rather than kept in the
// repository, because a fixture kept in the repository would be a benchmark in
// the repository, which is the thing forbidden.
func TestNoBenchmarkGateDetectsABenchmark(t *testing.T) {
	fixture := t.TempDir()

	files := map[string]string{
		// A benchmark exactly as `go test` dispatches it.
		"dispatched_test.go": `package fixture

import "testing"

func BenchmarkTaskMatches(b *testing.B) {
	for range b.N {
		_ = 1
	}
}
`,
		// A stranded helper: the benchmark that called it is gone, the parameter
		// is not.
		"helper_test.go": `package fixture

import "testing"

func seedForBenchmark(b *testing.B, n int) []int {
	b.Helper()
	return make([]int, n)
}
`,
		// A benchmark by name and intent that takes a *testing.T, so the
		// signature check alone would miss it.
		"misnamed_test.go": `package fixture

import "testing"

func BenchmarkRenderByName(t *testing.T) {
	_ = t
}
`,
		// An aliased import, to prove the match follows the file's own local name
		// for the testing package rather than the literal "testing".
		"aliased_test.go": `package fixture

import gotesting "testing"

func BenchmarkAliased(b *gotesting.B) {
	_ = b
}
`,
		// A file that must NOT be reported, so the gate is shown to discriminate
		// rather than to flag everything it parses.
		"innocent_test.go": `package fixture

import "testing"

func TestBenchmarkIsMentionedInAComment(t *testing.T) {
	// The word Benchmark appears here and in this string, and neither is a
	// benchmark.
	if s := "BenchmarkNothing"; s == "" {
		t.Fatal(s)
	}
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(fixture, name), []byte(body), 0600); err != nil {
			t.Fatalf("writing the fixture %s: %v", name, err)
		}
	}

	findings, scanned := scanForBenchmarks(t, fixture, nil)
	if scanned != len(files) {
		t.Fatalf("the sweep parsed %d of the %d fixture files", scanned, len(files))
	}

	for _, want := range []string{
		"dispatched_test.go",
		"helper_test.go",
		"misnamed_test.go",
		"aliased_test.go",
	} {
		if !containsSubstring(findings, want) {
			t.Errorf("the sweep did not report %s. That shape of benchmark would reach the "+
				"repository with the gate green, so the gate is not closed over it. Reported:\n%s",
				want, indentEvidence(findings))
		}
	}
	if containsSubstring(findings, "innocent_test.go") {
		t.Errorf("the sweep reported innocent_test.go, which declares no benchmark and only "+
			"mentions the word. A gate that flags every mention would be turned off rather than "+
			"obeyed. Reported:\n%s", indentEvidence(findings))
	}
}

// scanForBenchmarks walks root and returns one evidence line per benchmark
// artefact found, together with the number of Go files it parsed.
//
// skip, when non-nil, is consulted for directory names in addition to the shared
// skip list, so a caller can exclude a directory of its own.
func scanForBenchmarks(t *testing.T, root string, skip map[string]bool) (findings []string, scanned int) {
	t.Helper()

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (skipDirs[entry.Name()] || skip[entry.Name()]) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", rel, parseErr)
			return nil
		}
		scanned++

		testingName, imported := testingImportName(file)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "Benchmark") {
				findings = append(findings, rel+":"+itoa(fset.Position(fn.Pos()).Line)+
					": func "+fn.Name.Name)
			}
		}

		if !imported {
			return nil
		}
		ast.Inspect(file, func(node ast.Node) bool {
			field, ok := node.(*ast.Field)
			if !ok {
				return true
			}
			star, ok := field.Type.(*ast.StarExpr)
			if !ok {
				return true
			}
			selector, ok := star.X.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "B" {
				return true
			}
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok || qualifier.Name != testingName {
				return true
			}
			findings = append(findings, rel+":"+itoa(fset.Position(field.Pos()).Line)+
				": *"+testingName+".B in a signature")
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return findings, scanned
}

// testingImportName returns the local name the file uses for the standard
// library's testing package, following an alias, and whether the file imports it
// at all.
func testingImportName(file *ast.File) (string, bool) {
	for _, spec := range file.Imports {
		if spec.Path == nil || spec.Path.Value != `"testing"` {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				// A blank import cannot name a type, and a dot import would put
				// B in scope unqualified; neither appears in this module, and
				// neither is matched rather than guessed at.
				return "", false
			}
			return spec.Name.Name, true
		}
		return "testing", true
	}
	return "", false
}

// containsSubstring reports whether any line contains want.
func containsSubstring(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}
