package commands

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// fuzzCanonicalTimestamp is the exact shape SPEC/DATA_FORMATS.md § Dates - ISO
// 8601 with UTC publishes: YYYY-MM-DDTHH:mm:ss.sssZ.
var fuzzCanonicalTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// fuzzDateOnly is the short form the date-range filters accept alongside a full
// timestamp (SPEC/COMMANDS.md § List Tasks, `--created-since` / `--created-until`:
// "RFC3339 or YYYY-MM-DD").
const fuzzDateOnly = "2006-01-02"

// FuzzParseDateFilter drives the one entry point every date-range filter flag is
// parsed through (`task list --created-since/--created-until`, `audit list` and
// `audit stats` `--since/--until`) with arbitrary values.
//
// Invariants (SPEC/COMMANDS.md § List Tasks and § Audit error tables;
// SPEC/DATA_FORMATS.md § Dates - ISO 8601 with UTC):
//
//  1. A refused value carries ErrValidation (exit code 6) and ErrInvalidDateFormat,
//     and prints the published line `validation error: <flag>: invalid date
//     format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01):
//     "X"`.
//  2. An accepted value is in UTC and was written in one of the two published
//     forms; a date-only value denotes the first instant of that day in UTC.
//  3. An accepted value round-trips: its canonical rendering has the published
//     shape YYYY-MM-DDTHH:mm:ss.sssZ, is itself accepted, and denotes the same
//     instant to the millisecond.
//  4. A well-formed YYYY-MM-DD calendar date is never refused.
func FuzzParseDateFilter(f *testing.F) {
	for _, seed := range []string{
		"2026-03-20", "2026-03-20T00:00:00.000Z", "2026-03-20T00:00:00Z",
		"2026-03-20T00:00:00.000000Z", "2026-03-20T00:00:00+00:00", "2026-03-20T01:00:00+01:00",
		"2026-03-20T14:35:09.512Z", "1970-01-01T00:00:00.000Z", "9999-12-31T23:59:59.999Z",
		"not-a-date", "24/05/2026", "2026-13-01", "2026-02-30", "", "14:35:09", "2026",
		"2026-03", "2026-03-20 or thereabouts", "1969-12-31T23:59:59Z", "2026-03-20T24:00:00Z", "2026-03-20t00:00:00z",
		"2026-03-20T00:00:00-23:59", "2026-03-20T00:00:00.9999999999Z", "\xff2026-03-20",
	} {
		f.Add(seed)
	}

	const flag = "--created-since"

	f.Fuzz(func(t *testing.T, value string) {
		got, err := ParseDateFilter(flag, value)

		_, dateOnlyErr := time.Parse(fuzzDateOnly, value)
		if dateOnlyErr == nil && err != nil {
			t.Fatalf("ParseDateFilter(%q) refused a well-formed YYYY-MM-DD date: %v", value, err)
		}

		if err != nil {
			if !errors.Is(err, utils.ErrValidation) || !errors.Is(err, ErrInvalidDateFormat) {
				t.Fatalf("ParseDateFilter(%q) = %v, want ErrValidation (exit code 6) and ErrInvalidDateFormat",
					value, err)
			}
			want := fmt.Sprintf("validation error: %s: invalid date format: expected RFC3339 "+
				"(2026-01-01T00:00:00Z) or date-only (2026-01-01): %q", flag, value)
			if err.Error() != want {
				t.Fatalf("ParseDateFilter(%q) printed %q, want the published line %q", value, err.Error(), want)
			}
			if !utf8.ValidString(err.Error()) {
				t.Fatalf("ParseDateFilter(%q): the refusal %q is not valid UTF-8", value, err.Error())
			}
			return
		}

		if got.Location() != time.UTC {
			t.Fatalf("ParseDateFilter(%q) = %v in %v, want UTC", value, got, got.Location())
		}

		if dateOnlyErr == nil {
			day, _ := time.Parse(fuzzDateOnly, value)
			if !got.Equal(day) || got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 || got.Nanosecond() != 0 {
				t.Fatalf("ParseDateFilter(%q) = %v, want the first instant of that day in UTC", value, got)
			}
		} else {
			parsed, rfcErr := time.Parse(time.RFC3339Nano, value)
			if rfcErr != nil {
				t.Fatalf("ParseDateFilter(%q) accepted a value that is neither RFC3339 nor YYYY-MM-DD", value)
			}
			if !parsed.Equal(got) {
				t.Fatalf("ParseDateFilter(%q) = %v, want the instant the value denotes, %v", value, got, parsed)
			}
		}

		canonical := utils.FormatISO8601(got)
		if !fuzzCanonicalTimestamp.MatchString(canonical) {
			t.Fatalf("ParseDateFilter(%q) = %v, whose canonical rendering %q is not YYYY-MM-DDTHH:mm:ss.sssZ",
				value, got, canonical)
		}
		again, againErr := ParseDateFilter(flag, canonical)
		if againErr != nil {
			t.Fatalf("ParseDateFilter(%q) accepted the value, but refuses its own canonical rendering %q: %v",
				value, canonical, againErr)
		}
		if !again.Equal(got.Truncate(time.Millisecond)) {
			t.Fatalf("ParseDateFilter(%q) = %v; its canonical rendering %q reads back as %v, a different instant",
				value, got, canonical, again)
		}
	})
}
