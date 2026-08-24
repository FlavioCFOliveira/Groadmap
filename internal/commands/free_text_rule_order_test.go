package commands

import (
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file is the regression suite for rmp task 302: the ORDER in which the
// three rules a free-text value is subject to are applied, held against every
// command that writes one.
//
// The defect, measured against the compiled binary with a value that is at once
// over-long AND carries a BEL:
//
//	task create      --title        -> control characters are not allowed
//	sprint create    --title        -> title exceeds maximum length of 255 characters
//	sprint create    --description  -> control characters are not allowed
//	task edit        --title        -> title exceeds maximum length of 255 characters
//
// Four write paths, two verdicts, and the two fields of `sprint create`
// disagreeing with each other. `task create` and `sprint create` ran the
// control-character loop inline and left the cap to the model's Validate(),
// which runs afterwards; `sprint create` capped its title inline but not its
// description. Every other path capped first.
//
// The order is now the LENGTH cap, then the UTF-8 encoding rule, then the
// control-character rule — the order the comment body already had, because its
// bounded standard-input reader settles the length verdict without ever
// buffering the whole value and no order that judged the encoding first could
// leave that possible.
//
// # Why the exit code cannot be the assertion
//
// All three refusals exit 6. A test that asserted "it was refused" would pass on
// every ordering there is, including the one this file exists to forbid. Every
// assertion below is on the MESSAGE, and names the losing verdict when it sees
// one, so a reversal reports which rule answered instead of merely failing.

// ruleOrderWriter is one way a free-text value reaches the application: a
// command, the field it writes, and the invocation that hands it a value.
//
// It is the union of the two writer tables this package already maintains —
// alignedFreeTextWriters, which carries the twelve (command, field) pairs of the
// task and sprint commands, and commentBodyWriters, which carries the four
// comment subcommands on both of their input origins. A rule that governs every
// write path can only be proved on every write path, and those two tables
// together ARE the population: nothing else in the application writes one of the
// eight free-text fields.
type ruleOrderWriter struct {
	command string
	field   utils.Field
	invoke  func(t *testing.T, value string) error
}

// ruleOrderWriters builds that union. The comment entries are the ones that
// matter most to the sweep and the ones a flag-only table would miss: the four
// `<stdin>` origins go through models.ReadCommentBody, which decides the length
// verdict from a bounded prefix, and the SPEC gives that origin and the `--body`
// origin one verdict.
func ruleOrderWriters(roadmap string, taskCommentID, sprintCommentID int) []ruleOrderWriter {
	writers := make([]ruleOrderWriter, 0, 20)
	for _, w := range alignedFreeTextWriters(roadmap) {
		writers = append(writers, ruleOrderWriter{
			command: w.command,
			field:   w.field,
			invoke:  func(_ *testing.T, v string) error { return w.invoke(v) },
		})
	}
	for _, w := range commentBodyWriters(roadmap, taskCommentID, sprintCommentID) {
		writers = append(writers, ruleOrderWriter{
			command: w.command,
			field:   utils.FieldCommentBody,
			invoke:  w.invoke,
		})
	}
	return writers
}

// setupRuleOrderRoadmap seeds the fixture every test in this file addresses: one
// task and one sprint as id 1, and one comment on each so the `comment-edit`
// writers have something to edit.
func setupRuleOrderRoadmap(t *testing.T, name string) []ruleOrderWriter {
	t.Helper()

	setupEmptinessRoadmap(t, name)
	taskCommentID, sprintCommentID := seedCommentsForBodyProbes(t, name)

	writers := ruleOrderWriters(name, taskCommentID, sprintCommentID)
	assertRuleOrderSweepIsComplete(t, writers)
	return writers
}

// assertRuleOrderSweepIsComplete refuses a sweep that has quietly stopped
// covering the whole population. Without it a writer removed from either source
// table would make every test in this file pass on a smaller set, which is the
// failure mode a sweep of this shape actually has.
//
// The claims are the ones the task states: seven write paths, eight fields, and
// both input origins of every comment subcommand.
func assertRuleOrderSweepIsComplete(t *testing.T, writers []ruleOrderWriter) {
	t.Helper()

	// The seven write paths, as SPEC/COMMANDS.md names them, keyed by the prefix
	// each writer's own name carries.
	paths := map[string]string{
		"task create":   "task create",
		"task edit":     "task edit",
		"task stat":     "task stat --summary",
		"sprint create": "sprint create",
		"sprint update": "sprint update",
		"comment-add":   "the comment-add subcommands",
		"comment-edit":  "the comment-edit subcommands",
	}
	seenPath := make(map[string]bool, len(paths))
	seenField := make(map[utils.Field]bool, 8)
	stdinOrigins := 0

	for _, w := range writers {
		seenField[w.field] = true
		for prefix := range paths {
			if strings.Contains(w.command, prefix) {
				seenPath[prefix] = true
			}
		}
		if strings.Contains(w.command, "<stdin>") {
			stdinOrigins++
		}
	}

	for prefix, name := range paths {
		if !seenPath[prefix] {
			t.Fatalf("no writer in the sweep reaches %s; the order is not being proved on all seven write paths", name)
		}
	}
	if len(seenField) != len(fieldLimits) {
		t.Fatalf("the sweep reaches %d of the %d free-text fields; a field has fallen out of the writer tables",
			len(seenField), len(fieldLimits))
	}
	if stdinOrigins != 4 {
		t.Fatalf("the sweep carries %d standard-input origins, want 4 (one per comment subcommand); "+
			"the bounded reader is the path that most needs the cap to answer first", stdinOrigins)
	}
}

// ruleOrderProbes are values that break the length cap AND one of the two
// content rules at once. The cap is the earliest rule, so every one of them must
// be refused for its LENGTH on every write path.
//
// The control character is placed at both ends and in the middle because a
// reversal is not the only way to get the wrong verdict: an implementation that
// scanned for content violations while measuring would answer differently
// depending on where the offending code point sits, and a probe that only ever
// put it last would not see that.
func ruleOrderProbes(limit int) []struct{ name, value string } {
	over := strings.Repeat("x", limit+1)
	return []struct{ name, value string }{
		{"over the cap, trailing BEL", over + "\a"},
		{"over the cap, leading ESC", "\x1b" + over},
		{"over the cap, ESC in the middle", over[:len(over)/2] + "\x1b" + over[len(over)/2:]},
		{"over the cap, trailing VT", over + "\v"},
		{"over the cap, not valid UTF-8", over + "\xff"},
		{"over the cap, invalid UTF-8 AND a BEL", over + "\xff\a"},
		{"over the cap only once the padding is trimmed", "  " + over + "\a  "},
	}
}

// TestAnOverLongValueCarryingAControlCharacterIsRefusedForItsLength is the
// acceptance criterion, and the test a reverted path must fail by name.
//
// It says which verdict it got when it is not the expected one, so a path that
// goes back to the old order reports "refused as a CONTROL CHARACTER; the length
// cap must answer first" against its own command name rather than an opaque
// mismatch.
func TestAnOverLongValueCarryingAControlCharacterIsRefusedForItsLength(t *testing.T) {
	writers := setupRuleOrderRoadmap(t, "free-text-rule-order-length-first")

	for _, w := range writers {
		limit, ok := fieldLimits[w.field]
		if !ok {
			t.Fatalf("no maximum is declared for %s, which %s writes", w.field, w.command)
		}
		wantLength := utils.FieldTooLargeError(w.field, limit).Error()

		for _, probe := range ruleOrderProbes(limit) {
			t.Run(w.command+"/"+probe.name, func(t *testing.T) {
				err := w.invoke(t, probe.value)
				if err == nil {
					t.Fatalf("%s ACCEPTED a value of %d characters against a limit of %d that also carries a "+
						"forbidden code point", w.command, utils.FieldLength(strings.TrimSpace(probe.value)), limit)
				}
				switch err.Error() {
				case wantLength:
					return
				case utils.ControlCharError(w.field).Error():
					t.Fatalf("%s refused %q as a CONTROL CHARACTER; the length cap must answer first "+
						"(rmp task 302)\n got: %q\nwant: %q", w.command, probe.name, err.Error(), wantLength)
				case utils.InvalidUTF8Error(w.field).Error():
					t.Fatalf("%s refused %q for its ENCODING; the length cap must answer first "+
						"(rmp task 302)\n got: %q\nwant: %q", w.command, probe.name, err.Error(), wantLength)
				default:
					t.Fatalf("%s refused %q with an unexpected verdict\n got: %q\nwant: %q",
						w.command, probe.name, err.Error(), wantLength)
				}
			})
		}
	}
}

// TestEveryWritePathAgreesOnTheVerdict is the parity claim stated directly: one
// value, every write path, one answer.
//
// The test above compares each path against the SPEC; this one compares the
// paths against EACH OTHER, on the one probe whose limit-relative shape is the
// same everywhere. It is what would have reported the original defect as what it
// was — `sprint create --title` and `sprint create --description` answering
// differently — rather than as two independent failures.
func TestEveryWritePathAgreesOnTheVerdict(t *testing.T) {
	writers := setupRuleOrderRoadmap(t, "free-text-rule-order-parity")

	// The verdict is compared as a KIND rather than as a message, because the
	// message legitimately differs across fields: it names the field and its own
	// maximum.
	kind := func(err error, field utils.Field, limit int) string {
		switch {
		case err == nil:
			return "accepted"
		case err.Error() == utils.FieldTooLargeError(field, limit).Error():
			return "length"
		case err.Error() == utils.ControlCharError(field).Error():
			return "control characters"
		case err.Error() == utils.InvalidUTF8Error(field).Error():
			return "encoding"
		default:
			return "other (" + err.Error() + ")"
		}
	}

	var first, firstCommand string
	for _, w := range writers {
		limit := fieldLimits[w.field]
		got := kind(w.invoke(t, strings.Repeat("x", limit+1)+"\a"), w.field, limit)
		if first == "" {
			first, firstCommand = got, w.command
			continue
		}
		if got != first {
			t.Errorf("one value, two verdicts: %s answers %q and %s answers %q",
				firstCommand, first, w.command, got)
		}
	}
	if first != "length" {
		t.Errorf("every write path agrees on %q, but the specified verdict is %q", first, "length")
	}
}

// TestTheOrderProbeIsNonVacuous is what stops the sweep above from passing for
// the wrong reason.
//
// A probe that broke only ONE rule would be refused for that rule whatever the
// order, so the test would hold on an implementation that had stopped applying
// the other two entirely. Each half of every probe is therefore exercised alone:
// the over-long value with no forbidden code point must be refused for its
// LENGTH, and the forbidden code point inside a value well within the cap must
// be refused as a CONTROL CHARACTER. Only when both are live does the combined
// probe say anything about their ORDER.
func TestTheOrderProbeIsNonVacuous(t *testing.T) {
	writers := setupRuleOrderRoadmap(t, "free-text-rule-order-non-vacuous")

	for _, w := range writers {
		limit := fieldLimits[w.field]

		t.Run(w.command+"/length alone", func(t *testing.T) {
			err := w.invoke(t, strings.Repeat("x", limit+1))
			want := utils.FieldTooLargeError(w.field, limit).Error()
			if err == nil {
				t.Fatalf("%s accepted %d characters against a limit of %d; the cap is not applied at all",
					w.command, limit+1, limit)
			}
			if err.Error() != want {
				t.Errorf("%s\n got: %q\nwant: %q", w.command, err.Error(), want)
			}
		})

		t.Run(w.command+"/control character alone", func(t *testing.T) {
			err := w.invoke(t, "Expiry\ahardening")
			want := utils.ControlCharError(w.field).Error()
			if err == nil {
				t.Fatalf("%s accepted a BEL inside a value well within the cap; the control-character rule "+
					"is not applied at all", w.command)
			}
			if err.Error() != want {
				t.Errorf("%s\n got: %q\nwant: %q", w.command, err.Error(), want)
			}
		})

		t.Run(w.command+"/invalid encoding alone", func(t *testing.T) {
			err := w.invoke(t, "Expiry\xffhardening")
			want := utils.InvalidUTF8Error(w.field).Error()
			if err == nil {
				t.Fatalf("%s accepted a value that is not valid UTF-8 and is well within the cap; the "+
					"encoding rule is not applied at all", w.command)
			}
			if err.Error() != want {
				t.Errorf("%s\n got: %q\nwant: %q", w.command, err.Error(), want)
			}
		})
	}
}

// TestAValueAtTheLimitStillReachesTheContentRules pins the boundary the cap must
// not overreach at. A value of exactly the maximum passes the cap, so the two
// content rules are what answer for it — which is also the property that keeps
// the cap from becoming a way to skip them.
func TestAValueAtTheLimitStillReachesTheContentRules(t *testing.T) {
	writers := setupRuleOrderRoadmap(t, "free-text-rule-order-at-the-limit")

	for _, w := range writers {
		limit := fieldLimits[w.field]

		t.Run(w.command, func(t *testing.T) {
			// Exactly `limit` code points, one of which is a BEL.
			atLimit := strings.Repeat("x", limit-1) + "\a"
			if utils.FieldLength(atLimit) != limit {
				t.Fatalf("the probe is %d code points, want %d", utils.FieldLength(atLimit), limit)
			}

			err := w.invoke(t, atLimit)
			want := utils.ControlCharError(w.field).Error()
			if err == nil {
				t.Fatalf("%s accepted a value at the limit carrying a BEL", w.command)
			}
			if err.Error() == utils.FieldTooLargeError(w.field, limit).Error() {
				t.Fatalf("%s refused a value of exactly %d characters for its length; the cap is off by one\n"+
					" got: %q", w.command, limit, err.Error())
			}
			if err.Error() != want {
				t.Errorf("%s\n got: %q\nwant: %q", w.command, err.Error(), want)
			}
		})
	}
}
