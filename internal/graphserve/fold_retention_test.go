// Package graphserve — SPEC/GRAPH.md acceptance criterion 14: after a statement
// that wrote and its checkpoint, the write-ahead log is bounded, read through
// the engine's own inspection of the log rather than from a file's size.
package graphserve

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/store/wal"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// TestShutdownFold_BoundsTheLogInWholeSegments is acceptance criterion 14.
//
// A log shorter than one segment is not shortened by a fold at all, so the
// criterion is driven with enough writes to fill more than one segment: release
// notes for a knowledge graph, each node carrying a kilobyte of text, written
// until the log's end lies beyond the engine's segment size. The shutdown fold
// then records in the control file a covered position at or beyond the last
// record of those writes, and the log no longer holds the first segment, every
// record of which lies below it (SPEC/GRAPH.md § Synchronous Checkpoint on Write,
// step 3).
func TestShutdownFold_BoundsTheLogInWholeSegments(t *testing.T) {
	root := t.TempDir()
	graphDir := filepath.Join(root, "graph")
	if err := os.MkdirAll(graphDir, 0o700); err != nil {
		t.Fatal(err)
	}

	served := startRealServerOver(t, graphDir, filepath.Join(root, "graph.sock"), checkpointCadence{}, discardLogger())
	defer served.stop()

	note := strings.Repeat("The release folds the write-ahead log into the snapshot. ", 18)
	for batch := 0; logEnd(t, graphDir) <= wal.DefaultSegmentSize; batch++ {
		statement := "UNWIND range(1, 2000) AS i CREATE (:ReleaseNote {batch: " + itoa(batch) +
			", seq: i, body: '" + note + "'})"
		if _, err := graphclient.Send(context.Background(), served.socket, statement); err != nil {
			t.Fatalf("writing batch %d of release notes: %v", batch, err)
		}
	}
	// The engine rolls a full segment over at the next transaction boundary, so
	// one more write is what puts a record in a second segment.
	if _, err := graphclient.Send(context.Background(), served.socket,
		"CREATE (:ReleaseNote {batch: -1, seq: 0, body: 'the record that opens the second segment'})"); err != nil {
		t.Fatalf("writing the record that opens the second segment: %v", err)
	}
	written := logEnd(t, graphDir)
	before := testenv.WALSegmentFiles(t, graphDir)
	if !slices.Contains(before, "0000000000000001.wal") || len(before) < 3 {
		t.Fatalf("the log spans the segments %v; the criterion needs more than one segment of records", before)
	}

	served.stop()

	state := testenv.InspectWAL(t, graphDir)
	if state.Covered() < written {
		t.Errorf("the control file records position %d as covered, short of the %d the writes reached",
			state.Covered(), written)
	}
	if state.OldestRetained() == 0 || state.OldestRetained() > state.Covered() {
		t.Errorf("the control file records the log as retained from position %d with %d covered; the fold "+
			"must discard the whole segments below the covered position", state.OldestRetained(), state.Covered())
	}
	after := testenv.WALSegmentFiles(t, graphDir)
	if slices.Contains(after, "0000000000000001.wal") {
		t.Errorf("the log still holds its first segment (%v), every record of which lies below the "+
			"covered position %d", after, state.Covered())
	}
	if len(after) >= len(before) {
		t.Errorf("the log holds the segments %v after the fold and held %v before it; a fold discards "+
			"the segments wholly below the position it covers", after, before)
	}
	if len(state.Frames) > 0 && state.Frames[0].Pos > state.OldestRetained() {
		t.Errorf("the first retained record lies at %d, beyond the retained-from position %d",
			state.Frames[0].Pos, state.OldestRetained())
	}
}
