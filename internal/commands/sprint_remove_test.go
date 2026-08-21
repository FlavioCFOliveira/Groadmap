// Package commands — tests for `sprint remove`, the destructive sprint
// operation.
//
// These tests exist where they do on purpose. The delete used to have two
// implementations: the transaction in sprintRemove that the binary runs, and an
// unreachable copy in internal/db that nothing but the db package's own tests
// ever called. The atomicity guarantee of SPEC/DATABASE.md § Transactional
// Atomicity Guarantees (finding #65) was asserted against the copy, so the
// shipped path was free to drift — and had already drifted: the copy never
// received the finding-#49 fix that clears the lifecycle timestamps and the
// completion summary when a member task falls back to BACKLOG. The copy is gone
// (task #176) and this file is where its guarantee is now pinned, against the
// implementation the user actually reaches.
//
// Every assertion reads the stored row back from the database rather than
// trusting the command's own output, and the four properties the sprint cascade
// publishes are each asserted separately:
//
//  1. member tasks revert to BACKLOG, with every lifecycle field cleared;
//  2. the sprint's own comments go with it, through the foreign key;
//  3. a member task's comments are untouched;
//  4. the audit log records SPRINT_DELETE once, and no per-comment delete.
package commands

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// removalFixture is a roadmap holding one sprint, two member tasks, a comment on
// the sprint and a comment on each task, with the first task walked as far as
// TESTING so its lifecycle timestamps are populated before the delete.
type removalFixture struct {
	roadmap  string
	database *db.DB
	sprintID int
	taskIDs  []int
}

// setupSprintRemovalRoadmap builds that fixture through the real commands, so
// the state the delete meets is state the CLI can actually produce.
func setupSprintRemovalRoadmap(t *testing.T, name string) *removalFixture {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	fixture := &removalFixture{roadmap: name, database: database}

	seedTasks := []struct{ title, functional, technical, acceptance string }{
		{
			"Harden the JWT expiry boundary",
			"A token whose exp equals the current second is refused.",
			"Compare with !time.Now().Before(exp) instead of time.Now().After(exp).",
			"A unit test covers the exact boundary second.",
		},
		{
			"Move session tokens to the encrypted store",
			"Session tokens are never written in clear text.",
			"Route every write through the compliance store.",
			"A migration moves the rows that already exist.",
		},
	}
	for _, s := range seedTasks {
		_ = captureStdout(t, func() {
			if err := taskCreate([]string{
				"-r", name, "-t", s.title, "-fr", s.functional, "-tr", s.technical, "-ac", s.acceptance,
			}); err != nil {
				t.Fatalf("seeding task %q: %v", s.title, err)
			}
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
		fixture.taskIDs = append(fixture.taskIDs, tasks[i].ID)
	}

	_ = captureStdout(t, func() {
		if err := sprintCreate([]string{
			"-r", name, "-t", "Authentication hardening",
			"-d", "Close the expiry defect and the session storage finding.",
		}); err != nil {
			t.Fatalf("seeding the sprint: %v", err)
		}
	})
	sprints, err := database.ListSprints(context.Background(), nil)
	if err != nil {
		t.Fatalf("reading the seeded sprint back: %v", err)
	}
	if len(sprints) != 1 {
		t.Fatalf("seeded 1 sprint, found %d", len(sprints))
	}
	fixture.sprintID = sprints[0].ID

	_ = captureStdout(t, func() {
		if err := sprintAddTasks([]string{
			"-r", name, itoa(fixture.sprintID),
			itoa(fixture.taskIDs[0]) + "," + itoa(fixture.taskIDs[1]),
		}); err != nil {
			t.Fatalf("adding the tasks to the sprint: %v", err)
		}
	})

	// The sprint is started and the first task walked to TESTING, so started_at
	// and tested_at are set when the delete runs. A reset that only rewrote
	// status would leave them behind, which is finding #49.
	_ = captureStdout(t, func() {
		if err := sprintStart([]string{"-r", name, itoa(fixture.sprintID)}); err != nil {
			t.Fatalf("starting the sprint: %v", err)
		}
	})
	// The entry into DOING carries the mandatory --commit-open
	// (SPEC/COMMANDS.md § Change Status (stat)); TESTING takes no commit flag.
	for _, step := range []struct {
		status string
		flags  []string
	}{
		{"DOING", []string{"--commit-open", "5f93b51"}},
		{"TESTING", nil},
	} {
		_ = captureStdout(t, func() {
			args := append([]string{"-r", name, itoa(fixture.taskIDs[0]), step.status}, step.flags...)
			if err := taskSetStatus(args); err != nil {
				t.Fatalf("moving task to %s: %v", step.status, err)
			}
		})
	}

	_ = captureStdout(t, func() {
		if err := sprintCommentAdd([]string{
			"-r", name, itoa(fixture.sprintID), "--type", "DECISION",
			"--body", "Ship the expiry fix before the storage migration; the audit needs it first.",
		}); err != nil {
			t.Fatalf("commenting on the sprint: %v", err)
		}
	})
	for i, body := range []string{
		"The boundary second was accepted because After excludes equality.",
		"The compliance store needs a migration window agreed with the mobile team.",
	} {
		_ = captureStdout(t, func() {
			if err := taskCommentAdd([]string{
				"-r", name, itoa(fixture.taskIDs[i]), "--type", "FINDING", "--body", body,
			}); err != nil {
				t.Fatalf("commenting on task %d: %v", fixture.taskIDs[i], err)
			}
		})
	}

	return fixture
}

// removeSprint runs the command under test and fails on any error.
func (f *removalFixture) removeSprint(t *testing.T) {
	t.Helper()

	out := captureStdout(t, func() {
		if err := sprintRemove([]string{"-r", f.roadmap, itoa(f.sprintID)}); err != nil {
			t.Fatalf("sprint remove: %v", err)
		}
	})
	if out != "" {
		t.Errorf("sprint remove printed %q; SPEC/COMMANDS.md gives it an empty success output", out)
	}
}

// countRows runs a scalar COUNT against the fixture's database. The query is a
// test-supplied literal; every value is bound.
func (f *removalFixture) countRows(t *testing.T, query string, args ...any) int {
	t.Helper()

	var n int
	if err := f.database.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("counting rows (%s): %v", query, err)
	}
	return n
}

// auditOperations returns every audit operation recorded against one entity.
func (f *removalFixture) auditOperations(t *testing.T, entityType models.EntityType, entityID int) []string {
	t.Helper()

	entries, err := f.database.GetEntityHistory(context.Background(), string(entityType), entityID)
	if err != nil {
		t.Fatalf("reading the audit history of %s %d: %v", entityType, entityID, err)
	}
	ops := make([]string, 0, len(entries))
	for i := range entries {
		ops = append(ops, entries[i].Operation)
	}
	return ops
}

// count returns how many times op appears in ops.
func count(ops []string, op models.AuditOperation) int {
	n := 0
	for _, got := range ops {
		if got == string(op) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// The four published properties of the cascade
// ---------------------------------------------------------------------------

// TestSprintRemoveRevertsMemberTasksToBacklog covers property 1. It asserts the
// whole reset, not just the status: a task that reached TESTING inside the
// sprint comes back to BACKLOG with started_at, tested_at, closed_at and
// completion_summary all cleared, because SPEC/STATE_MACHINE.md § Reopening
// Behavior forbids a BACKLOG task carrying lifecycle timestamps (finding #49).
func TestSprintRemoveRevertsMemberTasksToBacklog(t *testing.T) {
	f := setupSprintRemovalRoadmap(t, "sprintremovebacklog")

	walked, err := f.database.GetTask(context.Background(), f.taskIDs[0])
	if err != nil {
		t.Fatalf("reading the walked task before the delete: %v", err)
	}
	if walked.Status != models.StatusTesting {
		t.Fatalf("fixture task status = %q, want TESTING", walked.Status)
	}
	if walked.StartedAt == nil || walked.TestedAt == nil {
		t.Fatalf("fixture task must carry started_at and tested_at before the delete: %+v", walked)
	}

	f.removeSprint(t)

	for _, id := range f.taskIDs {
		task, err := f.database.GetTask(context.Background(), id)
		if err != nil {
			t.Fatalf("reading task %d after the delete: %v", id, err)
		}
		if task.Status != models.StatusBacklog {
			t.Errorf("task %d status = %q, want BACKLOG", id, task.Status)
		}
		if task.StartedAt != nil {
			t.Errorf("task %d kept started_at = %q; a BACKLOG task carries none", id, *task.StartedAt)
		}
		if task.TestedAt != nil {
			t.Errorf("task %d kept tested_at = %q; a BACKLOG task carries none", id, *task.TestedAt)
		}
		if task.ClosedAt != nil {
			t.Errorf("task %d kept closed_at = %q; a BACKLOG task carries none", id, *task.ClosedAt)
		}
		if task.CompletionSummary != nil {
			t.Errorf("task %d kept completion_summary = %q; a BACKLOG task carries none", id, *task.CompletionSummary)
		}
	}

	// Neither task was deleted: the sprint goes, its members do not.
	if got := f.countRows(t, "SELECT COUNT(*) FROM tasks"); got != len(f.taskIDs) {
		t.Errorf("tasks remaining = %d, want %d; sprint remove deletes no task", got, len(f.taskIDs))
	}
}

// TestSprintRemoveDeletesItsOwnComments covers property 2: the sprint's comments
// go with it through sprint_comments.sprint_id ON DELETE CASCADE.
func TestSprintRemoveDeletesItsOwnComments(t *testing.T) {
	f := setupSprintRemovalRoadmap(t, "sprintremoveowncomments")

	before, err := f.database.ListSprintComments(context.Background(), f.sprintID, nil)
	if err != nil {
		t.Fatalf("listing the sprint's comments before the delete: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("fixture seeded %d sprint comments, want 1", len(before))
	}

	f.removeSprint(t)

	if got := f.countRows(t, "SELECT COUNT(*) FROM sprint_comments WHERE sprint_id = ?", f.sprintID); got != 0 {
		t.Errorf("the sprint kept %d comments after being deleted, want 0", got)
	}
	if _, err := f.database.GetSprintComment(context.Background(), before[0].ID); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("sprint comment %d outlived its sprint: %v", before[0].ID, err)
	}
}

// TestSprintRemoveLeavesMemberTaskCommentsUntouched covers property 3: a task's
// comments belong to the task, so losing the sprint costs the task nothing.
func TestSprintRemoveLeavesMemberTaskCommentsUntouched(t *testing.T) {
	f := setupSprintRemovalRoadmap(t, "sprintremovetaskcomments")

	before := make(map[int][]models.TaskComment, len(f.taskIDs))
	for _, id := range f.taskIDs {
		comments, err := f.database.ListTaskComments(context.Background(), id, nil)
		if err != nil {
			t.Fatalf("listing task %d comments before the delete: %v", id, err)
		}
		if len(comments) != 1 {
			t.Fatalf("fixture seeded %d comments on task %d, want 1", len(comments), id)
		}
		before[id] = comments
	}

	f.removeSprint(t)

	for _, id := range f.taskIDs {
		after, err := f.database.ListTaskComments(context.Background(), id, nil)
		if err != nil {
			t.Fatalf("listing task %d comments after the delete: %v", id, err)
		}
		if len(after) != len(before[id]) {
			t.Fatalf("task %d had %d comments, now has %d", id, len(before[id]), len(after))
		}
		for i := range after {
			// Byte-identical, not merely present: the cascade must not rewrite a
			// comment's body, type or timestamps either.
			if after[i] != before[id][i] {
				t.Errorf("task %d comment %d changed:\n  before: %+v\n  after:  %+v",
					id, after[i].ID, before[id][i], after[i])
			}
		}
	}
}

// TestSprintRemoveLogsOnlySprintDelete covers property 4: exactly one
// SPRINT_DELETE against the sprint, and no per-comment delete entry — the
// cascade is a database rule, not a comment operation.
func TestSprintRemoveLogsOnlySprintDelete(t *testing.T) {
	f := setupSprintRemovalRoadmap(t, "sprintremoveaudit")

	f.removeSprint(t)

	ops := f.auditOperations(t, models.EntitySprint, f.sprintID)
	if got := count(ops, models.OpSprintDelete); got != 1 {
		t.Errorf("SPRINT_DELETE recorded %d times, want exactly 1: %v", got, ops)
	}
	if got := count(ops, models.OpSprintCommentDelete); got != 0 {
		t.Errorf("the cascade wrote %d SPRINT_COMMENT_DELETE entries, want 0: %v", got, ops)
	}
	if got := count(ops, models.OpSprintCommentCreate); got != 1 {
		t.Errorf("the sprint's own SPRINT_COMMENT_CREATE entry is missing: %v", ops)
	}

	// The audit rows of the deleted sprint survive it: the audit log is a
	// record, not a projection of the current rows.
	if len(ops) == 0 {
		t.Error("the deleted sprint lost its whole audit history")
	}

	// And no task-side delete was invented for the members.
	for _, id := range f.taskIDs {
		taskOps := f.auditOperations(t, models.EntityTask, id)
		if got := count(taskOps, models.OpTaskDelete); got != 0 {
			t.Errorf("task %d was logged as deleted %d times: %v", id, got, taskOps)
		}
		if got := count(taskOps, models.OpTaskCommentDelete); got != 0 {
			t.Errorf("task %d lost comments to the sprint cascade: %v", id, taskOps)
		}
	}
}

// ---------------------------------------------------------------------------
// Atomicity and membership consistency (finding #65)
// ---------------------------------------------------------------------------

// TestSprintRemoveIsAtomic is the finding-#65 regression test, re-pointed at the
// implementation the binary runs. After the delete, no committed state may show
// a task still marked SPRINT while its sprint or its sprint_tasks row is gone.
func TestSprintRemoveIsAtomic(t *testing.T) {
	f := setupSprintRemovalRoadmap(t, "sprintremoveatomic")

	f.removeSprint(t)

	if _, err := f.database.GetSprint(context.Background(), f.sprintID); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("the sprint survived its own delete: %v", err)
	}
	if got := f.countRows(t, "SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?", f.sprintID); got != 0 {
		t.Errorf("%d sprint_tasks rows outlived the sprint, want 0", got)
	}
	if got := f.countRows(t, "SELECT COUNT(*) FROM tasks WHERE status = ?", models.StatusSprint); got != 0 {
		t.Errorf("%d tasks are still marked SPRINT with no sprint to belong to, want 0", got)
	}

	// Membership and status agree: every task without a sprint_tasks row is
	// BACKLOG, which is the invariant the transaction exists to hold.
	orphaned := f.countRows(t, `SELECT COUNT(*) FROM tasks t
		WHERE NOT EXISTS (SELECT 1 FROM sprint_tasks st WHERE st.task_id = t.id)
		  AND t.status <> ?`, models.StatusBacklog)
	if orphaned != 0 {
		t.Errorf("%d tasks have no sprint membership yet are not BACKLOG", orphaned)
	}
}

// TestSprintRemoveRollsBackWholesale proves the delete is one transaction and
// not a sequence that can half-commit. The sprints row is deleted inside a
// transaction that a concurrent-looking failure aborts: nothing at all may
// change — the sprint stays, its members keep their SPRINT status, and no
// SPRINT_DELETE is recorded.
//
// The failure is injected by running the same three statements the handler runs
// and then returning an error, which is the only way to observe the rollback
// without reaching into the handler.
func TestSprintRemoveRollsBackWholesale(t *testing.T) {
	f := setupSprintRemovalRoadmap(t, "sprintremoverollback")

	injected := errors.New("injected failure after the delete statements")
	err := f.database.WithTransaction(func(tx *sql.Tx) error {
		if _, execErr := tx.Exec(
			`UPDATE tasks SET status = 'BACKLOG', started_at = NULL, tested_at = NULL,
			        closed_at = NULL, completion_summary = NULL WHERE id IN (
				SELECT task_id FROM sprint_tasks WHERE sprint_id = ?
			)`, f.sprintID); execErr != nil {
			return execErr
		}
		if _, execErr := tx.Exec("DELETE FROM sprint_tasks WHERE sprint_id = ?", f.sprintID); execErr != nil {
			return execErr
		}
		if _, execErr := tx.Exec("DELETE FROM sprints WHERE id = ?", f.sprintID); execErr != nil {
			return execErr
		}
		if logErr := db.LogAuditTx(tx, models.OpSprintDelete, models.EntitySprint,
			f.sprintID, utils.NowISO8601()); logErr != nil {
			return logErr
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("expected the injected failure to surface, got %v", err)
	}

	if _, err := f.database.GetSprint(context.Background(), f.sprintID); err != nil {
		t.Errorf("the rolled-back delete removed the sprint anyway: %v", err)
	}
	if got := f.countRows(t, "SELECT COUNT(*) FROM sprint_tasks WHERE sprint_id = ?", f.sprintID); got != len(f.taskIDs) {
		t.Errorf("sprint_tasks rows after rollback = %d, want %d", got, len(f.taskIDs))
	}
	if got := count(f.auditOperations(t, models.EntitySprint, f.sprintID), models.OpSprintDelete); got != 0 {
		t.Errorf("a rolled-back delete left %d SPRINT_DELETE audit rows behind, want 0", got)
	}

	walked, err := f.database.GetTask(context.Background(), f.taskIDs[0])
	if err != nil {
		t.Fatalf("reading the walked task after the rollback: %v", err)
	}
	if walked.Status != models.StatusTesting {
		t.Errorf("the rolled-back reset changed task status to %q, want TESTING", walked.Status)
	}
	if walked.StartedAt == nil || walked.TestedAt == nil {
		t.Errorf("the rolled-back reset cleared the lifecycle timestamps: %+v", walked)
	}
}

// ---------------------------------------------------------------------------
// Refusals
// ---------------------------------------------------------------------------

// TestSprintRemoveUnknownSprint pins the refusal a user meets for an id that
// does not exist, message included: SPEC/COMMANDS.md § Remove Sprint publishes
// "resource not found: sprint <id> not found" at exit 4, and the registry's own
// example repeats it.
func TestSprintRemoveUnknownSprint(t *testing.T) {
	f := setupSprintRemovalRoadmap(t, "sprintremoveunknown")

	err := sprintRemove([]string{"-r", f.roadmap, "99999"})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an absent sprint, got %v", err)
	}
	if got, want := err.Error(), "resource not found: sprint 99999 not found"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}

	// The refusal changed nothing.
	if got := f.countRows(t, "SELECT COUNT(*) FROM sprints"); got != 1 {
		t.Errorf("sprints after a failed remove = %d, want 1", got)
	}
	if got := count(f.auditOperations(t, models.EntitySprint, 99999), models.OpSprintDelete); got != 0 {
		t.Errorf("a refused delete wrote %d audit rows, want 0", got)
	}
}

// TestSprintRemoveIsNotIdempotent pins the second call: the sprint is already
// gone, so the same not-found refusal applies. The registry declares this
// command Idempotent: false, and this is what that means to a user.
func TestSprintRemoveIsNotIdempotent(t *testing.T) {
	f := setupSprintRemovalRoadmap(t, "sprintremovetwice")

	f.removeSprint(t)

	err := sprintRemove([]string{"-r", f.roadmap, itoa(f.sprintID)})
	if !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("second remove: expected ErrNotFound, got %v", err)
	}

	// Exactly one SPRINT_DELETE, however many times the command is called.
	if got := count(f.auditOperations(t, models.EntitySprint, f.sprintID), models.OpSprintDelete); got != 1 {
		t.Errorf("SPRINT_DELETE recorded %d times across two calls, want 1", got)
	}
}
