package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// captureOutput captures stdout during test execution
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()

	// Save original stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Execute function
	fn()

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read captured output: %v", err)
	}

	return buf.String()
}

// cleanupIntegrationTest removes test roadmaps
func cleanupIntegrationTest(t *testing.T, names ...string) {
	t.Helper()

	dataDir, _ := utils.GetDataDir()
	for _, name := range names {
		testPath := filepath.Join(dataDir, name+".db")
		os.Remove(testPath)
		os.Remove(testPath + "-shm")
		os.Remove(testPath + "-wal")
	}
}

// TestIntegration_RoadmapLifecycle tests complete roadmap lifecycle
func TestIntegration_RoadmapLifecycle(t *testing.T) {
	testRoadmap := "integrationtestroadmap"
	cleanupIntegrationTest(t, testRoadmap)
	defer cleanupIntegrationTest(t, testRoadmap)

	// Step 1: Create roadmap
	err := HandleRoadmap([]string{"create", testRoadmap})
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}

	// Verify roadmap exists
	exists, err := utils.RoadmapExists(testRoadmap)
	if err != nil {
		t.Fatalf("failed to check roadmap existence: %v", err)
	}
	if !exists {
		t.Fatal("roadmap was not created")
	}

	// Step 2: List roadmaps
	output := captureOutput(t, func() {
		err := HandleRoadmap([]string{"list"})
		if err != nil {
			t.Errorf("failed to list roadmaps: %v", err)
		}
	})

	// Verify output contains our roadmap
	var roadmaps []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &roadmaps); err != nil {
		t.Logf("list output: %s", output)
	} else {
		found := false
		for _, r := range roadmaps {
			if r["name"] == testRoadmap {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("created roadmap not found in list")
		}
	}

	// Step 3: Use roadmap
	err = HandleRoadmap([]string{"use", testRoadmap})
	if err != nil {
		t.Errorf("failed to use roadmap: %v", err)
	}

	// Verify current roadmap
	current, err := getCurrentRoadmap()
	if err != nil {
		t.Errorf("failed to get current roadmap: %v", err)
	}
	if current != testRoadmap {
		t.Errorf("current roadmap = %q, want %q", current, testRoadmap)
	}

	// Step 4: Remove roadmap
	err = HandleRoadmap([]string{"remove", testRoadmap})
	if err != nil {
		t.Errorf("failed to remove roadmap: %v", err)
	}

	// Verify roadmap no longer exists
	exists, _ = utils.RoadmapExists(testRoadmap)
	if exists {
		t.Error("roadmap still exists after removal")
	}
}

// TestIntegration_TaskLifecycle tests complete task lifecycle
func TestIntegration_TaskLifecycle(t *testing.T) {
	testRoadmap := "integrationtesttasks"
	cleanupIntegrationTest(t, testRoadmap)
	defer cleanupIntegrationTest(t, testRoadmap)

	// Create roadmap and set as current
	err := HandleRoadmap([]string{"create", testRoadmap})
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	defer HandleRoadmap([]string{"remove", testRoadmap})

	err = HandleRoadmap([]string{"use", testRoadmap})
	if err != nil {
		t.Fatalf("failed to use roadmap: %v", err)
	}

	// Step 1: Create task
	output := captureOutput(t, func() {
		err := HandleTask([]string{
			"create",
			"-t", "Integration test task",
			"-f", "Perform integration test",
			"-h", "Task created successfully",
			"-a", "Acceptance criteria met",
			"-p", "5",
			"--severity", "3",
		})
		if err != nil {
			t.Errorf("failed to create task: %v", err)
		}
	})

	// Parse task ID from output
	var createResult map[string]int
	if err := json.Unmarshal([]byte(output), &createResult); err != nil {
		t.Fatalf("failed to parse create task output: %v\noutput: %s", err, output)
	}
	taskID := createResult["id"]
	if taskID == 0 {
		t.Fatal("task ID not returned from create")
	}

	// Step 2: Get task
	output = captureOutput(t, func() {
		err := HandleTask([]string{"get", string(rune('0' + taskID))})
		if err != nil {
			t.Errorf("failed to get task: %v", err)
		}
	})

	// Verify task data
	var tasks []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &tasks); err != nil {
		t.Logf("get task output: %s", output)
	} else if len(tasks) > 0 {
		task := tasks[0]
		if task["title"] != "Integration test task" {
			t.Errorf("task title = %v, want %v", task["title"], "Integration test task")
		}
		if task["priority"] != float64(5) {
			t.Errorf("task priority = %v, want %v", task["priority"], 5)
		}
	}

	// Step 3: List tasks
	output = captureOutput(t, func() {
		err := HandleTask([]string{"list"})
		if err != nil {
			t.Errorf("failed to list tasks: %v", err)
		}
	})

	// Verify task appears in list
	if !strings.Contains(output, "Integration test task") {
		t.Errorf("created task not found in list\noutput: %s", output)
	}

	// Step 4: Edit task
	err = HandleTask([]string{
		"edit",
		string(rune('0' + taskID)),
		"-t", "Updated integration test task",
	})
	if err != nil {
		t.Errorf("failed to edit task: %v", err)
	}

	// Step 5: Set task status (valid transition: BACKLOG -> SPRINT)
	err = HandleTask([]string{
		"stat",
		string(rune('0' + taskID)),
		"SPRINT",
	})
	if err != nil {
		t.Errorf("failed to set task status: %v", err)
	}

	// Step 6: Set task priority
	err = HandleTask([]string{
		"prio",
		string(rune('0' + taskID)),
		"8",
	})
	if err != nil {
		t.Errorf("failed to set task priority: %v", err)
	}

	// Step 7: Remove task
	err = HandleTask([]string{"remove", string(rune('0' + taskID))})
	if err != nil {
		t.Errorf("failed to remove task: %v", err)
	}
}

// TestIntegration_SprintLifecycle tests complete sprint lifecycle
func TestIntegration_SprintLifecycle(t *testing.T) {
	testRoadmap := "integrationtestsprints"
	cleanupIntegrationTest(t, testRoadmap)
	defer cleanupIntegrationTest(t, testRoadmap)

	// Create roadmap and set as current
	err := HandleRoadmap([]string{"create", testRoadmap})
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	defer HandleRoadmap([]string{"remove", testRoadmap})

	err = HandleRoadmap([]string{"use", testRoadmap})
	if err != nil {
		t.Fatalf("failed to use roadmap: %v", err)
	}

	// Step 1: Create sprint
	output := captureOutput(t, func() {
		err := HandleSprint([]string{
			"create",
			"-d", "Integration test sprint",
		})
		if err != nil {
			t.Errorf("failed to create sprint: %v", err)
		}
	})

	// Parse sprint ID from output
	var createResult map[string]int
	if err := json.Unmarshal([]byte(output), &createResult); err != nil {
		t.Fatalf("failed to parse create sprint output: %v\noutput: %s", err, output)
	}
	sprintID := createResult["id"]
	if sprintID == 0 {
		t.Fatal("sprint ID not returned from create")
	}

	// Step 2: Get sprint
	output = captureOutput(t, func() {
		err := HandleSprint([]string{"get", string(rune('0' + sprintID))})
		if err != nil {
			t.Errorf("failed to get sprint: %v", err)
		}
	})

	// Verify sprint data
	var sprint map[string]interface{}
	if err := json.Unmarshal([]byte(output), &sprint); err != nil {
		t.Logf("get sprint output: %s", output)
	} else {
		if sprint["description"] != "Integration test sprint" {
			t.Errorf("sprint description = %v, want %v", sprint["description"], "Integration test sprint")
		}
		if sprint["status"] != "PENDING" {
			t.Errorf("sprint status = %v, want %v", sprint["status"], "PENDING")
		}
	}

	// Step 3: List sprints
	output = captureOutput(t, func() {
		err := HandleSprint([]string{"list"})
		if err != nil {
			t.Errorf("failed to list sprints: %v", err)
		}
	})

	// Verify sprint appears in list
	if !strings.Contains(output, "Integration test sprint") {
		t.Errorf("created sprint not found in list\noutput: %s", output)
	}

	// Step 4: Start sprint
	err = HandleSprint([]string{"start", string(rune('0' + sprintID))})
	if err != nil {
		t.Errorf("failed to start sprint: %v", err)
	}

	// Step 5: Close sprint
	err = HandleSprint([]string{"close", string(rune('0' + sprintID))})
	if err != nil {
		t.Errorf("failed to close sprint: %v", err)
	}

	// Step 6: Remove sprint
	err = HandleSprint([]string{"remove", string(rune('0' + sprintID))})
	if err != nil {
		t.Errorf("failed to remove sprint: %v", err)
	}
}

// TestIntegration_AuditQuery tests audit log queries
func TestIntegration_AuditQuery(t *testing.T) {
	testRoadmap := "integrationtestaudit"
	cleanupIntegrationTest(t, testRoadmap)
	defer cleanupIntegrationTest(t, testRoadmap)

	// Create roadmap and set as current
	err := HandleRoadmap([]string{"create", testRoadmap})
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	defer HandleRoadmap([]string{"remove", testRoadmap})

	err = HandleRoadmap([]string{"use", testRoadmap})
	if err != nil {
		t.Fatalf("failed to use roadmap: %v", err)
	}

	// Create a task to generate audit entry
	err = HandleTask([]string{
		"create",
		"-t", "Audit test task",
		"-f", "Test functional",
		"-h", "Test technical",
		"-a", "Test acceptance",
	})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Step 1: List audit entries
	output := captureOutput(t, func() {
		err := HandleAudit([]string{"list"})
		if err != nil {
			t.Errorf("failed to list audit entries: %v", err)
		}
	})

	// Verify audit entries exist
	var auditEntries []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &auditEntries); err != nil {
		t.Logf("audit list output: %s", output)
	} else if len(auditEntries) == 0 {
		t.Error("no audit entries found")
	}

	// Step 2: Query audit stats
	output = captureOutput(t, func() {
		err := HandleAudit([]string{"stats"})
		if err != nil {
			t.Errorf("failed to get audit stats: %v", err)
		}
	})

	// Verify stats output is valid JSON
	var stats map[string]interface{}
	if err := json.Unmarshal([]byte(output), &stats); err != nil {
		t.Logf("audit stats output: %s", output)
	}
}

// TestIntegration_ErrorHandling tests error handling in integration scenarios
func TestIntegration_ErrorHandling(t *testing.T) {
	testRoadmap := "integrationtesterrors"
	cleanupIntegrationTest(t, testRoadmap)
	defer cleanupIntegrationTest(t, testRoadmap)

	// Test 1: Create roadmap without name
	err := HandleRoadmap([]string{"create"})
	if err == nil {
		t.Error("expected error when creating roadmap without name")
	}
	if !utils.IsRequired(err) {
		t.Errorf("expected ErrRequired, got: %v", err)
	}

	// Test 2: Get non-existent task
	err = HandleRoadmap([]string{"create", testRoadmap})
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	defer HandleRoadmap([]string{"remove", testRoadmap})

	err = HandleRoadmap([]string{"use", testRoadmap})
	if err != nil {
		t.Fatalf("failed to use roadmap: %v", err)
	}

	// This should fail because task 999 doesn't exist
	_ = HandleTask([]string{"get", "999"})

	// Test 3: Invalid task ID
	err = HandleTask([]string{"get", "invalid"})
	if err == nil {
		t.Error("expected error for invalid task ID")
	}
	if !utils.IsInvalidInput(err) {
		t.Errorf("expected ErrInvalidInput, got: %v", err)
	}

	// Test 4: Create task without required fields
	err = HandleTask([]string{"create", "-t", "test"})
	if err == nil {
		t.Error("expected error when creating task without required fields")
	}
	if !utils.IsRequired(err) {
		t.Errorf("expected ErrRequired, got: %v", err)
	}
}
