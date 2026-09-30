//go:build !windows

package graphserve

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// stagingPrefix is the reserved prefix of a staging directory's name, and
// stagingSuffixLen the number of random characters that follow it
// (SPEC/GRAPH.md § Socket Path and Permissions, rule 8).
const (
	stagingPrefix    = ".rmp-bind-"
	stagingSuffixLen = 6
)

// stagingAlphabet is the character set of the random suffix: lower-case ASCII
// letters and ASCII digits.
const stagingAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// stagedSocketName is the socket's name inside the staging directory.
const stagedSocketName = "s"

// stagedPathExcess is how many bytes the transient bind path adds to the
// socket's directory beyond a separator: the staging name, a separator and the
// socket's name inside it. The transient path is therefore the target's length,
// less its final component, plus this (SPEC/GRAPH.md § Socket Path Length,
// rule 9).
const stagedPathExcess = len(stagingPrefix) + stagingSuffixLen + 1 + len(stagedSocketName)

// stagingAttempts bounds how many staging-directory names bind tries before it
// gives up. A name is refused only when something already occupies it, and the
// names are random, so exhausting the bound means the directory is being filled
// faster than it can be probed — which is a refusal to report, not a condition
// to wait out.
const stagingAttempts = 64

// errNoStagingName is what an exhausted search for a free staging-directory name
// reports. It is a sentinel because err113 forbids constructing an error at the
// call site.
var errNoStagingName = errors.New("no free name for the socket's staging directory")

// ServeSocketPathLimit is the most bytes a socket path with socket's final
// component may occupy for `rmp graph serve` on this platform: the platform's
// bound, less the bytes by which the transient bind path exceeds the target when
// it does (SPEC/GRAPH.md § Socket Path Length, rule 9). A final component of
// stagedPathExcess bytes or more makes the transient path no longer than the
// target, and the limit is then the platform's bound itself.
//
// It is the M of the published path-length line when `graph serve` refuses, and
// it is exported because the CLI resolves and refuses the path before it reaches
// this package, while the transient path is this package's to define.
func ServeSocketPathLimit(socket string) int {
	excess := stagedPathExcess - len(filepath.Base(socket))
	if excess < 0 {
		excess = 0
	}
	return graphclient.MaxSocketPathLen - excess
}

// bind creates the listener with the socket at mode 0600 from its first instant
// at the path (SPEC/GRAPH.md § Socket Path and Permissions, rule 3; § Server
// Startup, step 5).
//
// # Why the mode cannot be set after the listener exists
//
// Binding a Unix domain socket creates its file with the mode the process umask
// leaves, and the kernel accepts connections into the listener's queue from the
// moment it listens — before the server answers anything. A chmod that follows
// the listen therefore narrows the mode of a socket that was already connectable
// under the wider one, and a connection accepted in that interval is not revoked
// by it. Under a umask of 0000 and a --socket path in a directory every account
// can write, that interval admitted another account's connection 3 times in 3
// (rmp task #561).
//
// # The mechanism
//
// The socket is created, listened on, and narrowed somewhere no other account can
// reach, and only then given its name:
//
//  1. A private directory is created beside the target, in the same parent and
//     therefore on the same filesystem, at mode 0700 or narrower. No other
//     account can traverse it, so nothing inside it is connectable by anyone the
//     final mode would deny, whatever mode the socket has there.
//  2. The listener binds and listens at a path inside that directory, and the
//     socket's mode is set to 0600 there.
//  3. The socket is hard-linked to the target path. The link is ONE directory
//     entry for the same inode, so the file appears at the target with the mode
//     it already carries: nobody can observe it at the target under any other
//     mode, and nobody can connect to it there before it has that mode.
//  4. The staging name and the staging directory are removed. The listener is
//     bound to the inode, not to a name, so it keeps accepting through the
//     target.
//
// A link and not a rename, because a rename REPLACES whatever occupies the target,
// and a bind never did: [removeStaleSocket] removes a stale socket and nothing
// else, precisely so that a mistyped --socket naming a file the caller cares
// about fails with the bind line rather than destroying it. link(2) refuses an
// occupied target exactly as bind(2) did, so that property is unchanged.
//
// No process-wide state is touched. Narrowing the umask around the bind would
// achieve the same end with one call, and it is not used because the umask
// belongs to the whole process: every file any other goroutine creates in that
// interval would be created under it.
//
// # The names, and the length of the staged path
//
// The staging directory is named .rmp-bind- and six random characters, and the
// socket inside it s (SPEC/GRAPH.md § Socket Path and Permissions, rule 8). The
// staged path is bound, so the platform's bound applies to it as much as to the
// target; `rmp graph serve` refuses a target whose staged path would exceed it
// before it reaches here (§ Socket Path Length, rule 9; [ServeSocketPathLimit]).
//
// # Residue
//
// A server killed while it binds can leave its staging directory behind. The
// next server removes it in the stale-socket step, and removes nothing else that
// carries the prefix; see [removeStagingResidue].
//
// # What the listener no longer does for itself
//
// The standard library's Unix listener unlinks the path it was bound to when it
// is closed. That path is the staging name, which is gone, so the unlink is
// switched off and [serverListener] removes the target instead — and only while
// it still names this server's socket, which is what [boundSocket] records.
func bind(path string) (*serverListener, error) {
	fail := func(err error) error {
		return fmt.Errorf("%w: cannot bind %s: %v", utils.ErrGraphServer, path, err)
	}

	stagingDir, err := makeStagingDir(filepath.Dir(path))
	if err != nil {
		return nil, fail(err)
	}
	// The staging directory is empty on every path out of this function once the
	// staged name has been removed, and removing it is best-effort: a leftover
	// empty directory is private to this user and costs nothing but a name.
	defer func() { _ = os.Remove(stagingDir) }() //nolint:errcheck // best-effort removal of an empty private directory

	staged := filepath.Join(stagingDir, stagedSocketName)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: staged, Net: "unix"})
	if err != nil {
		return nil, fail(err)
	}
	// The listener must never unlink by name: the staged name is removed below,
	// and the target is removed by serverListener, which checks it is still ours.
	ln.SetUnlinkOnClose(false)

	closeAll := func() {
		_ = ln.Close()        //nolint:errcheck // already failing; the close error cannot be acted on
		_ = os.Remove(staged) //nolint:errcheck // idem: best-effort cleanup inside the private directory
	}

	if err := os.Chmod(staged, graphclient.SocketMode); err != nil {
		closeAll()
		return nil, fail(err)
	}
	info, err := os.Lstat(staged)
	if err != nil {
		closeAll()
		return nil, fail(err)
	}
	if err := os.Link(staged, path); err != nil {
		closeAll()
		return nil, fail(err)
	}
	if err := os.Remove(staged); err != nil {
		closeAll()
		removeIfSame(path, info)
		return nil, fail(err)
	}

	l := newServerListener(ln)
	l.bound = &boundSocket{path: path, info: info}
	return l, nil
}

// stagingName returns a fresh random staging-directory name: the reserved
// prefix and six characters of [stagingAlphabet].
func stagingName() (string, error) {
	var raw [stagingSuffixLen]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	name := make([]byte, 0, len(stagingPrefix)+stagingSuffixLen)
	name = append(name, stagingPrefix...)
	for _, b := range raw {
		name = append(name, stagingAlphabet[int(b)%len(stagingAlphabet)])
	}
	return string(name), nil
}

// makeStagingDir creates a private staging directory inside parent and returns
// its path.
//
// The directory is created at 0700 and left to the umask, which can only NARROW
// the mode mkdir(2) applies: no umask lets another account traverse it, which is
// the only property the staging relies on. A umask that took the owner's own
// write or search permission away would make the bind inside it fail, with the
// published bind line, rather than widen anything.
//
// A name that is already taken is replaced by another random one of the same
// form; see [stagingAttempts] for the bound.
func makeStagingDir(parent string) (string, error) {
	for range stagingAttempts {
		name, err := stagingName()
		if err != nil {
			return "", err
		}
		dir := filepath.Join(parent, name)
		err = os.Mkdir(dir, 0o700)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", errNoStagingName
}

// removeStagingResidue removes the staging directories a server killed while
// it was binding left in dir, and nothing else that carries the prefix
// (SPEC/GRAPH.md § Socket Path and Permissions, rule 9; § Server Startup,
// step 4).
//
// An entry whose name begins with the reserved prefix is removed, with its
// content, when, and only when, it is a directory and not a symbolic link, it is
// owned by the user running the server, and it is empty or holds exactly one
// entry that is a socket accepting no connection. Every other entry is left
// exactly as it is. Nothing here refuses the start or reports anything: an entry
// this server cannot recognise as Groadmap's own is somebody else's, and a
// failure to remove residue that is Groadmap's costs a name and nothing more.
//
// A socket that ACCEPTS a connection is left alone because it belongs to
// another `rmp graph serve` binding in the same directory at this moment. Only a
// refused connection is read as "no connection is accepted"; any other failure
// to connect — a full queue among them — may be a live listener, and is left.
func removeStagingResidue(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), stagingPrefix) {
			removeIfResidue(filepath.Join(dir, entry.Name()))
		}
	}
}

// removeIfResidue removes candidate if it is staging residue as
// [removeStagingResidue] defines it.
func removeIfResidue(candidate string) {
	info, err := os.Lstat(candidate)
	if err != nil || !info.IsDir() || !ownedByCurrentUser(info) {
		return
	}
	inside, err := os.ReadDir(candidate)
	if err != nil {
		return
	}
	switch len(inside) {
	case 0:
	case 1:
		socket := filepath.Join(candidate, inside[0].Name())
		socketInfo, statErr := os.Lstat(socket)
		if statErr != nil || socketInfo.Mode()&os.ModeSocket == 0 || !refusesConnections(socket) {
			return
		}
		if rmErr := os.Remove(socket); rmErr != nil {
			return
		}
	default:
		return
	}
	_ = os.Remove(candidate) //nolint:errcheck // residue that cannot be removed costs a name; see removeStagingResidue
}

// ownedByCurrentUser reports whether info describes a file the user running
// this process owns.
func ownedByCurrentUser(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(st.Uid) == int64(os.Getuid())
}

// refusesConnections reports whether a connection to the socket at path is
// refused, which is what a socket nothing listens on answers.
func refusesConnections(path string) bool {
	conn, err := net.Dial("unix", path)
	if err == nil {
		_ = conn.Close() //nolint:errcheck // the connection existed; that is all this asked
		return false
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}
