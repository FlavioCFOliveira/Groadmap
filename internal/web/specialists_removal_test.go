// Package web — regression suite for the removal of the task specialists field
// from the read-only web interface (rmp task #248).
//
// The Task entity lost its `specialists` field: the column, the model field, the
// CLI surface, and — here — the presentation. SPEC/WEB.md was rewritten at 12
// sites, including Acceptance Criteria 15, 85, 91, 101 and 133, and this file
// pins the five things that must stay true of the web package afterwards:
//
//  1. Nothing the server SHIPS names the field. The templates and the static
//     assets are embedded, so the sweep reads the compiled-in bytes rather than
//     the working tree: an asset that named the field could not reach a page
//     without failing this test first.
//  2. Nothing the package COMPILES names the field, which is the handler and
//     loader half of the same claim.
//  3. The view model the templates resolve names against carries neither a field
//     nor a method for it. html/template resolves promoted fields and exported
//     methods by name at execution time, so this reflection walk covers exactly
//     the surface a template expression could reach.
//  4. The task page, which shows a task's full field set, names it nowhere.
//
// The tasks page's search deliberately excludes the field and always did; its
// query parameters and their behaviour are pinned by tasks_list_test.go, which
// this file does not duplicate.
package web

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// retiredFieldNeedle is the substring every sweep below searches for, folded to
// lower case. It is the stem rather than the whole field name, so it catches the
// singular ("specialist"), the plural ("specialists"), the Go identifier
// ("Specialists"), the retired method ("SpecialistsText"), the retired card role
// ("task-card-specialists") and the retired JSON key alike.
const retiredFieldNeedle = "specialist"

// ==================== WHAT THE SERVER SHIPS ====================

// TestSpecialistsRemoval_NoEmbeddedAssetNamesTheField sweeps every embedded
// template and every embedded static asset for the retired field.
//
// It reads templatesFS and staticFS — the compiled-in filesystems the server
// actually serves from, never the host filesystem (SPEC/WEB.md § Self-Contained
// Deliverable) — so it cannot pass against a stale working tree, and it covers
// every asset the removal could have left the field in, which is the point of
// sweeping rather than asserting file by file.
func TestSpecialistsRemoval_NoEmbeddedAssetNamesTheField(t *testing.T) {
	swept := 0
	for what, tree := range map[string]fs.FS{"templates": templatesFS, "static": staticFS} {
		err := fs.WalkDir(tree, ".", func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			content, readErr := fs.ReadFile(tree, path)
			if readErr != nil {
				return readErr
			}
			swept++
			if at := indexOfNeedle(string(content)); at >= 0 {
				t.Errorf("the embedded %s asset %s still names the removed specialists field: %q",
					what, path, excerptAround(string(content), at))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking the embedded %s tree: %v", what, err)
		}
	}

	// The sweep must have had something to sweep. A walk over an empty tree
	// reports no hit and would otherwise pass silently.
	if swept < 2 {
		t.Fatalf("the sweep read %d embedded asset(s); the interface ships more than that, so "+
			"the walk found the wrong tree", swept)
	}
}

// TestSpecialistsRemoval_NeedleDiscriminates is the control for both sweeps: it
// proves the search actually finds the retired markup when it is there.
//
// Without it, a sweep whose needle no longer matched anything — a renamed
// constant, a folding mistake — would report a clean tree for the wrong reason.
// The samples are the exact lines the removal deleted.
func TestSpecialistsRemoval_NeedleDiscriminates(t *testing.T) {
	for _, deleted := range []string{
		`{{with .SpecialistsText}}<span data-role="task-card-specialists">` +
			`<i class="ti ti-users me-1"></i>{{.}}</span>{{end}}`,
		`    grid.appendChild(datagridItem("Specialists", task.specialists));`,
		"func (v *taskView) SpecialistsText() string {",
		"		v.SpecialistsText() != \"\" ||",
	} {
		if indexOfNeedle(deleted) < 0 {
			t.Errorf("the sweep needle %q does not match %q, so a clean sweep proves nothing",
				retiredFieldNeedle, deleted)
		}
	}

	// And it must not fire on text that merely resembles the field, or the sweeps
	// would be unable to distinguish a real regression from ordinary prose.
	for _, innocent := range []string{
		`<span data-role="task-card-subtasks"><i class="ti ti-subtask me-1"></i>Subtasks: 2</span>`,
		"func (v *taskView) HasMeta() bool {",
	} {
		if at := indexOfNeedle(innocent); at >= 0 {
			t.Errorf("the sweep needle matched innocent text %q at %d", innocent, at)
		}
	}
}

// indexOfNeedle returns the index of the retired field's stem in text, folded to
// lower case on both sides, or -1. Folding the text rather than listing every
// spelling is what makes one needle cover the Go identifier, the JSON key, the
// card role and the prose.
func indexOfNeedle(text string) int {
	return strings.Index(strings.ToLower(text), retiredFieldNeedle)
}

// excerptAround returns the line the hit sits on, so a failure names the offending
// line rather than dumping a whole asset.
func excerptAround(text string, at int) string {
	start := strings.LastIndexByte(text[:at], '\n') + 1
	end := strings.IndexByte(text[at:], '\n')
	if end < 0 {
		return text[start:]
	}
	return text[start : at+end]
}

// ==================== WHAT THE PACKAGE COMPILES ====================

// TestSpecialistsRemoval_NoPackageSourceNamesTheField sweeps every non-test Go
// file of this package for the retired field: the handlers, the loaders, the view
// models, and the helpers they share.
//
// Test files are excluded deliberately. This file names the field throughout —
// that is what a removal regression suite is for — and the four updated test
// files name the retired card role in their absence assertions. What must be
// clean is the code that runs in the binary.
func TestSpecialistsRemoval_NoPackageSourceNamesTheField(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	swept := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, rerr := os.ReadFile(filepath.Clean(name))
		if rerr != nil {
			t.Fatalf("reading %s: %v", name, rerr)
		}
		swept++
		if at := indexOfNeedle(string(content)); at >= 0 {
			t.Errorf("%s still names the removed specialists field: %q",
				name, excerptAround(string(content), at))
		}
	}

	// data.go is where the retired accessor and the metadata predicate lived, so
	// a sweep that missed it would miss the whole point.
	if swept == 0 {
		t.Fatalf("the sweep read no non-test Go file; it is looking in the wrong directory")
	}
}

// ==================== WHAT A TEMPLATE CAN RESOLVE ====================

// TestSpecialistsRemoval_ViewModelExposesNothingForTheField walks the view model
// the board card and the sprint row are executed against, and asserts it exposes
// neither a field nor a method for the retired value.
//
// This is the compiled statement of the same claim the source sweep makes, and it
// is the one that matches how a template actually reaches a value: html/template
// resolves a name against the exported fields — INCLUDING the ones promoted from
// the embedded models.Task — and the exported methods of the value it is given.
// A field or method that reappeared under either route would make
// `{{.Specialists}}` or `{{.SpecialistsText}}` render again, and this test is what
// stands between that and a page.
func TestSpecialistsRemoval_ViewModelExposesNothingForTheField(t *testing.T) {
	// The two view models a task reaches a template through: the sprint board's
	// card, and the tasks page's list row, which is the task itself.
	for _, viewType := range []reflect.Type{reflect.TypeOf(taskView{}), reflect.TypeOf(models.Task{})} {
		// Fields, embedded ones included. FieldByName traverses the embedded struct
		// exactly as the template's name resolution does.
		for _, name := range promotedFieldNames(viewType) {
			if indexOfNeedle(name) >= 0 {
				t.Errorf("%s still exposes the field %q; a template could render it", viewType, name)
			}
		}

		// The two exact names the template used, asserted by the resolution rule the
		// template uses, so the walk above cannot pass by walking the wrong type.
		for _, name := range []string{"Specialists", "SpecialistsText"} {
			if _, ok := viewType.FieldByName(name); ok {
				t.Errorf("%s.%s resolves as a field; the Task entity no longer carries it", viewType, name)
			}
		}

		// Methods. Only exported methods are reachable from a template, and only
		// exported methods are what reflect reports here, so the two sets coincide.
		pointerType := reflect.PointerTo(viewType)
		for i := range pointerType.NumMethod() {
			if name := pointerType.Method(i).Name; indexOfNeedle(name) >= 0 {
				t.Errorf("%s still exposes the method %q; a template could call it", pointerType, name)
			}
		}

		// The control: the walk really does see the names it is meant to police. If
		// promotedFieldNames returned nothing, every assertion above would be vacuous.
		names := promotedFieldNames(viewType)
		if !containsName(names, "Title") || !containsName(names, "SubtaskCount") {
			t.Fatalf("the field walk did not reach the promoted models.Task fields of %s; it saw %v", viewType, names)
		}
	}
	// The method walk's control: it does see an exported method where one exists.
	if !hasMethod(reflect.TypeOf(sprintView{}), "Card") {
		t.Fatalf("the method walk did not see sprintView.Card, so asserting an absence proves nothing")
	}
}

// promotedFieldNames returns every field name a template can resolve on t: its
// own fields plus the fields promoted from any embedded struct, recursively.
func promotedFieldNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			names = append(names, promotedFieldNames(field.Type)...)
			continue
		}
		names = append(names, field.Name)
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func hasMethod(t reflect.Type, want string) bool {
	_, ok := t.MethodByName(want)
	return ok
}

// singleIndicatorFixture names the six tasks seedSingleIndicatorFixture creates:
// one in a sprint, one with a subtask, the two ends of one dependency edge, one
// with a comment, and one carrying none of these.
type singleIndicatorFixture struct {
	name string

	sprintID int

	sprintOnly   int // in a sprint; no subtask, no edge, no comment
	subtasksOnly int // one subtask; in no sprint
	dependsOnly  int // depends on blocksOnly
	blocksOnly   int // blocked by dependsOnly, which is the same one edge
	commentsOnly int // one comment
	bare         int // nothing at all
}

// seedSingleIndicatorFixture builds a roadmap whose tasks each carry one kind of
// related data, so the task page under test has every kind of field populated
// somewhere in the roadmap.
func seedSingleIndicatorFixture(t *testing.T, name string) singleIndicatorFixture {
	t.Helper()

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", name, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	f := singleIndicatorFixture{name: name}

	newTask := func(title, created string, parent *int) int {
		t.Helper()
		id, cerr := seedTask(database, &models.Task{
			Title:                  title,
			Type:                   models.TypeTask,
			Status:                 models.StatusBacklog,
			Priority:               5,
			Severity:               3,
			ParentTaskID:           parent,
			FunctionalRequirements: "The task page must show every field of this task.",
			TechnicalRequirements:  "Seeded read-only against the roadmap database.",
			AcceptanceCriteria:     "The task page names no retired field.",
			CreatedAt:              created,
		})
		if cerr != nil {
			t.Fatalf("creating task %q: %v", title, cerr)
		}
		return id
	}

	f.sprintOnly = newTask("Rotate the settlement acquirer API credentials",
		"2026-04-01T09:00:00Z", nil)
	f.subtasksOnly = newTask("Split the nightly reconciliation into acquirer batches",
		"2026-04-02T09:00:00Z", nil)
	f.dependsOnly = newTask("Publish the reconciliation dashboard to the finance team",
		"2026-04-03T09:00:00Z", nil)
	f.blocksOnly = newTask("Freeze the legacy reconciliation export",
		"2026-04-04T09:00:00Z", nil)
	f.commentsOnly = newTask("Alert on residual balances after the nightly close",
		"2026-04-05T09:00:00Z", nil)
	f.bare = newTask("Audit the settlement webhook signature verification",
		"2026-04-06T09:00:00Z", nil)

	// The subtask, which raises subtasksOnly's subtask_count without giving the
	// child any indicator of its own.
	newTask("Group the acquirer settlement lines by batch reference",
		"2026-04-07T09:00:00Z", &f.subtasksOnly)

	// The sprint. Membership forces the member's status to SPRINT, which is why
	// sprintOnly is read from the SPRINT column and the rest from BACKLOG.
	f.sprintID = newSprint(t, database, "Settlement reconciliation",
		"Reconcile the acquirer settlement file nightly and alert on any residual.")
	if aerr := database.AddTasksToSprint(ctx, f.sprintID, []int{f.sprintOnly}); aerr != nil {
		t.Fatalf("adding the credential-rotation task to the sprint: %v", aerr)
	}

	// The one dependency edge, which supplies both remaining indicators.
	if derr := database.AddTaskDependencyWithAudit(ctx, f.dependsOnly, f.blocksOnly); derr != nil {
		t.Fatalf("making the dashboard task depend on the export freeze: %v", derr)
	}

	addTaskCommentTo(t, database, f.commentsOnly, models.CommentNote,
		"The residual threshold is agreed with finance at one cent per acquirer.",
		"2026-04-08T09:00:00Z")

	return f
}

// ==================== THE TASK PAGE ====================

// TestSpecialistsRemoval_TaskPageHasNoItemForTheField serves the page that shows a
// task's full field set and asserts no part of it names the retired field: the
// Details card lists every short field, so a datagrid item that outlived the
// field would show here (SPEC/WEB.md § Roadmap Task Page; Acceptance Criterion
// 15).
func TestSpecialistsRemoval_TaskPageHasNoItemForTheField(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	f := seedSingleIndicatorFixture(t, "settlement-platform")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks/"+itoa(f.commentsOnly))

	// The control: the page really is the task's page with its Details card, so
	// the sweep below has something to sweep.
	for _, required := range []string{`<div class="datagrid-title">Subtasks</div>`, "Functional requirements"} {
		if !strings.Contains(body, required) {
			t.Fatalf("the task page carries no %q, so it is not the task's full field set", required)
		}
	}
	if at := indexOfNeedle(body); at >= 0 {
		t.Errorf("the task page still names the retired field at byte %d: %q", at,
			excerptAround(body, at))
	}
}
