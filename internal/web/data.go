package web

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// detailData is the view model handed to the roadmap detail template. It
// presents the roadmap's tasks and sprints and the relationships modelled
// in the data: sprint membership (with in-sprint order), the parent/subtask
// hierarchy, and dependency edges. It is read-only; nothing here is
// persisted.
type detailData struct {
	Name    string
	Tasks   []models.Task
	Sprints []sprintView
}

// sprintView pairs a sprint with the ordered task IDs that belong to it.
// The Tasks slice preserves sprint_tasks order (the in-sprint order shown
// on the detail page). Field order places the slice header before the
// embedded Sprint value to keep the pointer-scan prefix minimal
// (govet fieldalignment).
type sprintView struct {
	Tasks  []int
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

// loadDetail reads a roadmap's tasks and sprints read-only. It opens the
// roadmap database, reads every task (no status filter) and every sprint,
// and resolves each sprint's ordered membership. The database handle is
// released before the function returns; no row is written and no audit
// entry is produced (SPEC/WEB.md § Tasks and Sprints from SQLite).
//
// The caller is responsible for the {name} validation and existence check
// (resolveRoadmap); this function trusts name is a validated, existing
// roadmap.
func loadDetail(ctx context.Context, name string) (detailData, error) {
	database, err := db.OpenExisting(name)
	if err != nil {
		return detailData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	// Every task, any status: an unfiltered list with the maximum limit so
	// the page shows the whole roadmap. Task already carries depends_on,
	// blocks, subtask_count, and parent_task_id.
	tasks, err := database.ListTasks(ctx, &db.TaskListFilter{Limit: models.MaxTaskLimit})
	if err != nil {
		return detailData{}, err
	}

	sprints, err := database.ListSprints(ctx, nil)
	if err != nil {
		return detailData{}, err
	}

	views := make([]sprintView, 0, len(sprints))
	for _, sp := range sprints {
		taskIDs, terr := database.GetSprintTasks(ctx, sp.ID)
		if terr != nil {
			return detailData{}, terr
		}
		views = append(views, sprintView{Sprint: sp, Tasks: taskIDs})
	}

	return detailData{Name: name, Tasks: tasks, Sprints: views}, nil
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
