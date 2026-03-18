// Package export provides JSON export and import functionality for roadmaps.
package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== EXPORT TESTS ====================

func TestExport_Success(t *testing.T) {
	// Create a test roadmap with some data
	roadmapName := "testexport" + time.Now().Format("150405")

	database, err := db.Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}

	// Create a task
	ctx, cancel := db.WithDefaultTimeout()
	_, err = database.CreateTask(ctx, &models.Task{
		Priority:       5,
		Severity:       3,
		Status:         models.StatusBacklog,
		Description:    "Test task",
		Action:         "Test action",
		ExpectedResult: "Test result",
		CreatedAt:      utils.NowISO8601(),
	})
	cancel()
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Create a sprint
	ctx, cancel = db.WithDefaultTimeout()
	_, err = database.CreateSprint(ctx, &models.Sprint{
		Status:      models.SprintPending,
		Description: "Test sprint",
		CreatedAt:   utils.NowISO8601(),
	})
	cancel()
	if err != nil {
		t.Fatalf("failed to create sprint: %v", err)
	}

	database.Close()

	// Cleanup after test
	defer func() {
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
	}()

	// Export roadmap
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "export.json")

	resultPath, err := Export(roadmapName, outputPath, false)
	if err != nil {
		t.Fatalf("failed to export: %v", err)
	}

	if resultPath != outputPath {
		t.Errorf("expected output path %q, got %q", outputPath, resultPath)
	}

	// Verify file exists
	if _, err := os.Stat(resultPath); os.IsNotExist(err) {
		t.Errorf("export file was not created: %s", resultPath)
	}

	// Verify JSON structure
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("failed to read export file: %v", err)
	}

	var export RoadmapExport
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatalf("failed to parse export JSON: %v", err)
	}

	// Verify metadata
	if export.Metadata.Name != roadmapName {
		t.Errorf("expected roadmap name %q, got %q", roadmapName, export.Metadata.Name)
	}
	if export.Metadata.Version != ExportVersion {
		t.Errorf("expected version %q, got %q", ExportVersion, export.Metadata.Version)
	}
	if export.Metadata.TaskCount != 1 {
		t.Errorf("expected task count 1, got %d", export.Metadata.TaskCount)
	}
	if export.Metadata.SprintCount != 1 {
		t.Errorf("expected sprint count 1, got %d", export.Metadata.SprintCount)
	}

	// Verify tasks
	if len(export.Tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(export.Tasks))
	}

	// Verify sprints
	if len(export.Sprints) != 1 {
		t.Errorf("expected 1 sprint, got %d", len(export.Sprints))
	}
}

func TestExport_RoadmapNotFound(t *testing.T) {
	_, err := Export("nonexistentroadmap12345", "", false)
	if err == nil {
		t.Error("expected error for non-existent roadmap")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestExport_DefaultFilename(t *testing.T) {
	// Create a test roadmap
	roadmapName := "testexportdefault" + time.Now().Format("150405")

	database, err := db.Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	database.Close()

	// Cleanup after test
	defer func() {
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
	}()

	// Export without specifying output path
	tempDir := t.TempDir()
	os.Chdir(tempDir)

	resultPath, err := Export(roadmapName, "", false)
	if err != nil {
		t.Fatalf("failed to export: %v", err)
	}

	// Verify file was created with default name
	if !strings.HasPrefix(filepath.Base(resultPath), roadmapName) {
		t.Errorf("expected default filename to start with %q, got %q", roadmapName, filepath.Base(resultPath))
	}
	if !strings.HasSuffix(resultPath, ".json") {
		t.Errorf("expected .json extension, got %q", resultPath)
	}
}

// ==================== IMPORT TESTS ====================

func TestImport_Success(t *testing.T) {
	// Create a test roadmap
	roadmapName := "testimport" + time.Now().Format("150405")
	targetName := roadmapName + "_imported"

	database, err := db.Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}

	// Create a task
	ctx, cancel := db.WithDefaultTimeout()
	_, err = database.CreateTask(ctx, &models.Task{
		Priority:       5,
		Severity:       3,
		Status:         models.StatusBacklog,
		Description:    "Test task",
		Action:         "Test action",
		ExpectedResult: "Test result",
		CreatedAt:      utils.NowISO8601(),
	})
	cancel()
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	database.Close()

	// Cleanup after test
	defer func() {
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
		if path, err := utils.GetRoadmapPath(targetName); err == nil {
			os.Remove(path)
		}
	}()

	// Export roadmap
	tempDir := t.TempDir()
	exportPath := filepath.Join(tempDir, "export.json")

	_, err = Export(roadmapName, exportPath, false)
	if err != nil {
		t.Fatalf("failed to export: %v", err)
	}

	// Import with new name
	err = Import(exportPath, targetName)
	if err != nil {
		t.Fatalf("failed to import: %v", err)
	}

	// Verify imported roadmap exists
	exists, err := utils.RoadmapExists(targetName)
	if err != nil {
		t.Fatalf("failed to check imported roadmap: %v", err)
	}
	if !exists {
		t.Error("imported roadmap does not exist")
	}

	// Verify imported data
	importedDB, err := db.OpenExisting(targetName)
	if err != nil {
		t.Fatalf("failed to open imported roadmap: %v", err)
	}
	defer importedDB.Close()

	ctx, cancel = db.WithDefaultTimeout()
	tasks, _, err := importedDB.ListTasks(ctx, nil, nil, nil, nil, nil, nil, 1, 100)
	cancel()
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task in imported roadmap, got %d", len(tasks))
	}
}

func TestImport_FileNotFound(t *testing.T) {
	err := Import("/nonexistent/path/export.json", "target")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestImport_InvalidJSON(t *testing.T) {
	// Create invalid JSON file
	tempDir := t.TempDir()
	invalidPath := filepath.Join(tempDir, "invalid.json")

	if err := os.WriteFile(invalidPath, []byte("not valid json"), 0600); err != nil {
		t.Fatalf("failed to create invalid JSON file: %v", err)
	}

	err := Import(invalidPath, "target")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("expected 'parsing' error, got: %v", err)
	}
}

func TestImport_TargetExists(t *testing.T) {
	// Create a test roadmap
	roadmapName := "testimportexists" + time.Now().Format("150405")

	database, err := db.Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}
	database.Close()

	// Cleanup after test
	defer func() {
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
	}()

	// Export roadmap
	tempDir := t.TempDir()
	exportPath := filepath.Join(tempDir, "export.json")

	_, err = Export(roadmapName, exportPath, false)
	if err != nil {
		t.Fatalf("failed to export: %v", err)
	}

	// Try to import with same name (should fail)
	err = Import(exportPath, roadmapName)
	if err == nil {
		t.Error("expected error when target already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

// ==================== VALIDATION TESTS ====================

func TestValidateExport(t *testing.T) {
	tests := []struct {
		name      string
		export    RoadmapExport
		wantError bool
	}{
		{
			name: "valid export",
			export: RoadmapExport{
				Metadata: ExportMetadata{
					Name:        "test",
					Version:     ExportVersion,
					ExportedAt:  utils.NowISO8601(),
					TaskCount:   0,
					SprintCount: 0,
				},
			},
			wantError: false,
		},
		{
			name: "missing name",
			export: RoadmapExport{
				Metadata: ExportMetadata{
					Name:        "",
					Version:     ExportVersion,
					ExportedAt:  utils.NowISO8601(),
					TaskCount:   0,
					SprintCount: 0,
				},
			},
			wantError: true,
		},
		{
			name: "missing version",
			export: RoadmapExport{
				Metadata: ExportMetadata{
					Name:        "test",
					Version:     "",
					ExportedAt:  utils.NowISO8601(),
					TaskCount:   0,
					SprintCount: 0,
				},
			},
			wantError: true,
		},
		{
			name: "unsupported version",
			export: RoadmapExport{
				Metadata: ExportMetadata{
					Name:        "test",
					Version:     "2.0",
					ExportedAt:  utils.NowISO8601(),
					TaskCount:   0,
					SprintCount: 0,
				},
			},
			wantError: true,
		},
		{
			name: "missing exported_at",
			export: RoadmapExport{
				Metadata: ExportMetadata{
					Name:        "test",
					Version:     ExportVersion,
					ExportedAt:  "",
					TaskCount:   0,
					SprintCount: 0,
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExport(&tt.export)
			if tt.wantError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateExportFile(t *testing.T) {
	// Create valid export file
	tempDir := t.TempDir()
	validPath := filepath.Join(tempDir, "valid.json")

	export := RoadmapExport{
		Metadata: ExportMetadata{
			Name:        "test",
			Version:     ExportVersion,
			ExportedAt:  utils.NowISO8601(),
			TaskCount:   0,
			SprintCount: 0,
		},
	}

	data, _ := json.Marshal(export)
	os.WriteFile(validPath, data, 0600)

	// Test validation
	metadata, err := ValidateExportFile(validPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if metadata.Name != "test" {
		t.Errorf("expected name 'test', got %q", metadata.Name)
	}

	// Test with invalid file
	invalidPath := filepath.Join(tempDir, "invalid.json")
	os.WriteFile(invalidPath, []byte("not json"), 0600)

	_, err = ValidateExportFile(invalidPath)
	if err == nil {
		t.Error("expected error for invalid file")
	}

	// Test with non-existent file
	_, err = ValidateExportFile("/nonexistent/path.json")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

// ==================== ROUND-TRIP TEST ====================

func TestExportImportRoundTrip(t *testing.T) {
	// Create a test roadmap with data
	roadmapName := "testroundtrip" + time.Now().Format("150405")
	targetName := roadmapName + "_restored"

	database, err := db.Open(roadmapName)
	if err != nil {
		t.Fatalf("failed to create roadmap: %v", err)
	}

	// Create tasks
	tasks := []*models.Task{
		{
			Priority:       5,
			Severity:       3,
			Status:         models.StatusBacklog,
			Description:    "Task 1",
			Action:         "Action 1",
			ExpectedResult: "Result 1",
			CreatedAt:      utils.NowISO8601(),
		},
		{
			Priority:       3,
			Severity:       2,
			Status:         models.StatusDoing,
			Description:    "Task 2",
			Action:         "Action 2",
			ExpectedResult: "Result 2",
			CreatedAt:      utils.NowISO8601(),
		},
	}

	var taskIDs []int
	for _, task := range tasks {
		ctx, cancel := db.WithDefaultTimeout()
		id, err := database.CreateTask(ctx, task)
		cancel()
		if err != nil {
			t.Fatalf("failed to create task: %v", err)
		}
		taskIDs = append(taskIDs, id)
	}

	// Create sprint with tasks
	ctx, cancel := db.WithDefaultTimeout()
	sprintID, err := database.CreateSprint(ctx, &models.Sprint{
		Status:      models.SprintOpen,
		Description: "Test Sprint",
		CreatedAt:   utils.NowISO8601(),
	})
	cancel()
	if err != nil {
		t.Fatalf("failed to create sprint: %v", err)
	}

	// Add tasks to sprint
	ctx, cancel = db.WithDefaultTimeout()
	err = database.AddTasksToSprint(ctx, sprintID, taskIDs)
	cancel()
	if err != nil {
		t.Fatalf("failed to add tasks to sprint: %v", err)
	}

	database.Close()

	// Cleanup after test
	defer func() {
		if path, err := utils.GetRoadmapPath(roadmapName); err == nil {
			os.Remove(path)
		}
		if path, err := utils.GetRoadmapPath(targetName); err == nil {
			os.Remove(path)
		}
	}()

	// Export roadmap
	tempDir := t.TempDir()
	exportPath := filepath.Join(tempDir, "export.json")

	_, err = Export(roadmapName, exportPath, true)
	if err != nil {
		t.Fatalf("failed to export: %v", err)
	}

	// Import with new name
	err = Import(exportPath, targetName)
	if err != nil {
		t.Fatalf("failed to import: %v", err)
	}

	// Verify imported data
	importedDB, err := db.OpenExisting(targetName)
	if err != nil {
		t.Fatalf("failed to open imported roadmap: %v", err)
	}
	defer importedDB.Close()

	// Check tasks
	ctx, cancel = db.WithDefaultTimeout()
	importedTasks, _, err := importedDB.ListTasks(ctx, nil, nil, nil, nil, nil, nil, 1, 100)
	cancel()
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}
	if len(importedTasks) != len(tasks) {
		t.Errorf("expected %d tasks, got %d", len(tasks), len(importedTasks))
	}

	// Check sprints
	ctx, cancel = db.WithDefaultTimeout()
	importedSprints, err := importedDB.ListSprints(ctx, nil)
	cancel()
	if err != nil {
		t.Fatalf("failed to list sprints: %v", err)
	}
	if len(importedSprints) != 1 {
		t.Errorf("expected 1 sprint, got %d", len(importedSprints))
	}

	// Check sprint-task relationships
	ctx, cancel = db.WithDefaultTimeout()
	importedSprintTasks, err := importedDB.GetSprintTasks(ctx, importedSprints[0].ID)
	cancel()
	if err != nil {
		t.Fatalf("failed to get sprint tasks: %v", err)
	}
	if len(importedSprintTasks) != len(taskIDs) {
		t.Errorf("expected %d sprint tasks, got %d", len(taskIDs), len(importedSprintTasks))
	}
}
