package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"

	"github.com/FlavioCFOliveira/Groadmap/internal/cypherguard"
	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// defaultGraphQuery is the Cypher the graph data endpoint runs when the request
// carries no q parameter. It is identical to the query the page's query bar
// pre-fills on load, so a request with no q is backward compatible with the
// previous fixed full-graph read: MATCH (n) collects every node and the
// OPTIONAL MATCH collects every relationship with both endpoints (SPEC/WEB.md
// § Graph Data Endpoint; § Graph Query Bar, default query).
const defaultGraphQuery = "MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m"

// defaultGraphLimit is the node limit applied when the request carries no limit
// parameter; it matches the page dropdown's default selection (SPEC/WEB.md
// § Graph Data Endpoint, query parameters).
const defaultGraphLimit = 100

// allowedGraphLimits is the closed set of node-limit values the limit dropdown
// offers and the endpoint accepts. A limit outside this set is rejected as an
// invalid limit; the endpoint never clamps to the nearest value (SPEC/WEB.md
// § Graph Data Endpoint, query parameters; § Query-Bar Error Handling, rule 2).
var allowedGraphLimits = map[int]struct{}{
	50: {}, 100: {}, 250: {}, 500: {}, 1000: {}, 3000: {},
}

// reTopLevelLimit detects a top-level LIMIT clause on the masked normalization
// of a query. The endpoint injects its own LIMIT only when the user's query has
// none, so a user-authored LIMIT is respected as-is (SPEC/WEB.md § Graph Data
// Endpoint, node-limit injection). The check runs on the literal-masked query
// (cypherguard.MaskLiterals), so a LIMIT keyword that appears only inside a
// string literal, comment, or backtick identifier does not count as an existing
// LIMIT and does not suppress injection.
var reTopLevelLimit = regexp.MustCompile(`(?i)\bLIMIT\b`)

// graphQueryError classifies a query-bar failure so the handler can map it to a
// distinct, in-page, read-only message (SPEC/WEB.md § Query-Bar Error Handling).
// The three kinds are kept separate so the user understands what to fix: a
// rejection (the query is not read-only), an invalid limit, or an execution
// failure in the engine.
type graphQueryError struct {
	// Reason is the user-facing message shown in place on the page.
	Reason string
	// Kind is the machine-readable failure class (see the graphErr* constants).
	Kind string
}

func (e *graphQueryError) Error() string { return e.Reason }

// Query-bar failure kinds. They map 1:1 to the three distinct cases in
// SPEC/WEB.md § Query-Bar Error Handling.
const (
	graphErrNotReadOnly  = "not_read_only" // query contains a writing or DDL clause
	graphErrInvalidLimit = "invalid_limit" // limit not one of the six allowed values
	graphErrExecution    = "execution"     // accepted as read-only but failed in the engine
)

// newGraphQueryError builds a classified query-bar error.
func newGraphQueryError(kind, reason string) *graphQueryError {
	return &graphQueryError{Kind: kind, Reason: reason}
}

// sprintsData is the view model handed to the roadmap sprints template (the
// roadmap's landing page). It presents the roadmap's sprints grouped into the
// three tabs (Próximos / Actual / Concluídos), plus the relationships modelled
// in the data: sprint membership with in-sprint order. It is read-only;
// nothing here is persisted. The sprints page does NOT render the full tasks
// table (SPEC/WEB.md § Roadmap Sprints Page).
//
// The three sprint slices are disjoint partitions of the roadmap's sprints by
// status (SPEC/WEB.md § Roadmap Sprints Page):
//   - SprintsUpcoming: PENDING sprints, ascending sprint Order (next to execute first).
//   - SprintsCurrent:  OPEN sprints (zero, one, or more), ascending sprint Order.
//   - SprintsClosed:   CLOSED sprints, descending sprint Order (last executed first).
//
// Every sprint in every tab is rendered through the single shared sprintCard
// partial, so all sprints share identical card markup. The OPEN sprint under
// Actual uses the same card as a PENDING or CLOSED sprint and is NOT expanded
// into an inline task table or per-task modals; the full sprint detail block
// lives only on the single Roadmap Sprint Page. The sprints page therefore
// renders no task detail modal at all (SPEC/WEB.md § Shared Sprint-Card Partial;
// Acceptance Criteria 8/12/38).
type sprintsData struct {
	Name            string
	Chrome          chrome
	SprintsUpcoming []sprintView
	SprintsCurrent  []sprintView
	SprintsClosed   []sprintView
}

// tasksData is the view model handed to the roadmap tasks template. It
// presents the roadmap's full task table — every task, any status — with each
// row clickable to open the read-only task detail modal. Tasks is the full,
// unfiltered task list rendered in the Tasks table and the task detail modals.
// It is read-only; nothing here is persisted (SPEC/WEB.md § Roadmap Tasks
// Page).
type tasksData struct {
	Name   string
	Chrome chrome
	Tasks  []models.Task
}

// sprintCompletion is the precomputed per-sprint completion summary the shared
// sprint presentation sub-template renders as its status summary line. It is
// derived ONLY from the sprint's own loaded member tasks (no extra DB query),
// using the shared models.CalculateSprintSummary categorisation so it never
// diverges from models.CalculateSprintShowResult (SPEC/WEB.md § Shared Sprint
// Presentation Sub-Template, sprint status summary line). Precomputing it keeps
// the template declarative: the template reads fields instead of computing.
type sprintCompletion struct {
	Pending    int // P: tasks in BACKLOG or SPRINT.
	InProgress int // A ("Abertas"): tasks in DOING or TESTING.
	Completed  int // C: tasks in COMPLETED.
	Total      int // T: total member tasks.
	Pct        int // completion percentage, rounded to the nearest integer (0 when Total == 0).
}

// newSprintCompletion builds the completion summary for one sprint from its
// loaded member tasks. It reuses models.CalculateSprintSummary (the same
// categorisation models.CalculateSprintShowResult encodes) so the web summary
// and the CLI sprint report agree exactly.
func newSprintCompletion(tasks []models.Task) sprintCompletion {
	summary := models.CalculateSprintSummary(tasks)
	return sprintCompletion{
		Pending:    summary.Pending,
		InProgress: summary.InProgress,
		Completed:  summary.Completed,
		Total:      summary.TotalTasks,
		Pct:        summary.CompletionPercentage(),
	}
}

// Line renders the sprint status summary line in the exact documented format
// `<pct>% - P:<p> A:<a> C:<c> - T:<t>` (for example `33% - P:8 A:3 C:18 - T:55`).
// It is the single place the format string lives, so both call sites of the
// shared sub-template produce a byte-identical line (SPEC/WEB.md § Shared Sprint
// Presentation Sub-Template, sprint status summary line).
func (c sprintCompletion) Line() string {
	return fmt.Sprintf("%d%% - P:%d A:%d C:%d - T:%d",
		c.Pct, c.Pending, c.InProgress, c.Completed, c.Total)
}

// sprintView pairs a sprint with its ordered member tasks. Tasks preserves
// the planned in-sprint execution order (sprint_tasks position order) and
// carries each task's full record, so the Actual tab and the sprint page can
// show every task's status without a second lookup. Summary is the precomputed
// completion summary the Actual tab's shared sub-template renders. Field order
// places the slice header before the embedded Sprint value to keep the
// pointer-scan prefix minimal (govet fieldalignment).
type sprintView struct {
	Tasks   []models.Task
	Sprint  models.Sprint
	Summary sprintCompletion
}

// Card returns the context object the shared "sprintCard" partial consumes for
// one sprint on any tab of the Roadmap Sprints Page (SPEC/WEB.md § Shared
// Sprint-Card Partial). The roadmap Name is threaded through so the partial can
// build the card's link to the sprint's own page, and TaskCount is the loaded
// member-task count rendered in the card footer.
//
// The value receiver is deliberate: html/template invokes this method on a
// (copied) range element, and a pointer receiver would not be in the value's
// method set, so the template call would silently fail.
//
//nolint:gocritic // value receiver required by html/template (see comment above)
func (v sprintView) Card(name string) sprintCard {
	return sprintCard{Name: name, Sprint: v.Sprint, TaskCount: len(v.Tasks)}
}

// sprintCard is the single context shape the shared "sprintCard" partial
// renders. Every tab of the Roadmap Sprints Page — Próximos, Actual, and
// Concluídos — builds one of these per sprint and hands it to the same partial,
// so all sprints share identical card markup across the three tabs (SPEC/WEB.md
// § Shared Sprint-Card Partial; Acceptance Criteria 8/12/38). TaskCount is the
// sprint's total member-task count shown in the card footer (Acceptance
// Criterion 40).
type sprintCard struct {
	Name      string
	Sprint    models.Sprint
	TaskCount int
}

// sprintDetail is the single context shape the "sprintDetail" sub-template
// renders. Only the single Roadmap Sprint Page builds one and hands it to the
// sub-template, so the full sprint detail block appears only there (SPEC/WEB.md
// § Sprint Detail Sub-Template; Acceptance Criterion 38).
type sprintDetail struct {
	Name    string
	Tasks   []models.Task
	Sprint  models.Sprint
	Summary sprintCompletion
}

// sprintPageData is the view model handed to the roadmap sprint template. It
// presents a single sprint with all of its fields and its tasks in planned
// in-sprint execution order, each clickable to open the read-only task detail
// modal (SPEC/WEB.md § Roadmap Sprint Page). It is read-only.
type sprintPageData struct {
	Name    string
	Chrome  chrome
	Tasks   []models.Task
	Sprint  models.Sprint
	Summary sprintCompletion
}

// Detail returns the context object the "sprintDetail" sub-template consumes
// for the single sprint page, the only call site of that sub-template
// (SPEC/WEB.md § Sprint Detail Sub-Template).
//
// The value receiver is deliberate: renderHTML passes a sprintPageData value
// (not a pointer) to ExecuteTemplate, so a pointer-receiver Detail would not be
// in the dot's method set and the sprint.html template call would fail.
//
//nolint:gocritic // value receiver required by html/template (see comment above)
func (d sprintPageData) Detail() sprintDetail {
	return sprintDetail{Name: d.Name, Sprint: d.Sprint, Tasks: d.Tasks, Summary: d.Summary}
}

// graphView is the JSON shape returned by the graph data endpoint
// (SPEC/DATA_FORMATS.md § Graph View Data). nodes and edges are always
// present and never null; an empty graph returns empty arrays.
type graphView struct {
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

// loadRoadmapNames returns the names of all roadmaps under ~/.roadmaps/,
// using the same discovery rule the CLI uses (immediate subdirectories with
// a project.db). An empty result is not an error: the index renders an
// empty state.
func loadRoadmapNames() ([]string, error) {
	return utils.ListRoadmaps()
}

// loadSprints reads a roadmap's sprints read-only for the sprints landing
// page. It opens the roadmap database, reads every sprint, resolves each
// sprint's ordered member tasks, and classifies the sprints into the three
// tabs. It does NOT read the full task table — the sprints page does not
// render it (SPEC/WEB.md § Roadmap Sprints Page). The database handle is
// released before the function returns; no row is written and no audit entry
// is produced (SPEC/WEB.md § Tasks and Sprints from SQLite).
//
// Every sprint is rendered as a compact card through the shared sprintCard
// partial; the sprints page opens no task detail modal, so the member tasks are
// loaded only to compute each card's footer task count (SPEC/WEB.md § Shared
// Sprint-Card Partial).
//
// The caller is responsible for the {name} validation and existence check
// (resolveRoadmap); this function trusts name is a validated, existing
// roadmap.
func loadSprints(ctx context.Context, name string) (sprintsData, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return sprintsData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	sprints, err := database.ListSprints(ctx, nil)
	if err != nil {
		return sprintsData{}, err
	}

	views := make([]sprintView, 0, len(sprints))
	for i := range sprints {
		orderedTasks, terr := sprintOrderedTasks(ctx, database, sprints[i].ID)
		if terr != nil {
			return sprintsData{}, terr
		}
		views = append(views, sprintView{
			Sprint:  sprints[i],
			Tasks:   orderedTasks,
			Summary: newSprintCompletion(orderedTasks),
		})
	}

	upcoming, current, closed := classifySprints(views)
	return sprintsData{
		Name:            name,
		SprintsUpcoming: upcoming,
		SprintsCurrent:  current,
		SprintsClosed:   closed,
	}, nil
}

// loadTasks reads a roadmap's full task table read-only for the tasks page. It
// opens the roadmap database, reads every task (no status filter), and returns
// it. It does NOT read sprints — the tasks page only shows the task table
// (SPEC/WEB.md § Roadmap Tasks Page). The database handle is released before
// the function returns; no row is written and no audit entry is produced
// (SPEC/WEB.md § Tasks and Sprints from SQLite).
//
// The caller is responsible for the {name} validation and existence check
// (resolveRoadmap); this function trusts name is a validated, existing
// roadmap.
func loadTasks(ctx context.Context, name string) (tasksData, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return tasksData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	// Every task, any status: an unfiltered list with the maximum limit so
	// the page shows the whole roadmap. Task already carries depends_on,
	// blocks, subtask_count, and parent_task_id.
	tasks, err := database.ListTasks(ctx, &db.TaskListFilter{Limit: models.MaxTaskLimit})
	if err != nil {
		return tasksData{}, err
	}

	return tasksData{Name: name, Tasks: tasks}, nil
}

// loadSprint reads a single sprint of a roadmap read-only and returns the
// sprint-page view model: the sprint with all its fields and its member tasks
// in planned in-sprint execution order (SPEC/WEB.md § Roadmap Sprint Page).
// The database handle is released before the function returns; no row is
// written and no audit entry is produced.
//
// The caller validates {name} and confirms it exists (resolveRoadmap) and
// parses {id} to an integer before calling. loadSprint returns
// utils.ErrNotFound (from db.GetSprint) when no sprint with that id belongs to
// the roadmap, which the handler maps to HTTP 404.
func loadSprint(ctx context.Context, name string, id int) (sprintPageData, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return sprintPageData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	sprint, err := database.GetSprint(ctx, id)
	if err != nil {
		return sprintPageData{}, err
	}

	orderedTasks, err := sprintOrderedTasks(ctx, database, sprint.ID)
	if err != nil {
		return sprintPageData{}, err
	}

	return sprintPageData{
		Name:    name,
		Sprint:  *sprint,
		Tasks:   orderedTasks,
		Summary: newSprintCompletion(orderedTasks),
	}, nil
}

// sprintOrderedTasks resolves a sprint's member tasks in the planned in-sprint
// execution order, which is the sprint_tasks position order (DATABASE.md
// § Relationships; the schema's sprint_tasks.position column and its
// idx_sprint_tasks_order index). db.GetSprintTasksFull with a nil status
// filter and orderByPriority=false returns the full task records ordered by
// st.position ASC, so each task carries its status, depends_on, blocks, and
// the rest of its fields for the Actual tab, the sprint page, and the task
// detail modal — all without a second per-task query.
func sprintOrderedTasks(ctx context.Context, database *db.DB, sprintID int) ([]models.Task, error) {
	return database.GetSprintTasksFull(ctx, sprintID, nil, false)
}

// classifySprints partitions a roadmap's sprints into the three sprints-page
// tabs by status and orders each group as the page presents it (SPEC/WEB.md
// § Roadmap Sprints Page; Acceptance Criterion 12):
//   - upcoming: PENDING, ascending sprint Order (the unique execution order;
//     the next sprint to execute, lowest Order, appears first).
//   - current:  OPEN, ascending sprint Order (consistent with the other tabs).
//   - closed:   CLOSED, descending sprint Order (the last in execution order,
//     highest Order, appears first).
//
// Sprint Order is a positive integer unique across the roadmap (MODELS.md
// § Sprint), so the ordering is total and needs no tiebreak.
//
// A sprint whose status is none of PENDING/OPEN/CLOSED is dropped from all
// groups; the sprint status enum is closed (MODELS.md § Enums), so this is
// defensive only.
func classifySprints(views []sprintView) (upcoming, current, closed []sprintView) {
	upcoming = make([]sprintView, 0)
	current = make([]sprintView, 0)
	closed = make([]sprintView, 0)

	for i := range views {
		switch views[i].Sprint.Status {
		case models.SprintPending:
			upcoming = append(upcoming, views[i])
		case models.SprintOpen:
			current = append(current, views[i])
		case models.SprintClosed:
			closed = append(closed, views[i])
		}
	}

	sort.SliceStable(upcoming, func(i, j int) bool {
		return upcoming[i].Sprint.Order < upcoming[j].Sprint.Order
	})
	sort.SliceStable(current, func(i, j int) bool {
		return current[i].Sprint.Order < current[j].Sprint.Order
	})
	sort.SliceStable(closed, func(i, j int) bool {
		return closed[i].Sprint.Order > closed[j].Sprint.Order
	})

	return upcoming, current, closed
}

// resolveGraphLimit validates the raw limit query parameter and returns the
// resolved limit to apply. An absent or empty parameter resolves to the default
// limit (SPEC/WEB.md § Graph Data Endpoint, query parameters). A present value
// MUST be one of the six allowed values; anything else (non-integer or
// out-of-set) is rejected as an invalid limit and the query is NOT executed —
// the endpoint never clamps to the nearest allowed value (SPEC/WEB.md
// § Query-Bar Error Handling, rule 2). The returned error is a classified
// graphQueryError so the handler can surface a distinct in-page message.
func resolveGraphLimit(raw string) (int, error) {
	if raw == "" {
		return defaultGraphLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, newGraphQueryError(graphErrInvalidLimit, fmt.Sprintf("invalid limit %q: must be one of 50, 100, 250, 500, 1000, 3000", raw))
	}
	if _, ok := allowedGraphLimits[n]; !ok {
		return 0, newGraphQueryError(graphErrInvalidLimit, fmt.Sprintf("invalid limit %d: must be one of 50, 100, 250, 500, 1000, 3000", n))
	}
	return n, nil
}

// resolveGraphQuery returns the query to run: the trimmed user-supplied q, or
// the default full-graph query when q is absent or empty (SPEC/WEB.md § Graph
// Data Endpoint, q parameter). It is the single place the default-query
// fallback lives, so the endpoint stays backward compatible.
func resolveGraphQuery(raw string) string {
	if q := strings.TrimSpace(raw); q != "" {
		return q
	}
	return defaultGraphQuery
}

// applyGraphLimit appends a top-level LIMIT clause to query, but ONLY when the
// query does not already contain a top-level LIMIT (SPEC/WEB.md § Graph Data
// Endpoint, node-limit injection). The presence check runs on the literal-masked
// normalization (cypherguard.MaskLiterals), so a LIMIT keyword that appears only
// inside a string literal, a comment, or a backtick identifier does not count as
// an existing LIMIT and does not suppress injection. A user-authored top-level
// LIMIT is respected as-is and the resolved dropdown value is not applied.
func applyGraphLimit(query string, limit int) string {
	masked := cypherguard.MaskLiterals(query)
	if reTopLevelLimit.MatchString(masked) {
		return query
	}
	return query + " LIMIT " + strconv.Itoa(limit)
}

// loadGraphView reads a roadmap's knowledge graph read-only and returns its
// nodes and edges in the Graph View Data shape. It mirrors the read path of
// commands/graph.go runGraphRead: it opens the store via recovery and runs a
// single read-only Cypher query through the engine's read path. It MUST NOT run
// any writing clause and MUST NOT checkpoint or truncate the WAL (SPEC/WEB.md
// § Graph Data Endpoint, read-only guard-rail).
//
// rawQuery and rawLimit are the request's q and limit URL parameters (empty
// when absent). The query is resolved (default when absent), validated as
// read-only via the shared cypherguard guard-rail BEFORE execution, and has a
// LIMIT injected only when it has no top-level LIMIT. A query that contains any
// writing or DDL clause, or an invalid limit, is returned as a classified
// graphQueryError and is never executed; the store is not opened for it when
// the failure is detectable before opening.
//
// A roadmap that has never used the graph command (no graph/ directory) is an
// empty graph, not an error: loadGraphView returns empty arrays WITHOUT creating
// the directory, so a web read leaves the store's on-disk files untouched
// (SPEC/WEB.md § Roadmap Knowledge-Graph Page, empty graph).
func loadGraphView(ctx context.Context, name, rawQuery, rawLimit string) (graphView, error) {
	empty := graphView{Nodes: []map[string]any{}, Edges: []map[string]any{}}

	// Resolve and validate the limit first; an invalid limit rejects the
	// request before the query runs and before the store is opened (SPEC/WEB.md
	// § Query-Bar Error Handling, rule 2).
	limit, err := resolveGraphLimit(rawLimit)
	if err != nil {
		return graphView{}, err
	}

	// Read-only guard-rail (security-critical): the user-supplied query is
	// validated against the SAME masked-normalization read-only check the CLI
	// `graph query`/`search` subcommands enforce. A writing or DDL clause is
	// rejected here, before the query is ever handed to the engine, so it can
	// never run and never write (SPEC/WEB.md § Graph Data Endpoint).
	query := resolveGraphQuery(rawQuery)
	if !cypherguard.IsReadOnly(query) {
		return graphView{}, newGraphQueryError(graphErrNotReadOnly, "query rejected: not read-only")
	}

	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		return graphView{}, err
	}
	graphDir := filepath.Join(roadmapDir, "graph")

	// A read must not create the graph store. If the directory is absent the
	// roadmap simply has no graph yet — return the empty shape.
	//
	// graphDir derives from name, which utils.GetRoadmapDir validated against
	// the roadmap-name rules (^[a-z0-9_-]+$, no '/' and no '..') above, and the
	// route handler validated again before calling this function. A path
	// outside ~/.roadmaps/<name>/ is therefore unreachable here.
	info, statErr := os.Stat(graphDir) // #nosec G703 -- name validated by GetRoadmapDir and the route guard; no traversal possible
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return empty, nil
		}
		return graphView{}, fmt.Errorf("%w: stat graph store: %v", utils.ErrDatabase, statErr)
	} else if !info.IsDir() {
		return empty, nil
	}

	res, err := recovery.Open[string, float64](graphDir, recovery.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})
	if err != nil {
		return graphView{}, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, err)
	}

	engine := cypher.NewEngine(res.Graph)

	// Inject the node limit only when the (validated, read-only) query has no
	// top-level LIMIT of its own. The original query — not the masked copy — is
	// what executes; masking only governs the presence check and the guard-rail.
	executed := applyGraphLimit(query, limit)
	return runGraphViewQuery(ctx, engine, executed)
}

// runGraphViewQuery executes a validated read-only query through the engine's
// read path (Run, not RunInTx, so no write or checkpoint occurs), walks the
// ENTIRE result, and assembles the Graph View Data shape (SPEC/WEB.md § Graph
// Data Endpoint, result-to-graph extraction; SPEC/DATA_FORMATS.md § Graph View
// Data). An engine failure (for example invalid Cypher syntax) is returned as a
// classified execution-failure graphQueryError, distinct from a guard-rail
// rejection.
func runGraphViewQuery(ctx context.Context, engine *cypher.Engine, query string) (graphView, error) {
	result, err := engine.Run(ctx, query, nil)
	if err != nil {
		return graphView{}, newGraphQueryError(graphErrExecution, "query failed to execute: "+err.Error())
	}
	defer result.Close() //nolint:errcheck // read path; close commits nothing

	// Collect every node and relationship anywhere in the result, deduplicated
	// by id. nodeIDs records which node ids were collected so orphan edges (an
	// edge whose start or end node was not collected) can be dropped afterwards.
	c := newGraphCollector()
	cols := result.Columns()
	for result.Next() {
		rec := result.Record()
		for _, col := range cols {
			if v, ok := rec[col].(expr.Value); ok {
				c.walk(v)
			}
		}
	}
	if err := result.Err(); err != nil {
		return graphView{}, newGraphQueryError(graphErrExecution, "query failed to execute: "+err.Error())
	}

	return c.view(), nil
}

// graphCollector accumulates the deduplicated nodes and relationships found by
// walking a query result, in first-seen order, and resolves orphan edges when
// it builds the final view. Nodes and relationships are keyed by their GoGraph
// id (uint64). first-seen ordering keeps the response stable for a given result.
type graphCollector struct {
	nodeSet map[uint64]struct{}
	edgeSet map[uint64]struct{}
	nodes   []map[string]any
	edges   []relCandidate
}

// relCandidate is a collected relationship plus the endpoint ids needed to drop
// it if either endpoint node was not collected (orphan-edge dropping).
type relCandidate struct {
	obj     map[string]any
	startID uint64
	endID   uint64
}

func newGraphCollector() *graphCollector {
	return &graphCollector{
		nodes:   make([]map[string]any, 0),
		edges:   make([]relCandidate, 0),
		nodeSet: make(map[uint64]struct{}),
		edgeSet: make(map[uint64]struct{}),
	}
}

// walk recursively descends an expr.Value, collecting every node and
// relationship it finds — directly, or nested inside a list, a map, or a path
// (SPEC/WEB.md § Graph Data Endpoint, result-to-graph extraction). The walk is
// exhaustive so an element nested inside a returned list, map, or path is
// collected exactly as one returned in its own column is.
func (c *graphCollector) walk(v expr.Value) {
	if v == nil {
		return
	}
	switch v.Kind() {
	case expr.KindNode:
		if nv, ok := v.(expr.NodeValue); ok {
			c.addNode(nv)
		}
	case expr.KindRelationship:
		if rv, ok := v.(expr.RelationshipValue); ok {
			c.addRel(rv)
		}
	case expr.KindPath:
		if pv, ok := v.(expr.PathValue); ok {
			for i := range pv.Nodes {
				c.addNode(pv.Nodes[i])
			}
			for i := range pv.Relationships {
				c.addRel(pv.Relationships[i])
			}
		}
	case expr.KindList:
		if lv, ok := v.(expr.ListValue); ok {
			for _, elem := range lv {
				c.walk(elem)
			}
		}
	case expr.KindMap:
		if mv, ok := v.(expr.MapValue); ok {
			for _, val := range mv {
				c.walk(val)
			}
		}
	default:
		// Scalars (string, int, float, bool, temporal, duration, null) carry no
		// graph element and are ignored for extraction.
	}
}

// addNode collects a node once, deduplicated by id.
func (c *graphCollector) addNode(nv expr.NodeValue) {
	if _, seen := c.nodeSet[nv.ID]; seen {
		return
	}
	c.nodeSet[nv.ID] = struct{}{}
	c.nodes = append(c.nodes, map[string]any{
		"id":         nv.ID,
		"labels":     nv.Labels,
		"properties": serializeProps(nv.Properties),
	})
}

// addRel collects a relationship once, deduplicated by id. The endpoint ids are
// kept so view() can drop the edge if either endpoint node was not collected.
func (c *graphCollector) addRel(rv expr.RelationshipValue) {
	if _, seen := c.edgeSet[rv.ID]; seen {
		return
	}
	c.edgeSet[rv.ID] = struct{}{}
	c.edges = append(c.edges, relCandidate{
		startID: rv.StartID,
		endID:   rv.EndID,
		obj: map[string]any{
			"id":         rv.ID,
			"type":       rv.Type,
			"startId":    rv.StartID,
			"endId":      rv.EndID,
			"properties": serializeProps(rv.Properties),
		},
	})
}

// view assembles the final Graph View Data, dropping any edge whose start or end
// node is not in the collected node set (orphan-edge dropping). This guarantees
// the startId/endId invariant: every edge endpoint references a node present in
// the returned nodes array, without inventing a synthetic endpoint (SPEC/WEB.md
// § Graph Data Endpoint; SPEC/DATA_FORMATS.md § Graph View Data, rule 3).
func (c *graphCollector) view() graphView {
	out := graphView{
		Nodes: c.nodes,
		Edges: make([]map[string]any, 0, len(c.edges)),
	}
	for i := range c.edges {
		_, hasStart := c.nodeSet[c.edges[i].startID]
		_, hasEnd := c.nodeSet[c.edges[i].endID]
		if hasStart && hasEnd {
			out.Edges = append(out.Edges, c.edges[i].obj)
		}
	}
	return out
}

// asGraphQueryError extracts a *graphQueryError from err, if err is one. The
// handler uses it to map a classified query-bar failure to its distinct in-page
// message (SPEC/WEB.md § Query-Bar Error Handling).
func asGraphQueryError(err error) (*graphQueryError, bool) {
	var qe *graphQueryError
	if errors.As(err, &qe) {
		return qe, true
	}
	return nil, false
}

// serializeGraphValue converts an expr.Value into a JSON-compatible Go
// value following SPEC/DATA_FORMATS.md § Graph Query Result property-type
// and element mapping.
//
// This intentionally duplicates a subset of commands.serializeValue across
// the package boundary: serializeValue is unexported in package commands and
// the web package must not depend on commands (the dependency runs
// commands -> web, not the reverse). The mapping is small and stable; the
// duplication is documented here and accepted per the task brief.
func serializeGraphValue(v expr.Value) any {
	if v == nil {
		return nil
	}
	switch v.Kind() {
	case expr.KindNull:
		return nil

	case expr.KindInteger:
		iv, _ := v.(expr.IntegerValue)
		return int64(iv)

	case expr.KindFloat:
		fv, _ := v.(expr.FloatValue)
		f := float64(fv)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return f

	case expr.KindString:
		sv, _ := v.(expr.StringValue)
		return string(sv)

	case expr.KindBool:
		bv, _ := v.(expr.BoolValue)
		return bool(bv)

	case expr.KindDate:
		dv, _ := v.(expr.DateValue)
		return dv.ToTime().UTC().Format("2006-01-02")

	case expr.KindDateTime:
		dtv, _ := v.(expr.DateTimeValue)
		return dtv.T.UTC().Format(time.RFC3339Nano)

	case expr.KindLocalDateTime:
		ldtv, _ := v.(expr.LocalDateTimeValue)
		return ldtv.T.Format("2006-01-02T15:04:05.999999999")

	case expr.KindLocalTime:
		ltv, _ := v.(expr.LocalTimeValue)
		return ltv.String()

	case expr.KindTime:
		tv, _ := v.(expr.TimeValue)
		return tv.String()

	case expr.KindDuration:
		durv, _ := v.(expr.DurationValue)
		return durv.String()

	case expr.KindList:
		lv, _ := v.(expr.ListValue)
		out := make([]any, len(lv))
		for i, elem := range lv {
			out[i] = serializeGraphValue(elem)
		}
		return out

	case expr.KindMap:
		mv, _ := v.(expr.MapValue)
		out := make(map[string]any, len(mv))
		for k, val := range mv {
			out[k] = serializeGraphValue(val)
		}
		return out

	case expr.KindNode:
		nv, _ := v.(expr.NodeValue)
		return map[string]any{
			"id":         nv.ID,
			"labels":     nv.Labels,
			"properties": serializeProps(nv.Properties),
		}

	case expr.KindRelationship:
		rv, _ := v.(expr.RelationshipValue)
		return map[string]any{
			"id":         rv.ID,
			"type":       rv.Type,
			"startId":    rv.StartID,
			"endId":      rv.EndID,
			"properties": serializeProps(rv.Properties),
		}

	default:
		return v.String()
	}
}

// serializeProps maps a property bag's values recursively through
// serializeGraphValue, producing a non-nil map (empty for no properties) so
// the JSON renders as {} rather than null.
func serializeProps(props map[string]expr.Value) map[string]any {
	out := make(map[string]any, len(props))
	for k, val := range props {
		out[k] = serializeGraphValue(val)
	}
	return out
}
