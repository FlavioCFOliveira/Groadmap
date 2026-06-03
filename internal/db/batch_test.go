package db

import (
	"context"
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
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

// TestCalculateBatches verifies batch-count arithmetic across exact multiples,
// remainders, the empty set, and a single item.
func TestCalculateBatches(t *testing.T) {
	bp := NewBatchProcessor(100)
	cases := []struct {
		total int
		want  int
	}{
		{0, 0},
		{1, 1},
		{99, 1},
		{100, 1},
		{101, 2},
		{200, 2},
		{201, 3},
		{1000, 10},
		{1001, 11},
	}
	for _, c := range cases {
		if got := bp.CalculateBatches(c.total); got != c.want {
			t.Errorf("CalculateBatches(%d) = %d, want %d", c.total, got, c.want)
		}
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
	if chunkCount != bp.CalculateBatches(total) {
		t.Errorf("visited %d chunks, want %d", chunkCount, bp.CalculateBatches(total))
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

// TestUpdateTaskStatusBatchesBeyondVariableLimit is the end-to-end proof that
// batching keeps large id sets within SQLite's variable limit
// (SQLITE_LIMIT_VARIABLE_NUMBER, default 999). It creates more than 999 tasks
// and updates them in a single UpdateTaskStatus call; without the BatchProcessor
// chunking the IN clause this would exceed the limit and fail.
func TestUpdateTaskStatusBatchesBeyondVariableLimit(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const n = 1500 // comfortably above the 999 variable limit
	ids := createBenchmarkTasks(t, db, n)

	// A lifecycle transition (DOING) so the batched path also exercises a
	// template with a leading bound parameter beyond the ids.
	if err := db.UpdateTaskStatus(context.Background(), ids, models.StatusDoing); err != nil {
		t.Fatalf("UpdateTaskStatus over %d ids failed (batching broken?): %v", n, err)
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
		if task.Status != models.StatusDoing {
			t.Fatalf("task %d status = %q, want DOING", task.ID, task.Status)
		}
		if task.StartedAt == nil {
			t.Errorf("task %d: started_at not set on DOING transition", task.ID)
		}
	}
}

// TestUpdateTaskPriorityAndSeverityBatch confirms the priority/severity batch
// paths also chunk correctly beyond the variable limit and apply to every task.
func TestUpdateTaskPriorityAndSeverityBatch(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const n = 1200
	ids := createBenchmarkTasks(t, db, n)

	if err := db.UpdateTaskPriority(context.Background(), ids, 7); err != nil {
		t.Fatalf("UpdateTaskPriority over %d ids failed: %v", n, err)
	}
	if err := db.UpdateTaskSeverity(context.Background(), ids, 4); err != nil {
		t.Fatalf("UpdateTaskSeverity over %d ids failed: %v", n, err)
	}

	tasks, err := db.GetTasks(context.Background(), ids)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	for _, task := range tasks {
		if task.Priority != 7 {
			t.Fatalf("task %d priority = %d, want 7", task.ID, task.Priority)
		}
		if task.Severity != 4 {
			t.Fatalf("task %d severity = %d, want 4", task.ID, task.Severity)
		}
	}
}
