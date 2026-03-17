package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// mockDB is a mock database for testing export functionality
type mockDB struct {
	tasks   []models.Task
	sprints []models.Sprint
	audit   []models.AuditEntry
	name    string
}

// Ensure mockDB implements Database interface
var _ Database = (*mockDB)(nil)

func (m *mockDB) ListTasks(status *models.TaskStatus, minPriority, minSeverity *int, limit *int) ([]models.Task, error) {
	return m.tasks, nil
}

func (m *mockDB) ListSprints(status *models.SprintStatus) ([]models.Sprint, error) {
	return m.sprints, nil
}

func (m *mockDB) GetAuditEntries(operation, entityType *string, entityID *int, from, to *string, limit, offset int) ([]models.AuditEntry, error) {
	return m.audit, nil
}

func (m *mockDB) CreateTask(task *models.Task) (int, error) {
	task.ID = len(m.tasks) + 1
	m.tasks = append(m.tasks, *task)
	return task.ID, nil
}

func (m *mockDB) CreateSprint(sprint *models.Sprint) (int, error) {
	sprint.ID = len(m.sprints) + 1
	m.sprints = append(m.sprints, *sprint)
	return sprint.ID, nil
}

func (m *mockDB) LogAuditEntry(entry *models.AuditEntry) (int, error) {
	entry.ID = len(m.audit) + 1
	m.audit = append(m.audit, *entry)
	return entry.ID, nil
}

func (m *mockDB) RoadmapName() string {
	return m.name
}

func (m *mockDB) Close() error {
	return nil
}

func newMockDB(name string) *mockDB {
	return &mockDB{
		tasks:   []models.Task{},
		sprints: []models.Sprint{},
		audit:   []models.AuditEntry{},
		name:    name,
	}
}

func TestExportRoadmap(t *testing.T) {
	mock := newMockDB("test-roadmap")

	// Add test tasks
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	task1 := &models.Task{
		ID:             1,
		Priority:       5,
		Severity:       3,
		Status:         models.StatusBacklog,
		Description:    "Test task 1",
		Action:         "Do something",
		ExpectedResult: "Something done",
		CreatedAt:      now,
	}
	task2 := &models.Task{
		ID:             2,
		Priority:       8,
		Severity:       7,
		Status:         models.StatusDoing,
		Description:    "Test task 2",
		Action:         "Do another thing",
		ExpectedResult: "Another thing done",
		CreatedAt:      now,
	}
	mock.CreateTask(task1)
	mock.CreateTask(task2)

	// Add test sprints
	sprint1 := &models.Sprint{
		ID:          1,
		Status:      models.SprintOpen,
		Description: "Sprint 1",
		CreatedAt:   now,
	}
	mock.CreateSprint(sprint1)

	// Add test audit entries
	entry1 := &models.AuditEntry{
		ID:          1,
		Operation:   "CREATE",
		EntityType:  "TASK",
		EntityID:    1,
		PerformedAt: now,
	}
	mock.LogAuditEntry(entry1)

	tests := []struct {
		name         string
		includeAudit bool
		wantTasks    int
		wantSprints  int
		wantAudit    int
	}{
		{
			name:         "export without audit",
			includeAudit: false,
			wantTasks:    2,
			wantSprints:  1,
			wantAudit:    0,
		},
		{
			name:         "export with audit",
			includeAudit: true,
			wantTasks:    2,
			wantSprints:  1,
			wantAudit:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			export, err := ExportRoadmap(mock, tt.includeAudit)
			if err != nil {
				t.Fatalf("ExportRoadmap() error = %v", err)
			}

			if export.Version != ExportVersion {
				t.Errorf("Version = %q, want %q", export.Version, ExportVersion)
			}

			if export.Roadmap != "test-roadmap" {
				t.Errorf("Roadmap = %q, want %q", export.Roadmap, "test-roadmap")
			}

			if len(export.Tasks) != tt.wantTasks {
				t.Errorf("Tasks count = %d, want %d", len(export.Tasks), tt.wantTasks)
			}

			if len(export.Sprints) != tt.wantSprints {
				t.Errorf("Sprints count = %d, want %d", len(export.Sprints), tt.wantSprints)
			}

			if len(export.Audit) != tt.wantAudit {
				t.Errorf("Audit count = %d, want %d", len(export.Audit), tt.wantAudit)
			}

			// Verify exported_at is set
			if export.ExportedAt == "" {
				t.Error("ExportedAt should not be empty")
			}
		})
	}
}

func TestExportToFile(t *testing.T) {
	mock := newMockDB("test-export-file")

	// Add test data
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	task := &models.Task{
		ID:             1,
		Priority:       5,
		Severity:       3,
		Status:         models.StatusBacklog,
		Description:    "Test task",
		Action:         "Do something",
		ExpectedResult: "Something done",
		CreatedAt:      now,
	}
	mock.CreateTask(task)

	// Create temp directory
	tempDir := t.TempDir()
	exportFile := filepath.Join(tempDir, "export.json")

	// Test export to file
	err := ExportToFile(mock, exportFile, false)
	if err != nil {
		t.Fatalf("ExportToFile() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(exportFile); os.IsNotExist(err) {
		t.Fatal("Export file was not created")
	}

	// Verify file content
	data, err := os.ReadFile(exportFile)
	if err != nil {
		t.Fatalf("Reading export file: %v", err)
	}

	var export RoadmapExport
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatalf("Parsing export file: %v", err)
	}

	if export.Version != ExportVersion {
		t.Errorf("Version = %q, want %q", export.Version, ExportVersion)
	}

	if len(export.Tasks) != 1 {
		t.Errorf("Tasks count = %d, want 1", len(export.Tasks))
	}

	// Verify file permissions (should be readable)
	info, err := os.Stat(exportFile)
	if err != nil {
		t.Fatalf("Stat export file: %v", err)
	}

	// File should be readable by owner
	mode := info.Mode().Perm()
	if mode&0400 == 0 {
		t.Error("Export file should be readable by owner")
	}
}

func TestValidateExport(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	tests := []struct {
		name    string
		export  *RoadmapExport
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid export",
			export: &RoadmapExport{
				Version:    "1.0",
				Roadmap:    "test",
				ExportedAt: now,
				Tasks: []models.Task{
					{
						ID:             1,
						Priority:       5,
						Severity:       3,
						Status:         models.StatusBacklog,
						Description:    "Test task",
						Action:         "Do something",
						ExpectedResult: "Result",
						CreatedAt:      now,
					},
				},
				Sprints: []models.Sprint{
					{
						ID:          1,
						Status:      models.SprintOpen,
						Description: "Test sprint",
						CreatedAt:   now,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing version",
			export: &RoadmapExport{
				Roadmap: "test",
			},
			wantErr: true,
			errMsg:  "version is required",
		},
		{
			name: "missing roadmap name",
			export: &RoadmapExport{
				Version: "1.0",
			},
			wantErr: true,
			errMsg:  "roadmap name is required",
		},
		{
			name: "task missing description",
			export: &RoadmapExport{
				Version: "1.0",
				Roadmap: "test",
				Tasks: []models.Task{
					{
						ID:             1,
						Action:         "Do something",
						ExpectedResult: "Result",
						Status:         models.StatusBacklog,
					},
				},
			},
			wantErr: true,
			errMsg:  "description is required",
		},
		{
			name: "task missing action",
			export: &RoadmapExport{
				Version: "1.0",
				Roadmap: "test",
				Tasks: []models.Task{
					{
						ID:             1,
						Description:    "Test task",
						ExpectedResult: "Result",
						Status:         models.StatusBacklog,
					},
				},
			},
			wantErr: true,
			errMsg:  "action is required",
		},
		{
			name: "task missing expected_result",
			export: &RoadmapExport{
				Version: "1.0",
				Roadmap: "test",
				Tasks: []models.Task{
					{
						ID:          1,
						Description: "Test task",
						Action:      "Do something",
						Status:      models.StatusBacklog,
					},
				},
			},
			wantErr: true,
			errMsg:  "expected_result is required",
		},
		{
			name: "task invalid status",
			export: &RoadmapExport{
				Version: "1.0",
				Roadmap: "test",
				Tasks: []models.Task{
					{
						ID:             1,
						Description:    "Test task",
						Action:         "Do something",
						ExpectedResult: "Result",
						Status:         "INVALID_STATUS",
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid status",
		},
		{
			name: "sprint missing description",
			export: &RoadmapExport{
				Version: "1.0",
				Roadmap: "test",
				Sprints: []models.Sprint{
					{
						ID:     1,
						Status: models.SprintOpen,
					},
				},
			},
			wantErr: true,
			errMsg:  "description is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExport(tt.export)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateExport() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateExport() error message = %q, should contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestGetExportStats(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	export := &RoadmapExport{
		Version:    "1.0",
		ExportedAt: now,
		Roadmap:    "test-roadmap",
		Tasks: []models.Task{
			{ID: 1, Description: "Task 1"},
			{ID: 2, Description: "Task 2"},
		},
		Sprints: []models.Sprint{
			{ID: 1, Description: "Sprint 1"},
		},
		Audit: []models.AuditEntry{
			{ID: 1, Operation: "CREATE"},
			{ID: 2, Operation: "UPDATE"},
			{ID: 3, Operation: "DELETE"},
		},
	}

	stats := GetExportStats(export)

	if stats["version"] != "1.0" {
		t.Errorf("version = %v, want 1.0", stats["version"])
	}

	if stats["roadmap"] != "test-roadmap" {
		t.Errorf("roadmap = %v, want test-roadmap", stats["roadmap"])
	}

	if stats["tasks"] != 2 {
		t.Errorf("tasks = %v, want 2", stats["tasks"])
	}

	if stats["sprints"] != 1 {
		t.Errorf("sprints = %v, want 1", stats["sprints"])
	}

	if stats["audit"] != 3 {
		t.Errorf("audit = %v, want 3", stats["audit"])
	}

	if stats["exported_at"] != now {
		t.Errorf("exported_at = %v, want %v", stats["exported_at"], now)
	}
}

func TestImportRoadmap(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	tests := []struct {
		name        string
		export      *RoadmapExport
		wantTasks   int
		wantSprints int
		wantAudit   int
		wantErr     bool
	}{
		{
			name: "valid import",
			export: &RoadmapExport{
				Version: "1.0",
				Roadmap: "imported-roadmap",
				Tasks: []models.Task{
					{
						ID:             1,
						Priority:       5,
						Severity:       3,
						Status:         models.StatusBacklog,
						Description:    "Imported task",
						Action:         "Do something",
						ExpectedResult: "Result",
						CreatedAt:      now,
					},
				},
				Sprints: []models.Sprint{
					{
						ID:          1,
						Status:      models.SprintOpen,
						Description: "Imported sprint",
						CreatedAt:   now,
					},
				},
				Audit: []models.AuditEntry{
					{
						ID:          1,
						Operation:   "CREATE",
						EntityType:  "TASK",
						EntityID:    1,
						PerformedAt: now,
					},
				},
			},
			wantTasks:   1,
			wantSprints: 1,
			wantAudit:   1,
			wantErr:     false,
		},
		{
			name: "import without audit",
			export: &RoadmapExport{
				Version: "1.0",
				Roadmap: "no-audit-roadmap",
				Tasks: []models.Task{
					{
						ID:             1,
						Priority:       5,
						Severity:       3,
						Status:         models.StatusBacklog,
						Description:    "Task without audit",
						Action:         "Do something",
						ExpectedResult: "Result",
						CreatedAt:      now,
					},
				},
				Sprints: []models.Sprint{
					{
						ID:          1,
						Status:      models.SprintOpen,
						Description: "Sprint without audit",
						CreatedAt:   now,
					},
				},
				Audit: []models.AuditEntry{},
			},
			wantTasks:   1,
			wantSprints: 1,
			wantAudit:   0,
			wantErr:     false,
		},
		{
			name: "import with invalid task",
			export: &RoadmapExport{
				Version: "1.0",
				Roadmap: "invalid-task-roadmap",
				Tasks: []models.Task{
					{
						ID:          1,
						Status:      models.StatusBacklog,
						Description: "Invalid task - missing action",
						// Missing Action and ExpectedResult
						CreatedAt: now,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockDB("test-import")

			err := ImportRoadmap(mock, tt.export)
			if (err != nil) != tt.wantErr {
				t.Errorf("ImportRoadmap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if len(mock.tasks) != tt.wantTasks {
				t.Errorf("Tasks count = %d, want %d", len(mock.tasks), tt.wantTasks)
			}

			if len(mock.sprints) != tt.wantSprints {
				t.Errorf("Sprints count = %d, want %d", len(mock.sprints), tt.wantSprints)
			}

			if len(mock.audit) != tt.wantAudit {
				t.Errorf("Audit count = %d, want %d", len(mock.audit), tt.wantAudit)
			}
		})
	}
}

func TestImportFromFile(t *testing.T) {
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// Create temp directory
	tempDir := t.TempDir()

	// Create a valid export file
	exportData := RoadmapExport{
		Version:    "1.0",
		ExportedAt: now,
		Roadmap:    "file-import-test",
		Tasks: []models.Task{
			{
				ID:             1,
				Priority:       5,
				Severity:       3,
				Status:         models.StatusBacklog,
				Description:    "File imported task",
				Action:         "Do something",
				ExpectedResult: "Result",
				CreatedAt:      now,
			},
		},
		Sprints: []models.Sprint{
			{
				ID:          1,
				Status:      models.SprintOpen,
				Description: "File imported sprint",
				CreatedAt:   now,
			},
		},
	}

	data, _ := json.MarshalIndent(exportData, "", "  ")
	validFile := filepath.Join(tempDir, "valid_export.json")
	os.WriteFile(validFile, data, 0600)

	// Create an invalid JSON file
	invalidFile := filepath.Join(tempDir, "invalid.json")
	os.WriteFile(invalidFile, []byte("not valid json"), 0600)

	tests := []struct {
		name    string
		file    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid file",
			file:    validFile,
			wantErr: false,
		},
		{
			name:    "non-existent file",
			file:    filepath.Join(tempDir, "nonexistent.json"),
			wantErr: true,
			errMsg:  "reading import file",
		},
		{
			name:    "invalid JSON",
			file:    invalidFile,
			wantErr: true,
			errMsg:  "parsing import file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockDB("test-file-import")

			err := ImportFromFile(mock, tt.file)
			if (err != nil) != tt.wantErr {
				t.Errorf("ImportFromFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("ImportFromFile() error message = %q, should contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
