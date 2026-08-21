// Package models — user-documentation coverage gate for the audit operation enum.
//
// This is the fourth member of the enum-coverage family, and the last surface
// that enumerates audit operations to gain a gate. The other three are:
//
//  1. internal/commands/help_enum_coverage_test.go — the audit family HELP must
//     name every operation, so a reader at the terminal can discover it.
//  2. spec_enum_coverage_test.go, beside this file — the canonical catalogue of
//     SPEC/DATABASE.md must name every operation, so the section that declares
//     itself canonical really is.
//  3. internal/aihelp/audit_contract_test.go — `rmp --ai-help` must publish every
//     operation WITH the catalogue's own description, so an agent that reads only
//     the contract learns what a human reading the SPEC would learn.
//
// TestDocsEnumCoverage_AuditCommandDocListsEveryOperation, below, adds the user
// documentation. The four surfaces are written and maintained separately, and
// each has now shipped a gap the others did not have; this one shipped the
// largest. DOCS/commands/audit.md listed 21 of the 43 operations, having never
// been updated after the audit rework replaced the single TASK_STATUS_CHANGE with
// five per-destination operations, the single TASK_UPDATE with five per-field
// ones, and SPRINT_MOVE_TASK with a pair. A reader of the command documentation
// was therefore told that TASK_STATUS_DOING and TASK_STATUS_COMPLETED — the only
// two operations that record a commit hash — did not exist, and was shown a
// filter example naming a LEGACY operation that returns an empty array on every
// roadmap written at schema 1.12.0 or later.
//
// Why it lives in internal/models rather than beside the file it reads: the edit
// that introduces the defect is adding a constant to audit.go. Someone doing that
// gets the failure from the package they just changed, which is the same reason
// the SPEC gate lives here. Both need only the exported enum and a file on disk.
package models

import (
	"strings"
	"testing"
)

// The command documentation and the region inside it that enumerates the
// operations. As in the SPEC gate, the markers must begin a line, and a region
// this scan cannot locate is fatal rather than vacuously green.
const (
	docsAuditRelPath       = "DOCS/commands/audit.md"
	docsOperationsStart    = "**Operation Types:**"
	docsOperationsEnd      = "**Examples:**"
	docsMinOperationsFloor = 20
)

// docsGroupHeaders are the group labels the documented list is required to keep.
// The two that matter most are named explicitly: the status group holds the five
// destination operations, and the legacy group is what stops a reader taking the
// four retired names for live ones.
var docsGroupHeaders = []string{
	"**Task Status Operations:**",
	"**Task Field Operations:**",
	"**Sprint Operations:**",
	"**Legacy Operations:**",
}

func TestDocsEnumCoverage_AuditCommandDocListsEveryOperation(t *testing.T) {
	doc := readRepoFile(t, docsAuditRelPath)
	lines, firstLine := docsOperationsRegion(t, doc)

	region := strings.Join(lines, "\n")
	for _, header := range docsGroupHeaders {
		if !strings.Contains(region, header) {
			t.Fatalf("the operation list of %s no longer contains the %s group header; the region markers "+
				"still match but its structure has changed, so any coverage measured over it would be "+
				"measured over the wrong text\nregion (lines %d-%d):\n%s",
				docsAuditRelPath, header, firstLine, firstLine+len(lines)-1, region)
		}
	}

	documented := make([]string, 0, len(ValidAuditOperations))
	occurrences := make(map[string]int, len(ValidAuditOperations))
	for offset, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		// Every list item in the region must parse, for the same reason the SPEC
		// gate requires it: an item the parser cannot read is invisible to both
		// directions below, which is how a scan like this degrades into a no-op
		// one entry at a time.
		match := catalogueEntry.FindStringSubmatch(line)
		if match == nil {
			t.Errorf("%s:%d is a list item inside the operation list that does not have the entry shape "+
				"\"- `OPERATION_NAME` - description\", so this gate cannot see the operation it names: %q",
				docsAuditRelPath, firstLine+offset, line)
			continue
		}
		name := match[1]
		occurrences[name]++
		if occurrences[name] == 1 {
			documented = append(documented, name)
		}
	}

	for _, name := range documented {
		if occurrences[name] > 1 {
			t.Errorf("%s documents %s %d times; a repeated entry makes the documented set's size disagree "+
				"with the enum's while both coverage directions below still pass",
				docsAuditRelPath, name, occurrences[name])
		}
	}

	if len(documented) < docsMinOperationsFloor {
		t.Fatalf("only %d operations were parsed out of the operation list at %s:%d-%d, and the enum "+
			"declares %d; the region markers or the entry shape have drifted, so this gate is now measuring "+
			"almost nothing and would pass whatever the documentation said\nregion:\n%s",
			len(documented), docsAuditRelPath, firstLine, firstLine+len(lines)-1,
			len(ValidAuditOperations), region)
	}

	index := make(map[string]bool, len(documented))
	for _, name := range documented {
		index[name] = true
	}

	// Direction 1: the code is authoritative, so every constant `audit list
	// --operation` accepts must be documented. This is the defect that shipped.
	for _, op := range ValidAuditOperations {
		if !index[string(op)] {
			t.Errorf("%s omits %s, which `audit list --operation` accepts and the code can write, so a "+
				"reader of the command documentation concludes the operation does not exist: add "+
				"\"- `%s` - <what writes it>\" to the group it belongs to", docsAuditRelPath, op, op)
		}
	}

	// Direction 2: without it the documentation could grow a phantom operation
	// that no code path can produce, and the gate would still be green.
	for _, name := range documented {
		if !IsValidAuditOperation(name) {
			t.Errorf("%s documents %s, which internal/models does not declare, so no code path can ever "+
				"write it and `audit list --operation %s` is rejected with exit code 6",
				docsAuditRelPath, name, name)
		}
	}

	// With both directions satisfied and no repeated entry, the two sets can
	// still differ in size for exactly one reason: ValidAuditOperations listing
	// the same operation twice. The t.Failed guard keeps that reasoning true
	// whenever the message is printed — the same argument the SPEC gate makes.
	if !t.Failed() && len(documented) != len(ValidAuditOperations) {
		t.Errorf("%s documents %d operations and ValidAuditOperations declares %d; with both coverage "+
			"directions satisfied and no repeated entry, the difference can only come from a duplicate "+
			"inside ValidAuditOperations", docsAuditRelPath, len(documented), len(ValidAuditOperations))
	}
}

// docsOperationsRegion returns the lines of the operation list and the file line
// number of the first of them, so failures can name real lines.
//
// Every failure here is fatal, for the reason auditCatalogueRegion states: a
// region this scan cannot locate is not evidence that the documentation is
// correct, it is evidence that the gate has stopped looking at the documentation.
func docsOperationsRegion(t *testing.T, doc string) ([]string, int) {
	t.Helper()

	lines := strings.Split(doc, "\n")

	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, docsOperationsStart) {
			start = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("%s no longer contains a line beginning %q, so the operation list cannot be located and "+
			"this gate has nothing to measure", docsAuditRelPath, docsOperationsStart)
	}

	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], docsOperationsEnd) {
			end = i
			break
		}
	}
	if end == -1 {
		t.Fatalf("%s contains %q at line %d but no %q after it, so the end of the operation list cannot be "+
			"located and the scan would run to the end of the file",
			docsAuditRelPath, docsOperationsStart, start+1, docsOperationsEnd)
	}

	// Line numbers are 1-based for the reader; start+1 is the marker's own line,
	// so the first line of the region proper is start+2.
	return lines[start+1 : end], start + 2
}
