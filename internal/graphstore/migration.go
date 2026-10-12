// Package graphstore — the backup taken before the engine converts a store from
// the single-file write-ahead-log format (SPEC/GRAPH.md § Graph Store Format
// Migration).
//
// The pinned engine keeps the write-ahead log in the segmented format and
// converts a store written in the earlier single-file format one way, the first
// time it opens that store for writing. Groadmap converts nothing. What this file
// adds is the backup: detecting a store still in the single-file format, copying
// it beside the graph directory before anything opens it, and removing that copy
// once the conversion has succeeded.
//
// # Why it is here
//
// The backup is a step of the store's lifecycle — it runs inside the store's
// exclusive hold, after the hold is taken and before the open — and this package
// owns that lifecycle (SPEC/ARCHITECTURE.md, module 8). It is a unit of its own
// inside the package because its job is a different one from the open: it reads
// and writes the roadmap home beside graph/, and it never touches the engine.
// The records that tell the operator what became of a backup are the server's,
// so this file reports outcomes and writes no diagnostic of its own.
package graphstore

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// BackupDirPrefix starts the name of every backup directory in a roadmap home.
// The rest of the name is the UTC instant of the copy (SPEC/GRAPH.md § Graph
// Store Format Migration, rule 3).
const BackupDirPrefix = "graph-backup-"

// BackupInstantLayout is the instant's form inside a backup's name: ISO 8601
// basic format, UTC, to the second. The extended format's colon is not a
// portable character in a file name, so the basic one is used (rule 3).
const BackupInstantLayout = "20060102T150405Z"

// BackupPartialSuffix ends the name of a staging directory: an incomplete copy,
// or a backup whose removal did not finish. A directory carrying it is never a
// backup (rules 5 and 9).
const BackupPartialSuffix = ".partial"

// backupDirMode and backupFileMode are the modes of every directory and every
// file a backup holds, whatever the mode of the entry each copies (rule 4).
const (
	backupDirMode  os.FileMode = 0o700
	backupFileMode os.FileMode = 0o600
)

// ErrBackupFailed classifies a backup that could not be completed. It wraps
// utils.ErrGraphStore, because the failure is the store's: the server declined
// to open it (rule 7). Its text is the published half of the line
// SPEC/COMMANDS.md § Serve Error Cases fixes; the detail follows it.
var ErrBackupFailed = fmt.Errorf("%w: graph store backup failed; the store was not migrated", utils.ErrGraphStore)

// errUnsupportedEntry is the detail of a backup refused because graph/ holds an
// entry that is neither a regular file nor a directory.
var errUnsupportedEntry = errors.New("is neither a regular file nor a directory, and a backup copies nothing else")

// errEntryExists is the detail of a backup refused because its staging name or
// its final name is already taken.
var errEntryExists = errors.New("already exists, and an earlier backup is never overwritten")

// errEntryChanged is the detail of a copy whose source was replaced between
// the check of its type and its open.
var errEntryChanged = errors.New("changed while it was being copied")

// legacyStoreEntries are the entries of graph/ whose presence, beside a log with
// no control file, makes a directory a store in the single-file format: the
// single-file log, the snapshot directory, and the form an interrupted
// checkpoint leaves (rule 2).
var legacyStoreEntries = []string{walFileName, snapshotDirName, snapshotDirName + ".bak"}

// NeedsBackup reports whether the store in graphDir is in the single-file
// format and therefore owes a backup before it is opened (SPEC/GRAPH.md § Graph
// Store Format Migration, rule 2).
//
// It holds when the log has no control file, as the engine's inspection reports
// it, and graph/ holds the single-file log or a snapshot directory. A log with a
// control file has been converted already; a directory with neither holds no
// store yet. An inspection that fails is returned as a store that cannot be
// opened, classified utils.ErrGraphStore with the open-or-recovery wording,
// because the open would refuse the same directory.
func NeedsBackup(graphDir string) (bool, error) {
	_, hasControl, err := InspectLog(graphDir)
	if err != nil {
		return false, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrGraphStore, err)
	}
	if hasControl {
		return false, nil
	}
	for _, name := range legacyStoreEntries {
		_, err := os.Lstat(filepath.Join(graphDir, name))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrGraphStore, err)
		}
	}
	return false, nil
}

// Converted reports whether the engine's inspection of the log in graphDir finds
// a control file, which is the second half of a conversion that succeeded
// (rule 9). An inspection that fails is returned as a store that cannot be
// opened.
func Converted(graphDir string) (bool, error) {
	_, hasControl, err := InspectLog(graphDir)
	if err != nil {
		return false, fmt.Errorf("%w: graph store unavailable: %v", utils.ErrGraphStore, err)
	}
	return hasControl, nil
}

// Backup is one complete, durable copy of a graph directory, taken by
// [TakeBackup] before the engine converts the store.
type Backup struct {
	home string
	name string
}

// Path returns the backup directory's absolute path.
func (b *Backup) Path() string { return filepath.Join(b.home, b.name) }

// TakeBackup copies the store in graphDir to graph-backup-<T>/ beside it, where
// <T> is now in UTC, and returns once the copy is complete and durable
// (SPEC/GRAPH.md § Graph Store Format Migration, rules 3 to 7).
//
// The copy is assembled in graph-backup-<T>.partial/, created exclusively; every
// file is written and synchronised, every directory is synchronised once its
// entries are in place, the staging directory is renamed to its final name, and
// the roadmap home is synchronised so the rename is durable. A name that is
// already taken, at either form, fails the backup and is left untouched.
//
// Every path is resolved inside the roadmap home through an os.Root, so nothing
// the copy reads or writes can lie outside it, and a symbolic link met in graph/
// is refused rather than followed or copied.
//
// Every failure is returned wrapping [ErrBackupFailed], with the staging
// directory this call created removed as far as it can be. graphDir is only
// read: the store is left exactly as it was found, whatever happens here.
//
// It MUST run inside the store's exclusive hold, before the store is opened
// (rule 8); it takes no lock of its own.
func TakeBackup(graphDir string, now time.Time) (*Backup, error) {
	absGraph, err := filepath.Abs(graphDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBackupFailed, err)
	}
	backup := &Backup{
		home: filepath.Dir(absGraph),
		name: BackupDirPrefix + now.UTC().Format(BackupInstantLayout),
	}
	staging := backup.name + BackupPartialSuffix

	home, err := os.OpenRoot(backup.home)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBackupFailed, err)
	}
	defer home.Close() //nolint:errcheck // a directory handle opened for reading; nothing to report on close

	if _, err := home.Lstat(backup.name); err == nil {
		return nil, fmt.Errorf("%w: %s %v", ErrBackupFailed, backup.Path(), errEntryExists)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %v", ErrBackupFailed, err)
	}
	// Mkdir is the exclusive creation: it fails on an entry of any type at the
	// staging name, and nothing at that name is then touched.
	if err := home.Mkdir(staging, backupDirMode); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("%w: %s %v", ErrBackupFailed, filepath.Join(backup.home, staging), errEntryExists)
		}
		return nil, fmt.Errorf("%w: %v", ErrBackupFailed, err)
	}

	if err := assemble(home, filepath.Base(absGraph), staging, backup.name); err != nil {
		// Best-effort: a staging directory that cannot be removed is left as the
		// specification describes, and the backup's own failure is the one
		// reported.
		_ = home.RemoveAll(staging) //nolint:errcheck // see above
		return nil, fmt.Errorf("%w: %v", ErrBackupFailed, err)
	}
	return backup, nil
}

// assemble fills the staging directory with a copy of the graph directory,
// publishes it under its final name, and makes the publication durable. Every
// name is relative to home.
func assemble(home *os.Root, graph, staging, final string) error {
	if err := home.Chmod(staging, backupDirMode); err != nil {
		return err
	}
	src, err := home.OpenRoot(graph)
	if err != nil {
		return err
	}
	defer src.Close() //nolint:errcheck // a directory handle opened for reading
	dst, err := home.OpenRoot(staging)
	if err != nil {
		return err
	}
	defer dst.Close() //nolint:errcheck // the files written through it are synchronised and closed one by one

	if err := copyDir(src, dst, "."); err != nil {
		return err
	}
	// The rename onto a name that appeared since the check above is refused
	// rather than allowed to replace it: a rename replaces an empty directory on
	// some platforms, and an earlier backup is never overwritten.
	if _, err := home.Lstat(final); err == nil {
		return fmt.Errorf("%s %w", final, errEntryExists)
	}
	if err := home.Rename(staging, final); err != nil {
		return err
	}
	return syncDir(home, ".")
}

// copyDir copies every entry of the directory dir under src into the same
// relative directory under dst, which already exists, and synchronises it once
// its entries are in place. The graph directory's own write.lock is left out
// (rule 4).
func copyDir(src, dst *os.Root, dir string) error {
	entries, err := fs.ReadDir(src.FS(), dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if dir == "." && entry.Name() == graphlock.LockFileName {
			continue
		}
		rel := filepath.Join(dir, entry.Name())
		info, err := src.Lstat(rel)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := dst.Mkdir(rel, backupDirMode); err != nil {
				return err
			}
			if err := dst.Chmod(rel, backupDirMode); err != nil {
				return err
			}
			if err := copyDir(src, dst, rel); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			if err := copyFile(src, dst, rel, info); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s (%s) %w", filepath.Join(src.Name(), rel), info.Mode().Type(), errUnsupportedEntry)
		}
	}
	return syncDir(dst, dir)
}

// copyFile copies the regular file rel under src, described by info, to a new
// file at the same relative path under dst with the backup's file mode, and
// synchronises it.
//
// The source is opened only after it was found to be a regular file, and is
// confirmed to be that same file once open, so a link put in its place in
// between is refused rather than followed.
func copyFile(src, dst *os.Root, rel string, info os.FileInfo) (err error) {
	in, err := src.Open(rel)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := in.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	opened, err := in.Stat()
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return fmt.Errorf("%s %w", filepath.Join(src.Name(), rel), errEntryChanged)
	}

	out, err := dst.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, backupFileMode)
	if err != nil {
		return err
	}
	if err := out.Chmod(backupFileMode); err != nil {
		_ = out.Close() //nolint:errcheck // the chmod failure is the one reported
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close() //nolint:errcheck // the copy failure is the one reported
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close() //nolint:errcheck // the synchronisation failure is the one reported
		return err
	}
	return out.Close()
}

// Remove deletes the backup once the conversion it protected has succeeded
// (SPEC/GRAPH.md § Graph Store Format Migration, rule 9).
//
// It renames the backup to its .partial form first, so that no moment of the
// removal leaves an incomplete copy under a backup's final name, then removes
// that directory and everything in it, and synchronises the roadmap home. On
// failure it returns the absolute path that remains — the final name when the
// rename failed, the .partial name afterwards — with the error.
func (b *Backup) Remove() (remaining string, err error) {
	partial := b.name + BackupPartialSuffix
	home, err := os.OpenRoot(b.home)
	if err != nil {
		return b.Path(), err
	}
	defer home.Close() //nolint:errcheck // a directory handle; nothing to report on close

	if _, err := home.Lstat(partial); err == nil {
		return b.Path(), fmt.Errorf("%s %w", filepath.Join(b.home, partial), errEntryExists)
	}
	if err := home.Rename(b.name, partial); err != nil {
		return b.Path(), err
	}
	if err := home.RemoveAll(partial); err != nil {
		return filepath.Join(b.home, partial), err
	}
	if err := syncDir(home, "."); err != nil {
		return filepath.Join(b.home, partial), err
	}
	return "", nil
}
