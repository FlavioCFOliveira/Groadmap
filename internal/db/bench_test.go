package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// setupBenchDB creates a temporary database for benchmarking
func setupBenchDB(b *testing.B) (*sql.DB, func()) {
	b.Helper()

	tmpDir, err := os.MkdirTemp("", "groadmap_bench")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "bench.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		os.RemoveAll(tmpDir)
		b.Fatalf("Failed to open db: %v", err)
	}

	// Create the real production schema so the cached templates — which the
	// benchmarks exercise via QueryCache.GetQuery — run against the same table
	// shape as production (tasks columns + task_dependencies for OpGetTasks).
	schema := `
CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'BACKLOG',
    type TEXT NOT NULL DEFAULT 'TASK',
    functional_requirements TEXT NOT NULL,
    technical_requirements TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,
    created_at TEXT NOT NULL,
    specialists TEXT,
    started_at TEXT,
    tested_at TEXT,
    closed_at TEXT,
    completion_summary TEXT,
    parent_task_id INTEGER REFERENCES tasks(id),
    priority INTEGER NOT NULL DEFAULT 0,
    severity INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id INTEGER NOT NULL,
    depends_on_task_id INTEGER NOT NULL,
    PRIMARY KEY (task_id, depends_on_task_id)
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		b.Fatalf("Failed to create schema: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

// populateTasks inserts n tasks into the database
func populateTasks(b *testing.B, db *sql.DB, n int) []int {
	b.Helper()

	ids := make([]int, n)
	for i := 0; i < n; i++ {
		result, err := db.Exec(
			`INSERT INTO tasks (title, status, type, functional_requirements, technical_requirements, acceptance_criteria, created_at, priority, severity)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("Task %d", i), "BACKLOG", "TASK", "functional", "technical", "criteria", "2026-03-18T10:00:00Z", i%10, i%5,
		)
		if err != nil {
			b.Fatalf("Failed to insert task: %v", err)
		}
		id, _ := result.LastInsertId()
		ids[i] = int(id)
	}
	return ids
}

// BenchmarkQueryCache_CachedVsUncached compares cached query templates vs dynamic generation
func BenchmarkQueryCache_CachedVsUncached(b *testing.B) {
	qc := NewQueryCache()

	b.Run("Cached_10", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = qc.GetQuery(OpGetTasks, 10)
		}
	})

	b.Run("Cached_100", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = qc.GetQuery(OpGetTasks, 100)
		}
	})

	b.Run("Uncached_150", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = qc.GetQuery(OpGetTasks, 150)
		}
	})
}

// BenchmarkBatchProcessing measures batch chunking performance
func BenchmarkBatchProcessing(b *testing.B) {
	bp := NewBatchProcessor(100)

	sizes := []int{10, 100, 1000, 10000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			ids := make([]int, size)
			for i := range ids {
				ids[i] = i
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				count := 0
				err := bp.ProcessChunks(ids, func(chunk []int) error {
					count += len(chunk)
					return nil
				})
				if err != nil {
					b.Fatalf("ProcessChunks failed: %v", err)
				}
				if count != size {
					b.Fatalf("Expected %d items, got %d", size, count)
				}
			}
		})
	}
}

// BenchmarkGetTasks_CachedVsUncached compares GetTasks with and without query caching
func BenchmarkGetTasks_CachedVsUncached(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	// Populate with test data
	ids := populateTasks(b, db, 1000)

	qc := NewQueryCache()

	b.Run("Uncached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Build query dynamically (simulating old behavior)
			placeholders := make([]string, len(ids))
			args := make([]any, len(ids))
			for j, id := range ids {
				placeholders[j] = "?"
				args[j] = id
			}

			query := fmt.Sprintf(
				`SELECT t.id, t.title, t.status, t.type, t.functional_requirements, t.technical_requirements, t.acceptance_criteria,
				        t.created_at, t.specialists, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.parent_task_id,
				        t.priority, t.severity,
				        (SELECT COUNT(*) FROM tasks s WHERE s.parent_task_id = t.id) AS subtask_count`+taskDepsSelect+`
				 FROM tasks t WHERE t.id IN (%s) ORDER BY t.id`,
				strings.Join(placeholders, ","),
			)

			rows, err := db.QueryContext(context.Background(), query, args...)
			if err != nil {
				b.Fatalf("Query failed: %v", err)
			}
			rows.Close()
		}
	})

	b.Run("Cached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Use cached query template
			query := qc.GetQuery(OpGetTasks, len(ids))

			args := make([]any, len(ids))
			for j, id := range ids {
				args[j] = id
			}

			rows, err := db.QueryContext(context.Background(), query, args...)
			if err != nil {
				b.Fatalf("Query failed: %v", err)
			}
			rows.Close()
		}
	})
}

// BenchmarkBatchUpdate_CachedVsUncached compares batch updates with and without caching
func BenchmarkBatchUpdate_CachedVsUncached(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()

	// Populate with test data
	ids := populateTasks(b, db, 1000)

	qc := NewQueryCache()
	bp := NewBatchProcessor(100)

	b.Run("Uncached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Process without batching/caching
			placeholders := make([]string, len(ids))
			args := make([]any, len(ids)+1)
			args[0] = "DOING"
			for j, id := range ids {
				placeholders[j] = "?"
				args[j+1] = id
			}

			query := fmt.Sprintf(
				"UPDATE tasks SET status = ? WHERE id IN (%s)",
				strings.Join(placeholders, ","),
			)

			_, err := db.ExecContext(context.Background(), query, args...)
			if err != nil {
				b.Fatalf("Update failed: %v", err)
			}
		}
	})

	b.Run("Cached_WithBatching", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := bp.ProcessChunks(ids, func(chunk []int) error {
				// OpUpdateTaskStatus default shape: a single status parameter
				// precedes the IN-clause ids (mirrors (*DB).UpdateTaskStatus
				// for non-lifecycle transitions).
				query := qc.GetQuery(OpUpdateTaskStatus, len(chunk))

				args := make([]any, len(chunk)+1)
				args[0] = "DOING"
				for j, id := range chunk {
					args[j+1] = id
				}

				_, err := db.ExecContext(context.Background(), query, args...)
				return err
			})
			if err != nil {
				b.Fatalf("Batch update failed: %v", err)
			}
		}
	})
}

// BenchmarkPlaceholderGeneration measures placeholder string generation
func BenchmarkPlaceholderGeneration(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = generatePlaceholders(size)
			}
		})
	}
}

// BenchmarkQueryCache_Concurrent measures thread-safe concurrent access
func BenchmarkQueryCache_Concurrent(b *testing.B) {
	qc := NewQueryCache()

	b.RunParallel(func(pb *testing.PB) {
		size := 50
		for pb.Next() {
			_ = qc.GetQuery(OpGetTasks, size)
		}
	})
}

// BenchmarkBatchProcessing_Concurrent measures concurrent batch processing
func BenchmarkBatchProcessing_Concurrent(b *testing.B) {
	bp := NewBatchProcessor(100)
	ids := make([]int, 1000)
	for i := range ids {
		ids[i] = i
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = bp.ProcessChunks(ids, func(chunk []int) error {
				_ = len(chunk)
				return nil
			})
		}
	})
}
