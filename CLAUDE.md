# CLAUDE.md

## CRITICAL POLICY REMINDER

**SPECIFICATION FIRST IS MANDATORY AND NON-NEGOTIABLE**

- NO code implementation shall begin without a complete specification in `SPEC/`
- When ANY implementation is requested, invoke `spec-orchestrator` FIRST
- This rule has ZERO exceptions - urgency, simplicity, or user preference do not override it
- **WHEN IN DOUBT: SPEC FIRST. ALWAYS.**

---

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
**Exclusive authority** for technical specification. **THIS AGENT MUST BE INVOKED BEFORE ANY IMPLEMENTATION.**
- **MANDATORY FIRST STEP** for all new features and changes
- Creates and maintains all documents in `SPEC/`
- Produces specifications ONLY from user input
- NEVER derives specifications from existing code
- ALWAYS asks the user for decisions (the user is the single source of truth)
- **Acts as gatekeeper: NO implementation without clear specification**

### go-elite-developer
**Exclusive authority** for Go code development. **ONLY TO BE INVOKED AFTER spec-orchestrator.**
- **MUST NOT be invoked without prior specification in SPEC/**
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
- **MANDATORY: ALL tests must pass without failures before any git push**
- **MANDATORY: Explicit user authorization required after tests pass and before git push**

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

### Rule 1: Specification First (MANDATORY - ZERO EXCEPTIONS)

**THIS RULE IS ABSOLUTE AND NON-NEGOTIABLE. NO EXCEPTIONS EXIST.**

```
User Request → spec-orchestrator → SPEC/ → go-elite-developer → Implementation
```

#### ABSOLUTE REQUIREMENTS:
- **NO implementation starts without specification in `SPEC/`** - This is MANDATORY
- **ALWAYS invoke spec-orchestrator FIRST** when specification is missing
- **NEVER write code directly** in response to feature requests
- **NEVER bypass** this rule due to urgency, simplicity, or user insistence
- Requirement questions → ALWAYS ask the user

#### VIOLATION PROTOCOL:
If code is requested without specification:
1. STOP immediately
2. Inform user: "Vou primeiro invocar o spec-orchestrator para criar a especificação técnica, conforme a política Specification First."
3. Invoke spec-orchestrator
4. ONLY proceed to go-elite-developer after specification exists

**WHEN IN DOUBT: SPEC FIRST. ALWAYS.**

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

### Rule 6: Task Lifecycle and Specification Binding (MANDATORY)

#### Task Creation:
- **NO task can be created without a corresponding specification in `SPEC/`**
- Before creating any task, verify that the functionality is documented in the appropriate SPEC/ file
- Tasks must reference the specific specification document they implement

#### Task Completion:
- **Before marking any task as complete, VALIDATE that the specification exists in `SPEC/`**
- If the specification is missing, the task CANNOT be validated or marked as complete
- The task must remain open until the specification is created and the implementation aligns with it

#### Task Validation Protocol:
1. Review the task requirements
2. Locate the corresponding specification in `SPEC/`
3. If specification is MISSING → STOP and invoke spec-orchestrator
4. Only validate completion when specification exists and implementation matches it

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
├── IMPLEMENTATION_PLAN.md   # Project implementation plan
└── CLAUDE.md               # This file
```

### IMPLEMENTATION_PLAN.md

The `/IMPLEMENTATION_PLAN.md` file stores the project implementation plan and is divided into two sections:
- **Pending Tasks**: Tasks yet to be implemented
- **Completed Tasks**: Tasks that have been finished

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
| `SPEC/BUILD.md` | Build system, CI/CD workflows | spec-orchestrator |
| `SPEC/COMMANDS.md` | CLI hierarchy, aliases | spec-orchestrator |
| `SPEC/DATABASE.md` | SQLite schema, relationships | spec-orchestrator |
| `SPEC/DATA_FORMATS.md` | JSON output schema | spec-orchestrator |
| `SPEC/DEPLOY.md` | Deployment, installation | spec-orchestrator |
| `SPEC/HELP_EXAMPLES.md` | Help/error messages | spec-orchestrator |
| `SPEC/MODELS.md` | Model definitions | spec-orchestrator |
| `SPEC/STATE_MACHINE.md` | State machines | spec-orchestrator |
| `SPEC/VERSION.md` | Version management | spec-orchestrator |

---

## Decision Matrix

| Situation | Action |
|-----------|--------|
| **User requests new feature** | **MANDATORY: Invoke `spec-orchestrator` FIRST - NO EXCEPTIONS** |
| **User requests code changes** | **MANDATORY: Verify SPEC/ exists first, else invoke `spec-orchestrator`** |
| Specification exists, implement | Invoke `go-elite-developer` |
| Git operations (commit, branch, PR) | Invoke `go-gitflow` |
| Exhaustive testing needed | Invoke `exhaustive-qa-engineer` |
| Security audit | Invoke `red-team-hacker` |
| Performance analysis | Invoke `go-performance-advisor` |
| Requirement question | ASK the user |
| Code vs Specification diverge | Follow specification, ask the user |

---

## Anti-Patterns (FORBIDDEN - ZERO TOLERANCE)

### CRITICAL VIOLATIONS (Will Result in Immediate Rejection):
- **NEVER implement without specification** - THIS IS THE #1 RULE. NO EXCEPTIONS.
- **NEVER derive specification from existing code** - Specifications come ONLY from user input via spec-orchestrator
- **NEVER make product decisions** - Always ask the user

### Other Forbidden Patterns:
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
