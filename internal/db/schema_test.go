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

	if version != "1.0.0" {
		t.Errorf("expected schema version 1.0.0, got %s", version)
	}
}
