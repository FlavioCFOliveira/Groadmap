package web

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
)

// The tests in this file are SPEC/WEB.md Acceptance Criteria 262 and 263, and
// the regression for rmp task #563: § Requests Abandoned by the Client, and the
// half of § Graph Query Time Budget, rule 2, that says a client that disconnects
// cancels its statement.
//
// Every case counts the captured log BY LEVEL, as Acceptance Criterion 165 does:
// a check that merely looked for the INFO record would pass beside a stray WARN
// or ERROR, which is exactly the false record the section exists to remove.

// serveCancelled drives one request through the production handler chain with
// a context that is already cancelled, which is what a request whose client
// disconnected before the handler answered looks like to the handler.
func serveCancelled(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, req)
	return rec
}

// assertAbandoned asserts the complete outcome of an abandoned request: status
// 499 with no body, and a log holding exactly one INFO record — the published
// msg, status 499, the given attributes, an err naming the failure, and no kind
// — and zero WARN and zero ERROR records.
func assertAbandoned(t *testing.T, rec *httptest.ResponseRecorder, buf *bytes.Buffer, fragments ...string) string {
	t.Helper()
	if rec.Code != statusClientClosedRequest {
		t.Fatalf("status = %d, want 499; body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("a 499 carries no body, got %q", rec.Body.String())
	}
	lines := logLines(buf)
	if levelCount(lines, "ERROR") != 0 || levelCount(lines, "WARN") != 0 {
		t.Errorf("an abandoned request produced WARN or ERROR records; it must produce neither:\n%s", buf.String())
	}
	record := oneRecord(t, buf)
	mustContainAll(t, record, append([]string{
		" level=INFO ",
		`msg="request abandoned by client"`,
		"method=GET",
		" status=499 ",
		" err=",
	}, fragments...)...)
	if strings.Contains(record, "kind=") {
		t.Errorf("an abandoned request is not a query-bar failure and carries no kind\nrecord: %s", record)
	}
	return record
}

// TestAbandonedRequest_APageIsAnsweredAtInfoWith499 is the first case of
// Acceptance Criterion 262: a roadmap page whose read the cancellation fails.
// Before the fix each of these was answered 500 and recorded as an ERROR — a
// read failure nothing prevented.
func TestAbandonedRequest_APageIsAnsweredAtInfoWith499(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "identity-service")

	for _, c := range []struct {
		target    string
		fragments []string
	}{
		{"/roadmaps/" + name, []string{"roadmap=" + name}},
		{"/roadmaps/" + name + "/tasks?status=DOING", []string{"roadmap=" + name}},
		{"/roadmaps/" + name + "/audit", []string{"roadmap=" + name, "page=1"}},
		{"/roadmaps/" + name + "/tasks/1", []string{"roadmap=" + name, "task=1"}},
		{"/roadmaps/" + name + "/sprints/1", []string{"roadmap=" + name, "sprint=1"}},
	} {
		t.Run(c.target, func(t *testing.T) {
			buf := captureLog(t)
			rec := serveCancelled(t, c.target)
			assertAbandoned(t, rec, buf, append(c.fragments, "context canceled")...)

			// The response carries the headers every response of the route
			// carries, and nothing a 200 alone would set.
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("X-Frame-Options = %q, want DENY", got)
			}
			if strings.Contains(c.target, "/tasks?") {
				if vary := rec.Header().Values("Vary"); len(vary) != 1 || vary[0] != "Cookie" {
					t.Errorf("Vary = %q, want Cookie: the tasks route carries it on every response", vary)
				}
			}
			if cookies := rec.Header().Values("Set-Cookie"); len(cookies) != 0 {
				t.Errorf("Set-Cookie = %q: only a 200 sets the filter-state cookie", cookies)
			}
		})
	}
}

// TestAbandonedRequest_GraphDataWithNoServerIsNot503 is the second case of
// Acceptance Criterion 262, the one the old classification got wrong twice
// over: a graph data request for a roadmap with NO graph server listening, whose
// context is cancelled, is answered neither 503 nor 400 with kind execution.
func TestAbandonedRequest_GraphDataWithNoServerIsNot503(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "identity-service")
	target := "/roadmaps/" + name + "/graph/data"

	// The control: with its client connected the same request IS answered 503,
	// so the roadmap really has no server and the 499 below is not a server
	// that happened to be there.
	control := httptest.NewRecorder()
	handler().ServeHTTP(control, httptest.NewRequest(http.MethodGet, target, nil))
	if control.Code != http.StatusServiceUnavailable {
		t.Fatalf("the control request = %d, want 503: the roadmap must have no graph server", control.Code)
	}

	buf := captureLog(t)
	rec := serveCancelled(t, target)
	assertAbandoned(t, rec, buf, "roadmap="+name, "context canceled")
}

// TestAbandonedRequest_ARunningStatementIsStoppedAndNotCommitted is the third
// case of Acceptance Criterion 262 and the regression for rmp task #563.
//
// # The defect
//
// graphclient used the request's context only for its deadline. A browser that
// disconnected while its statement ran left the Bolt connection open, so the
// graph server ran the statement to its end and COMMITTED it for nobody: measured
// before the fix, a write cancelled 0.5 s into a statement of about 3 s was
// committed, and the cancelled call itself returned success 3.2 s later. SPEC/WEB.md
// § Graph Query Time Budget, rule 2, requires the disconnect to cancel the
// statement.
//
// # Why the control makes the absence meaningful
//
// The same statement, writing a different label, is sent afterwards with its
// client connected, and it commits. It started later than the cancelled one and
// does the same work, so by the time it has committed the cancelled one would
// have committed too had it kept running. Without it, an absent node would prove
// only that the statement had not finished yet.
func TestAbandonedRequest_ARunningStatementIsStoppedAndNotCommitted(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "identity-service")
	// A three-way product over 300 nodes: a statement whose work outlasts the
	// cancellation below by a wide margin, and which no node limit bounds.
	seedGraph(t, name, append(graphSeedQueries(), `UNWIND range(1,297) AS i CREATE (:Bulk {i:i})`)...)
	serveGraphAtBudget(t, name, time.Hour)

	slowWrite := func(label string) string {
		return "/roadmaps/" + name + "/graph/data?q=" + url.QueryEscape(
			"MATCH (a),(b),(c) WITH count(*) AS n CREATE (w:"+label+" {n:n}) RETURN w")
	}

	buf := captureLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The client leaves while its statement is running in the server.
	time.AfterFunc(300*time.Millisecond, cancel)
	req := httptest.NewRequest(http.MethodGet, slowWrite("AbandonedWrite"), nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, req)

	// The record names a statement that was SENT and then cut by the caller's
	// cancellation, so the cancellation reached a running statement rather than
	// one that had not been sent yet.
	assertAbandoned(t, rec, buf, "roadmap="+name, "lost: the caller cancelled the statement", "context canceled")

	control := httptest.NewRecorder()
	handler().ServeHTTP(control, httptest.NewRequest(http.MethodGet, slowWrite("ControlWrite"), nil))
	if control.Code != http.StatusOK {
		t.Fatalf("the control statement = %d, want 200; body=%q", control.Code, control.Body.String())
	}
	if n := countThroughTheServer(t, name, "MATCH (w:ControlWrite) RETURN count(w)"); n != 1 {
		t.Fatalf("the control statement committed %d node(s), want 1", n)
	}
	if n := countThroughTheServer(t, name, "MATCH (w:AbandonedWrite) RETURN count(w)"); n != 0 {
		t.Fatalf("the statement whose client disconnected committed %d node(s), want 0: the "+
			"disconnect must cancel the statement, not only the wait for its answer "+
			"(SPEC/WEB.md § Graph Query Time Budget, rule 2)", n)
	}
}

// TestAbandonedRequest_TheBudgetKeepsItsClassification is the first half of
// Acceptance Criterion 263: with the client still connected, a statement cut by
// the query time budget is a 400 with kind execution and the budget line, one
// WARN record, and no INFO record.
func TestAbandonedRequest_TheBudgetKeepsItsClassification(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "identity-service")
	seedExpensiveGraph(t, name)
	serveGraphAtBudget(t, name, 150*time.Millisecond)

	buf := captureLog(t)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/roadmaps/"+name+"/graph/data?q="+url.QueryEscape(expensiveGraphQuery), nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	kind, reason := decodeQueryError(t, rec.Body.Bytes())
	if kind != graphErrExecution || !strings.Contains(reason, "query time budget") {
		t.Errorf("kind %q reason %q, want execution and the budget line", kind, reason)
	}
	lines := logLines(buf)
	if levelCount(lines, "WARN") != 1 || levelCount(lines, "INFO") != 0 || len(lines) != 1 {
		t.Errorf("want exactly one WARN record and no INFO record:\n%s", buf.String())
	}
}

// silentGraphServer binds socket, completes the handshake and the session setup,
// and then answers a RUN with nothing while keeping the connection open: a graph
// server that accepted a statement and does not answer, which is what the
// endpoint's backstop deadline exists for.
func silentGraphServer(t *testing.T, socket string) {
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
			go silentGraphSession(conn)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close() //nolint:errcheck // the scripted listener is done with
		<-done
	})
}

// silentGraphSession answers every setup request with SUCCESS and a RUN with
// silence, reading until the client closes the connection.
func silentGraphSession(conn net.Conn) {
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
		if _, isRun := request.(*proto.Run); isRun {
			for {
				if _, err := reader.ReadMessage(); err != nil {
					return
				}
			}
		}
		var buf bytes.Buffer
		enc := packstream.NewEncoder(&buf)
		if proto.EncodeResponse(enc, &proto.Success{Metadata: map[string]packstream.Value{}}) != nil ||
			enc.Flush() != nil || writer.WriteMessage(buf.Bytes()) != nil {
			return
		}
	}
}

// TestAbandonedRequest_TheBackstopKeepsItsClassification is the second half of
// Acceptance Criterion 263: a graph server that accepts the statement and
// answers nothing inside the endpoint's backstop deadline is a 400 with kind
// execution and the line naming the silent server, one WARN record, and no INFO
// record — the backstop is a cancellation the server makes, with the request's
// context still live, so it is not an abandoned request.
func TestAbandonedRequest_TheBackstopKeepsItsClassification(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "identity-service")
	// The backstop is the statement budget plus the retry policy's total; a
	// negligible statement budget brings it down to the policy's total alone.
	setGraphQueryBudget(t, time.Millisecond)
	socket, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path: %v", err)
	}
	silentGraphServer(t, socket)

	buf := captureLog(t)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/roadmaps/"+name+"/graph/data?q="+url.QueryEscape("MATCH (n) RETURN n"), nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	kind, reason := decodeQueryError(t, rec.Body.Bytes())
	if kind != graphErrExecution || !strings.Contains(reason, "the graph server did not answer") {
		t.Errorf("kind %q reason %q, want execution and the line naming the silent server", kind, reason)
	}
	lines := logLines(buf)
	if levelCount(lines, "WARN") != 1 || levelCount(lines, "INFO") != 0 || len(lines) != 1 {
		t.Errorf("want exactly one WARN record and no INFO record:\n%s", buf.String())
	}
}

// TestAnswerIfAbandoned_DecidesByTheContextAlone pins rule 1 of § Requests
// Abandoned by the Client at the one classification point: the same failure
// text is answered as abandoned when the request's context is done and left to
// its own classification when it is not.
func TestAnswerIfAbandoned_DecidesByTheContextAlone(t *testing.T) {
	failure := errors.New("connection to the graph server lost")

	buf := captureLog(t)
	live := httptest.NewRecorder()
	if answerIfAbandoned(live, httptest.NewRequest(http.MethodGet, "/", nil), failure) {
		t.Fatal("a request whose context is live was answered as abandoned")
	}
	if live.Body.Len() != 0 || buf.Len() != 0 {
		t.Errorf("a live request must be left untouched: body %q, log %q", live.Body.String(), buf.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := httptest.NewRecorder()
	if !answerIfAbandoned(done, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx), failure) {
		t.Fatal("a request whose context is done was not answered as abandoned")
	}
	assertAbandoned(t, done, buf, `err="connection to the graph server lost"`)
}
