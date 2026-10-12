package graphserve

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

// This file is SPEC/GRAPH.md § Acceptance Criteria, criteria 96 to 100: the
// statement-scoped memory release under overlap, and its gate. As the
// criteria's preamble requires, they run no Cypher statement and start no
// server. The heap is a model in which the memory the runtime holds is live
// memory plus freed memory, a release returns the freed memory and is counted,
// and the timer that paces the settle passes fires each scheduled call once,
// when the test chooses. Nothing is measured.

const mib = 1 << 20

// modelHeap is the criteria's model of the heap: held = live + freed.
type modelHeap struct {
	mu       sync.Mutex
	live     uint64
	freed    uint64
	releases int
}

// held is the memory the runtime holds and has not returned.
func (h *modelHeap) held() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.live + h.freed
}

// release returns the freed memory and counts the release.
func (h *modelHeap) release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.freed = 0
	h.releases++
}

// grow adds live memory: a statement accumulating.
func (h *modelHeap) grow(n uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.live += n
}

// free turns live memory into freed memory: a statement ending. The memory
// the runtime holds does not change.
func (h *modelHeap) free(n uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.live -= n
	h.freed += n
}

// state returns the freed memory and the release count.
func (h *modelHeap) state() (freed uint64, releases int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.freed, h.releases
}

// modelClock records every scheduled settle pass; the test fires them.
type modelClock struct {
	mu     sync.Mutex
	timers []*modelTimer
}

// modelTimer is one scheduled call, fired at most once.
type modelTimer struct {
	clock   *modelClock
	fn      func()
	delay   time.Duration
	stopped bool
	fired   bool
}

func (m *modelTimer) Stop() bool {
	m.clock.mu.Lock()
	defer m.clock.mu.Unlock()
	was := !m.stopped && !m.fired
	m.stopped = true
	return was
}

func (c *modelClock) afterFunc(d time.Duration, f func()) stopper {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &modelTimer{clock: c, fn: f, delay: d}
	c.timers = append(c.timers, t)
	return t
}

// scheduled reports how many calls have been scheduled so far.
func (c *modelClock) scheduled() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

// runAll fires, in scheduling order, every call not stopped and not yet
// fired, including the calls those calls schedule, and reports how many ran.
// A chain longer than any the releaser may schedule fails the test rather
// than hanging it.
func (c *modelClock) runAll(t *testing.T) int {
	t.Helper()
	ran := 0
	for i := 0; ; i++ {
		c.mu.Lock()
		if i >= len(c.timers) {
			c.mu.Unlock()
			return ran
		}
		tm := c.timers[i]
		due := !tm.stopped && !tm.fired
		if due {
			tm.fired = true
		}
		c.mu.Unlock()
		if !due {
			continue
		}
		if tm.delay != settleInterval {
			t.Errorf("a settle pass was scheduled after %v, want %v", tm.delay, settleInterval)
		}
		tm.fn()
		ran++
		if ran > 4*maxSettlePasses {
			t.Fatalf("%d settle passes ran from one drain, more than any release may schedule", ran)
		}
	}
}

// modelReleaser is a releaser over the model heap and the model clock.
func modelReleaser(h *modelHeap, c *modelClock) *memoryReleaser {
	return &memoryReleaser{held: h.held, release: h.release, afterFunc: c.afterFunc}
}

// pipeListener hands the server's listener the server ends of in-memory
// connections, so a test drives the real Accept.
type pipeListener struct{ conns chan net.Conn }

func (p pipeListener) Accept() (net.Conn, error) {
	c, ok := <-p.conns
	if !ok {
		return nil, net.ErrClosed
	}
	return c, nil
}

func (p pipeListener) Close() error   { return nil }
func (p pipeListener) Addr() net.Addr { return pipeAddr{} }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

// overlapServer is a listener whose releaser runs over the model.
type overlapServer struct {
	l     *serverListener
	p     pipeListener
	heap  *modelHeap
	clock *modelClock
}

// newOverlapServer starts the model at an idle server's live memory.
func newOverlapServer(idle uint64) *overlapServer {
	p := pipeListener{conns: make(chan net.Conn, 1)}
	s := &overlapServer{
		l:     newServerListener(p),
		p:     p,
		heap:  &modelHeap{live: idle},
		clock: &modelClock{},
	}
	s.l.releaser = modelReleaser(s.heap, s.clock)
	return s
}

// accept accepts one connection through the server's own Accept; its client
// end is drained so every write completes.
func (s *overlapServer) accept(t *testing.T) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, client) }()
	s.p.conns <- server
	c, err := s.l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = c.Close()
	})
	return c
}

// The server's bytes on a connection, as the engine writes them.
var (
	boltHandshake = []byte{0, 0, 6, 5}
	// boltRunHeader is a RUN's SUCCESS: it lists the result's fields, so the
	// statement has started and not ended.
	boltRunHeader = boltMessage(summary(boltTagSuccess, "\xa1\x86fields\x91\x81n"), 4096)
	// boltFinal is the PULL's final SUCCESS: the statement's outcome.
	boltFinal = boltMessage(summary(boltTagSuccess, "\xa1\x84type\x81r"), 4096)
)

func write(t *testing.T, c net.Conn, b ...[]byte) {
	t.Helper()
	var all []byte
	for _, part := range b {
		all = append(all, part...)
	}
	if _, err := c.Write(all); err != nil {
		t.Fatal(err)
	}
}

// overlapAWithB runs criterion 96's prologue: statement A starts on its own
// connection and grows its live memory by aGrowth; statement B starts on
// another, frees bGarbage, and ends while A is live; the release B's end makes
// and every pass it schedules run while A is still live. It returns A's
// connection.
func (s *overlapServer) overlapAWithB(t *testing.T, aGrowth, bGarbage uint64) net.Conn {
	t.Helper()
	a := s.accept(t)
	b := s.accept(t)
	write(t, a, boltHandshake)
	write(t, b, boltHandshake)

	write(t, a, boltRunHeader)
	s.heap.grow(aGrowth)

	write(t, b, boltRunHeader)
	s.heap.grow(bGarbage)
	s.heap.free(bGarbage)
	write(t, b, boltFinal)
	if _, releases := s.heap.state(); releases == 0 {
		t.Fatal("B's end made no release; the criterion needs one to overlap A")
	}
	if passes := s.clock.runAll(t); passes < minSettlePasses {
		t.Fatalf("B's release ran %d settle passes, want at least %d", passes, minSettlePasses)
	}
	return a
}

// TestCriterion96_OverlappedStatementIsReleasedAtItsEnd is criterion 96 and
// the regression test for rmp task #607: B's release, and its passes, ran
// while A was live, so A's live memory was in the baseline they left; when A
// ends, the heap does not grow at all. A releaser that judged A's end by the
// heap's growth kept A's memory for as long as the server stayed idle.
func TestCriterion96_OverlappedStatementIsReleasedAtItsEnd(t *testing.T) {
	const idle = 30 * mib
	s := newOverlapServer(idle)
	a := s.overlapAWithB(t, 2048*mib, 40*mib)

	_, before := s.heap.state()
	s.heap.free(2048 * mib)
	write(t, a, boltFinal)
	if _, after := s.heap.state(); after == before {
		t.Fatal("A's end made no release although B's release overlapped A")
	}
	s.clock.runAll(t)
	if freed, _ := s.heap.state(); freed != 0 {
		t.Errorf("after A's settle passes the model still holds %d MiB freed, want 0", freed/mib)
	}
}

// TestCriterion98_ClosedConnectionEndsTheStatement is criterion 98: A's
// connection closes while A is live and no outcome is written for A. The close
// ends A, and since B's release overlapped A, the close releases.
func TestCriterion98_ClosedConnectionEndsTheStatement(t *testing.T) {
	const idle = 30 * mib
	s := newOverlapServer(idle)
	a := s.overlapAWithB(t, 2048*mib, 40*mib)

	_, before := s.heap.state()
	s.heap.free(2048 * mib)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, after := s.heap.state(); after == before {
		t.Fatal("the close of A's connection made no release although B's release overlapped A")
	}
	s.clock.runAll(t)
	if freed, _ := s.heap.state(); freed != 0 {
		t.Errorf("after the close's settle passes the model still holds %d MiB freed, want 0", freed/mib)
	}
}

// releaseFor runs one statement to its end on c, growing and freeing garbage
// past the gate, so its end makes a release; then every pass that release
// schedules runs.
func (s *overlapServer) releaseFor(t *testing.T, c net.Conn, garbage uint64) {
	t.Helper()
	_, before := s.heap.state()
	write(t, c, boltRunHeader)
	s.heap.grow(garbage)
	s.heap.free(garbage)
	write(t, c, boltFinal)
	if _, after := s.heap.state(); after == before {
		t.Fatal("the other connection's statement made no release")
	}
	if passes := s.clock.runAll(t); passes < minSettlePasses {
		t.Fatalf("the release ran %d settle passes, want at least %d", passes, minSettlePasses)
	}
}

// endOwed ends, on c, a statement whose live memory is growth, written to the
// connection in first, and checks that its end releases and that, after the
// passes it schedules, the model holds no freed memory.
func (s *overlapServer) endOwed(t *testing.T, name string, c net.Conn, first []byte, growth uint64) {
	t.Helper()
	write(t, c, first)
	s.heap.grow(growth)
	_, before := s.heap.state()
	s.heap.free(growth)
	write(t, c, boltFinal)
	if _, after := s.heap.state(); after == before {
		t.Fatalf("%s's end made no release although a release ran after its start", name)
	}
	s.clock.runAll(t)
	if freed, _ := s.heap.state(); freed != 0 {
		t.Errorf("after %s's settle passes the model still holds %d bytes freed, want 0", name, freed)
	}
}

// TestCriterion97_EarlyStartIsHonoured is criterion 97. Each statement's own
// growth stays below the gate, so only its start can owe it a release: the
// start of A2 is the end of A1 on the same connection, and the start of the
// fresh connection's first statement is the connection's acceptance, and in
// both cases another connection's release, and every pass it scheduled, ran
// between that start and the statement's growth.
func TestCriterion97_EarlyStartIsHonoured(t *testing.T) {
	const idle = 30 * mib
	const small = 4 * mib // below the 16 MiB gate
	s := newOverlapServer(idle)

	// The start taken at the previous end on the connection.
	first := s.accept(t)
	other := s.accept(t)
	write(t, first, boltHandshake)
	write(t, other, boltHandshake)
	write(t, first, boltRunHeader)
	write(t, first, boltFinal) // A1 ends
	s.releaseFor(t, other, 64*mib)
	s.endOwed(t, "A2", first, boltRunHeader, small)

	// The start taken at the connection's acceptance: nothing is written on the
	// fresh connection until the release and its passes have run, and its first
	// write carries the handshake reply and its first statement's header.
	fresh := s.accept(t)
	third := s.accept(t)
	write(t, third, boltHandshake)
	s.releaseFor(t, third, 64*mib)
	s.endOwed(t, "the fresh connection's first statement", fresh,
		append(append([]byte{}, boltHandshake...), boltRunHeader...), small)
}

// foldScript is the memory ledger a fold reports to in criterion 99: it hands
// the fold's start and end to the real releaser, and plays the model's events
// at the instants the criterion places them — while the fold is live, and as
// it ends.
type foldScript struct {
	r       *memoryReleaser
	onStart func()
	onEnd   func()
}

func (f *foldScript) statementStarted() uint64 {
	start := f.r.statementStarted()
	f.onStart()
	return start
}

func (f *foldScript) statementEnded(start uint64) {
	f.onEnd()
	f.r.statementEnded(start)
}

// TestCriterion99_OverlappedFoldIsReleasedAtItsEnd is criterion 99: the
// server's own fold, run through its fold gate, starts and grows its live
// memory; a statement's release, and every pass it schedules, run while the
// fold is live; the fold ends, its memory becomes freed, and its end releases.
func TestCriterion99_OverlappedFoldIsReleasedAtItsEnd(t *testing.T) {
	const idle = 30 * mib
	const foldGrowth = 1024 * mib
	h := &modelHeap{live: idle}
	clock := &modelClock{}
	r := modelReleaser(h, clock)

	var before int
	script := &foldScript{
		r: r,
		onStart: func() {
			h.grow(foldGrowth)
			// Statement B: starts, frees its garbage, and ends while the fold
			// is live; its release and every pass after it run now.
			b := r.statementStarted()
			h.grow(40 * mib)
			h.free(40 * mib)
			r.statementEnded(b)
			if _, n := h.state(); n == 0 {
				t.Error("B's end made no release; the criterion needs one to overlap the fold")
			}
			if passes := clock.runAll(t); passes < minSettlePasses {
				t.Errorf("B's release ran %d settle passes, want at least %d", passes, minSettlePasses)
			}
		},
		onEnd: func() {
			_, before = h.state()
			h.free(foldGrowth)
		},
	}

	graphDir := filepath.Join(t.TempDir(), "graph")
	if err := os.MkdirAll(graphDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hold, err := graphstore.Acquire(graphDir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := hold.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	closer, _, err := build(st, graphDir, checkpointCadence{}, logger, script)
	t.Cleanup(func() { _ = closer.Close() })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Engine().RunInTxAny(context.Background(),
		"CREATE (:Component {key: 'internal/graphserve'})", nil); err != nil {
		t.Fatalf("writing the statement that makes a fold owed: %v", err)
	}

	ran, err := closer.gate.foldIfOwed()
	if err != nil || !ran {
		t.Fatalf("the owed fold: ran %v, err %v", ran, err)
	}
	if _, after := h.state(); after == before {
		t.Fatal("the fold's end made no release although B's release overlapped the fold")
	}
	clock.runAll(t)
	if freed, _ := h.state(); freed != 0 {
		t.Errorf("after the fold's settle passes the model still holds %d MiB freed, want 0", freed/mib)
	}
}

// TestCriterion100_GateDecidesANonOverlappedEnd is criterion 100: with no
// release between a statement's start and its end, and the baseline set by a
// release, a growth equal to the gate makes no release and schedules no pass,
// and a growth one byte past it makes one release and schedules the passes —
// for a baseline whose gate is the 16 MiB floor and for one whose gate is a
// quarter of it.
func TestCriterion100_GateDecidesANonOverlappedEnd(t *testing.T) {
	h := &modelHeap{}
	clock := &modelClock{}
	r := modelReleaser(h, clock)

	for _, tc := range []struct {
		name           string
		baseline, gate uint64
	}{
		{"floor", 32 * mib, releaseFloor},
		{"quarter", 1024 * mib, 256 * mib},
	} {
		// Set the baseline by a release: the live memory is the baseline, and
		// freed memory past any gate makes the end release.
		h.mu.Lock()
		h.live, h.freed = tc.baseline, 4096*mib
		h.mu.Unlock()
		r.statementEnded(r.statementStarted())
		clock.runAll(t)
		if r.baseline != tc.baseline {
			t.Fatalf("%s: the baseline is %d, want %d", tc.name, r.baseline, tc.baseline)
		}

		// Growth equal to the gate: no release, no pass.
		start := r.statementStarted()
		h.grow(tc.gate)
		h.free(tc.gate)
		_, releases := h.state()
		scheduled := clock.scheduled()
		r.statementEnded(start)
		if _, n := h.state(); n != releases {
			t.Errorf("%s: a growth equal to the gate made %d releases, want 0", tc.name, n-releases)
		}
		if n := clock.scheduled(); n != scheduled {
			t.Errorf("%s: a growth equal to the gate scheduled %d settle passes, want 0", tc.name, n-scheduled)
		}

		// One byte more: one release, and the passes.
		start = r.statementStarted()
		h.grow(1)
		h.free(1)
		r.statementEnded(start)
		if _, n := h.state(); n != releases+1 {
			t.Errorf("%s: a growth one byte past the gate made %d releases, want 1", tc.name, n-releases)
		}
		if n := clock.scheduled(); n != scheduled+1 {
			t.Errorf("%s: a growth one byte past the gate scheduled %d settle passes, want 1 (the first)", tc.name, n-scheduled)
		}
		clock.runAll(t)
	}
}
