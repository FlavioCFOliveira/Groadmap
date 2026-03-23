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

    // Group 2: Nullable tracking fields - lifecycle timestamps (32 bytes total)
    Specialists *string `json:"specialists"`  // Comma-separated specialists, nullable, max 500 chars
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

**TaskOrder Field Behavior:**
- **Purpose:** Defines the execution sequence of tasks within the sprint. Lower positions (starting at 0) represent higher priority tasks that should be executed first.
- **Source:** Computed from the `sprint_tasks` junction table which maintains the many-to-many relationship between sprints and tasks, including the `position` column.
- **Always included** in the SprintStats response
- **Computed in real-time** from the sprint_tasks table, ordered by position (ASC)
- **Format:** Array of task IDs where index 0 is the first task to execute (position 0)
- **Empty sprint:** Returns empty array `[]` when sprint has no tasks
- **Dynamic:** Reflects the current sprint task ordering. Changes to task order via sprint reorder commands are immediately reflected.

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
