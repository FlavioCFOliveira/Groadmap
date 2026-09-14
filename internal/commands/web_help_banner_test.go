package commands

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
	"github.com/FlavioCFOliveira/Groadmap/internal/web"
)

// webDispatchDeadline bounds one dispatch of `rmp web` in these tests. Help
// and a refusal both return almost at once. The only way to exceed the
// deadline is for the invocation to have started the long-lived server, which
// blocks until SIGINT. Without the bound, that regression would hang the
// package's test run instead of failing it.
const webDispatchDeadline = 10 * time.Second

// dispatchWebBounded dispatches `rmp web` with args through the registry and
// returns what it wrote to stdout and the error it returned. If the
// invocation has not returned within webDispatchDeadline, the test binary is
// stopped with a message naming the arguments.
func dispatchWebBounded(t *testing.T, args ...string) (string, error) {
	t.Helper()
	watchdog := time.AfterFunc(webDispatchDeadline, func() {
		panic(fmt.Sprintf("rmp web %q did not return within %s: it started the server "+
			"instead of serving help or refusing", args, webDispatchDeadline))
	})
	defer watchdog.Stop()
	return dispatchInvocation(t, "web", args...)
}

// TestWebHelp_BannerWhereverTheTokenIsWritten is the regression guard for rmp
// task 475.
//
// `rmp web` is a leaf command, so the dispatcher hands its arguments to runWeb
// untouched. runWeb served help, with the AI-agent banner, only when the help
// token was the first argument. A help token written after another flag, as in
// `rmp web --no-open --help`, fell through to web.Run, whose own parser prints
// web.PrintHelp directly. The help arrived, the exit code was 0, and the first
// line was `Usage: rmp web [options]` instead of the banner that
// SPEC/HELP.md § AI agent banner requires on every plain-text help.
//
// Every row must produce exactly the bytes the banner-wrapped printer
// produces. The rows with the token first pin the behaviour that was already
// correct. Every row carries --no-open, so a regression that starts the server
// cannot also open a browser.
func TestWebHelp_BannerWhereverTheTokenIsWritten(t *testing.T) {
	want := captureStdout(t, func() { invokeHelpPrinter(web.PrintHelp) })

	cases := [][]string{
		// The help token first: unchanged.
		{"--help", "--no-open"},
		{"-h", "--no-open"},
		{"help", "--no-open"},
		// The help token after another flag: the defect of rmp task 475.
		{"--no-open", "--help"},
		{"--no-open", "-h"},
		{"--no-open", "help"},
		{"--no-open", "--port", "18231", "--help"},
		{"--no-open", "--port=18231", "--help"},
		{"--no-open", "--host", "127.0.0.1", "-h"},
	}

	for _, args := range cases {
		stdout, err := dispatchWebBounded(t, args...)
		if err != nil {
			t.Errorf("rmp web %q: a help request was refused: %v (stdout %.160q)", args, err, stdout)
			continue
		}
		if stdout != want {
			t.Errorf("rmp web %q: stdout is not the banner-wrapped help.\n got: %.160q\nwant: %.160q",
				args, stdout, want)
		}
	}
}

// TestWebHelp_OnlyTheExactTokensAreHelp is the inverse assertion. If runWeb
// treated any token that merely looks like help as a help request, every row
// above would still pass while an unrecognised flag was silently accepted.
// Such a token must still be refused with exit 2 and the line
// SPEC/COMMANDS.md § Web Interface publishes, and nothing may be written to
// stdout.
func TestWebHelp_OnlyTheExactTokensAreHelp(t *testing.T) {
	for _, flag := range []string{"--helpful", "-help"} {
		stdout, err := dispatchWebBounded(t, "--no-open", flag)
		if err == nil {
			t.Errorf("rmp web --no-open %s: a token that is not a help token was accepted; "+
				"stdout was %.160q", flag, stdout)
			continue
		}
		if !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("rmp web --no-open %s: error = %v, want it to wrap utils.ErrInvalidInput (exit 2)",
				flag, err)
		}
		if got, want := err.Error(), "invalid input: unknown flag: "+flag; got != want {
			t.Errorf("rmp web --no-open %s: message = %q, want %q", flag, got, want)
		}
		if stdout != "" {
			t.Errorf("rmp web --no-open %s: a refused invocation wrote to stdout: %.160q", flag, stdout)
		}
	}
}
