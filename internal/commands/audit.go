package commands

import (
	"fmt"

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
	fmt.Print(`Usage: rmp audit list -r <roadmap> [filters]

Returns audit-log entries for the roadmap, newest first (performed_at
DESC). Filters compose with AND.

Aliases: ls.

Required:
  -r, --roadmap <name>            Target roadmap

Filters:
  -o, --operation <op>            Filter by operation. See 'rmp audit --help'
                                  for the full operation enum.
  -e, --entity-type <type>        TASK or SPRINT
  --entity-id <id>                Integer id within the entity type
  --since <date>                  Inclusive lower bound on performed_at
                                  (ISO 8601 with millisecond precision, e.g.
                                  2026-01-01T00:00:00.000Z; date-only also accepted)
  --until <date>                  Inclusive upper bound
  -l, --limit <n>                 Maximum entries returned (default 100)

Output (stdout JSON):
  Array of audit entries:
    [{ "id": <int>, "operation": "...", "entity_type": "TASK|SPRINT",
       "entity_id": <int>, "performed_at": "<ISO 8601>" }, ...]

Exit codes:
  0  Success
  3  Missing -r
  6  Invalid operation, entity-type, or date format

Examples:
  rmp audit list -r myproject
  rmp audit list -r myproject -o TASK_STATUS_CHANGE -e TASK
  rmp audit list -r myproject --entity-id 42 --since 2026-01-01
  rmp audit list -r myproject --since 2026-01-01 --until 2026-01-31 -l 500
`)
}

// printAuditHistoryHelp — `rmp audit history`.
func printAuditHistoryHelp() {
	fmt.Print(`Usage: rmp audit history -r <roadmap> <entity-type> <entity-id>

Returns every audit entry recorded for a single entity, newest first.
Equivalent to 'rmp audit list -r <roadmap> -e <entity-type> --entity-id <id>'
without pagination.

Aliases: hist.

Required:
  -r, --roadmap <name>            Target roadmap
  <entity-type>                   TASK or SPRINT
  <entity-id>                     Integer id within the entity type

Output (stdout JSON):
  Array of audit entries (same shape as 'audit list').

Exit codes:
  0  Success
  3  Missing -r
  6  Bad entity-type value or non-integer id

Examples:
  rmp audit history -r myproject TASK 1
  rmp audit history -r myproject SPRINT 3
  rmp audit hist -r myproject TASK 42
`)
}

// printAuditStatsHelp — `rmp audit stats`.
func printAuditStatsHelp() {
	fmt.Print(`Usage: rmp audit stats -r <roadmap> [--since <date>] [--until <date>]

Aggregates the audit log over an optional time window: total entries,
the first/last timestamps observed, and per-operation/per-entity-type
counts.

Required:
  -r, --roadmap <name>            Target roadmap

Optional:
  --since <date>                  Aggregation window start (inclusive)
  --until <date>                  Aggregation window end (inclusive)

Output (stdout JSON):
  {
    "total_entries": <int>,
    "first_entry_at": "<ISO 8601 or empty>",
    "last_entry_at":  "<ISO 8601 or empty>",
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
		if !models.IsValidAuditOperation(op) {
			return fmt.Errorf("%w: invalid operation: %s", utils.ErrValidation, op)
		}
		operation = &op
	}
	if et, ok := result.Flags["EntityType"].(string); ok {
		if !models.IsValidEntityType(et) {
			return fmt.Errorf("%w: invalid entity type: %s", utils.ErrValidation, et)
		}
		entityType = &et
	}
	if id, ok := result.Flags["EntityID"].(int); ok {
		entityID = &id
	}
	if s, ok := result.Flags["Since"].(string); ok {
		t, err := utils.ParseISO8601(s)
		if err != nil {
			return fmt.Errorf("%w: invalid date format: %s", utils.ErrValidation, s)
		}
		normalized := utils.FormatISO8601(t)
		since = &normalized
	}
	if u, ok := result.Flags["Until"].(string); ok {
		t, err := utils.ParseISO8601(u)
		if err != nil {
			return fmt.Errorf("%w: invalid date format: %s", utils.ErrValidation, u)
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

	// Parse entity type
	if !models.IsValidEntityType(remaining[0]) {
		return fmt.Errorf("%w: invalid entity type: %s", utils.ErrValidation, remaining[0])
	}
	entityType := remaining[0]

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
		t, err := utils.ParseISO8601(s)
		if err != nil {
			return fmt.Errorf("%w: invalid date format: %s", utils.ErrValidation, s)
		}
		normalized := utils.FormatISO8601(t)
		since = &normalized
	}
	if u, ok := result.Flags["Until"].(string); ok {
		t, err := utils.ParseISO8601(u)
		if err != nil {
			return fmt.Errorf("%w: invalid date format: %s", utils.ErrValidation, u)
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

// printAuditHelp prints audit command help.
func printAuditHelp() {
	fmt.Print(`Usage: rmp audit [command] [arguments] [options]

Valid entity types (for --entity-type filter and 'history' arg):
  TASK, SPRINT

Valid operations (for --operation filter):
  Task ops:   TASK_CREATE, TASK_UPDATE, TASK_DELETE, TASK_STATUS_CHANGE,
              TASK_PRIORITY_CHANGE, TASK_SEVERITY_CHANGE,
              TASK_REOPEN, TASK_ASSIGN, TASK_UNASSIGN,
              TASK_ADD_DEP, TASK_REMOVE_DEP
  Sprint ops: SPRINT_CREATE, SPRINT_UPDATE, SPRINT_DELETE,
              SPRINT_START, SPRINT_CLOSE, SPRINT_REOPEN,
              SPRINT_ADD_TASK, SPRINT_REMOVE_TASK, SPRINT_MOVE_TASK,
              SPRINT_REORDER_TASKS, SPRINT_TASK_MOVE_POSITION, SPRINT_TASK_SWAP

Date format (--since / --until):
  ISO 8601 with millisecond precision and UTC suffix:
  YYYY-MM-DDTHH:mm:ss.sssZ   (e.g. 2026-01-01T00:00:00.000Z)
  RFC 3339 variants are also accepted.

Commands:
  list, ls [OPTIONS]              List audit entries (newest first)
  history, hist <type> <id>       Show full history for one entity (TASK or SPRINT)
  stats [OPTIONS]                 Show aggregate audit counts

Options (shared):
  -r, --roadmap <name>            REQUIRED. Target roadmap.
  -h, --help                      Show this help message

Options (list):
  -o, --operation <type>          Filter by operation (see Valid operations above)
  -e, --entity-type <type>        Filter by entity type (TASK or SPRINT)
  --entity-id <id>                Filter by specific entity numeric id
  --since <date>                  Lower bound on performed_at (inclusive)
  --until <date>                  Upper bound on performed_at (inclusive)
  -l, --limit <n>                 Maximum rows returned (default: 100)

Options (stats):
  --since <date>                  Aggregation window start
  --until <date>                  Aggregation window end

Output (stdout JSON):
  list, history       Array of audit-entry objects, performed_at DESC.
                       Keys: id, operation, entity_type, entity_id, performed_at.
  stats               AuditStats: total_entries, first_entry_at, last_entry_at,
                       by_operation (map), by_entity_type (map).

Exit codes:
  0   Success
  3   No roadmap specified (-r missing)
  6   Validation error (bad operation/entity-type/date format)

Examples:
  rmp audit list -r myproject
  rmp audit list -r myproject -o TASK_STATUS_CHANGE -e TASK
  rmp audit list -r myproject --since 2026-01-01 --until 2026-01-31 -l 500
  rmp audit history -r myproject TASK 1
  rmp audit history -r myproject SPRINT 3
  rmp audit stats -r myproject --since 2026-01-01T00:00:00.000Z
`)
}
