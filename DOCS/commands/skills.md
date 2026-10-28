# skills

## Description

Install the two agent skills `rmp` ships, `roadmap-manager` and `knowledge-authority`, where Claude Code loads them. The command has one subcommand, `install`, which takes no alias.

**When to run it.** An agent runs `rmp skills install` on two occasions: after `rmp` has been installed or upgraded, and when the `rmp-version` a skill declares in its frontmatter differs from the version `rmp --version` writes, which is the third whitespace-separated word of its line. It is run once per such occasion. When the declared version still differs after a successful run, the cause is the installed binary, not the skills: the skills on the `main` branch declare the version of the release `main` carries. `rmp` itself must then be upgraded, and the command is not run again.

`rmp skills` operates on no roadmap, so it does **not** require and does **not** accept the `-r` / `--roadmap` flag. `rmp skills` with no subcommand writes the family help and exits `0`; an unresolved subcommand name exits `127`.

## Synopsis

```
rmp skills install
```

## Subcommands

### install

Fetches the skills from the repository's `main` branch and replaces the installed copy of each, entirely, in the first of three skills directories that accepts them.

**Non-interactive and unconditional.** The command writes no prompt, reads nothing from standard input, and asks for no confirmation. It has no confirmation flag: `--yes`, `--force` and every other flag but `--help` are unknown flags, refused with exit code `2` before anything is downloaded. It replaces the installed skills on every run, and a run against skills already identical to the fetched ones replaces them all the same. It never invokes `sudo` and never requests elevated privileges.

**Where it fetches from.** It downloads one URL, `https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz`, the archive of the repository's `main` branch, over HTTPS with the HTTP client built into the binary; no external program is run. The download needs network access to `github.com`, follows only `https` redirects, honours the standard proxy environment variables, and is bounded to 300 seconds. GitHub publishes no checksum for a branch archive, so none is verified: the integrity of what is installed rests on the authenticated HTTPS connection and on the structural validation of the archive. Four size bounds protect the disk and the process: 64 MiB of compressed archive, 256 MiB of decompressed stream, 16 MiB of selected skill files, and 1,000 selected entries.

**Validation before anything is installed.** The archive is read as it arrives into a private staging directory under the temporary-file directory, and only the files of the two skills are extracted. An entry with an absolute name or a `.`, `..` or empty path component, a selected entry that is neither a directory nor a regular file (a symbolic link, a hard link, a device, a FIFO), a selected name that appears twice, and a skill without a regular `SKILL.md` each fail the run before any skills directory is touched. Files whose path has a component beginning with `.` are not part of a skill and are skipped.

**Where it installs.** It tries three skills directories, in this order, and stops at the first that installs both skills:

| Order | `scope` | Skills directory | Modes of what it writes |
|-------|---------|------------------|-------------------------|
| 1 | `global` | `<managed settings directory>/.claude/skills`: `/etc/claude-code/.claude/skills` on Linux, `/Library/Application Support/ClaudeCode/.claude/skills` on macOS, `C:\Program Files\ClaudeCode\.claude\skills` on Windows; none is documented for FreeBSD and OpenBSD | directories `0755`, files `0644` |
| 2 | `user` | `$CLAUDE_CONFIG_DIR/skills` when `CLAUDE_CONFIG_DIR` is set and not empty, otherwise `~/.claude/skills` | directories `0700`, files `0600` |
| 3 | `project` | `.claude/skills` in the current working directory | directories `0700`, files `0600` |

Writing the managed settings directory normally needs `root` or an administrator; a run without those privileges fails the global tier at the first operation the operating system refuses and moves on. A tier is also refused, and left exactly as it was found, when its configuration directory, skills directory or a skill path is not a directory, when a skill path is a symbolic link, when the skills directory or a skill path is not owned by you (or, for the global tier, by `root`), or when the skills directory is writable by every user without the sticky bit. Each global or user tier not used is reported with a warning on stderr; when the project tier fails too, the run fails. Claude Code prefers an enterprise copy of a skill over a personal one, and a personal one over a project one, and the command never removes a copy at a tier it did not install into.

**Replacement and rollback.** Within the chosen skills directory the command reads and writes only `roadmap-manager`, `knowledge-authority` and its own work directory, named `.rmp-skills-install.` plus random characters; every other skill is left untouched. It copies the validated skills into the work directory, then, for each skill in turn, renames the previous copy into the work directory and the new copy into place. After a successful run no file of a previous version remains, including a file the new version no longer ships. When a rename fails, every completed step is undone, both skills are left at their previous versions, and the next tier is tried. When an undo itself fails, the run ends at once, the work directory that still holds each previous copy is kept, and the error line names where each one is.

**Interruption.** `SIGINT` or `SIGTERM` during the run stops the download, or undoes the swap in progress, removes the temporary directories, writes nothing to stdout, and exits `130`. A signal that arrives after both renames of a tier have succeeded leaves that installation in place.

**Usage:** `rmp skills install`

**Flags:**
| Short Flag | Long Flag | Type | Default | Description |
|------------|-----------|------|---------|-------------|
| `-h` | `--help` | bool | false | Show subcommand help |

`install` accepts no positional argument.

**Output:** a single JSON object on stdout, with exactly these keys in this order:

| Key | Type | Value |
|-----|------|-------|
| `scope` | string | The tier the skills were installed into: `global`, `user`, or `project` |
| `directory` | string | The absolute path of that tier's skills directory |
| `skills` | array of string | Always `["roadmap-manager", "knowledge-authority"]`, the order they were replaced |
| `source` | string | Always `https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz` |
| `skipped` | array of object | One `{"scope", "reason"}` per tier tried before the one used, in the order tried; `[]` when the first tier was used |

```json
{
  "scope": "user",
  "directory": "/home/alice/.claude/skills",
  "skills": [
    "roadmap-manager",
    "knowledge-authority"
  ],
  "source": "https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz",
  "skipped": [
    {
      "scope": "global",
      "reason": "cannot create /etc/claude-code: mkdir /etc/claude-code: permission denied"
    }
  ]
}
```

**Warnings (stderr):** a warning does not change the exit code, and the same warnings precede the error line of a run that then fails.

```
warning: the global skills directory was not used, trying the user skills directory: <detail>
warning: the user skills directory was not used, trying the project skills directory: <detail>
warning: the temporary directory <path> could not be removed: <detail>
```

**Examples:**
```bash
# Install, or reinstall, both skills
rmp skills install

# Install into a relocated configuration directory's skills/
CLAUDE_CONFIG_DIR=/srv/agents/config rmp skills install
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Both skills were installed in one skills directory and the output was written to stdout; warnings may precede it |
| 1 | The staging directory could not be created or trusted; the archive could not be downloaded, exceeded a bound, or failed validation; no skills directory accepted the skills; or a skills directory's replacement failed and its rollback failed too. A home directory that is unset, empty, or not an absolute path is refused before the command runs |
| 2 | Unknown flag, a confirmation flag such as `--yes` or `--force` included, or an unexpected positional argument |
| 127 | Unknown subcommand |

## Error Lines

| Scenario | Exit Code | stderr Output |
|----------|-----------|---------------|
| Unknown flag | 2 | `Error: invalid input: unknown flag: --foo` |
| Unexpected positional argument | 2 | `Error: invalid input: unexpected argument "X"` |
| The staging directory cannot be created or trusted | 1 | `Error: I/O error: cannot create a private staging directory: <detail>` |
| The final response has a status other than `200` | 1 | `Error: I/O error: cannot download the skills from https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz: HTTP status N` |
| The response body exceeds 64 MiB | 1 | `Error: I/O error: the skills archive exceeds 64 MiB; nothing was installed` |
| No skills directory accepted the skills | 1 | `Error: I/O error: the skills could not be installed in the global, user, or project skills directory; the project skills directory was not used: <detail>` |
| A replacement failed and could not be rolled back | 1 | `Error: I/O error: the skills replacement failed and could not be rolled back: <detail>` |

`SPEC/COMMANDS.md § Skills Install Error Cases` publishes every line the command writes.
