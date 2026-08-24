// Package aihelp — documented sprint-status VALUE gate.
//
// TestExampleInvocations_NameOnlyThingsTheCLIHas, in this same package, resolves
// every documented `rmp ...` line against the command contract and deliberately
// stops at NAMES: commands, subcommands and flag spellings, never values. That
// restriction is measured and correct for a corpus-wide gate — 68 of the
// contract's own examples declare a non-zero exit and every one of them fails on
// a value or a missing flag, so a corpus-wide value checker would start
// reporting deliberately-failing examples as defects.
//
// It also means a documented flag VALUE the binary rejects passes that gate
// untouched, and one did. README.md § "What sprints exist?" published
//
//	rmp sprint list -r <name> --status PENDING,CLOSED
//
// for an entire release. Every name in it exists, so the name gate resolved it
// cleanly; the binary does not, because `--status` takes ONE state.
// sprintList hands the raw flag string to models.ParseSprintStatus, which is a
// single map lookup over {PENDING, OPEN, CLOSED} and knows nothing of commas, so
// the line exits 6 with `invalid sprint status: "PENDING,CLOSED"` and returns no
// sprints. SPEC/COMMANDS.md § List Sprints has always said so: "`--status
// <state>` ... restricts the result to sprints whose status equals `<state>`
// (one of PENDING, OPEN, CLOSED)".
//
// # What this gate checks
//
// One value, in one document, on one surface: every value written to the
// `--status` flag of `rmp sprint list` in README.md must be accepted by
// models.ParseSprintStatus — the same function the command calls. Nothing here
// restates which states are legal. The legal set is whatever the parser accepts
// today, so adding or retiring a SprintStatus moves this gate with it, and any
// unparseable value fails, not merely the comma form that produced it.
//
// # Why it is scoped this tightly, and not widened to the whole corpus
//
// Deliberately. The corpus contains examples that are MEANT to fail, which is
// precisely why the name gate refuses to judge values at all; a value gate that
// swept SPEC/ and DOCS/ would inherit that hazard and start failing on correct
// documentation of error paths. README.md is different in kind: it is the quick
// reference, every line in it is offered to the reader as something to type, and
// it documents no error paths. Narrowing to one flag on one surface in that one
// document buys a gate that cannot produce a false positive, and the reasoning
// is recorded here so a later widening is a decision rather than an accident.
//
// The one other `sprint list --status` in the corpus is SPEC/COMMANDS.md's
// synopsis `rmp sprint list -r <name> [--status <state>]`, whose value is a
// placeholder and carries no state to check.
//
// # Derivation rather than restatement
//
//   - The invocations are found by the corpus scanner this package already has,
//     so quoting, `--flag=value`, continuation lines and the `|`/`&&`/`;`
//     splitting are handled once, in one place.
//   - `sprint` and `list` are matched THROUGH the contract's aliases, so
//     `rmp s ls --status ...` is checked exactly like the canonical spelling.
//   - The flag spellings that name the filter are read from the contract, long
//     and short together. `sprint list` publishes only `--status` today; if a
//     short form is ever added, this gate covers it without being edited, and if
//     the flag is ever renamed, documentedSprintStatusSpellings fails rather
//     than quietly checking nothing.
//
// # Why it cannot quietly stop working
//
// A scanner that recognises nothing checks nothing and reports success, so the
// two floors below are the real gate. They also pin the shape of the README
// block as it was decided: the broken line was REPLACED, not deleted, so the
// block still demonstrates the filter twice, on two DIFFERENT states. Deleting
// either filtered example, or collapsing both onto the same state, fails here —
// which is the point, because a block that demonstrates the filter once no
// longer shows that it takes a value.
package aihelp

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The surface this gate reads. `documentedStatusDoc` is README.md and only
// README.md; see the note on scope above.
const (
	documentedStatusDoc        = exampleCorpusReadme
	documentedStatusCommand    = "sprint"
	documentedStatusSubcommand = "list"
	documentedStatusFlagLong   = "--status"
)

// Floors. Measured on this tree: README.md carries three `sprint list` examples,
// two of which filter, on two distinct states. Both floors are equalities in
// effect rather than loose bounds, because each encodes a decision about the
// block rather than an incidental count: the filter is demonstrated twice, and
// the two demonstrations differ.
const (
	minDocumentedSprintStatusValues   = 2
	minDocumentedSprintStatusDistinct = 2
)

// documentedSprintStatusSpellings returns every spelling that names the
// `--status` flag of `rmp sprint list`, read from the contract the binary
// generates from its own registry: the long form and, if one is ever declared,
// the short form. It fails the test if the flag is not there at all, so a rename
// cannot leave this gate scanning for a spelling nothing writes any more.
func documentedSprintStatusSpellings(t *testing.T) map[string]bool {
	t.Helper()

	out, err := Generate(ScopeAll(), testInfo())
	if err != nil {
		t.Fatalf("Generate(ScopeAll()) returned error: %v", err)
	}
	var shape contractShape
	if err := json.Unmarshal(out, &shape); err != nil {
		t.Fatalf("decoding the contract: %v", err)
	}

	spellings := make(map[string]bool, 2)
	for _, cmd := range shape.Commands {
		if cmd.Name != documentedStatusCommand {
			continue
		}
		for _, sub := range cmd.Subcommands {
			if sub.Name != documentedStatusSubcommand {
				continue
			}
			for _, flag := range sub.Flags {
				if flag.Long != documentedStatusFlagLong {
					continue
				}
				spellings[flag.Long] = true
				if flag.Short != nil && *flag.Short != "" {
					spellings[*flag.Short] = true
				}
			}
		}
	}

	if len(spellings) == 0 {
		t.Fatalf("the contract declares no %s flag on `rmp %s %s`; if the filter was renamed, "+
			"this gate must be renamed with it rather than left scanning for a spelling that no "+
			"longer exists",
			documentedStatusFlagLong, documentedStatusCommand, documentedStatusSubcommand)
	}
	return spellings
}

// documentedStatusValue is one value written to the status filter by one
// documented line.
type documentedStatusValue struct {
	file    string
	text    string
	spelled string // the spelling that named the flag, as written
	value   string // the value, unquoted by the scanner
	line    int
}

// statusValuesOf returns every value the given token run writes to one of the
// status spellings, in both the `--status VALUE` and `--status=VALUE` forms.
//
// A masked placeholder value is skipped: `[--status <state>]` names a slot, not
// a state. So is a flag with no value at all, which is a missing-argument defect
// (exit 2) rather than a bad state, and is the name gate's business, not this
// one's.
func statusValuesOf(seg []shellToken, spellings map[string]bool) []string {
	values := make([]string, 0, 1)
	for i, tok := range seg {
		if tok.isEndOfFlags() {
			break
		}
		if !tok.isFlag() || !spellings[tok.spelling()] {
			continue
		}
		if eq := strings.IndexByte(tok.text, '='); eq >= 0 {
			if v := tok.text[eq+1:]; v != shellPlaceholder && v != "" {
				values = append(values, v)
			}
			continue
		}
		if i+1 >= len(seg) {
			continue
		}
		next := seg[i+1]
		if next.isFlag() || next.isPlaceholder() {
			continue
		}
		values = append(values, next.text)
	}
	return values
}

// scanDocumentedSprintStatusValues collects every status value README.md writes
// on a `sprint list` invocation, resolving both names through their aliases.
func scanDocumentedSprintStatusValues(t *testing.T) []documentedStatusValue {
	t.Helper()

	oracle := loadExampleOracle(t)
	spellings := documentedSprintStatusSpellings(t)
	invocations, _, _ := scanExampleInvocations(t)

	found := make([]documentedStatusValue, 0, 4)
	for i := range invocations {
		inv := &invocations[i]
		if inv.file != documentedStatusDoc {
			continue
		}
		// Only a fully resolved invocation has a command and a subcommand to
		// compare; anything else is a synopsis, a bare `rmp`, or a name defect
		// the name gate reports.
		if outcome, _ := oracle.resolve(inv.seg); outcome != outcomeResolved {
			continue
		}
		command, subcommand, _ := oracle.surfaceOf(inv.seg)
		if command != documentedStatusCommand || subcommand != documentedStatusSubcommand {
			continue
		}
		for _, value := range statusValuesOf(inv.seg, spellings) {
			found = append(found, documentedStatusValue{
				file:    inv.file,
				line:    inv.line,
				text:    inv.text,
				spelled: documentedStatusFlagLong,
				value:   value,
			})
		}
	}
	return found
}

// TestDocumentedSprintStatusValues_ParseAsTheBinaryParsesThem is the gate
// proper: every state README.md writes to `sprint list --status` must be a state
// models.ParseSprintStatus accepts.
func TestDocumentedSprintStatusValues_ParseAsTheBinaryParsesThem(t *testing.T) {
	found := scanDocumentedSprintStatusValues(t)

	if len(found) < minDocumentedSprintStatusValues {
		t.Fatalf("found %d %s value(s) on `rmp %s %s` in %s, want at least %d: the filter is "+
			"documented by example, and an example that no longer passes a value stops showing "+
			"that the filter takes one",
			len(found), documentedStatusFlagLong, documentedStatusCommand,
			documentedStatusSubcommand, documentedStatusDoc, minDocumentedSprintStatusValues)
	}

	distinct := make(map[string]bool, len(found))
	for i := range found {
		v := &found[i]
		distinct[v.value] = true

		// ParseSprintStatus is an exact lookup in a map keyed by the canonical
		// spellings, so acceptance already means the documented value IS the
		// canonical one. There is nothing further to compare.
		if _, err := models.ParseSprintStatus(v.value); err != nil {
			t.Errorf("%s:%d writes an invalid state to %s:\n"+
				"    %s\n"+
				"  models.ParseSprintStatus(%q) = %v\n"+
				"  The filter takes ONE state, one of %s. Running the line as documented exits 6 "+
				"and returns no sprints. See SPEC/COMMANDS.md § List Sprints.",
				v.file, v.line, v.spelled, v.text, v.value, err, sprintStatusSet())
		}
	}

	if len(distinct) < minDocumentedSprintStatusDistinct {
		t.Errorf("the %d documented %s value(s) on `rmp %s %s` in %s use only %d distinct "+
			"state(s), want at least %d: the second filtered example exists to show a DIFFERENT "+
			"state, and two examples of the same one demonstrate nothing the first did not",
			len(found), documentedStatusFlagLong, documentedStatusCommand,
			documentedStatusSubcommand, documentedStatusDoc, len(distinct),
			minDocumentedSprintStatusDistinct)
	}
}

// sprintStatusSet renders the states the parser accepts, for a failure message
// that tells the writer what could have been meant. It is derived from
// models.ValidSprintStatuses, so it cannot drift from the parser.
func sprintStatusSet() string {
	names := make([]string, 0, len(models.ValidSprintStatuses))
	for _, s := range models.ValidSprintStatuses {
		names = append(names, string(s))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
