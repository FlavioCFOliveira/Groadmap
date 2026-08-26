package commands

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file is the behavioural gate for the `--limit` range rule.
//
// The defect it exists against was reproduced against the compiled binary. One
// rule, one failure class, one exit code, three commands, three-ish sentences:
//
//	rmp task list    --limit 0  ->  Error: validation error: limit must be between 1 and 100
//	rmp backlog list --limit 0  ->  Error: validation error: limit must be between 1 and 100
//	rmp audit list   --limit 0  ->  Error: validation error: --limit must be between 1 and 500 (got 0)
//
// They differed on three axes and only ONE of them was design: the maximum, 100
// against 500, is a real difference between a task listing and the audit log.
// The other two were not. The flag was named with its `--` prefix on one command
// and without it on the other two, and the value that caused the refusal was
// echoed on one and dropped on the other two (rmp task 329).
//
// The structural half of the fix is gated in internal/utils: the sentence is
// spelled once in utils.NumericRangeMessage, the ", got N" assembly once in
// utils.NumericRangeError, and published_field_names_test.go fails a second
// spelling introduced anywhere else. This file gates what a user actually reads,
// which is the thing the structural gate cannot state: that the three lines
// differ in the maximum and in nothing else.

// limitRefusal is one command that publishes a `--limit`, with the ceiling it
// publishes.
type limitRefusal struct {
	// run performs the listing against the roadmap named by the argument, with
	// the given --limit.
	run func(roadmap, limit string) error
	// command is the invocation as a user would type it, for failure messages.
	command string
	// ceiling is the maximum this command accepts. It is the ONE thing the
	// three refusals may disagree about.
	ceiling int
}

// limitRefusalCases is every command whose `--limit` is bounded. All three call
// their list function directly, at the level the divergent literals used to live
// at; tests/test_55_error_string_parity.py drives the same three through the
// compiled binary and compares against the strings SPEC/COMMANDS.md publishes.
func limitRefusalCases() []limitRefusal {
	return []limitRefusal{
		{
			command: "rmp task list",
			ceiling: models.MaxTaskLimit,
			run: func(r, limit string) error {
				return taskList([]string{"-r", r, "--limit", limit})
			},
		},
		{
			command: "rmp backlog list",
			ceiling: models.MaxTaskLimit,
			run: func(r, limit string) error {
				return backlogList([]string{"-r", r, "--limit", limit})
			},
		},
		{
			command: "rmp audit list",
			ceiling: models.MaxAuditLimit,
			run: func(r, limit string) error {
				return auditList([]string{"-r", r, "--limit", limit})
			},
		},
	}
}

// TestLimitRefusalsDifferOnlyInTheMaximum is the assertion the defect failed.
//
// Every command is given the SAME offending value, one below the floor that all
// three share, so nothing but the ceiling can legitimately differ between the
// three lines. Each line then has its own ceiling replaced by a placeholder, and
// the results must be equal.
//
// Stated that way the test is not a copy of the messages: it does not care what
// the sentence says, only that the three commands say the same one. A `--`
// prefix on one of them, a `, got N` missing from another, a parenthesised value
// on a third — each makes two normalised lines differ and fails here, which is
// precisely the class of drift that produced the defect.
func TestLimitRefusalsDifferOnlyInTheMaximum(t *testing.T) {
	roadmap := "limit-range-refusal-parity"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	const belowFloor = "0"
	const ceilingMark = "<ceiling>"

	normalised := make(map[string][]string, 2)
	for _, tc := range limitRefusalCases() {
		err := tc.run(roadmap, belowFloor)
		if err == nil {
			t.Fatalf("%s --limit %s: want a refusal, got nil", tc.command, belowFloor)
		}
		line := err.Error()

		ceiling := strconv.Itoa(tc.ceiling)
		if !strings.Contains(line, ceiling) {
			t.Errorf("%s: the refusal does not state its own maximum %s\n line: %q", tc.command, ceiling, line)
			continue
		}
		key := strings.Replace(line, ceiling, ceilingMark, 1)
		normalised[key] = append(normalised[key], tc.command)
	}

	if len(normalised) != 1 {
		t.Errorf("one rule, %d sentences. With the maximum masked, every command must produce the same line:",
			len(normalised))
		for line, commands := range normalised {
			t.Errorf("  %q\n    from: %s", line, strings.Join(commands, ", "))
		}
	}
}

// TestLimitRefusalsCarryTheAgreedForm pins the two axes that were wrong, in the
// direction rmp task 318 settled them for `priority` and `severity`: the value
// is named WITHOUT the flag's `--` prefix, and the offending value IS echoed.
//
// TestLimitRefusalsDifferOnlyInTheMaximum above would be satisfied by three
// commands agreeing on the WRONG form. This is what says which form is right,
// and it says it by comparing against the shared definition rather than against
// a literal copied out of it: utils.NumericRangeMessage is the same builder that
// words `priority must be between 0 and 9`, so the two rules cannot drift apart
// either.
func TestLimitRefusalsCarryTheAgreedForm(t *testing.T) {
	roadmap := "limit-range-refusal-form"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	for _, tc := range limitRefusalCases() {
		t.Run(tc.command, func(t *testing.T) {
			for _, offending := range []int{models.MinListLimit - 1, tc.ceiling + 1} {
				value := strconv.Itoa(offending)
				err := tc.run(roadmap, value)
				if err == nil {
					t.Fatalf("--limit %s: want a refusal, got nil", value)
				}

				want := "validation error: " +
					utils.NumericRangeMessage(utils.FieldListLimit, models.MinListLimit, tc.ceiling) +
					", got " + value
				if got := err.Error(); got != want {
					t.Errorf("--limit %s: rendered line differs\n got: %q\nwant: %q", value, got, want)
				}
				if strings.Contains(err.Error(), "--limit") {
					t.Errorf("--limit %s: the refusal names the FLAG; the range rule names the VALUE, "+
						"as it does for priority and severity (rmp task 318)\n line: %q", value, err.Error())
				}
				if !errors.Is(err, utils.ErrValidation) {
					t.Errorf("--limit %s: the refusal must wrap utils.ErrValidation so it maps to exit 6; got %v",
						value, err)
				}
			}
		})
	}
}

// TestLimitBoundsAcceptTheirEndpoints keeps the two tests above from passing on
// a command that refuses everything. Both endpoints of every range are accepted,
// which is what makes the bounds INCLUSIVE rather than merely stated.
func TestLimitBoundsAcceptTheirEndpoints(t *testing.T) {
	roadmap := "limit-range-refusal-endpoints"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	for _, tc := range limitRefusalCases() {
		t.Run(tc.command, func(t *testing.T) {
			for _, accepted := range []int{models.MinListLimit, tc.ceiling} {
				value := strconv.Itoa(accepted)
				if err := tc.run(roadmap, value); err != nil {
					t.Errorf("--limit %s is inside the published range and must be accepted; got %v", value, err)
				}
			}
		})
	}
}

// TestLimitSentinelsIdentifyTheirCeiling pins the one thing the shared wording
// must not cost: a caller can still tell WHICH cap was exceeded. The two rules
// are worded by one builder but owned by two sentinels, so errors.Is
// discriminates them exactly as it discriminates priority from severity.
func TestLimitSentinelsIdentifyTheirCeiling(t *testing.T) {
	taskErr := models.ValidateTaskLimit(models.MaxTaskLimit + 1)
	auditErr := models.ValidateAuditLimit(models.MaxAuditLimit + 1)
	if taskErr == nil || auditErr == nil {
		t.Fatal("both validators must refuse a value above their ceiling")
	}

	if !errors.Is(taskErr, models.ErrTaskLimitOutOfRange) {
		t.Error("the task-listing refusal must carry ErrTaskLimitOutOfRange")
	}
	if errors.Is(taskErr, models.ErrAuditLimitOutOfRange) {
		t.Error("the task-listing refusal must not answer to the audit sentinel")
	}
	if !errors.Is(auditErr, models.ErrAuditLimitOutOfRange) {
		t.Error("the audit refusal must carry ErrAuditLimitOutOfRange")
	}
	if errors.Is(auditErr, models.ErrTaskLimitOutOfRange) {
		t.Error("the audit refusal must not answer to the task-listing sentinel")
	}
}
