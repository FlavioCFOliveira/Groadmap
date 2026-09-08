// Regression fence for what `rmp graph client` does about the graph store's
// exclusive lock, which is now NOTHING
// (SPEC/GRAPH.md § Concurrency and Recovery; § Lock Contention;
// SPEC/GRAPH.md § The Bolt Client).
//
// # What this file used to assert, and why none of it can be asserted any more
//
// The store lock once had two modes, chosen from the subcommand's operation
// class: `graph create`, `graph update` and `graph delete` took an exclusive hold
// spanning the whole open, commit, checkpoint and truncation sequence, while
// `graph query` and `graph search` took a SHARED hold spanning the store open
// alone. Those five collapsed onto one subcommand that ran any statement, the
// operation class went with them, and one exclusive mode replaced the two —
// held by the invocation, for the length of the invocation, under a bounded
// wait when another invocation held it.
//
// That is gone too, and this time the lock did not change hands: THE CLI STOPPED
// TAKING IT. `rmp graph client` sends a statement to a running server and opens
// no store, so it takes no lock, and there is nothing left for it to contend
// with. The lock is held by `rmp graph serve`, once, for that process's whole
// lifetime (SPEC/GRAPH.md § Server Startup, step 2).
//
// # The two tests that are retired here, and where their subject now lives
//
//   - TestGraphExecute_WaitsForTheLockRatherThanFailingFast asserted that an
//     invocation which met a held lock waited for it instead of failing. There is
//     no invocation that takes the lock, so there is no invocation that can wait
//     for one. The bounded wait it exercised still exists and is still tested,
//     against the package that owns it: internal/graphlock's own suite drives the
//     ladder and the budget directly, and internal/graphserve's
//     TestLockRefusal_RewordsOnlyTheExhaustedWait covers the one surface that
//     still meets a held lock — a second `rmp graph serve` for a roadmap that
//     already has one.
//   - TestGraphExecute_FailsAfterTheBoundedWait asserted that an exhausted wait
//     surfaced as utils.ErrGraphStore and exit code 1 rather than hanging. Same
//     reason, same replacement: the exhausted wait is now reachable only through
//     `rmp graph serve`, where internal/graphserve asserts both the sentinel and
//     the reworded line.
//
// # What is asserted instead
//
// The inverse property, which is the one the collapse bought and the one a later
// change could silently undo: a client statement runs against a roadmap whose
// lock is held for the whole of the request, and does not wait for it. It is the
// command-line twin of internal/web's TestHandleGraphData_TakesNoStoreLock, and
// the two exist for the same reason — a surface that fell back to opening the
// store would wait its entire wait budget against a hold that has no upper bound,
// and would then fail, which is exactly the state resolution exists to prevent
// (SPEC/GRAPH.md § Server Resolution, rule 2).
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
// resolveGraphDir does.
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

// TestGraphClient_RunsAgainstAHeldLockWithoutWaitingForIt is the property that
// replaced lock contention on this surface.
//
// The server holds the store's exclusive lock for its whole lifetime, so the
// lock IS held for the whole of every statement below — this test does not have
// to arrange that, only to prove it. Three things are asserted and each is
// needed:
//
//   - the lock really is held, established by trying to take it and failing.
//     Without this the rest would pass against a server that had crashed;
//   - the statement succeeds, and produces the value its own Cypher names, so a
//     client that answered from nowhere would be caught;
//   - it completes well inside the lock's wait budget, because an implementation
//     that fell back to opening the store would spend that budget in full before
//     failing, and the elapsed time is the only thing that separates the two from
//     outside.
//
// Both statement kinds are driven. A read and a write take the same route now —
// the server does not examine the statement — and asserting only one would leave
// a reintroduced write-side store open unnoticed.
func TestGraphClient_RunsAgainstAHeldLockWithoutWaitingForIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		roadmap string
		query   string
		want    string
	}{
		{
			name:    "a statement that writes nothing",
			roadmap: "graph-client-held-lock-read",
			query:   "MATCH (s:Spec) RETURN s.key",
			want:    "lock-contention",
		},
		{
			name:    "a statement that writes",
			roadmap: "graph-client-held-lock-write",
			query:   "CREATE (c:Component {key:'written-under-contention'}) RETURN c.key",
			want:    "written-under-contention",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roadmap := tc.roadmap
			defer servedRoadmap(t, roadmap)()

			captureStdStreams(t, func() {
				if err := runGraphClient([]string{"-r", roadmap, "--query",
					"CREATE (s:Spec {key:'lock-contention'})"}); err != nil {
					t.Fatalf("seeding the graph: %v", err)
				}
			})

			// The server's hold, observed rather than assumed. AcquireExclusive
			// waits the bounded wait and then reports a busy store, which is what
			// a hold with no upper bound looks like to anyone who tries to take
			// it — and is precisely the outcome this command must never reach.
			graphDir := testGraphDir(t, roadmap)
			if release, err := graphlock.AcquireExclusive(graphDir); err == nil {
				release()
				t.Fatalf("the exclusive lock on %s was free while a server was running for %q. "+
					"Every assertion below would then be about an unheld lock", graphDir, roadmap)
			}

			start := time.Now()
			stdout, _ := captureStdStreams(t, func() {
				if runErr := runGraphClient([]string{"-r", roadmap, "--query", tc.query}); runErr != nil {
					t.Errorf("a client statement must run in the server that holds the lock, not "+
						"contend for it (SPEC/GRAPH.md § The Bolt Client): %v", runErr)
				}
			})
			elapsed := time.Since(start)

			if !strings.Contains(stdout, tc.want) {
				t.Errorf("the invocation returned %q, want it to contain %q", stdout, tc.want)
			}
			if elapsed >= graphlock.WaitBudget() {
				t.Errorf("the invocation took %v, reaching the lock's %v wait budget. A client "+
					"takes no lock and waits for none; an invocation that spent the budget was "+
					"contending for the store rather than speaking to the server",
					elapsed, graphlock.WaitBudget())
			}
		})
	}
}

// TestGraphClient_CreatesNoLockFileOfItsOwn is the structural half of the same
// property, and it is what catches a fall back that happened to be fast.
//
// A roadmap with no server has no graph directory at all, because nothing but
// `rmp graph serve` creates one. A refused client invocation must leave it that
// way: no directory, and therefore no lock file inside one
// (SPEC/GRAPH.md § Server Startup, step 1; § The Bolt Client).
func TestGraphClient_CreatesNoLockFileOfItsOwn(t *testing.T) {
	const roadmap = "graph-client-creates-no-lock"
	defer setupTestGraphRoadmap(t, roadmap)()

	var err error
	captureStdStreams(t, func() {
		err = runGraphClient([]string{"-r", roadmap, "--query", "MATCH (s:Spec) RETURN s.key"})
	})
	if err == nil {
		t.Fatal("a client statement succeeded against a roadmap nothing is serving; there is no " +
			"direct path left for it to have taken")
	}
	if !errors.Is(err, utils.ErrGraphServer) {
		t.Errorf("err = %v, want it to wrap utils.ErrGraphServer: nothing was listening, which is a "+
			"failure of the SERVER's absence rather than of the store", err)
	}

	if graphDirExists(t, roadmap) {
		t.Errorf("%s exists after a refused client invocation. The client opens no store, so it "+
			"creates no directory and writes no lock file", graphDirOf(t, roadmap))
	}
	if size := fileSize(t, filepath.Join(testGraphDir(t, roadmap), graphlock.LockFileName)); size != 0 {
		t.Errorf("a lock file of %d bytes exists after a refused client invocation", size)
	}
}
