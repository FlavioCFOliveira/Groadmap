package models

// SprintStatsSummary represents statistics for sprints in a roadmap.
type SprintStatsSummary struct {
	Current   *int `json:"current"`   // ID of the currently open sprint, or null if none
	Total     int  `json:"total"`     // Total number of sprints
	Completed int  `json:"completed"` // Number of closed sprints
	Pending   int  `json:"pending"`   // Number of open sprints (typically 0 or 1)
}

// TaskStatsSummary represents statistics for tasks in a roadmap.
type TaskStatsSummary struct {
	Backlog   int `json:"backlog"`   // Tasks with status BACKLOG
	Sprint    int `json:"sprint"`    // Tasks with status SPRINT
	Doing     int `json:"doing"`     // Tasks with status DOING
	Testing   int `json:"testing"`   // Tasks with status TESTING
	Completed int `json:"completed"` // Tasks with status COMPLETED
}

// RoadmapStats represents comprehensive statistics for a roadmap.
type RoadmapStats struct {
	Roadmap         string             `json:"roadmap"`
	Sprints         SprintStatsSummary `json:"sprints"`
	Tasks           TaskStatsSummary   `json:"tasks"`
	AverageVelocity float64            `json:"average_velocity"` // Average tasks/day across last 5 closed sprints (0.0 if none)
}
