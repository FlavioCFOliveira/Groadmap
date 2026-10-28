//go:build unix

package skillsinstall

import (
	"io/fs"
	"syscall"
)

// fileOwner returns the user id that owns the file fi describes.
func fileOwner(fi fs.FileInfo) (uint32, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

// ownerReason applies the ownership conditions of SPEC/SKILLS.md § Tier
// Destination Rules, rules 3 and 6: the owner of path must be the invoking
// user or, when acceptRoot is set, root. It returns the published reason, or
// "" when the owner is accepted.
func (in *installer) ownerReason(fi fs.FileInfo, path string, acceptRoot bool) string {
	uid, ok := in.ownerOf(fi)
	if !ok {
		return "cannot examine " + path + ": its owner cannot be read"
	}
	return ownerReasonFor(uid, path, in.euid, acceptRoot)
}

// worldWritableReason applies rule 4: a directory writable by every user must
// carry the sticky bit.
func worldWritableReason(fi fs.FileInfo, path string) string {
	if fi.Mode().Perm()&0o002 != 0 && fi.Mode()&fs.ModeSticky == 0 {
		return path + " is writable by every user and carries no sticky bit"
	}
	return ""
}

// isPrivateDir reports whether fi, read without following a link, is a
// directory owned by the invoking user with mode 0700 exactly.
func (in *installer) isPrivateDir(fi fs.FileInfo) bool {
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		return false
	}
	uid, ok := in.ownerOf(fi)
	if !ok || in.euid < 0 || uint64(uid) != uint64(in.euid) {
		return false
	}
	return fi.Mode().Perm() == privateDirMode
}
