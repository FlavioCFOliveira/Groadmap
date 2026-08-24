package utils

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file is the structural gate for acceptance criterion 6 of
// SPEC/COMMANDS.md § Published Field Names in Validation Messages: every field
// name in a validation message comes from the shared definition, and a test
// fails when a call site introduces a literal of its own.
//
// # What the type already guarantees, and what it cannot
//
// Half of the criterion needs no test at all. Field's underlying type is an
// integer, so a call site that spells a name itself,
//
//	utils.ValidateNoControlChars(value, "functional-requirements")
//
// does not compile: an untyped string constant does not convert to an
// integer-based type. That is stronger than any test — it cannot be forgotten,
// skipped, or made to pass by editing an expectation — and it is why the
// definition is not `type Field string`, which would have accepted that call in
// silence.
//
// What a type cannot stop is a call site that ignores the constructors and
// hand-builds the whole message. That is the hole this gate covers, and it is
// the hole the original defect actually went through: `task edit` built
// "%s cannot be empty" from its own map of column name to FLAG name, so one
// command named one field two ways.
//
// # How a message is recognised
//
// In every message the SPEC section governs, the field name sits immediately
// before the wording of the rule that was broken:
//
//	<field>: the value is not valid UTF-8
//	<field>: control characters are not allowed
//	<field> exceeds maximum length of N characters
//	<field> cannot be empty
//	<field> is required
//
// So the gate looks for exactly that: one of the governed wordings with a field
// name, or a verb that would interpolate one, directly in front of it. That
// adjacency is what separates a message from prose which merely contains the
// same words — the help registry's own description of `comment-edit` says both
// "--body is required" and "The previous body is not retained anywhere", and
// neither is a validation message naming a field.
//
// A name preceded by a hyphen is a FLAG and is left alone: a message about a
// missing or unknown flag keeps the flag's spelling, which the SPEC states
// explicitly and acceptance criterion 5 pins.

// definitionFile is the one production file allowed to spell a governed message
// template. It is the shared definition itself.
const definitionFile = "internal/utils/fields.go"

// governedFragments are the distinguishing words of the message classes the SPEC
// section governs: the encoding refusal, the control-character refusal, the
// length-cap refusal, and the two wordings of the empty/missing-value refusal.
//
// The encoding refusal is the fourth class, added by rmp task 180. Until it
// existed, this list carried a note saying that a rule added later would not be
// listed here and would be bound by the compile-time half alone — and it named
// task 180's UTF-8 refusal as the next such rule. That note was a promise about
// a rule that did not yet exist, and the promise is now due: the rule shipped,
// so it is listed.
//
// Listing it is not a departure from the reasoning that note recorded, it is the
// completion of it. SPEC/COMMANDS.md § Published Field Names in Validation
// Messages provides explicitly for rules added later over the same eight fields,
// and enumerates the classes of message they produce; leaving this one out would
// leave the only governed class the gate does not watch. The compile-time half
// binds a call site that passes a Field, which is most of them — but it says
// nothing about a call site that hand-builds the whole message, and
// `"body: the value is not valid UTF-8"` written out in some future file is
// precisely the defect this gate exists to catch.
//
// A rule added AFTER this one belongs here too, on the same reasoning, together
// with its own entries in TestTheGateDetectsTheDefectItWatchesFor: this gate is
// only worth the classes it is told about.
var governedFragments = []string{
	"the value is not valid UTF-8",
	"control characters are not allowed",
	"exceeds maximum length of",
	"cannot be empty",
	"is required",
}

// stringVerbPattern matches a format verb that interpolates text. A governed
// message carrying one immediately before the rule's wording is building the
// field name from something other than the definition — which is exactly how
// "%s cannot be empty" came to be fed the flag spelling. %w and %d are excluded:
// %w carries the sentinel every one of these messages chains, and %d carries the
// length cap.
const stringVerbPattern = `%[0-9.+#-]*[svq]`

// TestNoValidationMessageIsBuiltFromAFieldNameLiteral is the gate.
func TestNoValidationMessageIsBuiltFromAFieldNameLiteral(t *testing.T) {
	root := repoRoot(t)
	files := productionGoFiles(t, root)

	if len(files) < 20 {
		t.Fatalf("the sweep found only %d production files under %s; it is not looking where it thinks it is",
			len(files), root)
	}

	inspected := 0
	sawDefinition := false
	for _, path := range files {
		rel := relativeTo(t, root, path)
		if rel == definitionFile {
			sawDefinition = true
			continue // the definition is where the templates are allowed to live
		}
		for _, lit := range stringLiterals(t, path) {
			inspected++
			if reason := violation(lit.text); reason != "" {
				t.Errorf("%s:%d names a field inside a validation message: %s\n  literal: %q\n"+
					"  Take the name from the shared definition instead: utils.InvalidUTF8Error,\n"+
					"  utils.ControlCharError, utils.FieldTooLargeError, utils.FieldEmptyError and\n"+
					"  utils.RequiredFieldMessage all take a utils.Field and spell the message once\n"+
					"  (SPEC/COMMANDS.md § Published Field Names in Validation Messages, acceptance\n"+
					"  criterion 6). A constructor added for a new class belongs in this list too.",
					rel, lit.line, reason, lit.text)
			}
		}
	}

	if !sawDefinition {
		t.Fatalf("%s was not among the files swept; the gate would pass no matter what the definition said", definitionFile)
	}
	if inspected < 1000 {
		t.Fatalf("only %d string literals were inspected across the module; the collector is not reading what it thinks it is", inspected)
	}
}

// TestEachGovernedTemplateIsSpelledOnceInTheDefinition is the other half of the
// gate above: that one proves no file names a field, this one proves the
// wordings it searches for still exist, and exist exactly once, where they
// belong. Without it a reworded message would make the sweep pass by finding
// nothing at all.
func TestEachGovernedTemplateIsSpelledOnceInTheDefinition(t *testing.T) {
	path := filepath.Join(repoRoot(t), filepath.FromSlash(definitionFile))

	literals := stringLiterals(t, path)
	if len(literals) == 0 {
		t.Fatalf("%s holds no string literals at all; the definition moved", definitionFile)
	}

	for _, fragment := range governedFragments {
		count := 0
		for _, lit := range literals {
			if strings.Contains(lit.text, fragment) {
				count++
			}
		}
		if count != 1 {
			t.Errorf("the wording %q is spelled %d times in %s, want exactly 1", fragment, count, definitionFile)
		}
	}
}

// TestTheGateDetectsTheDefectItWatchesFor proves the detector can fail without
// depending on the repository containing a defect to find.
//
// The first group is what the codebase actually carried before rmp task 297; the
// second is the messages and the prose that legitimately use the same words
// about something that is not one of the eight fields — a flag, a roadmap name,
// an audit column — and that must keep passing.
func TestTheGateDetectsTheDefectItWatchesFor(t *testing.T) {
	mustFlag := []string{
		`%w: functional-requirements: control characters are not allowed`,
		`%w: acceptance-criteria: control characters are not allowed`,
		`%w: %s cannot be empty`,
		`%w: %s exceeds maximum length of %d characters`,
		`%w: %s: control characters are not allowed`,
		`functional_requirements is required`,
		`title is required`,
		`%w: body exceeds maximum length of %d characters`,
		`%w: completion_summary exceeds maximum length of %d characters`,
		// The fourth class, rmp task 180. The first of these is the literal a
		// future file would most plausibly carry, having copied the message out
		// of the SPEC instead of calling utils.InvalidUTF8Error.
		`%w: body: the value is not valid UTF-8`,
		`%w: %s: the value is not valid UTF-8`,
		`%w: functional-requirements: the value is not valid UTF-8`,
		`completion_summary: the value is not valid UTF-8`,
	}
	for _, text := range mustFlag {
		if violation(text) == "" {
			t.Errorf("the gate accepts %q, which names a field outside the definition", text)
		}
	}

	mustPass := []string{
		`%w: at least one of --type or --body is required`,
		`%w: at least one of --title, --description, --max-tasks or --order is required`,
		`--commit-open is required when transitioning to DOING`,
		`performed_at is required`,
		`roadmap name cannot be empty`,
		`Roadmap name is required`,
		`%w: --functional-requirements`,
		`%w: invalid %s ID: %q (must be a positive integer)`,
		`At least one of --type and --body is required: unlike sprint update, this command ` +
			`does not succeed as a no-op. The previous body is not retained anywhere.`,
		`functional_requirements`,
		`SELECT title, functional_requirements FROM tasks WHERE id = ?`,
		// The two boundaries of the fourth class, matching the ones the older
		// classes are held to: a FLAG keeps its own spelling and is not a field
		// (criterion 5), and a longer word that merely ENDS in a field name is
		// not that field.
		`--body: the value is not valid UTF-8`,
		`somebody: the value is not valid UTF-8`,
	}
	for _, text := range mustPass {
		if reason := violation(text); reason != "" {
			t.Errorf("the gate rejects %q, which names no field in a message: %s", text, reason)
		}
	}
}

// TestNoProductionCodeConvertsAnIntegerToField closes the second half of
// criterion 6 — "or from a name the definition does not contain" — for the one
// expression that could get round the type. Every Field a message uses must be
// one of the declared constants; converting an integer could invent a value the
// definition has no name for, and the message built from it would say Field(N).
func TestNoProductionCodeConvertsAnIntegerToField(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()

	for _, path := range productionGoFiles(t, root) {
		rel := relativeTo(t, root, path)
		if rel == definitionFile {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", rel, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 || !isFieldTypeName(call.Fun) {
				return true
			}
			t.Errorf("%s:%d converts a value to utils.Field. Use one of the declared constants.",
				rel, fset.Position(call.Pos()).Line)
			return true
		})
	}
}

// isFieldTypeName reports whether e names the Field type: `Field` inside package
// utils, `utils.Field` outside it.
func isFieldTypeName(e ast.Expr) bool {
	switch fun := e.(type) {
	case *ast.Ident:
		return fun.Name == "Field"
	case *ast.SelectorExpr:
		pkg, ok := fun.X.(*ast.Ident)
		return ok && pkg.Name == "utils" && fun.Sel.Name == "Field"
	}
	return false
}

// ---------------------------------------------------------------------------
// The detector.
// ---------------------------------------------------------------------------

// fieldNamingMatcher recognises one governed wording with a field name, or a
// verb that would supply one, directly in front of it.
type fieldNamingMatcher struct {
	re       *regexp.Regexp
	fragment string
}

// fieldNamingMatchers is built once from the declared fields, so it can never
// disagree with them about what a field is called.
var fieldNamingMatchers = buildFieldNamingMatchers()

func buildFieldNamingMatchers() []fieldNamingMatcher {
	// Every spelling a field could be written with: the published, underscored
	// name, and the kebab-case spelling of the flag that supplies it, which is
	// the spelling the defect actually used.
	seen := make(map[string]bool, 2*len(declaredFields))
	names := make([]string, 0, 2*len(declaredFields))
	for _, f := range declaredFields {
		for _, spelling := range []string{f.String(), strings.ReplaceAll(f.String(), "_", "-")} {
			if !seen[spelling] {
				seen[spelling] = true
				names = append(names, spelling)
			}
		}
	}
	// Longest first, so an alternation never settles for a shorter name that is
	// a prefix of the one actually written.
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})

	quoted := make([]string, 0, len(names)+1)
	for _, name := range names {
		quoted = append(quoted, regexp.QuoteMeta(name))
	}
	quoted = append(quoted, stringVerbPattern)
	subject := "(" + strings.Join(quoted, "|") + ")"

	matchers := make([]fieldNamingMatcher, 0, len(governedFragments))
	for _, fragment := range governedFragments {
		matchers = append(matchers, fieldNamingMatcher{
			fragment: fragment,
			// The subject must not be preceded by a word character or a hyphen:
			// a hyphen makes it a flag, and a word character makes it part of a
			// longer word ("subtitle", "somebody").
			re: regexp.MustCompile(`(?:^|[^0-9A-Za-z_-])` + subject + `:? ` + regexp.QuoteMeta(fragment)),
		})
	}
	return matchers
}

// violation reports why text is a validation message that names a field itself,
// or "" when it is not one.
func violation(text string) string {
	for _, m := range fieldNamingMatchers {
		hit := m.re.FindStringSubmatch(text)
		if hit == nil {
			continue
		}
		subject := hit[1]
		if strings.HasPrefix(subject, "%") {
			return "it interpolates the field name through " + subject + " in front of " + strconv.Quote(m.fragment)
		}
		return "it spells the field name " + strconv.Quote(subject) + " in front of " + strconv.Quote(m.fragment)
	}
	return ""
}

// ---------------------------------------------------------------------------
// Source collection.
// ---------------------------------------------------------------------------

// sourceString is one string literal, or one concatenation of them, in a file.
type sourceString struct {
	text string
	line int
}

// stringLiterals returns every string literal in path, plus the joined text of
// every `+` concatenation of two or more of them — so splitting a name off into
// its own operand ("functional_requirements" + " is required") does not slip
// past a per-literal check.
func stringLiterals(t *testing.T, path string) []sourceString {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var found []sourceString
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				found = append(found, sourceString{
					text: unquote(node.Value),
					line: fset.Position(node.Pos()).Line,
				})
			}
		case *ast.BinaryExpr:
			if node.Op != token.ADD {
				return true
			}
			if parts := concatenatedLiterals(node); len(parts) > 1 {
				found = append(found, sourceString{
					text: strings.Join(parts, ""),
					line: fset.Position(node.Pos()).Line,
				})
			}
		}
		return true
	})
	return found
}

// concatenatedLiterals flattens a `+` expression to the text of the string
// literals in it, in order. Non-literal operands are skipped rather than
// standing in for something, because a name assembled around one is still a name
// assembled inline.
func concatenatedLiterals(expr ast.Expr) []string {
	switch node := expr.(type) {
	case *ast.BasicLit:
		if node.Kind == token.STRING {
			return []string{unquote(node.Value)}
		}
	case *ast.BinaryExpr:
		if node.Op == token.ADD {
			return append(concatenatedLiterals(node.X), concatenatedLiterals(node.Y)...)
		}
	case *ast.ParenExpr:
		return concatenatedLiterals(node.X)
	}
	return nil
}

// unquote returns a literal's text, falling back to the raw source form when it
// cannot be unquoted, so an unusual literal is inspected rather than skipped.
func unquote(literal string) string {
	if text, err := strconv.Unquote(literal); err == nil {
		return text
	}
	return literal
}

// productionGoFiles lists every non-test .go file in the module. Build
// constraints are ignored on purpose: a file compiled only on another GOOS still
// builds messages, and a second spelling introduced there counts.
func productionGoFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", ".claude", "vendor", "node_modules", "coverage":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(files)
	return files
}

// relativeTo returns path as a slash-separated path relative to root, which is
// what the failure messages and the definition-file comparison use.
func relativeTo(t *testing.T, root, path string) string {
	t.Helper()

	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("relating %s to %s: %v", path, root, err)
	}
	return filepath.ToSlash(rel)
}
