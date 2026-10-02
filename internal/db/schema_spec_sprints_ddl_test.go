package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specFence opens and closes a fenced block of SPEC/DATABASE.md.
const specFence = "```"

// specDDLBlocks maps each table section heading of SPEC/DATABASE.md
// § DDL - Table Creation to the raw string literal of schema.go that
// CreateSchema executes for that table, as the table of that section states.
var specDDLBlocks = []struct {
	heading string // the section heading line, its newline included
	name    string // the literal's identifier, for the failure message
	ddl     string // the literal's value
}{
	{"### `tasks` Table\n", "tasksDDL", tasksDDL},
	{"### `sprints` Table\n", "sprintsDDL", sprintsDDL},
	{"### `sprint_tasks` Table (1:N Relationship)\n", "sprintTasksDDL", sprintTasksDDL},
	{"### `audit` Table\n", "auditDDL", auditDDL},
	{"### `task_dependencies` Table\n", "taskDependenciesDDL", taskDependenciesDDL},
	{"### `task_comments` Table\n", "taskCommentsDDL", taskCommentsDDL},
	{"### `sprint_comments` Table\n", "sprintCommentsDDL", sprintCommentsDDL},
	{"### `_metadata` Table\n", "metadataDDL", metadataDDL},
}

// specBlock returns the text between the fences of the first fenced block
// under heading in spec: from the line after the opening fence to the end of
// the line before the closing fence, that line's newline included. The section
// ends at the next "### " heading.
func specBlock(t *testing.T, spec, heading string) string {
	t.Helper()

	name := strings.TrimSpace(heading)
	start := strings.Index(spec, heading)
	if start < 0 {
		t.Fatalf("SPEC/DATABASE.md carries no %q heading", name)
	}
	section := spec[start+len(heading):]
	if next := strings.Index(section, "\n### "); next >= 0 {
		section = section[:next]
	}

	open := strings.Index(section, "\n"+specFence)
	if open < 0 {
		t.Fatalf("the %q section of SPEC/DATABASE.md carries no fenced block", name)
	}
	body := section[open+1:]
	lineEnd := strings.IndexByte(body, '\n')
	if lineEnd < 0 {
		t.Fatalf("the opening fence of the %q block ends the section", name)
	}
	body = body[lineEnd+1:]
	closing := strings.Index(body, "\n"+specFence)
	if closing < 0 {
		t.Fatalf("the %q block of SPEC/DATABASE.md is never closed", name)
	}
	return body[:closing+1]
}

// TestSchemaDDLMatchesSpecBlocks enforces the claim of SPEC/DATABASE.md
// § DDL - Table Creation: for every table, the text between the fences of the
// table's block is byte-identical to the table's literal in schema.go without
// the newline that opens the raw string literal.
func TestSchemaDDLMatchesSpecBlocks(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "SPEC", "DATABASE.md"))
	if err != nil {
		t.Fatalf("reading SPEC/DATABASE.md: %v", err)
	}
	spec := string(raw)

	for _, tc := range specDDLBlocks {
		t.Run(tc.name, func(t *testing.T) {
			block := specBlock(t, spec, tc.heading)

			if !strings.HasPrefix(tc.ddl, "\n") {
				t.Fatalf("%s no longer opens with a newline; the identity the SPEC states is defined without it", tc.name)
			}
			got := tc.ddl[1:]
			if got == block {
				return
			}

			// Name the first byte that differs, so a one-character drift is found at once.
			n := min(len(got), len(block))
			at := n
			for i := range n {
				if got[i] != block[i] {
					at = i
					break
				}
			}
			line := strings.Count(block[:min(at, len(block))], "\n") + 1
			t.Errorf("%s differs from the %q block of SPEC/DATABASE.md at byte %d (block line %d):\n"+
				"  %s: %q\n  SPEC block: %q",
				tc.name, strings.TrimSpace(tc.heading), at, line, tc.name, excerpt(got, at), excerpt(block, at))
		})
	}
}

// excerpt returns up to 40 bytes of s from at.
func excerpt(s string, at int) string {
	if at >= len(s) {
		return ""
	}
	return s[at:min(at+40, len(s))]
}
