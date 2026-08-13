package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rmpPackage is the import path of the released binary. It is used instead of a
// relative path so the build works regardless of the directory `go test` runs
// the package from, as long as it is inside this module.
const rmpPackage = "github.com/FlavioCFOliveira/Groadmap/cmd/rmp"

// specBuildPath locates SPEC/BUILD.md from this package's directory, which is
// where `go test` sets the working directory.
const specBuildPath = "../../SPEC/BUILD.md"

// buildTarget is one row of SPEC/BUILD.md § Supported Build Targets §
// Primary Platforms.
type buildTarget struct {
	goos   string
	goarch string
	goarm  string // empty when the SPEC row has no GOARM value
	name   string // the SPEC's "Target Name" column
}

// supportedBuildTargets mirrors SPEC/BUILD.md § Supported Build Targets §
// Primary Platforms. It is written out rather than parsed so the matrix is
// reviewable in the diff; TestSupportedBuildTargetsMatchSpec keeps it honest by
// failing if it ever drifts from the specification table.
var supportedBuildTargets = []buildTarget{
	{goos: "linux", goarch: "amd64", name: "linux-amd64"},
	{goos: "linux", goarch: "arm64", name: "linux-arm64"},
	{goos: "linux", goarch: "arm", goarm: "6", name: "linux-armv6"},
	{goos: "linux", goarch: "arm", goarm: "7", name: "linux-armv7"},
	{goos: "darwin", goarch: "amd64", name: "darwin-amd64"},
	{goos: "darwin", goarch: "arm64", name: "darwin-arm64"},
	{goos: "windows", goarch: "amd64", name: "windows-amd64"},
	{goos: "windows", goarch: "arm64", name: "windows-arm64"},
	{goos: "freebsd", goarch: "amd64", name: "freebsd-amd64"},
	{goos: "openbsd", goarch: "amd64", name: "openbsd-amd64"},
	{goos: "openbsd", goarch: "arm64", name: "openbsd-arm64"},
}

// TestSupportedBuildTargetsCompile is the regression gate for task #136: the
// windows/amd64 and windows/arm64 targets that SPEC/BUILD.md promises did not
// compile, because internal/commands called the Unix-only syscall.Flock from an
// untagged file. Nothing in the validation gates noticed, since every gate runs
// only for the host platform.
//
// It builds the released binary for every target in SPEC/BUILD.md § Supported
// Build Targets, so any future platform-specific break — an unguarded syscall,
// a dependency that drops a port — fails `go test ./...` instead of surfacing
// only when a release is cut. The project is pure Go and links with
// CGO_ENABLED=0, so no cross-toolchain is needed: the Go toolchain alone
// compiles and links every target.
func TestSupportedBuildTargetsCompile(t *testing.T) {
	for _, target := range supportedBuildTargets {
		t.Run(target.name, func(t *testing.T) {
			// Cross-builds are independent and the build cache makes
			// repeat runs cheap; run them concurrently so the gate stays
			// quick. `go test` caps this at -parallel (GOMAXPROCS).
			t.Parallel()

			cmd := exec.Command("go", "build", "-o", os.DevNull, rmpPackage)
			// GOARM is set unconditionally so an inherited value cannot
			// silently change which ARM variant is built; empty means the
			// toolchain default, which is what the non-ARM rows want.
			cmd.Env = append(os.Environ(),
				"CGO_ENABLED=0",
				"GOOS="+target.goos,
				"GOARCH="+target.goarch,
				"GOARM="+target.goarm,
			)

			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("target %s (GOOS=%s GOARCH=%s GOARM=%q) does not build, "+
					"but SPEC/BUILD.md lists it as a supported build target: %v\n%s",
					target.name, target.goos, target.goarch, target.goarm, err, out)
			}
		})
	}
}

// TestSupportedBuildTargetsMatchSpec pins supportedBuildTargets to the
// specification. Without it, TestSupportedBuildTargetsCompile could keep
// passing while silently covering fewer targets than SPEC/BUILD.md promises —
// which is exactly how the Windows break went unnoticed. Adding a row to the
// SPEC table therefore fails this test until the matrix above is extended and
// the new target genuinely builds.
func TestSupportedBuildTargetsMatchSpec(t *testing.T) {
	spec, err := os.ReadFile(filepath.Clean(specBuildPath))
	if err != nil {
		t.Fatalf("reading the build specification: %v", err)
	}

	want := parsePrimaryPlatforms(t, string(spec))
	if len(want) != len(supportedBuildTargets) {
		t.Fatalf("SPEC/BUILD.md lists %d primary platforms but the test matrix has %d: %+v vs %+v",
			len(want), len(supportedBuildTargets), want, supportedBuildTargets)
	}
	for i, w := range want {
		if got := supportedBuildTargets[i]; got != w {
			t.Errorf("target %d drifted from SPEC/BUILD.md: got %+v, spec says %+v", i, got, w)
		}
	}

	// The target name is not decoration: SPEC/BUILD.md § Artifact Structure
	// names release archives rmp-{version}-{target}.{ext}, so a mismatch
	// between the name and the GOOS/GOARCH/GOARM triple it stands for would
	// ship a mislabelled binary.
	for _, target := range supportedBuildTargets {
		derived := target.goos + "-" + target.goarch
		if target.goarm != "" {
			derived += "v" + target.goarm
		}
		if derived != target.name {
			t.Errorf("target name %q does not match its GOOS/GOARCH/GOARM triple (expected %q)", target.name, derived)
		}
	}
}

// parsePrimaryPlatforms extracts the Primary Platforms table from
// SPEC/BUILD.md. The table has the columns GOOS, GOARCH, GOARM, Target Name and
// Notes, and uses "-" for rows with no GOARM value.
func parsePrimaryPlatforms(t *testing.T, spec string) []buildTarget {
	t.Helper()

	const heading = "### Primary Platforms"
	start := strings.Index(spec, heading)
	if start < 0 {
		t.Fatalf("SPEC/BUILD.md has no %q section; the build-target gate cannot verify itself", heading)
	}
	section := spec[start+len(heading):]
	if end := strings.Index(section, "\n#"); end >= 0 {
		section = section[:end]
	}

	var targets []buildTarget
	for line := range strings.Lines(section) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 4 {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if cells[0] == "GOOS" || strings.HasPrefix(cells[0], "---") {
			continue // header or separator row
		}
		goarm := cells[2]
		if goarm == "-" {
			goarm = ""
		}
		targets = append(targets, buildTarget{
			goos:   cells[0],
			goarch: cells[1],
			goarm:  goarm,
			name:   cells[3],
		})
	}

	if len(targets) == 0 {
		t.Fatal("parsed no rows from the SPEC/BUILD.md Primary Platforms table; the table format changed")
	}
	return targets
}
