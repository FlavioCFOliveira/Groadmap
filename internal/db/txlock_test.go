package db

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== TRANSACTION LOCK MODE TESTS ====================
//
// SPEC/IMPLEMENTATION.md § Transaction Lock Mode: every read-write transaction
// is begun IMMEDIATE, so it takes the write lock at BEGIN, where SQLite invokes
// the busy handler and busy_timeout applies. A read-only open keeps SQLite's
// DEFERRED default. TestDSNForProducesAFileURI pins the DSN text; the tests
// below pin what the engine does with it.

// openLockProbe opens a second, independent connection to the database at
// dbPath with busy_timeout=0, so a lock it cannot take is reported at once
// instead of being waited for. It stands for another process: it shares no
// pool with the connection under test.
func openLockProbe(t *testing.T, dbPath string) *sql.Conn {
	t.Helper()

	connector, err := sqlite.NewConnector("file:" + uriPath(dbPath) + "?_busy_timeout=0")
	if err != nil {
		t.Fatalf("building the probe connector: %v", err)
	}
	probeDB := sql.OpenDB(connector)
	t.Cleanup(func() { probeDB.Close() })

	conn, err := probeDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("opening the probe connection: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// probeTakesWriteLock reports whether the probe could take the write lock, and
// releases it again when it could. Any failure other than SQLITE_BUSY or
// SQLITE_LOCKED fails the test, so a broken probe is never read as a held lock.
func probeTakesWriteLock(t *testing.T, probe *sql.Conn) bool {
	t.Helper()

	ctx := context.Background()
	if _, err := probe.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		if !isLockedError(err) {
			t.Fatalf("probe BEGIN IMMEDIATE failed for a reason other than a held lock: %v", err)
		}
		return false
	}
	if _, err := probe.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("probe ROLLBACK: %v", err)
	}
	return true
}

// TestReadWriteTransactionTakesTheWriteLockAtBegin proves that a transaction on
// a read-write open holds the write lock from BEGIN, before it has read or
// written anything, and that a read-only open and a ReadOnly transaction do not.
// It is the engine-level half of the regression for rmp task #593: begun
// DEFERRED, the transaction below would hold no lock at all until its first
// statement, and the probe would take the write lock.
func TestReadWriteTransactionTakesTheWriteLockAtBegin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmap = "settlement-engine"
	database, err := Open(roadmap)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	dbPath, err := utils.GetRoadmapPath(roadmap)
	if err != nil {
		t.Fatalf("GetRoadmapPath: %v", err)
	}
	probe := openLockProbe(t, dbPath)

	// Positive control: with no transaction open the probe takes the lock, so
	// a refusal below is attributable to the transaction under test.
	if !probeTakesWriteLock(t, probe) {
		t.Fatal("the probe cannot take the write lock on an idle database; the test cannot discriminate")
	}

	t.Run("read-write open begins IMMEDIATE", func(t *testing.T) {
		tx, err := database.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		defer tx.Rollback() //nolint:errcheck // the transaction only holds a lock; nothing to commit

		if probeTakesWriteLock(t, probe) {
			t.Error("a transaction begun on a read-write open holds no write lock at BEGIN; it was begun DEFERRED, not IMMEDIATE")
		}
	})

	t.Run("ReadOnly transaction on a read-write open begins DEFERRED", func(t *testing.T) {
		tx, err := database.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatalf("BeginTx(ReadOnly): %v", err)
		}
		defer tx.Rollback() //nolint:errcheck // read-only transaction; nothing to commit

		var tasks int
		if err := tx.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&tasks); err != nil {
			t.Fatalf("reading inside the read-only transaction: %v", err)
		}
		if !probeTakesWriteLock(t, probe) {
			t.Error("a ReadOnly transaction took the write lock; it must be begun with a plain BEGIN")
		}
	})

	if err := database.Close(); err != nil {
		t.Fatalf("closing the read-write open: %v", err)
	}

	t.Run("read-only open begins DEFERRED", func(t *testing.T) {
		readOnly, err := OpenReadOnly(roadmap)
		if err != nil {
			t.Fatalf("OpenReadOnly: %v", err)
		}
		defer readOnly.Close()

		tx, err := readOnly.Begin()
		if err != nil {
			t.Fatalf("Begin on the read-only open: %v", err)
		}
		defer tx.Rollback() //nolint:errcheck // read-only transaction; nothing to commit

		var tasks int
		if err := tx.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&tasks); err != nil {
			t.Fatalf("reading inside the read-only transaction: %v", err)
		}
		if !probeTakesWriteLock(t, probe) {
			t.Error("a transaction on a read-only open took the write lock; a read-only open must not carry _txlock=immediate")
		}
	})
}

// TestConcurrentSprintAdditionsFromSeparateOpens is the regression for rmp task
// #593. Twenty writers each add one distinct BACKLOG task to the same sprint at
// the same moment. Each writer has an Open of its own -- its own connection pool
// and its own SQLite connections -- which is what twenty `rmp sprint add-tasks`
// processes are to the engine. AddTasksToSprint reads the placements of its
// tasks before it writes, so begun DEFERRED its transaction asked for the write
// lock only after it had read, SQLite refused that upgrade with SQLITE_BUSY
// without invoking the busy handler, and several writers exhausted the retry
// policy. Begun IMMEDIATE, every writer waits at BEGIN under busy_timeout and
// all twenty succeed.
func TestConcurrentSprintAdditionsFromSeparateOpens(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const (
		roadmap = "payments-q4"
		writers = 20
	)

	seed, err := Open(roadmap)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	sprintID := mustSeedSprint(t, seed, &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Settlement hardening",
		Description: "Close the reconciliation gaps before the audit.",
		CreatedAt:   "2026-09-28T09:00:00Z",
	})
	areas := []string{"refund", "dispute", "invoice", "ledger export", "payout", "chargeback"}
	backlog := make([]int, 0, 3*writers)
	for i := range 3 * writers {
		backlog = append(backlog, mustSeedTask(t, seed, &models.Task{
			Status:                 models.StatusBacklog,
			Title:                  fmt.Sprintf("Reconcile %s batch %d against the provider daily report", areas[i%len(areas)], i+1),
			FunctionalRequirements: "Finance operations must close the month without manual spreadsheets.",
			TechnicalRequirements:  "Extend the payments service behind its existing repository interface.",
			AcceptanceCriteria:     "Covered by an integration test against the provider sandbox.",
			CreatedAt:              "2026-09-28T09:05:00Z",
		}))
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("closing the seeding open: %v", err)
	}

	chosen := make([]int, 0, writers)
	for i := 0; i < len(backlog) && len(chosen) < writers; i += 3 {
		chosen = append(chosen, backlog[i])
	}

	// Every writer opens before any of them writes, so the additions start
	// together rather than in the order the opens happen to finish.
	opens := make([]*DB, writers)
	for i := range opens {
		database, err := Open(roadmap)
		if err != nil {
			t.Fatalf("writer %d: Open: %v", i, err)
		}
		opens[i] = database
		t.Cleanup(func() { database.Close() })
	}

	start := make(chan struct{})
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			<-start
			errs[i] = opens[i].AddTasksToSprint(ctx, sprintID, []int{chosen[i]})
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d adding task %d: %v", i, chosen[i], err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	check, err := Open(roadmap)
	if err != nil {
		t.Fatalf("Open for the check: %v", err)
	}
	defer check.Close()
	ctx := context.Background()

	members, err := check.GetSprintTasks(ctx, sprintID)
	if err != nil {
		t.Fatalf("GetSprintTasks: %v", err)
	}
	slices.Sort(members)
	if !slices.Equal(members, chosen) {
		t.Errorf("the sprint holds tasks %v, want %v", members, chosen)
	}

	var positions []int
	rows, err := check.QueryContext(ctx, "SELECT position FROM sprint_tasks WHERE sprint_id = ? ORDER BY position", sprintID)
	if err != nil {
		t.Fatalf("reading positions: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scanning a position: %v", err)
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating positions: %v", err)
	}
	for i, p := range positions {
		if p != i {
			t.Fatalf("sprint positions %v, want the dense run 0..%d", positions, writers-1)
		}
	}

	var inSprint, audited int
	if err := check.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE status = ?", models.StatusSprint).Scan(&inSprint); err != nil {
		t.Fatalf("counting SPRINT tasks: %v", err)
	}
	if inSprint != writers {
		t.Errorf("%d tasks have status SPRINT, want %d", inSprint, writers)
	}
	if err := check.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit WHERE operation = ?", models.OpSprintAddTask).Scan(&audited); err != nil {
		t.Fatalf("counting audit entries: %v", err)
	}
	if audited != writers {
		t.Errorf("%d %s audit entries, want %d", audited, models.OpSprintAddTask, writers)
	}
}
