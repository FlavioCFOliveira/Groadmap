// Package aihelp — gates on what a workflow step is allowed to publish.
//
// `common_workflows` is the array an agent copies from. Resolving to a real
// subcommand is necessary and not sufficient: a step is a line an agent is
// told to run, so the whole line must be one the binary accepts once its
// placeholder tokens are replaced by values of the kind they name
// (SPEC/DATA_FORMATS.md § A workflow step MUST be an invocation the binary
// accepts).
//
// The rule this file enforces is the first of the three that section
// publishes, and it is the one a reader cannot check by inspection: a flag
// whose `required` is false at the SUBCOMMAND level, because it is
// mandatory only for some values of a positional argument, is mandatory in
// a step that supplies such a value. `task stat`'s commit flags are the
// case in point — the subcommand entry cannot mark either flag required,
// so the step is the only place the obligation can be seen, and a step that
// omits it publishes an invocation the binary refuses with exit code 6.
//
// The defect this replaces was exactly that: `record_task_working_log`
// ended with `task stat … COMPLETED --summary "…"`, which is refused, so
// the summary the workflow exists to record was never written. The same
// obligation was already published twice in the same contract — by the
// flag's own description and by the missing_commit_hash_on_transition
// pitfall — which is what made the step a self-contradiction rather than a
// gap.
package aihelp

import (
	"strings"
	"testing"
)

// workflowSteps yields every published step as (workflow name, index,
// command), so a gate is total over the catalogue without naming the
// workflows it covers.
func workflowSteps(t *testing.T, fn func(workflow string, index int, command string)) {
	t.Helper()

	m := unmarshalAsMap(t, generateOrFatal(t, ScopeAll()))
	flows, ok := m["common_workflows"].([]any)
	if !ok || len(flows) == 0 {
		t.Fatalf("common_workflows is missing or empty: %v", m["common_workflows"])
	}

	seen := 0
	for _, rawFlow := range flows {
		flow, ok := rawFlow.(map[string]any)
		if !ok {
			t.Fatalf("a common_workflows entry is not an object: %v", rawFlow)
		}
		name, _ := flow["name"].(string)
		steps, ok := flow["steps"].([]any)
		if !ok || len(steps) == 0 {
			t.Fatalf("%s: steps is missing or empty: %v", name, flow["steps"])
		}
		for i, rawStep := range steps {
			step, ok := rawStep.(map[string]any)
			if !ok {
				t.Fatalf("%s: steps[%d] is not an object: %v", name, i, rawStep)
			}
			command, _ := step["command"].(string)
			if strings.TrimSpace(command) == "" {
				t.Errorf("%s: steps[%d].command is empty", name, i)
				continue
			}
			seen++
			fn(name, i, command)
		}
	}

	if seen < 20 {
		t.Fatalf("walked only %d workflow steps; the traversal is broken and every gate over it is vacuous", seen)
	}
}

// TestGenerate_TaskStatStepsCarryTheirMandatoryCommitFlag is the gate on
// the rule. It is derived from the target status the step names, not from a
// list of the steps that happen to transition today: a workflow added later
// that closes a task is covered the moment it is written.
//
// The two target statuses are read as whole tokens so that a step naming
// COMPLETED is not matched by one whose text merely contains the word.
func TestGenerate_TaskStatStepsCarryTheirMandatoryCommitFlag(t *testing.T) {
	// Each entry: the target status a `task stat` step may name, and the
	// flag the binary requires on that transition.
	required := []struct {
		status string
		flag   string
	}{
		{"DOING", "--commit-open"},
		{"COMPLETED", "--commit-close"},
	}

	checked := 0
	workflowSteps(t, func(workflow string, index int, command string) {
		if !strings.Contains(command, "rmp task stat ") {
			return
		}
		fields := strings.Fields(command)
		for _, r := range required {
			named := false
			for _, f := range fields {
				if f == r.status {
					named = true
					break
				}
			}
			if !named {
				continue
			}
			checked++
			if !strings.Contains(command, r.flag+" ") {
				t.Errorf("%s steps[%d] transitions a task to %s without %s: %q\n"+
					"The binary refuses this with exit code 6 and %q. The subcommand entry cannot mark the "+
					"flag required, because it is required only for this target status, so the step is the "+
					"only place the obligation can be seen.",
					workflow, index, r.status, r.flag, command,
					"Error: "+r.flag+" is required when transitioning to "+r.status)
			}
		}
	})

	// Both transitions occur in the catalogue today. If neither did, the
	// loop above would pass without having looked at anything.
	if checked < 2 {
		t.Errorf("only %d task stat transition steps were checked; the gate is passing without exercising "+
			"the rule it enforces", checked)
	}
}

// TestGenerate_NoWorkflowStepIsARejectedInvocation is the general form of
// rule 2: `common_workflows` is copied from, so an invocation that fails
// belongs in `pitfalls`, beside its correction, and never in a step where
// nothing marks it as wrong.
//
// The check it can make from here is a cross-check rather than an
// execution: a step whose command line matches a pitfall's `wrong_example`
// is, by the contract's own account, an invocation that fails. The two
// arrays contradicting each other is a defect whichever one is right.
func TestGenerate_NoWorkflowStepIsARejectedInvocation(t *testing.T) {
	wrong := map[string]string{}
	for _, entry := range pitfallEntries(t) {
		id, _ := entry["id"].(string)
		exit, _ := entry["wrong_exit"].(float64)
		if exit == 0 {
			// The mistake is a wrong belief about a command that succeeds;
			// such a line is not a rejected invocation and a workflow may
			// legitimately contain it.
			continue
		}
		example, _ := entry["wrong_example"].(string)
		wrong[strings.TrimSpace(example)] = id
	}
	if len(wrong) == 0 {
		t.Fatal("no pitfall publishes a refused wrong_example; this gate has nothing to compare against")
	}

	workflowSteps(t, func(workflow string, index int, command string) {
		if id, ok := wrong[strings.TrimSpace(command)]; ok {
			t.Errorf("%s steps[%d] publishes %q, which the %s pitfall publishes as an invocation that "+
				"fails. A step that would be refused is a defect, not a lesson: the failing form belongs "+
				"in pitfalls beside its correction, where it is marked as wrong.",
				workflow, index, command, id)
		}
	})
}

// TestGenerate_RecordTaskWorkingLogClosesWithACommitHash pins the entry the
// SPEC names. Beyond the flag itself, the workflow must state the
// precondition its own closing step depends on: COMPLETED is legal only
// from TESTING, so a reader who establishes the published prerequisites and
// then runs the steps in order must not be refused by the last one.
func TestGenerate_RecordTaskWorkingLogClosesWithACommitHash(t *testing.T) {
	m := unmarshalAsMap(t, generateOrFatal(t, ScopeAll()))
	flows, _ := m["common_workflows"].([]any)

	var flow map[string]any
	for _, rawFlow := range flows {
		entry, _ := rawFlow.(map[string]any)
		if name, _ := entry["name"].(string); name == "record_task_working_log" {
			flow = entry
			break
		}
	}
	if flow == nil {
		t.Fatal("no common_workflows entry is named record_task_working_log")
	}

	steps, _ := flow["steps"].([]any)
	if len(steps) == 0 {
		t.Fatal("record_task_working_log publishes no steps")
	}
	last, _ := steps[len(steps)-1].(map[string]any)
	command, _ := last["command"].(string)

	if !strings.Contains(command, "COMPLETED") {
		t.Fatalf("the last step no longer closes the task: %q", command)
	}
	if !strings.Contains(command, "--commit-close ") {
		t.Errorf("the closing step omits --commit-close: %q\nPublished without the hash the step is "+
			"refused with exit code 6 and the completion summary the workflow exists to record is never "+
			"written.", command)
	}
	if !strings.Contains(command, "--summary ") {
		t.Errorf("the closing step no longer carries --summary: %q; recording the summary is what the "+
			"final step is for", command)
	}

	// The precondition the closing step needs, stated where the reader —
	// and the end-to-end gate that establishes prerequisites before running
	// steps — can act on it.
	prereqs, _ := flow["prerequisites"].([]any)
	if !anyContains(prereqs, "TESTING") {
		t.Errorf("record_task_working_log does not state that the task must be in TESTING before the "+
			"closing step runs; from any other status that step is refused as an illegal transition, and a "+
			"reader who establishes only the published prerequisites is refused by the workflow's own last "+
			"line. prerequisites = %v", prereqs)
	}
}
