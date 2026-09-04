// Package web — the graph data endpoint's served path.
//
// # The regression this file exists for
//
// Task #367 shipped `rmp graph serve`, and measured the consequence: with a
// server running, a graph data request for that roadmap waited the whole wait
// budget — 7.51 s — and answered HTTP 500, deterministically, on every request,
// for as long as that server ran. The cause is not contention: a server holds the
// store's exclusive advisory lock for its whole PROCESS LIFETIME, and no finite
// wait can be sized against a hold with no upper bound.
//
// SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 1, closes it by
// resolving the roadmap's socket BEFORE deciding what to open. The test that
// proves it is the one below, and its shape is what makes it a proof rather than
// a demonstration: the store's exclusive lock is HELD for the whole request, as a
// running server would hold it, and the request must still succeed. A request
// that took the direct path could not.
//
// # Why the server here is scripted
//
// The endpoint needs a socket that completes the Bolt handshake and answers a
// statement. Assembling a real engine would make this an integration test of
// GoGraph; scripting the server's half of the protocol with the engine's own
// proto package keeps the test about the ENDPOINT while staying wire-faithful.
// The real-server case is covered where a real server can be assembled:
// internal/graphserve.
package web

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// shortHome is a HOME under which the roadmap's DERIVED socket path fits.
//
// A Unix domain socket path is capped at 108 bytes, and t.TempDir() names its
// directory after the test — so a descriptive test name pushes
// <home>/.roadmaps/<name>/graph.sock past the cap and the bind fails with
// "invalid argument", which reads as a defect in the code under test rather than
// in the harness. It is the constraint rmp task #367 measured (FINDING #266)
// reaching a second harness, exactly as that finding predicted it would.
//
// The directory is created directly under the system temporary directory with a
// short prefix, and removed when the test ends.
func shortHome(t *testing.T) string {
	t.Helper()

	home, err := os.MkdirTemp("", "rmpw")
	if err != nil {
		t.Fatalf("creating a short HOME: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) }) //nolint:errcheck // a temporary directory the test is done with
	return home
}

// scriptedGraphServer answers the handshake and a fixed RUN/PULL exchange on the
// roadmap's derived socket.
//
// It returns one node so the assertion below can be about the RESULT and not
// merely about the status: an endpoint that answered 200 with an empty graph
// would have proved that nothing failed rather than that the statement ran.
func scriptedGraphServer(t *testing.T, socket string) (stop func()) {
	t.Helper()

	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("binding %s: %v", socket, err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go scriptedGraphSession(conn)
		}
	}()

	stop = func() {
		_ = ln.Close() //nolint:errcheck // the scripted listener is done with
		<-done
	}
	t.Cleanup(stop)
	return stop
}

// scriptedGraphSession drives one connection through the exchange the endpoint
// performs: handshake, HELLO, LOGON, RUN, PULL.
func scriptedGraphSession(conn net.Conn) {
	defer conn.Close() //nolint:errcheck // the scripted server has nothing to report

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := proto.Negotiate(ctx, conn); err != nil {
		return
	}

	reader := proto.NewChunkedReader(conn)
	writer := proto.NewChunkedWriter(conn)
	write := func(responses ...any) bool {
		for _, response := range responses {
			var buf bytes.Buffer
			enc := packstream.NewEncoder(&buf)
			if err := proto.EncodeResponse(enc, response); err != nil {
				return false
			}
			if err := enc.Flush(); err != nil {
				return false
			}
			if err := writer.WriteMessage(buf.Bytes()); err != nil {
				return false
			}
		}
		return true
	}

	for {
		msg, err := reader.ReadMessage()
		if err != nil {
			return
		}
		request, err := proto.DecodeRequest(packstream.NewDecoder(bytes.NewReader(msg)))
		if err != nil {
			return
		}
		switch request.(type) {
		case *proto.Run:
			if !write(&proto.Success{Metadata: map[string]packstream.Value{
				"fields": []packstream.Value{"n"},
				"qid":    int64(-1),
			}}) {
				return
			}
		case *proto.Pull:
			node := packstream.Struct{Tag: 0x4E, Fields: []packstream.Value{
				int64(140),
				[]packstream.Value{"Spec"},
				map[string]packstream.Value{"key": "user-authentication"},
				"140",
			}}
			if !write(
				&proto.Record{Data: []packstream.Value{node}},
				&proto.Success{Metadata: map[string]packstream.Value{"has_more": false}},
			) {
				return
			}
		default:
			if !write(&proto.Success{Metadata: map[string]packstream.Value{}}) {
				return
			}
		}
	}
}

// TestLoadGraphView_ServedRoadmapNeitherWaitsForTheLockNorTakesIt is the
// regression test for the state task #367 measured and this task closes.
//
// The exclusive advisory lock is held for the whole request, which is what a
// running server does for its entire process lifetime. Before resolution existed,
// this request waited the full wait budget and then failed; it must now answer
// from the server without touching the lock at all.
//
// Two things are asserted and both are needed. The RESULT, because a 200 carrying
// an empty graph would prove only that nothing failed; and the ELAPSED TIME,
// because a request that somehow served itself from the store after waiting would
// otherwise look identical from the outside.
func TestLoadGraphView_ServedRoadmapNeitherWaitsForTheLockNorTakesIt(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "backend-platform")

	graphDir := webGraphDir(t, name)
	if err := os.MkdirAll(graphDir, 0700); err != nil {
		t.Fatalf("creating %s: %v", graphDir, err)
	}

	// The hold a running server keeps for its whole process lifetime. It is NOT
	// released during the request: that is the point.
	release, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		t.Fatalf("taking the exclusive lock: %v", err)
	}
	defer release()

	socket, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path: %v", err)
	}
	scriptedGraphServer(t, socket)

	started := time.Now()
	view, err := loadGraphView(context.Background(), name, "", "")
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("a graph data request against a SERVED roadmap failed with %v after %v. Resolving "+
			"the socket first is what stops a running server from disabling this endpoint: the "+
			"server holds the store's exclusive lock for its process lifetime, and no finite wait "+
			"can be sized against such a hold (SPEC/WEB.md § Knowledge Graph from the GoGraph "+
			"Store, rule 1)", err, elapsed)
	}
	if len(view.Nodes) != 1 {
		t.Fatalf("the response carries %d node(s), want the one the server returned; a 200 with an "+
			"empty graph would prove only that nothing failed", len(view.Nodes))
	}
	properties, ok := view.Nodes[0]["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T, want a map", view.Nodes[0]["properties"])
	}
	if properties["key"] != "user-authentication" {
		t.Errorf("properties.key = %v, want the server's own value: the node came back through the "+
			"protocol and was mapped by the SAME collector the direct path runs", properties["key"])
	}
	if got := view.Nodes[0]["id"]; got != uint64(140) {
		t.Errorf("node id = %v, want the server's own 140", got)
	}

	if elapsed >= graphlock.WaitBudget() {
		t.Errorf("the request took %v, which reached the lock's %v wait budget. A served request "+
			"takes no lock and waits for none", elapsed, graphlock.WaitBudget())
	}
}

// TestLoadGraphView_ResolutionRunsPerRequestAndIsNotCached pins rule 1's last
// clause, and it is not a micro-optimisation question: a cached outcome would act
// on a server that had since stopped, sending a statement into a socket nobody is
// listening on for as long as the cache lived.
//
// The same roadmap is read twice — once served, once after the server has gone —
// and the second read must reach the store rather than the remembered server.
func TestLoadGraphView_ResolutionRunsPerRequestAndIsNotCached(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "payments-platform")
	seedGraph(t, name, "CREATE (s:Spec {key:'payment-capture'})")

	socket, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path: %v", err)
	}
	stop := scriptedGraphServer(t, socket)

	served, err := loadGraphView(context.Background(), name, "", "")
	if err != nil {
		t.Fatalf("the served request failed: %v", err)
	}
	if len(served.Nodes) != 1 || served.Nodes[0]["id"] != uint64(140) {
		t.Fatalf("the first request was not answered by the server: %+v", served.Nodes)
	}

	// The server stops and its socket goes with it, exactly as a clean shutdown
	// leaves things.
	stop()
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		t.Fatalf("removing the socket: %v", err)
	}

	direct, err := loadGraphView(context.Background(), name, "", "")
	if err != nil {
		t.Fatalf("the request after the server stopped failed with %v; resolution runs once per "+
			"request and its outcome is not cached", err)
	}
	if len(direct.Nodes) != 1 {
		t.Fatalf("the direct request carries %d node(s), want the one the store holds", len(direct.Nodes))
	}
	// The discriminator is the PROPERTY and not the id. The store assigns its own
	// identifiers, and one of them can coincide with the id the scripted server
	// returns — it did, on the first run of this test — so an id comparison would
	// report a cached resolution that never happened. The two keys cannot
	// coincide: one is the store's and one is the script's.
	properties, ok := direct.Nodes[0]["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T, want a map", direct.Nodes[0]["properties"])
	}
	if properties["key"] == "user-authentication" {
		t.Fatal("the second request returned the SERVER's node after the server had stopped, so " +
			"the resolution outcome was cached. A cached outcome acts on a server that has since " +
			"stopped (SPEC/GRAPH.md § Server Resolution, rule 9)")
	}
	if properties["key"] != "payment-capture" {
		t.Errorf("properties.key = %v, want the value the STORE holds", properties["key"])
	}
}

// TestResolveGraphServerForRequest_TheThreeOutcomes drives the endpoint's own
// wrapper over the shared resolver.
//
// The two definite negatives must come back as "not served" and NOT as errors,
// because for this endpoint they are the direct path — the state every roadmap
// has been in since before a server existed. The failing state must come back as
// an error, because a socket that answers may belong to a server holding the lock
// and opening the store on that observation is the outcome resolution exists to
// prevent.
func TestResolveGraphServerForRequest_TheThreeOutcomes(t *testing.T) {
	t.Run("no socket is not served and is not an error", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		name := seedRoadmap(t, "backend-platform")

		socket, err := resolveGraphServerForRequest(context.Background(), name)
		if err != nil {
			t.Fatalf("resolveGraphServerForRequest reported %v for a roadmap with no socket", err)
		}
		if socket != "" {
			t.Errorf("socket = %q, want none: a roadmap with no socket is not served", socket)
		}
	})

	t.Run("a served roadmap yields its derived socket", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		name := seedRoadmap(t, "backend-platform")

		derived, err := graphclient.SocketPath(name)
		if err != nil {
			t.Fatalf("deriving the socket path: %v", err)
		}
		scriptedGraphServer(t, derived)

		socket, err := resolveGraphServerForRequest(context.Background(), name)
		if err != nil {
			t.Fatalf("resolveGraphServerForRequest: %v", err)
		}
		if socket != derived {
			t.Errorf("socket = %q, want the DERIVED path %q. This endpoint has no command line and "+
				"no request parameter carries a socket, so the derived path is the only one it can "+
				"resolve (SPEC/GRAPH.md § Serving on a Non-Default Socket, rule 2)", socket, derived)
		}
	})

	t.Run("a path that is not a socket is an internal read error", func(t *testing.T) {
		t.Setenv("HOME", shortHome(t))
		name := seedRoadmap(t, "backend-platform")

		derived, err := graphclient.SocketPath(name)
		if err != nil {
			t.Fatalf("deriving the socket path: %v", err)
		}
		if err := os.WriteFile(derived, []byte("not a socket"), 0600); err != nil {
			t.Fatalf("writing %s: %v", derived, err)
		}

		socket, err := resolveGraphServerForRequest(context.Background(), name)
		if err == nil {
			t.Fatalf("a path that is not a socket resolved to %q with no error; this is not a "+
				"reason to open the store", socket)
		}
		if !errors.Is(err, utils.ErrDatabase) {
			t.Errorf("error = %v, want it to wrap utils.ErrDatabase, which handleGraphData answers "+
				"with HTTP 500 — the status this endpoint already returns for a graph store it "+
				"cannot open", err)
		}
		if _, isQueryError := asGraphQueryError(err); isQueryError {
			t.Error("an unreachable socket was classified as a query-bar failure, which would be " +
				"answered 400. It is an internal read error and is answered 500")
		}
	})
}

// TestServedGraphError_SeparatesTheInternalErrorFromTheExecutionFailures is the
// exhaustive mapping, and the distinction it draws is the one SPEC/WEB.md
// § Knowledge Graph from the GoGraph Store, rule 1, spends four bullets on.
//
// A server that could not be REACHED is an internal read error: nothing ran, and
// the request is answered 500 exactly as a store that cannot be opened is. Every
// other outcome surfaced once the statement was RUNNING, which is where
// § Query-Bar Error Handling, rule 6, already draws the boundary, so each is the
// single execution kind and 400. No new status and no new kind is introduced.
func TestServedGraphError_SeparatesTheInternalErrorFromTheExecutionFailures(t *testing.T) {
	const socket = "/home/user/.roadmaps/backend-platform/graph.sock"

	cases := []struct {
		name          string
		kind          graphclient.Failure
		diagnostic    string
		wantExecution bool
	}{
		{name: "unreachable", kind: graphclient.FailureUnreachable, wantExecution: false},
		{name: "the connection was lost", kind: graphclient.FailureLost, wantExecution: true},
		{name: "the server did not answer", kind: graphclient.FailureUnanswered, wantExecution: true},
		{name: "the budget cut the statement", kind: graphclient.FailureBudget, wantExecution: true},
		{
			name: "the engine refused the statement", kind: graphclient.FailureStatement,
			diagnostic: "cypher: parse: unexpected \"RETURN\"", wantExecution: true,
		},
		{
			name: "a value could not be mapped", kind: graphclient.FailureMapping,
			diagnostic: "unsupported protocol structure tag 0x7A", wantExecution: true,
		},
		{
			name: "every attempt lost a serialisation conflict", kind: graphclient.FailureConflict,
			diagnostic: "mvcc: serialization conflict in node properties", wantExecution: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := servedGraphError(context.Background(), socket, &graphclient.SendError{
				Kind: c.kind, Socket: socket, Diagnostic: c.diagnostic,
				Cause: errors.New("a transport observation"), //nolint:err113 // a fixture standing in for the operating system's own diagnostic
			})

			queryErr, isQueryError := asGraphQueryError(err)
			if isQueryError != c.wantExecution {
				t.Fatalf("classified as a query-bar failure = %v, want %v (a query-bar failure is "+
					"answered 400; anything else is the internal read error, 500)",
					isQueryError, c.wantExecution)
			}
			if !c.wantExecution {
				if !errors.Is(err, utils.ErrDatabase) {
					t.Errorf("error = %v, want it to wrap utils.ErrDatabase so handleGraphData "+
						"answers 500", err)
				}
				return
			}
			if queryErr.Kind != graphErrExecution {
				t.Errorf("kind = %q, want %q: the graph server introduces no new failure kind",
					queryErr.Kind, graphErrExecution)
			}
			if queryErr.Reason == "" {
				t.Error("the failure carries no reason; the reason is what the page shows in place")
			}
		})
	}
}

// TestServedGraphError_TheBudgetLineIsTheDirectPathsOwn pins that an exhausted
// budget reads the same on both paths.
//
// The endpoint has one query time budget and one wording for exhausting it, and
// the server merely happens to be the end that enforced it this time. A second
// wording here would make the page say something different about the same event
// depending on whether a server was running.
func TestServedGraphError_TheBudgetLineIsTheDirectPathsOwn(t *testing.T) {
	ctx := context.Background()

	served := servedGraphError(ctx, "/tmp/graph.sock", &graphclient.SendError{
		Kind: graphclient.FailureBudget, Diagnostic: "context deadline exceeded",
	})
	direct := graphExecutionError(ctx, graphlock.StatementBudget, context.DeadlineExceeded)

	servedErr, ok := asGraphQueryError(served)
	if !ok {
		t.Fatalf("the served budget failure is %T, want a query-bar failure", served)
	}
	if servedErr.Reason != direct.Reason {
		t.Errorf("the served path reports %q where the direct path reports %q; the two must not "+
			"say different things about one budget", servedErr.Reason, direct.Reason)
	}
	if servedErr.Kind != direct.Kind {
		t.Errorf("kind = %q, want the direct path's %q", servedErr.Kind, direct.Kind)
	}
}

// TestServedGraphError_TheContentionLineIsRmpsOwnAndNotTheEngines pins
// SPEC/WEB.md § Query-Bar Error Handling, rule 11, in the two halves that make
// it a rule rather than a preference.
//
// **The status and the kind are unchanged.** An exhausted retry is an execution
// failure, HTTP 400 with kind `execution`, because it surfaced once the
// statement was running, which is where rule 6 draws the boundary. Rule 4 weighs
// and refuses the alternatives — 409 describes a conflict of state that survives
// for the user to resolve, and by the time the endpoint answers, the loser has
// rolled back whole; 503 would announce a service that is unavailable, which a
// server that ran the statement and went on serving is not.
//
// **The `error` is `rmp`'s own text and carries no engine diagnostic.** That is
// the exception rule 7 names, and it exists because the engine's diagnostic is
// precisely what a reader cannot tell apart from an invalid statement. A page
// that showed the engine's text here would put the query bar's user in front of
// the decision the CLI's user was rescued from.
func TestServedGraphError_TheContentionLineIsRmpsOwnAndNotTheEngines(t *testing.T) {
	const engineDiagnostic = "mvcc: serialization conflict in node properties: id 41"

	err := servedGraphError(context.Background(), "/tmp/graph.sock", &graphclient.SendError{
		Kind: graphclient.FailureConflict,
		Code: "Neo.TransientError.Transaction.Outdated", Diagnostic: engineDiagnostic,
	})

	queryErr, ok := asGraphQueryError(err)
	if !ok {
		t.Fatalf("the served conflict is %T, want a query-bar failure answered 400", err)
	}
	if queryErr.Kind != graphErrExecution {
		t.Errorf("kind = %q, want %q: an exhausted retry is the fifth reason the execution kind "+
			"arises and it changes neither the status nor the kind set", queryErr.Kind, graphErrExecution)
	}
	if strings.Contains(queryErr.Reason, engineDiagnostic) {
		t.Errorf("the error carries the engine's diagnostic %q. Rule 11 requires `rmp`'s own line: "+
			"the engine's text is what a reader cannot tell apart from an invalid statement, which "+
			"is the whole reason this line exists. Got %q", engineDiagnostic, queryErr.Reason)
	}
	for _, fragment := range []string{
		"graph write conflict: another writer committed first on every attempt within the " +
			backoff.Total().String() + " retry budget",
		"nothing was written",
		"run it again, and spread concurrent writes across distinct nodes",
	} {
		if !strings.Contains(queryErr.Reason, fragment) {
			t.Errorf("the error does not say %q. The line must name the contention, state that "+
				"nothing was written, and name the remedy; got %q", fragment, queryErr.Reason)
		}
	}

	// The CLI prints the same words for the same condition, modulo this
	// endpoint's own "query failed to execute: " prefix and the socket path a
	// browser has no use for. Comparing the two would need internal/commands,
	// which this package cannot import, so what is asserted here is the half
	// that lives on this side: the sentence after the prefix.
	if !strings.HasPrefix(queryErr.Reason, "query failed to execute: ") {
		t.Errorf("the error does not carry the prefix every execution failure on this endpoint "+
			"carries; got %q", queryErr.Reason)
	}
}

// TestLoadGraphView_UnservedRoadmapStillTakesTheDirectPath is the control for
// every assertion above.
//
// Without it, the routing could be sending EVERY request at a socket and the
// served tests would still pass. The direct path is what every request took
// before a server existed and is what a request must still take when nothing is
// listening — including when a leftover socket file is sitting there, which is
// not an error and is not removed (SPEC/GRAPH.md § Server Resolution, rule 1).
func TestLoadGraphView_UnservedRoadmapStillTakesTheDirectPath(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "payments-platform")
	seedGraph(t, name, "CREATE (s:Spec {key:'payment-capture'})")

	socket, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path: %v", err)
	}

	// A socket file a killed server left behind: it exists, it is a socket, and
	// nothing is listening on it.
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("binding %s: %v", socket, err)
	}
	unix, ok := ln.(*net.UnixListener)
	if !ok {
		t.Fatalf("a unix listener is %T", ln)
	}
	unix.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatalf("closing the listener: %v", err)
	}

	view, err := loadGraphView(context.Background(), name, "", "")
	if err != nil {
		t.Fatalf("a request against a roadmap with a LEFTOVER socket failed with %v; the refusal "+
			"a connection to it receives is the whole of the evidence that nothing is listening, "+
			"and the request proceeds on the direct path", err)
	}
	if len(view.Nodes) != 1 {
		t.Fatalf("the response carries %d node(s), want the one the store holds", len(view.Nodes))
	}
	properties, ok := view.Nodes[0]["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T, want a map", view.Nodes[0]["properties"])
	}
	if properties["key"] != "payment-capture" {
		t.Errorf("properties.key = %v, want the value the STORE holds", properties["key"])
	}

	if _, statErr := os.Lstat(socket); statErr != nil {
		t.Errorf("the leftover socket was removed by the request (%v). Removing one is the next "+
			"server's business, and a caller that removed one would race a server that was "+
			"binding it", statErr)
	}
}
