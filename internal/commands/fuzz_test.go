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
// timestamp (SPEC/DATA_FORMATS.md § Date Filter Values, form 2).
const fuzzDateOnly = "2006-01-02"

// fuzzDateFilterTimestamp is the timestamp form of SPEC/DATA_FORMATS.md § Date
// Filter Values, transcribed from the SPEC's own description rather than taken
// from the code it judges: YYYY-MM-DDTHH:mm:ss, an optional full stop and one or
// more digits, then Z or a sign and hh:mm, with upper-case T and Z.
var fuzzDateFilterTimestamp = regexp.MustCompile(
	`^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2}):(\d{2})(\.\d+)?(Z|[+-](\d{2}):(\d{2}))$`)

// The published range, judged on the UTC instant: 1970-01-01 through 9999-12-31.
var (
	fuzzDateFilterEarliest = time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)
	fuzzDateFilterBeyond   = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
)

// fuzzDateFilterOracle decides, from the SPEC's wording alone, whether value is
// accepted and which instant it denotes.
func fuzzDateFilterOracle(value string) (time.Time, bool) {
	var instant time.Time
	if day, err := time.Parse(fuzzDateOnly, value); err == nil && len(value) == len(fuzzDateOnly) {
		instant = day
	} else if m := fuzzDateFilterTimestamp.FindStringSubmatch(value); m != nil {
		day, err := time.Parse(fuzzDateOnly, m[1])
		if err != nil {
			return time.Time{}, false
		}
		hh, mm, ss := atoiFuzz(m[2]), atoiFuzz(m[3]), atoiFuzz(m[4])
		if hh > 23 || mm > 59 || ss > 59 {
			return time.Time{}, false
		}
		var nanos int
		if m[5] != "" {
			digits := (m[5][1:] + "000000000")[:9]
			nanos = atoiFuzz(digits)
		}
		instant = day.Add(time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute +
			time.Duration(ss)*time.Second + time.Duration(nanos))
		if m[6] != "Z" {
			oh, om := atoiFuzz(m[7]), atoiFuzz(m[8])
			if oh > 23 || om > 59 {
				return time.Time{}, false
			}
			offset := time.Duration(oh)*time.Hour + time.Duration(om)*time.Minute
			if m[6][0] == '+' {
				instant = instant.Add(-offset)
			} else {
				instant = instant.Add(offset)
			}
		}
	} else {
		return time.Time{}, false
	}
	if instant.Before(fuzzDateFilterEarliest) || !instant.Before(fuzzDateFilterBeyond) {
		return time.Time{}, false
	}
	return instant, true
}

// atoiFuzz converts a run of ASCII digits the patterns above matched.
func atoiFuzz(digits string) int {
	n := 0
	for _, c := range digits {
		n = n*10 + int(c-'0')
	}
	return n
}

// FuzzParseDateFilter drives the one entry point every date-range filter flag is
// parsed through (`task list --created-since/--created-until`, `audit list` and
// `audit stats` `--since/--until`) with arbitrary values.
//
// Invariants (SPEC/DATA_FORMATS.md § Date Filter Values; SPEC/COMMANDS.md § List
// Tasks and § Audit error tables):
//
//  1. A refused value carries ErrValidation (exit code 6) and ErrInvalidDateFormat,
//     prints the published line `validation error: <flag>: invalid date format:
//     expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): "X"`,
//     and returns the zero time, which therefore never reaches a statement.
//  2. The verdict is the published one: a value is accepted exactly when it is
//     in one of the two forms and denotes an instant from 1970-01-01 through
//     9999-12-31 in UTC, and an accepted value is that instant, in UTC; a date
//     denotes the first instant of that day.
//  3. An accepted value round-trips: its canonical rendering has the published
//     shape YYYY-MM-DDTHH:mm:ss.sssZ, is itself accepted, and denotes the same
//     instant to the millisecond.
func FuzzParseDateFilter(f *testing.F) {
	for _, seed := range []string{
		"2026-03-20", "2026-03-20T00:00:00.000Z", "2026-03-20T00:00:00Z",
		"2026-03-20T00:00:00.000000Z", "2026-03-20T00:00:00+00:00", "2026-03-20T01:00:00+01:00",
		"2026-03-20T14:35:09.512Z", "1970-01-01T00:00:00.000Z", "9999-12-31T23:59:59.999Z",
		"not-a-date", "24/05/2026", "2026-13-01", "2026-02-30", "", "14:35:09", "2026",
		"2026-03", "2026-03-20 or thereabouts", "1969-12-31T23:59:59Z", "2026-03-20T24:00:00Z", "2026-03-20t00:00:00z",
		"2026-03-20T00:00:00-23:59", "2026-03-20T00:00:00.9999999999Z", "\xff2026-03-20",
		"1970-01-01T00:30:00+01:00", "9999-12-31T23:00:00-02:00", "9999-12-31", "1969-12-31",
		"2026-03-20T00:00:60Z", "2026-03-20T00:00:00,5Z", "2026-03-20T00:00:00.Z", "2026-03-20T00:00Z",
		"2026-03-20T00:00:00+0000", "2026-03-20T00:00:00+24:00", "2026-03-20T00:00:00+00:60",
		" 2026-03-20", "2026-03-20 ", "2026-03-20 00:00:00Z", "0001-01-01", "0000-01-01",
	} {
		f.Add(seed)
	}

	const flag = "--created-since"

	f.Fuzz(func(t *testing.T, value string) {
		got, err := ParseDateFilter(flag, value)
		want, accepted := fuzzDateFilterOracle(value)

		if err != nil {
			if accepted {
				t.Fatalf("ParseDateFilter(%q) refused a value the SPEC accepts (the instant %v): %v", value, want, err)
			}
			if !errors.Is(err, utils.ErrValidation) || !errors.Is(err, ErrInvalidDateFormat) {
				t.Fatalf("ParseDateFilter(%q) = %v, want ErrValidation (exit code 6) and ErrInvalidDateFormat",
					value, err)
			}
			wantLine := fmt.Sprintf("validation error: %s: invalid date format: expected RFC3339 "+
				"(2026-01-01T00:00:00Z) or date-only (2026-01-01): %q", flag, value)
			if err.Error() != wantLine {
				t.Fatalf("ParseDateFilter(%q) printed %q, want the published line %q", value, err.Error(), wantLine)
			}
			if !utf8.ValidString(err.Error()) {
				t.Fatalf("ParseDateFilter(%q): the refusal %q is not valid UTF-8", value, err.Error())
			}
			if !got.IsZero() {
				t.Fatalf("ParseDateFilter(%q) refused the value but returned the time %v", value, got)
			}
			return
		}

		if !accepted {
			t.Fatalf("ParseDateFilter(%q) = %v, accepting a value the SPEC refuses", value, got)
		}
		if got.Location() != time.UTC {
			t.Fatalf("ParseDateFilter(%q) = %v in %v, want UTC", value, got, got.Location())
		}
		if !got.Equal(want) {
			t.Fatalf("ParseDateFilter(%q) = %v, want the instant the value denotes, %v", value, got, want)
		}
		if got.IsZero() {
			t.Fatalf("ParseDateFilter(%q) accepted the value as the zero time", value)
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
