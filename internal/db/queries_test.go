package db

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// setupTestDB creates an in-memory database for testing
func setupTestDB(t *testing.T) (*DB, func()) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	// Configure connection
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		sqlDB.Close()
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	db := &DB{
		DB:          sqlDB,
		roadmapName: "test",
	}

	// Create schema
	if err := db.CreateSchema(); err != nil {
		db.Close()
		t.Fatalf("failed to create schema: %v", err)
	}

	cleanup := func() {
		db.Close()
	}

	return db, cleanup
}

// ==================== TASK TESTS ====================

func TestCreateTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	task := &models.Task{
		Priority:       5,
		Severity:       3,
		Status:         models.StatusBacklog,
		Description:    "Test task",
		Action:         "Test action",
		ExpectedResult: "Test result",
		CreatedAt:      time.Now().Format(time.RFC3339),
	}

	id, err := db.CreateTask(task)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	if id == 0 {
		t.Error("expected non-zero task ID")
	}

	// Verify task was created
	created, err := db.GetTask(id)
	if err != nil {
		t.Fatalf("failed to get created task: %v", err)
	}

	if created.Description != task.Description {
		t.Errorf("expected description %q, got %q", task.Description, created.Description)
	}

	if created.Status != task.Status {
		t.Errorf("expected status %q, got %q", task.Status, created.Status)
	}
}

func TestGetTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a task first
	task := &models.Task{
		Priority:       1,
		Severity:       1,
		Status:         models.StatusBacklog,
		Description:    "Test task",
		Action:         "Action",
		ExpectedResult: "Result",
		CreatedAt:      time.Now().Format(time.RFC3339),
	}

	id, _ := db.CreateTask(task)

	// Test getting existing task
	retrieved, err := db.GetTask(id)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}

	if retrieved.ID != id {
		t.Errorf("expected ID %d, got %d", id, retrieved.ID)
	}

	// Test getting non-existent task
	_, err = db.GetTask(99999)
	if err == nil {
		t.Error("expected error for non-existent task")
	}
}

func TestGetTasks(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create multiple tasks
	var ids []int
	for i := 0; i < 3; i++ {
		task := &models.Task{
			Priority:       i,
			Severity:       i,
			Status:         models.StatusBacklog,
			Description:    "Task",
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, _ := db.CreateTask(task)
		ids = append(ids, id)
	}

	// Test GetTasks
	tasks, err := db.GetTasks(ids)
	if err != nil {
		t.Fatalf("failed to get tasks: %v", err)
	}

	if len(tasks) != len(ids) {
		t.Errorf("expected %d tasks, got %d", len(ids), len(tasks))
	}

	// Test empty IDs
	tasks, err = db.GetTasks([]int{})
	if err != nil {
		t.Fatalf("failed to get tasks with empty IDs: %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks for empty IDs, got %d", len(tasks))
	}
}

func TestListTasks(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create tasks with different statuses
	statuses := []models.TaskStatus{models.StatusBacklog, models.StatusBacklog, models.StatusDoing}
	for i, status := range statuses {
		task := &models.Task{
			Priority:       i,
			Severity:       i,
			Status:         status,
			Description:    "Task",
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		db.CreateTask(task)
	}

	// Test list all tasks
	tasks, err := db.ListTasks(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}

	// Test filter by status
	backlogStatus := models.StatusBacklog
	tasks, err = db.ListTasks(&backlogStatus, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to list tasks by status: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks with BACKLOG status, got %d", len(tasks))
	}

	// Test filter by min priority
	minPriority := 1
	tasks, err = db.ListTasks(nil, &minPriority, nil, nil)
	if err != nil {
		t.Fatalf("failed to list tasks by priority: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks with priority >= 1, got %d", len(tasks))
	}

	// Test with limit
	limit := 2
	tasks, err = db.ListTasks(nil, nil, nil, &limit)
	if err != nil {
		t.Fatalf("failed to list tasks with limit: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks with limit, got %d", len(tasks))
	}
}

func TestUpdateTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a task
	task := &models.Task{
		Priority:       1,
		Severity:       1,
		Status:         models.StatusBacklog,
		Description:    "Original",
		Action:         "Action",
		ExpectedResult: "Result",
		CreatedAt:      time.Now().Format(time.RFC3339),
	}

	id, _ := db.CreateTask(task)

	// Update the task
	updates := map[string]interface{}{
		"description": "Updated",
		"priority":    5,
	}

	err := db.UpdateTask(id, updates)
	if err != nil {
		t.Fatalf("failed to update task: %v", err)
	}

	// Verify update
	updated, _ := db.GetTask(id)
	if updated.Description != "Updated" {
		t.Errorf("expected description 'Updated', got %q", updated.Description)
	}

	if updated.Priority != 5 {
		t.Errorf("expected priority 5, got %d", updated.Priority)
	}

	// Test update non-existent task
	err = db.UpdateTask(99999, updates)
	if err == nil {
		t.Error("expected error for non-existent task")
	}

	// Test empty updates
	err = db.UpdateTask(id, map[string]interface{}{})
	if err != nil {
		t.Errorf("expected no error for empty updates, got %v", err)
	}
}

func TestDeleteTask(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create a task
	task := &models.Task{
		Priority:       1,
		Severity:       1,
		Status:         models.StatusBacklog,
		Description:    "To delete",
		Action:         "Action",
		ExpectedResult: "Result",
		CreatedAt:      time.Now().Format(time.RFC3339),
	}

	id, _ := db.CreateTask(task)

	// Delete the task
	err := db.DeleteTask(id)
	if err != nil {
		t.Fatalf("failed to delete task: %v", err)
	}

	// Verify deletion
	_, err = db.GetTask(id)
	if err == nil {
		t.Error("expected error after deleting task")
	}

	// Test delete non-existent task
	err = db.DeleteTask(99999)
	if err == nil {
		t.Error("expected error for non-existent task")
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create tasks
	var ids []int
	for i := 0; i < 2; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    "Task",
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, _ := db.CreateTask(task)
		ids = append(ids, id)
	}

	// Update status
	err := db.UpdateTaskStatus(ids, models.StatusDoing)
	if err != nil {
		t.Fatalf("failed to update task status: %v", err)
	}

	// Verify
	for _, id := range ids {
		task, _ := db.GetTask(id)
		if task.Status != models.StatusDoing {
			t.Errorf("expected status DOING, got %q", task.Status)
		}
	}

	// Test empty IDs
	err = db.UpdateTaskStatus([]int{}, models.StatusDoing)
	if err != nil {
		t.Errorf("expected no error for empty IDs, got %v", err)
	}
}

func TestUpdateTaskPriority(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create tasks
	var ids []int
	for i := 0; i < 2; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    "Task",
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, _ := db.CreateTask(task)
		ids = append(ids, id)
	}

	// Update priority
	err := db.UpdateTaskPriority(ids, 9)
	if err != nil {
		t.Fatalf("failed to update task priority: %v", err)
	}

	// Verify
	for _, id := range ids {
		task, _ := db.GetTask(id)
		if task.Priority != 9 {
			t.Errorf("expected priority 9, got %d", task.Priority)
		}
	}
}

func TestUpdateTaskSeverity(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create tasks
	var ids []int
	for i := 0; i < 2; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    "Task",
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, _ := db.CreateTask(task)
		ids = append(ids, id)
	}

	// Update severity
	err := db.UpdateTaskSeverity(ids, 8)
	if err != nil {
		t.Fatalf("failed to update task severity: %v", err)
	}

	// Verify
	for _, id := range ids {
		task, _ := db.GetTask(id)
		if task.Severity != 8 {
			t.Errorf("expected severity 8, got %d", task.Severity)
		}
	}
}

// ==================== SPRINT TESTS ====================

func TestCreateSprint(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test sprint",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	id, err := db.CreateSprint(sprint)
	if err != nil {
		t.Fatalf("failed to create sprint: %v", err)
	}

	if id == 0 {
		t.Error("expected non-zero sprint ID")
	}

	// Verify
	created, err := db.GetSprint(id)
	if err != nil {
		t.Fatalf("failed to get created sprint: %v", err)
	}

	if created.Description != sprint.Description {
		t.Errorf("expected description %q, got %q", sprint.Description, created.Description)
	}
}

func TestGetSprint(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test sprint",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	id, _ := db.CreateSprint(sprint)

	// Test getting existing sprint
	retrieved, err := db.GetSprint(id)
	if err != nil {
		t.Fatalf("failed to get sprint: %v", err)
	}

	if retrieved.ID != id {
		t.Errorf("expected ID %d, got %d", id, retrieved.ID)
	}

	// Test getting non-existent sprint
	_, err = db.GetSprint(99999)
	if err == nil {
		t.Error("expected error for non-existent sprint")
	}
}

func TestListSprints(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create sprints with different statuses
	statuses := []models.SprintStatus{models.SprintPending, models.SprintOpen}
	for _, status := range statuses {
		sprint := &models.Sprint{
			Status:      status,
			Description: "Sprint",
			CreatedAt:   time.Now().Format(time.RFC3339),
		}
		db.CreateSprint(sprint)
	}

	// Test list all sprints
	sprints, err := db.ListSprints(nil)
	if err != nil {
		t.Fatalf("failed to list sprints: %v", err)
	}

	if len(sprints) != 2 {
		t.Errorf("expected 2 sprints, got %d", len(sprints))
	}

	// Test filter by status
	pendingStatus := models.SprintPending
	sprints, err = db.ListSprints(&pendingStatus)
	if err != nil {
		t.Fatalf("failed to list sprints by status: %v", err)
	}

	if len(sprints) != 1 {
		t.Errorf("expected 1 sprint with PENDING status, got %d", len(sprints))
	}
}

func TestUpdateSprint(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Original",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	id, _ := db.CreateSprint(sprint)

	// Update
	err := db.UpdateSprint(id, "Updated")
	if err != nil {
		t.Fatalf("failed to update sprint: %v", err)
	}

	// Verify
	updated, _ := db.GetSprint(id)
	if updated.Description != "Updated" {
		t.Errorf("expected description 'Updated', got %q", updated.Description)
	}

	// Test update non-existent sprint
	err = db.UpdateSprint(99999, "Test")
	if err == nil {
		t.Error("expected error for non-existent sprint")
	}
}

func TestUpdateSprintStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	id, _ := db.CreateSprint(sprint)

	// Update status to OPEN
	err := db.UpdateSprintStatus(id, models.SprintOpen)
	if err != nil {
		t.Fatalf("failed to update sprint status: %v", err)
	}

	// Verify
	updated, _ := db.GetSprint(id)
	if updated.Status != models.SprintOpen {
		t.Errorf("expected status OPEN, got %q", updated.Status)
	}
	if updated.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}

	// Update status to CLOSED
	err = db.UpdateSprintStatus(id, models.SprintClosed)
	if err != nil {
		t.Fatalf("failed to close sprint: %v", err)
	}

	updated, _ = db.GetSprint(id)
	if updated.Status != models.SprintClosed {
		t.Errorf("expected status CLOSED, got %q", updated.Status)
	}
	if updated.ClosedAt == nil {
		t.Error("expected ClosedAt to be set")
	}
}

func TestDeleteSprint(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "To delete",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	id, _ := db.CreateSprint(sprint)

	// Delete
	err := db.DeleteSprint(id)
	if err != nil {
		t.Fatalf("failed to delete sprint: %v", err)
	}

	// Verify
	_, err = db.GetSprint(id)
	if err == nil {
		t.Error("expected error after deleting sprint")
	}
}

func TestAddTasksToSprint(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test sprint",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}
	sprintID, _ := db.CreateSprint(sprint)

	// Create tasks
	var taskIDs []int
	for i := 0; i < 3; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    "Task",
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, _ := db.CreateTask(task)
		taskIDs = append(taskIDs, id)
	}

	// Add tasks to sprint
	err := db.AddTasksToSprint(sprintID, taskIDs)
	if err != nil {
		t.Fatalf("failed to add tasks to sprint: %v", err)
	}

	// Verify tasks are in sprint
	sprintTasks, err := db.GetSprintTasks(sprintID)
	if err != nil {
		t.Fatalf("failed to get sprint tasks: %v", err)
	}

	if len(sprintTasks) != 3 {
		t.Errorf("expected 3 tasks in sprint, got %d", len(sprintTasks))
	}

	// Verify task statuses were updated
	for _, taskID := range taskIDs {
		task, _ := db.GetTask(taskID)
		if task.Status != models.StatusSprint {
			t.Errorf("expected task status SPRINT, got %q", task.Status)
		}
	}
}

func TestRemoveTasksFromSprint(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test sprint",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}
	sprintID, _ := db.CreateSprint(sprint)

	// Create and add tasks
	var taskIDs []int
	for i := 0; i < 3; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    "Task",
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, _ := db.CreateTask(task)
		taskIDs = append(taskIDs, id)
	}
	db.AddTasksToSprint(sprintID, taskIDs)

	// Remove tasks from sprint
	err := db.RemoveTasksFromSprint(taskIDs[:2])
	if err != nil {
		t.Fatalf("failed to remove tasks from sprint: %v", err)
	}

	// Verify task statuses were reset
	for i := 0; i < 2; i++ {
		task, _ := db.GetTask(taskIDs[i])
		if task.Status != models.StatusBacklog {
			t.Errorf("expected task status BACKLOG, got %q", task.Status)
		}
	}
}

// ==================== AUDIT TESTS ====================

func TestLogAuditEntry(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	entry := &models.AuditEntry{
		Operation:   "TASK_CREATE",
		EntityType:  "TASK",
		EntityID:    1,
		PerformedAt: time.Now().Format(time.RFC3339),
	}

	id, err := db.LogAuditEntry(entry)
	if err != nil {
		t.Fatalf("failed to log audit entry: %v", err)
	}

	if id == 0 {
		t.Error("expected non-zero audit ID")
	}
}

func TestGetAuditEntries(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create audit entries
	for i := 0; i < 3; i++ {
		entry := &models.AuditEntry{
			Operation:   "TASK_CREATE",
			EntityType:  "TASK",
			EntityID:    i + 1,
			PerformedAt: time.Now().Format(time.RFC3339),
		}
		db.LogAuditEntry(entry)
	}

	// Get all entries
	entries, err := db.GetAuditEntries(nil, nil, nil, nil, nil, 10, 0)
	if err != nil {
		t.Fatalf("failed to get audit entries: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 audit entries, got %d", len(entries))
	}

	// Filter by operation
	op := "TASK_CREATE"
	entries, err = db.GetAuditEntries(&op, nil, nil, nil, nil, 10, 0)
	if err != nil {
		t.Fatalf("failed to filter audit entries: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries with TASK_CREATE, got %d", len(entries))
	}
}

// ==================== TRANSACTION TESTS ====================

func TestWithTransaction_Commit(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create task within transaction
	err := db.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO tasks (priority, severity, status, description, action, expected_result, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			1, 1, string(models.StatusBacklog), "Tx Task", "Action", "Result", time.Now().Format(time.RFC3339),
		)
		return err
	})

	if err != nil {
		t.Fatalf("transaction failed: %v", err)
	}

	// Verify task was created
	tasks, _ := db.ListTasks(nil, nil, nil, nil)
	if len(tasks) != 1 {
		t.Errorf("expected 1 task after commit, got %d", len(tasks))
	}
}

func TestWithTransaction_Rollback(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create task within transaction that will fail
	err := db.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO tasks (priority, severity, status, description, action, expected_result, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			1, 1, string(models.StatusBacklog), "Tx Task", "Action", "Result", time.Now().Format(time.RFC3339),
		)
		if err != nil {
			return err
		}
		// Return error to trigger rollback
		return fmt.Errorf("intentional error")
	})

	if err == nil {
		t.Error("expected error from transaction")
	}

	// Verify no tasks were created (rolled back)
	tasks, _ := db.ListTasks(nil, nil, nil, nil)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after rollback, got %d", len(tasks))
	}
}

// ==================== ADDITIONAL TESTS ====================

func TestGetSprintTasks(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test sprint",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}
	sprintID, _ := db.CreateSprint(sprint)

	// Create tasks
	var taskIDs []int
	for i := 0; i < 3; i++ {
		task := &models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    "Task",
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, _ := db.CreateTask(task)
		taskIDs = append(taskIDs, id)
	}

	// Add tasks to sprint
	db.AddTasksToSprint(sprintID, taskIDs)

	// Get sprint tasks
	tasks, err := db.GetSprintTasks(sprintID)
	if err != nil {
		t.Fatalf("failed to get sprint tasks: %v", err)
	}

	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}

	// Test GetSprintTasksFull with status filter
	// Note: When tasks are added to sprint, their status changes to SPRINT
	sprintStatus := models.StatusSprint
	tasksFull, err := db.GetSprintTasksFull(sprintID, &sprintStatus)
	if err != nil {
		t.Fatalf("failed to get sprint tasks with status: %v", err)
	}

	// All tasks should be in SPRINT status after being added to sprint
	if len(tasksFull) != 3 {
		t.Errorf("expected 3 tasks with SPRINT status, got %d", len(tasksFull))
	}
}

func TestGetAuditStats(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create audit entries
	for i := 0; i < 5; i++ {
		entry := &models.AuditEntry{
			Operation:   "TASK_CREATE",
			EntityType:  "TASK",
			EntityID:    i + 1,
			PerformedAt: time.Now().Format(time.RFC3339),
		}
		db.LogAuditEntry(entry)
	}

	// Get stats
	stats, err := db.GetAuditStats(nil, nil)
	if err != nil {
		t.Fatalf("failed to get audit stats: %v", err)
	}

	if stats.TotalEntries != 5 {
		t.Errorf("expected 5 total entries, got %d", stats.TotalEntries)
	}

	// Test with date range filters
	since := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	until := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	stats, err = db.GetAuditStats(&since, &until)
	if err != nil {
		t.Fatalf("failed to get audit stats with date range: %v", err)
	}

	if stats.TotalEntries != 5 {
		t.Errorf("expected 5 entries in date range, got %d", stats.TotalEntries)
	}
}

func TestGetSprintTasksFull(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create sprint
	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test sprint",
		CreatedAt:   time.Now().Format(time.RFC3339),
	}
	sprintID, _ := db.CreateSprint(sprint)

	// Create tasks with different priorities
	for i := 0; i < 3; i++ {
		task := &models.Task{
			Priority:       i,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    "Task",
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      time.Now().Format(time.RFC3339),
		}
		id, _ := db.CreateTask(task)
		db.AddTasksToSprint(sprintID, []int{id})
	}

	// Get all sprint tasks
	tasks, err := db.GetSprintTasksFull(sprintID, nil)
	if err != nil {
		t.Fatalf("failed to get sprint tasks full: %v", err)
	}

	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}

	// Verify ordering by priority DESC
	if tasks[0].Priority < tasks[1].Priority {
		t.Error("expected tasks ordered by priority DESC")
	}
}

func TestGetAuditEntriesWithFilters(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create audit entries with different types
	entries := []struct {
		op         string
		entityType string
		entityID   int
	}{
		{"TASK_CREATE", "TASK", 1},
		{"TASK_UPDATE", "TASK", 1},
		{"SPRINT_CREATE", "SPRINT", 1},
	}

	for _, e := range entries {
		entry := &models.AuditEntry{
			Operation:   e.op,
			EntityType:  e.entityType,
			EntityID:    e.entityID,
			PerformedAt: time.Now().Format(time.RFC3339),
		}
		db.LogAuditEntry(entry)
	}

	// Test filter by entity type
	entityType := "TASK"
	results, err := db.GetAuditEntries(nil, &entityType, nil, nil, nil, 10, 0)
	if err != nil {
		t.Fatalf("failed to filter by entity type: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 TASK entries, got %d", len(results))
	}

	// Test filter by entity ID
	entityID := 1
	results, err = db.GetAuditEntries(nil, nil, &entityID, nil, nil, 10, 0)
	if err != nil {
		t.Fatalf("failed to filter by entity ID: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 entries for entity ID 1, got %d", len(results))
	}

	// Test with offset
	results, err = db.GetAuditEntries(nil, nil, nil, nil, nil, 10, 1)
	if err != nil {
		t.Fatalf("failed to get entries with offset: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 entries with offset 1, got %d", len(results))
	}
}
