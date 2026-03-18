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

	// Create schema
	schema := `
CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    priority INTEGER NOT NULL DEFAULT 0,
    severity INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'BACKLOG',
    description TEXT NOT NULL,
    specialists TEXT,
    action TEXT NOT NULL,
    expected_result TEXT NOT NULL,
    created_at TEXT NOT NULL,
    completed_at TEXT
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
			"INSERT INTO tasks (priority, severity, status, description, action, expected_result, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			i%10, i%5, "BACKLOG", fmt.Sprintf("Task %d", i), "action", "result", "2026-03-18T10:00:00Z",
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
			args := make([]interface{}, len(ids))
			for j, id := range ids {
				placeholders[j] = "?"
				args[j] = id
			}

			query := fmt.Sprintf(
				`SELECT id, priority, severity, status, description, specialists, action, expected_result, created_at, completed_at
				 FROM tasks WHERE id IN (%s) ORDER BY id`,
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

			args := make([]interface{}, len(ids))
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
			args := make([]interface{}, len(ids)+1)
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
				query := qc.GetQuery(OpUpdateTaskStatus, len(chunk))

				args := make([]interface{}, len(chunk)+2)
				args[0] = "DOING"
				args[1] = nil // completed_at
				for j, id := range chunk {
					args[j+2] = id
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
