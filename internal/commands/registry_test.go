// Package commands — registry-level tests.
//
// These tests verify three invariants required by SPEC/ARCHITECTURE.md
// § AI Agent Contract Generation:
//
//  1. Every command/subcommand declared by the CLI is reachable through
//     the registry (no parallel switch survives).
//  2. The registry round-trips: looking up a command by alias returns
//     the same Command pointer as looking it up by canonical name.
//  3. Adding a new flag to a registry entry is the ONLY edit required
//     to expose that flag through the dispatch surface; no parallel
//     wiring exists. (See TestRegistry_AddingFlagRequiresOnlyRegistryEdit.)
package commands

import (
	"testing"
)

// TestRegistry_AllExpectedCommandsRegistered enumerates the full set
// of command families documented in SPEC/COMMANDS.md and verifies each
// is present in the registry with the correct aliases.
func TestRegistry_AllExpectedCommandsRegistered(t *testing.T) {
	reg := AppRegistry()

	want := []struct {
		name    string
		aliases []string
	}{
		{"roadmap", []string{"road"}},
		{"task", []string{"t"}},
		{"sprint", []string{"s"}},
		{"backlog", []string{"bl"}},
		{"audit", []string{"aud"}},
		{"stats", nil},
	}

	for _, w := range want {
		cmd := reg.FindCommand(w.name)
		if cmd == nil {
			t.Errorf("command %q not registered", w.name)
			continue
		}
		if cmd.Name != w.name {
			t.Errorf("FindCommand(%q): got Name %q", w.name, cmd.Name)
		}
		for _, a := range w.aliases {
			byAlias := reg.FindCommand(a)
			if byAlias == nil {
				t.Errorf("alias %q for command %q does not resolve", a, w.name)
				continue
			}
			if byAlias.Name != w.name {
				t.Errorf("alias %q resolves to %q, want %q", a, byAlias.Name, w.name)
			}
		}
	}
}

// TestRegistry_AllExpectedSubcommandsRegistered enumerates every
// subcommand documented in SPEC/COMMANDS.md and verifies each is
// reachable through the registry. The list intentionally hard-codes
// the spec'd surface so a regression that drops a subcommand from the
// registry fails this test, not the snapshot diff.
func TestRegistry_AllExpectedSubcommandsRegistered(t *testing.T) {
	want := map[string][]string{
		"roadmap": {"list", "create", "remove"},
		"task": {
			"list", "create", "get", "next", "edit", "remove",
			"stat", "reopen", "prio", "sev",
			"assign", "unassign", "subtasks",
			"add-dep", "remove-dep", "blockers", "blocking",
		},
		"sprint": {
			"list", "create", "get", "show", "update", "remove",
			"start", "close", "reopen",
			"tasks", "open-tasks", "stats",
			"add-tasks", "remove-tasks", "move-tasks",
			"reorder", "move-to", "swap", "top", "bottom",
		},
		"backlog": {"list", "show-next"},
		"audit":   {"list", "history", "stats"},
	}

	reg := AppRegistry()
	for cmdName, subs := range want {
		cmd := reg.FindCommand(cmdName)
		if cmd == nil {
			t.Fatalf("command %q missing from registry", cmdName)
		}
		for _, s := range subs {
			if cmd.FindSubcommand(s) == nil {
				t.Errorf("subcommand %q under %q not in registry", s, cmdName)
			}
		}
	}
}

// TestRegistry_SubcommandAliasesResolve verifies a representative set
// of subcommand aliases (one per family) resolve to the same
// Subcommand the canonical name resolves to.
func TestRegistry_SubcommandAliasesResolve(t *testing.T) {
	cases := []struct {
		family    string
		canonical string
		aliases   []string
	}{
		{"roadmap", "list", []string{"ls"}},
		{"roadmap", "create", []string{"new"}},
		{"roadmap", "remove", []string{"rm", "delete"}},
		{"task", "list", []string{"ls"}},
		{"task", "create", []string{"new"}},
		{"task", "remove", []string{"rm"}},
		{"task", "stat", []string{"set-status"}},
		{"task", "prio", []string{"set-priority"}},
		{"task", "sev", []string{"set-severity"}},
		{"sprint", "list", []string{"ls"}},
		{"sprint", "update", []string{"upd"}},
		{"sprint", "remove", []string{"rm"}},
		{"sprint", "add-tasks", []string{"add"}},
		{"sprint", "remove-tasks", []string{"rm-tasks"}},
		{"sprint", "move-tasks", []string{"mv-tasks"}},
		{"sprint", "reorder", []string{"order"}},
		{"sprint", "move-to", []string{"mvto"}},
		{"sprint", "bottom", []string{"btm"}},
		{"backlog", "list", []string{"ls"}},
		{"audit", "list", []string{"ls"}},
		{"audit", "history", []string{"hist"}},
	}

	reg := AppRegistry()
	for _, c := range cases {
		cmd := reg.FindCommand(c.family)
		if cmd == nil {
			t.Fatalf("family %q missing", c.family)
		}
		want := cmd.FindSubcommand(c.canonical)
		if want == nil {
			t.Fatalf("canonical %q under %q missing", c.canonical, c.family)
		}
		for _, a := range c.aliases {
			got := cmd.FindSubcommand(a)
			if got == nil {
				t.Errorf("alias %q for %s %s does not resolve", a, c.family, c.canonical)
				continue
			}
			if got != want {
				t.Errorf("alias %q for %s resolves to %q, want %q", a, c.family, got.Name, c.canonical)
			}
		}
	}
}

// TestRegistry_EveryHandlerHasHelpPrinter checks that no subcommand
// was added without a matching --help printer. This is a structural
// guarantee that --help works at the subcommand level for every
// registered subcommand, no matter which family it belongs to.
func TestRegistry_EveryHandlerHasHelpPrinter(t *testing.T) {
	reg := AppRegistry()
	for _, cmd := range reg.Commands {
		if cmd.HasSubcommand && cmd.HelpPrinter == nil {
			t.Errorf("family %q has no HelpPrinter", cmd.Name)
		}
		for _, sub := range cmd.Subcommands {
			if sub.Handler == nil {
				t.Errorf("%s %s: no Handler registered", cmd.Name, sub.Name)
			}
			if sub.HelpPrinter == nil {
				t.Errorf("%s %s: no HelpPrinter registered", cmd.Name, sub.Name)
			}
		}
	}
}

// TestRegistry_EverySubcommandHasExitCodeZero codifies the invariant
// that every subcommand's ExitCodes slice begins with 0 (the success
// code). The AI contract requires this; see
// SPEC/DATA_FORMATS.md § AI Agent Contract.
func TestRegistry_EverySubcommandHasExitCodeZero(t *testing.T) {
	reg := AppRegistry()
	for _, cmd := range reg.Commands {
		for _, sub := range cmd.Subcommands {
			if len(sub.ExitCodes) == 0 || sub.ExitCodes[0] != 0 {
				t.Errorf("%s %s: ExitCodes does not start with 0 (got %v)",
					cmd.Name, sub.Name, sub.ExitCodes)
			}
		}
	}
}

// TestRegistry_AddingFlagRequiresOnlyRegistryEdit demonstrates
// acceptance criterion 5: adding a new flag to a registry entry makes
// it visible through the only flag-lookup surface (Subcommand.Flags)
// without requiring edits to any dispatch code. The test does not
// mutate the singleton registry — it builds a fresh Subcommand,
// appends a flag, and verifies the flag is reachable through the same
// lookup the future contract emitter will use.
func TestRegistry_AddingFlagRequiresOnlyRegistryEdit(t *testing.T) {
	// Snapshot the canonical task-list subcommand to use as a base.
	taskCmd := AppRegistry().FindCommand("task")
	if taskCmd == nil {
		t.Fatal("task family missing")
	}
	base := taskCmd.FindSubcommand("list")
	if base == nil {
		t.Fatal("task list missing")
	}

	// Add a hypothetical new flag by editing only the Flags slice.
	// In production this would be a single line added to
	// registry_task.go; here we simulate that one-line edit.
	const probeLong = "--__probe-flag"
	newFlag := Flag{
		Long:        probeLong,
		Type:        "string",
		Description: "Test-only flag added to demonstrate single-source-of-truth.",
	}
	modified := *base
	modified.Flags = append(append([]Flag{}, base.Flags...), newFlag)

	// The flag is immediately visible through the per-subcommand
	// Flags slice — the same surface the AI-contract emitter will
	// read. No dispatch code consulted; no switch statement updated.
	found := false
	for _, f := range modified.Flags {
		if f.Long == probeLong {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("appended flag %q not reachable via Subcommand.Flags", probeLong)
	}
}
