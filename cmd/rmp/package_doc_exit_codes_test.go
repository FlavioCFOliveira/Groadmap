package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// cmd/rmp — package-doc exit-code completeness gate (rmp task #299).
//
// main.go opens with a package doc comment that enumerates the exit codes
// the binary can produce. It listed 0 through 6 and stopped there, omitting
// 126, 127 and 130 — three codes the same file declares as constants a few
// lines below and that the process really exits with (127 for a dispatch
// failure, 130 for SIGINT). The comment is go-doc only, never seen by a CLI
// user, which is exactly why it drifted unnoticed while the runtime grew
// new codes.
//
// The gate derives its expectation from the `Exit*` constant block in
// main.go rather than restating the catalogue: the constants are the source
// the exit paths are written against (`os.Exit(ExitSigint)` and friends), and
// SPEC/ARCHITECTURE.md § Exit Code Standards is the specification both sides
// answer to. A test that transcribed the numbers would have to be edited in
// lockstep with the very comment it is supposed to police, and would pass
// for the wrong reason the day a code is added to the constants and to
// nothing else.
//
// Scope note: the plain-text `--help` screens are a different surface with a
// different reader, and are gated end to end against the running binary by
// tests/test_61_family_help_dispatch_exit_code.py.

// The file under inspection is cmd/rmp/main.go, reached through the
// mainSourcePath constant workflow_gates_test.go already declares for the same
// purpose in this package.

// exitConstPrefix marks the identifiers in main.go's exit-code constant block
// (ExitSuccess, ExitFailure, ..., ExitSigint).
const exitConstPrefix = "Exit"

// minExitConstants is a floor on how many exit-code constants the extraction
// must find. Ten are declared today (0-6, 126, 127, 130). Without the floor a
// renamed constant block would make the whole comparison vacuously empty and
// therefore green.
const minExitConstants = 10

// minDocumentedCodes is a slack anti-vacuity floor on the doc-comment side,
// deliberately far below minExitConstants: its only job is to catch an entry
// parser that stopped matching the comment's layout altogether. Setting it at
// the full count would make every genuine omission fail here, hiding WHICH
// code went missing behind a parser-broken message.
const minDocumentedCodes = 3

// docCodeLine matches one code entry of the package doc comment's
// "Exit Codes:" list, which gofmt renders as a tab-indented line inside the
// comment: "\t127 Command not found ...". Anchoring on the leading tab keeps
// a number that merely appears inside a prose sentence from counting as an
// entry.
var docCodeLine = regexp.MustCompile(`(?m)^\t(\d+)\s`)

// TestPackageDoc_ListsEveryExitCodeConstant fails when main.go's package doc
// comment omits any exit code the file's own constant block declares.
func TestPackageDoc_ListsEveryExitCodeConstant(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, mainSourcePath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", mainSourcePath, err)
	}

	declared := exitCodeConstants(t, file)
	if len(declared) < minExitConstants {
		t.Fatalf("found %d %s* constants in %s (%v), want at least %d: the "+
			"constant-block extraction is broken, which would make this gate vacuous",
			len(declared), exitConstPrefix, mainSourcePath, declared, minExitConstants)
	}

	if file.Doc == nil {
		t.Fatalf("%s has no package doc comment; it must enumerate the exit codes "+
			"(SPEC/ARCHITECTURE.md § Exit Code Standards)", mainSourcePath)
	}
	doc := file.Doc.Text()

	documented := make(map[int]bool, len(declared))
	for _, m := range docCodeLine.FindAllStringSubmatch(doc, -1) {
		code, convErr := strconv.Atoi(m[1])
		if convErr != nil {
			continue
		}
		documented[code] = true
	}
	if len(documented) < minDocumentedCodes {
		t.Fatalf("parsed only %d code entries from the package doc comment (%v), "+
			"want at least %d: the entry parser stopped matching the comment's "+
			"layout, which would make this gate vacuous",
			len(documented), documented, minDocumentedCodes)
	}

	for name, code := range declared {
		if !documented[code] {
			t.Errorf("the package doc comment of %s omits exit code %d (%s), which "+
				"the same file declares as a constant and the binary really exits with",
				mainSourcePath, code, name)
		}
	}
}

// exitCodeConstants returns the value of every `Exit*` untyped integer
// constant declared at package level in file, keyed by identifier.
func exitCodeConstants(t *testing.T, file *ast.File) map[string]int {
	t.Helper()
	out := make(map[string]int)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, exitConstPrefix) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					continue
				}
				value, err := strconv.Atoi(lit.Value)
				if err != nil {
					continue
				}
				out[name.Name] = value
			}
		}
	}
	return out
}
