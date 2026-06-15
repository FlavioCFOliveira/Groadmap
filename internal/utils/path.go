package utils

import (
	"errors"
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
	// DBFileName is the fixed filename of the SQLite database inside each
	// roadmap home directory (~/.roadmaps/<name>/project.db). The roadmap
	// name is encoded in the directory, not in this basename.
	DBFileName = "project.db"
)

// ValidRoadmapNameRegex validates roadmap names: lowercase letters, numbers, underscores, hyphens.
var ValidRoadmapNameRegex = regexp.MustCompile(`^[a-z0-9_-]+$`)

// Sentinel errors for path and name validation.
var (
	ErrPermissionsMismatch         = errors.New("permissions mismatch (umask may have interfered)")
	ErrRoadmapNameEmpty            = errors.New("roadmap name cannot be empty")
	ErrRoadmapNameTooLong          = errors.New("roadmap name too long")
	ErrRoadmapNameStartsWithHyphen = errors.New("roadmap name cannot start with '-'")
	ErrRoadmapNameReserved         = errors.New("roadmap name is a reserved system name")
	ErrInvalidRoadmapName          = errors.New("invalid roadmap name")
)

// MaxRoadmapNameLength is the maximum allowed length for roadmap names.
const MaxRoadmapNameLength = 50

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
		return fmt.Errorf("expected %04o, got %04o: %w", expectedPerm, actualPerm, ErrPermissionsMismatch)
	}

	return nil
}

// ValidateRoadmapName checks if a roadmap name is valid.
// Names must:
//   - Not be empty
//   - Not exceed 50 characters
//   - Not start with '-' (to prevent flag confusion)
//   - Contain only lowercase letters, numbers, underscores, and hyphens
//   - Not be a Windows reserved name (CON, PRN, AUX, NUL, COM1-9, LPT1-9)
func ValidateRoadmapName(name string) error {
	if name == "" {
		// SPEC/COMMANDS.md mandates this verbatim message (finding #60).
		return ValidationMessage("Roadmap name is required", ErrRoadmapNameEmpty)
	}

	// Check maximum length
	if len(name) > MaxRoadmapNameLength {
		// SPEC/COMMANDS.md + SPEC/ARCHITECTURE.md mandate this verbatim message.
		return &MessageError{
			Msg:       fmt.Sprintf("Roadmap name must not exceed %d characters (got %d)", MaxRoadmapNameLength, len(name)),
			Sentinels: []error{ErrValidation, ErrRoadmapNameTooLong},
		}
	}

	// Check for flag confusion (names starting with '-')
	if name[0] == '-' {
		return fmt.Errorf("%w: %w", ErrValidation, ErrRoadmapNameStartsWithHyphen)
	}

	// Check against Windows reserved names (case-insensitive)
	upperName := strings.ToUpper(name)
	if WindowsReservedNames[upperName] {
		return fmt.Errorf("%w: %q: %w", ErrValidation, name, ErrRoadmapNameReserved)
	}

	// Check for extension variants of reserved names (e.g., CON.txt)
	baseName := strings.SplitN(upperName, ".", 2)[0]
	if WindowsReservedNames[baseName] {
		return fmt.Errorf("%w: %q: %w", ErrValidation, name, ErrRoadmapNameReserved)
	}

	// Validate against regex
	if !ValidRoadmapNameRegex.MatchString(name) {
		// SPEC/COMMANDS.md mandates this verbatim message (finding #60).
		return &MessageError{
			Msg:       "Roadmap name must only contain lowercase letters, numbers, underscores, and hyphens",
			Sentinels: []error{ErrValidation, ErrInvalidRoadmapName},
		}
	}

	return nil
}

// GetRoadmapDir returns the absolute path to a roadmap's home directory
// (~/.roadmaps/<name>/). This directory is the container for every file the
// application stores for that roadmap (today the SQLite database and its
// sidecars; designed to hold further per-roadmap artefacts later).
// The name is validated to prevent path traversal attacks.
func GetRoadmapDir(name string) (string, error) {
	if err := ValidateRoadmapName(name); err != nil {
		return "", err
	}

	dataDir, err := GetDataDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dataDir, name), nil
}

// GetRoadmapPath returns the full path to a roadmap database file
// (~/.roadmaps/<name>/project.db).
// Validates the name to prevent path traversal attacks.
func GetRoadmapPath(name string) (string, error) {
	dir, err := GetRoadmapDir(name)
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, DBFileName), nil
}

// EnsureRoadmapDir creates a roadmap's home directory if it does not exist.
// Sets permissions to 0700 (owner only) for security and verifies that the
// permissions were applied correctly after creation (umask may interfere).
// It mirrors EnsureDataDir but targets ~/.roadmaps/<name>/.
func EnsureRoadmapDir(name string) error {
	// The data directory must exist (and be private) before any roadmap
	// home directory can be created under it.
	if err := EnsureDataDir(); err != nil {
		return err
	}

	dir, err := GetRoadmapDir(name)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, DataDirPerm); err != nil {
		return fmt.Errorf("creating roadmap directory %s: %w", dir, err)
	}

	// Ensure permissions are set correctly (umask may have affected creation).
	if err := os.Chmod(dir, DataDirPerm); err != nil {
		return fmt.Errorf("setting permissions on roadmap directory: %w", err)
	}

	// Verify permissions were set correctly.
	if err := VerifyPermissions(dir, DataDirPerm); err != nil {
		return fmt.Errorf("verifying roadmap directory permissions: %w", err)
	}

	return nil
}

// RoadmapExists checks whether a roadmap exists under the current layout,
// i.e. whether ~/.roadmaps/<name>/project.db is present as a regular file.
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
		return false, fmt.Errorf("checking roadmap database: %w", err)
	}

	return !info.IsDir(), nil
}

// ListRoadmaps returns the names of all roadmaps in the data directory.
// Under the current layout each roadmap is an immediate subdirectory of
// ~/.roadmaps/ that contains a project.db database; top-level files are not
// considered roadmaps.
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

	// Initialise non-nil so the empty case returns [] rather than null,
	// matching the os.IsNotExist branch and the JSON contract expected by
	// callers.
	roadmaps := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dbPath := filepath.Join(dataDir, entry.Name(), DBFileName)
		info, statErr := os.Stat(dbPath)
		if statErr != nil || info.IsDir() {
			continue // not a roadmap home directory
		}
		roadmaps = append(roadmaps, entry.Name())
	}

	return roadmaps, nil
}
