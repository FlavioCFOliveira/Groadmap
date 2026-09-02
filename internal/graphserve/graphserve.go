// Package graphserve owns the lifecycle of the server `rmp graph serve` runs:
// the Unix domain socket listener, the options the engine's Bolt server is
// constructed with, the drain that precedes shutdown, and the ordered teardown.
// SPEC/GRAPH.md § The Dedicated Graph Server is canonical for all of it.
//
// # What it does not own
//
// It constructs no engine. It calls internal/graphstore for the store, the lock,
// the engine and the checkpoint, exactly as the other two surfaces do, so the
// single construction SPEC/GRAPH.md § Engine Constructor by Path fixes stays
// single. It reads no statement, serialises no result, and chooses no exit code:
// those are the CLI's, and internal/commands keeps them. And it is not a client
// of itself — the startup probe of step 3 below is internal/graphclient's, the
// one resolution rule every surface follows.
//
// # Three facts about the engine's server that shape this package
//
// All three were measured on rmp task #360 against the pinned release, and none
// of them is inferable from the engine's documentation:
//
//  1. The convenience entry point that both listens and serves binds a TCP
//     address and cannot be told otherwise, so this package builds the `unix`
//     listener itself and hands it to Serve.
//  2. Serve CLOSES that listener itself, on its own exit path and on Shutdown's.
//     This package must therefore not treat itself as the listener's owner, and
//     the wrapper below is written so that a close from either side is safe.
//  3. Serve and Shutdown CUT sessions rather than draining them. Every
//     connection's context derives from the accept context, and both stop
//     mechanisms cancel that context BEFORE waiting, so an idle authenticated
//     session's client sees a broken connection rather than an answer. The drain
//     SPEC/GRAPH.md § Server Shutdown and the Drain requires is therefore this
//     package's own work, and [serverListener] is what makes it possible: it
//     separates "stop accepting" from "cut the sessions", which the engine's own
//     stop mechanisms fuse.
package graphserve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/server"
	"github.com/FlavioCFOliveira/GoGraph/store"
	"github.com/FlavioCFOliveira/GoGraph/store/checkpoint"
	"github.com/FlavioCFOliveira/GoGraph/store/wal"
	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// connTimeoutMultiple is how many statement budgets the server's connection
// timeout is, and it is a MULTIPLE rather than a constant of its own so the two
// move together and cannot drift apart (SPEC/GRAPH.md § Server Options).
//
// The engine documents its connection timeout as the silent gap between
// messages, but it is armed as a read deadline on the socket while the message
// loop is busy executing the previous statement: a statement that runs longer
// than it destroys its own connection mid-flight, whatever the statement's own
// budget says. Measured on rmp task #360, the cut tracked the connection timeout
// exactly and ignored a statement timeout four times its size. The engine's
// default for it EQUALS its default statement timeout, so a server left at both
// defaults is one whose slowest permitted statement is guaranteed to die as a
// transport error rather than as a typed failure. This is the one option whose
// default is actively wrong here.
//
// Twelve is derived and not picked. A statement the deadline cuts while it is
// writing holds the engine call open for the budget multiplied by a factor the
// statement itself sets, measured from 1.005x to 6.97x, and the longest such hold
// measured at the budget in force is 34.5 seconds (rmp task #380). Sixty seconds
// clears that by 1.7x, and it clears the longest statement the server actually
// permits, 5 seconds, by twelve.
//
// The residual is stated rather than removed, because no multiple removes it:
// nothing measured establishes a ceiling on that factor, so no finite connection
// timeout guarantees that a cut write is answered rather than disconnected.
const connTimeoutMultiple = 12

// maxConnections is the ceiling on the connections the server accepts at once,
// and it is the ONE count that bounds every per-statement and per-transaction
// resource the server holds (SPEC/GRAPH.md § Server Options).
//
// The engine's default is 1024 (server.Options.MaxConnections, whose zero and
// negative values both take that default). Groadmap does not leave it there.
//
// # What a connection ceiling actually bounds here
//
// A connection carries at most ONE autocommit statement in flight — the Bolt v5
// state machine admits no second concurrent autocommit stream on one connection —
// and at most ONE open explicit transaction. Every other count the engine caps is
// therefore reached through a connection first, which is what makes this single
// number the bound on statements in flight, on open transactions, and on the undo
// log each writing statement accumulates. There is no second lever, and this one
// is not a tuning knob among several: it is the whole of the capacity decision.
//
// # The one cost it bounds in bytes, and the one it does not
//
// Only the inbound REASSEMBLY buffers scale with this ceiling. A connection's
// chunked reader accumulates one message up to MaxMessageBytes, whose default is
// 16 MiB, and there is one such reader per connection: the product is 16 GiB at
// the engine's own 1024 and 2 GiB at this 128. It is reachable
// PRE-AUTHENTICATION, because a HELLO must be decoded before it can be
// authenticated, which is what makes it a bound on what an unauthenticated caller
// can make this process allocate.
//
// The decoded-collection half does NOT scale with it, and saying that it did
// would overstate what this number buys. DefaultMaxInboundDecodeBytes is 1 GiB
// and resolveMaxInboundDecodeBytes applies it ENGINE-WIDE — one budget shared
// across every connection, not one per connection — whenever the option is left
// at zero and the process has no Go soft memory limit to derive a smaller
// fraction from, which is exactly this server's state: nothing here sets
// MaxInboundDecodeBytes and nothing here sets GOMEMLIMIT. Lowering the connection
// ceiling does not lower that 1 GiB, and does not need to.
//
// The engine's own comment on that constant is worth carrying across rather than
// paraphrasing: the branch that now returns it "used to return 0, i.e. no ceiling
// at all". A default is not a bound until something has checked which branch the
// process takes, and this is the record of that check.
//
// # The honest limit, stated rather than glossed
//
// It bounds a COUNT and not a COST. Peak resident memory is that count multiplied
// by what one statement costs, and the second factor has no measured ceiling. A
// statement the budget cuts while it is writing retains every mutation it had
// applied in an undo log, and one such statement drove a short-lived
// `rmp graph execute` process to 3.04 GB of resident memory over a store of
// 1.3 MB (rmp task #380). Lowering 1024 to this value divides the multiplier by
// eight; it does not bound the product, and nothing available in this
// configuration does. The short-lived invocation returned that memory to the
// operating system by exiting. A server has no exit to return it at.
//
// Nor can the ceiling be lowered until it does bound the product. The smallest
// ceiling that costs no throughput is 16 — the knee measured below — and sixteen
// of those cut writes at 3.04 GB each is 48.6 GB, more than the 30 GB of RAM the
// machine these numbers were taken on has. So there is no ceiling that
// both preserves throughput and bounds peak resident memory: the two requirements
// have no common value. This option is therefore set on what it CAN bound, and
// the claim made for it is only that.
//
// # Why it does not cost throughput
//
// It sits well above the concurrency at which read throughput stops rising, and
// rmp task #370 MEASURES where that is rather than assuming it: the ladder is 1,
// 2, 4, 8, 16, 32, 64 clients against a served store of the shape the real
// knowledge graph has, on a point lookup and on a whole-graph scan, on an AMD
// Ryzen 9 5900HX — 8 physical cores, two threads per core, 16 logical CPUs, 30 GB
// of RAM.
//
// Read throughput rises to 16 and then flattens. In process, on the point read:
// 2563 ops/s at one client, 4830 at two, 8191 at four, 13292 at eight, 17533 at
// sixteen, 18105 at thirty-two, 18359 at sixty-four. From 16 to 64 throughput
// gains 4.7% while p99 grows from 2.03 ms to 9.80 ms — nearly fivefold. Past the
// knee another concurrent reader buys no throughput and only lengthens the tail.
//
// The two ladders' knees have ONE physical explanation, and it is worth stating
// because it says where each of them moves if the machine changes. Work dominated by the
// transport and the client process saturates the 16 LOGICAL CPUs; work dominated
// by the engine saturates the 8 PHYSICAL cores — an engine-dominated full scan of
// a 150,001-node store peaked at 8.11x — because SMT buys a CPU-bound scan almost
// nothing: the two hardware threads of a core contend for the same execution
// units, and a scan keeps them busy. Neither knee is anywhere near this ceiling.
//
// 128 sits above 64, the widest rung either ladder measured, and at every rung
// including 64 not one connection was refused and not one statement failed. No
// client Groadmap's own surfaces produce can approach it either: `rmp graph
// execute` and the web graph data endpoint each hold one connection for the
// length of one statement.
//
// # Why the ceiling must never bind
//
// Because the engine applies NO backpressure when the semaphore is full. Serve
// logs `bolt: max connections reached, rejecting` at WARN and calls conn.Close()
// immediately, with no protocol answer at all — no failure message, no queue, no
// wait. The client sees the connection dropped during the handshake,
// internal/graphclient classifies that as unreachable, and isRetriable does not
// retry it: the one failure the policy re-sends is a serialisation conflict.
//
// A ceiling that binds therefore converts a load spike into hard client failures
// rather than into queueing, which is why this one is set well above every
// measured knee rather than near one. A ceiling near the working range would buy
// no throughput — the ladder above says the throughput is already flat there —
// and would pay for it in refusals the client cannot recover from.
const maxConnections = 128

// maxOpenTxPerPrincipal is how many explicit transactions the server admits open
// at once, and it is set EQUAL to the connection ceiling deliberately.
//
// # Why the engine's default is replaced even though it cannot bind
//
// The engine's default is 2048 (server.DefaultMaxOpenTxPerPrincipal), sixteen
// times this connection ceiling and therefore incapable of binding: a connection
// holds at most one open transaction, so the ceiling is reached first and it is
// the ceiling that bounds the resource. What is replaced is not a bound but the
// APPEARANCE of one — a number in the configuration that reads as a limit and is
// not one, which is a fact every later reader would have to rediscover.
//
// # Why equal to the ceiling, and not lower
//
// There is exactly one principal. Auth is server.NoAuthHandler, so every
// connection authenticates as the same principal and this quota isolates nobody
// from anybody. A quota BELOW the connection ceiling would therefore refuse a
// BEGIN from a client the ceiling had already admitted, converting a resource
// bound into an arbitrary refusal on a surface with no second principal to
// isolate from. Equal is the value at which the quota is exactly as tight as the
// resource bound and no tighter.
//
// # What an open transaction costs, and what actually bounds it
//
// From the engine's own documentation: an open transaction pins an MVCC read
// snapshot and holds the reclamation horizon back for its whole lifetime, in read
// mode and in write mode alike. That lifetime is bounded HERE at the statement
// budget, because MaxStatementTimeout clamps an explicit transaction's TOTAL life
// and serverOptions sets it to the graph store's 5 seconds — six times tighter
// than the engine's own 30 s default for a transaction. That clamp, and not this
// quota, is what bounds the resource; the quota bounds only how many may be held
// at once.
//
// # Who it binds
//
// Not Groadmap. internal/graphclient.Send sends autocommit statements and opens
// no explicit transaction, so neither `rmp graph execute` nor the web graph data
// endpoint ever counts against this. It binds a foreign Bolt client connecting to
// the socket, which is the only party that issues a BEGIN here.
const maxOpenTxPerPrincipal = maxConnections

// checkpointCadence is the pair of durations the in-flight checkpointer runs
// under: how stale the snapshot may become before a fold is owed, and how often
// the loop looks.
//
// # Why a parameter and not a package constant
//
// At a five-minute cadence fixed in a constant, NOTHING can drive the in-flight
// checkpointer in a test: the fold is unreachable inside any run a test may take,
// so every assertion about what an in-flight checkpoint does would have to be
// made about a code path that never executes (rmp task #369, FINDING #282). The
// value and the seam are therefore the same question, and this type takes both.
//
// The engine's own seam for this is unusable from here. checkpoint.WithClock
// injects a clock the cadence loop reads, but its parameter type is
// clock.Clock from GoGraph/internal/clock, and an internal package of another
// module cannot be imported. Injecting the DURATIONS is the only seam available,
// and it has the advantage of driving the production loop rather than a virtual
// one.
//
// # Why Interval is set explicitly rather than left to default to MaxAge/4
//
// The loop only CHECKS MaxAge on a tick, so Interval is the real granularity of
// the cadence: a fold is owed at MaxAge and taken at the first tick after it, so
// the effective staleness bound is MaxAge + Interval and not MaxAge. Interval
// also fixes when the FIRST fold happens — the loop's lastFire starts at the zero
// time, so clock.Since(lastFire) already exceeds any MaxAge at the very first
// tick and the first checkpoint fires one INTERVAL into the process, not one
// MaxAge in. Leaving Interval derived makes one of the two numbers a surprise
// rather than a decision, and it is the number that governs both of those
// behaviours.
//
// # What the cadence can and cannot bound
//
// It can only be a TIME. checkpoint.Config carries exactly Dir, MaxAge and
// Interval: there is no size trigger and no operation-count trigger, so the
// write-ahead log's growth between folds is bounded by the write rate alone. A
// burst inside one window grows the log without limit, and the next open replays
// all of it. That is the residual SPEC/GRAPH.md § Durability and Checkpointing in
// a Long-Lived Process, rule 6, leaves open by fixing that a cadence exists
// rather than what it bounds.
//
// A maxAge of zero DISABLES age-based triggering, which is the engine's
// documented meaning for it and not a Groadmap convention. It is what the
// benchmarks use to hold the write-ahead log still while they measure its growth
// per write: a fold that truncated the log mid-measurement would destroy the
// quantity being measured.
//
// # The zero value disables MORE than the trigger, deliberately
//
// checkpointCadence{} leaves interval at zero as well as maxAge, and the two
// zeroes compose into something stronger than "the age trigger never fires".
// checkpoint.New derives Interval = MaxAge/4 only when Interval == 0 AND
// MaxAge > 0, so a disabled cadence reaches the loop with Interval still zero —
// and the loop builds a ticker only when Interval is positive. A disabled cadence
// is therefore a loop with NO CLOCK at all, selecting on its stop and trigger
// channels and nothing else, rather than a loop that ticks and declines to fire.
//
// That is the property a benchmark wants: no periodic wake-up landing inside a
// timed region, not merely no fold. It is also why [build] starts no
// [checkpointWatch] under it — with no ticker there is no in-flight attempt to
// observe, so a poller would sample a level nothing ever writes.
type checkpointCadence struct {
	maxAge   time.Duration
	interval time.Duration
}

// productionCadence is the cadence a real `rmp graph serve` runs under.
//
// SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process requires
// that a cadence EXIST and that it be bounded by something other than the
// process's lifetime, and deliberately fixes no value: the cost of a checkpoint
// is proportional to the live graph — 19.7 ms on a 1.3 MB store, 964 ms on a
// 122 MB one — and its benefit is proportional to how fast the log is growing,
// which is a property of the workload. Neither quantity is knowable from a
// specification.
//
// Five minutes was chosen provisionally, by analogy with the interval PostgreSQL
// has defaulted its own checkpoint timeout to for two decades. rmp task #370
// measured the two costs that analogy stands in for, and the measurement INVERTED
// the expected profile: the cost everyone expects to bind is zero, and the cost
// that actually decides the value is one the analogy never raises. The values
// survive. The reason they survive has nothing to do with the analogy that chose
// them, and the rest of this comment is that reason and not that analogy.
//
// # The writer cost, which everyone expects to be binding, is zero
//
// BenchmarkServedWriteWithCheckpointing runs one write load twice at eight
// concurrent writers, 20,000 writes per run, four interleaved repeats per arm:
// once with no cadence at all, once folding every second. Folding every second
// gives 2730 ops/s against the control arm's 2725 — 0.2% apart, against a 1.5%
// spread WITHIN each arm — and p99 4.78 ms against 5.60 ms, which is inside the
// control arm's own 4.12-5.74 ms spread. The difference is not merely small: it
// is below the noise of the measurement that went looking for it.
//
// The engine's three-phase design is why, and it is the reason the result
// generalises past this hardware. Only phase 1a — quiesce, read the durable
// watermark, open the MVCC read instant — and phase 3 — truncate the log prefix —
// hold the commit lock, and both are O(1) in the graph. Phase 1b's O(V+E)
// serialisation and phase 2's "potentially multi-second" disk write both run with
// the lock RELEASED, and writers commit throughout them. There is therefore no
// p99 argument against any cadence, however short.
//
// # The benefit of a shorter cadence, quantified
//
// The write-ahead log grows 83.97 bytes per single-property write, constant at
// every concurrency measured. Recovery is linear in the log at 0.232 s per MB:
// 0.47 MB in 0.144 s, 1.90 MB in 0.466 s, 4.79 MB in 1.128 s, 9.59 MB in 2.222 s.
// At the maximum rate the server can be driven in process, 6729 writes/s, that is
// 565 KB/s of log, so five minutes is a worst case of about 162 MB of log and
// 38 s of recovery; at the 1265 writes/s concurrent `rmp graph client` processes
// reach, one statement per process, it is 30 MB and 7.0 s. Those are the numbers
// a shorter cadence buys down, and they are the whole of what it buys.
//
// # The cost of a shorter cadence, which is the one that decides it
//
// The engine's checkpointer has NO no-op gate. runNonBlocking captures and
// writeAndTruncate writes unconditionally; nothing anywhere compares this fold's
// watermark against the previous fold's, so a fold with nothing to fold still
// serialises the whole graph and still rewrites the whole snapshot. Measured
// directly rather than inferred: a server on a two-second cadence, with NO client
// connected and not one statement sent, rewrote its 20.2 MB snapshot on every
// tick, the manifest's mtime advancing every two to three seconds.
//
// That is the opposite of the direct path, where graphstore.Store.Checkpoint
// carries exactly that gate — it compares the writer's durable offset against the
// mark the last fold left and returns without writing when nothing was appended —
// so a statement that appended nothing leaves snapshot/ and wal untouched. The
// gate exists in this project; it does not exist in the loop.
//
// Over ONE DAY of an idle server, on the real 1.4 MB knowledge graph: 403 MB
// written at five minutes, 1.0 GB at two minutes, 2.0 GB at one minute. On a
// 36 MB graph — the largest real KNOWLEDGE GRAPH this project has measured, as
// distinct from the synthetic stores the checkpoint-cost figures above come
// from — 10 GB, 26 GB and 52 GB respectively.
//
// # The conclusion, stated as the inversion it is
//
// The two costs point in OPPOSITE directions, and the one expected to decide the
// value decides nothing: the writer cost of folding is zero, so nothing argues for
// a longer cadence from the writers' side, while the idle cost of folding is a
// whole graph rewritten per tick, which argues for a longer one from the disk's.
// This product's server is idle far more often than it is saturated — a knowledge
// graph is queried in bursts and written at a commit — so the idle cost is the one
// that is actually paid, and the longer cadence is the better one.
//
// Five minutes therefore survives, on evidence that has nothing to do with the
// analogy that provisionally chose it. Seventy-five seconds is its quarter, which
// is what the engine would have derived; it is stated rather than derived for the
// reasons [checkpointCadence] gives.
//
// # What would stop this being a trade-off at all
//
// The idle cost is entirely an artefact of the missing gate, and Groadmap could
// supply one: drive the fold itself with the checkpointer's trigger, gated on
// wal.Writer.DurableOffset having advanced since the last one, exactly as
// graphstore.Store.Checkpoint gates the direct path. An idle server would then
// write nothing at any cadence, and the cadence could be chosen on recovery time
// alone — which is the only quantity left once the idle cost is zero. That is
// recorded as an open question for the owner on rmp task #370 and is deliberately
// NOT built here: it moves the cadence decision out of the engine's loop and into
// this package, which is a change of ownership rather than a tuning change.
func productionCadence() checkpointCadence {
	return checkpointCadence{maxAge: 5 * time.Minute, interval: 75 * time.Second}
}

// logger is the server's diagnostic channel. It writes to stderr, never stdout:
// stdout carries the single startup object naming the bound socket, so a caller
// that reads stdout for the path is never disturbed by a log record
// (SPEC/COMMANDS.md § Serve Output).
//
// It is handed to the engine as well as used here, so the two warnings the engine
// emits at construction — one for a server running without transport security and
// one for a server running without authentication — arrive on the same stream in
// the same shape. Both are expected, both are accurate, and neither is a failure
// (SPEC/GRAPH.md § Socket Path and Permissions, rule 6).
var logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

// Options is everything Run needs, resolved by the caller.
//
// Resolving the roadmap and the socket path is the CLI's half of the work and is
// done before anything is opened, created, or removed (SPEC/GRAPH.md § Server
// Startup, step 1), which is why this package receives the results rather than
// the roadmap name alone.
type Options struct {
	// Announce reports the bound socket to whatever surface started the server,
	// after the store is open and before the first connection is served. It is a
	// callback rather than a print because the OUTPUT is the CLI's: this package
	// serialises nothing (SPEC/ARCHITECTURE.md module 10).
	Announce func(socket string) error

	// RoadmapName is the roadmap whose graph is served. It appears in the lock
	// failure's published line, which names the roadmap rather than the
	// directory.
	RoadmapName string

	// GraphDir is that roadmap's graph store directory. It MUST already exist:
	// `rmp graph serve` creates no graph directory (SPEC/COMMANDS.md § Serve).
	GraphDir string

	// SocketPath is the absolute path of the socket to bind: the default derived
	// from the roadmap, or the value of --socket.
	SocketPath string
}

// Run performs the whole of `rmp graph serve`: the startup sequence, the serve,
// and the ordered teardown. It returns nil when the server was stopped by SIGINT
// or SIGTERM, and a classified error otherwise.
//
// The order below is SPEC/GRAPH.md § Server Startup's, and it is load-bearing at
// every step: each one is what makes a later one safe.
//
//   - The lock is taken BEFORE the socket is probed, so a second server against
//     the same roadmap is refused by the lock without the incumbent's socket
//     being touched. The probe catches the case the lock cannot: a --socket path
//     some OTHER roadmap's server owns. The two interlocks are different and
//     neither is relied on to do the other's work.
//   - The listener is bound BEFORE the store is opened. The open costs up to
//     about a second on a large graph, and a caller that resolved the roadmap
//     during that second would find no socket, conclude the roadmap is not
//     served, and take the direct path into a lock this process is already
//     holding. Binding first spends that second with the socket already
//     accepting.
//
// One window remains and is stated rather than hidden: between the lock and the
// bind no socket answers, so a caller that resolves inside it takes the direct
// path, waits the whole wait budget, and fails. The window is a probe, an unlink
// and a bind — microseconds, not the store open — and the failure is loud,
// deterministic, and cleared by retrying (SPEC/GRAPH.md § Lock Contention, the
// first of the three residual cases).
func Run(opts Options) error {
	// Step 2. The exclusive advisory hold, under the ordinary bounded wait. A
	// server starting while a short-lived `rmp graph execute` invocation holds
	// the lock waits for it rather than failing on the first collision.
	hold, err := graphstore.Acquire(opts.GraphDir)
	if err != nil {
		return lockRefusal(opts.RoadmapName, err)
	}

	// Step 3. Refuse to start when a live server already answers there, and leave
	// its socket exactly as it was found.
	if state, _ := graphclient.Resolve(context.Background(), opts.SocketPath); state.Served() {
		hold.Release()
		return fmt.Errorf("%w: a graph server is already serving %s", utils.ErrDatabase, opts.SocketPath)
	}

	// Step 4. Replace a stale socket file: nothing answers there, so a file at
	// the path is a leftover from a process that was killed, and removing it is
	// what lets a relaunch after a kill succeed instead of failing on a name that
	// is already taken.
	removeStaleSocket(opts.SocketPath)

	// Step 5. Bind, and set the mode.
	ln, err := bind(opts.SocketPath)
	if err != nil {
		hold.Release()
		return err
	}

	// Step 6. Open the store and construct the engine, through the one lifecycle
	// internal/graphstore owns. A failure here closes the listener — which
	// unlinks the socket — so a caller waiting on the handshake sees the
	// connection dropped and fails, rather than falling back onto a lock this
	// process was about to hold (SPEC/GRAPH.md § Server Startup).
	st, err := hold.Open()
	if err != nil {
		_ = ln.Close() //nolint:errcheck // the store never opened; the close error cannot be acted on
		return err
	}

	closer, srv, err := build(st, opts.GraphDir, productionCadence())
	if err != nil {
		_ = closer.Close() //nolint:errcheck // tearing down a server that never served; the close error cannot be acted on
		_ = st.Close()     //nolint:errcheck // idem: the lock is released by this call whatever it returns
		_ = ln.Close()     //nolint:errcheck // idem
		return err
	}

	// Step 7. Serve, and report the socket. The announcement precedes the accept
	// loop, so a caller that reads stdout for the path has it before the first
	// session can produce a diagnostic.
	if err := opts.Announce(opts.SocketPath); err != nil {
		_ = closer.Close() //nolint:errcheck // the announcement failed, so nothing was served; the close error cannot be acted on
		_ = st.Close()     //nolint:errcheck // idem
		_ = ln.Close()     //nolint:errcheck // idem
		return err
	}

	return serve(srv, ln, st, opts.SocketPath)
}

// lockRefusal words an exhausted wait for the graph store lock in the terms the
// reader can act on (SPEC/COMMANDS.md § Graph Server Socket Error Lines).
//
// internal/graphlock reports a busy store, which is the right wording for a
// short-lived invocation contending with another. It is the wrong wording here: a
// server holds the lock for its whole process lifetime, so a second
// `rmp graph serve` against the same roadmap is the overwhelmingly likely cause
// and the line says so. It says "may" because the lock records no holder — the
// invocation reports the likely cause and does not assert it.
//
// Only the exhausted wait is reworded. Every other way the acquisition can fail —
// a lock file that cannot be opened at all — arrives already classified and is
// returned untouched, because a second server is not a plausible cause of it.
func lockRefusal(roadmapName string, err error) error {
	if !errors.Is(err, utils.ErrDatabase) {
		return err
	}
	return fmt.Errorf("%w: cannot take the graph store lock for roadmap %q: "+
		"another rmp graph serve may already be running for it", utils.ErrDatabase, roadmapName)
}

// removeStaleSocket removes the socket file at path when one is there and nothing
// is listening behind it.
//
// It removes a SOCKET and nothing else. `--socket` is a caller-supplied path, and
// a step that removed "any file at the path" would delete whatever the caller
// mistyped. A path occupied by something that is not a socket is left exactly as
// it was, and the bind that follows fails with the published bind line instead —
// which is the outcome a reader can act on, and the one that destroys nothing.
//
// A removal that fails is not reported here either: what matters is whether the
// bind succeeds, and the bind's own failure is the published line for it.
func removeStaleSocket(path string) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return
	}
	_ = os.Remove(path) //nolint:errcheck // a removal that fails surfaces as the bind failure below, with its own published line
}

// bind creates the listener and sets the socket's mode.
//
// The mode is set explicitly rather than left to the process umask: connecting to
// a Unix domain socket requires write permission on the file, so a permissive
// umask leaves the socket connectable by the user's group or by every account on
// the machine (SPEC/GRAPH.md § Socket Path and Permissions, rule 3). It is set
// immediately after the bind and therefore before the server answers its first
// connection, which is what the rule requires; the roadmap home directory's 0700
// is the outer fence that covers the instant between the two on the default path.
//
// A chmod failure closes the listener and reports the bind line, because a socket
// whose mode could not be set is a socket this server must not answer on.
func bind(path string) (*serverListener, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot bind %s: %v", utils.ErrDatabase, path, err)
	}
	if err := os.Chmod(path, graphclient.SocketMode); err != nil {
		_ = ln.Close() //nolint:errcheck // already failing; the close unlinks the socket and its own error cannot be acted on
		return nil, fmt.Errorf("%w: cannot bind %s: %v", utils.ErrDatabase, path, err)
	}
	return newServerListener(ln), nil
}

// build assembles the durability stack and the Bolt server over an open store.
//
// The checkpointer is the in-flight half of SPEC/GRAPH.md § Durability and
// Checkpointing in a Long-Lived Process: a server has later opportunities than a
// short-lived invocation, so it does not checkpoint per write, but the
// write-ahead log would otherwise grow for the whole process lifetime and the
// cost of recovering from a kill would grow with it.
//
// Three of the options are what make the snapshot it writes correct, and none is
// optional:
//
//   - WithCommitSerialiser hands it the store's commit lock. The engine's own
//     commit mutex is private, so the mutex a checkpointer is constructed with can
//     never be it; without the serialiser the capture is not a transaction
//     boundary and the snapshot can carry a transaction's two new nodes without
//     its edge.
//   - WithConstraintSpecs and WithIndexSpecs make the snapshot carry the
//     registered schema. Without them the checkpointer writes a schemaless
//     snapshot and truncates the log after it, which destroys every CREATE INDEX
//     and CREATE CONSTRAINT the graph has seen — the defect of release 1.15.2,
//     arriving through the engine instead of through our own snapshot call.
//
// A [shutdownCloser] over the composed store.DB is handed to the server as its
// Closer, which is what puts the shutdown checkpoint and the write-ahead log's
// close AFTER the drain rather than beside it: the server closes it only once no
// connection can still be writing. WithQuiesce runs that close inside the same
// commit lock, so an in-flight commit finishes before the writer is flushed and
// closed.
//
// WithFinalCheckpoint is deliberately NOT set, and [shutdownCloser] takes that
// checkpoint instead. The two run at the same instant and in the same order; what
// differs is that the composed store discards the checkpoint's error into a
// metric this project does not read, and shutdownCloser reports it. See there.
//
// The cadence is a PARAMETER rather than a constant read from here, for the
// reason [checkpointCadence] gives: at a production cadence nothing can drive the
// in-flight checkpointer within a test's lifetime, so the seam and the value are
// the same question. Production passes [productionCadence].
//
// A [checkpointWatch] is started over the checkpointer's statistics whenever the
// cadence is enabled, which is what makes an in-flight checkpoint failure
// observable at all (rmp task #369, DECISION #281). It is NOT started when the
// cadence is disabled: with no age trigger there is no in-flight attempt to
// observe, and a poller over a checkpointer that never fires would report on the
// shutdown checkpoint alone — which [shutdownCloser] already reports itself, in
// the words that fit it.
func build(st *graphstore.Store, graphDir string, cadence checkpointCadence) (*shutdownCloser, *server.Server, error) {
	txnStore := st.Txn()
	engine := st.Engine()

	// The mutex is unused: WithCommitSerialiser supersedes it, and the engine's
	// own commit lock is what the checkpointer must actually hold. The engine
	// requires the parameter all the same, so a throwaway is what is passed.
	var unused sync.Mutex

	cp := checkpoint.New[string, float64](
		checkpoint.Config{Dir: graphDir, MaxAge: cadence.maxAge, Interval: cadence.interval},
		st.Graph(), st.WAL(), &unused,
		checkpoint.WithCommitSerialiser[string, float64](txnStore.RunUnderCommitLock),
		checkpoint.WithMapperCodec[string, float64](txnStore.Codec()),
		checkpoint.WithWeightCodec[string, float64](txnStore.WeightCodec()),
		checkpoint.WithConstraintSpecs[string, float64](engine.ConstraintSpecsForSnapshot),
		checkpoint.WithIndexSpecs[string, float64](engine.IndexSpecsForSnapshot),
	)
	// The loop's own lifetime is bounded by store.DB.Close, which stops it, and
	// not by this context: a context cancelled from here would stop the loop
	// without the final checkpoint the shutdown owes.
	cp.Start(context.Background())

	db := store.New(st.WAL(),
		store.WithCheckpointer(cp),
		store.WithQuiesce(txnStore.RunUnderCommitLock))

	closer := &shutdownCloser{db: db, cp: cp}

	// The poll period is DERIVED from the cadence rather than picked; see
	// [checkpointWatch] for the sampling argument that fixes it at half the
	// checkpointer's own interval.
	if cadence.maxAge > 0 {
		closer.watch = newCheckpointWatch(cp.Stats, watchPeriod(cadence), logger)
		closer.watch.start()
	}

	srv, err := server.NewServer(engine, serverOptions(closer))
	if err != nil {
		return closer, nil, fmt.Errorf("%w: graph server unavailable: %v", utils.ErrDatabase, err)
	}
	return closer, srv, nil
}

// shutdownCloser is what the engine's server closes once its connections have
// drained, and it is steps 4 and 5 of SPEC/GRAPH.md § Server Shutdown and the
// Drain: checkpoint and truncate the write-ahead log, then close the store's
// durability stack.
//
// # Why it exists rather than the composed store alone
//
// The composed store takes that checkpoint itself when it is constructed with a
// final-checkpoint option, in the same place and in the same order. What it does
// NOT do is report the outcome: its own documentation makes the final checkpoint
// best-effort and its error is deliberately dropped into a metric, on the ground
// that durability does not depend on it. That ground is correct — every
// acknowledged commit is already durable in the write-ahead log, which is why
// step 7 still exits 0 — and the conclusion drawn from it is not ours to draw:
// SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rule 7,
// and § Synchronous Checkpoint on Write's failure policy both require a
// checkpoint failure to reach the reader as a diagnostic without changing the
// exit code. A server that silently skipped its shutdown checkpoint left a
// write-ahead log the next open replays in full and a snapshot older than the
// graph, and nothing anywhere said so.
//
// So the checkpoint is requested here, where the error is in hand, and the
// composed store is then closed for the two steps whose ordering it exists to
// own: stop the checkpoint loop, then close the write-ahead log under the commit
// lock. Requesting it before that close is not a choice either — a stopped
// checkpointer refuses the request — and it is the same order the composed store
// performs internally.
//
// # Why no error is exempted
//
// Every way this checkpoint can fail is worth a line. A refusal from a stopped
// checkpointer cannot happen from here — this is the only caller, it runs once,
// and it runs before the loop is stopped — so treating one as benign would
// silence the one message that would tell us the assumption had broken. The
// remaining failures are a write-ahead log the engine has poisoned, a capture
// that could not reach a transaction boundary, and a snapshot the filesystem
// refused; a reader can act on each of them and on none of them silently.
//
// Close is idempotent and returns the same result to every caller, which is what
// the engine requires of it: the server closes it from whichever of its two
// drained exit paths reaches the once-guard first, and Run closes it directly on
// the startup paths where nothing was ever served.
type shutdownCloser struct {
	db *store.DB
	cp *checkpoint.Checkpointer[string, float64]
	// watch is the in-flight checkpoint poller, or nil when the cadence is
	// disabled and there is nothing in flight to observe. Close stops it before
	// anything else; see there for why that ordering is load-bearing.
	watch *checkpointWatch
	err   error
	once  sync.Once
}

// Close performs the shutdown checkpoint and then closes the durability stack.
//
// The returned error is the composed store's — in practice the write-ahead log's
// close — and never the checkpoint's, which is reported instead. That is the
// division SPEC/GRAPH.md fixes: a failed close of the log is a teardown failure,
// and a failed checkpoint after commits that are already durable is a diagnostic.
//
// # Why the watch is stopped FIRST
//
// The ordering is load-bearing and not tidiness. The shutdown checkpoint below
// runs through the SAME checkpoint.runCheckpoint the in-flight cadence runs, and
// that call records its own outcome in Stats().LastError — a LEVEL, overwritten
// by every completed attempt. A watch still running would therefore sample the
// SHUTDOWN checkpoint's failure and report it a second time, worded as though an
// in-flight fold had failed, while this function reports the same failure once
// more in the words that actually fit it.
//
// The same fact is why reading LastError once at shutdown is not a substitute for
// the poll: by the time the shutdown checkpoint has run, the value describes the
// shutdown checkpoint and no longer describes any in-flight attempt. An in-flight
// failure that a later in-flight success cleared is unobservable after the fact by
// construction, which is what makes the poller the only place it can be seen.
func (c *shutdownCloser) Close() error {
	c.once.Do(func() {
		// Before anything else, and before the checkpoint below overwrites the
		// level the watch samples. stop joins, so no goroutine of this package's
		// outlives the server.
		if c.watch != nil {
			c.watch.stop()
		}

		// Step 4. Unbounded on purpose: SPEC/GRAPH.md § Server Shutdown and the
		// Drain does not bound the shutdown, and a deadline here would abandon a
		// capture that is quiescing behind an undo replay rather than shorten it.
		if err := c.cp.TriggerCtx(context.Background()); err != nil {
			logger.Error("the graph server's shutdown checkpoint failed; every acknowledged "+
				"commit is still durable in the write-ahead log and the next open recovers it, "+
				"but the log was not folded into the snapshot and the next open replays it in full",
				slog.String("err", err.Error()))
		}
		// Step 5, the store's half: stop the checkpoint loop, then close the
		// write-ahead log inside the commit lock. Releasing the exclusive
		// advisory hold is the other half and stays with the caller that took it.
		c.err = c.db.Close()
	})
	return c.err
}

// serverOptions is every option Groadmap fixes on the engine's Bolt server, and
// nothing else: an option the engine owns is left at the engine's own default
// rather than restated, because restating one would give this project a fact a
// dependency bump can falsify in silence (SPEC/GRAPH.md § Server Options).
//
// What is fixed, and why each:
//
//   - Auth is set explicitly to the handler that admits everyone. The engine
//     refuses to construct a server with no handler at all, so "no
//     authentication" here is a declaration and never an oversight. A caller that
//     can open the socket can read, write, delete and change the schema of that
//     roadmap's graph; the filesystem is the access control and it is the whole of
//     the access control there is.
//   - Closer is the composed durability stack, torn down after the drain.
//   - The statement bound is the graph store's, read from the one declaration the
//     other two surfaces already read, so a change to it changes all three
//     together. The MAXIMUM carries the same value, so a client cannot raise its
//     own statement timeout above the bound `rmp graph execute` and the web graph
//     data endpoint obey. The consequence is stated rather than left to be
//     discovered: the engine clamps an explicit transaction's total life by that
//     same maximum, so a BEGIN-to-COMMIT sequence has the same 5 seconds in total
//     that a single statement has, however many statements it carries.
//   - ConnTimeout is the one option whose default is actively wrong here; see
//     connTimeoutMultiple. It also bounds a session that sends nothing, so a
//     client that holds a session open without using it loses it after the same
//     60 seconds and must reconnect.
//   - MaxConnections is the capacity decision, and it is the only one: every
//     other count the engine caps is reached through a connection first. See
//     maxConnections, which states both what it bounds and what it cannot.
//   - MaxOpenTxPerPrincipal is fixed EQUAL to that ceiling. It cannot bind — a
//     connection holds at most one open transaction — and it is set anyway, so
//     that the configuration carries no number that reads as a bound and is not
//     one. See maxOpenTxPerPrincipal.
//
// What is deliberately NOT fixed:
//
//   - The inbound message and decode bounds. The requirement on them is a floor,
//     not a value: a statement `rmp graph execute` accepts is a statement this
//     server must accept, so they must leave room for a query of the maximum
//     length together with the protocol framing around it. The engine's defaults
//     already do, and lowering either would be a narrowing of the statement
//     surface rather than a tuning change.
//   - The database name. A server serves one roadmap's graph and exposes exactly
//     one database, under the engine's own default name.
func serverOptions(closer io.Closer) server.Options {
	budget := graphlock.StatementBudget
	return server.Options{
		Auth:                    server.NoAuthHandler{},
		Closer:                  closer,
		Logger:                  logger,
		DefaultStatementTimeout: budget,
		MaxStatementTimeout:     budget,
		ConnTimeout:             connTimeoutMultiple * budget,
		MaxConnections:          maxConnections,
		MaxOpenTxPerPrincipal:   maxOpenTxPerPrincipal,
	}
}

// serve runs the accept loop until a signal arrives, then performs the shutdown
// sequence of SPEC/GRAPH.md § Server Shutdown and the Drain.
//
// The signals are taken over from the process-wide handler cmd/rmp/main.go
// installs, which maps SIGINT and SIGTERM to os.Exit(130). For a long-lived
// command that would skip the drain, the checkpoint and the lock release, and
// report the wrong exit code: this command interprets a signal as an instruction
// to stop rather than as an interruption of unfinished work, so it drains,
// checkpoints and exits 0. `rmp web` takes the same signals over for the same
// reason.
func serve(srv *server.Server, ln *serverListener, st *graphstore.Store, socketPath string) error {
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// The context is never cancelled. Cancelling it is one of the two ways to
	// stop the engine's server, and both of them CUT: every connection's context
	// derives from this one. The drain below is what stops it instead.
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(context.Background(), ln) }()

	var signalled bool
	var runErr error
	select {
	case <-sigCh:
		signalled = true
		runErr = stop(srv, ln, serveErr)
	case runErr = <-serveErr:
		// The accept loop failed on its own, which is not a stop this command
		// asked for. The durability stack is already torn down: the engine closes
		// it on its own exit path, after its connection drain.
	}

	// Steps 4 and 5 completed inside the engine's post-drain close, which is where
	// [shutdownCloser] runs: it took the checkpoint that folds the write-ahead log
	// into the snapshot and truncates it, reported that checkpoint if it failed,
	// stopped the checkpoint loop, and closed the write-ahead log. What is left is
	// this package's half of step 5 — releasing the exclusive advisory hold —
	// which Close performs whatever the write-ahead log reports.
	// wal.ErrWriterClosed is that report's expected value here and is not a
	// failure: the log was closed by the composed store a moment ago,
	// deliberately, and this call is what releases the lock behind it.
	if err := st.Close(); err != nil && !errors.Is(err, wal.ErrWriterClosed) {
		logger.Error("graph store close failed", slog.String("err", err.Error()))
	}

	// Step 6.
	removeSocketIfStale(socketPath)

	if !signalled {
		if runErr == nil {
			// Serve returned without an error and without a signal. Nothing is
			// listening any more, so reporting success would tell a caller the
			// server had been stopped on purpose.
			return fmt.Errorf("%w: the graph server stopped accepting connections", utils.ErrDatabase)
		}
		return fmt.Errorf("%w: graph server failed: %v", utils.ErrDatabase, runErr)
	}

	// A teardown failure after a signal is a diagnostic and not an exit code. The
	// exit-code table of SPEC/COMMANDS.md § Serve Exit Codes admits no code for
	// one, and it is right not to: every acknowledged commit was made durable
	// before it was acknowledged, so a failure of the shutdown's own bookkeeping
	// costs no committed data.
	if runErr != nil {
		logger.Error("graph server teardown reported a failure", slog.String("err", runErr.Error()))
	}
	return nil
}

// stop performs steps 1 to 3 of the shutdown sequence and waits for the engine's
// server to return.
//
//  1. Stop accepting new connections. Closing the listener is what does it, and
//     it also unlinks the socket file, so a caller that resolves the roadmap from
//     this moment on reads no socket and takes the direct path — which is the
//     second of the three residual cases SPEC/GRAPH.md § Lock Contention records.
//     The in-flight sessions are untouched: the wrapper keeps the engine inside
//     its accept call, so the engine has not yet reached the cancellation its own
//     exit path performs.
//  2. Wait, bounded, for what is in flight to reach a quiescent point.
//  3. Shut the engine's server down. Whatever is still in flight at that moment
//     is cut, which is what the engine's shutdown does.
//
// Shutdown is given a context with NO deadline of its own, deliberately. A
// statement the budget cut while it was writing is inside an undo replay the
// engine takes no cancellation for, and the store cannot close until that call
// has returned, so the shutdown lasts as long as that replay lasts whatever any
// bound says — 34.5 seconds is the longest measured, with no ceiling established.
// A deadline here would not shorten it; it would only abandon the connections and
// leave the composed store to be closed by the engine's other exit path, which is
// the same wait reached less directly.
func stop(srv *server.Server, ln *serverListener, serveErr <-chan error) error {
	ln.stopAccepting()
	drain(srv, ln)

	shutdownErr := srv.Shutdown(context.Background())

	// Release a blocked Accept unconditionally. Shutdown closes the listener on
	// the way past, which releases it — but only if it found one to close, and it
	// finds none when the signal arrives before the goroutine above has entered
	// Serve. In that window Shutdown returns having cancelled nothing, Serve then
	// starts against a listener this shutdown has already stopped, and its Accept
	// would block for ever on a release nobody performs. Closing here is idempotent
	// and costs nothing on the ordinary path.
	_ = ln.Close() //nolint:errcheck // the wrapper's Close reports no error by construction

	serveReturn := <-serveErr

	// The listener was closed on purpose, so the accept failure that ended the
	// loop is this shutdown's own doing rather than a fault.
	if errors.Is(serveReturn, net.ErrClosed) {
		serveReturn = nil
	}
	return errors.Join(shutdownErr, serveReturn)
}

// drain waits for the work in flight to finish, bounded by the graph store's wait
// budget.
//
// The bound is REUSED rather than replaced by a figure of its own, so the project
// keeps one set of timing numbers. It is the right quantity because it is the one
// a waiter is already required to survive: the longest lawful hold of a statement
// that is a read or that runs to completion.
//
// # What quiescence is here, and what it is not
//
// It is two conditions together: no connection is live, and no explicit
// transaction is open. A connection that has closed has been answered — the
// engine writes a response before it reads the next message, and a client that
// has gone has nothing outstanding — and a transaction the engine still lists is
// in flight whatever its connection is doing.
//
// It is NOT "every connection is idle between messages", although that is the
// signal this drain wants and the one it was first written to use. MEASURED: the
// engine gives every connection a dedicated reader goroutine that reads the next
// message WHILE the message loop executes the previous one, so a connection is
// blocked in a read almost all of the time — including throughout a statement's
// execution. A drain keyed on "blocked in a read" is therefore vacuously
// satisfied, and it cut a statement that had been running for 1.5 seconds inside
// a shutdown that took 20 milliseconds. The same read-ahead is why the engine's
// connection timeout doubles as a statement bound (see connTimeoutMultiple):
// one goroutine's read deadline is armed across another's execution.
//
// The finer signal that WOULD distinguish the two — matching each message read
// against the terminal response written for it — is available only by telling a
// streamed record apart from a final summary, which means decoding the protocol
// at the transport. Groadmap defines no protocol and reads none: the whole of the
// Bolt layer is the engine's (SPEC/GRAPH.md § The Dedicated Graph Server), and a
// second, partial decoder here would be a second answer to what a message means.
//
// The cost of the coarser signal is stated rather than hidden: a client that
// holds a session open without using it keeps the drain running for its whole
// bound, because nothing outside the engine can tell that session apart from one
// whose statement is still executing. The cost is bounded, it is paid only at
// shutdown, and it is paid in the safe direction — waiting for a session that
// owed nothing, rather than cutting one that did.
//
// # What the drain does not guarantee
//
// It does not guarantee completion. The bound is finite, and past it the
// remaining sessions are cut by the shutdown that follows. A cut session's client
// sees a broken connection rather than a typed failure and cannot distinguish
// that from a crash; it does not have to, because the store is consistent either
// way. What it does buy over the engine's own shutdown is the one thing worth
// buying: a statement that completes during the drain is ANSWERED before the
// server stops, where without it a client that had just committed would be told
// nothing about a change that is already on disk.
func drain(srv *server.Server, ln *serverListener) {
	drainUntil(func() bool { return quiescent(ln.live.Load(), len(srv.Transactions())) })
}

// quiescent is the DECISION the drain waits for, separated from the waiting so
// that it can be read, and tested, on its own.
//
// It is a conjunction and each half is load-bearing. A connection that has closed
// has been answered — the engine writes a response before it reads the next
// message, and a client that has gone has nothing outstanding — while a
// transaction the engine still lists is in flight whatever its connection is
// doing. Neither implies the other.
//
// What it is NOT is the signal the drain was first written around, and the
// difference is the whole of FINDING #264 on rmp task #367: "every connection is
// idle between messages" is vacuously satisfied here, because the engine gives
// every connection a dedicated reader goroutine that reads the next message while
// the message loop executes the previous one. A live connection is therefore not
// quiescent even when it is blocked in a read, and that is exactly what this
// function says.
func quiescent(live int64, openTransactions int) bool {
	return live == 0 && openTransactions == 0
}

// drainUntil waits for quiet to hold, bounded by the graph store's wait budget.
//
// The waiting is internal/backoff's, and the budget is internal/graphlock's,
// exactly as they are when an invocation waits for the store lock. Neither the
// loop nor the ladder is written out here: this is a bounded wait on a condition,
// which is the shape that package owns, and a second one of those is defect #294
// with a different subject.
func drainUntil(quiet func() bool) {
	_, _ = backoff.RetryWithin(graphlock.WaitBudget(), func() (struct{}, error) {
		if quiet() {
			return struct{}{}, nil
		}
		return struct{}{}, errStillInFlight
	}, backoff.Always)
}

// errStillInFlight is the drain's "not yet" — the value that keeps the bounded
// wait climbing. It never reaches a caller: the drain's outcome is the shutdown
// that follows it, and an exhausted wait is a cut rather than a failure.
var errStillInFlight = errors.New("graph server: work still in flight")

// removeSocketIfStale is step 6 of the shutdown sequence: ensure the socket file
// is gone.
//
// Closing the listener already unlinked it, so in the ordinary case there is
// nothing here to do and this call is a single failed stat. What it must not do
// is remove a socket that is no longer this server's: the advisory lock was
// released one step earlier, so a new server may already have taken it and bound
// the same path, and an unconditional remove would unlink the incumbent's socket
// and leave a running server nobody can reach. The path is therefore probed
// first, and removed only when nothing answers on it — which is the same
// observation, made through the same resolver, that step 4 of the startup
// sequence makes about a socket a killed server left behind.
func removeSocketIfStale(path string) {
	state, _ := graphclient.Resolve(context.Background(), path)
	if state == graphclient.StateNoSocket || state.Served() {
		return
	}
	removeStaleSocket(path)
}

// serverListener is the listener Groadmap hands the engine's server, and it
// exists for one reason: the engine fuses "stop accepting" with "cut the
// sessions", and the drain needs them apart.
//
// The engine's accept loop exits when Accept returns an error, and its deferred
// exit cancels the accept context — from which every connection's context derives
// — before it waits for those connections. Closing the underlying listener
// therefore stops new connections AND cuts the live ones, in that order, with
// nothing in between for a drain to happen in.
//
// This wrapper puts the drain in between. stopAccepting closes the underlying
// listener, so the socket file is unlinked and no new connection is accepted, but
// Accept then BLOCKS instead of returning the error, which leaves the engine
// inside its accept call and its cancellation unreached. The live sessions run
// on. Accept is released when the listener is closed — by Shutdown, or by the
// engine's own close goroutine — at which point the engine proceeds exactly as it
// always would.
//
// It also counts what the drain has to observe. Every accepted connection is
// wrapped so that the listener knows how many are still live, which is the one
// thing about a session that is observable from outside the engine.
type serverListener struct {
	net.Listener

	// released is closed when Accept may finally return. Blocking on it is what
	// keeps the engine inside its accept call for the length of the drain.
	released chan struct{}
	// stopped is closed by stopAccepting, so Accept can tell an error it caused
	// from an error it did not.
	stopped chan struct{}

	// live counts connections accepted and not yet closed. It is what the drain
	// waits on; see drain for why nothing finer is observable from out here.
	live atomic.Int64

	stopOnce    sync.Once
	releaseOnce sync.Once
}

// newServerListener wraps ln.
func newServerListener(ln net.Listener) *serverListener {
	return &serverListener{
		Listener: ln,
		released: make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

// Accept returns the next connection, wrapped so the drain can observe it.
//
// Once stopAccepting has run, the underlying Accept fails immediately and this
// blocks until the listener is closed, holding the engine inside its accept loop
// so that its cancellation — which would cut every live session — is not reached
// while the drain is running.
func (l *serverListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		select {
		case <-l.stopped:
			<-l.released
		default:
		}
		return nil, err
	}
	l.live.Add(1)
	return &drainConn{Conn: conn, owner: l}, nil
}

// Close stops accepting and releases a blocked Accept.
//
// It is idempotent and reports no error, which is what the engine requires of it:
// Serve closes this listener from its own close goroutine and Shutdown closes it
// again, and Groadmap has closed it once already by the time either runs.
func (l *serverListener) Close() error {
	l.stopAccepting()
	l.releaseOnce.Do(func() { close(l.released) })
	return nil
}

// stopAccepting closes the underlying listener — unlinking the socket file and
// refusing every new connection — without releasing a blocked Accept.
func (l *serverListener) stopAccepting() {
	l.stopOnce.Do(func() {
		close(l.stopped)
		_ = l.Listener.Close() //nolint:errcheck // the socket is going away either way; a close error cannot be acted on
	})
}

// drainConn is one accepted connection, counted by its listener so the drain
// knows when the last of them has gone.
//
// It counts and does nothing else. An earlier version instrumented Read as well,
// on the premise that a connection inside one is a connection the server has
// finished answering; the engine's per-connection read-ahead makes that premise
// false, and drain records the measurement that established it.
type drainConn struct {
	net.Conn
	owner     *serverListener
	closeOnce sync.Once
}

// Close drops this connection from its listener's count, once however many times
// it is called.
func (c *drainConn) Close() error {
	err := c.Conn.Close()
	c.closeOnce.Do(func() { c.owner.live.Add(-1) })
	return err
}
