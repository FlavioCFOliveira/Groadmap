// Package web implements the read-only `rmp web` command: an HTTP server,
// embedded into the rmp binary, that presents the roadmaps under
// ~/.roadmaps/ as server-rendered HTML and an interactive knowledge-graph
// visualisation. The interface never writes; the rmp CLI remains the sole
// write path. See SPEC/WEB.md for the full behaviour and SPEC/COMMANDS.md
// § Web Interface for the command-line contract.
package web

import (
	"embed"
	"html/template"
	"io/fs"
)

// templatesFS holds the html/template set that renders every page. The
// templates are parsed once at package initialisation from this embedded
// filesystem (see pages.go); they are never read from the host filesystem
// at runtime, satisfying SPEC/WEB.md § Self-Contained Deliverable.
//
//go:embed templates/*.html
var templatesFS embed.FS

// staticFS holds every static asset served under /static/...: the
// stylesheet, the client scripts, and the vendored D3.js bundle (with the
// d3-sankey plugin). Every asset the interface loads comes from this embedded
// set; the server never serves an arbitrary host filesystem path (SPEC/WEB.md
// § Static Assets and § Security and Constraints, rule 4).
//
//go:embed static
var staticFS embed.FS

// staticSubFS is staticFS rooted at the static directory, so a request for
// /static/style.css maps to the embedded file "static/style.css" without
// the leading "static/" path segment. fs.Sub keeps the http.FileServer
// mount confined to the embedded asset set.
var staticSubFS fs.FS

// pageTemplates is the parsed html/template set shared by every page
// handler. It is built once at init from templatesFS.
var pageTemplates *template.Template

func init() {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// The embed directive guarantees the "static" directory exists in
		// the binary; a failure here is a build-time wiring error, not a
		// runtime condition, so panicking at init surfaces it immediately.
		panic("web: rooting embedded static FS: " + err.Error())
	}
	staticSubFS = sub

	// The badge FuncMap MUST be registered before parsing so the templates can
	// call the semantic colour helpers (taskStatusBadge, sprintStatusBadge,
	// priorityBadge, severityBadge) at execution time (SPEC/WEB.md § Status,
	// Priority, and Severity Badge Colours; see badge.go).
	pageTemplates = template.Must(
		template.New("").Funcs(badgeFuncMap).ParseFS(templatesFS, "templates/*.html"),
	)
}
