package db

import (
	"database/sql"
	"strconv"
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
		// Regression for the lexicographic-comparison bug (finding #42): a string
		// compare wrongly ordered "1.0.10" before "1.0.9" and "1.10.0" before
		// "1.9.0", skipping migrations once a component reached two digits.
		{"two-digit patch greater than one-digit", "1.0.9", "1.0.10", true},
		{"one-digit patch not applied over two-digit", "1.0.10", "1.0.9", false},
		{"two-digit minor greater than one-digit", "1.9.0", "1.10.0", true},
		{"one-digit minor not applied over two-digit", "1.10.0", "1.9.0", false},
		{"missing trailing component equals zero", "1.5", "1.5.0", false},
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

// TestCompareVersions is a regression gate for finding #42: version components
// must compare numerically, not lexicographically.
func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.9.0", "1.10.0", -1}, // numeric: 9 < 10
		{"1.10.0", "1.9.0", 1},  // numeric: 10 > 9
		{"1.0.9", "1.0.10", -1}, // numeric: 9 < 10
		{"2.0.0", "1.99.99", 1}, // major dominates
		{"1.5", "1.5.0", 0},     // missing trailing component == 0
		{"1.5.0", "1.5", 0},     // symmetric
		{"1.10.2", "1.10.10", -1},
	}
	for _, tt := range tests {
		if got := compareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
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
		Title:       "Test Sprint",
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
		Title:       "Test Sprint",
		Description: "Test Sprint",
		Status:      models.SprintPending,
	}
	sprintID, err := db.CreateSprint(ctx, sprint)
	if err != nil {
		t.Fatalf("CreateSprint failed: %v", err)
	}

	taskIDs := make([]int, 0, 3)
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

// TestMigrateV1_6_0_toV1_7_0 is the regression gate for the sprints.title
// migration. It builds a v1.6.0-shape sprints table (no title column), inserts a
// couple of rows, runs the migration, and asserts that every row receives the
// deterministic title 'Sprint ' || id and that the new column is NOT NULL. It
// also asserts that a second apply is a no-op and never clobbers a real title.
func TestMigrateV1_6_0_toV1_7_0(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer sqlDB.Close()

	// v1.6.0-shape sprints table: no title column.
	if _, err := sqlDB.Exec(`
		CREATE TABLE sprints (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			status TEXT NOT NULL DEFAULT 'PENDING',
			description TEXT NOT NULL,
			created_at TEXT NOT NULL,
			started_at TEXT,
			closed_at TEXT,
			max_tasks INTEGER
		)`); err != nil {
		t.Fatalf("creating v1.6.0 sprints table: %v", err)
	}

	// Insert two rows in the pre-title shape.
	for _, desc := range []string{"Authentication hardening", "Q3 performance push"} {
		if _, err := sqlDB.Exec(
			"INSERT INTO sprints (status, description, created_at) VALUES ('PENDING', ?, '2026-01-01T00:00:00.000Z')",
			desc,
		); err != nil {
			t.Fatalf("seeding sprint %q: %v", desc, err)
		}
	}

	// Apply the migration inside a transaction.
	tx, err := sqlDB.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := migrateV1_6_0_toV1_7_0(tx); err != nil {
		_ = tx.Rollback()
		t.Fatalf("migrateV1_6_0_toV1_7_0: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	// The title column must now exist and be NOT NULL.
	var notNull int
	if err := sqlDB.QueryRow(
		`SELECT "notnull" FROM pragma_table_info('sprints') WHERE name = 'title'`,
	).Scan(&notNull); err != nil {
		t.Fatalf("title column not found after migration: %v", err)
	}
	if notNull != 1 {
		t.Errorf("title column notnull = %d, want 1 (NOT NULL)", notNull)
	}

	// Every row must carry the deterministic backfilled title.
	assertBackfilledTitles := func(stage string) {
		rows, err := sqlDB.Query("SELECT id, title FROM sprints ORDER BY id")
		if err != nil {
			t.Fatalf("[%s] querying titles: %v", stage, err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			var title string
			if err := rows.Scan(&id, &title); err != nil {
				t.Fatalf("[%s] scanning title: %v", stage, err)
			}
			want := "Sprint " + strconv.Itoa(id)
			if title != want {
				t.Errorf("[%s] sprint %d title = %q, want %q", stage, id, title, want)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("[%s] iterating titles: %v", stage, err)
		}
	}
	assertBackfilledTitles("first apply")

	// Set a real title on row 1, then re-apply: the migration must be idempotent
	// and must NOT clobber the existing real title.
	if _, err := sqlDB.Exec("UPDATE sprints SET title = 'Real custom title' WHERE id = 1"); err != nil {
		t.Fatalf("setting real title: %v", err)
	}

	tx2, err := sqlDB.Begin()
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	if err := migrateV1_6_0_toV1_7_0(tx2); err != nil {
		_ = tx2.Rollback()
		t.Fatalf("second migrateV1_6_0_toV1_7_0: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit tx2: %v", err)
	}

	var title1 string
	if err := sqlDB.QueryRow("SELECT title FROM sprints WHERE id = 1").Scan(&title1); err != nil {
		t.Fatalf("re-reading title: %v", err)
	}
	if title1 != "Real custom title" {
		t.Errorf("idempotent re-apply clobbered real title: got %q, want %q", title1, "Real custom title")
	}
}

func TestMigrationsOrder(t *testing.T) {
	// Verify migrations are ordered by version
	if len(migrations) == 0 {
		t.Skip("No migrations to test")
	}

	for i := 1; i < len(migrations); i++ {
		if compareVersions(migrations[i].Version, migrations[i-1].Version) < 0 {
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

	// max_tasks column must exist in the fresh schema (added in 1.4.0)
	var maxTasksColType string
	err = db.QueryRow(
		`SELECT type FROM pragma_table_info('sprints') WHERE name = 'max_tasks'`,
	).Scan(&maxTasksColType)
	if err != nil {
		t.Fatalf("max_tasks column not found after CreateSchema: %v", err)
	}

	// Schema version must be 1.5.0 (current)
	version, err := db.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion failed: %v", err)
	}
	if version != "1.8.0" {
		t.Errorf("schema_version = %q, want %q", version, "1.8.0")
	}

	// Running migrations again must be idempotent
	if err := db.RunMigrations(); err != nil {
		t.Fatalf("RunMigrations (idempotency check) failed: %v", err)
	}

	version, err = db.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion after idempotent run failed: %v", err)
	}
	if version != "1.8.0" {
		t.Errorf("schema_version after idempotent migration = %q, want %q", version, "1.8.0")
	}
}
