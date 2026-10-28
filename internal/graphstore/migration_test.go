// Tests of the backup taken before the engine converts a store from the
// single-file write-ahead-log format (SPEC/GRAPH.md § Graph Store Format
// Migration).
//
// The fixture under testdata/singlefile-store is a real store written by
// `rmp graph serve` built against the previously pinned engine, which kept the
// log in the single-file format: nodes of four labels, relationships of three
// types, an index, a snapshot taken by that server's shutdown fold, and one
// acknowledged write in the single-file log that no snapshot covers — the second
// server that took it was killed before it could fold. It is copied for every
// test, so no test changes it.
package graphstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// backupName is the shape of a backup directory's name: the prefix, then a UTC
// instant in the ISO 8601 basic format (SPEC/GRAPH.md § Graph Store Format
// Migration, rule 3).
var backupName = regexp.MustCompile(`^graph-backup-\d{8}T\d{6}Z$`)

// singleFileFixture is the store written by the previously pinned engine, as
// this package's tests reach it.
var singleFileFixture = filepath.Join("testdata", "singlefile-store")

// copySingleFileFixture copies the single-file fixture into graphDir, which must
// not exist yet, at the modes a real store carries.
func copySingleFileFixture(t *testing.T, graphDir string) {
	t.Helper()
	testenv.CopyTree(t, singleFileFixture, graphDir)
}

// fingerprint is every entry under dir, each as its type, length and content
// digest, keyed by its path relative to dir — the comparison acceptance
// criteria 89 and 91 make.
func fingerprint(t *testing.T, dir string) map[string]string {
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
			content, err := os.ReadFile(path) //nolint:gosec // a path inside this test's temporary directory
			if err != nil {
				return err
			}
			sum := sha256.Sum256(content)
			entries[rel] = fmt.Sprintf("file %d %s", len(content), hex.EncodeToString(sum[:]))
		default:
			target, _ := os.Readlink(path) //nolint:errcheck // a non-link reads as an empty target, which is still a distinct fingerprint
			entries[rel] = fmt.Sprintf("%s -> %s", info.Mode().Type(), target)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("fingerprinting %s: %v", dir, err)
	}
	return entries
}

// withoutLock is a fingerprint less the store lock, the one entry of graph/ a
// backup does not copy and a start may create (rules 4 and 7).
func withoutLock(entries map[string]string) map[string]string {
	out := make(map[string]string, len(entries))
	for k, v := range entries {
		if k != graphlock.LockFileName {
			out[k] = v
		}
	}
	return out
}

// diffFingerprints describes how two fingerprints differ, or returns "".
func diffFingerprints(want, got map[string]string) string {
	var lines []string
	for k, w := range want {
		if g, ok := got[k]; !ok {
			lines = append(lines, "missing "+k)
		} else if g != w {
			lines = append(lines, fmt.Sprintf("changed %s: %s -> %s", k, w, g))
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			lines = append(lines, "extra "+k)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// homeEntries lists the names in the roadmap home that a backup could have left:
// every name starting with the backup prefix, final or staging.
func homeEntries(t *testing.T, home string) []string {
	t.Helper()

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("reading %s: %v", home, err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), BackupDirPrefix) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// newFixtureHome lays out a roadmap home holding the single-file fixture as its
// graph/, and returns the home and the graph directory.
func newFixtureHome(t *testing.T) (home, graphDir string) {
	t.Helper()

	home = filepath.Join(t.TempDir(), "groadmap")
	graphDir = filepath.Join(home, "graph")
	copySingleFileFixture(t, graphDir)
	return home, graphDir
}

// TestNeedsBackup_DetectsTheSingleFileFormat drives every branch of rule 2: what
// counts as the single-file format, what does not, and an inspection that fails.
func TestNeedsBackup_DetectsTheSingleFileFormat(t *testing.T) {
	t.Run("the single-file fixture owes a backup", func(t *testing.T) {
		_, graphDir := newFixtureHome(t)
		if due, err := NeedsBackup(graphDir); err != nil || !due {
			t.Fatalf("NeedsBackup = %v, %v over a store in the single-file format, want true, nil", due, err)
		}
	})

	t.Run("a graph directory nothing has opened owes none", func(t *testing.T) {
		graphDir := filepath.Join(t.TempDir(), "graph")
		if err := os.MkdirAll(graphDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if due, err := NeedsBackup(graphDir); err != nil || due {
			t.Fatalf("NeedsBackup = %v, %v over an empty graph directory, want false, nil", due, err)
		}
	})

	t.Run("a single-file log holding no record is still the single-file format", func(t *testing.T) {
		graphDir := filepath.Join(t.TempDir(), "graph")
		if err := os.MkdirAll(graphDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(graphDir, "wal"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if due, err := NeedsBackup(graphDir); err != nil || !due {
			t.Fatalf("NeedsBackup = %v, %v over an empty single-file log, want true, nil", due, err)
		}
	})

	t.Run("a snapshot an interrupted checkpoint left is the single-file format", func(t *testing.T) {
		graphDir := filepath.Join(t.TempDir(), "graph")
		if err := os.MkdirAll(filepath.Join(graphDir, "snapshot.bak"), 0o700); err != nil {
			t.Fatal(err)
		}
		if due, err := NeedsBackup(graphDir); err != nil || !due {
			t.Fatalf("NeedsBackup = %v, %v over a directory holding snapshot.bak/, want true, nil", due, err)
		}
	})

	t.Run("a converted store owes none", func(t *testing.T) {
		_, graphDir := newFixtureHome(t)
		st, err := Open(graphDir)
		if err != nil {
			t.Fatalf("opening the fixture: %v", err)
		}
		if err := st.Close(); err != nil {
			t.Fatalf("closing the converted store: %v", err)
		}
		if due, err := NeedsBackup(graphDir); err != nil || due {
			t.Fatalf("NeedsBackup = %v, %v over a converted store, want false, nil", due, err)
		}
	})

	t.Run("segments without a control file fail as a store that cannot be opened", func(t *testing.T) {
		graphDir := filepath.Join(t.TempDir(), "graph")
		if err := os.MkdirAll(filepath.Join(graphDir, "wal.d"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(graphDir, "wal.d", "0000000000000001.wal"), []byte("segment"), 0o600); err != nil {
			t.Fatal(err)
		}
		due, err := NeedsBackup(graphDir)
		if due || !errors.Is(err, utils.ErrGraphStore) || errors.Is(err, ErrBackupFailed) ||
			!strings.Contains(err.Error(), "graph store unavailable: ") {
			t.Fatalf("NeedsBackup = %v, %v; want the open-or-recovery failure, not a backup failure "+
				"and not a backup", due, err)
		}
	})
}

// TestTakeBackup_IsACompleteCopyAtTheRequiredModes is acceptance criterion 89's
// copy half, driven directly: the backup holds every entry of graph/ but
// write.lock, byte for byte, every directory at 0700 and every file at 0600, under
// the name the instant gives, and no staging directory remains.
func TestTakeBackup_IsACompleteCopyAtTheRequiredModes(t *testing.T) {
	home, graphDir := newFixtureHome(t)
	// The engine creates some of its own directories wider than 0700; the copy
	// must not carry that further.
	if err := os.Chmod(filepath.Join(graphDir, "snapshot"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(graphDir, graphlock.LockFileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	before := fingerprint(t, graphDir)

	instant := time.Date(2026, time.October, 12, 15, 30, 0, 0, time.FixedZone("WEST", 3600))
	backup, err := TakeBackup(graphDir, instant)
	if err != nil {
		t.Fatalf("TakeBackup: %v", err)
	}

	wantPath := filepath.Join(home, "graph-backup-20261012T143000Z")
	if backup.Path() != wantPath {
		t.Fatalf("the backup is at %s, want %s: the name carries the instant in UTC", backup.Path(), wantPath)
	}
	if !filepath.IsAbs(backup.Path()) {
		t.Errorf("the backup path %q is not absolute", backup.Path())
	}
	if got := homeEntries(t, home); len(got) != 1 || got[0] != filepath.Base(wantPath) || !backupName.MatchString(got[0]) {
		t.Fatalf("the roadmap home holds %v, want exactly the one backup and no staging directory", got)
	}

	if diff := diffFingerprints(withoutLock(before), fingerprint(t, backup.Path())); diff != "" {
		t.Errorf("the backup is not a copy of graph/ less write.lock:\n%s", diff)
	}
	if diff := diffFingerprints(before, fingerprint(t, graphDir)); diff != "" {
		t.Errorf("taking the backup changed graph/:\n%s", diff)
	}

	err = filepath.WalkDir(backup.Path(), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		want := backupFileMode
		if info.IsDir() {
			want = backupDirMode | fs.ModeDir
		}
		if info.Mode() != want {
			t.Errorf("%s has mode %v, want %v", path, info.Mode(), want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the backup: %v", err)
	}
}

// TestTakeBackup_RefusesWhatItMustNotCopy is acceptance criterion 91's copy
// half: an entry of graph/ that is neither a regular file nor a directory fails
// the backup, nothing is followed or copied, the store and the link's target are
// left exactly as they were, and neither a backup nor a staging directory
// remains (rules 4 and 7).
func TestTakeBackup_RefusesWhatItMustNotCopy(t *testing.T) {
	home, graphDir := newFixtureHome(t)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("not part of the store"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(graphDir, "snapshot", "linked")); err != nil {
		t.Fatalf("creating the symbolic link the test needs: %v", err)
	}
	storeBefore, outsideBefore := fingerprint(t, graphDir), fingerprint(t, outside)

	backup, err := TakeBackup(graphDir, time.Now())
	if err == nil || backup != nil {
		t.Fatalf("TakeBackup over a graph directory holding a symbolic link = %v, %v; want a failure", backup, err)
	}
	if !errors.Is(err, ErrBackupFailed) || !errors.Is(err, utils.ErrGraphStore) {
		t.Errorf("the failure %v does not carry the backup class and the store sentinel", err)
	}
	if !strings.HasPrefix(err.Error(), "graph store error: graph store backup failed; the store was not migrated: ") {
		t.Errorf("the failure reads %q, want the published backup line's text", err.Error())
	}
	if got := homeEntries(t, home); len(got) != 0 {
		t.Errorf("a failed backup left %v in the roadmap home", got)
	}
	if diff := diffFingerprints(storeBefore, fingerprint(t, graphDir)); diff != "" {
		t.Errorf("a failed backup changed graph/:\n%s", diff)
	}
	if diff := diffFingerprints(outsideBefore, fingerprint(t, outside)); diff != "" {
		t.Errorf("a failed backup changed the link's target:\n%s", diff)
	}
}

// TestTakeBackup_NeverOverwritesAnEntryAtEitherName is rule 6: an entry already
// at the staging name or at the final name fails the backup, and that entry is
// left exactly as it was.
func TestTakeBackup_NeverOverwritesAnEntryAtEitherName(t *testing.T) {
	instant := time.Date(2026, time.October, 12, 15, 30, 0, 0, time.UTC)
	for _, name := range []string{"graph-backup-20261012T153000Z", "graph-backup-20261012T153000Z.partial"} {
		t.Run(name, func(t *testing.T) {
			home, graphDir := newFixtureHome(t)
			occupied := filepath.Join(home, name)
			if err := os.MkdirAll(occupied, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(occupied, "operator-notes.txt"), []byte("kept by hand"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := fingerprint(t, occupied)

			if _, err := TakeBackup(graphDir, instant); !errors.Is(err, ErrBackupFailed) {
				t.Fatalf("TakeBackup with %s taken = %v, want the backup failure", name, err)
			}
			if diff := diffFingerprints(before, fingerprint(t, occupied)); diff != "" {
				t.Errorf("the entry already at %s was changed:\n%s", name, diff)
			}
			if got := homeEntries(t, home); len(got) != 1 || got[0] != name {
				t.Errorf("the roadmap home holds %v, want only the entry that was already there", got)
			}
		})
	}
}

// TestBackupRemove_LeavesNothingAndReportsWhatRemains is rule 9's removal: a
// removal that completes leaves neither the backup nor a staging directory, and
// one that cannot rename names the backup that remains.
func TestBackupRemove_LeavesNothingAndReportsWhatRemains(t *testing.T) {
	t.Run("a removal that completes", func(t *testing.T) {
		home, graphDir := newFixtureHome(t)
		backup, err := TakeBackup(graphDir, time.Now())
		if err != nil {
			t.Fatalf("TakeBackup: %v", err)
		}
		if remaining, err := backup.Remove(); err != nil || remaining != "" {
			t.Fatalf("Remove = %q, %v; want a complete removal", remaining, err)
		}
		if got := homeEntries(t, home); len(got) != 0 {
			t.Errorf("the roadmap home still holds %v after the removal", got)
		}
	})

	t.Run("a removal that cannot rename names the backup", func(t *testing.T) {
		home, graphDir := newFixtureHome(t)
		backup, err := TakeBackup(graphDir, time.Now())
		if err != nil {
			t.Fatalf("TakeBackup: %v", err)
		}
		blocker := backup.Path() + BackupPartialSuffix
		if err := os.MkdirAll(filepath.Join(blocker, "kept"), 0o700); err != nil {
			t.Fatal(err)
		}
		remaining, err := backup.Remove()
		if err == nil || remaining != backup.Path() {
			t.Fatalf("Remove = %q, %v; want a failure naming %s", remaining, err, backup.Path())
		}
		if got := homeEntries(t, home); len(got) != 2 {
			t.Errorf("the roadmap home holds %v, want the backup and the entry that blocked its removal", got)
		}
	})
}

// TestOpen_ConvertsTheSingleFileFixtureAndKeepsItsContent is the engine half of
// acceptance criterion 88, at the lifecycle this package owns: the open converts
// the store, the log then has a control file, and the graph holds the nodes the
// fixture's snapshot and its unfolded single-file log carried.
func TestOpen_ConvertsTheSingleFileFixtureAndKeepsItsContent(t *testing.T) {
	_, graphDir := newFixtureHome(t)

	st := openTestStore(t, graphDir)
	if converted, err := Converted(graphDir); err != nil || !converted {
		t.Fatalf("Converted = %v, %v after the open; the engine converts the store as it opens it", converted, err)
	}

	result, err := st.Engine().RunAny(t.Context(), "MATCH (m:Memory) RETURN m.key", nil)
	if err != nil {
		t.Fatalf("reading the converted store: %v", err)
	}
	var keys []string
	for result.Next() {
		keys = append(keys, result.ValueAt(0).String())
	}
	if err := result.Close(); err != nil {
		t.Fatalf("closing the read: %v", err)
	}
	if len(keys) != 1 || !strings.Contains(keys[0], "acknowledged-after-the-fold") {
		t.Errorf("the converted store holds Memory keys %v, want the acknowledged write the single-file "+
			"log held and no snapshot covered", keys)
	}
}
