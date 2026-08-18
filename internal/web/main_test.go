package web

import (
	"os"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// TestMain makes every test in this package hermetic by pointing HOME at a
// fresh temporary directory before any test runs.
//
// Unlike internal/db and internal/commands, this package was measured to be
// clean already: a full run created nothing under a scratch HOME, because every
// test that opens a database redirects HOME itself with t.Setenv. That
// cleanliness rests entirely on each author remembering, and the web tests open
// databases in more places than any other package. This TestMain turns the
// convention into a floor, so a test added later that forgets to redirect
// writes into a temporary directory instead of the developer's real
// ~/.roadmaps.
//
// The existing per-test t.Setenv("HOME", ...) calls keep working unchanged and
// still take precedence: t.Setenv restores the value installed here when the
// test finishes.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

// runTests exists so the deferred restore still runs when tests fail: os.Exit
// skips deferred calls, so it must be reached only after this function has
// returned. See testenv.HermeticHome for the guarantees and their limits.
func runTests(m *testing.M) int {
	restore := testenv.HermeticHome()
	defer restore()

	return m.Run()
}
