package commands

import (
	"context"
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
		t.Error("taskList with invalid status expected error, got nil")
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
	if err == nil {
		t.Error("taskCreate with no roadmap expected error, got nil")
	}
}

func TestTaskCreate_MissingTitle(t *testing.T) {
	testName := "testtaskcreatedesc"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"create", "-r", testName, "-fr", "functional", "-tr", "technical", "-ac", "criteria"})
	if err == nil {
		t.Error("taskCreate without title expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required parameter: --title") {
		t.Errorf("expected 'missing required parameter: --title' error, got: %v", err)
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
	if !strings.Contains(err.Error(), "missing required parameter: --functional-requirements") {
		t.Errorf("expected 'missing required parameter: --functional-requirements' error, got: %v", err)
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
	if !strings.Contains(err.Error(), "missing required parameter: --technical-requirements") {
		t.Errorf("expected 'missing required parameter: --technical-requirements' error, got: %v", err)
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
	if !strings.Contains(err.Error(), "missing required parameter: --acceptance-criteria") {
		t.Errorf("expected 'missing required parameter: --acceptance-criteria' error, got: %v", err)
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

func TestTaskCreate_WithSpecialists(t *testing.T) {
	testName := "testtaskcreatespec"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{
		"create",
		"-r", testName,
		"-t", "Task with specialists",
		"-fr", "Functional",
		"-tr", "Technical",
		"-ac", "Criteria",
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

func TestTaskGet_ZeroID(t *testing.T) {
	testName := "testtaskgetzeroid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"get", "-r", testName, "0"})
	if err == nil {
		t.Error("taskGet with zero ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must be positive") {
		t.Errorf("expected 'must be positive' error, got: %v", err)
	}
}

func TestTaskGet_NegativeID(t *testing.T) {
	testName := "testtaskgetnegativeid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"get", "-r", testName, "-1"})
	if err == nil {
		t.Error("taskGet with negative ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must be positive") {
		t.Errorf("expected 'must be positive' error, got: %v", err)
	}
}

func TestTaskGet_OverflowID(t *testing.T) {
	testName := "testtaskgetoverflowid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"get", "-r", testName, "99999999999999999"})
	if err == nil {
		t.Error("taskGet with overflow ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("expected 'exceeds maximum' error, got: %v", err)
	}
}

func TestTaskGet_MultipleIDs(t *testing.T) {
	testName := "testtaskgetmulti"
	db, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Create some tasks first
	for i := 0; i < 3; i++ {
		_, err := db.CreateTask(context.Background(), &models.Task{
			Priority:               1,
			Severity:               1,
			Status:                 models.StatusBacklog,
			Title:                  "Task " + string(rune('0'+i)),
			FunctionalRequirements: "Action",
			TechnicalRequirements:  "Result",
			AcceptanceCriteria:     "Criteria",
			CreatedAt:              utils.NowISO8601(),
		})
		if err != nil {
			t.Fatalf("failed to create task: %v", err)
		}
	}

	// Get multiple tasks
	err := HandleTask([]string{"get", "-r", testName, "1,2,3"})
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

	err := HandleTask([]string{"edit", "-r", testName})
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

	err := HandleTask([]string{"edit", "-r", testName, "notanumber"})
	if err == nil {
		t.Error("taskEdit with invalid ID expected error, got nil")
	}
}

func TestTaskEdit_NoFields(t *testing.T) {
	testName := "testtaskeditnofields"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "-r", testName, "1"})
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

	err := HandleTask([]string{"edit", "-r", testName, "1", "-p", "invalid"})
	if err == nil {
		t.Error("taskEdit with invalid priority expected error, got nil")
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

func TestTaskEdit_EmptyFunctionalRequirements(t *testing.T) {
	testName := "testtaskeditemptyaction"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"edit", "-r", testName, "1", "-fr", ""})
	if err == nil {
		t.Error("taskEdit with empty functional requirements expected error, got nil")
	}
	if !strings.Contains(err.Error(), "functional-requirements cannot be empty") {
		t.Errorf("expected 'functional-requirements cannot be empty' error, got: %v", err)
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
	if !strings.Contains(err.Error(), "technical-requirements cannot be empty") {
		t.Errorf("expected 'technical-requirements cannot be empty' error, got: %v", err)
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
	if !strings.Contains(err.Error(), "acceptance-criteria cannot be empty") {
		t.Errorf("expected 'acceptance-criteria cannot be empty' error, got: %v", err)
	}
}

func TestTaskEdit_EmptySpecialistsAllowed(t *testing.T) {
	testName := "testtaskeditemptyspec"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Empty specialists should be allowed (optional field)
	err := HandleTask([]string{"edit", "-r", testName, "1", "-sp", ""})
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

	err := HandleTask([]string{"remove", "-r", testName})
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

	err := HandleTask([]string{"remove", "-r", testName, "notanumber"})
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

	err := HandleTask([]string{"stat", "-r", testName})
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

	err := HandleTask([]string{"stat", "-r", testName, "1", "INVALID"})
	if err == nil {
		t.Error("taskSetStatus with invalid status expected error, got nil")
	}
}

func TestTaskSetStatus_InvalidID(t *testing.T) {
	testName := "testtaskstatid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleTask([]string{"stat", "-r", testName, "notanumber", "DOING"})
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
	if !strings.Contains(err.Error(), "must be 0-9") {
		t.Errorf("expected 'must be 0-9' error, got: %v", err)
	}
}
