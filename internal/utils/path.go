package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// DataDirName is the name of the data directory in user's home.
	DataDirName = ".roadmaps"
	// DataDirPerm is the permission for the data directory (0700 - owner only).
	DataDirPerm = 0700
	// DBFilePerm is the permission for database files (0600 - owner only).
	DBFilePerm = 0600
)

// ValidRoadmapNameRegex validates roadmap names: must start with letter, then lowercase letters, numbers, underscores, hyphens.
var ValidRoadmapNameRegex = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// MaxRoadmapNameLength is the maximum allowed length for roadmap names.
const MaxRoadmapNameLength = 255

// WindowsReservedNames contains reserved names that cannot be used on Windows systems.
var WindowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// GetDataDir returns the absolute path to the ~/.roadmaps/ directory.
// Creates the directory if it doesn't exist with 0700 permissions.
func GetDataDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting user home directory: %w", err)
	}

	dataDir := filepath.Join(homeDir, DataDirName)
	return dataDir, nil
}

// EnsureDataDir creates the data directory if it doesn't exist.
// Sets permissions to 0700 (owner only) for security.
// Verifies that permissions were set correctly after creation.
func EnsureDataDir() error {
	dataDir, err := GetDataDir()
	if err != nil {
		return err
	}

	// Create directory with restricted permissions
	if err := os.MkdirAll(dataDir, DataDirPerm); err != nil {
		return fmt.Errorf("creating data directory %s: %w", dataDir, err)
	}

	// Ensure permissions are set correctly (umask may have affected creation)
	if err := os.Chmod(dataDir, DataDirPerm); err != nil {
		return fmt.Errorf("setting permissions on data directory: %w", err)
	}

	// Verify permissions were set correctly
	if err := VerifyPermissions(dataDir, DataDirPerm); err != nil {
		return fmt.Errorf("verifying data directory permissions: %w", err)
	}

	return nil
}

// VerifyPermissions checks if a file or directory has the expected permissions.
// Returns an error if the actual permissions don't match the expected ones.
func VerifyPermissions(path string, expectedPerm os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking permissions: %w", err)
	}

	actualPerm := info.Mode().Perm()
	if actualPerm != expectedPerm {
		return fmt.Errorf("permissions mismatch: expected %04o, got %04o (umask may have interfered)",
			expectedPerm, actualPerm)
	}

	return nil
}

// ValidateRoadmapName checks if a roadmap name is valid.
// Names must:
//   - Not be empty
//   - Not exceed 255 characters
//   - Not start with '-' (to prevent flag confusion)
//   - Start with a letter and contain only lowercase letters, numbers, underscores, and hyphens
//   - Not be a Windows reserved name (CON, PRN, AUX, NUL, COM1-9, LPT1-9)
func ValidateRoadmapName(name string) error {
	if name == "" {
		return fmt.Errorf("roadmap name cannot be empty")
	}

	// Check maximum length
	if len(name) > MaxRoadmapNameLength {
		return fmt.Errorf("roadmap name too long: %d characters (maximum %d)", len(name), MaxRoadmapNameLength)
	}

	// Check for flag confusion (names starting with '-')
	if name[0] == '-' {
		return fmt.Errorf("roadmap name cannot start with '-'")
	}

	// Check against Windows reserved names (case-insensitive)
	upperName := strings.ToUpper(name)
	if WindowsReservedNames[upperName] {
		return fmt.Errorf("roadmap name %q is a reserved system name", name)
	}

	// Check for extension variants of reserved names (e.g., CON.txt)
	baseName := strings.SplitN(upperName, ".", 2)[0]
	if WindowsReservedNames[baseName] {
		return fmt.Errorf("roadmap name %q is a reserved system name", name)
	}

	// Validate against regex (must start with letter)
	if !ValidRoadmapNameRegex.MatchString(name) {
		return fmt.Errorf("invalid roadmap name %q: must start with a letter and contain only lowercase letters, numbers, underscores, and hyphens", name)
	}

	return nil
}

// GetRoadmapPath returns the full path to a roadmap database file.
// Validates the name to prevent path traversal attacks.
func GetRoadmapPath(name string) (string, error) {
	if err := ValidateRoadmapName(name); err != nil {
		return "", err
	}

	dataDir, err := GetDataDir()
	if err != nil {
		return "", err
	}

	// Use .db extension
	dbPath := filepath.Join(dataDir, name+".db")
	return dbPath, nil
}

// RoadmapExists checks if a roadmap database file exists.
func RoadmapExists(name string) (bool, error) {
	path, err := GetRoadmapPath(name)
	if err != nil {
		return false, err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking roadmap file: %w", err)
	}

	return !info.IsDir(), nil
}

// ListRoadmaps returns a list of all roadmap names in the data directory.
func ListRoadmaps() ([]string, error) {
	dataDir, err := GetDataDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("reading data directory: %w", err)
	}

	var roadmaps []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".db" {
			name := entry.Name()[:len(entry.Name())-len(".db")]
			roadmaps = append(roadmaps, name)
		}
	}

	return roadmaps, nil
}
