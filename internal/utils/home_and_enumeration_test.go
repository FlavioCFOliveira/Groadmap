package utils

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// homeVariable is the environment variable os.UserHomeDir reads on this
// platform.
func homeVariable() string {
	if runtime.GOOS == "windows" {
		return "USERPROFILE"
	}
	return "HOME"
}

// TestGetDataDir_RefusesARelativeHome pins SPEC/ARCHITECTURE.md § Directory
// Structure, location rule 1: a home directory that is not an absolute path is
// refused with the published line and ErrDatabase (exit 1), before anything is
// read or created under it — so neither the data directory nor a roadmap home
// appears relative to the working directory.
func TestGetDataDir_RefusesARelativeHome(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	for _, home := range []string{"relative-home", "./payments", "../elsewhere", "home with spaces"} {
		t.Setenv(homeVariable(), home)

		_, err := GetDataDir()
		want := `database error: home directory "` + home + `" is not an absolute path; refusing to locate the data directory under it`
		if err == nil || err.Error() != want || !errors.Is(err, ErrDatabase) || !errors.Is(err, ErrHomeUnresolved) {
			t.Errorf("HOME=%q: %v, want %q", home, err, want)
		}
		if err := EnsureRoadmapDir("payments-api"); err == nil {
			t.Errorf("HOME=%q: EnsureRoadmapDir created a roadmap home", home)
		}
		if err := MigrateLegacyLayout(); err == nil || !errors.Is(err, ErrHomeUnresolved) {
			t.Errorf("HOME=%q: the layout-migration sweep ran: %v", home, err)
		}
		if _, err := ListRoadmapEntries(); err == nil {
			t.Errorf("HOME=%q: enumeration read a relative data directory", home)
		}
	}
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a refused relative home left %d entries in the working directory", len(entries))
	}
}

func TestGetDataDir_AcceptsAnAbsoluteHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeVariable(), home)
	got, err := GetDataDir()
	if err != nil || got != filepath.Join(home, DataDirName) {
		t.Errorf("GetDataDir() = %q, %v", got, err)
	}
}

// TestListRoadmapEntries_OneNamePredicate pins SPEC/ARCHITECTURE.md § Directory
// Structure, location rule 9: enumeration offers only a subdirectory whose name
// satisfies every roadmap name rule and that holds a project.db, and skips every
// other entry silently, without reading or changing it.
func TestListRoadmapEntries_OneNamePredicate(t *testing.T) {
	t.Setenv(homeVariable(), t.TempDir())
	for _, name := range []string{"payments-api", "billing_v2"} {
		if err := EnsureRoadmapDir(name); err != nil {
			t.Fatal(err)
		}
		path, _ := GetRoadmapPath(name)
		if err := os.WriteFile(path, []byte("x"), DBFilePerm); err != nil {
			t.Fatal(err)
		}
	}
	dataDir, _ := GetDataDir()
	invalid := []string{"help", "con", "LPT1", "Payments", "-leading", "has space", strings.Repeat("b", 51)}
	for _, n := range invalid {
		if IsValidRoadmapName(n) {
			t.Fatalf("%q is a valid name; the fixture is wrong", n)
		}
		if err := os.Mkdir(filepath.Join(dataDir, n), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, n, DBFileName), []byte("left alone"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A valid name without project.db, and a top-level file, are not roadmaps.
	if err := os.Mkdir(filepath.Join(dataDir, "drafts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "notes.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	entries, err := ListRoadmapEntries()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
		if e.Size != 1 || e.Path != filepath.Join(dataDir, e.Name, DBFileName) {
			t.Errorf("entry %+v carries the wrong path or size", e)
		}
	}
	if strings.Join(names, ",") != "billing_v2,payments-api" {
		t.Errorf("enumerated %v, want billing_v2 and payments-api only", names)
	}
	listed, err := ListRoadmaps()
	if err != nil || strings.Join(listed, ",") != "billing_v2,payments-api" {
		t.Errorf("ListRoadmaps() = %v, %v; it must enumerate under the same rule", listed, err)
	}
	for _, n := range invalid {
		data, err := os.ReadFile(filepath.Join(dataDir, n, DBFileName))
		if err != nil || string(data) != "left alone" {
			t.Errorf("the skipped entry %q was changed", n)
		}
	}
}

// TestGetDataDir_AnUnsetHomeIsUnresolved: a home that is not set at all is
// refused too, and marked so the startup sweep leaves the refusal to the
// command (location rule 1).
func TestGetDataDir_AnUnsetHomeIsUnresolved(t *testing.T) {
	t.Setenv(homeVariable(), "")
	if _, err := GetDataDir(); err == nil || !errors.Is(err, ErrHomeUnresolved) {
		t.Errorf("an empty home: %v, want a refusal marked ErrHomeUnresolved", err)
	}
}
