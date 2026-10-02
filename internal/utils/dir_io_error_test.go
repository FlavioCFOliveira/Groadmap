package utils

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Regression fence for the classification rmp task #562 moved to its owner: a
// failure to create, bring to 0700, or verify the data directory or a roadmap
// home directory is utils.ErrIO (SPEC/ARCHITECTURE.md § Error Reuse Policy
// (Mandatory); SPEC/GRAPH.md § Server Startup, step 1).
//
// Two properties, and the second is why the first could be added at the source
// at all. The failure is ErrIO wherever it is reached from — `rmp graph serve`
// relies on it to fail its start with that class. And the TEXT is unchanged: the
// command line prints err.Error() after "Error: ", so a classification that
// prepended the sentinel's own text would change the line every command that
// opens a roadmap prints for this failure. Each case below therefore asserts the
// exact message as well as the class.

// TestEnsureDataDir_ClassifiesACreationFailureAsIOWithoutChangingItsText drives
// the one failure an unprivileged test can provoke on the data directory: a home
// directory the process may not write into, so ~/.roadmaps cannot be created.
func TestEnsureDataDir_ClassifiesACreationFailureAsIOWithoutChangingItsText(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatalf("this test provokes a permission refusal, which the superuser never receives; " +
			"run the suite as an unprivileged user")
	}

	home := t.TempDir()
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatalf("making %s unwritable: %v", home, err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) }) //nolint:errcheck // lets t.TempDir remove it
	t.Setenv("HOME", home)

	err := EnsureDataDir()
	if err == nil {
		t.Fatalf("EnsureDataDir succeeded inside an unwritable home directory")
	}
	if !errors.Is(err, ErrIO) {
		t.Errorf("the failure is not classified as ErrIO: %v", err)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("the operating system's own error left the chain: %v", err)
	}

	dataDir := filepath.Join(home, DataDirName)
	want := "creating data directory " + dataDir + ": mkdir " + dataDir + ": permission denied"
	if err.Error() != want {
		t.Errorf("the message changed with the classification\n got:  %q\n want: %q", err.Error(), want)
	}
}

// TestEnsureRoadmapDir_ClassifiesACreationFailureAsIOWithoutChangingItsText
// drives the roadmap home's own failure, past a data directory that is fine: a
// regular file planted where the roadmap home must be.
func TestEnsureRoadmapDir_ClassifiesACreationFailureAsIOWithoutChangingItsText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dataDir := filepath.Join(home, DataDirName)
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", dataDir, err)
	}
	roadmapDir := filepath.Join(dataDir, "billing-platform")
	if err := os.WriteFile(roadmapDir, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatalf("planting a file at %s: %v", roadmapDir, err)
	}

	err := EnsureRoadmapDir("billing-platform")
	if err == nil {
		t.Fatalf("EnsureRoadmapDir succeeded over a regular file at the roadmap home")
	}
	if !errors.Is(err, ErrIO) {
		t.Errorf("the failure is not classified as ErrIO: %v", err)
	}

	want := "creating roadmap directory " + roadmapDir + ": mkdir " + roadmapDir + ": not a directory"
	if err.Error() != want {
		t.Errorf("the message changed with the classification\n got:  %q\n want: %q", err.Error(), want)
	}
}

// TestEnsureRoadmapDir_KeepsTheSymlinkRefusalItsOwnClass pins the boundary of
// the classification: a symbolic link at the roadmap home is refused as
// ErrDatabase (SPEC/ARCHITECTURE.md § Directory Structure, rule 10), and it must
// not also become ErrIO — two classes for one refusal would let the exit-code map
// pick either.
func TestEnsureRoadmapDir_KeepsTheSymlinkRefusalItsOwnClass(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dataDir := filepath.Join(home, DataDirName)
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", dataDir, err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(dataDir, "billing-platform")); err != nil {
		t.Fatalf("planting a symbolic link: %v", err)
	}

	err := EnsureRoadmapDir("billing-platform")
	if !errors.Is(err, ErrDatabase) {
		t.Fatalf("the symbolic link was not refused as ErrDatabase: %v", err)
	}
	if errors.Is(err, ErrIO) {
		t.Errorf("the symbolic-link refusal also carries ErrIO: %v", err)
	}
}
