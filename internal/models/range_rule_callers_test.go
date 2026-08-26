package models

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file is the gate for one class of defect: a caller of the priority or
// severity range rule that no invocation of the binary can reach.
//
// The class is not hypothetical. models.TaskUpdate.Validate compared both
// bounds through ValidatePriority and ValidateSeverity, and nothing in
// production called it: its only caller had been db.UpdateTaskStruct, retired
// by task #188 when the command layer replaced the generic task update with one
// audit operation per field. Six tests in internal/commands/boundary_test.go and
// two cases in this package's error_message_dedup_test.go went on asserting
// against it. They measured what they called, but what they called could not
// fail for any reason a user could produce, and anyone changing the rule had to
// keep them in step for nothing (rmp task 332).
//
// So the rule is that every caller of ValidatePriority and ValidateSeverity is
// on a path the binary runs, and each one is listed below with the invocation
// that reaches it. The gate fails both ways: an unlisted caller is a failure,
// and so is a listed caller that has since disappeared — the list may not rot.
//
// The sweep is over the syntax tree of every non-test file in the module, so a
// caller in any package is seen, and a mention in a comment is not: comments
// are not identifiers.

// rangeRuleFunctions are the two functions that compare the bounds. They are
// the ONLY places the bounds are compared (rmp task 318), which is what makes
// their caller set the whole reach of the rule.
var rangeRuleFunctions = []string{"ValidatePriority", "ValidateSeverity"}

// rangeRuleCallers maps every production caller of the two functions above to
// the invocation that reaches it. The key is "<package dir>:<function>", with
// methods qualified by their receiver type so that Task.Validate and a
// same-named method on another type can never be confused for one another —
// the confusion that would let this defect back in under a different receiver.
var rangeRuleCallers = map[string]string{
	"internal/models:Task.Validate": "`rmp task create` validates the fully-built task before the INSERT " +
		"(internal/commands/task_create.go, task.Validate()).",
	"internal/commands:taskSetPriority": "`rmp task prio <ids> <n>` checks the bound before the UPDATE.",
	"internal/commands:taskSetSeverity": "`rmp task sev <ids> <n>` checks the bound before the UPDATE.",
	"internal/commands:taskEdit":        "`rmp task edit <id> -p <n> --severity <n>` checks both bounds before the UPDATE.",
}

// TestRangeRuleHasNoUnreachableCallers is the gate itself.
func TestRangeRuleHasNoUnreachableCallers(t *testing.T) {
	root := rangeRuleModuleRoot(t)
	found := rangeRuleCallSites(t, root)

	if len(found) == 0 {
		t.Fatal("no caller of the range rule found anywhere in the module; the sweep is not looking where it thinks it is")
	}

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if _, listed := rangeRuleCallers[name]; listed {
			continue
		}
		t.Errorf("%s calls %s but is not listed in rangeRuleCallers.\n"+
			"Name the invocation that reaches it and add it, or delete the caller.\n"+
			"A caller no invocation reaches is a validation path that cannot fail for any reason a user "+
			"can produce, and every assertion pinned to it measures nothing (rmp task 332).",
			name, strings.Join(found[name], " and "))
	}

	for name, reachedBy := range rangeRuleCallers {
		if _, still := found[name]; !still {
			t.Errorf("rangeRuleCallers lists %s, which no longer calls the range rule; remove the entry.\n"+
				"The listed invocation was: %s", name, reachedBy)
		}
	}
}

// rangeRuleModuleRoot returns the repository root, two levels above this package.
func rangeRuleModuleRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s; this test assumes internal/models sits two levels below the module root: %v", root, err)
	}
	return root
}

// rangeRuleCallSites returns, for every production function that calls one of
// rangeRuleFunctions, the qualified name of that function mapped to the rule
// functions it calls. Declarations do not count as calls, so the two functions
// do not report themselves.
func rangeRuleCallSites(t *testing.T, root string) map[string][]string {
	t.Helper()

	wanted := make(map[string]bool, len(rangeRuleFunctions))
	for _, name := range rangeRuleFunctions {
		wanted[name] = true
	}

	found := make(map[string][]string)
	fset := token.NewFileSet()

	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", ".claude", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", path, parseErr)
		}

		pkgDir, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			t.Fatalf("locating %s under %s: %v", path, root, relErr)
		}
		pkgDir = filepath.ToSlash(pkgDir)

		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Body == nil {
				continue
			}
			caller := pkgDir + ":" + rangeRuleQualifiedName(fn)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				if name := rangeRuleCalleeName(call.Fun); wanted[name] {
					found[caller] = appendOnce(found[caller], name)
				}
				return true
			})
		}
		return nil
	}

	if err := filepath.WalkDir(root, walk); err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return found
}

// rangeRuleQualifiedName names a function declaration, qualifying a method with
// its receiver type: "Task.Validate" rather than the ambiguous "Validate".
func rangeRuleQualifiedName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}

	expr := fn.Recv.List[0].Type
	if star, isPointer := expr.(*ast.StarExpr); isPointer {
		expr = star.X
	}
	if index, isGeneric := expr.(*ast.IndexExpr); isGeneric {
		expr = index.X
	}
	if ident, isIdent := expr.(*ast.Ident); isIdent {
		return ident.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// rangeRuleCalleeName returns the called function's own name, for both the
// unqualified form used inside this package and the models.X form used outside
// it. Anything else is not a plain function call and returns "".
func rangeRuleCalleeName(fun ast.Expr) string {
	switch called := fun.(type) {
	case *ast.Ident:
		return called.Name
	case *ast.SelectorExpr:
		return called.Sel.Name
	}
	return ""
}

// appendOnce keeps the reported callee list stable and free of repeats when a
// caller checks the same bound twice.
func appendOnce(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	list = append(list, value)
	sort.Strings(list)
	return list
}
