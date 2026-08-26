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

	// The FuncMap MUST be registered before parsing: html/template resolves a
	// function name at PARSE time, so a template calling a helper that is not in
	// the map fails to parse rather than failing to render.
	pageTemplates = template.Must(
		template.New("").Funcs(templateFuncs()).ParseFS(templatesFS, "templates/*.html"),
	)
}

// templateFuncs is the complete FuncMap the page templates are parsed with: the
// semantic badge colour helpers (see badge.go) and the audit-cell helpers (see
// audit.go).
//
// It is a function returning a fresh map rather than a package-level variable so
// a test can take the real set, replace one entry with a probe, and re-parse the
// templates against it without mutating the map the server uses.
func templateFuncs() template.FuncMap {
	funcs := make(template.FuncMap, len(badgeFuncMap)+len(auditFuncMap))
	for name, fn := range badgeFuncMap {
		funcs[name] = fn
	}
	for name, fn := range auditFuncMap {
		funcs[name] = fn
	}
	return funcs
}
