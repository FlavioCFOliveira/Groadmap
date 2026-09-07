package utils

import (
	"errors"
	"log/slog"
	"testing"
	"time"
)

// TestParseISO8601_DateRange tests date range validation in ParseISO8601
func TestParseISO8601_DateRange(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errType error
	}{
		{
			name:    "valid date at epoch",
			input:   "1970-01-01T00:00:00.000Z",
			wantErr: false,
		},
		{
			name:    "valid date at max",
			input:   "9999-12-31T23:59:59.999Z",
			wantErr: false,
		},
		{
			name:    "date before 1970",
			input:   "1969-12-31T23:59:59.999Z",
			wantErr: true,
			errType: ErrDateTooEarly,
		},
		{
			name:    "date after 9999 - fails at parsing",
			input:   "10000-01-01T00:00:00.000Z",
			wantErr: true,
			// Note: Go's time.Parse doesn't support years > 9999, so this fails at parsing
			// before our validation can run. This is acceptable behavior.
		},
		{
			name:    "year 0001",
			input:   "0001-01-01T00:00:00.000Z",
			wantErr: true,
			errType: ErrDateTooEarly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseISO8601(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseISO8601() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errType != nil && !errors.Is(err, tt.errType) {
				t.Errorf("ParseISO8601() error type = %v, want %v", err, tt.errType)
			}
			if !tt.wantErr && got.IsZero() {
				t.Error("ParseISO8601() returned zero time for valid input")
			}
		})
	}
}

func TestValidateDateRange(t *testing.T) {
	tests := []struct {
		name    string
		input   time.Time
		wantErr bool
		errType error
	}{
		{
			name:    "epoch date",
			input:   time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "current date",
			input:   time.Now().UTC(),
			wantErr: false,
		},
		{
			name:    "max valid date",
			input:   MaxValidDate,
			wantErr: false,
		},
		{
			name:    "before epoch",
			input:   time.Date(1969, 12, 31, 23, 59, 59, 0, time.UTC),
			wantErr: true,
			errType: ErrDateTooEarly,
		},
		{
			name:    "after max",
			input:   time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: true,
			errType: ErrDateTooLate,
		},
		{
			name:    "year 1900",
			input:   time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: true,
			errType: ErrDateTooEarly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDateRange(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDateRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.errType != nil && !errors.Is(err, tt.errType) {
				t.Errorf("ValidateDateRange() error type = %v, want %v", err, tt.errType)
			}
		})
	}
}

func TestValidateNotFuture(t *testing.T) {
	tests := []struct {
		name    string
		input   time.Time
		wantErr bool
	}{
		{
			name:    "past date",
			input:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "current time",
			input:   time.Now().UTC(),
			wantErr: false,
		},
		{
			name:    "30 seconds in future (within tolerance)",
			input:   time.Now().UTC().Add(30 * time.Second),
			wantErr: false,
		},
		{
			name:    "2 minutes in future (beyond tolerance)",
			input:   time.Now().UTC().Add(2 * time.Minute),
			wantErr: true,
		},
		{
			name:    "far future",
			input:   time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNotFuture(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNotFuture() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDateOrder(t *testing.T) {
	tests := []struct {
		name    string
		start   time.Time
		end     time.Time
		wantErr bool
	}{
		{
			name:    "valid order",
			start:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "same time",
			start:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "end before start",
			start:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			end:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: true,
		},
		{
			name:    "one second before",
			start:   time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
			end:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDateOrder(tt.start, tt.end)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDateOrder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDateRangeConstants validates the MinValidDate and MaxValidDate constants
func TestDateRangeConstants(t *testing.T) {
	// Verify MinValidDate is 1970-01-01
	expectedMin := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	if !MinValidDate.Equal(expectedMin) {
		t.Errorf("MinValidDate = %v, want %v", MinValidDate, expectedMin)
	}

	// Verify MaxValidDate is 9999-12-31
	expectedMax := time.Date(9999, 12, 31, 23, 59, 59, 999000000, time.UTC)
	if !MaxValidDate.Equal(expectedMax) {
		t.Errorf("MaxValidDate = %v, want %v", MaxValidDate, expectedMax)
	}
}

// TestErrorSentinels validates that error sentinels are properly defined
func TestErrorSentinels(t *testing.T) {
	if ErrDateTooEarly == nil {
		t.Error("ErrDateTooEarly should not be nil")
	}
	if ErrDateTooLate == nil {
		t.Error("ErrDateTooLate should not be nil")
	}
	if ErrDateInFuture == nil {
		t.Error("ErrDateInFuture should not be nil")
	}
}

// TestSlogTimestampUTC covers the hook every Groadmap logger installs. The gate
// in internal/testenv proves it is INSTALLED everywhere; this proves it is right.
func TestSlogTimestampUTC(t *testing.T) {
	// A fixed instant with a non-zero offset, so a hook that reformatted rather
	// than converted would produce a visibly different reading.
	east := time.FixedZone("TEST+09", 9*60*60)
	instant := time.Date(2026, 9, 3, 20, 51, 5, 221000000, east)

	t.Run("converts the built-in time attribute to UTC", func(t *testing.T) {
		got := SlogTimestampUTC(nil, slog.Time(slog.TimeKey, instant))

		if got.Value.Kind() != slog.KindString {
			t.Fatalf("the rewritten value is a %v, want a string: the handler would fall back to "+
				"its own rendering of a time value, which is the local form", got.Value.Kind())
		}
		const want = "2026-09-03T11:51:05.221Z"
		if got.Value.String() != want {
			t.Errorf("stamp = %q, want %q. A reading of 20:51 with a Z pasted on means the value was "+
				"REFORMATTED rather than converted, and the instant is wrong by the machine's offset",
				got.Value.String(), want)
		}
	})

	t.Run("leaves a time attribute inside a group alone", func(t *testing.T) {
		attr := slog.Time(slog.TimeKey, instant)
		got := SlogTimestampUTC([]string{"request"}, attr)

		if got.Value.Kind() != slog.KindTime {
			t.Errorf("an attribute a caller named %q INSIDE a group was rewritten. Only the record's "+
				"own built-in timestamp is the handler's to replace; rewriting a caller's attribute "+
				"would silently change data the caller chose to log", slog.TimeKey)
		}
	})

	t.Run("leaves an attribute that is not a time alone", func(t *testing.T) {
		attr := slog.String(slog.TimeKey, "not a timestamp")
		got := SlogTimestampUTC(nil, attr)

		if got.Value.String() != "not a timestamp" {
			t.Errorf("value = %q, want it untouched: an attribute keyed %q that is not a time is a "+
				"caller's own string and is not the record's clock reading",
				got.Value.String(), slog.TimeKey)
		}
	})

	t.Run("leaves every other key alone", func(t *testing.T) {
		attr := slog.Time("started_at", instant)
		got := SlogTimestampUTC(nil, attr)

		if got.Value.Kind() != slog.KindTime {
			t.Errorf("an attribute keyed %q was rewritten; only %q is the record's own timestamp",
				"started_at", slog.TimeKey)
		}
	})
}
