// Package commands — regression suite for the removal of the task specialists
// surface (sprint 36, task #247).
//
// The Task entity lost its `specialists` field. This file pins the four things
// that removal has to mean at the command surface, each of which could regress
// independently:
//
//  1. `rmp task assign` and `rmp task unassign` are not subcommands. They are
//     rejected by the SAME unknown-subcommand path that rejects any other
//     unrecognised name — no reserved exit code of their own, no bespoke branch
//     (SPEC/COMMANDS.md § Command Aliases Reference, note on `assign` and
//     `unassign`).
//  2. `task create` and `task edit` reject `-sp` / `--specialists` as an unknown
//     flag.
//  3. `task list` no longer accepts the filter, and the filters that remain
//     still compose under AND — the removal must not have loosened the
//     conjunction into a disjunction, nor dropped a neighbouring filter.
//  4. No help output anywhere in the binary mentions the field or the two
//     retired subcommand names.
//
// On exit codes. This package cannot observe a process exit status, and the
// mapping from sentinel to code lives in cmd/rmp (handleError). What it CAN
// pin — and what actually decides the code — is which sentinel the error
// carries. handleError tests ErrNotFound, ErrAlreadyExists, ErrNoRoadmap and
// ErrValidation/ErrFieldTooLarge BEFORE ErrInvalidInput, so an error that
// wrapped one of those as well as ErrInvalidInput would NOT exit 2. Every
// rejection asserted here is therefore checked twice: it must wrap
// utils.ErrInvalidInput, and it must wrap none of the sentinels that outrank it.
// The literal `2` is pinned end-to-end by
// cmd/rmp.TestSpecialistsRemoval_AssignUnassignExitMisuse.
package commands

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// retiredSubcommandNames are the two names that must no longer resolve.
var retiredSubcommandNames = []string{"assign", "unassign"}

// outrankingSentinels are the sentinels handleError matches ahead of
// ErrInvalidInput. An error carrying any of them lands on a code other than 2,
// so a rejection that claims exit 2 must carry none of them.
var outrankingSentinels = []struct {
	name string
	err  error
}{
	{"utils.ErrNotFound", utils.ErrNotFound},
	{"utils.ErrAlreadyExists", utils.ErrAlreadyExists},
	{"utils.ErrNoRoadmap", utils.ErrNoRoadmap},
	{"utils.ErrValidation", utils.ErrValidation},
	{"utils.ErrFieldTooLarge", utils.ErrFieldTooLarge},
}

// requireMisuse asserts that err is the kind of error that exits 2: it wraps
// utils.ErrInvalidInput and none of the sentinels handleError matches first.
func requireMisuse(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", label)
	}
	if !errors.Is(err, utils.ErrInvalidInput) {
		t.Errorf("%s: error = %v, want it to wrap utils.ErrInvalidInput (exit 2)", label, err)
	}
	for _, s := range outrankingSentinels {
		if errors.Is(err, s.err) {
			t.Errorf("%s: error also wraps %s, which handleError matches BEFORE "+
				"ErrInvalidInput; this would exit on a code other than 2 (error: %v)", label, s.name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// 1. assign / unassign are unknown subcommands.
// ---------------------------------------------------------------------------

// TestSpecialistsRemoval_AssignUnassignRejectedAsUnknownSubcommand feeds the two
// retired names the exact argument shape they used to accept and requires the
// generic unknown-subcommand rejection.
//
// The message assertion is what distinguishes "the name is gone" from "the name
// survives and its handler happens to fail": a surviving handler would report a
// missing roadmap, a bad id, or a missing task, never
// "unknown task subcommand: <name>".
func TestSpecialistsRemoval_AssignUnassignRejectedAsUnknownSubcommand(t *testing.T) {
	testName := "testspecialistsremovalsub"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	for _, name := range retiredSubcommandNames {
		// The full historical invocation, roadmap and all, so the rejection
		// cannot be blamed on a missing prerequisite.
		err := HandleTask([]string{name, "-r", testName, "7", "backend-team"})
		label := "rmp task " + name
		requireMisuse(t, label, err)
		if err != nil {
			want := "unknown task subcommand: " + name
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: message = %q, want it to contain %q", label, err.Error(), want)
			}
		}

		// Bare, with no arguments at all.
		requireMisuse(t, "rmp task "+name+" (bare)", HandleTask([]string{name}))
	}
}

// TestSpecialistsRemoval_AssignUnassignHaveNoHelpPage guards the one route that
// bypasses the handler: registry dispatch serves subcommand help BEFORE calling
// the handler, so a registration left in place with only its handler removed
// would still print a help page and return nil here.
func TestSpecialistsRemoval_AssignUnassignHaveNoHelpPage(t *testing.T) {
	for _, name := range retiredSubcommandNames {
		for _, helpToken := range []string{"--help", "-h", "help"} {
			label := "rmp task " + name + " " + helpToken
			var err error
			out := captureStdout(t, func() {
				err = HandleTask([]string{name, helpToken})
			})
			requireMisuse(t, label, err)
			if strings.TrimSpace(out) != "" {
				t.Errorf("%s: printed a help page to stdout (%d bytes); the subcommand must not exist",
					label, len(out))
			}
		}
	}
}

// TestSpecialistsRemoval_AssignUnassignNotInRegistry asserts the absence at the
// registry, the single source of truth the dispatcher, the help and the
// machine-readable contract are all derived from. It also checks the alias
// tables, since FindSubcommand resolves aliases too.
//
// The control leg is load-bearing: `sev` is a name that DOES resolve, so a
// FindSubcommand that had broken and started returning nil for everything would
// fail here rather than turn the whole test vacuously green.
func TestSpecialistsRemoval_AssignUnassignNotInRegistry(t *testing.T) {
	taskCmd := AppRegistry().FindCommand("task")
	if taskCmd == nil {
		t.Fatal("task command missing from registry")
	}

	if taskCmd.FindSubcommand("sev") == nil {
		t.Fatal("control: FindSubcommand(\"sev\") returned nil; the lookup itself is broken, so the " +
			"absence assertions below would prove nothing")
	}

	for _, name := range retiredSubcommandNames {
		if sub := taskCmd.FindSubcommand(name); sub != nil {
			t.Errorf("subcommand %q still resolves in the registry (as %q)", name, sub.Name)
		}
	}
	for i := range taskCmd.Subcommands {
		sub := &taskCmd.Subcommands[i]
		for _, name := range retiredSubcommandNames {
			if sub.Name == name {
				t.Errorf("subcommand %q is still registered under task", name)
			}
			for _, alias := range sub.Aliases {
				if alias == name {
					t.Errorf("subcommand %q still carries alias %q", sub.Name, name)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 2. create / edit reject the flag.
// ---------------------------------------------------------------------------

// TestSpecialistsRemoval_CreateAndEditRejectTheFlag drives the flag through the
// real command entry point in both spellings and both GNU forms
// (`--specialists value` and `--specialists=value`), because the parser splits
// on '=' before it looks the name up and the two paths could diverge.
func TestSpecialistsRemoval_CreateAndEditRejectTheFlag(t *testing.T) {
	testName := "testspecialistsremovalflag"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	baseCreate := []string{
		"create", "-r", testName,
		"-t", "Cache the roadmap statistics query",
		"-fr", "The statistics page returns in under 200ms on a 5000-task roadmap",
		"-tr", "Memoise the aggregate query behind the existing query cache",
		"-ac", "A cold and a warm request return identical JSON",
	}
	baseEdit := []string{"edit", "-r", testName, "1"}

	cases := []struct {
		label string
		args  []string
	}{
		{"task create -sp", append(append([]string{}, baseCreate...), "-sp", "backend-team")},
		{"task create --specialists", append(append([]string{}, baseCreate...), "--specialists", "backend-team")},
		{"task create --specialists=", append(append([]string{}, baseCreate...), "--specialists=backend-team")},
		{"task edit -sp", append(append([]string{}, baseEdit...), "-sp", "backend-team")},
		{"task edit --specialists", append(append([]string{}, baseEdit...), "--specialists", "backend-team")},
		{"task edit --specialists=", append(append([]string{}, baseEdit...), "--specialists=backend-team")},
	}

	for _, c := range cases {
		err := HandleTask(c.args)
		requireMisuse(t, c.label, err)
		if err != nil && !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("%s: message = %q, want it to report an unknown flag", c.label, err.Error())
		}
	}
}

// TestSpecialistsRemoval_FlagDefinitionsGone asserts the absence in the three
// FlagDef tables the handlers parse against, so a stray definition is caught at
// its source rather than only through one handler that happens to be tested.
func TestSpecialistsRemoval_FlagDefinitionsGone(t *testing.T) {
	tables := []struct {
		name string
		defs []FlagDef
	}{
		{"TaskCreateFlags", TaskCreateFlags},
		{"TaskEditFlags", TaskEditFlags},
		{"TaskListFlags", TaskListFlags},
	}
	for _, table := range tables {
		if len(table.defs) == 0 {
			t.Errorf("control: %s is empty; the absence assertion below would be vacuous", table.name)
		}
		for _, def := range table.defs {
			if def.Name == "--specialists" || def.Short == "-sp" || def.Field == "Specialists" {
				t.Errorf("%s still defines the removed flag: %+v", table.name, def)
			}
		}
	}
}

// TestSpecialistsRemoval_NoRegistryFlagNamedSpecialists sweeps the WHOLE
// registry, not just the task family: the contract and every generated help
// surface are built from these Flag values, so one left behind anywhere would
// republish the removed flag.
func TestSpecialistsRemoval_NoRegistryFlagNamedSpecialists(t *testing.T) {
	reg := AppRegistry()
	seen := 0
	check := func(label string, flags []Flag) {
		for _, f := range flags {
			seen++
			if f.Long == "--specialists" || f.Short == "-sp" {
				t.Errorf("%s: registry still publishes the removed flag %s / %s", label, f.Long, f.Short)
			}
		}
	}
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		for j := range cmd.Subcommands {
			sub := &cmd.Subcommands[j]
			check("rmp "+cmd.Name+" "+sub.Name, sub.Flags)
		}
	}
	if seen == 0 {
		t.Fatal("control: the registry sweep inspected 0 flags; the assertion is vacuous")
	}
}

// ---------------------------------------------------------------------------
// 3. task list: filter gone, remaining filters still AND.
// ---------------------------------------------------------------------------

// TestSpecialistsRemoval_ListRejectsTheFilter drives `task list` with the
// removed filter in every spelling.
func TestSpecialistsRemoval_ListRejectsTheFilter(t *testing.T) {
	testName := "testspecialistsremovallistflag"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	cases := [][]string{
		{"list", "-r", testName, "-sp", "backend"},
		{"list", "-r", testName, "--specialists", "backend"},
		{"list", "-r", testName, "--specialists=backend"},
		// Combined with a filter that DOES survive, so the rejection cannot be
		// attributed to the rest of the invocation.
		{"list", "-r", testName, "--status", "BACKLOG", "-sp", "backend"},
	}
	for _, args := range cases {
		label := "rmp task " + strings.Join(args, " ")
		err := HandleTask(args)
		requireMisuse(t, label, err)
		if err != nil && !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("%s: message = %q, want it to report an unknown flag", label, err.Error())
		}
	}
}

// seedANDCorpus inserts four tasks chosen so that each surviving filter of the
// AND test excludes a different one. Exactly one row satisfies all three
// conditions, and each pair of conditions admits strictly more than one — which
// is what makes the conjunction observable rather than coincidental.
//
// | id | title                     | status  | type | priority |
// |----|---------------------------|---------|------|----------|
// | 1  | matches everything        | BACKLOG | BUG  | 8        |
// | 2  | wrong status              | DOING   | BUG  | 8        |
// | 3  | wrong type                | BACKLOG | TASK | 8        |
// | 4  | priority under the floor  | BACKLOG | BUG  | 2        |
func seedANDCorpus(t *testing.T, database *db.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows := []struct {
		title    string
		status   string
		taskType string
		priority int
	}{
		{"Reject expired refresh tokens on reuse", "BACKLOG", "BUG", 8},
		{"Audit log loses the sprint id on move", "DOING", "BUG", 8},
		{"Extract the roadmap path resolver", "BACKLOG", "TASK", 8},
		{"Tolerate a trailing slash in the data dir", "BACKLOG", "BUG", 2},
	}
	for _, r := range rows {
		_, err := database.ExecContext(ctx,
			`INSERT INTO tasks (title, status, type, functional_requirements,
			                    technical_requirements, acceptance_criteria, created_at, priority, severity)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			r.title, r.status, r.taskType,
			"Functional requirement for "+r.title,
			"Technical requirement for "+r.title,
			"Acceptance criteria for "+r.title,
			utils.NowISO8601(), r.priority)
		if err != nil {
			t.Fatalf("seeding %q: %v", r.title, err)
		}
	}
}

// listTitles runs `task list` with the given arguments and returns the titles it
// emitted, in order.
func listTitles(t *testing.T, args []string) []string {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = HandleTask(args)
	})
	if err != nil {
		t.Fatalf("rmp task %s: %v", strings.Join(args, " "), err)
	}
	var tasks []struct {
		Title string `json:"title"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &tasks); jsonErr != nil {
		t.Fatalf("rmp task %s: stdout is not a JSON array: %v (raw: %q)",
			strings.Join(args, " "), jsonErr, out)
	}
	titles := make([]string, 0, len(tasks))
	for _, task := range tasks {
		titles = append(titles, task.Title)
	}
	return titles
}

// TestSpecialistsRemoval_ListRemainingFiltersComposeUnderAND proves the
// surviving filters intersect rather than union.
//
// Removing one filter from a chain is what makes this non-vacuous: if the
// handler had started ORing, or had stopped applying one of the three, the
// three-filter query would return more than the single row that satisfies all
// of them — and each two-filter query is required to return MORE than the
// three-filter one, so a handler that ignored a filter outright is caught in
// both directions.
func TestSpecialistsRemoval_ListRemainingFiltersComposeUnderAND(t *testing.T) {
	testName := "testspecialistsremovaland"
	database, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()
	seedANDCorpus(t, database)

	const wanted = "Reject expired refresh tokens on reuse"

	// All three together: exactly the one row that satisfies every condition.
	got := listTitles(t, []string{"list", "-r", testName,
		"--status", "BACKLOG", "--type", "BUG", "--priority", "8"})
	if len(got) != 1 || got[0] != wanted {
		t.Errorf("status+type+priority: got %v, want exactly [%q]", got, wanted)
	}

	// Each pair must admit strictly more, and must still contain the row the
	// full conjunction selected.
	pairs := []struct {
		label string
		args  []string
	}{
		{"status+type (priority dropped)", []string{"--status", "BACKLOG", "--type", "BUG"}},
		{"status+priority (type dropped)", []string{"--status", "BACKLOG", "--priority", "8"}},
		{"type+priority (status dropped)", []string{"--type", "BUG", "--priority", "8"}},
	}
	for _, p := range pairs {
		args := append([]string{"list", "-r", testName}, p.args...)
		gotPair := listTitles(t, args)
		if len(gotPair) <= 1 {
			t.Errorf("%s: got %v (%d rows), want more than the 1 the full conjunction returns; "+
				"the dropped filter was not the thing excluding the extra row",
				p.label, gotPair, len(gotPair))
		}
		if !containsString(gotPair, wanted) {
			t.Errorf("%s: got %v, want it to still contain %q", p.label, gotPair, wanted)
		}
	}

	// And the corpus really is a corpus: an unfiltered list returns all four.
	all := listTitles(t, []string{"list", "-r", testName})
	if len(all) != 4 {
		t.Errorf("unfiltered list returned %d rows, want 4; the seed did not land", len(all))
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 4. No help output mentions the removed surface.
// ---------------------------------------------------------------------------

// retiredSubcommandWord matches either retired name as a whole word. The word
// boundary is the entire point: the help corpus legitimately says "every task
// assigned to <sprint-id>" and "auto-assigned", and a bare substring search for
// "assign" flags both. `\bassign\b` matches the command token and neither of
// those, because "assigned" continues with a word character.
var retiredSubcommandWord = regexp.MustCompile(`(?i)\b(?:un)?assign\b`)

// TestSpecialistsRemoval_HelpNeedleDiscriminates is the control for the sweep
// below. A needle that matched nothing anywhere would make the sweep pass no
// matter what shipped, so the pattern is pinned against one string it MUST
// match and one it MUST NOT — the exact false positive that a substring search
// produces on the live sprint help.
func TestSpecialistsRemoval_HelpNeedleDiscriminates(t *testing.T) {
	mustMatch := []string{
		"rmp task assign -r myproject 7 alice",
		"  assign <task-id> <specialist>               Add specialist to task",
		"Use 'task unassign' to remove a specialist.",
		"  assign, unassign                                Empty (exit 0 on success).",
	}
	for _, sample := range mustMatch {
		if !retiredSubcommandWord.MatchString(sample) {
			t.Errorf("needle failed to match %q; the sweep would miss a real regression", sample)
		}
	}

	mustNotMatch := []string{
		"Lists every task assigned to <sprint-id>, regardless of status",
		"omitted, the next available value is auto-assigned as the highest existing",
		"Task is assigned to a sprint.",
		"Create, list, query, mutate, and order sprints and their task assignments",
	}
	for _, sample := range mustNotMatch {
		if retiredSubcommandWord.MatchString(sample) {
			t.Errorf("needle wrongly matched %q; the sweep would report a false positive", sample)
		}
	}
}

// TestSpecialistsRemoval_NoHelpOutputMentionsTheRemovedSurface sweeps every help
// output in the binary and requires that none of them names the removed field,
// the removed flag, or either retired subcommand
// (SPEC/HELP.md § Command Family Inventory).
func TestSpecialistsRemoval_NoHelpOutputMentionsTheRemovedSurface(t *testing.T) {
	outputs := allHelpOutputs(t)
	if len(outputs) == 0 {
		t.Fatal("control: allHelpOutputs returned nothing; the sweep is vacuous")
	}

	substrings := []string{
		"specialist", // the field, under any inflection
		"-sp,",       // the short form as help lines render it
		"-sp ",
		"--specialists",
	}

	for _, pair := range outputs {
		lower := strings.ToLower(pair.out)
		for _, needle := range substrings {
			if strings.Contains(lower, needle) {
				t.Errorf("%s: help output still mentions %q", pair.label, needle)
			}
		}
		if loc := retiredSubcommandWord.FindString(pair.out); loc != "" {
			t.Errorf("%s: help output still names the retired subcommand %q", pair.label, loc)
		}
	}
}

// ---------------------------------------------------------------------------
// 5. The audit read surface no longer accepts the retired operations by name.
// ---------------------------------------------------------------------------

// TestSpecialistsRemoval_AuditOperationFilterRejectsRetiredNames pins rule 1 of
// SPEC/DATABASE.md § audit Table, "A stored row may carry an operation the
// catalogue does not list": the two retired values are out of the valid set, so
// an `--operation` filter naming one is rejected as an invalid operation at exit
// code 6 — exactly as any other name outside the catalogue is.
//
// This is the command-surface half of the rule. internal/models covers the enum
// and internal/db covers the survival of the stored rows; neither observes what
// `rmp audit list -o TASK_ASSIGN` does.
//
// Note the sentinel: this one is utils.ErrValidation (exit 6), NOT
// ErrInvalidInput (exit 2). The name is a well-formed value of a known flag that
// fails validation, not a syntax error — so requireMisuse is deliberately not
// used here.
func TestSpecialistsRemoval_AuditOperationFilterRejectsRetiredNames(t *testing.T) {
	testName := "testspecialistsremovalauditop"
	_, cleanup := setupTestTaskRoadmap(t, testName)
	defer cleanup()

	// Control: an operation still in the catalogue must be ACCEPTED, so a filter
	// that had started rejecting everything cannot make the rejections below
	// look like proof.
	if err := HandleAudit([]string{"list", "-r", testName, "-o", string(models.OpTaskUpdate)}); err != nil {
		t.Fatalf("control: `audit list -o TASK_UPDATE` = %v, want nil; the filter rejects valid "+
			"operations too, so the assertions below prove nothing", err)
	}

	for _, name := range []string{"TASK_ASSIGN", "TASK_UNASSIGN"} {
		err := HandleAudit([]string{"list", "-r", testName, "-o", name})
		if err == nil {
			t.Errorf("`audit list -o %s`: expected rejection, got nil; the retired operation is still "+
				"in the valid set", name)
			continue
		}
		if !errors.Is(err, utils.ErrValidation) {
			t.Errorf("`audit list -o %s`: error = %v, want utils.ErrValidation (exit 6)", name, err)
		}
		if !strings.Contains(err.Error(), "invalid operation") {
			t.Errorf("`audit list -o %s`: message = %q, want the generic invalid-operation rejection",
				name, err.Error())
		}
	}
}
