// Package aihelp — the gate that pins the `delete_non_backlog_task` pitfall to
// the routes back to BACKLOG that actually work (rmp task #232).
//
// The pitfall told an agent that a non-BACKLOG task "must be moved back to
// BACKLOG first (via `sprint remove-tasks` for SPRINT, or `task reopen` for
// COMPLETED)". Both halves are true and the sentence is still wrong, because
// the route it leaves out — `task stat <ids> BACKLOG`, legal from SPRINT and
// from COMPLETED — is the cheap one, and it is the only one that returns the
// task without also throwing away its place in the sprint. An agent that
// followed the pitfall as written would detach a task from its sprint to delete
// a sibling.
//
// A pitfall is prose, so the only way to keep it honest is to measure the thing
// it describes and read the prose back against the measurement. That is what
// this file does: for each of the four non-BACKLOG source states it tries every
// route the pitfall names, records which ones landed the task in BACKLOG, and
// requires the sentence to name exactly the states each route was observed to
// work from.
//
// The commands are driven through commands.AppRegistry(), the same resolution
// the binary performs, so a renamed subcommand fails this gate instead of
// quietly making it untestable.
package aihelp

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/commands"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ---------------------------------------------------------------------------
// Driving the real commands
// ---------------------------------------------------------------------------

// pitfallRoadmap is the roadmap every case in this file is built in.
const pitfallRoadmap = "pitfall-backlog-routes"

// Commit hashes the walks supply. Real short hashes from this repository's
// history.
const (
	pitfallCommitOpen  = "5f93b51"
	pitfallCommitClose = "391cff7"
)

// captureCommandStdout redirects os.Stdout for the duration of fn. The command
// handlers write their JSON there, and a test that let it through would bury
// the `go test` output under it.
func captureCommandStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	os.Stdout = old
	<-done
	return buf.String()
}

// invoke resolves family/sub through the registry and runs the handler,
// returning its stdout and whatever it returned.
func invoke(t *testing.T, family, sub string, args ...string) (string, error) {
	t.Helper()

	cmd := commands.AppRegistry().FindCommand(family)
	if cmd == nil {
		t.Fatalf("command family %q is not registered; this gate drives it and can no longer find it",
			family)
	}
	entry := cmd.FindSubcommand(sub)
	if entry == nil {
		t.Fatalf("`%s %s` is not registered; this gate drives it and can no longer find it", family, sub)
	}

	var err error
	out := captureCommandStdout(t, func() { err = entry.Handler(args) })
	return out, err
}

// mustInvoke runs a command that has to succeed.
func mustInvoke(t *testing.T, family, sub string, args ...string) string {
	t.Helper()
	out, err := invoke(t, family, sub, args...)
	if err != nil {
		t.Fatalf("`%s %s %v`: %v", family, sub, args, err)
	}
	return out
}

// pitfallFixture is a roadmap with one OPEN sprint, built through the CLI.
type pitfallFixture struct {
	sprintID int
	seq      int
}

func setupPitfallRoadmap(t *testing.T) *pitfallFixture {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	mustInvoke(t, "roadmap", "create", pitfallRoadmap)
	mustInvoke(t, "sprint", "create",
		"-r", pitfallRoadmap,
		"-t", "Session store hardening",
		"-d", "Persist sessions to the shared store so a node restart keeps every live session.")

	sprints := decodeIDs(t, "sprint list", mustInvoke(t, "sprint", "list", "-r", pitfallRoadmap))
	if len(sprints) != 1 {
		t.Fatalf("seeded 1 sprint, `sprint list` reported %d", len(sprints))
	}
	f := &pitfallFixture{sprintID: sprints[0]}

	mustInvoke(t, "sprint", "start", "-r", pitfallRoadmap, itoa(f.sprintID))
	return f
}

// decodeIDs pulls the id field out of a JSON array of objects.
func decodeIDs(t *testing.T, label, out string) []int {
	t.Helper()
	var rows []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%s printed %q, which is not a JSON array of objects: %v", label, out, err)
	}
	ids := make([]int, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// pitfallTaskTitles keep the fixture reading like real roadmap work.
var pitfallTaskTitles = []string{
	"Rotate the JWT signing key without downtime",
	"Move session tokens to the encrypted store",
	"Rate-limit the password reset endpoint",
	"Record the audit row inside the mutation transaction",
}

// taskInState manufactures a fresh task and walks it into the requested state
// through the real commands, verifying the state before handing the id back.
func (f *pitfallFixture) taskInState(t *testing.T, status models.TaskStatus) int {
	t.Helper()

	f.seq++
	out := mustInvoke(t, "task", "create",
		"-r", pitfallRoadmap,
		"-t", pitfallTaskTitles[f.seq%len(pitfallTaskTitles)]+" ("+itoa(f.seq)+")",
		"-fr", "The behaviour survives a restart of every node in the pool.",
		"-tr", "Route the write through the shared store and migrate the existing rows.",
		"-ac", "A restart leaves every live session usable.")

	var created struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("`task create` printed %q, not the {\"id\": N} object: %v", out, err)
	}
	id := created.ID

	if status != models.StatusBacklog {
		mustInvoke(t, "sprint", "add-tasks", "-r", pitfallRoadmap, itoa(f.sprintID), itoa(id))
		switch status {
		case models.StatusSprint:
		case models.StatusDoing:
			f.stat(t, id, models.StatusDoing, "--commit-open", pitfallCommitOpen)
		case models.StatusTesting:
			f.stat(t, id, models.StatusDoing, "--commit-open", pitfallCommitOpen)
			f.stat(t, id, models.StatusTesting)
		case models.StatusCompleted:
			f.stat(t, id, models.StatusDoing, "--commit-open", pitfallCommitOpen)
			f.stat(t, id, models.StatusTesting)
			f.stat(t, id, models.StatusCompleted, "--commit-close", pitfallCommitClose)
		default:
			t.Fatalf("no route to state %s", status)
		}
	}

	if got := f.statusOf(t, id); got != status {
		t.Fatalf("task #%d was walked to %s but reads %s", id, status, got)
	}
	return id
}

func (f *pitfallFixture) stat(t *testing.T, id int, status models.TaskStatus, extra ...string) {
	t.Helper()
	mustInvoke(t, "task", "stat",
		append([]string{"-r", pitfallRoadmap, itoa(id), string(status)}, extra...)...)
}

// statusOf reads a task's status back through `task get`, the published read
// path, so the observation is one an agent could make for itself.
func (f *pitfallFixture) statusOf(t *testing.T, id int) models.TaskStatus {
	t.Helper()
	out := mustInvoke(t, "task", "get", "-r", pitfallRoadmap, itoa(id))
	var rows []struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("`task get` printed %q, which is not a task array: %v", out, err)
	}
	if len(rows) != 1 {
		t.Fatalf("`task get %d` returned %d tasks", id, len(rows))
	}
	return models.TaskStatus(rows[0].Status)
}

// ---------------------------------------------------------------------------
// The routes the pitfall names
// ---------------------------------------------------------------------------

// backlogRoute is one way back to BACKLOG, as the pitfall names it and as the
// CLI performs it. marker is the literal the pitfall must use, so a reworded
// pitfall fails this gate rather than losing a route from it silently.
type backlogRoute struct {
	run    func(t *testing.T, f *pitfallFixture, id int) error
	marker string
}

var backlogRoutes = []backlogRoute{
	{
		marker: "`task stat <ids> BACKLOG` from ",
		run: func(t *testing.T, f *pitfallFixture, id int) error {
			_, err := invoke(t, "task", "stat", "-r", pitfallRoadmap, itoa(id), string(models.StatusBacklog))
			return err
		},
	},
	{
		marker: "`task reopen` from ",
		run: func(t *testing.T, f *pitfallFixture, id int) error {
			_, err := invoke(t, "task", "reopen", "-r", pitfallRoadmap, itoa(id))
			return err
		},
	},
	{
		marker: "`sprint remove-tasks` from ",
		run: func(t *testing.T, f *pitfallFixture, id int) error {
			_, err := invoke(t, "sprint", "remove-tasks",
				"-r", pitfallRoadmap, itoa(f.sprintID), itoa(id))
			return err
		},
	},
}

// nonBacklogStates are the four source states `task remove` refuses.
var nonBacklogStates = []models.TaskStatus{
	models.StatusSprint,
	models.StatusDoing,
	models.StatusTesting,
	models.StatusCompleted,
}

// statusWord matches a task-status token inside the pitfall prose.
var statusWord = regexp.MustCompile(`\b(BACKLOG|SPRINT|DOING|TESTING|COMPLETED)\b`)

// deleteNonBacklogPitfall returns the catalogue entry under test.
func deleteNonBacklogPitfall(t *testing.T) Pitfall {
	t.Helper()
	const want = "delete_non_backlog_task"
	for _, p := range staticPitfalls() {
		if p.ID == want {
			return p
		}
	}
	t.Fatalf("the pitfall catalogue no longer carries %q; SPEC/DATA_FORMATS.md mandates it and this "+
		"gate pins its description", want)
	return Pitfall{}
}

// declaredSources extracts the source states the description attributes to one
// route. Exactly one occurrence of the marker is required: zero means the
// pitfall stopped naming the route, more than one means the extraction would
// have to guess which occurrence is the claim.
func declaredSources(t *testing.T, description, marker string) map[models.TaskStatus]bool {
	t.Helper()

	pattern := regexp.MustCompile(regexp.QuoteMeta(marker) + `([^;.]*)`)
	matches := pattern.FindAllStringSubmatch(description, -1)
	if len(matches) != 1 {
		t.Fatalf("the pitfall description should name the route %q exactly once and say which source "+
			"states it works from; found %d occurrences:\n%s", marker, len(matches), description)
	}

	out := map[models.TaskStatus]bool{}
	for _, word := range statusWord.FindAllString(matches[0][1], -1) {
		// BACKLOG is the destination of every route, never a source of one.
		if status := models.TaskStatus(word); status != models.StatusBacklog {
			out[status] = true
		}
	}
	return out
}

// TestDeleteNonBacklogPitfall_NamesEveryRouteBackToBacklog drives each route
// from each non-BACKLOG source state and requires the pitfall to name exactly
// the states the route was observed to work from.
func TestDeleteNonBacklogPitfall_NamesEveryRouteBackToBacklog(t *testing.T) {
	pitfall := deleteNonBacklogPitfall(t)

	for _, route := range backlogRoutes {
		observed := map[models.TaskStatus]bool{}

		for _, source := range nonBacklogStates {
			// A roadmap per case: `sprint remove-tasks` empties the sprint and
			// `task stat` mutates in place, so sharing one would let an earlier
			// case decide a later one.
			f := setupPitfallRoadmap(t)
			id := f.taskInState(t, source)

			err := route.run(t, f, id)
			after := f.statusOf(t, id)

			switch {
			case err == nil && after == models.StatusBacklog:
				observed[source] = true
			case err == nil:
				t.Fatalf("%q from %s reported success but left task #%d in %s", route.marker, source, id, after)
			case after != source:
				t.Fatalf("%q from %s was refused (%v) but moved task #%d to %s; a refusal must leave "+
					"the task untouched", route.marker, source, err, id, after)
			}
		}

		declared := declaredSources(t, pitfall.Description, route.marker)
		if !sameStates(declared, observed) {
			t.Errorf("the pitfall says %q works from %s, but it was observed to work from %s.\n"+
				"  description: %s", route.marker, statesString(declared), statesString(observed),
				pitfall.Description)
		}
		if len(observed) == 0 {
			t.Errorf("%q returned nothing to BACKLOG from any source state; a route the pitfall names "+
				"must be a route that works", route.marker)
		}
	}
}

// TestDeleteNonBacklogPitfall_StatBacklogKeepsSprintMembership pins the closing
// claim of the corrected description: the task `task stat <ids> BACKLOG`
// returns is still a sprint member, and `task remove` takes it anyway because
// the deletion precondition tests the status alone
// (SPEC/STATE_MACHINE.md § Task Deletion Precondition).
//
// The claim matters because it is the reason the omitted route is the one an
// agent should reach for: it is the only route back that costs the task
// nothing, so an agent that does not know it exists pays for the deletion of
// one task with the sprint membership of another.
func TestDeleteNonBacklogPitfall_StatBacklogKeepsSprintMembership(t *testing.T) {
	f := setupPitfallRoadmap(t)
	id := f.taskInState(t, models.StatusSprint)

	// The premise of the whole pitfall: a non-BACKLOG task is not removable.
	if _, err := invoke(t, "task", "remove", "-r", pitfallRoadmap, itoa(id)); err == nil {
		t.Fatal("`task remove` accepted a SPRINT task; the pitfall exists because it refuses one")
	}

	mustInvoke(t, "task", "stat", "-r", pitfallRoadmap, itoa(id), string(models.StatusBacklog))

	members := decodeIDs(t, "sprint tasks",
		mustInvoke(t, "sprint", "tasks", "-r", pitfallRoadmap, itoa(f.sprintID)))
	stillMember := false
	for _, m := range members {
		if m == id {
			stillMember = true
		}
	}

	if _, err := invoke(t, "task", "remove", "-r", pitfallRoadmap, itoa(id)); err != nil {
		t.Fatalf("`task remove` refused the BACKLOG task #%d: %v", id, err)
	}

	description := deleteNonBacklogPitfall(t).Description
	const membershipClaim = "stays a member of its sprint"

	if stillMember {
		if !strings.Contains(description, membershipClaim) {
			t.Errorf("`task stat <id> BACKLOG` left task #%d a member of sprint %d and `task remove` "+
				"took it anyway, but the pitfall does not say the membership survives (missing %q):\n%s",
				id, f.sprintID, membershipClaim, description)
		}
		return
	}

	// The other direction: if the route ever starts detaching, the claim above
	// is the false one and this is what says so.
	if strings.Contains(description, membershipClaim) {
		t.Errorf("`task stat <id> BACKLOG` detached task #%d from sprint %d, but the pitfall still says "+
			"%q:\n%s", id, f.sprintID, membershipClaim, description)
	}
}

func sameStates(a, b map[models.TaskStatus]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// statesString renders a state set in the state machine's own listing order.
func statesString(set map[models.TaskStatus]bool) string {
	ordered := []models.TaskStatus{
		models.StatusBacklog, models.StatusSprint, models.StatusDoing,
		models.StatusTesting, models.StatusCompleted,
	}
	parts := make([]string, 0, len(set))
	for _, s := range ordered {
		if set[s] {
			parts = append(parts, string(s))
		}
	}
	if len(parts) == 0 {
		return "(nothing)"
	}
	return strings.Join(parts, ", ")
}
