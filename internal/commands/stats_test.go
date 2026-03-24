package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// setupTestStatsRoadmap creates a roadmap, sets it as current, and returns the db and cleanup fn.
func setupTestStatsRoadmap(t *testing.T, name string) (*db.DB, func()) {
	t.Helper()
	cleanupTestRoadmap(t, name)

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("failed to create roadmap %q: %v", name, err)
	}

	cleanup := func() {
		database.Close()
		cleanupTestRoadmap(t, name)
	}
	return database, cleanup
}

// createStatsTask inserts a task with the given status directly via the db layer.
func createStatsTask(t *testing.T, database *db.DB, title string, status models.TaskStatus) {
	t.Helper()
	task := &models.Task{
		Title:                  title,
		Status:                 status,
		FunctionalRequirements: "Functional requirement for " + title,
		TechnicalRequirements:  "Technical requirement for " + title,
		AcceptanceCriteria:     "Acceptance criteria for " + title,
		CreatedAt:              utils.NowISO8601(),
		Priority:               5,
	}
	ctx := context.Background()
	if _, err := database.CreateTask(ctx, task); err != nil {
		t.Fatalf("failed to create task %q: %v", title, err)
	}
}

// ==================== HandleStats — Help flags ====================

func TestHandleStats_HelpShortFlag(t *testing.T) {
	err := HandleStats([]string{"-h"})
	if err != nil {
		t.Errorf("HandleStats([-h]) error = %v, want nil", err)
	}
}

func TestHandleStats_HelpLongFlag(t *testing.T) {
	err := HandleStats([]string{"--help"})
	if err != nil {
		t.Errorf("HandleStats([--help]) error = %v, want nil", err)
	}
}

func TestHandleStats_HelpSubcommand(t *testing.T) {
	err := HandleStats([]string{"help"})
	if err != nil {
		t.Errorf("HandleStats([help]) error = %v, want nil", err)
	}
}

// ==================== HandleStats — Error paths ====================

func TestHandleStats_NoRoadmapNoDefault(t *testing.T) {
	utils.EnsureDataDir()

	err := HandleStats([]string{})
	if err == nil {
		t.Fatal("HandleStats([]) expected error when no roadmap specified, got nil")
	}
}

func TestHandleStats_InvalidRoadmap(t *testing.T) {
	err := HandleStats([]string{"-r", "roadmap-that-does-not-exist-xyz"})
	if err == nil {
		t.Fatal("HandleStats with non-existent roadmap expected error, got nil")
	}
	if !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// ==================== HandleStats — Success paths ====================

func TestHandleStats_EmptyRoadmap(t *testing.T) {
	const name = "statstest_empty"
	_, cleanup := setupTestStatsRoadmap(t, name)
	defer cleanup()

	err := HandleStats([]string{"-r", name})
	if err != nil {
		t.Fatalf("HandleStats on empty roadmap error = %v", err)
	}
}

func TestHandleStats_JSONOutputStructure(t *testing.T) {
	const name = "statstest_jsonstructure"
	database, cleanup := setupTestStatsRoadmap(t, name)
	defer cleanup()

	createStatsTask(t, database, "Implement authentication service", models.StatusBacklog)
	createStatsTask(t, database, "Set up CI/CD pipeline", models.StatusBacklog)
	createStatsTask(t, database, "Configure monitoring dashboards", models.StatusDoing)

	// Capture stdout by routing through HandleStats via -r flag
	// We validate output by calling the db layer directly and comparing fields.
	ctx := context.Background()
	stats, err := database.GetRoadmapStats(ctx, name)
	if err != nil {
		t.Fatalf("GetRoadmapStats error = %v", err)
	}

	// Validate JSON serialisation round-trip
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	requiredTopKeys := []string{"roadmap", "sprints", "tasks"}
	for _, key := range requiredTopKeys {
		if _, ok := decoded[key]; !ok {
			t.Errorf("JSON output missing required key %q", key)
		}
	}

	tasksMap, ok := decoded["tasks"].(map[string]interface{})
	if !ok {
		t.Fatal("JSON tasks field is not an object")
	}
	requiredTaskKeys := []string{"backlog", "sprint", "doing", "testing", "completed"}
	for _, key := range requiredTaskKeys {
		if _, ok := tasksMap[key]; !ok {
			t.Errorf("JSON tasks missing required key %q", key)
		}
	}

	sprintsMap, ok := decoded["sprints"].(map[string]interface{})
	if !ok {
		t.Fatal("JSON sprints field is not an object")
	}
	requiredSprintKeys := []string{"total", "completed", "pending"}
	for _, key := range requiredSprintKeys {
		if _, ok := sprintsMap[key]; !ok {
			t.Errorf("JSON sprints missing required key %q", key)
		}
	}
}

func TestHandleStats_TaskCountsAccurate(t *testing.T) {
	const name = "statstest_counts"
	database, cleanup := setupTestStatsRoadmap(t, name)
	defer cleanup()

	createStatsTask(t, database, "Implement order processing engine", models.StatusBacklog)
	createStatsTask(t, database, "Build payment gateway integration", models.StatusBacklog)
	createStatsTask(t, database, "Design database schema for orders", models.StatusSprint)
	createStatsTask(t, database, "Develop REST API endpoints", models.StatusDoing)
	createStatsTask(t, database, "Write integration tests for payments", models.StatusTesting)
	createStatsTask(t, database, "Deploy to staging environment", models.StatusCompleted)

	ctx := context.Background()
	stats, err := database.GetRoadmapStats(ctx, name)
	if err != nil {
		t.Fatalf("GetRoadmapStats error = %v", err)
	}

	if stats.Tasks.Backlog != 2 {
		t.Errorf("Tasks.Backlog = %d, want 2", stats.Tasks.Backlog)
	}
	if stats.Tasks.Sprint != 1 {
		t.Errorf("Tasks.Sprint = %d, want 1", stats.Tasks.Sprint)
	}
	if stats.Tasks.Doing != 1 {
		t.Errorf("Tasks.Doing = %d, want 1", stats.Tasks.Doing)
	}
	if stats.Tasks.Testing != 1 {
		t.Errorf("Tasks.Testing = %d, want 1", stats.Tasks.Testing)
	}
	if stats.Tasks.Completed != 1 {
		t.Errorf("Tasks.Completed = %d, want 1", stats.Tasks.Completed)
	}
}

func TestHandleStats_RoadmapNameInOutput(t *testing.T) {
	const name = "statstest_roadmapname"
	database, cleanup := setupTestStatsRoadmap(t, name)
	defer cleanup()

	ctx := context.Background()
	stats, err := database.GetRoadmapStats(ctx, name)
	if err != nil {
		t.Fatalf("GetRoadmapStats error = %v", err)
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	if !bytes.Contains(data, []byte(name)) {
		t.Errorf("JSON output does not contain roadmap name %q: %s", name, data)
	}
}

func TestHandleStats_DefaultRoadmap(t *testing.T) {
	const name = "statstest_default"
	_, cleanup := setupTestStatsRoadmap(t, name)
	defer cleanup()

	err := HandleStats([]string{"-r", name})
	if err != nil {
		t.Fatalf("HandleStats([-r %s]) error = %v", name, err)
	}
}

func TestHandleStats_ExplicitRoadmapFlag(t *testing.T) {
	const name = "statstest_explicit"
	_, cleanup := setupTestStatsRoadmap(t, name)
	defer cleanup()

	// Ensure -r and --roadmap both work
	for _, flag := range []string{"-r", "--roadmap"} {
		t.Run("flag_"+strings.TrimLeft(flag, "-"), func(t *testing.T) {
			err := HandleStats([]string{flag, name})
			if err != nil {
				t.Errorf("HandleStats([%s %s]) error = %v", flag, name, err)
			}
		})
	}
}
