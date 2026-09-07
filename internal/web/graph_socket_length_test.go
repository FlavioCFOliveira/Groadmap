// Package web — the graph data endpoint against a derived socket path the
// platform cannot hold (rmp task #412).
//
// This endpoint publishes no --socket flag and has nowhere to receive one
// (SPEC/GRAPH.md § Socket Path and Permissions, rule 2), so only the DERIVED half
// of the path-length rule can reach it — and for a derived path the rule is to
// carry on. A roadmap whose home directory is deep enough to push
// ~/.roadmaps/<name>/graph.sock over the bound loses the server and keeps the
// page, because the constraint binds sockets and the store is not a socket
// (§ Socket Path Length, rule 6; § Server Resolution, rule 12).
package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
)

// deepHome returns a HOME so deep that the socket derived from a roadmap under it
// is longer than the platform allows a socket path to be.
//
// It is built from nested directories rather than one enormous component because
// a single filename is itself capped (NAME_MAX), and the shape it produces is the
// one the bound is actually met in: an ordinary roadmap name under a workspace
// several directories down. The derived path's length is ASSERTED before the test
// runs, so a short $TMPDIR cannot quietly turn this into a test of the ordinary
// case.
func deepHome(t *testing.T) string {
	t.Helper()

	root, err := os.MkdirTemp("", "rmpw")
	if err != nil {
		t.Fatalf("creating a temporary root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) }) //nolint:errcheck // a temporary directory the test is done with

	const segment = "deeply-nested-agent-workspace"
	home := root
	for len(home) < graphclient.MaxSocketPathLen {
		home = filepath.Join(home, segment)
	}
	home = filepath.Join(home, "home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatalf("creating a deep HOME: %v", err)
	}
	return home
}

// TestResolveGraphServerForRequest_ADerivedPathOverTheBoundIsNotServed is the
// web half of SPEC/GRAPH.md Acceptance Criterion 66.
//
// Both subtests assert the SAME outcome from two different starting states, and
// the second is the one that proves the settlement precedes the probe.
func TestResolveGraphServerForRequest_ADerivedPathOverTheBoundIsNotServed(t *testing.T) {
	t.Run("no file at the derived path", func(t *testing.T) {
		home := deepHome(t)
		t.Setenv("HOME", home)
		name := seedRoadmap(t, "backend-platform")

		derived, err := graphclient.SocketPath(name)
		if err != nil {
			t.Fatalf("deriving the socket path: %v", err)
		}
		if !graphclient.SocketPathTooLong(derived) {
			t.Fatalf("the derived path is %d bytes and the bound is %d, so this test is exercising "+
				"the ordinary case rather than the one it exists for: %s",
				len(derived), graphclient.MaxSocketPathLen, derived)
		}

		socket, err := resolveGraphServerForRequest(context.Background(), name)
		if err != nil {
			t.Fatalf("a derived path over the bound reported %v. No server can exist there, so it is "+
				"a definite negative and the request opens the store, exactly as it does for a "+
				"socket that is absent", err)
		}
		if socket != "" {
			t.Errorf("socket = %q, want none: no process on this platform can bind a %d-byte path",
				socket, len(derived))
		}
	})

	t.Run("an ordinary file at the derived path", func(t *testing.T) {
		// The same path with a FILE at it. Probing that would report
		// StateUnreachable and answer HTTP 500 -- the right answer for a path a
		// socket could lawfully occupy, and the wrong one here. Reaching "not
		// served" from this state is only possible if the length was settled
		// BEFORE the probe.
		home := deepHome(t)
		t.Setenv("HOME", home)
		name := seedRoadmap(t, "backend-platform")

		derived, err := graphclient.SocketPath(name)
		if err != nil {
			t.Fatalf("deriving the socket path: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(derived), 0700); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(derived), err)
		}
		if err := os.WriteFile(derived, []byte("not a socket"), 0600); err != nil {
			t.Fatalf("writing %s: %v", derived, err)
		}

		socket, err := resolveGraphServerForRequest(context.Background(), name)
		if err != nil {
			t.Fatalf("a derived path over the bound with a file at it reported %v; the length was "+
				"settled after the probe rather than before it", err)
		}
		if socket != "" {
			t.Errorf("socket = %q, want none", socket)
		}
	})
}

// TestResolveGraphServerForRequest_TheBoundIsNotAppliedToAShortPath is the
// negative control for the test above.
//
// Without it, the two subtests there would also pass on an endpoint that had
// simply stopped resolving servers altogether — which would withdraw the served
// path from every roadmap rather than from the ones whose derived path is too
// long.
func TestResolveGraphServerForRequest_TheBoundIsNotAppliedToAShortPath(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "backend-platform")

	derived, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path: %v", err)
	}
	if graphclient.SocketPathTooLong(derived) {
		t.Fatalf("the control's derived path is already over the bound (%d bytes, limit %d); the "+
			"short HOME this test relies on is not short: %s",
			len(derived), graphclient.MaxSocketPathLen, derived)
	}
	scriptedGraphServer(t, derived)

	socket, err := resolveGraphServerForRequest(context.Background(), name)
	if err != nil {
		t.Fatalf("resolveGraphServerForRequest: %v", err)
	}
	if socket != derived {
		t.Errorf("socket = %q, want the derived path %q. A short derived path with a server behind "+
			"it must still resolve as served; the bound applies to the path's length and to "+
			"nothing else", socket, derived)
	}
	if strings.TrimSpace(socket) == "" {
		t.Error("a served roadmap resolved to no socket at all")
	}
}
