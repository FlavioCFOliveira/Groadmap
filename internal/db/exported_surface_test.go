package db

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

// This file is the gate for one class of defect: an exported identifier in
// internal/db that no production code reaches.
//
// The class is not hypothetical. Sprint deletion used to have two
// implementations — the transaction the binary runs, in internal/commands, and
// an unreachable copy here — and only the reachable one was ever exercised, so
// the copy silently missed the finding-#49 fix that clears a reverted task's
// lifecycle timestamps. Anyone fixing a bug in sprint deletion had an even
// chance of fixing the copy that does not run. The audit log had the same
// shape: two convenience writers whose only callers were test fixtures, next to
// the transactional writer every command uses.
//
// A dead exported identifier here is worse than a missing one: it looks like an
// API, it compiles, its tests pass, and it is wrong in ways nothing reports.
// So the rule is that internal/db exports only what the binary reaches — and
// every exception is listed below, in writing, with its reason.
//
// The check is deliberately name-based and therefore conservative in the safe
// direction: an identifier is considered reached if its name appears in any
// non-test file anywhere in the module, in a position that is not a
// declaration. A same-named method on an unrelated type would let a genuinely
// dead export pass, but nothing that IS reached can ever be reported as dead,
// so the gate does not cry wolf. Comments do not count, because they are not
// identifiers in the syntax tree — which matters, since a doc comment
// necessarily repeats the name it documents.

// unreachedExports are the exported identifiers of this package that no
// production code reaches, and that are still here on purpose. Each entry
// carries the reason it survives. The gate fails both ways: an unlisted dead
// export is a failure, and so is a listed one that has since gained a caller —
// the list may only shrink, and it may never rot.
//
// It is empty, and that is the intended state. It held fifteen entries when the
// gate landed, one generation of the same defect: db-layer CRUD methods the
// command layer had replaced with its own transactions, plus four helpers
// nothing had ever called. Task #188 settled every one of them — eleven write
// methods and three helpers deleted, the task and sprint INSERT promoted to the
// transaction-scoped InsertTaskTx / InsertSprintTx that `task create` and
// `sprint create` now run, the eight query-cache templates they were the last
// users of removed with them, and the fixtures of this package, internal/web
// and internal/commands rewritten onto the path the binary takes.
//
// An entry added here from now on is a decision, not an inheritance: it says
// this package exports something the binary cannot reach and someone chose to
// keep it. The reason has to say why deleting it is not the answer.
var unreachedExports = map[string]string{}

// TestEveryExportedIdentifierIsReachedFromProduction is the gate itself.
func TestEveryExportedIdentifierIsReachedFromProduction(t *testing.T) {
	root := moduleRoot(t)
	used := identifiersUsedInProduction(t, root)
	exported := exportedDeclarations(t, filepath.Join(root, "internal", "db"))

	if len(exported) == 0 {
		t.Fatal("no exported declarations found; the sweep is not looking where it thinks it is")
	}

	var unlisted []string
	for _, name := range exported {
		if used[name] {
			if reason, listed := unreachedExports[name]; listed {
				t.Errorf("%s is listed as unreached but production code now uses it.\n"+
					"Delete its entry from unreachedExports; the listed reason was: %s", name, reason)
			}
			continue
		}
		if _, listed := unreachedExports[name]; !listed {
			unlisted = append(unlisted, name)
		}
	}

	sort.Strings(unlisted)
	for _, name := range unlisted {
		t.Errorf("%s is exported from internal/db but no non-test file in the module names it.\n"+
			"Either wire it up, delete it, or add it to unreachedExports with the reason it stays.\n"+
			"An unreachable exported helper here is a second implementation waiting to drift from the one that ships.", name)
	}

	// The list may not name identifiers that no longer exist either: a stale
	// entry would silently excuse a future export that reused the name.
	declared := make(map[string]bool, len(exported))
	for _, name := range exported {
		declared[name] = true
	}
	for name := range unreachedExports {
		if !declared[name] {
			t.Errorf("unreachedExports names %q, which internal/db no longer declares; remove the entry", name)
		}
	}
}

// moduleRoot returns the repository root, two levels above this package.
func moduleRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s; this test assumes internal/db sits two levels below the module root: %v", root, err)
	}
	return root
}

// productionGoFiles lists every non-test .go file in the module. Build
// constraints are ignored on purpose: a file compiled only on another GOOS
// still counts as production code, and a reference from it still counts.
func productionGoFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", ".claude", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return files
}

// identifiersUsedInProduction collects every identifier that appears in a
// non-declaration position in any production file. Declaration names are
// excluded so that declaring a function does not count as using it; struct and
// interface field names are NOT excluded, because an interface method name is
// how a *DB satisfies an interface, and that is a genuine use.
func identifiersUsedInProduction(t *testing.T, root string) map[string]bool {
	t.Helper()

	used := make(map[string]bool, 4096)
	fset := token.NewFileSet()

	for _, path := range productionGoFiles(t, root) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		declared := declarationNames(file)
		ast.Inspect(file, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			if declared[ident] {
				return true
			}
			used[ident.Name] = true
			return true
		})
	}
	return used
}

// declarationNames returns the identifier nodes that name a top-level
// declaration in this file, which are the ones a use may not be confused with.
func declarationNames(file *ast.File) map[*ast.Ident]bool {
	names := make(map[*ast.Ident]bool)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			names[d.Name] = true
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					names[s.Name] = true
				case *ast.ValueSpec:
					for _, name := range s.Names {
						names[name] = true
					}
				}
			}
		}
	}
	return names
}

// exportedDeclarations returns the names of every exported top-level
// declaration in the package directory: functions, methods, types, constants
// and variables.
func exportedDeclarations(t *testing.T, dir string) []string {
	t.Helper()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	var names []string
	add := func(ident *ast.Ident) {
		if ident.IsExported() {
			names = append(names, ident.Name)
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				add(d.Name)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						add(s.Name)
					case *ast.ValueSpec:
						for _, ident := range s.Names {
							add(ident)
						}
					}
				}
			}
		}
	}

	sort.Strings(names)
	return names
}
