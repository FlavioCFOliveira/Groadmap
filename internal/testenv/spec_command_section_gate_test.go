package testenv

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This file is the regression gate for rmp task #436.
//
// # The defect it closes
//
// `SPEC/COMMANDS.md` gives every command and subcommand a small, fixed family of
// sections — `<Name> Options`, `<Name> Exit Codes`, `<Name> Error Cases`,
// `<Name> Output` — and Go comments cite them by name to say where a published
// contract lives. When a subcommand is withdrawn, its whole family of sections
// goes with it, and every comment citing one is left pointing at a heading no
// document declares. Withdrawing `rmp graph execute` deleted five such headings
// (`Execute`, `Execute Options`, `Execute Exit Codes`, `Execute Error Cases`,
// `Execute Output`) and left FIVE Go comments citing them — four that were known
// and a fifth, in internal/graphlock, that only a sweep found. Nothing failed,
// because nothing compared the citations with the document.
//
// The same drift had happened before and was still in the tree when this gate
// was written: `GRAPH.md § Recovered Schema on Both Paths` was renamed to
// `§ Recovered Schema on Every Surface` at commit 40d1b37, and two citations kept
// the old name for a whole release.
//
// # What it checks
//
// Every `COMMANDS.md § <Name> <Section>` citation written in a Go file, for the
// four section suffixes above, must name a section `SPEC/COMMANDS.md` actually
// publishes — as a `#` heading, or as the **bolded label** the document uses for
// a block like `**Error Output:**`. Both sides are read: the citations are swept
// out of the Go source and the section names are parsed out of the document, so
// neither side is a list somebody has to remember to update here.
//
// # Why this shape rather than every citation in the tree
//
// A general gate over every `§` citation in Go would have to accept the tree's
// other, legitimate citation form: SPEC files also name **bolded rules** that are
// not headings at all — `MODELS.md § Free-Text UTF-8 Encoding Constraint` is one
// of about two dozen — and a gate that failed on those would need an exemption
// list longer than the rule it enforces. The four section suffixes are different:
// they are structural, they exist only as headings, and a citation of one is
// always a claim about a heading. That makes this gate total over the class it
// covers and free of exemptions.
//
// SPEC-to-SPEC citations are already gated, by a different measurement, in
// internal/models/spec_xref_test.go. This one covers the side that gate does not
// read: the Go source.
func TestSpecCommandSectionCitationsResolve(t *testing.T) {
	root := moduleRoot(t)

	commands := filepath.Join(root, "SPEC", "COMMANDS.md")
	raw, err := os.ReadFile(commands) //nolint:gosec // a fixed path under the module root
	if err != nil {
		t.Fatalf("reading %s: %v", commands, err)
	}
	// The document publishes a section under two forms, and a citation may name
	// either: a `#` heading, and a **bolded label** introducing a block — every
	// command's `**Error Output:**` paragraph is the second form and is cited as
	// `COMMANDS.md § Error Output`. Both are collected, so the gate fails on a
	// name the document does not publish AT ALL rather than on a name published
	// in the form it did not happen to look for.
	headingRe := regexp.MustCompile(`^#{1,6}\s+(.*?)\s*$`)
	labelRe := regexp.MustCompile(`\*\*([^*\n]{3,60}?):?\*\*`)
	headings := map[string]bool{}
	declared := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if m := headingRe.FindStringSubmatch(line); m != nil {
			headings[normaliseHeading(m[1])] = true
			declared++
		}
		for _, m := range labelRe.FindAllStringSubmatch(line, -1) {
			headings[normaliseHeading(m[1])] = true
		}
	}
	if declared < 50 {
		t.Fatalf("SPEC/COMMANDS.md yielded %d headings; the gate is not reading the document",
			declared)
	}

	// `COMMANDS.md § <Name> <Suffix>` where <Name> is one or more capitalised
	// words. The suffixes are the structural section families every command and
	// subcommand of COMMANDS.md carries.
	cite := regexp.MustCompile(
		`COMMANDS\.md\s+§\s+((?:[A-Z][A-Za-z/-]*\s+)+?(?:Options|Exit Codes|Error Cases|Output))\b`)

	type site struct {
		file    string
		line    int
		heading string
	}
	var unresolved []site
	checked := 0
	resolved := map[string]bool{}

	for _, path := range goFilesUnder(t, root) {
		body, readErr := os.ReadFile(path) //nolint:gosec // a path yielded by the module-root walk
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		for i, line := range strings.Split(string(body), "\n") {
			for _, m := range cite.FindAllStringSubmatch(line, -1) {
				checked++
				h := normaliseHeading(m[1])
				if headings[h] {
					resolved[h] = true
					continue
				}
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				unresolved = append(unresolved, site{file: rel, line: i + 1, heading: m[1]})
			}
		}
	}

	// A gate that swept up nothing would pass without reading anything. The tree
	// carries these citations by the dozen; the floor only has to rule out zero,
	// and it must have RESOLVED some, or "found no unresolved citation" would be
	// a statement about an empty set.
	if checked == 0 || len(resolved) == 0 {
		t.Fatalf("the gate matched %d citations and resolved %d distinct headings; it is not "+
			"reaching the source, so its silence establishes nothing", checked, len(resolved))
	}

	for _, s := range unresolved {
		t.Errorf("%s:%d cites `SPEC/COMMANDS.md § %s`, which that document does not declare.\n"+
			"    A command's section family — Options, Exit Codes, Error Cases, Output — is deleted "+
			"with the command, so a citation of one outlives its target silently. Point the comment "+
			"at the section that now carries the material.", s.file, s.line, s.heading)
	}
	t.Logf("checked %d COMMANDS.md section citations in Go, resolving %d distinct headings",
		checked, len(resolved))
}

// normaliseHeading flattens whitespace and strips the inline markup a heading may
// carry, so `### `audit` Table` and a citation of "audit Table" compare equal. It
// strips nothing else — no numeric prefix, no trailing parenthetical — for the
// reason internal/models/spec_xref_test.go measured and records: normalising
// further is what manufactures false positives.
func normaliseHeading(s string) string {
	s = strings.NewReplacer("`", "", "**", "", "*", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// moduleRoot walks up from the working directory to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolving the working directory: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the working directory")
		}
		dir = parent
	}
}

// goFilesUnder lists every .go file in the module, test files included: a stale
// citation in a test comment misleads the next reader exactly as one in
// production code does.
func goFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "bin", "node_modules", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	sort.Strings(out)
	return out
}
