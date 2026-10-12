//go:build !windows

package graphserve

import "syscall"

// serverUmask is the process umask `rmp graph serve` runs under: every bit but
// the owner's is masked off every mode the server and its engine ask for.
const serverUmask = 0o077

// RestrictUmask sets the process umask to 0077. `rmp graph serve` calls it as
// the first action of step 1 of SPEC/GRAPH.md § Server Startup and never
// changes the umask afterwards.
//
// It is what holds the graph store's entries at the modes of § Persistence
// Layout, rule 4, from the instant each is created: the engine chooses the mode
// of every entry it creates, and it creates some of its directories — the
// snapshot directory, a checkpoint's staging directory, the index directory
// inside a snapshot — at 0750, which only the umask narrows. The kernel applies
// the umask in the same system call that creates the entry, so no entry the
// server creates below graph/ is ever wider than owner-only, whether it is
// created at startup, by a checkpoint, or by a format conversion.
//
// The umask is process-wide, and that is acceptable for this process: every
// entry `rmp graph serve` creates is owner-only by its own rule, and each of
// those rules still sets its mode explicitly, so none depends on this call.
// Setting it writes nothing to disk, so a server that cannot start still
// touches nothing.
func RestrictUmask() {
	syscall.Umask(serverUmask)
}
