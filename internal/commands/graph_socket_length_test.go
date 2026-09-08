// Package commands — the tests for the socket-path-length refusal
// (rmp tasks #412 and #427).
//
// The defect this file exists against printed the operating system's own
// `bind: invalid argument`, which named the path twice and the cause not at all.
// What replaces it is a published line carrying a length, a limit and a remedy,
// and a rule that is UNIFORM: an over-long resolved socket path is refused by
// every surface, however the path was chosen. Both are pinned here — the line
// against SPEC/COMMANDS.md rather than against a copy written out in this file,
// and the rule against the outcomes SPEC/GRAPH.md Acceptance Criterion 66
// requires.
//
// The rule the tests below assert is not the rule they first asserted, and the
// difference is worth recording where the assertions live. Task #412 shipped a
// split on WHO CHOSE THE PATH: a --socket value over the bound failed everywhere,
// while a DERIVED path over the bound failed `serve` and `client` and let
// `execute` and the web graph data endpoint open the store. Task #427 replaced it
// with the uniform rule. A roadmap whose derived socket path cannot be bound has
// a real and permanent fault in its layout, and the split reported that fault at
// two surfaces and concealed it at two others; it also made `graph execute` exit
// 0 with a result where `graph client` refused the same roadmap, which is a
// divergence SPEC/DATA_FORMATS.md § Graph Client Result forbids as a requirement
// rather than merely observes.
//
// The subcommand that inversion was ABOUT is gone with the split it belonged to.
// `graph execute` was the only command-line surface with somewhere else to go,
// and it is withdrawn: `serve` must create the socket, `client` speaks to nothing
// else, and neither ever had a second path for the uniform rule to close. What
// the rule is worth on the command line is therefore no longer that it removes a
// fall back — there is none — but that the refusal is settled from the path
// ALONE, before the probe, so a roadmap whose derived path cannot be bound is
// told that no server can ever listen there rather than that none happens to be
// listening now. TestGraphClient_RefusesAnOverLongResolvedSocketHoweverThePathWasChosen
// is where that lands.
package commands

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// tooLongMarker identifies the published line inside
// SPEC/COMMANDS.md § Graph Server Socket Error Lines. It is a fragment of rmp's
// own text and carries neither of the two numbers.
const tooLongMarker = "socket path is too long"

// wordN and wordM match the two numeric placeholders as WHOLE words, so a path
// substituted into <socket> cannot have an "N" or an "M" of its own rewritten.
// The substitution therefore happens before <socket> is filled in, and the word
// boundaries make the order safe rather than merely lucky.
var (
	wordN = regexp.MustCompile(`\bN\b`)
	wordM = regexp.MustCompile(`\bM\b`)
)

// fillTooLongPlaceholders substitutes the three placeholders the published
// path-length line carries: the resolved path, the path's length in bytes, and
// the platform's limit.
//
// The numbers go in first and by word boundary; the path goes in last. A
// placeholder surviving substitution fails the test, because a comparison against
// a line still carrying one would pass against a message that interpolated
// nothing.
func fillTooLongPlaceholders(t *testing.T, published, socket string, length, limit int) string {
	t.Helper()

	if !wordN.MatchString(published) || !wordM.MatchString(published) {
		t.Fatalf("the published line %q does not carry both numeric placeholders N and M; "+
			"SPEC/GRAPH.md § Socket Path Length, rule 4, requires the line to name the actual "+
			"length AND the derived limit", published)
	}
	filled := wordN.ReplaceAllLiteralString(published, strconv.Itoa(length))
	filled = wordM.ReplaceAllLiteralString(filled, strconv.Itoa(limit))
	filled = strings.ReplaceAll(filled, "<socket>", socket)
	if strings.Contains(filled, "<socket>") {
		t.Fatalf("the published line %q carries no <socket> placeholder", published)
	}
	return filled
}

// TestGraphSocketTooLong_MatchesTheSpecificationCharacterForCharacter compares
// the line this package produces against the one SPEC/COMMANDS.md publishes.
//
// It is the same technique the four older socket lines are pinned by, and for the
// same reason: a test carrying its own copy of a published line asserts that the
// code agrees with the test.
func TestGraphSocketTooLong_MatchesTheSpecificationCharacterForCharacter(t *testing.T) {
	socket := "/home/user/" + strings.Repeat("d", graphclient.MaxSocketPathLen) + "/graph.sock"

	want := fillTooLongPlaceholders(t, publishedSocketLine(t, tooLongMarker), socket,
		len(socket), graphclient.MaxSocketPathLen)

	err := graphSocketTooLong(socket)
	if !errors.Is(err, utils.ErrGraphServer) {
		t.Errorf("error = %v, want it to wrap utils.ErrGraphServer, which is exit code 1. The "+
			"sentinel and the code are unchanged by this refusal; only the message differs "+
			"(SPEC/GRAPH.md § Socket Path Length, rule 7)", err)
	}
	if got := errorLine(err); got != want {
		t.Errorf("line = %q,\n want %q", got, want)
	}
}

// TestGraphSocketTooLong_NamesTwoDistinctNumbersAndNoErrno is the assertion
// SPEC/GRAPH.md Acceptance Criterion 65 spells out, at the level of the message
// this package builds.
//
// The two numbers are asserted SEPARATELY. A message that printed the limit in
// both positions — or the path's length in both — would satisfy a check that
// merely found two numbers in the line, and would tell the reader nothing about
// how far over the bound the path actually is.
func TestGraphSocketTooLong_NamesTwoDistinctNumbersAndNoErrno(t *testing.T) {
	// A path comfortably over the bound, so the two numbers cannot coincide.
	socket := "/srv/" + strings.Repeat("p", graphclient.MaxSocketPathLen+40) + ".sock"
	line := errorLine(graphSocketTooLong(socket))

	if !strings.Contains(line, strconv.Itoa(len(socket))+" bytes") {
		t.Errorf("the line does not report the path's own length (%d bytes): %q", len(socket), line)
	}
	if !strings.Contains(line, "at most "+strconv.Itoa(graphclient.MaxSocketPathLen)) {
		t.Errorf("the line does not report the platform's limit (%d): %q", graphclient.MaxSocketPathLen, line)
	}
	if len(socket) == graphclient.MaxSocketPathLen {
		t.Fatal("the fixture's length coincides with the limit, so the two assertions above cannot " +
			"tell a message that prints one number twice from one that prints both")
	}
	if !strings.Contains(line, socket) {
		t.Errorf("the line does not name the resolved path: %q", line)
	}
	if strings.Contains(line, "invalid argument") {
		t.Errorf("the line carries the operating system's own text, which is the defect rmp task "+
			"#412 exists against: %q", line)
	}
	if !strings.Contains(line, "--socket") {
		t.Errorf("the line names no remedy; SPEC/GRAPH.md § Socket Path Length, rule 4, requires it "+
			"to name --socket as the way to put the socket somewhere shorter: %q", line)
	}
}

// TestRefuseOverLongSocket_IsABoundaryAndNotAThreshold pins the check all three
// subcommands call to the bound in both directions.
//
// The at-the-bound half is what stops an implementation that is one byte too
// strict, and such an implementation refuses a path the kernel accepts on every
// platform at once (SPEC/GRAPH.md Acceptance Criterion 64). It matters more under
// the uniform rule than it did under the split: this one function now decides the
// outcome of every command-line surface, so a bound one byte out withdraws all
// three at once rather than the two that need a socket.
func TestRefuseOverLongSocket_IsABoundaryAndNotAThreshold(t *testing.T) {
	atBound := "/" + strings.Repeat("a", graphclient.MaxSocketPathLen-1)
	if len(atBound) != graphclient.MaxSocketPathLen {
		t.Fatalf("fixture is %d bytes, want %d", len(atBound), graphclient.MaxSocketPathLen)
	}
	if err := refuseOverLongSocket(atBound); err != nil {
		t.Errorf("a path of exactly the bound (%d bytes) was refused: %v", len(atBound), err)
	}

	overBound := atBound + "a"
	err := refuseOverLongSocket(overBound)
	if err == nil {
		t.Fatalf("a path one byte over the bound (%d bytes) was accepted", len(overBound))
	}
	if !errors.Is(err, utils.ErrGraphServer) {
		t.Errorf("error = %v, want utils.ErrGraphServer", err)
	}
	if !strings.Contains(err.Error(), tooLongMarker) {
		t.Errorf("error = %v, want the published path-length line", err)
	}
}

// deepGraphHome returns a HOME so deep that the socket derived for roadmapName
// under it is longer than the platform allows a socket path to be.
//
// It is built from nested directories rather than from one enormous component
// because a single filename is itself capped (NAME_MAX), and the shape it
// produces is the one the bound is actually met in: an ordinary roadmap name
// under a workspace several directories down. The loop measures the DERIVED PATH
// rather than the home directory, so the fixture is over the bound by
// construction on a short $TMPDIR and on a long one alike; every caller asserts
// it again before relying on it.
func deepGraphHome(t *testing.T, roadmapName string) string {
	t.Helper()

	root, err := os.MkdirTemp("", "rmpc")
	if err != nil {
		t.Fatalf("creating a temporary root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) }) //nolint:errcheck // a temporary directory the test is done with

	const segment = "deeply-nested-agent-workspace"
	derivedUnder := func(home string) string {
		return filepath.Join(home, ".roadmaps", roadmapName, graphclient.SocketFileName)
	}
	home := root
	for len(derivedUnder(home)) <= graphclient.MaxSocketPathLen {
		home = filepath.Join(home, segment)
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatalf("creating a deep HOME: %v", err)
	}
	return home
}

// shortSocketDir returns a directory short enough to build a lawful socket path
// in, whatever the test's name is.
//
// t.TempDir names its directory after the test, so a descriptive name pushes an
// ordinary socket path past the bound and the control below would be exercising
// the refusal it exists to contrast with.
func shortSocketDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "rmps")
	if err != nil {
		t.Fatalf("creating a short socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // a temporary directory the test is done with
	return dir
}

// assertTooLongRefusal asserts one invocation was refused by the path-length rule
// and wrote nothing to stdout.
//
// The absence of the UNREACHABLE line is asserted alongside the presence of the
// path-length one, and it is the half that pins the ORDER: a length settled after
// the probe would report an ordinary file sitting at the path as a server that
// could not be reached, which is a different published line for a different
// condition.
func assertTooLongRefusal(t *testing.T, err error, stdout, socket string) {
	t.Helper()

	if err == nil {
		t.Fatalf("the invocation succeeded against the socket path %q, which is %d bytes and over "+
			"the platform's bound of %d. No surface falls back to the store on this condition "+
			"(SPEC/GRAPH.md § Socket Path Length, rules 5 and 6)",
			socket, len(socket), graphclient.MaxSocketPathLen)
	}
	if !errors.Is(err, utils.ErrGraphServer) {
		t.Errorf("error = %v, want utils.ErrGraphServer, which is exit code 1; the sentinel and the "+
			"code are unchanged by this refusal (rule 7)", err)
	}
	line := errorLine(err)
	if !strings.Contains(line, tooLongMarker) {
		t.Errorf("line = %q, want the published path-length line", line)
	}
	if !strings.Contains(line, socket) {
		t.Errorf("line = %q, want it to name the resolved path %q", line, socket)
	}
	if strings.Contains(line, "unreachable") {
		t.Errorf("line = %q: the invocation reported the socket as unreachable, so the path's "+
			"length was settled AFTER the probe rather than before it "+
			"(SPEC/GRAPH.md § Server Resolution, rule 12)", line)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("a refused invocation wrote %q to stdout; it must write nothing there "+
			"(SPEC/COMMANDS.md § Failing Invocations Write Nothing to Stdout)", stdout)
	}
}

// TestGraphClient_RefusesAnOverLongResolvedSocketHoweverThePathWasChosen is
// SPEC/GRAPH.md § Socket Path Length, rules 5 and 6, on the client.
//
// Where the path came from is never asked: a supplied path over the bound and a
// derived one are refused alike, with the same line and the same exit code. The
// third subtest is what makes that a statement about the ORDER of the checks
// rather than about their agreement — an ordinary file sits at the derived path,
// which a probe would read as a server that could not be reached, and the
// published line must still be the one about the path's length.
//
// The last subtest is the control, and without it the three above would also pass
// on an implementation that had simply stopped reaching a server — which would
// break every roadmap rather than the ones whose resolved path cannot be bound.
// It is also the remedy the published line names, driven end to end: --socket
// pointed at a server listening on a lawful path reaches that server, from a
// roadmap whose own derived path never can.
func TestGraphClient_RefusesAnOverLongResolvedSocketHoweverThePathWasChosen(t *testing.T) {
	const read = "MATCH (s:Spec) RETURN s.key"

	t.Run("a supplied path over the bound", func(t *testing.T) {
		const roadmap = "graph-execute-socket-supplied"
		defer setupTestGraphRoadmap(t, roadmap)()

		over := "/" + strings.Repeat("a", graphclient.MaxSocketPathLen)
		if !graphclient.SocketPathTooLong(over) {
			t.Fatalf("the fixture is %d bytes and the bound is %d", len(over), graphclient.MaxSocketPathLen)
		}

		var runErr error
		stdout, _ := captureStdStreams(t, func() {
			runErr = runGraphClient([]string{"-r", roadmap, "--socket", over, "--query", read})
		})
		assertTooLongRefusal(t, runErr, stdout, over)
	})

	t.Run("the derived path over the bound", func(t *testing.T) {
		// A three-character roadmap name, which is the shape the bound was first
		// met in: the derived path crosses it on an ordinary installation rather
		// than on an unusual roadmap name (SPEC/GRAPH.md § Socket Path Length,
		// rule 8).
		const roadmap = "ctr"
		t.Setenv("HOME", deepGraphHome(t, roadmap))
		defer setupTestGraphRoadmap(t, roadmap)()

		derived, err := graphclient.SocketPath(roadmap)
		if err != nil {
			t.Fatalf("deriving the socket path: %v", err)
		}
		if !graphclient.SocketPathTooLong(derived) {
			t.Fatalf("the derived path is %d bytes and the bound is %d, so this subtest is "+
				"exercising the ordinary case rather than the one it exists for: %s",
				len(derived), graphclient.MaxSocketPathLen, derived)
		}

		// NO --socket flag at all: this is the provenance the uniform rule
		// changed, and the invocation must be written without the flag or it
		// asserts the case that already failed.
		var runErr error
		stdout, _ := captureStdStreams(t, func() {
			runErr = runGraphClient([]string{"-r", roadmap, "--query", read})
		})
		assertTooLongRefusal(t, runErr, stdout, derived)
	})

	t.Run("the derived path over the bound with a file at it", func(t *testing.T) {
		// The proof that the settlement precedes the probe, and not merely that
		// it agrees with one. An ordinary FILE sits at the derived path: a probe
		// would read it as StateUnreachable and publish the unreachable line,
		// which names a server that could not be reached rather than a path no
		// server can ever occupy (SPEC/GRAPH.md § Server Resolution, rule 12).
		const roadmap = "ctf"
		t.Setenv("HOME", deepGraphHome(t, roadmap))
		defer setupTestGraphRoadmap(t, roadmap)()

		derived, err := graphclient.SocketPath(roadmap)
		if err != nil {
			t.Fatalf("deriving the socket path: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(derived), 0700); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(derived), err)
		}
		if err := os.WriteFile(derived, []byte("not a socket"), 0600); err != nil {
			t.Fatalf("writing %s: %v", derived, err)
		}

		var runErr error
		stdout, _ := captureStdStreams(t, func() {
			runErr = runGraphClient([]string{"-r", roadmap, "--query", read})
		})
		assertTooLongRefusal(t, runErr, stdout, derived)
	})

	t.Run("a supplied path inside the bound still reaches a server", func(t *testing.T) {
		// Acceptance Criterion 66's recovery half, at unit level, and the control
		// for the three refusals above. The roadmap's DERIVED path is unusable, so
		// no server can be started for it in the ordinary way and no client can
		// reach one; --socket names a path that is lawful, a server is started on
		// it, and the same roadmap is then read and written through it. That is
		// the remedy the published line names, and the reason the refusal above is
		// recoverable on the command line without moving the roadmap
		// (SPEC/GRAPH.md § Socket Path Length, rule 6).
		//
		// Without this subtest the three above would pass against a client that
		// had simply stopped reaching any server at all.
		const roadmap = "ctc"
		t.Setenv("HOME", deepGraphHome(t, roadmap))
		defer setupTestGraphRoadmap(t, roadmap)()

		derived, err := graphclient.SocketPath(roadmap)
		if err != nil {
			t.Fatalf("deriving the socket path: %v", err)
		}
		if !graphclient.SocketPathTooLong(derived) {
			t.Fatalf("the control's derived path is %d bytes and the bound is %d; it is not the "+
				"state this subtest is the control for", len(derived), graphclient.MaxSocketPathLen)
		}

		lawful := filepath.Join(shortSocketDir(t), "graph.sock")
		if graphclient.SocketPathTooLong(lawful) {
			t.Fatalf("the control's own socket path is %d bytes and the bound is %d; $TMPDIR is "+
				"too long for this fixture: %s", len(lawful), graphclient.MaxSocketPathLen, lawful)
		}

		// The server binds the lawful path and serves the roadmap whose own
		// derived path cannot be bound. Its graph directory is the roadmap's, so
		// what is read back below is that roadmap's graph and not another.
		defer serveGraphOn(t, roadmap, graphDirOf(t, roadmap), lawful)()

		captureStdStreams(t, func() {
			if writeErr := runGraphClient([]string{"-r", roadmap, "--socket", lawful, "--query",
				"CREATE (:Spec {key:'reached-through-a-lawful-socket'})"}); writeErr != nil {
				t.Fatalf("a lawful socket path with a server behind it must carry the statement to "+
					"it, whatever the roadmap's derived path is: %v", writeErr)
			}
		})

		var runErr error
		stdout, _ := captureStdStreams(t, func() {
			runErr = runGraphClient([]string{"-r", roadmap, "--socket", lawful, "--query", read})
		})
		if runErr != nil {
			t.Fatalf("reading back through the lawful socket path failed: %v", runErr)
		}
		if !strings.Contains(stdout, "reached-through-a-lawful-socket") {
			t.Errorf("stdout = %q, want the row the write committed; the invocation exited without "+
				"error but the write never reached the server", stdout)
		}
	})
}
