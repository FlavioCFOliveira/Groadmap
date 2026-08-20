// Package aihelp — contract tests for the AuditOperation enum.
//
// These are the contract-side member of the enum-coverage family. The other two
// members ask the same question of the other two surfaces that enumerate audit
// operations:
//
//  1. internal/commands/help_enum_coverage_test.go —
//     TestHelpEnumCoverage_AuditHelpListsEveryOperation requires every operation
//     to appear in the audit family HELP, so a human reader can discover it.
//  2. internal/models/spec_enum_coverage_test.go —
//     TestSpecEnumCoverage_AuditCatalogueListsEveryOperation requires every
//     operation to appear in the canonical catalogue of SPEC/DATABASE.md, so the
//     section that declares itself canonical really is.
//  3. This file requires every operation to appear on the machine-readable
//     contract WITH the catalogue's own description, so an agent that reads only
//     `rmp --ai-help` learns what a human reading the SPEC would learn.
//
// The three are needed together because the three surfaces are written and
// maintained separately, and each has already shipped a gap the other two did
// not have: the SPEC once omitted two operations the contract published (task
// #171 — both have since been retired outright), and the
// contract published all 29 operations with no description at all (task #175).
//
// What the tie below does and does not cover, stated plainly:
//
//   - It covers WORDING. TestGenerate_AuditOperationDescriptionsMatchSpecCatalogue
//     re-derives all 29 descriptions from SPEC/DATABASE.md at test time and
//     compares byte for byte, so the transcription in static.go cannot drift from
//     the catalogue in either direction. Editing the catalogue without editing
//     static.go fails, and the failure prints the exact string to paste.
//   - It does not remove the copy. The binary must describe itself with no
//     repository present, so the contract cannot read the markdown at runtime;
//     the strings are transcribed and the gate pins the transcription. A build
//     from a source tree whose SPEC/DATABASE.md was edited without running the
//     tests would still ship the old wording — the gate makes that a red test,
//     not an impossibility.
//   - It does not cover the HELP text. The audit family help is prose, not a
//     per-operation table, so it is tied to the enum by coverage (member 1) and
//     not by wording. An operation's description can therefore be phrased
//     differently in the help than on the contract; both are required to exist.
//
// The catalogue scanner below is deliberately shaped like the one in
// internal/models/spec_enum_coverage_test.go — same heading, same markers, same
// rule that a region it cannot locate is fatal rather than vacuously green. It
// is not shared with that one because that scanner lives in an in-package test
// file of internal/models, which no other package can import; and it is not
// hoisted into a non-test package because a markdown parser has no business in
// the shipped binary. The duplication is bounded to locating a region, and both
// copies fail loudly rather than silently when the SPEC's structure moves.
package aihelp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The catalogue region inside SPEC/DATABASE.md § `audit` Table. Kept identical
// to the markers in internal/models/spec_enum_coverage_test.go: the heading is
// matched first so a line sharing a marker's opening elsewhere in the file
// cannot redirect the scan, and each marker must begin a line.
const (
	specDatabaseRelPath  = "SPEC/DATABASE.md"
	auditTableHeading    = "### `audit` Table"
	catalogueStartMarker = "**Valid values (validated by application):** This section is the canonical catalogue"
	catalogueEndMarker   = "**Note:** Read operations"
)

// catalogueEntry matches one catalogue entry, capturing the operation name and
// its description. The start-of-line anchor is load-bearing: entry descriptions
// cite other operations in backticks — TASK_STATUS_CHANGE's cites
// SPRINT_ADD_TASK, SPRINT_REMOVE_TASK and SPRINT_DELETE — so an unanchored
// search over the region would over-count.
var catalogueEntry = regexp.MustCompile("^- `([A-Z_]+)` - (.+)$")

// minCatalogueEntries is the floor below which a parse is treated as evidence
// that the scan has stopped matching rather than as evidence about the SPEC. The
// catalogue holds 29 entries today; the floor sits well under that so a genuine
// removal does not trip it, and far enough over zero that a gate measuring
// nothing cannot report success.
const minCatalogueEntries = 20

// ---------------------------------------------------------------------------
// 1. The enum itself: same values, same order, all described.
// ---------------------------------------------------------------------------

// TestGenerate_AuditOperationEnumMatchesModels pins the published AuditOperation
// enum to the canonical slice in internal/models — same values, same order — and
// requires a non-empty description on every one. It is the AuditOperation twin of
// TestGenerate_CommentTypeEnumsMatchModels.
func TestGenerate_AuditOperationEnumMatchesModels(t *testing.T) {
	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))
	values := enumValueList(t, enums, "AuditOperation")

	if len(values) != len(models.ValidAuditOperations) {
		t.Fatalf("enums.AuditOperation publishes %d values, want %d (from models.ValidAuditOperations)",
			len(values), len(models.ValidAuditOperations))
	}

	described := 0
	for i, want := range models.ValidAuditOperations {
		if values[i].value != string(want) {
			t.Errorf("enums.AuditOperation.values[%d].value = %q, want %q (declaration order must match "+
				"internal/models)", i, values[i].value, want)
		}
		if strings.TrimSpace(values[i].description) == "" {
			t.Errorf("enums.AuditOperation.values[%d=%s].description is empty; every operation carries the "+
				"description from the canonical catalogue in %s", i, values[i].value, specDatabaseRelPath)
			continue
		}
		described++
	}

	if described != len(values) {
		t.Errorf("enums.AuditOperation publishes %d values and %d descriptions; the two counts must agree",
			len(values), described)
	}
}

// ---------------------------------------------------------------------------
// 2. The tie: every description is the catalogue's own text.
// ---------------------------------------------------------------------------

// TestGenerate_AuditOperationDescriptionsMatchSpecCatalogue derives the expected
// description of every operation from SPEC/DATABASE.md and requires the contract
// to publish exactly that. This is what stops the contract wording becoming a
// third hand-maintained copy that drifts from the catalogue.
//
// The derivation is mechanical, and documented here because a failure message
// tells the reader to apply it:
//
//	expected = the catalogue entry's text, verbatim, backticks included
//	         + "." when the catalogue entry does not end in one
//	         + auditCommentParentSuffix for the six comment operations
//
// Backticks are kept rather than stripped because the contract's other
// descriptions already use them for command names ("Set automatically by
// `sprint add-tasks`"), so keeping them is what makes the enums map uniform.
func TestGenerate_AuditOperationDescriptionsMatchSpecCatalogue(t *testing.T) {
	catalogue := auditCatalogue(t)

	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))
	values := enumValueList(t, enums, "AuditOperation")

	compared := 0
	for _, v := range values {
		specText, present := catalogue[v.value]
		if !present {
			// The catalogue-coverage direction is owned by
			// TestSpecEnumCoverage_AuditCatalogueListsEveryOperation in
			// internal/models. Repeating its failure here would only duplicate
			// the report, so this gate says what it alone can say: with no
			// catalogue entry there is nothing to compare the wording against.
			t.Errorf("%s has no catalogue entry for %s, so the description the contract publishes is tied "+
				"to nothing; see TestSpecEnumCoverage_AuditCatalogueListsEveryOperation for the coverage "+
				"failure this follows from", specDatabaseRelPath, v.value)
			continue
		}
		want := expectedAuditDescription(models.AuditOperation(v.value), specText)
		if v.description != want {
			t.Errorf("enums.AuditOperation %s publishes\n  %q\nbut %s says it is\n  %q\nthe contract "+
				"transcribes the canonical catalogue verbatim, so update auditOperationDescriptions in "+
				"static.go to the second string (or fix the catalogue, whichever is wrong)",
				v.value, v.description, specDatabaseRelPath, want)
			continue
		}
		compared++
	}

	if compared == 0 {
		t.Fatalf("no description was compared against %s, so this gate measured nothing", specDatabaseRelPath)
	}
	t.Logf("compared %d of %d AuditOperation descriptions against %s", compared, len(values), specDatabaseRelPath)
}

// expectedAuditDescription applies the derivation the test above documents.
func expectedAuditDescription(op models.AuditOperation, specText string) string {
	want := specText
	if !strings.HasSuffix(want, ".") {
		want += "."
	}
	if auditCommentOperations[op] {
		want += auditCommentParentSuffix
	}
	return want
}

// ---------------------------------------------------------------------------
// 3. The six comment operations name the parent entity.
// ---------------------------------------------------------------------------

// TestGenerate_CommentAuditOperationsNameTheParentEntity requires each of the six
// comment operations to state, on the contract, that the entry is recorded
// against the parent entity — the fact an agent most needs and the one the
// operation name hides. TASK_COMMENT_DELETE reads as if it were an operation on a
// comment; it is an operation recorded against the task.
//
// The check is on the emitted contract rather than on the description table, so
// it fails whether the sentence is lost from the catalogue transcription or from
// the suffix that completes it.
func TestGenerate_CommentAuditOperationsNameTheParentEntity(t *testing.T) {
	// Precondition, not decoration. Every assertion below is a substring test
	// against this constant, and strings.Contains(anything, "") is true — so an
	// emptied suffix would make the six positive checks pass vacuously while the
	// converse check at the end reported all 23 other operations. Failing here
	// first turns that into one accurate message instead of 23 wrong ones.
	if auditCommentParentSuffix == "" {
		t.Fatal("auditCommentParentSuffix is empty, so the six comment operations no longer state that the " +
			"comment's own id is never recorded, and every substring assertion in this gate is vacuous")
	}

	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))
	values := enumValueList(t, enums, "AuditOperation")

	descriptions := make(map[string]string, len(values))
	for _, v := range values {
		descriptions[v.value] = v.description
	}

	// The expected parent noun per operation. Keyed by the model constants so a
	// rename is a compile error here too.
	parents := map[models.AuditOperation]string{
		models.OpTaskCommentCreate:   "parent task",
		models.OpTaskCommentUpdate:   "parent task",
		models.OpTaskCommentDelete:   "parent task",
		models.OpSprintCommentCreate: "parent sprint",
		models.OpSprintCommentUpdate: "parent sprint",
		models.OpSprintCommentDelete: "parent sprint",
	}
	if len(parents) != len(auditCommentOperations) {
		t.Fatalf("this gate checks %d comment operations but static.go declares %d; the two lists have "+
			"diverged, so an operation is going unchecked", len(parents), len(auditCommentOperations))
	}

	for op, parent := range parents {
		if !auditCommentOperations[op] {
			t.Errorf("%s is checked here as a comment operation but is absent from auditCommentOperations "+
				"in static.go, so it never receives the parent-entity sentence", op)
		}
		description, present := descriptions[string(op)]
		if !present {
			t.Errorf("%s is absent from the published AuditOperation enum", op)
			continue
		}
		if !strings.Contains(description, parent) {
			t.Errorf("enums.AuditOperation %s does not say it is logged against the %s: %q",
				op, parent, description)
		}
		if !strings.Contains(description, auditCommentParentSuffix) {
			t.Errorf("enums.AuditOperation %s does not carry the sentence stating that the comment's own id "+
				"is never recorded; an agent would look for the entry under the comment id and find nothing: %q",
				op, description)
		}
	}

	// The converse: no non-comment operation may carry the suffix, which would
	// tell an agent that, say, TASK_UPDATE concerns a comment.
	for _, v := range values {
		if auditCommentOperations[models.AuditOperation(v.value)] {
			continue
		}
		if strings.Contains(v.description, auditCommentParentSuffix) {
			t.Errorf("enums.AuditOperation %s carries the comment parent-entity sentence but is not a "+
				"comment operation: %q", v.value, v.description)
		}
	}
}

// ---------------------------------------------------------------------------
// Catalogue access.
// ---------------------------------------------------------------------------

// auditCatalogue parses the canonical catalogue of SPEC/DATABASE.md into
// operation -> description. Every failure is fatal: a catalogue this scan cannot
// read is not evidence that the contract is right, it is evidence that the gate
// has stopped looking at the SPEC.
func auditCatalogue(t *testing.T) map[string]string {
	t.Helper()

	lines, firstLine := auditCatalogueRegion(t, readRepoFile(t, specDatabaseRelPath))

	catalogue := make(map[string]string, len(models.ValidAuditOperations))
	for offset, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		match := catalogueEntry.FindStringSubmatch(line)
		if match == nil {
			// Entries outside the operation catalogue proper (the Entities list,
			// for one) are legitimately shaped differently. Only a line that
			// names an operation this package must describe is a problem.
			continue
		}
		name, description := match[1], match[2]
		if previous, seen := catalogue[name]; seen {
			t.Fatalf("%s:%d lists %s a second time; with two entries there is no single catalogue wording "+
				"to compare the contract against\n  first: %q\n second: %q",
				specDatabaseRelPath, firstLine+offset, name, previous, description)
		}
		catalogue[name] = description
	}

	if len(catalogue) < minCatalogueEntries {
		t.Fatalf("only %d operations were parsed out of the audit catalogue at %s:%d-%d, and the enum "+
			"declares %d; the region markers or the entry shape have drifted, so this gate would now pass "+
			"whatever the SPEC said", len(catalogue), specDatabaseRelPath, firstLine,
			firstLine+len(lines)-1, len(models.ValidAuditOperations))
	}
	return catalogue
}

// auditCatalogueRegion returns the lines of the canonical catalogue region and
// the file line number of the first of them, so failures can name real lines.
func auditCatalogueRegion(t *testing.T, spec string) ([]string, int) {
	t.Helper()

	// Each marker must begin a line. Searching for the marker preceded by a
	// newline enforces that, and the newline prepended here lets a marker at the
	// very start of the file match too.
	doc := "\n" + spec

	heading := strings.Index(doc, "\n"+auditTableHeading+"\n")
	if heading < 0 {
		t.Fatalf("%s has no %q heading, so the audit catalogue cannot be located",
			specDatabaseRelPath, auditTableHeading)
	}

	start := strings.Index(doc[heading:], "\n"+catalogueStartMarker)
	if start < 0 {
		t.Fatalf("%s § %s does not contain a line beginning %q, which opens the canonical catalogue of "+
			"audit operations", specDatabaseRelPath, auditTableHeading, catalogueStartMarker)
	}
	start += heading + 1 // step over the matched newline onto the marker itself

	end := strings.Index(doc[start:], "\n"+catalogueEndMarker)
	if end < 0 {
		t.Fatalf("%s § %s opens the canonical catalogue at line %d but has no following line beginning "+
			"%q, which closes it", specDatabaseRelPath, auditTableHeading,
			strings.Count(doc[:start], "\n"), catalogueEndMarker)
	}

	// doc carries one synthetic leading newline, so counting newlines before the
	// region yields its 1-based line number in the file directly.
	firstLine := strings.Count(doc[:start], "\n")
	return strings.Split(doc[start:start+end], "\n"), firstLine
}

// readRepoFile reads a module-root-relative file, failing loudly if it is not
// there. An unreadable SPEC must never be mistaken for an empty one.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()

	path := filepath.Join(repoRoot(t), filepath.FromSlash(rel))
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(content)
}

// repoRoot returns the module root. `go test` runs a package's tests with the
// working directory set to that package's own directory, so the root is two
// levels up from internal/aihelp. The go.mod check turns a wrong answer into a
// failure instead of turning this gate into a no-op.
//
// This reaches the repository, not the user's home: internal/aihelp is a leaf
// package that opens no roadmap, so it needs no hermetic TestMain and the
// hermeticity gate in internal/testenv does not flag it.
func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s, so this gate is not where it assumes it is: %v", root, err)
	}
	return root
}
