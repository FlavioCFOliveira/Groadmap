package commands

import (
	"fmt"
	"strconv"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleAudit handles audit commands.
func HandleAudit(args []string) error {
	if len(args) == 0 {
		printAuditHelp()
		return nil
	}

	subcommand := args[0]

	if subcommand == "-h" || subcommand == "--help" || subcommand == "help" {
		printAuditHelp()
		return nil
	}

	switch subcommand {
	case "list", "ls":
		return auditList(args[1:])
	case "history", "hist":
		return auditHistory(args[1:])
	case "stats":
		return auditStats(args[1:])
	default:
		return fmt.Errorf("%w: unknown audit subcommand: %s", utils.ErrInvalidInput, subcommand)
	}
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
			return fmt.Errorf("%w: invalid operation: %s", utils.ErrInvalidInput, op)
		}
		operation = &op
	}
	if et, ok := result.Flags["EntityType"].(string); ok {
		if !models.IsValidEntityType(et) {
			return fmt.Errorf("%w: invalid entity type: %s", utils.ErrInvalidInput, et)
		}
		entityType = &et
	}
	if id, ok := result.Flags["EntityID"].(int); ok {
		entityID = &id
	}
	if s, ok := result.Flags["Since"].(string); ok {
		t, err := utils.ParseISO8601(s)
		if err != nil {
			return fmt.Errorf("%w: invalid date format: %s", utils.ErrInvalidInput, s)
		}
		normalized := utils.FormatISO8601(t)
		since = &normalized
	}
	if u, ok := result.Flags["Until"].(string); ok {
		t, err := utils.ParseISO8601(u)
		if err != nil {
			return fmt.Errorf("%w: invalid date format: %s", utils.ErrInvalidInput, u)
		}
		normalized := utils.FormatISO8601(t)
		until = &normalized
	}
	if l, ok := result.Flags["Limit"].(int); ok {
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
		return fmt.Errorf("%w: invalid entity type: %s", utils.ErrInvalidInput, remaining[0])
	}
	entityType := remaining[0]

	// Parse entity ID
	entityID, err := strconv.Atoi(remaining[1])
	if err != nil {
		return fmt.Errorf("%w: invalid entity ID: %s", utils.ErrInvalidInput, remaining[1])
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
			return fmt.Errorf("%w: invalid date format: %s", utils.ErrInvalidInput, s)
		}
		normalized := utils.FormatISO8601(t)
		since = &normalized
	}
	if u, ok := result.Flags["Until"].(string); ok {
		t, err := utils.ParseISO8601(u)
		if err != nil {
			return fmt.Errorf("%w: invalid date format: %s", utils.ErrInvalidInput, u)
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
              TASK_PRIORITY_CHANGE, TASK_SEVERITY_CHANGE, TASK_TYPE_CHANGE,
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

Examples:
  rmp audit list -r myproject
  rmp audit list -r myproject -o TASK_STATUS_CHANGE -e TASK
  rmp audit list -r myproject --since 2026-01-01 --until 2026-01-31 -l 500
  rmp audit history -r myproject TASK 1
  rmp audit history -r myproject SPRINT 3
  rmp audit stats -r myproject --since 2026-01-01T00:00:00.000Z
`)
}
