// Regression fence for the statement time budget on `rmp graph execute`
// (SPEC/GRAPH.md § Statement Time Budget; SPEC/COMMANDS.md § Graph Management;
// acceptance criteria 39 and 40).
//
// The defect this closes. `rmp graph execute` ran its statement under
// context.Background(), so a CLI hold on the graph store lock had no lawful
// maximum, and the bounded wait internal/graphlock derives — statement budget
// plus backoff total — rests on every holder having one. One pathological
// statement therefore starved every concurrent invocation, web and CLI alike,
// however the wait was sized. Measured before the fix: a three-way Cartesian
// product over a real 44,906-node knowledge graph had not finished after 300
// seconds.
//
// What is asserted here, and what is asserted elsewhere:
//
//   - that the deadline exists, fires, and is the SHARED declaration rather
//     than a literal of this package's own — here;
//   - that a cut statement leaves the store exactly as it found it — here,
//     from disk, by reopening rather than by trusting the error;
//   - that the message the user reads equals the line SPEC/COMMANDS.md
//     § Graph Management publishes, character for character, at the production
//     budget — here at the unit level, and end to end against the built binary
//     by tests/test_55_error_string_parity.py, which reads the expected text out
//     of SPEC/COMMANDS.md itself rather than restating it.
//
// The web half of the same budget is fenced by internal/web/graph_budget_test.go.
// The two surfaces read one declaration, so a change to it moves both.
package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// setGraphStatementBudget installs a statement time budget for the duration of
// one test and restores the previous value when the test ends. It is the
// internal/commands twin of internal/web's setGraphQueryBudget, which is
// package-private to that package and cannot be shared across the import edge.
//
// It is the ONLY way the budget is ever reassigned: production initialises
// graphlock.StatementBudget from graphlock.DefaultStatementBudget and never
// writes to it again, and no flag, environment variable, or any other
// user-facing knob reaches it (SPEC/WEB.md § Graph Query Time Budget, rules 1
// and 8; the fourth option considered and declined on rmp task #377 was exactly
// a `--timeout` flag). The override exists so the tests below can prove the
// cancellation in milliseconds instead of spending five real seconds per run.
//
// **It moves the lock's wait budget with it, and that is the point rather than
// a side effect.** The quantity is graph-store-wide: it bounds the variable part
// of a lock hold as well as this surface's own execution, and
// graphlock.WaitBudget is derived from it (SPEC/GRAPH.md § Lock Contention). So
// a test that grants itself a long statement thereby grants a contending waiter
// long enough to outlast it, and a test that shortens the statement shortens the
// wait it has to spend. Neither could be arranged by moving a CLI-local figure,
// which is the second reason there is no CLI-local figure to move.
//
// Restoring through t.Cleanup makes nested calls safe: cleanups unwind LIFO, so
// a test that lowers the budget and later raises it back still leaves
// graphlock.DefaultStatementBudget installed for the next test. No test in this
// package calls t.Parallel, so no test observes another test's override.
func setGraphStatementBudget(t *testing.T, d time.Duration) {
	t.Helper()
	previous := graphlock.StatementBudget
	t.Cleanup(func() { graphlock.StatementBudget = previous })
	graphlock.StatementBudget = d
}

// publishedBudgetLine is the line SPEC/COMMANDS.md § Graph Management publishes
// for a statement the budget cuts, minus the "Error: " prefix the top-level
// error printer adds. Every character of it is rmp's own text: unlike the
// parse/execution and store-failure rows beside it, it carries no engine
// diagnostic and no placeholder, so it is compared in full.
//
// The only variable in it is the budget itself, which the code renders from the
// value that produced the deadline rather than from a literal, so a test that
// moves the budget gets a truthful message. At the production budget it renders
// "5s" and this string is the published line exactly.
//
// The em dashes are U+2014, as published.
const publishedBudgetLine = "database error: graph query exceeded the %s statement time budget; " +
	"nothing was written. Narrow the statement — add a label, an indexed property filter, " +
	"or a LIMIT — or split it into smaller statements."

// wantBudgetLine renders the published line for a given budget.
func wantBudgetLine(budget time.Duration) string {
	return fmt.Sprintf(publishedBudgetLine, budget)
}

// budgetCartesianRead is an aggregate over a three-way Cartesian product. It
// returns a single row, so nothing about the RESULT is large, while the engine
// must stream N^3 intermediate tuples to produce that row: the budget is the
// only thing that bounds that work. It is the shape SPEC/GRAPH.md
// § Statement Time Budget names as one of the two the budget cuts.
const budgetCartesianRead = "MATCH (a),(b),(c) RETURN count(*)"

// budgetCartesianWrite is the same product with a writing clause, so the cut
// lands on a statement that had been accumulating a write set. It is the shape
// acceptance criterion 39 requires the "nothing survived" assertion of.
const budgetCartesianWrite = "MATCH (a),(b),(c) CREATE (:BudgetCutProbe {k:a.i})"

// budgetProbeLabel is the label budgetCartesianWrite creates, counted afterwards
// in a separate invocation.
const budgetProbeLabel = "BudgetCutProbe"

// budgetSeedNodes is how many nodes the Cartesian product runs over. It is sized
// grossly over the budget rather than marginally, and that is the safe direction
// here rather than the expensive one: the cost of these tests is capped by the
// budget, not by the query, so a larger store buys robustness against a faster
// machine and costs no wall time. 600 nodes is 216 million tuples — measured
// elsewhere at 1.45 s for 252 nodes and 6.04 s for 400, so tens of seconds here —
// against budgets of a few hundred milliseconds.
const budgetSeedNodes = 600

// seedBudgetGraph creates the roadmap under a temporary HOME and fills its graph
// store with budgetSeedNodes nodes in one UNWIND, which costs a few
// milliseconds. It returns the roadmap name.
func seedBudgetGraph(t *testing.T, name string) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(setupTestGraphRoadmap(t, name))

	seed := fmt.Sprintf("UNWIND range(1,%d) AS i CREATE (:Bulk {i:i})", budgetSeedNodes)
	var err error
	_, _ = captureStdStreams(t, func() {
		err = runGraphExecute([]string{"-r", name, "--query", seed})
	})
	if err != nil {
		t.Fatalf("seeding %d nodes: %v", budgetSeedNodes, err)
	}
	return name
}

// countNodes runs a counting statement in a SEPARATE invocation — a fresh store
// open, reading what is actually on disk — and returns the count.
func countNodes(t *testing.T, roadmap, label string) int64 {
	t.Helper()
	var out string
	var err error
	out, _ = captureStdStreams(t, func() {
		err = runGraphExecute([]string{"-r", roadmap, "--query",
			fmt.Sprintf("MATCH (n:%s) RETURN count(n)", label)})
	})
	if err != nil {
		t.Fatalf("counting :%s nodes: %v", label, err)
	}
	var parsed struct {
		Rows [][]int64 `json:"rows"`
	}
	if jerr := json.Unmarshal([]byte(out), &parsed); jerr != nil {
		t.Fatalf("decoding the count of :%s nodes from %q: %v", label, out, jerr)
	}
	if len(parsed.Rows) != 1 || len(parsed.Rows[0]) != 1 {
		t.Fatalf("counting :%s nodes returned %q, want one row of one column", label, out)
	}
	return parsed.Rows[0][0]
}

// storeFingerprint hashes every file under the roadmap's graph store directory,
// so `wal` and every file under `snapshot/` are compared byte for byte rather
// than by size or timestamp.
func storeFingerprint(t *testing.T, roadmap string) string {
	t.Helper()
	dir := testGraphDir(t, roadmap)
	var lines []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path) //nolint:gosec // a path this test itself created under a temporary HOME
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(data)
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		lines = append(lines, rel+" "+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		t.Fatalf("fingerprinting the graph store at %s: %v", dir, err)
	}
	if len(lines) == 0 {
		t.Fatalf("fingerprinting the graph store at %s found no files: the comparison would be vacuous", dir)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestGraphExecute_StatementBudgetCutsAnExpensiveStatement is the regression for
// the defect itself: a statement whose work exceeds the budget is cancelled
// rather than run to completion, and it fails with the published line.
//
// It asserts the four things SPEC/GRAPH.md § Statement Time Budget states of a
// cut invocation, and the elapsed-time floor is not decoration: without it, an
// implementation that refused the statement instantly for some unrelated reason
// would satisfy an upper bound on its own.
func TestGraphExecute_StatementBudgetCutsAnExpensiveStatement(t *testing.T) {
	name := seedBudgetGraph(t, "graph-budget-cuts-read")

	const budget = 300 * time.Millisecond
	setGraphStatementBudget(t, budget)

	var err error
	started := time.Now()
	stdout, stderr := captureStdStreams(t, func() {
		err = runGraphExecute([]string{"-r", name, "--query", budgetCartesianRead})
	})
	elapsed := time.Since(started)

	// (i) It failed, in the class the specification fixes: utils.ErrDatabase,
	// which is exit code 1. No new sentinel and no new exit code
	// (SPEC/GRAPH.md § Constraints, rule 5).
	if err == nil {
		t.Fatalf("the statement completed in %v under a %v budget over %d nodes: the budget bounded nothing", elapsed, budget, budgetSeedNodes)
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("err = %v, want it to wrap utils.ErrDatabase (exit code 1)", err)
	}

	// (ii) The message is the published line, with the budget rendered from the
	// value that produced the deadline.
	if got, want := err.Error(), wantBudgetLine(budget); got != want {
		t.Errorf("message mismatch (SPEC/COMMANDS.md § Graph Management)\n got:  %q\n want: %q", got, want)
	}

	// (iii) It was cut at the budget rather than run to completion, and it did
	// not fail before the deadline could fire. The ceiling is generous so a
	// loaded or race-instrumented machine cannot flake it, and still far below
	// the tens of seconds the statement costs unbounded.
	if elapsed < budget {
		t.Errorf("the invocation returned after %v, before its %v budget could elapse: the failure was not the budget", elapsed, budget)
	}
	if elapsed > 20*time.Second {
		t.Errorf("the invocation took %v under a %v budget: the deadline was not honoured promptly", elapsed, budget)
	}
	t.Logf("a %d-node three-way Cartesian product was cut after %v under a %v budget", budgetSeedNodes, elapsed, budget)

	// (iv) Nothing was printed as a success. A cut statement produces no result.
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing: a cut statement produces no result", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want nothing from the handler itself: the message is carried by the returned error, which the top-level printer renders", stderr)
	}
}

// TestGraphExecute_BudgetCutWritesNothing covers acceptance criterion 39's
// second half: where the cut statement was a WRITE, nothing it was creating
// survives, and nothing on disk was rewritten.
//
// It is asserted from disk in a separate invocation rather than inferred from
// the error, because the error is exactly what a partially-applied write would
// also report. Two independent properties are checked: the graph holds none of
// the elements the statement was creating, and every file under the store
// directory — `wal` and everything under `snapshot/` — is byte for byte what it
// was, which is what proves the transaction rolled back whole AND that no
// checkpoint ran on the cut path.
func TestGraphExecute_BudgetCutWritesNothing(t *testing.T) {
	name := seedBudgetGraph(t, "graph-budget-cut-writes-nothing")

	// Ground truth first, so the comparisons below cannot pass by both sides
	// being empty.
	if seeded := countNodes(t, name, "Bulk"); seeded != budgetSeedNodes {
		t.Fatalf("the seed produced %d :Bulk nodes, want %d", seeded, budgetSeedNodes)
	}
	before := storeFingerprint(t, name)

	const budget = 300 * time.Millisecond
	setGraphStatementBudget(t, budget)

	var err error
	stdout, _ := captureStdStreams(t, func() {
		err = runGraphExecute([]string{"-r", name, "--query", budgetCartesianWrite})
	})
	if err == nil {
		t.Fatalf("the writing statement completed under a %v budget over %d nodes; stdout=%q", budget, budgetSeedNodes, stdout)
	}
	if got, want := err.Error(), wantBudgetLine(budget); got != want {
		t.Fatalf("message mismatch\n got:  %q\n want: %q", got, want)
	}
	// Restore the production budget before the assertions read the store back,
	// so the reads below are not themselves racing a deadline.
	setGraphStatementBudget(t, graphlock.DefaultStatementBudget)

	// (i) Nothing the statement was creating survived, read back through a fresh
	// store open.
	if survivors := countNodes(t, name, budgetProbeLabel); survivors != 0 {
		t.Errorf("%d :%s nodes survived a cut write, want 0: the transaction did not roll back whole", survivors, budgetProbeLabel)
	}
	// And the graph is otherwise exactly what it was.
	if seeded := countNodes(t, name, "Bulk"); seeded != budgetSeedNodes {
		t.Errorf(":Bulk nodes = %d after a cut write, want the %d seeded", seeded, budgetSeedNodes)
	}

	// (ii) Nothing on disk was rewritten: no partial write landed in the
	// write-ahead log, and no checkpoint ran to rewrite the snapshot
	// (SPEC/GRAPH.md § Statement Time Budget, rules 2 and 3).
	//
	// The fingerprint is taken before the reads above would have had a chance to
	// change anything, and compared after them: a read commits nothing, so the
	// store must be identical either way.
	if after := storeFingerprint(t, name); after != before {
		t.Errorf("the graph store changed across a cut write:\n before:\n%s\n after:\n%s", before, after)
	}
}

// TestGraphExecute_BudgetIsTheSharedDeclaration fences the property acceptance
// criterion 40 states: the deadline this surface applies is
// graphlock.StatementBudget itself, not a literal of internal/commands' own that
// merely happens to equal it today.
//
// It proves that behaviourally, and in the only way that cannot be satisfied by
// a coincidence: the shared declaration is moved to two different values, and
// BOTH the moment the statement is cut and the duration the message names must
// follow it. An implementation carrying its own constant would keep cutting at
// five seconds and would keep printing "5s" whatever this test set; one that
// read the declaration for the deadline but printed a literal would be caught by
// the message; one that printed the declaration but hardcoded the deadline would
// be caught by the clock.
//
// That the value is 5 seconds in production, and that production never
// reassigns it, is fenced once, in internal/web's
// TestGraphQueryBudget_ProductionDefault, against the same declaration. It is
// not restated here; what is asserted here is that this surface READS it.
func TestGraphExecute_BudgetIsTheSharedDeclaration(t *testing.T) {
	name := seedBudgetGraph(t, "graph-budget-shared-declaration")

	for _, budget := range []time.Duration{200 * time.Millisecond, 700 * time.Millisecond} {
		t.Run(budget.String(), func(t *testing.T) {
			setGraphStatementBudget(t, budget)

			var err error
			started := time.Now()
			_, _ = captureStdStreams(t, func() {
				err = runGraphExecute([]string{"-r", name, "--query", budgetCartesianRead})
			})
			elapsed := time.Since(started)

			if err == nil {
				t.Fatalf("the statement completed in %v under a %v budget: the moved declaration did not move the deadline", elapsed, budget)
			}
			if got, want := err.Error(), wantBudgetLine(budget); got != want {
				t.Errorf("the message does not name the budget in force\n got:  %q\n want: %q", got, want)
			}
			if elapsed < budget {
				t.Errorf("cut after %v under a %v budget: the deadline did not follow the declaration", elapsed, budget)
			}
			t.Logf("budget %v: cut after %v", budget, elapsed)
		})
	}
}

// TestGraphStatementError_Classification drives the classifier directly over the
// arrival points a statement's failure can reach it through, and is the
// unit-level companion to the behavioural tests above.
//
// Two things only this test can assert cheaply:
//
//   - that the message at the PRODUCTION budget is the published line character
//     for character, without spending five real seconds to provoke it;
//   - that all three arrival points classify identically. The engine call, the
//     walk over the result, and the commit each reach this function, and the
//     walk is the one that matters: the engine streams a disconnected pattern's
//     tuples as the result is iterated, so RunAny returns a nil error long
//     before the deadline fires and an implementation that classified only the
//     call's error would report a cut statement as an ordinary query failure
//     (SPEC/GRAPH.md § Statement Time Budget, rule 5).
func TestGraphStatementError_Classification(t *testing.T) {
	// What the engine returns: it wraps ctx.Err() (cypher.checkContext), so the
	// wrapped sentinel is what must be matched, not an equality test.
	wrappedDeadline := fmt.Errorf("cypher: %w", context.DeadlineExceeded)
	engineFailure := errors.New("cypher: parse error at offset 12")

	t.Run("the production budget renders the published line", func(t *testing.T) {
		err := graphStatementError(graphlock.DefaultStatementBudget, "graph query failed", wrappedDeadline)
		want := "database error: graph query exceeded the 5s statement time budget; nothing was " +
			"written. Narrow the statement — add a label, an indexed property filter, or a LIMIT " +
			"— or split it into smaller statements."
		if err.Error() != want {
			t.Errorf("at the production budget\n got:  %q\n want: %q", err.Error(), want)
		}
		// The same string, built the way every other assertion in this file
		// builds it, so the two cannot drift apart.
		if wantBudgetLine(graphlock.DefaultStatementBudget) != want {
			t.Errorf("wantBudgetLine renders %q at the production budget, want %q", wantBudgetLine(graphlock.DefaultStatementBudget), want)
		}
	})

	// Each stage carries a different ordinary-failure wording, and the budget
	// message must override all of them identically: what the user needs to know
	// is that the budget was exceeded, not which of the three points reported it.
	for _, stage := range []string{"graph query failed", "graph commit failed"} {
		t.Run("a cut at "+stage, func(t *testing.T) {
			err := graphStatementError(150*time.Millisecond, stage, wrappedDeadline)
			if !errors.Is(err, utils.ErrDatabase) {
				t.Errorf("err = %v, want it to wrap utils.ErrDatabase", err)
			}
			if got, want := err.Error(), wantBudgetLine(150*time.Millisecond); got != want {
				t.Errorf("\n got:  %q\n want: %q", got, want)
			}
			if strings.Contains(err.Error(), stage) {
				t.Errorf("err = %q: a budget cut must not be reported as %q, which says nothing about the budget", err.Error(), stage)
			}
		})

		t.Run("an ordinary failure at "+stage+" keeps its message", func(t *testing.T) {
			err := graphStatementError(150*time.Millisecond, stage, engineFailure)
			if !errors.Is(err, utils.ErrDatabase) {
				t.Errorf("err = %v, want it to wrap utils.ErrDatabase", err)
			}
			want := "database error: " + stage + ": cypher: parse error at offset 12"
			if err.Error() != want {
				t.Errorf("\n got:  %q\n want: %q", err.Error(), want)
			}
			if strings.Contains(err.Error(), "budget") {
				t.Errorf("err = %q: an ordinary engine failure must not be blamed on the budget", err.Error())
			}
		})
	}
}

// TestGraphExecute_OrdinaryStatementUnaffectedByTheBudget is the other half of
// acceptance criterion 40: a statement that completes inside the budget returns
// exactly what its own Cypher produces, with nothing truncated, no ordering
// changed and no latency added. The budget is observable only to a statement
// that would otherwise have run for longer than it.
//
// It is proved differentially rather than by restating the expected payload: the
// same statements are run under the production budget and under a budget so
// large it can never fire, and the two outputs must be byte-identical. Anything
// the deadline truncated, reordered or dropped would separate them.
func TestGraphExecute_OrdinaryStatementUnaffectedByTheBudget(t *testing.T) {
	name := seedBudgetGraph(t, "graph-budget-ordinary-unaffected")

	for _, query := range []string{
		"MATCH (n:Bulk) RETURN count(n)",
		"MATCH (n:Bulk) WHERE n.i < 5 RETURN n.i ORDER BY n.i",
		"CREATE (:Component {key:'ingest-pipeline'}) RETURN 1",
	} {
		t.Run(query, func(t *testing.T) {
			var err error
			budgeted, _ := captureStdStreams(t, func() {
				err = runGraphExecute([]string{"-r", name, "--query", query})
			})
			if err != nil {
				t.Fatalf("under the production budget: %v", err)
			}
			if strings.TrimSpace(budgeted) == "" {
				t.Fatalf("the statement wrote nothing to stdout; the comparison below would be vacuous")
			}

			// A budget that cannot possibly fire stands in for "before the budget
			// existed": the deadline is still derived and still installed, it just
			// never elapses.
			setGraphStatementBudget(t, time.Hour)
			unbounded, _ := captureStdStreams(t, func() {
				err = runGraphExecute([]string{"-r", name, "--query", query})
			})
			setGraphStatementBudget(t, graphlock.DefaultStatementBudget)
			if err != nil {
				t.Fatalf("effectively unbounded: %v", err)
			}
			if budgeted != unbounded {
				t.Errorf("the budget changed the output:\n with budget: %s\n unbounded:   %s", budgeted, unbounded)
			}
		})
	}
}
