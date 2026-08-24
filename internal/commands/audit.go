package commands

import (
	"fmt"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleAudit handles audit commands via the central registry.
func HandleAudit(args []string) error {
	return dispatchFamily("audit", args)
}

// printAuditListHelp — `rmp audit list`.
func printAuditListHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp audit list -r <roadmap> [filters]

Returns audit-log entries for the roadmap, newest first (performed_at
DESC). Filters compose with AND.

Aliases: ls.

Required:
  -r, --roadmap <name>            Target roadmap

Filters:
  -o, --operation <op>            Filter by operation. See 'rmp audit --help'
                                  for the full operation enum, grouped by the
                                  entity type each operation is recorded
                                  against. Four LEGACY values are accepted but
                                  written by no command; they stay accepted so
                                  the older entries already carrying them remain
                                  filterable, and filtering on one to find
                                  current activity returns nothing. That listing
                                  marks them.
  -e, --entity-type <type>        TASK or SPRINT
  --entity-id <id>                Positive integer 1-2147483647 within the entity type
  --since <date>                  Inclusive lower bound on performed_at
                                  (ISO 8601 with millisecond precision, e.g.
                                  2026-01-01T00:00:00.000Z; date-only also accepted)
  --until <date>                  Inclusive upper bound
  -l, --limit <n>                 Maximum entries returned (range 1-500, default 100)

Output (stdout JSON):
  Array of audit entries. Every entry carries all seven keys; the last two
  are null on the operations that do not use them and are never omitted:
    [{ "id": <int>, "operation": "...", "entity_type": "TASK|SPRINT",
       "entity_id": <int>, "performed_at": "<ISO 8601>",
       "related_entity_id": <int> or null, "commit_hash": "<hex>" or null }, ...]
  commit_hash is non-null on TASK_STATUS_DOING, which carries the commit the
  work started from, and on TASK_STATUS_COMPLETED, which carries the commit
  that concluded it. No other operation records one.
  related_entity_id names the COUNTERPART entity of the operation that produced
  the entry - the task a SPRINT_ADD_TASK entry added, the sprint a
  TASK_STATUS_SPRINT entry names, the other task of a dependency pair - and is
  null when the operation has no counterpart. It is not a second copy of
  entity_id, and its presence does not follow from the operation name:
  TASK_STATUS_BACKLOG carries a sprint id from 'sprint remove-tasks' and null
  from 'task stat'. Neither key can be filtered on.

Exit codes:
  0  Success
  2  Non-integer --limit or --entity-id (rejected by the flag parser as misuse)
  3  Missing -r
  6  Invalid operation, entity-type, or date format, --limit out of 1-500,
     or --entity-id out of 1-2147483647

Examples:
  rmp audit list -r myproject
  rmp audit list -r myproject -o TASK_STATUS_DOING -e TASK
  rmp audit list -r myproject --entity-id 42 --since 2026-01-01
  rmp audit list -r myproject --since 2026-01-01 --until 2026-01-31 -l 500
`)
}

// printAuditHistoryHelp — `rmp audit history`.
func printAuditHistoryHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp audit history -r <roadmap> <entity-type> <entity-id>

Returns every audit entry recorded for a single entity, newest first.
Equivalent to 'rmp audit list -r <roadmap> -e <entity-type> --entity-id <id>'
without pagination.

Aliases: hist.

Required:
  -r, --roadmap <name>            Target roadmap
  <entity-type>                   TASK or SPRINT
  <entity-id>                     Positive integer 1-2147483647 within the entity type

Output (stdout JSON):
  Array of audit entries (same shape as 'audit list').

Exit codes:
  0  Success
  2  Non-integer <entity-id>
  3  Missing -r
  6  Bad entity-type value, or <entity-id> out of range (<1 or >2147483647)

Examples:
  rmp audit history -r myproject TASK 1
  rmp audit history -r myproject SPRINT 3
  rmp audit hist -r myproject TASK 42
`)
}

// printAuditStatsHelp — `rmp audit stats`.
func printAuditStatsHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp audit stats -r <roadmap> [--since <date>] [--until <date>]

Aggregates the audit log over an optional time window: total entries,
the first/last timestamps observed, and per-operation/per-entity-type
counts.

Required:
  -r, --roadmap <name>            Target roadmap

Optional:
  --since <date>                  Aggregation window start (inclusive)
                                  (ISO 8601 with millisecond precision, e.g.
                                  2026-01-01T00:00:00.000Z; date-only also accepted)
  --until <date>                  Aggregation window end (inclusive)

Output (stdout JSON):
  {
    "total_entries": <int>,
    "first_entry_at": "<ISO 8601>" or null (when there are no entries),
    "last_entry_at":  "<ISO 8601>" or null (when there are no entries),
    "by_operation":  {"TASK_CREATE": <int>, "TASK_UPDATE": <int>, ...},
    "by_entity_type": {"TASK": <int>, "SPRINT": <int>}
  }

Exit codes:
  0  Success
  3  Missing -r
  6  Invalid --since/--until date

Examples:
  rmp audit stats -r myproject
  rmp audit stats -r myproject --since 2026-01-01T00:00:00.000Z
  rmp audit stats -r myproject --since 2026-01-01 --until 2026-01-31
`)
}

// auditList lists audit entries with filters.
func auditList(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	fp := NewFlagParser(AuditListFlags)
	result, err := fp.Parse(remaining)
	if err != nil {
		return err
	}

	var operation, entityType *string
	var entityID *int
	var since, until *string
	limit := models.DefaultTaskLimit

	if op, ok := result.Flags["Operation"].(string); ok {
		// The enum is owned by models.ParseAuditOperation, which is the single
		// place that decides what a valid operation is and how a bad one reads.
		// Its sentinel is model-level and the exit-code mapper does not know
		// it, so wrap in utils.ErrValidation to land on exit 6 — the same shape
		// every other enum filter uses (SPEC/COMMANDS.md § List Audit Log).
		// The model sentinel is chained with a SECOND %w, not rendered with %s,
		// so errors.Is can still tell WHICH enum was rejected. Both verbs render
		// the same bytes, so only the chain distinguishes them, and %s silently
		// discards it (task #290).
		parsed, parseErr := models.ParseAuditOperation(op)
		if parseErr != nil {
			return fmt.Errorf("%w: %w", utils.ErrValidation, parseErr)
		}
		normalized := string(parsed)
		operation = &normalized
	}
	if et, ok := result.Flags["EntityType"].(string); ok {
		parsed, parseErr := models.ParseEntityType(et)
		if parseErr != nil {
			return fmt.Errorf("%w: %w", utils.ErrValidation, parseErr)
		}
		normalized := string(parsed)
		entityType = &normalized
	}
	if id, ok := result.Flags["EntityID"].(int); ok {
		entityID = &id
	}
	if s, ok := result.Flags["Since"].(string); ok {
		// One acceptance rule for every date-range filter the CLI publishes:
		// the contract declares a single `date` flag type, so ParseDateFilter
		// is the only place that decides what a date is (see filter_date.go).
		t, parseErr := ParseDateFilter("--since", s)
		if parseErr != nil {
			return parseErr
		}
		normalized := utils.FormatISO8601(t)
		since = &normalized
	}
	if u, ok := result.Flags["Until"].(string); ok {
		t, parseErr := ParseDateFilter("--until", u)
		if parseErr != nil {
			return parseErr
		}
		normalized := utils.FormatISO8601(t)
		until = &normalized
	}
	if l, ok := result.Flags["Limit"].(int); ok {
		// Bound the limit to 1..MaxAuditLimit (SPEC/COMMANDS.md § Audit List).
		// Out-of-range values are rejected with exit code 6.
		if l < 1 || l > models.MaxAuditLimit {
			return fmt.Errorf("%w: --limit must be between 1 and %d (got %d)", utils.ErrValidation, models.MaxAuditLimit, l)
		}
		limit = l
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	entries, err := database.GetAuditEntries(ctx, &db.AuditFilter{
		Operation:  operation,
		EntityType: entityType,
		EntityID:   entityID,
		Since:      since,
		Until:      until,
		Limit:      limit,
	})
	if err != nil {
		return err
	}

	return utils.PrintJSON(entries)
}

// auditHistory shows history for a specific entity.
func auditHistory(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	if len(remaining) < 2 {
		return fmt.Errorf("%w: entity type and ID required", utils.ErrRequired)
	}

	// Parse entity type through the enum's owner, so this positional and the
	// `-e` flag of `audit list` refuse a bad value in exactly the same words.
	parsedEntityType, parseErr := models.ParseEntityType(remaining[0])
	if parseErr != nil {
		return fmt.Errorf("%w: %w", utils.ErrValidation, parseErr)
	}
	entityType := string(parsedEntityType)

	// Parse and validate entity ID as a positive int in 1..MaxInt32, consistent
	// with `task get` (SPEC/COMMANDS.md § Entity History). Non-positive or
	// out-of-range values are rejected with exit code 6.
	entityID, err := utils.ValidateIDString(remaining[1], "entity")
	if err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	entries, err := database.GetEntityHistory(ctx, entityType, entityID)
	if err != nil {
		return err
	}

	return utils.PrintJSON(entries)
}

// auditStats shows audit statistics.
func auditStats(args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	fp := NewFlagParser(AuditStatsFlags)
	result, err := fp.Parse(remaining)
	if err != nil {
		return err
	}

	var since, until *string
	if s, ok := result.Flags["Since"].(string); ok {
		t, parseErr := ParseDateFilter("--since", s)
		if parseErr != nil {
			return parseErr
		}
		normalized := utils.FormatISO8601(t)
		since = &normalized
	}
	if u, ok := result.Flags["Until"].(string); ok {
		t, parseErr := ParseDateFilter("--until", u)
		if parseErr != nil {
			return parseErr
		}
		normalized := utils.FormatISO8601(t)
		until = &normalized
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	stats, err := database.GetAuditStats(ctx, since, until)
	if err != nil {
		return err
	}

	return utils.PrintJSON(stats)
}

// auditOperationGroups partitions the operation catalogue for presentation.
// Each group names exactly one entity type and one legacy status, and holds
// exactly the operations declared against that pair in internal/models
// (SPEC/HELP.md § Audit operation entity-type classification rules 5(b)
// and 6).
//
// There is deliberately NO catch-all group. The block this replaced ended in
// one, because it grouped by name prefix and did not trust the prefix to match
// every name — the same distrust rule 1 states. Now that the classification is
// declared and total, nothing can be left over, and a catch-all could only hide
// the failure the totality gate exists to produce: an unclassified operation
// would still be printed, under a heading asserting nothing about it, while the
// block still listed every value the command accepts.
//
// Dropping an unclassified operation from the block is therefore the intended
// behaviour, and it is what gives the coverage gate over this block teeth it
// did not have before. The block is rendered from the catalogue, so a newly
// declared operation used to appear here by itself and no coverage gate could
// fail on it; now it appears only once someone classifies it, and until then
// TestHelpEnumCoverage_AuditHelpListsEveryOperation reports the omission
// alongside the totality gate in internal/models.
var auditOperationGroups = []struct {
	label      string
	entityType models.EntityType
	legacy     bool
}{
	{"TASK:", models.EntityTask, false},
	{"SPRINT:", models.EntitySprint, false},
	{"TASK, LEGACY:", models.EntityTask, true},
	{"SPRINT, LEGACY:", models.EntitySprint, true},
}

// auditOperationBlock renders the "Valid operations" body of the audit family
// help from the declared classification, so an operation reaches the help under
// the entity type its rows actually carry rather than under one inferred from
// its name. The returned string is a sequence of complete lines, each
// terminated by a newline; a group with no members contributes no line.
func auditOperationBlock() string {
	// Column at which every operation list starts: the two-space block
	// indent, the widest label, and one separating space. Derived from the
	// labels rather than hard-coded, so renaming a label cannot misalign the
	// continuation lines.
	widest := 0
	for _, g := range auditOperationGroups {
		if len(g.label) > widest {
			widest = len(g.label)
		}
	}
	const blockIndent, labelGap = 2, 1
	width := blockIndent + widest + labelGap
	indent := strings.Repeat(" ", width)

	grouped := make([][]string, len(auditOperationGroups))
	for _, op := range models.ValidAuditOperations {
		class, declared := models.ClassifyAuditOperation(op)
		if !declared {
			// Unclassified: no group asserts anything true about it, and
			// inventing one here is exactly what rule 5(b) forbids. The gates
			// name it; the help stays silent rather than wrong.
			continue
		}
		for i, g := range auditOperationGroups {
			if g.entityType == class.EntityType && g.legacy == class.Legacy {
				grouped[i] = append(grouped[i], string(op))
				break
			}
		}
	}

	var b strings.Builder
	for i, g := range auditOperationGroups {
		b.WriteString(wrapEnumList(labelCell(g.label, width), indent, grouped[i]))
	}
	return b.String()
}

// labelCell renders one left-aligned label padded to the shared column
// width, prefixed by the two-space block indent.
func labelCell(label string, width int) string {
	return "  " + label + strings.Repeat(" ", width-2-len(label))
}

// wrapEnumList renders names as a comma-separated list that starts after
// label and wraps at maxWidth columns, continuation lines being aligned with
// indent. An empty name list renders nothing at all, so an empty group
// contributes no line to the help.
func wrapEnumList(label, indent string, names []string) string {
	const maxWidth = 79 // keeps every help line inside an 80-column terminal

	var b strings.Builder
	line := label
	filled := false
	for i, name := range names {
		item := name
		if i < len(names)-1 {
			item += ","
		}
		sep := ""
		if filled {
			sep = " "
		}
		if filled && len(line)+len(sep)+len(item) > maxWidth {
			b.WriteString(line)
			b.WriteByte('\n')
			line, sep = indent, ""
		}
		line += sep + item
		filled = true
	}
	if filled {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// printAuditHelp prints audit command help.
//
// The operation list is rendered from models.ValidAuditOperations rather
// than typed out, so it cannot go stale: the six comment operations were
// missing from the hand-written list this replaces.
func printAuditHelp() {
	fmt.Fprintf(helpDst(), `Usage: rmp audit [command] [arguments] [options]

Aliases: aud.

Valid entity types (for --entity-type filter and 'history' arg):
  TASK, SPRINT

Operations are grouped below by the entity type each one is recorded against:
the value the entry's entity_type field holds, and so whose history an
--operation filter on it returns.

Valid operations (for --operation filter):
%s
No command writes a LEGACY operation. They stay accepted as filter values so
that the older entries already carrying them remain filterable; filtering on
one to find current activity returns nothing.

Comment operations are recorded against the PARENT entity: a task comment
writes entity_type TASK with the owning task's id, a sprint comment writes
entity_type SPRINT with the owning sprint's id. There is no COMMENT entity
type and the comment's own id never appears in the audit log.

Date format (--since / --until):
  ISO 8601 with millisecond precision and UTC suffix:
  YYYY-MM-DDTHH:mm:ss.sssZ   (e.g. 2026-01-01T00:00:00.000Z)
  RFC 3339 variants are also accepted.
  A bare calendar date, YYYY-MM-DD (e.g. 2026-01-01), is also accepted and
  means the first instant of that day in UTC. These are the same two forms
  'task list --created-since/--created-until' accepts.

Commands:
  list, ls [OPTIONS]              List audit entries (newest first)
  history, hist <type> <id>       Show full history for one entity (TASK or SPRINT)
  stats [OPTIONS]                 Show aggregate audit counts

Options (shared):
  -r, --roadmap <name>            REQUIRED. Target roadmap.
  -h, --help                      Show this help message

Options (list):
  -o, --operation <op>            Filter by operation (see Valid operations above)
  -e, --entity-type <type>        Filter by entity type (TASK or SPRINT)
  --entity-id <id>                Filter by specific entity numeric id
                                  (positive integer 1-2147483647)
  --since <date>                  Lower bound on performed_at (inclusive)
  --until <date>                  Upper bound on performed_at (inclusive)
  -l, --limit <n>                 Maximum rows returned (range 1-500, default 100)

Options (stats):
  --since <date>                  Aggregation window start
  --until <date>                  Aggregation window end

Output (stdout JSON):
  list, history       Array of audit-entry objects, performed_at DESC.
                       Keys: id, operation, entity_type, entity_id, performed_at,
                       related_entity_id, commit_hash. All seven are always
                       present; the last two are null on the operations that do
                       not use them and are never omitted. commit_hash is
                       written on TASK_STATUS_DOING and TASK_STATUS_COMPLETED
                       alone. related_entity_id names the COUNTERPART entity of
                       the operation that produced the entry - the task a
                       SPRINT_ADD_TASK entry added, the sprint a
                       TASK_STATUS_SPRINT entry names, the other task of a
                       dependency pair - and is null when the operation has no
                       counterpart. Its presence does not follow from the
                       operation name: TASK_STATUS_BACKLOG carries a sprint id
                       from 'sprint remove-tasks' and null from 'task stat'.
                       Neither key can be filtered on.
  stats               AuditStats: total_entries, first_entry_at, last_entry_at,
                       by_operation (map), by_entity_type (map).

Exit codes:
  0   Success
  2   Non-integer --limit or --entity-id (rejected by the flag parser as misuse)
  3   No roadmap specified (-r missing)
  6   Validation error (bad operation/entity-type/date, --limit out of 1-500,
       or --entity-id out of 1-2147483647)

Examples:
  rmp audit list -r myproject
  rmp audit list -r myproject -o TASK_STATUS_DOING -e TASK
  rmp audit list -r myproject --since 2026-01-01 --until 2026-01-31 -l 500
  rmp audit history -r myproject TASK 1
  rmp audit history -r myproject SPRINT 3
  rmp audit stats -r myproject --since 2026-01-01T00:00:00.000Z
  rmp audit list -r myproject -o SPRINT_COMMENT_CREATE
`, auditOperationBlock())
}
