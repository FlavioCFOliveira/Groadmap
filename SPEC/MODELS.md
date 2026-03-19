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
// Task represents a task in the roadmap.
// Field order optimized for memory layout (152 bytes, zero padding on 64-bit systems).
// Groups: Content fields (strings), Tracking fields (pointers), Metadata (ints).
type Task struct {
    // Group 1: Content fields - frequently accessed together (96 bytes total)
    Title                  string     `json:"title"`                    // Task title/summary
    Status                 TaskStatus `json:"status"`                   // Current status
    FunctionalRequirements string     `json:"functional_requirements"`  // Why: functional requirements
    TechnicalRequirements  string     `json:"technical_requirements"`   // How: technical description
    AcceptanceCriteria     string     `json:"acceptance_criteria"`      // How to verify: completion criteria
    CreatedAt              string     `json:"created_at"`               // ISO 8601 UTC, auto-set on creation

    // Group 2: Nullable tracking fields - lifecycle timestamps (32 bytes total)
    Specialists *string `json:"specialists"`  // Comma-separated specialists
    StartedAt   *string `json:"started_at"`   // ISO 8601 UTC, auto-set on DOING transition
    TestedAt    *string `json:"tested_at"`    // ISO 8601 UTC, auto-set on TESTING transition
    ClosedAt    *string `json:"closed_at"`    // ISO 8601 UTC, auto-set on COMPLETED transition

    // Group 3: Numeric metadata fields (24 bytes total)
    ID       int `json:"id"`       // Primary key
    Priority int `json:"priority"` // 0-9 priority level
    Severity int `json:"severity"` // 0-9 severity level
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
