package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// seedRoadmap creates a real on-disk roadmap under the test's temporary HOME
// with one task and one sprint that contains it, so a handler under test has
// genuine SQLite data to read. It returns the roadmap name. The caller must
// have already redirected HOME (t.Setenv("HOME", ...)) so nothing touches the
// developer's ~/.roadmaps. No graph store is created: the graph directory is
// intentionally absent so the graph-data handler exercises its empty-graph
// path (loadGraphView returns empty arrays without creating files).
func seedRoadmap(t *testing.T, name string) string {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	now := time.Now().UTC().Format(time.RFC3339)

	task := &models.Task{
		Priority:               1,
		Severity:               2,
		Status:                 models.StatusBacklog,
		Title:                  "Wire read-only web server to SQLite",
		FunctionalRequirements: "Serve roadmap tasks and sprints as HTML",
		TechnicalRequirements:  "net/http ServeMux with method+wildcard routes",
		AcceptanceCriteria:     "Detail page lists every task and sprint",
		CreatedAt:              now,
	}
	taskID, err := database.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("creating task: %v", err)
	}

	sprint := &models.Sprint{
		Status:      models.SprintPending,
		Description: "Ship the read-only web UI for roadmap inspection",
		CreatedAt:   now,
	}
	sprintID, err := database.CreateSprint(context.Background(), sprint)
	if err != nil {
		t.Fatalf("creating sprint: %v", err)
	}

	if err := database.AddTasksToSprint(context.Background(), sprintID, []int{taskID}); err != nil {
		t.Fatalf("adding task to sprint: %v", err)
	}

	return name
}

// TestHandleDetail_HappyPath drives handleDetail end-to-end against a populated
// roadmap: it must render 200 HTML whose body reflects the seeded task title
// and sprint membership. This covers loadDetail's full read path (tasks,
// sprints, sprint membership) and renderHTML's success branch
// (SPEC/WEB.md § Tasks and Sprints from SQLite).
func TestHandleDetail_HappyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeHTML {
		t.Errorf("content-type = %q, want %q", ct, contentTypeHTML)
	}
	body := rec.Body.String()
	if !contains(body, "Wire read-only web server to SQLite") {
		t.Errorf("detail body missing seeded task title")
	}
}

// TestHandleIndex_WithRoadmaps confirms the index renders 200 and links to a
// roadmap that exists on disk, covering handleIndex's success branch with a
// non-empty roadmap list (the empty-state branch is covered elsewhere).
func TestHandleIndex_WithRoadmaps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !contains(body, name) {
		t.Errorf("index body does not mention seeded roadmap %q", name)
	}
	if contains(body, "No roadmaps found") {
		t.Errorf("index still shows empty state despite a seeded roadmap")
	}
}

// TestHandleGraphData_EmptyGraph drives handleGraphData against a roadmap that
// has never used the graph command. The graph directory is absent, so the
// handler must return 200 with the empty Graph View Data shape
// ({"nodes":[],"edges":[]}) WITHOUT creating any graph file. This covers
// loadGraphView's no-graph-yet path and renderJSON's success branch
// (SPEC/DATA_FORMATS.md § Graph View Data; SPEC/WEB.md § empty graph).
func TestHandleGraphData_EmptyGraph(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/graph/data", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("graph data status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeJSON {
		t.Errorf("content-type = %q, want %q", ct, contentTypeJSON)
	}

	var view graphView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding graph data: %v; body=%q", err, rec.Body.String())
	}
	if view.Nodes == nil || len(view.Nodes) != 0 {
		t.Errorf("nodes = %#v, want empty non-nil slice", view.Nodes)
	}
	if view.Edges == nil || len(view.Edges) != 0 {
		t.Errorf("edges = %#v, want empty non-nil slice", view.Edges)
	}
}

// TestHandleGraphData_GraphPathIsFile covers loadGraphView's "graph is not a
// directory" branch (data.go: the stat succeeds but info.IsDir() is false).
// A stray regular file named "graph" in the roadmap home is treated as "no
// graph yet", so the endpoint returns the empty Graph View Data shape (200)
// rather than erroring. This guards the read path against a non-directory
// collision without creating or touching any store.
func TestHandleGraphData_GraphPathIsFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")

	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatalf("resolving roadmap dir: %v", err)
	}
	if werr := os.WriteFile(filepath.Join(roadmapDir, "graph"), []byte("stray file"), 0o600); werr != nil {
		t.Fatalf("writing stray graph file: %v", werr)
	}

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/graph/data", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("graph data status = %d, want 200 when graph path is a file; body=%q", rec.Code, rec.Body.String())
	}
	var view graphView
	if derr := json.Unmarshal(rec.Body.Bytes(), &view); derr != nil {
		t.Fatalf("decoding graph data: %v", derr)
	}
	if len(view.Nodes) != 0 || len(view.Edges) != 0 {
		t.Errorf("graph = %+v, want empty nodes/edges", view)
	}
}

// TestHandleGraphPage_HappyPath drives handleGraphPage against an existing
// roadmap: it renders the graph page shell (200 HTML) that bootstraps the
// client-side visualisation. This covers the renderHTML call for graph.html.
func TestHandleGraphPage_HappyPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/graph", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("graph page status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeHTML {
		t.Errorf("content-type = %q, want %q", ct, contentTypeHTML)
	}
}

// TestHandleFallback_UnknownReadPathIs404 covers handleFallback's GET/HEAD arm
// directly: a read of a path matched by no specific route lands here and must
// produce 404. The 405 arm (non-read method) is covered by TestRoutes_NameGuard.
func TestHandleFallback_UnknownReadPathIs404(t *testing.T) {
	mux := buildMux()

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req := httptest.NewRequest(method, "/no/such/page", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s /no/such/page: status = %d, want 404", method, rec.Code)
		}
	}
}

// TestHandleIndex_ListError covers handleIndex's 500 branch: when the data
// directory cannot be read, loadRoadmapNames returns an error and the handler
// must respond 500 rather than render a page. We make ~/.roadmaps a regular
// FILE so os.ReadDir inside ListRoadmaps returns a non-IsNotExist error (a
// "not a directory" error) deterministically, without OS-level fault
// injection.
func TestHandleIndex_ListError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Place a file exactly where ~/.roadmaps is expected, so ReadDir fails.
	roadmapsPath := filepath.Join(home, ".roadmaps")
	if err := os.WriteFile(roadmapsPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seeding ~/.roadmaps as a file: %v", err)
	}

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("index status = %d, want 500 when data dir is unreadable", rec.Code)
	}
}

// TestResolveRoadmap_ExistsError covers resolveRoadmap's 500 branch
// (routes.go: RoadmapExists I/O error). The {name} passes validation, but the
// existence check stats ~/.roadmaps/<name>/project.db where <name> is itself a
// regular FILE, so the stat returns a "not a directory" error (not
// IsNotExist). That is an internal read error, not a not-found, so the handler
// must respond 500.
func TestResolveRoadmap_ExistsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	roadmapsDir := filepath.Join(home, ".roadmaps")
	if err := os.MkdirAll(roadmapsDir, 0o700); err != nil {
		t.Fatalf("creating ~/.roadmaps: %v", err)
	}
	// "data-pipeline" is a valid roadmap name, but here it is a file, so
	// stat("~/.roadmaps/data-pipeline/project.db") yields ENOTDIR.
	if err := os.WriteFile(filepath.Join(roadmapsDir, "data-pipeline"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seeding roadmap name as a file: %v", err)
	}

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/data-pipeline", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("detail status = %d, want 500 on RoadmapExists I/O error", rec.Code)
	}
}

// TestRenderHTML_TemplateError covers renderHTML's error branch: executing a
// template name that does not exist in the set fails inside ExecuteTemplate, so
// the handler must respond 500 and must NOT have written a partial 200 (the
// buffer-then-write design exists precisely to make this clean).
func TestRenderHTML_TemplateError(t *testing.T) {
	rec := httptest.NewRecorder()
	renderHTML(rec, "no-such-template.html", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	// A clean 500 means the HTML content type was never set on a half-written
	// success response.
	if ct := rec.Header().Get("Content-Type"); ct == contentTypeHTML {
		t.Errorf("content-type = %q, want non-HTML (error path)", ct)
	}
}

// TestRenderJSON_EncodeError covers renderJSON's error branch: a value that the
// encoder cannot marshal (a channel) makes Encode fail, so the handler must
// respond 500 rather than emit a malformed body with a JSON content type.
func TestRenderJSON_EncodeError(t *testing.T) {
	rec := httptest.NewRecorder()
	renderJSON(rec, make(chan int)) // channels are not JSON-encodable
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == contentTypeJSON {
		t.Errorf("content-type = %q, want non-JSON (error path)", ct)
	}
}

// TestRenderJSON_HappyPath covers renderJSON's success branch directly: it
// encodes the value as pretty JSON with the JSON content type and a trailing
// newline (the project-wide JSON convention).
func TestRenderJSON_HappyPath(t *testing.T) {
	rec := httptest.NewRecorder()
	renderJSON(rec, graphView{Nodes: []map[string]any{}, Edges: []map[string]any{}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeJSON {
		t.Errorf("content-type = %q, want %q", ct, contentTypeJSON)
	}
	body := rec.Body.String()
	if !contains(body, "\"nodes\"") || !contains(body, "\"edges\"") {
		t.Errorf("json body = %q, want nodes/edges keys", body)
	}
}
