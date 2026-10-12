//go:build !windows

package graphstore

import "os"

// syncDir makes the entries of the directory name under root durable: a file
// created, renamed or removed inside it survives a crash once this returns. A
// backup relies on it for every directory it assembles and for the rename that
// publishes it (SPEC/GRAPH.md § Graph Store Format Migration, rule 5).
func syncDir(root *os.Root, name string) error {
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
