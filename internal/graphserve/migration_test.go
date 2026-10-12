// Package graphserve — the store format migration as `rmp graph serve` performs
// it: SPEC/GRAPH.md § Graph Store Format Migration and acceptance criteria 88 to
// 93.
//
// Every test here starts from a copy of a real store written by `rmp graph serve`
// built against the engine that kept the write-ahead log in the single-file format
// (see testenv.SingleFileStoreFixture), and all but the last run the production
// entry point in a child process, because what is under test is the start as a
// whole: which records reach stderr before the socket is announced on stdout, the
// exit code of a start that fails, and what the roadmap home holds afterwards.
package graphserve

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/GoGraph/store/wal"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// backupDirName is the shape of a backup directory's name: the prefix, then a
// UTC instant in the ISO 8601 basic format (rule 3).
var backupDirName = regexp.MustCompile(`^graph-backup-(\d{8}T\d{6}Z)$`)

// backupRecord is one record of the server's stderr that names a backup: its
// level, its message and the absolute path it names.
var backupRecord = regexp.MustCompile(`level=(INFO|WARN) msg="([^"]*)" backup=(\S+)`)

// singleFileRoot lays out a test root whose graph/ is a copy of the single-file
// fixture: the roadmap home a server started over it backs up into.
func singleFileRoot(t *testing.T) (root, graphDir string) {
	t.Helper()

	root = graphRoot(t)
	graphDir = filepath.Join(root, "graph")
	if err := os.RemoveAll(graphDir); err != nil {
		t.Fatal(err)
	}
	testenv.CopyTree(t, filepath.Join("..", "..", testenv.SingleFileStoreFixture), graphDir)
	return root, graphDir
}

// backupEntries lists every name in root that a backup could have left, final or
// staging.
func backupEntries(t *testing.T, root string) []string {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), graphstore.BackupDirPrefix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// backupRecords returns every record in stderr that names a backup, as
// level, message and path.
func backupRecords(stderr string) [][3]string {
	matches := backupRecord.FindAllStringSubmatch(stderr, -1)
	out := make([][3]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, [3]string{m[1], m[2], m[3]})
	}
	return out
}

// treeFingerprint is every entry under dir — its type, and a regular file's
// length and content digest — keyed by its path relative to dir.
func treeFingerprint(t *testing.T, dir string) map[string]string {
	t.Helper()

	entries := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			entries[rel] = "dir"
		case info.Mode().IsRegular():
			content, err := os.ReadFile(path) //nolint:gosec // a path inside this test's own temporary directory
			if err != nil {
				return err
			}
			sum := sha256.Sum256(content)
			entries[rel] = fmt.Sprintf("%d bytes, sha256 %s", len(content), hex.EncodeToString(sum[:]))
		default:
			target, _ := os.Readlink(path) //nolint:errcheck // a non-link reads as an empty target, still a distinct fingerprint
			entries[rel] = info.Mode().Type().String() + " -> " + target
		}
		return nil
	})
	if err != nil {
		t.Fatalf("fingerprinting %s: %v", dir, err)
	}
	return entries
}

// lessLock is a fingerprint without the store lock, the one entry of graph/ a
// backup does not copy and a start may create (rules 4 and 7).
func lessLock(entries map[string]string) map[string]string {
	out := make(map[string]string, len(entries))
	for k, v := range entries {
		if k != graphlock.LockFileName {
			out[k] = v
		}
	}
	return out
}

// waitForExit waits for a start that must fail and returns its exit code.
func waitForExit(t *testing.T, p *serverProcess) int {
	t.Helper()

	err := p.waitWithin(serverStartDeadline)
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the child server did not exit on its own: %v\nstderr:\n%s", err, p.capturedStderr(t))
	}
	return exitErr.ExitCode()
}

// TestServe_BacksUpConvertsAndRemovesTheBackup is acceptance criterion 88: a
// start over a store in the single-file format takes a backup, the engine
// converts the store, and the backup is removed once the conversion has
// succeeded — and the records saying so reach stderr before the socket
// announcement reaches stdout.
func TestServe_BacksUpConvertsAndRemovesTheBackup(t *testing.T) {
	root, graphDir := singleFileRoot(t)
	started := time.Now().UTC().Truncate(time.Second)

	p := launchServerProcess(t, root, "convert")
	// The first moment the announcement is on stdout, stderr must already carry
	// both records: the server flushes its queue before it announces (rule 10).
	deadline := time.Now().Add(serverStartDeadline)
	var stderrAtAnnouncement string
	for {
		if out, err := os.ReadFile(p.stdout); err == nil && len(out) > 0 {
			stderrAtAnnouncement = p.capturedStderr(t)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server announced nothing within %s.\nstderr:\n%s", serverStartDeadline, p.capturedStderr(t))
		}
		time.Sleep(time.Millisecond)
	}
	p.waitUntilServed(t)
	p.requireAnnounced(t)

	records := backupRecords(stderrAtAnnouncement)
	if len(records) != 2 {
		t.Fatalf("stderr carried %d backup record(s) when the socket was announced, want 2 (taken, "+
			"then removed):\n%s", len(records), stderrAtAnnouncement)
	}
	taken, removed := records[0], records[1]
	if taken[0] != "INFO" || !strings.Contains(taken[1], "backed up") {
		t.Errorf("the first record is %v, want the INFO record of the backup taken", taken)
	}
	if removed[0] != "INFO" || !strings.Contains(removed[1], "removed") || removed[2] != taken[2] {
		t.Errorf("the second record is %v, want the INFO record naming %s as removed", removed, taken[2])
	}
	if filepath.Dir(taken[2]) != root || !filepath.IsAbs(taken[2]) {
		t.Errorf("the backup %s is not an absolute path in the roadmap home %s", taken[2], root)
	}
	m := backupDirName.FindStringSubmatch(filepath.Base(taken[2]))
	if m == nil {
		t.Fatalf("the backup's name %q is not graph-backup-YYYYMMDDTHHMMSSZ", filepath.Base(taken[2]))
	}
	instant, err := time.Parse(graphstore.BackupInstantLayout, m[1])
	if err != nil || instant.Before(started) {
		t.Errorf("the backup is named for %v (%v), want a UTC instant no earlier than the start, %v",
			instant, err, started)
	}

	result := mustSend(t, p.socket, "MATCH (n) RETURN count(n)")
	if len(result.Rows) != 1 || result.Rows[0][0].String() != "6" {
		t.Errorf("the converted store counts %v nodes, want the 6 the fixture holds, the acknowledged "+
			"write its single-file log carried among them", result.Rows)
	}
	if keys := stringColumn(t, mustSend(t, p.socket, "MATCH (m:Memory) RETURN m.key")); len(keys) != 1 ||
		keys[0] != "acknowledged-after-the-fold" {
		t.Errorf("the converted store holds Memory keys %v, want the acknowledged write", keys)
	}
	p.signalAndWait(t, syscall.SIGINT)

	if got := backupEntries(t, root); len(got) != 0 {
		t.Errorf("the roadmap home holds %v after the start, want no backup and no staging directory", got)
	}
	if !testenv.InspectWAL(t, graphDir).HasControl {
		t.Errorf("graph/ carries no write-ahead-log control file after the start; the store was not converted")
	}
}

// corruptMiddleFrame flips the first payload byte of the frame in the middle of
// the single-file log at path, so that its checksum no longer matches and the
// frames after it are behind genuine corruption.
func corruptMiddleFrame(t *testing.T, path string) {
	t.Helper()

	content, err := os.ReadFile(path) //nolint:gosec // a copy of a fixture in this test's directory
	if err != nil {
		t.Fatal(err)
	}
	// The single-file format's frame: magic (4), version (2), payload length
	// (4, little-endian), checksum (4), payload.
	const header = 14
	var starts []int
	for off := 0; off+header <= len(content); {
		length := int(binary.LittleEndian.Uint32(content[off+6 : off+10]))
		starts = append(starts, off)
		off += header + length
	}
	if len(starts) < 3 {
		t.Fatalf("the fixture's single-file log holds %d frames; a frame in the middle needs at least 3", len(starts))
	}
	middle := starts[len(starts)/2]
	content[middle+header] ^= 0xFF
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestServe_KeepsACompleteBackupWhenTheOpenFails is acceptance criterion 89: a
// store the engine refuses to open is backed up first, completely and at the
// required modes, and the backup is kept and its path reported.
func TestServe_KeepsACompleteBackupWhenTheOpenFails(t *testing.T) {
	root, graphDir := singleFileRoot(t)
	corruptMiddleFrame(t, filepath.Join(graphDir, "wal"))
	before := treeFingerprint(t, graphDir)

	p := launchServerProcess(t, root, "corrupt")
	if code := waitForExit(t, p); code != 1 {
		t.Fatalf("the start over a corrupt store exited %d, want 1.\nstderr:\n%s", code, p.capturedStderr(t))
	}
	stderr := p.capturedStderr(t)
	if out := readFile(t, p.stdout); len(out) != 0 {
		t.Errorf("a failed start wrote %q to stdout", out)
	}
	if !strings.Contains(stderr, "graph store error: graph store unavailable: ") {
		t.Errorf("stderr does not carry the open-or-recovery line:\n%s", stderr)
	}

	records := backupRecords(stderr)
	var kept string
	for _, r := range records {
		if r[0] == "WARN" {
			kept = r[2]
		}
	}
	if kept == "" {
		t.Fatalf("no WARN record names the backup the failed start kept:\n%s", stderr)
	}
	if idx := strings.Index(stderr, "graph store unavailable"); idx >= 0 && strings.Index(stderr, kept) > idx {
		t.Errorf("the WARN record naming the kept backup follows the error line; it must precede it:\n%s", stderr)
	}
	if got := backupEntries(t, root); len(got) != 1 || filepath.Join(root, got[0]) != kept ||
		!backupDirName.MatchString(got[0]) {
		t.Fatalf("the roadmap home holds %v, want exactly the kept backup %s and no staging directory", got, kept)
	}

	if diff := describeFingerprintDiff(lessLock(before), treeFingerprint(t, kept)); diff != "" {
		t.Errorf("the kept backup is not a copy of graph/ as the server found it, write.lock aside:\n%s", diff)
	}
	if _, ok := treeFingerprint(t, kept)[graphlock.LockFileName]; ok {
		t.Errorf("the backup holds %s, which it must not copy", graphlock.LockFileName)
	}
	err := filepath.WalkDir(kept, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700 | fs.ModeDir
		}
		if info.Mode() != want {
			t.Errorf("%s has mode %v, want %v", path, info.Mode(), want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the kept backup: %v", err)
	}
}

// TestServe_TakesNoBackupWhenNoneIsDue is acceptance criterion 90: a roadmap with
// no graph, and a store already converted, are started without a backup.
func TestServe_TakesNoBackupWhenNoneIsDue(t *testing.T) {
	t.Run("a roadmap with no graph", func(t *testing.T) {
		root := graphRoot(t)
		p := startServerProcess(t, root, "fresh")
		p.signalAndWait(t, syscall.SIGINT)
		if got := backupEntries(t, root); len(got) != 0 {
			t.Errorf("the roadmap home holds %v after starting a server for a roadmap with no graph", got)
		}
		if records := backupRecords(p.capturedStderr(t)); len(records) != 0 {
			t.Errorf("stderr carries backup records %v for a start that owed no backup", records)
		}
	})

	t.Run("a second start of a converted store", func(t *testing.T) {
		root, _ := singleFileRoot(t)
		first := startServerProcess(t, root, "convert")
		first.signalAndWait(t, syscall.SIGINT)

		second := startServerProcess(t, root, "again")
		second.signalAndWait(t, syscall.SIGINT)
		if got := backupEntries(t, root); len(got) != 0 {
			t.Errorf("the roadmap home holds %v after a second start of a converted store", got)
		}
		if records := backupRecords(second.capturedStderr(t)); len(records) != 0 {
			t.Errorf("the second start's stderr carries backup records %v; it owed no backup", records)
		}
	})
}

// TestServe_AFailedBackupLeavesTheStoreUnconverted is acceptance criterion 91: a
// symbolic link inside graph/ fails the backup, the server does not start, and
// the store, the link's target and the roadmap home are exactly as they were.
func TestServe_AFailedBackupLeavesTheStoreUnconverted(t *testing.T) {
	root, graphDir := singleFileRoot(t)
	outside := filepath.Join(root, "operator-files")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "release-checklist.md"), []byte("verify the backup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(graphDir, "operator-files")); err != nil {
		t.Fatalf("creating the symbolic link the criterion needs: %v", err)
	}
	storeBefore, outsideBefore := treeFingerprint(t, graphDir), treeFingerprint(t, outside)

	p := launchServerProcess(t, root, "refused")
	if code := waitForExit(t, p); code != 1 {
		t.Fatalf("the start exited %d, want 1.\nstderr:\n%s", code, p.capturedStderr(t))
	}
	if out := readFile(t, p.stdout); len(out) != 0 {
		t.Errorf("a refused start wrote %q to stdout", out)
	}
	if stderr := p.capturedStderr(t); !strings.Contains(stderr,
		"graph store error: graph store backup failed; the store was not migrated: ") {
		t.Errorf("stderr does not carry the published backup line:\n%s", stderr)
	}
	if diff := describeFingerprintDiff(lessLock(storeBefore), lessLock(treeFingerprint(t, graphDir))); diff != "" {
		t.Errorf("graph/ changed across a backup that failed, write.lock aside:\n%s", diff)
	}
	if _, err := os.Lstat(filepath.Join(graphDir, "wal.control")); !os.IsNotExist(err) {
		t.Errorf("graph/ holds a control file (%v): the store was converted although its backup failed", err)
	}
	if got := backupEntries(t, root); len(got) != 0 {
		t.Errorf("the roadmap home holds %v after a failed backup", got)
	}
	if diff := describeFingerprintDiff(outsideBefore, treeFingerprint(t, outside)); diff != "" {
		t.Errorf("the link's target changed:\n%s", diff)
	}
}

// TestServe_NeverTouchesABackupAnEarlierStartLeft is acceptance criterion 92.
func TestServe_NeverTouchesABackupAnEarlierStartLeft(t *testing.T) {
	root, _ := singleFileRoot(t)
	earlier := filepath.Join(root, "graph-backup-20000101T000000Z")
	testenv.CopyTree(t, filepath.Join("..", "..", testenv.SingleFileStoreFixture), earlier)
	before := treeFingerprint(t, earlier)

	p := startServerProcess(t, root, "convert")
	p.signalAndWait(t, syscall.SIGINT)

	records := backupRecords(p.capturedStderr(t))
	if len(records) != 2 || records[0][2] == earlier || records[1][2] != records[0][2] {
		t.Errorf("the start's backup records are %v; want one backup of its own, taken and removed", records)
	}
	if got := backupEntries(t, root); len(got) != 1 || got[0] != filepath.Base(earlier) {
		t.Errorf("the roadmap home holds %v, want only the earlier backup", got)
	}
	if diff := describeFingerprintDiff(before, treeFingerprint(t, earlier)); diff != "" {
		t.Errorf("the earlier backup changed:\n%s", diff)
	}
}

// TestServe_FoldsConvertedHistoryWhileIdle is acceptance criterion 93: history
// the single-file log held at the conversion is folded at the first due instant
// of a server that receives no statement, and the single-file log is replaced by
// the engine's one-record stub.
func TestServe_FoldsConvertedHistoryWhileIdle(t *testing.T) {
	root, graphDir := singleFileRoot(t)
	manifestBefore := fingerprintDir(t, filepath.Join(graphDir, "snapshot"))["manifest.json"]

	served := startRealServerOver(t, graphDir, filepath.Join(root, "idle.sock"), testCadence(), discardLogger())
	defer served.stop()

	deadline := time.Now().Add(30 * time.Second)
	for served.closer.inFlight.attempts.Load() < 1 ||
		testenv.InspectWAL(t, graphDir).Control.Flags&wal.ControlLegacyV1Pending != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no in-flight fold folded the converted history within 30s: %d fold(s), %d due "+
				"instant(s), control flags %#x", served.closer.inFlight.attempts.Load(),
				served.closer.inFlight.due.Load(), testenv.InspectWAL(t, graphDir).Control.Flags)
		}
		time.Sleep(intervalFloor)
	}

	if after := fingerprintDir(t, filepath.Join(graphDir, "snapshot"))["manifest.json"]; after == manifestBefore {
		t.Errorf("snapshot/manifest.json is unchanged after the fold; snapshot/ was not rewritten")
	}
	// The file alone, through the engine's frame reader: wal.OpenReader on this
	// path would read the store's whole segmented log instead.
	file, err := os.Open(filepath.Join(graphDir, "wal"))
	if err != nil {
		t.Fatalf("reading the single-file log: %v", err)
	}
	reader := wal.NewReader(file, file)
	var frames, seals int
	for f := range reader.Frames() {
		frames++
		if _, ok := wal.DecodeLegacySeal(f.Payload); ok {
			seals++
		}
	}
	_ = reader.Close() //nolint:errcheck // a read-only reader
	if frames != 1 || seals != 1 {
		t.Errorf("the single-file log holds %d record(s), %d of them a seal; want the engine's "+
			"one-record stub", frames, seals)
	}

	keys := stringColumn(t, mustSend(t, served.socket, "MATCH (m:Memory) RETURN m.key"))
	if len(keys) != 1 || keys[0] != "acknowledged-after-the-fold" {
		t.Errorf("after the fold the graph holds Memory keys %v, want the acknowledged write", keys)
	}
}
