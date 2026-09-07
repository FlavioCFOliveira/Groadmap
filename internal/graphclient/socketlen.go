// Package graphclient — the bound the platform puts on a socket path.
//
// It lives beside the derivation and the probe rather than in the server
// package, and the reason is the set of callers. Four surfaces resolve a socket
// — `rmp graph serve`, `rmp graph execute`, `rmp graph client` and the web graph
// data endpoint — and all four already reach this package for SocketPath and
// Resolve, which is what makes it the one place a caller and a server agree on
// what a roadmap's socket IS. internal/graphserve is reached by exactly one of
// them: putting the bound there would make internal/web import the Bolt server,
// its listener, its drain and its teardown in order to learn a number, and would
// leave the two packages that never touch a server depending on the one that is
// nothing but a server.
package graphclient

import "syscall"

// MaxSocketPathLen is the greatest number of BYTES a Unix domain socket path may
// occupy on the platform this binary was built for.
//
// # It is derived, and that is the whole point
//
// The bound belongs to the operating system and not to Groadmap: the kernel
// copies the path into the fixed-size sun_path field of its socket address
// structure, and a path that does not fit there — terminator included — can be
// neither bound nor connected to. The expression below IS that sentence: the
// capacity of the field, less one byte for the terminator that must have
// somewhere to go (SPEC/GRAPH.md § Socket Path Length).
//
// The expression is constant, so the figure is fixed at compile time for the
// target being built and costs nothing at run time. It is NOT the same figure on
// every target, and that is why it is never written out. Across the nine targets
// SPEC/BUILD.md declares, the same expression yields 107 on Linux and Windows and
// 103 on macOS, FreeBSD and OpenBSD — two figures over nine targets, from one
// expression, each confirmed by cross-compiling a two-sided compile-time
// assertion (TestDerivedLimitMatchesEverySupportedTarget). A literal 107 here
// would be correct on two of the five operating systems and would silently
// accept four unbindable paths on each of the other three, handing the caller
// back the very errno the published line exists to replace.
//
// syscall.RawSockaddrUnix is declared on every one of the nine targets, Windows
// included, so the expression needs no build tag and no fallback; the
// cross-compilation gate in cmd/rmp is what keeps that true.
const MaxSocketPathLen = len(syscall.RawSockaddrUnix{}.Path) - 1

// SocketPathTooLong reports whether path is longer than this platform allows a
// socket path to be.
//
// The length is counted in BYTES and never in runes. sun_path holds bytes, and a
// roadmap name may carry multi-byte UTF-8, so a path of a hundred characters can
// be well over the bound; a rune count would pass paths the kernel refuses, and
// would do so only for callers whose roadmap names are not ASCII — the worst way
// for a bound to be wrong, because it would look correct everywhere it was
// tested (SPEC/GRAPH.md § Socket Path Length, rule 2).
//
// The caller passes the RESOLVED path — the default derived from the roadmap, or
// a --socket value already made absolute — because the kernel measures the string
// the invocation will actually use and a check on anything earlier would be
// measuring something else (rule 1). What a caller DOES with a true answer is not
// decided here: it depends on who chose the path and on whether that surface has
// a second one, and the four surfaces answer differently (rules 5 and 6,
// § Server Resolution, rule 12).
func SocketPathTooLong(path string) bool { return len(path) > MaxSocketPathLen }
