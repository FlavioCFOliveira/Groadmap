// Regression fence for the checkpoint GATE, and for the one thing that can
// defeat it.
//
// The gate is the rule that a transaction which appended nothing must neither
// snapshot nor truncate (SPEC/GRAPH.md § What a Statement That Writes Nothing
// Changes on Disk). It was written for the short-lived surfaces, where this Store
// is the only party that ever truncates the write-ahead log, and rmp task #380
// made it serve a second caller for which that is no longer true: the dedicated
// graph server folds through the engine's own checkpointer, and that checkpointer
// cuts the folded prefix itself.
//
// So there are two properties here and they pull in opposite directions. The gate
// must REFUSE a fold nothing owes — that is what keeps one cut, rolled-back write
// from publishing a mapper and a tombstone set the graph no longer holds — and it
// must not refuse a fold that IS owed just because somebody else shortened the
// log underneath it. A gate that only ever said "no" would pass the first pair of
// tests below and lose committed history to the next open's replay for ever.
package graphstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
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

// TestCheckpointIfAppended_RefusesAFoldNothingAppended is the half the graph
// server needed and did not have.
//
// A statement that appended nothing must leave the store alone. What made this
// worth a fence of its own rather than an implicit property of Checkpoint is that
// the fold is now a PARAMETER: a caller supplies its own, and the only thing
// standing between an unconditional checkpointer and the disk is this decision.
func TestCheckpointIfAppended_RefusesAFoldNothingAppended(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "graph"))

	folds := 0
	ran, err := st.CheckpointIfAppended(func() error { folds++; return nil })
	if err != nil {
		t.Fatalf("the gate reported an error over a store nothing has written to: %v", err)
	}
	if ran || folds != 0 {
		t.Errorf("the gate ran the fold %d time(s) and reported ran=%v over a store nothing has "+
			"appended to. An ungated fold serialises the whole graph and rewrites the whole "+
			"snapshot, and after a rolled-back write it publishes the key mapper and the "+
			"tombstone set that write left behind (rmp task #380: 80 KB to 134 MB, permanently)",
			folds, ran)
	}
}

// TestCheckpointIfAppended_RunsOnceForOneAppend pins the other half of the
// ordinary case: the fold runs when something was written, and the mark it leaves
// behind refuses the NEXT call. A gate that ran but forgot to move the mark would
// fold on every shutdown for ever after one write.
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

	// The supplied fold truncated nothing — it is a counter — so the durable
	// offset has NOT moved and only the mark can refuse the second call. That is
	// the point: the gate must record what it let through.
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
// requirement reachable through the gate: a shutdown checkpoint that fails must
// reach the reader as a diagnostic, and it can only do that if the error survives
// the call. The gate must also not record a fold that did not happen.
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

	// And the failure did not move the mark: the fold is still owed.
	folds := 0
	ran, err = st.CheckpointIfAppended(func() error { folds++; return nil })
	if err != nil {
		t.Fatalf("the gate reported an error after a failed fold: %v", err)
	}
	if !ran || folds != 1 {
		t.Errorf("the gate refused the retry after a fold that FAILED (ran=%v, %d folds). A mark "+
			"advanced by a fold that did not happen loses the log to the next truncation",
			ran, folds)
	}
}

// TestCheckpointIfAppended_FoldsWhatAnotherPartysTruncationLeftBehind is the
// clause the dedicated graph server needs, and the one a reader is most likely to
// take for defensive noise.
//
// # The state this reproduces
//
// A server opens a store whose write-ahead log is NOT empty — the ordinary state
// after a server was killed, or after a shutdown this same gate refused — so the
// mark starts above zero. The engine's own in-flight checkpointer then folds and
// cuts the prefix it folded with wal.Writer.TruncatePrefix, and the durable
// offset DROPS below that mark. Writes after it land in a log shorter than the
// one the mark describes.
//
// A gate that compared the offset against the mark and nothing else would read
// that as "nothing appended" and refuse the shutdown fold — and the surviving
// suffix is precisely the part no snapshot covers, so the refusal would hand the
// next open a replay of history that a fold was owed for. The clause exists to
// stop exactly that, and this test is what makes deleting it go red.
//
// The truncation here is the engine's own call, made directly rather than
// simulated: TruncatePrefix is what the checkpointer runs in its third phase, and
// a test that moved the offset any other way would fence a different mechanism.
func TestCheckpointIfAppended_FoldsWhatAnotherPartysTruncationLeftBehind(t *testing.T) {
	graphDir := filepath.Join(t.TempDir(), "graph")

	// A first store leaves a non-empty log behind: it writes and closes WITHOUT
	// a checkpoint, which is what a killed server leaves and what this gate
	// itself leaves after it has refused a fold.
	first := openTestStore(t, graphDir)
	for i := 0; i < 40; i++ {
		commitWrite(t, first, "CREATE (n:Carried {key:'unfolded-history'})")
	}
	carried := first.WAL().DurableOffset()
	if carried <= 0 {
		t.Fatalf("40 committed writes left a write-ahead log of %d bytes; this test needs a "+
			"store that opens with unfolded history", carried)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("closing the first store: %v", err)
	}

	// The second store opens on that log, so its mark is `carried` and not zero.
	second := openTestStore(t, graphDir)
	commitWrite(t, second, "CREATE (n:Served {key:'written-before-the-fold'})")

	// The engine's in-flight checkpointer, in the one phase that matters here: it
	// folds the durable prefix and cuts it away.
	watermark := second.WAL().DurableOffset()
	if _, err := second.WAL().TruncatePrefix(watermark); err != nil {
		t.Fatalf("truncating the folded prefix at %d: %v", watermark, err)
	}

	// One write after the fold. It is deliberately far smaller than `carried`, so
	// the log is now SHORTER than the mark describes — which is the whole of the
	// trap.
	commitWrite(t, second, "CREATE (n:Served {key:'written-after-the-fold'})")

	suffix := second.WAL().DurableOffset()
	if suffix <= 0 || suffix >= carried {
		t.Fatalf("the log holds %d bytes after the fold against a mark of %d; this test needs a "+
			"non-empty suffix BELOW the mark, or it proves nothing", suffix, carried)
	}

	folds := 0
	ran, err := second.CheckpointIfAppended(func() error { folds++; return nil })
	if err != nil {
		t.Fatalf("the gate reported an error: %v", err)
	}
	if !ran || folds != 1 {
		t.Fatalf("the gate REFUSED the shutdown fold (ran=%v, %d folds) over a log holding %d "+
			"bytes that no snapshot covers, because another party's TruncatePrefix left the "+
			"durable offset below the mark of %d. Those bytes are committed history: refusing "+
			"here hands them to the next open's replay instead of folding them",
			ran, folds, suffix, carried)
	}
}
