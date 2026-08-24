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

// TestGraphStoreRejectionCarriesRoadmapNameSentinel covers the site the sweep
// for #290 found outside the enum table. openGraphStore validates the roadmap
// name through utils.GetRoadmapDir, whose refusals carry a specific sentinel
// (reserved name, bad characters, too long) on top of utils.ErrValidation. That
// site flattened the chain with %v, which discards the specific sentinel in
// exactly the way %s does.
func TestGraphStoreRejectionCarriesRoadmapNameSentinel(t *testing.T) {
	cases := []struct {
		name         string
		roadmap      string
		wantSentinel error
	}{
		{"reserved device name", "CON", utils.ErrRoadmapNameReserved},
		{"characters outside the regex", "UPPERCASE", utils.ErrInvalidRoadmapName},
		{"longer than the maximum", strings.Repeat("a", utils.MaxRoadmapNameLength+1), utils.ErrRoadmapNameTooLong},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runGraphQuery([]string{"-r", tc.roadmap, "--query", "MATCH (n) RETURN n"})
			if err == nil {
				t.Fatalf("want a rejection for roadmap name %q, got nil", tc.roadmap)
			}
			if !errors.Is(err, tc.wantSentinel) {
				t.Errorf("the roadmap-name sentinel is unreachable through the wrap\n"+
					"       error: %q\n"+
					"        want: errors.Is(err, %v) == true", err, tc.wantSentinel)
			}
			// Same neutrality requirement as the enum sites: still exit 6.
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("classification sentinel lost; error: %q", err)
			}
			if code := exitCodeFor(err); code != 6 {
				t.Errorf("exit code = %d, want 6; error: %q", code, err)
			}
		})
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
