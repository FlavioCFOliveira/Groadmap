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
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
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

// TestHandleGraphData_TakesNoStoreLock is the NON-INTERFERENCE half of
// acceptance criterion 147.
//
// The exclusive lock is held for the whole request by another party, exactly as a
// running `rmp graph serve` holds it. The request must not be refused because of
// it: it is answered on its own terms — 503, because nothing is SERVING this
// roadmap — and the holder still holds the lock afterwards, which establishes
// that the request neither took it nor broke it.
//
// **The criterion's own proof is the served 200, and it lives next door.**
// Criterion 147 fixes a server running and the request answered 200 throughout,
// and says that the 200 is the assertion and is sufficient: the lock is
// exclusive and is held for the whole life of the server, so a request that
// contended for it would exhaust its bounded wait and fail rather than succeed at
// all. That case is
// TestLoadGraphView_ServedRoadmapNeitherWaitsForTheLockNorTakesIt in
// graph_served_test.go. The criterion forbids adding an assertion on elapsed
// time, which would establish nothing the 200 has not
// (SPEC/BUILD.md § No Benchmarks and No Performance-Measurement Tests), so what
// this test adds to that one is the part a success cannot show: the hold is
// intact afterwards and is still the same hold.
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

	rec := doGraphData(t, name, nil)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503. The lock is held but nothing is SERVING this roadmap, so "+
			"the answer is the no-server one and the lock is beside the point; body=%q",
			rec.Code, rec.Body.String())
	}

	// The request was answered while the lock was held, and answered as the
	// no-server condition rather than as a busy store: an implementation that
	// took this lock would have exhausted its bounded wait and produced the
	// store-failure answer instead (SPEC/WEB.md § Knowledge Graph from the
	// GoGraph Store, rule 7).
	if strings.Contains(rec.Body.String(), "busy") {
		t.Errorf("the request was answered %q, which reports the store as busy. This process never "+
			"takes the store's lock, so no request is refused because of one", rec.Body.String())
	}

	// The holder still holds it, and releasing it leaves the lock free for the
	// next taker. This is what rules out a request that stole or broke the hold.
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
// **Both requests being SERVED is the assertion.** Serialising on this side is
// what taking the store lock would produce, so the case is arranged as the
// criterion's own: a server answering on the socket, and the store's exclusive
// lock held throughout by a third party exactly as a running server holds it. A
// request that took that lock could only exhaust its bounded wait and fail; two
// that are both answered 200 therefore took no lock and waited on none of it, and
// no assertion on elapsed time is added because it would establish nothing the
// two answers have not (SPEC/BUILD.md § No Benchmarks and No
// Performance-Measurement Tests).
func TestHandleGraphData_ConcurrentRequestsDoNotSerialise(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "web-ui-rollout")
	graphDir := webGraphDir(t, name)
	if err := os.MkdirAll(graphDir, 0700); err != nil {
		t.Fatalf("creating %s: %v", graphDir, err)
	}

	// The hold a running server keeps for its whole process lifetime. It is NOT
	// released during the requests: that is the point.
	release, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		t.Fatalf("taking the store's exclusive lock: %v", err)
	}
	defer release()

	socket, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path: %v", err)
	}
	scriptedGraphServer(t, socket)

	const concurrent = 2
	done := make(chan int, concurrent)
	for range concurrent {
		go func() { done <- doGraphData(t, name, nil).Code }()
	}
	for range concurrent {
		if code := <-done; code != http.StatusOK {
			t.Errorf("a concurrent request was answered %d, want 200. The store's exclusive lock "+
				"was held throughout, so a request that contended for it could only have "+
				"exhausted its bounded wait of %v and failed: two requests against one roadmap "+
				"must not serialise on this side of the socket", code, graphlock.WaitBudget())
		}
	}
}

// TestHandleGraphData_CreatesNoLockFile is the lock-file half of acceptance criterion 163.
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
