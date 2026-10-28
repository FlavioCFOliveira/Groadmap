//go:build windows

package commands

import "testing"

// serveCLIChild reports false: the store-mode criteria it serves are POSIX-only
// (SPEC/GRAPH.md § Persistence Layout, rule 4), so Windows never starts the
// child.
func serveCLIChild() (exitCode int, isChild bool) { return 0, false }

// processUmask reports 0: Windows has no process umask.
func processUmask() int { return 0 }

// preserveUmask does nothing: Windows has no process umask to restore.
func preserveUmask(*testing.T) {}
