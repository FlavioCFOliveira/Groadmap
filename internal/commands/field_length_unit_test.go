package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file is the behavioural gate for the unit every free-text length cap
// measures in, and it is the regression test for rmp task 296.
//
// The defect, reproduced against the compiled binary:
//
//	rmp task create --title <102 CJK characters> ...
//	Error: field exceeds maximum size: title exceeds maximum length of 255 characters
//
// The title was 102 characters. The message named a limit of 255 CHARACTERS
// while the cap measured 306 BYTES, so every non-ASCII field was refused at
// roughly a third of its documented maximum — and the comment body, alone among
// the eight, was not, because it already counted characters. One codebase, two
// units, and which one applied depended on the field.
//
// It sweeps the SAME table of eight fields and their writers that the published
// name gate and the UTF-8 gate use (fieldWriterCases), rather than a list of its
// own: a rule that governs the whole set is only proved on the whole set, and a
// field or a command added to the SPEC table has one place to be added here.
// The probes come from internal/testenv for the same reason.
//
// An ASCII-only suite cannot see this defect at all — for ASCII the byte count
// and the character count are the same number — which is exactly why the Go
// suite stayed green while the binary refused a 102-character title. Every case
// below therefore runs in four scripts of one, two, three and four bytes per
// code point.

// fieldLimits is the maximum each of the eight free-text fields accepts, in
// characters. It is keyed by utils.Field so it can be held against
// fieldWriterCases, which is the reachability oracle for the whole set:
// TestEveryFreeTextFieldCapIsMeasuredInCharacters fails if the two ever disagree
// about which fields exist.
var fieldLimits = map[utils.Field]int{
	utils.FieldTaskTitle:                  models.MaxTaskTitle,
	utils.FieldTaskFunctionalRequirements: models.MaxTaskFunctionalRequirements,
	utils.FieldTaskTechnicalRequirements:  models.MaxTaskTechnicalRequirements,
	utils.FieldTaskAcceptanceCriteria:     models.MaxTaskAcceptanceCriteria,
	utils.FieldTaskCompletionSummary:      models.MaxTaskCompletionSummary,
	utils.FieldSprintTitle:                models.MaxSprintTitle,
	utils.FieldSprintDescription:          models.MaxSprintDescription,
	utils.FieldCommentBody:                models.MaxCommentBody,
}

// TestEveryFreeTextFieldCapIsMeasuredInCharacters is the acceptance criterion:
// a value AT the limit and a value ONE OVER it, in four scripts, for each of the
// eight fields, from every command that writes one.
//
// The two halves make different claims, and neither would be enough alone.
//
// At the limit the claim is narrow on purpose: the value was not refused for its
// LENGTH. It cannot be "the command succeeded", because two writers legitimately
// decline for reasons of their own — `task stat COMPLETED --summary` needs a
// task already in TESTING and a --commit-close, and `sprint create` needs a
// title as well as a description — and asserting success would be asserting
// something about the state machine rather than about this rule. Refusal for
// length is precisely what the defect produced, so that is what is refused here.
// The storage round-trip is covered end to end by tests/test_16_boundary_unicode.py.
//
// One over, the claim is total: the exact published message, built from the
// shared definition rather than spelled here, so a reworded refusal cannot make
// this pass by no longer matching.
func TestEveryFreeTextFieldCapIsMeasuredInCharacters(t *testing.T) {
	for _, script := range testenv.LengthProbeScripts() {
		t.Run(script.Name, func(t *testing.T) {
			roadmap := "field-length-" + strings.ToLower(strings.ReplaceAll(script.Name, " ", "-"))
			_, taskCommentID, sprintCommentID := setupPublishedNameRoadmap(t, roadmap)

			cases := fieldWriterCases(roadmap, taskCommentID, sprintCommentID)
			if len(cases) != len(fieldLimits) {
				t.Fatalf("the sweep covers %d fields and %d limits are declared; one of the two is missing a field",
					len(cases), len(fieldLimits))
			}

			for _, tc := range cases {
				limit, ok := fieldLimits[tc.field]
				if !ok {
					t.Fatalf("no maximum is declared for the field %q", tc.field)
				}
				assertProbeIsSound(t, script, limit)

				for _, w := range tc.writers {
					t.Run(tc.field.String()+"/"+w.command, func(t *testing.T) {
						assertAcceptedAtTheLimit(t, w, tc.field, limit, script)
						assertRefusedOneOver(t, w, tc.field, limit, script)
					})
				}
			}
		})
	}
}

// assertProbeIsSound checks the two properties that make the case non-vacuous
// before it is used: the probe is exactly `limit` code points, and — outside
// ASCII — its byte count differs from its character count, so a byte-counting
// cap would actually behave differently on it.
func assertProbeIsSound(t *testing.T, script testenv.LengthProbeScript, limit int) {
	t.Helper()

	at := script.Repeat(limit)
	if got := utils.FieldLength(at); got != limit {
		t.Fatalf("the %s probe is %d code points, want %d", script.Name, got, limit)
	}
	if want := limit * script.BytesPerRune; len(at) != want {
		t.Fatalf("the %s probe is %d bytes, want %d", script.Name, len(at), want)
	}
	if script.BytesPerRune > 1 && len(at) == utils.FieldLength(at) {
		t.Fatalf("the %s probe counts the same in bytes and characters; it cannot tell the two units apart",
			script.Name)
	}
}

func assertAcceptedAtTheLimit(t *testing.T, w fieldWriter, field utils.Field, limit int, script testenv.LengthProbeScript) {
	t.Helper()

	value := script.Repeat(limit)

	var err error
	_ = captureStdout(t, func() { err = w.invoke(value) })
	if err == nil {
		return // accepted outright: the strongest outcome
	}
	if refusal := utils.FieldTooLargeError(field, limit); strings.Contains(err.Error(), refusal.Error()) {
		t.Errorf("%s refused a value of exactly %d %s characters (%d bytes), which is the documented maximum:\n  %v",
			w.command, limit, script.Name, len(value), err)
	}
}

func assertRefusedOneOver(t *testing.T, w fieldWriter, field utils.Field, limit int, script testenv.LengthProbeScript) {
	t.Helper()

	value := script.Repeat(limit + 1)

	var err error
	_ = captureStdout(t, func() { err = w.invoke(value) })
	if err == nil {
		t.Fatalf("%s accepted %d %s characters, one over the maximum of %d",
			w.command, limit+1, script.Name, limit)
	}
	want := utils.FieldTooLargeError(field, limit).Error()
	if !strings.Contains(err.Error(), want) {
		t.Errorf("%s refused %d %s characters with the wrong message.\n got: %s\nwant: %s",
			w.command, limit+1, script.Name, err.Error(), want)
	}
	if !utils.IsFieldTooLarge(err) {
		t.Errorf("%s refused %d %s characters outside the field-too-large class, so the exit code is wrong: %v",
			w.command, limit+1, script.Name, err)
	}
}

// TestATitleOf255CJKCharactersIsAcceptedAndOneOf256IsRefused is the defect's own
// boundary, stated as the report stated it and carried all the way to storage.
//
// The two halves are what separate a cap that counts characters from one that
// counts bytes: 255 CJK characters occupy 765 bytes, so a byte-counting cap
// refuses the first, and 256 characters occupy 768, so it refuses the second for
// the wrong reason and with a number no reader can act on.
//
// The stored value is read back and compared byte for byte, which also proves
// the CHECK(length(title) <= 255) constraint on the column accepted 765 bytes —
// the schema half of the same agreement, measured directly in
// internal/db/field_length_check_test.go.
func TestATitleOf255CJKCharactersIsAcceptedAndOneOf256IsRefused(t *testing.T) {
	const roadmap = "field-length-cjk-title-boundary"
	database := setupCommentRoadmap(t, roadmap)

	cjk := cjkProbe(t)

	at := cjk.Repeat(models.MaxTaskTitle)
	if len(at) != 3*models.MaxTaskTitle {
		t.Fatalf("the probe is %d bytes, want %d; a byte-counting cap would not be caught by it",
			len(at), 3*models.MaxTaskTitle)
	}

	var out string
	out = captureStdout(t, func() {
		if err := taskCreate(taskCreateArgs(roadmap, utils.FieldTaskTitle, at)); err != nil {
			t.Fatalf("a title of %d CJK characters (%d bytes) was refused: %v", models.MaxTaskTitle, len(at), err)
		}
	})

	stored, err := database.GetTask(context.Background(), extractIntID(t, out))
	if err != nil {
		t.Fatalf("reading back the created task: %v", err)
	}
	if stored.Title != at {
		t.Errorf("the stored title is not the one supplied:\n got %d characters\nwant %d characters",
			utils.FieldLength(stored.Title), models.MaxTaskTitle)
	}

	over := cjk.Repeat(models.MaxTaskTitle + 1)
	err = taskCreate(taskCreateArgs(roadmap, utils.FieldTaskTitle, over))
	if err == nil {
		t.Fatalf("a title of %d CJK characters was accepted", models.MaxTaskTitle+1)
	}
	want := utils.FieldTooLargeError(utils.FieldTaskTitle, models.MaxTaskTitle).Error()
	if !strings.Contains(err.Error(), want) {
		t.Errorf("refusal = %q, want it to carry %q", err.Error(), want)
	}
}

// TestTheRefusalNamesTheUnitItMeasured is acceptance criterion 3, stated as a
// relation rather than as a copy of the wording: the number the message prints
// is a number of characters, so a value of exactly that many characters must be
// accepted, whatever the script.
//
// A message that named bytes while counting bytes would have been self
// consistent too. What makes "characters" the right word is that
// SPEC/MODELS.md § Task Field Constraints and the CHECK constraint on the column
// both say characters; the wording therefore did not change when the defect was
// fixed, it became true.
func TestTheRefusalNamesTheUnitItMeasured(t *testing.T) {
	const roadmap = "field-length-refusal-unit"
	setupCommentRoadmap(t, roadmap)

	for _, script := range testenv.LengthProbeScripts() {
		t.Run(script.Name, func(t *testing.T) {
			err := taskCreate(taskCreateArgs(roadmap, utils.FieldTaskTitle, script.Repeat(models.MaxTaskTitle+1)))
			if err == nil {
				t.Fatal("an oversize title was accepted")
			}

			limit := numberInRefusal(t, err.Error())
			accepted := script.Repeat(limit)
			if got := utils.FieldLength(accepted); got != limit {
				t.Fatalf("the probe is %d code points, want %d", got, limit)
			}
			_ = captureStdout(t, func() {
				if createErr := taskCreate(taskCreateArgs(roadmap, utils.FieldTaskTitle, accepted)); createErr != nil {
					t.Errorf("the refusal names a maximum of %d CHARACTERS, but a title of exactly %d %s "+
						"characters (%d bytes) is refused: %v",
						limit, limit, script.Name, len(accepted), createErr)
				}
			})
		})
	}
}

// numberInRefusal pulls the maximum out of a too-large refusal. The message is
// "<field> exceeds maximum length of N characters", so the number is the token
// between the wording and the unit — and the unit is required to be the word
// "characters", which is the half of criterion 3 a number alone would not pin.
func numberInRefusal(t *testing.T, message string) int {
	t.Helper()

	const marker = "exceeds maximum length of "
	cut := strings.Index(message, marker)
	if cut < 0 {
		t.Fatalf("the refusal is not a too-large refusal: %q", message)
	}
	rest := strings.Fields(message[cut+len(marker):])
	if len(rest) < 2 {
		t.Fatalf("the refusal names no maximum and no unit: %q", message)
	}
	if rest[1] != "characters" {
		t.Fatalf("the refusal names the unit %q, but the cap measures characters: %q", rest[1], message)
	}
	limit := 0
	for _, r := range rest[0] {
		if r < '0' || r > '9' {
			t.Fatalf("the maximum in the refusal is not a number: %q", message)
		}
		limit = limit*10 + int(r-'0')
	}
	return limit
}

// cjkProbe returns the three-bytes-per-code-point script from the shared corpus,
// failing rather than silently testing something else if the corpus changes.
func cjkProbe(t *testing.T) testenv.LengthProbeScript {
	t.Helper()

	for _, script := range testenv.LengthProbeScripts() {
		if script.BytesPerRune == 3 {
			return script
		}
	}
	t.Fatal("the shared corpus carries no three-byte script; the CJK boundary cannot be probed")
	return testenv.LengthProbeScript{}
}
