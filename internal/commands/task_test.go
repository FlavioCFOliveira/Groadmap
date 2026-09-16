package commands

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// setupTestTaskRoadmap creates a roadmap and returns a cleanup function
func setupTestTaskRoadmap(t *testing.T, name string) (*db.DB, func()) {
	t.Helper()

	// Clean up any existing
	cleanupTestRoadmap(t, name)

	// Create the roadmap
	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}

	cleanup := func() {
		database.Close()
		cleanupTestRoadmap(t, name)
	}

	return database, cleanup
}

// wantNoRoadmapMsg is the refusal SPEC/COMMANDS.md § Roadmap Selection (Always
// Required) publishes for every task subcommand invoked without -r, less the
// "Error: " prefix cmd/rmp writes in front of every error.
const wantNoRoadmapMsg = "no roadmap selected: use -r <name> or --roadmap <name>"

// assertPublishedRefusal requires err to be the refusal SPEC/COMMANDS.md
// publishes for the case: the sentinel that decides the exit code, the exit code
// cmd/rmp derives from it, and the whole message after the "Error: " prefix.
func assertPublishedRefusal(t *testing.T, label string, err, sentinel error, wantCode int, wantMsg string) {
	t.Helper()

	if err == nil {
		t.Fatalf("%s: expected the published refusal %q, got nil", label, wantMsg)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("%s: error %q does not chain %v", label, err, sentinel)
	}
	if got := exitCodeFor(err); got != wantCode {
		t.Errorf("%s: exit code = %d, want %d; error %q", label, got, wantCode, err)
	}
	if err.Error() != wantMsg {
		t.Errorf("%s:\n got: %q\nwant: %q", label, err.Error(), wantMsg)
	}
}

// ==================== HandleTask Tests ====================

func TestHandleTask_NoArgs(t *testing.T) {
	err := HandleTask([]string{})
	if err != nil {
		t.Errorf("HandleTask([]) error = %v, want nil", err)
	}
}

func TestHandleTask_Help(t *testing.T) {
	helpFlags := []string{"-h", "--help", "help"}

	for _, flag := range helpFlags {
		t.Run("flag_"+flag, func(t *testing.T) {
			err := HandleTask([]string{flag})
			if err != nil {
				t.Errorf("HandleTask([%s]) error = %v, want nil", flag, err)
			}
		})
	}
}

func TestHandleTask_UnknownSubcommand(t *testing.T) {
	err := HandleTask([]string{"unknown"})
	if err == nil {
		t.Error("HandleTask([unknown]) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown task subcommand") {
		t.Errorf("expected 'unknown task subcommand' error, got: %v", err)
	}
}

// ==================== taskList Tests ====================

func TestTaskList_NoRoadmap(t *testing.T) {
	// Remove current roadmap
	utils.EnsureDataDir()

	// Clear current
	requireRoadmap([]string{"-r", "nonexistent"})

	err := HandleTask([]string{"list"})
	assertPublishedRefusal(t, "task list without -r", err, utils.ErrNoRoadmap, 3, wantNoRoadmapMsg)
}

func TestTaskList_WithRoadmap(t *testing.T) {
	testName := "testtasklist"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Should not error
	err := HandleTask([]string{"list", "-r", testName})
	if err != nil {
		t.Errorf("taskList error = %v", err)
	}
}

func TestTaskList_WithFilters(t *testing.T) {
	testName := "testtasklistfilters"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Test with various filters
	testCases := [][]string{
		{"list", "-s", "BACKLOG"},
		{"list", "--status", "DOING"},
		{"list", "-p", "5"},
		{"list", "--priority", "3"},
		{"list", "--severity", "2"},
		{"list", "-l", "10"},
		{"list", "--limit", "5"},
	}

	for _, args := range testCases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			err := HandleTask(append(args, "-r", testName))
			if err != nil {
				t.Errorf("taskList(%v) error = %v", args, err)
			}
		})
	}
}

func TestTaskList_InvalidStatus(t *testing.T) {
	testName := "testtaskliststatus"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"list", "-r", testName, "-s", "INVALID"})
	if err == nil {
		t.Fatal("taskList with invalid status expected error, got nil")
	}
	// Regression: a bad --status enum value must map to utils.ErrValidation
	// (exit 6), not leak ParseTaskStatus's model-level sentinel as a generic
	// runtime error (exit 1). See SPEC/COMMANDS.md § Task List.
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("expected utils.ErrValidation (exit 6), got: %v", err)
	}
}

func TestTaskList_InvalidPriority(t *testing.T) {
	testName := "testtasklistprio"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"list", "-r", testName, "-p", "notanumber"})
	if err == nil {
		t.Error("taskList with invalid priority expected error, got nil")
	}
}

// ==================== taskCreate Tests ====================

func TestTaskCreate_NoRoadmap(t *testing.T) {
	// Clear current
	utils.EnsureDataDir()

	err := HandleTask([]string{"create", "-t", "test", "-fr", "functional", "-tr", "technical", "-ac", "criteria"})
	assertPublishedRefusal(t, "task create without -r", err, utils.ErrNoRoadmap, 3, wantNoRoadmapMsg)
}

func TestTaskCreate_MissingTitle(t *testing.T) {
	testName := "testtaskcreatedesc"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-r", testName, "-fr", "functional", "-tr", "technical", "-ac", "criteria"})
	if err == nil {
		t.Error("taskCreate without title expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required parameter missing: --title") {
		t.Errorf("expected 'required parameter missing: --title' error, got: %v", err)
	}
}

func TestTaskCreate_MissingFunctionalRequirements(t *testing.T) {
	testName := "testtaskcreateaction"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-r", testName, "-t", "title", "-tr", "technical", "-ac", "criteria"})
	if err == nil {
		t.Error("taskCreate without functional requirements expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required parameter missing: --functional-requirements") {
		t.Errorf("expected 'required parameter missing: --functional-requirements' error, got: %v", err)
	}
}

func TestTaskCreate_MissingTechnicalRequirements(t *testing.T) {
	testName := "testtaskcreateexpected"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-r", testName, "-t", "title", "-fr", "functional", "-ac", "criteria"})
	if err == nil {
		t.Error("taskCreate without technical requirements expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required parameter missing: --technical-requirements") {
		t.Errorf("expected 'required parameter missing: --technical-requirements' error, got: %v", err)
	}
}

func TestTaskCreate_MissingAcceptanceCriteria(t *testing.T) {
	testName := "testtaskcreateacceptance"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-r", testName, "-t", "title", "-fr", "functional", "-tr", "technical"})
	if err == nil {
		t.Error("taskCreate without acceptance criteria expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required parameter missing: --acceptance-criteria") {
		t.Errorf("expected 'required parameter missing: --acceptance-criteria' error, got: %v", err)
	}
}

func TestTaskCreate_InvalidPriority(t *testing.T) {
	testName := "testtaskcreateprio"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-r", testName, "-t", "title", "-fr", "functional", "-tr", "technical", "-ac", "criteria", "-p", "invalid"})
	if err == nil {
		t.Error("taskCreate with invalid priority expected error, got nil")
	}
}

func TestTaskCreate_InvalidSeverity(t *testing.T) {
	testName := "testtaskcreatesev"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-r", testName, "-t", "title", "-fr", "functional", "-tr", "technical", "-ac", "criteria", "--severity", "invalid"})
	if err == nil {
		t.Error("taskCreate with invalid severity expected error, got nil")
	}
}

func TestTaskCreate_Success(t *testing.T) {
	testName := "testtaskcreatesuccess"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{
		"create",
		"-r", testName,
		"-t", "Test task title",
		"-fr", "Functional requirements",
		"-tr", "Technical requirements",
		"-ac", "Acceptance criteria",
		"-p", "5",
		"--severity", "3",
	})
	if err != nil {
		t.Errorf("taskCreate error = %v", err)
	}
}

// TestTaskCreate_SpecialistsFlagRejected is the inverted successor of the former
// TestTaskCreate_WithSpecialists. The same invocation that used to succeed must
// now fail as an unknown flag, and it must fail through the flag parser rather
// than through a surviving handler that merely ignores the value: the task is
// NOT created, so the run cannot be mistaken for a tolerant success
// (SPEC/COMMANDS.md § Create Task).
func TestTaskCreate_SpecialistsFlagRejected(t *testing.T) {
	testName := "testtaskcreatespec"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{
		"create",
		"-r", testName,
		"-t", "Rotate the JWT signing key",
		"-fr", "Operators can rotate the signing key without downtime",
		"-tr", "Add a key-id header and accept the previous key during overlap",
		"-ac", "Tokens signed with the retired key stop verifying after the overlap",
		"-sp", "developer,tester",
	})
	if err == nil {
		t.Fatal("task create -sp: expected an unknown-flag error, got nil")
	}
	if !errors.Is(err, utils.ErrInvalidInput) {
		t.Errorf("task create -sp: error = %v, want utils.ErrInvalidInput (exit 2)", err)
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("task create -sp: message = %q, want it to name the unknown flag", err.Error())
	}

	// The rejection must happen before the INSERT: a handler that parsed the
	// flag away and created the task anyway would also return an error from a
	// later step, and this assertion is what separates the two.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	if qErr := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count); qErr != nil {
		t.Fatalf("counting tasks: %v", qErr)
	}
	if count != 0 {
		t.Errorf("task create -sp created %d task(s); the rejected invocation must write nothing", count)
	}
}

// ==================== taskGet Tests ====================

func TestTaskGet_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"get", "1"})
	assertPublishedRefusal(t, "task get without -r", err, utils.ErrNoRoadmap, 3, wantNoRoadmapMsg)
}

func TestTaskGet_NoID(t *testing.T) {
	testName := "testtaskgetnoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"get", "-r", testName})
	if err == nil {
		t.Error("taskGet with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task ID(s) required") {
		t.Errorf("expected 'task ID(s) required' error, got: %v", err)
	}
}

func TestTaskGet_InvalidID(t *testing.T) {
	testName := "testtaskgetinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"get", "-r", testName, "notanumber"})
	if err == nil {
		t.Error("taskGet with invalid ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid task ID") {
		t.Errorf("expected 'invalid task ID' error, got: %v", err)
	}
}

// TestTaskGet_OutOfRangeID covers both bounds of the ID RANGE rule on one
// command, and asserts the whole line rather than a fragment of it.
//
// The three cases used to be three tests asserting two different fragments,
// "must be positive" at the floor and "exceeds maximum" at the ceiling, because
// the validator produced two different sentences for one rule. It produces one
// now, and the parameters that differ between these rows are the offending value
// and nothing else (rmp task 330).
func TestTaskGet_OutOfRangeID(t *testing.T) {
	testName := "testtaskgetoutofrangeid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	for _, id := range []string{"0", "-1", "2147483648", "99999999999999999"} {
		t.Run(id, func(t *testing.T) {
			err := HandleTask([]string{"get", "-r", testName, id})
			if err == nil {
				t.Fatalf("taskGet with id %s expected an error, got nil", id)
			}
			want := "validation error: " + utils.IDRangeMessage(utils.FieldTaskID) + ", got " + id
			if err.Error() != want {
				t.Errorf("taskGet %s\n got: %q\nwant: %q", id, err.Error(), want)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("taskGet %s must chain ErrValidation (exit 6), got %v", id, err)
			}
		})
	}
}

func TestTaskGet_MultipleIDs(t *testing.T) {
	testName := "testtaskgetmulti"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Create some tasks first
	for i := 0; i < 3; i++ {
		createTaskViaCommand(t, testName,
			"Reconcile settlement window "+string(rune('1'+i)),
			"Every settlement line in the window must match a ledger entry.",
			"Match on the settlement reference and report the residual.",
			"The window reconciles with a zero residual.",
			"-p", "1", "--severity", "1")
	}

	// Get multiple tasks
	err := HandleTask([]string{"get", "-r", testName, "1,2,3"})
	if err != nil {
		t.Errorf("taskGet with multiple IDs error = %v", err)
	}
}

// TestTaskGet_FailFastUnknownID is a regression gate for finding #44: per
// SPEC/COMMANDS.md § Get Task(s) (fail-fast batch behavior), any unknown ID —
// including the all-invalid case (previously null/exit 0) and the mixed batch
// case (previously silently dropped) — must fail with utils.ErrNotFound
// (exit 4).
func TestTaskGet_FailFastUnknownID(t *testing.T) {
	testName := "testtaskgetfailfast"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	createTaskViaCommand(t, testName, "Existing task",
		"The batch read must distinguish a known id from an unknown one.",
		"Resolve every requested id before returning any of them.",
		"A batch naming one unknown id is refused whole.",
		"-p", "1", "--severity", "1")

	for _, tc := range []struct{ name, ids string }{
		{"all invalid", "999"},
		{"mixed valid and invalid", "1,999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := HandleTask([]string{"get", "-r", testName, tc.ids})
			if err == nil {
				t.Fatalf("%s: expected not-found error, got nil", tc.name)
			}
			if !errors.Is(err, utils.ErrNotFound) {
				t.Errorf("%s: expected utils.ErrNotFound (exit 4), got: %v", tc.name, err)
			}
		})
	}
}

// TestTaskMutate_FailFastUnknownID is a regression gate for finding #45:
// `task prio` / `task sev` must verify existence first. Nonexistent IDs must
// fail with utils.ErrNotFound (exit 4) and write NO audit rows, instead of
// succeeding (exit 0) and logging phantom audit entries.
func TestTaskMutate_FailFastUnknownID(t *testing.T) {
	testName := "testtaskmutatefailfast"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	createTaskViaCommand(t, testName, "Existing task",
		"A batch mutation must refuse an unknown id before writing anything.",
		"Resolve every requested id before the transaction opens.",
		"No audit row exists for an id the batch could not resolve.",
		"-p", "1", "--severity", "1")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"prio unknown id", []string{"prio", "-r", testName, "999", "5"}},
		{"sev unknown id", []string{"sev", "-r", testName, "999", "5"}},
		{"prio mixed batch", []string{"prio", "-r", testName, "1,999", "5"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := HandleTask(tc.args)
			if err == nil {
				t.Fatalf("%s: expected not-found error, got nil", tc.name)
			}
			if !errors.Is(err, utils.ErrNotFound) {
				t.Errorf("%s: expected utils.ErrNotFound (exit 4), got: %v", tc.name, err)
			}
		})
	}

	// No phantom audit rows for the nonexistent task #999 must remain.
	var phantom int
	if err := database.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM audit WHERE entity_id = 999").Scan(&phantom); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if phantom != 0 {
		t.Errorf("expected 0 audit rows for nonexistent task #999, got %d", phantom)
	}
}

// ==================== taskEdit Tests ====================

func TestTaskEdit_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"edit", "1"})
	assertPublishedRefusal(t, "task edit without -r", err, utils.ErrNoRoadmap, 3, wantNoRoadmapMsg)
}

func TestTaskEdit_NoID(t *testing.T) {
	testName := "testtaskeditnoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "-r", testName})
	if err == nil {
		t.Error("taskEdit with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task ID required") {
		t.Errorf("expected 'task ID required' error, got: %v", err)
	}
}

// TestTaskEdit_InvalidID asserts the row of SPEC/COMMANDS.md § Edit Task for an
// `<id>` that is not a positive integer.
func TestTaskEdit_InvalidID(t *testing.T) {
	testName := "testtaskeditinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "-r", testName, "notanumber"})
	assertPublishedRefusal(t, "task edit notanumber", err, utils.ErrInvalidInput, 2,
		`invalid input: invalid task ID: "notanumber" (must be a positive integer)`)
}

// TestTaskEdit_NoFields is a regression gate for finding #48: per
// SPEC/COMMANDS.md § Edit Task, an edit with no fields of a task the roadmap
// holds is a successful no-op (exit 0, no output, no audit entry), NOT a
// validation error. The task is seeded first because a no-field edit of a task
// the roadmap does not hold is refused with exit 4 (rmp task 492).
func TestTaskEdit_NoFields(t *testing.T) {
	testName := "testtaskeditnofields"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	_ = captureStdout(t, func() {
		if err := taskCreate([]string{
			"-r", testName,
			"-t", "Paginate the audit history endpoint",
			"-fr", "The history endpoint returns at most 100 entries per page.",
			"-tr", "Use keyset pagination on the audit id rather than OFFSET.",
			"-ac", "A roadmap holding 250 entries is read in exactly three pages.",
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

	if err := HandleTask([]string{"edit", "-r", testName, itoa(tasks[0].ID)}); err != nil {
		t.Errorf("taskEdit with no fields must be a no-op (exit 0), got error: %v", err)
	}
}

// TestTaskEdit_InvalidPriority asserts the row of SPEC/COMMANDS.md § Edit Task
// for a `priority` that is not an integer. The flag's value is refused while the
// flags are parsed, before the task is looked up, so the roadmap holds no task.
func TestTaskEdit_InvalidPriority(t *testing.T) {
	testName := "testtaskeditprio"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "-r", testName, "1", "-p", "invalid"})
	assertPublishedRefusal(t, "task edit -p invalid", err, utils.ErrInvalidInput, 2,
		`invalid input: invalid value for --priority: strconv.Atoi: parsing "invalid": invalid syntax`)
}

// TestTaskEdit_OutOfRangePriority is a regression gate for finding #46: an
// out-of-range (but numeric) priority/severity on `task edit` must fail with
// utils.ErrValidation (exit 6), not leak the raw SQLite CHECK error (exit 1).
func TestTaskEdit_OutOfRangePriority(t *testing.T) {
	testName := "testtaskeditpriorange"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"priority above max", []string{"edit", "-r", testName, "1", "-p", "99"}},
		{"severity above max", []string{"edit", "-r", testName, "1", "--severity", "50"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := HandleTask(tc.args)
			if err == nil {
				t.Fatalf("%s: expected validation error, got nil", tc.name)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("%s: expected utils.ErrValidation (exit 6), got: %v", tc.name, err)
			}
		})
	}
}

func TestTaskEdit_EmptyTitle(t *testing.T) {
	testName := "testtaskeditemptydesc"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "-r", testName, "1", "-t", ""})
	if err == nil {
		t.Error("taskEdit with empty title expected error, got nil")
	}
	if !strings.Contains(err.Error(), "title cannot be empty") {
		t.Errorf("expected 'title cannot be empty' error, got: %v", err)
	}
}

// The three tests below pinned the HYPHENATED flag spelling until rmp task 297.
// `task edit` used to build this one refusal from its own map of column name to
// FLAG name, so it answered `-fr ""` with "functional-requirements cannot be
// empty" while answering `-fr $'a\x1bb'` with "functional_requirements: control
// characters are not allowed" — one command, one field, two names. The refusal
// now names the field, because a value did reach the application and broke a
// rule about its content (SPEC/COMMANDS.md § Published Field Names in Validation
// Messages, acceptance criterion 4).
func TestTaskEdit_EmptyFunctionalRequirements(t *testing.T) {
	testName := "testtaskeditemptyaction"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "-r", testName, "1", "-fr", ""})
	if err == nil {
		t.Error("taskEdit with empty functional requirements expected error, got nil")
	}
	if !strings.Contains(err.Error(), "functional_requirements cannot be empty") {
		t.Errorf("expected 'functional_requirements cannot be empty' error, got: %v", err)
	}
}

func TestTaskEdit_EmptyTechnicalRequirements(t *testing.T) {
	testName := "testtaskeditemptyexpected"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "-r", testName, "1", "-tr", ""})
	if err == nil {
		t.Error("taskEdit with empty technical requirements expected error, got nil")
	}
	if !strings.Contains(err.Error(), "technical_requirements cannot be empty") {
		t.Errorf("expected 'technical_requirements cannot be empty' error, got: %v", err)
	}
}

func TestTaskEdit_EmptyAcceptanceCriteria(t *testing.T) {
	testName := "testtaskeditemptyacceptance"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "-r", testName, "1", "-ac", ""})
	if err == nil {
		t.Error("taskEdit with empty acceptance criteria expected error, got nil")
	}
	if !strings.Contains(err.Error(), "acceptance_criteria cannot be empty") {
		t.Errorf("expected 'acceptance_criteria cannot be empty' error, got: %v", err)
	}
}

// TestTaskEdit_SpecialistsFlagRejected is the inverted successor of the former
// TestTaskEdit_EmptySpecialistsAllowed. The empty value that `task edit` used to
// accept as a clear-the-field request is now rejected outright, as an unknown
// flag, and so is a populated one (SPEC/COMMANDS.md § Edit Task).
func TestTaskEdit_SpecialistsFlagRejected(t *testing.T) {
	testName := "testtaskeditemptyspec"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	for _, value := range []string{"", "developer,tester"} {
		err := HandleTask([]string{"edit", "-r", testName, "1", "-sp", value})
		if err == nil {
			t.Fatalf("task edit -sp %q: expected an unknown-flag error, got nil", value)
		}
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("task edit -sp %q: error = %v, want utils.ErrInvalidInput (exit 2)", value, err)
		}
		if !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("task edit -sp %q: message = %q, want it to name the unknown flag", value, err.Error())
		}
	}
}

// ==================== taskRemove Tests ====================

func TestTaskRemove_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"remove", "1"})
	assertPublishedRefusal(t, "task remove without -r", err, utils.ErrNoRoadmap, 3, wantNoRoadmapMsg)
}

func TestTaskRemove_NoID(t *testing.T) {
	testName := "testtaskremovenoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"remove", "-r", testName})
	if err == nil {
		t.Error("taskRemove with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task ID(s) required") {
		t.Errorf("expected 'task ID(s) required' error, got: %v", err)
	}
}

// TestTaskRemove_InvalidID asserts the row of SPEC/COMMANDS.md § Remove Task for
// an invalid ID format.
func TestTaskRemove_InvalidID(t *testing.T) {
	testName := "testtaskremoveinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"remove", "-r", testName, "notanumber"})
	assertPublishedRefusal(t, "task remove notanumber", err, utils.ErrInvalidInput, 2,
		`invalid input: invalid task ID: "notanumber" (must be a positive integer)`)
}

// ==================== taskSetStatus Tests ====================

func TestTaskSetStatus_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"stat", "1", "DOING"})
	assertPublishedRefusal(t, "task stat without -r", err, utils.ErrNoRoadmap, 3, wantNoRoadmapMsg)
}

func TestTaskSetStatus_NoArgs(t *testing.T) {
	testName := "testtaskstatnoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"stat", "-r", testName})
	if err == nil {
		t.Error("taskSetStatus with no args expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task ID(s) and status required") {
		t.Errorf("expected 'task ID(s) and status required' error, got: %v", err)
	}
}

// TestTaskSetStatus_InvalidStatus asserts the row of SPEC/COMMANDS.md § Change
// Status (stat) for a target state that is not a recognised status. The status
// is refused before any task is looked up, so the roadmap holds no task.
func TestTaskSetStatus_InvalidStatus(t *testing.T) {
	testName := "testtaskstatstatus"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"stat", "-r", testName, "1", "INVALID"})
	assertPublishedRefusal(t, "task stat 1 INVALID", err, utils.ErrValidation, 6,
		`validation error: invalid task status: "INVALID"`)
}

// TestTaskSetStatus_InvalidID asserts the row of SPEC/COMMANDS.md § Change Status
// (stat) for an ID that is not an integer.
func TestTaskSetStatus_InvalidID(t *testing.T) {
	testName := "testtaskstatid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"stat", "-r", testName, "notanumber", "DOING"})
	assertPublishedRefusal(t, "task stat notanumber DOING", err, utils.ErrInvalidInput, 2,
		`invalid input: invalid task ID: "notanumber" (must be a positive integer)`)
}

// ==================== taskSetPriority Tests ====================

func TestTaskSetPriority_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"prio", "1", "5"})
	assertPublishedRefusal(t, "task prio without -r", err, utils.ErrNoRoadmap, 3, wantNoRoadmapMsg)
}

func TestTaskSetPriority_NoArgs(t *testing.T) {
	testName := "testtaskpriornoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"prio", "-r", testName})
	if err == nil {
		t.Error("taskSetPriority with no args expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task ID(s) and priority required") {
		t.Errorf("expected 'task ID(s) and priority required' error, got: %v", err)
	}
}

func TestTaskSetPriority_InvalidPriority(t *testing.T) {
	testName := "testtaskprioinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"prio", "-r", testName, "1", "invalid"})
	if err == nil {
		t.Error("taskSetPriority with invalid priority expected error, got nil")
	}
}

func TestTaskSetPriority_OutOfRange(t *testing.T) {
	testName := "testtaskpriooutofrange"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"prio", "-r", testName, "1", "10"})
	if err == nil {
		t.Error("taskSetPriority with priority > 9 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "priority must be between 0 and 9, got 10") {
		t.Errorf("expected the range refusal, got: %v", err)
	}
}

// ==================== taskSetSeverity Tests ====================

func TestTaskSetSeverity_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"sev", "1", "5"})
	assertPublishedRefusal(t, "task sev without -r", err, utils.ErrNoRoadmap, 3, wantNoRoadmapMsg)
}

func TestTaskSetSeverity_NoArgs(t *testing.T) {
	testName := "testtasksevnoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"sev", "-r", testName})
	if err == nil {
		t.Error("taskSetSeverity with no args expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task ID(s) and severity required") {
		t.Errorf("expected 'task ID(s) and severity required' error, got: %v", err)
	}
}

func TestTaskSetSeverity_InvalidSeverity(t *testing.T) {
	testName := "testtasksevinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"sev", "-r", testName, "1", "invalid"})
	if err == nil {
		t.Error("taskSetSeverity with invalid severity expected error, got nil")
	}
}

func TestTaskSetSeverity_OutOfRange(t *testing.T) {
	testName := "testtasksevoutofrange"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"sev", "-r", testName, "1", "10"})
	if err == nil {
		t.Error("taskSetSeverity with severity > 9 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "severity must be between 0 and 9, got 10") {
		t.Errorf("expected the range refusal, got: %v", err)
	}
}
