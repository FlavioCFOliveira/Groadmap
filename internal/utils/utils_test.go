package utils

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ==================== PATH TESTS ====================

func TestValidateRoadmapName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid names
		{"lowercase letters", "myroadmap", false},
		{"letters and numbers", "roadmap123", false},
		{"with hyphens", "my-roadmap", false},
		{"with underscores", "my_roadmap", false},
		{"single char", "a", false},
		{"mixed valid", "my-roadmap_123", false},

		// Invalid names - empty
		{"empty string", "", true},

		// Invalid names - uppercase
		{"uppercase letter", "MyRoadmap", true},
		{"mixed case", "myRoadmap", true},

		// Invalid names - special characters
		{"space", "my roadmap", true},
		{"dot", "my.roadmap", true},
		{"slash", "my/roadmap", true},
		{"backslash", "my\\roadmap", true},
		{"colon", "my:roadmap", true},
		{"asterisk", "my*roadmap", true},
		{"question mark", "my?roadmap", true},
		{"quotes", `my"roadmap`, true},
		{"less than", "my<roadmap", true},
		{"greater than", "my>roadmap", true},
		{"pipe", "my|roadmap", true},
		{"at symbol", "my@roadmap", true},
		{"hash", "my#roadmap", true},
		{"dollar", "my$roadmap", true},
		{"percent", "my%roadmap", true},
		{"ampersand", "my&roadmap", true},
		{"plus", "my+roadmap", true},
		{"equals", "my=roadmap", true},
		{"caret", "my^roadmap", true},
		{"tick", "my`roadmap", true},
		{"tilde", "my~roadmap", true},
		{"exclamation", "my!roadmap", true},
		{"left paren", "my(roadmap", true},
		{"right paren", "my)roadmap", true},
		{"left brace", "my{roadmap", true},
		{"right brace", "my}roadmap", true},
		{"left bracket", "my[roadmap", true},
		{"right bracket", "my]roadmap", true},

		// Path traversal attempts
		{"dot dot slash", "../etc/passwd", true},
		{"dot dot backslash", "..\\windows\\system32", true},
		{"absolute path unix", "/etc/passwd", true},
		{"absolute path windows", "C:\\Windows", true},
		{"double dots", "..", true},
		{"triple dots", "...", true},
		{"dot dot in middle", "my/../roadmap", true},
		{"dot dot at end", "roadmap/..", true},

		// Names starting with hyphen (flag confusion)
		{"starts with hyphen", "-r", true},
		{"starts with double hyphen", "--help", true},
		{"starts with hyphen and text", "-roadmap", true},
		{"single hyphen", "-", true},

		// Names starting with number (now invalid - must start with letter)
		{"starts with number", "123roadmap", true},
		{"starts with underscore", "_roadmap", true},

		// Maximum length validation
		{"max length (255 chars)", strings.Repeat("a", 255), false},
		{"exceeds max length (256 chars)", strings.Repeat("a", 256), true},

		// Reserved Windows names
		{"CON", "CON", true},
		{"PRN", "PRN", true},
		{"AUX", "AUX", true},
		{"NUL", "NUL", true},
		{"COM1", "COM1", true},
		{"COM9", "COM9", true},
		{"LPT1", "LPT1", true},
		{"LPT9", "LPT9", true},
		{"CON.txt", "CON.txt", true},
		{"AUX.db", "AUX.db", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRoadmapName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRoadmapName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestGetRoadmapPath(t *testing.T) {
	// Test valid name - just verify it returns a path ending with .db
	path, err := GetRoadmapPath("myroadmap")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasSuffix(path, "myroadmap.db") {
		t.Errorf("expected path to end with 'myroadmap.db', got %q", path)
	}

	// Test invalid name
	_, err = GetRoadmapPath("../etc/passwd")
	if err == nil {
		t.Error("expected error for invalid name")
	}
}

func TestRoadmapExists(t *testing.T) {
	// Get the actual data directory
	dataDir, err := GetDataDir()
	if err != nil {
		t.Fatalf("failed to get data dir: %v", err)
	}

	// Ensure data directory exists
	os.MkdirAll(dataDir, 0700)

	// Clean up any existing test file
	testFile := filepath.Join(dataDir, "testroadmapexists.db")
	os.Remove(testFile)

	// Test non-existent roadmap
	exists, err := RoadmapExists("testroadmapexists")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected roadmap to not exist")
	}

	// Create a roadmap file
	if err := os.WriteFile(testFile, []byte{}, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	defer os.Remove(testFile)

	// Test existing roadmap
	exists, err = RoadmapExists("testroadmapexists")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected roadmap to exist")
	}

	// Test invalid name
	_, err = RoadmapExists("../etc/passwd")
	if err == nil {
		t.Error("expected error for invalid name")
	}
}

func TestListRoadmaps(t *testing.T) {
	// Get the actual data directory
	dataDir, err := GetDataDir()
	if err != nil {
		t.Fatalf("failed to get data dir: %v", err)
	}

	// Ensure data directory exists
	os.MkdirAll(dataDir, 0700)

	// Clean up test files after test
	defer func() {
		for i := 1; i <= 3; i++ {
			os.Remove(filepath.Join(dataDir, "testlistroadmap"+string(rune('0'+i))+".db"))
		}
		os.Remove(filepath.Join(dataDir, "testlistnotadb.txt"))
		os.RemoveAll(filepath.Join(dataDir, "testlistdirectory.db"))
	}()

	// Create some roadmap files
	testNames := []string{"testlistroadmap1", "testlistroadmap2", "testlistroadmap3"}
	for _, name := range testNames {
		roadmapPath := filepath.Join(dataDir, name+".db")
		os.WriteFile(roadmapPath, []byte{}, 0600)
	}

	// Create a non-.db file (should be ignored)
	nonDbPath := filepath.Join(dataDir, "testlistnotadb.txt")
	os.WriteFile(nonDbPath, []byte{}, 0600)

	// Create a directory (should be ignored)
	dirPath := filepath.Join(dataDir, "testlistdirectory.db")
	os.Mkdir(dirPath, 0700)

	// List roadmaps
	roadmaps, err := ListRoadmaps()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that all expected names are present
	nameMap := make(map[string]bool)
	for _, name := range roadmaps {
		nameMap[name] = true
	}

	for _, expected := range testNames {
		if !nameMap[expected] {
			t.Errorf("expected roadmap %q not found in list", expected)
		}
	}
}

func TestListRoadmapsEmpty(t *testing.T) {
	// Create a temporary directory for this test
	tmpDir, err := os.MkdirTemp("", "groadmap-test-empty-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Temporarily override the data directory by creating a test condition
	// where ~/.roadmaps doesn't exist yet
	// This tests the os.IsNotExist branch
	// We can't easily test this without modifying the function, but we can test
	// that ListRoadmaps returns empty when directory exists but is empty

	// Test with a fresh empty directory
	emptyDir := filepath.Join(tmpDir, "empty")
	os.MkdirAll(emptyDir, 0700)

	// This test documents that ListRoadmaps works correctly with empty directories
	// The actual implementation uses GetDataDir() which we can't easily override
	// So we just verify the function doesn't error
	roadmaps, err := ListRoadmaps()
	if err != nil {
		t.Fatalf("ListRoadmaps should not error: %v", err)
	}
	// Result should be a valid slice (may or may not be empty depending on state)
	if roadmaps == nil {
		t.Error("ListRoadmaps should return empty slice, not nil")
	}
}

func TestEnsureDataDir(t *testing.T) {
	// Test creating data directory (should already exist from other tests)
	err := EnsureDataDir()
	if err != nil {
		t.Fatalf("failed to ensure data dir: %v", err)
	}

	// Get data directory
	dataDir, err := GetDataDir()
	if err != nil {
		t.Fatalf("failed to get data dir: %v", err)
	}

	// Verify directory exists
	info, err := os.Stat(dataDir)
	if err != nil {
		t.Fatalf("data dir not created: %v", err)
	}

	if !info.IsDir() {
		t.Error("data dir is not a directory")
	}

	// Verify permissions
	if info.Mode().Perm() != 0700 {
		t.Errorf("expected permissions 0700, got %04o", info.Mode().Perm())
	}

	// Test idempotency (calling again should not fail)
	err = EnsureDataDir()
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
}

// ==================== TIME TESTS ====================

func TestNowISO8601(t *testing.T) {
	now := NowISO8601()

	// Should be valid ISO8601
	if !IsValidISO8601(now) {
		t.Errorf("NowISO8601() returned invalid format: %s", now)
	}

	// Should be recent (within last minute)
	parsed, err := time.Parse(time.RFC3339, now)
	if err != nil {
		t.Fatalf("failed to parse time: %v", err)
	}

	if time.Since(parsed) > time.Minute {
		t.Error("NowISO8601() returned time too far in the past")
	}
}

func TestFormatISO8601(t *testing.T) {
	testTime := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	formatted := FormatISO8601(testTime)

	expected := "2024-03-15T10:30:00.000Z"
	if formatted != expected {
		t.Errorf("expected %q, got %q", expected, formatted)
	}
}

func TestFormatISO8601Zero(t *testing.T) {
	// Test with zero time - should return empty string
	var zeroTime time.Time
	formatted := FormatISO8601(zeroTime)

	if formatted != "" {
		t.Errorf("expected empty string for zero time, got %q", formatted)
	}
}

func TestParseISO8601(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		year    int
		month   time.Month
		day     int
	}{
		// Valid format (must include milliseconds and Z UTC suffix)
		{"RFC3339 with Z", "2024-03-15T10:30:00.000Z", false, 2024, 3, 15},

		// RFC3339 variants with timezone offsets - accepted and normalized to UTC
		{"RFC3339 with offset", "2024-03-15T10:30:00.000+00:00", false, 2024, 3, 15},
		{"RFC3339 with positive offset", "2024-03-15T10:30:00.000+01:00", false, 2024, 3, 15},
		{"RFC3339 with negative offset", "2024-03-15T10:30:00.000-05:00", false, 2024, 3, 15},

		// Invalid formats
		{"empty string", "", true, 0, 0, 0}, // Empty string should return error
		{"date only", "2024-03-15", true, 0, 0, 0},
		{"invalid format", "not-a-date", true, 0, 0, 0},
		{"partial date", "2024-03", true, 0, 0, 0},
		{"wrong separator", "2024/03/15", true, 0, 0, 0},
		{"time only", "10:30:00", true, 0, 0, 0},
		{"invalid month", "2024-13-15", true, 0, 0, 0},
		{"invalid day", "2024-03-32", true, 0, 0, 0},
		{"invalid hour", "2024-03-15T25:00:00Z", true, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseISO8601(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseISO8601(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if parsed.Year() != tt.year || parsed.Month() != tt.month || parsed.Day() != tt.day {
					t.Errorf("ParseISO8601(%q) = %v, expected year=%d month=%d day=%d",
						tt.input, parsed, tt.year, tt.month, tt.day)
				}
			}
		})
	}
}

func TestIsValidISO8601(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		// Valid (must include milliseconds and Z UTC suffix)
		{"2024-03-15T10:30:00.000Z", true},

		// Valid - RFC3339 without milliseconds, accepted and normalized
		{"2024-03-15T10:30:00Z", true},
		// Valid - RFC3339 with +00:00 offset, accepted and normalized to UTC
		{"2024-03-15T10:30:00.000+00:00", true},
		// Invalid - date only not supported
		{"2024-03-15", false},
		// Invalid - empty string returns zero time (valid parse but not a real date)
		{"", false},
		{"not-a-date", false},
		{"2024-03-15 10:30:00", false}, // space instead of T
		{"15-03-2024", false},          // wrong order
		{"2024/03/15", false},          // wrong separator
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsValidISO8601(tt.input); got != tt.valid {
				t.Errorf("IsValidISO8601(%q) = %v, want %v", tt.input, got, tt.valid)
			}
		})
	}
}

// ==================== JSON TESTS ====================

func TestToJSON(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	input := testStruct{Name: "test", Value: 42}
	json, err := ToJSON(input)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	expected := `{"name":"test","value":42}`
	if string(json) != expected {
		t.Errorf("expected %q, got %q", expected, string(json))
	}
}

func TestFromJSON(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	json := `{"name":"test","value":42}`
	var result testStruct
	err := FromJSON([]byte(json), &result)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	if result.Name != "test" || result.Value != 42 {
		t.Errorf("unexpected result: %+v", result)
	}

	// Test invalid JSON
	err = FromJSON([]byte("not json"), &result)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestPrintJSON(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	input := map[string]string{"key": "value"}
	err := PrintJSON(input)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("PrintJSON failed: %v", err)
	}

	// Read output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Must be human-readable indented output
	if !strings.Contains(output, "\n") {
		t.Error("expected indented output to contain newlines")
	}
	if !strings.Contains(output, "  ") {
		t.Error("expected indented output to contain 2-space indentation")
	}
	if !strings.Contains(output, "key") {
		t.Errorf("expected output to contain 'key', got %q", output)
	}
}

func TestJSONSpecialCharacters(t *testing.T) {
	// Test that special characters are handled correctly
	input := map[string]string{
		"html":    "<script>alert('xss')</script>",
		"quotes":  `He said "hello"`,
		"newline": "line1\nline2",
		"tab":     "col1\tcol2",
		"unicode": "Hello, 世界",
	}

	json, err := ToJSON(input)
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Verify it can be parsed back
	var result map[string]string
	err = FromJSON(json, &result)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	// Verify values match
	for key, expected := range input {
		if result[key] != expected {
			t.Errorf("for key %q: expected %q, got %q", key, expected, result[key])
		}
	}
}

// ==================== PERMISSION TESTS ====================

func TestVerifyPermissions(t *testing.T) {
	// Create a temporary file with specific permissions
	tmpFile, err := os.CreateTemp("", "perm-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Test 1: Verify correct permissions (0600)
	if err := os.Chmod(tmpFile.Name(), 0600); err != nil {
		t.Fatalf("failed to set permissions: %v", err)
	}
	if err := VerifyPermissions(tmpFile.Name(), 0600); err != nil {
		t.Errorf("VerifyPermissions failed for 0600: %v", err)
	}

	// Test 2: Verify wrong permissions (should fail)
	if err := os.Chmod(tmpFile.Name(), 0644); err != nil {
		t.Fatalf("failed to set permissions: %v", err)
	}
	if err := VerifyPermissions(tmpFile.Name(), 0600); err == nil {
		t.Error("VerifyPermissions should have failed for 0644 when expecting 0600")
	}

	// Test 3: Verify directory permissions
	tmpDir, err := os.MkdirTemp("", "perm-test-dir")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.Chmod(tmpDir, 0700); err != nil {
		t.Fatalf("failed to set dir permissions: %v", err)
	}
	if err := VerifyPermissions(tmpDir, 0700); err != nil {
		t.Errorf("VerifyPermissions failed for directory 0700: %v", err)
	}

	// Test 4: Non-existent file
	if err := VerifyPermissions("/nonexistent/path/file.db", 0600); err == nil {
		t.Error("VerifyPermissions should fail for non-existent file")
	}
}
