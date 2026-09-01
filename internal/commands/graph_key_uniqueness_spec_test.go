package commands

// Regression gate for rmp task #310, acceptance criterion 5, guarding
// SPEC/GRAPH.md § Node Key Uniqueness and its acceptance criterion 61.
//
// THE DEFECT. knowledge-model.md stated flatly that every node's `key` is
// "globally unique across the whole graph", in the register of a guarantee the
// product holds up. Nothing in the product holds it up: no constraint exists
// (the five graph subcommands reject DDL), a node's identity in the store is an
// internal uint64 rather than its key, and keys are compared only inside the
// caller's own Cypher, byte for byte. Two Unicode normalisations of one key —
// visually identical, one precomposed and one decomposed — are therefore two
// nodes, and `MATCH (n {key:'...'})` binds whichever spelling the caller
// happened to write. Worse, the duplicate audit the project already runs groups
// on the STORED BYTES, so such a pair shows up as two groups of one and the
// violation is invisible. The owner's decision was to keep the invariant a
// convention and to publish an audit that detects a breach after the fact,
// normalising for COMPARISON only and storing the caller's bytes unchanged.
//
// WHAT THIS FILE GUARDS. The specification now carries the rule and
// knowledge-model.md defers to it. Nothing held those two documents together,
// and nothing held the specification's published audit query to the engine that
// has to run it. Both are guarded here.
//
// WHY IT CANNOT GO GREEN ON THE DRIFT IT CHASES. No sentence of either document
// is restated in this file, and no expected result is stored. Both sides are
// read at test time and compared to each other, and the canonical section is
// not named here at all: it is DERIVED from the heading knowledge-model.md
// itself cites, resolved against the headings SPEC/GRAPH.md actually has. So a
// heading renamed on one side, a citation repointed on the other, or a claim
// dropped from the section that is cited all fail. A gate that pinned a copy of
// either document would pass through exactly the divergence it exists to
// detect. The published audit query is likewise not transcribed: it is parsed
// out of the specification and then run — through the real guard rail, and
// against a real GoGraph store — so a query edited into something the engine
// refuses fails here rather than at the moment somebody needs the audit.
//
// Every region the gate needs is fatal when it cannot be located, so a
// restructure of either document produces a red test rather than a gate that
// quietly measures nothing.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"
	"github.com/FlavioCFOliveira/GoGraph/store/wal"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphkeys"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

const (
	// knowledgeModelPath locates the second of the two documents that must
	// agree, relative to this package. The first, SPEC/GRAPH.md, is reached
	// through the graphSpecPath constant this package already declares.
	knowledgeModelPath = "../../knowledge-model.md"

	// keysParagraphLead opens the knowledge-model.md paragraph that describes
	// the `key` property. The file writes each convention as a bold lead-in.
	keysParagraphLead = "**Keys.**"

	// auditSubheading is the subsection of the cited specification section that
	// publishes the audit's first step as a runnable query.
	auditSubheading = "#### Auditing the convention"
)

// ==================== THE CLAIMS BOTH DOCUMENTS MAY MAKE ====================

// keyClaim is one substantive proposition about node keys. The gate compares
// the two documents by which of these each one asserts, rather than by their
// wording, so either may be rewritten freely as long as they keep saying the
// same things.
type keyClaim struct {
	// name renders the claim in a failure message.
	name string
	// phrases are alternative ways a document may assert the claim. Each is a
	// meaning-bearing fragment rather than a bare keyword, and a document
	// asserts the claim when it carries any one of them.
	phrases []string
}

// keyClaims are the six propositions § Node Key Uniqueness exists to settle.
//
// The detectors are deliberately permissive about wording and strict about
// meaning: each phrase carries the proposition on its own, so a document cannot
// satisfy one by mentioning a word in passing. The limitation that comes with
// the choice, stated rather than hidden: a document could assert a claim in a
// seventh phrasing none of these anticipates and be read as silent on it. That
// direction is safe for the doc side — silence is what deferring to the
// specification looks like — and is caught on the spec side by the coverage
// floor below, which fails when the cited section stops asserting any one of
// them.
var keyClaims = []keyClaim{
	{
		name: "the key is unique across the graph",
		phrases: []string{
			"is unique across",
			"globally unique",
			"no two nodes carry the same",
		},
	},
	{
		name: "an unlabelled MATCH on key binds at most one node",
		phrases: []string{
			"binds at most one node",
			"without a label is unambiguous",
		},
	},
	{
		name: "Unicode NFC decides whether two keys are the same key",
		phrases: []string{
			"nfc",
			"normalization form c",
			"normalisation form c",
		},
	},
	{
		name: "the product does not enforce it; it is the caller's convention",
		phrases: []string{
			"does not enforce",
			"not a rule the product enforces",
			"nothing enforces",
			"command rejects, rewrites",
			"convention the caller honours",
		},
	},
	{
		name: "normalisation is for comparison only; the stored key is the caller's bytes",
		phrases: []string{
			"for comparison only",
		},
	},
	{
		name:    "a violation is detected by an audit",
		phrases: []string{"audit"},
	},
}

// assertedClaims returns the names of the claims text asserts.
func assertedClaims(text string) map[string]bool {
	lower := strings.ToLower(text)
	out := make(map[string]bool, len(keyClaims))
	for _, c := range keyClaims {
		for _, p := range c.phrases {
			if strings.Contains(lower, p) {
				out[c.name] = true
				break
			}
		}
	}
	return out
}

// ==================== READING THE TWO DOCUMENTS ====================

// mdHeading matches an ATX heading and captures its level and its text.
var mdHeading = regexp.MustCompile(`(?m)^(#{1,6}) +(.+?) *$`)

// specSection is one located section of SPEC/GRAPH.md.
type specSection struct {
	heading string
	level   int
	body    string
	line    int
}

// readGraphSpecSections splits SPEC/GRAPH.md into its sections, each running
// from its own heading to the next heading of the same level or shallower.
//
// It fails the test rather than returning an error: a specification this gate
// cannot parse is a broken gate, not a passing one.
func readGraphSpecSections(t *testing.T) []specSection {
	t.Helper()

	raw, err := os.ReadFile(graphSpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", graphSpecPath, err)
	}
	text := string(raw)
	lines := strings.Split(text, "\n")

	type start struct {
		heading string
		level   int
		at      int
	}
	var starts []start
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		m := mdHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		starts = append(starts, start{heading: m[2], level: len(m[1]), at: i})
	}
	if len(starts) == 0 {
		t.Fatalf("%s yielded no headings; the gate parses nothing and would pass vacuously", graphSpecPath)
	}

	sections := make([]specSection, 0, len(starts))
	for i, s := range starts {
		end := len(lines)
		for j := i + 1; j < len(starts); j++ {
			if starts[j].level <= s.level {
				end = starts[j].at
				break
			}
		}
		sections = append(sections, specSection{
			heading: s.heading,
			level:   s.level,
			body:    strings.Join(lines[s.at:end], "\n"),
			line:    s.at + 1,
		})
	}
	return sections
}

// readKeysParagraph returns the region of knowledge-model.md that describes the
// `key` property: the bold "Keys." lead-in through to the next bold lead-in or
// heading, whichever comes first.
func readKeysParagraph(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(knowledgeModelPath)
	if err != nil {
		t.Fatalf("read %s: %v", knowledgeModelPath, err)
	}
	region, ok := extractKeysParagraph(string(raw))
	if !ok {
		t.Fatalf("%s no longer contains a %q paragraph; the gate has lost the text it reads",
			knowledgeModelPath, keysParagraphLead)
	}
	return region
}

// extractKeysParagraph is readKeysParagraph's pure half, so the fixture test
// below can drive the same reader over text that is not on disk.
func extractKeysParagraph(doc string) (string, bool) {
	lines := strings.Split(doc, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), keysParagraphLead) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") {
			end = i
			break
		}
		// A second bold lead-in opens the next convention.
		if strings.HasPrefix(trimmed, "**") && strings.Contains(trimmed, ".**") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n"), true
}

// ==================== THE AGREEMENT RULE ====================

// citedSection resolves the specification section knowledge-model.md defers to,
// by finding which of SPEC/GRAPH.md's own headings the paragraph names.
//
// Deriving the link from both sides at once is what makes the citation a
// relation rather than a constant: the heading has to exist in the
// specification AND be named in the paragraph. Renaming it on either side
// breaks the resolution and fails the gate.
//
// Only headings of three words or more are considered, because a shorter one
// ("Keys", "Conventions") occurs in ordinary prose and would resolve by
// accident. The longest match wins, so a heading that is a prefix of another
// cannot shadow it.
func citedSection(paragraph string, sections []specSection) (specSection, bool) {
	best := specSection{}
	found := false
	for _, s := range sections {
		if len(strings.Fields(s.heading)) < 3 {
			continue
		}
		if !strings.Contains(paragraph, s.heading) {
			continue
		}
		if !found || len(s.heading) > len(best.heading) {
			best, found = s, true
		}
	}
	return best, found
}

// checkKeyDocAgreement returns the reasons paragraph fails to agree with the
// specification, or nothing when the two agree.
//
// The rule has three parts:
//
//  1. If the paragraph asserts the uniqueness of a key, or the unambiguity of
//     an unlabelled MATCH on one, it must also say that nothing enforces it.
//     This is the defect itself: a bare uniqueness claim reads as a guarantee
//     the product upholds, and no reader of it would think to check. It is
//     applied first and on the paragraph alone, because the paragraph that
//     carried the defect cited nothing, and a rule that ran only once a
//     citation resolved would have let it through.
//  2. The paragraph must cite SPEC/GRAPH.md and name a section that file has.
//     Without a resolvable citation the paragraph is on its own, which is the
//     other half of the state the defect left it in.
//  3. The cited section must assert every claim, because it is the document
//     that was declared canonical for them. This is the coverage floor: a
//     section gutted of any claim stops being a place to defer to.
//
// Part 1 is where the teeth are, and the fixture test below proves it by
// feeding this function the paragraph as it stood before the fix.
func checkKeyDocAgreement(paragraph string, sections []specSection) []string {
	var problems []string

	docClaims := assertedClaims(paragraph)

	// Part 3 first, and independently of everything below it. A bare uniqueness
	// claim is a defect in the paragraph on its own terms, so it must be
	// reported whether or not the citation resolves — the paragraph that
	// carried the defect had no citation at all, and a check that ran only
	// after the citation resolved would never have reached it.
	claimsUniqueness := docClaims[keyClaims[0].name] || docClaims[keyClaims[1].name]
	if claimsUniqueness && !docClaims[keyClaims[3].name] {
		problems = append(problems, fmt.Sprintf(
			"the paragraph states that %s without stating that %s; a bare uniqueness claim reads as a guarantee the product upholds",
			keyClaims[0].name, keyClaims[3].name))
	}

	if !strings.Contains(paragraph, "SPEC/GRAPH.md") {
		problems = append(problems, "the paragraph does not cite SPEC/GRAPH.md, so nothing ties it to the canonical rule")
	}

	cited, ok := citedSection(paragraph, sections)
	if !ok {
		problems = append(problems,
			"the paragraph names no section that SPEC/GRAPH.md has: either the citation or the heading was changed without the other")
		return problems
	}

	specClaims := assertedClaims(cited.body)
	for _, c := range keyClaims {
		if !specClaims[c.name] {
			problems = append(problems, fmt.Sprintf(
				"%s § %s is cited as canonical but no longer asserts that %s",
				graphSpecPath, cited.heading, c.name))
		}
	}

	for _, c := range keyClaims {
		if docClaims[c.name] && !specClaims[c.name] {
			problems = append(problems, fmt.Sprintf(
				"the paragraph asserts that %s, which %s § %s does not",
				c.name, graphSpecPath, cited.heading))
		}
	}

	return problems
}

// TestKeyUniqueness_KnowledgeModelAgreesWithTheSpec is acceptance criterion 1 of
// rmp task #310 held in place: the two documents must agree on the substance of
// the key-uniqueness rule.
func TestKeyUniqueness_KnowledgeModelAgreesWithTheSpec(t *testing.T) {
	sections := readGraphSpecSections(t)
	paragraph := readKeysParagraph(t)

	for _, problem := range checkKeyDocAgreement(paragraph, sections) {
		t.Errorf("%s § Keys disagrees with %s: %s", knowledgeModelPath, graphSpecPath, problem)
	}
}

// TestKeyUniqueness_TheParagraphAsItStoodWouldBeRejected proves this gate can
// fail, by running it against the exact paragraph knowledge-model.md carried
// before the fix. That paragraph asserted uniqueness flatly and said nothing
// about who upholds it, which is the defect.
//
// Without this test the agreement test above would be indistinguishable from
// one that passes because it checks nothing.
func TestKeyUniqueness_TheParagraphAsItStoodWouldBeRejected(t *testing.T) {
	const beforeTheFix = "**Keys.** Every node carries a `key` property that is globally unique across the whole\n" +
		"graph, so `MATCH (n {key:'...'})` without a label is unambiguous. The key is the natural\n" +
		"identifier of the artefact: a repository-relative file path for code, tests and specs, a\n" +
		"package path for components, a slug for requirements, releases and memories.\n"

	sections := readGraphSpecSections(t)

	paragraph, ok := extractKeysParagraph(beforeTheFix)
	if !ok {
		t.Fatalf("the reader failed on the pre-fix paragraph; the fixture and the reader have drifted apart")
	}

	problems := checkKeyDocAgreement(paragraph, sections)
	if len(problems) == 0 {
		t.Fatalf("the gate accepts the paragraph as it stood before the fix; it would not have caught the defect")
	}

	// It must be rejected for the RIGHT reason, not merely rejected. A gate that
	// happened to fail on the citation alone would go green again the moment
	// somebody re-added a bare uniqueness claim beside a citation.
	wantedReason := "reads as a guarantee the product upholds"
	if !strings.Contains(strings.Join(problems, "\n"), wantedReason) {
		t.Errorf("the pre-fix paragraph was rejected, but not for asserting uniqueness without non-enforcement.\nreasons:\n  %s",
			strings.Join(problems, "\n  "))
	}
}

// TestKeyUniqueness_TheGateReadsBothDocumentsRatherThanACopy pins the two
// properties that keep the agreement test honest, both of which would be lost
// if somebody replaced either side with a stored expectation.
func TestKeyUniqueness_TheGateReadsBothDocumentsRatherThanACopy(t *testing.T) {
	sections := readGraphSpecSections(t)
	paragraph := readKeysParagraph(t)

	cited, ok := citedSection(paragraph, sections)
	if !ok {
		t.Fatalf("the paragraph names no section of %s; the citation cannot be resolved", graphSpecPath)
	}

	// The resolved section must be the one that carries the audit, which is
	// what the next tests parse their query out of. If the citation ever
	// resolved to some unrelated heading, this is where it shows.
	if !strings.Contains(cited.body, auditSubheading) {
		t.Errorf("the cited section %q of %s does not contain %q; the citation resolves to the wrong place",
			cited.heading, graphSpecPath, auditSubheading)
	}

	// A claim set this small could be satisfied by an empty section if a
	// detector were ever loosened into a tautology. Requiring the cited section
	// to be substantial keeps that from passing silently.
	if len(strings.Fields(cited.body)) < 200 {
		t.Errorf("the cited section %q of %s is only %d words long; it is too small to be the canonical rule",
			cited.heading, graphSpecPath, len(strings.Fields(cited.body)))
	}
}

// ==================== THE PUBLISHED AUDIT QUERY ====================

// cypherFences extracts the fenced Cypher blocks of a section.
func cypherFences(body string) []string {
	var out []string
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```cypher" {
			continue
		}
		var block []string
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				out = append(out, strings.Join(block, "\n"))
				i = j
				break
			}
			block = append(block, lines[j])
		}
	}
	return out
}

// readAuditQuery returns the single Cypher query § Auditing the convention
// publishes as step 1 of the audit.
func readAuditQuery(t *testing.T) string {
	t.Helper()

	sections := readGraphSpecSections(t)
	paragraph := readKeysParagraph(t)
	cited, ok := citedSection(paragraph, sections)
	if !ok {
		t.Fatalf("the paragraph names no section of %s; the citation cannot be resolved", graphSpecPath)
	}

	var audit specSection
	for _, s := range sections {
		if "#"+strings.Repeat("#", s.level-1)+" "+s.heading == auditSubheading {
			audit = s
			break
		}
	}
	if audit.heading == "" {
		t.Fatalf("%s no longer contains %q; the gate has lost the query it runs", graphSpecPath, auditSubheading)
	}
	if !strings.Contains(cited.body, audit.heading) {
		t.Fatalf("%q is no longer inside the cited section %q", audit.heading, cited.heading)
	}

	fences := cypherFences(audit.body)
	if len(fences) != 1 {
		t.Fatalf("%s § %s publishes %d Cypher blocks; the gate expects the one step-1 query",
			graphSpecPath, audit.heading, len(fences))
	}
	return strings.TrimSpace(fences[0])
}

// TestKeyUniqueness_PublishedAuditQueryRunsAndReturnsEveryKeyedNode runs the
// published query against a real GoGraph store holding the canonical witness of
// the defect: one file path written with a precomposed E-acute and the same path
// written with a decomposed one.
//
// The specification publishes this query as executable, so the gate executes
// it. A RETURN list edited to drop a column, an ORDER BY the engine cannot
// resolve, or a function the engine does not have all fail here.
func TestKeyUniqueness_PublishedAuditQueryRunsAndReturnsEveryKeyedNode(t *testing.T) {
	query := readAuditQuery(t)
	graphDir := seedKeyUniquenessGraph(t)

	cols, rows := runGraphQueryForTest(t, graphDir, query)

	// The columns are step 2's whole input: it groups the keys by NFC form and
	// uses id and labels to name the nodes of a violating group. A query that
	// stopped returning one of them would leave the audit unable to report.
	if want := []string{"id", "labels", "key"}; !equalStrings(cols, want) {
		t.Fatalf("the published audit query returns columns %v; step 2 consumes %v", cols, want)
	}

	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, fmt.Sprintf("%v", row[2]))
	}
	sort.Strings(keys)

	for _, want := range []string{keyPrecomposed, keyDecomposed} {
		if !containsString(keys, want) {
			t.Errorf("the published audit query did not return the key %q (returned %q)", want, keys)
		}
	}
	if len(rows) != len(keyUniquenessSeed) {
		t.Errorf("the published audit query returned %d rows for %d keyed nodes; it must return every one, "+
			"since an audit that narrows to a candidate subset can miss a violation",
			len(rows), len(keyUniquenessSeed))
	}
}

// TestKeyUniqueness_ByteWiseAuditIsBlindToTheNormalisationPair is the half of
// acceptance criterion 61 that says WHY a second audit had to be published: the
// duplicate audit the project already runs groups on the stored bytes, so the
// two spellings appear as two groups of one and it reports nothing.
//
// It is asserted rather than assumed, because the entire case for the two-step
// audit rests on it. If a future engine were to group these two together, the
// two-step audit would be redundant and this test would say so.
func TestKeyUniqueness_ByteWiseAuditIsBlindToTheNormalisationPair(t *testing.T) {
	graphDir := seedKeyUniquenessGraph(t)

	const byteWise = "MATCH (n) WHERE n.key IS NOT NULL RETURN n.key AS key, count(*) AS n"
	_, rows := runGraphQueryForTest(t, graphDir, byteWise)

	counts := map[string]string{}
	for _, row := range rows {
		counts[fmt.Sprintf("%v", row[0])] = fmt.Sprintf("%v", row[1])
	}

	for _, spelling := range []string{keyPrecomposed, keyDecomposed} {
		got, ok := counts[spelling]
		if !ok {
			t.Fatalf("the byte-wise duplicate audit did not report the key %q at all", spelling)
		}
		if got != "1" {
			t.Errorf("the byte-wise duplicate audit reports %q %s times; the case for the two-step audit is that "+
				"it reports each spelling once and so reports no duplicate", spelling, got)
		}
	}
}

// TestKeyUniqueness_EitherSpellingBindsExactlyOneNode is the rest of acceptance
// criterion 61: the stored key is byte-for-byte what the caller supplied, and an
// unlabelled MATCH on either spelling binds that one node and never both.
//
// This is the product behaviour the convention is stated against. It is pinned
// so that a future change which started normalising a key on the way in — the
// option the owner declined — cannot land unnoticed.
func TestKeyUniqueness_EitherSpellingBindsExactlyOneNode(t *testing.T) {
	graphDir := seedKeyUniquenessGraph(t)

	if keyPrecomposed == keyDecomposed {
		t.Fatalf("the two witness spellings are the same string; the test would prove nothing")
	}

	for _, spelling := range []string{keyPrecomposed, keyDecomposed} {
		query := fmt.Sprintf("MATCH (n {key:'%s'}) RETURN n.key AS key", spelling)
		_, rows := runGraphQueryForTest(t, graphDir, query)

		if len(rows) != 1 {
			t.Errorf("MATCH on %q bound %d nodes; the specification states it binds exactly one",
				spelling, len(rows))
			continue
		}
		if got := fmt.Sprintf("%v", rows[0][0]); got != spelling {
			t.Errorf("MATCH on %q bound a node whose stored key is %q; the stored key must be the bytes supplied",
				spelling, got)
		}
	}
}

// TestKeyUniqueness_TheTwoStepAuditReportsTheViolationEndToEnd is the audit run
// whole: the query SPEC/GRAPH.md publishes as step 1, executed against a real
// store, its rows handed to step 2 exactly as a caller would hand them over.
//
// Each step is tested on its own — the query here, the grouping in
// internal/graphkeys — and neither of those proves they compose. This does. It is
// what makes the specification's audit executable rather than described: if the
// published RETURN list and the columns step 2 resolves ever stop lining up, the
// audit silently stops being runnable, and nothing else would notice.
func TestKeyUniqueness_TheTwoStepAuditReportsTheViolationEndToEnd(t *testing.T) {
	query := readAuditQuery(t)
	graphDir := seedKeyUniquenessGraph(t)

	cols, rows := runGraphQueryForTest(t, graphDir, query)

	audited, err := graphkeys.RowsFrom(cols, rows)
	if err != nil {
		t.Fatalf("step 2 cannot read what step 1 returned: %v", err)
	}
	if len(audited) != len(keyUniquenessSeed) {
		t.Fatalf("step 2 received %d rows for %d keyed nodes", len(audited), len(keyUniquenessSeed))
	}

	violations := graphkeys.Audit(audited)
	if len(violations) != 1 {
		t.Fatalf("the two-step audit reported %d violations on the witness graph; want exactly the one pair: %+v",
			len(violations), violations)
	}

	v := violations[0]
	if v.Kind != graphkeys.KindNormalisation {
		t.Errorf("the reported violation is %v; the pair differs in normalisation, not in bytes alone", v.Kind)
	}
	for _, want := range []string{keyPrecomposed, keyDecomposed} {
		if !containsString(v.Spellings, want) {
			t.Errorf("the reported violation does not name the stored spelling %q (named %q)", want, v.Spellings)
		}
	}
	// The audit must name the nodes, which is what the id and labels columns of
	// step 1 are returned for.
	if len(v.Rows) != 2 {
		t.Fatalf("the violation names %d nodes; both must be named", len(v.Rows))
	}
	for _, r := range v.Rows {
		if len(r.Labels) == 0 {
			t.Errorf("the violation names node %d with no labels; step 1 returns them so a report can identify it", r.ID)
		}
	}

	// The same audit reports nothing on a graph whose keys are all distinct under
	// NFC, which acceptance criterion 61 requires and which is what stops the
	// audit from being one that reports everything.
	clean := make([]graphkeys.Row, 0, len(audited))
	for _, r := range audited {
		if r.Key == keyDecomposed {
			continue
		}
		clean = append(clean, r)
	}
	if got := graphkeys.Audit(clean); len(got) != 0 {
		t.Errorf("the audit reported %d violations on a graph whose keys are all distinct under NFC: %+v", len(got), got)
	}
}

// ==================== THE WITNESS GRAPH ====================

// The canonical witness of the defect: one repository-relative path spelled two
// ways that Unicode calls the same text. They render identically and differ in
// bytes, which is the whole of the condition.
const (
	// keyPrecomposed carries U+00C9 LATIN CAPITAL LETTER E WITH ACUTE.
	keyPrecomposed = "docs/CAFÉ.md"
	// keyDecomposed carries U+0045 U+0301, E followed by COMBINING ACUTE ACCENT.
	keyDecomposed = "docs/CAFÉ.md"
)

// keyUniquenessSeed is the witness graph: the two spellings of one path, plus
// three ordinary ASCII keys so the audit query is exercised over a graph that
// is mostly well-behaved, exactly as the project's own is.
var keyUniquenessSeed = []string{
	"CREATE (:Doc {key:'" + keyPrecomposed + "'})",
	"CREATE (:Doc {key:'" + keyDecomposed + "'})",
	"CREATE (:Spec {key:'SPEC/GRAPH.md'})",
	"CREATE (:CodeFile {key:'internal/commands/graph.go'})",
	"CREATE (:Component {key:'internal/graphlock'})",
}

// seedKeyUniquenessGraph builds the witness graph in a temporary directory.
//
// It never touches ~/.roadmaps: the store is opened directly at a t.TempDir()
// path, so the gate cannot disturb a real roadmap's graph.
func seedKeyUniquenessGraph(t *testing.T) string {
	t.Helper()

	graphDir := filepath.Join(t.TempDir(), "graph")
	if err := os.MkdirAll(graphDir, 0o700); err != nil {
		t.Fatalf("creating the witness graph directory: %v", err)
	}
	for _, q := range keyUniquenessSeed {
		writeKeyUniquenessTx(t, graphDir, q)
	}
	return graphDir
}

// writeKeyUniquenessTx commits one seed query against the store at graphDir.
func writeKeyUniquenessTx(t *testing.T, graphDir, query string) {
	t.Helper()

	res, err := recovery.Open[string, float64](graphDir, graphstore.RecoveryOptions())
	if err != nil {
		t.Fatalf("opening the witness graph for seeding: %v", err)
	}
	w, err := wal.Open(filepath.Join(graphDir, "wal"))
	if err != nil {
		t.Fatalf("opening the witness graph WAL: %v", err)
	}
	defer w.Close() //nolint:errcheck // test cleanup

	store := txn.NewStoreWithOptions[string, float64](res.Graph, w, txn.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})
	result, err := cypher.NewEngineWithStore(store).RunInTx(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("seeding %q: %v", query, err)
	}
	for result.Next() { //nolint:revive // drain so the transaction may commit
	}
	if err := result.Close(); err != nil {
		t.Fatalf("committing the seed %q: %v", query, err)
	}
}

// runGraphQueryForTest executes a read query against the witness graph exactly
// as a graph read did before the collapse — recovery.Open then cypher.NewEngine — and
// serialises the result through the CLI's own serializeGraphResult, so what the
// gate inspects is what the command would print.
func runGraphQueryForTest(t *testing.T, graphDir, query string) ([]string, [][]any) {
	t.Helper()

	res, err := recovery.Open[string, float64](graphDir, graphstore.RecoveryOptions())
	if err != nil {
		t.Fatalf("opening the witness graph: %v", err)
	}
	result, err := cypher.NewEngine(res.Graph).RunInTx(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("running %q against the witness graph: %v", query, err)
	}
	out, err := serializeGraphResult(result)
	if err != nil {
		_ = result.Close() //nolint:errcheck // the serialisation error is the one that matters
		t.Fatalf("serialising the result of %q: %v", query, err)
	}
	if err := result.Close(); err != nil {
		t.Fatalf("closing the result of %q: %v", query, err)
	}
	return out.Columns, out.Rows
}

// equalStrings reports whether two string slices are equal element for element.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
