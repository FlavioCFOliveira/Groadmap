# CLAUDE.md

## Project Identity

**Groadmap** is a CLI tool in Go for managing technical roadmaps, using SQLite as backend.

---

## Documentation Standards

### Language and Tone
- **All project documentation must be in English**
- Use clear, technical language
- Maintain a professional tone
- **No emojis or ornamental characters allowed**

### Scope
- All documentation in `SPEC/`
- All skill definitions and agent configurations
- This CLAUDE.md file
- Code comments and docstrings
- README files
- Help text and CLI documentation

---

## User Interaction Language

- **Preferred language for user interactions**: Portuguese (Portugal) - PT-pt
- Responses to the user should be in Portuguese
- Technical terms may remain in English when appropriate

---

## Agent Responsibilities (Strict Ownership)

### spec-orchestrator
**Exclusive authority** for technical specification.
- Creates and maintains all documents in `SPEC/`
- Produces specifications ONLY from user input
- NEVER derives specifications from existing code
- ALWAYS asks the user for decisions (the user is the single source of truth)
- Acts as gatekeeper: no implementation without clear specification

### go-elite-developer
**Exclusive authority** for Go code development.
- Implements features according to technical specification
- Ensures idiomatic, efficient, and secure code
- Performs validation: `go build`, `go test`, `go vet`, `go fmt`
- NEVER implements without approved specification
- ALWAYS follows project patterns in `SPEC/`

### go-gitflow
**Exclusive authority** for Git operations.
- Creates branches, commits, merges, PRs
- Validates repository state before operations
- Ensures technical and descriptive commit messages
- Requires explicit user confirmation for destructive operations
- ALWAYS checks `go fmt`, `go vet`, `go test` before commits

### exhaustive-qa-engineer
**Exclusive authority** for testing and validation.
- Executes exhaustive tests, fuzzing, edge case analysis
- Validates security and robustness
- Acts on: critical features, pre-release validation, schema changes

### red-team-hacker
**Exclusive authority** for offensive security.
- Performs security audits, penetration testing
- Identifies vulnerabilities and exploit chains
- Acts on: security features, input validation, critical operations

### go-performance-advisor
**Exclusive authority** for performance analysis.
- Static and dynamic analysis of Go code
- Identifies bottlenecks, memory issues, concurrency problems
- Provides optimization recommendations

---

## Execution Rules

### Rule 1: Specification First
```
User Request → spec-orchestrator → SPEC/ → go-elite-developer → Implementation
```
- NO implementation starts without specification in `SPEC/`
- Requirement questions → ALWAYS ask the user

### Rule 2: Skill Delegation
- Code tasks → `go-elite-developer`
- Git tasks → `go-gitflow`
- Specification tasks → `spec-orchestrator`
- Exhaustive testing tasks → `exhaustive-qa-engineer`
- Security tasks → `red-team-hacker`
- Performance tasks → `go-performance-advisor`

### Rule 3: Validation Gates
Before any commit/merge:
1. `go fmt ./...` (format)
2. `go vet ./...` (static analysis)
3. `go test ./...` (tests)
4. `go build -o ./bin/ ./cmd/rmp` (build)

### Rule 4: Output Standards
- **Success**: JSON to stdout
- **Errors/Help**: Plain text to stderr
- **Dates**: ISO 8601 UTC

### Rule 5: Commit Standards (Strict)

#### Forbidden
- **NO reference to Claude** in commit messages
- **NO `Co-Author`** or similar in commits
- **NO mention** of AI assistants, external tools, or code origin

#### Required
- **Detailed description** of what was changed
- **Reason for the change** (why, not just what)
- **Technical context** relevant (structs, functions, affected packages)
- **Impact** of changes (breaking changes, dependencies, etc.)

#### Format
```
type(scope): subject

- Detailed explanation of what changed
- Technical reasoning for the change
- Impact on existing code
- References to SPEC/ if applicable
```

---

## Project Structure

```
/Users/flaviocfo/dev/github.com/FlavioCFOliveira/Groadmap/
├── cmd/rmp/main.go          # Entry point CLI
├── internal/
│   ├── commands/            # Subcommands (roadmap, task, sprint, audit)
│   ├── db/                  # SQLite, schema, parameterized queries
│   ├── models/              # Structs and enums
│   └── utils/               # JSON, ISO 8601 dates, paths
├── bin/                     # Build output
├── SPEC/                    # Technical specifications (spec-orchestrator only)
└── CLAUDE.md               # This file
```

---

## Development Commands

```bash
# Build (output to ./bin/)
go build -o ./bin/ ./cmd/rmp

# Test
go test ./...

# Run (development)
go run ./cmd/rmp [args]        # ex: go run ./cmd/rmp roadmap list

# Format and Vet
go fmt ./...
go vet ./...
```

---

## Technical Constraints

### Security
- Mandatory input validation for all CLI arguments
- Parameterized SQL queries (prepared statements)
- Filesystem permissions: `0700` for data directory (`~/.roadmaps/`)

### Data Standards
- All dates: ISO 8601 UTC
- Success output: JSON
- Error output: Plain text to stderr

### SQLite
- Individual `.db` files in `~/.roadmaps/`
- Versioned schema
- Migrations when necessary

---

## SPEC Directory Reference

| File | Content | Owner |
|------|---------|-------|
| `SPEC/ARCHITECTURE.md` | System design, structure | spec-orchestrator |
| `SPEC/COMMANDS.md` | CLI hierarchy, aliases | spec-orchestrator |
| `SPEC/DATABASE.md` | SQLite schema, relationships | spec-orchestrator |
| `SPEC/DATA_FORMATS.md` | JSON output schema | spec-orchestrator |
| `SPEC/HELP_EXAMPLES.md` | Help/error messages | spec-orchestrator |
| `SPEC/IMPLEMENTATION_PLAN.md` | Implementation plan | spec-orchestrator |
| `SPEC/MODELS.md` | Model definitions | spec-orchestrator |
| `SPEC/STATE_MACHINE.md` | State machines | spec-orchestrator |

---

## Decision Matrix

| Situation | Action |
|-----------|--------|
| User requests new feature | Invoke `spec-orchestrator` first |
| Specification exists, implement | Invoke `go-elite-developer` |
| Git operations (commit, branch, PR) | Invoke `go-gitflow` |
| Exhaustive testing needed | Invoke `exhaustive-qa-engineer` |
| Security audit | Invoke `red-team-hacker` |
| Performance analysis | Invoke `go-performance-advisor` |
| Requirement question | ASK the user |
| Code vs Specification diverge | Follow specification, ask the user |

---

## Anti-Patterns (Forbidden)

- NEVER implement without specification
- NEVER derive specification from existing code
- NEVER make product decisions (always ask the user)
- NEVER ignore failures in `go vet` or `go test`
- NEVER perform destructive Git operations without confirmation
- NEVER compromise security (input validation, SQL injection)
- NEVER reference Claude/AI in commits (messages must be technical and neutral)
- NEVER add Co-Author in commits (the user is the only author)
- NEVER create project documentation in Portuguese (always in English, per Documentation Standards)
- NEVER use emojis or ornamental characters in technical documentation

---

## Communication Protocol

1. **Understand**: Analyze user request
2. **Routing**: Identify correct skill/agent
3. **Delegation**: Invoke skill with complete context
4. **Validation**: Verify mandatory gates
5. **Delivery**: Present result to user

When multiple agents are needed, orchestrate sequentially according to dependencies.
