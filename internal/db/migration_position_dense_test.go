package db

import (
	"fmt"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ==================== MIGRATION 1.13.0 -> 1.14.0 ====================
//
// The regression gates for rmp task #304, data side. Migration 1.12.0 → 1.13.0
// made a sprint's positions DISTINCT; it did not keep them DENSE, and four write
// paths went on taking rows out of a sprint's run without repairing it. Teaching
// those paths to compact — which internal/commands/sprint_position_density_test.go
// verifies — stops new damage and repairs none of the damage already committed,
// so the runs that are sparse today are repaired by this migration
// (SPEC/VERSION.md § Migration 1.13.0 → 1.14.0, SPEC/DATABASE.md § Position
// Density Within a Sprint).
//
// Four properties carry the weight here, and each has an assertion that can
// fail:
//
//  1. The migration changes VALUES and never the SEQUENCE. A repair that
//     produced a dense run by reordering a sprint would satisfy the invariant
//     and destroy the plan, which is a worse defect than the one being fixed.
//  2. It works on the shape the defect actually produced. The fixture is the
//     measured one: 39 members holding 0..36, 53 and 57, the run the source side
//     of `sprint move-tasks` left behind in a real roadmap.
//  3. It works whatever the PHYSICAL order of the rows is. This is the property
//     that dictates the migration's shape: the repair is one multi-row UPDATE,
//     SQLite checks the unique index as each row is written, and a row moving
//     down can land on a value a row not yet rewritten still holds. The
//     reversed-layout fixture below is rejected outright if the migration
//     forgets to take the index down first.
//  4. It is idempotent and harmless on a database that is already dense.

// buildRoadmapAtSchema1130 creates a real on-disk roadmap under the test HOME
// and takes it back to schema 1.13.0, then overwrites its sprint_tasks rows with
// the given seeds.
//
// The roadmap is first created through the production path, which yields the
// current schema; the sprints and tasks are written through the shipped write
// paths; and only then are the membership rows replaced. Nothing between 1.13.0
// and 1.14.0 changed a table or an index definition — the migration rewrites
// DATA and puts the same index back — so a current database with its
// schema_version reset is a faithful 1.13.0 database.
//
// When reversePhysicalOrder is set the seeds are inserted from the last to the
// first. sprint_tasks is an ordinary rowid table, so insertion order is physical
// order, and inserting a descending-position sequence is what makes the physical
// layout disagree with the position order.
//
// It returns with the database CLOSED, at schema_version 1.13.0, ready to be
// reopened so that the production migration path runs against it.
func buildRoadmapAtSchema1130(t *testing.T, roadmapName string, sprintTitles []string, seeds []positionSeed, reversePhysicalOrder bool) {
	t.Helper()

	database, err := Open(roadmapName)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", roadmapName, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	for i, title := range sprintTitles {
		id, err := seedSprint(database, &models.Sprint{
			Title:       title,
			Description: "Clear the settlement backlog left by the acquirer outage.",
			Status:      models.SprintPending,
			CreatedAt:   fmt.Sprintf("2026-07-%02dT08:00:00.000Z", i+1),
		})
		if err != nil {
			t.Fatalf("creating fixture sprint %q: %v", title, err)
		}
		if id != i+1 {
			t.Fatalf("fixture sprint %q got id %d, want %d: the seeds address sprints by id", title, id, i+1)
		}
	}

	maxTaskID := 0
	for _, seed := range seeds {
		if seed.taskID > maxTaskID {
			maxTaskID = seed.taskID
		}
	}
	for i := 1; i <= maxTaskID; i++ {
		id, err := seedTask(database, &models.Task{
			Title:                  fmt.Sprintf("Reconcile settlement batch %d", i),
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			FunctionalRequirements: "Every settlement batch must reconcile to the cent before the ledger closes.",
			TechnicalRequirements:  "Replay the batch against the acquirer file and report the first divergence.",
			AcceptanceCriteria:     "A deliberately corrupted batch is reported, not silently accepted.",
			CreatedAt:              "2026-07-01T08:00:00.000Z",
			Priority:               5,
		})
		if err != nil {
			t.Fatalf("creating fixture task %d: %v", i, err)
		}
		if id != i {
			t.Fatalf("fixture task got id %d, want %d: the seeds address tasks by id", id, i)
		}
	}

	ordered := seeds
	if reversePhysicalOrder {
		ordered = make([]positionSeed, len(seeds))
		for i, seed := range seeds {
			ordered[len(seeds)-1-i] = seed
		}
	}
	for _, seed := range ordered {
		if _, err := database.Exec(
			"INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) VALUES (?, ?, ?, ?)",
			seed.sprintID, seed.taskID, seed.addedAt, seed.position,
		); err != nil {
			t.Fatalf("seeding sprint_tasks row (sprint %d, task %d, position %d): %v",
				seed.sprintID, seed.taskID, seed.position, err)
		}
	}

	if _, err := database.Exec(
		"UPDATE _metadata SET value = '1.13.0' WHERE key = 'schema_version'",
	); err != nil {
		t.Fatalf("resetting schema_version to 1.13.0: %v", err)
	}
}

// liveSparseRunSeeds returns the fixture that reproduces the shape MEASURED in a
// real roadmap: one sprint holding 39 member tasks at positions 0..36, 53 and
// 57. That run is what the source side of `sprint move-tasks` left behind by
// re-parenting rows away without compacting, and it is exactly the shape the
// migration exists to repair.
//
// A second sprint is seeded alongside it, already dense, so the same run proves
// that a conforming sprint is left alone.
func liveSparseRunSeeds() []positionSeed {
	seeds := make([]positionSeed, 0, 39+3)
	taskID := 1
	for position := 0; position <= 36; position++ {
		seeds = append(seeds, positionSeed{
			sprintID: 1, taskID: taskID, position: position,
			addedAt: fmt.Sprintf("2026-07-01T%02d:00:00.000Z", position%24),
		})
		taskID++
	}
	for _, position := range []int{53, 57} {
		seeds = append(seeds, positionSeed{
			sprintID: 1, taskID: taskID, position: position,
			addedAt: "2026-07-02T08:00:00.000Z",
		})
		taskID++
	}
	for position := 0; position < 3; position++ {
		seeds = append(seeds, positionSeed{
			sprintID: 2, taskID: taskID, position: position,
			addedAt: "2026-07-03T08:00:00.000Z",
		})
		taskID++
	}
	return seeds
}

// sparseFixtureSprintTitles names the two sprints liveSparseRunSeeds populates.
var sparseFixtureSprintTitles = []string{
	"Settlement reconciliation",
	"Ledger close hardening",
}

// TestMigrateV1_13_0_toV1_14_0_OnTheMeasuredSparseRun is the primary gate: a
// 1.13.0 database holding the run that was actually measured in the field must
// come out dense on the next open, with no user action, keeping every membership
// row and — the part that matters most — keeping every sprint's SEQUENCE exactly
// as it was.
func TestMigrateV1_13_0_toV1_14_0_OnTheMeasuredSparseRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmap = "settlement-density"
	seeds := liveSparseRunSeeds()
	buildRoadmapAtSchema1130(t, roadmap, sparseFixtureSprintTitles, seeds, false)

	// The order the fixture holds BEFORE the migration, read from the seeds
	// themselves rather than from the database, so the expectation cannot be
	// contaminated by whatever the migration does.
	wantOrder := map[int][]int{}
	for _, seed := range seeds {
		wantOrder[seed.sprintID] = append(wantOrder[seed.sprintID], seed.taskID)
	}
	if got := len(wantOrder[1]); got != 39 {
		t.Fatalf("the sparse fixture sprint holds %d members, want the 39 that were measured", got)
	}

	// The fixture must really be sparse, or the migration has nothing to do and
	// this gate proves nothing.
	sparseBefore := false
	for _, seed := range seeds {
		if seed.sprintID == 1 && seed.position > 38 {
			sparseBefore = true
		}
	}
	if !sparseBefore {
		t.Fatal("the fixture is already dense, so the migration cannot be observed to repair anything")
	}

	// Reopen through the production path: migrations run when the database is
	// opened, so this is the whole trigger.
	database, err := OpenExisting(roadmap)
	if err != nil {
		t.Fatalf("reopening the 1.13.0 roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	version, err := database.GetSchemaVersion()
	if err != nil {
		t.Fatalf("reading schema version after open: %v", err)
	}
	if version != SchemaVersion {
		t.Fatalf("schema_version after open = %q, want %q: a migrated database must land on the "+
			"version a fresh one is created at", version, SchemaVersion)
	}

	stored := readSprintPositions(t, database)

	// No row is lost and no membership pair changes.
	var total int
	if err := database.QueryRow("SELECT COUNT(*) FROM sprint_tasks").Scan(&total); err != nil {
		t.Fatalf("counting sprint_tasks: %v", err)
	}
	if total != len(seeds) {
		t.Errorf("sprint_tasks holds %d rows after the migration, want the %d it held before: "+
			"the migration must never discard a membership", total, len(seeds))
	}

	// The run is dense and distinct, in every sprint.
	assertDenseAndDistinct(t, stored)
	if groups := countCollidingGroups(t, database); groups != 0 {
		t.Errorf("colliding (sprint_id, position) groups after the migration = %d, want 0", groups)
	}

	// And the sequence survived. This is the assertion the whole migration turns
	// on: ranking by added_at would also have produced a dense run, while
	// silently replacing the planned order with the order the tasks were added.
	for sprintID, want := range wantOrder {
		if got := orderedTaskIDs(stored[sprintID]); !equalInts(got, want) {
			t.Errorf("sprint %d is ordered %v after the migration, want %v: a densification changes "+
				"VALUES and never the SEQUENCE", sprintID, got, want)
		}
	}

	// The index is back, unique, and there is exactly one of it.
	indexes := indexesOverSprintPosition(t, database)
	if len(indexes) != 1 {
		t.Errorf("indexes over (sprint_id, position) after the migration = %v, want exactly one", indexes)
	}
	if unique, ok := indexes["idx_sprint_tasks_order"]; !ok || !unique {
		t.Errorf("idx_sprint_tasks_order after the migration: present=%v unique=%v, want present and unique",
			ok, unique)
	}
}

// TestMigrateV1_13_0_toV1_14_0_WhateverThePhysicalRowOrder is the gate that
// pins the migration's SHAPE, and it is the reason the unique index is dropped
// for the duration of the repair rather than left in force.
//
// The repair is a single multi-row UPDATE. SQLite applies it row by row, checks
// the unique index as each row is written, and offers no deferred check; the
// order in which it visits the rows follows the physical layout, not the
// position order. When the two disagree, a row moving DOWN can land on a value a
// row not yet rewritten still holds. Measured against the pinned driver, the run
// 0, 2, 5 laid out in reverse physical order failed with
// "UNIQUE constraint failed: sprint_tasks.sprint_id, sprint_tasks.position" while
// the same three rows in ascending physical order succeeded.
//
// So this fixture is the same data as the previous test's, inserted backwards.
// If the migration stops taking the index down first, the whole open fails and
// the roadmap becomes unusable — which is the failure this gate catches, and the
// reason "it worked on the database I measured" is not evidence about anyone
// else's.
func TestMigrateV1_13_0_toV1_14_0_WhateverThePhysicalRowOrder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmap = "settlement-reversed-layout"
	seeds := []positionSeed{
		// The adversarial run: a gap EARLY, so the last member must move down
		// onto a value an earlier member still holds.
		{sprintID: 1, taskID: 1, position: 0, addedAt: "2026-07-01T08:00:00.000Z"},
		{sprintID: 1, taskID: 2, position: 2, addedAt: "2026-07-01T08:01:00.000Z"},
		{sprintID: 1, taskID: 3, position: 5, addedAt: "2026-07-01T08:02:00.000Z"},
		{sprintID: 1, taskID: 4, position: 6, addedAt: "2026-07-01T08:03:00.000Z"},
	}
	buildRoadmapAtSchema1130(t, roadmap, []string{"Settlement reconciliation"}, seeds, true)

	// The fixture must really be laid out backwards, or the hazard this gate
	// exists for is not present and it proves nothing.
	assertPhysicalOrderIsReversed(t, roadmap)

	database, err := OpenExisting(roadmap)
	if err != nil {
		t.Fatalf("reopening the 1.13.0 roadmap whose rows are laid out backwards: %v; the repair must "+
			"run with no unique index in force, or its intermediate states violate one", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	stored := readSprintPositions(t, database)
	assertDenseAndDistinct(t, stored)
	if got, want := orderedTaskIDs(stored[1]), []int{1, 2, 3, 4}; !equalInts(got, want) {
		t.Errorf("sprint 1 is ordered %v after the migration, want %v", got, want)
	}
}

// assertPhysicalOrderIsReversed fails unless the fixture's rows sit in the table
// in the opposite order to their positions. It reads the implicit rowid, which
// IS the physical order of an ordinary SQLite table, so the check is about the
// layout rather than about anything the schema declares.
func assertPhysicalOrderIsReversed(t *testing.T, roadmapName string) {
	t.Helper()

	database, err := OpenExisting(roadmapName)
	if err != nil {
		t.Fatalf("reopening %q to inspect its physical row order: %v", roadmapName, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	rows, err := database.Query("SELECT position FROM sprint_tasks ORDER BY rowid ASC")
	if err != nil {
		t.Fatalf("reading sprint_tasks in physical order: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	var positions []int
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scanning a position: %v", err)
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating sprint_tasks: %v", err)
	}
	if len(positions) < 2 {
		t.Fatalf("the fixture holds %d rows, too few to have a physical order at all", len(positions))
	}
	for i := 1; i < len(positions); i++ {
		if positions[i] >= positions[i-1] {
			t.Fatalf("the fixture's physical row order is %v, which is not the reverse of its position "+
				"order; this gate would then exercise the easy layout and prove nothing", positions)
		}
	}
}

// TestMigrateV1_13_0_toV1_14_0_IsIdempotentAndLeavesADenseDatabaseAlone covers
// the two states a migration must be safe on: one it has already been applied
// to, and one that never needed it.
//
// Re-application is exercised by running the migration function a second time
// against a database that is already at 1.14.0, which is what a partially
// applied migration set would do on the next open. The no-op case is exercised
// by seeding a database that is already dense and comparing the table before and
// after, row by row.
func TestMigrateV1_13_0_toV1_14_0_IsIdempotentAndLeavesADenseDatabaseAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	const roadmap = "settlement-already-dense"
	seeds := []positionSeed{
		{sprintID: 1, taskID: 1, position: 0, addedAt: "2026-07-01T08:00:00.000Z"},
		{sprintID: 1, taskID: 2, position: 1, addedAt: "2026-07-01T08:01:00.000Z"},
		{sprintID: 1, taskID: 3, position: 2, addedAt: "2026-07-01T08:02:00.000Z"},
		{sprintID: 2, taskID: 4, position: 0, addedAt: "2026-07-02T08:00:00.000Z"},
		{sprintID: 2, taskID: 5, position: 1, addedAt: "2026-07-02T08:01:00.000Z"},
	}
	buildRoadmapAtSchema1130(t, roadmap, sparseFixtureSprintTitles, seeds, false)

	database, err := OpenExisting(roadmap)
	if err != nil {
		t.Fatalf("reopening the already-dense 1.13.0 roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	before := readSprintPositions(t, database)
	assertDenseAndDistinct(t, before)
	for _, seed := range seeds {
		if got := before[seed.sprintID][seed.taskID]; got != seed.position {
			t.Errorf("task %d of sprint %d holds position %d after the migration, want the %d it held "+
				"before: a database that already conforms must come out untouched",
				seed.taskID, seed.sprintID, got, seed.position)
		}
	}

	// Apply the migration a second time, directly, against a database already at
	// 1.14.0. Nothing may change and nothing may error.
	if err := database.WithTransaction(migrateV1_13_0_toV1_14_0); err != nil {
		t.Fatalf("re-applying the migration to an already-migrated database: %v; it must be idempotent", err)
	}

	after := readSprintPositions(t, database)
	if !equalPositions(before, after) {
		t.Errorf("re-applying the migration changed the table: before %v, after %v", before, after)
	}

	indexes := indexesOverSprintPosition(t, database)
	if len(indexes) != 1 {
		t.Errorf("indexes over (sprint_id, position) after re-application = %v, want exactly one", indexes)
	}
	if unique, ok := indexes["idx_sprint_tasks_order"]; !ok || !unique {
		t.Errorf("idx_sprint_tasks_order after re-application: present=%v unique=%v, want present and unique",
			ok, unique)
	}
}

// TestMigrateV1_13_0_toV1_14_0_IsRegistered pins the migration into the set the
// production open path runs. A migration function that exists and is not
// registered repairs nothing, and the bug it was written for stays in the field.
func TestMigrateV1_13_0_toV1_14_0_IsRegistered(t *testing.T) {
	found := false
	for _, migration := range migrations {
		if migration.Version == "1.14.0" {
			found = true
			if migration.Apply == nil {
				t.Error("the 1.14.0 migration is registered with a nil Apply")
			}
			if migration.Name == "" {
				t.Error("the 1.14.0 migration is registered with an empty Name")
			}
		}
	}
	if !found {
		t.Errorf("no migration targets 1.14.0; the registered set is what RunMigrations walks, so an "+
			"unregistered repair never runs. SchemaVersion is %q", SchemaVersion)
	}
}
