package web

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the roadmap tasks page: one paginated task list in a Tabler card,
// filtered and paginated on the server from the six query parameters the page
// accepts (SPEC/WEB.md § Roadmap Tasks Page).
//
// The flow of one request is fixed, and each step has one owner here:
//
//  1. parseTasksQuery reads the six parameters and keeps ACCEPTED values only;
//     a value that is not accepted is dropped at this boundary, so nothing
//     downstream can apply it, echo it, or put it in a link (Query parameters,
//     Validation).
//  2. readTaskList performs the page's three reads: the sprint titles, the task
//     listing narrowed by the structured filters as bound parameters, and the
//     grouped sprint resolution of the rendered rows (Read cost).
//  3. The search term, the total, the page selection, and the slicing are applied
//     in memory over the rows the listing returned (Pagination).

// The page sizes the rows-per-page selector offers, in the order it offers them,
// and the default one (SPEC/WEB.md § Roadmap Tasks Page, Pagination, Page size).
const defaultTaskPageSize = 25

var taskPageSizes = [...]int{10, 25, 50, 100}

// taskListPath is the page's own path, the target of the filter bar's form and of
// every link the page generates to itself. {name} has already passed the
// roadmap-name rules, which admit no character that needs escaping in a path; it
// is escaped anyway, so the function does not rest on that.
func taskListPath(name string) string {
	return "/roadmaps/" + url.PathEscape(name) + "/tasks"
}

// tasksQuery is what one request asked the tasks page to show, reduced to
// ACCEPTED values. A parameter whose value is not accepted leaves its field at
// the zero value, which is exactly the state its absence produces, so "an
// unacceptable value is ignored, never an error" is settled once, here
// (SPEC/WEB.md § Roadmap Tasks Page, Validation).
//
// Search is the term as the request carried it, with each byte that is not valid
// UTF-8 replaced by U+FFFD: it is what the search input echoes and what the
// generated links carry. folded is the term trimmed, normalised, and folded,
// computed once per request.
//
// SprintID is 0 when no sprint id was accepted; a sprint id is a primary key and
// never 0. SprintNone is the accepted value `none`.
//
// Page is the requested page, at least 1 and possibly beyond the last page, which
// the read clamps once the total is known. Size is always one of taskPageSizes.
type tasksQuery struct {
	Search     string
	folded     string
	Status     models.TaskStatus
	Type       models.TaskType
	SprintID   int
	Page       int
	Size       int
	SprintNone bool
}

// firstValue returns the first value of a parameter, and whether the parameter
// is present at all. url.Values holds a parameter's occurrences in query-string
// order, so the first element is the first occurrence (Validation, rule 6). A
// parameter whose percent-encoding is malformed is dropped by url.ParseQuery and
// is therefore absent here (rule 5).
func firstValue(values url.Values, key string) string {
	if v := values[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// isCanonicalDecimal reports whether s is a canonical decimal integer: one or
// more ASCII digits, the first of which is not 0, with no sign, no whitespace, and
// no other character (SPEC/WEB.md § Roadmap Tasks Page, Query parameters).
func isCanonicalDecimal(s string) bool {
	if s == "" || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parsePage reads the page parameter. A value that is not a canonical decimal
// integer falls back to 1. A canonical value too large for an int is still a page
// beyond the last one, so it saturates rather than falling back, and the read
// renders the last page for it (Validation, rule 2).
func parsePage(raw string) int {
	if !isCanonicalDecimal(raw) {
		return 1
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return math.MaxInt
		}
		return 1
	}
	return n
}

// parseSize accepts exactly the four spellings of the four page sizes and falls
// back to the default for anything else, a decorated spelling such as "025"
// included (Validation, rule 3).
func parseSize(raw string) int {
	for _, size := range taskPageSizes {
		if raw == strconv.Itoa(size) {
			return size
		}
	}
	return defaultTaskPageSize
}

// replaceInvalidUTF8 replaces each byte of s that is not part of a valid UTF-8
// sequence with U+FFFD, one replacement per byte (SPEC/WEB.md § Roadmap Tasks
// Page, No malformed term is an error).
func replaceInvalidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// parseTasksQuery reads the page's six parameters. It cannot fail: every value
// a caller can send maps to a state, so no parameter changes the route's status
// codes. sprintIDs is the set of the roadmap's own sprint ids, which decides
// whether a sprint id is accepted: an id of another roadmap, or of no sprint, is
// not (SPEC/WEB.md § Roadmap Tasks Page, Query parameters; Validation).
func parseTasksQuery(values url.Values, sprintIDs map[int]struct{}) tasksQuery {
	search := replaceInvalidUTF8(firstValue(values, "q"))
	q := tasksQuery{
		Search: search,
		folded: foldSearchTerm(search),
		Page:   parsePage(firstValue(values, "page")),
		Size:   parseSize(firstValue(values, "size")),
	}
	if raw := firstValue(values, "status"); models.IsValidTaskStatus(raw) {
		q.Status = models.TaskStatus(raw)
	}
	if raw := firstValue(values, "type"); models.IsValidTaskType(raw) {
		q.Type = models.TaskType(raw)
	}
	switch raw := firstValue(values, "sprint"); {
	case raw == "none":
		q.SprintNone = true
	case isCanonicalDecimal(raw):
		if id, err := strconv.Atoi(raw); err == nil {
			if _, ok := sprintIDs[id]; ok {
				q.SprintID = id
			}
		}
	}
	return q
}

// active reports whether the request carries at least one accepted criterion:
// an accepted filter value, or a term that is not empty after the trim. It is
// what separates a request that no task satisfies from a roadmap that holds no
// task at all (SPEC/WEB.md § Roadmap Tasks Page, Empty states).
func (q *tasksQuery) active() bool {
	return q.folded != "" || q.SprintNone || q.SprintID != 0 || q.Status != "" || q.Type != ""
}

// listFilter is the task listing's filter for the request's accepted structured
// filters: one predicate per accepted value, each value bound by the listing, and
// no predicate for an absent or ignored one (SPEC/DATABASE.md § Main SQL Queries,
// "List All"). The term, the page, and the size never reach it.
func (q *tasksQuery) listFilter() *db.TaskListFilter {
	filter := &db.TaskListFilter{NoSprint: q.SprintNone}
	if q.Status != "" {
		status := q.Status
		filter.Status = &status
	}
	if q.Type != "" {
		taskType := q.Type
		filter.TaskType = &taskType
	}
	if q.SprintID != 0 {
		sprintID := q.SprintID
		filter.SprintID = &sprintID
	}
	return filter
}

// link builds a link to the page itself carrying every accepted filter of the
// request, and page and size as given. A link carries q only when the term is
// not empty after the trim, page only when it is greater than 1, and size only
// when it is not the default; url.Values.Encode percent-encodes every value
// (SPEC/WEB.md § Roadmap Tasks Page, Links keep the filters).
func (q *tasksQuery) link(name string, page, size int) string {
	values := url.Values{}
	if q.folded != "" {
		values.Set("q", q.Search)
	}
	switch {
	case q.SprintNone:
		values.Set("sprint", "none")
	case q.SprintID != 0:
		values.Set("sprint", strconv.Itoa(q.SprintID))
	}
	if q.Status != "" {
		values.Set("status", string(q.Status))
	}
	if q.Type != "" {
		values.Set("type", string(q.Type))
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if size != defaultTaskPageSize {
		values.Set("size", strconv.Itoa(size))
	}
	if len(values) == 0 {
		return taskListPath(name)
	}
	return taskListPath(name) + "?" + values.Encode()
}

// resetLink is the target of the no-match empty state's Reset link, the page's
// only Reset control: the page's path with no filter parameter, no q, and no
// page, carrying size only when the active page size is not the default
// (SPEC/WEB.md § Roadmap Tasks Page, Empty states).
func (q *tasksQuery) resetLink(name string) string {
	return (&tasksQuery{}).link(name, 1, q.Size)
}

// matchesSearch reports whether a task matches the already-prepared term. The
// searchable text is exactly the two things the row displays that identify the
// task: its title, and its reference written with the leading "#". Matching the
// reference as the literal string "#42" is what lets both "42" and "#42" find
// task 42 under the one substring rule. No other field is searched (SPEC/WEB.md
// § Roadmap Tasks Page, The text search; Acceptance Criterion 101).
func matchesSearch(task *models.Task, folded string) bool {
	if folded == "" {
		return true
	}
	if strings.Contains(searchableText(task.Title), folded) {
		return true
	}
	// The reference is ASCII, which the normalisation and the fold leave
	// unchanged, so it is already its own prepared form.
	return strings.Contains("#"+strconv.Itoa(task.ID), folded)
}

// taskRow is one row of the list: the task and the sprint it belongs to, nil
// when it belongs to none. A task belongs to at most one sprint, which
// sprint_tasks.task_id's UNIQUE constraint guarantees.
type taskRow struct {
	Sprint *db.SprintRef
	models.Task
}

// filterOption is one option of a filter-bar select: the value the parameter
// carries, the text the option shows, and whether this request selected it.
// Value and Label are the SERVER's own strings — an enum value or a sprint of
// the roadmap — never a string the caller
// supplied; a caller's parameter only decides which option is Selected
// (SPEC/WEB.md § Roadmap Tasks Page, Escaping).
type filterOption struct {
	Value    string
	Label    string
	Selected bool
}

// taskFilterBar is the filter bar as the template renders it: the echoed term,
// the option set of each select with exactly one option selected, the active page
// size for the hidden size input, and the target of the no-match empty state's
// Reset link (the bar itself carries no Reset control).
type taskFilterBar struct {
	Search   string
	ResetURL string
	Sprints  []filterOption
	Statuses []filterOption
	Types    []filterOption
	Size     int
}

// taskPageLink is one slot of the numbered pagination bar: a page-number link, the
// active current page, or a collapsed ellipsis, in the audit log page's shape
// (SPEC/WEB.md § Roadmap Audit Log Page, Sliding window with ellipsis).
type taskPageLink struct {
	URL        string
	Number     int
	IsCurrent  bool
	IsEllipsis bool
}

// taskSizeLink is one link of the rows-per-page selector.
type taskSizeLink struct {
	URL    string
	Size   int
	Active bool
}

// tasksData is the view model of the roadmap tasks page. It is read-only.
//
// Rows are the rows of the rendered page only. Total is the number of tasks
// satisfying every accepted criterion — the filtered total — and First and Last
// are the positions of the page's first and last rows in the filtered order.
// Page is the rendered page, already clamped into 1..Pages. PrevURL and NextURL
// are empty where the chevron is disabled.
//
// NoTasks and NoMatch select the empty state the list card renders in place of
// the table and the footer when no row is rendered: a roadmap with no task
// requested with no criterion, and a request with at least one accepted criterion
// that no task satisfies (SPEC/WEB.md § Roadmap Tasks Page, Empty states).
type tasksData struct {
	Rows      []taskRow
	PageItems []taskPageLink
	SizeLinks []taskSizeLink
	Name      string
	PrevURL   string
	NextURL   string
	Chrome    chrome
	Filters   taskFilterBar
	Total     int
	First     int
	Last      int
	Page      int
	Pages     int
	NoTasks   bool
	NoMatch   bool
}

// tasksSource is the complete read surface of the roadmap tasks page: the sprint
// titles, the filtered task listing, and the grouped sprint resolution. The
// per-task and per-sprint reads, and every comment read, are deliberately absent,
// so the page cannot express one query per row and reads no comment
// (SPEC/WEB.md § Roadmap Tasks Page, Read cost; Acceptance Criteria 70, 89 and
// 92). *db.DB satisfies the interface.
type tasksSource interface {
	ListSprintTitles(ctx context.Context) ([]db.SprintRef, error)
	ListAllTasks(ctx context.Context, filter *db.TaskListFilter) ([]models.Task, error)
	taskSprintReader
}

// loadTasks reads a roadmap's tasks page read-only. It opens the roadmap
// database and hands it to readTaskList, which performs the whole read; the
// handle is released before the function returns. The caller is responsible for
// the {name} validation and existence check (resolveRoadmap).
func loadTasks(ctx context.Context, name string, values url.Values) (tasksData, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return tasksData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	return readTaskList(ctx, database, name, values)
}

// readTaskList is the tasks page's entire read, against the page's read surface
// rather than a concrete connection, so a test can count what a render costs. It
// is THREE reads when the rendered page holds a row and TWO when it holds none:
// the sprint titles, the filtered task listing, and — only for a non-empty page —
// the grouped sprint resolution over the rendered rows' ids (SPEC/WEB.md
// § Roadmap Tasks Page, Read cost).
func readTaskList(ctx context.Context, src tasksSource, name string, values url.Values) (tasksData, error) {
	sprints, err := src.ListSprintTitles(ctx)
	if err != nil {
		return tasksData{}, err
	}
	sprintIDs := make(map[int]struct{}, len(sprints))
	for _, sprint := range sprints {
		sprintIDs[sprint.ID] = struct{}{}
	}

	q := parseTasksQuery(values, sprintIDs)

	// Every task the structured filters admit, with no LIMIT and no OFFSET: the
	// term is applied below, and the page after the term, so the read is bounded
	// by the filters alone and the total is correct by construction.
	tasks, err := src.ListAllTasks(ctx, q.listFilter())
	if err != nil {
		return tasksData{}, err
	}

	// The search removes rows from the listing's order and never reorders the
	// rows that remain; filtering in place keeps that order.
	matched := tasks[:0]
	for i := range tasks {
		if matchesSearch(&tasks[i], q.folded) {
			matched = append(matched, tasks[i])
		}
	}

	total := len(matched)
	pages := 1
	if total > 0 {
		pages = (total + q.Size - 1) / q.Size
	}
	page := min(q.Page, pages)

	data := tasksData{
		Name:    name,
		Filters: newTaskFilterBar(&q, sprints, name),
		Total:   total,
		Page:    page,
		Pages:   pages,
	}

	if total == 0 {
		data.NoMatch = q.active()
		data.NoTasks = !data.NoMatch
		return data, nil
	}

	start := (page - 1) * q.Size
	end := min(start+q.Size, total)
	rows := make([]taskRow, 0, end-start)
	ids := make([]int, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, taskRow{Task: matched[i]})
		ids = append(ids, matched[i].ID)
	}

	resolved, err := src.GetSprintsByTasks(ctx, ids)
	if err != nil {
		return tasksData{}, err
	}
	for i := range rows {
		if sprint, ok := resolved[rows[i].ID]; ok {
			rows[i].Sprint = &sprint
		}
	}

	data.Rows = rows
	data.First = start + 1
	data.Last = end
	data.PageItems = taskPageLinks(&q, name, page, pages)
	if page > 1 {
		data.PrevURL = q.link(name, page-1, q.Size)
	}
	if page < pages {
		data.NextURL = q.link(name, page+1, q.Size)
	}
	data.SizeLinks = make([]taskSizeLink, 0, len(taskPageSizes))
	for _, size := range taskPageSizes {
		data.SizeLinks = append(data.SizeLinks, taskSizeLink{
			URL:    q.link(name, 1, size),
			Size:   size,
			Active: size == q.Size,
		})
	}
	return data, nil
}

// taskPageLinks is the numbered bar's slots, from the audit log page's one
// sliding-window helper, each page number carrying its link.
func taskPageLinks(q *tasksQuery, name string, page, pages int) []taskPageLink {
	items := paginationItems(page, pages)
	links := make([]taskPageLink, len(items))
	for i, item := range items {
		links[i] = taskPageLink{Number: item.Number, IsCurrent: item.IsCurrent, IsEllipsis: item.IsEllipsis}
		if !item.IsEllipsis && !item.IsCurrent {
			links[i].URL = q.link(name, item.Number, q.Size)
		}
	}
	return links
}

// newTaskFilterBar builds the filter bar for one request: every option set from
// the server's own enumeration — the roadmap's sprints in ascending sprint Order,
// the TaskStatus values in state-machine order, and the TaskType values in enum
// order — with the one option the request's accepted
// value selects, or the first option when the parameter is absent or ignored.
func newTaskFilterBar(q *tasksQuery, sprints []db.SprintRef, name string) taskFilterBar {
	bar := taskFilterBar{
		Search:   q.Search,
		ResetURL: q.resetLink(name),
		Size:     q.Size,
	}

	bar.Sprints = make([]filterOption, 0, len(sprints)+2)
	bar.Sprints = append(bar.Sprints,
		filterOption{Label: "Any sprint", Selected: !q.SprintNone && q.SprintID == 0},
		filterOption{Value: "none", Label: "No sprint", Selected: q.SprintNone},
	)
	for _, sprint := range sprints {
		id := strconv.Itoa(sprint.ID)
		bar.Sprints = append(bar.Sprints, filterOption{
			Value:    id,
			Label:    "Sprint #" + id + " " + sprint.Title,
			Selected: sprint.ID == q.SprintID,
		})
	}

	bar.Statuses = make([]filterOption, 0, len(models.ValidTaskStatuses)+1)
	bar.Statuses = append(bar.Statuses, filterOption{Label: "Any status", Selected: q.Status == ""})
	for _, status := range models.ValidTaskStatuses {
		bar.Statuses = append(bar.Statuses, filterOption{
			Value: string(status), Label: string(status), Selected: status == q.Status,
		})
	}

	bar.Types = make([]filterOption, 0, len(models.ValidTaskTypes)+1)
	bar.Types = append(bar.Types, filterOption{Label: "Any type", Selected: q.Type == ""})
	for _, taskType := range models.ValidTaskTypes {
		bar.Types = append(bar.Types, filterOption{
			Value: string(taskType), Label: string(taskType), Selected: taskType == q.Type,
		})
	}
	return bar
}
