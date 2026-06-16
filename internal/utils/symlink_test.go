package utils

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestAssertNotSymlink covers the three cases of the symlink guard: a real
// directory and a missing path both pass (return nil), while an existing
// symbolic link is refused with an ErrInvalidInput-wrapped error.
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
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatalf("creating symlink target: %v", err)
		}
		link := filepath.Join(tmp, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("creating symlink: %v", err)
		}
		err := assertNotSymlink(link)
		if err == nil {
			t.Fatal("expected error for symlink, got nil")
		}
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput in chain, got %v", err)
		}
	})
}

// TestEnsureDataDirRefusesSymlink verifies EnsureDataDir refuses when
// ~/.roadmaps already exists as a symbolic link (finding #75), so the os.Chmod
// can never harden permissions on the link's external target.
func TestEnsureDataDirRefusesSymlink(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	target := filepath.Join(tmpHome, "external")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("creating external target: %v", err)
	}
	// Plant a symlink at ~/.roadmaps pointing outside the data directory.
	if err := os.Symlink(target, filepath.Join(tmpHome, DataDirName)); err != nil {
		t.Fatalf("planting symlink at data dir: %v", err)
	}

	err := EnsureDataDir()
	if err == nil {
		t.Fatal("expected EnsureDataDir to refuse a symlinked data directory, got nil")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput in chain, got %v", err)
	}
}

// TestEnsureRoadmapDirRefusesSymlink verifies EnsureRoadmapDir refuses when
// ~/.roadmaps/<name> already exists as a symbolic link (finding #72), so the
// os.Chmod and the project.db write can never be redirected through the link.
func TestEnsureRoadmapDirRefusesSymlink(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dataDir := filepath.Join(tmpHome, DataDirName)
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatalf("creating data directory: %v", err)
	}

	const name = "production-backend"
	target := filepath.Join(tmpHome, "external-roadmap")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("creating external target: %v", err)
	}
	// Plant a symlink at ~/.roadmaps/<name> pointing outside the data directory.
	if err := os.Symlink(target, filepath.Join(dataDir, name)); err != nil {
		t.Fatalf("planting symlink at roadmap dir: %v", err)
	}

	err := EnsureRoadmapDir(name)
	if err == nil {
		t.Fatal("expected EnsureRoadmapDir to refuse a symlinked roadmap directory, got nil")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput in chain, got %v", err)
	}

	// The external target must remain untouched: no project.db redirected into it.
	if _, statErr := os.Stat(filepath.Join(target, DBFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("expected no project.db written through the symlink, stat err = %v", statErr)
	}
}

// TestMigrateLegacyLayoutRefusesSymlinkedDataDir verifies that the startup
// migration sweep refuses (with ErrInvalidInput) when ~/.roadmaps is itself a
// symbolic link, so the data-directory os.Chmod can never harden permissions
// on the link's external target (finding #75). The sweep runs before any
// command, so this is the first touch of the symlinked data directory.
func TestMigrateLegacyLayoutRefusesSymlinkedDataDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	target := filepath.Join(tmpHome, "external")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("creating external target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(tmpHome, DataDirName)); err != nil {
		t.Fatalf("planting symlink at data dir: %v", err)
	}

	err := migrateLegacyLayout(os.Stderr)
	if err == nil {
		t.Fatal("expected migrateLegacyLayout to refuse a symlinked data directory, got nil")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput in chain, got %v", err)
	}

	// The external target's permissions must be untouched (still 0755).
	info, statErr := os.Stat(target)
	if statErr != nil {
		t.Fatalf("stat external target: %v", statErr)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected external target to keep 0755, got %04o", info.Mode().Perm())
	}
}

// TestMigrateOneRoadmapSkipsSymlink verifies that a legacy migration whose
// destination ~/.roadmaps/<name> is a pre-planted symlink is SKIPPED with a
// non-fatal ErrDatabase error (finding #74), leaving the legacy database
// untouched and never moving it through the link.
func TestMigrateOneRoadmapSkipsSymlink(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	dataDir := filepath.Join(tmpHome, DataDirName)
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatalf("creating data directory: %v", err)
	}

	const name = "legacy-service"
	legacyDB := filepath.Join(dataDir, name+legacyDBSuffix)
	if err := os.WriteFile(legacyDB, []byte("SQLite format 3\x00"), DBFilePerm); err != nil {
		t.Fatalf("writing legacy database: %v", err)
	}

	// Plant a symlink at the destination roadmap directory pointing outside.
	target := filepath.Join(tmpHome, "external-roadmap")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("creating external target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dataDir, name)); err != nil {
		t.Fatalf("planting symlink at roadmap dir: %v", err)
	}

	err := migrateOneRoadmap(dataDir, name, os.Stderr)
	if err == nil {
		t.Fatal("expected migrateOneRoadmap to skip a symlinked destination, got nil")
	}
	if !errors.Is(err, ErrDatabase) {
		t.Fatalf("expected ErrDatabase in chain, got %v", err)
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput in chain, got %v", err)
	}

	// The legacy database must be left untouched (no partial state).
	if _, statErr := os.Stat(legacyDB); statErr != nil {
		t.Fatalf("expected legacy database to remain untouched, stat err = %v", statErr)
	}
	// Nothing must have been moved through the symlink into the external target.
	if _, statErr := os.Stat(filepath.Join(target, DBFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("expected no project.db moved through the symlink, stat err = %v", statErr)
	}
}
