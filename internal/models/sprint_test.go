package models

import (
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

func TestSprintValidate(t *testing.T) {
	tests := []struct {
		name    string
		sprint  Sprint
		wantErr bool
	}{
		{
			name: "valid sprint",
			sprint: Sprint{
				Title:       "Authentication hardening",
				Description: "Test Sprint",
				Status:      SprintPending,
				CreatedAt:   "2024-01-15T10:00:00.000Z",
				Order:       1,
			},
			wantErr: false,
		},
		{
			name: "empty description",
			sprint: Sprint{
				Title:       "Authentication hardening",
				Description: "",
				Status:      SprintPending,
				CreatedAt:   "2024-01-15T10:00:00.000Z",
			},
			wantErr: true,
		},
		{
			name: "description too long",
			sprint: Sprint{
				Title:       "Authentication hardening",
				Description: string(make([]byte, MaxSprintDescription+1)),
				Status:      SprintPending,
				CreatedAt:   "2024-01-15T10:00:00.000Z",
			},
			wantErr: true,
		},
		{
			name: "invalid status",
			sprint: Sprint{
				Title:       "Authentication hardening",
				Description: "Test Sprint",
				Status:      "INVALID",
				CreatedAt:   "2024-01-15T10:00:00.000Z",
			},
			wantErr: true,
		},
		{
			name: "invalid created_at date",
			sprint: Sprint{
				Title:       "Authentication hardening",
				Description: "Test Sprint",
				Status:      SprintPending,
				CreatedAt:   "invalid-date",
			},
			wantErr: true,
		},
		{
			name: "started_at before created_at",
			sprint: Sprint{
				Title:       "Authentication hardening",
				Description: "Test Sprint",
				Status:      SprintOpen,
				CreatedAt:   "2024-01-15T10:00:00.000Z",
				StartedAt:   strPtr("2024-01-14T10:00:00.000Z"),
			},
			wantErr: true,
		},
		{
			name: "closed_at before started_at",
			sprint: Sprint{
				Title:       "Authentication hardening",
				Description: "Test Sprint",
				Status:      SprintClosed,
				CreatedAt:   "2024-01-15T10:00:00.000Z",
				StartedAt:   strPtr("2024-01-15T10:00:00.000Z"),
				ClosedAt:    strPtr("2024-01-14T10:00:00.000Z"),
			},
			wantErr: true,
		},
		{
			name: "valid dates in order",
			sprint: Sprint{
				Title:       "Authentication hardening",
				Description: "Test Sprint",
				Status:      SprintClosed,
				CreatedAt:   "2024-01-15T10:00:00.000Z",
				StartedAt:   strPtr("2024-01-16T10:00:00.000Z"),
				ClosedAt:    strPtr("2024-01-17T10:00:00.000Z"),
				Order:       1,
			},
			wantErr: false,
		},
		{
			name: "closed_at before created_at (no started_at)",
			sprint: Sprint{
				Title:       "Authentication hardening",
				Description: "Test Sprint",
				Status:      SprintClosed,
				CreatedAt:   "2024-01-15T10:00:00.000Z",
				ClosedAt:    strPtr("2024-01-14T10:00:00.000Z"),
			},
			wantErr: true,
		},
		{
			name: "empty title",
			sprint: Sprint{
				Title:       "",
				Description: "Test Sprint",
				Status:      SprintPending,
				CreatedAt:   "2024-01-15T10:00:00.000Z",
			},
			wantErr: true,
		},
		{
			name: "title too long",
			sprint: Sprint{
				Title:       string(make([]byte, MaxSprintTitle+1)),
				Description: "Test Sprint",
				Status:      SprintPending,
				CreatedAt:   "2024-01-15T10:00:00.000Z",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sprint.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Sprint.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSprintStatusMethods(t *testing.T) {
	tests := []struct {
		name      string
		status    SprintStatus
		isOpen    bool
		isClosed  bool
		isPending bool
	}{
		{"pending", SprintPending, false, false, true},
		{"open", SprintOpen, true, false, false},
		{"closed", SprintClosed, false, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Sprint{Status: tt.status}
			if s.IsOpen() != tt.isOpen {
				t.Errorf("IsOpen() = %v, want %v", s.IsOpen(), tt.isOpen)
			}
			if s.IsClosed() != tt.isClosed {
				t.Errorf("IsClosed() = %v, want %v", s.IsClosed(), tt.isClosed)
			}
			if s.IsPending() != tt.isPending {
				t.Errorf("IsPending() = %v, want %v", s.IsPending(), tt.isPending)
			}
		})
	}
}

func TestSprintStatusTransitions(t *testing.T) {
	tests := []struct {
		name      string
		status    SprintStatus
		canStart  bool
		canClose  bool
		canReopen bool
	}{
		{"pending", SprintPending, true, false, false},
		{"open", SprintOpen, false, true, false},
		{"closed", SprintClosed, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.CanStart(); got != tt.canStart {
				t.Errorf("CanStart() = %v, want %v", got, tt.canStart)
			}
			if got := tt.status.CanClose(); got != tt.canClose {
				t.Errorf("CanClose() = %v, want %v", got, tt.canClose)
			}
			if got := tt.status.CanReopen(); got != tt.canReopen {
				t.Errorf("CanReopen() = %v, want %v", got, tt.canReopen)
			}
		})
	}
}

func TestParseSprintStatus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    SprintStatus
		wantErr bool
	}{
		{"PENDING", "PENDING", SprintPending, false},
		{"OPEN", "OPEN", SprintOpen, false},
		{"CLOSED", "CLOSED", SprintClosed, false},
		{"invalid", "INVALID", "", true},
		{"lowercase", "pending", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSprintStatus(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSprintStatus(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseSprintStatus(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCalculateSprintStats(t *testing.T) {
	tests := []struct {
		name           string
		tasks          []Task
		wantTotal      int
		wantCompleted  int
		wantProgress   float64
		wantStatusDist map[string]int
	}{
		{
			name:           "empty sprint",
			tasks:          []Task{},
			wantTotal:      0,
			wantCompleted:  0,
			wantProgress:   0,
			wantStatusDist: map[string]int{},
		},
		{
			name: "all completed",
			tasks: []Task{
				{Status: StatusCompleted},
				{Status: StatusCompleted},
				{Status: StatusCompleted},
			},
			wantTotal:      3,
			wantCompleted:  3,
			wantProgress:   100.0,
			wantStatusDist: map[string]int{"COMPLETED": 3},
		},
		{
			name: "mixed statuses",
			tasks: []Task{
				{Status: StatusBacklog},
				{Status: StatusSprint},
				{Status: StatusDoing},
				{Status: StatusTesting},
				{Status: StatusCompleted},
			},
			wantTotal:     5,
			wantCompleted: 1,
			wantProgress:  20.0,
			wantStatusDist: map[string]int{
				"BACKLOG":   1,
				"SPRINT":    1,
				"DOING":     1,
				"TESTING":   1,
				"COMPLETED": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := CalculateSprintStats(1, tt.tasks)

			if stats.TotalTasks != tt.wantTotal {
				t.Errorf("TotalTasks = %d, want %d", stats.TotalTasks, tt.wantTotal)
			}
			if stats.CompletedTasks != tt.wantCompleted {
				t.Errorf("CompletedTasks = %d, want %d", stats.CompletedTasks, tt.wantCompleted)
			}
			if stats.ProgressPercentage != tt.wantProgress {
				t.Errorf("ProgressPercentage = %f, want %f", stats.ProgressPercentage, tt.wantProgress)
			}
			for status, count := range tt.wantStatusDist {
				if stats.StatusDistribution[status] != count {
					t.Errorf("StatusDistribution[%s] = %d, want %d", status, stats.StatusDistribution[status], count)
				}
			}
		})
	}
}

func TestSprintValidateDates(t *testing.T) {
	now := utils.NowISO8601()
	yesterday := "2024-01-14T10:00:00.000Z"
	today := "2024-01-15T10:00:00.000Z"
	tomorrow := "2024-01-16T10:00:00.000Z"
	dayAfter := "2024-01-17T10:00:00.000Z"

	tests := []struct {
		name    string
		sprint  Sprint
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid - only created_at",
			sprint: Sprint{
				CreatedAt: yesterday,
			},
			wantErr: false,
		},
		{
			name: "valid - created and started",
			sprint: Sprint{
				CreatedAt: yesterday,
				StartedAt: strPtr(today),
			},
			wantErr: false,
		},
		{
			name: "valid - all dates in order",
			sprint: Sprint{
				CreatedAt: yesterday,
				StartedAt: strPtr(today),
				ClosedAt:  strPtr(tomorrow),
			},
			wantErr: false,
		},
		{
			name: "invalid - started before created",
			sprint: Sprint{
				CreatedAt: today,
				StartedAt: strPtr(yesterday),
			},
			wantErr: true,
			errMsg:  "started_at",
		},
		{
			name: "invalid - closed before started",
			sprint: Sprint{
				CreatedAt: yesterday,
				StartedAt: strPtr(tomorrow),
				ClosedAt:  strPtr(today),
			},
			wantErr: true,
			errMsg:  "closed_at",
		},
		{
			name: "invalid - closed before created (no started)",
			sprint: Sprint{
				CreatedAt: today,
				ClosedAt:  strPtr(yesterday),
			},
			wantErr: true,
			errMsg:  "closed_at",
		},
		{
			name: "invalid - same day is valid (created == started)",
			sprint: Sprint{
				CreatedAt: today,
				StartedAt: strPtr(today),
			},
			wantErr: false,
		},
		{
			name: "invalid - same day is valid (started == closed)",
			sprint: Sprint{
				CreatedAt: yesterday,
				StartedAt: strPtr(today),
				ClosedAt:  strPtr(today),
			},
			wantErr: false,
		},
		{
			name: "invalid - future created_at",
			sprint: Sprint{
				CreatedAt: "2099-01-15T10:00:00.000Z",
			},
			wantErr: true,
			errMsg:  "future",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sprint.validateDates()
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDates() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !containsStr(err.Error(), tt.errMsg) {
					t.Errorf("validateDates() error message = %q, should contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}

	// Suppress unused variable warning
	_ = now
	_ = dayAfter
}

// Helper function
func strPtr(s string) *string {
	return &s
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// mkStatusTask builds a Task with only the status set, sufficient for sprint
// summary categorisation tests (the summary counts by status only).
func mkStatusTask(status TaskStatus) Task {
	return Task{Status: status}
}

// TestCategorizeTaskStatus asserts the shared status-to-category mapping that
// both CalculateSprintShowResult and the web sprint summary depend on: pending =
// BACKLOG + SPRINT, in-progress = DOING + TESTING, completed = COMPLETED
// (SPEC/WEB.md § Shared Sprint Presentation Sub-Template).
func TestCategorizeTaskStatus(t *testing.T) {
	cases := []struct {
		status TaskStatus
		want   TaskStatusCategory
	}{
		{StatusBacklog, CategoryPending},
		{StatusSprint, CategoryPending},
		{StatusDoing, CategoryInProgress},
		{StatusTesting, CategoryInProgress},
		{StatusCompleted, CategoryCompleted},
		{TaskStatus("UNKNOWN"), CategoryOther},
	}
	for _, c := range cases {
		if got := CategorizeTaskStatus(c.status); got != c.want {
			t.Errorf("CategorizeTaskStatus(%q) = %d, want %d", c.status, got, c.want)
		}
	}
}

// TestCalculateSprintSummary asserts the per-bucket counts and totals over a
// representative status mix, and that the counts match the categorisation
// CalculateSprintShowResult produces over the same tasks (the two must never
// diverge).
func TestCalculateSprintSummary(t *testing.T) {
	tasks := []Task{
		mkStatusTask(StatusBacklog),   // pending
		mkStatusTask(StatusSprint),    // pending
		mkStatusTask(StatusSprint),    // pending
		mkStatusTask(StatusDoing),     // in-progress
		mkStatusTask(StatusTesting),   // in-progress
		mkStatusTask(StatusCompleted), // completed
		mkStatusTask(StatusCompleted), // completed
	}

	got := CalculateSprintSummary(tasks)
	want := SprintSummary{TotalTasks: 7, Pending: 3, InProgress: 2, Completed: 2}
	if got != want {
		t.Errorf("CalculateSprintSummary = %+v, want %+v", got, want)
	}

	// Must agree with the full report's summary over the same tasks.
	full := CalculateSprintShowResult(&Sprint{ID: 1, Status: SprintOpen}, tasks)
	if full.Summary != want {
		t.Errorf("CalculateSprintShowResult.Summary = %+v, want %+v (must match CalculateSprintSummary)", full.Summary, want)
	}
}

// TestSprintSummary_CompletionPercentage covers the rounding rule and the
// zero-task case (0%) for the completion percentage shown in the sprint status
// summary line (SPEC/WEB.md § Shared Sprint Presentation Sub-Template).
func TestSprintSummary_CompletionPercentage(t *testing.T) {
	cases := []struct {
		name      string
		completed int
		total     int
		want      int
	}{
		{"zero tasks is 0%", 0, 0, 0},
		{"none completed is 0%", 0, 10, 0},
		{"all completed is 100%", 10, 10, 100},
		{"two thirds rounds to 67%", 2, 3, 67},
		{"one third rounds to 33%", 1, 3, 33},
		{"18 of 55 rounds to 33%", 18, 55, 33}, // SPEC AC39 worked example
		{"half rounds to 50%", 1, 2, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := SprintSummary{TotalTasks: c.total, Completed: c.completed}
			if got := s.CompletionPercentage(); got != c.want {
				t.Errorf("CompletionPercentage(completed=%d,total=%d) = %d, want %d",
					c.completed, c.total, got, c.want)
			}
		})
	}
}
