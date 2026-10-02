package commands

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The gates of rmp tasks 569 and 570: the one acceptance rule of
// SPEC/DATA_FORMATS.md § Date Filter Values — a strict RFC 3339 timestamp with
// an upper-case T and Z, no leap second and a full-stop fraction, or a bare
// calendar date, denoting an instant from 1970-01-01 through 9999-12-31 in UTC —
// and its refusal, exit code 6 with one line, before the roadmap is opened, so
// that no refused value, the zero time among them, ever reaches a statement.

// TestParseDateFilter_ThePublishedExamples drives every value the section names,
// in both directions.
func TestParseDateFilter_ThePublishedExamples(t *testing.T) {
	sameInstant := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	accepted := []struct {
		value string
		want  time.Time
	}{
		{"2026-01-01T01:00:00+01:00", sameInstant},
		{"2026-01-01T00:00:00+00:00", sameInstant},
		{"2026-01-01T00:00:00-00:00", sameInstant},
		{"2026-01-01T00:00:00Z", sameInstant},
		{"2026-01-01", sameInstant},
		{"2026-01-01T00:00:00.000Z", sameInstant},
		{"1970-01-01", time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{"1970-01-01T00:00:00.000Z", time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{"9999-12-31", time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)},
		{"9999-12-31T23:59:59.999Z", time.Date(9999, time.December, 31, 23, 59, 59, 999000000, time.UTC)},
		{"2026-03-20T14:35:09.5-03:30", time.Date(2026, time.March, 20, 18, 5, 9, 500000000, time.UTC)},
		{"2024-02-29", time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range accepted {
		got, err := ParseDateFilter("--since", tc.value)
		if err != nil || !got.Equal(tc.want) || got.Location() != time.UTC {
			t.Errorf("ParseDateFilter(%q) = (%v, %v), want %v in UTC", tc.value, got, err, tc.want)
		}
	}

	for _, value := range []string{
		"1969-12-31", "1970-01-01T00:30:00+01:00", "9999-12-31T23:00:00-02:00", "0001-01-01", "0000-01-01",
		"2026-01-01t00:00:00Z", "2026-01-01T00:00:00z", "2026-01-01 00:00:00Z",
		"2026-01-01T0:00:00Z", "2026-01-01T00:0:00Z", "2026-01-01T00:00:0Z",
		"2026-01-01T00:00:00,5Z", "2026-01-01T00:00:00.Z", "2026-01-01T00:00Z",
		"2026-01-01T00:00:00+0000", "2026-01-01T00:00:00+24:00", "2026-01-01T00:00:00+00:60",
		"2026-02-30", "2023-02-29", "12026-01-01", "926-01-01", " 2026-01-01", "2026-01-01 ", "",
		"2026-01-01T00:00:60Z", "2026-01-01T24:00:00Z", "1970-10-01T0:00:00,+00Z",
	} {
		got, err := ParseDateFilter("--since", value)
		want := fmt.Sprintf("validation error: --since: invalid date format: expected RFC3339 "+
			"(2026-01-01T00:00:00Z) or date-only (2026-01-01): %q", value)
		if err == nil || err.Error() != want || !errors.Is(err, utils.ErrValidation) || !got.IsZero() {
			t.Errorf("ParseDateFilter(%q) = (%v, %v), want the zero time and %q", value, got, err, want)
		}
	}
}

// TestDateFilterFlags_RefuseBeforeTheRoadmapIsOpened drives the six flags with
// values outside the range and one outside the grammar, against a roadmap that
// does not exist: the refusal is exit code 6, not the roadmap's exit code 4,
// so the value was refused before anything was opened or queried.
func TestDateFilterFlags_RefuseBeforeTheRoadmapIsOpened(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const absent = "no-such-roadmap"

	flags := []struct {
		family, sub, flag string
	}{
		{"task", "list", "--created-since"},
		{"task", "list", "--created-until"},
		{"audit", "list", "--since"},
		{"audit", "list", "--until"},
		{"audit", "stats", "--since"},
		{"audit", "stats", "--until"},
	}
	for _, f := range flags {
		for _, value := range []string{"0001-01-01", "1969-12-31", "9999-12-31T23:00:00-02:00", "2026-01-01t00:00:00Z"} {
			t.Run(f.family+"_"+f.sub+"_"+f.flag+"_"+value, func(t *testing.T) {
				_, err := dispatchInvocation(t, f.family, f.sub, "-r", absent, f.flag, value)
				assertPublishedRefusal(t, f.family+" "+f.sub+" "+f.flag, err, utils.ErrValidation, 6,
					fmt.Sprintf("validation error: %s: invalid date format: expected RFC3339 "+
						"(2026-01-01T00:00:00Z) or date-only (2026-01-01): %q", f.flag, value))
			})
		}
	}
}
