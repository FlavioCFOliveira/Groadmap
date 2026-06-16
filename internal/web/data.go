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
//   - SprintsUpcoming: PENDING sprints, ascending id (predicted execution order).
//   - SprintsCurrent:  OPEN sprints (zero, one, or more), ascending id.
//   - SprintsClosed:   CLOSED sprints, closed_at descending (nil/empty last).
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

// sprintView pairs a sprint with its ordered member tasks. Tasks preserves
// the planned in-sprint execution order (sprint_tasks position order) and
// carries each task's full record, so the Actual tab and the sprint page can
// show every task's status without a second lookup. Field order places the
// slice header before the embedded Sprint value to keep the pointer-scan
// prefix minimal (govet fieldalignment).
type sprintView struct {
	Tasks  []models.Task
	Sprint models.Sprint
}

// sprintPageData is the view model handed to the roadmap sprint template. It
// presents a single sprint with all of its fields and its tasks in planned
// in-sprint execution order, each clickable to open the read-only task detail
// modal (SPEC/WEB.md § Roadmap Sprint Page). It is read-only.
type sprintPageData struct {
	Name   string
	Chrome chrome
	Tasks  []models.Task
	Sprint models.Sprint
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
		views = append(views, sprintView{Sprint: sprints[i], Tasks: orderedTasks})
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

	return sprintPageData{Name: name, Sprint: *sprint, Tasks: orderedTasks}, nil
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

// classifySprints partitions a roadmap's sprints into the three detail-page
// tabs by status and orders each group as the detail page presents it
// (SPEC/WEB.md § Roadmap Detail Page; Acceptance Criterion 11):
//   - upcoming: PENDING, ascending id (predicted execution order; the next
//     sprint to start appears first).
//   - current:  OPEN, ascending id.
//   - closed:   CLOSED, closed_at descending; a sprint with a nil or empty
//     closed_at sorts last, with descending id as the tiebreak.
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
		return upcoming[i].Sprint.ID < upcoming[j].Sprint.ID
	})
	sort.SliceStable(current, func(i, j int) bool {
		return current[i].Sprint.ID < current[j].Sprint.ID
	})
	sort.SliceStable(closed, func(i, j int) bool {
		return closedSprintBefore(&closed[i].Sprint, &closed[j].Sprint)
	})

	return upcoming, current, closed
}

// closedSprintBefore reports whether closed sprint a should sort before b in
// the Concluídos tab: most recently closed first (closed_at descending). A
// sprint with no usable closed_at value sorts after any sprint that has one;
// when neither has a closed_at, or the two closed_at values are equal, the
// tiebreak is descending id (SPEC/WEB.md § Roadmap Detail Page, Concluídos).
func closedSprintBefore(a, b *models.Sprint) bool {
	ca, aok := closedAtValue(a)
	cb, bok := closedAtValue(b)

	switch {
	case aok && bok:
		if ca != cb {
			return ca > cb // descending closed_at: later timestamp first
		}
	case aok != bok:
		return aok // the one WITH a closed_at sorts first
	}
	// Equal closed_at, or both missing: descending id tiebreak.
	return a.ID > b.ID
}

// closedAtValue returns a sprint's closed_at timestamp and whether it is a
// usable (non-nil, non-empty) value. ISO 8601 UTC timestamps sort correctly
// as strings, so string comparison gives chronological order without parsing.
func closedAtValue(s *models.Sprint) (string, bool) {
	if s.ClosedAt == nil || *s.ClosedAt == "" {
		return "", false
	}
	return *s.ClosedAt, true
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
