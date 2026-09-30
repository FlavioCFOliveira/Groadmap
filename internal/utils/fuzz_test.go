package utils

import (
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Native fuzz targets for the two untrusted-input parsers this package owns: the
// roadmap name (SPEC/COMMANDS.md § Roadmap Name Validation) and the
// comma-separated task id list (SPEC/COMMANDS.md § Task ID Lists (Batch
// Commands) and § Entity Identifier Range).
//
// Each target asserts the published rule, not merely the absence of a panic. The
// oracles below are written from the SPEC's wording and deliberately do not call
// the helpers they judge: an oracle that reused ValidRoadmapNameRegex or
// WindowsReservedNames would agree with the code under test by construction.

// fuzzRoadmapNamePattern is the character rule SPEC/COMMANDS.md § Roadmap Name
// Validation publishes, compiled here from the SPEC's own text.
var fuzzRoadmapNamePattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// fuzzRoadmapNameMax is the published maximum, in characters.
const fuzzRoadmapNameMax = 50

// fuzzReservedRoadmapNames is the reserved-name set an ACCEPTED name must avoid:
// the two device names SPEC/COMMANDS.md names (`con`, `nul`), the rest of the
// Windows device-name list the validator documents itself as refusing (CON, PRN,
// AUX, NUL, COM1-COM9, LPT1-LPT9), and the CLI's own reserved `help`. The SPEC
// introduces the device names with "such as", so the set is a floor an accepted
// name must clear, not a closed list a rejection is judged against.
var fuzzReservedRoadmapNames = func() map[string]bool {
	set := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true, "help": true}
	for i := 1; i <= 9; i++ {
		set["com"+strconv.Itoa(i)] = true
		set["lpt"+strconv.Itoa(i)] = true
	}
	return set
}()

// FuzzValidateRoadmapName drives ValidateRoadmapName with arbitrary names.
//
// Invariants (SPEC/COMMANDS.md § Roadmap Name Validation):
//
//  1. An accepted name matches ^[a-z0-9_-]+$, is 1 to 50 characters long, does
//     not start with '-', is not a reserved name, and resolves to a roadmap home
//     that is a direct child of the data directory (the path-traversal guard).
//  2. A rejected name carries ErrValidation (exit code 6) and breaks at least one
//     published rule: no valid name is refused.
//  3. A rejection names exactly one rule, through its sentinel, and prints that
//     rule's published line.
//  4. The rule named is the FIRST the name breaks in the published order: empty,
//     length (counted in characters, an invalid byte counting as one), leading
//     hyphen, reserved, character set.
func FuzzValidateRoadmapName(f *testing.F) {
	for _, seed := range []string{
		"", "a", "myroadmap", "roadmap123", "my-roadmap", "my_roadmap", "my-roadmap_123",
		"MyRoadmap", "Bad_UPPER", "my roadmap", "my.roadmap", "my/roadmap", `my\roadmap`,
		"../etc/passwd", `..\windows\system32`, "/etc/passwd", "..", "...", "roadmap/..",
		"-r", "--help", "-", "con", "CON", "nul", "com1", "lpt9", "con.txt", "help", "Help",
		strings.Repeat("a", fuzzRoadmapNameMax), strings.Repeat("a", fuzzRoadmapNameMax+1),
		"roadmap\x00", "road‮map", "\xff",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		err := ValidateRoadmapName(name)

		charsOK := fuzzRoadmapNamePattern.MatchString(name)
		length := utf8.RuneCountInString(name)
		lengthOK := length >= 1 && length <= fuzzRoadmapNameMax
		hyphen := strings.HasPrefix(name, "-")
		reserved := fuzzReservedRoadmapNames[name]

		if err == nil {
			if !charsOK || !lengthOK || hyphen || reserved {
				t.Fatalf("ValidateRoadmapName(%q) accepted a name that breaks a published rule "+
					"(charset ok=%v, length %d, leading hyphen=%v, reserved=%v)",
					name, charsOK, length, hyphen, reserved)
			}
			dir, dirErr := GetRoadmapDir(name)
			if dirErr != nil {
				t.Fatalf("GetRoadmapDir(%q) failed for an accepted name: %v", name, dirErr)
			}
			dataDir, dataErr := GetDataDir()
			if dataErr != nil {
				t.Fatalf("GetDataDir: %v", dataErr)
			}
			if filepath.Dir(dir) != dataDir || filepath.Base(dir) != name {
				t.Fatalf("accepted name %q resolves to %q, which is not the direct child %q of %q",
					name, dir, name, dataDir)
			}
			return
		}

		if !errors.Is(err, ErrValidation) {
			t.Fatalf("ValidateRoadmapName(%q) = %v, which does not carry ErrValidation (exit code 6)",
				name, err)
		}
		if charsOK && lengthOK && !hyphen && !reserved {
			t.Fatalf("ValidateRoadmapName(%q) refused a name that satisfies every published rule: %v",
				name, err)
		}

		type row struct {
			sentinel error
			holds    bool   // the rule the sentinel names is broken by this input
			message  string // the published line, without the "Error: " prefix
		}
		// In the published order of SPEC/COMMANDS.md § Roadmap Name Validation.
		// The reserved row's holds is only a floor, because the SPEC names the
		// device names with "such as"; the order check below reads it that way.
		rows := []row{
			{ErrRoadmapNameEmpty, name == "", "Roadmap name is required"},
			{ErrRoadmapNameTooLong, length > fuzzRoadmapNameMax,
				fmt.Sprintf("Roadmap name must not exceed %d characters (got %d)", fuzzRoadmapNameMax, length)},
			{ErrRoadmapNameStartsWithHyphen, hyphen, "validation error: roadmap name cannot start with '-'"},
			{ErrRoadmapNameReserved, true, fmt.Sprintf("validation error: %q: roadmap name is a reserved system name", name)},
			{ErrInvalidRoadmapName, !charsOK, "Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens"},
		}
		named := 0
		for i, r := range rows {
			if !errors.Is(err, r.sentinel) {
				continue
			}
			named++
			// Invariant 4: no rule earlier in the order is broken. The reserved
			// row is judged by its floor set, so an earlier reserved name the
			// floor misses cannot make this check refuse a correct answer.
			for _, earlier := range rows[:i] {
				broken := earlier.holds
				if earlier.sentinel == ErrRoadmapNameReserved {
					broken = reserved
				}
				if broken {
					t.Fatalf("ValidateRoadmapName(%q) was refused as %q, but it breaks the earlier rule %q, "+
						"which decides the refusal", name, r.sentinel, earlier.sentinel)
				}
			}
			if !r.holds {
				t.Fatalf("ValidateRoadmapName(%q) was refused as %q, a rule this input does not break",
					name, r.sentinel)
			}
			if err.Error() != r.message {
				t.Fatalf("ValidateRoadmapName(%q) printed %q, want the published line %q",
					name, err.Error(), r.message)
			}
		}
		if named != 1 {
			t.Fatalf("ValidateRoadmapName(%q) = %v names %d published rules, want exactly one",
				name, err, named)
		}
	})
}

// fuzzIntegerToken is what SPEC/COMMANDS.md § Entity Identifier Range calls "an
// integer": one or more decimal digits, optionally preceded by a single minus
// sign, and nothing else. A plus sign, a sign with no digit after it, and an
// empty token are not integers and take the format rule.
var fuzzIntegerToken = regexp.MustCompile(`^-?[0-9]+$`)

// FuzzParseCommaSeparatedIDs drives the task id list parser, and the set
// reduction every batch command applies to its result, with arbitrary lists.
//
// Invariants (SPEC/COMMANDS.md § Task ID Lists (Batch Commands) and § Entity
// Identifier Range):
//
//  1. A list that is empty or whitespace only is ErrRequired (exit code 2).
//  2. Otherwise the tokens are judged in order and the FIRST bad token decides
//     the refusal: a token that is not an integer is the format rule
//     (ErrInvalidInput, exit code 2, `invalid task ID: "X" (must be a positive
//     integer)`); an integer outside 1..2147483647 is the range rule
//     (ErrValidation, exit code 6, `task_id must be between 1 and 2147483647,
//     got N`), N naming that integer.
//  3. An accepted list yields one id per token, each in 1..2147483647 and equal to
//     its token's value, and its reduction is a set: no id twice, every id of the
//     list present, in the order of each id's first occurrence.
func FuzzParseCommaSeparatedIDs(f *testing.F) {
	for _, seed := range []string{
		"1", "42", "1,2,3", "1,2, 3", "  100  ", "7,7", "3,1,3,2,1", "1,1,2",
		"", " ", ",", "1,", ",1", "1,,2", "abc", "12abc", "seven", "1,abc,3",
		"0", "-1", "-999", "1,0,3", "1,-1,3", "2147483647", "2147483648",
		"99999999999999999", "999999999999999999999", "+5", "-0", "0005",
		"1 ,2", "\u0085" + "7", "1;2", "1 2", "٣",
	} {
		f.Add(seed)
	}

	maxID := big.NewInt(MaxInt32)
	minID := big.NewInt(MinID)

	f.Fuzz(func(t *testing.T, list string) {
		ids, err := ParseCommaSeparatedIDs(list, FieldTaskID)

		if strings.TrimSpace(list) == "" {
			if err == nil || !errors.Is(err, ErrRequired) {
				t.Fatalf("ParseCommaSeparatedIDs(%q) = (%v, %v), want ErrRequired (exit code 2)", list, ids, err)
			}
			return
		}

		tokens := strings.Split(list, ",")
		for _, raw := range tokens {
			token := strings.TrimSpace(raw)
			if !fuzzIntegerToken.MatchString(token) {
				want := fmt.Sprintf("invalid input: invalid task ID: %q (must be a positive integer)", token)
				if err == nil || !errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrValidation) || err.Error() != want {
					t.Fatalf("ParseCommaSeparatedIDs(%q): token %q is not an integer, so the format rule "+
						"answers (exit code 2, %q); got (%v, %v)", list, token, want, ids, err)
				}
				return
			}
			value, _ := new(big.Int).SetString(token, 10)
			if value.Cmp(minID) < 0 || value.Cmp(maxID) > 0 {
				prefix := "validation error: task_id must be between 1 and 2147483647, got "
				if err == nil || !errors.Is(err, ErrValidation) || !strings.HasPrefix(err.Error(), prefix) {
					t.Fatalf("ParseCommaSeparatedIDs(%q): token %q is an integer outside the range, so "+
						"the range rule answers (exit code 6, %q...); got (%v, %v)", list, token, prefix, ids, err)
				}
				echoed, ok := new(big.Int).SetString(strings.TrimPrefix(err.Error(), prefix), 10)
				if !ok || echoed.Cmp(value) != 0 {
					t.Fatalf("ParseCommaSeparatedIDs(%q): the range refusal %q does not echo the value %s",
						list, err.Error(), value)
				}
				return
			}
		}

		if err != nil {
			t.Fatalf("ParseCommaSeparatedIDs(%q) refused a list whose every token is an id in range: %v", list, err)
		}
		if len(ids) != len(tokens) {
			t.Fatalf("ParseCommaSeparatedIDs(%q) = %v, want one id per token (%d)", list, ids, len(tokens))
		}
		for i, id := range ids {
			if !IDInRange(id) {
				t.Fatalf("ParseCommaSeparatedIDs(%q) accepted id %d outside 1..2147483647", list, id)
			}
			value, _ := new(big.Int).SetString(strings.TrimSpace(tokens[i]), 10)
			if value == nil || value.Cmp(big.NewInt(int64(id))) != 0 {
				t.Fatalf("ParseCommaSeparatedIDs(%q): id %d at position %d does not equal token %q",
					list, id, i, tokens[i])
			}
		}

		distinct := DistinctIDs(ids)
		seen := make(map[int]bool, len(distinct))
		for _, id := range distinct {
			if seen[id] {
				t.Fatalf("DistinctIDs(%v) = %v repeats id %d; the list is a set", ids, distinct, id)
			}
			seen[id] = true
		}
		var firstOccurrence []int
		order := make(map[int]bool, len(ids))
		for _, id := range ids {
			if !order[id] {
				order[id] = true
				firstOccurrence = append(firstOccurrence, id)
			}
		}
		if fmt.Sprint(firstOccurrence) != fmt.Sprint(distinct) {
			t.Fatalf("DistinctIDs(%v) = %v, want %v: every id once, in first-occurrence order",
				ids, distinct, firstOccurrence)
		}
	})
}
