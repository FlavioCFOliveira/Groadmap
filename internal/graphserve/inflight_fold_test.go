// Package graphserve — the in-flight fold against a real store, which is what
// rmp task #383 changed and what SPEC/GRAPH.md acceptance criteria 81, 82 and 83
// describe.
//
// # Why these run in process and over a short cadence
//
// Each criterion is about what a RUNNING server does between due instants of its
// cadence, and at the production cadence the first due instant is 75 seconds into
// the process. The cadence is a parameter of build for exactly this reason (see
// checkpointCadence), so the servers under test here are assembled in process
// with a cadence of milliseconds and are otherwise the production assembly: the
// same bind, the same build, the same gate. The stores they are given are made by
// the production server itself, in a child process, where a criterion is about a
// store another server left behind.
//
// # Why they wait on counters and not on time
//
// "At least two due instants have passed" is an EVENT, and the in-flight fold
// counts the event: due counts the instants at which the gate was consulted, and
// attempts the folds that ran. A test that slept for what it guessed was two
// cadences would pass vacuously on a machine that had not yet scheduled the
// ticker, which is the failure an idle-server test can least afford — it would
// report "nothing was written" about a server that had not yet had the chance.
package graphserve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
)

// testCadence is the cadence the servers below run under: a fold is due 20ms
// after the last one and the cadence looks every 5ms, so two due instants pass in
// tens of milliseconds rather than in minutes.
func testCadence() checkpointCadence {
	return checkpointCadence{maxAge: 20 * time.Millisecond, interval: 5 * time.Millisecond}
}

// storeState is every file under snapshot/ and the write-ahead log, each as its
// content digest and its modification time — the two things acceptance criterion
// 81 compares. The digest is what proves no byte changed; the modification time
// is what proves no file was REWRITTEN with the same bytes, which is what a fold
// that captured an unchanged graph does.
func storeState(t *testing.T, graphDir string) map[string]string {
	t.Helper()

	state := map[string]string{}
	record := func(path string) error {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path) //nolint:gosec // a path this test created in its own temporary directory
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(graphDir, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		state[rel] = fmt.Sprintf("%d bytes, sha256 %s, mtime %s",
			len(content), hex.EncodeToString(sum[:8]), info.ModTime().UTC().Format(time.RFC3339Nano))
		return nil
	}

	if err := record(filepath.Join(graphDir, "wal")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading the write-ahead log: %v", err)
	}
	err := filepath.WalkDir(filepath.Join(graphDir, "snapshot"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		return record(path)
	})
	if err != nil {
		t.Fatalf("reading the snapshot under %s: %v", graphDir, err)
	}
	return state
}

// hasSnapshot reports whether the store under graphDir carries a published
// snapshot, which a fold is the only thing to create.
func hasSnapshot(graphDir string) bool {
	_, err := os.Stat(filepath.Join(graphDir, "snapshot", "manifest.json"))
	return err == nil
}

// waitForFold blocks until the in-flight fold has folded at least once and the
// write-ahead log it truncated is empty, or fails. Both are required: a fold
// counted before its truncation reached the file would otherwise let the test
// read a log still holding the frames.
func waitForFold(t *testing.T, served realServer, graphDir string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for served.closer.inFlight.attempts.Load() < 1 || walBytesAt(graphDir) != 0 || !hasSnapshot(graphDir) {
		if time.Now().After(deadline) {
			t.Fatalf("no in-flight fold emptied the write-ahead log within 30s on a %v cadence: "+
				"%d fold(s) ran, %d due instant(s) passed, the log holds %d bytes, snapshot "+
				"present = %v", testCadence().maxAge, served.closer.inFlight.attempts.Load(),
				served.closer.inFlight.due.Load(), walBytesAt(graphDir), hasSnapshot(graphDir))
		}
		time.Sleep(intervalFloor)
	}
}

// TestIdleServer_WritesNothingToTheStore is acceptance criterion 81.
//
// The store is one the production server made and folded at its own shutdown, so
// the log holds nothing unfolded. A second server over it, with no client and no
// statement, must leave every file under snapshot/ and the log byte for byte and
// timestamp for timestamp as it found them across at least two due instants of
// its cadence — and it is compared WHILE that server runs, because a comparison
// after its shutdown would also observe the shutdown checkpoint.
//
// The engine's own loop, which drove the in-flight fold before rmp task #383,
// rewrote the whole snapshot at the first due instant and at every one after it,
// whether or not anything had been written: this is the test that fails on it.
func TestIdleServer_WritesNothingToTheStore(t *testing.T) {
	root := graphRoot(t)

	seeder := startServerProcess(t, root, "seed")
	mustSend(t, seeder.socket, "UNWIND range(1,200) AS i CREATE (:Component {key:'svc-'+toString(i)})")
	seeder.signalAndWait(t, syscall.SIGINT)

	if size := walSize(t, seeder.walPath()); size != 0 {
		t.Fatalf("the write-ahead log holds %d bytes after the seeding server's shutdown, want 0: "+
			"the store this test starts from must hold nothing unfolded", size)
	}
	if !hasSnapshot(seeder.graphDir) {
		t.Fatalf("the seeding server's shutdown published no snapshot under %s", seeder.graphDir)
	}
	before := storeState(t, seeder.graphDir)

	const dueInstants = 3
	served := startRealServerOver(t, seeder.graphDir, filepath.Join(root, "idle.sock"), testCadence(), discardLogger())
	defer served.stop()

	waitForCount(t, &served.closer.inFlight.due, dueInstants, "the due instants")

	after := storeState(t, seeder.graphDir)
	if diff := describeFingerprintDiff(before, after); diff != "" {
		t.Errorf("an idle server CHANGED the store across %d due instants of its cadence, with no "+
			"client connected and no statement sent:\n%s\nThe log held nothing unfolded, so no "+
			"fold was owed at any of them (SPEC/GRAPH.md § Durability and Checkpointing in a "+
			"Long-Lived Process, rules 9 and 10)", served.closer.inFlight.due.Load(), diff)
	}
	if got := served.closer.inFlight.attempts.Load(); got != 0 {
		t.Errorf("the idle server folded %d time(s); the gate owed it none", got)
	}
}

// TestServedWrite_IsFoldedWhileTheServerRuns is acceptance criterion 82: one
// write, and nothing else, is folded by the next due instant, and the fold is
// asserted while the server still runs. A test that stopped the server first
// would observe the shutdown checkpoint and pass a gate that withheld every
// in-flight fold.
func TestServedWrite_IsFoldedWhileTheServerRuns(t *testing.T) {
	root := graphRoot(t)
	graphDir := filepath.Join(root, "graph")

	served := startRealServerOver(t, graphDir, filepath.Join(root, "write.sock"), testCadence(), discardLogger())
	defer served.stop()

	// The fresh store owes nothing, so every due instant so far was withheld.
	waitForCount(t, &served.closer.inFlight.due, 2, "the due instants before the write")
	if got := served.closer.inFlight.attempts.Load(); got != 0 {
		t.Fatalf("the server folded %d time(s) over an empty store before any write", got)
	}
	if hasSnapshot(graphDir) {
		t.Fatalf("a snapshot exists under %s before any write was sent", graphDir)
	}

	if _, err := graphclient.Send(context.Background(), served.socket, "CREATE (s:Spec {key:'k'})"); err != nil {
		t.Fatalf("the write failed: %v", err)
	}

	waitForFold(t, served, graphDir)
}

// TestKilledServersTail_IsFoldedByTheNextServerWhileIdle is acceptance criterion
// 83, and the owner's decision on rmp task #383 that a tail present at open
// counts as not yet folded.
//
// The production server acknowledges a write and is killed outright, before any
// fold covers it — at the production cadence nothing folds for 75 seconds — so
// the log is not empty. A new server over that store receives no statement and no
// connection, and must fold the tail at its first due instant, while still
// running and idle. A server that took the log's length at open as already folded
// would fold it neither in flight nor at shutdown; one that is sent a write first
// would fold on that write's account, which is why nothing is sent until the
// fold has been observed.
func TestKilledServersTail_IsFoldedByTheNextServerWhileIdle(t *testing.T) {
	root := graphRoot(t)

	killed := startServerProcess(t, root, "killed")
	mustSend(t, killed.socket, "CREATE (:Decision {key:'acknowledged-before-the-kill'})")
	killed.killAndWait(t)

	if size := walSize(t, killed.walPath()); size <= 0 {
		t.Fatalf("the killed server left a write-ahead log of %d bytes; this test needs the tail "+
			"of an acknowledged write that no fold covered", size)
	}
	if hasSnapshot(killed.graphDir) {
		t.Fatalf("the killed server published a snapshot before it was killed, so the tail this " +
			"test is about may already be folded")
	}

	served := startRealServerOver(t, killed.graphDir, filepath.Join(root, "next.sock"), testCadence(), discardLogger())
	defer served.stop()

	waitForFold(t, served, killed.graphDir)

	// And the fold lost nothing: the acknowledged write is still there.
	result := mustSend(t, served.socket, "MATCH (d:Decision) RETURN d.key AS key")
	if keys := stringColumn(t, result); len(keys) != 1 || keys[0] != "acknowledged-before-the-kill" {
		t.Errorf("after folding the tail the graph holds %v, want exactly the acknowledged write", keys)
	}
}
