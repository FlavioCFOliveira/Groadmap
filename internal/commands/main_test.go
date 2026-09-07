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

// shortHome is a HOME under which a roadmap's DERIVED socket path fits, for a
// test that needs a home of its own rather than the package-wide one TestMain
// installs.
//
// t.TempDir() is the obvious way to get one and is the wrong way here: it names
// its directory after the TEST, so a descriptive name spends dozens of the bytes
// sun_path allows and <home>/.roadmaps/<name>/graph.sock lands over the platform's
// bound — at which point every graph surface refuses the roadmap outright
// (SPEC/GRAPH.md § Socket Path Length, rules 5 and 6). The refusal is correct and
// has nothing to do with what such a test is asserting, and whether a test meets
// it depends on how long its own name happens to be.
//
// The directory is testenv's, shared with internal/web's helper of the same name,
// so there is one implementation of "a home short enough to hold a socket"; what
// is here is the t.Fatalf and the t.Cleanup.
func shortHome(t *testing.T) string {
	t.Helper()

	home, remove, err := testenv.ShortHome()
	if err != nil {
		t.Fatalf("creating a short HOME: %v", err)
	}
	t.Cleanup(remove)
	return home
}
