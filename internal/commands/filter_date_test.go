// Package commands — the date-range filter gate (#324).
//
// The defect this file exists for was not a wrong date and not a wrong message.
// It was TWO parsers behind ONE published flag type.
//
// `task list --created-since/--created-until` parsed through
// commands.parseFilterDate, which took a full RFC3339 timestamp or a bare
// calendar date. `audit list --since/--until` and `audit stats --since/--until`
// parsed through utils.ParseISO8601, which took RFC3339 only. The
// machine-readable contract typed all six flags `date`, SPEC/COMMANDS.md called
// `--since` an "ISO 8601 date" — which a bare calendar date is — README.md wrote
// the date-only form on three lines, `rmp audit list --help` printed "date-only
// also accepted", and the contract published, as an EXAMPLE of `audit stats`,
// an invocation that exited 6. Six surfaces promised the wider rule and one
// parser refused it.
//
// So the tests below come in three layers, and the middle one is the one that
// would have caught it:
//
//  1. ParseDateFilter itself: the two accepted forms, what each means, and the
//     refusal — including the two `task list` strings SPEC/COMMANDS.md publishes
//     verbatim, asserted byte for byte, because widening `audit` must not move
//     them.
//  2. The audit family through its real handler: every date-range flag on both
//     subcommands accepts the date-only form AND reads it as the same instant
//     its RFC3339 midnight twin denotes. Narrowing either filter back to
//     RFC3339 fails here.
//  3. The singleton: within this package, exactly one function decides what a
//     date is, and the set of flags routed through it is exactly the set the
//     contract types `date`. Layer 2 catches a narrowing; this layer catches the
//     thing that MADE the narrowing possible, which is a second parser existing
//     at all. Two parsers that agree today is the state the defect grew out of.
package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ---------------------------------------------------------------------------
// 1. The entry point itself
// ---------------------------------------------------------------------------

// TestParseDateFilter_AcceptsTheTwoPublishedForms pins what each accepted form
// MEANS, not merely that it is accepted. A parser that took the date-only form
// and returned the wrong instant would pass an acceptance-only test and filter
// wrongly, which is worse than a refusal because no exit code reveals it.
func TestParseDateFilter_AcceptsTheTwoPublishedForms(t *testing.T) {
	midnight := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		value string
		want  time.Time
	}{
		{"date-only", "2026-03-20", midnight},
		{"RFC3339 with milliseconds", "2026-03-20T00:00:00.000Z", midnight},
		{"RFC3339 without fractional seconds", "2026-03-20T00:00:00Z", midnight},
		{"RFC3339 with microseconds", "2026-03-20T00:00:00.000000Z", midnight},
		{"RFC3339 with a zero offset", "2026-03-20T00:00:00+00:00", midnight},
		{"RFC3339 with a non-zero offset", "2026-03-20T01:00:00+01:00", midnight},
		{"date-only carries no time of day", "2026-03-20", midnight},
		{"a timestamp keeps its time of day", "2026-03-20T14:35:09.512Z",
			time.Date(2026, 3, 20, 14, 35, 9, 512000000, time.UTC)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDateFilter("--since", tc.value)
			if err != nil {
				t.Fatalf("ParseDateFilter(--since, %q) = error %v, want it accepted; the contract "+
					"types this flag %q and publishes both forms for it", tc.value, err, "date")
			}
			if !got.Equal(tc.want) {
				t.Errorf("ParseDateFilter(--since, %q) = %s, want %s; the value is accepted but read "+
					"as the wrong instant, which filters silently and wrongly",
					tc.value, got.Format(time.RFC3339Nano), tc.want.Format(time.RFC3339Nano))
			}
			if got.Location() != time.UTC {
				t.Errorf("ParseDateFilter(--since, %q) returned a time in %v, want UTC; every stored "+
					"timestamp is UTC and the comparison is lexicographic on the formatted string",
					tc.value, got.Location())
			}
		})
	}
}

// TestParseDateFilter_RefusesWhatIsNotADate pins the refusal: the condition, the
// sentinels that carry it to exit code 6, and the fact that the flag is named.
// A command that carries both a lower and an upper bound must say which one it
// refused; the pre-#324 audit message named neither.
func TestParseDateFilter_RefusesWhatIsNotADate(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"not a date at all", "not-a-date"},
		{"day-first with slashes", "24/05/2026"},
		{"month out of range", "2026-13-01"},
		{"day out of range for the month", "2026-02-30"},
		{"empty", ""},
		{"a timestamp with no date", "14:35:09"},
		{"a bare year", "2026"},
		{"year and month only", "2026-03"},
		{"trailing text after a valid date", "2026-03-20 or thereabouts"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDateFilter("--until", tc.value)
			if err == nil {
				t.Fatalf("ParseDateFilter(--until, %q) = nil error, want a refusal", tc.value)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("ParseDateFilter(--until, %q) does not wrap utils.ErrValidation, so it would "+
					"not map to exit code 6: %v", tc.value, err)
			}
			if !errors.Is(err, ErrInvalidDateFormat) {
				t.Errorf("ParseDateFilter(--until, %q) does not wrap ErrInvalidDateFormat, so a caller "+
					"cannot tell this condition from any other validation failure: %v", tc.value, err)
			}
			if !strings.Contains(err.Error(), "--until") {
				t.Errorf("ParseDateFilter(--until, %q) does not name the flag it refused; a command "+
					"carrying both a lower and an upper bound leaves the reader guessing: %q",
					tc.value, err.Error())
			}
			if !strings.Contains(err.Error(), strconv.Quote(tc.value)) {
				t.Errorf("ParseDateFilter(--until, %q) does not echo the value it refused: %q",
					tc.value, err.Error())
			}
		})
	}
}

// TestParseDateFilter_TaskListRefusalIsByteIdentical is the no-regression half of
// the widening. SPEC/COMMANDS.md § List Tasks publishes these two lines verbatim
// and tests/test_55_error_string_parity.py drives the binary to them, so routing
// `task list` through the shared entry point had to leave them untouched to the
// byte. Only `audit` was allowed to move.
func TestParseDateFilter_TaskListRefusalIsByteIdentical(t *testing.T) {
	// "Error: " is prepended by the exit-code mapper, not by this layer, so the
	// published line is this string with that prefix.
	const published = `validation error: --created-since: invalid date format: ` +
		`expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): "not-a-date"`

	_, err := ParseDateFilter("--created-since", "not-a-date")
	if err == nil {
		t.Fatal("ParseDateFilter(--created-since, \"not-a-date\") = nil error, want a refusal")
	}
	if err.Error() != published {
		t.Errorf("the refusal `task list --created-since` produces has changed:\n  got:  %q\n  want: %q\n"+
			"SPEC/COMMANDS.md § List Tasks publishes this line and tests/test_55_error_string_parity.py "+
			"asserts it character for character. Widening the audit filters must not move it.",
			err.Error(), published)
	}

	const publishedUntil = `validation error: --created-until: invalid date format: ` +
		`expected RFC3339 (2026-01-01T00:00:00Z) or date-only (2026-01-01): "not-a-date"`
	_, err = ParseDateFilter("--created-until", "not-a-date")
	if err == nil {
		t.Fatal("ParseDateFilter(--created-until, \"not-a-date\") = nil error, want a refusal")
	}
	if err.Error() != publishedUntil {
		t.Errorf("the refusal `task list --created-until` produces has changed:\n  got:  %q\n  want: %q",
			err.Error(), publishedUntil)
	}
}

// ---------------------------------------------------------------------------
// 2. The audit family, through its real handler
// ---------------------------------------------------------------------------

// auditDateFilterSurface is one date-range flag on one audit subcommand.
type auditDateFilterSurface struct {
	subcommand string
	flag       string
}

// auditDateFilterSurfaces is every place #324 widened. It is not hand-listed
// against the code: TestDateFilters_ParsedThroughOneEntryPoint asserts the set of
// flags routed through ParseDateFilter is exactly the set the contract types
// `date`, so a seventh date flag cannot appear without that gate naming it.
var auditDateFilterSurfaces = []auditDateFilterSurface{
	{subcommand: "list", flag: "--since"},
	{subcommand: "list", flag: "--until"},
	{subcommand: "stats", flag: "--since"},
	{subcommand: "stats", flag: "--until"},
}

// TestAuditDateFilters_AcceptTheDateOnlyForm is the regression test proper.
//
// Each of the four surfaces is driven twice on the SAME day: once with the
// date-only form and once with the RFC3339 timestamp that denotes the same
// instant. Two things are asserted, and they fail for different reasons:
//
//   - the date-only run must not error. Narrowing either filter back to
//     utils.ParseISO8601 fails here, which is what #324 was.
//   - its output must be byte-identical to the timestamp run. Accepting the form
//     and then reading it as some other instant would pass an acceptance-only
//     test and filter wrongly with exit code 0.
//
// A third assertion keeps the first two from being vacuous: a bound one day
// later must return nothing where the bound under test returns something, so the
// filter is demonstrably filtering rather than being ignored.
func TestAuditDateFilters_AcceptTheDateOnlyForm(t *testing.T) {
	const roadmap = "testauditdateonly"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	// One task is enough: creating it writes a TASK_CREATE entry stamped now.
	if err := HandleTask([]string{
		"create", "-r", roadmap,
		"-t", "Widen the audit date filters to the published form",
		"-fr", "Three documentation surfaces promise the date-only form",
		"-tr", "Route --since and --until through the shared date-filter parser",
		"-ac", "Both published forms are accepted and mean the same instant",
	}); err != nil {
		t.Fatalf("seeding the audit log: %v", err)
	}

	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")

	// For --since, today's midnight is a bound the seeded entry passes and
	// tomorrow's is one it does not. For --until it is the other way round.
	bounds := map[string]struct{ matching, empty string }{
		"--since": {matching: today, empty: tomorrow},
		"--until": {matching: tomorrow, empty: today},
	}

	for _, surface := range auditDateFilterSurfaces {
		t.Run(surface.subcommand+"_"+strings.TrimLeft(surface.flag, "-"), func(t *testing.T) {
			bound := bounds[surface.flag]

			dateOnly, err := runAudit(t, surface.subcommand, roadmap, surface.flag, bound.matching)
			if err != nil {
				t.Fatalf("`rmp audit %s %s %s` was refused: %v\n"+
					"The date-only form is what SPEC/COMMANDS.md, the machine-readable contract, "+
					"README.md and this command's own --help all publish for this flag. Refusing it is "+
					"defect #324 returning: the filter has been narrowed back to RFC3339 only.",
					surface.subcommand, surface.flag, bound.matching, err)
			}

			timestamp := bound.matching + "T00:00:00.000Z"
			rfc3339, err := runAudit(t, surface.subcommand, roadmap, surface.flag, timestamp)
			if err != nil {
				t.Fatalf("`rmp audit %s %s %s` was refused: %v; the full timestamp form has always "+
					"been accepted and must remain so", surface.subcommand, surface.flag, timestamp, err)
			}

			if dateOnly != rfc3339 {
				t.Errorf("`audit %s %s %s` and `audit %s %s %s` disagree, so the date-only form is "+
					"accepted but read as a different instant:\n  date-only: %s\n  timestamp: %s\n"+
					"A bare calendar date denotes the first instant of that day in UTC.",
					surface.subcommand, surface.flag, bound.matching,
					surface.subcommand, surface.flag, timestamp, dateOnly, rfc3339)
			}

			// Non-vacuity: the same flag one day the other side of the entry must
			// select nothing. Without this, a filter that was silently dropped
			// would satisfy every assertion above.
			empty, err := runAudit(t, surface.subcommand, roadmap, surface.flag, bound.empty)
			if err != nil {
				t.Fatalf("`rmp audit %s %s %s` was refused: %v",
					surface.subcommand, surface.flag, bound.empty, err)
			}
			if auditResultIsEmpty(t, surface.subcommand, dateOnly) {
				t.Fatalf("`audit %s %s %s` already selected nothing, so the comparison below proves "+
					"nothing: %s", surface.subcommand, surface.flag, bound.matching, dateOnly)
			}
			if !auditResultIsEmpty(t, surface.subcommand, empty) {
				t.Errorf("`audit %s %s %s` selected entries where the bound excludes every one of "+
					"them: %s\nThe date-only bound is being parsed but not applied, or applied as "+
					"some other day.", surface.subcommand, surface.flag, bound.empty, empty)
			}
		})
	}
}

// runAudit drives one audit subcommand through the real handler and returns what
// it wrote to stdout. The handler is the unit under test: a check that called
// ParseDateFilter directly would pass even if `audit` stopped calling it.
func runAudit(t *testing.T, subcommand, roadmap, flag, value string) (string, error) {
	t.Helper()

	var err error
	out := captureStdout(t, func() {
		err = HandleAudit([]string{subcommand, "-r", roadmap, flag, value})
	})
	if err != nil {
		return "", err
	}
	return out, nil
}

// auditResultIsEmpty reports whether an audit response selected no entries. The
// two subcommands answer in different shapes — `list` in an array, `stats` in an
// object with a total — so the emptiness question is asked of each in its own
// terms rather than by matching text.
func auditResultIsEmpty(t *testing.T, subcommand, out string) bool {
	t.Helper()

	switch subcommand {
	case "list":
		var entries []struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal([]byte(out), &entries); err != nil {
			t.Fatalf("decoding `audit list` output %q: %v", out, err)
		}
		return len(entries) == 0
	case "stats":
		var stats struct {
			TotalEntries int `json:"total_entries"`
		}
		if err := json.Unmarshal([]byte(out), &stats); err != nil {
			t.Fatalf("decoding `audit stats` output %q: %v", out, err)
		}
		return stats.TotalEntries == 0
	default:
		t.Fatalf("auditResultIsEmpty has no shape for subcommand %q", subcommand)
		return false
	}
}

// ---------------------------------------------------------------------------
// 3. The singleton
// ---------------------------------------------------------------------------

// dateFilterEntryPointFile is the one file allowed to decide what a date is.
const dateFilterEntryPointFile = "filter_date.go"

// TestDateFilters_ParsedThroughOneEntryPoint asserts the invariant the fix
// established, in both directions.
//
// Forwards: every flag this package parses as a date goes through
// ParseDateFilter. Backwards: the set of flags it does that for is EXACTLY the
// set the contract types `date` — so a date flag added to the registry and then
// parsed some other way fails here, and so does a flag routed through
// ParseDateFilter that the contract does not publish as a date.
//
// And the parser underneath is a singleton: utils.ParseISO8601 is called from
// one file in this package. That call is the RFC3339 half of the acceptance
// rule, and a second caller elsewhere in the package would be a second rule —
// which is precisely the shape defect #324 had, since `audit` reached for
// utils.ParseISO8601 directly while `task list` did not.
//
// internal/aihelp's README value gate depends on this too: its `date` check runs
// ParseDateFilter and is only meaningful while ParseDateFilter is what the
// commands actually run.
func TestDateFilters_ParsedThroughOneEntryPoint(t *testing.T) {
	fset := token.NewFileSet()
	files := parsePackageSource(t, fset)

	routed := make(map[string][]string, 8) // flag name -> call sites
	iso := make([]string, 0, 2)            // utils.ParseISO8601 call sites
	calls := 0

	for i := range files {
		name := files[i].name
		ast.Inspect(files[i].file, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			where := fmt.Sprintf("%s:%d", name, fset.Position(call.Pos()).Line)

			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if fn.Name != "ParseDateFilter" {
					return true
				}
				calls++
				flag, literal := stringLiteralOf(call.Args)
				if !literal {
					t.Errorf("%s calls ParseDateFilter with a non-literal flag name; this gate reads "+
						"the flag from the call so the registry and the code can be compared, and a "+
						"computed name makes that comparison blind", where)
					return true
				}
				routed[flag] = append(routed[flag], where)
			case *ast.SelectorExpr:
				pkgIdent, isIdent := fn.X.(*ast.Ident)
				if !isIdent || pkgIdent.Name != "utils" || fn.Sel.Name != "ParseISO8601" {
					return true
				}
				iso = append(iso, where)
			}
			return true
		})
	}

	// The parser underneath must be reached from one place only.
	if len(iso) == 0 {
		t.Fatalf("no call to utils.ParseISO8601 was found in package `commands`; this gate reads the "+
			"acceptance rule through that call, so finding none means it is now blind. Expected exactly "+
			"one, in %s", dateFilterEntryPointFile)
	}
	for _, where := range iso {
		if !strings.HasPrefix(where, dateFilterEntryPointFile+":") {
			t.Errorf("utils.ParseISO8601 is called from %s; the only file in this package allowed to "+
				"decide what a date is is %s. A second caller is a second acceptance rule, which is "+
				"exactly how `audit` came to refuse a form `task list` accepted (#324).",
				where, dateFilterEntryPointFile)
		}
	}

	// And the flags routed through it must be exactly the contract's date flags.
	declared := contractDateFlags()
	if len(declared) == 0 {
		t.Fatal("the registry declares no date-typed flag at all; every comparison below would be " +
			"vacuous")
	}
	if calls < len(declared) {
		t.Errorf("ParseDateFilter is called %d time(s) but the registry declares %d date-typed flag(s) "+
			"(%s); at least one published date flag is parsed some other way",
			calls, len(declared), strings.Join(sortedKeys(declared), ", "))
	}

	for flag := range declared {
		if _, ok := routed[flag]; !ok {
			t.Errorf("the registry types `%s` as a date flag, but nothing in this package parses it "+
				"through ParseDateFilter; it therefore has an acceptance rule of its own, which is the "+
				"defect #324 removed", flag)
		}
	}
	for flag, sites := range routed {
		if _, ok := declared[flag]; !ok {
			t.Errorf("%s parse(s) `%s` through ParseDateFilter, but the registry does not publish that "+
				"flag with type `date`; either the registry entry is wrong or the call is",
				strings.Join(sites, ", "), flag)
		}
	}

	t.Logf("%d ParseDateFilter call site(s) cover the %d date-typed flag(s) the registry publishes "+
		"(%s); utils.ParseISO8601 is reached only from %s",
		calls, len(declared), strings.Join(sortedKeys(declared), ", "), strings.Join(iso, ", "))
}

// parsedSourceFile is one non-test Go file of this package, parsed.
type parsedSourceFile struct {
	file *ast.File
	name string
}

// parsePackageSource parses every non-test Go file of this package. The test
// files are excluded deliberately: this very file names both ParseDateFilter and
// utils.ParseISO8601, and a scan that read itself would report its own
// assertions as call sites and pass whatever the production code did.
func parsePackageSource(t *testing.T, fset *token.FileSet) []parsedSourceFile {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	out := make([]parsedSourceFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		out = append(out, parsedSourceFile{name: name, file: file})
	}
	if len(out) == 0 {
		t.Fatal("no non-test Go file was parsed; the scan below would report a clean package over " +
			"nothing at all")
	}
	return out
}

// contractDateFlags returns every flag spelling the registry publishes with type
// `date`, long and short forms alike. Derived from the registry rather than
// listed, so a new date flag joins the gate by being declared.
func contractDateFlags() map[string]bool {
	out := make(map[string]bool, 8)
	reg := AppRegistry()
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		for j := range cmd.Subcommands {
			flags := cmd.Subcommands[j].Flags
			for k := range flags {
				if flags[k].Type != "date" || flags[k].Long == "" {
					continue
				}
				out[flags[k].Long] = true
			}
		}
	}
	return out
}

// stringLiteralOf returns the value of the first argument when it is an
// unquoted-able string literal.
func stringLiteralOf(args []ast.Expr) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	lit, ok := args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
