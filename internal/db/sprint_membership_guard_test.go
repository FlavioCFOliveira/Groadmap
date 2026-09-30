package db

import (
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The gates of the sprint membership guard, rmp task 575
// (SPEC/DATABASE.md § Sprint Membership Invariant Enforcement):
//
//  1. The guard checks both halves of the invariant, for the tasks it is given
//     and no others, reports a violation as utils.ErrDatabase with the
//     published line, and reads with one statement per chunk of ids.
//  2. A violation fails the transaction, so no row of the write is committed.
//  3. Every write path the SPEC enumerates calls it, and nothing else does.
//  4. No trigger exists anywhere: not in the schema, not in a migration, not in
//     a fresh database.

// guardFixture is a scratch roadmap with one sprint and three tasks, written
// directly so that the guard can be shown states no command produces.
type guardFixture struct {
	database *DB
	counter  *stmtCounter
	sprintID int
	tasks    []int
}

func setupGuardFixture(t *testing.T) *guardFixture {
	t.Helper()
	database, counter, cleanup := setupCountingDB(t)
	t.Cleanup(cleanup)
	f := &guardFixture{database: database, counter: counter}
	f.sprintID = newTestSprintWithCap(t, database, "Settlement hardening", 0)
	for _, title := range []string{"Reconcile acquirer files", "Replay failed payouts", "Alert on ledger drift"} {
		f.tasks = append(f.tasks, newTestTask(t, database, title))
	}
	return f
}

// guard runs the guard over ids inside a transaction that is then rolled back.
func (f *guardFixture) guard(t *testing.T, ids []int) error {
	t.Helper()
	tx, err := f.database.Begin()
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	return CheckSprintMembershipTx(tx, ids)
}

func (f *guardFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.database.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// TestSprintMembershipGuard_ChecksBothHalves drives the guard over a valid
// state and over one violation of each half, and requires the published line.
func TestSprintMembershipGuard_ChecksBothHalves(t *testing.T) {
	f := setupGuardFixture(t)
	backlog, member, active := f.tasks[0], f.tasks[1], f.tasks[2]
	f.exec(t, `INSERT INTO sprint_tasks (sprint_id, task_id, added_at, position) VALUES (?, ?, ?, 0)`,
		f.sprintID, member, "2026-09-01T08:00:00.000Z")
	f.exec(t, `UPDATE tasks SET status = 'DOING' WHERE id = ?`, member)

	// A BACKLOG task outside every sprint and a DOING member: valid.
	if err := f.guard(t, []int{backlog, member}); err != nil {
		t.Fatalf("the guard refused a valid state: %v", err)
	}
	// COMPLETED is left open by both halves, member or not.
	f.exec(t, `UPDATE tasks SET status = 'COMPLETED' WHERE id = ?`, active)
	if err := f.guard(t, []int{active}); err != nil {
		t.Errorf("the guard refused a COMPLETED task outside every sprint: %v", err)
	}

	// Half 1: a member in BACKLOG.
	f.exec(t, `UPDATE tasks SET status = 'BACKLOG' WHERE id = ?`, member)
	err := f.guard(t, []int{backlog, member})
	want := fmt.Sprintf("database error: sprint membership invariant violated: task %d would be BACKLOG in "+
		"sprint #%d; a sprint member is never in BACKLOG", member, f.sprintID)
	if !errors.Is(err, utils.ErrDatabase) || err == nil || err.Error() != want {
		t.Errorf("a BACKLOG member: guard = %v, want %q (utils.ErrDatabase, exit code 1)", err, want)
	}
	// The same state is invisible to a guard not given that task.
	if err := f.guard(t, []int{backlog}); err != nil {
		t.Errorf("the guard judged a task it was not given: %v", err)
	}

	// Half 2: an active task outside every sprint.
	f.exec(t, `UPDATE tasks SET status = 'SPRINT' WHERE id = ?`, member)
	f.exec(t, `UPDATE tasks SET status = 'TESTING' WHERE id = ?`, active)
	err = f.guard(t, []int{active, member})
	want = fmt.Sprintf("database error: sprint membership invariant violated: task %d would be TESTING and "+
		"belong to no sprint; a task in SPRINT, DOING or TESTING belongs to a sprint", active)
	if !errors.Is(err, utils.ErrDatabase) || err == nil || err.Error() != want {
		t.Errorf("an active non-member: guard = %v, want %q", err, want)
	}

	// No id, no statement and no verdict; an id naming no task is judged as nothing.
	f.counter.reset()
	if err := f.guard(t, nil); err != nil || f.counter.executed(sprintMembershipGuardQuery(generatePlaceholders(1))) != 0 {
		t.Errorf("an empty id set: guard = %v after %d reads, want nil and none", err,
			f.counter.executed(sprintMembershipGuardQuery(generatePlaceholders(1))))
	}
	if err := f.guard(t, []int{999999}); err != nil {
		t.Errorf("an id that names no task: guard = %v, want nil", err)
	}
}

// TestSprintMembershipGuard_OneStatementPerChunk pins the set-based read: 250
// ids are judged with three statements, two full chunks and one partial.
func TestSprintMembershipGuard_OneStatementPerChunk(t *testing.T) {
	f := setupGuardFixture(t)
	ids := slices.Clone(f.tasks)
	for i := len(ids); i < 250; i++ {
		ids = append(ids, newTestTask(t, f.database, fmt.Sprintf("Reconcile settlement window %d", i+1)))
	}

	f.counter.reset()
	if err := f.guard(t, ids); err != nil {
		t.Fatalf("guard over 250 valid tasks: %v", err)
	}
	full := f.counter.executed(sprintMembershipGuardQuery(generatePlaceholders(100)))
	partial := f.counter.executed(sprintMembershipGuardQuery(generatePlaceholders(50)))
	if full != 2 || partial != 1 {
		t.Errorf("the guard issued %d full-chunk and %d partial-chunk reads for 250 ids, want 2 and 1", full, partial)
	}

	// A violation in the last chunk is still found.
	f.exec(t, `UPDATE tasks SET status = 'DOING' WHERE id = ?`, ids[len(ids)-1])
	if err := f.guard(t, ids); !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("a violation in the last chunk: guard = %v, want utils.ErrDatabase", err)
	}
}

// TestSprintMembershipGuard_AViolationCommitsNothing pins the transactional half:
// a write whose result breaks the invariant is rolled back whole, its audit
// entry included, when the guard runs inside its transaction.
func TestSprintMembershipGuard_AViolationCommitsNothing(t *testing.T) {
	f := setupGuardFixture(t)
	id := f.tasks[0]

	err := f.database.WithTransaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE tasks SET status = 'DOING' WHERE id = ?`, id); err != nil {
			return err
		}
		if err := LogAuditTx(tx, models.OpTaskStatusDoing, models.EntityTask, id, "2026-09-02T10:00:00.000Z",
			WithCommitHash("5f93b51")); err != nil {
			return err
		}
		return CheckSprintMembershipTx(tx, []int{id})
	})
	if !errors.Is(err, utils.ErrDatabase) {
		t.Fatalf("the violating write returned %v, want utils.ErrDatabase", err)
	}
	var status string
	var audits int
	if err := f.database.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&status); err != nil {
		t.Fatalf("reading the task: %v", err)
	}
	if err := f.database.QueryRow(`SELECT COUNT(*) FROM audit WHERE entity_id = ? AND operation = ?`,
		id, string(models.OpTaskStatusDoing)).Scan(&audits); err != nil {
		t.Fatalf("counting the audit rows: %v", err)
	}
	if status != string(models.StatusBacklog) || audits != 0 {
		t.Errorf("after the refused write the task reads %s with %d audit entries, want BACKLOG and none", status, audits)
	}
}

// guardWritePaths maps each write path of the table in SPEC/DATABASE.md
// § Sprint Membership Invariant Enforcement to the function that performs its
// transaction, keyed "<package dir>:<function>" with methods qualified by their
// receiver type.
var guardWritePaths = map[string]string{
	"task create":                    "internal/commands:taskCreate",
	"task stat":                      "internal/commands:taskSetStatus",
	"task reopen":                    "internal/commands:taskReopen",
	"sprint add-tasks":               "internal/db:DB.AddTasksToSprint",
	"sprint move-tasks":              "internal/db:DB.MoveTasksBetweenSprints",
	"sprint remove-tasks":            "internal/commands:sprintRemoveTasks",
	"sprint remove":                  "internal/commands:sprintRemove",
	"The migration to schema 1.16.0": "internal/db:migrateV1_15_0_toV1_16_0",
}

// guardTableRow matches a row of the published write-path table, capturing its
// first cell with any backticks removed later.
var guardTableRow = regexp.MustCompile("^\\| ([^|]+?) \\| [^|]+ \\|$")

// TestSprintMembershipGuard_EveryPublishedWritePathCallsIt is the gate on the
// SPEC's enumeration, both ways: every write path the table lists is mapped to
// the function that implements it, that function calls CheckSprintMembershipTx,
// and no function outside the map calls it.
func TestSprintMembershipGuard_EveryPublishedWritePathCallsIt(t *testing.T) {
	root := moduleRoot(t)
	spec, err := os.ReadFile(filepath.Join(root, "SPEC", "DATABASE.md"))
	if err != nil {
		t.Fatalf("reading SPEC/DATABASE.md: %v", err)
	}
	section := string(spec)
	start := strings.Index(section, "### Sprint Membership Invariant Enforcement")
	if start < 0 {
		t.Fatal("SPEC/DATABASE.md no longer carries § Sprint Membership Invariant Enforcement")
	}
	section = section[start:]
	end := strings.Index(section[1:], "\n### ")
	section = section[:end+1]

	published := map[string]bool{}
	inTable := false
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "| Write path |") {
			inTable = true
			continue
		}
		if !inTable || strings.HasPrefix(line, "|---") {
			continue
		}
		m := guardTableRow.FindStringSubmatch(line)
		if m == nil {
			if line == "" {
				inTable = false
			}
			continue
		}
		published[strings.ReplaceAll(m[1], "`", "")] = true
	}
	if len(published) != len(guardWritePaths) {
		t.Errorf("the SPEC table lists %d write paths %v, this gate maps %d", len(published),
			sortedKeysOf(published), len(guardWritePaths))
	}
	for path := range published {
		if _, ok := guardWritePaths[path]; !ok {
			t.Errorf("the SPEC table lists the write path %q, which this gate does not map to a function", path)
		}
	}

	callers := guardCallers(t, root)
	for path, fn := range guardWritePaths {
		if !callers[fn] {
			t.Errorf("the write path %q is implemented by %s, which does not call CheckSprintMembershipTx", path, fn)
		}
	}
	mapped := map[string]bool{}
	for _, fn := range guardWritePaths {
		mapped[fn] = true
	}
	for fn := range callers {
		if !mapped[fn] {
			t.Errorf("%s calls CheckSprintMembershipTx but is not a write path the SPEC table lists; add the "+
				"path to SPEC/DATABASE.md § Sprint Membership Invariant Enforcement first", fn)
		}
	}
}

// guardCallers returns every production function of the module whose body,
// closures included, calls CheckSprintMembershipTx.
func guardCallers(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, dir := range []string{"internal/db", "internal/commands"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(root, dir, e.Name()), nil, 0)
			if err != nil {
				t.Fatalf("parsing %s/%s: %v", dir, e.Name(), err)
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				name := fn.Name.Name
				if fn.Recv != nil && len(fn.Recv.List) == 1 {
					recv := fn.Recv.List[0].Type
					if star, ok := recv.(*ast.StarExpr); ok {
						recv = star.X
					}
					if id, ok := recv.(*ast.Ident); ok {
						name = id.Name + "." + name
					}
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					switch f := call.Fun.(type) {
					case *ast.Ident:
						if f.Name == "CheckSprintMembershipTx" {
							out[dir+":"+name] = true
						}
					case *ast.SelectorExpr:
						if f.Sel.Name == "CheckSprintMembershipTx" {
							out[dir+":"+name] = true
						}
					}
					return true
				})
			}
		}
	}
	return out
}

func sortedKeysOf(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestNoTriggerAnywhere pins SPEC/DATABASE.md § Business Rules Are Enforced by
// Application Code: no CREATE TRIGGER in the schema, in a migration or in the
// production source, and none in a fresh database.
func TestNoTriggerAnywhere(t *testing.T) {
	root := moduleRoot(t)
	createTrigger := regexp.MustCompile(`(?i)create\s+(temp(orary)?\s+)?trigger`)
	for _, file := range productionGoFiles(t, root) {
		content, err := os.ReadFile(filepath.Clean(file))
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		if createTrigger.Match(content) {
			t.Errorf("%s contains a CREATE TRIGGER statement; business rules are enforced by application code", file)
		}
	}

	database, _, cleanup := setupCountingDB(t)
	defer cleanup()
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger'`).Scan(&n); err != nil {
		t.Fatalf("counting triggers: %v", err)
	}
	if n != 0 {
		t.Errorf("a fresh database holds %d trigger(s), want none", n)
	}
}
