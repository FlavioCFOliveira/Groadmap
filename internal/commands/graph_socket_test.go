// Package commands — the socket half's tests.
//
// # What is pinned here, and where each line comes from
//
// SPEC/COMMANDS.md § Graph Server Socket Error Lines publishes seven complete
// lines. Four of them are produced by this package — the no-server line, the
// unreachable line, the lost-connection line and the unanswered line — and this
// file compares each against the SPECIFICATION rather than against a copy written
// out here, so a line that drifts in either place fails.
//
// The comparison is possible because the specification writes each line in
// backticks, complete, with `<socket>` and `<detail>` declared as placeholders.
// Substituting a real socket path and a real detail is the same technique
// positional_refusal_families_test.go uses for the arity refusals, and for the
// same reason: a test that carried its own copy of a published line would be
// asserting that the code agrees with the test.
package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/backoff"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphlock"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// socketErrorLinesHeading bounds the section this file reads.
const socketErrorLinesHeading = "### Graph Server Socket Error Lines"

// publishedSocketErrorLines extracts every complete error line the section
// publishes, in the order it publishes them.
//
// A line is recognised by the one thing every published line has in common: it
// begins with the error-line prefix SPEC/HELP.md § Error line fixes. Reading them
// that way rather than by position means a line added to the section is picked up
// rather than silently skipped, which is what the floor below then checks.
func publishedSocketErrorLines(t *testing.T) []string {
	t.Helper()

	lines := readSpecSection(t, commandsSpecPath, socketErrorLinesHeading)
	// A published line may be wrapped across two source lines inside its
	// backticks, so the section is joined before the backticked spans are read.
	joined := strings.Join(lines, " ")
	backticked := regexp.MustCompile("`([^`]*)`")

	var published []string
	for _, match := range backticked.FindAllStringSubmatch(joined, -1) {
		candidate := strings.Join(strings.Fields(match[1]), " ")
		if strings.HasPrefix(candidate, specErrorLinePrefix) {
			published = append(published, candidate)
		}
	}
	if len(published) < 4 {
		t.Fatalf("%s § Graph Server Socket Error Lines yields %d complete line(s); this file "+
			"compares four of them and cannot do so against a section it can no longer read",
			commandsSpecPath, len(published))
	}
	return published
}

// publishedSocketLine returns the one published line containing marker, failing
// when no line or more than one carries it.
//
// Selecting by a distinguishing fragment rather than by index is what keeps the
// selection honest when the section is reordered, and the ambiguity check is what
// stops a fragment that matches two lines from silently pinning the wrong one.
func publishedSocketLine(t *testing.T, marker string) string {
	t.Helper()

	var found []string
	for _, line := range publishedSocketErrorLines(t) {
		if strings.Contains(line, marker) {
			found = append(found, line)
		}
	}
	switch len(found) {
	case 1:
		return found[0]
	case 0:
		t.Fatalf("no published socket error line contains %q", marker)
	default:
		t.Fatalf("%d published socket error lines contain %q, so the fragment does not identify "+
			"one: %v", len(found), marker, found)
	}
	return ""
}

// fillSocketPlaceholders substitutes the two placeholders the section declares.
// A placeholder left in the expected line would make the comparison below pass
// against a message that never interpolated anything.
func fillSocketPlaceholders(t *testing.T, published, socket, detail string) string {
	t.Helper()

	if !strings.Contains(published, "<socket>") {
		t.Fatalf("the published line %q carries no <socket> placeholder", published)
	}
	filled := strings.ReplaceAll(published, "<socket>", socket)
	filled = strings.ReplaceAll(filled, "<detail>", detail)
	if strings.Contains(filled, "<") && strings.Contains(filled, ">") {
		t.Fatalf("the published line %q still carries a placeholder after substitution: %q",
			published, filled)
	}
	return filled
}

// TestGraphSocketLines_MatchTheSpecificationCharacterForCharacter compares each
// line this package produces against the one the specification publishes.
//
// Every one carries utils.ErrDatabase and therefore exit code 1
// (SPEC/GRAPH.md § Error Handling and Exit Codes), and that is asserted alongside
// the text: the wording and the exit code are two halves of one contract and a
// test that checked only the first would let the second drift.
func TestGraphSocketLines_MatchTheSpecificationCharacterForCharacter(t *testing.T) {
	const socket = "/home/user/.roadmaps/backend-platform/graph.sock"

	cases := []struct {
		name    string
		marker  string
		detail  string
		produce func() error
	}{
		{
			name:    "no server listening",
			marker:  "no graph server is listening",
			produce: func() error { return graphNoServerListening(socket) },
		},
		{
			name:   "unreachable",
			marker: "graph server unreachable at",
			detail: "read unix @->" + socket + ": i/o timeout",
			produce: func() error {
				return graphSocketUnreachable(socket, errors.New("read unix @->"+socket+": i/o timeout"))
			}, //nolint:err113 // a fixture standing in for the operating system's own diagnostic
		},
		{
			name:    "the connection was lost",
			marker:  "was lost; the statement's outcome is unknown",
			produce: func() error { return graphConnectionLost(socket) },
		},
		{
			name:    "the server did not answer",
			marker:  "did not answer within",
			produce: func() error { return graphServerSilent(socket) },
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := fillSocketPlaceholders(t, publishedSocketLine(t, c.marker), socket, c.detail)

			err := c.produce()
			if !errors.Is(err, utils.ErrDatabase) {
				t.Errorf("error = %v, want it to wrap utils.ErrDatabase (exit code 1)", err)
			}
			if got := errorLine(err); got != want {
				t.Errorf("line = %q,\n want %q", got, want)
			}
		})
	}
}

// TestGraphSocket_TheBackstopLiteralMatchesTheDeclaredBudget is what keeps the
// one literal in graph_socket.go honest.
//
// SPEC/COMMANDS.md § Graph Server Socket Error Lines requires "7.5s" to be a
// FIXED value and not one the binary interpolates, so that the line is the same
// characters whatever a test has done to the budget and can be compared in full.
// A fixed value is a fact that can go stale, and this is the assertion that stops
// it: the backstop is the statement budget plus the backoff total, and rendering
// the DEFAULT declaration must produce exactly the literal the line carries.
func TestGraphSocket_TheBackstopLiteralMatchesTheDeclaredBudget(t *testing.T) {
	declared := graphlock.DefaultStatementBudget + backoff.Total()
	if got := declared.String(); got != backstopLine {
		t.Errorf("the published unanswered line says the server did not answer within %q, and the "+
			"declared backstop renders as %q. SPEC/COMMANDS.md fixes the line's value as a literal, "+
			"so a change to the statement budget or to the retry policy has to move the literal with "+
			"it", backstopLine, got)
	}
}

// TestExtractSocketFlag_ConsumesTheFlagAndLeavesEverythingElse pins the
// difference between this function and readSocketFlag.
//
// `graph serve` takes no other flag and refuses whatever is left where it stands.
// `graph execute` and `graph client` take --query as well and read a statement
// out of what remains, so the socket flag has to be removed from the arguments
// WITHOUT an opinion about the rest — and the leftovers must arrive at readQuery
// in their original order, or a query would be read from the wrong token.
func TestExtractSocketFlag_ConsumesTheFlagAndLeavesEverythingElse(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantValue string
		wantRest  []string
	}{
		{
			name:     "absent",
			args:     []string{"--query", "MATCH (n) RETURN n"},
			wantRest: []string{"--query", "MATCH (n) RETURN n"},
		},
		{
			name:      "before the query",
			args:      []string{"--socket", "/run/user/1000/graph.sock", "--query", "MATCH (n) RETURN n"},
			wantValue: "/run/user/1000/graph.sock",
			wantRest:  []string{"--query", "MATCH (n) RETURN n"},
		},
		{
			name:      "after the query",
			args:      []string{"--query", "MATCH (n) RETURN n", "--socket", "/run/user/1000/graph.sock"},
			wantValue: "/run/user/1000/graph.sock",
			wantRest:  []string{"--query", "MATCH (n) RETURN n"},
		},
		{
			name:      "a stray token is left for the reader that refuses it",
			args:      []string{"--socket", "/tmp/g.sock", "reconciliation-report"},
			wantValue: "/tmp/g.sock",
			wantRest:  []string{"reconciliation-report"},
		},
		{
			name:      "an unknown flag is left for the reader that refuses it",
			args:      []string{"--socket", "/tmp/g.sock", "--include-archived"},
			wantValue: "/tmp/g.sock",
			wantRest:  []string{"--include-archived"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			value, rest, err := extractSocketFlag(c.args)
			if err != nil {
				t.Fatalf("extractSocketFlag(%v) = %v", c.args, err)
			}
			if value != c.wantValue {
				t.Errorf("value = %q, want %q", value, c.wantValue)
			}
			if len(rest) != len(c.wantRest) {
				t.Fatalf("rest = %v, want %v", rest, c.wantRest)
			}
			for i := range rest {
				if rest[i] != c.wantRest[i] {
					t.Fatalf("rest = %v, want %v (order matters: readQuery reads its value from the "+
						"token that follows --query)", rest, c.wantRest)
				}
			}
		})
	}
}

// TestExtractSocketFlag_AnEmptyValueIsAMissingParameter pins the classification.
//
// A flag supplied with an empty — or whitespace-only — value names no socket at
// all, which is the same condition as writing the flag with nothing after it. It
// is a MISSING parameter (exit code 2 through utils.ErrRequired) and not a
// validation failure, which is what SPEC/COMMANDS.md § Execute Exit Codes and
// § Client Exit Codes both publish for it.
func TestExtractSocketFlag_AnEmptyValueIsAMissingParameter(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"nothing after the flag", []string{"--socket"}},
		{"a flag after the flag", []string{"--socket", "--query", "MATCH (n) RETURN n"}},
		{"an empty value", []string{"--socket", ""}},
		{"a whitespace-only value", []string{"--socket", "   "}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := extractSocketFlag(c.args)
			if err == nil {
				t.Fatalf("extractSocketFlag(%v) reported no error", c.args)
			}
			if !errors.Is(err, utils.ErrRequired) {
				t.Errorf("error = %v, want it to wrap utils.ErrRequired (exit code 2). Supplying a "+
					"flag with no value is a missing parameter, not a validation failure", err)
			}
			if errors.Is(err, utils.ErrValidation) {
				t.Errorf("error = %v, but the two classes carry different exit codes and "+
					"SPEC/COMMANDS.md publishes exit 2 for this one", err)
			}
		})
	}
}

// TestExtractSocketFlag_DoesNotTrimThePathItKeeps pins a small decision with a
// real consequence: a path is not text a user reads back, and a path may
// legitimately end in a space, so the value is checked for emptiness after a trim
// and then kept as supplied.
func TestExtractSocketFlag_DoesNotTrimThePathItKeeps(t *testing.T) {
	const path = "/run/user/1000/graph sock "

	value, _, err := extractSocketFlag([]string{"--socket", path})
	if err != nil {
		t.Fatalf("extractSocketFlag: %v", err)
	}
	if value != path {
		t.Errorf("value = %q, want %q unchanged: a path may legitimately end in a space", value, path)
	}
}

// TestGraphSocketInForce_DerivesTheDefaultAndAbsolutisesTheFlag pins the
// derivation all three subcommands share.
//
// It matters that they share it: a caller and a server that name the same roadmap
// must name the same socket without either being told a path
// (SPEC/GRAPH.md § Socket Path and Permissions, rule 1).
func TestGraphSocketInForce_DerivesTheDefaultAndAbsolutisesTheFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("no flag derives the roadmap's path", func(t *testing.T) {
		got, err := graphSocketInForce("backend-platform", "")
		if err != nil {
			t.Fatalf("graphSocketInForce: %v", err)
		}
		want := filepath.Join(home, ".roadmaps", "backend-platform", graphclient.SocketFileName)
		if got != want {
			t.Errorf("socket = %q, want %q", got, want)
		}

		// The same derivation internal/graphclient publishes, reached through the
		// one function, so the CLI cannot come to derive a path of its own.
		derived, err := graphclient.SocketPath("backend-platform")
		if err != nil {
			t.Fatalf("graphclient.SocketPath: %v", err)
		}
		if got != derived {
			t.Errorf("the CLI derives %q where the shared resolver derives %q", got, derived)
		}
	})

	t.Run("a relative flag is made absolute", func(t *testing.T) {
		got, err := graphSocketInForce("backend-platform", "relative/graph.sock")
		if err != nil {
			t.Fatalf("graphSocketInForce: %v", err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("socket = %q, want an absolute path: the value is PUBLISHED — by the startup "+
				"object and by every error line that names the socket — and a relative path would "+
				"be meaningless to a reader who does not know the working directory", got)
		}
	})

	t.Run("an invalid roadmap name is refused", func(t *testing.T) {
		if got, err := graphSocketInForce("../escape", ""); err == nil {
			t.Errorf("graphSocketInForce resolved %q for a crafted roadmap name; the validation is "+
				"what keeps a name from resolving a path outside ~/.roadmaps/", got)
		}
	})
}

// TestResolveGraphServer_ReportsTheStatesTheSurfacesActOn drives the resolver
// through the CLI's wrapper, which is where the ONE failing state becomes a
// published line and the other three are left to the caller.
//
// The two negatives must arrive as states and NOT as errors, because for
// `graph execute` they are the direct path: a wrapper that reported them as
// failures would turn an unserved roadmap into a broken one.
func TestResolveGraphServer_ReportsTheStatesTheSurfacesActOn(t *testing.T) {
	dir := t.TempDir()

	t.Run("an absent socket is a state and not an error", func(t *testing.T) {
		state, err := resolveGraphServer(filepath.Join(dir, "absent.sock"))
		if err != nil {
			t.Fatalf("resolveGraphServer reported %v for an absent socket; for `graph execute` "+
				"that state is the direct path, not a failure", err)
		}
		if !state.NotServed() {
			t.Errorf("state = %v, want one of the two definite negatives", state)
		}
	})

	t.Run("a path that is not a socket is the published unreachable line", func(t *testing.T) {
		path := filepath.Join(dir, "notes.txt")
		if err := os.WriteFile(path, []byte("a file the caller mistyped --socket onto"), 0600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}

		state, err := resolveGraphServer(path)
		if err == nil {
			t.Fatal("a path that is not a socket resolved without error; falling back on it would " +
				"send the caller at a lock a server may well be holding")
		}
		if state.NotServed() {
			t.Errorf("state = %v; an unreachable path must not put the caller on the direct path", state)
		}
		if !errors.Is(err, utils.ErrDatabase) {
			t.Errorf("error = %v, want it to wrap utils.ErrDatabase (exit code 1)", err)
		}
		if !strings.Contains(err.Error(), "graph server unreachable at "+path) {
			t.Errorf("error = %q, want the published unreachable line naming %q", err.Error(), path)
		}
	})
}

// TestGraphServerFailure_MapsEveryClientFailureOntoAPublishedLine is the
// exhaustive half: every failure the shared client can classify must reach a
// line, and the two that are the STATEMENT's must reach the SAME lines the direct
// path produces.
//
// The last point is the one worth a test. A statement that failed to parse and a
// statement the budget cut read identically whether they ran in this process or
// in a server, and SPEC/COMMANDS.md publishes one line for each across both
// subcommands — so the served path must not word them a second way.
func TestGraphServerFailure_MapsEveryClientFailureOntoAPublishedLine(t *testing.T) {
	const socket = "/home/user/.roadmaps/backend-platform/graph.sock"
	const diagnostic = `cypher: parse: unexpected "RETURN" at 1:9`

	budget := graphlock.StatementBudget

	cases := []struct {
		name string
		err  *graphclient.SendError
		want string
	}{
		{
			name: "a statement the engine refused carries the direct path's own line",
			err:  &graphclient.SendError{Kind: graphclient.FailureStatement, Socket: socket, Diagnostic: diagnostic},
			want: errorLine(graphStatementError(budget, "graph query failed", errors.New(diagnostic))), //nolint:err113 // a fixture standing in for the engine's own diagnostic
		},
		{
			name: "a statement the budget cut carries the direct path's own line",
			err:  &graphclient.SendError{Kind: graphclient.FailureBudget, Socket: socket, Diagnostic: "context deadline exceeded"},
			want: errorLine(graphStatementError(budget, "graph query failed", budgetSelector())),
		},
		{
			name: "a lost connection",
			err:  &graphclient.SendError{Kind: graphclient.FailureLost, Socket: socket},
			want: errorLine(graphConnectionLost(socket)),
		},
		{
			name: "an unanswered server",
			err:  &graphclient.SendError{Kind: graphclient.FailureUnanswered, Socket: socket},
			want: errorLine(graphServerSilent(socket)),
		},
		{
			name: "an exhausted serialisation retry carries a line of its own",
			err: &graphclient.SendError{
				Kind: graphclient.FailureConflict, Socket: socket,
				Code:       "Neo.TransientError.Transaction.Outdated",
				Diagnostic: "mvcc: serialization conflict in node properties",
			},
			want: errorLine(graphWriteConflict()),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := graphServerFailure(socket, c.err)
			if !errors.Is(got, utils.ErrDatabase) {
				t.Errorf("error = %v, want it to wrap utils.ErrDatabase (exit code 1)", got)
			}
			if line := errorLine(got); line != c.want {
				t.Errorf("line = %q,\n want %q", line, c.want)
			}
		})
	}

	t.Run("an unreachable server", func(t *testing.T) {
		cause := errors.New("read unix: i/o timeout") //nolint:err113 // a fixture standing in for the operating system's own diagnostic
		got := graphServerFailure(socket, &graphclient.SendError{
			Kind: graphclient.FailureUnreachable, Socket: socket, Cause: cause,
		})
		if line, want := errorLine(got), errorLine(graphSocketUnreachable(socket, cause)); line != want {
			t.Errorf("line = %q,\n want %q", line, want)
		}
	})

	t.Run("a value that could not be mapped", func(t *testing.T) {
		got := graphServerFailure(socket, &graphclient.SendError{
			Kind: graphclient.FailureMapping, Socket: socket, Diagnostic: "unsupported protocol structure tag 0x7A",
		})
		if !errors.Is(got, utils.ErrDatabase) {
			t.Errorf("error = %v, want it to wrap utils.ErrDatabase (exit code 1): a value the "+
				"mapping cannot represent fails the statement (SPEC/DATA_FORMATS.md § Graph Client "+
				"Result, rule 3)", got)
		}
		if !strings.Contains(got.Error(), "graph query failed: ") {
			t.Errorf("error = %q, want it to carry the published prefix a reader matching the "+
				"parse/execution row already matches", got.Error())
		}
		if !strings.Contains(got.Error(), "0x7A") {
			t.Errorf("error = %q, want it to name the value it could not map", got.Error())
		}
	})
}

// TestGraphWriteConflict_MatchesThePublishedLineAndIsNotTheStatementLine pins
// the contention line character for character against SPEC/COMMANDS.md, and pins
// the ONE thing it was published to establish: that a caller reading it is not
// reading the line an invalid statement produces.
//
// The second half is not decoration. Before this line existed, an exhausted
// retry printed exactly the parse/execution line, and the only text separating
// the two was the engine's diagnostic tail that SPEC/COMMANDS.md deliberately
// declines to specify — so a caller could not lawfully tell "run it again" from
// "correct it" (SPEC/GRAPH.md § Concurrency Inside the Server, rule 9). A future
// edit that folded the conflict back onto graphStatementError would restore that
// defect while still printing a line, and only an assertion about what the line
// is NOT would notice.
func TestGraphWriteConflict_MatchesThePublishedLineAndIsNotTheStatementLine(t *testing.T) {
	const published = "Error: database error: graph write conflict: another writer committed first " +
		"on every attempt within the 2.5s retry budget; nothing was written. The statement is " +
		"valid — run it again, and spread concurrent writes across distinct nodes."

	err := graphWriteConflict()

	if got := errorLine(err); got != published {
		t.Errorf("the contention line does not match SPEC/COMMANDS.md § Client Error Cases "+
			"character for character:\n got %q\nwant %q", got, published)
	}
	if !errors.Is(err, utils.ErrDatabase) {
		t.Errorf("error = %v, want it to wrap utils.ErrDatabase (exit code 1): the graph feature "+
			"introduces no new sentinel and no new exit code "+
			"(SPEC/GRAPH.md § Error Handling and Exit Codes, rule 7)", err)
	}
	if strings.Contains(err.Error(), "graph query failed: ") {
		t.Error("the contention line carries the parse/execution prefix. That prefix is what an " +
			"invalid statement prints, and the whole purpose of this line is that the two are " +
			"distinguishable by text the contract fixes")
	}

	// The budget it names is the retry policy's own, read rather than written
	// out, so a change to the policy cannot leave the line claiming a budget
	// nothing spends. 2.5s is what the policy renders today and what
	// SPEC/COMMANDS.md publishes.
	if got, want := backoff.Total().String(), "2.5s"; got != want {
		t.Errorf("backoff.Total() renders %q, but SPEC/COMMANDS.md publishes %q inside the "+
			"contention line. Either the policy moved and the specification must follow, or the "+
			"line has stopped naming the policy", got, want)
	}
}

// budgetSelector is the value graphStatementError branches on to produce the
// published budget line. It is named rather than written inline so a reader sees
// WHY it is passed: it is the truthful value on the served path too, because the
// server's typed failure reports a statement its own deadline cut.
func budgetSelector() error { return context.DeadlineExceeded }
