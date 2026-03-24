# Core Models Specification

This document defines the Go structures and enums for Groadmap, ensuring consistency across the implementation.

## Enums

### Task Status
```go
type TaskStatus string

const (
    StatusBacklog   TaskStatus = "BACKLOG"
    StatusSprint    TaskStatus = "SPRINT"    // Automatically set when added to sprint
    StatusDoing     TaskStatus = "DOING"
    StatusTesting   TaskStatus = "TESTING"
    StatusCompleted TaskStatus = "COMPLETED"
)
```

**Status Usage Notes:**

| Status | Set Automatically | Set Manually | Description |
|--------|-------------------|--------------|-------------|
| `BACKLOG` | Yes (on remove from sprint) | Yes | Task is in backlog, not assigned to sprint |
| `SPRINT` | **Yes** | No | Task is assigned to sprint. **Do not set manually** - use `sprint add-tasks` |
| `DOING` | No | Yes | Task is being worked on |
| `TESTING` | No | Yes | Task is in testing phase |
| `COMPLETED` | No | Yes | Task is complete |

**Important:** The `SPRINT` status is automatically managed by sprint operations (`sprint add-tasks`, `sprint remove-tasks`). Attempting to manually transition to `SPRINT` via `task stat` should be rejected.

### Task Type
```go
type TaskType string

const (
    TypeUserStory TaskType = "USER_STORY"
    TypeTask      TaskType = "TASK"
    TypeBug       TaskType = "BUG"
    TypeSubTask   TaskType = "SUB_TASK"
    TypeEpic      TaskType = "EPIC"
    TypeRefactor  TaskType = "REFACTOR"
    TypeChore     TaskType = "CHORE"
    TypeSpike     TaskType = "SPIKE"
    TypeDesignUX  TaskType = "DESIGN_UX"
    TypeImprovement TaskType = "IMPROVEMENT"
)
```

**Descriptions:**

| Type | Description |
|------|-------------|
| `USER_STORY` | New feature from end user's perspective. Focuses on "who", "what", and "why". |
| `TASK` | Internal work units that don't deliver direct value but are necessary (e.g., configure database). |
| `BUG` | Report of something not working as expected in existing code. |
| `SUB_TASK` | Decomposition of a Story or Task into smaller steps for easier tracking. |
| `EPIC` | Large body of work grouping multiple related Stories and Tasks. Spans multiple sprints. |
| `REFACTOR` | Improvement of internal code structure without changing external behavior. Reduces technical debt. |
| `CHORE` | Necessary maintenance that doesn't add features or fix bugs (e.g., update dependencies). |
| `SPIKE` | Research or prototyping task to reduce technical uncertainties before development. |
| `DESIGN_UX` | Tasks focused on creating prototypes, wireframes, or interface flows. |
| `IMPROVEMENT` | Refinement of an existing working feature that can be optimized. |

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

**Field Length Constraints:**
- `Title`: Maximum 255 characters
- `FunctionalRequirements`: Maximum 4096 characters
- `TechnicalRequirements`: Maximum 4096 characters
- `AcceptanceCriteria`: Maximum 4096 characters
- `Specialists`: Maximum 500 characters (comma-separated list of specialist names)
- `CompletionSummary`: Maximum 4096 characters (optional, set only on close)

```go
// Task represents a task in the roadmap.
// Field order optimized for memory layout (168 bytes, zero padding on 64-bit systems).
// Groups: Content fields (strings), Tracking fields (pointers), Metadata (ints).
// All content fields are mandatory (NOT NULL) with enforced maximum lengths.
type Task struct {
    // Group 1: Content fields - frequently accessed together (112 bytes total)
    // All fields are mandatory (NOT NULL) with length constraints enforced by application
    Title                  string     `json:"title"`                    // Task title/summary, max 255 chars
    Status                 TaskStatus `json:"status"`                   // Current status
    Type                   TaskType   `json:"type"`                     // Task type classification
    FunctionalRequirements string     `json:"functional_requirements"`  // Why: functional requirements, max 4096 chars
    TechnicalRequirements  string     `json:"technical_requirements"`   // How: technical description, max 4096 chars
    AcceptanceCriteria     string     `json:"acceptance_criteria"`      // How to verify: completion criteria, max 4096 chars
    CreatedAt              string     `json:"created_at"`               // ISO 8601 UTC, auto-set on creation

    // Group 2: Nullable tracking fields - lifecycle timestamps and completion data (40 bytes total)
    Specialists        *string `json:"specialists"`          // Comma-separated specialists, nullable, max 500 chars
    StartedAt          *string `json:"started_at"`           // ISO 8601 UTC, auto-set on DOING transition
    TestedAt           *string `json:"tested_at"`            // ISO 8601 UTC, auto-set on TESTING transition
    ClosedAt           *string `json:"closed_at"`            // ISO 8601 UTC, auto-set on COMPLETED transition
    CompletionSummary  *string `json:"completion_summary"`   // Optional summary of work done, settable only on TESTING → COMPLETED, max 4096 chars
    ParentTaskID       *int    `json:"parent_task_id"`       // NULL for top-level tasks; non-NULL links to parent task

    // Group 3: Numeric metadata fields (24 bytes total)
    ID           int `json:"id"`            // Primary key
    Priority     int `json:"priority"`      // 0-9 priority level
    Severity     int `json:"severity"`      // 0-9 severity level
    SubtaskCount int `json:"subtask_count"` // Computed: number of direct subtasks (not stored in DB)

    // Dependency fields (fetched from task_dependencies table)
    DependsOn []int `json:"depends_on"` // IDs of tasks this task depends on (blocking this task)
    Blocks    []int `json:"blocks"`     // IDs of tasks that depend on this task (tasks this task is blocking)
}
```

### Sprint
Maps to the `sprints` table and `Sprint` JSON object.

```go
type Sprint struct {
    ID          int          `json:"id"`
    Status      SprintStatus `json:"status"`
    Description string       `json:"description"`
    Tasks       []int        `json:"tasks"`            // Computed from sprint_tasks (ordered by position)
    TaskCount   int          `json:"task_count"`       // Computed
    CreatedAt   string       `json:"created_at"`
    StartedAt   *string      `json:"started_at"`       // Nullable
    ClosedAt    *string      `json:"closed_at"`        // Nullable
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

### BurndownEntry
Represents a single day's snapshot of tasks remaining in a sprint. Used in the `burndown` field of `SprintStats`.

```go
type BurndownEntry struct {
    Date           string `json:"date"`            // ISO 8601 date (YYYY-MM-DD)
    TasksRemaining int    `json:"tasks_remaining"` // Number of tasks not yet completed at end of day
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
    TaskOrder          []int          `json:"task_order"`
    Velocity           float64        `json:"velocity"`
    DaysElapsed        *int           `json:"days_elapsed"`
    DaysRemaining      *int           `json:"days_remaining"`
    Burndown           []BurndownEntry `json:"burndown"`
}
```

**Field Descriptions:**

| Field | Type | Description |
|-------|------|-------------|
| `sprint_id` | int | Sprint identifier |
| `total_tasks` | int | Total number of tasks in sprint |
| `completed_tasks` | int | Number of tasks with status COMPLETED |
| `progress_percentage` | float64 | Percentage of completed tasks (0.0-100.0) |
| `status_distribution` | map[string]int | Count of tasks per status |
| `task_order` | []int | Ordered array of task IDs by position (computed in real-time from sprint_tasks table) |
| `velocity` | float64 | Tasks completed per day. Non-zero only for CLOSED sprints with completed tasks and positive duration. 0.0 otherwise |
| `days_elapsed` | *int (nullable) | Days since the sprint was started. Present only for OPEN sprints with a started_at date. null otherwise |
| `days_remaining` | *int (nullable) | Always null. Sprint has no end_date field |
| `burndown` | []BurndownEntry | Daily tasks-remaining snapshots derived from task closed_at dates. Empty array when no tasks completed |

**TaskOrder Field Behavior:**
- **Purpose:** Defines the execution sequence of tasks within the sprint. Lower positions (starting at 0) represent higher priority tasks that should be executed first.
- **Source:** Computed from the `sprint_tasks` junction table which maintains the many-to-many relationship between sprints and tasks, including the `position` column.
- **Always included** in the SprintStats response
- **Computed in real-time** from the sprint_tasks table, ordered by position (ASC)
- **Format:** Array of task IDs where index 0 is the first task to execute (position 0)
- **Empty sprint:** Returns empty array `[]` when sprint has no tasks
- **Dynamic:** Reflects the current sprint task ordering. Changes to task order via sprint reorder commands are immediately reflected.

**Velocity Computation:**
- `velocity = completed_tasks / sprint_duration_days`
- `sprint_duration_days = (closed_at - started_at)` in fractional days
- Only computed for CLOSED sprints that have both `started_at` and `closed_at` set and a positive duration
- 0.0 for sprints with no completed tasks, zero/negative duration, or non-CLOSED status

**Burndown Computation:**
- Queries task `closed_at` dates for all COMPLETED tasks belonging to the sprint
- Groups completions by calendar date (YYYY-MM-DD)
- Starts with `total_tasks` remaining on the sprint start date (or first completion date if no start date)
- Decrements remaining count by completions per day
- Only dates with at least one completion are included

### Sprint Task Order

Represents the ordering of a task within a sprint. Used for sprint task sequence operations.

```go
type SprintTaskOrder struct {
    TaskID   int `json:"task_id"`   // Task identifier
    Position int `json:"position"` // 0-based position in sprint task order
}
```

### Sprint Task with Order

Represents a task within a sprint including its position. Used for sprint task listings.

```go
type SprintTask struct {
    Task
    Position int `json:"position"` // 0-based position in sprint task order
}
```

### Sprint Task Reorder Request

Represents a request to reorder sprint tasks.

```go
type SprintTaskReorderRequest struct {
    SprintID int   `json:"sprint_id"` // Sprint identifier
    TaskIDs  []int `json:"task_ids"`  // Ordered list of task IDs defining new sequence
}
```

### Roadmap Stats

Used for the `rmp stats` command. Provides comprehensive roadmap statistics.

```go
type SprintStatsSummary struct {
    Current   *int `json:"current"`   // ID of the currently open sprint, or null if none
    Total     int  `json:"total"`     // Total number of sprints
    Completed int  `json:"completed"` // Number of closed sprints
    Pending   int  `json:"pending"`   // Number of open sprints (typically 0 or 1)
}

type TaskStatsSummary struct {
    Backlog   int `json:"backlog"`   // Tasks with status BACKLOG
    Sprint    int `json:"sprint"`    // Tasks with status SPRINT
    Doing     int `json:"doing"`     // Tasks with status DOING
    Testing   int `json:"testing"`   // Tasks with status TESTING
    Completed int `json:"completed"` // Tasks with status COMPLETED
}

type RoadmapStats struct {
    Roadmap         string             `json:"roadmap"`
    Sprints         SprintStatsSummary `json:"sprints"`
    Tasks           TaskStatsSummary   `json:"tasks"`
    AverageVelocity float64            `json:"average_velocity"` // Average tasks/day across last 5 closed sprints (0.0 if none)
}
```

**Field Descriptions:**

| Field | Type | Description |
|-------|------|-------------|
| `roadmap` | string | Name of the roadmap |
| `sprints` | SprintStatsSummary | Sprint counts by state |
| `tasks` | TaskStatsSummary | Task counts by status |
| `average_velocity` | float64 | Average tasks completed per day across the last 5 closed sprints. 0.0 when no qualifying sprints exist |

**average_velocity Computation:**
- Considers the last 5 CLOSED sprints with both `started_at` and `closed_at` set
- Per-sprint velocity = `completed_tasks / sprint_duration_days`
- Sprints with zero duration are excluded from the count entirely
- Sprints with zero completed tasks contribute 0.0 to the average
- Returns 0.0 when no qualifying sprints exist
