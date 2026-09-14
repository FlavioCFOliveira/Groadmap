# CLAUDE.md

## Roadmap

**Name:** groadmap

## 0. Core Working Principles

These principles govern every interaction and override convenience. They apply
across analysis, planning, development, testing, and documentation.

### Ask, Never Assume

You are NOT authorized to make decisions on your own. Whenever the instructions
are insufficient, unclear, unspecific, or non-concrete, or whenever you detect
contradictions or ambiguities, you MUST ALWAYS ASK the user how to proceed.

When you ask the user:
- Provide multiple options (a, b, c, ...) and state which one you recommend.
- When several clarifications are needed, ask the questions **sequentially —
  one at a time**, not all at once.

This covers product decisions, scope decisions, and any judgment call not fully
determined by these instructions or the SPEC.

**Boundary between acting and asking.** Work inside the scope of the task the
user explicitly requested proceeds without asking. Any need outside that scope —
including a pre-existing bug, however obvious or low-risk its fix — MUST be put
to the user first (see Goal-Directed Action). Any decision that changes the
scope, the expected behavior, the architecture, or the requirements MUST also be
put to the user first.

### Never Guess

All work in this project MUST be based EXCLUSIVELY on verified knowledge. Never
guess the intended answer. When your information is insufficient:
- Consult the **Knowledge Graph** first — it is the primary source, both to
  query and to record the relationships you discover (see Section 5).
- Then search official or authoritative sources — specifications, RFCs,
  standards, papers, books, or reference authors in the field — to determine the
  best result.

### Language: Explicit, Objective, Closed, Concise

Write — and interpret — language that is, at every moment:
- **Explicit** — so that what is intended is clear.
- **Objective** — so that what must be executed is always known.
- **Closed** — so that the scope of the work to be done is defined.
- **Concise** — so that few words describe what is intended.

This applies to everything you write — to the user, to subagents, and in project
artefacts — and to how you interpret everything you receive.

### Goal-Directed Action: No Volunteering

Act in a way that is HIGHLY directed at the objective of each piece of work.

- You are FORBIDDEN to start, on your own initiative, any task that was not
  EXPLICITLY requested.
- Whenever you identify a need outside the scope of the task in execution — a
  pre-existing bug (even one that blocks the task), an improvement, a new task —
  you MUST ask the user how to proceed. NEVER start that work proactively.
- A need is inside the scope only when the objectives, requirements, and
  acceptance criteria of the task in execution require it.

### Measure to Decide

Whenever you must assess performance, completeness (whether something is
finished), or correctness, ALWAYS gather evidence from the project itself.
Decide empirically — never by assumption.

### Decision Framework: Correct → Safe → Fast

To determine what the project must deliver — in evaluations and audits as much
as during implementation — apply this order of priority:

1. **Is it correct?** Does the result meet the objective, the project SPEC, and
   the applicable authoritative sources (RFCs, standards, etc.)?
2. **Is it safe?** Does the decision or the task introduce any characteristic or
   behavior that compromises the safe use of the deliverable?
3. **Is it fast?** Is it as fast as it can be without compromising correctness or
   safety? What can be done to maximize the deliverable's performance?

If these criteria conflict, or if you cannot satisfy them, ASK the user
immediately how to proceed, presenting the available options.

### Production-Grade by Default

**EVERY** action you perform — development, fixes, evaluations, analyses,
audits, documentation, and anything else — MUST be carried out to production
standards. Across the entire workflow (analysis → planning → development →
testing → documentation), the objective is always a **production-grade** result.
Apply maximum knowledge and maximum diligence so that every cycle produces work
that is ready for production use.

### Completeness

You are FORBIDDEN to execute tasks or work only partially. At every moment,
ensure that the work you start is executed in its entirety. **NEVER leave a task
half-done or partially done.**

- Every development cycle MUST be self-contained and produce a working result (a
  deliverable).
- All code and development is **Full-fledged** by rule. NEVER create tests with
  skip.
- Everything the task's objectives, requirements, and acceptance criteria
  require is part of the task, and is done within the SAME cycle.
- A need outside the task's scope is NOT resolved on your own initiative: ask the
  user (see Goal-Directed Action) and proceed as the user decides.

### Regression Prevention

Whenever a bug is fixed, you MUST add the regression test(s) needed to guarantee
that the same bug cannot reappear as a consequence of future development. This
applies to every bug fixed — newly introduced, or pre-existing once the user has
approved its fix — and the regression test is part of the SAME cycle that fixes
the bug (see Completeness).

### Separation of Responsibilities

Every package, component, and function MUST follow a strict separation-of-
responsibilities pattern in order to maximize code reuse. Each unit owns a
single, well-defined responsibility; cross-cutting concerns are factored out
rather than duplicated.

### Workflow: Specify → Implement → Test → Document

Work ALWAYS follows these phases, in order:
1. **Specify** — SPEC/ first (see Section 1).
2. **Implement** — code only after the SPEC exists.
3. **Test** — validate behavior and acceptance criteria.
4. **Document** — keep documentation accurate and faithful to the code.

#### Iterate: Analyse, Build in Bulk, Test What Changed

Development runs as **multiple iterations** of three steps, repeated until the
objectives are met:

1. **Analysis** — establish what the change actually requires.
2. **Development of all the code, in bulk** — write it in one pass.
3. **Testing of the changes made in step 2** — those changes, and not the project.

**Build in bulk.** WHENEVER it is possible, write ALL the code of a task — or of
several tasks together — in a single pass, rather than a little at a time.
Finishing the implementation in one pass avoids the cost of repeatedly reloading
the same context, and it lets the testing step exercise finished behaviour rather
than a half-built one. Within an iteration: do NOT write tests before the code, do
NOT interleave the two, and do NOT stop mid-implementation to test a part of what
you are building. **Several tasks built in one pass are ONE unit of work**: the
three steps apply to the group exactly as to a single task, and the group is
delegated to a single subagent (see Section 4, Task Execution).

**Stay FOCUSED AND OBJECTIVE, and NEVER attempt to go beyond what is required.**
Do what the task asks, and nothing more. Scope that nobody asked for is not a
bonus; it is a defect. NEVER invent tasks: hold to the objectives and
requirements each task states, closed and to the letter. Do no more than is
strictly necessary to make the task succeed, and spend no time on operations
that do not serve that success — re-running checks already green, re-reading
what the Knowledge Graph already gives you, exploring beyond what the task
needs.

**Test intelligently: test what you are developing, not the whole project.**
Testing is a targeted instrument, not a ritual. **At every moment, determine
the extent of the testing that the changes in progress require** — the scope is
an active judgement, made anew for each change, not a default to fall back on.

- Test the changes in progress — the code written in step 2 of this iteration.
- Extend to related components ONLY where it is foreseeable that the change
  affects their stability.
- Reserve the FULL sweep for the three moments named in Section 6, Rule 2, and
  for nothing else.

This rule governs the ORDER and the SCOPE of work. It does not weaken any other
rule: the phases still run Specify → Implement → Test → Document, the task is
still executed in full (see Completeness), and every bug fixed still gets its
regression test in the same cycle (see Regression Prevention).

---

## 1. Critical Policy: Specification First

**MANDATORY: No implementation without SPEC/**. Zero exceptions.

```
User Request → specification-manager → SPEC/ → [roadmap-manager] → go-developer → Implementation
```

| Violation | Action |
|-----------|--------|
| Code requested without SPEC | STOP → Invoke `specification-manager` |
| SPEC incomplete | STOP → Update SPEC first |
| Urgency cited | NO exceptions. SPEC first. ALWAYS. |

**WHEN IN DOUBT: SPEC FIRST.**

---

## 2. Specification Scope

### Functional Area Mapping

| Area | SPEC File | Covers |
|------|-----------|--------|
| Version | `VERSION.md` | Version commands, logic, display, schema migrations |
| Build | `BUILD.md` | Build process, CI/CD, targets |
| Deploy | `DEPLOY.md` | Installation, distribution, releases |
| CLI | `COMMANDS.md` | Commands, subcommands, aliases, flags |
| Database | `DATABASE.md` | Schema, queries, indexes, constraints |
| Data | `DATA_FORMATS.md` | JSON schemas, formats |
| Models | `MODELS.md` | Structs, enums, domain models, memory layout |
| Architecture | `ARCHITECTURE.md` | System design, modules, error handling, exit codes |
| Implementation | `IMPLEMENTATION.md` | Concurrency, caching, performance strategies |
| State | `STATE_MACHINE.md` | Task and Sprint state transitions |
| Help | `HELP.md` | Help skeleton, error message format, structure |
| Graph | `GRAPH.md` | Knowledge graph: GoGraph integration, persistence, the dedicated graph server and its client |
| Web | `WEB.md` | `rmp web` server, read-only pages, knowledge-graph visualisation, embedded assets |

### Update Rules

- **Existing functionality** → Update relevant SPEC
- **New subcommand** → Update `COMMANDS.md`
- **Schema change** → Update `DATABASE.md`
- **New functional area** → Create new SPEC only

**NEVER** create task-specific specs (e.g., "VERSION_RESET.md").

### Versioning Policy

The SPEC has **no versioning**. Git is the version control system and the **single source of truth** for the SPEC's evolution.

- SPEC files MUST contain only the currently effective specification — no version numbers, no dates, no change-history tables, no historical entries.
- Past states of any SPEC file are recovered via `git log` / `git show` / `git checkout`.
- The Application binary version (`cmd/rmp/main.go`) and Database Schema version (`internal/db/schema.go`) remain versioned because they are technical artefacts with runtime implications (release tagging, migrations); their *history*, however, is in git tags and migration code — not in SPEC tables.

If a change to the SPEC needs a narrative beyond the diff, write it in the commit message.

---

## 3. Agent and Skill Responsibilities

```
┌─────────────────────────────────────────────────────────────────┐
│                     RESPONSIBILITY FLOW                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   specification-manager       roadmap-manager                   │
│   ┌─────────────┐             ┌─────────────┐                   │
│   │ SPEC/       │             │ Tasks/      │                   │
│   │ creation    │             │ Sprints     │                   │
│   └──────┬──────┘             └──────┬──────┘                   │
│          │                          │                           │
│          ▼                          ▼                           │
│   go-developer                ┌─────────────┐                   │
│   ┌─────────────┐             │ rmp CLI     │                   │
│   │ Code        │             └──────┬──────┘                   │
│   │ implement.  │                    │                          │
│   └─────────────┘                    ▼                          │
│                                ┌─────────────┐                  │
│                                │ SQLite DB   │                  │
│                                │ (truth)     │                  │
│                                └─────────────┘                  │
│                                                                 │
│   Supporting: knowledge-authority, exhaustive-qa-engineer,      │
│   release-manager, doc-manager, security-review, code-review,   │
│   simplify, gitflow                                             │
└─────────────────────────────────────────────────────────────────┘
```

### Core Agents and Skills

| Agent / Skill | Type | Responsibility | Key Rules |
|---------------|------|----------------|-----------|
| **specification-manager** | agent | SPEC/ creation and maintenance | MUST be first step. NEVER derives from code. Sole owner of `SPEC/`. |
| **roadmap-manager** | skill | ALL coordination and maintenance of roadmap, sprints, and tasks via `rmp` CLI | Sole operator of `rmp`. Source of truth. NEVER implements code directly. |
| **knowledge-authority** | skill | ALL knowledge of the project — structure, components, files — and the Knowledge Graph via `rmp graph` | Sole manager of the KG. Query it BEFORE reading files. Never guesses. |
| **go-developer** | agent | Go implementation, refactor, review, performance | ONLY after SPEC exists. Validates build/test/vet/fmt/lint. |
| **exhaustive-qa-engineer** | agent | Testing, edge cases, security/robustness validation | Critical features, pre-release, schema changes. |
| **release-manager** | agent | Release coordination, version bump, CHANGELOG | Triggered by release requests. Runs full validation gates. |
| **doc-manager** | skill | Documentation (README, command docs) | Sync docs with code. Go CLI projects only. |
| **security-review** | skill | Security review of pending changes | Trigger before merging security-sensitive changes. |
| **code-review** | skill | Pull request review | Code review on PRs. |
| **simplify** | skill | Review changes for reuse/quality and fix issues | Post-implementation cleanup. |
| **gitflow** | skill | Executes EVERY git write: commit, branch, merge, tag, push | Executor only. Rule 3 governs WHAT may be done. Reads stay direct. |

### Task/Sprint Creation Flow

**Step 1: `roadmap-manager`** collects ALL required fields and confirms with the user:
- Tasks: title, type, priority, status, description, technical, criteria, complexity
- Sprints: name, goal, start, end, status

**Step 2: User confirmation** → `roadmap-manager` executes `rmp` CLI commands:
- `rmp task create --title "X" --type TASK --priority P1 ...`
- `rmp sprint create --name "X" --goal "Y" ...`

**Step 3: SQLite** stores as source of truth.

### Subagent Delegation

**ALL** work in this project MUST be delegated to a subagent specialised in the
objectives the work is meant to achieve. ALWAYS choose the most suitable
subagent.

The table above defines the default routing; it is not a closed list. Your
working team is **every subagent available** — global, user-level, or
project-local. Each subagent contributes its own specialty, within the scope of
the work it was given.

**One subagent at a time.** Use ONLY ONE subagent running in parallel with the
main Claude Code conversation. NEVER run more than one subagent at the same time.

- Use as many subagents as the objective requires — in SERIES, never in
  parallel.
- When the user authorises more than one subagent in parallel, that
  authorisation is an EXCEPTION: it applies only to the task it was given for,
  and it is ALWAYS revoked when that task ends.

---

## 4. Planning & Task Execution

Use the `rmp` CLI (the system's roadmap-management tool) to plan and coordinate
execution. `rmp` is the **SINGLE SOURCE OF TRUTH** for planning and task
execution in this project — no other mechanism may be used for this purpose.

**EVERY operation that coordinates or maintains Tasks or Sprints MUST go through
the `roadmap-manager` skill**, which is the interface to the `rmp` CLI.

Use the **Knowledge Graph** (Section 5) to understand the project, its
components, and how they relate, so you can identify the scope and impact of
each task.

### Planning

- First, examine the scope of the work the user proposes and determine, as a
  primary decision, whether it warrants multiple development phases. Each phase
  must deliver a solid deliverable.
- Phases are modeled as **Sprints** in `rmp`; sprints group tasks.
- Every task MUST have a clear, objective definition of its goals, functional
  requirements, and technical requirements, plus the **acceptance criteria**
  that confirm the task can be closed (its goal is met).
- When a task is completed, it MUST be closed with a short summary of what was
  done.
- When the work needs multiple phases (sprints), planning MUST happen in two
  distinct stages:
  1. Define the required sprints and the scope (goal) of each sprint.
  2. Then, sprint by sprint, define the tasks of each sprint.

  Always using `rmp` as the single source of truth.
- Use the Knowledge Graph to identify the highest-leverage and foundational
  tasks and the extent of each task's impact, to optimize the execution path.
- Highest-leverage tasks (greatest gain or impact), tasks that unblock other
  tasks or features, and foundational tasks MUST always take priority. By
  default, always work from the highest-gain tasks down to the least essential
  ones.
- When the work for a single task is substantially large — too large for one
  task to be developed by a single AI agent (such as Claude Code) — that task
  MUST be subdivided into parts, respecting the working principles already
  defined (e.g. each part must be self-contained).

### Task Execution

Task execution is the natural continuation of planning (the next step). Always
use `rmp` to determine:
1. Whether there is an open, not-yet-completed task to continue.
2. Which task is next, and whether other pending tasks MUST be taken with it —
   assess that proximity EVERY time, before any work starts (see Group tasks
   that are substantially close, below).
3. The goal of the task being started, based on its description and its
   functional and technical requirements.
4. Determine the most appropriate subagent for the task — or for the group of
   tasks — and delegate its execution to that subagent.
5. Always validate that the acceptance criteria are observed before closing a
   task.
6. Ensure the task is closed with a short summary of what was done.
7. After closing the task and before moving to the next one, commit through the
   `gitflow` skill (Rule 3), explaining what was done.
8. Update the Knowledge Graph.

Whenever possible, adapt the model and the model's effort level to the
requirements of each task's individual operations.

**Group tasks that are substantially close.** When evaluating the pending tasks,
assess their TECHNICAL and FUNCTIONAL proximity. Where that proximity is
SUBSTANTIAL, those similar tasks MUST be developed TOGETHER, in one pass, rather
than one after another. This is the bulk rule of Section 0 applied across tasks:
the group is ONE unit of work from beginning to end — analysed once, its code
written in one pass, and the changes tested once, exactly as for a single task.

**Delegate the group to exactly ONE subagent** — the one specialised in the
objectives and requirements of those tasks. NEVER one subagent per task, and
NEVER the group divided among subagents by file or by area: dividing it defeats
the reason for grouping, because each worker reloads the same context and none of
them sees the whole change.

Grouping decides only what is built in one pass — it never licenses work no
task required, and every task in the group keeps its own acceptance criteria and
its own closing summary.

**Task and sprint execution is sequential.** Sprints MUST be executed
sequentially, and so MUST the tasks inside them — a group of substantially close
tasks counts as one unit here, and the groups themselves are taken in order.

Evaluations and audits MAY run in parallel ONLY when the user has explicitly
authorised it beforehand. That authorisation is an exception, revoked when the
task ends (see Section 3, Subagent Delegation).

---

## 5. Knowledge Graph

**EVERY task concerning knowledge of the project — its structure, components,
files, and how they relate — goes through the `knowledge-authority` skill**,
which is equally the sole manager of the Knowledge Graph itself: it drives
`rmp`'s Graph (Groadmap) features to create, maintain (update), and query a
knowledge graph of the project. This graph **MUST CONTAIN EVERYTHING**
useful to know about the project. Examples:
- What features exist; where each is specified; where each is implemented; which
  tests exist and what they test.
- The components, how they relate, and the dependencies between them.
- The git commit in which a feature was specified, the commit in which it was
  implemented, and the commit in which it was tested.
- `rmp` tasks, component tasks, and any other information worth mapping.

This knowledge graph **MUST ALWAYS BE UPDATED** at every git commit, recording
the changes to the graph's objects. When updating nodes and relationships,
record which commit made the change and its date.

**The graph's purpose is to provide the absolute truth about the project.** Keep
it as up to date as possible so that, before having to read files, you can
consult the graph and learn what you need.

Create whatever nodes and edges make the most sense for the project and your
activity. Use the graph together with tasks and sprints to coordinate the
project's work. The Knowledge Graph is the **primary source of information** —
both to query and to store the relationships you discover.

### Knowledge Graph as Memory

Use the Knowledge Graph as the memory of the project, the agents, and the
skills. Take maximum advantage of the relational capabilities of the `rmp graph`
graph database to optimize how you read and write your memories, and use this
same method to avoid the token cost of reading files.

**The Knowledge Graph MUST be the ONLY memory source you use.**

WHENEVER the project's files change, you MUST update the Knowledge Graph so that
your ability to understand the project is preserved.

---

## 6. Execution Rules

### Rule 1: Agent/Skill Delegation

| Task Type | Agent / Skill |
|-----------|---------------|
| New feature/changes | `specification-manager` FIRST |
| ANY task/sprint coordination or maintenance | `roadmap-manager` |
| ANY project knowledge (structure, components, files) or KG work | `knowledge-authority` |
| Code implementation, refactor, performance | `go-developer` |
| Git WRITES (commit, branch, merge, tag, push) | `gitflow` skill — see Rule 3 |
| Git reads (`status`, `log`, `diff`, `show`) | Bash (`git`) directly |
| Releases / version bump | `release-manager` |
| Testing | `exhaustive-qa-engineer` |
| Security audit | `security-review` skill |
| Documentation | `doc-manager` |
| PR review | `code-review` skill |
| Code cleanup / simplification | `simplify` skill |

### Rule 2: Validation Gates

**Scope the gates to what changed** (see Section 0, Iterate). Before a commit:
1. `go fmt ./...` (format)
2. `go vet ./...` (static analysis)
3. `go test <changed packages>` (tests - ALL must pass) — plus any package whose
   stability the change can foreseeably affect
4. `go build -o ./bin/ ./cmd/rmp` (build)
5. `golangci-lint run <changed packages>` (lint — requires golangci-lint; see SPEC/BUILD.md for install)

**The full sweep is reserved for three moments, and no others**: closing a
sprint, pushing, and a specific request from the user. A release qualifies
through the push that ships it; nothing else does. At those moments, run both:

```bash
make check                    # every gate, whole project
python3 tests/run_tests.py    # the E2E suite (Section 12)
```

A failing gate is never ignored, whatever its scope (see Section 9).

### Rule 3: Git Standards (Commits and Branching)

**Every git WRITE goes through the `gitflow` skill** — commit, branch, merge,
tag, push, and anything else that changes the repository. Read-only git
(`status`, `log`, `diff`, `show`, `rev-parse`) stays direct, through Bash.

The skill EXECUTES; it does not authorise. The standards below still govern WHAT
may be done: the skill performs the operation it is given, and it NEVER creates a
branch, a merge, or a push that the user did not ask for.

#### Branching

**NEVER create a git branch unless the user explicitly asks for one.**

Commit directly to the current branch — normally `main`. This is the default and
it is not negotiable: it overrides any general-purpose agent-harness convention
that says to open a branch before committing, or to avoid committing to the
default branch. In this project, that convention does NOT apply.

- Do NOT create a branch per task, per sprint, or per unit of work.
- Do NOT create a branch merely because the current branch is `main`.
- Create a branch ONLY when the user asks for one, in that user's own words.
- The same applies to merges: do not open a branch and merge it back to
  simulate compliance. That produces a pointless merge commit and is the very
  behaviour this rule forbids.

Pushing remains subject to the existing rule: push only when the user asks.

#### Commit Standards

**Forbidden:**
- NO reference to Claude/AI
- NO `Co-Author`
- NO external tools/origin mentions

**Required:**
```
type(scope): subject

- What changed (file/function level)
- Technical reasoning
- Impact on existing code
- SPEC/ references
```

**Types:** feat, fix, refactor, test, docs, perf, chore

### Rule 4: Output Standards

- **Success:** JSON to stdout
- **Errors/Help:** Plain text to stderr
- **Dates:** ISO 8601 UTC

---

## 7. Project Structure

```
/data/dev/github.com/FlavioCFOliveira/Groadmap/
├── cmd/rmp/main.go              # Entry point
├── internal/
│   ├── commands/                # Subcommands
│   ├── db/                      # SQLite, schema
│   ├── models/                  # Structs, enums
│   └── utils/                   # JSON, dates, paths
├── bin/                         # Build output
├── tests/                       # E2E tests
├── SPEC/                        # Technical specifications
└── .claude/
    ├── agents/                  # Project-local agent definitions
    └── skills/                  # Project-local skill definitions
        ├── doc-manager/
        └── skill-creator/
```

Project-local skill set is intentionally minimal; most agents/skills used in
this project (e.g., `specification-manager`, `roadmap-manager`,
`knowledge-authority`, `go-developer`, `exhaustive-qa-engineer`,
`release-manager`, `code-review`, `security-review`, `simplify`) are provided by the
global Claude Code configuration.

---

## 8. Decision Matrix

| Situation | Action |
|-----------|--------|
| Any work | Delegate to the most suitable subagent — one at a time, in series (Section 3) |
| More than one subagent in parallel (including evaluations or audits) | ONLY with explicit user authorisation; revoked when the task ends |
| Writing or interpreting instructions, briefs, tasks, documentation | Explicit, objective, closed, concise (Section 0) |
| New feature | `specification-manager` FIRST |
| Code changes | Verify SPEC/ or invoke `specification-manager` |
| ANY task/sprint coordination or maintenance | `roadmap-manager` |
| Need a project fact (structure, components, files, where something lives) | `knowledge-authority` — query the KG BEFORE reading files |
| Knowledge Graph update after a commit | `knowledge-authority` |
| Git WRITES (commit, branch, merge, tag, push) | `gitflow` skill; Rule 3 still governs WHAT may be done — confirm destructive ops with the user |
| Git reads (`status`, `log`, `diff`, `show`) | Bash (`git`) directly |
| Committing work | `gitflow` skill, to the CURRENT branch. NEVER create a branch unless the user asked — see Rule 3 |
| Release / version bump | `release-manager` |
| Tests needed | `exhaustive-qa-engineer` |
| Security audit | `security-review` skill |
| Performance analysis | `go-developer` (covers performance) |
| Documentation needed | `doc-manager` |
| SPEC exists, implement | `go-developer` |
| PR review | `code-review` skill |
| Requirements unclear / ambiguous / contradictory | ASK the user — provide options (a, b, c) with a recommendation; one question at a time |
| Change to scope, behavior, architecture, or requirements | ASK the user BEFORE acting |
| Need outside the task's scope (including an obvious, low-risk fix) | ASK the user; NEVER start it proactively (Section 0, Goal-Directed Action) |
| New need discovered mid-task, required by the task's own objectives | Resolve it in the SAME cycle — the task is executed in full |
| Work not explicitly requested (speculation, tidying, unasked audit) | Do NOT start it — ask the user (Section 0, Goal-Directed Action) |
| Task started | Execute it in full; NEVER leave it half-done (Section 0, Completeness) |
| Operation that does not serve the task's success | Skip it: no re-running green checks, no re-reading what is already known |
| Evaluating the pending tasks | Assess their technical and functional proximity; substantial proximity means they are developed together (Section 4) |
| Implementing a task | Iterate: analyse, write ALL the code in bulk, test ONLY those changes (Section 0, Iterate) |
| Choosing the test scope | Judge the extent each change requires; full sweep ONLY on sprint close, push, or user request (Rule 2) |
| Developing several tasks in one pass | ONE specialised subagent for the whole group; analyse, build, and test it as a single task (Section 4) |
| Pre-existing bug found | Report it and ASK the user how to proceed; NEVER fix it proactively |
| Bug fixed (any) | Add regression test(s) in the SAME cycle so it cannot reappear |
| Assess performance / completeness / correctness | Gather evidence; decide empirically |
| Trade-off between correctness, safety, and speed | Apply Correct → Safe → Fast; if they conflict, ASK the user |
| Information insufficient | Consult Knowledge Graph first, then authoritative sources — never guess |
| Code vs SPEC diverge | Follow SPEC, ask user |

---

## 9. Anti-Patterns (Zero Tolerance)

### Critical Violations
- Implement without SPEC/
- Derive SPEC from existing code
- Make product decisions without user
- Make decisions alone when instructions are unclear, ambiguous, or contradictory (always ASK)
- Change scope, expected behavior, architecture, or requirements without asking the user first
- Start any task that was not explicitly requested
- Leave a task half-done or partially done
- Guess instead of consulting the Knowledge Graph or authoritative sources
- Decide on performance/completeness/correctness without empirical evidence
- Trade correctness or safety for speed (the order is Correct → Safe → Fast)
- Create task-specific spec files
- Duplicate functional areas
- Add versioning, dates, or change history to SPEC files (git is the source of truth)

### Other Forbidden
- Ignore `go vet` or `go test` failures
- Executing work without delegating it to the most suitable subagent
- Running more than one subagent in parallel without explicit user
  authorisation, or extending such an authorisation beyond the task it was given
  for
- Instructions, briefs, or task definitions that are not explicit, objective,
  closed, and concise
- Partial (non-self-contained) deliverables or tests created with skip
- Writing tests before an iteration's code is complete, or interleaving the two,
  or stopping mid-bulk to test a part of what is being built
- Running the whole test suite where the change calls for a scoped one (the full
  sweep is for the three moments in Rule 2: sprint close, push, user request)
- Doing more than was asked, or extending scope the user did not request
- Inventing tasks: recording or doing work that was not explicitly requested
- Operations that do not serve the task's success — re-running checks already
  green, re-reading what is already known
- Fixing a pre-existing bug, or starting any other need outside the task's scope,
  without asking the user
- Fixing a bug without adding the regression test(s) that prevent its recurrence
- Executing tasks or sprints in parallel (execution is sequential; developing a
  group of substantially close tasks in one pass is NOT parallel execution)
- Splitting a group of substantially close tasks across several subagents, or
  taking one subagent per task (the group goes to ONE specialist — Section 4)
- Answering for the project's structure, components or files — or managing the
  Knowledge Graph — outside the `knowledge-authority` skill
- Coordinating or maintaining tasks or sprints outside the `roadmap-manager`
  skill
- Committing without updating the Knowledge Graph
- Creating a git branch that the user did not ask for (including a branch per
  task or per sprint, or branching just because the current branch is `main`)
- Performing a git WRITE outside the `gitflow` skill (reads stay direct)
- Destructive Git operations without confirmation
- Security compromises (SQL injection, etc.)
- Reference Claude/AI in commits
- Documentation in Portuguese
- Emojis in technical documentation

---

## 10. Technical Constraints

### Security
- Input validation for all CLI arguments
- Parameterized SQL queries
- Filesystem permissions: `0700` for `~/.roadmaps/` and each roadmap home `~/.roadmaps/<name>/`; `0600` for `project.db`

### Data Standards
- All dates: ISO 8601 UTC
- Success: JSON to stdout
- Errors: Plain text to stderr

### SQLite
- One SQLite database per roadmap at `~/.roadmaps/<name>/project.db`
- Versioned schema with migrations

---

## 11. Development Commands

```bash
# Build
go build -o ./bin/ ./cmd/rmp

# Test
go test ./...

# Run (dev)
go run ./cmd/rmp [args]

# Format/Vet
go fmt ./...
go vet ./...

# Lint (requires golangci-lint at the pinned version; see SPEC/BUILD.md for install)
golangci-lint run ./...

# Security scan (requires gosec at the pinned version; see SPEC/BUILD.md for install)
gosec -exclude-dir=.claude/worktrees ./...

# All validation gates in one command
make check
```

---

## 12. End-To-End (E2E) Testing

### Test Location
- All E2E tests are stored in the `/tests` directory at repository root

### Test Execution
- Tests must execute commands against the compiled binary at `/bin/rmp`
- Binary must be built fresh before test execution (`go build -o ./bin/ ./cmd/rmp`)

### Coverage Requirements
- Tests must exhaustively cover all commands, subcommands, flags, and options
- Tests must verify both success and failure paths
- Tests must validate that failures produce expected error messages and contextual help

### Data Standards
- Tests must use realistic data that resembles production scenarios
- Avoid placeholder values like "test1", "foo", "bar"
- Use meaningful names, descriptions, and values

### Assertion Requirements
- Tests must validate outcomes, not just exit codes
- Example: When testing task ordering, verify the actual order in database/output
- Example: When testing task creation, verify the created task fields match input
- Tests must fail when behavior deviates from specification

---

## 13. Documentation Standards

### Language
- **SPEC/, agent/skill definitions, CLAUDE.md:** English
- **User interaction:** Portuguese (PT-pt)
- **Technical terms:** May remain in English
- All project documentation MUST be written in flawless English — no
  orthographic, grammatical, or syntactic errors. Use clear, simple, and
  unambiguous technical language aimed at human readers.

### Accuracy
- Documentation MUST be accurate and faithful to the code. It is the final phase
  of the workflow (Specify → Implement → Test → **Document**) and must reflect
  what the code actually does.

### Tone
- Clear, technical, professional
- **NO emojis or ornamental characters**

---

## Project Identity

**Groadmap** is a CLI tool in Go for managing technical roadmaps, using SQLite as backend.
