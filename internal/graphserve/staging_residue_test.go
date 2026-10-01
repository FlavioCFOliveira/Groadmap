//go:build !windows

// Acceptance criterion 84 (SPEC/GRAPH.md § Socket Path and Permissions, rules 8
// and 9; § Server Startup, step 4): the next server removes the staging residue
// a server killed while binding left beside the socket, and nothing else that
// carries the reserved prefix.
//
// Both halves are asserted, because each alone passes a wrong implementation:
// only the removals would pass one that removes everything carrying the prefix,
// and only the survivors would pass one that removes nothing.
package graphserve

import (
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// plantFile writes content at path and fails the test if it cannot.
func plantFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("planting %s: %v", path, err)
	}
}

// plantDir creates a private directory at path and fails the test if it cannot.
func plantDir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("planting %s: %v", path, err)
	}
}

// plantDeadSocket leaves a socket file at path on which nothing listens, which
// is what a server killed after its staged listen leaves inside its staging
// directory.
func plantDeadSocket(t *testing.T, path string) {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("binding %s: %v", path, err)
	}
	ln.SetUnlinkOnClose(false)
	if err := ln.Close(); err != nil {
		t.Fatalf("closing the listener at %s: %v", path, err)
	}
}

func TestServe_RemovesStagingResidueAndNothingElseCarryingThePrefix(t *testing.T) {
	root := graphRoot(t)
	entry := func(name string) string { return filepath.Join(root, name) }

	// Removed: an empty staging directory, and one holding a dead socket.
	emptyResidue := entry(".rmp-bind-a1b2c3")
	plantDir(t, emptyResidue)
	socketResidue := entry(".rmp-bind-d4e5f6")
	plantDir(t, socketResidue)
	plantDeadSocket(t, filepath.Join(socketResidue, "s"))

	// Left untouched: a prefixed directory holding a regular file, a prefixed
	// regular file, and a prefixed symbolic link to a directory — an EMPTY one,
	// so a removal that followed the link would find it qualifying.
	withFile := entry(".rmp-bind-g7h8i9")
	plantDir(t, withFile)
	plantFile(t, filepath.Join(withFile, "notes.txt"), "release checklist for the billing service\n")
	regular := entry(".rmp-bind-j0k1l2")
	plantFile(t, regular, "not a directory\n")
	linkTarget := entry("linked-target")
	plantDir(t, linkTarget)
	link := entry(".rmp-bind-m3n4o5")
	if err := os.Symlink(linkTarget, link); err != nil {
		t.Fatalf("planting %s: %v", link, err)
	}

	// Left untouched as well: a staging directory whose socket ACCEPTS a
	// connection, which is another server binding in the same directory now.
	live := entry(".rmp-bind-p6q7r8")
	plantDir(t, live)
	liveListener, err := net.Listen("unix", filepath.Join(live, "s"))
	if err != nil {
		t.Fatalf("binding the live staged socket: %v", err)
	}
	defer liveListener.Close() //nolint:errcheck // test teardown

	server := startServerProcess(t, root, "residue")
	mustSend(t, server.socket, "CREATE (:Decision {key:'staging-residue-removed'})")

	for _, gone := range []string{emptyResidue, socketResidue} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("the staging residue %s is still there after the server started (%v); a "+
				"directory of the reserved form, owned by this user, empty or holding one dead "+
				"socket, must be removed in the stale-socket step", gone, err)
		}
	}

	if got, err := os.ReadFile(filepath.Join(withFile, "notes.txt")); err != nil || string(got) != "release checklist for the billing service\n" {
		t.Errorf("the prefixed directory holding a regular file was changed: %q, %v", got, err)
	}
	if info, err := os.Lstat(regular); err != nil || !info.Mode().IsRegular() {
		t.Errorf("the prefixed regular file was removed or replaced (%v)", err)
	} else if got, _ := os.ReadFile(regular); string(got) != "not a directory\n" { //nolint:errcheck // an unreadable file fails the comparison
		t.Errorf("the prefixed regular file's content changed: %q", got)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the prefixed symbolic link was removed or replaced (%v)", err)
	} else if target, _ := os.Readlink(link); target != linkTarget { //nolint:errcheck // an unreadable link fails the comparison
		t.Errorf("the prefixed symbolic link now points at %q, want %q", target, linkTarget)
	}
	if info, err := os.Lstat(linkTarget); err != nil || !info.IsDir() {
		t.Errorf("the directory behind the prefixed symbolic link was removed (%v)", err)
	}
	if info, err := os.Lstat(filepath.Join(live, "s")); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Errorf("the staged socket another server is listening on was removed (%v)", err)
	}

	server.signalAndWait(t, syscall.SIGINT)
}
