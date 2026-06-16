package web

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// contentSecurityPolicy restricts every resource type to the server's own
// origin, consistent with the self-contained, no-remote-origin asset model.
// It allows inline styles (the vendored Tabler framework and D3 use them) and
// data: image sources, but forbids inline and remote scripts (every script is
// served from /static/), forbids framing, and pins <base> to the same origin
// (SPEC/WEB.md § Security Headers).
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'"

// noDirFS wraps an fs.FS so that opening a directory fails with fs.ErrNotExist.
// Mounting http.FileServer over it suppresses the auto-generated browseable
// directory listings (GET /static/ and GET /static/vendor/ return 404) while
// individual files still resolve normally (SPEC/WEB.md § Static Assets).
type noDirFS struct{ fs.FS }

// Open opens name, but returns fs.ErrNotExist (a 404 through http.FileServer)
// when name is a directory, so no directory listing is ever served.
func (f noDirFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	s, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if s.IsDir() {
		file.Close()
		return nil, fs.ErrNotExist
	}
	return file, nil
}

// securityHeaders is the outermost middleware: it sets the hardening response
// headers on every response — including the 404/405 produced by the fallback
// handler and the static file server — before delegating to next
// (SPEC/WEB.md § Security Headers).
//
// It is also the single authoritative location for the cache policy: every
// data-derived response carries Cache-Control: no-store, while embedded static
// assets under /static/... are excluded and remain cacheable (SPEC/WEB.md
// § Cache Policy). Setting it here — the outermost layer that runs on every
// response, including the fallback handler's data-state-dependent 404/405/500 —
// covers all dynamic pages, the JSON data endpoint, and those error responses
// in one place. It is deliberately NOT duplicated in renderHTML/renderJSON, so
// the no-store guarantee has exactly one source of truth and cannot diverge.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		// Embedded static assets are immutable and stay cacheable; every other
		// response is data-derived (computed from the SQLite DB or the GoGraph
		// store, including the data-state-dependent 404/405/500) and must never
		// be re-presented from a stale cache (SPEC/WEB.md § Cache Policy).
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// handler builds the fully wired read-only HTTP handler: the route mux wrapped
// by the security-header middleware, which is the outermost layer so every
// response carries the hardening headers.
func handler() http.Handler {
	return securityHeaders(buildMux())
}

// buildMux wires the read-only routes onto an http.ServeMux. Go 1.22+
// method+wildcard patterns register GET and HEAD explicitly for every
// route, so any other method on a matched path falls through to a 405
// (SPEC/WEB.md § Routes and Pages). The {name} path segment is validated
// against the roadmap-name rules inside each handler before it is used to
// build any filesystem path.
func buildMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Static assets served only from the embedded set. http.FileServer over
	// the embedded sub-FS returns 404 for any path not embedded and never
	// maps to a host filesystem path. The noDirFS wrapper additionally turns
	// directory requests into 404s, suppressing browseable directory listings
	// (SPEC/WEB.md § Static Assets).
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(noDirFS{staticSubFS})))
	mux.Handle("GET /static/", staticHandler)
	mux.Handle("HEAD /static/", staticHandler)

	// Index page.
	mux.HandleFunc("GET /{$}", handleIndex)
	mux.HandleFunc("HEAD /{$}", handleIndex)

	// Roadmap sprints page (landing): the roadmap's sprints as three tabs.
	mux.HandleFunc("GET /roadmaps/{name}", handleSprints)
	mux.HandleFunc("HEAD /roadmaps/{name}", handleSprints)

	// Roadmap tasks page: the full task table. A distinct, more specific
	// pattern than /roadmaps/{name}; Go 1.22+ ServeMux routes the literal
	// "tasks" segment here and {name} alone to handleSprints without conflict,
	// exactly as the sprints/{id} and graph patterns already coexist.
	mux.HandleFunc("GET /roadmaps/{name}/tasks", handleTasks)
	mux.HandleFunc("HEAD /roadmaps/{name}/tasks", handleTasks)

	// Roadmap sprint page. {id} is parsed and validated inside the handler;
	// a non-integer id, or an id that is not a sprint of the roadmap, is a 404
	// (SPEC/WEB.md § Routes and Pages, path-parameter rule 3).
	mux.HandleFunc("GET /roadmaps/{name}/sprints/{id}", handleSprint)
	mux.HandleFunc("HEAD /roadmaps/{name}/sprints/{id}", handleSprint)

	// Knowledge-graph page and its data endpoint.
	mux.HandleFunc("GET /roadmaps/{name}/graph", handleGraphPage)
	mux.HandleFunc("HEAD /roadmaps/{name}/graph", handleGraphPage)
	mux.HandleFunc("GET /roadmaps/{name}/graph/data", handleGraphData)
	mux.HandleFunc("HEAD /roadmaps/{name}/graph/data", handleGraphData)

	// Catch-all: any request whose method+path did not match a GET/HEAD
	// pattern above. A non-read method on a known path lands here (405); an
	// unknown read path lands here (404). The split is decided by whether the
	// method is GET/HEAD.
	mux.HandleFunc("/", handleFallback)

	return mux
}

// handleFallback answers requests that matched no read route. A GET/HEAD on
// an unknown path is a 404; any other method is a 405. The more specific
// GET/HEAD patterns registered in buildMux take precedence over this
// handler, so a GET to a registered path never reaches here — only a
// non-read method on one of those paths (405) or a read of an unmapped path
// (404) does.
func handleFallback(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		http.NotFound(w, r)
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// resolveRoadmap validates the {name} path parameter and confirms the
// roadmap exists, WITHOUT touching the filesystem for an invalid name. It
// returns the validated name and true on success. On failure it has already
// written a 404 response and returns false: a name that fails the
// roadmap-name rules (the path-traversal guard) and a syntactically valid
// but non-existent roadmap both map to 404 (SPEC/WEB.md § Routes and Pages,
// path-parameter rules 1 and 2).
func resolveRoadmap(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")

	// Validate BEFORE building any filesystem path. A crafted name (e.g.
	// "../etc") fails this check and is rejected without a filesystem touch.
	if err := utils.ValidateRoadmapName(name); err != nil {
		http.NotFound(w, r)
		return "", false
	}

	exists, err := utils.RoadmapExists(name)
	if err != nil {
		// An I/O error while stat-ing the (now validated) path is an
		// internal read error, not a not-found.
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return "", false
	}
	if !exists {
		http.NotFound(w, r)
		return "", false
	}

	return name, true
}
