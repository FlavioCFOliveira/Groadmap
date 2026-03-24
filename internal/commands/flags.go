package commands

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// FlagDef defines a command-line flag.
type FlagDef struct {
	Name        string                  // Long name (e.g., "--description")
	Short       string                  // Short name (e.g., "-d")
	Field       string                  // Struct field name to populate
	Type        string                  // "string", "int", "bool"
	Required    bool                    // Whether the flag is required
	Default     string                  // Default value (as string)
	Validator   func(interface{}) error // Optional validation function
	DisplayName string                  // Human-readable name for parse error messages (e.g., "entity ID")
}

// ParseResult holds the result of flag parsing.
type ParseResult struct {
	Flags   map[string]interface{}
	Args    []string // Positional arguments
	Roadmap string   // Roadmap name if specified
}

// FlagParser is a generic flag parser for commands.
type FlagParser struct {
	defs []FlagDef
}

// NewFlagParser creates a new flag parser with the given flag definitions.
func NewFlagParser(defs []FlagDef) *FlagParser {
	return &FlagParser{defs: defs}
}

// Parse parses command-line arguments according to the flag definitions.
// Returns a map of parsed values and any remaining positional arguments.
func (fp *FlagParser) Parse(args []string) (*ParseResult, error) {
	result := &ParseResult{
		Flags: make(map[string]interface{}),
		Args:  make([]string, 0),
	}

	// Initialize with defaults
	for _, def := range fp.defs {
		if def.Default != "" {
			val, err := fp.parseValue(def.Default, def.Type)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid default value for %s: %v", utils.ErrInvalidInput, def.Name, err)
			}
			result.Flags[def.Field] = val
		}
	}

	// Parse arguments
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Handle roadmap flag specially
		if arg == "-r" || arg == "--roadmap" {
			if i+1 < len(args) {
				result.Roadmap = args[i+1]
				i++
				continue
			}
			return nil, fmt.Errorf("%w: %s requires a value", utils.ErrRequired, arg)
		}

		// Check if it's a flag
		if !strings.HasPrefix(arg, "-") {
			// Positional argument
			result.Args = append(result.Args, arg)
			continue
		}

		// Find matching flag definition
		def := fp.findDef(arg)
		if def == nil {
			return nil, fmt.Errorf("%w: unknown flag: %s", utils.ErrInvalidInput, arg)
		}

		// Handle boolean flags (no value required)
		if def.Type == "bool" {
			result.Flags[def.Field] = true
			continue
		}

		// Get value for non-boolean flags
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
			return nil, fmt.Errorf("%w: %s requires a value", utils.ErrRequired, arg)
		}

		value := args[i+1]
		i++

		// Parse and validate value
		parsed, err := fp.parseValue(value, def.Type)
		if err != nil {
			if def.DisplayName != "" {
				return nil, fmt.Errorf("%w: invalid %s: %s", utils.ErrInvalidInput, def.DisplayName, value)
			}
			return nil, fmt.Errorf("%w: invalid value for %s: %v", utils.ErrInvalidInput, def.Name, err)
		}

		// Run custom validator if provided
		if def.Validator != nil {
			if err := def.Validator(parsed); err != nil {
				return nil, err
			}
		}

		result.Flags[def.Field] = parsed
	}

	// Check required flags
	for _, def := range fp.defs {
		if def.Required {
			if _, ok := result.Flags[def.Field]; !ok {
				return nil, fmt.Errorf("%w: missing required flag: %s", utils.ErrRequired, def.Name)
			}
		}
	}

	return result, nil
}

// findDef finds a flag definition by name or short name.
func (fp *FlagParser) findDef(arg string) *FlagDef {
	for i := range fp.defs {
		if fp.defs[i].Name == arg || fp.defs[i].Short == arg {
			return &fp.defs[i]
		}
	}
	return nil
}

// parseValue parses a string value into the appropriate type.
func (fp *FlagParser) parseValue(value string, typ string) (interface{}, error) {
	switch typ {
	case "string":
		return value, nil
	case "int":
		return strconv.Atoi(value)
	case "bool":
		return strconv.ParseBool(value)
	default:
		return nil, fmt.Errorf("%w: unknown type: %s", utils.ErrInvalidInput, typ)
	}
}

// Bind binds the parsed result to a target struct using reflection.
// The target struct must have exported fields matching the Field names in FlagDef.
func (fp *FlagParser) Bind(result *ParseResult, target interface{}) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("%w: target must be a pointer to a struct", utils.ErrInvalidInput)
	}

	v = v.Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// Skip unexported fields
		if !fieldValue.CanSet() {
			continue
		}

		// Find matching flag value
		if val, ok := result.Flags[field.Name]; ok {
			if err := fp.setField(fieldValue, val); err != nil {
				return fmt.Errorf("cannot set field %s: %w", field.Name, err)
			}
		}
	}

	return nil
}

// setField sets a struct field from an interface{} value.
func (fp *FlagParser) setField(field reflect.Value, value interface{}) error {
	switch field.Kind() {
	case reflect.String:
		if s, ok := value.(string); ok {
			field.SetString(s)
			return nil
		}
		return fmt.Errorf("%w: expected string, got %T", utils.ErrInvalidInput, value)
	case reflect.Int:
		if i, ok := value.(int); ok {
			field.SetInt(int64(i))
			return nil
		}
		return fmt.Errorf("%w: expected int, got %T", utils.ErrInvalidInput, value)
	case reflect.Bool:
		if b, ok := value.(bool); ok {
			field.SetBool(b)
			return nil
		}
		return fmt.Errorf("%w: expected bool, got %T", utils.ErrInvalidInput, value)
	}
	return fmt.Errorf("%w: unsupported field type: %s", utils.ErrInvalidInput, field.Kind())
}

// Common flag definitions for reuse across command handlers.
var (
	// TaskCreateFlags defines flags for task creation.
	TaskCreateFlags = []FlagDef{
		{Name: "--title", Short: "-t", Field: "Title", Type: "string"},
		{Name: "--functional-requirements", Short: "-fr", Field: "FunctionalRequirements", Type: "string"},
		{Name: "--technical-requirements", Short: "-tr", Field: "TechnicalRequirements", Type: "string"},
		{Name: "--acceptance-criteria", Short: "-ac", Field: "AcceptanceCriteria", Type: "string"},
		{Name: "--type", Short: "-y", Field: "Type", Type: "string"},
		{Name: "--priority", Short: "-p", Field: "Priority", Type: "int"},
		{Name: "--severity", Field: "Severity", Type: "int"},
		{Name: "--specialists", Short: "-sp", Field: "Specialists", Type: "string"},
		{Name: "--parent", Field: "ParentID", Type: "int", DisplayName: "parent task ID"},
	}

	// TaskEditFlags defines flags for task editing.
	TaskEditFlags = []FlagDef{
		{Name: "--title", Short: "-t", Field: "Title", Type: "string"},
		{Name: "--functional-requirements", Short: "-fr", Field: "FunctionalRequirements", Type: "string"},
		{Name: "--technical-requirements", Short: "-tr", Field: "TechnicalRequirements", Type: "string"},
		{Name: "--acceptance-criteria", Short: "-ac", Field: "AcceptanceCriteria", Type: "string"},
		{Name: "--type", Short: "-y", Field: "Type", Type: "string"},
		{Name: "--priority", Short: "-p", Field: "Priority", Type: "int"},
		{Name: "--severity", Field: "Severity", Type: "int"},
		{Name: "--specialists", Short: "-sp", Field: "Specialists", Type: "string"},
	}

	// TaskListFlags defines flags for task listing.
	TaskListFlags = []FlagDef{
		{Name: "--status", Short: "-s", Field: "Status", Type: "string"},
		{Name: "--priority", Short: "-p", Field: "Priority", Type: "int"},
		{Name: "--severity", Field: "Severity", Type: "int"},
		{Name: "--limit", Short: "-l", Field: "Limit", Type: "int"},
		{Name: "--type", Short: "-y", Field: "Type", Type: "string"},
		{Name: "--specialists", Short: "-sp", Field: "Specialists", Type: "string"},
		{Name: "--created-since", Field: "CreatedSince", Type: "string"},
		{Name: "--created-until", Field: "CreatedUntil", Type: "string"},
		{Name: "--sort", Field: "Sort", Type: "string"},
	}

	// SprintCreateFlags defines flags for sprint creation and update.
	SprintCreateFlags = []FlagDef{
		{Name: "--description", Short: "-d", Field: "Description", Type: "string"},
		{Name: "--max-tasks", Field: "MaxTasks", Type: "int"},
	}

	// SprintListFlags defines flags for sprint listing.
	SprintListFlags = []FlagDef{
		{Name: "--status", Field: "Status", Type: "string"},
	}

	// SprintTasksFlags defines flags for listing tasks in a sprint.
	SprintTasksFlags = []FlagDef{
		{Name: "--status", Field: "Status", Type: "string"},
		{Name: "--order-by-priority", Field: "OrderByPriority", Type: "bool"},
	}

	// AuditListFlags defines flags for audit listing.
	AuditListFlags = []FlagDef{
		{Name: "--operation", Short: "-o", Field: "Operation", Type: "string"},
		{Name: "--entity-type", Short: "-e", Field: "EntityType", Type: "string"},
		{Name: "--entity-id", Field: "EntityID", Type: "int", DisplayName: "entity ID"},
		{Name: "--since", Field: "Since", Type: "string"},
		{Name: "--until", Field: "Until", Type: "string"},
		{Name: "--limit", Short: "-l", Field: "Limit", Type: "int", DisplayName: "limit"},
	}

	// AuditStatsFlags defines flags for audit statistics.
	AuditStatsFlags = []FlagDef{
		{Name: "--since", Field: "Since", Type: "string"},
		{Name: "--until", Field: "Until", Type: "string"},
	}
)
