package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// contentTypeHTML and contentTypeJSON are the response content types for
// page routes and the graph data endpoint respectively (SPEC/WEB.md
// § Routes and Pages).
const (
	contentTypeHTML = "text/html; charset=utf-8"
	contentTypeJSON = "application/json"
)

// chrome carries the data the shared Tabler admin-shell partials need
// (head, sidebar, top navbar): the page <title>, the active roadmap (empty
// on the index, set on a roadmap's pages so the sidebar lists that roadmap's
// Tasks/Sprints/Graph links), and the active nav section so the sidebar can
// highlight the current view. It is embedded on every page view model under
// the field name Chrome, which the layout.html partials reference
// (SPEC/WEB.md § UI Framework).
type chrome struct {
	Title   string
	Roadmap string
	Active  string
}

// indexView is the view model for the roadmap index page. Roadmaps is the
// discovered roadmap list (empty renders the Tabler empty state).
type indexView struct {
	Chrome   chrome
	Roadmaps []string
}

// graphPageView is the view model for the knowledge-graph page shell. Name
// is the roadmap whose graph the page visualises; it is reused to build the
// /static/-free, same-origin data-endpoint URL the client script fetches.
type graphPageView struct {
	Chrome chrome
	Name   string
}

// handleIndex renders the roadmap index page: every roadmap discovered
// under ~/.roadmaps/, each linking to its detail and graph pages. With no
// roadmaps it renders an empty state (still HTTP 200) — the absence of
// roadmaps is not an error (SPEC/WEB.md § Roadmap Index Page).
func handleIndex(w http.ResponseWriter, r *http.Request) {
	names, err := loadRoadmapNames()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	renderHTML(w, "index.html", indexView{
		Chrome:   chrome{Title: "Groadmap — Roadmaps", Active: "roadmaps"},
		Roadmaps: names,
	})
}

// handleSprints renders a roadmap's sprints landing page: the roadmap's
// sprints grouped into the three tabs (Próximos / Actual / Concluídos), with
// the Actual tab active by default and the OPEN sprints expanded with their
// member tasks. It does NOT render the full tasks table (SPEC/WEB.md § Roadmap
// Sprints Page). The {name} is validated and confirmed to exist before any
// data read; an invalid or unknown name yields 404 (handled by
// resolveRoadmap), an internal read error yields 500.
func handleSprints(w http.ResponseWriter, r *http.Request) {
	name, ok := resolveRoadmap(w, r)
	if !ok {
		return
	}

	data, err := loadSprints(r.Context(), name)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data.Chrome = chrome{Title: "Groadmap — " + name, Roadmap: name, Active: "sprints"}
	renderHTML(w, "sprints.html", data)
}

// handleTasks renders a roadmap's tasks page: the full task table (every task,
// any status), each row clickable to open the read-only task detail modal
// (SPEC/WEB.md § Roadmap Tasks Page). The {name} is validated and confirmed to
// exist before any data read; an invalid or unknown name yields 404 (handled
// by resolveRoadmap), an internal read error yields 500.
func handleTasks(w http.ResponseWriter, r *http.Request) {
	name, ok := resolveRoadmap(w, r)
	if !ok {
		return
	}

	data, err := loadTasks(r.Context(), name)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data.Chrome = chrome{Title: "Groadmap — " + name + " / Tasks", Roadmap: name, Active: "tasks"}
	renderHTML(w, "tasks.html", data)
}

// handleAudit renders a roadmap's audit log page: one page of the full audit
// log (every operation and entity type) as a read-only table ordered by
// performed_at DESC, with a Previous/Next pagination footer (SPEC/WEB.md
// § Roadmap Audit Log Page). The {name} is validated and confirmed to exist
// before any data read (resolveRoadmap); an invalid or unknown name yields 404,
// an internal read error yields 500.
//
// The page query parameter is parsed defensively and never errors: an absent,
// empty, non-integer, or garbage value resolves to page 1, and any out-of-range
// value is clamped to the nearest valid page inside loadAudit (page < 1 -> 1,
// page > last -> last). The audit page therefore never returns 404 for an
// out-of-range or unparseable page; it renders 200 with the clamped page
// (SPEC/WEB.md § Routes and Pages, audit page status mapping).
func handleAudit(w http.ResponseWriter, r *http.Request) {
	name, ok := resolveRoadmap(w, r)
	if !ok {
		return
	}

	// Parse the page parameter defensively. strconv.Atoi rejects empty,
	// non-integer, and out-of-range text; on any failure we fall back to page 1,
	// which loadAudit then clamps against the real total. An absent parameter is
	// the empty string and also falls back to 1. No error path here ever yields
	// a non-200 status (SPEC/WEB.md § Roadmap Audit Log Page).
	requestedPage, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil {
		requestedPage = 1
	}

	data, err := loadAudit(r.Context(), name, requestedPage)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data.Chrome = chrome{
		Title:   "Groadmap — " + name + " / Audit",
		Roadmap: name,
		Active:  "audit",
	}
	renderHTML(w, "audit.html", data)
}

// handleSprint renders a single sprint's page: all of its fields and its task
// list in planned in-sprint execution order. The {name} is validated and
// confirmed to exist before any data read (resolveRoadmap). The {id} path
// value MUST be a valid integer; a non-integer id yields 404 without any data
// read, and an integer id that is not a sprint of the roadmap yields 404 from
// loadSprint (db.GetSprint returns utils.ErrNotFound). Any other read error is
// an internal 500 (SPEC/WEB.md § Roadmap Sprint Page; Routes and Pages,
// path-parameter rule 3, and the HTTP status mapping).
func handleSprint(w http.ResponseWriter, r *http.Request) {
	name, ok := resolveRoadmap(w, r)
	if !ok {
		return
	}

	// A non-integer {id} is a 404, exactly like an unknown roadmap: it is not
	// a sprint of the roadmap. strconv.Atoi also rejects out-of-int64 range
	// and any non-numeric text. No filesystem or database read happens for an
	// invalid id.
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	data, err := loadSprint(r.Context(), name, id)
	if err != nil {
		// No sprint with this id belongs to the roadmap -> 404. Any other
		// read failure (I/O, corrupt store) is an internal 500.
		if errors.Is(err, utils.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	data.Chrome = chrome{
		Title:   "Groadmap — " + name + " / Sprint #" + strconv.Itoa(id),
		Roadmap: name,
		Active:  "sprints",
	}
	renderHTML(w, "sprint.html", data)
}

// handleGraphPage renders the knowledge-graph page shell. The page loads the
// vendored D3.js (and the d3-sankey plugin) and the local graph.js from
// /static/, then fetches the graph data endpoint to render the node-link
// visualisation in the layout chosen on the page. The {name} is validated and
// confirmed to exist before rendering.
func handleGraphPage(w http.ResponseWriter, r *http.Request) {
	name, ok := resolveRoadmap(w, r)
	if !ok {
		return
	}
	renderHTML(w, "graph.html", graphPageView{
		Chrome: chrome{Title: "Groadmap — " + name + " graph", Roadmap: name, Active: "graph"},
		Name:   name,
	})
}

// handleGraphData serves the roadmap's graph as JSON in the Graph View Data
// shape. The graph is read read-only; a roadmap with no graph yet returns
// {"nodes":[],"edges":[]} (SPEC/DATA_FORMATS.md § Graph View Data). The
// {name} is validated and confirmed to exist before any read.
//
// The endpoint accepts two optional URL query parameters the page's query bar
// sends: q (the Cypher query to run; the default full-graph query when absent)
// and limit (the node limit; default 100 when absent). It stays GET/HEAD only;
// there is no POST and no request body (SPEC/WEB.md § Graph Data Endpoint;
// § Graph Query Bar). The user-supplied query is validated as read-only by the
// shared guard-rail BEFORE execution, so a writing or DDL query never runs.
//
// A classified query-bar failure (the query was rejected as not read-only, the
// limit was invalid, or the accepted query failed in the engine) is returned to
// the client as a structured, read-only JSON error with HTTP 400 so the page can
// surface the distinct in-place, non-fatal message; the failure triggers no
// write, no checkpoint, and no navigation (SPEC/WEB.md § Query-Bar Error
// Handling). Any other (internal I/O) error is a 500.
func handleGraphData(w http.ResponseWriter, r *http.Request) {
	name, ok := resolveRoadmap(w, r)
	if !ok {
		return
	}

	view, err := loadGraphView(r.Context(), name, r.URL.Query().Get("q"), r.URL.Query().Get("limit"))
	if err != nil {
		if qe, isQE := asGraphQueryError(err); isQE {
			// A classified query-bar failure is a client-visible, non-fatal
			// condition: return it as a structured JSON error with 400 so the
			// page can show the distinct in-place message. No write occurred.
			renderJSONStatus(w, http.StatusBadRequest, map[string]any{"error": qe.Reason, "kind": qe.Kind})
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	renderJSON(w, view)
}

// renderHTML executes the named template into a buffer first, so a template
// error produces a clean 500 rather than a half-written 200 response, then
// writes the buffered HTML with the HTML content type. html/template's
// contextual auto-escaping is the defence against injecting roadmap-derived
// text into the page (SPEC/WEB.md § Frontend Rules, rule 1).
func renderHTML(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentTypeHTML)
	_, _ = buf.WriteTo(w)
}

// renderJSON writes v as pretty-printed JSON with two-space indentation and
// a trailing newline, consistent with all other Groadmap JSON output
// (SPEC/DATA_FORMATS.md § Implementation Notes). HTML escaping is kept ON
// (the encoder's default) as defence-in-depth: <, >, and & in roadmap-derived
// strings are emitted as <, >, and &, so the JSON can never be
// mistaken for or broken out into executable HTML if it is ever read as a
// document. graph.js consumes the payload via fetch()+JSON.parse and renders
// strings with D3 .text()/textContent, where the unicode escapes are decoded
// back to the identical characters — the visualisation is unchanged
// (SPEC/WEB.md § Graph Data Endpoint).
func renderJSON(w http.ResponseWriter, v any) {
	renderJSONStatus(w, http.StatusOK, v)
}

// renderJSONStatus is renderJSON with an explicit HTTP status, used for the
// structured, read-only query-bar error responses (HTTP 400) the graph data
// endpoint returns (SPEC/WEB.md § Query-Bar Error Handling). The body is
// buffered and encoded first, so a Content-Type header and the status line are
// written together and never after the body, and an encode failure still yields
// a clean 500 rather than a half-written response. HTML escaping stays ON, as in
// renderJSON.
func renderJSONStatus(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
