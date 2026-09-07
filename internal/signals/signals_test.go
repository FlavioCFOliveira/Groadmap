package signals

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// These tests drive [dispatch] directly, over a channel the test writes to,
// rather than sending real signals to the test binary. The unit under test is
// the decision dispatch makes — owner, or default, and which one at which
// moment — and a real signal adds nothing to that while adding a dependency on
// the operating system's delivery timing. What a real signal DOES establish is
// tested in window_test.go, against child processes, because the property there
// is about the interval between two registrations and can only be observed from
// outside the process.

// resetPackageState returns the package to its pre-Install condition so each
// test starts from the same place. It cannot un-register a signal — nothing in
// this package ever does, which is the whole point — but no test here arms the
// real registration, so there is nothing to undo.
func resetPackageState(t *testing.T) {
	t.Helper()

	mu.Lock()
	owner = nil
	fallback = nil
	mu.Unlock()
}

// deliver sends one signal into a dispatcher started for this test and waits
// for the dispatcher to have finished handling it, so the assertions that
// follow are not racing the goroutine.
func deliver(t *testing.T, ch chan os.Signal, s os.Signal, handled <-chan struct{}) {
	t.Helper()

	ch <- s
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the dispatcher did not handle the delivery within 5s")
	}
}

func TestDispatchRunsTheDefaultActionWhenNothingHasTakenOver(t *testing.T) {
	resetPackageState(t)

	handled := make(chan struct{}, 1)
	var got os.Signal
	mu.Lock()
	fallback = func(s os.Signal) {
		got = s
		handled <- struct{}{}
	}
	mu.Unlock()

	ch := make(chan os.Signal, 1)
	go dispatch(ch)

	deliver(t, ch, syscall.SIGTERM, handled)

	if got != syscall.SIGTERM {
		t.Errorf("the default action received %v, want SIGTERM", got)
	}
}

func TestDispatchDeliversToTheOwnerAndNotToTheDefault(t *testing.T) {
	resetPackageState(t)

	defaultRan := make(chan os.Signal, 1)
	mu.Lock()
	fallback = func(s os.Signal) { defaultRan <- s }
	mu.Unlock()

	out, release := TakeOver()
	defer release()

	ch := make(chan os.Signal, 1)
	go dispatch(ch)
	ch <- syscall.SIGINT

	select {
	case s := <-out:
		if s != syscall.SIGINT {
			t.Errorf("the owner received %v, want SIGINT", s)
		}
	case s := <-defaultRan:
		t.Fatalf("the default action ran with %v while an owner was registered", s)
	case <-time.After(5 * time.Second):
		t.Fatal("the owner received nothing within 5s")
	}

	select {
	case s := <-defaultRan:
		t.Errorf("the default action also ran, with %v", s)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestReleaseRestoresTheDefaultAction(t *testing.T) {
	resetPackageState(t)

	handled := make(chan struct{}, 1)
	mu.Lock()
	fallback = func(os.Signal) { handled <- struct{}{} }
	mu.Unlock()

	_, release := TakeOver()
	release()
	release() // idempotent: a second call must neither panic nor undo anything

	ch := make(chan os.Signal, 1)
	go dispatch(ch)

	deliver(t, ch, syscall.SIGTERM, handled)
}

func TestASecondTakeOverReplacesTheFirstAndAStaleReleaseCannotUnclaimIt(t *testing.T) {
	resetPackageState(t)

	defaultRan := make(chan os.Signal, 1)
	mu.Lock()
	fallback = func(s os.Signal) { defaultRan <- s }
	mu.Unlock()

	first, releaseFirst := TakeOver()
	second, releaseSecond := TakeOver()
	defer releaseSecond()

	// The first owner's release runs AFTER the second took over. It must be a
	// no-op: an out-of-order release that cleared the owner would hand the
	// signal to the default action and exit 130 while a server was draining.
	releaseFirst()

	ch := make(chan os.Signal, 1)
	go dispatch(ch)
	ch <- syscall.SIGTERM

	select {
	case <-first:
		t.Fatal("the replaced owner received the signal, want the current owner")
	case s := <-defaultRan:
		t.Fatalf("the default action ran with %v after a stale release", s)
	case s := <-second:
		if s != syscall.SIGTERM {
			t.Errorf("the current owner received %v, want SIGTERM", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing received the signal within 5s")
	}
}

func TestTheOwnerChannelCoalescesASecondSignalRatherThanBlocking(t *testing.T) {
	resetPackageState(t)

	out, release := TakeOver()
	defer release()

	// An UNBUFFERED delivery channel, so each send returns only once the
	// dispatcher has taken the value. That is what makes the sequence below
	// ordered rather than hopeful: with a buffered channel the test could not
	// tell whether the dispatcher had reached its send yet.
	ch := make(chan os.Signal)
	go dispatch(ch)

	sendWithin(t, ch, syscall.SIGTERM, "the first signal")
	waitFor(t, func() bool { return len(out) == 1 }, "the owner to hold the first signal")

	// The owner has not consumed it. A second signal must neither block the
	// dispatcher nor queue up behind the first: a surface that has been told to
	// stop and has not yet read the instruction is already stopping, and a
	// blocked dispatcher would stop delivering to every later owner as well.
	sendWithin(t, ch, syscall.SIGTERM, "the second signal, with the owner's channel full")

	// A third send returns only after the dispatcher has come back round its
	// loop, which is after it finished handling the second. At that instant the
	// second has either been delivered or dropped, and nothing is in flight
	// that could still change the answer.
	sendWithin(t, ch, syscall.SIGTERM, "the third signal")

	if n := len(out); n != 1 {
		t.Errorf("the owner holds %d signals, want exactly 1: the send must coalesce", n)
	}
	if s := <-out; s != syscall.SIGTERM {
		t.Errorf("the owner received %v, want SIGTERM", s)
	}
}

// sendWithin fails rather than hangs when the dispatcher does not take the
// value, which is the failure this file exists to catch: a dispatcher blocked
// on one owner delivers to no later one either.
func sendWithin(t *testing.T, ch chan os.Signal, s os.Signal, what string) {
	t.Helper()

	select {
	case ch <- s:
	case <-time.After(5 * time.Second):
		t.Fatalf("the dispatcher did not take %s within 5s", what)
	}
}

// waitFor polls until cond holds, so a test can order itself against the
// dispatcher goroutine without sleeping for a duration somebody guessed.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after 5s waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestInstallReplacesTheDefaultAction(t *testing.T) {
	resetPackageState(t)

	// Install is idempotent in the registration and not in the action: calling
	// it twice must leave the second action in force, because a process that
	// re-declares what an interrupt means has changed its mind, not registered
	// a second handler. That the registration itself happens exactly once is a
	// property of the sync.Once in arm, and is pinned structurally by
	// TestOnlyOnePackageOwnsTheSignalDiscipline in internal/testenv, which
	// requires the one signal.Notify call to sit inside that Once. It is not
	// asserted here: reaching into the Once from a test would mean copying it,
	// which go vet correctly refuses.
	first := make(chan os.Signal, 1)
	second := make(chan os.Signal, 1)
	Install(func(s os.Signal) { first <- s })
	Install(func(s os.Signal) { second <- s })

	ch := make(chan os.Signal, 1)
	go dispatch(ch)
	ch <- syscall.SIGINT

	select {
	case <-first:
		t.Error("the replaced default action ran, want the most recently installed one")
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("no default action ran within 5s")
	}
}
