//go:build !windows

package graphstore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The modes every entry of the graph store carries (SPEC/GRAPH.md § Persistence
// Layout, rule 4): the graph directory and every directory below it are
// owner-only and traversable, every regular file below it owner-only.
const (
	storeDirMode  os.FileMode = 0o700
	storeFileMode os.FileMode = 0o600
)

// modeBits are the bits a mode comparison reads: the permission bits and the
// three special bits. A directory at 02700 is not at 0700, and os.Chmod with
// storeDirMode clears the special bits as it sets the permission bits.
const modeBits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// SecureModes brings dir, the graph store directory, and every entry below it
// to the modes SPEC/GRAPH.md § Persistence Layout, rule 4, requires: every
// directory to 0700 and every regular file to 0600. It is the repair that begins
// step 6 of § Server Startup, and the caller MUST hold the store's exclusive
// lock: the lock is what guarantees that no other process creates, renames or
// removes an entry of dir while the walk runs.
//
// # What the walk does, entry by entry
//
//   - It reads the entry's mode with Lstat first and changes nothing on an entry
//     already at its mode, so a store that is already private is only read.
//   - It never follows a symbolic link. filepath.WalkDir reports a link as a
//     link and does not descend through it, and a mode is changed only on an
//     entry Lstat reports as a directory or a regular file, reached through
//     directories the walk itself has read. An entry that is neither — a link,
//     a socket, a device — is left exactly as it is and does not fail the walk:
//     what such an entry means to the backup and to the open is decided where
//     those are specified.
//   - It reads the mode again after a change, and an entry whose mode cannot be
//     changed, that is no longer the entry it changed, or that is still not at
//     its mode, fails the walk.
//
// A directory is brought to its mode BEFORE its entries are read, so a
// directory the walk cannot list at its present mode is listed at 0700.
//
// # Failure
//
// Any failure is a store that cannot be opened: utils.ErrGraphStore, reported
// with the open-or-recovery line of SPEC/COMMANDS.md § Serve Error Cases. A store
// whose entries cannot be made private is not served.
func SecureModes(dir string) error {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		switch {
		case d.IsDir():
			return secureEntry(path, storeDirMode, fs.FileInfo.IsDir)
		case d.Type().IsRegular():
			return secureEntry(path, storeFileMode, func(info fs.FileInfo) bool { return info.Mode().IsRegular() })
		default:
			return nil
		}
	})
	if err != nil {
		return fmt.Errorf("%w: graph store unavailable: cannot bring the graph store to owner-only modes: %v",
			utils.ErrGraphStore, err)
	}
	return nil
}

// chmod changes an entry's mode. It is a variable so a test can make a change
// fail, which no unprivileged test can arrange on an entry it owns; production
// never reassigns it.
var chmod = os.Chmod

// errModeNotApplied is the failure of an entry whose mode was changed without an
// error and that still does not carry the mode afterwards.
var errModeNotApplied = errors.New("the mode did not take effect")

// errEntryReplaced is the failure of an entry that is not, after the change, the
// entry whose mode was read before it.
var errEntryReplaced = errors.New("the entry changed while its mode was being set")

// secureEntry brings the entry at path to want when it is not already there.
// isKind reports whether a FileInfo is still the kind of entry the walk found,
// so a change applied to anything else is detected rather than trusted.
func secureEntry(path string, want os.FileMode, isKind func(fs.FileInfo) bool) error {
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !isKind(before) {
		// The walk saw a directory or a regular file and Lstat now sees
		// something else: under the lock nothing may replace it, so this is
		// not an entry the walk may touch.
		return fmt.Errorf("%s: %w", path, errEntryReplaced)
	}
	if before.Mode()&modeBits == want {
		return nil
	}
	if err := chmod(path, want); err != nil {
		return err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !isKind(after) || !os.SameFile(before, after) {
		return fmt.Errorf("%s: %w", path, errEntryReplaced)
	}
	if got := after.Mode() & modeBits; got != want {
		return fmt.Errorf("%s is at mode %#o after it was set to %#o: %w", path, got, want, errModeNotApplied)
	}
	return nil
}
