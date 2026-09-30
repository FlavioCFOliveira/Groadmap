//go:build !windows

// Regression fence for rmp task #561: the socket was connectable under a mode
// wider than 0600, and SPEC/GRAPH.md acceptance criterion 79 is what it answers.
//
// # The defect
//
// bind created the listener and THEN set the socket's mode. Under a umask of 0000
// the socket existed, listening, at mode 0777 between the two calls, and the
// kernel accepts connections into a listener's queue from the moment it listens.
// With --socket placing it in a directory every account can write, another
// account connected 3 times in 3, and a connection accepted in that interval is
// not revoked by the chmod that follows it (SPEC/GRAPH.md § Socket Path and
// Permissions, rule 3).
//
// # What the fence observes, since a second account is not available
//
// A test cannot switch uid. What the criterion's second account would observe
// is, however, fully determined by two things this process CAN observe from the
// outside of the socket: the mode the file carries at the path, and whether a
// connection through the path succeeds. So the socket is bound again and again
// under a umask of 0000 in a mode-1777 directory while other goroutines watch the
// path in tight loops, and two things must never be seen:
//
//   - the file at the path with any mode other than 0600, from the instant it
//     first appears;
//   - a connection through the path that succeeds right after the file was seen
//     there under any other mode.
//
// On the old code the first is seen within a handful of binds: the file appears
// at 0777 and is narrowed microseconds later. On the staged bind the file only
// ever appears at the path already at 0600.
package graphserve

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
)

// shortSocketRoot creates a directory with a short path for a socket, and
// returns it. See graphRoot for why t.TempDir's test-named directories are too
// long for a socket path.
func shortSocketRoot(t *testing.T) string {
	t.Helper()

	root, err := os.MkdirTemp("", "rmps")
	if err != nil {
		t.Fatalf("creating the test's socket root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

// socketObservations is what the watching goroutines saw.
type socketObservations struct {
	violations []string
	// narrow counts the observations of the file at the path at 0600, which is
	// what makes a clean run mean something: a watcher that never saw the file
	// at all would report no violation either.
	narrow    atomic.Int64
	connected atomic.Int64
	mu        sync.Mutex
}

func (o *socketObservations) violate(what string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.violations) < 10 {
		o.violations = append(o.violations, what)
	}
}

// TestBind_TheSocketIsNeverConnectableUnderAWiderMode is acceptance criterion 79,
// observed from inside one account; see the file comment for what that observes
// and why it is the same thing.
func TestBind_TheSocketIsNeverConnectableUnderAWiderMode(t *testing.T) {
	const (
		binds    = 300
		watchers = 3
		dialers  = 2
	)

	dir := shortSocketRoot(t)
	// A directory every account can traverse and write, as --socket may name.
	if err := os.Chmod(dir, 0o1777); err != nil { //nolint:gosec // the criterion's world-writable directory, inside this test's own temporary root
		t.Fatalf("making %s world-writable: %v", dir, err)
	}
	path := filepath.Join(dir, "graph.sock")

	// The umask is process-wide, which is acceptable in a test that runs alone
	// and restores it: nothing else in this package's tests runs in parallel.
	previous := syscall.Umask(0)
	defer syscall.Umask(previous)

	var seen socketObservations
	stop := make(chan struct{})
	var wg sync.WaitGroup

	for range watchers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				info, err := os.Lstat(path)
				if err != nil {
					continue
				}
				if mode := info.Mode().Perm(); mode != graphclient.SocketMode {
					seen.violate("the file at the path had mode " + mode.String())
					continue
				}
				seen.narrow.Add(1)
			}
		}()
	}
	for range dialers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				before, statErr := os.Lstat(path)
				conn, err := net.Dial("unix", path)
				if err != nil {
					continue
				}
				_ = conn.Close() //nolint:errcheck // the connection existed; that is all this records
				seen.connected.Add(1)
				if statErr == nil && before.Mode().Perm() != graphclient.SocketMode {
					seen.violate("a connection succeeded right after the file was seen at mode " +
						before.Mode().Perm().String())
				}
			}
		}()
	}

	for i := 0; i < binds; i++ {
		ln, err := bind(path)
		if err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("bind %d of %d failed: %v", i+1, binds, err)
		}
		_ = ln.Close() //nolint:errcheck // the wrapper's Close reports no error by construction
	}
	close(stop)
	wg.Wait()

	if len(seen.violations) > 0 {
		t.Errorf("under a umask of 0000, in a mode-1777 directory, the socket was observable at "+
			"the path under a mode other than 0600 (first %d shown):\n  %s\nAnother account "+
			"connects to it in that interval, and a chmod applied afterwards does not revoke the "+
			"connection (SPEC/GRAPH.md § Socket Path and Permissions, rule 3; criterion 79)",
			len(seen.violations), strings.Join(seen.violations, "\n  "))
	}
	if seen.narrow.Load() == 0 {
		t.Errorf("across %d binds no watcher ever saw the socket at the path at all, so the "+
			"absence of a violation proves nothing", binds)
	}

	// Nothing the staging used is left beside the socket's parent.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listing %s: %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("%d binds and closes left %v in %s; each bind must remove its staging "+
			"directory and each close its socket", binds, names, dir)
	}
	t.Logf("%d binds; the path was seen at 0600 %d times and connected through %d times",
		binds, seen.narrow.Load(), seen.connected.Load())
}

// TestBind_LeavesAFileAtThePathExactlyAsItWas pins the property the staged bind
// must not lose: a bind never replaced what occupied its path, and neither may
// the link that now names the socket. A rename would REPLACE the file, so a
// mistyped --socket naming a file the caller cares about would destroy it; the
// bind must fail with the published line and leave the file, byte for byte.
func TestBind_LeavesAFileAtThePathExactlyAsItWas(t *testing.T) {
	dir := shortSocketRoot(t)
	path := filepath.Join(dir, "notes.txt")
	const content = "architecture decisions for the payments service\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	ln, err := bind(path)
	if err == nil {
		_ = ln.Close() //nolint:errcheck // the test is already failing
		t.Fatalf("bind succeeded over an existing regular file at %s", path)
	}
	if want := "cannot bind " + path + ": "; !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal %q does not carry the published bind line %q", err, want)
	}

	got, readErr := os.ReadFile(path) //nolint:gosec // the file this test wrote
	if readErr != nil || string(got) != content {
		t.Errorf("the file at the path was changed by a refused bind: %q, %v", got, readErr)
	}
	entries, _ := os.ReadDir(dir) //nolint:errcheck // an unreadable directory fails the count below
	if len(entries) != 1 {
		t.Errorf("a refused bind left %d entries in %s, want only the caller's file", len(entries), dir)
	}
}

// TestBind_AcceptsAPathAtTheServeLimit pins the other side of the transient
// path's length: `graph serve` refuses a target whose transient bind path would
// exceed the platform's bound (SPEC/GRAPH.md § Socket Path Length, rule 9), and
// a target of exactly the limit it reports must then bind. The default basename,
// graph.sock, is 8 bytes short of the transient path's excess, so its limit is
// the bound less 8.
func TestBind_AcceptsAPathAtTheServeLimit(t *testing.T) {
	dir := shortSocketRoot(t)
	const base = "graph.sock"
	limit := ServeSocketPathLimit(filepath.Join(dir, base))
	if want := graphclient.MaxSocketPathLen - (stagedPathExcess - len(base)); limit != want {
		t.Fatalf("ServeSocketPathLimit(.../%s) = %d, want the bound less %d = %d",
			base, limit, stagedPathExcess-len(base), want)
	}
	pad := limit - len(dir) - 2 - len(base)
	if pad < 1 {
		t.Fatalf("the temporary directory %s is too long to build a path at the limit", dir)
	}
	parent := filepath.Join(dir, strings.Repeat("p", pad))
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatalf("creating %s: %v", parent, err)
	}
	path := filepath.Join(parent, base)
	if len(path) != limit {
		t.Fatalf("built a %d-byte path, want %d", len(path), limit)
	}

	ln, err := bind(path)
	if err != nil {
		t.Fatalf("a %d-byte path, exactly the serve limit, was refused: %v", len(path), err)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		_ = ln.Close() //nolint:errcheck // the test is already failing
		t.Fatalf("the socket bound at the limit does not answer: %v", err)
	}
	_ = conn.Close() //nolint:errcheck // the dial is what was asserted
	_ = ln.Close()   //nolint:errcheck // the wrapper's Close reports no error by construction

	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("closing the listener left the socket file at %s (%v)", path, err)
	}
}

// TestServeSocketPathLimit_IsTheBoundForALongFinalComponent pins the other
// branch of the arithmetic: a final component of 18 bytes or more makes the
// transient path no longer than the target, so the limit is the platform's
// bound itself and rule 9 refuses nothing rules 1 to 5 would not.
func TestServeSocketPathLimit_IsTheBoundForALongFinalComponent(t *testing.T) {
	for _, base := range []string{strings.Repeat("g", stagedPathExcess), strings.Repeat("g", 40)} {
		if got := ServeSocketPathLimit("/run/user/1000/" + base); got != graphclient.MaxSocketPathLen {
			t.Errorf("ServeSocketPathLimit for a %d-byte final component = %d, want the bound %d",
				len(base), got, graphclient.MaxSocketPathLen)
		}
	}
}

// TestStagingName_IsThePrefixAndSixCharacters pins the reserved form of the
// staging directory's name (SPEC/GRAPH.md § Socket Path and Permissions,
// rule 8), which the next server's residue removal relies on recognising.
func TestStagingName_IsThePrefixAndSixCharacters(t *testing.T) {
	form := regexp.MustCompile(`^\.rmp-bind-[a-z0-9]{6}$`)
	for range 200 {
		name, err := stagingName()
		if err != nil {
			t.Fatalf("stagingName: %v", err)
		}
		if !form.MatchString(name) {
			t.Fatalf("staging name %q is not .rmp-bind- and six characters of [a-z0-9]", name)
		}
	}
}
