// Package graphserve — the measurement harness of rmp task #370.
//
// # What it is for
//
// Two decisions in this package are recorded as PROVISIONAL and say so in their
// own doc comments: the connection ceiling (maxConnections) and the in-flight
// checkpoint cadence (productionCadence). SPEC/GRAPH.md § Server Options and
// § Durability and Checkpointing in a Long-Lived Process both decline to fix a
// value and both say the same thing about why — a quota is a capacity decision
// and a cadence is a workload property, and a specification has no measurement of
// a running server to make either from. This file is that measurement.
//
// It produces four quantities and nothing else:
//
//  1. Throughput and p99 latency on the SERVER path, across a concurrency ladder,
//     so the point at which read throughput stops rising can be STATED rather
//     than assumed. That knee is what makes a connection ceiling defensible.
//  2. Throughput and p99 latency on the PER-INVOCATION path, sequential, as the
//     baseline the server is compared against.
//  3. Write-ahead log growth per write, which is the benefit side of the cadence:
//     bytes per unit time is this multiplied by throughput.
//  4. What one in-flight fold costs the writers it quiesces, which is the cost
//     side of the same decision.
//
// # Why every benchmark drives a real server and a real store
//
// Nothing here is mocked, and nothing may be. The numbers are used to set values
// that ship, so a harness that measured a substitute would set them from a
// substitute's behaviour. The server is assembled by [startRealServerAt], which
// is the same bind and the same build the production startup sequence performs,
// and the per-invocation baseline runs the same graphstore sequence
// internal/commands.runGraphExecute runs, in the same order.
//
// # How to run them
//
//	go test ./internal/graphserve -run '^$' -bench . -benchtime 3s
//
// The race detector is deliberately NOT part of that: it changes the timings by
// roughly an order of magnitude, and every number here is a timing.
package graphserve

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

// benchNodes and benchEdges are the MEASURED shape of the real `groadmap`
// knowledge graph on the machine these numbers were taken on: 749 nodes, 2285
// edges, 1.4 MB on disk. They are rounded to 750 and 2250 so the edge count is a
// whole multiple of the node count and every node has the same out-degree, which
// makes the generator trivial and the graph regular. The rounding costs 0.1% of
// the nodes and 1.5% of the edges — far inside the variation between one
// knowledge graph and another, and the point of the size is the ORDER, not the
// exact count.
//
// The size is not arbitrary and it is not "big enough to be interesting". A
// checkpoint's cost is proportional to the LIVE GRAPH — 19.7 ms on a 1.3 MB store
// against 964 ms on a 122 MB one (SPEC/GRAPH.md § Lock Contention) — so a cadence
// measured over a graph of the wrong size is a cadence set for a product nobody
// has. Measuring the store the product actually has is what makes these numbers
// applicable to it.
const (
	benchNodes = 750
	benchEdges = 2250
)

// benchSeedBatch is how many rows one seeding statement carries.
//
// Seeding must not be the benchmark's cost, so it is batched rather than sent one
// statement per node; and a batch must stay well inside the server's 5 s
// statement budget, because a seed the budget cut would leave the benchmarks
// measuring a graph of an unpredictable size. 250 rows is three statements for
// the nodes and nine for the edges.
const benchSeedBatch = 250

// benchConcurrency is the ladder every server-path benchmark runs over. It is
// shared rather than restated so that a rung added for one benchmark is added for
// all of them and the results stay comparable side by side.
//
// It doubles from 1 to 64. Doubling is what locates a knee — a linear ladder
// spends its rungs where the curve is already flat — and 64 is chosen because the
// connection ceiling under test sits above it, so the ladder measures the server
// and never the ceiling's refusal.
var benchConcurrency = []int{1, 2, 4, 8, 16, 32, 64}

// seedStatements builds the deterministic synthetic graph: nodes labelled Item
// carrying a seq property, and edges wiring each node to three others.
//
// It is deterministic in the strict sense — the same nodes and edges every run,
// with no randomness anywhere — because a benchmark whose graph varies is a
// benchmark whose numbers cannot be compared across runs, which is the only thing
// these numbers are for.
//
// The edge generator uses `(i * 7 + 1) % nodes`. Seven is coprime with 750, so
// the targets of the three edges leaving each node are distinct and spread across
// the whole key space rather than clustered next to the source; a graph wired
// only to its neighbours would give a scan an unrepresentatively friendly memory
// access pattern.
func seedStatements(nodes, edges int) []string {
	statements := make([]string, 0, (nodes+edges)/benchSeedBatch+2)

	for lo := 0; lo < nodes; lo += benchSeedBatch {
		hi := min(lo+benchSeedBatch, nodes) - 1
		statements = append(statements, fmt.Sprintf(
			"UNWIND range(%d, %d) AS i CREATE (:Item {seq: i, kind: 'item'})", lo, hi))
	}

	for lo := 0; lo < edges; lo += benchSeedBatch {
		hi := min(lo+benchSeedBatch, edges) - 1
		statements = append(statements, fmt.Sprintf(
			"UNWIND range(%d, %d) AS i "+
				"MATCH (a:Item {seq: i %% %d}), (b:Item {seq: ((i * 7) + 1) %% %d}) "+
				"CREATE (a)-[:LINKS]->(b)", lo, hi, nodes, nodes))
	}

	return statements
}

// seedGraph sends the synthetic graph to a running server.
//
// It goes through graphclient.Send, the one client every surface uses, so the
// seed crosses the same protocol the measured statements cross and the store the
// benchmarks read was written the way the product writes.
func seedGraph(tb testing.TB, socket string, nodes, edges int) {
	tb.Helper()

	for _, statement := range seedStatements(nodes, edges) {
		if _, err := graphclient.Send(context.Background(), socket, statement); err != nil {
			tb.Fatalf("seeding the benchmark graph with %q: %v", statement, err)
		}
	}
}

// runConcurrent spreads b.N operations over workers goroutines, timing each one,
// and reports throughput and p99 latency as custom metrics.
//
// # Why each worker owns its own latency slice
//
// Because the harness must measure the server and not itself. A shared slice
// needs a mutex on the hot path, and a mutex taken once per operation by 64
// goroutines is a contention point of the harness's own making that would appear
// in the numbers as the server's. Per-worker slices need no synchronisation at
// all: each is written by exactly one goroutine, and the join before they are
// read is what publishes them.
//
// Each slice is pre-allocated to its worker's EXACT share of b.N for the same
// reason: an append that reallocates inside the timed region copies the slice on
// some operation and not others, which shows up as a latency outlier the server
// never produced. The shares are contiguous ranges, so worker w owns a known
// interval of the operation index and the index it receives is stable across
// concurrency levels.
//
// # Why the wall clock is measured here rather than read from b
//
// The metric wanted is THROUGHPUT — operations per second of wall-clock time
// while workers goroutines run — and b's own timer is not that: with N goroutines
// running concurrently, ns/op counts one operation's share of a run whose length
// is set by the slowest worker. time.Now around the whole fan-out is the quantity
// the ladder needs, so it is taken explicitly and reported as ops/s.
//
// # Why errors are collected rather than reported where they happen
//
// b.Fatalf must be called from the goroutine running the benchmark and from no
// other; b.Errorf is safe elsewhere but does not stop the worker. Each worker
// therefore records its first error into a slot it alone writes and returns, and
// the failure is raised on the main goroutine after the join.
func runConcurrent(b *testing.B, workers int, op func(worker, i int) error) {
	b.Helper()

	if workers < 1 {
		workers = 1
	}

	latencies := make([][]time.Duration, workers)
	failures := make([]error, workers)
	bounds := make([]int, workers+1)

	// Contiguous, exact shares: the first (b.N mod workers) workers take one
	// operation more, so the shares sum to b.N precisely and no worker's slice
	// is over- or under-allocated.
	base, extra := b.N/workers, b.N%workers
	for w := range workers {
		size := base
		if w < extra {
			size++
		}
		bounds[w+1] = bounds[w] + size
		latencies[w] = make([]time.Duration, 0, size)
	}

	var wg sync.WaitGroup
	wg.Add(workers)

	b.ResetTimer()
	wall := time.Now()

	for w := range workers {
		go func(w int) {
			defer wg.Done()
			for i := bounds[w]; i < bounds[w+1]; i++ {
				at := time.Now()
				err := op(w, i)
				latencies[w] = append(latencies[w], time.Since(at))
				if err != nil {
					failures[w] = err
					return
				}
			}
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(wall)
	b.StopTimer()

	for w, err := range failures {
		if err != nil {
			b.Fatalf("worker %d failed: %v", w, err)
		}
	}

	b.ReportMetric(opsPerSecond(b.N, elapsed), "ops/s")
	b.ReportMetric(p99Millis(latencies...), "p99ms")
}

// opsPerSecond is throughput, guarded against a run so short the clock reports
// nothing: a zero elapsed time would report +Inf, which is not a measurement and
// would silently poison a benchstat comparison.
func opsPerSecond(n int, elapsed time.Duration) float64 {
	seconds := elapsed.Seconds()
	if seconds <= 0 {
		return 0
	}
	return float64(n) / seconds
}

// p99Millis merges the per-worker latency slices and returns the 99th percentile
// in milliseconds.
//
// The index is ceil(0.99*n)-1 on the sorted merge — the nearest-rank definition,
// which returns an OBSERVED latency rather than an interpolation between two of
// them. An empty input returns zero rather than panicking on an index of -1: a
// benchmark whose op never ran has no percentile, and reporting zero keeps the
// metric column present so the surrounding rows still line up.
//
// The arithmetic is float64 throughout and narrows nothing. A conversion from a
// float index to an integer is exactly the shape gosec's G115 flags, and it would
// be right to: math.Ceil of a product of a float and a count is not bounded by
// anything the type system knows. Clamping into range afterwards is what makes it
// safe, and it is done explicitly rather than assumed.
func p99Millis(perWorker ...[]time.Duration) float64 {
	total := 0
	for _, worker := range perWorker {
		total += len(worker)
	}
	if total == 0 {
		return 0
	}

	merged := make([]time.Duration, 0, total)
	for _, worker := range perWorker {
		merged = append(merged, worker...)
	}
	slices.Sort(merged)

	rank := int(math.Ceil(0.99*float64(total))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= total {
		rank = total - 1
	}
	return float64(merged[rank]) / float64(time.Millisecond)
}

// walBytesAt reports the size of the write-ahead log under graphDir, or zero when
// it cannot be stated.
//
// A missing log is zero and not a failure: the file is created on the first
// append, so a store that has been read and never written has none, and a
// benchmark that measured growth from an absent log to a present one is measuring
// the same growth either way.
func walBytesAt(graphDir string) int64 {
	info, err := os.Stat(filepath.Join(graphDir, "wal"))
	if err != nil {
		return 0
	}
	return info.Size()
}

// readStatement is the point lookup: one node by its seq, returning one scalar.
func readStatement(i int) string {
	return "MATCH (n:Item {seq: " + itoa(i%benchNodes) + "}) RETURN n.seq"
}

// writeStatement is the write every write benchmark issues: a property set on one
// existing node, which genuinely commits.
//
// It writes a DIFFERENT value on every operation (the operation index) so the
// engine cannot elide the write as a no-op, and it targets a node chosen by that
// same index so concurrent writers mostly touch different nodes. Mostly, not
// always: two workers do collide, the loser's transaction fails as
// Neo.TransientError.Transaction.Outdated, and graphclient.Send retries it
// (SPEC/GRAPH.md § Concurrency Inside the Server, rule 4). That retry is part of
// what the server path costs and is deliberately inside the measurement.
func writeStatement(i int) string {
	return "MATCH (n:Item {seq: " + itoa(i%benchNodes) + "}) SET n.touched = " + itoa(i)
}

// BenchmarkServedRead is the point lookup across the ladder.
//
// The cadence is DISABLED. An in-flight fold quiesces writers for the instant it
// captures the graph and then writes a snapshot proportional to the live graph;
// landing inside a timed region it would appear as a latency spike belonging to
// the checkpointer rather than to the read path. What a fold costs is measured
// separately and deliberately, by BenchmarkServedWriteWithCheckpointing.
func BenchmarkServedRead(b *testing.B) {
	socket, stop := startRealServer(b, checkpointCadence{})
	defer stop()

	seedGraph(b, socket, benchNodes, benchEdges)

	for _, workers := range benchConcurrency {
		b.Run(fmt.Sprintf("c=%d", workers), func(b *testing.B) {
			runConcurrent(b, workers, func(_, i int) error {
				_, err := graphclient.Send(context.Background(), socket, readStatement(i))
				return err
			})
		})
	}
}

// BenchmarkServedReadWholeGraph is the scan across the ladder, and it exists
// because the point lookup alone cannot answer the question the ladder is asked.
//
// A point lookup returning one scalar is dominated by connect, HELLO/LOGON, the
// protocol framing and the disconnect: what it measures as concurrency rises is
// whether the TRANSPORT parallelises. A scan over every node does real work
// inside the engine, so what it measures is whether the ENGINE's readers run in
// parallel. "Do concurrent readers serialise" needs both answers — a server whose
// transport scaled and whose engine did not would look healthy on the first
// benchmark alone — and the knee that maxConnections cites is the one that
// survives on both.
func BenchmarkServedReadWholeGraph(b *testing.B) {
	socket, stop := startRealServer(b, checkpointCadence{})
	defer stop()

	seedGraph(b, socket, benchNodes, benchEdges)

	for _, workers := range benchConcurrency {
		b.Run(fmt.Sprintf("c=%d", workers), func(b *testing.B) {
			runConcurrent(b, workers, func(_, _ int) error {
				_, err := graphclient.Send(context.Background(), socket, "MATCH (n:Item) RETURN count(n)")
				return err
			})
		})
	}
}

// BenchmarkServedMixed is 90% reads and 10% writes across the ladder, which is
// the shape the knowledge graph is actually used in: it is queried constantly and
// written at a commit.
//
// The split is DETERMINISTIC — every tenth operation index writes — rather than
// sampled, so two runs issue exactly the same statements in exactly the same
// order and are comparable. A randomised split would make the mix a variable of
// the run.
//
// It reports a separate p99 for the writes alone. The overall p99 is dominated by
// the 90% that read, so a write path that had grown a long tail would be
// invisible in it; wp99ms is what makes the two separable. The write latencies
// are collected per worker for the reason [runConcurrent] gives about its own
// slices — one writer per slot, no mutex on the hot path.
func BenchmarkServedMixed(b *testing.B) {
	socket, stop := startRealServer(b, checkpointCadence{})
	defer stop()

	seedGraph(b, socket, benchNodes, benchEdges)

	for _, workers := range benchConcurrency {
		b.Run(fmt.Sprintf("c=%d", workers), func(b *testing.B) {
			writeLatencies := make([][]time.Duration, workers)

			runConcurrent(b, workers, func(worker, i int) error {
				if i%10 != 0 {
					_, err := graphclient.Send(context.Background(), socket, readStatement(i))
					return err
				}
				at := time.Now()
				_, err := graphclient.Send(context.Background(), socket, writeStatement(i))
				writeLatencies[worker] = append(writeLatencies[worker], time.Since(at))
				return err
			})

			b.ReportMetric(p99Millis(writeLatencies...), "wp99ms")
		})
	}
}

// BenchmarkServedWrite is writes only across the ladder, and it is the benchmark
// the checkpoint cadence is set from.
//
// # walB/op, and why it is the quantity that matters
//
// The cadence's BENEFIT is bounded write-ahead log growth, and growth is bytes per
// unit time, which is this figure multiplied by the throughput the same run
// reports. A cadence chosen without it is chosen against an imagined write rate.
// The log is stat-ed immediately before the timed region and immediately after,
// and the difference is divided by b.N.
//
// # Why the cadence is disabled here specifically
//
// Not to flatter the numbers: to make this one exist at all. A fold TRUNCATES the
// log, so a fold landing inside the measured window subtracts an unknown number of
// bytes from the growth and the figure reported is not growth but growth minus
// whatever was folded. Disabling the cadence is what makes the difference between
// the two stats a measurement of appends rather than of appends and truncations
// together.
func BenchmarkServedWrite(b *testing.B) {
	socket, graphDir, stop := startRealServerAt(b, checkpointCadence{})
	defer stop()

	seedGraph(b, socket, benchNodes, benchEdges)

	for _, workers := range benchConcurrency {
		b.Run(fmt.Sprintf("c=%d", workers), func(b *testing.B) {
			before := walBytesAt(graphDir)

			runConcurrent(b, workers, func(_, i int) error {
				_, err := graphclient.Send(context.Background(), socket, writeStatement(i))
				return err
			})

			grown := walBytesAt(graphDir) - before
			b.ReportMetric(float64(grown)/float64(b.N), "walB/op")
		})
	}
}

// BenchmarkServedWriteWithCheckpointing is the COST side of the cadence decision:
// what one in-flight fold costs the writers it quiesces, in throughput and in the
// tail.
//
// The same write load runs twice at one concurrency level, once with the cadence
// off and once with it fast enough that several folds land inside a benchmark's
// lifetime. The difference between the two rows is the whole answer; benchstat
// over them names it with a confidence interval.
//
// Concurrency 8 is the level, not the ladder, because the question is not how the
// cost scales but whether it is payable at all — and a single level keeps the two
// rows a clean pair. Eight is a rung the ladder measures, so this benchmark's
// cadence=off row can be read against BenchmarkServedWrite's c=8 row as a check
// that the two harnesses agree.
//
// The fast cadence is a MaxAge of one second polled every 250 ms. It is not a
// candidate production value and is not proposed as one: it is short enough that
// a fold is reachable inside a benchmark, which a five-minute cadence is not, and
// the cost it measures is the cost of ONE fold however often folds happen.
func BenchmarkServedWriteWithCheckpointing(b *testing.B) {
	const workers = 8

	cases := []struct {
		name    string
		cadence checkpointCadence
	}{
		{name: "cadence=off", cadence: checkpointCadence{}},
		{name: "cadence=fast", cadence: checkpointCadence{maxAge: time.Second, interval: 250 * time.Millisecond}},
	}

	for _, tc := range cases {
		// The server is built per case and outside b.Run, because the cadence is
		// fixed at build time and cannot be changed on a running server — and
		// because building it inside would rebuild and reseed on every one of the
		// framework's growing-N passes.
		socket, stop := startRealServer(b, tc.cadence)
		seedGraph(b, socket, benchNodes, benchEdges)

		b.Run(tc.name, func(b *testing.B) {
			runConcurrent(b, workers, func(_, i int) error {
				_, err := graphclient.Send(context.Background(), socket, writeStatement(i))
				return err
			})
		})

		stop()
	}
}

// BenchmarkServedNoop is the noise floor of the server path, at one client.
//
// `RETURN 1` touches no graph at all, so what this measures is connect,
// HELLO/LOGON, RUN, PULL and disconnect — the whole of the transport and none of
// the engine. Every other served figure in this file is THIS plus the statement,
// which is what makes it the number to read the others against: a served figure
// that approaches this one is reporting the protocol and not the graph, and no
// conclusion about the engine may be drawn from it.
//
// There is no ladder. A floor is a property of the path, not of the concurrency,
// and the ladder's own scaling questions are asked by the benchmarks that do
// graph work.
func BenchmarkServedNoop(b *testing.B) {
	socket, stop := startRealServer(b, checkpointCadence{})
	defer stop()

	runConcurrent(b, 1, func(_, _ int) error {
		_, err := graphclient.Send(context.Background(), socket, "RETURN 1")
		return err
	})
}

// seedDirect builds a graph directory holding the same synthetic graph the served
// benchmarks read, seeded through the per-invocation sequence rather than through
// a server.
//
// It is seeded in process, and once: the shape must be identical to the served
// benchmarks' or the two sets of numbers are not comparable, and seedStatements
// is the single generator both use, so they cannot drift. The store is opened
// once for the whole seed rather than per statement — this is setup, not the
// measurement, and the measurement opens per invocation because that is what the
// product does.
//
// The seed runs on a background context with no deadline. The statement budget
// belongs on the measured invocations, where production applies it; a seed the
// budget cut would leave a graph of an unpredictable size behind and every
// benchmark reading it would be measuring something else.
func seedDirect(tb testing.TB) string {
	tb.Helper()

	graphDir := filepath.Join(tb.TempDir(), "graph")
	if err := os.MkdirAll(graphDir, 0700); err != nil {
		tb.Fatalf("creating %s: %v", graphDir, err)
	}

	st, err := graphstore.Open(graphDir)
	if err != nil {
		tb.Fatalf("opening the graph store at %s: %v", graphDir, err)
	}
	defer st.Close() //nolint:errcheck // the seed's own close error is reported below, on the path that matters

	for _, statement := range seedStatements(benchNodes, benchEdges) {
		result, runErr := st.Engine().RunAny(context.Background(), statement, nil)
		if runErr != nil {
			tb.Fatalf("seeding %q: %v", statement, runErr)
		}
		for result.Next() {
		}
		if iterErr := result.Err(); iterErr != nil {
			_ = result.Close() //nolint:errcheck // rolling back a seed that already failed
			tb.Fatalf("seeding %q: %v", statement, iterErr)
		}
		if closeErr := result.Close(); closeErr != nil {
			tb.Fatalf("committing the seed %q: %v", statement, closeErr)
		}
	}

	if _, err := st.Checkpoint(); err != nil {
		tb.Fatalf("checkpointing the seeded store: %v", err)
	}
	if err := st.Close(); err != nil {
		tb.Fatalf("closing the seeded store: %v", err)
	}

	return graphDir
}

// directInvocation performs the WHOLE per-invocation sequence
// internal/commands.runGraphExecute performs on its direct path, in the same
// order: take the exclusive advisory hold, open the store through recovery, run
// the statement under the statement budget, drain the result, commit by closing
// it, checkpoint, and close the store — which releases the hold.
//
// Every step is here because every step is on the real path. An implementation
// that hoisted the open out of the loop would measure a server without a socket
// rather than the per-invocation path, and the open is a large part of what that
// path costs.
func directInvocation(graphDir, statement string) error {
	hold, err := graphstore.Acquire(graphDir)
	if err != nil {
		return err
	}

	// Hold.Open releases the hold itself on failure, so there is nothing to
	// release here and nothing to close.
	st, err := hold.Open()
	if err != nil {
		return err
	}
	defer st.Close() //nolint:errcheck // idempotent; this covers the early returns, and the close that matters is reported below

	ctx, cancel := context.WithTimeout(context.Background(), graphlock.StatementBudget)
	defer cancel()

	result, err := st.Engine().RunAny(ctx, statement, nil)
	if err != nil {
		return err
	}
	for result.Next() {
	}
	if err := result.Err(); err != nil {
		_ = result.Close() //nolint:errcheck // rolling back; the commit error is moot once iteration has failed
		return err
	}
	// Close is the commit: the write transaction applies here and this is where a
	// commit failure surfaces.
	if err := result.Close(); err != nil {
		return err
	}
	if _, err := st.Checkpoint(); err != nil {
		return err
	}
	return st.Close()
}

// runSequential drives b.N per-invocation operations one after another and
// reports the same two metrics the served benchmarks report, so the two sets of
// rows can be read against each other without converting anything.
func runSequential(b *testing.B, op func(i int) error) {
	b.Helper()

	latencies := make([]time.Duration, 0, b.N)

	b.ResetTimer()
	wall := time.Now()

	for i := range b.N {
		at := time.Now()
		err := op(i)
		latencies = append(latencies, time.Since(at))
		if err != nil {
			b.Fatalf("invocation %d failed: %v", i, err)
		}
	}

	elapsed := time.Since(wall)
	b.StopTimer()

	b.ReportMetric(opsPerSecond(b.N, elapsed), "ops/s")
	b.ReportMetric(p99Millis(latencies), "p99ms")
}

// BenchmarkDirectRead is the per-invocation baseline for a read.
//
// # What it excludes, and why the exclusion is honest
//
// It runs in process. It therefore excludes the fork and exec of ./bin/rmp, the
// dynamic loading and Go runtime start-up that follow, the argument parsing, and
// the JSON serialisation of the result — all of which a real
// `rmp graph execute` pays and none of which is here. That makes this a LOWER
// BOUND on what the per-invocation path costs, and it makes the comparison it
// supports CONSERVATIVE in exactly the direction that matters: the server path's
// advantage over a real invocation can only be LARGER than these rows show, never
// smaller. A baseline that flattered the alternative would be worthless; one that
// flatters it against itself is safe to conclude from.
//
// # Why there is no concurrency ladder
//
// Because the path admits no concurrency, structurally. A per-invocation
// execution takes the graph store's EXCLUSIVE advisory lock for the whole
// sequence — open, statement, commit, checkpoint, close — so two invocations
// cannot overlap by construction: a second one waits under the bounded backoff
// and then fails (SPEC/GRAPH.md § Lock Contention). A ladder over it would not
// measure concurrency; it would measure the lock's wait budget. That structural
// fact is what the whole server-versus-invocation comparison rests on, and the
// absence of a ladder here is the shape of it.
func BenchmarkDirectRead(b *testing.B) {
	graphDir := seedDirect(b)

	runSequential(b, func(i int) error {
		return directInvocation(graphDir, readStatement(i))
	})
}

// BenchmarkDirectWrite is the per-invocation baseline for a write, over the same
// store shape and the same statement the served write benchmarks issue.
//
// It carries one cost the served path does not, and the difference is not an
// artefact: a short-lived invocation checkpoints SYNCHRONOUSLY after any
// transaction that appended to the log, because it has no later opportunity —
// it is about to exit (SPEC/GRAPH.md § Synchronous Checkpoint on Write). Every
// iteration here therefore writes a full snapshot of the live graph and truncates
// the log. That is precisely the cost a server exists to amortise across a
// cadence, so this row is not merely the baseline for the write path; it is the
// measurement that says what the cadence is worth.
//
// The exclusions and the absence of a ladder are [BenchmarkDirectRead]'s, for the
// same reasons.
func BenchmarkDirectWrite(b *testing.B) {
	graphDir := seedDirect(b)

	runSequential(b, func(i int) error {
		return directInvocation(graphDir, writeStatement(i))
	})
}
