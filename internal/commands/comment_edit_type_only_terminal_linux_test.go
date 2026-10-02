package commands

import (
	"os"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// TestCommentEditTypeOnly_ATerminalIsNotRead pins the terminal branch of rule 7
// of SPEC/COMMANDS.md § Comment Body Input Source and Precedence (rmp task 401):
// with --type and no --body, a terminal on standard input is not read at all,
// so a type-only edit typed at a terminal proceeds and never waits for input.
//
// WHAT IS ASSERTED, AND WHY IT IS NOT A DURATION. Standard input is a
// pseudo-terminal that is never written to and never closed, so it will never
// carry a byte and never reach end of stream. An implementation that read it to
// learn whether it carries data could not return at all. The assertion is
// therefore that the edit RETURNS, and succeeds with the type changed — an
// outcome, not a measurement (SPEC/BUILD.md § No Benchmarks and No
// Performance-Measurement Tests: "Requiring termination is not a timing
// assertion"). A regressed build hangs here and is stopped by the test gate's
// own timeout, exactly as TestReadQueryStdinRefusesATerminalWithoutWaiting in
// graph_query_terminal_linux_test.go is.
//
// The file is constrained to Linux by its name, because testenv.OpenPTY
// implements the Linux sequence.
func TestCommentEditTypeOnly_ATerminalIsNotRead(t *testing.T) {
	for _, fam := range typeOnlyEditFamilies() {
		t.Run(fam.name, func(t *testing.T) {
			roadmap := "type-only-terminal-" + fam.name
			id, database := fam.seed(t, roadmap)

			master, slave, err := testenv.OpenPTY()
			if err != nil {
				t.Fatalf("opening a pseudo-terminal: %v", err)
			}
			defer func() { _ = slave.Close() }()
			defer func() { _ = master.Close() }()

			restore := os.Stdin
			os.Stdin = slave
			editErr := fam.edit([]string{"-r", roadmap, itoa(id), "--type", fam.newType})
			os.Stdin = restore

			if editErr != nil {
				t.Fatalf("a type-only edit with a terminal on standard input = %v, want success", editErr)
			}
			typ, updated, audits := fam.state(t, database, id)
			if typ != fam.newType || !updated || audits != 1 {
				t.Errorf("after the edit: type %s (want %s), updated_at set %v (want true), %d update audit "+
					"entries (want 1)", typ, fam.newType, updated, audits)
			}
		})
	}
}
