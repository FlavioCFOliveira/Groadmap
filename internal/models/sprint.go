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

// IsValidSprintStatus checks if a string is a valid sprint status.
func IsValidSprintStatus(s string) bool {
	for _, status := range ValidSprintStatuses {
		if string(status) == s {
			return true
		}
	}
	return false
}

// ParseSprintStatus parses a string into a SprintStatus.
func ParseSprintStatus(s string) (SprintStatus, error) {
	if !IsValidSprintStatus(s) {
		return "", fmt.Errorf("invalid sprint status: %q", s)
	}
	return SprintStatus(s), nil
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
type Sprint struct {
	ID          int          `json:"id"`
	Status      SprintStatus `json:"status"`
	Description string       `json:"description"`
	Tasks       []int        `json:"tasks"`      // Computed from sprint_tasks
	TaskCount   int          `json:"task_count"` // Computed
	CreatedAt   string       `json:"created_at"` // ISO 8601 UTC
	StartedAt   *string      `json:"started_at"` // ISO 8601 UTC, nullable
	ClosedAt    *string      `json:"closed_at"`  // ISO 8601 UTC, nullable
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

// SprintStats represents statistics for a sprint.
type SprintStats struct {
	SprintID           int            `json:"sprint_id"`
	TotalTasks         int            `json:"total_tasks"`
	CompletedTasks     int            `json:"completed_tasks"`
	ProgressPercentage float64        `json:"progress_percentage"`
	StatusDistribution map[string]int `json:"status_distribution"`
}

// CalculateSprintStats calculates statistics from a list of tasks.
func CalculateSprintStats(sprintID int, tasks []Task) SprintStats {
	stats := SprintStats{
		SprintID:           sprintID,
		TotalTasks:         len(tasks),
		StatusDistribution: make(map[string]int),
	}

	for _, task := range tasks {
		statusStr := string(task.Status)
		stats.StatusDistribution[statusStr]++

		if task.Status == StatusCompleted {
			stats.CompletedTasks++
		}
	}

	if stats.TotalTasks > 0 {
		stats.ProgressPercentage = float64(stats.CompletedTasks) * 100.0 / float64(stats.TotalTasks)
	}

	return stats
}
