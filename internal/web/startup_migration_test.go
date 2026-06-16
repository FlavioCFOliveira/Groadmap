package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// buildStaleSchemaDB creates a real on-disk roadmap database at the v1.6.0
// schema level: the sprints table has no `title` column and no `order_index`
// column (both added by migrations 1.7.0 and 1.8.0 respectively). It mirrors
// the shape built by TestMigrateV1_6_0_toV1_7_0 in internal/db/migrations_test.go,
// adapted to write a real file under the test HOME rather than an in-memory DB
// so migrateRoadmapsAtStartup (which calls db.Open by name, not by path) can
// discover and migrate it.
//
// The function writes the database file, sets its _metadata schema_version to
// "1.6.0", inserts one sprint row to make the table non-empty, and closes the
// raw *sql.DB before returning so the caller can re-open via db.Open.
func buildStaleSchemaDB(t *testing.T, roadmapName string) {
	t.Helper()

	// Ensure the roadmap home directory exists with correct permissions.
	if err := utils.EnsureRoadmapDir(roadmapName); err != nil {
		t.Fatalf("creating roadmap dir for %q: %v", roadmapName, err)
	}

	dbPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		t.Fatalf("resolving db path for %q: %v", roadmapName, err)
	}

	// Pre-create the file at 0600 before sql.Open touches it (matches the
	// production path in db.Open which does the same to avoid umask exposure).
	f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, utils.DBFilePerm)
	if err != nil {
		t.Fatalf("pre-creating db file %s: %v", dbPath, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing pre-created db file: %v", err)
	}

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening stale db %s: %v", dbPath, err)
	}
	defer sqlDB.Close() //nolint:errcheck // test cleanup

	// Enable WAL mode and foreign keys so the file matches production.
	if _, err := sqlDB.Exec("PRAGMA journal_mode = WAL"); err != nil {
		t.Fatalf("enabling WAL: %v", err)
	}
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign keys: %v", err)
	}

	// Create a v1.6.0-shape schema: all tables are present as of 1.6.0, but
	// sprints lacks the `title` and `order_index` columns that 1.7.0 and 1.8.0
	// add.  The tasks, sprint_tasks, audit, _metadata, and task_dependencies
	// tables are present in their 1.6.0 forms.
	staleSchema := `
CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL CHECK(length(title) <= 255),
    status TEXT NOT NULL DEFAULT 'BACKLOG',
    type TEXT NOT NULL DEFAULT 'TASK',
    functional_requirements TEXT NOT NULL,
    technical_requirements TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,
    created_at TEXT NOT NULL,
    specialists TEXT,
    started_at TEXT,
    tested_at TEXT,
    closed_at TEXT,
    completion_summary TEXT,
    parent_task_id INTEGER REFERENCES tasks(id),
    priority INTEGER NOT NULL DEFAULT 0,
    severity INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sprints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    status TEXT NOT NULL DEFAULT 'PENDING',
    description TEXT NOT NULL,
    created_at TEXT NOT NULL,
    started_at TEXT,
    closed_at TEXT,
    max_tasks INTEGER
);

CREATE TABLE IF NOT EXISTS sprint_tasks (
    sprint_id INTEGER NOT NULL,
    task_id INTEGER NOT NULL UNIQUE,
    added_at TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (sprint_id, task_id)
);

CREATE TABLE IF NOT EXISTS audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id INTEGER NOT NULL,
    performed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS _metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id INTEGER NOT NULL,
    depends_on_task_id INTEGER NOT NULL,
    PRIMARY KEY (task_id, depends_on_task_id)
);
`
	if _, err := sqlDB.Exec(staleSchema); err != nil {
		t.Fatalf("creating stale schema: %v", err)
	}

	// Mark schema version as 1.6.0 in _metadata.
	now := time.Now().UTC().Format(time.RFC3339)
	for _, kv := range []struct{ k, v string }{
		{"schema_version", "1.6.0"},
		{"created_at", now},
		{"application", "Groadmap"},
	} {
		if _, err := sqlDB.Exec(
			"INSERT OR REPLACE INTO _metadata (key, value) VALUES (?, ?)",
			kv.k, kv.v,
		); err != nil {
			t.Fatalf("inserting metadata %s: %v", kv.k, err)
		}
	}

	// Insert one sprint row using the v1.6.0 shape (no title, no order_index)
	// so the table is non-empty and the migrations must backfill real rows.
	if _, err := sqlDB.Exec(
		"INSERT INTO sprints (status, description, created_at) VALUES ('PENDING', 'Harden the authentication pipeline', ?)",
		now,
	); err != nil {
		t.Fatalf("seeding stale sprint row: %v", err)
	}
}

// TestMigrateRoadmapsAtStartup_StaleDBBecomesCurrentSchema is the primary
// regression gate for the startup-migration feature (SPEC/WEB.md § Startup
// Schema Migration, Acceptance Criterion 41). It:
//
//  1. Builds a roadmap whose on-disk database is at schema v1.6.0 (sprints table
//     lacks `title` and `order_index`).
//  2. Calls migrateRoadmapsAtStartup() — the function introduced in server.go.
//  3. Asserts the schema_version is now the current SchemaVersion (1.8.0).
//  4. Asserts both previously-missing columns exist in the sprints table.
func TestMigrateRoadmapsAtStartup_StaleDBBecomesCurrentSchema(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "auth-pipeline-hardening"
	buildStaleSchemaDB(t, roadmapName)

	// Pre-condition: schema_version must be "1.6.0" before startup migration.
	{
		dbPath, err := utils.GetRoadmapPath(roadmapName)
		if err != nil {
			t.Fatalf("resolving db path: %v", err)
		}
		rawDB, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("opening raw db: %v", err)
		}
		var pre string
		if serr := rawDB.QueryRow("SELECT value FROM _metadata WHERE key = 'schema_version'").Scan(&pre); serr != nil {
			rawDB.Close() //nolint:errcheck // test cleanup
			t.Fatalf("reading pre-migration schema_version: %v", serr)
		}
		rawDB.Close() //nolint:errcheck // test cleanup
		if pre != "1.6.0" {
			t.Fatalf("pre-condition: schema_version = %q, want 1.6.0", pre)
		}
	}

	// Execute the function under test.
	migrateRoadmapsAtStartup()

	// Post-condition: re-open via db.Open (which calls GetSchemaVersion) and
	// confirm the schema is now at the current version.
	database, err := db.Open(roadmapName)
	if err != nil {
		t.Fatalf("re-opening roadmap after startup migration: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	version, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion after startup migration: %v", err)
	}
	if version != db.SchemaVersion {
		t.Errorf("schema_version = %q, want %q (current)", version, db.SchemaVersion)
	}

	// Confirm both previously-absent columns now exist.
	for _, col := range []string{"title", "order_index"} {
		var count int
		if serr := database.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('sprints') WHERE name = ?`, col,
		).Scan(&count); serr != nil {
			t.Fatalf("checking sprints.%s existence: %v", col, serr)
		}
		if count == 0 {
			t.Errorf("sprints.%s missing after startup migration", col)
		}
	}
}

// TestMigrateRoadmapsAtStartup_SprintsPageReturns200 is the end-to-end
// regression for the 500→200 fix (SPEC/WEB.md § Startup Schema Migration,
// Acceptance Criterion 42). Before the fix, a roadmap at v1.6.0 caused the
// sprints page to HTTP 500 because OpenReadOnly cannot run migrations. After
// the fix, migrateRoadmapsAtStartup brings the schema to current and the page
// returns 200.
//
// The test simulates the full fix path:
//  1. Build a stale-schema (v1.6.0) on-disk roadmap.
//  2. Run migrateRoadmapsAtStartup (the startup step in serve).
//  3. Drive GET /roadmaps/{name} through buildMux() and assert 200.
func TestMigrateRoadmapsAtStartup_SprintsPageReturns200(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "q3-performance-push"
	buildStaleSchemaDB(t, roadmapName)

	// Run the startup migration — this is what serve() calls before binding.
	migrateRoadmapsAtStartup()

	// Drive the sprints landing page through the httptest mux.
	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+roadmapName, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sprints page status = %d after startup migration, want 200\nbody: %s",
			rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeHTML {
		t.Errorf("content-type = %q, want %q", ct, contentTypeHTML)
	}
}

// TestMigrateRoadmapsAtStartup_NonFatalWhenOneRoadmapBroken covers
// SPEC/WEB.md § Startup Schema Migration rule 6: a per-roadmap failure is
// non-fatal — the function must not panic or abort, and the healthy roadmap
// is still migrated.
//
// Approach: one roadmap is represented by a regular FILE at the roadmap-home
// path (so utils.ListRoadmaps discovers it as a roadmap name but the
// underlying project.db is absent, making db.Open fail), while a second
// roadmap is a genuine stale database. The function must continue past the
// broken entry and migrate the healthy one.
func TestMigrateRoadmapsAtStartup_NonFatalWhenOneRoadmapBroken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	home := os.Getenv("HOME")
	roadmapsDir := filepath.Join(home, ".roadmaps")
	if err := os.MkdirAll(roadmapsDir, utils.DataDirPerm); err != nil {
		t.Fatalf("creating ~/.roadmaps: %v", err)
	}

	// "broken-roadmap": the roadmap-home directory exists as a regular FILE
	// instead of a directory, so db.Open("broken-roadmap") fails with ENOTDIR
	// when it tries to create the roadmap home under it. This exercises the
	// per-roadmap open-failure branch in migrateRoadmapsAtStartup.
	brokenHome := filepath.Join(roadmapsDir, "broken-roadmap")
	if err := os.WriteFile(brokenHome, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("writing broken roadmap entry: %v", err)
	}
	// Place a project.db INSIDE the "broken-roadmap" file to make
	// ListRoadmaps see it as a roadmap (ListRoadmaps looks for
	// ~/.roadmaps/<name>/project.db as a regular file; with the name itself
	// being a file we cannot do that, but ListRoadmaps reads subdirectories
	// so a file at ~/.roadmaps/broken-roadmap is NOT listed). Instead, place a
	// subdirectory named "broken-roadmap" but without a valid project.db so
	// db.Open fails at the schema step.

	// Re-do: ListRoadmaps returns subdirectories that contain project.db. To
	// simulate a broken roadmap that IS discovered but cannot be opened, we:
	// (a) create a real roadmap directory, (b) put a zero-byte project.db in
	// it (so ListRoadmaps lists it), but the zero-byte file is not a valid
	// SQLite database, so db.Open fails to configure it.
	if err := os.Remove(brokenHome); err != nil {
		t.Fatalf("removing placeholder file: %v", err)
	}
	if err := os.MkdirAll(brokenHome, utils.DataDirPerm); err != nil {
		t.Fatalf("creating broken roadmap home: %v", err)
	}
	brokenDB := filepath.Join(brokenHome, utils.DBFileName)
	if err := os.WriteFile(brokenDB, []byte("not a sqlite database\x00\x00garbage"), 0o600); err != nil {
		t.Fatalf("writing corrupt db file: %v", err)
	}

	// "analytics-platform": a healthy stale-schema roadmap that should be migrated.
	const healthyName = "analytics-platform"
	buildStaleSchemaDB(t, healthyName)

	// Confirm healthy roadmap is at 1.6.0 before the call.
	{
		healthyPath, err := utils.GetRoadmapPath(healthyName)
		if err != nil {
			t.Fatalf("resolving healthy path: %v", err)
		}
		rawDB, err := sql.Open("sqlite", healthyPath)
		if err != nil {
			t.Fatalf("opening healthy raw db: %v", err)
		}
		var pre string
		serr := rawDB.QueryRow("SELECT value FROM _metadata WHERE key = 'schema_version'").Scan(&pre)
		rawDB.Close() //nolint:errcheck // test cleanup
		if serr != nil {
			t.Fatalf("reading healthy pre-migration schema_version: %v", serr)
		}
		if pre != "1.6.0" {
			t.Fatalf("pre-condition: healthy schema_version = %q, want 1.6.0", pre)
		}
	}

	// Must not panic; the broken roadmap must not prevent the healthy one
	// from being migrated.
	migrateRoadmapsAtStartup()

	// The healthy roadmap is now at the current schema.
	healthyDB, err := db.Open(healthyName)
	if err != nil {
		t.Fatalf("opening healthy roadmap after startup migration: %v", err)
	}
	defer healthyDB.Close() //nolint:errcheck // test cleanup

	version, err := healthyDB.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion on healthy roadmap: %v", err)
	}
	if version != db.SchemaVersion {
		t.Errorf("healthy roadmap schema_version = %q, want %q (current)", version, db.SchemaVersion)
	}
}

// TestMigrateRoadmapsAtStartup_MultipleRoadmapsAllMigrated confirms that
// migrateRoadmapsAtStartup migrates every roadmap in the data directory, not
// just the first one (SPEC/WEB.md § Startup Schema Migration rule 2:
// "Migrates every existing roadmap").
func TestMigrateRoadmapsAtStartup_MultipleRoadmapsAllMigrated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	roadmaps := []string{"identity-service", "billing-engine", "notification-hub"}
	for _, name := range roadmaps {
		buildStaleSchemaDB(t, name)
	}

	migrateRoadmapsAtStartup()

	for _, name := range roadmaps {
		database, err := db.Open(name)
		if err != nil {
			t.Fatalf("re-opening %q after startup migration: %v", name, err)
		}
		version, verr := database.GetSchemaVersion()
		database.Close() //nolint:errcheck // test cleanup
		if verr != nil {
			t.Fatalf("GetSchemaVersion for %q: %v", name, verr)
		}
		if version != db.SchemaVersion {
			t.Errorf("roadmap %q schema_version = %q, want %q", name, version, db.SchemaVersion)
		}
	}
}

// TestMigrateRoadmapsAtStartup_IdempotentOnCurrentSchema verifies rule 3
// (SPEC/WEB.md § Startup Schema Migration): calling migrateRoadmapsAtStartup
// on roadmaps that are already at the current schema leaves them unchanged —
// the migration is a no-op and the function returns normally.
func TestMigrateRoadmapsAtStartup_IdempotentOnCurrentSchema(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "search-indexing-pipeline"
	// Use db.Open which creates a fresh current-schema database.
	freshDB, err := db.Open(roadmapName)
	if err != nil {
		t.Fatalf("creating fresh roadmap %q: %v", roadmapName, err)
	}
	freshDB.Close() //nolint:errcheck // test cleanup

	// Running startup migration against an already-current schema must not error
	// and must not alter the schema_version.
	migrateRoadmapsAtStartup()

	database, err := db.Open(roadmapName)
	if err != nil {
		t.Fatalf("re-opening %q after idempotent migration: %v", roadmapName, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	version, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}
	if version != db.SchemaVersion {
		t.Errorf("schema_version after idempotent startup migration = %q, want %q", version, db.SchemaVersion)
	}
}

// TestReadOnlyInvariant_PerRequestLoadersUseOpenReadOnly asserts that the
// per-request data loaders (loadSprints, loadTasks, loadSprint) open the
// database via OpenReadOnly (the query_only path) and therefore never migrate
// or write to the schema (SPEC/WEB.md § Read-Only Data Flow; finding #43).
//
// The test drives GET requests against a current-schema roadmap and confirms
// that the audit table gains no new rows and schema_version is not modified
// by the request — i.e. the handler performed a pure read. If any per-request
// loader were to accidentally call db.Open (the writable path), it would write
// an audit entry, which would be detected here.
func TestReadOnlyInvariant_PerRequestLoadersUseOpenReadOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmapName = "event-sourcing-refactor"
	// Seed the roadmap with current schema and a sprint so the sprints page has
	// data to render and is not vacuously empty.
	initial := seedRoadmap(t, roadmapName)

	// Capture audit row count and schema_version BEFORE any GET request.
	measureDB := func(label string) (auditCount int, version string) {
		t.Helper()
		database, err := db.Open(initial)
		if err != nil {
			t.Fatalf("[%s] opening db: %v", label, err)
		}
		defer database.Close() //nolint:errcheck // test cleanup
		if serr := database.QueryRow("SELECT COUNT(*) FROM audit").Scan(&auditCount); serr != nil {
			t.Fatalf("[%s] counting audit rows: %v", label, serr)
		}
		version, verr := database.GetSchemaVersion()
		if verr != nil {
			t.Fatalf("[%s] GetSchemaVersion: %v", label, verr)
		}
		return auditCount, version
	}

	auditBefore, versionBefore := measureDB("before")

	mux := buildMux()

	// Drive the three read-path pages — sprints landing, tasks, and the
	// graph-data endpoint — to exercise all per-request loader code paths.
	readPaths := []string{
		"/roadmaps/" + roadmapName,
		"/roadmaps/" + roadmapName + "/tasks",
		"/roadmaps/" + roadmapName + "/graph/data",
	}
	for _, path := range readPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", path, rec.Code)
		}
	}

	auditAfter, versionAfter := measureDB("after")

	// The audit table must not have gained rows: a writable open would have
	// produced an audit INSERT for any write operation.
	if auditAfter != auditBefore {
		t.Errorf("audit row count changed from %d to %d after read-only GET requests: per-request loaders must never write", auditBefore, auditAfter)
	}

	// The schema_version must be identical: a writable open that ran RunMigrations
	// could change it.
	if versionAfter != versionBefore {
		t.Errorf("schema_version changed from %q to %q after read-only GET requests: per-request loaders must never migrate", versionBefore, versionAfter)
	}
}
