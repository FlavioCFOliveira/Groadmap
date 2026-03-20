# CLAUDE.md

## 1. Critical Policy: Specification First

**MANDATORY: No implementation without SPEC/**. Zero exceptions.

```
User Request → spec-orchestrator → SPEC/ → [task-creator → roadmap-coordinator] → go-elite-developer → Implementation
```

| Violation | Action |
|-----------|--------|
| Code requested without SPEC | STOP → Invoke spec-orchestrator |
| SPEC incomplete | STOP → Update SPEC first |
| Urgency cited | NO exceptions. SPEC first. ALWAYS. |

**WHEN IN DOUBT: SPEC FIRST.**

---

## 2. Specification Scope

### Functional Area Mapping

| Area | SPEC File | Covers |
|------|-----------|--------|
| Version | `VERSION.md` | Version commands, logic, display |
| Build | `BUILD.md` | Build process, CI/CD, targets |
| Deploy | `DEPLOY.md` | Installation, distribution, releases |
| CLI | `COMMANDS.md` | Commands, subcommands, aliases, flags |
| Database | `DATABASE.md` | Schema, migrations, queries |
| Data | `DATA_FORMATS.md` | JSON schemas, formats |
| Models | `MODELS.md` | Structs, enums, domain models |
| Architecture | `ARCHITECTURE.md` | System design, components |
| State | `STATE_MACHINE.md` | State transitions, workflows |
| Help | `HELP_EXAMPLES.md` | Help text, errors, examples |

### Update Rules

- **Existing functionality** → Update relevant SPEC
- **New subcommand** → Update `COMMANDS.md`
- **Schema change** → Update `DATABASE.md`
- **New functional area** → Create new SPEC only

**NEVER** create task-specific specs (e.g., "VERSION_RESET.md").

---

## 3. Skill Responsibilities

```
┌─────────────────────────────────────────────────────────────────┐
│                     RESPONSIBILITY FLOW                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   spec-orchestrator          task-creator                      │
│   ┌─────────────┐            ┌─────────────┐                  │
│   │ SPEC/       │            │ Collects    │                  │
│   │ creation    │            │ ALL data    │                  │
│   └──────┬──────┘            └──────┬──────┘                  │
│          │                         │                           │
│          ▼                         ▼                           │
│   go-elite-developer         roadmap-coordinator              │
│   ┌─────────────┐            ┌─────────────┐                  │
│   │ Code        │◄──────────│ CLI exec    │                  │
│   │ implementation│          │ (rmp)       │                  │
│   └─────────────┘            └──────┬──────┘                  │
│                                   │                           │
│                                   ▼                           │
│                            ┌─────────────┐                  │
│                            │ SQLite DB   │                  │
│                            │ (truth)     │                  │
│                            └─────────────┘                  │
│                                                                  │
│   Supporting: go-gitflow, exhaustive-qa-engineer,              │
│   red-team-hacker, go-performance-advisor, doc-manager         │
└─────────────────────────────────────────────────────────────────┘
```

### Core Skills

| Skill | Responsibility | Key Rules |
|-------|---------------|-----------|
| **spec-orchestrator** | SPEC/ creation and maintenance | MUST be first step. NEVER derives from code. |
| **task-creator** | Task/sprint data collection | Collects ALL fields. Delegates to coordinator. |
| **roadmap-coordinator** | CLI execution and coordination | Source of truth via `rmp` CLI. NEVER implements directly. |
| **go-elite-developer** | Go implementation | ONLY after SPEC exists. Validates build/test/vet/fmt. |
| **go-gitflow** | Git operations | Tests MUST pass before push. User approval required. |
| **exhaustive-qa-engineer** | Testing and validation | Critical features, pre-release, schema changes. |
| **red-team-hacker** | Security audits | Security features, input validation. |
| **go-performance-advisor** | Performance analysis | Bottlenecks, memory, concurrency. |
| **doc-manager** | Documentation management | README, command docs, sync with code. |

### Task/Sprint Creation Flow

**Step 1: task-creator** collects ALL required fields:
- Tasks: title, type, priority, status, description, technical, criteria, specialists, time, complexity
- Sprints: name, goal, start, end, status

**Step 2: User confirmation** → task-creator delegates to roadmap-coordinator

**Step 3: roadmap-coordinator** executes CLI commands:
- `rmp task create --title "X" --type TASK --priority P1 ...`
- `rmp sprint create --name "X" --goal "Y" ...`

**Step 4: SQLite** stores as source of truth

---

## 4. Execution Rules

### Rule 1: Skill Delegation

| Task Type | Skill |
|-----------|-------|
| New feature/changes | `spec-orchestrator` FIRST |
| Create task/sprint | `task-creator` → `roadmap-coordinator` |
| Execute/coordinate tasks | `roadmap-coordinator` |
| Code implementation | `go-elite-developer` |
| Git operations | `go-gitflow` |
| Testing | `exhaustive-qa-engineer` |
| Security audit | `red-team-hacker` |
| Performance | `go-performance-advisor` |

### Rule 2: Validation Gates

Before any commit:
1. `go fmt ./...` (format)
2. `go vet ./...` (static analysis)
3. `go test ./...` (tests - ALL must pass)
4. `go build -o ./bin/ ./cmd/rmp` (build)

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
/Users/flaviocfo/dev/github.com/FlavioCFOliveira/Groadmap/
├── cmd/rmp/main.go              # Entry point
├── internal/
│   ├── commands/                # Subcommands
│   ├── db/                    # SQLite, schema
│   ├── models/                # Structs, enums
│   └── utils/                 # JSON, dates, paths
├── bin/                       # Build output
├── SPEC/                      # Technical specifications
└── .claude/skills/            # Skill definitions
    ├── task-creator/
    ├── roadmap-coordinator/
    ├── spec-orchestrator/
    ├── go-elite-developer/
    └── go-gitflow/
```

---

## 6. Decision Matrix

| Situation | Action |
|-----------|--------|
| New feature | `spec-orchestrator` FIRST |
| Code changes | Verify SPEC/ or invoke `spec-orchestrator` |
| Create task/sprint | `task-creator` |
| Execute tasks | `roadmap-coordinator` |
| Git operations | `go-gitflow` |
| Tests needed | `exhaustive-qa-engineer` |
| Security audit | `red-team-hacker` |
| Performance analysis | `go-performance-advisor` |
| Documentation needed | `doc-manager` |
| SPEC exists, implement | `go-elite-developer` |
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
```

---

## 10. Documentation Standards

### Language
- **SPEC/, skills, CLAUDE.md:** English
- **User interaction:** Portuguese (PT-pt)
- **Technical terms:** May remain in English

### Tone
- Clear, technical, professional
- **NO emojis or ornamental characters**

---

## Project Identity

**Groadmap** is a CLI tool in Go for managing technical roadmaps, using SQLite as backend.
