package web

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

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
//   - SprintsCurrent:  OPEN sprints (zero, one, or more), ascending id.
//   - SprintsClosed:   CLOSED sprints, descending sprint Order (last executed first).
//
// ModalTasks carries the tasks for which the page renders a task detail modal:
// the OPEN sprints' member tasks (those shown clickable on the Actual tab),
// deduplicated by task ID so each modal id is rendered exactly once. The
// Próximos and Concluídos tabs only link to sprint pages and do not open task
// modals, so their member tasks are not included here.
type sprintsData struct {
	Name            string
	Chrome          chrome
	SprintsUpcoming []sprintView
	SprintsCurrent  []sprintView
	SprintsClosed   []sprintView
	ModalTasks      []models.Task
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

// Detail returns the shared context object the "sprintDetail" sub-template
// consumes for one OPEN sprint on the Actual tab. Both call sites (the Actual
// tab and the single sprint page) feed the sub-template the identical shape, so
// a sprint renders identically in both places (SPEC/WEB.md § Shared Sprint
// Presentation Sub-Template). The roadmap Name is threaded through so the
// sub-template can build sprint links.
//
// The value receiver is deliberate: html/template invokes this method on a
// (copied) range element on the Actual tab, and a pointer receiver would not be
// in the value's method set, so the template call would silently fail.
//
//nolint:gocritic // value receiver required by html/template (see comment above)
func (v sprintView) Detail(name string) sprintDetail {
	return sprintDetail{Name: name, Sprint: v.Sprint, Tasks: v.Tasks, Summary: v.Summary}
}

// sprintDetail is the single context shape the shared "sprintDetail"
// sub-template renders. The Roadmap Sprint Page and each OPEN sprint on the
// Actual tab both build one of these and hand it to the same sub-template, so
// the full sprint detail block is byte-identical everywhere it appears
// (SPEC/WEB.md § Shared Sprint Presentation Sub-Template; Acceptance Criterion
// 38).
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

// Detail returns the shared context object the "sprintDetail" sub-template
// consumes for the single sprint page, identical in shape to the one the Actual
// tab feeds the same sub-template (SPEC/WEB.md § Shared Sprint Presentation
// Sub-Template).
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
// ModalTasks is the deduplicated set of OPEN-sprint member tasks the Actual
// tab shows clickable, so the page renders exactly one task detail modal per
// distinct task without scanning the whole roadmap.
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
		ModalTasks:      dedupeSprintTasks(current),
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

// dedupeSprintTasks flattens the member tasks of the given sprint views into a
// single slice with each task ID appearing once, preserving first-seen order.
// A task can in principle belong to more than one OPEN sprint; deduplicating
// by ID keeps each task detail modal's id unique so the page renders one modal
// per distinct task (SPEC/WEB.md § Roadmap Sprints Page, Task Detail Modal).
func dedupeSprintTasks(views []sprintView) []models.Task {
	out := make([]models.Task, 0)
	seen := make(map[int]struct{})
	for i := range views {
		tasks := views[i].Tasks
		for j := range tasks {
			id := tasks[j].ID
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, tasks[j])
		}
	}
	return out
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
//   - current:  OPEN, ascending id.
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
		return current[i].Sprint.ID < current[j].Sprint.ID
	})
	sort.SliceStable(closed, func(i, j int) bool {
		return closed[i].Sprint.Order > closed[j].Sprint.Order
	})

	return upcoming, current, closed
}

// loadGraphView reads a roadmap's knowledge graph read-only and returns its
// nodes and edges in the Graph View Data shape. It mirrors the read path of
// commands/graph.go runGraphRead: it opens the store via recovery and runs
// read-only Cypher queries through the engine. It MUST NOT run any writing
// clause and MUST NOT checkpoint or truncate the WAL.
//
// A roadmap that has never used the graph command (no graph/ directory) is
// an empty graph, not an error: loadGraphView returns empty arrays WITHOUT
// creating the directory, so a web read leaves the store's on-disk files
// untouched (SPEC/WEB.md § Roadmap Knowledge-Graph Page, empty graph).
func loadGraphView(ctx context.Context, name string) (graphView, error) {
	empty := graphView{Nodes: []map[string]any{}, Edges: []map[string]any{}}

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

	nodes, err := queryGraphElements(ctx, engine, "MATCH (n) RETURN n")
	if err != nil {
		return graphView{}, err
	}
	edges, err := queryGraphElements(ctx, engine, "MATCH ()-[r]->() RETURN r")
	if err != nil {
		return graphView{}, err
	}

	out := graphView{Nodes: make([]map[string]any, 0, len(nodes)), Edges: make([]map[string]any, 0, len(edges))}
	out.Nodes = append(out.Nodes, nodes...)
	out.Edges = append(out.Edges, edges...)
	return out, nil
}

// queryGraphElements runs a single-column read-only query that returns one
// graph element per row and serialises each into a JSON object map. The
// query is run through the engine's read path (Run, not RunInTx), so no
// write or checkpoint occurs.
func queryGraphElements(ctx context.Context, engine *cypher.Engine, query string) ([]map[string]any, error) {
	result, err := engine.Run(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: graph read failed: %v", utils.ErrDatabase, err)
	}
	defer result.Close() //nolint:errcheck // read path; close commits nothing

	out := make([]map[string]any, 0)
	cols := result.Columns()
	for result.Next() {
		rec := result.Record()
		for _, col := range cols {
			raw, ok := rec[col]
			if !ok {
				continue
			}
			v, ok := raw.(expr.Value)
			if !ok {
				continue
			}
			if m, ok := serializeGraphValue(v).(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("%w: graph read failed: %v", utils.ErrDatabase, err)
	}
	return out, nil
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
