package db

import (
	"database/sql"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

func TestShouldApplyMigration(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		targetVersion  string
		shouldApply    bool
	}{
		{"current less than target", "1.0.0", "1.1.0", true},
		{"current equals target", "1.0.0", "1.0.0", false},
		{"current greater than target", "1.1.0", "1.0.0", false},
		{"multiple version jumps", "1.0.0", "2.0.0", true},
		{"patch version", "1.0.0", "1.0.1", true},
		{"lexicographic order caveat", "1.0.9", "1.0.10", false}, // String comparison: "1.0.9" > "1.0.10"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldApplyMigration(tt.currentVersion, tt.targetVersion)
			if result != tt.shouldApply {
				t.Errorf("shouldApplyMigration(%q, %q) = %v, want %v",
					tt.currentVersion, tt.targetVersion, result, tt.shouldApply)
			}
		})
	}
}

func TestRunMigrations_FreshDatabase(t *testing.T) {
	// Create a fresh database
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// For fresh databases, RunMigrations should return nil
	// because _metadata table doesn't exist yet
	err := db.RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations on fresh database failed: %v", err)
	}
}

func TestRunMigrations_UpToDate(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := testContext()

	// Create schema (which sets schema version to latest)
	if err := db.CreateSchema(); err != nil {
		t.Fatalf("CreateSchema failed: %v", err)
	}

	// Get current schema version
	version, err := db.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion failed: %v", err)
	}

	// Create a sprint and add tasks to test position column
	sprint := &models.Sprint{
		Description: "Test Sprint",
		Status:      models.SprintPending,
	}
	sprintID, err := db.CreateSprint(ctx, sprint)
	if err != nil {
		t.Fatalf("CreateSprint failed: %v", err)
	}

	task := &models.Task{
		Title:                  "Test task",
		FunctionalRequirements: "Functional",
		TechnicalRequirements:  "Technical",
		AcceptanceCriteria:     "Criteria",
		Type:                   models.TypeTask,
		Status:                 models.StatusBacklog,
	}
	taskID, err := db.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	// Add task to sprint
	err = db.AddTasksToSprint(ctx, sprintID, []int{taskID})
	if err != nil {
		t.Fatalf("AddTasksToSprint failed: %v", err)
	}

	// Run migrations again - should be no-op since already up to date
	err = db.RunMigrations()
	if err != nil {
		t.Fatalf("RunMigrations on up-to-date database failed: %v", err)
	}

	// Verify schema version hasn't changed
	newVersion, err := db.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion failed: %v", err)
	}
	if newVersion != version {
		t.Errorf("Schema version changed from %s to %s after no-op migration", version, newVersion)
	}
}

func TestMigrateV1_0_0_toV1_1_0(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := testContext()

	// Create schema first
	if err := db.CreateSchema(); err != nil {
		t.Fatalf("CreateSchema failed: %v", err)
	}

	// Create sprint and tasks before migration
	sprint := &models.Sprint{
		Description: "Test Sprint",
		Status:      models.SprintPending,
	}
	sprintID, err := db.CreateSprint(ctx, sprint)
	if err != nil {
		t.Fatalf("CreateSprint failed: %v", err)
	}

	var taskIDs []int
	for i := 0; i < 3; i++ {
		task := &models.Task{
			Title:                  "Test task",
			FunctionalRequirements: "Functional",
			TechnicalRequirements:  "Technical",
			AcceptanceCriteria:     "Criteria",
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
		}
		taskID, err := db.CreateTask(ctx, task)
		if err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
		taskIDs = append(taskIDs, taskID)
	}

	// Add tasks to sprint
	err = db.AddTasksToSprint(ctx, sprintID, taskIDs)
	if err != nil {
		t.Fatalf("AddTasksToSprint failed: %v", err)
	}

	// Verify position column exists by querying sprint tasks with position
	var position int
	err = db.QueryRowContext(ctx,
		"SELECT position FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?",
		sprintID, taskIDs[0],
	).Scan(&position)
	if err != nil {
		t.Errorf("Failed to query position column: %v", err)
	}
}

func TestMigrationsOrder(t *testing.T) {
	// Verify migrations are ordered by version
	if len(migrations) == 0 {
		t.Skip("No migrations to test")
	}

	for i := 1; i < len(migrations); i++ {
		if migrations[i].Version < migrations[i-1].Version {
			t.Errorf("Migrations not ordered: %s comes before %s",
				migrations[i-1].Version, migrations[i].Version)
		}
	}
}

func TestMigrationsHaveRequiredFields(t *testing.T) {
	for _, migration := range migrations {
		if migration.Version == "" {
			t.Errorf("Migration %q has empty Version", migration.Name)
		}
		if migration.Name == "" {
			t.Errorf("Migration %q has empty Name", migration.Version)
		}
		if migration.Apply == nil {
			t.Errorf("Migration %q has nil Apply function", migration.Version)
		}
	}
}

func TestMigrationStruct(t *testing.T) {
	migration := Migration{
		Version: "1.0.0",
		Name:    "Test Migration",
		Apply:   func(tx *sql.Tx) error { return nil },
	}

	if migration.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", migration.Version, "1.0.0")
	}
	if migration.Name != "Test Migration" {
		t.Errorf("Name = %q, want %q", migration.Name, "Test Migration")
	}
	if migration.Apply == nil {
		t.Error("Apply function should not be nil")
	}
}

func TestMigrationFuncType(t *testing.T) {
	// Test that MigrationFunc works correctly
	var fn MigrationFunc = func(tx *sql.Tx) error {
		return nil
	}

	if fn == nil {
		t.Error("MigrationFunc should not be nil")
	}
}

func TestMigrateV1_2_0_toV1_3_0(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Create schema (sets version to current SchemaVersion)
	if err := db.CreateSchema(); err != nil {
		t.Fatalf("CreateSchema failed: %v", err)
	}

	// completion_summary column must exist in the fresh schema
	var colType string
	err := db.QueryRow(
		`SELECT type FROM pragma_table_info('tasks') WHERE name = 'completion_summary'`,
	).Scan(&colType)
	if err != nil {
		t.Fatalf("completion_summary column not found after CreateSchema: %v", err)
	}

	// Schema version must be 1.3.0
	version, err := db.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion failed: %v", err)
	}
	if version != "1.3.0" {
		t.Errorf("schema_version = %q, want %q", version, "1.3.0")
	}

	// Running migrations again must be idempotent
	if err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations (idempotency check) failed: %v", err)
	}

	version, err = db.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion after idempotent run failed: %v", err)
	}
	if version != "1.3.0" {
		t.Errorf("schema_version after idempotent migration = %q, want %q", version, "1.3.0")
	}
}
