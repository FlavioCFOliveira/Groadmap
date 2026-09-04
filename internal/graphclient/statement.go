// Package graphclient — the statement-sending half: SPEC/GRAPH.md § The Bolt
// Client.
//
// [Send] is the ONE realisation of "run this statement on that server" in the
// product. `rmp graph client` is a thin command-line wrapper over it,
// `rmp graph execute` calls it whenever resolution reports a server, and so does
// the web graph data endpoint. A second implementation would be a second set of
// answers to every question that section settles — which retry, which deadline,
// which mapping — so there is one.
//
// # What crosses the boundary, and in which direction
//
// A statement goes out as text and comes back as [Result]: column names and rows
// of expr.Value, the engine's OWN value model, not JSON.
//
// That choice is what makes SPEC/DATA_FORMATS.md § Graph Client Result's central
// requirement — that the bytes `rmp graph client` writes are the bytes
// `rmp graph execute` writes for the same statement — hold by construction rather
// than by inspection. The step from those values to the published JSON is the ONE
// realisation every surface shares, internal/graphjson, and each caller runs it
// over a served result exactly as it runs it over a result the engine handed it
// in-process (SPEC/DATA_FORMATS.md § One Realisation of the Mapping). Mapping to
// JSON here instead would have created another copy of the property-type mapping
// and of the element mapping, free to drift from the one that already exists, and
// the identity would then be an assertion in a test rather than a property of the
// code.
//
// This paragraph used to say a copy here would be the THIRD of one mapping and
// the FOURTH of the other, because at the time it was written the mapping really
// was expressed twice and the element rows three times. Declining to add another
// was right and it left the ones that existed; rmp task #394 collapsed them, and
// internal/testenv now fails the build if a second appears.
//
// The mapping this file does own is therefore the protocol's encoding back onto
// expr.Value, and it is exact in both directions for everything the engine can
// produce: the encoder that wrote these bytes is
// GoGraph's bolt/server/entity_struct.go and session.go, and this is its inverse.
package graphclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
)

// conflictCode is the engine's code for a retriable serialisation conflict:
// two write transactions overlapped and the loser's snapshot no longer covered
// what it tried to change (SPEC/GRAPH.md § Concurrency Inside the Server, rule 3).
//
// It is matched as a STRING because that is the only form it arrives in: the
// failure crosses the protocol as a code and a message, and the typed error the
// engine classified is on the other side of the wire. The engine's own mapping
// (bolt/server/errors.go) is the authority for the value, and the code was chosen
// there over the plausible-looking Neo.TransientError.Transaction.Terminated
// precisely because the latter is silently demoted out of the transient family by
// the reference driver.
const conflictCode = "Neo.TransientError.Transaction.Outdated"

// timeoutCode is the engine's code for a statement its own deadline cut. The
// server bounds every statement at the statement budget
// (SPEC/GRAPH.md § Server Options), and this is what that bound looks like from
// the client: a TYPED failure on an intact connection, not a disconnect.
const timeoutCode = "Neo.ClientError.Transaction.TransactionTimedOut"

// userAgent identifies this client in the HELLO metadata. The server logs it and
// nothing branches on it.
const userAgent = "rmp-graph-client/1.0"

// authScheme is the scheme the LOGON carries. The server this client talks to
// runs with no authentication at all — the socket's mode is the whole of the
// access control (SPEC/GRAPH.md § Socket Path and Permissions) — and "none" is
// the scheme name that says so.
const authScheme = "none"

// Failure classifies why a statement did not produce a result, so that each
// surface can report the outcome in its own terms without re-deciding what
// happened.
//
// The classification is this package's; the WORDING is not. internal/commands
// turns these into the lines SPEC/COMMANDS.md § Graph Server Socket Error Lines
// publishes, and internal/web turns them into HTTP statuses
// (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 1). A package that
// decided the wording would have decided it for a surface whose wording it cannot
// see.
type Failure uint8

const (
	// FailureUnreachable is a server that could not be reached through the
	// socket at all: the connection failed, the handshake did not complete, or
	// the session could not be established. The statement was NOT sent, so
	// nothing ran — but the caller still must not fall back to the store, because
	// a socket that answers may belong to a server holding the lock
	// (SPEC/GRAPH.md § Server Resolution, rule 2).
	FailureUnreachable Failure = iota + 1

	// FailureLost is a connection lost AFTER the statement was sent. The
	// statement's outcome is genuinely unknown: a commit is durable before it is
	// acknowledged, so a connection that dies between the two leaves the caller
	// unable to tell whether it happened (rule 4).
	FailureLost

	// FailureUnanswered is the caller's own backstop deadline firing on an intact
	// connection. The server is alive and simply not answering, which is what a
	// statement the budget cut mid-write looks like from outside (rule 7). The
	// outcome is unknown for FailureLost's reason.
	FailureUnanswered

	// FailureStatement is the server reporting that the statement failed: a
	// parse error, an execution error, a schema refusal. It is a statement that
	// will fail the same way every time, which is what separates it from
	// FailureConflict.
	FailureStatement

	// FailureConflict is a serialisation conflict every attempt of the retry
	// policy collided on. It reaches the caller as itself once retrying has
	// stopped being the answer (rule 8).
	//
	// It is kept apart from FailureStatement for the reason the separate line
	// exists at all: the two demand OPPOSITE things of a caller. A statement the
	// engine refused must be corrected; a statement that lost every conflict is
	// valid and must be run again, and nothing distinguished the two while both
	// arrived here as one kind carrying an engine diagnostic the contract
	// deliberately declines to specify (SPEC/GRAPH.md § Concurrency Inside the
	// Server, rule 9).
	FailureConflict

	// FailureBudget is the statement exhausting the time budget the server
	// enforces. It is kept apart from FailureStatement because the two have
	// different remedies and SPEC/COMMANDS.md publishes a different line for
	// each: the budget line names a valid statement against a healthy store.
	FailureBudget

	// FailureMapping is a value the server returned that cannot be represented in
	// the published result shape. It fails the statement rather than substituting
	// a placeholder, so that a caller never reads a result that is quietly not
	// the one the graph holds (SPEC/DATA_FORMATS.md § Graph Client Result,
	// rule 3).
	FailureMapping
)

// SendError is a classified failure of [Send].
//
// Cause carries the transport observation for the three failures that are the
// connection's; Code and Diagnostic carry the server's own for the failures that
// are the statement's. They are strings rather than an error because that is what
// crossed the wire: the engine's typed error stayed on the server, and minting a
// Go error to hold its text would claim a chain this side never had.
type SendError struct {
	Cause      error
	Socket     string
	Code       string
	Diagnostic string
	Kind       Failure
	retriable  bool
}

// Error renders the failure for a diagnostic. It is deliberately NOT one of the
// published error lines: those are the CLI's and live in internal/commands, which
// selects between them on [SendError.Kind].
func (e *SendError) Error() string {
	switch e.Kind {
	case FailureUnreachable:
		return "graph server unreachable at " + e.Socket + ": " + causeText(e.Cause)
	case FailureLost:
		return "connection to the graph server at " + e.Socket + " lost: " + causeText(e.Cause)
	case FailureUnanswered:
		return "no answer from the graph server at " + e.Socket
	case FailureConflict:
		return "serialisation conflict on every attempt: " + e.Code + ": " + e.Diagnostic
	case FailureBudget:
		return "statement cut by the server's time budget: " + e.Diagnostic
	case FailureMapping:
		return "cannot represent a value the server returned: " + e.Diagnostic
	default:
		return "graph server reported " + e.Code + ": " + e.Diagnostic
	}
}

// Unwrap exposes the transport cause, so a caller may still ask errors.Is about
// it. It is nil for a failure the server reported.
func (e *SendError) Unwrap() error { return e.Cause }

// causeText renders a possibly-nil transport cause.
func causeText(err error) string {
	if err == nil {
		return "no further detail"
	}
	return err.Error()
}

// Notification is one advisory the server attached to a statement's result. It
// carries exactly the three fields the CLI prints per notification, in the shape
// SPEC/GRAPH.md § Query Notifications as Diagnostics fixes.
type Notification struct {
	Severity    string
	Code        string
	Description string
}

// Result is one statement's outcome as it came back from a server.
//
// Columns is empty for a statement that produces none, which is the discriminator
// SPEC/DATA_FORMATS.md § Graph Write Result uses to select between the two
// published shapes — the same discriminator the direct path uses, applied to the
// same information.
//
// Rows is never nil: a statement that matched nothing carries its columns and no
// rows, which the published shape renders as [] rather than null.
type Result struct {
	Columns       []string
	Rows          [][]expr.Value
	Notifications []Notification
}

// Send runs statement on the server listening at socketPath and returns what it
// produced.
//
// It does NOT resolve: the caller has already done that, and what it does with
// each of the four states differs by surface (SPEC/GRAPH.md § Server Resolution).
// Send is what happens after the one state in which a statement is sent.
//
// # The two bounds, and why they are not the same value
//
// The SERVER bounds the statement at the statement budget. Send keeps a deadline
// of its own so that a server which answers nothing at all cannot hold a caller
// for ever, and that deadline is the WAIT budget — the statement budget plus the
// backoff total — and deliberately not the statement budget itself. Rule 7 gives
// the reason: a statement that commits a few milliseconds before the budget
// expires has its acknowledgement in flight when a caller-side deadline of
// exactly the budget fires, and the caller would then report that nothing was
// written over a write that had succeeded. The later deadline makes the server's
// typed failure the one that arrives, so the budget line is printed only when the
// engine really did cut the statement and really did roll it back.
//
// Both bounds are read from graphlock, which declares the budget once, so they
// cannot come to disagree about the value.
//
// # The retry, and what it protects
//
// A serialisation conflict is a NORMAL outcome inside a server: MVCC is the only
// concurrency control, writers do not exclude one another, and a collision is
// detected rather than prevented. A client that surfaced one at once would report
// a defect where the store reported ordinary concurrency, so the statement is
// re-sent under the project's single retry policy. Re-sending is safe because the
// losing transaction committed nothing (rule 5), and it is bounded by the policy
// and by the deadline above, whichever ends first.
//
// # Which of the policy's two shapes, and why it is not the ladder
//
// Full jitter, backoff.RetryJitteredWithin, and not the fixed ladder every other
// retry in this project walks (SPEC/IMPLEMENTATION.md § Retry Logic is canonical
// for both shapes; SPEC/GRAPH.md § Concurrency Inside the Server, rule 4,
// requires this one here). The conflict is not a wait for a resource somebody
// holds: the winner committed before the loser learned it had lost, so there is
// nothing left to wait for, and what the delay buys is the loser leaving the
// contending set. That makes the conflict rate a function of the load the
// retries themselves offer — measured, six immediate attempts exhaust on 79.9%
// of statements where the fixed ladder exhausts on 0.15% — and it makes
// identical ladders, walked by every loser at the same instant, the wrong shape:
// they keep the contending set synchronised. Drawing each wait independently
// spreads the losers out, and inside the SAME 2500 ms budget it removed the
// failure entirely at sixteen writers on one node (0 in 18,000 against 0.15%)
// with a worst case SHORTER than the ladder's (rmp task #384).
//
// The residual is stated rather than hidden: a deadline that expires DURING a
// wait is observed at the end of it rather than at the instant it fires. Under
// this shape the longest a wait can be is the 250 ms ceiling rather than the
// ladder's second, so the residual is a quarter of what it was. The published
// line names a fixed duration rather than the elapsed time, so what a caller
// reads is unaffected; what it costs is that much extra waiting on an invocation
// that was going to fail.
func Send(ctx context.Context, socketPath, statement string) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, graphlock.WaitBudget())
	defer cancel()

	// "Whichever ends first": the policy's own sleeping total, or what is left of
	// the caller's deadline. The two are equal on an ordinary invocation, because
	// the wait budget is the larger of the two by construction; they part company
	// when the caller arrived with a nearer deadline of its own, which is what a
	// web request does.
	bound := backoff.Total()
	if remaining := timeUntilDeadline(ctx); remaining < bound {
		bound = remaining
	}

	return backoff.RetryJitteredWithin(bound, func() (*Result, error) {
		return sendOnce(ctx, socketPath, statement)
	}, isRetriable)
}

// timeUntilDeadline reports how long ctx has left, or the largest possible
// duration when it carries no deadline. Send always derives one, so the fallback
// is unreachable there and exists so the helper is total.
func timeUntilDeadline(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return time.Duration(1<<63 - 1)
	}
	return time.Until(deadline)
}

// isRetriable reports whether err is the one failure the policy re-sends: a
// serialisation conflict the server detected.
//
// Nothing else is retried, and the omissions are deliberate. A statement the
// engine refused fails the same way every time. A connection lost after the
// statement was sent must NOT be re-sent, because the statement may already have
// committed (SPEC/GRAPH.md § Server Resolution, rule 4). An unreachable server is
// a resolution failure the caller reports rather than works around.
func isRetriable(err error) bool {
	var se *SendError
	return errors.As(err, &se) && se.retriable
}

// sendOnce performs one attempt: connect, handshake, authenticate, RUN, PULL.
//
// The boundary that matters is the RUN write. Before it, a transport failure is
// a server that could not be reached and nothing has run; after it, the statement
// is in flight and its outcome is unknown, which is a different failure with a
// different line and a different rule about falling back.
func sendOnce(ctx context.Context, socketPath, statement string) (*Result, error) {
	if err := ctx.Err(); err != nil {
		// The deadline expired while the retry policy was sleeping. Attempting
		// the connection would only produce the same verdict a moment later.
		return nil, &SendError{Kind: FailureUnanswered, Socket: socketPath, Cause: err}
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, &SendError{Kind: FailureUnreachable, Socket: socketPath, Cause: err}
	}
	defer conn.Close() //nolint:errcheck // the result is already read or already lost; a close error cannot change it

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, &SendError{Kind: FailureUnreachable, Socket: socketPath, Cause: err}
		}
	}

	s := &session{
		socket: socketPath,
		conn:   conn,
		reader: proto.NewChunkedReader(conn),
		writer: proto.NewChunkedWriter(conn),
	}

	version, err := handshake(ctx, conn)
	if err != nil {
		return nil, &SendError{Kind: FailureUnreachable, Socket: socketPath, Cause: err}
	}
	if err := s.authenticate(version); err != nil {
		return nil, err
	}
	return s.run(ctx, statement)
}

// session is one connection's client half of the Bolt exchange.
//
// It is not safe for concurrent use and is never shared: one is built per
// attempt, used once, and dropped with its connection. The engine's own packages
// carry the same restriction (proto and packstream are both documented as unsafe
// for concurrent use), and this respects it by construction rather than by
// discipline.
type session struct {
	conn   net.Conn
	reader *proto.ChunkedReader
	writer *proto.ChunkedWriter
	socket string

	// sent records that the statement has crossed the wire, which is what
	// separates a server that could not be reached from a statement whose outcome
	// is unknown.
	sent bool
}

// authenticate performs HELLO and, on Bolt 5.1 and above, the LOGON that version
// split out of it.
//
// Sending a LOGON to a server that authenticates inline at HELLO would be an
// illegal transition, and omitting one on a server that defers authentication
// leaves the session short of READY, so the version decides. The threshold is the
// protocol's, not a preference: 5.1 introduced the split flow.
func (s *session) authenticate(version proto.Version) error {
	hello := &proto.Hello{Extra: map[string]packstream.Value{
		"user_agent": userAgent,
	}}
	if !deferredAuth(version) {
		// Bolt 5.0 and below authenticate inline, so the credentials the LOGON
		// would have carried travel on the HELLO instead.
		hello.Extra["scheme"] = authScheme
	}
	if err := s.exchangeSuccess(hello); err != nil {
		return err
	}
	if !deferredAuth(version) {
		return nil
	}
	return s.exchangeSuccess(&proto.Logon{Auth: map[string]packstream.Value{
		"scheme": authScheme,
	}})
}

// deferredAuth reports whether version defers authentication from HELLO to
// LOGON, which Bolt 5.1 and later do.
func deferredAuth(version proto.Version) bool {
	if version.Major != 5 {
		return version.Major > 5
	}
	return version.Minor >= 1
}

// exchangeSuccess sends one request and requires a SUCCESS in reply. It is the
// shape of every step of the session's setup, where anything but a success means
// the server could not be reached in a usable state.
func (s *session) exchangeSuccess(request any) error {
	if err := s.write(request); err != nil {
		return err
	}
	response, err := s.read()
	if err != nil {
		return err
	}
	if _, ok := response.(*proto.Success); ok {
		return nil
	}
	if failure, ok := response.(*proto.Failure); ok {
		return &SendError{
			Kind: FailureUnreachable, Socket: s.socket,
			Code: failure.Code, Diagnostic: failure.Message,
			Cause: fmt.Errorf("the server refused the session: %s: %w", failure.Code, errProtocolViolation),
		}
	}
	return &SendError{
		Kind: FailureUnreachable, Socket: s.socket,
		Cause: fmt.Errorf("the server answered %T where a success was required: %w", response, errProtocolViolation),
	}
}

// run sends the statement and streams its whole result back.
//
// The statement executes at RUN and not at PULL, measured on rmp task #367: an
// aggregating statement returned its RUN success only after its full budget had
// been spent, and the PULL that followed carried the failure. Both messages are
// therefore treated as places a statement's failure can surface, and neither is
// assumed to be the cheap one.
func (s *session) run(ctx context.Context, statement string) (*Result, error) {
	runMsg := &proto.Run{
		Query:      statement,
		Parameters: map[string]packstream.Value{},
		Extra:      map[string]packstream.Value{},
	}
	if err := s.write(runMsg); err != nil {
		return nil, err
	}
	// From here the statement is in flight, whatever happens to the connection.
	s.sent = true

	response, err := s.read()
	if err != nil {
		return nil, err
	}
	success, ok := response.(*proto.Success)
	if !ok {
		return nil, s.responseFailure(ctx, response)
	}

	result := &Result{Columns: columnsOf(success.Metadata), Rows: [][]expr.Value{}}

	if err := s.write(&proto.Pull{N: -1, QID: -1}); err != nil {
		return nil, err
	}
	for {
		response, err := s.read()
		if err != nil {
			return nil, err
		}
		switch m := response.(type) {
		case *proto.Record:
			row, mapErr := s.mapRow(m.Data)
			if mapErr != nil {
				return nil, mapErr
			}
			result.Rows = append(result.Rows, row)
		case *proto.Success:
			result.Notifications = notificationsOf(m.Metadata)
			return result, nil
		default:
			return nil, s.responseFailure(ctx, response)
		}
	}
}

// mapRow maps one record's fields onto the engine's value model.
func (s *session) mapRow(data []packstream.Value) ([]expr.Value, error) {
	row := make([]expr.Value, len(data))
	for i, cell := range data {
		value, err := toExprValue(cell)
		if err != nil {
			return nil, &SendError{Kind: FailureMapping, Socket: s.socket, Diagnostic: err.Error()}
		}
		row[i] = value
	}
	return row, nil
}

// responseFailure classifies a server response that is not the one the exchange
// expected.
//
// A FAILURE carries the server's own code, which is what separates the three
// outcomes a caller reports differently: the budget, a retriable conflict, and
// everything else. An IGNORED or an unexpected message type is neither the
// statement's failure nor the connection's, so it is reported as the connection
// having become unusable, which is what it is.
func (s *session) responseFailure(ctx context.Context, response any) error {
	failure, ok := response.(*proto.Failure)
	if !ok {
		return &SendError{
			Kind: FailureLost, Socket: s.socket,
			Cause: fmt.Errorf("the server answered %T mid-statement: %w", response, errProtocolViolation),
		}
	}
	switch failure.Code {
	case timeoutCode:
		// The server cut the statement at its own budget and answered. The
		// caller's deadline is deliberately later than that, so this is the
		// failure that should arrive — and it says truthfully that nothing was
		// written, because the engine rolled the transaction back before
		// reporting it.
		return &SendError{
			Kind: FailureBudget, Socket: s.socket,
			Code: failure.Code, Diagnostic: failure.Message,
		}
	case conflictCode:
		return &SendError{
			Kind: FailureConflict, Socket: s.socket,
			Code: failure.Code, Diagnostic: failure.Message,
			// Retried while the policy and the deadline allow it. Once neither
			// does, this same value reaches the caller as the contention it is,
			// under a kind and a published line of its own
			// (SPEC/GRAPH.md § Server Resolution, rule 8).
			retriable: ctx.Err() == nil,
		}
	default:
		return &SendError{
			Kind: FailureStatement, Socket: s.socket,
			Code: failure.Code, Diagnostic: failure.Message,
		}
	}
}

// write frames and sends one request message.
func (s *session) write(request any) error {
	var buf bytes.Buffer
	enc := packstream.NewEncoder(&buf)
	if err := proto.EncodeRequest(enc, request); err != nil {
		return s.transportFailure(err)
	}
	if err := enc.Flush(); err != nil {
		return s.transportFailure(err)
	}
	if err := s.writer.WriteMessage(buf.Bytes()); err != nil {
		return s.transportFailure(err)
	}
	return nil
}

// read receives one response message.
func (s *session) read() (any, error) {
	msg, err := s.reader.ReadMessage()
	if err != nil {
		return nil, s.transportFailure(err)
	}
	response, err := proto.DecodeResponse(packstream.NewDecoder(bytes.NewReader(msg)))
	if err != nil {
		return nil, s.transportFailure(err)
	}
	return response, nil
}

// transportFailure classifies an I/O or framing failure by WHEN it happened and
// by WHAT ended the wait.
//
// Before the statement was sent, nothing ran and the server was simply not
// reachable through this socket. After it, the statement is in flight and the
// question becomes which of two things happened: the connection died, or it
// stayed up and the server did not answer inside the caller's backstop. They are
// told apart by the deadline, because a deadline that fires leaves the connection
// intact and the server alive — which is exactly what a statement the budget cut
// mid-write looks like from outside, the engine's undo replay running past the
// deadline by a factor nothing bounds.
func (s *session) transportFailure(err error) error {
	if !s.sent {
		return &SendError{Kind: FailureUnreachable, Socket: s.socket, Cause: err}
	}
	if errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return &SendError{Kind: FailureUnanswered, Socket: s.socket, Cause: err}
	}
	return &SendError{Kind: FailureLost, Socket: s.socket, Cause: err}
}

// columnsOf reads the result's ordered column names out of a RUN success's
// metadata. A statement that produces no columns carries no "fields" key, and the
// nil this returns for it is the discriminator between the two published shapes.
func columnsOf(metadata map[string]packstream.Value) []string {
	raw, ok := metadata["fields"].([]packstream.Value)
	if !ok {
		return nil
	}
	columns := make([]string, 0, len(raw))
	for _, field := range raw {
		name, ok := field.(string)
		if !ok {
			continue
		}
		columns = append(columns, name)
	}
	if len(columns) == 0 {
		return nil
	}
	return columns
}

// notificationsOf reads the advisory notifications out of the final success's
// metadata. The key is absent when the engine attached none, which is the
// ordinary case.
func notificationsOf(metadata map[string]packstream.Value) []Notification {
	raw, ok := metadata["notifications"].([]packstream.Value)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]Notification, 0, len(raw))
	for _, entry := range raw {
		fields, ok := entry.(map[string]packstream.Value)
		if !ok {
			continue
		}
		out = append(out, Notification{
			Severity:    stringField(fields, "severity"),
			Code:        stringField(fields, "code"),
			Description: stringField(fields, "description"),
		})
	}
	return out
}

// stringField reads one string out of a protocol map, yielding the empty string
// for a key that is absent or carries something else.
func stringField(fields map[string]packstream.Value, key string) string {
	s, _ := fields[key].(string)
	return s
}

// Bolt structure tags. They are the tags GoGraph's own encoder writes
// (bolt/server/entity_struct.go and session.go), transcribed from the decoder
// contract both sides were built against — the neo4j-go-driver hydrator — so the
// two halves of this product describe one protocol.
const (
	tagNode                byte = 0x4E // 'N' [id, labels, properties, element_id]
	tagRelationship        byte = 0x52 // 'R' [id, start, end, type, properties, +3 element ids]
	tagUnboundRelationship byte = 0x72 // 'r' [id, type, properties, element_id]
	tagPath                byte = 0x50 // 'P' [nodes, unbound relationships, indices]
	tagDate                byte = 0x44 // 'D' [epoch day]
	tagLocalTime           byte = 0x74 // 't' [nanoseconds of day]
	tagTime                byte = 0x54 // 'T' [nanoseconds of day, zone offset seconds]
	tagLocalDateTime       byte = 0x64 // 'd' [epoch second, nanosecond]
	tagDateTimeOffset      byte = 0x49 // 'I' [utc epoch second, nanosecond, zone offset seconds]
	tagDateTimeZoneID      byte = 0x69 // 'i' [utc epoch second, nanosecond, zone id]
	tagLegacyDateTimeOff   byte = 0x46 // 'F' [local epoch second, nanosecond, zone offset seconds]
	tagLegacyDateTimeZone  byte = 0x66 // 'f' [local epoch second, nanosecond, zone id]
	tagDuration            byte = 0x45 // 'E' [months, days, seconds, nanoseconds]
)

// errUnrepresentable is the class every failure of the protocol-to-value mapping
// carries.
//
// It exists because a mapping failure has to be MATCHABLE and not merely
// readable: SPEC/DATA_FORMATS.md § Graph Client Result, rule 3, makes such a
// value fail the statement rather than become a placeholder, and a caller that
// has to tell that outcome from an engine refusal needs something better than a
// string comparison. Each wrap adds what was found; this says what was wrong with
// it.
var errUnrepresentable = errors.New("the published result shape has no representation for it")

// errProtocolViolation is the class of a server answer the exchange cannot
// continue from: a message where another was required, or a session the server
// refused. It is not the statement's failure and not the connection's — the bytes
// arrived and were decoded — so it is named apart from both.
var errProtocolViolation = errors.New("the server did not answer the protocol")

// secondsPerDay is the divisor GoGraph's encoder used to turn a date into an
// epoch day, and therefore the multiplier that inverts it.
const secondsPerDay = 86400

// toExprValue maps one protocol value onto the engine's value model.
//
// A value it cannot map is an ERROR and never a substitute. The published result
// shape is a claim about what the graph holds, and a placeholder in place of a
// value would make that claim false in the one case a caller most needs it to be
// true (SPEC/DATA_FORMATS.md § Graph Client Result, rule 3).
func toExprValue(v packstream.Value) (expr.Value, error) {
	switch x := v.(type) {
	case nil:
		return expr.Null, nil
	case bool:
		return expr.BoolValue(x), nil
	case int64:
		return expr.IntegerValue(x), nil
	case float64:
		return expr.FloatValue(x), nil
	case string:
		return expr.StringValue(x), nil
	case []byte:
		// PackStream carries a byte string; the published shape carries a
		// base64-standard-encoded JSON string for one, and the engine's value
		// model has no byte kind to hold it in between. Encoding here is what
		// makes the serialiser downstream produce exactly the published
		// representation without learning a new kind
		// (SPEC/DATA_FORMATS.md § Property-Type Mapping).
		return expr.StringValue(base64.StdEncoding.EncodeToString(x)), nil
	case []packstream.Value:
		return toListValue(x)
	case map[string]packstream.Value:
		return toMapValue(x)
	case packstream.Struct:
		return toStructValue(x)
	default:
		return nil, fmt.Errorf("unsupported protocol value of type %T: %w", v, errUnrepresentable)
	}
}

// toListValue maps a protocol list element-wise.
func toListValue(raw []packstream.Value) (expr.ListValue, error) {
	out := make(expr.ListValue, len(raw))
	for i, element := range raw {
		value, err := toExprValue(element)
		if err != nil {
			return nil, err
		}
		out[i] = value
	}
	return out, nil
}

// toMapValue maps a protocol map value-wise. It returns a non-nil map for an
// empty one, so a property bag with no entries renders as {} rather than null.
func toMapValue(raw map[string]packstream.Value) (expr.MapValue, error) {
	out := make(expr.MapValue, len(raw))
	for key, element := range raw {
		value, err := toExprValue(element)
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

// toStructValue maps a protocol structure onto the value kind its tag names.
//
//nolint:gocyclo,cyclop // one branch per protocol structure tag; splitting the dispatch would hide which tags are covered
func toStructValue(s packstream.Struct) (expr.Value, error) {
	switch s.Tag {
	case tagNode:
		return toNodeValue(s)
	case tagRelationship:
		return toRelationshipValue(s)
	case tagPath:
		return toPathValue(s)
	case tagDate:
		day, err := intField(s, 0)
		if err != nil {
			return nil, err
		}
		return expr.DateFromTime(time.Unix(day*secondsPerDay, 0).UTC()), nil
	case tagLocalTime:
		nanos, err := intField(s, 0)
		if err != nil {
			return nil, err
		}
		return expr.LocalTimeValue{Nanos: nanos}, nil
	case tagTime:
		nanos, err := intField(s, 0)
		if err != nil {
			return nil, err
		}
		offset, err := narrowField(s, 1)
		if err != nil {
			return nil, err
		}
		return expr.TimeValue{Nanos: nanos, OffsetSec: offset}, nil
	case tagLocalDateTime:
		return toLocalDateTimeValue(s)
	case tagDateTimeOffset, tagLegacyDateTimeOff:
		return toOffsetDateTimeValue(s, s.Tag == tagLegacyDateTimeOff)
	case tagDateTimeZoneID, tagLegacyDateTimeZone:
		return toZonedDateTimeValue(s, s.Tag == tagLegacyDateTimeZone)
	case tagDuration:
		return toDurationValue(s)
	default:
		return nil, fmt.Errorf("unsupported protocol structure tag 0x%02X: %w", s.Tag, errUnrepresentable)
	}
}

// toNodeValue maps the 'N' structure. The element id Bolt 5 appends is read and
// dropped: publishing it would make a result depend on which path carried it,
// which is the one thing the published shape may not do
// (SPEC/DATA_FORMATS.md § Graph Client Result, rules 1 and 2).
func toNodeValue(s packstream.Struct) (expr.NodeValue, error) {
	id, err := identifierField(s, 0)
	if err != nil {
		return expr.NodeValue{}, err
	}
	labels, err := stringListField(s, 1)
	if err != nil {
		return expr.NodeValue{}, err
	}
	properties, err := propertiesField(s, 2)
	if err != nil {
		return expr.NodeValue{}, err
	}
	return expr.NodeValue{ID: id, Labels: labels, Properties: properties}, nil
}

// toRelationshipValue maps the 'R' structure, which carries its endpoints.
func toRelationshipValue(s packstream.Struct) (expr.RelationshipValue, error) {
	id, err := identifierField(s, 0)
	if err != nil {
		return expr.RelationshipValue{}, err
	}
	startID, err := identifierField(s, 1)
	if err != nil {
		return expr.RelationshipValue{}, err
	}
	endID, err := identifierField(s, 2)
	if err != nil {
		return expr.RelationshipValue{}, err
	}
	relType, err := stringFieldAt(s, 3)
	if err != nil {
		return expr.RelationshipValue{}, err
	}
	properties, err := propertiesField(s, 4)
	if err != nil {
		return expr.RelationshipValue{}, err
	}
	return expr.RelationshipValue{
		ID: id, StartID: startID, EndID: endID,
		Type: relType, Properties: properties,
	}, nil
}

// toPathValue maps the 'P' structure by REPLAYING its index list rather than by
// trusting the order of its node list.
//
// A path carries its relationships in the unbound form, which omits the
// endpoints, and supplies them through pairs of indices instead: a one-based,
// signed relationship index whose sign says whether the hop traverses the
// relationship in its natural direction, and a zero-based node index naming the
// hop's end node. Replaying that is what makes a reverse-traversed hop come back
// with its endpoints the right way round, and it is the only way to recover the
// endpoints the unbound form dropped.
func toPathValue(s packstream.Struct) (expr.PathValue, error) {
	rawNodes, err := listField(s, 0)
	if err != nil {
		return expr.PathValue{}, err
	}
	rawRels, err := listField(s, 1)
	if err != nil {
		return expr.PathValue{}, err
	}
	indices, err := listField(s, 2)
	if err != nil {
		return expr.PathValue{}, err
	}
	if len(indices)%2 != 0 {
		return expr.PathValue{}, fmt.Errorf("a path carries %d index entries, which is not a whole number of hops: %w", len(indices), errUnrepresentable)
	}

	nodes, err := pathNodes(rawNodes)
	if err != nil {
		return expr.PathValue{}, err
	}
	if len(nodes) == 0 {
		return expr.PathValue{}, errEmptyPath
	}

	unbound, err := pathRelationships(rawRels)
	if err != nil {
		return expr.PathValue{}, err
	}

	walk := make([]expr.NodeValue, 0, len(indices)/2+1)
	hops := make([]expr.RelationshipValue, 0, len(indices)/2)
	walk = append(walk, nodes[0])
	previous := nodes[0]
	for i := 0; i < len(indices); i += 2 {
		relIndex, ok := indices[i].(int64)
		if !ok {
			return expr.PathValue{}, fmt.Errorf("a path's relationship index is %T rather than an integer: %w", indices[i], errUnrepresentable)
		}
		nodeIndex, ok := indices[i+1].(int64)
		if !ok {
			return expr.PathValue{}, fmt.Errorf("a path's node index is %T rather than an integer: %w", indices[i+1], errUnrepresentable)
		}
		if nodeIndex < 0 || nodeIndex >= int64(len(nodes)) {
			return expr.PathValue{}, fmt.Errorf("a path's node index %d is outside its %d nodes: %w", nodeIndex, len(nodes), errUnrepresentable)
		}
		next := nodes[nodeIndex]

		reversed := relIndex < 0
		slot := relIndex - 1
		if reversed {
			slot = -relIndex - 1
		}
		if slot < 0 || slot >= int64(len(unbound)) {
			return expr.PathValue{}, fmt.Errorf("a path's relationship index %d is outside its %d relationships: %w", relIndex, len(unbound), errUnrepresentable)
		}
		hop := unbound[slot]
		if reversed {
			hop.StartID, hop.EndID = next.ID, previous.ID
		} else {
			hop.StartID, hop.EndID = previous.ID, next.ID
		}
		hops = append(hops, hop)
		walk = append(walk, next)
		previous = next
	}

	return expr.PathValue{Nodes: walk, Relationships: hops}, nil
}

// errEmptyPath is a path structure carrying no node at all, which no well-formed
// path is: the shortest one is a single node with no relationship.
var errEmptyPath = fmt.Errorf("a path carries no node: %w", errUnrepresentable)

// pathNodes maps a path's node list.
func pathNodes(raw []packstream.Value) ([]expr.NodeValue, error) {
	out := make([]expr.NodeValue, len(raw))
	for i, entry := range raw {
		s, ok := entry.(packstream.Struct)
		if !ok || s.Tag != tagNode {
			return nil, fmt.Errorf("a path's node %d is %T rather than a node structure: %w", i, entry, errUnrepresentable)
		}
		node, err := toNodeValue(s)
		if err != nil {
			return nil, err
		}
		out[i] = node
	}
	return out, nil
}

// pathRelationships maps a path's unbound-relationship list. The endpoints are
// left zero here and are filled in by the index replay, which is the only place
// they exist.
func pathRelationships(raw []packstream.Value) ([]expr.RelationshipValue, error) {
	out := make([]expr.RelationshipValue, len(raw))
	for i, entry := range raw {
		s, ok := entry.(packstream.Struct)
		if !ok || s.Tag != tagUnboundRelationship {
			return nil, fmt.Errorf("a path's relationship %d is %T rather than an unbound relationship structure: %w", i, entry, errUnrepresentable)
		}
		id, err := identifierField(s, 0)
		if err != nil {
			return nil, err
		}
		relType, err := stringFieldAt(s, 1)
		if err != nil {
			return nil, err
		}
		properties, err := propertiesField(s, 2)
		if err != nil {
			return nil, err
		}
		out[i] = expr.RelationshipValue{ID: id, Type: relType, Properties: properties}
	}
	return out, nil
}

// toLocalDateTimeValue maps the 'd' structure. The engine stores a zoneless
// date-time as a UTC time.Time whose zone is a sentinel rather than an offset, so
// the instant is rebuilt in UTC and the wall clock comes back unchanged.
func toLocalDateTimeValue(s packstream.Struct) (expr.LocalDateTimeValue, error) {
	seconds, err := intField(s, 0)
	if err != nil {
		return expr.LocalDateTimeValue{}, err
	}
	nanos, err := intField(s, 1)
	if err != nil {
		return expr.LocalDateTimeValue{}, err
	}
	return expr.LocalDateTimeValue{T: time.Unix(seconds, nanos).UTC()}, nil
}

// toOffsetDateTimeValue maps the 'I' and 'F' structures, which differ only in
// what their seconds field means: Bolt 5 carries the true UTC instant, while the
// legacy Bolt 4.4 form carries the wall clock expressed as if it were UTC. The
// offset is subtracted from the legacy form to recover the instant.
func toOffsetDateTimeValue(s packstream.Struct, legacy bool) (expr.DateTimeValue, error) {
	seconds, err := intField(s, 0)
	if err != nil {
		return expr.DateTimeValue{}, err
	}
	nanos, err := intField(s, 1)
	if err != nil {
		return expr.DateTimeValue{}, err
	}
	offset, err := intField(s, 2)
	if err != nil {
		return expr.DateTimeValue{}, err
	}
	if legacy {
		seconds -= offset
	}
	zone := time.FixedZone("", int(offset)) //nolint:gosec // G115: a zone offset is bounded by ±18 hours, far inside int
	return expr.DateTimeValue{T: time.Unix(seconds, nanos).In(zone)}, nil
}

// toZonedDateTimeValue maps the 'i' and 'f' structures, whose third field is an
// IANA zone name rather than an offset.
//
// A name the running system cannot resolve falls back to UTC rather than failing
// the statement: the instant is already exact — it is carried by the seconds and
// nanoseconds — and every published rendering of a date-time is in UTC, so the
// zone affects nothing a caller reads. Failing here would refuse a result that is
// correct because a time-zone database is missing.
func toZonedDateTimeValue(s packstream.Struct, legacy bool) (expr.DateTimeValue, error) {
	seconds, err := intField(s, 0)
	if err != nil {
		return expr.DateTimeValue{}, err
	}
	nanos, err := intField(s, 1)
	if err != nil {
		return expr.DateTimeValue{}, err
	}
	name, err := stringFieldAt(s, 2)
	if err != nil {
		return expr.DateTimeValue{}, err
	}
	zone, loadErr := time.LoadLocation(name)
	if loadErr != nil {
		zone = time.UTC
	}
	if legacy {
		// The legacy form's seconds are the wall clock expressed as if UTC, so
		// the instant is whatever that wall clock is in the named zone.
		asUTC := time.Unix(seconds, nanos).UTC()
		return expr.DateTimeValue{T: time.Date(asUTC.Year(), asUTC.Month(), asUTC.Day(),
			asUTC.Hour(), asUTC.Minute(), asUTC.Second(), asUTC.Nanosecond(), zone)}, nil
	}
	return expr.DateTimeValue{T: time.Unix(seconds, nanos).In(zone)}, nil
}

// toDurationValue maps the 'E' structure.
func toDurationValue(s packstream.Struct) (expr.DurationValue, error) {
	months, err := intField(s, 0)
	if err != nil {
		return expr.DurationValue{}, err
	}
	days, err := intField(s, 1)
	if err != nil {
		return expr.DurationValue{}, err
	}
	seconds, err := intField(s, 2)
	if err != nil {
		return expr.DurationValue{}, err
	}
	nanos, err := narrowField(s, 3)
	if err != nil {
		return expr.DurationValue{}, err
	}
	return expr.DurationValue{Months: months, Days: days, Seconds: seconds, Nanos: nanos}, nil
}

// intField reads field i of s as an integer.
func intField(s packstream.Struct, i int) (int64, error) {
	if i >= len(s.Fields) {
		return 0, fmt.Errorf("structure 0x%02X carries %d fields, and field %d was required: %w", s.Tag, len(s.Fields), i, errUnrepresentable)
	}
	n, ok := s.Fields[i].(int64)
	if !ok {
		return 0, fmt.Errorf("field %d of structure 0x%02X is %T rather than an integer: %w", i, s.Tag, s.Fields[i], errUnrepresentable)
	}
	return n, nil
}

// identifierField reads field i of s as an entity identifier.
//
// The engine's identifiers are uint64 and the protocol carries them as int64, so
// the narrowing has to happen somewhere; doing it here rather than at each call
// site is what lets a NEGATIVE value be refused instead of wrapping silently into
// an enormous one. Nothing this server sends is negative — the same engine wrote
// these bytes from its own uint64 — but the value arrives over a socket, and a
// mapping that trusted it would turn a corrupt or hostile message into a node
// that appears to exist (SPEC/DATA_FORMATS.md § Graph Client Result, rule 3).
func identifierField(s packstream.Struct, i int) (uint64, error) {
	n, err := intField(s, i)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("field %d of structure 0x%02X carries the negative identifier %d: %w",
			i, s.Tag, n, errUnrepresentable)
	}
	return uint64(n), nil
}

// narrowField reads field i of s as a 32-bit integer.
//
// Two protocol fields are 32-bit in the engine's value model and 64-bit on the
// wire: a zone offset in seconds, and a duration's sub-second component. Both are
// bounded far inside int32 by their own definitions — a zone offset by ±18 hours,
// a nanosecond component by 999,999,999 — so a value outside the range is a
// message that is not what it claims to be, and it is refused rather than
// truncated into a plausible one.
func narrowField(s packstream.Struct, i int) (int32, error) {
	n, err := intField(s, i)
	if err != nil {
		return 0, err
	}
	if n < math.MinInt32 || n > math.MaxInt32 {
		return 0, fmt.Errorf("field %d of structure 0x%02X carries %d, which is outside the range "+
			"the value model holds it in: %w", i, s.Tag, n, errUnrepresentable)
	}
	return int32(n), nil
}

// stringFieldAt reads field i of s as a string.
func stringFieldAt(s packstream.Struct, i int) (string, error) {
	if i >= len(s.Fields) {
		return "", fmt.Errorf("structure 0x%02X carries %d fields, and field %d was required: %w", s.Tag, len(s.Fields), i, errUnrepresentable)
	}
	value, ok := s.Fields[i].(string)
	if !ok {
		return "", fmt.Errorf("field %d of structure 0x%02X is %T rather than a string: %w", i, s.Tag, s.Fields[i], errUnrepresentable)
	}
	return value, nil
}

// listField reads field i of s as a list.
func listField(s packstream.Struct, i int) ([]packstream.Value, error) {
	if i >= len(s.Fields) {
		return nil, fmt.Errorf("structure 0x%02X carries %d fields, and field %d was required: %w", s.Tag, len(s.Fields), i, errUnrepresentable)
	}
	if s.Fields[i] == nil {
		return nil, nil
	}
	list, ok := s.Fields[i].([]packstream.Value)
	if !ok {
		return nil, fmt.Errorf("field %d of structure 0x%02X is %T rather than a list: %w", i, s.Tag, s.Fields[i], errUnrepresentable)
	}
	return list, nil
}

// stringListField reads field i of s as a list of strings. It returns a non-nil
// empty slice for an empty list, because a node with no labels publishes [] and
// never null (SPEC/DATA_FORMATS.md § Graph element mapping, rule 2).
func stringListField(s packstream.Struct, i int) ([]string, error) {
	raw, err := listField(s, i)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(raw))
	for j, entry := range raw {
		value, ok := entry.(string)
		if !ok {
			return nil, fmt.Errorf("entry %d of field %d of structure 0x%02X is %T rather than a string: %w", j, i, s.Tag, entry, errUnrepresentable)
		}
		out[j] = value
	}
	return out, nil
}

// propertiesField reads field i of s as a property bag. It returns a non-nil map
// for an absent or empty one, so the published shape carries {} rather than null.
func propertiesField(s packstream.Struct, i int) (expr.MapValue, error) {
	if i >= len(s.Fields) {
		return nil, fmt.Errorf("structure 0x%02X carries %d fields, and field %d was required: %w", s.Tag, len(s.Fields), i, errUnrepresentable)
	}
	if s.Fields[i] == nil {
		return expr.MapValue{}, nil
	}
	raw, ok := s.Fields[i].(map[string]packstream.Value)
	if !ok {
		return nil, fmt.Errorf("field %d of structure 0x%02X is %T rather than a map: %w", i, s.Tag, s.Fields[i], errUnrepresentable)
	}
	return toMapValue(raw)
}
