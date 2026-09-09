// Package graphserve — what a SIGKILL actually costs a running server, and the
// pin that keeps this package's published account of it honest.
//
// # The defect this file is the regression for
//
// Three failure messages and doc comments in this package told an operator that
// SIGKILL "skips the shutdown checkpoint, the socket removal and the lock
// release". The last clause was false, and had always been false. The store's
// exclusive hold is an advisory lock taken with flock(2) and held by the OPEN
// FILE DESCRIPTION, so the kernel drops it when the process dies, however it
// dies (internal/graphlock/graphlock_unix.go; SPEC/GRAPH.md § Server Shutdown
// and the Drain, which lists the released lock among the two things a kill does
// NOT cost, and § Concurrency and Recovery). A published string that asserts
// something the system does not do is the same defect class as rmp tasks #419
// and #423, and it is the operator who pays for it: told the lock is stranded,
// the reasonable next action is to hunt for a lock to clear before restarting,
// and there is none.
//
// # Why the regression is shaped this way
//
// A prose rule alone would only be one more string to trust. What is asserted
// here is a MEASUREMENT first and the prose second:
// [TestKilledServer_StrandsNoStoreLock] kills a real server that really holds
// the lock, takes the lock afterwards to prove the kernel released it, and only
// then requires the one published cost list to agree with what it just observed.
// The cost list is a single constant so that there is one thing to pin, and
// [TestSigkillCostList_IsWrittenOnce] is what stops a second, unpinned copy
// being written beside it — the two mutations fail different tests by design:
// re-wording the constant to name the lock fails the first, hand-writing the
// list somewhere else fails the second.
package graphserve

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
)

// sigkillSkips is the ONE place this package words what a SIGKILL costs a graph
// server. Every failure message that has to name that cost interpolates it, so
// the account an operator reads is the same wherever they meet it and there is a
// single thing for [TestKilledServer_StrandsNoStoreLock] to hold to the truth.
//
// It names the two steps of SPEC/GRAPH.md § Server Shutdown and the Drain that a
// kill really does skip in the situation both its readers are in — a shutdown
// that has already drained and is now held open by a goroutine that will not
// come back — namely the shutdown checkpoint of step 4 and the socket removal of
// step 6. A kill delivered EARLIER also skips the drain of step 2, which is why
// the specification's own list is longer than this one; a message that needed
// that case would name it and would not reuse this constant. What no wording of
// it may ever name is the store lock, and that omission is asserted rather than
// left to good intentions.
const sigkillSkips = "which skips the shutdown checkpoint and the socket removal"

// sigkillCostListPhrase is the opening of that list, and it is what
// [TestSigkillCostList_IsWrittenOnce] counts. It is keyed on the list's own
// wording rather than on the word "lock", so a sentence that discusses the lock
// truthfully — as this file's own comments do — is not a false positive, and a
// second hand-written copy of the list is not a false negative.
const sigkillCostListPhrase = "skips the shutdown checkpoint"

// TestKilledServer_StrandsNoStoreLock is the measurement, and the prose pin that
// rests on it.
//
// # What each half establishes
//
// The acquisition BEFORE the kill is the non-vacuity check, and it is the half
// without which the rest asserts nothing: a lock that was never held is trivially
// available afterwards. It costs the whole bounded wait, because that is what a
// contender that never gets the lock spends (internal/graphlock.WaitBudget), and
// the refusal it ends in must be ErrBusy — the one failure that means the lock
// was HELD, as opposed to a lock file that could not be opened at all.
//
// The acquisition AFTER the kill is the claim. Nothing in this project releases
// that lock on the way out of a kill, because nothing runs on the way out of a
// kill; the kernel releases it when the file description is destroyed with the
// process. That the acquisition succeeds is therefore an observation of the
// operating system's behaviour on this platform, which is exactly what the
// published account depends on and exactly what no amount of reading the source
// establishes.
//
// The kill is asserted to BE a kill, through the same helper the durability tests
// use, because a child that happened to exit cleanly here would release its lock
// through the orderly path and make every assertion below a test of a graceful
// shutdown wearing a kill's name.
func TestKilledServer_StrandsNoStoreLock(t *testing.T) {
	root := graphRoot(t)
	server := startServerProcess(t, root, "locked")

	if release, err := graphlock.AcquireExclusive(server.graphDir); err == nil {
		release()
		t.Fatalf("the store lock at %s was free while a server was serving over that graph "+
			"directory. A server holds it for its whole life (SPEC/GRAPH.md § Lock "+
			"Contention), so nothing below would be observing a lock that was ever held and "+
			"the assertion after the kill would pass on a store that had no lock at all",
			filepath.Join(server.graphDir, graphlock.LockFileName))
	} else if !errors.Is(err, graphlock.ErrBusy) {
		t.Fatalf("acquiring the store lock against a live server failed with %v, want %v. "+
			"A different failure means the wait never reached contention — the lock file "+
			"could not be opened, say — so this run establishes nothing about a HELD lock",
			err, graphlock.ErrBusy)
	}

	server.killAndWait(t)

	release, err := graphlock.AcquireExclusive(server.graphDir)
	if err != nil {
		t.Fatalf("the store lock at %s is STILL HELD after the server holding it was killed "+
			"outright (%v). SIGKILL runs no code in the dying process, so nothing in this "+
			"project can release it: the lock is taken with flock(2) and held by the open "+
			"file description, and the kernel is what drops it when the process dies, however "+
			"it dies (internal/graphlock/graphlock_unix.go). A killed server that stranded "+
			"this lock would leave the roadmap's graph unreachable until an operator cleared "+
			"something by hand, and SPEC/GRAPH.md § Server Shutdown and the Drain promises "+
			"the opposite: the next server starts",
			filepath.Join(server.graphDir, graphlock.LockFileName), err)
	}
	release()

	// The prose, pinned to what was just measured rather than to what the last
	// author believed. This is the assertion that fails if the withdrawn clause
	// is ever written back into the cost list.
	if strings.Contains(sigkillSkips, "lock") {
		t.Errorf("the published cost of a SIGKILL, %q, mentions the lock. The acquisition "+
			"above measured the opposite on this machine: the lock was held by a live server, "+
			"the kill released it, and the next acquisition succeeded. Naming it among the "+
			"costs sends an operator hunting for a lock to clear that the kernel has already "+
			"dropped", sigkillSkips)
	}
	for _, step := range []struct{ phrase, why string }{
		{"shutdown checkpoint", "step 4 of SPEC/GRAPH.md § Server Shutdown and the Drain: the " +
			"write-ahead log is left unfolded and the next open replays a longer one"},
		{"socket removal", "step 6 of the same sequence: a dead socket file is left behind, " +
			"refusing every connection until the next server clears it at startup"},
	} {
		if !strings.Contains(sigkillSkips, step.phrase) {
			t.Errorf("the published cost of a SIGKILL, %q, no longer names %q, which a kill "+
				"really does skip — %s. Understating the cost is the same defect as "+
				"overstating it", sigkillSkips, step.phrase, step.why)
		}
	}
}

// TestSigkillCostList_IsWrittenOnce keeps the pin above total.
//
// [TestKilledServer_StrandsNoStoreLock] can hold exactly one string to the
// measurement it makes, so the account is only as honest as the number of copies
// of it: the defect it regresses existed in THREE places at once, two failure
// messages and a doc comment, and correcting one of them would have left the
// other two telling an operator the same false thing. This sweep is what makes
// "the cost list is written once" a property of the package rather than of the
// day it was tidied.
//
// It reads this package's own directory, which is the working directory of a
// `go test` run, and it counts occurrences rather than files so that a file
// carrying two copies is reported as carrying two.
//
// Known limits, stated rather than papered over:
//
//   - It is a text sweep keyed on the list's opening phrase, so a copy that
//     paraphrased the list in different words would pass it. The cheaper defence
//     against that is the one this file is built around: there is a named
//     constant to interpolate, so writing the words out by hand is the harder
//     path rather than the obvious one.
//   - This file is exempt, because it is the file that declares the constant,
//     pins it, and quotes the withdrawn clause in order to record what the defect
//     was. Moving the constant out of it therefore fails this gate rather than
//     following it silently, which is the intended direction.
func TestSigkillCostList_IsWrittenOnce(t *testing.T) {
	// Non-vacuity, and it is checked against the constant rather than against the
	// file, because that is where it can actually go wrong: reword sigkillSkips
	// so it no longer opens with this phrase and the sweep below is watching for
	// something nothing writes, at which point it passes over every copy it
	// exists to catch.
	if !strings.Contains(sigkillSkips, sigkillCostListPhrase) {
		t.Fatalf("sigkillSkips is %q, which no longer contains the phrase this sweep keys on, "+
			"%q. The sweep would then report a clean package however many hand-written copies "+
			"of the cost list it held; re-key it on the new wording rather than deleting it",
			sigkillSkips, sigkillCostListPhrase)
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading this package's directory: %v", err)
	}

	scanned := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || entry.Name() == thisFile {
			continue
		}
		source, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("reading %s: %v", entry.Name(), err)
		}
		scanned++
		if n := strings.Count(string(source), sigkillCostListPhrase); n > 0 {
			t.Errorf("%s writes the SIGKILL cost list out by hand (%d occurrence(s) of %q). "+
				"There is one constant for it, sigkillSkips, and one test that holds that "+
				"constant to a measurement; a second copy is a second thing to be wrong, which "+
				"is exactly how the withdrawn lock-release clause came to be published in three "+
				"places at once. Interpolate the constant instead",
				entry.Name(), n, sigkillCostListPhrase)
		}
	}

	// A sweep that read nothing reports nothing. This catches the case where the
	// working directory is not the package's — the assumption the walk rests on.
	if scanned == 0 {
		t.Errorf("the sweep read no Go file beside %s, so it examined nothing. A `go test` run "+
			"has this package's directory as its working directory; if that stopped holding, "+
			"this gate passes vacuously", thisFile)
	}
}

// thisFile is the only file allowed to carry the cost list, because it is the
// file that declares the constant, the file that pins it, and the file that
// records the withdrawn clause verbatim. It is written out rather than derived
// from runtime.Caller so that the exemption is a decision a reader can see.
const thisFile = "killlock_test.go"
