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
// explicitly and acceptance criterion 5 pins. The one class that opts out of
// that exemption is the numeric range rule, which has no missing-flag message
// for the exemption to protect; see governedRule.flagSpellingIsADefect.

// definitionFile is the one production file allowed to spell a governed message
// template. It is the shared definition itself.
const definitionFile = "internal/utils/fields.go"

// freeTextFragments are the distinguishing words of the message classes the SPEC
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
//
// One such rule has since arrived, and it is NOT in this list: the numeric range
// rule governs the values declaredRangedFields names, not one of which is a
// free-text field and not one of which is in the SPEC table these five classes
// come from, so it carries its own subject set. See numericRangeFragment below.
var freeTextFragments = []string{
	"the value is not valid UTF-8",
	"control characters are not allowed",
	"exceeds maximum length of",
	"cannot be empty",
	"is required",
}

// numericRangeFragment identifies a FIFTH class, which the SPEC section above
// does not govern and which reaches this gate for a different reason.
//
// # What it watches
//
// `priority` and `severity` must lie in 0-9, and that one rule used to announce
// itself in two sentences depending on which command applied it: package models
// refused a value with `priority must be between 0 and 9, got 99`, while a
// generic helper in this package refused the identical value with
// `invalid priority: must be 0-9 (got 99)`. Same rule, same offending value,
// same sentinel, same exit code, two lines — and the specification had begun to
// publish both, which is how such a split stops being a defect and becomes a
// contract (rmp task 318).
//
// Nothing detected it. The wording of a numeric range was in no list here, so
// the second spelling was introduced, tested, and specified without any gate
// noticing. The rule is now worded once, in utils.NumericRangeMessage, and
// listing its distinguishing words here is what makes a THIRD spelling fail
// instead of quietly joining the other two.
//
// # The width of the class, and why it grew
//
// The subject set for this class comes from declaredRangedFields, so the gate
// recognises exactly the names that table declares and no others. That is
// deliberate, and it is the mechanism by which the class widens.
//
// It was `priority` and `severity` alone until rmp task 329, and the note here
// said so: `--limit` was then refused as `limit must be between 1 and N` by
// `task list` and `backlog list` but as `--limit must be between 1 and N (got N)`
// by `audit list`, a live divergence with a task of its own, and widening the
// list to reach it before that task converged it would have failed the build for
// a defect this gate's own task did not fix. Task 329 converged the three onto
// utils.NumericRangeMessage, and the subject set widened exactly as promised: one
// line, FieldListLimit in the declared table, with no fragment and no name
// spelled here.
//
// One thing DID have to change, and it is the axis the promise did not cover.
// Half of what `audit list` got wrong was the `--` prefix, and the hyphen
// exemption this file applies everywhere else would have let that half back in
// unreported. governedRule.flagSpellingIsADefect turns the exemption off for this
// class, and this class only, for the reason recorded there.
//
// It widened a second time, for the same reason and by the same one lever, in
// rmp task 330. The rule that an entity id must lie in 1..MaxInt32 was announced
// in three sentences across four sites — `--entity-id must be between 1 and
// 2147483647 (got 0)` from the audit flag, `invalid entity ID: 0 (must be
// positive)` and `invalid entity ID: N (exceeds maximum value 2147483647)` from
// the shared id validator, which split ONE rule by which bound was crossed, and
// `invalid comment ID: "0" (must be a positive integer no greater than
// 2147483647)` from the four comment subcommands. The five id fields are in the
// declared table now, so all four are one sentence and a fifth is a test failure.
//
// That widening is also what moved `--entity-id` from the must-PASS list below
// to the must-FLAG list: it was a boundary case only while `entity-id` named
// nothing this class governed.
//
// It widened a THIRD time, by the same one lever, in rmp task 338, and that one
// is the clearest statement of what the lever is for. A sprint's `--max-tasks`
// was refused identically by `sprint create` and by `sprint update`, so it was
// never the two-wordings defect tasks 318 and 329 fixed; what it was, was the
// last site still saying `--max-tasks must be between 1 and 10000 (got 0)` after
// every other range had retired the flag prefix and the parentheses. A wording
// that survives only where no task has reached is not a second contract, and
// bringing it under the class is one line in declaredRangedFields.
//
// What is still outside the class is outside it for a reason of its own, and
// neither reason is "no task has reached it yet". `--order` states its bound as
// prose rather than as a range at all; the commit-hash rule words a range over a
// LENGTH in hexadecimal characters, not over the value that broke it; and
// `--port` bounds a flag of `rmp web` that no field of any entity supplies. Each
// is pinned as a must-PASS case below so that the boundary is asserted rather
// than assumed.
const numericRangeFragment = "must be between"

// governedRule is one message class this gate watches: the words that identify
// the class, and the names that may legitimately stand in front of them.
//
// The subject set is per class rather than shared, because the classes govern
// different sets of fields. `priority cannot be empty` is not a defect this gate
// has anything to say about — priority is not a free-text field and cannot be
// empty — and `title must be between 0 and 9` is not one either. Pairing each
// wording with its own fields is what keeps the gate reporting only what it is
// actually able to reason about.
//
// # flagSpellingIsADefect
//
// The general rule of this file is that a name preceded by a hyphen is a FLAG
// and is left alone, because a message about a MISSING or UNKNOWN flag keeps the
// flag's own spelling and SPEC/COMMANDS.md § Published Field Names in Validation
// Messages says so explicitly.
//
// The range class has no such message. Its wording is only ever about a VALUE
// that arrived and fell outside its bounds, so there is no legitimate refusal in
// this class that names a flag, and `--limit must be between 1 and 500 (got 0)`
// is not the flag spelling of a rule — it is the second wording of one, which is
// the very defect rmp task 329 removed. For this class alone, therefore, the
// flag spelling of a subject is itself a subject, so reintroducing it fails
// instead of slipping through the hyphen exemption.
//
// The exemption still applies to every other name: `--entity-id must be between`
// is not matched, because `entity-id` is not one of this class's subjects at all.
type governedRule struct {
	fragment string
	subjects []string
	// flagSpellingIsADefect adds the `--` spelling of every subject to the
	// alternation, instead of exempting it as a flag name.
	flagSpellingIsADefect bool
}

// governedRules is every class the gate watches, built from the declared field
// tables so it can never disagree with them about what a field is called.
var governedRules = buildGovernedRules()

func buildGovernedRules() []governedRule {
	freeText := make([]string, 0, len(declaredFields))
	for _, f := range declaredFields {
		freeText = append(freeText, f.String())
	}
	ranged := make([]string, 0, len(declaredRangedFields))
	for _, f := range declaredRangedFields {
		ranged = append(ranged, f.String())
	}
	freeTextSubjects := spellings(freeText)
	rangedSubjects := spellings(ranged)

	rules := make([]governedRule, 0, len(freeTextFragments)+1)
	for _, fragment := range freeTextFragments {
		rules = append(rules, governedRule{fragment: fragment, subjects: freeTextSubjects})
	}
	rules = append(rules, governedRule{
		fragment:              numericRangeFragment,
		subjects:              rangedSubjects,
		flagSpellingIsADefect: true,
	})
	return rules
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
					"  criterion 6). utils.NumericRangeMessage does the same for the numeric range\n"+
					"  rule over every subject declaredRangedFields names. A constructor added for\n"+
					"  a new class belongs here too.",
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

	for _, rule := range governedRules {
		count := 0
		for _, lit := range literals {
			if strings.Contains(lit.text, rule.fragment) {
				count++
			}
		}
		if count != 1 {
			t.Errorf("the wording %q is spelled %d times in %s, want exactly 1", rule.fragment, count, definitionFile)
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
		// The fifth class, rmp task 318. The first two are the literals package
		// models carried until the rule was factored out; the third is the
		// second wording this gate exists to refuse, in the shape a call site
		// would most plausibly reintroduce it (`invalid <field>: ...`); the
		// fourth is the same defect reached through a generic helper that
		// interpolates the name.
		`priority must be between 0 and 9`,
		`severity must be between 0 and 9`,
		`%w: invalid priority: must be between 0 and 9 (got %d)`,
		`%w: %s must be between %d and %d, got %d`,
		// The same class, widened by rmp task 329. These two are verbatim the
		// literals `task list`/`backlog list` and `audit list` carried until the
		// three converged, and they are the two shapes a call site would most
		// plausibly reintroduce: the bare name, and the flag spelling that the
		// hyphen exemption would otherwise wave through.
		`%w: limit must be between 1 and %d`,
		`%w: --limit must be between 1 and %d (got %d)`,
		// The same class, widened again by rmp task 330 to the five id fields.
		// The first is verbatim the literal `audit list` carried until the flag
		// and the positional converged, and it is the case that proves
		// flagSpellingIsADefect reaches this rule too: it was a must-PASS
		// boundary case on the line above until `entity-id` became a subject.
		// The rest are the shapes a call site would most plausibly reintroduce
		// on each of the other four.
		`%w: --entity-id must be between 1 and %d (got %d)`,
		`%w: entity_id must be between 1 and %d, got %d`,
		`%w: task_id must be between 1 and %d, got %d`,
		`%w: invalid sprint_id: must be between 1 and %d (got %d)`,
		`%w: --comment-id must be between 1 and %d (got %d)`,
		`%w: dependency_task_id must be between 1 and %d, got %d`,
		// The same class, widened a third time by rmp task 338 to a sprint's
		// capacity cap. These are the two directions the task requires proven.
		// The first is VERBATIM the literal both `sprint create` and
		// `sprint update` carried until the rule moved to
		// models.ValidateSprintMaxTasks: the last survivor of the prefixed,
		// parenthesised form, and, like `--entity-id` before it, a must-PASS
		// boundary case on the list below until `max_tasks` became a subject.
		// The second is the bare wording a call site would most plausibly
		// reintroduce having got the prefix right and the sentence wrong.
		`%w: --max-tasks must be between 1 and %d (got %d)`,
		`%w: max_tasks must be between 1 and %d, got %d`,
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
		// The boundaries of the fifth class. The first group is every OTHER
		// numeric range the application words today: each is a live divergence
		// with a task of its own, and this gate must not fail the build for it
		// (see numericRangeFragment for why the scope stops where it does).
		// Note that the flag spellings here are NOT exempted by the hyphen rule
		// — this class opts out of it — they simply name nothing this class
		// governs, which is the boundary being asserted.
		//
		// `--entity-id` was on this list until rmp task 330 converged the id
		// rule, and `--max-tasks` until rmp task 338 converged the capacity
		// cap; both are must-FLAG cases now. What is left is the two rules that
		// are NOT instances of this class: the commit-hash rule words a range
		// over a LENGTH in characters rather than over the value, and `--port`
		// names a flag of `rmp web` that no field of any entity supplies.
		`%w: commit hash must be between %d and %d hexadecimal characters, got %d: %w`,
		`%w: --port must be an integer between %d and %d (got %d)`,
		// And the word boundary on the newest subjects, in both directions: a
		// longer word ENDING in a subject is not that subject, whether it is
		// written with a hyphen or without.
		`%w: rate-limit must be between 1 and %d`,
		`%w: sublimit must be between 1 and %d`,
		`%w: subtask_id must be between 1 and %d`,
		`%w: related-entity-id must be between 1 and %d`,
		`%w: parent_task_id must be between 1 and %d`,
		`%w: sprint_max_tasks must be between 1 and %d`,
		`%w: soft-max-tasks must be between 1 and %d`,
		// And the two directions of the per-class subject sets: a free-text
		// wording about a ranged field, and a range wording about a free-text
		// field. Neither is a message this gate can reason about, and pairing
		// each wording with its own fields is what keeps both out.
		`%w: priority cannot be empty`,
		`%w: title must be between 0 and 9, got %d`,
		// Prose and messages that mention a ranged field without wording its
		// range.
		`%w: task ID(s) and priority required`,
		`Set the new priority (0-9) on each chosen task.`,
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
//
// RangedField is held to the same rule and for the same reason: it is the same
// kind of closed integer enum, over the two fields the numeric range rule
// governs, and a converted integer would render as RangedField(N).
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
			t.Errorf("%s:%d converts a value to a declared field type. Use one of the declared constants.",
				rel, fset.Position(call.Pos()).Line)
			return true
		})
	}
}

// isFieldTypeName reports whether e names one of the closed field enums: `Field`
// or `RangedField` inside package utils, `utils.Field` or `utils.RangedField`
// outside it.
func isFieldTypeName(e ast.Expr) bool {
	named := func(name string) bool {
		return name == "Field" || name == "RangedField"
	}
	switch fun := e.(type) {
	case *ast.Ident:
		return named(fun.Name)
	case *ast.SelectorExpr:
		pkg, ok := fun.X.(*ast.Ident)
		return ok && pkg.Name == "utils" && named(fun.Sel.Name)
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
	matchers := make([]fieldNamingMatcher, 0, len(governedRules))
	for _, rule := range governedRules {
		quoted := make([]string, 0, 2*len(rule.subjects)+1)
		for _, name := range rule.subjects {
			// The flag spelling first, so the alternation prefers the longer
			// match and the failure message names what was actually written.
			if rule.flagSpellingIsADefect {
				quoted = append(quoted, regexp.QuoteMeta("--"+name))
			}
			quoted = append(quoted, regexp.QuoteMeta(name))
		}
		quoted = append(quoted, stringVerbPattern)
		subject := "(" + strings.Join(quoted, "|") + ")"

		matchers = append(matchers, fieldNamingMatcher{
			fragment: rule.fragment,
			// The subject must not be preceded by a word character or a hyphen:
			// a hyphen makes it a flag, and a word character makes it part of a
			// longer word ("subtitle", "somebody").
			re: regexp.MustCompile(`(?:^|[^0-9A-Za-z_-])` + subject + `:? ` + regexp.QuoteMeta(rule.fragment)),
		})
	}
	return matchers
}

// spellings returns every way one of these names could be written in a message:
// the published, underscored name, and the kebab-case spelling of the flag that
// supplies it, which is the spelling the original defect actually used. A name
// with no underscore yields one spelling, not two.
//
// The result is longest first, so an alternation never settles for a shorter
// name that is a prefix of the one actually written.
func spellings(names []string) []string {
	seen := make(map[string]bool, 2*len(names))
	out := make([]string, 0, 2*len(names))
	for _, name := range names {
		for _, spelling := range []string{name, strings.ReplaceAll(name, "_", "-")} {
			if !seen[spelling] {
				seen[spelling] = true
				out = append(out, spelling)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
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
		if strings.HasPrefix(subject, "--") {
			return "it names the flag " + strconv.Quote(subject) + " where the field name belongs, in front of " +
				strconv.Quote(m.fragment)
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
