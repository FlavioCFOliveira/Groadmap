package web

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the roadmap tasks page: one paginated task list in a Tabler card,
// filtered and paginated on the server from the six query parameters the page
// accepts, or from the filter-state cookie, or from the defaults (SPEC/WEB.md
// § Roadmap Tasks Page).
//
// The flow of one request is fixed, and each step has one owner here:
//
//  1. newTasksRequest classifies the request as explicit (its URL carries at
//     least one of the six parameters) or bare, and captures the URL's
//     parameters and the filter-state cookie (Filter persistence).
//  2. resolveTasksQuery turns that into the ACTIVE filter state: the URL's
//     parameters for an explicit request, the cookie's for a bare one that
//     carries it, the defaults otherwise. parseTasksQuery keeps ACCEPTED values
//     only; a value that is not accepted is dropped at this boundary, so nothing
//     downstream can apply it, echo it, or put it in a link (Query parameters,
//     Validation).
//  3. readTaskList performs the page's reads: the sprint titles, the task listing
//     narrowed by the structured filters as bound parameters, and the task count
//     only when the filtered list is empty and the listing carried a predicate.
//     The page shows no task's sprint, so it resolves none (Read cost).
//  4. The search term, the total, the page selection, and the slicing are applied
//     in memory over the rows the listing returned (Pagination).
//  5. handleTasks writes the cookie for an explicit request, and declares the
//     response's dependency on the Cookie header (Cache Policy, rule 5).

// The page sizes the rows-per-page selector offers, in the order it offers them,
// and the default one (SPEC/WEB.md § Roadmap Tasks Page, Pagination, Page size).
const defaultTaskPageSize = 25

var taskPageSizes = [...]int{10, 25, 50, 100}

// The filter-state cookie: its name, its lifetime (one year, in seconds), and the
// largest encoded value the server writes (SPEC/WEB.md § Roadmap Tasks Page,
// Filter persistence, The cookie and The size limit).
const (
	tasksFilterCookie       = "rmp_tasks_filters"
	tasksFilterCookieMaxAge = 31536000
	maxTasksFilterCookieLen = 4000
)

// defaultTaskStatuses is the default status selection: every TaskStatus value
// except COMPLETED, in the state machine's order (SPEC/WEB.md § Roadmap Tasks
// Page, Filter persistence, The defaults). The default state filters no type, no
// sprint, and no term, at the default page size.
var defaultTaskStatuses = []models.TaskStatus{
	models.StatusBacklog, models.StatusSprint, models.StatusDoing, models.StatusTesting,
}

// tasksPageParams are the six parameters the page accepts. A request whose URL
// carries at least one occurrence of any of them is explicit (Filter
// persistence, Explicit and bare requests).
var tasksPageParams = [...]string{"q", "sprint", "status", "type", "page", "size"}

// taskListPath is the page's own path, the target of the filter bar's form and of
// every link the page generates to itself. {name} has already passed the
// roadmap-name rules, which admit no character that needs escaping in a path; it
// is escaped anyway, so the function does not rest on that.
func taskListPath(name string) string {
	return "/roadmaps/" + url.PathEscape(name) + "/tasks"
}

// tasksQuery is the ACTIVE filter state of one response, reduced to ACCEPTED
// values: the q, sprint, status, type, and size the list is produced from, and the
// requested page. A value that is not accepted leaves its field at the zero value,
// which is exactly the state its absence produces, so "an unacceptable value is
// ignored, never an error" is settled once, here (SPEC/WEB.md § Roadmap Tasks
// Page, Validation).
//
// Search is the term as the request carried it, with each byte that is not valid
// UTF-8 replaced by U+FFFD: it is what the search input echoes and what the
// generated links and the cookie carry. folded is the term trimmed, normalised,
// and folded, computed once per request.
//
// Statuses and Types are the accepted values of the two repeatable parameters,
// each value once, in enum order; an empty slice filters nothing.
//
// SprintID is 0 when no sprint id was accepted; a sprint id is a primary key and
// never 0. SprintNone is the accepted value `none`.
//
// Page is the requested page, at least 1 and possibly beyond the last page, which
// the read clamps once the total is known. Size is always one of taskPageSizes.
type tasksQuery struct {
	Search     string
	folded     string
	Statuses   []models.TaskStatus
	Types      []models.TaskType
	SprintID   int
	Page       int
	Size       int
	SprintNone bool
}

// tasksRequest is what one request brings to the tasks page: its URL parameters,
// whether it is explicit, and the filter-state cookie's raw value, if it carries
// the cookie (SPEC/WEB.md § Roadmap Tasks Page, Filter persistence).
type tasksRequest struct {
	values    url.Values
	cookie    string
	explicit  bool
	hasCookie bool
}

// newTasksRequest captures the tasks page's inputs from an HTTP request. The
// cookie is read only for a bare request, because an explicit request takes its
// state from the URL alone.
func newTasksRequest(r *http.Request) tasksRequest {
	req := tasksRequest{values: r.URL.Query(), explicit: isExplicitTasksQuery(r.URL.RawQuery)}
	if !req.explicit {
		req.cookie, req.hasCookie = firstCookieValue(r.Header, tasksFilterCookie)
	}
	return req
}

// isExplicitTasksQuery reports whether a raw query string carries at least one
// occurrence of a parameter the page accepts, whatever that occurrence's value —
// empty, unaccepted, or undecodable included. The query string is split on "&" as
// url.ParseQuery splits it, and each name is percent-decoded, so a name spelled
// with an escape still counts; a name that cannot be decoded is not one of the
// six (SPEC/WEB.md § Roadmap Tasks Page, Filter persistence, Explicit and bare
// requests).
func isExplicitTasksQuery(rawQuery string) bool {
	for rawQuery != "" {
		var part string
		part, rawQuery, _ = strings.Cut(rawQuery, "&")
		key, _, _ := strings.Cut(part, "=")
		name, err := url.QueryUnescape(key)
		if err != nil {
			continue
		}
		if slices.Contains(tasksPageParams[:], name) {
			return true
		}
	}
	return false
}

// firstCookieValue returns the raw value of the FIRST cookie named name that the
// request's Cookie headers carry, and whether one is present. It reads the header
// itself rather than through http.Request.Cookie, which skips an occurrence whose
// value holds a byte it rejects and would then return a LATER occurrence: the
// page reads the first occurrence whatever its content, and validates that
// content itself (SPEC/WEB.md § Roadmap Tasks Page, Filter persistence, Reading
// the cookie validates it like a URL). A value wrapped in double quotes, which
// RFC 6265's cookie-value grammar admits, is unwrapped.
func firstCookieValue(header http.Header, name string) (string, bool) {
	for _, line := range header.Values("Cookie") {
		for line != "" {
			var pair string
			pair, line, _ = strings.Cut(line, ";")
			key, value, found := strings.Cut(strings.Trim(pair, " \t"), "=")
			if !found || strings.Trim(key, " \t") != name {
				continue
			}
			value = strings.Trim(value, " \t")
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				value = value[1 : len(value)-1]
			}
			return value, true
		}
	}
	return "", false
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

// parseTasksQuery reads the page's parameters from a set of values — the URL's,
// or the cookie's. It cannot fail: every value a caller can send maps to a state,
// so no parameter changes the route's status codes. sprintIDs is the set of the
// roadmap's own sprint ids, which decides whether a sprint id is accepted: an id
// of another roadmap, or of no sprint, is not (SPEC/WEB.md § Roadmap Tasks Page,
// Query parameters; Validation).
//
// status and type are repeatable: every occurrence is read and validated on its
// own, an unaccepted one is ignored without affecting the others, and a value
// that occurs more than once counts once. The accepted values are kept in enum
// order, the order the dropdowns list them in.
func parseTasksQuery(values url.Values, sprintIDs map[int]struct{}) tasksQuery {
	search := replaceInvalidUTF8(firstValue(values, "q"))
	q := tasksQuery{
		Search: search,
		folded: foldSearchTerm(search),
		Page:   parsePage(firstValue(values, "page")),
		Size:   parseSize(firstValue(values, "size")),
	}
	for _, status := range models.ValidTaskStatuses {
		if slices.Contains(values["status"], string(status)) {
			q.Statuses = append(q.Statuses, status)
		}
	}
	for _, taskType := range models.ValidTaskTypes {
		if slices.Contains(values["type"], string(taskType)) {
			q.Types = append(q.Types, taskType)
		}
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

// resolveTasksQuery is the active filter state of a request (SPEC/WEB.md
// § Roadmap Tasks Page, Filter persistence):
//   - an explicit request takes its state from the URL alone;
//   - a bare request carrying the cookie takes it from the cookie's value, parsed
//     as a query string and validated by exactly the URL's rules for THIS
//     roadmap, a cookie with no accepted part giving the state with no filter;
//   - a bare request without the cookie takes the defaults.
//
// A bare request always renders page 1: the cookie never carries a page, and a
// page part in it is ignored.
func resolveTasksQuery(req *tasksRequest, sprintIDs map[int]struct{}) tasksQuery {
	switch {
	case req.explicit:
		return parseTasksQuery(req.values, sprintIDs)
	case req.hasCookie:
		// url.ParseQuery drops each part it cannot decode and keeps the others,
		// which is the per-part rule the cookie is read by; the error it returns
		// for a dropped part is therefore not a failure of the read.
		values, _ := url.ParseQuery(req.cookie) //nolint:errcheck // malformed parts are ignored one by one, as in a URL
		q := parseTasksQuery(values, sprintIDs)
		q.Page = 1
		return q
	default:
		return tasksQuery{
			Statuses: slices.Clone(defaultTaskStatuses),
			Page:     1,
			Size:     defaultTaskPageSize,
		}
	}
}

// hasPredicate reports whether the task read carries at least one filter
// predicate — a sprint, status, or type filter. The term is not a predicate: it is
// applied in memory after the read (SPEC/WEB.md § Roadmap Tasks Page, Read cost).
func (q *tasksQuery) hasPredicate() bool {
	return q.SprintNone || q.SprintID != 0 || len(q.Statuses) > 0 || len(q.Types) > 0
}

// listFilter is the task listing's filter for the active structured filters: one
// predicate per filtered dimension, each distinct value bound by the listing, and
// no predicate for an absent or ignored one (SPEC/DATABASE.md § Main SQL Queries,
// "List All"). The term, the page, and the size never reach it.
func (q *tasksQuery) listFilter() *db.TaskListFilter {
	filter := &db.TaskListFilter{
		NoSprint:  q.SprintNone,
		Statuses:  slices.Clone(q.Statuses),
		TaskTypes: slices.Clone(q.Types),
	}
	if q.SprintID != 0 {
		sprintID := q.SprintID
		filter.SprintID = &sprintID
	}
	return filter
}

// encode writes the active filter state as an application/x-www-form-urlencoded
// query string, in the fixed order q, sprint, status, type, page, size. q is
// written only when the term is not empty after the trim, one status and one
// type per active value, page only when it is greater than 1, and size when it is
// not the default or when alwaysSize is set. url.QueryEscape percent-encodes
// every value, a space as "+", so the result holds only ASCII letters and digits
// and the characters - . _ ~ % + & = (SPEC/WEB.md § Roadmap Tasks Page, Links
// keep the filters; Filter persistence, The cookie's value).
func (q *tasksQuery) encode(page, size int, alwaysSize bool) string {
	var b strings.Builder
	add := func(name, value string) {
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(value))
	}
	if q.folded != "" {
		add("q", q.Search)
	}
	switch {
	case q.SprintNone:
		add("sprint", "none")
	case q.SprintID != 0:
		add("sprint", strconv.Itoa(q.SprintID))
	}
	for _, status := range q.Statuses {
		add("status", string(status))
	}
	for _, taskType := range q.Types {
		add("type", string(taskType))
	}
	if page > 1 {
		add("page", strconv.Itoa(page))
	}
	if alwaysSize || size != defaultTaskPageSize {
		add("size", strconv.Itoa(size))
	}
	return b.String()
}

// link builds a link to the page itself carrying every filter of the active
// filter state, and page and size as given. A generated link is always an
// explicit request, so a link these rules would leave with no parameter at all
// carries size=25, and following it reproduces the list it names rather than the
// stored state (SPEC/WEB.md § Roadmap Tasks Page, Links keep the filters).
func (q *tasksQuery) link(name string, page, size int) string {
	query := q.encode(page, size, false)
	if query == "" {
		query = "size=" + strconv.Itoa(defaultTaskPageSize)
	}
	return taskListPath(name) + "?" + query
}

// cookieValue is the filter-state cookie's value for this state: q, sprint, one
// status and one type per active value, and size, always; never page
// (SPEC/WEB.md § Roadmap Tasks Page, Filter persistence, The cookie's value).
func (q *tasksQuery) cookieValue() string {
	return q.encode(1, q.Size, true)
}

// resetLink is the target of the no-match empty state's Reset link, the page's
// only Reset control: the page's path carrying the default filter state written
// explicitly — the four default status values — with no sprint, no type, no q,
// and no page, carrying size only when the active page size is not the default
// (SPEC/WEB.md § Roadmap Tasks Page, Empty states).
func (q *tasksQuery) resetLink(name string) string {
	defaults := tasksQuery{Statuses: defaultTaskStatuses}
	return defaults.link(name, 1, q.Size)
}

// filterCookie is the Set-Cookie of an explicit request's HTTP 200 response, or
// nil when the encoded value would exceed maxTasksFilterCookieLen, in which case
// no cookie is written and the one the browser holds stays as it was. Its
// attributes are exactly Path=/, Max-Age one year, HttpOnly, and SameSite=Lax: no
// Domain (a host-only cookie), no Expires, and no Secure, because the server
// speaks plain HTTP and a Secure cookie would never be sent back (SPEC/WEB.md
// § Roadmap Tasks Page, Filter persistence, The cookie and The size limit).
func filterCookie(value string) *http.Cookie {
	if len(value) > maxTasksFilterCookieLen {
		return nil
	}
	return &http.Cookie{ // #nosec G124 -- Secure is deliberately unset: rmp web serves plain HTTP only, and a user agent returns a Secure cookie only over a secure channel (RFC 6265 § 4.1.2.5), so it would never be sent back; HttpOnly and SameSite=Lax are set (SPEC/WEB.md § Roadmap Tasks Page, Filter persistence)
		Name:     tasksFilterCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   tasksFilterCookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

// matchesSearch reports whether a task matches the already-prepared term. The
// searchable text is exactly the two things the row displays that identify the
// task: its title, and its reference written with the leading "#". Matching the
// reference as the literal string "#42" is what lets both "42" and "#42" find
// task 42 under the one substring rule. No other field is searched (SPEC/WEB.md
// § Roadmap Tasks Page, The text search; Acceptance Criterion 101).
func matchesSearch(task *db.TaskRef, folded string) bool {
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

// filterOption is one option of the sprint select: the value the parameter
// carries, the text the option shows, and whether the active state selects it.
// Value and Label are the SERVER's own strings — a sprint of the roadmap — never a
// string the caller supplied; a caller's value only decides which option is
// Selected (SPEC/WEB.md § Roadmap Tasks Page, Escaping).
type filterOption struct {
	Value    string
	Label    string
	Selected bool
}

// filterCheck is one checkbox of a multi-select dropdown's menu: an enum value,
// which is both the box's value and its label's text, and whether it is one of
// the dimension's active values. Value is the server's own enumeration, never a
// string the caller supplied (SPEC/WEB.md § Roadmap Tasks Page, Multi-select
// dropdowns; Escaping).
type filterCheck struct {
	Value   string
	Checked bool
}

// taskFilterBar is the filter bar as the template renders it: the echoed term,
// the sprint select's options with exactly one selected, each dropdown's
// checkboxes and toggle text, the active page size for the hidden size input, and
// the target of the no-match empty state's Reset link (the bar itself carries no
// Reset control).
type taskFilterBar struct {
	Search     string
	ResetURL   string
	StatusText string
	TypeText   string
	Sprints    []filterOption
	Statuses   []filterCheck
	Types      []filterCheck
	Size       int
}

// toggleText is a dropdown toggle's text, computed from the dimension's active
// values: the any text when there is none, the value itself when there is one,
// and "<n> selected" when there are two or more (SPEC/WEB.md § Roadmap Tasks Page,
// Multi-select dropdowns).
func toggleText[T ~string](active []T, anyText string) string {
	switch len(active) {
	case 0:
		return anyText
	case 1:
		return string(active[0])
	default:
		return strconv.Itoa(len(active)) + " selected"
	}
}

// filterChecks is one dropdown's checkboxes: every value of the enum, in the
// enum's order, each checked exactly when it is an active value.
func filterChecks[T ~string](all, active []T) []filterCheck {
	checks := make([]filterCheck, 0, len(all))
	for _, value := range all {
		checks = append(checks, filterCheck{Value: string(value), Checked: slices.Contains(active, value)})
	}
	return checks
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
// Rows are the tasks of the rendered page only. Total is the number of tasks
// satisfying every active criterion — the filtered total — and First and Last
// are the positions of the page's first and last rows in the filtered order.
// Page is the rendered page, already clamped into 1..Pages. PrevURL and NextURL
// are empty where the chevron is disabled.
//
// NoTasks and NoMatch select the empty state the list card renders in place of
// the table and the footer when no row is rendered: a roadmap that holds no task,
// whatever the active filter state, and a roadmap holding at least one task, none
// of which satisfies the active criteria (SPEC/WEB.md § Roadmap Tasks Page, Empty
// states).
//
// query is the active filter state the page was produced from; the handler
// writes it to the filter-state cookie for an explicit request.
type tasksData struct {
	Rows      []db.TaskRow
	PageItems []taskPageLink
	SizeLinks []taskSizeLink
	Name      string
	PrevURL   string
	NextURL   string
	Chrome    chrome
	Filters   taskFilterBar
	query     tasksQuery
	Total     int
	First     int
	Last      int
	Page      int
	Pages     int
	NoTasks   bool
	NoMatch   bool
}

// tasksSource is the complete read surface of the roadmap tasks page: the sprint
// titles, the task listing with the page-rows read of the selected page, and the
// roadmap's task count. The per-task
// and per-sprint reads, the sprint resolution, and every comment read are
// deliberately absent, so the page cannot express one query per row, resolves no
// task's sprint, and reads no comment (SPEC/WEB.md § Roadmap Tasks Page, Read
// cost; Acceptance Criteria 70, 89 and 92). *db.DB satisfies the interface.
type tasksSource interface {
	ListSprintTitles(ctx context.Context) ([]db.SprintRef, error)
	ReadTaskListPage(ctx context.Context, filter *db.TaskListFilter, withTitle bool,
		selectPage func(listing []db.TaskRef) []int) ([]db.TaskRow, error)
	CountTasks(ctx context.Context) (int, error)
}

// loadTasks reads a roadmap's tasks page read-only. It opens the roadmap
// database and hands it to readTaskList, which performs the whole read; the
// handle is released before the function returns. The caller is responsible for
// the {name} validation and existence check (resolveRoadmap).
func loadTasks(ctx context.Context, name string, req *tasksRequest) (tasksData, error) {
	database, err := db.OpenReadOnly(name)
	if err != nil {
		return tasksData{}, err
	}
	defer database.Close() //nolint:errcheck // read-only handle; close error is non-actionable

	return readTaskList(ctx, database, name, req)
}

// readTaskList is the tasks page's entire read, against the page's read surface
// rather than a concrete connection, so a test can count what a render costs. It
// is the sprint titles, the lean task listing, and the page-rows read of the
// selected page's tasks — the last issued only when the filtered list holds a
// row — with the listing and the page-rows read in one read transaction; and
// the roadmap's task count, only when the filtered list is empty and the
// listing carried at least one predicate (SPEC/WEB.md § Roadmap Tasks Page, Read
// cost).
func readTaskList(ctx context.Context, src tasksSource, name string, req *tasksRequest) (tasksData, error) {
	sprints, err := src.ListSprintTitles(ctx)
	if err != nil {
		return tasksData{}, err
	}
	sprintIDs := make(map[int]struct{}, len(sprints))
	for _, sprint := range sprints {
		sprintIDs[sprint.ID] = struct{}{}
	}

	q := resolveTasksQuery(req, sprintIDs)

	// The task listing holds every task the structured filters admit, with no
	// LIMIT and no OFFSET: the term is applied in selectPage, and the page after
	// the term, so the listing is bounded by the filters alone and the total is
	// correct by construction. Only the selected page's ids reach the page-rows
	// read.
	//
	// The listing carries each task's title only when the request carries a term
	// to match it against — a q that is not empty after the trim, the one test
	// matchesSearch applies — and its id alone otherwise: the rows the page shows
	// take their titles from the page-rows read (SPEC/WEB.md § Roadmap Tasks Page,
	// Read cost).
	var read, total, page, pages, start, end int
	rows, err := src.ReadTaskListPage(ctx, q.listFilter(), q.folded != "", func(listing []db.TaskRef) []int {
		read = len(listing)

		// The search removes rows from the listing's order and never reorders the
		// rows that remain; filtering in place keeps that order.
		matched := listing[:0]
		for i := range listing {
			if matchesSearch(&listing[i], q.folded) {
				matched = append(matched, listing[i])
			}
		}

		total = len(matched)
		pages = 1
		if total > 0 {
			pages = (total + q.Size - 1) / q.Size
		}
		page = min(q.Page, pages)
		start = (page - 1) * q.Size
		end = min(start+q.Size, total)

		ids := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			ids = append(ids, matched[i].ID)
		}
		return ids
	})
	if err != nil {
		return tasksData{}, err
	}

	data := tasksData{
		Name:    name,
		Filters: newTaskFilterBar(&q, sprints, name),
		query:   q,
		Total:   total,
		Page:    page,
		Pages:   pages,
	}

	if total == 0 {
		// Which empty state: whether the roadmap holds any task. A listing with no
		// predicate returned every task of the roadmap, so its own row count
		// answers; a listing with one needs the count, issued here and only here.
		held := read
		if q.hasPredicate() {
			if held, err = src.CountTasks(ctx); err != nil {
				return tasksData{}, err
			}
		}
		data.NoTasks = held == 0
		data.NoMatch = !data.NoTasks
		return data, nil
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

// newTaskFilterBar builds the filter bar for the active filter state: the sprint
// select's options from the roadmap's sprints in ascending sprint Order, with the
// one option the active sprint selects, or the first option when there is none;
// and each dropdown's checkboxes from its enum — the TaskStatus values in
// state-machine order, the TaskType values in enum order — checked exactly for
// the dimension's active values, with the toggle text those values give.
func newTaskFilterBar(q *tasksQuery, sprints []db.SprintRef, name string) taskFilterBar {
	bar := taskFilterBar{
		Search:     q.Search,
		ResetURL:   q.resetLink(name),
		StatusText: toggleText(q.Statuses, "Any status"),
		TypeText:   toggleText(q.Types, "Any type"),
		Statuses:   filterChecks(models.ValidTaskStatuses, q.Statuses),
		Types:      filterChecks(models.ValidTaskTypes, q.Types),
		Size:       q.Size,
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
	return bar
}
