# Project Status Report (Ponto de Situação — PDS)

> Template for the PDS report. Replace the example values with the real values obtained via `rmp` (see "Data-collection sequence" in `SKILL.md`). Render the report in the **user's language**; this template is written in English. Base every statement on actual CLI output — never on assumption. All ordering and metrics come from `rmp`.

## Executive summary

Two-to-four lines on the overall project state.

- **Current sprint**: `<N>` completed, `<N>` in progress (DOING + TESTING), `<N>` not yet started (SPRINT).
- **Pending sprints**: `<N>` sprints in `PENDING`, `<N>` tasks in total.
- **Closed sprints**: `<N>` sprints `CLOSED`, `<N>` tasks in total.
- **Global progress**: `<X>%` complete (over all tasks in the roadmap).
- **Average velocity (last 5 closed sprints)**: `<float>`.
- **Interrupted tasks**: `<N>` started but not finished (see the flag below).
- **Active impediments**: `<N>` tasks blocked by incomplete dependencies.

> If there is no OPEN sprint, say so explicitly and move directly to the planning view (backlog + pending sprints).

---

## Current sprint

- **ID**: `<sprint-id>`
- **Title**: `<sprint title>`
- **Description (macro goal)**: `<sprint description>`
- **Started at**: `<YYYY-MM-DD>`
- **Progress**: `<X>%` (`<completed>/<total>`)
- **Capacity** (if `max_tasks` is set): `<current_load>/<max_tasks>` (`<capacity_pct>%`)
- **Velocity**: `<velocity>` (from `sprint stats`)

A short summary of the sprint's objective and of the tasks in progress vs. completed, framed by the macro goal. Draw the narrative from the sprint's own comment log (`sprint comment-list <sid>`) — never invent one.

### Sprint tasks

Order: `COMPLETED` → `TESTING` → `DOING` → `SPRINT`.

|  ID | Title            | Type | Severity | Priority |      Status | Closing commit | Closed at    |
|----:|:-----------------|:-----|---------:|---------:|------------:|:---------------|:-------------|
| 001 | Task title       | TASK |        9 |        9 |   COMPLETED | `e8a9d9a4`     | `YYYY-MM-DD` |
| 002 | Task title       | BUG  |        8 |        8 |     TESTING |                |              |
| 003 | Task title       | TASK |        7 |        7 |       DOING |                |              |
| 004 | Task title       | TASK |        6 |        6 |      SPRINT |                |              |

Show `commit_close` for completed tasks (and `commit_open` when the question is where in-flight work started); omit the column entirely if no task in the sprint carries a hash.

**Interrupted tasks** (started but not closed — resume before starting new work): list any `DOING`/`TESTING` task with `started_at` set and `closed_at` null, with how long it has been stalled (from the audit log).

**Impediments**: list any task with incomplete blockers (`task blockers <id>`), naming what blocks it.

**Decisions taken this sprint**: the choices that shaped the work, drawn from the logs — the sprint's own `DECISION` entries (`sprint comment-list <sid> --type DECISION`) plus those of its tasks (`task comment-list <id> --type DECISION`). One line each: the decision, and the reason it was taken. This is the section a reader returns to months later; it is built by reading, never by inference. If a sprint recorded no decisions, say so plainly rather than summarising the work as though it had.

---

## Next sprints (PENDING)

Sprints already created but not yet started, with their intended macro goals.

| Sprint ID | Title            | Description (macro goal) | Tasks |
|----------:|:-----------------|:-------------------------|------:|
|       005 | Sprint title     | Sprint macro goal        |    12 |
|       006 | Sprint title     | Sprint macro goal        |     8 |

---

## Closed sprints (CLOSED)

History of closed sprints, with start and close dates. The closing summary lives in each sprint's comment log, not in its description (see "Close a sprint with carryover" in `references/workflows.md`). Read the log (`sprint comment-list <sid>`) for what actually happened in each — including any carryover recorded as a `DECISION`.

| Sprint ID | Title            | Tasks | Started      | Closed       |
|----------:|:-----------------|------:|:-------------|:-------------|
|       001 | Sprint title     |    14 | `YYYY-MM-DD` | `YYYY-MM-DD` |
|       002 | Sprint title     |    11 | `YYYY-MM-DD` | `YYYY-MM-DD` |

---

## Backlog

Tasks in `BACKLOG`, ordered by priority descending. Not yet assigned to any sprint.

|  ID | Title            | Type        | Severity | Priority | Created at   |
|----:|:-----------------|:------------|---------:|---------:|:-------------|
| 042 | Task title       | TASK        |        7 |        8 | `YYYY-MM-DD` |
| 043 | Task title       | BUG         |        9 |        7 | `YYYY-MM-DD` |
| 044 | Task title       | IMPROVEMENT |        5 |        6 | `YYYY-MM-DD` |

---

## Metrics

- **Sprint progress** = `completed_tasks / total_tasks × 100` (from `sprint stats`).
- **Global progress** = `tasks.completed / Σ tasks × 100` (from `rmp stats`).
- **Velocity** = `average_velocity` (global, last 5 closed sprints, from `rmp stats`) and per-sprint `velocity` (from `sprint stats <id>`).
- **No timebox**: `days_remaining` is always `null`; do not report or promise deadlines.
