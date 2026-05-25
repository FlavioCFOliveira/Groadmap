# CLAUDE.md

## Roadmap

**Name:** groadmap

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

## 4. Execution Rules

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

## 5. Project Structure

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

## 6. Decision Matrix

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
| Requirements unclear | ASK the user |
| Code vs SPEC diverge | Follow SPEC, ask user |

---

## 7. Anti-Patterns (Zero Tolerance)

### Critical Violations
- Implement without SPEC/
- Derive SPEC from existing code
- Make product decisions without user
- Create task-specific spec files
- Duplicate functional areas
- Add versioning, dates, or change history to SPEC files (git is the source of truth)

### Other Forbidden
- Ignore `go vet` or `go test` failures
- Destructive Git operations without confirmation
- Security compromises (SQL injection, etc.)
- Reference Claude/AI in commits
- Documentation in Portuguese
- Emojis in technical documentation

---

## 8. Technical Constraints

### Security
- Input validation for all CLI arguments
- Parameterized SQL queries
- Filesystem permissions: `0700` for `~/.roadmaps/`

### Data Standards
- All dates: ISO 8601 UTC
- Success: JSON to stdout
- Errors: Plain text to stderr

### SQLite
- Individual `.db` files in `~/.roadmaps/`
- Versioned schema with migrations

---

## 9. Development Commands

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

## 10. End-To-End (E2E) Testing

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

## 11. Documentation Standards

### Language
- **SPEC/, agent/skill definitions, CLAUDE.md:** English
- **User interaction:** Portuguese (PT-pt)
- **Technical terms:** May remain in English

### Tone
- Clear, technical, professional
- **NO emojis or ornamental characters**

---

## Project Identity

**Groadmap** is a CLI tool in Go for managing technical roadmaps, using SQLite as backend.
