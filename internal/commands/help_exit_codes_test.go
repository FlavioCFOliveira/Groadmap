// Tests for the `Exit codes:` block every subcommand help renders from the
// registry (SPEC/HELP.md § Agreement with the contract, "How the block is
// rendered"; rmp task 487).
//
// The defect these guard against: the block was written by hand in each of the
// 55 subcommand helps, and 267 of its per-code descriptions disagreed with the
// conditions the AI Agent Contract publishes for the same code. The fix renders
// the block from the registry in one function; these tests hold that function to
// the published shape, and every help printer to the registry entry it names.
// tests/test_73_help_exit_codes_contract.py holds the compiled binary to the same
// rules from the contract alone.
package commands

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderExitCodesBlock_Shape(t *testing.T) {
	word14 := strings.TrimSpace(strings.Repeat("aaaa ", 14))
	word18 := strings.TrimSpace(strings.Repeat("aaaa ", 18))
	long := strings.Repeat("x", 90)
	// "—" is three bytes and one code point: nineteen of them with their spaces
	// fill 8 + 19*3 + 18 = 83 code points, so the line must break after the
	// seventeenth-and-a-bit, counted in code points and not in bytes.
	dashes := strings.TrimSpace(strings.Repeat("——— ", 19))

	cases := []struct {
		name     string
		entries  []ExitCodeEntry
		remedies []exitCodeRemedy
		want     []string
	}{
		{
			name:    "a one-digit code pads to column eight",
			entries: []ExitCodeEntry{{Code: 0, Conditions: []string{"Done."}}},
			want:    []string{"Exit codes:", "  0     Done."},
		},
		{
			name:    "a three-digit code leaves three spaces",
			entries: []ExitCodeEntry{{Code: 127, Conditions: []string{"Unresolved."}}},
			want:    []string{"Exit codes:", "  127   Unresolved."},
		},
		{
			name:    "each condition begins on a line of its own",
			entries: []ExitCodeEntry{{Code: 2, Conditions: []string{"First.", "Second."}}},
			want:    []string{"Exit codes:", "  2     First.", "        Second."},
		},
		{
			name:    "words are placed greedily within 80 code points",
			entries: []ExitCodeEntry{{Code: 6, Conditions: []string{word18}}},
			want: []string{
				"Exit codes:",
				"  6     " + word14,
				"        aaaa aaaa aaaa aaaa",
			},
		},
		{
			name:    "a word longer than 72 code points stands alone and unbroken",
			entries: []ExitCodeEntry{{Code: 1, Conditions: []string{"short " + long + " tail"}}},
			want:    []string{"Exit codes:", "  1     short", "        " + long, "        tail"},
		},
		{
			name:    "runs of whitespace collapse and no line ends in a space",
			entries: []ExitCodeEntry{{Code: 3, Conditions: []string{"  Neither\t-r   nor\n--roadmap.  "}}},
			want:    []string{"Exit codes:", "  3     Neither -r nor --roadmap."},
		},
		{
			name:    "width is counted in code points",
			entries: []ExitCodeEntry{{Code: 4, Conditions: []string{dashes}}},
			want: []string{
				"Exit codes:",
				"  4     " + strings.TrimSpace(strings.Repeat("——— ", 18)),
				"        ———",
			},
		},
		{
			name: "remedies follow the last condition of their own code only",
			entries: []ExitCodeEntry{
				{Code: 1, Conditions: []string{"Cause one.", "Cause two."}},
				{Code: 2, Conditions: []string{"Misuse."}},
			},
			remedies: []exitCodeRemedy{{Code: 1, Remedies: []string{"Remedy one.", "Remedy two."}}},
			want: []string{
				"Exit codes:",
				"  1     Cause one.",
				"        Cause two.",
				"        Remedy one.",
				"        Remedy two.",
				"  2     Misuse.",
			},
		},
		{
			name:    "no entry renders the header alone",
			entries: nil,
			want:    []string{"Exit codes:"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderExitCodesBlock(tc.entries, tc.remedies)
			want := strings.Join(tc.want, "\n") + "\n\n"
			if got != want {
				t.Fatalf("rendered block differs\n got: %q\nwant: %q", got, want)
			}
			for _, line := range strings.Split(strings.TrimSuffix(got, "\n\n"), "\n") {
				if strings.HasSuffix(line, " ") {
					t.Errorf("line ends in a space: %q", line)
				}
				if n := utf8.RuneCountInString(line); n > exitCodesWidth && !strings.Contains(line, long) {
					t.Errorf("line holds %d code points, over %d: %q", n, exitCodesWidth, line)
				}
			}
		})
	}
}

// helpBlock returns the lines of the one `Exit codes:` block of help, from its
// header to the line before the first empty line after it.
func helpBlock(t *testing.T, label, help string) []string {
	t.Helper()
	if n := strings.Count(help, "\n"+exitCodesHeader+"\n"); n != 1 {
		t.Fatalf("%s: the help carries %d `Exit codes:` blocks, want exactly one", label, n)
	}
	start := strings.Index(help, "\n"+exitCodesHeader+"\n") + 1
	end := strings.Index(help[start:], "\n\n")
	if end < 0 {
		t.Fatalf("%s: the `Exit codes:` block is not closed by an empty line", label)
	}
	return strings.Split(help[start:start+end], "\n")
}

// TestExitCodesBlock_EveryHelpCarriesItsRenderedBlock holds every subcommand help
// printer to the registry entry of the subcommand it serves: the block the help
// writes is the block rendered from that entry, line for line. A printer that
// named another subcommand, or kept a hand-written block, fails here.
//
// `graph client` is the one help whose exit-code-1 entry carries remedies after
// its conditions; its block is compared with the conditions-only rendering, and
// the lines between the end of the exit-1 conditions and the next entry are
// required to be continuation lines.
func TestExitCodesBlock_EveryHelpCarriesItsRenderedBlock(t *testing.T) {
	reg := AppRegistry()
	checked, published := 0, 0
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		for j := range cmd.Subcommands {
			sub := &cmd.Subcommands[j]
			published++
			if cmd.Name == "ai-help" {
				continue
			}
			label := strings.TrimSpace("rmp " + cmd.Name + " " + sub.Name + " --help")
			argv := []string{"--help"}
			if cmd.HasSubcommand {
				argv = []string{sub.Name, "--help"}
			}
			help := captureStdout(t, func() { _ = cmd.DispatchFamily(argv) })
			got := helpBlock(t, label, help)
			want := strings.Split(strings.TrimSuffix(renderExitCodesBlock(sub.ExitCodes, nil), "\n\n"), "\n")
			checked++

			remedyCode := -1
			if cmd.Name == "graph" && sub.Name == "client" {
				remedyCode = 1
			}
			compareRenderedBlock(t, label, got, want, remedyCode)
		}
	}
	if checked != published-1 {
		t.Errorf("checked %d helps, want %d: every subcommand the registry declares other than ai-help",
			checked, published-1)
	}
}

// compareRenderedBlock compares a help's block with the rendered one line for
// line. When remedyCode is a code, the help may carry continuation lines after
// that entry's rendered conditions, and nowhere else.
func compareRenderedBlock(t *testing.T, label string, got, want []string, remedyCode int) {
	t.Helper()
	remedyEntry := ""
	if remedyCode >= 0 {
		remedyEntry = strconv.Itoa(remedyCode)
	}
	g := 0
	for w := 0; w < len(want); w++ {
		if g >= len(got) || got[g] != want[w] {
			line := "<end of block>"
			if g < len(got) {
				line = got[g]
			}
			t.Errorf("%s: block line %d differs from the rendered block\n help:     %q\n rendered: %q",
				label, g+1, line, want[w])
			return
		}
		g++
		// The rendered line that ends the remedy entry's conditions is the last
		// line before the next entry opens, or before the block ends.
		endsRemedyEntry := remedyEntry != "" && entryOf(want, w) == remedyEntry &&
			(w+1 == len(want) || isEntryLine(want[w+1]))
		if !endsRemedyEntry {
			continue
		}
		remedies := 0
		for g < len(got) && !isEntryLine(got[g]) {
			line := got[g]
			if !strings.HasPrefix(line, "        ") || strings.HasPrefix(line, "         ") ||
				strings.HasSuffix(line, " ") || utf8.RuneCountInString(line) > exitCodesWidth {
				t.Errorf("%s: remedy line %d is not a continuation line of at most %d code points: %q",
					label, g+1, exitCodesWidth, line)
				return
			}
			remedies++
			g++
		}
		if remedies == 0 {
			t.Errorf("%s: the exit-%d entry carries no remedy after its conditions", label, remedyCode)
		}
	}
	if g != len(got) {
		t.Errorf("%s: the help's block continues past the rendered block at line %d: %q", label, g+1, got[g])
	}
}

// isEntryLine reports whether line opens an entry: two spaces then a digit.
func isEntryLine(line string) bool {
	return len(line) > 2 && strings.HasPrefix(line, "  ") && line[2] >= '0' && line[2] <= '9'
}

// entryOf returns the code, in decimal, of the entry line w belongs to, or ""
// when no entry line precedes it.
func entryOf(lines []string, w int) string {
	for ; w >= 0; w-- {
		if isEntryLine(lines[w]) {
			return strings.Fields(lines[w])[0]
		}
	}
	return ""
}

func TestExitCodesBlock_UnknownNameRendersNoEntry(t *testing.T) {
	for _, tc := range []struct{ command, subcommand string }{
		{"task", "no-such-subcommand"},
		{"no-such-command", "list"},
		{"stats", "list"},
	} {
		if got := exitCodesBlock(tc.command, tc.subcommand); got != exitCodesHeader+"\n\n" {
			t.Errorf("exitCodesBlock(%q, %q) = %q, want the header alone", tc.command, tc.subcommand, got)
		}
	}
}
