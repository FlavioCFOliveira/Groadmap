package models

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The application-layer half of the commit-hash rule (SPEC/MODELS.md § Task,
// Commit Hash Constraint). The database-layer half — the CHECK constraint that
// backs it — is exercised over the same accept/reject matrix in
// internal/db/migration_commit_hashes_test.go, so a divergence between the two
// layers fails on one side or the other.

// Real hashes of this repository, used so the fixtures are the values the
// command layer will actually be handed rather than invented digit strings.
const (
	// The abbreviated form git log --oneline prints: the shortest accepted value.
	sevenCharHash = "5f93b51"
	// A full SHA-1 commit hash (the SPEC commit this work implements).
	sha1Hash = "1d0f66a0b91387206c493a857d39b9642b477bb2"
	// A full SHA-256 commit hash, as produced by a repository created with
	// git init --object-format=sha256.
	sha256Hash = "9a7d3f21c05b48e6ff1c2d84b7e0a6539cc41d8b27fe5a0369b4c1de82f7a05c"
)

// TestNormalizeCommitHashAccepts covers every value the SPEC admits, and
// asserts the normalised form each one produces.
func TestNormalizeCommitHashAccepts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		why   string
	}{
		{
			name:  "seven characters, the lower bound",
			input: sevenCharHash,
			want:  sevenCharHash,
			why:   "the conventional abbreviated hash is the shortest accepted value",
		},
		{
			name:  "forty characters, a full SHA-1",
			input: sha1Hash,
			want:  sha1Hash,
			why:   "SHA-1 is the default object format and must round-trip unchanged",
		},
		{
			name:  "sixty-four characters, a full SHA-256",
			input: sha256Hash,
			want:  sha256Hash,
			why:   "the upper bound exists to admit git init --object-format=sha256",
		},
		{
			name:  "uppercase input normalises to lowercase",
			input: strings.ToUpper(sha1Hash),
			want:  sha1Hash,
			why:   "the SPEC accepts any letter case on input and stores the lowercase form",
		},
		{
			name:  "mixed case input normalises to lowercase",
			input: "1D0f66A0b91387206c493A857d39B9642b477BB2",
			want:  sha1Hash,
			why:   "case folding is per character, not all-or-nothing",
		},
		{
			name:  "uppercase abbreviated hash normalises to lowercase",
			input: "5F93B51",
			want:  sevenCharHash,
			why:   "the database CHECK is case-sensitive, so an unnormalised short hash would fail the write",
		},
		{
			name:  "all sixteen hexadecimal digits in both cases",
			input: "0123456789abcdefABCDEF",
			want:  "0123456789abcdefabcdef",
			why:   "every character of the hexadecimal alphabet must be accepted in either case",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeCommitHash(tc.input)
			if err != nil {
				t.Fatalf("NormalizeCommitHash(%q) returned %v; the SPEC accepts this value because %s",
					tc.input, err, tc.why)
			}
			if got != tc.want {
				t.Errorf("NormalizeCommitHash(%q) = %q, want %q (%s)", tc.input, got, tc.want, tc.why)
			}
		})
	}
}

// TestNormalizeCommitHashRejects covers every value the SPEC refuses. Each case
// asserts the error chains BOTH utils.ErrValidation — which SPEC/ARCHITECTURE.md
// maps to exit code 6 — and ErrInvalidCommitHash, so a caller can tell this
// failure apart from any other validation failure.
func TestNormalizeCommitHashRejects(t *testing.T) {
	tests := []struct {
		name  string
		input string
		why   string
	}{
		{
			name:  "empty",
			input: "",
			why:   "an empty value has length 0, below the lower bound",
		},
		{
			name:  "six characters, one below the lower bound",
			input: "5f93b5",
			why:   "the lower bound is 7 and it is inclusive, so 6 is rejected",
		},
		{
			name:  "sixty-five characters, one above the upper bound",
			input: sha256Hash + "0",
			why:   "the upper bound is 64 and it is inclusive, so 65 is rejected",
		},
		{
			name:  "non-hexadecimal letter",
			input: "5f93b5g",
			why:   "g is outside 0-9a-f",
		},
		{
			name:  "non-hexadecimal letter inside a full-length value",
			input: strings.Replace(sha1Hash, "b", "z", 1),
			why:   "one bad character anywhere invalidates the whole value",
		},
		{
			name:  "leading space",
			input: " " + sha1Hash,
			why:   "the SPEC applies no trimming, so a leading space is simply a non-hexadecimal character",
		},
		{
			name:  "trailing space",
			input: sha1Hash + " ",
			why:   "the SPEC applies no trimming, so a trailing space is simply a non-hexadecimal character",
		},
		{
			name:  "leading space on a value that is 7 characters without it",
			input: " 5f93b51",
			why:   "trimming would have made this valid; not trimming makes it 8 characters, one of them a space",
		},
		{
			name:  "embedded newline",
			input: "5f93b5\n1",
			why:   "control characters are not hexadecimal",
		},
		{
			name:  "git ref name rather than a hash",
			input: "refs/heads/main",
			why:   "rmp validates the format only, and a ref name is not hexadecimal",
		},
		{
			name:  "0x-prefixed hexadecimal",
			input: "0x5f93b51",
			why:   "x is not a hexadecimal digit; the SPEC admits the bare hash alone",
		},
		{
			name:  "non-ASCII digits",
			input: "５f93b51",
			why:   "a full-width digit is not in the hexadecimal alphabet",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeCommitHash(tc.input)
			if err == nil {
				t.Fatalf("NormalizeCommitHash(%q) = %q, nil; the SPEC rejects this value because %s",
					tc.input, got, tc.why)
			}
			if got != "" {
				t.Errorf("NormalizeCommitHash(%q) returned the value %q alongside its error; a "+
					"rejected hash must yield the zero value so no caller can store it", tc.input, got)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("NormalizeCommitHash(%q) error %v does not chain utils.ErrValidation, so it "+
					"would not map to exit code 6 (SPEC/ARCHITECTURE.md § Exit Codes)", tc.input, err)
			}
			if !errors.Is(err, ErrInvalidCommitHash) {
				t.Errorf("NormalizeCommitHash(%q) error %v does not chain ErrInvalidCommitHash, so a "+
					"caller cannot tell this failure apart from any other validation failure", tc.input, err)
			}
		})
	}
}

// TestNormalizeCommitHashBoundsMatchTheSpec pins the two constants to the values
// SPEC/MODELS.md § Task states, and proves the bounds are inclusive at both ends
// by exercising the exact boundary lengths rather than restating the numbers.
func TestNormalizeCommitHashBoundsMatchTheSpec(t *testing.T) {
	if MinCommitHashLength != 7 {
		t.Errorf("MinCommitHashLength = %d, want 7 (SPEC/MODELS.md § Task, Commit Hash Constraint)",
			MinCommitHashLength)
	}
	if MaxCommitHashLength != 64 {
		t.Errorf("MaxCommitHashLength = %d, want 64 (SPEC/MODELS.md § Task, Commit Hash Constraint)",
			MaxCommitHashLength)
	}

	atMin := strings.Repeat("a", MinCommitHashLength)
	belowMin := strings.Repeat("a", MinCommitHashLength-1)
	atMax := strings.Repeat("a", MaxCommitHashLength)
	aboveMax := strings.Repeat("a", MaxCommitHashLength+1)

	for _, accepted := range []string{atMin, atMax} {
		if _, err := NormalizeCommitHash(accepted); err != nil {
			t.Errorf("a %d-character hash was rejected (%v); both bounds are inclusive",
				len(accepted), err)
		}
	}
	for _, rejected := range []string{belowMin, aboveMax} {
		if _, err := NormalizeCommitHash(rejected); err == nil {
			t.Errorf("a %d-character hash was accepted; it lies outside [%d, %d]",
				len(rejected), MinCommitHashLength, MaxCommitHashLength)
		}
	}
}

// TestNormalizeCommitHashIsIdempotent asserts that normalising an already
// normalised value changes nothing. The command layer relies on it: a task that
// re-enters DOING is handed a hash that may already have passed through here.
func TestNormalizeCommitHashIsIdempotent(t *testing.T) {
	for _, input := range []string{sevenCharHash, sha1Hash, sha256Hash, strings.ToUpper(sha1Hash)} {
		once, err := NormalizeCommitHash(input)
		if err != nil {
			t.Fatalf("NormalizeCommitHash(%q): %v", input, err)
		}
		twice, err := NormalizeCommitHash(once)
		if err != nil {
			t.Fatalf("NormalizeCommitHash(%q) (second pass): %v", once, err)
		}
		if twice != once {
			t.Errorf("normalising %q twice gave %q then %q; the operation must be idempotent",
				input, once, twice)
		}
	}
}

// TestNormalizeCommitHashDoesNotAllocateForLowercaseInput pins the fast path.
// Every value rmp stores is lowercase, and the transitions that write these
// hashes are on the interactive command path, so the overwhelmingly common input
// must be returned as-is rather than copied byte by byte.
func TestNormalizeCommitHashDoesNotAllocateForLowercaseInput(t *testing.T) {
	var sink string
	allocs := testing.AllocsPerRun(100, func() {
		var err error
		sink, err = NormalizeCommitHash(sha1Hash)
		if err != nil {
			t.Fatalf("NormalizeCommitHash(%q): %v", sha1Hash, err)
		}
	})
	if sink != sha1Hash {
		t.Fatalf("NormalizeCommitHash(%q) = %q", sha1Hash, sink)
	}
	if allocs != 0 {
		t.Errorf("NormalizeCommitHash allocated %.0f time(s) for an already-lowercase hash, want 0: "+
			"the input must be returned unchanged rather than rebuilt", allocs)
	}
}
