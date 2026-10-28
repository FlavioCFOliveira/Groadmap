// Regression fence for the checkpoint GATE, stated in log positions.
//
// The gate is the rule that a fold is owed when, and only when, the write-ahead
// log holds a durable record no fold has covered (SPEC/GRAPH.md § What a
// Statement That Writes Nothing Changes on Disk; § Durability and Checkpointing
// in a Long-Lived Process, rules 4, 5, 9 and 10). Every record carries a logical
// position that only grows, and a fold neither resets positions nor moves them
// when it discards the folded prefix, so the gate compares the log's durable end
// with the position the last fold covered and with nothing else.
//
// There are two properties here and they pull in opposite directions. The gate
// must REFUSE a fold nothing owes — that is what keeps an idle server off the
// disk — and it must never refuse a fold that IS owed: records the store opened
// with, records a writer appended while a fold ran, and history the single-file
// log still held when the engine converted the store. A gate that only ever said
// "no" would pass the first pair of tests below and leave committed history to
// the next open's replay for ever.
package graphstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/store/checkpoint"
	"github.com/FlavioCFOliveira/GoGraph/store/wal"
)

// errFoldRefused is the failure a test's own fold reports. It is a package-level
// sentinel because a test must be able to say that THIS error came back out of
// the gate rather than some error, and because err113 forbids constructing one
// at the call site.
var errFoldRefused = errors.New("the fold refused: the snapshot directory is read-only")

// openTestStore opens a Store over a fresh graph directory and closes it with the
// test. The directory is created here because Open deliberately does not create
// one (see [Open]).
func openTestStore(t *testing.T, graphDir string) *Store {
	t.Helper()

	if err := os.MkdirAll(graphDir, 0700); err != nil {
		t.Fatalf("creating %s: %v", graphDir, err)
	}
	st, err := Open(graphDir)
	if err != nil {
		t.Fatalf("opening the graph store at %s: %v", graphDir, err)
	}
	t.Cleanup(func() { _ = st.Close() }) //nolint:errcheck // the close releases the hold whatever the log reports
	return st
}

// commitWrite runs one write through the engine and commits it, which is the
// sequence every Groadmap surface performs: RunAny, drain, Close. Close is the
// durability boundary — it applies and commits — so a test that skipped it would
// leave the write-ahead log untouched and measure nothing.
func commitWrite(t *testing.T, st *Store, statement string) {
	t.Helper()

	result, err := st.Engine().RunAny(context.Background(), statement, nil)
	if err != nil {
		t.Fatalf("running %q: %v", statement, err)
	}
	for result.Next() {
	}
	if err := result.Err(); err != nil {
		_ = result.Close() //nolint:errcheck // rolling back; the commit error is moot after an iteration failure
		t.Fatalf("draining %q: %v", statement, err)
	}
	if err := result.Close(); err != nil {
		t.Fatalf("committing %q: %v", statement, err)
	}
}

// engineFold is the fold the dedicated graph server performs, built the way the
// server builds it: the engine's own checkpointer, under the store's commit
// serialiser, carrying the mapper codec and the registered schema. It writes a
// snapshot that records the position it covers, records that position in the
// log's control file, and discards the segments wholly below it.
//
// It is a test's copy of the server's construction, and deliberately so: this
// package owns the gate and not the fold, and a test of the gate needs a REAL
// fold, because the property under test is how the gate's opening mark reads
// the position a real snapshot recorded.
func engineFold(st *Store) func() error {
	return func() error {
		var unused sync.Mutex
		cp := checkpoint.New[string, float64](
			checkpoint.Config{Dir: st.Dir()},
			st.Graph(), st.WAL(), &unused,
			checkpoint.WithCommitSerialiser[string, float64](st.Txn().RunUnderCommitLock),
			checkpoint.WithMapperCodec[string, float64](st.Txn().Codec()),
			checkpoint.WithWeightCodec[string, float64](st.Txn().WeightCodec()),
			checkpoint.WithConstraintSpecs[string, float64](st.Engine().ConstraintSpecsForSnapshot),
			checkpoint.WithIndexSpecs[string, float64](st.Engine().IndexSpecsForSnapshot),
		)
		return cp.RunCheckpoint()
	}
}

// TestCheckpointIfAppended_RefusesAFoldNothingAppended is the half an idle
// server depends on: a store nothing has written to owes no fold.
func TestCheckpointIfAppended_RefusesAFoldNothingAppended(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "graph"))

	folds := 0
	ran, err := st.CheckpointIfAppended(func() error { folds++; return nil })
	if err != nil {
		t.Fatalf("the gate reported an error over a store nothing has written to: %v", err)
	}
	if ran || folds != 0 {
		t.Errorf("the gate ran the fold %d time(s) and reported ran=%v over a store nothing has "+
			"appended to. An ungated fold serialises the whole graph and rewrites the whole snapshot "+
			"for no change at all", folds, ran)
	}
}

// TestCheckpointIfAppended_RunsOnceForOneAppend pins the ordinary case: the fold
// runs when something was written, and the mark it leaves behind refuses the NEXT
// call. A gate that ran but forgot to move the mark would fold on every due
// instant for ever after one write.
func TestCheckpointIfAppended_RunsOnceForOneAppend(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "graph"))
	commitWrite(t, st, "CREATE (n:Gate {key:'one-append'})")

	folds := 0
	fold := func() error { folds++; return nil }

	ran, err := st.CheckpointIfAppended(fold)
	if err != nil {
		t.Fatalf("the gate reported an error after a committed write: %v", err)
	}
	if !ran || folds != 1 {
		t.Fatalf("the gate ran the fold %d time(s) and reported ran=%v after a committed write; "+
			"the write-ahead log grew, so the fold is owed", folds, ran)
	}

	ran, err = st.CheckpointIfAppended(fold)
	if err != nil {
		t.Fatalf("the gate reported an error on the second call: %v", err)
	}
	if ran || folds != 1 {
		t.Errorf("the gate ran the fold again (ran=%v, %d folds in total) with nothing appended "+
			"since the first one. The mark is what stops a server folding the same log at every "+
			"opportunity it is given", ran, folds)
	}
}

// TestCheckpointIfAppended_ReturnsTheFoldsFailure keeps rmp task #369's
// requirement reachable through the gate: a checkpoint that fails must reach the
// reader as a diagnostic, and it can only do that if the error survives the call.
// The gate must also not record a fold that did not happen.
func TestCheckpointIfAppended_ReturnsTheFoldsFailure(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "graph"))
	commitWrite(t, st, "CREATE (n:Gate {key:'a-failing-fold'})")

	ran, err := st.CheckpointIfAppended(func() error { return errFoldRefused })
	if ran {
		t.Errorf("the gate reported ran=true for a fold that failed")
	}
	if !errors.Is(err, errFoldRefused) {
		t.Fatalf("the gate returned %v, want the fold's own error. A shutdown checkpoint that "+
			"fails must reach the reader as a diagnostic (SPEC/GRAPH.md § Durability and "+
			"Checkpointing in a Long-Lived Process, rule 7), which it cannot do if the gate "+
			"swallows it", err)
	}

	folds := 0
	ran, err = st.CheckpointIfAppended(func() error { folds++; return nil })
	if err != nil {
		t.Fatalf("the gate reported an error after a failed fold: %v", err)
	}
	if !ran || folds != 1 {
		t.Errorf("the gate refused the retry after a fold that FAILED (ran=%v, %d folds). A mark "+
			"advanced by a fold that did not happen records unfolded history as folded",
			ran, folds)
	}
}

// TestCheckpointIfAppended_ARealFoldMovesNoPosition is what makes the gate's
// comparison sound in positions: a fold that records its position and discards
// the folded prefix leaves the durable end where it was, and a write after it
// lands beyond the mark. Under the single-file log a truncation moved the log's
// coordinates back to zero, and a gate in offsets had to recognise that; in
// positions there is nothing to recognise, and this test fails if the engine ever
// starts resetting them again (SPEC/GRAPH.md § Synchronous Checkpoint on Write,
// step 3).
func TestCheckpointIfAppended_ARealFoldMovesNoPosition(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "graph"))
	commitWrite(t, st, "CREATE (s:Spec {key:'SPEC/GRAPH.md'})")

	before := st.WAL().DurableOffset()
	ran, err := st.CheckpointIfAppended(engineFold(st))
	if err != nil || !ran {
		t.Fatalf("the first fold did not run (ran=%v, err=%v) after a committed write", ran, err)
	}
	if after := st.WAL().DurableOffset(); after != before {
		t.Fatalf("the log's durable end moved from %d to %d across a fold. Positions only grow and "+
			"a fold does not move them; a gate comparing positions is unsound on an engine that does",
			before, after)
	}

	if ran, err := st.CheckpointIfAppended(func() error { t.Error("a fold ran with nothing appended"); return nil }); err != nil || ran {
		t.Fatalf("the gate folded again (ran=%v, err=%v) with nothing appended since the real fold", ran, err)
	}

	commitWrite(t, st, "CREATE (s:Spec {key:'SPEC/WEB.md'})")
	folds := 0
	ran, err = st.CheckpointIfAppended(func() error { folds++; return nil })
	if err != nil || !ran || folds != 1 {
		t.Fatalf("the gate refused (ran=%v, %d folds, err=%v) a write committed after a real fold; "+
			"its records lie beyond the position that fold covered", ran, folds, err)
	}
}

// leaveUnfoldedTail writes to a fresh store and closes it WITHOUT a fold, which
// is the store a killed server leaves behind: a write-ahead log holding committed
// records that no snapshot covers. It returns the log's durable end.
func leaveUnfoldedTail(t *testing.T, graphDir string) int64 {
	t.Helper()

	if err := os.MkdirAll(graphDir, 0700); err != nil {
		t.Fatalf("creating %s: %v", graphDir, err)
	}
	first, err := Open(graphDir)
	if err != nil {
		t.Fatalf("opening the graph store at %s: %v", graphDir, err)
	}
	commitWrite(t, first, "CREATE (n:Acknowledged {key:'written-before-the-kill'})")
	tail := first.WAL().DurableOffset()
	if err := first.Close(); err != nil {
		t.Fatalf("closing the first store: %v", err)
	}
	if tail <= 0 {
		t.Fatalf("a committed write left a log whose durable end is %d; this test needs a store "+
			"that opens with an unfolded tail", tail)
	}
	return tail
}

// TestCheckpointIfAppended_FoldsATailTheStoreOpenedWith pins the opening mark at
// the position the snapshot covers — the start of the log when there is no
// snapshot — and not at the log's end at open (SPEC/GRAPH.md § Durability and
// Checkpointing in a Long-Lived Process, rule 4; acceptance criterion 83).
//
// A server that was killed leaves records no snapshot covers, and the next
// server replays them at open without folding them. A gate whose mark started at
// the log's end would find nothing grown and fold them never.
func TestCheckpointIfAppended_FoldsATailTheStoreOpenedWith(t *testing.T) {
	graphDir := filepath.Join(t.TempDir(), "graph")
	tail := leaveUnfoldedTail(t, graphDir)

	st := openTestStore(t, graphDir)
	if got := st.WAL().DurableOffset(); got != tail {
		t.Fatalf("the reopened log's durable end is %d, want the %d the first store left", got, tail)
	}

	folds := 0
	ran, err := st.CheckpointIfAppended(func() error { folds++; return nil })
	if err != nil {
		t.Fatalf("the gate reported an error: %v", err)
	}
	if !ran || folds != 1 {
		t.Fatalf("the gate refused to fold (ran=%v, %d folds) records up to position %d the store "+
			"opened with. They are committed history no snapshot covers", ran, folds, tail)
	}
}

// TestCheckpointIfAppended_ReopenedAfterAFoldOwesNothing is the other half of the
// opening mark, and the one the change to positions made necessary: a store
// whose last fold covered the whole log, reopened, owes no fold — although its
// log still holds every record of that history, because a log shorter than one
// segment is not shortened by a fold at all.
//
// A gate that took the start of the log as its opening mark would see those
// retained records as grown and rewrite the snapshot at the first due instant of
// every idle server, which is the defect acceptance criterion 81 names
// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rules 4
// and 10).
func TestCheckpointIfAppended_ReopenedAfterAFoldOwesNothing(t *testing.T) {
	graphDir := filepath.Join(t.TempDir(), "graph")
	leaveUnfoldedTail(t, graphDir)

	first, err := Open(graphDir)
	if err != nil {
		t.Fatalf("opening the graph store: %v", err)
	}
	if ran, err := first.CheckpointIfAppended(engineFold(first)); err != nil || !ran {
		_ = first.Close() //nolint:errcheck // the test is failing; the close error adds nothing
		t.Fatalf("the fold of the opening tail did not run (ran=%v, err=%v)", ran, err)
	}
	covered := first.WAL().DurableOffset()
	if err := first.Close(); err != nil {
		t.Fatalf("closing the folded store: %v", err)
	}

	st := openTestStore(t, graphDir)
	if end := st.WAL().DurableOffset(); end != covered || end <= 0 {
		t.Fatalf("the reopened log's durable end is %d, want the %d the fold covered; the test needs a "+
			"log that still holds the folded records", end, covered)
	}
	ran, err := st.CheckpointIfAppended(func() error { t.Error("a fold ran over a folded log"); return nil })
	if err != nil || ran {
		t.Errorf("the gate folded (ran=%v, err=%v) a reopened store whose snapshot covers its whole "+
			"log. The opening mark is the position the snapshot records, not the start of the log, "+
			"because positions do not restart at a fold", ran, err)
	}
}

// TestCheckpointIfAppended_FoldsAWriteThatLandedDuringTheFold pins the rule that
// the mark is a position the fold is proven to have covered — the durable end
// read before the fold was requested — never the end read after it returns
// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rule 4
// and rule 10, third bullet).
//
// The fold here commits a write while it "writes its snapshot", as writers do
// while the engine's checkpointer works lock-free. A gate that took the end after
// the fold as its mark would record that write as folded, and a server that then
// received no further write would fold it neither in flight nor at shutdown.
func TestCheckpointIfAppended_FoldsAWriteThatLandedDuringTheFold(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "graph"))
	commitWrite(t, st, "CREATE (n:Folded {key:'before-the-fold'})")

	ran, err := st.CheckpointIfAppended(func() error {
		commitWrite(t, st, "CREATE (n:Concurrent {key:'committed-while-the-snapshot-was-written'})")
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("the fold did not run (ran=%v, err=%v)", ran, err)
	}

	folds := 0
	ran, err = st.CheckpointIfAppended(func() error { folds++; return nil })
	if err != nil {
		t.Fatalf("the gate reported an error: %v", err)
	}
	if !ran || folds != 1 {
		t.Fatalf("the gate REFUSED to fold (ran=%v, %d folds) a write that committed while the "+
			"previous fold was running. Nothing proves that fold covered it; a mark taken from the "+
			"durable end after the fold records it as folded", ran, folds)
	}
}

// TestCheckpointIfAppended_ConvertedHistoryIsOwedOneFold is acceptance
// criterion 93's gate half: history the single-file log held when the engine
// converted the store lies outside the segmented log's positions, so no durable
// end can stand for it, and it is owed one fold all the same (SPEC/GRAPH.md §
// Durability and Checkpointing in a Long-Lived Process, rule 4; § Graph Store
// Format Migration, rule 11). The fold that pays it replaces the single-file log
// with the engine's one-record stub, and after it the gate refuses.
func TestCheckpointIfAppended_ConvertedHistoryIsOwedOneFold(t *testing.T) {
	graphDir := filepath.Join(t.TempDir(), "graph")
	copySingleFileFixture(t, graphDir)

	st := openTestStore(t, graphDir)
	ctl, ok, err := InspectLog(graphDir)
	if err != nil || !ok || ctl.Flags&wal.ControlLegacyV1Pending == 0 {
		t.Fatalf("the converted fixture's log reports control=%v flags=%#x err=%v; this test needs a "+
			"store whose single-file history is still unfolded", ok, ctl.Flags, err)
	}
	if end := st.WAL().DurableOffset(); end != 0 {
		t.Fatalf("the freshly converted log's durable end is %d; the test is about history that lies "+
			"before every segmented position, so it needs an empty segmented log", end)
	}

	ran, err := st.CheckpointIfAppended(engineFold(st))
	if err != nil || !ran {
		t.Fatalf("the gate did not fold (ran=%v, err=%v) the history the single-file log held at the "+
			"conversion. No fold in the segmented format covers it", ran, err)
	}
	ctl, _, err = InspectLog(graphDir)
	if err != nil || ctl.Flags&wal.ControlLegacyV1Pending != 0 {
		t.Fatalf("after the fold the log reports flags=%#x err=%v; the fold should have replaced the "+
			"single-file log with its stub", ctl.Flags, err)
	}

	if ran, err := st.CheckpointIfAppended(func() error { t.Error("a second fold ran"); return nil }); err != nil || ran {
		t.Errorf("the gate folded again (ran=%v, err=%v) once the converted history was folded", ran, err)
	}
}
