// Package commands — help-surface enum and inventory coverage gates.
//
// The tests in this file are deliberately generic: each one closes a CLASS of
// "the feature exists but the help does not mention it" defect rather than
// pinning one instance of it.
//
//  1. TestHelpEnumCoverage_AuditHelpListsEveryOperation walks
//     models.ValidAuditOperations and requires each value to appear in the
//     audit family help. The six *_COMMENT_* operations were accepted by
//     `audit list --operation` while being absent from that list, so an agent
//     reading the help concluded they did not exist.
//  2. TestHelpEnumCoverage_FamilyHelpListsEverySubcommand walks the registry
//     and requires every family help to name every subcommand registered under
//     it, which is what makes a new subcommand discoverable to a human reader.
//
// The per-family comment-type blocks are covered separately by
// TestHelpContent_CommentTypeBlockIsPerFamily in help_content_test.go.
//
// (1) has a specification-side twin: TestSpecEnumCoverage_AuditCatalogueListsEveryOperation
// in internal/models/spec_enum_coverage_test.go requires the same enum to be
// covered by the canonical catalogue of SPEC/DATABASE.md. The help and the SPEC
// are written and maintained separately, so neither gate substitutes for the
// other; the twin lives in internal/models because that is where the enum — and
// therefore the edit that introduces the defect — actually is.
//
// The wrapping helper introduced for (1) is unit-tested here too: it is the
// only piece of layout logic in the help layer, so its column arithmetic is
// pinned rather than eyeballed.
package commands

import (
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// helpMaxColumns is the width every rendered help line must respect so the
// output stays readable in an 80-column terminal.
const helpMaxColumns = 79

// ---------------------------------------------------------------------------
// 1. Every audit operation appears in the audit family help.
// ---------------------------------------------------------------------------

func TestHelpEnumCoverage_AuditHelpListsEveryOperation(t *testing.T) {
	out := captureStdout(t, printAuditHelp)

	block, ok := sectionBody(out, "Valid operations (for --operation filter):")
	if !ok {
		t.Fatalf("audit help has no 'Valid operations' block:\n%s", out)
	}

	for _, op := range models.ValidAuditOperations {
		if !strings.Contains(block, string(op)) {
			t.Errorf("audit --help 'Valid operations' block omits %s; `audit list --operation %s` "+
				"is accepted by the command, so the help must publish it\nblock:\n%s", op, op, block)
		}
	}

	// The block is rendered, not typed: nothing outside the enum may appear in
	// its list column. This catches a stale hand-edit surviving in the
	// template, which is the other half of the same defect — a help that names
	// an operation the command would reject.
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		list := line
		// A label line carries "<group label>:" before the list; continuation
		// lines carry only the list. No operation name contains a colon, so
		// cutting at the last one isolates the list column whatever the labels
		// are renamed to.
		if idx := strings.LastIndex(list, ":"); idx >= 0 {
			list = list[idx+1:]
		}
		for _, field := range strings.FieldsFunc(list, func(r rune) bool { return r == ' ' || r == ',' }) {
			if !models.IsValidAuditOperation(field) {
				t.Errorf("audit --help 'Valid operations' block lists %q, which is not a valid audit operation", field)
			}
		}
	}
}

// sectionBody returns the lines that follow heading up to the next blank line.
func sectionBody(out, heading string) (string, bool) {
	idx := strings.Index(out, heading)
	if idx < 0 {
		return "", false
	}
	rest := out[idx+len(heading):]
	rest = strings.TrimPrefix(rest, "\n")
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		rest = rest[:end]
	}
	return rest, true
}

// ---------------------------------------------------------------------------
// 2. Every family help names every subcommand registered under it.
// ---------------------------------------------------------------------------

func TestHelpEnumCoverage_FamilyHelpListsEverySubcommand(t *testing.T) {
	reg := AppRegistry()
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		if cmd.Name == "ai-help" || !cmd.HasSubcommand || cmd.HelpPrinter == nil {
			continue
		}
		out := captureStdout(t, func() {
			_ = cmd.DispatchFamily([]string{"--help"})
		})
		inventory, ok := subcommandInventory(out)
		if !ok {
			t.Errorf("rmp %s --help has no 'Commands:' / 'Subcommands:' inventory block", cmd.Name)
			continue
		}
		for j := range cmd.Subcommands {
			name := cmd.Subcommands[j].Name
			if name == "" {
				continue
			}
			if !inventoryLists(inventory, name) {
				t.Errorf("the inventory block of `rmp %s --help` does not list the %q subcommand; a "+
					"subcommand absent from its family help is undiscoverable to a reader\nblock:\n%s",
					cmd.Name, name, inventory)
			}
		}
	}
}

// subcommandInventory returns the indented list that follows the family help's
// subcommand heading. Two spellings are in use — "Commands:" in most families
// and "Subcommands:" in graph — and both are accepted; the block ends at the
// first blank line.
func subcommandInventory(out string) (string, bool) {
	// "Subcommands:" is tried first because it contains "Commands:" as a
	// substring, so the shorter heading would otherwise match inside it.
	for _, heading := range []string{"Subcommands:", "Commands:"} {
		if body, ok := sectionBody(out, heading); ok {
			return body, true
		}
	}
	return "", false
}

// inventoryLists reports whether name opens one of the inventory's entries.
// Matching on the leading token, rather than anywhere in the help text, is what
// makes the assertion meaningful: every comment subcommand name also occurs in
// the prose and in the output table, so a substring search over the whole help
// stays green even when the inventory entry is deleted.
func inventoryLists(inventory, name string) bool {
	for _, line := range strings.Split(inventory, "\n") {
		entry := strings.TrimSpace(line)
		if !strings.HasPrefix(entry, name) {
			continue
		}
		rest := entry[len(name):]
		if rest == "" || rest[0] == ' ' || rest[0] == ',' {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Layout helper: wrapEnumList / labelCell / auditOperationBlock.
// ---------------------------------------------------------------------------

func TestAuditOperationBlock_LayoutIsAlignedAndWrapped(t *testing.T) {
	block := auditOperationBlock()
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("auditOperationBlock produced %d lines, want at least one per group:\n%s", len(lines), block)
	}

	labelWidth := -1
	for _, line := range lines {
		if strings.ContainsRune(line, '\t') {
			t.Errorf("auditOperationBlock emitted a hard TAB: %q", line)
		}
		if len(line) > helpMaxColumns {
			t.Errorf("line exceeds %d columns (%d): %q", helpMaxColumns, len(line), line)
		}
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("line is not indented into the block: %q", line)
			continue
		}
		// The operation list must start at the same column on every line,
		// label lines and continuation lines alike.
		col := strings.IndexFunc(line[2:], func(r rune) bool { return r != ' ' })
		if col < 0 {
			t.Errorf("blank line inside the block: %q", line)
			continue
		}
		if auditGroupLabelled(line) {
			// Label line: the list starts after the padded label.
			start := strings.Index(line, ":") + 1
			for start < len(line) && line[start] == ' ' {
				start++
			}
			if labelWidth < 0 {
				labelWidth = start
			} else if start != labelWidth {
				t.Errorf("label line starts its list at column %d, want %d: %q", start, labelWidth, line)
			}
			continue
		}
		if labelWidth >= 0 && 2+col != labelWidth {
			t.Errorf("continuation line starts at column %d, want %d (aligned under the label): %q", 2+col, labelWidth, line)
		}
	}
}

// TestWrapEnumList_Empty pins the empty-input contract: an empty group must
// contribute no line at all, which is what keeps the "Other ops:" fallback
// invisible while every operation matches a known prefix.
func TestWrapEnumList_Empty(t *testing.T) {
	if got := wrapEnumList("  Other ops: ", "             ", nil); got != "" {
		t.Errorf("wrapEnumList with no names = %q, want empty string", got)
	}
}

// TestWrapEnumList_WrapsWithoutLosingNames feeds a list long enough to wrap
// several times and verifies that every name survives, that commas separate
// them, and that no line exceeds the column budget.
func TestWrapEnumList_WrapsWithoutLosingNames(t *testing.T) {
	names := []string{
		"SPRINT_TASK_MOVE_POSITION", "SPRINT_REORDER_TASKS", "SPRINT_COMMENT_CREATE",
		"SPRINT_COMMENT_UPDATE", "SPRINT_COMMENT_DELETE", "SPRINT_MOVE_TASK",
	}
	const label = "  Sprint ops: "
	indent := strings.Repeat(" ", len(label))

	got := wrapEnumList(label, indent, names)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected the list to wrap over several lines, got %d:\n%s", len(lines), got)
	}
	for _, line := range lines {
		if len(line) > helpMaxColumns {
			t.Errorf("line exceeds %d columns (%d): %q", helpMaxColumns, len(line), line)
		}
	}
	for i, n := range names {
		if !strings.Contains(got, n) {
			t.Errorf("wrapped output lost %q", n)
		}
		// Every name but the last is followed by a comma.
		if i < len(names)-1 && !strings.Contains(got, n+",") {
			t.Errorf("%q is not comma-terminated in the wrapped output", n)
		}
	}
	if strings.Contains(got, names[len(names)-1]+",") {
		t.Errorf("the last name carries a trailing comma:\n%s", got)
	}
}

// auditGroupLabelled reports whether line opens one of the declared groups,
// read off auditOperationGroups rather than spelled out, so renaming a label
// cannot quietly turn the layout assertions below into no-ops.
func auditGroupLabelled(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, g := range auditOperationGroups {
		if strings.HasPrefix(trimmed, g.label) {
			return true
		}
	}
	return false
}

// TestAuditOperationBlock_HasNoCatchAllGroup is rule 5(b) of
// SPEC/HELP.md § Audit operation entity-type classification, and it is the
// exact inverse of the guarantee this file used to pin. The block previously
// ended in a catch-all group whose empty prefix matched every name the entity
// prefixes rejected, so an operation named after a third entity still reached
// the help. That was the right shape while the grouping was inferred from the
// operation's name and the prefix was not trusted to match it.
//
// With the classification declared and total, nothing can be left over, and a
// catch-all can only hide the failure the totality gate exists to produce: an
// unclassified operation would still be printed, under a heading that asserts
// nothing about it, while the block still listed every value the command
// accepts and the reader was told nothing about whose history it belongs to.
func TestAuditOperationBlock_HasNoCatchAllGroup(t *testing.T) {
	if len(auditOperationGroups) == 0 {
		t.Fatal("auditOperationGroups is empty, so the Valid operations block renders nothing")
	}
	for i, g := range auditOperationGroups {
		if g.label == "" {
			t.Errorf("group %d has an empty label, so its operations are printed under no heading at all", i)
		}
		if !models.IsValidEntityType(string(g.entityType)) {
			t.Errorf("group %d (%s) names entity type %q, which is not a valid EntityType; a group whose "+
				"label matches no entity type is a catch-all wearing a name", i, g.label, g.entityType)
		}
	}
}

// TestAuditOperationBlock_GroupsHoldExactlyTheirDeclaredOperations is the other
// half of rule 5(b): every group must hold exactly the operations declared
// against its (entity type, legacy) pair. A group that merely happens to look
// right today would let a misfiled operation be published under an entity whose
// history does not contain it, which is a false statement about stored data
// rather than a cosmetic slip.
func TestAuditOperationBlock_GroupsHoldExactlyTheirDeclaredOperations(t *testing.T) {
	block := auditOperationBlock()

	// Where each operation was actually printed, by group label.
	printedUnder := map[string]string{}
	current := ""
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		for _, g := range auditOperationGroups {
			if strings.HasPrefix(trimmed, g.label) {
				current = g.label
			}
		}
		if current == "" {
			t.Fatalf("the block opens with a line under no group label, so nothing can be attributed: %q", line)
		}
		list := line
		if idx := strings.LastIndex(list, ":"); idx >= 0 {
			list = list[idx+1:]
		}
		for _, field := range strings.FieldsFunc(list, func(r rune) bool { return r == ' ' || r == ',' }) {
			printedUnder[field] = current
		}
	}

	// The label a given operation is required to appear under.
	wantLabel := func(op models.AuditOperation) string {
		class, declared := models.ClassifyAuditOperation(op)
		if !declared {
			return ""
		}
		for _, g := range auditOperationGroups {
			if g.entityType == class.EntityType && g.legacy == class.Legacy {
				return g.label
			}
		}
		return ""
	}

	checked := 0
	for _, op := range models.ValidAuditOperations {
		want := wantLabel(op)
		if want == "" {
			t.Errorf("%s has no group to be printed under; it is either unclassified (see "+
				"TestAuditOperationClassification_IsTotal in internal/models) or its declared "+
				"(entity type, legacy) pair has no group in auditOperationGroups", op)
			continue
		}
		got, printed := printedUnder[string(op)]
		if !printed {
			t.Errorf("%s is declared but absent from the rendered block, so the help publishes a subset "+
				"of the catalogue", op)
			continue
		}
		if got != want {
			t.Errorf("%s is printed under %q but is declared against %q; the heading asserts that its rows "+
				"hold that entity_type, so the block is making a false statement about stored data",
				op, got, want)
			continue
		}
		checked++
	}

	// A rendering change that stopped matching would leave every assertion above
	// unexecuted while the test still reported success.
	if checked < len(models.ValidAuditOperations) {
		t.Errorf("only %d of %d operations were attributed to a group; the block scan has stopped matching "+
			"and this gate is no longer measuring the whole catalogue", checked, len(models.ValidAuditOperations))
	}
}

// TestAuditOperationBlock_LegacyGroupsAreLabelledLegacy is rule 2 of
// SPEC/HELP.md § Audit family help specifics, at the level the rule actually
// operates: the LEGACY marking must be carried by the group's own label, not by
// an inline marker beside a name. An inline marker would put a token in the list
// column that is not an operation `audit list --operation` accepts, which is the
// exact basis on which TestHelpEnumCoverage_AuditHelpListsEveryOperation checks
// the block.
func TestAuditOperationBlock_LegacyGroupsAreLabelledLegacy(t *testing.T) {
	legacyGroups := 0
	for _, g := range auditOperationGroups {
		marked := strings.Contains(g.label, "LEGACY")
		if g.legacy && !marked {
			t.Errorf("group %q holds the LEGACY operations of %s but its label does not say LEGACY, so a "+
				"reader cannot tell them from the operations in use", g.label, g.entityType)
		}
		if !g.legacy && marked {
			t.Errorf("group %q is labelled LEGACY but holds operations commands still write, which tells a "+
				"reader to stop filtering on values that are still being recorded", g.label)
		}
		if g.legacy {
			legacyGroups++
		}
	}
	if legacyGroups == 0 {
		t.Error("no group holds LEGACY operations, so the four legacy values are listed indistinguishably " +
			"from the operations in use")
	}
}

// ---------------------------------------------------------------------------
// A family help header that says "for -y, --type on 'a', 'b' and 'c'" is a
// promise about the command surface, and the registry is the authority on
// whether that promise holds.
// ---------------------------------------------------------------------------

// subcommandsNamedInEnumHeaders returns every subcommand name quoted inside a
// "Valid ... (for <flag> on '...', '...' and '...')" header of one help output,
// keyed by the flag the header attributes them to.
//
// The header legitimately wraps across lines, so the scan accumulates from the
// "for" clause until the closing "):" rather than working line by line.
func subcommandsNamedInEnumHeaders(out string) map[string][]string {
	named := map[string][]string{}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		idx := strings.Index(line, "(for ")
		if idx < 0 || !strings.HasPrefix(strings.TrimSpace(line), "Valid ") {
			continue
		}
		header := line[idx+len("(for "):]
		for _, next := range lines[i+1:] {
			if strings.Contains(header, "):") {
				break
			}
			header += " " + strings.TrimSpace(next)
		}
		// An unterminated header means the help text no longer has the shape this
		// scan assumes. Skipping it silently would make the gate vacuous, but the
		// caller's minimum-attributions guard catches that, so skipping here is
		// safe and keeps the helper total.
		end := strings.Index(header, "):")
		if end < 0 {
			continue
		}
		header = header[:end]

		// The flag the header attributes the values to: the long form is the
		// discriminator, since the short form alone is ambiguous across families.
		flag := ""
		for _, token := range strings.FieldsFunc(header, func(r rune) bool { return r == ' ' || r == ',' }) {
			if strings.HasPrefix(token, "--") {
				flag = token
				break
			}
		}
		if flag == "" {
			continue // a header that attributes its values to a positional, not a flag
		}
		for _, quoted := range regexp.MustCompile(`'([a-z][a-z-]*)'`).FindAllStringSubmatch(header, -1) {
			named[flag] = append(named[flag], quoted[1])
		}
	}
	return named
}

// TestHelpEnumCoverage_HeaderNamesOnlySubcommandsThatAcceptTheValues closes a
// class of defect rather than one instance of it: a family help header that
// attributes a set of enum values to a subcommand which cannot accept them at
// all, so a reader who follows the help gets exit code 2.
//
// The shipped instance was `task --help`, whose comment-type header read
// "for -y, --type on 'comment-add', 'comment-list', 'comment-edit' and
// 'comment-remove'" while `task comment-remove --type NOTE` rejects the flag as
// unknown. The sprint family got it right, which is exactly why a per-family
// eyeball is not a gate: the two texts are written and maintained separately.
//
// A header may legitimately name a subcommand that takes the values
// POSITIONALLY rather than through the flag — `task --help` names the 'stat'
// setter for the status values and `audit --help` names the 'history' arg for
// the entity types, both correctly. So the rule is not "declares the flag" but
// "can accept these values at all": through the flag, or through a positional
// argument carrying the same enum. The registry is the authority for both.
//
// The assertion is one-directional. A header is allowed to describe a subset,
// so a subcommand that accepts the values without being named is not a defect.
func TestHelpEnumCoverage_HeaderNamesOnlySubcommandsThatAcceptTheValues(t *testing.T) {
	families := []struct {
		name  string
		build func() Command
	}{
		{"task", buildTaskCommand},
		{"sprint", buildSprintCommand},
		{"backlog", buildBacklogCommand},
		{"audit", buildAuditCommand},
		{"graph", buildGraphCommand},
		{"roadmap", buildRoadmapCommand},
	}

	checked := 0
	for _, family := range families {
		command := family.build()

		out := captureStdout(t, command.HelpPrinter)
		headers := subcommandsNamedInEnumHeaders(out)
		if len(headers) == 0 {
			continue // this family publishes no enum header attributed to a flag
		}

		for flag, subcommands := range headers {
			// Which enum does this flag carry? Read it off whichever subcommand
			// declares it, so the positional comparison below is against the
			// same enum rather than against any enum at all.
			enum := ""
			for _, sub := range command.Subcommands {
				for _, declared := range sub.Flags {
					if declared.Long == flag && declared.Enum != "" {
						enum = declared.Enum
					}
				}
			}

			for _, name := range subcommands {
				checked++

				var sub *Subcommand
				for i := range command.Subcommands {
					if command.Subcommands[i].Name == name {
						sub = &command.Subcommands[i]
					}
				}
				if sub == nil {
					t.Errorf("rmp %s --help attributes %s to subcommand %q, which the registry does not define",
						family.name, flag, name)
					continue
				}

				accepts := false
				for _, declared := range sub.Flags {
					if declared.Long == flag {
						accepts = true
					}
				}
				if !accepts && enum != "" {
					// The values may arrive positionally instead, which the
					// header signals in prose ("setter", "arg") and the registry
					// records as a positional carrying the same enum.
					for _, arg := range sub.Positional {
						if arg.Enum == enum {
							accepts = true
						}
					}
				}
				if !accepts {
					t.Errorf("rmp %s --help says %s applies to %q, but that subcommand accepts those values "+
						"neither as %s nor as a positional carrying enum %q, so following the help fails with "+
						"exit 2 (unknown flag)", family.name, flag, name, flag, enum)
				}
			}
		}
	}

	// Guard against the test silently measuring nothing, which is how a header
	// scan regresses: a wording change that stops matching leaves it green.
	if checked < 7 {
		t.Fatalf("only %d header-to-subcommand attributions were checked; the header scan has stopped "+
			"matching and the gate is now vacuous", checked)
	}
}
