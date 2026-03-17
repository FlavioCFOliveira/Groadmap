package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/export"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleExport handles export commands.
func HandleExport(args []string) error {
	if len(args) == 0 {
		printExportHelp()
		return nil
	}

	subcommand := args[0]

	switch subcommand {
	case "export":
		return exportRoadmap(args[1:])
	case "import":
		return importRoadmap(args[1:])
	default:
		return fmt.Errorf("unknown export subcommand: %s", subcommand)
	}
}

// exportRoadmap exports a roadmap to JSON.
func exportRoadmap(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("roadmap name required")
	}

	roadmapName := args[0]
	includeAudit := false
	outputFile := ""

	// Parse optional flags
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--audit":
			includeAudit = true
		case "-o", "--output":
			if i+1 < len(args) {
				outputFile = args[i+1]
				i++
			}
		default:
			// Assume it's the output file if no flag
			if outputFile == "" && !strings.HasPrefix(args[i], "-") {
				outputFile = args[i]
			}
		}
	}

	// Validate roadmap name
	if err := utils.ValidateRoadmapName(roadmapName); err != nil {
		return err
	}

	// Open database
	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Generate default output filename if not provided
	if outputFile == "" {
		timestamp := time.Now().UTC().Format("20060102_150405")
		outputFile = fmt.Sprintf("%s_export_%s.json", roadmapName, timestamp)
	}

	// Ensure .json extension
	if !strings.HasSuffix(outputFile, ".json") {
		outputFile += ".json"
	}

	// Export to file
	if err := export.ExportToFile(database, outputFile, includeAudit); err != nil {
		return err
	}

	// Get stats
	exp, err := export.ExportRoadmap(database, includeAudit)
	if err != nil {
		return err
	}
	stats := export.GetExportStats(exp)

	fmt.Printf("Exported roadmap %q to %s\n", roadmapName, outputFile)
	fmt.Printf("  Tasks: %d\n", stats["tasks"])
	fmt.Printf("  Sprints: %d\n", stats["sprints"])
	if includeAudit {
		fmt.Printf("  Audit entries: %d\n", stats["audit"])
	}

	return nil
}

// importRoadmap imports a roadmap from JSON.
func importRoadmap(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("import file required")
	}

	inputFile := args[0]
	newName := ""

	// Optional new name
	if len(args) > 1 {
		newName = args[1]
	}

	// Validate input file exists
	if _, err := os.Stat(inputFile); err != nil {
		return fmt.Errorf("import file not found: %w", err)
	}

	// Read and validate export
	data, err := os.ReadFile(inputFile)
	if err != nil {
		return fmt.Errorf("reading import file: %w", err)
	}

	var exp export.RoadmapExport
	if err := utils.FromJSON(data, &exp); err != nil {
		return fmt.Errorf("parsing import file: %w", err)
	}

	// Validate export
	if err := export.ValidateExport(&exp); err != nil {
		return fmt.Errorf("invalid export file: %w", err)
	}

	// Determine target roadmap name
	targetName := exp.Roadmap
	if newName != "" {
		targetName = newName
	}

	// Validate target name
	if err := utils.ValidateRoadmapName(targetName); err != nil {
		return err
	}

	// Check if target exists
	exists, err := utils.RoadmapExists(targetName)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("roadmap %q already exists", targetName)
	}

	// Create new database
	database, err := db.Open(targetName)
	if err != nil {
		return err
	}
	defer database.Close()

	// Import data
	if err := export.ImportRoadmap(database, &exp); err != nil {
		return err
	}

	fmt.Printf("Imported roadmap %q from %s\n", targetName, inputFile)
	fmt.Printf("  Tasks: %d\n", len(exp.Tasks))
	fmt.Printf("  Sprints: %d\n", len(exp.Sprints))
	if len(exp.Audit) > 0 {
		fmt.Printf("  Audit entries: %d\n", len(exp.Audit))
	}

	return nil
}

// printExportHelp prints help for export commands.
func printExportHelp() {
	fmt.Println("Usage: rmp roadmap export|import [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  export <name> [file.json] [--audit]")
	fmt.Println("    Export a roadmap to JSON")
	fmt.Println("    --audit: Include audit log in export")
	fmt.Println()
	fmt.Println("  import <file.json> [new-name]")
	fmt.Println("    Import a roadmap from JSON")
	fmt.Println("    new-name: Optional name for the imported roadmap")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  rmp roadmap export myroadmap")
	fmt.Println("  rmp roadmap export myroadmap export.json --audit")
	fmt.Println("  rmp roadmap import myroadmap_export.json")
	fmt.Println("  rmp roadmap import myroadmap_export.json newname")
}

// Helper functions for file operations
func init() {
	// These would be in utils but adding here for completeness
}
