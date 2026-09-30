package commands

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The command-level gates of rmp tasks 575 and 490: the sprint membership
// invariant (SPEC/STATE_MACHINE.md § Sprint Membership and the BACKLOG Status)
// as the eight write paths keep it, and the capacity check that counts the
// tasks an addition makes active (SPEC/COMMANDS.md § Task Assignment).
//
// Every refusal is asserted with its published line and exit code, and with a
// snapshot of the rows it must not change: a refusal that wrote first and then
// failed would pass an assertion on the error alone.

// invariantFixture is a roadmap with two sprints, driven through the real
// commands.
type invariantFixture struct {
	database *db.DB
	roadmap  string
	sprintA  int
	sprintB  int
}

func setupInvariantFixture(t *testing.T, roadmap string) *invariantFixture {
	t.Helper()
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	t.Cleanup(cleanup)
	f := &invariantFixture{database: database, roadmap: roadmap}
	f.sprintA = createSprintViaCommand(t, roadmap, "Card tokenisation", "Tokenise every stored card number.")
	f.sprintB = createSprintViaCommand(t, roadmap, "Chargeback handling", "Answer every chargeback within the scheme deadline.")
	run(t, func() error { return sprintStart([]string{"-r", roadmap, itoa(f.sprintA)}) })
	return f
}

// task creates a task and drives it into status inside sprintID.
func (f *invariantFixture) task(t *testing.T, title string, sprintID int, status models.TaskStatus) int {
	t.Helper()
	id := mkSprintTask(t, f.roadmap, title)
	driveTaskToStatus(t, f.roadmap, sprintID, id, status)
	return id
}

// taskRow is the part of a task the invariant is about.
type taskRow struct {
	status      string
	sprint      sql.NullInt64
	position    sql.NullInt64
	startedAt   sql.NullString
	commitOpen  sql.NullString
	commitClose sql.NullString
}

func (f *invariantFixture) row(t *testing.T, id int) taskRow {
	t.Helper()
	var r taskRow
	if err := f.database.QueryRow(
		`SELECT t.status, st.sprint_id, st.position, t.started_at, t.commit_open, t.commit_close
		   FROM tasks t LEFT JOIN sprint_tasks st ON st.task_id = t.id WHERE t.id = ?`, id,
	).Scan(&r.status, &r.sprint, &r.position, &r.startedAt, &r.commitOpen, &r.commitClose); err != nil {
		t.Fatalf("reading task %d: %v", id, err)
	}
	return r
}

// state is the whole observable state the refusals below must leave alone: every
// task with its membership, and the audit row count.
func (f *invariantFixture) state(t *testing.T) string {
	t.Helper()
	rows, err := f.database.Query(
		`SELECT t.id, t.status, IFNULL(st.sprint_id, 0), IFNULL(st.position, -1),
		        IFNULL(t.started_at, ''), IFNULL(t.closed_at, ''), IFNULL(t.commit_close, '')
		   FROM tasks t LEFT JOIN sprint_tasks st ON st.task_id = t.id ORDER BY t.id`)
	if err != nil {
		t.Fatalf("reading the tasks: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var id, sprint, position int
		var status, started, closed, commitClose string
		if err := rows.Scan(&id, &status, &sprint, &position, &started, &closed, &commitClose); err != nil {
			t.Fatalf("scanning a task: %v", err)
		}
		fmt.Fprintf(&b, "%d %s %d %d %s %s %s\n", id, status, sprint, position, started, closed, commitClose)
	}
	var sprints, audits int
	_ = f.database.QueryRow(`SELECT COUNT(*) FROM sprints`).Scan(&sprints)
	_ = f.database.QueryRow(`SELECT COUNT(*) FROM audit`).Scan(&audits)
	fmt.Fprintf(&b, "sprints=%d audit=%d\n", sprints, audits)
	return b.String()
}

// refuse runs a command expected to be refused with line (without "Error: ")
// and exit code, and requires the roadmap to be exactly as it was.
func (f *invariantFixture) refuse(t *testing.T, family string, args []string, sentinel error, code int, line string) {
	t.Helper()
	before := f.state(t)
	out, err := dispatchInvocation(t, family, args...)
	assertPublishedRefusal(t, family+" "+strings.Join(args, " "), err, sentinel, code, line)
	if out != "" {
		t.Errorf("a refused invocation wrote to stdout: %q", out)
	}
	if after := f.state(t); after != before {
		t.Errorf("the refused invocation changed the roadmap:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestTaskStat_BacklogTargetIsRefusedForEveryMember pins the BACKLOG target rule
// of SPEC/STATE_MACHINE.md § Valid Transitions: refused for every sprint member,
// with the line that names the command fitting the task's status, naming the
// first refused task in the order the command line supplied them.
func TestTaskStat_BacklogTargetIsRefusedForEveryMember(t *testing.T) {
	f := setupInvariantFixture(t, "invariant-stat-backlog")
	sprint := f.task(t, "Tokenise the card vault export", f.sprintA, models.StatusSprint)
	doing := f.task(t, "Tokenise the refund ledger", f.sprintA, models.StatusDoing)
	testing := f.task(t, "Tokenise the dispute evidence", f.sprintA, models.StatusTesting)
	completed := f.task(t, "Tokenise the settlement archive", f.sprintA, models.StatusCompleted)

	leaves := "a task leaves its sprint only through 'rmp sprint remove-tasks'"
	for _, tc := range []struct {
		id     int
		status string
		tail   string
	}{
		{sprint, "SPRINT", leaves},
		{doing, "DOING", leaves},
		{testing, "TESTING", leaves},
		{completed, "COMPLETED", "a completed task is reopened with 'rmp task reopen'"},
	} {
		f.refuse(t, "task", []string{"stat", "-r", f.roadmap, itoa(tc.id), "BACKLOG"}, utils.ErrValidation, 6,
			fmt.Sprintf("validation error: invalid status transition from %s to BACKLOG for task %d: %s",
				tc.status, tc.id, tc.tail))
	}

	// N is the first refused task in command-line order, not the lowest id.
	f.refuse(t, "task", []string{"stat", "-r", f.roadmap, csv(completed, doing), "BACKLOG"}, utils.ErrValidation, 6,
		fmt.Sprintf("validation error: invalid status transition from COMPLETED to BACKLOG for task %d: "+
			"a completed task is reopened with 'rmp task reopen'", completed))

	// The SPRINT target has its own line.
	f.refuse(t, "task", []string{"stat", "-r", f.roadmap, itoa(doing), "SPRINT"}, utils.ErrValidation, 6,
		"validation error: status SPRINT cannot be set by 'task stat'; it is set by 'sprint add-tasks' and 'task reopen'")
}

// TestTaskReopen_ReturnsTheTaskToSprintInItsSprint pins SPEC/COMMANDS.md
// § Reopen Task: DOING, TESTING and COMPLETED return to SPRINT, keeping the
// sprint and the position, with commit_open preserved and one TASK_REOPEN entry
// and no TASK_STATUS_* entry; SPRINT and BACKLOG are skipped with no entry.
func TestTaskReopen_ReturnsTheTaskToSprintInItsSprint(t *testing.T) {
	f := setupInvariantFixture(t, "invariant-reopen")
	doing := f.task(t, "Reject expired card tokens", f.sprintA, models.StatusDoing)
	testing := f.task(t, "Rotate the token encryption key", f.sprintA, models.StatusTesting)
	completed := f.task(t, "Purge plaintext card numbers", f.sprintA, models.StatusCompleted)
	sprint := f.task(t, "Document the token format", f.sprintA, models.StatusSprint)
	backlog := mkSprintTask(t, f.roadmap, "Evaluate a second token vault")

	before := map[int]taskRow{}
	for _, id := range []int{doing, testing, completed, sprint, backlog} {
		before[id] = f.row(t, id)
	}
	auditsBefore := map[int]int{}
	for _, id := range []int{doing, testing, completed, sprint, backlog} {
		auditsBefore[id] = len(auditRecordsFor(t, f.database, id))
	}

	run(t, func() error {
		return taskReopen([]string{"-r", f.roadmap, csv(doing, testing, completed, sprint, backlog)})
	})

	for _, id := range []int{doing, testing, completed} {
		after := f.row(t, id)
		if after.status != string(models.StatusSprint) {
			t.Errorf("task %d reopened to %s, want SPRINT", id, after.status)
		}
		if after.sprint != before[id].sprint || after.position != before[id].position {
			t.Errorf("task %d moved from sprint %v position %v to sprint %v position %v; a reopening keeps both",
				id, before[id].sprint, before[id].position, after.sprint, after.position)
		}
		if after.startedAt.Valid || after.commitClose.Valid {
			t.Errorf("task %d kept started_at %v / commit_close %v; a reopening clears them",
				id, after.startedAt, after.commitClose)
		}
		if after.commitOpen != before[id].commitOpen || !after.commitOpen.Valid {
			t.Errorf("task %d commit_open %v -> %v; a reopening preserves it", id, before[id].commitOpen, after.commitOpen)
		}
		records := auditRecordsFor(t, f.database, id)[auditsBefore[id]:]
		if len(records) != 1 || records[0].operation != string(models.OpTaskReopen) {
			t.Errorf("task %d: the reopening wrote %v, want exactly one TASK_REOPEN entry", id, operationsOf(records))
		}
	}
	for _, id := range []int{sprint, backlog} {
		if after := f.row(t, id); after != before[id] {
			t.Errorf("task %d, already at the start of the lifecycle, changed: %+v -> %+v", id, before[id], after)
		}
		if n := len(auditRecordsFor(t, f.database, id)); n != auditsBefore[id] {
			t.Errorf("task %d, skipped, gained %d audit entries", id, n-auditsBefore[id])
		}
	}
}

// TestTaskReopen_IsRefusedWhileTheSprintIsClosed pins the CLOSED refusal: no
// task of a batch is changed, N is the first such task in command-line order,
// and once the sprint is reopened the same invocation succeeds.
func TestTaskReopen_IsRefusedWhileTheSprintIsClosed(t *testing.T) {
	f := setupInvariantFixture(t, "invariant-reopen-closed")
	completed := f.task(t, "Retire the legacy token table", f.sprintA, models.StatusCompleted)
	doing := f.task(t, "Backfill tokens for archived cards", f.sprintA, models.StatusDoing)
	run(t, func() error { return sprintClose([]string{"-r", f.roadmap, itoa(f.sprintA), "--force"}) })

	f.refuse(t, "task", []string{"reopen", "-r", f.roadmap, csv(doing, completed)}, utils.ErrValidation, 6,
		fmt.Sprintf("validation error: cannot reopen task %d: sprint #%d is CLOSED; reopen the sprint first "+
			"with 'rmp sprint reopen'", doing, f.sprintA))

	run(t, func() error { return sprintReopen([]string{"-r", f.roadmap, itoa(f.sprintA)}) })
	run(t, func() error { return taskReopen([]string{"-r", f.roadmap, csv(doing, completed)}) })
	for _, id := range []int{doing, completed} {
		if got := f.row(t, id).status; got != string(models.StatusSprint) {
			t.Errorf("task %d reads %s after the sprint was reopened and the task reopened, want SPRINT", id, got)
		}
	}
}

// TestTaskReopen_ACompletedTaskOutsideEverySprintReturnsToBacklog pins the one
// case only data written before the invariant can reach: a COMPLETED task with
// no membership returns to BACKLOG, the one status a task outside every sprint
// may hold, with the same fields cleared.
func TestTaskReopen_ACompletedTaskOutsideEverySprintReturnsToBacklog(t *testing.T) {
	f := setupInvariantFixture(t, "invariant-reopen-orphan")
	id := f.task(t, "Close the 2025 card audit", f.sprintA, models.StatusCompleted)
	// Legacy state: the membership row is gone, the task stays COMPLETED.
	if _, err := f.database.Exec(`DELETE FROM sprint_tasks WHERE task_id = ?`, id); err != nil {
		t.Fatalf("removing the membership row: %v", err)
	}

	run(t, func() error { return taskReopen([]string{"-r", f.roadmap, itoa(id)}) })

	after := f.row(t, id)
	if after.status != string(models.StatusBacklog) || after.sprint.Valid || after.commitClose.Valid || after.startedAt.Valid {
		t.Errorf("the sprintless COMPLETED task reads %+v after reopen, want BACKLOG with no sprint and the "+
			"lifecycle fields cleared", after)
	}
}

// TestSprintAssignment_ACompletedTaskStaysInItsSprint pins rule 4 of the
// invariant on the three assignment commands, and the add-tasks line for a
// COMPLETED task that belongs to no sprint.
func TestSprintAssignment_ACompletedTaskStaysInItsSprint(t *testing.T) {
	f := setupInvariantFixture(t, "invariant-completed-bound")
	completed := f.task(t, "Certify the tokenisation service", f.sprintA, models.StatusCompleted)
	other := f.task(t, "Publish the token API reference", f.sprintA, models.StatusSprint)
	outsider := mkSprintTask(t, f.roadmap, "Plan the next certification")

	stays := fmt.Sprintf("validation error: task %d is COMPLETED in sprint #%d; a completed task stays in "+
		"the sprint it was completed in", completed, f.sprintA)
	for _, args := range [][]string{
		{"add-tasks", "-r", f.roadmap, itoa(f.sprintB), csv(outsider, completed)},
		{"add-tasks", "-r", f.roadmap, itoa(f.sprintA), itoa(completed)},
		{"move-tasks", "-r", f.roadmap, itoa(f.sprintA), itoa(f.sprintB), csv(other, completed)},
		{"remove-tasks", "-r", f.roadmap, itoa(f.sprintA), csv(other, completed)},
	} {
		f.refuse(t, "sprint", args, utils.ErrValidation, 6, stays)
	}

	// Legacy data: a COMPLETED task outside every sprint cannot join one.
	if _, err := f.database.Exec(`DELETE FROM sprint_tasks WHERE task_id = ?`, completed); err != nil {
		t.Fatalf("removing the membership row: %v", err)
	}
	f.refuse(t, "sprint", []string{"add-tasks", "-r", f.roadmap, itoa(f.sprintB), itoa(completed)},
		utils.ErrValidation, 6,
		fmt.Sprintf("validation error: task %d is COMPLETED and belongs to no sprint; a completed task "+
			"cannot join a sprint", completed))
}

// TestSprintAddTasks_CarriesUnfinishedWorkOverAndAuditsEachShape pins the
// carry-over workflow and the three audit shapes of `sprint add-tasks`: a
// BACKLOG task (SPRINT_ADD_TASK and TASK_STATUS_SPRINT), a task already in the
// sprint (SPRINT_ADD_TASK alone), and a DOING task taken from a CLOSED sprint
// (SPRINT_ADD_TASK, SPRINT_MOVE_TASK_OUT against the sprint it left and
// TASK_SPRINT_CHANGE naming the sprint it entered), all sharing one
// performed_at, the carried task keeping its status, started_at and commit_open.
func TestSprintAddTasks_CarriesUnfinishedWorkOverAndAuditsEachShape(t *testing.T) {
	f := setupInvariantFixture(t, "invariant-carry-over")
	carried := f.task(t, "Migrate stored cards to tokens", f.sprintA, models.StatusDoing)
	staying := f.task(t, "Verify token round-trips", f.sprintA, models.StatusSprint)
	run(t, func() error { return sprintClose([]string{"-r", f.roadmap, itoa(f.sprintA), "--force"}) })

	member := f.task(t, "Track chargeback deadlines", f.sprintB, models.StatusSprint)
	fresh := mkSprintTask(t, f.roadmap, "Answer chargebacks within five days")
	carriedBefore := f.row(t, carried)
	auditBefore := len(readAuditTable(t, f.database))

	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.sprintB), csv(fresh, member, carried)})
	})

	after := f.row(t, carried)
	if after.status != string(models.StatusDoing) || after.sprint.Int64 != int64(f.sprintB) ||
		after.startedAt != carriedBefore.startedAt || after.commitOpen != carriedBefore.commitOpen {
		t.Errorf("the carried task reads %+v, want DOING in sprint %d with started_at %v and commit_open %v kept",
			after, f.sprintB, carriedBefore.startedAt, carriedBefore.commitOpen)
	}
	if got := f.row(t, fresh).status; got != string(models.StatusSprint) {
		t.Errorf("the BACKLOG task joined as %s, want SPRINT", got)
	}
	if got := f.row(t, staying); got.position.Int64 != 0 {
		t.Errorf("the closed sprint's remaining member holds position %d, want 0: the sprint the task left is compacted",
			got.position.Int64)
	}

	written := readAuditTable(t, f.database)[auditBefore:]
	type key struct {
		op      string
		entity  string
		id      int
		related int64
	}
	got := make([]key, 0, len(written))
	stamps := map[string]bool{}
	for _, r := range written {
		got = append(got, key{r.operation, r.entityType, r.entityID, r.relatedEntityID.Int64})
		stamps[r.performedAt] = true
	}
	want := []key{
		{string(models.OpSprintAddTask), "SPRINT", f.sprintB, int64(fresh)},
		{string(models.OpTaskStatusSprint), "TASK", fresh, int64(f.sprintB)},
		{string(models.OpSprintAddTask), "SPRINT", f.sprintB, int64(member)},
		{string(models.OpSprintAddTask), "SPRINT", f.sprintB, int64(carried)},
		{string(models.OpSprintMoveTaskOut), "SPRINT", f.sprintA, int64(carried)},
		{string(models.OpTaskSprintChange), "TASK", carried, int64(f.sprintB)},
	}
	if !slices.Equal(got, want) {
		t.Errorf("the addition wrote\n  %v\nwant exactly\n  %v", got, want)
	}
	if len(stamps) != 1 {
		t.Errorf("the addition's entries carry %d distinct performed_at values, want 1", len(stamps))
	}
}

// TestSprintAddTasks_CapacityCountsTheTasksThatBecomeActive is the rmp task 490
// gate: the check counts the distinct named tasks that are not already members,
// against the active load, which excludes COMPLETED members; an addition that
// counts no task is never refused, even above the cap.
func TestSprintAddTasks_CapacityCountsTheTasksThatBecomeActive(t *testing.T) {
	f := setupInvariantFixture(t, "invariant-capacity")
	run(t, func() error { return sprintUpdate([]string{"-r", f.roadmap, itoa(f.sprintA), "--max-tasks", "5"}) })

	active := make([]int, 0, 4)
	for i := range 4 {
		active = append(active, f.task(t, fmt.Sprintf("Harden the payout step %d", i+1), f.sprintA, models.StatusSprint))
	}
	// A COMPLETED member does not count against the cap.
	f.task(t, "Ship the payout audit trail", f.sprintA, models.StatusCompleted)
	outsiderA := mkSprintTask(t, f.roadmap, "Alert on a stuck payout")
	outsiderB := mkSprintTask(t, f.roadmap, "Retry a failed payout once")
	outsiderC := mkSprintTask(t, f.roadmap, "Report payouts over the daily limit")

	// Two outsiders take the load of 4 past the cap of 5; N counts them, not the ids written.
	f.refuse(t, "sprint", []string{"add-tasks", "-r", f.roadmap, itoa(f.sprintA), csv(outsiderA, active[0], outsiderB)},
		utils.ErrValidation, 6,
		fmt.Sprintf("validation error: adding 2 task(s) would exceed sprint #%d capacity (4/5 tasks active)", f.sprintA))

	// Two members and one outsider count one task, which fits.
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.sprintA), csv(active[0], active[1], outsiderA)})
	})

	// The cap lowered below the load: naming only members counts nothing and is accepted.
	run(t, func() error { return sprintUpdate([]string{"-r", f.roadmap, itoa(f.sprintA), "--max-tasks", "2"}) })
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(f.sprintA), csv(active[2], active[3])})
	})
	f.refuse(t, "sprint", []string{"add-tasks", "-r", f.roadmap, itoa(f.sprintA), itoa(outsiderC)},
		utils.ErrValidation, 6,
		fmt.Sprintf("validation error: adding 1 task(s) would exceed sprint #%d capacity (5/2 tasks active)", f.sprintA))
}

// TestSprintRemove_IsRefusedWhileItHoldsACompletedTask pins SPEC/COMMANDS.md
// § Remove Sprint: the refusal names every COMPLETED member in ascending id
// order and changes nothing; a sprint with none is removed and its members
// return to BACKLOG outside every sprint.
func TestSprintRemove_IsRefusedWhileItHoldsACompletedTask(t *testing.T) {
	f := setupInvariantFixture(t, "invariant-sprint-remove")
	second := f.task(t, "Sign the tokenisation attestation", f.sprintA, models.StatusCompleted)
	first := f.task(t, "File the PCI evidence pack", f.sprintA, models.StatusCompleted)
	f.task(t, "Brief support on the token rollout", f.sprintA, models.StatusDoing)
	ids := []int{first, second}
	slices.Sort(ids)

	f.refuse(t, "sprint", []string{"remove", "-r", f.roadmap, itoa(f.sprintA)}, utils.ErrValidation, 6,
		fmt.Sprintf("validation error: cannot remove sprint #%d: completed tasks stay in their sprint: #%d, #%d",
			f.sprintA, ids[0], ids[1]))

	doing := f.task(t, "Dry-run the chargeback feed", f.sprintB, models.StatusDoing)
	run(t, func() error { return sprintRemove([]string{"-r", f.roadmap, itoa(f.sprintB)}) })
	if r := f.row(t, doing); r.status != string(models.StatusBacklog) || r.sprint.Valid {
		t.Errorf("the member of the removed sprint reads %+v, want BACKLOG outside every sprint", r)
	}
}
