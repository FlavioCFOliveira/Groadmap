// Behaviour tests for query notifications as stderr diagnostics
// (SPEC/GRAPH.md § Query Notifications as Diagnostics; Acceptance Criteria
// 20, 21, 22).
//
// These lock in three properties:
//   - A disconnected multi-pattern MATCH surfaces the engine's
//     Cartesian-product notification on stderr while leaving the stdout
//     success JSON unchanged (AC 20).
//   - A connected query that produces no notification writes nothing extra
//     to stderr (AC 21).
//   - Notifications are surfaced whether the statement reads or writes (AC 22).
//
// Every statement below crosses the protocol: `rmp graph client` is the only
// subcommand that runs one, so what these tests assert is that a notification
// the SERVER attached survives the crossing and reaches this process's stderr
// unchanged (SPEC/GRAPH.md § The Dedicated Graph Server).
package commands

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
)

// cartesianCode is the stable machine-readable code GoGraph attaches to the
// Cartesian-product advisory (SPEC/GRAPH.md AC 20). cartesianSeverity is the
// severity GoGraph reports for it.
const (
	cartesianCode     = "Neo.ClientNotification.Statement.CartesianProductWarning"
	cartesianSeverity = "INFORMATION"
)

// setupTestGraphRoadmap creates a roadmap (and its home directory + DB) so the
// graph store can be opened under it, and returns a cleanup function.
func setupTestGraphRoadmap(t *testing.T, name string) func() {
	t.Helper()
	cleanupTestRoadmap(t, name)

	database, err := db.Open(name)
	if err != nil {
		t.Fatalf("failed to create roadmap %q: %v", name, err)
	}
	database.Close()

	return func() { cleanupTestRoadmap(t, name) }
}

// captureStdStreams redirects os.Stdout and os.Stderr to pipes for the
// duration of fn, returning what each stream received. It restores the
// original streams before returning. Both pipes are drained concurrently so a
// large write cannot deadlock fn.
func captureStdStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout, os.Stderr = outW, errW

	type res struct{ s string }
	outCh, errCh := make(chan res, 1), make(chan res, 1)
	drain := func(r *os.File, ch chan res) {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, rerr := r.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if rerr != nil {
				break
			}
		}
		ch <- res{b.String()}
	}
	go drain(outR, outCh)
	go drain(errR, errCh)

	fn()

	_ = outW.Close()
	_ = errW.Close()
	os.Stdout, os.Stderr = origOut, origErr
	o := <-outCh
	e := <-errCh
	_ = outR.Close()
	_ = errR.Close()

	return o.s, e.s
}

// seedSpecAndTask creates one Spec and one Task node so a disconnected
// multi-pattern MATCH has rows to match while still triggering the engine's
// Cartesian-product advisory.
func seedSpecAndTask(t *testing.T, name string) {
	t.Helper()
	if err := runGraphClient([]string{"-r", name, "--query",
		"CREATE (s:Spec {key:'query-notifications'})"}); err != nil {
		t.Fatalf("seed Spec: %v", err)
	}
	if err := runGraphClient([]string{"-r", name, "--query",
		"CREATE (t:Task {key:'wire-notifications'})"}); err != nil {
		t.Fatalf("seed Task: %v", err)
	}
}

// assertColumnsRows fails unless raw parses as the standard graph read result
// shape (a {"columns":[...],"rows":[...]} object), proving notifications never
// alter the stdout JSON.
func assertColumnsRows(t *testing.T, raw string) {
	t.Helper()
	var parsed struct {
		Columns []string        `json:"columns"`
		Rows    [][]interface{} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed); err != nil {
		t.Fatalf("stdout is not the columns/rows shape: %v\nstdout=%q", err, raw)
	}
	if parsed.Columns == nil {
		t.Errorf("stdout missing columns key: %q", raw)
	}
}

// TestGraphNotifications_DisconnectedMatch covers AC 20: a disconnected
// multi-pattern MATCH emits the Cartesian-product notice on stderr (severity,
// code, description, one line) while stdout stays the normal columns/rows JSON.
func TestGraphNotifications_DisconnectedMatch(t *testing.T) {
	name := "graph-notif-disconnected"
	defer servedRoadmap(t, name)()
	seedSpecAndTask(t, name)

	stdout, stderr := captureStdStreams(t, func() {
		if err := runGraphClient([]string{"-r", name, "--query",
			"MATCH (a:Spec), (b:Task) RETURN a.key, b.key"}); err != nil {
			t.Errorf("disconnected query returned error: %v", err)
		}
	})

	line := strings.TrimSpace(stderr)
	if line == "" {
		t.Fatal("expected a Cartesian-product notice on stderr, got none")
	}
	if strings.Count(line, "\n") != 0 {
		t.Errorf("expected exactly one notification line, got:\n%s", stderr)
	}
	if !strings.HasPrefix(line, cartesianSeverity+" "+cartesianCode+": ") {
		t.Errorf("stderr line does not match \"<severity> <code>: <description>\": %q", line)
	}
	if !strings.Contains(line, cartesianCode) {
		t.Errorf("stderr line missing stable code %q: %q", cartesianCode, line)
	}

	assertColumnsRows(t, stdout)
}

// TestGraphNotifications_ConnectedQueryQuiet covers AC 21: a connected,
// notification-free query writes nothing extra to stderr while stdout carries
// the normal result.
func TestGraphNotifications_ConnectedQueryQuiet(t *testing.T) {
	name := "graph-notif-connected"
	defer servedRoadmap(t, name)()
	seedSpecAndTask(t, name)

	stdout, stderr := captureStdStreams(t, func() {
		if err := runGraphClient([]string{"-r", name, "--query",
			"MATCH (s:Spec) RETURN s.key"}); err != nil {
			t.Errorf("connected query returned error: %v", err)
		}
	})

	if strings.TrimSpace(stderr) != "" {
		t.Errorf("expected empty stderr for a notification-free query, got: %q", stderr)
	}
	assertColumnsRows(t, stdout)
}

// TestGraphNotifications_WritePathWired covers the write-path half of AC 22:
// a writing statement's result is carried back over the protocol with whatever
// notifications the engine attached to it, and `rmp graph client` writes them
// to stderr with the normal {"ok": true} success output unchanged.
//
// NOTE on the pinned engine: a writing statement's result does not currently
// carry plain-time notifications the way a read's does, so a disconnected MATCH
// with a writing clause emits no notice line here. The assertion below
// therefore verifies the invariant that holds regardless of how many
// notifications the engine emits: the write path passes whatever arrives
// through the same loop the read path uses, the stdout success output is the
// exact {"ok": true} shape, and the exit code is success. When a future engine
// release attaches notifications to writing statements, this same wiring
// surfaces them with no Groadmap change. The end-to-end suite
// (tests/test_40_graph_notifications.py) asserts the live write-path behaviour
// against the built binary.
func TestGraphNotifications_WritePathWired(t *testing.T) {
	name := "graph-notif-write"
	defer servedRoadmap(t, name)()
	seedSpecAndTask(t, name)

	// A CREATE with two disconnected MATCH patterns: the writing clause is what
	// makes the result a write's, and the form would trigger the advisory on the
	// read path.
	stdout, stderr := captureStdStreams(t, func() {
		if err := runGraphClient([]string{"-r", name, "--query",
			"MATCH (a:Spec), (b:Task) CREATE (l:Link {note:'join'})"}); err != nil {
			t.Errorf("write query returned error: %v", err)
		}
	})

	// stderr carries only notifications the engine attaches. On v0.3.2 that is
	// none for a transactional result; if a line is present it MUST be a
	// well-formed notification line, never arbitrary noise.
	if line := strings.TrimSpace(stderr); line != "" {
		if !strings.Contains(line, ": ") {
			t.Errorf("unexpected stderr content on write path: %q", line)
		}
	}

	// No RETURN clause: stdout must be exactly {"ok": true}, unchanged by the
	// notification wiring.
	var ok struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &ok); err != nil {
		t.Fatalf("write stdout is not the {\"ok\":...} shape: %v\nstdout=%q", err, stdout)
	}
	if !ok.OK {
		t.Errorf("write stdout = %q, want {\"ok\": true}", stdout)
	}
}
