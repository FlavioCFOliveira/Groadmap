package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ==================== JSON Assertion Helpers ====================

// parseJSONObject parses a JSON object from output, failing the test on error.
func parseJSONObject(t *testing.T, output string) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not a valid JSON object: %v\noutput: %s", err, output)
	}
	return result
}

// parseJSONArray parses a JSON array from output, failing the test on error.
func parseJSONArray(t *testing.T, output string) []map[string]interface{} {
	t.Helper()
	var result []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not a valid JSON array: %v\noutput: %s", err, output)
	}
	return result
}

// assertStringField asserts that a JSON object has a string field with the expected value.
func assertStringField(t *testing.T, obj map[string]interface{}, field, want string) {
	t.Helper()
	raw, ok := obj[field]
	if !ok {
		t.Errorf("field %q missing from JSON object; keys: %v", field, jsonKeys(obj))
		return
	}
	got, ok := raw.(string)
	if !ok {
		t.Errorf("field %q: expected string, got %T (%v)", field, raw, raw)
		return
	}
	if got != want {
		t.Errorf("field %q = %q, want %q", field, got, want)
	}
}

// assertNumericField asserts that a JSON object has a numeric field with the expected value.
func assertNumericField(t *testing.T, obj map[string]interface{}, field string, want float64) {
	t.Helper()
	raw, ok := obj[field]
	if !ok {
		t.Errorf("field %q missing from JSON object; keys: %v", field, jsonKeys(obj))
		return
	}
	got, ok := raw.(float64)
	if !ok {
		t.Errorf("field %q: expected number, got %T (%v)", field, raw, raw)
		return
	}
	if got != want {
		t.Errorf("field %q = %v, want %v", field, got, want)
	}
}

// assertFieldExists asserts that a JSON object contains the given field key.
func assertFieldExists(t *testing.T, obj map[string]interface{}, field string) {
	t.Helper()
	if _, ok := obj[field]; !ok {
		t.Errorf("field %q missing from JSON object; keys: %v", field, jsonKeys(obj))
	}
}

// assertNonEmptyArray asserts that the JSON array is not empty.
func assertNonEmptyArray(t *testing.T, arr []map[string]interface{}, context string) {
	t.Helper()
	if len(arr) == 0 {
		t.Errorf("%s: expected non-empty JSON array, got empty", context)
	}
}

// jsonKeys returns the sorted key list of a JSON object for readable error messages.
func jsonKeys(obj map[string]interface{}) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	return keys
}

// extractIntID extracts the "id" field from a JSON create-response object.
func extractIntID(t *testing.T, output string) int {
	t.Helper()
	obj := parseJSONObject(t, output)
	raw, ok := obj["id"]
	if !ok {
		t.Fatalf("create response missing 'id' field; output: %s", output)
	}
	id, ok := raw.(float64)
	if !ok {
		t.Fatalf("'id' field is not a number, got %T (%v)", raw, raw)
	}
	return int(id)
}

// ==================== Output Validation: Task ====================

func TestOutputValidation_TaskCreate_JSONStructure(t *testing.T) {
	const roadmap = "outputval-taskcreate"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	output := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create", "-r", roadmap,
			"-t", "Implement payment gateway integration",
			"-fr", "Enable recurring billing for subscription plans",
			"-tr", "Use Stripe SDK with idempotency keys to prevent duplicate charges",
			"-ac", "Successful charge for new and existing customers without duplicate entries",
			"-p", "7",
			"--severity", "5",
		}); err != nil {
			t.Errorf("taskCreate error = %v", err)
		}
	})

	obj := parseJSONObject(t, output)
	assertFieldExists(t, obj, "id")
	assertNumericField(t, obj, "id", float64(int(obj["id"].(float64)))) // id must be a positive int
	if id, ok := obj["id"].(float64); !ok || id <= 0 {
		t.Errorf("'id' must be a positive integer, got %v", obj["id"])
	}
}

func TestOutputValidation_TaskCreate_DBState(t *testing.T) {
	const roadmap = "outputval-taskcreatedb"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	var taskID int
	output := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create", "-r", roadmap,
			"-t", "Migrate authentication service to OAuth2",
			"-fr", "Allow SSO login via company identity provider",
			"-tr", "Integrate Auth0 SDK and update session middleware",
			"-ac", "Users can log in via SSO without password reset",
			"-p", "8",
			"--severity", "6",
		}); err != nil {
			t.Errorf("taskCreate error = %v", err)
		}
	})
	taskID = extractIntID(t, output)

	// Verify DB state matches the create payload
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask(%d) error = %v", taskID, err)
	}
	if task.Title != "Migrate authentication service to OAuth2" {
		t.Errorf("DB title = %q, want %q", task.Title, "Migrate authentication service to OAuth2")
	}
	if task.Priority != 8 {
		t.Errorf("DB priority = %d, want 8", task.Priority)
	}
	if task.Severity != 6 {
		t.Errorf("DB severity = %d, want 6", task.Severity)
	}
	if task.Status != models.StatusBacklog {
		t.Errorf("DB status = %q, want %q", task.Status, models.StatusBacklog)
	}
	if task.FunctionalRequirements != "Allow SSO login via company identity provider" {
		t.Errorf("DB functional_requirements = %q, want %q", task.FunctionalRequirements, "Allow SSO login via company identity provider")
	}
}

func TestOutputValidation_TaskGet_JSONStructure(t *testing.T) {
	const roadmap = "outputval-taskget"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	// Create a task first
	var taskID int
	createOut := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create", "-r", roadmap,
			"-t", "Configure CI/CD pipeline for staging environment",
			"-fr", "Automate deployment to staging on every merge to main",
			"-tr", "Extend GitHub Actions workflow with staging deploy step",
			"-ac", "Staging deploys automatically within 5 minutes of merge",
			"-p", "6",
		}); err != nil {
			t.Fatalf("taskCreate error = %v", err)
		}
	})
	taskID = extractIntID(t, createOut)

	// Get the task and validate JSON structure
	output := captureOutput(t, func() {
		if err := HandleTask([]string{"get", "-r", roadmap, strconv.Itoa(taskID)}); err != nil {
			t.Errorf("taskGet error = %v", err)
		}
	})

	arr := parseJSONArray(t, output)
	assertNonEmptyArray(t, arr, "taskGet")

	task := arr[0]
	requiredFields := []string{"id", "title", "status", "type", "priority", "severity",
		"functional_requirements", "technical_requirements", "acceptance_criteria", "created_at"}
	for _, f := range requiredFields {
		assertFieldExists(t, task, f)
	}

	assertStringField(t, task, "title", "Configure CI/CD pipeline for staging environment")
	assertStringField(t, task, "status", string(models.StatusBacklog))
	assertNumericField(t, task, "id", float64(taskID))
	assertNumericField(t, task, "priority", 6)
}

func TestOutputValidation_TaskEdit_DBState(t *testing.T) {
	const roadmap = "outputval-taskedit"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	// Create a task
	var taskID int
	createOut := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create", "-r", roadmap,
			"-t", "Optimise database query for user dashboard",
			"-fr", "Reduce dashboard load time to under 200ms",
			"-tr", "Add composite index on (user_id, created_at)",
			"-ac", "P95 latency below 200ms measured in staging",
			"-p", "4",
		}); err != nil {
			t.Fatalf("taskCreate error = %v", err)
		}
	})
	taskID = extractIntID(t, createOut)

	// Edit priority and title
	if err := HandleTask([]string{
		"edit", "-r", roadmap, strconv.Itoa(taskID),
		"-t", "Optimise database query for analytics dashboard",
		"-p", "9",
	}); err != nil {
		t.Fatalf("taskEdit error = %v", err)
	}

	// Verify DB reflects the updates
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask(%d) after edit error = %v", taskID, err)
	}
	if task.Title != "Optimise database query for analytics dashboard" {
		t.Errorf("DB title after edit = %q, want %q", task.Title, "Optimise database query for analytics dashboard")
	}
	if task.Priority != 9 {
		t.Errorf("DB priority after edit = %d, want 9", task.Priority)
	}
}

func TestOutputValidation_TaskList_JSONArray(t *testing.T) {
	const roadmap = "outputval-tasklist"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	titles := []string{
		"Implement rate limiter for public API",
		"Add distributed tracing with OpenTelemetry",
		"Refactor session management to use Redis",
	}

	for i, title := range titles {
		if out := captureOutput(t, func() {
			_ = HandleTask([]string{
				"create", "-r", roadmap,
				"-t", title,
				"-fr", fmt.Sprintf("Functional requirement for task %d", i),
				"-tr", fmt.Sprintf("Technical approach for task %d", i),
				"-ac", fmt.Sprintf("Acceptance criteria for task %d", i),
			})
		}); out == "" {
			t.Fatalf("task create returned empty output for %q", title)
		}
	}

	output := captureOutput(t, func() {
		if err := HandleTask([]string{"list", "-r", roadmap}); err != nil {
			t.Errorf("taskList error = %v", err)
		}
	})

	arr := parseJSONArray(t, output)
	if len(arr) != 3 {
		t.Errorf("taskList returned %d items, want 3", len(arr))
	}

	// Verify each element has the core fields
	for i, item := range arr {
		assertFieldExists(t, item, "id")
		assertFieldExists(t, item, "title")
		assertFieldExists(t, item, "status")
		assertFieldExists(t, item, "priority")
		// Verify all items have BACKLOG status
		assertStringField(t, item, "status", string(models.StatusBacklog))
		_ = i
	}
}

func TestOutputValidation_TaskList_StatusFilter(t *testing.T) {
	const roadmap = "outputval-tasklistfilter"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	// Create 3 tasks via CLI
	taskIDs := make([]int, 3)
	for i := range taskIDs {
		out := captureOutput(t, func() {
			_ = HandleTask([]string{
				"create", "-r", roadmap,
				"-t", fmt.Sprintf("Deploy microservice component %d to production", i),
				"-fr", fmt.Sprintf("Requirement %d", i),
				"-tr", fmt.Sprintf("Technical spec %d", i),
				"-ac", fmt.Sprintf("Verified in production %d", i),
			})
		})
		taskIDs[i] = extractIntID(t, out)
	}

	// Manually advance one task to DOING via DB (simulates sprint add + start)
	doingStatus := models.StatusDoing
	if err := database.UpdateTaskStatus(context.Background(), []int{taskIDs[0]}, doingStatus); err != nil {
		t.Fatalf("UpdateTaskStatus error = %v", err)
	}

	// List with DOING filter
	output := captureOutput(t, func() {
		if err := HandleTask([]string{"list", "-r", roadmap, "-s", "DOING"}); err != nil {
			t.Errorf("taskList -s DOING error = %v", err)
		}
	})

	arr := parseJSONArray(t, output)
	if len(arr) != 1 {
		t.Errorf("filtered list (DOING) returned %d items, want 1", len(arr))
	}
	if len(arr) > 0 {
		assertStringField(t, arr[0], "status", string(models.StatusDoing))
		assertNumericField(t, arr[0], "id", float64(taskIDs[0]))
	}
}

// ==================== Output Validation: Sprint ====================

func TestOutputValidation_SprintCreate_JSONStructure(t *testing.T) {
	const roadmap = "outputval-sprintcreate"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	output := captureOutput(t, func() {
		if err := HandleSprint([]string{
			"create", "-r", roadmap,
			"-d", "Sprint 01: Core authentication and authorisation features",
		}); err != nil {
			t.Errorf("sprintCreate error = %v", err)
		}
	})

	obj := parseJSONObject(t, output)
	assertFieldExists(t, obj, "id")
	if id, ok := obj["id"].(float64); !ok || id <= 0 {
		t.Errorf("'id' must be a positive integer, got %v", obj["id"])
	}
}

func TestOutputValidation_SprintCreate_DBState(t *testing.T) {
	const roadmap = "outputval-sprintcreatedb"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	output := captureOutput(t, func() {
		if err := HandleSprint([]string{
			"create", "-r", roadmap,
			"-d", "Sprint 02: Payment integration and billing workflows",
		}); err != nil {
			t.Errorf("sprintCreate error = %v", err)
		}
	})

	obj := parseJSONObject(t, output)
	sprintID := int(obj["id"].(float64))

	sprint, err := database.GetSprint(context.Background(), sprintID)
	if err != nil {
		t.Fatalf("GetSprint(%d) error = %v", sprintID, err)
	}
	if sprint.Description != "Sprint 02: Payment integration and billing workflows" {
		t.Errorf("DB description = %q, want %q", sprint.Description, "Sprint 02: Payment integration and billing workflows")
	}
	if sprint.Status != models.SprintPending {
		t.Errorf("DB status = %q, want %q", sprint.Status, models.SprintPending)
	}
}

func TestOutputValidation_SprintList_JSONArray(t *testing.T) {
	const roadmap = "outputval-sprintlist"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	descriptions := []string{
		"Sprint 01: Infrastructure setup and baseline configuration",
		"Sprint 02: User authentication and session management",
		"Sprint 03: API design and documentation",
	}

	for _, desc := range descriptions {
		d := desc
		_ = captureOutput(t, func() {
			_ = HandleSprint([]string{"create", "-r", roadmap, "-d", d})
		})
	}

	output := captureOutput(t, func() {
		if err := HandleSprint([]string{"list", "-r", roadmap}); err != nil {
			t.Errorf("sprintList error = %v", err)
		}
	})

	arr := parseJSONArray(t, output)
	if len(arr) != 3 {
		t.Errorf("sprintList returned %d items, want 3", len(arr))
	}

	for _, item := range arr {
		assertFieldExists(t, item, "id")
		assertFieldExists(t, item, "status")
		assertFieldExists(t, item, "description")
		assertFieldExists(t, item, "created_at")
		assertStringField(t, item, "status", string(models.SprintPending))
	}
}

// ==================== Output Validation: Roadmap ====================

func TestOutputValidation_RoadmapCreate_JSONStructure(t *testing.T) {
	const roadmap = "outputval-roadmapcreate"
	cleanupTestRoadmap(t, roadmap)
	defer cleanupTestRoadmap(t, roadmap)

	output := captureOutput(t, func() {
		if err := HandleRoadmap([]string{"create", roadmap}); err != nil {
			t.Errorf("roadmapCreate error = %v", err)
		}
	})

	obj := parseJSONObject(t, output)
	assertStringField(t, obj, "name", roadmap)
}

func TestOutputValidation_RoadmapList_JSONArray(t *testing.T) {
	const roadmap = "outputval-roadmaplist"
	cleanupTestRoadmap(t, roadmap)
	defer cleanupTestRoadmap(t, roadmap)

	// Create the roadmap
	if err := HandleRoadmap([]string{"create", roadmap}); err != nil {
		t.Fatalf("roadmapCreate error = %v", err)
	}

	output := captureOutput(t, func() {
		if err := HandleRoadmap([]string{"list"}); err != nil {
			t.Errorf("roadmapList error = %v", err)
		}
	})

	arr := parseJSONArray(t, output)
	assertNonEmptyArray(t, arr, "roadmapList")

	// Find our created roadmap in the list
	found := false
	for _, item := range arr {
		assertFieldExists(t, item, "name")
		assertFieldExists(t, item, "path")
		assertFieldExists(t, item, "size")
		if name, ok := item["name"].(string); ok && name == roadmap {
			found = true
			// size must be a positive number (the .db file has schema content)
			if sz, ok := item["size"].(float64); ok && sz <= 0 {
				t.Errorf("roadmap %q size = %v, want > 0", roadmap, sz)
			}
		}
	}
	if !found {
		t.Errorf("roadmap %q not found in list output", roadmap)
	}
}

// ==================== Output Validation: Sprint Show ====================

func TestOutputValidation_SprintShow_JSONStructure(t *testing.T) {
	const roadmap = "outputval-sprintshow"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	// Create a sprint and two tasks, then add tasks to sprint
	sprintOut := captureOutput(t, func() {
		_ = HandleSprint([]string{
			"create", "-r", roadmap,
			"-d", "Sprint 01: Search index implementation and relevance tuning",
		})
	})
	sprintID := int(parseJSONObject(t, sprintOut)["id"].(float64))

	taskIDs := make([]int, 2)
	for i := range taskIDs {
		out := captureOutput(t, func() {
			_ = HandleTask([]string{
				"create", "-r", roadmap,
				"-t", fmt.Sprintf("Implement search feature component %d", i+1),
				"-fr", fmt.Sprintf("Search capability %d required by product team", i+1),
				"-tr", fmt.Sprintf("Elasticsearch integration approach %d", i+1),
				"-ac", fmt.Sprintf("Search returns relevant results for query %d", i+1),
			})
		})
		taskIDs[i] = extractIntID(t, out)
	}

	if err := HandleSprint([]string{
		"add-tasks", "-r", roadmap,
		strconv.Itoa(sprintID),
		strconv.Itoa(taskIDs[0]),
		strconv.Itoa(taskIDs[1]),
	}); err != nil {
		t.Fatalf("add-tasks error = %v", err)
	}

	// Verify DB: tasks now have SPRINT status
	for _, id := range taskIDs {
		task, err := database.GetTask(context.Background(), id)
		if err != nil {
			t.Fatalf("GetTask(%d) error = %v", id, err)
		}
		if task.Status != models.StatusSprint {
			t.Errorf("task %d status after add-tasks = %q, want %q", id, task.Status, models.StatusSprint)
		}
	}

	// Start sprint and verify show output structure
	if err := HandleSprint([]string{"start", "-r", roadmap, strconv.Itoa(sprintID)}); err != nil {
		t.Fatalf("sprint start error = %v", err)
	}

	output := captureOutput(t, func() {
		if err := HandleSprint([]string{"show", "-r", roadmap, strconv.Itoa(sprintID)}); err != nil {
			t.Errorf("sprint show error = %v", err)
		}
	})

	obj := parseJSONObject(t, output)

	// Top-level fields from SprintShowResult
	for _, f := range []string{"sprint_id", "sprint_description", "status", "summary", "progress", "task_order"} {
		assertFieldExists(t, obj, f)
	}

	assertNumericField(t, obj, "sprint_id", float64(sprintID))
	assertStringField(t, obj, "status", string(models.SprintOpen))

	// Validate nested summary object
	summary, ok := obj["summary"].(map[string]interface{})
	if !ok {
		t.Fatalf("'summary' is not an object, got %T", obj["summary"])
	}
	for _, f := range []string{"total_tasks", "pending", "in_progress", "completed"} {
		if _, exists := summary[f]; !exists {
			t.Errorf("summary.%s missing", f)
		}
	}
	if summary["total_tasks"] != float64(2) {
		t.Errorf("summary.total_tasks = %v, want 2", summary["total_tasks"])
	}
	if summary["completed"] != float64(0) {
		t.Errorf("summary.completed = %v, want 0", summary["completed"])
	}

	// Validate task_order array
	taskOrder, ok := obj["task_order"].([]interface{})
	if !ok {
		t.Fatalf("task_order is not an array, got %T", obj["task_order"])
	}
	if len(taskOrder) != 2 {
		t.Errorf("task_order length = %d, want 2", len(taskOrder))
	}
}

// ==================== Output Validation: Stats ====================

func TestOutputValidation_Stats_JSONStructure(t *testing.T) {
	const roadmap = "outputval-stats"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	// Create two tasks so stats are non-trivial
	for i := 0; i < 2; i++ {
		_ = captureOutput(t, func() {
			_ = HandleTask([]string{
				"create", "-r", roadmap,
				"-t", fmt.Sprintf("Integrate monitoring dashboard component %d", i+1),
				"-fr", fmt.Sprintf("Observability requirement %d", i+1),
				"-tr", fmt.Sprintf("Prometheus metrics endpoint %d", i+1),
				"-ac", fmt.Sprintf("Alerts firing correctly for scenario %d", i+1),
			})
		})
	}

	output := captureOutput(t, func() {
		if err := HandleStats([]string{"-r", roadmap}); err != nil {
			t.Errorf("HandleStats error = %v", err)
		}
	})

	obj := parseJSONObject(t, output)
	assertFieldExists(t, obj, "roadmap")
	assertFieldExists(t, obj, "tasks")
	assertFieldExists(t, obj, "sprints")
	assertStringField(t, obj, "roadmap", roadmap)

	tasks, ok := obj["tasks"].(map[string]interface{})
	if !ok {
		t.Fatalf("'tasks' field is not an object, got %T", obj["tasks"])
	}
	for _, key := range []string{"backlog", "sprint", "doing", "testing", "completed"} {
		if _, ok := tasks[key]; !ok {
			t.Errorf("tasks.%s missing from stats output", key)
		}
	}
	if tasks["backlog"] != float64(2) {
		t.Errorf("tasks.backlog = %v, want 2", tasks["backlog"])
	}

	sprints, ok := obj["sprints"].(map[string]interface{})
	if !ok {
		t.Fatalf("'sprints' field is not an object, got %T", obj["sprints"])
	}
	for _, key := range []string{"current", "total", "completed", "pending"} {
		if _, ok := sprints[key]; !ok {
			t.Errorf("sprints.%s missing from stats output", key)
		}
	}
}

// ==================== Output Validation: Type Flag ====================

func TestOutputValidation_TaskCreate_TypeFlag(t *testing.T) {
	const roadmap = "outputval-tasktype"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	tests := []struct {
		typeFlag string
		wantType models.TaskType
	}{
		{"-y", models.TypeBug},
		{"--type", models.TypeRefactor},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(string(tc.wantType), func(t *testing.T) {
			out := captureOutput(t, func() {
				if err := HandleTask([]string{
					"create", "-r", roadmap,
					"-t", fmt.Sprintf("Task of type %s", tc.wantType),
					"-fr", "Functional requirement for type test",
					"-tr", "Technical approach for type test",
					"-ac", "Verified type persisted correctly",
					tc.typeFlag, string(tc.wantType),
				}); err != nil {
					t.Errorf("taskCreate (%s) error = %v", tc.wantType, err)
				}
			})

			taskID := extractIntID(t, out)
			task, err := database.GetTask(context.Background(), taskID)
			if err != nil {
				t.Fatalf("GetTask(%d) error = %v", taskID, err)
			}
			if task.Type != tc.wantType {
				t.Errorf("DB type = %q, want %q", task.Type, tc.wantType)
			}
		})
	}
}

// ==================== Output Validation: Task Status Transition ====================

func TestOutputValidation_TaskStat_DBState(t *testing.T) {
	const roadmap = "outputval-taskstat"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	// Create sprint and task, add task to sprint to reach SPRINT status
	sprintOut := captureOutput(t, func() {
		_ = HandleSprint([]string{
			"create", "-r", roadmap,
			"-d", "Sprint 01: Notification system implementation",
		})
	})
	sprintID := int(parseJSONObject(t, sprintOut)["id"].(float64))

	taskOut := captureOutput(t, func() {
		_ = HandleTask([]string{
			"create", "-r", roadmap,
			"-t", "Implement push notification delivery service",
			"-fr", "Users receive push notifications within 30 seconds",
			"-tr", "Use Firebase Cloud Messaging with retry backoff",
			"-ac", "Notifications delivered to 99.9% of active users",
		})
	})
	taskID := extractIntID(t, taskOut)

	if err := HandleSprint([]string{
		"add-tasks", "-r", roadmap, strconv.Itoa(sprintID), strconv.Itoa(taskID),
	}); err != nil {
		t.Fatalf("add-tasks error = %v", err)
	}
	if err := HandleSprint([]string{"start", "-r", roadmap, strconv.Itoa(sprintID)}); err != nil {
		t.Fatalf("sprint start error = %v", err)
	}

	transitions := []struct {
		status     string
		wantStatus models.TaskStatus
		wantField  string // DB timestamp field that should be set
	}{
		{"DOING", models.StatusDoing, "started_at"},
		{"TESTING", models.StatusTesting, "tested_at"},
		{"COMPLETED", models.StatusCompleted, "closed_at"},
	}

	idStr := strconv.Itoa(taskID)
	for _, tr := range transitions {
		tr := tr
		t.Run(tr.status, func(t *testing.T) {
			if err := HandleTask([]string{"stat", "-r", roadmap, idStr, tr.status}); err != nil {
				t.Fatalf("task stat %s error = %v", tr.status, err)
			}

			task, err := database.GetTask(context.Background(), taskID)
			if err != nil {
				t.Fatalf("GetTask after %s error = %v", tr.status, err)
			}
			if task.Status != tr.wantStatus {
				t.Errorf("DB status after %s = %q, want %q", tr.status, task.Status, tr.wantStatus)
			}

			switch tr.wantField {
			case "started_at":
				if task.StartedAt == nil {
					t.Errorf("started_at should be set after DOING transition")
				}
			case "tested_at":
				if task.TestedAt == nil {
					t.Errorf("tested_at should be set after TESTING transition")
				}
			case "closed_at":
				if task.ClosedAt == nil {
					t.Errorf("closed_at should be set after COMPLETED transition")
				}
			}
		})
	}
}
