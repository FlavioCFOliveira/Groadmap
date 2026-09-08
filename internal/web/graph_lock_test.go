// Regression fence for the graph store lock the graph data endpoint no longer
// takes (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rules 6 and 7;
// Acceptance Criterion 163; SPEC/GRAPH.md § Lock Contention).
//
// # What this file used to assert, and why it now asserts the opposite
//
// The endpoint used to open the roadmap's store itself, under the store's
// exclusive advisory lock, and this file fenced that hold: that a request waited
// for a writer rather than failing fast, that the hold spanned the statement and
// the checkpoint, that two requests against one roadmap serialised, and that the
// lock file was the one artefact a read created.
//
// Every one of those is now false by construction, and the change is the point
// rather than an accident of it. **The endpoint opens no store**, so there is no
// hold to size, nothing to wait for, and nothing to serialise against. A
// dedicated graph server holds that lock for its whole PROCESS LIFETIME, and no
// finite wait can be sized against such a hold — which is what made the old
// arrangement fail deterministically the moment a server existed, not
// intermittently and not under load but on every request for as long as that
// server ran.
//
// So the assertions invert. What is fenced here is the NEGATIVE: no request takes
// the lock, no request waits on one another party holds, and no request leaves a
// lock file behind. The negative needs a fence precisely because it is invisible
// in a passing request — an endpoint that quietly took the lock again would serve
// every read correctly until the day a server was running.
package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// webGraphDir resolves the roadmap's graph store directory, the directory whose
// lock file a graph server takes and this endpoint does not.
func webGraphDir(t *testing.T, name string) string {
	t.Helper()
	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatalf("resolving roadmap dir: %v", err)
	}
	return filepath.Join(roadmapDir, "graph")
}

// TestHandleGraphData_TakesNoStoreLock is the inverted acceptance criterion 147.
//
// The exclusive lock is held for the whole request by another party, exactly as a
// running `rmp graph serve` holds it. The request must not wait for it and must
// not be refused because of it: it is answered promptly on its own terms — 503,
// because nothing is serving this roadmap — and the holder still holds the lock
// afterwards, which is what establishes that the request neither took it nor
// broke it.
//
// **Promptness is the assertion.** A request that waited would still answer 503
// in the end, so a status check alone would pass against the very defect this
// fences. The wall clock is what separates "did not take the lock" from "waited
// for it and gave up".
func TestHandleGraphData_TakesNoStoreLock(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)
	graphDir := webGraphDir(t, name)

	release, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		t.Fatalf("taking the store's exclusive lock: %v", err)
	}
	released := false
	defer func() {
		if !released {
			release()
		}
	}()

	start := time.Now()
	rec := doGraphData(t, name, nil)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503. The lock is held but nothing is SERVING this roadmap, so "+
			"the answer is the no-server one and the lock is beside the point; body=%q",
			rec.Code, rec.Body.String())
	}
	if elapsed >= graphlock.WaitBudget() {
		t.Errorf("the request took %v, reaching the %v lock wait budget. This process never takes "+
			"the store's lock, so no request waits on it and none is refused because of it "+
			"(SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 7)",
			elapsed, graphlock.WaitBudget())
	}

	// The holder still holds it, and releasing it leaves the lock free for the
	// next taker. A request that had taken and released the lock would leave this
	// passing too, so it is the promptness above that carries the weight; this
	// rules out the coarser failure of a request that stole or broke the hold.
	release()
	released = true
	regained, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		t.Fatalf("the lock could not be retaken after the request: %v. The request interfered "+
			"with a hold it should never have touched", err)
	}
	regained()
}

// TestHandleGraphData_ConcurrentRequestsDoNotSerialise is the inverted acceptance
// criterion 148.
//
// Two graph pages open on one roadmap used to serialise against each other on
// this side of the socket, because both opened the same store under the same
// exclusive lock. They do not any more: their statements run concurrently inside
// the server and are resolved by the store's MVCC, and a slow statement submitted
// through one query bar no longer delays another request against the same roadmap
// here (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 7).
//
// With no server running both requests are answered 503, and the assertion is
// that neither waited for the other: two requests issued back to back cost about
// what one costs, rather than one lock wait each.
func TestHandleGraphData_ConcurrentRequestsDoNotSerialise(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	done := make(chan time.Duration, 2)
	for range 2 {
		go func() {
			start := time.Now()
			rec := doGraphData(t, name, nil)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503; body=%q", rec.Code, rec.Body.String())
			}
			done <- time.Since(start)
		}()
	}
	for range 2 {
		if elapsed := <-done; elapsed >= graphlock.WaitBudget() {
			t.Errorf("a concurrent request took %v, reaching the %v lock wait budget. Two graph "+
				"requests against one roadmap no longer serialise on this side of the socket",
				elapsed, graphlock.WaitBudget())
		}
	}
}

// TestHandleGraphData_CreatesNoLockFile is the inverted acceptance criterion 149.
//
// The lock file used to be the ONE artefact a request that wrote nothing was
// allowed to create. It is now one more thing this process does not create,
// because creating it was a consequence of opening the store and the store is not
// opened. The read case and the write case are both driven, because the write
// case is the one an implementation that had quietly reopened the store would
// fail on hardest (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 6).
func TestHandleGraphData_CreatesNoLockFile(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)
	graphDir := webGraphDir(t, name)

	lockFile := filepath.Join(graphDir, graphlock.LockFileName)
	if err := os.Remove(lockFile); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clearing the lock file before the requests: %v", err)
	}

	for _, params := range []url.Values{
		nil,
		{"q": {`MATCH (n) RETURN n`}},
		{"q": {`CREATE (n:WebProbe {key:'p'})`}},
	} {
		if rec := doGraphData(t, name, params); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("q=%v answered %d, want 503; body=%q", params, rec.Code, rec.Body.String())
		}
		if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
			t.Fatalf("q=%v created %s (stat error %v). Taking the lock is a consequence of "+
				"opening the store, and this process opens none", params, lockFile, err)
		}
	}

	// And no checkpoint ran: a snapshot directory over a never-checkpointed store
	// would mean a statement had been executed here rather than in a server.
	if _, err := os.Stat(filepath.Join(graphDir, "snapshot")); !os.IsNotExist(err) {
		t.Errorf("a snapshot/ directory appeared after three requests this process did not "+
			"execute. Checkpointing is the graph server's business on its own cadence "+
			"(SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process): %v", err)
	}
}
