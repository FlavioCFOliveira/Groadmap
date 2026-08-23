// Package models — specification cross-reference gate.
//
// This is the third specification-side gate file in this package, and the first
// that measures the SPEC against itself rather than against the code. The
// precedents — spec_enum_coverage_test.go and docs_enum_coverage_test.go — each
// pin one region of one document against an enum. This one resolves every
// cross-reference in SPEC/ against the headings that actually exist, so a rename
// that orphans a reference is caught by `make check` instead of by the next
// person to follow the link.
//
// The defect class this closes has been found twice by hand. A SPEC file cites a
// section of another SPEC file by name — "DATABASE.md § `audit` Table" — the
// section is later renamed, and the citation is left pointing at a heading no
// document declares. Nothing in the build noticed, because until now nothing in
// the build read a cross-reference.
//
// What the gate checks, and what it deliberately does not.
//
// It checks EXISTENCE: the cited heading must be declared by the cited document.
// It does NOT check uniqueness. Eleven heading names are declared more than once
// inside their own file today — "Exit Codes" three times in COMMANDS.md, "Audit"
// twice in DATABASE.md, and so on — and three references in WEB.md cite
// `DATABASE.md § Audit`, which therefore resolves to either of two sections. Those
// references resolve; they are simply not unique, and disambiguating them is a
// separate piece of work. Making this gate demand uniqueness would fail the tree
// today for a reason the acceptance criterion does not ask about.
//
// It does not require a citation to be written in any particular form either.
// Five forms exist in the tree; all five are recognised, and no new policy is
// imposed on which one a future reference should use.
//
// The matching rule.
//
// Every element of it was measured against the tree before it was written, and
// each is load-bearing:
//
//   - Whitespace runs are flattened to a single space on both sides first,
//     because references wrap across lines.
//   - Inline markup is stripped from the HEADING side only. 60 of the 893
//     references depend on this alone: `### `audit` Table` is cited as
//     "DATABASE.md § audit Table". No reference in the tree contains a backtick,
//     so the reference side needs no stripping and gets none.
//   - Comparison is case-sensitive. Zero references need case folding, so folding
//     would be looseness bought for nothing.
//   - NOTHING else is stripped from the heading: not a numeric prefix, not a
//     trailing parenthetical, not an anchor. 833 of the 893 references match the
//     raw heading byte for byte once whitespace is flattened. Normalising the
//     heading further and then requiring the raw reference to match the stripped
//     form is what manufactured the false positives that produced this task: it
//     flags CORRECT references, such as `VERSION.md`'s citation of
//     `DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN)`.
//     TestSpecXref_HeadingNormalisationStripsMarkupAndNothingElse pins that.
//   - A cited file name may carry a directory prefix, so files are compared by
//     basename, and the repository-root CLAUDE.md is a legitimate target
//     alongside SPEC/*.md.
//
// Why the gate cannot quietly stop working.
//
// A cross-reference checker that recognises nothing resolves nothing and passes.
// Two independent guards make that impossible:
//
//  1. Totality. Every section mark in every SPEC file must be claimed by exactly
//     one recognised reference. If a scanner stops matching, its section marks go
//     unclaimed and the reconciliation fails naming each one.
//  2. Floors. The totality check is itself vacuous if the section marks are never
//     found, so the number of documents, headings, references of the dominant
//     form, references in total, and anchor links must each clear a floor.
//
// Between them every way of breaking recognition is covered: if the code-span
// scanner dies, the prose fallback absorbs its marks and the code-span floor
// fails; if the prose scanner dies, totality fails; if the section-mark search
// itself dies, the floors fail.
//
// Why this file lives in internal/models: the same reason the other two do. It
// needs no unexported access to anything, only files on disk, and it sits beside
// the gates a reader already knows to look for.
package models

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The corpus. SPEC/*.md are both citing documents and targets; the
// repository-root CLAUDE.md is a target only, because SPEC/VERSION.md cites its
// "Versioning Policy" section and nothing in CLAUDE.md cites the SPEC back.
const (
	specDirRelPath   = "SPEC"
	rootGuideRelPath = "CLAUDE.md"
)

// sectionMark is the character every cross-reference is written with.
const sectionMark = "§"

// Floors below which a count is treated as evidence that the scan has stopped
// working rather than as evidence about the SPEC. The tree holds 14 documents
// under SPEC/, 647 headings across those and CLAUDE.md, 880 code-span
// references, 893 references in total and 825 anchor links; each floor sits far
// enough under its measurement that ordinary editing cannot trip it, and far
// enough over zero that a gate measuring nothing cannot report success.
const (
	minSpecDocuments      = 10
	minSpecHeadings       = 400
	minCodeSpanReferences = 600
	minSectionReferences  = 700
	minAnchorReferences   = 500
)

// minParenthesisedHeadings keeps the byte-for-byte rule honest from the corpus
// side. At least one reference must resolve to a heading whose name ends in a
// closing parenthesis, so the tree still holds the case that a "helpful"
// normalisation would break. 58 do today, among them every citation of
// DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN); the floor is 1
// because the rule needs one witness, not a census.
const minParenthesisedHeadings = 1

// ---------------------------------------------------------------------------
// Corpus
// ---------------------------------------------------------------------------

// xrefHeading is one heading declaration inside a document.
type xrefHeading struct {
	text string // exactly as written, inline markup included
	name string // comparison form: markup stripped, whitespace flattened
	line int
}

// xrefDoc is one Markdown document of the corpus.
//
// body is raw with every line of every fenced code block replaced by spaces.
// The replacement is byte for byte, so an offset into body indexes the same
// position in raw; that is what lets the fenced and unfenced scans share one set
// of claimed offsets.
type xrefDoc struct {
	name     string // basename, the key a reference cites
	rel      string // repository-relative path, for failure messages
	raw      string
	body     string
	headings []xrefHeading
	names    map[string]bool
	anchors  map[string]bool
}

// loadXrefCorpus reads SPEC/*.md as citing documents and returns them together
// with the target index, which adds the repository-root CLAUDE.md.
func loadXrefCorpus(t *testing.T) ([]*xrefDoc, map[string]*xrefDoc) {
	t.Helper()

	root := repoRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, specDirRelPath, "*.md"))
	if err != nil {
		t.Fatalf("listing %s/*.md: %v", specDirRelPath, err)
	}

	citing := make([]*xrefDoc, 0, len(paths))
	targets := make(map[string]*xrefDoc, len(paths)+1)
	for _, p := range paths {
		rel := specDirRelPath + "/" + filepath.Base(p)
		doc := newXrefDoc(filepath.Base(p), rel, readRepoFile(t, rel))
		citing = append(citing, doc)
		targets[doc.name] = doc
	}

	if _, err := os.Stat(filepath.Join(root, rootGuideRelPath)); err == nil {
		guide := newXrefDoc(rootGuideRelPath, rootGuideRelPath, readRepoFile(t, rootGuideRelPath))
		targets[guide.name] = guide
	} else {
		t.Fatalf("%s is a documented cross-reference target but is not readable: %v", rootGuideRelPath, err)
	}

	return citing, targets
}

// newXrefDoc indexes one document by heading name and by anchor slug.
func newXrefDoc(name, rel, raw string) *xrefDoc {
	doc := &xrefDoc{name: name, rel: rel, raw: raw, body: blankFencedBlocks(raw)}
	doc.headings = atxHeadings(doc.body)
	doc.names = make(map[string]bool, len(doc.headings))
	doc.anchors = make(map[string]bool, len(doc.headings))
	for _, h := range doc.headings {
		doc.names[h.name] = true
		doc.anchors[uniqueAnchor(doc.anchors, headingAnchor(h.text))] = true
	}
	return doc
}

var fenceOpenerPattern = regexp.MustCompile("^[ \t]{0,3}(`{3,}|~{3,})")

// blankFencedBlocks replaces every byte of every fenced-code-block line with a
// space. Blanking rather than deleting is what preserves byte offsets, so a
// position found in the blanked text points at the same character of the
// original; blanking by byte rather than by rune is what keeps that true across
// the multi-byte section mark.
func blankFencedBlocks(raw string) string {
	lines := strings.Split(raw, "\n")
	out := make([]string, len(lines))
	var fence byte
	inFence := false
	for i, line := range lines {
		marker := fenceOpenerPattern.FindStringSubmatch(line)
		switch {
		case !inFence && marker != nil:
			inFence, fence = true, marker[1][0]
		case inFence && marker != nil && marker[1][0] == fence:
			inFence = false
		case !inFence:
			out[i] = line
			continue
		}
		out[i] = strings.Repeat(" ", len(line))
	}
	return strings.Join(out, "\n")
}

var atxHeadingPattern = regexp.MustCompile(`(?m)^(#{1,6})[ \t]+(.+?)[ \t]*#*[ \t]*$`)

// atxHeadings returns the ATX headings of an already-blanked body, in document
// order. A heading inside a fenced code block is sample text, not a declaration,
// and blanking has already removed it.
func atxHeadings(body string) []xrefHeading {
	matches := atxHeadingPattern.FindAllStringSubmatchIndex(body, -1)
	headings := make([]xrefHeading, 0, len(matches))
	for _, m := range matches {
		text := body[m[4]:m[5]]
		headings = append(headings, xrefHeading{
			text: text,
			name: normaliseHeading(text),
			line: lineOf(body, m[0]),
		})
	}
	return headings
}

// uniqueAnchor mirrors the disambiguation a Markdown renderer applies when two
// headings slug to the same anchor: the second gets -1, the third -2.
func uniqueAnchor(taken map[string]bool, base string) string {
	if !taken[base] {
		return base
	}
	for n := 1; ; n++ {
		candidate := base + "-" + strconv.Itoa(n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// headingAnchor is the slug a heading is reachable by: inline markup stripped,
// lowercased, everything that is not a word character, a hyphen or a space
// removed, spaces turned into hyphens.
func headingAnchor(text string) string {
	s := strings.ToLower(strings.TrimSpace(stripInlineMarkup(text)))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || isWordRune(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func lineOf(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

// ---------------------------------------------------------------------------
// Normalisation
// ---------------------------------------------------------------------------

var (
	backtickRunPattern   = regexp.MustCompile("`+")
	boldPattern          = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicStarPattern    = regexp.MustCompile(`\*(.+?)\*`)
	inlineLinkPattern    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	whitespaceRunPattern = regexp.MustCompile(`\s+`)
)

// normaliseHeading is the comparison form of a heading name. It removes the
// inline markup a heading is decorated with and flattens whitespace, and it does
// nothing else — no case folding, no numeric prefix, no trailing parenthetical,
// no anchor. See the package comment for why each of those omissions matters.
func normaliseHeading(text string) string {
	return flattenWhitespace(stripInlineMarkup(text))
}

// stripInlineMarkup removes code ticks, bold, emphasis and link syntax, leaving
// the text a reader sees rendered. Only the heading side is passed through it:
// no reference in the tree is written with markup, so applying it to a citation
// would only create ways for a correct citation to be misread.
func stripInlineMarkup(s string) string {
	s = backtickRunPattern.ReplaceAllString(s, "")
	s = boldPattern.ReplaceAllString(s, "$1")
	s = italicStarPattern.ReplaceAllString(s, "$1")
	s = stripUnderscoreEmphasis(s)
	return inlineLinkPattern.ReplaceAllString(s, "$1")
}

// stripUnderscoreEmphasis removes the delimiters of _underscore emphasis_ and
// only those.
//
// CommonMark does not recognise an intraword underscore as emphasis, and that
// exclusion is what keeps this gate from inventing failures. Three headings in
// the tree carry intraword underscores — DEPLOY.md's `is_raspberry_pi()` and
// `get_download_url(version, arch)`, and VERSION.md's
// "Reclassifying `TASK_STATUS_CHANGE`". A rule that treated every pair of
// underscores as emphasis would index them as "israspberrypi()",
// "getdownloadurl(version, arch)" and "Reclassifying TASKSTATUSCHANGE", so the
// first correct citation any of them ever receives would be reported as dangling.
// Nothing cites them today, which is the only reason such a rule has not already
// produced a false positive.
func stripUnderscoreEmphasis(s string) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(runes); i++ {
		if runes[i] != '_' || !opensUnderscoreEmphasis(runes, i) {
			b.WriteRune(runes[i])
			continue
		}
		closer := -1
		for j := i + 1; j < len(runes); j++ {
			if runes[j] == '_' && closesUnderscoreEmphasis(runes, j) {
				closer = j
				break
			}
		}
		if closer < 0 || closer == i+1 {
			b.WriteRune(runes[i])
			continue
		}
		b.WriteString(string(runes[i+1 : closer]))
		i = closer
	}
	return b.String()
}

// opensUnderscoreEmphasis reports whether the underscore at i can open emphasis:
// something other than a space must follow it, and no word character may precede
// it.
func opensUnderscoreEmphasis(runes []rune, i int) bool {
	if i+1 >= len(runes) || unicode.IsSpace(runes[i+1]) {
		return false
	}
	return i == 0 || !isWordRune(runes[i-1])
}

// closesUnderscoreEmphasis is the mirror image: something other than a space must
// precede it, and no word character may follow it.
func closesUnderscoreEmphasis(runes []rune, i int) bool {
	if i == 0 || unicode.IsSpace(runes[i-1]) {
		return false
	}
	return i+1 >= len(runes) || !isWordRune(runes[i+1])
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// flattenWhitespace collapses every whitespace run to a single space and trims
// the ends, so a reference that wraps across two lines compares equal to the
// single-line heading it names.
func flattenWhitespace(s string) string {
	return strings.TrimSpace(whitespaceRunPattern.ReplaceAllString(s, " "))
}

// ---------------------------------------------------------------------------
// Recognition
// ---------------------------------------------------------------------------

// xrefKind names the syntactic form a citation is written in. The distinction
// matters for two reasons: where a heading name ends, and what a floor on the
// dominant form can prove.
type xrefKind string

const (
	// xrefCodeSpan covers `FILE.md § Heading` and the same-file `§ Heading`,
	// both written entirely inside one code span. This is the dominant form,
	// and the closing backtick ends the heading name unambiguously.
	xrefCodeSpan xrefKind = "code span"
	// xrefLinkLabel covers [FILE.md § Heading](./FILE.md#anchor), where the
	// closing bracket ends the name.
	xrefLinkLabel xrefKind = "link label"
	// xrefProse covers every section mark that is neither: a file name in a code
	// span followed by the heading in running prose, and a plain
	// FILE.md § Heading written inside a fenced code block, where the fence makes
	// the surrounding text an SQL comment or a JSON string value.
	xrefProse xrefKind = "prose"
)

// xrefRef is one section cross-reference as found in a citing document.
type xrefRef struct {
	from      *xrefDoc
	kind      xrefKind
	line      int
	mark      int    // byte offset of the section mark that produced it
	target    string // basename of the cited document
	heading   string // heading name as cited, whitespace flattened
	text      string // the citation as written, for the failure message
	exact     bool   // the end of the name is unambiguous, so no prefix fallback
	malformed bool   // a section mark this gate could not read as a citation
}

var (
	// A citation that fills a whole code span. The file name is optional; without
	// it the citation is to a section of the citing document itself.
	codeSpanCitationPattern = regexp.MustCompile(
		`(?s)^\s*(?:((?:[A-Za-z0-9_.\-]+/)*[A-Za-z0-9_.\-]+\.md)\s*)?§\s*(.+?)\s*$`)
	// A citation used as the label of a Markdown link.
	linkLabelCitationPattern = regexp.MustCompile(
		`\[\s*((?:[A-Za-z0-9_.\-]+/)*[A-Za-z0-9_.\-]+\.md)?\s*§\s*([^\]]+?)\s*\]\(`)
	// The file name a prose citation attaches to: the last thing before the
	// section mark on that line, optionally closed by a backtick.
	fileBeforeMarkPattern = regexp.MustCompile("((?:[A-Za-z0-9_.\\-]+/)*[A-Za-z0-9_.\\-]+\\.md)`?[ \t]*$")
)

// codeSpan records one CommonMark code span: a run of N backticks closes at the
// next run of exactly N.
type codeSpan struct {
	contentStart int
	contentEnd   int
}

func codeSpans(body string) []codeSpan {
	runs := backtickRunPattern.FindAllStringIndex(body, -1)
	spans := make([]codeSpan, 0, len(runs)/2)
	for i := 0; i < len(runs); {
		width := runs[i][1] - runs[i][0]
		j := i + 1
		for j < len(runs) && runs[j][1]-runs[j][0] != width {
			j++
		}
		if j >= len(runs) {
			i++
			continue
		}
		spans = append(spans, codeSpan{contentStart: runs[i][1], contentEnd: runs[j][0]})
		i = j + 1
	}
	return spans
}

// scanSectionRefs returns every citation in one document, in the order the three
// scans run. Each citation records the offset of the single section mark it was
// built from, which is what the totality reconciliation checks.
func scanSectionRefs(doc *xrefDoc) []xrefRef {
	refs := make([]xrefRef, 0, strings.Count(doc.raw, sectionMark))
	claimed := make(map[int]bool, cap(refs))

	// Form 1 and 2: the whole citation lives inside one code span.
	for _, span := range codeSpans(doc.body) {
		content := doc.body[span.contentStart:span.contentEnd]
		mark := strings.Index(content, sectionMark)
		if mark < 0 {
			continue
		}
		offset := span.contentStart + mark
		claimed[offset] = true
		ref := xrefRef{
			from:  doc,
			kind:  xrefCodeSpan,
			line:  lineOf(doc.body, offset),
			mark:  offset,
			text:  "`" + flattenWhitespace(content) + "`",
			exact: true,
		}
		m := codeSpanCitationPattern.FindStringSubmatch(content)
		if m == nil {
			ref.malformed = true
		} else {
			ref.target = citedFile(doc, m[1])
			ref.heading = flattenWhitespace(m[2])
		}
		refs = append(refs, ref)
	}

	// Form 4: the citation is the label of a Markdown link.
	for _, m := range linkLabelCitationPattern.FindAllStringSubmatchIndex(doc.body, -1) {
		offset := m[0] + strings.Index(doc.body[m[0]:m[1]], sectionMark)
		if claimed[offset] {
			continue
		}
		claimed[offset] = true
		refs = append(refs, xrefRef{
			from:    doc,
			kind:    xrefLinkLabel,
			line:    lineOf(doc.body, offset),
			mark:    offset,
			target:  citedFile(doc, submatch(doc.body, m, 1)),
			heading: flattenWhitespace(submatch(doc.body, m, 2)),
			text:    flattenWhitespace(doc.body[m[0]:m[1]]),
			exact:   true,
		})
	}

	// Forms 3 and 5: everything else. These are read from raw rather than from
	// body, because the fenced ones are blanked out of body by design.
	for offset := 0; ; {
		next := strings.Index(doc.raw[offset:], sectionMark)
		if next < 0 {
			break
		}
		offset += next
		if !claimed[offset] {
			claimed[offset] = true
			refs = append(refs, proseRef(doc, offset))
		}
		offset += len(sectionMark)
	}

	return refs
}

// proseRef builds a citation whose heading name has no closing delimiter. The
// candidate runs to the end of the line and the resolver takes the longest
// prefix of it that names a real heading; see resolveHeading for why that is the
// only rule that works for all of them.
func proseRef(doc *xrefDoc, offset int) xrefRef {
	lineStart := strings.LastIndex(doc.raw[:offset], "\n") + 1
	lineEnd := offset + len(sectionMark)
	if n := strings.Index(doc.raw[lineEnd:], "\n"); n >= 0 {
		lineEnd += n
	} else {
		lineEnd = len(doc.raw)
	}

	target := doc.name
	if m := fileBeforeMarkPattern.FindStringSubmatch(doc.raw[lineStart:offset]); m != nil {
		target = path.Base(m[1])
	}

	return xrefRef{
		from:    doc,
		kind:    xrefProse,
		line:    lineOf(doc.raw, offset),
		mark:    offset,
		target:  target,
		heading: flattenWhitespace(doc.raw[offset+len(sectionMark) : lineEnd]),
		text:    flattenWhitespace(doc.raw[lineStart:lineEnd]),
	}
}

// citedFile resolves the file half of a citation, by basename so that a
// directory prefix such as SPEC/DEPLOY.md names the same document as DEPLOY.md,
// and defaulting to the citing document when the citation gives no file.
func citedFile(doc *xrefDoc, cited string) string {
	if cited == "" {
		return doc.name
	}
	return path.Base(cited)
}

// submatch returns capture group n of a FindAllStringSubmatchIndex result, or
// the empty string when the group did not participate.
func submatch(text string, m []int, n int) string {
	if m[2*n] < 0 {
		return ""
	}
	return text[m[2*n]:m[2*n+1]]
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

// resolveHeading reports the heading a citation names, when the cited document
// declares one.
//
// For a citation with a closing delimiter the comparison is exact: the name ends
// where the backtick or the bracket says it ends, and anything else is a
// dangling reference.
//
// For a prose citation there is no delimiter, so where the name ends is
// genuinely undecidable from the text: "ARCHITECTURE.md § Exit Codes,
// ErrAlreadyExists)." ends at a comma, "STATE_MACHINE.md § Task State Machine"
// inside a JSON string ends at the quote, and
// "DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN))." ends at the
// first of two closing parentheses, one of which belongs to the name. The rule
// that settles all three without a special case per punctuation mark is the
// longest prefix that both ends at a word boundary and names a real heading. The
// word-boundary condition is what stops a prefix from resolving by cutting a word
// in half, so a citation of a heading that does not exist still fails.
func resolveHeading(ref *xrefRef, target *xrefDoc) (string, bool) {
	if ref.exact {
		return ref.heading, target.names[ref.heading]
	}
	runes := []rune(ref.heading)
	for n := len(runes); n > 0; n-- {
		if n < len(runes) && isWordRune(runes[n]) {
			continue
		}
		candidate := strings.TrimRight(string(runes[:n]), " ")
		if candidate != "" && target.names[candidate] {
			return candidate, true
		}
	}
	return ref.heading, false
}

// nearestHeading returns the heading of a document closest to a cited name, and
// whether it is close enough to be worth printing. The threshold keeps the
// suggestion honest: a name within a third of its own length of a real heading is
// a typo or a rename, while anything further away would be an invention.
func nearestHeading(target *xrefDoc, cited string) (xrefHeading, bool) {
	best := xrefHeading{}
	bestDistance := -1
	for _, h := range target.headings {
		d := editDistance(cited, h.name)
		if bestDistance < 0 || d < bestDistance {
			best, bestDistance = h, d
		}
	}
	// A distance of zero would mean the citation names a heading exactly, which
	// resolution would already have accepted; the guard is here so a future
	// caller cannot get "did you mean the thing you wrote".
	if bestDistance <= 0 {
		return xrefHeading{}, false
	}
	return best, bestDistance*3 <= len([]rune(cited))
}

// editDistance is the Levenshtein distance between two strings, over runes. It
// runs only when a reference has already failed to resolve, so its cost is paid
// once per defect and never on a green tree.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	previous := make([]int, len(br)+1)
	current := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		current[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			current[j] = min(min(current[j-1]+1, previous[j]+1), previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(br)]
}

// ---------------------------------------------------------------------------
// 1. Every section cross-reference names a heading that exists.
// ---------------------------------------------------------------------------

// TestSpecXref_EverySectionReferenceNamesAnExistingHeading is the gate proper.
// It resolves every "FILE.md § Heading" citation in SPEC/ against the headings
// the cited document declares, and fails naming the citing file and line, the
// target, and the heading that is not there.
func TestSpecXref_EverySectionReferenceNamesAnExistingHeading(t *testing.T) {
	citing, targets := loadXrefCorpus(t)

	headings := 0
	for _, doc := range targets {
		headings += len(doc.headings)
	}
	if len(citing) < minSpecDocuments {
		t.Fatalf("only %d documents were found under %s/, want at least %d; the corpus is not where this "+
			"gate assumes it is, so it is about to resolve nothing and report success",
			len(citing), specDirRelPath, minSpecDocuments)
	}
	if headings < minSpecHeadings {
		t.Fatalf("only %d headings were collected from %d documents, want at least %d; heading collection "+
			"has stopped working, and every reference would then be reported as dangling or none would be "+
			"checked at all", headings, len(targets), minSpecHeadings)
	}

	byKind := make(map[xrefKind]int, 3)
	total, parenthesised := 0, 0
	for _, doc := range citing {
		refs := scanSectionRefs(doc)
		assertSectionMarksAccountedFor(t, doc, refs)

		for i := range refs {
			ref := &refs[i]
			total++
			byKind[ref.kind]++

			if ref.malformed {
				t.Errorf("%s:%d holds a section mark inside a code span that this gate cannot read as a "+
					"cross-reference: %s\nA citation must be written as `FILE.md § Heading`, or as "+
					"`§ Heading` for a section of the same document.", ref.from.rel, ref.line, ref.text)
				continue
			}

			target, known := targets[ref.target]
			if !known {
				t.Errorf("%s:%d cites %s, which is not one of the documents this gate resolves against "+
					"(%s/*.md and the repository-root %s).\n  citation : %s\nEither the document was moved "+
					"or renamed, or the citation names a file that never existed.",
					ref.from.rel, ref.line, ref.target, specDirRelPath, rootGuideRelPath, ref.text)
				continue
			}

			name, ok := resolveHeading(ref, target)
			if !ok {
				t.Errorf("%s:%d cites a heading that %s does not declare.\n  citation : %s\n  target   : "+
					"%s\n  heading  : %q\n%s%sA cross-reference must name a heading that exists, so a "+
					"rename has to carry its citations with it. Fix the citation, or restore the heading.",
					ref.from.rel, ref.line, ref.target, ref.text, target.rel, ref.heading,
					boundaryNote(ref), nearestHeadingHint(target, ref.heading))
				continue
			}
			if strings.HasSuffix(name, ")") {
				parenthesised++
			}
		}
	}

	// The floors. Their job is not to describe the SPEC, it is to make a gate
	// that has stopped recognising references fail instead of passing with
	// nothing to do. See the package comment for how they and the totality
	// reconciliation divide the work between them.
	if byKind[xrefCodeSpan] < minCodeSpanReferences {
		t.Errorf("only %d cross-references were recognised in their dominant form, a citation written "+
			"inside a code span, want at least %d. The code-span scan has stopped matching; the remaining "+
			"section marks are still resolved by the prose fallback, which is lenient enough to hide the "+
			"loss, so this floor is the only thing that reports it.\nrecognised by form: %v",
			byKind[xrefCodeSpan], minCodeSpanReferences, byKind)
	}
	if total < minSectionReferences {
		t.Errorf("only %d cross-references were recognised across %d documents, want at least %d; "+
			"recognition has stopped working and this gate is now checking almost nothing\n"+
			"recognised by form: %v", total, len(citing), minSectionReferences, byKind)
	}
	if parenthesised < minParenthesisedHeadings {
		t.Errorf("no cross-reference resolves to a heading whose name ends in a closing parenthesis, and "+
			"at least %d must. That case — DATABASE.md § Migration Idempotency (ALTER TABLE ADD COLUMN) — "+
			"is what proves the heading name is compared byte for byte rather than stripped of a trailing "+
			"parenthetical, and without it in the corpus that rule stops being exercised",
			minParenthesisedHeadings)
	}

	t.Logf("resolved %d cross-references (%v) over %d headings in %d documents",
		total, byKind, headings, len(targets))
}

// assertSectionMarksAccountedFor is the totality reconciliation: every section
// mark in the document must have produced exactly one citation.
//
// It is what catches a scan that has been removed rather than one that has been
// weakened. Recognition ends in a catch-all — any mark the code-span and
// link-label scans leave behind becomes a prose citation — so a weakened scan is
// covered for, and the per-form floor in the test above is what reports that. A
// scan that stops producing citations altogether leaves its marks unclaimed, and
// this reconciliation names each one.
func assertSectionMarksAccountedFor(t *testing.T, doc *xrefDoc, refs []xrefRef) {
	t.Helper()

	claimed := make(map[int]int, len(refs))
	for i := range refs {
		claimed[refs[i].mark]++
	}

	for offset := 0; ; {
		next := strings.Index(doc.raw[offset:], sectionMark)
		if next < 0 {
			break
		}
		offset += next
		switch claimed[offset] {
		case 1:
		case 0:
			t.Errorf("%s:%d holds a section mark that no scan recognised as a cross-reference, so it is "+
				"resolved against nothing and this gate would pass whatever it names: %q",
				doc.rel, lineOf(doc.raw, offset), sectionMarkContext(doc.raw, offset))
		default:
			t.Errorf("%s:%d holds a section mark that %d citations claim; each mark must produce exactly "+
				"one, or the counts this gate reports no longer measure what it checked: %q",
				doc.rel, lineOf(doc.raw, offset), claimed[offset], sectionMarkContext(doc.raw, offset))
		}
		delete(claimed, offset)
		offset += len(sectionMark)
	}

	for offset, n := range claimed {
		t.Errorf("%s: %d citations were built at byte offset %d, where the document holds no section mark; "+
			"the scans and this reconciliation are reading different text", doc.rel, n, offset)
	}
}

// sectionMarkContext returns the line a section mark sits on, for a failure that
// has to show a reader what was found rather than only where.
func sectionMarkContext(raw string, offset int) string {
	start := strings.LastIndex(raw[:offset], "\n") + 1
	end := len(raw)
	if n := strings.Index(raw[offset:], "\n"); n >= 0 {
		end = offset + n
	}
	return flattenWhitespace(raw[start:end])
}

// boundaryNote explains the heading name printed for a citation that has no
// closing delimiter. The name shown is the whole rest of the line, because that
// is the widest reading of it, and the resolver has already tried every shorter
// one; without the note a reader would think the gate had misread the citation.
func boundaryNote(ref *xrefRef) string {
	if ref.exact {
		return ""
	}
	return "  note     : this citation has no closing backtick or bracket, so the name shown runs to the " +
		"end of the line; no prefix of it names a heading either\n"
}

// nearestHeadingHint renders the "did you mean" line, or nothing when no heading
// is close enough for the suggestion to be a suggestion rather than a guess.
func nearestHeadingHint(target *xrefDoc, cited string) string {
	nearest, ok := nearestHeading(target, cited)
	if !ok {
		return ""
	}
	return "  nearest  : " + strconv.Quote(nearest.name) + " (" + target.rel + ":" +
		strconv.Itoa(nearest.line) + ")\n"
}

// ---------------------------------------------------------------------------
// 2. Every anchor link names a heading that exists.
// ---------------------------------------------------------------------------

var (
	// A Markdown link whose target carries an anchor, with an optional file.
	anchorLinkPattern = regexp.MustCompile(`\]\(\s*(?:\./)?([A-Za-z0-9_.\-]*\.md)?#([^)\s]+)\s*\)`)
	// Every Markdown link whose target contains an anchor at all, whatever its
	// shape. The difference between the two is what the reconciliation reports.
	anyAnchorLinkPattern = regexp.MustCompile(`\]\(([^)\s]*#[^)\s]*)\)`)
)

// TestSpecXref_EveryAnchorLinkNamesAnExistingHeading is the other half of the
// same class. A cross-reference written as [label](#anchor) names a heading by
// its slug rather than by its text, and a rename orphans it exactly as it
// orphans a "§ Heading" citation — with the additional property that the reader
// following it lands on the top of the document rather than on an error.
func TestSpecXref_EveryAnchorLinkNamesAnExistingHeading(t *testing.T) {
	citing, targets := loadXrefCorpus(t)

	total := 0
	for _, doc := range citing {
		recognised := anchorLinkPattern.FindAllStringSubmatchIndex(doc.body, -1)
		assertAnchorLinksAccountedFor(t, doc, recognised)

		for _, m := range recognised {
			total++
			line := lineOf(doc.body, m[0])
			text := doc.body[m[0]:m[1]]
			name := citedFile(doc, submatch(doc.body, m, 1))

			target, known := targets[name]
			if !known {
				t.Errorf("%s:%d links to %s, which is not one of the documents this gate resolves against "+
					"(%s/*.md and the repository-root %s).\n  link : %s",
					doc.rel, line, name, specDirRelPath, rootGuideRelPath, text)
				continue
			}

			anchor := strings.ToLower(submatch(doc.body, m, 2))
			if !target.anchors[anchor] {
				t.Errorf("%s:%d links to an anchor that %s does not declare.\n  link   : %s\n  target : "+
					"%s\n  anchor : %q\nNo heading of that document slugs to it, so the link silently lands "+
					"at the top of the file instead of at the section it names.",
					doc.rel, line, name, text, target.rel, anchor)
			}
		}
	}

	if total < minAnchorReferences {
		t.Errorf("only %d anchor links were recognised across %d documents, want at least %d; recognition "+
			"has stopped working and this gate is now checking almost nothing",
			total, len(citing), minAnchorReferences)
	}

	t.Logf("resolved %d anchor links across %d documents", total, len(citing))
}

// assertAnchorLinksAccountedFor is the anchor-side totality reconciliation:
// every Markdown link whose target holds an anchor must be one this gate reads.
// A link shape the pattern cannot parse is not a link this gate may ignore.
func assertAnchorLinksAccountedFor(t *testing.T, doc *xrefDoc, recognised [][]int) {
	t.Helper()

	seen := make(map[int]bool, len(recognised))
	for _, m := range recognised {
		seen[m[0]] = true
	}
	for _, m := range anyAnchorLinkPattern.FindAllStringIndex(doc.body, -1) {
		if !seen[m[0]] {
			t.Errorf("%s:%d holds a link to an anchor in a shape this gate does not recognise, so the "+
				"anchor is resolved against nothing: %s", doc.rel, lineOf(doc.body, m[0]), doc.body[m[0]:m[1]])
		}
	}
}

// ---------------------------------------------------------------------------
// 3. The heading normalisation strips inline markup and nothing else.
// ---------------------------------------------------------------------------

// TestSpecXref_HeadingNormalisationStripsMarkupAndNothingElse pins the matching
// rule itself, because the rule is the part of this gate that can fail silently
// in the direction that hurts most: a normalisation that strips more than markup
// reports CORRECT references as dangling, and the person who then "fixes" the
// reference breaks a link that worked.
//
// Every case below is a real heading of this repository or a real shape one of
// them has. The two intraword-underscore cases and the trailing-parenthetical
// case are the ones that have already been got wrong once.
func TestSpecXref_HeadingNormalisationStripsMarkupAndNothingElse(t *testing.T) {
	cases := []struct {
		name    string
		heading string
		want    string
	}{
		{
			name:    "backticks are markup, and 60 references depend on their removal",
			heading: "`audit` Table",
			want:    "audit Table",
		},
		{
			name:    "bold is markup",
			heading: "**Valid** values",
			want:    "Valid values",
		},
		{
			name:    "star emphasis is markup",
			heading: "*Deprecated* form",
			want:    "Deprecated form",
		},
		{
			name:    "word-bounded underscore emphasis is markup",
			heading: "The _canonical_ catalogue",
			want:    "The canonical catalogue",
		},
		{
			name:    "a link is reduced to its label",
			heading: "See [the contract](./ARCHITECTURE.md#contract)",
			want:    "See the contract",
		},
		{
			name:    "a whitespace run flattens, so a reference may wrap across lines",
			heading: "Sprint  Membership\tand the BACKLOG Status",
			want:    "Sprint Membership and the BACKLOG Status",
		},
		{
			name:    "a trailing parenthetical is part of the name, not decoration",
			heading: "Migration Idempotency (ALTER TABLE ADD COLUMN)",
			want:    "Migration Idempotency (ALTER TABLE ADD COLUMN)",
		},
		{
			name:    "a numeric prefix is part of the name",
			heading: "3. Agent and Skill Responsibilities",
			want:    "3. Agent and Skill Responsibilities",
		},
		{
			name:    "an intraword underscore is not emphasis",
			heading: "`is_raspberry_pi()`",
			want:    "is_raspberry_pi()",
		},
		{
			name:    "an intraword underscore in an enum constant is not emphasis either",
			heading: "Reclassifying `TASK_STATUS_CHANGE`",
			want:    "Reclassifying TASK_STATUS_CHANGE",
		},
		{
			name:    "case is preserved, because no reference in the tree needs folding",
			heading: "Exit Codes",
			want:    "Exit Codes",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normaliseHeading(tc.heading); got != tc.want {
				t.Errorf("normaliseHeading(%q) = %q, want %q\nThe comparison form of a heading must differ "+
					"from the heading only by its inline markup. Stripping anything else makes a correct "+
					"cross-reference look dangling.", tc.heading, got, tc.want)
			}
		})
	}

	// Case sensitivity is a decision, not an accident: zero references in the
	// tree need folding, so folding would only widen what a citation may say.
	if normaliseHeading("Exit Codes") == normaliseHeading("exit codes") {
		t.Error("normaliseHeading folds case; it must not. Comparison is case-sensitive because no " +
			"reference in the tree needs folding, and a looser rule accepts citations that name no heading " +
			"as a reader would read it")
	}

	// And the same rule swept over the corpus, so the pinned cases cannot drift
	// away from the headings that actually exist: no heading may lose an
	// underscore that sits between two word characters.
	_, targets := loadXrefCorpus(t)
	intraword := 0
	for _, doc := range targets {
		for _, h := range doc.headings {
			// Backticks are stripped before emphasis, and stripping them is what
			// can turn a delimited underscore into an intraword one, so the
			// sweep starts from that intermediate form.
			ticked := backtickRunPattern.ReplaceAllString(h.text, "")
			before := intrawordUnderscores(ticked)
			intraword += before
			if after := intrawordUnderscores(stripUnderscoreEmphasis(ticked)); after != before {
				t.Errorf("%s:%d declares the heading %q, and the emphasis rule removes %d of its %d "+
					"intraword underscores, indexing it as %q. A citation naming the heading as written "+
					"would then be reported as dangling.",
					doc.rel, h.line, h.text, before-after, before, h.name)
			}
		}
	}
	if intraword == 0 {
		t.Error("no heading in the corpus carries an intraword underscore, so the sweep above proved " +
			"nothing; the pinned cases are the only remaining guard against the emphasis rule eating one")
	}
}

// intrawordUnderscores counts the underscores that sit between two word
// characters, which is exactly the set CommonMark refuses to read as emphasis.
func intrawordUnderscores(s string) int {
	runes := []rune(s)
	n := 0
	for i, r := range runes {
		if r != '_' || i == 0 || i+1 >= len(runes) {
			continue
		}
		if isWordRune(runes[i-1]) && isWordRune(runes[i+1]) {
			n++
		}
	}
	return n
}
