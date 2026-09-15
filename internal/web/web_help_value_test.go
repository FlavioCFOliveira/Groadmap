package web

import (
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestParseArgs_ATokenCarryingAValueIsNeverHelp is a regression guard for rmp
// task 477.
//
// parseArgs split every token on its first "=" before comparing it with the help
// tokens, so `--help=1` and `-h=` were read as `--help` and `-h` and served the
// help. SPEC/HELP.md § Help tokens compares the whole token: both are unknown
// flags, refused with exit 2, and the line names the flag without the "=value"
// tail, as every command names an unknown flag (SPEC/COMMANDS.md § Positional
// Arguments, rule 5).
func TestParseArgs_ATokenCarryingAValueIsNeverHelp(t *testing.T) {
	cases := []struct {
		token string
		name  string
	}{
		{"--help=1", "--help"},
		{"--help=", "--help"},
		{"-h=1", "-h"},
		{"-h=", "-h"},
		{"--zzz=1", "--zzz"},
	}
	for _, tc := range cases {
		_, showHelp, err := parseArgs([]string{tc.token})
		if showHelp {
			t.Errorf("%q: showHelp = true, want the token refused", tc.token)
		}
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("%q: error = %v, want utils.ErrInvalidInput (exit 2)", tc.token, err)
			continue
		}
		if got, want := err.Error(), "invalid input: unknown flag: "+tc.name; got != want {
			t.Errorf("%q: message = %q, want %q", tc.token, got, want)
		}
	}
}
