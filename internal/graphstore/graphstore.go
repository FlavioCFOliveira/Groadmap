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
//   - The CHECKPOINT's DECISION — whether a fold is owed — is the store's, and
//     it is here. The fold itself is the engine's checkpointer, which the server
//     composes over the store it opened; it is the one route a snapshot takes to
//     disk, because only it records the write-ahead-log position the snapshot
//     covers, without which the engine refuses the store after a truncation.
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
// What can drift in a checkpoint is the GATING: a transaction that appended
// nothing must neither snapshot nor truncate, and a record no fold has covered
// must never be recorded as folded. So the exported unit is
// [Store.CheckpointIfAppended], a method that DECIDES, holding its own mark, and
// runs the caller's fold only when one is owed; a caller is never handed a
// position to compare for itself. The dedicated graph server puts both of its
// folds — the in-flight one and the shutdown one — through it, so ONE
// realisation of the rule governs both, rather than a copy of the comparison
// beside a copy of the mark, which is the drift this package exists to prevent.
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
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/FlavioCFOliveira/GoGraph/cypher"
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

// walFileName is the write-ahead log's path, under the graph directory: the
// single-file log of the earlier format, and the stem the segmented format names
// its control file and segment directory from.
const walFileName = "wal"

// manifestFileName is the snapshot manifest, under the snapshot directory.
const manifestFileName = "manifest.json"

// segmentedWALFormat is the value a snapshot manifest's WALFormat carries when
// the snapshot records a position of a segmented write-ahead log. Recovery
// trusts the position under exactly this test, so the gate's opening mark does
// too.
const segmentedWALFormat = 2

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
// only concurrency control — but the Store's own bookkeeping is NOT:
// CheckpointIfAppended and Close both mutate unsynchronised fields.
//
// The one production caller is the dedicated graph server, which holds one Store
// across concurrent statements for its whole process lifetime. It MUST serialise
// its own checkpoint and Close calls against each other, and must take the
// checkpoint at a transaction boundary rather than mid-commit, for which the
// engine supplies txn.Store.RunUnderCommitLock (reachable through Txn). The
// server meets both itself — its in-flight fold and its shutdown checkpoint pass
// through one mutex of its own — so the requirement is stated here rather than
// met with a second mutex inside this type.
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

	// mark is the log position up to which a fold this Store gated has covered
	// the write-ahead log: every record below it is in the snapshot. It starts at
	// the position the snapshot the store was recovered from covers, and not at
	// the log's end at open: records the open replayed are records no snapshot
	// covers, so they count as grown from the moment the store is opened
	// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process,
	// rule 4; see [openingMark]). [Store.CheckpointIfAppended] compares against
	// it and moves it; nothing else reads it.
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
// Every failure is returned classified as utils.ErrGraphStore, except a lock
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
// utils.ErrGraphStore.
func (h *Hold) Open() (*Store, error) {
	if h.spent {
		return nil, fmt.Errorf("%w: graph store hold already spent", utils.ErrGraphStore)
	}
	h.spent = true

	graphDir := h.dir
	release := h.release

	res, err := recovery.Open[string, float64](graphDir, openOpts)
	if err != nil {
		release()
		return nil, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrGraphStore, err)
	}

	w, err := openWAL(filepath.Join(graphDir, walFileName))
	if err != nil {
		release()
		return nil, err
	}

	// The mark the gate starts from is read only now, once the log is open: the
	// open is what converts a store still in the single-file format, and the
	// conversion is what records whether that format's history is still
	// unfolded.
	ctl, _, err := InspectLog(graphDir)
	if err != nil {
		_ = w.Close() //nolint:errcheck // the open is being abandoned; the inspection's failure is the one reported
		release()
		return nil, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrGraphStore, err)
	}
	mark, err := openingMark(graphDir, ctl.Flags&wal.ControlLegacyV1Pending != 0)
	if err != nil {
		_ = w.Close() //nolint:errcheck // idem
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
		mark:    mark,
	}, nil
}

// walRetryable is the classifier openWAL hands the retry policy, held in a
// variable so that a test can COUNT the failed opens the policy classified.
//
// That count is the observable the regression fence for #294 turns on at this
// call site, and it determines the attempt count exactly: the loop asks the
// classifier once after every attempt that FAILED and never after one that
// succeeded, so an exhausted open classifies once per attempt and an open that
// succeeds classifies once per attempt before the last. The defect was a loop
// that gave up an attempt early, and SPEC/BUILD.md § No Benchmarks and No
// Performance-Measurement Tests names a count of attempts as an admissible proof
// while forbidding the elapsed-time comparison that used to stand here.
//
// The seam is here rather than on wal.Open so that the call to wal.Open stays a
// call to wal.Open: internal/testenv's TestGraphEngineConstructionsMatchSpec
// reads this function's write-side shape from that call expression, and a seam
// that hid it would make this constructor look like one that opens a
// transactional store over no write-ahead log at all.
//
// Production never reassigns it, it is unexported, and openWAL behaves exactly as
// it did when backoff.Always was passed literally: every failure is retryable
// either way.
var walRetryable = backoff.Always

// openWAL opens the write-ahead-log writer at walPath under the project's single
// bounded backoff policy (internal/backoff), which owns the attempt count and the
// delay ladder. This sequence used to keep its own constants and its own loop,
// and the loop disagreed with them (task #294); it now has neither.
//
// Every failure is waited on, because the one this retry exists for is contention
// — another process holding the WAL directory lock — and a WAL that cannot be
// opened for any other reason is not distinguishable here anyway.
func openWAL(walPath string) (*wal.Writer, error) {
	w, err := backoff.Retry(func() (*wal.Writer, error) { return wal.Open(walPath) }, walRetryable)
	if err != nil {
		return nil, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrGraphStore, err)
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

// CheckpointIfAppended runs fold — the caller's own realisation of a checkpoint —
// if and only if the write-ahead log holds a durable record that no fold this
// Store gated has covered, including records the log already held when the store
// was opened. It reports whether fold ran, and returns fold's error unchanged.
//
// # The gate, in log positions
//
// Every record of the log carries a logical position, positions only grow, and
// neither a fold nor the truncation that follows it moves them (SPEC/GRAPH.md §
// Synchronous Checkpoint on Write, step 3). The question the gate asks is
// therefore always the same one: does the log's durable end — the position just
// past the last record its last synchronisation covers — lie beyond the mark,
// the position up to which the last fold covered it? It reads that one value,
// once per call, before the fold, and that is the whole of its cost
// (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process, rules
// 4 and 9).
//
// A fold nothing owes MUST NOT snapshot and MUST NOT truncate: it would rewrite a
// full snapshot of the whole graph for nothing (SPEC/GRAPH.md § What a Statement
// That Writes Nothing Changes on Disk). The comparison lives here rather than at
// the call sites, because it is the rule and not the number that has to hold, and
// the dedicated graph server puts BOTH of its checkpoints — the in-flight fold its
// cadence makes due and the shutdown checkpoint — through this one method, so the
// two share one comparison and one mark (rules 4 and 5 of that section).
//
// # What the mark records after a fold, and what it must never record
//
// The mark becomes the durable end read BEFORE fold was called. The fold captures
// the graph at a transaction boundary it reaches afterwards, under the store's
// commit serialiser, so its capture is proven to cover that position; it may
// cover more, and the cost of the shortfall is at most one further fold. The
// durable end read AFTER the fold returns is never the mark: writers commit while
// the snapshot is written, what they append lies beyond the capture, and taking
// that end would record those records as folded, so that a server that then
// received no further write would fold them neither in flight nor at shutdown
// (rules 4 and 10).
//
// A fold that FAILED moves nothing: the fold is still owed, and the next call
// makes it again.
//
// # Concurrency
//
// The mark is unsynchronised, like the rest of a Store's own bookkeeping, so a
// caller that holds one Store across concurrent statements must serialise this
// against itself and against [Store.Close] exactly as the type documents.
func (s *Store) CheckpointIfAppended(fold func() error) (bool, error) {
	end := s.wal.DurableOffset()
	if end <= s.mark {
		return false, nil
	}
	if err := fold(); err != nil {
		return false, err
	}
	s.mark = end
	return true, nil
}

// openingMark is the mark a freshly opened Store starts with: the position up to
// which the snapshot the store was recovered from covers the write-ahead log, and
// not the log's end at open (SPEC/GRAPH.md § Durability and Checkpointing in a
// Long-Lived Process, rule 4).
//
// A server that was killed leaves records beyond that position, the next open
// replays them, and the replay folds nothing, so they count as grown from the
// moment the store is opened. A store with no snapshot, or with one that records
// no position — the form an engine that kept the single-file log wrote — starts
// at the beginning of the log, position zero.
//
// legacyPending reports that the log's control file still marks the single-file
// log's history as unfolded, which is the state the engine's one-way conversion
// leaves a store whose single-file log held history (SPEC/GRAPH.md § Graph Store
// Format Migration, rule 11). That history lies outside the segmented log's
// positions, so no durable end can stand for it; the mark is set below every
// position instead, so that the first gated fold is owed whatever the durable end
// reads, and the fold that pays it replaces the single-file log with its stub.
func openingMark(graphDir string, legacyPending bool) (int64, error) {
	if legacyPending {
		return -1, nil
	}
	m, err := snapshot.ReadManifestFile(filepath.Join(graphDir, snapshotDirName, manifestFileName))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: graph store unavailable: reading the snapshot manifest: %v", utils.ErrGraphStore, err)
	}
	// The same test recovery applies before it trusts the position: a manifest
	// that does not declare the segmented format records none.
	if m.WALFormat != segmentedWALFormat {
		return 0, nil
	}
	if m.WALRedoPos > math.MaxInt64 {
		return 0, fmt.Errorf("%w: graph store unavailable: the snapshot manifest records an impossible log position %d",
			utils.ErrGraphStore, m.WALRedoPos)
	}
	return int64(m.WALRedoPos), nil
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
