package web

import (
	"strconv"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The audit log records what happened but not what it belonged to: an entry
// names an operation, an entity type, that entity's id and a timestamp, and
// nothing else (MODELS.md § Audit Entry). Read as a flat table, six task
// operations and the sprint close that followed them are seven unrelated rows.
//
// This file derives the missing relationship. Every entry is assigned to a
// PATH, and the paths branch and merge the way a git history does: a sprint's
// path opens where the sprint is created, carries the sprint's own operations
// and those of its member tasks, and merges back into the roadmap line where
// the sprint closes (SPEC/WEB.md § Audit History Paths).
//
// The derivation is done here, on the server, for two reasons. It is the only
// side that can read sprint membership, and it is the side that can be tested
// without a browser: the client receives a decided model and only draws it.
const (
	// auditPathRoadmap is the main line. Paths branch from it and merge back
	// into it; no entry is ever assigned to it.
	auditPathRoadmap = "roadmap"

	// auditPathBacklog carries the entries of tasks that are a member of no
	// sprint. A task that no longer exists has no membership either, so its
	// entries land here too (SPEC/WEB.md § Audit History Paths, rule 1).
	auditPathBacklog = "backlog"

	// auditPathSprintPrefix prefixes a sprint's path key: sprint/<id>.
	auditPathSprintPrefix = "sprint/"
)

// auditEntryView is one audit entry together with the path it belongs to and
// the part it plays in that path's shape. It embeds the entry rather than
// copying its fields, so the template and the JSON stay in step with
// models.AuditEntry by construction.
//
// Opens and Merges mark the three operations that give the tree its shape:
// SPRINT_CREATE and SPRINT_REOPEN branch a sprint's path off the roadmap line,
// and SPRINT_CLOSE merges it back. Every other entry is an ordinary point on
// its path (SPEC/WEB.md § Audit History Paths, rule 3).
// Field order follows the layout convention models.AuditEntry itself uses:
// the string fields first, so every pointer word sits in one contiguous prefix
// for the garbage collector to scan, then the embedded entry with its own ints,
// then the flags. Putting the embedded entry first would strand Path and
// PathLabel behind its two int fields and widen the scanned prefix from 72 to
// 88 bytes, which `govet`'s fieldalignment check reports.
type auditEntryView struct {
	// Path is the path key: "roadmap", "backlog", or "sprint/<id>".
	Path string

	// PathLabel is the human-readable lane name: "Backlog" or "Sprint #<id>".
	PathLabel string

	models.AuditEntry

	// Opens is true when this entry branches its path off the roadmap line.
	Opens bool

	// Merges is true when this entry merges its path back into the roadmap.
	Merges bool
}

// auditTaskIDs returns the ids of the tasks named by a page of audit entries,
// without duplicates. It is what the membership lookup is given, so the page
// asks about exactly the tasks it is about to show and no others.
//
// Order is the order of first appearance, which keeps the lookup's argument
// list deterministic and so keeps the query text stable across identical
// requests.
func auditTaskIDs(entries []models.AuditEntry) []int {
	ids := make([]int, 0, len(entries))
	seen := make(map[int]struct{}, len(entries))
	for _, entry := range entries {
		if entry.EntityType != string(models.EntityTask) {
			continue
		}
		if _, ok := seen[entry.EntityID]; ok {
			continue
		}
		seen[entry.EntityID] = struct{}{}
		ids = append(ids, entry.EntityID)
	}
	return ids
}

// buildAuditHistory assigns every entry of one page to its path.
//
// sprintOf maps a task id to the sprint it currently belongs to, as returned by
// db.GetSprintsByTasks. A task absent from the map belongs to no sprint and its
// entries go to the backlog path.
//
// The membership is CURRENT, not historical, and deliberately so: the audit
// cannot supply the historical answer, because a SPRINT_ADD_TASK or
// SPRINT_MOVE_TASK entry stores the sprint's id and never the task's, so which
// task moved is recorded nowhere. A task later moved between sprints therefore
// shows its whole history on its current sprint's path. This is a property of
// the recorded data, and the page states it to the reader rather than
// presenting a derived relationship as a historical one (SPEC/WEB.md § Audit
// History Paths, rule 5).
//
// The returned slice preserves the order it was given: the caller reads entries
// performed_at descending, and that is the order the table renders. The tree is
// built oldest first by the client, which reverses this order — a path must
// exist before a point lands on it (SPEC/WEB.md § Audit History Tree, rule 2).
func buildAuditHistory(entries []models.AuditEntry, sprintOf map[int]db.SprintRef) []auditEntryView {
	views := make([]auditEntryView, 0, len(entries))
	for _, entry := range entries {
		view := auditEntryView{AuditEntry: entry}

		switch {
		case entry.EntityType == string(models.EntitySprint):
			view.Path = auditSprintPath(entry.EntityID)
			view.PathLabel = auditSprintLabel(entry.EntityID)
			// The three operations that shape the tree. A sprint that is
			// reopened branches from the roadmap line a second time, which is
			// why REOPEN opens rather than merges.
			switch models.AuditOperation(entry.Operation) {
			case models.OpSprintCreate, models.OpSprintReopen:
				view.Opens = true
			case models.OpSprintClose:
				view.Merges = true
			}

		case sprintOf != nil:
			if sprint, ok := sprintOf[entry.EntityID]; ok {
				view.Path = auditSprintPath(sprint.ID)
				view.PathLabel = auditSprintLabel(sprint.ID)
			}
		}

		// Every entity type other than SPRINT is a task (the audit table's
		// entity_type is constrained to TASK and SPRINT; DATABASE.md § audit
		// Table), and a task with no membership belongs to the backlog.
		if view.Path == "" {
			view.Path = auditPathBacklog
			view.PathLabel = "Backlog"
		}

		views = append(views, view)
	}
	return views
}

// auditSprintPath is the path key of a sprint: sprint/<id>.
func auditSprintPath(id int) string {
	return auditPathSprintPrefix + strconv.Itoa(id)
}

// auditSprintLabel is the lane name a sprint's path shows: Sprint #<id>.
func auditSprintLabel(id int) string {
	return "Sprint #" + strconv.Itoa(id)
}
