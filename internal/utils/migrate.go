package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// legacyDBSuffix is the basename suffix of a roadmap database under the
// legacy filesystem layout (~/.roadmaps/<name>.db). The SQLite sidecars
// (-wal, -shm) carry the longer ".db-wal" / ".db-shm" suffixes and are
// therefore not matched by this suffix, so they are never mistaken for a
// roadmap candidate.
const legacyDBSuffix = ".db"

// sidecarSuffixes are the SQLite auxiliary files that accompany a database.
// They are migrated alongside their database only when present.
var sidecarSuffixes = []string{"-wal", "-shm"}

// MigrateLegacyLayout relocates every roadmap that is still stored in the
// legacy top-level layout (~/.roadmaps/<name>.db) into the current per-roadmap
// home-directory layout (~/.roadmaps/<name>/project.db). It is the startup
// sweep specified in SPEC/ARCHITECTURE.md § Filesystem Layout Migration and is
// intended to run once, before command routing, on every rmp invocation.
//
// Behaviour contract (see the SPEC for the authoritative description):
//   - A single read of ~/.roadmaps/ detects candidates: immediate top-level
//     REGULAR files whose name ends in ".db". A top-level entry that ends in
//     ".db" but is not a regular file (a symbolic link, a directory, or any
//     other special file) is not a candidate; it is skipped silently and left
//     completely untouched, never followed, renamed, or chmod-ed. Sidecars
//     (-wal/-shm) are not candidates either.
//   - When the data directory is absent or has no candidates, the sweep is a
//     no-op and returns nil.
//   - Each candidate is migrated independently. A skipped or failed roadmap
//     (invalid name, layout conflict, or a contained move failure) does not
//     abort the sweep: it emits a non-fatal warning to stderr and the sweep
//     continues with the remaining candidates. A successful database move
//     whose best-effort sidecar move fails is NOT a failed roadmap: it is
//     reported migrated with a distinct sidecar warning.
//   - The only fatal condition is an inability to read the data directory
//     itself; that is wrapped as ErrDatabase (exit code 1).
//
// The move uses os.Rename (atomic within a filesystem); the database content
// is never copied, so a roadmap's data exists exactly once on disk at every
// instant. No file is ever unlinked except as the source side of a successful
// rename.
func MigrateLegacyLayout() error {
	return migrateLegacyLayout(os.Stderr)
}

// migrateLegacyLayout is the testable core of MigrateLegacyLayout; the warning
// sink is injected so tests can assert on the non-fatal warnings.
func migrateLegacyLayout(warn *os.File) error {
	dataDir, err := GetDataDir()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no data directory yet: nothing to migrate
		}
		// Inability to read the data directory is the only fatal condition.
		return fmt.Errorf("reading data directory %s: %w", dataDir, ErrDatabase)
	}

	for _, entry := range entries {
		// Candidates are top-level REGULAR files ending in ".db".
		// Everything else is silently skipped and left completely
		// untouched, exactly like a ".db"-named directory: it is simply
		// not a roadmap. DirEntry.Type() reports the type bits from
		// lstat WITHOUT following symbolic links, so a symlink (dangling,
		// or pointing at a file or a directory) reports as non-regular
		// and is excluded here. This is the security boundary: the sweep
		// never renames or chmods a symlink, so it can never move or
		// change permissions of anything reached through the link and
		// thus never mutates a path outside ~/.roadmaps/<name>/. Skipping
		// directories, symlinks, and other special files in one check
		// also keeps already-migrated roadmaps and the -wal/-shm sidecars
		// out of the candidate set.
		if !entry.Type().IsRegular() {
			continue
		}
		legacyName := entry.Name()
		if !strings.HasSuffix(legacyName, legacyDBSuffix) {
			continue
		}

		name := strings.TrimSuffix(legacyName, legacyDBSuffix)
		if err := migrateOneRoadmap(dataDir, name, warn); err != nil {
			fmt.Fprintf(warn, "Warning: skipping migration of legacy roadmap %q: %v\n", name, err)
			continue
		}
	}

	return nil
}

// migrateOneRoadmap migrates a single legacy roadmap database (<name>.db) and
// any present sidecars into ~/.roadmaps/<name>/project.db. It returns a
// non-nil error ONLY when the roadmap must be reported as skipped/failed; the
// caller turns that into a non-fatal "skipping migration" warning. On any such
// returned error the legacy database is left untouched (no partial state),
// because the database is moved with a single atomic rename and that rename is
// the first mutation attempted.
//
// Sidecar (-wal/-shm) moves are best-effort and happen AFTER the authoritative
// database rename has succeeded. A sidecar move that fails does not undo the
// successful database move and does not make the roadmap report as failed: the
// database is the authoritative state and SQLite regenerates -wal/-shm as
// needed. Such a failure is surfaced as a DISTINCT warning via warn and the
// function still returns nil (the roadmap migrated successfully). A leftover
// legacy "<name>.db-wal" is harmless: it is not a ".db" candidate and is
// ignored by every future sweep.
//
// All non-nil errors returned here carry the ErrDatabase sentinel in their
// chain (see SPEC/ARCHITECTURE.md § Error Reuse Policy), so errors.Is detects
// them; this does not change exit-code behaviour, as the sweep surfaces them as
// non-fatal warnings.
func migrateOneRoadmap(dataDir, name string, warn *os.File) error {
	// 1. The basename must be a valid roadmap name; otherwise the entry is
	//    not a roadmap and must be left untouched.
	if err := ValidateRoadmapName(name); err != nil {
		return fmt.Errorf("invalid roadmap name: %w", err)
	}

	legacyDB := filepath.Join(dataDir, name+legacyDBSuffix)
	roadmapDir := filepath.Join(dataDir, name)
	currentDB := filepath.Join(roadmapDir, DBFileName)

	// 2. Conflict: the current layout already exists for this name. The
	//    conflict is keyed on the destination DATABASE FILE project.db, NOT on
	//    the home directory. If ~/.roadmaps/<name>/project.db already exists
	//    (as any file type) the current layout wins: skip this name and leave
	//    the legacy files untouched. An existing home directory WITHOUT
	//    project.db is NOT a conflict (it is the idempotent-recovery case from
	//    an earlier run interrupted after the directory was created but before
	//    the rename completed); it is handled by step 3, which reuses the
	//    directory. This project.db-absent check is the mandatory safety guard:
	//    the atomic rename in step 4 SILENTLY OVERWRITES its destination, so
	//    confirming project.db is absent here is exactly what guarantees the
	//    rename is never reached when project.db exists, and no existing data
	//    is ever destroyed. The steps must never be reordered so that the
	//    rename could run with project.db present.
	if _, err := os.Lstat(currentDB); err == nil {
		return fmt.Errorf("current layout already present at %s (legacy file left untouched): %w", currentDB, ErrAlreadyExists)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking current layout at %s: %v: %w", currentDB, err, ErrDatabase)
	}

	// 3. Ensure the roadmap home directory exists with 0700 permissions before
	//    moving any file into it. MkdirAll is idempotent (no error if the
	//    directory already exists), so a pre-existing project.db-less directory
	//    from an interrupted earlier run is REUSED rather than rejected; the
	//    subsequent Chmod (re)applies and the later VerifyPermissions confirms
	//    the 0700 posture on it. Step 2 has already guaranteed project.db is
	//    absent, so reusing the directory cannot clobber an existing database.
	if err := os.MkdirAll(roadmapDir, DataDirPerm); err != nil {
		return fmt.Errorf("creating roadmap directory %s: %v: %w", roadmapDir, err, ErrDatabase)
	}
	if err := os.Chmod(roadmapDir, DataDirPerm); err != nil {
		return fmt.Errorf("setting permissions on roadmap directory %s: %v: %w", roadmapDir, err, ErrDatabase)
	}

	// 4. Move the database (atomic rename). This is the first mutation of
	//    legacy state; if it fails the legacy file is intact and the roadmap
	//    is reported as a contained failure. os.Rename SILENTLY OVERWRITES an
	//    existing destination, so this is only ever reached because the step-2
	//    guard confirmed project.db (currentDB) is absent; the rename therefore
	//    cannot overwrite an existing database.
	if err := os.Rename(legacyDB, currentDB); err != nil {
		return fmt.Errorf("moving %s to %s: %v: %w", legacyDB, currentDB, err, ErrDatabase)
	}

	// At this point the authoritative database has been moved successfully:
	// the roadmap IS migrated. Everything below is best-effort hardening and
	// must NOT downgrade the roadmap to "failed/skipped".

	// 5. Move the sidecars only when present (best-effort). This never makes
	//    the roadmap report as failed: see moveSidecars.
	moveSidecars(name, legacyDB, currentDB, warn)

	// 6. Re-verify the security posture after the move: 0700 directory,
	//    0600 database. A failure here is reported as a (contained) roadmap
	//    failure, but the database has already moved to the current layout.
	if err := os.Chmod(currentDB, DBFilePerm); err != nil {
		return fmt.Errorf("setting permissions on %s: %v: %w", currentDB, err, ErrDatabase)
	}
	if err := VerifyPermissions(roadmapDir, DataDirPerm); err != nil {
		return fmt.Errorf("verifying roadmap directory permissions: %v: %w", err, ErrDatabase)
	}
	if err := VerifyPermissions(currentDB, DBFilePerm); err != nil {
		return fmt.Errorf("verifying database permissions: %v: %w", err, ErrDatabase)
	}

	return nil
}

// moveSidecars relocates the SQLite -wal/-shm sidecars of a roadmap from the
// legacy basename (legacyDB+suffix) to the current layout (currentDB+suffix).
// It runs ONLY after the authoritative database has already been moved, so it
// is best-effort: a missing sidecar is skipped, and a sidecar whose move fails
// does not undo the successful database move and does not abort the migration.
// Each failure is surfaced as a DISTINCT, accurate warning that makes clear the
// roadmap itself migrated successfully (it must not be confused with the
// "skipping migration" warning the sweep emits for a genuinely failed roadmap).
// A leftover legacy "<name>.db-wal" is harmless: it is not a ".db" candidate
// and is ignored by every future sweep, and SQLite regenerates -wal/-shm from
// the authoritative database as needed.
func moveSidecars(name, legacyDB, currentDB string, warn *os.File) {
	for _, suffix := range sidecarSuffixes {
		legacySidecar := legacyDB + suffix
		if _, err := os.Lstat(legacySidecar); err != nil {
			continue // sidecar absent: nothing to move
		}
		currentSidecar := currentDB + suffix
		if err := os.Rename(legacySidecar, currentSidecar); err != nil {
			fmt.Fprintf(warn, "Warning: roadmap %q migrated, but sidecar %q could not be moved: %v\n", name, legacySidecar, err)
		}
	}
}
