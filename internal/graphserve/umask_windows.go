//go:build windows

package graphserve

// RestrictUmask does nothing on Windows, which has no process umask. Access to
// the graph store there is governed by the access-control list its entries
// inherit from the roadmap home directory (SPEC/GRAPH.md § Persistence Layout,
// rule 4).
func RestrictUmask() {}
