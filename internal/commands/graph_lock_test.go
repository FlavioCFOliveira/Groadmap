// Regression fence for the graph store's single lock mode
// (SPEC/GRAPH.md § Concurrency and Recovery; § Lock Contention; acceptance
// criteria 19 and 20).
//
// What this file used to be, and why it is not that any more. The store lock had
// two modes because the subcommand's operation class told the CLI, before it ran
// anything, whether an invocation would write: `graph create`, `graph update` and
// `graph delete` took an exclusive hold spanning the whole open, commit,
// checkpoint and truncation sequence, while `graph query` and `graph search` took
// a SHARED hold spanning the store open ALONE. The narrowness of that reader hold
// was load-bearing, and this file was the tripwire for the anti-widening clause
// that protected it.
//
// The five subcommands collapsed onto `rmp graph execute`, and the operation
// class went with them. Groadmap does not examine a statement, so it cannot learn
// whether one will write, and a lock mode chosen on that guess would be a shared
// hold released while a statement was still to commit. There is ONE mode now,
// exclusive, held across the whole sequence, and ONE contention policy, the
// bounded wait.
//
// Two tests that used to live here are gone rather than adapted, and it is worth
// recording which:
//
//   - the one asserting that the store is not read after the open. It was the
//     premise the narrow reader hold rested on; nothing rests on it now, and the
//     specification no longer states it.
//   - the one asserting that a write issued during an in-flight read SUCCEEDS.
//     That property is now the opposite of the contract: acceptance criterion 19
//     requires the second invocation to wait, "whether or not either statement
//     writes". Keeping the test would have been asserting a defect.
//
// What is asserted instead is the cost the collapse imposes, stated in the
// specification rather than discovered later: two statements against the same
// roadmap serialise even when neither of them writes.
package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// testGraphDir resolves the roadmap's graph store directory the same way
// openGraphStore does.
func testGraphDir(t *testing.T, roadmap string) string {
	t.Helper()
	roadmapDir, err := utils.GetRoadmapDir(roadmap)
	if err != nil {
		t.Fatalf("resolving roadmap directory for %q: %v", roadmap, err)
	}
	return filepath.Join(roadmapDir, "graph")
}

// fileSize returns the size of path, or 0 when it does not exist.
func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Size()
}

// lockHoldRelease is how long the test holds the store lock before releasing it.
// It must be comfortably longer than the backoff ladder's first delay, so that
// an invocation which failed on the first collision is distinguishable from one
// that waited, and comfortably shorter than the bounded wait, so that the
// invocation genuinely succeeds rather than exhausting its budget.
const lockHoldRelease = 250 * time.Millisecond

// waitedAtLeast is the floor an invocation's elapsed time must clear for the
// test to conclude that it contended for the lock at all. It sits below
// lockHoldRelease with room for a coarse timer.
const waitedAtLeast = 100 * time.Millisecond

// TestGraphExecute_WaitsForTheLockRatherThanFailingFast covers SPEC/GRAPH.md
// acceptance criterion 19: an invocation that finds the exclusive lock held does
// not fail on the first collision — it waits, and succeeds once the holder
// releases.
//
// **Both statement kinds are asserted, and the non-writing one is the point.**
// Criterion 19 says the property holds "whether or not either statement writes",
// and it says so because that is exactly what the single lock mode costs: a
// statement that changes nothing used to take a shared hold and overlap freely,
// and now serialises. A test that only exercised a writing statement would pass
// unchanged against an implementation that quietly reintroduced a read path, and
// would therefore assert nothing about the mode that was chosen.
//
// The holder is the lock itself rather than a second `rmp graph execute`, because
// the test must control exactly when it is released.
func TestGraphExecute_WaitsForTheLockRatherThanFailingFast(t *testing.T) {
	for _, tc := range []struct {
		name    string
		roadmap string
		query   string
		want    string
	}{
		{
			name:    "a statement that writes nothing",
			roadmap: "graph-execute-waits-read",
			query:   "MATCH (s:Spec) RETURN s.key",
			want:    "lock-contention",
		},
		{
			name:    "a statement that writes",
			roadmap: "graph-execute-waits-write",
			query:   "CREATE (c:Component {key:'written-under-contention'}) RETURN c.key",
			want:    "written-under-contention",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roadmap := tc.roadmap
			defer setupTestGraphRoadmap(t, roadmap)()

			captureStdStreams(t, func() {
				if err := runGraphExecute([]string{"-r", roadmap, "--query",
					"CREATE (s:Spec {key:'lock-contention'})"}); err != nil {
					t.Fatalf("seeding the graph: %v", err)
				}
			})
			graphDir := testGraphDir(t, roadmap)

			release, err := graphlock.AcquireExclusive(graphDir)
			if err != nil {
				t.Fatalf("taking the exclusive lock: %v", err)
			}

			// Released well inside the invocation's bounded wait, on a
			// goroutine, so the invocation is genuinely contending when it
			// starts.
			go func() {
				time.Sleep(lockHoldRelease)
				release()
			}()

			start := time.Now()
			stdout, _ := captureStdStreams(t, func() {
				if runErr := runGraphExecute([]string{"-r", roadmap, "--query", tc.query}); runErr != nil {
					t.Errorf("an invocation must WAIT for a current holder, not fail on the first "+
						"collision (SPEC/GRAPH.md § Lock Contention rule 1): %v", runErr)
				}
			})
			elapsed := time.Since(start)

			if !strings.Contains(stdout, tc.want) {
				t.Errorf("the invocation returned %q, want it to contain %q", stdout, tc.want)
			}
			if elapsed < waitedAtLeast {
				t.Errorf("the invocation completed in %v, less than the %v the lock was held for; "+
					"it cannot have waited, so it did not take the exclusive lock at all",
					elapsed, lockHoldRelease)
			}
		})
	}
}

// TestGraphExecute_FailsAfterTheBoundedWait covers SPEC/GRAPH.md acceptance
// criterion 20 for the CLI half: an invocation that cannot take the lock within
// the bounded wait exits 1 rather than hanging. utils.ErrDatabase is the
// sentinel the exit-code mapping turns into 1.
func TestGraphExecute_FailsAfterTheBoundedWait(t *testing.T) {
	const roadmap = "graph-execute-bounded-wait"
	defer setupTestGraphRoadmap(t, roadmap)()

	captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"CREATE (s:Spec {key:'bounded-wait'})"}); err != nil {
			t.Fatalf("seeding the graph: %v", err)
		}
	})
	graphDir := testGraphDir(t, roadmap)

	release, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		t.Fatalf("taking the exclusive lock: %v", err)
	}
	defer release()

	done := make(chan error, 1)
	go func() {
		_, _ = captureStdStreams(t, func() {
			done <- runGraphExecute([]string{"-r", roadmap, "--query", "MATCH (s:Spec) RETURN s.key"})
		})
	}()

	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("the invocation succeeded while another holder held the exclusive lock")
		}
		if !errors.Is(runErr, utils.ErrDatabase) {
			t.Errorf("an exhausted wait must surface as utils.ErrDatabase (exit 1), got: %v", runErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the invocation never returned; the wait must be BOUNDED and end in a failure, " +
			"never an indefinite block (SPEC/GRAPH.md § Lock Contention rule 2)")
	}
}
