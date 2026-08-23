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
//  4. The card's metadata predicate has exactly FIVE contributors and each one is
//     individually sufficient. The retired field was a sixth, and removing a
//     contributor from a disjunction changes when the region renders, so the
//     predicate is pinned in both directions: none of the five renders no footer,
//     and any one of the five alone renders a footer holding that indicator and
//     no other (Acceptance Criteria 85 and 91).
//  5. The JSON the task detail modal is filled from carries no key for it, so the
//     modal script is never fed a key that no longer exists.
//
// The board's four query parameters are NOT touched by this removal: the header
// search deliberately excludes the field and always did, and `q`, `type`,
// `priority` and `severity` remain the whole control surface. Their names, their
// number and their behaviour are pinned by search_test.go and filter_test.go,
// which this file does not duplicate.
package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
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

// retiredCardRole is the data-role the retired metadata indicator carried, and
// retiredCardIcon the Tabler glyph it was drawn with. They are named here because
// the rendered assertions below look for the MARKUP a surviving indicator would
// emit, not for a stored value: no fixture can produce a value for a field the
// model no longer has, so the markup is the only thing left to assert against.
const (
	retiredCardRole = "task-card-specialists"
	retiredCardIcon = "ti ti-users"
)

// ==================== WHAT THE SERVER SHIPS ====================

// TestSpecialistsRemoval_NoEmbeddedAssetNamesTheField sweeps every embedded
// template and every embedded static asset for the retired field.
//
// It reads templatesFS and staticFS — the compiled-in filesystems the server
// actually serves from, never the host filesystem (SPEC/WEB.md § Self-Contained
// Deliverable) — so it cannot pass against a stale working tree, and it covers
// the two sites the removal touched here: the tasks board card's indicator in
// templates/tasks.html and the datagrid item in static/task-modal.js. It also
// covers every asset neither of them is in, which is the point of sweeping
// rather than asserting file by file.
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
	viewType := reflect.TypeOf(taskView{})

	// Fields, embedded ones included. FieldByName traverses the embedded struct
	// exactly as the template's name resolution does.
	for _, name := range promotedFieldNames(viewType) {
		if indexOfNeedle(name) >= 0 {
			t.Errorf("taskView still exposes the field %q; a template could render it", name)
		}
	}

	// The two exact names the template used, asserted by the resolution rule the
	// template uses, so the walk above cannot pass by walking the wrong type.
	for _, name := range []string{"Specialists", "SpecialistsText"} {
		if _, ok := viewType.FieldByName(name); ok {
			t.Errorf("taskView.%s resolves as a field; the Task entity no longer carries it", name)
		}
	}

	// Methods. Only exported methods are reachable from a template, and only
	// exported methods are what reflect reports here, so the two sets coincide.
	pointerType := reflect.TypeOf(&taskView{})
	for i := range pointerType.NumMethod() {
		if name := pointerType.Method(i).Name; indexOfNeedle(name) >= 0 {
			t.Errorf("*taskView still exposes the method %q; a template could call it", name)
		}
	}

	// The control: the walk really does see the names it is meant to police. If
	// promotedFieldNames returned nothing, every assertion above would be vacuous.
	names := promotedFieldNames(viewType)
	if !containsName(names, "Title") || !containsName(names, "SubtaskCount") {
		t.Fatalf("the field walk did not reach the promoted models.Task fields; it saw %v", names)
	}
	if !hasMethod(pointerType, "HasMeta") {
		t.Fatalf("the method walk did not see HasMeta, so asserting an absence proves nothing")
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

// ==================== THE METADATA RENDER PREDICATE ====================

// TestSpecialistsRemoval_HasMetaHasExactlyFiveContributors pins the predicate
// itself, away from any page: with none of the five the footer is not rendered,
// and with any ONE of the five it is.
//
// The retired field was a sixth term of this disjunction, so removing it changed
// when the region renders. Both directions are asserted because either alone is
// satisfiable by a broken predicate: a `return false` passes the empty case and a
// `return true` passes all five populated ones (SPEC/WEB.md § Roadmap Tasks Page,
// absent metadata renders nothing; Acceptance Criterion 85).
func TestSpecialistsRemoval_HasMetaHasExactlyFiveContributors(t *testing.T) {
	sprint := &db.SprintRef{ID: 4, Title: "Checkout hardening"}

	for _, c := range []struct {
		name string
		view taskView
		want bool
	}{
		{"no contributor at all", taskView{}, false},
		{"the sprint alone", taskView{Sprint: sprint}, true},
		{"the subtask count alone", taskView{Task: models.Task{SubtaskCount: 1}}, true},
		{"the depends-on list alone", taskView{Task: models.Task{DependsOn: []int{7}}}, true},
		{"the blocks list alone", taskView{Task: models.Task{Blocks: []int{7}}}, true},
		{"the comment count alone", taskView{CommentCount: 1}, true},
	} {
		view := c.view
		if got := view.HasMeta(); got != c.want {
			t.Errorf("%s: HasMeta() = %t, want %t", c.name, got, c.want)
		}
	}

	// The zero values that must NOT count. An indicator whose value is zero or
	// empty renders nothing, so a predicate that tested for presence of the slice
	// rather than for its length would render an empty footer.
	for _, c := range []struct {
		name string
		view taskView
	}{
		{"a zero subtask count", taskView{Task: models.Task{SubtaskCount: 0}}},
		{"an empty depends-on list", taskView{Task: models.Task{DependsOn: []int{}}}},
		{"an empty blocks list", taskView{Task: models.Task{Blocks: []int{}}}},
		{"a zero comment count", taskView{CommentCount: 0}},
	} {
		view := c.view
		if view.HasMeta() {
			t.Errorf("%s: HasMeta() = true; a zero or empty value is not an indicator", c.name)
		}
	}
}

// singleIndicatorFixture names the six tasks seedSingleIndicatorFixture creates:
// one for each of the five metadata contributors, carrying that contributor and
// nothing else, plus one carrying none.
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

// seedSingleIndicatorFixture builds the board that isolates each contributor.
//
// The single dependency edge is what gives two of the tasks their one indicator:
// dependsOnly depends on blocksOnly, so the first shows a depends-on count and the
// second a blocks count, and neither shows anything else. The subtask child is a
// task of the roadmap but carries no indicator of its own, so it appears on the
// board as a second bare card and disturbs nothing.
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
			FunctionalRequirements: "The board must show this task exactly one metadata indicator.",
			TechnicalRequirements:  "Seeded read-only against the roadmap database.",
			AcceptanceCriteria:     "The card's metadata footer holds one indicator and no other.",
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

// allCardRoles is every data-role the metadata footer can emit, plus the role the
// removed indicator used to emit. Each rendered case below asserts exactly one of
// these present and every other absent, so a footer that gained an indicator fails
// as loudly as one that lost the right one.
var allCardRoles = []string{
	"task-card-sprint",
	"task-card-subtasks",
	"task-card-depends-on",
	"task-card-blocks",
	"task-card-comments",
	retiredCardRole,
}

// TestSpecialistsRemoval_MetadataFooterRendersPerContributor is the rendered half
// of the predicate gate, and the one Acceptance Criteria 85 and 91 are written
// against: it drives the real board through the real mux.
//
// A task carrying exactly one of the five contributors renders a footer holding
// that indicator and no other; the task carrying none renders no footer element
// at all. Together with TestSpecialistsRemoval_HasMetaHasExactlyFiveContributors,
// which pins the predicate away from the page, this covers both the case where
// other metadata is present and the case where none is.
func TestSpecialistsRemoval_MetadataFooterRendersPerContributor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSingleIndicatorFixture(t, "settlement-platform")
	mux := buildMux()

	body := servePage(t, mux, "/roadmaps/"+f.name+"/tasks")
	columns := boardColumns(t, body)

	for _, c := range []struct {
		name   string
		taskID int
		role   string
		text   string
	}{
		{"the sprint alone", f.sprintOnly, "task-card-sprint",
			"Settlement reconciliation (Sprint #" + itoa(f.sprintID) + ")"},
		{"the subtask count alone", f.subtasksOnly, "task-card-subtasks", "Subtasks: 1"},
		{"the depends-on count alone", f.dependsOnly, "task-card-depends-on", "Depends on: 1"},
		{"the blocks count alone", f.blocksOnly, "task-card-blocks", "Blocks: 1"},
		{"the comment count alone", f.commentsOnly, "task-card-comments", "Comments: 1"},
	} {
		card := cardAnywhere(t, columns, c.taskID)
		meta := metaFooter(t, card)
		if meta == "" {
			t.Errorf("%s: task #%d renders no metadata footer, but it has one indicator\ncard: %s",
				c.name, c.taskID, card)
			continue
		}
		if !strings.Contains(meta, c.text) {
			t.Errorf("%s: the footer does not show %q\nfooter: %s", c.name, c.text, meta)
		}
		for _, role := range allCardRoles {
			present := strings.Contains(meta, role)
			if role == c.role && !present {
				t.Errorf("%s: the footer does not carry data-role=%q\nfooter: %s",
					c.name, role, meta)
			}
			if role != c.role && present {
				t.Errorf("%s: the footer also carries data-role=%q, which this task has no "+
					"value for\nfooter: %s", c.name, role, meta)
			}
		}
		if strings.Contains(meta, retiredCardIcon) {
			t.Errorf("%s: the footer draws the retired specialists icon %q\nfooter: %s",
				c.name, retiredCardIcon, meta)
		}
	}

	// None of the five: no footer element, no placeholder, and no dash — and the
	// card is still a full card, so the absence is of indicators, not of the task.
	bare := cardAnywhere(t, columns, f.bare)
	if strings.Contains(bare, `data-role="task-card-meta"`) {
		t.Errorf("a task with none of the five indicators renders a metadata footer\ncard: %s", bare)
	}
	for _, role := range allCardRoles {
		if strings.Contains(bare, role) {
			t.Errorf("a task with no metadata renders data-role=%q\ncard: %s", role, bare)
		}
	}
	if !strings.Contains(bare, "Audit the settlement webhook signature verification") {
		t.Errorf("the metadata-free card lost its title\ncard: %s", bare)
	}
}

// cardAnywhere returns one task's card from whichever board column holds it. The
// fixture spreads its tasks over two columns — sprint membership moves one of them
// — so the column a card sits in is not what the assertions are about.
func cardAnywhere(t *testing.T, columns []string, taskID int) string {
	t.Helper()

	for _, column := range columns {
		if strings.Contains(column, cardMarker(taskID)) {
			return cardSlice(t, column, taskID)
		}
	}
	t.Fatalf("task #%d has no card in any of the %d board columns", taskID, len(columns))
	return ""
}

// ==================== WHAT FILLS THE MODAL ====================

// TestSpecialistsRemoval_TaskDetailJSONHasNoKeyForTheField decodes the endpoint
// the modal is filled from into a generic map and asserts no key of the task
// object names the retired field.
//
// Decoding into a map rather than into taskDetailView is the whole point: the
// typed decode would silently ignore a key the struct no longer has, which is
// exactly the failure this guards against. The modal script reads the object key
// by key, so a key that outlived the field would reach datagridItem — and a key
// the script no longer reads would be dead weight on every modal open
// (SPEC/WEB.md § Task Detail Endpoint; Acceptance Criterion 15).
func TestSpecialistsRemoval_TaskDetailJSONHasNoKeyForTheField(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSingleIndicatorFixture(t, "settlement-platform")
	mux := buildMux()

	status, body := fetchTaskDetail(t, mux, f.name, f.commentsOnly)
	if status != http.StatusOK {
		t.Fatalf("GET the detail of task #%d: status = %d, want 200; body=%q",
			f.commentsOnly, status, body)
	}

	var envelope struct {
		Task map[string]json.RawMessage `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decoding the task detail: %v; body=%q", err, body)
	}

	// The control: the object really is the task, so an empty map cannot make the
	// sweep below pass by having nothing to sweep.
	for _, required := range []string{"id", "title", "functional_requirements", "subtask_count"} {
		if _, ok := envelope.Task[required]; !ok {
			t.Fatalf("the decoded task object has no %q key, so it is not the task payload; "+
				"body=%q", required, body)
		}
	}

	for key := range envelope.Task {
		if indexOfNeedle(key) >= 0 {
			t.Errorf("the task detail JSON still carries the key %q; the Task entity no longer "+
				"has the field", key)
		}
	}
}
