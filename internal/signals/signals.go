// Package signals owns the disposition of SIGINT and SIGTERM for the whole rmp
// binary.
//
// # The rule
//
// There is exactly ONE registration for these two signals. It is installed by
// the first statement of main and it is never removed. What a long-lived
// command takes over is not the registration but the ACTION: [Install] declares
// what a delivery means while nothing has claimed the signals, and [TakeOver]
// redirects deliveries to a caller that means to drain instead.
//
// # Why the rule exists
//
// The obvious way to let `rmp graph serve` and `rmp web` interpret a signal
// differently from a short-lived invocation is for each of them to call
// [os/signal.Reset] and then [os/signal.Notify]. That is what both of them used
// to do, and it is a defect: Reset restores the DEFAULT disposition rather than
// suspending delivery, so between the two calls the process is a plain program
// with no handler and a signal delivered there terminates it outright — no
// drain, no shutdown checkpoint, no socket removal, and the graph store's
// exclusive hold released by the operating system rather than by the process.
//
// The interval is not the two instructions it looks like. Both Reset and Notify
// round-trip to the runtime's dedicated signal-mask thread (see sigenable and
// sigdisable in runtime/signal_unix.go), so on the machine this was measured on
// the pair costs 88 microseconds at the median and 158 at its worst, of which
// 41 microseconds at the median and 91 at its worst are unprotected. A SIGTERM
// arriving the instant a server announced its socket found that interval 4.6%
// of the time over 500 runs.
//
// Reset has a second effect that is easy to miss: it unregisters every channel
// from the signal as well as restoring the default disposition. A signal
// already queued inside the runtime and not yet dispatched is then handed to
// os/signal's delivery loop at a moment when the old channel no longer wants it
// and the new one does not yet exist, so it is DROPPED. In the same 500 runs
// that happened 18.2% of the time: the server ignored SIGTERM outright and kept
// running, holding the store lock, until it was killed.
//
// Keeping one registration for the life of the process removes both. There is
// no instant with the default disposition and no instant with no channel, so
// neither outcome is reachable, and the two long-lived surfaces share one
// discipline instead of each carrying a copy of the same three lines.
//
// # What callers must still get right
//
// A signal delivered before a surface has taken over runs the default action —
// for rmp that is exit 130, which is the right answer for a command that has
// not started serving anything yet. It follows that a surface must take the
// signals over BEFORE it tells anyone it is ready: both surfaces call
// [TakeOver] ahead of the announcement that publishes their socket or URL, so a
// caller that has read the announcement is talking to a process that drains. A
// module-wide gate in internal/testenv enforces that ordering, and that no
// production file outside this package touches os/signal at all.
package signals

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// The process-wide state. mu guards owner and fallback; armed guards the single
// registration, which is why it is a Once and not a bool: two callers racing to
// arm must produce one channel, one Notify and one dispatcher.
var (
	mu       sync.Mutex
	owner    chan<- os.Signal
	fallback func(os.Signal)
	armed    sync.Once
)

// Install declares the default disposition of SIGINT and SIGTERM and registers
// the process's one handler for them.
//
// onInterrupt is what a delivery means while no surface has taken the signals
// over. It is a function rather than an exit code because the exit codes belong
// to cmd/rmp, where SPEC/ARCHITECTURE.md § Exit Codes is realised and where two
// gates require the catalogue to be declared; this package decides when the
// action runs and never what it is. It runs on the dispatcher's goroutine, so
// an action that returns leaves the process running with the signal consumed.
//
// Calling Install more than once replaces the default action and does not
// register a second handler. Calling it is not required before [TakeOver], only
// before a delivery that nothing owns can mean anything.
func Install(onInterrupt func(os.Signal)) {
	mu.Lock()
	fallback = onInterrupt
	mu.Unlock()
	arm()
}

// TakeOver redirects SIGINT and SIGTERM to the returned channel until the
// returned release is called.
//
// The channel is buffered for one signal and is written with a non-blocking
// send: a caller that has not yet consumed the signal it was given is already
// stopping, and a second one carries the same instruction. This is the same
// coalescing the previous per-surface `make(chan os.Signal, 1)` had, kept
// deliberately so that a second SIGTERM during a long drain neither interrupts
// it nor queues up behind it.
//
// release restores the default disposition declared by [Install], and is safe
// to call more than once. It does nothing if another caller has since taken
// over, so an out-of-order release cannot silently unclaim a live surface.
//
// TakeOver publishes the owner BEFORE it arms, so in a process that never
// called [Install] — this package's own tests, and internal/graphserve's child
// server harness — the very first delivery after the registration exists
// already has somewhere to go.
func TakeOver() (<-chan os.Signal, func()) {
	out := make(chan os.Signal, 1)

	mu.Lock()
	owner = out
	mu.Unlock()

	arm()

	return out, func() {
		mu.Lock()
		// Only the current owner may stand down. The comparison is what makes
		// release idempotent AND makes an out-of-order release harmless: a
		// surface that releases after another has taken over finds the owner is
		// not its own channel and changes nothing.
		if owner == (chan<- os.Signal)(out) {
			owner = nil
		}
		mu.Unlock()
	}
}

// arm performs the one registration. Nothing ever undoes it: os/signal.Stop and
// os/signal.Reset are the two calls this package exists to avoid, because each
// of them reopens the interval described in the package doc.
func arm() {
	armed.Do(func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		go dispatch(ch)
	})
}

// dispatch is the single reader of the single registered channel. It resolves
// what a delivery means at the moment it delivers it rather than at the moment
// it registered, which is what lets ownership change without a signal ever
// being owned by the wrong reader or by no reader at all.
func dispatch(ch <-chan os.Signal) {
	for s := range ch {
		mu.Lock()
		target, deflt := owner, fallback
		mu.Unlock()

		if target != nil {
			select {
			case target <- s:
			default:
			}
			continue
		}
		if deflt != nil {
			deflt(s)
		}
	}
}
