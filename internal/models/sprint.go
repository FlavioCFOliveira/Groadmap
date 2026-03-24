package models

import (
	"fmt"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// SprintStatus represents the current state of a sprint.
type SprintStatus string

// Sprint status constants following the lifecycle in SPEC/DATA_FORMATS.md.
const (
	SprintPending SprintStatus = "PENDING"
	SprintOpen    SprintStatus = "OPEN"
	SprintClosed  SprintStatus = "CLOSED"
)

// ValidSprintStatuses contains all valid sprint statuses.
var ValidSprintStatuses = []SprintStatus{
	SprintPending,
	SprintOpen,
	SprintClosed,
}

// validSprintStatusMap provides O(1) lookup for sprint status validation.
var validSprintStatusMap = map[string]SprintStatus{
	"PENDING": SprintPending,
	"OPEN":    SprintOpen,
	"CLOSED":  SprintClosed,
}

// IsValidSprintStatus checks if a string is a valid sprint status.
// Uses O(1) map lookup instead of O(n) slice iteration.
func IsValidSprintStatus(s string) bool {
	_, ok := validSprintStatusMap[s]
	return ok
}

// ParseSprintStatus parses a string into a SprintStatus.
// Uses O(1) map lookup for validation.
func ParseSprintStatus(s string) (SprintStatus, error) {
	if status, ok := validSprintStatusMap[s]; ok {
		return status, nil
	}
	return "", fmt.Errorf("invalid sprint status: %q", s)
}

// CanStart checks if a sprint can be started (PENDING -> OPEN).
func (ss SprintStatus) CanStart() bool {
	return ss == SprintPending
}

// CanClose checks if a sprint can be closed (OPEN -> CLOSED).
func (ss SprintStatus) CanClose() bool {
	return ss == SprintOpen
}

// CanReopen checks if a sprint can be reopened (CLOSED -> OPEN).
func (ss SprintStatus) CanReopen() bool {
	return ss == SprintClosed
}

// Sprint field size limits
const (
	MaxSprintDescription = 500
)

// Sprint represents a sprint in the roadmap.
// Field order optimized for memory alignment (largest fields first).
type Sprint struct {
	// 16-byte fields
	Description string       `json:"description"`
	CreatedAt   string       `json:"created_at"` // ISO 8601 UTC
	Status      SprintStatus `json:"status"`     // string underlying type

	// 8-byte fields (slice header, then pointers, then integers)
	Tasks     []int   `json:"tasks"`      // Computed from sprint_tasks
	StartedAt *string `json:"started_at"` // ISO 8601 UTC, nullable
	ClosedAt  *string `json:"closed_at"`  // ISO 8601 UTC, nullable
	MaxTasks  *int    `json:"max_tasks"`  // Optional capacity limit
	ID        int     `json:"id"`
	TaskCount int     `json:"task_count"` // Computed
}

// Validate checks if the sprint data is valid.
func (s *Sprint) Validate() error {
	if s.Description == "" {
		return fmt.Errorf("description is required")
	}
	if len(s.Description) > MaxSprintDescription {
		return fmt.Errorf("%w: description exceeds maximum length of %d characters", utils.ErrFieldTooLarge, MaxSprintDescription)
	}
	if !IsValidSprintStatus(string(s.Status)) {
		return fmt.Errorf("invalid status: %q", s.Status)
	}

	// Validate dates
	if err := s.validateDates(); err != nil {
		return err
	}

	return nil
}

// validateDates validates sprint date fields.
// - created_at must not be in the future (with 1 minute tolerance)
// - started_at must not be before created_at
// - closed_at must not be before started_at (if started)
// - closed_at must not be before created_at (if not started)
func (s *Sprint) validateDates() error {
	// Parse and validate created_at
	if s.CreatedAt != "" {
		createdTime, err := utils.ParseISO8601(s.CreatedAt)
		if err != nil {
			return fmt.Errorf("invalid created_at: %w", err)
		}

		// Validate created_at is not in the future
		if err := utils.ValidateNotFuture(createdTime); err != nil {
			return fmt.Errorf("invalid created_at: %w", err)
		}

		// Parse and validate started_at if present
		if s.StartedAt != nil && *s.StartedAt != "" {
			startedTime, err := utils.ParseISO8601(*s.StartedAt)
			if err != nil {
				return fmt.Errorf("invalid started_at: %w", err)
			}

			// Validate started_at is not before created_at
			if err := utils.ValidateDateOrder(createdTime, startedTime); err != nil {
				return fmt.Errorf("invalid date order: started_at %w", err)
			}

			// Parse and validate closed_at if present
			if s.ClosedAt != nil && *s.ClosedAt != "" {
				closedTime, err := utils.ParseISO8601(*s.ClosedAt)
				if err != nil {
					return fmt.Errorf("invalid closed_at: %w", err)
				}

				// Validate closed_at is not before started_at
				if err := utils.ValidateDateOrder(startedTime, closedTime); err != nil {
					return fmt.Errorf("invalid date order: closed_at %w", err)
				}
			}
		} else if s.ClosedAt != nil && *s.ClosedAt != "" {
			// Sprint was closed without being started (edge case)
			closedTime, err := utils.ParseISO8601(*s.ClosedAt)
			if err != nil {
				return fmt.Errorf("invalid closed_at: %w", err)
			}

			// Validate closed_at is not before created_at
			if err := utils.ValidateDateOrder(createdTime, closedTime); err != nil {
				return fmt.Errorf("invalid date order: closed_at %w", err)
			}
		}
	}

	return nil
}

// IsOpen returns true if the sprint status is OPEN.
func (s *Sprint) IsOpen() bool {
	return s.Status == SprintOpen
}

// IsClosed returns true if the sprint status is CLOSED.
func (s *Sprint) IsClosed() bool {
	return s.Status == SprintClosed
}

// IsPending returns true if the sprint status is PENDING.
func (s *Sprint) IsPending() bool {
	return s.Status == SprintPending
}

// BurndownEntry represents a single day's snapshot of tasks remaining in a sprint.
type BurndownEntry struct {
	Date           string `json:"date"`            // ISO 8601 date (YYYY-MM-DD)
	TasksRemaining int    `json:"tasks_remaining"` // Number of tasks not yet completed at end of day
}

// SprintStats represents statistics for a sprint.
type SprintStats struct {
	SprintID           int             `json:"sprint_id"`
	TotalTasks         int             `json:"total_tasks"`
	CompletedTasks     int             `json:"completed_tasks"`
	ProgressPercentage float64         `json:"progress_percentage"`
	StatusDistribution map[string]int  `json:"status_distribution"`
	TaskOrder          []int           `json:"task_order"`     // Task IDs ordered by position
	Velocity           float64         `json:"velocity"`       // Tasks completed per day (CLOSED sprints only; 0.0 if no completed tasks or sprint not closed)
	DaysElapsed        *int            `json:"days_elapsed"`   // Days since sprint started (OPEN sprints only; null otherwise)
	DaysRemaining      *int            `json:"days_remaining"` // Always null — Sprint has no end_date field
	Burndown           []BurndownEntry `json:"burndown"`       // Daily tasks-remaining snapshots, derived from task closed_at dates
}

// CalculateSprintStats calculates statistics from a list of tasks.
// The tasks slice must be ordered by position for correct task_order output.
// sprint is required for velocity and days_elapsed computation; burndown entries are passed separately.
func CalculateSprintStats(sprintID int, tasks []Task) SprintStats {
	stats := SprintStats{
		SprintID:           sprintID,
		TotalTasks:         len(tasks),
		StatusDistribution: make(map[string]int),
		TaskOrder:          make([]int, 0, len(tasks)),
		Burndown:           []BurndownEntry{},
	}

	for _, task := range tasks {
		statusStr := string(task.Status)
		stats.StatusDistribution[statusStr]++

		if task.Status == StatusCompleted {
			stats.CompletedTasks++
		}

		// Add task ID to order array (tasks should already be ordered by position)
		stats.TaskOrder = append(stats.TaskOrder, task.ID)
	}

	if stats.TotalTasks > 0 {
		stats.ProgressPercentage = float64(stats.CompletedTasks) * 100.0 / float64(stats.TotalTasks)
	}

	return stats
}

// ApplySprintMetrics enriches a SprintStats with velocity, days_elapsed, days_remaining, and burndown.
// sprint provides timing context; burndown is a pre-computed slice of BurndownEntry values.
// now is the current time for computing days_elapsed (pass time.Now().UTC() from callers).
func (s *SprintStats) ApplySprintMetrics(sprint *Sprint, burndown []BurndownEntry, now string) {
	if burndown != nil {
		s.Burndown = burndown
	}

	switch sprint.Status {
	case SprintClosed:
		// Velocity: tasks_completed / sprint_duration_days (only meaningful when there is a start and close date).
		if sprint.StartedAt != nil && sprint.ClosedAt != nil {
			startedTime, err1 := utils.ParseISO8601(*sprint.StartedAt)
			closedTime, err2 := utils.ParseISO8601(*sprint.ClosedAt)
			if err1 == nil && err2 == nil {
				durationDays := closedTime.Sub(startedTime).Hours() / 24
				if durationDays > 0 && s.CompletedTasks > 0 {
					s.Velocity = float64(s.CompletedTasks) / durationDays
				}
			}
		}
		// DaysElapsed and DaysRemaining are not applicable for CLOSED sprints.

	case SprintOpen:
		// DaysElapsed: days since sprint started.
		if sprint.StartedAt != nil {
			startedTime, err := utils.ParseISO8601(*sprint.StartedAt)
			nowTime, errNow := utils.ParseISO8601(now)
			if err == nil && errNow == nil {
				elapsed := int(nowTime.Sub(startedTime).Hours() / 24)
				if elapsed < 0 {
					elapsed = 0
				}
				s.DaysElapsed = &elapsed
			}
		}
		// DaysRemaining is always null — Sprint model has no end_date field.
	}
}

// SeverityRangeCount represents count and percentage for a severity range.
type SeverityRangeCount struct {
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// CriticalityDistribution represents task distribution by criticality level.
type CriticalityDistribution struct {
	Low      SeverityRangeCount `json:"low"`
	Medium   SeverityRangeCount `json:"medium"`
	High     SeverityRangeCount `json:"high"`
	Critical SeverityRangeCount `json:"critical"`
}

// SeverityDistribution represents task distribution by severity ranges.
type SeverityDistribution struct {
	Range0To2 SeverityRangeCount `json:"0-2"`
	Range3To5 SeverityRangeCount `json:"3-5"`
	Range6To7 SeverityRangeCount `json:"6-7"`
	Range8To9 SeverityRangeCount `json:"8-9"`
}

// SprintSummary represents the task count summary for a sprint.
type SprintSummary struct {
	TotalTasks int `json:"total_tasks"`
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
}

// SprintProgress represents the progress percentages for a sprint.
type SprintProgress struct {
	PendingPercentage    float64 `json:"pending_percentage"`
	InProgressPercentage float64 `json:"in_progress_percentage"`
	CompletedPercentage  float64 `json:"completed_percentage"`
}

// SprintShowResult represents a comprehensive sprint status report.
// Used for the 'rmp sprint show' command.
type SprintShowResult struct {
	SprintID                int                     `json:"sprint_id"`
	SprintDescription       string                  `json:"sprint_description"`
	Status                  SprintStatus            `json:"status"`
	Summary                 SprintSummary           `json:"summary"`
	Progress                SprintProgress          `json:"progress"`
	SeverityDistribution    SeverityDistribution    `json:"severity_distribution"`
	CriticalityDistribution CriticalityDistribution `json:"criticality_distribution"`
	TaskOrder               []int                   `json:"task_order"` // Task IDs ordered by position
	CurrentLoad             int                     `json:"current_load"`
	MaxTasks                *int                    `json:"max_tasks"`
	CapacityPct             *float64                `json:"capacity_pct"`
}

// CalculateSprintShowResult calculates a comprehensive sprint report from tasks.
// The tasks slice must be ordered by position for correct task_order output.
func CalculateSprintShowResult(sprint *Sprint, tasks []Task) SprintShowResult {
	result := SprintShowResult{
		SprintID:          sprint.ID,
		SprintDescription: sprint.Description,
		Status:            sprint.Status,
		MaxTasks:          sprint.MaxTasks,
		TaskOrder:         make([]int, 0, len(tasks)),
		Summary: SprintSummary{
			TotalTasks: len(tasks),
		},
	}

	// Severity counters
	var severity0To2, severity3To5, severity6To7, severity8To9 int

	for _, task := range tasks {
		// Add task ID to order (tasks should already be ordered by position)
		result.TaskOrder = append(result.TaskOrder, task.ID)

		// Count by status category (preserving existing summary semantics)
		switch task.Status {
		case StatusBacklog, StatusSprint:
			result.Summary.Pending++
		case StatusDoing, StatusTesting:
			result.Summary.InProgress++
		case StatusCompleted:
			result.Summary.Completed++
		}

		// current_load: all incomplete tasks assigned to the sprint
		if task.Status == StatusSprint || task.Status == StatusDoing || task.Status == StatusTesting {
			result.CurrentLoad++
		}

		// Count by severity range
		switch {
		case task.Severity >= 0 && task.Severity <= 2:
			severity0To2++
		case task.Severity >= 3 && task.Severity <= 5:
			severity3To5++
		case task.Severity >= 6 && task.Severity <= 7:
			severity6To7++
		case task.Severity >= 8 && task.Severity <= 9:
			severity8To9++
		}
	}

	// Compute capacity_pct when max_tasks is set and positive.
	if sprint.MaxTasks != nil && *sprint.MaxTasks > 0 {
		pct := float64(result.CurrentLoad) * 100.0 / float64(*sprint.MaxTasks)
		result.CapacityPct = &pct
	}

	// Calculate percentages
	total := result.Summary.TotalTasks
	if total > 0 {
		result.Progress.PendingPercentage = float64(result.Summary.Pending) * 100.0 / float64(total)
		result.Progress.InProgressPercentage = float64(result.Summary.InProgress) * 100.0 / float64(total)
		result.Progress.CompletedPercentage = float64(result.Summary.Completed) * 100.0 / float64(total)

		result.SeverityDistribution.Range0To2 = SeverityRangeCount{Count: severity0To2, Percentage: float64(severity0To2) * 100.0 / float64(total)}
		result.SeverityDistribution.Range3To5 = SeverityRangeCount{Count: severity3To5, Percentage: float64(severity3To5) * 100.0 / float64(total)}
		result.SeverityDistribution.Range6To7 = SeverityRangeCount{Count: severity6To7, Percentage: float64(severity6To7) * 100.0 / float64(total)}
		result.SeverityDistribution.Range8To9 = SeverityRangeCount{Count: severity8To9, Percentage: float64(severity8To9) * 100.0 / float64(total)}

		result.CriticalityDistribution.Low = SeverityRangeCount{Count: severity0To2, Percentage: float64(severity0To2) * 100.0 / float64(total)}
		result.CriticalityDistribution.Medium = SeverityRangeCount{Count: severity3To5, Percentage: float64(severity3To5) * 100.0 / float64(total)}
		result.CriticalityDistribution.High = SeverityRangeCount{Count: severity6To7, Percentage: float64(severity6To7) * 100.0 / float64(total)}
		result.CriticalityDistribution.Critical = SeverityRangeCount{Count: severity8To9, Percentage: float64(severity8To9) * 100.0 / float64(total)}
	}

	return result
}
