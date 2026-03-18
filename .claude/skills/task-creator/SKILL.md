---
name: task-creator
description: |
  **ALWAYS use this skill when the user wants to create new tasks for the Groadmap project implementation plan.**

  This skill is triggered when:
  - User asks to "create a task", "add a task", "new task" for the implementation plan
  - User describes a feature or improvement that needs to be added to IMPLEMENTATION_PLAN.md
  - User mentions "tarefa" (Portuguese) in context of project planning
  - User describes requirements that need to be formalized as implementation tasks
  - User wants to convert ideas, bugs, or features into structured implementation tasks

  **Use this skill even if the user doesn't explicitly say "create task"** - whenever they describe
  something that needs to be implemented, converted into a structured task format for the project roadmap.

  This skill creates professional, production-ready implementation tasks following the Groadmap project standards.
---

# Task Creator Skill

## Purpose

Create structured, professional implementation tasks for the Groadmap project that can be directly added to `IMPLEMENTATION_PLAN.md`. Each task follows the project's established format and includes all necessary information for developers to understand, implement, and validate the work.

## When to Use

**ALWAYS use this skill when:**
- User wants to create new tasks for the implementation plan
- User describes a feature, improvement, or bug fix that needs formalization
- User says things like: "create task", "add task", "new task", "preciso de uma tarefa"
- User describes requirements in conversational form that need structure
- Converting audit findings or review comments into actionable tasks

**DO NOT use this skill for:**
- Creating code TODOs or inline comments
- Creating GitHub issues or external tickets
- Simple reminders or notes

## Task Structure Standards

Every task created MUST follow this exact structure:

```markdown
### TASK-XXX: [Task Title]

**Priority:** [P0-Critical / P1-High / P2-Medium / P3-Low / P4-Optional]
**Dependencies:** [List of task IDs or "None"]
**Complexity:** [Low / Medium / High / Very High]
**Estimated Time:** [X days/weeks]
**Specialists:** [List of recommended specialists]

**Identified Problem:**
[Clear description of the problem or need from a functional perspective]

**Technical Description of Need:**
[Detailed technical explanation of what needs to be implemented]
- Technical requirement 1
- Technical requirement 2
- Technical requirement 3

**Validation Requirements (Acceptance Criteria):**
- [ ] Criterion 1 that validates functional description
- [ ] Criterion 2 that validates technical implementation
- [ ] Criterion 3 for edge cases
- [ ] Criterion 4 for error handling
- [ ] Criterion 5 for performance (if applicable)

**Affected Files:**
- `path/to/file1.go` (description of change)
- `path/to/file2.go` (description of change)

**Files to Create:**
- `path/to/new/file.go` (purpose)
```

## Task Components

### 1. Functional Description (Identified Problem)

**Requirements:**
- Describe the problem from the user's perspective
- Explain WHY this task is needed
- Use clear, non-technical language
- Include impact if not implemented
- Reference any related specifications or audits

**Example:**
```markdown
**Identified Problem:**
The current implementation allows users to create tasks with empty required fields
(description, action, expected_result), which violates the data model validation rules
and creates inconsistent data in the database. This leads to incomplete task records
that cannot be properly processed or displayed.
```

### 2. Technical Description

**Requirements:**
- Describe WHAT needs to be implemented technically
- Include specific technical requirements
- Mention files, functions, or components to modify
- Describe algorithms or approaches
- Include error handling requirements
- Mention any architectural changes

**Example:**
```markdown
**Technical Description of Need:**
Modify `internal/commands/task.go` in `taskEdit` function:
- Before applying updates, validate that required fields are not empty strings
- Required fields: `description`, `action`, `expected_result`
- Return specific error indicating which field is invalid
- Do not apply partial updates if there are invalid fields (all or nothing)
- Maintain current behavior for optional fields (`specialists` can be empty/null)
```

### 3. Acceptance Criteria (Validation Requirements)

**Requirements:**
- Minimum 5 criteria per task
- Must be objectively verifiable (pass/fail)
- Cover: happy path, error cases, edge cases, performance
- Include specific commands or behaviors to test
- Reference exit codes if applicable

**Categories to cover:**
- Functional validation (does it work as intended?)
- Technical validation (is the implementation correct?)
- Error handling (how does it fail?)
- Edge cases (boundary conditions)
- Integration (does it work with other components?)

**Example:**
```markdown
**Validation Requirements:**
- [ ] `./rmp task edit -r roadmap 1 --description ""` must return error
- [ ] `./rmp task edit -r roadmap 1 --action ""` must return error
- [ ] `./rmp task edit -r roadmap 1 --expected-result ""` must return error
- [ ] `./rmp task edit -r roadmap 1 --specialists ""` must work (optional field)
- [ ] Error messages must clearly indicate the invalid field
- [ ] Valid updates must continue to work normally
- [ ] Unit tests must cover required field validation
```

### 4. Specialists Assignment

**Available Specialists:**
- `go-elite-developer` - Go code implementation, refactoring
- `spec-orchestrator` - Technical specifications, requirements
- `go-gitflow` - Git operations, branching, commits
- `red-team-hacker` - Security audits, vulnerability assessment
- `go-performance-advisor` - Performance optimization, benchmarking
- `exhaustive-qa-engineer` - Testing, validation, edge cases
- `doc-manager` - Documentation updates

**Assignment Guidelines:**
- Security-related tasks → `red-team-hacker`
- Performance tasks → `go-performance-advisor`
- Database schema changes → `go-elite-developer` + `exhaustive-qa-engineer`
- New features → `spec-orchestrator` (first) + `go-elite-developer`
- Git operations → `go-gitflow`
- Documentation → `doc-manager`

### 5. Priority Classification

**P0 - Critical:**
- Blocking for production
- Security vulnerabilities
- Data loss risks
- Complete feature failures
- SLA: 1-2 days

**P1 - High:**
- High security/stability risk
- Significant user impact
- Workaround exists but is painful
- SLA: 1 week

**P2 - Medium:**
- Important improvements
- Technical debt reduction
- Performance optimizations
- SLA: 2 weeks

**P3 - Low:**
- Desirable improvements
- Nice to have features
- Minor optimizations
- SLA: 1 month

**P4 - Optional:**
- Future considerations
- Experimental features
- Backlog items
- SLA: As time permits

### 6. Complexity Assessment

**Low:**
- Single file changes
- Well-understood patterns
- Minimal testing required
- 1-2 days

**Medium:**
- Multiple files affected
- Some architectural decisions
- Requires unit tests
- 2-4 days

**High:**
- Cross-package changes
- Significant refactoring
- Integration tests required
- 4-7 days

**Very High:**
- Architectural changes
- New patterns or technologies
- Comprehensive testing needed
- 1-2 weeks

## Execution Workflow

### Step 1: Understand the Requirement

1. Read the user's description carefully
2. Ask clarifying questions if needed:
   - What is the specific problem?
   - What is the expected outcome?
   - Are there any constraints?
   - What is the priority?
3. Check existing tasks in IMPLEMENTATION_PLAN.md to avoid duplicates
4. Identify the next available task number

### Step 2: Determine Task Metadata

Based on the requirement, determine:
- **Priority:** Use classification guidelines above
- **Complexity:** Estimate based on scope
- **Time:** Use complexity to estimate
- **Dependencies:** Check if other tasks must be completed first
- **Specialists:** Assign based on task type

### Step 3: Draft the Task

Create the task following the structure above:
1. Write the functional description (why)
2. Write the technical description (what/how)
3. Define acceptance criteria (validation)
4. Identify affected files
5. List files to create

### Step 4: Validate Completeness

Before presenting to user, verify:
- [ ] Functional description is clear and non-technical
- [ ] Technical description has actionable details
- [ ] At least 5 acceptance criteria defined
- [ ] Criteria are objectively verifiable
- [ ] Specialists are appropriate
- [ ] Priority matches urgency
- [ ] Time estimate is realistic
- [ ] File paths follow project structure

### Step 5: Present to User

Present the task in this format:

```
## Nova Tarefa Criada

### Resumo
- **Código:** TASK-XXX
- **Prioridade:** P[X]
- **Complexidade:** [Low/Medium/High]
- **Tempo Estimado:** [X dias]
- **Especialistas:** [list]

### Descrição Funcional
[Summary]

### Descrição Técnica
[Summary]

### Critérios de Aceitação
[Number of criteria]

### Ficheiros Afetados
[List]

**Deseja adicionar esta tarefa ao IMPLEMENTATION_PLAN.md?**
```

### Step 6: Add to Implementation Plan (if requested)

If user confirms:
1. Read current IMPLEMENTATION_PLAN.md
2. Identify correct section based on priority
3. Insert task in appropriate phase
4. Update completion checklist
5. Write updated file

## Examples

### Example 1: Security Task

**User Input:** "Preciso de uma tarefa para verificar se há vulnerabilidades de SQL injection no código"

**Output:**
```markdown
### TASK-XXX: Security Audit - SQL Injection Vulnerability Assessment

**Priority:** P0 - Critical
**Dependencies:** None
**Complexity:** Medium
**Estimated Time:** 3 days
**Specialists:** red-team-hacker, exhaustive-qa-engineer

**Identified Problem:**
The application uses SQLite for data persistence. Without proper validation,
dynamic SQL queries could be vulnerable to injection attacks, potentially allowing
unauthorized data access, modification, or deletion.

**Technical Description of Need:**
Conduct comprehensive security audit of all database operations:
- Review all SQL query constructions in `internal/db/queries.go`
- Verify all queries use parameterized statements (prepared queries)
- Check for any string concatenation in SQL building
- Validate input sanitization before database operations
- Test with malicious inputs: `' OR '1'='1`, `'; DROP TABLE tasks; --`
- Verify whitelist validation for dynamic field updates
- Check transaction boundaries for injection scenarios

**Validation Requirements:**
- [ ] All SQL queries use `?` placeholders, no string concatenation
- [ ] Attempted SQL injection in task description returns error, not executed
- [ ] Dynamic field updates validate against whitelist
- [ ] No raw user input reaches SQL execution
- [ ] Security report documents all findings with CVE references
- [ ] Remediation steps provided for any vulnerabilities found
- [ ] Regression tests prevent future SQL injection vulnerabilities

**Affected Files:**
- `internal/db/queries.go` (audit all query functions)
- `internal/db/connection.go` (verify transaction safety)
- `internal/commands/task.go` (validate input handling)
- `internal/commands/sprint.go` (validate input handling)

**Files to Create:**
- `SECURITY/sql-injection-audit-XXXX-XX-XX.md` (security report)
```

### Example 2: Feature Task

**User Input:** "Cria uma tarefa para adicionar autocompletion na shell"

**Output:**
```markdown
### TASK-XXX: Add Shell Autocompletion Support

**Priority:** P3 - Low
**Dependencies:** None
**Complexity:** Low
**Estimated Time:** 1-2 days
**Specialists:** go-elite-developer

**Identified Problem:**
The CLI does not support shell autocompletion, reducing user experience and
increasing likelihood of command errors. Users must remember exact command names,
flags, and valid values without assistance.

**Technical Description of Need:**
Implement shell autocompletion using Cobra's built-in support:
- Add `completion` command that generates completion scripts
- Support Bash, Zsh, Fish, and PowerShell
- Implement custom completions for:
  - Roadmap names (from `~/.roadmaps/`)
  - Task IDs (from current roadmap)
  - Sprint IDs (from current roadmap)
  - Status values (BACKLOG, SPRINT, DOING, TESTING, COMPLETED)
- Use `ValidArgsFunction` in Cobra for dynamic completion
- Document setup in README.md

**Validation Requirements:**
- [ ] `rmp completion bash` generates valid Bash script
- [ ] `rmp completion zsh` generates valid Zsh script
- [ ] Roadmap name completion lists existing roadmaps
- [ ] Task ID completion lists tasks from current/default roadmap
- [ ] Status completion suggests valid statuses
- [ ] Completion scripts work after following setup instructions
- [ ] Unit tests validate completion logic

**Affected Files:**
- `cmd/rmp/main.go` (add completion command)
- `README.md` (add completion setup instructions)

**Files to Create:**
- `internal/commands/completion.go` (completion logic)
- `internal/commands/completion_test.go` (completion tests)
```

## Language Guidelines

- **User interaction:** Portuguese (Portugal) - PT-pt
- **Task content:** English (technical documentation)
- **Code references:** Use actual file paths from project
- **Technical terms:** May remain in English when appropriate

## Output Format

**ALWAYS** present the task in a clear, structured format that:
1. Shows the complete task structure
2. Highlights key metadata (priority, time, specialists)
3. Asks for confirmation before adding to IMPLEMENTATION_PLAN.md
4. Provides option to edit before finalizing

## Success Criteria

A task is successfully created when:
- [ ] Functional description explains the "why" clearly
- [ ] Technical description explains the "what" and "how"
- [ ] At least 5 acceptance criteria are defined
- [ ] Criteria are objectively verifiable
- [ ] Specialists are appropriate for the task type
- [ ] Priority reflects actual urgency
- [ ] Time estimate is realistic
- [ ] File paths follow project conventions
- [ ] User confirms the task meets their needs
