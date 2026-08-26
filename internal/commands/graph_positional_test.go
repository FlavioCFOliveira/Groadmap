// Package commands — the `graph` family's refusal of a stray positional
// argument, read against ALL FIVE subcommands at once.
//
// # What is pinned here
//
// SPEC/GRAPH.md § No Positional Query: A Stray Token Is Refused is canonical.
// A graph subcommand accepts no positional argument at all — the Cypher it runs
// comes from `--query` or from standard input and from nowhere else — so a bare
// query written on the command line is an excess positional argument and is
// refused with exit code 2 and one published line:
//
//	Error: invalid input: unexpected argument "X" (graph queries use --query or stdin)
//
// Acceptance criteria 57 and 58 of that file are what this suite holds:
//
//   - 57. All five subcommands refuse, with ONE wording. The whole line is
//     compared, the parenthetical hint included, and the five lines are
//     compared AGAINST EACH OTHER. The five share one argument parser
//     (readQuery in graph.go), so the family has one wording and not five;
//     a wording that drifts on a single subcommand is the failure this
//     criterion exists to catch, and a suite that asserted only `graph query`
//     — which is all the CLI-wide arity suite does, in
//     TestPositionalArity_SelfRefusingCommandsKeepTheirWording — would stay
//     green through it.
//   - 58. The classification of a `-`-prefixed token, asserted in BOTH
//     directions. On this family a `-` followed by a digit or a decimal point
//     is a query value and not a flag, so a stray `-1` and a stray bare `-`
//     are UNEXPECTED ARGUMENTS, while a genuine long flag the family does not
//     define is an UNKNOWN FLAG. The comment subcommands classify the same
//     `-1` the other way (SPEC/COMMANDS.md § Comment Positional Argument
//     Contract, rule 2, pinned in comment_positional_test.go), and the two
//     refusals share exit code 2 — so nothing but the wording tells them
//     apart, and only an assertion on the wording can hold the difference.
//
// # Why the five are enumerated from the registry
//
// The table below is keyed by subcommand name and is checked against
// AppRegistry() rather than against a list written out here: a sixth `graph`
// subcommand added tomorrow fails TestGraphPositional_TableCoversTheWholeFamily
// instead of being silently left out of every assertion in the file.
//
// # Why each case carries a query of its own operation class
//
// Every invocation driven below is one that would SUCCEED were the stray token
// removed, and TestGraphPositional_AllFiveRefuseAStrayTokenWithOneWording proves
// it by running each control first. Without that half, a case could be passing
// on a guard-rail rejection (exit 6) or a missing-query refusal that happened to
// carry the right sentinel, and the suite would be asserting nothing about the
// stray token at all.
//
// Dispatch goes through Command.DispatchFamily, never through runGraphQuery and
// friends directly, because the shared arity enforcement point sits on that path
// and must be proven to DEFER to this family's own wording rather than override
// it (checkPositionalArity, positional_arity.go).
package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// graphFamilyName is the family every case below dispatches through.
const graphFamilyName = "graph"

// graphStrayCase pairs one `graph` subcommand with a Cypher query of that
// subcommand's own operation class, so the invocation is otherwise valid.
type graphStrayCase struct {
	subcommand string
	// query is accepted by that subcommand's guard rail and executes against
	// the seeded store.
	query string
	// seeded names the node the control invocation acts on, purely so a
	// failure message can say which one.
	seeded string
}

// graphStrayCases is the family-wide table. The queries act on different nodes
// of the same seeded graph, so the five controls can run in one roadmap without
// one of them undoing another.
func graphStrayCases() []graphStrayCase {
	return []graphStrayCase{
		{subcommand: "create", query: "CREATE (:Spec {key:'chargeback-handling'})", seeded: "chargeback-handling"},
		{subcommand: "query", query: "MATCH (s:Spec) RETURN s.key ORDER BY s.key", seeded: "every Spec"},
		{subcommand: "update", query: "MATCH (s:Spec {key:'payment-capture'}) SET s.status = 'ready'", seeded: "payment-capture"},
		{subcommand: "delete", query: "MATCH (s:Spec {key:'refund-flow'}) DETACH DELETE s", seeded: "refund-flow"},
		{subcommand: "search", query: "MATCH p=(a:Spec)-[*1..3]-(b:Spec) RETURN p", seeded: "the DEPENDS_ON path"},
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
		out, err := dispatchInvocation(t, graphFamilyName, "create", "-r", name, "--query", seed)
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
// checks the two declarations every assertion below depends on: each subcommand
// takes NO positional argument, and each publishes its own refusal wording so
// the shared enforcement point defers to it.
func registeredGraphSubcommands(t *testing.T) []string {
	t.Helper()

	cmd := AppRegistry().FindCommand(graphFamilyName)
	if cmd == nil {
		t.Fatalf("family %q missing from the registry", graphFamilyName)
	}

	names := make([]string, 0, len(cmd.Subcommands))
	for i := range cmd.Subcommands {
		sub := &cmd.Subcommands[i]
		if got := len(sub.Positional); got != 0 {
			t.Errorf("graph %s declares %d positional argument(s); SPEC/GRAPH.md § No Positional Query "+
				"gives every graph subcommand a maximum of zero", sub.Name, got)
		}
		if !sub.PublishesOwnArityRefusal {
			t.Errorf("graph %s no longer sets PublishesOwnArityRefusal; the shared enforcement point "+
				"would override this family's published line with the canonical one", sub.Name)
		}
		names = append(names, sub.Name)
	}
	if len(names) == 0 {
		t.Fatalf("the registry lists no graph subcommand; every table in this file would be vacuous")
	}
	return names
}

// TestGraphPositional_TableCoversTheWholeFamily keeps graphStrayCases honest
// against the registry. It is the reason the rest of the file can say "all five"
// rather than "the five somebody remembered".
func TestGraphPositional_TableCoversTheWholeFamily(t *testing.T) {
	registered := registeredGraphSubcommands(t)

	covered := make(map[string]bool, len(graphStrayCases()))
	for _, c := range graphStrayCases() {
		if covered[c.subcommand] {
			t.Errorf("graphStrayCases lists %q twice", c.subcommand)
		}
		covered[c.subcommand] = true
	}

	for _, name := range registered {
		if !covered[name] {
			t.Errorf("the registry declares `graph %s` and graphStrayCases does not drive it; "+
				"the family-wide assertions in this file would silently skip it", name)
		}
		delete(covered, name)
	}
	for name := range covered {
		t.Errorf("graphStrayCases drives `graph %s`, which the registry does not declare", name)
	}
}

// TestGraphPositional_AllFiveRefuseAStrayTokenWithOneWording is acceptance
// criterion 57 of SPEC/GRAPH.md.
//
// Two halves, and the suite needs both:
//
//   - the CONTROL, which runs each invocation without the stray token and
//     requires it to succeed, so the refusal below is caused by the stray token
//     and not by a query the subcommand's guard rail was going to reject anyway;
//   - the PROBE, which adds the stray token and requires the exact published
//     line, the parenthetical included, exit code 2 through utils.ErrInvalidInput,
//     and an empty stdout.
//
// The five probe lines are then compared AGAINST EACH OTHER. That comparison is
// the criterion's own instruction and the half a per-subcommand table cannot
// give: an edit that reworded one subcommand's refusal would satisfy every
// assertion made about that subcommand alone.
func TestGraphPositional_AllFiveRefuseAStrayTokenWithOneWording(t *testing.T) {
	roadmap := seedGraphStrayRoadmap(t, "graph-stray-wording")

	// The offending token is the same across the five, so the five lines differ
	// only where the WORDING differs and the cross-comparison below reads as
	// exactly that.
	const stray = "reconciliation-report"

	// Read once from the SPEC so this file carries no second copy of the line;
	// the two families' relationship is held by
	// positional_refusal_families_test.go, which this test shares the reader
	// with (see SPEC/GRAPH.md acceptance criterion 60).
	want := refusalLineWithToken(t, publishedGraphRefusalLine(t), stray)

	// subcommand -> the line it produced, so a drifting member can be named.
	produced := make(map[string]string, len(graphStrayCases()))

	for _, c := range graphStrayCases() {
		t.Run(c.subcommand, func(t *testing.T) {
			controlOut, controlErr := dispatchInvocation(t, graphFamilyName,
				c.subcommand, "-r", roadmap, "--query", c.query)
			if controlErr != nil {
				t.Fatalf("the control invocation `graph %s` (acting on %s) failed with %v; "+
					"the probe below would then be asserting nothing about the stray token",
					c.subcommand, c.seeded, controlErr)
			}
			if controlOut == "" {
				t.Fatalf("the control invocation wrote nothing to stdout; every graph subcommand "+
					"reports its result there, so %q did not run", c.query)
			}

			out, err := dispatchInvocation(t, graphFamilyName,
				c.subcommand, "-r", roadmap, "--query", c.query, stray)
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
			produced[c.subcommand] = errorLine(err)
		})
	}

	distinct := make(map[string][]string)
	for sub, line := range produced {
		distinct[line] = append(distinct[line], sub)
	}
	if len(distinct) > 1 {
		for line, subs := range distinct {
			t.Errorf("the family no longer has one wording: %v emit %q", subs, line)
		}
	}
}

// TestGraphPositional_HyphenPrefixedTokensAreClassifiedBothWays is acceptance
// criterion 58 of SPEC/GRAPH.md, in both of the directions the criterion names.
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
			why:            "a long flag no graph subcommand defines is an unknown flag, not a positional argument",
		},
		{
			token:          "-x",
			wantUnexpected: false,
			why:            "'-' followed by an ASCII letter is a short flag, and no graph subcommand defines this one",
		},
	}

	graphLine := publishedGraphRefusalLine(t)

	for _, c := range graphStrayCases() {
		for _, tc := range cases {
			t.Run(c.subcommand+"/"+tc.token, func(t *testing.T) {
				out, err := dispatchInvocation(t, graphFamilyName,
					c.subcommand, "-r", roadmap, "--query", c.query, tc.token)
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
// acceptance criterion 58: the tokens are examined left to right and the first
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
				args:  []string{c.subcommand, "-r", roadmap, "--query", c.query, first, second},
			},
			{
				label: "the first stray written before the flags",
				args:  []string{c.subcommand, first, "-r", roadmap, "--query", c.query, second},
			},
		}
		for _, layout := range layouts {
			t.Run(c.subcommand+"/"+layout.label, func(t *testing.T) {
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
