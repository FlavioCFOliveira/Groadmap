// Package db provides SQLite database connectivity and operations.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

const (
	// DefaultBusyTimeout is the SQLite busy timeout in milliseconds.
	// This prevents "database is locked" errors by waiting up to this duration.
	DefaultBusyTimeout = 10000 // 10 seconds
)

// SQLite result codes. See https://www.sqlite.org/rescode.html.
//
// The constraint codes are EXTENDED codes: SQLITE_CONSTRAINT (19) is the
// primary code shared by every constraint kind, so it cannot tell a uniqueness
// collision apart from a CHECK (275) or NOT NULL (1299) violation. Only the
// extended code can.
const (
	sqliteBusy                   = 5
	sqliteLocked                 = 6
	sqliteNotADB                 = 26   // SQLITE_NOTADB: the file is not a database
	sqliteConstraintCheck        = 275  // SQLITE_CONSTRAINT_CHECK
	sqliteConstraintForeignKey   = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
	sqliteConstraintNotNull      = 1299 // SQLITE_CONSTRAINT_NOTNULL
	sqliteConstraintPrimaryKey   = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	sqliteConstraintUniqueViolat = 2067 // SQLITE_CONSTRAINT_UNIQUE
)

// extendedResultCode returns the SQLite EXTENDED result code carried by err, or
// 0 when err is nil or carries no code. It is the single place the driver's
// coded error is unwrapped, so every classification in this package
// (IsUniqueConstraintErr, the comment write classifier) reads the same value.
func extendedResultCode(err error) int {
	if err == nil {
		return 0
	}
	var coded sqliteCoded
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return 0
}

// IsUniqueConstraintErr reports whether err is a SQLite UNIQUE or PRIMARY-KEY
// constraint violation, and only those. Callers translate it into
// ErrAlreadyExists (exit code 5), so it must not answer true for a CHECK, NOT
// NULL or FOREIGN KEY violation: those are different failures and must not be
// reported to the user as an "already in use" collision.
//
// The check is on the extended result code, not the primary one. Masking down
// to the primary code (19) makes every constraint kind look like a uniqueness
// collision, which is exactly the bug this guards against.
func IsUniqueConstraintErr(err error) bool {
	code := extendedResultCode(err)
	return code == sqliteConstraintUniqueViolat || code == sqliteConstraintPrimaryKey
}

// ClassifyDriverError gives an unclassified failure of the SQLite driver the
// class SPEC/ARCHITECTURE.md § Classification of Database Driver Failures,
// rule 3, assigns it: utils.ErrDatabase, printed as
// `Error: database error: <detail>` with the driver's diagnostic as the detail,
// and exit code 1.
//
// It changes nothing else. An error that already carries a sentinel of the
// catalogue has been classified by whoever owns it — a lost race translated
// into the refusal of the rule it lost on, the not-a-roadmap-database line, a
// validation refusal — and is returned untouched, and so is an error that
// carries no driver result code at all. The boundary is the classification,
// not the layer, which is why the one dispatch point applies it to whatever a
// handler returns.
func ClassifyDriverError(err error) error {
	if err == nil || utils.IsClassified(err) || extendedResultCode(err) == 0 {
		return err
	}
	var published *migrationFailure
	if errors.As(err, &published) {
		return err
	}
	return fmt.Errorf("%w: %w", utils.ErrDatabase, err)
}

// migrationFailure is the failure of a migration applied on open. Its line is
// published by SPEC/DATABASE.md § The failure surface, without a sentinel:
//
//	Error: running migrations: migration <version> failed: applying migration: <step>: <detail>
//
// so ClassifyDriverError leaves it as it is, and it exits 1 by the fallback
// that table names.
type migrationFailure struct{ err error }

func (m *migrationFailure) Error() string { return "running migrations: " + m.err.Error() }
func (m *migrationFailure) Unwrap() error { return m.err }

// sqliteCoded is satisfied by modernc.org/sqlite's *sqlite.Error. The check
// stays structural rather than a type assertion against that concrete type:
// it is what lets the classification above be tested with a fake, and it keeps
// the error handling independent of the driver even though the package now
// imports it for NewConnector.
type sqliteCoded interface {
	Code() int
}

// isLockedError checks if an error is a SQLite busy/locked error by
// inspecting the structured result code rather than matching strings.
func isLockedError(err error) bool {
	if err == nil {
		return false
	}
	var coded sqliteCoded
	if errors.As(err, &coded) {
		c := coded.Code() & 0xFF // primary result code (low 8 bits)
		return c == sqliteBusy || c == sqliteLocked
	}
	return false
}

// retryWithBackoff runs fn under the project's single bounded backoff policy
// (internal/backoff), which owns the attempt count and the delay ladder so that
// this site cannot hold a different opinion of them. SPEC/IMPLEMENTATION.md
// § Retry Logic is the specification; the policy is stated there and in
// internal/backoff, and nowhere else.
//
// Only SQLite busy/locked failures are waited on, per that section's Retry
// Conditions. Anything else — a constraint violation, a schema error, bad input
// — is returned unchanged and at once: waiting on it would delay a failure that
// cannot become a success, and wrapping it would hide the result code callers
// such as IsUniqueConstraintErr classify.
func retryWithBackoff(operation string, fn func() error) error {
	// backoff.Retry is value-returning because two of its three callers produce
	// a value; this one produces only an error, so the value carries nothing.
	_, err := backoff.Retry(func() (struct{}, error) { return struct{}{}, fn() }, isLockedError)
	if err == nil {
		return nil
	}
	if !isLockedError(err) {
		// Retry stops early only on an error it was told not to retry, so this
		// branch is exactly the non-retryable case: return it as fn produced it.
		return err
	}
	return fmt.Errorf("%s: failed after %d attempts: %w", operation, backoff.Attempts, err)
}

// DB wraps sql.DB with roadmap-specific operations.
//
// The connection does not carry the roadmap's name. It used to, behind a
// RoadmapName accessor, and nothing ever asked: every caller opens a roadmap
// by name and still holds that name, so the field was written twice and read
// only by tests (task #188). A name stored here would also be a second answer
// to "which roadmap is this", next to the path the connection was opened from.
type DB struct {
	*sql.DB
	queryCache *QueryCache
	batchProc  *BatchProcessor
}

// Placeholders returns a comma-separated string of n SQL "?" placeholders,
// generated on demand (see QueryCache). Use from command handlers to build
// IN (...) clauses.
func (db *DB) Placeholders(n int) string {
	return db.queryCache.GetPlaceholders(n)
}

// chmodFunc changes the permission bits of an existing path. Production code
// passes os.Chmod; it is a parameter rather than a hard-wired call so the
// failure branches of secureDBFile and restrictSidecars can be exercised
// deterministically. Those branches are unreachable from a test that owns the
// file it is testing (the owner's chmod succeeds) and permanently unreachable
// for a test running as root (root's chmod always succeeds), so injecting the
// failure is the only way to cover them without a privileged mount or a
// filesystem that discards POSIX permission bits.
type chmodFunc func(string, os.FileMode) error

// sqliteSidecarSuffixes are the two files SQLite keeps alongside a WAL-mode
// database. Declared as an array so restrictSidecars allocates nothing.
var sqliteSidecarSuffixes = [...]string{"-wal", "-shm"}

// secureDBFile brings ~/.roadmaps/<name>/project.db to 0600 and refuses the
// open when it cannot. The 0600 restriction is an OPEN-time guarantee, not a
// creation-time one: a database can arrive wider through entirely ordinary
// means (restored from an archive that carried no modes, copied off a
// filesystem that does not record them, synchronised without preserving them,
// written by an older binary under a permissive umask, or moved in from the
// legacy layout), and its contents — comment bodies included — would otherwise
// stay readable by every user of the machine for the rest of the file's life.
// See SPEC/ARCHITECTURE.md § Open-Time Permission Enforcement, which is the
// canonical statement of this rule.
//
// The sequence is exactly the one the SPEC fixes, and each step is load-bearing:
//
//  1. Read the mode. utils.VerifyPermissions IS that read — it stats the file
//     and compares — so there is one permission check in the codebase, not two.
//  2. If the mode is already 0600, change nothing and proceed. Attempting an
//     unconditional chmod would fail for a correctly restricted database the
//     caller can read but does not own, and refusing there would protect
//     nothing: the file is already private.
//  3. If the mode deviates, change it and read it again. A change that reports
//     success without moving the mode (a filesystem that does not record POSIX
//     permission bits) is a failure, which is why the confirming read exists.
//
// Callers MUST invoke this after the enclosing directories are at 0700 — so no
// other user can act on the file between the read and the repair — and before
// any connection is established, so the mode SQLite copies onto the sidecars it
// creates in this session is already 0600.
func secureDBFile(dbPath string, chmod chmodFunc) error {
	err := utils.VerifyPermissions(dbPath, utils.DBFilePerm)
	if err == nil {
		return nil // already 0600: proceed, attempt no change
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil // nothing on disk yet; creation applies 0600 from the outset
	}
	if !errors.Is(err, utils.ErrPermissionsMismatch) {
		// The mode could not even be read (a stat failure, not a mismatch).
		return cannotSecure(dbPath, err.Error())
	}

	if cerr := chmod(dbPath, utils.DBFilePerm); cerr != nil {
		return cannotSecure(dbPath, cerr.Error())
	}

	if verr := utils.VerifyPermissions(dbPath, utils.DBFilePerm); verr != nil {
		return cannotSecure(dbPath, modeMismatchDetail(dbPath, verr))
	}

	return nil
}

// cannotSecure renders the SPEC-mandated failure line for a database that
// cannot be brought to 0600:
//
//	Error: database error: cannot secure <path> to 0600: <detail>
//
// The "database error: " prefix is the rendering of the wrapped sentinel, which
// is what maps the failure to exit code 1. The sentinel is wrapped explicitly
// rather than left to the exit-1 fallback for unclassified errors, so the exit
// code is a stated property of this failure and not an accident of the default
// branch (SPEC/ARCHITECTURE.md § Open-Time Permission Enforcement, C. Failure
// mode; § Error Reuse Policy (Mandatory)).
func cannotSecure(dbPath, detail string) error {
	return fmt.Errorf("%w: cannot secure %s to 0600: %s", utils.ErrDatabase, dbPath, detail)
}

// modeMismatchDetail renders the second of the two <detail> forms the SPEC
// specifies — "expected 0600, got <mode>" — for a mode change that reported
// success without moving the mode. The file is re-read so the message carries
// exactly the two values the SPEC names rather than the verification error's
// own wording; if that read fails, the verification error is reported instead,
// which is strictly more information than the SPEC's minimum.
func modeMismatchDetail(dbPath string, verifyErr error) string {
	info, err := os.Stat(dbPath)
	if err != nil {
		return verifyErr.Error()
	}
	return fmt.Sprintf("expected %04o, got %04o", os.FileMode(utils.DBFilePerm), info.Mode().Perm())
}

// restrictSidecars brings project.db-wal and project.db-shm to 0600 when they
// are present. It is best-effort, silent, and never fatal: a sidecar that
// cannot be restricted changes neither the exit code nor the output of the
// command (SPEC/ARCHITECTURE.md § Open-Time Permission Enforcement, D.
// Sidecars).
//
// A sidecar's mode does NOT come from the process umask, which is the intuitive
// but wrong reading. findCreateFileMode stats the DATABASE FILE and hands
// robust_open its st_mode&0777, and robust_open then fstats the descriptor it
// just opened and fchmods it back to that mode whenever the new file is still
// empty — deliberately undoing the umask. Measured against the driver this
// binary links (modernc.org/sqlite): a database at 0666 produces -wal and -shm
// at 0666 under umask 0002, which a umask-derived mode could not do. The umask
// can therefore only ever narrow the result, never widen it, and in practice it
// does not narrow it either.
//
// That is precisely why settling project.db at 0600 BEFORE the connection is
// established (secureDBFile) is what makes every sidecar created in that
// session 0600, and it leaves this pass to catch only a sidecar left behind by
// an earlier session or another tool, created while the database file was still
// more permissive.
//
// Non-fatal is the correct rule because SQLite owns the sidecars' lifetime: a
// checkpoint, or the closing of the last connection, can remove the write-ahead
// log between the Stat and the mode change, so a fatal rule would turn a benign
// race that has already resolved itself into an intermittent command failure.
// The asymmetry with project.db is deliberate and is not an oversight to be
// unified later: the sidecars are transient state that never leaves the roadmap
// home directory, which the directory step has just verified is 0700, whereas
// project.db is the durable artefact whose mode travels with it.
func restrictSidecars(dbPath string, chmod chmodFunc) {
	for _, suffix := range sqliteSidecarSuffixes {
		p := dbPath + suffix
		if _, err := os.Stat(p); err != nil {
			continue // absent (e.g. checkpointed away): nothing to restrict
		}
		_ = chmod(p, utils.DBFilePerm) // #nosec G104 -- best-effort hardening; the SPEC makes a sidecar failure non-fatal and silent
	}
}

// Open opens a connection to a roadmap database.
// Creates the database file if it doesn't exist.
func Open(roadmapName string) (*DB, error) {
	return openRoadmap(roadmapName, os.Chmod)
}

// openRoadmap is Open with the mode-changing primitive injected. See chmodFunc.
//
// The order of the steps is the one SPEC/ARCHITECTURE.md § Open-Time Permission
// Enforcement and SPEC/DATABASE.md § Opening a Roadmap Database File fix, and
// each step is what makes the next one safe:
//
//  1. the roadmap home and the data directory are brought to 0700;
//  2. project.db and its three companions are examined for symbolic links,
//     without following one, before anything reads or changes a mode;
//  3. an absent project.db is pre-created at 0600, and the file's mode is
//     repaired to 0600;
//  4. the file's shape is decided — zero bytes, not SQLite, or SQLite — before
//     any connection exists, so a file that is not a roadmap database is
//     refused with nothing written into it;
//  5. a SQLite file's schema version is read before any connection setting is
//     written, so a database newer than this binary is refused unchanged;
//  6. only then is WAL configured and the schema created or migrated.
func openRoadmap(roadmapName string, chmod chmodFunc) (*DB, error) {
	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return nil, err
	}

	// Ensure the roadmap home directory (~/.roadmaps/<name>/) exists with
	// 0700 permissions before opening project.db inside it. This also
	// ensures the parent data directory exists and is private.
	if err := utils.EnsureRoadmapDir(roadmapName); err != nil {
		return nil, err
	}

	// Get database path (~/.roadmaps/<name>/project.db)
	dbPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		return nil, err
	}

	// No file the open is about to read, chmod, create or hand to SQLite may be
	// a symbolic link (SPEC/ARCHITECTURE.md § Directory Structure, location
	// rule 10). The examination precedes the pre-create below too: O_EXCL
	// refuses a dangling link as an existing file, and the stat that follows it
	// would then follow the link.
	if err := refuseSymlinkedDBFiles(dbPath); err != nil {
		return nil, err
	}

	// For a NEW database, pre-create the file with 0600 BEFORE sql.Open touches
	// it, so the descriptor never exists at umask-derived (potentially
	// world-readable) permissions (finding #77, SPEC/ARCHITECTURE.md §
	// Open-Time Permission Enforcement). O_EXCL guarantees we are the creator;
	// if a concurrent process created it first we fall back to treating it as
	// existing. The umask can still NARROW the created mode (0600 &^ umask), so
	// secureDBFile below settles it either way. Whether the file was created
	// here or found empty, the shape check below reads it as a zero-byte file
	// and the schema is created once, under the check in createSchemaIfAbsent.
	if _, statErr := os.Lstat(dbPath); os.IsNotExist(statErr) {
		// #nosec G304 -- dbPath is the internal per-roadmap path ~/.roadmaps/<name>/project.db; <name> is validated by ValidateRoadmapName upstream (no traversal), and this O_EXCL pre-create at 0600 is the fix for security finding #77
		f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, utils.DBFilePerm)
		if err != nil {
			if !os.IsExist(err) {
				return nil, fmt.Errorf("pre-creating database %s: %w", roadmapName, err)
			}
			// Lost the race: another process created it. Treat as existing.
		} else if cerr := f.Close(); cerr != nil {
			// Close the descriptor immediately; the connector reopens the
			// path. The file was never wider than 0600 at any instant.
			return nil, fmt.Errorf("closing pre-created database %s: %w", roadmapName, cerr)
		}
	}

	// Settle project.db at 0600 BEFORE the connection is established. The
	// enclosing directories are already at 0700 (EnsureRoadmapDir above), so no
	// other user can traverse into the roadmap home between reading this file's
	// mode and repairing it, and SQLite has not yet had the chance to stamp a
	// wider mode onto a sidecar it creates. A database that cannot be brought to
	// 0600 fails the command here: no connection is established and no SQL runs
	// (SPEC/ARCHITECTURE.md § Open-Time Permission Enforcement).
	if err := secureDBFile(dbPath, chmod); err != nil {
		return nil, err
	}

	// Decide what the file is before any connection exists
	// (SPEC/DATABASE.md § Opening a Roadmap Database File).
	shape, err := classifyDBFile(dbPath)
	if err != nil {
		return nil, err
	}

	// NewConnector reaches the same driver sql.Open("sqlite", ...) would, so
	// the connections are identical, but it examines the DSN. sql.Open does
	// not: it only looks up the registered driver, and because
	// modernc.org/sqlite deliberately does not implement driver.DriverContext,
	// a malformed DSN went unreported until whichever query first forced the
	// pool to dial — surfacing mid-command and attributed to that query rather
	// than to opening the database. Neither function connects, so this failure
	// is immediate and not retryable; wrapping it in retryWithBackoff would be
	// dead weight. See SPEC/IMPLEMENTATION.md § Entry Point.
	//
	// foreign_keys and busy_timeout are CONNECTION-scoped PRAGMAs: a one-shot
	// db.Exec only configures whichever single pooled connection services it,
	// leaving the second pooled connection (SetMaxOpenConns(2)) on the SQLite
	// defaults foreign_keys=OFF / busy_timeout=0 — so ON DELETE CASCADE would
	// silently not fire and locked-database waits would return BUSY immediately.
	// Carrying them in the DSN makes modernc.org/sqlite apply them on EVERY new
	// connection. See SPEC/IMPLEMENTATION.md (foreign_keys on every connection;
	// busy_timeout) and SPEC/DATABASE.md (CASCADE integrity).
	connector, err := sqlite.NewConnector(dsnFor(dbPath, false))
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", roadmapName, err)
	}
	sidecarsBefore := presentSidecars(dbPath)
	sqlDB := sql.OpenDB(connector)

	// A SQLite file is judged by its schema version before anything is written
	// to it: the WAL setting configureConnection writes is a change to the
	// file's header, and a database newer than this binary must be left byte
	// for byte as it was (SPEC/VERSION.md § Database Schema Newer Than the
	// Binary). A file whose header passed but that the engine still refuses to
	// read is refused as not a roadmap database.
	needsSchema := shape == dbShapeEmpty
	if shape == dbShapeSQLite {
		uninitialised, verr := checkSchemaVersion(sqlDB, dbPath)
		if verr != nil {
			sqlDB.Close() // #nosec G104 -- cleanup call in error path, original error returned
			removeNewSidecars(dbPath, sidecarsBefore)
			return nil, verr
		}
		needsSchema = uninitialised
	}

	// Configure connection with retry logic
	if err := retryWithBackoff("configuring database", func() error {
		return configureConnection(sqlDB)
	}); err != nil {
		sqlDB.Close() // #nosec G104 -- cleanup call in error path, original error returned
		return nil, fmt.Errorf("configuring database: %w", err)
	}

	db := &DB{
		DB:         sqlDB,
		queryCache: NewQueryCache(),
		batchProc:  NewBatchProcessor(100),
	}

	if needsSchema {
		// An uninitialised database — a zero-byte file, whether pre-created
		// above or found on disk — receives the whole schema in one
		// transaction. The transaction re-checks for the schema itself, so two
		// invocations that found the same empty file both succeed and the
		// schema is created once (SPEC/DATABASE.md § Opening a Roadmap Database
		// File).
		if err := retryWithBackoff("creating schema", func() error {
			return db.createSchemaIfAbsent()
		}); err != nil {
			db.Close() // #nosec G104 -- cleanup call in error path, original error returned
			return nil, fmt.Errorf("creating schema: %w", err)
		}
		// No post-schema chmod/verify: secureDBFile above already read the
		// mode, repaired it (including a umask-narrowed pre-create) and
		// confirmed it, and it did so BEFORE the connection existed, which is
		// what the sidecars depend on. Repeating it here would only be able to
		// fail after SQL had already run, which the SPEC's failure contract
		// rules out.
	} else {
		// Run migrations for existing databases
		if err := retryWithBackoff("running migrations", func() error {
			return db.RunMigrations()
		}); err != nil {
			db.Close() // #nosec G104 -- cleanup call in error path, original error returned
			return nil, &migrationFailure{err: err}
		}
	}

	// Tighten the WAL/SHM sidecars to 0600 (finding #78, SPEC/ARCHITECTURE.md §
	// Open-Time Permission Enforcement, D. Sidecars). WAL mode is enabled in
	// configureConnection and the schema-creation/migration writes above create
	// project.db-wal / project.db-shm.
	restrictSidecars(dbPath, chmod)

	return db, nil
}

// dbShape is what a roadmap's project.db is, decided before any connection is
// established (SPEC/DATABASE.md § Opening a Roadmap Database File).
type dbShape int

const (
	// dbShapeEmpty is a file of zero bytes: an uninitialised database.
	dbShapeEmpty dbShape = iota
	// dbShapeSQLite is a file that begins with the SQLite header string.
	dbShapeSQLite
)

// sqliteHeader is the 16-byte string every SQLite database file begins with:
// "SQLite format 3" followed by a zero byte (https://www.sqlite.org/fileformat.html,
// § 1.3.1).
const sqliteHeader = "SQLite format 3\x00"

// classifyDBFile reads the first bytes of dbPath and decides its shape. A file
// that is not empty and does not begin with the SQLite header string is refused
// with the published line; the file is opened read-only for the decision, so
// nothing is written to it.
func classifyDBFile(dbPath string) (dbShape, error) {
	f, err := os.Open(dbPath) // #nosec G304 -- dbPath is the internal per-roadmap path ~/.roadmaps/<name>/project.db, examined for symbolic links before this read
	if err != nil {
		return 0, fmt.Errorf("%w: reading %s: %v", utils.ErrDatabase, dbPath, err)
	}
	defer f.Close() //nolint:errcheck // read-only descriptor; a close error cannot be acted on

	var header [len(sqliteHeader)]byte
	n, err := io.ReadFull(f, header[:])
	switch {
	case n == 0 && (err == nil || errors.Is(err, io.EOF)):
		return dbShapeEmpty, nil
	case err != nil && !errors.Is(err, io.ErrUnexpectedEOF):
		return 0, fmt.Errorf("%w: reading %s: %v", utils.ErrDatabase, dbPath, err)
	case n < len(sqliteHeader) || string(header[:]) != sqliteHeader:
		return 0, notValidRoadmapDatabase(dbPath)
	}
	return dbShapeSQLite, nil
}

// notValidRoadmapDatabase renders the refusal of a file that is not empty and is
// not a SQLite database:
//
//	Error: database error: <path> is not a valid roadmap database
//
// It carries no text of the SQLite driver (SPEC/DATABASE.md § Opening a Roadmap
// Database File; SPEC/ARCHITECTURE.md § Classification of Database Driver
// Failures).
func notValidRoadmapDatabase(dbPath string) error {
	return fmt.Errorf("%w: %s is not a valid roadmap database", utils.ErrDatabase, dbPath)
}

// newerSchemaRefusal renders the refusal of a database whose stored schema
// version is newer than SchemaVersion (SPEC/VERSION.md § Database Schema Newer
// Than the Binary; the line is published in SPEC/DATABASE.md § Opening a
// Roadmap Database File).
func newerSchemaRefusal(dbPath, dbVersion string) error {
	return fmt.Errorf("%w: %s has schema version %s, newer than schema version %s supported by this rmp; upgrade rmp to open it",
		utils.ErrDatabase, dbPath, dbVersion, SchemaVersion)
}

// checkSchemaVersion reads _metadata.schema_version through sqlDB, before any
// other table is read and before anything is written, and refuses a database
// whose version is newer than SchemaVersion, compared numerically component by
// component.
//
// It reports uninitialised = true for a SQLite file that holds no schema object
// at all — the same uninitialised database a zero-byte file is, in the form
// SQLite gives one once a page size has been written. A file the engine refuses
// to read as a database is refused with the not-a-roadmap-database line. A
// database without a readable schema version that does hold schema objects is
// left to the migration step, exactly as before this check existed.
func checkSchemaVersion(sqlDB *sql.DB, dbPath string) (uninitialised bool, err error) {
	var version string
	qerr := retryWithBackoff("reading schema version", func() error {
		return sqlDB.QueryRow("SELECT value FROM _metadata WHERE key = 'schema_version'").Scan(&version)
	})
	if qerr == nil {
		if compareVersions(version, SchemaVersion) > 0 {
			return false, newerSchemaRefusal(dbPath, version)
		}
		return false, nil
	}
	if extendedResultCode(qerr)&0xFF == sqliteNotADB {
		return false, notValidRoadmapDatabase(dbPath)
	}
	if errors.Is(qerr, sql.ErrNoRows) {
		return false, nil
	}

	var objects int
	cerr := retryWithBackoff("reading schema", func() error {
		return sqlDB.QueryRow("SELECT COUNT(*) FROM sqlite_master").Scan(&objects)
	})
	if cerr != nil {
		if extendedResultCode(cerr)&0xFF == sqliteNotADB {
			return false, notValidRoadmapDatabase(dbPath)
		}
		return false, fmt.Errorf("%w: reading schema of %s: %w", utils.ErrDatabase, dbPath, cerr)
	}
	return objects == 0, nil
}

// dbFileSuffixes are project.db and its three SQLite companions: the
// write-ahead log, the shared-memory index, and the rollback journal. None of
// them may be a symbolic link (SPEC/ARCHITECTURE.md § Directory Structure,
// location rule 10). Declared as an array so the examination allocates nothing.
var dbFileSuffixes = [...]string{"", "-wal", "-shm", "-journal"}

// refuseSymlinkedDBFiles examines project.db and each of its companions without
// following a link, and refuses the open when any of them is a symbolic link,
// dangling or not:
//
//	Error: database error: <path> is a symbolic link; refusing to use it as a roadmap database file
//
// A name that does not exist is not refused. The link is neither followed,
// removed nor replaced, and no connection is established, so SQLite never
// follows it either.
func refuseSymlinkedDBFiles(dbPath string) error {
	for _, suffix := range dbFileSuffixes {
		p := dbPath + suffix
		info, err := os.Lstat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("%w: examining %s: %v", utils.ErrDatabase, p, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s is a symbolic link; refusing to use it as a roadmap database file", utils.ErrDatabase, p)
		}
	}
	return nil
}

// presentSidecars reports which of project.db's companions exist before a
// connection is established, so a refusal that follows the first read can
// remove exactly the companions this invocation's connection created and none
// that were already there (SPEC/DATABASE.md § Opening a Roadmap Database File:
// on the refusal "no sidecar has been created by the invocation").
func presentSidecars(dbPath string) [len(dbFileSuffixes)]bool {
	var present [len(dbFileSuffixes)]bool
	for i, suffix := range dbFileSuffixes {
		if suffix == "" {
			continue
		}
		if _, err := os.Lstat(dbPath + suffix); err == nil {
			present[i] = true
		}
	}
	return present
}

// removeNewSidecars removes every companion of dbPath that presentSidecars did
// not find, provided it is a regular file. It is best-effort: a companion that
// cannot be removed changes neither the refusal nor its exit code.
func removeNewSidecars(dbPath string, before [len(dbFileSuffixes)]bool) {
	for i, suffix := range dbFileSuffixes {
		if suffix == "" || before[i] {
			continue
		}
		p := dbPath + suffix
		if info, err := os.Lstat(p); err == nil && info.Mode().IsRegular() {
			_ = os.Remove(p) // #nosec G104 -- best-effort cleanup on a refusal path; the refusal is returned either way
		}
	}
}

// CreateRoadmapDatabase creates ~/.roadmaps/<name>/project.db, complete, for
// `rmp roadmap create`, atomically against every concurrent creator
// (SPEC/COMMANDS.md § Create Roadmap, "Creation is atomic against concurrent
// creators").
//
// The database is built whole under a temporary name inside the roadmap home
// directory — created at 0600 from the outset by os.CreateTemp — and is then
// published under project.db by os.Link, which fails when project.db already
// exists and never replaces a file. That one operation is the claim on the
// name: no check-then-create window exists in which two creators both find it
// free, no invocation can observe project.db holding a partial schema, and a
// creator that loses fails with the published already-exists refusal. The
// temporary file and every companion SQLite gave it are removed on every path,
// winner and loser alike, and a loser changes nothing the winner left.
//
// A project.db that is a symbolic link is neither a roadmap that exists nor a
// name the command may claim: it is refused with the symbolic-link line.
func CreateRoadmapDatabase(roadmapName string) error {
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return err
	}
	if err := utils.EnsureRoadmapDir(roadmapName); err != nil {
		return err
	}
	dbPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		return err
	}
	if err := refuseSymlinkedDBFiles(dbPath); err != nil {
		return err
	}
	if _, err := os.Lstat(dbPath); err == nil {
		return utils.RoadmapAlreadyExistsError(roadmapName)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dbPath), ".rmp-create-*.db")
	if err != nil {
		return fmt.Errorf("%w: creating roadmap database %s: %v", utils.ErrDatabase, dbPath, err)
	}
	tmpPath := tmp.Name()
	defer removeTempDatabase(tmpPath)
	// CreateTemp asks for 0600 and the umask can only narrow it; a narrower
	// mode would leave SQLite unable to write the database it is building, so
	// the file is brought to exactly 0600 before it is used. It is never wider
	// than 0600 at any instant.
	if err := tmp.Chmod(utils.DBFilePerm); err != nil {
		_ = tmp.Close() // #nosec G104 -- error path; the chmod failure is returned
		return fmt.Errorf("%w: creating roadmap database %s: %v", utils.ErrDatabase, dbPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: creating roadmap database %s: %v", utils.ErrDatabase, dbPath, err)
	}

	if err := buildDatabase(tmpPath); err != nil {
		return err
	}

	if err := os.Link(tmpPath, dbPath); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return utils.RoadmapAlreadyExistsError(roadmapName)
		}
		return fmt.Errorf("%w: publishing roadmap database %s: %v", utils.ErrDatabase, dbPath, err)
	}
	return nil
}

// buildDatabase creates the whole schema in the database file at path and
// closes it, leaving no companion behind: the last connection to close in WAL
// mode checkpoints the log and removes it.
func buildDatabase(path string) error {
	connector, err := sqlite.NewConnector(dsnFor(path, false))
	if err != nil {
		return fmt.Errorf("%w: opening %s: %v", utils.ErrDatabase, path, err)
	}
	sqlDB := sql.OpenDB(connector)
	db := &DB{DB: sqlDB, queryCache: NewQueryCache(), batchProc: NewBatchProcessor(100)}

	if err := configureConnection(sqlDB); err != nil {
		db.Close() // #nosec G104 -- cleanup call in error path, original error returned
		return fmt.Errorf("configuring database: %w", err)
	}
	if err := db.CreateSchema(); err != nil {
		db.Close() // #nosec G104 -- cleanup call in error path, original error returned
		return fmt.Errorf("creating schema: %w", err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("%w: closing %s: %v", utils.ErrDatabase, path, err)
	}
	return nil
}

// removeTempDatabase removes the temporary database CreateRoadmapDatabase built
// and every companion SQLite may have given it. Removing the temporary name of
// a database already published by os.Link leaves the published name intact.
func removeTempDatabase(tmpPath string) {
	for _, suffix := range dbFileSuffixes {
		_ = os.Remove(tmpPath + suffix) // #nosec G104 -- best-effort cleanup; an absent file is the expected case for the companions
	}
}

// OpenExisting opens an existing roadmap database.
// Returns an error if the database doesn't exist. A roadmap home that is a
// regular file, or any other entry that is neither a directory nor a symbolic
// link, is a roadmap that does not exist (SPEC/COMMANDS.md § Roadmap Selection
// (Always Required)).
func OpenExisting(roadmapName string) (*DB, error) {
	exists, err := utils.RoadmapExists(roadmapName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: roadmap %q", utils.ErrNotFound, roadmapName)
	}

	return Open(roadmapName)
}

// OpenReadOnly opens an existing roadmap database for strictly read-only
// access. Unlike Open/OpenExisting it does NOT run schema migrations (no DDL)
// and opens every connection with query_only(true), so the SQLite engine
// rejects any write — schema change, row mutation, or audit insert. This is
// required by the web interface, which MUST NOT modify rows, write an audit
// entry, or alter the schema (SPEC/WEB.md § Read-Only Data Flow / Security and
// Constraints). Previously the web pages opened via OpenExisting -> Open, which
// ran RunMigrations and could rewrite a stale-schema database on a mere read
// (finding #43). journal_mode is intentionally not set (it is a write blocked
// by query_only; the database is already WAL from creation).
//
// It applies the same open-time 0600 rule as Open: it reads the mode, attempts
// no change when the file is already 0600 — which is what keeps a legitimate
// read working for a correctly restricted database the caller does not own —
// repairs the mode when it deviates, and refuses the open when it cannot.
// Restricting the mode is the ONLY filesystem effect this path may have: it
// creates no file and no directory, never widens a mode, and does not create,
// modify or verify directories (SPEC/ARCHITECTURE.md § Open-Time Permission
// Enforcement, E. The read-only open path; SPEC/WEB.md § Read-Only Data Flow).
// A refusal is a read failure on the affected route (HTTP 500), not a reason
// for the web server to stop.
//
// It applies the same refusals as Open, in the same order: a symbolic link
// among the database's files, a file that is not a SQLite database, and a
// schema newer than this binary (SPEC/DATABASE.md § Opening a Roadmap Database
// File). A file of zero bytes is refused as well, because a read-only open
// creates no schema.
func OpenReadOnly(roadmapName string) (*DB, error) {
	return openRoadmapReadOnly(roadmapName, os.Chmod)
}

// openRoadmapReadOnly is OpenReadOnly with the mode-changing primitive
// injected. See chmodFunc.
func openRoadmapReadOnly(roadmapName string, chmod chmodFunc) (*DB, error) {
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return nil, err
	}

	exists, err := utils.RoadmapExists(roadmapName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("%w: roadmap %q", utils.ErrNotFound, roadmapName)
	}

	dbPath, err := utils.GetRoadmapPath(roadmapName)
	if err != nil {
		return nil, err
	}

	if err := refuseSymlinkedDBFiles(dbPath); err != nil {
		return nil, err
	}

	// Settle the mode before the connector exists, exactly as the writable path
	// does. query_only blocks SQL writes, not the engine's creation of a
	// sidecar, so the sidecars this path can produce still inherit their mode
	// from project.db.
	if err := secureDBFile(dbPath, chmod); err != nil {
		return nil, err
	}

	shape, err := classifyDBFile(dbPath)
	if err != nil {
		return nil, err
	}
	if shape == dbShapeEmpty {
		return nil, fmt.Errorf("%w: %s holds no schema; a read-only open creates none", utils.ErrDatabase, dbPath)
	}

	connector, err := sqlite.NewConnector(dsnFor(dbPath, true))
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", roadmapName, err)
	}
	sqlDB := sql.OpenDB(connector)

	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	if _, err := checkSchemaVersion(sqlDB, dbPath); err != nil {
		sqlDB.Close() // #nosec G104 -- cleanup call in error path, original error returned
		return nil, err
	}

	restrictSidecars(dbPath, chmod)

	return &DB{
		DB:         sqlDB,
		queryCache: NewQueryCache(),
		batchProc:  NewBatchProcessor(100),
	}, nil
}

// dsnFor builds the DSN that modernc.org/sqlite opens dbPath with, carrying the
// connection-scoped PRAGMAs (foreign_keys, busy_timeout, and query_only when
// readOnly) so the driver applies them to EVERY pooled connection. Unlike a
// one-shot db.Exec("PRAGMA ..."), which configures only whichever connection
// services that call, the driver replays these on each connection it opens, so
// the integrity and lock-waiting guarantees hold regardless of which pooled
// connection a given query lands on. journal_mode=WAL is intentionally NOT set
// here: it is a persistent, database-level setting (stored in the file header)
// and is configured once in configureConnection.
//
// The DSN is a SQLite file: URI with the path percent-encoded, NEVER
// dbPath+"?"+params. The driver splits a DSN at the first '?' and, unless the
// string starts with "file:", keeps only what precedes it as the filename, so a
// path containing '?' opens a DIFFERENT file and hands its own tail to the
// parameter parser -- which since driver v1.55.0 recognises keys that turn off
// foreign_keys or downgrade synchronous. The roadmap name is validated upstream,
// but the home directory the path is rooted in is not. Percent-encoding makes
// every character in the path inert.
//
// The PRAGMAs travel as the driver's validated shorthand keys, not as the
// verbatim _pragma=name(value) form. A _pragma value is executed exactly as
// written and is never validated, and it is the one DSN parameter class that can
// still fail partway through, leaving the settings ahead of the failure already
// applied. The shorthand keys are checked against a fixed accepted set before
// ANY parameter is applied, so a bad value fails the connection outright instead
// of half-configuring it. Only the primary key names are used: each has an alias
// (_fk, _timeout) and the alias wins when both appear, so supplying both is a
// trap. The driver fixes the order it applies them in -- _busy_timeout first,
// _query_only last -- independent of the order written here.
//
// A read-write DSN also carries _txlock=immediate, which is not a PRAGMA: it
// makes the driver begin every read-write transaction with BEGIN IMMEDIATE, so
// the write lock is taken at BEGIN, before the transaction has read anything,
// where SQLite does invoke the busy handler and busy_timeout applies. Begun
// DEFERRED, a transaction that reads before it writes asks for the write lock
// at its first write, and if another connection holds it SQLite returns
// SQLITE_BUSY at once without invoking the busy handler, since waiting could not
// succeed while the transaction keeps what it has read; concurrent writer
// processes then failed instead of waiting for one another. A read-only DSN
// carries no _txlock: an IMMEDIATE begin on a connection that never writes
// would only hold the write lock against every writer for the length of each
// read. The driver begins a transaction requested with sql.TxOptions{ReadOnly:
// true} with a plain BEGIN whatever _txlock says. See SPEC/IMPLEMENTATION.md
// § Transaction Lock Mode.
//
// See SPEC/IMPLEMENTATION.md § DSN Construction and https://www.sqlite.org/uri.html.
func dsnFor(dbPath string, readOnly bool) string {
	params := fmt.Sprintf("_busy_timeout=%d&_foreign_keys=1", DefaultBusyTimeout)
	if readOnly {
		params += "&_query_only=1"
	} else {
		params += "&_txlock=immediate"
	}
	return "file:" + uriPath(dbPath) + "?" + params
}

// uriPath renders a filesystem path as the path component of a SQLite file:
// URI, following https://www.sqlite.org/uri.html: backslashes become forward
// slashes, a leading drive letter gets a '/' in front of it, and the result is
// percent-encoded. An absolute path is returned with the empty-authority "//"
// prefix (file:///path); a relative one without it (file:path), which is the
// form SQLite documents for relative filenames.
func uriPath(path string) string {
	// ToSlash is a no-op outside Windows.
	p := filepath.ToSlash(path)

	// "On windows only, if the filename begins with a drive letter, prepend a
	// single '/' character." The test is the drive letter itself rather than
	// runtime.GOOS, so it cannot turn a relative path into an absolute one.
	if len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]) {
		p = "/" + p
	}

	escaped := (&url.URL{Path: p}).EscapedPath()
	if strings.HasPrefix(p, "/") {
		return "//" + escaped
	}
	return escaped
}

// isASCIILetter reports whether c is an unaccented A-Z or a-z, the only
// characters SQLite accepts as a Windows drive letter.
func isASCIILetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// configureConnection sets up the persistent, database-level SQLite settings
// and the connection pool. Connection-scoped PRAGMAs (foreign_keys,
// busy_timeout) are NOT set here — they are carried in the DSN (see
// dsnFor) so they apply to every pooled connection, not just the one
// that happens to service this call.
func configureConnection(db *sql.DB) error {
	// Enable WAL mode for better concurrency. WAL is a persistent database-level
	// setting recorded in the file header, so a single Exec suffices for the
	// lifetime of the database (it survives reopen and applies to all connections).
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return fmt.Errorf("enabling WAL mode: %w", err)
	}

	// Configure connection pool for SQLite with WAL mode
	// SQLite only supports 1 writer at a time, so limit connections
	db.SetMaxOpenConns(2)                   // One for reads, one for writes
	db.SetMaxIdleConns(1)                   // Keep one warm connection
	db.SetConnMaxLifetime(30 * time.Minute) // Recycle connections more frequently
	db.SetConnMaxIdleTime(10 * time.Minute) // Close idle connections after 10 min

	return nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.DB != nil {
		return db.DB.Close()
	}
	return nil
}

// WithTransaction executes a function within a database transaction.
// Automatically commits on success or rolls back on error.
// Uses retry logic for handling database locked errors.
func (db *DB) WithTransaction(fn func(*sql.Tx) error) error {
	return retryWithBackoff("transaction", func() error {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction: %w", err)
		}

		defer func() {
			if r := recover(); r != nil {
				// #nosec G104 -- rollback during panic recovery; the original panic is re-raised below and takes precedence over any rollback error
				tx.Rollback() //nolint:errcheck // rollback in panic recovery, original panic takes precedence
				panic(r)
			}
		}()

		if err := fn(tx); err != nil {
			// #nosec G104 -- rollback after a failed operation; the original error is returned to the caller and takes precedence over any rollback error
			tx.Rollback() //nolint:errcheck // rollback after error, original error is returned to caller
			return err
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing transaction: %w", err)
		}

		return nil
	})
}

// ==================== CONTEXT TIMEOUT HELPERS ====================

const (
	// DefaultQueryTimeout is the default timeout for database queries (30 seconds).
	DefaultQueryTimeout = 30 * time.Second

	// QuickQueryTimeout is the timeout for simple read operations (5 seconds).
	QuickQueryTimeout = 5 * time.Second
)

// WithDefaultTimeout returns a context with the default query timeout.
func WithDefaultTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), DefaultQueryTimeout)
}

// WithQuickTimeout returns a context with the quick query timeout.
func WithQuickTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), QuickQueryTimeout)
}
