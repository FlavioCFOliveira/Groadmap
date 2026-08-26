package utils

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// TestValidateIDString_OverflowExitClass is a regression gate for finding #58:
// an int64-overflowing all-digits ID must map to ErrValidation (exit 6), the
// same class as an in-range value that exceeds the valid ID maximum — NOT
// ErrInvalidInput (exit 2), which is reserved for genuine syntax errors.
func TestValidateIDString_OverflowExitClass(t *testing.T) {
	_, err := ValidateIDString("999999999999999999999", FieldTaskID)
	if err == nil {
		t.Fatal("expected error for overflowing ID, got nil")
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("overflow ID must wrap ErrValidation (exit 6), got: %v", err)
	}
	if errors.Is(err, ErrInvalidInput) {
		t.Errorf("overflow ID must NOT be classified as ErrInvalidInput (exit 2): %v", err)
	}

	// A genuine syntax error (non-digit) stays ErrInvalidInput (exit 2).
	_, err = ValidateIDString("12abc", FieldTaskID)
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("non-numeric ID must remain ErrInvalidInput (exit 2), got: %v", err)
	}
}

// TestValidateRoadmapName_SpecVerbatimMessages is a regression gate for finding
// #60: the roadmap-name validation messages must match SPEC/COMMANDS.md verbatim
// (no sentinel prefix) while still wrapping ErrValidation (exit 6).
func TestValidateRoadmapName_SpecVerbatimMessages(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"empty", "", "Roadmap name is required"},
		{"too long", strings.Repeat("a", MaxRoadmapNameLength+1),
			"Roadmap name must not exceed 50 characters (got 51)"},
		{"invalid chars", "Bad_UPPER",
			"Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRoadmapName(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if err.Error() != tc.wantMsg {
				t.Errorf("message = %q, want SPEC-verbatim %q", err.Error(), tc.wantMsg)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("must wrap ErrValidation (exit 6): %v", err)
			}
		})
	}
}

func TestValidateID(t *testing.T) {
	tests := []struct {
		name    string
		id      int
		field   RangedField
		wantErr bool
		wantMsg string
	}{
		// Valid IDs
		{"valid positive ID", 1, FieldTaskID, false, ""},
		{"valid ID 42", 42, FieldTaskID, false, ""},
		{"valid ID 1000", 1000, FieldTaskID, false, ""},
		{"valid ID MaxInt32", MaxInt32, FieldTaskID, false, ""},
		{"valid sprint ID", 5, FieldSprintID, false, ""},

		// Out of range at the floor. The message states the RULE and not the
		// bound that was crossed, which is the whole of what rmp task 330 fixed
		// here: "must be positive" named neither bound and left a caller refused
		// at the floor knowing nothing about the ceiling.
		{"zero ID", 0, FieldTaskID, true,
			"validation error: task_id must be between 1 and 2147483647, got 0"},
		{"negative ID -1", -1, FieldTaskID, true,
			"validation error: task_id must be between 1 and 2147483647, got -1"},
		{"negative ID -100", -100, FieldTaskID, true,
			"validation error: task_id must be between 1 and 2147483647, got -100"},

		// Out of range at the ceiling — the SAME sentence, which is the point.
		{"overflow ID", MaxInt32 + 1, FieldTaskID, true,
			"validation error: task_id must be between 1 and 2147483647, got 2147483648"},
		{"large overflow", MaxInt32 + 1000000, FieldTaskID, true,
			"validation error: task_id must be between 1 and 2147483647, got 2148483647"},

		// The field is named by the message, and each field names itself.
		{"zero sprint ID", 0, FieldSprintID, true,
			"validation error: sprint_id must be between 1 and 2147483647, got 0"},
		{"zero entity ID", 0, FieldEntityID, true,
			"validation error: entity_id must be between 1 and 2147483647, got 0"},
		{"zero comment ID", 0, FieldCommentID, true,
			"validation error: comment_id must be between 1 and 2147483647, got 0"},
		{"zero dependency task ID", 0, FieldDependencyTaskID, true,
			"validation error: dependency_task_id must be between 1 and 2147483647, got 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateID(tt.id, tt.field)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateID(%d, %s) error = %v, wantErr %v", tt.id, tt.field, err, tt.wantErr)
				return
			}
			if err == nil {
				return
			}
			if got := err.Error(); got != tt.wantMsg {
				t.Errorf("ValidateID(%d, %s)\n got: %q\nwant: %q", tt.id, tt.field, got, tt.wantMsg)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("ValidateID(%d, %s) must chain ErrValidation (exit 6), got %v", tt.id, tt.field, err)
			}
		})
	}
}

// TestValidateIDStatesOneSentenceAtEitherBound is the regression gate for the
// second half of rmp task 330: below the range and above it are the SAME rule,
// and one rule states itself once.
//
// The assertion is deliberately blind to what the sentence says. It takes the
// refusal at the floor and the refusal at the ceiling, replaces the offending
// value in each, and requires what is left to be identical — so a future edit
// that reintroduces a bound-specific wording fails here without this test having
// to be told what the new wording would be.
func TestValidateIDStatesOneSentenceAtEitherBound(t *testing.T) {
	for _, field := range declaredRangedFields {
		if field.IDEntity() == "" {
			continue // not an id; the rule does not govern it
		}
		t.Run(field.String(), func(t *testing.T) {
			below := ValidateID(MinID-1, field)
			above := ValidateID(MaxInt32+1, field)
			if below == nil || above == nil {
				t.Fatalf("both bounds must refuse: below=%v above=%v", below, above)
			}

			strip := func(err error, got string) string {
				return strings.Replace(err.Error(), got, "<value>", 1)
			}
			lo := strip(below, strconv.Itoa(MinID-1))
			hi := strip(above, strconv.Itoa(MaxInt32+1))
			if lo != hi {
				t.Errorf("one rule states itself in two sentences\n floor: %q\nceiling: %q", lo, hi)
			}
		})
	}
}

// TestIDRefusalsAgreeAcrossEveryFieldAndClass proves the convergence rmp task 330
// delivered: every id field, and both failure classes, produce ONE sentence about
// the rule, differing only in the subject and in the sentinel that carries the
// exit code.
//
// The comparison masks the field name and the offending value, so the test
// asserts the agreement without restating the sentence — drift on punctuation,
// on the echo, or on the bounds fails it, and a deliberate rewording of the one
// message fails it nowhere.
func TestIDRefusalsAgreeAcrossEveryFieldAndClass(t *testing.T) {
	normalise := func(err error, class error, field RangedField, got string) string {
		msg := strings.TrimPrefix(err.Error(), class.Error()+": ")
		msg = strings.Replace(msg, field.String(), "<field>", 1)
		return strings.Replace(msg, got, "<value>", 1)
	}

	var shapes []string
	for _, field := range declaredRangedFields {
		if field.IDEntity() == "" {
			continue
		}
		// The exit-6 class every surface but the comment subcommands publishes.
		shapes = append(shapes, normalise(ValidateID(0, field), ErrValidation, field, "0"))

		// The exit-2 class the four comment subcommands publish for the same
		// condition. The CLASS differs by published contract; the SENTENCE must
		// not.
		_, err := ValidateIDStringAs("0", field, ErrInvalidInput)
		if err == nil {
			t.Fatalf("%s: an out-of-range id must be refused", field)
		}
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: the supplied range class must survive, got %v", field, err)
		}
		shapes = append(shapes, normalise(err, ErrInvalidInput, field, "0"))

		// And the token that never reaches a comparison because it overflows the
		// platform's int: the same rule, the same sentence, the token echoed.
		_, err = ValidateIDString("999999999999999999999", field)
		if err == nil {
			t.Fatalf("%s: an int-overflowing id must be refused", field)
		}
		shapes = append(shapes, normalise(err, ErrValidation, field, "999999999999999999999"))
	}

	if len(shapes) < 12 {
		t.Fatalf("only %d refusals were compared; the sweep is not reaching the id fields", len(shapes))
	}
	for i, shape := range shapes {
		if shape != shapes[0] {
			t.Errorf("refusal %d words the id range rule differently\n got: %q\nwant: %q", i, shape, shapes[0])
		}
	}
}

func TestValidateIDList(t *testing.T) {
	tests := []struct {
		name    string
		ids     []int
		field   RangedField
		wantErr bool
	}{
		// Valid lists
		{"empty list", []int{}, FieldTaskID, false},
		{"single valid ID", []int{1}, FieldTaskID, false},
		{"multiple valid IDs", []int{1, 2, 3}, FieldTaskID, false},
		{"duplicate IDs", []int{1, 1, 2}, FieldTaskID, false},
		{"large valid ID", []int{MaxInt32}, FieldTaskID, false},

		// Invalid - contains zero
		{"list with zero", []int{1, 0, 3}, FieldTaskID, true},

		// Invalid - contains negative
		{"list with negative", []int{1, -1, 3}, FieldTaskID, true},

		// Invalid - contains overflow
		{"list with overflow", []int{1, MaxInt32 + 1}, FieldTaskID, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIDList(tt.ids, tt.field)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIDList(%v, %s) error = %v, wantErr %v", tt.ids, tt.field, err, tt.wantErr)
			}
		})
	}
}

func TestValidateIDString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		field   RangedField
		wantID  int
		wantErr bool
		wantMsg string
	}{
		// Valid strings
		{"valid ID 1", "1", FieldTaskID, 1, false, ""},
		{"valid ID 42", "42", FieldTaskID, 42, false, ""},
		{"valid ID with spaces", "  100  ", FieldTaskID, 100, false, ""},
		{"valid sprint ID", "5", FieldSprintID, 5, false, ""},

		// The FORMAT rule: a token that is not an integer at all. It is a
		// different rule from the range and keeps its own wording and its own
		// class (exit 2), which rmp task 330 deliberately left standing.
		{"invalid text", "abc", FieldTaskID, 0, true,
			`invalid input: invalid task ID: "abc" (must be a positive integer)`},
		{"mixed text", "12abc", FieldTaskID, 0, true,
			`invalid input: invalid task ID: "12abc" (must be a positive integer)`},
		{"empty string", "", FieldTaskID, 0, true,
			`invalid input: invalid task ID: "" (must be a positive integer)`},
		{"non-integer sprint id", "seven", FieldSprintID, 0, true,
			`invalid input: invalid sprint ID: "seven" (must be a positive integer)`},

		// The RANGE rule, at both bounds and through both overflow paths: one
		// sentence.
		{"zero", "0", FieldTaskID, 0, true,
			"validation error: task_id must be between 1 and 2147483647, got 0"},
		{"negative", "-1", FieldTaskID, 0, true,
			"validation error: task_id must be between 1 and 2147483647, got -1"},
		{"negative large", "-999", FieldTaskID, 0, true,
			"validation error: task_id must be between 1 and 2147483647, got -999"},
		{"overflow", "99999999999999999", FieldTaskID, 0, true,
			"validation error: task_id must be between 1 and 2147483647, got 99999999999999999"},
		// Regression for finding #58: a value too large for the platform's int
		// never reaches the comparison, so the TOKEN is echoed. It is still the
		// same rule, the same class and the same sentence as the row above.
		{"int64 overflow", "999999999999999999999", FieldTaskID, 0, true,
			"validation error: task_id must be between 1 and 2147483647, got 999999999999999999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, err := ValidateIDString(tt.input, tt.field)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIDString(%q, %s) error = %v, wantErr %v", tt.input, tt.field, err, tt.wantErr)
				return
			}
			if err == nil {
				if gotID != tt.wantID {
					t.Errorf("ValidateIDString(%q, %s) = %d, want %d", tt.input, tt.field, gotID, tt.wantID)
				}
				return
			}
			if got := err.Error(); got != tt.wantMsg {
				t.Errorf("ValidateIDString(%q, %s)\n got: %q\nwant: %q", tt.input, tt.field, got, tt.wantMsg)
			}
		})
	}
}

// TestValidateNoControlChars_AcceptsLegitimateText is a regression gate for the
// Free-Text Control-Character Constraint (SPEC/MODELS.md): legitimate Unicode —
// accents, emoji, CJK — and the three permitted whitespace controls (TAB, LF,
// CR) must be accepted unchanged (findings #82, #83).
func TestValidateNoControlChars_AcceptsLegitimateText(t *testing.T) {
	accepted := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"ascii", "Refactor the auth module"},
		{"accents", "Implementação de migração à base"},
		{"emoji", "Ship the release 🚀 today"},
		{"cjk", "ユーザー認証を実装する 实现用户认证"},
		{"tab", "column1\tcolumn2"},
		{"newline", "line one\nline two"},
		{"carriage-return", "line one\r\nline two"},
		{"mixed-permitted-whitespace", "a\tb\nc\rd"},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateNoControlChars(tc.value, FieldTaskTitle); err != nil {
				t.Errorf("ValidateNoControlChars(%q) = %v, want nil", tc.value, err)
			}
		})
	}
}

// TestValidateNoControlChars_RejectsControlAndBidi is a regression gate that the
// forbidden control / DEL / bidi-format code points are rejected with an
// ErrValidation-wrapped error (exit 6) (SPEC/MODELS.md; findings #82, #83).
func TestValidateNoControlChars_RejectsControlAndBidi(t *testing.T) {
	rejected := []struct {
		name  string
		value string
	}{
		{"NUL", "before\x00after"},
		{"ESC", "before\x1bafter"},
		{"BEL", "alert\x07here"},
		{"unit-separator", "a\x1fb"},
		{"DEL", "before\x7fafter"},
		{"LRM-U+200E", "text\u200emore"},
		{"RLM-U+200F", "text\u200fmore"},
		{"LRE-U+202A", "text\u202amore"},
		{"RLO-U+202E", "Trojan\u202eSource"},
		{"LRI-U+2066", "text\u2066more"},
		{"PDI-U+2069", "text\u2069more"},
		{"BOM-U+FEFF", "text\ufeffmore"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNoControlChars(tc.value, FieldTaskTitle)
			if err == nil {
				t.Fatalf("ValidateNoControlChars(%q) = nil, want error", tc.value)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("ValidateNoControlChars(%q) error = %v, want wrapped ErrValidation", tc.value, err)
			}
			if !strings.Contains(err.Error(), "title") {
				t.Errorf("error %q should name the field", err.Error())
			}
		})
	}
}
