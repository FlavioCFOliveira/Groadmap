package commands

// Acceptance criterion 3 of SPEC/COMMANDS.md § Positional Arguments: every
// command's declared maximum equals the number § Positional Arity by Command
// publishes for it.
//
// The gate reads BOTH sides and compares them; neither side is restated here.
// The published side is parsed out of the markdown table, so a row edited in
// the file reaches the expectation immediately. The declared side is read live
// from AppRegistry, so it is what the dispatcher and the AI contract actually
// consume. A test that hard-coded either list would pin a copy rather than the
// correspondence, and would stay green through exactly the drift it exists to
// catch.
//
// The comparison is total in both directions, which is the half that keeps it
// honest as the CLI grows:
//
//   - a subcommand in the registry that the table does not publish fails,
//   - a command the table publishes that the registry does not declare fails,
//   - and a count that differs fails.
//
// The table also publishes three rows for forms that never reach the registry:
// the global switch forms resolved in cmd/rmp/main.go before any command
// lookup. They are recognised here, held to the maximum of zero the table
// publishes, and checked against their own enforcement site by
// cmd/rmp/global_arity_test.go.

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// commandsSpecPath locates SPEC/COMMANDS.md relative to this package.
const commandsSpecPath = "../../SPEC/COMMANDS.md"

// arityTableHeading is the section whose table this gate reads.
const arityTableHeading = "### Positional Arity by Command"

// backtickSpan matches one `...` span, the markup the table uses for every
// command name and every form it publishes.
var backtickSpan = regexp.MustCompile("`([^`]*)`")

// publishedArity is one parsed row of the table.
type publishedArity struct {
	// registryNames are the "family" or "family subcommand" keys the row
	// names, resolvable against AppRegistry.
	registryNames []string
	// globalForms are the whole-binary forms the row names, written with a
	// leading "rmp" and resolved before any command lookup.
	globalForms []string
	max         int
	line        int
}

// readArityTable parses § Positional Arity by Command out of SPEC/COMMANDS.md.
// It fails the test rather than returning an error: a table this gate cannot
// find is a broken gate, not a passing one.
func readArityTable(t *testing.T) []publishedArity {
	t.Helper()

	raw, err := os.ReadFile(commandsSpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", commandsSpecPath, err)
	}
	lines := strings.Split(string(raw), "\n")

	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == arityTableHeading {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s no longer contains %q; the gate has lost the table it reads", commandsSpecPath, arityTableHeading)
	}

	var rows []publishedArity
	for i := start + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "###") {
			break
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := splitTableRow(line)
		if len(cells) != 3 {
			continue
		}
		maxValue, convErr := strconv.Atoi(strings.TrimSpace(cells[1]))
		if convErr != nil {
			// The header row and the separator row land here and are the only
			// two rows that legitimately do.
			continue
		}

		row := publishedArity{max: maxValue, line: i + 1}
		for _, span := range backtickSpan.FindAllStringSubmatch(cells[0], -1) {
			form := strings.TrimSpace(span[1])
			if form == "" {
				continue
			}
			if body, isGlobal := strings.CutPrefix(form, "rmp"); isGlobal {
				row.globalForms = append(row.globalForms, form)
				// `rmp ai-help` names a registry entry as well as a global
				// form: the command exists in the registry so the contract
				// can describe it, even though the early-pass scan answers
				// the invocation.
				if name := strings.TrimSpace(body); name != "" && !strings.HasPrefix(name, "-") {
					if AppRegistry().FindCommand(name) != nil {
						row.registryNames = append(row.registryNames, name)
					}
				}
				continue
			}
			row.registryNames = append(row.registryNames, form)
		}
		if len(row.registryNames) == 0 && len(row.globalForms) == 0 {
			t.Errorf("%s:%d: table row names no command: %q", commandsSpecPath, row.line, line)
			continue
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		t.Fatalf("%s § %s yielded no rows; the gate parses nothing and would pass vacuously",
			commandsSpecPath, arityTableHeading)
	}
	return rows
}

// splitTableRow splits one markdown table row into its cells, dropping the
// empty strings the leading and trailing pipes produce.
func splitTableRow(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// lookupDeclared resolves a "family" or "family subcommand" key against the
// registry and returns the subcommand that declares the arity.
func lookupDeclared(name string) *Subcommand {
	family, sub, _ := strings.Cut(name, " ")
	cmd := AppRegistry().FindCommand(family)
	if cmd == nil {
		return nil
	}
	return cmd.FindSubcommand(strings.TrimSpace(sub))
}

// TestPositionalArity_DeclarationsMatchTheSpecTable is acceptance criterion 3
// in the direction the table leads: every published count is the count the
// command declares.
func TestPositionalArity_DeclarationsMatchTheSpecTable(t *testing.T) {
	for _, row := range readArityTable(t) {
		for _, name := range row.registryNames {
			sub := lookupDeclared(name)
			if sub == nil {
				t.Errorf("%s:%d publishes an arity for %q, which the registry does not declare",
					commandsSpecPath, row.line, name)
				continue
			}
			if got := len(sub.Positional); got != row.max {
				t.Errorf("%s:%d publishes max %d for %q; the registry declares %d positional argument(s)",
					commandsSpecPath, row.line, row.max, name, got)
			}
		}
		for _, form := range row.globalForms {
			if row.max != 0 {
				t.Errorf("%s:%d publishes max %d for the global form %q; the switch in cmd/rmp/main.go "+
					"resolves it before any command lookup and can carry no positional argument",
					commandsSpecPath, row.line, row.max, form)
			}
		}
	}
}

// TestPositionalArity_SpecTableCoversEveryRegisteredSubcommand is the same
// criterion in the other direction. Without it a subcommand added tomorrow
// would be enforced from a declaration nothing had ever compared against the
// contract.
func TestPositionalArity_SpecTableCoversEveryRegisteredSubcommand(t *testing.T) {
	published := map[string]int{}
	for _, row := range readArityTable(t) {
		for _, name := range row.registryNames {
			if prev, dup := published[name]; dup {
				t.Errorf("%s:%d publishes %q a second time (first max %d, now %d)",
					commandsSpecPath, row.line, name, prev, row.max)
			}
			published[name] = row.max
		}
	}

	var missing []string
	for _, cmd := range AppRegistry().Commands {
		for i := range cmd.Subcommands {
			name := cmd.Name
			if sub := cmd.Subcommands[i].Name; sub != "" {
				name += " " + sub
			}
			if _, ok := published[name]; !ok {
				missing = append(missing, name)
			}
			delete(published, name)
		}
	}

	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d registered subcommand(s) declare an arity that %s § %s does not publish: %v",
			len(missing), commandsSpecPath, arityTableHeading, missing)
	}

	orphaned := make([]string, 0, len(published))
	for name := range published {
		orphaned = append(orphaned, name)
	}
	sort.Strings(orphaned)
	if len(orphaned) > 0 {
		t.Errorf("%s § %s publishes an arity for %d command(s) the registry does not declare: %v",
			commandsSpecPath, arityTableHeading, len(orphaned), orphaned)
	}
}
