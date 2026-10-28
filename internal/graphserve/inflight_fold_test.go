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
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// testCadence is the cadence the servers below run under: a fold is due 20ms
// after the last one and the cadence looks every 5ms, so two due instants pass in
// tens of milliseconds rather than in minutes.
func testCadence() checkpointCadence {
	return checkpointCadence{maxAge: 20 * time.Millisecond, interval: 5 * time.Millisecond}
}

// storeState is every file under snapshot/ and every file of the write-ahead
// log — `wal`, `wal.control` and every segment under `wal.d/` — each as its
// content digest and its modification time, the two things acceptance criterion
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

	err := filepath.WalkDir(graphDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(graphDir, path)
		if relErr != nil {
			return relErr
		}
		top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if top != "snapshot" && top != "wal" && top != "wal.control" && top != "wal.d" {
			if d.IsDir() && path != graphDir {
				return filepath.SkipDir
			}
			return nil
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
// log's control file records as covered a position at or beyond `through`, or
// fails. Both are required: a fold counted before its control-file write reached
// the disk would otherwise let the test read a covered position short of the
// records it is about.
func waitForFold(t *testing.T, served realServer, graphDir string, through int64) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for served.closer.inFlight.attempts.Load() < 1 || testenv.InspectWAL(t, graphDir).Covered() < through ||
		!hasSnapshot(graphDir) {
		if time.Now().After(deadline) {
			t.Fatalf("no in-flight fold covered the log through position %d within 30s on a %v "+
				"cadence: %d fold(s) ran, %d due instant(s) passed, the control file records %d as "+
				"covered, snapshot present = %v", through, testCadence().maxAge,
				served.closer.inFlight.attempts.Load(), served.closer.inFlight.due.Load(),
				testenv.InspectWAL(t, graphDir).Covered(), hasSnapshot(graphDir))
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
// So does a gate that took the start of the log, rather than the position the
// snapshot covers, as its mark at open: the folded records are still in the
// log's one segment, because positions do not restart at a fold.
func TestIdleServer_WritesNothingToTheStore(t *testing.T) {
	root := graphRoot(t)

	seeder := startServerProcess(t, root, "seed")
	mustSend(t, seeder.socket, "UNWIND range(1,200) AS i CREATE (:Component {key:'svc-'+toString(i)})")
	seeder.signalAndWait(t, syscall.SIGINT)

	if !logFolded(t, seeder.graphDir) {
		t.Fatalf("the log holds records beyond the position the seeding server's shutdown fold " +
			"covered: the store this test starts from must hold nothing unfolded")
	}
	if logEnd(t, seeder.graphDir) == 0 {
		t.Fatalf("the seeded store's log holds no record, so it cannot tell a gate that reads the " +
			"snapshot's position from one that takes the start of the log as its mark")
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

	waitForFold(t, served, graphDir, logEnd(t, graphDir))
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

	tail := testenv.InspectWAL(t, killed.graphDir)
	if tail.End <= tail.Covered() {
		t.Fatalf("the killed server left a log ending at %d with %d covered; this test needs the "+
			"records of an acknowledged write that no fold covered", tail.End, tail.Covered())
	}
	if hasSnapshot(killed.graphDir) {
		t.Fatalf("the killed server published a snapshot before it was killed, so the tail this " +
			"test is about may already be folded")
	}

	served := startRealServerOver(t, killed.graphDir, filepath.Join(root, "next.sock"), testCadence(), discardLogger())
	defer served.stop()

	waitForFold(t, served, killed.graphDir, tail.End)

	// And the fold lost nothing: the acknowledged write is still there.
	result := mustSend(t, served.socket, "MATCH (d:Decision) RETURN d.key AS key")
	if keys := stringColumn(t, result); len(keys) != 1 || keys[0] != "acknowledged-before-the-kill" {
		t.Errorf("after folding the tail the graph holds %v, want exactly the acknowledged write", keys)
	}
}
