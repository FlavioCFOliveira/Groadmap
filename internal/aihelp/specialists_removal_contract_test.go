// Package aihelp — regression suite for the removal of the task specialists
// surface from the machine-readable contract (sprint 36, task #247).
//
// `rmp --ai-help` is the surface an agent reads INSTEAD of the plain-text help.
// The two must therefore agree: a command, flag or enum value that survives on
// one and not the other is a defect in its own right, whichever way round the
// difference falls. The plain-text side is swept by
// internal/commands.TestSpecialistsRemoval_NoHelpOutputMentionsTheRemovedSurface;
// this file is the contract side of the same claim, and it asserts against the
// emitted JSON rather than against the Go structures that build it, so an
// alternative rendering path cannot reintroduce what the structures no longer
// carry.
package aihelp

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// retiredContractWord matches either retired subcommand name as a whole word.
// The contract legitimately contains "assigned" ("Task is assigned to a
// sprint.") and "assignments", so a substring search would report a false
// positive; the word boundary is what separates the command token from the
// English participle. TestSpecialistsRemoval_ContractNeedleDiscriminates below
// pins that distinction rather than assuming it.
var retiredContractWord = regexp.MustCompile(`(?i)\b(?:un)?assign\b`)

func TestSpecialistsRemoval_ContractNeedleDiscriminates(t *testing.T) {
	// Word-form samples only. "TASK_UNASSIGN" and "<specialist>" are caught by
	// the literal-substring needles instead: '_' is a word character, so
	// \bunassign\b does not match inside TASK_UNASSIGN, and "specialist" is not
	// the verb at all. Splitting the two jobs is deliberate — this needle exists
	// solely to find the COMMAND token without tripping on "assigned".
	for _, sample := range []string{
		"rmp task assign -r myproject 7 alice",
		`"name": "unassign",`,
		"Use 'task unassign' to remove a specialist.",
	} {
		if !retiredContractWord.MatchString(sample) {
			t.Errorf("needle failed to match %q; the sweep would miss a real regression", sample)
		}
	}
	for _, sample := range []string{
		"Task is assigned to a sprint.",
		"Create, list, query, mutate, and order sprints and their task assignments",
		"the next available value is auto-assigned",
	} {
		if retiredContractWord.MatchString(sample) {
			t.Errorf("needle wrongly matched %q; the sweep would report a false positive", sample)
		}
	}
}

// TestSpecialistsRemoval_ContractIsValidJSONWithNoTrace is the whole acceptance
// criterion in one test: the contract still parses as JSON, and the parsed bytes
// carry no trace of the two subcommands, the flag, the filter, or the two
// retired audit operations.
func TestSpecialistsRemoval_ContractIsValidJSONWithNoTrace(t *testing.T) {
	out := generateOrFatal(t, ScopeAll())

	// Valid JSON first: the assertions below scan the raw bytes, and a scan over
	// something that is not a contract at all would prove nothing.
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("`rmp --ai-help` output is not valid JSON: %v", err)
	}
	if _, ok := doc["commands"]; !ok {
		t.Fatal("contract has no \"commands\" key; the sweep below would be scanning the wrong document")
	}

	text := string(out)
	if len(text) < 1000 {
		t.Fatalf("control: contract is only %d bytes; too small to be the full surface", len(text))
	}

	lower := strings.ToLower(text)
	for _, needle := range []string{
		"specialist",
		"--specialists",
		"\"-sp\"",
		"task_assign",
		"task_unassign",
	} {
		if strings.Contains(lower, needle) {
			t.Errorf("contract still contains %q", needle)
		}
	}
	if hit := retiredContractWord.FindString(text); hit != "" {
		t.Errorf("contract still names the retired subcommand %q", hit)
	}
}

// TestSpecialistsRemoval_ContractHasNoRetiredSubcommandOrFlag walks the parsed
// contract structurally rather than by text, so a name that happened to be
// escaped or split across the JSON encoding is still caught.
func TestSpecialistsRemoval_ContractHasNoRetiredSubcommandOrFlag(t *testing.T) {
	doc := unmarshalAsMap(t, generateOrFatal(t, ScopeAll()))
	cmds, ok := doc["commands"].([]any)
	if !ok || len(cmds) == 0 {
		t.Fatal("contract publishes no commands; the walk below is vacuous")
	}

	subsSeen, flagsSeen := 0, 0
	for _, rawCmd := range cmds {
		cmd, _ := rawCmd.(map[string]any)
		cmdName, _ := cmd["name"].(string)
		subs, _ := cmd["subcommands"].([]any)
		for _, rawSub := range subs {
			sub, _ := rawSub.(map[string]any)
			subsSeen++
			subName, _ := sub["name"].(string)
			if subName == "assign" || subName == "unassign" {
				t.Errorf("contract still publishes subcommand %q under %q", subName, cmdName)
			}
			flags, _ := sub["flags"].([]any)
			for _, rawFlag := range flags {
				flag, _ := rawFlag.(map[string]any)
				flagsSeen++
				long, _ := flag["long"].(string)
				short, _ := flag["short"].(string)
				if long == "--specialists" || short == "-sp" {
					t.Errorf("contract still publishes flag %q / %q on `rmp %s %s`",
						long, short, cmdName, subName)
				}
			}
		}
	}
	if subsSeen == 0 || flagsSeen == 0 {
		t.Fatalf("control: walked %d subcommands and %d flags; the assertions are vacuous",
			subsSeen, flagsSeen)
	}
}

// TestSpecialistsRemoval_AuditOperationEnumHasNoRetiredValues asserts the two
// retired operations are gone from the published AuditOperation enum, and — the
// half that keeps the assertion honest — that the operations that remain are
// still published. SPEC/DATA_FORMATS.md § Audit Entry keeps TASK_ASSIGN and
// TASK_UNASSIGN readable in STORED rows while removing them from the valid set,
// so what must disappear is the contract's offer of them, not the reader's
// tolerance of them.
func TestSpecialistsRemoval_AuditOperationEnumHasNoRetiredValues(t *testing.T) {
	enums := contractEnums(t, generateOrFatal(t, ScopeAll()))
	values := enumValueList(t, enums, "AuditOperation")
	if len(values) == 0 {
		t.Fatal("AuditOperation publishes no values; the assertion is vacuous")
	}

	for _, v := range values {
		if v.value == "TASK_ASSIGN" || v.value == "TASK_UNASSIGN" {
			t.Errorf("AuditOperation still publishes the retired value %q", v.value)
		}
		if strings.Contains(strings.ToLower(v.description), "specialist") {
			t.Errorf("AuditOperation value %q still describes the removed specialists field: %q",
				v.value, v.description)
		}
	}

	// The enum is published from models.ValidAuditOperations, so the removal
	// must be visible there too — and the count must match, which is what stops
	// a stray value being added back alongside the removal.
	if len(values) != len(models.ValidAuditOperations) {
		t.Errorf("AuditOperation publishes %d values, models.ValidAuditOperations has %d",
			len(values), len(models.ValidAuditOperations))
	}
	for _, op := range models.ValidAuditOperations {
		if op == "TASK_ASSIGN" || op == "TASK_UNASSIGN" {
			t.Errorf("models.ValidAuditOperations still contains the retired operation %q", op)
		}
	}
}
