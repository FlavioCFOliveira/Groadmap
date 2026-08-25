// Package commands — the gates for the in-sprint position DENSITY invariant
// (rmp task #304).
//
// SPEC/DATABASE.md § Position Density Within a Sprint states that the positions
// held by the member tasks of one sprint form a dense run from zero: a sprint
// with N members holds exactly 0, 1, ..., N-1. Uniqueness is the schema's job
// and the schema does it; density has no constraint form in SQLite — a CHECK
// sees one row, UNIQUE only says two values differ, and a FOR EACH ROW trigger
// would reject the correct multi-row renumberings — so the write paths carry it
// and these tests are where it is verified. That is the whole reason this file
// exists: "Density is therefore upheld by the write paths and proved by tests,
// and by nothing else."
//
// Three gates, each answering one requirement of that section's "What a test
// must show":
//
//  1. TestPositionDensity_EveryPublishedWritePathLeavesADenseRun runs one case
//     per write path and reads sprint_tasks back. The list of write paths is
//     PARSED OUT OF THE SPEC TABLE rather than transcribed here, so a path added
//     to the published table with no case fails this file, and a case naming a
//     path the table does not publish fails it too.
//  2. TestPositionDensity_RemovingFromTheMiddleLeavesNoGap attacks the four
//     paths the table marks "Leaves a gap" — and that set of four is likewise
//     derived from the table, not typed out. Each removes a member from the
//     MIDDLE of a sprint, because removing the LAST member proves nothing: a
//     dense run minus its last element is dense whether or not anything
//     compacted it.
//  3. TestMoveTaskToPosition_OverADenseRun covers the five moves the section
//     enumerates for `Move Task to Position`.
//
// The defect these gates close: four removals took a row out of a sprint's run
// and none of them compacted. Three of the four repair a sprint the caller's
// arguments never name, which is why they went unnoticed for so long — the
// damage lands somewhere the invocation does not mention. The measured instance
// was a sprint left holding 39 members at positions 0..36, 53 and 57 by the
// source side of `sprint move-tasks`.
package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ---------------------------------------------------------------------------
// Reading the published table of write paths
// ---------------------------------------------------------------------------

// densitySpecRelPath is the specification file that publishes the table, and
// densitySectionHeading is the heading the scan starts at. The heading is
// matched before anything else so that a table elsewhere in the same file
// cannot be mistaken for this one.
const (
	densitySpecRelPath    = "SPEC/DATABASE.md"
	densitySectionHeading = "#### Position Density Within a Sprint"
)

// Floors below which the parse is treated as evidence that the scan stopped
// matching, rather than as evidence about the specification. The table holds 13
// rows naming 12 distinct write paths today, four of which leave a gap; the
// floors sit under those numbers so a legitimate removal does not trip them and
// far enough above zero that a gate measuring nothing cannot report success.
const (
	minDensityRows     = 10
	minDensityPaths    = 8
	minGapLeavingPaths = 4
)

// densityCommandInCell matches a backticked token in the table's first cell and
// keeps only the ones that name a command: a lowercase family word followed by
// the rest of the invocation. The filter is what keeps `SPRINT`, `DOING`,
// `TESTING` and `BACKLOG` — status names the same cell also backticks — out of
// the command set, because those are uppercase.
var densityCommandInCell = regexp.MustCompile("`((?:sprint|task) [^`]+)`")

// densityWritePath is one write path as the published table describes it.
type densityWritePath struct {
	// command is the invocation the first cell names, e.g. "sprint add-tasks".
	command string
	// leavesGap is true when at least one of the path's rows says the operation
	// takes a row out of a sprint's run.
	leavesGap bool
	// touchesTable is false only for a path whose rows all say it does not
	// write sprint_tasks at all.
	touchesTable bool
}

// parseDensityWritePaths reads the published table and returns one entry per
// distinct command it names, together with the number of table rows parsed.
//
// A command may appear in more than one row — `sprint add-tasks` has one row
// for a task that belonged to no sprint and another for a task that did — so
// the flags are folded across the rows: a command leaves a gap if any of its
// rows says so, and touches the table unless every one of its rows says it does
// not.
func parseDensityWritePaths(t *testing.T) ([]densityWritePath, int) {
	t.Helper()

	content := readDensitySpec(t)
	start := strings.Index(content, densitySectionHeading)
	if start < 0 {
		t.Fatalf("%s no longer contains the heading %q, so this gate cannot find the table it verifies",
			densitySpecRelPath, densitySectionHeading)
	}

	rows := 0
	byCommand := make(map[string]*densityWritePath)
	order := make([]string, 0, 16)

	inTable := false
	for _, line := range strings.Split(content[start:], "\n") {
		trimmed := strings.TrimSpace(line)

		// A heading at the same level or above ends the section, so a table
		// further down the file is never read as this one.
		if trimmed != densitySectionHeading && strings.HasPrefix(trimmed, "#") {
			break
		}
		if !strings.HasPrefix(trimmed, "|") {
			inTable = false
			continue
		}

		cells := splitMarkdownRow(trimmed)
		if len(cells) < 3 {
			continue
		}
		// The header row opens the table; its separator and the header itself
		// are skipped.
		if cells[0] == "Write path" {
			inTable = true
			continue
		}
		if !inTable || strings.HasPrefix(cells[0], "---") {
			continue
		}

		rows++
		leavesGap := strings.Contains(cells[2], "Leaves a gap")
		touches := !strings.Contains(cells[1], "Does not touch")

		matches := densityCommandInCell.FindAllStringSubmatch(cells[0], -1)
		if len(matches) == 0 {
			t.Errorf("table row %q names no command in its first cell; the parse would silently "+
				"drop the write path it describes", trimmed)
			continue
		}
		for _, m := range matches {
			command := m[1]
			entry, ok := byCommand[command]
			if !ok {
				entry = &densityWritePath{command: command}
				byCommand[command] = entry
				order = append(order, command)
			}
			entry.leavesGap = entry.leavesGap || leavesGap
			entry.touchesTable = entry.touchesTable || touches
		}
	}

	paths := make([]densityWritePath, 0, len(order))
	for _, command := range order {
		paths = append(paths, *byCommand[command])
	}
	return paths, rows
}

// splitMarkdownRow splits a pipe-delimited table row into its cells, dropping
// the empty leading and trailing fields the outer pipes produce.
func splitMarkdownRow(row string) []string {
	parts := strings.Split(strings.Trim(row, "|"), "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

// readDensitySpec returns the specification file's content, failing loudly if
// it cannot be read: an unreadable file must never be mistaken for an empty one.
func readDensitySpec(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s, so this gate is not looking where it assumes: %v", root, err)
	}
	path := filepath.Join(root, filepath.FromSlash(densitySpecRelPath))
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading %s: %v", densitySpecRelPath, err)
	}
	return string(content)
}

// TestPositionDensity_SpecTableParses is the floor under the two gates that
// read the table. Both of them assert set equality against what the parse
// returns, and set equality against an empty set is satisfied by an empty set:
// a scan that stopped matching would turn them into gates that verify nothing
// while still reporting success.
func TestPositionDensity_SpecTableParses(t *testing.T) {
	paths, rows := parseDensityWritePaths(t)

	if rows < minDensityRows {
		t.Errorf("parsed %d rows out of %s § Position Density Within a Sprint, want at least %d: "+
			"the scan has stopped matching the published table",
			rows, densitySpecRelPath, minDensityRows)
	}
	if len(paths) < minDensityPaths {
		t.Errorf("parsed %d distinct write paths, want at least %d", len(paths), minDensityPaths)
	}

	gaps := 0
	touching := 0
	for _, p := range paths {
		if p.leavesGap {
			gaps++
		}
		if p.touchesTable {
			touching++
		}
	}
	if gaps < minGapLeavingPaths {
		t.Errorf("parsed %d write paths marked \"Leaves a gap\", want at least %d: the middle-removal "+
			"gate derives its cases from this set and would run none",
			gaps, minGapLeavingPaths)
	}
	if touching == len(paths) {
		t.Errorf("every parsed write path is marked as touching sprint_tasks; the published table " +
			"holds one that does not (`task stat <ids> BACKLOG`), so the \"Does not touch\" branch " +
			"of the parse is never taken and its assertion is vacuous")
	}
}

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// densityFixture is a roadmap holding two sprints, each populated through the
// real commands, so every state these gates meet is a state the CLI produces.
//
// Two sprints rather than one, because half the write paths under test move a
// row from one sprint to another and the gap they leave lands in the sprint the
// invocation does NOT name. A single-sprint fixture could not observe that.
type densityFixture struct {
	database *db.DB
	roadmap  string
	planned  int // PENDING sprint
	running  int // OPEN sprint, so tasks can be walked to DOING and beyond
	seq      int
}

// Commit hashes the walks below use. Real short hashes from this repository's
// history, so the fixture reads like a roadmap someone worked through.
const (
	densityCommitOpen  = "813c190"
	densityCommitClose = "902f022"
)

// densityMembers is how many tasks each fixture sprint starts with. Five is the
// smallest count that lets a test remove a member with at least two members on
// each side of it, which is what makes "the middle" mean something.
const densityMembers = 5

// setupDensityRoadmap builds the fixture and returns it with both sprints
// already holding densityMembers tasks.
func setupDensityRoadmap(t *testing.T, name string) *densityFixture {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	database, cleanup := setupTestTaskRoadmap(t, name)
	t.Cleanup(cleanup)

	f := &densityFixture{roadmap: name, database: database}
	f.running = f.newSprint(t, "Settlement reconciliation")
	f.planned = f.newSprint(t, "Ledger close hardening")

	// Exactly one sprint may be OPEN at a time, so the first is started and the
	// second stays PENDING. Both accept task membership; only CLOSED refuses.
	run(t, func() error { return sprintStart([]string{"-r", name, itoa(f.running)}) })

	f.fill(t, f.running, densityMembers)
	f.fill(t, f.planned, densityMembers)

	f.assertDense(t, "fixture")
	return f
}

// newSprint creates one sprint through the real command and returns the id the
// command itself reported. The id is read out of the command's stdout rather
// than out of the table, so a sprint this fixture believes it created is a
// sprint the CLI said it created.
func (f *densityFixture) newSprint(t *testing.T, title string) int {
	t.Helper()

	out := captureStdout(t, func() {
		if err := sprintCreate([]string{
			"-r", f.roadmap, "-t", title,
			"-d", "Reconcile every acquirer batch to the cent before the ledger closes.",
		}); err != nil {
			t.Fatalf("creating sprint %q: %v", title, err)
		}
	})
	return decodeCreatedID(t, "sprint create", out)
}

// decodeCreatedID reads the {"id": N} object SPEC/COMMANDS.md promises out of a
// creation command's stdout.
func decodeCreatedID(t *testing.T, label, out string) int {
	t.Helper()

	var created struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("%s printed %q, which is not the id object SPEC/COMMANDS.md promises: %v",
			label, out, err)
	}
	if created.ID <= 0 {
		t.Fatalf("%s reported id %d", label, created.ID)
	}
	return created.ID
}

// densityTaskTitles are the task titles the fixture cycles through, so a
// failure message names a task that reads like real roadmap work.
var densityTaskTitles = []string{
	"Replay the acquirer batch against the ledger",
	"Reject a settlement whose totals disagree",
	"Record the reconciliation inside the write transaction",
	"Alert when a batch arrives after the cut-off",
	"Retire the nightly manual reconciliation sheet",
}

// newTask creates one BACKLOG task and returns the id the command reported.
func (f *densityFixture) newTask(t *testing.T) int {
	t.Helper()

	f.seq++
	title := densityTaskTitles[f.seq%len(densityTaskTitles)]

	out := captureStdout(t, func() {
		if err := taskCreate([]string{
			"-r", f.roadmap,
			"-t", title + " (" + itoa(f.seq) + ")",
			"-fr", "Every settlement batch reconciles to the cent before the ledger closes.",
			"-tr", "Replay the batch against the acquirer file and report the first divergence.",
			"-ac", "A deliberately corrupted batch is reported, not silently accepted.",
		}); err != nil {
			t.Fatalf("creating a task: %v", err)
		}
	})
	return decodeCreatedID(t, "task create", out)
}

// fill adds n freshly created tasks to a sprint, in order.
func (f *densityFixture) fill(t *testing.T, sprintID, n int) []int {
	t.Helper()

	ids := make([]int, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, f.newTask(t))
	}
	f.addToSprint(t, sprintID, ids...)
	if got := f.order(t, sprintID); !sameIDs(got, ids) {
		t.Fatalf("sprint %d holds %v after being filled, want %v in that order", sprintID, got, ids)
	}
	return ids
}

// addToSprint runs `sprint add-tasks`.
func (f *densityFixture) addToSprint(t *testing.T, sprintID int, taskIDs ...int) {
	t.Helper()
	run(t, func() error {
		return sprintAddTasks([]string{"-r", f.roadmap, itoa(sprintID), joinIDs(taskIDs)})
	})
}

// order returns one sprint's task ids in stored position order.
func (f *densityFixture) order(t *testing.T, sprintID int) []int {
	t.Helper()

	rows, err := f.database.Query(
		"SELECT task_id FROM sprint_tasks WHERE sprint_id = ? ORDER BY position ASC, task_id ASC",
		sprintID,
	)
	if err != nil {
		t.Fatalf("reading the order of sprint %d: %v", sprintID, err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scanning a member of sprint %d: %v", sprintID, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the members of sprint %d: %v", sprintID, err)
	}
	return ids
}

// membership is one sprint_tasks row, read straight out of the table because
// the row is the fact these gates are about.
type membership struct {
	sprintID int
	taskID   int
	position int
}

// snapshot returns every sprint_tasks row in the roadmap, sorted, so two
// snapshots can be compared for equality.
func (f *densityFixture) snapshot(t *testing.T) []membership {
	t.Helper()

	rows, err := f.database.Query("SELECT sprint_id, task_id, position FROM sprint_tasks")
	if err != nil {
		t.Fatalf("reading sprint_tasks: %v", err)
	}
	defer rows.Close() //nolint:errcheck // test cleanup

	var out []membership
	for rows.Next() {
		var m membership
		if err := rows.Scan(&m.sprintID, &m.taskID, &m.position); err != nil {
			t.Fatalf("scanning a sprint_tasks row: %v", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating sprint_tasks: %v", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].sprintID != out[j].sprintID {
			return out[i].sprintID < out[j].sprintID
		}
		return out[i].taskID < out[j].taskID
	})
	return out
}

// assertDense is the invariant itself, checked over EVERY sprint in the
// roadmap rather than over the one an operation named. Three of the four
// gap-opening paths damage a sprint the invocation does not mention, so a check
// scoped to the named sprint would look right and see nothing.
func (f *densityFixture) assertDense(t *testing.T, label string) {
	t.Helper()

	held := make(map[int]map[int]int)
	for _, m := range f.snapshot(t) {
		if held[m.sprintID] == nil {
			held[m.sprintID] = make(map[int]int)
		}
		held[m.sprintID][m.taskID] = m.position
	}

	for sprintID, positions := range held {
		seen := make(map[int]int, len(positions))
		for taskID, position := range positions {
			if other, dup := seen[position]; dup {
				t.Errorf("%s: sprint %d holds position %d twice (tasks %d and %d); "+
					"SPEC/DATABASE.md § Position Uniqueness Within a Sprint forbids it",
					label, sprintID, position, other, taskID)
			}
			seen[position] = taskID
		}
		for want := 0; want < len(positions); want++ {
			if _, ok := seen[want]; !ok {
				t.Errorf("%s: sprint %d holds %d members but nothing at position %d — the run is %v, "+
					"and SPEC/DATABASE.md § Position Density Within a Sprint requires exactly 0..%d",
					label, sprintID, len(positions), want, sortedPositions(positions), len(positions)-1)
				break
			}
		}
	}
}

// sortedPositions renders a sprint's held positions for a failure message.
func sortedPositions(positions map[int]int) []int {
	out := make([]int, 0, len(positions))
	for _, p := range positions {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// joinIDs renders ids as the comma-separated list the CLI parses.
func joinIDs(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = itoa(id)
	}
	return strings.Join(parts, ",")
}

// roadmapSlug turns a write-path phrase into a roadmap name the CLI accepts:
// lowercase letters, digits, underscores and hyphens only. `task stat <ids>
// BACKLOG` is the phrase that forces it.
func roadmapSlug(command string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(command) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// sameIDs reports whether two id sequences are identical, order included.
func sameIDs(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sameMemberships reports whether two snapshots hold the same rows with the
// same positions.
func sameMemberships(a, b []membership) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// walkToDoing takes a SPRINT member of the running sprint to DOING.
func (f *densityFixture) walkToDoing(t *testing.T, taskID int) {
	t.Helper()
	run(t, func() error {
		return taskSetStatus([]string{"-r", f.roadmap, itoa(taskID), string(models.StatusDoing),
			"--commit-open", densityCommitOpen})
	})
}

// walkToCompleted takes a SPRINT member of the running sprint all the way to
// COMPLETED.
func (f *densityFixture) walkToCompleted(t *testing.T, taskID int) {
	t.Helper()
	f.walkToDoing(t, taskID)
	run(t, func() error {
		return taskSetStatus([]string{"-r", f.roadmap, itoa(taskID), string(models.StatusTesting)})
	})
	run(t, func() error {
		return taskSetStatus([]string{"-r", f.roadmap, itoa(taskID), string(models.StatusCompleted),
			"--commit-close", densityCommitClose})
	})
}

// ---------------------------------------------------------------------------
// Gate 1: every published write path leaves a dense run
// ---------------------------------------------------------------------------

// densityCase is one write path exercised end to end.
type densityCase struct {
	// exercise performs a representative invocation of the path. It returns the
	// sprint whose run the caller should read back; a case that damages another
	// sprint relies on assertDense sweeping every sprint anyway.
	exercise func(t *testing.T, f *densityFixture)
	// command is the phrase the published table uses for this path, matched
	// against the parse.
	command string
}

// densityCases holds one entry per write path the published table names. The
// keys are checked against the parse in both directions, so this list cannot
// drift from the specification without a failure.
func densityCases() []densityCase {
	return []densityCase{
		{
			command: "sprint add-tasks",
			exercise: func(t *testing.T, f *densityFixture) {
				// Both forms the table distinguishes: a task that belonged to no
				// sprint (appends) and a task that already belonged to another
				// one (re-parents, leaving a gap behind in the planned sprint).
				f.addToSprint(t, f.running, f.newTask(t))

				middle := f.order(t, f.planned)[2]
				f.addToSprint(t, f.running, middle)
			},
		},
		{
			command: "sprint move-tasks",
			exercise: func(t *testing.T, f *densityFixture) {
				middle := f.order(t, f.running)[2]
				run(t, func() error {
					return sprintMoveTasks([]string{"-r", f.roadmap,
						itoa(f.running), itoa(f.planned), itoa(middle)})
				})
			},
		},
		{
			command: "sprint reorder",
			exercise: func(t *testing.T, f *densityFixture) {
				members := f.order(t, f.running)
				reversed := make([]int, len(members))
				for i, id := range members {
					reversed[len(members)-1-i] = id
				}
				run(t, func() error {
					return sprintReorder([]string{"-r", f.roadmap, itoa(f.running), joinIDs(reversed)})
				})
			},
		},
		{
			command: "sprint move-to",
			exercise: func(t *testing.T, f *densityFixture) {
				first := f.order(t, f.running)[0]
				run(t, func() error {
					return sprintMoveTo([]string{"-r", f.roadmap, itoa(f.running), itoa(first), "3"})
				})
			},
		},
		{
			command: "sprint top",
			exercise: func(t *testing.T, f *densityFixture) {
				members := f.order(t, f.running)
				run(t, func() error {
					return sprintTop([]string{"-r", f.roadmap, itoa(f.running), itoa(members[len(members)-1])})
				})
			},
		},
		{
			command: "sprint bottom",
			exercise: func(t *testing.T, f *densityFixture) {
				first := f.order(t, f.running)[0]
				run(t, func() error {
					return sprintBottom([]string{"-r", f.roadmap, itoa(f.running), itoa(first)})
				})
			},
		},
		{
			command: "sprint swap",
			exercise: func(t *testing.T, f *densityFixture) {
				members := f.order(t, f.running)
				run(t, func() error {
					return sprintSwap([]string{"-r", f.roadmap, itoa(f.running),
						itoa(members[0]), itoa(members[len(members)-1])})
				})
			},
		},
		{
			command: "sprint remove-tasks",
			exercise: func(t *testing.T, f *densityFixture) {
				middle := f.order(t, f.running)[2]
				run(t, func() error {
					return sprintRemoveTasks([]string{"-r", f.roadmap, itoa(f.running), itoa(middle)})
				})
			},
		},
		{
			command: "sprint remove",
			exercise: func(t *testing.T, f *densityFixture) {
				run(t, func() error {
					return sprintRemove([]string{"-r", f.roadmap, itoa(f.planned)})
				})
			},
		},
		{
			command: "task reopen",
			exercise: func(t *testing.T, f *densityFixture) {
				// Both forms the table distinguishes: from a sprint-associated
				// state, which deletes the membership row and leaves a gap, and
				// from COMPLETED, which keeps it.
				members := f.order(t, f.running)
				f.walkToDoing(t, members[2])
				run(t, func() error {
					return taskReopen([]string{"-r", f.roadmap, itoa(members[2])})
				})

				survivors := f.order(t, f.running)
				f.walkToCompleted(t, survivors[1])
				run(t, func() error {
					return taskReopen([]string{"-r", f.roadmap, itoa(survivors[1])})
				})
			},
		},
		{
			command: "task remove",
			exercise: func(t *testing.T, f *densityFixture) {
				// `task remove` refuses anything but BACKLOG, and a BACKLOG task
				// can still be a sprint member: `task stat <ids> BACKLOG` is the
				// route into exactly that state.
				middle := f.order(t, f.running)[2]
				run(t, func() error {
					return taskSetStatus([]string{"-r", f.roadmap, itoa(middle), string(models.StatusBacklog)})
				})
				run(t, func() error {
					return taskRemove([]string{"-r", f.roadmap, itoa(middle)})
				})
			},
		},
		{
			command: "task stat <ids> BACKLOG",
			exercise: func(t *testing.T, f *densityFixture) {
				middle := f.order(t, f.running)[2]
				run(t, func() error {
					return taskSetStatus([]string{"-r", f.roadmap, itoa(middle), string(models.StatusBacklog)})
				})
			},
		},
	}
}

// TestPositionDensity_EveryPublishedWritePathLeavesADenseRun is the first
// requirement of SPEC/DATABASE.md § Position Density Within a Sprint, "What a
// test must show": for every entry of the published table, the sprint's
// positions read back as a dense 0..N-1 run once the operation's transaction
// has committed.
//
// The set of entries is compared against the parse in BOTH directions. A write
// path added to the table with no case here fails; a case naming a path the
// table does not publish fails too. That is what keeps the specification's claim
// — "the whole weight of the guarantee rests on the enumeration below" — backed
// by something that can break.
func TestPositionDensity_EveryPublishedWritePathLeavesADenseRun(t *testing.T) {
	published, _ := parseDensityWritePaths(t)
	cases := densityCases()

	publishedByCommand := make(map[string]densityWritePath, len(published))
	for _, p := range published {
		publishedByCommand[p.command] = p
	}
	covered := make(map[string]bool, len(cases))
	for _, c := range cases {
		covered[c.command] = true
		if _, ok := publishedByCommand[c.command]; !ok {
			t.Errorf("this file exercises %q, which %s § Position Density Within a Sprint does not "+
				"publish as a write path", c.command, densitySpecRelPath)
		}
	}
	for _, p := range published {
		if !covered[p.command] {
			t.Errorf("%s § Position Density Within a Sprint publishes the write path %q and this file "+
				"exercises no case for it; a path is not finished until its effect on the run is proved",
				densitySpecRelPath, p.command)
		}
	}

	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.command, " ", "_"), func(t *testing.T) {
			f := setupDensityRoadmap(t, "density-"+roadmapSlug(tc.command))

			before := f.snapshot(t)
			tc.exercise(t, f)
			after := f.snapshot(t)

			f.assertDense(t, tc.command)

			// The anti-vacuity floor, and it is derived from the table rather
			// than asserted by hand: the published second cell says of exactly
			// one path that it "does not touch the sprint_tasks table", so that
			// path must leave the rows byte-identical and every other path must
			// change them. A case that silently exercised nothing would fail
			// here instead of passing on an untouched fixture.
			changed := !sameMemberships(before, after)
			if want := publishedByCommand[tc.command].touchesTable; changed != want {
				verb := "leave sprint_tasks unchanged"
				if want {
					verb = "write sprint_tasks"
				}
				t.Errorf("%s § Position Density Within a Sprint says %q %s, but the invocation %s it.\n"+
					"  before: %v\n  after:  %v",
					densitySpecRelPath, tc.command, verb, map[bool]string{true: "changed", false: "left unchanged"}[changed],
					before, after)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Gate 2: removing from the MIDDLE of a sprint
// ---------------------------------------------------------------------------

// middleRemoval is one of the four paths that take a row out of a sprint's run.
// Each removes the member at index 2 of a five-member sprint, so two members
// sit on each side of the hole it opens.
type middleRemoval struct {
	// remove takes the sprint the fixture should damage and the task to take
	// out of it, and performs the removal through the real command.
	remove func(t *testing.T, f *densityFixture, sprintID, taskID int)
	// command is the phrase the published table uses for this path.
	command string
	// damages names which fixture sprint the removal is aimed at, so the
	// assertion reads the run the operation actually thinned.
	damagesPlanned bool
}

// middleRemovals holds one entry per path the published table marks "Leaves a
// gap". The set is checked against the parse, so the four cannot quietly become
// three or five.
func middleRemovals() []middleRemoval {
	return []middleRemoval{
		{
			command:        "sprint add-tasks",
			damagesPlanned: true,
			remove: func(t *testing.T, f *densityFixture, _, taskID int) {
				// The re-parenting form: adding a member of the planned sprint
				// to the running one takes its row out of the planned sprint's
				// run — a sprint this invocation never names.
				f.addToSprint(t, f.running, taskID)
			},
		},
		{
			command: "sprint move-tasks",
			remove: func(t *testing.T, f *densityFixture, sprintID, taskID int) {
				run(t, func() error {
					return sprintMoveTasks([]string{"-r", f.roadmap,
						itoa(sprintID), itoa(f.planned), itoa(taskID)})
				})
			},
		},
		{
			command: "task reopen",
			remove: func(t *testing.T, f *densityFixture, _, taskID int) {
				f.walkToDoing(t, taskID)
				run(t, func() error {
					return taskReopen([]string{"-r", f.roadmap, itoa(taskID)})
				})
			},
		},
		{
			command: "task remove",
			remove: func(t *testing.T, f *densityFixture, _, taskID int) {
				run(t, func() error {
					return taskSetStatus([]string{"-r", f.roadmap, itoa(taskID), string(models.StatusBacklog)})
				})
				run(t, func() error {
					return taskRemove([]string{"-r", f.roadmap, itoa(taskID)})
				})
			},
		},
	}
}

// TestPositionDensity_RemovingFromTheMiddleLeavesNoGap is the second
// requirement of SPEC/DATABASE.md § Position Density Within a Sprint, "What a
// test must show": the removal of a task from the MIDDLE of a sprint leaves a
// dense run, not only the removal of the last one.
//
// Removing the last member proves nothing, and that is not a stylistic
// preference: a dense run minus its last element is dense whether or not
// anything compacted it, so a test built that way passes against the broken
// code it was written to catch. Every case here takes out the member at index 2
// of five, and asserts the SURVIVING ORDER as well as the density — because a
// compaction that reordered the sprint would also produce a dense run, and would
// be a different and worse defect.
func TestPositionDensity_RemovingFromTheMiddleLeavesNoGap(t *testing.T) {
	published, _ := parseDensityWritePaths(t)

	publishedGaps := make(map[string]bool)
	for _, p := range published {
		if p.leavesGap {
			publishedGaps[p.command] = true
		}
	}
	covered := make(map[string]bool)
	for _, r := range middleRemovals() {
		covered[r.command] = true
		if !publishedGaps[r.command] {
			t.Errorf("this gate treats %q as a path that leaves a gap, but %s § Position Density "+
				"Within a Sprint does not mark it so", r.command, densitySpecRelPath)
		}
	}
	for command := range publishedGaps {
		if !covered[command] {
			t.Errorf("%s § Position Density Within a Sprint marks %q as leaving a gap and this gate "+
				"never removes anything through it", densitySpecRelPath, command)
		}
	}

	for _, tc := range middleRemovals() {
		t.Run(strings.ReplaceAll(tc.command, " ", "_"), func(t *testing.T) {
			f := setupDensityRoadmap(t, "middle-"+roadmapSlug(tc.command))

			sprintID := f.running
			if tc.damagesPlanned {
				sprintID = f.planned
			}

			members := f.order(t, sprintID)
			if len(members) != densityMembers {
				t.Fatalf("sprint %d holds %d members, want %d", sprintID, len(members), densityMembers)
			}

			const middleIndex = 2
			victim := members[middleIndex]

			// The floor that makes the case mean what it claims: the member
			// being removed must sit strictly inside the run. A case that
			// silently attacked the last element would pass while proving
			// nothing.
			if middleIndex == 0 || middleIndex == len(members)-1 {
				t.Fatalf("index %d is an end of a %d-member run, not its middle", middleIndex, len(members))
			}
			var storedPosition int
			if err := f.database.QueryRow(
				"SELECT position FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?",
				sprintID, victim,
			).Scan(&storedPosition); err != nil {
				t.Fatalf("reading the position of task %d in sprint %d: %v", victim, sprintID, err)
			}
			if storedPosition != middleIndex {
				t.Fatalf("task %d is the %d-th member of sprint %d but stores position %d; the fixture "+
					"is not dense, so this case cannot prove anything about a removal",
					victim, middleIndex, sprintID, storedPosition)
			}

			want := make([]int, 0, len(members)-1)
			want = append(want, members[:middleIndex]...)
			want = append(want, members[middleIndex+1:]...)

			tc.remove(t, f, sprintID, victim)

			survivors := f.order(t, sprintID)
			if !sameIDs(survivors, want) {
				t.Errorf("after %q removed the middle member of sprint %d, the order is %v, want %v: "+
					"a compaction changes VALUES and never the SEQUENCE",
					tc.command, sprintID, survivors, want)
			}

			positions := make([]int, 0, len(survivors))
			for _, id := range survivors {
				var p int
				if err := f.database.QueryRow(
					"SELECT position FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?",
					sprintID, id,
				).Scan(&p); err != nil {
					t.Fatalf("reading the position of task %d: %v", id, err)
				}
				positions = append(positions, p)
			}
			for i, p := range positions {
				if p != i {
					t.Errorf("after %q removed the middle member of sprint %d, the survivors hold %v, "+
						"want 0..%d: the gap at position %d was never closed",
						tc.command, sprintID, positions, len(positions)-1, middleIndex)
					break
				}
			}

			f.assertDense(t, tc.command+" (middle removal)")
		})
	}
}

// ---------------------------------------------------------------------------
// Gate 3: Move Task to Position over a dense run
// ---------------------------------------------------------------------------

// TestMoveTaskToPosition_OverADenseRun is the third requirement of
// SPEC/DATABASE.md § Position Density Within a Sprint, "What a test must show":
// `Move Task to Position` reaches the correct order over a run that starts at
// zero and has no gap, for a move up, a move down, a move to the first slot, a
// move to the last slot, and a target beyond the member count.
//
// The five are run in sequence against one sprint, each asserting the exact
// resulting order, because the interesting failure is not that one move is
// wrong on its own but that a move leaves the run in a shape the next one
// misreads.
func TestMoveTaskToPosition_OverADenseRun(t *testing.T) {
	f := setupDensityRoadmap(t, "density-move-to-position")

	members := f.order(t, f.running)
	if len(members) != densityMembers {
		t.Fatalf("the fixture sprint holds %d members, want %d", len(members), densityMembers)
	}
	a, b, c, d, e := members[0], members[1], members[2], members[3], members[4]

	moves := []struct {
		name     string
		task     int
		want     []int
		position int
	}{
		// Move DOWN the list: the first member takes index 3, and the three it
		// passes each shift up one.
		{name: "down", task: a, position: 3, want: []int{b, c, d, a, e}},
		// Move UP the list: the last member takes index 1.
		{name: "up", task: e, position: 1, want: []int{b, e, c, d, a}},
		// The first slot.
		{name: "first_slot", task: d, position: 0, want: []int{d, b, e, c, a}},
		// The last slot, named exactly.
		{name: "last_slot", task: b, position: 4, want: []int{d, e, c, a, b}},
		// A target beyond the member count, which SPEC/COMMANDS.md § Move Task
		// to Position says moves the task to the end.
		{name: "beyond_the_count", task: e, position: 97, want: []int{d, c, a, b, e}},
	}

	for _, mv := range moves {
		t.Run(mv.name, func(t *testing.T) {
			run(t, func() error {
				return sprintMoveTo([]string{"-r", f.roadmap, itoa(f.running),
					itoa(mv.task), itoa(mv.position)})
			})

			if got := f.order(t, f.running); !sameIDs(got, mv.want) {
				t.Errorf("moving task %d to index %d over a dense run gives %v, want %v",
					mv.task, mv.position, got, mv.want)
			}
			f.assertDense(t, "sprint move-to "+mv.name)
		})
	}
}
