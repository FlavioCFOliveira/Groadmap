// Package db provides SQLite database connectivity and operations.
package db

import (
	"sync"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// TestRaceConcurrentTaskCreation tests concurrent task creation for race conditions.
func TestRaceConcurrentTaskCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race test in short mode")
	}

	db, cleanup := setupStressTestDB(t)
	defer cleanup()

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			task := &models.Task{
				Priority:       id % 10,
				Severity:       id % 10,
				Status:         models.StatusBacklog,
				Description:    "Race test task",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      "2026-03-16T12:00:00.000Z",
			}
			_, err := db.CreateTask(task)
			if err != nil {
				t.Logf("CreateTask error: %v", err)
			}
		}(i)
	}

	wg.Wait()

	// Verify tasks were created
	tasks, err := db.ListTasks(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(tasks) == 0 {
		t.Error("expected some tasks to be created")
	}
}

// TestRaceConcurrentTaskReads tests concurrent reads for race conditions.
func TestRaceConcurrentTaskReads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race test in short mode")
	}

	db, cleanup := setupStressTestDB(t)
	defer cleanup()

	// Create some initial tasks
	for i := 0; i < 10; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    "Initial task",
			Action:         "Test action",
			ExpectedResult: "Test result",
			CreatedAt:      "2026-03-16T12:00:00.000Z",
		}
		if _, err := db.CreateTask(task); err != nil {
			t.Fatalf("failed to create initial task: %v", err)
		}
	}

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := db.ListTasks(nil, nil, nil, nil)
			if err != nil {
				t.Logf("ListTasks error: %v", err)
			}
		}()
	}

	wg.Wait()
}

// TestRaceConcurrentReadWrite tests concurrent reads and writes for race conditions.
func TestRaceConcurrentReadWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race test in short mode")
	}

	db, cleanup := setupStressTestDB(t)
	defer cleanup()

	// Create initial task
	task := &models.Task{
		Priority:       1,
		Severity:       1,
		Status:         models.StatusBacklog,
		Description:    "Initial task",
		Action:         "Test action",
		ExpectedResult: "Test result",
		CreatedAt:      "2026-03-16T12:00:00.000Z",
	}
	taskID, err := db.CreateTask(task)
	if err != nil {
		t.Fatalf("failed to create initial task: %v", err)
	}

	const numGoroutines = 40
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Half readers, half writers
	for i := 0; i < numGoroutines/2; i++ {
		// Readers
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, err := db.GetTask(taskID)
				if err != nil {
					t.Logf("GetTask error: %v", err)
				}
			}
		}()
	}

	for i := 0; i < numGoroutines/2; i++ {
		// Writers
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				updates := map[string]interface{}{
					"priority": id % 10,
				}
				err := db.UpdateTask(taskID, updates)
				if err != nil {
					t.Logf("UpdateTask error: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestRaceConcurrentSprintOperations tests concurrent sprint operations for race conditions.
func TestRaceConcurrentSprintOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race test in short mode")
	}

	db, cleanup := setupStressTestDB(t)
	defer cleanup()

	// Create initial sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test sprint",
		CreatedAt:   "2026-03-16T12:00:00.000Z",
	}
	sprintID, err := db.CreateSprint(sprint)
	if err != nil {
		t.Fatalf("failed to create initial sprint: %v", err)
	}

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			// Concurrent reads
			_, err := db.GetSprint(sprintID)
			if err != nil {
				t.Logf("GetSprint error: %v", err)
			}
		}(i)
	}

	wg.Wait()
}

// TestRaceRateLimiter tests the rate limiter for race conditions.
func TestRaceRateLimiter(t *testing.T) {
	rl := NewRateLimiter(1000, 0) // No window for this test

	const numGoroutines = 100
	const requestsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				rl.Allow("test-key")
			}
		}()
	}

	wg.Wait()
}

// TestRaceRetryWithBackoff tests retry mechanism for race conditions.
func TestRaceRetryWithBackoff(t *testing.T) {
	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			// This should succeed immediately
			_ = retryWithBackoff("race test", func() error {
				return nil
			})
		}()
	}

	wg.Wait()
}
