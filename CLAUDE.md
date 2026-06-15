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

### Never Guess

All work in this project MUST be based EXCLUSIVELY on knowledge you already
have. Never guess the intended answer. When your information is insufficient:
- Consult the **Knowledge Graph** first — it is the primary source, both to
  query and to record the relationships you discover (see Section 5).
- Then search the internet in official or authoritative sources, papers, books,
  or domain experts to determine the best result.

### Measure to Decide

Whenever you must assess performance, completeness (whether something is
finished), or correctness, ALWAYS gather evidence from the project itself.
Decide empirically — never by assumption.

### Production-Grade by Default

Across the entire workflow (analysis → planning → development → testing), the
objective is always a **production-grade** result. Apply maximum knowledge and
maximum diligence so that every cycle produces code that is ready for production
use.

### Self-Contained Development

Every development cycle MUST be self-contained. NEVER deliver only part of a
task; each cycle must produce a working result (a deliverable).
- All code and development is **Full-fledged** by rule. NEVER create tests with
  skip.
- When new, previously-unforeseen needs are discovered mid-task, resolve them
  within the SAME development cycle (as immediately as possible): add the new
  tasks via `rmp` and develop them as fast as possible.
- When you find pre-existing bugs, fix them on the spot, then resume the work
  you were doing when you found the bug.

### Regression Prevention

Whenever a bug is identified, you MUST add the regression test(s) needed to
guarantee that the same bug cannot reappear as a consequence of future
development. This applies to every bug — pre-existing or newly introduced — and
the regression test is part of the SAME self-contained cycle that fixes the bug
(see Self-Contained Development).

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
| Graph | `GRAPH.md` | Knowledge graph: GoGraph integration, persistence, guard-rail validation |
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
│   Supporting: exhaustive-qa-engineer, release-manager,          │
│   doc-manager, security-review, review, simplify                │
└─────────────────────────────────────────────────────────────────┘
```

### Core Agents and Skills

| Agent / Skill | Type | Responsibility | Key Rules |
|---------------|------|----------------|-----------|
| **specification-manager** | agent | SPEC/ creation and maintenance | MUST be first step. NEVER derives from code. Sole owner of `SPEC/`. |
| **roadmap-manager** | skill | Roadmap/sprint/task management via `rmp` CLI | Source of truth via `rmp`. NEVER implements code directly. |
| **go-developer** | agent | Go implementation, refactor, review, performance | ONLY after SPEC exists. Validates build/test/vet/fmt/lint. |
| **exhaustive-qa-engineer** | agent | Testing, edge cases, security/robustness validation | Critical features, pre-release, schema changes. |
| **release-manager** | agent | Release coordination, version bump, CHANGELOG | Triggered by release requests. Runs full validation gates. |
| **doc-manager** | skill | Documentation (README, command docs) | Sync docs with code. Go CLI projects only. |
| **security-review** | skill | Security review of pending changes | Trigger before merging security-sensitive changes. |
| **review** | skill | Pull request review | Code review on PRs. |
| **simplify** | skill | Review changes for reuse/quality and fix issues | Post-implementation cleanup. |

### Task/Sprint Creation Flow

**Step 1: `roadmap-manager`** collects ALL required fields and confirms with the user:
- Tasks: title, type, priority, status, description, technical, criteria, specialists, time, complexity
- Sprints: name, goal, start, end, status

**Step 2: User confirmation** → `roadmap-manager` executes `rmp` CLI commands:
- `rmp task create --title "X" --type TASK --priority P1 ...`
- `rmp sprint create --name "X" --goal "Y" ...`

**Step 3: SQLite** stores as source of truth.

---

## 4. Planning & Task Execution

Use the `rmp` CLI (the system's roadmap-management tool) to plan and coordinate
execution. `rmp` is the **SINGLE SOURCE OF TRUTH** for planning and task
execution in this project — no other mechanism may be used for this purpose.

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
2. Which task is next.
3. The goal of the task being started, based on its description and its
   functional and technical requirements.
4. Determine the most appropriate subagent for the task and delegate its
   execution to that subagent.
5. Always validate that the acceptance criteria are observed before closing a
   task.
6. Ensure the task is closed with a short summary of what was done.
7. After closing the task and before moving to the next one, make a git commit
   following best practices, explaining what was done.
8. Update the Knowledge Graph.

Whenever possible, adapt the model and the model's effort level to the
requirements of each task's individual operations.

Sprints and tasks are preferably executed sequentially. **Sprints MUST be
executed sequentially.** Tasks may run in parallel only when there is
justification for it.

---

## 5. Knowledge Graph

Use `rmp`'s Graph (Groadmap) features to create, maintain (update), and query a
knowledge graph of the project. This graph **MUST CONTAIN EVERYTHING** useful to
know about the project. Examples:
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
| Create/manage task/sprint | `roadmap-manager` |
| Code implementation, refactor, performance | `go-developer` |
| Git operations | Bash (`git`) — see Rule 3 |
| Releases / version bump | `release-manager` |
| Testing | `exhaustive-qa-engineer` |
| Security audit | `security-review` skill |
| Documentation | `doc-manager` |
| PR review | `review` skill |
| Code cleanup / simplification | `simplify` skill |

### Rule 2: Validation Gates

Before any commit:
1. `go fmt ./...` (format)
2. `go vet ./...` (static analysis)
3. `go test ./...` (tests - ALL must pass)
4. `go build -o ./bin/ ./cmd/rmp` (build)
5. `golangci-lint run ./...` (lint — requires golangci-lint; see SPEC/BUILD.md for install)

Run all gates at once: `make check`

### Rule 3: Commit Standards

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
this project (e.g., `specification-manager`, `roadmap-manager`, `go-developer`,
`exhaustive-qa-engineer`, `release-manager`, `review`, `security-review`,
`simplify`) are provided by the global Claude Code configuration.

---

## 8. Decision Matrix

| Situation | Action |
|-----------|--------|
| New feature | `specification-manager` FIRST |
| Code changes | Verify SPEC/ or invoke `specification-manager` |
| Create/manage task/sprint | `roadmap-manager` |
| Git operations | Bash (`git`) with user confirmation for destructive ops |
| Release / version bump | `release-manager` |
| Tests needed | `exhaustive-qa-engineer` |
| Security audit | `security-review` skill |
| Performance analysis | `go-developer` (covers performance) |
| Documentation needed | `doc-manager` |
| SPEC exists, implement | `go-developer` |
| PR review | `review` skill |
| Requirements unclear / ambiguous / contradictory | ASK the user — provide options (a, b, c) with a recommendation; one question at a time |
| New need discovered mid-task | Add task via `rmp`; resolve in the SAME cycle |
| Pre-existing bug found | Fix on the spot, then resume the original work |
| Bug identified (any) | Add regression test(s) in the SAME cycle so it cannot reappear |
| Assess performance / completeness / correctness | Gather evidence; decide empirically |
| Information insufficient | Consult Knowledge Graph first, then authoritative sources — never guess |
| Code vs SPEC diverge | Follow SPEC, ask user |

---

## 9. Anti-Patterns (Zero Tolerance)

### Critical Violations
- Implement without SPEC/
- Derive SPEC from existing code
- Make product decisions without user
- Make decisions alone when instructions are unclear, ambiguous, or contradictory (always ASK)
- Guess instead of consulting the Knowledge Graph or authoritative sources
- Decide on performance/completeness/correctness without empirical evidence
- Create task-specific spec files
- Duplicate functional areas
- Add versioning, dates, or change history to SPEC files (git is the source of truth)

### Other Forbidden
- Ignore `go vet` or `go test` failures
- Partial (non-self-contained) deliverables or tests created with skip
- Leaving pre-existing bugs unfixed once found
- Fixing a bug without adding the regression test(s) that prevent its recurrence
- Committing without updating the Knowledge Graph
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

# Lint (requires golangci-lint; install: brew install golangci-lint)
golangci-lint run ./...

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
