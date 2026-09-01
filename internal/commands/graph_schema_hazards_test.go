package commands

// Tests for the two schema-statement hazards published as SPEC/GRAPH.md § What
// Groadmap Does Not Check, items 6 and 7. They are the behaviour that remains
// once Groadmap stopped inspecting the statements it is handed: it neither
// rewrites a schema statement's keyword spacing nor refuses one that carries a
// further clause after it, so both reach the engine and the engine decides.
//
// Item 7's outcome is a MISLEADING DIAGNOSTIC and item 6's is a SILENT PARTIAL
// EXECUTION. Neither is a Groadmap refusal any more, and each is asserted in
// both directions -- the statement that fails and the statement that succeeds --
// so that a case cannot pass by refusing everything or by accepting everything.
//
// Acceptance criterion 37 fixes the exit code for the spacing case (1, not 6),
// and criterion 38's fifth bullet fixes the outcome for the trailing clause.

import (
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestGraphSchema_IntrospectionKeywordSpacingReachesTheEngine is item 7 for the
// two schema-introspection commands.
//
// The engine routes a statement to its schema-introspection parser by testing it
// against the literal prefixes `SHOW CONSTRAINT` and `SHOW INDEX`, each carrying
// exactly one space. A statement that misses those prefixes by its spacing goes
// to the general Cypher grammar, which has no SHOW production. The refusal is
// therefore the ENGINE's, with the exit code an engine failure carries, and its
// diagnostic points nowhere near the spacing.
func TestGraphSchema_IntrospectionKeywordSpacingReachesTheEngine(t *testing.T) {
	const roadmap = "graph-schema-spacing-show"
	defer setupTestGraphRoadmap(t, roadmap)()

	for _, accepted := range []string{"SHOW INDEXES", "SHOW CONSTRAINTS"} {
		t.Run(accepted, func(t *testing.T) {
			// The single-space spelling answers. Without this half the case below
			// would pass against an engine that had lost schema introspection
			// altogether.
			_, _ = captureStdStreams(t, func() {
				if err := runGraphExecute([]string{"-r", roadmap, "--query", accepted}); err != nil {
					t.Errorf("%q must be answered by the engine's introspection parser: %v", accepted, err)
				}
			})

			keyword, target, _ := strings.Cut(accepted, " ")
			for _, sep := range []string{"  ", "\t", "\n", " \t "} {
				query := keyword + sep + target
				err := runGraphExecute([]string{"-r", roadmap, "--query", query})
				if err == nil {
					t.Errorf("%q was answered; the engine routes only the single-space spelling to "+
						"its introspection parser (SPEC/GRAPH.md § What Groadmap Does Not Check, "+
						"item 7). If it now tolerates the spacing, that item is stale", query)
					continue
				}
				// The exit code is the whole point: 1, the engine's own failure
				// class, and NOT the 6 the withdrawn guard rail used to carry.
				if !errors.Is(err, utils.ErrDatabase) {
					t.Errorf("%q must fail as an engine error (ErrDatabase, exit 1), got %v", query, err)
				}
				if errors.Is(err, utils.ErrValidation) {
					t.Errorf("%q was refused by Groadmap; the spacing is the engine's business now: %v", query, err)
				}
			}
		})
	}
}

// TestGraphSchema_DDLKeywordSpacingReachesTheEngine is item 7 for the
// schema-MUTATING forms, and acceptance criterion 37's last clause: the badly
// spaced statement fails with exit code 1 and creates nothing, while the
// well-spaced one creates the index.
func TestGraphSchema_DDLKeywordSpacingReachesTheEngine(t *testing.T) {
	const roadmap = "graph-schema-spacing-ddl"
	defer setupTestGraphRoadmap(t, roadmap)()

	const misspaced = "CREATE   INDEX spec_key FOR (n:Spec) ON (n.key)"
	err := runGraphExecute([]string{"-r", roadmap, "--query", misspaced})
	if err == nil {
		t.Fatalf("%q was accepted; the engine routes only the single-space spelling to its "+
			"schema parser (SPEC/GRAPH.md § What Groadmap Does Not Check, item 7)", misspaced)
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("%q must fail as an engine error (ErrDatabase, exit 1), got %v", misspaced, err)
	}
	if names := readSchemaNames(t, runGraphExecute, roadmap, "SHOW INDEXES"); containsName(names, "spec_key") {
		t.Fatalf("the refused statement created the index anyway: %v", names)
	}

	// The identical statement with one space is created, which is what makes the
	// assertion above about the SPACING rather than about the statement.
	_, _ = captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"CREATE INDEX spec_key FOR (n:Spec) ON (n.key)"}); err != nil {
			t.Fatalf("the single-space spelling must create the index: %v", err)
		}
	})
	if names := readSchemaNames(t, runGraphExecute, roadmap, "SHOW INDEXES"); !containsName(names, "spec_key") {
		t.Fatalf("the single-space spelling did not create the index: %v", names)
	}
}

// TestGraphSchema_TrailingClauseAfterASchemaStatementExecutesInPart is item 6
// and acceptance criterion 38's fifth bullet.
//
// The engine's schema parser stops as soon as its grammar is satisfied and
// discards the rest of the statement without an error, without a notification,
// and without any other trace. The command therefore reports success for a
// statement half of which never ran.
//
// The assertion is on both halves: the index exists (so the statement did run),
// and the property the discarded clause would have written is absent (so the
// rest did not). An exit-code-only check would pass against an engine that ran
// the whole statement, which is not what this specification says happens.
func TestGraphSchema_TrailingClauseAfterASchemaStatementExecutesInPart(t *testing.T) {
	const roadmap = "graph-schema-trailing-clause"
	defer setupTestGraphRoadmap(t, roadmap)()

	if err := runGraphExecute([]string{"-r", roadmap, "--query",
		"CREATE (n:Spec {key:'user-authentication'})"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"CREATE INDEX spec_key FOR (n:Spec) ON (n.key) MATCH (m:Spec) SET m.reviewed = true"}); err != nil {
			t.Fatalf("the statement must execute; Groadmap no longer refuses a schema statement "+
				"that carries a further clause (SPEC/GRAPH.md § What Groadmap Does Not Check, "+
				"item 6): %v", err)
		}
	})
	if !strings.Contains(stdout, `"ok"`) {
		t.Errorf("the statement must report the no-RETURN success shape, got %q", stdout)
	}

	// The schema half ran.
	if names := readSchemaNames(t, runGraphExecute, roadmap, "SHOW INDEXES"); !containsName(names, "spec_key") {
		t.Fatalf("the schema half of the statement did not run: %v", names)
	}

	// The clause after it did not, and nothing said so.
	rows := graphQueryRows(t, roadmap,
		"MATCH (n:Spec {key:'user-authentication'}) RETURN n.reviewed")
	if len(rows) != 1 {
		t.Fatalf("expected exactly one Spec node, got %v", rows)
	}
	if rows[0][0] != nil {
		t.Fatalf("n.reviewed = %v; the specified outcome is that the trailing MATCH ... SET is "+
			"DISCARDED by the engine's schema parser. If the engine now runs it, item 6 of "+
			"SPEC/GRAPH.md § What Groadmap Does Not Check is stale", rows[0][0])
	}
}
