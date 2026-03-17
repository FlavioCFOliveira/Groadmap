package commands

import (
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
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

	// Set as current
	HandleRoadmap([]string{"use", name})

	cleanup := func() {
		database.Close()
		cleanupTestRoadmap(t, name)
	}

	return database, cleanup
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
	if err == nil {
		t.Error("taskList with no roadmap expected error, got nil")
	}
}

func TestTaskList_WithRoadmap(t *testing.T) {
	testName := "testtasklist"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Should not error
	err := HandleTask([]string{"list"})
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
			err := HandleTask(args)
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

	err := HandleTask([]string{"list", "-s", "INVALID"})
	if err == nil {
		t.Error("taskList with invalid status expected error, got nil")
	}
}

func TestTaskList_InvalidPriority(t *testing.T) {
	testName := "testtasklistprio"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"list", "-p", "notanumber"})
	if err == nil {
		t.Error("taskList with invalid priority expected error, got nil")
	}
}

// ==================== taskCreate Tests ====================

func TestTaskCreate_NoRoadmap(t *testing.T) {
	// Clear current
	utils.EnsureDataDir()

	err := HandleTask([]string{"create", "-d", "test", "-a", "action", "-e", "result"})
	if err == nil {
		t.Error("taskCreate with no roadmap expected error, got nil")
	}
}

func TestTaskCreate_MissingDescription(t *testing.T) {
	testName := "testtaskcreatedesc"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-a", "action", "-e", "result"})
	if err == nil {
		t.Error("taskCreate without description expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required parameter: --description") {
		t.Errorf("expected 'missing required parameter: --description' error, got: %v", err)
	}
}

func TestTaskCreate_MissingAction(t *testing.T) {
	testName := "testtaskcreateaction"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-d", "description", "-e", "result"})
	if err == nil {
		t.Error("taskCreate without action expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required parameter: --action") {
		t.Errorf("expected 'missing required parameter: --action' error, got: %v", err)
	}
}

func TestTaskCreate_MissingExpectedResult(t *testing.T) {
	testName := "testtaskcreateexpected"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-d", "description", "-a", "action"})
	if err == nil {
		t.Error("taskCreate without expected result expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required parameter: --expected-result") {
		t.Errorf("expected 'missing required parameter: --expected-result' error, got: %v", err)
	}
}

func TestTaskCreate_InvalidPriority(t *testing.T) {
	testName := "testtaskcreateprio"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-d", "desc", "-a", "action", "-e", "result", "-p", "invalid"})
	if err == nil {
		t.Error("taskCreate with invalid priority expected error, got nil")
	}
}

func TestTaskCreate_InvalidSeverity(t *testing.T) {
	testName := "testtaskcreatesev"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-d", "desc", "-a", "action", "-e", "result", "--severity", "invalid"})
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
		"-d", "Test task description",
		"-a", "Perform the test action",
		"-e", "Expected result achieved",
		"-p", "5",
		"--severity", "3",
	})
	if err != nil {
		t.Errorf("taskCreate error = %v", err)
	}
}

func TestTaskCreate_WithSpecialists(t *testing.T) {
	testName := "testtaskcreatespec"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{
		"create",
		"-d", "Task with specialists",
		"-a", "Action",
		"-e", "Result",
		"-sp", "developer,tester",
	})
	if err != nil {
		t.Errorf("taskCreate with specialists error = %v", err)
	}
}

// ==================== taskGet Tests ====================

func TestTaskGet_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"get", "1"})
	if err == nil {
		t.Error("taskGet with no roadmap expected error, got nil")
	}
}

func TestTaskGet_NoID(t *testing.T) {
	testName := "testtaskgetnoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"get"})
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

	err := HandleTask([]string{"get", "notanumber"})
	if err == nil {
		t.Error("taskGet with invalid ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid task ID") {
		t.Errorf("expected 'invalid task ID' error, got: %v", err)
	}
}

func TestTaskGet_MultipleIDs(t *testing.T) {
	testName := "testtaskgetmulti"
	db, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Create some tasks first
	for i := 0; i < 3; i++ {
		_, err := db.CreateTask(&models.Task{
			Priority:       1,
			Severity:       1,
			Status:         models.StatusBacklog,
			Description:    "Task " + string(rune('0'+i)),
			Action:         "Action",
			ExpectedResult: "Result",
			CreatedAt:      utils.NowISO8601(),
		})
		if err != nil {
			t.Fatalf("failed to create task: %v", err)
		}
	}

	// Get multiple tasks
	err := HandleTask([]string{"get", "1,2,3"})
	if err != nil {
		t.Errorf("taskGet with multiple IDs error = %v", err)
	}
}

// ==================== taskEdit Tests ====================

func TestTaskEdit_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"edit", "1"})
	if err == nil {
		t.Error("taskEdit with no roadmap expected error, got nil")
	}
}

func TestTaskEdit_NoID(t *testing.T) {
	testName := "testtaskeditnoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit"})
	if err == nil {
		t.Error("taskEdit with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task ID required") {
		t.Errorf("expected 'task ID required' error, got: %v", err)
	}
}

func TestTaskEdit_InvalidID(t *testing.T) {
	testName := "testtaskeditinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "notanumber"})
	if err == nil {
		t.Error("taskEdit with invalid ID expected error, got nil")
	}
}

func TestTaskEdit_NoFields(t *testing.T) {
	testName := "testtaskeditnofields"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "1"})
	if err == nil {
		t.Error("taskEdit with no fields expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no fields to update") {
		t.Errorf("expected 'no fields to update' error, got: %v", err)
	}
}

func TestTaskEdit_InvalidPriority(t *testing.T) {
	testName := "testtaskeditprio"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "1", "-p", "invalid"})
	if err == nil {
		t.Error("taskEdit with invalid priority expected error, got nil")
	}
}

func TestTaskEdit_EmptyDescription(t *testing.T) {
	testName := "testtaskeditemptydesc"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "1", "-d", ""})
	if err == nil {
		t.Error("taskEdit with empty description expected error, got nil")
	}
	if !strings.Contains(err.Error(), "description cannot be empty") {
		t.Errorf("expected 'description cannot be empty' error, got: %v", err)
	}
}

func TestTaskEdit_EmptyAction(t *testing.T) {
	testName := "testtaskeditemptyaction"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "1", "-a", ""})
	if err == nil {
		t.Error("taskEdit with empty action expected error, got nil")
	}
	if !strings.Contains(err.Error(), "action cannot be empty") {
		t.Errorf("expected 'action cannot be empty' error, got: %v", err)
	}
}

func TestTaskEdit_EmptyExpectedResult(t *testing.T) {
	testName := "testtaskeditemptyexpected"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "1", "-e", ""})
	if err == nil {
		t.Error("taskEdit with empty expected result expected error, got nil")
	}
	if !strings.Contains(err.Error(), "expected-result cannot be empty") {
		t.Errorf("expected 'expected-result cannot be empty' error, got: %v", err)
	}
}

func TestTaskEdit_EmptySpecialistsAllowed(t *testing.T) {
	testName := "testtaskeditemptyspec"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Empty specialists should be allowed (optional field)
	err := HandleTask([]string{"edit", "1", "-sp", ""})
	// This may fail because task 1 doesn't exist, but should NOT fail due to empty specialists
	// The error should be about task not found, not about empty field
	if err != nil {
		// If there's an error, it should NOT be about empty specialists
		if strings.Contains(err.Error(), "cannot be empty") {
			t.Errorf("specialists empty should be allowed, got error: %v", err)
		}
	}
}

// ==================== taskRemove Tests ====================

func TestTaskRemove_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"remove", "1"})
	if err == nil {
		t.Error("taskRemove with no roadmap expected error, got nil")
	}
}

func TestTaskRemove_NoID(t *testing.T) {
	testName := "testtaskremovenoid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"remove"})
	if err == nil {
		t.Error("taskRemove with no ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task ID(s) required") {
		t.Errorf("expected 'task ID(s) required' error, got: %v", err)
	}
}

func TestTaskRemove_InvalidID(t *testing.T) {
	testName := "testtaskremoveinvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"remove", "notanumber"})
	if err == nil {
		t.Error("taskRemove with invalid ID expected error, got nil")
	}
}

// ==================== taskSetStatus Tests ====================

func TestTaskSetStatus_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"stat", "1", "DOING"})
	if err == nil {
		t.Error("taskSetStatus with no roadmap expected error, got nil")
	}
}

func TestTaskSetStatus_NoArgs(t *testing.T) {
	testName := "testtaskstatnoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"stat"})
	if err == nil {
		t.Error("taskSetStatus with no args expected error, got nil")
	}
	if !strings.Contains(err.Error(), "task ID(s) and status required") {
		t.Errorf("expected 'task ID(s) and status required' error, got: %v", err)
	}
}

func TestTaskSetStatus_InvalidStatus(t *testing.T) {
	testName := "testtaskstatstatus"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"stat", "1", "INVALID"})
	if err == nil {
		t.Error("taskSetStatus with invalid status expected error, got nil")
	}
}

func TestTaskSetStatus_InvalidID(t *testing.T) {
	testName := "testtaskstatid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"stat", "notanumber", "DOING"})
	if err == nil {
		t.Error("taskSetStatus with invalid ID expected error, got nil")
	}
}

// ==================== taskSetPriority Tests ====================

func TestTaskSetPriority_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"prio", "1", "5"})
	if err == nil {
		t.Error("taskSetPriority with no roadmap expected error, got nil")
	}
}

func TestTaskSetPriority_NoArgs(t *testing.T) {
	testName := "testtaskpriornoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"prio"})
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

	err := HandleTask([]string{"prio", "1", "invalid"})
	if err == nil {
		t.Error("taskSetPriority with invalid priority expected error, got nil")
	}
}

func TestTaskSetPriority_OutOfRange(t *testing.T) {
	testName := "testtaskpriooutofrange"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"prio", "1", "10"})
	if err == nil {
		t.Error("taskSetPriority with priority > 9 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must be 0-9") {
		t.Errorf("expected 'must be 0-9' error, got: %v", err)
	}
}

// ==================== taskSetSeverity Tests ====================

func TestTaskSetSeverity_NoRoadmap(t *testing.T) {
	err := HandleTask([]string{"sev", "1", "5"})
	if err == nil {
		t.Error("taskSetSeverity with no roadmap expected error, got nil")
	}
}

func TestTaskSetSeverity_NoArgs(t *testing.T) {
	testName := "testtasksevnoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"sev"})
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

	err := HandleTask([]string{"sev", "1", "invalid"})
	if err == nil {
		t.Error("taskSetSeverity with invalid severity expected error, got nil")
	}
}

func TestTaskSetSeverity_OutOfRange(t *testing.T) {
	testName := "testtasksevoutofrange"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"sev", "1", "10"})
	if err == nil {
		t.Error("taskSetSeverity with severity > 9 expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must be 0-9") {
		t.Errorf("expected 'must be 0-9' error, got: %v", err)
	}
}
