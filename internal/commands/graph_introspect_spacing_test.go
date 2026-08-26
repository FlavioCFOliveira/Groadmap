package commands

// CLI half of the regression for rmp task #275, and of SPEC/GRAPH.md Acceptance
// Criterion 39.
//
// The defect: `rmp graph query --query "SHOW  INDEXES"` (two spaces) exited 1
// with `database error: graph query failed: cypher: parse: unexpected "SHOW" at
// 1:0, expected one of {CALL, YIELD, ...}` — a list that names every clause
// keyword EXCEPT SHOW, so it reads as though schema introspection were
// unsupported. The identical statement with one space returned its result set.
// The guard rail admitted the statement because its introspection matcher
// tolerated arbitrary whitespace between the two keywords, while the engine
// routes to its introspection parser on literal prefixes carrying exactly one
// space. The two disagreed about the same input and the user got the wrong
// diagnostic.
//
// The contract these tests pin (SPEC/GRAPH.md § Keyword Spacing in a
// Schema-Introspection Command; § Per-Subcommand Validation Rules note 8):
//
//   - exactly one space is accepted and still executes, for all four target
//     keywords;
//   - any other separator is refused by the GUARD RAIL, with utils.ErrValidation
//     (exit code 6) and the guard rail's own message, before the engine sees the
//     query — never with the ErrDatabase (exit code 1) an engine parse failure
//     carries, which SPEC/GRAPH.md Acceptance Criterion 11 specifies and which
//     this change does not touch;
//   - the write subcommands are unaffected: they reject a SHOW on its operation
//     class, with their own message, at any spacing (note 6);
//   - the DDL class stays whitespace-TOLERANT. The asymmetry is deliberate and
//     must survive: the DDL pairing exists to refuse, so being wider than the
//     engine only refuses more, which is fail-closed; the introspection pairing
//     exists to admit, so being wider admits what the engine then refuses.

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// misspacedIntrospectSeparators are the separators that must be refused. One
// space is the only accepted spelling.
var misspacedIntrospectSeparators = []struct {
	name string
	sep  string
}{
	{"two spaces", "  "},
	{"a tab", "\t"},
	{"a line break", "\n"},
	{"four spaces", "    "},
	{"a space and a tab", " \t"},
}

// introspectTargetKeywords are the four keywords the schema-introspection class
// covers. Both singulars are exercised as well as both plurals, because the
// matcher spells the two plurals differently (INDEX(ES)? against CONSTRAINTS?)
// and a regression in either spelling would drop one form silently.
var introspectTargetKeywords = []string{"INDEXES", "INDEX", "CONSTRAINTS", "CONSTRAINT"}

// assertGuardRailSpacingRejection asserts err is the guard rail's own
// keyword-spacing refusal: ErrValidation (exit code 6), a message naming the
// spacing and the accepted spelling, and NOT the engine's parse diagnostic.
func assertGuardRailSpacingRejection(t *testing.T, err error, query, accepted string) {
	t.Helper()

	if err == nil {
		t.Fatalf("%q was accepted; the engine refuses this spelling, so the guard rail must refuse it first", query)
	}
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("%q: error = %v, want ErrValidation (exit code 6)", query, err)
	}
	// Exit code 1 belongs to an engine parse or execution failure. A statement
	// refused here never reaches the engine, so it must not carry that class.
	if errors.Is(err, utils.ErrDatabase) {
		t.Errorf("%q: error = %v, want the guard rail's ErrValidation, not the ErrDatabase (exit code 1) "+
			"of an engine failure — the statement must be refused before the engine sees it", query, err)
	}

	msg := err.Error()
	for _, want := range []string{"schema-introspection command", "exactly one space", "keyword spacing", accepted} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q: message = %q, want it to contain %q", query, msg, want)
		}
	}
	// The whole point of the fix: the user must not be shown the engine's
	// diagnostic, which names the wrong problem.
	for _, forbidden := range []string{"cypher: parse", `unexpected "SHOW"`, "database error", "not read-only"} {
		if strings.Contains(msg, forbidden) {
			t.Errorf("%q: message = %q, want it never to contain %q", query, msg, forbidden)
		}
	}
}

// TestGraphRead_IntrospectionKeywordSpacing is the core regression: for each of
// the four target keywords, `graph query` and `graph search` accept exactly one
// space and refuse every other separator, with the guard rail's message and exit
// code 6.
//
// It fails if cypherguard's reIntrospect is widened back to arbitrary
// whitespace: the refusal disappears, the query is admitted as an ordinary read,
// and the engine's parse diagnostic returns under ErrDatabase.
func TestGraphRead_IntrospectionKeywordSpacing(t *testing.T) {
	const roadmap = "graph-introspect-spacing"
	defer setupTestGraphRoadmap(t, roadmap)()

	// A real store, so the accepted spelling genuinely executes rather than
	// failing for the want of a graph.
	if err := runGraphCreate([]string{"-r", roadmap, "--query",
		"CREATE (:Spec {key:'graph-keyword-spacing'})"}); err != nil {
		t.Fatalf("seeding the graph store: %v", err)
	}

	for _, subcmd := range []struct {
		name string
		run  func([]string) error
	}{
		{"query", runGraphQuery},
		{"search", runGraphSearch},
	} {
		t.Run(subcmd.name, func(t *testing.T) {
			for _, keyword := range introspectTargetKeywords {
				t.Run(keyword, func(t *testing.T) {
					accepted := "SHOW " + keyword

					t.Run("one space is accepted and executes", func(t *testing.T) {
						stdout, _ := captureStdStreams(t, func() {
							if err := subcmd.run([]string{"-r", roadmap, "--query", accepted}); err != nil {
								t.Fatalf("%q was refused: %v", accepted, err)
							}
						})
						// It ran: the command printed the result set, so the
						// acceptance is not the vacuous kind that would also
						// hold if the query had never been executed.
						if !strings.Contains(stdout, `"columns"`) || !strings.Contains(stdout, `"rows"`) {
							t.Errorf("%q produced no result set on stdout; stdout=%q", accepted, stdout)
						}
					})

					for _, sep := range misspacedIntrospectSeparators {
						query := "SHOW" + sep.sep + keyword
						t.Run(sep.name+" is refused", func(t *testing.T) {
							var err error
							stdout, _ := captureStdStreams(t, func() {
								err = subcmd.run([]string{"-r", roadmap, "--query", query})
							})
							assertGuardRailSpacingRejection(t, err, query, accepted)
							// A refused query produces nothing on stdout: the
							// rejection precedes execution and the store open.
							if strings.TrimSpace(stdout) != "" {
								t.Errorf("%q wrote %q to stdout; a refused query must produce no output", query, stdout)
							}
						})
					}
				})
			}
		})
	}
}

// TestGraphRead_IntrospectionKeywordSpacingIgnoresCase asserts the refusal folds
// keyword case exactly as the engine's prefix test does, so a lowercase or
// mixed-case statement is refused for its spacing rather than admitted by a case
// accident — and that the message names the CANONICAL spelling whatever case the
// user typed.
func TestGraphRead_IntrospectionKeywordSpacingIgnoresCase(t *testing.T) {
	const roadmap = "graph-introspect-spacing-case"
	defer setupTestGraphRoadmap(t, roadmap)()

	cases := []struct {
		query    string
		accepted string
	}{
		{"show  indexes", "SHOW INDEXES"},
		{"Show\tIndex", "SHOW INDEX"},
		{"sHoW\ncOnStRaInTs", "SHOW CONSTRAINTS"},
		{"SHOW    constraint", "SHOW CONSTRAINT"},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			err := runGraphQuery([]string{"-r", roadmap, "--query", tc.query})
			assertGuardRailSpacingRejection(t, err, tc.query, tc.accepted)
		})
	}
}

// TestGraphGuardRail_IntrospectionSpacingLeavesWriteSubcommandsAlone asserts the
// spacing rule changed nothing for `graph create`, `graph update` and
// `graph delete`. Their objection to a SHOW statement is that it carries none of
// the data-writing clauses they accept, and that objection is decided first and
// holds at every spacing (SPEC/GRAPH.md § Per-Subcommand Validation Rules note 6;
// Acceptance Criterion 24).
//
// This matters because the fix would be easy to write in the shared code path:
// doing so would replace each write subcommand's own contract message with a
// spacing complaint that tells the user nothing about why `graph create` refused
// a SHOW.
func TestGraphGuardRail_IntrospectionSpacingLeavesWriteSubcommandsAlone(t *testing.T) {
	writeSubcommands := []struct {
		subcmd  string
		allowed string
		wantMsg string
	}{
		{"create", "CREATE/MERGE", "graph create accepts only CREATE/MERGE queries"},
		{"update", "SET/REMOVE", "graph update accepts only SET/REMOVE queries"},
		{"delete", "DELETE/DETACH DELETE", "graph delete accepts only DELETE/DETACH DELETE queries"},
	}

	for _, w := range writeSubcommands {
		t.Run(w.subcmd, func(t *testing.T) {
			for _, query := range []string{"SHOW INDEXES", "SHOW  INDEXES", "SHOW\tCONSTRAINT", "SHOW\nINDEX"} {
				t.Run(query, func(t *testing.T) {
					err := validateGuardRail(w.subcmd, w.allowed, query)
					if err == nil {
						t.Fatalf("graph %s accepted %q", w.subcmd, query)
					}
					if !errors.Is(err, utils.ErrValidation) {
						t.Errorf("graph %s on %q: error = %v, want ErrValidation (exit code 6)", w.subcmd, query, err)
					}
					if got := strings.TrimPrefix(err.Error(), "validation error: "); got != w.wantMsg {
						t.Errorf("graph %s on %q: message = %q, want %q — the class objection, not a spacing complaint",
							w.subcmd, query, got, w.wantMsg)
					}
				})
			}
		})
	}
}

// TestGraphGuardRail_DDLStaysWhitespaceTolerant pins the other half of the
// deliberate asymmetry SPEC/GRAPH.md § Keyword Spacing in a Schema-Introspection
// Command requires to survive.
//
// A reader who notices that introspection is matched exactly while DDL is matched
// with \s+ will be tempted to align the two. Narrowing the DDL matcher would
// reopen a guard-rail hole: the engine folds case and skips leading comments
// before its own DDL prefix test, but a Groadmap matcher that demanded one space
// would stop seeing `CREATE   INDEX`, and a schema-mutating statement would pass
// the check that exists to stop it. This test sits beside the spacing rule so
// that alignment fails here rather than silently succeeding.
func TestGraphGuardRail_DDLStaysWhitespaceTolerant(t *testing.T) {
	ddl := []string{
		"CREATE   INDEX spec_key_idx FOR (n:Spec) ON (n.key)",
		"CREATE\tINDEX spec_key_idx FOR (n:Spec) ON (n.key)",
		"DROP\n\tINDEX spec_key_idx",
		"create     constraint c1 ON (n:Spec) ASSERT n.key IS UNIQUE",
		"DROP  CONSTRAINT c1",
	}

	for _, query := range ddl {
		t.Run(query, func(t *testing.T) {
			err := validateGuardRail("query", "read-only", query)
			if err == nil {
				t.Fatalf("graph query accepted schema-mutating DDL %q: the DDL matcher must stay tolerant of "+
					"arbitrary whitespace between the two keywords, whatever the introspection matcher does", query)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("%q: error = %v, want ErrValidation (exit code 6)", query, err)
			}
			if got := err.Error(); !strings.Contains(got, "graph query accepts only read-only queries") {
				t.Errorf("%q: message = %q, want the read-only class objection", query, got)
			}
		})
	}
}

// TestGraphRead_NonIntrospectionShowStillReachesTheEngine asserts the refusal is
// confined to statements that ARE schema-introspection commands under some
// spacing. A near miss on the keyword and a SHOW family the engine does not
// implement must keep reaching the engine, which already names the real problem
// for them, so the guard rail must not start answering for those too
// (SPEC/GRAPH.md § Per-Subcommand Validation Rules note 3).
func TestGraphRead_NonIntrospectionShowStillReachesTheEngine(t *testing.T) {
	const roadmap = "graph-introspect-spacing-passthrough"
	defer setupTestGraphRoadmap(t, roadmap)()

	if err := runGraphCreate([]string{"-r", roadmap, "--query",
		"CREATE (:Spec {key:'graph-keyword-spacing-passthrough'})"}); err != nil {
		t.Fatalf("seeding the graph store: %v", err)
	}

	for _, query := range []string{"SHOW  INDEXER", "SHOW  DATABASES", "SHOW  CONSTRAINTX"} {
		t.Run(query, func(t *testing.T) {
			err := runGraphQuery([]string{"-r", roadmap, "--query", query})
			if err == nil {
				t.Fatalf("%q was accepted; the engine has no production for it", query)
			}
			if !errors.Is(err, utils.ErrDatabase) {
				t.Errorf("%q: error = %v, want the engine's ErrDatabase (exit code 1) — the guard rail must "+
					"not answer for a statement that is not a schema-introspection command", query, err)
			}
			if errors.Is(err, utils.ErrValidation) {
				t.Errorf("%q: error = %v, want no guard-rail rejection", query, err)
			}
		})
	}
}
