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

// taskView pairs one task with its comment log and, where the surface shows it,
// with the sprint the task belongs to. It is the context every surface that shows
// a task consumes: the board card and the sprint page's table row, and the
// read-only task detail modal both of them open, whose last block renders the
// comments as a chronological timeline (SPEC/WEB.md § Task Detail Modal, comments
// timeline).
//
// models.Task is EMBEDDED rather than a named field: html/template resolves
// promoted fields, so every card, row, and modal expression that reads a task's
// own fields ({{.ID}}, {{.Title}}, {{.CompletionSummary}}, ...) is unchanged by
// the addition of the comment log, and the timeline block reads {{.Comments}}.
//
// Comments is oldest first — created_at ascending, comment id ascending as the
// tie-breaker — exactly the order `rmp task comment-list` returns, because that
// order is what makes the log readable as one. A task with no comments carries a
// nil slice, which ranges as empty, so the template's empty-state branch needs no
// presence check (SPEC/DATABASE.md § List Comments for Many Parents (Grouped)).
//
// Sprint is the sprint the task belongs to, or nil when it belongs to none — a
// task belongs to at most one, which sprint_tasks.task_id's UNIQUE constraint
// guarantees. It is populated only by the surface that shows it, the tasks page's
// board cards; the sprint page leaves it nil, because that page renders one
// sprint and would gain a query for a value its markup never reads (SPEC/WEB.md
// § Roadmap Tasks Page, the sprint indicator).
//
// Field order places the pointer-bearing fields before the embedded struct to
// keep the pointer-scan prefix minimal (govet fieldalignment), as in sprintView.
type taskView struct {
	Comments []models.TaskComment
	Sprint   *db.SprintRef
	models.Task
}

// SpecialistsText returns the task's specialists as text, or the empty string
// when the task names none.
//
// It collapses the two shapes "no specialists" takes in the data — a NULL column,
// which reaches the view as a nil pointer, and a present but blank value — into
// the one the card's rule is written against: an indicator whose value is absent
// or empty renders nothing at all, no dash and no placeholder (SPEC/WEB.md
// § Roadmap Tasks Page, absent metadata renders nothing). Deciding it here rather
// than in the template keeps `{{with .SpecialistsText}}` correct for both shapes,
// where `{{with .Specialists}}` would render an empty indicator for the second.
func (v *taskView) SpecialistsText() string {
	if v.Specialists == nil {
		return ""
	}
	return strings.TrimSpace(*v.Specialists)
}

// HasMeta reports whether the card has at least one metadata indicator to show:
// its sprint, its specialists, its subtasks, its dependencies, the tasks it
// blocks, or its comments. A task with none of the six renders no metadata footer
// at all — not an empty one (SPEC/WEB.md § Roadmap Tasks Page, absent metadata
// renders nothing; Acceptance Criterion 85).
//
// The six conditions are exactly the six the footer's own items are rendered
// under, so the footer can never be emitted empty and can never swallow an
// indicator the card should show.
func (v *taskView) HasMeta() bool {
	return v.Sprint != nil ||
		v.SpecialistsText() != "" ||
		v.SubtaskCount > 0 ||
		len(v.DependsOn) > 0 ||
		len(v.Blocks) > 0 ||
		len(v.Comments) > 0
}

// taskColumn is one column of the roadmap tasks page's Kanban board: a task
// status, the cards of the tasks in that status, and the count its header shows.
//
// Tasks holds POINTERS into the page's flat task list rather than copies, so the
// board and the task detail modals it opens are rendered from one set of values
// and cannot drift apart. Count is len(Tasks), set where the column is built, so
// the header badge and the rendered cards are counted once from the same slice
// (SPEC/WEB.md § Roadmap Tasks Page, count per column; Acceptance Criterion 83).
//
// Field order puts the string before the slice so the pointer-scan prefix stops
// at the slice header rather than spanning the whole struct (govet
// fieldalignment).
type taskColumn struct {
	Status models.TaskStatus
	Tasks  []*taskView
	Count  int
}

// tasksData is the view model handed to the roadmap tasks template. It presents
// the roadmap's full task set — every task, any status — as a Kanban board of
// five fixed columns, one per models.TaskStatus, each card clickable to open the
// read-only task detail modal. It is read-only; nothing here is persisted
// (SPEC/WEB.md § Roadmap Tasks Page).
//
// Tasks is the full, unfiltered task list in the order the read returned it
// (priority DESC, created_at ASC), each task carrying its own comment log and its
// sprint. It is the single source of the page's task values: the board's columns
// point into it, and it is what the page ranges over to render exactly one task
// detail modal per task.
//
// Columns is that same list grouped into the board's five columns, in the order
// of the task state machine's flow. The grouping is in memory over the values
// already read: the board issues no query of its own, none per column and none
// per card (SPEC/WEB.md § Roadmap Tasks Page, read cost).
type tasksData struct {
	Name    string
	Chrome  chrome
	Tasks   []taskView
	Columns []taskColumn
}

// auditPageSize is the fixed number of audit entries shown per page on the
// read-only audit log page (SPEC/WEB.md § Roadmap Audit Log Page, pagination).
// It is well within the data layer's MaxAuditLimit hard cap (500), so a
// single-page request never exceeds that cap.
const auditPageSize = 100

// auditData is the view model handed to the roadmap audit log template. It
// presents one page of the roadmap's full audit log — every operation and
// entity type — ordered by performed_at DESC, with the read-only pagination
// footer state precomputed so the template stays declarative (SPEC/WEB.md
// § Roadmap Audit Log Page). It is read-only; reading the audit log writes no
// row and produces no new audit entry.
//
// Page and TotalPages are 1-based and clamped: Page is always in [1,
// TotalPages] and TotalPages is always at least 1 (even for an empty log), so
// the template can render "Page X of Y" and the Previous/Next controls without
// any further arithmetic. HasPrev is false on the first page and HasNext is
// false on the last page. PageItems is the precomputed ordered sequence of
// numbered-bar slots (page numbers, the active current page, and collapsed
// ellipses) the template renders for the numbered pagination bar, so the
// sliding-window-with-ellipsis rules live in one tested helper rather than in
// the template (SPEC/WEB.md § Roadmap Audit Log Page, sliding window with
// ellipsis).
type auditData struct {
	Name       string
	Chrome     chrome
	Entries    []models.AuditEntry
	PageItems  []pageItem
	Page       int
	TotalPages int
	PrevPage   int
	NextPage   int
	HasPrev    bool
	HasNext    bool
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
//
// Comments is the sprint's OWN comment log — the sprint's progression account —
// oldest first, rendered in the Comments card the sub-template places last. It
// never carries a member task's comments: those belong to that task's own detail
// modal, and the sprint level presents no aggregate of them (SPEC/WEB.md § Sprint
// Detail Sub-Template, Comments card scope; Acceptance Criterion 69).
type sprintDetail struct {
	Name     string
	Tasks    []taskView
	Comments []models.SprintComment
	Sprint   models.Sprint
	Summary  sprintCompletion
}

// sprintPageData is the view model handed to the roadmap sprint template. It
// presents a single sprint with all of its fields, its tasks in planned in-sprint
// execution order — each clickable to open the read-only task detail modal, and
// each carrying its own comment log — and the sprint's own comments (SPEC/WEB.md
// § Roadmap Sprint Page). It is read-only.
type sprintPageData struct {
	Name     string
	Chrome   chrome
	Tasks    []taskView
	Comments []models.SprintComment
	Sprint   models.Sprint
	Summary  sprintCompletion
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
	return sprintDetail{
		Name:     d.Name,
		Sprint:   d.Sprint,
		Tasks:    d.Tasks,
		Comments: d.Comments,
		Summary:  d.Summary,
	}
}

// graphView is the JSON shape returned by the graph data endpoint
// (SPEC/DATA_FORMATS.md § Graph View Data). nodes and edges are always
// present and never null; an empty graph returns empty arrays.
type graphView struct {
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

// taskCommentReader is the ONLY comment read a page that renders task detail
// modals is given: the grouped read over the whole set of rendered task ids, one
// statement for every task (SPEC/DATABASE.md § List Comments for Many Parents
// (Grouped)).
//
// The per-task listing (db.ListTaskComments) is deliberately absent from this
// interface. The page read path therefore cannot express the N+1 pattern
// SPEC/WEB.md § Task Detail Modal forbids — one query per rendered task — because
// the method that would do it is not reachable through the dependency it is
// handed. *db.DB satisfies the interface.
type taskCommentReader interface {
	ListTaskCommentsByTasks(ctx context.Context, taskIDs []int) (map[int][]models.TaskComment, error)
}

// taskSprintReader is the ONLY sprint read the tasks page is given: the grouped
// resolution over the whole set of rendered task ids, one statement for every
// task (SPEC/DATABASE.md § Resolve the Sprint of Many Tasks (Grouped)).
//
// The per-task and per-sprint reads (db.GetSprint, db.GetSprintTasks) are
// deliberately absent from this interface. The board's read path therefore cannot
// express the pattern SPEC/WEB.md § Roadmap Tasks Page forbids — one query per
// rendered card or per board column — because the methods that would do it are not
// reachable through the dependency it is handed. *db.DB satisfies the interface.
type taskSprintReader interface {
	GetSprintsByTasks(ctx context.Context, taskIDs []int) (map[int]db.SprintRef, error)
}

// tasksSource is the complete read surface of the roadmap tasks page: the full
// task list, the grouped comment read for the modals it renders, and the grouped
// sprint read for the sprint each card names. Naming it separates opening the
// database (loadTasks) from reading it (readTasks), so the page's queries can be
// counted against a real database (Acceptance Criteria 70 and 92).
type tasksSource interface {
	ListAllTasks(ctx context.Context) ([]models.Task, error)
	taskCommentReader
	taskSprintReader
}

// sprintTaskSource resolves a sprint's member tasks in the planned in-sprint
// execution order. Both the sprints landing page and the single sprint page read
// through it.
type sprintTaskSource interface {
	GetSprintTasksFull(ctx context.Context, sprintID int, status *models.TaskStatus, orderByPriority bool) ([]models.Task, error)
}

// sprintSource is the complete read surface of the single Roadmap Sprint Page:
// the sprint, its ordered member tasks, the grouped comment read for the task
// modals, and the sprint's own comments.
//
// The sprint comment read is the SINGLE-parent listing: there is deliberately no
// grouped multi-sprint read, because this page renders exactly one sprint
// (SPEC/DATABASE.md § List Comments for Many Parents (Grouped)).
type sprintSource interface {
	GetSprint(ctx context.Context, id int) (*models.Sprint, error)
	ListSprintComments(ctx context.Context, sprintID int, commentType *models.CommentType) ([]models.SprintComment, error)
	sprintTaskSource
	taskCommentReader
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

// loadTasks reads a roadmap's full task set read-only for the tasks page. It
// opens the roadmap database, reads every task (no status filter), the comments
// of every task, and the sprint of every task, and returns them grouped into the
// board's five columns (SPEC/WEB.md § Roadmap Tasks Page). The database handle is
// released before the function returns; no row is written and no audit entry is
// produced (SPEC/WEB.md § Tasks and Sprints from SQLite).
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

	return readTasks(ctx, database, name)
}

// readTasks is the tasks page's entire read, expressed against the page's read
// surface rather than a concrete connection. It is THREE reads and no more: the
// full task list UNBOUNDED, then the comments of every task the page renders in
// ONE grouped query, then the sprint of every task the page renders in ONE grouped
// query
// (SPEC/WEB.md § Roadmap Tasks Page, read cost; § Task Detail Modal, one grouped
// comment query, never N+1).
//
// A roadmap with no task costs ONE read: both grouped queries take the set of
// rendered task ids, and that set is empty, so both are skipped outright rather
// than issued against an empty IN list.
//
// Grouping the tasks into the board's five columns is done here, in memory, over
// the values already read: no query is issued per column and none per card, so
// the page's query count is independent of the number of tasks, of sprints, and
// of columns.
//
// Separating it from loadTasks is what makes the query count of a page render
// measurable against a real database: the caller supplies the source, so a test
// can hand in a counting one (Acceptance Criteria 70 and 92).
func readTasks(ctx context.Context, src tasksSource, name string) (tasksData, error) {
	// EVERY task of the roadmap, any status, with no limit and no pagination.
	// The board prints a count on each column header as a statement of fact about
	// the roadmap, so a partial read would publish wrong counts as true ones with
	// nothing on the page to reveal the omission: reading every row is what makes
	// those counts correct by construction, which is a correctness requirement
	// rather than a performance choice (SPEC/WEB.md § Roadmap Tasks Page,
	// Unbounded read; SPEC/DATABASE.md § Main SQL Queries, "List All").
	//
	// Task already carries depends_on, blocks, subtask_count, and parent_task_id.
	// The order is the read's own — priority DESC, created_at ASC — and the board
	// preserves it.
	tasks, err := src.ListAllTasks(ctx)
	if err != nil {
		return tasksData{}, err
	}

	views, err := newTaskViews(ctx, src, tasks)
	if err != nil {
		return tasksData{}, err
	}

	if err := attachSprints(ctx, src, views); err != nil {
		return tasksData{}, err
	}

	return tasksData{Name: name, Tasks: views, Columns: groupIntoColumns(views)}, nil
}

// attachSprints resolves the sprint of EVERY view in one grouped query over the
// whole set of rendered task ids — never one per card and never one per board
// column (SPEC/DATABASE.md § Resolve the Sprint of Many Tasks (Grouped);
// Acceptance Criterion 92).
//
// A page that renders no task issues no sprint query at all: the read is skipped
// outright rather than called with an empty id set (which db.GetSprintsByTasks
// would also answer without a statement).
//
// A task that belongs to no sprint is ABSENT from the grouped map, so its view
// keeps a nil Sprint and its card renders no sprint indicator — not a dash and
// not an empty slot.
func attachSprints(ctx context.Context, r taskSprintReader, views []taskView) error {
	if len(views) == 0 {
		return nil
	}

	ids := make([]int, len(views))
	for i := range views {
		ids[i] = views[i].ID
	}

	sprints, err := r.GetSprintsByTasks(ctx, ids)
	if err != nil {
		return err
	}

	for i := range views {
		if sprint, ok := sprints[views[i].ID]; ok {
			views[i].Sprint = &sprint
		}
	}
	return nil
}

// groupIntoColumns groups the page's task views into the board's five fixed
// columns, one per task status.
//
// The columns come from models.ValidTaskStatuses, which is both the set and the
// order the board needs: the five values of the TaskStatus enum, in the order of
// the task state machine's flow (BACKLOG, SPRINT, DOING, TESTING, COMPLETED).
// Taking them from the model rather than from a literal here or in the template
// is what makes the board's columns fixed — all five are built on every request,
// whatever the data holds, and an empty column is a built column with no card
// (SPEC/WEB.md § Roadmap Tasks Page, columns; Acceptance Criterion 81).
//
// The grouping is a single ordered pass, so the cards of one column keep the
// relative order the read returned them in — priority DESC, created_at ASC — and
// the board applies no sort of its own (Acceptance Criterion 84).
//
// Every task lands in exactly one column, because tasks.status is restricted by a
// CHECK constraint to exactly these five values (SPEC/DATABASE.md § tasks Table),
// so the board needs no sixth column and no "other" column, and the five counts
// sum to the roadmap's task count (Acceptance Criterion 82).
func groupIntoColumns(views []taskView) []taskColumn {
	columns := make([]taskColumn, len(models.ValidTaskStatuses))
	byStatus := make(map[models.TaskStatus]int, len(models.ValidTaskStatuses))
	for i, status := range models.ValidTaskStatuses {
		columns[i] = taskColumn{Status: status}
		byStatus[status] = i
	}

	for i := range views {
		if column, ok := byStatus[views[i].Status]; ok {
			columns[column].Tasks = append(columns[column].Tasks, &views[i])
		}
	}

	for i := range columns {
		columns[i].Count = len(columns[i].Tasks)
	}
	return columns
}

// newTaskViews pairs every task with its comment log, reading the comments of ALL
// the tasks in a SINGLE grouped query over the whole set of rendered task ids —
// never one query per task (SPEC/WEB.md § Task Detail Modal, one grouped comment
// query, never N+1; Acceptance Criterion 70).
//
// It is the one place a page resolves task comments, so both surfaces that render
// task detail modals — the tasks page and the sprint page — share the same single
// read and the same pairing rule.
//
// A page that renders no task issues no comment query at all: the read is skipped
// outright rather than called with an empty id set (which db.ListTaskCommentsByTasks
// would also answer without a statement).
//
// A task with no comment is ABSENT from the grouped map. Its zero value is a nil
// slice, which has length 0 and ranges as empty, so the pairing needs no presence
// check and the template's empty-state branch is reached naturally.
func newTaskViews(ctx context.Context, r taskCommentReader, tasks []models.Task) ([]taskView, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	ids := make([]int, len(tasks))
	for i := range tasks {
		ids[i] = tasks[i].ID
	}

	grouped, err := r.ListTaskCommentsByTasks(ctx, ids)
	if err != nil {
		return nil, err
	}

	views := make([]taskView, len(tasks))
	for i := range tasks {
		views[i] = taskView{Comments: grouped[tasks[i].ID], Task: tasks[i]}
	}
	return views, nil
}

// loadAudit reads one page of a roadmap's full audit log read-only for the
// audit log page. It opens the roadmap database, counts the total audit rows to
// compute the total page count, clamps the requested page into the valid range,
// reads exactly that page of entries ordered by performed_at DESC, and returns
// the precomputed pagination footer state (SPEC/WEB.md § Roadmap Audit Log
// Page). The database handle is released before the function returns; no row is
// written and no audit entry is produced (SPEC/WEB.md § Tasks and Sprints from
// SQLite).
//
// requestedPage is the already-parsed 1-based page (a non-integer or garbage
// page parameter is parsed to a sentinel by the handler; this function clamps
// any value, however out of range, into [1, totalPages]). Clamping happens
// AFTER the total is known, so a page beyond the last page resolves to the last
// page and a page below 1 resolves to 1. The page is never rejected: an
// out-of-range page renders successfully, never a 404 (SPEC/WEB.md § Roadmap
// Audit Log Page, pagination is clamped, not strict).
//
// The caller is responsible for the {name} validation and existence check
// (resolveRoadmap); this function trusts name is a validated, existing roadmap.
func loadAudit(ctx context.Context, name string, requestedPage int) (auditData, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return auditData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	total, err := database.CountAuditEntries(ctx)
	if err != nil {
		return auditData{}, err
	}

	// Total pages is ceil(total / pageSize), with a floor of 1 so an empty
	// audit log still renders "Page 1 of 1" (SPEC/WEB.md § Roadmap Audit Log
	// Page, empty state). Integer ceil without floats: (total + size - 1) / size.
	totalPages := (total + auditPageSize - 1) / auditPageSize
	if totalPages < 1 {
		totalPages = 1
	}

	// Clamp the requested page into [1, totalPages]. A value below 1 (including
	// the handler's parse-failure sentinel) clamps to 1; a value beyond the last
	// page clamps to the last page. The page is never rejected.
	page := requestedPage
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	entries, err := database.GetAuditEntries(ctx, &db.AuditFilter{
		Limit:  auditPageSize,
		Offset: (page - 1) * auditPageSize,
	})
	if err != nil {
		return auditData{}, err
	}

	return auditData{
		Name:       name,
		Entries:    entries,
		PageItems:  paginationItems(page, totalPages),
		Page:       page,
		TotalPages: totalPages,
		PrevPage:   page - 1,
		NextPage:   page + 1,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
	}, nil
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

	return readSprint(ctx, database, name, id)
}

// readSprint is the sprint page's entire read, expressed against the page's read
// surface rather than a concrete connection: the sprint, its member tasks in
// planned in-sprint execution order, the comments of every member task in ONE
// grouped query, and the sprint's OWN comments in one further query (SPEC/WEB.md
// § Roadmap Sprint Page; § Tasks and Sprints from SQLite, rule 1).
//
// The number of comment queries is therefore two, whatever the number of member
// tasks: it does not grow with the tasks shown (Acceptance Criterion 70).
func readSprint(ctx context.Context, src sprintSource, name string, id int) (sprintPageData, error) {
	sprint, err := src.GetSprint(ctx, id)
	if err != nil {
		return sprintPageData{}, err
	}

	orderedTasks, err := sprintOrderedTasks(ctx, src, sprint.ID)
	if err != nil {
		return sprintPageData{}, err
	}

	views, err := newTaskViews(ctx, src, orderedTasks)
	if err != nil {
		return sprintPageData{}, err
	}

	// The sprint's own comments: every one of them, oldest first, with no type
	// filter (nil) and no count limit, exactly as `rmp sprint comment-list`
	// returns them. This is the single-parent listing, not a grouped read: the
	// page renders one sprint (SPEC/WEB.md § Sprint Detail Sub-Template, Comments
	// card, order and completeness).
	comments, err := src.ListSprintComments(ctx, sprint.ID, nil)
	if err != nil {
		return sprintPageData{}, err
	}

	return sprintPageData{
		Name:     name,
		Sprint:   *sprint,
		Tasks:    views,
		Comments: comments,
		Summary:  newSprintCompletion(orderedTasks),
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
func sprintOrderedTasks(ctx context.Context, src sprintTaskSource, sprintID int) ([]models.Task, error) {
	return src.GetSprintTasksFull(ctx, sprintID, nil, false)
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
// The injected clause is separated from the query by a NEWLINE, never by a
// space. A query whose last line ends in a line comment ("MATCH (n) RETURN n //")
// swallows anything appended on the same line, so a space-separated injection
// landed INSIDE the comment and the limit was silently not applied: the endpoint
// then returned the whole graph, defeating the cap it exists to enforce (proven
// against a 252-node store, which returned all 252 nodes for "… RETURN n //"
// instead of the resolved 100). A newline terminates the comment, so the clause
// is always top-level and always applies. Cypher treats the newline as ordinary
// whitespace, so every query that worked before is unaffected.
func applyGraphLimit(query string, limit int) string {
	masked := cypherguard.MaskLiterals(query)
	if reTopLevelLimit.MatchString(masked) {
		return query
	}
	return query + "\nLIMIT " + strconv.Itoa(limit)
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
