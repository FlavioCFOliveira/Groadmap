package commands

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== HandleSprint Tests ====================

func TestHandleSprint_NoArgs(t *testing.T) {
	err := HandleSprint([]string{})
	if err != nil {
		t.Errorf("HandleSprint([]) error = %v, want nil", err)
	}
}

func TestHandleSprint_Help(t *testing.T) {
	helpFlags := []string{"-h", "--help", "help"}

	for _, flag := range helpFlags {
		t.Run("flag_"+flag, func(t *testing.T) {
			err := HandleSprint([]string{flag})
			if err != nil {
				t.Errorf("HandleSprint([%s]) error = %v, want nil", flag, err)
			}
		})
	}
}

func TestHandleSprint_UnknownSubcommand(t *testing.T) {
	err := HandleSprint([]string{"unknown"})
	if err == nil {
		t.Error("HandleSprint([unknown]) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown sprint subcommand") {
		t.Errorf("expected 'unknown sprint subcommand' error, got: %v", err)
	}
}

// ==================== sprintList Tests ====================

func TestSprintList_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"list"})
	if err == nil {
		t.Error("sprintList with no roadmap expected error, got nil")
	}
}

func TestSprintList_WithRoadmap(t *testing.T) {
	testName := "testsprintlist"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"list", "-r", testName})
	if err != nil {
		t.Errorf("sprintList error = %v", err)
	}
}

func TestSprintList_WithStatusFilter(t *testing.T) {
	testName := "testsprintliststatus"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"list", "-r", testName, "--status", "PENDING"})
	if err != nil {
		t.Errorf("sprintList with status filter error = %v", err)
	}
}

func TestSprintList_InvalidStatus(t *testing.T) {
	testName := "testsprintlistinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"list", "-r", testName, "--status", "INVALID"})
	if err == nil {
		t.Fatal("sprintList with invalid status expected error, got nil")
	}
	// Regression: a bad --status enum value must map to utils.ErrValidation
	// (exit 6), not leak ParseSprintStatus's model-level sentinel as a generic
	// runtime error (exit 1).
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("expected utils.ErrValidation (exit 6), got: %v", err)
	}
}

// TestSprintTasks_InvalidStatus is a regression guard: the --status filter on
// `sprint tasks` is wired via the -s short flag (matching task list / backlog
// list), and a bad enum value must map to utils.ErrValidation (exit 6) rather
// than leak ParseTaskStatus's model sentinel as a generic runtime error (exit 1).
func TestSprintTasks_InvalidStatus(t *testing.T) {
	testName := "testsprinttasksinvalidstatus"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	ctx := context.Background()
	sprintID, err := database.CreateSprint(ctx, &models.Sprint{
		Title:       "Sprint 1",
		Description: "first",
		Status:      models.SprintPending,
	})
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}

	err = HandleSprint([]string{"tasks", "-r", testName, strconv.Itoa(sprintID), "-s", "INVALID"})
	if err == nil {
		t.Fatal("sprintTasks with invalid status expected error, got nil")
	}
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("expected utils.ErrValidation (exit 6), got: %v", err)
	}
}

// TestSprintBottom_SprintNotFound is a regression guard for the bug where
// `sprint bottom` skipped the sprint-existence check that `sprint top` and
// `sprint move-to` perform. For a non-existent sprint, GetSprintTasks returns
// an empty set and the membership check reported a misleading
// "task does not belong to sprint" (utils.ErrValidation, exit 6) instead of
// the correct "sprint not found" (utils.ErrNotFound, exit 4). bottom must now
// behave identically to top.
func TestSprintBottom_SprintNotFound(t *testing.T) {
	testName := "testsprintbottomnotfound"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"bottom", "-r", testName, "99999", "1"})
	if err == nil {
		t.Fatal("sprintBottom on a non-existent sprint expected error, got nil")
	}
	if !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("expected utils.ErrNotFound (exit 4) for a missing sprint, got: %v", err)
	}
}

// ==================== sprintCreate Tests ====================

func TestSprintCreate_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"create", "-d", "Test sprint"})
	if err == nil {
		t.Error("sprintCreate with no roadmap expected error, got nil")
	}
}

func TestSprintCreate_MissingDescription(t *testing.T) {
	testName := "testsprintcreatedesc"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Provide a title so the missing-description path is exercised (title is
	// validated before description).
	err := HandleSprint([]string{"create", "-r", testName, "-t", "Authentication hardening"})
	if err == nil {
		t.Error("sprintCreate without description expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required parameter missing: --description") {
		t.Errorf("expected 'required parameter missing: --description' error, got: %v", err)
	}
}

// TestSprintCreate_MissingTitle verifies that omitting --title is rejected with
// the canonical "required parameter missing: --title" error (title is the first
// required field validated).
func TestSprintCreate_MissingTitle(t *testing.T) {
	testName := "testsprintcreatetitle"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"create", "-r", testName, "-d", "Test sprint description"})
	if err == nil {
		t.Error("sprintCreate without title expected error, got nil")
	}
	if !strings.Contains(err.Error(), "required parameter missing: --title") {
		t.Errorf("expected 'required parameter missing: --title' error, got: %v", err)
	}
}

func TestSprintCreate_Success(t *testing.T) {
	testName := "testsprintcreatesuccess"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"create", "-r", testName, "-t", "Authentication hardening", "-d", "Test sprint description"})
	if err != nil {
		t.Errorf("sprintCreate error = %v", err)
	}
}

// ==================== sprintGet Tests ====================

func TestSprintGet_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"get", "1"})
	if err == nil {
		t.Error("sprintGet with no roadmap expected error, got nil")
	}
}

func TestSprintGet_NoID(t *testing.T) {
	testName := "testsprintgetnoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"get", "-r", testName})
	if err == nil {
		t.Error("sprintGet with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sprint ID required") {
		t.Errorf("expected 'sprint ID required' error, got: %v", err)
	}
}

func TestSprintGet_InvalidID(t *testing.T) {
	testName := "testsprintgetinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"get", "-r", testName, "notanumber"})
	if err == nil {
		t.Error("sprintGet with invalid ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid sprint ID") {
		t.Errorf("expected 'invalid sprint ID' error, got: %v", err)
	}
}

func TestSprintGet_NotFound(t *testing.T) {
	testName := "testsprintgetnotfound"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"get", "-r", testName, "999"})
	if err == nil {
		t.Error("sprintGet for non-existent sprint expected error, got nil")
	}
}

// ==================== sprintUpdate Tests ====================

func TestSprintUpdate_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"update", "1", "-d", "New description"})
	if err == nil {
		t.Error("sprintUpdate with no roadmap expected error, got nil")
	}
}

func TestSprintUpdate_NoID(t *testing.T) {
	testName := "testsprintupdatenoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"update", "-r", testName})
	if err == nil {
		t.Error("sprintUpdate with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sprint ID required") {
		t.Errorf("expected 'sprint ID required' error, got: %v", err)
	}
}

func TestSprintUpdate_InvalidID(t *testing.T) {
	testName := "testsprintupdateinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"update", "-r", testName, "notanumber"})
	if err == nil {
		t.Error("sprintUpdate with invalid ID expected error, got nil")
	}
}

func TestSprintUpdate_MissingDescription(t *testing.T) {
	testName := "testsprintupdatedesc"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"update", "-r", testName, "1"})
	if err == nil {
		t.Error("sprintUpdate without any flags expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--description") && !strings.Contains(err.Error(), "--max-tasks") {
		t.Errorf("expected error referencing --description or --max-tasks, got: %v", err)
	}
}

// ==================== Sprint title regression tests ====================

// TestSprintCreate_TitleTooLong verifies a title above the 255-char cap is
// rejected with utils.ErrFieldTooLarge.
func TestSprintCreate_TitleTooLong(t *testing.T) {
	testName := "testsprinttitletoolong"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	longTitle := strings.Repeat("a", models.MaxSprintTitle+1)
	err := HandleSprint([]string{"create", "-r", testName, "-t", longTitle, "-d", "Valid description"})
	if err == nil {
		t.Fatal("sprintCreate with over-long title expected error, got nil")
	}
	if !errors.Is(err, utils.ErrFieldTooLarge) {
		t.Errorf("expected ErrFieldTooLarge, got: %v", err)
	}
}

// TestSprintCreate_TitleControlChars verifies a title containing control
// characters is rejected by the Free-Text Control-Character Constraint.
func TestSprintCreate_TitleControlChars(t *testing.T) {
	testName := "testsprinttitlectrl"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"create", "-r", testName, "-t", "Bad\x07title", "-d", "Valid description"})
	if err == nil {
		t.Fatal("sprintCreate with control-char title expected error, got nil")
	}
}

// TestSprintCreate_TitleRoundTrip verifies the happy path: a created sprint's
// title round-trips through `sprint get`.
func TestSprintCreate_TitleRoundTrip(t *testing.T) {
	testName := "testsprinttitleroundtrip"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	const wantTitle = "Q3 performance push"
	createOut := captureOutput(t, func() {
		if err := HandleSprint([]string{"create", "-r", testName, "-t", wantTitle, "-d", "Tune hot paths"}); err != nil {
			t.Fatalf("sprintCreate error = %v", err)
		}
	})
	sprintID := int(parseJSONObject(t, createOut)["id"].(float64))

	getOut := captureOutput(t, func() {
		if err := HandleSprint([]string{"get", "-r", testName, strconv.Itoa(sprintID)}); err != nil {
			t.Fatalf("sprintGet error = %v", err)
		}
	})
	obj := parseJSONObject(t, getOut)
	if got, _ := obj["title"].(string); got != wantTitle {
		t.Errorf("sprint get title = %q, want %q", got, wantTitle)
	}
}

// TestSprintUpdate_TitleOnly verifies that --title alone is an accepted update,
// changes the persisted title, and writes a SPRINT_UPDATE audit row.
func TestSprintUpdate_TitleOnly(t *testing.T) {
	testName := "testsprintupdatetitle"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	createOut := captureOutput(t, func() {
		if err := HandleSprint([]string{"create", "-r", testName, "-t", "Initial title", "-d", "Initial description"}); err != nil {
			t.Fatalf("sprintCreate error = %v", err)
		}
	})
	sprintID := int(parseJSONObject(t, createOut)["id"].(float64))

	const newTitle = "Authentication hardening"
	if err := HandleSprint([]string{"update", "-r", testName, strconv.Itoa(sprintID), "-t", newTitle}); err != nil {
		t.Fatalf("sprintUpdate with only --title error = %v", err)
	}

	// The persisted title must reflect the update.
	sprint, err := database.GetSprint(context.Background(), sprintID)
	if err != nil {
		t.Fatalf("GetSprint error = %v", err)
	}
	if sprint.Title != newTitle {
		t.Errorf("updated title = %q, want %q", sprint.Title, newTitle)
	}

	// A SPRINT_UPDATE audit row must exist for this sprint.
	history, err := database.GetEntityHistory(context.Background(), string(models.EntitySprint), sprintID)
	if err != nil {
		t.Fatalf("GetEntityHistory error = %v", err)
	}
	var sawUpdate bool
	for i := range history {
		if history[i].Operation == string(models.OpSprintUpdate) {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Errorf("expected a %s audit entry for sprint %d, got %+v", models.OpSprintUpdate, sprintID, history)
	}
}

// ==================== sprintRemove Tests ====================

func TestSprintRemove_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"remove", "1"})
	if err == nil {
		t.Error("sprintRemove with no roadmap expected error, got nil")
	}
}

func TestSprintRemove_NoID(t *testing.T) {
	testName := "testsprintremovenoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"remove", "-r", testName})
	if err == nil {
		t.Error("sprintRemove with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sprint ID required") {
		t.Errorf("expected 'sprint ID required' error, got: %v", err)
	}
}

func TestSprintRemove_InvalidID(t *testing.T) {
	testName := "testsprintremoveinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"remove", "-r", testName, "notanumber"})
	if err == nil {
		t.Error("sprintRemove with invalid ID expected error, got nil")
	}
}

// ==================== sprintStart Tests ====================

func TestSprintStart_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"start", "1"})
	if err == nil {
		t.Error("sprintStart with no roadmap expected error, got nil")
	}
}

func TestSprintStart_NoID(t *testing.T) {
	testName := "testsprintstartnoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"start", "-r", testName})
	if err == nil {
		t.Error("sprintStart with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sprint ID required") {
		t.Errorf("expected 'sprint ID required' error, got: %v", err)
	}
}

func TestSprintStart_InvalidID(t *testing.T) {
	testName := "testsprintstartinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"start", "-r", testName, "notanumber"})
	if err == nil {
		t.Error("sprintStart with invalid ID expected error, got nil")
	}
}

// ==================== sprintClose Tests ====================

func TestSprintClose_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"close", "1"})
	if err == nil {
		t.Error("sprintClose with no roadmap expected error, got nil")
	}
}

func TestSprintClose_NoID(t *testing.T) {
	testName := "testsprintclosenoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"close", "-r", testName})
	if err == nil {
		t.Error("sprintClose with no ID expected error, got nil")
	}
}

// ==================== sprintReopen Tests ====================

func TestSprintReopen_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"reopen", "1"})
	if err == nil {
		t.Error("sprintReopen with no roadmap expected error, got nil")
	}
}

func TestSprintReopen_NoID(t *testing.T) {
	testName := "testsprintreopennoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"reopen", "-r", testName})
	if err == nil {
		t.Error("sprintReopen with no ID expected error, got nil")
	}
}

// ==================== sprintTasks Tests ====================

func TestSprintTasks_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"tasks", "1"})
	if err == nil {
		t.Error("sprintTasks with no roadmap expected error, got nil")
	}
}

func TestSprintTasks_NoID(t *testing.T) {
	testName := "testsprinttasksnoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"tasks", "-r", testName})
	if err == nil {
		t.Error("sprintTasks with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sprint ID required") {
		t.Errorf("expected 'sprint ID required' error, got: %v", err)
	}
}

func TestSprintTasks_InvalidID(t *testing.T) {
	testName := "testsprinttasksinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"tasks", "-r", testName, "notanumber"})
	if err == nil {
		t.Error("sprintTasks with invalid ID expected error, got nil")
	}
}

// ==================== sprintOpenTasks Tests ====================

func TestSprintOpenTasks_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"open-tasks", "1"})
	if err == nil {
		t.Error("sprintOpenTasks with no roadmap expected error, got nil")
	}
}

func TestSprintOpenTasks_NoID(t *testing.T) {
	testName := "testsprintopentasksnoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"open-tasks", "-r", testName})
	if err == nil {
		t.Error("sprintOpenTasks with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sprint ID required") {
		t.Errorf("expected 'sprint ID required' error, got: %v", err)
	}
}

func TestSprintOpenTasks_InvalidID(t *testing.T) {
	testName := "testsprintopentasksinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"open-tasks", "-r", testName, "notanumber"})
	if err == nil {
		t.Error("sprintOpenTasks with invalid ID expected error, got nil")
	}
}

func TestSprintOpenTasks_NonexistentSprint(t *testing.T) {
	testName := "testsprintopentasksmissing"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"open-tasks", "-r", testName, "999"})
	if err == nil {
		t.Error("sprintOpenTasks against missing sprint expected error, got nil")
	}
	if !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing sprint, got: %v", err)
	}
}

func TestSprintOpenTasks_EmptySprint(t *testing.T) {
	testName := "testsprintopentasksempty"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Create a sprint with no tasks; open-tasks must succeed and return an empty list.
	ctx, cancel := db.WithQuickTimeout()
	defer cancel()
	sprintID, err := database.CreateSprint(ctx, &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Sprint without tasks for open-tasks happy-path test",
		Description: "Sprint without tasks for open-tasks happy-path test",
		CreatedAt:   utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("seed sprint failed: %v", err)
	}

	if err := HandleSprint([]string{"open-tasks", "-r", testName, strconv.Itoa(sprintID)}); err != nil {
		t.Errorf("sprintOpenTasks on empty sprint should succeed, got: %v", err)
	}
}

func TestSprintOpenTasks_OrderByPriorityFlag(t *testing.T) {
	testName := "testsprintopentasksorder"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()
	sprintID, err := database.CreateSprint(ctx, &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Sprint for order-by-priority verification",
		Description: "Sprint for order-by-priority verification",
		CreatedAt:   utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("seed sprint failed: %v", err)
	}

	// --order-by-priority is a boolean flag and should be accepted without value.
	err = HandleSprint([]string{"open-tasks", "-r", testName, strconv.Itoa(sprintID), "--order-by-priority"})
	if err != nil {
		t.Errorf("sprintOpenTasks --order-by-priority should be accepted, got: %v", err)
	}
}

// ==================== sprintStats Tests ====================

func TestSprintStats_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"stats", "1"})
	if err == nil {
		t.Error("sprintStats with no roadmap expected error, got nil")
	}
}

func TestSprintStats_NoID(t *testing.T) {
	testName := "testsprintstatsnoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"stats", "-r", testName})
	if err == nil {
		t.Error("sprintStats with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sprint ID required") {
		t.Errorf("expected 'sprint ID required' error, got: %v", err)
	}
}

func TestSprintStats_InvalidID(t *testing.T) {
	testName := "testsprintstatsinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"stats", "-r", testName, "notanumber"})
	if err == nil {
		t.Error("sprintStats with invalid ID expected error, got nil")
	}
}

// ==================== sprintAddTasks Tests ====================

func TestSprintAddTasks_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"add-tasks", "1", "1,2,3"})
	if err == nil {
		t.Error("sprintAddTasks with no roadmap expected error, got nil")
	}
}

func TestSprintAddTasks_NoArgs(t *testing.T) {
	testName := "testsprintaddnoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"add-tasks", "-r", testName})
	if err == nil {
		t.Error("sprintAddTasks with no args expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sprint ID and task ID(s) required") {
		t.Errorf("expected 'sprint ID and task ID(s) required' error, got: %v", err)
	}
}

func TestSprintAddTasks_InvalidSprintID(t *testing.T) {
	testName := "testsprintaddinvalidsprint"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"add-tasks", "-r", testName, "notanumber", "1"})
	if err == nil {
		t.Error("sprintAddTasks with invalid sprint ID expected error, got nil")
	}
}

func TestSprintAddTasks_InvalidTaskID(t *testing.T) {
	testName := "testsprintaddinvalidtask"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"add-tasks", "-r", testName, "1", "notanumber"})
	if err == nil {
		t.Error("sprintAddTasks with invalid task ID expected error, got nil")
	}
}

// ==================== sprintRemoveTasks Tests ====================

func TestSprintRemoveTasks_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"remove-tasks", "1", "1,2,3"})
	if err == nil {
		t.Error("sprintRemoveTasks with no roadmap expected error, got nil")
	}
}

func TestSprintRemoveTasks_NoArgs(t *testing.T) {
	testName := "testsprintrmnoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"remove-tasks", "-r", testName})
	if err == nil {
		t.Error("sprintRemoveTasks with no args expected error, got nil")
	}
	if !strings.Contains(err.Error(), "sprint ID and task ID(s) required") {
		t.Errorf("expected 'sprint ID and task ID(s) required' error, got: %v", err)
	}
}

// ==================== sprintMoveTasks Tests ====================

func TestSprintMoveTasks_NoRoadmap(t *testing.T) {
	err := HandleSprint([]string{"move-tasks", "1", "2", "3,4"})
	if err == nil {
		t.Error("sprintMoveTasks with no roadmap expected error, got nil")
	}
}

func TestSprintMoveTasks_NoArgs(t *testing.T) {
	testName := "testsprintmvnoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"move-tasks", "-r", testName})
	if err == nil {
		t.Error("sprintMoveTasks with no args expected error, got nil")
	}
	if !strings.Contains(err.Error(), "from sprint ID, to sprint ID, and task ID(s) required") {
		t.Errorf("expected 'from sprint ID, to sprint ID, and task ID(s) required' error, got: %v", err)
	}
}

func TestSprintMoveTasks_InvalidFromID(t *testing.T) {
	testName := "testsprintmvinvalidfrom"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"move-tasks", "-r", testName, "notanumber", "2", "3"})
	if err == nil {
		t.Error("sprintMoveTasks with invalid from ID expected error, got nil")
	}
}

func TestSprintMoveTasks_InvalidToID(t *testing.T) {
	testName := "testsprintmvinvalidto"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"move-tasks", "-r", testName, "1", "notanumber", "3"})
	if err == nil {
		t.Error("sprintMoveTasks with invalid to ID expected error, got nil")
	}
}

func TestSprintMoveTasks_InvalidTaskID(t *testing.T) {
	testName := "testsprintmvinvalidtask"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"move-tasks", "-r", testName, "1", "2", "notanumber"})
	if err == nil {
		t.Error("sprintMoveTasks with invalid task ID expected error, got nil")
	}
}

// mkSprintTask creates a BACKLOG task and returns its ID (test helper).
func mkSprintTask(t *testing.T, database *db.DB, title string) int {
	t.Helper()
	id, err := database.CreateTask(context.Background(), &models.Task{
		Title: title, FunctionalRequirements: "f", TechnicalRequirements: "t",
		AcceptanceCriteria: "a", Type: models.TypeTask, Status: models.StatusBacklog,
		CreatedAt: utils.NowISO8601(),
	})
	if err != nil {
		t.Fatalf("creating task %q: %v", title, err)
	}
	return id
}

// TestSprintRemoveTasks_MembershipGuard is a regression gate for finding #40:
// `sprint remove-tasks` must only remove tasks that belong to the NAMED sprint.
// Naming the wrong sprint must fail with exit 6 and leave the task untouched in
// the sprint it actually belongs to (previously it deleted by task_id alone,
// corrupting the other sprint).
func TestSprintRemoveTasks_MembershipGuard(t *testing.T) {
	testName := "testsprintremovemembership"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()
	ctx := context.Background()

	s1, err := database.CreateSprint(ctx, &models.Sprint{Title: "Velocity baseline", Description: "Sprint one", Status: models.SprintPending})
	if err != nil {
		t.Fatalf("creating sprint 1: %v", err)
	}
	s2, err := database.CreateSprint(ctx, &models.Sprint{Title: "Hardening pass", Description: "Sprint two", Status: models.SprintPending})
	if err != nil {
		t.Fatalf("creating sprint 2: %v", err)
	}
	t1 := mkSprintTask(t, database, "Task in sprint one")
	if err := database.AddTasksToSprint(ctx, s1, []int{t1}); err != nil {
		t.Fatalf("adding task to sprint 1: %v", err)
	}

	// Removing t1 while naming s2 (where it is NOT a member) must fail-fast.
	err = HandleSprint([]string{"remove-tasks", "-r", testName, strconv.Itoa(s2), strconv.Itoa(t1)})
	if err == nil {
		t.Fatal("expected membership error removing task from the wrong sprint, got nil")
	}
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("expected utils.ErrValidation (exit 6), got: %v", err)
	}

	// t1 must still belong to s1 and still be in SPRINT status.
	members, err := database.GetSprintTasks(ctx, s1)
	if err != nil {
		t.Fatalf("reading sprint 1 members: %v", err)
	}
	if len(members) != 1 || members[0] != t1 {
		t.Errorf("t1 must still be in sprint 1; members=%v", members)
	}
}

// TestSprintRemoveTasks_ClearsLifecycleAndCompacts is a regression gate for
// findings #49 and #50: removing a task from a sprint must reset it to BACKLOG
// with ALL lifecycle dates/summary cleared (even when it had progressed to
// COMPLETED), and the remaining tasks' positions must stay contiguous (0..N-1).
func TestSprintRemoveTasks_ClearsLifecycleAndCompacts(t *testing.T) {
	testName := "testsprintremovelifecycle"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()
	ctx := context.Background()

	s1, err := database.CreateSprint(ctx, &models.Sprint{Title: "Velocity baseline", Description: "Sprint one", Status: models.SprintPending})
	if err != nil {
		t.Fatalf("creating sprint: %v", err)
	}
	t1 := mkSprintTask(t, database, "Task one")
	t2 := mkSprintTask(t, database, "Task two")
	t3 := mkSprintTask(t, database, "Task three")
	if err := database.AddTasksToSprint(ctx, s1, []int{t1, t2, t3}); err != nil {
		t.Fatalf("adding tasks: %v", err)
	}

	// Drive t2 all the way to COMPLETED so it has started_at/tested_at/closed_at set.
	for _, st := range []string{"DOING", "TESTING"} {
		if err := HandleTask([]string{"stat", "-r", testName, strconv.Itoa(t2), st}); err != nil {
			t.Fatalf("transition t2 -> %s: %v", st, err)
		}
	}
	if err := HandleTask([]string{"stat", "-r", testName, strconv.Itoa(t2), "COMPLETED", "--summary", "all done"}); err != nil {
		t.Fatalf("complete t2: %v", err)
	}

	// Remove the middle task t2 from the sprint.
	if err := HandleSprint([]string{"remove-tasks", "-r", testName, strconv.Itoa(s1), strconv.Itoa(t2)}); err != nil {
		t.Fatalf("remove-tasks: %v", err)
	}

	// #49: t2 must be BACKLOG with every lifecycle field cleared to NULL.
	var status string
	var started, tested, closed, summary sql.NullString
	if err := database.QueryRowContext(ctx,
		"SELECT status, started_at, tested_at, closed_at, completion_summary FROM tasks WHERE id = ?", t2,
	).Scan(&status, &started, &tested, &closed, &summary); err != nil {
		t.Fatalf("reading t2: %v", err)
	}
	if status != "BACKLOG" {
		t.Errorf("t2 status = %q, want BACKLOG", status)
	}
	if started.Valid || tested.Valid || closed.Valid || summary.Valid {
		t.Errorf("t2 lifecycle fields must be NULL after removal; got started=%v tested=%v closed=%v summary=%v",
			started, tested, closed, summary)
	}

	// #50: remaining positions must be contiguous 0..N-1.
	rows, err := database.QueryContext(ctx,
		"SELECT task_id, position FROM sprint_tasks WHERE sprint_id = ? ORDER BY position ASC", s1)
	if err != nil {
		t.Fatalf("reading positions: %v", err)
	}
	defer rows.Close()
	var positions []int
	var remaining []int
	for rows.Next() {
		var tid, pos int
		if err := rows.Scan(&tid, &pos); err != nil {
			t.Fatalf("scanning position: %v", err)
		}
		remaining = append(remaining, tid)
		positions = append(positions, pos)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining tasks, got %d (%v)", len(remaining), remaining)
	}
	for i, p := range positions {
		if p != i {
			t.Errorf("positions must be contiguous 0..N-1; got %v", positions)
			break
		}
	}
}

// TestSprintHelpMentionsTitle is a regression test guarding against the human
// help text drifting out of sync with the flag set. The sprint title flag is
// REQUIRED on `sprint create` and optional on `sprint update`, so both static
// help screens MUST advertise -t/--title (a prior version omitted it even
// though the flag was enforced and present in --ai-help).
func TestSprintHelpMentionsTitle(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"create", printSprintCreateHelp},
		{"update", printSprintUpdateHelp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, tc.fn)
			for _, want := range []string{"-t, --title", "--title <text>"} {
				if !strings.Contains(out, want) {
					t.Errorf("sprint %s help missing %q; got:\n%s", tc.name, want, out)
				}
			}
		})
	}
}
