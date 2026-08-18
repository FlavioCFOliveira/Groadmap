// Package models — specification coverage gates for the audit operation enum.
//
// This file is the specification-side twin of
// internal/commands/help_enum_coverage_test.go. Together the two close one class
// of defect — "the enum grew and a surface that claims to enumerate it did not" —
// from the two directions that surface can face:
//
//  1. TestHelpEnumCoverage_AuditHelpListsEveryOperation (internal/commands)
//     requires every value in ValidAuditOperations to appear in the audit family
//     HELP, so an operation `audit list --operation` accepts is discoverable to
//     a reader.
//  2. TestSpecEnumCoverage_AuditCatalogueListsEveryOperation, below, requires
//     every value to appear in the canonical catalogue of SPEC/DATABASE.md, so
//     an operation the code writes is documented where the specification itself
//     says the catalogue lives.
//
// Both are needed, because the help text and the SPEC are written and maintained
// separately. The shipped instance was in the SPEC alone: TASK_ASSIGN and
// TASK_UNASSIGN were written by `task assign` / `task unassign` and rendered in
// the help, while the section that declares itself canonical — "All other SPEC
// files referencing audit operations MUST link here rather than re-listing" —
// omitted both. A reader who trusted the catalogue concluded the operations did
// not exist.
//
// TestSpecEnumCoverage_AuditEntityTypeCheckMatchesSchema is the other half of the
// same reconciliation: the catalogue's closure argument for the entity_type value
// set rests on the column CHECK, so the CHECK the SPEC prints and the CHECK the
// code executes must not drift apart.
//
// Why this file lives in internal/models and not beside its twin: the edit that
// introduces the defect is made here. Someone adding an AuditOperation constant
// to audit.go gets the failure from the package they just changed, on the first
// `go test ./internal/models/` they run. The twin has no such choice — it calls
// internal/commands' unexported help printers — whereas these gates need no
// unexported access at all, only the exported enum and two files on disk.
package models

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Paths of the two artifacts these gates read, relative to the module root.
const (
	specDatabaseRelPath = "SPEC/DATABASE.md"
	schemaSourceRelPath = "internal/db/schema.go"
)

// The catalogue region inside SPEC/DATABASE.md § `audit` Table. The heading is
// matched first so a line that happens to share a marker's opening elsewhere in
// the file cannot redirect the scan, and each marker must begin a line.
const (
	auditTableHeading    = "### `audit` Table"
	catalogueStartMarker = "**Valid values (validated by application):** This section is the canonical catalogue"
	catalogueEndMarker   = "**Note:** Read operations"
)

// catalogueGroupHeaders are the group labels the region is required to keep.
// Losing one would drop a whole entity's operations while the region still
// parsed, which is the quiet shrinkage these gates exist to make loud.
var catalogueGroupHeaders = []string{"**Tasks:**", "**Sprints:**"}

// catalogueEntry matches one catalogue entry and captures the operation name.
//
// The anchor at the start of the line is load-bearing, not stylistic. Entry
// descriptions cite other operations in backticks — TASK_STATUS_CHANGE's cites
// SPRINT_ADD_TASK and SPRINT_REMOVE_TASK, TASK_ASSIGN's cites TASK_UPDATE — so an
// unanchored search for a backticked uppercase word over-counts the region. The
// anchor also keeps the single-quoted 'TASK_CREATE' literals in the SQL examples
// further down the file out of the parse.
var catalogueEntry = regexp.MustCompile("^- `([A-Z_]+)` - .")

// minCatalogueEntries is the floor below which the parse is treated as evidence
// that the scan has stopped matching rather than as evidence about the SPEC. The
// catalogue holds 29 entries today; the floor sits well under that so a genuine
// removal does not trip it, and far enough over zero that a gate measuring
// nothing cannot report success. The stricter guard is the malformed-entry check
// in the test body, which requires every list item in the region to parse.
const minCatalogueEntries = 20

// auditEntityTypeDDL is the reconciled declaration of the audit table's
// entity_type column, indentation included. SPEC/DATABASE.md and the DDL
// CreateSchema executes must both contain it verbatim.
const auditEntityTypeDDL = "    entity_type TEXT NOT NULL CHECK(entity_type IN ('TASK', 'SPRINT')),"

// auditEntityTypeColumnPrefix identifies a line as that column's declaration
// without assuming it is already correct, so a drifted line is reported as a
// mismatch rather than silently counted as absent.
const auditEntityTypeColumnPrefix = "entity_type TEXT NOT NULL"

// ---------------------------------------------------------------------------
// 1. Every audit operation appears in the canonical catalogue, and the
//    catalogue names no operation the code never writes.
// ---------------------------------------------------------------------------

func TestSpecEnumCoverage_AuditCatalogueListsEveryOperation(t *testing.T) {
	spec := readRepoFile(t, specDatabaseRelPath)
	lines, firstLine := auditCatalogueRegion(t, spec)

	region := strings.Join(lines, "\n")
	for _, header := range catalogueGroupHeaders {
		if !strings.Contains(region, header) {
			t.Fatalf("the audit catalogue region of %s no longer contains the %s group header; the region "+
				"markers still match but its structure has changed, so any coverage measured over it would "+
				"be measured over the wrong text\nregion (lines %d-%d):\n%s",
				specDatabaseRelPath, header, firstLine, firstLine+len(lines)-1, region)
		}
	}

	catalogued := make([]string, 0, len(ValidAuditOperations))
	occurrences := make(map[string]int, len(ValidAuditOperations))
	for offset, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		// Every list item in the region must parse. An item the parser cannot
		// read would otherwise be invisible to both directions below, which is
		// how a scan like this degrades into a no-op one entry at a time.
		match := catalogueEntry.FindStringSubmatch(line)
		if match == nil {
			t.Errorf("%s:%d is a list item inside the audit catalogue that does not have the entry shape "+
				"\"- `OPERATION_NAME` - description\", so this gate cannot see the operation it names: %q",
				specDatabaseRelPath, firstLine+offset, line)
			continue
		}
		name := match[1]
		occurrences[name]++
		if occurrences[name] == 1 {
			catalogued = append(catalogued, name)
		}
	}

	for _, name := range catalogued {
		if occurrences[name] > 1 {
			t.Errorf("the audit catalogue in %s lists %s %d times; a repeated entry makes the catalogue's "+
				"size disagree with the enum's while both coverage directions below still pass",
				specDatabaseRelPath, name, occurrences[name])
		}
	}

	if len(catalogued) < minCatalogueEntries {
		t.Fatalf("only %d operations were parsed out of the audit catalogue at %s:%d-%d, and the enum "+
			"declares %d; the region markers or the entry shape have drifted, so this gate is now measuring "+
			"almost nothing and would pass whatever the SPEC said\nregion:\n%s",
			len(catalogued), specDatabaseRelPath, firstLine, firstLine+len(lines)-1,
			len(ValidAuditOperations), region)
	}

	index := make(map[string]bool, len(catalogued))
	for _, name := range catalogued {
		index[name] = true
	}

	// Direction 1: the code is authoritative, so every constant must be
	// documented. This is the acceptance criterion of the task that added the
	// file: a new AuditOperation constant without its catalogue entry fails here.
	for _, op := range ValidAuditOperations {
		if !index[string(op)] {
			t.Errorf("%s omits %s from the audit catalogue, which that section declares canonical for the "+
				"whole SPEC. The code writes this operation, so the catalogue is incomplete: add "+
				"\"- `%s` - <what writes it>\" to the Tasks or Sprints group",
				specDatabaseRelPath, op, op)
		}
	}

	// Direction 2: without it the SPEC could grow a phantom operation that no
	// code path can produce, and the gate would still be green.
	for _, name := range catalogued {
		if !IsValidAuditOperation(name) {
			t.Errorf("the audit catalogue in %s lists %s, which internal/models does not declare, so no "+
				"code path can ever write it and `audit list --operation %s` is rejected. Either the "+
				"constant was removed and its entry outlived it, or the entry documents an operation that "+
				"was only ever planned", specDatabaseRelPath, name, name)
		}
	}

	// With both directions satisfied and no repeated catalogue entry, the two
	// sets can still differ in size for exactly one reason: ValidAuditOperations
	// listing the same operation twice, which would inflate every count taken
	// from it without changing what the code can write.
	//
	// The t.Failed guard keeps that reasoning true whenever the message is
	// printed. Any failure above already explains the size difference, and
	// repeating it here with a premise that no longer holds would send a reader
	// hunting for a duplicate that is not there.
	if !t.Failed() && len(catalogued) != len(ValidAuditOperations) {
		t.Errorf("the catalogue names %d operations and ValidAuditOperations declares %d; with both "+
			"coverage directions satisfied and no repeated catalogue entry, the difference can only come "+
			"from a duplicate inside ValidAuditOperations", len(catalogued), len(ValidAuditOperations))
	}
}

// auditCatalogueRegion returns the lines of the canonical catalogue region and
// the file line number of the first of them, so failures can name real lines.
//
// Every failure here is fatal. A region this scan cannot locate is not evidence
// that the SPEC is correct; it is evidence that the gate has stopped looking at
// the SPEC, and reporting that as a pass is the failure mode the whole file is
// built to avoid.
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

// ---------------------------------------------------------------------------
// 2. The entity_type CHECK the SPEC prints is the CHECK the code executes.
// ---------------------------------------------------------------------------

// TestSpecEnumCoverage_AuditEntityTypeCheckMatchesSchema pins the constraint the
// catalogue's closure argument rests on. SPEC/DATABASE.md argues that the
// entity_type value set stays exactly TASK and SPRINT because the column carries
// CHECK(entity_type IN ('TASK', 'SPRINT')) — an argument that holds only while
// the storage layer really declares it. If the two drift, the SPEC either
// documents a constraint SQLite does not enforce or hides one it does, and in
// both cases the closure argument becomes an assertion again.
func TestSpecEnumCoverage_AuditEntityTypeCheckMatchesSchema(t *testing.T) {
	sources := []struct {
		label string
		path  string
	}{
		{label: "SPEC/DATABASE.md § `audit` Table", path: specDatabaseRelPath},
		{label: "the DDL CreateSchema executes (internal/db/schema.go)", path: schemaSourceRelPath},
	}

	for _, source := range sources {
		declarations := entityTypeDeclarations(readRepoFile(t, source.path))
		if len(declarations) != 1 {
			t.Errorf("%s declares the audit entity_type column %d times, want exactly 1; with anything "+
				"else there is no single declaration to compare and this check measures nothing\nfound:\n%s",
				source.label, len(declarations), strings.Join(declarations, "\n"))
			continue
		}
		if declarations[0] != auditEntityTypeDDL {
			t.Errorf("%s declares the audit entity_type column as\n  %q\nbut the reconciled declaration is\n"+
				"  %q\nthe specification and the executed DDL must stay byte-identical, or the catalogue's "+
				"argument that the entity_type value set is closed by the table definition no longer holds",
				source.label, declarations[0], auditEntityTypeDDL)
		}
	}
}

// entityTypeDeclarations returns every line of content that declares the audit
// table's entity_type column, indentation preserved. Matching on the trimmed
// prefix rather than on the full declaration is what lets a drifted line be
// reported as a mismatch instead of vanishing into "not found".
func entityTypeDeclarations(content string) []string {
	declarations := make([]string, 0, 2)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), auditEntityTypeColumnPrefix) {
			declarations = append(declarations, line)
		}
	}
	return declarations
}

// ---------------------------------------------------------------------------
// Shared file access.
// ---------------------------------------------------------------------------

// readRepoFile reads a module-root-relative file, failing loudly if it is not
// there. Both gates in this file compare code against text on disk, so an
// unreadable file must never be mistaken for an empty one.
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
// working directory set to that package's own directory — not to the directory
// the `go` command was invoked from — so the root is two levels up from
// internal/models. The go.mod check turns a wrong answer into a failure instead
// of turning these gates into no-ops.
func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s, so the repository root is not where these gates assume it is: %v", root, err)
	}
	return root
}
