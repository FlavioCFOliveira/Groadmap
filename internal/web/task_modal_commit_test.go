package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the two commit-tracking fields on the task detail
// modal (SPEC/WEB.md § Task Detail Modal, Fields shown; Acceptance Criterion 15).
//
// The interesting case is not the fully populated task — it is the REOPENED one.
// A reopen clears commit_close and preserves commit_open, so a task can carry one
// hash and not the other, and the modal must present that half-populated state as
// readably as the other two. A renderer that only ever ran against a task with
// both hashes, or neither, would never meet it.

// seedCommitFixture creates a roadmap holding the three states a task's commit
// pair can be in, and returns their ids in that order: neither hash (never
// started), both hashes (completed), and commit_open alone (completed, then
// reopened).
func seedCommitFixture(t *testing.T, name string) (untouched, completed, reopened int) {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	const now = "2026-08-14T09:30:00Z"

	mk := func(title string) int {
		id, cerr := seedTask(database, seededTask(now, title))
		if cerr != nil {
			t.Fatalf("creating task %q: %v", title, cerr)
		}
		return id
	}

	untouched = mk("Draft the settlement reconciliation report")
	completed = mk("Migrate the ledger export to the batched writer")
	reopened = mk("Harden the payment webhook against replayed deliveries")

	// The hashes are written directly, because this file gates the RENDERING of
	// the two columns, not the command layer that fills them; the transitions
	// themselves are gated in internal/commands and in tests/test_52.
	set := func(id int, query string, args ...any) {
		if _, uerr := database.ExecContext(ctx, query, append(args, id)...); uerr != nil {
			t.Fatalf("seeding commit hashes on task %d: %v", id, uerr)
		}
	}
	set(completed,
		"UPDATE tasks SET status = ?, started_at = ?, tested_at = ?, closed_at = ?, commit_open = ?, commit_close = ? WHERE id = ?",
		models.StatusCompleted, now, now, now,
		"5f93b518375f7f65df4f275f1ee9b2b2e2fd17f0", "2578d18abc1234567890abcdef1234567890abcd")
	// Exactly the row a reopen leaves behind: timestamps and commit_close gone,
	// commit_open surviving.
	set(reopened,
		"UPDATE tasks SET status = ?, commit_open = ? WHERE id = ?",
		models.StatusBacklog, "391cff7cba9876543210fedcba9876543210fedc")

	return untouched, completed, reopened
}

// TestTaskDetailEndpoint_CarriesBothCommitHashes proves the endpoint publishes
// the two columns in all three reachable states. The half-populated row is the
// one that matters: it is what a reopen produces, and it is the state the SPEC
// singles out as reachable rather than theoretical.
func TestTaskDetailEndpoint_CarriesBothCommitHashes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	untouched, completed, reopened := seedCommitFixture(t, "settlement-reconciliation")
	mux := buildMux()

	fetch := func(id int) models.Task {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/roadmaps/settlement-reconciliation/tasks/"+itoa(id)+"/data", nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("task %d: endpoint answered %d, want 200", id, rec.Code)
		}
		var payload struct {
			Task models.Task `json:"task"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("task %d: decoding payload: %v", id, err)
		}
		return payload.Task
	}

	if got := fetch(untouched); got.CommitOpen != nil || got.CommitClose != nil {
		t.Errorf("a task that never started carries commit_open=%v commit_close=%v; both must be null",
			deref(got.CommitOpen), deref(got.CommitClose))
	}

	done := fetch(completed)
	if deref(done.CommitOpen) != "5f93b518375f7f65df4f275f1ee9b2b2e2fd17f0" {
		t.Errorf("completed task commit_open = %q, want the seeded opening hash", deref(done.CommitOpen))
	}
	if deref(done.CommitClose) != "2578d18abc1234567890abcdef1234567890abcd" {
		t.Errorf("completed task commit_close = %q, want the seeded closing hash", deref(done.CommitClose))
	}

	back := fetch(reopened)
	if deref(back.CommitOpen) != "391cff7cba9876543210fedcba9876543210fedc" {
		t.Errorf("reopened task commit_open = %q; a reopen preserves it", deref(back.CommitOpen))
	}
	if back.CommitClose != nil {
		t.Errorf("reopened task commit_close = %q; a reopen clears it", deref(back.CommitClose))
	}
}

// TestTaskModalScript_RendersBothCommitHashes pins what the modal does with the
// two values. The SPEC is explicit about what the modal must NOT grow here: no
// link to a code-hosting service and no copy control, because the page is
// read-only and offline and holds no repository URL from which such a link could
// be built. Those absences are asserted, not assumed — an added anchor would
// otherwise pass every other test in this package.
func TestTaskModalScript_RendersBothCommitHashes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	script := stripJSComments(readEmbeddedAsset(t, "static/task-modal.js"))

	for _, field := range []string{"task.commit_open", "task.commit_close"} {
		if !strings.Contains(script, field) {
			t.Errorf("the modal script never reads %s, so the field cannot appear in the modal", field)
		}
	}
	for _, label := range []string{`"Commit open"`, `"Commit close"`} {
		if !strings.Contains(script, label) {
			t.Errorf("the modal script carries no %s label", label)
		}
	}
	// A hash is compared character by character against a repository, so it is
	// monospaced rather than set in the body face.
	if !strings.Contains(script, "font-monospace") {
		t.Error("the modal script does not render the commit hashes monospaced")
	}
	// The two prohibitions from SPEC/WEB.md § Task Detail Modal.
	for _, forbidden := range []string{"github.com", "gitlab.com", "commit/", "navigator.clipboard"} {
		if strings.Contains(script, forbidden) {
			t.Errorf("the modal script contains %q; the modal adds no code-host link and no copy control "+
				"for the commit hashes", forbidden)
		}
	}
}

// deref renders a nullable string for an error message without a nil check at
// every call site.
func deref(s *string) string {
	if s == nil {
		return "<null>"
	}
	return *s
}
