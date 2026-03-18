// Package export provides JSON export and import functionality for roadmaps.
package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

const (
	// ExportVersion is the current export format version.
	ExportVersion = "1.0"

	// DefaultExportFilename is the default filename for exports.
	DefaultExportFilename = "{name}_export.json"
)

// RoadmapExport represents the complete export structure.
type RoadmapExport struct {
	Metadata ExportMetadata      `json:"metadata"`
	Tasks    []models.Task       `json:"tasks"`
	Sprints  []models.Sprint     `json:"sprints"`
	Audit    []models.AuditEntry `json:"audit,omitempty"`
	// SprintTasks maps sprint IDs to their task IDs
	SprintTasks map[int][]int `json:"sprint_tasks"`
}

// ExportMetadata contains information about the export.
type ExportMetadata struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	ExportedAt  string `json:"exported_at"` // ISO 8601 UTC
	TaskCount   int    `json:"task_count"`
	SprintCount int    `json:"sprint_count"`
	AuditCount  int    `json:"audit_count"`
}

// Export exports a roadmap to JSON format.
// If outputPath is empty, a default filename is generated.
func Export(roadmapName string, outputPath string, includeAudit bool) (string, error) {
	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return "", err
	}

	// Check if roadmap exists
	exists, err := utils.RoadmapExists(roadmapName)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("%w: roadmap %q", utils.ErrNotFound, roadmapName)
	}

	// Open database
	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return "", fmt.Errorf("opening roadmap: %w", err)
	}
	defer database.Close()

	// Export tasks
	ctx, cancel := db.WithDefaultTimeout()
	tasks, _, err := database.ListTasks(ctx, nil, nil, nil, nil, nil, nil, 1, 100000)
	cancel()
	if err != nil {
		return "", fmt.Errorf("exporting tasks: %w", err)
	}

	// Export sprints
	ctx, cancel = db.WithDefaultTimeout()
	sprints, err := database.ListSprints(ctx, nil)
	cancel()
	if err != nil {
		return "", fmt.Errorf("exporting sprints: %w", err)
	}

	// Get sprint tasks relationships
	sprintTasks := make(map[int][]int)
	for _, sprint := range sprints {
		ctx, cancel = db.WithDefaultTimeout()
		taskIDs, err := database.GetSprintTasks(ctx, sprint.ID)
		cancel()
		if err != nil {
			return "", fmt.Errorf("getting sprint tasks: %w", err)
		}
		sprintTasks[sprint.ID] = taskIDs
	}

	// Build export structure
	export := RoadmapExport{
		Metadata: ExportMetadata{
			Name:        roadmapName,
			Version:     ExportVersion,
			ExportedAt:  utils.NowISO8601(),
			TaskCount:   len(tasks),
			SprintCount: len(sprints),
		},
		Tasks:       tasks,
		Sprints:     sprints,
		SprintTasks: sprintTasks,
	}

	// Export audit if requested
	if includeAudit {
		ctx, cancel = db.WithDefaultTimeout()
		auditEntries, err := database.GetAuditEntries(ctx, nil, nil, nil, nil, nil, 0, 0)
		cancel()
		if err != nil {
			return "", fmt.Errorf("exporting audit: %w", err)
		}
		export.Audit = auditEntries
		export.Metadata.AuditCount = len(auditEntries)
	}

	// Generate output path if not provided
	if outputPath == "" {
		outputPath = fmt.Sprintf("%s_export_%s.json", roadmapName, time.Now().UTC().Format("20060102_150405"))
	}

	// Ensure absolute path
	if !filepath.IsAbs(outputPath) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getting working directory: %w", err)
		}
		outputPath = filepath.Join(cwd, outputPath)
	}

	// Create output file
	file, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("creating output file: %w", err)
	}
	defer file.Close()

	// Encode to JSON with indentation
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(export); err != nil {
		return "", fmt.Errorf("encoding JSON: %w", err)
	}

	return outputPath, nil
}

// Import imports a roadmap from JSON format.
// If targetName is empty, the original roadmap name from the export is used.
func Import(inputPath string, targetName string) error {
	// Validate input path
	if inputPath == "" {
		return fmt.Errorf("%w: input file path required", utils.ErrRequired)
	}

	// Check if file exists
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		return fmt.Errorf("%w: file %q", utils.ErrNotFound, inputPath)
	}

	// Read and parse JSON file
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading input file: %w", err)
	}

	var export RoadmapExport
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	// Validate export structure
	if err := validateExport(&export); err != nil {
		return fmt.Errorf("validating export: %w", err)
	}

	// Determine target name
	if targetName == "" {
		targetName = export.Metadata.Name
	}

	// Validate target name
	if err := utils.ValidateRoadmapName(targetName); err != nil {
		return err
	}

	// Check if target already exists
	exists, err := utils.RoadmapExists(targetName)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: roadmap %q already exists, remove it first or use a different name", utils.ErrAlreadyExists, targetName)
	}

	// Create new database
	database, err := db.Open(targetName)
	if err != nil {
		return fmt.Errorf("creating roadmap: %w", err)
	}
	defer database.Close()

	// Import tasks and track ID mappings
	// Map old task IDs to new task IDs
	taskIDMap := make(map[int]int)

	for _, task := range export.Tasks {
		oldID := task.ID
		task.ID = 0 // Reset ID to get auto-assigned

		// Validate task data
		if err := task.Validate(); err != nil {
			return fmt.Errorf("invalid task data: %w", err)
		}

		ctx, cancel := db.WithDefaultTimeout()
		newID, err := database.CreateTask(ctx, &task)
		cancel()
		if err != nil {
			return fmt.Errorf("importing task: %w", err)
		}
		taskIDMap[oldID] = newID
	}

	// Import sprints and rebuild sprint-task relationships
	sprintIDMap := make(map[int]int)

	for _, sprint := range export.Sprints {
		oldID := sprint.ID
		sprint.ID = 0 // Reset ID to get auto-assigned

		// Clear computed fields
		sprint.Tasks = nil
		sprint.TaskCount = 0

		// Validate sprint data
		if err := sprint.Validate(); err != nil {
			return fmt.Errorf("invalid sprint data: %w", err)
		}

		ctx, cancel := db.WithDefaultTimeout()
		newID, err := database.CreateSprint(ctx, &sprint)
		cancel()
		if err != nil {
			return fmt.Errorf("importing sprint: %w", err)
		}
		sprintIDMap[oldID] = newID

		// Rebuild sprint-task relationships
		if oldTaskIDs, ok := export.SprintTasks[oldID]; ok {
			var newTaskIDs []int
			for _, oldTaskID := range oldTaskIDs {
				if newTaskID, ok := taskIDMap[oldTaskID]; ok {
					newTaskIDs = append(newTaskIDs, newTaskID)
				}
			}

			if len(newTaskIDs) > 0 {
				ctx, cancel = db.WithDefaultTimeout()
				err = database.AddTasksToSprint(ctx, newID, newTaskIDs)
				cancel()
				if err != nil {
					return fmt.Errorf("adding tasks to sprint: %w", err)
				}
			}
		}
	}

	// Import audit entries if present
	if len(export.Audit) > 0 {
		for _, entry := range export.Audit {
			// Map entity IDs
			if entry.EntityType == string(models.EntityTask) {
				if newID, ok := taskIDMap[entry.EntityID]; ok {
					entry.EntityID = newID
				} else {
					// Skip entries for tasks that weren't imported
					continue
				}
			} else if entry.EntityType == string(models.EntitySprint) {
				if newID, ok := sprintIDMap[entry.EntityID]; ok {
					entry.EntityID = newID
				} else {
					// Skip entries for sprints that weren't imported
					continue
				}
			}

			// Validate audit entry
			if err := entry.Validate(); err != nil {
				return fmt.Errorf("invalid audit entry: %w", err)
			}

			ctx, cancel := db.WithDefaultTimeout()
			_, err := database.LogAuditEntry(ctx, &entry)
			cancel()
			if err != nil {
				return fmt.Errorf("importing audit entry: %w", err)
			}
		}
	}

	return nil
}

// validateExport validates the export structure.
func validateExport(export *RoadmapExport) error {
	// Check required fields
	if export.Metadata.Name == "" {
		return fmt.Errorf("missing metadata.name")
	}
	if export.Metadata.Version == "" {
		return fmt.Errorf("missing metadata.version")
	}
	if export.Metadata.ExportedAt == "" {
		return fmt.Errorf("missing metadata.exported_at")
	}

	// Validate version (only 1.0 supported currently)
	if export.Metadata.Version != ExportVersion {
		return fmt.Errorf("unsupported export version: %s (expected %s)", export.Metadata.Version, ExportVersion)
	}

	// Validate tasks
	for i, task := range export.Tasks {
		if err := task.Validate(); err != nil {
			return fmt.Errorf("task[%d]: %w", i, err)
		}
	}

	// Validate sprints
	for i, sprint := range export.Sprints {
		if err := sprint.Validate(); err != nil {
			return fmt.Errorf("sprint[%d]: %w", i, err)
		}
	}

	return nil
}

// ValidateExportFile validates an export file without importing it.
// Returns the export metadata if valid.
func ValidateExportFile(inputPath string) (*ExportMetadata, error) {
	// Read file
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	// Parse JSON
	var export RoadmapExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	// Validate structure
	if err := validateExport(&export); err != nil {
		return nil, err
	}

	return &export.Metadata, nil
}
