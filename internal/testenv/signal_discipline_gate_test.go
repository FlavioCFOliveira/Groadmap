package testenv

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file is the structural half of the regression gate for defect #388: a
// long-lived process killed by the very signal it was meant to drain on.
//
// The defect: `rmp graph serve` and `rmp web` each had to take SIGINT and
// SIGTERM over from the process-wide handler cmd/rmp/main.go installed, and
// each did it with the same three lines of its own —
//
//	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
//	sigCh := make(chan os.Signal, 1)
//	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
//
// — where Reset restores the DEFAULT disposition rather than suspending
// delivery. Measured over 400 process launches, that leaves 41 microseconds at
// the median and 91 at its worst in which SIGTERM terminates the process
// outright, and it drops a signal already queued when Reset unregisters the
// channel. Against the built binary, 100 servers signalled the instant they
// announced their socket produced 7 killed, 18 that never stopped, and 75
// clean drains.
//
// The fix moved the discipline — not merely the constants — into
// internal/signals, which registers once and never unregisters, and swaps the
// ACTION rather than the registration. The behavioural tests beside it and in
// internal/graphserve prove that a signal at the announcement is drained today;
// this gate proves the two things they cannot, because a test can only exercise
// the code it knows about:
//
//  1. that no SECOND signal discipline can appear anywhere in the module
//     without a test failing — including a re-arm inside internal/signals
//     itself, which is where the tempting call sits, and including a
//     registration that is no longer guarded by the sync.Once that makes it
//     happen exactly once;
//  2. that in each surface the take-over still PRECEDES the announcement, which
//     is what makes "this server is ready" mean "this server drains". Nothing
//     about that ordering is enforced by the type system, and reversing it
//     restores a window the behavioural tests would only sometimes catch.
//
// Why here. The sweep spans every package in the module, so no audited package
// is its home, and internal/testenv already hosts the module-wide AST gates
// whose repository walk, skip list and formatting helpers this file reuses
// rather than duplicating.
//
// Known limits, stated rather than papered over:
//
//   - The sweep is syntactic. It follows an aliased import, but a call reached
//     through a function value or through reflection is not seen. Nothing in
//     this project reaches os/signal that way.
//   - It sees production files only. internal/signals/window_test.go uses
//     signal.Reset deliberately, to reproduce the defect this gate guards
//     against; holding a test to this rule would delete the one thing that
//     proves the rule is worth having.
//   - It proves that one package owns the discipline and that the ordering
//     holds at each announcement, not that the drain that follows is correct.
//     SPEC/GRAPH.md § Server Shutdown and the Drain and rmp task #369 own that.

// signalOwnerDir is the one package allowed to touch os/signal in production
// code: the home of the project's single signal discipline.
const signalOwnerDir = "internal/signals"

// forbiddenSignalCalls are the os/signal entry points that UNDO a registration.
// Each of them reopens the interval the defect lived in, so neither is allowed
// anywhere — not even in the owning package, which exists precisely because it
// does not call them.
var forbiddenSignalCalls = map[string]bool{
	"Reset":  true,
	"Stop":   true,
	"Ignore": true,
}

// takeOverSurfaces are the packages that must reach the shared discipline, and
// the announcement in each that the take-over has to precede.
//
// The announcement is the moment a caller learns the process exists and can
// therefore signal it: `rmp graph serve` invokes the Announce callback its
// caller supplied, and `rmp web` prints the URL through the project's one JSON
// writer. Taking the signals over after either would publish a server that can
// still be stopped the wrong way, for as long as the take-over takes — and in
// the web server's case for as long as the browser launch that follows takes,
// which is a process spawn.
var takeOverSurfaces = []struct {
	dir          string
	announcement string // the selector, as written
	why          string
}{
	{
		dir:          "internal/graphserve",
		announcement: "opts.Announce",
		why:          "the socket path `rmp graph serve` prints on stdout",
	},
	{
		dir:          "internal/web",
		announcement: "utils.PrintJSON",
		why:          "the URL `rmp web` prints on stdout, ahead of the browser launch",
	},
}

// signalUse is one production reference to os/signal.
type signalUse struct {
	dir      string
	evidence string // "path:line: expression"
}

// registrationFuncs are the os/signal entry points that INSTALL a handler. Each
// one must sit inside a sync.Once, so that the module's single registration is
// single by construction rather than by the call site being reached once today.
var registrationFuncs = map[string]bool{
	"Notify":        true,
	"NotifyContext": true,
}

func TestOnlyOnePackageOwnsTheSignalDiscipline(t *testing.T) {
	root := repoRoot(t)
	uses, forbidden := scanSignalUses(t, root)

	var trespassers []string
	var ownerSeen bool
	for _, use := range uses {
		if use.dir == signalOwnerDir {
			ownerSeen = true
			continue
		}
		trespassers = append(trespassers, use.evidence)
	}
	sort.Strings(trespassers)

	if len(trespassers) > 0 {
		t.Errorf("%d production reference(s) to os/signal outside %s:\n%s\n\n"+
			"A second signal discipline is what defect #388 was. Take the signals "+
			"over through signals.TakeOver, which swaps the ACTION and leaves the "+
			"one registration in place, instead of registering again here.",
			len(trespassers), signalOwnerDir, indentEvidence(trespassers))
	}

	// Anti-vacuity: if nothing imports os/signal at all, the sweep above is
	// satisfied by a module that has lost its signal handling entirely, and the
	// binary would then die by signal on every SIGINT.
	if !ownerSeen {
		t.Errorf("no production file in %s references os/signal. Either the sweep no "+
			"longer finds what it is looking for, or the process no longer registers "+
			"a handler at all — in which case every rmp invocation is terminated by "+
			"SIGINT instead of exiting %d.", signalOwnerDir, 130)
	}

	if len(forbidden) > 0 {
		sort.Strings(forbidden)
		t.Errorf("%d call(s) to an os/signal function that UNDOES a registration:\n%s\n\n"+
			"Reset, Stop and Ignore each restore a disposition the process does not "+
			"want and leave an interval with no handler at all. internal/signals "+
			"registers once and never unregisters; that is the whole of the repair.",
			len(forbidden), indentEvidence(forbidden))
	}

	unguarded := scanUnguardedRegistrations(t, root)
	if len(unguarded) > 0 {
		sort.Strings(unguarded)
		t.Errorf("%d registration(s) not guarded by a sync.Once:\n%s\n\n"+
			"The one registration has to be single by construction. A Notify reached "+
			"twice adds a second channel and a second reader of the same signal, which "+
			"is the arrangement whose residual FINDING #345 recorded: a signal "+
			"buffered for one reader and acted on by the other.",
			len(unguarded), indentEvidence(unguarded))
	}
}

// scanUnguardedRegistrations returns every production call that INSTALLS a
// signal handler and does not sit inside a `Do(func() { ... })` closure.
//
// The check is lexical containment rather than a proof that the enclosing Do
// belongs to a sync.Once: a syntactic gate cannot resolve the receiver's type,
// and no other Do in this module wraps a closure. What it does establish is the
// property that matters — that the registration cannot be reached a second time
// by a second caller — and it fails loudly if the guard is removed.
func scanUnguardedRegistrations(t *testing.T, root string) []string {
	t.Helper()

	var unguarded []string
	fset := token.NewFileSet()
	walkProductionFiles(t, root, func(path, _ string, file *ast.File) {
		local := importLocalName(file, "os/signal")
		if local == "" {
			return
		}

		var guards []struct{ from, to token.Pos }
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "Do" {
				guards = append(guards, struct{ from, to token.Pos }{call.Pos(), call.End()})
			}
			return true
		})

		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			ident, isIdent := selector.X.(*ast.Ident)
			if !isIdent || ident.Name != local || !registrationFuncs[selector.Sel.Name] {
				return true
			}
			for _, guard := range guards {
				if call.Pos() > guard.from && call.End() < guard.to {
					return true
				}
			}
			position := fset.Position(call.Pos())
			unguarded = append(unguarded, filepath.ToSlash(path)+":"+itoa(position.Line)+": "+
				local+"."+selector.Sel.Name)
			return true
		})
	}, fset)
	return unguarded
}

func TestBothLongLivedSurfacesTakeTheSignalsOverBeforeAnnouncing(t *testing.T) {
	root := repoRoot(t)

	for _, surface := range takeOverSurfaces {
		takeOver, announce := scanTakeOverOrdering(t, root, surface.dir, surface.announcement)

		switch {
		case announce == nil:
			t.Errorf("%s: no production call to %s was found, so this gate cannot say "+
				"anything about the ordering it exists to pin. The announcement is %s; "+
				"if it has been renamed, rename it here too rather than leaving the "+
				"check looking for something that is gone.",
				surface.dir, surface.announcement, surface.why)
		case takeOver == nil:
			t.Errorf("%s announces (%s at %s) without calling signals.TakeOver anywhere. "+
				"Both long-lived surfaces must run on ONE signal discipline: a repair "+
				"applied to one of them leaves the other with the interval defect #388 "+
				"removed.", surface.dir, surface.announcement, announce.where)
		case takeOver.line > announce.line || (takeOver.line == announce.line && takeOver.column > announce.column):
			t.Errorf("%s takes the signals over at %s, AFTER it announces at %s.\n\n"+
				"The announcement is %s. A caller learns the process exists from it and "+
				"can signal it from that instant; until the take-over runs, that signal "+
				"means cmd/rmp/main.go's exit 130 and not a drain. Order the take-over "+
				"first — that is the reachability half of defect #388's repair, and the "+
				"behavioural tests only sometimes catch its absence.",
				surface.dir, takeOver.where, announce.where, surface.why)
		}
	}
}

// callSite is where one call was found.
type callSite struct {
	where  string
	line   int
	column int
}

// scanSignalUses walks the module's production files and returns every
// reference to os/signal, plus the subset that calls a registration-undoing
// function anywhere at all.
func scanSignalUses(t *testing.T, root string) (uses []signalUse, forbidden []string) {
	t.Helper()

	fset := token.NewFileSet()
	walkProductionFiles(t, root, func(path, dir string, file *ast.File) {
		local := importLocalName(file, "os/signal")
		if local == "" {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok || ident.Name != local {
				return true
			}
			position := fset.Position(selector.Pos())
			evidence := filepath.ToSlash(path) + ":" + itoa(position.Line) + ": " +
				local + "." + selector.Sel.Name
			uses = append(uses, signalUse{dir: dir, evidence: evidence})
			if forbiddenSignalCalls[selector.Sel.Name] {
				forbidden = append(forbidden, evidence)
			}
			return true
		})
	}, fset)
	return uses, forbidden
}

// scanTakeOverOrdering finds, within dir's production files, the FIRST call to
// signals.TakeOver and the FIRST call to the named announcement, and returns
// where each is. Both are reported by position rather than merely by presence,
// because presence is not the property: the order is.
func scanTakeOverOrdering(t *testing.T, root, dir, announcement string) (takeOver, announce *callSite) {
	t.Helper()

	wantPkg, wantSel, ok := strings.Cut(announcement, ".")
	if !ok {
		t.Fatalf("the announcement %q is not a selector; this gate compares selectors", announcement)
	}

	fset := token.NewFileSet()
	walkProductionFiles(t, root, func(path, fileDir string, file *ast.File) {
		if fileDir != dir {
			return
		}
		signalsLocal := importLocalName(file, "github.com/FlavioCFOliveira/Groadmap/internal/signals")

		ast.Inspect(file, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			ident, isIdent := selector.X.(*ast.Ident)
			if !isIdent {
				return true
			}
			site := siteAt(fset, path, call.Pos())
			if signalsLocal != "" && ident.Name == signalsLocal && selector.Sel.Name == "TakeOver" {
				takeOver = earlier(takeOver, site)
			}
			if ident.Name == wantPkg && selector.Sel.Name == wantSel {
				announce = earlier(announce, site)
			}
			return true
		})
	}, fset)
	return takeOver, announce
}

// earlier keeps whichever of the two sites comes first in its file, so the
// comparison the gate makes is between the FIRST take-over and the FIRST
// announcement rather than between arbitrary ones.
func earlier(current, candidate *callSite) *callSite {
	if current == nil {
		return candidate
	}
	if candidate.line < current.line ||
		(candidate.line == current.line && candidate.column < current.column) {
		return candidate
	}
	return current
}

func siteAt(fset *token.FileSet, path string, pos token.Pos) *callSite {
	position := fset.Position(pos)
	return &callSite{
		where:  filepath.ToSlash(path) + ":" + itoa(position.Line),
		line:   position.Line,
		column: position.Column,
	}
}

// walkProductionFiles parses every non-test .go file in the module and hands it
// to visit, with the path relative to the module root and the slash-separated
// directory that holds it.
func walkProductionFiles(t *testing.T, root string, visit func(path, dir string, file *ast.File), fset *token.FileSet) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", path, parseErr)
		}
		visit(filepath.ToSlash(rel), filepath.ToSlash(filepath.Dir(rel)), file)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the repository: %v", err)
	}
}
