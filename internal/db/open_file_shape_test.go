package db

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The tests in this file pin SPEC/DATABASE.md § Opening a Roadmap Database File
// and SPEC/VERSION.md § Database Schema Newer Than the Binary: what a
// project.db is decides, before any statement runs, whether the open
// initialises it, refuses it, or opens it.

// homeWithFile gives the test a fresh HOME holding roadmap name whose
// project.db carries exactly content, and returns the file's path.
func homeWithFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := utils.EnsureRoadmapDir(name); err != nil {
		t.Fatal(err)
	}
	path, err := utils.GetRoadmapPath(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, utils.DBFilePerm); err != nil {
		t.Fatal(err)
	}
	return path
}

// homeEntries lists the roadmap home directory of path.
func homeEntries(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestOpen_ZeroByteFileIsInitialised(t *testing.T) {
	path := homeWithFile(t, "empty-ledger", nil)
	database, err := Open("empty-ledger")
	if err != nil {
		t.Fatalf("opening a zero-byte project.db: %v", err)
	}
	defer database.Close()
	version, err := database.GetSchemaVersion()
	if err != nil || version != SchemaVersion {
		t.Fatalf("schema version %q (%v), want %s", version, err, SchemaVersion)
	}
	var rows int
	if err := database.QueryRow("SELECT COUNT(*) FROM _metadata").Scan(&rows); err != nil || rows != 3 {
		t.Errorf("_metadata holds %d rows (%v), want 3", rows, err)
	}
	if _, err := database.ListTasks(context.Background(), nil); err != nil {
		t.Errorf("the initialised roadmap cannot be read: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Errorf("project.db is still empty after the open: %v", err)
	}
}

// TestOpen_ZeroByteFileTwoConcurrentOpens: two invocations that find the same
// empty file both succeed, the schema is created once, and neither receives the
// driver's text.
func TestOpen_ZeroByteFileTwoConcurrentOpens(t *testing.T) {
	for round := 0; round < 10; round++ {
		name := fmt.Sprintf("empty-race-%d", round)
		homeWithFile(t, name, nil)

		const openers = 4
		errs := make([]error, openers)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < openers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				database, err := Open(name)
				if err == nil {
					err = database.Close()
				}
				errs[i] = err
			}(i)
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Errorf("round %d: opener %d failed: %v", round, i, err)
			}
		}

		database, err := Open(name)
		if err != nil {
			t.Fatal(err)
		}
		var rows, created int
		if err := database.QueryRow("SELECT COUNT(*) FROM _metadata").Scan(&rows); err != nil || rows != 3 {
			t.Errorf("round %d: _metadata holds %d rows (%v), want one per key", round, rows, err)
		}
		if err := database.QueryRow("SELECT COUNT(DISTINCT value) FROM _metadata WHERE key = 'created_at'").Scan(&created); err != nil || created != 1 {
			t.Errorf("round %d: %d created_at values, want 1", round, created)
		}
		_ = database.Close()
	}
}

func TestOpen_NonSQLiteFileIsRefusedAndLeftAsItIs(t *testing.T) {
	cases := map[string][]byte{
		"a zip archive":               []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00 settlement-report-2026-09.csv"),
		"shorter than the header":     []byte("SQLite"),
		"a header with a bad body":    append([]byte(sqliteHeader), bytes.Repeat([]byte{0xFF}, 4080)...),
		"plain text release notes":    []byte("Release 1.4: refunds are now idempotent.\n"),
		"the header and nothing else": []byte(sqliteHeader),
	}
	for label, content := range cases {
		t.Run(label, func(t *testing.T) {
			path := homeWithFile(t, "not-a-roadmap", content)
			before := homeEntries(t, path)

			for _, open := range []struct {
				name string
				fn   func(string) (*DB, error)
			}{{"Open", Open}, {"OpenReadOnly", OpenReadOnly}} {
				database, err := open.fn("not-a-roadmap")
				if err == nil {
					_ = database.Close()
					t.Fatalf("%s accepted a file that is not a SQLite database", open.name)
				}
				want := "database error: " + path + " is not a valid roadmap database"
				if err.Error() != want || !errors.Is(err, utils.ErrDatabase) {
					t.Errorf("%s: %q, want %q (exit 1)", open.name, err.Error(), want)
				}
				if strings.Contains(err.Error(), "(26)") || strings.Contains(err.Error(), "file is not a database") {
					t.Errorf("%s: the driver's text reached the line: %v", open.name, err)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, content) {
				t.Errorf("the refused file changed: %v", err)
			}
			if got := homeEntries(t, path); strings.Join(got, ",") != strings.Join(before, ",") {
				t.Errorf("the refusal left files behind: before %v, after %v", before, got)
			}
		})
	}
}

// newerFixture creates roadmap name, sets its recorded schema version to
// version, leaves it in rollback-journal mode so that any connection setting
// written into it would change its header, and returns its path and bytes.
func newerFixture(t *testing.T, name, version string) (string, []byte) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := CreateRoadmapDatabase(name); err != nil {
		t.Fatal(err)
	}
	path, err := utils.GetRoadmapPath(name)
	if err != nil {
		t.Fatal(err)
	}
	conn := rawWritable(t, path)
	for _, stmt := range []string{
		"PRAGMA journal_mode = DELETE",
		fmt.Sprintf("UPDATE _metadata SET value = '%s' WHERE key = 'schema_version'", version),
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, data
}

// rawWritable opens path with the driver directly, writable.
func rawWritable(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestOpen_NewerSchemaIsRefusedForEveryOpenAndNothingIsWritten(t *testing.T) {
	// 1.100.0 is newer than 1.17.0 numerically and older lexically: the
	// comparison must be the numeric one.
	for _, version := range []string{"1.100.0", "2.0.0", "1.17.1"} {
		t.Run(version, func(t *testing.T) {
			path, before := newerFixture(t, "future-ledger", version)
			for _, open := range []struct {
				name string
				fn   func(string) (*DB, error)
			}{{"Open", Open}, {"OpenExisting", OpenExisting}, {"OpenReadOnly", OpenReadOnly}} {
				database, err := open.fn("future-ledger")
				if err == nil {
					_ = database.Close()
					t.Fatalf("%s opened a database at schema %s", open.name, version)
				}
				want := fmt.Sprintf("database error: %s has schema version %s, newer than schema version %s supported by this rmp; upgrade rmp to open it",
					path, version, SchemaVersion)
				if err.Error() != want || !errors.Is(err, utils.ErrDatabase) {
					t.Errorf("%s: %q, want %q", open.name, err.Error(), want)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, before) {
				t.Errorf("the refused database changed on disk (%v)", err)
			}
		})
	}
}

func TestOpen_OlderAndEqualSchemaVersionsAreNotRefused(t *testing.T) {
	for _, version := range []string{SchemaVersion, "1.9.0"} {
		t.Run(version, func(t *testing.T) {
			newerFixture(t, "current-ledger", version)
			database, err := Open("current-ledger")
			if err != nil {
				t.Fatalf("opening a database at %s: %v", version, err)
			}
			_ = database.Close()
		})
	}
}

// TestCreateRoadmapDatabase_ExactlyOneConcurrentCreatorWins pins
// SPEC/COMMANDS.md § Create Roadmap, "Creation is atomic against concurrent
// creators": one creator succeeds, every other one receives the already-exists
// refusal, and the home directory holds only the complete project.db.
func TestCreateRoadmapDatabase_ExactlyOneConcurrentCreatorWins(t *testing.T) {
	for round := 0; round < 10; round++ {
		t.Setenv("HOME", t.TempDir())
		const name = "payments-race"
		const creators = 6
		errs := make([]error, creators)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < creators; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = CreateRoadmapDatabase(name)
			}(i)
		}
		close(start)
		wg.Wait()

		won := 0
		for _, err := range errs {
			switch {
			case err == nil:
				won++
			case err.Error() != `resource already exists: roadmap "payments-race" already exists` || !errors.Is(err, utils.ErrAlreadyExists):
				t.Errorf("round %d: a loser received %q", round, err)
			}
		}
		if won != 1 {
			t.Errorf("round %d: %d creators won, want exactly 1", round, won)
		}
		path, _ := utils.GetRoadmapPath(name)
		if got := homeEntries(t, path); strings.Join(got, ",") != utils.DBFileName {
			t.Errorf("round %d: the home holds %v, want only %s", round, got, utils.DBFileName)
		}
		database, err := OpenExisting(name)
		if err != nil {
			t.Fatalf("round %d: the created roadmap cannot be opened: %v", round, err)
		}
		if v, err := database.GetSchemaVersion(); err != nil || v != SchemaVersion {
			t.Errorf("round %d: the published database is at %q (%v)", round, v, err)
		}
		_ = database.Close()
	}
}

// fakeCoded is an error carrying a SQLite result code, as the driver's does.
type fakeCoded struct{ code int }

func (e fakeCoded) Error() string { return fmt.Sprintf("disk I/O error (%d)", e.code) }
func (e fakeCoded) Code() int     { return e.code }

func TestClassifyDriverError(t *testing.T) {
	driver := fmt.Errorf("checking active tasks: %w", fakeCoded{code: 10})
	got := ClassifyDriverError(driver)
	if !errors.Is(got, utils.ErrDatabase) || got.Error() != "database error: checking active tasks: disk I/O error (10)" {
		t.Errorf("an unclassified driver failure became %q", got)
	}
	classified := fmt.Errorf("%w: sprint #4 is already open — close it first", utils.ErrValidation)
	if ClassifyDriverError(classified) != classified {
		t.Errorf("a classified error was rewrapped")
	}
	plain := errors.New("flag parsing failed")
	if ClassifyDriverError(plain) != plain {
		t.Errorf("an error carrying no driver code was rewrapped")
	}
	if ClassifyDriverError(nil) != nil {
		t.Errorf("nil was rewrapped")
	}
	// The migration failure has a published line of its own, without a
	// sentinel (SPEC/DATABASE.md § The failure surface).
	var migration error = &migrationFailure{err: fmt.Errorf("migration 1.13.0 failed: applying migration: %w", fakeCoded{code: 1})}
	if got := ClassifyDriverError(migration); got != migration ||
		got.Error() != "running migrations: migration 1.13.0 failed: applying migration: disk I/O error (1)" {
		t.Errorf("the migration failure line became %q", got)
	}
}
