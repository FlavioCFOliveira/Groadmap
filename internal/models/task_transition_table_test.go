package models

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The gates of SPEC/STATE_MACHINE.md § Implementation (rmp task 308): the task
// transition table is declared once, in internal/models/task.go, and
// CanTransitionTo and GetValidTransitions both read that one declaration.

// allTaskStatuses are the five task statuses, in lifecycle order.
var allTaskStatuses = []TaskStatus{StatusBacklog, StatusSprint, StatusDoing, StatusTesting, StatusCompleted}

// TestTransitionTable_CanTransitionToAgreesWithGetValidTransitions settles the
// agreement the SPEC states: for every pair of statuses s and t,
// s.CanTransitionTo(t) is true exactly when t is a member of
// GetValidTransitions(s).
func TestTransitionTable_CanTransitionToAgreesWithGetValidTransitions(t *testing.T) {
	if len(allTaskStatuses) != len(validStatusMap) {
		t.Fatalf("the matrix covers %d statuses but the model defines %d; the matrix is incomplete",
			len(allTaskStatuses), len(validStatusMap))
	}
	accepted := 0
	for _, s := range allTaskStatuses {
		targets := GetValidTransitions(s)
		for _, target := range allTaskStatuses {
			can := s.CanTransitionTo(target)
			if listed := slices.Contains(targets, target); can != listed {
				t.Errorf("%s.CanTransitionTo(%s) = %v but GetValidTransitions(%s) = %v", s, target, can, s, targets)
			}
			if can {
				accepted++
			}
		}
	}
	// The five transitions of SPEC/STATE_MACHINE.md § Implementation; a matrix
	// that accepts nothing would agree vacuously.
	if accepted != 5 {
		t.Errorf("the matrix accepts %d transitions, want the 5 the SPEC lists", accepted)
	}
}

// TestTransitionTable_GetValidTransitionsReturnsACopy pins that a caller who
// changes the slice GetValidTransitions returned changes neither the next
// answer of GetValidTransitions nor what CanTransitionTo accepts.
func TestTransitionTable_GetValidTransitionsReturnsACopy(t *testing.T) {
	got := GetValidTransitions(StatusTesting)
	if !slices.Equal(got, []TaskStatus{StatusDoing, StatusCompleted}) {
		t.Fatalf("GetValidTransitions(TESTING) = %v, want [DOING COMPLETED]", got)
	}
	got[0] = StatusBacklog

	if again := GetValidTransitions(StatusTesting); !slices.Equal(again, []TaskStatus{StatusDoing, StatusCompleted}) {
		t.Errorf("changing the returned slice changed the table: GetValidTransitions(TESTING) is now %v", again)
	}
	if StatusTesting.CanTransitionTo(StatusBacklog) || !StatusTesting.CanTransitionTo(StatusDoing) {
		t.Error("changing the returned slice changed what CanTransitionTo accepts from TESTING")
	}
}

// transitionTableType is the type of the one transition table, as written in
// the source.
const transitionTableType = "map[TaskStatus][]TaskStatus"

// TestTransitionTable_DeclaredOnce is the guard against a second table: across
// every production file of this package there is exactly one composite literal
// of the transition table type, it is the initialiser of taskTransitions in
// task.go, and both CanTransitionTo and GetValidTransitions read taskTransitions.
func TestTransitionTable_DeclaredOnce(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listing the package files: %v", err)
	}
	fset := token.NewFileSet()
	var literals []string
	readers := map[string]bool{}
	var tableFile string
	parsed := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		parsed++

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || lit.Type == nil {
				return true
			}
			if typ := string(src[fset.Position(lit.Type.Pos()).Offset:fset.Position(lit.Type.End()).Offset]); typ == transitionTableType {
				literals = append(literals, fset.Position(lit.Pos()).String())
			}
			return true
		})

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, id := range vs.Names {
						if id.Name == "taskTransitions" {
							tableFile = name
						}
					}
				}
			case *ast.FuncDecl:
				if d.Name.Name != "CanTransitionTo" && d.Name.Name != "GetValidTransitions" {
					continue
				}
				ast.Inspect(d.Body, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok && id.Name == "taskTransitions" {
						readers[d.Name.Name] = true
					}
					return true
				})
			}
		}
	}
	if parsed == 0 {
		t.Fatal("no production file was parsed; the guard checks nothing")
	}

	if len(literals) != 1 {
		t.Errorf("found %d composite literals of %s, want exactly 1 (SPEC/STATE_MACHINE.md § Implementation): %v",
			len(literals), transitionTableType, literals)
	}
	if tableFile != "task.go" {
		t.Errorf("taskTransitions is declared in %q, want task.go", tableFile)
	}
	for _, fn := range []string{"CanTransitionTo", "GetValidTransitions"} {
		if !readers[fn] {
			t.Errorf("%s does not read taskTransitions, the one declaration of the table", fn)
		}
	}
}
