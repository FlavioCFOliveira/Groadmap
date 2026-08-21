// Package commands — regression tests for what a field edit records in the
// audit log (rmp task #264).
//
// An edit used to be recorded as TASK_UPDATE or SPRINT_UPDATE, which said that
// something about the entity had changed and never what. It is the same
// genericity that made TASK_STATUS_CHANGE useless, one level down, and it
// carried an inconsistency of its own: `task prio <id> 5` wrote
// TASK_PRIORITY_CHANGE while `task edit <id> -p 5` performed the identical
// mutation and wrote TASK_UPDATE, so a filter on TASK_PRIORITY_CHANGE found
// only half the priority changes in a roadmap.
//
// SPEC/COMMANDS.md § Edit Task and § Update Sprint replace both generic values
// with one operation per field. Five properties are pinned here, each
// separately because each can be implemented almost right:
//
//  1. **One row per supplied flag.** N flags write N rows, one naming each
//     field, and nothing for a field the invocation left alone.
//  2. **The two setter fields reuse the setter's operation.** `task edit -p 5`
//     and `task prio <id> 5` write the same operation value, which is the
//     inconsistency the change exists to remove.
//  3. **The trigger is the flag, not a difference in value.** A flag supplied
//     with the value already stored still writes its row. The command compares
//     nothing against the stored value, so the audit log records the commands
//     issued rather than the deltas they happened to produce.
//  4. **One timestamp, one transaction.** Every row of one invocation carries
//     the same performed_at, and a rejected edit writes none at all — including
//     a rejection raised after the UPDATE has already run.
//  5. **Neither legacy value is ever written**, while both stay reachable as an
//     `audit list --operation` filter, so the rows a former binary wrote are not
//     stranded.
//
// The row-level assertions read the audit table straight out of SQLite through
// the helpers in task_status_audit_test.go, for the reason given there: a read
// path that dropped a column would otherwise hide the defect from the very
// tests meant to catch it.
package commands

import (
	"context"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// fieldEditFixture is a roadmap holding one BACKLOG task and two PENDING
// sprints. The second sprint exists so that an `--order` collision — the one
// rejection `sprint update` raises after its UPDATE has already run — can be
// provoked without contriving anything.
type fieldEditFixture struct {
	roadmap       string
	database      *db.DB
	taskID        int
	sprintID      int
	otherSprintID int
	otherOrder    int
}

// The seeded values. They are spelled out as constants because several tests
// re-supply one of them unchanged, and the point of those tests is that the
// value is the one already stored.
const (
	fieldEditTitle      = "Cache the roadmap statistics query"
	fieldEditFunctional = "The statistics page answers from cache while the underlying counts are unchanged."
	fieldEditTechnical  = "Key the cache on the roadmap name and invalidate it on every mutating command."
	fieldEditAcceptance = "A second request inside the window issues no query against the tasks table."
	fieldEditPriority   = 4
	fieldEditSeverity   = 2

	fieldEditSprintTitle       = "Read-path performance"
	fieldEditSprintDescription = "Cut the cost of the read commands that every other command calls."
	fieldEditSprintMaxTasks    = 8
)

// setupFieldEditRoadmap builds the fixture through the real commands, so every
// state the tests meet is a state the CLI can actually produce.
func setupFieldEditRoadmap(t *testing.T, name string) *fieldEditFixture {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	f := &fieldEditFixture{roadmap: name, database: database}

	_ = captureStdout(t, func() {
		if err := taskCreate([]string{
			"-r", name, "-t", fieldEditTitle, "-fr", fieldEditFunctional,
			"-tr", fieldEditTechnical, "-ac", fieldEditAcceptance,
			"-p", itoa(fieldEditPriority), "--severity", itoa(fieldEditSeverity),
		}); err != nil {
			t.Fatalf("seeding the task: %v", err)
		}
	})
	tasks, err := database.ListTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded task back: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("seeded 1 task, found %d", len(tasks))
	}
	f.taskID = tasks[0].ID

	for _, s := range []struct{ title, description string }{
		{fieldEditSprintTitle, fieldEditSprintDescription},
		{"Write-path performance", "Cut the cost of the commands that mutate a roadmap."},
	} {
		_ = captureStdout(t, func() {
			if err := sprintCreate([]string{
				"-r", name, "-t", s.title, "-d", s.description,
				"--max-tasks", itoa(fieldEditSprintMaxTasks),
			}); err != nil {
				t.Fatalf("seeding sprint %q: %v", s.title, err)
			}
		})
	}
	sprints, err := database.ListSprints(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded sprints back: %v", err)
	}
	if len(sprints) != 2 {
		t.Fatalf("seeded 2 sprints, found %d", len(sprints))
	}
	f.sprintID, f.otherSprintID = sprints[0].ID, sprints[1].ID
	f.otherOrder = sprints[1].Order
	if f.otherOrder <= 0 {
		t.Fatalf("the second sprint carries order %d; the collision test needs a real one", f.otherOrder)
	}

	return f
}

// edit runs `task edit` against the fixture's task and fails on rejection.
func (f *fieldEditFixture) edit(t *testing.T, flags ...string) {
	t.Helper()

	args := append([]string{"-r", f.roadmap, itoa(f.taskID)}, flags...)
	out := captureStdout(t, func() {
		if err := taskEdit(args); err != nil {
			t.Fatalf("task edit %v: %v", args, err)
		}
	})
	if out != "" {
		t.Errorf("task edit %v printed %q; SPEC/COMMANDS.md gives it an empty success output", args, out)
	}
}

// update runs `sprint update` against the fixture's first sprint and fails on
// rejection.
func (f *fieldEditFixture) update(t *testing.T, flags ...string) {
	t.Helper()

	args := append([]string{"-r", f.roadmap, itoa(f.sprintID)}, flags...)
	out := captureStdout(t, func() {
		if err := sprintUpdate(args); err != nil {
			t.Fatalf("sprint update %v: %v", args, err)
		}
	})
	if out != "" {
		t.Errorf("sprint update %v printed %q; SPEC/COMMANDS.md gives it an empty success output", args, out)
	}
}

// auditRecordsForSprint returns the rows recorded against one sprint, in id
// order. It is the sprint-side twin of auditRecordsFor.
func auditRecordsForSprint(t *testing.T, database *db.DB, sprintID int) []auditRecord {
	t.Helper()

	all := readAuditTable(t, database)
	out := make([]auditRecord, 0, len(all))
	for _, r := range all {
		if r.entityType == string(models.EntitySprint) && r.entityID == sprintID {
			out = append(out, r)
		}
	}
	return out
}

// assertFieldEntries requires the rows added after `before` to be exactly want,
// in that order, each recorded against the given entity with both nullable
// columns NULL, and all of them sharing one performed_at.
func assertFieldEntries(t *testing.T, records []auditRecord, before int,
	entityType models.EntityType, entityID int, want []models.AuditOperation) {
	t.Helper()

	added := records[before:]
	if len(added) != len(want) {
		t.Fatalf("the invocation wrote %d entries, want %d (one per supplied field)\nwrote: %v\nwant:  %v",
			len(added), len(want), operationsOf(added), want)
	}
	for i := range want {
		if added[i].operation != string(want[i]) {
			t.Errorf("entry %d carries %s, want %s; the operation names the FIELD the invocation "+
				"supplied\nwrote: %v\nwant:  %v", i, added[i].operation, want[i], operationsOf(added), want)
		}
		if added[i].entityType != string(entityType) || added[i].entityID != entityID {
			t.Errorf("the %s entry is recorded against %s #%d, want %s #%d",
				added[i].operation, added[i].entityType, added[i].entityID, entityType, entityID)
		}
		if added[i].relatedEntityID.Valid {
			t.Errorf("the %s entry names entity %d as a counterpart; a field edit has none",
				added[i].operation, added[i].relatedEntityID.Int64)
		}
		if added[i].commitHash.Valid {
			t.Errorf("the %s entry carries the commit hash %q; only the two status operations do",
				added[i].operation, added[i].commitHash.String)
		}
		if added[i].performedAt != added[0].performedAt {
			t.Errorf("entry %d carries performed_at %q and entry 0 carries %q; the entries of one "+
				"invocation share one timestamp", i, added[i].performedAt, added[0].performedAt)
		}
	}
}

// ---------------------------------------------------------------------------
// 1. One row per supplied flag
// ---------------------------------------------------------------------------

// TestTaskEdit_OneFlagWritesOneEntryNamingItsField walks every flag `task edit`
// accepts, one invocation each, and requires exactly one entry carrying that
// field's operation.
//
// Each flag gets a fresh roadmap rather than sharing one, so the assertion is
// over the whole audit history of the task and not over a delta that a previous
// subtest could have polluted.
func TestTaskEdit_OneFlagWritesOneEntryNamingItsField(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		want  models.AuditOperation
	}{
		{"title", []string{"-t", "Cache the roadmap statistics query behind an invalidating key"}, models.OpTaskTitleChange},
		{"type", []string{"-y", "IMPROVEMENT"}, models.OpTaskTypeChange},
		{"functional", []string{"-fr", "The statistics page answers from cache for the whole window."}, models.OpTaskFunctionalRequirementsChange},
		{"technical", []string{"-tr", "Invalidate on every mutating command rather than on a timer."}, models.OpTaskTechnicalRequirementsChange},
		{"acceptance", []string{"-ac", "A second request inside the window issues no query at all."}, models.OpTaskAcceptanceCriteriaChange},
		{"priority", []string{"-p", "7"}, models.OpTaskPriorityChange},
		{"severity", []string{"--severity", "5"}, models.OpTaskSeverityChange},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setupFieldEditRoadmap(t, "fieldedit"+c.name)
			before := len(auditRecordsFor(t, f.database, f.taskID))

			f.edit(t, c.flags...)

			records := auditRecordsFor(t, f.database, f.taskID)
			assertFieldEntries(t, records, before, models.EntityTask, f.taskID,
				[]models.AuditOperation{c.want})
		})
	}
}

// TestTaskEdit_FourFlagsWriteFourEntriesAndNothingForTheRest is the count
// property stated the other way round: an invocation that supplies four fields
// writes four entries, one per field, and nothing at all for the three fields it
// left alone.
//
// The expected order is the order of the UPDATE statement, which sorts the
// column names: functional_requirements, priority, title, type. Asserting the
// sequence rather than a set is what catches a writer that emitted the right
// operations in an order unrelated to the statement it accompanied.
func TestTaskEdit_FourFlagsWriteFourEntriesAndNothingForTheRest(t *testing.T) {
	f := setupFieldEditRoadmap(t, "fieldeditfour")
	before := len(auditRecordsFor(t, f.database, f.taskID))

	f.edit(t,
		"-t", "Cache the roadmap statistics query behind an invalidating key",
		"-y", "IMPROVEMENT",
		"-fr", "The statistics page answers from cache for the whole window.",
		"-p", "7",
	)

	records := auditRecordsFor(t, f.database, f.taskID)
	assertFieldEntries(t, records, before, models.EntityTask, f.taskID, []models.AuditOperation{
		models.OpTaskFunctionalRequirementsChange,
		models.OpTaskPriorityChange,
		models.OpTaskTitleChange,
		models.OpTaskTypeChange,
	})

	// The three untouched fields left no trace anywhere in the roadmap.
	for _, untouched := range []models.AuditOperation{
		models.OpTaskTechnicalRequirementsChange,
		models.OpTaskAcceptanceCriteriaChange,
		models.OpTaskSeverityChange,
	} {
		if n := countRows(t, f.database,
			`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(untouched)); n != 0 {
			t.Errorf("the invocation wrote %d %s entries; the flag was not supplied, so the field "+
				"has no entry (SPEC/COMMANDS.md § Edit Task)", n, untouched)
		}
	}
}

// TestTaskEdit_NoFlagsWriteNoEntry is acceptance criterion 2 of
// SPEC/COMMANDS.md § Edit Task: a flagless edit is a successful no-op, not a
// rejection, and it writes nothing.
func TestTaskEdit_NoFlagsWriteNoEntry(t *testing.T) {
	f := setupFieldEditRoadmap(t, "fieldeditnoflags")
	before := len(readAuditTable(t, f.database))

	f.edit(t)

	if after := len(readAuditTable(t, f.database)); after != before {
		t.Errorf("a flagless `task edit` wrote %d entries, want 0", after-before)
	}
}

// TestSprintUpdate_OneFlagWritesOneEntryNamingItsField is the `sprint update`
// twin of the task walk above, over all four flags the command accepts.
func TestSprintUpdate_OneFlagWritesOneEntryNamingItsField(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		want  models.AuditOperation
	}{
		{"title", []string{"-t", "Read-path performance and caching"}, models.OpSprintTitleChange},
		{"description", []string{"-d", "Cut the cost of every read command the CLI runs."}, models.OpSprintDescriptionChange},
		{"maxtasks", []string{"--max-tasks", "12"}, models.OpSprintMaxTasksChange},
		{"order", []string{"--order", "9"}, models.OpSprintOrderChange},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setupFieldEditRoadmap(t, "sprintupd"+c.name)
			before := len(auditRecordsForSprint(t, f.database, f.sprintID))

			f.update(t, c.flags...)

			records := auditRecordsForSprint(t, f.database, f.sprintID)
			assertFieldEntries(t, records, before, models.EntitySprint, f.sprintID,
				[]models.AuditOperation{c.want})
		})
	}
}

// TestSprintUpdate_AllFourFlagsWriteFourEntriesInStatementOrder is acceptance
// criterion 1 of SPEC/COMMANDS.md § Update Sprint, taken to the full width of
// the command: four flags, four entries, one shared performed_at, in the order
// the UPDATE applies the columns.
func TestSprintUpdate_AllFourFlagsWriteFourEntriesInStatementOrder(t *testing.T) {
	f := setupFieldEditRoadmap(t, "sprintupdall")
	before := len(auditRecordsForSprint(t, f.database, f.sprintID))

	f.update(t,
		"-t", "Read-path performance and caching",
		"-d", "Cut the cost of every read command the CLI runs.",
		"--max-tasks", "12",
		"--order", "9",
	)

	records := auditRecordsForSprint(t, f.database, f.sprintID)
	assertFieldEntries(t, records, before, models.EntitySprint, f.sprintID, []models.AuditOperation{
		models.OpSprintTitleChange,
		models.OpSprintDescriptionChange,
		models.OpSprintMaxTasksChange,
		models.OpSprintOrderChange,
	})
}

// ---------------------------------------------------------------------------
// 2. The setter fields reuse the setter command's operation
// ---------------------------------------------------------------------------

// TestTaskEdit_PriorityAndSeverityWriteTheSetterCommandsOperation is the
// inconsistency this change exists to remove, asserted directly: `task edit -p`
// and `task prio` write the same operation value, and so do `task edit
// --severity` and `task sev`. A filter on either operation therefore finds every
// change to that field however it was made (SPEC/COMMANDS.md § Edit Task,
// acceptance criterion 5).
func TestTaskEdit_PriorityAndSeverityWriteTheSetterCommandsOperation(t *testing.T) {
	f := setupFieldEditRoadmap(t, "fieldeditsetters")
	before := len(auditRecordsFor(t, f.database, f.taskID))

	f.edit(t, "-p", "7")
	_ = captureStdout(t, func() {
		if err := taskSetPriority([]string{"-r", f.roadmap, itoa(f.taskID), "7"}); err != nil {
			t.Fatalf("task prio: %v", err)
		}
	})
	f.edit(t, "--severity", "5")
	_ = captureStdout(t, func() {
		if err := taskSetSeverity([]string{"-r", f.roadmap, itoa(f.taskID), "5"}); err != nil {
			t.Fatalf("task sev: %v", err)
		}
	})

	records := auditRecordsFor(t, f.database, f.taskID)
	added := records[before:]
	if len(added) != 4 {
		t.Fatalf("the four invocations wrote %d entries, want 4: %v", len(added), operationsOf(added))
	}

	// Pairwise, not against a constant: what matters is that the two commands
	// agree, and the values are checked against the constant as well so the
	// agreement is on the right value rather than on a shared mistake.
	if added[0].operation != added[1].operation {
		t.Errorf("`task edit -p 7` wrote %s and `task prio 7` wrote %s; the two perform the identical "+
			"mutation, so they write the identical operation", added[0].operation, added[1].operation)
	}
	if added[2].operation != added[3].operation {
		t.Errorf("`task edit --severity 5` wrote %s and `task sev 5` wrote %s; the two perform the "+
			"identical mutation, so they write the identical operation", added[2].operation, added[3].operation)
	}
	if added[0].operation != string(models.OpTaskPriorityChange) {
		t.Errorf("the priority entries carry %s, want %s", added[0].operation, models.OpTaskPriorityChange)
	}
	if added[2].operation != string(models.OpTaskSeverityChange) {
		t.Errorf("the severity entries carry %s, want %s", added[2].operation, models.OpTaskSeverityChange)
	}

	// And the filter that motivated the whole change returns both entries.
	if n := countRows(t, f.database, `SELECT COUNT(*) FROM audit WHERE operation = ?`,
		string(models.OpTaskPriorityChange)); n != 2 {
		t.Errorf("filtering on %s returns %d entries, want 2 — one from `task edit -p` and one from "+
			"`task prio`", models.OpTaskPriorityChange, n)
	}
}

// ---------------------------------------------------------------------------
// 3. The trigger is the flag, not a difference in value
// ---------------------------------------------------------------------------

// TestFieldEdit_ASuppliedFlagWritesItsEntryEvenWhenNothingChanges pins the rule
// SPEC/COMMANDS.md states explicitly and twice: the command compares no supplied
// value against the stored one, so re-supplying the value a field already holds
// still writes that field's entry.
//
// It is asserted rather than assumed because the opposite is the natural thing
// to implement — read the row, diff it, record what moved — and because the rule
// is what keeps `task edit -p` behaving like `task prio`, which has always
// written its entry unconditionally. The entry count is derivable from the
// command line alone, and that is the property under test.
func TestFieldEdit_ASuppliedFlagWritesItsEntryEvenWhenNothingChanges(t *testing.T) {
	f := setupFieldEditRoadmap(t, "fieldeditunchanged")

	t.Run("task edit", func(t *testing.T) {
		before := len(auditRecordsFor(t, f.database, f.taskID))

		// Every value here is the value the fixture seeded, so the UPDATE
		// changes nothing at all.
		f.edit(t,
			"-t", fieldEditTitle,
			"-p", itoa(fieldEditPriority),
			"--severity", itoa(fieldEditSeverity),
		)

		records := auditRecordsFor(t, f.database, f.taskID)
		assertFieldEntries(t, records, before, models.EntityTask, f.taskID, []models.AuditOperation{
			models.OpTaskPriorityChange,
			models.OpTaskSeverityChange,
			models.OpTaskTitleChange,
		})

		// The stored task is still what it was, which is what makes the three
		// entries above entries about a no-op rather than about a change.
		task, err := f.database.GetTask(context.Background(), f.taskID)
		if err != nil {
			t.Fatalf("reading the task back: %v", err)
		}
		if task.Title != fieldEditTitle || task.Priority != fieldEditPriority || task.Severity != fieldEditSeverity {
			t.Fatalf("the edit changed the task (%q, p%d, s%d); the test needs it unchanged",
				task.Title, task.Priority, task.Severity)
		}
	})

	t.Run("sprint update", func(t *testing.T) {
		before := len(auditRecordsForSprint(t, f.database, f.sprintID))

		f.update(t,
			"-t", fieldEditSprintTitle,
			"--max-tasks", itoa(fieldEditSprintMaxTasks),
		)

		records := auditRecordsForSprint(t, f.database, f.sprintID)
		assertFieldEntries(t, records, before, models.EntitySprint, f.sprintID, []models.AuditOperation{
			models.OpSprintTitleChange,
			models.OpSprintMaxTasksChange,
		})

		sprint, err := f.database.GetSprint(context.Background(), f.sprintID)
		if err != nil {
			t.Fatalf("reading the sprint back: %v", err)
		}
		if sprint.MaxTasks == nil {
			t.Fatalf("the sprint carries no max_tasks; the fixture seeded %d", fieldEditSprintMaxTasks)
		}
		if sprint.Title != fieldEditSprintTitle || *sprint.MaxTasks != fieldEditSprintMaxTasks {
			t.Fatalf("the update changed the sprint (%q, max %d); the test needs it unchanged",
				sprint.Title, *sprint.MaxTasks)
		}
	})
}

// ---------------------------------------------------------------------------
// 4. One timestamp per invocation, and nothing at all when the edit is rejected
// ---------------------------------------------------------------------------

// TestTaskEdit_EveryEntryOfOneInvocationSharesOnePerformedAt takes `task edit`
// to its full width — all seven flags in one invocation — and requires the seven
// entries to carry one timestamp.
//
// Seven rows are written well inside a single millisecond, so this assertion
// cannot by itself distinguish a shared timestamp from a re-stamped one; that
// distinction is drawn where it is observable, by
// TestLogAuditFieldsTx_OnePerformedAtForTheWholeInvocation in internal/db, which
// writes enough rows to span several milliseconds and proves the premise with a
// control batch. What this test adds is the other half: the seven entries really
// are one invocation's, and the command really does hand the writer a single
// timestamp rather than calling the clock per field.
func TestTaskEdit_EveryEntryOfOneInvocationSharesOnePerformedAt(t *testing.T) {
	f := setupFieldEditRoadmap(t, "fieldeditstamp")
	before := len(auditRecordsFor(t, f.database, f.taskID))

	f.edit(t,
		"-t", "Cache the roadmap statistics query behind an invalidating key",
		"-y", "IMPROVEMENT",
		"-fr", "The statistics page answers from cache for the whole window.",
		"-tr", "Invalidate on every mutating command rather than on a timer.",
		"-ac", "A second request inside the window issues no query at all.",
		"-p", "7",
		"--severity", "5",
	)

	records := auditRecordsFor(t, f.database, f.taskID)
	// Column order: acceptance_criteria, functional_requirements, priority,
	// severity, technical_requirements, title, type.
	assertFieldEntries(t, records, before, models.EntityTask, f.taskID, []models.AuditOperation{
		models.OpTaskAcceptanceCriteriaChange,
		models.OpTaskFunctionalRequirementsChange,
		models.OpTaskPriorityChange,
		models.OpTaskSeverityChange,
		models.OpTaskTechnicalRequirementsChange,
		models.OpTaskTitleChange,
		models.OpTaskTypeChange,
	})
}

// TestTaskEdit_ARejectedEditWritesNoEntry is acceptance criterion 4 of
// SPEC/COMMANDS.md § Edit Task. The rejections span both sides of the
// transaction boundary: the first four are raised before it opens, and the last
// is raised after the UPDATE has already run, which is the one that proves the
// entries are written inside the transaction rather than merely near it.
func TestTaskEdit_ARejectedEditWritesNoEntry(t *testing.T) {
	longTitle := make([]byte, models.MaxTaskTitle+1)
	for i := range longTitle {
		longTitle[i] = 'x'
	}

	cases := []struct {
		name  string
		id    string
		flags []string
	}{
		{"empty title", "", []string{"-t", "   "}},
		{"oversized title", "", []string{"-t", string(longTitle)}},
		{"priority out of range", "", []string{"-p", "12"}},
		{"unknown type", "", []string{"-y", "REFACTORING_OF_SORTS"}},
		{"unknown task", "99999", []string{"-t", "A title for a task that does not exist"}},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The roadmap name is indexed rather than derived from the case
			// name, which would collide between two cases of equal length.
			f := setupFieldEditRoadmap(t, "fieldeditreject"+itoa(i))
			before := readAuditTable(t, f.database)

			id := c.id
			if id == "" {
				id = itoa(f.taskID)
			}
			args := append([]string{"-r", f.roadmap, id}, c.flags...)

			var err error
			_ = captureStdout(t, func() { err = taskEdit(args) })
			if err == nil {
				t.Fatalf("task edit %v was accepted; the SPEC rejects it", c.flags)
			}

			after := readAuditTable(t, f.database)
			if len(after) != len(before) {
				t.Errorf("a rejected `task edit` wrote %d entries, want 0; the entries are written in "+
					"the same transaction as the UPDATE\nadded: %v",
					len(after)-len(before), operationsOf(after[len(before):]))
			}
		})
	}
}

// TestSprintUpdate_ARejectedUpdateWritesNoEntry is acceptance criterion 3 of
// SPEC/COMMANDS.md § Update Sprint, including the `--order` collision the
// criterion names explicitly. That case matters more than the others: the
// collision is raised by the UNIQUE index as the UPDATE executes, so it is the
// rejection that can only leave the audit table clean if the entries share the
// UPDATE's transaction.
func TestSprintUpdate_ARejectedUpdateWritesNoEntry(t *testing.T) {
	cases := []struct {
		name  string
		id    string
		flags func(f *fieldEditFixture) []string
	}{
		{"max-tasks below the range", "", func(*fieldEditFixture) []string { return []string{"--max-tasks", "0"} }},
		{"max-tasks above the range", "", func(*fieldEditFixture) []string { return []string{"--max-tasks", "10001"} }},
		{"order not positive", "", func(*fieldEditFixture) []string { return []string{"--order", "0"} }},
		{"order already in use", "", func(f *fieldEditFixture) []string { return []string{"--order", itoa(f.otherOrder)} }},
		{"unknown sprint", "99999", func(*fieldEditFixture) []string { return []string{"-t", "A title for a sprint that does not exist"} }},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := setupFieldEditRoadmap(t, "sprintupdreject"+itoa(i))
			before := readAuditTable(t, f.database)

			id := c.id
			if id == "" {
				id = itoa(f.sprintID)
			}
			args := append([]string{"-r", f.roadmap, id}, c.flags(f)...)

			var err error
			_ = captureStdout(t, func() { err = sprintUpdate(args) })
			if err == nil {
				t.Fatalf("sprint update %v was accepted; the SPEC rejects it", args)
			}

			after := readAuditTable(t, f.database)
			if len(after) != len(before) {
				t.Errorf("a rejected `sprint update` wrote %d entries, want 0; the entries are written "+
					"in the same transaction as the UPDATE\nadded: %v",
					len(after)-len(before), operationsOf(after[len(before):]))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 5. The two legacy values: never written, always readable
// ---------------------------------------------------------------------------

// TestFieldEdit_NeitherLegacyUpdateOperationIsEverWritten is acceptance
// criterion 4 of SPEC/DATABASE.md § One Row per Thing That Happened, restricted
// to the two operations this change retires, and its acceptance criterion 5 —
// the half that keeps the stored rows reachable.
//
// The count is taken over the whole audit table after a sequence that exercises
// every writer of a field edit, so a stray entry against any entity is caught
// wherever it was written from.
func TestFieldEdit_NeitherLegacyUpdateOperationIsEverWritten(t *testing.T) {
	f := setupFieldEditRoadmap(t, "fieldeditlegacy")

	f.edit(t, "-t", "Cache the roadmap statistics query behind an invalidating key")
	f.edit(t, "-y", "IMPROVEMENT", "-p", "7")
	f.edit(t, "-fr", "The statistics page answers from cache for the whole window.",
		"-tr", "Invalidate on every mutating command rather than on a timer.",
		"-ac", "A second request inside the window issues no query at all.",
		"--severity", "5")
	f.update(t, "-t", "Read-path performance and caching")
	f.update(t, "-d", "Cut the cost of every read command the CLI runs.",
		"--max-tasks", "12", "--order", "9")

	for _, legacy := range []models.AuditOperation{models.OpTaskUpdate, models.OpSprintUpdate} {
		if n := countRows(t, f.database,
			`SELECT COUNT(*) FROM audit WHERE operation = ?`, string(legacy)); n != 0 {
			t.Errorf("%d %s entries were written; the operation is LEGACY and no code path writes it "+
				"(SPEC/DATABASE.md § audit Table, Legacy)", n, legacy)
		}

		// Readable is the other half of LEGACY, and the reason the value is not
		// simply deleted: an `audit list --operation` filter naming it must be
		// accepted, so the rows a former binary wrote stay reachable by name.
		if !models.IsValidAuditOperation(string(legacy)) {
			t.Errorf("IsValidAuditOperation(%s) = false; a LEGACY operation is readable", legacy)
		}
		_ = captureStdout(t, func() {
			if err := HandleAudit([]string{"list", "-r", f.roadmap, "--operation", string(legacy)}); err != nil {
				t.Errorf("`audit list --operation %s` error = %v, want nil; the filter accepts a LEGACY "+
					"value and returns the rows carrying it", legacy, err)
			}
		})
	}

	// The sequence really did write entries, so the zero counts above are a
	// statement about the legacy values and not about an empty table.
	if n := len(readAuditTable(t, f.database)); n < 12 {
		t.Errorf("the sequence wrote %d audit entries; too few for the assertions above to have "+
			"measured anything", n)
	}
}
