package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The tests in this file pin SPEC/COMMANDS.md § Reopen Task, "A reopening
// respects the sprint's capacity": returning a COMPLETED member to SPRINT
// raises its sprint's active load, and a reopening that would take a sprint
// past its --max-tasks cap is refused with exit code 6, fail-fast and
// atomically, inside the transaction that would reopen the tasks.

// reopenFixtureTask creates one task and returns its id.
func reopenFixtureTask(t *testing.T, roadmap, title string) int {
	t.Helper()
	return createTaskViaCommand(t, roadmap, title,
		"Finance needs refunds to be safe to retry",
		"Extend the refunds service behind its repository interface",
		"Covered by an integration test against the provider sandbox")
}

// auditCount returns how many audit entries the roadmap holds.
func auditCount(t *testing.T, database *db.DB) int {
	t.Helper()
	n, err := database.CountAuditEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestTaskReopen_CapacityRefusal is acceptance criterion 4 of § Reopen Task:
// a sprint with max_tasks 2, two members in SPRINT and DOING, and one COMPLETED
// member. Reopening the COMPLETED one exits 6 with the capacity line and
// changes nothing; reopening a DOING one succeeds.
func TestTaskReopen_CapacityRefusal(t *testing.T) {
	const roadmap = "reopen-capacity"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	sprint := createSprintViaCommand(t, roadmap, "Refund safety", "Make refunds safe to retry", "--max-tasks", "2")
	done := reopenFixtureTask(t, roadmap, "Add idempotency keys to the refund endpoint")
	planned := reopenFixtureTask(t, roadmap, "Retry provider webhooks with exponential backoff")
	doing := reopenFixtureTask(t, roadmap, "Reconcile settlement totals against the daily report")
	driveTaskToStatus(t, roadmap, sprint, done, models.StatusCompleted)
	driveTaskToStatus(t, roadmap, sprint, planned, models.StatusSprint)
	driveTaskToStatus(t, roadmap, sprint, doing, models.StatusDoing)

	ctx := context.Background()
	before, err := database.GetTask(ctx, done)
	if err != nil {
		t.Fatal(err)
	}
	audits := auditCount(t, database)

	stdout, err := dispatchInvocation(t, "task", "reopen", "-r", roadmap, itoa(done))
	want := fmt.Sprintf("validation error: reopening 1 task(s) would exceed sprint #%d capacity (2/2 tasks active)", sprint)
	if err == nil || err.Error() != want || !errors.Is(err, utils.ErrValidation) {
		t.Fatalf("reopening past the cap: err %v, want %q (exit 6)", err, want)
	}
	if stdout != "" {
		t.Errorf("a refused reopen wrote to stdout: %q", stdout)
	}
	after, err := database.GetTask(ctx, done)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != models.StatusCompleted || !equalTaskFields(before, after) {
		t.Errorf("the refused reopen changed the task: before %+v, after %+v", before, after)
	}
	if got := auditCount(t, database); got != audits {
		t.Errorf("the refused reopen wrote %d audit entries", got-audits)
	}

	// The DOING member leaves the load unchanged, so it is never refused.
	if _, err := dispatchInvocation(t, "task", "reopen", "-r", roadmap, itoa(doing)); err != nil {
		t.Fatalf("reopening a DOING member of a full sprint: %v", err)
	}
	reopened, err := database.GetTask(ctx, doing)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != models.StatusSprint {
		t.Errorf("the DOING member is %s after reopen, want SPRINT", reopened.Status)
	}
}

// equalTaskFields compares every published field of two reads of one task.
func equalTaskFields(a, b *models.Task) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// TestTaskReopen_CapacityAcrossSprintsAndBatches pins the counting rule: N is
// the number of distinct named COMPLETED tasks in the refused sprint, a batch
// that fits is accepted whole, a batch that does not is refused whole, an
// uncapped sprint never refuses, and when two sprints would overflow the line
// names the sprint of the first such task in command-line order.
func TestTaskReopen_CapacityAcrossSprintsAndBatches(t *testing.T) {
	const roadmap = "reopen-capacity-batch"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	capped := createSprintViaCommand(t, roadmap, "Refund safety", "Make refunds safe to retry", "--max-tasks", "3")
	other := createSprintViaCommand(t, roadmap, "Dispute workflow", "Document and automate chargebacks", "--max-tasks", "1")
	open := createSprintViaCommand(t, roadmap, "Invoice performance", "Bring the renderer under budget")

	cappedDone := make([]int, 0, 3)
	otherDone := make([]int, 0, 2)
	openDone := make([]int, 0, 2)
	for _, title := range []string{"Persist idempotency keys", "Expire stale refund locks", "Alert on refund retries"} {
		id := reopenFixtureTask(t, roadmap, title)
		driveTaskToStatus(t, roadmap, capped, id, models.StatusCompleted)
		cappedDone = append(cappedDone, id)
	}
	for _, title := range []string{"Document the dispute workflow", "Export dispute evidence"} {
		id := reopenFixtureTask(t, roadmap, title)
		driveTaskToStatus(t, roadmap, other, id, models.StatusCompleted)
		otherDone = append(otherDone, id)
	}
	for _, title := range []string{"Profile the invoice renderer", "Cache invoice templates"} {
		id := reopenFixtureTask(t, roadmap, title)
		driveTaskToStatus(t, roadmap, open, id, models.StatusCompleted)
		openDone = append(openDone, id)
	}
	active := reopenFixtureTask(t, roadmap, "Retry provider webhooks")
	driveTaskToStatus(t, roadmap, capped, active, models.StatusSprint)

	// The two sprints both overflow; the first such task on the command line
	// belongs to `other`, so the line names `other`, with its own figures.
	list := fmt.Sprintf("%d,%d,%d,%d,%d", otherDone[0], cappedDone[0], otherDone[1], cappedDone[1], cappedDone[2])
	_, err := dispatchInvocation(t, "task", "reopen", "-r", roadmap, list)
	want := fmt.Sprintf("validation error: reopening 2 task(s) would exceed sprint #%d capacity (0/1 tasks active)", other)
	if err == nil || err.Error() != want {
		t.Fatalf("two overflowing sprints: %v, want %q", err, want)
	}
	// A repeated id is one task.
	list = fmt.Sprintf("%d,%d,%d,%d,%d", cappedDone[0], cappedDone[1], cappedDone[2], cappedDone[0], openDone[0])
	_, err = dispatchInvocation(t, "task", "reopen", "-r", roadmap, list)
	want = fmt.Sprintf("validation error: reopening 3 task(s) would exceed sprint #%d capacity (1/3 tasks active)", capped)
	if err == nil || err.Error() != want {
		t.Fatalf("a batch past the cap: %v, want %q", err, want)
	}
	// Nothing was reopened by either refusal, the uncapped member included.
	for _, id := range append(append(append([]int{}, cappedDone...), otherDone...), openDone...) {
		task, gerr := database.GetTask(context.Background(), id)
		if gerr != nil {
			t.Fatal(gerr)
		}
		if task.Status != models.StatusCompleted {
			t.Errorf("task %d is %s after a refused batch, want COMPLETED", id, task.Status)
		}
	}

	// A batch that fits exactly is accepted, and the uncapped sprint is
	// never refused.
	list = fmt.Sprintf("%d,%d,%d,%d", cappedDone[0], cappedDone[1], openDone[0], openDone[1])
	if _, err := dispatchInvocation(t, "task", "reopen", "-r", roadmap, list); err != nil {
		t.Fatalf("a batch that reaches the cap exactly: %v", err)
	}
	if n, err := database.CountActiveSprintTasks(context.Background(), capped); err != nil || n != 3 {
		t.Errorf("capped sprint load after the batch = %d (%v), want 3", n, err)
	}
}

// TestTaskReopen_ConcurrentReopeningsCannotTogetherExceedTheCap is the
// transactional half of the rule: two invocations that each fit alone, run at
// the same time against a sprint with room for one, end with exactly one
// reopened and the other refused with the line for the state the winner left.
func TestTaskReopen_ConcurrentReopeningsCannotTogetherExceedTheCap(t *testing.T) {
	for round := 0; round < 5; round++ {
		roadmap := fmt.Sprintf("reopen-race-%d", round)
		database, cleanup := setupTestTaskRoadmap(t, roadmap)

		sprint := createSprintViaCommand(t, roadmap, "Refund safety", "Make refunds safe to retry", "--max-tasks", "3")
		first := reopenFixtureTask(t, roadmap, "Persist idempotency keys")
		second := reopenFixtureTask(t, roadmap, "Expire stale refund locks")
		driveTaskToStatus(t, roadmap, sprint, first, models.StatusCompleted)
		driveTaskToStatus(t, roadmap, sprint, second, models.StatusCompleted)
		for _, title := range []string{"Retry provider webhooks", "Alert on refund retries"} {
			driveTaskToStatus(t, roadmap, sprint, reopenFixtureTask(t, roadmap, title), models.StatusSprint)
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		for i, id := range []int{first, second} {
			wg.Add(1)
			go func(i, id int) {
				defer wg.Done()
				<-start
				errs[i] = taskReopen([]string{"-r", roadmap, itoa(id)})
			}(i, id)
		}
		close(start)
		wg.Wait()

		ok, refused := 0, 0
		want := fmt.Sprintf("validation error: reopening 1 task(s) would exceed sprint #%d capacity (3/3 tasks active)", sprint)
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case err.Error() == want:
				refused++
			default:
				t.Errorf("round %d: unexpected outcome %v", round, err)
			}
		}
		if ok != 1 || refused != 1 {
			t.Errorf("round %d: %d reopened and %d refused, want exactly one of each", round, ok, refused)
		}
		if n, err := database.CountActiveSprintTasks(context.Background(), sprint); err != nil || n != 3 {
			t.Errorf("round %d: load %d (%v), want the cap 3", round, n, err)
		}
		cleanup()
	}
}

// TestTaskFamilyHelp_ReopenNamesSprint is HELP.md § 5's single destination: the
// `reopen` entry of the task family help names SPRINT, not BACKLOG.
func TestTaskFamilyHelp_ReopenNamesSprint(t *testing.T) {
	out, err := dispatchInvocation(t, "task", "--help")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "reopen <task-ids>") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("the task family help lists no reopen entry:\n%s", out)
	}
	if !strings.Contains(line, "to SPRINT") || strings.Contains(line, "BACKLOG") {
		t.Errorf("the reopen entry names the wrong destination: %q", line)
	}
}
