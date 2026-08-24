package commands

import (
	"fmt"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file holds the fixture seeds of this package's tests.
//
// Several of them used to reach past the commands into db-layer write methods —
// CreateTask, CreateSprint, UpdateTaskStatus, UpdateTaskPriority,
// UpdateTaskSeverity — that the command layer had already replaced with its own
// transactions. A test of the command layer that builds its state with SQL the
// command layer does not run is testing against a second implementation, which
// is what task #188 retired. Those methods are gone; this package seeds through
// its own commands, which it can, because they are right here.
//
// The commands print their new id as JSON on stdout, so the helpers read it back
// from there rather than from a listing: a listing would only report the id it
// happens to order first.

// createTaskViaCommand runs `task create` and returns the id it reports. Extra
// arguments are appended verbatim, for the tests that need --priority, --severity
// or a type.
func createTaskViaCommand(t *testing.T, roadmap, title, functional, technical, acceptance string, extra ...string) int {
	t.Helper()

	args := append([]string{
		"-r", roadmap, "-t", title, "-fr", functional, "-tr", technical, "-ac", acceptance,
	}, extra...)

	var err error
	out := captureStdout(t, func() { err = taskCreate(args) })
	if err != nil {
		t.Fatalf("`task create` %q: %v", title, err)
	}
	return extractIntID(t, out)
}

// createSprintViaCommand runs `sprint create` and returns the id it reports.
func createSprintViaCommand(t *testing.T, roadmap, title, description string, extra ...string) int {
	t.Helper()

	args := append([]string{"-r", roadmap, "-t", title, "-d", description}, extra...)

	var err error
	out := captureStdout(t, func() { err = sprintCreate(args) })
	if err != nil {
		t.Fatalf("`sprint create` %q: %v", title, err)
	}
	return extractIntID(t, out)
}

// driveTaskToStatus walks one task from BACKLOG to the requested status through
// the commands that are allowed to make each move, and it is the only way a
// fixture in this package should produce a task past BACKLOG.
//
// The route is not a detail of the helper, it is the state machine: BACKLOG
// reaches SPRINT only through `sprint add-tasks`, and DOING and COMPLETED
// refuse to be entered without the commit hash each records
// (SPEC/STATE_MACHINE.md § Valid Transitions, SPEC/COMMANDS.md § Change Status).
// A fixture that wrote the status column directly would state a row the product
// cannot produce, and would keep passing if a transition stopped writing the
// timestamp or the hash that goes with it.
func driveTaskToStatus(t *testing.T, roadmap string, sprintID, taskID int, target models.TaskStatus) {
	t.Helper()

	if target == models.StatusBacklog {
		return
	}

	run(t, func() error {
		return sprintAddTasks([]string{"-r", roadmap, itoa(sprintID), itoa(taskID)})
	})
	if target == models.StatusSprint {
		return
	}

	// The hashes are distinct per task so a fixture with several driven tasks
	// cannot pass an assertion that confuses one task's commit with another's.
	openHash := fmt.Sprintf("%07x", 0x5f93b50+taskID)
	run(t, func() error {
		return taskSetStatus([]string{"-r", roadmap, itoa(taskID), "DOING", "--commit-open", openHash})
	})
	if target == models.StatusDoing {
		return
	}

	run(t, func() error {
		return taskSetStatus([]string{"-r", roadmap, itoa(taskID), "TESTING"})
	})
	if target == models.StatusTesting {
		return
	}

	closeHash := fmt.Sprintf("%07x", 0x7ac10e0+taskID)
	run(t, func() error {
		return taskSetStatus([]string{"-r", roadmap, itoa(taskID), "COMPLETED", "--commit-close", closeHash})
	})
	if target != models.StatusCompleted {
		t.Fatalf("no route to status %q", target)
	}
}
