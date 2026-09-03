package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/signals"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// shutdownGrace bounds how long graceful shutdown waits for in-flight
// requests to complete before the listener is forced closed. Web reads are
// short-lived, so a small bound is sufficient (SPEC/WEB.md § Server
// Lifecycle: "a brief bounded period to complete").
const shutdownGrace = 5 * time.Second

// serve runs the long-lived HTTP server for one `rmp web` invocation. It
// ensures the data directory, binds a listener (with the SPEC port-fallback
// rules), prints the served URL as the success object, optionally opens a
// browser, and serves until SIGINT/SIGTERM, after which it shuts down
// gracefully and returns nil (exit 0).
func serve(opts options) error {
	// 1. Ensure ~/.roadmaps/ exists and is readable. The legacy-layout
	//    migration sweep already ran in main.go before dispatch; we only
	//    confirm the data directory here.
	if err := utils.EnsureDataDir(); err != nil {
		return fmt.Errorf("%w: cannot read data directory ~/.roadmaps: %v", utils.ErrDatabase, err)
	}

	// 2. Ensure every served roadmap's SQLite schema is current. The web
	//    server's per-request loaders open each database with
	//    OpenReadOnly/query_only and therefore CANNOT run migrations (finding
	//    #43: a read must never rewrite a stale-schema database). If `rmp web`
	//    is the first command run after a binary upgrade that bumped the
	//    schema, the read-only queries would reference columns the stale file
	//    lacks and the affected pages would fail. Migrating once here, before
	//    any read-only connection is opened, keeps the per-request path
	//    strictly read-only while guaranteeing automatic, no-input migration
	//    (SPEC/WEB.md § Startup Schema Migration).
	migrateRoadmapsAtStartup()

	// 3. Bind a TCP listener, applying the port-selection rules.
	ln, err := bindListener(opts)
	if err != nil {
		return err
	}

	// 4. Determine the actual bound port (which differs from the requested
	//    one under the ephemeral fallback or --port 0) and print the URL.
	actualPort := ln.Addr().(*net.TCPAddr).Port
	url := "http://" + net.JoinHostPort(opts.host, strconv.Itoa(actualPort))

	// When the resolved bind host is not a loopback address the interface is
	// reachable from the network: warn on stderr (informational only; it does
	// not change the exit code or prevent startup). Loopback binds print no
	// warning (SPEC/WEB.md § Bind Address and Port Selection, item 3). The
	// warning is written before the success object so a caller that reads
	// stdout for the URL is unaffected.
	if !isLoopbackHost(opts.host) {
		logger.Warn("web interface is reachable from the network",
			slog.String("host", opts.host),
			slog.String("hint", "use --host 127.0.0.1 to restrict to this machine"))
	}

	// Take the signals over BEFORE the URL is printed, and therefore before
	// the browser launch that follows it.
	//
	// What SIGINT and SIGTERM mean to this process changes at this line, from
	// the exit-130 cmd/rmp/main.go declares for a short-lived invocation to the
	// graceful shutdown in runServer. Ordering it ahead of the announcement is
	// what makes the printed URL mean "this server shuts down cleanly": a caller
	// that has read the URL cannot signal a process that has not yet taken over.
	//
	// It matters more here than it looks. Step 5 below spawns a browser, and a
	// process spawn is far slower than anything else between the URL and the
	// server; taking over after it would leave that whole span disowned. See
	// internal/signals for the interval this replaces and what was measured in
	// it (rmp task #388).
	sigCh, releaseSignals := signals.TakeOver()
	defer releaseSignals()

	if perr := utils.PrintJSON(map[string]string{"url": url}); perr != nil {
		_ = ln.Close()
		return perr
	}

	// 5. Best-effort browser launch. A failed launch is not fatal: the URL
	//    has already been printed (SPEC/WEB.md § Server Lifecycle).
	if !opts.noOpen {
		openBrowser(url)
	}

	// 6. Serve and wait for a termination signal, then shut down
	//    gracefully.
	return runServer(ln, sigCh)
}

// migrateRoadmapsAtStartup brings every existing roadmap's SQLite schema up to
// the current version exactly once, at server startup, before any read-only
// request connection is opened. It opens each roadmap via the normal writable
// db.Open path — which runs RunMigrations and is idempotent (a no-op when the
// schema is already current) — and closes it immediately. This is the web
// server's ONLY write to the databases; per-request handlers open read-only
// (db.OpenReadOnly/query_only) and never migrate (SPEC/WEB.md § Startup Schema
// Migration; Read-Only Data Flow — finding #43).
//
// Best-effort and non-fatal: a roadmap that cannot be listed, opened, or
// migrated is logged as a WARN record on stderr and skipped so the server still starts for the
// remaining roadmaps. The stale roadmap simply surfaces its underlying error
// on its own routes, exactly as it would have without this step. This mirrors
// the best-effort tone of the legacy-layout sweep and the network-exposure
// warning.
func migrateRoadmapsAtStartup() {
	names, err := utils.ListRoadmaps()
	if err != nil {
		logger.Warn("cannot list roadmaps for startup schema migration", slog.String("err", err.Error()))
		return
	}
	for _, name := range names {
		database, err := db.Open(name)
		if err != nil {
			logger.Warn("startup schema migration skipped for roadmap",
				slog.String("roadmap", name), slog.String("err", err.Error()))
			continue
		}
		_ = database.Close() // #nosec G104 -- best-effort startup migration; close error is non-actionable
	}
}

// bindListener resolves the bind address and returns a listening TCP
// socket, applying the SPEC port-selection rules (SPEC/WEB.md § Bind
// Address and Port Selection):
//   - An explicit --port that cannot be bound is a fatal bind error; there
//     is no fallback.
//   - The default port (no --port), when busy, falls back to an
//     OS-chosen ephemeral port so the server still starts.
//   - --port 0 is explicit and binds an ephemeral port directly.
func bindListener(opts options) (net.Listener, error) {
	addr := net.JoinHostPort(opts.host, strconv.Itoa(opts.port))
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}

	// An explicit --port does not fall back: the user asked for that exact
	// port, so a bind failure is fatal.
	if opts.portExplicit {
		return nil, fmt.Errorf("%w: cannot bind %s: %v", utils.ErrDatabase, addr, err)
	}

	// Default port busy: fall back to an OS-chosen ephemeral port.
	fbAddr := net.JoinHostPort(opts.host, "0")
	fbLn, fbErr := net.Listen("tcp", fbAddr)
	if fbErr != nil {
		// Even the ephemeral fallback failed (for example the host is not
		// assignable); report the original requested address.
		return nil, fmt.Errorf("%w: cannot bind %s: %v", utils.ErrDatabase, addr, fbErr)
	}
	return fbLn, nil
}

// isLoopbackHost reports whether host resolves to a loopback bind address, so
// the caller can decide whether the network-exposure warning is needed. The
// literal "localhost" is treated as loopback (it resolves to 127.0.0.1/::1);
// "127.0.0.1", "::1", and any other address in the loopback range
// (for example 127.0.0.2) are loopback. Any address that does not parse as a
// loopback IP — including 0.0.0.0, a routable IP, or a hostname — is treated as
// non-loopback and therefore network-exposed (SPEC/WEB.md § Bind Address and
// Port Selection, item 3).
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// newServer builds the configured http.Server: the security-hardened handler
// plus the three mandatory timeouts that protect the read-only server from
// resource exhaustion by slow or idle connections (SPEC/WEB.md § HTTP Server
// Timeouts). ReadHeaderTimeout bounds slow-header (Slowloris) connections,
// WriteTimeout bounds a slow-reading client stalling the response, and
// IdleTimeout bounds idle keep-alive connections.
func newServer() *http.Server {
	return &http.Server{
		Handler:           handler(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// runServer serves HTTP on ln until SIGINT/SIGTERM, then shuts down
// gracefully. It returns nil on a clean shutdown (exit 0) and a wrapped
// ErrDatabase when Serve fails for any reason other than the expected
// http.ErrServerClosed.
//
// sigCh is the channel [serve] took the signals over on, one step before the
// URL was printed. cmd/rmp/main.go's default action for SIGINT and SIGTERM is
// os.Exit(130), which for `rmp web` would skip the graceful shutdown and report
// the wrong exit code; the take-over replaces that action for the server's
// lifetime and releases it afterwards.
//
// The channel arrives as a parameter rather than being registered here because
// the take-over has to precede the announcement, which happens in [serve]. It
// used to be registered here with signal.Reset followed by signal.Notify, and
// that pair is the defect rmp task #388 fixed: see internal/signals.
func runServer(ln net.Listener, sigCh <-chan os.Signal) error {
	srv := newServer()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	select {
	case <-sigCh:
		// Graceful shutdown: stop accepting new connections and give
		// in-flight requests a brief bounded window to finish.
		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return nil
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("%w: web server failed: %v", utils.ErrDatabase, err)
		}
		return nil
	}
}

// openBrowser launches the user's default browser at url, detached, on a
// best-effort basis. Any failure (no GUI, missing launcher) is ignored:
// the URL has already been printed to stdout. The launcher per platform is
// the conventional one; no outbound network request is made by rmp itself.
func openBrowser(url string) {
	// url is built internally as "http://host:port" and passed as a single
	// argument to the conventional launcher via exec.Command, which runs the
	// program directly without a shell — no shell-metacharacter injection is
	// possible, even though host originates from the --host flag.
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url) // #nosec G204 -- internal URL, no shell, no injection
	case "darwin":
		cmd = exec.Command("open", url) // #nosec G204 -- internal URL, no shell, no injection
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) // #nosec G204 -- internal URL, no shell, no injection
	default:
		return
	}
	// Start (not Run) so the launcher runs detached; ignore the error.
	_ = cmd.Start()
}
