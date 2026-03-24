package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// setupTestRoadmap ensures the data directory exists and removes any pre-existing test roadmap.
func setupTestRoadmap(t *testing.T, name string) {
	t.Helper()

	if err := utils.EnsureDataDir(); err != nil {
		t.Fatalf("failed to ensure data dir: %v", err)
	}

	cleanupTestRoadmap(t, name)
}

// cleanupTestRoadmap removes test roadmap database files.
func cleanupTestRoadmap(t *testing.T, name string) {
	t.Helper()

	dataDir, err := utils.GetDataDir()
	if err != nil {
		return
	}

	testPath := filepath.Join(dataDir, name+".db")
	os.Remove(testPath)
	os.Remove(testPath + "-shm")
	os.Remove(testPath + "-wal")
}

// ==================== HandleRoadmap Tests ====================

func TestHandleRoadmap_NoArgs(t *testing.T) {
	// Should print help and return nil
	err := HandleRoadmap([]string{})
	if err != nil {
		t.Errorf("HandleRoadmap([]) error = %v, want nil", err)
	}
}

func TestHandleRoadmap_Help(t *testing.T) {
	// Test various help flags
	helpFlags := []string{"-h", "--help", "help"}

	for _, flag := range helpFlags {
		t.Run("flag_"+flag, func(t *testing.T) {
			err := HandleRoadmap([]string{flag})
			if err != nil {
				t.Errorf("HandleRoadmap([%s]) error = %v, want nil", flag, err)
			}
		})
	}
}

func TestHandleRoadmap_UnknownSubcommand(t *testing.T) {
	err := HandleRoadmap([]string{"unknown"})
	if err == nil {
		t.Error("HandleRoadmap([unknown]) expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown roadmap subcommand") {
		t.Errorf("expected 'unknown roadmap subcommand' error, got: %v", err)
	}
}

func TestHandleRoadmap_List(t *testing.T) {
	// Should not error (may return empty list)
	err := HandleRoadmap([]string{"list"})
	if err != nil {
		t.Errorf("HandleRoadmap([list]) error = %v, want nil", err)
	}
}

func TestHandleRoadmap_ListAlias(t *testing.T) {
	// Test "ls" alias
	err := HandleRoadmap([]string{"ls"})
	if err != nil {
		t.Errorf("HandleRoadmap([ls]) error = %v, want nil", err)
	}
}

// ==================== roadmapCreate Tests ====================

func TestRoadmapCreate_NoName(t *testing.T) {
	err := HandleRoadmap([]string{"create"})
	if err == nil {
		t.Error("roadmapCreate with no name expected error, got nil")
	}
	if !strings.Contains(err.Error(), "roadmap name required") {
		t.Errorf("expected 'roadmap name required' error, got: %v", err)
	}
}

func TestRoadmapCreate_InvalidName(t *testing.T) {
	invalidNames := []string{"-r", "--help", "123abc", "MYROADMAP", "my roadmap", "../etc"}

	for _, name := range invalidNames {
		t.Run("invalid_"+name, func(t *testing.T) {
			err := HandleRoadmap([]string{"create", name})
			if err == nil {
				t.Errorf("roadmapCreate(%q) expected error, got nil", name)
			}
		})
	}
}

func TestRoadmapCreate_Success(t *testing.T) {
	testName := "testroadmapcreate"
	setupTestRoadmap(t, testName)
	defer cleanupTestRoadmap(t, testName)

	// Create the roadmap
	err := HandleRoadmap([]string{"create", testName})
	if err != nil {
		t.Fatalf("roadmapCreate(%q) error = %v", testName, err)
	}

	// Verify it exists
	exists, err := utils.RoadmapExists(testName)
	if err != nil {
		t.Fatalf("RoadmapExists error = %v", err)
	}
	if !exists {
		t.Error("roadmap was not created")
	}
}

func TestRoadmapCreate_AlreadyExists(t *testing.T) {
	testName := "testroadmapexists"
	setupTestRoadmap(t, testName)
	defer cleanupTestRoadmap(t, testName)

	// Create first time
	err := HandleRoadmap([]string{"create", testName})
	if err != nil {
		t.Fatalf("first create error = %v", err)
	}

	// Try to create again
	err = HandleRoadmap([]string{"create", testName})
	if err == nil {
		t.Error("creating duplicate roadmap expected error, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

// ==================== roadmapRemove Tests ====================

func TestRoadmapRemove_NoName(t *testing.T) {
	err := HandleRoadmap([]string{"remove"})
	if err == nil {
		t.Error("roadmapRemove with no name expected error, got nil")
	}
	if !strings.Contains(err.Error(), "roadmap name required") {
		t.Errorf("expected 'roadmap name required' error, got: %v", err)
	}
}

func TestRoadmapRemove_NotFound(t *testing.T) {
	err := HandleRoadmap([]string{"remove", "nonexistentroadmap12345"})
	if err == nil {
		t.Error("removing non-existent roadmap expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestRoadmapRemove_InvalidName(t *testing.T) {
	err := HandleRoadmap([]string{"remove", "-r"})
	if err == nil {
		t.Error("removing roadmap with invalid name expected error, got nil")
	}
}

func TestRoadmapRemove_Success(t *testing.T) {
	testName := "testroadmapremove"
	setupTestRoadmap(t, testName)
	defer cleanupTestRoadmap(t, testName)

	// Create first
	err := HandleRoadmap([]string{"create", testName})
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	// Then remove
	err = HandleRoadmap([]string{"remove", testName})
	if err != nil {
		t.Errorf("roadmapRemove error = %v", err)
	}

	// Verify it no longer exists
	exists, _ := utils.RoadmapExists(testName)
	if exists {
		t.Error("roadmap still exists after removal")
	}
}

// ==================== requireRoadmap Tests ====================

func TestRequireRoadmap_FromFlag(t *testing.T) {
	testName := "testrequireflag"
	setupTestRoadmap(t, testName)
	defer cleanupTestRoadmap(t, testName)

	// Create the roadmap
	err := HandleRoadmap([]string{"create", testName})
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	// Test with -r flag
	name, remaining, err := requireRoadmap([]string{"-r", testName, "extra", "args"})
	if err != nil {
		t.Errorf("requireRoadmap error = %v", err)
	}
	if name != testName {
		t.Errorf("name = %q, want %q", name, testName)
	}
	if len(remaining) != 2 || remaining[0] != "extra" || remaining[1] != "args" {
		t.Errorf("remaining = %v, want [extra args]", remaining)
	}
}

func TestRequireRoadmap_FromLongFlag(t *testing.T) {
	testName := "testrequirelongflag"
	setupTestRoadmap(t, testName)
	defer cleanupTestRoadmap(t, testName)

	// Create the roadmap
	err := HandleRoadmap([]string{"create", testName})
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	// Test with --roadmap flag
	name, _, err := requireRoadmap([]string{"--roadmap", testName})
	if err != nil {
		t.Errorf("requireRoadmap error = %v", err)
	}
	if name != testName {
		t.Errorf("name = %q, want %q", name, testName)
	}
}

func TestRequireRoadmap_NoRoadmap(t *testing.T) {
	_, _, err := requireRoadmap([]string{})
	if err == nil {
		t.Error("requireRoadmap with no -r flag expected error, got nil")
	}
	if !utils.IsNoRoadmap(err) {
		t.Errorf("expected ErrNoRoadmap, got: %v", err)
	}
}
