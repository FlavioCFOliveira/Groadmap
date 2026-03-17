# Core Models Specification

This document defines the Go structures and enums for Groadmap, ensuring consistency across the implementation.

## Enums

### Task Status
```go
type TaskStatus string

const (
    StatusBacklog   TaskStatus = "BACKLOG"
    StatusSprint    TaskStatus = "SPRINT"
    StatusDoing     TaskStatus = "DOING"
    StatusTesting   TaskStatus = "TESTING"
    StatusCompleted TaskStatus = "COMPLETED"
)
```

### Sprint Status
```go
type SprintStatus string

const (
    SprintPending SprintStatus = "PENDING"
    SprintOpen    SprintStatus = "OPEN"
    SprintClosed  SprintStatus = "CLOSED"
)
```

---

## Structures

### Task
Maps to the `tasks` table and `Task` JSON object.

```go
type Task struct {
    ID             int        `json:"id"`
    Priority       int        `json:"priority"`
    Severity       int        `json:"severity"`
    Status         TaskStatus `json:"status"`
    Description    string     `json:"description"`
    Specialists    *string    `json:"specialists"` // Nullable in DB
    Action         string     `json:"action"`
    ExpectedResult string     `json:"expected_result"`
    CreatedAt      string     `json:"created_at"`   // ISO 8601 UTC
    CompletedAt    *string    `json:"completed_at"` // ISO 8601 UTC, nullable
}
```

### Sprint
Maps to the `sprints` table and `Sprint` JSON object.

```go
type Sprint struct {
    ID          int          `json:"id"`
    Status      SprintStatus `json:"status"`
    Description string       `json:"description"`
    Tasks       []int        `json:"tasks"`      // Computed from sprint_tasks
    TaskCount   int          `json:"task_count"` // Computed
    CreatedAt   string       `json:"created_at"`
    StartedAt   *string      `json:"started_at"` // Nullable
    ClosedAt    *string      `json:"closed_at"`  // Nullable
}
```

### Audit Entry
Maps to the `audit` table.

```go
type AuditEntry struct {
    ID          int    `json:"id"`
    Operation   string `json:"operation"`
    EntityType  string `json:"entity_type"`
    EntityID    int    `json:"entity_id"`
    PerformedAt string `json:"performed_at"`
}
```

### Roadmap (Metadata)
Used for listing roadmaps.

```go
type Roadmap struct {
    Name string `json:"name"`
    Path string `json:"path"`
    Size int64  `json:"size"`
}
```

### Sprint Stats
Used for the `rmp sprint stats` command.

```go
type SprintStats struct {
    SprintID           int            `json:"sprint_id"`
    TotalTasks         int            `json:"total_tasks"`
    CompletedTasks     int            `json:"completed_tasks"`
    ProgressPercentage float64        `json:"progress_percentage"`
    StatusDistribution map[string]int `json:"status_distribution"`
}
```
