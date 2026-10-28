// Package skillsinstall fetches, validates, and installs the two agent skills
// the repository ships, roadmap-manager and knowledge-authority, where Claude
// Code loads them (SPEC/SKILLS.md § Skills Install Command).
//
// It owns everything `rmp skills install` does after its arguments are
// accepted: the download of the fixed source, the private staging directory,
// the validation and extraction of the archive, the three destination tiers
// and their rules, the replacement procedure and its rollback, and the reaction
// to a signal (SPEC/ARCHITECTURE.md § Modules and Responsibilities, module 12).
//
// What it deliberately does not own: it writes nothing to stdout or stderr and
// chooses no exit code. [Install] returns the outcome, hands every warning line
// to the caller's [Options.Warn], and reports an interruption as an
// [*InterruptedError] for the caller to turn into exit code 130. It registers
// no signal handler of its own; it takes the action over through
// internal/signals.
//
// The source, the tiers, and the failure points are parameters of [Options],
// so the package's tests exercise every path against a local HTTPS server and
// directories they create (SPEC/SKILLS.md § Install Verification). The command
// passes the fixed values; nothing outside the package's tests passes any
// other.
package skillsinstall

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/signals"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// SourceURL is the one URL the command fetches: the archive of the
// repository's main branch (SPEC/SKILLS.md § Install Source, item 1).
const SourceURL = "https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz"

// The scope values a tier publishes (SPEC/SKILLS.md § Destination Tiers).
const (
	ScopeGlobal  = "global"
	ScopeUser    = "user"
	ScopeProject = "project"
)

// The bounds of SPEC/SKILLS.md § Install Source, items 4 and 6.
const (
	maxBodyBytes       = 64 << 20
	maxStreamBytes     = 256 << 20
	maxSelectedBytes   = 16 << 20
	maxSelectedEntries = 1000
	downloadTimeout    = 300 * time.Second
	maxRedirects       = 10
)

// The names the command creates (SPEC/SKILLS.md § Install Staging Directory,
// item 3, and § Tier Replacement Procedure, item 2).
const (
	stagingPattern = "rmp-skills-install."
	workPattern    = ".rmp-skills-install."
)

// The modes of SPEC/SKILLS.md § Tier Modes.
const (
	privateDirMode  os.FileMode = 0o700
	privateFileMode os.FileMode = 0o600
	sharedDirMode   os.FileMode = 0o755
	sharedFileMode  os.FileMode = 0o644
)

// skillNames are the two skills, in the order they are replaced.
var skillNames = [...]string{"roadmap-manager", "knowledge-authority"}

// SkillNames returns the two skills in the order they are replaced, the value
// the output publishes as `skills`.
func SkillNames() []string {
	return []string{skillNames[0], skillNames[1]}
}

// Skipped is one tier tried before the one used: its scope and the reason it
// was not used (SPEC/COMMANDS.md § Skills Install Output).
type Skipped struct {
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}

// Result is the success output of the command. The field order is the key
// order SPEC/COMMANDS.md § Skills Install Output publishes.
type Result struct {
	Scope     string    `json:"scope"`
	Directory string    `json:"directory"`
	Skills    []string  `json:"skills"`
	Source    string    `json:"source"`
	Skipped   []Skipped `json:"skipped"`
}

// Tier is one destination the command tries (SPEC/SKILLS.md § Destination
// Tiers and § Tier Destination Rules).
type Tier struct {
	// Scope is the tier's scope value: ScopeGlobal, ScopeUser or ScopeProject.
	Scope string
	// Unavailable, when not empty, is the reason the tier fails without
	// touching the filesystem.
	Unavailable string
	// ConfigDir is the directory that holds SkillsDir.
	ConfigDir string
	// SkillsDir is the tier's skills directory.
	SkillsDir string
	// Create lists, outermost first, every directory the tier creates when it
	// is missing; it ends with ConfigDir and SkillsDir.
	Create []string
	// DirMode and FileMode are the tier's modes (SPEC/SKILLS.md § Tier Modes).
	DirMode  os.FileMode
	FileMode os.FileMode
	// AcceptRoot admits root (user id 0) as an owner beside the invoking user.
	AcceptRoot bool
}

// DefaultTiers returns the three tiers of SPEC/SKILLS.md § Destination Tiers,
// in the order they are tried, for the home directory home. The caller has
// already refused a home directory that is not an absolute path
// (SPEC/ARCHITECTURE.md § Location Rules, rule 1).
func DefaultTiers(home string) []Tier {
	tiers := make([]Tier, 0, 3)

	global := Tier{Scope: ScopeGlobal, DirMode: sharedDirMode, FileMode: sharedFileMode, AcceptRoot: true}
	if managed := managedSettingsDir(); managed == "" {
		global.Unavailable = "no managed settings directory is documented for this platform"
	} else {
		global.ConfigDir = filepath.Join(managed, ".claude")
		global.SkillsDir = filepath.Join(global.ConfigDir, "skills")
		global.Create = []string{managed, global.ConfigDir, global.SkillsDir}
	}
	tiers = append(tiers, global)

	user := Tier{Scope: ScopeUser, DirMode: privateDirMode, FileMode: privateFileMode}
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		if filepath.IsAbs(v) {
			user.ConfigDir = filepath.Clean(v)
		} else {
			user.Unavailable = fmt.Sprintf("CLAUDE_CONFIG_DIR %q is not an absolute path", v)
		}
	} else {
		user.ConfigDir = filepath.Join(home, ".claude")
	}
	if user.Unavailable == "" {
		user.SkillsDir = filepath.Join(user.ConfigDir, "skills")
		user.Create = []string{user.ConfigDir, user.SkillsDir}
	}
	tiers = append(tiers, user)

	project := Tier{Scope: ScopeProject, DirMode: privateDirMode, FileMode: privateFileMode}
	if cwd, err := os.Getwd(); err != nil {
		project.Unavailable = "cannot determine the current working directory: " + err.Error()
	} else {
		project.ConfigDir = filepath.Join(cwd, ".claude")
		project.SkillsDir = filepath.Join(project.ConfigDir, "skills")
		project.Create = []string{project.ConfigDir, project.SkillsDir}
	}
	return append(tiers, project)
}

// Options are the parameters of one run.
type Options struct {
	// Transport carries the request; nil means http.DefaultTransport, which
	// honours the standard proxy environment variables and verifies server
	// certificates against the system's trusted roots.
	Transport http.RoundTripper
	// Warn receives each warning line, without its trailing newline, in the
	// order the conditions arise. Nil discards them.
	Warn func(line string)
	// rename moves a whole directory; nil means os.Rename. Tests replace it
	// to inject a failure at a chosen rename.
	rename func(oldpath, newpath string) error
	// takeOver claims SIGINT and SIGTERM; nil means signals.TakeOver. Tests
	// replace it to deliver a signal at a chosen point.
	takeOver func() (<-chan os.Signal, func())
	// ownerOf reads the owner of a file; nil means the platform's own
	// reading. Tests replace it to make a directory they created foreign, or
	// owned by root, without privileges.
	ownerOf func(fs.FileInfo) (uint32, bool)
	// removeAll removes a temporary directory; nil means os.RemoveAll. Tests
	// replace it to make a removal fail.
	removeAll func(path string) error
	// Source is the URL fetched; the command passes SourceURL.
	Source string
	// TempDir is the directory the staging directory is created in; empty
	// means os.TempDir().
	TempDir string
	// Tiers are tried in order; the command passes DefaultTiers.
	Tiers []Tier
	// timeout bounds the whole exchange; zero means downloadTimeout.
	timeout time.Duration
}

// InterruptedError reports that SIGINT or SIGTERM arrived while the command
// held state on disk. Every completed swap step of the tier in progress has
// been undone and the temporary directories removed, unless Err reports that
// the undo itself failed (SPEC/SKILLS.md § Install Interruption).
type InterruptedError struct {
	// Signal is the signal received.
	Signal os.Signal
	// Err is the failed-rollback error, or nil when nothing failed.
	Err error
}

// Error describes the interruption, or the failed rollback it carries.
func (e *InterruptedError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "interrupted by " + e.Signal.String()
}

// Unwrap returns the failed-rollback error, when there is one.
func (e *InterruptedError) Unwrap() error { return e.Err }

// installer is the state of one run.
type installer struct {
	sig       os.Signal
	sigCh     <-chan os.Signal
	opts      *Options
	rename    func(oldpath, newpath string) error
	removeAll func(path string) error
	ownerOf   func(fs.FileInfo) (uint32, bool)
	euid      int
}

// Install runs `rmp skills install`: it downloads and validates the skills
// once, then tries each tier of opts.Tiers in order and stops at the first
// that installs both. It returns the success output, or the error whose line
// the command writes, or an [*InterruptedError].
func Install(opts *Options) (*Result, error) {
	in := &installer{
		opts:      opts,
		rename:    opts.rename,
		removeAll: opts.removeAll,
		ownerOf:   opts.ownerOf,
		euid:      os.Geteuid(),
	}
	if in.rename == nil {
		in.rename = os.Rename
	}
	if in.removeAll == nil {
		in.removeAll = os.RemoveAll
	}
	if in.ownerOf == nil {
		in.ownerOf = fileOwner
	}
	takeOver := opts.takeOver
	if takeOver == nil {
		takeOver = signals.TakeOver
	}

	// The action is taken over before the staging directory is created and
	// handed back after it has been removed: the deferred release runs after
	// the deferred removal below (SPEC/SKILLS.md § Install Interruption).
	sigCh, release := takeOver()
	defer release()
	in.sigCh = sigCh

	staging, err := in.createStaging()
	if err != nil {
		return nil, err
	}
	defer in.removeTemp(staging)

	if err := in.download(staging); err != nil {
		if s := in.pending(); s != nil {
			return nil, &InterruptedError{Signal: s}
		}
		return nil, err
	}
	if s := in.pending(); s != nil {
		return nil, &InterruptedError{Signal: s}
	}

	skipped := make([]Skipped, 0, len(opts.Tiers))
	for i := range opts.Tiers {
		t := &opts.Tiers[i]
		reason, err := in.tryTier(t, staging)
		if err != nil {
			return nil, err
		}
		if reason == "" {
			if s := in.pending(); s != nil {
				return nil, &InterruptedError{Signal: s}
			}
			return &Result{
				Scope:     t.Scope,
				Directory: t.SkillsDir,
				Skills:    SkillNames(),
				Source:    opts.Source,
				Skipped:   skipped,
			}, nil
		}
		skipped = append(skipped, Skipped{Scope: t.Scope, Reason: reason})
		if i+1 == len(opts.Tiers) {
			return nil, fmt.Errorf("%w: the skills could not be installed in the global, user, or project skills directory; the %s skills directory was not used: %s",
				utils.ErrIO, t.Scope, reason)
		}
		in.warn(fmt.Sprintf("warning: the %s skills directory was not used, trying the %s skills directory: %s",
			t.Scope, opts.Tiers[i+1].Scope, reason))
		if s := in.pending(); s != nil {
			return nil, &InterruptedError{Signal: s}
		}
	}
	return nil, fmt.Errorf("%w: the skills could not be installed: no skills directory was tried", utils.ErrIO)
}

// pending reports the signal received so far, if any, without blocking.
func (in *installer) pending() os.Signal {
	if in.sig != nil {
		return in.sig
	}
	select {
	case s := <-in.sigCh:
		in.sig = s
	default:
	}
	return in.sig
}

// warn hands one warning line to the caller.
func (in *installer) warn(line string) {
	if in.opts.Warn != nil {
		in.opts.Warn(line)
	}
}

// removeTemp removes a temporary directory the run created and accepted, and
// warns when the operating system refuses (SPEC/SKILLS.md § Tier Replacement
// Procedure, item 7).
func (in *installer) removeTemp(dir string) {
	if err := in.removeAll(dir); err != nil {
		in.warn(fmt.Sprintf("warning: the temporary directory %s could not be removed: %v", dir, err))
	}
}

// createStaging creates and checks the private staging directory
// (SPEC/SKILLS.md § Install Staging Directory, items 1 to 4).
func (in *installer) createStaging() (string, error) {
	parent := in.opts.TempDir
	if parent == "" {
		parent = os.TempDir()
	}
	fail := func(detail string) error {
		return fmt.Errorf("%w: cannot create a private staging directory: %s", utils.ErrIO, detail)
	}
	if !filepath.IsAbs(parent) {
		return "", fail(fmt.Sprintf("the temporary-file directory %q is not an absolute path", parent))
	}
	fi, err := os.Stat(parent)
	if err != nil {
		return "", fail(fmt.Sprintf("cannot examine %s: %v", parent, err))
	}
	if !fi.IsDir() {
		return "", fail(parent + " is not a directory")
	}
	if reason := worldWritableReason(fi, parent); reason != "" {
		return "", fail(reason)
	}
	dir, err := os.MkdirTemp(parent, stagingPattern)
	if err != nil {
		return "", fail(fmt.Sprintf("cannot create a directory in %s: %v", parent, err))
	}
	if reason := in.privateDirReason(dir); reason != "" {
		// A created directory that fails a check is left in place and never
		// removed (SPEC/SKILLS.md § Install Staging Directory, item 4).
		return "", fail(reason)
	}
	return dir, nil
}

// privateDirReason checks a directory the run created: not a symbolic link,
// owned by the invoking user, and mode 0700 exactly. It returns the reason the
// directory is refused, or "" when it is accepted.
func (in *installer) privateDirReason(dir string) string {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Sprintf("cannot examine %s: %v", dir, err)
	}
	if !in.isPrivateDir(fi) {
		return dir + " is not a private directory owned by the invoking user"
	}
	return ""
}
