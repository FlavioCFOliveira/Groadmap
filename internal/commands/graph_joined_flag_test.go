package commands

import (
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestExtractSocketFlag_JoinedForm is a regression guard for rmp task 477.
//
// `--socket` was read in the separate form only, so `--socket=/tmp/x.sock` was
// left among the arguments and refused as an unknown flag named with its whole
// value. SPEC/COMMANDS.md § Serve Options and § Client Options read the joined
// form as well: the value is the text after the first "=", later "=" characters
// included, and `--socket=` supplies the empty value, which is a missing
// parameter.
func TestExtractSocketFlag_JoinedForm(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantValue string
		wantRest  []string
	}{
		{
			name:      "joined, before the query",
			args:      []string{"--socket=/run/user/1000/graph.sock", "--query", "MATCH (n) RETURN n"},
			wantValue: "/run/user/1000/graph.sock",
			wantRest:  []string{"--query", "MATCH (n) RETURN n"},
		},
		{
			name:      "later = characters belong to the value",
			args:      []string{"--socket=/tmp/ledger=a.sock"},
			wantValue: "/tmp/ledger=a.sock",
			wantRest:  []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, rest, err := extractSocketFlag(tc.args)
			if err != nil {
				t.Fatalf("extractSocketFlag(%q) = %v", tc.args, err)
			}
			if value != tc.wantValue {
				t.Errorf("value = %q, want %q", value, tc.wantValue)
			}
			if len(rest) != len(tc.wantRest) {
				t.Fatalf("rest = %q, want %q", rest, tc.wantRest)
			}
			for i := range rest {
				if rest[i] != tc.wantRest[i] {
					t.Fatalf("rest = %q, want %q", rest, tc.wantRest)
				}
			}
		})
	}

	for _, args := range [][]string{{"--socket="}, {"--socket=   "}} {
		if _, _, err := extractSocketFlag(args); !errors.Is(err, utils.ErrRequired) {
			t.Errorf("extractSocketFlag(%q) = %v, want utils.ErrRequired (a missing parameter)", args, err)
		}
	}
}

// TestGraphFlagParsers_UnknownFlagNamedWithoutItsValue is a regression guard for
// rmp task 477: `graph serve` and `graph client` named an unknown flag with its
// whole token, `unknown flag: --zzz=1`, where every other command writes
// `unknown flag: --zzz` (SPEC/COMMANDS.md § Positional Arguments, rule 5).
func TestGraphFlagParsers_UnknownFlagNamedWithoutItsValue(t *testing.T) {
	cases := []struct {
		token string
		name  string
	}{
		{"--zzz=1", "--zzz"},
		{"--zzz=", "--zzz"},
		{"-z=1", "-z"},
		{"--help=1", "--help"},
		{"--zzz", "--zzz"},
	}
	for _, tc := range cases {
		_, err := readSocketFlag([]string{tc.token})
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("graph serve %s: error = %v, want utils.ErrInvalidInput", tc.token, err)
		} else if got, want := err.Error(), "invalid input: unknown flag: "+tc.name; got != want {
			t.Errorf("graph serve %s: message = %q, want %q", tc.token, got, want)
		}

		_, err = readQuery([]string{"--query", "RETURN 1", tc.token})
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("graph client %s: error = %v, want utils.ErrInvalidInput", tc.token, err)
		} else if got, want := err.Error(), "invalid input: unknown flag: "+tc.name; got != want {
			t.Errorf("graph client %s: message = %q, want %q", tc.token, got, want)
		}
	}
}

// TestReadQuery_JoinedForm is a regression guard for rmp task 477:
// `--query=<cypher>` and `-q=<cypher>` carry the statement in the same token
// (SPEC/GRAPH.md § Cypher Input Source and Precedence, rule 4). The value `help`
// is the statement and asks for no help, and an empty or whitespace-only value is
// the absent value.
func TestReadQuery_JoinedForm(t *testing.T) {
	accepted := []struct {
		args []string
		want string
	}{
		{[]string{"--query=MATCH (n:Spec) RETURN n.key"}, "MATCH (n:Spec) RETURN n.key"},
		{[]string{"-q=help"}, "help"},
		{[]string{"--query=RETURN 'a=b' AS pair"}, "RETURN 'a=b' AS pair"},
	}
	for _, tc := range accepted {
		got, err := readQuery(tc.args)
		if err != nil {
			t.Errorf("readQuery(%q) = %v, want the statement %q", tc.args, err, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("readQuery(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}

	for _, args := range [][]string{{"--query="}, {"-q=   "}} {
		if _, err := readQuery(args); !errors.Is(err, utils.ErrRequired) {
			t.Errorf("readQuery(%q) = %v, want utils.ErrRequired (no query supplied)", args, err)
		}
	}
}
