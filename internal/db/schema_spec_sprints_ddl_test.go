package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sprintsSpecHeading and sprintsSpecFence locate the canonical block of
// SPEC/DATABASE.md § DDL - Table Creation, `sprints` Table.
const (
	sprintsSpecHeading = "### `sprints` Table\n"
	sprintsSpecFence   = "```"
)

// sprintsSpecBlock returns the text between the fences of the first fenced
// block under the `sprints` Table heading of spec: from the line after the
// opening fence to the end of the line before the closing fence, that line's
// newline included.
func sprintsSpecBlock(t *testing.T, spec string) string {
	t.Helper()

	start := strings.Index(spec, sprintsSpecHeading)
	if start < 0 {
		t.Fatalf("SPEC/DATABASE.md carries no %q heading", strings.TrimSpace(sprintsSpecHeading))
	}
	section := spec[start+len(sprintsSpecHeading):]
	if next := strings.Index(section, "\n### "); next >= 0 {
		section = section[:next]
	}

	open := strings.Index(section, "\n"+sprintsSpecFence)
	if open < 0 {
		t.Fatal("the `sprints` Table section of SPEC/DATABASE.md carries no fenced block")
	}
	body := section[open+1:]
	lineEnd := strings.IndexByte(body, '\n')
	if lineEnd < 0 {
		t.Fatal("the opening fence of the `sprints` block ends the section")
	}
	body = body[lineEnd+1:]
	closing := strings.Index(body, "\n"+sprintsSpecFence)
	if closing < 0 {
		t.Fatal("the `sprints` block of SPEC/DATABASE.md is never closed")
	}
	return body[:closing+1]
}

// TestSprintsDDLMatchesSpecBlock enforces the claim of SPEC/DATABASE.md
// § DDL - Table Creation, `sprints` Table: the text between the fences of that
// block is byte-identical to sprintsDDL without the newline that opens the raw
// string literal.
func TestSprintsDDLMatchesSpecBlock(t *testing.T) {
	spec, err := os.ReadFile(filepath.Join(moduleRoot(t), "SPEC", "DATABASE.md"))
	if err != nil {
		t.Fatalf("reading SPEC/DATABASE.md: %v", err)
	}
	block := sprintsSpecBlock(t, string(spec))

	if !strings.HasPrefix(sprintsDDL, "\n") {
		t.Fatal("sprintsDDL no longer opens with a newline; the identity the SPEC states is defined without it")
	}
	got := sprintsDDL[1:]
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
	t.Errorf("sprintsDDL differs from the `sprints` block of SPEC/DATABASE.md at byte %d (block line %d):\n"+
		"  sprintsDDL: %q\n  SPEC block: %q",
		at, line, excerpt(got, at), excerpt(block, at))
}

// excerpt returns up to 40 bytes of s from at.
func excerpt(s string, at int) string {
	if at >= len(s) {
		return ""
	}
	return s[at:min(at+40, len(s))]
}
