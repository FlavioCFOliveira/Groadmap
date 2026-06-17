# ai-help

## Description

Emits the AI Agent Contract — a pretty-printed JSON document that fully describes every command, subcommand, flag, exit code, enum, and example exposed by the binary. The contract is the canonical machine-readable surface for AI agents and automated callers.

The contract is generated at runtime from the internal command registry and is the single source of truth for the CLI shape. Agents should fetch this contract once and use it instead of scraping `--help` output, which is intended for human operators.

## Synopsis

```
rmp ai-help
rmp --ai-help
rmp <command> --ai-help
rmp <command> <subcommand> --ai-help
```

## Forms

| Form | Scope |
|------|-------|
| `rmp --ai-help` | Whole CLI: every command and every subcommand. |
| `rmp ai-help` | Whole CLI: byte-identical payload to `rmp --ai-help`. |
| `rmp <command> --ai-help` | One command and all of its subcommands. |
| `rmp <command> <subcommand> --ai-help` | One subcommand only. |

The flag `--ai-help` is a global flag recognised at every level of the command tree. The `ai-help` top-level command accepts no positional arguments and no flags other than `--help`; any other argument exits 2.

## Flags

The contract is a static description of the CLI and does not touch any roadmap database, so the `-r` / `--roadmap` flag is never required and is never used.

- The `ai-help` top-level command accepts no positional arguments and no flags other than `--help`. Passing `-r`/`--roadmap` (or any other flag or argument) to `rmp ai-help` is rejected with exit code 2.
- The `--ai-help` global flag, once it takes effect, emits the contract and ignores any trailing arguments. `rmp --ai-help -r <roadmap>` succeeds and emits the whole-CLI contract; the `-r` value is irrelevant.
- When `--ai-help` is scoped to a command, place it after the command (and any subcommand). `rmp task -r <roadmap> --ai-help` emits the `task` contract. Note that `rmp -r <roadmap> --ai-help` fails with exit code 2, because `-r` is parsed as an unknown command name for the `--ai-help` scope.

## Output

JSON document on stdout, pretty-printed with two-space indent and a trailing newline, encoded as UTF-8. Top-level fields:

| Field | Type | Description |
|-------|------|-------------|
| `schema_version` | string | Semantic version of the contract schema (independent of the binary version). |
| `tool` | object | Tool identity: `name`, `display_name`, `binary_version`, `description`. |
| `conventions` | object | Cross-cutting conventions: stdout/stderr, datetime format, list separator, the `-r` flag contract, and the `AI_AGENT` environment variable. |
| `exit_codes` | array | Canonical exit-code catalogue with name, code, sentinel, and meaning. |
| `enums` | object | Every enum the CLI accepts (task status, type, sprint status, etc.). |
| `global_flags` | array | Flags recognised at every level of the command tree (`--help`, `--ai-help`, `--version`, `-r`). |
| `commands` | array | Recursive tree of commands and subcommands with flags, examples, exit codes, side effects, and `stdout_on_success` shape. |
| `common_workflows` | array | Canonical end-to-end command sequences agents are expected to perform. |
| `pitfalls` | array | Known mistakes agents make, each paired with the correct alternative. |

The JSON shape is specified in `SPEC/DATA_FORMATS.md § AI Agent Contract`.

## Examples

Fetch the whole contract:
```bash
rmp --ai-help
rmp ai-help
```

Scope to one command:
```bash
rmp task --ai-help
```

Scope to one subcommand:
```bash
rmp task create --ai-help
```

Use with `jq` to extract specific information:
```bash
rmp --ai-help | jq '.commands[] | select(.name == "task") | .subcommands[].name'
rmp --ai-help | jq '.pitfalls[] | .id'
rmp --ai-help | jq '.common_workflows | length'
```

## Precedence

When `--ai-help` is combined with any other action flag or positional argument, the contract wins: the JSON is emitted and no action is performed. Example — combining `--ai-help` with `task create` action flags emits the contract and creates no task.

## Exit Codes

| Code | When |
|------|------|
| `0` | Contract emitted successfully. |
| `2` | `ai-help` invoked with unexpected positional arguments or flags; `--ai-help` used with an unknown command or subcommand name preceding it. |

## Discoverability

Agents that did not start at the contract are reminded of its existence through three surfaces:

- **`--help` banner**: every plain-text help output begins with the literal line `AI agents: run \`rmp --ai-help\` for a machine-readable command contract.` (followed by a blank line, then the existing help body).
- **Error path**: every `Error: ...` line written to stderr is followed by a blank line and the same hint.
- **`AI_AGENT` environment variable**: setting `AI_AGENT=1` prepends the hint as the first line of stderr on every invocation. Only the literal string `1` enables this surface; other values (including `true`, `yes`, `0`, empty, `on`) leave the CLI silent. When `AI_AGENT=1` is active and an invocation fails, the hint is emitted exactly once at the top of stderr (the trailing error-path hint is suppressed to avoid duplication).

None of these surfaces appear inside `--ai-help` JSON output or in `rmp --version` output. Banner and hint are suppressed for every contract-emitting invocation to avoid recursive guidance.

## Related Specifications

- `SPEC/COMMANDS.md § AI Help` — CLI surface and precedence rules.
- `SPEC/DATA_FORMATS.md § AI Agent Contract` — full JSON schema and required workflows / pitfalls.
- `SPEC/HELP.md § AI agent banner`, `§ Error message format`, `§ AI_AGENT environment variable` — discoverability surfaces.
- `SPEC/ARCHITECTURE.md § AI Agent Contract Generation` — registry-driven generator design.
