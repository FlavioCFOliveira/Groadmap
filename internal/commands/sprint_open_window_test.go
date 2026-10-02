package commands

import (
	"context"
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestSprintLifecycle_LoserOfTheStatusWindowReceivesTheStatusRefusal pins
// SPEC/COMMANDS.md § Sprint Lifecycle deterministically, where
// TestSprintStart_ConcurrentLosersReceiveTheSequentialRefusal pins it only by
// chance: "the status check runs first", and "Concurrent invocations receive
// the refusals of the sequential case". A competing invocation commits the same
// transition on the same sprint after the loser has read the sprint's status
// and before it reads the OPEN sprint. The sequential case run in commit order
// is the winner and then the loser, and the loser's status check refuses it
// first: it exits 6 with the status line, never with "sprint #N is already
// open", and it changes nothing.
func TestSprintLifecycle_LoserOfTheStatusWindowReceivesTheStatusRefusal(t *testing.T) {
	cases := []struct {
		name string
		// prepare brings the sprint to the status the verb transitions from.
		prepare func(t *testing.T, roadmap string, sprint int)
		op      func([]string) error
		auditOp models.AuditOperation
		want    string
		// wantStatus is the status the winner leaves.
		wantStatus models.SprintStatus
	}{
		{
			name:       "start",
			prepare:    func(*testing.T, string, int) {},
			op:         sprintStart,
			auditOp:    models.OpSprintStart,
			want:       "validation error: cannot start sprint with status OPEN",
			wantStatus: models.SprintOpen,
		},
		{
			name: "reopen",
			prepare: func(t *testing.T, roadmap string, sprint int) {
				run(t, func() error { return sprintStart([]string{"-r", roadmap, itoa(sprint)}) })
				run(t, func() error { return sprintClose([]string{"-r", roadmap, itoa(sprint)}) })
			},
			op:         sprintReopen,
			auditOp:    models.OpSprintReopen,
			want:       "validation error: cannot reopen sprint with status OPEN",
			wantStatus: models.SprintOpen,
		},
		{
			// close does not read the OPEN sprint, but it runs through the
			// same window, and its loser is held to the same rule.
			name: "close",
			prepare: func(t *testing.T, roadmap string, sprint int) {
				run(t, func() error { return sprintStart([]string{"-r", roadmap, itoa(sprint)}) })
			},
			op:         sprintClose,
			auditOp:    models.OpSprintClose,
			want:       "validation error: cannot close sprint with status CLOSED",
			wantStatus: models.SprintClosed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roadmap := "sprint-status-window-" + tc.name
			database, cleanup := setupTestTaskRoadmap(t, roadmap)
			defer cleanup()

			sprint := createSprintViaCommand(t, roadmap, "Settlement reconciliation", "Match the card network settlement files to the ledger")
			tc.prepare(t, roadmap, sprint)
			args := []string{"-r", roadmap, itoa(sprint)}
			before := countCommentAudit(t, database, tc.auditOp, models.EntitySprint, sprint)

			fired := false
			afterSprintStatusRead = func() {
				// The competitor runs the same path; it must not re-enter.
				afterSprintStatusRead = nil
				fired = true
				if err := tc.op(args); err != nil {
					t.Errorf("competing %s: %v", tc.name, err)
				}
			}
			t.Cleanup(func() { afterSprintStatusRead = nil })

			err := tc.op(args)
			if !fired {
				t.Fatal("the hook did not run: the window was not exercised")
			}
			if err == nil {
				t.Fatalf("the loser of the window succeeded, want %q (exit 6)", tc.want)
			}
			if err.Error() != tc.want || !errors.Is(err, utils.ErrValidation) {
				t.Errorf("loser: %q (ErrValidation: %t), want %q (exit 6)", err.Error(), errors.Is(err, utils.ErrValidation), tc.want)
			}

			got, err := database.GetSprint(context.Background(), sprint)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("sprint status %s, want %s", got.Status, tc.wantStatus)
			}
			if n := countCommentAudit(t, database, tc.auditOp, models.EntitySprint, sprint) - before; n != 1 {
				t.Errorf("%d %s audit entries written in the window, want the winner's alone", n, tc.auditOp)
			}
		})
	}
}
