package terminal

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsTerminalRejectsEveryNonTerminal pins the answer for every kind of file a
// command can find on standard input other than a terminal.
//
// os.DevNull is the case that decides the implementation. It IS a character
// device, so a check built on os.File.Stat and os.ModeCharDevice — the cheap
// answer this package deliberately does not give — reports true for it. The
// consequence is not academic: the same bit is set for /dev/zero, and reporting
// "terminal" for a source that supplies a megabyte of bytes per millisecond
// would refuse it as "no query supplied" (exit code 2) instead of as a query
// over the maximum length (exit code 6). This test fails the moment the check
// regresses to the mode bit.
func TestIsTerminalRejectsEveryNonTerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	defer func() { _ = devNull.Close() }()

	pipeRead, pipeWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating a pipe: %v", err)
	}
	defer func() { _ = pipeRead.Close() }()
	defer func() { _ = pipeWrite.Close() }()

	regularPath := filepath.Join(t.TempDir(), "query.cypher")
	if err := os.WriteFile(regularPath, []byte("MATCH (n) RETURN n"), 0o600); err != nil {
		t.Fatalf("writing the regular file: %v", err)
	}
	regular, err := os.Open(regularPath)
	if err != nil {
		t.Fatalf("opening the regular file: %v", err)
	}
	defer func() { _ = regular.Close() }()

	// A file whose descriptor is already closed: no system call can answer for
	// it, and the safe answer is false, which lets the caller read and fail on
	// the read rather than mistake it for a terminal.
	closed, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s a second time: %v", os.DevNull, err)
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("closing the second %s handle: %v", os.DevNull, err)
	}

	cases := []struct {
		file *os.File
		name string
	}{
		{name: "nil file", file: nil},
		{name: "os.DevNull, a NON-INTERACTIVE character device", file: devNull},
		{name: "the read end of a pipe", file: pipeRead},
		{name: "the write end of a pipe", file: pipeWrite},
		{name: "a regular file", file: regular},
		{name: "an already-closed file", file: closed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if IsTerminal(tc.file) {
				t.Errorf("IsTerminal(%s) = true, want false: only an interactive "+
					"terminal may be reported as one", tc.name)
			}
		})
	}
}
