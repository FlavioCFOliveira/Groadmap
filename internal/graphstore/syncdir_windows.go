//go:build windows

package graphstore

import "os"

// syncDir is a no-op on Windows, which exposes no synchronisation of a
// directory's entries: the creation, rename or removal of an entry becomes
// durable when the file system commits its own metadata journal. The engine
// whose store a backup copies relies on the same journal on this platform.
func syncDir(_ *os.Root, _ string) error { return nil }
