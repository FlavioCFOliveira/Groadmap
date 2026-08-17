// Package testenv provides the test-support helpers that keep this module's
// package tests hermetic. It is imported only from _test.go files; no
// production code path depends on it.
//
// The problem it solves: rmp resolves ~/.roadmaps through os.UserHomeDir,
// which reads $HOME on Unix. A test that opens a roadmap without redirecting
// $HOME therefore writes into the real home directory of whoever runs
// `go test ./...`, leaving roadmap directories behind, making the suite's
// result depend on state outside the repository, and exercising a different
// directory from the one every other test uses.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// homePrefix is the basename prefix of every per-run test home directory
// created under the OS temporary directory. It is also the pattern the stale
// sweep matches, so it must stay specific enough not to collide with anything
// else in a shared /tmp.
const homePrefix = "groadmap-testhome-"

// staleAfter is how old a leftover test home must be before the sweep reclaims
// it. Package tests run concurrently under `go test ./...`, each with its own
// TestMain and its own home directory, so the sweep must never remove a
// directory that another package is still using. A threshold far longer than
// any plausible test run makes that impossible: a live home is written to
// continuously (creating ~/.roadmaps alone updates its modification time), so
// it can never look this old.
const staleAfter = 24 * time.Hour

// HermeticHome points $HOME at a fresh, empty, private directory under the OS
// temporary directory and returns a function that restores the previous $HOME
// and removes that directory. It is the mechanism behind every TestMain in this
// module.
//
// Call it from TestMain, not from a test. TestMain runs before any *testing.T
// exists, so t.Setenv is unavailable there; and t.Setenv cannot be called from
// a parallel test at all, which would leave parallel tests unprotected. Setting
// the variable once, before any test runs, covers every test in the package
// including ones added later, and composes with per-test t.Setenv("HOME", ...):
// t.Setenv saves whatever this function installed and restores it during test
// cleanup, so a test that wants its own home still gets one.
//
// Wire it so the cleanup survives a failing run, because os.Exit skips
// deferred calls:
//
//	func TestMain(m *testing.M) { os.Exit(runTests(m)) }
//
//	func runTests(m *testing.M) int {
//		restore := testenv.HermeticHome()
//		defer restore()
//		return m.Run()
//	}
//
// Cleanup is guaranteed when m.Run reports failures, since os.Exit is called
// after runTests has returned and its deferred restore has run. It is NOT
// guaranteed when a test panics: the panic is re-raised on the test's own
// goroutine and terminates the process without unwinding TestMain, so no
// deferred code of any kind executes. That case is covered instead by the
// sweep below, which reclaims homes left by earlier crashed runs. The leak is
// therefore bounded, self-healing, and confined to the OS temporary directory
// rather than the developer's ~/.roadmaps.
//
// It panics if the temporary home cannot be created or installed. Continuing
// would silently fall back to the real home directory, which is exactly the
// defect this helper exists to prevent.
func HermeticHome() func() {
	sweepStaleHomes(os.TempDir(), time.Now())

	// os.MkdirTemp generates an unpredictable name and creates the directory
	// with 0700, so no other user can pre-create or read it. That matters
	// because test databases are written inside it.
	home, err := os.MkdirTemp("", homePrefix)
	if err != nil {
		panic(fmt.Sprintf("testenv: creating a temporary HOME: %v", err))
	}

	previous, had := os.LookupEnv("HOME")
	if err := os.Setenv("HOME", home); err != nil {
		_ = os.RemoveAll(home)
		panic(fmt.Sprintf("testenv: setting HOME to %s: %v", home, err))
	}

	return func() {
		if had {
			_ = os.Setenv("HOME", previous)
		} else {
			_ = os.Unsetenv("HOME")
		}
		_ = os.RemoveAll(home)
	}
}

// sweepStaleHomes removes test home directories left behind by earlier runs
// that died before their cleanup could run (a panicking test terminates the
// process outright). Only entries older than staleAfter are touched, so a home
// belonging to a package running concurrently right now is never removed.
//
// The directory to scan and the current time are parameters so the sweep can be
// tested against a private directory and a fake clock. Passing os.TempDir() and
// a real clock, as HermeticHome does, is the production configuration; a test
// that faked the clock while scanning the real temporary directory would delete
// the live home of whichever package happened to be running beside it.
//
// Every error is ignored on purpose: the sweep is opportunistic housekeeping,
// and a temporary directory that cannot be read or removed must not fail an
// otherwise healthy test run.
func sweepStaleHomes(root string, now time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), homePrefix) {
			continue
		}
		// Use the DirEntry's own type rather than a stat of the path: a
		// symbolic link planted under this name must not be followed, and
		// must not be mistaken for one of our directories.
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < staleAfter {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, entry.Name()))
	}
}
