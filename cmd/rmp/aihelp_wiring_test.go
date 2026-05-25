// cmd/rmp/aihelp_wiring_test.go — unit tests for the AI Agent
// Contract wiring.
//
// The tests cover:
//
//   - Scope extraction from argv shapes (pure function).
//   - End-to-end wiring through maybeHandleAIHelp (captures stdout
//     and stderr in bytes.Buffer so no subprocess is required).
//   - Suppression sentinel transitions (aihelp.WasInvoked() flips
//     to true exactly when Generate succeeds).
//   - Identical payload between `rmp --ai-help` and `rmp ai-help`.
//   - Exit code mapping for unknown command/subcommand scopes.
//
// E2E behaviour against the compiled binary is exercised separately
// in /tests via the Python integration suite; this file restricts
// itself to in-process verification.

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/aihelp"
)

// ------------------------------------------------------------------
// detectAIHelpInvocation — scope extraction.
// ------------------------------------------------------------------

func TestDetectAIHelpInvocation_NotPresent(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"task"},
		{"task", "list", "-r", "myproject"},
		{"--help"},
		{"--version"},
	}
	for _, args := range cases {
		_, ok := detectAIHelpInvocation(args)
		if ok {
			t.Errorf("detectAIHelpInvocation(%v): want ok=false, got ok=true", args)
		}
	}
}

func TestDetectAIHelpInvocation_FlagAtRoot(t *testing.T) {
	scope, ok := detectAIHelpInvocation([]string{"--ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "" || scope.Subcommand != "" {
		t.Errorf("want ScopeAll, got %+v", scope)
	}
}

func TestDetectAIHelpInvocation_CommandToken(t *testing.T) {
	scope, ok := detectAIHelpInvocation([]string{"ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "" || scope.Subcommand != "" {
		t.Errorf("want ScopeAll, got %+v", scope)
	}
}

func TestDetectAIHelpInvocation_CommandTokenWithHelpTail(t *testing.T) {
	// `rmp ai-help --help` → ScopeAll (contract wins).
	for _, tail := range [][]string{
		{"--help"},
		{"-h"},
		{"help"},
		{"--help", "-h"},
	} {
		args := append([]string{"ai-help"}, tail...)
		scope, ok := detectAIHelpInvocation(args)
		if !ok {
			t.Fatalf("args=%v: want ok=true", args)
		}
		if scope.Command != "" || scope.Subcommand != "" {
			t.Errorf("args=%v: want ScopeAll, got %+v", args, scope)
		}
	}
}

func TestDetectAIHelpInvocation_CommandTokenWithGarbage(t *testing.T) {
	// `rmp ai-help foo` → routed to a sentinel scope name so Generate
	// rejects it with ErrInvalidInput → exit 2. The exact sentinel is
	// an implementation detail; we just assert that the scope is NOT
	// ScopeAll and the test below in TestMaybeHandleAIHelp_AIHelpCommandWithGarbage
	// verifies the exit code.
	scope, ok := detectAIHelpInvocation([]string{"ai-help", "foo"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command == "" {
		t.Errorf("want non-empty scope.Command for garbage trailing arg, got %+v", scope)
	}
}

func TestDetectAIHelpInvocation_FlagAfterCommand(t *testing.T) {
	scope, ok := detectAIHelpInvocation([]string{"task", "--ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "task" || scope.Subcommand != "" {
		t.Errorf("want ScopeCommand(task), got %+v", scope)
	}
}

func TestDetectAIHelpInvocation_FlagAfterCommandAlias(t *testing.T) {
	// Aliases must canonicalise through the registry: `t` → `task`.
	scope, ok := detectAIHelpInvocation([]string{"t", "--ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "task" {
		t.Errorf("want canonical name 'task', got %q", scope.Command)
	}
}

func TestDetectAIHelpInvocation_FlagAfterSubcommand(t *testing.T) {
	scope, ok := detectAIHelpInvocation([]string{"task", "create", "--ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "task" || scope.Subcommand != "create" {
		t.Errorf("want ScopeSubcommand(task,create), got %+v", scope)
	}
}

func TestDetectAIHelpInvocation_TrailingActionFlagsIgnored(t *testing.T) {
	// `rmp task create --ai-help -t "should not be created"` → the
	// trailing flag/value tail must be discarded. The scope is
	// determined purely by the tokens BEFORE --ai-help.
	scope, ok := detectAIHelpInvocation([]string{"task", "create", "--ai-help", "-t", "should not be created"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "task" || scope.Subcommand != "create" {
		t.Errorf("want ScopeSubcommand(task,create), got %+v", scope)
	}
}

func TestDetectAIHelpInvocation_UnknownCommandPreceding(t *testing.T) {
	// Unknown commands preceding --ai-help are forwarded as
	// ScopeCommand(unknown) so Generate fails with ErrInvalidInput
	// (exit 2). The end-to-end test below asserts the exit code.
	scope, ok := detectAIHelpInvocation([]string{"invalidcmd", "--ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "invalidcmd" {
		t.Errorf("want scope.Command='invalidcmd', got %q", scope.Command)
	}
}

func TestDetectAIHelpInvocation_HelpFlagBeforeAIHelp(t *testing.T) {
	// `rmp --help --ai-help` → contract wins (ScopeAll).
	scope, ok := detectAIHelpInvocation([]string{"--help", "--ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "" || scope.Subcommand != "" {
		t.Errorf("want ScopeAll, got %+v", scope)
	}
}

func TestDetectAIHelpInvocation_VersionFlagBeforeAIHelp(t *testing.T) {
	// `rmp --version --ai-help` → contract wins (ScopeAll). SPEC is
	// silent on this combination; we apply the same precedence rule
	// that wins over --help.
	scope, ok := detectAIHelpInvocation([]string{"--version", "--ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "" || scope.Subcommand != "" {
		t.Errorf("want ScopeAll, got %+v", scope)
	}
}

func TestDetectAIHelpInvocation_SubcommandHelpFolds(t *testing.T) {
	// `rmp task --help --ai-help` → ScopeCommand(task) (the --help
	// at the subcommand slot collapses into the command-scope form
	// because the contract wins).
	scope, ok := detectAIHelpInvocation([]string{"task", "--help", "--ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "task" || scope.Subcommand != "" {
		t.Errorf("want ScopeCommand(task), got %+v", scope)
	}
}

func TestDetectAIHelpInvocation_FlagAtSubcommandSlotIsCommandScope(t *testing.T) {
	// `rmp task -h --ai-help` (subcommand slot is a help flag, not a
	// subcommand name) → ScopeCommand(task).
	scope, ok := detectAIHelpInvocation([]string{"task", "-h", "--ai-help"})
	if !ok {
		t.Fatalf("want ok=true")
	}
	if scope.Command != "task" || scope.Subcommand != "" {
		t.Errorf("want ScopeCommand(task), got %+v", scope)
	}
}

// ------------------------------------------------------------------
// maybeHandleAIHelp — end-to-end wiring.
// ------------------------------------------------------------------

// generateBytes calls maybeHandleAIHelp and returns stdout/stderr
// captures along with the exit code. Returns (handled, exit, stdout,
// stderr).
func runWiring(t *testing.T, args []string) (bool, int, []byte, []byte) {
	t.Helper()
	aihelp.ResetForTesting()
	var stdout, stderr bytes.Buffer
	handled, code := maybeHandleAIHelp(args, &stdout, &stderr)
	return handled, code, stdout.Bytes(), stderr.Bytes()
}

func TestMaybeHandleAIHelp_NotTriggered(t *testing.T) {
	handled, code, stdout, stderr := runWiring(t, []string{"task", "list", "-r", "myproject"})
	if handled {
		t.Errorf("handled=true for non-ai-help argv")
	}
	if code != 0 || len(stdout) != 0 || len(stderr) != 0 {
		t.Errorf("non-handled call leaked output: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if aihelp.WasInvoked() {
		t.Error("WasInvoked() flipped true without contract emission")
	}
}

func TestMaybeHandleAIHelp_RootFlag(t *testing.T) {
	handled, code, stdout, stderr := runWiring(t, []string{"--ai-help"})
	if !handled {
		t.Fatalf("handled=false for --ai-help")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	if len(stdout) == 0 || stdout[0] != '{' {
		t.Errorf("stdout does not start with '{': %q", stdout[:min(40, len(stdout))])
	}
	if stdout[len(stdout)-1] != '\n' {
		t.Error("stdout missing trailing newline")
	}
	// Validate JSON.
	var doc map[string]any
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if _, ok := doc["schema_version"]; !ok {
		t.Error("contract missing schema_version")
	}
	if !aihelp.WasInvoked() {
		t.Error("WasInvoked() did not flip true after successful Generate")
	}
}

func TestMaybeHandleAIHelp_CommandForm(t *testing.T) {
	handled, code, stdout, _ := runWiring(t, []string{"ai-help"})
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d, want true/0", handled, code)
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
}

func TestMaybeHandleAIHelp_FlagAndCommandFormsIdentical(t *testing.T) {
	// SPEC: "rmp ai-help" output identical to "rmp --ai-help".
	_, _, stdoutFlag, _ := runWiring(t, []string{"--ai-help"})
	_, _, stdoutCmd, _ := runWiring(t, []string{"ai-help"})
	if !bytes.Equal(stdoutFlag, stdoutCmd) {
		t.Errorf("--ai-help and ai-help produced different output\nflag len=%d cmd len=%d", len(stdoutFlag), len(stdoutCmd))
	}
}

func TestMaybeHandleAIHelp_CommandScope(t *testing.T) {
	_, code, stdout, stderr := runWiring(t, []string{"task", "--ai-help"})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	cmds, _ := doc["commands"].([]any)
	if len(cmds) != 1 {
		t.Errorf("ScopeCommand: want exactly 1 command, got %d", len(cmds))
	}
}

func TestMaybeHandleAIHelp_SubcommandScope(t *testing.T) {
	_, code, stdout, stderr := runWiring(t, []string{"task", "create", "--ai-help"})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	cmds, _ := doc["commands"].([]any)
	if len(cmds) != 1 {
		t.Fatalf("ScopeSubcommand: want exactly 1 command, got %d", len(cmds))
	}
	cmd0, _ := cmds[0].(map[string]any)
	subs, _ := cmd0["subcommands"].([]any)
	if len(subs) != 1 {
		t.Errorf("ScopeSubcommand: want exactly 1 subcommand, got %d", len(subs))
	}
}

func TestMaybeHandleAIHelp_UnknownCommandExits2(t *testing.T) {
	handled, code, stdout, stderr := runWiring(t, []string{"invalidcmd", "--ai-help"})
	if !handled {
		t.Fatalf("handled=false")
	}
	if code != ExitMisuse {
		t.Errorf("exit code = %d, want %d (ExitMisuse)", code, ExitMisuse)
	}
	if len(stdout) != 0 {
		t.Errorf("unknown-command path wrote stdout: %q", stdout)
	}
	if !bytes.HasPrefix(stderr, []byte("Error:")) {
		t.Errorf("stderr does not start with 'Error:': %q", stderr)
	}
	if !strings.Contains(string(stderr), "invalidcmd") {
		t.Errorf("stderr does not mention the offending token: %q", stderr)
	}
}

func TestMaybeHandleAIHelp_UnknownSubcommandExits2(t *testing.T) {
	handled, code, _, stderr := runWiring(t, []string{"task", "nonexistent-sub", "--ai-help"})
	if !handled {
		t.Fatalf("handled=false")
	}
	if code != ExitMisuse {
		t.Errorf("exit code = %d, want %d (ExitMisuse); stderr=%s", code, ExitMisuse, stderr)
	}
	if !strings.Contains(string(stderr), "nonexistent-sub") {
		t.Errorf("stderr does not mention the offending subcommand: %q", stderr)
	}
}

func TestMaybeHandleAIHelp_AIHelpCommandWithGarbage(t *testing.T) {
	// `rmp ai-help foo` must exit 2 with a SPEC-accurate error message
	// (no sentinel name leakage).
	handled, code, stdout, stderr := runWiring(t, []string{"ai-help", "foo"})
	if !handled {
		t.Fatalf("handled=false")
	}
	if code != ExitMisuse {
		t.Errorf("exit code = %d, want %d (ExitMisuse)", code, ExitMisuse)
	}
	if len(stdout) != 0 {
		t.Errorf("garbage-args path wrote stdout: %q", stdout)
	}
	if !bytes.HasPrefix(stderr, []byte("Error:")) {
		t.Errorf("stderr does not start with 'Error:': %q", stderr)
	}
	// Sentinel name must not leak to the user.
	if strings.Contains(string(stderr), aiHelpRejectScopeName) {
		t.Errorf("stderr leaked the sentinel name %q: %q", aiHelpRejectScopeName, stderr)
	}
	// Message must hint at the SPEC's "no positional arguments or flags
	// other than --help" rule.
	if !strings.Contains(string(stderr), "ai-help") {
		t.Errorf("stderr does not mention ai-help: %q", stderr)
	}
}

func TestMaybeHandleAIHelp_TrailingActionFlagsDoNotMutate(t *testing.T) {
	// `rmp task create --ai-help -t "should not be created"` must
	// emit the contract and exit 0 — no DB call should happen.
	// We cannot directly observe "no DB call" in a unit test (the
	// dispatcher is never invoked because the wiring short-circuits),
	// but we CAN verify that the function returns handled=true with
	// code=0, which is the necessary precondition.
	handled, code, stdout, stderr := runWiring(t, []string{"task", "create", "--ai-help", "-t", "should not be created"})
	if !handled {
		t.Fatalf("handled=false — fall-through would have called taskCreate and attempted a DB write")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	if len(stdout) == 0 || stdout[0] != '{' {
		t.Errorf("stdout does not start with '{'")
	}
}

// ------------------------------------------------------------------
// Suppression sentinel.
// ------------------------------------------------------------------

func TestWasInvoked_FlipsOnSuccessfulGenerate(t *testing.T) {
	aihelp.ResetForTesting()
	if aihelp.WasInvoked() {
		t.Fatal("WasInvoked() should be false at reset")
	}
	_, _, _, _ = runWiring(t, []string{"--ai-help"})
	if !aihelp.WasInvoked() {
		t.Error("WasInvoked() did not flip true after successful Generate")
	}
}

func TestWasInvoked_StaysOffOnInvalidScope(t *testing.T) {
	aihelp.ResetForTesting()
	_, _, _, _ = runWiring(t, []string{"invalidcmd", "--ai-help"})
	if aihelp.WasInvoked() {
		t.Error("WasInvoked() flipped true on scope-resolution failure; tasks #5/#6 will mis-suppress the agent hint")
	}
}

func TestWasInvoked_StaysOffWhenNotInvoked(t *testing.T) {
	aihelp.ResetForTesting()
	_, _, _, _ = runWiring(t, []string{"task", "list", "-r", "myproject"})
	if aihelp.WasInvoked() {
		t.Error("WasInvoked() flipped true without any --ai-help invocation")
	}
}

// TestMaybeHandleAIHelp_ContractOutputHasNoBanner asserts that the
// AI-agent discovery banner (SPEC/HELP.md § AI agent banner) is NEVER
// emitted on the contract path. The banner exists to make the JSON
// contract discoverable from --help; emitting it on top of the
// contract itself would corrupt the JSON. The first byte of every
// contract-path response must therefore be `{`.
//
// This is the in-process complement to the SPEC rule "The banner is
// not printed when the contract itself is being emitted".
func TestMaybeHandleAIHelp_ContractOutputHasNoBanner(t *testing.T) {
	cases := [][]string{
		{"--ai-help"},
		{"ai-help"},
		{"task", "--ai-help"},
		{"task", "create", "--ai-help"},
	}
	for _, args := range cases {
		handled, code, stdout, _ := runWiring(t, args)
		if !handled || code != 0 {
			t.Fatalf("%v: handled=%v code=%d, want true/0", args, handled, code)
		}
		if len(stdout) == 0 {
			t.Fatalf("%v: stdout empty", args)
		}
		// The contract is pretty-printed JSON whose first non-whitespace
		// byte is `{`. A banner prefix would push that byte past index
		// 0 and would also fail JSON parsing for any consumer using
		// json.Decoder without a stripper.
		if stdout[0] != '{' {
			t.Errorf("%v: first byte is %q, expected '{' (banner leaked into contract path)\nfirst 200 bytes: %q", args, stdout[0], string(stdout[:min(200, len(stdout))]))
		}
		// Belt-and-braces: the SPEC banner literal must not appear
		// anywhere in the contract output.
		if bytes.Contains(stdout, []byte("AI agents: run `rmp --ai-help`")) {
			t.Errorf("%v: contract output contains the discovery banner string", args)
		}
	}
}

// Pre-Go-1.21 codebases would need a local min(). Go 1.21+ has it
// built in. The module's go.mod targets Go 1.21+, so the builtin is
// available — but if a downstream toolchain change ever rolls back,
// this comment is the reminder to inline a tiny helper here.
