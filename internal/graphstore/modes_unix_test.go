//go:build !windows

package graphstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file pins the repair pass of SPEC/GRAPH.md § Persistence Layout, rule 4:
// the walk that brings graph/ and every entry below it to 0700 and 0600 before
// the store is opened (rmp task #603). Acceptance criterion 95 drives it through
// `rmp graph serve` in internal/commands; the tests here pin the properties of
// the walk that criterion does not reach — links, non-regular entries, the
// special bits, and the failure.

// modeOf returns the permission and special bits of the entry at path, without
// following a link.
func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("reading the mode of %s: %v", path, err)
	}
	return info.Mode() & modeBits
}

// widerStore lays out a graph directory whose entries are all wider than their
// modes, at every depth, the graph directory itself included.
func widerStore(t *testing.T) (graphDir string, dirs, files []string) {
	t.Helper()

	graphDir = filepath.Join(t.TempDir(), "graph")
	dirs = []string{
		graphDir,
		filepath.Join(graphDir, "snapshot"),
		filepath.Join(graphDir, "snapshot", "indexes"),
		filepath.Join(graphDir, "wal.d"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files = []string{
		filepath.Join(graphDir, "write.lock"),
		filepath.Join(graphDir, "snapshot", "manifest.json"),
		filepath.Join(graphDir, "snapshot", "indexes", "spec_key.bin"),
		filepath.Join(graphDir, "wal.d", "0000000000000001.wal"),
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("store entry"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Widened explicitly, so the layout does not depend on the test's umask.
	// The innermost directory is made unreadable, so the walk must repair a
	// directory before it lists it.
	wider := map[string]os.FileMode{
		dirs[0]: 0o755, dirs[1]: 0o750, dirs[2]: 0o000, dirs[3]: 0o2770,
		files[0]: 0o644, files[1]: 0o640, files[2]: 0o666, files[3]: 0o4600,
	}
	// Files first: a directory at 0000 would refuse the chmod of its entries.
	for _, p := range append(append([]string{}, files...), dirs[3], dirs[1], dirs[0], dirs[2]) {
		if err := os.Chmod(p, wider[p]); err != nil {
			t.Fatalf("widening %s: %v", p, err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(dirs[2], 0o700) }) //nolint:errcheck // lets TempDir remove a directory a failing run left at 0000
	return graphDir, dirs, files
}

// TestSecureModes_BringsEveryEntryToItsMode is the repair itself: every
// directory, the graph directory included, at 0700 and every regular file at
// 0600, at every depth, with the special bits cleared.
func TestSecureModes_BringsEveryEntryToItsMode(t *testing.T) {
	graphDir, dirs, files := widerStore(t)

	if err := SecureModes(graphDir); err != nil {
		t.Fatalf("SecureModes: %v", err)
	}
	for _, d := range dirs {
		if got := modeOf(t, d); got != storeDirMode {
			t.Errorf("directory %s is at %#o after the repair, want %#o", d, got, storeDirMode)
		}
	}
	for _, f := range files {
		if got := modeOf(t, f); got != storeFileMode {
			t.Errorf("file %s is at %#o after the repair, want %#o", f, got, storeFileMode)
		}
	}
}

// TestSecureModes_ChangesNothingAlreadyAtItsMode pins the first bullet of the
// rule: the mode is read first, and an entry already at its mode is not
// changed. A chmod that fails stands in for "changed": the walk over a private
// store must not reach it at all.
func TestSecureModes_ChangesNothingAlreadyAtItsMode(t *testing.T) {
	graphDir, _, _ := widerStore(t)
	if err := SecureModes(graphDir); err != nil {
		t.Fatalf("the first repair: %v", err)
	}

	calls := 0
	defer func(previous func(string, os.FileMode) error) { chmod = previous }(chmod)
	chmod = func(string, os.FileMode) error {
		calls++
		return syscall.EPERM
	}
	if err := SecureModes(graphDir); err != nil {
		t.Fatalf("a repair over a store already at its modes failed: %v", err)
	}
	if calls != 0 {
		t.Errorf("a repair over a store already at its modes changed %d entries, want none", calls)
	}
}

// TestSecureModes_NeverFollowsALinkAndLeavesNonRegularEntries pins the second
// bullet: a symbolic link is neither followed nor changed, whether it names a
// file or a directory outside the store, and an entry that is neither a
// directory nor a regular file is left as it is without failing the walk.
func TestSecureModes_NeverFollowsALinkAndLeavesNonRegularEntries(t *testing.T) {
	graphDir, _, _ := widerStore(t)

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "operator-notes.txt")
	if err := os.WriteFile(outsideFile, []byte("not the store's"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideDir := filepath.Join(outside, "exports")
	if err := os.Mkdir(outsideDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outsideInner := filepath.Join(outsideDir, "graph.cypher")
	if err := os.WriteFile(outsideInner, []byte("not the store's"), 0o600); err != nil {
		t.Fatal(err)
	}
	for p, m := range map[string]os.FileMode{outsideFile: 0o644, outsideDir: 0o755, outsideInner: 0o644} {
		if err := os.Chmod(p, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outsideFile, filepath.Join(graphDir, "snapshot", "linked-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(graphDir, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(graphDir, "wal.d", "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatalf("creating the FIFO: %v", err)
	}
	if err := os.Chmod(fifo, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SecureModes(graphDir); err != nil {
		t.Fatalf("a store holding links and a FIFO failed the repair: %v; such an entry must not "+
			"fail the walk", err)
	}
	for p, want := range map[string]os.FileMode{outsideFile: 0o644, outsideDir: 0o755, outsideInner: 0o644} {
		if got := modeOf(t, p); got != want {
			t.Errorf("%s, outside the store and reached only through a link, is at %#o, want the "+
				"%#o it had: the walk followed a link", p, got, want)
		}
	}
	if got := modeOf(t, fifo); got != 0o644 {
		t.Errorf("the FIFO is at %#o, want the 0644 it had: an entry that is neither a directory "+
			"nor a regular file is left as it is", got)
	}
	if got := modeOf(t, filepath.Join(graphDir, "snapshot")); got != storeDirMode {
		t.Errorf("snapshot/ is at %#o, want %#o: the links must not stop the repair of the rest", got, storeDirMode)
	}
}

// TestSecureModes_AnEntryThatCannotBeMadePrivateFailsAsTheStore pins the third
// bullet: an entry whose mode cannot be changed, and one still not at its mode
// after the change, both fail the walk as a store that cannot be opened —
// utils.ErrGraphStore with the open-or-recovery line.
func TestSecureModes_AnEntryThatCannotBeMadePrivateFailsAsTheStore(t *testing.T) {
	cases := []struct {
		name  string
		chmod func(string, os.FileMode) error
		want  string
	}{
		{"the change is refused", func(string, os.FileMode) error { return syscall.EPERM }, "operation not permitted"},
		{"the change does not take effect", func(string, os.FileMode) error { return nil }, errModeNotApplied.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			graphDir, _, _ := widerStore(t)
			defer func(previous func(string, os.FileMode) error) { chmod = previous }(chmod)
			chmod = tc.chmod

			err := SecureModes(graphDir)
			if !errors.Is(err, utils.ErrGraphStore) {
				t.Fatalf("err = %v, want it to wrap utils.ErrGraphStore (exit code 1)", err)
			}
			const line = "graph store error: graph store unavailable: "
			if !strings.HasPrefix(err.Error(), line) {
				t.Errorf("err = %q, want the open-or-recovery line, which begins %q", err, line)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want it to carry the cause %q", err, tc.want)
			}
		})
	}
}
