package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
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

// defaultGraphQueryBudget is the per-request time budget the graph data
// endpoint executes the caller's Cypher query under: the run against the
// engine's read path plus the walk over the result that run produces
// (SPEC/WEB.md § Graph Query Time Budget, rule 1).
//
// Five seconds sits well above the slowest execution measured on a small store
// — a three-way Cartesian product over a 252-node store spent 1.32 seconds of
// server time to return a single aggregate row — and well below the 30-second
// WriteTimeout (SPEC/WEB.md § HTTP Server Timeouts), so a query that exhausts
// the budget is cancelled and its failure is still written to the client.
//
// The budget bounds the WORK; the injected LIMIT bounds only the RESULT
// (SPEC/WEB.md § Graph Query Time Budget, rule 3). An aggregate over a
// Cartesian product returns one row whatever the limit is, yet scans the whole
// product to produce it, so the limit cannot bound it and the budget is the
// only bound on that work.
const defaultGraphQueryBudget = 5 * time.Second

// graphQueryBudget is the budget runGraphViewQuery actually applies. It is a
// var rather than a const for exactly one reason: the regression test for this
// bound drives it down to a few milliseconds so it can prove the cancellation
// without spending five real seconds per run.
//
// Production never reassigns it. It is initialised from
// defaultGraphQueryBudget and there is no flag, environment variable, request
// parameter, or any other user-facing knob that can change it, so every graph
// data request the server serves runs under the 5-second budget (SPEC/WEB.md
// § Graph Query Time Budget, rules 1 and 8).
var graphQueryBudget = defaultGraphQueryBudget

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

// reLeadingCall and reTopLevelReturn together recognise a STANDALONE PROCEDURE
// CALL: a statement whose first clause is CALL and that carries no top-level
// RETURN. That form admits no LIMIT clause, so the endpoint must inject nothing
// into it (SPEC/WEB.md § Graph Data Endpoint, node-limit injection,
// Suppression 2; Acceptance Criterion 111).
//
// The boundary is exactly the presence of a top-level RETURN, and it comes
// straight from the engine's grammar (GoGraph cypher/parser/grammar):
//
//	query           : regularQuery | standaloneCall
//	standaloneCall  : CALL invocationName parenExpressionChain? (YIELD ...)?
//	singlePartQ     : readingStatement* (returnSt | updatingStatement+ returnSt?)
//	projectionBody  : DISTINCT? projectionItems orderSt? skipSt? limitSt?
//
// A LIMIT attaches only to a projectionBody, which only a RETURN or a WITH
// carries; standaloneCall has no projectionBody at all. So a leading CALL with
// no RETURN parses as standaloneCall and rejects an appended LIMIT outright,
// while a leading CALL that IS projected through a RETURN parses as an ordinary
// regularQuery whose queryCallSt is just a reading statement, and takes the
// LIMIT exactly like a MATCH ... RETURN does. Measured against the engine:
// "CALL db.labels()\nLIMIT 100" fails with `cypher: parse: unexpected "LIMIT"`,
// whereas "CALL db.labels() YIELD label RETURN label\nLIMIT 1" runs and returns
// one row instead of two.
//
// Both run on the masked normalization (cypherguard.MaskLiterals), like every
// other discriminator in the guard rail, so a CALL or RETURN keyword that
// appears only inside a string literal, a comment, or a backtick identifier
// does not affect the decision.
var (
	// reLeadingCall is ANCHORED at the start of the statement (\A), the same
	// anchoring cypherguard's introspection matcher uses and for the same
	// reason: CALL introduces the standalone form only as the FIRST clause. A
	// CALL nested inside a larger query — "MATCH (n) CALL db.labels() YIELD
	// label RETURN n, label" — is a reading statement in a limitable query, so
	// it must still receive the injection. Leading whitespace is allowed, which
	// also covers a leading comment: MaskLiterals neutralises a comment to
	// spaces before this runs.
	reLeadingCall = regexp.MustCompile(`(?i)\A\s*CALL\b`)
	// reTopLevelReturn detects the RETURN that makes a leading CALL a projected,
	// and therefore limitable, query. Like reTopLevelLimit above it is a
	// presence check on the masked text rather than a full parse, and it errs the
	// same way: a RETURN the check sees means the query is treated as limitable
	// and receives the injection. That is the safe direction — a query wrongly
	// judged limitable keeps the node cap it would otherwise escape, and the only
	// place a RETURN can hide inside a standalone call is the "CALL ... YIELD ...
	// WHERE ..." tail, which this engine fails to plan at all ("plan root must be
	// ProduceResults, got *ir.Selection") with or without an injected LIMIT.
	reTopLevelReturn = regexp.MustCompile(`(?i)\bRETURN\b`)
)

// graphQueryError classifies a query-bar failure so the handler can map it to a
// distinct, in-page, read-only message (SPEC/WEB.md § Query-Bar Error Handling).
// The four kinds are kept separate so the user understands what to fix: a
// rejection (the query is not read-only), an invalid limit, a schema-
// introspection command whose keyword spacing the engine does not accept, or an
// execution failure in the engine.
type graphQueryError struct {
	// Reason is the user-facing message shown in place on the page.
	Reason string
	// Kind is the machine-readable failure class (see the graphErr* constants).
	Kind string
}

func (e *graphQueryError) Error() string { return e.Reason }

// Query-bar failure kinds. They map 1:1 to the distinct cases in
// SPEC/WEB.md § Query-Bar Error Handling.
//
// Every one of them is answered with HTTP 400 and told apart by this field, not
// by the status: RFC 9110 section 15.5 puts the explanation of an error in the
// response representation, so one status serves them all and the kind carries
// the class. Splitting them across different statuses would assert a distinction
// HTTP does not carry, while the body already carries it precisely.
//
// A kind exists per distinct FIX the caller must make, which is why
// graphErrRelationshipDirection is its own kind rather than a variant of
// graphErrNotReadOnly: such a query IS read-only and IS well formed, and what it
// needs is the traversal rewritten as outgoing.
//
// graphErrInvalidKeywordSpacing is deliberately NOT folded into
// graphErrNotReadOnly. A SHOW statement reads the schema and writes nothing
// whatever its spacing, so answering not_read_only would publish a
// classification the message printed beside it contradicts, and would tell a
// client the query writes when it does not. The two failures also have different
// fixes — one query must be rewritten to stop writing, the other only to close a
// gap between two keywords (SPEC/WEB.md § Query-Bar Error Handling, case 10).
const (
	graphErrNotReadOnly           = "not_read_only"               // query contains a writing or DDL clause
	graphErrInvalidLimit          = "invalid_limit"               // limit not one of the six allowed values
	graphErrInvalidKeywordSpacing = "invalid_keyword_spacing"     // SHOW INDEX(ES)/CONSTRAINT(S) spelled with a separator the engine does not accept
	graphErrRelationshipDirection = "relationship_read_direction" // reads a relationship bound by an incoming or undirected pattern
	graphErrExecution             = "execution"                   // accepted as read-only but failed in the engine
)

// newGraphQueryError builds a classified query-bar error.
func newGraphQueryError(kind, reason string) *graphQueryError {
	return &graphQueryError{Kind: kind, Reason: reason}
}

// graphRelationshipDirectionReason words the in-place message for a query that
// reads a relationship the engine would misresolve.
//
// It is deliberately shorter than the CLI's refusal: this text is rendered in
// the query bar rather than read in a terminal, so it states the objection and
// the shape of the rewrite, and leaves the full treatment to the SPEC. The two
// wordings are separate for the same reason every other kind's is — the CLI and
// this endpoint address different readers — while the CLASSIFICATION behind them
// is shared, which is what must not diverge.
//
// The variable name is caller-derived text (Cypher admits a backtick-quoted
// identifier holding arbitrary characters), so it reaches the response body as
// untrusted input. renderJSONStatus keeps encoding/json's HTML escaping ON, which
// is what makes that echo safe; see the graph query-bar body tests, which assert
// no raw angle bracket survives while the decoded value stays the original text.
func graphRelationshipDirectionReason(ref cypherguard.RelReadReference) string {
	return fmt.Sprintf(
		"query rejected: relationship %q is bound by an %s pattern, and on a node pair carrying "+
			"edges in both directions the engine reports the forward edge's type and orientation "+
			"for the reverse one. Rewrite the traversal as outgoing, anchoring it on either "+
			"endpoint: MATCH (x)-[%s]->(target {key:'...'}). To cover both directions, take the "+
			"union of the two outgoing legs with UNION ALL.",
		ref.Variable, ref.Direction, ref.Variable)
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
// CommentCount is how many comments the task has, which is all a card shows. The
// comment TEXT is deliberately absent: it is read only when a user opens that
// task's modal, by the task detail endpoint, one task at a time, so a page never
// reads a comment body in order to display a number (SPEC/WEB.md § Roadmap Tasks
// Page, read cost; SPEC/DATABASE.md § Count Comments for Many Parents (Grouped)).
//
// Match is whether the task satisfies EVERY active board control: the search term
// and the type, minimum-priority and minimum-severity filters, conjoined. It is
// true for every task when no control is active. The four controls reach the card
// through this ONE verdict rather than through a criterion of their own, which is
// what keeps the board to a single filtering model (SPEC/WEB.md § Roadmap Tasks
// Page, How the criteria compose). A task that does not match stays in the
// document — every card is in the page so the browser can narrow without a round
// trip — but is not SHOWN, and does not count towards its column's badge
// (SPEC/WEB.md § Roadmap Tasks Page, Effect on the board).
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
	Sprint *db.SprintRef
	models.Task
	CommentCount int
	Match        bool
}

// SearchText is the task's title normalised and folded by the board search's
// rules, which the card carries so the browser matches against the SAME text the
// server matched against.
//
// Transforming the corpus once, here, is what keeps the two paths equivalent: the
// script normalises and folds only the term the user typed, never the task text,
// so nothing about a task's text is ever transformed twice (SPEC/WEB.md § Roadmap
// Tasks Page, One rule, and only one implementation of it; Server and client
// produce the same board).
//
// The transformation is searchableText, the same function foldSearchTerm prepares
// a term with, so the corpus and the term are one implementation of one rule
// rather than two implementations of one description (see fold.go). The title is
// NOT trimmed: the trim is the term's alone, so a task's own leading or trailing
// whitespace is part of its text.
//
// It is a DERIVED form and nothing else. The bytes rmp stores are untouched, and
// the card renders v.Title itself, so normalisation reaches the comparison and
// neither storage nor display (SPEC/WEB.md § Roadmap Tasks Page, The
// normalisation rule; Acceptance Criterion 152).
func (v *taskView) SearchText() string {
	return searchableText(v.Title)
}

// HasMeta reports whether the card has at least one metadata indicator to show:
// its sprint, its subtasks, its dependencies, the tasks it blocks, or its
// comments. A task with none of the five renders no metadata footer at all — not
// an empty one (SPEC/WEB.md § Roadmap Tasks Page, absent metadata renders
// nothing; Acceptance Criterion 85).
//
// The five conditions are exactly the five the footer's own items are rendered
// under, so the footer can never be emitted empty and can never swallow an
// indicator the card should show.
//
// It governs the TASKS board's card alone. The sprint page's member-tasks board
// needs no such predicate: its card carries exactly two indicators, both of them
// counts, and renders both on every card including when either is 0, so its footer
// row is unconditional (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, Both
// counters are always rendered; Acceptance Criterion 134).
func (v *taskView) HasMeta() bool {
	return v.Sprint != nil ||
		v.SubtaskCount > 0 ||
		len(v.DependsOn) > 0 ||
		len(v.Blocks) > 0 ||
		v.CommentCount > 0
}

// matchesSearch reports whether the task matches an already-folded term.
//
// The searchable text is exactly the two things the card itself displays: the
// task title, and the task reference written with its leading "#". Matching the
// reference as the literal string "#42" is what lets both `42` and `#42` find task
// 42 under the one substring rule, with no special case for either form.
//
// Every other task field is deliberately outside the search: a term occurring
// only in a task's `functional_requirements`, and matching nothing in that task's
// title or reference, does not match it. The box answers "which task is this?"
// from what identifies a task on its card, and matching an attribute would answer
// a different question through the same control (SPEC/WEB.md § Roadmap Tasks
// Page, What the search matches; Acceptance Criterion 101).
func (v *taskView) matchesSearch(folded string) bool {
	if folded == "" {
		return true
	}
	if strings.Contains(v.SearchText(), folded) {
		return true
	}
	return strings.Contains("#"+strconv.Itoa(v.ID), folded)
}

// The board's minimum-priority and minimum-severity filters offer the thresholds
// minFilterThreshold to maxFilterThreshold. That is the priority and severity
// range of SPEC/MODELS.md § Task WITHOUT its 0 floor: a threshold of 0 admits
// every task and IS the unfiltered board, which already has its own option and
// its own URL form — the parameter absent — so offering it would give one board
// two URLs and two control settings (SPEC/WEB.md § Roadmap Tasks Page, What each
// filter matches).
//
// 0 is therefore free to mean "no filter on this dimension", which is exactly
// what `task.Priority >= 0` computes for every task: the inactive filter needs no
// branch of its own.
const (
	minFilterThreshold = 1
	maxFilterThreshold = 9
)

// boardControls is what one request asked the roadmap tasks board to show: the
// search term and the three header filters, reduced to the values the matching
// rule compares with.
//
// The three filters carry ACCEPTED values only. A value a dimension does not
// accept never reaches this struct: the parser turns it into the zero value,
// which is the same state the parameter's absence produces, so "no filter value
// is an error" is settled once, at the boundary, and every consumer downstream
// sees one representation of "this dimension is not filtered" (SPEC/WEB.md
// § Roadmap Tasks Page, No filter value is an error).
//
// Search keeps the term exactly as the request carried it, because that string is
// echoed back into the input and the no-match message; folded is the same term in
// the form the matching rule uses, computed ONCE per request rather than once per
// task.
//
// Field order places the three string-shaped fields before the two ints, so the
// pointer-scan prefix stops at the ints (govet fieldalignment).
type boardControls struct {
	Search   string
	folded   string
	Type     models.TaskType
	Priority int
	Severity int
}

// newBoardControls builds the controls from values already reduced to their
// accepted forms, folding the term once.
func newBoardControls(search string, taskType models.TaskType, priority, severity int) boardControls {
	return boardControls{
		Search:   search,
		folded:   foldSearchTerm(search),
		Type:     taskType,
		Priority: priority,
		Severity: severity,
	}
}

// parseBoardControls reads the board's four header controls out of a request's
// query string. It cannot fail: every value a caller can send maps to a control
// state, so this route's status codes do not depend on what the query carries
// (SPEC/WEB.md § Roadmap Tasks Page, No malformed term is an error; No filter
// value is an error).
//
// url.Values.Get answers the FIRST value of a repeated parameter, which is the
// reading the specification fixes for ?type=BUG&type=EPIC, and answers "" for a
// parameter url.URL.Query could not decode, which is the reading it fixes for an
// undecodable one. Both rules therefore come from the standard library's own
// semantics rather than from a special case here.
func parseBoardControls(query url.Values) boardControls {
	return newBoardControls(
		query.Get("q"),
		parseTypeFilter(query.Get("type")),
		parseThresholdFilter(query.Get("priority")),
		parseThresholdFilter(query.Get("severity")),
	)
}

// parseTypeFilter reduces a raw type parameter to the TaskType it names, or to
// the empty TaskType when it names none.
//
// The comparison is EXACT against the enum's own spelling, in upper case, and no
// case folding is applied: `rmp task list -y bug` is rejected by the CLI for the
// same reason, so one parameter name means one thing across the two surfaces
// (SPEC/WEB.md § Roadmap Tasks Page, What each filter matches; SPEC/COMMANDS.md
// § List Tasks). A comma-packed value such as "BUG,EPIC" is one string, is not
// one of the ten, and is ignored whole — no filter is ever partly applied.
func parseTypeFilter(raw string) models.TaskType {
	if models.IsValidTaskType(raw) {
		return models.TaskType(raw)
	}
	return ""
}

// parseThresholdFilter reduces a raw minimum-priority or minimum-severity
// parameter to the threshold it names, or to 0 — no filter — when it names none.
//
// Accepted is the canonical spelling of an integer in [minFilterThreshold,
// maxFilterThreshold] and nothing else. strconv.Atoi already rejects a value with
// surrounding spaces or a non-numeric body; the round-trip through strconv.Itoa
// rejects the decorated spellings it would otherwise accept — "+5" and "05" parse
// as 5 but are not how 5 is written — so a decorated value applies no filter
// rather than silently applying one the URL does not say (SPEC/WEB.md § Roadmap
// Tasks Page, No filter value is an error).
//
// strconv.Itoa allocates nothing for a value below 100: it slices a package-level
// string of the small integers.
func parseThresholdFilter(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < minFilterThreshold || n > maxFilterThreshold {
		return 0
	}
	if raw != strconv.Itoa(n) {
		return 0
	}
	return n
}

// active reports whether any control is narrowing the board. It is the condition
// that separates a board narrowed to nothing — which says so — from a roadmap
// that holds no task at all, which does not (SPEC/WEB.md § Roadmap Tasks Page,
// Empty states).
func (c boardControls) active() bool {
	return c.folded != "" || c.Type != "" || c.Priority > 0 || c.Severity > 0
}

// matches is the board's ONE verdict on one task: the task is shown when it
// satisfies EVERY active control, and a board with no active control shows every
// task (SPEC/WEB.md § Roadmap Tasks Page, How the criteria compose).
//
// The conjunction is total and each clause decides only its own dimension, so
// narrowing a control can only shrink the shown set and no control ever re-admits
// a task another control excluded.
//
// The two ordinal clauses need no "is this filter active" test: an inactive
// threshold is 0, and every priority and severity is >= 0 by the range
// SPEC/MODELS.md § Task fixes, so the inactive filter admits everything by
// arithmetic rather than by a branch.
//
// The clauses are ordered cheapest first, which a conjunction of pure predicates
// leaves free to choose: the two integer comparisons and the string equality cost
// nothing, while the term clause folds the task's title (taskView.SearchText).
// Rejecting a task on a threshold therefore skips that fold entirely. Measured
// over 200 tasks with all four controls in force
// (BenchmarkTaskMatches_OrdinalFirst vs BenchmarkTaskMatches_TermFirst):
// 513 ns/op and 0 allocs against 20158 ns/op and 200 allocs — one allocation per
// task saved per render (three runs, 20000 iterations each, all within 0.4%). Evaluation order cannot change the verdict, so the
// property that server and client agree is untouched.
func (v *taskView) matches(c boardControls) bool {
	return v.Priority >= c.Priority &&
		v.Severity >= c.Severity &&
		(c.Type == "" || v.Type == c.Type) &&
		v.matchesSearch(c.folded)
}

// filterOption is one option of a header filter dropdown: the value the URL
// parameter carries, the text the option shows, and whether this request's value
// selected it.
//
// Value and Label are the SERVER's own strings — a TaskType from the enum, a
// threshold from the range, or the empty value of the dimension's no-filter
// option — never a string the caller supplied. A caller's parameter only decides
// which of them carries Selected, so no caller-supplied string reaches the page
// through type, priority, or severity (SPEC/WEB.md § Roadmap Tasks Page, A filter
// value is never echoed into the page).
type filterOption struct {
	Value    string
	Label    string
	Selected bool
}

// boardFilters is the three header dropdowns as the template renders them: one
// fixed option set per dimension, with exactly one option marked selected.
type boardFilters struct {
	Types      []filterOption
	Priorities []filterOption
	Severities []filterOption
}

// newBoardFilters builds the three option sets for one request's controls.
func newBoardFilters(c boardControls) boardFilters {
	return boardFilters{
		Types:      typeFilterOptions(c.Type),
		Priorities: thresholdFilterOptions("Any priority", c.Priority),
		Severities: thresholdFilterOptions("Any severity", c.Severity),
	}
}

// typeFilterOptions enumerates the type dropdown: the no-filter option, then the
// ten TaskType values.
//
// The values come from models.ValidTaskTypes rather than from a literal here or
// in the template, so the dropdown cannot drift from the enum: a type added to
// SPEC/MODELS.md § Enums appears here with no change to this file.
func typeFilterOptions(selected models.TaskType) []filterOption {
	options := make([]filterOption, 0, len(models.ValidTaskTypes)+1)
	options = append(options, filterOption{Label: "Any type", Selected: selected == ""})
	for _, taskType := range models.ValidTaskTypes {
		options = append(options, filterOption{
			Value:    string(taskType),
			Label:    string(taskType),
			Selected: taskType == selected,
		})
	}
	return options
}

// thresholdFilterOptions enumerates one threshold dropdown: the no-filter option,
// then the thresholds minFilterThreshold to maxFilterThreshold. The range is the
// one the constants fix, so the offered set and the accepted set are the same set
// and neither can drift from the other.
func thresholdFilterOptions(anyLabel string, selected int) []filterOption {
	options := make([]filterOption, 0, maxFilterThreshold-minFilterThreshold+2)
	options = append(options, filterOption{Label: anyLabel, Selected: selected == 0})
	for threshold := minFilterThreshold; threshold <= maxFilterThreshold; threshold++ {
		value := strconv.Itoa(threshold)
		options = append(options, filterOption{
			Value:    value,
			Label:    value,
			Selected: threshold == selected,
		})
	}
	return options
}

// taskColumn is one column of the roadmap tasks page's Kanban board: a task
// status, the cards of the tasks in that status, and the count its header shows.
//
// Tasks holds POINTERS into the page's flat task list rather than copies, so the
// board and the task detail modals it opens are rendered from one set of values
// and cannot drift apart. It holds every task of that status, including the ones a
// search hides: the card stays in the document so the browser can show it again
// without a round trip.
//
// Count is the number of tasks the column SHOWS — those whose Match is true — so
// the header badge always states what the user is looking at. A count that kept
// reporting the unnarrowed total while the column displayed fewer cards would
// state something false, which is exactly what the count exists to prevent
// (SPEC/WEB.md § Roadmap Tasks Page, Count per column; Effect on the board;
// Acceptance Criteria 83 and 101).
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
// Search is the term exactly as the request carried it, echoed back into the
// search input and into the no-match message; html/template escapes it in both
// places. Filters is the three header dropdowns with their fixed option sets and
// the one option this request selected — the server's own strings throughout, so
// no filter value is ever echoed into the page.
//
// SearchActive says a term is in force (the folded term is non-empty), which is
// what decides whether the no-match message names the term at all. NoMatches says
// at least one control is in force and nothing matched it, which is the condition
// for the board's "no task matches" message — a different condition from a
// roadmap that holds no task at all. One message covers the term and the three
// filters together, because the shown set is their conjunction (SPEC/WEB.md
// § Roadmap Tasks Page, Effect on the board; Empty states).
type tasksData struct {
	Name         string
	Search       string
	Chrome       chrome
	Filters      boardFilters
	Tasks        []taskView
	Columns      []taskColumn
	SearchActive bool
	NoMatches    bool
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
// `<pct>% - P:<p> A:<a> C:<c> - T:<t>` (for example `33% - P:8 A:29 C:18 - T:55`).
// P, A and C always sum to T: the three categories partition the closed task
// status enum, so no member task is counted twice and none is left out.
// It is the single place the format string lives, so both call sites of the
// shared sub-template produce a byte-identical line (SPEC/WEB.md § Shared Sprint
// Presentation Sub-Template, sprint status summary line).
func (c sprintCompletion) Line() string {
	return fmt.Sprintf("%d%% - P:%d A:%d C:%d - T:%d",
		c.Pct, c.Pending, c.InProgress, c.Completed, c.Total)
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
// so the board and the single modal shell its cards open are rendered from one
// set of values and cannot drift apart. There is no Count field: the column's
// badge is len(Tasks), because this board carries no narrowing control and so has
// no second notion of "how many are shown" to keep in step with a stored number
// (contrast taskColumn, whose count is what the tasks page's search left visible).
//
// CanonicalStatus is the status the column's count badge takes its colour from: a
// column of this board groups a SET of statuses and writes none of them, so the
// status its colour is keyed on has to be named somewhere, and it is named in the
// model rather than in the template. The template hands it to taskStatusBadge, the
// same helper every task status badge takes its colour from, so the board reads
// the ONE semantic mapping instead of carrying colour literals that could drift
// from it (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, Column header;
// § Status, Priority, and Severity Badge Colours, rule 2; Acceptance Criterion
// 140).
//
// Field order puts the two strings before the slice so the pointer-scan prefix
// stops at the slice header rather than spanning the whole struct (govet
// fieldalignment).
type sprintBoardColumn struct {
	Heading         string
	CanonicalStatus models.TaskStatus
	Tasks           []*taskView
}

// sprintBoardColumns is the board's three fixed columns, left to right, each
// pairing the heading it shows with the sprint-summary category it holds
// (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, Three fixed columns).
//
// The category is models.TaskStatusCategory — the SAME categorisation the sprint
// status summary line is computed from (models.CalculateSprintSummary, which
// counts through models.CategorizeTaskStatus). Naming the categories here rather
// than the statuses is what makes each column's badge equal one of that line's own
// numbers by construction instead of by coincidence: there is one mapping from
// status to bucket in the project, and both presentations read it (Acceptance
// Criterion 131).
//
// The headings are written exactly as the specification spells them, in upper
// case, and are not translated.
//
// The third field is the CANONICAL status of the group — the status a task is
// normally in at that stage of the sprint — and it is what the column's count
// badge is coloured by (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, Column
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
// (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, Order within a column;
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
var sprintBoardColumns = [...]struct {
	orderingTimestamp func(*models.Task) *string
	heading           string
	canonical         models.TaskStatus
	category          models.TaskStatusCategory
}{
	{nil, "WAITING", models.StatusSprint, models.CategoryPending},
	{startedAt, "DOING", models.StatusDoing, models.CategoryInProgress},
	{closedAt, "CLOSED", models.StatusCompleted, models.CategoryCompleted},
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
// Tasks is the sprint's member tasks in planned in-sprint execution order
// (sprint_tasks position ascending), each carrying its own comment count; Columns
// is that same list grouped into the member-tasks board's three fixed columns and
// ordered per column — WAITING keeping the position order, DOING and CLOSED
// reordered by started_at and closed_at descending — which is what the
// sub-template renders. Both describe the same tasks: Columns points into Tasks,
// and only the order in which it walks them differs.
//
// Comments is the sprint's OWN comment log — the sprint's progression account —
// oldest first, rendered in the Comments card the sub-template places last. It
// never carries a member task's comments: those belong to that task's own detail
// modal, and the sprint level presents no aggregate of them (SPEC/WEB.md § Sprint
// Detail Sub-Template, Comments card scope; Acceptance Criterion 69). A board
// card shows the NUMBER of a member task's comments and never their text, which
// is read only when a user opens that task's modal, one task at a time.
type sprintDetail struct {
	Name     string
	Tasks    []taskView
	Columns  []sprintBoardColumn
	Comments []models.SprintComment
	Sprint   models.Sprint
	Summary  sprintCompletion
}

// sprintPageData is the view model handed to the roadmap sprint template. It
// presents a single sprint with all of its fields, its member tasks as a Kanban
// board of three fixed columns each ordered by its own key — each card clickable
// to open the read-only task detail modal — and the sprint's own comments
// (SPEC/WEB.md § Roadmap Sprint Page). It is read-only.
type sprintPageData struct {
	Name     string
	Chrome   chrome
	Tasks    []taskView
	Columns  []sprintBoardColumn
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
		Columns:  d.Columns,
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

// taskCommentCounter is the ONLY comment read the tasks page is given: the
// grouped COUNT over the whole set of rendered task ids, one statement for every
// task (SPEC/DATABASE.md § Count Comments for Many Parents (Grouped)).
//
// The per-task listing (db.ListTaskComments) is deliberately absent from this
// interface, and it is the only read that could bring a comment BODY onto this
// path. Its absence therefore carries two guarantees at once: the page cannot
// express the N+1 pattern SPEC/WEB.md forbids — one query per rendered task — and
// it cannot read comment text at all. The board shows a number, and a task's
// comment text is read only when a user opens that task's modal, by the task
// detail endpoint. *db.DB satisfies the interface.
type taskCommentCounter interface {
	CountTaskCommentsByTasks(ctx context.Context, taskIDs []int) (map[int]int, error)
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
	taskCommentCounter
	taskSprintReader
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
// The member-task read (db.GetSprintTasksFull) is deliberately absent, exactly
// as the per-task comment listing is absent from tasksSource: it is the read
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
// ONE grouped count over the whole set of rendered member-task ids, which is what
// gives each board card its comment number. Neither grows with the member-task
// count (SPEC/WEB.md § Sprint Detail Sub-Template, Read cost; Acceptance Criteria
// 70 and 137).
//
// The per-task listing (db.ListTaskComments) is deliberately absent from this
// interface, exactly as it is from tasksSource: it is the only read that could
// bring a comment BODY onto this path, so its absence carries two guarantees at
// once — the page cannot express the N+1 pattern SPEC/WEB.md forbids, one query
// per rendered card, and it cannot read comment text at all. A member task's
// comment text is read only when a user opens that task's modal, one task at a
// time, through the task detail endpoint.
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
func loadTasks(ctx context.Context, name string, controls boardControls) (tasksData, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return tasksData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	return readTasks(ctx, database, name, controls)
}

// readTasks is the tasks page's entire read, expressed against the page's read
// surface rather than a concrete connection. It is THREE reads and no more: the
// full task list, then the comment COUNT of every task the page renders in ONE
// grouped query, then the sprint of every task the page renders in ONE grouped
// query (SPEC/WEB.md § Roadmap Tasks Page, read cost).
//
// The page reads comment counts, never comment bodies: the card shows a number,
// and a task's comment text is read only when a user opens that task's modal, by
// the task detail endpoint (SPEC/WEB.md § Task Detail Endpoint).
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
func readTasks(ctx context.Context, src tasksSource, name string, controls boardControls) (tasksData, error) {
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

	views := newTaskViews(tasks)

	if err := attachCommentCounts(ctx, src, views); err != nil {
		return tasksData{}, err
	}

	if err := attachSprints(ctx, src, views); err != nil {
		return tasksData{}, err
	}

	// The header controls narrow what the board SHOWS; they narrow neither what
	// the page reads nor what the document carries. Every card stays in the page,
	// which is what lets the browser re-narrow with no round trip, and the ONE
	// verdict recorded here — the conjunction of the term and the three filters —
	// is the one the browser recomputes when the user types or picks a value.
	//
	// A filter adds no clause to the read above, no second read, and no
	// per-dimension query: it is applied in memory over the rows already in hand,
	// exactly as the term is, so the page's query count is the same narrowed as
	// unnarrowed (SPEC/WEB.md § Roadmap Tasks Page, Read cost; Server and client
	// produce the same board).
	shown := 0
	for i := range views {
		views[i].Match = views[i].matches(controls)
		if views[i].Match {
			shown++
		}
	}

	return tasksData{
		Name:         name,
		Search:       controls.Search,
		Filters:      newBoardFilters(controls),
		Tasks:        views,
		Columns:      groupIntoColumns(views),
		SearchActive: controls.folded != "",
		NoMatches:    controls.active() && shown == 0,
	}, nil
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

	sprints, err := r.GetSprintsByTasks(ctx, taskViewIDs(views))
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
		shown := 0
		for _, task := range columns[i].Tasks {
			if task.Match {
				shown++
			}
		}
		columns[i].Count = shown
	}
	return columns
}

// newTaskViews wraps every task in the view the templates consume. It performs
// no read: a view carries the task itself, and the two values a surface may add
// to it — the comment count and the sprint — are attached by the loaders that
// need them.
//
// It is the one place a page turns tasks into task views, so both surfaces that
// show a clickable task build the same value from the same rule.
func newTaskViews(tasks []models.Task) []taskView {
	views := make([]taskView, len(tasks))
	for i := range tasks {
		views[i] = taskView{Task: tasks[i]}
	}
	return views
}

// attachCommentCounts reads the comment COUNT of EVERY view in one grouped query
// over the whole set of rendered task ids — never one per card, and never the
// comment bodies (SPEC/DATABASE.md § Count Comments for Many Parents (Grouped);
// Acceptance Criterion 70).
//
// A page that renders no task issues no comment query at all: the read is skipped
// outright rather than called with an empty id set (which
// db.CountTaskCommentsByTasks would also answer without a statement).
//
// A task with no comment is ABSENT from the grouped map, and the zero value a
// missing key yields is already the right count, so the pairing needs no presence
// check.
func attachCommentCounts(ctx context.Context, r taskCommentCounter, views []taskView) error {
	if len(views) == 0 {
		return nil
	}

	counts, err := r.CountTaskCommentsByTasks(ctx, taskViewIDs(views))
	if err != nil {
		return err
	}

	for i := range views {
		views[i].CommentCount = counts[views[i].ID]
	}
	return nil
}

// taskViewIDs is the set of rendered task ids, in render order, which is what
// every grouped read of this page is given.
func taskViewIDs(views []taskView) []int {
	ids := make([]int, len(views))
	for i := range views {
		ids[i] = views[i].ID
	}
	return ids
}

// taskDetailSource is the complete read surface of the task detail endpoint: one
// task, and that task's comments in full.
//
// The comment read here is the SINGLE-parent listing, deliberately: the endpoint
// serves exactly one task, requested when a user opens its modal, so there is no
// set of ids to group over. It is also the only path on which the web interface
// reads comment TEXT for a task, which is what keeps the page-rendering paths
// free of it (SPEC/WEB.md § Task Detail Endpoint, Reads).
type taskDetailSource interface {
	GetTask(ctx context.Context, id int) (*models.Task, error)
	ListTaskComments(ctx context.Context, taskID int, commentType *models.CommentType) ([]models.TaskComment, error)
}

// taskDetailView is the JSON body of the task detail endpoint: exactly two
// members, `task` and `comments` (SPEC/WEB.md § Task Detail Endpoint, Response).
//
// It introduces NO new object shape. Task and TaskComment are marshalled through
// their own JSON tags, which are the shapes SPEC/DATA_FORMATS.md § Task and
// § Task Comment already fix for CLI output, so a value carries the same field
// names, types, and null conventions here as it does there.
//
// Comments is never nil: a task with no comment yields `[]`, not `null`.
type taskDetailView struct {
	Comments []models.TaskComment `json:"comments"`
	Task     models.Task          `json:"task"`
}

// loadTaskDetail reads one task and its comments read-only for the task detail
// endpoint. It opens the roadmap database, reads, and releases the handle; no row
// is written and no audit entry is produced (SPEC/WEB.md § Task Detail Endpoint,
// Read-only).
//
// The caller is responsible for the {name} validation and existence check
// (resolveRoadmap); this function trusts name is a validated, existing roadmap.
// A task id that is not a task of THIS roadmap yields utils.ErrNotFound, which
// the handler maps to 404: a task of another roadmap is not reachable through
// this roadmap's path space, because the read is scoped to this roadmap's own
// database file.
func loadTaskDetail(ctx context.Context, name string, id int) (taskDetailView, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return taskDetailView{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	return readTaskDetail(ctx, database, id)
}

// readTaskDetail is the endpoint's entire read, expressed against its read
// surface rather than a concrete connection: the task, then that task's comments.
// Two reads for the one task requested, issued only when a user opens a modal, so
// they are not on the page-rendering path.
//
// Separating it from loadTaskDetail is what makes the endpoint's query count
// measurable against a real database, as it is for every page loader.
//
// The comments are oldest first — created_at ascending, comment id ascending as
// the tie-breaker — exactly the order `rmp task comment-list` returns and the
// order the modal's timeline presents. No type filter (nil) and no count limit
// apply: every comment of the task is returned.
func readTaskDetail(ctx context.Context, src taskDetailSource, id int) (taskDetailView, error) {
	task, err := src.GetTask(ctx, id)
	if err != nil {
		return taskDetailView{}, err
	}

	comments, err := src.ListTaskComments(ctx, id, nil)
	if err != nil {
		return taskDetailView{}, err
	}
	// A task with no comment must serialise as [], never null: the client walks
	// the array unconditionally.
	if comments == nil {
		comments = []models.TaskComment{}
	}

	return taskDetailView{Task: *task, Comments: comments}, nil
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
// tasks in ONE grouped query, and the sprint's OWN comments (SPEC/WEB.md
// § Roadmap Sprint Page; § Tasks and Sprints from SQLite, rule 1).
//
// TWO comment reads, and only two, whatever the number of member tasks: the
// sprint's own log, which the Comments card renders in full, and the grouped
// count that gives each board card its comment number. Neither grows with the
// member-task count, and the page reads no comment BODY for a task it renders —
// the text of a member task's comments is fetched only when a user opens that
// task's modal, by the task detail endpoint (Acceptance Criteria 70 and 137).
//
// A sprint with no member task costs one of those two: the grouped count takes
// the set of rendered task ids, and that set is empty, so it is skipped outright
// rather than issued against an empty IN list. The Comments card is always
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
	// the whole set of ids — the same helper and the same statement the tasks
	// page's board uses, so the two boards read their card counts one way
	// (SPEC/DATABASE.md § Count Comments for Many Parents (Grouped)).
	if err := attachCommentCounts(ctx, src, views); err != nil {
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
		Summary:  newSprintCompletion(orderedTasks),
	}, nil
}

// groupIntoSprintBoardColumns groups a sprint's member-task views into the
// member-tasks board's three fixed columns — WAITING, DOING, CLOSED — in that
// order (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4; Acceptance Criteria
// 130 to 132).
//
// The bucket a task falls in comes from models.CategorizeTaskStatus, which is the
// project's ONE mapping from a task status to a sprint-summary category and is
// what models.CalculateSprintSummary counts the summary line's P, A and C through.
// Reusing it, rather than writing a second status-to-column mapping here, is what
// makes each column's badge equal its counterpart in the summary line by
// construction: there is a single categorisation, so the board and the line cannot
// come to disagree about which tasks are waiting, which are being worked on, and
// which are done (Acceptance Criterion 131).
//
// All three columns are built on every request, whatever the sprint holds, so an
// empty column is a built column with no card and a sprint with no member task
// renders an empty board rather than an absent one (Acceptance Criterion 130).
//
// EACH COLUMN THEN TAKES ITS OWN ORDER, because the three columns answer three
// different questions (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, Order
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
// carrying it is placed in no column rather than in an invented fourth one, which
// is the same defensive treatment groupIntoColumns gives an unknown status on the
// tasks board.
func groupIntoSprintBoardColumns(views []taskView) []sprintBoardColumn {
	columns := make([]sprintBoardColumn, len(sprintBoardColumns))
	for i := range sprintBoardColumns {
		columns[i] = sprintBoardColumn{
			Heading:         sprintBoardColumns[i].heading,
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
// absent last (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, The tiebreaker is
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
	// empty CLOSED (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, Every column
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
// the rest of its fields for the sprint page and the task detail modal — all
// without a second per-task query.
//
// Only the single Roadmap Sprint Page reads through it. The sprints landing page
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

// applyGraphLimit appends a top-level LIMIT clause to query, and is the single
// place the node-limit injection rule lives (SPEC/WEB.md § Graph Data Endpoint,
// node-limit injection). Injection is suppressed in exactly two cases and
// applies in every other one:
//
//   - Suppression 1: the query already carries a top-level LIMIT. The
//     user-authored LIMIT is respected as-is and the resolved dropdown value is
//     not applied.
//   - Suppression 2: the query is a statement form that admits NO LIMIT clause
//     at all — a schema-introspection command, or a standalone procedure call.
//     Appending a LIMIT to one of those bounds nothing; it makes the statement
//     fail in the PARSER, so a read the guard rail admits, and that
//     `rmp graph query` runs, would be unusable through this endpoint and the
//     endpoint would be stricter than the contract it publishes.
//
// Both suppression checks run on the literal-masked normalization
// (cypherguard.MaskLiterals), so a LIMIT, SHOW, CALL, or RETURN keyword that
// appears only inside a string literal, a comment, or a backtick identifier does
// not affect the decision, and both forms of Suppression 2 are anchored to the
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
	masked := cypherguard.MaskLiterals(query)

	// Suppression 1: the caller wrote their own top-level LIMIT.
	if reTopLevelLimit.MatchString(masked) {
		return query
	}
	// Suppression 2: a statement form that cannot carry a LIMIT clause.
	if !admitsLimitClause(query, masked) {
		return query
	}
	return query + "\nLIMIT " + strconv.Itoa(limit)
}

// admitsLimitClause reports whether query is a statement form that can carry a
// top-level LIMIT clause. masked MUST be cypherguard.MaskLiterals(query); it is
// passed in rather than recomputed because the caller already holds it.
//
// This is a SYNTAX question — "can this statement carry a LIMIT?" — and not a
// read-only or safety question, which is why the standalone-call predicate lives
// here beside the injection rule it serves and not in the shared cypherguard
// guard rail: the endpoint's admission decision is, and stays, exactly the guard
// rail's, and this function changes no operation class. Both forms below are
// read-only before this check and after it, and cypherguard.IsReadOnly has
// already decided, alone, whether they execute at all. The one piece that IS
// shared is reused rather than reimplemented: the literal masking, and the
// recognition of the introspection class itself, both come from cypherguard, so
// the two can never drift apart on what a SHOW command is (SPEC/WEB.md § Graph
// Data Endpoint, Suppression 2; SPEC/GRAPH.md § Schema Introspection).
//
// Two forms admit no LIMIT:
//
//  1. A schema-introspection command — the SHOW INDEXES / SHOW INDEX /
//     SHOW CONSTRAINTS / SHOW CONSTRAINT class, INCLUDING one carrying a YIELD,
//     WHERE, or RETURN tail: the engine's SHOW parser rejects ORDER BY, SKIP, and
//     LIMIT on every one of those forms, so a tail does not make a LIMIT
//     injectable. The predicate is cypherguard's own Introspect classification,
//     which is exactly this class and nothing wider: every other SHOW the guard
//     rail sees (SHOW DATABASES, SHOW FUNCTIONS, SHOW PROCEDURES, ...) is not
//     part of it, and the engine rejects those at the parser whether or not a
//     LIMIT is appended, so leaving them out changes nothing for them. A SHOW
//     command whose keyword spacing the engine does not accept is not part of
//     the class either, and never reaches here: loadGraphView refuses it before
//     the store is opened, so the question of whether it admits a LIMIT never
//     arises (SPEC/GRAPH.md § Keyword Spacing in a Schema-Introspection Command).
//  2. A standalone procedure call — first clause CALL, no top-level RETURN.
//     See reLeadingCall and reTopLevelReturn for why the top-level RETURN is the
//     whole of the boundary.
func admitsLimitClause(query, masked string) bool {
	if cypherguard.Classify(query).Introspect {
		return false
	}
	if reLeadingCall.MatchString(masked) && !reTopLevelReturn.MatchString(masked) {
		return false
	}
	return true
}

// loadGraphView reads a roadmap's knowledge graph and returns its nodes and
// edges in the Graph View Data shape. It mirrors the read path of
// commands/graph.go runGraphRead exactly, down to the lock: it takes the graph
// store's SHARED lock, opens the store via recovery, releases the lock as soon
// as the open returns, and runs a single read-only Cypher query through the
// engine's read path with no lock held. It MUST NOT run any writing clause and
// MUST NOT checkpoint or truncate the WAL (SPEC/WEB.md § Graph Data Endpoint,
// read-only guard-rail).
//
// rawQuery and rawLimit are the request's q and limit URL parameters (empty
// when absent). The query is resolved (default when absent), validated as
// read-only via the shared cypherguard guard-rail BEFORE execution, and has a
// LIMIT injected only when it has no top-level LIMIT of its own AND is a
// statement form that admits a LIMIT clause. A query that contains any
// writing or DDL clause, a query that is a schema-introspection command whose
// keyword spacing the engine does not accept, or an invalid limit, is returned
// as a classified graphQueryError and is never executed; the store is not opened
// for it when the failure is detectable before opening.
//
// A roadmap that has never used the graph command (no graph/ directory) is an
// empty graph, not an error: loadGraphView returns empty arrays WITHOUT creating
// the directory (SPEC/WEB.md § Roadmap Knowledge-Graph Page, empty graph). When
// the directory does exist, a read changes no graph DATA but is not free of
// on-disk effect: it may create the lock file, and the recovery that opening
// runs may complete an interrupted checkpoint. The exhaustive list is
// SPEC/GRAPH.md § What a Read Changes on Disk.
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

	// Keyword-spacing rejection, third and last in the precedence order the
	// endpoint publishes (invalid_limit, then not_read_only, then
	// invalid_keyword_spacing; SPEC/WEB.md § Query-Bar Error Handling, rule 6).
	// It is decided AFTER the read-only check because the objection that a query
	// writes outranks the objection that it is misspelled, and it carries its own
	// kind because a SHOW statement is read-only at any spacing — reporting it as
	// not read-only would be a false classification.
	//
	// The rejection precedes execution and precedes the node-limit injection, so
	// applyGraphLimit is never reached for such a statement and the question of
	// whether it admits a LIMIT clause never arises (see admitsLimitClause).
	if reason, misspaced := cypherguard.IntrospectSpacingRejection(query); misspaced {
		return graphView{}, newGraphQueryError(graphErrInvalidKeywordSpacing, "query rejected: "+reason)
	}

	// Relationship-read direction, last in the precedence order and, like the
	// three above, decided before the store is opened and before the query ever
	// reaches the engine (SPEC/GRAPH.md § Relationship Read Direction, which owns
	// the rule; SPEC/WEB.md § Query-Bar Error Handling).
	//
	// It is decided LAST because every earlier objection outranks it: a query
	// that writes must be told that it writes, and a schema-introspection command
	// the engine cannot parse has no pattern to orient. The check runs on the
	// query the caller supplied, before applyGraphLimit injects the node limit,
	// because injecting a LIMIT changes no relationship pattern.
	//
	// The classifier is the SAME one the CLI subcommands use. There is one
	// classification of pattern direction in this project, not a second copy that
	// could drift: this endpoint executes caller-supplied Cypher through its own
	// engine instance, so without it the identical misresolved read would remain
	// reachable from the query bar after the CLI had been closed off.
	if misread := cypherguard.MisreadRelationshipReferences(query); len(misread) > 0 {
		return graphView{}, newGraphQueryError(
			graphErrRelationshipDirection, graphRelationshipDirectionReason(misread[0]))
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

	// Shared store lock, held across the store open ALONE, exactly as the CLI
	// read subcommands hold it. It is taken because opening the store is not a
	// read-only operation on disk: recovery repairs an interrupted checkpoint
	// first, so an unlocked request could delete or race the staging directory
	// a concurrent `rmp graph` write is publishing its snapshot from. Waiting
	// for the lock is bounded (at most 2.5 s) and is spent BEFORE the query
	// starts, so it does not consume this endpoint's query time budget and
	// stays well inside the server's write timeout. A wait that is exhausted
	// returns a plain ErrDatabase, which handleGraphData answers with HTTP 500
	// — the status it already returns for a store that cannot be opened
	// (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 5).
	//
	// It is released with an explicit call rather than a defer, on both the
	// success and the failure path, so the hold cannot be silently widened to
	// the query by a later edit: the server must not hold the lock for the
	// duration of a request, or a slow query would fail a concurrent CLI write.
	releaseLock, err := graphlock.AcquireShared(graphDir)
	if err != nil {
		return graphView{}, err
	}
	res, openErr := recovery.Open[string, float64](graphDir, recovery.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})
	releaseLock()
	if openErr != nil {
		return graphView{}, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, openErr)
	}

	engine := cypher.NewEngine(res.Graph)

	// Inject the node limit only when the (validated, read-only) query has no
	// top-level LIMIT of its own AND is a statement form that admits a LIMIT
	// clause at all. The original query — not the masked copy — is what executes;
	// masking only governs the suppression checks and the guard-rail.
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
//
// The whole of that execution runs under the per-request query time budget
// (SPEC/WEB.md § Graph Query Time Budget). The deadline is derived HERE, and
// not in loadGraphView, because rule 1 defines the budget as covering exactly
// what this function does — the run against the engine's read path and the walk
// over the result that run produces — and nothing else: resolving the limit,
// the read-only guard rail, and opening the store are not query execution. The
// walk MUST be inside the deadline and not merely the Run: the engine streams a
// disconnected pattern's tuples as the result is iterated, so a Cartesian
// product's cost is paid during result.Next(), and Run returns a nil error long
// before it. A deadline covering only Run would therefore bound nothing.
//
// Deriving it from ctx (context.WithTimeout, not context.WithDeadline on a
// fresh context) keeps the two cancellation sources composed, per rule 2: a
// client that disconnects still cancels the query immediately, and a client
// that stays connected can no longer hold it beyond the budget.
func runGraphViewQuery(ctx context.Context, engine *cypher.Engine, query string) (graphView, error) {
	// Read the budget once so the deadline that fires and the message that
	// reports it can never disagree.
	budget := graphQueryBudget
	budgeted, cancel := context.WithTimeout(ctx, budget)
	// Deferred FIRST, so it unwinds LAST: result.Close() below still runs on a
	// live context. Releasing the timer here also means the budget is strictly
	// per request and nothing outlives the call (rule 7).
	defer cancel()

	result, err := engine.Run(budgeted, query, nil)
	if err != nil {
		return graphView{}, graphExecutionError(ctx, budget, err)
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
		return graphView{}, graphExecutionError(ctx, budget, err)
	}

	return c.view(), nil
}

// graphExecutionError classifies a failure raised by the engine's read path —
// whether it surfaced from Run or from the walk over the result — as the single
// execution-failure kind, and words the user-facing reason truthfully.
//
// Every case is graphErrExecution: exhausting the query time budget is a query
// execution failure, case 3 of SPEC/WEB.md § Query-Bar Error Handling, exactly
// as a query that fails in the engine is. No new kind, no new sentinel error,
// no new HTTP status (SPEC/WEB.md § Graph Query Time Budget, rules 4 and 5).
// Only the reason differs, and it must not lie about which of the two composed
// cancellation sources fired:
//
//   - The engine wraps ctx.Err() (cypher.checkContext), so a budget exhaustion
//     arrives as context.DeadlineExceeded and a client disconnect as
//     context.Canceled, both matchable with errors.Is.
//   - DeadlineExceeded alone is not proof of the budget: it is also what a
//     parent context with its own earlier deadline reports through the derived
//     one. parent is therefore consulted — it is the REQUEST's context, without
//     the budget layered on — and only a live parent attributes the failure to
//     the budget.
//
// An ordinary engine failure (invalid Cypher, for example) keeps the exact
// message it had before the budget existed. The page renders whichever reason
// it is given verbatim in place, so all three read as the same "query failed to
// execute" message the user already knows (graph.js showQueryError).
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
