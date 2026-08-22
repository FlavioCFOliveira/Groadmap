// Package commands — the gate that pins the taskSetStatus doc comment to the
// transitions the command actually accepts (rmp task #232).
//
// The comment listed DOING → SPRINT as a valid manual transition. It never was:
// models.CanTransitionTo gives DOING the single target TESTING, and fifty lines
// under the comment the command refuses the SPRINT target outright. Two guards,
// both saying no, and a comment above them saying yes — for as long as nothing
// tied the two together.
//
// A doc comment is the hard case of #232, because no test can observe it the
// way a test observes an exit code: it is not data the binary emits, it is text
// in a source file. What CAN be done is to read it back out of that file, parse
// it into the same shape the behaviour has, and compare the two. That is what
// this file does, and it is why the comparison is a set equality rather than a
// substring check: a substring check passes on a comment that lists a
// transition too many, which is precisely the defect that was there.
//
// The behaviour side is measured, not derived from models.GetValidTransitions.
// Deriving it would pin the comment to a second table that could itself drift
// from the command; driving the command over the whole 5x5 matrix pins it to
// the only thing that matters, which is what happens when an agent tries.
package commands

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// taskStatDocSourceFile and taskStatDocFunc locate the comment. Both are
// checked before anything is parsed, so a rename turns this gate red instead of
// letting it pass over a comment it never found.
const (
	taskStatDocSourceFile = "task_mutate.go"
	taskStatDocFunc       = "taskSetStatus"
	taskStatDocHeader     = "Valid manual status transitions (this command):"
)

// taskStatDoc returns the whole doc comment of taskSetStatus, with the comment
// markers stripped.
func taskStatDoc(t *testing.T) string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, taskStatDocSourceFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", taskStatDocSourceFile, err)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != taskStatDocFunc {
			continue
		}
		if fn.Doc == nil {
			t.Fatalf("%s has no doc comment; this gate exists to keep that comment true and cannot "+
				"do it against nothing", taskStatDocFunc)
		}
		return fn.Doc.Text()
	}

	t.Fatalf("%s declares no function named %s; this gate pins that function's doc comment and can no "+
		"longer find it", taskStatDocSourceFile, taskStatDocFunc)
	return ""
}

// documentedTransitions parses the bullet list under the header into the same
// shape the observation below produces.
//
// The grammar it accepts is exactly the one the comment is written in — a
// header line, then one "  - SOURCE → TARGET, TARGET" bullet per source state,
// terminated by the first line that is not a bullet. Every token is validated
// against models.IsValidTaskStatus, so a misspelt state fails the gate instead
// of silently dropping out of the comparison.
func documentedTransitions(t *testing.T, doc string) map[models.TaskStatus][]models.TaskStatus {
	t.Helper()

	lines := strings.Split(doc, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == taskStatDocHeader {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("the doc comment of %s no longer carries the header %q, so this gate cannot tell which "+
			"lines are the transition list:\n%s", taskStatDocFunc, taskStatDocHeader, doc)
	}

	out := map[models.TaskStatus][]models.TaskStatus{}
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			break
		}
		body := strings.TrimPrefix(trimmed, "- ")

		source, targets, found := strings.Cut(body, "→")
		if !found {
			t.Fatalf("transition bullet %q carries no → and cannot be read as a transition", body)
		}
		from := models.TaskStatus(strings.TrimSpace(source))
		if !models.IsValidTaskStatus(string(from)) {
			t.Fatalf("transition bullet %q names %q as a source state, which is not a task status",
				body, from)
		}
		if _, dup := out[from]; dup {
			t.Fatalf("the transition list gives %s two bullets; the comparison below would silently "+
				"honour only one", from)
		}

		for _, raw := range strings.Split(targets, ",") {
			// "BACKLOG (reopen)" — the parenthetical is a gloss, not a state.
			token := strings.TrimSpace(raw)
			if idx := strings.Index(token, " ("); idx >= 0 {
				token = token[:idx]
			}
			to := models.TaskStatus(token)
			if !models.IsValidTaskStatus(string(to)) {
				t.Fatalf("transition bullet %q names %q as a target state, which is not a task status",
					body, to)
			}
			out[from] = append(out[from], to)
		}
	}

	if len(out) == 0 {
		t.Fatalf("no transition bullets were found under %q; the comparison below would be vacuous",
			taskStatDocHeader)
	}
	return out
}

// statTargetFlags returns the flags `task stat` requires for a target state.
// They are mandatory and are checked before the transition guard runs, so an
// attempt made without them would be refused for the wrong reason and would be
// miscounted as a forbidden transition.
func statTargetFlags(target models.TaskStatus) []string {
	switch target {
	case models.StatusDoing:
		return []string{"--commit-open", backlogRouteCommitOpen}
	case models.StatusCompleted:
		return []string{"--commit-close", backlogRouteCommitClose}
	default:
		return nil
	}
}

// observedStatTransitions drives `task stat` over the whole source×target
// matrix and returns the transitions it accepted.
//
// Every attempt gets a task manufactured for it, so an attempt is never made
// against a state some earlier attempt moved. Every refusal is classified by
// its reason, and an unrecognised reason fails the run: a refusal this gate
// cannot account for is a refusal it must not quietly record as "forbidden".
func observedStatTransitions(t *testing.T, f *backlogRouteFixture) map[models.TaskStatus][]models.TaskStatus {
	t.Helper()

	accepted := map[models.TaskStatus][]models.TaskStatus{}
	sprintTargetRefusals := 0

	for _, source := range taskStatusOrder {
		for _, target := range taskStatusOrder {
			id := f.taskInState(t, source)
			err := f.tryStat(t, id, target, statTargetFlags(target)...)

			switch {
			case err == nil:
				if got := f.statusOf(t, id); got != target {
					t.Fatalf("task stat %s → %s returned no error but left task #%d in %s",
						source, target, id, got)
				}
				accepted[source] = append(accepted[source], target)

			case strings.Contains(err.Error(), "can only be set automatically"):
				if target != models.StatusSprint {
					t.Fatalf("task stat %s → %s was refused as an automatic-only target: %v",
						source, target, err)
				}
				sprintTargetRefusals++

			case strings.Contains(err.Error(), "invalid status transition"):
				if got := f.statusOf(t, id); got != source {
					t.Fatalf("task stat %s → %s was refused but moved task #%d to %s; a refusal must "+
						"leave the task untouched", source, target, id, got)
				}

			default:
				t.Fatalf("task stat %s → %s failed for a reason this gate cannot classify, so it cannot "+
					"say whether the transition is allowed: %v", source, target, err)
			}

			if !errors.Is(err, utils.ErrValidation) && err != nil {
				t.Fatalf("task stat %s → %s was refused without wrapping utils.ErrValidation, so it "+
					"would not land on exit code 6: %v", source, target, err)
			}
		}
	}

	// The SPRINT column is refused from every source, including SPRINT itself:
	// five refusals, none of them a transition verdict.
	if sprintTargetRefusals != len(taskStatusOrder) {
		t.Fatalf("the SPRINT target was refused as automatic-only %d times out of %d source states; "+
			"the doc comment's claim about it describes all of them",
			sprintTargetRefusals, len(taskStatusOrder))
	}

	return accepted
}

// TestTaskStatDocComment_ListsExactlyTheTransitionsAccepted is the gate the doc
// comment of taskSetStatus points at.
//
// It parses the comment's own transition list, drives `task stat` over every
// source/target pair, and requires the two to be the same set. The comment that
// listed DOING → SPRINT fails it: nothing in the matrix accepts that pair.
func TestTaskStatDocComment_ListsExactlyTheTransitionsAccepted(t *testing.T) {
	f := setupBacklogRouteRoadmap(t, "task-stat-doc-comment")

	doc := taskStatDoc(t)
	documented := documentedTransitions(t, doc)
	observed := observedStatTransitions(t, f)

	for _, source := range taskStatusOrder {
		want := sortedStatuses(observed[source])
		got := sortedStatuses(documented[source])
		if strings.Join(want, ",") == strings.Join(got, ",") {
			continue
		}
		t.Errorf("the doc comment of %s says %s → %s, but `task stat` accepted %s → %s",
			taskStatDocFunc, source, renderStatuses(got), source, renderStatuses(want))
	}

	// The sentence that explains the one target missing from the list above.
	// Every source refused it, so the comment must say so; the exit code it
	// names is the one utils.ErrValidation maps to, pinned in cmd/rmp.
	if !strings.Contains(doc, "SPRINT` is rejected with exit code 6") {
		t.Errorf("`task stat` refused the SPRINT target from every source state, but the doc comment of "+
			"%s no longer says it is rejected with exit code 6:\n%s", taskStatDocFunc, doc)
	}
}

// sortedStatuses renders a status list in a stable order for comparison.
func sortedStatuses(statuses []models.TaskStatus) []string {
	out := make([]string, len(statuses))
	for i, s := range statuses {
		out[i] = string(s)
	}
	sort.Strings(out)
	return out
}

func renderStatuses(names []string) string {
	if len(names) == 0 {
		return "(nothing)"
	}
	return strings.Join(names, ", ")
}
