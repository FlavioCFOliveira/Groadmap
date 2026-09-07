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

The name by which a validation message identifies one of these fields is the
field's published name, specified once in
`COMMANDS.md § Published Field Names in Validation Messages`. That section is
canonical for the name and lists it for every free-text field.

**Free-Text UTF-8 Encoding Constraint:**

Every free-text field holds text encoded as UTF-8, and only text encoded as UTF-8.
The application rejects an input whose bytes are not a valid UTF-8 sequence, with
exit code 6, before the value is stored. This rule governs exactly the fields the
Free-Text Control-Character Constraint above governs, and that constraint is
canonical for the set. Read the two constraints together. They apply to the same
fields on the same commands, and each one on its own is sufficient grounds to refuse
an input.

A byte sequence is valid UTF-8 when it is well-formed under the Unicode Standard,
Table 3-7 ("Well-Formed UTF-8 Byte Sequences"). The application therefore rejects,
among other malformed sequences:

1. A continuation byte (`0x80`-`0xBF`) that no lead byte introduces.
2. A byte that never occurs in valid UTF-8 at all: `0xC0`, `0xC1`, and `0xF5`-`0xFF`.
3. An overlong encoding, meaning a code point written with more bytes than its
   shortest form requires, such as `0xC0 0xAF` for `/` (`U+002F`).
4. A surrogate code point, `U+D800`-`U+DFFF`, written as the three bytes
   `0xED 0xA0 0x80` through `0xED 0xBF 0xBF`. Surrogate code points are not
   characters and have no UTF-8 encoding.
5. A sequence that a lead byte begins and that the input ends before completing.

**Both input paths.** The rule binds the value as the caller supplied it, whichever
way the value reached the application: as the value of a command-line flag, or as the
byte stream the application reads from standard input. The comment `Body` is the only
free-text field that has a standard-input source (see `COMMANDS.md § Comment Body
Input Source and Precedence`); every other free-text field arrives by flag alone.
The application reads that stream under a bounded budget instead of holding the whole
value in memory, and this rule leaves the standing property of that bounded read
intact: the verdict the caller sees is exactly the verdict a read-to-EOF
implementation would reach. The bounded read therefore carries the bytes it read
forward exactly as they arrived, invalid bytes included, and never substitutes a
replacement character for one; this rule is then applied to those bytes in the order
stated below, and reaches on them the verdict it would reach on the same bytes
supplied through a flag.

Decoding a stream in pieces raises one question, and item 5 above settles it. A
multi-byte sequence that one read ends inside is not malformed for that reason: item
5 is about the input ending, not about a read ending, so the application waits for
the bytes that would complete the sequence. A sequence that a lead byte begins and
that the input itself ends before completing is malformed under item 5, and the
application refuses it.

**Refusal.** A value that is not valid UTF-8 is a validation failure, in the same
class and with the same exit code, 6, as a value that carries a forbidden control
character. It carries its own message. The message body is
`<field>: the value is not valid UTF-8`, and the full line the application writes to
standard error is
`Error: validation error: <field>: the value is not valid UTF-8`. `<field>` is the
field's published name, specified by `COMMANDS.md § Published Field Names in
Validation Messages`, which is canonical for it. That section provides for a rule
added later over the same fields, and this constraint is one: the name resolves
there, and this constraint does not restate the mapping. The application stores nothing and changes
no entity when it refuses.

**Order.** The application applies this rule immediately before the Free-Text
Control-Character Constraint, on every command and for every field the two rules
govern. The control-character rule is defined over decoded code points, so it is only
meaningful once the bytes are known to decode, and checking the encoding first is
what makes it so. Both checks run on the value **as the caller supplied it**, and both
run after the field's length cap, which answers first. The whole sequence is the length
cap, the encoding check, the control-character check, the trim, and last the emptiness
judgement, and it does not vary by command, by field, or by the way the value reached
the application. The Free-Text Emptiness and Trimming Constraint below states that
sequence in full and is canonical for it;
`COMMANDS.md § UTF-8 Encoding Constraint (All Free-Text Fields)` states the same order
once for every command that writes a free-text field, together with the refusal each
step produces (see also `COMMANDS.md § Field Validation`).

One consequence of that order is deliberate and MUST be preserved. Because the length
cap answers first, a value that is at once longer than the limit and not valid UTF-8 is
refused for its length and not for its encoding: the caller sees the
`field exceeds maximum size` refusal and never the encoding message. Every one of the
eight free-text fields is such a field, on every command that writes one, on the flag
path and on the standard-input path alike, and every one of them must remain one.

The reason the cap holds that position is the comment `Body`, the one free-text field
that has a standard-input source. There the length verdict is fixed the moment the value
cannot fit within the cap, and the bounded read stops at that point without ever
materialising the whole input; applying the encoding rule ahead of the limit would both
change that verdict and defeat the bounded read, because a reader cannot judge the
encoding of bytes it has refused to read. Cap-first is therefore the only order every
field and every command can share, and the other write paths were moved onto the comment
path's order rather than the reverse.

The verdict that order produces is well defined and not an accident of it. The length
the application measures is defined on malformed input as well: each byte that decodes
to no valid rune counts as one, so the cap is answerable on a value whose encoding has
not been established, and **Length limits are unchanged** below states how that count
relates to SQLite's `length()`. The trim the measurement runs on is equally safe there,
because it removes only whitespace code points and no byte that fails to decode is one,
so the trim can neither introduce nor remove an encoding failure.

Rationale: rejection is the only outcome under which what the caller supplied, what
the database stores, and what every reader prints are the same string. Without this
rule an invalid byte reaches the `TEXT` column verbatim, while the JSON encoder that
produces command output replaces each such byte with `U+FFFD` (REPLACEMENT
CHARACTER). The reported value then differs both from the stored value and from the
supplied one, so the JSON output that `DATA_FORMATS.md` documents cannot round-trip
what the field holds. Two other outcomes were considered and declined. Replacing the
invalid bytes with `U+FFFD` at the boundary would make the stored value agree with
the printed one, but it would silently alter what the caller wrote, so what is
stored would no longer be what was supplied. Documenting the behaviour as it stood
would leave the divergence between the stored value and the reported value in place,
and that divergence is the defect. This constraint is not a defence against
injection or escape: the application binds every value as a SQL parameter, and the
web interface escapes every value for its context (see `WEB.md`). It exists for data
integrity and for the output contract.

**Length limits are unchanged.** This rule changes nothing about the maximum length
each field carries or how the application measures that length; where the measurement
falls in the sequence of checks is stated in **Order** above. Two facts about those
limits hold both before and after this rule, and a later change must preserve both.
First, the count the application measures is never smaller than the count SQLite's
`length()` function returns for the same stored value, because `length()` counts only
the bytes of a value that are not UTF-8 continuation bytes. Second, and in
consequence, the `CHECK(length(<column>) <= <n>)` constraints that `DATABASE.md`
defines cannot be tripped by an input the application accepted: they are an
independent backstop, not the operative check. Neither count is to be brought into
line with the other on the strength of this rule.

**Where the two constraints apply beyond the model's own fields:**

The two constraints above are stated here for the free-text fields of the `Task`,
`Sprint`, `TaskComment`, and `SprintComment` models, and this file stays canonical
for what each rule forbids.

**Neither constraint reaches the knowledge graph.** The Cypher `rmp graph execute`
runs is not inspected for either rule, and neither is a property value that Cypher
writes: a statement whose raw bytes are not valid UTF-8 is executed with `U+FFFD`
substituted for each undecodable byte, and a property value carrying a control
character is stored as supplied. `GRAPH.md § What Groadmap Does Not Check`,
items 2 and 3, is canonical for the outcome of each. A Cypher property value is not
a free-text field of the `Task` or `Sprint` model, and the statement above that the
encoding rule governs exactly the fields the control-character rule governs is a
statement about this model's free-text **fields**, which stands unchanged.

**Free-Text Emptiness and Trimming Constraint:**

Two rules govern the whitespace at the edges of a free-text value. They answer
different questions, and this specification keeps them apart on purpose: a change to
one is not a change to the other, and an implementation can satisfy either one while
breaking the other.

**Rule 1 — when a required field is judged empty: after trimming.** A free-text field
that is required to be non-empty is judged against the value that remains once
leading and trailing whitespace has been removed. A value made only of whitespace
leaves nothing behind and counts as absent, so the command refuses it, stores nothing,
and changes no entity. The rule governs the seven free-text fields that are required
to be non-empty:

1. Task `Title`, `FunctionalRequirements`, `TechnicalRequirements`, and
   `AcceptanceCriteria`.
2. Sprint `Title` and `Description`.
3. The `Body` of `TaskComment` and `SprintComment`.

Task `CompletionSummary` is the one free-text field Rule 1 does not govern, and the
reason is that the field is optional: `task stat` accepts a transition to `COMPLETED`
that supplies no `--summary` at all, so a supplied value that is empty after trimming
is a summary the caller chose not to write rather than a violation. Rule 2 below
still governs it.

Rule 1 binds every command that writes one of the seven fields, and it does not vary
between them. `task create` and `task edit` judge a task field by this criterion
alike; `sprint create` and `sprint update` judge a sprint field by it alike; the four
comment subcommands judge a `Body` by it alike. What varies is the refusal, not the
criterion: `task create`, `task edit`, `sprint create`, and `sprint update` refuse
with exit code 6 and a message naming the field, while the comment `Body` is refused
with exit code 2 under a rule of its own that predates this constraint and that this
constraint leaves untouched (`COMMANDS.md § Comment Body Input Source and
Precedence`). `COMMANDS.md § Emptiness Constraint (All Required Free-Text Fields)` is
canonical for every refusal; this constraint is canonical for the criterion.

**Rule 2 — what is stored: the trimmed value.** The application removes leading and
trailing whitespace before the value reaches the database. This holds for all eight
free-text fields, `CompletionSummary` included, and on every command that writes one.
The stored value is therefore not the value as supplied whenever the two differ, and
two callers who supply the same text with different surrounding whitespace store the
same value. Interior whitespace is never altered: no line break, and no interior run
of spaces, is removed or collapsed by this rule. A comment body is expected to be
multi-line and survives as written (see `COMMANDS.md § Comment Body Input Source and
Precedence`).

One consequence of Rule 2 is required and MUST be preserved: **the field's maximum
length is measured on the trimmed value**, which is the same value the database
stores. A value of exactly the maximum length carrying surrounding whitespace is
therefore accepted, and what the application counted is what the column holds.
Measuring the value as supplied instead would refuse an input that the stored value
would have accommodated, and would leave the count the application checked different
from the count in the column — the same class of divergence between what the caller
supplied, what the database stores, and what a reader is shown that the Free-Text
UTF-8 Encoding Constraint above exists to close. Rule 2 fixes **what** the length
check measures. **When** it runs is fixed once, for every command, by the Free-Text UTF-8
Encoding Constraint above: the cap answers first, ahead of both content checks, and that
position does not vary by command.

**Order, and why a single trim is not enough.** The value is examined in two forms — as
the caller supplied it, and as it will be stored — and exactly one trim produces the
second form. The sequence is:

1. The field's maximum length is checked against the value as it will be **stored**,
   which Rule 2 above defines as the trimmed value.
2. The Free-Text UTF-8 Encoding Constraint, and immediately after it the Free-Text
   Control-Character Constraint, are applied to the value **as the caller supplied
   it**, before any whitespace is removed.
3. The value is trimmed.
4. Rule 1 is applied to the **trimmed** value.

Step 1 and step 2 examine different strings on purpose: the cap must measure what the
column holds, for the reason Rule 2 above gives, while the two content rules must see
the bytes the caller sent, for the reason the next paragraph gives. Step 1's position
relative to step 4 changes no verdict in either direction, because a value that trims to
nothing is zero characters long, so no input exists that both steps could answer for.

Step 2 MUST NOT be moved after step 3, and the reason is specific rather than
stylistic. VT (`0x0B`, line tabulation) and FF (`0x0C`, form feed) are forbidden
control characters under the Free-Text Control-Character Constraint above, and they
are also whitespace under the definition the trim applies. Trimming first removes a
leading or trailing VT or FF, and the control-character check then examines a value
from which the forbidden character has already vanished: the input is accepted, the
character is discarded in silence, and the protection against terminal escape-sequence
injection (CWE-150) that the constraint exists to provide fails at exactly the
position where such a character is easiest to hide. Applying the encoding and
control-character checks to the value as supplied is what closes that hole, and this
constraint does not move them.

**Both facts hold at the same time, and neither weakens the other: emptiness is
judged after a trim, and control characters are judged before it.** The reading to
avoid is the intuitive one — trim once on the way in, then run every check on the
result. That reading satisfies Rule 1 and breaks the Control-Character Constraint.
The sequence above is the one that satisfies both, and any validator written or
refactored for one of these fields MUST implement it.

One observable consequence follows, and a test MUST pin it: a value whose only
content is a forbidden control character that the trim would also remove — a value
consisting solely of VT, for instance — is refused as a control-character violation
and **not** as an empty value, because the control-character check reaches it first.
That refusal is the visible signature of the order being correct, and it changes the
moment the trim moves ahead of the check.

**Acceptance criteria:**

1. For each of the seven fields Rule 1 governs, and on each command that writes it, a
   value made only of spaces is refused and the entity is neither created nor changed.
   The exit code is 6 for the task and sprint fields and 2 for the comment `Body`, as
   `COMMANDS.md § Emptiness Constraint (All Required Free-Text Fields)` states.
2. The same is true of a value made only of TAB, of LF, of CR, of any mixture of them,
   or of a whitespace character outside ASCII such as `U+00A0` (no-break space) or
   `U+0085` (NEL): the criterion is what the trim leaves behind, not which whitespace
   character the caller supplied.
3. On every command that writes a free-text field, a value carrying surrounding
   whitespace and a non-empty core is accepted, and the value read back afterwards is
   the trimmed one.
4. A value of exactly a field's maximum length carrying surrounding whitespace is
   accepted on every command that writes that field.
5. A value carrying a leading or trailing VT or FF is refused with exit code 6 as a
   control-character violation on every command that writes a free-text field, and
   the refusal names the control-character rule and not the emptiness rule.
6. A value consisting solely of VT is refused as a control-character violation, not
   as an empty value.
7. Moving the control-character check onto the trimmed value fails at least one test.
8. On every command that writes a free-text field, a value that is at once longer than
   the field's maximum and not valid UTF-8 is refused for its **length**. Both refusals
   carry exit code 6, so the exit code alone establishes nothing about the order; the
   criterion is that the message is
   `Error: field exceeds maximum size: <field> exceeds maximum length of N characters`
   and not `Error: validation error: <field>: the value is not valid UTF-8`.
9. The same holds for a value that is at once longer than the maximum and carrying a
   forbidden control character: the refusal names the length and not the control
   character. Here too both refusals exit 6, and the exit code distinguishes nothing.
10. A value of exactly the field's maximum length that carries a forbidden control
    character is refused as a control-character violation, and a value of exactly that
    length that is not valid UTF-8 is refused for its encoding. Reaching the cap is
    therefore never a way past the two content rules: step 1 answers only for a value
    that exceeds the maximum.
11. Criteria 8 to 10 hold on every command that writes a free-text field and for every
    field that command writes, and on the comment `Body`'s standard-input path as well
    as its flag path. A check made on one command and one field alone would pass while
    another command disagreed, and that divergence is what these criteria exist to
    exclude.

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
    Tasks       []int        `json:"tasks"`            // Computed from sprint_tasks: member task IDs, ascending ID order
    TaskCount   int          `json:"task_count"`       // Computed from sprint_tasks: number of member tasks
    CreatedAt   string       `json:"created_at"`
    StartedAt   *string      `json:"started_at"`       // Nullable
    ClosedAt    *string      `json:"closed_at"`        // Nullable
    MaxTasks    *int         `json:"max_tasks"`        // Nullable; NULL means unlimited capacity
    Order       int          `json:"order"`            // Sprint execution order; positive integer (> 0), unique across the roadmap; stored in column order_index
}
```

#### Sprint Field Constraints

- `Title`: Required (NOT NULL), maximum 255 characters. Same cap as the task `Title` field. Subject to the Free-Text Control-Character Constraint, the Free-Text UTF-8 Encoding Constraint, and the Free-Text Emptiness and Trimming Constraint above, so a value that is empty after trimming is refused on `sprint create` and `sprint update` alike, and the value stored is the trimmed one.
- `Description`: Required (NOT NULL), maximum 2048 characters. Subject to the Free-Text Control-Character Constraint, the Free-Text UTF-8 Encoding Constraint, and the Free-Text Emptiness and Trimming Constraint above, on the same terms as the sprint `Title`. This field is the canonical statement of the sprint's purpose, and it carries the following semantics on every command that writes it (`sprint create` and `sprint update`):
  - The `Description` MUST state the high-level (macro) goal of the development effort that the sprint delivers: a new development, a fix, a refactoring, or another kind of change.
  - Together with the sprint `Title`, the `Description` MUST give a human reader or an AI agent a clear macro idea of what the sprint's tasks are specifically aimed at.
  - The `Description` states the macro goal only. Detailed scope, technical detail, and acceptance conditions do not belong in the `Description`: the tasks that compose the sprint specify them in full, through their `FunctionalRequirements`, `TechnicalRequirements`, and `AcceptanceCriteria` fields (see the `Task` model above).
  - The `Description` states the goal of the sprint as a whole. It does not enumerate the individual tasks of the sprint.

  See `COMMANDS.md § Create Sprint` and `COMMANDS.md § Update Sprint` for the flag that writes this field, and `HELP.md § Sprint family help specifics` for the help text that states these semantics to the caller.
- `Tasks`: Computed from the `sprint_tasks` junction table on every read; never stored in the `sprints` table. The field carries the **ids** of the sprint's member tasks as integers — task identifiers, not task objects, and not titles. The ids are ordered by ascending task id. That order is deliberately not the sprint's planned in-sprint execution order: the planned order is the `sprint_tasks.position` column, and a caller that needs it reads `COMMANDS.md § List Sprint Tasks` or the `task_order` field of `COMMANDS.md § Sprint Statistics` (see `DATABASE.md § List by Sprint`). Membership does not depend on task status: a member task is listed whatever its status, including a member task whose status is `BACKLOG` (see `STATE_MACHINE.md § Sprint Membership and the BACKLOG Status`). A sprint that holds no task carries the empty array `[]`, never `null` (see `DATA_FORMATS.md § Implementation Notes`, Empty arrays).
- `TaskCount`: Computed from the `sprint_tasks` junction table on every read; never stored in the `sprints` table. It is the number of tasks that belong to the sprint, and it therefore always equals the number of entries in `Tasks` on the same object. It counts member tasks in every status, on the same membership rule `Tasks` follows. A sprint that holds no task carries `0`.
- **Both computed fields are populated on every read that returns a `Sprint` object.** `rmp sprint get` populates them for the sprint it returns, and `rmp sprint list` populates them for every sprint in the array it returns, including when the listing is narrowed by `--status`. No command returns a `Sprint` object with `Tasks` or `TaskCount` left unresolved, so a reader never has to issue a second command to learn what a returned sprint holds, and two reads of the same sprint at the same moment never report different membership. See `COMMANDS.md § List Sprints`, `COMMANDS.md § Get Sprint`, and `DATABASE.md § Read the Membership of Many Sprints (Grouped)`.
- `Order`: Required (NOT NULL), positive integer strictly greater than zero (`> 0`), and unique across every sprint in the roadmap. It records the natural, sequential execution order of sprints: the sprint with the lowest `Order` value executes first. It is also the order in which `rmp sprint list` returns a roadmap's sprints, lowest first (see `COMMANDS.md § List Sprints`). Two sprints can never share the same `Order` value. The value is auto-assigned on creation when the caller does not supply one (see `COMMANDS.md § Create Sprint`) and can be changed while the sprint is `PENDING` or `OPEN`. Once the sprint is `CLOSED`, the `Order` value becomes immutable and can never change again, because it then represents the historical execution record (see `STATE_MACHINE.md § Sprint Order Immutability`). The JSON field name is `order`; the underlying database column is named `order_index`, because `ORDER` is a reserved SQL keyword (see `DATABASE.md § sprints Table`).

### Task Comment
Maps to the `task_comments` table and the `TaskComment` JSON object.

A `TaskComment` is one entry in the durable log attached to a task. The log records exclusively the work carried out within the scope of that task: findings, hypotheses raised and tested, tests run, decisions taken, progress, the reason behind a change to the task's definition, and notes. Read oldest-first, the log shows how the work on that task progressed.

**Field Length Constraints:**
- `Body`: Required, minimum 1 character after trimming, maximum 4096 characters (`models.MaxCommentBody`). Both bounds are measured on the trimmed value (Free-Text Emptiness and Trimming Constraint above).

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
- `Body`: Required, maximum 4096 characters. Subject to the Free-Text Control-Character Constraint, the Free-Text UTF-8 Encoding Constraint, and the Free-Text Emptiness and Trimming Constraint above, so the body may contain TAB, LF, and CR but no other control character, its bytes must be valid UTF-8, a value that is empty after trimming counts as absent, and the value stored is the trimmed one. The `Body` is the one required free-text field whose refusal of an empty value is not exit code 6: it is refused with exit code 2, under a rule of its own that predates this constraint (see `COMMANDS.md § Comment Body Input Source and Precedence`).
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
- `Body`: Required, minimum 1 character after trimming, maximum 4096 characters (`models.MaxCommentBody`). Both bounds are measured on the trimmed value (Free-Text Emptiness and Trimming Constraint above).

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
