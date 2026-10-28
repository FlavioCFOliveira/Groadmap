// Package graphserve — reading the write-ahead log the way the specification
// requires a test to read it: through the engine's own inspection, in log
// positions, and never from a file's size.
//
// The log is a sequence of segments whose records carry positions that only
// grow. A fold records in the log's control file the position its snapshot
// covers and discards only whole segments below it, so a log shorter than one
// segment is not shortened by a fold at all, and the single-file `wal` beside
// the segments is a fixed one-record stub (SPEC/GRAPH.md § Synchronous
// Checkpoint on Write, step 3; § Persistence Layout). "The log was folded" is
// therefore "the control file records as covered a position at or beyond the
// last record", and "the log did not change" is "every file of the log is byte
// for byte what it was".
package graphserve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// logFiles are the entries of the graph directory that make up the write-ahead
// log: the single-file log or its stub, the control file, and the segment
// directory (SPEC/GRAPH.md § Persistence Layout).
var logFiles = []string{"wal", "wal.control", "wal.d"}

// logFingerprint is every file of the write-ahead log under graphDir, each as its
// length and content digest, keyed by its path relative to graphDir.
func logFingerprint(t *testing.T, graphDir string) map[string]string {
	t.Helper()

	state := map[string]string{}
	for _, name := range logFiles {
		root := filepath.Join(graphDir, name)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			content, err := os.ReadFile(path) //nolint:gosec // a path inside this test's own temporary directory
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(graphDir, path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(content)
			state[rel] = fmt.Sprintf("%d bytes, sha256 %s", len(content), hex.EncodeToString(sum[:8]))
			return nil
		})
		if err != nil {
			t.Fatalf("fingerprinting the write-ahead log under %s: %v", graphDir, err)
		}
	}
	return state
}

// describeLogDiff names every file of the log whose fingerprint differs.
func describeLogDiff(before, after map[string]string) string {
	var lines []string
	for k, b := range before {
		if a, ok := after[k]; !ok {
			lines = append(lines, "removed "+k)
		} else if a != b {
			lines = append(lines, fmt.Sprintf("changed %s: %s -> %s", k, b, a))
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			lines = append(lines, "added "+k)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// logEnd is the position just past the last durable record of the write-ahead
// log under graphDir, as the engine's inspection reads it from outside the
// process that writes it.
func logEnd(t *testing.T, graphDir string) int64 {
	t.Helper()
	return testenv.InspectWAL(t, graphDir).End
}

// logFolded reports whether the control file of the log under graphDir records
// as covered a position at or beyond every record the log holds: the state a
// fold leaves behind when nothing was appended after its capture.
func logFolded(t *testing.T, graphDir string) bool {
	t.Helper()
	state := testenv.InspectWAL(t, graphDir)
	return state.HasControl && state.Covered() >= state.End
}

// requireLogFolded fails the test unless the log under graphDir is folded up to
// its end and a snapshot manifest exists, naming what was being asserted.
func requireLogFolded(t *testing.T, graphDir, what string) {
	t.Helper()

	state := testenv.InspectWAL(t, graphDir)
	if !state.HasControl || state.Covered() < state.End {
		t.Errorf("%s: the log's control file records position %d as covered and the log ends at %d; "+
			"a fold covers every record the log held when it was taken (SPEC/GRAPH.md § Synchronous "+
			"Checkpoint on Write, step 3)", what, state.Covered(), state.End)
	}
	if !hasSnapshot(graphDir) {
		t.Errorf("%s: no snapshot manifest under %s, so the log was folded into nothing", what, graphDir)
	}
}
