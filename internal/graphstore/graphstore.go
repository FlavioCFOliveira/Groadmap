// Package graphstore owns the LIFECYCLE of a roadmap's GoGraph store — opening
// it, holding it, checkpointing it, and closing it — and nothing else.
//
// # Why the package exists
//
// The open sequence is not obvious and it is not short. A caller has to take the
// store's advisory lock before anything reads the directory, open the store
// through recovery with the right codec pair, open a write-ahead-log writer under
// the project's retry policy, wrap the recovered graph and that writer in a
// transactional store with the SAME codec pair stated a second time, and then
// construct the engine from the store and the whole recovery result — not from
// the store alone, and not from a recovery result belonging to a different open.
// Getting any one of those wrong is silent: the wrong engine constructor rebuilds
// every index by a full scan, a shared lock held while a statement commits is the
// lost-write corruption graphlock.AcquireExclusive documents, and a store built
// over no write-ahead log commits nothing while reporting success.
//
// It was written out twice. internal/commands held the first copy for the CLI and
// internal/web gained a second when the graph data endpoint moved onto the
// transactional path (rmp task #364), and the two could not share it: internal/
// commands imports internal/web, so the dependency cannot run the other way.
// Sharing it therefore meant a new package, which is an architecture decision;
// rmp task #375 is where it was taken, and its DECISION comment carries the
// options that were declined.
//
// A third caller is what settled it. The dedicated Bolt server (rmp task #367)
// opens the store once and holds it for its process lifetime, so deferring would
// have written the sequence a third time into the feature the sprint exists for.
//
// # Why the boundary is here and not wider
//
// The task that created this package described "the store open, statement, commit
// and checkpoint sequence". Measured rather than assumed, that is TWO sequences,
// not one, and only one of them is shared by all three callers:
//
//   - The OPEN — lock, recovery, write-ahead log, transactional store, engine —
//     is identical for the CLI, the web endpoint and the server. It is here.
//   - The CHECKPOINT is written the same way by the CLI and the web, and the
//     ARTEFACT it produces is common to all three, because recovery reads that
//     artefact whoever wrote it. It is here.
//   - The STATEMENT and the COMMIT are common to the CLI and the web ALONE, and
//     even between those two they agree on the rule and not on the code: the CLI
//     runs on a background context and prints notifications between the drain and
//     the commit, the web derives a query time budget and classifies three
//     distinct failure points into an HTTP-facing error kind, and the server runs
//     no statement of its own at all — GoGraph's Bolt session layer does, with its
//     own transactions and its own streaming. They are NOT here.
//
// A package that also owned the middle would fit two callers of three and hand
// the third an exported surface half of which it must ignore. That is a boundary
// drawn around today's callers rather than around the shared thing.
//
// # What is exported, and why it is this and not the values
//
// internal/graphlock and internal/backoff each export the thing that DRIFTED
// rather than the values it uses, and the same reasoning picks the surface here.
// The codec pair is the obvious candidate and the wrong one: it is stated twice
// per open, but a shared variable holding it would have caught nothing — that is
// exactly the lesson of task #294, where three copies of the retry policy agreed
// on the constants and diverged in the loop that consumed them.
//
// What can drift in a checkpoint is the ORDER and the GATING. The snapshot must
// be made durable BEFORE the log is truncated, because until it exists the log
// holds the only record of every CREATE INDEX and CREATE CONSTRAINT the graph has
// seen. The schema must be read from the engine that ran the statement, because
// the engine is the only party that knows what is registered after it ran. And a
// transaction that appended nothing must neither snapshot nor truncate. So the
// exported unit is [Store.Checkpoint], a method that DECIDES, holding its own
// mark; a caller is never handed an offset to compare for itself.
//
// The DECISION is exported a second time, as [Store.CheckpointIfAppended], and
// for the same reason rather than as a generalisation offered on spec. The
// dedicated graph server folds through the engine's own checkpointer instead of
// through this package's snapshot writer, because its capture has to hold the
// store's commit lock while sessions are live — and that checkpointer has no gate
// at all. Handing it the gate keeps ONE realisation of the rule under two folds;
// the alternative was a copy of the comparison beside a copy of the mark, which
// is the drift this package exists to prevent.
//
// # Why Open takes the lock and Close releases it
//
// internal/graphlock defines the exclusive hold as spanning "the whole open,
// execution, commit, checkpoint, and write-ahead-log truncation sequence".
// Acquiring in Open and releasing in Close IS that span, for all three
// lifecycles, and it makes structural two things the call sites previously kept
// right by comment alone: the lock is taken before anything reads the directory,
// and the write-ahead-log writer is closed while the hold is still held. The web
// copy carried a comment explaining precisely that defer-registration hazard.
//
// # What Open does not do
//
// It does not create the graph directory. The CLI creates it at 0700 before
// opening; the web is forbidden to create one and answers an absent directory
// with an empty graph (SPEC/WEB.md § Security and Constraints, rule 4). That
// difference sits BEFORE the lock, so it stays with the callers, and Open is
// given a directory that already exists.
//
// It does not execute statements, drain results, or classify failures. See
// "Why the boundary is here and not wider" above.
package graphstore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
	"github.com/FlavioCFOliveira/GoGraph/graph/csr"
	"github.com/FlavioCFOliveira/GoGraph/graph/lpg"
	"github.com/FlavioCFOliveira/GoGraph/store/recovery"
	"github.com/FlavioCFOliveira/GoGraph/store/snapshot"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"
	"github.com/FlavioCFOliveira/GoGraph/store/wal"
	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// snapshotDirName is the directory a checkpoint publishes into, under the graph
// directory. recovery.Open reads the same name, so the two must agree.
const snapshotDirName = "snapshot"

// walFileName is the write-ahead log, under the graph directory.
const walFileName = "wal"

// openOpts carries the recovery.Options value used for every graph store open.
// It is stated once here, and the transactional store below states the same two
// codecs a second time because txn.Options is a different type carrying the same
// pair; both are in this file, so they can be read against each other.
var openOpts = recovery.Options[string, float64]{
	Codec:       txn.NewStringCodec(),
	WeightCodec: txn.NewFloat64WeightCodec(),
}

// RecoveryOptions returns the recovery.Options every open of a Groadmap graph
// store uses: string node keys and float64 edge weights, with the codecs that
// encode them.
//
// It exists so that a caller which must open the store RAW — without an engine,
// without a transactional store, and without this package's advisory hold — uses
// the same codec pair the production open uses instead of restating it. A test
// that asserts what a checkpoint left on disk is the case that needs it: opened
// under a different codec, the same bytes decode to a different graph, and the
// assertion would be about something other than what the product wrote.
//
// It returns a copy, so a caller cannot reach through it and change the options
// this package opens with.
func RecoveryOptions() recovery.Options[string, float64] { return openOpts }

// Store is one open GoGraph store: the exclusive advisory hold on its directory,
// the graph recovery reconstructed, the write-ahead-log writer, the transactional
// store over the two, and the Cypher engine over that.
//
// A Store is owned by whoever opened it. The engine it exposes is safe for
// concurrent use — that is the engine's own contract, and MVCC is the store's
// only concurrency control — but the Store's own bookkeeping is NOT: Checkpoint
// and Close both mutate unsynchronised fields.
//
// That is correct and free for a caller that opens, runs one statement and
// closes, which is what the CLI and the web endpoint each do. A caller that holds
// one Store across concurrent statements — a long-running server — MUST serialise
// its own Checkpoint and Close calls against each other, and must take the
// checkpoint at a transaction boundary rather than mid-commit, for which the
// engine supplies txn.Store.RunUnderCommitLock (reachable through Txn). The
// requirement is stated here rather than met with a mutex the two current callers
// would pay for and never need.
//
// Every Store MUST be closed, and Close is idempotent.
type Store struct {
	// The pointer-bearing fields lead, so the prefix the garbage collector must
	// scan is as short as the struct allows (govet fieldalignment).
	graph   *lpg.Graph[string, float64]
	wal     *wal.Writer
	txn     *txn.Store[string, float64]
	engine  *cypher.Engine
	release func()

	dir string

	// mark is the write-ahead log's durable offset as of the last point from
	// which "has anything been appended since?" is asked: the open, or the most
	// recent successful checkpoint. Checkpoint compares against it and updates
	// it; nothing else reads it.
	mark int64

	closed bool
}

// Open takes the graph directory's exclusive advisory lock and opens the store
// inside it, returning a Store whose engine is ready to run statements.
//
// graphDir MUST already exist: Open neither creates it nor reports on its
// absence beyond whatever recovery.Open says. The lock is held from here until
// Close, which is the span SPEC/GRAPH.md § Concurrency and Recovery requires —
// the recovery an open runs REPAIRS the directory a concurrent writer's
// checkpoint publishes into, so the hold has to start before the open and end
// after the last truncation.
//
// Every failure is returned classified as utils.ErrDatabase, except a lock
// acquisition failure, which arrives already classified by internal/graphlock.
// On any failure Open releases everything it had taken, so a caller that gets an
// error holds nothing and must not call Close.
//
// It is [Acquire] followed immediately by [Hold.Open], and it is the entry point
// of a surface that has nothing to do between the two. A surface that does —
// today only the dedicated graph server, which binds its socket inside the hold
// and before the open — calls the two halves itself.
func Open(graphDir string) (*Store, error) {
	h, err := Acquire(graphDir)
	if err != nil {
		return nil, err
	}
	return h.Open()
}

// Hold is the graph directory's exclusive advisory hold, taken WITHOUT the store
// being opened inside it yet. It exists for one caller and one reason.
//
// SPEC/GRAPH.md § Server Startup fixes the order `rmp graph serve` performs, and
// the order is load-bearing: the lock is taken (step 2), the socket is probed and
// bound (steps 3 to 5), and only then is the store opened (step 6). Binding
// first is deliberate — the open costs up to about a second on a large graph, and
// a caller that resolved the roadmap during that second would find no socket,
// conclude the roadmap is not served, and take the direct path into a lock this
// process is already holding. [Open] cannot express that order, because it takes
// the lock and opens the store in one call.
//
// Splitting the two is therefore the server's requirement and not a
// generalisation offered on spec: everything between the lock and the open still
// happens inside the one hold, and the sequence the open performs is still the
// single one this package owns.
//
// A Hold is spent by [Hold.Open], whether that call succeeds or fails. After a
// success the resulting Store owns the release and [Store.Close] performs it;
// after a failure the hold has already been released. [Hold.Release] releases a
// hold that was never opened, and is a no-op on a spent one, so a caller may
// defer it unconditionally.
//
// A Hold is not safe for concurrent use.
type Hold struct {
	release func()
	dir     string
	spent   bool
}

// Acquire takes the graph directory's exclusive advisory lock under the bounded
// wait internal/graphlock specifies, and returns the hold without opening
// anything inside it.
//
// graphDir MUST already exist: the lock file is created inside it, and this
// package creates no directory (see [Open]).
//
// A caller that has nothing to do between the lock and the open calls [Open]
// instead; see [Hold] for the one caller that has.
func Acquire(graphDir string) (*Hold, error) {
	// The lock first, and before anything reads the directory: opening the store
	// is not a read-only operation on disk, because recovery repairs an
	// interrupted checkpoint before it loads anything (internal/graphlock).
	release, err := graphlock.AcquireExclusive(graphDir)
	if err != nil {
		return nil, err
	}
	return &Hold{release: release, dir: graphDir}, nil
}

// Dir returns the graph directory this hold covers.
func (h *Hold) Dir() string { return h.dir }

// Release drops a hold that was never opened. It is idempotent, and it is a
// no-op once [Hold.Open] has been called: after a successful open the Store owns
// the release, and after a failed one the release has already run.
func (h *Hold) Release() {
	if h.spent {
		return
	}
	h.spent = true
	h.release()
}

// Open opens the store inside this hold and constructs the engine over it,
// returning a Store that owns the hold from here until Close.
//
// It spends the hold either way: on success the Store's Close releases it, and
// on failure Open releases it before returning, so a caller that gets an error
// holds nothing and must not call Close. Every failure is classified as
// utils.ErrDatabase.
func (h *Hold) Open() (*Store, error) {
	if h.spent {
		return nil, fmt.Errorf("%w: graph store hold already spent", utils.ErrDatabase)
	}
	h.spent = true

	graphDir := h.dir
	release := h.release

	res, err := recovery.Open[string, float64](graphDir, openOpts)
	if err != nil {
		release()
		return nil, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, err)
	}

	w, err := openWAL(filepath.Join(graphDir, walFileName))
	if err != nil {
		release()
		return nil, err
	}

	st := txn.NewStoreWithOptions[string, float64](res.Graph, w, txn.Options[string, float64]{
		Codec:       txn.NewStringCodec(),
		WeightCodec: txn.NewFloat64WeightCodec(),
	})

	// The whole recovery result, not extracted fields: this constructor
	// re-registers the recovered constraints and index definitions AND hydrates
	// each index from the snapshot payload the same open returned, instead of
	// rebuilding it by a full scan of the graph. res MUST be the result of the
	// open that produced this store, a few lines above: a result from any other
	// open would describe a different graph, and neither the engine nor the store
	// could detect the substitution (SPEC/GRAPH.md § Engine Constructor by Path).
	engine := cypher.NewEngineWithStoreAndRecovery(st, res)

	return &Store{
		graph:   res.Graph,
		wal:     w,
		txn:     st,
		engine:  engine,
		release: release,
		dir:     graphDir,
		// Taken here, after the engine is constructed, because that is where both
		// former copies took it: everything between the writer opening and this
		// line is construction, and a mark taken earlier would attribute
		// construction's appends — if there were ever any — to the caller's
		// statement.
		mark: w.DurableOffset(),
	}, nil
}

// openWAL opens the write-ahead-log writer at walPath under the project's single
// bounded backoff policy (internal/backoff), which owns the attempt count and the
// delay ladder. This sequence used to keep its own constants and its own loop,
// and the loop disagreed with them (task #294); it now has neither.
//
// Every failure is waited on, because the one this retry exists for is contention
// — another process holding the WAL directory lock — and a WAL that cannot be
// opened for any other reason is not distinguishable here anyway.
func openWAL(walPath string) (*wal.Writer, error) {
	w, err := backoff.Retry(func() (*wal.Writer, error) { return wal.Open(walPath) }, backoff.Always)
	if err != nil {
		return nil, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrDatabase, err)
	}
	return w, nil
}

// Engine returns the Cypher engine the caller runs statements through. It is
// valid until Close.
func (s *Store) Engine() *cypher.Engine { return s.engine }

// Graph returns the in-memory graph recovery reconstructed and the engine commits
// into. The engine exposes no accessor for it, which is why a Store does.
func (s *Store) Graph() *lpg.Graph[string, float64] { return s.graph }

// Txn returns the transactional store. A long-running caller needs it for
// txn.Store.RunUnderCommitLock, which is the quiesce boundary GoGraph's composed
// teardown and its background checkpointer are wired against.
func (s *Store) Txn() *txn.Store[string, float64] { return s.txn }

// WAL returns the write-ahead-log writer. A long-running caller needs it to
// compose a store.DB or a checkpoint.Checkpointer over the same log this Store
// holds open.
func (s *Store) WAL() *wal.Writer { return s.wal }

// Dir returns the graph directory this Store was opened on.
func (s *Store) Dir() string { return s.dir }

// Checkpoint performs the synchronous checkpoint of SPEC/GRAPH.md § Synchronous
// Checkpoint on Write — IF the write-ahead log has grown since this Store was
// opened, or since the last checkpoint it took. It reports whether it ran.
//
// The DECISION is [Store.CheckpointIfAppended]'s and the FOLD is this method's
// own. They are separable because a second caller needs the first without the
// second; see there.
//
// # What the snapshot must carry, and the order
//
// The snapshot is a self-sufficient full snapshot under graphDir/snapshot/: it
// carries the node-key mapping for string keys AND the registered schema, so that
// snapshot plus write-ahead-log tail is enough for recovery to reconstruct both
// the graph and the schema declared over it. The schema is read from the engine
// that ran the statement, at the one moment it is correct, because the engine is
// the only party that knows what is registered after a statement has run and
// SPEC/GRAPH.md forbids Groadmap keeping a record of its own beside it.
//
// The order below is load-bearing. The specifications are read, and the snapshot
// they go into is made durable, BEFORE the log is truncated: until that snapshot
// exists the log holds the only record of every CREATE INDEX and CREATE
// CONSTRAINT the graph has seen, and truncating first would destroy the schema
// outright (SPEC/GRAPH.md § Synchronous Checkpoint on Write, step 2). That defect
// has been shipped once and fixed once, in release 1.15.2.
//
// # Failure
//
// Checkpoint MUST be called only after the write transaction has committed
// durably. A failure here is NOT a failure of the write: the commit is the
// durability boundary, the log is intact, recovery still works, and the next
// write reconciles the snapshot. Callers surface the error as a diagnostic and
// keep their success (SPEC FR7).
func (s *Store) Checkpoint() (bool, error) {
	return s.CheckpointIfAppended(s.writeSnapshotAndTruncate)
}

// CheckpointIfAppended runs fold — the caller's own realisation of a checkpoint —
// if and only if the write-ahead log has grown since this Store was opened or
// since the last checkpoint taken through this Store. It reports whether fold
// ran, and returns fold's error unchanged.
//
// # The gate
//
// A transaction that appended nothing MUST NOT snapshot and MUST NOT truncate: it
// would rewrite a full snapshot of the whole graph for every statement that read,
// and it would shorten the history a later recovery replays (SPEC/GRAPH.md § What
// a Statement That Writes Nothing Changes on Disk, rules 2 and 3). The log's own
// durable offset is the answer to "did anything append", which is the question
// the specification asks — not a guess made from the statement's text, which
// Groadmap does not examine.
//
// The comparison lives in here rather than at the call sites, because it is the
// rule and not the number that has to hold.
//
// # Why the decision is exported apart from the fold it usually guards
//
// Because a caller needs it that way, and because the alternative is a second
// copy of the rule. The dedicated graph server does not write its snapshot
// through this package: it composes the engine's own checkpointer, which holds
// the store's commit lock across a capture the server may take while sessions are
// live, and asks it to fold. That checkpointer has NO gate of its own — it
// serialises the whole graph and rewrites the whole snapshot unconditionally —
// and an ungated fold is not merely wasteful. A statement the budget cut and
// rolled back leaves the key mapper's interned keys and the tombstone set behind
// even though the graph is restored, so the fold PUBLISHES that residue: measured
// on rmp task #380, ONE cut write served by `rmp graph serve` grew an 80 KB store
// holding 600 nodes to 134 MB permanently, and a later `MATCH (n) RETURN count(*)`
// over the same 600 nodes cost 1.48 s and 670 MB instead of 0.01 s and 21.6 MB.
// The direct path was never exposed to it, for exactly one reason: this gate.
//
// So the server passes its own fold and gets this decision, rather than carrying
// a copy of the comparison beside a copy of the mark (SPEC/ARCHITECTURE.md module
// 8: this package is the single realisation of the store lifecycle).
//
// # A fold this Store did not perform
//
// A caller that composes the engine's checkpointer has a SECOND party truncating
// the log: that checkpointer cuts the folded prefix with wal.Writer.TruncatePrefix
// and the durable offset DROPS, without this Store's mark moving. Left
// unaccounted for, the next comparison would read an offset below the mark, take
// it for "nothing appended", and skip a fold that is owed — the log's surviving
// suffix is precisely the part no snapshot covers.
//
// An offset BELOW the mark is therefore read as what it can only be: a fold
// somebody else performed, which folded everything up to it and left the rest
// unfolded. The mark drops to zero to say so, and whatever remains counts as
// appended. The clause cannot fire on the direct path — nothing there truncates
// but this Store — so the CLI and the web endpoint keep byte-for-byte the
// behaviour they had.
//
// # Concurrency
//
// The mark is unsynchronised, like the rest of a Store's own bookkeeping, so a
// caller that holds one Store across concurrent statements must serialise this
// against [Store.Checkpoint] and [Store.Close] exactly as the type documents.
func (s *Store) CheckpointIfAppended(fold func() error) (bool, error) {
	if !s.appendedSinceMark() {
		return false, nil
	}
	if err := fold(); err != nil {
		return false, err
	}
	// Re-read rather than assume: a fold truncates, and this mark is what the
	// next call compares against. wal.Writer.Truncate resets the durable offset
	// to zero and wal.Writer.TruncatePrefix leaves the unfolded suffix, so the
	// writer is the only party that can say which of the two just happened.
	s.mark = s.wal.DurableOffset()
	return true, nil
}

// appendedSinceMark reports whether the write-ahead log holds anything this Store
// has not already accounted for, and repairs the mark when a fold outside this
// Store has moved the log underneath it. See [Store.CheckpointIfAppended] for
// both halves.
func (s *Store) appendedSinceMark() bool {
	off := s.wal.DurableOffset()
	if off < s.mark {
		s.mark = 0
	}
	return off > s.mark
}

// writeSnapshotAndTruncate is [Store.Checkpoint]'s own fold: publish a
// self-sufficient full snapshot, then truncate the log it covers. It is called
// only through the gate, and only after the write transaction has committed
// durably; see [Store.Checkpoint] for the order and the failure policy.
func (s *Store) writeSnapshotAndTruncate() error {
	// A CSR view of the committed in-memory graph, for the snapshot.
	cs := csr.BuildFromAdjList(s.graph.AdjList())

	// The registered schema, read from the engine while the write-ahead log is
	// still intact. Either slice may be empty — the common case, a graph with no
	// schema declared over it — and the writer then omits the corresponding
	// snapshot component.
	constraints := s.engine.ConstraintSpecsForSnapshot()
	indexDefs := s.engine.IndexSpecsForSnapshot()

	snapDir := filepath.Join(s.dir, snapshotDirName)
	// WriteSnapshotFullWithMapperCodecConstraintsAndIndexDefs assembles in
	// snapDir+".tmp" and renames atomically into snapDir; the codec emits
	// mapper.bin so the snapshot is self-sufficient for string keys, and the two
	// specification slices are what make it self-sufficient for the schema. The
	// plain WriteSnapshotFullWithMapperCodec persists no schema at all, and the
	// truncation below then leaves nothing to recover it from.
	if err := snapshot.WriteSnapshotFullWithMapperCodecConstraintsAndIndexDefs(
		snapDir, cs, s.graph, txn.NewStringCodec(), constraints, indexDefs); err != nil {
		return fmt.Errorf("snapshot write: %w", err)
	}

	// Flush the log, then truncate it to bound its growth. Truncation happens
	// only after the snapshot is durable, so no committed data is lost.
	if err := s.wal.Sync(); err != nil {
		return fmt.Errorf("wal sync: %w", err)
	}
	if _, err := s.wal.Truncate(); err != nil {
		return fmt.Errorf("wal truncate: %w", err)
	}

	// Keep the snapshot directory consistent with the 0700 the roadmap tree
	// carries. Best-effort: a failure here does not invalidate the durable
	// snapshot.
	//
	// #nosec G302 G703 -- 0700 on a DIRECTORY is mandated by SPEC (CLAUDE.md §10: 0700 for the ~/.roadmaps tree), and gosec G302 false-positives on directory permissions; snapDir derives from the graph directory the caller resolved through utils.GetRoadmapDir, so no traversal is reachable
	_ = os.Chmod(snapDir, 0700)

	return nil
}

// Close releases everything Open took, in the one order that is safe: the
// write-ahead-log writer is closed FIRST and the advisory lock released after it,
// so the log is never closed outside the hold that covers this store.
//
// It is idempotent, so a deferred Close beside an explicit one is harmless. The
// write-ahead log's close error is returned; the lock is released whether or not
// that close succeeded, because a held lock outliving a failed close would block
// every later invocation against this roadmap.
func (s *Store) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.wal.Close()
	s.release()
	return err
}
