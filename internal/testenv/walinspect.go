package testenv

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/store/wal"
)

// WALState is a store's write-ahead log as the engine's own read-only
// inspection reports it, in log positions.
//
// It exists because the log's files no longer say what a test used to read off
// them. The log is a sequence of segments whose records carry logical positions
// that only grow; a fold records in the control file the position its snapshot
// covers and discards only whole segments below it, so a log shorter than one
// segment is not shortened by a fold at all, and the single-file `wal` beside the
// segments is a fixed one-record stub. A test that read a file's size would fail
// on a correct store (SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 3;
// acceptance criterion 14). What a fold covered, and what lies beyond it, are
// answered here instead, from the engine's own reading of the log.
type WALState struct {
	// Frames is every retained record of the segmented log, in position order.
	Frames []WALFrame
	// Control is the log's control file; HasControl is false for a store whose
	// log the engine has never opened in the segmented format.
	Control    wal.Control
	HasControl bool
	// End is the position just past the last valid record: the log's end.
	End int64

	// covered and oldestRetained are the control file's CheckpointRedoPos and
	// OldestRetainedPos, converted to int64 once, by InspectWAL, under the
	// range check of logPosition.
	covered, oldestRetained int64
}

// WALFrame is one retained record of the segmented log.
type WALFrame struct {
	// Pos is the record's logical position; End is the position just past it.
	Pos, End int64
	// Reservation reports a control record — the engine's node-identifier
	// reservations and its clean-close marks — rather than a transaction's
	// operation. Such a record describes no node, relationship, property, label,
	// index or constraint (SPEC/GRAPH.md § Statement Time Budget, rule 3).
	Reservation bool
}

// Covered is the position the control file records as covered by the last fold:
// every record below it is in the snapshot.
func (s *WALState) Covered() int64 {
	return s.covered
}

// OldestRetained is the first position the log retains, as the control file
// records it.
func (s *WALState) OldestRetained() int64 {
	return s.oldestRetained
}

// FramesFrom returns the retained records at or beyond pos.
func (s *WALState) FramesFrom(pos int64) []WALFrame {
	var out []WALFrame
	for _, f := range s.Frames {
		if f.Pos >= pos {
			out = append(out, f)
		}
	}
	return out
}

// InspectWAL reads the write-ahead log of the store in graphDir through the
// engine's read-only inspection, failing the test if the log cannot be read. It
// changes nothing on disk, and it may be called while a server holds the store.
func InspectWAL(tb testing.TB, graphDir string) *WALState {
	tb.Helper()

	log, err := wal.OpenLog(filepath.Join(graphDir, "wal"))
	if err != nil {
		tb.Fatalf("inspecting the write-ahead log under %s: %v", graphDir, err)
	}
	defer log.Close() //nolint:errcheck // a read-only inspection; nothing to report on close

	state := &WALState{}
	state.Control, state.HasControl = log.Control()
	state.covered = logPosition(tb, "the control file's covered position", state.Control.CheckpointRedoPos)
	state.oldestRetained = logPosition(tb, "the control file's oldest retained position", state.Control.OldestRetainedPos)
	for f := range log.Frames() {
		pos := logPosition(tb, "a record's position", f.Pos)
		state.Frames = append(state.Frames, WALFrame{
			Pos:         pos,
			End:         pos + int64(wal.HeaderSizeV2) + int64(len(f.Payload)),
			Reservation: len(f.Payload) > 0 && f.Payload[0] == wal.ControlRecordTag,
		})
	}
	state.End = log.TailOffset()
	return state
}

// logPosition converts a log position, which the engine stores unsigned, to the
// int64 the log's own end (Log.TailOffset) is expressed in. A position beyond
// math.MaxInt64 cannot belong to a log whose end is an int64, so it fails the
// test as a log the inspection cannot represent rather than wrapping negative.
func logPosition(tb testing.TB, what string, pos uint64) int64 {
	tb.Helper()

	if pos > math.MaxInt64 {
		tb.Fatalf("inspecting the write-ahead log: %s %d exceeds the int64 range of the log's end", what, pos)
		return 0
	}
	return int64(pos)
}

// WALSegmentFiles lists the names of the segment files under graphDir's
// write-ahead-log segment directory, in order. It reads the directory and not
// any file's content or size, so a test can assert which whole segments a fold
// discarded.
func WALSegmentFiles(tb testing.TB, graphDir string) []string {
	tb.Helper()

	entries, err := os.ReadDir(wal.SegmentDir(filepath.Join(graphDir, "wal")))
	if err != nil {
		tb.Fatalf("listing the write-ahead-log segments under %s: %v", graphDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
