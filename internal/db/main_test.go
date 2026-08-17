package db

import (
	"os"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// TestMain makes every test in this package hermetic by pointing HOME at a
// fresh temporary directory before any test runs.
//
// Open, OpenExisting and OpenReadOnly resolve ~/.roadmaps/<name>/project.db
// through os.UserHomeDir, so tests that call them without redirecting HOME
// created real roadmaps in the home directory of whoever ran `go test ./...`.
// TestOpen_ValidRoadmap, TestOpenExisting, TestConfigureConnection,
// TestPerConnectionPragmas, TestDBClose, TestRoadmapName,
// TestWithTransaction_Success, TestWithTransaction_RollbackOnError,
// TestGetSchemaVersion and TestGetEntityHistory each left one behind;
// TestOpenReadOnly_NoMigrationNoWrites went further and ran os.RemoveAll
// against a path inside that real home.
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
