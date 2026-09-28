package web

import "strconv"

// absentPlaceholder is the text the interface shows in the place of a value a
// record does not carry. It is the em dash, and it exists so a reader can tell
// an absent value from a rendering fault: a cell left empty says nothing about
// which of the two produced it (SPEC/WEB.md § Roadmap Audit Log Page, "the two
// nullable columns are always rendered").
//
// It is the same placeholder the Details card of the Roadmap Task Page shows for
// an absent value, because that card renders its absent values through
// absentAuditCell, its absent commit hashes included (taskCommitHashCell). The audit
// table and the task page therefore cannot drift into two conventions for saying
// "there is nothing here"; TestAuditCell_MirrorsTheTaskPagePresentation reads the
// served task page and fails if they ever do.
const absentPlaceholder = "—"

// The Tabler class sets an audit cell carries. The Details card of the Roadmap
// Task Page shares the absent class and the monospaced face, and wraps a hash
// instead of truncating it (taskHashClass).
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
	// taskHashClass presents a task's own commit hash on the Details card of the
	// Roadmap Task Page: monospaced like the audit column, but shown whole and
	// wrapping inside its datagrid column rather than truncated, because the
	// card's column is narrow and a hash is read character by character
	// (SPEC/WEB.md § Roadmap Task Page, A commit hash is shown whole).
	taskHashClass = "font-monospace text-break"
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

// auditCommitHashCell renders a stored commit hash verbatim, or the absent
// placeholder where none is stored: an audit entry's commit_hash.
//
// The empty string counts as absent, as defence in depth: the schema requires 7
// to 64 hexadecimal characters, so an empty hash cannot be stored, and were one to
// arrive anyway a monospaced empty cell would read as the rendering fault the
// placeholder exists to rule out.
func auditCommitHashCell(hash *string) auditCell {
	if hash == nil || *hash == "" {
		return absentAuditCell()
	}
	return auditCell{Text: *hash, Class: auditHashClass}
}

// taskCommitHashCell renders a task's commit_open or commit_close on the Details
// card of the Roadmap Task Page: the stored hash whole, in the wrapping
// monospaced class, or the same absent cell the audit table shows.
func taskCommitHashCell(hash *string) auditCell {
	if hash == nil || *hash == "" {
		return absentAuditCell()
	}
	return auditCell{Text: *hash, Class: taskHashClass}
}

// auditFuncMap exposes the audit-cell helpers to the page templates: the audit
// log page and the Details card of the Roadmap Task Page. The templates call them
// rather than writing the placeholder and the classes into the markup, so the presence rule and the presentation it selects have one
// source and stay verifiable in one place — the same reason the badge colour
// helpers exist (see badge.go).
var auditFuncMap = map[string]any{
	"auditRelatedEntityCell": auditRelatedEntityCell,
	"auditCommitHashCell":    auditCommitHashCell,
	"absentCell":             absentAuditCell,
	"taskCommitHashCell":     taskCommitHashCell,
}
