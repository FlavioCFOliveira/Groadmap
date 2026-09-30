package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The gates of SPEC/WEB.md Acceptance Criterion 264, path-parameter rule 2 and
// § What Is Not Logged, item 1: the web decides whether a roadmap exists exactly
// as the CLI does. A regular file at ~/.roadmaps/<name> is a roadmap that does
// not exist — 404 with the ordinary body, no log record, not listed — and only a
// failure of the existence check itself is the logged 500.

// criterion264Routes are the three requests the criterion names.
var criterion264Routes = []string{"", "/tasks", "/graph/data"}

// plantUnsearchableRoadmapHome makes the existence check of roadmap name fail
// with an I/O error, which is the one case the logging table answers with 500
// and an ERROR record. It returns which trigger it used.
//
// The trigger the criterion names is a roadmap home at mode 0000 holding
// project.db, so the check cannot learn whether project.db is there. The
// superuser is not denied by a mode, so under euid 0 the trigger is instead a
// data directory that is itself a regular file: the lstat of
// ~/.roadmaps/<name> then fails with ENOTDIR, a real failure of the check and
// not a roadmap that does not exist. Neither branch skips the test.
func plantUnsearchableRoadmapHome(t *testing.T, home, name string) string {
	t.Helper()
	dataDir := filepath.Join(home, utils.DataDirName)
	if os.Geteuid() == 0 {
		if err := os.WriteFile(dataDir, []byte("a data directory that is a file"), 0o600); err != nil {
			t.Fatalf("planting the regular-file data directory: %v", err)
		}
		return "data directory is a regular file (ENOTDIR)"
	}
	roadmapHome := filepath.Join(dataDir, name)
	if err := os.MkdirAll(roadmapHome, 0o700); err != nil {
		t.Fatalf("creating the roadmap home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(roadmapHome, utils.DBFileName), nil, 0o600); err != nil {
		t.Fatalf("creating project.db: %v", err)
	}
	if err := os.Chmod(roadmapHome, 0o000); err != nil {
		t.Fatalf("taking the search permission away: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roadmapHome, 0o700) }) // #nosec G302 -- restores a test directory so TempDir can remove it
	if _, err := utils.RoadmapExists(name); err == nil {
		t.Fatal("the existence check of a mode-0000 roadmap home reported no error; the 500 path would not be reached")
	}
	return "roadmap home at mode 0000 holding project.db (EACCES)"
}

// TestCriterion264_ARegularFileHomeIsARoadmapThatDoesNotExist drives the three
// named routes against a regular file at the roadmap home and against a name
// under which nothing exists, and requires the same 404 and body, no log
// record, no listing, and the file left as it was found.
func TestCriterion264_ARegularFileHomeIsARoadmapThatDoesNotExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const name = "settlement-exports"
	seedRoadmap(t, "ledger-core") // a real roadmap, so the index has something to list
	occupant := filepath.Join(home, utils.DataDirName, name)
	content := []byte("A settlement export saved where a roadmap home belongs.\n")
	if err := os.WriteFile(occupant, content, 0o600); err != nil {
		t.Fatalf("planting the regular file: %v", err)
	}

	h := handler()
	for _, suffix := range criterion264Routes {
		absent := httptest.NewRecorder()
		h.ServeHTTP(absent, httptest.NewRequest(http.MethodGet, "/roadmaps/no-such-roadmap"+suffix, nil))

		buf := captureLog(t)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+suffix, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET /roadmaps/%s%s = %d, want 404", name, suffix, rec.Code)
		}
		if rec.Body.String() != absent.Body.String() || absent.Code != http.StatusNotFound {
			t.Errorf("GET /roadmaps/%s%s answered %q, want the body a name under which nothing exists gets (%d %q)",
				name, suffix, rec.Body.String(), absent.Code, absent.Body.String())
		}
		if lines := logLines(buf); len(lines) != 0 {
			t.Errorf("GET /roadmaps/%s%s wrote %d log record(s), want none:\n%s", name, suffix, len(lines), buf.String())
		}
	}

	index := httptest.NewRecorder()
	h.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "/roadmaps/ledger-core") {
		t.Fatalf("the roadmap index = %d and does not list the real roadmap; the absence below would prove nothing", index.Code)
	}
	if strings.Contains(index.Body.String(), "/roadmaps/"+name) {
		t.Errorf("the roadmap index lists %q, which is a regular file and not a roadmap", name)
	}

	got, err := os.ReadFile(occupant) // #nosec G304 -- this test's own file under its own HOME
	if err != nil || !bytes.Equal(got, content) {
		t.Errorf("the regular file was changed: %q (%v)", got, err)
	}
	if info, err := os.Lstat(occupant); err != nil || !info.Mode().IsRegular() {
		t.Errorf("the regular file is no longer a regular file: %v (%v)", info, err)
	}
}

// TestCriterion264_AFailedExistenceCheckIsA500WithOneErrorRecord is the other
// half of the criterion: when the check itself fails, each of the three routes
// answers 500 and writes exactly one ERROR record for the failed check.
func TestCriterion264_AFailedExistenceCheckIsA500WithOneErrorRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const name = "treasury-ops"
	trigger := plantUnsearchableRoadmapHome(t, home, name)

	h := handler()
	for _, suffix := range criterion264Routes {
		buf := captureLog(t)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+suffix, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("[%s] GET /roadmaps/%s%s = %d, want 500", trigger, name, suffix, rec.Code)
		}
		record := oneRecord(t, buf)
		mustContainAll(t, record, "level=ERROR", `msg="roadmap existence check failed"`, "roadmap="+name, "status=500")
	}
}
