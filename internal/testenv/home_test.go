package testenv

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHermeticHomeRedirectsAndRestores covers the contract every TestMain in
// this module depends on: while it is installed, os.UserHomeDir resolves to a
// fresh empty directory; once restored, the previous value is back and the
// directory is gone.
func TestHermeticHomeRedirectsAndRestores(t *testing.T) {
	before, hadBefore := os.LookupEnv("HOME")

	restore := HermeticHome()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir under a hermetic home: %v", err)
	}
	if hadBefore && home == before {
		t.Fatalf("HOME is still %q; nothing was redirected, so tests would still write to the real home", home)
	}

	info, err := os.Stat(home)
	if err != nil {
		t.Fatalf("the hermetic home %q does not exist: %v", home, err)
	}
	if !info.IsDir() {
		t.Fatalf("the hermetic home %q is not a directory", home)
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("the hermetic home has permissions %04o, want 0700; test databases are written inside it", perm)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("reading the hermetic home: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("the hermetic home is not empty (%d entries); a run must not inherit a previous run's state", len(entries))
	}

	// A roadmap created now must land inside the temporary home, which is the
	// whole point of the redirection.
	roadmaps := filepath.Join(home, ".roadmaps", "payment-reconciliation")
	if err := os.MkdirAll(roadmaps, 0700); err != nil {
		t.Fatalf("creating a roadmap directory under the hermetic home: %v", err)
	}

	restore()

	after, hadAfter := os.LookupEnv("HOME")
	if hadAfter != hadBefore || after != before {
		t.Errorf("HOME after restore is (%q, set=%t), want (%q, set=%t)", after, hadAfter, before, hadBefore)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Errorf("the hermetic home %q survived restore (stat error: %v); the fix would trade "+
			"pollution of ~/.roadmaps for pollution of the temporary directory", home, err)
	}
}

// TestHermeticHomeRestoresAnUnsetHome covers the branch where HOME was not set
// in the first place. Restoring it to the empty string instead of unsetting it
// would leave the process in a state it did not start in.
func TestHermeticHomeRestoresAnUnsetHome(t *testing.T) {
	original, had := os.LookupEnv("HOME")
	t.Cleanup(func() {
		if had {
			if err := os.Setenv("HOME", original); err != nil {
				t.Errorf("restoring HOME: %v", err)
			}
			return
		}
		if err := os.Unsetenv("HOME"); err != nil {
			t.Errorf("unsetting HOME: %v", err)
		}
	})

	if err := os.Unsetenv("HOME"); err != nil {
		t.Fatalf("unsetting HOME for the test: %v", err)
	}

	restore := HermeticHome()
	if _, ok := os.LookupEnv("HOME"); !ok {
		t.Fatal("HOME is unset while the hermetic home is installed")
	}
	restore()

	if value, ok := os.LookupEnv("HOME"); ok {
		t.Errorf("HOME was unset before, but restore left it set to %q", value)
	}
}

// TestSweepStaleHomesReclaimsOnlyAbandonedHomes pins the sweep that bounds the
// one leak the deferred restore cannot cover: a panicking test kills the
// process without unwinding TestMain, so its home is left behind.
//
// The sweep must reclaim those without ever touching a home that a package
// running concurrently is still using, which is why the age threshold exists.
// Each case scans a private directory with an explicit clock, so the assertions
// are exact and no real test home is at risk.
func TestSweepStaleHomesReclaimsOnlyAbandonedHomes(t *testing.T) {
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)

	t.Run("an abandoned home older than the threshold is reclaimed", func(t *testing.T) {
		root := t.TempDir()
		abandoned := makeDir(t, root, homePrefix+"crashed-run")
		backdate(t, abandoned, now.Add(-staleAfter-time.Hour))

		sweepStaleHomes(root, now)

		if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
			t.Errorf("a home abandoned %v ago survived the sweep (stat error: %v); "+
				"leaked homes would accumulate in the temporary directory", staleAfter+time.Hour, err)
		}
	})

	t.Run("a home in use by a concurrent package is left alone", func(t *testing.T) {
		root := t.TempDir()
		// `go test ./...` runs packages in parallel, each with its own home.
		// A home younger than the threshold belongs to a run that may still be
		// executing, and removing it would break that run.
		live := makeDir(t, root, homePrefix+"live-run")
		backdate(t, live, now.Add(-staleAfter+time.Minute))

		sweepStaleHomes(root, now)

		if _, err := os.Stat(live); err != nil {
			t.Errorf("the sweep removed a home only %v old: %v; a package running "+
				"concurrently would have lost its home mid-run", staleAfter-time.Minute, err)
		}
	})

	t.Run("an unrelated directory is never touched", func(t *testing.T) {
		root := t.TempDir()
		unrelated := makeDir(t, root, "someone-elses-scratch-dir")
		backdate(t, unrelated, now.Add(-10*staleAfter))

		sweepStaleHomes(root, now)

		if _, err := os.Stat(unrelated); err != nil {
			t.Errorf("the sweep removed %q, which does not carry the test-home prefix: %v", unrelated, err)
		}
	})

	t.Run("a symbolic link wearing the prefix is not followed", func(t *testing.T) {
		root := t.TempDir()
		// The clock is pushed far past the threshold so age cannot be what
		// spares the link: only the directory-type check can. This is the
		// guard that stops a planted link from redirecting os.RemoveAll at
		// a directory outside the temporary area.
		target := t.TempDir()
		payload := filepath.Join(target, "important.db")
		if err := os.WriteFile(payload, []byte("payload"), 0600); err != nil {
			t.Fatalf("writing the link target's contents: %v", err)
		}

		link := filepath.Join(root, homePrefix+"planted-link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("creating the symbolic link: %v", err)
		}

		sweepStaleHomes(root, now.Add(100*staleAfter))

		if _, err := os.Stat(payload); err != nil {
			t.Errorf("the sweep followed a symbolic link and deleted its target's contents: %v", err)
		}
		if _, err := os.Lstat(link); err != nil {
			t.Errorf("the sweep removed the symbolic link itself: %v", err)
		}
	})

	t.Run("an unreadable root is tolerated", func(t *testing.T) {
		// The sweep is housekeeping. A temporary directory it cannot read must
		// not stop a test run, so this must return rather than panic.
		sweepStaleHomes(filepath.Join(t.TempDir(), "no-such-directory"), now)
	})
}

// makeDir creates a directory under root and returns its path.
func makeDir(t *testing.T, root, name string) string {
	t.Helper()

	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	return path
}

// backdate sets a directory's modification time so the sweep sees it as being
// of a chosen age.
func backdate(t *testing.T, path string, modTime time.Time) {
	t.Helper()

	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("backdating %s: %v", path, err)
	}
}
