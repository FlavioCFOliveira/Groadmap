package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

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

	// 2. Bind a TCP listener, applying the port-selection rules.
	ln, err := bindListener(opts)
	if err != nil {
		return err
	}

	// 3. Determine the actual bound port (which differs from the requested
	//    one under the ephemeral fallback or --port 0) and print the URL.
	actualPort := ln.Addr().(*net.TCPAddr).Port
	url := "http://" + net.JoinHostPort(opts.host, strconv.Itoa(actualPort))
	if perr := utils.PrintJSON(map[string]string{"url": url}); perr != nil {
		_ = ln.Close()
		return perr
	}

	// 4. Best-effort browser launch. A failed launch is not fatal: the URL
	//    has already been printed (SPEC/WEB.md § Server Lifecycle, step 5).
	if !opts.noOpen {
		openBrowser(url)
	}

	// 5. Serve and wait for a termination signal, then shut down
	//    gracefully.
	return runServer(ln)
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

// runServer serves HTTP on ln until SIGINT/SIGTERM, then shuts down
// gracefully. It returns nil on a clean shutdown (exit 0) and a wrapped
// ErrDatabase when Serve fails for any reason other than the expected
// http.ErrServerClosed.
//
// Signal handling gotcha: cmd/rmp/main.go installs a global SIGINT/SIGTERM
// handler that calls os.Exit(130). For `rmp web` that would skip graceful
// shutdown and report the wrong exit code, so we first Reset those signals
// (removing main's handler) and then register our own, scoped to the
// server's lifetime.
func runServer(ln net.Listener) error {
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	srv := &http.Server{
		Handler:           buildMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

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
