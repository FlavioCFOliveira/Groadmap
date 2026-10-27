package web

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

// The graceful shutdown of SPEC/WEB.md § Server Lifecycle, step 7, driven
// through runServer, the function the signal path of `rmp web` runs: on SIGINT
// or SIGTERM the server stops accepting new connections, allows in-flight
// requests a brief BOUNDED period to complete, and exits 0 (acceptance
// criterion 24).
//
// The two halves of "a brief bounded period to complete" fail in opposite
// directions, so each has its own test. A shutdown that cut its connections at
// once would honour the bound and break "complete"; an unbounded one would
// honour "complete" and break the bound.
//
// Both tests hold a response IN FLIGHT by construction rather than by size:
// TestRunServer_SignalDrainsAnInFlightResponse blocks the server's second write
// on a gate the test opens, and TestRunServer_SignalDrainIsBoundedByShutdownGrace
// serves over net.Pipe, which has no buffer, to a client that stops reading. No
// socket buffer is involved in either, so no asset has to be larger than one.
//
// What stays outside this file: the delivery of the signal to the process and
// its mapping onto sigCh (internal/signals, and the repeated E2E case in
// tests/test_35_web_interface.py, which signals on the announcement), the exit
// code a nil return becomes (cmd/rmp/main.go), and the shutdownGrace value
// itself (test_the_shutdown_bound_is_the_one_the_server_declares in the same
// E2E module).

// stopSignals are the two signals step 7 names. runServer treats every value on
// sigCh alike, and both are driven so that stays a measured fact.
var stopSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}

// largestStaticAsset returns the path and content of the largest embedded
// static asset. Its size only has to exceed one 4 KiB write of the server's
// connection buffer, so that the response needs a second write for the gate to
// hold; the caller asserts that.
func largestStaticAsset(t *testing.T) (string, []byte) {
	t.Helper()
	var name string
	var size int64
	err := fs.WalkDir(staticSubFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > size {
			name, size = p, info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded static assets: %v", err)
	}
	content, err := fs.ReadFile(staticSubFS, name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	if len(content) < 64<<10 {
		t.Fatalf("the largest embedded asset, %s, is %d bytes; at least 64 KiB is needed "+
			"so the response spans many writes and one can be held in flight", name, len(content))
	}
	return "/static/" + name, content
}

// awaitClosed waits for ch to be closed. The deadline is a hang guard that
// turns a broken synchronisation into a failure; it is never what orders the
// test.
func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// gatedListener is a real loopback TCP listener whose accepted connections let
// their first Write through and hold every later one until release is closed.
// closed is closed once the server has closed the listener, AFTER the
// underlying socket is closed, so a probe that follows it observes the kernel's
// answer and not a race with it.
type gatedListener struct {
	net.Listener

	blocked     chan struct{} // closed when a Write is first held
	blockedOnce sync.Once
	release     chan struct{} // closed by the test to let held writes proceed
	releaseOnce sync.Once
	closed      chan struct{}
	closedOnce  sync.Once
}

func newGatedListener(t *testing.T) *gatedListener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	l := &gatedListener{
		Listener: ln,
		blocked:  make(chan struct{}),
		release:  make(chan struct{}),
		closed:   make(chan struct{}),
	}
	// A failing test must not leave a handler held on the gate.
	t.Cleanup(l.open)
	return l
}

func (l *gatedListener) open() { l.releaseOnce.Do(func() { close(l.release) }) }

func (l *gatedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &gatedConn{Conn: c, l: l}, nil
}

func (l *gatedListener) Close() error {
	err := l.Listener.Close()
	l.closedOnce.Do(func() { close(l.closed) })
	return err
}

type gatedConn struct {
	net.Conn
	l      *gatedListener
	writes atomic.Int64
}

// Write lets the first call through — the status line, the headers and the
// first part of the body — and holds every later one on the listener's gate,
// so the handler is blocked mid-response for as long as the test chooses.
func (c *gatedConn) Write(p []byte) (int, error) {
	if c.writes.Add(1) > 1 {
		c.l.blockedOnce.Do(func() { close(c.l.blocked) })
		<-c.l.release
	}
	return c.Conn.Write(p)
}

// TestRunServer_SignalDrainsAnInFlightResponse drives step 7's first promise on
// a server that is still running, and the "complete" half of its second.
//
// A response is held mid-write when the signal arrives. The server must then
// refuse a new connection WHILE runServer has not returned — after an exit the
// kernel would refuse whatever the code did, so only this vantage point makes
// the refusal an assertion — and, once the client reads on, the held response
// must arrive whole: every byte of the asset, under the Content-Length it
// declared. runServer must then return nil, and before shutdownGrace has run
// out, because a drained server has nothing left to wait for.
func TestRunServer_SignalDrainsAnInFlightResponse(t *testing.T) {
	path, asset := largestStaticAsset(t)
	for _, sig := range stopSignals {
		t.Run(sig.String(), func(t *testing.T) {
			ln := newGatedListener(t)
			addr := ln.Addr().(*net.TCPAddr)
			sigCh := make(chan os.Signal, 1)
			done := make(chan error, 1)
			go func() { done <- runServer(ln, sigCh, newHostPolicy("127.0.0.1", addr.Port)) }()

			conn, err := net.Dial("tcp", addr.String())
			if err != nil {
				t.Fatalf("dialling the server: %v", err)
			}
			defer conn.Close() //nolint:errcheck // test cleanup

			req, err := http.NewRequest(http.MethodGet, "http://"+addr.String()+path, nil)
			if err != nil {
				t.Fatalf("building the request: %v", err)
			}
			req.Close = true
			if err := req.Write(conn); err != nil {
				t.Fatalf("sending the request: %v", err)
			}
			br := bufio.NewReader(conn)
			resp, err := http.ReadResponse(br, req)
			if err != nil {
				t.Fatalf("reading the response head: %v", err)
			}
			defer resp.Body.Close() //nolint:errcheck // test cleanup
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
			}
			if resp.ContentLength != int64(len(asset)) {
				t.Fatalf("declared Content-Length = %d, want the asset's %d bytes",
					resp.ContentLength, len(asset))
			}

			// The handler is blocked mid-response: the request is in flight.
			awaitClosed(t, ln.blocked, "the response to be held mid-write")
			signalled := time.Now()
			sigCh <- sig

			// Promise 1, on a live server.
			awaitClosed(t, ln.closed, "the server to close its listener")
			probe, err := net.Dial("tcp", addr.String())
			if err == nil {
				_ = probe.Close() // #nosec G104 -- the probe is the failure being reported
				t.Fatalf("a new connection was accepted after %s", sig)
			}
			if !errors.Is(err, syscall.ECONNREFUSED) {
				t.Fatalf("a new connection after %s failed with %v, want ECONNREFUSED", sig, err)
			}
			select {
			case rerr := <-done:
				t.Fatalf("runServer returned %v with a response still in flight", rerr)
			default:
			}

			// Promise 2, "complete".
			ln.open()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("the in-flight response was cut after %d of %d bytes: %v",
					len(body), len(asset), err)
			}
			if !bytes.Equal(body, asset) {
				t.Fatalf("the in-flight response delivered %d bytes that are not the "+
					"%d-byte asset", len(body), len(asset))
			}

			// Exit 0 is runServer's nil, which serve returns as is.
			select {
			case rerr := <-done:
				if rerr != nil {
					t.Fatalf("runServer = %v after %s, want nil", rerr, sig)
				}
			case <-time.After(shutdownGrace + 5*time.Second):
				t.Fatalf("runServer did not return after the in-flight response was drained")
			}
			if elapsed := time.Since(signalled); elapsed >= shutdownGrace {
				t.Fatalf("runServer returned %v after %s, not inside shutdownGrace (%v): "+
					"the drain ran into the deadline instead of finishing", elapsed, sig, shutdownGrace)
			}
		})
	}
}

// pipeListener is an in-memory listener whose connections are net.Pipe pairs.
// A net.Pipe write blocks until the peer reads it, so a client that stops
// reading holds the server's write for good: a response wedged with no buffer
// in between. Everything here blocks on channels, which keeps the server's
// goroutines durably blocked inside a synctest bubble.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: defaultPort}
}

// dial hands the server end of a fresh pipe to Accept and returns the client
// end.
func (l *pipeListener) dial() net.Conn {
	server, client := net.Pipe()
	l.conns <- server
	return client
}

// TestRunServer_SignalDrainIsBoundedByShutdownGrace drives the "bounded" half of
// step 7's second promise.
//
// The client reads the response head and then never reads again, so the
// handler stays blocked in Write and http.Server.Shutdown can never go
// quiescent. The only thing that can end runServer is the deadline it puts on
// Shutdown, so the time it takes is a direct reading of shutdownGrace: not
// before it, which would be a connection cut instead of waited for, and not
// long after it, which would be a wait bounded by nothing but the server's 30 s
// WriteTimeout.
//
// It runs in a synctest bubble, so the clock is the bubble's: the reading is
// exact, and the test takes no real time.
func TestRunServer_SignalDrainIsBoundedByShutdownGrace(t *testing.T) {
	path, asset := largestStaticAsset(t)
	for _, sig := range stopSignals {
		t.Run(sig.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ln := newPipeListener()
				sigCh := make(chan os.Signal, 1)
				done := make(chan error, 1)
				go func() { done <- runServer(ln, sigCh, newHostPolicy("127.0.0.1", defaultPort)) }()

				conn := ln.dial()
				// Closing the client end is what finally releases the wedged
				// handler, so the bubble's goroutines can all exit.
				defer conn.Close() //nolint:errcheck // test cleanup

				req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+path, nil)
				if err != nil {
					t.Fatalf("building the request: %v", err)
				}
				req.Close = true
				if err := req.Write(conn); err != nil {
					t.Fatalf("sending the request: %v", err)
				}
				br := bufio.NewReader(conn)
				resp, err := http.ReadResponse(br, req)
				if err != nil {
					t.Fatalf("reading the response head: %v", err)
				}
				// The response is sent with Connection: close, so closing the
				// body does not read the rest of it.
				defer resp.Body.Close() //nolint:errcheck // test cleanup
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
				}
				if resp.ContentLength != int64(len(asset)) {
					t.Fatalf("declared Content-Length = %d, want the asset's %d bytes",
						resp.ContentLength, len(asset))
				}
				// From here the client reads nothing. Once every goroutine in
				// the bubble is blocked, the handler is wedged in Write.
				synctest.Wait()
				if got := br.Buffered(); got >= len(asset) {
					t.Fatalf("the client already holds %d bytes, the whole asset: nothing is in flight", got)
				}

				signalled := time.Now()
				sigCh <- sig
				rerr := <-done
				elapsed := time.Since(signalled)

				if rerr != nil {
					t.Fatalf("runServer = %v after %s, want nil", rerr, sig)
				}
				if elapsed < shutdownGrace {
					t.Fatalf("runServer returned %v after %s with a response still in flight, "+
						"before shutdownGrace (%v): the connection was cut instead of allowed "+
						"the bounded period", elapsed, sig, shutdownGrace)
				}
				// The deadline fires at exactly shutdownGrace on the bubble's
				// clock; the second of slack only keeps the assertion free of
				// net/http's internal polling cadence.
				if elapsed > shutdownGrace+time.Second {
					t.Fatalf("runServer returned %v after %s, past shutdownGrace (%v): the "+
						"drain was not bounded by it", elapsed, sig, shutdownGrace)
				}
			})
		})
	}
}
