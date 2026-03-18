// Package commands implements CLI command handlers.
package commands

import (
	"fmt"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// HandleCompletion generates shell completion scripts.
// Usage: rmp completion [bash|zsh|fish|powershell]
func HandleCompletion(args []string) error {
	if len(args) < 1 {
		printCompletionHelp()
		return fmt.Errorf("shell type required")
	}

	shell := strings.ToLower(args[0])

	switch shell {
	case "bash":
		fmt.Print(bashCompletionScript)
	case "zsh":
		fmt.Print(zshCompletionScript)
	case "fish":
		fmt.Print(fishCompletionScript)
	case "powershell", "ps":
		fmt.Print(powershellCompletionScript)
	default:
		return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish, powershell)", shell)
	}

	return nil
}

// printCompletionHelp prints help for the completion command.
func printCompletionHelp() {
	fmt.Print(`Usage: rmp completion [shell]

Generate shell completion script for the specified shell.

Supported shells:
  bash         Generate completion script for Bash
  zsh          Generate completion script for Zsh
  fish         Generate completion script for Fish
  powershell   Generate completion script for PowerShell

Examples:
  # Bash
  source <(rmp completion bash)
  # Or save to file
  rmp completion bash > /etc/bash_completion.d/rmp

  # Zsh
  source <(rmp completion zsh)
  # Or save to file
  rmp completion zsh > "${fpath[1]}/_rmp"

  # Fish
  rmp completion fish > ~/.config/fish/completions/rmp.fish

  # PowerShell
  rmp completion powershell | Out-String | Invoke-Expression
`)
}

// CompleteRoadmapNames returns roadmap names for completion.
func CompleteRoadmapNames() []string {
	names, err := utils.ListRoadmaps()
	if err != nil {
		return nil
	}
	return names
}

// CompleteTaskIDs returns task IDs from the current/default roadmap.
func CompleteTaskIDs() []string {
	// Get current roadmap
	currentRoadmap, err := getCurrentRoadmap()
	if err != nil {
		return nil
	}

	// Open database
	dbConn, err := openDB(currentRoadmap)
	if err != nil {
		return nil
	}
	defer dbConn.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Get all tasks
	tasks, _, err := dbConn.ListTasks(ctx, nil, nil, nil, nil, nil, nil, 1, 1000)
	if err != nil {
		return nil
	}

	var ids []string
	for _, task := range tasks {
		ids = append(ids, fmt.Sprintf("%d", task.ID))
	}
	return ids
}

// CompleteSprintIDs returns sprint IDs from the current/default roadmap.
func CompleteSprintIDs() []string {
	// Get current roadmap
	currentRoadmap, err := getCurrentRoadmap()
	if err != nil {
		return nil
	}

	// Open database
	dbConn, err := openDB(currentRoadmap)
	if err != nil {
		return nil
	}
	defer dbConn.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Get all sprints
	sprints, err := dbConn.ListSprints(ctx, nil)
	if err != nil {
		return nil
	}

	var ids []string
	for _, sprint := range sprints {
		ids = append(ids, fmt.Sprintf("%d", sprint.ID))
	}
	return ids
}

// CompleteStatuses returns valid task statuses.
func CompleteStatuses() []string {
	return []string{
		"BACKLOG",
		"SPRINT",
		"DOING",
		"TESTING",
		"COMPLETED",
	}
}

// CompleteSprintStatuses returns valid sprint statuses.
func CompleteSprintStatuses() []string {
	return []string{
		"PENDING",
		"OPEN",
		"CLOSED",
	}
}

// openDB opens a database connection for the specified roadmap.
func openDB(roadmapName string) (*db.DB, error) {
	return db.Open(roadmapName)
}

// bashCompletionScript is the Bash completion script.
const bashCompletionScript = `# Bash completion for rmp
_rmp_completion() {
    local cur prev opts
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    # Main commands
    local commands="roadmap task sprint audit help"
    local roadmap_cmds="list create remove use"
    local task_cmds="list create get edit remove set-status set-priority set-severity"
    local sprint_cmds="list create get update remove start close reopen tasks stats add-tasks remove-tasks move-tasks"
    local audit_cmds="list history stats"

    # Complete based on position
    if [[ ${COMP_CWORD} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${commands}" -- ${cur}) )
        return 0
    fi

    # Complete subcommands
    local cmd="${COMP_WORDS[1]}"
    case "${cmd}" in
        roadmap|road)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${roadmap_cmds}" -- ${cur}) )
            elif [[ ${COMP_CWORD} -eq 3 && "${prev}" == "remove" || "${prev}" == "use" ]]; then
                # Complete roadmap names
                local roadmaps=$(rmp roadmap list 2>/dev/null | jq -r '.[].name' 2>/dev/null)
                COMPREPLY=( $(compgen -W "${roadmaps}" -- ${cur}) )
            fi
            ;;
        task|t)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${task_cmds}" -- ${cur}) )
            fi
            ;;
        sprint|s)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${sprint_cmds}" -- ${cur}) )
            fi
            ;;
        audit|aud)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${audit_cmds}" -- ${cur}) )
            fi
            ;;
    esac
}

complete -F _rmp_completion rmp
`

// zshCompletionScript is the Zsh completion script.
const zshCompletionScript = `#compdef rmp

# Zsh completion for rmp
_rmp() {
    local curcontext="$curcontext" state line
    typeset -A opt_args

    _arguments -C \
        '(-h --help)'{-h,--help}'[Show help]' \
        '(-v --version)'{-v,--version}'[Show version]' \
        '--verbose[Enable verbose logging]' \
        '1: :_rmp_commands' \
        '*:: :->args'

    case "$line[1]" in
        roadmap|road)
            _rmp_roadmap
            ;;
        task|t)
            _rmp_task
            ;;
        sprint|s)
            _rmp_sprint
            ;;
        audit|aud)
            _rmp_audit
            ;;
    esac
}

_rmp_commands() {
    local commands=(
        "roadmap:Manage roadmaps"
        "task:Manage tasks"
        "sprint:Manage sprints"
        "audit:View audit log"
        "help:Show help"
    )
    _describe -t commands 'rmp command' commands
}

_rmp_roadmap() {
    local subcmds=(
        "list:List all roadmaps"
        "create:Create a new roadmap"
        "remove:Remove a roadmap"
        "use:Set default roadmap"
    )
    _describe -t subcmds 'roadmap subcommand' subcmds
}

_rmp_task() {
    local subcmds=(
        "list:List tasks"
        "create:Create a task"
        "get:Get task details"
        "edit:Edit a task"
        "remove:Remove a task"
        "set-status:Set task status"
        "set-priority:Set task priority"
        "set-severity:Set task severity"
    )
    _describe -t subcmds 'task subcommand' subcmds
}

_rmp_sprint() {
    local subcmds=(
        "list:List sprints"
        "create:Create a sprint"
        "get:Get sprint details"
        "update:Update a sprint"
        "remove:Remove a sprint"
        "start:Start a sprint"
        "close:Close a sprint"
        "reopen:Reopen a sprint"
        "tasks:List sprint tasks"
        "stats:Get sprint stats"
        "add-tasks:Add tasks to sprint"
        "remove-tasks:Remove tasks from sprint"
        "move-tasks:Move tasks between sprints"
    )
    _describe -t subcmds 'sprint subcommand' subcmds
}

_rmp_audit() {
    local subcmds=(
        "list:List audit entries"
        "history:Get entity history"
        "stats:Get audit statistics"
    )
    _describe -t subcmds 'audit subcommand' subcmds
}

compdef _rmp rmp
`

// fishCompletionScript is the Fish completion script.
const fishCompletionScript = `# Fish completion for rmp

# Disable file completions for the rmp command
complete -c rmp -f

# Global options
complete -c rmp -s h -l help -d "Show help"
complete -c rmp -s v -l version -d "Show version"
complete -c rmp -l verbose -d "Enable verbose logging"

# Main commands
complete -c rmp -n "__fish_use_subcommand" -a "roadmap" -d "Manage roadmaps"
complete -c rmp -n "__fish_use_subcommand" -a "task" -d "Manage tasks"
complete -c rmp -n "__fish_use_subcommand" -a "sprint" -d "Manage sprints"
complete -c rmp -n "__fish_use_subcommand" -a "audit" -d "View audit log"
complete -c rmp -n "__fish_use_subcommand" -a "help" -d "Show help"

# Roadmap subcommands
complete -c rmp -n "__fish_seen_subcommand_from roadmap road" -a "list" -d "List all roadmaps"
complete -c rmp -n "__fish_seen_subcommand_from roadmap road" -a "create" -d "Create a new roadmap"
complete -c rmp -n "__fish_seen_subcommand_from roadmap road" -a "remove" -d "Remove a roadmap"
complete -c rmp -n "__fish_seen_subcommand_from roadmap road" -a "use" -d "Set default roadmap"

# Task subcommands
complete -c rmp -n "__fish_seen_subcommand_from task t" -a "list" -d "List tasks"
complete -c rmp -n "__fish_seen_subcommand_from task t" -a "create" -d "Create a task"
complete -c rmp -n "__fish_seen_subcommand_from task t" -a "get" -d "Get task details"
complete -c rmp -n "__fish_seen_subcommand_from task t" -a "edit" -d "Edit a task"
complete -c rmp -n "__fish_seen_subcommand_from task t" -a "remove" -d "Remove a task"
complete -c rmp -n "__fish_seen_subcommand_from task t" -a "set-status" -d "Set task status"
complete -c rmp -n "__fish_seen_subcommand_from task t" -a "set-priority" -d "Set task priority"
complete -c rmp -n "__fish_seen_subcommand_from task t" -a "set-severity" -d "Set task severity"

# Sprint subcommands
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "list" -d "List sprints"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "create" -d "Create a sprint"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "get" -d "Get sprint details"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "update" -d "Update a sprint"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "remove" -d "Remove a sprint"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "start" -d "Start a sprint"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "close" -d "Close a sprint"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "reopen" -d "Reopen a sprint"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "tasks" -d "List sprint tasks"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "stats" -d "Get sprint stats"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "add-tasks" -d "Add tasks to sprint"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "remove-tasks" -d "Remove tasks from sprint"
complete -c rmp -n "__fish_seen_subcommand_from sprint s" -a "move-tasks" -d "Move tasks between sprints"

# Audit subcommands
complete -c rmp -n "__fish_seen_subcommand_from audit aud" -a "list" -d "List audit entries"
complete -c rmp -n "__fish_seen_subcommand_from audit aud" -a "history" -d "Get entity history"
complete -c rmp -n "__fish_seen_subcommand_from audit aud" -a "stats" -d "Get audit statistics"
`

// powershellCompletionScript is the PowerShell completion script.
const powershellCompletionScript = `# PowerShell completion for rmp

$scriptBlock = {
    param($wordToComplete, $commandAst, $cursorPosition)

    $commands = @('roadmap', 'task', 'sprint', 'audit', 'help')
    $roadmapCmds = @('list', 'create', 'remove', 'use')
    $taskCmds = @('list', 'create', 'get', 'edit', 'remove', 'set-status', 'set-priority', 'set-severity')
    $sprintCmds = @('list', 'create', 'get', 'update', 'remove', 'start', 'close', 'reopen', 'tasks', 'stats', 'add-tasks', 'remove-tasks', 'move-tasks')
    $auditCmds = @('list', 'history', 'stats')

    $commandElements = $commandAst.CommandElements | Select-Object -ExpandProperty Value
    $command = $commandElements[1]

    switch ($command) {
        { $_ -in 'roadmap', 'road' } {
            $roadmapCmds | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        { $_ -in 'task', 't' } {
            $taskCmds | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        { $_ -in 'sprint', 's' } {
            $sprintCmds | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        { $_ -in 'audit', 'aud' } {
            $auditCmds | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        default {
            $commands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
    }
}

Register-ArgumentCompleter -CommandName rmp -ScriptBlock $scriptBlock
`
