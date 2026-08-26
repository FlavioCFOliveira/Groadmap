// Regression fence for the graph read path's store lock
// (SPEC/GRAPH.md § Concurrency and Recovery; § What a Read Changes on Disk;
// Acceptance Criteria 31, 33, 34, 35, 36).
//
// Two properties matter more than "the lock is taken", because a test that only
// checked that the reader locks would still pass after the hold was widened to
// the whole read — which is the regression that costs the most and is the
// easiest to introduce (turning the explicit releaseLock() into a
// defer releaseLock() is a one-character change that looks tidier):
//
//  1. The lock is released AT THE OPEN. A read that is still in flight must not
//     fail a write issued after its open returned (AC 34).
//  2. Nothing in the store is read after the open. With the store directory
//     removed immediately after the open, the query must still return complete,
//     correct results — including state that reached memory only by replay of
//     the write-ahead-log tail (AC 35).
//
// Property 2 is the premise property 1 rests on. SPEC/GRAPH.md carries an
// anti-widening clause naming the four changes that would invalidate it; this
// file is the tripwire for all four, and if it ever goes red the correct
// response is to WIDEN the hold, not to weaken the test.
package commands

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"
	"github.com/FlavioCFOliveira/GoGraph/store/wal"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// testGraphDir resolves the roadmap's graph store directory the same way
// openGraphStore does.
func testGraphDir(t *testing.T, roadmap string) string {
	t.Helper()
	roadmapDir, err := utils.GetRoadmapDir(roadmap)
	if err != nil {
		t.Fatalf("resolving roadmap directory for %q: %v", roadmap, err)
	}
	return filepath.Join(roadmapDir, "graph")
}

// commitWithoutCheckpoint commits a write through GoGraph's transactional path
// but deliberately skips the checkpoint that runGraphWrite performs afterwards.
// The change is therefore durable in the write-ahead log and absent from the
// snapshot, which is the only way to produce a node that a later open can reach
// ONLY by replaying the log tail.
//
// It uses the same engine, store, codec and write-ahead-log entry points as
// runGraphWrite, minus checkpointGraph, so the state it leaves behind is a state
// the production write path genuinely produces — a write whose checkpoint failed
// after a durable commit is explicitly allowed to leave exactly this
// (SPEC/GRAPH.md FR7).
func commitWithoutCheckpoint(t *testing.T, graphDir, query string) {
	t.Helper()

	walPath := filepath.Join(graphDir, "wal")
	sizeBefore := fileSize(t, walPath)

	res, err := recovery.Open[string, float64](graphDir, graphReadOpts)
	if err != nil {
		t.Fatalf("opening the store for an uncheckpointed write: %v", err)
	}
	w, err := wal.Open(walPath)
	if err != nil {
		t.Fatalf("opening the write-ahead log: %v", err)
	}
	defer w.Close() //nolint:errcheck

	store := txn.NewStoreWithOptions[string, float64](res.Graph, w, txn.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})
	result, err := cypher.NewEngineWithStore(store).RunInTx(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("running the uncheckpointed write: %v", err)
	}
	// Draining the result is what allows Close to commit.
	for result.Next() {
	}
	if err := result.Err(); err != nil {
		t.Fatalf("iterating the uncheckpointed write: %v", err)
	}
	// Close is the durability boundary: it applies and commits the transaction.
	if err := result.Close(); err != nil {
		t.Fatalf("committing the uncheckpointed write: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("syncing the write-ahead log: %v", err)
	}

	// Non-vacuity: the caller depends on this state living ONLY in the log. The
	// preceding checkpoint truncated the log, so the log having grown is what
	// proves the change is in the tail and not already in the snapshot. Without
	// this check a change to the write path that started checkpointing here
	// would turn the WAL-tail assertion into a second snapshot assertion, and
	// nothing would say so.
	if sizeAfter := fileSize(t, walPath); sizeAfter <= sizeBefore {
		t.Fatalf("the write-ahead log did not grow (%d -> %d bytes); the change is not in the "+
			"log tail, so a test that relies on log replay would prove nothing", sizeBefore, sizeAfter)
	}
}

// fileSize returns the size of path, or 0 when it does not exist.
func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Size()
}

// TestGraphRead_StoreIsNotReadAfterTheOpen covers SPEC/GRAPH.md acceptance
// criterion 35, the premise the narrow lock hold depends on: the open returns a
// graph that is fully materialised in memory, so the query, the traversal and
// the serialisation that follow touch no file in the store.
//
// The store directory is destroyed — not merely made unwritable — in the gap
// between the open and the query, which is precisely where runGraphRead releases
// the shared lock. A lazily loaded snapshot component, a memory-mapped file, a
// handle kept open past recovery, or a re-read during iteration would all show
// up here as an error or as missing rows.
//
// The two assertions are chosen to exercise different parts of the loaded graph:
// a label scan reads node records, and a relationship traversal reads the
// adjacency structure. The WAL-tail node proves the log was replayed IN FULL
// during the open rather than referenced afterwards.
func TestGraphRead_StoreIsNotReadAfterTheOpen(t *testing.T) {
	const roadmap = "graph-read-open-premise"
	defer setupTestGraphRoadmap(t, roadmap)()

	// Checkpointed state: two nodes and a relationship, written through the
	// ordinary CLI write path, so they live in the snapshot.
	captureStdStreams(t, func() {
		if err := runGraphCreate([]string{"-r", roadmap, "--query",
			"CREATE (spec:Spec {key:'graph-concurrency'})-[:IMPLEMENTED_BY]->(comp:Component {key:'graphlock'})"}); err != nil {
			t.Fatalf("seeding the checkpointed state: %v", err)
		}
	})

	graphDir := testGraphDir(t, roadmap)

	// Write-ahead-log-tail state: committed durably, never checkpointed, so it
	// exists only in the log and can reach memory only by replay on open.
	commitWithoutCheckpoint(t, graphDir,
		"CREATE (t:Test {key:'wal-tail-only'})")

	// Phase one, exactly as runGraphRead performs it: take the shared lock, open
	// the store, release the lock. The lock is taken here too so the sequence
	// under test is the production sequence and not a shortcut.
	release, err := graphlock.AcquireShared(graphDir)
	if err != nil {
		t.Fatalf("taking the shared store lock: %v", err)
	}
	res, err := recovery.Open[string, float64](graphDir, graphReadOpts)
	release()
	if err != nil {
		t.Fatalf("opening the store: %v", err)
	}

	// The destructive probe: between the open and the query, the whole store
	// goes away. Anything the read still needed from disk is now unreachable.
	if err := os.RemoveAll(graphDir); err != nil {
		t.Fatalf("removing the store directory: %v", err)
	}
	if _, statErr := os.Stat(graphDir); !os.IsNotExist(statErr) {
		t.Fatalf("the store directory is still present, so the probe proves nothing: %v", statErr)
	}

	// Phase two, exactly as runGraphRead performs it: an engine over the graph
	// the open produced, with no lock held and no store on disk.
	engine := cypher.NewEngine(res.Graph)
	ctx := context.Background()

	t.Run("label scan sees snapshot state", func(t *testing.T) {
		keys := queryStrings(t, ctx, engine, "MATCH (s:Spec) RETURN s.key")
		if len(keys) != 1 || keys[0] != "graph-concurrency" {
			t.Errorf("label scan returned %v, want [graph-concurrency]; the snapshot was not "+
				"fully materialised at open", keys)
		}
	})

	t.Run("label scan sees write-ahead-log tail state", func(t *testing.T) {
		keys := queryStrings(t, ctx, engine, "MATCH (t:Test) RETURN t.key")
		if len(keys) != 1 || keys[0] != "wal-tail-only" {
			t.Errorf("label scan returned %v, want [wal-tail-only]; the write-ahead log was not "+
				"replayed in full during the open, so the lock must be held past it", keys)
		}
	})

	t.Run("relationship traversal sees the adjacency", func(t *testing.T) {
		keys := queryStrings(t, ctx, engine,
			"MATCH (:Spec {key:'graph-concurrency'})-[:IMPLEMENTED_BY]->(c:Component) RETURN c.key")
		if len(keys) != 1 || keys[0] != "graphlock" {
			t.Errorf("traversal returned %v, want [graphlock]; the adjacency structure was not "+
				"fully materialised at open", keys)
		}
	})
}

// queryStrings runs query through the engine's read path and returns the first
// column of every row as a string. It fails the test on any engine or
// serialisation error, so a read that broke because the store had gone is
// reported as such rather than as an empty result.
func queryStrings(t *testing.T, ctx context.Context, engine *cypher.Engine, query string) []string {
	t.Helper()

	result, err := engine.Run(ctx, query, nil)
	if err != nil {
		t.Fatalf("query %q failed after the store was removed: %v", query, err)
	}
	defer result.Close() //nolint:errcheck

	out, err := serializeGraphResult(result)
	if err != nil {
		t.Fatalf("serialising %q failed after the store was removed: %v", query, err)
	}

	values := make([]string, 0, len(out.Rows))
	for _, row := range out.Rows {
		if len(row) == 0 {
			t.Fatalf("query %q returned a row with no columns", query)
		}
		s, ok := row[0].(string)
		if !ok {
			t.Fatalf("query %q returned a non-string first column: %#v", query, row[0])
		}
		values = append(values, s)
	}
	return values
}

// bulkPayloadSize is the size of the property value used to make a read block on
// its own stdout. It must comfortably exceed the operating system's pipe buffer
// — 64 KiB on Linux, smaller elsewhere — so that the encoder's single Write
// cannot complete, which is what pins the read in flight deterministically
// instead of relying on a slow query and a timing window.
const bulkPayloadSize = 512 * 1024

// TestGraphRead_DoesNotHoldTheLockPastTheOpen covers SPEC/GRAPH.md acceptance
// criterion 34, the criterion that exists specifically to stop the reader's hold
// being silently widened back to the whole read: a read that is still in flight
// must NOT fail a concurrent `rmp graph create` issued after the read's open
// returned.
//
// The read is pinned in flight without any timing assumption. Its stdout is a
// pipe that the test does not drain, and its result is far larger than the
// pipe's buffer, so utils.PrintJSON blocks inside a single Write and the read
// cannot possibly return until the test chooses to drain it. Whatever the
// machine's speed, the read is demonstrably unfinished while the write runs.
//
// If the hold were widened — a defer releaseLock(), or a release moved after the
// query — the write below would fail with "graph store is busy" and this test
// would go red, which is exactly what should happen.
func TestGraphRead_DoesNotHoldTheLockPastTheOpen(t *testing.T) {
	const roadmap = "graph-read-narrow-hold"
	defer setupTestGraphRoadmap(t, roadmap)()

	// A payload large enough that the read's JSON cannot fit in a pipe buffer.
	payload := strings.Repeat("a", bulkPayloadSize)
	captureStdStreams(t, func() {
		if err := runGraphCreate([]string{"-r", roadmap, "--query",
			"CREATE (b:Component {key:'bulk-payload', payload:'" + payload + "'})"}); err != nil {
			t.Fatalf("seeding the bulk node: %v", err)
		}
	})

	readR, readW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdout := os.Stdout
	restored := false
	restore := func() {
		if !restored {
			os.Stdout = origStdout
			restored = true
		}
	}
	defer restore()
	os.Stdout = readW

	readErr := make(chan error, 1)
	go func() {
		readErr <- runGraphQuery([]string{"-r", roadmap, "--query",
			"MATCH (b:Component {key:'bulk-payload'}) RETURN b.payload"})
	}()

	// Consume a first chunk. A blocking read returns only once the reader has
	// actually written, which proves two things at once: the read is past its
	// store open, and it has already resolved os.Stdout — so the swap below
	// cannot race it. The remaining payload is far larger than the buffer, so
	// the read blocks again immediately.
	firstChunk := make([]byte, 4096)
	if _, err := readR.Read(firstChunk); err != nil {
		restore()
		t.Fatalf("reading the first chunk of the read's output: %v", err)
	}

	// The read cannot have finished: its output does not fit in the pipe.
	select {
	case err := <-readErr:
		restore()
		t.Fatalf("the read finished before the write was attempted (err=%v); the payload no longer "+
			"exceeds the pipe buffer, so this test proves nothing — enlarge bulkPayloadSize", err)
	default:
	}

	// Give the write its own stdout, which IS drained, so it cannot block on the
	// read's full pipe. The read already resolved os.Stdout, so this is safe.
	writeStdout, writeStdoutR, writeDone := captureToBuffer(t)
	os.Stdout = writeStdout

	// The property under test: a write issued while a read is in flight must
	// succeed. Under the defect this fixes, or under a widened hold, this fails
	// with utils.ErrDatabase.
	writeErr := runGraphCreate([]string{"-r", roadmap, "--query",
		"CREATE (t:Task {key:'written-during-a-read'})"})

	// The read must still be unfinished, or the write did not overlap it.
	stillRunning := false
	select {
	case err := <-readErr:
		readErr <- err
	default:
		stillRunning = true
	}

	restore()
	_ = writeStdout.Close()
	writeJSON := <-writeDone
	_ = writeStdoutR.Close()

	if writeErr != nil {
		t.Errorf("a write issued while a read was in flight failed: %v\n"+
			"The reader must hold the shared lock across the STORE OPEN ALONE and release it as "+
			"soon as the open returns (SPEC/GRAPH.md § Concurrency and Recovery). A hold that "+
			"spans the query blocks writers for as long as the query takes and buys no safety.", writeErr)
	}
	if !stillRunning {
		t.Error("the read had already finished when the write completed, so the write did not " +
			"overlap it and the test proves nothing")
	}
	if writeErr == nil && !strings.Contains(writeJSON, `"ok": true`) {
		t.Errorf("the write's stdout = %q, want the success JSON", writeJSON)
	}

	// Drain the rest of the read's output and confirm it completed correctly:
	// the write, its commit and its checkpoint all ran underneath it, and the
	// read still served the state it loaded at open time.
	rest := make(chan string, 1)
	go func() {
		var b strings.Builder
		b.Write(firstChunk)
		buf := make([]byte, 32*1024)
		for {
			n, rerr := readR.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if rerr != nil {
				break
			}
		}
		rest <- b.String()
	}()

	select {
	case err := <-readErr:
		if err != nil {
			t.Fatalf("the read failed: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the read never completed after its output was drained")
	}
	_ = readW.Close()
	stdout := <-rest
	_ = readR.Close()

	var parsed graphQueryResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("the read's stdout is not the columns/rows shape: %v", err)
	}
	if len(parsed.Rows) != 1 {
		t.Fatalf("the read returned %d rows, want 1", len(parsed.Rows))
	}
	got, ok := parsed.Rows[0][0].(string)
	if !ok || len(got) != bulkPayloadSize {
		t.Errorf("the read returned a payload of %d bytes (string=%t), want %d; a write that ran "+
			"underneath an in-flight read must not truncate or corrupt its result", len(got), ok, bulkPayloadSize)
	}

	// The write really committed, and is visible to a read that opens the store
	// afterwards — so the narrow hold did not cost the write its durability.
	stdoutAfter, _ := captureStdStreams(t, func() {
		if err := runGraphQuery([]string{"-r", roadmap, "--query",
			"MATCH (t:Task) RETURN t.key"}); err != nil {
			t.Errorf("reading back the write: %v", err)
		}
	})
	if !strings.Contains(stdoutAfter, "written-during-a-read") {
		t.Errorf("the node written during the read is not visible afterwards; stdout=%q", stdoutAfter)
	}
}

// captureToBuffer returns a pipe writer to install as os.Stdout, its reader, and
// a channel that yields everything written once the writer is closed. Unlike
// captureStdStreams it does not run a function, because the caller needs the
// stream installed across several statements.
func captureToBuffer(t *testing.T) (w, r *os.File, done chan string) {
	t.Helper()

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	done = make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, rerr := pr.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if rerr != nil {
				break
			}
		}
		done <- b.String()
	}()
	return pw, pr, done
}

// TestGraphRead_WaitsForAWriterRatherThanFailingFast covers SPEC/GRAPH.md
// acceptance criterion 33: while a write holds the exclusive lock, a concurrent
// read does not fail on the first collision — it waits, and succeeds once the
// writer releases.
//
// Reads are by far the more frequent operation, so a reader that failed fast
// would make ordinary reads intermittently unavailable beside any writer. The
// holder here is the lock itself rather than a real `rmp graph create`, because
// the test must control exactly when the lock is released.
func TestGraphRead_WaitsForAWriterRatherThanFailingFast(t *testing.T) {
	const roadmap = "graph-read-waits-for-writer"
	defer setupTestGraphRoadmap(t, roadmap)()

	captureStdStreams(t, func() {
		if err := runGraphCreate([]string{"-r", roadmap, "--query",
			"CREATE (s:Spec {key:'lock-contention'})"}); err != nil {
			t.Fatalf("seeding the graph: %v", err)
		}
	})
	graphDir := testGraphDir(t, roadmap)

	release, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		t.Fatalf("taking the exclusive lock: %v", err)
	}

	// Released well inside the reader's bounded wait, on a goroutine, so the
	// reader is genuinely contending when it starts.
	go func() {
		time.Sleep(250 * time.Millisecond)
		release()
	}()

	start := time.Now()
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphQuery([]string{"-r", roadmap, "--query",
			"MATCH (s:Spec) RETURN s.key"}); err != nil {
			t.Errorf("a read must WAIT for an in-flight writer, not fail on the first collision "+
				"(SPEC/GRAPH.md § Lock Contention rule 2): %v", err)
		}
	})
	elapsed := time.Since(start)

	if !strings.Contains(stdout, "lock-contention") {
		t.Errorf("the read returned %q, want it to contain the seeded key", stdout)
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("the read completed in %v; it cannot have waited for the writer, so it did not "+
			"take the shared lock at all", elapsed)
	}
}

// TestGraphRead_FailsAfterTheBoundedWait covers SPEC/GRAPH.md acceptance
// criterion 36 for the CLI half: a read that cannot take the shared lock within
// the bounded wait exits 1 rather than hanging. utils.ErrDatabase is the
// sentinel the exit-code mapping turns into 1.
func TestGraphRead_FailsAfterTheBoundedWait(t *testing.T) {
	const roadmap = "graph-read-bounded-wait"
	defer setupTestGraphRoadmap(t, roadmap)()

	captureStdStreams(t, func() {
		if err := runGraphCreate([]string{"-r", roadmap, "--query",
			"CREATE (s:Spec {key:'bounded-wait'})"}); err != nil {
			t.Fatalf("seeding the graph: %v", err)
		}
	})
	graphDir := testGraphDir(t, roadmap)

	release, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		t.Fatalf("taking the exclusive lock: %v", err)
	}
	defer release()

	done := make(chan error, 1)
	go func() {
		_, _ = captureStdStreams(t, func() {
			done <- runGraphQuery([]string{"-r", roadmap, "--query", "MATCH (s:Spec) RETURN s.key"})
		})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the read succeeded while a writer held the exclusive lock")
		}
		if !errors.Is(err, utils.ErrDatabase) {
			t.Errorf("an exhausted wait must surface as utils.ErrDatabase (exit 1), got: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the read never returned; the wait must be BOUNDED and end in a failure, " +
			"never an indefinite block (SPEC/GRAPH.md § Lock Contention rule 2)")
	}
}
