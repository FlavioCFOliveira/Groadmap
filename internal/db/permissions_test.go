package db

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// This file pins SPEC/ARCHITECTURE.md § Open-Time Permission Enforcement,
// subsection "F. Verifiable behaviour", statement by statement:
//
//	F.1 a 0666 database owned by the caller is opened successfully and left
//	    at 0600  -> TestOpenHardensExistingDatabase,
//	               TestOpenReadOnlyHardensExistingDatabase
//	F.2 a database whose mode cannot be brought to 0600 fails with exit code
//	    1, the SPEC's stderr line, nothing on stdout, and no change to the
//	    file  -> TestOpenRefusesDatabaseItCannotSecure,
//	            TestOpenRefusesWhenModeChangeDoesNotTake,
//	            TestOpenReadOnlyRefusesDatabaseItCannotSecure
//	            (exit code 1 itself: cmd/rmp TestHandleError_SentinelErrors)
//	F.3 a database already at 0600 is opened with no mode change attempted
//	    -> TestOpenAttemptsNoModeChangeOnRestrictedDatabase
//	F.4 both directories are 0700 after any successful open
//	    -> TestOpenRehardensDirectories
//	F.5 a sidecar that cannot be restricted changes neither the exit code nor
//	    the output  -> TestSidecarRestrictionFailureIsNotFatal
//
// F.1 and F.4 are also pinned end to end against the compiled binary, and F.1
// on the read-only path against a running `rmp web`, by
// tests/test_42_security_audit.py.
//
// The refusal branch of F.2 is covered here by fault injection rather than end
// to end, and deliberately so: an unprivileged process cannot make chmod fail
// on a file it owns, and a process running as root cannot make it fail at all,
// so no end-to-end arrangement provokes it without a privileged mount or a
// filesystem that discards POSIX permission bits. The seam is the chmodFunc
// parameter of openRoadmap/openRoadmapReadOnly; production code passes
// os.Chmod and behaves identically.

// ==================== fault-injection chmod stubs ====================

// errChmodDenied is what a real EPERM from chmod(2) renders as, so the message
// these tests assert on is the message a user would actually read.
func errChmodDenied(path string) error {
	return &os.PathError{Op: "chmod", Path: path, Err: os.ErrPermission}
}

// denyingChmod refuses every mode change, standing in for a database the
// invoking user can read but does not own.
func denyingChmod(path string, _ os.FileMode) error {
	return errChmodDenied(path)
}

// silentlyIneffectiveChmod reports success without moving the mode, standing in
// for a filesystem that does not record POSIX permission bits. It is the case
// the confirming re-read exists for.
func silentlyIneffectiveChmod(string, os.FileMode) error { return nil }

// recordingChmod counts the paths a call was attempted on and fails every one,
// so a test can assert both that no attempt was made and that the open
// succeeded regardless.
type recordingChmod struct {
	attempts []string
}

func (r *recordingChmod) chmod(path string, _ os.FileMode) error {
	r.attempts = append(r.attempts, path)
	return errChmodDenied(path)
}

// sidecarDenyingChmod refuses only the WAL/SHM sidecars, lets the database file
// through to the real primitive, and records the sidecars it refused — which is
// the arrangement F.5 describes, plus the evidence that the refusal really
// happened.
type sidecarDenyingChmod struct {
	refused []string
}

func (s *sidecarDenyingChmod) chmod(path string, mode os.FileMode) error {
	if strings.HasSuffix(path, "-wal") || strings.HasSuffix(path, "-shm") {
		s.refused = append(s.refused, path)
		return errChmodDenied(path)
	}
	return os.Chmod(path, mode)
}

// ==================== helpers ====================

// newRoadmapInTempHome redirects HOME to a per-test directory — nothing in this
// file may touch the developer's real ~/.roadmaps — creates a roadmap with one
// task in it, and returns the roadmap name and its database path.
func newRoadmapInTempHome(t *testing.T, name string) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	database, err := Open(name)
	if err != nil {
		t.Fatalf("creating roadmap %q: %v", name, err)
	}
	newTestTask(t, database, "durable record of findings and decisions")
	if err := database.Close(); err != nil {
		t.Fatalf("closing roadmap %q: %v", name, err)
	}

	dbPath, err := utils.GetRoadmapPath(name)
	if err != nil {
		t.Fatalf("resolving database path: %v", err)
	}
	return name, dbPath
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

// digestOf fingerprints a file's full contents, which is how these tests prove
// that a refused open ran no SQL: schema creation, a migration, a row change or
// an audit insert would all move the digest.
func digestOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test-owned path under t.TempDir()
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// dirEntries lists a directory so a test can prove a refused open created
// nothing beside the database — no sidecar, no journal, no replacement file.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// captureStdout runs fn with os.Stdout redirected and returns everything it
// wrote, so "writes nothing to stdout" is asserted rather than assumed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		_, _ = io.Copy(&sb, r)
		done <- sb.String()
	}()

	fn()

	os.Stdout = original
	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	out := <-done
	if err := r.Close(); err != nil {
		t.Fatalf("closing pipe reader: %v", err)
	}
	return out
}

// ==================== F.1 ====================

// TestOpenHardensExistingDatabase pins F.1 on the writable path: a database
// that arrives at 0666 is opened successfully and left at 0600. Before this
// rule existed the command succeeded and left the file at 0666, which is the
// whole defect (rmp task #178).
func TestOpenHardensExistingDatabase(t *testing.T) {
	name, dbPath := newRoadmapInTempHome(t, "hardenexisting")

	if err := os.Chmod(dbPath, 0o666); err != nil {
		t.Fatalf("widening the database mode: %v", err)
	}

	database, err := OpenExisting(name)
	if err != nil {
		t.Fatalf("opening a 0666 database must succeed: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	if got := modeOf(t, dbPath); got != utils.DBFilePerm {
		t.Errorf("project.db is %04o after an open, want %04o", got, os.FileMode(utils.DBFilePerm))
	}

	// The database is still usable: hardening is not a substitute for opening.
	if _, err := database.GetTask(testContext(), 1); err != nil {
		t.Errorf("reading a task from the hardened database: %v", err)
	}
}

// TestOpenReadOnlyHardensExistingDatabase pins F.1 on the read-only path the
// web server uses. Restricting the mode is the only filesystem effect that path
// may have, and it is one it MUST have: the web interface is the surface on
// which the contents leave the invoking user's terminal.
func TestOpenReadOnlyHardensExistingDatabase(t *testing.T) {
	name, dbPath := newRoadmapInTempHome(t, "hardenreadonly")

	if err := os.Chmod(dbPath, 0o666); err != nil {
		t.Fatalf("widening the database mode: %v", err)
	}

	database, err := OpenReadOnly(name)
	if err != nil {
		t.Fatalf("opening a 0666 database read-only must succeed: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	if got := modeOf(t, dbPath); got != utils.DBFilePerm {
		t.Errorf("project.db is %04o after a read-only open, want %04o", got, os.FileMode(utils.DBFilePerm))
	}
	if _, err := database.GetTask(testContext(), 1); err != nil {
		t.Errorf("reading a task through the read-only handle: %v", err)
	}
}

// ==================== F.2 ====================

// TestOpenRefusesDatabaseItCannotSecure pins F.2: a database whose mode cannot
// be changed fails the command with the SPEC's message, and by the time it
// fails nothing has happened — no connection, no SQL, no output, and the file's
// contents, name, location and mode are exactly as they were found.
func TestOpenRefusesDatabaseItCannotSecure(t *testing.T) {
	name, dbPath := newRoadmapInTempHome(t, "refusewritable")
	roadmapDir := filepath.Dir(dbPath)

	if err := os.Chmod(dbPath, 0o666); err != nil {
		t.Fatalf("widening the database mode: %v", err)
	}
	digestBefore := digestOf(t, dbPath)
	entriesBefore := dirEntries(t, roadmapDir)

	var (
		database *DB
		err      error
	)
	stdout := captureStdout(t, func() {
		database, err = openRoadmap(name, denyingChmod)
	})

	if err == nil {
		_ = database.Close() //nolint:errcheck // test cleanup on an unexpected success
		t.Fatal("opening a database that cannot be secured must fail")
	}
	if database != nil {
		t.Error("no connection may be handed back on the refusal path")
	}

	// The message, verbatim. `rmp` prints "Error: " + err.Error(), so this is
	// the stderr line SPEC/ARCHITECTURE.md § Open-Time Permission Enforcement,
	// C. Failure mode, specifies.
	want := "database error: cannot secure " + dbPath + " to 0600: chmod " + dbPath + ": permission denied"
	if err.Error() != want {
		t.Errorf("error message:\n got: %s\nwant: %s", err.Error(), want)
	}
	// The wrapped sentinel is what maps the failure to exit code 1, and it is
	// wrapped explicitly rather than relying on the unclassified-error fallback.
	if !errors.Is(err, utils.ErrDatabase) {
		t.Error("the failure must wrap utils.ErrDatabase (exit code 1)")
	}
	// No OTHER sentinel may be in the chain, or cmd/rmp's handleError would map
	// the failure to a different exit code before it ever reaches ErrDatabase.
	for _, other := range []error{
		utils.ErrNotFound, utils.ErrAlreadyExists, utils.ErrNoRoadmap,
		utils.ErrValidation, utils.ErrFieldTooLarge, utils.ErrInvalidInput,
		utils.ErrRequired,
	} {
		if errors.Is(err, other) {
			t.Errorf("the failure must not wrap %v: it would change the exit code", other)
		}
	}

	if stdout != "" {
		t.Errorf("the refusal wrote to stdout: %q", stdout)
	}
	if got := modeOf(t, dbPath); got != 0o666 {
		t.Errorf("the file's mode changed to %04o; it must be left exactly as found (0666)", got)
	}
	if got := digestOf(t, dbPath); got != digestBefore {
		t.Error("the database contents changed: no SQL may run before the refusal")
	}
	if got := dirEntries(t, roadmapDir); !equalStrings(got, entriesBefore) {
		t.Errorf("the roadmap directory changed: %v -> %v", entriesBefore, got)
	}
}

// TestOpenRefusesWhenModeChangeDoesNotTake pins the SECOND <detail> form: the
// mode change reported success but the mode did not move, which is what happens
// on a filesystem that does not record POSIX permission bits. Without the
// confirming re-read this case would open a world-readable database believing
// it had restricted it.
func TestOpenRefusesWhenModeChangeDoesNotTake(t *testing.T) {
	name, dbPath := newRoadmapInTempHome(t, "refusenoop")

	if err := os.Chmod(dbPath, 0o666); err != nil {
		t.Fatalf("widening the database mode: %v", err)
	}
	digestBefore := digestOf(t, dbPath)

	database, err := openRoadmap(name, silentlyIneffectiveChmod)
	if err == nil {
		_ = database.Close() //nolint:errcheck // test cleanup on an unexpected success
		t.Fatal("a mode change that does not take must fail the open")
	}
	if database != nil {
		t.Error("no connection may be handed back on the refusal path")
	}

	want := "database error: cannot secure " + dbPath + " to 0600: expected 0600, got 0666"
	if err.Error() != want {
		t.Errorf("error message:\n got: %s\nwant: %s", err.Error(), want)
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Error("the failure must wrap utils.ErrDatabase (exit code 1)")
	}
	if got := modeOf(t, dbPath); got != 0o666 {
		t.Errorf("the file's mode changed to %04o; it must be left exactly as found (0666)", got)
	}
	if got := digestOf(t, dbPath); got != digestBefore {
		t.Error("the database contents changed: no SQL may run before the refusal")
	}
}

// TestOpenReadOnlyRefusesDatabaseItCannotSecure pins F.2 on the read-only path.
// A reader that cannot restrict a database it can read is, by construction,
// reading a database other users of the machine can also read; the web server
// surfaces the refusal as HTTP 500 on that route and keeps serving.
func TestOpenReadOnlyRefusesDatabaseItCannotSecure(t *testing.T) {
	name, dbPath := newRoadmapInTempHome(t, "refusereadonly")
	roadmapDir := filepath.Dir(dbPath)

	if err := os.Chmod(dbPath, 0o666); err != nil {
		t.Fatalf("widening the database mode: %v", err)
	}
	// The read-only path must not create, modify or verify directories, so the
	// roadmap home is left deliberately wide and must stay that way.
	if err := os.Chmod(roadmapDir, 0o777); err != nil {
		t.Fatalf("widening the roadmap directory: %v", err)
	}
	digestBefore := digestOf(t, dbPath)
	entriesBefore := dirEntries(t, roadmapDir)

	database, err := openRoadmapReadOnly(name, denyingChmod)
	if err == nil {
		_ = database.Close() //nolint:errcheck // test cleanup on an unexpected success
		t.Fatal("the read-only path must refuse a database it cannot secure")
	}
	if database != nil {
		t.Error("no connection may be handed back on the refusal path")
	}

	want := "database error: cannot secure " + dbPath + " to 0600: chmod " + dbPath + ": permission denied"
	if err.Error() != want {
		t.Errorf("error message:\n got: %s\nwant: %s", err.Error(), want)
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Error("the failure must wrap utils.ErrDatabase (exit code 1)")
	}
	if got := modeOf(t, dbPath); got != 0o666 {
		t.Errorf("the file's mode changed to %04o; it must be left exactly as found (0666)", got)
	}
	if got := digestOf(t, dbPath); got != digestBefore {
		t.Error("the database contents changed on a read-only refusal")
	}
	if got := dirEntries(t, roadmapDir); !equalStrings(got, entriesBefore) {
		t.Errorf("the read-only path created something: %v -> %v", entriesBefore, got)
	}
	if got := modeOf(t, roadmapDir); got != 0o777 {
		t.Errorf("the read-only path changed the roadmap directory to %04o; it must not touch directories", got)
	}
}

// ==================== F.3 ====================

// TestOpenAttemptsNoModeChangeOnRestrictedDatabase pins F.3: a database already
// at 0600 is opened with NO mode change attempted, on both paths. The injected
// chmod fails every call, so the open can only succeed if it was never called —
// which is exactly the condition of a correctly restricted database the caller
// can read but does not own. An unconditional chmod would fail there and
// refusing would protect nothing, because the file is already private.
func TestOpenAttemptsNoModeChangeOnRestrictedDatabase(t *testing.T) {
	name, dbPath := newRoadmapInTempHome(t, "alreadyrestricted")

	if got := modeOf(t, dbPath); got != utils.DBFilePerm {
		t.Fatalf("precondition: project.db is %04o, want %04o", got, os.FileMode(utils.DBFilePerm))
	}

	t.Run("writable", func(t *testing.T) {
		rec := &recordingChmod{}
		database, err := openRoadmap(name, rec.chmod)
		if err != nil {
			t.Fatalf("opening an already-restricted database must succeed: %v", err)
		}
		defer database.Close() //nolint:errcheck // test cleanup
		for _, attempt := range rec.attempts {
			if attempt == dbPath {
				t.Error("a mode change was attempted on a database already at 0600")
			}
		}
	})

	t.Run("read-only", func(t *testing.T) {
		rec := &recordingChmod{}
		database, err := openRoadmapReadOnly(name, rec.chmod)
		if err != nil {
			t.Fatalf("opening an already-restricted database read-only must succeed: %v", err)
		}
		defer database.Close() //nolint:errcheck // test cleanup
		for _, attempt := range rec.attempts {
			if attempt == dbPath {
				t.Error("a mode change was attempted on a database already at 0600")
			}
		}
	})
}

// ==================== F.4 ====================

// TestOpenRehardensDirectories pins F.4: after any successful open, ~/.roadmaps
// and ~/.roadmaps/<name> are 0700 whatever mode they had before. The directory
// rule is unchanged by this work and MUST NOT be relaxed to align it with the
// file rule — the directory is the boundary that stops another user reaching
// any file inside the roadmap home at all.
func TestOpenRehardensDirectories(t *testing.T) {
	name, dbPath := newRoadmapInTempHome(t, "rehardendirs")
	roadmapDir := filepath.Dir(dbPath)
	dataDir := filepath.Dir(roadmapDir)

	for _, dir := range []string{dataDir, roadmapDir} {
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatalf("widening %s: %v", dir, err)
		}
	}

	database, err := OpenExisting(name)
	if err != nil {
		t.Fatalf("opening the roadmap: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	for _, dir := range []string{dataDir, roadmapDir} {
		if got := modeOf(t, dir); got != utils.DataDirPerm {
			t.Errorf("%s is %04o after an open, want %04o", dir, got, os.FileMode(utils.DataDirPerm))
		}
	}
}

// ==================== F.5 ====================

// TestSidecarRestrictionFailureIsNotFatal pins F.5: a project.db-wal or
// project.db-shm that cannot be restricted changes neither the exit code nor
// the output of the command. SQLite owns those files' lifetime — a checkpoint
// can remove the log between the stat and the mode change — so a fatal rule
// would turn a self-resolving race into an intermittent failure.
func TestSidecarRestrictionFailureIsNotFatal(t *testing.T) {
	name, dbPath := newRoadmapInTempHome(t, "sidecarnotfatal")

	// Leave sidecars behind at a wide mode, as an earlier session or another
	// tool would have, then refuse every attempt to restrict them.
	sidecars := []string{dbPath + "-wal", dbPath + "-shm"}
	for _, p := range sidecars {
		if err := os.WriteFile(p, []byte{}, 0o666); err != nil { // #nosec G306 -- deliberately wide: this is the condition under test
			t.Fatalf("creating %s: %v", p, err)
		}
		if err := os.Chmod(p, 0o666); err != nil { // umask may have narrowed the create
			t.Fatalf("widening %s: %v", p, err)
		}
	}

	var (
		database *DB
		err      error
	)
	stub := &sidecarDenyingChmod{}
	stdout := captureStdout(t, func() {
		database, err = openRoadmap(name, stub.chmod)
	})
	if err != nil {
		t.Fatalf("a sidecar that cannot be restricted must not fail the open: %v", err)
	}
	defer database.Close() //nolint:errcheck // test cleanup
	if stdout != "" {
		t.Errorf("the sidecar failure produced output: %q", stdout)
	}

	// The refusal really happened, so the test is pinning the non-fatal rule
	// and not an unreached branch.
	if len(stub.refused) == 0 {
		t.Fatal("no sidecar mode change was even attempted: this test proves nothing")
	}

	// The sidecars are nonetheless 0600, and NOT because rmp restricted them:
	// every attempt to do so was refused. SQLite gives a sidecar the permission
	// bits of the database file it belongs to, which secureDBFile settled at
	// 0600 before the connection was established. That is the mechanism the
	// explicit restriction is defence in depth for, not the mechanism it
	// provides.
	for _, p := range sidecars {
		if _, statErr := os.Stat(p); statErr != nil {
			continue // SQLite may have checkpointed it away; that is the race the rule tolerates
		}
		if got := modeOf(t, p); got&0o077 != 0 {
			t.Errorf("%s is %04o: a sidecar must never be left accessible to other users", p, got)
		}
	}
}

// ==================== secureDBFile unit coverage ====================

// TestSecureDBFileAbsentFileIsNotAFailure covers the branch a brand-new roadmap
// takes: with nothing on disk yet there is no mode to read, and creation
// applies 0600 from the outset.
func TestSecureDBFileAbsentFileIsNotAFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "project.db")
	if err := secureDBFile(missing, denyingChmod); err != nil {
		t.Errorf("an absent database must not fail the check: %v", err)
	}
}

// TestSecureDBFileRepairsDeviatingMode covers the ordinary repair: the mode
// deviates, the change is applied with the real primitive, and the confirming
// re-read passes.
func TestSecureDBFileRepairsDeviatingMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.db")
	if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("widening %s: %v", path, err)
	}

	if err := secureDBFile(path, os.Chmod); err != nil {
		t.Fatalf("securing a 0644 file must succeed: %v", err)
	}
	if got := modeOf(t, path); got != utils.DBFilePerm {
		t.Errorf("mode is %04o, want %04o", got, os.FileMode(utils.DBFilePerm))
	}
}

// TestSecureDBFileUnreadableModeIsReported covers the third branch: the mode
// could not even be read. The path is reported with the same message shape, so
// no unclassified error escapes to the exit-1 fallback unlabelled.
func TestSecureDBFileUnreadableModeIsReported(t *testing.T) {
	// A path whose parent is a regular file: stat fails with ENOTDIR, which is
	// neither "not found" nor a mode mismatch.
	parent := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatalf("creating %s: %v", parent, err)
	}
	path := filepath.Join(parent, "project.db")

	err := secureDBFile(path, denyingChmod)
	if err == nil {
		t.Fatal("an unreadable mode must be reported, not ignored")
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("the failure must wrap utils.ErrDatabase: %v", err)
	}
	if !strings.HasPrefix(err.Error(), "database error: cannot secure "+path+" to 0600: ") {
		t.Errorf("unexpected message shape: %s", err.Error())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
