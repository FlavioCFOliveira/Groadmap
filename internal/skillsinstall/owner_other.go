//go:build !unix

package skillsinstall

import "io/fs"

// On a platform without POSIX file modes and owners the ownership and
// permission conditions are not checked: access is decided by the
// access-control list a file inherits from its directory, and the operating
// system's own refusal of a write is what fails a tier (SPEC/SKILLS.md § Tier
// Destination Rules).

// fileOwner reports that no owner can be read on this platform.
func fileOwner(fs.FileInfo) (uint32, bool) { return 0, false }

func (in *installer) ownerReason(fs.FileInfo, string, bool) string { return "" }

func worldWritableReason(fs.FileInfo, string) string { return "" }

// isPrivateDir accepts any directory that is not a symbolic link.
func (in *installer) isPrivateDir(fi fs.FileInfo) bool {
	return fi.Mode()&fs.ModeSymlink == 0 && fi.IsDir()
}
