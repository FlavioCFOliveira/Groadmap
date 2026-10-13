// Mitigation 4 of SPEC/GRAPH.md § Dependency Maturity Risk, for the engine
// upgrade that made every graph a directed multigraph (risk 2, the third format
// change): a store written by the previously pinned engine is opened through
// Groadmap's own open path, read back by the three published measures, written
// to, folded, and reopened.
//
// # The fixture
//
// testdata/multigraph-v016-store is a real store written by `rmp graph serve`
// built against GoGraph v0.16.0, the engine pinned before v0.17.0. It is in the
// segmented write-ahead-log format, and its snapshot manifest is at version 4
// and declares `directed: true, multigraph: true`. It holds nine nodes of five
// labels (Spec, Component, CodeFile, Test, Memory) and fourteen relationships of
// five types (CONTAINS 2, DEPENDS_ON 3, IMPLEMENTS 4, SEE_ALSO 3, TESTS 2),
// among them parallel relationships of one type between one ordered pair
// (IMPLEMENTS, DEPENDS_ON, SEE_ALSO and TESTS each carry such a pair) and one
// self-loop (SEE_ALSO on the Spec 'SPEC/GRAPH.md'), and every relationship but
// the two CONTAINS carrying properties.
//
// It was generated in two server lifetimes, in an isolated HOME:
//
//  1. `git archive develop | tar -x` at the commit that pinned GoGraph v0.16.0
//     (c708e6d), and `go build -o rmp016 ./cmd/rmp` in that tree.
//  2. `rmp016 roadmap create groadmap`, then `rmp016 graph serve -r groadmap
//     --socket <path>`; eight nodes and eleven relationships written through
//     `rmp016 graph client`; the server stopped with SIGINT, so its shutdown
//     fold wrote the version-4 snapshot (order 8, size 11).
//  3. A second `rmp016 graph serve`; one Memory node and three relationships
//     written through `rmp016 graph client` and acknowledged; the server killed
//     with SIGKILL before it could fold. That history is in the segmented log
//     beyond the position the snapshot records, so an open must replay it.
//  4. The graph directory copied as it stood, and its content read back by a
//     third `rmp016 graph serve` over a separate copy: 9 nodes, 14
//     relationships, and the distribution above. Those are the recorded values
//     below.
//
// It is copied for every test, so no test changes it.
package graphstore

import (
	"maps"
	"path/filepath"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/GoGraph/store/snapshot"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// multigraphFixture is the store written by GoGraph v0.16.0, as this package's
// tests reach it.
var multigraphFixture = filepath.Join("testdata", "multigraph-v016-store")

// previousManifestVersion is the snapshot manifest version GoGraph v0.16.0
// stamped, which the fixture carries and the adopted engine still reads.
const previousManifestVersion = 4

// graphContent is the content mitigation 4 compares: the node count, the
// relationship count, and the relationship count of each type.
type graphContent struct {
	nodes         int64
	relationships int64
	types         map[string]int64
}

// recordedFixtureContent is what the previously pinned engine read back from the
// fixture when it was written.
func recordedFixtureContent() graphContent {
	return graphContent{
		nodes:         9,
		relationships: 14,
		types: map[string]int64{
			"CONTAINS":   2,
			"DEPENDS_ON": 3,
			"IMPLEMENTS": 4,
			"SEE_ALSO":   3,
			"TESTS":      2,
		},
	}
}

// readRows runs a read and returns every row's values.
func readRows(t *testing.T, st *Store, statement string) [][]expr.Value {
	t.Helper()

	result, err := st.Engine().RunAny(t.Context(), statement, nil)
	if err != nil {
		t.Fatalf("running %q: %v", statement, err)
	}
	var rows [][]expr.Value
	for result.Next() {
		row := make([]expr.Value, len(result.Columns()))
		for i := range row {
			row[i] = result.ValueAt(i)
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		_ = result.Close() //nolint:errcheck // the iteration error is the one reported
		t.Fatalf("reading %q: %v", statement, err)
	}
	if err := result.Close(); err != nil {
		t.Fatalf("closing %q: %v", statement, err)
	}
	return rows
}

// integer returns v as an integer, failing the test when it is not one.
func integer(t *testing.T, statement string, v expr.Value) int64 {
	t.Helper()

	n, ok := v.(expr.IntegerValue)
	if !ok {
		t.Fatalf("%q returned %v (%T), want an integer", statement, v, v)
	}
	return int64(n)
}

// readCount runs a statement returning one integer and returns it.
func readCount(t *testing.T, st *Store, statement string) int64 {
	t.Helper()

	rows := readRows(t, st, statement)
	if len(rows) != 1 || len(rows[0]) != 1 {
		t.Fatalf("%q returned %v, want one row of one column", statement, rows)
	}
	return integer(t, statement, rows[0][0])
}

// readContent measures st by the three measures of mitigation 4.
func readContent(t *testing.T, st *Store) graphContent {
	t.Helper()

	c := graphContent{
		nodes:         readCount(t, st, "MATCH (n) RETURN count(n)"),
		relationships: readCount(t, st, "MATCH ()-[r]->() RETURN count(r)"),
		types:         map[string]int64{},
	}
	const byType = "MATCH ()-[r]->() RETURN type(r), count(*)"
	for _, row := range readRows(t, st, byType) {
		name, ok := row[0].(expr.StringValue)
		if !ok {
			t.Fatalf("%q returned the type %v (%T), want a string", byType, row[0], row[0])
		}
		c.types[string(name)] = integer(t, byType, row[1])
	}
	return c
}

// assertContent fails the test when got differs from want by any measure.
func assertContent(t *testing.T, when string, want, got graphContent) {
	t.Helper()

	if got.nodes != want.nodes || got.relationships != want.relationships || !maps.Equal(got.types, want.types) {
		t.Fatalf("%s the store reads %d node(s), %d relationship(s), types %v; want %d, %d, %v",
			when, got.nodes, got.relationships, got.types, want.nodes, want.relationships, want.types)
	}
}

// manifestVersion reads the version the snapshot manifest under graphDir carries.
func manifestVersion(t *testing.T, graphDir string) int {
	t.Helper()

	m, err := snapshot.ReadManifestFile(filepath.Join(graphDir, snapshotDirName, manifestFileName))
	if err != nil {
		t.Fatalf("reading the snapshot manifest under %s: %v", graphDir, err)
	}
	return m.Version
}

// TestOpen_ReadsAndRewritesAStoreThePreviousEngineWrote is mitigation 4's
// empirical check for the adoption of the engine that made every graph a directed
// multigraph: the store the previous engine wrote reads back the content recorded
// when it was written, a write against it is read back, the fold rewrites the
// snapshot manifest at the adopted engine's version, the store reopens with the
// same content, and no backup is taken at any point (risk 2: this format change
// is not one Groadmap backs up).
func TestOpen_ReadsAndRewritesAStoreThePreviousEngineWrote(t *testing.T) {
	home := filepath.Join(t.TempDir(), "groadmap")
	graphDir := filepath.Join(home, "graph")
	testenv.CopyTree(t, multigraphFixture, graphDir)

	if got := manifestVersion(t, graphDir); got != previousManifestVersion {
		t.Fatalf("the fixture's snapshot manifest is at version %d, want %d: it is not the store the "+
			"previous engine wrote", got, previousManifestVersion)
	}
	if snapshot.ManifestVersion <= previousManifestVersion {
		t.Fatalf("the adopted engine stamps manifest version %d, want one above %d",
			snapshot.ManifestVersion, previousManifestVersion)
	}
	if due, err := NeedsBackup(graphDir); err != nil || due {
		t.Fatalf("NeedsBackup = %v, %v over the fixture, want false, nil: a segmented store owes no backup",
			due, err)
	}

	st, err := Open(graphDir)
	if err != nil {
		t.Fatalf("opening the store the previous engine wrote: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() }) //nolint:errcheck // the explicit Close below is the one checked

	want := recordedFixtureContent()
	assertContent(t, "opened by the adopted engine,", want, readContent(t, st))

	// A parallel relationship of a type the pair already carries, and a node:
	// the write that makes the fold owed.
	commitWrite(t, st, "MATCH (a:Component {key:'internal/web'}), (b:Component {key:'internal/graphstore'}) "+
		"CREATE (a)-[:DEPENDS_ON {via:'CheckpointIfAppended'}]->(b), "+
		"(:Memory {key:'gograph-v0-17-0-adoption', summary:'Every graph is a directed multigraph'})")
	want.nodes++
	want.relationships++
	want.types["DEPENDS_ON"]++
	assertContent(t, "after the write,", want, readContent(t, st))

	ran, err := st.CheckpointIfAppended(engineFold(st))
	if err != nil || !ran {
		t.Fatalf("the fold after the write did not run (ran=%v, err=%v)", ran, err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("closing the store after the fold: %v", err)
	}
	if got := manifestVersion(t, graphDir); got != snapshot.ManifestVersion {
		t.Errorf("after the fold the snapshot manifest is at version %d, want the adopted engine's %d",
			got, snapshot.ManifestVersion)
	}

	reopened, err := Open(graphDir)
	if err != nil {
		t.Fatalf("reopening the store the adopted engine folded: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() }) //nolint:errcheck // a read-only reopen
	assertContent(t, "reopened after the fold,", want, readContent(t, reopened))
	if err := reopened.Close(); err != nil {
		t.Fatalf("closing the reopened store: %v", err)
	}

	if got := homeEntries(t, home); len(got) != 0 {
		t.Errorf("the roadmap home holds %v; no backup is taken for this format change", got)
	}
}
