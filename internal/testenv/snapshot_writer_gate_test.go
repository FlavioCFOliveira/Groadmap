package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file is the regression fence for the one defect a duplicated checkpoint
// can produce in silence: a snapshot written WITHOUT the graph's registered
// schema.
//
// Why it exists. SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 2, calls
// the schema clause of the self-sufficiency requirement "load-bearing" and spells
// out the consequence: an index or constraint definition lives in the write-ahead
// log until a snapshot carries it, so a snapshot that omits it, followed by the
// truncation in step 3, DESTROYS it. The next invocation opens a graph whose
// schema is empty, SHOW INDEXES reports nothing, a DROP fails as though the
// object had never existed, and — worst of the three — a UNIQUE constraint stops
// being enforced while the data it was declared to protect is still there. One
// successful write is enough, because every successful write checkpoints. That
// is a real defect this project has already shipped and fixed once (release
// 1.15.2, "the checkpoint erasing schema").
//
// Why a GATE and not a test beside each call site. There are now TWO checkpoint
// implementations in the module — internal/commands (the CLI) and internal/web
// (the graph data endpoint, since rmp task #364) — and they cannot share one
// today, because internal/commands imports internal/web and the dependency
// cannot run the other way. Two copies of a durability sequence drift; that is
// the hazard internal/backoff's singleton gate and the engine-constructor gate
// were each written against, in their own guise. A behavioural test beside each
// site proves that site correct on the day it is written and says nothing about
// the next site somebody adds.
//
// What it checks: every call in production source to a snapshot-writing function
// of GoGraph's store/snapshot package is the schema-carrying variant. The engine
// also exposes WriteSnapshotFullWithMapperCodec, which persists no schema at all,
// and the plainer WriteSnapshotFull; either of them, followed by the truncation
// that always follows, is the defect above.
//
// Known limits, stated rather than papered over:
//
//   - The sweep is syntactic. It matches calls on the local name of the
//     store/snapshot import, so an aliased import is followed, but a writer
//     reached through a function value or through reflection is not seen.
//     Nothing in this project writes one that way.
//   - It sees production files only. A test may write a schemaless snapshot
//     deliberately — internal/commands/graph_schema_durability_test.go does
//     exactly that, to prove what the defect looks like — and holding test files
//     to this rule would delete the proof.

const snapshotImportPath = goGraphModulePath + "/store/snapshot"

// snapshotWriterPrefix is what makes a snapshot package function a snapshot
// writer. The gate asserts below that the pinned engine exposes at least one
// name under it, so a rename upstream fails loudly instead of emptying the sweep.
const snapshotWriterPrefix = "WriteSnapshot"

// schemaCarryingSnapshotWriter is the only writer a checkpoint may call: it takes
// the constraint and index specification slices, which is what makes the snapshot
// self-sufficient for the schema as well as for the graph.
const schemaCarryingSnapshotWriter = "WriteSnapshotFullWithMapperCodecConstraintsAndIndexDefs"

// mustWriteSnapshotIn are the directories that MUST hold at least one snapshot
// write. Without this anchor the sweep could stop matching — a renamed import, a
// changed AST shape — and the gate would report success over an empty set, which
// is how a guard quietly becomes decoration.
var mustWriteSnapshotIn = []string{"internal/commands", "internal/web"}

// snapshotWriteSite is one call to a snapshot-writing function in production
// source.
type snapshotWriteSite struct {
	file     string // repository-relative
	dir      string // repository-relative directory
	function string // enclosing top-level function
	writer   string // the snapshot package function called, unqualified
	line     int
}

// TestEveryCheckpointWritesTheSchema asserts that every snapshot a production
// file writes is the schema-carrying one.
func TestEveryCheckpointWritesTheSchema(t *testing.T) {
	if !snapshotWriterExists(t, schemaCarryingSnapshotWriter) {
		t.Fatalf("GoGraph %s does not expose snapshot.%s. Either it was renamed upstream and this "+
			"gate outlived it, or the name is misspelt — and a misspelt name here is a rule that can "+
			"never be violated", goGraphVersion(t), schemaCarryingSnapshotWriter)
	}

	sites := scanSnapshotWrites(t, repoRoot(t))
	if len(sites) == 0 {
		t.Fatal("the sweep found no snapshot write anywhere in the production source. The " +
			"implementation certainly writes one, so the sweep has stopped matching and this gate " +
			"would now pass whatever the code did")
	}
	for _, dir := range mustWriteSnapshotIn {
		found := false
		for _, site := range sites {
			if site.dir == dir {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no snapshot write was found in %s, which runs the synchronous checkpoint of "+
				"SPEC/GRAPH.md § Synchronous Checkpoint on Write. Either the checkpoint moved — in "+
				"which case this list needs amending — or the sweep no longer recognises it", dir)
		}
	}

	for _, site := range sites {
		if site.writer == schemaCarryingSnapshotWriter {
			continue
		}
		t.Errorf("%s:%d (%s) writes a snapshot through snapshot.%s.\n"+
			"  SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 2, requires the snapshot to "+
			"carry the registered schema, and the truncation in step 3 destroys anything the "+
			"snapshot left behind: the next invocation opens a graph whose indexes and constraints "+
			"are gone, and a UNIQUE constraint stops being enforced over data that still exists.\n"+
			"  Call snapshot.%s, passing the constraint and index specifications read from the "+
			"engine that ran the statement.",
			site.file, site.line, site.function, site.writer, schemaCarryingSnapshotWriter)
	}
}

// snapshotWriterExists reports whether the pinned GoGraph release exposes name as
// an exported function of its store/snapshot package, read from the upstream
// source in the module cache rather than from a list kept here.
//
// It is what stops the rule below from becoming unfalsifiable: a constant naming
// a function that no longer exists is a rule nothing can violate.
func snapshotWriterExists(t *testing.T, name string) bool {
	t.Helper()

	dir := filepath.Join(goGraphModuleDir(t), "store", "snapshot")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the GoGraph snapshot package at %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	found, writers := false, 0
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".go") || strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, fileName), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", fileName, parseErr)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, snapshotWriterPrefix) {
				continue
			}
			writers++
			if fn.Name.Name == name {
				found = true
			}
		}
	}
	if writers == 0 {
		t.Fatalf("no function named %s* was found in the GoGraph snapshot package at %s; the "+
			"upstream package moved or changed shape, and the sweep below would then find nothing "+
			"to check", snapshotWriterPrefix, dir)
	}
	return found
}

// goGraphModuleDir locates the pinned GoGraph module in the cache. `go list -m`
// is asked rather than the cache path being reconstructed, so the answer is the
// one the build itself uses.
func goGraphModuleDir(t *testing.T) string {
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
	return dir
}

// scanSnapshotWrites walks the repository's production Go files and returns every
// call to a snapshot-writing function of GoGraph's store/snapshot package.
func scanSnapshotWrites(t *testing.T, root string) []*snapshotWriteSite {
	t.Helper()

	fset := token.NewFileSet()
	var sites []*snapshotWriteSite

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
		rel = filepath.ToSlash(rel)

		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", rel, parseErr)
		}
		local := importLocalName(file, snapshotImportPath)
		if local == "" {
			return nil
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall {
					return true
				}
				sel, isSel := call.Fun.(*ast.SelectorExpr)
				if !isSel {
					return true
				}
				pkg, isIdent := sel.X.(*ast.Ident)
				if !isIdent || pkg.Name != local {
					return true
				}
				if !strings.HasPrefix(sel.Sel.Name, snapshotWriterPrefix) {
					return true
				}
				sites = append(sites, &snapshotWriteSite{
					file:     rel,
					dir:      filepath.ToSlash(filepath.Dir(rel)),
					function: fn.Name.Name,
					writer:   sel.Sel.Name,
					line:     fset.Position(call.Pos()).Line,
				})
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}

	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].line < sites[j].line
	})
	return sites
}
