package main

import (
	"runtime/debug"
	"testing"
)

// TestVersionLineIdentifiesTheBuild holds the rule that turns build information
// into one of the three displays of SPEC/VERSION.md § Build Identification
// (acceptance criterion 4). The build information is supplied here rather than
// read, because a test binary carries no vcs.* settings. The version number is
// taken from the application version constant, never written as a literal
// (acceptance criterion 5).
func TestVersionLineIdentifiesTheBuild(t *testing.T) {
	const (
		sha1Revision   = "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567"
		sha256Revision = "9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c5b4a39281706f5e4d3c2b1a0"
	)

	stamped := func(settings ...debug.BuildSetting) *debug.BuildInfo {
		return &debug.BuildInfo{Settings: settings}
	}
	setting := func(key, value string) debug.BuildSetting {
		return debug.BuildSetting{Key: key, Value: value}
	}

	cases := []struct {
		info           *debug.BuildInfo
		name           string
		identification string
		ok             bool
	}{
		{
			name: "clean revision",
			info: stamped(setting("vcs", "git"), setting("vcs.revision", sha1Revision),
				setting("vcs.modified", "false")),
			ok:             true,
			identification: "commit 0a1b2c3",
		},
		{
			name: "modified revision",
			info: stamped(setting("vcs", "git"), setting("vcs.revision", sha1Revision),
				setting("vcs.modified", "true")),
			ok:             true,
			identification: "commit 0a1b2c3, modified",
		},
		{
			name:           "no build information",
			info:           nil,
			ok:             false,
			identification: "commit unknown",
		},
		{
			name:           "build information reported present but absent",
			info:           nil,
			ok:             true,
			identification: "commit unknown",
		},
		{
			name: "no vcs.revision, even with vcs.modified true",
			info: stamped(setting("vcs", "git"), setting("vcs.modified", "true"),
				setting("vcs.time", "2026-09-14T10:00:00Z")),
			ok:             true,
			identification: "commit unknown",
		},
		{
			name:           "vcs.revision shorter than seven characters, even with vcs.modified true",
			info:           stamped(setting("vcs.revision", "0a1b2c"), setting("vcs.modified", "true")),
			ok:             true,
			identification: "commit unknown",
		},
		{
			name:           "empty vcs.revision",
			info:           stamped(setting("vcs.revision", ""), setting("vcs.modified", "false")),
			ok:             true,
			identification: "commit unknown",
		},
		{
			name:           "vcs.revision of exactly seven characters",
			info:           stamped(setting("vcs.revision", "0a1b2c3"), setting("vcs.modified", "false")),
			ok:             true,
			identification: "commit 0a1b2c3",
		},
		{
			name:           "vcs.modified absent",
			info:           stamped(setting("vcs.revision", sha1Revision)),
			ok:             true,
			identification: "commit 0a1b2c3",
		},
		{
			name:           "vcs.modified not exactly true",
			info:           stamped(setting("vcs.revision", sha1Revision), setting("vcs.modified", "True")),
			ok:             true,
			identification: "commit 0a1b2c3",
		},
		{
			name:           "SHA-256 revision",
			info:           stamped(setting("vcs.revision", sha256Revision), setting("vcs.modified", "false")),
			ok:             true,
			identification: "commit 9f8e7d6",
		},
		{
			// The main module's version and every other setting are ignored:
			// the number on the line is the constant's, whatever the toolchain
			// derived from the repository's tags.
			name: "no other setting is read",
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "github.com/FlavioCFOliveira/Groadmap", Version: "v0.0.0-unrelated"},
				Settings: []debug.BuildSetting{
					setting("-buildmode", "exe"), setting("-trimpath", "true"),
					setting("vcs", "git"), setting("vcs.revision", sha1Revision),
					setting("vcs.time", "2026-09-14T10:00:00Z"), setting("vcs.modified", "false"),
				},
			},
			ok:             true,
			identification: "commit 0a1b2c3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := "Groadmap version " + version + " (" + tc.identification + ")"
			if got := versionLine(tc.info, tc.ok); got != want {
				t.Errorf("versionLine() = %q, want %q", got, want)
			}
		})
	}
}
