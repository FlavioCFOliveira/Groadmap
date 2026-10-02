package graphserve

import (
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"sync/atomic"
	"time"
)

// This file is SPEC/GRAPH.md § Statement-Scoped Memory Release: a graph server
// returns a statement's memory to the operating system when the statement
// ends, on every outcome, so that an idle server does not stay resident at the
// high-water mark of the heaviest statement it has run.
//
// # Why the server has to do it at all
//
// A statement the time budget cuts can leave gigabytes of freed heap behind:
// measured on rmp task #589 against this server, one cut read over 600 nodes
// (`MATCH (a),(b),(c) WITH a.i AS x, b.i AS y, c.i AS z ORDER BY x RETURN
// count(*)`) left the process at 1.35 GB resident for as long as it was
// observed, against a 29 MB baseline. The engine drops its references when the
// statement ends; the Go runtime keeps the freed spans mapped and resident until
// a later collection and its background scavenger return them, and an idle
// server triggers neither.
//
// # Where "the statement ends" is observed
//
// The engine's server exposes no hook for the end of a statement, so the end is
// observed where this package already instruments every connection: on the
// bytes the server writes ([drainConn.Write]). A statement ends when the server
// has written the summary that concludes it — a FAILURE or IGNORED, or a
// SUCCESS that is neither the RUN header (it carries `fields`) nor an
// intermediate PULL batch (it carries `has_more: true`). [summaryScanner] reads
// the Bolt framing of the outgoing stream to find those summaries, and costs
// nothing per byte for the payload of every other message: it skips whole
// chunks of a RECORD.
//
// # What the release costs, and the gate that bounds it
//
// Returning freed memory to the operating system is a full collection followed
// by a scavenge (debug.FreeOSMemory). It is worth that only when there is
// something to return, so [memoryReleaser] first reads, without stopping the
// world, how much heap the runtime holds — the bytes in objects, free, and
// unused spans, all still mapped — and releases only when that has grown past
// the figure the last release left by more than releaseFloor or a quarter of
// that figure, whichever is larger. A statement that allocated little ends
// without a collection; one that grew the heap ends with one. The proportional
// term keeps a stream of small statements beside one heavy statement still in
// flight from collecting at every summary: only the memory of the statements
// that ended is owed back, and the in-flight one's is its own (rule 3).

// releaseFloor is the least growth of the runtime's heap, since the last
// release, that a statement's end returns to the operating system. Below it a
// full collection costs more than the memory it could return.
const releaseFloor = 16 << 20

// heapSampleNames are the runtime/metrics samples whose sum is the heap the
// runtime holds mapped and has not returned: live and dead objects, free
// spans not yet released, and the unused part of in-use spans.
var heapSampleNames = [...]string{
	"/memory/classes/heap/objects:bytes",
	"/memory/classes/heap/free:bytes",
	"/memory/classes/heap/unused:bytes",
}

// readHeldHeap returns the heap bytes the runtime holds and has not released
// to the operating system. runtime/metrics.Read does not stop the world.
func readHeldHeap() uint64 {
	var samples [len(heapSampleNames)]metrics.Sample
	for i, name := range heapSampleNames {
		samples[i].Name = name
	}
	metrics.Read(samples[:])
	var total uint64
	for i := range samples {
		if samples[i].Value.Kind() == metrics.KindUint64 {
			total += samples[i].Value.Uint64()
		}
	}
	return total
}

// memoryReleaser returns freed heap to the operating system when a statement
// ends. One exists per server, owned by its listener; every connection reports
// the statements it ends to it.
//
// Releases are serialised and coalesced: a statement that ends while a release
// is running marks one as pending, and the release in progress runs again once
// it is done, so the memory of a statement that ended during a collection is
// still returned and two collections never run at the same time.
type memoryReleaser struct {
	// held reads the heap the runtime holds; release returns the free part of
	// it to the operating system. Production uses readHeldHeap and
	// debug.FreeOSMemory; they are fields so a test can count releases.
	held    func() uint64
	release func()

	// afterFunc schedules a settle pass; production uses time.AfterFunc. It is
	// a field so a test can run the passes without waiting for them.
	afterFunc func(time.Duration, func()) stopper

	// settle is the pending settle pass, or nil; settleStep counts the passes
	// run since the release that scheduled them; stopped is set by stop. All three,
	// and baseline, are touched only under mu.
	settle     stopper
	settleStep int

	// baseline is what held read right after the last release.
	baseline uint64

	mu      sync.Mutex
	pending atomic.Bool
	stopped bool
}

// stopper is a scheduled call that can be cancelled; *time.Timer is one.
type stopper interface{ Stop() bool }

// settleInterval, minSettlePasses and maxSettlePasses shape the settle
// passes: after a release a statement's end made, the releaser returns freed
// memory again every settleInterval, whatever the heap reading says — at least
// minSettlePasses times, then for as long as the last pass recovered at least
// releaseFloor, and never more than maxSettlePasses times.
//
// They exist because the end of a statement, as this package observes it, is
// the moment the server WRITES the summary, and the engine still holds the
// statement's structures at that moment: it drops them in stages as its
// connection goroutine unwinds, after the write has returned. Measured on rmp
// task #589 with a cut `MATCH (a),(b),(c) CREATE ()` over 600 nodes: the
// release made at the summary found 1.46 GB still live, and passes after it
// read 987, 794, 470 and then 290 MB, flat from about 1.6 s on — 1.2 GB that
// became free only after the release and that nothing else returned, because
// an idle server ends no further statement. Each pass is a full collection,
// and passes are scheduled only after a release the gate admitted, so a stream
// of small statements never pays for one.
const (
	settleInterval  = 250 * time.Millisecond
	minSettlePasses = 4
	maxSettlePasses = 8
)

// newMemoryReleaser returns the production releaser.
func newMemoryReleaser() *memoryReleaser {
	return &memoryReleaser{
		held:    readHeldHeap,
		release: debug.FreeOSMemory,
		afterFunc: func(d time.Duration, f func()) stopper {
			return time.AfterFunc(d, f)
		},
	}
}

// stop cancels a pending settle pass and refuses every later one. The
// listener calls it when it closes, so no pass outlives the server.
func (r *memoryReleaser) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopped = true
	if r.settle != nil {
		r.settle.Stop()
		r.settle = nil
	}
}

// scheduleSettle (re)starts the settle passes from the first one. Callers
// hold mu.
func (r *memoryReleaser) scheduleSettle() {
	if r.stopped || r.afterFunc == nil {
		return
	}
	if r.settle != nil {
		r.settle.Stop()
	}
	r.settleStep = 0
	r.settle = r.afterFunc(settleInterval, r.settlePass)
}

// settlePass returns freed memory to the operating system and schedules the
// next pass, if any.
func (r *memoryReleaser) settlePass() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	before := r.held()
	r.release()
	r.baseline = r.held()
	r.settleStep++
	gained := before > r.baseline && before-r.baseline >= releaseFloor
	if r.settleStep < maxSettlePasses && (r.settleStep < minSettlePasses || gained) {
		r.settle = r.afterFunc(settleInterval, r.settlePass)
		return
	}
	r.settle = nil
}

// statementEnded records that a statement has ended and, unless another
// goroutine is already releasing, returns the memory owed now. It runs on the
// goroutine of the connection that wrote the statement's summary, after the
// write has completed, so the client already holds its answer.
func (r *memoryReleaser) statementEnded() {
	r.pending.Store(true)
	for {
		if !r.mu.TryLock() {
			// The goroutine releasing will see the pending mark.
			return
		}
		for r.pending.Swap(false) {
			r.releaseIfOwed()
		}
		r.mu.Unlock()
		// A statement that ended between the last Swap and the Unlock found
		// the lock taken and left its mark for this goroutine.
		if !r.pending.Load() {
			return
		}
	}
}

// releaseIfOwed returns freed memory to the operating system when the heap the
// runtime holds has grown past the gate since the last release. Callers hold mu.
func (r *memoryReleaser) releaseIfOwed() {
	gate := r.baseline / 4
	if gate < releaseFloor {
		gate = releaseFloor
	}
	if r.held() <= r.baseline+gate {
		return
	}
	r.release()
	r.baseline = r.held()
	r.scheduleSettle()
}

// Bolt message tags the scanner recognises (Bolt protocol, server messages).
const (
	boltTagSuccess = 0x70
	boltTagIgnored = 0x7E
	boltTagFailure = 0x7F
)

// boltServerHandshakeBytes is the length of the server's half of the Bolt
// handshake: the one version it agreed, four bytes, written before any chunked
// message.
const boltServerHandshakeBytes = 4

// Two key spellings a SUCCESS summary can carry that mean the statement has
// NOT ended: the RUN header lists the result's `fields`, and a PULL batch that
// leaves records behind carries `has_more: true`. Each is written as its
// PackStream encoding: a tiny-string marker (0x80 | length), the key, and for
// has_more the boolean true marker 0xC3.
var (
	patternFields  = []byte("\x86fields")
	patternHasMore = []byte("\x88has_more\xc3")
)

// summaryScanner reads the bytes one connection's server writes and counts the
// summaries that end a statement. It is fed in write order by the
// connection's single writer and needs no lock.
//
// The stream is the Bolt framing: the handshake reply, then messages, each a
// sequence of chunks — a two-byte big-endian length followed by that many
// bytes — terminated by a chunk of length zero. A zero-length chunk outside a
// message is a NOOP keep-alive. The first two bytes of a message are its
// PackStream structure marker and its tag.
type summaryScanner struct {
	// handshake is how many bytes of the handshake reply are still to come.
	handshake int
	// headerLen counts the bytes of a partially received chunk header.
	headerLen int
	// chunkLeft is how many payload bytes of the current chunk are still to
	// come.
	chunkLeft int
	// msgBytes counts the payload bytes of the current message seen so far.
	msgBytes int
	// fieldsAt and hasMoreAt are how far each pattern has matched inside the
	// current SUCCESS.
	fieldsAt, hasMoreAt int
	// header holds a partially received chunk header.
	header [2]byte
	// inMessage reports whether a message is open.
	inMessage bool
	// tag is the current message's tag, read from its second payload byte.
	tag byte
	// sawFields and sawHasMore report a complete match of each pattern.
	sawFields, sawHasMore bool
}

// newSummaryScanner returns a scanner positioned before the handshake reply.
func newSummaryScanner() *summaryScanner {
	return &summaryScanner{handshake: boltServerHandshakeBytes}
}

// feed consumes the bytes of one completed write and returns how many
// statement-ending summaries they completed.
func (s *summaryScanner) feed(b []byte) int {
	ended := 0
	for len(b) > 0 {
		switch {
		case s.handshake > 0:
			n := min(s.handshake, len(b))
			s.handshake -= n
			b = b[n:]

		case s.chunkLeft > 0:
			n := min(s.chunkLeft, len(b))
			s.scanPayload(b[:n])
			s.chunkLeft -= n
			b = b[n:]

		default:
			s.header[s.headerLen] = b[0]
			s.headerLen++
			b = b[1:]
			if s.headerLen < len(s.header) {
				continue
			}
			s.headerLen = 0
			size := int(s.header[0])<<8 | int(s.header[1])
			if size > 0 {
				if !s.inMessage {
					s.inMessage = true
					s.msgBytes, s.tag = 0, 0
					s.fieldsAt, s.hasMoreAt = 0, 0
					s.sawFields, s.sawHasMore = false, false
				}
				s.chunkLeft = size
				continue
			}
			// A zero-length chunk ends the open message, or is a NOOP.
			if s.inMessage {
				s.inMessage = false
				if s.endsStatement() {
					ended++
				}
			}
		}
	}
	return ended
}

// scanPayload reads one span of the current message's payload. Only the tag
// and, for a SUCCESS, the two key patterns are of interest; the payload of any
// other message is skipped without being examined.
func (s *summaryScanner) scanPayload(p []byte) {
	if s.msgBytes < 2 {
		take := min(2-s.msgBytes, len(p))
		if s.msgBytes+take == 2 {
			s.tag = p[take-1]
		}
		s.msgBytes += take
		p = p[take:]
	}
	if s.tag != boltTagSuccess {
		s.msgBytes += len(p)
		return
	}
	for _, c := range p {
		s.fieldsAt, s.sawFields = advance(patternFields, s.fieldsAt, c, s.sawFields)
		s.hasMoreAt, s.sawHasMore = advance(patternHasMore, s.hasMoreAt, c, s.sawHasMore)
	}
	s.msgBytes += len(p)
}

// advance moves a match of pattern by one byte. Neither pattern's first byte
// recurs inside it, so a mismatch restarts the match at that byte alone.
func advance(pattern []byte, at int, c byte, seen bool) (int, bool) {
	if seen {
		return at, true
	}
	if c == pattern[at] {
		at++
		if at == len(pattern) {
			return 0, true
		}
		return at, false
	}
	if c == pattern[0] {
		return 1, false
	}
	return 0, false
}

// endsStatement reports whether the message just closed is a summary that ends
// a statement.
func (s *summaryScanner) endsStatement() bool {
	switch s.tag {
	case boltTagFailure, boltTagIgnored:
		return true
	case boltTagSuccess:
		return !s.sawFields && !s.sawHasMore
	}
	return false
}
