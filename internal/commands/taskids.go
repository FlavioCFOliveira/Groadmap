package commands

import (
	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// taskIDsOf projects a task slice onto its ids, which is what the id-list
// helpers in internal/utils compare against.
//
// It exists so those helpers can stay free of a models dependency: the set
// arithmetic of SPEC/COMMANDS.md § Task ID Lists (Batch Commands) is about ids
// and nothing else, and keeping it that way lets the sprint assignment commands
// use it over their own membership queries, which return ids rather than tasks.
func taskIDsOf(tasks []models.Task) []int {
	ids := make([]int, len(tasks))
	for i := range tasks {
		ids[i] = tasks[i].ID
	}
	return ids
}

// taskStateIDsOf projects a slice of lean task projections onto its ids, for
// the same id-list helpers taskIDsOf serves.
func taskStateIDsOf(states []db.TaskState) []int {
	ids := make([]int, len(states))
	for i := range states {
		ids[i] = states[i].ID
	}
	return ids
}

// tasksOfStates renders the member read of a sprint as the task values the
// sprint report calculators take (models.CalculateSprintStats,
// models.CalculateSprintShowResult and models.CalculateSprintBurndown). Those
// calculators read a task's ID, Status, Severity and ClosedAt and nothing else,
// which are exactly the columns the member read carries, so the report computed
// from these values is the one the full rows would produce
// (SPEC/IMPLEMENTATION.md, "Read only the columns the caller uses"). The values
// are for those calculators only: every other field is its zero value.
func tasksOfStates(states []db.SprintMemberState) []models.Task {
	tasks := make([]models.Task, len(states))
	for i := range states {
		tasks[i] = models.Task{ID: states[i].ID, Status: states[i].Status, Severity: states[i].Severity}
		if states[i].ClosedAt.Valid {
			tasks[i].ClosedAt = &states[i].ClosedAt.String
		}
	}
	return tasks
}
