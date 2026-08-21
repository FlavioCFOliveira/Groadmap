// Package commands — tests for the counterpart entity of a relational audit
// entry (rmp task #263).
//
// The defect these close is one an audit log can carry for a long time without
// anyone noticing, because nothing about it looks broken: every row is present,
// every column is populated, and the operation names are right. What was missing
// is the second entity.
//
// `sprint add-tasks <s> 1,2,3` wrote three SPRINT_ADD_TASK rows that were
// identical in every column but id, so the sprint's history recorded that three
// tasks had been added without recording which three; and the tasks' own
// histories were silent, even though each task's status had just changed from
// BACKLOG to SPRINT. `sprint move-tasks` was worse still: one row, against the
// destination sprint only, so the source sprint's history had no record of
// losing the tasks and no row named the task that moved.
//
// Every test here therefore asserts the ids a row carries, never merely how many
// rows exist. A count-only assertion is exactly what let the defect survive.
//
// The governing rule is one sentence — entity_type/entity_id name the entity
// whose history the row belongs to, related_entity_id names the counterpart of
// the operation that produced it — and SPEC/DATABASE.md § The Two Entities of a
// Relational Operation applies it in an eight-case table. The commands are
// driven for real, so what is measured is what the binary stores.
package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ---------------------------------------------------------------------------
// The fixture
// ---------------------------------------------------------------------------

// membershipFixture is a roadmap with two PENDING sprints and four BACKLOG
// tasks, built through the real commands so every state met below is a state
// the CLI can actually produce. Two sprints, because a move needs a source and
// a destination and the whole point of the pair of rows is telling them apart.
type membershipFixture struct {
	roadmap  string
	database *db.DB
	source   int
	dest     int
	taskIDs  []int
}

// setupMembershipRoadmap builds the fixture.
func setupMembershipRoadmap(t *testing.T, name string) *membershipFixture {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	f := &membershipFixture{roadmap: name, database: database}

	seedTasks := []struct{ title, functional, technical, acceptance string }{
		{
			"Pin the TLS cipher suite list to the approved set",
			"Only the cipher suites on the approved list are offered to a client.",
			"Set CipherSuites and MinVersion explicitly on the server TLS config.",
			"A handshake offering only a retired suite is refused.",
		},
		{
			"Expire idle database connections before the proxy does",
			"A query never fails because the pool handed out a connection the proxy had closed.",
			"Set ConnMaxIdleTime below the proxy idle timeout and verify against the pool metrics.",
			"No connection-reset error appears in an hour of idle-then-query traffic.",
		},
		{
			"Emit a request id on every access log line",
			"Two log lines from one request can be correlated without timestamps.",
			"Generate the id in the outermost middleware and carry it on the request context.",
			"Every line of a single request's log carries the same request id.",
		},
		{
			"Back the rate limiter with the shared counter store",
			"A limit is enforced across every instance, not per instance.",
			"Replace the in-process bucket with the shared store, keyed on the account.",
			"Two instances together allow the documented rate, not twice it.",
		},
	}
	for _, s := range seedTasks {
		run(t, func() error {
			return taskCreate([]string{
				"-r", name, "-t", s.title, "-fr", s.functional, "-tr", s.technical, "-ac", s.acceptance,
			})
		})
	}

	tasks, err := database.ListTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded tasks back: %v", err)
	}
	if len(tasks) != len(seedTasks) {
		t.Fatalf("seeded %d tasks, found %d", len(seedTasks), len(tasks))
	}
	for i := range tasks {
		f.taskIDs = append(f.taskIDs, tasks[i].ID)
	}

	for _, s := range []struct{ title, description string }{
		{"Transport hardening", "Close the TLS and connection-pool findings raised by the review."},
		{"Observability rollout", "Make a single request traceable end to end across the fleet."},
	} {
		run(t, func() error {
			return sprintCreate([]string{"-r", name, "-t", s.title, "-d", s.description})
		})
	}

	sprints, err := database.ListSprints(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded sprints back: %v", err)
	}
	if len(sprints) != 2 {
		t.Fatalf("seeded 2 sprints, found %d", len(sprints))
	}
	f.source, f.dest = sprints[0].ID, sprints[1].ID

	return f
}

// csv renders ids the way the CLI takes them.
func csv(ids ...int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = itoa(id)
	}
	return strings.Join(parts, ",")
}

// ---------------------------------------------------------------------------
// Reading the rows back
// ---------------------------------------------------------------------------

// relationalRow is an audit row reduced to the four columns this file is about.
// related is -1 when the stored column is NULL, so a test can tell "no
// counterpart" from a counterpart that happens to be entity 0 — a value the
// column CHECK rejects, and therefore a value that could only arrive through a
// defect.
type relationalRow struct {
	operation   string
	entityType  string
	entityID    int
	related     int
	performedAt string
}

const noCounterpart = -1

// rowsFor returns every audit row carrying one operation, in id order.
func rowsFor(t *testing.T, database *db.DB, op models.AuditOperation) []relationalRow {
	t.Helper()

	rows, err := database.Query(
		`SELECT entity_type, entity_id, COALESCE(related_entity_id, ?), performed_at
		   FROM audit WHERE operation = ? ORDER BY id`, noCounterpart, string(op))
	if err != nil {
		t.Fatalf("reading the %s rows: %v", op, err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	out := make([]relationalRow, 0, 8)
	for rows.Next() {
		r := relationalRow{operation: string(op)}
		if err := rows.Scan(&r.entityType, &r.entityID, &r.related, &r.performedAt); err != nil {
			t.Fatalf("scanning a %s row: %v", op, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the %s rows: %v", op, err)
	}
	return out
}

// relatedIDsOf renders the counterpart column of a row set, for assertions on
// the whole set and for failure messages that show it.
func relatedIDsOf(rows []relationalRow) []int {
	out := make([]int, len(rows))
	for i, r := range rows {
		out[i] = r.related
	}
	return out
}

// countMirrors runs the self-join that IS the mirror invariant: it counts the
// rows of the entity side that have a matching row on the counterpart side with
// the two ids transposed and the same performed_at.
//
// Expressing it as a join rather than as two Go loops is deliberate. The
// invariant is a statement about pairs, and a join either finds the partner row
// or it does not; a pair of loops over two separately-read row sets can agree on
// the counts while pairing the wrong rows with each other.
func countMirrors(t *testing.T, database *db.DB, entitySide, counterpartSide models.AuditOperation) int {
	t.Helper()

	return countRows(t, database,
		`SELECT COUNT(*)
		   FROM audit a
		   JOIN audit b
		     ON b.operation         = ?
		    AND b.entity_id         = a.related_entity_id
		    AND b.related_entity_id = a.entity_id
		    AND b.performed_at      = a.performed_at
		  WHERE a.operation = ?`,
		string(counterpartSide), string(entitySide))
}

// distinctCount reports how many distinct values a column holds across the rows
// of one operation. Three rows naming one task and three rows naming three
// tasks are the same row count and the opposite record.
func distinctCount(t *testing.T, database *db.DB, op models.AuditOperation, column string) int {
	t.Helper()

	// The column name is a literal from the call sites below, never input.
	query := fmt.Sprintf( // #nosec G201 -- column is a compile-time literal, the value is bound
		`SELECT COUNT(DISTINCT %s) FROM audit WHERE operation = ?`, column)
	return countRows(t, database, query, string(op))
}

// historyOf returns what `rmp audit history <type> <id>` prints, decoded.
func historyOf(t *testing.T, f *membershipFixture, entityType models.EntityType, id int) []models.AuditEntry {
	t.Helper()

	var err error
	out := captureStdout(t, func() {
		err = auditHistory([]string{"-r", f.roadmap, string(entityType), itoa(id)})
	})
	if err != nil {
		t.Fatalf("`audit history %s %d` failed: %v", entityType, id, err)
	}

	var entries []models.AuditEntry
	if jsonErr := json.Unmarshal([]byte(out), &entries); jsonErr != nil {
		t.Fatalf("decoding `audit history %s %d` output %q: %v", entityType, id, out, jsonErr)
	}
	return entries
}

// ---------------------------------------------------------------------------
// 1. The reported defect: three rows that named nothing
// ---------------------------------------------------------------------------

// TestSprintAddTasks_EveryEntryNamesItsOwnTaskAndTheTaskNamesTheSprint is the
// regression test for the defect as reported. Three tasks are added in one
// invocation, which is what produced the three indistinguishable rows.
//
// It asserts the two halves the defect had in common — the sprint's rows say
// which task, and the task has a row at all — and it asserts them on ids, so it
// fails against the previous commit on both counts: there the three
// SPRINT_ADD_TASK rows carried NULL and no TASK_STATUS_SPRINT row was ever
// written by anything.
func TestSprintAddTasks_EveryEntryNamesItsOwnTaskAndTheTaskNamesTheSprint(t *testing.T) {
	f := setupMembershipRoadmap(t, "membership-audit-add")

	added := f.taskIDs[:3]
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.source), csv(added...)})
	})

	// The sprint's side: one row per task, against the sprint, naming the task.
	sprintRows := rowsFor(t, f.database, models.OpSprintAddTask)
	if len(sprintRows) != len(added) {
		t.Fatalf("`sprint add-tasks` wrote %d %s rows for %d tasks, want one per task",
			len(sprintRows), models.OpSprintAddTask, len(added))
	}
	for i, r := range sprintRows {
		if r.entityType != string(models.EntitySprint) || r.entityID != f.source {
			t.Errorf("%s row %d is recorded against %s #%d, want SPRINT #%d: the row belongs to the "+
				"sprint's history", models.OpSprintAddTask, i, r.entityType, r.entityID, f.source)
		}
		if r.related != added[i] {
			t.Errorf("%s row %d names task #%d, want #%d", models.OpSprintAddTask, i, r.related, added[i])
		}
	}

	// Three DISTINCT tasks. This is the assertion the reported defect fails:
	// three rows that all name nothing, or all name the same task, are three
	// rows and still no record of which tasks were added.
	if n := distinctCount(t, f.database, models.OpSprintAddTask, "related_entity_id"); n != len(added) {
		t.Errorf("the %d %s rows name %d distinct tasks, want %d; rows that do not name their own task "+
			"are indistinguishable and the sprint's history does not say what was added\nnamed: %v",
			len(sprintRows), models.OpSprintAddTask, n, len(added), relatedIDsOf(sprintRows))
	}

	// The task's side: one row per task, against the task, naming the sprint.
	taskRows := rowsFor(t, f.database, models.OpTaskStatusSprint)
	if len(taskRows) != len(added) {
		t.Fatalf("`sprint add-tasks` wrote %d %s rows for %d tasks, want one per task; without them the "+
			"task's own history is silent about a status change from BACKLOG to SPRINT",
			len(taskRows), models.OpTaskStatusSprint, len(added))
	}
	for i, r := range taskRows {
		if r.entityType != string(models.EntityTask) || r.entityID != added[i] {
			t.Errorf("%s row %d is recorded against %s #%d, want TASK #%d",
				models.OpTaskStatusSprint, i, r.entityType, r.entityID, added[i])
		}
		if r.related != f.source {
			t.Errorf("%s row %d names sprint #%d, want #%d: a row that says the task joined a sprint "+
				"without saying which one is not a record of the operation",
				models.OpTaskStatusSprint, i, r.related, f.source)
		}
	}

	// And the reader the defect report used: `audit history TASK <id>` showed
	// nothing at all for a task that had just changed status.
	for _, id := range added {
		history := historyOf(t, f, models.EntityTask, id)
		found := false
		for _, e := range history {
			if e.Operation != string(models.OpTaskStatusSprint) {
				continue
			}
			found = true
			if e.RelatedEntityID == nil {
				t.Errorf("`audit history TASK %d` reports the task entering a sprint without naming it", id)
			} else if *e.RelatedEntityID != f.source {
				t.Errorf("`audit history TASK %d` names sprint #%d, want #%d", id, *e.RelatedEntityID, f.source)
			}
		}
		if !found {
			t.Errorf("`audit history TASK %d` shows no %s entry; the task's status changed from BACKLOG "+
				"to SPRINT and its own history says nothing about it (SPEC/COMMANDS.md § Task "+
				"Assignment, rule 2)", id, models.OpTaskStatusSprint)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. The mirror invariant
// ---------------------------------------------------------------------------

// TestSprintMembership_TheTwoEntriesOfAChangeAreMirrors states the invariant as
// the self-join SPEC/DATABASE.md § The Two Entities of a Relational Operation
// acceptance criterion 4 describes: for each row on the sprint's side there is a
// row on the task's side with the two ids transposed and the same performed_at.
//
// Both directions are counted, and both are compared with the row count of the
// side they join from, so an unmatched row on either side fails: a pair that
// shared no timestamp, or that named the wrong counterpart, joins nothing.
func TestSprintMembership_TheTwoEntriesOfAChangeAreMirrors(t *testing.T) {
	f := setupMembershipRoadmap(t, "membership-audit-mirror")

	moved := f.taskIDs[:3]
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.source), csv(moved...)})
	})
	run(t, func() error {
		return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.source), csv(moved...)})
	})

	for _, pair := range []struct {
		sprintSide, taskSide models.AuditOperation
	}{
		{models.OpSprintAddTask, models.OpTaskStatusSprint},
		{models.OpSprintRemoveTask, models.OpTaskStatusBacklog},
	} {
		t.Run(string(pair.sprintSide), func(t *testing.T) {
			sprintRows := len(rowsFor(t, f.database, pair.sprintSide))
			taskRows := len(rowsFor(t, f.database, pair.taskSide))
			if sprintRows != len(moved) || taskRows != len(moved) {
				t.Fatalf("the invocation wrote %d %s rows and %d %s rows for %d tasks, want one of each "+
					"per task", sprintRows, pair.sprintSide, taskRows, pair.taskSide, len(moved))
			}

			if n := countMirrors(t, f.database, pair.sprintSide, pair.taskSide); n != sprintRows {
				t.Errorf("%d of the %d %s rows have a mirrored %s row (transposed ids, shared "+
					"performed_at), want all of them", n, sprintRows, pair.sprintSide, pair.taskSide)
			}
			if n := countMirrors(t, f.database, pair.taskSide, pair.sprintSide); n != taskRows {
				t.Errorf("%d of the %d %s rows have a mirrored %s row, want all of them",
					n, taskRows, pair.taskSide, pair.sprintSide)
			}
		})
	}

	// One performed_at for the whole invocation, not merely one per pair: the
	// add wrote six rows and the remove another six, and each command captured
	// its timestamp once (SPEC/COMMANDS.md § Task Assignment).
	for _, ops := range [][]models.AuditOperation{
		{models.OpSprintAddTask, models.OpTaskStatusSprint},
		{models.OpSprintRemoveTask, models.OpTaskStatusBacklog},
	} {
		n := countRows(t, f.database,
			`SELECT COUNT(DISTINCT performed_at) FROM audit WHERE operation IN (?, ?)`,
			string(ops[0]), string(ops[1]))
		if n != 1 {
			t.Errorf("the %s / %s rows of one invocation carry %d distinct performed_at values, want 1",
				ops[0], ops[1], n)
		}
	}
}

// ---------------------------------------------------------------------------
// 3. Removal writes the symmetric pair
// ---------------------------------------------------------------------------

// TestSprintRemoveTasks_WritesTheSymmetricPair pins the removal side row by row.
// The task entry is what makes `audit history TASK <id>` able to say which
// sprint the task left, and it is written for every task named on the command
// line — including one already in BACKLOG status while still a sprint member,
// which is the case a count taken from the status transition alone would miss
// (SPEC/COMMANDS.md § Task Assignment).
func TestSprintRemoveTasks_WritesTheSymmetricPair(t *testing.T) {
	f := setupMembershipRoadmap(t, "membership-audit-remove")

	members := f.taskIDs[:3]
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.source), csv(members...)})
	})

	// One member is driven back to BACKLOG while staying in the sprint, so the
	// removal below has no status change to make for it and must write its
	// entry all the same.
	alreadyBacklog := members[2]
	run(t, func() error {
		return taskSetStatus([]string{"-r", f.roadmap, itoa(alreadyBacklog), "BACKLOG"})
	})

	removed := members
	run(t, func() error {
		return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.source), csv(removed...)})
	})

	sprintRows := rowsFor(t, f.database, models.OpSprintRemoveTask)
	if len(sprintRows) != len(removed) {
		t.Fatalf("`sprint remove-tasks` wrote %d %s rows for %d tasks, want one per task",
			len(sprintRows), models.OpSprintRemoveTask, len(removed))
	}
	for i, r := range sprintRows {
		if r.entityType != string(models.EntitySprint) || r.entityID != f.source {
			t.Errorf("%s row %d is recorded against %s #%d, want SPRINT #%d",
				models.OpSprintRemoveTask, i, r.entityType, r.entityID, f.source)
		}
		if r.related != removed[i] {
			t.Errorf("%s row %d names task #%d, want #%d",
				models.OpSprintRemoveTask, i, r.related, removed[i])
		}
	}

	// The task side. The removal's rows are the ones naming a sprint; the one
	// `task stat` wrote above names nothing, and both operations are
	// TASK_STATUS_BACKLOG, so the column is what tells them apart.
	backlogRows := rowsFor(t, f.database, models.OpTaskStatusBacklog)
	named := make(map[int]int, len(removed))
	unnamed := 0
	for _, r := range backlogRows {
		if r.related == noCounterpart {
			unnamed++
			continue
		}
		if r.related != f.source {
			t.Errorf("a %s row against task #%d names sprint #%d, want #%d",
				models.OpTaskStatusBacklog, r.entityID, r.related, f.source)
		}
		named[r.entityID]++
	}
	if unnamed != 1 {
		t.Errorf("%d %s rows name no counterpart, want 1 (the one `task stat` wrote); the same operation "+
			"is written by two commands and only one of them has a sprint to name",
			unnamed, models.OpTaskStatusBacklog)
	}
	for _, id := range removed {
		if named[id] != 1 {
			t.Errorf("task #%d has %d %s rows naming the sprint it left, want exactly 1 — one per task "+
				"named on the command line, whatever its status was", id, named[id], models.OpTaskStatusBacklog)
		}
	}
}

// ---------------------------------------------------------------------------
// 4. A move is two sprint rows and no task row
// ---------------------------------------------------------------------------

// TestSprintMoveTasks_WritesOneEntryPerSprintAndNoTaskStatusEntry pins the whole
// record of a move: one row against the source sprint and one against the
// destination, both naming the task, and nothing against the task itself —
// because the move preserves the task's status, so nothing happened to the
// task's lifecycle for a status row to record (SPEC/COMMANDS.md § Task
// Assignment, rule 4).
//
// The absence is asserted over every TASK_STATUS_* operation rather than the one
// a reader would expect, so a move that wrote any of them fails here.
func TestSprintMoveTasks_WritesOneEntryPerSprintAndNoTaskStatusEntry(t *testing.T) {
	f := setupMembershipRoadmap(t, "membership-audit-move")

	moved := f.taskIDs[:2]
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.source), csv(moved...)})
	})

	statusBefore := map[models.AuditOperation]int{}
	for _, op := range taskStatusOperations() {
		statusBefore[op] = countRows(t, f.database,
			`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(op))
	}

	run(t, func() error {
		return sprintMoveTasks([]string{"-r", f.roadmap, itoa(f.source), itoa(f.dest), csv(moved...)})
	})

	for _, side := range []struct {
		op     models.AuditOperation
		sprint int
		what   string
	}{
		{models.OpSprintMoveTaskOut, f.source, "the sprint the task left"},
		{models.OpSprintMoveTaskIn, f.dest, "the sprint the task entered"},
	} {
		rows := rowsFor(t, f.database, side.op)
		if len(rows) != len(moved) {
			t.Fatalf("the move wrote %d %s rows for %d tasks, want one per task",
				len(rows), side.op, len(moved))
		}
		for i, r := range rows {
			if r.entityType != string(models.EntitySprint) || r.entityID != side.sprint {
				t.Errorf("%s row %d is recorded against %s #%d, want SPRINT #%d (%s)",
					side.op, i, r.entityType, r.entityID, side.sprint, side.what)
			}
			if r.related != moved[i] {
				t.Errorf("%s row %d names task #%d, want #%d", side.op, i, r.related, moved[i])
			}
		}
	}

	// The two rows of one move share a performed_at, as every pair does.
	if n := countRows(t, f.database,
		`SELECT COUNT(DISTINCT performed_at) FROM audit WHERE operation IN (?, ?)`,
		string(models.OpSprintMoveTaskOut), string(models.OpSprintMoveTaskIn)); n != 1 {
		t.Errorf("the move's rows carry %d distinct performed_at values, want 1", n)
	}

	// Nothing against the task.
	for op, before := range statusBefore {
		if after := countRows(t, f.database,
			`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(op)); after != before {
			t.Errorf("the move wrote %d %s rows; it changes no task's status, so the two sprint rows are "+
				"the whole record of it", after-before, op)
		}
	}
	for _, id := range moved {
		if n := countRows(t, f.database,
			`SELECT COUNT(*) FROM audit WHERE entity_type = ? AND entity_id = ? AND performed_at = `+
				`(SELECT performed_at FROM audit WHERE operation = ? LIMIT 1)`,
			string(models.EntityTask), id, string(models.OpSprintMoveTaskIn)); n != 0 {
			t.Errorf("the move wrote %d rows against task #%d; it writes no entry with "+
				"entity_type = TASK at all", n, id)
		}
	}
}

// taskStatusOperations returns the five destination-named status operations,
// which are exactly the operations a move must not write.
func taskStatusOperations() []models.AuditOperation {
	return []models.AuditOperation{
		models.OpTaskStatusBacklog, models.OpTaskStatusSprint, models.OpTaskStatusDoing,
		models.OpTaskStatusTesting, models.OpTaskStatusCompleted,
	}
}

// ---------------------------------------------------------------------------
// 5. A dependency states its own direction
// ---------------------------------------------------------------------------

// TestTaskDependency_TheTwoEntriesAreMirrors pins both dependency commands. Each
// writes one row against each task of the pair, and each row names the other,
// so reading either task's history shows WHICH dependency the row concerns and
// two rows from two different invocations are distinguishable (SPEC/COMMANDS.md
// § Add Task Dependency).
//
// A second dependency is added between a different pair, so the assertion is not
// satisfied by an implementation that names any task at all: with two
// invocations in the table, naming the wrong one still fails.
func TestTaskDependency_TheTwoEntriesAreMirrors(t *testing.T) {
	f := setupMembershipRoadmap(t, "membership-audit-dep")

	dependent, dependency := f.taskIDs[0], f.taskIDs[1]
	otherDependent, otherDependency := f.taskIDs[2], f.taskIDs[3]

	run(t, func() error {
		return taskAddDep([]string{"-r", f.roadmap, itoa(dependent), itoa(dependency)})
	})
	run(t, func() error {
		return taskAddDep([]string{"-r", f.roadmap, itoa(otherDependent), itoa(otherDependency)})
	})

	assertDependencyPair := func(t *testing.T, op models.AuditOperation, a, b int) {
		t.Helper()

		want := map[int]int{a: b, b: a}
		got := make(map[int]int, 2)
		for _, r := range rowsFor(t, f.database, op) {
			if r.entityID != a && r.entityID != b {
				continue // the other invocation's pair
			}
			if r.entityType != string(models.EntityTask) {
				t.Errorf("a %s row is recorded against %s #%d, want TASK", op, r.entityType, r.entityID)
			}
			got[r.entityID] = r.related
		}
		if len(got) != len(want) {
			t.Fatalf("`%s` wrote %d rows for the pair (#%d, #%d), want one against each",
				op, len(got), a, b)
		}
		for entity, counterpart := range want {
			if got[entity] != counterpart {
				t.Errorf("the %s row against task #%d names #%d, want #%d: the two rows carry transposed "+
					"ids, so each states its own direction", op, entity, got[entity], counterpart)
			}
		}
	}

	assertDependencyPair(t, models.OpTaskAddDep, dependent, dependency)
	assertDependencyPair(t, models.OpTaskAddDep, otherDependent, otherDependency)

	// The removal writes the same arrangement.
	run(t, func() error {
		return taskRemoveDep([]string{"-r", f.roadmap, itoa(dependent), itoa(dependency)})
	})
	run(t, func() error {
		return taskRemoveDep([]string{"-r", f.roadmap, itoa(otherDependent), itoa(otherDependency)})
	})
	assertDependencyPair(t, models.OpTaskRemoveDep, dependent, dependency)
	assertDependencyPair(t, models.OpTaskRemoveDep, otherDependent, otherDependency)

	// Each invocation's two rows are one operation, so the mirror join holds
	// over the whole set as it does for a membership change.
	for _, op := range []models.AuditOperation{models.OpTaskAddDep, models.OpTaskRemoveDep} {
		rows := len(rowsFor(t, f.database, op))
		if n := countMirrors(t, f.database, op, op); n != rows {
			t.Errorf("%d of the %d %s rows have a mirrored partner (transposed ids, shared "+
				"performed_at), want all of them", n, rows, op)
		}
	}
}

// ---------------------------------------------------------------------------
// 6. The operation the pair replaced
// ---------------------------------------------------------------------------

// TestSprintMoveTask_LegacyOperationIsNeverWrittenButStaysFilterable pins both
// halves of what LEGACY means for SPRINT_MOVE_TASK, because either half alone is
// a defect: no command may write it again, and the rows a previous version wrote
// must stay reachable by name.
//
// The stored row is seeded with a verbatim INSERT rather than through LogAuditTx,
// because the writer refuses a legacy operation's counterpart and the row a
// 1.11.0 binary wrote carried none anyway.
func TestSprintMoveTask_LegacyOperationIsNeverWrittenButStaysFilterable(t *testing.T) {
	f := setupMembershipRoadmap(t, "membership-audit-legacy-move")

	moved := f.taskIDs[:2]
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.source), csv(moved...)})
	})
	run(t, func() error {
		return sprintMoveTasks([]string{"-r", f.roadmap, itoa(f.source), itoa(f.dest), csv(moved...)})
	})
	run(t, func() error {
		return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.dest), csv(moved...)})
	})

	// Half 1: nothing any of the three commands did wrote the legacy operation.
	if n := countRows(t, f.database,
		`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(models.OpSprintMoveTask)); n != 0 {
		t.Errorf("%d %s rows were written by commands running at schema 1.12.0; the operation is LEGACY "+
			"and no code path may produce one (SPEC/DATABASE.md § audit Table, Legacy)",
			n, models.OpSprintMoveTask)
	}

	// A stored row from before the move was split into its two directions. It
	// names one sprint and no task, which is precisely why the migration cannot
	// reclassify it and why the value has to stay readable.
	const legacyStamp = "2026-02-11T16:42:09.117Z"
	if _, err := f.database.Exec(
		`INSERT INTO audit (operation, entity_type, entity_id, performed_at) VALUES (?, ?, ?, ?)`,
		string(models.OpSprintMoveTask), string(models.EntitySprint), f.dest, legacyStamp,
	); err != nil {
		t.Fatalf("seeding the legacy audit row: %v", err)
	}

	// Half 2: the filter still accepts the name and reaches the row.
	if !models.IsValidAuditOperation(string(models.OpSprintMoveTask)) {
		t.Fatalf("%s is no longer in the valid set, so `audit list --operation %s` is rejected and the "+
			"stored rows carrying it are unreachable by name",
			models.OpSprintMoveTask, models.OpSprintMoveTask)
	}

	var listErr error
	out := captureStdout(t, func() {
		listErr = auditList([]string{"-r", f.roadmap, "--operation", string(models.OpSprintMoveTask)})
	})
	if listErr != nil {
		t.Fatalf("`audit list --operation %s` failed: %v", models.OpSprintMoveTask, listErr)
	}

	var listed []models.AuditEntry
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decoding `audit list` output %q: %v", out, err)
	}
	if len(listed) != 1 {
		t.Fatalf("`audit list --operation %s` returned %d entries, want the 1 legacy entry stored",
			models.OpSprintMoveTask, len(listed))
	}
	if listed[0].PerformedAt != legacyStamp || listed[0].EntityID != f.dest {
		t.Errorf("the returned entry is %+v, want the seeded one (sprint #%d at %s)",
			listed[0], f.dest, legacyStamp)
	}
	if listed[0].RelatedEntityID != nil {
		t.Errorf("the legacy entry names counterpart #%d; a row written before the column existed "+
			"carries NULL, which is what makes it unreclassifiable", *listed[0].RelatedEntityID)
	}
}

// ---------------------------------------------------------------------------
// 7. The column means the counterpart, not "the sprint"
// ---------------------------------------------------------------------------

// TestTaskStatBacklog_NamesNoCounterpart is the worked contrast SPEC/DATABASE.md
// § The Two Entities of a Relational Operation draws in full: one operation,
// TASK_STATUS_BACKLOG, two producing commands, and one rule.
//
// `sprint remove-tasks` names the sprint the task left because the sprint is
// party to that operation; `task stat <id> BACKLOG` names nothing because no
// second entity is. A reader never has to know which command wrote a row: NULL
// says "this operation had no counterpart", never "it had one and it was not
// recorded".
//
// The two rows are produced against the SAME task, in one test, so an
// implementation that read the column as "the sprint this task is in" — which
// would name a sprint on both, the task still being a member when `task stat`
// runs — fails here rather than passing two separate tests.
func TestTaskStatBacklog_NamesNoCounterpart(t *testing.T) {
	f := setupMembershipRoadmap(t, "membership-audit-stat-backlog")

	id := f.taskIDs[0]
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.source), itoa(id)})
	})

	// `task stat <id> BACKLOG` leaves the task a member of the sprint
	// (SPEC/STATE_MACHINE.md § Sprint Membership and the BACKLOG Status), so a
	// sprint is available to name and must still not be named.
	run(t, func() error {
		return taskSetStatus([]string{"-r", f.roadmap, itoa(id), "BACKLOG"})
	})

	rows := rowsFor(t, f.database, models.OpTaskStatusBacklog)
	if len(rows) != 1 {
		t.Fatalf("`task stat %d BACKLOG` produced %d %s rows, want 1", id, len(rows), models.OpTaskStatusBacklog)
	}
	if rows[0].entityType != string(models.EntityTask) || rows[0].entityID != id {
		t.Errorf("the row is recorded against %s #%d, want TASK #%d",
			rows[0].entityType, rows[0].entityID, id)
	}
	if rows[0].related != noCounterpart {
		t.Errorf("the row names counterpart #%d, want NULL: `task stat` has no second entity party to "+
			"the operation, and the task's sprint membership is not one — the column means the "+
			"counterpart of the operation, not the sprint the task happens to be in", rows[0].related)
	}

	// Still a member, so the sprint the row declined to name really was there.
	if n := countRows(t, f.database,
		`SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?`, f.source, id); n != 1 {
		t.Fatalf("task #%d is no longer a member of sprint #%d, so the assertion above is vacuous: "+
			"there was no sprint to name either way", id, f.source)
	}

	// The other producing command, on the same task, does name it.
	run(t, func() error {
		return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.source), itoa(id)})
	})
	rows = rowsFor(t, f.database, models.OpTaskStatusBacklog)
	if len(rows) != 2 {
		t.Fatalf("the removal produced %d %s rows in total, want 2", len(rows), models.OpTaskStatusBacklog)
	}
	if rows[1].related != f.source {
		t.Errorf("the %s row written by `sprint remove-tasks` names counterpart #%d, want sprint #%d; "+
			"the same operation carries a counterpart from one command and not the other, which is the "+
			"governing rule applied consistently and not a per-command exception",
			models.OpTaskStatusBacklog, rows[1].related, f.source)
	}
}

// ---------------------------------------------------------------------------
// 8. A rejected command writes nothing
// ---------------------------------------------------------------------------

// TestSprintMembership_ARejectedCommandWritesNoEntry covers the failure
// direction for the new rows: a command rejected at any validation step leaves
// neither half of a pair behind (SPEC/COMMANDS.md § Task Assignment, acceptance
// criterion 7).
//
// Both rejections here are validation rejections, refused before any row is
// written: the capacity violation by the pre-check `sprint add-tasks` runs
// before it calls the db layer, and the non-member removal by the fail-fast
// membership check. The case where rows ARE written and then rolled back is the
// subject of TestSprintMembership_AFailedTransactionLeavesNoPairBehind below,
// which injects a failure the validation cannot pre-empt.
func TestSprintMembership_ARejectedCommandWritesNoEntry(t *testing.T) {
	f := setupMembershipRoadmap(t, "membership-audit-rejected")

	// A capped sprint: adding two tasks to it must fail and roll back.
	if _, err := f.database.Exec(
		`UPDATE sprints SET max_tasks = ? WHERE id = ?`, 1, f.dest); err != nil {
		t.Fatalf("capping the destination sprint: %v", err)
	}

	relational := []models.AuditOperation{
		models.OpSprintAddTask, models.OpTaskStatusSprint,
		models.OpSprintRemoveTask, models.OpTaskStatusBacklog,
	}
	before := map[models.AuditOperation]int{}
	for _, op := range relational {
		before[op] = countRows(t, f.database,
			`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(op))
	}

	overCapacity := f.taskIDs[:2]
	if err := sprintAddTasks([]string{"-r", f.roadmap, itoa(f.dest), csv(overCapacity...)}); err == nil {
		t.Fatal("adding 2 tasks to a sprint capped at 1 succeeded; the capacity check did not run")
	}

	notAMember := f.taskIDs[3]
	if err := sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.source), itoa(notAMember)}); err == nil {
		t.Fatalf("removing task #%d from a sprint it does not belong to succeeded", notAMember)
	}

	for _, op := range relational {
		if after := countRows(t, f.database,
			`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(op)); after != before[op] {
			t.Errorf("the rejected commands left %d %s rows behind; every audit write happens inside the "+
				"transaction that performs the mutation, so a rejected command writes none",
				after-before[op], op)
		}
	}
}

// TestSprintMembership_AFailedTransactionLeavesNoPairBehind is the other half of
// the atomicity guarantee, and the half a validation rejection cannot show: here
// the membership insert and the status update succeed and the audit write fails,
// so there is something to roll back.
//
// The failure is injected the only way it can be without touching production
// code — by moving the audit table aside, so every statement before the audit
// INSERT succeeds and that one does not. A rejection at validation would prove
// nothing about the transaction: those happen before the database is written at
// all (SPEC/DATABASE.md § Transactional Atomicity Guarantees).
//
// What is asserted afterwards is the whole transaction, not just the audit: an
// implementation that wrote its audit rows outside the transaction that performs
// the mutation would leave the membership committed here, which is exactly the
// shape the guarantee forbids.
func TestSprintMembership_AFailedTransactionLeavesNoPairBehind(t *testing.T) {
	f := setupMembershipRoadmap(t, "membership-audit-rollback")

	added := f.taskIDs[:2]
	// The fixture's own task and sprint creations are already audited, so what
	// the rollback must leave unchanged is this count, not an empty table.
	before := countRows(t, f.database, `SELECT COUNT(*) FROM audit`)

	// The handler reopens the roadmap, so the schema change has to be committed.
	if err := f.database.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE audit RENAME TO audit_moved_aside`)
		return err
	}); err != nil {
		t.Fatalf("moving the audit table aside: %v", err)
	}

	err := sprintAddTasks([]string{"-r", f.roadmap, itoa(f.source), csv(added...)})
	if err == nil {
		t.Fatal("`sprint add-tasks` succeeded with the audit table moved aside")
	}
	if !strings.Contains(err.Error(), "audit") {
		t.Fatalf("the injected failure is not the audit write, so the rollback below proves nothing: %v", err)
	}

	if err := f.database.WithTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(`ALTER TABLE audit_moved_aside RENAME TO audit`)
		return err
	}); err != nil {
		t.Fatalf("restoring the audit table: %v", err)
	}

	if n := countRows(t, f.database, `SELECT COUNT(*) FROM audit`); n != before {
		t.Errorf("the rolled-back transaction left %d audit rows behind (%d → %d)", n-before, before, n)
	}
	for _, id := range added {
		if n := countRows(t, f.database,
			`SELECT COUNT(*) FROM sprint_tasks WHERE task_id = ?`, id); n != 0 {
			t.Errorf("task #%d is a sprint member after the transaction rolled back; the membership "+
				"change and its audit rows commit together or not at all", id)
		}
		var status string
		if err := f.database.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&status); err != nil {
			t.Fatalf("reading the status of task #%d: %v", id, err)
		}
		if status != string(models.StatusBacklog) {
			t.Errorf("task #%d reads %s after the transaction rolled back, want BACKLOG", id, status)
		}
	}
}
