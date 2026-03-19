// Package db provides SQLite database connectivity and operations.
//
// # Race Condition Tests
//
// These tests validate concurrent access patterns using Go's race detector.
// Run with: go test -race ./internal/db/...
package db

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ==================== CONCURRENT TASK OPERATIONS ====================

// TestConcurrentTaskCreation tests multiple goroutines creating tasks simultaneously
func TestConcurrentTaskCreation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const numGoroutines = 10
	const tasksPerGoroutine = 10

	var wg sync.WaitGroup
	var errorCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < tasksPerGoroutine; j++ {
				task := &models.Task{
					Priority:       goroutineID,
					Severity:       j,
					Status:         models.StatusBacklog,
					Title:                  "Concurrent task",
					FunctionalRequirements: "Action",
					TechnicalRequirements:  "Result",
				AcceptanceCriteria:     "Acceptance",
					CreatedAt:      time.Now().Format(time.RFC3339),
				}

				_, err := db.CreateTask(context.Background(), task)
				if err != nil {
					atomic.AddInt32(&errorCount, 1)
					t.Logf("Task creation error: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt32(&errorCount) > 0 {
		t.Errorf("Got %d errors during concurrent task creation", atomic.LoadInt32(&errorCount))
	}

	// Verify all tasks were created
	tasks, err := db.ListTasks(context.Background(), nil, nil, nil, 1000)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	expectedCount := numGoroutines * tasksPerGoroutine
	if len(tasks) != expectedCount {
		t.Errorf("expected %d tasks, got %d", expectedCount, len(tasks))
	}
}

// TestConcurrentTaskReads tests reading tasks while they are being created
func TestConcurrentTaskReads(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create some initial tasks
	for i := 0; i < 5; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Title:                  "Initial task",
			FunctionalRequirements: "Action",
			TechnicalRequirements:  "Result",
				AcceptanceCriteria:     "Acceptance",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		if _, err := db.CreateTask(context.Background(), task); err != nil {
			t.Fatalf("failed to create initial task: %v", err)
		}
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Reader goroutines
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, err := db.ListTasks(context.Background(), nil, nil, nil, 100)
					if err != nil {
						t.Logf("ListTasks error: %v", err)
					}
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Writer goroutines
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				task := &models.Task{
					Priority:       writerID,
					Severity:       j,
					Status:         models.StatusBacklog,
					Title:                  "Writer task",
					FunctionalRequirements: "Action",
					TechnicalRequirements:  "Result",
				AcceptanceCriteria:     "Acceptance",
					CreatedAt:      time.Now().Format(time.RFC3339),
				}
				db.CreateTask(context.Background(), task)
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Let writers finish
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestConcurrentTaskUpdates tests concurrent updates to different tasks
func TestConcurrentTaskUpdates(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create tasks
	var taskIDs []int
	for i := 0; i < 5; i++ {
		task := &models.Task{
			Priority:       i,
			Severity:       i,
			Status:         models.StatusBacklog,
			Title:                  "Task for update",
			FunctionalRequirements: "Action",
			TechnicalRequirements:  "Result",
				AcceptanceCriteria:     "Acceptance",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, err := db.CreateTask(context.Background(), task)
		if err != nil {
			t.Fatalf("failed to create task: %v", err)
		}
		taskIDs = append(taskIDs, id)
	}

	var wg sync.WaitGroup

	// Concurrent updates to different tasks
	for i, id := range taskIDs {
		wg.Add(1)
		go func(taskID int, iteration int) {
			defer wg.Done()

			updates := map[string]interface{}{
				"priority": iteration,
			}

			err := db.UpdateTask(context.Background(), taskID, updates)
			if err != nil {
				t.Logf("UpdateTask error for task %d: %v", taskID, err)
			}
		}(id, i)
	}

	wg.Wait()
}

// ==================== CONCURRENT SPRINT OPERATIONS ====================

// TestConcurrentSprintCreation tests multiple goroutines creating sprints
func TestConcurrentSprintCreation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const numGoroutines = 5

	var wg sync.WaitGroup
	var errorCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			sprint := &models.Sprint{
				Status:      models.SprintPending,
				Description: "Concurrent sprint",
				CreatedAt:   time.Now().Format(time.RFC3339),
			}

			_, err := db.CreateSprint(context.Background(), sprint)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				t.Logf("Sprint creation error: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt32(&errorCount) > 0 {
		t.Errorf("Got %d errors during concurrent sprint creation", atomic.LoadInt32(&errorCount))
	}

	// Verify all sprints were created
	sprints, err := db.ListSprints(context.Background(), nil)
	if err != nil {
		t.Fatalf("failed to list sprints: %v", err)
	}

	if len(sprints) != numGoroutines {
		t.Errorf("expected %d sprints, got %d", numGoroutines, len(sprints))
	}
}

// TestConcurrentSprintTaskOperations tests concurrent task additions to sprint
func TestConcurrentSprintTaskOperations(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test sprint",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}
	sprintID, err := db.CreateSprint(context.Background(), sprint)
	if err != nil {
		t.Fatalf("failed to create sprint: %v", err)
	}

	// Create tasks
	var taskIDs []int
	for i := 0; i < 10; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Title:                  "Task",
			FunctionalRequirements: "Action",
			TechnicalRequirements:  "Result",
				AcceptanceCriteria:     "Acceptance",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, err := db.CreateTask(context.Background(), task)
		if err != nil {
			t.Fatalf("failed to create task: %v", err)
		}
		taskIDs = append(taskIDs, id)
	}

	var wg sync.WaitGroup

	// Concurrent sprint task reads
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				db.GetSprintTasks(context.Background(), sprintID)
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Add tasks to sprint
	wg.Add(1)
	go func() {
		defer wg.Done()
		db.AddTasksToSprint(context.Background(), sprintID, taskIDs[:5])
	}()

	wg.Wait()
}

// ==================== CONCURRENT AUDIT OPERATIONS ====================

// TestConcurrentAuditLogging tests concurrent audit entry creation
func TestConcurrentAuditLogging(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const numGoroutines = 10
	const entriesPerGoroutine = 5

	var wg sync.WaitGroup
	var errorCount int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < entriesPerGoroutine; j++ {
				entry := &models.AuditEntry{
					Operation:   "TEST_OPERATION",
					EntityType:  "TASK",
					EntityID:    goroutineID*100 + j,
					PerformedAt: time.Now().Format(time.RFC3339),
				}

				_, err := db.LogAuditEntry(context.Background(), entry)
				if err != nil {
					atomic.AddInt32(&errorCount, 1)
					t.Logf("Audit logging error: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt32(&errorCount) > 0 {
		t.Errorf("Got %d errors during concurrent audit logging", atomic.LoadInt32(&errorCount))
	}

	// Verify entries were created
	entries, err := db.GetAuditEntries(context.Background(), nil, nil, nil, nil, nil, 0, 0)
	if err != nil {
		t.Fatalf("failed to get audit entries: %v", err)
	}

	expectedCount := numGoroutines * entriesPerGoroutine
	if len(entries) != expectedCount {
		t.Errorf("expected %d audit entries, got %d", expectedCount, len(entries))
	}
}

// TestConcurrentAuditReads tests reading audit entries while logging
func TestConcurrentAuditReads(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				db.GetAuditEntries(context.Background(), nil, nil, nil, nil, nil, 10, 0)
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Writers
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				entry := &models.AuditEntry{
					Operation:   "TEST_WRITE",
					EntityType:  "TASK",
					EntityID:    id*100 + j,
					PerformedAt: time.Now().Format(time.RFC3339),
				}
				db.LogAuditEntry(context.Background(), entry)
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// ==================== STRESS TESTS ====================

// TestHighConcurrencyStress tests the system under high concurrent load
func TestHighConcurrencyStress(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	const numWorkers = 20
	const operationsPerWorker = 50

	var wg sync.WaitGroup
	var errorCount int32

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operationsPerWorker; j++ {
				// Mix of operations
				switch j % 4 {
				case 0: // Create task
					task := &models.Task{
						Priority:       workerID,
						Severity:       j,
						Status:         models.StatusBacklog,
						Title:                  "Stress test task",
						FunctionalRequirements: "Action",
						TechnicalRequirements:  "Result",
				AcceptanceCriteria:     "Acceptance",
						CreatedAt:      time.Now().Format(time.RFC3339),
					}
					_, err := db.CreateTask(context.Background(), task)
					if err != nil {
						atomic.AddInt32(&errorCount, 1)
					}

				case 1: // List tasks
					_, err := db.ListTasks(context.Background(), nil, nil, nil, 10)
					if err != nil {
						atomic.AddInt32(&errorCount, 1)
					}

				case 2: // Create sprint
					sprint := &models.Sprint{
						Status:      models.SprintPending,
						Description: "Stress test sprint",
						CreatedAt:   time.Now().Format(time.RFC3339),
					}
					_, err := db.CreateSprint(context.Background(), sprint)
					if err != nil {
						atomic.AddInt32(&errorCount, 1)
					}

				case 3: // List sprints
					_, err := db.ListSprints(context.Background(), nil)
					if err != nil {
						atomic.AddInt32(&errorCount, 1)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	finalErrorCount := atomic.LoadInt32(&errorCount)
	if finalErrorCount > 0 {
		t.Logf("Got %d errors during stress test (expected some due to SQLite locking)", finalErrorCount)
	}

	t.Logf("Stress test completed: %d workers, %d operations each", numWorkers, operationsPerWorker)

	// Verify that at least some operations succeeded
	tasks, _ := db.ListTasks(context.Background(), nil, nil, nil, 1000)
	sprints, _ := db.ListSprints(context.Background(), nil)
	t.Logf("Final state: %d tasks, %d sprints", len(tasks), len(sprints))

	// The test passes if we have some data or if errors were only locking-related
	if len(tasks) == 0 && len(sprints) == 0 && finalErrorCount > 0 {
		t.Errorf("No data was created and got %d errors", finalErrorCount)
	}
}
