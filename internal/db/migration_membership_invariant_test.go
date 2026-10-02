package db

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== MIGRATION 1.15.0 -> 1.16.0 ====================
//
// The gates of SPEC/VERSION.md § Migration 1.15.0 → 1.16.0 (rmp task 575): a
// roadmap written before the sprint membership invariant was enforced is
// repaired on the next open — every sprint member in BACKLOG becomes SPRINT
// (repair B), every task in SPRINT, DOING or TESTING outside every sprint
// returns to BACKLOG (repair A) — with one audit entry per repaired task, the
// guard then verifies the whole roadmap, and a second open changes nothing.
//
// The fixture holds both kinds of violation next to every kind of row the
// migration must leave alone, so each of the eight acceptance criteria has an
// assertion that can fail.

// membershipFixture1150 names the tasks of the 1.15.0 fixture by the role each
// plays in the migration.
type membershipFixture1150 struct {
	sprintA, sprintB int
	// Repair B: sprint members in BACKLOG.
	backlogMemberA, backlogMemberB int
	// Repair A: active tasks outside every sprint.
	orphanSprint, orphanDoing, orphanTesting int
	// Left alone.
	backlogLoner, completedLoner, doingMember, completedMember int
}

const (
	migTS1150Created = "2026-08-03T09:00:00.000Z"
	migTS1150Started = "2026-08-04T10:15:00.000Z"
	migTS1150Tested  = "2026-08-05T11:30:00.000Z"
	migTS1150Closed  = "2026-08-06T12:45:00.000Z"
)

// buildRoadmapAtSchema1150 creates a real on-disk roadmap under the test HOME,
// writes the fixture through direct SQL — the states it holds are exactly the
// ones no command can produce any more — and takes it back to schema 1.15.0.
// Nothing between 1.15.0 and 1.16.0 changed a table, a column or an index, and
// 1.17.0 only dropped idx_sprints_created_at, so a current database with that
// index restored and its schema_version reset is a faithful 1.15.0 database.
// It returns with the database CLOSED.
func buildRoadmapAtSchema1150(t *testing.T, roadmapName string) membershipFixture1150 {
	t.Helper()

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", roadmapName, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	var f membershipFixture1150
	f.sprintA = mustSeedSprint(t, database, &models.Sprint{Title: "Acquirer failover",
		Description: "Keep card payments clearing while an acquirer is down.", Status: models.SprintPending, CreatedAt: migTS1150Created})
	f.sprintB = mustSeedSprint(t, database, &models.Sprint{Title: "Settlement reporting",
		Description: "Report every settlement batch to finance before noon.", Status: models.SprintPending, CreatedAt: migTS1150Created})

	task := func(title string, status models.TaskStatus) int {
		t.Helper()
		return mustSeedTask(t, database, &models.Task{
			Title: title, Type: models.TypeTask, Status: status, Priority: 5, Severity: 3,
			FunctionalRequirements: "Payments keep clearing through the secondary acquirer.",
			TechnicalRequirements:  "Route by health check and replay the queued authorisations.",
			AcceptanceCriteria:     "A simulated outage loses no authorisation.",
			CreatedAt:              migTS1150Created,
		})
	}
	f.backlogMemberA = task("Route authorisations by acquirer health", models.StatusBacklog)
	f.backlogMemberB = task("Replay queued authorisations after failover", models.StatusBacklog)
	f.orphanSprint = task("Alert on acquirer latency", models.StatusSprint)
	f.orphanDoing = task("Record the failover decision in the audit log", models.StatusDoing)
	f.orphanTesting = task("Expose the acquirer health endpoint", models.StatusTesting)
	f.backlogLoner = task("Evaluate a third acquirer", models.StatusBacklog)
	f.completedLoner = task("Close the 2025 acquirer review", models.StatusCompleted)
	f.doingMember = task("Reconcile the settlement batch totals", models.StatusDoing)
	f.completedMember = task("Publish the settlement report format", models.StatusCompleted)

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	// Lifecycle fields the repairs must clear, or keep.
	exec(`UPDATE tasks SET started_at = ?, commit_open = ?, commit_close = ? WHERE id = ?`,
		migTS1150Started, "5f93b51", "391cff7", f.orphanDoing)
	exec(`UPDATE tasks SET started_at = ?, tested_at = ?, commit_open = ?, completion_summary = ? WHERE id = ?`,
		migTS1150Started, migTS1150Tested, "a1b2c3d", "Health endpoint shipped behind a flag.", f.orphanTesting)
	exec(`UPDATE tasks SET commit_open = ? WHERE id = ?`, "0badc0d", f.backlogMemberA)
	exec(`UPDATE tasks SET started_at = ?, commit_open = ? WHERE id = ?`, migTS1150Started, "c0ffee1", f.doingMember)
	exec(`UPDATE tasks SET started_at = ?, tested_at = ?, closed_at = ?, commit_open = ?, commit_close = ? WHERE id IN (?, ?)`,
		migTS1150Started, migTS1150Tested, migTS1150Closed, "d00d1e5", "feedfac", f.completedLoner, f.completedMember)

	// Memberships: two BACKLOG members in sprint A among valid ones, one in B.
	for _, row := range []struct{ sprint, task, position int }{
		{f.sprintA, f.doingMember, 0},
		{f.sprintA, f.backlogMemberA, 1},
		{f.sprintA, f.completedMember, 2},
		{f.sprintB, f.backlogMemberB, 0},
	} {
		exec(`INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) VALUES (?, ?, ?, ?)`,
			row.sprint, row.task, migTS1150Created, row.position)
	}

	restoreSprintsCreatedAtIndex(t, database)
	exec(`UPDATE _metadata SET value = '1.15.0' WHERE key = 'schema_version'`)
	return f
}

// openRawRoadmap opens a roadmap's database with the production DSN and runs no
// migration, so the state before the migration can be read and a single
// migration can be run by hand.
func openRawRoadmap(t *testing.T, roadmapName string) *DB {
	t.Helper()
	path, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		t.Fatalf("resolving the roadmap path: %v", err)
	}
	sqlDB, err := sql.Open("sqlite", dsnFor(path, false))
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	database := &DB{DB: sqlDB, queryCache: NewQueryCache(), batchProc: NewBatchProcessor(100)}
	t.Cleanup(func() { database.Close() })
	return database
}

// migTaskRow is one task row as the migration may rewrite it.
type migTaskRow struct {
	status, startedAt, testedAt, closedAt, summary, commitOpen, commitClose string
	sprint, position                                                        int
}

func readMigTaskRow(t *testing.T, database *DB, id int) migTaskRow {
	t.Helper()
	var r migTaskRow
	var started, tested, closed, summary, open, closeHash sql.NullString
	var sprint, position sql.NullInt64
	if err := database.QueryRow(
		`SELECT t.status, t.started_at, t.tested_at, t.closed_at, t.completion_summary, t.commit_open, t.commit_close,
		        st.sprint_id, st.position
		   FROM tasks t LEFT JOIN sprint_tasks st ON st.task_id = t.id WHERE t.id = ?`, id,
	).Scan(&r.status, &started, &tested, &closed, &summary, &open, &closeHash, &sprint, &position); err != nil {
		t.Fatalf("reading task %d: %v", id, err)
	}
	r.startedAt, r.testedAt, r.closedAt = started.String, tested.String, closed.String
	r.summary, r.commitOpen, r.commitClose = summary.String, open.String, closeHash.String
	r.sprint, r.position = int(sprint.Int64), int(position.Int64)
	if !sprint.Valid {
		r.sprint, r.position = 0, -1
	}
	return r
}

// migAuditRow is one audit row the migration wrote.
type migAuditRow struct {
	operation, entityType, performedAt string
	entityID                           int
	related                            sql.NullInt64
	commitHash                         sql.NullString
}

func auditRowsAfter(t *testing.T, database *DB, lastID int) []migAuditRow {
	t.Helper()
	rows, err := database.Query(`SELECT operation, entity_type, entity_id, related_entity_id, commit_hash, performed_at
		FROM audit WHERE id > ? ORDER BY id`, lastID)
	if err != nil {
		t.Fatalf("reading audit rows: %v", err)
	}
	defer rows.Close()
	var out []migAuditRow
	for rows.Next() {
		var r migAuditRow
		if err := rows.Scan(&r.operation, &r.entityType, &r.entityID, &r.related, &r.commitHash, &r.performedAt); err != nil {
			t.Fatalf("scanning an audit row: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func maxAuditID(t *testing.T, database *DB) int {
	t.Helper()
	var id sql.NullInt64
	if err := database.QueryRow(`SELECT MAX(id) FROM audit`).Scan(&id); err != nil {
		t.Fatalf("reading the last audit id: %v", err)
	}
	return int(id.Int64)
}

// TestMigrateV1_15_0_toV1_16_0_RepairsBothHalvesOnNextOpen settles acceptance
// criteria 1 to 5 and 8, and criterion 6 by a second open.
func TestMigrateV1_15_0_toV1_16_0_RepairsBothHalvesOnNextOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const roadmapName = "acquirer-failover"
	f := buildRoadmapAtSchema1150(t, roadmapName)

	// The 1.15.0 state, read before the migration runs.
	pre := openRawRoadmap(t, roadmapName)
	allTasks := []int{f.backlogMemberA, f.backlogMemberB, f.orphanSprint, f.orphanDoing, f.orphanTesting,
		f.backlogLoner, f.completedLoner, f.doingMember, f.completedMember}
	before := map[int]migTaskRow{}
	for _, id := range allTasks {
		before[id] = readMigTaskRow(t, pre, id)
	}
	lastAudit := maxAuditID(t, pre)
	pre.Close() //nolint:errcheck // reopened through Open below

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("opening the 1.15.0 roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	// Criterion 8: the migration set runs through 1.16.0 and on to the current version.
	if version, err := database.GetSchemaVersion(); err != nil || version != "1.17.0" {
		t.Fatalf("schema_version after open = %q (%v), want 1.17.0", version, err)
	}

	// Criterion 1: neither half is broken any more.
	var orphans, backlogMembers int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status IN ('SPRINT', 'DOING', 'TESTING') AND id NOT IN (SELECT task_id FROM sprint_tasks)`).Scan(&orphans); err != nil {
		t.Fatalf("criterion 1, first query: %v", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM sprint_tasks st JOIN tasks t ON t.id = st.task_id WHERE t.status = 'BACKLOG'`).Scan(&backlogMembers); err != nil {
		t.Fatalf("criterion 1, second query: %v", err)
	}
	if orphans != 0 || backlogMembers != 0 {
		t.Errorf("after the migration %d active tasks are outside every sprint and %d members are in BACKLOG, want 0 and 0",
			orphans, backlogMembers)
	}

	// Criterion 2: repair B changes the status and nothing else.
	for _, id := range []int{f.backlogMemberA, f.backlogMemberB} {
		want := before[id]
		want.status = string(models.StatusSprint)
		if got := readMigTaskRow(t, database, id); got != want {
			t.Errorf("repaired member %d reads %+v, want %+v", id, got, want)
		}
	}

	// Criterion 4: repair A clears what removal from a sprint clears and keeps
	// commit_open; the rows the migration must leave alone are unchanged.
	for _, id := range []int{f.orphanSprint, f.orphanDoing, f.orphanTesting} {
		want := before[id]
		want.status = string(models.StatusBacklog)
		want.startedAt, want.testedAt, want.closedAt, want.summary, want.commitClose = "", "", "", "", ""
		if got := readMigTaskRow(t, database, id); got != want {
			t.Errorf("repaired orphan %d reads %+v, want %+v", id, got, want)
		}
	}
	for _, id := range []int{f.backlogLoner, f.completedLoner, f.doingMember, f.completedMember} {
		if got := readMigTaskRow(t, database, id); got != before[id] {
			t.Errorf("task %d, which breaks no half, changed: %+v -> %+v", id, before[id], got)
		}
	}

	// Criteria 3 and 4: exactly the entries of the two repairs, one each, one timestamp.
	written := auditRowsAfter(t, database, lastAudit)
	want := map[int]migAuditRow{
		f.orphanSprint:   {operation: "TASK_STATUS_BACKLOG", entityType: "TASK", entityID: f.orphanSprint},
		f.orphanDoing:    {operation: "TASK_STATUS_BACKLOG", entityType: "TASK", entityID: f.orphanDoing},
		f.orphanTesting:  {operation: "TASK_STATUS_BACKLOG", entityType: "TASK", entityID: f.orphanTesting},
		f.backlogMemberA: {operation: "TASK_STATUS_SPRINT", entityType: "TASK", entityID: f.backlogMemberA, related: sql.NullInt64{Int64: int64(f.sprintA), Valid: true}},
		f.backlogMemberB: {operation: "TASK_STATUS_SPRINT", entityType: "TASK", entityID: f.backlogMemberB, related: sql.NullInt64{Int64: int64(f.sprintB), Valid: true}},
	}
	if len(written) != len(want) {
		t.Fatalf("the migration wrote %d audit entries %+v, want %d", len(written), written, len(want))
	}
	stamp := written[0].performedAt
	iso := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
	if !iso.MatchString(stamp) {
		t.Errorf("performed_at %q is not in the ISO 8601 UTC form", stamp)
	}
	for _, r := range written {
		w, ok := want[r.entityID]
		if !ok {
			t.Errorf("the migration wrote an entry for task %d, which it had nothing to repair: %+v", r.entityID, r)
			continue
		}
		delete(want, r.entityID)
		w.performedAt = stamp
		if r != w {
			t.Errorf("entry for task %d is %+v, want %+v", r.entityID, r, w)
		}
	}

	// Criterion 5: no trigger.
	var triggers int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger'`).Scan(&triggers); err != nil {
		t.Fatalf("counting triggers: %v", err)
	}
	if triggers != 0 {
		t.Errorf("the migration left %d trigger(s), want none", triggers)
	}

	// Criterion 6: a second application, directly and through a second open, is a no-op.
	once := tableContents(t, database)
	if err := database.runMigration(Migration{Version: "1.16.0", Name: "re-apply", Apply: migrateV1_15_0_toV1_16_0}); err != nil {
		t.Fatalf("re-applying the migration: %v", err)
	}
	database.Close() //nolint:errcheck // reopened below
	again, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("opening a second time: %v", err)
	}
	defer again.Close() //nolint:errcheck // test cleanup
	if twice := tableContents(t, again); twice != once {
		t.Errorf("re-applying the migration changed rows.\nonce:\n%s\ntwice:\n%s", once, twice)
	}
}

// TestMigrateV1_15_0_toV1_16_0_FailureRollsBack settles criterion 7: a failure
// after every step has run leaves the version at 1.15.0 and the tasks and audit
// tables exactly as they were, the audit entries of the repairs included.
func TestMigrateV1_15_0_toV1_16_0_FailureRollsBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const roadmapName = "acquirer-failover-rollback"
	buildRoadmapAtSchema1150(t, roadmapName)

	rw := openRawRoadmap(t, roadmapName)
	before := tableContents(t, rw)
	injected := errors.New("injected failure after the last step")
	err := rw.runMigration(Migration{Version: "1.16.0", Name: "fails after its last step",
		Apply: func(tx *sql.Tx) error {
			if err := migrateV1_15_0_toV1_16_0(tx); err != nil {
				return err
			}
			return injected
		}})
	if !errors.Is(err, injected) {
		t.Fatalf("runMigration returned %v, want the injected failure", err)
	}
	if after := tableContents(t, rw); after != before {
		t.Errorf("a failed migration changed rows.\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if version, err := rw.GetSchemaVersion(); err != nil || version != "1.15.0" {
		t.Errorf("schema_version after a failed migration = %q (%v), want 1.15.0", version, err)
	}
}

// TestMigrateV1_15_0_toV1_16_0_IsRegistered pins the registration: the
// migration that targets 1.16.0 is this one.
func TestMigrateV1_15_0_toV1_16_0_IsRegistered(t *testing.T) {
	i := slices.IndexFunc(migrations, func(m Migration) bool { return m.Version == "1.16.0" })
	if i < 0 {
		t.Fatal("no migration targets 1.16.0")
	}
	if got := migrations[i]; fmt.Sprintf("%p", got.Apply) != fmt.Sprintf("%p", migrateV1_15_0_toV1_16_0) {
		t.Errorf("the migration to 1.16.0 is %s, want the one applied by migrateV1_15_0_toV1_16_0", got.Name)
	}
}
