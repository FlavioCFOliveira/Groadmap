//go:build windows

package graphserve

import (
	"fmt"
	"net"
	"os"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// bind creates the listener on Windows.
//
// The staged bind the other platforms perform (see bind_unix.go) exists to keep
// a POSIX mode from ever being wider than 0600 while the socket is connectable,
// and it rests on two POSIX properties: a process umask that decides the mode a
// bind creates, and a hard link that gives a bound socket a second name. Windows
// has neither in that sense. Access to a socket file there is governed by the
// access-control list the file inherits from its directory, which a bind applies
// atomically, and the POSIX mode is an emulation the operating system does not
// enforce for connections. The listener is therefore bound directly, and the mode
// is set afterwards exactly as it was before the staged bind existed, so the
// published 0600 is still what the file reports (SPEC/GRAPH.md § Socket Path
// and Permissions, rule 3; § Server Startup, step 5).
func bind(path string) (*serverListener, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot bind %s: %v", utils.ErrGraphServer, path, err)
	}
	if err := os.Chmod(path, graphclient.SocketMode); err != nil {
		_ = ln.Close() //nolint:errcheck // already failing; the close unlinks the socket and its own error cannot be acted on
		return nil, fmt.Errorf("%w: cannot bind %s: %v", utils.ErrGraphServer, path, err)
	}
	return newServerListener(ln), nil
}

// ServeSocketPathLimit is the most bytes a socket path may occupy for
// `rmp graph serve` on Windows: the platform's bound, unchanged, because Windows
// binds no transient path (SPEC/GRAPH.md § Socket Path Length, rule 9).
func ServeSocketPathLimit(string) int { return graphclient.MaxSocketPathLen }

// removeStagingResidue does nothing on Windows: no staging directory is ever
// created there, so none is examined (SPEC/GRAPH.md § Socket Path and
// Permissions, rule 9).
func removeStagingResidue(string) {}
