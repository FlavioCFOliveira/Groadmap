package utils

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// symlinkRefusalMessage is the invariant half of the refusal message: the text
// the user reads, which the sentinel reclassification (task #186) deliberately
// left untouched. Only the sentinel prefix that handleError renders changed.
const symlinkRefusalMessage = "is a symbolic link; refusing to use it as a roadmap directory"

// assertSymlinkRefusal is the single place that encodes the SPEC-mandated
// classification of a symlinked roadmap directory, so no call site can drift
// from it independently.
//
// SPEC/ARCHITECTURE.md states the rule twice — § Directory Structure, location
// rule 10, and § Security Guarantees — and both say the refusal fails with
// utils.ErrDatabase and exit code 1. cmd/rmp/handleError maps ErrDatabase to
// ExitFailure (1) and ErrInvalidInput to ExitMisuse (2), a mapping pinned by
// TestHandleError_SentinelErrors in cmd/rmp/main_test.go; asserting the sentinel
// here therefore pins the exit code. The end-to-end pin on the real process exit
// status lives in tests/test_42_security_audit.py (findings #72 and #75).
//
// The negative assertion is the load-bearing half: before task #186 the refusal
// carried ErrInvalidInput and exited 2, and exit 2 is documented as MISUSE — a
// syntax or flag error — which a symbolic link on disk is not.
func assertSymlinkRefusal(t *testing.T, err error, wantPath string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected a refusal, got nil")
	}
	if !errors.Is(err, ErrDatabase) {
		t.Errorf("refusal must wrap ErrDatabase (exit 1) per SPEC/ARCHITECTURE.md; got %v", err)
	}
	if errors.Is(err, ErrInvalidInput) {
		t.Errorf("refusal must NOT wrap ErrInvalidInput (exit 2, documented as MISUSE); got %v", err)
	}
	// The classification helpers must agree with errors.Is; they are what the
	// rest of the codebase reaches for.
	if !IsDatabase(err) || IsInvalidInput(err) {
		t.Errorf("IsDatabase/IsInvalidInput disagree with the sentinel chain: IsDatabase=%t IsInvalidInput=%t for %v",
			IsDatabase(err), IsInvalidInput(err), err)
	}
	// The message text itself is unchanged by the reclassification: it must
	// still name the offending path and say why it was refused.
	if !strings.Contains(err.Error(), symlinkRefusalMessage) {
		t.Errorf("refusal message lost its explanation; got %q, want it to contain %q", err.Error(), symlinkRefusalMessage)
	}
	if wantPath != "" && !strings.Contains(err.Error(), wantPath) {
		t.Errorf("refusal message does not name the offending path %q; got %q", wantPath, err.Error())
	}
	// The sentinel prefix handleError renders is part of what the user reads.
	if !strings.HasPrefix(err.Error(), ErrDatabase.Error()+": ") {
		t.Errorf("refusal must render with the %q prefix; got %q", ErrDatabase.Error(), err.Error())
	}
	// A sentinel must never appear twice in the rendered text.
	if n := strings.Count(err.Error(), ErrDatabase.Error()); n != 1 {
		t.Errorf("sentinel %q appears %d times in %q; want exactly 1", ErrDatabase.Error(), n, err.Error())
	}
}

// assertTargetUntouched verifies the security property the refusal exists to
// protect: the link's target keeps its permissions and receives no database.
// The refusal must happen BEFORE anything is read or written through the link,
// so this is asserted rather than assumed (security finding #72/#74/#75).
func assertTargetUntouched(t *testing.T, target string, wantPerm os.FileMode) {
	t.Helper()

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat link target %s: %v", target, err)
	}
	if got := info.Mode().Perm(); got != wantPerm {
		t.Errorf("link target %s was chmod-ed through the symlink: got %04o, want %04o", target, got, wantPerm)
	}
	if _, err := os.Stat(filepath.Join(target, DBFileName)); !os.IsNotExist(err) {
		t.Errorf("a database was written through the symlink into %s; stat err = %v", target, err)
	}
}

// TestAssertNotSymlink covers the three cases of the symlink guard: a real
// directory and a missing path both pass (return nil), while an existing
// symbolic link is refused with an ErrDatabase-wrapped error (exit 1).
func TestAssertNotSymlink(t *testing.T) {
	tmp := t.TempDir()

	t.Run("real directory passes", func(t *testing.T) {
		realDir := filepath.Join(tmp, "real")
		if err := os.Mkdir(realDir, 0o700); err != nil {
			t.Fatalf("creating real directory: %v", err)
		}
		if err := assertNotSymlink(realDir); err != nil {
			t.Fatalf("expected nil for real directory, got %v", err)
		}
	})

	t.Run("missing path passes", func(t *testing.T) {
		missing := filepath.Join(tmp, "does-not-exist")
		if err := assertNotSymlink(missing); err != nil {
			t.Fatalf("expected nil for missing path, got %v", err)
		}
	})

	t.Run("symlink is refused", func(t *testing.T) {
		target := filepath.Join(tmp, "external-target")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatalf("creating symlink target: %v", err)
		}
		link := filepath.Join(tmp, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("creating symlink: %v", err)
		}
		assertSymlinkRefusal(t, assertNotSymlink(link), link)
		assertTargetUntouched(t, target, 0o755)
	})
}

// TestSymlinkedRoadmapDirectoryExitsOne is the regression gate for task #186:
// the refusal of a symlinked data directory or roadmap home directory is
// classified ErrDatabase (exit code 1), as SPEC/ARCHITECTURE.md states in both
// § Directory Structure (location rule 10) and § Security Guarantees.
//
// It is a class gate rather than four scattered assertions: it exercises EVERY
// production entry point that reaches assertNotSymlink, so a call site that
// re-wraps the refusal into a different sentinel — as migrateOneRoadmap did
// before this task — is caught here rather than surviving because only one path
// happened to be tested.
//
// Each case also asserts that the link's target is untouched, because the
// exit-code contract is worthless if the refusal happens after the damage.
func TestSymlinkedRoadmapDirectoryExitsOne(t *testing.T) {
	// Every case plants a symlink somewhere under a private HOME, invokes a
	// production entry point, and returns the offending path plus the target
	// whose permissions must survive. The target is always created 0755 so a
	// chmod to 0700 through the link would be visible.
	cases := []struct {
		name string
		// run plants the symlink and invokes the entry point. It returns the
		// error, the path that must be named in the message, and the target.
		run func(t *testing.T, home string) (err error, offending, target string)
	}{
		{
			// path.go: EnsureDataDir -> assertNotSymlink(dataDir).
			// Defence in depth: for a real command the startup sweep refuses
			// first, but internal/web calls EnsureDataDir directly.
			name: "EnsureDataDir with symlinked ~/.roadmaps",
			run: func(t *testing.T, home string) (error, string, string) {
				target := mkTarget(t, home, "external-datadir")
				link := filepath.Join(home, DataDirName)
				mkLink(t, target, link)
				return EnsureDataDir(), link, target
			},
		},
		{
			// path.go: EnsureRoadmapDir -> assertNotSymlink(dir). This is the
			// path every roadmap-opening command takes, via db.openRoadmap.
			name: "EnsureRoadmapDir with symlinked ~/.roadmaps/<name>",
			run: func(t *testing.T, home string) (error, string, string) {
				dataDir := filepath.Join(home, DataDirName)
				if err := os.Mkdir(dataDir, DataDirPerm); err != nil {
					t.Fatalf("creating data directory: %v", err)
				}
				target := mkTarget(t, home, "external-roadmap")
				link := filepath.Join(dataDir, "production-backend")
				mkLink(t, target, link)
				return EnsureRoadmapDir("production-backend"), link, target
			},
		},
		{
			// migrate.go: the startup sweep's data-directory guard. This is
			// the fatal condition an invocation actually hits first, because
			// the sweep runs before command routing in main.go.
			name: "startup sweep with symlinked ~/.roadmaps",
			run: func(t *testing.T, home string) (error, string, string) {
				target := mkTarget(t, home, "external-sweep")
				link := filepath.Join(home, DataDirName)
				mkLink(t, target, link)
				return migrateLegacyLayout(os.Stderr), link, target
			},
		},
		{
			// migrate.go: the per-roadmap migration guard. This one is
			// surfaced as a non-fatal warning rather than an exit code, but
			// the sentinel must still be ErrDatabase so the classification is
			// uniform across all four call sites.
			name: "legacy migration with symlinked destination",
			run: func(t *testing.T, home string) (error, string, string) {
				dataDir := filepath.Join(home, DataDirName)
				if err := os.Mkdir(dataDir, DataDirPerm); err != nil {
					t.Fatalf("creating data directory: %v", err)
				}
				const name = "legacy-service"
				legacyDB := filepath.Join(dataDir, name+legacyDBSuffix)
				if err := os.WriteFile(legacyDB, []byte("SQLite format 3\x00"), DBFilePerm); err != nil {
					t.Fatalf("writing legacy database: %v", err)
				}
				target := mkTarget(t, home, "external-legacy")
				link := filepath.Join(dataDir, name)
				mkLink(t, target, link)

				err := migrateOneRoadmap(dataDir, name, os.Stderr)

				// The legacy database must be left exactly where it was: the
				// refusal precedes the rename, so there is no partial state.
				if _, statErr := os.Stat(legacyDB); statErr != nil {
					t.Errorf("legacy database must be left untouched; stat err = %v", statErr)
				}
				return err, link, target
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			err, offending, target := tc.run(t, home)
			assertSymlinkRefusal(t, err, offending)
			assertTargetUntouched(t, target, 0o755)
		})
	}
}

// mkTarget creates a link target OUTSIDE the data directory at 0755, a mode
// distinct from the 0700 rmp would apply, so following the link is detectable.
func mkTarget(t *testing.T, home, name string) string {
	t.Helper()

	target := filepath.Join(home, name)
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("creating link target: %v", err)
	}
	// Mkdir applies the umask, which would make the 0755 assertion a test of
	// the runner's environment rather than of rmp. Settle the mode explicitly.
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatalf("setting link target permissions: %v", err)
	}
	return target
}

// mkLink plants the attacker-controlled symbolic link.
func mkLink(t *testing.T, target, link string) {
	t.Helper()

	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("planting symlink at %s: %v", link, err)
	}
}

// TestEnsureDataDirRefusesSymlink verifies EnsureDataDir refuses when
// ~/.roadmaps already exists as a symbolic link (finding #75), so the os.Chmod
// can never harden permissions on the link's external target.
func TestEnsureDataDirRefusesSymlink(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	target := mkTarget(t, tmpHome, "external")
	// Plant a symlink at ~/.roadmaps pointing outside the data directory.
	link := filepath.Join(tmpHome, DataDirName)
	mkLink(t, target, link)

	assertSymlinkRefusal(t, EnsureDataDir(), link)
	assertTargetUntouched(t, target, 0o755)
}

// TestEnsureRoadmapDirRefusesSymlink verifies EnsureRoadmapDir refuses when
// ~/.roadmaps/<name> already exists as a symbolic link (finding #72), so the
// os.Chmod and the project.db write can never be redirected through the link.
func TestEnsureRoadmapDirRefusesSymlink(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dataDir := filepath.Join(tmpHome, DataDirName)
	if err := os.Mkdir(dataDir, DataDirPerm); err != nil {
		t.Fatalf("creating data directory: %v", err)
	}

	const name = "production-backend"
	target := mkTarget(t, tmpHome, "external-roadmap")
	// Plant a symlink at ~/.roadmaps/<name> pointing outside the data directory.
	link := filepath.Join(dataDir, name)
	mkLink(t, target, link)

	assertSymlinkRefusal(t, EnsureRoadmapDir(name), link)
	// The external target must remain untouched: no project.db redirected into
	// it, and no 0700 hardening applied through the link.
	assertTargetUntouched(t, target, 0o755)
}

// TestMigrateLegacyLayoutRefusesSymlinkedDataDir verifies that the startup
// migration sweep refuses (with ErrDatabase, exit 1) when ~/.roadmaps is itself
// a symbolic link, so the data-directory os.Chmod can never harden permissions
// on the link's external target (finding #75). The sweep runs before any
// command, so this is the first touch of the symlinked data directory.
func TestMigrateLegacyLayoutRefusesSymlinkedDataDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	target := mkTarget(t, tmpHome, "external")
	link := filepath.Join(tmpHome, DataDirName)
	mkLink(t, target, link)

	assertSymlinkRefusal(t, migrateLegacyLayout(os.Stderr), link)
	// The external target's permissions must be untouched (still 0755).
	assertTargetUntouched(t, target, 0o755)
}

// TestMigrateOneRoadmapSkipsSymlink verifies that a legacy migration whose
// destination ~/.roadmaps/<name> is a pre-planted symlink is SKIPPED with a
// non-fatal ErrDatabase error (finding #74), leaving the legacy database
// untouched and never moving it through the link.
func TestMigrateOneRoadmapSkipsSymlink(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dataDir := filepath.Join(tmpHome, DataDirName)
	if err := os.Mkdir(dataDir, DataDirPerm); err != nil {
		t.Fatalf("creating data directory: %v", err)
	}

	const name = "legacy-service"
	legacyDB := filepath.Join(dataDir, name+legacyDBSuffix)
	if err := os.WriteFile(legacyDB, []byte("SQLite format 3\x00"), DBFilePerm); err != nil {
		t.Fatalf("writing legacy database: %v", err)
	}

	// Plant a symlink at the destination roadmap directory pointing outside.
	target := mkTarget(t, tmpHome, "external-roadmap")
	link := filepath.Join(dataDir, name)
	mkLink(t, target, link)

	assertSymlinkRefusal(t, migrateOneRoadmap(dataDir, name, os.Stderr), link)

	// The legacy database must be left untouched (no partial state).
	if _, statErr := os.Stat(legacyDB); statErr != nil {
		t.Fatalf("expected legacy database to remain untouched, stat err = %v", statErr)
	}
	// Nothing must have been moved through the symlink into the external target.
	assertTargetUntouched(t, target, 0o755)
}

// TestMigrateLegacyLayoutSymlinkWarningIsNotDuplicated pins the user-visible
// text of the non-fatal per-roadmap warning. migrateOneRoadmap used to re-wrap
// the refusal as "%w: %w" with ErrDatabase; once assertNotSymlink itself carries
// ErrDatabase that wrap became tautological and would have rendered the sentinel
// twice ("database error: ...: database error"). The sweep must keep emitting a
// single, readable warning and must not become fatal.
func TestMigrateLegacyLayoutSymlinkWarningIsNotDuplicated(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dataDir := filepath.Join(tmpHome, DataDirName)
	if err := os.Mkdir(dataDir, DataDirPerm); err != nil {
		t.Fatalf("creating data directory: %v", err)
	}

	const name = "legacy-service"
	legacyDB := filepath.Join(dataDir, name+legacyDBSuffix)
	if err := os.WriteFile(legacyDB, []byte("SQLite format 3\x00"), DBFilePerm); err != nil {
		t.Fatalf("writing legacy database: %v", err)
	}
	target := mkTarget(t, tmpHome, "external-roadmap")
	mkLink(t, target, filepath.Join(dataDir, name))

	warnings := captureWarnings(t, func(warn *os.File) {
		if err := migrateLegacyLayout(warn); err != nil {
			t.Fatalf("a symlinked destination must be a per-roadmap skip, not fatal; got %v", err)
		}
	})

	if !strings.Contains(warnings, symlinkRefusalMessage) {
		t.Errorf("sweep must warn about the symlinked destination; got %q", warnings)
	}
	if n := strings.Count(warnings, ErrDatabase.Error()); n != 1 {
		t.Errorf("warning renders the %q sentinel %d times; want exactly 1. Got: %q",
			ErrDatabase.Error(), n, warnings)
	}
	// The whole point of the sweep continuing: the legacy file is still there,
	// and nothing went through the link.
	if _, err := os.Stat(legacyDB); err != nil {
		t.Errorf("legacy database must be left untouched; stat err = %v", err)
	}
	assertTargetUntouched(t, target, 0o755)
}

// captureWarnings runs fn with a temporary file as the warning sink and returns
// what was written to it. The sink is an *os.File because that is the type
// migrateLegacyLayout accepts.
func captureWarnings(t *testing.T, fn func(warn *os.File)) string {
	t.Helper()

	sink, err := os.CreateTemp(t.TempDir(), "warnings-*.txt")
	if err != nil {
		t.Fatalf("creating warning sink: %v", err)
	}
	defer func() {
		if cerr := sink.Close(); cerr != nil {
			t.Errorf("closing warning sink: %v", cerr)
		}
	}()

	fn(sink)

	data, err := os.ReadFile(sink.Name())
	if err != nil {
		t.Fatalf("reading warning sink: %v", err)
	}
	return string(data)
}
