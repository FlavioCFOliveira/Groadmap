package commands

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The defect this file guards (task #290).
//
// A command that rejects an enum value wraps the failure the model raised, so
// the rendered line reads "validation error: invalid task type: \"BOGUS\"".
// Three of the four sites that did this wrote the wrap as
//
//	fmt.Errorf("%w: %s", utils.ErrValidation, parseErr.Error())
//
// Calling .Error() and formatting with %s renders the text but FLATTENS the
// chain: the sentinel models.Parse* had wrapped is turned into a string and
// dropped. errors.Is(err, models.ErrInvalidTaskType) was therefore false even
// when a bad task type was exactly what happened.
//
// The bug was latent rather than visible. The message is right, and the exit
// code stays 6, because that code derives from utils.ErrValidation — which is
// applied at the wrap site with %w and so survives either way. Nothing
// misbehaved. It mattered because any caller needing to DISCRIMINATE the
// failure, rather than merely classify it, could not.
//
// Why the existing tests did not catch it: the assertions next door in
// enum_message_dedup_test.go are about the rendered message, and the message is
// byte-identical under %s and under %w — fmt renders an error operand through
// Error() for both verbs. A message assertion passes in both states. That is
// precisely why the defect survived, so the tests below assert the chain
// instead, which is the only thing the two forms disagree about.

// mapperSentinels is every classification sentinel that cmd/rmp/main.go's
// handleError consults. The exit code of a refusal is decided entirely by which
// of these matches first, so asserting the whole match set — not just the one
// that should match — is what proves the fix left the exit code alone.
//
// The exit code itself is computed by exitCodeFor, shared with
// sprint_update_flag_presence_test.go.
var mapperSentinels = []struct {
	name string
	err  error
}{
	{"utils.ErrUnknownCommand", utils.ErrUnknownCommand},
	{"utils.ErrNotFound", utils.ErrNotFound},
	{"utils.ErrAlreadyExists", utils.ErrAlreadyExists},
	{"utils.ErrNoRoadmap", utils.ErrNoRoadmap},
	{"utils.ErrValidation", utils.ErrValidation},
	{"utils.ErrFieldTooLarge", utils.ErrFieldTooLarge},
	{"utils.ErrInvalidInput", utils.ErrInvalidInput},
	{"utils.ErrRequired", utils.ErrRequired},
}

// TestEnumRejectionsCarrySpecificSentinel is the regression test for #290. It
// asserts that errors.Is reaches the enum-SPECIFIC sentinel through the
// command's wrap, at every site that rejects an enum value.
//
// Reverting any one site to fmt.Errorf("%w: %s", utils.ErrValidation,
// parseErr.Error()) fails this test at that site, while leaving the message
// assertions in enum_message_dedup_test.go green — which is the whole point.
func TestEnumRejectionsCarrySpecificSentinel(t *testing.T) {
	roadmap := "testenumsentinelchain"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	for _, tc := range enumRejectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantSentinel == nil {
				t.Fatalf("case declares no specific sentinel; every enum refusal has one")
			}
			err := tc.run(roadmap)
			if err == nil {
				t.Fatalf("want a rejection, got nil")
			}
			if !errors.Is(err, tc.wantSentinel) {
				t.Errorf("the enum-specific sentinel is unreachable through the wrap\n"+
					"       error: %q\n"+
					"        want: errors.Is(err, %v) == true\n"+
					"        note: wrap with %%w, not %%s + .Error(), or the chain is flattened",
					err, tc.wantSentinel)
			}
		})
	}
}

// TestEnumRejectionsPreserveExitCode pins the behavioural neutrality the fix
// had to have. Carrying the specific sentinel in the chain must not change
// which classification sentinel the exit-code mapper matches: every one of
// these refusals is exit 6 via utils.ErrValidation, and no other mapper
// sentinel may become reachable.
//
// This is the assertion that would catch the one way this change could go
// wrong — a newly exposed sentinel that also satisfies an earlier case in
// handleError's switch and silently moves the refusal to a different exit code.
func TestEnumRejectionsPreserveExitCode(t *testing.T) {
	roadmap := "testenumexitneutral"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	for _, tc := range enumRejectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(roadmap)
			if err == nil {
				t.Fatalf("want a rejection, got nil")
			}
			for _, s := range mapperSentinels {
				want := s.err == utils.ErrValidation
				if got := errors.Is(err, s.err); got != want {
					t.Errorf("classification changed: errors.Is(err, %s) = %v, want %v\n error: %q",
						s.name, got, want, err)
				}
			}
			if code := exitCodeFor(err); code != 6 {
				t.Errorf("exit code = %d, want 6 (SPEC/ARCHITECTURE.md); error: %q", code, err)
			}
		})
	}
}

// roadmapNameRejection is one roadmap name utils.ValidateRoadmapName refuses,
// together with the sentinel naming WHICH of its rules the name broke.
//
// The four names cover the two shapes the refusal takes, because the fix for
// #325 had to be right for both: "CON" and "-mobile-release" are built with
// fmt.Errorf and so render the classification, while "UPPERCASE" and the
// over-long name are utils.MessageError values whose Error() is the SPEC-pinned
// sentence alone, with no classification in it at all. A wrap that restates the
// classification doubles the prefix on the first pair and INVENTS one on the
// second, so a table covering only the first pair would miss half the defect.
type roadmapNameRejection struct {
	name         string
	roadmap      string
	wantSentinel error
}

// roadmapNameRejectionCases is the shared table. The empty name is not listed:
// `-r ""` is refused earlier, by requireRoadmap, as utils.ErrNoRoadmap (exit 3),
// so it never reaches utils.GetRoadmapDir on any family.
func roadmapNameRejectionCases() []roadmapNameRejection {
	return []roadmapNameRejection{
		{"reserved device name", "CON", utils.ErrRoadmapNameReserved},
		{"leading hyphen", "-mobile-release", utils.ErrRoadmapNameStartsWithHyphen},
		{"characters outside the regex", "UPPERCASE", utils.ErrInvalidRoadmapName},
		{"longer than the maximum", strings.Repeat("a", utils.MaxRoadmapNameLength+1), utils.ErrRoadmapNameTooLong},
	}
}

// roadmapEntryPoint is one command path that resolves a roadmap by name and so
// must surface utils.GetRoadmapDir's refusal.
type roadmapEntryPoint struct {
	name string
	run  func(roadmap string) error
}

// roadmapNameEntryPoints spans the families that reach utils.GetRoadmapDir by
// two different routes: the graph family calls it directly from openGraphStore,
// while task, sprint, backlog and audit reach it through db.OpenExisting. Both
// routes refuse the name before touching the filesystem, so these invocations
// create nothing and need no roadmap to exist.
func roadmapNameEntryPoints() []roadmapEntryPoint {
	return []roadmapEntryPoint{
		{"graph query", func(r string) error {
			return runGraphQuery([]string{"-r", r, "--query", "MATCH (n) RETURN n"})
		}},
		{"graph search", func(r string) error {
			return runGraphSearch([]string{"-r", r, "--query", "MATCH p=(a)-[*1..3]-(b) RETURN p"})
		}},
		{"task list", func(r string) error { return HandleTask([]string{"list", "-r", r}) }},
		{"sprint list", func(r string) error { return HandleSprint([]string{"list", "-r", r}) }},
		{"backlog list", func(r string) error { return backlogList([]string{"-r", r}) }},
		{"audit list", func(r string) error { return HandleAudit([]string{"list", "-r", r}) }},
	}
}

// TestGraphStoreRejectionCarriesRoadmapNameSentinel covers the site the sweep
// for #290 found outside the enum table. openGraphStore validates the roadmap
// name through utils.GetRoadmapDir, whose refusals carry a specific sentinel
// (reserved name, leading hyphen, bad characters, too long) on top of
// utils.ErrValidation. That site flattened the chain with %v, which discards the
// specific sentinel in exactly the way %s does.
//
// This is the assertion the fix for #325 had to keep true while removing the
// wrap that carried it: returning the inner error unchanged must leave BOTH
// sentinels reachable, since both were applied by utils.ValidateRoadmapName with
// %w in the first place.
func TestGraphStoreRejectionCarriesRoadmapNameSentinel(t *testing.T) {
	for _, tc := range roadmapNameRejectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			for _, ep := range roadmapNameEntryPoints() {
				err := ep.run(tc.roadmap)
				if err == nil {
					t.Fatalf("%s: want a rejection for roadmap name %q, got nil", ep.name, tc.roadmap)
				}
				if !errors.Is(err, tc.wantSentinel) {
					t.Errorf("%s: the roadmap-name sentinel is unreachable through the wrap\n"+
						"       error: %q\n"+
						"        want: errors.Is(err, %v) == true", ep.name, err, tc.wantSentinel)
				}
				// Same neutrality requirement as the enum sites: still exit 6.
				if !errors.Is(err, utils.ErrValidation) {
					t.Errorf("%s: classification sentinel lost; error: %q", ep.name, err)
				}
				if code := exitCodeFor(err); code != 6 {
					t.Errorf("%s: exit code = %d, want 6; error: %q", ep.name, code, err)
				}
			}
		})
	}
}

// TestRoadmapNameRefusalIsIdenticalAcrossFamilies is the regression test for
// #325.
//
// The defect: openGraphStore restated utils.ErrValidation over an error
// utils.GetRoadmapDir had already classified, so `rmp graph query -r CON` read
//
//	Error: validation error: validation error: "CON": roadmap name is a reserved system name
//
// with the class named twice, while every other family printed it once. The
// same wrap also put a "validation error: " prefix in front of the two
// roadmap-name sentences SPEC/COMMANDS.md § Roadmap Name Validation publishes
// WITHOUT one, so the graph family printed three of the five refusals in words
// the SPEC does not contain.
//
// The assertion is deliberately relational rather than a pinned literal: the
// reference is the error utils.GetRoadmapDir itself returns, so what is proven
// is "every family renders exactly what the producer produced". That survives a
// future rewording of the messages, and it fails the moment any family adds or
// removes a single byte — which is precisely what restating a classification
// does.
func TestRoadmapNameRefusalIsIdenticalAcrossFamilies(t *testing.T) {
	for _, tc := range roadmapNameRejectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			_, refErr := utils.GetRoadmapDir(tc.roadmap)
			if refErr == nil {
				t.Fatalf("utils.GetRoadmapDir(%q) accepted the name; the case is vacuous", tc.roadmap)
			}
			want := refErr.Error()

			for _, ep := range roadmapNameEntryPoints() {
				err := ep.run(tc.roadmap)
				if err == nil {
					t.Fatalf("%s: want a rejection for roadmap name %q, got nil", ep.name, tc.roadmap)
				}
				if got := err.Error(); got != want {
					t.Errorf("%s renders the refusal differently from the producer\n"+
						"        got: %q\n"+
						"       want: %q\n"+
						"       note: a command must not restate a classification the error already carries",
						ep.name, got, want)
				}
			}
		})
	}
}

// TestRoadmapNameRefusalStatesItsClassOnce states the property behind the
// equality above directly, so it keeps holding if the wording of any of the four
// messages is later changed for an unrelated reason.
//
// It asserts two things: that the producer names the class at most once (no
// message may be born doubled), and that no family changes that count on the way
// out. The second half is what fails when the wrap is restored — for "CON" the
// count goes 1 -> 2, and for "UPPERCASE" it goes 0 -> 1.
func TestRoadmapNameRefusalStatesItsClassOnce(t *testing.T) {
	class := utils.ErrValidation.Error()

	for _, tc := range roadmapNameRejectionCases() {
		t.Run(tc.name, func(t *testing.T) {
			_, refErr := utils.GetRoadmapDir(tc.roadmap)
			if refErr == nil {
				t.Fatalf("utils.GetRoadmapDir(%q) accepted the name; the case is vacuous", tc.roadmap)
			}
			wantCount := strings.Count(refErr.Error(), class)
			if wantCount > 1 {
				t.Fatalf("the producer already states %q %d times: %q", class, wantCount, refErr)
			}

			for _, ep := range roadmapNameEntryPoints() {
				err := ep.run(tc.roadmap)
				if err == nil {
					t.Fatalf("%s: want a rejection for roadmap name %q, got nil", ep.name, tc.roadmap)
				}
				if got := strings.Count(err.Error(), class); got != wantCount {
					t.Errorf("%s states %q %d time(s), the producer states it %d time(s)\n line: %q",
						ep.name, class, got, wantCount, err)
				}
			}
		})
	}
}

// TestRefusalsStateTheirClassificationOnce is the standing behavioural sweep for
// the #325 defect class: a command applying a classification sentinel to an
// error that already carries one.
//
// It is the behavioural counterpart of TestNoFmtErrorfFlattensAnErrorChain
// below. That one is structural and catches the #290 signature, which is
// syntactic (X.Error() passed to fmt.Errorf). This one cannot be structural,
// because whether an inner error already carries a classification is not
// decidable from the wrap site's syntax — it depends on what the callee returns.
// So it runs a corpus of real refusals and reads what the user would read: no
// classification from the exit-code mapper's catalogue may be named twice in one
// line.
func TestRefusalsStateTheirClassificationOnce(t *testing.T) {
	roadmap := "testclassificationstatedonce"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	check := func(t *testing.T, label string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: want a refusal, got nil; the case proves nothing", label)
		}
		msg := err.Error()
		for _, s := range mapperSentinels {
			if n := strings.Count(msg, s.err.Error()); n > 1 {
				t.Errorf("%s names the %s classification %d times in one line\n"+
					" line: %q\n"+
					" note: the error being wrapped already carries the class; state it once, at its owner",
					label, s.name, n, msg)
			}
		}
	}

	t.Run("enum refusals", func(t *testing.T) {
		for _, tc := range enumRejectionCases() {
			check(t, tc.name, tc.run(roadmap))
		}
	})

	t.Run("roadmap-name refusals", func(t *testing.T) {
		for _, tc := range roadmapNameRejectionCases() {
			for _, ep := range roadmapNameEntryPoints() {
				check(t, ep.name+" / "+tc.name, ep.run(tc.roadmap))
			}
		}
	})

	t.Run("other refusals", func(t *testing.T) {
		cases := crossFamilyRefusalCases()
		if len(cases) < 10 {
			t.Fatalf("the corpus has shrunk to %d cases; the sweep is losing its reach", len(cases))
		}
		for _, tc := range cases {
			check(t, tc.name, tc.run(roadmap))
		}
	})
}

// crossFamilyRefusalCases is a corpus of refusals reaching the exit-code mapper
// through classifications other than the roadmap-name one, so the sweep above
// covers more of the catalogue than utils.ErrValidation alone. Every entry must
// fail; the sweep asserts that, so a case that silently starts succeeding is a
// test failure rather than a hole.
//
// Nothing here writes: the graph entries are refused by the clause guard rail
// before the store is opened, and the rest fail on missing or unresolvable
// arguments. No entry reads standard input.
func crossFamilyRefusalCases() []enumRejection {
	return []enumRejection{
		{name: "graph create given a read query", run: func(r string) error {
			return runGraphCreate([]string{"-r", r, "--query", "MATCH (n) RETURN n"})
		}},
		{name: "graph query given a write query", run: func(r string) error {
			return runGraphQuery([]string{"-r", r, "--query", "CREATE (s:Spec {key: 'SPEC/GRAPH.md'})"})
		}},
		{name: "graph update given a delete query", run: func(r string) error {
			return runGraphUpdate([]string{"-r", r, "--query", "MATCH (s:Spec) DELETE s"})
		}},
		{name: "graph delete given a set query", run: func(r string) error {
			return runGraphDelete([]string{"-r", r, "--query", "MATCH (s:Spec) SET s.key = 'x'"})
		}},
		{name: "graph query with an unknown flag", run: func(r string) error {
			return runGraphQuery([]string{"-r", r, "--depth", "3", "--query", "MATCH (n) RETURN n"})
		}},
		{name: "task get with no id", run: func(r string) error {
			return HandleTask([]string{"get", "-r", r})
		}},
		{name: "task get with an unknown id", run: func(r string) error {
			return HandleTask([]string{"get", "-r", r, "424242"})
		}},
		{name: "task create with no title", run: func(r string) error {
			return HandleTask([]string{"create", "-r", r, "-y", "BUG"})
		}},
		{name: "task list with an unparsable date", run: func(r string) error {
			return HandleTask([]string{"list", "-r", r, "--created-since", "last Tuesday"})
		}},
		{name: "sprint get with an unknown id", run: func(r string) error {
			return HandleSprint([]string{"get", "-r", r, "424242"})
		}},
		{name: "sprint create with no title", run: func(r string) error {
			return HandleSprint([]string{"create", "-r", r})
		}},
		{name: "audit list with an out-of-range limit", run: func(r string) error {
			return HandleAudit([]string{"list", "-r", r, "--limit", "0"})
		}},
		{name: "audit history with no id", run: func(r string) error {
			return HandleAudit([]string{"history", "-r", r, "TASK"})
		}},
		{name: "roadmap create with no name", run: func(_ string) error {
			return HandleRoadmap([]string{"create"})
		}},
	}
}

// TestNoFmtErrorfFlattensAnErrorChain is the standing sweep. It fails if any
// non-test file in this package passes X.Error() to fmt.Errorf, which is the
// syntactic signature of the #290 defect: rendering an error into a string
// argument necessarily drops whatever sentinels it wrapped.
//
// Asserting this structurally, rather than re-listing the known sites, is what
// stops the defect being reintroduced at a site that does not exist yet.
func TestNoFmtErrorfFlattensAnErrorChain(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package source: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no package parsed; the guard would be vacuous")
	}

	var files int
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			files++
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isFmtErrorf(call) {
					return true
				}
				for _, arg := range call.Args {
					inner, ok := arg.(*ast.CallExpr)
					if !ok || len(inner.Args) != 0 {
						continue
					}
					sel, ok := inner.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Error" {
						continue
					}
					t.Errorf("%s:%d: fmt.Errorf receives %s.Error(), which flattens the "+
						"error chain and discards its sentinels; wrap the error itself with %%w",
						name, fset.Position(inner.Pos()).Line, exprText(sel.X))
				}
				return true
			})
		}
	}

	// Guard against the scan silently reading nothing.
	if files < 10 {
		t.Fatalf("scanned only %d source files; the sweep is not reaching the package", files)
	}
}

// isFmtErrorf reports whether call is a call to fmt.Errorf.
func isFmtErrorf(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errorf" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "fmt"
}

// exprText renders an expression for a diagnostic, falling back to a
// placeholder for shapes with no short textual form.
func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	default:
		return "<expr>"
	}
}
