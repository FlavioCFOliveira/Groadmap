package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file is the regression fence for the one defect a checkpoint can produce
// in silence: a snapshot written WITHOUT the graph's registered schema.
//
// Why it exists. SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 2, calls
// the schema clause of the self-sufficiency requirement "load-bearing" and spells
// out the consequence: an index or constraint definition lives in the write-ahead
// log until a snapshot carries it, so a snapshot that omits it, followed by the
// truncation in step 3, DESTROYS it. The next server opens a graph whose schema
// is empty, SHOW INDEXES reports nothing, a DROP fails as though the object had
// never existed, and — worst of the three — a UNIQUE constraint stops being
// enforced while the data it was declared to protect is still there. That is a
// real defect this project has already shipped and fixed once (release 1.15.2,
// "the checkpoint erasing schema").
//
// Why there is now exactly ONE route to the disk, and it is GoGraph's. The engine
// keeps the write-ahead log in segments whose records carry positions, and a
// snapshot is accepted beside such a log only when it records the position it
// covers. None of GoGraph's store/snapshot WriteSnapshot* writers records one, so
// a snapshot Groadmap wrote through them, followed by the truncation that always
// follows, would leave a store the engine refuses to open — on top of being a
// second checkpoint implementation beside the server's. The engine's own
// checkpointer (store/checkpoint) is what records the position, in the snapshot
// and in the log's control file, and it is the one route `rmp graph serve` folds
// through (SPEC/GRAPH.md § Synchronous Checkpoint on Write). So the gate asserts
// two things:
//
//   - TestEveryCheckpointWritesTheSchema: production source calls NO WriteSnapshot*
//     writer, and constructs the checkpointer in exactly one place, the dedicated
//     graph server — the same shape as internal/backoff's singleton gate;
//   - TestCheckpointerCarriesTheSchema: that construction carries the two options
//     that make the snapshot it writes carry the registered schema.
//
// Both verify against the upstream source that the names they match still exist,
// so a rename upstream fails loudly instead of leaving a rule nothing could
// violate.
//
// Known limits, stated rather than papered over:
//
//   - The sweeps are syntactic. They match calls on the local name of the
//     imported package, so an aliased import is followed, but a writer reached
//     through a function value or through reflection is not seen. Nothing in
//     this project writes one that way.
//   - They see production files only. A test may write a schemaless snapshot
//     deliberately, and holding test files to this rule would delete the proof.

const snapshotImportPath = goGraphModulePath + "/store/snapshot"

// snapshotWriterPrefix is what makes a snapshot package function a snapshot
// writer. The gate asserts below that the pinned engine exposes at least one
// name under it, so a rename upstream fails loudly instead of emptying the sweep.
const snapshotWriterPrefix = "WriteSnapshot"

// checkpointerDir is the ONE directory production source may construct the
// engine's checkpointer in: the dedicated graph server's, the only process that
// opens a store and therefore the only one that folds it.
const checkpointerDir = "internal/graphserve"

// The GoGraph checkpointer, and the two options that make the snapshot it writes
// carry the registered schema. A construction without them is the defect this
// file exists against, arriving through GoGraph rather than through our own
// snapshot call.
const (
	checkpointImportPath   = goGraphModulePath + "/store/checkpoint"
	checkpointConstructor  = "New"
	checkpointConstraintFn = "WithConstraintSpecs"
	checkpointIndexFn      = "WithIndexSpecs"
)

// snapshotWriteSite is one call to a snapshot-writing function in production
// source.
type snapshotWriteSite struct {
	file     string // repository-relative
	dir      string // repository-relative directory
	function string // enclosing top-level function
	writer   string // the snapshot package function called, unqualified
	line     int
}

// TestEveryCheckpointWritesTheSchema asserts that no production file writes a
// snapshot itself, and that the engine's checkpointer — the one route a snapshot
// takes to disk — is constructed exactly once, in the dedicated graph server.
func TestEveryCheckpointWritesTheSchema(t *testing.T) {
	if !snapshotWriterExists(t, "WriteSnapshotFullWithMapperCodecConstraintsAndIndexDefs") {
		t.Fatalf("GoGraph %s exposes no snapshot.WriteSnapshotFullWithMapperCodecConstraintsAndIndexDefs. "+
			"Either it was renamed upstream and this gate outlived it, or the name is misspelt — and a "+
			"misspelt name here is a rule that can never be violated", goGraphVersion(t))
	}

	for _, site := range scanSnapshotWrites(t, repoRoot(t)) {
		t.Errorf("%s:%d (%s) writes a snapshot through snapshot.%s.\n"+
			"  No WriteSnapshot* writer records the write-ahead-log position the snapshot covers, so the "+
			"engine refuses a store whose log was truncated after one; and a snapshot written here is a "+
			"second checkpoint implementation beside the server's. Fold through the engine's "+
			"checkpointer in %s instead (SPEC/GRAPH.md § Synchronous Checkpoint on Write).",
			site.file, site.line, site.function, site.writer, checkpointerDir)
	}

	sites := scanCheckpointerConstructions(t, repoRoot(t))
	if len(sites) != 1 || path.Dir(sites[0].file) != checkpointerDir {
		where := make([]string, 0, len(sites))
		for _, site := range sites {
			where = append(where, site.file+":"+itoa(site.line)+" ("+site.function+")")
		}
		t.Errorf("production source constructs the engine's checkpointer %d time(s), at %v. Exactly one "+
			"is expected, in %s: the dedicated graph server is the only process that opens a store, and "+
			"a second construction is a second checkpoint beside its gate. A sweep that finds none has "+
			"stopped matching, and this gate would then pass whatever the code did",
			len(sites), where, checkpointerDir)
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

// TestCheckpointerCarriesTheSchema fences the one route a snapshot reaches disk
// by: GoGraph's own checkpointer.
//
// The checkpointer writes the snapshot itself, so the snapshot-writer sweep in
// this file cannot see what it wrote, and it carries the registered schema only
// when it is given the two option functions that supply it. Built without them
// it writes a schemaless snapshot and discards the folded log after it — the
// defect this file exists against.
//
// Its non-vacuity anchor is upstream: both option names are verified to exist in
// the pinned GoGraph release, so a rename there fails this test loudly instead of
// leaving behind a rule that nothing could violate; and
// TestEveryCheckpointWritesTheSchema requires exactly one construction to check.
func TestCheckpointerCarriesTheSchema(t *testing.T) {
	for _, name := range []string{checkpointConstructor, checkpointConstraintFn, checkpointIndexFn} {
		if !checkpointFuncExists(t, name) {
			t.Fatalf("GoGraph %s does not expose checkpoint.%s. Either it was renamed upstream and this "+
				"gate outlived it, or the name is misspelt — and a misspelt name here is a rule that can "+
				"never be violated", goGraphVersion(t), name)
		}
	}

	for _, site := range scanCheckpointerConstructions(t, repoRoot(t)) {
		missing := make([]string, 0, 2)
		if !site.withConstraints {
			missing = append(missing, checkpointConstraintFn)
		}
		if !site.withIndexes {
			missing = append(missing, checkpointIndexFn)
		}
		if len(missing) == 0 {
			continue
		}
		t.Errorf("%s:%d (%s) builds a checkpoint.Checkpointer without checkpoint.%s.\n"+
			"  The checkpointer writes the snapshot itself, so the snapshot-writer sweep in this file "+
			"cannot see what it wrote. Without those options it writes a snapshot carrying no schema and "+
			"truncates the write-ahead log after it, which is the defect of release 1.15.2 arriving "+
			"through GoGraph instead of through our own snapshot call.\n"+
			"  Pass checkpoint.%s and checkpoint.%s, reading them from the engine that runs the "+
			"statements — Engine.ConstraintSpecsForSnapshot and Engine.IndexSpecsForSnapshot.",
			site.file, site.line, site.function, strings.Join(missing, " and "),
			checkpointConstraintFn, checkpointIndexFn)
	}
}

// checkpointerSite is one construction of a GoGraph checkpointer in production
// source, with whether the two schema options appear in the same enclosing
// function.
type checkpointerSite struct {
	file            string
	function        string
	line            int
	withConstraints bool
	withIndexes     bool
}

// checkpointFuncExists reports whether the pinned GoGraph release exposes name as
// an exported function of its store/checkpoint package, read from the upstream
// source in the module cache rather than from a list kept here.
func checkpointFuncExists(t *testing.T, name string) bool {
	t.Helper()

	dir := filepath.Join(goGraphModuleDir(t), "store", "checkpoint")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the GoGraph checkpoint package at %s: %v", dir, err)
	}

	fset := token.NewFileSet()
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
			if !ok || fn.Recv != nil {
				continue
			}
			if fn.Name.Name == name {
				return true
			}
		}
	}
	return false
}

// scanCheckpointerConstructions walks the repository's production Go files and
// returns every construction of a GoGraph checkpointer, noting whether the two
// schema options are applied in the same enclosing function.
//
// The options are looked for across the whole enclosing function rather than
// inside the constructor's own argument list, because a caller that assembles its
// options into a slice first is doing the same thing and must not be reported for
// it. The attribution is therefore generous in the direction that avoids a false
// failure and strict in the direction that matters: a function that never
// mentions the option at all cannot be passing it.
func scanCheckpointerConstructions(t *testing.T, root string) []*checkpointerSite {
	t.Helper()

	fset := token.NewFileSet()
	var sites []*checkpointerSite

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
		local := importLocalName(file, checkpointImportPath)
		if local == "" {
			return nil
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			called := checkpointCallsIn(fn.Body, local)
			for _, line := range called[checkpointConstructor] {
				sites = append(sites, &checkpointerSite{
					file:            rel,
					function:        fn.Name.Name,
					line:            fset.Position(line).Line,
					withConstraints: len(called[checkpointConstraintFn]) > 0,
					withIndexes:     len(called[checkpointIndexFn]) > 0,
				})
			}
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

// checkpointCallsIn returns, for every function of the checkpoint package called
// inside body under the local import name, the positions of those calls. Generic
// instantiations are followed, because every option function in that package is
// generic and is therefore written checkpoint.WithIndexSpecs[string, float64](…).
func checkpointCallsIn(body *ast.BlockStmt, local string) map[string][]token.Pos {
	calls := make(map[string][]token.Pos, 4)
	ast.Inspect(body, func(n ast.Node) bool {
		call, isCall := n.(*ast.CallExpr)
		if !isCall {
			return true
		}
		fun := call.Fun
		// checkpoint.WithIndexSpecs[string, float64] — strip the instantiation.
		if idx, isIndex := fun.(*ast.IndexExpr); isIndex {
			fun = idx.X
		}
		if idx, isIndex := fun.(*ast.IndexListExpr); isIndex {
			fun = idx.X
		}
		sel, isSel := fun.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		pkg, isIdent := sel.X.(*ast.Ident)
		if !isIdent || pkg.Name != local {
			return true
		}
		calls[sel.Sel.Name] = append(calls[sel.Sel.Name], call.Pos())
		return true
	})
	return calls
}
