package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestSprintStatsHelpMatchesDaysElapsedBehaviour pins the published help text of
// `rmp sprint stats` to what the model actually computes for days_elapsed.
//
// The help text used to read "For CLOSED sprints it spans started_at ->
// closed_at", which was false: ApplySprintMetrics sets DaysElapsed only in the
// SprintOpen branch, so a closed sprint reports null. Nothing compared the help
// string against the code, so the claim shipped as published contract and was
// repeated into DOCS/commands/sprint.md (rmp task #429).
//
// The test drives the model first and reads the help second, so it fails if
// either side moves without the other.
func TestSprintStatsHelpMatchesDaysElapsedBehaviour(t *testing.T) {
	started := "2026-01-01T00:00:00.000Z"
	closed := "2026-01-11T00:00:00.000Z"
	now := utils.FormatISO8601(time.Date(2026, 1, 21, 0, 0, 0, 0, time.UTC))

	// A CLOSED sprint that genuinely ran for ten days.
	closedSprint := &models.Sprint{
		ID:        1,
		Status:    models.SprintClosed,
		StartedAt: &started,
		ClosedAt:  &closed,
	}
	closedStats := models.CalculateSprintStats(1, nil)
	closedStats.ApplySprintMetrics(closedSprint, nil, now)

	if closedStats.DaysElapsed != nil {
		t.Fatalf("a CLOSED sprint must report a null days_elapsed, got %d", *closedStats.DaysElapsed)
	}

	// An OPEN sprint is the one status that reports a figure.
	openSprint := &models.Sprint{
		ID:        2,
		Status:    models.SprintOpen,
		StartedAt: &started,
	}
	openStats := models.CalculateSprintStats(2, nil)
	openStats.ApplySprintMetrics(openSprint, nil, now)

	if openStats.DaysElapsed == nil {
		t.Fatal("an OPEN sprint with a started_at must report a days_elapsed figure, got null")
	}

	// A PENDING sprint reports nothing either.
	pendingStats := models.CalculateSprintStats(3, nil)
	pendingStats.ApplySprintMetrics(&models.Sprint{ID: 3, Status: models.SprintPending}, nil, now)

	if pendingStats.DaysElapsed != nil {
		t.Fatalf("a PENDING sprint must report a null days_elapsed, got %d", *pendingStats.DaysElapsed)
	}

	help := captureOutput(t, printSprintStatsHelp)

	// The help must name OPEN as the status that carries the figure, and must
	// name CLOSED among those that do not.
	if !strings.Contains(help, "days_elapsed") {
		t.Fatal("the sprint stats help no longer documents days_elapsed at all")
	}

	// The retracted claim, in the exact shape it shipped in, must never return.
	for _, forbidden := range []string{
		"For CLOSED sprints it spans",
		"spans started_at -> closed_at",
	} {
		if strings.Contains(help, forbidden) {
			t.Errorf("the help text claims %q, but a CLOSED sprint reports a null days_elapsed", forbidden)
		}
	}

	// The help must state the null answer for CLOSED, since that is the case a
	// caller is most likely to get wrong.
	daysLine := daysElapsedNote(help)
	if daysLine == "" {
		t.Fatal("the sprint stats help carries no note explaining days_elapsed")
	}
	if !strings.Contains(daysLine, "CLOSED") {
		t.Errorf("the days_elapsed note must name CLOSED among the statuses reporting null; got: %q", daysLine)
	}
	if !strings.Contains(daysLine, "null") {
		t.Errorf("the days_elapsed note must say the field is null outside OPEN; got: %q", daysLine)
	}
	if !strings.Contains(daysLine, "OPEN") {
		t.Errorf("the days_elapsed note must name OPEN as the status that carries a figure; got: %q", daysLine)
	}
}

// daysElapsedNote returns the "Notes for callers" bullet describing
// days_elapsed, joined into one line. The note wraps across source lines, so a
// per-line search would only ever see its first fragment.
func daysElapsedNote(help string) string {
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "- days_elapsed") {
			continue
		}
		note := []string{strings.TrimSpace(line)}
		for _, cont := range lines[i+1:] {
			trimmed := strings.TrimSpace(cont)
			if trimmed == "" || strings.HasPrefix(trimmed, "- ") {
				break
			}
			note = append(note, trimmed)
		}
		return strings.Join(note, " ")
	}
	return ""
}
