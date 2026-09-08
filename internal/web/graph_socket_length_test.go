// Package web — the graph data endpoint against a derived socket path the
// platform cannot hold (rmp tasks #412 and #427).
//
// This endpoint publishes no --socket flag and has nowhere to receive one
// (SPEC/GRAPH.md § Socket Path and Permissions, rule 2), so only the DERIVED half
// of the path-length rule can reach it. Task #412 shipped that half as a fall
// back: a roadmap whose home directory was deep enough to push
// ~/.roadmaps/<name>/graph.sock over the bound lost the server and kept the page,
// on the reasoning that the constraint binds sockets and the store is not a
// socket.
//
// Task #427 replaced that reasoning with the uniform rule, and this file asserts
// the opposite outcome. A roadmap whose derived socket path cannot be bound has a
// real and permanent fault in its layout, and a rule that refused `rmp graph
// serve` while letting this endpoint open the store reported the fault at one
// surface and concealed it at another — leaving an operator to watch the server
// fail while the page worked, with nothing to connect the two. The endpoint now
// refuses the request with HTTP 500 and no `kind`, and the cost is stated rather
// than avoided: this surface cannot be pointed at a shorter path, so the only
// remedy is a shorter home directory or a shorter roadmap name
// (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 1, and Acceptance
// Criterion 160; SPEC/GRAPH.md § Socket Path Length, rules 5 and 6).
//
// # The bound is measured here, never written down
//
// Every length in this file derives from a figure it measures itself, by binding
// real AF_UNIX listeners at increasing path lengths until one is refused. That is
// what SPEC/WEB.md Acceptance Criterion 160 requires in as many words, and the
// reason is the one GRAPH.md Acceptance Criterion 67 gives: the bound is 107
// bytes on Linux and Windows and 103 on macOS, FreeBSD and OpenBSD, so a file
// that took its figure from a literal — or from the very constant the code under
// test applies — would agree with a wrong implementation instead of catching it.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// bindDir returns a directory short enough to build measured socket paths inside.
//
// It is created directly under the system temporary directory with a short
// prefix, because t.TempDir names its directory after the test and a descriptive
// name would leave no measurable range at all.
func bindDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "rmpb")
	if err != nil {
		t.Fatalf("creating a short measurement directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // a temporary directory the test is done with
	return dir
}

// bindableAt reports whether the kernel accepts a Unix domain socket whose
// absolute path inside dir is exactly n bytes long.
func bindableAt(t *testing.T, dir string, n int) bool {
	t.Helper()

	padding := n - len(dir) - 1
	if padding < 1 {
		t.Fatalf("cannot build a %d-byte path inside %q", n, dir)
	}
	path := filepath.Join(dir, strings.Repeat("s", padding))
	if len(path) != n {
		t.Fatalf("built a %d-byte path where %d was wanted: %s", len(path), n, path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return false
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("closing the listener at %d bytes: %v", n, err)
	}
	_ = os.Remove(path) //nolint:errcheck // Close already unlinks it on every supported platform
	return true
}

// measuredSocketPathBound returns the greatest socket-path length this platform
// will bind, found by binding real sockets at increasing lengths until one is
// refused.
//
// The figure returned is the largest length that SUCCEEDED, which is a pure
// kernel observation. The refusals above it are corroboration, and four of them
// are required so that a single anomalous length cannot be mistaken for the
// boundary.
func measuredSocketPathBound(t *testing.T, dir string) int {
	t.Helper()

	const floor, ceiling = 64, 400
	largest := -1
	for length := floor; length <= ceiling; length++ {
		if bindableAt(t, dir, length) {
			largest = length
			continue
		}
		if largest < 0 {
			t.Fatalf("no socket path bound at any length from %d bytes upward; the measurement "+
				"directory %q is unusable", floor, dir)
		}
		if largest != length-1 {
			t.Fatalf("the boundary is not contiguous: the largest length that bound was %d but %d "+
				"is the first refused", largest, length)
		}
		for over := length; over <= min(length+4, ceiling); over++ {
			if bindableAt(t, dir, over) {
				t.Fatalf("a path of %d bytes bound after %d was refused, so the refusal at %d was "+
					"not the platform's bound", over, length, length)
			}
		}
		return largest
	}
	t.Fatalf("no socket path was refused at any length up to %d bytes; this platform appears to "+
		"have no sun_path bound, which contradicts every target SPEC/BUILD.md declares", ceiling)
	return 0
}

// deepHome returns a HOME so deep that the socket derived for roadmapName under
// it is longer than the MEASURED bound.
//
// It is built from nested directories rather than one enormous component because
// a single filename is itself capped (NAME_MAX), and the shape it produces is the
// one the bound is actually met in: an ordinary roadmap name under a workspace
// several directories down. The loop measures the DERIVED PATH rather than the
// home directory, so a short $TMPDIR cannot quietly turn a test of the deep case
// into a test of the ordinary one; every caller asserts it again before relying
// on it.
func deepHome(t *testing.T, bound int, roadmapName string) string {
	t.Helper()

	root, err := os.MkdirTemp("", "rmpw")
	if err != nil {
		t.Fatalf("creating a temporary root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) }) //nolint:errcheck // a temporary directory the test is done with

	const segment = "deeply-nested-agent-workspace"
	derivedUnder := func(home string) string {
		return filepath.Join(home, ".roadmaps", roadmapName, graphclient.SocketFileName)
	}
	home := root
	for len(derivedUnder(home)) <= bound {
		home = filepath.Join(home, segment)
	}
	home = filepath.Join(home, "home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatalf("creating a deep HOME: %v", err)
	}
	return home
}

// assertDerivedPathIsOverTheBound is the fixture guard every test below shares.
func assertDerivedPathIsOverTheBound(t *testing.T, name string, bound int) string {
	t.Helper()

	derived, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path: %v", err)
	}
	if len(derived) <= bound {
		t.Fatalf("the derived path is %d bytes and this platform was MEASURED to bind at most %d, "+
			"so this test is exercising the ordinary case rather than the one it exists for: %s",
			len(derived), bound, derived)
	}
	return derived
}

// TestResolveGraphServerForRequest_ADerivedPathOverTheBoundIsRefused is the web
// half of SPEC/GRAPH.md Acceptance Criterion 66.
//
// Both subtests assert the SAME outcome from two different starting states, and
// the second is the one that proves the settlement precedes the probe: an
// ordinary file at the derived path would probe as StateUnreachable, which is a
// different condition with a different message, so reaching the refusal from that
// state is only possible if the length was settled first.
func TestResolveGraphServerForRequest_ADerivedPathOverTheBoundIsRefused(t *testing.T) {
	bound := measuredSocketPathBound(t, bindDir(t))

	assertRefusal := func(t *testing.T, socket string, err error, derived string) {
		t.Helper()

		if err == nil {
			t.Fatalf("the endpoint resolved a %d-byte derived path to %q instead of refusing it. "+
				"No process can bind it, so no server can EVER answer there — which is not the "+
				"fact the two definite negatives report and is not served by the answer they get "+
				"(SPEC/GRAPH.md § Socket Path Length, rules 5 and 6)", len(derived), socket)
		}
		if !errors.Is(err, utils.ErrGraphServer) {
			t.Errorf("error = %v, want utils.ErrGraphServer, which is what carries this refusal to "+
				"the handler's internal-error branch and HTTP 500", err)
		}
		if socket != "" {
			t.Errorf("socket = %q, want none alongside the refusal", socket)
		}
		if strings.Contains(err.Error(), "unreachable") {
			t.Errorf("error = %v: the endpoint reported the socket as unreachable, so the path's "+
				"length was settled AFTER the probe rather than before it "+
				"(SPEC/GRAPH.md § Server Resolution, rule 12)", err)
		}
		if !strings.Contains(err.Error(), derived) {
			t.Errorf("error = %v, want it to name the derived path %q", err, derived)
		}
	}

	t.Run("no file at the derived path", func(t *testing.T) {
		const roadmap = "backend-platform"
		t.Setenv("HOME", deepHome(t, bound, roadmap))
		name := seedRoadmap(t, roadmap)
		derived := assertDerivedPathIsOverTheBound(t, name, bound)

		socket, err := resolveGraphServerForRequest(context.Background(), name)
		assertRefusal(t, socket, err, derived)
	})

	t.Run("an ordinary file at the derived path", func(t *testing.T) {
		const roadmap = "backend-platform"
		t.Setenv("HOME", deepHome(t, bound, roadmap))
		name := seedRoadmap(t, roadmap)
		derived := assertDerivedPathIsOverTheBound(t, name, bound)

		if err := os.MkdirAll(filepath.Dir(derived), 0700); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(derived), err)
		}
		if err := os.WriteFile(derived, []byte("not a socket"), 0600); err != nil {
			t.Fatalf("writing %s: %v", derived, err)
		}

		socket, err := resolveGraphServerForRequest(context.Background(), name)
		assertRefusal(t, socket, err, derived)
	})
}

// TestResolveGraphServerForRequest_TheBoundIsNotAppliedToAShortPath is the
// negative control for the test above.
//
// Without it, the two subtests there would also pass on an endpoint that had
// simply stopped resolving servers altogether — or that refused every roadmap —
// which would withdraw the graph from every installation rather than from the
// ones whose derived path cannot be bound.
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

// TestHandleGraphData_ADerivedPathOverTheBoundRefusesTheRequest is SPEC/WEB.md
// Acceptance Criterion 160, driven through the mux rather than through the
// resolver, so the status the criterion names is the status a client would see.
//
// The roadmap has never run a graph statement, which is what makes the two
// assertions after the status worth making. A `500` on its own separates nothing
// — it is also the answer to a store that cannot be opened and to a lock the
// bounded wait does not win — so the criterion pairs it with the absence of a
// `kind`, which is what tells this refusal apart from the query-bar failures that
// carry one, and with the absence of ~/.roadmaps/<roadmap>/graph/, which is what
// tells a refusal apart from a request that went on to look at a store.
//
// The control at the end is what makes the status a discriminator: under a SHORT
// home the same request against the same never-used roadmap is answered 200 with
// the empty graph, so a `500` here cannot be an endpoint that had simply stopped
// answering.
func TestHandleGraphData_ADerivedPathOverTheBoundRefusesTheRequest(t *testing.T) {
	bound := measuredSocketPathBound(t, bindDir(t))

	t.Run("a derived path over the measured bound", func(t *testing.T) {
		const roadmap = "backend-platform"
		t.Setenv("HOME", deepHome(t, bound, roadmap))
		name := seedRoadmap(t, roadmap)
		assertDerivedPathIsOverTheBound(t, name, bound)

		rec := doGraphData(t, name, nil)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusInternalServerError,
				rec.Body.String())
		}

		// No `kind`. The query-bar error shape belongs to the 400s and carries a
		// kind because a statement of the caller's failed; this request never
		// reached a statement (SPEC/WEB.md § Query-Bar Error Handling, rule 6).
		body := rec.Body.String()
		if strings.Contains(body, "kind") {
			t.Errorf("the response body names a kind: %q. This refusal introduces no new status "+
				"and no kind, because the request never reached a statement", body)
		}
		var decoded map[string]any
		if json.Unmarshal(rec.Body.Bytes(), &decoded) == nil {
			if _, has := decoded["kind"]; has {
				t.Errorf("the response carries a kind key: %#v", decoded)
			}
		}

		// The store was not opened. The graph directory is created on first use
		// (SPEC/GRAPH.md § Persistence Layout, rule 2), so its absence after a
		// request against a roadmap that has never run a statement is the
		// observation that the request stopped before the store.
		roadmapDir, err := utils.GetRoadmapDir(name)
		if err != nil {
			t.Fatalf("resolving the roadmap directory: %v", err)
		}
		graphDir := filepath.Join(roadmapDir, "graph")
		if _, statErr := os.Stat(graphDir); !os.IsNotExist(statErr) {
			t.Errorf("%s exists after a refused request (stat error %v); the request reached the "+
				"store rather than stopping at the socket path", graphDir, statErr)
		}
	})

	// The second and third answers of Acceptance Criterion 160. The criterion
	// drives ONE request three times and turns on all three: 500 alone is
	// satisfied by an endpoint that fails for any reason, 503 alone by one that
	// never reaches a server, and 200 alone by one that ignores the bound
	// entirely. What the triple establishes is that the endpoint tells a
	// permanent defect in the roadmap's layout apart from a dependency the
	// operator has not started, and both apart from success.
	t.Run("a short derived path with NO server is 503, not 500", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		name := seedRoadmap(t, "backend-platform")

		derived, err := graphclient.SocketPath(name)
		if err != nil {
			t.Fatalf("deriving the socket path: %v", err)
		}
		if len(derived) > bound {
			t.Fatalf("the control's derived path is %d bytes and the measured bound is %d; the "+
				"short HOME this subtest relies on is not short: %s", len(derived), bound, derived)
		}

		rec := doGraphData(t, name, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 for a roadmap whose derived path fits and which no "+
				"server is serving. The path is bindable, so the condition is a dependency the "+
				"operator has not started, and it is transitory; body=%q",
				rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "kind") {
			t.Errorf("the 503 body names a kind: %q; neither 5xx reached a statement",
				rec.Body.String())
		}
	})

	t.Run("a short derived path WITH a server is 200", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		name := seedRoadmap(t, "backend-platform")

		derived, err := graphclient.SocketPath(name)
		if err != nil {
			t.Fatalf("deriving the socket path: %v", err)
		}
		if len(derived) > bound {
			t.Fatalf("the derived path is %d bytes and the measured bound is %d: %s",
				len(derived), bound, derived)
		}
		scriptedGraphServer(t, derived)

		rec := doGraphData(t, name, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 for a roadmap whose derived path fits and which a "+
				"server is serving; body=%q", rec.Code, rec.Body.String())
		}
		var view graphView
		if decodeErr := json.Unmarshal(rec.Body.Bytes(), &view); decodeErr != nil {
			t.Fatalf("decoding the graph view: %v; body=%q", decodeErr, rec.Body.String())
		}
		// The scripted server answers with one node, so this half asserts the
		// RESULT and not merely that nothing failed: an endpoint that answered
		// 200 with an empty graph would have proved only the former.
		if len(view.Nodes) != 1 {
			t.Errorf("graph carries %d node(s), want the one the server answered with: %+v",
				len(view.Nodes), view)
		}
	})
}
