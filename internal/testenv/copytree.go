package testenv

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// SingleFileStoreFixture is the path, relative to the module root, of a graph
// store written by `rmp graph serve` built against the engine that kept the
// write-ahead log in the single-file format: nodes of four labels, relationships
// of three types, an index, a snapshot taken by that server's shutdown fold, and
// one acknowledged write in the single-file log that no snapshot covers. The
// tests of SPEC/GRAPH.md § Graph Store Format Migration start from copies of it.
const SingleFileStoreFixture = "internal/graphstore/testdata/singlefile-store"

// CopyTree copies the directory tree at src to dst, which must not exist yet,
// with every directory at 0700 and every file at 0600 — the modes a roadmap's
// graph store carries. It copies regular files and directories only, reading
// through an os.Root so nothing outside src is reached, and it is how a test
// takes a private copy of a fixture it must not change.
func CopyTree(tb testing.TB, src, dst string) {
	tb.Helper()

	if err := os.MkdirAll(dst, 0o700); err != nil {
		tb.Fatalf("creating %s: %v", dst, err)
	}
	from, err := os.OpenRoot(src)
	if err != nil {
		tb.Fatalf("opening %s: %v", src, err)
	}
	defer from.Close() //nolint:errcheck // a directory handle opened for reading
	to, err := os.OpenRoot(dst)
	if err != nil {
		tb.Fatalf("opening %s: %v", dst, err)
	}
	defer to.Close() //nolint:errcheck // every file written through it is closed by WriteFile

	err = fs.WalkDir(from.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return to.MkdirAll(filepath.FromSlash(rel), 0o700)
		}
		content, err := from.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			return err
		}
		return to.WriteFile(filepath.FromSlash(rel), content, 0o600)
	})
	if err != nil {
		tb.Fatalf("copying %s to %s: %v", src, dst, err)
	}
}
