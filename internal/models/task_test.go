package models

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

func TestTaskValidate(t *testing.T) {
	validSpecialists := "developer,tester"
	validCreatedAt := "2026-03-16T12:00:00.000Z"
	validCompletedAt := "2026-03-17T12:00:00.000Z"

	tests := []struct {
		name    string
		task    Task
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid task",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: false,
		},
		{
			name: "valid task with all fields",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusCompleted,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				Specialists:    &validSpecialists,
				CreatedAt:      validCreatedAt,
				CompletedAt:    &validCompletedAt,
			},
			wantErr: false,
		},
		{
			name: "empty description",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "description is required",
		},
		{
			name: "empty action",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "action is required",
		},
		{
			name: "empty expected_result",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "expected_result is required",
		},
		{
			name: "invalid priority - negative",
			task: Task{
				Priority:       -1,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "priority must be between 0 and 9",
		},
		{
			name: "invalid priority - too high",
			task: Task{
				Priority:       10,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "priority must be between 0 and 9",
		},
		{
			name: "invalid severity - negative",
			task: Task{
				Priority:       5,
				Severity:       -1,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "severity must be between 0 and 9",
		},
		{
			name: "invalid severity - too high",
			task: Task{
				Priority:       5,
				Severity:       10,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "severity must be between 0 and 9",
		},
		{
			name: "invalid status",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         "INVALID",
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "invalid status",
		},
		{
			name: "description too long",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    strings.Repeat("a", MaxTaskDescription+1),
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "description exceeds maximum length",
		},
		{
			name: "action too long",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         strings.Repeat("a", MaxTaskAction+1),
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "action exceeds maximum length",
		},
		{
			name: "expected_result too long",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: strings.Repeat("a", MaxTaskExpectedResult+1),
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "expected_result exceeds maximum length",
		},
		{
			name: "specialists too long",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				Specialists:    func() *string { s := strings.Repeat("a", MaxTaskSpecialists+1); return &s }(),
				CreatedAt:      validCreatedAt,
			},
			wantErr: true,
			errMsg:  "specialists exceeds maximum length",
		},
		// Date validation tests
		{
			name: "created_at before 1970",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      "1969-12-31T23:59:59.999Z",
			},
			wantErr: true,
			errMsg:  "invalid created_at",
		},
		{
			name: "created_at in far future",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      "2030-01-01T00:00:00.000Z",
			},
			wantErr: true,
			errMsg:  "invalid created_at",
		},
		{
			name: "completed_at before created_at",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusCompleted,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      "2026-03-16T12:00:00.000Z",
				CompletedAt:    func() *string { s := "2026-03-15T12:00:00.000Z"; return &s }(),
			},
			wantErr: true,
			errMsg:  "invalid date order",
		},
		{
			name: "invalid created_at format",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      "invalid-date",
			},
			wantErr: true,
			errMsg:  "invalid created_at",
		},
		{
			name: "invalid completed_at format",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusCompleted,
				Description:    "Test description",
				Action:         "Test action",
				ExpectedResult: "Test result",
				CreatedAt:      validCreatedAt,
				CompletedAt:    func() *string { s := "invalid-date"; return &s }(),
			},
			wantErr: true,
			errMsg:  "invalid completed_at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.task.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Task.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Task.Validate() error message = %q, should contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestTaskIsComplete(t *testing.T) {
	tests := []struct {
		name     string
		status   TaskStatus
		expected bool
	}{
		{
			name:     "completed task",
			status:   StatusCompleted,
			expected: true,
		},
		{
			name:     "backlog task",
			status:   StatusBacklog,
			expected: false,
		},
		{
			name:     "sprint task",
			status:   StatusSprint,
			expected: false,
		},
		{
			name:     "doing task",
			status:   StatusDoing,
			expected: false,
		},
		{
			name:     "testing task",
			status:   StatusTesting,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := Task{Status: tt.status}
			if got := task.IsComplete(); got != tt.expected {
				t.Errorf("Task.IsComplete() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestIsValidTaskStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"BACKLOG", "BACKLOG", true},
		{"SPRINT", "SPRINT", true},
		{"DOING", "DOING", true},
		{"TESTING", "TESTING", true},
		{"COMPLETED", "COMPLETED", true},
		{"invalid", "INVALID", false},
		{"empty", "", false},
		{"lowercase", "backlog", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidTaskStatus(tt.input); got != tt.expected {
				t.Errorf("IsValidTaskStatus(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseTaskStatus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    TaskStatus
		wantErr bool
	}{
		{
			name:    "valid BACKLOG",
			input:   "BACKLOG",
			want:    StatusBacklog,
			wantErr: false,
		},
		{
			name:    "valid COMPLETED",
			input:   "COMPLETED",
			want:    StatusCompleted,
			wantErr: false,
		},
		{
			name:    "invalid status",
			input:   "INVALID",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTaskStatus(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTaskStatus() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseTaskStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanTransitionTo(t *testing.T) {
	tests := []struct {
		name       string
		from       TaskStatus
		to         TaskStatus
		canTransit bool
	}{
		// BACKLOG transitions
		{"BACKLOG to SPRINT", StatusBacklog, StatusSprint, true},
		{"BACKLOG to DOING", StatusBacklog, StatusDoing, false},
		{"BACKLOG to COMPLETED", StatusBacklog, StatusCompleted, false},

		// SPRINT transitions
		{"SPRINT to BACKLOG", StatusSprint, StatusBacklog, true},
		{"SPRINT to DOING", StatusSprint, StatusDoing, true},
		{"SPRINT to TESTING", StatusSprint, StatusTesting, false},

		// DOING transitions
		{"DOING to SPRINT", StatusDoing, StatusSprint, true},
		{"DOING to TESTING", StatusDoing, StatusTesting, true},
		{"DOING to COMPLETED", StatusDoing, StatusCompleted, false},

		// TESTING transitions
		{"TESTING to DOING", StatusTesting, StatusDoing, true},
		{"TESTING to COMPLETED", StatusTesting, StatusCompleted, true},
		{"TESTING to BACKLOG", StatusTesting, StatusBacklog, false},

		// COMPLETED transitions
		{"COMPLETED to BACKLOG", StatusCompleted, StatusBacklog, true},
		{"COMPLETED to SPRINT", StatusCompleted, StatusSprint, false},
		{"COMPLETED to DOING", StatusCompleted, StatusDoing, false},

		// Same status
		{"BACKLOG to BACKLOG", StatusBacklog, StatusBacklog, false},
		{"COMPLETED to COMPLETED", StatusCompleted, StatusCompleted, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.canTransit {
				t.Errorf("%v.CanTransitionTo(%v) = %v, want %v", tt.from, tt.to, got, tt.canTransit)
			}
		})
	}
}

func TestParseSpecialists(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single specialist",
			input:    "developer",
			expected: []string{"developer"},
		},
		{
			name:     "multiple specialists",
			input:    "developer,tester,manager",
			expected: []string{"developer", "tester", "manager"},
		},
		{
			name:     "with spaces",
			input:    " developer , tester ",
			expected: []string{"developer", "tester"},
		},
		{
			name:     "empty items",
			input:    "developer,,tester",
			expected: []string{"developer", "tester"},
		},
		{
			name:     "only spaces",
			input:    "  ,  ",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSpecialists(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("ParseSpecialists() = %v, want %v", got, tt.expected)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("ParseSpecialists()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestFormatSpecialists(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: "",
		},
		{
			name:     "nil slice",
			input:    nil,
			expected: "",
		},
		{
			name:     "single specialist",
			input:    []string{"developer"},
			expected: "developer",
		},
		{
			name:     "multiple specialists",
			input:    []string{"developer", "tester", "manager"},
			expected: "developer,tester,manager",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatSpecialists(tt.input); got != tt.expected {
				t.Errorf("FormatSpecialists() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestTaskValidateDates specifically tests the date validation logic
func TestTaskValidateDates(t *testing.T) {
	now := time.Now().UTC()
	oneHourAgo := now.Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	oneHourLater := now.Add(time.Hour).Format("2006-01-02T15:04:05.000Z")

	tests := []struct {
		name    string
		task    Task
		wantErr bool
		errType error
	}{
		{
			name: "valid dates - completed after created",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusCompleted,
				Description:    "Test",
				Action:         "Test",
				ExpectedResult: "Test",
				CreatedAt:      "2026-03-16T10:00:00.000Z",
				CompletedAt:    func() *string { s := "2026-03-16T12:00:00.000Z"; return &s }(),
			},
			wantErr: false,
		},
		{
			name: "valid dates - same time",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusCompleted,
				Description:    "Test",
				Action:         "Test",
				ExpectedResult: "Test",
				CreatedAt:      "2026-03-16T12:00:00.000Z",
				CompletedAt:    func() *string { s := "2026-03-16T12:00:00.000Z"; return &s }(),
			},
			wantErr: false,
		},
		{
			name: "valid created_at - recent past",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test",
				Action:         "Test",
				ExpectedResult: "Test",
				CreatedAt:      oneHourAgo,
			},
			wantErr: false,
		},
		{
			name: "invalid - created_at in future",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusBacklog,
				Description:    "Test",
				Action:         "Test",
				ExpectedResult: "Test",
				CreatedAt:      oneHourLater,
			},
			wantErr: true,
			errType: utils.ErrDateInFuture,
		},
		{
			name: "invalid - completed_at before created_at",
			task: Task{
				Priority:       5,
				Severity:       3,
				Status:         StatusCompleted,
				Description:    "Test",
				Action:         "Test",
				ExpectedResult: "Test",
				CreatedAt:      "2026-03-16T12:00:00.000Z",
				CompletedAt:    func() *string { s := "2026-03-16T10:00:00.000Z"; return &s }(),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.task.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Task.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errType != nil && !errors.Is(err, tt.errType) {
				t.Errorf("Task.Validate() error type check failed: got %v, want to contain %v", err, tt.errType)
			}
		})
	}
}

// TestTaskFieldLimits validates the field limit constants
func TestTaskFieldLimits(t *testing.T) {
	// Verify constants are positive
	if MaxTaskDescription <= 0 {
		t.Error("MaxTaskDescription should be positive")
	}
	if MaxTaskAction <= 0 {
		t.Error("MaxTaskAction should be positive")
	}
	if MaxTaskExpectedResult <= 0 {
		t.Error("MaxTaskExpectedResult should be positive")
	}
	if MaxTaskSpecialists <= 0 {
		t.Error("MaxTaskSpecialists should be positive")
	}
}

// TestValidateStatusTransition tests the ValidateStatusTransition function
func TestValidateStatusTransition(t *testing.T) {
	tests := []struct {
		name    string
		current string
		new     string
		wantErr bool
		errMsg  string
	}{
		// Valid transitions
		{"BACKLOG to SPRINT", "BACKLOG", "SPRINT", false, ""},
		{"SPRINT to DOING", "SPRINT", "DOING", false, ""},
		{"DOING to TESTING", "DOING", "TESTING", false, ""},
		{"TESTING to COMPLETED", "TESTING", "COMPLETED", false, ""},
		{"COMPLETED to BACKLOG", "COMPLETED", "BACKLOG", false, ""},

		// Invalid transitions
		{"BACKLOG to DOING (invalid)", "BACKLOG", "DOING", true, "cannot transition"},
		{"BACKLOG to COMPLETED (invalid)", "BACKLOG", "COMPLETED", true, "cannot transition"},
		{"COMPLETED to SPRINT (invalid)", "COMPLETED", "SPRINT", true, "cannot transition"},

		// Invalid status values
		{"invalid current status", "INVALID", "BACKLOG", true, "invalid current status"},
		{"invalid new status", "BACKLOG", "INVALID", true, "invalid target status"},
		{"both invalid", "INVALID1", "INVALID2", true, "invalid current status"},
		{"empty current", "", "BACKLOG", true, "invalid current status"},
		{"empty new", "BACKLOG", "", true, "invalid target status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStatusTransition(tt.current, tt.new)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStatusTransition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateStatusTransition() error message = %q, should contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestGetValidTransitions tests the GetValidTransitions function
func TestGetValidTransitions(t *testing.T) {
	tests := []struct {
		name     string
		status   TaskStatus
		expected []TaskStatus
	}{
		{"BACKLOG", StatusBacklog, []TaskStatus{StatusSprint}},
		{"SPRINT", StatusSprint, []TaskStatus{StatusBacklog, StatusDoing}},
		{"DOING", StatusDoing, []TaskStatus{StatusSprint, StatusTesting}},
		{"TESTING", StatusTesting, []TaskStatus{StatusDoing, StatusCompleted}},
		{"COMPLETED", StatusCompleted, []TaskStatus{StatusBacklog}},
		{"invalid status", TaskStatus("INVALID"), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetValidTransitions(tt.status)
			if len(got) != len(tt.expected) {
				t.Errorf("GetValidTransitions() = %v, want %v", got, tt.expected)
				return
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("GetValidTransitions()[%d] = %v, want %v", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

// TestCanTransitionTo_InvalidStatus tests CanTransitionTo with invalid statuses
func TestCanTransitionTo_InvalidStatus(t *testing.T) {
	// Invalid current status
	invalidCurrent := TaskStatus("INVALID")
	if invalidCurrent.CanTransitionTo(StatusBacklog) {
		t.Error("CanTransitionTo should return false for invalid current status")
	}

	// Invalid target status
	if StatusBacklog.CanTransitionTo(TaskStatus("INVALID")) {
		t.Error("CanTransitionTo should return false for invalid target status")
	}
}
