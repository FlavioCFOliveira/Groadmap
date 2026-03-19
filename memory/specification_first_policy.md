---
name: Specification First Policy - STRICT ENFORCEMENT
description: ABSOLUTE requirement that NO code implementation or task can begin/complete without prior written specification in SPEC/ directory. Includes task lifecycle rules. This is non-negotiable.
type: feedback
---

# Specification First Policy - STRICT ENFORCEMENT

## The Rule (ABSOLUTE - ZERO EXCEPTIONS)

**NO CODE IMPLEMENTATION SHALL BEGIN WITHOUT A COMPLETE SPECIFICATION DOCUMENT IN THE SPEC/ DIRECTORY.**

This is not a guideline. This is not a preference. This is MANDATORY and NON-NEGOTIABLE.

## Why This Policy Exists

**Why:** Past incidents where code was written without clear requirements led to:
- Misaligned implementations that did not match user expectations
- Rework and wasted development effort
- Technical debt from assumptions made without proper specification
- Features that solved the wrong problem

**How to apply:** When ANY implementation task is requested:
1. STOP immediately
2. Check if SPEC/ contains the specification
3. If NO specification exists → INVOKE spec-orchestrator FIRST
4. NEVER proceed to go-elite-developer without specification
5. NEVER write code directly in response to feature requests

## Enforcement Protocol

### Before Any Implementation:
- [ ] Verify SPEC/ contains the relevant specification
- [ ] Confirm spec-orchestrator has been invoked if specification is missing
- [ ] Validate that requirements are clear and unambiguous
- [ ] Only then delegate to go-elite-developer

### Violation Consequences:
- Implementation without specification is FORBIDDEN
- Code written without spec must be rejected
- Always route through spec-orchestrator first

## User Confirmation Required

When user requests implementation:
1. Acknowledge the request
2. State clearly: "Vou primeiro invocar o spec-orchestrator para criar a especificação técnica, conforme a política Specification First."
3. Invoke spec-orchestrator
4. Only proceed to implementation after specification is approved

## This Policy Overrides:
- Urgency ("faz isto rapidamente")
- Simplicity ("é só uma coisa pequena")
- User insistence ("não precisa de especificação")
- Assumptions about requirements

**WHEN IN DOUBT: SPEC FIRST. ALWAYS.**

---

## Task Lifecycle and Specification Binding

### Task Creation Rule:
- **NO task can be created without a corresponding specification in `SPEC/`**
- Before creating any task, verify that the functionality is documented
- Tasks must reference the specific specification document they implement

### Task Completion Rule:
- **Before marking any task as complete, VALIDATE that the specification exists in `SPEC/`**
- If the specification is missing, the task CANNOT be validated or marked as complete
- The task must remain open until the specification is created

### Task Validation Protocol:
1. Review the task requirements
2. Locate the corresponding specification in `SPEC/`
3. If specification is MISSING → STOP and invoke spec-orchestrator
4. Only validate completion when specification exists and implementation matches it
