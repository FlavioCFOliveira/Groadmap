package main

// Regression suite for the arity of the global switch forms.
//
// `rmp help`, `rmp --help`, `rmp -h`, `rmp version`, `rmp --version` and
// `rmp -v` are resolved by the switch in main() BEFORE any command lookup, so
// the shared enforcement point that covers every registered command
// (internal/commands/positional_arity.go, called from
// Command.DispatchFamily) never sees them. Measured before this work:
// `rmp version foo`, `rmp help foo` and `rmp --version foo` all exited 0 and
// discarded the token. SPEC/COMMANDS.md § Positional Arity by Command
// declares a maximum of zero for all six.
//
// Three things are held here, and the third is the one that keeps the other
// two from going stale:
//
//  1. refuseGlobalPositional names the first positional argument and ignores
//     "-"-prefixed tokens, which are flags and not positional arguments.
//  2. Its error reaches exit code 2 through the ordinary handleError path,
//     so the refusal is not a private exit code of its own.
//  3. The set of forms the switch resolves is exactly what the table
//     publishes for the whole-binary forms, minus the two the early-pass
//     ai-help scan answers instead — and EVERY case clause of that switch
//     calls refuseGlobalPositional. A seventh global form added without
//     enforcement fails this test rather than quietly reopening the defect.
//
// The end-to-end behaviour of the six forms, which needs a process because
// main() calls os.Exit, is driven against the binary by
// tests/test_57_positional_arity.py.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"

	"errors"
)

const commandsSpecPath = "../../SPEC/COMMANDS.md"

// ---------------------------------------------------------------------------
// 1. The refusal itself
// ---------------------------------------------------------------------------

func TestRefuseGlobalPositional_NamesTheFirstPositionalArgument(t *testing.T) {
	cases := []struct {
		label string
		rest  []string
		want  string // "" means the invocation must be accepted
	}{
		{"nothing follows the form", nil, ""},
		{"only flags follow", []string{"--colour", "-x"}, ""},
		{"one positional argument", []string{"payment-api"}, "payment-api"},
		{"two positional arguments", []string{"payment-api", "settlement"}, "payment-api"},
		{"a flag then a positional argument", []string{"--colour", "payment-api"}, "payment-api"},
		{"a positional argument then a flag", []string{"payment-api", "--colour"}, "payment-api"},
	}

	for _, c := range cases {
		err := refuseGlobalPositional(c.rest)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: refuseGlobalPositional(%q) = %v, want nil", c.label, c.rest, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: refuseGlobalPositional(%q) = nil, want a refusal naming %q", c.label, c.rest, c.want)
			continue
		}
		want := "invalid input: unexpected argument \"" + c.want + "\""
		if err.Error() != want {
			t.Errorf("%s: message = %q, want %q", c.label, err.Error(), want)
		}
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want it to wrap utils.ErrInvalidInput", c.label, err)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. The exit code, through the production error path
// ---------------------------------------------------------------------------

func TestRefuseGlobalPositional_ExitsTwoAndKeepsStdoutEmpty(t *testing.T) {
	resetHintState()

	var code int
	streams := captureStreams(t, func() {
		code = handleError(refuseGlobalPositional([]string{"payment-api"}))
	})

	if code != ExitMisuse {
		t.Errorf("exit code = %d, want %d (ExitMisuse)", code, ExitMisuse)
	}
	if streams.stdout != "" {
		t.Errorf("a refused invocation wrote to stdout: %q", streams.stdout)
	}
	wantLine := `Error: invalid input: unexpected argument "payment-api"`
	if first := firstLine(streams.stderr); first != wantLine {
		t.Errorf("stderr first line = %q, want %q", first, wantLine)
	}
}

// ---------------------------------------------------------------------------
// 3. The switch is exactly the published set, and all of it is enforced
// ---------------------------------------------------------------------------

// globalSwitchCases reads cmd/rmp/main.go and returns, for the `switch arg`
// statement in main(), each case value together with whether that clause
// calls refuseGlobalPositional.
func globalSwitchCases(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		tag, ok := sw.Tag.(*ast.Ident)
		if !ok || tag.Name != "arg" {
			return true
		}
		for _, stmt := range sw.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			enforced := false
			for _, bodyStmt := range clause.Body {
				ast.Inspect(bodyStmt, func(inner ast.Node) bool {
					if id, ok := inner.(*ast.Ident); ok && id.Name == "refuseGlobalPositional" {
						enforced = true
					}
					return true
				})
			}
			for _, expr := range clause.List {
				lit, ok := expr.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, unquoteErr := strconv.Unquote(lit.Value)
				if unquoteErr != nil {
					t.Fatalf("case value %s is not a quoted string: %v", lit.Value, unquoteErr)
				}
				found[value] = enforced
			}
		}
		return true
	})

	if len(found) == 0 {
		t.Fatal("no `switch arg` case values found in main.go; the gate reads nothing and would pass vacuously")
	}
	return found
}

// publishedGlobalForms reads § Positional Arity by Command and returns the
// forms it publishes with a leading `rmp` — the whole-binary forms — as the
// token that follows `rmp`. The bare `rmp` row contributes nothing, since
// "no arguments" is not a token.
func publishedGlobalForms(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(commandsSpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", commandsSpecPath, err)
	}

	span := regexp.MustCompile("`([^`]*)`")
	var forms []string
	inTable := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "### Positional Arity by Command" {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		if strings.HasPrefix(trimmed, "###") {
			break
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) != 3 {
			continue
		}
		maxValue, convErr := strconv.Atoi(strings.TrimSpace(cells[1]))
		if convErr != nil {
			continue
		}
		for _, match := range span.FindAllStringSubmatch(cells[0], -1) {
			body, isGlobal := strings.CutPrefix(strings.TrimSpace(match[1]), "rmp")
			if !isGlobal {
				continue
			}
			if maxValue != 0 {
				t.Errorf("%s publishes max %d for the global form `rmp%s`; a form resolved before "+
					"command lookup can carry no positional argument", commandsSpecPath, maxValue, body)
			}
			if body = strings.TrimSpace(body); body != "" {
				forms = append(forms, body)
			}
		}
	}

	if len(forms) == 0 {
		t.Fatalf("%s § Positional Arity by Command publishes no global form; the gate reads nothing", commandsSpecPath)
	}
	return forms
}

// TestGlobalSwitch_EnforcesEveryPublishedForm compares the two sides. The
// forms the table publishes and the switch does NOT resolve must be exactly
// the two the early-pass ai-help scan answers instead, and those two are read
// from the scan's own constants rather than retyped here.
func TestGlobalSwitch_EnforcesEveryPublishedForm(t *testing.T) {
	switchCases := globalSwitchCases(t)
	published := publishedGlobalForms(t)

	answeredElsewhere := map[string]string{
		aiHelpFlagToken:    "the early-pass ai-help scan in aihelp_wiring.go",
		aiHelpCommandToken: "the early-pass ai-help scan in aihelp_wiring.go",
	}

	seen := map[string]bool{}
	for _, form := range published {
		seen[form] = true
		if _, resolved := switchCases[form]; resolved {
			continue
		}
		if where, ok := answeredElsewhere[form]; ok {
			t.Logf("`rmp %s` is published with max 0 and is answered by %s", form, where)
			continue
		}
		t.Errorf("%s publishes the global form `rmp %s`, which neither the switch in main.go nor "+
			"the early-pass ai-help scan resolves; its arity is enforced nowhere", commandsSpecPath, form)
	}

	var unpublished []string
	for form := range switchCases {
		if !seen[form] {
			unpublished = append(unpublished, form)
		}
	}
	sort.Strings(unpublished)
	if len(unpublished) > 0 {
		t.Errorf("the switch in main.go resolves %v, which %s § Positional Arity by Command does not "+
			"publish; every whole-binary form must declare an arity", unpublished, commandsSpecPath)
	}
}

// TestGlobalSwitch_EveryCaseEnforcesItsArity is the guard against the defect
// coming back one case at a time: a new global form added to the switch
// without a refuseGlobalPositional call fails here.
func TestGlobalSwitch_EveryCaseEnforcesItsArity(t *testing.T) {
	var unenforced []string
	for value, enforced := range globalSwitchCases(t) {
		if !enforced {
			unenforced = append(unenforced, value)
		}
	}
	sort.Strings(unenforced)
	if len(unenforced) > 0 {
		t.Errorf("case(s) %v of the `switch arg` statement in main.go do not call "+
			"refuseGlobalPositional; each declares a maximum of zero positional arguments "+
			"(SPEC/COMMANDS.md § Positional Arity by Command)", unenforced)
	}
}
