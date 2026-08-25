// Package models — SPEC/ directory-listing gate.
//
// This is the fourth specification-side gate file in this package. Its
// precedents each measure one written enumeration against the thing enumerated:
// spec_enum_coverage_test.go and docs_enum_coverage_test.go pin a documented
// list against an enum in the code, and spec_xref_test.go resolves every
// cross-reference in SPEC/ against the headings that exist. This one pins a
// written enumeration of a directory against that directory.
//
// The defect it closes was found by hand. SPEC/ARCHITECTURE.md § Directory
// Structure renders the repository as a tree, and the SPEC/ node of that tree
// named thirteen files when fourteen existed: GRAPH.md was missing, so the graph
// area — a whole functional area with its own SPEC file — read as unspecified to
// anyone who trusted the tree. Nothing in the build noticed, because until now
// nothing in the build compared that listing with the directory.
//
// The class is the same one task 279 closed for .claude/: a static enumeration of
// a directory, written by hand once and never re-derived, which drifts silently
// as the directory changes. What makes the class expensive is that it fails at
// the moment of USE rather than the moment of reading. A stale entry and a live
// entry look identical on the page; the reader cannot tell them apart without
// checking every name against the filesystem, which is precisely the work the
// listing exists to save.
//
// What the gate checks.
//
// Both directions, because only one of them was the observed defect and the
// other is just as silent: a file present in SPEC/ and absent from the listing
// (the GRAPH.md case), and an entry in the listing with no file behind it (a
// SPEC file renamed or removed, its line left standing). Either failure names
// the file.
//
// What it deliberately does not check.
//
// It covers this one listing and no other. Two further enumerations of SPEC/
// exist — the Functional Area Mapping table in CLAUDE.md § 2 and the index in
// SPEC/README.md — and both are outside the fix this gate belongs to. It also
// does not assert the order of the entries; the listing is maintained in
// alphabetical order today, but order is not a claim about the directory's
// contents and asserting it here would widen the gate past what it exists for.
//
// Anti-vacuity.
//
// A scanner that has stopped matching compares an empty set with an empty set
// and reports success, which is the failure mode this suite has been bitten by
// before. So every way of finding nothing is fatal here rather than silent: a
// tree block that cannot be located, a SPEC/ node that is not unique or not
// inside a fenced block, a region that yields zero entries, a count under the
// floor on either side, and an entry line the parser cannot read. The region
// parser is itself exercised against synthetic trees below, so the claim that it
// can fail is demonstrated rather than assumed.
package models

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// specArchitectureRelPath is the document that carries the listing. The
// directory it enumerates is specDirRelPath, declared by spec_xref_test.go and
// reused here so the two gates cannot disagree about where SPEC/ is.
const specArchitectureRelPath = "SPEC/ARCHITECTURE.md"

// specTreeNode is the tree node whose children this gate reads. The trailing
// slash is part of the match: it is what distinguishes the directory node from a
// hypothetical file of the same name.
const specTreeNode = "SPEC/"

// The four glyph runs the tree is drawn with. A child line is an indent built
// from verticalRun and blankRun, then one connector, then the entry name.
const (
	branchConnector = "├── "
	lastConnector   = "└── "
	verticalRun     = "│   "
	blankRun        = "    "
)

// minSpecDirectoryEntries is the floor below which a count is treated as
// evidence that the gate has stopped measuring rather than as evidence about the
// tree. Both sides are held to it: the listing and the directory each hold 14
// entries today. The floor matches minSpecDocuments in spec_xref_test.go, which
// counts the same files from the other side — far enough under 14 that removing
// a SPEC file does not trip it, and far enough over zero that a gate measuring
// nothing cannot report success.
const minSpecDirectoryEntries = 10

// treeEntry is one child line of the listing.
type treeEntry struct {
	name      string // the entry as written, trailing slash included for a directory
	connector string // branchConnector, or lastConnector on the final child
	line      int    // 1-based line number in the document, so failures name real lines
}

// ---------------------------------------------------------------------------
// 1. The listing names every entry of SPEC/, and names nothing else.
// ---------------------------------------------------------------------------

func TestSpecDirectoryListing_ArchitectureTreeMatchesSpecDirectory(t *testing.T) {
	entries, err := specDirectoryListing(readRepoFile(t, specArchitectureRelPath))
	if err != nil {
		t.Fatalf("the %s node of the tree in %s could not be read, so this gate is comparing nothing "+
			"against nothing and would pass whatever the listing said: %v",
			specTreeNode, specArchitectureRelPath, err)
	}

	if len(entries) < minSpecDirectoryEntries {
		t.Fatalf("only %d entries were parsed out of the %s listing in %s, and the floor is %d; the tree "+
			"glyphs or the node's shape have drifted, so the scan is no longer reading the listing",
			len(entries), specTreeNode, specArchitectureRelPath, minSpecDirectoryEntries)
	}

	listed := make(map[string]int, len(entries))
	for _, e := range entries {
		if first, dup := listed[e.name]; dup {
			t.Errorf("%s:%d lists %s a second time; it is already on line %d, and a repeated entry inflates "+
				"every count taken from the listing without adding a file to SPEC/",
				specArchitectureRelPath, e.line, e.name, first)
			continue
		}
		listed[e.name] = e.line
	}

	actual := readSpecDirectory(t)
	if len(actual) < minSpecDirectoryEntries {
		t.Fatalf("only %d entries were read out of %s/, and the floor is %d; the directory is not where "+
			"this gate assumes it is, so nothing is being compared",
			len(actual), specDirRelPath, minSpecDirectoryEntries)
	}

	present := make(map[string]bool, len(actual))
	for _, name := range actual {
		present[name] = true
	}

	// Direction 1: the directory is authoritative, so everything in it must be
	// listed. This is the observed defect — GRAPH.md existed and the tree did not
	// name it, so the graph area read as unspecified.
	for _, name := range actual {
		if _, ok := listed[name]; !ok {
			t.Errorf("%s/%s exists but the %s listing in %s § Directory Structure does not name it, so a "+
				"reader of that tree cannot tell the file is there: add \"%s%s%s\" in its alphabetical "+
				"position", specDirRelPath, name, specTreeNode, specArchitectureRelPath,
				blankRun, branchConnector, name)
		}
	}

	// Direction 2: without it the listing could keep naming a file that was
	// renamed or removed, and the gate would still be green. A stale entry is
	// worse than a missing one — it sends the reader after a document that is not
	// there.
	for _, e := range entries {
		if !present[e.name] {
			t.Errorf("%s:%d lists %s under %s, but %s/%s does not exist; either the file was renamed or "+
				"removed and its line outlived it, or the line names a document that was only ever planned",
				specArchitectureRelPath, e.line, e.name, specTreeNode, specDirRelPath, e.name)
		}
	}

	// With both directions satisfied and no repeated entry, the two sets can no
	// longer differ in size. Saying so turns a future parser change that starts
	// dropping lines into a failure rather than into a quieter gate. The t.Failed
	// guard keeps the reasoning true whenever the message is printed: any failure
	// above already explains the difference.
	if !t.Failed() && len(entries) != len(actual) {
		t.Errorf("the listing names %d entries and %s/ holds %d; with both directions satisfied and no "+
			"repeated entry, the counts cannot differ, so the region parser is dropping lines",
			len(entries), specDirRelPath, len(actual))
	}
}

// readSpecDirectory returns the visible entries of SPEC/, a directory carrying
// the trailing slash the tree would draw it with.
//
// Dot-prefixed entries are skipped. A tree rendering does not draw them, and
// they are tooling artefacts — an editor's swap file left in SPEC/ must not fail
// a gate about which specifications exist. os.ReadDir sorts by filename, so the
// order of any failure message is stable.
func readSpecDirectory(t *testing.T) []string {
	t.Helper()

	dir := filepath.Join(repoRoot(t), filepath.FromSlash(specDirRelPath))
	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s/: %v", specDirRelPath, err)
	}

	names := make([]string, 0, len(items))
	for _, item := range items {
		name := item.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if item.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	return names
}

// ---------------------------------------------------------------------------
// Region parser.
// ---------------------------------------------------------------------------

// specDirectoryListing returns the children of the SPEC/ node of the directory
// tree in doc.
//
// It returns an error rather than failing a test, so the parser can be exercised
// against trees this repository does not contain — see the table below. Every
// error is a reason the listing could not be read; none of them is evidence that
// the listing is correct.
func specDirectoryListing(doc string) ([]treeEntry, error) {
	lines := strings.Split(doc, "\n")

	nodeIdx, childIndent, err := locateSpecNode(lines)
	if err != nil {
		return nil, err
	}

	// The node was found inside a fenced block, so the next delimiter closes it.
	fenceEnd := len(lines)
	for j := nodeIdx + 1; j < len(lines); j++ {
		if fenceOpenerPattern.MatchString(lines[j]) {
			fenceEnd = j
			break
		}
	}

	entries := make([]treeEntry, 0, fenceEnd-nodeIdx)
	regionEnd := fenceEnd
	for j := nodeIdx + 1; j < fenceEnd; j++ {
		indent, connector, remainder, ok := splitTreeLine(lines[j])
		// A deeper line belongs to a child's own subtree, not to this level.
		if ok && indent != childIndent && strings.HasPrefix(indent, childIndent) {
			continue
		}
		if !ok || indent != childIndent {
			regionEnd = j
			break
		}
		name, err := treeEntryName(remainder)
		if err != nil {
			return nil, fmt.Errorf("line %d of the %s listing: %w", j+1, specTreeNode, err)
		}
		entries = append(entries, treeEntry{name: name, connector: connector, line: j + 1})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("the %s node on line %d has no child entries; the listing is empty or its "+
			"indent is not %q", specTreeNode, nodeIdx+1, childIndent)
	}

	// Nothing at this level may appear after the region closes. Without this, an
	// entry appended past the final connector — or after a blank line — would be
	// invisible to the gate while being perfectly visible to a reader.
	for j := regionEnd; j < fenceEnd; j++ {
		if indent, _, _, ok := splitTreeLine(lines[j]); ok && indent == childIndent {
			return nil, fmt.Errorf("line %d sits at the depth of the %s listing but outside it; the listing "+
				"ends on line %d, so this entry is not part of the region the gate reads",
				j+1, specTreeNode, entries[len(entries)-1].line)
		}
	}

	// The connectors are the tree's own claim about where the listing ends: every
	// entry but the last joins with branchConnector, and the last closes with
	// lastConnector. A listing that breaks that is drawn wrong, and it is also the
	// shape that lets an appended entry hide.
	for i, e := range entries {
		want := branchConnector
		if i == len(entries)-1 {
			want = lastConnector
		}
		if e.connector != want {
			return nil, fmt.Errorf("line %d draws %s with %q, but at position %d of %d it must use %q",
				e.line, e.name, e.connector, i+1, len(entries), want)
		}
	}

	return entries, nil
}

// locateSpecNode finds the one SPEC/ directory node of the tree and returns its
// line index together with the indent its children must carry.
//
// A child's indent is the node's own indent extended by one level: blankRun when
// the node closes its level, verticalRun when the level continues below it.
func locateSpecNode(lines []string) (nodeIdx int, childIndent string, err error) {
	inFence := false
	found := make([]int, 0, 2)
	fenced := make([]bool, 0, 2)
	indents := make([]string, 0, 2)

	for i, line := range lines {
		if fenceOpenerPattern.MatchString(line) {
			inFence = !inFence
			continue
		}
		indent, connector, remainder, ok := splitTreeLine(line)
		if !ok {
			continue
		}
		name, nameErr := treeEntryName(remainder)
		if nameErr != nil || name != specTreeNode {
			continue
		}
		found = append(found, i)
		fenced = append(fenced, inFence)
		if connector == lastConnector {
			indents = append(indents, indent+blankRun)
		} else {
			indents = append(indents, indent+verticalRun)
		}
	}

	switch {
	case len(found) == 0:
		return 0, "", fmt.Errorf("no tree node named %s was found; the directory tree this gate reads is "+
			"gone, moved, or drawn with different glyphs", specTreeNode)
	case len(found) > 1:
		return 0, "", fmt.Errorf("%d tree nodes named %s were found, on lines %s; the gate cannot tell "+
			"which listing it is meant to hold to the directory", len(found), specTreeNode, lineList(found))
	case !fenced[0]:
		return 0, "", fmt.Errorf("the %s node on line %d is not inside a fenced code block, so it is prose "+
			"that happens to look like a tree rather than the directory listing", specTreeNode, found[0]+1)
	}

	return found[0], indents[0], nil
}

// splitTreeLine decomposes one line of a tree rendering into its indent, its
// connector, and whatever follows. ok is false for any line that is not a node.
func splitTreeLine(line string) (indent, connector, remainder string, ok bool) {
	i := 0
	for {
		if strings.HasPrefix(line[i:], verticalRun) {
			i += len(verticalRun)
			continue
		}
		if strings.HasPrefix(line[i:], blankRun) {
			i += len(blankRun)
			continue
		}
		break
	}

	switch {
	case strings.HasPrefix(line[i:], branchConnector):
		connector = branchConnector
	case strings.HasPrefix(line[i:], lastConnector):
		connector = lastConnector
	default:
		return "", "", "", false
	}

	return line[:i], connector, line[i+len(connector):], true
}

// errNamelessTreeEntry reports a connector with nothing after it: a line drawn
// as a tree node that names no entry.
var errNamelessTreeEntry = errors.New("a tree connector is followed by nothing, so the entry has no name")

// treeEntryName takes the name off the text following a connector.
//
// The tree annotates some of its nodes with a trailing comment — "SPEC/
// # Technical specification" — so the name is the first field and a comment is
// allowed after it. Anything else is a line the parser does not understand, and
// an entry it cannot read must never be counted as an entry that is not there.
func treeEntryName(remainder string) (string, error) {
	fields := strings.Fields(remainder)
	if len(fields) == 0 {
		return "", errNamelessTreeEntry
	}
	if len(fields) > 1 && !strings.HasPrefix(fields[1], "#") {
		return "", fmt.Errorf("the entry %q is followed by %q, which is neither a comment nor part of a "+
			"name this parser understands", fields[0], strings.TrimSpace(remainder[len(fields[0]):]))
	}
	return fields[0], nil
}

// lineList renders zero-based line indexes as a comma-separated list of the
// 1-based numbers a reader will find in the file.
func lineList(idx []int) string {
	parts := make([]string, 0, len(idx))
	for _, i := range idx {
		parts = append(parts, strconv.Itoa(i+1))
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// 2. The region parser can fail, and fails on the shapes that would hide drift.
// ---------------------------------------------------------------------------

// TestSpecDirectoryListing_RegionParserRejectsUnreadableTrees exercises the
// parser against trees this repository does not contain.
//
// Its purpose is the anti-vacuity claim above. The gate's whole value rests on
// the parser reporting an unreadable listing instead of an empty one, and that
// is a claim about code paths the real document never takes, so it cannot be
// demonstrated by running the gate. Each case below is a way the listing could
// stop being readable — or a way an entry could hide from a scan that trusted
// the first non-matching line.
func TestSpecDirectoryListing_RegionParserRejectsUnreadableTrees(t *testing.T) {
	const fence = "```"

	tree := func(body ...string) string {
		return strings.Join(append(append([]string{fence}, body...), fence), "\n")
	}

	tests := []struct {
		name    string
		doc     string
		want    []string
		wantErr string
	}{
		{
			name: "the shape the repository uses",
			doc: tree(
				"Groadmap/",
				"├── go.mod",
				"└── SPEC/                  # Technical specification",
				"    ├── ARCHITECTURE.md",
				"    ├── BUILD.md",
				"    └── WEB.md",
			),
			want: []string{"ARCHITECTURE.md", "BUILD.md", "WEB.md"},
		},
		{
			name: "a node whose level continues below it",
			doc: tree(
				"Groadmap/",
				"├── SPEC/",
				"│   ├── ARCHITECTURE.md",
				"│   └── WEB.md",
				"└── go.mod",
			),
			want: []string{"ARCHITECTURE.md", "WEB.md"},
		},
		{
			name: "a subdirectory of SPEC/ and its own children",
			doc: tree(
				"Groadmap/",
				"└── SPEC/",
				"    ├── ARCHITECTURE.md",
				"    ├── assets/",
				"    │   └── schema.svg",
				"    └── WEB.md",
			),
			want: []string{"ARCHITECTURE.md", "assets/", "WEB.md"},
		},
		{
			name:    "no tree at all",
			doc:     "# Directory Structure\n\nThe SPEC/ directory holds the specifications.\n",
			wantErr: "no tree node named",
		},
		{
			name: "a tree drawn in prose, outside any fence",
			doc: strings.Join([]string{
				"Groadmap/",
				"└── SPEC/",
				"    └── ARCHITECTURE.md",
			}, "\n"),
			wantErr: "not inside a fenced code block",
		},
		{
			name: "the same node drawn twice",
			doc: tree(
				"└── SPEC/",
				"    └── ARCHITECTURE.md",
			) + "\n" + tree(
				"└── SPEC/",
				"    └── WEB.md",
			),
			wantErr: "2 tree nodes named",
		},
		{
			name: "a node with no children",
			doc: tree(
				"Groadmap/",
				"├── go.mod",
				"└── SPEC/",
			),
			wantErr: "has no child entries",
		},
		{
			name: "an entry appended past the end of the listing",
			doc: tree(
				"└── SPEC/",
				"    ├── ARCHITECTURE.md",
				"    └── WEB.md",
				"",
				"    └── GRAPH.md",
			),
			wantErr: "outside it",
		},
		{
			name: "a listing that never closes its level",
			doc: tree(
				"└── SPEC/",
				"    ├── ARCHITECTURE.md",
				"    ├── WEB.md",
			),
			wantErr: "must use",
		},
		{
			// The previous case and this one are caught by different rules, which
			// is the point of keeping both. There the region ran to the fence and
			// the connectors were checked; here the entry sits immediately after
			// the closing connector, so it is still inside the region and the
			// connector rule is what refuses it. Separate a stray entry from the
			// listing by a blank line instead, as the case above does, and it is
			// the outside-the-region scan that catches it.
			name: "a listing closed before its last entry",
			doc: tree(
				"└── SPEC/",
				"    └── ARCHITECTURE.md",
				"    ├── WEB.md",
			),
			wantErr: "must use",
		},
		{
			name: "an entry with no name",
			doc: tree(
				"└── SPEC/",
				"    ├── ARCHITECTURE.md",
				"    └── ",
			),
			wantErr: "has no name",
		},
		{
			name: "an entry whose name is followed by unreadable text",
			doc: tree(
				"└── SPEC/",
				"    ├── ARCHITECTURE.md",
				"    └── WEB.md and GRAPH.md",
			),
			wantErr: "neither a comment nor part of a",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := specDirectoryListing(tc.doc)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parsed %d entries (%v) from a tree that cannot be read; the gate would compare "+
						"that against SPEC/ as if it were the listing", len(entries), entryNames(entries))
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want it to mention %q so the reader is told what to fix",
						err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("a readable tree was rejected: %v", err)
			}
			got := entryNames(entries)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("entries = %v, want %v", got, tc.want)
			}
		})
	}
}

// entryNames projects parsed entries onto their names, for comparison and for
// failure messages.
func entryNames(entries []treeEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.name)
	}
	return names
}
