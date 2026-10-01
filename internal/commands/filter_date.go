package commands

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ErrInvalidDateFormat indicates that a date string does not match any accepted format.
var ErrInvalidDateFormat = errors.New("invalid date format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01)")

// dateFilterTimestamp is the timestamp form of SPEC/DATA_FORMATS.md § Date Filter
// Values: the date-time of RFC 3339 (section 5.6) with an upper-case T and Z, an
// optional fraction of a second that is a full stop followed by one or more
// digits, and a Z or ±hh:mm zone designator, with nothing before or after it.
// The pattern fixes the shape; the numeric bounds of each field are checked in
// dateFilterInstant, because a regular expression is the wrong tool for "a day
// the month has in that year". \d is ASCII-only in RE2, so no other script's
// digits pass.
var dateFilterTimestamp = regexp.MustCompile(
	`^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(?:(Z)|([+-])(\d{2}):(\d{2}))$`)

// dateFilterDate is the calendar-date form: YYYY-MM-DD and nothing else.
var dateFilterDate = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)

// The range every accepted value MUST denote an instant inside, judged on the
// instant in UTC after the zone designator has been applied: from the first
// instant of 1970-01-01 up to, and excluding, the first instant of 10000-01-01,
// which includes the whole of 9999-12-31 (SPEC/DATA_FORMATS.md § Date Filter
// Values).
var (
	dateFilterEarliest = time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)
	dateFilterBeyond   = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
)

// ParseDateFilter parses the value written to a date-range filter flag and
// produces the refusal the CLI prints when the value is not accepted.
//
// It is the single entry point for every date-range filter the CLI publishes —
// `task list --created-since/--created-until` and `audit list`/`audit stats`
// `--since/--until` — so the six flags have exactly one acceptance rule behind
// them, the one SPEC/DATA_FORMATS.md § Date Filter Values is canonical for.
//
// Two forms are accepted:
//
//	timestamp   YYYY-MM-DDTHH:mm:ss, an optional fraction of a second, and a Z
//	            or ±hh:mm zone designator; the instant is the date and time less
//	            the offset
//	date        YYYY-MM-DD, read as YYYY-MM-DDT00:00:00.000Z on a lower bound
//	            and on an upper bound alike
//
// Either form MUST denote an instant from 1970-01-01 through 9999-12-31 in UTC.
// Every other value is refused — a lower-case t or z, a leap second, a comma
// fraction, an offset without its colon, a date that does not exist, leading
// or trailing whitespace, the empty value — and so is a well-formed value
// outside the range, with the same line, because the range is part of what the
// flag accepts. A refused value therefore never yields a time at all: the zero
// time.Time a caller could otherwise carry into a statement is never returned
// with a nil error.
//
// The returned time is always UTC. On failure the error wraps utils.ErrValidation,
// so the caller lands on exit code 6, and ErrInvalidDateFormat, so a caller that
// wants to distinguish the condition can test for it; the flag is named in the
// message because a command carrying both a lower and an upper bound must say
// which one it refused.
func ParseDateFilter(flag, value string) (time.Time, error) {
	t, ok := dateFilterInstant(value)
	if !ok || t.Before(dateFilterEarliest) || !t.Before(dateFilterBeyond) {
		return time.Time{}, fmt.Errorf("%w: %s: %w: %q", utils.ErrValidation, flag, ErrInvalidDateFormat, value)
	}
	return t, nil
}

// dateFilterInstant reads value in one of the two accepted forms and returns
// the UTC instant it denotes, or false when value is in neither form. It does
// not judge the range.
func dateFilterInstant(value string) (time.Time, bool) {
	if m := dateFilterDate.FindStringSubmatch(value); m != nil {
		return calendarInstant(m[1], m[2], m[3], "00", "00", "00", "")
	}

	m := dateFilterTimestamp.FindStringSubmatch(value)
	if m == nil {
		return time.Time{}, false
	}
	t, ok := calendarInstant(m[1], m[2], m[3], m[4], m[5], m[6], m[7])
	if !ok {
		return time.Time{}, false
	}
	if m[8] == "Z" {
		return t, true
	}

	offHours, offMinutes := atoiDigits(m[10]), atoiDigits(m[11])
	if offHours > 23 || offMinutes > 59 {
		return time.Time{}, false
	}
	offset := time.Duration(offHours)*time.Hour + time.Duration(offMinutes)*time.Minute
	// The instant is the date and time LESS the offset: +01:00 is one hour
	// ahead of UTC, so its instant is one hour earlier.
	if m[9] == "+" {
		return t.Add(-offset), true
	}
	return t.Add(offset), true
}

// calendarInstant builds the UTC instant of the given fields, all ASCII digit
// strings the patterns above matched, and reports false when a field is out of
// its range: a month outside 01-12, a day the month does not have in that year,
// an hour outside 00-23, a minute or second outside 00-59 (no leap second).
// fraction is the digits after the full stop, possibly empty; digits beyond the
// ninth are below the nanosecond and do not change the instant.
func calendarInstant(year, month, day, hour, minute, second, fraction string) (time.Time, bool) {
	y, mo, d := atoiDigits(year), atoiDigits(month), atoiDigits(day)
	h, mi, s := atoiDigits(hour), atoiDigits(minute), atoiDigits(second)
	if mo < 1 || mo > 12 || d < 1 || h > 23 || mi > 59 || s > 59 {
		return time.Time{}, false
	}

	nanos := 0
	for i := range 9 {
		nanos *= 10
		if i < len(fraction) {
			nanos += int(fraction[i] - '0')
		}
	}

	t := time.Date(y, time.Month(mo), d, h, mi, s, nanos, time.UTC)
	// time.Date normalises an impossible day (February 30th becomes March 1st
	// or 2nd); a date that did not survive unchanged does not exist.
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return time.Time{}, false
	}
	return t, true
}

// atoiDigits converts a string of at most four ASCII digits, already matched by
// one of the patterns above, to its value.
func atoiDigits(digits string) int {
	n, err := strconv.Atoi(digits)
	if err != nil {
		// Unreachable: the patterns admit ASCII digits only, at most four of them.
		return -1
	}
	return n
}
