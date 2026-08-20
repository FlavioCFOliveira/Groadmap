package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// setGraphQueryBudget installs a per-request query time budget for the duration
// of one test and restores the previous value when the test ends.
//
// It is the ONLY way the budget is ever reassigned: production initialises
// graphQueryBudget from defaultGraphQueryBudget and never writes to it again,
// and no flag, environment variable, or URL parameter reaches it (SPEC/WEB.md
// § Graph Query Time Budget, rules 1 and 8). The override exists so the
// regression tests below can prove the cancellation in milliseconds instead of
// spending five real seconds per run; it does not weaken the production value,
// which TestGraphQueryBudget_ProductionDefault fences.
//
// Restoring through t.Cleanup makes nested calls safe: cleanups unwind LIFO, so
// a test that lowers the budget and later raises it back still leaves
// defaultGraphQueryBudget installed for the next test. The package runs no test
// with t.Parallel, so no test observes another test's override.
func setGraphQueryBudget(t *testing.T, d time.Duration) {
	t.Helper()
	previous := graphQueryBudget
	t.Cleanup(func() { graphQueryBudget = previous })
	graphQueryBudget = d
}

// expensiveGraphQuery is an aggregate over a three-way Cartesian product: it
// returns a single row, so the injected node LIMIT bounds it not at all, while
// the engine must stream N^3 intermediate tuples to produce that row. It is the
// query SPEC/WEB.md § Acceptance Criteria, criterion 110 names to prove the
// budget bounds WORK where the limit bounds only the RESULT.
const expensiveGraphQuery = "MATCH (a),(b),(c) RETURN count(*)"

// expensiveGraphNodes is the node count the expensive query runs against. It is
// chosen from measurement, not from taste: on the development machine the
// three-way product costs 0.10 s at 100 nodes, 1.45 s at 252 (the store size
// SPEC/WEB.md § Graph Query Time Budget quotes) and 6.04 s at 400. At 400 the
// query therefore overruns even the 5-second production budget, so a run that
// completes is unambiguous evidence that nothing bounded the work.
const expensiveGraphNodes = 400

// seedExpensiveGraph seeds the roadmap's store with the small semantic graph
// every other graph test uses plus enough filler nodes to make the Cartesian
// product expensive, and returns the total node count. The bulk nodes are
// created by a single UNWIND so seeding costs a few milliseconds.
func seedExpensiveGraph(t *testing.T, name string) int {
	t.Helper()
	seeds := graphSeedQueries()
	bulk := expensiveGraphNodes - 3 // graphSeedQueries creates three nodes.
	seeds = append(seeds, fmt.Sprintf(`UNWIND range(1,%d) AS i CREATE (:Bulk {i:i})`, bulk))
	seedGraph(t, name, seeds...)
	return expensiveGraphNodes
}

// decodeQueryError decodes a classified query-bar error body and returns its
// kind and reason.
func decodeQueryError(t *testing.T, body []byte) (kind, reason string) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding error body: %v; body=%q", err, string(body))
	}
	k, _ := decoded["kind"].(string)
	r, _ := decoded["error"].(string)
	return k, r
}

// TestGraphQueryBudget_ProductionDefault fences the production value the whole
// bound rests on. The regression tests below lower the budget to prove the
// cancellation quickly, so the value they lower it FROM must be asserted
// somewhere or an accidental edit to it would go unnoticed: a budget silently
// reduced to milliseconds would cancel legitimate queries, and one silently
// raised past the 30-second WriteTimeout would let the connection time out
// before the failure could be written (SPEC/WEB.md § Graph Query Time Budget,
// rule 1; § HTTP Server Timeouts).
func TestGraphQueryBudget_ProductionDefault(t *testing.T) {
	if defaultGraphQueryBudget != 5*time.Second {
		t.Errorf("defaultGraphQueryBudget = %v, want 5s (SPEC/WEB.md § Graph Query Time Budget, rule 1)", defaultGraphQueryBudget)
	}
	if graphQueryBudget != defaultGraphQueryBudget {
		t.Errorf("graphQueryBudget = %v, want the production default %v: production must never reassign it", graphQueryBudget, defaultGraphQueryBudget)
	}
	if defaultGraphQueryBudget >= 30*time.Second {
		t.Errorf("defaultGraphQueryBudget = %v, must stay well below the 30s WriteTimeout so the failure response can still be written", defaultGraphQueryBudget)
	}
}

// TestHandleGraphData_BudgetIsNotCallerControlled asserts the budget is the
// server's alone: no URL parameter shortens or lengthens it, so a caller cannot
// turn the bound off (SPEC/WEB.md § Graph Query Time Budget, rule 8 — the
// budget introduces no new knob, and the endpoint's only parameters remain q
// and limit).
func TestHandleGraphData_BudgetIsNotCallerControlled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	// Parameters that would disable or shorten a budget if any of them were
	// wired to it. All must be ignored: the request is served normally.
	params := url.Values{
		"budget":   {"1ns"},
		"timeout":  {"1ns"},
		"deadline": {"0"},
	}
	rec := doGraphData(t, name, params)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: unknown parameters must not affect the request; body=%q", rec.Code, rec.Body.String())
	}
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(view.Nodes) != 3 || len(view.Edges) != 2 {
		t.Errorf("nodes/edges = %d/%d, want 3/2: a caller-supplied budget parameter must change nothing", len(view.Nodes), len(view.Edges))
	}
}

// TestHandleGraphData_ExpensiveQueryHitsTimeBudget is the regression for the
// defect the budget exists to close: the graph data endpoint ran a
// caller-supplied Cypher query with no time budget of its own, so a client that
// stayed connected held one server goroutine for as long as its query took to
// run. The injected node LIMIT was no defence — it bounds the RESULT, and an
// aggregate over a Cartesian product returns a single 33-byte row after scanning
// the whole product, at a cost cubic in the size of the store.
//
// The test proves the three properties SPEC/WEB.md § Acceptance Criteria,
// criterion 110 requires: the request comes back inside the budget instead of
// running to completion, it is answered as a query EXECUTION failure (the same
// classification an engine failure gets, not a new one), and the server keeps
// serving afterwards.
func TestHandleGraphData_ExpensiveQueryHitsTimeBudget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	nodes := seedExpensiveGraph(t, name)

	// Unbounded, this query costs about six seconds against this store — more
	// than the production budget itself. Lowering the budget keeps the test fast
	// without changing what it proves: the endpoint stops the work when its own
	// deadline elapses, whatever that deadline is.
	const budget = 150 * time.Millisecond
	setGraphQueryBudget(t, budget)

	started := time.Now()
	rec := doGraphData(t, name, url.Values{"q": {expensiveGraphQuery}, "limit": {"3000"}})
	elapsed := time.Since(started)

	// (i) It returned within the budget rather than running to completion. The
	// ceiling is generous (13x the budget) so a loaded or race-instrumented
	// machine cannot flake it, and still an order of magnitude below the ~6s the
	// unbounded query costs against this store.
	if elapsed > 2*time.Second {
		t.Errorf("request took %v with a %v budget over %d nodes: the query ran to completion; the budget did not bound the work", elapsed, budget, nodes)
	}
	t.Logf("expensive query over %d nodes returned in %v under a %v budget (unbounded cost: ~6s)", nodes, elapsed, budget)

	// (ii) It is classified as a query execution failure — case 3 of
	// § Query-Bar Error Handling — with no new status and no new kind
	// (SPEC/WEB.md § Graph Query Time Budget, rules 4 and 5).
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (the existing classified-failure status); body=%q", rec.Code, rec.Body.String())
	}
	kind, reason := decodeQueryError(t, rec.Body.Bytes())
	if kind != graphErrExecution {
		t.Errorf("kind = %q, want %q: exhausting the budget is an execution failure, not a guard-rail rejection or an invalid limit", kind, graphErrExecution)
	}
	// The page renders this string verbatim (static/graph.js showQueryError), so
	// it must read as the "query failed to execute" message the user already
	// knows, and it must name the budget rather than blame the client.
	if !strings.HasPrefix(reason, "query failed to execute: ") {
		t.Errorf("reason = %q, want the existing \"query failed to execute: \" message the page already shows", reason)
	}
	if !strings.Contains(reason, "query time budget") {
		t.Errorf("reason = %q, want it to name the query time budget as the cause", reason)
	}
	if strings.Contains(reason, "cancelled") {
		t.Errorf("reason = %q: the client never disconnected, so the failure must not be reported as a cancellation", reason)
	}

	// (iii) The server keeps serving: the process did not terminate and the very
	// next ordinary request is answered normally, on the production budget.
	setGraphQueryBudget(t, defaultGraphQueryBudget)
	next := doGraphData(t, name, url.Values{"limit": {"3000"}})
	if next.Code != http.StatusOK {
		t.Fatalf("follow-up status = %d, want 200: the server must keep serving; body=%q", next.Code, next.Body.String())
	}
	var view graphView
	if err := json.Unmarshal(next.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding follow-up: %v", err)
	}
	if len(view.Nodes) != nodes {
		t.Errorf("follow-up nodes = %d, want %d", len(view.Nodes), nodes)
	}
}

// TestHandleGraphData_BudgetExhaustionWritesNothing asserts rule 7: cancelling a
// query changes nothing on disk. The store is opened read-only, so an abandoned
// query writes no data, runs no checkpoint, and truncates no write-ahead log.
//
// It asserts that the way every other graph test asserts store immutability — a
// fresh read after the failure must still return exactly the seeded graph, with
// no node or edge added, removed, or altered.
func TestHandleGraphData_BudgetExhaustionWritesNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	nodes := seedExpensiveGraph(t, name)

	// The seeded graph as it must still look afterwards.
	before := doGraphData(t, name, url.Values{"limit": {"3000"}})
	if before.Code != http.StatusOK {
		t.Fatalf("baseline read status = %d, want 200; body=%q", before.Code, before.Body.String())
	}
	baseline := before.Body.String()

	setGraphQueryBudget(t, 150*time.Millisecond)
	rec := doGraphData(t, name, url.Values{"q": {expensiveGraphQuery}, "limit": {"3000"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expensive query status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	setGraphQueryBudget(t, defaultGraphQueryBudget)

	after := doGraphData(t, name, url.Values{"limit": {"3000"}})
	if after.Code != http.StatusOK {
		t.Fatalf("post-cancellation read status = %d, want 200; body=%q", after.Code, after.Body.String())
	}
	if after.Body.String() != baseline {
		var view graphView
		_ = json.Unmarshal(after.Body.Bytes(), &view)
		t.Fatalf("the store changed across a cancelled query: nodes=%d edges=%d, want the %d seeded nodes unchanged", len(view.Nodes), len(view.Edges), nodes)
	}
}

// TestHandleGraphData_OrdinaryQueryUnaffectedByBudget asserts rule 6: a query
// that completes within the budget is served exactly as it was before the budget
// existed — the same nodes and edges, in the same response shape, with nothing
// truncated and no ordering changed.
//
// It proves that differentially rather than by restating the expected payload:
// the same request is served under the production budget and under a budget so
// large it can never fire, and the two response bodies must be byte-identical.
// Anything the deadline truncated, reordered, or dropped would separate them.
// The payload is also asserted against the seeded graph, so the comparison
// cannot pass by both sides being empty.
func TestHandleGraphData_OrdinaryQueryUnaffectedByBudget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	queries := []struct {
		name  string
		param url.Values
	}{
		{"default query", nil},
		{"explicit full read", url.Values{"q": {"MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m"}}},
		{"path projection", url.Values{"q": {"MATCH p = (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN p"}}},
		{"aggregate", url.Values{"q": {"MATCH (n) RETURN count(*)"}}},
	}

	for _, tc := range queries {
		t.Run(tc.name, func(t *testing.T) {
			withBudget := doGraphData(t, name, tc.param)
			if withBudget.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 under the production budget; body=%q", withBudget.Code, withBudget.Body.String())
			}

			// A budget that cannot possibly fire stands in for "before the budget
			// existed": the deadline is still derived and still installed, it just
			// never elapses.
			setGraphQueryBudget(t, time.Hour)
			unbounded := doGraphData(t, name, tc.param)
			setGraphQueryBudget(t, defaultGraphQueryBudget)

			if unbounded.Code != withBudget.Code {
				t.Fatalf("status = %d under the production budget, %d effectively unbounded", withBudget.Code, unbounded.Code)
			}
			if withBudget.Body.String() != unbounded.Body.String() {
				t.Errorf("the 5s budget changed the response:\n with budget: %s\n unbounded:   %s", withBudget.Body.String(), unbounded.Body.String())
			}
		})
	}

	// The comparison above is not vacuous: the default query really does return
	// the seeded graph in full.
	rec := doGraphData(t, name, nil)
	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(view.Nodes) != 3 || len(view.Edges) != 2 {
		t.Fatalf("nodes/edges = %d/%d, want 3/2: the differential comparison must run on a non-empty graph", len(view.Nodes), len(view.Edges))
	}
}

// TestLoadGraphView_ClientDisconnectIsNotReportedAsTheBudget guards the trap in
// classifying the two composed cancellation sources: the budget is derived FROM
// the request context, so both a client disconnect and an exhausted budget abort
// the same query through the same derived context. They must not be confused.
//
// A request whose own context is already cancelled — the client disconnected —
// must still be an execution failure (rule 4: no new kind), but reported as the
// cancellation it is, never as budget exhaustion.
func TestLoadGraphView_ClientDisconnectIsNotReportedAsTheBudget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	// A generous budget, so nothing but the disconnect can cancel this query.
	setGraphQueryBudget(t, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the client went away before the query could run

	_, err := loadGraphView(ctx, name, "MATCH (n) RETURN n", "100")
	qe, ok := asGraphQueryError(err)
	if !ok {
		t.Fatalf("err = %v (%T), want a classified graphQueryError", err, err)
	}
	if qe.Kind != graphErrExecution {
		t.Errorf("kind = %q, want %q: a cancelled request is still an execution failure, not a new kind", qe.Kind, graphErrExecution)
	}
	if strings.Contains(qe.Reason, "budget") {
		t.Errorf("reason = %q: the client disconnected, so the failure must not be blamed on the query time budget", qe.Reason)
	}
	if !strings.HasPrefix(qe.Reason, "query failed to execute: ") {
		t.Errorf("reason = %q, want the existing \"query failed to execute: \" message", qe.Reason)
	}
}

// TestGraphExecutionError_Classification drives the classifier directly over the
// three failures the endpoint can see, including the pair a live parent context
// separates. It is the unit-level companion to the two tests above: it fences
// the wording of each reason, and it fences the rule that all three keep the
// single execution kind (SPEC/WEB.md § Graph Query Time Budget, rules 4 and 5).
func TestGraphExecutionError_Classification(t *testing.T) {
	live := context.Background()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	// What the engine returns: it wraps ctx.Err() with a "cypher:" prefix
	// (cypher.checkContext), so the wrapped sentinel is what must be matched.
	wrappedDeadline := fmt.Errorf("cypher: %w", context.DeadlineExceeded)
	wrappedCancel := fmt.Errorf("cypher: %w", context.Canceled)
	engineFailure := errors.New("cypher: parse error at offset 12")

	cases := []struct {
		name         string
		parent       context.Context
		err          error
		wantContains string
		wantAbsent   string
	}{
		{
			name:         "budget exhausted on a live request",
			parent:       live,
			err:          wrappedDeadline,
			wantContains: "exceeded the 150ms query time budget",
			wantAbsent:   "cancelled",
		},
		{
			name:         "client disconnected",
			parent:       cancelled,
			err:          wrappedCancel,
			wantContains: "the request was cancelled before the query finished",
			wantAbsent:   "budget",
		},
		{
			name:         "parent deadline, not ours",
			parent:       cancelled,
			err:          wrappedDeadline,
			wantContains: "the request was cancelled before the query finished",
			wantAbsent:   "budget",
		},
		{
			name:         "ordinary engine failure keeps its message",
			parent:       live,
			err:          engineFailure,
			wantContains: "cypher: parse error at offset 12",
			wantAbsent:   "budget",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			qe := graphExecutionError(tc.parent, 150*time.Millisecond, tc.err)
			if qe.Kind != graphErrExecution {
				t.Errorf("kind = %q, want %q for every execution failure", qe.Kind, graphErrExecution)
			}
			if !strings.HasPrefix(qe.Reason, "query failed to execute: ") {
				t.Errorf("reason = %q, want the shared \"query failed to execute: \" prefix", qe.Reason)
			}
			if !strings.Contains(qe.Reason, tc.wantContains) {
				t.Errorf("reason = %q, want it to contain %q", qe.Reason, tc.wantContains)
			}
			if strings.Contains(qe.Reason, tc.wantAbsent) {
				t.Errorf("reason = %q, must not contain %q", qe.Reason, tc.wantAbsent)
			}
		})
	}
}
