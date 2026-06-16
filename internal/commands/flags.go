package commands

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// hasHelpFlag reports whether args contains a help flag in any form
// (-h, --help, help). Subcommand handlers call this before any other
// parsing so that 'rmp <cmd> <sub> --help' shows the subcommand-level
// help instead of forwarding --help to the underlying parser (which
// would normally complain about missing -r or positional arguments).
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "help" {
			return true
		}
	}
	return false
}

// FlagDef defines a command-line flag.
type FlagDef struct {
	Validator   func(any) error // Optional validation function
	Name        string          // Long name (e.g., "--description")
	Short       string          // Short name (e.g., "-d")
	Field       string          // Struct field name to populate
	Type        string          // "string", "int", "bool"
	Default     string          // Default value (as string)
	DisplayName string          // Human-readable name for parse error messages (e.g., "entity ID")
	Required    bool            // Whether the flag is required
}

// ParseResult holds the result of flag parsing.
type ParseResult struct {
	Flags   map[string]any
	Roadmap string   // Roadmap name if specified
	Args    []string // Positional arguments
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
		Flags: make(map[string]any),
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

		// Support GNU-style "--flag=value" by splitting on the first '='.
		// The right-hand side becomes the value, the left-hand side the flag.
		flagName, inlineValue, hasInline := strings.Cut(arg, "=")

		def := fp.findDef(flagName)
		if def == nil {
			return nil, fmt.Errorf("%w: unknown flag: %s", utils.ErrInvalidInput, flagName)
		}

		// Handle boolean flags (no value required, but '--flag=true|false' tolerated)
		if def.Type == "bool" {
			if hasInline {
				parsed, err := fp.parseValue(inlineValue, "bool")
				if err != nil {
					return nil, fmt.Errorf("%w: invalid value for %s: %v", utils.ErrInvalidInput, flagName, err)
				}
				result.Flags[def.Field] = parsed
			} else {
				result.Flags[def.Field] = true
			}
			continue
		}

		// Get value for non-boolean flags. Either the next arg, or the
		// inline value from "--flag=value".
		var value string
		if hasInline {
			value = inlineValue
		} else {
			// The next token is the value unless it looks like a flag. A token
			// beginning with '-' is normally treated as a flag, NOT a value — but
			// a negative integer (e.g. "-1") is a legitimate value for an int
			// flag. Accepting it here lets out-of-range negatives reach the
			// flag's range validation and surface as the documented exit 6,
			// instead of a misleading "requires a value" exit 2 (finding #64).
			if i+1 >= len(args) || (strings.HasPrefix(args[i+1], "-") && !(def.Type == "int" && isNegativeInteger(args[i+1]))) {
				return nil, fmt.Errorf("%w: %s requires a value", utils.ErrRequired, flagName)
			}
			value = args[i+1]
			i++
		}

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
func (fp *FlagParser) parseValue(value string, typ string) (any, error) {
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
func (fp *FlagParser) Bind(result *ParseResult, target any) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
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
func (fp *FlagParser) setField(field reflect.Value, value any) error {
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
		{Name: "--title", Short: "-t", Field: "Title", Type: "string"},
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
		{Name: "--entity-id", Field: "EntityID", Type: "int", DisplayName: "entity ID", Validator: validateAuditEntityID},
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

// isNegativeInteger reports whether s is a syntactically valid negative integer
// token ("-" followed by one or more digits), so the flag parser can accept it
// as an int-flag value rather than mistaking it for a flag.
func isNegativeInteger(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// validateAuditEntityID bounds the audit --entity-id flag to a positive integer
// in 1..MaxInt32 (SPEC/COMMANDS.md § Audit List). A non-positive or out-of-range
// value is rejected with exit code 6 (ErrValidation).
func validateAuditEntityID(parsed any) error {
	id, ok := parsed.(int)
	if !ok {
		return nil
	}
	if id < 1 || id > utils.MaxInt32 {
		return fmt.Errorf("%w: --entity-id must be between 1 and %d (got %d)", utils.ErrValidation, utils.MaxInt32, id)
	}
	return nil
}
