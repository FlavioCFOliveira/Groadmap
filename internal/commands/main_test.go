package commands

import (
	"os"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// TestMain makes every test in this package hermetic by pointing HOME at a
// fresh temporary directory before any test runs.
//
// The command handlers create, open and remove roadmaps under ~/.roadmaps,
// which os.UserHomeDir resolves from HOME. Without this redirection the
// helpers setupTestRoadmap, setupTestTaskRoadmap, setupTestStatsRoadmap,
// setupTestGraphRoadmap and cleanupIntegrationTest operated on the real home
// directory of whoever ran `go test ./...`: they created roadmaps there and
// then ran os.RemoveAll over paths inside it. No roadmap survived a passing
// run because each helper cleans up after itself, but a test that failed
// before its cleanup ran would leave one behind, and utils.EnsureDataDir
// created the real ~/.roadmaps in any case.
//
// The redirection is done here rather than in each test because the defect
// returns the moment someone adds a test that forgets: a package-level TestMain
// covers tests added later automatically. Tests that need a home of their own
// still call t.Setenv("HOME", ...) and keep working — t.Setenv restores the
// value installed here when the test finishes.
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
