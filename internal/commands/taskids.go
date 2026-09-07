package commands

import "github.com/FlavioCFOliveira/Groadmap/internal/models"

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
