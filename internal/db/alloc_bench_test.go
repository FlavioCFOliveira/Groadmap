package db

import (
	"fmt"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// BenchmarkScanTasksAllocations benchmarks the memory allocations of scanTasks.
// This verifies TASK-P005: Optimize scanTasks Memory Allocations.
func BenchmarkScanTasksAllocations(b *testing.B) {
	sqlDB, cleanup := setupBenchDB(b)
	defer cleanup()

	// Create wrapper DB with schema
	db := &DB{DB: sqlDB, roadmapName: "bench"}
	if err := db.CreateSchema(); err != nil {
		b.Fatalf("Failed to create schema: %v", err)
	}

	ctx := testContext()
	// Create 100 tasks (max limit for ListTasks)
	for i := 0; i < 100; i++ {
		task := &models.Task{
			Priority:               5,
			Severity:               3,
			Status:                 models.StatusBacklog,
			Title:                  "Test task title",
			FunctionalRequirements: "Test functional requirements",
			TechnicalRequirements:  "Test technical requirements",
			AcceptanceCriteria:     "Test acceptance criteria",
			CreatedAt:              "2026-03-18T10:00:00.000Z",
		}
		_, err := db.CreateTask(ctx, task)
		if err != nil {
			b.Fatalf("Failed to create task: %v", err)
		}
	}

	b.Run("Scan100Tasks", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			tasks, err := db.ListTasks(ctx, TaskListFilter{Limit: 100})
			if err != nil {
				b.Fatalf("Failed to list tasks: %v", err)
			}
			if len(tasks) != 100 {
				b.Fatalf("Expected 100 tasks, got %d", len(tasks))
			}
		}
	})
}

// BenchmarkScanTasksMemoryProfile profiles memory usage for large result sets.
func BenchmarkScanTasksMemoryProfile(b *testing.B) {
	// Test with different batch sizes (max 100 due to ListTasks limit)
	for _, numTasks := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("Tasks%d", numTasks), func(b *testing.B) {
			sqlDB, cleanup := setupBenchDB(b)
			defer cleanup()

			db := &DB{DB: sqlDB, roadmapName: "bench"}
			if err := db.CreateSchema(); err != nil {
				b.Fatalf("Failed to create schema: %v", err)
			}

			ctx := testContext()
			for i := 0; i < numTasks; i++ {
				task := &models.Task{
					Priority:               5,
					Severity:               3,
					Status:                 models.StatusBacklog,
					Title:                  "Test task title",
					FunctionalRequirements: "Test functional requirements",
					TechnicalRequirements:  "Test technical requirements",
					AcceptanceCriteria:     "Test acceptance criteria",
					CreatedAt:              "2026-03-18T10:00:00.000Z",
				}
				_, err := db.CreateTask(ctx, task)
				if err != nil {
					b.Fatalf("Failed to create task: %v", err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				tasks, err := db.ListTasks(ctx, TaskListFilter{Limit: numTasks})
				if err != nil {
					b.Fatalf("Failed to list tasks: %v", err)
				}
				if len(tasks) != numTasks {
					b.Fatalf("Expected %d tasks, got %d", numTasks, len(tasks))
				}
			}
		})
	}
}
