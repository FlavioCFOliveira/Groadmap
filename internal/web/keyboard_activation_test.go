package web

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the regression gate for the defect recorded as task #204: a
// clickable task never opened its detail modal from the keyboard, on any surface
// that showed one.
//
// The cause is not a matter of opinion, and neither is the fix. The vendored
// Bootstrap binds the modal data-api on the click event only —
// `ge.on(document, On, '[data-bs-toggle="modal"]', ...)` with `On = "click..."`
// in static/vendor/tabler/tabler.min.js — and registers no keydown handler for a
// trigger. Per the HTML activation behaviour, only a <button>, an <a href>, and
// form controls synthesise a click from Enter or Space. `role="button"` plus
// `tabindex="0"` grants focus and announces the role; it does not grant
// activation, so those elements announced themselves to a screen reader as
// buttons that could not be pressed.
//
// The fix is therefore structural, and it has two halves. Every task can be
// opened by a real control — the board card is itself a <button>, and the sprint
// page's task title is one inside its row — and nothing that cannot be activated
// pretends to be a control: no element carries role="button" with tabindex="0"
// any more. A <tr> that keeps the modal data attributes for the POINTER is
// harmless, because it takes no focus and announces no role.
//
// These tests assert both halves across every surface that renders a clickable
// task, so the defect cannot regress on one surface while the other holds —
// which is what SPEC/WEB.md requires when it ties the board card's treatment to
// the sprint page's task rows.

// reOpeningTag captures the tag name and the attribute text of every opening tag
// in a document.
var reOpeningTag = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)\b([^>]*)>`)

// nativelyActivatable is the set of elements that turn Enter or Space into a
// click without any script. An <a> qualifies only with an href, which is why the
// check below looks at the attributes too.
var nativelyActivatable = map[string]bool{"button": true, "a": true, "input": true}

// modalTrigger is one element carrying data-bs-toggle="modal".
type modalTrigger struct {
	tag   string
	attrs string
}

// modalTriggers returns every modal trigger in a served page.
func modalTriggers(body string) []modalTrigger {
	triggers := make([]modalTrigger, 0, 8)
	for _, m := range reOpeningTag.FindAllStringSubmatch(body, -1) {
		if strings.Contains(m[2], `data-bs-toggle="modal"`) {
			triggers = append(triggers, modalTrigger{tag: strings.ToLower(m[1]), attrs: m[2]})
		}
	}
	return triggers
}

// clickableTaskPaths are the two surfaces that render a clickable task: the tasks
// page's Kanban board and the sprint page's member-tasks table. Both must satisfy
// every property below; fixing one at the expense of the other is the divergence
// SPEC/WEB.md forbids.
func clickableTaskPaths(name string, sprintID int) []string {
	return []string{
		"/roadmaps/" + name + "/tasks",
		"/roadmaps/" + name + "/sprints/" + itoa(sprintID),
	}
}

// reModalTarget captures the task id a trigger carries. The page holds ONE modal
// shell, so every trigger points at the same data-bs-target and the task id is
// what identifies which task will be fetched into it.
var reModalTarget = regexp.MustCompile(`data-task-id="(\d+)"`)

// renderedTitleOf returns a task's title as the page must render it in an
// attribute: the stored title, escaped the way html/template escapes a value in
// any context.
//
// The title is read from the ROADMAP, not from the markup under test: the page no
// longer carries a per-task modal to read it back from, and taking it from the
// trigger's own attribute would make the assertion circular. rendered() applies
// the escaping html/template applies — the same replacement table in element text
// and attribute value alike, which
// TestModalTriggers_AccessibleNameEscapesAHostileTitle pins directly.
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

// wantAccessibleName is the accessible name every task trigger must carry: the
// task reference, so the name identifies the task, followed by the task's title,
// so the name CONTAINS the visible label of a trigger whose visible text is that
// title. A control whose accessible name omits its visible label fails WCAG 2.5.3
// (Label in Name, Level A), and the sprint page's title button is exactly such a
// control (SPEC/WEB.md § Sprint Detail Sub-Template; § Roadmap Tasks Page,
// clickable card).
func wantAccessibleName(taskID, renderedTitle string) string {
	return `aria-label="Open details for task #` + taskID + `: ` + renderedTitle + `"`
}

// TestModalTriggers_EveryTaskHasANativelyActivatableTrigger is the gate for
// Acceptance Criteria 1 and 4 of task #204: on every surface that shows a
// clickable task, each task has at least one trigger that is natively activatable
// — so Enter and Space open its modal with no JavaScript added — and that
// trigger's accessible name identifies the task it opens.
//
// The property is "at least one activatable trigger per task", not "every trigger
// is activatable", because the sprint page deliberately keeps the whole row
// clickable by POINTER: the <tr> carries the modal data attributes so a click
// anywhere on it still works. That row is not focusable and claims no role, so it
// offers nothing to the keyboard and misleads nobody — which is exactly what
// TestModalTriggers_NeverFakeAButtonWithRoleAndTabindex pins. What the keyboard
// needs is that some real control exists for each task, and this is that test.
func TestModalTriggers_EveryTaskHasANativelyActivatableTrigger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	mux := buildMux()

	for _, path := range clickableTaskPaths(name, 1) {
		body := servePage(t, mux, path)
		triggers := modalTriggers(body)

		// Falsifiability control: a page with no trigger would satisfy every
		// assertion below vacuously. Both surfaces render at least one.
		if len(triggers) == 0 {
			t.Fatalf("%s: no modal trigger found; the extraction is broken or the page "+
				"renders no clickable task", path)
		}

		// tasks maps each task the page can open to whether some trigger of it is
		// natively activatable.
		tasks := map[string]bool{}
		for _, trigger := range triggers {
			target := reModalTarget.FindStringSubmatch(trigger.attrs)
			if target == nil {
				continue // not a task trigger
			}
			taskID := target[1]
			activatable := nativelyActivatable[trigger.tag]
			// An anchor is activatable only with an href; a button must be
			// type="button", because a typeless button submits its form and the
			// interface has no write path at all.
			if trigger.tag == "a" && !strings.Contains(trigger.attrs, "href=") {
				activatable = false
			}
			if trigger.tag == "button" && !strings.Contains(trigger.attrs, `type="button"`) {
				t.Errorf("%s: a modal trigger button carries no type=\"button\"\nattrs: %s",
					path, trigger.attrs)
			}
			if !activatable {
				tasks[taskID] = tasks[taskID] // a pointer-only trigger neither helps nor harms
				continue
			}
			tasks[taskID] = true

			// The accessible name of the activatable trigger identifies the task it
			// opens AND carries the task's title, which is the visible label of the
			// sprint page's trigger.
			id, convErr := strconv.Atoi(taskID)
			if convErr != nil {
				t.Fatalf("%s: trigger carries a non-integer task id %q", path, taskID)
			}
			wantLabel := wantAccessibleName(taskID, renderedTitleOf(t, name, id))
			if !strings.Contains(trigger.attrs, wantLabel) {
				t.Errorf("%s: the trigger of task #%s does not carry %s, so its accessible name "+
					"no longer names the task it opens\nattrs: %s",
					path, taskID, wantLabel, trigger.attrs)
			}
		}

		if len(tasks) == 0 {
			t.Fatalf("%s: no task trigger found; the extraction is broken", path)
		}
		for taskID, ok := range tasks {
			if !ok {
				t.Errorf("%s: task #%s can be opened by pointer only: no trigger of it is a "+
					"natively activatable element, and Bootstrap binds the modal data-api on "+
					"click alone, so Enter and Space do nothing", path, taskID)
			}
		}
	}
}

// TestModalTriggers_NeverFakeAButtonWithRoleAndTabindex is the gate for
// Acceptance Criterion 2 of task #204: no trigger anywhere in the served HTML is
// a div or a tr carrying role="button" with tabindex="0" — the shape that
// announces a button which cannot be pressed.
//
// The assertion runs over EVERY page the interface serves, not only the two that
// show clickable tasks, so the pattern cannot reappear on a page this feature
// never touched.
func TestModalTriggers_NeverFakeAButtonWithRoleAndTabindex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	seedRoadmapWithAudit(t, name, 3)
	mux := buildMux()

	scanned := 0
	for _, path := range allPagePaths(name) {
		body := servePage(t, mux, path)

		for _, m := range reOpeningTag.FindAllStringSubmatch(body, -1) {
			tag, attrs := strings.ToLower(m[1]), m[2]
			scanned++

			// The exact defect: a non-activatable element dressed as a button.
			if strings.Contains(attrs, `role="button"`) && strings.Contains(attrs, `tabindex="0"`) {
				if !nativelyActivatable[tag] {
					t.Errorf("%s: a <%s> carries role=\"button\" with tabindex=\"0\"; it takes focus "+
						"and announces a button that cannot be pressed. Use an element that is "+
						"natively activatable instead\nattrs: %s", path, tag, attrs)
				}
			}
			// And the same shape stated the other way round: a modal trigger that
			// needs role or tabindex is a trigger that is not a real control.
			if strings.Contains(attrs, `data-bs-toggle="modal"`) {
				for _, prop := range []string{`role="button"`, `tabindex="0"`} {
					if strings.Contains(attrs, prop) {
						t.Errorf("%s: a modal trigger carries %s; a real button has it natively, "+
							"and anything else that needs it cannot be activated\nattrs: %s",
							path, prop, attrs)
					}
				}
			}
		}
	}

	// Falsifiability control: an extraction that matched nothing would make the
	// sweep above pass without looking at anything.
	if scanned < 200 {
		t.Fatalf("scanned only %d opening tags across every page; the extraction is broken "+
			"and the assertions above would be vacuous", scanned)
	}
}

// TestModalTriggers_AddNoScriptAndKeepTheContentSecurityPolicy is the gate for
// Acceptance Criterion 6 of task #204: the fix comes from the markup, so the
// Content-Security-Policy is unchanged and the surfaces load no script beyond the
// vendored one they already loaded.
//
// This is what makes "no JavaScript was added" a measured property rather than a
// promise: a script that taught a div to respond to Enter would have to be served
// from /static/ (the policy forbids inline script), and it would show up here.
func TestModalTriggers_AddNoScriptAndKeepTheContentSecurityPolicy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "platform-core")
	// handler() is the fully wired handler the server runs: the mux wrapped by the
	// security-header middleware. buildMux() alone would answer with no headers at
	// all and the assertion below would measure nothing. The CSP literal itself is
	// pinned against the SPEC table by TestSecurityHeaders.
	srv := handler()

	reScript := regexp.MustCompile(`<script\b([^>]*)>`)
	reSrc := regexp.MustCompile(`src="([^"]*)"`)

	for _, path := range clickableTaskPaths(name, 1) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
			t.Errorf("%s: Content-Security-Policy = %q, want the unchanged policy %q",
				path, got, contentSecurityPolicy)
		}
		// The policy that makes "no JavaScript was added" checkable: inline script
		// is forbidden, so a script could only arrive from /static/.
		if !strings.Contains(contentSecurityPolicy, "script-src 'self'") {
			t.Fatalf("the policy no longer restricts script to 'self': %q", contentSecurityPolicy)
		}

		body := rec.Body.String()
		scripts := reScript.FindAllStringSubmatch(body, -1)

		// The two scripts a page that shows clickable tasks loads: the vendored
		// framework that opens the modal, and the project script that fills it from
		// the task detail endpoint. Both are served from /static/, which is what the
		// policy admits; neither is inline, which the policy forbids outright.
		wantScripts := map[string]bool{
			"/static/vendor/tabler/tabler.min.js": true,
			"/static/task-modal.js":               true,
		}
		if len(scripts) != len(wantScripts) {
			t.Errorf("%s: the page loads %d scripts, want %d", path, len(scripts), len(wantScripts))
		}
		for _, script := range scripts {
			src := reSrc.FindStringSubmatch(script[1])
			if src == nil {
				t.Errorf("%s: an inline <script> reached the page, which the policy forbids\nattrs: %s",
					path, script[1])
				continue
			}
			if !strings.HasPrefix(src[1], "/static/") {
				t.Errorf("%s: the page loads the script %q from outside /static/", path, src[1])
			}
			if !wantScripts[src[1]] {
				t.Errorf("%s: the page loads the unexpected script %q", path, src[1])
			}
		}
	}
}

// TestSprintPage_RowStaysClickableByPointer is the gate for Acceptance Criterion
// 5 of task #204 on the sprint page: moving the keyboard trigger onto the task
// title did not take the pointer affordance off the row. The row still carries
// the modal data attributes, so a click anywhere on it still opens the task's
// modal, and the rendered values of the row are unchanged.
func TestSprintPage_RowStaysClickableByPointer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-task-modal")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/sprints/"+itoa(f.openID))
	taskID := `data-task-id="` + itoa(f.openTaskID) + `"`

	// The row itself: still a trigger, still styled as clickable, no longer
	// pretending to be a button.
	row := `<tr class="task-row" data-bs-toggle="modal" data-bs-target="#task-modal" ` + taskID + `>`
	if !strings.Contains(body, row) {
		t.Errorf("the member-task row is no longer clickable by pointer; want the row %q", row)
	}
	// The title inside it: a real button, carrying the accessible name the row
	// used to carry, and the task's own title as its visible text.
	trigger := `<button type="button" class="task-row__trigger p-0 border-0 bg-transparent text-reset text-start" ` +
		`data-bs-toggle="modal" data-bs-target="#task-modal" ` + taskID + ` ` +
		wantAccessibleName(itoa(f.openTaskID), renderedTitleOf(t, f.name, f.openTaskID)) + `>`
	if !strings.Contains(body, trigger) {
		t.Errorf("the member-task title is not the row's keyboard trigger; want %q", trigger)
	}
	// Both carry the task id, so pointer and keyboard open the same task in the
	// page's single modal shell.
	if got := strings.Count(body, taskID); got != 2 {
		t.Errorf("the task id appears %d times in the row, want twice: on the row and on its "+
			"title trigger", got)
	}
}

// hostileTitle is a task title carrying every character that has meaning inside
// an HTML attribute: the double quote that delimits the attribute, the angle
// brackets that delimit a tag, the ampersand that opens an entity, an apostrophe,
// and an already-escaped entity that must not be decoded on the way in. A title
// is free text a user wrote through the CLI, so this is input the interface must
// survive, not a hypothetical.
const hostileTitle = `Reject "quoted" <b>bold</b> & O'Brien &amp; 100% > 50%`

// TestModalTriggers_AccessibleNameEscapesAHostileTitle proves the accessible name
// composed from the task title is safe in the attribute context on BOTH surfaces:
// the attribute stays delimited, the markup keeps its shape, no title character
// reaches the page unescaped, and the value decodes back to exactly the title the
// user wrote.
//
// This is the escaping question the composed name raises: the title moved from
// element text, where html/template was already escaping it, into an attribute
// value, where an unescaped double quote would end the attribute and everything
// after it would become markup.
func TestModalTriggers_AccessibleNameEscapesAHostileTitle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintFixture(t, "web-hostile-title")
	renameTask(t, f.name, f.openTaskID, hostileTitle)
	mux := buildMux()

	taskID := itoa(f.openTaskID)
	reLabel := regexp.MustCompile(`aria-label="([^"]*)"`)

	for _, path := range clickableTaskPaths(f.name, f.openID) {
		body := servePage(t, mux, path)

		// The board card and the sprint-page title button both carry the composed
		// name, and it is EXACTLY the escaped form html/template produces — the same
		// bytes the modal heading carries for the same title, which is what makes
		// composing the expectation from the page legitimate everywhere else.
		want := wantAccessibleName(taskID, renderedTitleOf(t, f.name, f.openTaskID))
		if !strings.Contains(body, want) {
			t.Errorf("%s: the trigger of task #%s does not carry the escaped accessible name %s",
				path, taskID, want)
		}

		// The raw title never reaches the page: an unescaped double quote would
		// close the attribute and turn the rest of the title into markup.
		if strings.Contains(body, hostileTitle) {
			t.Errorf("%s: the raw title reached the page unescaped, so the attribute is broken", path)
		}
		for _, raw := range []string{`<b>bold</b>`, `"quoted"`} {
			if strings.Contains(body, raw) {
				t.Errorf("%s: %q reached the page unescaped", path, raw)
			}
		}

		// Every aria-label on the page is a well-formed attribute value: the
		// extraction below is bounded by the quote characters, so a label that had
		// swallowed a stray quote would come back truncated and fail to decode.
		var found bool
		for _, m := range reLabel.FindAllStringSubmatch(body, -1) {
			if !strings.HasPrefix(m[1], "Open details for task #"+taskID+":") {
				continue
			}
			found = true
			if decoded := html.UnescapeString(m[1]); decoded != "Open details for task #"+taskID+": "+hostileTitle {
				t.Errorf("%s: the accessible name decodes to %q, want the task reference followed "+
					"by the title exactly as it was written", path, decoded)
			}
		}
		if !found {
			t.Errorf("%s: no accessible name for task #%s survived attribute extraction, so the "+
				"attribute is not well formed", path, taskID)
		}

		// The markup kept its shape: the page carries exactly one modal shell,
		// whatever the titles it renders.
		if got := strings.Count(body, `id="task-modal"`); got != 1 {
			t.Errorf("%s: the page carries %d modal shells, want 1; the hostile title broke the markup",
				path, got)
		}
	}
}

// renameTask sets a task's title through the production write path, so the value
// under test travels the same route a title written through the CLI travels.
func renameTask(t *testing.T, roadmap string, taskID int, title string) {
	t.Helper()

	database, err := db.Open(roadmap)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", roadmap, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	if err := database.UpdateTaskStruct(testContext(t), taskID, &models.TaskUpdate{Title: &title}); err != nil {
		t.Fatalf("renaming task %d: %v", taskID, err)
	}
}

// testContext is the context the test write path uses.
func testContext(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
