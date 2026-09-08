// Package graphclient — the tests for the platform's socket-path bound.
//
// Two kinds of proof live here, and neither is a restatement of the constant:
//
//   - An EMPIRICAL one, which binds real sockets on the host at increasing path
//     lengths and finds the boundary the kernel actually enforces. It is the unit
//     analogue of SPEC/GRAPH.md Acceptance Criterion 67, and it fails against any
//     figure that is not the one in force here.
//   - A CROSS-TARGET one, which takes the expression the constant is declared
//     from — read out of the source with go/ast, never copied into this file —
//     and compiles it, under a two-sided compile-time assertion, for every target
//     SPEC/BUILD.md declares, against the figures SPEC/GRAPH.md publishes. It is
//     what the empirical proof cannot be: the host runs one operating system, and
//     a hard-coded 107 is correct on that one and wrong on three of the other
//     four.
package graphclient

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// bindableAt reports whether the kernel accepts a Unix domain socket whose path
// is exactly n bytes long.
//
// The name is RELATIVE and the test has chdir'd into a temporary directory, which
// is what makes the measurement exact: the kernel copies into sun_path exactly
// the bytes the caller passed, so a relative name of n bytes occupies n bytes of
// the field regardless of how deep the temporary directory happens to be. An
// absolute path would have made the measurable range depend on $TMPDIR's length,
// and on a machine with a long one there would have been no range at all.
func bindableAt(t *testing.T, n int) bool {
	t.Helper()

	name := strings.Repeat("s", n)
	ln, err := net.Listen("unix", name)
	if err != nil {
		return false
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("closing the listener at %d bytes: %v", n, err)
	}
	_ = os.Remove(name) //nolint:errcheck // Close already unlinks it on every supported platform
	return true
}

// TestMaxSocketPathLenIsTheBoundTheKernelEnforces measures the boundary instead
// of asserting the constant against itself.
//
// It binds a real listener at the constant's own figure — which must succeed —
// and at one byte more — which must fail. Both halves are load-bearing. A check
// of the refusal alone passes on a constant that is one byte too strict, and such
// a constant refuses a path the kernel accepts on every platform at once; a check
// of the acceptance alone passes on a constant that is too permissive, which is
// the defect rmp task #412 records, with the operating system's `invalid
// argument` arriving in place of the refusal.
func TestMaxSocketPathLenIsTheBoundTheKernelEnforces(t *testing.T) {
	t.Chdir(t.TempDir())

	if !bindableAt(t, MaxSocketPathLen) {
		t.Fatalf("a socket path of exactly MaxSocketPathLen (%d) bytes could not be bound, so the "+
			"constant is stricter than the platform and refuses paths the kernel accepts",
			MaxSocketPathLen)
	}
	if bindableAt(t, MaxSocketPathLen+1) {
		t.Fatalf("a socket path of MaxSocketPathLen+1 (%d) bytes bound successfully, so the constant "+
			"is more permissive than the platform and would let a caller through to the kernel's "+
			"own bind failure", MaxSocketPathLen+1)
	}

	// The two neighbours, so the measurement is a boundary rather than a pair of
	// coincidences: everything below the bound binds and everything above it does
	// not.
	if !bindableAt(t, MaxSocketPathLen-1) {
		t.Errorf("a socket path of %d bytes could not be bound, one byte under the measured bound",
			MaxSocketPathLen-1)
	}
	if bindableAt(t, MaxSocketPathLen+2) {
		t.Errorf("a socket path of %d bytes bound successfully, two bytes over the measured bound",
			MaxSocketPathLen+2)
	}
}

// TestSocketPathTooLongAtTheBoundary pins the predicate to the same boundary,
// including the off-by-one in both directions.
func TestSocketPathTooLongAtTheBoundary(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "empty", path: "", want: false},
		{name: "one byte under the bound", path: strings.Repeat("a", MaxSocketPathLen-1), want: false},
		{name: "exactly the bound", path: strings.Repeat("a", MaxSocketPathLen), want: false},
		{name: "one byte over the bound", path: strings.Repeat("a", MaxSocketPathLen+1), want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SocketPathTooLong(c.path); got != c.want {
				t.Errorf("SocketPathTooLong(%d bytes) = %v, want %v", len(c.path), got, c.want)
			}
		})
	}
}

// TestSocketPathTooLongCountsBytesAndNotRunes is the check a rune count passes on
// an ASCII machine and fails the moment a roadmap name is not ASCII.
//
// sun_path holds BYTES. A path of MaxSocketPathLen RUNES made of two-byte
// characters occupies twice that many bytes and cannot be bound, so the predicate
// must report it as too long. A rune count would have passed it, and would have
// done so only for callers whose roadmap names are not ASCII — the worst way for
// a bound to be wrong, because it looks correct everywhere it is tested
// (SPEC/GRAPH.md § Socket Path Length, rule 2).
func TestSocketPathTooLongCountsBytesAndNotRunes(t *testing.T) {
	// "é" is U+00E9: one rune, two bytes in UTF-8.
	const twoByteRune = "é"

	// Exactly the bound in RUNES, and twice the bound in bytes.
	atBoundInRunes := strings.Repeat(twoByteRune, MaxSocketPathLen)
	if runes := len([]rune(atBoundInRunes)); runes != MaxSocketPathLen {
		t.Fatalf("fixture is %d runes, want %d", runes, MaxSocketPathLen)
	}
	if !SocketPathTooLong(atBoundInRunes) {
		t.Errorf("a path of %d runes and %d bytes was reported as fitting; the bound is measured in "+
			"bytes, and a rune count would accept a path the kernel refuses",
			MaxSocketPathLen, len(atBoundInRunes))
	}

	// And the mirror: exactly the bound in BYTES, made of the same multi-byte
	// character, fits. Without this half the test above would also pass on a
	// predicate that simply refused every non-ASCII path.
	halfBound := MaxSocketPathLen / 2
	atBoundInBytes := strings.Repeat(twoByteRune, halfBound) + strings.Repeat("a", MaxSocketPathLen-2*halfBound)
	if len(atBoundInBytes) != MaxSocketPathLen {
		t.Fatalf("fixture is %d bytes, want %d", len(atBoundInBytes), MaxSocketPathLen)
	}
	if SocketPathTooLong(atBoundInBytes) {
		t.Errorf("a path of exactly %d bytes carrying multi-byte characters was reported as too "+
			"long; the bound is a byte count and does not care what the bytes encode", MaxSocketPathLen)
	}
}

// ---------------------------------------------------------------------------
// The cross-target proof
// ---------------------------------------------------------------------------

const (
	graphSpecPath = "../../SPEC/GRAPH.md"
	buildSpecPath = "../../SPEC/BUILD.md"
	socketLenFile = "socketlen.go"
)

// specOSNames maps the operating-system names SPEC/GRAPH.md writes for a reader
// onto the GOOS values SPEC/BUILD.md's table declares.
//
// It is a translation between two spellings of the same thing and carries no
// figure of its own: every number in this gate is read from the specification and
// every expression from the source.
var specOSNames = map[string]string{
	"Linux":   "linux",
	"Windows": "windows",
	"macOS":   "darwin",
	"FreeBSD": "freebsd",
	"OpenBSD": "openbsd",
}

// publishedLimitSentence matches the bolded figures of
// SPEC/GRAPH.md § Socket Path Length: "**107 bytes on Linux and Windows**".
//
// The whitespace between "on" and the first operating-system name is \s+ rather
// than a literal space because markdown prose wraps: the second of the two spans
// carries a newline exactly there, and a literal space would have read one figure
// and silently left the other unchecked.
var publishedLimitSentence = regexp.MustCompile(`\*\*(\d+) bytes on\s+([^*]+)\*\*`)

// publishedLimits reads the figure SPEC/GRAPH.md publishes for each operating
// system, keyed by GOOS.
func publishedLimits(t *testing.T) map[string]int {
	t.Helper()

	raw, err := os.ReadFile(graphSpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", graphSpecPath, err)
	}

	limits := make(map[string]int)
	for _, match := range publishedLimitSentence.FindAllStringSubmatch(string(raw), -1) {
		limit, convErr := strconv.Atoi(match[1])
		if convErr != nil {
			t.Fatalf("%s publishes a non-numeric limit %q", graphSpecPath, match[1])
		}
		for _, name := range splitOSList(match[2]) {
			goos, known := specOSNames[name]
			if !known {
				t.Fatalf("%s publishes a limit for %q, which this gate cannot map onto a GOOS. "+
					"Add it to specOSNames, or the target it names goes unchecked", graphSpecPath, name)
			}
			if previous, seen := limits[goos]; seen && previous != limit {
				t.Fatalf("%s publishes two different limits for %s: %d and %d", graphSpecPath, goos, previous, limit)
			}
			limits[goos] = limit
		}
	}
	if len(limits) == 0 {
		t.Fatalf("%s § Socket Path Length publishes no bolded limit this gate can read; the sentence "+
			"was reworded and the gate is now checking nothing", graphSpecPath)
	}
	return limits
}

// splitOSList turns "macOS, FreeBSD and OpenBSD" into its three names.
func splitOSList(list string) []string {
	list = strings.ReplaceAll(list, " and ", ",")
	var names []string
	for _, part := range strings.Split(list, ",") {
		if name := strings.TrimSpace(part); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// buildTargetRow is one GOOS/GOARCH pair of SPEC/BUILD.md § Primary Platforms.
type buildTargetRow struct {
	goos   string
	goarch string
}

// supportedTargets reads SPEC/BUILD.md § Primary Platforms.
//
// The list is read rather than written out so a target added to the
// specification is covered by this gate the day it is added, with no second
// place to remember.
func supportedTargets(t *testing.T) []buildTargetRow {
	t.Helper()

	raw, err := os.ReadFile(buildSpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", buildSpecPath, err)
	}
	const heading = "### Primary Platforms"
	spec := string(raw)
	start := strings.Index(spec, heading)
	if start < 0 {
		t.Fatalf("%s has no %q section", buildSpecPath, heading)
	}
	section := spec[start+len(heading):]
	if end := strings.Index(section, "\n#"); end >= 0 {
		section = section[:end]
	}

	var targets []buildTargetRow
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 2 {
			continue
		}
		goos := strings.TrimSpace(cells[0])
		goarch := strings.TrimSpace(cells[1])
		if goos == "GOOS" || strings.HasPrefix(goos, "---") {
			continue
		}
		targets = append(targets, buildTargetRow{goos: goos, goarch: goarch})
	}
	if len(targets) == 0 {
		t.Fatalf("parsed no rows from %s § Primary Platforms; the table format changed", buildSpecPath)
	}
	return targets
}

// declaredLimitExpression returns the source text of the expression
// MaxSocketPathLen is declared from, read out of socketlen.go.
//
// Reading the EXPRESSION rather than the value is what makes the gate below
// capable of failing a hard-coded figure. A constant declared as `107` yields the
// text "107", which then fails to compile against the 103 the specification
// publishes for macOS, FreeBSD and OpenBSD — on this host, without a macOS
// machine anywhere in sight.
func declaredLimitExpression(t *testing.T) string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, socketLenFile, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", socketLenFile, err)
	}

	for _, decl := range file.Decls {
		gen, isGen := decl.(*ast.GenDecl)
		if !isGen || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, isValue := spec.(*ast.ValueSpec)
			if !isValue || len(value.Names) != 1 || value.Names[0].Name != "MaxSocketPathLen" {
				continue
			}
			if len(value.Values) != 1 {
				t.Fatalf("MaxSocketPathLen is declared with %d values; this gate reads one expression",
					len(value.Values))
			}
			var rendered strings.Builder
			if printErr := printer.Fprint(&rendered, fset, value.Values[0]); printErr != nil {
				t.Fatalf("rendering the MaxSocketPathLen expression: %v", printErr)
			}
			return rendered.String()
		}
	}
	t.Fatalf("%s declares no constant MaxSocketPathLen; this gate has nothing to check", socketLenFile)
	return ""
}

// TestDeclaredLimitYieldsTheSpecifiedFigureOnEverySupportedTarget compiles the
// constant's own expression for every target SPEC/BUILD.md declares and asserts,
// at compile time and from both directions, that it yields the figure
// SPEC/GRAPH.md publishes for that operating system.
//
// The two-sided assertion matters. `uint(derived - want)` alone fails only when
// the expression is SMALLER than the published figure; the mirror term fails when
// it is larger. One of them on its own can be satisfied vacuously, and a gate
// that could only be wrong in one direction is not a boundary.
//
// The probe is its own module in a temporary directory rather than a file dropped
// into this package: it must be compiled for nine targets without the rest of
// Groadmap coming with it, and it must leave nothing behind in the repository if
// the run is interrupted.
func TestDeclaredLimitYieldsTheSpecifiedFigureOnEverySupportedTarget(t *testing.T) {
	expression := declaredLimitExpression(t)
	limits := publishedLimits(t)
	targets := supportedTargets(t)

	// Totality: every target the project builds must have a published figure, or
	// the gate would silently cover fewer platforms than the project ships.
	for _, target := range targets {
		if _, published := limits[target.goos]; !published {
			t.Fatalf("SPEC/BUILD.md declares the target %s/%s but SPEC/GRAPH.md § Socket Path Length "+
				"publishes no limit for %s, so that target's derivation goes unchecked",
				target.goos, target.goarch, target.goos)
		}
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module socketlenprobe\n\ngo 1.27.0\n"), 0600); err != nil {
		t.Fatalf("writing the probe module: %v", err)
	}

	imports := ""
	if strings.Contains(expression, "syscall.") {
		imports = "import \"syscall\"\n"
	}

	for _, target := range targets {
		t.Run(target.goos+"/"+target.goarch, func(t *testing.T) {
			t.Parallel()

			want := limits[target.goos]
			probe := fmt.Sprintf(`package main

%s
const derived = %s
const want = %d

// Two-sided, so the assertion is a boundary and not a one-way inequality: an
// untyped negative constant cannot be converted to uint, so either term fails to
// compile the moment the two figures differ.
const _ = uint(derived - want)
const _ = uint(want - derived)

func main() {}
`, imports, expression, want)

			// One file per target, since the subtests run in parallel.
			name := "probe_" + target.goos + "_" + target.goarch
			probeDir := filepath.Join(dir, name)
			if err := os.MkdirAll(probeDir, 0750); err != nil {
				t.Fatalf("creating %s: %v", probeDir, err)
			}
			if err := os.WriteFile(filepath.Join(probeDir, "main.go"), []byte(probe), 0600); err != nil {
				t.Fatalf("writing the probe: %v", err)
			}
			if err := os.WriteFile(filepath.Join(probeDir, "go.mod"),
				[]byte("module "+name+"\n\ngo 1.27.0\n"), 0600); err != nil {
				t.Fatalf("writing the probe module: %v", err)
			}

			cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
			cmd.Dir = probeDir
			cmd.Env = append(os.Environ(),
				"CGO_ENABLED=0",
				"GOOS="+target.goos,
				"GOARCH="+target.goarch,
				"GOARM=",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("the expression MaxSocketPathLen is declared from (%s) does not yield the %d "+
					"bytes SPEC/GRAPH.md publishes for %s, on %s/%s:\n%s\n\n"+
					"A figure written out in socketlen.go rather than derived is the usual cause: "+
					"the same literal cannot be right on both a 108-byte and a 104-byte sun_path.",
					expression, want, target.goos, target.goos, target.goarch, out)
			}
		})
	}
}
