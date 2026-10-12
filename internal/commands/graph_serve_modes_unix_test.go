//go:build !windows

// Acceptance criteria 94 and 95 of SPEC/GRAPH.md (rmp task #603): every entry of
// the graph store is owner-only from its creation, and an entry a store already
// carries at a wider mode is repaired before the store is opened
// (§ Persistence Layout, rule 4; § Server Startup, steps 1 and 6).
//
// # Why the server is this package's CLI entry point, in a child process
//
// Both criteria start `rmp graph serve` under a umask of 0000, and the umask
// `rmp graph serve` sets is the first action of runGraphServe, in this package —
// internal/graphserve.Run, which the other child harnesses run, begins at step 2
// and would never set it. A test that ran Run would therefore pass an
// implementation that sets no umask at all. The child here runs runGraphServe,
// the whole of the subcommand, after setting its own umask to 0000: that is the
// umask a caller's shell would hand the process.
//
// The child is a process for the reason every server in these suites is: the
// umask is process-wide, and the server is stopped by SIGTERM.
package commands

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
	"github.com/FlavioCFOliveira/Groadmap/internal/signals"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// serveCLIChildEnv, when set, turns a run of this test binary into
// `rmp graph serve -r <value>` started under a umask of 0000.
const serveCLIChildEnv = "GROADMAP_COMMANDS_TEST_SERVE_CLI"

// serveCLIChild runs the child server when the parent asked for one, and reports
// false on every ordinary run.
func serveCLIChild() (exitCode int, isChild bool) {
	roadmap := os.Getenv(serveCLIChildEnv)
	if roadmap == "" {
		return 0, false
	}
	// The umask the criteria start the server under. runGraphServe must narrow
	// it itself; nothing else in this process does.
	syscall.Umask(0)
	// cmd/rmp/main.go's default disposition, so a signal before the server
	// takes the signals over behaves as it does for the real command.
	signals.Install(func(os.Signal) { os.Exit(130) })

	if err := runGraphServe([]string{"-r", roadmap}); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1, true
	}
	return 0, true
}

// processUmask reads the process umask without changing it. syscall.Umask only
// sets, returning the previous value, so the read is a set and a restore; it is
// called where nothing else in the process creates files concurrently.
func processUmask() int {
	previous := syscall.Umask(0)
	syscall.Umask(previous)
	return previous
}

// preserveUmask restores the process umask, when the test ends, to the value it
// has now. Every in-process call of runGraphServe MUST be preceded by it:
// runGraphServe narrows the umask to 0077 for the life of the process
// (SPEC/GRAPH.md § Server Startup, step 1), which in a test binary would narrow
// the files every later test creates. TestMain fails the run if a test leaves
// the umask changed, so a caller that forgets this is caught.
func preserveUmask(t *testing.T) {
	t.Helper()
	previous := processUmask()
	t.Cleanup(func() { syscall.Umask(previous) })
}

// modeServer is one child `rmp graph serve`.
type modeServer struct {
	cmd    *exec.Cmd
	stderr string
	socket string
	exited chan error
}

// startModeServer starts the child for roadmap and waits until it serves.
func startModeServer(t *testing.T, roadmap string) *modeServer {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary to re-execute: %v", err)
	}
	socket, err := graphSocketInForce(roadmap, "")
	if err != nil {
		t.Fatalf("resolving the socket of roadmap %q: %v", roadmap, err)
	}
	s := &modeServer{
		stderr: filepath.Join(t.TempDir(), "serve.err"),
		socket: socket,
		exited: make(chan error, 1),
	}
	errOut, err := os.Create(s.stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer errOut.Close() //nolint:errcheck // the child holds its own descriptor

	s.cmd = exec.Command(self)
	s.cmd.Stderr = errOut
	s.cmd.Env = append(os.Environ(), serveCLIChildEnv+"="+roadmap)
	if err := s.cmd.Start(); err != nil {
		t.Fatalf("starting the child server: %v", err)
	}
	go func() { s.exited <- s.cmd.Wait() }()
	t.Cleanup(func() {
		_ = s.cmd.Process.Kill() //nolint:errcheck // a child already gone reports an error that changes nothing
	})

	deadline := time.Now().Add(30 * time.Second)
	for {
		if state, _ := graphclient.Resolve(context.Background(), socket); state.Served() {
			return s
		}
		select {
		case err := <-s.exited:
			t.Fatalf("the child server exited before it served: %v\nstderr:\n%s", err, s.diagnostics(t))
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child server did not serve %s within 30s.\nstderr:\n%s", socket, s.diagnostics(t))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *modeServer) diagnostics(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(s.stderr)
	if err != nil {
		return "<unreadable: " + err.Error() + ">"
	}
	return string(raw)
}

// stop sends SIGTERM and waits for a clean exit.
func (s *modeServer) stop(t *testing.T) {
	t.Helper()
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signalling the child server: %v", err)
	}
	select {
	case err := <-s.exited:
		if err != nil {
			t.Fatalf("the child server exited with %v after SIGTERM, want 0.\nstderr:\n%s", err, s.diagnostics(t))
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("the child server did not exit within 30s of SIGTERM.\nstderr:\n%s", s.diagnostics(t))
	}
}

// modeWatch is what the observations of a graph directory found.
type modeWatch struct {
	graphDir   string
	mu         sync.Mutex
	violations []string
	// seen is every entry observed at least once, relative to graphDir, with
	// its kind; it is what makes a clean run mean something.
	seen  map[string]string
	walks atomic.Int64
}

func newModeWatch(graphDir string) *modeWatch {
	return &modeWatch{graphDir: graphDir, seen: map[string]string{}}
}

// observe walks the graph directory once and records every entry at a mode the
// rule forbids. An entry that disappears during the walk — a checkpoint renames
// and removes directories as it runs — is skipped: it is the entries that EXIST
// that must be private.
func (w *modeWatch) observe() {
	_ = filepath.WalkDir(w.graphDir, func(path string, d fs.DirEntry, err error) error { //nolint:errcheck // every error is an entry that vanished mid-walk, skipped below
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(w.graphDir, path) //nolint:errcheck // path is below graphDir by construction
		mode := info.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
		var kind string
		var want os.FileMode
		switch {
		case info.IsDir():
			kind, want = "directory", 0o700
		case info.Mode().IsRegular():
			kind, want = "file", 0o600
		default:
			return nil
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		w.seen[rel] = kind
		if mode != want && len(w.violations) < 20 {
			// Each distinct violation is reported once, however often it is
			// observed.
			if v := fmt.Sprintf("%s %s at %#o, want %#o", kind, rel, mode, want); !slices.Contains(w.violations, v) {
				w.violations = append(w.violations, v)
			}
		}
		return nil
	})
	w.walks.Add(1)
}

// watchUntil observes the graph directory repeatedly until stop is closed.
func (w *modeWatch) watchUntil(stop <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			w.observe()
		}
	}()
	return done
}

// report fails the test with every violation observed.
func (w *modeWatch) report(t *testing.T, when string) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.violations) > 0 {
		t.Errorf("%s, entries of graph/ were observed at a mode the store forbids (first %d shown):\n  %s\n"+
			"Every directory of the graph store is 0700 and every regular file 0600 from its creation "+
			"(SPEC/GRAPH.md § Persistence Layout, rule 4)",
			when, len(w.violations), strings.Join(w.violations, "\n  "))
	}
}

// subdirectoriesOf lists the directories directly inside dir.
func subdirectoriesOf(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listing %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// TestGraphServe_EveryStoreEntryIsOwnerOnlyFromCreation is acceptance criterion
// 94. The server is started under a umask of 0000 for a roadmap with no graph;
// an index is declared, nodes and relationships carrying its label and property
// are created and one node deleted, so the first checkpoint — the shutdown one —
// writes the index directory and the optional snapshot components. The store is
// observed from the announcement until the exit and once afterwards.
//
// Without the umask the engine creates snapshot/, its staging directory and
// snapshot/indexes/ at 0750, and the watch sees them there.
func TestGraphServe_EveryStoreEntryIsOwnerOnlyFromCreation(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const roadmap = "store-modes"
	defer setupTestGraphRoadmap(t, roadmap)()
	graphDir := graphDirOf(t, roadmap)
	if graphDirExists(t, roadmap) {
		t.Fatalf("the roadmap already has a graph at %s; the criterion starts from none", graphDir)
	}

	server := startModeServer(t, roadmap)
	watch := newModeWatch(graphDir)
	stop := make(chan struct{})
	watching := watch.watchUntil(stop)

	for _, statement := range []string{
		"CREATE INDEX component_key FOR (n:Component) ON (n.key)",
		"CREATE (:Component {key: 'internal/graphserve'})-[:DEPENDS_ON]->(:Component {key: 'internal/graphstore'})",
		"CREATE (:Component {key: 'internal/commands'})-[:DEPENDS_ON]->(:Component {key: 'internal/graphclient'})",
		"CREATE (:Component {key: 'internal/specialists'})",
		"MATCH (n:Component {key: 'internal/specialists'}) DELETE n",
	} {
		graphClientJSON(t, roadmap, statement)
	}

	server.stop(t)
	close(stop)
	<-watching
	watch.report(t, "while the server ran and while it stopped")

	after := newModeWatch(graphDir)
	after.observe()
	after.report(t, "after the server exited")

	if _, err := os.Lstat(filepath.Join(graphDir, "snapshot", "manifest.json")); err != nil {
		t.Fatalf("snapshot/manifest.json is missing after the exit (%v): the checkpoint the criterion "+
			"observes never ran.\nstderr:\n%s", err, server.diagnostics(t))
	}
	if subs := subdirectoriesOf(t, filepath.Join(graphDir, "snapshot")); len(subs) == 0 {
		t.Errorf("snapshot/ holds no subdirectory after the exit, so the checkpoint created no directory " +
			"below it and the criterion observed none")
	}
	if watch.walks.Load() < 2 {
		t.Errorf("the store was walked %d times while the server ran; the criterion requires repeated "+
			"observation", watch.walks.Load())
	}
	t.Logf("%d walks while the server ran; entries seen: %v", watch.walks.Load(), after.seen)
}

// TestGraphServe_AWiderStoreIsRepairedBeforeItIsOpened is acceptance criterion
// 95. The single-file fixture of criterion 88 is given a wider snapshot/, a wider
// directory below it and a wider file below it; the server is started under a
// umask of 0000, converts the store, answers the fixture's count, and is
// stopped. From the announcement on, and after the exit, every entry is
// owner-only, those the conversion created included, and no backup remains.
//
// Without the repair the wider entries are still wider when the server serves;
// without the umask, the conversion's new entries are created under 0000.
func TestGraphServe_AWiderStoreIsRepairedBeforeItIsOpened(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	const roadmap = "store-repair"
	defer setupTestGraphRoadmap(t, roadmap)()
	graphDir := graphDirOf(t, roadmap)

	testenv.CopyTree(t, filepath.Join("..", "..", testenv.SingleFileStoreFixture), graphDir)
	for path, mode := range map[string]os.FileMode{
		graphDir:                            0o700,
		filepath.Join(graphDir, "snapshot"): 0o750,
		filepath.Join(graphDir, "snapshot", "indexes"):       0o750,
		filepath.Join(graphDir, "snapshot", "manifest.json"): 0o644,
	} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("setting %s to %#o: %v", path, mode, err)
		}
	}

	server := startModeServer(t, roadmap)
	watch := newModeWatch(graphDir)
	// The first observation is taken before any statement: the store is
	// already being served, so a wider entry left for a checkpoint to rewrite
	// is seen here.
	watch.observe()
	stop := make(chan struct{})
	watching := watch.watchUntil(stop)

	out := graphClientJSON(t, roadmap, "MATCH (n) RETURN count(n)")
	rows, _ := out["rows"].([]any) //nolint:errcheck // a missing member fails the comparison below
	if got := fmt.Sprint(rows); got != "[[6]]" {
		t.Errorf("the converted store answers count(n) = %s, want [[6]], the six nodes the fixture holds", got)
	}

	server.stop(t)
	close(stop)
	<-watching
	watch.report(t, "while the server served and while it stopped")

	after := newModeWatch(graphDir)
	after.observe()
	after.report(t, "after the server exited")

	// The conversion's own entries are among those observed.
	for rel, kind := range map[string]string{"wal.control": "file", "wal.d": "directory"} {
		if got := after.seen[rel]; got != kind {
			t.Errorf("the conversion's %s %s was not observed (seen as %q); the criterion includes it", kind, rel, got)
		}
	}
	if !testenv.InspectWAL(t, graphDir).HasControl {
		t.Errorf("graph/ carries no write-ahead-log control file after the start; the store was not " +
			"converted, so the conversion's entries were never observed")
	}
	home := filepath.Dir(graphDir)
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("listing the roadmap home %s: %v", home, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), graphstore.BackupDirPrefix) {
			t.Errorf("the roadmap home holds %s after the start; a successful conversion removes its backup", e.Name())
		}
	}
	t.Logf("%d walks while the server ran; entries seen: %v", watch.walks.Load(), after.seen)
}
