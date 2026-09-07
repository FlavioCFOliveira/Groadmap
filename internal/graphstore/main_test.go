package graphstore

import (
	"os"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// TestMain makes this package's tests hermetic against the roadmap home.
//
// The tests here open stores under t.TempDir and resolve no roadmap, so nothing
// they do today reaches ~/.roadmaps. The TestMain is not for today: this package
// exports a function called Open, and internal/testenv's hermeticity gate flags
// an unqualified call to that name wherever it appears in a test file, on a bias
// it states and defends — "the cost is one TestMain and the cost of the opposite
// bias is a polluted home directory". This is that one TestMain, and it also
// covers the test added later that DOES resolve a roadmap.
//
// The two-function shape is the gate's own, and it is not a style: os.Exit skips
// deferred calls, so the restore has to happen inside a function that returns
// before the exit.
func TestMain(m *testing.M) { os.Exit(runTests(m)) }

func runTests(m *testing.M) int {
	restore := testenv.HermeticHome()
	defer restore()
	return m.Run()
}
