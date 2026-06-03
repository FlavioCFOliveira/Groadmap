package web

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// contentTypeHTML and contentTypeJSON are the response content types for
// page routes and the graph data endpoint respectively (SPEC/WEB.md
// § Routes and Pages).
const (
	contentTypeHTML = "text/html; charset=utf-8"
	contentTypeJSON = "application/json"
)

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
	renderHTML(w, "index.html", map[string]any{"Roadmaps": names})
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
	renderHTML(w, "detail.html", data)
}

// handleGraphPage renders the knowledge-graph page shell. The page loads the
// vendored Cytoscape.js and the local graph.js from /static/, then fetches
// the graph data endpoint to render the node-link visualisation. The
// {name} is validated and confirmed to exist before rendering.
func handleGraphPage(w http.ResponseWriter, r *http.Request) {
	name, ok := resolveRoadmap(w, r)
	if !ok {
		return
	}
	renderHTML(w, "graph.html", map[string]any{"Name": name})
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
