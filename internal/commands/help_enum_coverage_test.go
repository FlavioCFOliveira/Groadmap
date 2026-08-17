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
// The wrapping helper introduced for (1) is unit-tested here too: it is the
// only piece of layout logic in the help layer, so its column arithmetic is
// pinned rather than eyeballed.
package commands

import (
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
		if strings.HasPrefix(strings.TrimSpace(line), "Task ops:") || strings.HasPrefix(strings.TrimSpace(line), "Sprint ops:") {
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

// TestAuditOperationBlock_HasCatchAllGroup pins the structural guarantee that
// makes the coverage test above impossible to defeat: the last group's prefix
// is empty, so it matches every name the earlier prefixes reject. Without it,
// an operation named after a third entity would be classified into no group
// and silently vanish from the help.
func TestAuditOperationBlock_HasCatchAllGroup(t *testing.T) {
	if len(auditOperationGroups) == 0 {
		t.Fatal("auditOperationGroups is empty")
	}
	last := auditOperationGroups[len(auditOperationGroups)-1]
	if last.prefix != "" {
		t.Errorf("the last operation group has prefix %q, want \"\" (the catch-all that keeps an "+
			"unrecognised operation from being dropped from the help)", last.prefix)
	}
	for i, g := range auditOperationGroups[:len(auditOperationGroups)-1] {
		if g.prefix == "" {
			t.Errorf("group %d (%s) has an empty prefix, so it swallows every operation before the "+
				"entity groups are consulted", i, g.label)
		}
	}
	// Today every operation matches an entity prefix, so the catch-all
	// contributes no line: its presence must not add noise to the help.
	if strings.Contains(auditOperationBlock(), last.label) {
		t.Errorf("the catch-all group is rendered even though every operation matches an entity prefix:\n%s",
			auditOperationBlock())
	}
}
