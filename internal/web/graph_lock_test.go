// Regression fence for the graph data endpoint's store lock
// (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rules 5 and 6;
// Acceptance Criteria 147, 148, 149; SPEC/GRAPH.md § Lock Contention).
//
// The defect these close is that an unauthenticated GET on
// /roadmaps/{name}/graph/data opened the store with no lock at all, while
// opening the store is not a read-only operation on disk: GoGraph's recovery
// removes a stale snapshot.tmp staging directory and can promote snapshot.bak
// to snapshot. A request could therefore delete the staging directory an
// `rmp graph` write was assembling its snapshot in.
//
// Taking a lock is only half the fix. The other half is releasing it AT THE
// OPEN, so that serving a slow graph query never fails a concurrent CLI write —
// TestHandleGraphData_ReleasesTheLockAtTheOpen is what stops that hold being
// widened back over the query.
package web

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// webGraphDir resolves the roadmap's graph store directory, the directory whose
// lock file both the server and the CLI take.
func webGraphDir(t *testing.T, name string) string {
	t.Helper()
	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatalf("resolving roadmap dir: %v", err)
	}
	return filepath.Join(roadmapDir, "graph")
}

// TestHandleGraphData_WaitsForAWriterRatherThanFailingFast covers SPEC/WEB.md
// acceptance criterion 147: while an `rmp graph` write holds the store's
// exclusive lock, a graph data request for the same roadmap does NOT fail on the
// first collision. It waits, and is served once the writer releases.
//
// Failing fast here would make the graph page intermittently unavailable
// whenever anyone ran a write, which is why the reader waits where the writer
// does not (SPEC/GRAPH.md § Lock Contention).
func TestHandleGraphData_WaitsForAWriterRatherThanFailingFast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	release, err := graphlock.AcquireExclusive(webGraphDir(t, name))
	if err != nil {
		t.Fatalf("taking the exclusive lock: %v", err)
	}

	// Released well inside the reader's bounded wait, so the request is
	// genuinely contending when it starts.
	go func() {
		time.Sleep(250 * time.Millisecond)
		release()
	}()

	start := time.Now()
	rec := doGraphData(t, name, nil)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: a request must WAIT for an in-flight writer, not fail on "+
			"the first collision (SPEC/WEB.md acceptance criterion 147); body=%q", rec.Code, rec.Body.String())
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("the request completed in %v; it cannot have waited for the writer, so it did not "+
			"take the shared lock at all", elapsed)
	}
}

// TestHandleGraphData_LockExhaustionIsAnInternalError covers SPEC/WEB.md
// acceptance criterion 149: when the shared lock cannot be taken within the
// bounded wait, the request is answered HTTP 500 with the opaque error body
// every other 500 carries, accompanied by exactly one ERROR log record, and the
// server keeps serving other requests throughout.
//
// The 500 matters as much as the bound. A lock failure is an internal read
// failure, not a client mistake, so it must NOT be classified as one of the
// query-bar 400s: the page would then show the user a "your query failed"
// message for a condition the user's query had nothing to do with.
func TestHandleGraphData_LockExhaustionIsAnInternalError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	release, err := graphlock.AcquireExclusive(webGraphDir(t, name))
	if err != nil {
		t.Fatalf("taking the exclusive lock: %v", err)
	}

	buf := captureLog(t)

	done := make(chan *struct {
		code int
		body string
	}, 1)
	go func() {
		rec := doGraphData(t, name, nil)
		done <- &struct {
			code int
			body string
		}{rec.Code, rec.Body.String()}
	}()

	select {
	case got := <-done:
		release()
		if got.code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500: an exhausted lock wait is an internal read failure, "+
				"not a query-bar rejection; body=%q", got.code, got.body)
		}
		// The body is the opaque one every other 500 carries: it must not leak
		// the store path or the lock file name.
		if !strings.Contains(got.body, "internal server error") {
			t.Errorf("body = %q, want the opaque internal-error body", got.body)
		}
		if strings.Contains(got.body, "write.lock") || strings.Contains(got.body, ".roadmaps") {
			t.Errorf("body = %q leaks server-side detail", got.body)
		}
		record := oneRecord(t, buf)
		mustContainAll(t, record, "ERROR", "graph view load failed", name)
	case <-time.After(30 * time.Second):
		release()
		t.Fatal("the request never returned; the wait must be BOUNDED and end in a 500, never an " +
			"indefinite block that would hang the connection until the write timeout fired " +
			"(SPEC/WEB.md acceptance criterion 149)")
	}

	// The server keeps serving: the very next request is answered normally.
	next := doGraphData(t, name, nil)
	if next.Code != http.StatusOK {
		t.Fatalf("follow-up status = %d, want 200: one lock failure must not take the server "+
			"down; body=%q", next.Code, next.Body.String())
	}
}

// slowReadGraphNodes is the node count the three-way Cartesian product runs
// against in TestHandleGraphData_ReleasesTheLockAtTheOpen. The cost is cubic and
// measured rather than guessed: the neighbouring budget tests record 0.10 s at
// 100 nodes, 1.45 s at 252 and 6.04 s at 400. 252 buys a query lasting well over
// a second while the store open it must be distinguished from costs
// milliseconds — a margin of three orders of magnitude, which is what makes the
// observation below robust rather than a race.
const slowReadGraphNodes = 252

// TestHandleGraphData_ReleasesTheLockAtTheOpen covers SPEC/WEB.md acceptance
// criterion 148, the criterion that exists to stop the server's hold being
// widened back over the query: the shared lock is released when the open
// returns, so a concurrent `rmp graph` write can start, commit and checkpoint
// while the request is still executing its query.
//
// The observation is the exclusive lock becoming acquirable WHILE the request is
// still in flight. That is exactly what a CLI writer would find. If the hold
// were widened — a defer releaseLock(), or a release moved after the query — the
// acquisition would fail for the whole duration of the request and this test
// would go red, which is what should happen.
//
// Two assertions guard against the test passing for the wrong reason: the
// acquisition must happen while the request is unfinished (or it proves nothing
// about overlap), and the request must have taken meaningfully longer than the
// acquisition (or the query was not slow enough to distinguish a narrow hold
// from a wide one).
func TestHandleGraphData_ReleasesTheLockAtTheOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")

	seeds := graphSeedQueries()
	seeds = append(seeds, fmt.Sprintf(`UNWIND range(1,%d) AS i CREATE (:Bulk {i:i})`, slowReadGraphNodes-3))
	seedGraph(t, name, seeds...)

	// A budget large enough that the query runs to completion; the point here is
	// a long-running READ, not a cancelled one.
	setGraphQueryBudget(t, 60*time.Second)

	graphDir := webGraphDir(t, name)

	// Remove the lock file, so that its (re)appearance is an unambiguous,
	// monotonic signal that the request has reached the lock acquisition. This
	// is what makes the observation below race-free: polling the exclusive lock
	// from t=0 would succeed trivially while the request was still resolving the
	// roadmap and validating the query, long before it ever took the shared
	// lock, and the test would then pass no matter how wide the hold was.
	lockPath := filepath.Join(graphDir, graphlock.LockFileName)
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clearing the lock file before the request: %v", err)
	}

	type response struct {
		body string
		code int
	}
	done := make(chan response, 1)
	start := time.Now()
	go func() {
		rec := doGraphData(t, name, url.Values{"q": {expensiveGraphQuery}})
		done <- response{rec.Body.String(), rec.Code}
	}()

	// Phase one: wait until the request has reached the lock. Creating the lock
	// file is the first thing graphlock does and it never undoes it, so unlike
	// the lock state itself there is no window here that a poll can miss.
	reachedLock := false
	deadline := time.Now().Add(20 * time.Second)
	lockDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(lockDeadline) {
		if _, err := os.Stat(lockPath); err == nil {
			reachedLock = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !reachedLock {
		t.Fatal("the graph data request never created the store lock file, so it never took the " +
			"shared lock at all (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 5)")
	}

	// Phase two: from here on the request is demonstrably inside or past its
	// lock acquisition, so an exclusive acquisition that succeeds says something.
	// Under the correct narrow hold it succeeds as soon as the store open
	// returns, milliseconds in; under a hold that spans the query it cannot
	// succeed until the request has finished, which the done check catches.
	var (
		acquiredAt    time.Duration
		acquired      bool
		stillInFlight bool
	)
	for time.Now().Before(deadline) {
		select {
		case got := <-done:
			done <- got
			t.Fatalf("the request finished (status %d) before a writer could take the exclusive "+
				"lock. The server must hold the shared lock across the STORE OPEN ALONE and "+
				"release it as soon as the open returns (SPEC/WEB.md § Knowledge Graph from the "+
				"GoGraph Store, rule 5): a hold that spans the query blocks the CLI for the whole "+
				"duration of the request and buys no safety.", got.code)
		default:
		}

		release, err := graphlock.AcquireExclusive(graphDir)
		if err == nil {
			acquiredAt = time.Since(start)
			acquired = true
			select {
			case got := <-done:
				done <- got
			default:
				stillInFlight = true
			}
			release()
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	if !acquired {
		t.Fatal("a writer could not take the exclusive lock at any point while a graph data " +
			"request was in flight, within the deadline")
	}
	if !stillInFlight {
		t.Error("the lock became free only as the request finished, so the observation does not " +
			"distinguish a narrow hold from a hold spanning the whole request")
	}

	got := <-done
	total := time.Since(start)
	if got.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", got.code, got.body)
	}
	if total <= acquiredAt*2 {
		t.Errorf("the request took %v and the lock was free after %v; the query was not slow "+
			"enough for the gap to be meaningful", total, acquiredAt)
	}
	t.Logf("exclusive lock free after %v; request completed in %v (%d nodes)", acquiredAt, total, slowReadGraphNodes)
}

// TestHandleGraphData_LockFileIsTheOnlyArtefactCreated covers SPEC/WEB.md
// § Security and Constraints rule 4: the one artefact a graph read may create is
// the store's lock file, inside a graph/ directory that already exists. In
// particular a read must NOT create the graph directory for a roadmap that has
// none — that roadmap is an empty graph and is served as such — and must not
// checkpoint, so no snapshot/ appears.
func TestHandleGraphData_LockFileIsTheOnlyArtefactCreated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	graphDir := webGraphDir(t, name)

	// A roadmap with no graph directory: serving it must not create one.
	if rec := doGraphData(t, name, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a roadmap with no graph; body=%q", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(graphDir); !os.IsNotExist(err) {
		t.Fatalf("serving a roadmap with no graph created %s; the web interface must create no "+
			"graph store (SPEC/WEB.md § Security and Constraints rule 4): %v", graphDir, err)
	}

	// With a graph directory that exists, the lock file is the one thing a read
	// adds, and no checkpoint runs.
	seedGraph(t, name, graphSeedQueries()...)
	if err := os.Remove(filepath.Join(graphDir, graphlock.LockFileName)); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clearing the lock file before the read: %v", err)
	}

	if rec := doGraphData(t, name, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(graphDir, graphlock.LockFileName)); err != nil {
		t.Errorf("the read did not create the lock file it must take: %v", err)
	}
	if _, err := os.Stat(filepath.Join(graphDir, "snapshot")); !os.IsNotExist(err) {
		t.Errorf("a snapshot/ directory appeared after reads of a never-checkpointed store; the "+
			"web read path must never checkpoint (SPEC/WEB.md acceptance criterion 19): %v", err)
	}
}
