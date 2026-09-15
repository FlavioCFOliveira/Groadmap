package testenv

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file is the regression gate for rmp task #423.
//
// # The defect it closes
//
// A SPEC file that carries acceptance criteria numbers them, and code, tests and
// documentation cite them by number. SPEC/GRAPH.md numbers its criteria in one
// list, and that list was renumbered when criteria were withdrawn and then
// appended to, while the citations kept the numbers they were written with. Some
// named a criterion the list no longer reaches; the rest resolved to a criterion
// about something else. Nothing compared a citation with the list it cites.
//
// # What it checks
//
// Every citation that binds a criterion number to one SPEC file, in a .go, .py
// or .md file of the repository, names a number that file's criteria reach: no
// greater than the length of the longest numbered acceptance-criteria list the
// file carries. A file whose criteria are unnumbered, or that carries none,
// reaches no number.
//
// A citation binds a number to a file when, within one sentence, that file is
// the only SPEC file named before the number and nothing but section references
// stands between the two:
//
//	SPEC/GRAPH.md acceptance criterion 31
//	SPEC/COMMANDS.md § Edit Task, criterion 6
//	SPEC/GRAPH.md section "Socket Path Length", Acceptance Criteria 64-67
//	SPEC/GRAPH.md "Schema Management" and Acceptance Criteria 32 to 38
//	SPEC/WEB.md AC156 and AC157
//
// The last is the abbreviated label form. Most labels name no file in their
// sentence, and those are not read: a label is bound to a file by the same rule as
// a criterion phrase, or not at all.
//
// A sentence that names two SPEC files before the number does not say which one
// it cites; a file named inside a parenthesis the number stands outside of is not
// the number's file; and a criterion followed by "of rmp task" belongs to a task,
// not to the specification. None of the three is read.
//
// # What it does not check
//
// The subject. A number inside a file's reach can still name the wrong
// criterion, and only reading the criterion settles that. The gate catches the
// citation a renumbering pushed past the end of its list, and the one that names
// a file whose criteria it was never about.
//
// # What it does not read
//
// SPEC/, where each file numbers and cites its own list. CHANGELOG.md and
// release-notes/, which record what a release shipped in the numbering of that
// release and are not corrected afterwards. bin/ and hidden directories, which
// hold build output and tool configuration rather than the project's sources
// and documentation.

// criterionCitationFloor is a floor under the sweep, not a count to keep equal.
// The sweep read 184 citations when this gate was written. One that reads fewer
// than this has stopped recognising the forms above, and a gate that reads
// nothing passes everything.
const criterionCitationFloor = 100

// criterionCitationExcludedDirs are the directories, relative to the module
// root, the sweep does not enter, for the reasons the file comment gives.
var criterionCitationExcludedDirs = map[string]bool{
	"SPEC":          true,
	"release-notes": true,
	"bin":           true,
}

// criterionCitationExcludedFiles are the files, relative to the module root, the
// sweep does not read.
var criterionCitationExcludedFiles = map[string]bool{
	"CHANGELOG.md": true,
}

var (
	// criteriaMarker opens a criteria list: a heading at any level, or the
	// bolded label several SPEC files use.
	criteriaMarker = regexp.MustCompile(`(?i)^(?:#+\s+acceptance criteria|\*\*acceptance criteria:\*\*)\s*$`)
	// markdownHeading ends a criteria list.
	markdownHeading = regexp.MustCompile(`^#+\s`)
	// numberedItem is one criterion: a number at the start of a line.
	numberedItem = regexp.MustCompile(`^(\d+)\.\s`)

	// criterionPhrase is the number part of a citation, with every number of a
	// list or a range: "acceptance criterion 31", "criteria 2, 6 and 7",
	// "Acceptance Criteria 64-67".
	criterionPhrase = regexp.MustCompile(`(?i)\b(?:acceptance\s+)?criteri(?:on|a)\s+(\d+(?:\s*(?:,|and|or|to|-|–)\s*\d+)*)`)
	// criterionLabel is the abbreviated form of the same citation, with every
	// label of a list or a range: "AC31", "AC 20", "AC156 and AC157", "AC1-AC22".
	// It is case-sensitive, so a lowercase `ac156` inside a fixture value is not a
	// label.
	criterionLabel = regexp.MustCompile(`\bAC[ -]?(\d+(?:\s*(?:,|and|or|to|-|–|/)\s*AC[ -]?\d+)*)`)
	// sentenceEnd is a full stop, question mark or exclamation mark followed by
	// whitespace and the start of a sentence; a dotted version number is not one.
	sentenceEnd = regexp.MustCompile("[.!?]\\s+[A-Z`\"'(\\[]")
	// citationGap is what may stand between a SPEC file and its number: nothing,
	// or a section reference and the punctuation around it.
	citationGap = regexp.MustCompile(`(?i)^\s*(?:$|§|"|sections?\b|,|;)`)
	// taskCriterion follows a number that belongs to a task.
	taskCriterion = regexp.MustCompile(`(?i)^\s+of\s+(?:rmp\s+)?task`)
	// criterionNumber is one number of a criterion phrase.
	criterionNumber = regexp.MustCompile(`\d+`)
)

// criterionCitation is one citation the sweep read.
type criterionCitation struct {
	path    string // the citing file, relative to the module root
	file    string // the SPEC file cited, such as "GRAPH.md"
	text    string // the citation, from the file's name to its last number
	numbers []int
}

// TestSpecCriterionCitationsStayWithinTheirFile is the gate: every citation the
// sweep reads names a number its SPEC file's criteria reach.
func TestSpecCriterionCitationsStayWithinTheirFile(t *testing.T) {
	root := moduleRoot(t)
	reach, specFile := specCriteriaReach(t, root)
	citations := sweepCriterionCitations(t, root, specFile)

	if len(citations) < criterionCitationFloor {
		t.Fatalf("the sweep read %d criterion citations, fewer than the floor of %d; the readers have "+
			"stopped recognising the citation forms, and a gate that reads nothing passes everything",
			len(citations), criterionCitationFloor)
	}

	var problems []string
	for _, c := range citations {
		for _, n := range c.numbers {
			if n > reach[c.file] {
				problems = append(problems, fmt.Sprintf("%s: %q cites criterion %d of SPEC/%s, whose "+
					"longest numbered acceptance-criteria list ends at %d", c.path, c.text, n, c.file, reach[c.file]))
				break
			}
		}
	}
	if len(problems) > 0 {
		t.Errorf("%d citation(s) name a criterion their SPEC file does not number. Read the criterion the "+
			"citation means in the SPEC file and correct the number; never renumber the SPEC:\n  %s",
			len(problems), strings.Join(problems, "\n  "))
	}
}

// TestSpecCriterionCitationGate_Discriminates holds both readers to cases whose
// verdicts are known, so a reader that stops matching, or starts matching too
// much, fails here instead of passing the tree.
func TestSpecCriterionCitationGate_Discriminates(t *testing.T) {
	reachCases := []struct {
		name    string
		content string
		want    int
	}{
		{"a heading list, continuation lines and a blank line inside it",
			"## Acceptance Criteria\n\n1. First.\n   continued\n2. Second.\n\n3. Third.\n\n## See Also\n\n4. Not a criterion.\n", 3},
		{"a bolded list stops at the first gap in its numbering",
			"**Acceptance criteria:**\n\n1. One.\n3. Three.\n", 1},
		{"the longest of two lists",
			"**Acceptance criteria:**\n1. a\n2. b\n### Next\n**Acceptance criteria:**\n1. a\n", 2},
		{"an unnumbered checklist reaches nothing",
			"## Acceptance Criteria\n- [ ] Builds.\n", 0},
		{"a marker inside fenced code opens nothing",
			"```\n## Acceptance Criteria\n1. a\n```\n", 0},
	}
	for _, c := range reachCases {
		if got := criteriaReach(c.content); got != c.want {
			t.Errorf("criteriaReach, %s: got %d, want %d", c.name, got, c.want)
		}
	}

	// The cases name two SPEC files that do not exist, so the sweep above, which
	// reads this file too, finds no citation in them.
	specFile := regexp.MustCompile(`\b(?:SPEC/)?(ALPHA|BETA)\.md\b`)
	citationCases := []struct {
		name    string
		path    string
		content string
		want    []string
	}{
		{"a bare citation", "a.go",
			"// SPEC/ALPHA.md acceptance criterion 79 is gone.", []string{"ALPHA.md:79"}},
		{"a citation wrapped across comment lines, after section references", "a.go",
			"// (SPEC/ALPHA.md § Node Key Uniqueness; § Auditing; acceptance\n// criteria 31 and 33).",
			[]string{"ALPHA.md:31,33"}},
		{"a quoted section and a range", "a.py",
			`"""SPEC/ALPHA.md section "Socket Path Length", Acceptance Criteria 64-67."""`,
			[]string{"ALPHA.md:64,67"}},
		{"two SPEC files before the number", "a.go",
			"// (SPEC/ALPHA.md § Budget; SPEC/BETA.md § Graph; acceptance criteria 39 and 40)", nil},
		{"prose between the file and the number", "a.go",
			"// SPEC/ALPHA.md rules out a toolchain, so criterion 124 is measured.", nil},
		{"the file inside a parenthesis the number stands outside of", "a.go",
			"// (SPEC/ALPHA.md § Table), so the count holds (Acceptance Criterion 82).", nil},
		{"the number in a later sentence", "a.md",
			"SPEC/ALPHA.md is canonical. Criterion 99 is not about it.", nil},
		{"a task's criterion", "a.go",
			"// SPEC/ALPHA.md § Lock, acceptance criterion 3 of rmp task #266.", nil},
		{"a comment does not join the code above it", "a.go",
			"x := \"SPEC/ALPHA.md\"\n// acceptance criterion 99", nil},
		{"an abbreviated label", "a.go",
			"// Cartesian-product advisory (SPEC/ALPHA.md AC 79).", []string{"ALPHA.md:79"}},
		{"labels after a file, and a label the sentence binds to no file", "a.py",
			`"""(SPEC/ALPHA.md AC156 and AC157, canonical for the endpoint's half of AC34)."""`,
			[]string{"ALPHA.md:156,157"}},
		{"a lowercase label inside a value", "a.py",
			`query = "SPEC/ALPHA.md ac156"`, nil},
	}
	for _, c := range citationCases {
		var got []string
		for _, p := range citationParagraphs(c.path, c.content) {
			for _, cite := range citationsIn(p, specFile) {
				numbers := make([]string, len(cite.numbers))
				for i, n := range cite.numbers {
					numbers[i] = strconv.Itoa(n)
				}
				got = append(got, cite.file+":"+strings.Join(numbers, ","))
			}
		}
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("citationsIn, %s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// specCriteriaReach reads every SPEC file and returns how far each one's criteria
// reach, keyed by file name, with the expression that recognises a mention of any
// of them. SPEC/README.md is the specification's index and carries no criteria,
// and a bare `README.md` in the repository names the project's own README, so it
// is left out of the expression.
func specCriteriaReach(t *testing.T, root string) (map[string]int, *regexp.Regexp) {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(root, "SPEC"))
	if err != nil {
		t.Fatalf("listing SPEC/: %v", err)
	}
	reach := map[string]int{}
	var names []string
	numbered := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".md" || name == "README.md" {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(root, "SPEC", name)) //nolint:gosec // a file listed from SPEC/ under the module root
		if readErr != nil {
			t.Fatalf("reading SPEC/%s: %v", name, readErr)
		}
		reach[name] = criteriaReach(string(raw))
		if reach[name] > 0 {
			numbered++
		}
		names = append(names, regexp.QuoteMeta(strings.TrimSuffix(name, ".md")))
	}
	if numbered == 0 {
		t.Fatal("no SPEC file carries a numbered acceptance-criteria list; the list reader has stopped matching")
	}
	sort.Strings(names)
	return reach, regexp.MustCompile(`\b(?:SPEC/)?(` + strings.Join(names, "|") + `)\.md\b`)
}

// criteriaReach returns the length of the longest numbered acceptance-criteria
// list in a SPEC file's content: after a criteria marker and before the next
// heading, the items numbered from 1 without a gap at the start of a line.
// Fenced code opens no list and carries no item.
func criteriaReach(content string) int {
	lines := strings.Split(content, "\n")
	longest := 0
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !criteriaMarker.MatchString(line) {
			continue
		}
		next, fence := 1, false
		for _, item := range lines[i+1:] {
			if strings.HasPrefix(item, "```") {
				fence = !fence
				continue
			}
			if fence {
				continue
			}
			if markdownHeading.MatchString(item) {
				break
			}
			m := numberedItem.FindStringSubmatch(item)
			if m == nil {
				continue
			}
			if n, err := strconv.Atoi(m[1]); err != nil || n != next {
				break
			}
			next++
		}
		longest = max(longest, next-1)
	}
	return longest
}

// sweepCriterionCitations reads every citation in the .go, .py and .md files
// under root, other than those the file comment excludes.
func sweepCriterionCitations(t *testing.T, root string, specFile *regexp.Regexp) []criterionCitation {
	t.Helper()

	var out []criterionCitation
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel != "." && (strings.HasPrefix(entry.Name(), ".") || criterionCitationExcludedDirs[rel]) {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".py", ".md":
		default:
			return nil
		}
		if criterionCitationExcludedFiles[rel] {
			return nil
		}
		raw, readErr := os.ReadFile(path) //nolint:gosec // a file the walk found under the module root
		if readErr != nil {
			return readErr
		}
		for _, paragraph := range citationParagraphs(path, string(raw)) {
			for _, c := range citationsIn(paragraph, specFile) {
				c.path = rel
				out = append(out, c)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	return out
}

// citationParagraphs joins a file's lines into the runs a citation can span:
// consecutive non-blank lines of one kind, where a comment line never joins a
// line that is not one. Comment markers are removed, so a citation wrapped
// across comment lines reads as the one sentence it is.
func citationParagraphs(path, content string) []string {
	const (
		kindNone = iota
		kindComment
		kindOther
	)
	var out, current []string
	kind := kindNone
	flush := func() {
		if len(current) > 0 {
			out = append(out, strings.Join(current, " "))
			current = nil
		}
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		k := kindOther
		switch {
		case strings.HasSuffix(path, ".go") && strings.HasPrefix(line, "//"):
			k, line = kindComment, strings.TrimSpace(strings.TrimPrefix(line, "//"))
		case strings.HasSuffix(path, ".py") && strings.HasPrefix(line, "#"):
			k, line = kindComment, strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
		if line == "" || k != kind {
			flush()
			kind = k
			if line == "" {
				continue
			}
		}
		current = append(current, line)
	}
	flush()
	return out
}

// citationsIn returns the citations one paragraph makes, by the binding rule the
// file comment states.
func citationsIn(paragraph string, specFile *regexp.Regexp) []criterionCitation {
	return append(citationsBy(criterionPhrase, paragraph, specFile), citationsBy(criterionLabel, paragraph, specFile)...)
}

// citationsBy returns the citations one paragraph makes in the form phrase
// recognises: a criterion phrase or an abbreviated label.
func citationsBy(phrase *regexp.Regexp, paragraph string, specFile *regexp.Regexp) []criterionCitation {
	var out []criterionCitation
	for _, loc := range phrase.FindAllStringSubmatchIndex(paragraph, -1) {
		if taskCriterion.MatchString(paragraph[loc[1]:]) {
			continue
		}
		sentence := paragraph[:loc[0]]
		if ends := sentenceEnd.FindAllStringIndex(sentence, -1); len(ends) > 0 {
			sentence = sentence[ends[len(ends)-1][0]+1:]
		}
		mentions := specFile.FindAllStringSubmatchIndex(sentence, -1)
		if len(mentions) == 0 {
			continue
		}
		files := map[string]bool{}
		for _, m := range mentions {
			files[sentence[m[2]:m[3]]] = true
		}
		if len(files) != 1 {
			continue
		}
		last := mentions[len(mentions)-1]
		gap := sentence[last[1]:]
		if !citationGap.MatchString(gap) || closesAnOuterParenthesis(gap) {
			continue
		}
		var numbers []int
		for _, digits := range criterionNumber.FindAllString(paragraph[loc[2]:loc[3]], -1) {
			n, err := strconv.Atoi(digits)
			if err != nil {
				// Too large to parse is beyond every list.
				n = math.MaxInt
			}
			numbers = append(numbers, n)
		}
		out = append(out, criterionCitation{
			file:    sentence[last[2]:last[3]] + ".md",
			text:    sentence[last[0]:] + paragraph[loc[0]:loc[1]],
			numbers: numbers,
		})
	}
	return out
}

// closesAnOuterParenthesis reports whether gap closes a parenthesis it did not
// open, which puts the file named before the gap inside a parenthesis the number
// after it stands outside of.
func closesAnOuterParenthesis(gap string) bool {
	depth := 0
	for _, r := range gap {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return true
			}
		}
	}
	return false
}
