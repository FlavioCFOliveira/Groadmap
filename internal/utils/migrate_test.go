package utils

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempDataDir redirects the user's home directory to a fresh temporary
// directory for the duration of the test and returns the resulting
// ~/.roadmaps data directory path (created with 0700). os.UserHomeDir honours
// $HOME on this platform, so this makes every path helper hermetic without
// touching the developer's real ~/.roadmaps.
func withTempDataDir(t *testing.T) string {
	t.Helper()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dataDir := filepath.Join(tmpHome, DataDirName)
	if err := os.MkdirAll(dataDir, DataDirPerm); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	return dataDir
}

// writeLegacyDB creates a legacy top-level database file (~/.roadmaps/<name>.db)
// with the given content and 0600 permissions, returning its path.
func writeLegacyDB(t *testing.T, dataDir, name, content string) string {
	t.Helper()
	path := filepath.Join(dataDir, name+".db")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write legacy db %q: %v", path, err)
	}
	return path
}

// mustReadFile reads a file and fails the test if it cannot be read.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("failed to read %q: %v", path, err)
	}
	return string(b)
}

// TestMigrateLegacyLayout_HardensDataDir is a regression gate for finding #59:
// SPEC/ARCHITECTURE.md requires directory permissions to be (re)verified to
// 0700 after every layout migration. The per-roadmap loop only hardened each
// roadmap home, leaving a loosened ~/.roadmaps data directory unfixed.
func TestMigrateLegacyLayout_HardensDataDir(t *testing.T) {
	dataDir := withTempDataDir(t)
	writeLegacyDB(t, dataDir, "paymentservice", "DBCONTENT")

	// Loosen the data directory's permissions to simulate a pre-existing
	// insecure posture.
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatalf("loosening data dir perms: %v", err)
	}

	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("MigrateLegacyLayout returned error: %v", err)
	}

	// The data directory itself must be re-secured to 0700.
	if err := VerifyPermissions(dataDir, DataDirPerm); err != nil {
		t.Errorf("data directory must be re-secured to 0700 after migration: %v", err)
	}
}

func TestMigrateLegacyLayout_HappyPathNoSidecars(t *testing.T) {
	dataDir := withTempDataDir(t)

	legacy := writeLegacyDB(t, dataDir, "paymentservice", "DBCONTENT-PAYMENT")

	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("MigrateLegacyLayout returned error: %v", err)
	}

	// Legacy file is gone.
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy file should no longer exist, stat err = %v", err)
	}

	// Current layout exists with identical content.
	current := filepath.Join(dataDir, "paymentservice", DBFileName)
	if got := mustReadFile(t, current); got != "DBCONTENT-PAYMENT" {
		t.Errorf("migrated content = %q, want %q", got, "DBCONTENT-PAYMENT")
	}

	// Permissions: 0700 directory, 0600 database.
	if err := VerifyPermissions(filepath.Join(dataDir, "paymentservice"), DataDirPerm); err != nil {
		t.Errorf("roadmap dir permissions: %v", err)
	}
	if err := VerifyPermissions(current, DBFilePerm); err != nil {
		t.Errorf("database permissions: %v", err)
	}
}

func TestMigrateLegacyLayout_HappyPathWithSidecars(t *testing.T) {
	dataDir := withTempDataDir(t)

	legacy := writeLegacyDB(t, dataDir, "mobileapp", "DBCONTENT-MOBILE")
	if err := os.WriteFile(legacy+"-wal", []byte("WAL-DATA"), 0600); err != nil {
		t.Fatalf("failed to write wal sidecar: %v", err)
	}
	if err := os.WriteFile(legacy+"-shm", []byte("SHM-DATA"), 0600); err != nil {
		t.Fatalf("failed to write shm sidecar: %v", err)
	}

	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("MigrateLegacyLayout returned error: %v", err)
	}

	roadmapDir := filepath.Join(dataDir, "mobileapp")
	current := filepath.Join(roadmapDir, DBFileName)

	// Legacy artefacts all gone.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(legacy + suffix); !os.IsNotExist(err) {
			t.Errorf("legacy file %q should be gone, stat err = %v", legacy+suffix, err)
		}
	}

	// Current artefacts present with preserved content.
	if got := mustReadFile(t, current); got != "DBCONTENT-MOBILE" {
		t.Errorf("db content = %q, want %q", got, "DBCONTENT-MOBILE")
	}
	if got := mustReadFile(t, current+"-wal"); got != "WAL-DATA" {
		t.Errorf("wal content = %q, want %q", got, "WAL-DATA")
	}
	if got := mustReadFile(t, current+"-shm"); got != "SHM-DATA" {
		t.Errorf("shm content = %q, want %q", got, "SHM-DATA")
	}
}

func TestMigrateLegacyLayout_MultipleRoadmaps(t *testing.T) {
	dataDir := withTempDataDir(t)

	names := []string{"alpha-service", "beta_api", "gamma123"}
	for _, n := range names {
		writeLegacyDB(t, dataDir, n, "CONTENT-"+n)
	}

	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("MigrateLegacyLayout returned error: %v", err)
	}

	for _, n := range names {
		current := filepath.Join(dataDir, n, DBFileName)
		if got := mustReadFile(t, current); got != "CONTENT-"+n {
			t.Errorf("roadmap %q content = %q, want %q", n, got, "CONTENT-"+n)
		}
		if _, err := os.Stat(filepath.Join(dataDir, n+".db")); !os.IsNotExist(err) {
			t.Errorf("legacy file for %q should be gone", n)
		}
	}
}

func TestMigrateLegacyLayout_IdempotentNoOp(t *testing.T) {
	dataDir := withTempDataDir(t)

	writeLegacyDB(t, dataDir, "analytics", "CONTENT-ANALYTICS")

	// First sweep migrates.
	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("first sweep error: %v", err)
	}
	current := filepath.Join(dataDir, "analytics", DBFileName)
	contentAfterFirst := mustReadFile(t, current)

	// Second sweep: no legacy files remain, so it must be a no-op and must
	// not disturb the already-migrated database.
	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("second sweep error: %v", err)
	}
	if got := mustReadFile(t, current); got != contentAfterFirst {
		t.Errorf("idempotent sweep changed content: got %q, want %q", got, contentAfterFirst)
	}
}

// TestMigrateLegacyLayout_ConflictLeavesLegacyUntouched covers the conflict
// edge case (SPEC/ARCHITECTURE.md § Filesystem Layout Migration, Edge Cases):
// the conflict is keyed on the destination DATABASE FILE project.db, so the
// current layout must hold a real project.db (with content distinct from the
// legacy file) to constitute a conflict. The current layout wins: the legacy
// file is left byte-for-byte untouched, project.db is NOT overwritten, and a
// conflicting roadmap does not stop other valid roadmaps in the same sweep from
// migrating.
func TestMigrateLegacyLayout_ConflictLeavesLegacyUntouched(t *testing.T) {
	dataDir := withTempDataDir(t)

	// Legacy file present...
	legacy := writeLegacyDB(t, dataDir, "billing", "LEGACY-BILLING")
	// ...and the current layout ALSO present, holding a real project.db with
	// DISTINCT content. The presence of project.db (not merely the directory)
	// is what makes this a conflict.
	roadmapDir := filepath.Join(dataDir, "billing")
	if err := os.MkdirAll(roadmapDir, DataDirPerm); err != nil {
		t.Fatalf("failed to create current dir: %v", err)
	}
	current := filepath.Join(roadmapDir, DBFileName)
	if err := os.WriteFile(current, []byte("CURRENT-BILLING"), DBFilePerm); err != nil {
		t.Fatalf("failed to write current db: %v", err)
	}

	// A second, conflict-free legacy roadmap in the SAME sweep must still
	// migrate: a per-roadmap conflict is contained and never aborts the sweep.
	otherLegacy := writeLegacyDB(t, dataDir, "shipping", "LEGACY-SHIPPING")

	// Sweep must NOT abort and must NOT touch either side of the conflict.
	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("sweep should not be fatal on conflict: %v", err)
	}

	// Current layout wins and project.db is NOT overwritten by the legacy file.
	if got := mustReadFile(t, current); got != "CURRENT-BILLING" {
		t.Errorf("current project.db must NOT be overwritten, got %q want %q", got, "CURRENT-BILLING")
	}
	// Legacy file is left byte-for-byte untouched (not moved, deleted, or overwritten).
	if got := mustReadFile(t, legacy); got != "LEGACY-BILLING" {
		t.Errorf("legacy db must be left byte-for-byte untouched, got %q want %q", got, "LEGACY-BILLING")
	}

	// The conflict-free roadmap still migrated: its legacy file is gone and its
	// project.db holds the moved content.
	otherCurrent := filepath.Join(dataDir, "shipping", DBFileName)
	if got := mustReadFile(t, otherCurrent); got != "LEGACY-SHIPPING" {
		t.Errorf("conflict-free roadmap must still migrate, got %q want %q", got, "LEGACY-SHIPPING")
	}
	if _, err := os.Stat(otherLegacy); !os.IsNotExist(err) {
		t.Errorf("conflict-free roadmap legacy file should be gone, stat err = %v", err)
	}
}

func TestMigrateLegacyLayout_ConflictEmitsWarning(t *testing.T) {
	dataDir := withTempDataDir(t)

	writeLegacyDB(t, dataDir, "billing", "LEGACY-BILLING")
	// The conflict is keyed on project.db, so a real project.db must exist for
	// this to be a conflict (an empty directory alone is the idempotent-recovery
	// case and would migrate silently, not warn).
	roadmapDir := filepath.Join(dataDir, "billing")
	if err := os.MkdirAll(roadmapDir, DataDirPerm); err != nil {
		t.Fatalf("failed to create current dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(roadmapDir, DBFileName), []byte("CURRENT-BILLING"), DBFilePerm); err != nil {
		t.Fatalf("failed to write current db: %v", err)
	}

	warnPath := filepath.Join(t.TempDir(), "warn.txt")
	warn, err := os.Create(warnPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("failed to create warn sink: %v", err)
	}

	if err := migrateLegacyLayout(warn); err != nil {
		_ = warn.Close()
		t.Fatalf("sweep should not be fatal on conflict: %v", err)
	}
	if err := warn.Close(); err != nil {
		t.Fatalf("failed to close warn sink: %v", err)
	}

	out := mustReadFile(t, warnPath)
	if out == "" {
		t.Error("expected a non-fatal warning on conflict, got none")
	}
	// The warning must name the roadmap so the user can act on it.
	if !strings.Contains(out, "billing") {
		t.Errorf("conflict warning should mention the roadmap name, got %q", out)
	}
}

// TestMigrateLegacyLayout_IdempotentRecoveryReusesEmptyDir covers the
// idempotent-recovery edge case (SPEC/ARCHITECTURE.md § Filesystem Layout
// Migration, Edge Cases): a ~/.roadmaps/<name>/ directory that already exists
// but does NOT contain project.db is NOT a conflict. It models an earlier run
// interrupted after the directory was created but before the rename completed.
// The migration must PROCEED: the existing directory is reused with 0700, the
// legacy database is moved in as project.db with its content preserved, the
// legacy top-level file is gone, and NO warning is emitted.
func TestMigrateLegacyLayout_IdempotentRecoveryReusesEmptyDir(t *testing.T) {
	dataDir := withTempDataDir(t)

	const name = "fulfilment-service"
	legacy := writeLegacyDB(t, dataDir, name, "DBCONTENT-FULFILMENT")

	// Pre-create an EMPTY home directory (no project.db): the interrupted-run
	// state. Use a deliberately non-0700 mode to prove step 3 re-applies 0700.
	roadmapDir := filepath.Join(dataDir, name)
	if err := os.Mkdir(roadmapDir, 0755); err != nil {
		t.Fatalf("failed to pre-create empty home dir: %v", err)
	}

	warnPath := filepath.Join(t.TempDir(), "warn.txt")
	warn, err := os.Create(warnPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("failed to create warn sink: %v", err)
	}
	if err := migrateLegacyLayout(warn); err != nil {
		_ = warn.Close()
		t.Fatalf("idempotent recovery must not be fatal: %v", err)
	}
	if err := warn.Close(); err != nil {
		t.Fatalf("failed to close warn sink: %v", err)
	}

	// Migration proceeded: legacy moved into the reused directory as project.db.
	current := filepath.Join(roadmapDir, DBFileName)
	if got := mustReadFile(t, current); got != "DBCONTENT-FULFILMENT" {
		t.Errorf("legacy content must be moved into the reused dir, got %q want %q", got, "DBCONTENT-FULFILMENT")
	}
	// Legacy top-level file is gone (it was the rename source).
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy file should be gone after recovery migration, stat err = %v", err)
	}
	// The reused directory is 0700 and the database is 0600.
	if err := VerifyPermissions(roadmapDir, DataDirPerm); err != nil {
		t.Errorf("reused home dir must be re-secured to 0700: %v", err)
	}
	if err := VerifyPermissions(current, DBFilePerm); err != nil {
		t.Errorf("migrated db must be 0600: %v", err)
	}
	// No warning: idempotent recovery completes normally (not a skip).
	if out := mustReadFile(t, warnPath); out != "" {
		t.Errorf("idempotent recovery must emit no warning, got %q", out)
	}
}

// TestMigrateLegacyLayout_ExistingDBNotOverwritten is the data-safety
// regression guard for the rename-overwrite hazard. os.Rename silently
// overwrites its destination, so the step-2 conflict check (keyed on project.db)
// is the only thing preventing the legacy database from clobbering an existing
// project.db. With project.db holding content X and the legacy file holding a
// DIFFERENT content Y, the sweep must leave project.db == X (never Y), leave the
// legacy file untouched (still Y), and emit a conflict warning. If the step-2
// guard is reverted to a directory-keyed check this test still passes (the dir
// exists), but the dedicated mutation check below proves the file-keyed guard is
// load-bearing: see TestMigrateOneRoadmap_DirKeyedGuardWouldOverwrite.
func TestMigrateLegacyLayout_ExistingDBNotOverwritten(t *testing.T) {
	dataDir := withTempDataDir(t)

	const name = "orders-service"
	const contentX = "PROJECT-DB-CONTENT-X-KEEP-ME"
	const contentY = "LEGACY-DB-CONTENT-Y-MUST-NOT-WIN"

	legacy := writeLegacyDB(t, dataDir, name, contentY)
	roadmapDir := filepath.Join(dataDir, name)
	if err := os.MkdirAll(roadmapDir, DataDirPerm); err != nil {
		t.Fatalf("failed to create current dir: %v", err)
	}
	current := filepath.Join(roadmapDir, DBFileName)
	if err := os.WriteFile(current, []byte(contentX), DBFilePerm); err != nil {
		t.Fatalf("failed to write current project.db: %v", err)
	}

	warnPath := filepath.Join(t.TempDir(), "warn.txt")
	warn, err := os.Create(warnPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("failed to create warn sink: %v", err)
	}
	if err := migrateLegacyLayout(warn); err != nil {
		_ = warn.Close()
		t.Fatalf("sweep should not be fatal on conflict: %v", err)
	}
	if err := warn.Close(); err != nil {
		t.Fatalf("failed to close warn sink: %v", err)
	}

	// The decisive assertion: project.db still holds X, never the legacy Y.
	if got := mustReadFile(t, current); got != contentX {
		t.Errorf("project.db was OVERWRITTEN: got %q, want %q (rename-overwrite guard failed)", got, contentX)
	}
	// The legacy file is untouched and still holds Y.
	if got := mustReadFile(t, legacy); got != contentY {
		t.Errorf("legacy db must be untouched, got %q want %q", got, contentY)
	}
	// A conflict warning was emitted naming the roadmap.
	out := mustReadFile(t, warnPath)
	if out == "" {
		t.Error("expected a non-fatal conflict warning, got none")
	}
	if !strings.Contains(out, name) {
		t.Errorf("conflict warning should name the roadmap %q, got %q", name, out)
	}
}

// TestMigrateOneRoadmap_DirKeyedGuardWouldOverwrite is the mutation check for
// the data-safety guard. It proves that the file-keyed conflict check is what
// prevents the overwrite, by reproducing what a DIRECTORY-keyed check (the old
// behaviour) would and would not catch:
//
//   - Directory exists AND project.db exists: both the old (directory) and the
//     new (file) check treat this as a conflict, so the rename is skipped. Not
//     a discriminating case.
//   - Directory exists but project.db is ABSENT: the new (file) check lets the
//     migration PROCEED (idempotent recovery). The old (directory) check would
//     instead SKIP, which is merely over-cautious here (nothing to overwrite),
//     not a data-safety failure.
//
// The genuine overwrite hazard the file-keyed guard removes is the path where
// the destination project.db exists. This test asserts directly on
// migrateOneRoadmap that, with project.db present, it returns the
// ErrAlreadyExists conflict and performs NO rename — i.e. the guard fires on the
// FILE. Were the guard reverted to keying on a project.db-less directory, the
// idempotent-recovery test above would fail (it pre-creates an empty dir and
// requires the migration to proceed), so the two tests together pin the
// behaviour from both sides.
func TestMigrateOneRoadmap_DirKeyedGuardWouldOverwrite(t *testing.T) {
	dataDir := withTempDataDir(t)

	const name = "catalog-service"
	const contentX = "EXISTING-PROJECT-DB-X"
	const contentY = "INCOMING-LEGACY-Y"

	legacy := writeLegacyDB(t, dataDir, name, contentY)
	roadmapDir := filepath.Join(dataDir, name)
	if err := os.MkdirAll(roadmapDir, DataDirPerm); err != nil {
		t.Fatalf("failed to create current dir: %v", err)
	}
	current := filepath.Join(roadmapDir, DBFileName)
	if err := os.WriteFile(current, []byte(contentX), DBFilePerm); err != nil {
		t.Fatalf("failed to write current project.db: %v", err)
	}

	warn, err := os.CreateTemp(t.TempDir(), "warn-*.txt")
	if err != nil {
		t.Fatalf("failed to create warn sink: %v", err)
	}
	defer func() { _ = warn.Close() }()

	// The file-keyed guard must fire: conflict reported, no rename performed.
	err = migrateOneRoadmap(dataDir, name, warn)
	if err == nil {
		t.Fatal("migrateOneRoadmap must return a conflict when project.db exists; got nil")
	}
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("conflict must wrap ErrAlreadyExists for errors.Is; got %v", err)
	}
	// The destination project.db is intact (no rename happened).
	if got := mustReadFile(t, current); got != contentX {
		t.Errorf("project.db must be intact after a conflict, got %q want %q", got, contentX)
	}
	// The legacy source is intact (no rename happened).
	if got := mustReadFile(t, legacy); got != contentY {
		t.Errorf("legacy db must be intact after a conflict, got %q want %q", got, contentY)
	}
}

func TestMigrateLegacyLayout_InvalidNameSkipped(t *testing.T) {
	dataDir := withTempDataDir(t)

	// An uppercase basename violates the roadmap name regex, so the entry is
	// not a valid roadmap and must be left untouched.
	invalid := filepath.Join(dataDir, "BadName.db")
	if err := os.WriteFile(invalid, []byte("UNTOUCHED"), 0600); err != nil {
		t.Fatalf("failed to write invalid-named db: %v", err)
	}

	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("sweep should not be fatal on invalid name: %v", err)
	}

	// File still present and untouched.
	if got := mustReadFile(t, invalid); got != "UNTOUCHED" {
		t.Errorf("invalid-named legacy file must be untouched, got %q", got)
	}
	// No directory was created for it.
	if _, err := os.Stat(filepath.Join(dataDir, "BadName")); !os.IsNotExist(err) {
		t.Errorf("no home directory should be created for an invalid name, stat err = %v", err)
	}
}

func TestMigrateLegacyLayout_DataDirAbsentNoOp(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Deliberately do NOT create ~/.roadmaps.

	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("sweep over absent data dir should be a no-op, got: %v", err)
	}

	// The sweep must not have created the data directory.
	if _, err := os.Stat(filepath.Join(tmpHome, DataDirName)); !os.IsNotExist(err) {
		t.Errorf("sweep must not create the data directory, stat err = %v", err)
	}
}

func TestMigrateLegacyLayout_SidecarsNotTreatedAsRoadmaps(t *testing.T) {
	dataDir := withTempDataDir(t)

	// Orphan sidecars (names ending in .db-wal / .db-shm, no .db base file).
	// They must not be treated as roadmap candidates: no directory named
	// "orphan-wal" / "orphan-shm" / "orphan" should be created, and the
	// files must be left where they are.
	walPath := filepath.Join(dataDir, "orphan.db-wal")
	shmPath := filepath.Join(dataDir, "orphan.db-shm")
	if err := os.WriteFile(walPath, []byte("WAL"), 0600); err != nil {
		t.Fatalf("failed to write orphan wal: %v", err)
	}
	if err := os.WriteFile(shmPath, []byte("SHM"), 0600); err != nil {
		t.Fatalf("failed to write orphan shm: %v", err)
	}

	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("sweep error: %v", err)
	}

	// Sidecars untouched.
	if got := mustReadFile(t, walPath); got != "WAL" {
		t.Errorf("orphan wal must be untouched, got %q", got)
	}
	if got := mustReadFile(t, shmPath); got != "SHM" {
		t.Errorf("orphan shm must be untouched, got %q", got)
	}

	// No spurious directories created.
	for _, n := range []string{"orphan", "orphan.db", "orphan.db-wal", "orphan.db-shm"} {
		if info, err := os.Stat(filepath.Join(dataDir, n)); err == nil && info.IsDir() {
			t.Errorf("no directory %q should have been created", n)
		}
	}
}

// TestMigrateLegacyLayout_DanglingSymlinkLeftUntouched is the security guard
// for a top-level ".db" symlink whose target does not exist. Such an entry is
// not a regular file and therefore not a roadmap candidate: it must be left
// exactly where it is, no home directory must be created for it, and no
// warning must be emitted (it is silently not a roadmap, like a ".db"-named
// directory).
func TestMigrateLegacyLayout_DanglingSymlinkLeftUntouched(t *testing.T) {
	dataDir := withTempDataDir(t)

	// escape.db -> /nonexistent (dangling). Use an absolute, definitely
	// non-existent target so the symlink cannot be resolved.
	link := filepath.Join(dataDir, "escape.db")
	target := filepath.Join(t.TempDir(), "this-target-does-not-exist")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("failed to create dangling symlink: %v", err)
	}

	warnPath := filepath.Join(t.TempDir(), "warn.txt")
	warn, err := os.Create(warnPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("failed to create warn sink: %v", err)
	}
	if err := migrateLegacyLayout(warn); err != nil {
		_ = warn.Close()
		t.Fatalf("sweep must not fail on a dangling symlink: %v", err)
	}
	if err := warn.Close(); err != nil {
		t.Fatalf("failed to close warn sink: %v", err)
	}

	// The symlink is still present, still a symlink, still pointing at the
	// same (non-existent) target: completely untouched.
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("symlink must still exist after sweep: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("entry must still be a symlink, got mode %v", fi.Mode())
	}
	if dest, err := os.Readlink(link); err != nil || dest != target {
		t.Errorf("symlink target changed: got %q (err %v), want %q", dest, err, target)
	}
	// No roadmap home directory was created for it.
	if _, err := os.Stat(filepath.Join(dataDir, "escape")); !os.IsNotExist(err) {
		t.Errorf("no home directory must be created for a symlink, stat err = %v", err)
	}
	// Silent skip: no warning.
	if out := mustReadFile(t, warnPath); out != "" {
		t.Errorf("a non-regular entry must be skipped silently, got warning: %q", out)
	}
}

// TestMigrateLegacyLayout_SymlinkToFileNotChmoded is the security guard for a
// top-level ".db" symlink pointing at a regular file OUTSIDE the data
// directory. The sweep must never follow the link: the external file must not
// be moved and its permissions must be unchanged (chmod follows symlinks, so a
// naive os.Chmod on the entry would corrupt the external file's mode). The
// symlink itself must be left untouched and no home directory created.
func TestMigrateLegacyLayout_SymlinkToFileNotChmoded(t *testing.T) {
	dataDir := withTempDataDir(t)

	// External regular file with known permissions, in a separate temp dir
	// that is NOT inside ~/.roadmaps.
	externalDir := t.TempDir()
	externalFile := filepath.Join(externalDir, "outside-secret.txt")
	if err := os.WriteFile(externalFile, []byte("OUTSIDE-PAYLOAD"), 0644); err != nil {
		t.Fatalf("failed to create external file: %v", err)
	}
	if err := os.Chmod(externalFile, 0644); err != nil { // defeat umask
		t.Fatalf("failed to chmod external file: %v", err)
	}

	link := filepath.Join(dataDir, "escape.db")
	if err := os.Symlink(externalFile, link); err != nil {
		t.Fatalf("failed to create symlink-to-file: %v", err)
	}

	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("sweep must not fail on a symlink-to-file: %v", err)
	}

	// External file's permissions are UNCHANGED (chmod must not have followed
	// the link). This is the core security assertion.
	if err := VerifyPermissions(externalFile, 0644); err != nil {
		t.Errorf("external file permissions must be unchanged (0644): %v", err)
	}
	// External file's content and location are unchanged (not moved).
	if got := mustReadFile(t, externalFile); got != "OUTSIDE-PAYLOAD" {
		t.Errorf("external file content/location changed, got %q", got)
	}
	// The symlink itself is untouched.
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("symlink must still exist: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("entry must still be a symlink, got mode %v", fi.Mode())
	}
	// No home directory created.
	if _, err := os.Stat(filepath.Join(dataDir, "escape")); !os.IsNotExist(err) {
		t.Errorf("no home directory must be created for a symlink, stat err = %v", err)
	}
}

// TestMigrateLegacyLayout_SymlinkToDirNotChmoded is the security guard for a
// top-level ".db" symlink pointing at a directory OUTSIDE the data directory.
// The sweep must not follow the link: the external directory must not be moved
// and its permissions must be unchanged.
func TestMigrateLegacyLayout_SymlinkToDirNotChmoded(t *testing.T) {
	dataDir := withTempDataDir(t)

	// External directory with known permissions, outside ~/.roadmaps.
	externalDir := filepath.Join(t.TempDir(), "outside-dir")
	if err := os.Mkdir(externalDir, 0755); err != nil {
		t.Fatalf("failed to create external dir: %v", err)
	}
	if err := os.Chmod(externalDir, 0755); err != nil { // defeat umask
		t.Fatalf("failed to chmod external dir: %v", err)
	}
	// Drop a sentinel file inside to detect any accidental move/rename.
	sentinel := filepath.Join(externalDir, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("KEEP"), 0600); err != nil {
		t.Fatalf("failed to write sentinel: %v", err)
	}

	link := filepath.Join(dataDir, "escape.db")
	if err := os.Symlink(externalDir, link); err != nil {
		t.Fatalf("failed to create symlink-to-dir: %v", err)
	}

	if err := MigrateLegacyLayout(); err != nil {
		t.Fatalf("sweep must not fail on a symlink-to-dir: %v", err)
	}

	// External directory permissions UNCHANGED (not chmod-ed through the link).
	if err := VerifyPermissions(externalDir, 0755); err != nil {
		t.Errorf("external dir permissions must be unchanged (0755): %v", err)
	}
	// External directory not moved (sentinel still in place).
	if got := mustReadFile(t, sentinel); got != "KEEP" {
		t.Errorf("external dir must not be moved, sentinel got %q", got)
	}
	// Symlink untouched.
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("symlink must still exist: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("entry must still be a symlink, got mode %v", fi.Mode())
	}
	// No home directory created at ~/.roadmaps/escape.
	if _, err := os.Stat(filepath.Join(dataDir, "escape")); !os.IsNotExist(err) {
		t.Errorf("no home directory must be created for a symlink, stat err = %v", err)
	}
}

// TestMoveSidecars_FailureIsBestEffortWithDistinctWarning covers ISSUE-005:
// a sidecar (-wal/-shm) move that fails AFTER the authoritative database has
// already been relocated is best-effort. It must NOT report the roadmap as
// failed (moveSidecars returns no error / does not abort), the database is left
// in place at the current layout, and the warning must be the distinct
// "migrated, but sidecar ... could not be moved" form rather than the
// misleading "skipping migration of legacy roadmap" form.
//
// The failure is forced hermetically without dependency injection: the
// destination sidecar path is pre-created as a directory, so renaming the
// regular-file source onto it fails with EISDIR/EEXIST on POSIX. moveSidecars
// is the production code path the sweep uses for step 5; exercising it directly
// keeps the test deterministic while still covering the real branch.
func TestMoveSidecars_FailureIsBestEffortWithDistinctWarning(t *testing.T) {
	dataDir := withTempDataDir(t)

	const name = "inventory-service"
	roadmapDir := filepath.Join(dataDir, name)
	if err := os.MkdirAll(roadmapDir, DataDirPerm); err != nil {
		t.Fatalf("failed to create roadmap dir: %v", err)
	}
	legacyDB := filepath.Join(dataDir, name+".db")
	currentDB := filepath.Join(roadmapDir, DBFileName)

	// The authoritative database has already been moved into the current
	// layout (this is the state moveSidecars runs in: post DB rename).
	if err := os.WriteFile(currentDB, []byte("DBCONTENT-INVENTORY"), DBFilePerm); err != nil {
		t.Fatalf("failed to write migrated db: %v", err)
	}

	// Stage a legacy -wal sidecar that still needs moving...
	legacyWAL := legacyDB + "-wal"
	if err := os.WriteFile(legacyWAL, []byte("WAL-DATA"), 0600); err != nil {
		t.Fatalf("failed to write legacy wal: %v", err)
	}
	// ...and obstruct its destination by making it a non-empty directory, so
	// the rename of a regular file onto it fails on POSIX.
	currentWAL := currentDB + "-wal"
	if err := os.Mkdir(currentWAL, DataDirPerm); err != nil {
		t.Fatalf("failed to create obstructing dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(currentWAL, "blocker"), []byte("x"), 0600); err != nil {
		t.Fatalf("failed to populate obstructing dir: %v", err)
	}

	warnPath := filepath.Join(t.TempDir(), "warn.txt")
	warn, err := os.Create(warnPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("failed to create warn sink: %v", err)
	}

	// moveSidecars must be best-effort: it never aborts the migration on a
	// failed sidecar move; it emits a distinct warning and returns.
	moveSidecars(name, legacyDB, currentDB, warn)
	if err := warn.Close(); err != nil {
		t.Fatalf("failed to close warn sink: %v", err)
	}

	// The migrated database is untouched and still readable at the new path.
	if got := mustReadFile(t, currentDB); got != "DBCONTENT-INVENTORY" {
		t.Errorf("migrated db must be untouched, got %q", got)
	}

	out := mustReadFile(t, warnPath)
	// The warning is the DISTINCT sidecar form, not the "skipping" form.
	if !strings.Contains(out, "migrated") || !strings.Contains(out, "sidecar") {
		t.Errorf("warning must report a migrated roadmap with a sidecar problem, got %q", out)
	}
	if strings.Contains(out, "skipping migration") {
		t.Errorf("sidecar failure must NOT be reported as a skipped migration, got %q", out)
	}
	// It must name the roadmap and the offending sidecar so the user can act.
	if !strings.Contains(out, name) {
		t.Errorf("warning must name the roadmap %q, got %q", name, out)
	}
}

// TestMigrateOneRoadmap_OSFailureCarriesErrDatabase covers ISSUE-006: the
// non-fatal OS-error paths in the per-roadmap migration must carry the
// ErrDatabase sentinel so errors.Is recognises them, consistent with the Error
// Reuse Policy. The failure is forced by making the data directory read-only,
// so creating the roadmap home directory (the first mutation, before any
// rename) fails with EACCES. We assert errors.Is(err, ErrDatabase) without
// asserting exit-code behaviour (the sweep keeps surfacing these as non-fatal
// warnings).
func TestMigrateOneRoadmap_OSFailureCarriesErrDatabase(t *testing.T) {
	dataDir := withTempDataDir(t)

	const name = "billing-engine"
	legacyDB := filepath.Join(dataDir, name+".db")
	if err := os.WriteFile(legacyDB, []byte("DBCONTENT-BILLING"), 0600); err != nil {
		t.Fatalf("failed to write legacy db: %v", err)
	}

	// Make the data directory read-only so MkdirAll of the roadmap home
	// directory fails with EACCES. Restore writability on cleanup so t.TempDir
	// can be removed.
	if err := os.Chmod(dataDir, 0500); err != nil {
		t.Fatalf("failed to chmod data dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, DataDirPerm) })

	warn, err := os.CreateTemp(t.TempDir(), "warn-*.txt")
	if err != nil {
		t.Fatalf("failed to create warn sink: %v", err)
	}
	defer func() { _ = warn.Close() }()

	err = migrateOneRoadmap(dataDir, name, warn)
	if err == nil {
		// On some environments (e.g. running as root) a read-only directory is
		// still writable; skip rather than assert a false negative.
		t.Skip("environment permits writing to a read-only directory (likely running as root); cannot exercise the OS-failure path")
	}
	if !errors.Is(err, ErrDatabase) {
		t.Errorf("OS-failure path must wrap ErrDatabase for errors.Is; got %v", err)
	}
}
