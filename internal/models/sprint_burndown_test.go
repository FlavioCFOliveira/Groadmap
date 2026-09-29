package models

import (
	"slices"
	"testing"
)

// TestCalculateSprintBurndown pins the burndown derived from the member read
// (SPEC/COMMANDS.md § Sprint Statistics, Burndown Computation; SPEC/DATABASE.md
// § Join Order of the Sprint Completion Counts) on the cases the SQL grouping it
// replaced decided: which members count as completions, the grouping by calendar
// date in ascending order whatever the member order, the opening entry on the
// start date, and the empty series.
func TestCalculateSprintBurndown(t *testing.T) {
	started := "2026-04-01T08:00:00.000Z"
	sameDay := "2026-04-02T07:00:00.000Z"
	empty := ""
	member := func(status TaskStatus, closedAt string) Task {
		task := Task{Status: status}
		if closedAt != "-" {
			task.ClosedAt = &closedAt
		}
		return task
	}
	// Six members in position order, completions out of date order; one
	// COMPLETED member with no closed_at and one closed member that is not
	// COMPLETED, neither of which is a completion.
	members := []Task{
		member(StatusCompleted, "2026-04-04T09:00:00.000Z"),
		member(StatusDoing, "-"),
		member(StatusCompleted, "2026-04-02T15:00:00.000Z"),
		member(StatusCompleted, "-"),
		member(StatusBacklog, "2026-04-03T10:00:00.000Z"),
		member(StatusCompleted, "2026-04-02T10:00:00.000Z"),
	}

	for _, c := range []struct {
		sprint  Sprint
		name    string
		members []Task
		want    []BurndownEntry
	}{
		{Sprint{StartedAt: &started}, "start date before the first completion", members, []BurndownEntry{
			{Date: "2026-04-01", TasksRemaining: 6},
			{Date: "2026-04-02", TasksRemaining: 4},
			{Date: "2026-04-04", TasksRemaining: 3},
		}},
		{Sprint{}, "no start date", members, []BurndownEntry{
			{Date: "2026-04-02", TasksRemaining: 4},
			{Date: "2026-04-04", TasksRemaining: 3},
		}},
		{Sprint{StartedAt: &empty}, "an empty start date", members, []BurndownEntry{
			{Date: "2026-04-02", TasksRemaining: 4},
			{Date: "2026-04-04", TasksRemaining: 3},
		}},
		{Sprint{StartedAt: &sameDay}, "started on the first completion date", members, []BurndownEntry{
			{Date: "2026-04-02", TasksRemaining: 4},
			{Date: "2026-04-04", TasksRemaining: 3},
		}},
		{Sprint{StartedAt: &started}, "a closed_at shorter than a date", []Task{
			member(StatusCompleted, "2026"), member(StatusSprint, "-"),
		}, []BurndownEntry{{Date: "2026", TasksRemaining: 1}}},
		{Sprint{StartedAt: &started}, "no completion", []Task{
			member(StatusDoing, "-"), member(StatusCompleted, "-"),
		}, []BurndownEntry{}},
		{Sprint{StartedAt: &started}, "no member", nil, []BurndownEntry{}},
	} {
		got := CalculateSprintBurndown(&c.sprint, c.members)
		if got == nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: burndown = %#v, want %#v", c.name, got, c.want)
		}
	}
}

// TestLeadingChars pins the substr(s, 1, n) rule the completion date is cut by:
// characters, not bytes, and the whole string when it is shorter.
func TestLeadingChars(t *testing.T) {
	for _, c := range []struct {
		in, want string
		n        int
	}{
		{"2026-04-02T10:00:00.000Z", "2026-04-02", 10},
		{"2026-04", "2026-04", 10},
		{"", "", 10},
		{"2026-04-0\u00e9T", "2026-04-0\u00e9", 10},
	} {
		if got := leadingChars(c.in, c.n); got != c.want {
			t.Errorf("leadingChars(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
