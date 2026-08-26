package commands

import (
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// terminalRefusalBudget is how long readQueryStdin may take to refuse a
// terminal. The contract is "without being read at all", so the honest budget is
// microseconds; a second is chosen only so that a loaded machine cannot produce
// a false failure, and it is still four orders of magnitude below the forty
// minutes the defect burned.
const terminalRefusalBudget = time.Second

// TestReadQueryStdinRefusesATerminalWithoutWaiting is the regression gate for
// the half of SPEC/GRAPH.md acceptance criterion 41 that an exit code cannot
// express (task #181).
//
// The defect: with --query absent and a terminal on standard input, the read
// waited for a query nobody was going to type. Nothing on the command line looked
// wrong, nothing was printed, and the process never returned — one invocation was
// killed after roughly forty minutes. Any automated caller, a script or a CI step
// or an agent, blocks indefinitely with no diagnostic.
//
// WHY THIS IS ASSERTED ON WALL-CLOCK TIME. A test that only checked the error
// would never fail on the defect: a call that never returns produces no error to
// examine, so the assertion would hang with it. The property is therefore stated
// as the specification states it — the refusal comes BEFORE any read — and the
// only way to observe "before any read" from outside is that the call returns
// while a read would still be waiting.
//
// The file is constrained to Linux by its name, because it needs a real
// pseudo-terminal and testenv.OpenPTY implements the Linux sequence. The
// end-to-end suite drives the compiled binary the same way on whatever platform
// it runs, so the criterion is covered against the shipped artefact too.
func TestReadQueryStdinRefusesATerminalWithoutWaiting(t *testing.T) {
	master, slave, err := testenv.OpenPTY()
	if err != nil {
		t.Fatalf("opening a pseudo-terminal: %v", err)
	}
	defer func() { _ = slave.Close() }()
	defer func() { _ = master.Close() }()

	// Nothing is ever written to the master, so the terminal carries no input at
	// all: exactly the situation the defect hung in.
	type outcome struct {
		err   error
		query string
	}
	done := make(chan outcome, 1)
	go func() {
		query, readErr := readQueryStdin(slave)
		done <- outcome{query: query, err: readErr}
	}()

	select {
	case got := <-done:
		assertNoQuery(t, got.err)
		if got.query != "" {
			t.Errorf("a refused invocation must return no query, got %q", got.query)
		}
	case <-time.After(terminalRefusalBudget):
		// The goroutine is parked in a read that will never complete; it ends with
		// the test binary. Failing here rather than waiting is the whole point:
		// the defect this gate closes is precisely a call that does not return.
		t.Fatalf("readQueryStdin did not refuse a terminal within %s: it is waiting "+
			"for input on a terminal instead of failing at once (SPEC/GRAPH.md "+
			"§ Standard Input That Supplies No Query)", terminalRefusalBudget)
	}
}
