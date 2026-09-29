package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/GoGraph/cypher/parser"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphjson"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
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
// § Graph Data Endpoint, query parameters; § Query-Bar Error Handling, rule 1).
var allowedGraphLimits = map[int]struct{}{
	50: {}, 100: {}, 250: {}, 500: {}, 1000: {}, 3000: {},
}

// reTopLevelLimit detects a top-level LIMIT clause on the masked normalization
// of a query. The endpoint injects its own LIMIT only when the user's query has
// none, so a user-authored LIMIT is respected as-is (SPEC/WEB.md § Graph Data
// Endpoint, node-limit injection). The check runs on the literal-masked query
// (maskLiterals), so a LIMIT keyword that appears only inside a string literal,
// comment, or backtick identifier does not count as an existing LIMIT and does
// not suppress injection.
var reTopLevelLimit = regexp.MustCompile(`(?i)\bLIMIT\b`)

// reTopLevelReturn detects the RETURN that makes a statement LIMITABLE. It is
// the whole of the general suppression rule, and it comes straight from the
// engine's grammar (GoGraph cypher/parser/grammar):
//
//	query           : regularQuery | standaloneCall
//	standaloneCall  : CALL invocationName parenExpressionChain? (YIELD ...)?
//	singlePartQ     : readingStatement* (returnSt | updatingStatement+ returnSt?)
//	projectionBody  : DISTINCT? projectionItems orderSt? skipSt? limitSt?
//
// A LIMIT attaches only to a projectionBody, and only a RETURN or a WITH carries
// one — so a statement that does not end in a projection admits no appended
// LIMIT at all. Measured against the engine: "CALL db.labels()\nLIMIT 100" fails
// with `cypher: parse: unexpected "LIMIT"`, and so does
// "CREATE (n:Probe {key:'p'})\nLIMIT 100"; whereas
// "CALL db.labels() YIELD label RETURN label\nLIMIT 1" runs and returns one row
// instead of two.
//
// **Suppressing a statement with no RETURN costs nothing, and that is why the
// rule can be this wide.** The node limit bounds the RESULT; a statement that
// projects nothing returns no row, so it contributes no node and no edge to the
// response whatever is appended to it. There is nothing for the limit to bound,
// and the only effect an injection could have on such a statement is to make it
// fail in the parser — which is exactly what a `CREATE` submitted through the
// query bar used to do (SPEC/WEB.md § Graph Data Endpoint, Suppression 2;
// Acceptance Criteria 47, 111 and 156).
//
// The rule subsumes the standalone procedure call the specification enumerates:
// a leading CALL with no top-level RETURN parses as standaloneCall and has no
// projectionBody, so it is suppressed by the same absence. A CALL that IS
// projected through a top-level RETURN is an ordinary limitable query and takes
// the injection exactly as a MATCH ... RETURN does, which is what keeps the
// endpoint from being stricter than the contract it publishes.
//
// It runs on the masked normalization (maskLiterals), so a RETURN keyword that
// appears only inside a string literal, a comment, or a backtick identifier does
// not make a non-projecting statement look limitable. It is a
// presence check rather than a full parse, and it errs towards judging a
// statement limitable: a statement wrongly judged limitable keeps the node cap
// it would otherwise escape, and fails in the parser only if it also turns out
// to carry no top-level projection — the exotic case being a RETURN buried in a
// subquery.
var reTopLevelReturn = regexp.MustCompile(`(?i)\bRETURN\b`)

// reIntrospect recognises the one form that admits no LIMIT clause even when it
// DOES carry a top-level projection: a
// schema-introspection command — SHOW INDEXES, SHOW INDEX, SHOW CONSTRAINTS or
// SHOW CONSTRAINT, with or without a YIELD, WHERE, or RETURN tail. The engine's
// SHOW parser rejects ORDER BY, SKIP and LIMIT on every one of those forms, so a
// tail does not make a LIMIT injectable (SPEC/WEB.md § Graph Data Endpoint,
// Suppression 2; Acceptance Criterion 111).
//
// The separator between the two keywords is a SINGLE SPACE, written as a literal
// space and deliberately not as \s+. That is the engine's own routing rule, not
// a rule of this endpoint's: a statement written with any other separator is not
// routed to the engine's schema parser and fails there as a syntax error, so
// injecting into it changes nothing about its outcome (SPEC/GRAPH.md § What
// Groadmap Does Not Check, item 7). Matching \s+ here would suppress the
// injection for a statement the engine is going to reject anyway, and would make
// this endpoint's LIMIT decision disagree with the engine about what a SHOW
// command is.
//
// This is the one place in the module that recognises the class. It moved here
// from the shared guard rail when the guard rail was withdrawn: nothing else
// classifies a statement by what it does any more, and what remains is a
// question about a STATEMENT FORM — "can it carry a LIMIT?" — which belongs
// beside the injection rule it serves. It refuses nothing.
var reIntrospect = regexp.MustCompile(`(?i)\A\s*SHOW (?:INDEX(?:ES)?|CONSTRAINTS?)\b`)

// graphQueryError classifies a query-bar failure so the handler can map it to a
// distinct, in-page message. The kinds are kept separate so the user understands
// what to fix, and the set of them is enumerated in exactly one place:
// SPEC/WEB.md § Query-Bar Error Handling, rule 4. This comment names that place
// instead of repeating the list, because a repeated list is what let the count
// here drift out of step with the constants below it.
type graphQueryError struct {
	// Reason is the user-facing message shown in place on the page.
	Reason string
	// Kind is the machine-readable failure class (see the graphErr* constants).
	Kind string
}

func (e *graphQueryError) Error() string { return e.Reason }

// Query-bar failure kinds. This block is the code side of the closed value set
// SPEC/WEB.md § Query-Bar Error Handling, rule 4, publishes and is canonical for;
// a value is added or removed there first, and no other comment in this file
// restates the list.
//
// Every one is answered with HTTP 400 and told apart by this field, not by the
// status: RFC 9110 section 15.5 puts the explanation of an error in the
// response representation, so one status serves them all and the kind carries
// the class. Splitting them across different statuses would assert a distinction
// HTTP does not carry, while the body already carries it precisely.
//
// A kind names the correction that clears the failure, and none of them is a
// verdict on what a statement does. The plan-prefix kind is decided before
// anything is sent, but it refuses a statement for the answer it asks for — a
// query plan the node-and-edge response has no place for — and the same statement
// written without its prefix is sent like any other (rule 12).
//
// Three further kinds were once published — not_read_only, schema_introspection
// and relationship_read_direction — and they were the classifications of a guard
// rail that examined what a statement does before running it. That guard rail is
// withdrawn: the endpoint refuses nothing on the ground of what the statement
// does, so there is no longer any verdict for those kinds to carry (SPEC/WEB.md
// § Graph Data Endpoint, "The statement is executed as written"). The four-deep
// precedence rule between them went with them; what survives is rule 5's
// ordering — the limit first, the prefix second — which is not a precedence
// between verdicts but the order the endpoint does its work in.
const (
	graphErrInvalidLimit = "invalid_limit" // limit not one of the six allowed values
	graphErrPlanPrefix   = "plan_prefix"   // the statement carries an EXPLAIN or PROFILE prefix
	graphErrExecution    = "execution"     // the statement failed once it was running
)

// graphPlanPrefixLine is the `error` of every plan-prefix refusal, exactly as
// SPEC/WEB.md § Query-Bar Error Handling, rule 12, publishes it. It names neither
// the prefix nor the statement, so it is the same for every refused request, and
// it is `rmp`'s own text because nothing ran to produce a diagnostic (rule 7).
const graphPlanPrefixLine = "query not run: the query bar cannot show a query plan; " +
	"remove the EXPLAIN or PROFILE prefix, or run the statement with rmp graph client"

// newGraphQueryError builds a classified query-bar error.
func newGraphQueryError(kind, reason string) *graphQueryError {
	return &graphQueryError{Kind: kind, Reason: reason}
}

// graphUnavailableError marks the one graph failure this endpoint answers HTTP
// 503 Service Unavailable for: the request never reached a statement because no
// graph server could be reached at all.
//
// It is the counterpart of graphQueryError rather than a variant of it, and the
// difference is what failed. A graphQueryError is a 400: the caller submitted
// something the graph refused or could not finish, so the response carries a
// `kind` naming the fault in the submission. Nothing the caller submitted is at
// fault here, so this carries no kind, its response body is the opaque
// `internal server error` every other server-side failure carries, and the line
// that names the socket goes to the log instead (SPEC/WEB.md § Query-Bar Error
// Handling, rule 6; § Knowledge Graph from the GoGraph Store, rules 1 and 2).
//
// **It is distinct from the OTHER 5xx this endpoint publishes, and the split is
// the whole point of the type.** A derived socket path over the platform's bound
// stays a 500 recorded at ERROR: no server can EVER listen there, so it is a
// permanent fault in the roadmap's layout rather than a dependency the operator
// has not started. That one is returned unwrapped and reaches the handler's
// internal-error branch. Answering both alike would either report a real defect
// as a transitory condition or emit an ERROR on every page load of a roadmap
// whose server is not running — which trains an operator to ignore the level that
// means something is broken (SPEC/WEB.md Acceptance Criteria 160 and 165).
type graphUnavailableError struct{ err error }

func (e *graphUnavailableError) Error() string { return e.err.Error() }

// Unwrap keeps utils.ErrGraphServer matchable through the wrapper, so the
// sentinel this endpoint classifies by is the sentinel `rmp graph client`
// classifies by (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 2).
func (e *graphUnavailableError) Unwrap() error { return e.err }

// newGraphUnavailable marks err as the no-server-reachable condition.
func newGraphUnavailable(err error) *graphUnavailableError {
	return &graphUnavailableError{err: err}
}

// asGraphUnavailable extracts a *graphUnavailableError from err, if err is one.
// It is errors.As rather than a type assertion so a wrapped one still classifies,
// matching asGraphQueryError.
func asGraphUnavailable(err error) (*graphUnavailableError, bool) {
	var ue *graphUnavailableError
	ok := errors.As(err, &ue)
	return ue, ok
}

// sprintsData is the view model handed to the roadmap sprints template (the
// roadmap's landing page). It presents the roadmap's sprints grouped into the
// three tabs (Próximos / Actual / Concluídos). It is read-only; nothing here is
// persisted. The sprints page does NOT render the full tasks table, and it
// carries no member task: every sprint is a card whose only derived value is the
// footer's total task count, which the sprint record itself already carries
// (SPEC/WEB.md § Roadmap Sprints Page).
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
// into an inline task table; the full sprint detail block lives only on the
// single Roadmap Sprint Page, and the sprints page links to no task page
// (SPEC/WEB.md § Shared Sprint-Card Partial;
// Acceptance Criteria 8/12/38).
type sprintsData struct {
	Name            string
	Chrome          chrome
	SprintsUpcoming []sprintView
	SprintsCurrent  []sprintView
	SprintsClosed   []sprintView
}

// taskView pairs one task with its comment count. It is the context the card of
// the sprint page's member-tasks board consumes, each card a link to the task's
// own page (SPEC/WEB.md § Sprint Detail Sub-Template; § Roadmap Task Page).
//
// models.Task is EMBEDDED rather than a named field: html/template resolves
// promoted fields, so every card expression that reads a task's own fields
// ({{.ID}}, {{.Title}}, ...) reads them directly.
//
// CommentCount is how many comments the task has, which is all a card shows. The
// comment TEXT is deliberately absent: it is read only by that task's own page,
// one task at a time, so the board never reads a comment body in order to display
// a number (SPEC/WEB.md § Sprint Detail Sub-Template, Read cost;
// SPEC/DATABASE.md § Count Comments for the Member Tasks of One Sprint (Grouped)).
type taskView struct {
	models.Task
	CommentCount int
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

// sprintView is one sprint as the Roadmap Sprints Page presents it: the sprint
// record and nothing else. The page renders every sprint as a card with no
// member tasks on it, so it holds no member-task slice and no completion
// summary — the card's only derived value is the footer count, and the sprint
// record already carries it as TaskCount (SPEC/WEB.md § Roadmap Sprints Page;
// § Tasks and Sprints from SQLite).
//
// The member tasks and the completion summary belong to the single Roadmap
// Sprint Page, which loads them into sprintPageData and renders them through the
// sprintDetail sub-template.
type sprintView struct {
	Sprint models.Sprint
}

// Card returns the context object the shared "sprintCard" partial consumes for
// one sprint on any tab of the Roadmap Sprints Page (SPEC/WEB.md § Shared
// Sprint-Card Partial). The roadmap Name is threaded through so the partial can
// build the card's link to the sprint's own page, and TaskCount is the sprint's
// own total member-task count rendered in the card footer.
//
// TaskCount is read from the sprint record rather than counted from a loaded
// member-task slice: every read that returns a Sprint populates it, the listing
// included, resolving the membership of all sprints in ONE grouped read
// (SPEC/MODELS.md § Sprint; SPEC/DATABASE.md § Read the Membership of Many
// Sprints (Grouped)). The page therefore pays nothing per sprint for the number
// its footer shows.
//
// The value receiver is deliberate: html/template invokes this method on a
// (copied) range element, and a pointer receiver would not be in the value's
// method set, so the template call would silently fail.
//
//nolint:gocritic // value receiver required by html/template (see comment above)
func (v sprintView) Card(name string) sprintCard {
	return sprintCard{Name: name, Sprint: v.Sprint, TaskCount: v.Sprint.TaskCount}
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

// sprintBoardColumn is one column of the Roadmap Sprint Page's member-tasks
// board: the heading it shows, the canonical status its count badge is coloured
// by, and the cards of the sprint's tasks it holds.
//
// Tasks holds POINTERS into the page's flat member-task list rather than copies,
// so the board is rendered from the one set of values the page read and cannot
// drift from it. There is no Count field: the column's
// badge is len(Tasks), because this board carries no narrowing control and so has
// no second notion of "how many are shown" to keep in step with a stored number.
//
// CanonicalStatus is the status the column's count badge takes its colour from: a
// column of this board groups a SET of statuses and writes none of them, so the
// status its colour is keyed on has to be named somewhere, and it is named in the
// model rather than in the template. The template hands it to taskStatusBadge, the
// same helper every task status badge takes its colour from, so the board reads
// the ONE semantic mapping instead of carrying colour literals that could drift
// from it (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Column header;
// § Status, Priority, and Severity Badge Colours, rule 2; Acceptance Criterion
// 140).
//
// BodyID is the id of the column's body — the element holding its cards or its
// empty state — which the column's collapse toggle names in aria-controls and
// static/sprint-board.js resolves to find the element it hides. It is taken from
// the column table below rather than derived in the template, so the three ids
// are fixed literals, unique within the page by construction (SPEC/WEB.md
// § Sprint Detail Sub-Template, rule 3, Column collapse; Acceptance Criterion
// 213).
//
// Field order puts the strings before the slice so the pointer-scan prefix stops
// at the slice header rather than spanning the whole struct (govet
// fieldalignment).
type sprintBoardColumn struct {
	Heading         string
	BodyID          string
	CanonicalStatus models.TaskStatus
	Tasks           []*taskView
}

// sprintBoardColumns is the board's three fixed columns, left to right, each
// pairing the heading it shows with the sprint-summary category it holds
// (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Three fixed columns).
//
// The category is models.TaskStatusCategory — the SAME categorisation
// models.CalculateSprintShowResult counts its Summary.Pending,
// Summary.InProgress, and Summary.Completed through
// (models.CategorizeTaskStatus). Naming the categories here rather than the
// statuses is what makes each column's badge equal one of those counters by
// construction instead of by coincidence: there is one mapping from status to
// bucket in the project, and both the board and the CLI sprint report read it
// (Acceptance Criterion 131).
//
// The headings are written exactly as the specification spells them, in upper
// case, and are not translated.
//
// The third field is the CANONICAL status of the group — the status a task is
// normally in at that stage of the sprint — and it is what the column's count
// badge is coloured by (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Column
// header; Acceptance Criterion 140). A task waiting in a sprint is normally a
// SPRINT task: a BACKLOG task inside a sprint is the exceptional case, the case of
// a task returned to the backlog without leaving the sprint, so SPRINT is the
// status WAITING stands for. The column named DOING takes the colour of the status
// named DOING, which is the reading a user expects and the only one that leaves
// the heading agreeing with the colour. CLOSED calls for no choice at all: it
// holds COMPLETED alone, and that status is its canonical one.
//
// Naming the status here, beside the category it belongs to, is what keeps the
// colour out of the template: the template reads this value through
// taskStatusBadge and writes no colour class of its own, so there is ONE mapping
// from a status to a badge variant in the project and this board reads it.
//
// The fourth field is the column's own ORDERING KEY: the timestamp the column's
// cards are ordered by, descending, read off the task the card presents
// (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Order within a column;
// Acceptance Criteria 14 and 132). The three columns do not share one order, so
// naming each column's key beside the column itself keeps the whole ordering rule
// in one table instead of spread over a switch elsewhere.
//
// WAITING names NO key, and that is the rule rather than an omission: WAITING is
// the queue of work not yet started, its order is the plan, and the plan is the
// sprint_tasks position order the page's read already returns — so the column is
// rendered exactly as it was read and is never sorted (see
// groupIntoSprintBoardColumns).
//
// DOING names started_at, and it names it for the WHOLE column, including the
// column's TESTING cards. tested_at orders nothing: the column groups DOING and
// TESTING, a task reaches TESTING only from DOING, and started_at records entry
// into DOING for both, so a TESTING card takes its place from when its task
// entered DOING and never from when it entered TESTING (SPEC/STATE_MACHINE.md
// § Date Tracking Fields).
//
// The fifth field is the id of the column's body, the element the column's
// collapse toggle controls. The specification fixes the three ids, so they are
// written out here in full, beside the heading they belong to, rather than
// assembled from it (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Column
// collapse; Acceptance Criterion 213).
var sprintBoardColumns = [...]struct {
	orderingTimestamp func(*models.Task) *string
	heading           string
	bodyID            string
	canonical         models.TaskStatus
	category          models.TaskStatusCategory
}{
	{nil, "WAITING", "sprint-board-column-waiting", models.StatusSprint, models.CategoryPending},
	{startedAt, "DOING", "sprint-board-column-doing", models.StatusDoing, models.CategoryInProgress},
	{closedAt, "CLOSED", "sprint-board-column-closed", models.StatusCompleted, models.CategoryCompleted},
}

// startedAt and closedAt are the two ordering keys sprintBoardColumns names. Each
// is a plain accessor, written out so the table above reads as a table and so the
// key a column is ordered by is a named thing rather than an inline literal.
//
// Both return the task's field as it is stored, nil included: a nil result IS the
// "absent timestamp" case the ordering rule is written against, and it is the
// caller's business, not theirs, to decide where an absent value sorts
// (MODELS.md § Task makes both fields nullable).
func startedAt(t *models.Task) *string { return t.StartedAt }

func closedAt(t *models.Task) *string { return t.ClosedAt }

// sprintDetail is the single context shape the "sprintDetail" sub-template
// renders. Only the single Roadmap Sprint Page builds one and hands it to the
// sub-template, so the full sprint detail block appears only there (SPEC/WEB.md
// § Sprint Detail Sub-Template; Acceptance Criterion 38).
//
// Columns is the sprint's member tasks, each carrying its own comment count,
// grouped into the member-tasks board's three fixed columns and ordered per
// column — WAITING keeping the planned in-sprint execution order (sprint_tasks
// position ascending), DOING and CLOSED reordered by started_at and closed_at
// descending — which is what the sub-template renders.
//
// Comments is the sprint's OWN comment log — the sprint's progression account —
// oldest first, rendered in the Comments card the sub-template places last. It
// never carries a member task's comments: those are shown on that task's own
// page, and the sprint level presents no aggregate of them (SPEC/WEB.md § Sprint
// Detail Sub-Template, Comments card scope; Acceptance Criterion 69). A board
// card shows the NUMBER of a member task's comments and never their text, which
// is read only by that task's own page, one task at a time.
type sprintDetail struct {
	Name     string
	Columns  []sprintBoardColumn
	Comments []models.SprintComment
	Sprint   models.Sprint
}

// sprintPageData is the view model handed to the roadmap sprint template. It
// presents a single sprint's details, its member tasks as a Kanban
// board of three fixed columns each ordered by its own key — each card a link to
// that task's own page — and the sprint's own comments
// (SPEC/WEB.md § Roadmap Sprint Page). It is read-only.
type sprintPageData struct {
	Name     string
	Chrome   chrome
	Tasks    []taskView
	Columns  []sprintBoardColumn
	Comments []models.SprintComment
	Sprint   models.Sprint
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
		Columns:  d.Columns,
		Comments: d.Comments,
	}
}

// graphView is the JSON shape returned by the graph data endpoint
// (SPEC/DATA_FORMATS.md § Graph View Data). nodes and edges are always
// present and never null; an empty graph returns empty arrays.
type graphView struct {
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

// taskCommentCounter is the ONLY task-comment read the sprint page is given: the
// grouped COUNT over the sprint's member tasks, one statement binding one
// parameter, the sprint id, for every card (SPEC/DATABASE.md § Count Comments for
// the Member Tasks of One Sprint (Grouped)).
//
// The per-task listing (db.ListTaskComments) is deliberately absent from this
// interface, and it is the only read that could bring a comment BODY onto this
// path. Its absence therefore carries two guarantees at once: the page cannot
// express the N+1 pattern SPEC/WEB.md forbids — one query per rendered task — and
// it cannot read comment text at all. The board shows a number, and a task's
// comment text is read only by that task's own page. *db.DB satisfies the
// interface.
type taskCommentCounter interface {
	CountTaskCommentsBySprint(ctx context.Context, sprintID int) (map[int]int, error)
}

// taskSprintReader is the grouped sprint resolution over a whole set of task ids,
// one statement for every task (SPEC/DATABASE.md § Resolve the Sprint of Many
// Tasks (Grouped)). The tasks page resolves the sprint of every row it renders
// through it, and the task page the sprint of its one task.
//
// The per-task and per-sprint reads (db.GetSprint, db.GetSprintTasks) are
// deliberately absent from this interface, so a page handed only this read cannot
// express one query per rendered row. *db.DB satisfies the interface.
type taskSprintReader interface {
	GetSprintsByTasks(ctx context.Context, taskIDs []int) (map[int]db.SprintRef, error)
}

// sprintTaskSource resolves a sprint's member tasks in the planned in-sprint
// execution order. It is the read surface of the single Roadmap Sprint Page,
// which is the only page that renders member tasks.
type sprintTaskSource interface {
	GetSprintTasksFull(ctx context.Context, sprintID int, status *models.TaskStatus, orderByPriority bool) ([]models.Task, error)
}

// sprintsSource is the complete read surface of the Roadmap Sprints Page: the
// roadmap's sprint listing, and nothing else. Naming it separates opening the
// database (loadSprints) from reading it (readSprints), so the page's queries
// can be counted against a real database.
//
// The listing alone is the whole surface because it already carries every value
// the page renders, the card footer's task count included: ListSprints resolves
// the membership of all the sprints it returns in ONE grouped read, so TaskCount
// is populated on every sprint it hands back (SPEC/MODELS.md § Sprint;
// SPEC/COMMANDS.md § List Sprints).
//
// The member-task read (db.GetSprintTasksFull) is deliberately absent: it is the read
// that would make the page's cost grow with the number of sprints, and the page
// renders no member task at all (SPEC/WEB.md § Tasks and Sprints from SQLite).
// Its absence means the sprints page cannot express that pattern through the
// dependency it is handed.
type sprintsSource interface {
	ListSprints(ctx context.Context, status *models.SprintStatus) ([]models.Sprint, error)
}

// sprintSource is the complete read surface of the single Roadmap Sprint Page:
// the sprint, its ordered member tasks, the grouped comment COUNT of those tasks,
// and the sprint's own comments.
//
// The page makes exactly TWO comment reads, whatever the number of member tasks:
// the sprint's own listing, which the Comments card renders in full as a log, and
// ONE grouped count over the sprint's member tasks, selected by the sprint id,
// which is what gives each board card its comment number. Neither grows with the member-task
// count (SPEC/WEB.md § Sprint Detail Sub-Template, Read cost; Acceptance Criteria
// 70 and 137).
//
// The per-task listing (db.ListTaskComments) is deliberately absent from this
// interface: it is the only read that could
// bring a comment BODY onto this path, so its absence carries two guarantees at
// once — the page cannot express the N+1 pattern SPEC/WEB.md forbids, one query
// per rendered card, and it cannot read comment text at all. A member task's
// comment text is read only by that task's own page, one task at a time.
//
// The sprint comment read is the SINGLE-parent listing: there is deliberately no
// grouped multi-sprint read, because this page renders exactly one sprint
// (SPEC/DATABASE.md § List Comments for Many Parents (Grouped)).
type sprintSource interface {
	GetSprint(ctx context.Context, id int) (*models.Sprint, error)
	ListSprintComments(ctx context.Context, sprintID int, commentType *models.CommentType) ([]models.SprintComment, error)
	sprintTaskSource
	taskCommentCounter
}

// loadRoadmapNames returns the names of all roadmaps under ~/.roadmaps/,
// using the same discovery rule the CLI uses (immediate subdirectories with
// a project.db). An empty result is not an error: the index renders an
// empty state.
func loadRoadmapNames() ([]string, error) {
	return utils.ListRoadmaps()
}

// loadSprints reads a roadmap's sprints read-only for the sprints landing page.
// It opens the roadmap database and hands it to readSprints, which performs the
// whole read. The database handle is released before the function returns; no
// row is written and no audit entry is produced (SPEC/WEB.md § Tasks and Sprints
// from SQLite).
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

	return readSprints(ctx, database, name)
}

// readSprints is the sprints page's entire read, expressed against the page's
// read surface rather than a concrete connection. It is ONE read and no more:
// the roadmap's sprint listing (SPEC/WEB.md § Tasks and Sprints from SQLite).
//
// The page reads no member task. Every sprint is rendered as a compact card with
// no member tasks on it, and the one derived value a card shows — the footer's
// total task count — comes from the sprint record the listing already returned,
// because ListSprints populates TaskCount for every sprint it returns, resolving
// the membership of all of them in ONE grouped read (SPEC/MODELS.md § Sprint;
// SPEC/DATABASE.md § Read the Membership of Many Sprints (Grouped)). Nothing is
// computed here that the sprints template does not render: the member tasks and
// the completion summary belong to the single Roadmap Sprint Page.
//
// Classifying the sprints into the three tabs is done here, in memory, over the
// values already read: no query is issued per tab and none per card, so the
// page's query count is independent of the number of sprints. It does NOT read
// the full task table either — the sprints page does not render it
// (SPEC/WEB.md § Roadmap Sprints Page).
//
// Separating it from loadSprints is what makes the query count of a page render
// measurable against a real database: the caller supplies the source, so a test
// can count what a render costs on a real roadmap.
func readSprints(ctx context.Context, src sprintsSource, name string) (sprintsData, error) {
	sprints, err := src.ListSprints(ctx, nil)
	if err != nil {
		return sprintsData{}, err
	}

	views := make([]sprintView, 0, len(sprints))
	for i := range sprints {
		views = append(views, sprintView{Sprint: sprints[i]})
	}

	upcoming, current, closed := classifySprints(views)
	return sprintsData{
		Name:            name,
		SprintsUpcoming: upcoming,
		SprintsCurrent:  current,
		SprintsClosed:   closed,
	}, nil
}

// newTaskViews wraps every task in the view the sprint board's cards consume. It
// performs no read: a view carries the task itself, and the comment count is
// attached by attachCommentCounts.
func newTaskViews(tasks []models.Task) []taskView {
	views := make([]taskView, len(tasks))
	for i := range tasks {
		views[i] = taskView{Task: tasks[i]}
	}
	return views
}

// attachCommentCounts reads the comment COUNT of EVERY view, the member tasks of
// sprint sprintID, in one grouped query over that sprint's members, selected by
// the sprint id rather than by a list of the member ids, so the statement binds
// one parameter whatever the number of cards — never one query per card, and
// never the comment bodies (SPEC/DATABASE.md § Count Comments for the Member
// Tasks of One Sprint (Grouped); SPEC/WEB.md Acceptance Criteria 70 and 137).
//
// A page that renders no task issues no comment query at all: the member-task
// read has already shown that there is no card to count for.
//
// A task with no comment is ABSENT from the grouped map, and the zero value a
// missing key yields is already the right count, so the pairing needs no presence
// check.
func attachCommentCounts(ctx context.Context, r taskCommentCounter, sprintID int, views []taskView) error {
	if len(views) == 0 {
		return nil
	}

	counts, err := r.CountTaskCommentsBySprint(ctx, sprintID)
	if err != nil {
		return err
	}

	for i := range views {
		views[i].CommentCount = counts[views[i].ID]
	}
	return nil
}

// taskPageSource is the complete read surface of the Roadmap Task Page: the task,
// that task's comments in full, the grouped sprint resolution over the set that
// holds the one task id, and — only when that resolution finds a sprint — the
// sprint itself and its member tasks in sprint_tasks position order (SPEC/WEB.md
// § Roadmap Task Page, Read cost).
//
// The comment read is the SINGLE-parent listing, deliberately: the page renders
// exactly one task, so there is no set of ids to group over. It is the only path
// on which the web interface reads comment TEXT for a task, which is what keeps
// the sprint board and the tasks page's list free of it. The grouped comment count is absent: the page shows no
// card of a member task, so it has no count to display. *db.DB satisfies the
// interface.
type taskPageSource interface {
	GetTask(ctx context.Context, id int) (*models.Task, error)
	ListTaskComments(ctx context.Context, taskID int, commentType *models.CommentType) ([]models.TaskComment, error)
	GetSprint(ctx context.Context, id int) (*models.Sprint, error)
	taskSprintReader
	sprintTaskSource
}

// taskPageData is the view model handed to the roadmap task template: one task,
// its comments oldest first, the context of the sprint it belongs to, and the four
// Markdown field cards (SPEC/WEB.md § Roadmap Task Page). It is read-only.
//
// Sprint is nil exactly when the task belongs to no sprint, which is what selects
// the backlog form of the Sprint card; membership, not status, decides it.
type taskPageData struct {
	Sprint   *taskSprintContext
	Name     string
	Chrome   chrome
	Fields   []taskFieldCard
	Comments []models.TaskComment
	Task     models.Task
}

// taskSprintContext is what the Sprint card of the task page shows about the
// sprint the task belongs to, and nothing more: the sprint record, the task's
// 1-based rank in the sprint's planned execution order, the number of member
// tasks, and how many of them are COMPLETED (SPEC/WEB.md § Roadmap Task Page,
// Sprint card).
type taskSprintContext struct {
	Sprint    models.Sprint
	Position  int
	Members   int
	Completed int
}

// taskFieldCard is one of the four Markdown field cards of the task page: the
// card title, and the renderer's HTML for the field, or Empty when the field is
// empty or null, in which case the card shows the em dash in place of the
// Markdown container (SPEC/WEB.md § Roadmap Task Page, Markdown field cards).
type taskFieldCard struct {
	Title string
	HTML  template.HTML
	Empty bool
}

// errTaskSprintInconsistent reports that the sprint the membership read named
// could not be read back as holding the task, which only a write racing the
// page's reads produces. It deliberately does not wrap utils.ErrNotFound: the
// task itself was found, so the answer is a 500 and never the task page's 404.
var errTaskSprintInconsistent = errors.New("the sprint of the task changed during the read")

// loadTask reads one task of a roadmap read-only for the Roadmap Task Page. It
// opens the roadmap database, reads, and releases the handle; no row is written
// and no audit entry is produced (SPEC/WEB.md § Tasks and Sprints from SQLite).
//
// The caller validates {name} and confirms it exists (resolveRoadmap) and parses
// {id} to an integer before calling. An id that is not a task of THIS roadmap
// yields utils.ErrNotFound, which the handler maps to 404: a task of another
// roadmap is not reachable through this roadmap's path space, because the read is
// scoped to this roadmap's own database file.
func loadTask(ctx context.Context, name string, id int) (taskPageData, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return taskPageData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	return readTask(ctx, database, name, id)
}

// readTask is the task page's entire read, expressed against its read surface
// rather than a concrete connection, so its query count can be measured against a
// real database (SPEC/WEB.md § Roadmap Task Page, Read cost; Acceptance Criterion
// 226):
//
//  1. the task, with subtask_count, depends_on, and blocks;
//  2. the task's comments, every one of them, oldest first, with no type filter;
//  3. the sprint the task belongs to, through the grouped resolution over the set
//     holding the one task id;
//  4. only when a sprint was found, that sprint and its member tasks in
//     sprint_tasks position order.
//
// That is at most five reads, and three for a task in no sprint, whatever the
// number of comments or of member tasks. The position and the progress are
// computed in memory over the member tasks already read.
func readTask(ctx context.Context, src taskPageSource, name string, id int) (taskPageData, error) {
	task, err := src.GetTask(ctx, id)
	if err != nil {
		return taskPageData{}, err
	}

	comments, err := src.ListTaskComments(ctx, id, nil)
	if err != nil {
		return taskPageData{}, err
	}

	refs, err := src.GetSprintsByTasks(ctx, []int{id})
	if err != nil {
		return taskPageData{}, err
	}

	var sprintContext *taskSprintContext
	if ref, ok := refs[id]; ok {
		sprintContext, err = readTaskSprintContext(ctx, src, ref.ID, id)
		if err != nil {
			return taskPageData{}, err
		}
	}

	fields, err := newTaskFieldCards(task)
	if err != nil {
		return taskPageData{}, err
	}

	return taskPageData{
		Name:     name,
		Task:     *task,
		Comments: comments,
		Sprint:   sprintContext,
		Fields:   fields,
	}, nil
}

// readTaskSprintContext reads the sprint a task belongs to and its member tasks,
// and derives the Sprint card's position and progress from them in memory.
//
// The position is the task's 1-based rank among the member tasks in sprint_tasks
// position ascending order — the order the read returns them in, and the order
// the WAITING column of the sprint page's board keeps — whatever the task's
// status. The progress counts the member tasks the project's one status
// categorisation places in the completed category, which is the count the CLOSED
// column of the sprint page's board carries (SPEC/WEB.md § Roadmap Task Page,
// Sprint card).
func readTaskSprintContext(ctx context.Context, src taskPageSource, sprintID, taskID int) (*taskSprintContext, error) {
	sprint, err := src.GetSprint(ctx, sprintID)
	if err != nil {
		if errors.Is(err, utils.ErrNotFound) {
			return nil, fmt.Errorf("%w: sprint %d is gone", errTaskSprintInconsistent, sprintID)
		}
		return nil, err
	}

	members, err := sprintOrderedTasks(ctx, src, sprintID)
	if err != nil {
		return nil, err
	}

	sc := &taskSprintContext{Sprint: *sprint, Members: len(members)}
	for i := range members {
		if members[i].ID == taskID {
			sc.Position = i + 1
		}
		if models.CategorizeTaskStatus(members[i].Status) == models.CategoryCompleted {
			sc.Completed++
		}
	}
	if sc.Position == 0 {
		return nil, fmt.Errorf("%w: task %d is not a member of sprint %d", errTaskSprintInconsistent, taskID, sprintID)
	}
	return sc, nil
}

// newTaskFieldCards renders the task's four Markdown fields, in the order the page
// presents them, through the one Markdown renderer in its ordinary form and with
// the footnote identifier prefix of each field (SPEC/WEB.md § Markdown Rendering,
// rule 12). An empty or null field renders nothing and is marked Empty, so its
// card shows the em dash in place of the Markdown container.
func newTaskFieldCards(task *models.Task) ([]taskFieldCard, error) {
	summary := ""
	if task.CompletionSummary != nil {
		summary = *task.CompletionSummary
	}
	sources := [...]struct {
		title  string
		field  string
		source string
	}{
		{"Functional requirements", "functional_requirements", task.FunctionalRequirements},
		{"Technical requirements", "technical_requirements", task.TechnicalRequirements},
		{"Acceptance criteria", "acceptance_criteria", task.AcceptanceCriteria},
		{"Completion summary", "completion_summary", summary},
	}

	cards := make([]taskFieldCard, len(sources))
	for i, s := range sources {
		cards[i].Title = s.title
		if s.source == "" {
			cards[i].Empty = true
			continue
		}
		html, err := trustedMarkdown(s.source, taskFieldIDPrefix(task.ID, s.field), markdownInteractive)
		if err != nil {
			return nil, err
		}
		cards[i].HTML = html
	}
	return cards, nil
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
// planned in-sprint execution order, the comment COUNT of every one of those
// tasks in ONE grouped query selected by the sprint id, and the sprint's OWN
// comments (SPEC/WEB.md
// § Roadmap Sprint Page; § Tasks and Sprints from SQLite, rule 1).
//
// TWO comment reads, and only two, whatever the number of member tasks: the
// sprint's own log, which the Comments card renders in full, and the grouped
// count that gives each board card its comment number. Neither grows with the
// member-task count, and the page reads no comment BODY for a task it renders —
// the text of a member task's comments is read only by that task's own page
// (Acceptance Criteria 70 and 137).
//
// A sprint with no member task costs one of those two: there is no card to count
// for, so the grouped count is skipped outright. The Comments card is always
// present, so the sprint's own listing is issued regardless.
//
// Grouping the member tasks into the board's three columns AND ordering each
// column by its own key is done here, in memory, over the rows already read: the
// read returns the rows in sprint_tasks position order and the board reorders two
// of the three columns afterwards, issuing no query per column and none per card,
// so the page's query count is independent of the number of member tasks and of
// columns (SPEC/WEB.md § Tasks and Sprints from SQLite).
func readSprint(ctx context.Context, src sprintSource, name string, id int) (sprintPageData, error) {
	sprint, err := src.GetSprint(ctx, id)
	if err != nil {
		return sprintPageData{}, err
	}

	orderedTasks, err := sprintOrderedTasks(ctx, src, sprint.ID)
	if err != nil {
		return sprintPageData{}, err
	}

	views := newTaskViews(orderedTasks)

	// The comment count of every rendered member task, in one grouped query over
	// the sprint's members, binding the sprint id alone (SPEC/DATABASE.md § Count
	// Comments for the Member Tasks of One Sprint (Grouped)).
	if err := attachCommentCounts(ctx, src, sprint.ID, views); err != nil {
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
		Columns:  groupIntoSprintBoardColumns(views),
		Comments: comments,
	}, nil
}

// groupIntoSprintBoardColumns groups a sprint's member-task views into the
// member-tasks board's three fixed columns — WAITING, DOING, CLOSED — in that
// order (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3; Acceptance Criteria
// 130 to 132).
//
// The bucket a task falls in comes from models.CategorizeTaskStatus, which is the
// project's ONE mapping from a task status to a sprint-summary category and is
// what models.CalculateSprintShowResult counts its Pending, InProgress, and
// Completed counters through. Reusing it, rather than writing a second
// status-to-column mapping here, is what makes each column's badge equal its
// counterpart counter by construction: there is a single categorisation, so the
// board and the CLI sprint report cannot come to disagree about which tasks are
// waiting, which are being worked on, and which are done (Acceptance Criterion
// 131).
//
// All three columns are built on every request, whatever the sprint holds, so an
// empty column is a built column with no card and a sprint with no member task
// renders an empty board rather than an absent one (Acceptance Criterion 130).
//
// EACH COLUMN THEN TAKES ITS OWN ORDER, because the three columns answer three
// different questions (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Order
// within a column; Acceptance Criteria 14 and 132):
//
//   - WAITING keeps the sprint_tasks position order, ascending — the plan, which
//     answers "which task do I develop next?". That is the order the page's read
//     already returns, so the column is left exactly as the grouping pass built it
//     and is not sorted at all.
//   - DOING is reordered by started_at descending — the most recently started task
//     first — which answers "what has just been picked up?".
//   - CLOSED is reordered by closed_at descending — the most recently closed task
//     first — which answers "what has just been finished?".
//
// started_at orders the WHOLE of the DOING column. That column groups DOING and
// TESTING, and a TESTING card takes its place from when its task entered DOING,
// never from when it entered TESTING: tested_at orders nothing on this board (see
// sprintBoardColumns).
//
// THE TIEBREAKER IS THE PLAN, AND IT COSTS NOTHING. Equal ordering timestamps are
// an ordinary case here — `rmp task stat` moves a batch of tasks in one operation
// and stamps them alike — and MODELS.md § Task makes both timestamps nullable, so
// a card may carry none at all. Both cases fall back to the sprint_tasks position
// order, ascending, and a card carrying no timestamp sorts LAST in its column. No
// position is compared to obtain that: the rows arrive from the read in position
// order, the grouping pass below preserves it, and sort.SliceStable keeps the
// relative order of every pair its comparison calls equal — which is exactly the
// pair the tiebreaker speaks about. Reading the position into the comparison would
// not merely be redundant, it would be unsound: sprint_tasks.position carries no
// uniqueness constraint (SPEC/DATABASE.md § `sprint_tasks` Table (1:N
// Relationship), whose DDL constrains sprint_id and task_id and leaves position
// free), so two member tasks may share one, and stability is what keeps such a
// pair in the order the read gave it.
//
// THE ORDERING COSTS NO READ. It is an in-memory reorder of the rows the page has
// already read: no query per column, no query per card, and no second read of any
// kind (SPEC/WEB.md § Tasks and Sprints from SQLite).
//
// Every member task lands in exactly one column: tasks.status is restricted by a
// CHECK constraint to the five values of the closed status enum
// (SPEC/DATABASE.md § tasks Table), each of which one of the three categories
// claims. models.CategoryOther is therefore unreachable from stored data; a view
// carrying it is placed in no column rather than in an invented fourth one.
func groupIntoSprintBoardColumns(views []taskView) []sprintBoardColumn {
	columns := make([]sprintBoardColumn, len(sprintBoardColumns))
	for i := range sprintBoardColumns {
		columns[i] = sprintBoardColumn{
			Heading:         sprintBoardColumns[i].heading,
			BodyID:          sprintBoardColumns[i].bodyID,
			CanonicalStatus: sprintBoardColumns[i].canonical,
		}
	}

	// One ordered pass over the views as the read returned them, so every column
	// starts out in the sprint_tasks position order — which is WAITING's final
	// order and the other two columns' tiebreaker.
	for i := range views {
		if column, ok := sprintBoardColumnOf(models.CategorizeTaskStatus(views[i].Status)); ok {
			columns[column].Tasks = append(columns[column].Tasks, &views[i])
		}
	}

	for i := range sprintBoardColumns {
		if key := sprintBoardColumns[i].orderingTimestamp; key != nil {
			sortByTimestampDescending(columns[i].Tasks, key)
		}
	}
	return columns
}

// sortByTimestampDescending orders a board column's cards by the timestamp key
// reads off each card's task: most recent first, and a card whose timestamp is
// absent last (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, The tiebreaker is
// the plan; Acceptance Criterion 132).
//
// The sort is STABLE and the comparison reads the timestamp and nothing else, so
// the cards the timestamp does not separate — two carrying the same instant, and
// the ones carrying none — keep the order they arrived in, which is the
// sprint_tasks position order the page read them in. That is the tiebreaker the
// rule calls for, obtained without carrying the position into the comparison.
//
// The timestamps are compared as strings, which is correct rather than merely
// convenient: every write of started_at and closed_at goes through
// utils.NowISO8601, whose format (utils.ISO8601Format, YYYY-MM-DDTHH:mm:ss.sssZ)
// is fixed-width, zero-padded, and always UTC, so the byte order of two such
// values is their chronological order. Comparing them directly therefore parses
// nothing, allocates nothing, and cannot fail on a value a parse would have to
// reject.
func sortByTimestampDescending(cards []*taskView, key func(*models.Task) *string) {
	// A column of fewer than two cards is already in order, and the guard is not
	// cosmetic: sort.SliceStable builds a reflect.Swapper before it looks at the
	// length, so sorting an empty or single-card column costs an allocation and
	// about 32ns to reach a conclusion that is free here. Both are ordinary
	// columns — a sprint whose work has not started renders an empty DOING and an
	// empty CLOSED (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Every column
	// is always rendered).
	if len(cards) < 2 {
		return
	}

	sort.SliceStable(cards, func(i, j int) bool {
		left, right := key(&cards[i].Task), key(&cards[j].Task)
		switch {
		case left == nil:
			// An absent timestamp is never above anything: it sorts after every
			// card that carries one, and stability leaves two absent cards in
			// position order.
			return false
		case right == nil:
			return true
		default:
			return *left > *right
		}
	})
}

// sprintBoardColumnOf returns the index of the column that holds a category, and
// false for a category no column claims (models.CategoryOther).
//
// The lookup is a scan of three entries rather than a map: at this size a linear
// scan over a contiguous array is both faster and allocation-free, and the array
// is the same value that fixes the columns' order.
func sprintBoardColumnOf(category models.TaskStatusCategory) (int, bool) {
	for i := range sprintBoardColumns {
		if sprintBoardColumns[i].category == category {
			return i, true
		}
	}
	return 0, false
}

// sprintOrderedTasks resolves a sprint's member tasks in the planned in-sprint
// execution order, which is the sprint_tasks position order (DATABASE.md
// § Relationships; the schema's sprint_tasks.position column and its
// idx_sprint_tasks_order index). db.GetSprintTasksFull with a nil status
// filter and orderByPriority=false returns the full task records ordered by
// st.position ASC, so each task carries its status, depends_on, blocks, and
// the rest of its fields for the sprint page and the task page — all
// without a second per-task query.
//
// The single Roadmap Sprint Page and the Roadmap Task Page, which derives its
// Sprint card from a sprint's member tasks, read through it. The sprints landing page
// does not: it renders every sprint as a card with no member tasks on it, so it
// would be paying a full member-task read per sprint for a number the sprint
// record already carries (SPEC/WEB.md § Tasks and Sprints from SQLite).
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
// § Query-Bar Error Handling, rule 1). The returned error is a classified
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

// planPrefixCheck is the step loadGraphView takes between resolving the limit
// and resolving a graph server, and in production it is refusePlanPrefix and
// nothing else. It is a variable only so that SPEC/WEB.md Acceptance Criterion
// 168 can drive the endpoint with the prefix recognition left out of its path and
// compare the bytes of the two answers; no production code assigns it.
var planPrefixCheck = refusePlanPrefix

// refusePlanPrefix refuses a statement the pinned engine's parser reports as
// carrying an EXPLAIN or PROFILE prefix, and returns nil for every other
// statement (SPEC/WEB.md § Query-Bar Error Handling, rule 12).
//
// **The endpoint's response has no place for a plan.** It carries nodes and
// edges and nothing else, so a sent EXPLAIN would render as an empty graph that
// cannot be told from a statement that matched nothing, and a sent PROFILE would
// render its rows and discard the measurement the caller asked for.
//
// **Recognition is the engine's.** The prefix belongs to the engine's grammar, so
// the question is put to the engine's own statement parser, the one the engine
// consults to decide whether a statement carries a prefix, and this function
// scans for no token of its own. Case, whitespace and a comment ahead of the
// prefix count exactly as that grammar counts them, and `MATCH (explain) RETURN
// explain`, where the word is an identifier, is a plain statement to it.
//
// **The parse is a question and nothing else.** Its tree is discarded, and the
// statement the endpoint sends is the one it resolved, never text derived from
// the parse. Text the parser cannot parse is not refused here: it is sent, and
// the engine's own diagnostic reaches the caller as an execution failure, which
// is the path it took before this refusal existed.
//
// **It refuses nothing on the ground of what a statement does.** The same
// statement without its prefix is sent like any other, so the refusal withdraws
// no write and is no security control (§ Security and Constraints, rule 3).
//
// **Its cost is the parse's, paid before any server is resolved.** On the
// development machine the default query parsed in 39 µs and 22 KB. The costliest
// input found at the parser's own 1 MiB ceiling, a flat list of integers, parsed
// in 2.44 s to 2.72 s and allocated 1.46 GiB; the parser rejects anything longer in one
// linear pass before it builds a tree, and ParseStatement takes no context, so
// that time is spent whatever the client does meanwhile.
func refusePlanPrefix(statement string) error {
	_, mode, err := parser.ParseStatement(statement)
	if err != nil || mode == parser.PlanModeNone {
		return nil
	}
	return newGraphQueryError(graphErrPlanPrefix, graphPlanPrefixLine)
}

// applyGraphLimit appends a top-level LIMIT clause to query, and is the single
// place the node-limit injection rule lives (SPEC/WEB.md § Graph Data Endpoint,
// node-limit injection). Injection is suppressed in exactly two cases and
// applies in every other one:
//
//   - Suppression 1: the query already carries a top-level LIMIT. The
//     user-authored LIMIT is respected as-is and the resolved dropdown value is
//     not applied.
//   - Suppression 2: the query is a statement form that admits NO LIMIT clause
//     at all — one carrying no top-level RETURN (the standalone procedure call
//     and every write with no projection), or a schema-introspection command.
//     Appending a LIMIT to one bounds nothing; it makes the statement fail in the
//     PARSER, so a statement that `rmp graph client` sends would be unusable
//     through this endpoint and the endpoint would be stricter than the contract
//     it publishes. Both forms are executed as the caller wrote them
//     (SPEC/WEB.md § Graph Data Endpoint, Suppression 2).
//
// Both suppression checks run on the literal-masked normalization
// (maskLiterals), so a LIMIT, SHOW, CALL, or RETURN keyword that appears only
// inside a string literal, a comment, or a backtick identifier does not affect
// the decision, and both forms of Suppression 2 are anchored to the
// start of the statement. A suppressed query is not bounded by the node limit;
// it remains bounded by the per-request query time budget, which applies to
// every query the endpoint executes (SPEC/WEB.md § Graph Query Time Budget).
//
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
	masked := maskLiterals(query)

	// Suppression 1: the caller wrote their own top-level LIMIT.
	if reTopLevelLimit.MatchString(masked) {
		return query
	}
	// Suppression 2: a statement form that cannot carry a LIMIT clause.
	if !admitsLimitClause(masked) {
		return query
	}
	return query + "\nLIMIT " + strconv.Itoa(limit)
}

// admitsLimitClause reports whether a statement is a form that can carry a
// top-level LIMIT clause. masked MUST be maskLiterals(query); it is
// passed in rather than recomputed because the caller already holds it.
//
// This is a SYNTAX question — "can this statement carry a LIMIT?" — and it is
// one of the two questions this endpoint asks about a statement; the other,
// whether the engine's parser reports an EXPLAIN or PROFILE prefix, is
// refusePlanPrefix's. It is not a read-only question and not a safety question:
// the endpoint refuses nothing on the ground of what a statement does, and both
// forms below are executed exactly as the caller wrote them. What the answer
// decides is whether five more characters are appended (SPEC/WEB.md § Graph Data
// Endpoint, Suppression 2).
//
// Two forms admit no LIMIT:
//
//  1. A statement carrying no top-level RETURN. A LIMIT attaches only to a
//     projection, so a statement that ends in anything else admits none. This
//     covers the standalone procedure call the specification enumerates, and it
//     covers the write with no RETURN — a CREATE, a SET, a DETACH DELETE, a
//     schema DDL statement — which the endpoint could not execute at all while
//     it injected into one. See reTopLevelReturn for the grammar this rests on
//     and for why suppressing such a statement costs nothing.
//
//  2. A schema-introspection command — the SHOW INDEXES / SHOW INDEX /
//     SHOW CONSTRAINTS / SHOW CONSTRAINT class, INCLUDING one carrying a YIELD,
//     WHERE, or RETURN tail: the engine's SHOW parser rejects ORDER BY, SKIP, and
//     LIMIT on every one of those forms, so a projection does not make a LIMIT
//     injectable and rule 1 alone would not catch it. Recognition is
//     reIntrospect's and is exactly this class and nothing wider: every other
//     SHOW (SHOW DATABASES, SHOW FUNCTIONS, SHOW PROCEDURES, ...) is outside it.
//
// Each matcher is applied to the masked text AND to its upper-cased copy, which
// is the transformation the engine itself falls back to when a non-ASCII byte
// appears in the keyword window. Unicode uppercasing maps some non-ASCII letters
// onto ASCII ones, so a statement spelled with U+0131 (dotless i) routes to the
// engine's schema parser while a plain case-insensitive match would miss it. The
// consequence of missing it is no longer a security one — nothing is refused
// here — but it would be an injection into a statement that cannot carry the
// clause, which is the outcome this function exists to prevent.
func admitsLimitClause(masked string) bool {
	upper := strings.ToUpper(masked)
	matches := func(re *regexp.Regexp) bool {
		return re.MatchString(masked) || re.MatchString(upper)
	}
	if !matches(reTopLevelReturn) {
		return false
	}
	return !matches(reIntrospect)
}

// loadGraphView sends the caller's statement to the roadmap's graph server and
// returns the nodes and edges it produced, in the Graph View Data shape.
//
// **This endpoint reaches a graph through the client mechanism and through
// nothing else.** It resolves the roadmap's derived socket, sends the statement
// to the `rmp graph serve` process listening there, and reads the result back
// over the protocol. It opens no graph store, takes no advisory lock, constructs
// no engine, and has no second route in (SPEC/WEB.md § Knowledge Graph from the
// GoGraph Store, rule 1).
//
// The direct path this function used to fall back to — resolve the socket, and
// on finding nothing there open the store, take its exclusive lock, run the
// statement through an engine of this process's own and checkpoint — is gone,
// along with the resolution logic that chose between the two routes. A roadmap
// nothing is serving is now reported as a graph that cannot be reached, HTTP 503,
// rather than read from disk. Two things follow, and both are the point of the
// change rather than a cost of it:
//
//   - **A request leaves nothing at all on disk on this process's account.**
//     Opening a store runs GoGraph's recovery, which repairs an interrupted
//     checkpoint before it loads anything, and that repair used to be reachable
//     from a web request that wrote nothing. It is not reachable now: no store is
//     opened, so no recovery runs, no write.lock appears, and no graph/ directory
//     is created (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 6).
//   - **No request waits for the store's lock.** A server holds that lock for its
//     whole process lifetime, and no finite wait can be sized against such a
//     hold; this process never takes it, so no request waits on it (rule 7).
//
// It uses the client MECHANISM and not the client COMMAND: it calls
// internal/graphclient in this process and MUST NOT spawn `rmp graph client` as a
// child. A subprocess would put a process boundary, an argument-quoting layer, an
// exit code and a second copy of the output serialisation between this endpoint
// and the answer it owes, and would make the endpoint's behaviour depend on which
// binary is on a path rather than on the code it was built from (rule 3;
// SPEC/ARCHITECTURE.md § 9. internal/graphclient/ and reaching a graph server).
//
// **The statement is sent as written, and may write.** What it does is not
// examined. A CREATE, a SET, a DETACH DELETE or a schema DDL submitted through the
// query bar is executed and committed — in the server, which is the only process
// that holds the graph open — over HTTP, with no authentication: the interface's
// principal security property, stated in full in SPEC/WEB.md § Security and
// Constraints, rule 3, and granted there deliberately. The one statement not sent
// is one the engine's parser reports as carrying an EXPLAIN or PROFILE prefix,
// which is refused for the answer it asks for and not for what it does (see
// refusePlanPrefix).
//
// rawQuery and rawLimit are the request's q and limit URL parameters (empty when
// absent). The query is resolved (the default query when absent) and has a LIMIT
// injected only when it has no top-level LIMIT of its own AND is a statement form
// that admits a LIMIT clause. Two things can reject a request before a statement
// is sent, in this order: an invalid limit, and then a statement carrying a plan
// prefix. Each is returned as a classified graphQueryError for which no graph
// server is resolved and nothing is sent (SPEC/WEB.md § Query-Bar Error Handling,
// rules 5 and 12).
func loadGraphView(ctx context.Context, name, rawQuery, rawLimit string) (graphView, error) {
	// Resolve and validate the limit first; an invalid limit rejects the
	// request before the statement is sent and before the socket is probed
	// (SPEC/WEB.md § Query-Bar Error Handling, rules 1 and 5).
	limit, err := resolveGraphLimit(rawLimit)
	if err != nil {
		return graphView{}, err
	}
	query := resolveGraphQuery(rawQuery)

	// Examine the prefix second: after the limit was accepted, and before the
	// socket path is derived, checked against the platform's bound, or probed, so
	// a refusal is the same 400 whether or not a server could ever be reached. The
	// parse is of the resolved statement, before any LIMIT is appended, and the
	// statement sent below is that same resolved statement (SPEC/WEB.md
	// § Query-Bar Error Handling, rule 12).
	if refusal := planPrefixCheck(query); refusal != nil {
		return graphView{}, refusal
	}

	// Resolution runs once per request and its outcome is not cached: a cached
	// outcome would act on a server that had since stopped (SPEC/WEB.md
	// § Knowledge Graph from the GoGraph Store, rule 1).
	//
	// The socket is the DERIVED one and nothing can point it elsewhere. Both
	// rmp graph subcommands take a --socket flag; this endpoint takes none,
	// accepts no request parameter carrying a path, and has no command line to
	// receive one — rmp web serves every roadmap at once rather than one. A
	// server started on a non-default socket is therefore invisible here, and
	// SPEC/GRAPH.md § Serving on a Non-Default Socket is canonical for that
	// boundary and states plainly that no flag closes it.
	socket, err := resolveGraphServerForRequest(ctx, name)
	if err != nil {
		return graphView{}, err
	}

	// The statement crosses unchanged apart from the node-LIMIT injection
	// (SPEC/GRAPH.md § Server Resolution, rule 5).
	return servedGraphView(ctx, socket, applyGraphLimit(query, limit))
}

// resolveGraphServerForRequest probes the roadmap's derived socket and reports
// the socket path when a server is answering there.
//
// The rule and the probe are internal/graphclient's — the ONE realisation
// SPEC/ARCHITECTURE.md module 9 fixes and the one every surface follows. What
// this function adds is the outcome THIS surface reports for each failing state,
// because a status code is the web's own business, and the two failing outcomes
// are deliberately NOT the same one (SPEC/WEB.md § Knowledge Graph from the
// GoGraph Store, rule 1).
//
// **No server listening, or none reachable through a socket that answered: 503,
// at WARN.** RFC 9110, Section 15.6.4, defines 503 for a server "currently unable
// to handle the request due to a temporary overload or scheduled maintenance,
// which will likely be alleviated after some delay". A graph server is exactly
// that kind of dependency: this web server is working correctly, the roadmap and
// its database are readable, every other route is served, and the one thing
// missing is a process the operator starts with `rmp graph serve`. Starting it
// clears the condition, with no change to this server and none to the request.
// 500 was the alternative and is refused: it asserts that THIS server failed,
// which conflates a defect in the product with a configuration the operator has
// simply not set up yet, and would send an operator looking for a fault where
// there is none. No Retry-After is sent: RFC 9110 permits one and does not
// require it, and nothing in the product knows when an operator will start a
// server, so a header carrying a guess would be worse than its absence.
//
// A socket file a killed server left behind is the FIRST of those two states and
// is not distinguished from an absent one: for this endpoint the two are one
// condition, there is nothing to send the statement to, and the leftover file is
// neither an error nor removed here.
//
// **A derived path longer than the platform allows: 500, at ERROR, and it is
// settled here, before the probe.** No process can bind such a path, so probing
// it could tell this handler nothing it does not already know — and what it knows
// is not "no server happens to be listening". It is that NONE CAN EVER LISTEN
// THERE, which is a different fact and is not served by the answer the first one
// gets. 503 is refused for it precisely because no delay alleviates it and no
// operator action short of moving or renaming the roadmap changes it, so
// announcing a service that will come back would be false and would tell the
// operator to start a server that can never bind (SPEC/GRAPH.md § Socket Path
// Length, rules 5 and 6; § Server Resolution, rule 12; SPEC/WEB.md Acceptance
// Criteria 160 and 165).
//
// The cost of that permanence is stated rather than avoided: this endpoint
// publishes no --socket flag and has nowhere to receive one, so unlike the
// command line it cannot be pointed at a shorter path. The graph page still
// renders and every fetch it makes for this roadmap is refused, for as long as
// the derived path is what it is; the only remedy is a shorter home directory or
// a shorter roadmap name.
//
// Neither outcome is a graphQueryError: those are the 400s, and they carry a
// kind because a statement of the caller's failed. Neither of these requests
// reached a statement.
func resolveGraphServerForRequest(ctx context.Context, name string) (string, error) {
	socket, err := graphclient.SocketPath(name)
	if err != nil {
		return "", err
	}
	if graphclient.SocketPathTooLong(socket) {
		return "", fmt.Errorf("%w: the socket path derived for roadmap %q cannot be bound: %s is "+
			"%d bytes and this platform allows at most %d, so no graph server can ever listen there",
			utils.ErrGraphServer, name, socket, len(socket), graphclient.MaxSocketPathLen)
	}
	state, probeErr := graphclient.Resolve(ctx, socket)
	if state.Served() {
		return socket, nil
	}
	// **A probe the REQUEST's own cancellation stopped is not evidence about the
	// server**, and reporting it as one is the defect this branch closes.
	//
	// The probe runs under the request's context, so a client that disconnects
	// before it completes fails the dial — and every failed dial otherwise means
	// "nothing is listening". The two are opposite conditions with opposite
	// remedies: one is a dependency the operator starts, the other is a caller
	// that went away, and a server may well be listening throughout the second.
	// Left undistinguished, an ordinary disconnect produced a WARN record reading
	// "graph server unavailable" and naming a socket that was in fact being
	// served, which is the one record an operator is meant to act on
	// (SPEC/WEB.md Acceptance Criteria 164 and 165).
	//
	// The parent is consulted rather than the probe's own error, which is the same
	// disambiguation graphExecutionError performs one layer down for the same pair
	// of causes, and it is classified the same way it is there: an execution
	// failure, HTTP 400, no new kind and no new status. Nothing reads that body —
	// the caller has gone — so what the classification decides is the record.
	if ctx.Err() != nil {
		return "", newGraphQueryError(graphErrExecution,
			"query failed to execute: the request was cancelled before the graph server could be reached")
	}
	if state.NotServed() {
		return "", newGraphUnavailable(graphNoServerListening(socket))
	}
	return "", newGraphUnavailable(graphServerUnreachable(socket, probeErr))
}

// graphNoServerListening is the line this endpoint records for a roadmap nothing
// is serving.
//
// It is deliberately the line `rmp graph client` writes for the same condition,
// down to the sentinel: the failure the CLI reports and the failure this endpoint
// reports ARE the same failure, met through the same client, and they must be
// classified the same way (SPEC/WEB.md § Knowledge Graph from the GoGraph Store,
// rule 2; SPEC/COMMANDS.md § Graph Server Socket Error Lines, the no-server row).
//
// Where the two legitimately differ is what the CALLER is shown, and only that. A
// CLI caller is the operator, so the line goes to stderr and the process exits 1.
// An HTTP caller is a browser that may not be the operator's, so this line never
// enters the response body — it names a filesystem path inside the operator's
// home directory — and goes to the server's log instead, where the operator reads
// it (SPEC/WEB.md § Record Content, rule 6; Acceptance Criterion 164).
func graphNoServerListening(socket string) error {
	return fmt.Errorf("%w: no graph server is listening on %s", utils.ErrGraphServer, socket)
}

// graphServerUnreachable is the line for a socket that answered and yielded no
// server: the connection was accepted but the handshake did not complete inside
// the probe, or it failed for a reason other than the socket being absent or
// refusing (SPEC/GRAPH.md § Server Resolution, the Unreachable state).
//
// It is reported apart from the no-server line because the two call for different
// actions, and it is a FAILURE and never a fall back. A socket that answers may
// belong to a server holding the store's lock for its process lifetime, so a
// caller that opened the store on this observation would wait its whole wait
// budget and then fail — which is the outcome resolution exists to prevent. This
// process has no way to open a store in any case.
func graphServerUnreachable(socket string, cause error) error {
	return fmt.Errorf("%w: graph server unreachable at %s: %v", utils.ErrGraphServer, socket, cause)
}

// servedGraphView sends the statement to a running server and assembles the
// Graph View Data shape from what comes back.
//
// The shared Bolt client hands back expr.Value rather than JSON, precisely so
// that a result which crossed the protocol is mapped back onto the ENGINE's value
// model and not onto a second JSON representation: the extraction, the
// deduplication, the orphan-edge dropping and the property mapping are then one
// implementation, shared with `rmp graph client`, rather than two that could come
// to disagree (SPEC/DATA_FORMATS.md § Graph Client Result; § Graph element
// mapping — two surfaces, one realisation).
//
// It applies no deadline of its own. graphclient.Send already keeps the caller's
// backstop — the wait budget, statement budget plus the retry policy's total, and
// deliberately not the statement budget itself — so a server that answers nothing
// cannot hold this request for ever, and a statement that committed just before
// the budget expired is not reported as one that wrote nothing
// (SPEC/GRAPH.md § Server Resolution, rule 7).
//
// Nothing is checkpointed here and no lock is released, because this process
// opened nothing: a statement that wrote committed in the server, which
// checkpoints on its own cadence and at its own shutdown
// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process).
func servedGraphView(ctx context.Context, socket, query string) (graphView, error) {
	result, err := graphclient.Send(ctx, socket, query)
	if err != nil {
		return graphView{}, servedGraphError(ctx, socket, err)
	}

	c := newGraphCollector()
	for _, row := range result.Rows {
		for _, cell := range row {
			c.walk(cell)
		}
	}
	return c.view(), nil
}

// servedGraphError words a failure the shared Bolt client classified in the terms
// this surface answers in.
//
// One of the six outcomes is NOT an execution failure and must not be answered
// 400. A server that could not be reached between the probe and the send is the
// no-server-reachable condition, answered 503 at WARN like the resolution states
// that report it — the same condition, met a moment later. Every other outcome
// surfaced once the statement was running, which is where SPEC/WEB.md
// § Query-Bar Error Handling, rule 6, already draws the boundary, so each is the
// single execution kind and 400.
//
// A connection lost or unanswered after the statement was sent is NOT re-sent and
// NOT retried anywhere else, and this function's job is to make that structural
// rather than remembered: it returns an error, and the only caller returns it.
// The statement may already have committed on the server — a commit is durable
// before it is acknowledged — and this process has no store to re-run it against
// in any case (SPEC/GRAPH.md § Server Resolution, rules 4 and 7).
func servedGraphError(ctx context.Context, socket string, err error) error {
	var sendErr *graphclient.SendError
	if !errors.As(err, &sendErr) {
		return fmt.Errorf("%w: graph store unavailable: %v", utils.ErrGraphStore, err)
	}

	switch sendErr.Kind {
	case graphclient.FailureUnreachable:
		return newGraphUnavailable(graphServerUnreachable(socket, sendErr.Cause))
	case graphclient.FailureLost:
		return newGraphQueryError(graphErrExecution,
			"query failed to execute: the connection to the graph server was lost; the statement's outcome is unknown")
	case graphclient.FailureUnanswered:
		return newGraphQueryError(graphErrExecution,
			"query failed to execute: the graph server did not answer; the statement's outcome is unknown")
	case graphclient.FailureConflict:
		// Contention inside the server, and the one execution failure whose text
		// is `rmp`'s rather than an engine's — because the whole reason the line
		// exists is that the engine's diagnostic is what a reader cannot tell
		// apart from an invalid statement, and the query bar's user faces exactly
		// the decision the CLI's user faces: run it again, or correct it
		// (SPEC/WEB.md § Query-Bar Error Handling, rule 11).
		//
		// It is a 400 with the execution kind like every other failure that
		// surfaced once the statement was running (rule 6), and rule 4 weighs and
		// refuses the alternatives: 409 describes a conflict of state that
		// survives for the user to resolve, and by the time this is answered the
		// loser has rolled back whole and no such conflict is left; 503 would
		// announce a service that is unavailable, which a server that ran the
		// statement and went on serving is not.
		//
		// **This endpoint DOES publish 503, and the contrast is exactly why that
		// argument still stands.** It publishes it when there is no graph server
		// to reach at all — a dependency an operator starts, which delay does
		// clear (see resolveGraphServerForRequest). The test is the same one, and
		// this condition fails it: there, no server could be reached; here, one
		// was reached, ran the statement, went on serving every other request
		// while it did, and would have served the same statement against a
		// different node. 503 would also invite a Retry-After nothing in the
		// product can compute, because nothing knows when the contending writer
		// will stop (SPEC/WEB.md § Query-Bar Error Handling, rule 4).
		//
		// The statement is NOT re-sent outside the retry policy and there is no
		// store to run it against: this process opens none, and the graph is in
		// any case held by the server it was sent to (SPEC/GRAPH.md § Server
		// Resolution, rule 3).
		return newGraphQueryError(graphErrExecution,
			"query failed to execute: graph write conflict: another writer committed first on "+
				"every attempt within the "+backoff.Total().String()+" retry budget; nothing was "+
				"written. The statement is valid — run it again, and spread concurrent writes "+
				"across distinct nodes.")
	case graphclient.FailureBudget:
		// The budget line, produced by the one function that owns it, so the two
		// paths report an exhausted budget in the same words.
		return graphExecutionError(ctx, graphlock.StatementBudget, context.DeadlineExceeded)
	default:
		return newGraphQueryError(graphErrExecution, "query failed to execute: "+sendErr.Diagnostic)
	}
}

// graphExecutionError words a statement the time budget cut as the single
// execution-failure kind, truthfully about which cancellation source fired.
//
// The budget is enforced by the graph server, which takes the value as its
// MAXIMUM statement timeout and sets no default beside it (SPEC/GRAPH.md § Server
// Options; SPEC/WEB.md § Graph Query Time Budget, rule 1). The maximum alone
// carries the budget to every caller: the engine clamps a client-supplied timeout
// to it and applies it unconditionally to a statement that supplies none, so the
// failure this function words arrives typed over the protocol rather than from an
// engine in this process. It stays graphErrExecution and HTTP 400:
// exhausting the budget is a query execution failure, case 2 of SPEC/WEB.md
// § Query-Bar Error Handling, exactly as a query that fails in the graph is. No
// new kind, no new sentinel, no new status (§ Graph Query Time Budget, rules 4
// and 5).
//
// Only the reason differs, and it must not lie about which of the two composed
// cancellation sources fired. The request's own context still cancels the
// statement when the client disconnects, and that cancellation and the server's
// budget are two independent ends of the same statement:
//
//   - A budget exhaustion is reported as context.DeadlineExceeded and a client
//     disconnect as context.Canceled, both matchable with errors.Is.
//   - DeadlineExceeded alone is not proof of the budget: it is also what a
//     parent context with its own earlier deadline reports through a derived
//     one. parent is therefore consulted — it is the REQUEST's context, without
//     the budget layered on — and only a live parent attributes the failure to
//     the budget.
//
// The page renders whichever reason it is given verbatim in place, so all three
// read as the same "query failed to execute" message the user already knows
// (graph.js showQueryError).
func graphExecutionError(parent context.Context, budget time.Duration, err error) *graphQueryError {
	switch {
	case errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil:
		// The request is still live, so the deadline that fired is ours.
		return newGraphQueryError(graphErrExecution,
			"query failed to execute: exceeded the "+budget.String()+" query time budget")
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		// The request's own context died first: the client disconnected, or the
		// caller gave up. Not the budget.
		return newGraphQueryError(graphErrExecution,
			"query failed to execute: the request was cancelled before the query finished")
	default:
		return newGraphQueryError(graphErrExecution, "query failed to execute: "+err.Error())
	}
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
//
// The element object itself is built by internal/graphjson, the one realisation
// of the mapping every surface that publishes a graph value shares
// (SPEC/DATA_FORMATS.md § One Realisation of the Mapping). This endpoint owns
// the document those objects are placed in — collecting each element once,
// dropping an orphan edge, decomposing a path — and no part of the mapping
// inside them.
//
// The graphjson.Unmapped seam is nil because this endpoint publishes no path
// object: walk decomposes a path into its elements before anything is mapped,
// and a property value is never a graph element, so no value reaching the shared
// mapping from here can be of a kind it carries no row for.
func (c *graphCollector) addNode(nv expr.NodeValue) {
	if _, seen := c.nodeSet[nv.ID]; seen {
		return
	}
	c.nodeSet[nv.ID] = struct{}{}
	c.nodes = append(c.nodes, graphjson.Node(nv, nil))
}

// addRel collects a relationship once, deduplicated by id. The endpoint ids are
// kept so view() can drop the edge if either endpoint node was not collected.
// The element object comes from internal/graphjson; see addNode.
func (c *graphCollector) addRel(rv expr.RelationshipValue) {
	if _, seen := c.edgeSet[rv.ID]; seen {
		return
	}
	c.edgeSet[rv.ID] = struct{}{}
	c.edges = append(c.edges, relCandidate{
		startID: rv.StartID,
		endID:   rv.EndID,
		obj:     graphjson.Relationship(rv, nil),
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
