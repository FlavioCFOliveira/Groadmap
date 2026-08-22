package db

import (
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== MIGRATION 1.12.0 -> 1.13.0 ====================
//
// The regression gates for rmp task #236: (sprint_id, position) becomes unique,
// so a sprint's planned execution order is total and the schema is what holds it
// that way (SPEC/VERSION.md § Migration 1.12.0 → 1.13.0, SPEC/DATABASE.md
// § Position Uniqueness Within a Sprint and § Introducing a Uniqueness
// Constraint over Existing Rows).
//
// Four properties carry the weight, and each is proved by an assertion that can
// fail:
//
//  1. The migration REPAIRS and TIGHTENS, and discards nothing. A duplicate
//     position is an ambiguous order, not a redundant membership, so both tasks
//     survive it and the set of (sprint_id, task_id) pairs is identical
//     afterwards.
//  2. The repair is run on BOTH data states. A database that already satisfies
//     the constraint must come out untouched; one that does not must come out
//     conforming, with the lower task id taking the lower position.
//  3. The constraint really is in force afterwards, on a migrated database and
//     on a fresh one alike, and it is carried by ONE index rather than two.
//  4. Every command that permutes positions still works, which is the half a
//     plain unique index breaks: reorder, move-to, top, bottom and swap all
//     assign sequentially over values that are still occupied, so each needs the
//     parking step to reach its result without a transient duplicate.

// sprintTasksOrderDDL1120 is the ordering index exactly as schema 1.12.0
// declared it: covering the same pair of columns, and NOT unique.
//
// It is transcribed rather than derived from the current schema, for the same
// reason auditDDL1110 is: a fixture for a historical schema must not follow
// later changes to the fresh-schema definition, or the migration it exercises
// stops being the migration that ships.
const sprintTasksOrderDDL1120 = `CREATE INDEX idx_sprint_tasks_order ON sprint_tasks(sprint_id, position ASC)`

// positionSeed is one sprint_tasks row of a 1.12.0 fixture, written by hand
// because no shipped write path can produce a colliding position.
type positionSeed struct {
	addedAt  string
	sprintID int
	taskID   int
	position int
}

// buildRoadmapAtSchema1120 creates a real on-disk roadmap under the test HOME
// and takes it back to schema 1.12.0, then overwrites its sprint_tasks
// positions with the given seeds.
//
// The roadmap is first created through the production path, which yields the
// current schema and every table 1.12.0 had; the sprints and tasks are written
// through the shipped write paths; and only then is the ordering index
// downgraded to its non-unique 1.12.0 form so the seeds below can be written at
// all. Only that index changed between 1.12.0 and 1.13.0, so restoring it is
// enough to produce a faithful 1.12.0 database while the other tables stay
// correct by construction.
//
// It returns the database CLOSED, at schema_version 1.12.0, ready to be reopened
// so that the production migration path runs against it.
func buildRoadmapAtSchema1120(t *testing.T, roadmapName string, sprintTitles []string, seeds []positionSeed) {
	t.Helper()

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", roadmapName, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := testContext()

	for i, title := range sprintTitles {
		id, err := database.CreateSprint(ctx, &models.Sprint{
			Title:       title,
			Description: "Clear the settlement backlog left by the acquirer outage.",
			Status:      models.SprintPending,
			CreatedAt:   "2026-05-0" + string(rune('1'+i)) + "T08:00:00.000Z",
		})
		if err != nil {
			t.Fatalf("creating fixture sprint %q: %v", title, err)
		}
		if id != i+1 {
			t.Fatalf("fixture sprint %q got id %d, want %d: the seeds address sprints by id", title, id, i+1)
		}
	}

	// One task per seed, created through the shipped write path so every
	// sprint_tasks row below points at a task that really exists.
	maxTaskID := 0
	for _, seed := range seeds {
		if seed.taskID > maxTaskID {
			maxTaskID = seed.taskID
		}
	}
	for i := 1; i <= maxTaskID; i++ {
		id, err := database.CreateTask(ctx, &models.Task{
			Title:                  fmt.Sprintf("Reconcile settlement batch %d", i),
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			FunctionalRequirements: "Every settlement batch must reconcile to the cent before the ledger closes.",
			TechnicalRequirements:  "Replay the batch against the acquirer file and report the first divergence.",
			AcceptanceCriteria:     "A deliberately corrupted batch is reported, not silently accepted.",
			CreatedAt:              "2026-05-01T08:00:00.000Z",
			Priority:               5,
		})
		if err != nil {
			t.Fatalf("creating fixture task %d: %v", i, err)
		}
		if id != i {
			t.Fatalf("fixture task got id %d, want %d: the seeds address tasks by id", id, i)
		}
	}

	// Downgrade the ordering index to its 1.12.0 shape. Until this runs, the
	// colliding seeds below cannot be written at all -- which is itself the
	// point of the constraint.
	if _, err := database.Exec("DROP INDEX IF EXISTS idx_sprint_tasks_order"); err != nil {
		t.Fatalf("dropping the unique ordering index: %v", err)
	}
	if _, err := database.Exec(sprintTasksOrderDDL1120); err != nil {
		t.Fatalf("creating the 1.12.0 ordering index: %v", err)
	}

	for _, seed := range seeds {
		if _, err := database.Exec(
			"INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) VALUES (?, ?, ?, ?)",
			seed.sprintID, seed.taskID, seed.addedAt, seed.position,
		); err != nil {
			t.Fatalf("seeding sprint_tasks row (sprint %d, task %d, position %d): %v",
				seed.sprintID, seed.taskID, seed.position, err)
		}
	}

	if _, err := database.Exec(
		"UPDATE _metadata SET value = '1.12.0' WHERE key = 'schema_version'",
	); err != nil {
		t.Fatalf("resetting schema_version to 1.12.0: %v", err)
	}
}

// readSprintPositions returns every sprint_tasks row as sprint -> task -> position.
func readSprintPositions(t *testing.T, database *DB) map[int]map[int]int {
	t.Helper()

	rows, err := database.Query("SELECT sprint_id, task_id, position FROM sprint_tasks")
	if err != nil {
		t.Fatalf("reading sprint_tasks: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	out := make(map[int]map[int]int)
	for rows.Next() {
		var sprintID, taskID, position int
		if err := rows.Scan(&sprintID, &taskID, &position); err != nil {
			t.Fatalf("scanning sprint_tasks row: %v", err)
		}
		if out[sprintID] == nil {
			out[sprintID] = make(map[int]int)
		}
		out[sprintID][taskID] = position
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating sprint_tasks: %v", err)
	}
	return out
}

// orderedTaskIDs returns one sprint's task ids in stored position order.
func orderedTaskIDs(positions map[int]int) []int {
	ids := make([]int, 0, len(positions))
	for id := range positions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if positions[ids[i]] != positions[ids[j]] {
			return positions[ids[i]] < positions[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}

// countCollidingGroups is the check SPEC/DATABASE.md § Introducing a Uniqueness
// Constraint over Existing Rows states verbatim: the number of (sprint_id,
// position) groups holding more than one row.
func countCollidingGroups(t *testing.T, database *DB) int {
	t.Helper()

	var groups int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT sprint_id, position
			FROM sprint_tasks
			GROUP BY sprint_id, position
			HAVING COUNT(*) > 1
		)`).Scan(&groups); err != nil {
		t.Fatalf("counting colliding position groups: %v", err)
	}
	return groups
}

// indexesOverSprintPosition reports every index on sprint_tasks whose columns
// are exactly (sprint_id, position), together with whether it is unique. It
// reads PRAGMA index_list and PRAGMA index_info rather than the SQL text, so a
// UNIQUE spelled any other way is still detected.
func indexesOverSprintPosition(t *testing.T, database *DB) map[string]bool {
	t.Helper()

	rows, err := database.Query("SELECT name, \"unique\" FROM pragma_index_list('sprint_tasks')")
	if err != nil {
		t.Fatalf("reading pragma_index_list('sprint_tasks'): %v", err)
	}
	type indexRow struct {
		name   string
		unique int
	}
	var all []indexRow
	for rows.Next() {
		var r indexRow
		if err := rows.Scan(&r.name, &r.unique); err != nil {
			rows.Close() //nolint:errcheck // test cleanup
			t.Fatalf("scanning pragma_index_list row: %v", err)
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close() //nolint:errcheck // test cleanup
		t.Fatalf("iterating pragma_index_list: %v", err)
	}
	rows.Close() //nolint:errcheck // test cleanup

	out := make(map[string]bool)
	for _, r := range all {
		cols, err := database.Query(
			"SELECT name FROM pragma_index_info(?) ORDER BY seqno", r.name,
		)
		if err != nil {
			t.Fatalf("reading pragma_index_info(%q): %v", r.name, err)
		}
		var columns []string
		for cols.Next() {
			var name string
			if err := cols.Scan(&name); err != nil {
				cols.Close() //nolint:errcheck // test cleanup
				t.Fatalf("scanning pragma_index_info row: %v", err)
			}
			columns = append(columns, name)
		}
		cols.Close() //nolint:errcheck // test cleanup

		if len(columns) == 2 && columns[0] == "sprint_id" && columns[1] == "position" {
			out[r.name] = r.unique == 1
		}
	}
	return out
}

// assertDenseAndDistinct checks that every sprint's positions are a dense
// 0..N-1 run, which is acceptance criteria 2 and 3 of SPEC/VERSION.md
// § Migration 1.12.0 → 1.13.0 read row by row rather than in aggregate.
func assertDenseAndDistinct(t *testing.T, stored map[int]map[int]int) {
	t.Helper()

	for sprintID, positions := range stored {
		seen := make(map[int]bool, len(positions))
		for taskID, position := range positions {
			if seen[position] {
				t.Errorf("sprint %d: position %d is held by more than one task (task %d is one of them); "+
					"SPEC/DATABASE.md § Position Uniqueness Within a Sprint requires the order to be total",
					sprintID, position, taskID)
			}
			seen[position] = true
		}
		for want := 0; want < len(positions); want++ {
			if !seen[want] {
				t.Errorf("sprint %d holds %d tasks but no task at position %d; SPEC/VERSION.md "+
					"§ Migration 1.12.0 → 1.13.0 acceptance criterion 3 requires a dense 0..N-1 run",
					sprintID, len(positions), want)
			}
		}
	}
}

// TestMigrateV1_12_0_toV1_13_0_OnNextOpen is the primary gate: a database
// created at 1.12.0 whose positions BOTH collide and leave gaps must reach
// 1.13.0 on the next open, with no user action, keeping every membership row and
// coming out dense, distinct, and ordered the way the specification says.
//
// The fixture is deliberately the hard case, because the easy one proves less:
//
//   - sprint 1 holds two colliding pairs (tasks 10 and 11 both at 0, tasks 12
//     and 13 both at 1). This is the exact input on which a correlated-subquery
//     repair was measured to leave tasks 12 and 13 BOTH at 2 -- trading one
//     collision for another -- so it is what separates the sound repair from the
//     plausible one.
//   - sprint 2 is already distinct but NOT dense (0, 3, 7). It must keep its
//     relative order and lose its gaps.
//   - sprint 3 is already dense and distinct, and must come out untouched.
//   - sprint 4 holds a single task at a non-zero position, the smallest
//     non-dense sprint there is.
func TestMigrateV1_12_0_toV1_13_0_OnNextOpen(t *testing.T) {
	const roadmap = "settlement-positions"

	seeds := []positionSeed{
		// Sprint 1: two colliding pairs.
		{sprintID: 1, taskID: 10, position: 0, addedAt: "2026-05-01T09:00:00.000Z"},
		{sprintID: 1, taskID: 11, position: 0, addedAt: "2026-05-01T09:01:00.000Z"},
		{sprintID: 1, taskID: 12, position: 1, addedAt: "2026-05-01T09:02:00.000Z"},
		{sprintID: 1, taskID: 13, position: 1, addedAt: "2026-05-01T09:03:00.000Z"},
		// Sprint 2: distinct but gappy.
		{sprintID: 2, taskID: 20, position: 0, addedAt: "2026-05-02T09:00:00.000Z"},
		{sprintID: 2, taskID: 21, position: 3, addedAt: "2026-05-02T09:01:00.000Z"},
		{sprintID: 2, taskID: 22, position: 7, addedAt: "2026-05-02T09:02:00.000Z"},
		// Sprint 3: already conforming.
		{sprintID: 3, taskID: 30, position: 0, addedAt: "2026-05-03T09:00:00.000Z"},
		{sprintID: 3, taskID: 31, position: 1, addedAt: "2026-05-03T09:01:00.000Z"},
		// Sprint 4: one task, parked away from 0.
		{sprintID: 4, taskID: 40, position: 5, addedAt: "2026-05-04T09:00:00.000Z"},
	}
	sprints := []string{
		"Settlement reconciliation",
		"Acquirer failover",
		"Ledger migration",
		"Postmortem follow-up",
	}

	buildRoadmapAtSchema1120(t, roadmap, sprints, seeds)

	// Reopen through the production path: migrations run when the database is
	// opened, so this is the whole trigger.
	database, err := OpenExisting(roadmap)
	if err != nil {
		t.Fatalf("reopening the 1.12.0 roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	// Acceptance criterion 10: the recorded version advances.
	version, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("reading schema version after open: %v", err)
	}
	if version != "1.13.0" {
		t.Fatalf("schema_version after open = %q, want 1.13.0 (SPEC/VERSION.md § Current Schema Version)", version)
	}
	if version != SchemaVersion {
		t.Errorf("schema_version after open = %q but the SchemaVersion constant is %q; a migrated "+
			"database must land on the version a fresh one is created at", version, SchemaVersion)
	}

	stored := readSprintPositions(t, database)

	// Acceptance criterion 1: no row is lost and no pair changes.
	var total int
	if err := database.QueryRow("SELECT COUNT(*) FROM sprint_tasks").Scan(&total); err != nil {
		t.Fatalf("counting sprint_tasks: %v", err)
	}
	if total != len(seeds) {
		t.Errorf("sprint_tasks holds %d rows after the migration, want %d: SPEC/VERSION.md "+
			"§ Migration 1.12.0 → 1.13.0 forbids discarding a row to make the index succeed",
			total, len(seeds))
	}
	for _, seed := range seeds {
		if _, ok := stored[seed.sprintID][seed.taskID]; !ok {
			t.Errorf("membership (sprint %d, task %d) is gone after the migration; a duplicate position "+
				"is an ambiguous order, not a redundant membership", seed.sprintID, seed.taskID)
		}
	}

	// Acceptance criteria 2 and 3.
	if groups := countCollidingGroups(t, database); groups != 0 {
		t.Errorf("colliding (sprint_id, position) groups after the migration = %d, want 0", groups)
	}
	assertDenseAndDistinct(t, stored)

	// Acceptance criterion 5: the lower task id takes the lower position of a
	// colliding pair. Both pairs of sprint 1 are checked, and the pairs must not
	// be interleaved either -- 10 and 11 shared position 0, so they come before
	// 12 and 13, which shared position 1.
	if got, want := orderedTaskIDs(stored[1]), []int{10, 11, 12, 13}; !equalInts(got, want) {
		t.Errorf("sprint 1 order after the migration = %v, want %v: SPEC/VERSION.md "+
			"§ Migration 1.12.0 → 1.13.0 acceptance criterion 5 puts the lower task id first "+
			"within a colliding pair, and the repair ranks by position before task_id, so a "+
			"pair that shared position 0 must stay ahead of one that shared position 1", got, want)
	}

	// Acceptance criterion 4: a sprint whose positions were already distinct
	// keeps its relative order. Sprint 2 was 20 @ 0, 21 @ 3, 22 @ 7.
	if got, want := orderedTaskIDs(stored[2]), []int{20, 21, 22}; !equalInts(got, want) {
		t.Errorf("sprint 2 order after the migration = %v, want %v: a sprint whose positions were "+
			"already distinct must keep its relative order", got, want)
	}
	if got := stored[2]; got[20] != 0 || got[21] != 1 || got[22] != 2 {
		t.Errorf("sprint 2 positions after the migration = %v, want 20@0 21@1 22@2: the gaps must close", got)
	}

	// Sprint 4's single task is pulled down to 0.
	if got := stored[4][40]; got != 0 {
		t.Errorf("sprint 4's only task holds position %d after the migration, want 0", got)
	}

	// Acceptance criterion 6: one index over the pair, and it is unique.
	assertSingleUniqueOrderIndex(t, database)

	// Acceptance criterion 7: the constraint is in force on the migrated database.
	assertCollidingWritesRejected(t, database, 1, 10, 11)
}

// assertSingleUniqueOrderIndex is acceptance criterion 6: PRAGMA index_list
// reports idx_sprint_tasks_order with unique = 1, and reports no second index
// over (sprint_id, position). One index serves both the ordering reads and the
// constraint (SPEC/DATABASE.md § Index Design Rationale).
func assertSingleUniqueOrderIndex(t *testing.T, database *DB) {
	t.Helper()

	found := indexesOverSprintPosition(t, database)
	if len(found) != 1 {
		t.Errorf("indexes over (sprint_id, position) = %v, want exactly one named idx_sprint_tasks_order: "+
			"a second index over the same pair would be an exact duplicate", found)
	}
	unique, ok := found["idx_sprint_tasks_order"]
	if !ok {
		t.Fatalf("idx_sprint_tasks_order does not cover (sprint_id, position); found %v", found)
	}
	if !unique {
		t.Errorf("idx_sprint_tasks_order is NOT unique; SPEC/DATABASE.md § Position Uniqueness Within " +
			"a Sprint makes this index the enforcement point of the invariant")
	}
}

// assertCollidingWritesRejected is acceptance criterion 7, read in all three of
// its directions: an INSERT that collides fails, an UPDATE that collides fails,
// and the same position in a DIFFERENT sprint is accepted, because the
// constraint is over the pair and not over position alone.
func assertCollidingWritesRejected(t *testing.T, database *DB, sprintID, taskA, taskB int) {
	t.Helper()

	var posA int
	if err := database.QueryRow(
		"SELECT position FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?", sprintID, taskA,
	).Scan(&posA); err != nil {
		t.Fatalf("reading position of task %d: %v", taskA, err)
	}

	// An UPDATE that moves task B onto task A's position.
	_, err := database.Exec(
		"UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
		posA, sprintID, taskB,
	)
	if err == nil {
		t.Errorf("UPDATE moving task %d onto position %d (held by task %d of the same sprint) SUCCEEDED; "+
			"the unique index must reject it", taskB, posA, taskA)
	} else if !IsUniqueConstraintErr(err) {
		t.Errorf("UPDATE onto a taken position failed with %v, want a UNIQUE constraint error", err)
	}

	// An INSERT of a brand-new membership at a taken position. The task id is
	// one no fixture uses, and the row is rolled back either way.
	tx, err := database.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // the probe row must never commit

	var freeTaskID int
	if err := tx.QueryRow("SELECT COALESCE(MAX(id), 0) + 1 FROM tasks").Scan(&freeTaskID); err != nil {
		t.Fatalf("computing a free task id: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO tasks
		(title, status, type, functional_requirements, technical_requirements, acceptance_criteria, created_at, priority, severity)
		VALUES (?, 'BACKLOG', 'TASK', ?, ?, ?, ?, 5, 5)`,
		"Probe the position constraint",
		"A colliding position must be refused by the schema.",
		"Insert a membership row at a position the sprint already uses.",
		"The insert fails with a UNIQUE constraint error.",
		"2026-05-05T09:00:00.000Z",
	); err != nil {
		t.Fatalf("creating the probe task: %v", err)
	}

	_, err = tx.Exec(
		"INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) VALUES (?, ?, ?, ?)",
		sprintID, freeTaskID, "2026-05-05T09:00:00.000Z", posA,
	)
	if err == nil {
		t.Errorf("INSERT of a membership at position %d (already held by task %d of sprint %d) SUCCEEDED; "+
			"the unique index must reject it", posA, taskA, sprintID)
	} else if !IsUniqueConstraintErr(err) {
		t.Errorf("INSERT at a taken position failed with %v, want a UNIQUE constraint error", err)
	}

	// The same position in a DIFFERENT sprint is fine: position on its own is
	// not unique, only the pair is.
	var otherSprint int
	if err := tx.QueryRow(
		"SELECT id FROM sprints WHERE id <> ? ORDER BY id LIMIT 1", sprintID,
	).Scan(&otherSprint); err != nil {
		t.Fatalf("finding a second sprint: %v", err)
	}
	var otherTaken int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ? AND position = ?", otherSprint, posA,
	).Scan(&otherTaken); err != nil {
		t.Fatalf("checking position %d of sprint %d: %v", posA, otherSprint, err)
	}
	if otherTaken > 0 {
		// The position is already used over there, which is itself the proof
		// that two sprints may share one position value.
		return
	}
	if _, err := tx.Exec(
		"INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) VALUES (?, ?, ?, ?)",
		otherSprint, freeTaskID, "2026-05-05T09:00:00.000Z", posA,
	); err != nil {
		t.Errorf("INSERT of position %d into sprint %d was REJECTED (%v); the constraint is over the pair "+
			"(sprint_id, position), so two different sprints may both use position %d",
			posA, otherSprint, err, posA)
	}
}

// TestMigrateV1_12_0_toV1_13_0_LeavesConformingDataUntouched is acceptance
// criterion 9, and the half of the migration that is easiest to get wrong in the
// other direction: a database whose rows already satisfy the constraint and are
// already dense must come out of the repair holding exactly the values it went
// in with.
//
// It is checked value by value rather than by a row count, because a repair that
// renumbered by added_at instead of position would also produce a valid dense run
// -- and would silently replace the planned order with the insertion order. The
// fixture's added_at order is deliberately the REVERSE of its position order, so
// such a repair fails here and nowhere else.
func TestMigrateV1_12_0_toV1_13_0_LeavesConformingDataUntouched(t *testing.T) {
	const roadmap = "conforming-positions"

	seeds := []positionSeed{
		{sprintID: 1, taskID: 1, position: 0, addedAt: "2026-06-01T12:00:00.000Z"},
		{sprintID: 1, taskID: 2, position: 1, addedAt: "2026-06-01T11:00:00.000Z"},
		{sprintID: 1, taskID: 3, position: 2, addedAt: "2026-06-01T10:00:00.000Z"},
		{sprintID: 1, taskID: 4, position: 3, addedAt: "2026-06-01T09:00:00.000Z"},
	}

	buildRoadmapAtSchema1120(t, roadmap, []string{"Chargeback automation"}, seeds)

	database, err := OpenExisting(roadmap)
	if err != nil {
		t.Fatalf("reopening the 1.12.0 roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	stored := readSprintPositions(t, database)
	for _, seed := range seeds {
		got, ok := stored[seed.sprintID][seed.taskID]
		if !ok {
			t.Fatalf("membership (sprint %d, task %d) is gone after the migration", seed.sprintID, seed.taskID)
		}
		if got != seed.position {
			t.Errorf("task %d holds position %d after the migration, want %d (unchanged): the repair is a "+
				"no-op on a database that already satisfies the constraint and is already dense. "+
				"A repair ranking by added_at instead of position would produce %d here",
				seed.taskID, got, seed.position, len(seeds)-1-seed.position)
		}
	}

	assertSingleUniqueOrderIndex(t, database)
}

// TestMigrateV1_12_0_toV1_13_0_IsIdempotent is acceptance criterion 8: running
// the migration set twice against the same database produces the same result as
// running it once, and raises no error.
//
// Both steps are re-run directly against a database that is already at 1.13.0,
// which is the state a re-application would meet: the repair must assign every
// row the value it already holds, and DROP INDEX IF EXISTS followed by
// CREATE UNIQUE INDEX IF NOT EXISTS must leave one unique index behind.
func TestMigrateV1_12_0_toV1_13_0_IsIdempotent(t *testing.T) {
	const roadmap = "idempotent-positions"

	seeds := []positionSeed{
		{sprintID: 1, taskID: 1, position: 0, addedAt: "2026-07-01T09:00:00.000Z"},
		{sprintID: 1, taskID: 2, position: 0, addedAt: "2026-07-01T09:01:00.000Z"},
		{sprintID: 1, taskID: 3, position: 4, addedAt: "2026-07-01T09:02:00.000Z"},
		{sprintID: 2, taskID: 4, position: 2, addedAt: "2026-07-01T09:03:00.000Z"},
	}

	buildRoadmapAtSchema1120(t, roadmap, []string{"Fraud scoring", "Dispute intake"}, seeds)

	database, err := OpenExisting(roadmap)
	if err != nil {
		t.Fatalf("reopening the 1.12.0 roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	first := readSprintPositions(t, database)

	for pass := 2; pass <= 3; pass++ {
		tx, err := database.Begin()
		if err != nil {
			t.Fatalf("begin (pass %d): %v", pass, err)
		}
		if err := migrateV1_12_0_toV1_13_0(tx); err != nil {
			tx.Rollback() //nolint:errcheck // the pass already failed
			t.Fatalf("migration pass %d returned error (not idempotent): %v", pass, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit (pass %d): %v", pass, err)
		}

		again := readSprintPositions(t, database)
		if !equalPositions(first, again) {
			t.Fatalf("pass %d changed the stored positions: %v -> %v; running the migration twice must "+
				"produce the same result as running it once", pass, first, again)
		}
		assertSingleUniqueOrderIndex(t, database)
	}
}

// TestFreshDatabaseHasUniquePositionIndex is acceptance criterion 11: a database
// created at 1.13.0 receives the unique index directly from the sprint_tasks
// schema definition and requires no repair. It is what stops the fresh DDL and
// the migration drifting apart, since the constraint is declared in both.
func TestFreshDatabaseHasUniquePositionIndex(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	assertSingleUniqueOrderIndex(t, database)

	// And the constraint bites on a fresh database too, without any migration
	// having run.
	sprintID, _ := seedSprintWithTasks(t, database, "Fresh schema check", 2)
	assertCollidingWritesRejectedFresh(t, database, sprintID)
}

// assertCollidingWritesRejectedFresh writes a colliding position into the one
// sprint a fresh database holds. It is separate from
// assertCollidingWritesRejected because a single-sprint database cannot exercise
// the "same position, different sprint" leg.
//
// The sprint id is passed in rather than assumed to be 1: an UPDATE that matched
// no row would "succeed" and be reported as a missing constraint, which is a
// diagnosis this test must never produce for the wrong reason.
func assertCollidingWritesRejectedFresh(t *testing.T, database *DB, sprintID int) {
	t.Helper()

	result, err := database.Exec(
		"UPDATE sprint_tasks SET position = 0 WHERE sprint_id = ? AND position = 1",
		sprintID,
	)
	if err == nil {
		affected, rowsErr := result.RowsAffected()
		if rowsErr == nil && affected == 0 {
			t.Fatalf("the probe UPDATE matched no row in sprint %d, so it proves nothing: the fixture "+
				"must hold a task at position 1", sprintID)
		}
		t.Error("UPDATE moving a task onto position 0, already held by another task of the same sprint, " +
			"SUCCEEDED on a FRESH database; the unique index in CreateSchema must reject it")
		return
	}
	if !IsUniqueConstraintErr(err) {
		t.Errorf("UPDATE onto a taken position failed with %v, want a UNIQUE constraint error", err)
	}
}

// ==================== THE ORDERING COMMANDS UNDER THE CONSTRAINT ====================

// seedSprintWithTasks creates one sprint with n tasks added through the shipped
// write path, and returns the sprint id and the task ids in the order they were
// added (which is position order, since AddTasksToSprint appends).
func seedSprintWithTasks(t *testing.T, database *DB, title string, n int) (int, []int) {
	t.Helper()

	ctx := testContext()
	sprintID, err := database.CreateSprint(ctx, &models.Sprint{
		Title:       title,
		Description: "Exercise the ordering commands against the unique position index.",
		Status:      models.SprintPending,
		CreatedAt:   "2026-08-01T08:00:00.000Z",
	})
	if err != nil {
		t.Fatalf("creating sprint %q: %v", title, err)
	}

	taskIDs := make([]int, 0, n)
	for i := 1; i <= n; i++ {
		id, err := database.CreateTask(ctx, &models.Task{
			Title:                  fmt.Sprintf("%s step %d", title, i),
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			FunctionalRequirements: "The sprint's planned order must survive every ordering command.",
			TechnicalRequirements:  "Permute positions without ever presenting a transient duplicate.",
			AcceptanceCriteria:     "The stored order matches the requested order exactly.",
			CreatedAt:              "2026-08-01T08:00:00.000Z",
			Priority:               5,
		})
		if err != nil {
			t.Fatalf("creating task %d for %q: %v", i, title, err)
		}
		taskIDs = append(taskIDs, id)
	}

	if err := database.AddTasksToSprint(ctx, sprintID, taskIDs); err != nil {
		t.Fatalf("adding tasks to sprint %d: %v", sprintID, err)
	}
	return sprintID, taskIDs
}

// storedOrder returns one sprint's task ids in stored position order.
func storedOrder(t *testing.T, database *DB, sprintID int) []int {
	t.Helper()

	stored := readSprintPositions(t, database)
	return orderedTaskIDs(stored[sprintID])
}

// TestReorderSprintTasksFullReversal is acceptance criterion 12 for `sprint
// reorder`, on the input that makes the parking step unavoidable.
//
// A full reversal is the worst case for a sequential assignment: the first write
// claims position 0, which the sprint's first task still holds, so a reorder
// without parking fails on statement one with
// "UNIQUE constraint failed: sprint_tasks.sprint_id, sprint_tasks.position".
func TestReorderSprintTasksFullReversal(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID, taskIDs := seedSprintWithTasks(t, database, "Reversal", 6)

	reversed := make([]int, len(taskIDs))
	for i, id := range taskIDs {
		reversed[len(taskIDs)-1-i] = id
	}

	if err := database.ReorderSprintTasks(sprintID, reversed); err != nil {
		t.Fatalf("reordering to the full reversal: %v", err)
	}
	if got := storedOrder(t, database, sprintID); !equalInts(got, reversed) {
		t.Errorf("order after full reversal = %v, want %v", got, reversed)
	}
	assertNoCollisions(t, database)

	// And back again, which reverses a reversal -- the same worst case from the
	// other side.
	if err := database.ReorderSprintTasks(sprintID, taskIDs); err != nil {
		t.Fatalf("reordering back to the original order: %v", err)
	}
	if got := storedOrder(t, database, sprintID); !equalInts(got, taskIDs) {
		t.Errorf("order after reversing back = %v, want %v", got, taskIDs)
	}
	assertNoCollisions(t, database)
}

// TestReorderSprintTasksRejectsIncompleteListInsideTransaction closes the TOCTOU
// that SPEC/DATABASE.md § Reorder Sprint Tasks (Set Exact Order) names: the CLI
// reads the sprint's membership to check completeness, and another process can
// add a task before the reorder is applied. The list is then complete when it is
// read and partial when it is applied, and a partial assignment leaves the
// omitted tasks holding positions the reorder also assigns.
//
// The race is reproduced by its outcome rather than by timing: the db-layer call
// is handed exactly the list the CLI would have carried into the write -- one
// that was complete a moment ago -- and it must refuse it. Before the checks
// moved inside the transaction, the DB layer verified membership only, so this
// list passed and the sprint came out with duplicate positions.
func TestReorderSprintTasksRejectsIncompleteListInsideTransaction(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID, taskIDs := seedSprintWithTasks(t, database, "Concurrent addition", 3)

	// The CLI read the membership here: three tasks.
	stale := []int{taskIDs[2], taskIDs[0], taskIDs[1]}

	// Another process adds a fourth task before the reorder is applied.
	ctx := testContext()
	latecomer, err := database.CreateTask(ctx, &models.Task{
		Title:                  "Added while the reorder was in flight",
		Type:                   models.TypeTask,
		Status:                 models.StatusBacklog,
		FunctionalRequirements: "A task added between the check and the write must not be silently overwritten.",
		TechnicalRequirements:  "The completeness check runs inside the write transaction.",
		AcceptanceCriteria:     "The reorder is refused and the sprint keeps its order.",
		CreatedAt:              "2026-08-01T08:05:00.000Z",
		Priority:               5,
	})
	if err != nil {
		t.Fatalf("creating the latecomer task: %v", err)
	}
	if err := database.AddTasksToSprint(ctx, sprintID, []int{latecomer}); err != nil {
		t.Fatalf("adding the latecomer to the sprint: %v", err)
	}

	before := storedOrder(t, database, sprintID)

	err = database.ReorderSprintTasks(sprintID, stale)
	if err == nil {
		t.Fatal("ReorderSprintTasks accepted a list that names only 3 of the sprint's 4 members; " +
			"the completeness check must run INSIDE the write transaction, or a concurrent addition " +
			"turns a valid request into one that leaves duplicate positions behind")
	}
	if !utils.IsValidation(err) {
		t.Errorf("refusal error = %v; it must wrap utils.ErrValidation so the CLI maps it to exit 6, "+
			"the code SPEC/COMMANDS.md § Reorder Tasks gives an incomplete task list", err)
	}

	// The transaction rolled back, so the sprint kept the order it had.
	if got := storedOrder(t, database, sprintID); !equalInts(got, before) {
		t.Errorf("order after the refused reorder = %v, want %v (unchanged): a refused reorder must "+
			"write nothing", got, before)
	}
	assertNoCollisions(t, database)
}

// TestReorderSprintTasksRejectsDuplicateInsideTransaction proves the third of
// the in-transaction checks, and it asserts on the DIAGNOSIS rather than only on
// the refusal, because refusal alone would not distinguish the check.
//
// A duplicated id makes the list the right LENGTH while naming one member twice
// and another not at all, so the completeness check passes it. The membership
// count then refuses it anyway -- d duplicates shrink the distinct id set to
// len-d, which can never equal len -- so removing the duplicate check would
// still reject this list, while blaming membership for a list whose every id IS
// a member. The message is therefore what the assertion pins: the caller must be
// told which id was repeated.
func TestReorderSprintTasksRejectsDuplicateInsideTransaction(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID, taskIDs := seedSprintWithTasks(t, database, "Duplicate id", 3)
	before := storedOrder(t, database, sprintID)

	err := database.ReorderSprintTasks(sprintID, []int{taskIDs[0], taskIDs[1], taskIDs[1]})
	if err == nil {
		t.Fatal("ReorderSprintTasks accepted a list naming one task twice; the duplicate check must run " +
			"inside the write transaction with the other two")
	}
	if !utils.IsValidation(err) {
		t.Errorf("refusal error = %v; it must wrap utils.ErrValidation (exit 6)", err)
	}
	want := fmt.Sprintf("duplicate task ID %d", taskIDs[1])
	if !strings.Contains(err.Error(), want) {
		t.Errorf("refusal error = %q, want it to contain %q: every id in the list IS a member of the "+
			"sprint, so blaming membership would misdiagnose a repeated id (SPEC/COMMANDS.md "+
			"§ Reorder Tasks (Set Exact Order), \"Duplicate task IDs\")", err.Error(), want)
	}
	if got := storedOrder(t, database, sprintID); !equalInts(got, before) {
		t.Errorf("order after the refused reorder = %v, want %v (unchanged)", got, before)
	}
}

// TestMoveTaskToPositionBothDirections is acceptance criterion 12 for `sprint
// move-to`, `sprint top` and `sprint bottom`.
//
// The shift form this replaced collides in BOTH directions: moving up walks a
// run of rows upwards onto values their neighbours still hold, and moving down
// does the same downwards. Both directions are therefore exercised, and so is
// each end of the sprint.
func TestMoveTaskToPositionBothDirections(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID, ids := seedSprintWithTasks(t, database, "Move to position", 5)

	cases := []struct {
		name   string
		taskID int
		target int
		want   []int
	}{
		{"move up to the top (sprint top)", ids[4], 0, []int{ids[4], ids[0], ids[1], ids[2], ids[3]}},
		{"move down to the bottom (sprint bottom)", ids[4], 4, []int{ids[0], ids[1], ids[2], ids[3], ids[4]}},
		{"move up into the middle", ids[3], 1, []int{ids[0], ids[3], ids[1], ids[2], ids[4]}},
		{"move down into the middle", ids[0], 3, []int{ids[3], ids[1], ids[2], ids[0], ids[4]}},
		{"target beyond the end is clamped to the end", ids[3], 99, []int{ids[1], ids[2], ids[0], ids[4], ids[3]}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := database.MoveTaskToPosition(sprintID, tc.taskID, tc.target); err != nil {
				t.Fatalf("moving task %d to position %d: %v", tc.taskID, tc.target, err)
			}
			if got := storedOrder(t, database, sprintID); !equalInts(got, tc.want) {
				t.Errorf("order after the move = %v, want %v", got, tc.want)
			}
			assertNoCollisions(t, database)
		})
	}
}

// TestSwapTasksAdjacentAndDistant is acceptance criterion 12 for `sprint swap`.
//
// Adjacent tasks are the case that makes parking necessary: writing the two
// positions directly fails on the FIRST statement, because the position it
// assigns is the one the other task still holds. Distant tasks fail the same way
// for the same reason, and a swap of the two ends covers the whole range.
func TestSwapTasksAdjacentAndDistant(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID, ids := seedSprintWithTasks(t, database, "Swap", 4)

	if err := database.SwapTasks(sprintID, ids[1], ids[2]); err != nil {
		t.Fatalf("swapping two adjacent tasks: %v", err)
	}
	if got, want := storedOrder(t, database, sprintID), []int{ids[0], ids[2], ids[1], ids[3]}; !equalInts(got, want) {
		t.Errorf("order after the adjacent swap = %v, want %v", got, want)
	}
	assertNoCollisions(t, database)

	if err := database.SwapTasks(sprintID, ids[0], ids[3]); err != nil {
		t.Fatalf("swapping the two ends: %v", err)
	}
	if got, want := storedOrder(t, database, sprintID), []int{ids[3], ids[2], ids[1], ids[0]}; !equalInts(got, want) {
		t.Errorf("order after swapping the ends = %v, want %v", got, want)
	}
	assertNoCollisions(t, database)
}

// TestCompactSprintPositionsNeedsNoParking pins the property the corrected
// comment on CompactSprintPositionsTx now claims: the routine renumbers
// downwards over an ascending read, so it reaches a dense run without a parking
// step even while the unique index is in force.
//
// The fixture leaves gaps of the kind a removal produces, so the compaction has
// real work to do rather than assigning every row the value it already holds.
func TestCompactSprintPositionsNeedsNoParking(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID, ids := seedSprintWithTasks(t, database, "Compaction", 5)

	// Spread the positions out to 0, 2, 4, 6, 8 -- descending order, so no
	// intermediate write can be excused by the values happening to be free.
	for i := len(ids) - 1; i >= 0; i-- {
		if _, err := database.Exec(
			"UPDATE sprint_tasks SET position = ? WHERE sprint_id = ? AND task_id = ?",
			i*2, sprintID, ids[i],
		); err != nil {
			t.Fatalf("spreading position of task %d: %v", ids[i], err)
		}
	}

	if err := database.WithTransaction(func(tx *sql.Tx) error {
		return CompactSprintPositionsTx(tx, sprintID)
	}); err != nil {
		t.Fatalf("compacting positions: %v", err)
	}

	if got := storedOrder(t, database, sprintID); !equalInts(got, ids) {
		t.Errorf("order after compaction = %v, want %v", got, ids)
	}
	stored := readSprintPositions(t, database)
	for i, id := range ids {
		if got := stored[sprintID][id]; got != i {
			t.Errorf("task %d holds position %d after compaction, want %d (a dense 0..N-1 run)", id, got, i)
		}
	}
	assertNoCollisions(t, database)
}

// assertNoCollisions is the invariant every ordering test re-checks after its
// own assertions: whatever the command did, no two members of one sprint may
// share a position.
func assertNoCollisions(t *testing.T, database *DB) {
	t.Helper()

	if groups := countCollidingGroups(t, database); groups != 0 {
		t.Errorf("colliding (sprint_id, position) groups = %d, want 0", groups)
	}
}

// equalInts reports whether two id sequences are identical, order included.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalPositions reports whether two sprint -> task -> position maps hold the
// same values.
func equalPositions(a, b map[int]map[int]int) bool {
	if len(a) != len(b) {
		return false
	}
	for sprintID, tasks := range a {
		other, ok := b[sprintID]
		if !ok || len(other) != len(tasks) {
			return false
		}
		for taskID, position := range tasks {
			if other[taskID] != position {
				return false
			}
		}
	}
	return true
}

// ==================== THE ONE INSERT PATH ====================

// sprintTasksInsertPattern matches an INSERT into sprint_tasks, however it is
// spaced or cased.
var sprintTasksInsertPattern = regexp.MustCompile(`(?i)INSERT\s+INTO\s+sprint_tasks`)

// TestSprintTasksHasExactlyOneProductionInsertPath is the other half of rmp task
// #236, and it guards a DOCUMENTATION invariant rather than a runtime one.
//
// SPEC/DATABASE.md § Add Task to Sprint with Position states, of one statement:
// "This is the only statement in the application that inserts a sprint_tasks
// row... Any other insert would be a new write path and would have to be
// specified here before it is written." That claim used to be false in the other
// direction -- the file documented an "Associate to Sprint" insert that no code
// had, whose column list omitted position and would therefore have left the
// DEFAULT 0 -- and nothing in the repository would have noticed either drift.
//
// This test is what notices. A second insert path added without a specification
// fails here, and so does one that omits position from its column list, which is
// precisely the shape the unique index now rejects at runtime.
//
// It walks the AST and inspects STRING LITERALS ONLY, never raw lines. A
// line-based scan would also match a comment that merely quotes the statement --
// and this file, the migration beside it, and the specification excerpts in both
// are full of such prose -- so it would fail on documentation and pass on a real
// second insert built by concatenation in an adjacent string.
func TestSprintTasksHasExactlyOneProductionInsertPath(t *testing.T) {
	type hit struct {
		file string
		line int
		text string
	}
	var hits []hit

	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0) // comments deliberately not parsed
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if sprintTasksInsertPattern.MatchString(value) {
				hits = append(hits, hit{
					file: path,
					line: fset.Position(lit.Pos()).Line,
					text: strings.Join(strings.Fields(value), " "),
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the source tree: %v", err)
	}

	if len(hits) != 1 {
		for _, h := range hits {
			t.Logf("  %s:%d  %s", h.file, h.line, h.text)
		}
		t.Fatalf("found %d production statements that insert into sprint_tasks, want exactly 1: "+
			"SPEC/DATABASE.md § Add Task to Sprint with Position states there is only one, and any "+
			"other insert must be specified there before it is written", len(hits))
	}

	// The one statement must name position. A column list that omits it takes
	// DEFAULT 0, which collides with whichever task already holds position 0 --
	// the exact defect the documented-but-absent "Associate to Sprint" statement
	// would have had.
	if !strings.Contains(hits[0].text, "position") {
		t.Errorf("the one sprint_tasks INSERT (%s:%d) does not name position in its column list: %q. "+
			"The column carries DEFAULT 0, so omitting it places the task at position 0 and collides "+
			"with the sprint's first task", hits[0].file, hits[0].line, hits[0].text)
	}
}
