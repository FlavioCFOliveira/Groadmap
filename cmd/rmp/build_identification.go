package main

import "runtime/debug"

// shortCommitLength is how many characters of the vcs.revision build setting
// the version line displays: always exactly seven, a fixed-length prefix rather
// than git's variable-length abbreviation (SPEC/VERSION.md § Short commit).
const shortCommitLength = 7

// Build settings the Go toolchain records for a binary built inside a
// version-control working tree. They are the only two the version line reads
// (SPEC/VERSION.md § Mechanism).
const (
	buildSettingRevision = "vcs.revision"
	buildSettingModified = "vcs.modified"
)

// versionLine renders the line `rmp version`, `rmp --version` and `rmp -v`
// write, without its trailing newline: the application name, the version
// constant, and the build identification in parentheses (SPEC/COMMANDS.md §
// Version).
//
// It takes the two results of runtime/debug.ReadBuildInfo rather than calling
// it, so a test can supply the build information itself: a test binary carries
// no vcs.* settings, and could never observe the stamp of a real build.
func versionLine(info *debug.BuildInfo, ok bool) string {
	return appName + " version " + version + " (" + buildIdentification(info, ok) + ")"
}

// buildIdentification returns what the version line holds inside its
// parentheses, by the three displays of SPEC/VERSION.md § Build Identification:
//
//   - "commit <commit>" when vcs.revision is at least seven characters long and
//     vcs.modified is not exactly "true";
//   - "commit <commit>, modified" when vcs.revision is at least seven characters
//     long and vcs.modified is exactly "true";
//   - "commit unknown" for anything else: no build information, no vcs.revision,
//     or a vcs.revision shorter than seven characters. This display does not
//     consult vcs.modified.
//
// No other setting is read. In particular the main module's version, which the
// toolchain derives from the repository's tags, is ignored: the version number
// comes from the constant alone.
func buildIdentification(info *debug.BuildInfo, ok bool) string {
	const unknown = "commit unknown"
	if !ok || info == nil {
		return unknown
	}

	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case buildSettingRevision:
			revision = setting.Value
		case buildSettingModified:
			modified = setting.Value
		}
	}

	// A revision is a hexadecimal hash, so its characters are single bytes and
	// the byte length is the character count.
	if len(revision) < shortCommitLength {
		return unknown
	}

	commit := "commit " + revision[:shortCommitLength]
	if modified == "true" {
		return commit + ", modified"
	}
	return commit
}
