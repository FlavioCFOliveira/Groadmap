// Package commands — the `graph` family's refusal of a stray positional
// argument, read against every class of statement and every subcommand that
// runs one.
//
// # What is pinned here
//
// SPEC/GRAPH.md § No Positional Query: A Stray Token Is Refused is canonical.
// `graph execute` and `graph client` accept no positional argument at all — the
// Cypher each runs comes from `--query` or from standard input and from nowhere
// else — so a bare query written on the command line is an excess positional
// argument and is refused with exit code 2 and one published line, the SAME line
// on both:
//
//	Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)
//
// Acceptance criteria 25 and 26 of that file are what this suite holds:
//
//   - 25. The refusal is asserted on the WHOLE line, the parenthetical hint
//     included, with exit code 2 through utils.ErrInvalidInput and zero bytes on
//     stdout.
//   - 26. The classification of a `-`-prefixed token, asserted in BOTH
//     directions. On this family a `-` followed by a digit or a decimal point
//     is a query value and not a flag, so a stray `-1` and a stray bare `-`
//     are UNEXPECTED ARGUMENTS, while a genuine long flag the family does not
//     define is an UNKNOWN FLAG. The comment subcommands classify the same
//     `-1` the other way (SPEC/COMMANDS.md § Comment Positional Argument
//     Contract, rule 2, pinned in comment_positional_test.go), and the two
//     refusals share exit code 2 — so nothing but the wording tells them
//     apart, and only an assertion on the wording can hold the difference. Of
//     several stray tokens only the first is named.
//
// # What this file used to be
//
// It drove FIVE subcommands and compared their five refusal lines against each
// other, because they shared one argument parser and a wording that drifted on
// one of them was the failure the criterion existed to catch. `create`, `query`,
// `update`, `delete` and `search` are no longer subcommand names
// (SPEC/COMMANDS.md § Graph Management), so that cross-comparison has nothing
// left to compare.
//
// The table did not become a single row, because a single row would have made
// every "one wording" assertion in this file vacuous. What it varies now is the
// STATEMENT CLASS: a read, a write, a property update, a deletion, a traversal
// and a schema statement, each of which would succeed were the stray token
// removed. The property that replaces the old one is the one that now matters —
// the refusal does not depend on what the statement would have done, which is
// exactly the claim a family with no operation-class check has to keep.
//
// # Why the family is still enumerated from the registry
//
// The subcommands driven below are read off AppRegistry() rather than written
// out here: a further statement-reading `graph` subcommand is driven through
// every assertion in this file the day it is registered, and one that publishes
// the hinted refusal without reading a statement — or reads one without
// publishing it — fails TestGraphPositional_TableCoversTheWholeFamily. Neither
// can be silently left out.
//
// # Why each case carries a query that would otherwise succeed
//
// Every invocation driven below is one whose outcome WITHOUT the stray token is
// known and is not the refusal being asserted, and
// TestGraphPositional_EveryStatementClassRefusesWithOneWording proves it by
// running each control first. Without that half, a case could be passing on a
// missing-query refusal that happened to carry the right sentinel, and the suite
// would be asserting nothing about the stray token at all. What "known" means
// differs by subcommand and graphControlExpectation is where that is decided:
// `graph execute` must succeed, while `graph client` — which requires a server
// no unit test starts — must fail with something that is not this refusal.
//
// Dispatch goes through Command.DispatchFamily, never through runGraphExecute
// directly, because the shared arity enforcement point sits on that path and
// must be proven to DEFER to this family's own wording rather than override it
// (checkPositionalArity, positional_arity.go).
package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// graphFamilyName is the family every case below dispatches through.
// graphSubcommandName is the subcommand the SEEDS run through: seeding needs a
// statement that reaches the store, which is the one thing `graph client` cannot
// do without a server. The probes themselves are driven over every subcommand
// that publishes the hinted refusal, read from the registry.
const (
	graphFamilyName     = "graph"
	graphSubcommandName = "execute"
)

// graphStrayCase pairs one class of Cypher statement with a query of that class,
// so the invocation is otherwise valid and its refusal can only be the stray
// token's doing.
type graphStrayCase struct {
	// class names the statement class, and is the subtest's name.
	class string
	// query executes against the seeded store and would succeed on its own.
	query string
	// seeded names what the control invocation acts on, purely so a failure
	// message can say which one.
	seeded string
}

// graphStrayCases is the statement-class table. The queries act on different
// nodes of the same seeded graph, so the controls can run in one roadmap without
// one of them undoing another; the delete runs on a node no other case touches.
func graphStrayCases() []graphStrayCase {
	return []graphStrayCase{
		{class: "create", query: "CREATE (:Spec {key:'chargeback-handling'})", seeded: "chargeback-handling"},
		{class: "read", query: "MATCH (s:Spec) RETURN s.key ORDER BY s.key", seeded: "every Spec"},
		{class: "update", query: "MATCH (s:Spec {key:'payment-capture'}) SET s.status = 'ready'", seeded: "payment-capture"},
		{class: "delete", query: "MATCH (s:Spec {key:'refund-flow'}) DETACH DELETE s", seeded: "refund-flow"},
		{class: "traversal", query: "MATCH p=(a:Spec)-[*1..3]-(b:Spec) RETURN p", seeded: "the DEPENDS_ON path"},
		{class: "schema-ddl", query: "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)", seeded: "the spec_key index"},
		{class: "schema-introspection", query: "SHOW INDEXES", seeded: "the registered schema"},
	}
}

// seedGraphStrayRoadmap creates the roadmap and the graph the controls act on,
// and returns the roadmap name. The graph carries two linked specs plus one
// standalone spec, which is enough for a read, a traversal, a property write
// and a delete to each have something real to touch.
func seedGraphStrayRoadmap(t *testing.T, name string) string {
	t.Helper()

	t.Cleanup(setupTestGraphRoadmap(t, name))

	for _, seed := range []string{
		"CREATE (:Spec {key:'payment-capture'})-[:DEPENDS_ON]->(:Spec {key:'ledger-posting'})",
		"CREATE (:Spec {key:'refund-flow'})",
	} {
		out, err := dispatchInvocation(t, graphFamilyName, graphSubcommandName, "-r", name, "--query", seed)
		if err != nil {
			t.Fatalf("seeding the graph store with %q: %v", seed, err)
		}
		if out == "" {
			t.Fatalf("seeding the graph store with %q wrote nothing to stdout; the seed did not run", seed)
		}
	}
	return name
}

// registeredGraphSubcommands reads the family straight out of AppRegistry and
// partitions it by the property this file is about: whether a subcommand
// publishes the HINTED refusal or the canonical one.
//
// The partition is not a convenience. SPEC/COMMANDS.md § Positional Arguments
// confines the hint — "(graph queries use --query or stdin)" — to the
// subcommands that READ A CYPHER STATEMENT, "because it names the two sources
// such a statement may come from and no other command has them", and says in the
// same breath that `graph serve` "reads no statement and publishes the canonical
// line of rule 1 instead". A family-wide rule would therefore have been wrong
// about the family the moment it gained a subcommand that runs no statement.
//
// Both halves are checked, because both are declarations the assertions below
// depend on:
//
//   - Every graph subcommand takes NO positional argument, statement-reading or
//     not. That is what makes a stray token an excess one on all of them.
//   - A statement-reading subcommand publishes its own refusal wording, so the
//     shared enforcement point defers to it and the hinted line survives.
//   - Every other one does NOT, so the shared point answers it with the
//     canonical line. Asserting this direction is what stops the hint from
//     spreading to a subcommand the specification does not give it to.
func registeredGraphSubcommands(t *testing.T) (hinted, canonical []string) {
	t.Helper()

	cmd := AppRegistry().FindCommand(graphFamilyName)
	if cmd == nil {
		t.Fatalf("family %q missing from the registry", graphFamilyName)
	}

	for i := range cmd.Subcommands {
		sub := &cmd.Subcommands[i]
		if got := len(sub.Positional); got != 0 {
			t.Errorf("graph %s declares %d positional argument(s); SPEC/GRAPH.md § No Positional Query "+
				"gives every graph subcommand a maximum of zero", sub.Name, got)
		}
		if sub.PublishesOwnArityRefusal {
			hinted = append(hinted, sub.Name)
			continue
		}
		canonical = append(canonical, sub.Name)
	}
	if len(hinted) == 0 {
		t.Fatalf("no graph subcommand publishes its own arity refusal; every table in this file " +
			"would be vacuous")
	}
	return hinted, canonical
}

// TestGraphPositional_TableCoversTheWholeFamily keeps this file honest against
// the registry: every assertion below is driven over registeredGraphSubcommands'
// HINTED half, so the suite covers the hinted refusal for the whole of the class
// that publishes it rather than for one member of it.
//
// It is keyed on the CLASS and not on the family, because the family stopped
// being the class when `rmp graph serve` was added: that subcommand reads no
// statement, so the hint naming the two sources of one would be a hint about
// something it does not have, and SPEC/COMMANDS.md § Positional Arguments gives
// it the canonical line instead.
//
// What the gate asserts is the SPEC's own rule, in both directions rather than a
// count: the hint is confined to "the subcommands that read a Cypher statement,
// `graph execute` and `graph client`", so a subcommand publishes the hinted
// refusal EXACTLY when it declares the flag through which such a statement is
// supplied. A count would have had to be bumped when `graph client` landed; this
// does not, and it fails just as loudly if a subcommand ever publishes the hint
// without reading a statement, or reads one without publishing the hint.
func TestGraphPositional_TableCoversTheWholeFamily(t *testing.T) {
	hinted, canonical := registeredGraphSubcommands(t)

	for _, name := range hinted {
		sub := AppRegistry().FindCommand(graphFamilyName).FindSubcommand(name)
		if sub == nil {
			t.Fatalf("graph %s vanished from the registry between two reads of it", name)
		}
		if !hasQueryFlag(sub) {
			t.Errorf("graph %s publishes its own arity refusal but declares no --query flag, so the "+
				"hinted line would name two statement sources it does not have. SPEC/COMMANDS.md "+
				"§ Positional Arguments confines the hint to the subcommands that read a Cypher "+
				"statement", name)
		}
	}
	for _, name := range canonical {
		sub := AppRegistry().FindCommand(graphFamilyName).FindSubcommand(name)
		if sub == nil {
			t.Fatalf("graph %s vanished from the registry between two reads of it", name)
		}
		if hasQueryFlag(sub) {
			t.Errorf("graph %s takes a --query flag but does not publish its own arity refusal, so "+
				"the shared enforcement point answers it with the canonical line. SPEC/COMMANDS.md "+
				"§ Positional Arguments gives the hinted line to the subcommands that read a Cypher "+
				"statement; this one reads one and would not print it", name)
		}
	}

	// Every hinted subcommand must be one this file knows how to CONTROL, or the
	// probes driven over it would be asserting nothing. graphControlExpectation
	// is the total function that decides which control a subcommand gets, and it
	// fails rather than guessing for a name it has never seen.
	for _, name := range hinted {
		graphControlExpectation(t, name)
	}

	seen := make(map[string]bool, len(graphStrayCases()))
	for _, c := range graphStrayCases() {
		if seen[c.class] {
			t.Errorf("graphStrayCases lists the statement class %q twice", c.class)
		}
		seen[c.class] = true
	}
	if len(seen) < 2 {
		t.Errorf("graphStrayCases carries %d statement class(es); the cross-comparison in "+
			"TestGraphPositional_EveryStatementClassRefusesWithOneWording needs at least two to "+
			"compare anything", len(seen))
	}
}

// graphControlExpectation says what a CONTROL invocation of one hinted
// subcommand — the same invocation without the stray token — must do, so that a
// probe's refusal is known to be the stray token's doing and not a failure the
// invocation was going to have anyway.
//
// The two subcommands need different controls, and the difference is their whole
// contract rather than a testing convenience:
//
//   - `graph execute` runs the statement against the store when nothing is
//     serving the roadmap, so its control SUCCEEDS and writes a result. That is
//     the strongest control there is.
//   - `graph client` requires a server and there is none in a unit test, so its
//     control cannot succeed. What it must do instead is fail with something
//     OTHER than the arity refusal — the no-server line, which is a different
//     class and a different exit code — which establishes exactly what the strong
//     control establishes: that the refusal the probe reads is caused by the
//     stray token. Running a server here to make it succeed would make this file
//     an end-to-end suite, which is rmp task #371's, not this one's.
//
// It is total by construction: a hinted subcommand it has no entry for fails,
// rather than being driven with an expectation somebody guessed.
func graphControlExpectation(t *testing.T, subcommand string) (mustSucceed bool) {
	t.Helper()
	switch subcommand {
	case "execute":
		return true
	case "client":
		return false
	default:
		t.Fatalf("graph %s publishes the hinted arity refusal and this file does not know what a "+
			"control invocation of it must do. Add it to graphControlExpectation rather than "+
			"leaving it driven with an expectation nobody chose", subcommand)
		return false
	}
}

// runGraphStrayControl drives one control invocation and holds it to whatever
// graphControlExpectation says about that subcommand. It reports nothing on
// success and fails the test otherwise, so a caller can read the probe below as
// asserting the stray token's effect alone.
func runGraphStrayControl(t *testing.T, subcommand, roadmap string, c graphStrayCase) {
	t.Helper()

	out, err := dispatchInvocation(t, graphFamilyName, subcommand, "-r", roadmap, "--query", c.query)
	if graphControlExpectation(t, subcommand) {
		if err != nil {
			t.Fatalf("the control invocation of `graph %s` with the %s statement (acting on %s) "+
				"failed with %v; the probe below would then be asserting nothing about the stray token",
				subcommand, c.class, c.seeded, err)
		}
		if out == "" {
			t.Fatalf("the control invocation of `graph %s` wrote nothing to stdout; it reports its "+
				"result there whatever the statement, so %q did not run", subcommand, c.query)
		}
		return
	}

	// The weaker control: the invocation is expected to fail, but NOT with the
	// refusal the probe is about. A control that already produced that refusal
	// would make the probe vacuous, which is the one thing this half exists to
	// rule out.
	if err == nil {
		t.Fatalf("the control invocation of `graph %s` with the %s statement succeeded, and this "+
			"file was written expecting it to fail for want of a server. Give it the strong control "+
			"in graphControlExpectation rather than leaving the weak one in place", subcommand, c.class)
	}
	if errors.Is(err, utils.ErrInvalidInput) {
		t.Fatalf("the control invocation of `graph %s` with the %s statement already failed with "+
			"utils.ErrInvalidInput (%v), so the probe below cannot tell the stray token's refusal "+
			"from it", subcommand, c.class, err)
	}
	if out != "" {
		t.Errorf("the control invocation of `graph %s` failed and still wrote to stdout: %q",
			subcommand, out)
	}
}

// TestGraphPositional_EveryStatementClassRefusesWithOneWording is acceptance
// criterion 25 of SPEC/GRAPH.md.
//
// Two halves, and the suite needs both:
//
//   - the CONTROL, which runs each invocation without the stray token and
//     requires the outcome graphControlExpectation fixes for that subcommand, so
//     the refusal below is caused by the stray token and not by a failure the
//     invocation was going to have anyway;
//   - the PROBE, which adds the stray token and requires the exact published
//     line, the parenthetical included, exit code 2 through utils.ErrInvalidInput,
//     and an empty stdout.
//
// The probe lines are then compared AGAINST EACH OTHER, across statement classes
// AND across the subcommands that publish the hinted line. That comparison is
// what survives the collapse of the five subcommands: `graph execute` holds no
// opinion about what a statement does, so its refusal of a stray token must not
// vary with the statement either — a refusal that named the class, or that
// reached a different branch for a write than for a read, would be a class
// distinction reappearing in the one place the family has left to put one. The
// cross-SUBCOMMAND half is the same property one level up: `graph client` reads
// its statement from the same two sources through the same reader, so a wording
// that drifted on one of them would be the drift the five-subcommand version of
// this file was written to catch.
func TestGraphPositional_EveryStatementClassRefusesWithOneWording(t *testing.T) {
	roadmap := seedGraphStrayRoadmap(t, "graph-stray-wording")
	hinted, _ := registeredGraphSubcommands(t)

	// The offending token is the same across the classes, so the lines differ
	// only where the WORDING differs and the cross-comparison below reads as
	// exactly that.
	const stray = "reconciliation-report"

	// Read once from the SPEC so this file carries no second copy of the line;
	// the two families' relationship is held by
	// positional_refusal_families_test.go, which this test shares the reader
	// with (see SPEC/GRAPH.md acceptance criterion 60).
	want := refusalLineWithToken(t, publishedGraphRefusalLine(t), stray)

	// subcommand/statement class -> the line it produced, so a drifting member
	// can be named.
	produced := make(map[string]string, len(hinted)*len(graphStrayCases()))

	for _, subcommand := range hinted {
		for _, c := range graphStrayCases() {
			t.Run(subcommand+"/"+c.class, func(t *testing.T) {
				runGraphStrayControl(t, subcommand, roadmap, c)

				out, err := dispatchInvocation(t, graphFamilyName,
					subcommand, "-r", roadmap, "--query", c.query, stray)
				if err == nil {
					t.Fatalf("a stray positional argument was accepted; stdout=%q", out)
				}
				if !errors.Is(err, utils.ErrInvalidInput) {
					t.Errorf("error = %v, want it to wrap utils.ErrInvalidInput (exit 2)", err)
				}
				if got := errorLine(err); got != want {
					t.Errorf("stderr line = %q,\n                want %q", got, want)
				}
				if out != "" {
					t.Errorf("a refused invocation wrote to stdout: %q", out)
				}
				produced[subcommand+"/"+c.class] = errorLine(err)
			})
		}
	}

	distinct := make(map[string][]string)
	for key, line := range produced {
		distinct[line] = append(distinct[line], key)
	}
	if len(distinct) > 1 {
		for line, keys := range distinct {
			t.Errorf("the refusal no longer has one wording: %v emit %q", keys, line)
		}
	}
}

// TestGraphPositional_HyphenPrefixedTokensAreClassifiedBothWays is acceptance
// criterion 26 of SPEC/GRAPH.md, in both of the directions the criterion names.
//
// The two refusals carry the SAME exit code, so an exit-code assertion cannot
// tell them apart and a test that made one would keep passing if `-1` were
// reclassified as a flag. What the classification decides is which message the
// caller reads, and that is what is asserted: an unexpected argument names a
// token the command will never accept in any position, while an unknown flag
// invites the caller to look for the flag they meant.
//
// This is also the one point on which the `graph` family and the comment
// subcommands deliberately disagree about the same token: on a comment
// subcommand `-1` is an unknown flag (SPEC/COMMANDS.md § Comment Positional
// Argument Contract, rule 2). Both sides are asserted so neither can be
// "aligned" onto the other by mistake; the comment side lives in
// comment_positional_test.go, and the relationship between the two published
// lines is held by positional_refusal_families_test.go.
func TestGraphPositional_HyphenPrefixedTokensAreClassifiedBothWays(t *testing.T) {
	roadmap := seedGraphStrayRoadmap(t, "graph-stray-classification")

	cases := []struct {
		// token is the stray written after a well-formed --query value, so it
		// is a leftover token and never that flag's operand.
		token string
		// wantUnexpected selects which of the two refusals the token must draw.
		wantUnexpected bool
		why            string
	}{
		{
			token:          "-1",
			wantUnexpected: true,
			why: "a '-' followed by a digit is a negative numeric literal, which this family " +
				"passes to the engine as a query value rather than reading as a short flag",
		},
		{
			token:          "-0.5",
			wantUnexpected: true,
			why:            "a '-' followed by a decimal point is a numeric literal for the same reason",
		},
		{
			token:          "-",
			wantUnexpected: true,
			why:            "a bare '-' is neither a long flag nor '-' plus a letter, so it is not flag-like",
		},
		{
			token:          "--include-archived",
			wantUnexpected: false,
			why:            "a long flag graph execute does not define is an unknown flag, not a positional argument",
		},
		{
			token:          "-x",
			wantUnexpected: false,
			why:            "'-' followed by an ASCII letter is a short flag, and graph execute does not define this one",
		},
	}

	graphLine := publishedGraphRefusalLine(t)
	hinted, _ := registeredGraphSubcommands(t)

	for _, subcommand := range hinted {
		for _, c := range graphStrayCases() {
			for _, tc := range cases {
				t.Run(subcommand+"/"+c.class+"/"+tc.token, func(t *testing.T) {
					out, err := dispatchInvocation(t, graphFamilyName,
						subcommand, "-r", roadmap, "--query", c.query, tc.token)
					if err == nil {
						t.Fatalf("the stray token %q was accepted; stdout=%q", tc.token, out)
					}
					if !errors.Is(err, utils.ErrInvalidInput) {
						t.Errorf("error = %v, want it to wrap utils.ErrInvalidInput (exit 2)", err)
					}
					if out != "" {
						t.Errorf("a refused invocation wrote to stdout: %q", out)
					}

					got := errorLine(err)
					if tc.wantUnexpected {
						want := refusalLineWithToken(t, graphLine, tc.token)
						if got != want {
							t.Errorf("%s: line = %q, want %q (%s)", tc.token, got, want, tc.why)
						}
						return
					}

					wantFlag := "unknown flag: " + tc.token
					if !strings.Contains(got, wantFlag) {
						t.Errorf("%s: line = %q, want it to report %q (%s)", tc.token, got, wantFlag, tc.why)
					}
					if strings.Contains(got, "unexpected argument") {
						t.Errorf("%s: line = %q, but a genuine flag must NOT be reported as a positional "+
							"argument (%s)", tc.token, got, tc.why)
					}
				})
			}
		}
	}
}

// TestGraphPositional_OnlyTheFirstStrayTokenIsNamed is the remaining half of
// acceptance criterion 26: the tokens are examined left to right and the first
// positional argument ends the invocation.
//
// Both orders are driven. A stray written BEFORE the flags must be refused
// exactly as one written after them (SPEC/GRAPH.md § No Positional Query,
// rule 4), and the later token must not appear in the message at all — an
// assertion that merely looked for the first token would pass on a message that
// listed every one of them.
func TestGraphPositional_OnlyTheFirstStrayTokenIsNamed(t *testing.T) {
	roadmap := seedGraphStrayRoadmap(t, "graph-stray-first-only")

	// The two tokens play the roles SPEC/GRAPH.md writes as `alpha` and `beta`.
	const first = "reconciliation-report"
	const second = "settlement-summary"

	graphLine := publishedGraphRefusalLine(t)
	want := refusalLineWithToken(t, graphLine, first)
	hinted, _ := registeredGraphSubcommands(t)

	for _, subcommand := range hinted {
		for _, c := range graphStrayCases() {
			layouts := []struct {
				label string
				args  []string
			}{
				{
					label: "both strays after the flags",
					args:  []string{subcommand, "-r", roadmap, "--query", c.query, first, second},
				},
				{
					label: "the first stray written before the flags",
					args:  []string{subcommand, first, "-r", roadmap, "--query", c.query, second},
				},
			}
			for _, layout := range layouts {
				t.Run(subcommand+"/"+c.class+"/"+layout.label, func(t *testing.T) {
					out, err := dispatchInvocation(t, graphFamilyName, layout.args...)
					if err == nil {
						t.Fatalf("two stray positional arguments were accepted; stdout=%q", out)
					}
					if got := errorLine(err); got != want {
						t.Errorf("line = %q, want %q", got, want)
					}
					if strings.Contains(err.Error(), second) {
						t.Errorf("the message names the second stray token %q as well: %q; only the first "+
							"offending token may be named", second, err.Error())
					}
					if out != "" {
						t.Errorf("a refused invocation wrote to stdout: %q", out)
					}
				})
			}
		}
	}
}

// hasQueryFlag reports whether sub declares the flag through which a Cypher
// statement is supplied. It is what tells a statement-reading subcommand apart
// from one that runs no statement, read from the registry rather than from a
// name written out here.
func hasQueryFlag(sub *Subcommand) bool {
	for i := range sub.Flags {
		if sub.Flags[i].Long == "--query" {
			return true
		}
	}
	return false
}
