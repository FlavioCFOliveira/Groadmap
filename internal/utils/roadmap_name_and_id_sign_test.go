package utils

import (
	"errors"
	"strings"
	"testing"
)

// The gates of rmp tasks 567 and 568.
//
// 567 (SPEC/COMMANDS.md § Roadmap Name Validation): the length rule counts
// characters, never bytes, an invalid byte counting as one; and the rules are
// applied in a fixed order — empty, length, leading hyphen, reserved, character
// set — the first rule a name breaks deciding the refusal.
//
// 568 (SPEC/COMMANDS.md § Entity Identifier Range, "What an integer is"): a sign
// with no digit after it, a plus sign and an empty token are not integers, so
// they take the format line with exit code 2 and never reach the range rule.

const (
	lengthLineOf51 = "Roadmap name must not exceed 50 characters (got 51)"
	charsetLine    = "Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens"
)

// TestValidateRoadmapName_LengthCountsCharacters pins the unit of the length
// rule on names whose byte length and character count differ.
func TestValidateRoadmapName_LengthCountsCharacters(t *testing.T) {
	cases := []struct {
		label, name, want string
		sentinel          error
	}{
		{"50 two-byte characters (100 bytes) are within the length rule", strings.Repeat("é", 50), charsetLine, ErrInvalidRoadmapName},
		{"51 two-byte characters exceed it", strings.Repeat("é", 51), lengthLineOf51, ErrRoadmapNameTooLong},
		{"50 invalid bytes count as 50 characters", strings.Repeat("\xff", 50), charsetLine, ErrInvalidRoadmapName},
		{"51 invalid bytes count as 51 characters", strings.Repeat("\xff", 51), lengthLineOf51, ErrRoadmapNameTooLong},
		{"49 ASCII characters and one three-byte character", strings.Repeat("a", 49) + "€", charsetLine, ErrInvalidRoadmapName},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateRoadmapName(tc.name)
			if err == nil || err.Error() != tc.want || !errors.Is(err, tc.sentinel) || !errors.Is(err, ErrValidation) {
				t.Errorf("ValidateRoadmapName(%d bytes) = %v, want %q (%v, exit code 6)", len(tc.name), err, tc.want, tc.sentinel)
			}
		})
	}
	if err := ValidateRoadmapName(strings.Repeat("a", 50)); err != nil {
		t.Errorf("a 50-character name was refused: %v", err)
	}
}

// TestValidateRoadmapName_TheFirstBrokenRuleDecides pins the order on names that
// break several rules at once.
func TestValidateRoadmapName_TheFirstBrokenRuleDecides(t *testing.T) {
	cases := []struct {
		label, name, want string
	}{
		{"empty", "", "Roadmap name is required"},
		{"length before the character set: 51 characters holding a space", strings.Repeat("a", 25) + " " + strings.Repeat("b", 25), lengthLineOf51},
		{"length before the leading hyphen", "-" + strings.Repeat("a", 50), lengthLineOf51},
		{"leading hyphen before the character set", "-Bad Name", "validation error: roadmap name cannot start with '-'"},
		{"reserved before the character set", "con.txt", `validation error: "con.txt": roadmap name is a reserved system name`},
		{"the character set last", "Bad Name!", charsetLine},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			if err := ValidateRoadmapName(tc.name); err == nil || err.Error() != tc.want {
				t.Errorf("ValidateRoadmapName(%q) = %v, want %q", tc.name, err, tc.want)
			}
		})
	}
}

// TestIDParsers_ASignWithoutDigitsIsTheFormatRule pins 568 on the list parser
// every batch command uses and on the single-id parser, in both its range
// classes: a lone "-", a lone "+", a plus-signed number and an empty token all
// take the format line, and a real negative number still takes the range line.
func TestIDParsers_ASignWithoutDigitsIsTheFormatRule(t *testing.T) {
	format := func(token string) string {
		return `invalid input: invalid task ID: "` + token + `" (must be a positive integer)`
	}
	for _, tc := range []struct{ list, token string }{
		{"-", "-"},
		{" - ", "-"},
		{"1,-", "-"},
		{"-,1", "-"},
		{"+", "+"},
		{"+5", "+5"},
		{"1,,2", ""},
		{"1,", ""},
	} {
		ids, err := ParseCommaSeparatedIDs(tc.list, FieldTaskID)
		if err == nil || err.Error() != format(tc.token) || !errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrValidation) {
			t.Errorf("ParseCommaSeparatedIDs(%q) = (%v, %v), want the format line %q (exit code 2)",
				tc.list, ids, err, format(tc.token))
		}
	}

	if _, err := ParseCommaSeparatedIDs("1,-1", FieldTaskID); err == nil ||
		err.Error() != "validation error: task_id must be between 1 and 2147483647, got -1" {
		t.Errorf(`ParseCommaSeparatedIDs("1,-1") = %v, want the range line: -1 is an integer`, err)
	}

	// The comment subcommands publish exit code 2 for their range rule too, and
	// still the format line for a sign without digits.
	_, err := ValidateIDStringAs("-", FieldCommentID, ErrInvalidInput)
	if err == nil || err.Error() != `invalid input: invalid comment ID: "-" (must be a positive integer)` {
		t.Errorf(`ValidateIDStringAs("-", comment) = %v, want the comment format line`, err)
	}
	_, err = ValidateIDString("-", FieldSprintID)
	if err == nil || err.Error() != `invalid input: invalid sprint ID: "-" (must be a positive integer)` {
		t.Errorf(`ValidateIDString("-", sprint) = %v, want the sprint format line`, err)
	}
}
