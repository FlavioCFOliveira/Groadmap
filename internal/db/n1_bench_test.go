package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// BenchmarkGetSprint_N1Optimization benchmarks the N+1 query optimization.
// This verifies TASK-P012: Optimize Sprint Tasks N+1 Query.
// Uses single query with JSON aggregation vs separate queries.
func BenchmarkGetSprint_N1Optimization(b *testing.B) {
	// Test with different numbers of tasks in sprint
	for _, numTasks := range []int{0, 10, 50, 100} {
		b.Run(fmt.Sprintf("Tasks%d", numTasks), func(b *testing.B) {
			sqlDB, cleanup := setupBenchDB(b)
			defer cleanup()

			db := &DB{DB: sqlDB, roadmapName: "bench", queryCache: NewQueryCache(), batchProc: NewBatchProcessor(100)}
			if err := db.CreateSchema(); err != nil {
				b.Fatalf("Failed to create schema: %v", err)
			}

			ctx := context.Background()

			// Create sprint
			sprint := &models.Sprint{
				Status:      models.SprintPending,
				Description: "Test Sprint",
				CreatedAt:   "2026-03-18T10:00:00.000Z",
			}
			sprintID, err := db.CreateSprint(ctx, sprint)
			if err != nil {
				b.Fatalf("Failed to create sprint: %v", err)
			}

			// Create tasks and add to sprint
			var taskIDs []int
			for i := 0; i < numTasks; i++ {
				task := &models.Task{
					Priority:               5,
					Severity:               3,
					Status:                 models.StatusSprint,
					Title:                  fmt.Sprintf("Task %d", i),
					FunctionalRequirements: "Test functional requirements",
					TechnicalRequirements:  "Test technical requirements",
					AcceptanceCriteria:     "Test acceptance criteria",
					CreatedAt:              "2026-03-18T10:00:00.000Z",
				}
				taskID, err := db.CreateTask(ctx, task)
				if err != nil {
					b.Fatalf("Failed to create task: %v", err)
				}
				taskIDs = append(taskIDs, taskID)
			}

			// Add tasks to sprint
			if len(taskIDs) > 0 {
				if err := db.AddTasksToSprint(ctx, sprintID, taskIDs); err != nil {
					b.Fatalf("Failed to add tasks to sprint: %v", err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := db.GetSprint(ctx, sprintID)
				if err != nil {
					b.Fatalf("Failed to get sprint: %v", err)
				}
			}
		})
	}
}

// BenchmarkGetSprint_SimulatedOldApproach simulates the old N+1 approach
// by making separate queries for sprint and tasks.
func BenchmarkGetSprint_SimulatedOldApproach(b *testing.B) {
	for _, numTasks := range []int{0, 10, 50, 100} {
		b.Run(fmt.Sprintf("Tasks%d", numTasks), func(b *testing.B) {
			sqlDB, cleanup := setupBenchDB(b)
			defer cleanup()

			db := &DB{DB: sqlDB, roadmapName: "bench", queryCache: NewQueryCache(), batchProc: NewBatchProcessor(100)}
			if err := db.CreateSchema(); err != nil {
				b.Fatalf("Failed to create schema: %v", err)
			}

			ctx := context.Background()

			// Create sprint
			sprint := &models.Sprint{
				Status:      models.SprintPending,
				Description: "Test Sprint",
				CreatedAt:   "2026-03-18T10:00:00.000Z",
			}
			sprintID, err := db.CreateSprint(ctx, sprint)
			if err != nil {
				b.Fatalf("Failed to create sprint: %v", err)
			}

			// Create tasks and add to sprint
			var taskIDs []int
			for i := 0; i < numTasks; i++ {
				task := &models.Task{
					Priority:               5,
					Severity:               3,
					Status:                 models.StatusSprint,
					Title:                  fmt.Sprintf("Task %d", i),
					FunctionalRequirements: "Test functional requirements",
					TechnicalRequirements:  "Test technical requirements",
					AcceptanceCriteria:     "Test acceptance criteria",
					CreatedAt:              "2026-03-18T10:00:00.000Z",
				}
				taskID, err := db.CreateTask(ctx, task)
				if err != nil {
					b.Fatalf("Failed to create task: %v", err)
				}
				taskIDs = append(taskIDs, taskID)
			}

			if len(taskIDs) > 0 {
				if err := db.AddTasksToSprint(ctx, sprintID, taskIDs); err != nil {
					b.Fatalf("Failed to add tasks to sprint: %v", err)
				}
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				// Simulate old N+1 approach: query sprint, then query tasks separately
				var startedAt, closedAt sql.NullString
				var sprint models.Sprint
				err := db.QueryRowContext(ctx,
					`SELECT id, status, description, created_at, started_at, closed_at FROM sprints WHERE id = ?`,
					sprintID,
				).Scan(&sprint.ID, &sprint.Status, &sprint.Description, &sprint.CreatedAt, &startedAt, &closedAt)
				if err != nil {
					b.Fatalf("Failed to get sprint: %v", err)
				}

				// Second query for tasks (N+1 pattern)
				tasks, err := db.GetSprintTasks(ctx, sprintID)
				if err != nil {
					b.Fatalf("Failed to get sprint tasks: %v", err)
				}
				sprint.Tasks = tasks
				sprint.TaskCount = len(tasks)
			}
		})
	}
}
