package web

import "github.com/FlavioCFOliveira/Groadmap/internal/models"

// Tabler "light" badge colour-variant utility classes (the bg-*-lt family).
// These are the only badge variants the semantic mapping uses; they are
// consistent with Tabler's badge examples and its dark theme (SPEC/WEB.md
// § Status, Priority, and Severity Badge Colours).
const (
	badgeGreen     = "bg-green-lt"
	badgeYellow    = "bg-yellow-lt"
	badgeBlue      = "bg-blue-lt"
	badgeCyan      = "bg-cyan-lt"
	badgeOrange    = "bg-orange-lt"
	badgeRed       = "bg-red-lt"
	badgeSecondary = "bg-secondary-lt"
)

// taskStatusBadge returns the Tabler badge colour-variant class for a task
// status, per the authoritative mapping in SPEC/WEB.md § Status, Priority, and
// Severity Badge Colours:
//
//	COMPLETED -> bg-green-lt   (work finished)
//	TESTING   -> bg-yellow-lt  (in testing / awaiting verification)
//	DOING     -> bg-blue-lt    (in progress)
//	SPRINT    -> bg-cyan-lt     (assigned to a sprint, not yet started)
//	BACKLOG   -> bg-secondary-lt (neutral / not yet planned)
//
// The function is total: every canonical TaskStatus is covered, and any value
// outside the enum falls back to the neutral bg-secondary-lt band, matching the
// BACKLOG (neutral) colour, so the helper never returns an empty class.
func taskStatusBadge(s models.TaskStatus) string {
	switch s {
	case models.StatusCompleted:
		return badgeGreen
	case models.StatusTesting:
		return badgeYellow
	case models.StatusDoing:
		return badgeBlue
	case models.StatusSprint:
		return badgeCyan
	case models.StatusBacklog:
		return badgeSecondary
	default:
		return badgeSecondary
	}
}

// sprintStatusBadge returns the Tabler badge colour-variant class for a sprint
// status, per the authoritative mapping in SPEC/WEB.md § Status, Priority, and
// Severity Badge Colours:
//
//	CLOSED  -> bg-green-lt     (sprint completed)
//	OPEN    -> bg-blue-lt      (sprint in progress / current)
//	PENDING -> bg-secondary-lt (neutral / not yet started)
//
// The function is total: every canonical SprintStatus is covered, and any value
// outside the enum falls back to the neutral bg-secondary-lt band, matching the
// PENDING (neutral) colour, so the helper never returns an empty class.
func sprintStatusBadge(s models.SprintStatus) string {
	switch s {
	case models.SprintClosed:
		return badgeGreen
	case models.SprintOpen:
		return badgeBlue
	case models.SprintPending:
		return badgeSecondary
	default:
		return badgeSecondary
	}
}

// priorityBadge returns the Tabler badge colour-variant class for a task
// priority integer, per the authoritative band mapping in SPEC/WEB.md § Status,
// Priority, and Severity Badge Colours:
//
//	7-9 -> bg-red-lt       (high)
//	4-6 -> bg-yellow-lt    (medium)
//	0-3 -> bg-secondary-lt (low / neutral)
//
// The bands cover the whole 0-9 range with no gap and no overlap. The function
// is total: any value outside 0-9 (which the data layer never produces, since
// models.Task validates priority in 0-9) falls back to the neutral
// bg-secondary-lt band, so the helper never returns an empty class.
func priorityBadge(p int) string {
	switch {
	case p >= 7:
		return badgeRed
	case p >= 4:
		return badgeYellow
	default:
		return badgeSecondary
	}
}

// severityBadge returns the Tabler badge colour-variant class for a task
// severity integer, per the authoritative band mapping in SPEC/WEB.md § Status,
// Priority, and Severity Badge Colours (reusing the canonical criticality
// ranges from COMMANDS.md § Show Sprint):
//
//	8-9 -> bg-red-lt       (critical)
//	6-7 -> bg-orange-lt    (high)
//	3-5 -> bg-yellow-lt    (medium)
//	0-2 -> bg-secondary-lt (low / neutral)
//
// The bands cover the whole 0-9 range with no gap and no overlap. The function
// is total: any value outside 0-9 (which the data layer never produces, since
// models.Task validates severity in 0-9) falls back to the neutral
// bg-secondary-lt band, so the helper never returns an empty class.
func severityBadge(s int) string {
	switch {
	case s >= 8:
		return badgeRed
	case s >= 6:
		return badgeOrange
	case s >= 3:
		return badgeYellow
	default:
		return badgeSecondary
	}
}

// badgeFuncMap is the html/template FuncMap that exposes the semantic badge
// colour helpers to every page template. It is merged into the template set at
// parse time (see embed.go) so the templates can render a status, priority, or
// severity badge with the deterministic Tabler colour variant the SPEC assigns
// to each value (SPEC/WEB.md § Status, Priority, and Severity Badge Colours).
var badgeFuncMap = map[string]any{
	"taskStatusBadge":   taskStatusBadge,
	"sprintStatusBadge": sprintStatusBadge,
	"priorityBadge":     priorityBadge,
	"severityBadge":     severityBadge,
}
