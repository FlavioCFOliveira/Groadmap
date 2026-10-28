package skillsinstall

import "runtime"

// managedSettingsDir returns the managed settings directory Claude Code reads
// on this platform, or "" when none is documented (SPEC/SKILLS.md
// § Destination Tiers, item 1).
func managedSettingsDir() string {
	return managedSettingsDirFor(runtime.GOOS)
}

// managedSettingsDirFor is managedSettingsDir for the platform goos.
func managedSettingsDirFor(goos string) string {
	switch goos {
	case "linux":
		return "/etc/claude-code"
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return ""
	}
}
