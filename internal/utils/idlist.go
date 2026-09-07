package utils

import (
	"fmt"
	"strconv"
	"strings"
)

// The id-list rules of SPEC/COMMANDS.md § Task ID Lists (Batch Commands), in the
// one place every batch command reaches.
//
// # Why this exists rather than a count comparison at each call site
//
// Nine commands took a comma-separated id list and each guarded the same way:
//
//	if len(tasks) != len(ids) { ...not found... }
//
// The database returns one row per DISTINCT id, so that comparison differs
// whenever the caller repeats one -- and `rmp task get 7,7`, with task 7
// present, was refused with `resource not found`. A valid invocation was
// rejected and the stated cause had not occurred: nothing was missing.
//
// The set difference below answers both questions the call site actually has,
// and it cannot answer one of them wrongly while answering the other: which ids
// name no task, and -- because a repeat contributes nothing to the difference --
// that a repeated id is not one of them.

// DistinctIDs returns ids with repeats removed, in order of each id's FIRST
// occurrence. The order is the caller's, per § Task ID Lists rule 2: a message
// built from it reads back in the order the command line was written, which is
// what lets a caller correct the invocation without re-reading it.
func DistinctIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// MissingIDs returns the distinct requested ids that do not appear in found, in
// the caller's order. found may be in any order and may hold ids the caller did
// not request; neither affects the answer.
//
// An empty result means every requested id resolved, which is the only condition
// under which a batch command may proceed (§ Task ID Lists, "The test is on the
// set, never on a count").
func MissingIDs(requested, found []int) []int {
	present := make(map[int]struct{}, len(found))
	for _, id := range found {
		present[id] = struct{}{}
	}
	var missing []int
	for _, id := range DistinctIDs(requested) {
		if _, ok := present[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

// TasksNotFoundError builds the refusal for a batch command whose list named at
// least one task that does not exist, per § Task ID Lists.
//
// The singular and plural forms follow the number of MISSING ids and not the
// number supplied (rule 4), so one absent id out of five prints the singular
// line. The list is never shortened (rule 1): a caller can rebuild a corrected
// invocation from the message alone, which is the whole point of naming them.
//
// It returns nil for an empty list, so a call site may hand it the result of
// [MissingIDs] unconditionally.
func TasksNotFoundError(missing []int) error {
	switch len(missing) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("%w: task %d not found", ErrNotFound, missing[0])
	default:
		return fmt.Errorf("%w: tasks %s not found", ErrNotFound, JoinIDs(missing))
	}
}

// TasksNotInSprintError builds the refusal for a sprint assignment command whose
// list named at least one task that is not a member of the sprint.
//
// It carries ErrValidation rather than ErrNotFound because the tasks exist: what
// failed is a membership rule, not a lookup. The list obeys rules 1 to 4 exactly
// as the not-found list does.
func TasksNotInSprintError(notMembers []int, sprintID int) error {
	switch len(notMembers) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("%w: task %d is not in sprint #%d", ErrValidation, notMembers[0], sprintID)
	default:
		return fmt.Errorf("%w: tasks %s are not in sprint #%d", ErrValidation, JoinIDs(notMembers), sprintID)
	}
}

// JoinIDs renders ids separated by a comma and a space, which is the published
// rendering of the <ids> placeholder (SPEC/COMMANDS.md § Error Output). Go's own
// slice rendering was used before and produced "[4 4]" -- bracketed, space
// separated, and undeduplicated -- which is not what any table published.
func JoinIDs(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ", ")
}
