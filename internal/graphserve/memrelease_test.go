package graphserve

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

// boltMessage frames payload as one Bolt message split into chunks of at most
// chunk bytes, terminated by the zero-length chunk.
func boltMessage(payload []byte, chunk int) []byte {
	var out []byte
	for len(payload) > 0 {
		n := min(chunk, len(payload))
		out = append(out, byte(n>>8), byte(n))
		out = append(out, payload[:n]...)
		payload = payload[n:]
	}
	return append(out, 0, 0)
}

// summary builds a PackStream structure with tag and a body of raw bytes.
func summary(tag byte, body string) []byte {
	return append([]byte{0xB1, tag}, body...)
}

// TestSummaryScanner_FindsTheSummariesThatEndAStatement pins where SPEC/GRAPH.md
// § Statement-Scoped Memory Release observes the end of a statement: a FAILURE,
// an IGNORED, or a SUCCESS that is neither the RUN header (it lists fields)
// nor a PULL batch that leaves records behind (has_more: true) — and nothing
// else, however the server's writes split the stream.
func TestSummaryScanner_FindsTheSummariesThatEndAStatement(t *testing.T) {
	handshake := []byte{0, 0, 6, 5}
	runHeader := boltMessage(summary(boltTagSuccess, "\xa3\x86fields\x91\x81n\x85t_first\x00\x83qid\x00"), 4096)
	record := boltMessage(append([]byte{0xB1, 0x71, 0x91}, make([]byte, 70000)...), 65535)
	batch := boltMessage(summary(boltTagSuccess, "\xa1\x88has_more\xc3"), 4096)
	final := boltMessage(summary(boltTagSuccess, "\xa2\x88has_more\xc2\x84type\x81r"), 4096)
	failure := boltMessage(summary(boltTagFailure, "\xa2\x84code\x8bNeo.Timeout\x87message\x84late"), 3)
	ignored := boltMessage(summary(boltTagIgnored, ""), 4096)
	noop := []byte{0, 0}

	stream := [][]byte{handshake, runHeader, record, noop, batch, record, final, failure, ignored}
	var all []byte
	for _, part := range stream {
		all = append(all, part...)
	}

	// Whole stream at once, and then one byte per write: the count is the same.
	for _, step := range []int{len(all), 1, 7, 4096} {
		s := newSummaryScanner()
		ended := 0
		for i := 0; i < len(all); i += step {
			ended += s.feed(all[i:min(i+step, len(all))])
		}
		if ended != 3 {
			t.Errorf("writes of %d bytes: %d statement ends, want 3 (the final SUCCESS, the FAILURE, the IGNORED)", step, ended)
		}
	}
}

// countingReleaser returns a releaser whose heap reading is held, and that
// counts its releases.
func countingReleaser(held *uint64, mu *sync.Mutex, releases *int) *memoryReleaser {
	return &memoryReleaser{
		held: func() uint64 {
			mu.Lock()
			defer mu.Unlock()
			return *held
		},
		release: func() {
			mu.Lock()
			defer mu.Unlock()
			*releases++
			*held = 4 << 20 // what a collection leaves: the live heap
		},
	}
}

// TestMemoryReleaser_ReleasesWhatAStatementLeftBehind pins rules 3 and 4 of the
// section: the memory a statement grew the heap by is returned when it ends,
// and a statement that grew it by little ends without a collection.
func TestMemoryReleaser_ReleasesWhatAStatementLeftBehind(t *testing.T) {
	var mu sync.Mutex
	held, releases := uint64(4<<20), 0
	r := countingReleaser(&held, &mu, &releases)

	r.statementEnded() // the first statement grew nothing past the floor
	if releases != 0 {
		t.Errorf("%d releases for a statement that grew the heap by nothing", releases)
	}
	held = 1350 << 20 // a cut statement left 1.35 GB of freed heap
	r.statementEnded()
	if releases != 1 || held != 4<<20 {
		t.Errorf("after a heavy statement: %d releases, held %d; want one release back to the live heap", releases, held)
	}
	held += 2 << 20 // a small statement
	r.statementEnded()
	if releases != 1 {
		t.Errorf("a small statement triggered a collection")
	}
}

// TestMemoryReleaser_ConcurrentEndsDoNotStack: statements that end together
// produce no concurrent collections, and none of them is lost.
func TestMemoryReleaser_ConcurrentEndsDoNotStack(t *testing.T) {
	var mu sync.Mutex
	held, releases := uint64(0), 0
	inFlight, maxInFlight := 0, 0
	r := countingReleaser(&held, &mu, &releases)
	release := r.release
	r.release = func() {
		mu.Lock()
		inFlight++
		maxInFlight = max(maxInFlight, inFlight)
		mu.Unlock()
		release()
		mu.Lock()
		inFlight--
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			held += 64 << 20
			mu.Unlock()
			r.statementEnded()
		}()
	}
	wg.Wait()
	if maxInFlight != 1 {
		t.Errorf("%d collections ran at once, want 1", maxInFlight)
	}
	if releases == 0 {
		t.Fatal("no release ran")
	}
	// Whatever was owed when the last statement ended has been returned.
	if held > 4<<20 {
		t.Errorf("%d bytes still held after every statement ended, want the live heap", held)
	}
}

// TestDrainConn_EveryOutcomeReachesTheReleaser pins rule 4's property on the
// server's own connection type: a statement that ends with a summary — even one
// whose client has gone and that therefore cannot be delivered — and a
// connection that closes in the middle of one both reach the release.
func TestDrainConn_EveryOutcomeReachesTheReleaser(t *testing.T) {
	var mu sync.Mutex
	held, releases := uint64(0), 0
	l := newServerListener(nil)
	l.releaser = countingReleaser(&held, &mu, &releases)

	server, client := net.Pipe()
	conn := &drainConn{Conn: server, owner: l, summaries: newSummaryScanner()}
	l.conns[conn] = 0
	l.live.Add(1)
	go func() {
		buf := make([]byte, 1<<16)
		for {
			if _, err := client.Read(buf); err != nil {
				return
			}
		}
	}()

	owe := func() { mu.Lock(); held = 512 << 20; mu.Unlock() }

	owe()
	if _, err := conn.Write([]byte{0, 0, 6, 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(boltMessage(summary(boltTagFailure, "\xa0"), 4096)); err != nil {
		t.Fatal(err)
	}
	if releases != 1 {
		t.Fatalf("a FAILURE summary reached %d releases, want 1", releases)
	}

	// The client goes away; the next summary cannot be delivered and still
	// ends the statement.
	_ = client.Close()
	owe()
	_, _ = conn.Write(boltMessage(summary(boltTagSuccess, "\xa0"), 4096))
	if releases != 2 {
		t.Fatalf("an undeliverable summary reached %d releases, want 2", releases)
	}

	owe()
	_ = conn.Close()
	if releases != 3 {
		t.Errorf("a closing connection reached %d releases, want 3", releases)
	}
}

// fakeTimer is a scheduled settle pass a test runs by hand.
type fakeTimer struct {
	delay   time.Duration
	fn      func()
	stopped bool
}

func (f *fakeTimer) Stop() bool { f.stopped = true; return true }

// TestMemoryReleaser_SettlePassesReturnWhatTheEngineFreesAfterTheSummary is the
// regression test for the cut-write residue of rmp task #589: the release made
// as a statement's summary is written finds the engine still holding the
// statement's structures, which it drops in stages only after the write
// returns. Without the settle passes nothing returned that memory, and an idle
// server stayed at 1.5 GB. The passes run every 250 ms, at least four times and
// then while each still recovers memory, and stop cancels a pending one.
func TestMemoryReleaser_SettlePassesReturnWhatTheEngineFreesAfterTheSummary(t *testing.T) {
	var mu sync.Mutex
	// What the engine references at the summary, then at each later instant
	// as its connection goroutine unwinds (the measured stages, in MB).
	stages := []uint64{1460, 987, 794, 470, 290, 290, 290, 290, 290}
	stage, held, releases := 0, uint64(3280<<20), 0
	var timers []*fakeTimer
	r := &memoryReleaser{
		held: func() uint64 { mu.Lock(); defer mu.Unlock(); return held },
		release: func() {
			mu.Lock()
			defer mu.Unlock()
			releases++
			held = stages[min(stage, len(stages)-1)] << 20
			stage++
		},
		afterFunc: func(d time.Duration, f func()) stopper {
			ft := &fakeTimer{delay: d, fn: f}
			timers = append(timers, ft)
			return ft
		},
	}

	r.statementEnded()
	if releases != 1 || held != 1460<<20 {
		t.Fatalf("at the summary: %d releases, held %d MB", releases, held>>20)
	}
	for i := 0; i < len(timers); i++ {
		if timers[i].delay != settleInterval {
			t.Errorf("pass %d scheduled after %v, want %v", i, timers[i].delay, settleInterval)
		}
		timers[i].fn()
	}
	if held != 290<<20 {
		t.Errorf("after the settle passes the server holds %d MB, want the 290 MB still live", held>>20)
	}
	// 1460 -> 987 -> 794 -> 470 -> 290 recover memory, the fifth pass
	// recovers nothing and is the last.
	if len(timers) != 5 {
		t.Errorf("%d settle passes ran, want 5: four, and one more while the last recovered memory", len(timers))
	}

	// A new heavy statement restarts the passes; stop cancels the pending one
	// and refuses any later.
	mu.Lock()
	held = 3000 << 20
	mu.Unlock()
	r.statementEnded()
	pending := timers[len(timers)-1]
	r.stop()
	if !pending.stopped {
		t.Error("stop did not cancel the pending settle pass")
	}
	before := releases
	pending.fn()
	if releases != before {
		t.Error("a settle pass ran after stop")
	}
}

// TestFoldGate_AFoldThatRanReturnsItsMemory pins the second half of the
// residue of rmp task #589: the server's own in-flight fold serialises the
// whole graph, and that memory is garbage once the fold returns. An idle
// server triggers no collection, so without the hook the memory stayed
// resident for minutes (0.38 GB to 1.1 GB, measured). A fold that ran calls
// afterFold once; a fold that was not owed does not.
func TestFoldGate_AFoldThatRanReturnsItsMemory(t *testing.T) {
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

	folds := 0
	closer, _, err := build(st, graphDir, checkpointCadence{}, logger, func() { folds++ })
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
	if folds != 1 {
		t.Errorf("a fold that ran called afterFold %d times, want 1", folds)
	}
	ran, err = closer.gate.foldIfOwed()
	if err != nil || ran {
		t.Fatalf("a fold with nothing owed: ran %v, err %v", ran, err)
	}
	if folds != 1 {
		t.Errorf("a fold that did not run called afterFold")
	}
}
