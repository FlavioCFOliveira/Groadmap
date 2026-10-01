//go:build unix

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The tests in this file are unix-only because they depend on flock(2)
// conflicting between two open file descriptions of one process, and on
// symbolic links, which Windows grants only to privileged accounts.

// homeOf returns the absolute roadmap home of name.
func homeOf(t *testing.T, name string) string {
	t.Helper()
	dir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestRoadmapRemove_RefusedWhileAGraphServerHoldsTheLock pins
// SPEC/COMMANDS.md § Remove Roadmap, "A roadmap whose graph server is running
// is not removed": while another holder has graph/write.lock, the removal
// exits 6 with the published line and removes nothing; once the holder lets
// go, the removal succeeds.
func TestRoadmapRemove_RefusedWhileAGraphServerHoldsTheLock(t *testing.T) {
	const name = "remove-graph-locked"
	_, cleanup := setupTestTaskRoadmap(t, name)
	defer cleanup()
	home := homeOf(t, name)
	graphDir := filepath.Join(home, "graph")
	if err := os.Mkdir(graphDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A server holds the lock for its lifetime; a second open file description
	// in this process conflicts with it exactly as another process would.
	release, err := graphlock.TryExclusive(graphDir)
	if err != nil {
		t.Fatalf("taking the lock as the server would: %v", err)
	}
	sock := filepath.Join(home, "graph.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	before := listTree(t, home)

	stdout, err := dispatchInvocation(t, "roadmap", "remove", name)
	want := `validation error: cannot remove roadmap "` + name + `": a graph server is running for it; stop the server first`
	if err == nil || err.Error() != want || !errors.Is(err, utils.ErrValidation) {
		t.Fatalf("removal under a held lock: %v, want %q (exit 6)", err, want)
	}
	if stdout != "" {
		t.Errorf("the refusal wrote to stdout: %q", stdout)
	}
	if after := listTree(t, home); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("the refused removal changed the home directory:\n before %v\n after  %v", before, after)
	}

	release()
	if _, err := dispatchInvocation(t, "roadmap", "remove", name); err != nil {
		t.Fatalf("removal once the lock is free: %v", err)
	}
	if _, err := os.Lstat(home); !os.IsNotExist(err) {
		t.Errorf("the home directory survived the removal: %v", err)
	}
}

// TestRoadmapRemove_WithoutAGraphCreatesNothingAndRemoves covers rule 1 of the
// decision: a roadmap with no graph/ directory has no server, and finding that
// out creates nothing.
func TestRoadmapRemove_WithoutAGraphCreatesNothingAndRemoves(t *testing.T) {
	const name = "remove-no-graph"
	_, cleanup := setupTestTaskRoadmap(t, name)
	defer cleanup()
	home := homeOf(t, name)
	if _, err := dispatchInvocation(t, "roadmap", "remove", name); err != nil {
		t.Fatalf("removal: %v", err)
	}
	if _, err := os.Lstat(home); !os.IsNotExist(err) {
		t.Errorf("the home directory survived the removal: %v", err)
	}
}

// TestRoadmapRemove_ALockFileThatCannotBeTakenIsAGraphStoreFailure covers rule
// 4: an attempt that fails for any reason other than a lock another process
// holds carries ErrGraphStore, exit 1, and removes nothing.
func TestRoadmapRemove_ALockFileThatCannotBeTakenIsAGraphStoreFailure(t *testing.T) {
	const name = "remove-graph-unopenable"
	_, cleanup := setupTestTaskRoadmap(t, name)
	defer cleanup()
	home := homeOf(t, name)
	// A directory where the lock file belongs cannot be opened as one, for
	// any user, root included.
	if err := os.MkdirAll(filepath.Join(home, "graph", graphlock.LockFileName), 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := dispatchInvocation(t, "roadmap", "remove", name)
	if err == nil || !errors.Is(err, utils.ErrGraphStore) || errors.Is(err, utils.ErrValidation) {
		t.Fatalf("removal with an unopenable lock file: %v, want a graph store failure (exit 1)", err)
	}
	if _, err := os.Stat(filepath.Join(home, utils.DBFileName)); err != nil {
		t.Errorf("the failed removal deleted project.db: %v", err)
	}
}

// listTree returns every path under root, relative to it, sorted.
func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestRoadmapList_SkipsDirectoriesWithInvalidNames pins SPEC/COMMANDS.md § List
// Roadmaps, "Only a valid roadmap name is listed": a directory whose name
// breaks any roadmap name rule is skipped silently, whether or not it holds a
// project.db, and is neither read nor changed.
func TestRoadmapList_SkipsDirectoriesWithInvalidNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const valid = "payments-api"
	if _, err := dispatchInvocation(t, "roadmap", "create", valid); err != nil {
		t.Fatal(err)
	}
	dataDir, err := utils.GetDataDir()
	if err != nil {
		t.Fatal(err)
	}
	invalid := []string{"help", "con", "Payments", "-leading", "has space", strings.Repeat("a", 51)}
	for _, n := range invalid {
		dir := filepath.Join(dataDir, n)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, utils.DBFileName), []byte("not read"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	stdout, err := dispatchInvocation(t, "roadmap", "list")
	if err != nil {
		t.Fatal(err)
	}
	var listed []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("listing is not JSON: %v\n%s", err, stdout)
	}
	if len(listed) != 1 || listed[0].Name != valid {
		t.Errorf("listed %+v, want only %q", listed, valid)
	}
	for _, n := range invalid {
		data, err := os.ReadFile(filepath.Join(dataDir, n, utils.DBFileName))
		if err != nil || string(data) != "not read" {
			t.Errorf("the skipped entry %q was changed: %q, %v", n, data, err)
		}
	}
}

// TestRoadmapCreate_ASymlinkedProjectDBIsRefused pins the last paragraph of
// SPEC/COMMANDS.md § Create Roadmap: a project.db that is a symbolic link is
// neither a roadmap that exists nor a name to claim, and is refused with the
// symbolic-link line, exit 1, creating, changing and removing nothing.
func TestRoadmapCreate_ASymlinkedProjectDBIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const name = "linked-ledger"
	home := homeOf(t, name)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere.db")
	link := filepath.Join(home, utils.DBFileName)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := dispatchInvocation(t, "roadmap", "create", name)
	want := "database error: " + link + " is a symbolic link; refusing to use it as a roadmap database file"
	if err == nil || err.Error() != want || !errors.Is(err, utils.ErrDatabase) {
		t.Fatalf("create over a symlinked project.db: %v, want %q", err, want)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Errorf("the link target was created: %v", err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was not left as it was: %v", err)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 1 {
		t.Errorf("the refused create left %d entries in the home, want only the link", len(entries))
	}
}

// TestGraphServe_NeverCreatesTheRoadmapHome pins SPEC/GRAPH.md § Concurrency
// and Recovery, obligation 2: the server creates graph/ only inside a home
// directory that exists, so a startup that lost a race to a removal fails as
// the roadmap that does not exist and recreates nothing.
func TestGraphServe_NeverCreatesTheRoadmapHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const name = "removed-under-serve"
	home := homeOf(t, name)

	err := createGraphDir(name, filepath.Join(home, "graph"))
	if !errors.Is(err, utils.ErrNotFound) || err.Error() != `resource not found: roadmap "`+name+`" not found` {
		t.Errorf("createGraphDir under a missing home: %v, want the roadmap-not-found refusal", err)
	}
	if err := utils.SecureExistingRoadmapDir(name); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("SecureExistingRoadmapDir of a missing home: %v, want the roadmap-not-found refusal", err)
	}
	if _, err := os.Lstat(home); !os.IsNotExist(err) {
		t.Errorf("the roadmap home was created: %v", err)
	}
	if err := confirmRoadmapExists(name); !errors.Is(err, utils.ErrNotFound) {
		t.Errorf("the post-lock confirmation accepted a missing roadmap: %v", err)
	}
}
