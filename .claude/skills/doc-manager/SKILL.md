---
name: doc-manager
description: |
  Automated documentation management for Go CLI projects.

  USE this skill when the user asks for:
  - "generate documentation", "create docs", "update documentation"
  - "sync docs with code"
  - "document CLI commands"
  - Any request related to project documentation, README, or command docs

  This skill is EXCLUSIVE for Go projects with CLI structure (commands/subcommands).
  DO NOT use for REST API documentation, libraries, or non-CLI projects.
---

# doc-manager

Skill for automatically managing documentation for Go CLI projects.

## Objective

Maintain project documentation synchronized with source code and technical specifications, automatically generating:

1. **README.md** at project root with command index
2. **Markdown files** per command in `./DOCS/commands/`

## When to Use

- When adding new commands/subcommands
- When changing arguments, flags, or command behavior
- To keep documentation consistent with code
- To generate initial documentation for a CLI project

## Execution Process

### Step 1: Analysis of Current State

1. Check existence of `./DOCS/` and `./DOCS/commands/` folders
2. Read existing `README.md` (if any)
3. Identify existing command documentation
4. Compare with current code structure

### Step 2: Analysis of Source of Truth

Extract information from:

1. **Source code** (`internal/commands/`):
   - Command names
   - Available subcommands
   - Arguments (positional)
   - Flags (short `-f` and long `--flag`)
   - Flag descriptions
   - Default values
   - Required vs optional commands

2. **Technical specification** (`SPEC/COMMANDS.md`):
   - Command hierarchy
   - Aliases
   - Formal descriptions
   - Usage examples

### Step 3: Documentation Generation

#### README.md (project root)

Required structure:
```markdown
# [Project Name]

[Brief project description]

## Available Commands

| Command | Description | Documentation |
|---------|-------------|---------------|
| `[command]` | [Short description] | [DOCS/commands/command.md](DOCS/commands/command.md) |

## Installation

[Installation instructions]

## Quick Start

[Basic usage example]
```

#### Files per Command (`DOCS/commands/{command}.md`)

Template per command:

```markdown
# [Command Name]

## Description

[Full description extracted from SPEC/code]

## Synopsis

```
[command] [subcommand] [arguments] [flags]
```

## Subcommands

### [subcommand-1]

**Usage:** `[command] [subcommand-1] [args] [flags]`

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `arg1` | Yes | Description |

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-f` | `--flag` | string | "" | Description |

**Examples:**
```bash
# Example 1
[command] [subcommand-1] --flag value

# Example 2
[command] [subcommand-1] arg1 -v
```

### [subcommand-2]
...

## Aliases

- `[alias1]` → `[command]`

## Notes

[Additional relevant notes]
```

### Step 4: Conflict Management

**BEFORE writing any file:**

1. Check if file already exists
2. If exists, show diff between existing and new version
3. **ASK the user**:
   - "Overwrite [file]? (y/n)"
   - "Show full diff first? (y/n)"
4. Only write after explicit confirmation

### Step 5: Validation

After generation:

1. Verify all links in README.md point to existing files
2. Verify valid markdown formatting
3. Confirm directory structure is correct

## Output Format

For each operation, report:

```
=== Generated Documentation ===

✓ README.md updated
  - 5 commands indexed
  - All links verified

✓ DOCS/commands/roadmap.md created
  - 4 subcommands documented
  - 12 flags cataloged

⚠ DOCS/commands/task.md modified
  - Awaiting user confirmation

=== Summary ===
Created: 2 | Updated: 1 | Unchanged: 2
```

## Information Extraction

### From Go Code

Search in `internal/commands/*.go`:

```go
// Main command
var [Name]Cmd = &cobra.Command{
    Use:   "[usage]",
    Short: "[short description]",
    Long:  "[long description]",
}

// Subcommand
var [Name]SubCmd = &cobra.Command{...}

// Flags
[Name]Cmd.Flags().StringP("[long]", "[short]", "[default]", "[description]")
[Name]Cmd.Flags().BoolP("[long]", "[short]", [default], "[description]")
```

### From SPEC

Read `SPEC/COMMANDS.md` and `SPEC/HELP_EXAMPLES.md` for:
- Formal descriptions
- Usage examples
- Documented aliases

## Conventions

1. **File names**: lowercase, no spaces, `.md`
   - Ex: `roadmap.md`, `task-create.md`

2. **Relative links**: use paths relative to root
   - `[docs](DOCS/commands/roadmap.md)`

3. **Tables**: use GitHub-flavored markdown format

4. **Code examples**: always specify language
   - ` ```bash `, ` ```go `

5. **Dates**: do not include generation dates (documentation is "timeless")

## Limitations

- Only documents CLI commands (not APIs, libraries, etc.)
- Requires Cobra or similar structure for automatic extraction
- Complex examples may require manual input

## Error Handling

- If `SPEC/` does not exist: use only code as source
- If command has no description: use placeholder `[description pending]`
- If flag has no documentation: extract from name + type
