package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestSprintStart_ConcurrentLosersReceiveTheSequentialRefusal pins
// SPEC/COMMANDS.md § Sprint Lifecycle, "Concurrent invocations receive the
// refusals of the sequential case": of several `sprint start` and
// `sprint reopen` invocations racing to open a sprint, at most one succeeds,
// and every other one exits 6 with the line the sequential case prints for the
// state the winner left — never the SQLite driver's text for the violation of
// idx_one_open_sprint, and never exit 1 (SPEC/ARCHITECTURE.md § Classification
// of Database Driver Failures, rule 1).
func TestSprintStart_ConcurrentLosersReceiveTheSequentialRefusal(t *testing.T) {
	for round := 0; round < 5; round++ {
		roadmap := fmt.Sprintf("sprint-open-race-%d", round)
		database, cleanup := setupTestTaskRoadmap(t, roadmap)

		titles := []string{"Refund safety", "Dispute workflow", "Invoice performance", "Ledger exports"}
		sprints := make([]int, 0, len(titles))
		for _, title := range titles {
			sprints = append(sprints, createSprintViaCommand(t, roadmap, title, "Quarterly payments work: "+title))
		}
		// One CLOSED sprint, so `sprint reopen` races too.
		closed := createSprintViaCommand(t, roadmap, "First quarter hardening", "Close the January audit findings")
		run(t, func() error { return sprintStart([]string{"-r", roadmap, itoa(closed)}) })
		run(t, func() error { return sprintClose([]string{"-r", roadmap, itoa(closed)}) })

		type attempt struct {
			op     func([]string) error
			sprint int
			verb   string
		}
		attempts := []attempt{
			{sprintStart, sprints[0], "start"}, {sprintStart, sprints[0], "start"},
			{sprintStart, sprints[1], "start"}, {sprintStart, sprints[2], "start"},
			{sprintStart, sprints[3], "start"}, {sprintReopen, closed, "reopen"},
		}
		errs := make([]error, len(attempts))
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, a := range attempts {
			wg.Add(1)
			go func(i int, a attempt) {
				defer wg.Done()
				<-start
				errs[i] = a.op([]string{"-r", roadmap, itoa(a.sprint)})
			}(i, a)
		}
		close(start)
		wg.Wait()

		winner := -1
		for i, err := range errs {
			if err == nil {
				if winner != -1 {
					t.Fatalf("round %d: two invocations opened a sprint", round)
				}
				winner = attempts[i].sprint
			}
		}
		if winner == -1 {
			t.Fatalf("round %d: no invocation opened a sprint: %v", round, errs)
		}
		for i, err := range errs {
			if err == nil {
				continue
			}
			a := attempts[i]
			want := fmt.Sprintf("validation error: sprint #%d is already open — close it first", winner)
			if a.sprint == winner {
				want = fmt.Sprintf("validation error: cannot %s sprint with status OPEN", a.verb)
			}
			if err.Error() != want || !errors.Is(err, utils.ErrValidation) {
				t.Errorf("round %d: %s of sprint %d: %q, want %q (exit 6)", round, a.verb, a.sprint, err.Error(), want)
			}
			if strings.Contains(err.Error(), "constraint") {
				t.Errorf("round %d: the driver's text reached the caller: %v", round, err)
			}
		}

		open, err := database.GetOpenSprint(context.Background())
		if err != nil || open.ID != winner {
			t.Errorf("round %d: the OPEN sprint is %v (%v), want %d", round, open, err, winner)
		}
		entries, err := database.GetAuditEntries(context.Background(), &db.AuditFilter{Limit: 500})
		if err != nil {
			t.Fatal(err)
		}
		opened := 0
		for _, e := range entries {
			if e.Operation == string(models.OpSprintStart) || e.Operation == string(models.OpSprintReopen) {
				opened++
			}
		}
		// The fixture's own start of the CLOSED sprint accounts for one.
		if opened != 2 {
			t.Errorf("round %d: %d sprint-opening audit entries, want the fixture's and the winner's", round, opened)
		}
		cleanup()
	}
}
