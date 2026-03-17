package commands

import (
	"strings"
	"testing"
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

	err := HandleSprint([]string{"list"})
	if err != nil {
		t.Errorf("sprintList error = %v", err)
	}
}

func TestSprintList_WithStatusFilter(t *testing.T) {
	testName := "testsprintliststatus"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"list", "--status", "PENDING"})
	if err != nil {
		t.Errorf("sprintList with status filter error = %v", err)
	}
}

func TestSprintList_InvalidStatus(t *testing.T) {
	testName := "testsprintlistinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"list", "--status", "INVALID"})
	if err == nil {
		t.Error("sprintList with invalid status expected error, got nil")
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

	err := HandleSprint([]string{"create"})
	if err == nil {
		t.Error("sprintCreate without description expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required parameter: --description") {
		t.Errorf("expected 'missing required parameter: --description' error, got: %v", err)
	}
}

func TestSprintCreate_Success(t *testing.T) {
	testName := "testsprintcreatesuccess"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"create", "-d", "Test sprint description"})
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

	err := HandleSprint([]string{"get"})
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

	err := HandleSprint([]string{"get", "notanumber"})
	if err == nil {
		t.Error("sprintGet with invalid ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid sprint ID") {
		t.Errorf("expected 'invalid sprint ID' error, got: %v", err)
	}
}

func TestSprintGet_ZeroID(t *testing.T) {
	testName := "testsprintgetzeroid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"get", "0"})
	if err == nil {
		t.Error("sprintGet with ID 0 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must be positive") {
		t.Errorf("expected 'must be positive' error, got: %v", err)
	}
}

func TestSprintGet_NegativeID(t *testing.T) {
	testName := "testsprintgetnegativeid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"get", "-1"})
	if err == nil {
		t.Error("sprintGet with negative ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must be positive") {
		t.Errorf("expected 'must be positive' error, got: %v", err)
	}
}

func TestSprintGet_NotFound(t *testing.T) {
	testName := "testsprintgetnotfound"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"get", "999"})
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

	err := HandleSprint([]string{"update"})
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

	err := HandleSprint([]string{"update", "notanumber"})
	if err == nil {
		t.Error("sprintUpdate with invalid ID expected error, got nil")
	}
}

func TestSprintUpdate_MissingDescription(t *testing.T) {
	testName := "testsprintupdatedesc"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"update", "1"})
	if err == nil {
		t.Error("sprintUpdate without description expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required parameter: --description") {
		t.Errorf("expected 'missing required parameter: --description' error, got: %v", err)
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

	err := HandleSprint([]string{"remove"})
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

	err := HandleSprint([]string{"remove", "notanumber"})
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

	err := HandleSprint([]string{"start"})
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

	err := HandleSprint([]string{"start", "notanumber"})
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

	err := HandleSprint([]string{"close"})
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

	err := HandleSprint([]string{"reopen"})
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

	err := HandleSprint([]string{"tasks"})
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

	err := HandleSprint([]string{"tasks", "notanumber"})
	if err == nil {
		t.Error("sprintTasks with invalid ID expected error, got nil")
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

	err := HandleSprint([]string{"stats"})
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

	err := HandleSprint([]string{"stats", "notanumber"})
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

	err := HandleSprint([]string{"add-tasks"})
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

	err := HandleSprint([]string{"add-tasks", "notanumber", "1"})
	if err == nil {
		t.Error("sprintAddTasks with invalid sprint ID expected error, got nil")
	}
}

func TestSprintAddTasks_InvalidTaskID(t *testing.T) {
	testName := "testsprintaddinvalidtask"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"add-tasks", "1", "notanumber"})
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

	err := HandleSprint([]string{"remove-tasks"})
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

	err := HandleSprint([]string{"move-tasks"})
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

	err := HandleSprint([]string{"move-tasks", "notanumber", "2", "3"})
	if err == nil {
		t.Error("sprintMoveTasks with invalid from ID expected error, got nil")
	}
}

func TestSprintMoveTasks_InvalidToID(t *testing.T) {
	testName := "testsprintmvinvalidto"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"move-tasks", "1", "notanumber", "3"})
	if err == nil {
		t.Error("sprintMoveTasks with invalid to ID expected error, got nil")
	}
}

func TestSprintMoveTasks_InvalidTaskID(t *testing.T) {
	testName := "testsprintmvinvalidtask"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleSprint([]string{"move-tasks", "1", "2", "notanumber"})
	if err == nil {
		t.Error("sprintMoveTasks with invalid task ID expected error, got nil")
	}
}
