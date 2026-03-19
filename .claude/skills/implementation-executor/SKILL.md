---
name: implementation-executor
description: |
  Execute implementation tasks from IMPLEMENTATION_PLAN.md by reading task requirements,
  implementing them according to specifications, verifying against acceptance criteria,
  marking tasks as complete, and creating git commits. ALWAYS trigger this skill when the user asks to
  "advance", "execute", "implement", or "develop" one or more tasks from the implementation
  plan. This skill manages the full task lifecycle from selection through completion including
  automatic git commit creation after each task.
  Trigger phrases: "avançar com", "executar", "implementar", "desenvolver",
  "próxima tarefa", "next task", "proseguir com", "seguir com as tarefas".
---

# Implementation Executor

Execute tasks from IMPLEMENTATION_PLAN.md following a structured workflow.

## When to Use

This skill MUST be triggered when the user requests to:
- Execute/implement/develop one or more tasks from the implementation plan
- Advance with specific tasks or the next available task
- Process tasks sequentially from the plan

## Task Selection Strategy

When the user asks to execute tasks without specifying which ones:

1. **Read IMPLEMENTATION_PLAN.md** to identify all pending tasks
2. **Prioritize by simplicity and dependencies**:
   - Tasks with NO dependencies come first
   - Among those, choose the SIMPLEST task (least complex description)
   - Only after completing simpler tasks, move to complex/dependent ones
3. **Process SEQUENTIALLY** - one task at a time, never in parallel

When the user specifies task IDs, respect their order unless dependencies require otherwise.

## Execution Workflow

For EACH task:

### Step 1: Understand the Task
- Read the task from IMPLEMENTATION_PLAN.md
- Extract: task ID, description, acceptance criteria, dependencies
- **Verify dependencies are completed** - if not, stop and inform the user
- Check if SPEC/ documentation exists for the task (per Specification First policy)

### Step 2: Verify Specification Exists
- Before implementing, confirm the task has a corresponding specification in SPEC/
- If specification is missing:
  - STOP implementation
  - Inform the user that specification is required per CLAUDE.md policy
  - Suggest invoking spec-orchestrator first

### Step 3: Implement the Task
- Use go-elite-developer for Go code tasks
- Follow the acceptance criteria as requirements
- Ensure code compiles: `go build -o ./bin/ ./cmd/rmp`
- Run tests: `go test ./...`
- Apply formatting: `go fmt ./...`

### Step 4: Verify Against Acceptance Criteria
- Review each acceptance criterion
- Confirm implementation meets all criteria
- If any criterion fails, fix before marking complete

### Step 5: Update IMPLEMENTATION_PLAN.md
- Move the task from "Pending Tasks" to "Completed Tasks"
- Preserve the original task description and metadata
- Add completion timestamp if helpful

### Step 6: Create Git Commit
- **MANDATORY**: After successfully updating IMPLEMENTATION_PLAN.md, create a git commit
- Use `go-gitflow` skill to create the commit
- Commit message must follow the format from CLAUDE.md:
  - Type: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, or `chore`
  - Scope: affected module/component
  - Subject: brief description
  - Body: detailed explanation of changes
- Include all modified files (implementation + IMPLEMENTATION_PLAN.md)
- Wait for commit confirmation before proceeding

### Step 7: Report Success
- Provide brief summary of what was implemented
- Mention key files changed
- Confirm task is marked complete
- Confirm git commit was created

## Output Format

Report to user with:
- Task ID completed
- Brief description of implementation
- Key changes made
- Confirmation of plan update

Keep reports concise - no verbose output unless requested.

## Constraints

- **ALWAYS sequential execution** - never parallel task development
- **NEVER skip dependency checks** - blocked tasks must be surfaced
- **NEVER implement without specification** - per Specification First policy
- **ALWAYS verify acceptance criteria** before marking complete
- **NEVER modify multiple tasks simultaneously** - one at a time
- **ALWAYS create git commit** after updating IMPLEMENTATION_PLAN.md for each completed task

## Example Workflow

User: "avançar com a próxima tarefa"

1. Read IMPLEMENTATION_PLAN.md
2. Identify pending tasks
3. Select simplest task with no dependencies (e.g., task-001)
4. Verify specification exists in SPEC/
5. Implement using go-elite-developer
6. Verify against acceptance criteria
7. Move task-001 to Completed Tasks section
8. Create git commit using go-gitflow with all changes
9. Report: "Task 001 complete: Implemented X in file Y. Git commit created. Acceptance criteria verified."

## Integration with Other Skills

- **spec-orchestrator**: Invoke if specification is missing
- **go-elite-developer**: Use for all Go implementation work
- **go-gitflow**: **MANDATORY** - Use to create git commit after each task completion (Step 6)
- **exhaustive-qa-engineer**: Invoke if complex testing needed
