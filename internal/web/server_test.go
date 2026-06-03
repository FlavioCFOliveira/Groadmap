package web

import (
	"errors"
	"net"
	"strconv"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// occupyPort binds an ephemeral loopback port and returns the listener and its
// port. The caller closes the listener. Because the listener stays open, that
// exact port is guaranteed busy for the duration of the test, which lets us
// drive bindListener's bind-failure branches deterministically without racing
// the OS port allocator.
func occupyPort(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupying a port: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

// TestBindListener_EphemeralSuccess covers bindListener's happy path: --port 0
// is an explicit ephemeral request and binds straight away on the first
// net.Listen, returning a usable listener.
func TestBindListener_EphemeralSuccess(t *testing.T) {
	ln, err := bindListener(options{host: "127.0.0.1", port: 0, portExplicit: true})
	if err != nil {
		t.Fatalf("bindListener(--port 0) failed: %v", err)
	}
	defer ln.Close() //nolint:errcheck // test cleanup
	if got := ln.Addr().(*net.TCPAddr).Port; got == 0 {
		t.Errorf("bound port = 0, want an OS-assigned non-zero port")
	}
}

// TestBindListener_ExplicitPortBusyIsFatal covers the explicit-port branch: a
// busy port requested via an explicit --port does NOT fall back; the bind
// failure is fatal and surfaces as a wrapped ErrDatabase (SPEC/WEB.md rule:
// an explicit --port that cannot be bound is a fatal bind error).
func TestBindListener_ExplicitPortBusyIsFatal(t *testing.T) {
	busy, port := occupyPort(t)
	defer busy.Close() //nolint:errcheck // test cleanup

	ln, err := bindListener(options{host: "127.0.0.1", port: port, portExplicit: true})
	if err == nil {
		_ = ln.Close()
		t.Fatalf("bindListener on busy explicit port = nil error, want fatal bind error")
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("error = %v, want wrapping ErrDatabase", err)
	}
	if !contains(err.Error(), strconv.Itoa(port)) {
		t.Errorf("error %q should name the requested port %d", err.Error(), port)
	}
}

// TestRun_HelpShortCircuits covers Run's showHelp branch (web.go): a help
// token makes Run print help and return nil WITHOUT starting a server, so the
// call returns immediately. This is the only Run path that does not block.
func TestRun_HelpShortCircuits(t *testing.T) {
	for _, tok := range []string{"-h", "--help", "help"} {
		if err := Run([]string{tok}); err != nil {
			t.Errorf("Run(%q) = %v, want nil", tok, err)
		}
	}
}

// TestRun_ParseErrorPropagates covers Run's parse-error branch: a bad flag is
// reported by parseArgs and Run returns that error without starting a server.
func TestRun_ParseErrorPropagates(t *testing.T) {
	if err := Run([]string{"--port", "not-a-number"}); err == nil {
		t.Errorf("Run with bad --port = nil, want a validation error")
	}
}

// TestRunServer_ServeErrorPropagates covers runServer's serveErr branch: when
// srv.Serve fails with an error other than http.ErrServerClosed, runServer
// returns a wrapped ErrDatabase. Passing an already-closed listener makes
// Serve return immediately with a real error (not ErrServerClosed), so the
// select lands on the serveErr case without needing a signal — and the test
// does not block.
func TestRunServer_ServeErrorPropagates(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	// Close before serving so Serve's Accept fails immediately with a
	// non-ErrServerClosed error.
	if cerr := ln.Close(); cerr != nil {
		t.Fatalf("closing listener: %v", cerr)
	}

	rerr := runServer(ln)
	if rerr == nil {
		t.Fatalf("runServer on a closed listener = nil, want a wrapped serve error")
	}
	if !errors.Is(rerr, utils.ErrDatabase) {
		t.Errorf("error = %v, want wrapping ErrDatabase", rerr)
	}
}

// TestServe_BindFailureReturnsError covers serve's early bind-failure exit:
// with the data directory present (temp HOME) and an explicit busy port,
// serve must propagate bindListener's fatal error and return WITHOUT entering
// the long-lived runServer loop (so the test does not block). This exercises
// serve through EnsureDataDir success and the `bindListener` error return.
func TestServe_BindFailureReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	busy, port := occupyPort(t)
	defer busy.Close() //nolint:errcheck // test cleanup

	err := serve(options{host: "127.0.0.1", port: port, portExplicit: true, noOpen: true})
	if err == nil {
		t.Fatalf("serve with busy explicit port = nil, want fatal bind error")
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("error = %v, want wrapping ErrDatabase", err)
	}
}

// TestBindListener_DefaultPortBusyFallsBack covers the ephemeral-fallback
// branch: when the (non-explicit) default-style port is busy, bindListener
// silently falls back to an OS-chosen ephemeral port so the server still
// starts. We simulate the busy default by occupying a known port and passing
// it with portExplicit:false (the same code path the real default port takes
// when 8787 is busy), avoiding any dependency on 8787 being free or busy on
// the host.
func TestBindListener_DefaultPortBusyFallsBack(t *testing.T) {
	busy, port := occupyPort(t)
	defer busy.Close() //nolint:errcheck // test cleanup

	ln, err := bindListener(options{host: "127.0.0.1", port: port, portExplicit: false})
	if err != nil {
		t.Fatalf("bindListener with busy non-explicit port should fall back, got error: %v", err)
	}
	defer ln.Close() //nolint:errcheck // test cleanup

	got := ln.Addr().(*net.TCPAddr).Port
	if got == 0 {
		t.Errorf("fallback bound port = 0, want OS-assigned non-zero port")
	}
	if got == port {
		t.Errorf("fallback bound the busy port %d; want a different ephemeral port", port)
	}
}
