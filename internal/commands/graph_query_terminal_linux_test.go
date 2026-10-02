package commands

import (
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// TestReadQueryStdinRefusesATerminalWithoutWaiting is the regression gate for
// the half of SPEC/GRAPH.md acceptance criterion 24 that an exit code alone
// cannot express (task #181).
//
// The defect: with --query absent and a terminal on standard input, the read
// waited for a query nobody was going to type. Nothing on the command line looked
// wrong, nothing was printed, and the process never returned — one invocation was
// killed after roughly forty minutes. Any automated caller, a script or a CI step
// or an agent, blocks indefinitely with no diagnostic.
//
// WHAT IS ASSERTED, AND WHY IT IS NOT A DURATION. The standard input this drives
// is a pseudo-terminal that is never written to and never closed, so it will
// never reach end of stream and never carry a byte. An implementation that reads
// it before deciding cannot return at all. The assertion is therefore that the
// call RETURNS, and returns the refusal — an outcome, and not a measurement of
// how long it took. Criterion 24 says so in as many words, and SPEC/BUILD.md
// § No Benchmarks and No Performance-Measurement Tests is the rule behind it:
// "Requiring termination is not a timing assertion. A test may require that an
// invocation exits rather than blocks; what it asserts is that the process
// ended."
//
// A build that regressed hangs here rather than failing, and the run is ended by
// the `test` gate's own timeout — the harness stopping a stuck run, which is
// exactly what that paragraph reserves for this case. The call is therefore made
// on this goroutine: a goroutine and a select would only convert that stop into a
// deadline of this test's own choosing, which is the assertion the criterion
// forbids.
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

	// Nothing is ever written to the master and neither end is closed, so the
	// terminal carries no input and never will: exactly the situation the defect
	// hung in.
	query, readErr := readQueryStdin(slave)

	assertNoQuery(t, readErr)
	if query != "" {
		t.Errorf("a refused invocation must return no query, got %q", query)
	}
}
