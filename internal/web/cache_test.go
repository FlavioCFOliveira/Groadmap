package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCachePolicy_NoStoreOnDataDerivedResponses asserts that every
// data-derived response carries Cache-Control: no-store, so a freshly read
// database or graph-store state is never masked by a client-side or
// intermediary cache (SPEC/WEB.md § Cache Policy, rule 1; Acceptance
// Criterion 37). The probes cover a representative 200 response of EACH
// data-derived kind: the index page, the sprints landing page, the tasks
// page, a sprint page, the graph page shell, and the graph data endpoint.
// The full handler() stack is exercised so the assertion proves the header
// is set by the authoritative securityHeaders middleware.
func TestCachePolicy_NoStoreOnDataDerivedResponses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")

	// seedRoadmap inserts exactly one sprint into a fresh database, so the
	// seeded sprint's id is 1; the sprint-page probe therefore hits a real
	// 200 rather than a 404.
	const sprintID = "1"

	h := handler()

	paths := []string{
		"/",                            // roadmap index
		"/roadmaps/" + name,            // sprints landing page
		"/roadmaps/" + name + "/tasks", // tasks page
		"/roadmaps/" + name + "/sprints/" + sprintID, // sprint page
		"/roadmaps/" + name + "/graph",               // graph page shell
		"/roadmaps/" + name + "/graph/data",          // graph data endpoint (JSON)
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200; body=%q", path, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("GET %s: Cache-Control = %q, want %q", path, got, "no-store")
		}
	}
}

// TestCachePolicy_NoStoreOnDataStateDependentErrors asserts that the
// data-state-dependent error responses also carry Cache-Control: no-store,
// because whether a path is found depends on the current database or store
// state, so those responses are themselves data-derived (SPEC/WEB.md § Cache
// Policy, rule 1). The probes cover a 404 for a non-existent roadmap, a 404
// for a non-existent sprint id of an existing roadmap, and a 405 for a
// non-read method on a known route.
func TestCachePolicy_NoStoreOnDataStateDependentErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")

	h := handler()

	type probe struct {
		method, path string
		wantStatus   int
	}
	probes := []probe{
		// A syntactically valid but non-existent roadmap -> 404.
		{http.MethodGet, "/roadmaps/no-such-roadmap", http.StatusNotFound},
		// An existing roadmap but a sprint id that is not one of its sprints
		// -> 404 (the read decides found/not-found from DB state).
		{http.MethodGet, "/roadmaps/" + name + "/sprints/999999", http.StatusNotFound},
		// A non-read method on a known route -> 405.
		{http.MethodPost, "/roadmaps/" + name, http.StatusMethodNotAllowed},
	}

	for _, p := range probes {
		req := httptest.NewRequest(p.method, p.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != p.wantStatus {
			t.Fatalf("%s %s: status = %d, want %d; body=%q", p.method, p.path, rec.Code, p.wantStatus, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s %s: Cache-Control = %q, want %q", p.method, p.path, got, "no-store")
		}
	}
}

// TestCachePolicy_StaticAssetsNotForcedNoStore asserts that embedded static
// assets under /static/... are EXCLUDED from the no-store rule and remain
// cacheable: a 200 static asset response must NOT carry Cache-Control:
// no-store, because static assets are immutable embedded data, not
// data-derived responses (SPEC/WEB.md § Cache Policy, rule 3).
func TestCachePolicy_StaticAssetsNotForcedNoStore(t *testing.T) {
	h := handler()

	req := httptest.NewRequest(http.MethodGet, "/static/style.css", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /static/style.css: status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got == "no-store" {
		t.Errorf("GET /static/style.css: Cache-Control = %q, want it NOT forced to no-store", got)
	}
}
