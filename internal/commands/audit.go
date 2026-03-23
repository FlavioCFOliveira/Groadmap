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

	// Parse filters
	var operation, entityType *string
	var entityID *int
	var since, until *string
	limit := models.DefaultTaskLimit // Default limit

	for i := 0; i < len(remaining); i++ {
		switch remaining[i] {
		case "-o", "--operation":
			if i+1 < len(remaining) {
				if !models.IsValidAuditOperation(remaining[i+1]) {
					return fmt.Errorf("%w: invalid operation: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				operation = &remaining[i+1]
				i++
			}
		case "-e", "--entity-type":
			if i+1 < len(remaining) {
				if !models.IsValidEntityType(remaining[i+1]) {
					return fmt.Errorf("%w: invalid entity type: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				entityType = &remaining[i+1]
				i++
			}
		case "--entity-id":
			if i+1 < len(remaining) {
				id, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid entity ID: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				entityID = &id
				i++
			}
		case "--since":
			if i+1 < len(remaining) {
				if !utils.IsValidISO8601(remaining[i+1]) {
					return fmt.Errorf("%w: invalid date format: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				since = &remaining[i+1]
				i++
			}
		case "--until":
			if i+1 < len(remaining) {
				if !utils.IsValidISO8601(remaining[i+1]) {
					return fmt.Errorf("%w: invalid date format: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				until = &remaining[i+1]
				i++
			}
		case "-l", "--limit":
			if i+1 < len(remaining) {
				l, err := strconv.Atoi(remaining[i+1])
				if err != nil {
					return fmt.Errorf("%w: invalid limit: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				limit = l
				i++
			}
		}
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	entries, err := database.GetAuditEntries(ctx, operation, entityType, entityID, since, until, limit, 0)
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

	// Parse date range
	var since, until *string
	for i := 0; i < len(remaining); i++ {
		switch remaining[i] {
		case "--since":
			if i+1 < len(remaining) {
				if !utils.IsValidISO8601(remaining[i+1]) {
					return fmt.Errorf("%w: invalid date format: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				since = &remaining[i+1]
				i++
			}
		case "--until":
			if i+1 < len(remaining) {
				if !utils.IsValidISO8601(remaining[i+1]) {
					return fmt.Errorf("%w: invalid date format: %s", utils.ErrInvalidInput, remaining[i+1])
				}
				until = &remaining[i+1]
				i++
			}
		}
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

Commands:
  list, ls [OPTIONS]              List audit entries
  history, hist <type> <id>       Show entity history
  stats [OPTIONS]                 Show audit statistics

Options:
  -r, --roadmap <name>            Roadmap name (or use default)
  -o, --operation <type>          Filter by operation
  -e, --entity-type <type>        Filter by entity type
  --entity-id <id>                Filter by entity ID
  --since <date>                  Filter from date (ISO 8601)
  --until <date>                  Filter until date (ISO 8601)
  -l, --limit <n>                 Limit results (default: 100)
  -h, --help                      Show this help message

Examples:
  rmp audit list -r myproject
  rmp audit history TASK 1
  rmp audit stats --since 2026-01-01T00:00:00.000Z
`)
}
