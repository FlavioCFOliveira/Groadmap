package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// TestTaskStatusBadge asserts the FULL task-status -> Tabler colour-variant
// mapping defined in SPEC/WEB.md § Status, Priority, and Severity Badge Colours.
// Every canonical TaskStatus is covered, plus the neutral fallback for an
// out-of-enum value, proving the helper is total (SPEC rule 1).
func TestTaskStatusBadge(t *testing.T) {
	cases := []struct {
		status models.TaskStatus
		want   string
	}{
		{models.StatusCompleted, "bg-green-lt"},
		{models.StatusTesting, "bg-yellow-lt"},
		{models.StatusDoing, "bg-blue-lt"},
		{models.StatusSprint, "bg-cyan-lt"},
		{models.StatusBacklog, "bg-secondary-lt"},
		{models.TaskStatus("GARBAGE"), "bg-secondary-lt"}, // out-of-enum -> neutral
	}
	for _, c := range cases {
		if got := taskStatusBadge(c.status); got != c.want {
			t.Errorf("taskStatusBadge(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestTaskStatusBadge_CoversEveryEnumValue guards against a future TaskStatus
// being added in MODELS.md without a mapping here: it iterates the canonical
// ValidTaskStatuses list and fails if any value falls through to the neutral
// fallback by accident (only BACKLOG legitimately maps to bg-secondary-lt).
func TestTaskStatusBadge_CoversEveryEnumValue(t *testing.T) {
	for _, s := range models.ValidTaskStatuses {
		got := taskStatusBadge(s)
		if got == "" {
			t.Errorf("taskStatusBadge(%q) returned empty class; mapping must be total", s)
		}
		if got == "bg-secondary-lt" && s != models.StatusBacklog {
			t.Errorf("taskStatusBadge(%q) = neutral fallback %q; add an explicit mapping per SPEC", s, got)
		}
	}
}

// TestSprintStatusBadge asserts the FULL sprint-status -> Tabler colour-variant
// mapping defined in SPEC/WEB.md § Status, Priority, and Severity Badge Colours.
func TestSprintStatusBadge(t *testing.T) {
	cases := []struct {
		status models.SprintStatus
		want   string
	}{
		{models.SprintClosed, "bg-green-lt"},
		{models.SprintOpen, "bg-blue-lt"},
		{models.SprintPending, "bg-secondary-lt"},
		{models.SprintStatus("GARBAGE"), "bg-secondary-lt"}, // out-of-enum -> neutral
	}
	for _, c := range cases {
		if got := sprintStatusBadge(c.status); got != c.want {
			t.Errorf("sprintStatusBadge(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}

// TestSprintStatusBadge_CoversEveryEnumValue guards against a future
// SprintStatus being added without a mapping: every canonical value must
// resolve, and only PENDING legitimately maps to the neutral variant.
func TestSprintStatusBadge_CoversEveryEnumValue(t *testing.T) {
	for _, s := range models.ValidSprintStatuses {
		got := sprintStatusBadge(s)
		if got == "" {
			t.Errorf("sprintStatusBadge(%q) returned empty class; mapping must be total", s)
		}
		if got == "bg-secondary-lt" && s != models.SprintPending {
			t.Errorf("sprintStatusBadge(%q) = neutral fallback %q; add an explicit mapping per SPEC", s, got)
		}
	}
}

// TestPriorityBadge asserts the priority band boundaries from SPEC/WEB.md
// § Status, Priority, and Severity Badge Colours: 7-9 -> red, 4-6 -> yellow,
// 0-3 -> secondary. It checks every band boundary (0, 3, 4, 6, 7, 9) and that
// the whole 0-9 range resolves to a non-empty class with no gap (totality).
func TestPriorityBadge(t *testing.T) {
	cases := []struct {
		priority int
		want     string
	}{
		{0, "bg-secondary-lt"},
		{3, "bg-secondary-lt"},
		{4, "bg-yellow-lt"},
		{6, "bg-yellow-lt"},
		{7, "bg-red-lt"},
		{9, "bg-red-lt"},
	}
	for _, c := range cases {
		if got := priorityBadge(c.priority); got != c.want {
			t.Errorf("priorityBadge(%d) = %q, want %q", c.priority, got, c.want)
		}
	}
	for p := 0; p <= 9; p++ {
		if priorityBadge(p) == "" {
			t.Errorf("priorityBadge(%d) returned empty class; the 0-9 range must be total", p)
		}
	}
}

// TestSeverityBadge asserts the severity band boundaries from SPEC/WEB.md
// § Status, Priority, and Severity Badge Colours: 8-9 -> red, 6-7 -> orange,
// 3-5 -> yellow, 0-2 -> secondary. It checks every band boundary
// (0, 2, 3, 5, 6, 7, 8, 9) and that the whole 0-9 range resolves (totality).
func TestSeverityBadge(t *testing.T) {
	cases := []struct {
		severity int
		want     string
	}{
		{0, "bg-secondary-lt"},
		{2, "bg-secondary-lt"},
		{3, "bg-yellow-lt"},
		{5, "bg-yellow-lt"},
		{6, "bg-orange-lt"},
		{7, "bg-orange-lt"},
		{8, "bg-red-lt"},
		{9, "bg-red-lt"},
	}
	for _, c := range cases {
		if got := severityBadge(c.severity); got != c.want {
			t.Errorf("severityBadge(%d) = %q, want %q", c.severity, got, c.want)
		}
	}
	for s := 0; s <= 9; s++ {
		if severityBadge(s) == "" {
			t.Errorf("severityBadge(%d) returned empty class; the 0-9 range must be total", s)
		}
	}
}

// seedBadgeRoadmap creates an on-disk roadmap whose single task carries
// distinctive, non-neutral status/priority/severity values, and an OPEN sprint,
// so the rendered HTML carries unambiguous, distinct Tabler colour variants the
// template-level assertions below can detect. The caller must already have
// redirected HOME with t.Setenv.
func seedBadgeRoadmap(t *testing.T, name string) (roadmap string, sprintID int) {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	now := time.Now().UTC().Format(time.RFC3339)

	// The task is created and then added to a sprint, which transitions its
	// status to SPRINT (-> bg-cyan-lt). priority 8 -> bg-red-lt, severity 9 ->
	// bg-red-lt. SPRINT is a distinctive, non-neutral status so the rendered
	// colour is unambiguous.
	task := &models.Task{
		Priority:               8,
		Severity:               9,
		Status:                 models.StatusBacklog,
		Title:                  "Render semantic badge colours across the web UI",
		FunctionalRequirements: "Map status, priority, and severity to Tabler colour variants",
		TechnicalRequirements:  "html/template FuncMap helpers driven by models enums",
		AcceptanceCriteria:     "Every badge uses the SPEC colour for its value",
		CreatedAt:              now,
	}
	taskID, err := database.CreateTask(context.Background(), task)
	if err != nil {
		t.Fatalf("creating task: %v", err)
	}

	// OPEN -> bg-blue-lt on the sprint card, header, and datagrid.
	sprint := &models.Sprint{
		Status:      models.SprintOpen,
		Title:       "Apply the semantic badge colour mapping",
		Description: "Apply the semantic badge colour mapping",
		Order:       1,
		StartedAt:   &now,
		CreatedAt:   now,
	}
	sprintID, err = database.CreateSprint(context.Background(), sprint)
	if err != nil {
		t.Fatalf("creating sprint: %v", err)
	}
	if err := database.AddTasksToSprint(context.Background(), sprintID, []int{taskID}); err != nil {
		t.Fatalf("adding task to sprint: %v", err)
	}
	return name, sprintID
}

// TestTasksPage_RendersSemanticBadgeColours proves the helpers are actually
// wired into the tasks template and emit the SPEC colour variant in the rendered
// HTML: a SPRINT / priority 8 / severity 9 task must render bg-cyan-lt,
// bg-red-lt, and bg-red-lt badges respectively, and priority/severity must now
// be Tabler badges rather than the old plain cells (SPEC/WEB.md § Status,
// Priority, and Severity Badge Colours, rule 2).
func TestTasksPage_RendersSemanticBadgeColours(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name, _ := seedBadgeRoadmap(t, "badge-colours")

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/tasks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("tasks status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Status badge: SPRINT -> bg-cyan-lt.
	if !strings.Contains(body, `<span class="badge bg-cyan-lt">SPRINT</span>`) {
		t.Errorf("tasks page missing SPRINT status badge with bg-cyan-lt")
	}
	// Priority 8 -> bg-red-lt badge (was previously a plain cell).
	if !strings.Contains(body, `<span class="badge bg-red-lt">8</span>`) {
		t.Errorf("tasks page missing priority badge with bg-red-lt for priority 8")
	}
	// Severity 9 -> bg-red-lt badge (was previously a plain cell).
	if !strings.Contains(body, `<span class="badge bg-red-lt">9</span>`) {
		t.Errorf("tasks page missing severity badge with bg-red-lt for severity 9")
	}
}

// TestSprintPage_RendersSemanticStatusBadge proves the sprint status helper is
// wired into the sprint page header, the sprint detail datagrid, and the member
// task table: an OPEN sprint must render bg-blue-lt for its status, and the
// member SPRINT / priority 8 / severity 9 task must render its semantic badges
// in the sprint detail table (SPEC/WEB.md § Status, Priority, and Severity Badge
// Colours, rule 2).
func TestSprintPage_RendersSemanticStatusBadge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name, sprintID := seedBadgeRoadmap(t, "badge-colours")

	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/sprints/"+itoa(sprintID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sprint status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// OPEN sprint status -> bg-blue-lt (appears in header and datagrid).
	if !strings.Contains(body, "bg-blue-lt") || !strings.Contains(body, ">OPEN<") {
		t.Errorf("sprint page missing OPEN status badge with bg-blue-lt")
	}
	// Member task badges in the sprint detail table (the task is SPRINT after
	// being added to the sprint -> bg-cyan-lt).
	if !strings.Contains(body, `<span class="badge bg-cyan-lt">SPRINT</span>`) {
		t.Errorf("sprint detail table missing SPRINT status badge with bg-cyan-lt")
	}
	if !strings.Contains(body, `<span class="badge bg-red-lt">8</span>`) {
		t.Errorf("sprint detail table missing priority badge with bg-red-lt for priority 8")
	}
	if !strings.Contains(body, `<span class="badge bg-red-lt">9</span>`) {
		t.Errorf("sprint detail table missing severity badge with bg-red-lt for severity 9")
	}
}
