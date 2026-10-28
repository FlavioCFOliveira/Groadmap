// Package commands — skills command: registry entry, help, and handler.
//
// `rmp skills install` installs the two agent skills the repository ships.
// Everything it does after its arguments are accepted belongs to
// internal/skillsinstall (SPEC/ARCHITECTURE.md § Modules and Responsibilities,
// module 12); this file parses the arguments, calls that package with the fixed
// source and tiers, and writes its result (SPEC/COMMANDS.md § Skills
// Management).
package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/FlavioCFOliveira/Groadmap/internal/skillsinstall"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// skillsWhenToRun is the rule SPEC/HELP.md § Skills command help specifics,
// item 1, requires the help and the contract to state.
const skillsWhenToRun = "Run `rmp skills install` after rmp has been installed or upgraded, and when the rmp-version a skill declares differs from the version `rmp --version` writes; run it once per such occasion. When the two still differ after a successful run, rmp itself must be upgraded, so the command is not run again."

// printSkillsHelp prints help for `rmp skills`.
func printSkillsHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp skills <subcommand>

Installs the two agent skills rmp ships, roadmap-manager and
knowledge-authority, where Claude Code loads them. Run `+"`rmp skills install`"+`
after rmp has been installed or upgraded, and when the rmp-version a skill
declares differs from the version `+"`rmp --version`"+` writes; run it once per such
occasion. When the two still differ after a successful run, rmp itself must be
upgraded, so the command is not run again. rmp skills does not take
-r/--roadmap.

Commands:
  install   Fetch the skills from the repository's main branch and replace the
            installed copy of each, in the first skills directory that accepts
            them

Options:
  -h, --help   Show this help message

Output (stdout JSON):
  install   {"scope", "directory", "skills", "source", "skipped"}

Exit codes:
  0   Success
  1   The skills could not be downloaded, failed validation, or could not be
      installed in any skills directory
  2   Unknown flag or unexpected positional argument
  127 Unknown subcommand

Examples:
  rmp skills install
`)
}

// printSkillsInstallHelp prints help for `rmp skills install`.
func printSkillsInstallHelp() {
	fmt.Fprint(helpDst(), `Usage: rmp skills install

Fetches the agent skills roadmap-manager and knowledge-authority from the
repository's main branch and installs them where Claude Code loads them.

When to run it: after rmp has been installed or upgraded, and when the
rmp-version a skill declares differs from the version `+"`rmp --version`"+` writes.
Run it once per such occasion. When the two still differ after a successful
run, rmp itself must be upgraded, so the command is not run again.

What it replaces: the installed roadmap-manager and knowledge-authority,
entirely, on every run. It writes no prompt, reads nothing from standard input,
and has no confirmation flag. Every other skill is left untouched.

Where it installs: it tries three skills directories, in this order, and uses
the first that accepts both skills:
  1. global    the managed settings directory's .claude/skills
               (/etc/claude-code/.claude/skills on Linux)
  2. user      $CLAUDE_CONFIG_DIR/skills, or ~/.claude/skills when
               CLAUDE_CONFIG_DIR is unset
  3. project   .claude/skills in the current working directory
Each directory tried and not used is reported with a warning on stderr. The
command never requests elevated privileges.

Where it fetches from: the archive of the repository's main branch, downloaded
over HTTPS from github.com, which needs network access to github.com. No
checksum exists for that archive.

It does not take -r/--roadmap.

Arguments: (none)
Options:   -h, --help

Output (stdout JSON):
  {
    "scope": "global" | "user" | "project",
    "directory": "<absolute path of the skills directory used>",
    "skills": ["roadmap-manager", "knowledge-authority"],
    "source": "https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz",
    "skipped": [{"scope": "<tier not used>", "reason": "<why>"}, ...]
  }
  "skipped" is [] when the first skills directory tried was used.

`+exitCodesBlock("skills", "install")+`Examples:
  rmp skills install
`)
}

// runSkillsInstall is the handler of `rmp skills install`. The shared arity
// point has already refused a positional argument; every flag other than the
// help tokens, which the dispatcher serves, is unknown — the command has no
// confirmation flag (SPEC/COMMANDS.md § Skills Install Options).
func runSkillsInstall(args []string) error {
	if err := rejectUnknownFlags(args); err != nil {
		return err
	}
	// The home directory is resolved, and refused when it is not an absolute
	// path, before anything is created (SPEC/ARCHITECTURE.md § Location Rules,
	// rule 1). The user tier lies under it.
	dataDir, err := utils.GetDataDir()
	if err != nil {
		return err
	}
	res, err := skillsinstall.Install(&skillsinstall.Options{
		Source: skillsinstall.SourceURL,
		Tiers:  skillsinstall.DefaultTiers(filepath.Dir(dataDir)),
		Warn: func(line string) {
			fmt.Fprintln(os.Stderr, line)
		},
	})
	if err != nil {
		return err
	}
	return utils.PrintJSON(res)
}

// Interrupted reports whether err is the interruption of `rmp skills install`
// by SIGINT or SIGTERM, which exits 130 with nothing on stdout. rollbackErr is
// the failed-rollback error the invocation still reports on stderr, or nil
// (SPEC/SKILLS.md § Install Interruption).
func Interrupted(err error) (rollbackErr error, ok bool) {
	var intr *skillsinstall.InterruptedError
	if !errors.As(err, &intr) {
		return nil, false
	}
	return intr.Err, true
}

// buildSkillsCommand registers the `rmp skills` family. Like `rmp web` it takes
// no -r/--roadmap flag: it opens no roadmap (SPEC/COMMANDS.md § Roadmap
// Selection (Always Required)).
func buildSkillsCommand() Command {
	return Command{
		Name:          "skills",
		Summary:       "Install the agent skills roadmap-manager and knowledge-authority for Claude Code.",
		Description:   "Installs the two agent skills rmp ships, roadmap-manager and knowledge-authority, where Claude Code loads them. " + skillsWhenToRun + " It does not take -r/--roadmap.",
		HelpPrinter:   printSkillsHelp,
		HasSubcommand: true,
		Subcommands: []Subcommand{
			{
				Name:        "install",
				Summary:     "Fetch the skills from the repository's main branch and replace the installed copy of each.",
				Description: skillsWhenToRun + " It downloads the archive of the repository's main branch from github.com over HTTPS, which needs network access to github.com; no checksum exists for that archive. It validates the two skills the archive carries and replaces the installed roadmap-manager and knowledge-authority entirely on every run, without a prompt, without reading standard input, and without a confirmation flag, leaving every other skill untouched. It tries three skills directories in order, the managed settings directory's .claude/skills (global), then $CLAUDE_CONFIG_DIR/skills or ~/.claude/skills (user), then .claude/skills in the current working directory (project), and uses the first that accepts both skills; each directory tried and not used is reported with a warning on stderr. It never requests elevated privileges. It does not take -r/--roadmap.",
				Usage:       "rmp skills install",
				HelpPrinter: printSkillsInstallHelp,
				Handler:     runSkillsInstall,
				Flags:       []Flag{helpFlag()},
				Output: SuccessOutput{
					Kind:    "object",
					Schema:  `{"scope": "global"|"user"|"project", "directory": "<absolute path>", "skills": ["roadmap-manager", "knowledge-authority"], "source": "<url>", "skipped": [<one element per skills directory tried and not used>]}, each element of skipped being {"scope": "<tier>", "reason": "<text>"}`,
					Example: `{"scope":"user","directory":"/home/alice/.claude/skills","skills":["roadmap-manager","knowledge-authority"],"source":"https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz","skipped":[{"scope":"global","reason":"cannot create /etc/claude-code: mkdir /etc/claude-code: permission denied"}]}`,
				},
				SideEffects: SideEffects{
					Database:   "Read-only (no roadmap database is opened).",
					Filesystem: "Creates a private staging directory in the temporary-file directory and removes it. In the first skills directory that accepts the skills, creates any missing directory, replaces <skills directory>/roadmap-manager and <skills directory>/knowledge-authority through a work directory inside it, and removes that work directory; every other entry of the skills directory is left untouched.",
					Network:    "Downloads https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz, the archive of the repository's main branch, from github.com over HTTPS.",
				},
				Idempotent: true,
				Prerequisites: []string{
					"Network access to github.com over HTTPS, to download the archive of the repository's main branch.",
				},
				ExitCodes: []ExitCodeEntry{
					ec(0, "Both skills were installed in one skills directory and the output was written to stdout; a warning on stderr names each skills directory tried before it."),
					ec(1,
						"The private staging directory could not be created or trusted.",
						"The archive could not be downloaded, exceeded a size bound, or failed validation; nothing was installed.",
						"No skills directory accepted the skills: the global, user and project skills directories were each refused or failed.",
						"A skills directory's replacement failed and its rollback failed too; the work directory that holds the previous copies is kept.",
						"The home directory is unset, empty, or not an absolute path.",
					),
					ec(2, condUnknownFlag, condExcessPositional),
				},
				Examples: []Example{
					{
						Title:  "Install the skills",
						Cmd:    "rmp skills install",
						Stdout: `{"scope":"user","directory":"/home/alice/.claude/skills","skills":["roadmap-manager","knowledge-authority"],"source":"https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz","skipped":[{"scope":"global","reason":"cannot create /etc/claude-code: mkdir /etc/claude-code: permission denied"}]}`,
						Exit:   0,
					},
					{
						Title:  "Confirmation flag",
						Cmd:    "rmp skills install --yes",
						Stderr: "Error: invalid input: unknown flag: --yes",
						Exit:   2,
					},
					{
						Title:  "Unexpected positional argument",
						Cmd:    "rmp skills install roadmap-manager",
						Stderr: `Error: invalid input: unexpected argument "roadmap-manager"`,
						Exit:   2,
					},
				},
			},
		},
	}
}
