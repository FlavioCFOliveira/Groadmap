// Package aihelp — documented filter-VALUE gate.
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
// untouched, and three did, all of them the same mistake: a comma-separated list
// written to a filter that takes exactly one value. The list separator is real —
// the contract publishes `"list_separator": ","` — but it belongs to the ID
// arguments of `task get`, `task stat` and their siblings, not to these filters.
//
//	README.md § "What sprints exist?"
//	  rmp sprint list -r <name> --status PENDING,CLOSED    exit 6, no sprints
//
//	README.md § "How do I filter and search tasks?"
//	  rmp task list -r <name> --status DOING,TESTING       exit 6, no tasks
//	  rmp task list -r <name> --type BUG --severity 8,9    exit 2, no tasks
//
// Every name in all three exists, so the name gate resolved them cleanly; the
// binary does not accept any of them. sprintList and taskList hand the raw
// `--status` string to models.ParseSprintStatus / models.ParseTaskStatus, each a
// single map lookup that knows nothing of commas, so both status lines exit 6
// with the offending string quoted back. `--severity` never reaches a domain
// check at all: it is declared `Type: "int"` in commands.TaskListFlags, so
// FlagParser.parseValue calls strconv.Atoi on it and fails at exit 2, one code
// EARLIER than the status lines, before the roadmap is even opened.
//
// SPEC/COMMANDS.md has always said so. § List Sprints: "`--status <state>` ...
// restricts the result to sprints whose status equals `<state>`". § List Tasks:
// "`-s, --status <state>` - Filter by status" and "`--severity <n>` - Filter
// severity >= n (0-9)". The severity line is the one worth reading twice: the
// filter is a THRESHOLD, so the single value `8` already denotes the 8-and-9 set
// the comma form was reaching for. Nothing was lost by correcting it.
//
// # What this file checks
//
// Documented filter VALUES, in one document, on the surfaces named below.
// Nothing here restates a legal value. Two gates, because the two kinds of
// filter have two different notions of "legal":
//
//   - TestDocumentedEnumFilterValues_ParseAsTheBinaryParsesThem covers the
//     enum-valued filters. Legality is whatever the binary's own parser accepts
//     today, so adding or retiring a state moves the gate with it.
//
//   - TestDocumentedSeverityFilterValues_ParseAsTheBinaryParsesThem covers the
//     integer-valued `--severity`. See the note on that test for why it sits
//     beside the enum gate rather than inside it.
//
// # Why it is scoped this tightly, and not widened to the whole corpus
//
// Deliberately. The corpus contains examples that are MEANT to fail, which is
// precisely why the name gate refuses to judge values at all; a value gate that
// swept SPEC/ and DOCS/ would inherit that hazard and start failing on correct
// documentation of error paths. README.md is different in kind: it is the quick
// reference, every line in it is offered to the reader as something to type, and
// it documents no error paths. Narrowing to a named flag on a named surface in
// that one document buys a gate that cannot produce a false positive, and the
// reasoning is recorded here so a later widening is a decision rather than an
// accident.
//
// The one other `sprint list --status` in the corpus is SPEC/COMMANDS.md's
// synopsis `rmp sprint list -r <name> [--status <state>]`, whose value is a
// placeholder and carries no state to check.
//
// # Derivation rather than restatement
//
//   - The invocations are found by the corpus scanner this package already has,
//     so quoting, `--flag=value`, continuation lines and the `|`/`&&`/`;`
//     splitting are handled once, in one place. The corpus is walked ONCE per
//     test, not once per surface.
//   - The command and the subcommand are matched THROUGH the contract's aliases,
//     so `rmp t ls --status ...` is checked exactly like the canonical spelling.
//   - The flag spellings that name each filter are read from the contract, long
//     and short together, and the spelling actually written is carried into both
//     the check and the failure message. This matters: `sprint list --status`
//     has no short form, `task list --status` publishes `-s`, and a gate that
//     assumed the long form would misreport the line it failed on.
//   - If a flag is renamed or dropped, documentedFilterFlag fails rather than
//     quietly checking nothing.
//
// # Why it cannot quietly stop working
//
// A scanner that recognises nothing checks nothing and reports success, so the
// floors below are the real gate. They also pin the shape of the README blocks
// as they were decided: every broken line was REPLACED, not deleted, so each
// block still demonstrates its filter, and the enum blocks still demonstrate
// theirs on DIFFERENT states. Deleting a filtered example, or collapsing two
// onto the same state, fails here — which is the point, because a block that
// stops passing a value no longer shows that the filter takes one.
package aihelp

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// documentedFilterDoc is the surface this file reads: README.md and only
// README.md; see the note on scope above.
const documentedFilterDoc = exampleCorpusReadme

// documentedFilterSurface names one documented filter: the command and
// subcommand that carry it, and the long spelling of the flag.
type documentedFilterSurface struct {
	command    string
	subcommand string
	flagLong   string
}

// String renders the surface the way a reader would type it.
func (s documentedFilterSurface) String() string {
	return "rmp " + s.command + " " + s.subcommand + " " + s.flagLong
}

// testName renders the surface as a subtest name (no spaces, no dashes to trim).
func (s documentedFilterSurface) testName() string {
	return s.command + "_" + s.subcommand + "_" + strings.TrimLeft(s.flagLong, "-")
}

// ---------------------------------------------------------------------------
// Reading the contract
// ---------------------------------------------------------------------------

// documentedFilterFlagShape is the subtree of one contract flag entry these
// gates read. It is intentionally NOT contractShape, which the name gate owns:
// the name gate has no business knowing a flag's type or numeric domain, and
// this gate would have no reason to decode global flags or aliases. Each gate
// decodes exactly the view it validates against.
type documentedFilterFlagShape struct {
	Short *string `json:"short"`
	Range *struct {
		Max *int `json:"max"`
		Min int  `json:"min"`
	} `json:"range"`
	Long string `json:"long"`
	Type string `json:"type"`
}

// documentedFilterContract is the subtree of the contract these gates decode.
type documentedFilterContract struct {
	Commands []struct {
		Name        string `json:"name"`
		Subcommands []struct {
			Name  string                      `json:"name"`
			Flags []documentedFilterFlagShape `json:"flags"`
		} `json:"subcommands"`
	} `json:"commands"`
	SchemaVersion string `json:"schema_version"`
}

// documentedFilterFlag returns the contract's declaration of one surface's flag.
// It fails the test if the flag is not declared there at all, so a rename cannot
// leave a gate scanning for a spelling nothing writes any more.
func documentedFilterFlag(t *testing.T, shape *documentedFilterContract, s documentedFilterSurface) documentedFilterFlagShape {
	t.Helper()

	for _, cmd := range shape.Commands {
		if cmd.Name != s.command {
			continue
		}
		for _, sub := range cmd.Subcommands {
			if sub.Name != s.subcommand {
				continue
			}
			for _, flag := range sub.Flags {
				if flag.Long == s.flagLong {
					return flag
				}
			}
		}
	}

	t.Fatalf("the contract declares no %s flag on `rmp %s %s`; if the filter was renamed, this "+
		"gate must be renamed with it rather than left scanning for a spelling that no longer exists",
		s.flagLong, s.command, s.subcommand)
	return documentedFilterFlagShape{}
}

// spellings returns every spelling that names the flag — the long form and, when
// one is declared, the short form.
func (f documentedFilterFlagShape) spellings() map[string]bool {
	out := make(map[string]bool, 2)
	out[f.Long] = true
	if f.Short != nil && *f.Short != "" {
		out[*f.Short] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// Reading the corpus
// ---------------------------------------------------------------------------

// documentedFilterValue is one value written to one filter by one documented
// line, together with the spelling that named the flag.
type documentedFilterValue struct {
	file    string
	text    string
	spelled string
	value   string
	line    int
}

// documentedFlagUse is a spelling-and-value pair as written on one line.
type documentedFlagUse struct {
	spelled string
	value   string
}

// flagValuesOf returns every value the given token run writes to one of the
// spellings, in both the `--flag VALUE` and `--flag=VALUE` forms.
//
// A masked placeholder value is skipped: `[--status <state>]` names a slot, not
// a state. So is a flag with no value at all, which is a missing-argument defect
// (exit 2) rather than a bad value, and is the name gate's business, not this
// one's.
func flagValuesOf(seg []shellToken, spellings map[string]bool) []documentedFlagUse {
	uses := make([]documentedFlagUse, 0, 1)
	for i, tok := range seg {
		if tok.isEndOfFlags() {
			break
		}
		if !tok.isFlag() || !spellings[tok.spelling()] {
			continue
		}
		if eq := strings.IndexByte(tok.text, '='); eq >= 0 {
			if v := tok.text[eq+1:]; v != shellPlaceholder && v != "" {
				uses = append(uses, documentedFlagUse{spelled: tok.spelling(), value: v})
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
		uses = append(uses, documentedFlagUse{spelled: tok.text, value: next.text})
	}
	return uses
}

// documentedFilterScan holds the corpus and the contract, read once, so a test
// that inspects several surfaces pays for one walk instead of one per surface.
type documentedFilterScan struct {
	oracle      *exampleOracle
	shape       *documentedFilterContract
	invocations []exampleInvocation
}

// newDocumentedFilterScan reads the contract and the whole corpus once.
func newDocumentedFilterScan(t *testing.T) *documentedFilterScan {
	t.Helper()

	out, err := Generate(ScopeAll(), testInfo())
	if err != nil {
		t.Fatalf("Generate(ScopeAll()) returned error: %v", err)
	}
	shape := &documentedFilterContract{}
	if err := json.Unmarshal(out, shape); err != nil {
		t.Fatalf("decoding the contract: %v", err)
	}
	if shape.SchemaVersion != SchemaVersion {
		t.Fatalf("the contract declares schema_version %q, this gate was written against %q; "+
			"re-read the shape before trusting anything it reports", shape.SchemaVersion, SchemaVersion)
	}

	invocations, _, _ := scanExampleInvocations(t)
	return &documentedFilterScan{
		oracle:      loadExampleOracle(t),
		shape:       shape,
		invocations: invocations,
	}
}

// valuesOn collects every value README.md writes to one surface's filter,
// resolving the command and the subcommand through their aliases.
func (s *documentedFilterScan) valuesOn(t *testing.T, surface documentedFilterSurface) []documentedFilterValue {
	t.Helper()

	spellings := documentedFilterFlag(t, s.shape, surface).spellings()

	found := make([]documentedFilterValue, 0, 4)
	for i := range s.invocations {
		inv := &s.invocations[i]
		if inv.file != documentedFilterDoc {
			continue
		}
		// Only a fully resolved invocation has a command and a subcommand to
		// compare; anything else is a synopsis, a bare `rmp`, or a name defect
		// the name gate reports.
		if outcome, _ := s.oracle.resolve(inv.seg); outcome != outcomeResolved {
			continue
		}
		command, subcommand, _ := s.oracle.surfaceOf(inv.seg)
		if command != surface.command || subcommand != surface.subcommand {
			continue
		}
		for _, use := range flagValuesOf(inv.seg, spellings) {
			found = append(found, documentedFilterValue{
				file:    inv.file,
				line:    inv.line,
				text:    inv.text,
				spelled: use.spelled,
				value:   use.value,
			})
		}
	}
	return found
}

// ---------------------------------------------------------------------------
// Gate 1: the enum-valued filters
// ---------------------------------------------------------------------------

// documentedEnumFilter is one enum-valued filter and the floors its README block
// must hold.
type documentedEnumFilter struct {
	parse   func(string) error
	legal   func() string
	spec    string
	noun    string
	surface documentedFilterSurface
	// minValues is how many times the block must demonstrate the filter, and
	// minDistinct how many DIFFERENT states those demonstrations must name.
	// Both are measured on this tree and are equalities in effect rather than
	// loose bounds, because each encodes a decision about the block rather than
	// an incidental count: the filter is demonstrated N times, and the
	// demonstrations differ from one another.
	minValues   int
	minDistinct int
	// exit is the code the binary returns on a value this parser rejects.
	exit int
}

// documentedEnumFilters are the enum-valued filters README.md demonstrates.
//
// Measured on this tree: § "What sprints exist?" writes OPEN and CLOSED to
// `sprint list --status`; § "How do I filter and search tasks?" writes BACKLOG,
// DOING and SPRINT to `task list --status`. Every value is distinct on its own
// surface.
var documentedEnumFilters = []documentedEnumFilter{
	{
		surface:     documentedFilterSurface{command: "sprint", subcommand: "list", flagLong: "--status"},
		parse:       func(s string) error { _, err := models.ParseSprintStatus(s); return err },
		legal:       sprintStatusSet,
		noun:        "sprint",
		spec:        "SPEC/COMMANDS.md § List Sprints",
		minValues:   2,
		minDistinct: 2,
		exit:        6,
	},
	{
		surface:     documentedFilterSurface{command: "task", subcommand: "list", flagLong: "--status"},
		parse:       func(s string) error { _, err := models.ParseTaskStatus(s); return err },
		legal:       taskStatusSet,
		noun:        "task",
		spec:        "SPEC/COMMANDS.md § List Tasks",
		minValues:   3,
		minDistinct: 3,
		exit:        6,
	},
}

// TestDocumentedEnumFilterValues_ParseAsTheBinaryParsesThem is the enum gate:
// every state README.md writes to one of these filters must be a state the
// parser the command itself calls accepts.
func TestDocumentedEnumFilterValues_ParseAsTheBinaryParsesThem(t *testing.T) {
	scan := newDocumentedFilterScan(t)

	for _, filter := range documentedEnumFilters {
		t.Run(filter.surface.testName(), func(t *testing.T) {
			found := scan.valuesOn(t, filter.surface)

			if len(found) < filter.minValues {
				t.Fatalf("found %d value(s) written to `%s` in %s, want at least %d: the filter is "+
					"documented by example, and an example that no longer passes a value stops "+
					"showing that the filter takes one",
					len(found), filter.surface, documentedFilterDoc, filter.minValues)
			}

			distinct := make(map[string]bool, len(found))
			for i := range found {
				v := &found[i]
				distinct[v.value] = true

				// The parsers are exact lookups in maps keyed by the canonical
				// spellings, so acceptance already means the documented value IS
				// the canonical one. There is nothing further to compare.
				if err := filter.parse(v.value); err != nil {
					t.Errorf("%s:%d writes an invalid %s status to %s:\n"+
						"    %s\n"+
						"  the parser `%s` calls rejects %q: %v\n"+
						"  The filter takes ONE state, one of %s. The list separator \",\" belongs to "+
						"the ID arguments, not here. Running the line as documented exits %d and "+
						"returns nothing. See %s.",
						v.file, v.line, filter.noun, v.spelled, v.text, filter.surface, v.value, err,
						filter.legal(), filter.exit, filter.spec)
				}
			}

			if len(distinct) < filter.minDistinct {
				t.Errorf("the %d documented value(s) on `%s` in %s use only %d distinct state(s), "+
					"want at least %d: each filtered example exists to show a DIFFERENT state, and "+
					"two examples of the same one demonstrate nothing the first did not",
					len(found), filter.surface, documentedFilterDoc, len(distinct), filter.minDistinct)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Gate 2: the integer-valued severity filter
// ---------------------------------------------------------------------------

// The severity surface, and the floor its README block must hold.
//
// Measured on this tree: § "How do I filter and search tasks?" writes exactly
// one severity, on the `--type BUG --severity 8` line. The floor is 1, which is
// what makes DELETING that line fail rather than pass — the same structural
// assertion the enum gate makes.
//
// There is deliberately NO distinct-values floor here, and the reason is not
// laziness: a distinct floor is only meaningful once a filter is demonstrated
// more than once, and it exists on the enum surfaces to stop two examples
// collapsing onto the same state. One example cannot collapse onto anything.
// Adding a second severity example would be a documentation decision, and the
// floor should be raised then, by that decision — not pre-emptively guessed at
// here.
var (
	documentedSeveritySurface = documentedFilterSurface{
		command:    "task",
		subcommand: "list",
		flagLong:   "--severity",
	}
	minDocumentedSeverityValues = 1
)

// TestDocumentedSeverityFilterValues_ParseAsTheBinaryParsesThem is the severity
// gate.
//
// # Why this is a gate of its own, beside the enum gate rather than inside it
//
// `--severity` shares everything the enum filters have on the READING side — the
// same corpus scan, the same alias resolution, the same contract-derived
// spellings, the same floor — and that machinery is reused here rather than
// duplicated. What it cannot share is the notion of legality, because severity
// has no parser to defer to:
//
//   - There is no models.ParseSeverity. `--severity` is declared `Type: "int"`
//     in commands.TaskListFlags, so the only code the binary runs on the value
//     is FlagParser.parseValue, which calls strconv.Atoi and nothing else. That
//     is why the comma form exits 2 and not 6.
//
//   - Atoi alone is too weak to be the whole check. taskList assigns the parsed
//     integer straight to TaskListFilter.MinSeverity with no domain check, so
//     the binary accepts `--severity 42` and exits 0 with an empty array. A
//     README line documenting a severity outside the domain would therefore be a
//     documentation defect that no exit code reveals, which is precisely the
//     class of defect this file exists to catch.
//
// So legality here is two derived facts, neither of them restated: the binary's
// own flag parser must accept the value, and the value must fall inside the
// numeric domain the contract itself publishes for the flag (`"range": {"min":
// 0, "max": 9}`, generated from the command registry). Retype the flag, move its
// range, or drop it, and this gate moves or fails with it.
func TestDocumentedSeverityFilterValues_ParseAsTheBinaryParsesThem(t *testing.T) {
	scan := newDocumentedFilterScan(t)
	surface := documentedSeveritySurface

	declared := documentedFilterFlag(t, scan.shape, surface)
	if declared.Range == nil {
		t.Fatalf("the contract declares `%s` without a numeric range; this gate checks documented "+
			"values against the domain the contract itself publishes, and has nothing to check "+
			"them against if that domain is gone", surface)
	}

	found := scan.valuesOn(t, surface)
	if len(found) < minDocumentedSeverityValues {
		t.Fatalf("found %d value(s) written to `%s` in %s, want at least %d: the filter is "+
			"documented by example, and an example that no longer passes a value stops showing "+
			"that the filter takes one",
			len(found), surface, documentedFilterDoc, minDocumentedSeverityValues)
	}

	for i := range found {
		v := &found[i]

		def := taskListFlagDef(t, v.spelled)
		result, err := commands.NewFlagParser(commands.TaskListFlags).Parse([]string{v.spelled, v.value})
		if err != nil {
			t.Errorf("%s:%d writes a value the binary's own flag parser rejects to %s:\n"+
				"    %s\n"+
				"  commands.FlagParser.Parse(%q, %q) = %v\n"+
				"  The filter takes ONE integer, and filters severity >= it. The list separator "+
				"\",\" belongs to the ID arguments, not here; a threshold already spans the values "+
				"above it. Running the line as documented exits 2 and returns nothing. "+
				"See SPEC/COMMANDS.md § List Tasks.",
				v.file, v.line, v.spelled, v.text, v.spelled, v.value, err)
			continue
		}

		n, ok := result.Flags[def.Field].(int)
		if !ok {
			t.Errorf("%s:%d: the binary parsed %s %q into %T, not an int; `%s` is declared %q in "+
				"the contract, so either the flag was retyped or this gate is reading the wrong "+
				"field", v.file, v.line, v.spelled, v.value, result.Flags[def.Field], surface, declared.Type)
			continue
		}

		if n < declared.Range.Min || (declared.Range.Max != nil && n > *declared.Range.Max) {
			t.Errorf("%s:%d documents a severity outside the domain the contract publishes:\n"+
				"    %s\n"+
				"  %s %d is outside %s\n"+
				"  The binary does NOT reject this — taskList assigns it to MinSeverity unchecked, "+
				"so the line exits 0 and returns an empty array, which is worse than an error. "+
				"See SPEC/COMMANDS.md § List Tasks.",
				v.file, v.line, v.text, v.spelled, n, renderRange(declared.Range.Min, declared.Range.Max))
		}
	}
}

// taskListFlagDef returns the registry definition behind one spelling of a
// `task list` flag, so the gate reads the parsed value out of the field the
// binary actually populates instead of naming that field itself.
func taskListFlagDef(t *testing.T, spelled string) commands.FlagDef {
	t.Helper()

	for _, def := range commands.TaskListFlags {
		if def.Name == spelled || def.Short == spelled {
			return def
		}
	}
	t.Fatalf("commands.TaskListFlags declares no flag spelled %q, yet the contract generated from "+
		"that same registry publishes it; the two have diverged", spelled)
	return commands.FlagDef{}
}

// renderRange renders a contract range for a failure message. An absent maximum
// is rendered as open-ended rather than invented.
func renderRange(minValue int, maxValue *int) string {
	if maxValue == nil {
		return ">= " + strconv.Itoa(minValue)
	}
	return strconv.Itoa(minValue) + "-" + strconv.Itoa(*maxValue)
}

// ---------------------------------------------------------------------------
// The legal sets, for failure messages
// ---------------------------------------------------------------------------

// sprintStatusSet renders the states the sprint parser accepts, for a failure
// message that tells the writer what could have been meant. It is derived from
// models.ValidSprintStatuses, so it cannot drift from the parser.
func sprintStatusSet() string {
	names := make([]string, 0, len(models.ValidSprintStatuses))
	for _, s := range models.ValidSprintStatuses {
		names = append(names, string(s))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// taskStatusSet is the same, derived from models.ValidTaskStatuses.
func taskStatusSet() string {
	names := make([]string, 0, len(models.ValidTaskStatuses))
	for _, s := range models.ValidTaskStatuses {
		names = append(names, string(s))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
