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

// seedRoadmapWithAudit creates a real on-disk roadmap under the test's
// temporary HOME and inserts n audit entries with strictly increasing
// performed_at timestamps, so the most recently performed operation (the
// highest entity id) sorts first under the page's performed_at DESC ordering.
// It returns the roadmap name. The caller must already have redirected HOME so
// nothing touches the developer's ~/.roadmaps.
//
// Each entry's EntityID is its 1-based insertion index, so a test can assert
// the ordering and the page slice by entity id without depending on the
// audit row id allocation.
func seedRoadmapWithAudit(t *testing.T, name string, n int) string {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	// Distinct, strictly increasing timestamps so performed_at DESC is a total
	// order: entity id i+1 is performed one second after entity id i, so the
	// last-inserted (highest id) entry is the most recent and sorts first.
	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		entry := &models.AuditEntry{
			Operation:   "TASK_CREATE",
			EntityType:  "TASK",
			EntityID:    i + 1,
			PerformedAt: base.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
		}
		if _, err := database.LogAuditEntry(context.Background(), entry); err != nil {
			t.Fatalf("seeding audit entry %d: %v", i, err)
		}
	}

	return name
}

// getAudit drives handleAudit through the mux for the given roadmap and raw
// query string (for example "page=2"), returning the recorder.
func getAudit(t *testing.T, name, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	mux := buildMux()
	path := "/roadmaps/" + name + "/audit"
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestHandleAudit_HappyPathOrdering verifies the audit page renders 200 HTML
// and lists entries ordered by performed_at DESC (most recent first). With a
// handful of entries the whole log fits on page 1 and "Page 1 of 1" is shown
// (SPEC/WEB.md § Roadmap Audit Log Page, ordering).
func TestHandleAudit_HappyPathOrdering(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmapWithAudit(t, "web-audit-rollout", 5)

	rec := getAudit(t, name, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("audit status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeHTML {
		t.Errorf("content-type = %q, want %q", ct, contentTypeHTML)
	}
	body := rec.Body.String()

	if !contains(body, "Audit log") {
		t.Errorf("audit body missing the audit card title")
	}
	if !contains(body, "Page 1 of 1") {
		t.Errorf("audit body missing 'Page 1 of 1'; body=%q", body)
	}

	// Ordering: entity id 5 (the most recent) must appear before entity id 1
	// (the oldest) in the rendered table body.
	posNewest := strings.Index(body, "<td>5</td>")
	posOldest := strings.Index(body, "<td>1</td>")
	if posNewest < 0 || posOldest < 0 {
		t.Fatalf("expected both entity ids 5 and 1 in body; newest=%d oldest=%d", posNewest, posOldest)
	}
	if posNewest > posOldest {
		t.Errorf("entries not ordered performed_at DESC: newest (id 5) at %d should precede oldest (id 1) at %d", posNewest, posOldest)
	}
}

// TestHandleAudit_FullFirstPageAndRemainder verifies a roadmap with more than
// one page of audit entries renders exactly auditPageSize rows on a full first
// page, and the remainder on the last page, with the correct "Page X of Y"
// indicator (SPEC/WEB.md § Roadmap Audit Log Page, pagination).
func TestHandleAudit_FullFirstPageAndRemainder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// 100 + 30 entries -> 2 pages: 100 on page 1, 30 on page 2.
	const remainder = 30
	total := auditPageSize + remainder
	name := seedRoadmapWithAudit(t, "web-audit-paged", total)

	// Page 1: exactly auditPageSize data rows, "Page 1 of 2".
	rec := getAudit(t, name, "page=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("page 1 status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if got := countDataRows(body); got != auditPageSize {
		t.Errorf("page 1 data rows = %d, want %d", got, auditPageSize)
	}
	if !contains(body, "Page 1 of 2") {
		t.Errorf("page 1 missing 'Page 1 of 2'")
	}

	// Page 2: exactly the remainder rows, "Page 2 of 2".
	rec = getAudit(t, name, "page=2")
	if rec.Code != http.StatusOK {
		t.Fatalf("page 2 status = %d, want 200", rec.Code)
	}
	body = rec.Body.String()
	if got := countDataRows(body); got != remainder {
		t.Errorf("page 2 data rows = %d, want %d", got, remainder)
	}
	if !contains(body, "Page 2 of 2") {
		t.Errorf("page 2 missing 'Page 2 of 2'")
	}
}

// countDataRows counts the audit table data rows in the rendered body. The
// header row uses <th> cells, so every <tr> that introduces a <td> cell is a
// data row; counting "<tr>\n" occurrences that wrap <td> cells is brittle, so
// we count the unique per-row "Performed At" cell class instead, which appears
// exactly once per data row and never in the header.
func countDataRows(body string) int {
	return strings.Count(body, `<td class="text-nowrap text-secondary">`)
}

// TestHandleAudit_PageClamping verifies every out-of-range or garbage page
// value clamps to a valid page and renders 200, never 404 (SPEC/WEB.md
// § Roadmap Audit Log Page, pagination is clamped, not strict; Routes and
// Pages, audit page status mapping).
func TestHandleAudit_PageClamping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// 250 entries -> 3 pages (100 + 100 + 50).
	total := 250
	name := seedRoadmapWithAudit(t, "web-audit-clamp", total)
	lastPage := "Page 3 of 3"
	firstPage := "Page 1 of 3"

	cases := []struct {
		name     string
		rawQuery string
		wantPage string
	}{
		{"page zero clamps to first", "page=0", firstPage},
		{"negative page clamps to first", "page=-5", firstPage},
		{"non-integer page clamps to first", "page=abc", firstPage},
		{"empty page clamps to first", "page=", firstPage},
		{"absent page is first", "", firstPage},
		{"beyond last clamps to last", "page=99999", lastPage},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := getAudit(t, name, tc.rawQuery)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (clamp, never 404); body=%q", rec.Code, rec.Body.String())
			}
			if !contains(rec.Body.String(), tc.wantPage) {
				t.Errorf("body missing %q for query %q", tc.wantPage, tc.rawQuery)
			}
		})
	}
}

// TestHandleAudit_EmptyState verifies an empty audit log renders 200 with the
// empty-state message and "Page 1 of 1", and that neither pagination control is
// active (no prev/next links) (SPEC/WEB.md § Roadmap Audit Log Page, empty
// state).
func TestHandleAudit_EmptyState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// seedRoadmap creates a roadmap whose audit log is empty (its task/sprint
	// helpers do not write audit rows via the LogAuditEntry path used here).
	name := seedRoadmapWithAudit(t, "web-audit-empty", 0)

	rec := getAudit(t, name, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty audit status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !contains(body, "No audit entries yet") {
		t.Errorf("empty audit body missing empty-state message")
	}
	if !contains(body, "Page 1 of 1") {
		t.Errorf("empty audit body missing 'Page 1 of 1'")
	}
	// No active prev/next links: the only pagination anchors would be
	// href="?page=...". An empty single-page log has neither.
	if contains(body, `href="?page=`) {
		t.Errorf("empty audit page must have no active prev/next pagination link")
	}
}

// TestHandleAudit_PrevNextEdges verifies the Previous control is absent (no
// active link) on the first page, the Next control is absent on the last page,
// and both are present on a middle page (SPEC/WEB.md § Roadmap Audit Log Page,
// pagination controls).
func TestHandleAudit_PrevNextEdges(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// 250 entries -> 3 pages, so page 2 is a genuine middle page.
	name := seedRoadmapWithAudit(t, "web-audit-edges", 250)

	// Page 1: no prev link, a next link.
	body := getAudit(t, name, "page=1").Body.String()
	if contains(body, `href="?page=0"`) {
		t.Errorf("page 1 must not render an active Previous link")
	}
	if !contains(body, `href="?page=2"`) {
		t.Errorf("page 1 must render an active Next link to page 2")
	}

	// Page 3 (last): a prev link, no next link.
	body = getAudit(t, name, "page=3").Body.String()
	if !contains(body, `href="?page=2"`) {
		t.Errorf("page 3 must render an active Previous link to page 2")
	}
	if contains(body, `href="?page=4"`) {
		t.Errorf("page 3 (last) must not render an active Next link")
	}

	// Page 2 (middle): both prev and next links present.
	body = getAudit(t, name, "page=2").Body.String()
	if !contains(body, `href="?page=1"`) {
		t.Errorf("page 2 must render an active Previous link to page 1")
	}
	if !contains(body, `href="?page=3"`) {
		t.Errorf("page 2 must render an active Next link to page 3")
	}
}

// TestHandleAudit_NameGuardAndMethod verifies the {name} guard returns 404 for
// an invalid or nonexistent roadmap, and that a non-read HTTP method returns
// 405 (SPEC/WEB.md § Roadmap Audit Log Page, path parameters; Routes and
// Pages, status mapping).
func TestHandleAudit_NameGuardAndMethod(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// One real roadmap so the 405 cases hit a registered path, not a 404.
	name := seedRoadmapWithAudit(t, "web-audit-guard", 3)
	mux := buildMux()

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"invalid name traversal", http.MethodGet, "/roadmaps/..%2fetc/audit", http.StatusNotFound},
		{"invalid name uppercase", http.MethodGet, "/roadmaps/NotValid/audit", http.StatusNotFound},
		{"valid but missing", http.MethodGet, "/roadmaps/nonexistent-roadmap/audit", http.StatusNotFound},
		{"post is 405", http.MethodPost, "/roadmaps/" + name + "/audit", http.StatusMethodNotAllowed},
		{"put is 405", http.MethodPut, "/roadmaps/" + name + "/audit", http.StatusMethodNotAllowed},
		{"delete is 405", http.MethodDelete, "/roadmaps/" + name + "/audit", http.StatusMethodNotAllowed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("%s %s: status = %d, want %d", tc.method, tc.path, rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestHandleAudit_Head confirms the audit route answers a HEAD request with 200
// and the HTML content type (SPEC/WEB.md § Routes and Pages: all routes serve
// GET and HEAD only).
func TestHandleAudit_Head(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmapWithAudit(t, "web-audit-head", 3)
	mux := buildMux()

	req := httptest.NewRequest(http.MethodHead, "/roadmaps/"+name+"/audit", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD audit: status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeHTML {
		t.Errorf("HEAD audit: content-type = %q, want %q", ct, contentTypeHTML)
	}
}

// TestHandleAudit_CacheControlNoStore confirms the audit response carries
// Cache-Control: no-store, applied by the securityHeaders middleware for every
// non-/static/ route, so a freshly read audit page is never served from a stale
// cache (SPEC/WEB.md § Cache Policy). The full handler() chain is exercised so
// the middleware runs.
func TestHandleAudit_CacheControlNoStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmapWithAudit(t, "web-audit-cache", 3)

	h := handler()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/audit", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("audit status = %d, want 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
	}
}
