package web

import "strconv"

// absentPlaceholder is the text the interface shows in the place of a value a
// record does not carry. It is the em dash, and it exists so a reader can tell
// an absent value from a rendering fault: a cell left empty says nothing about
// which of the two produced it (SPEC/WEB.md § Roadmap Audit Log Page, "the two
// nullable columns are always rendered").
//
// It is the same placeholder the task detail modal writes, where it is the
// ABSENT constant of static/task-modal.js.
// TestAuditCell_MirrorsTheTaskModalPresentation reads that file and fails if the
// two ever disagree, so the audit table and the modal cannot drift into two
// conventions for saying "there is nothing here".
const absentPlaceholder = "—"

// The Tabler class sets an audit cell carries. Both come from the task detail
// modal's commitItem, which presents the task's own commit hashes
// (static/task-modal.js); they are reused here rather than chosen again.
//
//   - auditHashClass presents a commit hash: monospaced, because a hash is read
//     character by character when it is compared against a repository, and
//     abbreviated by the stylesheet rather than by the renderer. The stored value
//     reaches the page verbatim, which is what SPEC/WEB.md § Roadmap Audit Log
//     Page requires of the Commit column — "does not abbreviate it, does not
//     expand it" — while text-truncate keeps a 64-character hash from wrapping
//     into an unreadable block or from setting the width of every other column.
//   - auditAbsentClass mutes the placeholder, so it reads as the absence it is
//     and not as data.
//
// A present counterpart id takes neither: it is an entity id, and it is
// presented exactly like the Entity ID column standing beside it.
const (
	auditHashClass   = "font-monospace text-truncate"
	auditAbsentClass = "text-secondary"
)

// auditCell is one rendered cell of a nullable audit-log column: the text the
// cell shows and the Tabler classes that present it, or no class where the cell
// takes the table's own presentation.
//
// The pair travels together because presence decides both. Splitting them would
// put one nullable field's presence test in two places — the helper for the text
// and an {{if}} in the template for the class — and a page that muted a hash it
// had rendered, or set a placeholder in the monospaced face, is exactly the
// drift that arrangement invites.
type auditCell struct {
	Text  string
	Class string
}

// absentAuditCell is what every nullable audit column renders when the entry
// carries no value.
func absentAuditCell() auditCell {
	return auditCell{Text: absentPlaceholder, Class: auditAbsentClass}
}

// auditRelatedEntityCell renders an audit entry's related_entity_id: the
// counterpart entity of the operation that produced the entry, or the absent
// placeholder when that operation has no counterpart.
//
// It reads the entry's own field and nothing else. Whether a counterpart exists
// does not follow from the operation name — a TASK_STATUS_BACKLOG row written by
// `sprint remove-tasks` names the sprint the task left, and one written by
// `task stat` carries none — so the value is never derived, suppressed, or
// substituted from the operation shown beside it (SPEC/WEB.md § Roadmap Audit Log
// Page, "Related Entity ID renders per entry, never inferred from the
// operation").
//
// A non-nil pointer is rendered whatever it holds. The schema admits only
// positive ids (SPEC/DATABASE.md § audit Table), so a zero would be a stored
// fault, and showing it is more honest than hiding it behind a placeholder that
// means "this operation has no counterpart".
func auditRelatedEntityCell(id *int) auditCell {
	if id == nil {
		return absentAuditCell()
	}
	return auditCell{Text: strconv.Itoa(*id)}
}

// auditCommitHashCell renders an audit entry's commit_hash verbatim, or the
// absent placeholder on the operations that carry none.
//
// The empty string counts as absent. The modal's commitItem makes the same call
// — its `if (value)` is false for an empty string as much as for null — and here
// it is also defence in depth: the schema requires 7 to 64 hexadecimal
// characters, so an empty hash cannot be stored, and were one to arrive anyway a
// monospaced empty cell would read as the rendering fault the placeholder exists
// to rule out.
func auditCommitHashCell(hash *string) auditCell {
	if hash == nil || *hash == "" {
		return absentAuditCell()
	}
	return auditCell{Text: *hash, Class: auditHashClass}
}

// auditFuncMap exposes the audit-cell helpers to the page templates. The
// template calls them rather than writing the placeholder and the classes into
// the markup, so the presence rule and the presentation it selects have one
// source and stay verifiable in one place — the same reason the badge colour
// helpers exist (see badge.go).
var auditFuncMap = map[string]any{
	"auditRelatedEntityCell": auditRelatedEntityCell,
	"auditCommitHashCell":    auditCommitHashCell,
}
