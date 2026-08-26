package commands

import (
	"errors"
	"fmt"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// dateOnlyLayout is the bare calendar date the filter flags accept alongside a
// full timestamp. A value in this form names a day, not an instant, so it is
// read as the first instant of that day in UTC.
const dateOnlyLayout = "2006-01-02"

// ErrInvalidDateFormat indicates that a date string does not match any accepted format.
var ErrInvalidDateFormat = errors.New("invalid date format: expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01)")

// ParseDateFilter parses the value written to a date-range filter flag and
// produces the refusal the CLI prints when the value is not a date.
//
// It is the single entry point for every date-range filter the CLI publishes —
// `task list --created-since/--created-until` and `audit list`/`audit stats`
// `--since/--until` — so the one flag type the machine-readable contract
// declares has exactly one acceptance rule behind it. Two parsers that merely
// agreed today is what let `audit` drift to a narrower rule than the SPEC, the
// contract and README.md all publish for it.
//
// Two forms are accepted:
//
//	RFC3339     2026-01-01T00:00:00Z, and the variants utils.ParseISO8601 takes
//	            (millisecond precision, sub-second precision, +00:00 offset)
//	date-only   2026-01-01, read as 2026-01-01T00:00:00.000Z
//
// The returned time is always UTC. On failure the error wraps utils.ErrValidation,
// so the caller lands on exit code 6, and ErrInvalidDateFormat, so a caller that
// wants to distinguish the condition can test for it; the flag is named in the
// message because a command carrying both a lower and an upper bound must say
// which one it refused.
func ParseDateFilter(flag, value string) (time.Time, error) {
	// The full-timestamp form is tried first: it is the canonical form, and it
	// is the only one that carries a time of day.
	if t, err := utils.ParseISO8601(value); err == nil {
		return t, nil
	}
	t, err := time.Parse(dateOnlyLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %s: %w: %q", utils.ErrValidation, flag, ErrInvalidDateFormat, value)
	}
	return t.UTC(), nil
}
