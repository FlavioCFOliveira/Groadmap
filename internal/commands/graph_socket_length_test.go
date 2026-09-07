// Package commands — the tests for the socket-path-length refusal (rmp task #412).
//
// The defect this file exists against printed the operating system's own
// `bind: invalid argument`, which named the path twice and the cause not at all.
// What replaces it is a published line carrying a length, a limit and a remedy,
// and a rule that splits on WHO CHOSE THE PATH. Both are pinned here: the line
// against SPEC/COMMANDS.md rather than against a copy written out in this file,
// and the split against the three outcomes SPEC/GRAPH.md Acceptance Criterion 66
// requires.
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

// TestRefuseOverLongSocket_IsABoundaryAndNotAThreshold pins the two subcommands
// with no second path to the bound in both directions.
//
// The at-the-bound half is what stops an implementation that is one byte too
// strict, and such an implementation refuses a path the kernel accepts on every
// platform at once (SPEC/GRAPH.md Acceptance Criterion 64).
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

// TestServedOnResolvedSocket_SplitsOnWhoChoseThePath is the whole of
// SPEC/GRAPH.md § Socket Path Length, rules 5 and 6, for the ONE command-line
// surface that has a second path.
//
// The value of the rule is entirely in the split. An implementation that refused
// both cases would pass a check of the supplied half while withdrawing a working
// surface from every roadmap whose home directory is deep; an implementation that
// fell back on both would make --socket mean nothing in exactly the case the
// product can tell that it does.
func TestServedOnResolvedSocket_SplitsOnWhoChoseThePath(t *testing.T) {
	overBound := "/" + strings.Repeat("a", graphclient.MaxSocketPathLen)
	if !graphclient.SocketPathTooLong(overBound) {
		t.Fatalf("fixture of %d bytes is not over the bound of %d", len(overBound), graphclient.MaxSocketPathLen)
	}

	t.Run("a supplied path over the bound fails and does not fall back", func(t *testing.T) {
		served, err := servedOnResolvedSocket(overBound, overBound)
		if err == nil {
			t.Fatalf("served = %v with no error; the caller named a socket no process can create, "+
				"and silently ignoring the flag is how a flag comes to mean nothing", served)
		}
		if !errors.Is(err, utils.ErrGraphServer) {
			t.Errorf("error = %v, want utils.ErrGraphServer (exit code 1)", err)
		}
		if !strings.Contains(err.Error(), tooLongMarker) {
			t.Errorf("error = %v, want the published path-length line", err)
		}
		if served {
			t.Error("a refused invocation reported the roadmap as served")
		}
	})

	t.Run("a derived path over the bound is not served and is not an error", func(t *testing.T) {
		served, err := servedOnResolvedSocket(overBound, "")
		if err != nil {
			t.Fatalf("a DERIVED path over the bound produced %v. It is the strongest of the definite "+
				"negatives -- no server can exist there -- so the statement runs against the store, "+
				"exactly as it does for a socket that is absent (SPEC/GRAPH.md § Socket Path Length, "+
				"rule 6)", err)
		}
		if served {
			t.Error("served = true for a path no process on this platform can bind")
		}
	})

	t.Run("a derived path over the bound is settled before the probe", func(t *testing.T) {
		// The proof that the settlement precedes the probe, and not merely that
		// it agrees with one: an ordinary FILE sits at the path. A probe would
		// read that as StateUnreachable and FAIL the invocation -- which is the
		// right answer for a path a socket could lawfully occupy, and the wrong
		// one here, because no socket can occupy this path at all
		// (SPEC/GRAPH.md § Server Resolution, rule 12).
		dir := t.TempDir()
		name := strings.Repeat("f", graphclient.MaxSocketPathLen+8-len(dir)-1)
		path := filepath.Join(dir, name)
		if len(path) <= graphclient.MaxSocketPathLen {
			t.Fatalf("the temporary directory %q is too long for this fixture: the path is %d bytes "+
				"and the bound is %d", dir, len(path), graphclient.MaxSocketPathLen)
		}
		if err := os.WriteFile(path, []byte("not a socket"), 0600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}

		served, err := servedOnResolvedSocket(path, "")
		if err != nil {
			t.Fatalf("a derived path over the bound with a FILE at it produced %v; the length was "+
				"settled after the probe rather than before it", err)
		}
		if served {
			t.Error("served = true for a file")
		}
	})
}
