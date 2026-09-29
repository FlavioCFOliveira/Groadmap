package web

import (
	"html"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
)

// This file is the gate for the ways a task links to its own page — the card of
// the sprint page's board and the two links of each row of the tasks page's list —
// and for the removal of the task detail modal, its script, and its JSON endpoint
// (SPEC/WEB.md § Sprint Detail Sub-Template, The card is a link to the task page;
// § Roadmap Tasks Page, Links to the task page; § Routes and Pages, rule 5;
// Acceptance Criteria 86, 93, 96, and 135).
//
// A link with an href is natively focusable and natively activatable: a click, a
// tap, and Enter follow it with no script, and a middle click opens it in a new
// tab. That is why the card and the row's two links are <a> elements and why
// nothing else — no <button>, no <div> or <tr> carrying a role and a tabindex —
// stands in for them.

// reOpeningTag captures the tag name and the attribute text of every opening tag
// in a document.
var reOpeningTag = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)\b([^>]*)>`)

// taskCardPaths are the surfaces that render a task card: the sprint page's
// member-tasks board. The tasks page renders rows, not cards, and is covered by the
// list tests below.
func taskCardPaths(name string, sprintID int) []string {
	return []string{
		"/roadmaps/" + name + "/sprints/" + itoa(sprintID),
	}
}

// taskPaths are both pages that show many tasks, each linking every task to its
// own page: the tasks page's list and the sprint page's board.
func taskPaths(name string, sprintID int) []string {
	return []string{
		"/roadmaps/" + name + "/tasks",
		"/roadmaps/" + name + "/sprints/" + itoa(sprintID),
	}
}

// renderedTitleOf returns a task's title as the page must render it in an
// attribute: the stored title, escaped the way html/template escapes a value in
// any context. The title is read from the ROADMAP, not from the markup under
// test, so the assertion is not circular.
func renderedTitleOf(t *testing.T, roadmap string, taskID int) string {
	t.Helper()

	for id, title := range roadmapTaskTitles(t, roadmap) {
		if id == taskID {
			return rendered(title)
		}
	}
	t.Fatalf("roadmap %q has no task #%d to read a title from", roadmap, taskID)
	return ""
}

// wantAccessibleName is the accessible name every task card must carry: the task
// reference, so the name identifies the task, followed by the task's title, so
// the name CONTAINS the card's visible label. A control whose accessible name
// omits its visible label fails WCAG 2.5.3 (Label in Name, Level A).
func wantAccessibleName(taskID, renderedTitle string) string {
	return `aria-label="Open details for task #` + taskID + `: ` + renderedTitle + `"`
}

// cardTags returns the attribute text of every opening tag in a board region that
// carries the task-card class.
func cardTags(region string) (tags []string, names []string) {
	for _, m := range reOpeningTag.FindAllStringSubmatch(region, -1) {
		if class := classAttrRe.FindStringSubmatch(m[2]); class != nil && hasClassToken(class[1], "task-card") {
			names = append(names, strings.ToLower(m[1]))
			tags = append(tags, m[2])
		}
	}
	return tags, names
}

// TestTaskCards_AreLinksToTheirTaskPage is the gate for Acceptance Criteria 93 and
// 135: on the sprint board every task card is ONE <a> carrying the classes card
// and card-link and the href /roadmaps/{name}/tasks/{id} of its own task, named
// `Open details for task #<id>: <title>`, with no tabindex and no role, holding no
// nested link; no card is a <button>, a <div>, or a <tr>; no element carrying
// role="button" or tabindex stands in for a card; and no <tr> links to a task.
func TestTaskCards_AreLinksToTheirTaskPage(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "checkout-platform")
	mux := buildMux()
	titles := roadmapTaskTitles(t, f.name)

	reHref := regexp.MustCompile(`href="/roadmaps/` + regexp.QuoteMeta(f.name) + `/tasks/(\d+)"`)

	for _, path := range taskCardPaths(f.name, f.openID) {
		body := servePage(t, mux, path)
		region := boardRegion(t, body)

		tags, names := cardTags(region)
		if len(tags) == 0 {
			t.Fatalf("%s: no task card found; the extraction is broken or the board is empty", path)
		}
		for i, attrs := range tags {
			if names[i] != "a" {
				t.Errorf("%s: a task card is a <%s>, want an <a>\nattrs: %s", path, names[i], attrs)
				continue
			}
			if !strings.HasPrefix(attrs, ` class="card card-sm card-link text-reset task-card" href="`) {
				t.Errorf("%s: a task card does not carry Tabler's card and card-link classes followed by "+
					"its href\nattrs: %s", path, attrs)
			}
			m := reHref.FindStringSubmatch(attrs)
			if m == nil {
				t.Errorf("%s: a task card carries no href to a task page of this roadmap\nattrs: %s", path, attrs)
				continue
			}
			id := m[1]
			if _, ok := titles[atoi(t, id)]; !ok {
				t.Errorf("%s: a card links to task #%s, which the roadmap does not hold", path, id)
			}
			if want := wantAccessibleName(id, renderedTitleOf(t, f.name, atoi(t, id))); !strings.Contains(attrs, want) {
				t.Errorf("%s: task #%s's card does not carry the accessible name %s\nattrs: %s", path, id, want, attrs)
			}
			for _, redundant := range []string{"tabindex=", "role=", "data-bs-toggle", "data-bs-target"} {
				if strings.Contains(attrs, redundant) {
					t.Errorf("%s: task #%s's card carries %s\nattrs: %s", path, id, redundant, attrs)
				}
			}
			// An id-only name does not satisfy the criterion: the title must follow.
			if strings.Contains(attrs, `aria-label="Open details for task #`+id+`"`) {
				t.Errorf("%s: task #%s's card is named by its id alone", path, id)
			}
		}

		// Every card holds exactly one link, itself: its sprint indicator stays
		// plain text.
		for _, card := range strings.Split(region, cardOpen)[1:] {
			if end := strings.Index(card, "</a>"); end >= 0 && strings.Contains(card[:end], "<a ") {
				t.Errorf("%s: a task card holds a nested link", path)
			}
		}

		// Nothing that is not a link stands in for a card.
		for _, m := range reOpeningTag.FindAllStringSubmatch(body, -1) {
			attrs := m[2]
			if strings.Contains(attrs, `role="button"`) {
				t.Errorf("%s: an element carries role=\"button\": <%s%s>", path, m[1], attrs)
			}
			if strings.Contains(attrs, "tabindex=") {
				t.Errorf("%s: an element carries a tabindex: <%s%s>", path, m[1], attrs)
			}
		}
		for _, gone := range []string{"<tr", `<button type="button" class="card`} {
			if strings.Contains(body, gone) {
				t.Errorf("%s: the page carries %q; no task card is a table row or a button", path, gone)
			}
		}
	}
}

// TestTaskCards_FollowTheirHrefToTheTaskPage proves the href of every card is a
// working navigation to that task's own page: an ordinary GET that answers 200
// with the page whose pretitle names that task (Acceptance Criteria 15 and 86).
func TestTaskCards_FollowTheirHrefToTheTaskPage(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "checkout-platform")
	mux := buildMux()

	reHref := regexp.MustCompile(`class="card card-sm card-link text-reset task-card" href="([^"]+)"`)
	for _, path := range taskCardPaths(f.name, f.openID) {
		hrefs := reHref.FindAllStringSubmatch(servePage(t, mux, path), -1)
		if len(hrefs) == 0 {
			t.Fatalf("%s: no card href found", path)
		}
		for _, m := range hrefs {
			page := servePage(t, mux, m[1])
			id := m[1][strings.LastIndex(m[1], "/")+1:]
			if !strings.Contains(page, `<div class="page-pretitle">Task #`+id+` <span class="badge `) {
				t.Errorf("%s: following %s does not serve task #%s's page", path, m[1], id)
			}
		}
	}
}

// TestTaskCards_ShowAVisibleFocusIndicator is the structural gate for the focus
// half of Acceptance Criterion 93: the card links carry the task-card class on
// the sprint board, and the project stylesheet sets an outline on that class's
// :focus-visible state, which the unfocused card does not carry. The vendored
// card-link rule gives a focused card no indicator of its own, which the second
// assertion confirms so the rule is not redundant. The outline's 3:1 contrast is
// gated by TestFocusIndicatorContrast (Acceptance Criterion 247).
func TestTaskCards_ShowAVisibleFocusIndicator(t *testing.T) {
	css := readEmbeddedAsset(t, "static/style.css")
	rule := ".task-card:focus-visible { outline: 2px solid #dce1e7; outline-offset: -2px; }"
	if !strings.Contains(css, rule) {
		t.Errorf("static/style.css carries no focus-visible outline for the task card; want %q", rule)
	}
	if got := regexp.MustCompile(`\.task-card\s*\{[^}]*outline`).FindString(css); got != "" {
		t.Errorf("the unfocused card carries an outline too, so focus is not distinguished: %q", got)
	}
	tabler := readEmbeddedAsset(t, "static/vendor/tabler/tabler.min.css")
	if strings.Contains(tabler, ".card-link:focus-visible") || strings.Contains(tabler, ".card-link:focus{") {
		t.Log("the vendored distribution now styles a focused card-link; the project rule remains the one pinned")
	}

	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "checkout-platform")
	mux := buildMux()
	for _, path := range taskCardPaths(f.name, f.openID) {
		if !strings.Contains(servePage(t, mux, path), cardOpen) {
			t.Errorf("%s: the card link does not carry the task-card class the focus rule selects", path)
		}
	}
}

// TestTaskCards_AddNoScriptAndKeepTheContentSecurityPolicy pins that the links to
// a task need no JavaScript: the tasks page and the sprint page each carry exactly
// the policy of Acceptance Criterion 33, every script they load comes from
// /static/, none is inline, and neither loads a script whose purpose is to show a
// task. The tasks page loads the admin shell's script alone (Acceptance Criteria
// 93, 96, 107, 122 and 135).
func TestTaskCards_AddNoScriptAndKeepTheContentSecurityPolicy(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "checkout-platform")
	srv := handler()

	reScript := regexp.MustCompile(`<script\b([^>]*)>`)
	reSrc := regexp.MustCompile(`src="([^"]*)"`)

	for _, path := range taskPaths(f.name, f.openID) {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
			t.Errorf("%s: Content-Security-Policy = %q, want %q", path, got, contentSecurityPolicy)
		}

		// The vendored framework, plus, on the sprint page alone, the
		// column-collapse script, which takes no part in following a card.
		wantScripts := map[string]bool{"/static/vendor/tabler/tabler.min.js": true}
		if !strings.HasSuffix(path, "/tasks") {
			wantScripts["/static/sprint-board.js"] = true
		}
		scripts := reScript.FindAllStringSubmatch(rec.Body.String(), -1)
		if len(scripts) != len(wantScripts) {
			t.Errorf("%s: the page loads %d scripts, want %d", path, len(scripts), len(wantScripts))
		}
		for _, script := range scripts {
			src := reSrc.FindStringSubmatch(script[1])
			if src == nil {
				t.Errorf("%s: an inline <script> reached the page\nattrs: %s", path, script[1])
				continue
			}
			if !wantScripts[src[1]] {
				t.Errorf("%s: the page loads the unexpected script %q", path, src[1])
			}
		}
	}
}

// hostileTitle is a task title carrying every character that has meaning inside
// an HTML attribute: the double quote that delimits the attribute, the angle
// brackets that delimit a tag, the ampersand that opens an entity, an apostrophe,
// and an already-escaped entity that must not be decoded on the way in.
const hostileTitle = `Reject "quoted" <b>bold</b> & O'Brien &amp; 100% > 50%`

// TestTaskCards_AccessibleNameEscapesAHostileTitle proves the accessible name
// composed from the task title is safe in the attribute context on the sprint
// board and in the tasks page's list:
// the attribute stays delimited, no title character reaches the page unescaped,
// and the value decodes back to exactly the title the user wrote.
func TestTaskCards_AccessibleNameEscapesAHostileTitle(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "web-hostile-title")
	renameTask(t, f.name, f.openTaskID, hostileTitle)
	mux := buildMux()

	taskID := itoa(f.openTaskID)
	reLabel := regexp.MustCompile(`aria-label="([^"]*)"`)

	for _, path := range taskPaths(f.name, f.openID) {
		body := servePage(t, mux, path)
		prefix := "Open details for task #"
		want := wantAccessibleName(taskID, renderedTitleOf(t, f.name, f.openTaskID))
		if strings.HasSuffix(path, "/tasks") {
			prefix = "View task #"
			want = `aria-label="View task #` + taskID + `: ` + renderedTitleOf(t, f.name, f.openTaskID) + `"`
		}
		if !strings.Contains(body, want) {
			t.Errorf("%s: the card of task #%s does not carry the escaped accessible name %s",
				path, taskID, want)
		}
		if strings.Contains(body, hostileTitle) {
			t.Errorf("%s: the raw title reached the page unescaped", path)
		}
		for _, raw := range []string{`<b>bold</b>`, `"quoted"`} {
			if strings.Contains(body, raw) {
				t.Errorf("%s: %q reached the page unescaped", path, raw)
			}
		}

		var found bool
		for _, m := range reLabel.FindAllStringSubmatch(body, -1) {
			if !strings.HasPrefix(m[1], prefix+taskID+":") {
				continue
			}
			found = true
			if decoded := html.UnescapeString(m[1]); decoded != prefix+taskID+": "+hostileTitle {
				t.Errorf("%s: the accessible name decodes to %q, want the task reference followed "+
					"by the title exactly as it was written", path, decoded)
			}
		}
		if !found {
			t.Errorf("%s: no accessible name for task #%s survived attribute extraction", path, taskID)
		}
		// The markup kept its shape: every link still closes where it opens.
		if got, opened := strings.Count(boardRegion(t, body), "</a>"), strings.Count(boardRegion(t, body), "<a "); got != opened {
			t.Errorf("%s: %d </a> closers for %d links; the hostile title broke the markup", path, got, opened)
		}
	}
}

// TestTaskModal_IsGone is the gate for Acceptance Criterion 96: the interface has
// no task modal, no task detail script, and no task JSON. Neither the tasks page
// nor the sprint page carries an element with the class modal or a data-bs-toggle="modal" control,
// GET /static/task-modal.js is 404 because no such asset is embedded, and GET
// /roadmaps/{name}/tasks/{id}/data — and every other path below the task page —
// is 404 with no JSON body, as a path no route matches.
func TestTaskModal_IsGone(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSprintFixture(t, "checkout-platform")
	srv := handler()

	reModalClass := regexp.MustCompile(`class="(?:[^"]* )?modal(?: [^"]*)?"`)
	for _, path := range taskPaths(f.name, f.openID) {
		body := servePage(t, buildMux(), path)
		if reModalClass.MatchString(body) {
			t.Errorf("%s: the page carries an element with the class modal", path)
		}
		for _, gone := range []string{`data-bs-toggle="modal"`, "task-modal", "/data"} {
			if strings.Contains(body, gone) {
				t.Errorf("%s: the page still carries %q", path, gone)
			}
		}
	}

	if _, err := fs.Stat(staticSubFS, "task-modal.js"); err == nil {
		t.Error("static/task-modal.js is still in the embedded asset set")
	}

	task := "/roadmaps/" + f.name + "/tasks/" + itoa(f.openTaskID)
	for _, path := range []string{
		"/static/task-modal.js",
		task + "/data",
		task + "/comments",
		task + "/data/extra",
		task + "/",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: status = %d, want 404", method, path, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "json") {
				t.Errorf("%s %s: the 404 carries the content type %q", method, path, ct)
			}
			if body := rec.Body.String(); strings.HasPrefix(strings.TrimSpace(body), "{") {
				t.Errorf("%s %s: the 404 carries a JSON body %q", method, path, body)
			}
		}
	}

	// The data route is gone from the registration source as well.
	for _, pattern := range registeredRoutePatterns(t) {
		if strings.HasSuffix(pattern, "/tasks/{id}/data") {
			t.Errorf("the mux still registers %q", pattern)
		}
	}
}

// atoi parses a decimal id captured from served markup.
func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("%q is not a decimal id", s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// renameTask sets a task's title directly, so the value under test travels the
// same column a title written through the CLI does. Editing a task is taskEdit in
// internal/commands, which this package cannot reach (internal/commands imports
// internal/web), and the fixture needs the renamed row rather than the per-field
// audit entries the command owes.
func renameTask(t *testing.T, roadmap string, taskID int, title string) {
	t.Helper()

	database, err := db.Open(roadmap)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", roadmap, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	if _, err := database.Exec(`UPDATE tasks SET title = ? WHERE id = ?`, title, taskID); err != nil {
		t.Fatalf("renaming task %d: %v", taskID, err)
	}
}
