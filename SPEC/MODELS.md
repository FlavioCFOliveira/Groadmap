# Core Models Specification

This document defines the Go structures and enums for Groadmap, ensuring consistency across the implementation.

## Table of Contents

- [Enums](#enums)
  - [Task Status](#task-status)
  - [Task Type](#task-type)
  - [Sprint Status](#sprint-status)
  - [Comment Type](#comment-type)
  - [Entity Type](#entity-type)
  - [Audit Operation](#audit-operation)
- [Structures](#structures)
  - [Task](#task)
  - [Sprint](#sprint)
  - [Task Comment](#task-comment)
  - [Sprint Comment](#sprint-comment)
  - [Audit Entry](#audit-entry)
  - [Roadmap (Metadata)](#roadmap-metadata)
  - [BurndownEntry](#burndownentry)
  - [Sprint Stats](#sprint-stats)
  - [Sprint Show Result](#sprint-show-result)
  - [Roadmap Stats](#roadmap-stats)
- [Memory Layout Optimization](#memory-layout-optimization)
  - [Struct Field Ordering](#struct-field-ordering)
  - [Cache Line Considerations](#cache-line-considerations)

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
| `BACKLOG` | Yes (on remove from sprint) | Yes | Task is in the backlog. It usually belongs to no sprint, but it can still be a sprint member; see `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status` |
| `SPRINT` | **Yes** | No | Task is assigned to sprint. **Do not set manually** - use `sprint add-tasks` |
| `DOING` | No | Yes | Task is being worked on |
| `TESTING` | No | Yes | Task is in testing phase |
| `COMPLETED` | No | Yes | Task is complete |

**Important:** The `SPRINT` status is automatically managed by sprint operations (`sprint add-tasks`, `sprint remove-tasks`). Attempting to manually transition to `SPRINT` via `task stat` should be rejected.

**Status is not membership:** The `status` column does not record which sprint a task belongs to; the `sprint_tasks` table does (see `DATABASE.md § sprint_tasks Table (1:N Relationship)`). A task whose status is `BACKLOG` may still be a member of a sprint. `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status` is the canonical description of that state.

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

### Comment Type

Classifies a comment. There is one `CommentType` enum, with seven values. Each entity accepts a subset of it, and the application enforces the subset that applies to the entity being commented on.

```go
type CommentType string

const (
    CommentFinding    CommentType = "FINDING"
    CommentHypothesis CommentType = "HYPOTHESIS"
    CommentTest       CommentType = "TEST"
    CommentDecision   CommentType = "DECISION"
    CommentProgress   CommentType = "PROGRESS"
    CommentUpdate     CommentType = "UPDATE"
    CommentNote       CommentType = "NOTE"
)

// ValidTaskCommentTypes lists, in canonical order, the seven values a task
// comment accepts.
var ValidTaskCommentTypes = []CommentType{
    CommentFinding, CommentHypothesis, CommentTest, CommentDecision,
    CommentProgress, CommentUpdate, CommentNote,
}

// ValidSprintCommentTypes lists, in canonical order, the four values a sprint
// comment accepts.
var ValidSprintCommentTypes = []CommentType{
    CommentFinding, CommentDecision, CommentProgress, CommentUpdate,
}
```

**Descriptions:**

| Type | Description |
|------|-------------|
| `FINDING` | Something discovered during the work: an observed behaviour, a measurement, a cause identified, a constraint that turned out to apply. |
| `HYPOTHESIS` | A proposition raised to explain a problem or to guide the next step, stated before it is confirmed or refuted. |
| `TEST` | A test that was run and what it showed. Covers both automated tests and manual verification. |
| `DECISION` | A decision taken during the work, and the reasoning behind it. |
| `PROGRESS` | A statement of how the work advanced: what was done, what remains. |
| `UPDATE` | The reason behind a modification to the definition of the task or the sprint: something added, updated, removed, complemented, or clarified. |
| `NOTE` | A remark that belongs in the log but fits none of the categories above. |

**Per-entity valid subsets:**

| Entity | Accepted values | Rejected values |
|--------|-----------------|-----------------|
| Task | `FINDING`, `HYPOTHESIS`, `TEST`, `DECISION`, `PROGRESS`, `UPDATE`, `NOTE` | none |
| Sprint | `FINDING`, `DECISION`, `PROGRESS`, `UPDATE` | `HYPOTHESIS`, `TEST`, `NOTE` |

The subsets follow from what each entity's log is for. A task comment records exclusively the work carried out within the scope of that task, which is where hypotheses, tests, and incidental notes belong. A sprint comment records only the progression of the work during the sprint's development — findings, decisions, progress, and the reason behind a change to the sprint's definition — so the three task-only values are not accepted on a sprint.

The type is mandatory on every comment: there is no default value and no untyped comment. An invalid value, or a value valid for the other entity but not for this one, is rejected with exit code 6 and a message naming the valid set for the entity (see `COMMANDS.md § Add Task Comment` and `COMMANDS.md § Add Sprint Comment`). The database enforces the same subsets independently, through a `CHECK` constraint on each comment table (see `DATABASE.md § task_comments Table` and `DATABASE.md § sprint_comments Table`).

The two subsets are also kept apart on the two surfaces that publish enums to a caller: each family's help lists only its own set, and the machine-readable AI Agent Contract exposes them as two enum keys, `TaskCommentType` and `SprintCommentType`, because a contract flag names exactly one enum (see `HELP.md § Comment subcommand help specifics` and `DATA_FORMATS.md § enums map entry`).

---

### Entity Type

Names the kind of entity an audit entry belongs to.

```go
type EntityType string

const (
    EntityTask   EntityType = "TASK"
    EntitySprint EntityType = "SPRINT"
)
```

The set is closed at two values. It is validated by the application on every audit
write and enforced by the `entity_type` column `CHECK` (see
`DATABASE.md § audit Table`). A comment operation is recorded against the task or
the sprint that owns the comment, so no `COMMENT` value exists; the audit log has no
entity type beyond these two.

`audit history` accepts exactly these two values as its first positional argument
and rejects anything else with exit code 6, and `audit list --entity-type` accepts
exactly these two values (see `COMMANDS.md § Audit Log Management`).

### Audit Operation

Names what happened. Every row of the `audit` table carries one value of this type.

```go
type AuditOperation string
```

**`DATABASE.md § audit Table` is the canonical catalogue of the constant values, and
this section deliberately does not repeat them.** A value added to `ValidAuditOperations`
without being added to that catalogue is a defect, and a value in that catalogue that
no constant declares is the same defect from the other side.

Three rules govern the constants themselves:

1. **Every value is declared as an `AuditOperation` constant**, and
   `ValidAuditOperations` lists every declared constant exactly once, in the order
   the catalogue publishes them.
2. **The name states the outcome, not the kind of change.** A status change is named
   for the state entered (`TASK_STATUS_DOING`, not a generic status-change value),
   and a field edit is named for the field changed (`TASK_TITLE_CHANGE`, not a
   generic update value). The naming pattern is
   `<ENTITY>_<SUBJECT>_<OUTCOME>`: `TASK_STATUS_<STATE>` for the five task states,
   `<ENTITY>_<FIELD>_CHANGE` for a single-field edit, and `<ENTITY>_<VERB>` for
   everything else.
3. **A value the application no longer writes is not deleted.** It stays declared and
   stays in `ValidAuditOperations` so that the rows already carrying it remain
   reachable by an `--operation` filter, and the catalogue marks it LEGACY. Removing
   such a constant would leave its stored entries with no filter value that reaches
   them, which is the defect the catalogue's LEGACY group exists to prevent.

**Validation surface.** `IsValidAuditOperation(name string) bool` reports whether a
name is in the valid set, and `ParseAuditOperation(name string) (AuditOperation, error)`
returns the constant or `ErrInvalidAuditOperation`. Both treat LEGACY values as valid,
because they are readable, and both reject a name outside the set whether or not the
table happens to hold rows carrying it.

**Acceptance criteria:**

1. Every value in `ValidAuditOperations` appears exactly once in the canonical catalogue of `DATABASE.md § audit Table`, and every operation named in that catalogue appears in `ValidAuditOperations`.
2. `ValidAuditOperations` contains no duplicate and no empty value.
3. `IsValidAuditOperation` returns true for each of the four LEGACY values and false for `TASK_ASSIGN` and `TASK_UNASSIGN`.
4. `AuditEntry.Validate()` rejects an entry whose `Operation` is not in the valid set.

## Structures

### Task
Maps to the `tasks` table and `Task` JSON object.

**Field Length Constraints:**
- `Title`: Maximum 255 characters
- `FunctionalRequirements`: Maximum 4096 characters
- `TechnicalRequirements`: Maximum 4096 characters
- `AcceptanceCriteria`: Maximum 4096 characters
- `CompletionSummary`: Maximum 4096 characters (optional, set only on close)
- `CommitOpen`: 7 to 64 hexadecimal characters, lowercase (optional, set only on entry into `DOING`)
- `CommitClose`: 7 to 64 hexadecimal characters, lowercase (optional, set only on entry into `COMPLETED`)

**Commit Hash Constraint:**

The `CommitOpen` and `CommitClose` fields hold git commit hashes and share one
format rule. A value that the application stores MUST satisfy all of the
following:

1. It consists solely of hexadecimal characters (`0`-`9`, `a`-`f`, `A`-`F`).
2. Its length is at least 7 characters and at most 64 characters, inclusive. The
   lower bound admits the conventional abbreviated hash; the upper bound admits
   both the 40-character SHA-1 hash and the 64-character SHA-256 hash that a
   repository created with `git init --object-format=sha256` produces.
3. The application accepts the value in any letter case and **normalises it to
   lowercase before storing it**. Every stored value is therefore lowercase, and
   two callers who supply the same hash in different cases produce the same
   stored value.

The application applies no other transformation. It does not trim surrounding
whitespace, so a value carrying a leading or trailing space contains a
non-hexadecimal character and is rejected. An empty value is rejected, because
its length is below the lower bound. Every rejection uses exit code 6 and makes
no change to any task. The database enforces the same rule as a backstop through
a `CHECK` constraint on each column (see `DATABASE.md § tasks Table`).

The same rule governs `AuditEntry.CommitHash`, which receives the same normalised
value on the two transitions that write it. There is one format rule for commit
hashes in Groadmap, stated here and backstopped by an identical `CHECK` on all three
columns that store one (see `DATABASE.md § Commit Hash Format Constraint`).

Groadmap never derives these values. It runs no git command, reads no working
directory, and inspects no repository: the caller supplies the hash explicitly on
the command line (see `COMMANDS.md § Change Status (stat)`). The application
therefore does not verify that the hash names a commit that exists in any
repository; it validates the format alone.

**Free-Text Control-Character Constraint:**

All free-text fields — `Title`, `FunctionalRequirements`, `TechnicalRequirements`,
`AcceptanceCriteria`, and `CompletionSummary` (and the `Sprint` `Title` and
`Description` fields, and the `Body` field of `TaskComment` and
`SprintComment`) — MUST reject control characters. The application rejects an
input that contains any of the following code points, with exit code 6, before the
value is stored:

1. **ASCII control bytes below `0x20`**, with three exceptions that are permitted:
   TAB (`0x09`), LF (`0x0A`, line feed), and CR (`0x0D`, carriage return). Every
   other byte in the range `0x00`-`0x1F` is rejected.
2. **DEL (`0x7F`)**.
3. **Unicode bidirectional and format control code points:** `U+200E`
   (LEFT-TO-RIGHT MARK), `U+200F` (RIGHT-TO-LEFT MARK), `U+202A`-`U+202E`
   (the embedding and override controls), `U+2066`-`U+2069` (the isolate
   controls), and `U+FEFF` (zero-width no-break space / byte-order mark).

Rationale: forbidding these code points prevents terminal escape-sequence injection
(CWE-150) when field values are echoed to a terminal, and prevents Trojan Source
attacks (CVE-2021-42574) in which bidirectional control characters reorder how
text is displayed without changing its stored bytes. This constraint applies to
every field listed above on every command that accepts the field
(see `COMMANDS.md § Field Validation`).

```go
// Task represents a task in the roadmap.
// Field order optimized for memory layout (248 bytes, zero padding on 64-bit systems).
// Groups: Content (strings), Tracking (pointers), Metadata (ints), Dependencies (slices).
// All Group 1 fields are mandatory (NOT NULL) with enforced maximum lengths.
type Task struct {
    // Group 1: Content fields - frequently accessed together (112 bytes: 7 x 16)
    // All fields are mandatory (NOT NULL) with length constraints enforced by application
    Title                  string     `json:"title"`                    // Task title/summary, max 255 chars
    Status                 TaskStatus `json:"status"`                   // Current status
    Type                   TaskType   `json:"type"`                     // Task type classification
    FunctionalRequirements string     `json:"functional_requirements"`  // Why: functional requirements, max 4096 chars
    TechnicalRequirements  string     `json:"technical_requirements"`   // How: technical description, max 4096 chars
    AcceptanceCriteria     string     `json:"acceptance_criteria"`      // How to verify: completion criteria, max 4096 chars
    CreatedAt              string     `json:"created_at"`               // ISO 8601 UTC, auto-set on creation

    // Group 2: Nullable tracking fields - lifecycle timestamps, commit hashes, and parent link (56 bytes: 7 x 8)
    StartedAt          *string `json:"started_at"`           // ISO 8601 UTC, auto-set on DOING transition
    TestedAt           *string `json:"tested_at"`            // ISO 8601 UTC, auto-set on TESTING transition
    ClosedAt           *string `json:"closed_at"`            // ISO 8601 UTC, auto-set on COMPLETED transition
    CompletionSummary  *string `json:"completion_summary"`   // Optional summary of work done, settable only on TESTING → COMPLETED, max 4096 chars
    CommitOpen         *string `json:"commit_open"`          // Git commit hash the task was started from; mandatory on every transition into DOING, 7-64 lowercase hex chars
    CommitClose        *string `json:"commit_close"`         // Git commit hash the task was concluded at; mandatory on every transition into COMPLETED, 7-64 lowercase hex chars
    ParentTaskID       *int    `json:"parent_task_id"`       // NULL for top-level tasks; non-NULL links to parent task

    // Group 3: Numeric metadata fields (32 bytes: 4 x 8)
    ID           int `json:"id"`            // Primary key
    Priority     int `json:"priority"`      // 0-9 priority level
    Severity     int `json:"severity"`      // 0-9 severity level
    SubtaskCount int `json:"subtask_count"` // Computed: number of direct subtasks (not stored in DB)

    // Group 4: Dependency fields - fetched from task_dependencies table (48 bytes: 2 x 24 slice headers)
    DependsOn []int `json:"depends_on"` // IDs of tasks this task depends on (blocking this task)
    Blocks    []int `json:"blocks"`     // IDs of tasks that depend on this task (tasks this task is blocking)
}
```

**The block above groups the fields by role, for reading.** It is not the
declaration order the Go source must use. The struct occupies 248 bytes with zero
padding in either arrangement, because every field is 8-byte aligned, but the
`govet:fieldalignment` linter also governs the pointer-scan prefix and rejects this
reading order. `Memory Layout Optimization` below states the declaration order the
linter produces, and that order is the canonical one; copying the block above into
the Go source verbatim fails the lint validation gate.

### Sprint
Maps to the `sprints` table and `Sprint` JSON object.

```go
type Sprint struct {
    ID          int          `json:"id"`
    Status      SprintStatus `json:"status"`
    Title       string       `json:"title"`            // Sprint title, required (NOT NULL), max 255 chars
    Description string       `json:"description"`      // Sprint description, required (NOT NULL), max 2048 chars; states the sprint's high-level (macro) goal
    Tasks       []int        `json:"tasks"`            // Computed from sprint_tasks (ordered by position)
    TaskCount   int          `json:"task_count"`       // Computed
    CreatedAt   string       `json:"created_at"`
    StartedAt   *string      `json:"started_at"`       // Nullable
    ClosedAt    *string      `json:"closed_at"`        // Nullable
    MaxTasks    *int         `json:"max_tasks"`        // Nullable; NULL means unlimited capacity
    Order       int          `json:"order"`            // Sprint execution order; positive integer (> 0), unique across the roadmap; stored in column order_index
}
```

#### Sprint Field Constraints

- `Title`: Required (NOT NULL), maximum 255 characters. Same cap as the task `Title` field. Subject to the Free-Text Control-Character Constraint above.
- `Description`: Required (NOT NULL), maximum 2048 characters. Subject to the Free-Text Control-Character Constraint above. This field is the canonical statement of the sprint's purpose, and it carries the following semantics on every command that writes it (`sprint create` and `sprint update`):
  - The `Description` MUST state the high-level (macro) goal of the development effort that the sprint delivers: a new development, a fix, a refactoring, or another kind of change.
  - Together with the sprint `Title`, the `Description` MUST give a human reader or an AI agent a clear macro idea of what the sprint's tasks are specifically aimed at.
  - The `Description` states the macro goal only. Detailed scope, technical detail, and acceptance conditions do not belong in the `Description`: the tasks that compose the sprint specify them in full, through their `FunctionalRequirements`, `TechnicalRequirements`, and `AcceptanceCriteria` fields (see the `Task` model above).
  - The `Description` states the goal of the sprint as a whole. It does not enumerate the individual tasks of the sprint.

  See `COMMANDS.md § Create Sprint` and `COMMANDS.md § Update Sprint` for the flag that writes this field, and `HELP.md § Sprint family help specifics` for the help text that states these semantics to the caller.
- `Order`: Required (NOT NULL), positive integer strictly greater than zero (`> 0`), and unique across every sprint in the roadmap. It records the natural, sequential execution order of sprints: the sprint with the lowest `Order` value executes first. Two sprints can never share the same `Order` value. The value is auto-assigned on creation when the caller does not supply one (see `COMMANDS.md § Create Sprint`) and can be changed while the sprint is `PENDING` or `OPEN`. Once the sprint is `CLOSED`, the `Order` value becomes immutable and can never change again, because it then represents the historical execution record (see `STATE_MACHINE.md § Sprint Order Immutability`). The JSON field name is `order`; the underlying database column is named `order_index`, because `ORDER` is a reserved SQL keyword (see `DATABASE.md § sprints Table`).

### Task Comment
Maps to the `task_comments` table and the `TaskComment` JSON object.

A `TaskComment` is one entry in the durable log attached to a task. The log records exclusively the work carried out within the scope of that task: findings, hypotheses raised and tested, tests run, decisions taken, progress, the reason behind a change to the task's definition, and notes. Read oldest-first, the log shows how the work on that task progressed.

**Field Length Constraints:**
- `Body`: Required, minimum 1 character after trimming, maximum 4096 characters (`models.MaxCommentBody`)

```go
// TaskComment represents one comment attached to a task.
// Field order optimized for memory layout (72 bytes, zero padding on 64-bit systems).
type TaskComment struct {
    UpdatedAt *string     `json:"updated_at"`  // ISO 8601 UTC; null until the comment is first edited
    Type      CommentType `json:"type"`        // Mandatory classification; one of the seven task values
    Body      string      `json:"body"`        // Comment text, 1-4096 chars
    CreatedAt string      `json:"created_at"`  // ISO 8601 UTC, auto-set on creation, never changed
    ID        int         `json:"id"`          // Primary key, unique within task_comments only
    TaskID    int         `json:"task_id"`     // Owning task
}
```

#### Task Comment Field Constraints

- `Type`: Required. One of `ValidTaskCommentTypes`. There is no default: a comment without a type is rejected before it reaches the database. See [Comment Type](#comment-type).
- `Body`: Required, maximum 4096 characters. Leading and trailing whitespace is trimmed before validation; a value that is empty after trimming counts as absent. Subject to the Free-Text Control-Character Constraint above, so the body may contain TAB, LF, and CR but no other control character.
- `CreatedAt`: Set by the application when the comment is created and never modified afterwards.
- `UpdatedAt`: `null` while the comment has never been edited. Every edit sets it to the edit's timestamp, so a reader can see that the stored text is no longer the text originally written. The previous text is not retained and is not recoverable; the audit log records that the edit happened, not what was replaced (see `DATABASE.md § audit Table`).
- `ID`: Unique within `task_comments` only. Task comment ids and sprint comment ids are independent sequences.
- `TaskID`: The owning task. A comment never changes parent, and a comment cannot exist without its parent: deleting the task deletes its comments.

**No authorship.** A comment records no author. There is no author field, no `--author` flag, and no derivation of an author from the environment. This keeps the model consistent with `AuditEntry`, which records no actor either, and keeps command output deterministic.

**Lifecycle independence.** Comments are accepted on a task in every status, including `COMPLETED`: a finding made after the work closed is exactly the kind of entry the log exists for. `task reopen` clears the task's lifecycle timestamps and its completion summary and does not touch its comments (see `STATE_MACHINE.md § Task State Machine`).

**Not embedded in the task.** The `Task` struct carries no comment field and no comment count, and no task JSON output includes comments. Comments are read only through the comment listing commands and the read-only web interface.

### Sprint Comment
Maps to the `sprint_comments` table and the `SprintComment` JSON object.

A `SprintComment` is one entry in the durable log attached to a sprint. The log records only the progression of the work during the sprint's development: findings, decisions, progress, and the reason behind a change to the sprint's definition. Detailed per-task work belongs in that task's own comments.

**Field Length Constraints:**
- `Body`: Required, minimum 1 character after trimming, maximum 4096 characters (`models.MaxCommentBody`)

```go
// SprintComment represents one comment attached to a sprint.
// Field order optimized for memory layout (72 bytes, zero padding on 64-bit systems).
type SprintComment struct {
    UpdatedAt *string     `json:"updated_at"`  // ISO 8601 UTC; null until the comment is first edited
    Type      CommentType `json:"type"`        // Mandatory classification; one of the four sprint values
    Body      string      `json:"body"`        // Comment text, 1-4096 chars
    CreatedAt string      `json:"created_at"`  // ISO 8601 UTC, auto-set on creation, never changed
    ID        int         `json:"id"`          // Primary key, unique within sprint_comments only
    SprintID  int         `json:"sprint_id"`   // Owning sprint
}
```

#### Sprint Comment Field Constraints

Every constraint stated for `TaskComment` above applies to `SprintComment`, with two differences:

- `Type`: Required. One of `ValidSprintCommentTypes` — the four sprint values. `HYPOTHESIS`, `TEST`, and `NOTE` are rejected with exit code 6.
- `SprintID`: The owning sprint, in place of `TaskID`. Deleting the sprint deletes its comments. Removing or moving a task does not affect any sprint comment.

Comments are accepted on a sprint in every status, including `CLOSED`. The `Sprint` struct carries no comment field and no comment count.

### Audit Entry
Maps to the `audit` table and to the `AuditEntry` JSON object
(`DATA_FORMATS.md § Audit Entry`).

An `AuditEntry` is one immutable record of one thing that happened to one entity.
Nothing updates or deletes an entry once written.

```go
// AuditEntry represents one entry in the roadmap's audit log.
// Field order optimized for memory layout (80 bytes, zero padding on 64-bit systems).
type AuditEntry struct {
    RelatedEntityID *int    `json:"related_entity_id"` // Counterpart entity of the producing operation; nil when it has none
    CommitHash      *string `json:"commit_hash"`       // Git commit bracketing the work; nil on every operation but two
    Operation       string  `json:"operation"`         // One AuditOperation value, treated as opaque on read
    EntityType      string  `json:"entity_type"`       // One EntityType value: TASK or SPRINT
    PerformedAt     string  `json:"performed_at"`      // ISO 8601 UTC
    ID              int     `json:"id"`                // Primary key
    EntityID        int     `json:"entity_id"`         // The entity whose history this entry belongs to
}
```

#### Audit Entry Field Constraints

- `Operation`: Written from the `AuditOperation` catalogue. **On read it is an opaque
  string**: a stored row can carry a value the catalogue does not list, so no
  consumer may assume membership (see `DATABASE.md § audit Table`).
- `EntityType`: One of the two `EntityType` values.
- `EntityID`: The id of the task or the sprint whose history the entry belongs to.
  For a comment operation this is the parent task or sprint, never the comment.
- `RelatedEntityID`: The counterpart entity of the operation that produced the
  entry, or `nil` when that operation has no counterpart.
  `DATABASE.md § The Two Entities of a Relational Operation` is canonical for the
  rule and for the eight operation-and-command combinations that write it. Note that
  one operation value can carry it or not depending on the command that produced the
  entry: `TASK_STATUS_BACKLOG` names a sprint when `sprint remove-tasks` wrote it and
  is `nil` when `task stat` did, because only the first has a second entity party to
  it.
- `CommitHash`: The git commit bracketing a task's development work, or `nil`.
  Non-`nil` on exactly two operations, `TASK_STATUS_DOING` and
  `TASK_STATUS_COMPLETED`; `DATABASE.md § The Commit Hash of an Audit Entry` is
  canonical. The value satisfies the Commit Hash Constraint stated under `Task`
  above.
- `PerformedAt`: ISO 8601 UTC. Every entry a single command writes carries the same
  value.

**The two nullable fields are pointers, not empty values.** `RelatedEntityID` is a
`*int` and `CommitHash` a `*string` so that "no counterpart" and "no commit"
serialise as JSON `null` rather than as `0` and `""`. An entity id of `0` and an
empty hash are both invalid values that the database `CHECK` constraints reject, so a
non-pointer field could not distinguish absence from corruption.

**No authorship.** An audit entry records no actor. There is no author field and no
derivation of one from the environment, which is the same choice `TaskComment` and
`SprintComment` make.

**Acceptance criteria:**

1. `AuditEntry` measures 80 bytes on a 64-bit target, pinned by the struct-size test alongside the other domain structs.
2. Marshalling an entry whose `RelatedEntityID` and `CommitHash` are `nil` produces `"related_entity_id": null` and `"commit_hash": null`, never `0` or `""`.
3. Round-tripping an entry through the database and back preserves both nullable fields exactly, including the distinction between `nil` and a present value.

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
- **Source:** Computed from the `sprint_tasks` junction table which maintains the 1:N relationship between sprints and tasks (one sprint has many tasks; each task belongs to at most one sprint), including the `position` column.
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

### Sprint Show Result

Used for the `rmp sprint show` command. Provides a comprehensive sprint status report.

```go
// SeverityRangeCount represents count and percentage for a severity range.
type SeverityRangeCount struct {
    Count      int     `json:"count"`
    Percentage float64 `json:"percentage"`
}

// SeverityDistribution represents task distribution by severity ranges.
type SeverityDistribution struct {
    Range0To2 SeverityRangeCount `json:"0-2"`
    Range3To5 SeverityRangeCount `json:"3-5"`
    Range6To7 SeverityRangeCount `json:"6-7"`
    Range8To9 SeverityRangeCount `json:"8-9"`
}

// CriticalityDistribution represents task distribution by criticality level.
type CriticalityDistribution struct {
    Low      SeverityRangeCount `json:"low"`
    Medium   SeverityRangeCount `json:"medium"`
    High     SeverityRangeCount `json:"high"`
    Critical SeverityRangeCount `json:"critical"`
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
    SprintTitle             string                  `json:"sprint_title"`
    SprintDescription       string                  `json:"sprint_description"`
    Status                  SprintStatus            `json:"status"`
    Summary                 SprintSummary           `json:"summary"`
    Progress                SprintProgress          `json:"progress"`
    SeverityDistribution    SeverityDistribution    `json:"severity_distribution"`
    CriticalityDistribution CriticalityDistribution `json:"criticality_distribution"`
    TaskOrder               []int                   `json:"task_order"`   // Task IDs ordered by position
    CurrentLoad             int                     `json:"current_load"` // Number of tasks currently in sprint
    MaxTasks                *int                    `json:"max_tasks"`    // Nullable; NULL means unlimited
    CapacityPct             *float64                `json:"capacity_pct"` // Nullable; NULL when max_tasks is unset
}
```

**Field Descriptions:**

| Field | Type | Description |
|-------|------|-------------|
| `sprint_id` | int | Sprint identifier |
| `sprint_title` | string | Sprint title text |
| `sprint_description` | string | Sprint description text |
| `status` | SprintStatus | Current sprint status |
| `summary` | SprintSummary | Task counts by category (pending, in_progress, completed) |
| `progress` | SprintProgress | Percentage breakdown of task categories |
| `severity_distribution` | SeverityDistribution | Task counts per severity range (0-2, 3-5, 6-7, 8-9) |
| `criticality_distribution` | CriticalityDistribution | Task counts per criticality level (low, medium, high, critical) |
| `task_order` | []int | Task IDs ordered by position (ascending) |
| `current_load` | int | Total number of tasks in the sprint |
| `max_tasks` | *int | Capacity limit; null when unlimited |
| `capacity_pct` | *float64 | `(current_load / max_tasks) * 100`; null when `max_tasks` is null |

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

---

## Memory Layout Optimization

### Struct Field Ordering

Field order in all domain structs is enforced by the `govet:fieldalignment`
linter (see `BUILD.md § Enabled Linters`). The linter rearranges fields to
minimise padding on 64-bit targets; the resulting order is the canonical one
and must not be changed without also accepting the linter's revised order.

**Field sizes on 64-bit systems:**
- `*T` (pointer): 8 bytes, 8-byte aligned
- `string`: 16 bytes (pointer + length), 8-byte aligned
- `[]T` (slice header): 24 bytes (pointer + length + capacity), 8-byte aligned
- `map[K]V`: 8 bytes (header pointer), 8-byte aligned
- `int` / `float64`: 8 bytes, 8-byte aligned

**Task struct (248 bytes, zero padding on 64-bit):**
```
Group 1: Pointer fields (7 × 8 = 56 bytes)
  ParentTaskID, CompletionSummary, CommitOpen, CommitClose, TestedAt,
  ClosedAt, StartedAt

Group 2: String fields (7 × 16 = 112 bytes)
  AcceptanceCriteria, CreatedAt, Status, TechnicalRequirements,
  FunctionalRequirements, Type, Title
  (Status and Type are string-typed enums.)

Group 3: Slice fields (2 × 24 = 48 bytes)
  DependsOn, Blocks

Group 4: Int fields (4 × 8 = 32 bytes)
  ID, Priority, Severity, SubtaskCount
```

**TaskComment and SprintComment structs (72 bytes each, zero padding on 64-bit):**
```
Group 1: Pointer field (1 × 8 = 8 bytes)
  UpdatedAt

Group 2: String fields (3 × 16 = 48 bytes)
  Type, Body, CreatedAt
  (Type is a string-typed enum.)

Group 3: Int fields (2 × 8 = 16 bytes)
  ID and the parent id (TaskID or SprintID)
```

The two structs have identical layouts and differ only in the name of the
parent-id field. Every field is 8-byte aligned, so no ordering introduces
padding, and the byte count is 72 whatever the order. What the order decides is
the pointer-scan prefix, and that is what `fieldalignment` enforces here: with
the `*string` first and the three string headers after it, the last word that can
hold a pointer ends at byte 48, and the two `int` fields, which hold no pointer,
trail. Moving `UpdatedAt` after the strings pushes that boundary to byte 56 and
the linter rejects the struct with "struct with 56 pointer bytes could be 48".

**AuditEntry struct (80 bytes, zero padding on 64-bit):**
```
Group 1: Pointer fields (2 × 8 = 16 bytes)
  RelatedEntityID, CommitHash

Group 2: String fields (3 × 16 = 48 bytes)
  Operation, EntityType, PerformedAt

Group 3: Int fields (2 × 8 = 16 bytes)
  ID, EntityID
```

Every field is 8-byte aligned, so the byte count is 80 whatever the order; what the
order decides is the pointer-scan prefix. With the two pointers first and the three
string headers after them, the last word that can hold a pointer ends at byte 56, and
the two `int` fields trail. Putting the `int` fields anywhere before `PerformedAt`
pushes that boundary out and `fieldalignment` rejects the struct. This is the same
grouping `TaskComment` and `SprintComment` follow.

**SprintStats struct (112 bytes, zero padding on 64-bit):**
```
Group 1: Reference-type fields (3 × 8 = 24 bytes)
  StatusDistribution (map header), DaysElapsed (*int), DaysRemaining (*int)

Group 2: Slice fields (2 × 24 = 48 bytes)
  TaskOrder, Burndown

Group 3: Int + float fields (5 × 8 = 40 bytes)
  SprintID, TotalTasks, CompletedTasks, ProgressPercentage, Velocity
```

**Rationale:**
- Largest-alignment fields go first so the compiler does not insert padding
  between groups of differing alignment.
- Pointer/string/slice groups stay together because their header sizes line
  up with the 8-byte word boundary.
- Int/float scalars trail because they are the smallest naturally aligned
  group and absorb any remainder.

**Sprint-Task Relationship (1:N — one sprint to many tasks; each task in at most one sprint):**

The relationship between sprints and tasks is maintained in the `sprint_tasks` junction table. While structurally a junction table, the `UNIQUE` constraint on `task_id` enforces that any task belongs to at most one sprint at a time:

```
sprint_tasks table:
- sprint_id (FK to sprints.id)
- task_id (FK to tasks.id)
- position (int) -- Execution order within the sprint (0 = first, 1 = second, ...)
```

**Task ordering semantics:**
- The `position` column in `sprint_tasks` defines the execution sequence of tasks within a sprint
- Position 0 represents the highest priority task that should be executed first
- Tasks with the same sprint_id are ordered by position ASC
- The `task_order` field in SprintStats is derived from this position ordering

### Cache Line Considerations

The Task struct (248 bytes) spans approximately 4 cache lines (64 bytes each).
The `fieldalignment`-driven grouping keeps fields of the same kind contiguous,
so common access patterns (e.g. iterating the pointer or string groups during
display) stay within a small number of cache lines.
