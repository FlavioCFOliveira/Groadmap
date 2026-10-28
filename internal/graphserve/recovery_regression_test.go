// Package graphserve — the regression fences for the two recovery defects of
// GoGraph v0.17.2's adoption: a reopen that restarted the write-ahead log's
// transaction sequence (rmp task #631), and a recovery that was not clean being
// served for writing (rmp task #632).
//
// Both are about what happens to a store across PROCESSES — a kill, a damaged
// log, a restart — so both drive the production entry point in a child process,
// exactly as the durability tests do, rather than a re-assembly of its steps.
package graphserve

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/cypher/expr"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"
	"github.com/FlavioCFOliveira/GoGraph/store/wal"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// walSegmentHeaderSize is the length of the header that opens every segment file
// of the write-ahead log. The engine does not export it; [segmentFrames] checks
// that the first record begins exactly there, so a change of the format fails the
// test loudly instead of corrupting the wrong bytes.
const walSegmentHeaderSize = 32

// TestServe_ResumesTheTransactionSequenceAcrossATornReopen is the regression fence
// for rmp task #631: a store reopened after a kill that left a torn tail, written
// once, and killed again, must open a third time with every surviving commit, and
// its log's transaction sequence must be strictly increasing across all three
// opens (SPEC/GRAPH.md § Engine Construction and Lifecycle, step 4).
//
// Before the fix each open restarted the sequence at 1, so the log held two
// transactions under one number, and recovery refused the store after the second
// kill ("op names a node key the commit annex does not bind").
func TestServe_ResumesTheTransactionSequenceAcrossATornReopen(t *testing.T) {
	root := graphRoot(t)
	graphDir := filepath.Join(root, "graph")

	first := startServerProcess(t, root, "first")
	for _, i := range []int{1, 2, 3} {
		mustSend(t, first.socket, fmt.Sprintf("CREATE (:Task {i: %d})", i))
	}
	first.killAndWait(t)
	removeLeftoverSocket(t, first.socket)

	second := startServerProcess(t, root, "second")
	mustSend(t, second.socket, "CREATE (:Task {i: 900}), (:Task {i: 901}), (:Task {i: 902})")
	second.killAndWait(t)
	removeLeftoverSocket(t, second.socket)

	tearLastSegment(t, graphDir, 5)

	third := startServerProcess(t, root, "third")
	mustSend(t, third.socket, "CREATE (:Task {i: 500})")
	third.killAndWait(t)
	removeLeftoverSocket(t, third.socket)

	// The open the defect made impossible.
	fourth := launchServerProcess(t, root, "fourth")
	fourth.waitUntilServed(t)
	got := intColumn(t, mustSend(t, fourth.socket, "MATCH (n:Task) RETURN n.i AS i"))
	fourth.signalAndWait(t, os.Interrupt)

	// The four single-node commits were acknowledged before a kill that tore
	// nothing of theirs, so each survives. The three-node commit is the one the
	// tear may have reached: it survives whole or not at all, never in part.
	for _, want := range []int64{1, 2, 3, 500} {
		if !slices.Contains(got, want) {
			t.Errorf("the reopened store lacks the acknowledged node i=%d; it holds %v", want, got)
		}
	}
	torn := 0
	for _, i := range got {
		if i >= 900 {
			torn++
		}
	}
	if torn != 0 && torn != 3 {
		t.Errorf("the reopened store holds %d of the three nodes one transaction created; a transaction "+
			"is recovered whole or not at all. It holds %v", torn, got)
	}

	seqs := testenv.InspectWAL(t, graphDir).TxnSeqs()
	if len(seqs) < 4 {
		t.Fatalf("the log holds %d transactions (%v), want at least the four single-node commits; the "+
			"inspection has stopped reading the sequence", len(seqs), seqs)
	}
	for k := 1; k < len(seqs); k++ {
		if seqs[k] <= seqs[k-1] {
			t.Fatalf("the log's transaction sequence is %v: it is not strictly increasing at index %d. A "+
				"reopen restarted the sequence instead of resuming it from the recovered log "+
				"(SPEC/GRAPH.md § Engine Construction and Lifecycle, step 4)", seqs, k)
		}
	}
}

// TestServe_RefusesAStoreWhoseRecoveryWasNotClean is the regression fence for rmp
// task #632: a committed transaction holding an operation that cannot be decoded
// makes the recovery return no error and report itself not clean. The server must
// refuse that store with its own line and exit code 1, before opening anything
// for writing, and leave every file of graph/ byte for byte as it found it
// (SPEC/GRAPH.md § Concurrency and Recovery; § Error Handling and Exit Codes,
// rule 11).
//
// Before the fix the server served the committed prefix and acknowledged new
// writes after the damaged record, and the next recovery discarded them.
func TestServe_RefusesAStoreWhoseRecoveryWasNotClean(t *testing.T) {
	root, graphDir := seedUncleanStore(t)
	before := lessLock(treeFingerprint(t, graphDir))

	p := launchServerProcess(t, root, "unclean")
	if code := waitForExit(t, p); code != 1 {
		t.Fatalf("the start over a store whose recovery was not clean exited %d, want 1.\nstderr:\n%s",
			code, p.capturedStderr(t))
	}
	if out := readFile(t, p.stdout); len(out) != 0 {
		t.Errorf("the refused start wrote %q to stdout; the socket must not be announced", out)
	}

	stderr := p.capturedStderr(t)
	line := lastLine(stderr)
	if !strings.HasPrefix(line, notCleanRecoveryHead) || len(line) == len(notCleanRecoveryHead) {
		t.Errorf("the refused start ended with %q, want the not-clean-recovery line %q followed by the "+
			"recovery's own account of the damage.\nstderr:\n%s", line, notCleanRecoveryHead+"<detail>", stderr)
	}

	if diff := describeFingerprintDiff(before, lessLock(treeFingerprint(t, graphDir))); diff != "" {
		t.Errorf("the refused start changed graph/; the write-ahead log must be left byte for byte as it "+
			"was found:\n%s", diff)
	}
	if _, err := os.Lstat(p.socket); !os.IsNotExist(err) {
		t.Errorf("the refused start left %s behind (lstat: %v)", p.socket, err)
	}
}

// TestServe_UncleanRecoveryWritesOnlyServerRecordsToStderr is the regression
// fence for rmp task #640: the engine's recovery reports the damage it found
// through the process's default slog logger, and a start over a store whose
// recovery was not clean must still write to stderr nothing but this server's
// records, each stamped with the canonical UTC timestamp, followed by its own
// error line (SPEC/GRAPH.md § Server Diagnostics on Stderr).
//
// Before the fix the default logger was Go's, and the recovery's record reached
// stderr through the standard log package: its date-and-time prefix, in local
// time, ahead of the error line. The child runs in a zone other than UTC so that
// a record stamped in local time cannot pass as a canonical one.
func TestServe_UncleanRecoveryWritesOnlyServerRecordsToStderr(t *testing.T) {
	t.Setenv("TZ", "Asia/Kolkata")
	root, _ := seedUncleanStore(t)

	p := launchServerProcess(t, root, "unclean")
	if code := waitForExit(t, p); code != 1 {
		t.Fatalf("the start over a store whose recovery was not clean exited %d, want 1.\nstderr:\n%s",
			code, p.capturedStderr(t))
	}

	stderr := p.capturedStderr(t)
	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	last := len(lines) - 1
	if !strings.HasPrefix(lines[last], notCleanRecoveryHead) {
		t.Fatalf("stderr does not end with the not-clean-recovery line %q.\nstderr:\n%s",
			notCleanRecoveryHead+"<detail>", stderr)
	}
	if last == 0 {
		t.Fatalf("stderr holds the error line alone; the recovery's record of the damage is missing, so "+
			"this test no longer exercises a record emitted during the open.\nstderr:\n%s", stderr)
	}
	for i, line := range lines[:last] {
		if !canonicalTimestamp.MatchString(line) {
			t.Errorf("stderr line %d is neither a server record stamped in canonical UTC nor the error "+
				"line: %q.\nstderr:\n%s", i+1, line, stderr)
		}
	}
}

// notCleanRecoveryHead is the fixed head of the line a start over a store whose
// recovery was not clean ends with; the recovery's own account of the damage
// follows it (SPEC/GRAPH.md § Error Handling and Exit Codes, rule 11). The child
// prints the error without the CLI's "Error: " prefix.
const notCleanRecoveryHead = "graph store error: graph store recovery was not clean; the write-ahead log " +
	"was left unchanged and the store was not served: "

// seedUncleanStore builds a store whose next recovery returns no error and
// reports itself not clean: three committed transactions written by a server
// that was then killed, the last of them holding an operation that cannot be
// decoded behind a record checksum that is still valid. It returns the test's
// root and the store's graph directory.
func seedUncleanStore(t *testing.T) (root, graphDir string) {
	t.Helper()

	root = graphRoot(t)
	graphDir = filepath.Join(root, "graph")

	seed := startServerProcess(t, root, "seed")
	for _, i := range []int{1, 2, 3} {
		mustSend(t, seed.socket, fmt.Sprintf("CREATE (:Task {i: %d})", i))
	}
	seed.killAndWait(t)
	removeLeftoverSocket(t, seed.socket)

	seqs := testenv.InspectWAL(t, graphDir).TxnSeqs()
	if len(seqs) < 3 {
		t.Fatalf("the seeded log holds the transactions %v, want three", seqs)
	}
	corruptCommittedOp(t, graphDir, seqs[len(seqs)-1])
	return root, graphDir
}

// removeLeftoverSocket removes the socket file a killed server left at its path,
// which the next start would otherwise have to replace itself; the starts here
// are about the store, not about stale sockets.
func removeLeftoverSocket(t *testing.T, socket string) {
	t.Helper()
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		t.Fatalf("removing the socket a killed server left at %s: %v", socket, err)
	}
}

// intColumn reads one integer column of a result, sorted, failing on any value
// the statement was not written to produce.
func intColumn(t *testing.T, result *graphclient.Result) []int64 {
	t.Helper()

	values := make([]int64, 0, len(result.Rows))
	for i, row := range result.Rows {
		if len(row) != 1 {
			t.Fatalf("row %d carries %d columns, want exactly 1", i, len(row))
		}
		v, ok := row[0].(expr.IntegerValue)
		if !ok {
			t.Fatalf("row %d carries %T, want an integer", i, row[0])
		}
		values = append(values, int64(v))
	}
	slices.Sort(values)
	return values
}

// lastLine returns the last non-empty line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// tearLastSegment removes the last n bytes of the last write-ahead-log segment
// that holds records, which leaves the torn tail a kill during a write leaves.
// A segment holding its header alone is skipped: an open starts a new segment
// before it has anything to append to it.
func tearLastSegment(t *testing.T, graphDir string, n int64) {
	t.Helper()

	dir := wal.SegmentDir(filepath.Join(graphDir, "wal"))
	segments := testenv.WALSegmentFiles(t, graphDir)
	for k := len(segments) - 1; k >= 0; k-- {
		path := filepath.Join(dir, segments[k])
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if info.Size() <= walSegmentHeaderSize+n {
			continue
		}
		if err := os.Truncate(path, info.Size()-n); err != nil {
			t.Fatalf("tearing %s: %v", path, err)
		}
		return
	}
	t.Fatalf("no write-ahead-log segment under %s holds more than %d bytes of records to tear", dir, n)
}

// segmentFrame is one record of a segment file, located by its byte offset.
type segmentFrame struct {
	offset  int // where the record's header begins
	payload []byte
}

// segmentFrames splits a segment file's bytes into its records.
func segmentFrames(t *testing.T, path string, b []byte) []segmentFrame {
	t.Helper()

	var frames []segmentFrame
	off := walSegmentHeaderSize
	for off+wal.HeaderSizeV2 <= len(b) {
		if [4]byte(b[off:off+4]) != wal.Magic {
			t.Fatalf("%s: no record magic at offset %d; the segment layout this test assumes has changed",
				path, off)
		}
		plen := int(binary.LittleEndian.Uint32(b[off+8 : off+12]))
		start := off + wal.HeaderSizeV2
		if start+plen > len(b) {
			break
		}
		frames = append(frames, segmentFrame{offset: off, payload: b[start : start+plen]})
		off = start + plen
	}
	return frames
}

// corruptCommittedOp makes the first operation of transaction seq undecodable
// while keeping its record's checksum valid, which is the damage that makes a
// recovery return no error and report itself not clean (the engine's
// ErrCommittedTxnCorruptOp): the record is intact as a record, and what it
// carries cannot be applied.
func corruptCommittedOp(t *testing.T, graphDir string, seq uint64) {
	t.Helper()

	castagnoli := crc32.MakeTable(crc32.Castagnoli)
	dir := wal.SegmentDir(filepath.Join(graphDir, "wal"))
	segments := testenv.WALSegmentFiles(t, graphDir)
	sort.Strings(segments)
	for _, name := range segments {
		path := filepath.Join(dir, name)
		b, err := os.ReadFile(path) //nolint:gosec // a path inside this test's own temporary directory
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		for _, f := range segmentFrames(t, path, b) {
			p := f.payload
			if len(p) <= 10 || p[0] != txn.OpRecordV3 || txn.OpKind(p[1]) == txn.OpCommit ||
				binary.LittleEndian.Uint64(p[2:10]) != seq {
				continue
			}
			for i := 10; i < len(p); i++ {
				p[i] ^= 0xFF
			}
			header := b[f.offset : f.offset+wal.HeaderSizeV2]
			crc := crc32.Update(crc32.Update(0, castagnoli, p), castagnoli, header[0:32])
			binary.LittleEndian.PutUint32(header[32:36], crc)
			if err := os.WriteFile(path, b, 0o600); err != nil {
				t.Fatalf("writing %s: %v", path, err)
			}
			return
		}
	}
	t.Fatalf("the log holds no operation of transaction %d to corrupt", seq)
}
