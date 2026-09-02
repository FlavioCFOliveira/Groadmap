// Package commands — the `graph` family's refusal of a stray positional
// argument, read against every class of statement the one subcommand runs.
//
// # What is pinned here
//
// SPEC/GRAPH.md § No Positional Query: A Stray Token Is Refused is canonical.
// `graph execute` accepts no positional argument at all — the Cypher it runs
// comes from `--query` or from standard input and from nowhere else — so a bare
// query written on the command line is an excess positional argument and is
// refused with exit code 2 and one published line:
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
// The table is checked against AppRegistry() rather than against a list written
// out here: a second `graph` subcommand added tomorrow fails
// TestGraphPositional_TableCoversTheWholeFamily instead of being silently left
// out of every assertion in the file.
//
// # Why each case carries a query that would otherwise succeed
//
// Every invocation driven below is one that would SUCCEED were the stray token
// removed, and TestGraphPositional_EveryStatementClassRefusesWithOneWording
// proves it by running each control first. Without that half, a case could be
// passing on a missing-query refusal that happened to carry the right sentinel,
// and the suite would be asserting nothing about the stray token at all.
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

// graphFamilyName is the family every case below dispatches through, and
// graphSubcommandName is its one subcommand.
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
// the registry: every assertion below dispatches graphSubcommandName, so the
// suite covers the hinted refusal only for as long as that name is the whole of
// the class that publishes it.
//
// It is keyed on the CLASS and not on the family, because the family stopped
// being the class when `rmp graph serve` was added: that subcommand reads no
// statement, so the hint naming the two sources of one would be a hint about
// something it does not have, and SPEC/COMMANDS.md § Positional Arguments gives
// it the canonical line instead. A second statement-reading subcommand added
// tomorrow — `graph client` is the one the specification already describes —
// fails here rather than being silently left out of every assertion in the file,
// which is the guarantee the five-name version of this test gave.
func TestGraphPositional_TableCoversTheWholeFamily(t *testing.T) {
	hinted, canonical := registeredGraphSubcommands(t)

	if len(hinted) != 1 || hinted[0] != graphSubcommandName {
		t.Errorf("the registry declares the statement-reading graph subcommands %v, and every "+
			"assertion in this file drives `graph %s` alone. A second one is a subcommand this suite "+
			"no longer covers", hinted, graphSubcommandName)
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

// TestGraphPositional_EveryStatementClassRefusesWithOneWording is acceptance
// criterion 25 of SPEC/GRAPH.md.
//
// Two halves, and the suite needs both:
//
//   - the CONTROL, which runs each invocation without the stray token and
//     requires it to succeed, so the refusal below is caused by the stray token
//     and not by a query that was going to fail anyway;
//   - the PROBE, which adds the stray token and requires the exact published
//     line, the parenthetical included, exit code 2 through utils.ErrInvalidInput,
//     and an empty stdout.
//
// The probe lines are then compared AGAINST EACH OTHER, across statement
// classes. That comparison is what survives the collapse of the five
// subcommands: `graph execute` holds no opinion about what a statement does, so
// its refusal of a stray token must not vary with the statement either — a
// refusal that named the class, or that reached a different branch for a write
// than for a read, would be a class distinction reappearing in the one place the
// family has left to put one.
func TestGraphPositional_EveryStatementClassRefusesWithOneWording(t *testing.T) {
	roadmap := seedGraphStrayRoadmap(t, "graph-stray-wording")

	// The offending token is the same across the classes, so the lines differ
	// only where the WORDING differs and the cross-comparison below reads as
	// exactly that.
	const stray = "reconciliation-report"

	// Read once from the SPEC so this file carries no second copy of the line;
	// the two families' relationship is held by
	// positional_refusal_families_test.go, which this test shares the reader
	// with (see SPEC/GRAPH.md acceptance criterion 60).
	want := refusalLineWithToken(t, publishedGraphRefusalLine(t), stray)

	// statement class -> the line it produced, so a drifting member can be named.
	produced := make(map[string]string, len(graphStrayCases()))

	for _, c := range graphStrayCases() {
		t.Run(c.class, func(t *testing.T) {
			controlOut, controlErr := dispatchInvocation(t, graphFamilyName,
				graphSubcommandName, "-r", roadmap, "--query", c.query)
			if controlErr != nil {
				t.Fatalf("the control invocation of the %s statement (acting on %s) failed with %v; "+
					"the probe below would then be asserting nothing about the stray token",
					c.class, c.seeded, controlErr)
			}
			if controlOut == "" {
				t.Fatalf("the control invocation wrote nothing to stdout; graph execute reports its "+
					"result there whatever the statement, so %q did not run", c.query)
			}

			out, err := dispatchInvocation(t, graphFamilyName,
				graphSubcommandName, "-r", roadmap, "--query", c.query, stray)
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
			produced[c.class] = errorLine(err)
		})
	}

	distinct := make(map[string][]string)
	for class, line := range produced {
		distinct[line] = append(distinct[line], class)
	}
	if len(distinct) > 1 {
		for line, classes := range distinct {
			t.Errorf("the refusal no longer has one wording: the %v statement classes emit %q",
				classes, line)
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

	for _, c := range graphStrayCases() {
		for _, tc := range cases {
			t.Run(c.class+"/"+tc.token, func(t *testing.T) {
				out, err := dispatchInvocation(t, graphFamilyName,
					graphSubcommandName, "-r", roadmap, "--query", c.query, tc.token)
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

	for _, c := range graphStrayCases() {
		layouts := []struct {
			label string
			args  []string
		}{
			{
				label: "both strays after the flags",
				args:  []string{graphSubcommandName, "-r", roadmap, "--query", c.query, first, second},
			},
			{
				label: "the first stray written before the flags",
				args:  []string{graphSubcommandName, first, "-r", roadmap, "--query", c.query, second},
			},
		}
		for _, layout := range layouts {
			t.Run(c.class+"/"+layout.label, func(t *testing.T) {
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
