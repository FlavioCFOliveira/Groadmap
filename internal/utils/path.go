package utils

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
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

// assertNotSymlink refuses to operate on a path that already exists as a
// symbolic link. SPEC/ARCHITECTURE.md (Security Guarantees / Directory
// Structure) mandates that ~/.roadmaps and each roadmap home directory MUST NOT
// be symbolic links: rmp must refuse rather than follow a symlink, because the
// permission-hardening (os.Chmod) and the database write would otherwise be
// redirected through the link to a target outside ~/.roadmaps.
//
// os.Lstat does NOT follow symlinks, so it observes the link itself rather than
// its target. A non-existent path is fine (it will be created as a real
// directory); only an existing symlink is refused. Any other stat error is
// surfaced unchanged so genuine I/O failures are not masked.
//
// The refusal carries ErrDatabase, which handleError maps to exit code 1. This
// is the classification SPEC/ARCHITECTURE.md mandates in both § Directory
// Structure (location rule 10) and § Security Guarantees. A symbolic link at
// one of these paths is a condition of the filesystem's state, not a syntax or
// flag error, so exit 2 (documented as MISUSE) would misreport it to the AI
// agents that read the exit code as the contract. Exit 1 also groups this
// refusal with the other file-level refusals it belongs with, including the
// 0600 refusal in db.secureDBFile.
func assertNotSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symbolic link; refusing to use it as a roadmap directory", ErrDatabase, path)
	}
	if err != nil && !os.IsNotExist(err) {
		return dirIOError(err)
	}
	return nil
}

// dirIOError classifies a failure to create, bring to 0700, or verify the data
// directory or a roadmap home directory as utils.ErrIO, which is the class
// SPEC/ARCHITECTURE.md § Error Reuse Policy (Mandatory) assigns to a directory the
// CLI writes that is not a roadmap's database.
//
// The classification is added WITHOUT changing the text: Error returns err's own
// message, exactly as before, and err stays in the chain beside the sentinel, so
// every caller that already matched a sentinel inside it — ErrPermissionsMismatch
// among them — still does. That is what lets the classification live here, with
// the owner of the failure, rather than be restated by each caller: every
// command that opens a roadmap, `rmp web` and `rmp graph serve` all reach these
// two functions (SPEC/GRAPH.md § Server Startup, step 1), and the first two print
// the line and return the exit code they did before the classification existed.
func dirIOError(err error) error {
	return &MessageError{Msg: err.Error(), Sentinels: []error{ErrIO, err}}
}

// EnsureDataDir creates the data directory if it doesn't exist.
// Sets permissions to 0700 (owner only) for security.
// Verifies that permissions were set correctly after creation.
func EnsureDataDir() error {
	dataDir, err := GetDataDir()
	if err != nil {
		return err
	}

	// Refuse to follow a symlink planted at ~/.roadmaps: the os.Chmod below
	// would otherwise harden permissions on the link's external target
	// (finding #75).
	if err := assertNotSymlink(dataDir); err != nil {
		return err
	}

	// Create directory with restricted permissions
	if err := os.MkdirAll(dataDir, DataDirPerm); err != nil {
		return dirIOError(fmt.Errorf("creating data directory %s: %w", dataDir, err))
	}

	// Ensure permissions are set correctly (umask may have affected creation)
	if err := os.Chmod(dataDir, DataDirPerm); err != nil {
		return dirIOError(fmt.Errorf("setting permissions on data directory: %w", err))
	}

	// Verify permissions were set correctly
	if err := VerifyPermissions(dataDir, DataDirPerm); err != nil {
		return dirIOError(fmt.Errorf("verifying data directory permissions: %w", err))
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

// HelpRoadmapName is the roadmap name the CLI itself reserves. No command can
// create or remove a roadmap of this name: the name is a positional argument
// there, and in that position the word is a help token that writes the
// subcommand's help (SPEC/COMMANDS.md § Roadmap Name Validation,
// SPEC/HELP.md § Help tokens). It is therefore refused wherever a roadmap name
// is validated, the -r/--roadmap selector included.
const HelpRoadmapName = "help"

// ValidateRoadmapName checks if a roadmap name is valid.
// Names must:
//   - Not be empty
//   - Not exceed 50 characters, counted as code points
//   - Not start with '-' (to prevent flag confusion)
//   - Not be a Windows reserved name (CON, PRN, AUX, NUL, COM1-9, LPT1-9)
//   - Not be HelpRoadmapName, in that exact spelling
//   - Contain only lowercase letters, numbers, underscores, and hyphens
//
// The rules are applied in that order, and the first one the name breaks
// decides the refusal.
func ValidateRoadmapName(name string) error {
	if name == "" {
		// SPEC/COMMANDS.md mandates this verbatim message (finding #60).
		return ValidationMessage("Roadmap name is required", ErrRoadmapNameEmpty)
	}

	// Check maximum length. The rule counts characters (code points), never
	// bytes, and a byte that is not part of a valid UTF-8 sequence counts as one
	// character, which is exactly what utf8.RuneCountInString counts
	// (SPEC/COMMANDS.md § Roadmap Name Validation, "The length rule counts
	// characters"). The rules run in the order that section fixes: empty,
	// length, leading hyphen, reserved, character set.
	if n := utf8.RuneCountInString(name); n > MaxRoadmapNameLength {
		// SPEC/COMMANDS.md + SPEC/ARCHITECTURE.md mandate this verbatim message.
		return &MessageError{
			Msg:       fmt.Sprintf("Roadmap name must not exceed %d characters (got %d)", MaxRoadmapNameLength, n),
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

	// The CLI's own reserved name, in its exact spelling only: every other
	// letter case is refused by the character rule below with that rule's line.
	if name == HelpRoadmapName {
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

	// Refuse to follow a symlink planted at ~/.roadmaps/<name>: the os.Chmod
	// below and the subsequent project.db write would otherwise be redirected
	// through the link to a target outside ~/.roadmaps (finding #72).
	if err := assertNotSymlink(dir); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, DataDirPerm); err != nil {
		return dirIOError(fmt.Errorf("creating roadmap directory %s: %w", dir, err))
	}

	// Ensure permissions are set correctly (umask may have affected creation).
	if err := os.Chmod(dir, DataDirPerm); err != nil {
		return dirIOError(fmt.Errorf("setting permissions on roadmap directory: %w", err))
	}

	// Verify permissions were set correctly.
	if err := VerifyPermissions(dir, DataDirPerm); err != nil {
		return dirIOError(fmt.Errorf("verifying roadmap directory permissions: %w", err))
	}

	return nil
}

// RoadmapHomeOccupied reports whether ~/.roadmaps/<name> is occupied by an
// entry that is neither a directory nor a symbolic link, such as a regular
// file, and returns the absolute path of that entry.
//
// Such an entry is not a roadmap: a roadmap exists only when its home is a
// directory that holds project.db, and an entry of any other kind at that path
// is a roadmap that does not exist, refused with exit code 4 by every command
// that selects it and removed by none (SPEC/COMMANDS.md § Roadmap Selection
// (Always Required), shape 3). `roadmap create` cannot create the home there
// either, and refuses with an I/O line naming the path. A symbolic link is
// outside this rule: it is refused where the home is secured
// (SPEC/ARCHITECTURE.md § Directory Structure), so it is reported as not
// occupied here. Nothing at the path, or a directory, is not occupied.
func RoadmapHomeOccupied(name string) (string, bool, error) {
	dir, err := GetRoadmapDir(name)
	if err != nil {
		return "", false, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return dir, false, nil
		}
		return dir, false, fmt.Errorf("checking roadmap directory: %w", err)
	}
	mode := info.Mode()
	return dir, !mode.IsDir() && mode&os.ModeSymlink == 0, nil
}

// RoadmapExists checks whether a roadmap exists under the current layout,
// i.e. whether ~/.roadmaps/<name>/project.db is present as a regular file.
//
// It is the one existence check of the application, and the CLI and the web
// interface both resolve a roadmap through it. A home occupied by an entry that
// is neither a directory nor a symbolic link, such as a regular file, is a
// roadmap that does not exist, never a failure of the check
// (SPEC/COMMANDS.md § Roadmap Selection (Always Required), shape 3; SPEC/WEB.md
// § Routes and Pages, path-parameter rule 2). Only a failure of the filesystem
// to answer — a home the process may not search, for instance — is an error.
func RoadmapExists(name string) (bool, error) {
	_, occupied, err := RoadmapHomeOccupied(name)
	if err != nil {
		return false, err
	}
	if occupied {
		return false, nil
	}

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
