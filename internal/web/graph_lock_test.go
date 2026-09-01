// Regression fence for the graph data endpoint's store lock
// (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rules 5 and 6;
// Acceptance Criteria 147, 148, 149; SPEC/GRAPH.md § Lock Contention).
//
// The defect these close is that an unauthenticated GET on
// /roadmaps/{name}/graph/data opened the store with no lock at all, while
// opening the store is not a read-only operation on disk: GoGraph's recovery
// removes a stale snapshot.tmp staging directory and can promote snapshot.bak
// to snapshot. A request could therefore delete the staging directory an
// `rmp graph execute` write was assembling its snapshot in.
//
// **The hold used to span the store open ALONE, and one of the tests below used
// to fail if it were widened.** That test is now inverted, and the inversion is
// the point of rmp task #364 rather than an accident of it: the endpoint's
// statement may write, so the hold has to cover the open, the statement, the
// commit and the checkpoint — the same span `rmp graph execute` holds, for the
// same reason. There is one lock mode because there is one execution path, and
// nothing examines a statement to learn which it needs (SPEC/GRAPH.md
// § Concurrency and Recovery).
//
// The cost is real and is asserted rather than assumed: a slow statement
// submitted through the query bar blocks the CLI, and blocks a second graph
// page, for as long as it runs.
package web

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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

// TestHandleGraphData_HoldsTheLockAcrossTheStatement covers SPEC/WEB.md
// acceptance criterion 148: the hold spans the statement, so an
// `rmp graph execute` invocation issued while a slow graph data request is still
// executing WAITS for it rather than proceeding beside it.
//
// This test is the inversion of the one that stood here before. It used to
// assert the opposite — that the exclusive lock became acquirable WHILE the
// request was still in flight — because the endpoint was read-only and a hold
// spanning the query would have blocked the CLI for nothing. The endpoint's
// statement can now write and commit, so a hold released at the open would let a
// concurrent invocation load the graph, checkpoint a full snapshot of its own
// stale in-memory copy, and truncate the write-ahead log that still held this
// request's committed change: an acknowledged write lost in silence
// (SPEC/GRAPH.md § Concurrency and Recovery).
//
// The observation is an ORDERING and not a threshold, which is what keeps it
// robust on a slow machine: the acquisition must return no earlier than the
// moment the request completed. Under a hold released at the open it would
// return a whole statement's execution earlier, and under the correct hold it
// cannot return before the request has released. The second assertion — that the
// request took meaningfully longer than a store open — is what makes the
// ordering worth observing at all.
func TestHandleGraphData_HoldsTheLockAcrossTheStatement(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")

	seeds := graphSeedQueries()
	seeds = append(seeds, fmt.Sprintf(`UNWIND range(1,%d) AS i CREATE (:Bulk {i:i})`, slowReadGraphNodes-3))
	seedGraph(t, name, seeds...)

	// A budget large enough that the statement runs to completion; the point
	// here is a long-running statement, not a cancelled one.
	setGraphQueryBudget(t, 60*time.Second)

	graphDir := webGraphDir(t, name)

	// Remove the lock file, so that its (re)appearance is an unambiguous,
	// monotonic signal that the request has reached the lock acquisition. This
	// is what makes the observation below race-free: acquiring from t=0 would
	// succeed trivially while the request was still resolving the roadmap and
	// validating the limit, long before it took the lock at all.
	lockPath := filepath.Join(graphDir, graphlock.LockFileName)
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clearing the lock file before the request: %v", err)
	}

	type response struct {
		finishedAt time.Time
		body       string
		code       int
	}
	done := make(chan response, 1)
	start := time.Now()
	go func() {
		rec := doGraphData(t, name, url.Values{"q": {expensiveGraphQuery}})
		done <- response{time.Now(), rec.Body.String(), rec.Code}
	}()

	// Phase one: wait until the request has reached the lock. Creating the lock
	// file is the first thing graphlock does and it never undoes it, so unlike
	// the lock state itself there is no window here that a poll can miss.
	reachedLock := false
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
			"lock at all (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 5)")
	}

	// Phase two: a writer contends, exactly as `rmp graph execute` would. It
	// waits under the project's bounded policy and is served once the request
	// releases.
	release, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		got := <-done
		t.Fatalf("a writer contending with a graph data request exhausted its bounded wait after "+
			"%v (the request itself took %v): %v", time.Since(start), got.finishedAt.Sub(start), err)
	}
	acquiredAt := time.Now()
	release()

	got := <-done
	if got.code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", got.code, got.body)
	}
	requestDuration := got.finishedAt.Sub(start)

	// The ordering: the writer was served only after the request released.
	// A small tolerance absorbs the scheduling gap between the request's own
	// release and the moment it recorded its completion.
	if acquiredAt.Before(got.finishedAt.Add(-5 * time.Millisecond)) {
		t.Fatalf("a writer took the exclusive lock %v before the graph data request finished "+
			"(request took %v). The hold MUST span the open, the statement, the commit and the "+
			"checkpoint (SPEC/WEB.md acceptance criterion 148): a hold released at the open lets a "+
			"concurrent invocation checkpoint a stale graph and truncate the log that held this "+
			"request's committed change", got.finishedAt.Sub(acquiredAt), requestDuration)
	}
	if requestDuration < 200*time.Millisecond {
		t.Fatalf("the request completed in %v, which is not meaningfully longer than a store open; "+
			"the ordering above cannot distinguish a hold spanning the statement from one released "+
			"at the open", requestDuration)
	}
	t.Logf("request took %v; a contending writer was served %v after it finished",
		requestDuration, acquiredAt.Sub(got.finishedAt))
}

// TestHandleGraphData_ConcurrentRequestsSerialise is the other half of
// acceptance criterion 148: two graph data requests against one roadmap do not
// overlap, and both are served.
//
// The two statements WRITE, and each read-back afterwards is what makes the
// serialisation matter rather than merely be observed: two overlapping writers
// would each checkpoint a full snapshot of its own in-memory graph, and the
// second would truncate the log that still held the first's committed change.
// Both nodes being present at the end is the property the lock exists to
// deliver.
func TestHandleGraphData_ConcurrentRequestsSerialise(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	seedGraph(t, name, graphSeedQueries()...)

	codes := make(chan int, 2)
	for _, key := range []string{"concurrent-a", "concurrent-b"} {
		go func() {
			rec := doGraphData(t, name, url.Values{"q": {`CREATE (n:WebProbe {key:'` + key + `'})`}})
			codes <- rec.Code
		}()
	}
	for i := 0; i < 2; i++ {
		if code := <-codes; code != http.StatusOK {
			t.Errorf("concurrent request %d: status = %d, want 200: both must be served, one after "+
				"the other (SPEC/WEB.md acceptance criterion 148)", i, code)
		}
	}

	keys := nodeKeys(t, doGraphData(t, name, nil))
	for _, want := range []string{"concurrent-a", "concurrent-b"} {
		if !slices.Contains(keys, want) {
			t.Errorf("%q is absent after two concurrent writes (%v): an overlapping writer "+
				"checkpointed a stale graph and truncated the log that held the other's change", want, keys)
		}
	}
}

// TestHandleGraphData_LockFileIsTheOnlyArtefactCreated covers SPEC/WEB.md
// § Security and Constraints rule 4: the one artefact a statement that writes
// nothing may create is the store's lock file, inside a graph/ directory that
// already exists. In particular the endpoint must NOT create the graph directory
// for a roadmap that has none — that roadmap is an empty graph and is served as
// such, even when the statement submitted would have written — and a statement
// whose transaction appended nothing must not checkpoint, so no snapshot/
// appears.
func TestHandleGraphData_LockFileIsTheOnlyArtefactCreated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	name := seedRoadmap(t, "web-ui-rollout")
	graphDir := webGraphDir(t, name)

	// A roadmap with no graph directory: serving it must not create one, and
	// that holds for a statement that would have written as much as for a read.
	for _, params := range []url.Values{nil, {"q": {`CREATE (n:WebProbe {key:'p'})`}}} {
		if rec := doGraphData(t, name, params); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 for a roadmap with no graph; body=%q", rec.Code, rec.Body.String())
		}
		if _, err := os.Stat(graphDir); !os.IsNotExist(err) {
			t.Fatalf("serving a roadmap with no graph created %s; the web interface must create no "+
				"graph store (SPEC/WEB.md § Security and Constraints rule 4): %v", graphDir, err)
		}
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
		t.Errorf("a snapshot/ directory appeared after statements that wrote nothing, over a "+
			"never-checkpointed store; the checkpoint is gated on the write-ahead log having grown "+
			"(SPEC/WEB.md acceptance criterion 19): %v", err)
	}
}
