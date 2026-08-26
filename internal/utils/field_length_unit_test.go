package utils

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// This file is the structural gate for the unit Groadmap measures its eight
// free-text fields in.
//
// The defect it exists against, reproduced against the compiled binary before
// rmp task 296:
//
//	rmp task create --title <102 CJK characters> ...
//	Error: field exceeds maximum size: title exceeds maximum length of 255 characters
//
// A title of 102 characters was refused for exceeding a limit of 255, because
// the cap measured its 306 BYTES while the message, SPEC/MODELS.md and
// CHECK(length(title) <= 255) all named CHARACTERS. The comment body, alone
// among the eight, already counted characters — so one codebase carried two
// units for one word, and which one a caller got depended on the field.
//
// The behavioural half of the fix is proved in package commands, over every
// field and every command that writes one
// (internal/commands/field_length_unit_test.go). This half proves the property
// that keeps it fixed: there is exactly one place in the application that
// measures a field's length, so a unit cannot be reintroduced one field at a
// time.

// lengthDefinitionFile is the one production file allowed to turn a measured
// length into the refusal. It is the shared definition, where FieldLength,
// CheckFieldLength and FieldTooLargeError live together.
const lengthDefinitionFile = "internal/utils/fields.go"

// limitConstantNames are the eight maximums, one per free-text field. They live
// in package models, which package utils cannot import — models imports utils —
// so the gate matches them by name in the syntax tree rather than by value. That
// is enough for what it asserts: it is looking for the SHAPE `len(x) > <limit>`,
// not for the number.
var limitConstantNames = map[string]bool{
	"MaxTaskTitle":                  true,
	"MaxTaskFunctionalRequirements": true,
	"MaxTaskTechnicalRequirements":  true,
	"MaxTaskAcceptanceCriteria":     true,
	"MaxTaskCompletionSummary":      true,
	"MaxSprintTitle":                true,
	"MaxSprintDescription":          true,
	"MaxCommentBody":                true,
}

// limitFieldNames are the names a cap carries its maximum under when it reads it
// from a table instead of naming the constant — `f.limit` in the `task edit`
// sweep, `tc.limit` in a driver. A revert there would not mention any constant,
// so the constants alone would not see it.
var limitFieldNames = map[string]bool{
	"limit":     true,
	"Limit":     true,
	"MaxLength": true,
}

// TestOnlyTheDefinitionProducesTheTooLargeRefusal is the gate.
//
// Every "field exceeds maximum size" the user can read is built by
// FieldTooLargeError, and this test proves that after the fix exactly one
// production call site reaches it: CheckFieldLength, inside the definition. A
// cap that goes back to measuring for itself has to call FieldTooLargeError from
// its own file to report anything, and lands here.
//
// The remaining escape — a site that hand-builds the message text instead of
// calling the constructor — is closed by
// TestNoValidationMessageIsBuiltFromAFieldNameLiteral in this same package. The
// two together leave the length refusal with exactly one origin, and that origin
// counts code points.
func TestOnlyTheDefinitionProducesTheTooLargeRefusal(t *testing.T) {
	root := repoRoot(t)
	files := productionGoFiles(t, root)
	if len(files) < 20 {
		t.Fatalf("the sweep found only %d production files under %s; it is not looking where it thinks it is",
			len(files), root)
	}

	sawDefinition := false
	for _, path := range files {
		rel := relativeTo(t, root, path)
		if rel == lengthDefinitionFile {
			sawDefinition = true
			continue
		}
		for _, call := range callsTo(t, path, "FieldTooLargeError") {
			t.Errorf("%s:%d builds the too-large refusal itself. Call utils.CheckFieldLength, "+
				"which measures the value in the unit the message names (code points) and returns "+
				"this error for you (rmp task 296).", rel, call)
		}
	}
	if !sawDefinition {
		t.Fatalf("%s was not among the files swept; the gate would pass no matter what the definition said",
			lengthDefinitionFile)
	}
}

// TestNoProductionCapMeasuresAFieldInBytes is the second half, aimed at the
// exact expression the defect was written as: `len(value) > MaxTaskTitle`.
//
// The gate above would already catch the refusal such a cap has to return, but
// only once it returns one. This one refuses the measurement itself, wherever it
// appears and whatever it does with the answer — including in a file that reads
// its maximum out of a table and so names no constant at all, which is how
// `task edit` carried the defect.
func TestNoProductionCapMeasuresAFieldInBytes(t *testing.T) {
	root := repoRoot(t)

	for _, path := range productionGoFiles(t, root) {
		rel := relativeTo(t, root, path)
		src := parseProduction(t, path)
		for _, line := range byteMeasurementsOfAFieldLimit(src.fset, src.file) {
			t.Errorf("%s:%d measures a free-text field against its maximum with len(), which counts "+
				"BYTES; the maximum is stated in characters (SPEC/MODELS.md) and enforced in characters "+
				"by CHECK(length(<column>) <= N). Use utils.CheckFieldLength (rmp task 296).", rel, line)
		}
	}
}

// TestTheLengthGateDetectsTheDefectItWatchesFor proves both detectors can fail,
// without the repository having to contain a defect for them to find. The first
// group is the code as it stood before rmp task 296; the second is code that
// uses the same words about something that is not one of the eight fields — a
// Cypher query capped in bytes on purpose, a roadmap name, a slice — and that
// must keep passing.
func TestTheLengthGateDetectsTheDefectItWatchesFor(t *testing.T) {
	mustFlag := []string{
		`if len(t.Title) > MaxTaskTitle { return nil }`,
		`if len(s.Description) > MaxSprintDescription { return nil }`,
		`if completionSummary != nil && len(strings.TrimSpace(*completionSummary)) > models.MaxTaskCompletionSummary { return nil }`,
		`if str, ok := updates[f.column].(string); ok && len(strings.TrimSpace(str)) > f.limit { return nil }`,
		`if MaxSprintTitle < len(title) { return nil }`,
	}
	for _, body := range mustFlag {
		if lines := flaggedLinesIn(t, body); len(lines) == 0 {
			t.Errorf("the gate accepts %q, which measures a field's length outside the definition", body)
		}
	}

	mustPass := []string{
		`if len(query) > maxQueryBytes { return nil }`,
		`if len(name) > MaxRoadmapNameLength { return nil }`,
		`if len(cases) != 8 { return nil }`,
		`if len(hash) < MinCommitHashLength || len(hash) > MaxCommitHashLength { return nil }`,
		`if err := utils.CheckFieldLength(t.Title, utils.FieldTaskTitle, MaxTaskTitle); err != nil { return err }`,
		`buf := make([]byte, MaxCommentBody)`,
		// The comment body's own cap as it stood BEFORE this change. Its unit was
		// already right, so this detector must leave it alone; what was wrong with
		// it — that it produced the refusal itself, outside the definition — is
		// what TestOnlyTheDefinitionProducesTheTooLargeRefusal is for.
		`if utf8.RuneCountInString(stored) > MaxCommentBody { return nil }`,
	}
	for _, body := range mustPass {
		if lines := flaggedLinesIn(t, body); len(lines) != 0 {
			t.Errorf("the gate rejects %q, which caps nothing in the wrong unit", body)
		}
	}
}

// TestFieldLengthCountsCodePoints pins the unit itself, in the four scripts the
// acceptance criteria name.
//
// The emoji case is the one a UTF-16 implementation gets wrong: U+1F680 is a
// single code point that occupies four UTF-8 bytes and two UTF-16 units, and it
// must count as one.
func TestFieldLengthCountsCodePoints(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		want  int
	}{
		{"ascii", "reconciliation", 14},
		{"cjk", "資料庫遷移", 5},
		{"emoji beyond the BMP", "\U0001F680\U0001F5C4\U0001F9EA", 3},
		{"accented latin, precomposed", "migração", 8},
		{"empty", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FieldLength(tc.value); got != tc.want {
				t.Errorf("FieldLength(%q) = %d, want %d", tc.value, got, tc.want)
			}
			if tc.value != "" && FieldLength(tc.value) == len(tc.value) && tc.name != "ascii" {
				t.Errorf("FieldLength(%q) = %d equals its byte count; the unit is not code points",
					tc.value, len(tc.value))
			}
		})
	}
}

// TestTheUnitIsCodePointsAndNotGraphemes states the deliberate limit of the
// choice, so that a later reading of "characters" as user-perceived characters
// has to change this test on purpose rather than by accident.
//
// "migração" written with a combining tilde is nine code points and eight
// graphemes. Nine is the answer, and it is the answer SQLite's length() gives
// for the same stored bytes — which is the whole point: Groadmap does not
// normalise free text, so the only count that can agree with the CHECK
// constraint guarding the column is the code-point count.
func TestTheUnitIsCodePointsAndNotGraphemes(t *testing.T) {
	const precomposed = "migração"  // ...ç + U+00E3
	const decomposed = "migração" // ...c + U+0327, a + U+0303

	if got := FieldLength(precomposed); got != 8 {
		t.Errorf("FieldLength(precomposed) = %d, want 8", got)
	}
	if got := FieldLength(decomposed); got != 10 {
		t.Errorf("FieldLength(decomposed) = %d, want 10; code points are the unit, not graphemes", got)
	}
	if precomposed == decomposed {
		t.Fatal("the two spellings are identical; the test proves nothing")
	}
}

// TestCheckFieldLengthAcceptsAtTheLimitAndRefusesOneOver is the boundary, in
// each of the four scripts, against the field with the tightest cap.
func TestCheckFieldLengthAcceptsAtTheLimitAndRefusesOneOver(t *testing.T) {
	const limit = 255

	for _, script := range testenv.LengthProbeScripts() {
		t.Run(script.Name, func(t *testing.T) {
			at := script.Repeat(limit)
			if got := FieldLength(at); got != limit {
				t.Fatalf("the probe is %d code points, want %d; the test would prove nothing", got, limit)
			}
			if err := CheckFieldLength(at, FieldTaskTitle, limit); err != nil {
				t.Errorf("a title of %d %s characters (%d bytes) was refused: %v",
					limit, script.Name, len(at), err)
			}

			over := script.Repeat(limit + 1)
			err := CheckFieldLength(over, FieldTaskTitle, limit)
			if err == nil {
				t.Fatalf("a title of %d %s characters was accepted", limit+1, script.Name)
			}
			if want := FieldTooLargeError(FieldTaskTitle, limit).Error(); err.Error() != want {
				t.Errorf("refusal = %q, want %q", err.Error(), want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The detector.
// ---------------------------------------------------------------------------

type parsedSource struct {
	fset *token.FileSet
	file *ast.File
}

func parseProduction(t *testing.T, path string) parsedSource {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return parsedSource{fset: fset, file: file}
}

// callsTo returns the line of every call to the named function, whether it is
// called unqualified (inside package utils) or through a package selector.
func callsTo(t *testing.T, path, name string) []int {
	t.Helper()

	src := parseProduction(t, path)
	var lines []int
	ast.Inspect(src.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if calleeName(call.Fun) == name {
			lines = append(lines, src.fset.Position(call.Pos()).Line)
		}
		return true
	})
	return lines
}

func calleeName(e ast.Expr) string {
	switch fun := e.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

// byteMeasurementsOfAFieldLimit returns the line of every comparison that
// measures something with len() against one of the eight field maximums.
//
// The measurement is recognised by its two halves standing on opposite sides of
// a comparison, in either order: a call to the builtin len, and a name that is
// one of the eight limit constants or a table column holding one. utf8-based
// counting is recognised too, but only to be left alone — it is the right unit,
// and listing it here is what lets the self-test assert that the OLD comment
// body cap would not have been flagged for its unit.
func byteMeasurementsOfAFieldLimit(fset *token.FileSet, file *ast.File) []int {
	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		expr, ok := n.(*ast.BinaryExpr)
		if !ok || !isComparison(expr.Op.String()) {
			return true
		}
		if (isLenCall(expr.X) && namesAFieldLimit(expr.Y)) ||
			(isLenCall(expr.Y) && namesAFieldLimit(expr.X)) {
			lines = append(lines, fset.Position(expr.Pos()).Line)
		}
		return true
	})
	return lines
}

func isComparison(op string) bool {
	switch op {
	case ">", ">=", "<", "<=", "==", "!=":
		return true
	}
	return false
}

// isLenCall reports whether e is a call to the builtin len. A call to
// utf8.RuneCountInString is deliberately NOT one: it counts the right unit.
func isLenCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "len"
}

// namesAFieldLimit reports whether e names one of the eight maximums, either as
// the declared constant (`MaxTaskTitle`, `models.MaxTaskTitle`) or as the table
// column a sweep reads it from (`f.limit`).
func namesAFieldLimit(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return limitConstantNames[v.Name]
	case *ast.SelectorExpr:
		return limitConstantNames[v.Sel.Name] || limitFieldNames[v.Sel.Name]
	}
	return false
}

// flaggedLinesIn runs the detector over one statement, wrapped in the smallest
// compilable file, so the self-test exercises the same code path the sweep does.
func flaggedLinesIn(t *testing.T, body string) []int {
	t.Helper()

	src := "package p\nfunc f() error {\n" + body + "\nreturn nil\n}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "probe.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing the probe %q: %v", body, err)
	}
	return byteMeasurementsOfAFieldLimit(fset, file)
}
