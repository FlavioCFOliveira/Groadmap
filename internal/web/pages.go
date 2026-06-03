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

// handleDetail renders a roadmap's tasks and sprints with their
// relationships. The {name} is validated and confirmed to exist before any
// data read; an invalid or unknown name yields 404 (handled by
// resolveRoadmap).
func handleDetail(w http.ResponseWriter, r *http.Request) {
	name, ok := resolveRoadmap(w, r)
	if !ok {
		return
	}

	data, err := loadDetail(r.Context(), name)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data.Chrome = chrome{Title: "Groadmap — " + name, Roadmap: name, Active: "tasks"}
	renderHTML(w, "detail.html", data)
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
func handleGraphData(w http.ResponseWriter, r *http.Request) {
	name, ok := resolveRoadmap(w, r)
	if !ok {
		return
	}

	view, err := loadGraphView(r.Context(), name)
	if err != nil {
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
// (SPEC/DATA_FORMATS.md § Implementation Notes). HTML escaping is disabled
// because the payload is consumed by the page's fetch() as JSON, not
// interpolated into HTML.
func renderJSON(w http.ResponseWriter, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	_, _ = buf.WriteTo(w)
}
