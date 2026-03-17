// Package export provides export/import functionality for Groadmap data.
package export

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// Database defines the interface required for export/import operations.
// This interface is satisfied by *db.DB and can be mocked for testing.
type Database interface {
	ListTasks(status *models.TaskStatus, minPriority, minSeverity *int, limit *int) ([]models.Task, error)
	ListSprints(status *models.SprintStatus) ([]models.Sprint, error)
	GetAuditEntries(operation, entityType *string, entityID *int, since, until *string, limit, offset int) ([]models.AuditEntry, error)
	CreateTask(task *models.Task) (int, error)
	CreateSprint(sprint *models.Sprint) (int, error)
	LogAuditEntry(entry *models.AuditEntry) (int, error)
	RoadmapName() string
}

// RoadmapExport represents a complete roadmap export.
type RoadmapExport struct {
	Version    string              `json:"version"`
	ExportedAt string              `json:"exported_at"`
	Roadmap    string              `json:"roadmap"`
	Tasks      []models.Task       `json:"tasks"`
	Sprints    []models.Sprint     `json:"sprints"`
	Audit      []models.AuditEntry `json:"audit,omitempty"`
}

// ExportVersion is the current export format version.
const ExportVersion = "1.0"

// ExportRoadmap exports a complete roadmap to JSON.
func ExportRoadmap(database Database, includeAudit bool) (*RoadmapExport, error) {
	export := &RoadmapExport{
		Version:    ExportVersion,
		ExportedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Roadmap:    database.RoadmapName(),
	}

	// Export all tasks
	tasks, err := database.ListTasks(nil, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("exporting tasks: %w", err)
	}
	export.Tasks = tasks

	// Export all sprints
	sprints, err := database.ListSprints(nil)
	if err != nil {
		return nil, fmt.Errorf("exporting sprints: %w", err)
	}
	export.Sprints = sprints

	// Export audit if requested
	if includeAudit {
		audit, err := database.GetAuditEntries(nil, nil, nil, nil, nil, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("exporting audit: %w", err)
		}
		export.Audit = audit
	}

	return export, nil
}

// ExportToFile exports a roadmap to a JSON file.
func ExportToFile(database Database, filepath string, includeAudit bool) error {
	export, err := ExportRoadmap(database, includeAudit)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling export: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0600); err != nil {
		return fmt.Errorf("writing export file: %w", err)
	}

	return nil
}

// ImportRoadmap imports a roadmap from an export.
func ImportRoadmap(database Database, export *RoadmapExport) error {
	// Validate version
	if export.Version == "" {
		return fmt.Errorf("export version is required")
	}

	// Validate tasks
	for _, task := range export.Tasks {
		if err := task.Validate(); err != nil {
			return fmt.Errorf("invalid task in export: %w", err)
		}
	}

	// Import tasks
	for _, task := range export.Tasks {
		newTask := &models.Task{
			Priority:       task.Priority,
			Severity:       task.Severity,
			Status:         task.Status,
			Description:    task.Description,
			Specialists:    task.Specialists,
			Action:         task.Action,
			ExpectedResult: task.ExpectedResult,
			CreatedAt:      task.CreatedAt,
			CompletedAt:    task.CompletedAt,
		}

		_, err := database.CreateTask(newTask)
		if err != nil {
			return fmt.Errorf("importing task: %w", err)
		}
	}

	// Import sprints
	for _, sprint := range export.Sprints {
		newSprint := &models.Sprint{
			Status:      sprint.Status,
			Description: sprint.Description,
			CreatedAt:   sprint.CreatedAt,
			StartedAt:   sprint.StartedAt,
			ClosedAt:    sprint.ClosedAt,
		}

		_, err := database.CreateSprint(newSprint)
		if err != nil {
			return fmt.Errorf("importing sprint: %w", err)
		}
	}

	// Import audit if present
	for _, entry := range export.Audit {
		newEntry := &models.AuditEntry{
			Operation:   entry.Operation,
			EntityType:  entry.EntityType,
			EntityID:    entry.EntityID,
			PerformedAt: entry.PerformedAt,
		}

		_, err := database.LogAuditEntry(newEntry)
		if err != nil {
			// Log but don't fail - audit is optional
			continue
		}
	}

	return nil
}

// ImportFromFile imports a roadmap from a JSON file.
func ImportFromFile(database Database, filepath string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("reading import file: %w", err)
	}

	var export RoadmapExport
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("parsing import file: %w", err)
	}

	return ImportRoadmap(database, &export)
}

// ValidateExport validates an export structure.
func ValidateExport(export *RoadmapExport) error {
	if export.Version == "" {
		return fmt.Errorf("export version is required")
	}

	if export.Roadmap == "" {
		return fmt.Errorf("roadmap name is required")
	}

	// Validate tasks
	for i, task := range export.Tasks {
		if task.Description == "" {
			return fmt.Errorf("task %d: description is required", i)
		}
		if task.Action == "" {
			return fmt.Errorf("task %d: action is required", i)
		}
		if task.ExpectedResult == "" {
			return fmt.Errorf("task %d: expected_result is required", i)
		}
		if !models.IsValidTaskStatus(string(task.Status)) {
			return fmt.Errorf("task %d: invalid status %q", i, task.Status)
		}
	}

	// Validate sprints
	for i, sprint := range export.Sprints {
		if sprint.Description == "" {
			return fmt.Errorf("sprint %d: description is required", i)
		}
	}

	return nil
}

// GetExportStats returns statistics about an export.
func GetExportStats(export *RoadmapExport) map[string]interface{} {
	return map[string]interface{}{
		"version":     export.Version,
		"exported_at": export.ExportedAt,
		"roadmap":     export.Roadmap,
		"tasks":       len(export.Tasks),
		"sprints":     len(export.Sprints),
		"audit":       len(export.Audit),
	}
}
