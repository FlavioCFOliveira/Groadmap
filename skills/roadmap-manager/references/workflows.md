# Extended workflow library

Load this file when the work goes beyond the three top workflows inlined in `SKILL.md` (execute next task, bootstrap project, plan & start a new sprint). Replace `<rdm>` with the target roadmap name. Every `sprint create` here requires both a title (`-t`) and a macro-goal description (`-d`); every `→ DOING` requires `--commit-open` and every `→ COMPLETED` requires `--commit-close`.

## Table of contents

1. [Identify the next task(s) to execute](#1-identify-the-next-tasks-to-execute)
2. [Resume an interrupted task](#2-resume-an-interrupted-task)
3. [Keep the task work log](#3-keep-the-task-work-log)
3b. [Read the work log — recover history and reasoning](#3b-read-the-work-log--recover-history-and-reasoning)
4. [Keep the sprint log](#4-keep-the-sprint-log)
5. [Close a sprint with carryover](#5-close-a-sprint-with-carryover)
6. [Move a task between sprints](#6-move-a-task-between-sprints)
7. [Dependency & impediment management](#7-dependency--impediment-management)
8. [Subtask decomposition](#8-subtask-decomposition)
9. [Backlog refinement (grooming)](#9-backlog-refinement-grooming)
10. [Reprioritise the backlog](#10-reprioritise-the-backlog)
11. [Backlog triage](#11-backlog-triage)

---

## 1. Identify the next task(s) to execute

Only OPEN-sprint tasks are executable. Resolve "what's next" strictly from the CLI, in precedence order.

```bash
rmp sprint list -r <rdm> --status OPEN            # is there an OPEN sprint at all?
# 1) Interrupted work first (see workflow 2): tasks already DOING/TESTING in the OPEN sprint.
rmp task list -r <rdm> -s DOING
rmp task list -r <rdm> -s TESTING
# 2) Otherwise the next tasks of the OPEN sprint, in POSITION order:
rmp task next -r <rdm> [N]                        # default N = 1
rmp task blockers <id> -r <rdm>                   # run per candidate — task next does NOT filter blocked tasks
```

- `task next` is the **single** source for "what to develop next"; it only returns OPEN-sprint tasks, ordered by **sprint position ascending**, and it returns **one** task unless N is given.
- **Position, not priority, is the execution order.** To change what comes next, change the position (`sprint top`, `sprint move-to`, `sprint reorder`) — bumping priority will not move it. `sprint open-tasks <sid> --order-by-priority` gives the priority-ranked view of the same set when that is what the user wants to see.
- **`task next` does not filter out blocked tasks.** It will happily return a task whose blockers are incomplete; completing it would then fail with exit 6. Check `task blockers` on each candidate and set the blocked ones aside.
- No OPEN sprint → `task next` exits 4. There is nothing to execute: switch to planning (workflow C in `SKILL.md`), do not run backlog tasks.
- **`sprint next` does not exist** — invoking it exits 127 (unknown subcommand).
- To pull a backlog / other-sprint task into scope, that is a **planning action requiring confirmation**: `sprint add-tasks` (backlog → OPEN sprint) or `sprint move-tasks` (other sprint → destination).

Present each candidate with id, title, priority/severity, status, position, blockers, and the OPEN sprint's purpose (title + description) as context.

---

## 2. Resume an interrupted task

Resuming half-done work takes priority over starting new work.

```bash
# Detect stalled tasks (started but not closed).
rmp task list -r <rdm> -s DOING
rmp task list -r <rdm> -s TESTING
# Read what was already tried — the fastest route back into context.
rmp task comment-list <id> -r <rdm>
# Understand how long it has been stalled and its last transition.
rmp audit history TASK <id> -r <rdm>
rmp audit list -r <rdm> -o TASK_STATUS_DOING -e TASK      # NOT TASK_STATUS_CHANGE — that is legacy, returns []
# Re-read the original assumptions and the sprint purpose.
rmp task get <id> -r <rdm>                        # -fr / -tr / -ac, plus commit_open
rmp sprint get <sid> -r <rdm>                     # title + description
```

Then reconcile the current implementation against the original `-fr`/`-tr`/`-ac`:

- If the implementation drifted from the requirements, fix it to meet them (delegate the work).
- If the assumptions legitimately changed, update the task with `rmp task edit <id> -r <rdm> [...]` — with user confirmation — and record **why** with `rmp task comment-add <id> -r <rdm> --type UPDATE`.
- `commit_open` tells you which commit the previous attempt started from. It survives a `reopen`, so on a restarted task it may be stale; a fresh `stat DOING --commit-open <hash>` replaces it.
- Keep the state faithful and move the task forward. Never mark `COMPLETED` without confirmation that the acceptance criteria are met, and never without the real closing commit hash.

---

## 3. Keep the task work log

The task's four content fields say what the task *is*; the comment log says how the work actually *went* and **why**. Record entries **as the work happens**, one entry per fact, not reconstructed at closing time. A log backfilled at the end records conclusions and loses everything that was ruled out — which is the half that saves the next reader time.

```bash
rmp task comment-add <id> -r <rdm> --type HYPOTHESIS --body "<the proposition, stated before it is tested>"
rmp task comment-add <id> -r <rdm> --type TEST       --body "<what was run, and what it showed>"
rmp task comment-add <id> -r <rdm> --type FINDING    --body "<the behaviour observed / cause identified>"
rmp task comment-add <id> -r <rdm> --type DECISION   --body "<the choice taken and the reasoning>"
rmp task comment-add <id> -r <rdm> --type PROGRESS   --body "<what was done, what remains>"
rmp task comment-add <id> -r <rdm> --type UPDATE     --body "<why the task's definition changed>"
rmp task comment-add <id> -r <rdm> --type NOTE       --body "<anything else worth keeping>"

# Multi-paragraph bodies: pipe them instead of quoting them.
cat finding.txt | rmp task comment-add <id> -r <rdm> --type FINDING

rmp task comment-list <id> -r <rdm>                 # whole log, oldest first
rmp task comment-list <id> -r <rdm> --type DECISION # just the decisions
rmp task comment-edit <comment-id> -r <rdm> --type FINDING   # reclassify; id is the COMMENT's own id
```

Rules that keep the log trustworthy:

- **Record the hypothesis before the result.** A log that only holds conclusions cannot show what was ruled out.
- **Prefer appending a correction over rewriting.** `comment-edit` discards the previous body irrecoverably and the audit row records only that an edit happened. Reclassifying a type is the safe edit; rewriting history is not.
- **`comment-remove` is final and takes exactly one id.** Confirm before running it.
- **Comments never gate a status** and are accepted on a `COMPLETED` task, so a late-arriving finding still belongs on the task rather than nowhere.
- **`--summary` on `COMPLETED` states the outcome; the log preserves the route.** Do not collapse the log into the summary — draft the summary *from* the log.
- **Close the hypothesis loop.** Every `HYPOTHESIS` must eventually be answered by a `FINDING`, including when the answer is "refuted". An unresolved hypothesis is an incomplete record.
- **Pair every `task edit` with an `UPDATE`** saying why the definition changed. The edit shows the new text; only the comment says why.

**The bar for closing a task.** Before proposing `COMPLETED`, verify the log already holds a `DECISION` for every non-obvious choice, a `TEST` for the verification that establishes the acceptance criteria, a `FINDING` for anything discovered a future reader would need, and an `UPDATE` for every definition change. A task whose work involved choices or evidence and whose log is empty is not ready to close, however finished the code is — say so and record it first.

---

## 3b. Read the work log — recover history and reasoning

The log is written so it can be **read back**. Reading is not optional courtesy: it is what stops the same ground being covered twice and what makes past decisions answerable.

```bash
# Full history, oldest first — the fastest route into a task's context.
rmp task comment-list <id> -r <rdm>

# The "why": every choice taken, with its reasoning and the options rejected.
rmp task comment-list <id> -r <rdm> --type DECISION

# The evidence: what was actually run and what it showed.
rmp task comment-list <id> -r <rdm> --type TEST

# What was discovered — including hypotheses that were refuted.
rmp task comment-list <id> -r <rdm> --type FINDING

# Why the task's definition changed, before changing it again.
rmp task comment-list <id> -r <rdm> --type UPDATE

# The sprint's own narrative — scope changes, sequencing calls, cross-task findings.
rmp sprint comment-list <sid> -r <rdm>
```

**Read before acting, at these moments:**

| Moment | Read |
|---|---|
| resuming a `DOING`/`TESTING` task | the whole task log (see workflow 2) |
| starting any investigation on a task | the whole log — what was already tried and ruled out |
| about to propose `COMPLETED` | the whole log — check the closing bar, draft `--summary` from it |
| about to `task edit` | `--type UPDATE` |
| the user asks *why*, *what was tried*, *was it tested* | `--type DECISION`, then `--type FINDING`, then `--type TEST` |
| revisiting a settled decision | `--type DECISION` — start from the recorded argument, not from scratch |
| a task was reopened from `COMPLETED` | the whole log — every comment survives the reopen, as does `commit_open` |
| building a PDS or execution report | the sprint log, plus the `DECISION` entries of its tasks |

**Pair the log with the audit trail when the question is chronological.** `rmp audit history TASK <id>` says *when* each transition happened and at which commit; the log says *what was learnt* and *what was chosen*. Neither explains the work alone.

**Report silence as silence.** If the log does not answer the question, say it was not recorded and, where it matters, record it now. Never reconstruct a plausible reason and present it as the history.

---

## 4. Keep the sprint log

The sprint's own log records how the *sprint* went — scope changes, sequencing calls, cross-task observations. It accepts only four types: `FINDING`, `DECISION`, `PROGRESS`, `UPDATE`. `HYPOTHESIS`, `TEST` and `NOTE` are task-only and are rejected here with exit 6.

```bash
rmp sprint comment-add <sid> -r <rdm> --type DECISION --body "<a scope or sequencing decision, and why>"
rmp sprint comment-add <sid> -r <rdm> --type UPDATE   --body "<why the sprint's definition changed>"
rmp sprint comment-add <sid> -r <rdm> --type PROGRESS --body "<how the sprint advanced>"
rmp sprint comment-add <sid> -r <rdm> --type FINDING  --body "<a cross-task observation>"
rmp sprint comment-list <sid> -r <rdm>                # the sprint's narrative, oldest first
```

Use it in preference to rewriting `sprint update -d`: the description states the macro goal, and overloading it with a running narrative destroys the chronology. The sprint log is the primary source for the PDS narrative and for the closing summary.

Write a sprint entry at these moments: the sprint's scope changed (`UPDATE`, with the reason), tasks were carried over or resequenced (`DECISION`), a milestone was reached or the sprint's shape shifted (`PROGRESS`), or something was observed that spans several tasks and belongs to none of them (`FINDING`). Keep the diary of an individual task on that task — the sprint log answers *how the sprint went*, and it is the first thing read when a sprint is reviewed or reported.

---

## 5. Close a sprint with carryover

Adopted policy: when a sprint closes with incomplete tasks, **carry them over** (preserving status) to another sprint before closing — do not `--force` over active tasks unless the user explicitly approves.

```bash
# 1. Inspect unfinished tasks (SPRINT/DOING/TESTING).
rmp sprint open-tasks <sid> -r <rdm>

# 2. Choose / prepare the destination sprint (the next PENDING one, or a new one).
rmp sprint list -r <rdm> --status PENDING
#   If none suitable, create one (confirm title + macro goal first):
rmp sprint create -r <rdm> -t "Sprint N+1: <objective>" -d "<macro goal>"

# 3. Carry the incomplete tasks over — status is preserved. Confirm the destination first.
rmp sprint move-tasks <sid> <dest-sid> <incomplete-ids-csv> -r <rdm>

# 4. Record the outcome in the sprint's LOG (chronological, dated) — not by rewriting the description.
rmp sprint comment-add <sid> -r <rdm> --type PROGRESS \
  --body "Closed with <n> completed: <ids>. Carried over to sprint <dest-sid>: <ids|none>."
rmp sprint comment-add <sid> -r <rdm> --type DECISION \
  --body "<decisions taken, blockers resolved, scope changes and why>"

# 5. Now the sprint has no active tasks — close it cleanly (no --force).
rmp sprint close <sid> -r <rdm>
rmp sprint get   <sid> -r <rdm>

# 6. Optionally promote the destination/next sprint.
rmp sprint start <dest-sid> -r <rdm>              # PENDING → OPEN (only one OPEN at a time)
```

`sprint close --force` closes over active SPRINT/DOING/TESTING tasks and only warns on stderr. Use it **only** with explicit user approval; the carryover-move path above is the default. Comments remain readable and appendable after the sprint is CLOSED, so a retrospective entry can still be added later.

---

## 6. Move a task between sprints

```bash
rmp sprint tasks -r <rdm> <from-sid>                       # identify tasks to move
rmp sprint move-tasks -r <rdm> <from-sid> <to-sid> <ids>   # task status preserved; destination must not be CLOSED
rmp sprint tasks -r <rdm> <to-sid>                         # verify — and check the resulting position order
```

A `COMPLETED` task cannot be moved (exit 6): it stays in the sprint it was completed in. A move writes three audit rows per task (`SPRINT_MOVE_TASK_OUT` against the source, `SPRINT_MOVE_TASK_IN` against the destination, and `TASK_SPRINT_CHANGE` against the task, naming the destination). The legacy single `SPRINT_MOVE_TASK` operation is no longer written. Moved tasks land at the end of the destination's order — reorder if their execution sequence matters.

---

## 7. Dependency & impediment management

Treat incomplete dependencies as impediments to remove before work can complete.

```bash
rmp task add-dep    <id> <dep-id> -r <rdm>     # <id> depends on <dep-id>   (self-edges and cycles rejected, exit 6)
rmp task remove-dep <id> <dep-id> -r <rdm>     # exit 4 if the edge does not exist
rmp task blockers <id> -r <rdm>                # what is currently blocking <id> (incomplete deps)
rmp task blocking <id> -r <rdm>                # what is downstream of <id>
```

Always check `blockers` before proposing a task for execution — `task next` will not do it for you — and list active impediments when reporting state. An open blocker makes `COMPLETED` fail (exit 6): help sequence the blockers' resolution, never bypass the gate. A task's own object also carries `depends_on` and `blocks`, so a single `task get` answers both directions without extra calls.

---

## 8. Subtask decomposition

When a task turns out to hold several independent needs, decompose it rather than widening it.

```bash
rmp task create -r <rdm> -t "..." -fr "..." -tr "..." -ac "..." --parent <parent-id> -y SUB_TASK
rmp task subtasks <parent-id> -r <rdm>         # direct children, one level only
```

- `--parent` is accepted on `create` only; it cannot be set or changed by `task edit`.
- `--parent` does **not** set the type — pass `-y SUB_TASK` explicitly if that is what you mean.
- The parent cannot reach `COMPLETED` until every subtask is `COMPLETED` (exit 6).
- The parent cannot be removed while it has any subtask, whatever their status — remove the children first.
- Record why the decomposition happened with an `UPDATE` comment on the parent.

---

## 9. Backlog refinement (grooming)

Keep the backlog prioritised and well-formed. Flag refinement candidates; apply content changes with confirmation.

```bash
rmp backlog list -r <rdm> --sort priority          # review ordering
rmp backlog show-next 10 -r <rdm>                   # top candidates for the next sprint
rmp task get <id> -r <rdm>                          # inspect a suspect task's fields
```

Refinement signals — the first four are the Writing rules in `SKILL.md` applied as a checklist:

| Signal | What it looks like | Fix |
|---|---|---|
| **Not short** | padded prose, restated title, hedging, background the reader has; a field near its 4096 cap | rewrite tight; if it *needs* the length, the task is too big → workflow 8 |
| **Not formatted** | wall of plain text with no emphasis; identifiers and paths unmarked | `**bold**` the decisive fact, `` `code` `` the identifiers, `*` bullets for enumerations |
| **Markdown in the title** | `` `pkg` **breaks** on … `` | strip to plain text — titles render inside tables |
| **Open scope** | "etc.", "and so on", "improve X generally", "review the module", "as needed"; `-ac` reading "works correctly" | name the exact surface; make `-ac` a command or an observable state; state what is out of scope |
| **No reproduction** | a `BUG` whose `-fr` has no steps, environment, or observed-vs-expected | add them to `-fr`, or ask the user — do not invent them |
| **Too large / EPIC** | several independent needs in one task | decompose (workflow 8); propose the split before editing |

Then apply, and say why:

```bash
rmp task edit <id> -r <rdm> [-fr ...] [-tr ...] [-ac ...] [-y ...]
rmp task comment-add <id> -r <rdm> --type UPDATE --body "<why the definition changed>"
```

---

## 10. Reprioritise the backlog

```bash
rmp backlog list -r <rdm>                          # inspect current ordering
rmp task prio <id1>,<id2> <0-9> -r <rdm>           # batch-set priority (fail-fast: all IDs valid or none)
rmp backlog list -r <rdm>                          # verify the new ordering
```

Priority ranks the **backlog** for sprint planning. Once a task is inside a sprint, its **position** governs execution order — reprioritising a sprint member does not change what `task next` returns.

---

## 11. Backlog triage

```bash
rmp backlog list -r <rdm> --sort priority -l 100   # top of the pile
rmp backlog list -r <rdm> -y BUG --sort created    # oldest bugs first
rmp task list -r <rdm> -s BACKLOG --created-until <YYYY-MM-DD>   # stale items
rmp task prio <ids> <0-9> -r <rdm>                 # bulk-tune priority
rmp task sev  <ids> <0-9> -r <rdm>                 # bulk-tune severity
```
