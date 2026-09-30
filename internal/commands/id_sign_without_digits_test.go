package commands

import (
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestIDSurfaces_ASignWithoutDigitsTakesTheFormatLine is the command-level gate
// of rmp task 568: a lone "-" written in an id position, and one inside a
// comma-separated list, is not an integer, so every id surface — the comment
// subcommands, whose range rule is also exit code 2, included — refuses it with
// its own format line and exit code 2, never with the range line, and before the
// roadmap is opened (SPEC/COMMANDS.md § Entity Identifier Range, "What an integer
// is, for this rule"; § Task ID Lists (Batch Commands)).
func TestIDSurfaces_ASignWithoutDigitsTakesTheFormatLine(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const r = "no-such-roadmap"

	format := func(entity string) string {
		return `invalid input: invalid ` + entity + ` ID: "-" (must be a positive integer)`
	}
	cases := []struct {
		family string
		args   []string
		entity string
	}{
		{"task", []string{"get", "-r", r, "-"}, "task"},
		{"task", []string{"remove", "-r", r, "3,-"}, "task"},
		{"task", []string{"stat", "-r", r, "-", "DOING"}, "task"},
		{"task", []string{"add-dep", "-r", r, "1", "-"}, "dependency task"},
		{"sprint", []string{"get", "-r", r, "-"}, "sprint"},
		{"sprint", []string{"add-tasks", "-r", r, "1", "-"}, "task"},
		{"audit", []string{"history", "-r", r, "TASK", "-"}, "entity"},
		{"task", []string{"comment-add", "-r", r, "-", "--type", "NOTE", "--body", "A note."}, "task"},
		{"task", []string{"comment-list", "-r", r, "-"}, "task"},
		{"task", []string{"comment-edit", "-r", r, "-", "--type", "NOTE"}, "comment"},
		{"task", []string{"comment-remove", "-r", r, "-"}, "comment"},
		{"sprint", []string{"comment-add", "-r", r, "-", "--type", "DECISION", "--body", "A decision."}, "sprint"},
		{"sprint", []string{"comment-list", "-r", r, "-"}, "sprint"},
		{"sprint", []string{"comment-edit", "-r", r, "-", "--type", "UPDATE"}, "comment"},
		{"sprint", []string{"comment-remove", "-r", r, "-"}, "comment"},
	}
	for _, tc := range cases {
		t.Run(tc.family+"_"+tc.args[0], func(t *testing.T) {
			var err error
			withEmptyStdin(t, func() { _, err = dispatchInvocation(t, tc.family, tc.args...) })
			assertPublishedRefusal(t, tc.family+" "+tc.args[0]+" with a lone -", err, utils.ErrInvalidInput, 2,
				format(tc.entity))
		})
	}
}
