package commands

import (
	"strings"
	"testing"
)

// ==================== HandleAudit Tests ====================

func TestHandleAudit_NoArgs(t *testing.T) {
	err := HandleAudit([]string{})
	if err != nil {
		t.Errorf("HandleAudit([]) error = %v, want nil", err)
	}
}

func TestHandleAudit_Help(t *testing.T) {
	helpFlags := []string{"-h", "--help", "help"}

	for _, flag := range helpFlags {
		t.Run("flag_"+flag, func(t *testing.T) {
			err := HandleAudit([]string{flag})
			if err != nil {
				t.Errorf("HandleAudit([%s]) error = %v, want nil", flag, err)
			}
		})
	}
}

func TestHandleAudit_UnknownSubcommand(t *testing.T) {
	err := HandleAudit([]string{"unknown"})
	if err == nil {
		t.Error("HandleAudit([unknown]) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown audit subcommand") {
		t.Errorf("expected 'unknown audit subcommand' error, got: %v", err)
	}
}

// ==================== auditList Tests ====================

func TestAuditList_NoRoadmap(t *testing.T) {
	err := HandleAudit([]string{"list"})
	if err == nil {
		t.Error("auditList with no roadmap expected error, got nil")
	}
}

func TestAuditList_WithRoadmap(t *testing.T) {
	testName := "testauditlist"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"list", "-r", testName})
	if err != nil {
		t.Errorf("auditList error = %v", err)
	}
}

func TestAuditList_WithFilters(t *testing.T) {
	testName := "testauditlistfilters"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	testCases := [][]string{
		{"list", "-o", "TASK_CREATE"},
		{"list", "--operation", "TASK_UPDATE"},
		{"list", "-e", "TASK"},
		{"list", "--entity-type", "SPRINT"},
		{"list", "--entity-id", "1"},
		{"list", "-l", "50"},
		{"list", "--limit", "10"},
	}

	for _, args := range testCases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			err := HandleAudit(append(args, "-r", testName))
			if err != nil {
				t.Errorf("auditList(%v) error = %v", args, err)
			}
		})
	}
}

func TestAuditList_InvalidOperation(t *testing.T) {
	testName := "testauditlistinvalidop"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"list", "-r", testName, "-o", "INVALID_OPERATION"})
	if err == nil {
		t.Error("auditList with invalid operation expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid operation") {
		t.Errorf("expected 'invalid operation' error, got: %v", err)
	}
}

func TestAuditList_InvalidEntityType(t *testing.T) {
	testName := "testauditlistinvalidentity"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"list", "-r", testName, "-e", "INVALID_TYPE"})
	if err == nil {
		t.Error("auditList with invalid entity type expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid entity type") {
		t.Errorf("expected 'invalid entity type' error, got: %v", err)
	}
}

func TestAuditList_InvalidEntityID(t *testing.T) {
	testName := "testauditlistinvalidid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"list", "-r", testName, "--entity-id", "notanumber"})
	if err == nil {
		t.Error("auditList with invalid entity ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid entity ID") {
		t.Errorf("expected 'invalid entity ID' error, got: %v", err)
	}
}

func TestAuditList_InvalidLimit(t *testing.T) {
	testName := "testauditlistinvalidlimit"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"list", "-r", testName, "-l", "notanumber"})
	if err == nil {
		t.Error("auditList with invalid limit expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid limit") {
		t.Errorf("expected 'invalid limit' error, got: %v", err)
	}
}

func TestAuditList_InvalidSinceDate(t *testing.T) {
	testName := "testauditlistinvalidsince"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"list", "-r", testName, "--since", "not-a-date"})
	if err == nil {
		t.Error("auditList with invalid since date expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid date format") {
		t.Errorf("expected 'invalid date format' error, got: %v", err)
	}
}

func TestAuditList_InvalidUntilDate(t *testing.T) {
	testName := "testauditlistinvaliduntil"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"list", "-r", testName, "--until", "not-a-date"})
	if err == nil {
		t.Error("auditList with invalid until date expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid date format") {
		t.Errorf("expected 'invalid date format' error, got: %v", err)
	}
}

func TestAuditList_ValidDates(t *testing.T) {
	testName := "testauditlistvaliddates"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{
		"list",
		"-r", testName,
		"--since", "2026-01-01T00:00:00.000Z",
		"--until", "2026-12-31T23:59:59.000Z",
	})
	if err != nil {
		t.Errorf("auditList with valid dates error = %v", err)
	}
}

// ==================== auditHistory Tests ====================

func TestAuditHistory_NoRoadmap(t *testing.T) {
	err := HandleAudit([]string{"history", "TASK", "1"})
	if err == nil {
		t.Error("auditHistory with no roadmap expected error, got nil")
	}
}

func TestAuditHistory_NoArgs(t *testing.T) {
	testName := "testaudithistnoargs"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"history", "-r", testName})
	if err == nil {
		t.Error("auditHistory with no args expected error, got nil")
	}
	if !strings.Contains(err.Error(), "entity type and ID required") {
		t.Errorf("expected 'entity type and ID required' error, got: %v", err)
	}
}

func TestAuditHistory_InvalidEntityType(t *testing.T) {
	testName := "testaudithistinvalidentity"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"history", "-r", testName, "INVALID_TYPE", "1"})
	if err == nil {
		t.Error("auditHistory with invalid entity type expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid entity type") {
		t.Errorf("expected 'invalid entity type' error, got: %v", err)
	}
}

func TestAuditHistory_InvalidEntityID(t *testing.T) {
	testName := "testaudithistinvalidid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"history", "-r", testName, "TASK", "notanumber"})
	if err == nil {
		t.Error("auditHistory with invalid entity ID expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid entity ID") {
		t.Errorf("expected 'invalid entity ID' error, got: %v", err)
	}
}

func TestAuditHistory_Success(t *testing.T) {
	testName := "testaudithistsuccess"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"history", "-r", testName, "TASK", "1"})
	if err != nil {
		t.Errorf("auditHistory error = %v", err)
	}
}

// ==================== auditStats Tests ====================

func TestAuditStats_NoRoadmap(t *testing.T) {
	err := HandleAudit([]string{"stats"})
	if err == nil {
		t.Error("auditStats with no roadmap expected error, got nil")
	}
}

func TestAuditStats_WithRoadmap(t *testing.T) {
	testName := "testauditstats"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"stats", "-r", testName})
	if err != nil {
		t.Errorf("auditStats error = %v", err)
	}
}

func TestAuditStats_InvalidSinceDate(t *testing.T) {
	testName := "testauditstatsinvalidsince"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"stats", "-r", testName, "--since", "not-a-date"})
	if err == nil {
		t.Error("auditStats with invalid since date expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid date format") {
		t.Errorf("expected 'invalid date format' error, got: %v", err)
	}
}

func TestAuditStats_InvalidUntilDate(t *testing.T) {
	testName := "testauditstatsinvaliduntil"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{"stats", "-r", testName, "--until", "not-a-date"})
	if err == nil {
		t.Error("auditStats with invalid until date expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid date format") {
		t.Errorf("expected 'invalid date format' error, got: %v", err)
	}
}

func TestAuditStats_ValidDateRange(t *testing.T) {
	testName := "testauditstatsvalid"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	err := HandleAudit([]string{
		"stats",
		"-r", testName,
		"--since", "2026-01-01T00:00:00.000Z",
		"--until", "2026-12-31T23:59:59.000Z",
	})
	if err != nil {
		t.Errorf("auditStats with valid date range error = %v", err)
	}
}
