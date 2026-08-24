// Package aihelp — documented flag-VALUE gate.
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
// untouched. Six did, and they arrived three times, which is the fact that
// shaped this file:
//
//	#287  README.md:218  sprint list --status PENDING,CLOSED   exit 6
//	#322  README.md:256  task list   --status DOING,TESTING    exit 6
//	      README.md:257  task list   --severity 8,9            exit 2
//	#323  README.md:668  task list   --priority 8,9            exit 2
//	      README.md:646  prose: "filter: --priority 8,9"
//	      README.md:648  prose: "filter: --severity 8,9"
//
// Every name in all six exists, so the name gate resolved them cleanly. All six
// are the same mistake — a comma-separated list written to a filter that takes
// exactly one value. The list separator is real, the contract publishes
// `"list_separator": ","`, but it belongs to the ID arguments of `task get`,
// `task stat` and their siblings, not to these filters. And the sets they were
// reaching for were already denoted: SPEC/COMMANDS.md § List Tasks defines
// `-p, --priority <n>` as "Filter priority >= n" and `--severity <n>` as "Filter
// severity >= n", so a single threshold spans everything above it. Measured on a
// throwaway roadmap holding tasks at priority 9, 8 and 2: `--priority 8` returns
// exactly the 9 and the 8.
//
// # The lesson those three rounds taught, which is what this file implements
//
// #287 fixed one line and the next instance surfaced. #322 fixed two more, added
// the first two value gates, and three more surfaced. The instances were never
// the problem: the gates' REACH was. Each watched a flag it had been told to
// watch — `--status` on two surfaces, `--severity` on one — so every filter
// nobody had thought of stayed unwatched, and the class could not converge.
//
// This gate therefore names no flag. It derives its own reach from the generated
// contract: every command, every subcommand, every flag declared on it. What it
// checks is decided by the flag's TYPE, and the type partition below is asserted
// to be TOTAL, so a contract that grows a new flag type fails here until someone
// decides which side of the boundary it falls on.
//
// Reach has a second dimension, and #323 is where it showed. Two of its three
// defects were not invocations at all: they were ASSERTIONS about a filter,
// written in inline code spans in prose, where the corpus scanner — which reads
// fenced blocks, correctly, because those are the lines you copy and run — could
// never see them. So this gate reads both halves of the document. The fenced
// half is judged against the surface each line resolves to; the prose half,
// which names no command, is judged against every surface the contract declares
// the flag on. A reader takes a claim about a filter exactly as seriously as an
// example of one.
//
// # What is checked, and what deliberately is not
//
// Three of the five types the contract publishes are checked, because for those
// three — and only those three — legality is decidable here without restating
// anything:
//
//	enum     CHECKED. The value must be accepted by the parser the binary itself
//	         calls for that enum. The checkers are keyed by ENUM NAME, not by
//	         flag, and documentedEnumCheckers must be total over the contract's
//	         `enums` map, so a new enum-typed flag that references a new enum
//	         fails here until its parser is wired.
//	integer  CHECKED, twice over. The binary's own FlagParser must accept the
//	         value, AND the value must fall inside the range the contract
//	         publishes for that flag. Atoi alone is too weak: taskList assigns
//	         the parsed integer straight to TaskListFilter.MinSeverity with no
//	         domain check, so `--severity 42` exits 0 with an empty array. A
//	         README line documenting a severity outside the domain would be a
//	         defect no exit code reveals. Where the contract publishes no range,
//	         the parser is the whole check — that is the contract's own statement
//	         that the flag is unbounded, not an omission here.
//	date     CHECKED, since #324. The value must be accepted by
//	         commands.ParseDateFilter, which is the single entry point every
//	         date-range filter the CLI publishes now goes through, so the check
//	         here is the rule the binary runs rather than a copy of it.
//
//	boolean  NOT CHECKED: it carries no value. There is nothing to judge.
//	string   NOT CHECKED: the contract publishes no value domain for a title, a
//	         body or a Cypher query, so there is nothing to check against that is
//	         not a restatement. Three of the 59 documented string values are
//	         shell substitutions — `--commit-open "$(git rev-parse HEAD)"` — that
//	         no static check can evaluate at all.
//
// # How `date` reached the checked side (#324), and why it could not before
//
// The `date` type was the one exclusion that was a finding rather than a
// principle, and it is recorded here because the shape of the defect is the
// argument for the shape of this gate.
//
// The contract published ONE `date` type over TWO acceptance rules:
// `task list --created-since/--created-until` went through
// commands.parseFilterDate, which took RFC3339 or a bare calendar date, while
// `audit list --since/--until` and `audit stats --since/--until` went through
// utils.ParseISO8601, which took RFC3339 only. Type alone did not determine
// legality, so a type-derived check had to either miss one surface or report a
// correct line on the other as broken — and either way it would have been
// judging the flag, not the type.
//
// That was not hypothetical. Measured against the binary built at 8c3343c:
//
//	README.md:517  rmp audit list -r <name> --since 2026-03-20
//	README.md:518  rmp audit list -r <name> --since 2026-03-01 --until 2026-03-31
//	README.md:525  rmp audit stats -r <name> --since 2026-03-01 --until 2026-03-31
//
// each exited 6 with `Error: validation error: invalid date format: ...`, while
// README.md:259's `task list --created-since 2026-03-01` exited 0. #323
// therefore left the three lines alone: correcting them would have entrenched
// the narrower rule, and the narrower rule was the outlier. SPEC/COMMANDS.md
// § Audit Statistics calls the flag an "ISO 8601 date", which a bare calendar
// date is; the contract's own `audit stats` example declares
// `--since 2026-01-01 --until 2026-01-31` at exit 0; and `rmp audit list --help`
// already printed "date-only also accepted". The code was the divergent party.
//
// #324 widened it: both audit subcommands now parse through the same
// commands.ParseDateFilter that `task list` uses, so one published type has one
// acceptance rule again and this gate can derive the check from the type the way
// it derives every other one. The three README lines above are unchanged and now
// run — which is the point, and TestDocumentedFlagValues_ParseAsTheBinaryParses-
// Them would fail on them again if either audit filter narrowed back.
//
// Positional argument values are outside this gate for a structural reason:
// attributing a bare token to a positional slot needs the arity resolution the
// corpus scanner deliberately does not do (see its own package comment). The
// contract does publish enums on positionals, so this is the boundary most
// likely to move next.
//
// # Why it is scoped to README.md, and not widened to the whole corpus
//
// Deliberately, and measured. The corpus contains examples that are MEANT to
// fail — that is precisely why the name gate refuses to judge values at all — so
// a value gate over SPEC/ and DOCS/ would inherit the hazard and start failing
// on correct documentation of error paths. README.md is different in kind: it is
// the quick reference, every line in it is offered to the reader as something to
// type, and it documents no error paths. TestDocumentedFlagValues_ReadmeHasNoErrorPaths
// pins the half of that claim a scanner can see, so it is an assertion rather
// than a belief.
//
// # Derivation rather than restatement
//
//   - The invocations come from the corpus scanner this package already has, so
//     quoting, `--flag=value`, continuation lines and the `|`/`&&`/`;` splitting
//     are handled once, in one place, and the corpus is walked ONCE per test.
//   - The command and the subcommand are matched THROUGH the contract's aliases,
//     so `rmp t ls --status ...` is checked exactly like the canonical spelling.
//   - The flags are indexed per surface from the contract, long and short
//     spellings together, and the spelling actually written is carried into the
//     failure. This matters: `sprint list --status` has no short form,
//     `task list --status` publishes `-s`, and a gate that assumed the long form
//     would misreport the line it failed on.
//   - A flag the contract does not declare on that surface is skipped here and
//     reported by the NAME gate, which owns it. Nothing is checked twice.
//   - The integer check runs the binary's own commands.FlagParser over a
//     definition built FROM the contract entry, so the accept/reject verdict is
//     produced by the same parseValue the CLI runs. (Two flags carry a
//     DisplayName the contract does not publish, so their rejection WORDING
//     differs from the binary's; the verdict, which is what is asserted, does
//     not.)
//
// # Why it cannot quietly stop working
//
// A scanner that recognises nothing checks nothing and reports success, so the
// floors are the real gate, and they come in two kinds.
//
// The reach floors below keep the sweep honest: invocations examined, values
// examined, values actually checked, per-type counts, distinct surfaces reached,
// prose spans read and prose assertions checked. The prose floors matter as much
// as the fenced ones — the prose scan is the newest half and the easiest to
// break without noticing.
//
// The per-surface floors in documentedFilterBlocks are different in kind: they
// are not about the sweep, they are the decisions #287, #322 and #323 recorded
// about the README blocks themselves. Every broken line was REPLACED, never
// deleted, so each block still demonstrates its filter, and the blocks that
// demonstrate a filter more than once still do it on DIFFERENT values. Deleting
// a filtered example, or collapsing two onto the same value, fails there — which
// is the point, because a block that stops passing a value no longer shows that
// the filter takes one.
package aihelp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// documentedValueDoc is the surface this file reads: README.md and only
// README.md; see the note on scope above.
const documentedValueDoc = exampleCorpusReadme

// The flag types the contract publishes. They are the registry's vocabulary
// (internal/commands.Flag.Type), not Go's and not commands.FlagDef's: the
// registry says "integer" where FlagDef says "int".
const (
	contractTypeEnum    = "enum"
	contractTypeInteger = "integer"
	contractTypeString  = "string"
	contractTypeBoolean = "boolean"
	contractTypeDate    = "date"
)

// documentedCheckedTypes and documentedUncheckedTypes partition the flag types.
// The reasons are the payload: TestDocumentedFlagValues_EveryContractTypeIsDecided
// asserts the partition is total over the types the contract actually publishes,
// so a new type cannot slip onto the unchecked side by default — it lands on
// neither side and fails. The boundary is a decision, and these strings are it.
var (
	documentedCheckedTypes = map[string]string{
		contractTypeEnum: "the parser the binary calls for that enum decides, and " +
			"documentedEnumCheckers must be total over the contract's enums",
		contractTypeInteger: "the binary's own FlagParser must accept the value, and the value must " +
			"fall inside the range the contract publishes for the flag",
		contractTypeDate: "commands.ParseDateFilter, the single entry point every date-range filter " +
			"the CLI publishes goes through, must accept the value",
	}

	documentedUncheckedTypes = map[string]string{
		contractTypeBoolean: "carries no value; there is nothing to judge",
		contractTypeString: "the contract publishes no value domain for a title, a body or a Cypher " +
			"query, and three documented values are shell substitutions no static check can evaluate",
	}
)

// documentedEnumCheckers maps an enum NAME the contract publishes to the check
// the binary itself applies to a value of that enum. Keyed by enum rather than
// by flag on purpose: 17 enum-typed flags across 6 command families reference
// these 8 enums, and a gate keyed by flag would have to name all 17.
//
// TaskSort is the one entry that does not call an exported parser, because there
// is none: `task list --sort` and `backlog list --sort` both consult
// commands.validSortFields, an unexported map. The set here is the contract's
// own, which internal/aihelp derives from models.ValidTaskSorts, and
// TestSortFieldTable_MatchesTheModelCatalogue in internal/commands pins that map
// to the same slice — so the chain from this gate to the code the binary runs is
// closed by a test at the one link this package cannot reach directly.
var documentedEnumCheckers = map[string]func(string) error{
	"TaskStatus":        func(s string) error { _, err := models.ParseTaskStatus(s); return err },
	"TaskType":          func(s string) error { _, err := models.ParseTaskType(s); return err },
	"SprintStatus":      func(s string) error { _, err := models.ParseSprintStatus(s); return err },
	"TaskCommentType":   func(s string) error { _, err := models.ParseTaskCommentType(s); return err },
	"SprintCommentType": func(s string) error { _, err := models.ParseSprintCommentType(s); return err },
	"AuditEntityType":   func(s string) error { _, err := models.ParseEntityType(s); return err },
	"AuditOperation": func(s string) error {
		if models.IsValidAuditOperation(s) {
			return nil
		}
		return fmt.Errorf("invalid operation: %s", s)
	},
	"TaskSort": func(s string) error {
		for _, valid := range models.ValidTaskSorts {
			if string(valid) == s {
				return nil
			}
		}
		return fmt.Errorf("--sort must be one of: %s", taskSortSet())
	},
}

// Reach floors. Their job is not to describe the documentation; it is to make a
// gate that has stopped recognising values fail rather than pass with nothing to
// do. Measured on this tree: 118 README invocations resolve to a command and a
// subcommand, they write 98 values to contract-declared flags, 39 of those are
// checkable (16 enum, 17 integer, 6 date) and they land on 13 distinct surfaces.
//
// minDocumentedDateValues is the newest of them and the one with the most to
// hold up. Four of the six date values README writes are the `audit list` and
// `audit stats` lines #324 fixed; a gate that stopped reading them would go back
// to reporting a clean document over exactly the lines that were broken.
const (
	minDocumentedInvocations  = 90
	minDocumentedValuesSeen   = 70
	minDocumentedValuesTyped  = 30
	minDocumentedEnumValues   = 12
	minDocumentedIntValues    = 12
	minDocumentedDateValues   = 4
	minDocumentedSurfaces     = 8
	minDocumentedEnumCheckers = 8
)

// Prose floors. Measured on this tree: README.md holds 274 inline code spans
// outside every fence. Three of them write a non-placeholder value to a flag the
// contract declares — `--host 0.0.0.0`, `--priority 8` and `--severity 8` — and
// none is ambiguous, so two are of a checkable type and are checked. The span
// floor is what makes a prose scan that has stopped finding spans fail instead
// of reporting a clean document.
const (
	minDocumentedProseSpans  = 200
	minDocumentedProseValues = 2
)

// documentedValuePromptedLines is exact, not a floor. README.md holds no
// "$"-prompted transcript: every line in it is offered to be typed, which is the
// premise the whole scope rests on. A prompted line would be a transcript of a
// session, possibly of a failing one, and this gate would judge its value as if
// it were a promise.
const documentedValuePromptedLines = 0

// ---------------------------------------------------------------------------
// Reading the contract
// ---------------------------------------------------------------------------

// documentedFlagShape is the subtree of one contract flag entry this gate reads.
// It is intentionally NOT contractShape, which the name gate owns: the name gate
// has no business knowing a flag's type, enum or numeric domain, and this gate
// has no reason to decode aliases. Each gate decodes exactly the view it
// validates against.
type documentedFlagShape struct {
	Short *string `json:"short"`
	Enum  *string `json:"enum"`
	Range *struct {
		Max *int `json:"max"`
		Min int  `json:"min"`
	} `json:"range"`
	Long string `json:"long"`
	Type string `json:"type"`
}

// documentedValueContract is the subtree of the contract this gate decodes: the
// command tree down to flags, and the enum catalogue the enum-typed flags name.
type documentedValueContract struct {
	Enums map[string]struct {
		Values []struct {
			Value string `json:"value"`
		} `json:"values"`
	} `json:"enums"`
	Commands []struct {
		Name        string `json:"name"`
		Subcommands []struct {
			Name  string                `json:"name"`
			Flags []documentedFlagShape `json:"flags"`
		} `json:"subcommands"`
	} `json:"commands"`
	SchemaVersion string `json:"schema_version"`
}

// legalSet renders an enum's published values for a failure message that has to
// tell the writer what could have been meant.
func (c *documentedValueContract) legalSet(enum string) string {
	def, ok := c.Enums[enum]
	if !ok {
		return "(the contract publishes no values for " + enum + ")"
	}
	names := make([]string, 0, len(def.Values))
	for _, v := range def.Values {
		names = append(names, v.Value)
	}
	return strings.Join(names, ", ")
}

// taskSortSet renders the sort fields, derived from models.ValidTaskSorts so it
// cannot drift from the catalogue the contract publishes.
func taskSortSet() string {
	names := make([]string, 0, len(models.ValidTaskSorts))
	for _, s := range models.ValidTaskSorts {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}

// ---------------------------------------------------------------------------
// The surface index
// ---------------------------------------------------------------------------

// documentedSurface names one command-and-subcommand pair, canonically.
type documentedSurface struct {
	command    string
	subcommand string
}

// String renders the surface the way a reader would type it. A flat family whose
// single subcommand carries the family name — `stats`, `web`, `ai-help` — is
// written once, not twice.
func (s documentedSurface) String() string {
	if s.command == s.subcommand {
		return "rmp " + s.command
	}
	return "rmp " + s.command + " " + s.subcommand
}

// documentedFlagIndex maps a surface, then a flag SPELLING, to the contract's
// declaration of that flag. Both spellings of a flag point at the same entry.
type documentedFlagIndex map[documentedSurface]map[string]documentedFlagShape

// buildDocumentedFlagIndex indexes every flag the contract declares. Nothing is
// filtered here: the type decision belongs to the check, and indexing every flag
// is what lets the reach report say how many values of each type were seen.
func buildDocumentedFlagIndex(shape *documentedValueContract) documentedFlagIndex {
	index := make(documentedFlagIndex, len(shape.Commands)*8)
	for i := range shape.Commands {
		cmd := &shape.Commands[i]
		for j := range cmd.Subcommands {
			sub := &cmd.Subcommands[j]
			spellings := make(map[string]documentedFlagShape, len(sub.Flags)*2)
			for k := range sub.Flags {
				flag := sub.Flags[k]
				spellings[flag.Long] = flag
				if flag.Short != nil && *flag.Short != "" {
					spellings[*flag.Short] = flag
				}
			}
			index[documentedSurface{command: cmd.Name, subcommand: sub.Name}] = spellings
		}
	}
	return index
}

// ---------------------------------------------------------------------------
// Reading the corpus
// ---------------------------------------------------------------------------

// documentedFlagValue is one value written by one documented line to one flag
// the contract declares on the surface that line resolves to.
type documentedFlagValue struct {
	file    string
	text    string
	spelled string
	value   string
	// scope names what the value was judged against: the surface the line
	// resolved to, or, for a prose assertion that names no command, every
	// surface the contract declares the flag on.
	scope   string
	flag    documentedFlagShape
	surface documentedSurface
	line    int
	// prose marks a value asserted in an inline code span rather than published
	// as a line to run. Both must be true; only the wording of the failure
	// differs.
	prose bool
}

// documentedValueScan holds the contract, its flag index and the corpus, read
// once, so a test that inspects several surfaces pays for one walk rather than
// one per surface.
type documentedValueScan struct {
	oracle *exampleOracle
	shape  *documentedValueContract
	index  documentedFlagIndex
	// spellings collapses index by flag spelling, for the prose scan, which has
	// no command to resolve against.
	spellings documentedSpellingIndex
	values    []documentedFlagValue
	// prose holds the values asserted in inline code spans outside every fence.
	prose []documentedFlagValue
	// proseSpans is how many spans were read and proseAmbiguous how many
	// flag-and-value pairs were skipped because the contract declares the flag
	// with two different meanings.
	proseSpans     int
	proseAmbiguous int
	// invocations is how many README lines resolved to a command and a
	// subcommand, and prompted how many carried an interactive prompt.
	invocations int
	prompted    int
}

// newDocumentedValueScan reads the contract and the whole corpus once and
// collects every documented flag value on the target document.
func newDocumentedValueScan(t *testing.T) *documentedValueScan {
	t.Helper()

	out, err := Generate(ScopeAll(), testInfo())
	if err != nil {
		t.Fatalf("Generate(ScopeAll()) returned error: %v", err)
	}
	shape := &documentedValueContract{}
	if err := json.Unmarshal(out, shape); err != nil {
		t.Fatalf("decoding the contract: %v", err)
	}
	if shape.SchemaVersion != SchemaVersion {
		t.Fatalf("the contract declares schema_version %q, this gate was written against %q; "+
			"re-read the shape before trusting anything it reports", shape.SchemaVersion, SchemaVersion)
	}

	index := buildDocumentedFlagIndex(shape)
	scan := &documentedValueScan{
		oracle:    loadExampleOracle(t),
		shape:     shape,
		index:     index,
		spellings: buildDocumentedSpellingIndex(index),
		values:    make([]documentedFlagValue, 0, 128),
		prose:     make([]documentedFlagValue, 0, 8),
	}

	for _, span := range proseSpansOf(documentedValueDoc, readRepoFile(t, documentedValueDoc)) {
		scan.proseSpans++
		values, ambiguous := documentedProseValuesOf(&span, scan.spellings)
		scan.prose = append(scan.prose, values...)
		scan.proseAmbiguous += ambiguous
	}

	invocations, _, _ := scanExampleInvocations(t)
	for i := range invocations {
		inv := &invocations[i]
		if inv.file != documentedValueDoc {
			continue
		}
		if inv.prompted {
			scan.prompted++
		}
		// Only a fully resolved invocation has a command and a subcommand to
		// index against; anything else is a synopsis, a bare `rmp`, or a name
		// defect the name gate reports.
		if outcome, _ := scan.oracle.resolve(inv.seg); outcome != outcomeResolved {
			continue
		}
		scan.invocations++
		command, subcommand, _ := scan.oracle.surfaceOf(inv.seg)
		surface := documentedSurface{command: command, subcommand: subcommand}
		scan.values = append(scan.values, documentedValuesOn(inv, surface, scan.index[surface])...)
	}
	return scan
}

// documentedValuesOn returns every value one invocation writes to a flag the
// contract declares on its surface, in both the `--flag VALUE` and
// `--flag=VALUE` forms.
//
// Three things are skipped, each for its own reason. A spelling the contract
// does not declare on this surface belongs to the NAME gate, which reports it. A
// masked placeholder value names a slot, not a value: `[--status <state>]` says
// nothing about any state. A flag with no value at all is a missing-argument
// defect (exit 2) rather than a bad value, and is again the name gate's
// business.
func documentedValuesOn(inv *exampleInvocation, surface documentedSurface,
	spellings map[string]documentedFlagShape) []documentedFlagValue {
	seg := inv.seg
	out := make([]documentedFlagValue, 0, 4)
	for i, tok := range seg {
		if tok.isEndOfFlags() {
			break
		}
		if !tok.isFlag() {
			continue
		}
		flag, declared := spellings[tok.spelling()]
		if !declared {
			continue
		}

		value := ""
		if eq := strings.IndexByte(tok.text, '='); eq >= 0 {
			value = tok.text[eq+1:]
		} else {
			if i+1 >= len(seg) {
				continue
			}
			if next := seg[i+1]; !next.isFlag() && !next.isPlaceholder() {
				value = next.text
			}
		}
		if value == "" || value == shellPlaceholder {
			continue
		}

		out = append(out, documentedFlagValue{
			file:    inv.file,
			line:    inv.line,
			text:    inv.text,
			surface: surface,
			scope:   surface.String(),
			spelled: tok.spelling(),
			value:   value,
			flag:    flag,
		})
	}
	return out
}

// on returns the values written to one flag of one surface, for the structural
// floors.
func (s *documentedValueScan) on(surface documentedSurface, flagLong string) []documentedFlagValue {
	found := make([]documentedFlagValue, 0, 4)
	for i := range s.values {
		v := &s.values[i]
		if v.surface == surface && v.flag.Long == flagLong {
			found = append(found, *v)
		}
	}
	return found
}

// ---------------------------------------------------------------------------
// Reading the prose
// ---------------------------------------------------------------------------

// Two of the six defects this file exists for were not invocations at all:
//
//	README.md:646  - `rmp task prio -r <name> <id> 9` / filter: `--priority 8,9`
//	README.md:648  - `rmp task sev -r <name> <id> 8` / filter: `--severity 8,9`
//
// They are ASSERTIONS about how a filter behaves, written in inline code spans
// in prose, and the corpus scanner cannot see them: it reads fenced blocks only,
// which is right for a gate about lines you copy and run. A reader takes a claim
// about a filter exactly as seriously as an example of one, so the value in the
// claim has to be as true as the value in the example.
//
// A prose span carries no command, so it cannot be resolved to a surface the way
// an invocation is. The rule instead is the strongest one that needs no guess: a
// flag-and-value written in prose must be legal under EVERY surface the contract
// declares that flag on. Where those declarations disagree — `--status` is a
// SprintStatus on `sprint list` and a TaskStatus on `task list` — the span is
// genuinely ambiguous, and it is skipped and counted rather than judged against
// a surface nobody named.
//
// Measured on this tree: of README.md's 274 prose code spans, two write
// `--roadmap <name>`, whose value is a placeholder naming a slot rather than a
// value; one writes `--host 0.0.0.0`, which is string-typed and therefore
// unchecked; and two write `--priority 8` and `--severity 8` — both integer,
// both declared with the identical 0-9 domain on every surface that has them,
// so both are judged. Nothing in the document is currently skipped as
// ambiguous, but `--status` would be: it is a SprintStatus on `sprint list` and
// a TaskStatus on `task list`.

// documentedSpellingEntry is every declaration of one flag spelling across the
// whole command surface, and whether they agree well enough to judge a value
// written without a command.
type documentedSpellingEntry struct {
	flag     documentedFlagShape
	surfaces []string
	agreed   bool
}

// documentedSpellingIndex maps a flag spelling to that summary.
type documentedSpellingIndex map[string]*documentedSpellingEntry

// buildDocumentedSpellingIndex collapses the per-surface index into one keyed by
// spelling alone. Agreement is over the three things a check reads — the type,
// the enum and the numeric domain — and nothing else: two surfaces declaring the
// same flag with different descriptions still agree for this purpose.
func buildDocumentedSpellingIndex(index documentedFlagIndex) documentedSpellingIndex {
	out := make(documentedSpellingIndex, 64)
	for surface, spellings := range index {
		for spelling, flag := range spellings {
			entry, seen := out[spelling]
			if !seen {
				out[spelling] = &documentedSpellingEntry{
					flag:     flag,
					surfaces: []string{surface.String()},
					agreed:   true,
				}
				continue
			}
			entry.surfaces = append(entry.surfaces, surface.String())
			if !documentedFlagsAgree(&entry.flag, &flag) {
				entry.agreed = false
			}
		}
	}
	for _, entry := range out {
		sort.Strings(entry.surfaces)
	}
	return out
}

// documentedFlagsAgree reports whether two declarations of one spelling would
// produce the same verdict on the same value.
func documentedFlagsAgree(a, b *documentedFlagShape) bool {
	if a.Type != b.Type {
		return false
	}
	if (a.Enum == nil) != (b.Enum == nil) {
		return false
	}
	if a.Enum != nil && *a.Enum != *b.Enum {
		return false
	}
	if (a.Range == nil) != (b.Range == nil) {
		return false
	}
	if a.Range == nil {
		return true
	}
	if a.Range.Min != b.Range.Min || (a.Range.Max == nil) != (b.Range.Max == nil) {
		return false
	}
	return a.Range.Max == nil || *a.Range.Max == *b.Range.Max
}

// proseSpansOf returns every inline code span that lies OUTSIDE a fenced block,
// one shellLine per span, carrying the physical line it was written on.
//
// The fence tracking is the same rule the corpus scanner applies, so a span
// inside a fence is never read twice: fenced content belongs to the invocation
// scan, prose to this one, and the two partition the document.
func proseSpansOf(file, raw string) []shellLine {
	physical := strings.Split(raw, "\n")
	out := make([]shellLine, 0, 64)

	inFence := false
	var marker byte
	var width int

	for i, line := range physical {
		m, w, _, isFence := fenceMarkerAt(line)
		if inFence {
			if isFence && m == marker && w >= width {
				inFence = false
			}
			continue
		}
		if isFence {
			inFence, marker, width = true, m, w
			continue
		}
		for _, span := range inlineCodeSpans(line) {
			out = append(out, shellLine{file: file, text: span, line: i + 1})
		}
	}
	return out
}

// inlineCodeSpans returns the contents of every inline code span on one line. A
// span opens on a run of N backticks and closes on the next run of exactly N,
// which is the CommonMark rule and the reason “ `a` “ inside a longer span is
// not mistaken for a delimiter.
func inlineCodeSpans(line string) []string {
	spans := make([]string, 0, 2)
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		openRun := backtickRun(line, i)
		content := i + openRun

		j, closed := content, false
		for j < len(line) {
			if line[j] != '`' {
				j++
				continue
			}
			run := backtickRun(line, j)
			if run == openRun {
				spans = append(spans, line[content:j])
				closed = true
				break
			}
			j += run
		}
		if !closed {
			break
		}
		i = j + openRun
	}
	return spans
}

// backtickRun returns the length of the run of backticks starting at i.
func backtickRun(line string, i int) int {
	n := 0
	for i+n < len(line) && line[i+n] == '`' {
		n++
	}
	return n
}

// documentedProseValuesOf reads one prose span and returns every value it writes
// to a flag the contract declares, under the agreement rule above.
//
// The span is run through the same preparation and tokenisation the fenced lines
// get, so a `--flag=value`, a quoted value or a placeholder is read identically
// on both sides of the document.
func documentedProseValuesOf(span *shellLine, spellings documentedSpellingIndex) (
	values []documentedFlagValue, ambiguous int) {
	body, _ := prepareShellLine(span.text)
	values = make([]documentedFlagValue, 0, 1)

	for _, seg := range splitShellSegments(body) {
		for i, tok := range seg {
			if tok.isEndOfFlags() {
				break
			}
			if !tok.isFlag() {
				continue
			}
			entry, declared := spellings[tok.spelling()]
			if !declared {
				continue
			}

			value := ""
			if eq := strings.IndexByte(tok.text, '='); eq >= 0 {
				value = tok.text[eq+1:]
			} else if i+1 < len(seg) {
				if next := seg[i+1]; !next.isFlag() && !next.isPlaceholder() {
					value = next.text
				}
			}
			if value == "" || value == shellPlaceholder {
				continue
			}
			if !entry.agreed {
				ambiguous++
				continue
			}

			values = append(values, documentedFlagValue{
				file:    span.file,
				line:    span.line,
				text:    "`" + span.text + "`",
				scope:   "every surface that declares it: " + strings.Join(entry.surfaces, ", "),
				spelled: tok.spelling(),
				value:   value,
				flag:    entry.flag,
				prose:   true,
			})
		}
	}
	return values, ambiguous
}

// ---------------------------------------------------------------------------
// 1. The gate: every documented value of a checkable type is one the binary
//    accepts.
// ---------------------------------------------------------------------------

// TestDocumentedFlagValues_ParseAsTheBinaryParsesThem is the gate proper. It
// names no flag: the set it watches is every flag the contract declares, and the
// check applied to each is decided by that flag's published type.
func TestDocumentedFlagValues_ParseAsTheBinaryParsesThem(t *testing.T) {
	scan := newDocumentedValueScan(t)

	requireTotalEnumCheckers(t, scan.shape)

	byType := make(map[string]int, len(documentedCheckedTypes)+len(documentedUncheckedTypes))
	surfaces := make(map[documentedSurface]bool, len(scan.index))
	checked := 0

	for i := range scan.values {
		v := &scan.values[i]
		byType[v.flag.Type]++
		if _, isChecked := documentedCheckedTypes[v.flag.Type]; !isChecked {
			continue
		}
		checked++
		surfaces[v.surface] = true
		checkDocumentedValue(t, scan.shape, v)
	}

	// The prose assertions. Same checks, same reasons; only the failure wording
	// and the scope they are judged against differ. They are counted separately
	// so the reach report shows both halves of the document, and so a prose scan
	// that has gone quiet cannot hide behind the fenced count.
	proseChecked := 0
	for i := range scan.prose {
		v := &scan.prose[i]
		byType[v.flag.Type]++
		if _, isChecked := documentedCheckedTypes[v.flag.Type]; !isChecked {
			continue
		}
		proseChecked++
		checkDocumentedValue(t, scan.shape, v)
	}

	if scan.prompted != documentedValuePromptedLines {
		t.Errorf("%s holds %d \"$\"-prompted invocation(s), want exactly %d. A prompted line is a "+
			"transcript of a session rather than a line offered to be typed, and this gate judges every "+
			"value it finds as a promise. If %s has gained a transcript, this gate's scope must be "+
			"reconsidered before the constant is changed",
			documentedValueDoc, scan.prompted, documentedValuePromptedLines, documentedValueDoc)
	}
	if scan.invocations < minDocumentedInvocations {
		t.Errorf("only %d %s invocations resolved to a command and a subcommand, want at least %d; "+
			"recognition has stopped working and this gate is now reading almost nothing",
			scan.invocations, documentedValueDoc, minDocumentedInvocations)
	}
	if len(scan.values) < minDocumentedValuesSeen {
		t.Errorf("only %d values were read from %s across every declared flag, want at least %d; the "+
			"value extraction has stopped working, so a wrong value would now be invisible whatever its "+
			"type", len(scan.values), documentedValueDoc, minDocumentedValuesSeen)
	}
	if checked < minDocumentedValuesTyped {
		t.Errorf("only %d of the %d documented values were of a checkable type, want at least %d; "+
			"the contract's types are no longer reaching this gate\nby type: %s",
			checked, len(scan.values), minDocumentedValuesTyped, renderTypeCounts(byType))
	}
	if byType[contractTypeEnum] < minDocumentedEnumValues {
		t.Errorf("only %d enum-typed values were checked, want at least %d; the enum side of the gate "+
			"has gone quiet", byType[contractTypeEnum], minDocumentedEnumValues)
	}
	if byType[contractTypeInteger] < minDocumentedIntValues {
		t.Errorf("only %d integer-typed values were checked, want at least %d; the integer side of the "+
			"gate has gone quiet", byType[contractTypeInteger], minDocumentedIntValues)
	}
	if byType[contractTypeDate] < minDocumentedDateValues {
		t.Errorf("only %d date-typed values were checked, want at least %d; the date side of the gate "+
			"has gone quiet, and it is the side that watches the three README lines #324 fixed",
			byType[contractTypeDate], minDocumentedDateValues)
	}
	if scan.proseSpans < minDocumentedProseSpans {
		t.Errorf("only %d inline code spans were read from %s prose, want at least %d; the prose scan "+
			"has stopped working, and the two filter ASSERTIONS #323 corrected live in prose rather "+
			"than in a fence", scan.proseSpans, documentedValueDoc, minDocumentedProseSpans)
	}
	if proseChecked < minDocumentedProseValues {
		t.Errorf("only %d prose flag-value assertion(s) were checked, want at least %d; %d span(s) "+
			"wrote a value to a declared flag and %d were skipped as ambiguous. A prose claim about a "+
			"filter is read exactly as a promise, so it has to be as true as an example of one",
			proseChecked, minDocumentedProseValues, len(scan.prose), scan.proseAmbiguous)
	}
	if len(surfaces) < minDocumentedSurfaces {
		t.Errorf("the checked values reached only %d distinct surfaces, want at least %d; a sweep that "+
			"has collapsed onto one command cannot clear this floor\nreached: %s",
			len(surfaces), minDocumentedSurfaces, renderSurfaces(surfaces))
	}

	t.Logf("examined %d resolved %s invocations writing %d flag values, and %d prose code spans "+
		"writing %d more (%d skipped as ambiguous); checked %d fenced values across %d surfaces and "+
		"%d prose assertions\n  by type: %s\n  reached: %s",
		scan.invocations, documentedValueDoc, len(scan.values), scan.proseSpans, len(scan.prose),
		scan.proseAmbiguous, checked, len(surfaces), proseChecked,
		renderTypeCounts(byType), renderSurfaces(surfaces))
}

// checkDocumentedValue applies the check the value's published type calls for.
// The switch is exhaustive over documentedCheckedTypes, which
// TestDocumentedFlagValues_EveryContractTypeIsDecided keeps total, so a type
// that reaches here without a case is a type nobody decided about.
func checkDocumentedValue(t *testing.T, shape *documentedValueContract, v *documentedFlagValue) {
	t.Helper()

	switch v.flag.Type {
	case contractTypeEnum:
		checkDocumentedEnumValue(t, shape, v)
	case contractTypeInteger:
		checkDocumentedIntegerValue(t, v)
	case contractTypeDate:
		checkDocumentedDateValue(t, v)
	default:
		t.Errorf("%s:%d writes %q to %s, whose type %q is listed as checked but has no check here; "+
			"documentedCheckedTypes and this switch have diverged",
			v.file, v.line, v.value, v.spelled, v.flag.Type)
	}
}

// checkDocumentedEnumValue judges one enum-typed value with the check the binary
// itself applies to that enum.
func checkDocumentedEnumValue(t *testing.T, shape *documentedValueContract, v *documentedFlagValue) {
	t.Helper()

	if v.flag.Enum == nil || *v.flag.Enum == "" {
		t.Errorf("%s:%d writes %q to %s, which the contract declares type %q with no enum name:\n"+
			"    %s\n  An enum-typed flag that names no enum has no value catalogue, so nothing can be "+
			"checked against it. Either the registry entry is missing its Enum, or the type is wrong.",
			v.file, v.line, v.value, v.spelled, v.flag.Type, v.text)
		return
	}

	check, wired := documentedEnumCheckers[*v.flag.Enum]
	if !wired {
		// Unreachable while requireTotalEnumCheckers runs first, and a guard
		// rather than a fall-through: a missing checker must never read as a
		// passing value.
		t.Errorf("%s:%d writes %q to %s, whose enum %q has no checker in documentedEnumCheckers",
			v.file, v.line, v.value, v.spelled, *v.flag.Enum)
		return
	}

	// The parsers are exact lookups keyed by the canonical spellings, so
	// acceptance already means the documented value IS the canonical one. There
	// is nothing further to compare.
	if err := check(v.value); err != nil {
		t.Errorf("%s:%d writes a value the binary rejects to %s:\n"+
			"    %s\n"+
			"  the check `%s` applies to %s rejects %q: %v\n"+
			"  The flag takes ONE %s, one of: %s\n"+
			"  The list separator \",\" belongs to the ID arguments of `task get` and its siblings, not "+
			"to a flag whose value is a single enum member. Running the line as documented returns "+
			"nothing.",
			v.file, v.line, v.spelled, v.text, v.scope, *v.flag.Enum, v.value, err,
			*v.flag.Enum, shape.legalSet(*v.flag.Enum))
	}
}

// checkDocumentedIntegerValue judges one integer-typed value twice: the binary's
// own flag parser must accept it, and it must fall inside the domain the
// contract publishes for that flag.
//
// The two halves catch different defects. The parser catches `8,9`, which never
// reaches a domain check at all — FlagParser.parseValue calls strconv.Atoi and
// fails at exit 2, one code EARLIER than a rejected enum, before the roadmap is
// even opened. The range catches a value that parses and then means nothing:
// taskList assigns `--severity 42` straight to MinSeverity unchecked, so the
// line exits 0 and returns an empty array, which is worse than an error.
func checkDocumentedIntegerValue(t *testing.T, v *documentedFlagValue) {
	t.Helper()

	// The definition is built FROM the contract entry, so the parse is the one
	// the binary runs and the rejection names the flag the way the binary names
	// it.
	def := commands.FlagDef{Name: v.flag.Long, Field: "Value", Type: "int"}
	if v.flag.Short != nil {
		def.Short = *v.flag.Short
	}

	result, err := commands.NewFlagParser([]commands.FlagDef{def}).Parse([]string{v.spelled, v.value})
	if err != nil {
		t.Errorf("%s:%d writes a value the binary's own flag parser rejects to %s:\n"+
			"    %s\n"+
			"  commands.FlagParser.Parse(%q, %q) = %v\n"+
			"  The flag takes ONE integer. Where it is a filter it filters `>= n`, so a single "+
			"threshold already spans every value above it; the list separator \",\" belongs to the ID "+
			"arguments, not here. Running the line as documented exits 2 and returns nothing.",
			v.file, v.line, v.spelled, v.text, v.spelled, v.value, err)
		return
	}

	n, ok := result.Flags["Value"].(int)
	if !ok {
		t.Errorf("%s:%d: the binary parsed %s %q into %T, not an int; `%s` is declared %q in the "+
			"contract, so either the flag was retyped or this gate is reading the wrong field",
			v.file, v.line, v.spelled, v.value, result.Flags["Value"], v.flag.Long, v.flag.Type)
		return
	}

	if v.flag.Range == nil {
		// Not an omission to work around: the contract declaring no range IS the
		// statement that the flag is unbounded, and the parser was the whole
		// check.
		return
	}
	if n < v.flag.Range.Min || (v.flag.Range.Max != nil && n > *v.flag.Range.Max) {
		t.Errorf("%s:%d documents a value outside the domain the contract publishes:\n"+
			"    %s\n"+
			"  %s %d is outside %s on %s\n"+
			"  The binary does not necessarily reject this: a value that parses can still be assigned "+
			"unchecked. `task list --severity 42` exits 0 and returns an empty array, which is worse "+
			"than an error, because no exit code reveals it.",
			v.file, v.line, v.text, v.spelled, n,
			renderRange(v.flag.Range.Min, v.flag.Range.Max), v.scope)
	}
}

// checkDocumentedDateValue judges one date-typed value with the parser every
// date-range filter in the CLI runs.
//
// There is exactly one such parser, and that is what makes this check possible:
// commands.ParseDateFilter is the single entry point behind `task list
// --created-since/--created-until` and `audit list`/`audit stats`
// `--since/--until` alike. While the audit family had a parser of its own the
// contract's one `date` type stood over two different acceptance rules, and no
// check derived from the type could have been right on both surfaces (see the
// package comment). A second parser reappearing anywhere is therefore not a
// tidiness question: it would silently make this check wrong again.
//
// The flag name is passed through because ParseDateFilter names the refused flag
// in its message, so a failure here reads exactly as the binary's own refusal
// would.
func checkDocumentedDateValue(t *testing.T, v *documentedFlagValue) {
	t.Helper()

	if _, err := commands.ParseDateFilter(v.spelled, v.value); err != nil {
		t.Errorf("%s:%d writes a value the binary rejects to %s:\n"+
			"    %s\n"+
			"  commands.ParseDateFilter(%q, %q) = %v\n"+
			"  A date-range filter takes a full RFC3339 timestamp (2026-01-01T00:00:00.000Z) or a "+
			"bare calendar date (2026-01-01), which means the first instant of that day in UTC. The "+
			"same two forms are accepted on every surface the contract types %q, on %s. Running the "+
			"line as documented exits 6 and returns nothing.",
			v.file, v.line, v.spelled, v.text, v.spelled, v.value, err, contractTypeDate, v.scope)
	}
}

// requireTotalEnumCheckers fails unless every enum the contract publishes has a
// checker here. This is what makes the gate converge rather than accumulate
// cases: a new enum-typed flag that references a new enum cannot be documented
// without someone wiring the check the binary applies to it.
func requireTotalEnumCheckers(t *testing.T, shape *documentedValueContract) {
	t.Helper()

	if len(shape.Enums) < minDocumentedEnumCheckers {
		t.Fatalf("the contract publishes only %d enums, this gate was written when it published at "+
			"least %d; an empty catalogue would make every enum check vacuous",
			len(shape.Enums), minDocumentedEnumCheckers)
	}

	missing := make([]string, 0, 1)
	for name := range shape.Enums {
		if _, ok := documentedEnumCheckers[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("the contract publishes %d enum(s) this gate has no checker for: %s\n"+
			"Every enum-typed flag's documented values are judged by the check the binary applies to "+
			"its enum, so an unwired enum is a flag whose documented values nothing reads. Add the "+
			"check to documentedEnumCheckers — the parser the command itself calls, not a restatement "+
			"of the value list.", len(missing), strings.Join(missing, ", "))
	}

	// And the other direction, so a retired enum does not leave a checker behind
	// pretending to cover something.
	stale := make([]string, 0, 1)
	for name := range documentedEnumCheckers {
		if _, ok := shape.Enums[name]; !ok {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("documentedEnumCheckers holds %d checker(s) for enum(s) the contract no longer "+
			"publishes: %s\nA checker for a retired enum reads as coverage and is none.",
			len(stale), strings.Join(stale, ", "))
	}
}

// ---------------------------------------------------------------------------
// 2. The type boundary is decided, not defaulted.
// ---------------------------------------------------------------------------

// TestDocumentedFlagValues_EveryContractTypeIsDecided asserts the partition of
// flag types is total over the types the contract actually declares.
//
// This is the assertion that keeps the boundary honest. Without it, a contract
// that grew a sixth flag type would sail past the gate: the sweep would see
// values of that type, find no entry in documentedCheckedTypes, skip them, and
// report success. With it, the new type belongs to neither set and the build
// stops until someone writes down which side it is on and why.
func TestDocumentedFlagValues_EveryContractTypeIsDecided(t *testing.T) {
	scan := newDocumentedValueScan(t)

	declared := make(map[string]int, 8)
	for _, spellings := range scan.index {
		for _, flag := range spellings {
			declared[flag.Type]++
		}
	}
	if len(declared) == 0 {
		t.Fatal("the contract declares no flags at all; the index is empty and every assertion below " +
			"would be vacuous")
	}

	undecided := make([]string, 0, 1)
	for name := range declared {
		_, checked := documentedCheckedTypes[name]
		_, unchecked := documentedUncheckedTypes[name]
		switch {
		case checked && unchecked:
			t.Errorf("flag type %q is listed as both checked and unchecked; the partition must be a "+
				"partition", name)
		case !checked && !unchecked:
			undecided = append(undecided, name)
		}
	}
	sort.Strings(undecided)
	if len(undecided) > 0 {
		t.Errorf("the contract declares %d flag type(s) this gate has decided nothing about: %s\n"+
			"Every type is either checked, with the check named, or not checked, with the reason "+
			"written down. A type in neither set is a type whose documented values are silently "+
			"unread — which is exactly how three rounds of the same defect reached a release. Add it "+
			"to documentedCheckedTypes or to documentedUncheckedTypes.",
			len(undecided), strings.Join(undecided, ", "))
	}

	// The reasons are the point of the unchecked set, so an empty one is a
	// boundary that has quietly stopped being justified.
	for name, reason := range documentedUncheckedTypes {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("flag type %q is excluded with no reason recorded; the exclusion must be a "+
				"decision, not an accident", name)
		}
	}

	t.Logf("the contract declares %d flag types: %s", len(declared), renderTypeCounts(declared))
	for _, name := range sortedKeys(documentedCheckedTypes) {
		t.Logf("  checked   %-8s (%d declared) — %s", name, declared[name], documentedCheckedTypes[name])
	}
	for _, name := range sortedKeys(documentedUncheckedTypes) {
		t.Logf("  unchecked %-8s (%d declared) — %s", name, declared[name], documentedUncheckedTypes[name])
	}
}

// ---------------------------------------------------------------------------
// 3. The blocks still demonstrate their filter: the decisions #287, #322 and
//    #323 recorded.
// ---------------------------------------------------------------------------

// documentedFilterBlock is one README block and the floors it must hold.
//
// These are not reach floors. Each one is a decision about the documentation:
// every broken line was REPLACED rather than deleted, so the block still shows
// that the filter takes a value, and where a block demonstrates a filter more
// than once the demonstrations differ from one another. Both numbers are
// measured on this tree and are equalities in effect rather than loose bounds.
//
// minDistinct == 0 means "not asserted", and it is deliberate on `--severity`: a
// distinct-values floor is only meaningful once a filter is demonstrated more
// than once, and one example cannot collapse onto anything. Adding a second
// severity example would be a documentation decision, and the floor should be
// raised then, by that decision, rather than pre-emptively guessed at here.
type documentedFilterBlock struct {
	section     string
	flagLong    string
	surface     documentedSurface
	minValues   int
	minDistinct int
}

// documentedFilterBlocks are the README blocks whose shape was decided.
//
// Measured on this tree: § "What sprints exist?" writes OPEN and CLOSED to
// `sprint list --status`; § "How do I filter and search tasks?" writes BACKLOG,
// DOING and SPRINT to `task list --status`, one severity on the `--type BUG
// --severity 8` line, 7 to `--priority`, and 2026-03-01 to `--created-since`;
// § "What is the difference between sprint order and task priority?" writes 8 to
// `--priority`; § "What happened recently?" writes 2026-03-20 and 2026-03-01 to
// `audit list --since` and 2026-03-31 to `--until`; § "How do I see audit
// statistics?" writes 2026-03-01 and 2026-03-31 to `audit stats --since/--until`.
//
// The four audit blocks are #324's. They are the lines the narrower parser
// refused, and #323 deliberately left them intact rather than rewrite them to
// the form the code happened to accept, because the code was the party that had
// diverged. Deleting one now would remove the evidence rather than the defect,
// which is exactly what a floor is for. `task list --created-since` is here as
// their counterpart: it is the surface whose rule was the correct one all along,
// and the gate checks all five against the one parser they now share.
var documentedFilterBlocks = []documentedFilterBlock{
	{
		surface:     documentedSurface{command: "sprint", subcommand: "list"},
		flagLong:    "--status",
		section:     `README.md § "What sprints exist?" (#287)`,
		minValues:   2,
		minDistinct: 2,
	},
	{
		surface:     documentedSurface{command: "task", subcommand: "list"},
		flagLong:    "--status",
		section:     `README.md § "How do I filter and search tasks?" (#322)`,
		minValues:   3,
		minDistinct: 3,
	},
	{
		surface:     documentedSurface{command: "task", subcommand: "list"},
		flagLong:    "--severity",
		section:     `README.md § "How do I filter and search tasks?" (#322)`,
		minValues:   1,
		minDistinct: 0,
	},
	{
		surface:     documentedSurface{command: "task", subcommand: "list"},
		flagLong:    "--priority",
		section:     `README.md §§ "How do I filter and search tasks?" and "sprint order vs task priority" (#323)`,
		minValues:   2,
		minDistinct: 2,
	},
	{
		surface:     documentedSurface{command: "task", subcommand: "list"},
		flagLong:    "--created-since",
		section:     `README.md § "How do I filter and search tasks?" (#324)`,
		minValues:   1,
		minDistinct: 0,
	},
	{
		surface:     documentedSurface{command: "audit", subcommand: "list"},
		flagLong:    "--since",
		section:     `README.md § "What happened recently?" (#324)`,
		minValues:   2,
		minDistinct: 2,
	},
	{
		surface:     documentedSurface{command: "audit", subcommand: "list"},
		flagLong:    "--until",
		section:     `README.md § "What happened recently?" (#324)`,
		minValues:   1,
		minDistinct: 0,
	},
	{
		surface:     documentedSurface{command: "audit", subcommand: "stats"},
		flagLong:    "--since",
		section:     `README.md § "How do I see audit statistics?" (#324)`,
		minValues:   1,
		minDistinct: 0,
	},
	{
		surface:     documentedSurface{command: "audit", subcommand: "stats"},
		flagLong:    "--until",
		section:     `README.md § "How do I see audit statistics?" (#324)`,
		minValues:   1,
		minDistinct: 0,
	},
}

// TestDocumentedFilterBlocks_StillDemonstrateTheirFilter holds the per-surface
// floors. A gate that only parsed values would let a filtered example be deleted
// and still pass, because a document with no examples has no wrong ones.
func TestDocumentedFilterBlocks_StillDemonstrateTheirFilter(t *testing.T) {
	scan := newDocumentedValueScan(t)

	for i := range documentedFilterBlocks {
		block := &documentedFilterBlocks[i]
		name := block.surface.command + "_" + block.surface.subcommand + "_" +
			strings.TrimLeft(block.flagLong, "-")

		t.Run(name, func(t *testing.T) {
			// A flag this gate watches must still exist under that spelling; a
			// rename must move the floor with it rather than leave it counting a
			// spelling nothing writes any more.
			if _, declared := scan.index[block.surface][block.flagLong]; !declared {
				t.Fatalf("the contract declares no %s flag on `%s`; if the filter was renamed, this "+
					"floor must be renamed with it rather than left scanning for a spelling that no "+
					"longer exists", block.flagLong, block.surface)
			}

			found := scan.on(block.surface, block.flagLong)
			if len(found) < block.minValues {
				t.Fatalf("found %d value(s) written to `%s %s` in %s, want at least %d: the filter is "+
					"documented by example, and an example that no longer passes a value stops showing "+
					"that the filter takes one.\n  block: %s",
					len(found), block.surface, block.flagLong, documentedValueDoc, block.minValues,
					block.section)
			}

			if block.minDistinct == 0 {
				t.Logf("%s %s: %d value(s), no distinct floor asserted", block.surface, block.flagLong,
					len(found))
				return
			}
			distinct := make(map[string]bool, len(found))
			for j := range found {
				distinct[found[j].value] = true
			}
			if len(distinct) < block.minDistinct {
				t.Errorf("the %d documented value(s) on `%s %s` in %s use only %d distinct value(s), "+
					"want at least %d: each filtered example exists to show a DIFFERENT one, and two "+
					"examples of the same value demonstrate nothing the first did not.\n  block: %s",
					len(found), block.surface, block.flagLong, documentedValueDoc, len(distinct),
					block.minDistinct, block.section)
			}
			t.Logf("%s %s: %d value(s), %d distinct", block.surface, block.flagLong, len(found),
				len(distinct))
		})
	}
}

// ---------------------------------------------------------------------------
// 4. The premise the scope rests on.
// ---------------------------------------------------------------------------

// TestDocumentedFlagValues_ReadmeHasNoErrorPaths pins the claim that lets this
// gate judge values in README.md while the corpus-wide name gate refuses to: the
// document offers every line as something to type, and demonstrates no failure.
//
// It cannot be proven in full from a scanner — "this line is meant to work" is
// not a syntactic property — but the two shapes a documented failure takes in
// this corpus ARE syntactic, and both are asserted absent: a "$"-prompted
// transcript printed above its own output, and an example annotated with the
// exit code it returns.
func TestDocumentedFlagValues_ReadmeHasNoErrorPaths(t *testing.T) {
	scan := newDocumentedValueScan(t)

	if scan.prompted != documentedValuePromptedLines {
		t.Errorf("%s holds %d prompted transcript(s), want %d; see the note on the constant",
			documentedValueDoc, scan.prompted, documentedValuePromptedLines)
	}

	// The contract's failing examples are annotated with their exit code, and
	// SPEC/ARCHITECTURE.md's `# Exits 3 if no roadmap specified` — the line that
	// produced the name gate — was too. An annotation like that INSIDE A SHELL
	// FENCE is a documented error path, and judging its values as promises would
	// be wrong.
	//
	// The scan is restricted to fenced lines on purpose, and the restriction is
	// what makes it correct rather than merely quiet. README.md:476 describes in
	// prose that a sprint comment rejects the task-only comment types with exit
	// code 6; that sentence documents a RULE, names no value written to a flag,
	// and sits outside every fence, so it is not a line anyone is invited to
	// type and this gate never judges anything on it. Scanning prose would
	// report it, and reporting it would be a false positive.
	fenced := 0
	annotations := make([]string, 0, 1)
	for _, line := range shellLinesOf(documentedValueDoc, readRepoFile(t, documentedValueDoc)) {
		if !strings.Contains(line.text, "rmp") {
			continue
		}
		body, _ := prepareShellLine(line.text)
		isInvocation := false
		for _, seg := range splitShellSegments(body) {
			if isRmpInvocation(seg) {
				isInvocation = true
			}
		}
		if !isInvocation {
			continue
		}
		fenced++
		if claimsExitCode(line.text) {
			annotations = append(annotations, documentedValueDoc+":"+strconv.Itoa(line.line)+"  "+
				strings.Join(strings.Fields(line.text), " "))
		}
	}

	// A scan that recognised no fenced invocation would report no annotation and
	// pass, which is the one way this assertion could stop meaning anything.
	if fenced < minDocumentedInvocations {
		t.Errorf("only %d fenced invocations were read from %s, want at least %d; with none of them "+
			"recognised this assertion is vacuous", fenced, documentedValueDoc, minDocumentedInvocations)
	}
	if len(annotations) > 0 {
		t.Errorf("%d fenced `rmp` line(s) of %s carry an exit-code claim:\n%s\n"+
			"This gate judges every documented value as a value the binary must accept, which is safe "+
			"only while the document demonstrates no failures. If one of these really is a documented "+
			"error path, the scope recorded in this file's package comment has to be revisited before "+
			"the line stays.", len(annotations), documentedValueDoc, strings.Join(annotations, "\n"))
	}

	// And the detector itself, because a scan whose predicate can never fire
	// reports "no error paths" over any document at all. The positives are the
	// three spellings the corpus uses; the negatives are lines README.md really
	// holds, which must not be reported.
	for _, tc := range [...]struct {
		line string
		want bool
	}{
		{`rmp task add -r myproject -d "New task"   # Exits 3 if no roadmap specified`, true},
		{"rmp task list -r myproject --sort foo  # exit code 6", true},
		{"rmp sprint list -r myproject --status BOGUS  # exit 6", true},
		{"rmp task list -r <name> --priority 8      # Priority 8 and above", false},
		{"rmp task next -r <name>                   # Returns task 5 first", false},
		{"rmp backlog show-next -r <name> 5         # Top 5 by priority for sprint planning", false},
	} {
		if got := claimsExitCode(tc.line); got != tc.want {
			t.Errorf("claimsExitCode(%q) = %v, want %v; a predicate that cannot fire turns this whole "+
				"test into a report that any document is free of error paths", tc.line, got, tc.want)
		}
	}

	t.Logf("%s holds %d fenced `rmp` invocations, %d prompted transcripts and %d exit-code annotations",
		documentedValueDoc, fenced, scan.prompted, len(annotations))
}

// claimsExitCode reports whether a documented line pairs its invocation with an
// exit-code claim, in any of the three spellings the corpus uses: "Exits 3",
// "exit code 6", and a bare "exit 6" in a trailing comment.
func claimsExitCode(text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, "exits ") || strings.Contains(lower, "exit code") {
		return true
	}
	for i := strings.Index(lower, "exit "); i >= 0; {
		if rest := lower[i+len("exit "):]; rest != "" && rest[0] >= '0' && rest[0] <= '9' {
			return true
		}
		next := strings.Index(lower[i+1:], "exit ")
		if next < 0 {
			return false
		}
		i += next + 1
	}
	return false
}

// ---------------------------------------------------------------------------
// Rendering helpers
// ---------------------------------------------------------------------------

// renderRange renders a contract range for a failure message. An absent maximum
// is rendered as open-ended rather than invented.
func renderRange(minValue int, maxValue *int) string {
	if maxValue == nil {
		return ">= " + strconv.Itoa(minValue)
	}
	return strconv.Itoa(minValue) + "-" + strconv.Itoa(*maxValue)
}

// renderTypeCounts renders a type tally in a stable order.
func renderTypeCounts(counts map[string]int) string {
	parts := make([]string, 0, len(counts))
	for _, name := range sortedKeysInt(counts) {
		parts = append(parts, name+"="+strconv.Itoa(counts[name]))
	}
	return strings.Join(parts, ", ")
}

// renderSurfaces renders the reached surfaces in a stable order.
func renderSurfaces(surfaces map[documentedSurface]bool) string {
	names := make([]string, 0, len(surfaces))
	for s := range surfaces {
		names = append(names, s.String())
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// sortedKeys returns the sorted keys of a string-to-string map.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedKeysInt returns the sorted keys of a string-to-int map.
func sortedKeysInt(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
