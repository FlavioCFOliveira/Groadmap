package commands

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The gates of rmp tasks 493 and 574, driven through EVERY subcommand that takes
// the -r / --roadmap selector: an invalid roadmap name is refused with exit code
// 6 and the line of the first rule it breaks (SPEC/COMMANDS.md § Roadmap Name
// Validation), and a roadmap home that is a regular file is a roadmap that does
// not exist, refused with exit code 4 and nothing created or changed
// (SPEC/COMMANDS.md § Roadmap Selection (Always Required); SPEC/GRAPH.md § Error
// Handling and Exit Codes, rule 10).
//
// The set of subcommands is read from the registry — every one whose exit code 3
// carries the no-roadmap condition — and each is driven with its own first
// published success example, with only the selector's value replaced, so the
// invocation passes every check the subcommand places before the roadmap is
// resolved. A subcommand added later is covered without anyone extending a list.

// selectorInvocation is one roadmap-scoped subcommand and the arguments its first
// success example passes to the family dispatcher.
type selectorInvocation struct {
	label  string
	family string
	args   []string // the example's arguments after the family word
}

// roadmapScopedInvocations reads, from the registry, one invocation per
// subcommand that takes -r, with the selector's value replaced by name.
func roadmapScopedInvocations(t *testing.T, name string) []selectorInvocation {
	t.Helper()

	var out []selectorInvocation
	reg := AppRegistry()
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		for j := range cmd.Subcommands {
			sub := &cmd.Subcommands[j]
			if !declaresCondition(sub.ExitCodes, 3, condNoRoadmap) {
				continue
			}
			example := ""
			for _, e := range sub.Examples {
				if e.Exit == 0 {
					example = e.Cmd
					break
				}
			}
			label := strings.TrimSpace(cmd.Name + " " + sub.Name)
			if example == "" {
				t.Fatalf("%s publishes no success example; this gate drives each subcommand through its own", label)
			}
			tokens := shellWords(t, example)
			if len(tokens) < 2 || tokens[0] != "rmp" || tokens[1] != cmd.Name {
				t.Fatalf("%s: the example %q does not start with `rmp %s`", label, example, cmd.Name)
			}
			args := tokens[2:]
			replaced := false
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "-r" || args[i] == "--roadmap" {
					args[i+1] = name
					replaced = true
				}
			}
			if !replaced {
				t.Fatalf("%s: the example %q carries no -r selector to replace", label, example)
			}
			out = append(out, selectorInvocation{label: label, family: cmd.Name, args: args})
		}
	}
	if len(out) < 40 {
		t.Fatalf("found %d roadmap-scoped subcommands in the registry, want at least 40; the walk would "+
			"prove the rule for too few", len(out))
	}
	return out
}

// shellWords splits a published example into its words: whitespace separates
// them, and a double-quoted run is one word without its quotes. The examples
// use no other quoting.
func shellWords(t *testing.T, s string) []string {
	t.Helper()

	var words []string
	var cur strings.Builder
	inQuote, inWord := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			inWord = true
		case (r == ' ' || r == '\t') && !inQuote:
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inQuote {
		t.Fatalf("unterminated quote in the example %q", s)
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// withEmptyStdin runs fn with an empty file on standard input, so no subcommand
// that reads it — a type-only comment edit probes it — depends on what the test
// runner happens to connect there.
func withEmptyStdin(t *testing.T, fn func()) {
	t.Helper()
	_ = withStdin(t, "", fn)
}

// TestRoadmapSelector_AnInvalidNameIsRefusedWithExitCode6 is the rmp task 493
// gate: `con`, a reserved name, and `Bad Name!`, which breaks the character
// rule, are each refused on every roadmap-scoped subcommand with that rule's
// line and exit code 6, and the registry publishes the condition under exit
// code 6 of each.
func TestRoadmapSelector_AnInvalidNameIsRefusedWithExitCode6(t *testing.T) {
	t.Setenv("HOME", shortHome(t))

	for _, tc := range []struct {
		name string
		want string
	}{
		{"con", `validation error: "con": roadmap name is a reserved system name`},
		{"Bad Name!", "Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens"},
	} {
		for _, inv := range roadmapScopedInvocations(t, tc.name) {
			t.Run(tc.name+"/"+inv.label, func(t *testing.T) {
				var out string
				var err error
				withEmptyStdin(t, func() { out, err = dispatchInvocation(t, inv.family, inv.args...) })
				assertPublishedRefusal(t, inv.label+" -r "+tc.name, err, utils.ErrValidation, 6, tc.want)
				if out != "" {
					t.Errorf("a refused invocation wrote to stdout: %q", out)
				}
			})
		}
	}

	// The contract publishes the condition on every one of them.
	for _, cmd := range AppRegistry().Commands {
		for _, sub := range cmd.Subcommands {
			if declaresCondition(sub.ExitCodes, 3, condNoRoadmap) && !declaresCondition(sub.ExitCodes, 6, condInvalidRoadmapName) {
				t.Errorf("`%s %s` takes -r but does not publish the invalid-name condition under exit code 6",
					cmd.Name, sub.Name)
			}
		}
	}
}

// TestRoadmapSelector_ARegularFileHomeIsARoadmapThatDoesNotExist is the rmp task
// 574 gate: with a regular file at ~/.roadmaps/<name>, every roadmap-scoped
// subcommand refuses the roadmap as one that does not exist, with exit code 4,
// and leaves the file and the data directory exactly as it found them.
func TestRoadmapSelector_ARegularFileHomeIsARoadmapThatDoesNotExist(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)

	const name = "ledger-archive"
	dataDir := filepath.Join(home, utils.DataDirName)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("creating the data directory: %v", err)
	}
	occupant := filepath.Join(dataDir, name)
	content := []byte("A settlement export someone saved in the wrong place.\n")
	if err := os.WriteFile(occupant, content, 0o600); err != nil {
		t.Fatalf("writing the regular file at the roadmap home: %v", err)
	}

	assertUntouched := func(t *testing.T, label string) {
		t.Helper()
		info, err := os.Lstat(occupant)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: the regular file at %s was changed (%v, %v)", label, occupant, info, err)
		}
		got, err := os.ReadFile(occupant) // #nosec G304 -- this test's own file under its own HOME
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("%s: the file's content changed: %q (%v)", label, got, err)
		}
		entries, err := os.ReadDir(dataDir)
		if err != nil {
			t.Fatalf("%s: reading the data directory: %v", label, err)
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if !slices.Equal(names, []string{name}) {
			t.Fatalf("%s: the data directory holds %v, want only %q: something was created", label, names, name)
		}
	}

	for _, inv := range roadmapScopedInvocations(t, name) {
		t.Run(inv.label, func(t *testing.T) {
			var out string
			var err error
			withEmptyStdin(t, func() { out, err = dispatchInvocation(t, inv.family, inv.args...) })
			if !errors.Is(err, utils.ErrNotFound) || exitCodeFor(err) != 4 {
				t.Fatalf("%s against a regular-file home = %v (exit %d), want the roadmap-not-found refusal "+
					"(exit 4)", inv.label, err, exitCodeFor(err))
			}
			if !strings.HasPrefix(err.Error(), `resource not found: roadmap "`+name+`"`) {
				t.Errorf("%s printed %q, want the roadmap-not-found line naming %q", inv.label, err.Error(), name)
			}
			if out != "" {
				t.Errorf("a refused invocation wrote to stdout: %q", out)
			}
			assertUntouched(t, inv.label)
		})
	}

	// `roadmap remove` refuses it the same way and removes nothing.
	_, err := dispatchInvocation(t, "roadmap", "remove", name)
	assertPublishedRefusal(t, "roadmap remove over a regular file", err, utils.ErrNotFound, 4,
		`resource not found: roadmap "`+name+`" not found`)
	assertUntouched(t, "roadmap remove")

	// `roadmap create` cannot create the home there, and says where.
	_, err = dispatchInvocation(t, "roadmap", "create", name)
	assertPublishedRefusal(t, "roadmap create over a regular file", err, utils.ErrIO, 1,
		`I/O error: cannot create roadmap "`+name+`": `+occupant+` is occupied and is not a directory`)
	assertUntouched(t, "roadmap create")
}
