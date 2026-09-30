package commands

import (
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The gates of rmp task 503: every bounded integer flag, and the positional
// <priority> and <severity>, refuses a value it cannot read as an integer with
// exit code 2 and one line naming the flag, echoing the value and stating the
// published range — never the text of the library routine that failed to read
// it (SPEC/COMMANDS.md § Create Task, "Every bounded integer flag is refused in
// the same shape"). The listing filters also check the range of priority and
// severity, with exit code 6, before the roadmap is opened.

// TestBoundedIntegerFlags_RefuseANonIntegerWithThePublishedLine drives every
// surface the SPEC names, with a word and with a value too large for the
// platform's integer type, which cannot be read as an integer either.
func TestBoundedIntegerFlags_RefuseANonIntegerWithThePublishedLine(t *testing.T) {
	const roadmap = "bounded-integer-flags"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	sprintID := createSprintViaCommand(t, roadmap, "Settlement retries", "Bound the settlement retry ladder.")
	taskID := mkSprintTask(t, roadmap, "Cap the retry ladder at five attempts")

	const tooLarge = "99999999999999999999"
	create := []string{"create", "-r", roadmap, "-t", "Cap the retry ladder", "-fr", "Bounded retries.",
		"-tr", "A capped ladder.", "-ac", "The sixth attempt is refused."}

	cases := []struct {
		label  string
		family string
		args   []string
		want   string
	}{
		{"task create -p", "task", append(append([]string{}, create...), "-p", "high"),
			`invalid input: invalid value for --priority: "high" is not an integer in 0-9`},
		{"task create --severity (too large)", "task", append(append([]string{}, create...), "--severity", tooLarge),
			`invalid input: invalid value for --severity: "` + tooLarge + `" is not an integer in 0-9`},
		{"task edit --priority", "task", []string{"edit", "-r", roadmap, itoa(taskID), "--priority", "7.5"},
			`invalid input: invalid value for --priority: "7.5" is not an integer in 0-9`},
		{"task edit --severity", "task", []string{"edit", "-r", roadmap, itoa(taskID), "--severity", "severe"},
			`invalid input: invalid value for --severity: "severe" is not an integer in 0-9`},
		{"task list --severity", "task", []string{"list", "-r", roadmap, "--severity", "high"},
			`invalid input: invalid value for --severity: "high" is not an integer in 0-9`},
		{"task list -l", "task", []string{"list", "-r", roadmap, "-l", "ten"},
			`invalid input: invalid value for --limit: "ten" is not an integer in 1-100`},
		{"task list --limit (too large)", "task", []string{"list", "-r", roadmap, "--limit", tooLarge},
			`invalid input: invalid value for --limit: "` + tooLarge + `" is not an integer in 1-100`},
		{"backlog list -p", "backlog", []string{"list", "-r", roadmap, "-p", "urgent"},
			`invalid input: invalid value for --priority: "urgent" is not an integer in 0-9`},
		{"backlog list -l", "backlog", []string{"list", "-r", roadmap, "-l", "all"},
			`invalid input: invalid value for --limit: "all" is not an integer in 1-100`},
		{"audit list -l", "audit", []string{"list", "-r", roadmap, "-l", "all"},
			`invalid input: invalid value for --limit: "all" is not an integer in 1-500`},
		{"sprint create --max-tasks", "sprint", []string{"create", "-r", roadmap, "-t", "Ledger close",
			"-d", "Close the ledger nightly.", "--max-tasks", "many"},
			`invalid input: invalid value for --max-tasks: "many" is not an integer in 1-10000`},
		{"sprint update --max-tasks", "sprint", []string{"update", "-r", roadmap, itoa(sprintID), "--max-tasks", "many"},
			`invalid input: invalid value for --max-tasks: "many" is not an integer in 1-10000`},
		{"task prio <priority> (too large)", "task", []string{"prio", "-r", roadmap, itoa(taskID), tooLarge},
			`invalid input: invalid priority: "` + tooLarge + `" is not an integer in 0-9`},
		{"task sev <severity>", "task", []string{"sev", "-r", roadmap, itoa(taskID), "7.5"},
			`invalid input: invalid severity: "7.5" is not an integer in 0-9`},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			out, err := dispatchInvocation(t, tc.family, tc.args...)
			assertPublishedRefusal(t, tc.label, err, utils.ErrInvalidInput, 2, tc.want)
			if out != "" {
				t.Errorf("a refused invocation wrote to stdout: %q", out)
			}
		})
	}
}

// TestListingFilters_RefuseAPriorityOrSeverityOutOfRange pins the range half:
// `task list` and `backlog list` refuse an integer outside 0-9 with exit code 6
// and the range line `task create` prints, before the roadmap is opened — which
// the roadmap that does not exist proves, since its refusal would be exit code 4.
func TestListingFilters_RefuseAPriorityOrSeverityOutOfRange(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const absent = "no-such-roadmap"

	cases := []struct {
		label  string
		family string
		args   []string
		want   string
	}{
		{"task list -p 10", "task", []string{"list", "-r", absent, "-p", "10"},
			"validation error: priority must be between 0 and 9, got 10"},
		{"task list --priority -1", "task", []string{"list", "-r", absent, "--priority", "-1"},
			"validation error: priority must be between 0 and 9, got -1"},
		{"task list --severity 12", "task", []string{"list", "-r", absent, "--severity", "12"},
			"validation error: severity must be between 0 and 9, got 12"},
		{"backlog list -p 10", "backlog", []string{"list", "-r", absent, "-p", "10"},
			"validation error: priority must be between 0 and 9, got 10"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := dispatchInvocation(t, tc.family, tc.args...)
			assertPublishedRefusal(t, tc.label, err, utils.ErrValidation, 6, tc.want)
		})
	}

	// The boundaries themselves are accepted: the refusal is of the range, not
	// of the flag. Against the absent roadmap the command then fails where the
	// roadmap is opened, with exit code 4.
	for _, args := range [][]string{
		{"list", "-r", absent, "-p", "0"},
		{"list", "-r", absent, "--severity", "9"},
	} {
		_, err := dispatchInvocation(t, "task", args...)
		if exitCodeFor(err) != 4 {
			t.Errorf("task %v = %v (exit %d), want the roadmap refusal (exit 4): a bound itself is in range",
				args, err, exitCodeFor(err))
		}
	}
}
