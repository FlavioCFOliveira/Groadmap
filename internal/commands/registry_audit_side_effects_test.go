// Package commands — gates over the audit operations a mutating subcommand
// publishes in its side effects.
//
// `side_effects.database` is the only place the AI Agent Contract says what a
// write subcommand does to the database (SPEC/DATA_FORMATS.md § subcommands
// array entry). For a subcommand whose rows an agent will later want to find,
// "audit log" alone is not a description of a write: it says an entry appears
// somewhere without saying which `audit list --operation` filter reaches it. Six
// subcommands write more than one operation, or write an operation whose name is
// not derivable from the subcommand's own name, and those are the six this file
// pins.
//
// The pinning here is well-formedness and non-vacuity: every operation named is
// a real catalogue value, every subcommand still exists, and the declared set is
// actually present in the published text. The EMPIRICAL half — that the text
// names every operation the subcommand really writes, observed by reading the
// audit table back — lives in tests/test_05_audit_reporting.py, which drives the
// compiled binary. Neither half subsumes the other: this one cannot observe a
// write, and that one cannot fail at `go test` time.
package commands

import (
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// auditOperationToken matches a token shaped like a catalogue value: two or more
// SCREAMING_SNAKE_CASE words. It is deliberately broader than the catalogue, so
// a misspelt operation in the prose is caught as an invalid token rather than
// slipping past as text nobody reads.
//
// The `TASK_STATUS_*` wildcard one of the texts uses is NOT a token of this
// pattern, and no exemption list is needed for it: `_` is a word character, so
// the trailing `\b` never closes after `TASK_STATUS`, and the match is
// abandoned. Measured, not assumed — scanning that sentence yields only
// SPRINT_MOVE_TASK. A bare `TASK_STATUS` written without the wildcard WOULD be
// flagged, which is right: it is not a value `audit list --operation` accepts.
var auditOperationToken = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+\b`)

// auditSideEffectExpectations lists, per subcommand, the operations its
// side-effect text MUST name. The sets were established by reading the writers,
// not by reading the subcommand names:
//
//   - task edit        internal/commands/task_edit.go, taskEditFieldOperations
//   - task stat        internal/commands/task_mutate.go, the destination switch
//   - sprint update    internal/commands/sprint_crud.go, the `ops` accumulator
//   - sprint add-tasks internal/db/queries.go, AddTasksToSprint
//   - sprint remove-tasks internal/commands/sprint_tasks.go, sprintRemoveTasks
//   - sprint move-tasks   internal/db/queries.go, MoveTasksBetweenSprints
//
// A LEGACY operation named in the prose as one the subcommand does NOT write is
// listed here too: the sentence that says nothing writes it is exactly as
// load-bearing as the ones that say what is written, and dropping it would let
// the text go quiet about the value a reader is most likely to pick by mistake.
var auditSideEffectExpectations = []struct {
	family     string
	subcommand string
	operations []models.AuditOperation
}{
	{"task", "edit", []models.AuditOperation{
		models.OpTaskTitleChange,
		models.OpTaskTypeChange,
		models.OpTaskFunctionalRequirementsChange,
		models.OpTaskTechnicalRequirementsChange,
		models.OpTaskAcceptanceCriteriaChange,
		models.OpTaskPriorityChange,
		models.OpTaskSeverityChange,
		models.OpTaskUpdate, // named as the LEGACY value nothing writes
	}},
	{"task", "stat", []models.AuditOperation{
		models.OpTaskStatusBacklog,
		models.OpTaskStatusDoing,
		models.OpTaskStatusTesting,
		models.OpTaskStatusCompleted,
		models.OpTaskStatusSprint, // named as the one this subcommand rejects
		models.OpTaskStatusChange, // named as the LEGACY value nothing writes
	}},
	{"sprint", "update", []models.AuditOperation{
		models.OpSprintTitleChange,
		models.OpSprintDescriptionChange,
		models.OpSprintMaxTasksChange,
		models.OpSprintOrderChange,
		models.OpSprintUpdate, // named as the LEGACY value nothing writes
	}},
	{"sprint", "add-tasks", []models.AuditOperation{
		models.OpSprintAddTask,
		models.OpTaskStatusSprint,
	}},
	{"sprint", "remove-tasks", []models.AuditOperation{
		models.OpSprintRemoveTask,
		models.OpTaskStatusBacklog,
	}},
	{"sprint", "move-tasks", []models.AuditOperation{
		models.OpSprintMoveTaskOut,
		models.OpSprintMoveTaskIn,
		models.OpSprintMoveTask, // named as the LEGACY value nothing writes
	}},
}

// subcommandSideEffects resolves one subcommand through the registry and returns
// its database side-effect text. Resolving through the registry rather than
// reaching into the builder functions is what makes the gate fail when a
// subcommand is renamed: the name in the table stops resolving.
func subcommandSideEffects(t *testing.T, family, sub string) string {
	t.Helper()
	cmd := AppRegistry().FindCommand(family)
	if cmd == nil {
		t.Fatalf("command family %q is not registered", family)
	}
	entry := cmd.FindSubcommand(sub)
	if entry == nil {
		t.Fatalf("%s %s is not registered; the expectation table names a subcommand that no longer exists",
			family, sub)
	}
	return entry.SideEffects.Database
}

// TestAuditSideEffects_NameTheOperationsTheyWrite is acceptance criterion 3 of
// rmp task #266: the side-effect text of the six mutating subcommands names the
// operations each writes. An agent reading the contract otherwise learns that a
// row appeared without learning the filter that finds it again.
func TestAuditSideEffects_NameTheOperationsTheyWrite(t *testing.T) {
	if len(auditSideEffectExpectations) != 6 {
		t.Fatalf("the expectation table holds %d subcommands, want the 6 named by the acceptance criterion",
			len(auditSideEffectExpectations))
	}

	checked := 0
	for _, want := range auditSideEffectExpectations {
		label := want.family + " " + want.subcommand
		text := subcommandSideEffects(t, want.family, want.subcommand)
		if strings.TrimSpace(text) == "" {
			t.Errorf("%s: side_effects.database is empty", label)
			continue
		}
		if len(want.operations) == 0 {
			t.Errorf("%s: the expectation table names no operation for it, so this row asserts nothing", label)
			continue
		}
		for _, op := range want.operations {
			if !strings.Contains(text, string(op)) {
				t.Errorf("%s: side_effects.database does not name %s, so an agent reading the contract "+
					"cannot tell which `audit list --operation` filter finds the row this subcommand "+
					"writes.\n  text: %s", label, op, text)
				continue
			}
			checked++
		}
	}

	wantChecks := 0
	for _, want := range auditSideEffectExpectations {
		wantChecks += len(want.operations)
	}
	if checked != wantChecks {
		t.Errorf("%d of %d expected operations were found; a gate that stops matching reports success "+
			"while measuring nothing", checked, wantChecks)
	}
}

// TestAuditSideEffects_NameOnlyRealOperations is the converse direction, and it
// is what makes a typo in the prose fail rather than mislead. Every token in the
// six texts shaped like a catalogue value must BE one: a text naming
// TASK_PRIORITY_CHANGED sends an agent to a filter `audit list` rejects with
// exit 6, and nothing else in the build would notice.
func TestAuditSideEffects_NameOnlyRealOperations(t *testing.T) {
	scanned := 0
	for _, want := range auditSideEffectExpectations {
		label := want.family + " " + want.subcommand
		text := subcommandSideEffects(t, want.family, want.subcommand)
		for _, token := range auditOperationToken.FindAllString(text, -1) {
			scanned++
			if !models.IsValidAuditOperation(token) {
				t.Errorf("%s: side_effects.database names %q, which is not a value in "+
					"ValidAuditOperations; `audit list --operation %s` exits 6.\n  text: %s",
					label, token, token, text)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no operation-shaped token was found in any of the six side-effect texts, so this gate " +
			"scanned nothing; either the texts lost their operation names or the token pattern stopped " +
			"matching them")
	}
}

// TestAuditSideEffects_LegacyOperationsAreNamedAsUnwritten pins the one thing a
// list of written operations cannot say. Three of the four LEGACY values were
// retired from exactly these subcommands, so a reader of `task edit` is more
// likely to reach for TASK_UPDATE than for any operation the subcommand does
// write. Naming it as unwritten is what stops that.
func TestAuditSideEffects_LegacyOperationsAreNamedAsUnwritten(t *testing.T) {
	// The subcommand each retired operation used to be written by.
	retiredFrom := map[models.AuditOperation]string{
		models.OpTaskUpdate:       "task edit",
		models.OpTaskStatusChange: "task stat",
		models.OpSprintUpdate:     "sprint update",
		models.OpSprintMoveTask:   "sprint move-tasks",
	}

	for op, label := range retiredFrom {
		class, declared := models.ClassifyAuditOperation(op)
		if !declared {
			t.Errorf("%s has no declared classification; see TestAuditOperationClassification_IsTotal", op)
			continue
		}
		if !class.Legacy {
			t.Errorf("%s is listed here as retired from `%s` but internal/models does not mark it Legacy; "+
				"if a command writes it again, this expectation is the thing that is wrong", op, label)
			continue
		}
		parts := strings.SplitN(label, " ", 2)
		text := subcommandSideEffects(t, parts[0], parts[1])
		if !strings.Contains(text, "LEGACY "+string(op)) {
			t.Errorf("`%s`: side_effects.database does not say that the LEGACY %s is not written. That "+
				"value was retired from this very subcommand, so it is the filter a reader is most likely "+
				"to choose and get nothing back from.\n  text: %s", label, op, text)
		}
	}
}
