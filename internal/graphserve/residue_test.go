// Package graphserve — the regression fence for the on-disk residue a cut write
// used to leave behind, and for the gate that stops it.
//
// # The defect this fences
//
// Measured on rmp task #380 against this server: ONE statement the budget cut
// while it was writing — `MATCH (a),(b),(c) CREATE ()`, rolled back whole — grew
// an 80 KB store holding 600 nodes to 134 MB, permanently, and made a later
// `MATCH (n) RETURN count(*)` over the same 600 nodes cost 1.48 s and 670 MB
// instead of 0.01 s and 21.6 MB. The isolating control was a cut READ over the
// same store, which left it at 80 KB.
//
// The rollback restores the LOGICAL graph and not the PHYSICAL one: the key
// mapper keeps the interned key of every node the statement created and the
// tombstone set keeps a tombstone for each. Nothing on the direct path ever
// published that, because graphstore.Store.Checkpoint refuses to fold when the
// write-ahead log has not grown — and a rolled-back transaction appends nothing.
// The server's shutdown checkpoint went straight to the engine's checkpointer,
// which has no such gate and serialises the whole graph unconditionally.
//
// # Why the assertion is the WHOLE directory and not its size
//
// The acceptance criterion is that the store is the size it was. Size alone is a
// weak reading of it: a fold whose residue happened to be small would pass while
// still rewriting the snapshot, and the property that actually holds is stronger
// and simpler — a shutdown that owes no fold writes NOTHING. So the fence compares
// every file under the graph directory by name, length and content, which is an
// assertion the gate satisfies exactly and an ungated fold cannot satisfy at all:
// the snapshot it rewrites carries a fresh sequence number whatever the graph did.
//
// # Why the statement is cut by a CLIENT-supplied timeout
//
// The production budget is 5 seconds and the memory a cut write reaches over that
// budget is measured in gigabytes — 3618 MB for this exact statement — which is
// not a cost a unit test may impose on whoever runs `go test ./...`, still less
// under the race detector. The engine's Bolt session honours a per-statement
// `timeout` in the RUN metadata, clamped by the server's own maximum, so a client
// may ask for LESS. The cut is then the same cut, taken by the same deadline
// mechanism at the same point in the same statement, at a hundredth of the cost.
// That is the only thing this test scales down: everything else — the process, the
// protocol, the rollback, the shutdown sequence — is production's.
package graphserve

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/bolt/packstream"
	"github.com/FlavioCFOliveira/GoGraph/bolt/proto"
)

// residueSeedNodes is how many nodes the store holds. The cut statement below is
// their triple Cartesian product, so this number decides only that the product
// cannot be exhausted inside the cut: 200 nodes are 8,000,000 combinations, which
// is four orders of magnitude more than the statement can reach in the window it
// is given.
const residueSeedNodes = 200

// residueCutMillis is the per-statement timeout the client asks for. It has to be
// long enough that the statement is inside its write loop when the deadline fires
// — a statement cut before it applied anything would roll back nothing and leave
// no residue for the gate to refuse — and short enough that the mutations it
// applies cost megabytes rather than gigabytes. See the package comment.
const residueCutMillis = 300

// TestServedCutWrite_LeavesTheStoreExactlyAsItFoundIt is the fence.
//
// The three phases are one store seen three times: a server that writes it and
// folds it, a server that serves one cut write over it and must leave it alone,
// and a third open that reads back what the second one did NOT do.
func TestServedCutWrite_LeavesTheStoreExactlyAsItFoundIt(t *testing.T) {
	root := graphRoot(t)

	// Phase 1. A store with real content and a CURRENT snapshot: the seeding
	// server's own shutdown folds the log it grew, which is the fold this gate
	// must still let through. Everything after this compares against what that
	// shutdown left.
	seeder := startServerProcess(t, root, "seed")
	mustSend(t, seeder.socket, fmt.Sprintf(
		"UNWIND range(1,%d) AS i CREATE (:Bulk {i:i})", residueSeedNodes))
	seeder.signalAndWait(t, syscall.SIGINT)

	if size := walSize(t, seeder.walPath()); size != 0 {
		t.Fatalf("the write-ahead log holds %d bytes after the seeding server's shutdown, want 0. "+
			"That shutdown was OWED a fold — it wrote %d nodes — so a log it did not truncate "+
			"means the gate is refusing folds it must allow, and every assertion below would "+
			"then be measuring a store nothing ever checkpointed", size, residueSeedNodes)
	}
	baseline := fingerprintDir(t, seeder.graphDir)
	if _, ok := baseline[filepath.Join("snapshot", "manifest.json")]; !ok {
		t.Fatalf("there is no snapshot/manifest.json after the seeding server's shutdown; the "+
			"store this test compares against was never checkpointed. It holds: %v",
			sortedKeys(baseline))
	}

	// Phase 2. One cut write, and nothing else, against that store.
	server := startServerProcess(t, root, "cut")
	session := dialBolt(t, server.socket)

	started := time.Now()
	failure := runExpectingFailure(t, session, "MATCH (a),(b),(c) CREATE ()", residueCutMillis)
	elapsed := time.Since(started)

	if elapsed < residueCutMillis*time.Millisecond {
		t.Fatalf("the statement failed after %v, sooner than the %dms it was given, with %s: %s. "+
			"It was refused rather than CUT, so it applied nothing, rolled back nothing, and "+
			"left no residue for this test to be about",
			elapsed, residueCutMillis, failure.Code, failure.Message)
	}

	// The rollback is real: the graph the same server answers from holds the
	// seeded nodes and not one of the anonymous nodes the statement created.
	if rows := len(mustSend(t, server.socket, "MATCH (n) RETURN n").Rows); rows != residueSeedNodes {
		t.Fatalf("the graph holds %d nodes after the cut write, want the %d it was seeded with. "+
			"The statement was not rolled back, so what follows would be a test of a store that "+
			"legitimately changed", rows, residueSeedNodes)
	}

	session.close()
	server.signalAndWait(t, syscall.SIGINT)

	// Phase 3. The assertion.
	after := fingerprintDir(t, server.graphDir)
	if diff := describeFingerprintDiff(baseline, after); diff != "" {
		t.Errorf("the graph store CHANGED across a server that served one cut write and nothing "+
			"else:\n%s\nA statement that is rolled back appends nothing to the write-ahead log, "+
			"so the shutdown owes no fold and must write nothing. An ungated fold publishes the "+
			"key mapper and the tombstone set the rolled-back write left behind, which is "+
			"permanent: measured on rmp task #380, one such write took a store from 80 KB to "+
			"134 MB and a later count of 600 nodes from 0.01 s to 1.48 s.\nTotal size: %d bytes "+
			"before, %d after.\nServer stderr:\n%s",
			diff, totalBytes(baseline), totalBytes(after), server.capturedStderr(t))
	}

	// And the store a fresh process opens is the seeded graph, unchanged. This is
	// what separates "wrote nothing" from "wrote nothing because there was
	// nothing left to write".
	relaunched := startServerProcess(t, root, "verify")
	if rows := len(mustSend(t, relaunched.socket, "MATCH (n) RETURN n").Rows); rows != residueSeedNodes {
		t.Errorf("a fresh open of the store holds %d nodes, want %d", rows, residueSeedNodes)
	}
	relaunched.signalAndWait(t, syscall.SIGINT)
}

// runExpectingFailure sends one autocommit statement under a client-supplied
// per-statement timeout and returns the FAILURE the server answers with.
//
// It streams past the RUN's own success and the records a PULL may produce,
// because where the failure surfaces depends on when the deadline fires relative
// to the message loop, and this test is about the statement's effect on disk
// rather than about which message carried the refusal.
func runExpectingFailure(t *testing.T, s *boltSession, query string, timeoutMillis int64) *proto.Failure {
	t.Helper()

	send := func(label string, request any) {
		var buf bytes.Buffer
		enc := packstream.NewEncoder(&buf)
		if err := proto.EncodeRequest(enc, request); err != nil {
			t.Fatalf("encoding %s: %v", label, err)
		}
		if err := enc.Flush(); err != nil {
			t.Fatalf("encoding %s: %v", label, err)
		}
		if err := s.writer.WriteMessage(buf.Bytes()); err != nil {
			t.Fatalf("sending %s: %v", label, err)
		}
	}

	send("RUN", &proto.Run{
		Query: query,
		Extra: map[string]packstream.Value{"timeout": timeoutMillis},
	})

	pulled := false
	for {
		msg, err := s.reader.ReadMessage()
		if err != nil {
			t.Fatalf("reading the answer to %q: %v", query, err)
		}
		response, err := proto.DecodeResponse(packstream.NewDecoder(bytes.NewReader(msg)))
		if err != nil {
			t.Fatalf("decoding the answer to %q: %v", query, err)
		}
		switch answer := response.(type) {
		case *proto.Record:
			continue
		case *proto.Failure:
			return answer
		case *proto.Success:
			if pulled {
				t.Fatalf("%q SUCCEEDED. It is a three-way Cartesian product over %d nodes under "+
					"a %dms bound and it must not be able to finish; a statement that completes "+
					"commits, and this whole test is about one that does not",
					query, residueSeedNodes, timeoutMillis)
			}
			pulled = true
			send("PULL", &proto.Pull{N: -1, QID: -1})
		default:
			t.Fatalf("%q was answered %T", query, response)
		}
	}
}

// fingerprintDir reads every regular file under dir and returns a map from the
// path relative to dir to its length and content hash.
//
// Content and not mtime: a fold rewrites the snapshot into a temporary directory
// and renames it into place, so the inode's timestamps change on a rename that
// produced identical bytes, and a test keyed on them would report a fold that did
// not happen. The hash reports one that did.
func fingerprintDir(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		content, readErr := os.ReadFile(path) //nolint:gosec // a path this test walked in its own temporary directory
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(content)
		files[rel] = fmt.Sprintf("%d bytes, sha256 %s", len(content), hex.EncodeToString(sum[:8]))
		return nil
	})
	if err != nil {
		t.Fatalf("reading the graph store at %s: %v", dir, err)
	}
	return files
}

// describeFingerprintDiff reports what changed between two fingerprints, one line
// per file, and returns the empty string when they are identical. Naming the file
// is the whole point: mapper.bin and tombstones.bin are where the residue lands,
// and manifest.json is where a fold that carried no residue still shows.
func describeFingerprintDiff(before, after map[string]string) string {
	var lines []string
	for _, name := range sortedKeys(before) {
		switch got, present := after[name]; {
		case !present:
			lines = append(lines, "  REMOVED "+name+" ("+before[name]+")")
		case got != before[name]:
			lines = append(lines, "  CHANGED "+name+": "+before[name]+" -> "+got)
		}
	}
	for _, name := range sortedKeys(after) {
		if _, present := before[name]; !present {
			lines = append(lines, "  ADDED   "+name+" ("+after[name]+")")
		}
	}
	return strings.Join(lines, "\n")
}

// sortedKeys is the file list in a stable order, so a failure reads the same way
// on every run.
func sortedKeys(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// totalBytes is the store's size on disk, which is the quantity the acceptance
// criterion names. It is reported in the failure beside the per-file diff rather
// than asserted on its own; see the package comment for why the diff is the
// assertion.
func totalBytes(files map[string]string) int64 {
	var total int64
	for _, description := range files {
		var n int64
		if _, err := fmt.Sscanf(description, "%d bytes", &n); err == nil {
			total += n
		}
	}
	return total
}
