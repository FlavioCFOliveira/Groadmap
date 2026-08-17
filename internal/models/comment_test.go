package models

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The two rejection messages SPEC/COMMANDS.md pins verbatim (§ Validation Error
// Messages, § Add Task Comment, § Add Sprint Comment). They are written out here
// rather than composed from the type slices on purpose: composing them would
// reproduce whatever the code does, and the point is to hold the code against
// the text the SPEC states. The "Error: " prefix is added by the CLI's error
// printer, so it is not part of the error value.
const (
	wantTaskTypeMsgFmt   = `validation error: invalid comment type %q for a task comment; valid types: FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE`
	wantSprintTypeMsgFmt = `validation error: invalid comment type %q for a sprint comment; valid types: FINDING, DECISION, PROGRESS, UPDATE`

	wantBodyTooLargeMsg = "field exceeds maximum size: body exceeds maximum length of 4096 characters"
	wantBodyControlMsg  = "validation error: body: control characters are not allowed"
)

// mapsToExitInvalidData reports whether err maps to exit code 6
// (ExitInvalidData). It mirrors the sentinel test cmd/rmp/main.go's
// handleError performs, so a change to the sentinels a comment error chains
// shows up here as a failure instead of silently changing the exit code.
//
// It also rejects the sentinels that would win the switch ahead of exit 6:
// handleError tests ErrNotFound, ErrAlreadyExists and ErrNoRoadmap first, and
// ErrInvalidInput/ErrRequired map to exit 2.
func mapsToExitInvalidData(err error) bool {
	if err == nil {
		return false
	}
	for _, wrongClass := range []error{
		utils.ErrNotFound, utils.ErrAlreadyExists, utils.ErrNoRoadmap,
		utils.ErrInvalidInput, utils.ErrRequired,
	} {
		if errors.Is(err, wrongClass) {
			return false
		}
	}
	return errors.Is(err, utils.ErrValidation) || errors.Is(err, utils.ErrFieldTooLarge)
}

// TestCommentTypeSetsMatchSpec pins the two accepted sets and their canonical
// order (SPEC/MODELS.md § Comment Type), and pins the lookup maps against the
// slices so the two representations of the same set cannot drift apart.
func TestCommentTypeSetsMatchSpec(t *testing.T) {
	wantTask := []CommentType{"FINDING", "HYPOTHESIS", "TEST", "DECISION", "PROGRESS", "UPDATE", "NOTE"}
	wantSprint := []CommentType{"FINDING", "DECISION", "PROGRESS", "UPDATE"}

	sets := []struct {
		name   string
		got    []CommentType
		want   []CommentType
		lookup map[string]CommentType
	}{
		{"task", ValidTaskCommentTypes, wantTask, validTaskCommentTypeMap},
		{"sprint", ValidSprintCommentTypes, wantSprint, validSprintCommentTypeMap},
	}

	for _, set := range sets {
		t.Run(set.name, func(t *testing.T) {
			if len(set.got) != len(set.want) {
				t.Fatalf("%s comment accepts %d types, SPEC/MODELS.md states %d: got %v",
					set.name, len(set.got), len(set.want), set.got)
			}
			for i := range set.want {
				if set.got[i] != set.want[i] {
					t.Errorf("%s type %d is %q, SPEC/MODELS.md states %q (the order is canonical: "+
						"it is the order the rejection message and the help publish)",
						set.name, i, set.got[i], set.want[i])
				}
			}

			// The map must hold exactly the slice's values, keyed by themselves.
			if len(set.lookup) != len(set.want) {
				t.Errorf("%s lookup map holds %d entries but the accepted set has %d",
					set.name, len(set.lookup), len(set.want))
			}
			for _, commentType := range set.want {
				got, ok := set.lookup[string(commentType)]
				if !ok {
					t.Errorf("%s lookup map is missing %q", set.name, commentType)
					continue
				}
				if got != commentType {
					t.Errorf("%s lookup map maps %q to %q", set.name, commentType, got)
				}
			}
		})
	}
}

// TestCommentTypeConstantValues pins each constant's wire value. The database
// CHECK constraints and the JSON output carry these strings literally
// (SPEC/DATABASE.md, SPEC/DATA_FORMATS.md), so a renamed value is a
// data-compatibility break, not a refactor.
func TestCommentTypeConstantValues(t *testing.T) {
	constants := []struct {
		got  CommentType
		want string
	}{
		{CommentFinding, "FINDING"},
		{CommentHypothesis, "HYPOTHESIS"},
		{CommentTest, "TEST"},
		{CommentDecision, "DECISION"},
		{CommentProgress, "PROGRESS"},
		{CommentUpdate, "UPDATE"},
		{CommentNote, "NOTE"},
	}

	for _, c := range constants {
		if string(c.got) != c.want {
			t.Errorf("comment type constant is %q, SPEC/MODELS.md states %q", c.got, c.want)
		}
	}
}

// commentTypeCase is one enum value weighed against both entities. Every one of
// the seven values appears exactly once, so the table covers the whole enum on
// both entities (acceptance criterion 1).
type commentTypeCase struct {
	value      string
	taskOK     bool
	sprintOK   bool
	whySprint  string
	isEnumWide bool
}

var commentTypeCases = []commentTypeCase{
	{value: "FINDING", taskOK: true, sprintOK: true, isEnumWide: true},
	{value: "HYPOTHESIS", taskOK: true, sprintOK: false, isEnumWide: true,
		whySprint: "task-only: a hypothesis belongs to the work inside a task"},
	{value: "TEST", taskOK: true, sprintOK: false, isEnumWide: true,
		whySprint: "task-only: a test is run within the scope of a task"},
	{value: "DECISION", taskOK: true, sprintOK: true, isEnumWide: true},
	{value: "PROGRESS", taskOK: true, sprintOK: true, isEnumWide: true},
	{value: "UPDATE", taskOK: true, sprintOK: true, isEnumWide: true},
	{value: "NOTE", taskOK: true, sprintOK: false, isEnumWide: true,
		whySprint: "task-only: an incidental note belongs in the task's log"},

	// Values outside the enum entirely.
	{value: "", taskOK: false, sprintOK: false},
	{value: "BUG", taskOK: false, sprintOK: false},        // a TaskType, not a comment type
	{value: "USER_STORY", taskOK: false, sprintOK: false}, // a TaskType, not a comment type
	{value: "COMPLETED", taskOK: false, sprintOK: false},  // a TaskStatus, not a comment type
	{value: "finding", taskOK: false, sprintOK: false},    // the enum is case-sensitive
	{value: "Finding", taskOK: false, sprintOK: false},
	{value: " FINDING", taskOK: false, sprintOK: false}, // the enum is not trimmed here
	{value: "FINDING ", taskOK: false, sprintOK: false},
	{value: "FINDING,DECISION", taskOK: false, sprintOK: false}, // not a list
}

// TestIsValidCommentTypePerEntity exercises the per-entity validity check over
// every enum value and over values outside the enum.
func TestIsValidCommentTypePerEntity(t *testing.T) {
	for _, tc := range commentTypeCases {
		t.Run("task/"+tc.value, func(t *testing.T) {
			if got := IsValidTaskCommentType(tc.value); got != tc.taskOK {
				t.Errorf("IsValidTaskCommentType(%q) = %v, want %v", tc.value, got, tc.taskOK)
			}
		})
		t.Run("sprint/"+tc.value, func(t *testing.T) {
			if got := IsValidSprintCommentType(tc.value); got != tc.sprintOK {
				t.Errorf("IsValidSprintCommentType(%q) = %v, want %v (%s)",
					tc.value, got, tc.sprintOK, tc.whySprint)
			}
		})
	}
}

// TestSprintRejectsTaskOnlyCommentTypes states the feature's semantic core as
// its own assertion: the three task-only values are refused on a sprint. It is
// the rule that keeps a task's execution diary out of a sprint's progression log
// (SPEC/MODELS.md § Comment Type).
func TestSprintRejectsTaskOnlyCommentTypes(t *testing.T) {
	for _, taskOnly := range []CommentType{CommentHypothesis, CommentTest, CommentNote} {
		t.Run(string(taskOnly), func(t *testing.T) {
			if !IsValidTaskCommentType(string(taskOnly)) {
				t.Fatalf("%q must be valid on a task", taskOnly)
			}
			if IsValidSprintCommentType(string(taskOnly)) {
				t.Errorf("%q is accepted on a sprint comment; SPEC/MODELS.md § Comment Type "+
					"states the four sprint values exclude it", taskOnly)
			}

			comment := &SprintComment{Type: taskOnly, Body: "A body that is itself perfectly valid."}
			err := comment.Validate()
			if err == nil {
				t.Fatalf("SprintComment.Validate() accepted the task-only type %q", taskOnly)
			}
			// The message must offer the sprint's own set back, not the enum. The
			// rejected value itself appears earlier in the message as the quoted
			// input, so only the list after "valid types: " is examined.
			_, list, found := strings.Cut(err.Error(), "valid types: ")
			if !found {
				t.Fatalf("the rejection message names no valid set: %q", err)
			}
			if got, want := list, FormatCommentTypes(ValidSprintCommentTypes); got != want {
				t.Errorf("the sprint rejection message offers %q, want the four sprint values %q", got, want)
			}
			for _, offered := range strings.Split(list, ", ") {
				if !IsValidSprintCommentType(offered) {
					t.Errorf("the sprint rejection message offers %q, which a sprint comment does not accept", offered)
				}
			}
		})
	}
}

// TestParseCommentTypePerEntity checks the returned value on acceptance and the
// exact message, on both entities, over the whole enum.
func TestParseCommentTypePerEntity(t *testing.T) {
	for _, tc := range commentTypeCases {
		t.Run("task/"+tc.value, func(t *testing.T) {
			got, err := ParseTaskCommentType(tc.value)
			assertParsedCommentType(t, tc.value, string(got), err, tc.taskOK, wantTaskTypeMsgFmt)
		})
		t.Run("sprint/"+tc.value, func(t *testing.T) {
			got, err := ParseSprintCommentType(tc.value)
			assertParsedCommentType(t, tc.value, string(got), err, tc.sprintOK, wantSprintTypeMsgFmt)
		})
	}
}

// assertParsedCommentType holds one Parse result against the accepted/rejected
// expectation, the pinned message, and the sentinel chain.
func assertParsedCommentType(t *testing.T, input, got string, err error, wantOK bool, wantMsgFmt string) {
	t.Helper()

	if wantOK {
		if err != nil {
			t.Fatalf("parse(%q) returned %v, want the value accepted", input, err)
		}
		if got != input {
			t.Errorf("parse(%q) returned %q; the parsed value must be the input itself", input, got)
		}
		return
	}

	if err == nil {
		t.Fatalf("parse(%q) accepted a value the entity does not accept", input)
	}
	if got != "" {
		t.Errorf("parse(%q) returned the value %q alongside an error; a rejected value must return the zero value", input, got)
	}

	wantMsg := strings.Replace(wantMsgFmt, "%q", `"`+input+`"`, 1)
	if err.Error() != wantMsg {
		t.Errorf("message is not the one SPEC/COMMANDS.md pins.\n got: %q\nwant: %q", err.Error(), wantMsg)
	}
	if !errors.Is(err, ErrInvalidCommentType) {
		t.Errorf("parse(%q) error does not chain ErrInvalidCommentType: %v", input, err)
	}
	if !mapsToExitInvalidData(err) {
		t.Errorf("parse(%q) error does not map to exit code 6: %v", input, err)
	}
}

// TestParseCommentTypeMessageNamesTheEntitysOwnSet proves the messages are
// per-entity: a value valid on the other entity is rejected with the set of the
// entity being commented on, which is what SPEC/MODELS.md § Comment Type
// requires instead of a generic rejection.
func TestParseCommentTypeMessageNamesTheEntitysOwnSet(t *testing.T) {
	_, taskErr := ParseTaskCommentType("BUG")
	if got, want := taskErr.Error(), strings.Replace(wantTaskTypeMsgFmt, "%q", `"BUG"`, 1); got != want {
		t.Errorf("task message:\n got: %q\nwant: %q", got, want)
	}

	// HYPOTHESIS is valid on a task; on a sprint it must be rejected with the
	// sprint's four values, and the message must not offer it back.
	_, sprintErr := ParseSprintCommentType("HYPOTHESIS")
	if got, want := sprintErr.Error(), strings.Replace(wantSprintTypeMsgFmt, "%q", `"HYPOTHESIS"`, 1); got != want {
		t.Errorf("sprint message:\n got: %q\nwant: %q", got, want)
	}
}

// TestFormatCommentTypes pins the rendering the message, the help, and the AI
// Agent Contract share.
func TestFormatCommentTypes(t *testing.T) {
	cases := []struct {
		name  string
		types []CommentType
		want  string
	}{
		{"task set", ValidTaskCommentTypes, "FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE"},
		{"sprint set", ValidSprintCommentTypes, "FINDING, DECISION, PROGRESS, UPDATE"},
		{"single", []CommentType{CommentNote}, "NOTE"},
		{"empty", nil, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FormatCommentTypes(tc.types); got != tc.want {
				t.Errorf("FormatCommentTypes() = %q, want %q", got, tc.want)
			}
		})
	}
}

// bodyCase is one comment body weighed against every rule the body is subject
// to. wantMsg empty means the body is accepted.
type bodyCase struct {
	name       string
	body       string
	wantStored string
	wantMsg    string
	why        string
}

// bodyCases covers every branch of the body contract: emptiness, the length cap
// in both units, and the control-character rule including the three permitted
// controls (acceptance criterion 2).
func bodyCases() []bodyCase {
	return append(acceptedAndSizeBodyCases(), controlCharBodyCases()...)
}

// acceptedAndSizeBodyCases covers the accepted bodies, the emptiness rule and
// the length cap.
func acceptedAndSizeBodyCases() []bodyCase {
	const (
		wantRequiredMsg = "validation error: body is required"
		multiByte       = "ç" // 2 bytes per character
		fourByte        = "🔍" // 4 bytes per character
	)

	return []bodyCase{
		{
			name:       "ordinary body",
			body:       "The JWT middleware accepts a token whose exp claim equals the current second.",
			wantStored: "The JWT middleware accepts a token whose exp claim equals the current second.",
		},
		{
			name:       "interior line breaks are preserved",
			body:       "Measured three runs:\n- 41ms\n- 39ms\n- 40ms",
			wantStored: "Measured three runs:\n- 41ms\n- 39ms\n- 40ms",
			why:        "a comment body is expected to be multi-line",
		},
		{
			name:       "surrounding whitespace is trimmed",
			body:       "  \n\t Findings recorded after the profiling run. \n\t ",
			wantStored: "Findings recorded after the profiling run.",
		},
		{
			name:    "empty body",
			body:    "",
			wantMsg: wantRequiredMsg,
			why:     "the database accepts an empty body, so this layer is the only one that refuses it",
		},
		{
			name:    "whitespace only body",
			body:    "   \t\n  ",
			wantMsg: wantRequiredMsg,
			why:     "empty after trimming counts as absent",
		},

		// The length cap, in characters. The database enforces the same cap with
		// CHECK(length(body) <= 4096) and SQLite counts characters, so a
		// byte-based check here would reject bodies the schema accepts.
		{
			name:       "exactly 4096 ASCII characters",
			body:       strings.Repeat("a", MaxCommentBody),
			wantStored: strings.Repeat("a", MaxCommentBody),
			why:        "4096 is the cap itself, not one past it",
		},
		{
			name:    "4097 ASCII characters",
			body:    strings.Repeat("a", MaxCommentBody+1),
			wantMsg: wantBodyTooLargeMsg,
		},
		{
			name:       "exactly 4096 two-byte characters",
			body:       strings.Repeat(multiByte, MaxCommentBody),
			wantStored: strings.Repeat(multiByte, MaxCommentBody),
			why:        "8192 bytes but 4096 characters: the cap counts characters, matching the schema",
		},
		{
			name:    "4097 two-byte characters",
			body:    strings.Repeat(multiByte, MaxCommentBody+1),
			wantMsg: wantBodyTooLargeMsg,
		},
		{
			name:       "exactly 4096 four-byte characters",
			body:       strings.Repeat(fourByte, MaxCommentBody),
			wantStored: strings.Repeat(fourByte, MaxCommentBody),
			why:        "16384 bytes but 4096 characters",
		},
		{
			name:    "4097 four-byte characters",
			body:    strings.Repeat(fourByte, MaxCommentBody+1),
			wantMsg: wantBodyTooLargeMsg,
		},
		{
			name:       "the cap is measured after trimming",
			body:       "  " + strings.Repeat("a", MaxCommentBody) + "  ",
			wantStored: strings.Repeat("a", MaxCommentBody),
			why:        "trimming precedes validation, so the cap applies to the stored form",
		},

		// The three permitted control characters.
		{
			name:       "TAB is permitted",
			body:       "column\tvalue",
			wantStored: "column\tvalue",
		},
		{
			name:       "LF is permitted",
			body:       "first line\nsecond line",
			wantStored: "first line\nsecond line",
		},
		{
			name:       "CR is permitted",
			body:       "first line\rsecond line",
			wantStored: "first line\rsecond line",
		},
		{
			name:       "CRLF is permitted",
			body:       "first line\r\nsecond line",
			wantStored: "first line\r\nsecond line",
		},
		{
			name:       "legitimate Unicode is permitted",
			body:       "Medição concluída: 41ms — 日本語 ✅",
			wantStored: "Medição concluída: 41ms — 日本語 ✅",
			why:        "accents, CJK and emoji are not control characters",
		},
	}
}

// controlCharBodyCases holds one case per forbidden code point. The list is the
// one SPEC/MODELS.md § Free-Text Control-Character Constraint states.
func controlCharBodyCases() []bodyCase {
	forbidden := []struct {
		name string
		r    rune
	}{
		{"NUL", 0x00},
		{"SOH", 0x01},
		{"BEL", 0x07},
		{"VT", 0x0B},
		{"FF", 0x0C},
		{"SO", 0x0E},
		{"ESC", 0x1B},
		{"US", 0x1F},
		{"DEL", 0x7F},
		{"LRM", 0x200E},
		{"RLM", 0x200F},
		{"LRE", 0x202A},
		{"RLE", 0x202B},
		{"PDF", 0x202C},
		{"LRO", 0x202D},
		{"RLO", 0x202E},
		{"LRI", 0x2066},
		{"RLI", 0x2067},
		{"FSI", 0x2068},
		{"PDI", 0x2069},
		{"BOM", 0xFEFF},
	}
	cases := make([]bodyCase, 0, len(forbidden))
	for _, f := range forbidden {
		cases = append(cases, bodyCase{
			name:    "forbidden " + f.name,
			body:    "before" + string(f.r) + "after",
			wantMsg: wantBodyControlMsg,
			why:     "guards against CWE-150 escape injection and CVE-2021-42574 Trojan Source",
		})
	}
	return cases
}

// TestValidateCommentBody exercises the shared body contract directly.
func TestValidateCommentBody(t *testing.T) {
	for _, tc := range bodyCases() {
		t.Run(tc.name, func(t *testing.T) {
			stored, err := ValidateCommentBody(tc.body)

			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("body was rejected with %v, want it accepted (%s)", err, tc.why)
				}
				if stored != tc.wantStored {
					t.Errorf("stored form is %q, want %q", truncate(stored), truncate(tc.wantStored))
				}
				return
			}

			if err == nil {
				t.Fatalf("body was accepted, want it rejected (%s)", tc.why)
			}
			if err.Error() != tc.wantMsg {
				t.Errorf("message is not the one SPEC/COMMANDS.md pins.\n got: %q\nwant: %q", err.Error(), tc.wantMsg)
			}
			if stored != "" {
				t.Errorf("a rejected body returned the stored form %q; it must return the zero value", truncate(stored))
			}
			if !mapsToExitInvalidData(err) {
				t.Errorf("error does not map to exit code 6: %v", err)
			}
		})
	}
}

// TestCommentBodySentinels pins which sentinel each rejection branch chains, so
// a caller can tell the branches apart with errors.Is.
func TestCommentBodySentinels(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{"empty", "", ErrCommentBodyRequired},
		{"whitespace only", "  \t ", ErrCommentBodyRequired},
		{"over the cap", strings.Repeat("a", MaxCommentBody+1), utils.ErrFieldTooLarge},
		{"control character", "bad\x1bbyte", utils.ErrValidation},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateCommentBody(tc.body)
			if !errors.Is(err, tc.want) {
				t.Errorf("error %v does not chain %v", err, tc.want)
			}
			if !mapsToExitInvalidData(err) {
				t.Errorf("error does not map to exit code 6: %v", err)
			}
		})
	}
}

// TestCommentBodyControlCharsAreCheckedBeforeTrimming is a regression test for a
// concrete evasion. strings.TrimSpace strips VT (0x0B) and FF (0x0C), and both
// are forbidden control characters, so validating the trimmed form instead of
// the supplied one would silently accept a body that carries them at either end.
func TestCommentBodyControlCharsAreCheckedBeforeTrimming(t *testing.T) {
	trimmable := []struct {
		name string
		r    rune
	}{
		{"VT", 0x0B},
		{"FF", 0x0C},
	}

	for _, c := range trimmable {
		t.Run(c.name, func(t *testing.T) {
			// Confirm the premise: the character really is trimmed away.
			if NormalizeCommentBody(string(c.r)+"body") != "body" {
				t.Fatalf("%s is not stripped by trimming; this test's premise no longer holds", c.name)
			}

			for _, position := range []string{"leading", "trailing"} {
				body := "body" + string(c.r)
				if position == "leading" {
					body = string(c.r) + "body"
				}
				_, err := ValidateCommentBody(body)
				if err == nil {
					t.Errorf("a %s %s was accepted: the control-character rule was applied to the "+
						"trimmed body instead of the body as supplied", position, c.name)
					continue
				}
				if err.Error() != wantBodyControlMsg {
					t.Errorf("%s %s: message is %q, want %q", position, c.name, err.Error(), wantBodyControlMsg)
				}
			}
		})
	}
}

// TestNormalizeCommentBody pins the storable form: surrounding whitespace gone,
// interior structure intact.
func TestNormalizeCommentBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"already normalized", "A finding.", "A finding."},
		{"leading and trailing spaces", "   A finding.   ", "A finding."},
		{"leading and trailing newlines", "\n\nA finding.\n\n", "A finding."},
		{"mixed surrounding whitespace", " \t\r\n A finding. \r\n\t ", "A finding."},
		{"interior line breaks kept", "Line one.\n\nLine two.", "Line one.\n\nLine two."},
		{"interior tabs kept", "key\tvalue", "key\tvalue"},
		{"whitespace only", " \t\n ", ""},
		{"empty", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeCommentBody(tc.body); got != tc.want {
				t.Errorf("NormalizeCommentBody(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

// TestCommentValidateAcrossEveryTypeAndEntity runs both Validate methods over
// the whole enum with a valid body, so the per-entity subset is exercised
// through the method the persistence layer will call (acceptance criterion 1).
func TestCommentValidateAcrossEveryTypeAndEntity(t *testing.T) {
	const validBody = "Reproduced on the second run; the boundary second is the trigger."

	for _, tc := range commentTypeCases {
		if !tc.isEnumWide {
			continue // covered by the parse tests; this table is about the enum
		}

		t.Run("task/"+tc.value, func(t *testing.T) {
			comment := &TaskComment{Type: CommentType(tc.value), Body: validBody, TaskID: 42}
			err := comment.Validate()
			if tc.taskOK && err != nil {
				t.Errorf("TaskComment.Validate() rejected the valid type %q: %v", tc.value, err)
			}
			if !tc.taskOK && err == nil {
				t.Errorf("TaskComment.Validate() accepted the invalid type %q", tc.value)
			}
		})

		t.Run("sprint/"+tc.value, func(t *testing.T) {
			comment := &SprintComment{Type: CommentType(tc.value), Body: validBody, SprintID: 7}
			err := comment.Validate()
			if tc.sprintOK {
				if err != nil {
					t.Errorf("SprintComment.Validate() rejected the valid type %q: %v", tc.value, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("SprintComment.Validate() accepted %q, which a sprint does not accept (%s)",
					tc.value, tc.whySprint)
			}
			wantMsg := strings.Replace(wantSprintTypeMsgFmt, "%q", `"`+tc.value+`"`, 1)
			if err.Error() != wantMsg {
				t.Errorf("message:\n got: %q\nwant: %q", err.Error(), wantMsg)
			}
			if !mapsToExitInvalidData(err) {
				t.Errorf("error does not map to exit code 6: %v", err)
			}
		})
	}
}

// TestCommentValidateEnforcesTheBodyRules runs the body table through both
// Validate methods, so the entity structs and the shared validator cannot drift.
func TestCommentValidateEnforcesTheBodyRules(t *testing.T) {
	for _, tc := range bodyCases() {
		t.Run("task/"+tc.name, func(t *testing.T) {
			comment := &TaskComment{Type: CommentNote, Body: tc.body, TaskID: 42}
			assertBodyOutcome(t, comment.Validate(), tc.wantMsg, tc.why)
		})
		t.Run("sprint/"+tc.name, func(t *testing.T) {
			comment := &SprintComment{Type: CommentProgress, Body: tc.body, SprintID: 7}
			assertBodyOutcome(t, comment.Validate(), tc.wantMsg, tc.why)
		})
	}
}

// assertBodyOutcome holds one Validate result against the body expectation.
func assertBodyOutcome(t *testing.T, err error, wantMsg, why string) {
	t.Helper()

	if wantMsg == "" {
		if err != nil {
			t.Fatalf("Validate() rejected a valid body with %v (%s)", err, why)
		}
		return
	}
	if err == nil {
		t.Fatalf("Validate() accepted an invalid body (%s)", why)
	}
	if err.Error() != wantMsg {
		t.Errorf("message:\n got: %q\nwant: %q", err.Error(), wantMsg)
	}
	if !mapsToExitInvalidData(err) {
		t.Errorf("error does not map to exit code 6: %v", err)
	}
}

// TestCommentValidateChecksTypeBeforeBody pins the order SPEC/COMMANDS.md fixes.
// The command layer relies on the type being decidable on its own, before the
// body is resolved, so an invalid --type never leaves the command waiting on
// standard input for a body it would reject anyway.
func TestCommentValidateChecksTypeBeforeBody(t *testing.T) {
	// Both the type and the body are invalid: the type must be the failure
	// reported.
	taskErr := (&TaskComment{Type: "BUG", Body: ""}).Validate()
	if !errors.Is(taskErr, ErrInvalidCommentType) {
		t.Errorf("TaskComment.Validate() reported %v; the type must be checked before the body", taskErr)
	}

	sprintErr := (&SprintComment{Type: CommentNote, Body: strings.Repeat("a", MaxCommentBody+1)}).Validate()
	if !errors.Is(sprintErr, ErrInvalidCommentType) {
		t.Errorf("SprintComment.Validate() reported %v; the type must be checked before the body", sprintErr)
	}

	// And the type check is reachable without a body at all, which is what makes
	// the pinned order expressible by the command layer.
	if _, err := ParseTaskCommentType("BUG"); err == nil {
		t.Error("ParseTaskCommentType must reject an invalid type without needing a body")
	}
}

// TestCommentTypeIsMandatory pins that no comment type defaults: the zero value
// of the field is rejected on both entities (SPEC/MODELS.md § Comment Type,
// "there is no default value and no untyped comment").
func TestCommentTypeIsMandatory(t *testing.T) {
	const validBody = "A body that is itself perfectly valid."

	taskErr := (&TaskComment{Body: validBody}).Validate()
	if taskErr == nil {
		t.Error("TaskComment.Validate() accepted a comment with no type")
	}
	if want := strings.Replace(wantTaskTypeMsgFmt, "%q", `""`, 1); taskErr != nil && taskErr.Error() != want {
		t.Errorf("task message:\n got: %q\nwant: %q", taskErr.Error(), want)
	}

	sprintErr := (&SprintComment{Body: validBody}).Validate()
	if sprintErr == nil {
		t.Error("SprintComment.Validate() accepted a comment with no type")
	}
	if want := strings.Replace(wantSprintTypeMsgFmt, "%q", `""`, 1); sprintErr != nil && sprintErr.Error() != want {
		t.Errorf("sprint message:\n got: %q\nwant: %q", sprintErr.Error(), want)
	}
}

// TestCommentFieldLimits pins the constant the SPEC names.
func TestCommentFieldLimits(t *testing.T) {
	if MaxCommentBody != 4096 {
		t.Errorf("MaxCommentBody is %d; SPEC/MODELS.md and the database CHECK both state 4096", MaxCommentBody)
	}
}

// truncate keeps a failure message readable when the value under test is a
// 4096-character body.
func truncate(s string) string {
	const limit = 60
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "... (" + strconv.Itoa(len(runes)) + " characters)"
}
