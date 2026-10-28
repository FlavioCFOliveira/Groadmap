//go:build windows

package graphstore

// SecureModes does nothing on Windows. A file's mode there toggles only its
// read-only attribute and grants and refuses no account anything; access to the
// graph store is governed by the access-control list its entries inherit from
// the roadmap home directory (SPEC/GRAPH.md § Persistence Layout, rule 4).
func SecureModes(string) error { return nil }
