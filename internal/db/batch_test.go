package db

import (
	"context"
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestBatchProcessorDefaults verifies the constructor's zero/negative guard and
// the BatchSize accessor.
func TestBatchProcessorDefaults(t *testing.T) {
	if bp := NewBatchProcessor(0); bp.BatchSize() != 100 {
		t.Errorf("NewBatchProcessor(0).BatchSize() = %d, want 100 (default)", bp.BatchSize())
	}
	if bp := NewBatchProcessor(-5); bp.BatchSize() != 100 {
		t.Errorf("NewBatchProcessor(-5).BatchSize() = %d, want 100 (default)", bp.BatchSize())
	}
	if bp := NewBatchProcessor(250); bp.BatchSize() != 250 {
		t.Errorf("NewBatchProcessor(250).BatchSize() = %d, want 250", bp.BatchSize())
	}
}

// TestProcessChunksPartitioning verifies that ProcessChunks visits every id
// exactly once, in order, and partitions into chunks no larger than batchSize.
func TestProcessChunksPartitioning(t *testing.T) {
	bp := NewBatchProcessor(100)

	const total = 1000
	ids := make([]int, total)
	for i := range ids {
		ids[i] = i
	}

	var seen []int
	var chunkCount int
	err := bp.ProcessChunks(ids, func(chunk []int) error {
		if len(chunk) == 0 {
			t.Errorf("received empty chunk")
		}
		if len(chunk) > bp.BatchSize() {
			t.Errorf("chunk size %d exceeds batchSize %d", len(chunk), bp.BatchSize())
		}
		chunkCount++
		seen = append(seen, chunk...)
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessChunks returned error: %v", err)
	}
	wantChunks := (total + bp.BatchSize() - 1) / bp.BatchSize()
	if chunkCount != wantChunks {
		t.Errorf("visited %d chunks, want %d", chunkCount, wantChunks)
	}
	if len(seen) != total {
		t.Fatalf("visited %d ids, want %d", len(seen), total)
	}
	for i, v := range seen {
		if v != i {
			t.Fatalf("id order corrupted at index %d: got %d", i, v)
		}
	}
}

// TestProcessChunksEmpty verifies that an empty id set performs no work.
func TestProcessChunksEmpty(t *testing.T) {
	bp := NewBatchProcessor(100)
	called := false
	err := bp.ProcessChunks(nil, func(chunk []int) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Errorf("fn called for empty id set")
	}
}

// TestProcessChunksStopsOnError verifies that ProcessChunks stops at the first
// failing chunk and wraps the underlying error.
func TestProcessChunksStopsOnError(t *testing.T) {
	bp := NewBatchProcessor(10)
	ids := make([]int, 35) // 4 chunks: 10,10,10,5
	for i := range ids {
		ids[i] = i
	}

	sentinel := errors.New("chunk failure")
	var calls int
	err := bp.ProcessChunks(ids, func(chunk []int) error {
		calls++
		if calls == 2 {
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected to stop after 2 chunks, ran %d", calls)
	}
}

// TestProcessChunksWithResult verifies result accumulation and ordering.
func TestProcessChunksWithResult(t *testing.T) {
	ids := make([]int, 250)
	for i := range ids {
		ids[i] = i
	}

	results, err := ProcessChunksWithResult(ids, 100, func(chunk []int) ([]int, error) {
		// Echo each id doubled so we can verify both order and completeness.
		out := make([]int, len(chunk))
		for i, id := range chunk {
			out[i] = id * 2
		}
		return out, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != len(ids) {
		t.Fatalf("got %d results, want %d", len(results), len(ids))
	}
	for i, v := range results {
		if v != i*2 {
			t.Fatalf("result %d = %d, want %d", i, v, i*2)
		}
	}
}

// TestProcessChunksWithResultError verifies error propagation from a chunk.
func TestProcessChunksWithResultError(t *testing.T) {
	ids := []int{1, 2, 3, 4, 5}
	sentinel := errors.New("boom")
	_, err := ProcessChunksWithResult(ids, 2, func(chunk []int) ([]int, error) {
		return nil, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel, got %v", err)
	}
}

// TestAddTasksToSprintChunksALargeIDSet is the end-to-end proof that the
// chunked write path stays correct when a batch spans several chunks: it adds
// more tasks to one sprint than the BatchProcessor's chunk size, so the status
// update behind the membership change runs as several statements, and every
// task must still arrive in SPRINT with a membership row.
//
// The chunk size is the query cache's bucket size, not a limit workaround. The
// name this test used to carry said "beyond the variable limit" and the comment
// beneath it said 999; measured against the driver this module uses
// (modernc.org/sqlite), a statement takes 32766 bound parameters and refuses the
// 32767th, and an argv large enough to name that many ids does not fit in
// MAX_ARG_STRLEN. What chunking buys is a cached template per chunk size, which
// is what GetQuery can only answer for sizes it holds.
//
// It used to drive the property through UpdateTaskStatus, UpdateTaskPriority and
// UpdateTaskSeverity. Those were db-layer methods the command layer had replaced
// with its own single-statement transactions, so the batching they proved was
// batching nothing the binary runs; they are gone with them (task #188).
// AddTasksToSprint is the write path that still chunks, and this is the test of
// it.
func TestAddTasksToSprintChunksALargeIDSet(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Comfortably more than one chunk (the processor's batch size is 100), so
	// the status update is split and every chunk must execute for the
	// assertions below to hold.
	const n = 350
	ids := createBenchmarkTasks(t, db, n)

	sprintID := mustSeedSprint(t, db, &models.Sprint{
		Title:       "Absorb the March reconciliation backlog",
		Description: "One sprint holding every outstanding reconciliation task.",
		Status:      models.SprintPending,
		CreatedAt:   utils.NowISO8601(),
	})

	if err := db.AddTasksToSprint(context.Background(), sprintID, ids); err != nil {
		t.Fatalf("AddTasksToSprint over %d ids failed (chunking broken?): %v", n, err)
	}

	// Verify every task actually transitioned — proves all chunks executed.
	tasks, err := db.GetTasks(context.Background(), ids)
	if err != nil {
		t.Fatalf("GetTasks over %d ids failed: %v", n, err)
	}
	if len(tasks) != n {
		t.Fatalf("GetTasks returned %d tasks, want %d", len(tasks), n)
	}
	for _, task := range tasks {
		if task.Status != models.StatusSprint {
			t.Fatalf("task %d status = %q, want SPRINT", task.ID, task.Status)
		}
	}

	members, err := db.GetSprintTasks(context.Background(), sprintID)
	if err != nil {
		t.Fatalf("GetSprintTasks: %v", err)
	}
	if len(members) != n {
		t.Fatalf("sprint holds %d members, want %d", len(members), n)
	}
}
