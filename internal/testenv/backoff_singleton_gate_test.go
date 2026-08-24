package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file is the structural half of the regression gate for defect #294
// (acceptance criteria 2 and 4).
//
// The defect: three subsystems each wrote out the project's bounded
// exponential-backoff retry loop for themselves — internal/db's
// retryWithBackoff, internal/commands's openWALWriter and internal/graphlock's
// AcquireShared. Two of them read the specification's "Maximum retries: 5" as
// five ATTEMPTS and guarded their sleep with `attempt < N-1`, so they waited
// four times for 1500 ms where the specification, their own comments and the
// third site all promised five waits for 2500 ms. The three loops were only
// ever compared by eye, and for as long as they were written apart they drifted.
//
// The fix moved the loop — not merely the constants — into internal/backoff, so
// no other package decides how many times or how long to wait. The measured
// tests beside each call site prove that each one currently waits the shared
// 2.5 s; this gate proves something they cannot, because a test can only measure
// the loops it knows about: that no FOURTH copy can appear anywhere in the
// module without a test failing.
//
// It works by the property that makes a backoff a backoff — it blocks for a
// duration. A retry loop that does not sleep is not one, so a production file
// that blocks on time is either the shared policy or a second opinion about it.
// Today the module contains exactly one such call, in internal/backoff.
//
// Why here. The gate spans every package in the module, so no audited package is
// its home, and internal/testenv already hosts the module-wide AST gates
// (hermetic_gate_test.go, engine_constructor_gate_test.go) whose repository
// walk, skip list and formatting helpers this file reuses rather than
// duplicating. internal/testenv also has no first-party dependencies, so this
// audit compiles and runs against a tree in which the audited packages do not.
//
// Known limits, stated rather than papered over:
//
//   - The sweep is syntactic. It matches calls on the local name of the time
//     import, so an aliased import is followed, but a delay reached through a
//     function value, through reflection, or through a third-party package that
//     sleeps internally is not seen. Nothing in this project waits that way.
//   - It sees production files only. Test files sleep freely — several
//     coordinate goroutines that way — and holding them to this rule would say
//     nothing about the binary's behaviour.
//   - It proves that only one package waits, not that the three call sites route
//     through it. That is what the measured tests in internal/db,
//     internal/commands and internal/graphlock establish, each against
//     backoff.Total() rather than against a figure of its own.

// backoffPkgDir is the one package allowed to block on time in production code:
// the home of the project's single retry policy.
const backoffPkgDir = "internal/backoff"

// blockingTimeFuncs are the standard-library entry points by which a production
// file can wait for a duration. Sleep is the one a hand-rolled backoff reaches
// for; the others are here so that rewriting `time.Sleep(d)` as
// `<-time.After(d)` does not walk a second copy of the policy past this gate.
var blockingTimeFuncs = map[string]bool{
	"Sleep":     true,
	"After":     true,
	"Tick":      true,
	"NewTimer":  true,
	"NewTicker": true,
}

// TestOnlyTheBackoffPackageWaits asserts that internal/backoff is the only
// production package in the module that blocks on a duration, so the bounded
// retry policy cannot be written out a second time.
//
// A failure here is not necessarily a bug — a new legitimate delay may exist —
// but it IS a decision that has to be made deliberately rather than by copying a
// loop, which is exactly the review this gate exists to force.
func TestOnlyTheBackoffPackageWaits(t *testing.T) {
	root := repoRoot(t)

	waits := map[string][]string{} // package dir -> "file:line: expression"

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", rel, parseErr)
			return nil
		}

		timeName, imported := timeImportName(file)
		if !imported {
			return nil
		}

		pkgDir := filepath.ToSlash(filepath.Dir(rel))
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !blockingTimeFuncs[selector.Sel.Name] {
				return true
			}
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok || qualifier.Name != timeName {
				return true
			}

			position := fset.Position(call.Pos())
			waits[pkgDir] = append(waits[pkgDir],
				rel+":"+itoa(position.Line)+": "+qualifier.Name+"."+selector.Sel.Name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}

	// The allowed package must still be there. Without this half the gate passes
	// vacuously the day the shared policy is deleted or renamed, which is the
	// most consequential drift of all.
	if len(waits[backoffPkgDir]) == 0 {
		t.Errorf("no production file in %s blocks on time; the shared bounded backoff must live there "+
			"(SPEC/IMPLEMENTATION.md § Retry Logic). If the policy moved, move this gate with it",
			backoffPkgDir)
	}

	offenders := make([]string, 0, len(waits))
	for pkgDir := range waits {
		if pkgDir != backoffPkgDir {
			offenders = append(offenders, pkgDir)
		}
	}
	sort.Strings(offenders)

	for _, pkgDir := range offenders {
		evidence := waits[pkgDir]
		sort.Strings(evidence)
		t.Errorf("package %s blocks on time in production code, but %s is the only package allowed to:\n%s\n"+
			"    A bounded retry belongs in %s, which owns the attempt count and the delay ladder\n"+
			"    (SPEC/IMPLEMENTATION.md § Retry Logic). Writing the loop out again is defect #294:\n"+
			"    two of the three private copies read \"5 retries\" as five attempts and waited four times.\n"+
			"    If this delay is genuinely not a retry, say so here and widen the gate deliberately.",
			pkgDir, backoffPkgDir, indentEvidence(evidence), backoffPkgDir)
	}
}

// timeImportName returns the local name the file uses for the standard library's
// time package, following an alias, and whether the file imports it at all.
func timeImportName(file *ast.File) (string, bool) {
	for _, spec := range file.Imports {
		if spec.Path == nil || spec.Path.Value != `"time"` {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				// A blank import cannot be called through, and a dot import
				// would put Sleep in scope unqualified; neither appears in this
				// module, and neither is matched rather than guessed at.
				return "", false
			}
			return spec.Name.Name, true
		}
		return "time", true
	}
	return "", false
}
