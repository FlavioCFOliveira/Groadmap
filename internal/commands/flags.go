package commands

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// hasHelpFlag reports whether args carries a help token in a token position
// (SPEC/HELP.md § Help tokens). The dispatcher, the shared arity point and the
// leaf handlers call it before any other check, so that
// `rmp <cmd> <sub> --help` writes the help even when -r is missing.
//
// A help token is exactly one of `--help`, `-h` and `help`, compared as the
// whole token: `--help=1` is not one. Every position is a token position except
// the value of a flag that takes one, and whether a flag takes a value is read
// from sub's registry declaration and from nothing else. A token is the value of
// the flag written immediately before it when that flag is declared with a type
// other than "boolean" and the token does not begin with "-": the word `help`
// there is the flag's value, while `--help` and `-h` there still ask for help.
// A flag the subcommand does not declare, a boolean flag, and a flag written in
// the joined form `--flag=value` take no following token.
//
// sub may be nil, which declares no flag at all.
func hasHelpFlag(sub *Subcommand, args []string) bool {
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if isHelpToken(tok) {
			return true
		}
		if i+1 < len(args) && flagTakesValue(sub, tok) && !strings.HasPrefix(args[i+1], "-") {
			i++ // args[i+1] is tok's value, which stands in no token position
		}
	}
	return false
}

// flagTakesValue reports whether tok names a flag sub declares with a type
// other than "boolean". The long and the short spelling of one declared flag are
// the same flag; the roadmap selector is declared like any other flag on every
// subcommand that takes it. A token carrying "=" names no declared spelling, so
// it takes no following value.
func flagTakesValue(sub *Subcommand, tok string) bool {
	if sub == nil {
		return false
	}
	for i := range sub.Flags {
		f := &sub.Flags[i]
		if tok == f.Long || (f.Short != "" && tok == f.Short) {
			return f.Type != "boolean"
		}
	}
	return false
}

// splitPositionals separates the tokens a command reads by position from the
// "-"-prefixed tokens that stand in no positional slot
// (SPEC/COMMANDS.md § Positional Arguments, the paragraphs on a token written
// after and between the positional arguments). tokens are what is left once the
// command's own flags and the roadmap selector have been consumed, in
// command-line order; declared is the number of positional arguments the command
// declares.
//
// A token that does not begin with "-" is a positional argument and fills the
// next slot. A "-"-prefixed token is a stray, returned in strays in command-line
// order, when every declared slot is already filled, or when it is written after
// one positional argument and before another, in which case the positional
// argument that follows it fills the next slot. Any other "-"-prefixed token —
// written before the first positional argument, or with no positional argument
// after it while a slot is still empty — stands in that slot and is returned
// among the positionals, so the checks that slot's value faces refuse it.
//
// The caller refuses the strays with rejectUnknownFlags after its own checks on
// the positionals and before anything that needs the roadmap.
func splitPositionals(tokens []string, declared int) (positionals, strays []string) {
	total := 0
	for _, tok := range tokens {
		if !strings.HasPrefix(tok, "-") {
			total++
		}
	}

	positionals = make([]string, 0, len(tokens))
	seen := 0
	for _, tok := range tokens {
		if !strings.HasPrefix(tok, "-") {
			positionals = append(positionals, tok)
			seen++
			continue
		}
		if len(positionals) >= declared || (seen > 0 && seen < total) {
			strays = append(strays, tok)
			continue
		}
		positionals = append(positionals, tok)
	}
	return positionals, strays
}

// errUnknownFlag words the CLI-wide refusal of a token that begins with "-"
// and names none of the flags of the command it was written on
// (SPEC/COMMANDS.md § Positional Arguments, rule 5). flagName is the token as
// the command line spelled it, with a GNU-style "=value" suffix already
// removed, so "--limit=3" is reported as "--limit".
//
// The sentence exists once, here, and both refusal sites call it: the parser
// below, which is reached by every command that declares flags of its own,
// and rejectUnknownFlags, which is reached by the commands that declare none
// and therefore never build a parser. A second literal would be a second
// sentence to keep in step with SPEC/COMMANDS.md, and the two would drift.
func errUnknownFlag(flagName string) error {
	return fmt.Errorf("%w: unknown flag: %s", utils.ErrInvalidInput, flagName)
}

// rejectUnknownFlags refuses the first "-"-prefixed token in args, with the
// line errUnknownFlag words and exit code 2.
//
// It is the refusal for a command that does not build a FlagParser: every
// token it can legitimately receive has already been consumed before this
// runs — the help tokens by hasHelpFlag, the roadmap selector and its value
// by requireRoadmap where the command takes one, and the command's positional
// arguments by splitPositionals where it reads them by position — so a
// "-"-prefixed token that survives to here names nothing the command accepts.
//
// Who calls hasHelpFlag depends on the command's shape. For a family's
// subcommand, such as `roadmap list`, Command.DispatchFamily calls it before
// the handler runs. For a leaf command, such as `stats`, DispatchFamily passes
// the arguments through untouched, so the handler itself must call it over
// its whole argument list before calling this. A leaf handler that checks
// only its first token lets a help token written after the selector reach
// this function, which refuses it as an unknown flag (rmp task 474). A command that DOES declare flags of its own must not use
// this: it refuses through FlagParser.Parse, which knows its flag table.
//
// Positional arguments are not this function's concern; the shared arity
// point (checkPositionalArity, positional_arity.go) refuses those before the
// handler runs. The two together are why a command with neither flags nor
// positional arguments accepts nothing beyond its selector.
func rejectUnknownFlags(args []string) error {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		// Report the flag as written, without the GNU-style "=value" tail,
		// exactly as FlagParser.Parse reports it.
		flagName, _, _ := strings.Cut(arg, "=")
		return errUnknownFlag(flagName)
	}
	return nil
}

// FlagDef defines a command-line flag.
type FlagDef struct {
	Validator func(any) error // Optional validation function
	// ParseString, when set, reads the flag's value in place of the parser of
	// its Type and owns every refusal of it. `--entity-id` uses it so that the
	// flag and the `<entity-id>` positional of `audit history` refuse the same
	// value with the same line (SPEC/COMMANDS.md § Entity Identifier Range
	// (All Positional Ids and --entity-id), rule 4).
	ParseString func(string) (any, error)
	Name        string // Long name (e.g., "--description")
	Short       string // Short name (e.g., "-d")
	Field       string // Struct field name to populate
	Type        string // "string", "int", "bool"
	Default     string // Default value (as string)
	DisplayName string // Human-readable name for parse error messages (e.g., "entity ID")
	// IntRange is, for an "int" flag with a published range, that range as the
	// refusal of a value that is not an integer names it (e.g. "0-9"). It takes
	// precedence over DisplayName, so the refusal never carries a parser's text
	// (SPEC/COMMANDS.md § Create Task, "Every bounded integer flag is refused in
	// the same shape").
	IntRange string
	Required bool // Whether the flag is required
}

// intRange renders a flag's published range as its not-an-integer refusal
// names it: the two bounds joined by a hyphen.
func intRange(minimum, maximum int) string {
	return strconv.Itoa(minimum) + "-" + strconv.Itoa(maximum)
}

// errNotAnInteger words the refusal of a value written to a bounded integer
// flag, or to a bounded integer positional argument, that cannot be read as an
// integer — including one too large for the platform's integer type. subject
// is "value for --priority" for a flag and "priority" for a positional
// argument; the value is echoed inside the quotes as supplied, and the line
// exits 2 (SPEC/COMMANDS.md § Create Task, § Change Priority (prio)).
func errNotAnInteger(subject, value, rng string) error {
	return fmt.Errorf("%w: invalid %s: %q is not an integer in %s", utils.ErrInvalidInput, subject, value, rng)
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
	for i := range fp.defs {
		def := &fp.defs[i]
		if def.Default != "" {
			val, err := fp.parseValue(def.Default, def.Type)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid default value for %s: %v", utils.ErrInvalidInput, def.Name, err)
			}
			result.Flags[def.Field] = val
		}
	}

	// No flag is repeatable, and no value of a repeated flag is validated
	// (SPEC/COMMANDS.md § Repeated Flags): the repetition is found before any
	// value is parsed.
	if err := fp.refuseRepeats(args); err != nil {
		return nil, err
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
			return nil, errUnknownFlag(flagName)
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
			// flag, and also for the order flag (parsed as a string so the handler
			// can map a non-positive / non-integer value to exit code 6 rather than
			// the generic int-parse exit code 2). Accepting it here lets the value
			// reach the handler's validation and surface as the documented exit 6,
			// instead of a misleading "requires a value" exit 2 (finding #64).
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%w: %s requires a value", utils.ErrRequired, flagName)
			}
			acceptNegInt := isNegativeInteger(args[i+1]) && (def.Type == "int" || def.Field == "Order")
			if strings.HasPrefix(args[i+1], "-") && !acceptNegInt {
				return nil, fmt.Errorf("%w: %s requires a value", utils.ErrRequired, flagName)
			}
			value = args[i+1]
			i++
		}

		// A flag that owns its parse refuses its own values.
		if def.ParseString != nil {
			parsed, err := def.ParseString(value)
			if err != nil {
				return nil, err
			}
			result.Flags[def.Field] = parsed
			continue
		}

		// Parse and validate value
		parsed, err := fp.parseValue(value, def.Type)
		if err != nil {
			if def.IntRange != "" {
				return nil, errNotAnInteger("value for "+def.Name, value, def.IntRange)
			}
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
	for i := range fp.defs {
		def := &fp.defs[i]
		if def.Required {
			if _, ok := result.Flags[def.Field]; !ok {
				return nil, fmt.Errorf("%w: missing required flag: %s", utils.ErrRequired, def.Name)
			}
		}
	}

	return result, nil
}

// refuseRepeats reads args left to right exactly as Parse does — the selector
// and every declared flag, skipping the token that is a flag's value — and
// refuses the second occurrence of any flag, in any spelling, with the
// repeated-flag line. It examines no value, so a repeated flag is refused
// whatever its occurrences carry. It stops, refusing nothing, at the first
// token Parse itself refuses — an unrecognised flag, or a value-taking flag
// with no value — because that token is the one Parse reaches first, reading
// left to right, and its own line is the one the invocation gets.
func (fp *FlagParser) refuseRepeats(args []string) error {
	var seen utils.FlagOccurrences
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-r" || arg == "--roadmap" {
			if err := seen.Note(roadmapFlagLong, arg); err != nil {
				return err
			}
			i++
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		flagName, _, hasInline := strings.Cut(arg, "=")
		def := fp.findDef(flagName)
		if def == nil {
			return nil
		}
		if err := seen.Note(def.Name, arg); err != nil {
			return err
		}
		if def.Type == "bool" || hasInline {
			continue
		}
		if i+1 >= len(args) {
			return nil
		}
		acceptNegInt := isNegativeInteger(args[i+1]) && (def.Type == "int" || def.Field == "Order")
		if strings.HasPrefix(args[i+1], "-") && !acceptNegInt {
			return nil
		}
		i++
	}
	return nil
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
		{Name: "--priority", Short: "-p", Field: "Priority", Type: "int", IntRange: intRange(models.MinPriority, models.MaxPriority)},
		{Name: "--severity", Field: "Severity", Type: "int", IntRange: intRange(models.MinSeverity, models.MaxSeverity)},
		{Name: "--parent", Field: "ParentID", Type: "int", DisplayName: "parent task ID"},
	}

	// TaskEditFlags defines flags for task editing.
	TaskEditFlags = []FlagDef{
		{Name: "--title", Short: "-t", Field: "Title", Type: "string"},
		{Name: "--functional-requirements", Short: "-fr", Field: "FunctionalRequirements", Type: "string"},
		{Name: "--technical-requirements", Short: "-tr", Field: "TechnicalRequirements", Type: "string"},
		{Name: "--acceptance-criteria", Short: "-ac", Field: "AcceptanceCriteria", Type: "string"},
		{Name: "--type", Short: "-y", Field: "Type", Type: "string"},
		{Name: "--priority", Short: "-p", Field: "Priority", Type: "int", IntRange: intRange(models.MinPriority, models.MaxPriority)},
		{Name: "--severity", Field: "Severity", Type: "int", IntRange: intRange(models.MinSeverity, models.MaxSeverity)},
	}

	// TaskListFlags defines flags for task listing.
	TaskListFlags = []FlagDef{
		{Name: "--status", Short: "-s", Field: "Status", Type: "string"},
		{Name: "--priority", Short: "-p", Field: "Priority", Type: "int", IntRange: intRange(models.MinPriority, models.MaxPriority)},
		{Name: "--severity", Field: "Severity", Type: "int", IntRange: intRange(models.MinSeverity, models.MaxSeverity)},
		{Name: "--limit", Short: "-l", Field: "Limit", Type: "int", IntRange: intRange(models.MinListLimit, models.MaxTaskLimit)},
		{Name: "--type", Short: "-y", Field: "Type", Type: "string"},
		{Name: "--created-since", Field: "CreatedSince", Type: "string"},
		{Name: "--created-until", Field: "CreatedUntil", Type: "string"},
		{Name: "--sort", Field: "Sort", Type: "string"},
	}

	// SprintCreateFlags defines flags for sprint creation and update.
	SprintCreateFlags = []FlagDef{
		{Name: "--title", Short: "-t", Field: "Title", Type: "string"},
		{Name: "--description", Short: "-d", Field: "Description", Type: "string"},
		{Name: "--max-tasks", Field: "MaxTasks", Type: "int", IntRange: intRange(models.MinSprintMaxTasks, models.MaxSprintMaxTasks)},
		// --order is parsed as a string so the handler can enforce the
		// non-integer / non-positive cases as exit code 6 (ErrValidation) with the
		// SPEC-mandated messages, rather than the generic int-parse exit code 2.
		{Name: "--order", Field: "Order", Type: "string"},
	}

	// SprintListFlags defines flags for sprint listing.
	SprintListFlags = []FlagDef{
		{Name: "--status", Field: "Status", Type: "string"},
	}

	// SprintTasksFlags defines flags for listing tasks in a sprint.
	SprintTasksFlags = []FlagDef{
		{Name: "--status", Short: "-s", Field: "Status", Type: "string"},
		{Name: "--order-by-priority", Field: "OrderByPriority", Type: "bool"},
	}

	// AuditListFlags defines flags for audit listing.
	AuditListFlags = []FlagDef{
		{Name: "--operation", Short: "-o", Field: "Operation", Type: "string"},
		{Name: "--entity-type", Short: "-e", Field: "EntityType", Type: "string"},
		{Name: "--entity-id", Field: "EntityID", Type: "int", ParseString: parseAuditEntityID},
		{Name: "--since", Field: "Since", Type: "string"},
		{Name: "--until", Field: "Until", Type: "string"},
		{Name: "--limit", Short: "-l", Field: "Limit", Type: "int", IntRange: intRange(models.MinListLimit, models.MaxAuditLimit)},
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

// parseAuditEntityID reads the audit --entity-id flag. The flag and the second
// positional of `audit history` address the identical field — the SPEC defines
// the second command as the first with this filter applied — so both reach
// utils.ValidateIDString and refuse one value with one line: a token that is
// not an integer with the format line, `invalid entity ID: "X" (must be a
// positive integer)`, exit code 2, and an integer outside 1-2147483647 with the
// range line, exit code 6 (SPEC/COMMANDS.md § Entity Identifier Range (All
// Positional Ids and --entity-id), rule 4; § List Audit Log). The flag used to
// carry a display name of its own and printed the format verdict as
// `invalid entity ID: X`, unquoted and without the suffix.
func parseAuditEntityID(value string) (any, error) {
	id, err := utils.ValidateIDString(value, utils.FieldEntityID)
	if err != nil {
		return nil, err
	}
	return id, nil
}
