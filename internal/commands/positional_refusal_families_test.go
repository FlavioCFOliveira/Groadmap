// Package commands — one rule, two families, one gate.
//
// # The criterion
//
// SPEC/GRAPH.md acceptance criterion 28: "The rule is one rule across the two
// families that publish it. The line `graph execute` emits is the line
// COMMANDS.md § Positional Arguments publishes for the whole CLI with this
// family's hint appended, and the line the comment subcommands emit is that same
// line without a hint (COMMANDS.md § Comment Positional Argument Contract). A
// test that asserts one family's wording MUST cite the other's, so that a change
// to either is made deliberately rather than by copying."
//
// # Why a second literal copy would be the wrong gate
//
// Both families refuse an excess positional argument with exit code 2 and with a
// line built from the same sentence. Writing that sentence out twice — once for
// the `graph` family, once for the comment subcommands — pins two copies and
// asserts nothing about the fact that they are copies. The drift this criterion
// exists to catch is precisely an edit to ONE of them: reword the shared half on
// the `graph` side and both literal pins are simply updated to match, one at a
// time, and nothing ever objects.
//
// This gate therefore writes neither line down. It reads BOTH sides and asserts
// the RELATION between them:
//
//	graph line   ==  canonical line + this family's hint
//	comment line ==  canonical line, exactly
//
// Nothing here knows what the canonical line says, what the hint says, or even
// that the hint is a parenthetical. The hint is DERIVED as the difference
// between the two lines SPEC/COMMANDS.md § Positional Arguments publishes, so
// the only thing this file can be said to hardcode is the shape of the
// relationship, which is what the criterion states.
//
// # The four readings, and what each one closes
//
//  1. SPEC/COMMANDS.md § Positional Arguments publishes exactly two template
//     refusals: the canonical one and the `graph` one. The shorter must be a
//     proper prefix of the longer, and their difference is the hint.
//  2. SPEC/GRAPH.md § No Positional Query: A Stray Token Is Refused publishes
//     the same `graph` line, and quotes the same hint in its rule 6. Two files
//     publishing one line is only safe while something compares them.
//  3. SPEC/COMMANDS.md § Comment Positional Argument Contract publishes the
//     canonical line for itself AND cites the `graph` line, which is how "a
//     reader who finds one of these two sections must be able to reach the
//     other from it" becomes a checkable fact rather than an intention.
//  4. The binary. The canonical producer (checkPositionalArity), the `graph`
//     producer (readQuery) and the comment producer (parseCommentArgs) are
//     driven with the SAME offending token, and the three lines they return
//     must stand in the published relation. This is the reading that fails
//     when the code drifts from the SPEC on either side.
//
// Every single-sided edit is caught by at least one of the four. An edit to the
// shared half on ONE side breaks the prefix relation in reading 1 or the live
// comparison in reading 4; an edit to the hint breaks reading 2 or 4; deleting
// the cross-reference breaks reading 3.
//
// # The citation floor
//
// TestPositionalRefusal_APinOfOneFamilysWordingCitesTheOther is the criterion's
// own sentence, enforced over the test suite: a test file that pins one
// family's wording must name the other family. It is a floor and not a proof —
// the readings above are what actually catch drift — but it is what stops the
// next test file from pinning a third literal copy in isolation.
package commands

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// graphSpecPath and helpSpecPath locate the two other SPEC files this gate
// reads. commandsSpecPath is declared by positional_arity_spec_test.go, which
// reads the same file for the arity table.
const (
	graphSpecPath = "../../SPEC/GRAPH.md"
	helpSpecPath  = "../../SPEC/HELP.md"
)

// The four SPEC sections read below, by their exact heading text.
const (
	positionalArgumentsHeading = "## Positional Arguments"
	commentContractHeading     = "### Comment Positional Argument Contract"
	noPositionalQueryHeading   = "### No Positional Query: A Stray Token Is Refused"
	errorLineHeading           = "### Error line"
)

// refusalDetail is the sentinel-free half of the message both families share.
// It selects the published strings this gate is about, and is deliberately NOT
// the assertion: what is asserted is how the two full lines relate.
const refusalDetail = "unexpected argument"

// quotedPlaceholder is the offending value as SPEC/COMMANDS.md § Published
// Error Strings Are Exact publishes it: the placeholder `X`, with the quotes
// the binary writes shown around it. A published string carrying it is a
// TEMPLATE; a published string carrying a real token is a worked example, and
// the sections read here contain both.
const quotedPlaceholder = `"X"`

// specErrorLinePrefix is what cmd/rmp/main.go's writeFailureReport writes ahead
// of every message, so an error value returned by a handler becomes the line the
// user reads. The handlers under test return the message alone, which is why the
// prefix has to be supplied here to compare against a published line.
//
// It is the one string in this file that is written out rather than derived, and
// TestPositionalRefusal_TheErrorLinePrefixIsTheOneTheSpecPublishes pins it to
// SPEC/HELP.md § Error line so it cannot quietly diverge.
const specErrorLinePrefix = "Error: "

// errorLine renders the complete stderr line a failing invocation produces, from
// the error its handler returned.
func errorLine(err error) string {
	if err == nil {
		return ""
	}
	return specErrorLinePrefix + err.Error()
}

// ---------------------------------------------------------------------------
// Reading the SPEC
// ---------------------------------------------------------------------------

// headingLevel returns the ATX heading level of line, or 0 when line is not a
// heading. A run of '#' must be followed by a space to be a heading, which is
// what keeps a comment or a Cypher fragment from ending a section early.
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n >= len(line) || line[n] != ' ' {
		return 0
	}
	return n
}

// sectionName strips the leading ATX marker from a heading, so a failure
// message can read "FILE § Heading" without repeating the "####".
func sectionName(heading string) string {
	return strings.TrimSpace(strings.TrimLeft(heading, "#"))
}

// readSpecSection returns the body of one SPEC section: every line after the
// heading, up to the next heading of the same level or shallower. Lines inside a
// fenced code block are never read as headings, so a fence may carry anything.
//
// It fails the test rather than returning an error. A section this gate cannot
// find is a broken gate, not a passing one.
func readSpecSection(t *testing.T, path, heading string) []string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(raw), "\n")

	level := headingLevel(heading)
	if level == 0 {
		t.Fatalf("%q is not an ATX heading; this gate cannot bound a section with it", heading)
	}

	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s no longer contains the heading %q; the gate has lost the section it reads",
			path, heading)
	}

	end := len(lines)
	fenced := false
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if lv := headingLevel(trimmed); lv > 0 && lv <= level {
			end = i
			break
		}
	}

	body := lines[start+1 : end]
	if len(body) == 0 {
		t.Fatalf("%s § %s is empty; every assertion drawn from it would be vacuous", path, heading)
	}
	return body
}

// publishedTemplateRefusals returns every distinct excess-positional-argument
// line a section publishes as a TEMPLATE, sorted.
//
// Two loci are read, because the SPEC uses both: a backtick span (prose and
// table cells) and a bare line inside a fenced code block. A string qualifies
// only when it carries the `Error: ` prefix, the shared detail, and the
// placeholder — the last of which is what separates a published template from
// the worked examples that name a real token.
func publishedTemplateRefusals(lines []string) []string {
	seen := make(map[string]bool)
	var out []string

	add := func(candidate string) {
		switch {
		case !strings.HasPrefix(candidate, specErrorLinePrefix),
			!strings.Contains(candidate, refusalDetail),
			!strings.Contains(candidate, quotedPlaceholder),
			seen[candidate]:
			return
		}
		seen[candidate] = true
		out = append(out, candidate)
	}

	for _, line := range lines {
		for _, span := range backtickSpan.FindAllStringSubmatch(line, -1) {
			add(span[1])
		}
		add(strings.TrimSpace(line))
	}

	sort.Strings(out)
	return out
}

// publishedPair splits the two template refusals a section publishes into the
// canonical line and the longer line built from it, and returns their
// difference as the hint.
//
// The split is by CONTAINMENT and not by content: the gate does not know which
// line is which until it sees that one is a proper prefix of the other. A
// section publishing anything but exactly two, or two that do not stand in that
// relation, fails here — which is the whole point, because "the graph line is
// the canonical line with a hint appended" is exactly the claim being read.
func publishedPair(t *testing.T, where string, published []string) (canonical, extended, hint string) {
	t.Helper()

	if len(published) != 2 {
		t.Fatalf("%s publishes %d template refusal(s), want exactly 2 — the canonical line and the "+
			"`graph` line built from it: %q", where, len(published), published)
	}

	shorter, longer := published[0], published[1]
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}
	if shorter == longer || !strings.HasPrefix(longer, shorter) {
		t.Fatalf("%s publishes two refusals that are not one built from the other:\n"+
			"  %q\n  %q\n"+
			"SPEC/GRAPH.md acceptance criterion 28 requires the `graph` line to be the canonical "+
			"CLI-wide line with this family's hint APPENDED, so the shared half must be shared "+
			"character for character", where, shorter, longer)
	}
	return shorter, longer, strings.TrimPrefix(longer, shorter)
}

// publishedCanonicalRefusalLine, publishedGraphRefusalLine and
// publishedGraphHint are the three values the rest of the suite draws from the
// SPEC. Each re-reads the file: the gate runs a handful of times, and a cached
// value read once would be one more thing that could go stale.
func publishedCanonicalRefusalLine(t *testing.T) string {
	t.Helper()
	canonical, _, _ := publishedPair(t, commandsSpecPath+" § Positional Arguments",
		publishedTemplateRefusals(readSpecSection(t, commandsSpecPath, positionalArgumentsHeading)))
	return canonical
}

func publishedGraphRefusalLine(t *testing.T) string {
	t.Helper()
	_, extended, _ := publishedPair(t, commandsSpecPath+" § Positional Arguments",
		publishedTemplateRefusals(readSpecSection(t, commandsSpecPath, positionalArgumentsHeading)))
	return extended
}

func publishedGraphHint(t *testing.T) string {
	t.Helper()
	_, _, hint := publishedPair(t, commandsSpecPath+" § Positional Arguments",
		publishedTemplateRefusals(readSpecSection(t, commandsSpecPath, positionalArgumentsHeading)))
	return hint
}

// refusalLineWithToken substitutes a real offending token for the published
// placeholder, so a published template can be compared against a captured line
// character for character.
func refusalLineWithToken(t *testing.T, published, token string) string {
	t.Helper()

	if token == "X" {
		t.Fatalf("the offending token must not be the placeholder itself, or the substitution below " +
			"would prove nothing")
	}
	if !strings.Contains(published, quotedPlaceholder) {
		t.Fatalf("the published line %q carries no %s placeholder to substitute", published, quotedPlaceholder)
	}
	return strings.Replace(published, quotedPlaceholder, `"`+token+`"`, 1)
}

// ---------------------------------------------------------------------------
// Reading 0: the prefix that turns a handler's error into the user's line
// ---------------------------------------------------------------------------

// TestPositionalRefusal_TheErrorLinePrefixIsTheOneTheSpecPublishes pins
// specErrorLinePrefix, the single literal this file carries, to SPEC/HELP.md
// § Error line.
//
// Without it the comparisons below could pass while every user-visible line
// started with something else, because a handler's error value never carries
// the prefix and nothing else in this package writes it.
func TestPositionalRefusal_TheErrorLinePrefixIsTheOneTheSpecPublishes(t *testing.T) {
	lines := readSpecSection(t, helpSpecPath, errorLineHeading)

	const anchor = "The wording starts with"
	for _, line := range lines {
		if !strings.Contains(line, anchor) {
			continue
		}
		spans := backtickSpan.FindAllStringSubmatch(line, -1)
		if len(spans) == 0 {
			t.Fatalf("%s § %s: the line beginning %q quotes no prefix", helpSpecPath, errorLineHeading, anchor)
		}
		if got := spans[0][1]; got != specErrorLinePrefix {
			t.Fatalf("%s § %s publishes the prefix %q; this package models it as %q",
				helpSpecPath, errorLineHeading, got, specErrorLinePrefix)
		}
		return
	}
	t.Fatalf("%s § %s no longer contains a line beginning %q; the prefix is unpinned",
		helpSpecPath, errorLineHeading, anchor)
}

// ---------------------------------------------------------------------------
// Reading 1 and 2: the two published lines, and the hint between them
// ---------------------------------------------------------------------------

// TestPositionalRefusal_TheGraphLineIsTheCanonicalLinePlusThisFamilysHint is
// acceptance criterion 28 read against the SPEC alone.
//
// It asserts the relation and never the content: that § Positional Arguments
// publishes exactly two template refusals, that one is a proper prefix of the
// other, that SPEC/GRAPH.md publishes the longer of the two verbatim, and that
// SPEC/GRAPH.md's rule 6 quotes exactly the difference between them.
//
// The last of those is what stops rule 6 from describing a hint the line does
// not carry — the failure mode of any prose that restates a string in its own
// words.
func TestPositionalRefusal_TheGraphLineIsTheCanonicalLinePlusThisFamilysHint(t *testing.T) {
	where := commandsSpecPath + " § Positional Arguments"
	canonical, graphLine, hint := publishedPair(t, where,
		publishedTemplateRefusals(readSpecSection(t, commandsSpecPath, positionalArgumentsHeading)))

	if hint == "" {
		t.Fatalf("%s publishes a `graph` line identical to the canonical one; the hint that "+
			"distinguishes the family has been lost", where)
	}
	if strings.Contains(canonical, hint) {
		t.Errorf("the hint %q also appears in the canonical line %q; SPEC/GRAPH.md rule 6 confines it "+
			"to the `graph` family, because it names the two sources of a CYPHER QUERY and no other "+
			"family has them", hint, canonical)
	}
	if !strings.Contains(canonical, quotedPlaceholder) {
		t.Errorf("the canonical line %q carries no %s placeholder; nothing could be substituted into it",
			canonical, quotedPlaceholder)
	}

	graphSection := readSpecSection(t, graphSpecPath, noPositionalQueryHeading)

	// The same line, published a second time in the file that is canonical for
	// this family's whole rule. Two files publishing one line is safe only
	// while something compares them.
	graphPublished := publishedTemplateRefusals(graphSection)
	if len(graphPublished) != 1 {
		t.Fatalf("%s § %s publishes %d template refusal(s), want exactly 1: %q",
			graphSpecPath, sectionName(noPositionalQueryHeading), len(graphPublished), graphPublished)
	}
	if graphPublished[0] != graphLine {
		t.Errorf("the two files no longer publish one line:\n  %s: %q\n  %s: %q",
			commandsSpecPath, graphLine, graphSpecPath, graphPublished[0])
	}

	// Rule 6 quotes the hint on its own. Finding it by EQUALITY against the
	// derived difference is what makes the check about the hint the line
	// actually carries rather than about a string this test chose.
	quoted := false
	for _, line := range graphSection {
		for _, span := range backtickSpan.FindAllStringSubmatch(line, -1) {
			if span[1] == hint {
				quoted = true
			}
		}
	}
	if !quoted {
		t.Errorf("%s § %s never quotes the hint %q on its own; its rule 6 calls the hint part of the "+
			"published line, so the text it quotes must be the text the line carries",
			graphSpecPath, sectionName(noPositionalQueryHeading), hint)
	}
}

// ---------------------------------------------------------------------------
// Reading 3: the comment family's section, and the bridge back
// ---------------------------------------------------------------------------

// TestPositionalRefusal_TheCommentContractPublishesTheCanonicalLineAndCitesTheGraphOne
// is the other half of criterion 28 on the SPEC side.
//
// § Comment Positional Argument Contract must publish exactly two template
// refusals as well, and they must be the SAME two: the canonical line, which is
// what these eight subcommands emit, and the `graph` line, which it cites in
// order to say how the other family differs. That second one is the bridge the
// criterion requires — "a reader who finds one of these two sections must be
// able to reach the other from it" — and a citation nothing compares is a
// citation that goes stale.
func TestPositionalRefusal_TheCommentContractPublishesTheCanonicalLineAndCitesTheGraphOne(t *testing.T) {
	canonical := publishedCanonicalRefusalLine(t)
	graphLine := publishedGraphRefusalLine(t)

	where := commandsSpecPath + " § Comment Positional Argument Contract"
	published := publishedTemplateRefusals(readSpecSection(t, commandsSpecPath, commentContractHeading))

	want := []string{canonical, graphLine}
	sort.Strings(want)

	if len(published) != len(want) {
		t.Fatalf("%s publishes %d template refusal(s), want exactly 2 — its own line and the `graph` "+
			"line it cites: got %q, want %q", where, len(published), published, want)
	}
	for i := range want {
		if published[i] != want[i] {
			t.Errorf("%s publishes %q where the family relationship requires %q",
				where, published[i], want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Reading 4: the binary
// ---------------------------------------------------------------------------

// TestPositionalRefusal_TheThreeProducersStandInThePublishedRelation drives the
// three places in the binary that refuse an excess positional argument and
// compares what they return against the published relation.
//
// The three are deliberately different code paths:
//
//   - checkPositionalArity (positional_arity.go), the shared enforcement point,
//     reached here through `roadmap list`, which belongs to neither family;
//   - readQuery (graph.go), reached through all five `graph` subcommands;
//   - parseCommentArgs (comments.go), reached through all eight comment
//     subcommands.
//
// One offending token is used throughout, so the three captured lines differ
// only where the WORDING differs. The assertions are then exactly the
// criterion's two sentences: the `graph` line is the canonical line plus the
// hint, and the comment line is the canonical line unchanged.
//
// This is the reading that fails when the code drifts from the SPEC on either
// side, and it fails on a ONE-SIDED edit specifically: rewording the shared half
// in graph.go alone leaves the graph line no longer equal to the canonical line
// plus the hint, and rewording it in positional_arity.go alone leaves both the
// graph line and the comment line disagreeing with it.
func TestPositionalRefusal_TheThreeProducersStandInThePublishedRelation(t *testing.T) {
	canonicalPublished := publishedCanonicalRefusalLine(t)
	graphPublished := publishedGraphRefusalLine(t)
	hint := publishedGraphHint(t)

	// One token for all three families. It is a plausible report name a caller
	// might really append by mistake, and it carries no character that any of
	// the three producers treats specially.
	const stray = "reconciliation-report"

	wantCanonical := refusalLineWithToken(t, canonicalPublished, stray)
	wantGraph := refusalLineWithToken(t, graphPublished, stray)

	// The comment seed re-points HOME at a temporary directory of its own, so
	// it runs BEFORE the graph roadmap is created under that same HOME.
	seed := setupCommentPositionalRoadmap(t, "positional-refusal-families")
	graphRoadmap := seedGraphStrayRoadmap(t, "positional-refusal-families-graph")

	// --- the canonical producer -------------------------------------------

	canonicalOut, canonicalErr := dispatchInvocation(t, "roadmap", "list", stray)
	if canonicalErr == nil {
		t.Fatalf("`roadmap list %s` was accepted; the shared enforcement point produced no line to "+
			"compare the two families against", stray)
	}
	if !errors.Is(canonicalErr, utils.ErrInvalidInput) {
		t.Errorf("the canonical refusal = %v, want it to wrap utils.ErrInvalidInput (exit 2)", canonicalErr)
	}
	if canonicalOut != "" {
		t.Errorf("a refused `roadmap list` wrote to stdout: %q", canonicalOut)
	}
	canonicalLive := errorLine(canonicalErr)
	if canonicalLive != wantCanonical {
		t.Fatalf("the shared enforcement point emits %q; %s § Positional Arguments publishes %q. "+
			"Every comparison below is relative to this line, so the run stops here.",
			canonicalLive, commandsSpecPath, wantCanonical)
	}

	// --- the graph producer -----------------------------------------------

	for _, c := range graphStrayCases() {
		t.Run("graph/"+c.class, func(t *testing.T) {
			out, err := dispatchInvocation(t, graphFamilyName,
				graphSubcommandName, "-r", graphRoadmap, "--query", c.query, stray)
			if err == nil {
				t.Fatalf("a stray positional argument was accepted; stdout=%q", out)
			}
			if out != "" {
				t.Errorf("a refused invocation wrote to stdout: %q", out)
			}

			got := errorLine(err)
			if got != canonicalLive+hint {
				t.Errorf("`graph execute` given a %s statement emits\n  %q\nbut the canonical line plus "+
					"this family's hint is\n  %q\nSPEC/GRAPH.md acceptance criterion 28 makes the "+
					"`graph` line the canonical CLI-wide line with the hint appended, so the shared "+
					"half must be the shared half", c.class, got, canonicalLive+hint)
			}
			if got != wantGraph {
				t.Errorf("`graph execute` given a %s statement emits %q; %s publishes %q",
					c.class, got, commandsSpecPath, wantGraph)
			}
		})
	}

	// --- the comment producer ---------------------------------------------

	for _, sub := range allCommentSubcommands() {
		t.Run("comment/"+strings.ReplaceAll(sub.name, " ", "-"), func(t *testing.T) {
			args := argsFor(seed.roadmap, "1", []string{stray}, tailFor(sub.name))

			var err error
			out := captureStdout(t, func() { err = sub.handler(args) })
			if err == nil {
				t.Fatalf("a second positional argument was accepted; stdout=%q", out)
			}
			if out != "" {
				t.Errorf("a refused invocation wrote to stdout: %q", out)
			}

			got := errorLine(err)
			if got != canonicalLive {
				t.Errorf("`%s` emits\n  %q\nbut the canonical CLI-wide line is\n  %q\n"+
					"SPEC/COMMANDS.md § Comment Positional Argument Contract gives these eight the "+
					"canonical line unchanged", sub.name, got, canonicalLive)
			}
			if strings.Contains(got, hint) {
				t.Errorf("`%s` emits %q, which carries the `graph` family's hint %q. The hint names the "+
					"two sources of a Cypher query; a comment body comes from --body or standard input "+
					"and never from --query, so the hint would be false here", sub.name, got, hint)
			}
		})
	}

	seed.assertSeedIntact(t, "after driving the comment producer")
}

// ---------------------------------------------------------------------------
// The citation floor
// ---------------------------------------------------------------------------

// citationScanRoots are the directories holding tests that could pin either
// family's wording: this package, the `main` package where the six global forms
// are enforced, and the end-to-end suite, which drives the compiled binary and
// is where a literal copy is likeliest to be pasted.
var citationScanRoots = []string{".", "../../cmd/rmp", "../../tests"}

// TestPositionalRefusal_APinOfOneFamilysWordingCitesTheOther enforces the
// closing sentence of acceptance criterion 28 over the test suite itself: a
// test that asserts one family's wording must cite the other's.
//
// The detection is deliberately coarse. It is a FLOOR, not a proof: the four
// readings above are what catch drift, and this only stops a future test file
// from pinning one family's line while saying nothing about the family that
// shares it — which is how two copies come to be maintained separately in the
// first place.
//
//   - A file PINS the `graph` wording when it contains the hint, which appears
//     in no other line in the CLI.
//   - A file SPEAKS FOR the comment family when it names that family or its
//     SPEC section.
//
// A file doing either must mention the other family somewhere in its text. The
// hint counts as a mention of the `graph` family, which is why a file that pins
// the `graph` line satisfies the second rule by construction.
//
// The hint is searched for WITHOUT the space that joins it to the canonical
// line: a source file is free to wrap the assembled line across two adjacent
// string literals, which puts that space at the end of one and the hint's own
// text at the start of the next. Searching for the joined form would then miss a
// genuine pin — as it did for tests/test_57_positional_arity.py, whose assertion
// is written exactly that way.
func TestPositionalRefusal_APinOfOneFamilysWordingCitesTheOther(t *testing.T) {
	hint := strings.TrimSpace(publishedGraphHint(t))
	if hint == "" {
		t.Fatalf("the published hint is empty once trimmed; the scan below would match every file")
	}

	graphMentions := []string{hint, "graph subcommand", "graph family", "No Positional Query"}
	commentMentions := []string{"comment subcommand", "Comment Positional Argument"}

	mentions := func(body string, needles []string) bool {
		for _, n := range needles {
			if strings.Contains(body, n) {
				return true
			}
		}
		return false
	}

	scanned, pinning := 0, 0
	for _, root := range citationScanRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read %s: %v; the scan below would silently cover nothing", root, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !(strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".py")) {
				continue
			}
			path := filepath.Join(root, name)
			// The path is built from citationScanRoots, a fixed list of
			// in-repository directories, and a name ReadDir returned from one
			// of them, so nothing here reaches outside the repository.
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read %s: %v", path, readErr)
			}
			body := string(raw)
			if !strings.Contains(body, refusalDetail) {
				continue
			}
			scanned++

			pinsGraph := strings.Contains(body, hint)
			if pinsGraph {
				pinning++
				if !mentions(body, commentMentions) {
					t.Errorf("%s pins the `graph` family's refusal line and never names the comment "+
						"subcommands, which publish the same line without this family's hint. "+
						"SPEC/GRAPH.md acceptance criterion 28 requires a test that asserts one "+
						"family's wording to cite the other's, so the shared half is never reworded "+
						"by copying.", path)
				}
			}
			if mentions(body, commentMentions) && !mentions(body, graphMentions) {
				t.Errorf("%s asserts the comment subcommands' refusal and never names the `graph` "+
					"family, which publishes the same line with a hint appended. See "+
					"SPEC/COMMANDS.md § Comment Positional Argument Contract, \"The other family "+
					"that publishes this refusal\".", path)
			}
		}
	}

	if scanned == 0 {
		t.Fatalf("the scan of %v found no test file mentioning %q; the gate is reading the wrong "+
			"directories and passes vacuously", citationScanRoots, refusalDetail)
	}
	if pinning == 0 {
		t.Fatalf("the scan of %v found no test file pinning the `graph` family's hint %q; either the "+
			"hint changed shape or the gate is reading the wrong directories", citationScanRoots, hint)
	}
}
