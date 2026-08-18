package utils

import (
	"os"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// TestMain makes every test in this package hermetic by pointing HOME at a
// fresh temporary directory before any test runs.
//
// This package owns the path resolution itself: GetDataDir calls
// os.UserHomeDir, which reads HOME on Unix. Most tests here already redirect
// HOME through withTempDataDir, but TestEnsureDataDir did not, so it created
// the real ~/.roadmaps and then asserted its permissions — an assertion whose
// outcome depended on a directory the test did not create. Under this TestMain
// it creates and inspects a directory of its own, which is what it was always
// meant to verify.
//
// The redirection is done here rather than in each test because the defect
// returns the moment someone adds a test that forgets: a package-level TestMain
// covers tests added later automatically. withTempDataDir and the other
// t.Setenv("HOME", ...) call sites keep working unchanged — t.Setenv restores
// the value installed here when the test finishes.
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
