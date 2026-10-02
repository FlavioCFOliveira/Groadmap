// Regression fence for the checkpoint GATE, and for the one thing that can
// defeat it.
//
// The gate is the rule that a fold is owed when, and only when, the write-ahead
// log holds bytes no fold has covered (SPEC/GRAPH.md § What a Statement That
// Writes Nothing Changes on Disk; § Durability and Checkpointing in a Long-Lived
// Process, rules 4, 5 and 10). The dedicated graph server puts both of its
// checkpoints through it, and folds through the engine's own checkpointer, which
// cuts the folded prefix itself and keeps the unfolded suffix.
//
// So there are two properties here and they pull in opposite directions. The gate
// must REFUSE a fold nothing owes — that is what keeps an idle server off the
// disk, and one cut, rolled-back write from publishing a mapper and a tombstone
// set the graph no longer holds — and it must never refuse a fold that IS owed: a
// tail the store opened with, bytes a writer appended while a fold ran, or a
// suffix somebody else's truncation left behind. A gate that only ever said "no"
// would pass the first pair of tests below and lose committed history to the next
// open's replay for ever.
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
	ran, err := st.CheckpointIfAppended(func() (int64, error) { folds++; return 0, nil })
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
	fold := func() (int64, error) { folds++; return 0, nil }

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

	ran, err := st.CheckpointIfAppended(func() (int64, error) { return 0, errFoldRefused })
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
	ran, err = st.CheckpointIfAppended(func() (int64, error) { folds++; return 0, nil })
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
// clause that keeps the gate honest when a party it did not gate truncates the
// log, and the one a reader is most likely to take for defensive noise.
//
// # The state this reproduces
//
// A fold the gate let through truncated nothing, so the mark sits at the offset
// that fold was proven to cover, above zero. A party outside the gate then cuts
// the prefix it folded with wal.Writer.TruncatePrefix, and the durable offset
// DROPS below that mark. Writes after it land in a log shorter than the one the
// mark describes.
//
// A gate that compared the offset against the mark and nothing else would read
// that as "nothing unfolded" and refuse the next fold — and the surviving suffix
// is precisely the part no snapshot covers, so the refusal would hand the next
// open a replay of history that a fold was owed for. The clause exists to stop
// exactly that, and this test is what makes deleting it go red.
//
// The truncation here is the engine's own call, made directly rather than
// simulated: TruncatePrefix is what the checkpointer runs in its third phase, and
// a test that moved the offset any other way would fence a different mechanism.
func TestCheckpointIfAppended_FoldsWhatAnotherPartysTruncationLeftBehind(t *testing.T) {
	st := openTestStore(t, filepath.Join(t.TempDir(), "graph"))

	for i := 0; i < 40; i++ {
		commitWrite(t, st, "CREATE (n:Carried {key:'folded-without-a-cut'})")
	}
	// A fold that cut nothing: the mark moves to the offset it covered.
	if ran, err := st.CheckpointIfAppended(func() (int64, error) { return 0, nil }); err != nil || !ran {
		t.Fatalf("the first fold did not run (ran=%v, err=%v) over 40 committed writes", ran, err)
	}
	mark := st.WAL().DurableOffset()

	// A party outside the gate folds the durable prefix and cuts it away.
	if _, err := st.WAL().TruncatePrefix(mark); err != nil {
		t.Fatalf("truncating the folded prefix at %d: %v", mark, err)
	}

	// One write after that cut. It is deliberately far smaller than the mark, so
	// the log is now SHORTER than the mark describes — which is the whole of the
	// trap.
	commitWrite(t, st, "CREATE (n:Served {key:'written-after-the-cut'})")

	suffix := st.WAL().DurableOffset()
	if suffix <= 0 || suffix >= mark {
		t.Fatalf("the log holds %d bytes after the cut against a mark of %d; this test needs a "+
			"non-empty suffix BELOW the mark, or it proves nothing", suffix, mark)
	}

	folds := 0
	ran, err := st.CheckpointIfAppended(func() (int64, error) { folds++; return 0, nil })
	if err != nil {
		t.Fatalf("the gate reported an error: %v", err)
	}
	if !ran || folds != 1 {
		t.Fatalf("the gate REFUSED the fold (ran=%v, %d folds) over a log holding %d bytes that no "+
			"snapshot covers, because another party's TruncatePrefix left the durable offset "+
			"below the mark of %d. Those bytes are committed history: refusing here hands them "+
			"to the next open's replay instead of folding them",
			ran, folds, suffix, mark)
	}
}

// leaveUnfoldedTail writes to a fresh store and closes it WITHOUT a fold, which
// is the store a killed server leaves behind: a write-ahead log holding committed
// frames that no snapshot covers. It returns the log's length.
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
		t.Fatalf("a committed write left a write-ahead log of %d bytes; this test needs a store "+
			"that opens with an unfolded tail", tail)
	}
	return tail
}

// TestCheckpointIfAppended_FoldsATailTheStoreOpenedWith is the owner's decision
// on rmp task #383, pinned: the mark starts at the beginning of the log, not at
// its length at open (SPEC/GRAPH.md § Durability and Checkpointing in a
// Long-Lived Process, rule 4).
//
// A server that was killed leaves a log tail no snapshot covers, and the next
// server replays it at open without folding it. A gate whose mark started at the
// log's length would find nothing grown and fold that tail never — neither in
// flight nor at shutdown — so every later open would replay it again. The fold
// here is owed with NO write in this Store at all.
func TestCheckpointIfAppended_FoldsATailTheStoreOpenedWith(t *testing.T) {
	graphDir := filepath.Join(t.TempDir(), "graph")
	tail := leaveUnfoldedTail(t, graphDir)

	st := openTestStore(t, graphDir)
	if got := st.WAL().DurableOffset(); got != tail {
		t.Fatalf("the reopened log holds %d bytes, want the %d the first store left", got, tail)
	}

	folds := 0
	ran, err := st.CheckpointIfAppended(func() (int64, error) { folds++; return 0, nil })
	if err != nil {
		t.Fatalf("the gate reported an error: %v", err)
	}
	if !ran || folds != 1 {
		t.Fatalf("the gate refused to fold (ran=%v, %d folds) a %d-byte tail the store opened "+
			"with. That tail is committed history no snapshot covers; a mark taken at the log's "+
			"length at open records it as folded, and it is then folded neither in flight nor "+
			"at shutdown", ran, folds, tail)
	}
}

// TestCheckpointIfAppended_RefusesAgainOnceTheOpeningTailIsFolded is the other
// half of the decision above: the tail is owed ONE fold, not one per call. Once a
// fold has cut it away the log is empty and the gate refuses, which is what lets
// an idle server that opened with a tail write nothing after its first fold
// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rule 10).
func TestCheckpointIfAppended_RefusesAgainOnceTheOpeningTailIsFolded(t *testing.T) {
	graphDir := filepath.Join(t.TempDir(), "graph")
	leaveUnfoldedTail(t, graphDir)
	st := openTestStore(t, graphDir)

	// The real fold: a snapshot, then the whole log truncated.
	ran, err := st.Checkpoint()
	if err != nil || !ran {
		t.Fatalf("the first checkpoint did not fold the opening tail (ran=%v, err=%v)", ran, err)
	}
	if off := st.WAL().DurableOffset(); off != 0 {
		t.Fatalf("the log holds %d bytes after a fold that truncated it", off)
	}

	folds := 0
	ran, err = st.CheckpointIfAppended(func() (int64, error) { folds++; return 0, nil })
	if err != nil {
		t.Fatalf("the gate reported an error: %v", err)
	}
	if ran || folds != 0 {
		t.Errorf("the gate folded again (ran=%v, %d folds) over an empty log whose tail was "+
			"already folded; an idle server would rewrite its snapshot at every due instant",
			ran, folds)
	}
}

// TestCheckpointIfAppended_FoldsAWriteThatLandedDuringTheFold pins the rule rmp
// task #383 turns on: the mark is the point the fold CAPTURED, never the offset
// read after it returns (SPEC/GRAPH.md § Durability and Checkpointing in a
// Long-Lived Process, rule 10, third bullet).
//
// # What the fold below does, and why it is the engine's shape
//
// The engine's checkpointer captures the graph and the durable offset W at one
// transaction boundary, releases the commit lock while it writes the snapshot —
// writers commit throughout — and then cuts only the prefix [0, W). What writers
// appended meanwhile survives as the log's new content, and no snapshot covers
// it. The fold here does exactly that with the engine's own calls: it reads W,
// commits a write while "writing the snapshot", cuts [0, W) with TruncatePrefix,
// and reports W as what it cut.
//
// A gate that took the offset after the fold as its mark — which is what this
// gate did before #383 — would record the surviving write as folded and refuse
// the next call, and a server that then received no further write would fold that
// write neither in flight nor at shutdown. Both sub-tests run the same scenario
// for the two shapes a fold's report can take.
func TestCheckpointIfAppended_FoldsAWriteThatLandedDuringTheFold(t *testing.T) {
	for _, tc := range []struct {
		name string
		// cut is whether the fold cuts the prefix it captured, as the engine's
		// checkpointer does when its snapshot is self-sufficient, or keeps the
		// whole log, as it does when it is not.
		cut bool
	}{
		{name: "a fold that cuts the prefix it captured", cut: true},
		{name: "a fold that keeps the whole log", cut: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := openTestStore(t, filepath.Join(t.TempDir(), "graph"))
			commitWrite(t, st, "CREATE (n:Folded {key:'before-the-fold'})")

			ran, err := st.CheckpointIfAppended(func() (int64, error) {
				watermark := st.WAL().DurableOffset()
				commitWrite(t, st, "CREATE (n:Concurrent {key:'committed-while-the-snapshot-was-written'})")
				if !tc.cut {
					return 0, nil
				}
				return st.WAL().TruncatePrefix(watermark)
			})
			if err != nil || !ran {
				t.Fatalf("the fold did not run (ran=%v, err=%v)", ran, err)
			}
			if off := st.WAL().DurableOffset(); off <= 0 {
				t.Fatalf("the log holds %d bytes after the fold; the write committed during it "+
					"should have survived the cut, so this test proves nothing", off)
			}

			folds := 0
			ran, err = st.CheckpointIfAppended(func() (int64, error) { folds++; return 0, nil })
			if err != nil {
				t.Fatalf("the gate reported an error: %v", err)
			}
			if !ran || folds != 1 {
				t.Fatalf("the gate REFUSED to fold (ran=%v, %d folds) a write that committed while "+
					"the previous fold was writing its snapshot. That fold captured the log before "+
					"the write, so no snapshot covers it; a mark taken from the offset after the fold "+
					"records it as folded, and it would then be folded neither in flight nor at "+
					"shutdown", ran, folds)
			}
		})
	}
}
