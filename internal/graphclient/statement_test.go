// Package graphclient — the statement-sending half's tests.
//
// # Why a scripted server and not a real one
//
// Every outcome this file drives is one the real engine produces only under a
// condition a unit test cannot arrange reliably: a serialisation conflict needs
// two writers to collide at the right instant, a budget failure needs a statement
// that runs for five seconds, and a connection lost mid-statement needs a server
// that dies at exactly the wrong moment. Scripting the SERVER's half of the
// protocol makes each of them a deterministic single-attempt exchange, and the
// engine's own proto package is what encodes and decodes both sides, so the
// scripts are protocol-faithful rather than approximate.
//
// The one thing a scripted server cannot establish — that the retry actually
// saves two REAL overlapping writers — is established where it can be, against a
// real server: TestServer_TwoOverlappingWritersBothSucceed, in
// internal/graphserve.
//
// # Why the value mapping is tested separately from the exchange
//
// SPEC/DATA_FORMATS.md § Graph Client Result requires the bytes `graph client`
// writes to be the bytes `graph execute` writes for the same statement. That
// holds by construction here, because [Send] returns the engine's own value model
// and both callers keep the serialiser they already had — so what actually needs
// proving is the inverse mapping this file owns: that a protocol value comes back
// as the expr.Value the engine put on the wire. Those tests are pure and
// exhaustive over the structure tags, which an exchange-level test could never be.
package graphclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
)

// ---------------------------------------------------------------------------
// The scripted server
// ---------------------------------------------------------------------------

// exchange is the scripted server's answer to one client request: the messages
// to write back, and whether to hang up instead of writing them.
type exchange struct {
	responses []any
	// hangUp closes the connection without answering, which is what a server
	// that dies mid-statement looks like from the client's side.
	hangUp bool
	// silent leaves the request unanswered and the connection open, which is
	// what a server whose undo replay is running past the deadline looks like.
	silent bool
}

// scriptedServer is a Bolt server whose answers are supplied by the test.
type scriptedServer struct {
	socket string
	// runs counts RUN messages received, so a retry is observable rather than
	// inferred from the outcome.
	runs atomic.Int64
	stop func()
}

// startScriptedServer binds a socket and answers each request with what answer
// returns for it. answer is called once per request, with the request itself and
// the one-based count of RUN messages seen so far, so a script can behave
// differently on a retry.
func startScriptedServer(t *testing.T, answer func(request any, runCount int64) exchange) *scriptedServer {
	t.Helper()

	path := socketPathIn(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("binding %s: %v", path, err)
	}

	s := &scriptedServer{socket: path}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go s.session(conn, answer)
		}
	}()
	s.stop = func() {
		_ = ln.Close() //nolint:errcheck // the scripted listener is done with
		<-done
	}
	t.Cleanup(s.stop)
	return s
}

// session drives one connection: the handshake through the engine's own server
// half, then request/response until the client goes away or the script hangs up.
func (s *scriptedServer) session(conn net.Conn, answer func(request any, runCount int64) exchange) {
	defer conn.Close() //nolint:errcheck // the scripted server has nothing to report

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := proto.Negotiate(ctx, conn); err != nil {
		return
	}

	reader := proto.NewChunkedReader(conn)
	writer := proto.NewChunkedWriter(conn)
	for {
		msg, err := reader.ReadMessage()
		if err != nil {
			return
		}
		request, err := proto.DecodeRequest(packstream.NewDecoder(bytes.NewReader(msg)))
		if err != nil {
			return
		}
		var count int64
		if _, isRun := request.(*proto.Run); isRun {
			count = s.runs.Add(1)
		}

		ex := answer(request, count)
		if ex.hangUp {
			return
		}
		if ex.silent {
			// Hold the connection open and answer nothing. The client's own
			// backstop deadline is what must end this.
			_, _ = io.Copy(io.Discard, conn)
			return
		}
		for _, response := range ex.responses {
			var buf bytes.Buffer
			enc := packstream.NewEncoder(&buf)
			if err := proto.EncodeResponse(enc, response); err != nil {
				return
			}
			if err := enc.Flush(); err != nil {
				return
			}
			if err := writer.WriteMessage(buf.Bytes()); err != nil {
				return
			}
		}
	}
}

// ok is the SUCCESS every setup step of a session is answered with.
func ok() exchange {
	return exchange{responses: []any{&proto.Success{Metadata: map[string]packstream.Value{}}}}
}

// runSuccess is the SUCCESS a RUN carrying result columns is answered with.
func runSuccess(columns ...string) exchange {
	fields := make([]packstream.Value, len(columns))
	for i, c := range columns {
		fields[i] = c
	}
	return exchange{responses: []any{&proto.Success{Metadata: map[string]packstream.Value{
		"fields": fields,
		"qid":    int64(-1),
	}}}}
}

// defaultSession answers HELLO and LOGON, and delegates everything else.
func defaultSession(request any, onStatement func(request any, runCount int64) exchange, runCount int64) exchange {
	switch request.(type) {
	case *proto.Hello, *proto.Logon:
		return ok()
	default:
		return onStatement(request, runCount)
	}
}

// ---------------------------------------------------------------------------
// The exchange
// ---------------------------------------------------------------------------

// TestSend_ReturnsTheColumnsAndRowsTheServerStreamed is the ordinary read.
func TestSend_ReturnsTheColumnsAndRowsTheServerStreamed(t *testing.T) {
	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			switch request.(type) {
			case *proto.Run:
				return runSuccess("s.key", "c.path")
			case *proto.Pull:
				return exchange{responses: []any{
					&proto.Record{Data: []packstream.Value{"user-authentication", "internal/auth/jwt.go"}},
					&proto.Record{Data: []packstream.Value{"payment-capture", "internal/payments/stripe.go"}},
					&proto.Success{Metadata: map[string]packstream.Value{"has_more": false}},
				}}
			default:
				return ok()
			}
		}, runCount)
	})

	result, err := Send(context.Background(), server.socket,
		"MATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code) RETURN s.key, c.path")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got, want := result.Columns, []string{"s.key", "c.path"}; !reflect.DeepEqual(got, want) {
		t.Errorf("columns = %v, want %v", got, want)
	}
	if got := len(result.Rows); got != 2 {
		t.Fatalf("rows = %d, want 2", got)
	}
	if got, want := result.Rows[0][0], expr.StringValue("user-authentication"); got != want {
		t.Errorf("row 0 cell 0 = %v, want %v", got, want)
	}
	if got, want := result.Rows[1][1], expr.StringValue("internal/payments/stripe.go"); got != want {
		t.Errorf("row 1 cell 1 = %v, want %v", got, want)
	}
}

// TestSend_AStatementWithNoColumnsIsTheWriteShape pins the discriminator between
// the two published shapes, which is the COLUMNS and not the RETURN clause.
//
// A CREATE INDEX produces no columns and must reach the caller as the shape that
// renders {"ok": true}, while a SHOW INDEXES produces columns while carrying no
// RETURN and must not (SPEC/DATA_FORMATS.md § Graph Write Result).
func TestSend_AStatementWithNoColumnsIsTheWriteShape(t *testing.T) {
	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			switch request.(type) {
			case *proto.Run:
				// No "fields" key at all, which is what the engine sends for a
				// statement that produces no columns.
				return exchange{responses: []any{&proto.Success{Metadata: map[string]packstream.Value{
					"qid": int64(-1),
				}}}}
			case *proto.Pull:
				return exchange{responses: []any{
					&proto.Success{Metadata: map[string]packstream.Value{"has_more": false}},
				}}
			default:
				return ok()
			}
		}, runCount)
	})

	result, err := Send(context.Background(), server.socket, "CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(result.Columns) != 0 {
		t.Errorf("columns = %v, want none: a statement that produces no columns is the write shape",
			result.Columns)
	}
	if result.Rows == nil {
		t.Error("Rows is nil; it is never nil, so a statement that matched nothing renders as [] " +
			"rather than null")
	}
}

// TestSend_RetriesASerialisationConflictAndDoesNotSurfaceIt is the acceptance
// criterion's deterministic half.
//
// MVCC is the store's only concurrency control: writers do not exclude one
// another and a write-write collision is DETECTED rather than prevented, so the
// conflict is a normal outcome of concurrent writes and not a fault
// (SPEC/GRAPH.md § Concurrency Inside the Server, rules 3 and 4). A client that
// surfaced it at once would report a defect where the store reported ordinary
// concurrency.
//
// Retrying is safe because the losing transaction committed nothing (rule 5), so
// re-running its statement runs it against a graph that never saw it.
//
// What is asserted is both halves: that the second attempt HAPPENED — the RUN
// count, not merely the outcome — and that the conflict never reached the caller.
func TestSend_RetriesASerialisationConflictAndDoesNotSurfaceIt(t *testing.T) {
	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, count int64) exchange {
			switch request.(type) {
			case *proto.Run:
				if count == 1 {
					return exchange{responses: []any{&proto.Failure{
						Code:    conflictCode,
						Message: "transaction has seen state which has been invalidated by applied updates",
					}}}
				}
				return runSuccess("s.status")
			case *proto.Pull:
				return exchange{responses: []any{
					&proto.Record{Data: []packstream.Value{"implemented"}},
					&proto.Success{Metadata: map[string]packstream.Value{"has_more": false}},
				}}
			default:
				return ok()
			}
		}, runCount)
	})

	result, err := Send(context.Background(), server.socket,
		"MATCH (s:Spec {key:'user-authentication'}) SET s.status = 'implemented' RETURN s.status")
	if err != nil {
		t.Fatalf("a serialisation conflict reached the caller as %v. It is a normal outcome of "+
			"concurrent writes and MUST be retried rather than surfaced on its first occurrence", err)
	}
	if got := server.runs.Load(); got != 2 {
		t.Errorf("the server saw %d RUN message(s), want 2: the statement must be RE-SENT, not "+
			"merely reported as succeeding", got)
	}
	if len(result.Rows) != 1 {
		t.Errorf("rows = %d, want the retry's own result", len(result.Rows))
	}
}

// TestSend_AnExhaustedRetryPolicyReportsTheEnginesOwnDiagnostic is the other
// outcome rule 8 names.
//
// Once every attempt has collided, the conflict IS the outcome and concealing it
// would leave the caller with nothing to act on. The failure that reaches the
// caller therefore carries the engine's own code and message, classified as a
// statement failure rather than as a transport one.
func TestSend_AnExhaustedRetryPolicyReportsTheEnginesOwnDiagnostic(t *testing.T) {
	const diagnostic = "transaction has seen state which has been invalidated by applied updates"

	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			if _, isRun := request.(*proto.Run); isRun {
				return exchange{responses: []any{&proto.Failure{Code: conflictCode, Message: diagnostic}}}
			}
			return ok()
		}, runCount)
	})

	_, err := Send(context.Background(), server.socket, "MATCH (s:Spec) SET s.status = 'ready'")
	if err == nil {
		t.Fatal("a statement every attempt collided on succeeded")
	}

	var sendErr *SendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("error = %v (%T), want a *SendError", err, err)
	}
	if sendErr.Kind != FailureStatement {
		t.Errorf("kind = %v, want FailureStatement: once retrying has stopped being the answer, "+
			"the conflict is the outcome", sendErr.Kind)
	}
	if sendErr.Code != conflictCode {
		t.Errorf("code = %q, want the engine's own %q", sendErr.Code, conflictCode)
	}
	if sendErr.Diagnostic != diagnostic {
		t.Errorf("diagnostic = %q, want the engine's own %q", sendErr.Diagnostic, diagnostic)
	}

	// The policy is the project's single one, so the attempt count is its own.
	if got, want := server.runs.Load(), int64(backoff.Attempts); got != want {
		t.Errorf("the server saw %d RUN message(s), want %d — one initial attempt plus the "+
			"project's %d retries (internal/backoff)", got, want, backoff.Attempts)
	}
}

// TestSend_ClassifiesTheServersBudgetFailure pins the failure the server's own
// deadline produces, and pins that it is NOT retried.
//
// The engine cuts a statement at the statement budget and answers with a TYPED
// failure on an intact connection — measured on rmp task #367 at exactly 5.0
// seconds, answered rather than disconnected. Retrying it would re-run a
// statement whose only problem is that it takes too long.
func TestSend_ClassifiesTheServersBudgetFailure(t *testing.T) {
	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			if _, isRun := request.(*proto.Run); isRun {
				return exchange{responses: []any{&proto.Failure{
					Code:    timeoutCode,
					Message: "context deadline exceeded",
				}}}
			}
			return ok()
		}, runCount)
	})

	_, err := Send(context.Background(), server.socket, "MATCH (a),(b),(c) RETURN count(*)")

	var sendErr *SendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("error = %v (%T), want a *SendError", err, err)
	}
	if sendErr.Kind != FailureBudget {
		t.Errorf("kind = %v, want FailureBudget: the budget line names a valid statement against a "+
			"healthy store and has a different remedy from a parse error", sendErr.Kind)
	}
	if got := server.runs.Load(); got != 1 {
		t.Errorf("the server saw %d RUN message(s), want 1: a statement the budget cut must not be "+
			"retried", got)
	}
}

// TestSend_ClassifiesAStatementFailure pins the ordinary engine refusal, which
// reaches the caller with the engine's own diagnostic after the published prefix.
func TestSend_ClassifiesAStatementFailure(t *testing.T) {
	const diagnostic = `cypher: parse: unexpected "RETURN" at 1:9`

	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			if _, isRun := request.(*proto.Run); isRun {
				return exchange{responses: []any{&proto.Failure{
					Code:    "Neo.ClientError.Statement.SyntaxError",
					Message: diagnostic,
				}}}
			}
			return ok()
		}, runCount)
	})

	_, err := Send(context.Background(), server.socket, "MATCH (n RETURN n")

	var sendErr *SendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("error = %v (%T), want a *SendError", err, err)
	}
	if sendErr.Kind != FailureStatement {
		t.Errorf("kind = %v, want FailureStatement", sendErr.Kind)
	}
	if sendErr.Diagnostic != diagnostic {
		t.Errorf("diagnostic = %q, want the engine's own %q", sendErr.Diagnostic, diagnostic)
	}
	if got := server.runs.Load(); got != 1 {
		t.Errorf("the server saw %d RUN message(s), want 1: a statement the engine refuses fails "+
			"the same way every time", got)
	}
}

// TestSend_ClassifiesAConnectionLostAfterTheStatementWasSent is rule 4.
//
// A commit is durable before it is acknowledged, so a connection that dies
// between the two leaves the caller unable to tell whether the statement
// happened. The failure is therefore its own class — the outcome is UNKNOWN — and
// it must not be retried and must not send the caller to the store.
func TestSend_ClassifiesAConnectionLostAfterTheStatementWasSent(t *testing.T) {
	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			if _, isRun := request.(*proto.Run); isRun {
				return exchange{hangUp: true}
			}
			return ok()
		}, runCount)
	})

	_, err := Send(context.Background(), server.socket, "CREATE (s:Spec {key:'rate-limiting'})")

	var sendErr *SendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("error = %v (%T), want a *SendError", err, err)
	}
	if sendErr.Kind != FailureLost {
		t.Errorf("kind = %v, want FailureLost: the statement had been sent, so its outcome is "+
			"unknown rather than known not to have happened", sendErr.Kind)
	}
	if got := server.runs.Load(); got != 1 {
		t.Errorf("the server saw %d RUN message(s), want 1: a statement whose outcome is unknown "+
			"must never be re-sent, because it may already have committed", got)
	}
}

// TestSend_ClassifiesAServerThatCannotBeReached is the failure before the
// statement crosses: nothing ran, and the caller still must not fall back to the
// store, because a socket that answers may belong to a server holding the lock.
func TestSend_ClassifiesAServerThatCannotBeReached(t *testing.T) {
	t.Run("nothing listening", func(t *testing.T) {
		_, err := Send(context.Background(), socketPathIn(t), "MATCH (n) RETURN n")

		var sendErr *SendError
		if !errors.As(err, &sendErr) {
			t.Fatalf("error = %v (%T), want a *SendError", err, err)
		}
		if sendErr.Kind != FailureUnreachable {
			t.Errorf("kind = %v, want FailureUnreachable", sendErr.Kind)
		}
		if sendErr.Cause == nil {
			t.Error("an unreachable server must carry the transport observation behind it")
		}
	})

	t.Run("a session the server refuses", func(t *testing.T) {
		server := startScriptedServer(t, func(request any, _ int64) exchange {
			if _, isHello := request.(*proto.Hello); isHello {
				return exchange{responses: []any{&proto.Failure{
					Code:    "Neo.ClientError.Security.Unauthorized",
					Message: "authentication failed",
				}}}
			}
			return ok()
		})

		_, err := Send(context.Background(), server.socket, "MATCH (n) RETURN n")

		var sendErr *SendError
		if !errors.As(err, &sendErr) {
			t.Fatalf("error = %v (%T), want a *SendError", err, err)
		}
		if sendErr.Kind != FailureUnreachable {
			t.Errorf("kind = %v, want FailureUnreachable: a session that could not be established "+
				"is a server that could not be reached, and no statement ran", sendErr.Kind)
		}
		if got := server.runs.Load(); got != 0 {
			t.Errorf("the server saw %d RUN message(s); the statement must not be sent on a session "+
				"that was refused", got)
		}
	})
}

// TestSend_ClassifiesAServerThatDoesNotAnswer is rule 7's backstop.
//
// The connection is intact and the server is alive; it is simply not answering,
// which is what a statement the budget cut MID-WRITE looks like from outside,
// because the engine's undo replay runs past the deadline by a factor nothing
// bounds. The outcome is unknown for the same reason a lost connection's is, and
// the caller does not fall back to the store.
//
// The budget is shortened through the same declaration production reads, so what
// the test asserts is the relationship — the caller waits the WAIT budget — rather
// than a figure of its own.
func TestSend_ClassifiesAServerThatDoesNotAnswer(t *testing.T) {
	previous := graphlock.StatementBudget
	t.Cleanup(func() { graphlock.StatementBudget = previous })
	graphlock.StatementBudget = 0

	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			if _, isRun := request.(*proto.Run); isRun {
				return exchange{silent: true}
			}
			return ok()
		}, runCount)
	})

	started := time.Now()
	_, err := Send(context.Background(), server.socket, "MATCH (n) DETACH DELETE n")
	elapsed := time.Since(started)

	var sendErr *SendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("error = %v (%T), want a *SendError", err, err)
	}
	if sendErr.Kind != FailureUnanswered {
		t.Errorf("kind = %v, want FailureUnanswered: the connection is intact and the server is "+
			"alive, so this is not a lost connection", sendErr.Kind)
	}
	if budget := graphlock.WaitBudget(); elapsed < budget {
		t.Errorf("Send gave up after %v, before its %v backstop. The backstop is deliberately LATER "+
			"than the server's statement budget so that a statement which committed just before the "+
			"budget expired is never reported as one that wrote nothing", elapsed, budget)
	}
}

// TestSend_TheBackstopIsTheWaitBudgetAndNotTheStatementBudget pins rule 7's
// central claim, which is a relationship between two declared quantities rather
// than a behaviour: the caller's deadline is the statement budget PLUS the
// backoff total, and never the statement budget itself.
//
// The reason is a false report the equal values would produce. A statement that
// commits a few milliseconds before the budget expires has its acknowledgement in
// flight when a caller-side deadline of exactly the budget fires, and the caller
// would then print the budget line — which states that nothing was written — over
// a write that had succeeded.
func TestSend_TheBackstopIsTheWaitBudgetAndNotTheStatementBudget(t *testing.T) {
	if graphlock.WaitBudget() <= graphlock.StatementBudget {
		t.Errorf("the caller's backstop (%v) is not later than the server's statement budget (%v). "+
			"Equal values make the caller's deadline race the server's acknowledgement, and the "+
			"caller would report that nothing was written over a write that had succeeded",
			graphlock.WaitBudget(), graphlock.StatementBudget)
	}
	if got, want := graphlock.WaitBudget(), graphlock.StatementBudget+backoff.Total(); got != want {
		t.Errorf("the backstop is %v, want the statement budget plus the backoff total, %v: both "+
			"bounds are derived from one declaration so they cannot disagree about the value",
			got, want)
	}
}

// TestSend_CarriesTheServersNotifications pins that an advisory the engine
// attached reaches the caller, which is what lets a served invocation write the
// same stderr diagnostic a direct one writes
// (SPEC/GRAPH.md § Query Notifications as Diagnostics).
func TestSend_CarriesTheServersNotifications(t *testing.T) {
	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			switch request.(type) {
			case *proto.Run:
				return runSuccess("a.key")
			case *proto.Pull:
				return exchange{responses: []any{&proto.Success{Metadata: map[string]packstream.Value{
					"has_more": false,
					"notifications": []packstream.Value{
						map[string]packstream.Value{
							"code":        "Neo.ClientNotification.Statement.CartesianProductWarning",
							"description": "If a part of a query contains multiple disconnected patterns",
							"severity":    "INFORMATION",
							"title":       "This query builds a cartesian product",
							"category":    "PERFORMANCE",
						},
					},
				}}}}
			default:
				return ok()
			}
		}, runCount)
	})

	result, err := Send(context.Background(), server.socket, "MATCH (a:Spec), (b:Code) RETURN a.key")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(result.Notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(result.Notifications))
	}
	n := result.Notifications[0]
	if n.Severity != "INFORMATION" {
		t.Errorf("severity = %q, want INFORMATION", n.Severity)
	}
	if n.Code != "Neo.ClientNotification.Statement.CartesianProductWarning" {
		t.Errorf("code = %q", n.Code)
	}
	if n.Description == "" {
		t.Error("the description is empty; it is the body of the line the CLI prints")
	}
}

// TestSend_AValueItCannotMapFailsTheStatement is
// SPEC/DATA_FORMATS.md § Graph Client Result, rule 3.
//
// The client does not substitute a placeholder for a value it could not map, so
// that a caller never reads a result that is quietly not the one the graph holds.
func TestSend_AValueItCannotMapFailsTheStatement(t *testing.T) {
	server := startScriptedServer(t, func(request any, runCount int64) exchange {
		return defaultSession(request, func(request any, _ int64) exchange {
			switch request.(type) {
			case *proto.Run:
				return runSuccess("n")
			case *proto.Pull:
				return exchange{responses: []any{
					// A structure tag no version of this protocol defines.
					&proto.Record{Data: []packstream.Value{packstream.Struct{Tag: 0x7A}}},
					&proto.Success{Metadata: map[string]packstream.Value{"has_more": false}},
				}}
			default:
				return ok()
			}
		}, runCount)
	})

	result, err := Send(context.Background(), server.socket, "MATCH (n) RETURN n")
	if err == nil {
		t.Fatalf("a value the mapping cannot represent produced a result: %+v", result)
	}

	var sendErr *SendError
	if !errors.As(err, &sendErr) {
		t.Fatalf("error = %v (%T), want a *SendError", err, err)
	}
	if sendErr.Kind != FailureMapping {
		t.Errorf("kind = %v, want FailureMapping", sendErr.Kind)
	}
	if sendErr.Diagnostic == "" {
		t.Error("a mapping failure must say what it could not map")
	}
}

// ---------------------------------------------------------------------------
// The value mapping
// ---------------------------------------------------------------------------

// TestToExprValue_Scalars pins the scalar half of
// SPEC/DATA_FORMATS.md § Property-Type Mapping, in the direction this package
// owns: the protocol's encoding back onto the engine's value model.
func TestToExprValue_Scalars(t *testing.T) {
	cases := []struct {
		name string
		in   packstream.Value
		want expr.Value
	}{
		{"null", nil, expr.Null},
		{"boolean", true, expr.BoolValue(true)},
		{"integer", int64(-17), expr.IntegerValue(-17)},
		{"float", 0.5, expr.FloatValue(0.5)},
		{"string", "user-authentication", expr.StringValue("user-authentication")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := toExprValue(c.in)
			if err != nil {
				t.Fatalf("toExprValue(%v): %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("toExprValue(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestToExprValue_ANonFiniteFloatSurvivesTheMapping pins that the mapping does
// NOT decide what to do with a non-finite float.
//
// SPEC/DATA_FORMATS.md § Property-Type Mapping makes NaN and the infinities JSON
// null, and the serialiser downstream is where that happens — the same serialiser
// the direct path uses. Deciding it here as well would be a second answer to one
// question, and the two could disagree.
func TestToExprValue_ANonFiniteFloatSurvivesTheMapping(t *testing.T) {
	got, err := toExprValue(math.NaN())
	if err != nil {
		t.Fatalf("toExprValue(NaN): %v", err)
	}
	f, ok := got.(expr.FloatValue)
	if !ok {
		t.Fatalf("toExprValue(NaN) = %T, want expr.FloatValue", got)
	}
	if !math.IsNaN(float64(f)) {
		t.Errorf("toExprValue(NaN) = %v; the non-finite value must reach the serialiser, which is "+
			"where the published mapping turns it into JSON null on BOTH paths", f)
	}
}

// TestToExprValue_ByteStringBecomesItsBase64Rendering is the one place the
// mapping cannot be an identity, and the comment on it is the justification.
//
// PackStream carries a byte string; the published shape carries a
// base64-standard-encoded JSON string for one; and the engine's value model has
// no byte kind to hold it in between. Encoding here is what makes the serialiser
// downstream produce exactly the published representation without learning a new
// kind.
func TestToExprValue_ByteStringBecomesItsBase64Rendering(t *testing.T) {
	raw := []byte{0x00, 0x01, 0xFE, 0xFF}

	got, err := toExprValue(raw)
	if err != nil {
		t.Fatalf("toExprValue: %v", err)
	}
	want := expr.StringValue(base64.StdEncoding.EncodeToString(raw))
	if got != want {
		t.Errorf("toExprValue(bytes) = %v, want %v", got, want)
	}
}

// TestToExprValue_ContainersRecurse pins that a list and a map are mapped
// element-wise, so an entity or a temporal nested inside one is mapped exactly as
// one returned in its own column is.
func TestToExprValue_ContainersRecurse(t *testing.T) {
	in := map[string]packstream.Value{
		"names": []packstream.Value{"alpha", "beta"},
		"nested": map[string]packstream.Value{
			"count": int64(2),
		},
	}

	got, err := toExprValue(in)
	if err != nil {
		t.Fatalf("toExprValue: %v", err)
	}
	m, ok := got.(expr.MapValue)
	if !ok {
		t.Fatalf("toExprValue(map) = %T, want expr.MapValue", got)
	}
	list, ok := m["names"].(expr.ListValue)
	if !ok {
		t.Fatalf("names = %T, want expr.ListValue", m["names"])
	}
	if len(list) != 2 || list[0] != expr.StringValue("alpha") {
		t.Errorf("names = %v, want the two strings mapped element-wise", list)
	}
	nested, ok := m["nested"].(expr.MapValue)
	if !ok {
		t.Fatalf("nested = %T, want expr.MapValue", m["nested"])
	}
	if nested["count"] != expr.IntegerValue(2) {
		t.Errorf("nested.count = %v, want 2", nested["count"])
	}
}

// TestToExprValue_AnEmptyPropertyBagIsNotNil pins that a node with no properties
// renders as {} rather than null, and a node with no labels as [] rather than
// null (SPEC/DATA_FORMATS.md § Graph element mapping, rule 2).
func TestToExprValue_AnEmptyPropertyBagIsNotNil(t *testing.T) {
	node := packstream.Struct{Tag: tagNode, Fields: []packstream.Value{
		int64(17),
		[]packstream.Value{},
		map[string]packstream.Value{},
		"17",
	}}

	got, err := toExprValue(node)
	if err != nil {
		t.Fatalf("toExprValue: %v", err)
	}
	nv, ok := got.(expr.NodeValue)
	if !ok {
		t.Fatalf("toExprValue(node) = %T, want expr.NodeValue", got)
	}
	if nv.Labels == nil {
		t.Error("Labels is nil; a node that carries no labels publishes [] and never null")
	}
	if nv.Properties == nil {
		t.Error("Properties is nil; an empty property bag publishes {} and never null")
	}
}

// TestToExprValue_Node pins the 'N' structure, including that the element id
// Bolt 5 appends is read and DROPPED.
//
// Publishing it would make a result depend on which path carried it, which is the
// one thing the published shape may not do
// (SPEC/DATA_FORMATS.md § Graph Client Result, rules 1 and 2: a key the protocol's
// encoding adds is not added to the JSON).
func TestToExprValue_Node(t *testing.T) {
	node := packstream.Struct{Tag: tagNode, Fields: []packstream.Value{
		int64(140),
		[]packstream.Value{"Spec", "Reviewed"},
		map[string]packstream.Value{"key": "user-authentication", "ord": int64(3)},
		"140",
	}}

	got, err := toExprValue(node)
	if err != nil {
		t.Fatalf("toExprValue: %v", err)
	}
	nv, ok := got.(expr.NodeValue)
	if !ok {
		t.Fatalf("toExprValue(node) = %T, want expr.NodeValue", got)
	}
	if nv.ID != 140 {
		t.Errorf("id = %d, want 140", nv.ID)
	}
	if !reflect.DeepEqual(nv.Labels, []string{"Spec", "Reviewed"}) {
		t.Errorf("labels = %v, want the order the engine reported", nv.Labels)
	}
	if nv.Properties["key"] != expr.StringValue("user-authentication") {
		t.Errorf("properties.key = %v", nv.Properties["key"])
	}
	if nv.Properties["ord"] != expr.IntegerValue(3) {
		t.Errorf("properties.ord = %v", nv.Properties["ord"])
	}
	// The element id has no home in the value model, which is what makes rule 2
	// hold by construction rather than by an omission somebody has to remember.
	if _, leaked := nv.Properties["element_id"]; leaked {
		t.Error("the protocol's element id was carried into the properties; a key the protocol's " +
			"encoding adds is not added to the published shape")
	}
}

// TestToExprValue_Relationship pins the 'R' structure, which carries its
// endpoints.
func TestToExprValue_Relationship(t *testing.T) {
	rel := packstream.Struct{Tag: tagRelationship, Fields: []packstream.Value{
		int64(1), int64(140), int64(165), "IMPLEMENTED_BY",
		map[string]packstream.Value{"since": "2026-01-01"},
		"1", "140", "165",
	}}

	got, err := toExprValue(rel)
	if err != nil {
		t.Fatalf("toExprValue: %v", err)
	}
	rv, ok := got.(expr.RelationshipValue)
	if !ok {
		t.Fatalf("toExprValue(relationship) = %T, want expr.RelationshipValue", got)
	}
	if rv.ID != 1 || rv.StartID != 140 || rv.EndID != 165 {
		t.Errorf("ids = (%d, %d, %d), want (1, 140, 165)", rv.ID, rv.StartID, rv.EndID)
	}
	if rv.Type != "IMPLEMENTED_BY" {
		t.Errorf("type = %q", rv.Type)
	}
	if rv.Properties["since"] != expr.StringValue("2026-01-01") {
		t.Errorf("properties.since = %v", rv.Properties["since"])
	}
}

// TestToExprValue_PathReplaysItsIndices is the subtle one, and it is the reason
// the path mapping does not simply read the two lists in order.
//
// A path carries its relationships in the UNBOUND form, which omits the
// endpoints, and supplies them through pairs of indices instead: a one-based,
// SIGNED relationship index whose sign says whether the hop traverses the
// relationship in its natural direction, and a zero-based node index naming the
// hop's end node. Replaying that is the only way to recover the endpoints the
// unbound form dropped — and a reverse-traversed hop is the case that proves the
// replay happened, because reading the lists in order would give it the wrong
// endpoints.
func TestToExprValue_PathReplaysItsIndices(t *testing.T) {
	node := func(id int64, label string) packstream.Value {
		return packstream.Struct{Tag: tagNode, Fields: []packstream.Value{
			id, []packstream.Value{label}, map[string]packstream.Value{}, "x",
		}}
	}
	unbound := func(id int64, relType string) packstream.Value {
		return packstream.Struct{Tag: tagUnboundRelationship, Fields: []packstream.Value{
			id, relType, map[string]packstream.Value{}, "x",
		}}
	}

	t.Run("a forward hop", func(t *testing.T) {
		path := packstream.Struct{Tag: tagPath, Fields: []packstream.Value{
			[]packstream.Value{node(140, "Spec"), node(165, "Code")},
			[]packstream.Value{unbound(1, "IMPLEMENTED_BY")},
			[]packstream.Value{int64(1), int64(1)},
		}}

		got, err := toExprValue(path)
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		pv, ok := got.(expr.PathValue)
		if !ok {
			t.Fatalf("toExprValue(path) = %T, want expr.PathValue", got)
		}
		if len(pv.Nodes) != 2 || len(pv.Relationships) != 1 {
			t.Fatalf("path = %d nodes and %d relationships, want 2 and 1",
				len(pv.Nodes), len(pv.Relationships))
		}
		if pv.Relationships[0].StartID != 140 || pv.Relationships[0].EndID != 165 {
			t.Errorf("hop endpoints = (%d, %d), want (140, 165)",
				pv.Relationships[0].StartID, pv.Relationships[0].EndID)
		}
	})

	t.Run("a reverse hop", func(t *testing.T) {
		// The same two nodes, traversed the other way: the relationship index is
		// NEGATIVE, so the relationship starts at the hop's END node.
		path := packstream.Struct{Tag: tagPath, Fields: []packstream.Value{
			[]packstream.Value{node(165, "Code"), node(140, "Spec")},
			[]packstream.Value{unbound(1, "IMPLEMENTED_BY")},
			[]packstream.Value{int64(-1), int64(1)},
		}}

		got, err := toExprValue(path)
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		pv, ok := got.(expr.PathValue)
		if !ok {
			t.Fatalf("toExprValue(path) = %T, want expr.PathValue", got)
		}
		if pv.Relationships[0].StartID != 140 || pv.Relationships[0].EndID != 165 {
			t.Errorf("hop endpoints = (%d, %d), want (140, 165): a NEGATIVE relationship index "+
				"means the hop is traversed against the relationship's own direction, so the "+
				"relationship still starts where it started",
				pv.Relationships[0].StartID, pv.Relationships[0].EndID)
		}
	})

	t.Run("a zero-length path", func(t *testing.T) {
		// openCypher produces one for a single disconnected node: one node, no
		// relationship, and an empty index list.
		path := packstream.Struct{Tag: tagPath, Fields: []packstream.Value{
			[]packstream.Value{node(140, "Spec")},
			[]packstream.Value{},
			[]packstream.Value{},
		}}

		got, err := toExprValue(path)
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		pv, ok := got.(expr.PathValue)
		if !ok {
			t.Fatalf("toExprValue(path) = %T, want expr.PathValue", got)
		}
		if len(pv.Nodes) != 1 || len(pv.Relationships) != 0 {
			t.Errorf("path = %d nodes and %d relationships, want 1 and 0",
				len(pv.Nodes), len(pv.Relationships))
		}
	})

	t.Run("a malformed index list is a mapping failure", func(t *testing.T) {
		path := packstream.Struct{Tag: tagPath, Fields: []packstream.Value{
			[]packstream.Value{node(140, "Spec")},
			[]packstream.Value{unbound(1, "IMPLEMENTED_BY")},
			[]packstream.Value{int64(1)}, // an odd number of entries: not a whole hop
		}}

		if _, err := toExprValue(path); err == nil {
			t.Error("a path whose index list is not a whole number of hops was mapped; a value the " +
				"mapping cannot represent must fail rather than produce a different result")
		}
	})
}

// TestToExprValue_Temporals pins each temporal structure against the layout
// GoGraph's own encoder writes, so the two halves of one product describe one
// protocol.
func TestToExprValue_Temporals(t *testing.T) {
	t.Run("date", func(t *testing.T) {
		// 2026-09-02 is 20698 days after the epoch.
		day := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC).Unix() / secondsPerDay
		got, err := toExprValue(packstream.Struct{Tag: tagDate, Fields: []packstream.Value{day}})
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		dv, ok := got.(expr.DateValue)
		if !ok {
			t.Fatalf("toExprValue(date) = %T, want expr.DateValue", got)
		}
		if dv.Year != 2026 || dv.Month != 9 || dv.Day != 2 {
			t.Errorf("date = %04d-%02d-%02d, want 2026-09-02", dv.Year, dv.Month, dv.Day)
		}
	})

	t.Run("local time", func(t *testing.T) {
		const nanos = int64(13*3600+45*60) * 1e9
		got, err := toExprValue(packstream.Struct{Tag: tagLocalTime, Fields: []packstream.Value{nanos}})
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		if got != (expr.LocalTimeValue{Nanos: nanos}) {
			t.Errorf("local time = %v, want %v", got, expr.LocalTimeValue{Nanos: nanos})
		}
	})

	t.Run("time with a zone offset", func(t *testing.T) {
		const nanos = int64(13*3600+45*60) * 1e9
		got, err := toExprValue(packstream.Struct{Tag: tagTime,
			Fields: []packstream.Value{nanos, int64(3600)}})
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		want := expr.TimeValue{Nanos: nanos, OffsetSec: 3600}
		if got != want {
			t.Errorf("time = %v, want %v", got, want)
		}
	})

	t.Run("local date-time", func(t *testing.T) {
		instant := time.Date(2026, 9, 2, 13, 45, 30, 500_000_000, time.UTC)
		got, err := toExprValue(packstream.Struct{Tag: tagLocalDateTime,
			Fields: []packstream.Value{instant.Unix(), int64(instant.Nanosecond())}})
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		ldt, ok := got.(expr.LocalDateTimeValue)
		if !ok {
			t.Fatalf("toExprValue(local date-time) = %T, want expr.LocalDateTimeValue", got)
		}
		if !ldt.T.Equal(instant) {
			t.Errorf("local date-time = %v, want %v", ldt.T, instant)
		}
	})

	t.Run("date-time with an offset", func(t *testing.T) {
		instant := time.Date(2026, 9, 2, 12, 45, 30, 0, time.UTC)
		got, err := toExprValue(packstream.Struct{Tag: tagDateTimeOffset,
			Fields: []packstream.Value{instant.Unix(), int64(0), int64(3600)}})
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		dt, ok := got.(expr.DateTimeValue)
		if !ok {
			t.Fatalf("toExprValue(date-time) = %T, want expr.DateTimeValue", got)
		}
		if !dt.T.Equal(instant) {
			t.Errorf("date-time = %v, want the same INSTANT as %v", dt.T, instant)
		}
		if _, offset := dt.T.Zone(); offset != 3600 {
			t.Errorf("zone offset = %d, want 3600", offset)
		}
	})

	t.Run("the legacy Bolt 4.4 date-time carries a wall clock, not an instant", func(t *testing.T) {
		// The legacy form's seconds are the wall clock expressed as if UTC, so
		// the instant is that value MINUS the offset. Reading it as an instant
		// would put the value an hour out.
		wall := time.Date(2026, 9, 2, 13, 45, 30, 0, time.UTC)
		got, err := toExprValue(packstream.Struct{Tag: tagLegacyDateTimeOff,
			Fields: []packstream.Value{wall.Unix(), int64(0), int64(3600)}})
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		dt, ok := got.(expr.DateTimeValue)
		if !ok {
			t.Fatalf("toExprValue(legacy date-time) = %T, want expr.DateTimeValue", got)
		}
		want := wall.Add(-time.Hour)
		if !dt.T.Equal(want) {
			t.Errorf("legacy date-time = %v (UTC %v), want the instant %v", dt.T, dt.T.UTC(), want)
		}
	})

	t.Run("date-time with a named zone", func(t *testing.T) {
		instant := time.Date(2026, 9, 2, 12, 45, 30, 0, time.UTC)
		got, err := toExprValue(packstream.Struct{Tag: tagDateTimeZoneID,
			Fields: []packstream.Value{instant.Unix(), int64(0), "Europe/Lisbon"}})
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		dt, ok := got.(expr.DateTimeValue)
		if !ok {
			t.Fatalf("toExprValue(date-time) = %T, want expr.DateTimeValue", got)
		}
		if !dt.T.Equal(instant) {
			t.Errorf("date-time = %v, want the same INSTANT as %v", dt.T, instant)
		}
	})

	t.Run("a zone the system cannot resolve falls back to UTC", func(t *testing.T) {
		// The instant is already exact — it is carried by the seconds and the
		// nanoseconds — and every published rendering of a date-time is in UTC,
		// so the zone affects nothing a caller reads. Failing here would refuse a
		// result that is correct because a time-zone database is missing.
		instant := time.Date(2026, 9, 2, 12, 45, 30, 0, time.UTC)
		got, err := toExprValue(packstream.Struct{Tag: tagDateTimeZoneID,
			Fields: []packstream.Value{instant.Unix(), int64(0), "Nowhere/Imaginary"}})
		if err != nil {
			t.Fatalf("an unresolvable zone failed the statement: %v", err)
		}
		dt, ok := got.(expr.DateTimeValue)
		if !ok {
			t.Fatalf("toExprValue(date-time) = %T, want expr.DateTimeValue", got)
		}
		if !dt.T.Equal(instant) {
			t.Errorf("date-time = %v, want the same INSTANT as %v", dt.T, instant)
		}
	})

	t.Run("duration", func(t *testing.T) {
		got, err := toExprValue(packstream.Struct{Tag: tagDuration,
			Fields: []packstream.Value{int64(14), int64(3), int64(90), int64(500_000_000)}})
		if err != nil {
			t.Fatalf("toExprValue: %v", err)
		}
		want := expr.DurationValue{Months: 14, Days: 3, Seconds: 90, Nanos: 500_000_000}
		if got != want {
			t.Errorf("duration = %v, want %v", got, want)
		}
	})
}

// TestToExprValue_RefusesWhatItCannotRepresent pins rule 3 at the level of the
// mapping itself: an unknown tag, a field of the wrong type, and a structure with
// too few fields are each a failure rather than a substitute.
func TestToExprValue_RefusesWhatItCannotRepresent(t *testing.T) {
	cases := []struct {
		name string
		in   packstream.Value
	}{
		{"an unknown structure tag", packstream.Struct{Tag: 0x7A, Fields: []packstream.Value{int64(1)}}},
		{"a node with too few fields", packstream.Struct{Tag: tagNode, Fields: []packstream.Value{int64(1)}}},
		{"a node id that is not an integer", packstream.Struct{Tag: tagNode, Fields: []packstream.Value{
			"140", []packstream.Value{}, map[string]packstream.Value{}, "140",
		}}},
		{"a label that is not a string", packstream.Struct{Tag: tagNode, Fields: []packstream.Value{
			int64(140), []packstream.Value{int64(1)}, map[string]packstream.Value{}, "140",
		}}},
		{"a value of a Go type the protocol never carries", struct{ X int }{X: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, err := toExprValue(c.in); err == nil {
				t.Errorf("toExprValue(%v) = %v with no error; a value the mapping cannot represent "+
					"must fail the statement rather than become a different value", c.in, got)
			}
		})
	}
}

// TestIsRetriable_OnlyASerialisationConflict pins what the retry policy re-sends,
// and — more importantly — what it does not.
//
// A statement the engine refused fails the same way every time. A connection lost
// after the statement was sent must NOT be re-sent, because the statement may
// already have committed (SPEC/GRAPH.md § Server Resolution, rule 4). An
// unreachable server is a resolution failure the caller reports rather than works
// around.
func TestIsRetriable_OnlyASerialisationConflict(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"a serialisation conflict", &SendError{Kind: FailureStatement, Code: conflictCode, retriable: true}, true},
		{"a statement failure", &SendError{Kind: FailureStatement, Code: "Neo.ClientError.Statement.SyntaxError"}, false},
		{"a budget failure", &SendError{Kind: FailureBudget, Code: timeoutCode}, false},
		{"a lost connection", &SendError{Kind: FailureLost}, false},
		{"an unanswered server", &SendError{Kind: FailureUnanswered}, false},
		{"an unreachable server", &SendError{Kind: FailureUnreachable}, false},
		{"a mapping failure", &SendError{Kind: FailureMapping}, false},
		{"anything that is not a SendError", errors.New("something else"), false}, //nolint:err113 // a test fixture standing in for an error from outside this package
	}
	for _, c := range cases {
		if got := isRetriable(c.err); got != c.want {
			t.Errorf("isRetriable(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
