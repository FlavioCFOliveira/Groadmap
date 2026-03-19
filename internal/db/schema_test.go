package db

import (
	"database/sql"
	"testing"
)

func TestCreateSchema(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	db := &DB{DB: sqlDB, roadmapName: "test"}

	// Test creating schema
	err = db.CreateSchema()
	if err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	// Verify tables exist
	tables := []string{"tasks", "sprints", "sprint_tasks", "audit", "_metadata"}
	for _, table := range tables {
		var name string
		err := sqlDB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
		if name != table {
			t.Errorf("expected table %s, got %s", table, name)
		}
	}
}

func TestSchemaVersion(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	db := &DB{DB: sqlDB, roadmapName: "test"}

	// Create schema
	if err := db.CreateSchema(); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	// Verify schema version
	var version string
	err = sqlDB.QueryRow("SELECT value FROM _metadata WHERE key = 'schema_version'").Scan(&version)
	if err != nil {
		t.Fatalf("failed to get schema version: %v", err)
	}

	if version != "1.2.0" {
		t.Errorf("expected schema version 1.2.0, got %s", version)
	}
}

func TestForeignKeyConstraints(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	db := &DB{DB: sqlDB, roadmapName: "test"}

	// Create schema
	if err := db.CreateSchema(); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	// Test 1: Insert a sprint
	result, err := sqlDB.Exec(
		"INSERT INTO sprints (status, description, created_at) VALUES (?, ?, ?)",
		"PENDING", "Test Sprint", "2024-01-15T10:00:00.000Z",
	)
	if err != nil {
		t.Fatalf("failed to insert sprint: %v", err)
	}
	sprintID, _ := result.LastInsertId()

	// Test 2: Insert a task
	result, err = sqlDB.Exec(
		"INSERT INTO tasks (title, status, functional_requirements, technical_requirements, acceptance_criteria, created_at, priority, severity) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		"Test Task", "BACKLOG", "Do something", "Result", "Acceptance", "2024-01-15T10:00:00.000Z", 0, 0,
	)
	if err != nil {
		t.Fatalf("failed to insert task: %v", err)
	}
	taskID, _ := result.LastInsertId()

	// Test 3: Insert sprint_tasks with valid foreign keys (should succeed)
	_, err = sqlDB.Exec(
		"INSERT INTO sprint_tasks (sprint_id, task_id, added_at) VALUES (?, ?, ?)",
		sprintID, taskID, "2024-01-15T10:00:00.000Z",
	)
	if err != nil {
		t.Errorf("failed to insert sprint_tasks with valid FKs: %v", err)
	}

	// Test 4: Insert sprint_tasks with invalid sprint_id (should fail)
	_, err = sqlDB.Exec(
		"INSERT INTO sprint_tasks (sprint_id, task_id, added_at) VALUES (?, ?, ?)",
		9999, taskID, "2024-01-15T10:00:00.000Z",
	)
	if err == nil {
		t.Error("expected error when inserting sprint_tasks with invalid sprint_id")
	}

	// Test 5: Insert sprint_tasks with invalid task_id (should fail)
	_, err = sqlDB.Exec(
		"INSERT INTO sprint_tasks (sprint_id, task_id, added_at) VALUES (?, ?, ?)",
		sprintID, 9999, "2024-01-15T10:00:00.000Z",
	)
	if err == nil {
		t.Error("expected error when inserting sprint_tasks with invalid task_id")
	}

	// Test 6: Delete sprint should cascade delete sprint_tasks
	_, err = sqlDB.Exec("DELETE FROM sprints WHERE id = ?", sprintID)
	if err != nil {
		t.Fatalf("failed to delete sprint: %v", err)
	}

	// Verify sprint_tasks was deleted
	var count int
	err = sqlDB.QueryRow("SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?", sprintID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count sprint_tasks: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 sprint_tasks after cascade delete, got %d", count)
	}
}

func TestAuditEntityTypeConstraint(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	db := &DB{DB: sqlDB, roadmapName: "test"}

	// Create schema
	if err := db.CreateSchema(); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	// Test 1: Insert audit with valid entity_type (should succeed)
	_, err = sqlDB.Exec(
		"INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)",
		"CREATE", "TASK", 1, "2024-01-15T10:00:00.000Z",
	)
	if err != nil {
		t.Errorf("failed to insert audit with valid entity_type: %v", err)
	}

	// Test 2: Insert audit with another valid entity_type (should succeed)
	_, err = sqlDB.Exec(
		"INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)",
		"CREATE", "SPRINT", 1, "2024-01-15T10:00:00.000Z",
	)
	if err != nil {
		t.Errorf("failed to insert audit with SPRINT entity_type: %v", err)
	}

	// Test 3: Insert audit with invalid entity_type (should fail)
	_, err = sqlDB.Exec(
		"INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)",
		"CREATE", "INVALID", 1, "2024-01-15T10:00:00.000Z",
	)
	if err == nil {
		t.Error("expected error when inserting audit with invalid entity_type")
	}
}
