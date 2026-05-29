// Package aihelp — common workflows catalogue.
//
// This file holds the curated list of end-to-end command sequences that
// an AI agent is expected to perform against the rmp CLI. The list is
// mandated by SPEC/DATA_FORMATS.md § AI Agent Contract (mandatory
// `common_workflows` entries) and is intentionally hand-written rather
// than derived from the registry: the registry knows which commands
// exist, not how they compose.
//
// Two invariants are enforced by unit test in generator_test.go:
//
//  1. Every workflow name in the SPEC's mandatory table is present.
//  2. Every step.command's first token resolves to a real registered
//     command (and its second token, when present, to a real subcommand
//     under that family). This guarantees the contract is internally
//     consistent: an agent that picks a workflow can rely on the
//     `commands` array of the same contract to discover the full flag
//     list of each step.
//
// The placeholder syntax inside step.command strings uses angle
// brackets (`<name>`, `<sprint-id>`, ...). Agents are expected to
// substitute these with caller-supplied values; the strings are NOT
// shell-executable as-is.
package aihelp

// staticWorkflows returns the six canonical workflows required by
// SPEC/DATA_FORMATS.md § AI Agent Contract. The slice is returned
// fresh on every call so the caller may mutate it without affecting
// later invocations (defensive copy semantics consistent with
// staticConventions / staticExitCodes).
func staticWorkflows() []Workflow {
	return []Workflow{
		{
			Name: "bootstrap_new_project",
			Description: "Create a fresh roadmap, populate the backlog with one task per work item, " +
				"create the first sprint, attach the chosen backlog tasks to it, and start the sprint. " +
				"Use when an agent is asked to set up tracking for a project that has no existing roadmap database.",
			Prerequisites: []string{
				"No roadmap with the target name exists yet (verify with `rmp roadmap list`).",
				"The desired roadmap name matches the regex ^[a-z0-9_-]+$ and is no longer than 50 characters.",
			},
			Steps: []WorkflowStep{
				{
					Command: "rmp roadmap create <name>",
					Purpose: "Create the roadmap home directory ~/.roadmaps/<name>/ and its SQLite database project.db, registering the roadmap.",
				},
				{
					Command: "rmp task create -r <name> -t \"<title>\" -fr \"<functional-requirements>\" -tr \"<technical-requirements>\" -ac \"<acceptance-criteria>\" --type <TYPE> --priority <0-9>",
					Purpose: "Populate the backlog with one task per work item. Repeat once per task; each invocation returns the new task ID on stdout.",
				},
				{
					Command: "rmp sprint create -r <name> -d \"<description>\" --max-tasks <n>",
					Purpose: "Create the first sprint in PENDING state. Returns the new sprint ID on stdout.",
				},
				{
					Command: "rmp sprint add-tasks -r <name> <sprint-id> <task-id-1,task-id-2,...>",
					Purpose: "Move selected backlog tasks into the sprint. Tasks transition BACKLOG to SPRINT automatically.",
				},
				{
					Command: "rmp sprint start -r <name> <sprint-id>",
					Purpose: "Transition the sprint from PENDING to OPEN so `rmp task next` will return its tasks.",
				},
			},
			ExpectedOutcome: "One roadmap exists, one sprint is in OPEN state, and that sprint contains the selected tasks in SPRINT status.",
		},
		{
			Name: "plan_next_sprint",
			Description: "From an existing roadmap with a populated backlog, choose the next batch of work, " +
				"create a new sprint in PENDING state, and attach the chosen backlog tasks to it. " +
				"Use between development cycles when there is already an active or recently closed sprint.",
			Prerequisites: []string{
				"Roadmap `<name>` exists.",
				"The backlog contains at least one task in BACKLOG status (verify with `rmp backlog list`).",
			},
			Steps: []WorkflowStep{
				{
					Command: "rmp backlog list -r <name>",
					Purpose: "Inspect the current backlog and identify the task IDs to include in the next sprint.",
				},
				{
					Command: "rmp sprint create -r <name> -d \"<description>\" --max-tasks <n>",
					Purpose: "Create the new sprint in PENDING state. Returns the new sprint ID on stdout.",
				},
				{
					Command: "rmp sprint add-tasks -r <name> <sprint-id> <task-id-1,task-id-2,...>",
					Purpose: "Attach the selected backlog tasks to the new sprint. Tasks transition BACKLOG to SPRINT automatically.",
				},
				{
					Command: "rmp sprint tasks -r <name> <sprint-id>",
					Purpose: "Verify the new sprint contains the expected task set in the expected order.",
				},
			},
			ExpectedOutcome: "A new sprint exists in PENDING state containing the selected backlog tasks in SPRINT status, ready to be started.",
		},
		{
			Name: "close_active_sprint_and_open_next",
			Description: "Mark the currently OPEN sprint as CLOSED, returning any unfinished tasks to the backlog if " +
				"necessary, and promote the next PENDING sprint to OPEN. Use at the end of a development cycle " +
				"when the team is ready to move on.",
			Prerequisites: []string{
				"Roadmap `<name>` exists.",
				"Exactly one sprint is in OPEN state (verify with `rmp sprint list -r <name> --status OPEN`).",
				"At least one sprint is in PENDING state if the team intends to promote one next.",
			},
			Steps: []WorkflowStep{
				{
					Command: "rmp sprint open-tasks -r <name> <open-sprint-id>",
					Purpose: "List the unfinished tasks (SPRINT, DOING, TESTING) still attached to the open sprint.",
				},
				{
					Command: "rmp sprint remove-tasks -r <name> <open-sprint-id> <task-id-1,task-id-2,...>",
					Purpose: "Return any unfinished tasks the team does not want to close with the sprint back to BACKLOG status.",
				},
				{
					Command: "rmp sprint close -r <name> <open-sprint-id> --force",
					Purpose: "Close the active sprint. Use --force only if you have already removed or accepted the unfinished tasks; otherwise omit --force and complete them first.",
				},
				{
					Command: "rmp sprint start -r <name> <next-pending-sprint-id>",
					Purpose: "Promote the next PENDING sprint to OPEN so `rmp task next` will start returning its tasks.",
				},
			},
			ExpectedOutcome: "The previously OPEN sprint is CLOSED, the chosen PENDING sprint is now OPEN, and any unfinished tasks from the closed sprint are back in BACKLOG.",
		},
		{
			Name: "reprioritise_backlog",
			Description: "Inspect the backlog, change the priority of selected tasks, and verify the resulting " +
				"order. Use when planning shifts or new information re-ranks pending work.",
			Prerequisites: []string{
				"Roadmap `<name>` exists.",
				"At least one task is in BACKLOG status.",
			},
			Steps: []WorkflowStep{
				{
					Command: "rmp backlog list -r <name>",
					Purpose: "Inspect the current backlog and identify the task IDs whose priorities need to change.",
				},
				{
					Command: "rmp task prio -r <name> <task-id-1,task-id-2,...> <new-priority>",
					Purpose: "Set the new priority (0-9) on each chosen task. Batch operation: all IDs must be valid or no change is made.",
				},
				{
					Command: "rmp backlog list -r <name>",
					Purpose: "Re-list the backlog to confirm the new ordering matches expectations.",
				},
			},
			ExpectedOutcome: "Each chosen backlog task carries its new priority value, and the backlog listing reflects the new ordering.",
		},
		{
			Name: "move_task_between_sprints",
			Description: "Transfer one or more tasks from one sprint to another without altering their task status. " +
				"Use when re-allocating work between an OPEN sprint and a PENDING sprint, or between two PENDING sprints.",
			Prerequisites: []string{
				"Roadmap `<name>` exists.",
				"Both the source sprint and the destination sprint exist.",
				"The destination sprint is not in CLOSED state.",
			},
			Steps: []WorkflowStep{
				{
					Command: "rmp sprint tasks -r <name> <from-sprint-id>",
					Purpose: "Inspect the source sprint and identify the task IDs to move.",
				},
				{
					Command: "rmp sprint move-tasks -r <name> <from-sprint-id> <to-sprint-id> <task-id-1,task-id-2,...>",
					Purpose: "Atomically transfer the chosen tasks from the source sprint to the destination sprint. Task status is preserved.",
				},
				{
					Command: "rmp sprint tasks -r <name> <to-sprint-id>",
					Purpose: "Verify the destination sprint now lists the moved tasks.",
				},
			},
			ExpectedOutcome: "The named tasks are now attached to the destination sprint, no longer attached to the source sprint, and their task status is unchanged.",
		},
		{
			Name: "complete_task_with_summary",
			Description: "Walk a task through the SPRINT to DOING to TESTING to COMPLETED transitions, attaching a " +
				"completion summary to the final transition. Use whenever an agent finishes a unit of work " +
				"and wants the summary recorded on the task.",
			Prerequisites: []string{
				"Roadmap `<name>` exists.",
				"The target task is in SPRINT status (i.e. it has already been attached to a sprint via `rmp sprint add-tasks`).",
				"The task has no incomplete dependencies (verify with `rmp task blockers -r <name> <task-id>`).",
			},
			Steps: []WorkflowStep{
				{
					Command: "rmp task stat -r <name> <task-id> DOING",
					Purpose: "Start work on the task. Sets started_at to the current timestamp.",
				},
				{
					Command: "rmp task stat -r <name> <task-id> TESTING",
					Purpose: "Mark the task as ready for testing once implementation is done. Sets tested_at to the current timestamp.",
				},
				{
					Command: "rmp task stat -r <name> <task-id> COMPLETED --summary \"<one-paragraph completion summary>\"",
					Purpose: "Close the task. Sets closed_at and stores the completion summary on the task. --summary is accepted only on this final transition.",
				},
			},
			ExpectedOutcome: "The task is in COMPLETED status, carries the supplied completion_summary, and has all three of started_at, tested_at, and closed_at set.",
		},
	}
}
