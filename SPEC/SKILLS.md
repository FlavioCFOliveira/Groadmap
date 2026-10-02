# Agent Skills Specification

## Table of Contents

- [Overview](#overview)
- [Location and Identity](#location-and-identity)
- [Scope Partition](#scope-partition)
- [Invocation](#invocation)
- [Alignment Invariant](#alignment-invariant)
- [Packaging](#packaging)
- [Skills Installer](#skills-installer)
- [Acceptance Criteria](#acceptance-criteria)

## Overview

The repository ships two Claude Code skills that teach an AI agent to operate
`rmp`. This specification defines where the skills live, which part of the CLI
each one operates, the invariant that keeps them aligned with the binary, how a
release publishes them, and how `install-skills.sh` installs them.

The AI Agent Contract that `rmp --ai-help` emits is the reference against which
the skills are judged. Its shape is specified in `DATA_FORMATS.md § AI Agent Contract`.

## Location and Identity

1. The directory `skills/` at the repository root holds exactly two skills, each
   in a directory of its own:
   - `skills/roadmap-manager/`
   - `skills/knowledge-authority/`

   No other entry exists in `skills/`, except entries whose name begins with `.`,
   which no rule of this specification reads or ships.
2. Each skill directory contains a file `SKILL.md`. It MAY contain a directory
   `references/` holding supporting files. `skills/roadmap-manager/` MAY also
   contain a file `PDS.md`.
3. `SKILL.md` begins with YAML frontmatter, the format Claude Code reads
   (<https://code.claude.com/docs/en/skills>): the first line of the file is
   `---`, and the frontmatter ends at the next line that is exactly `---`. The
   frontmatter carries:
   - `name`, equal to the name of the skill's directory;
   - `description`, a non-empty string;
   - the version declaration defined in `§ Version Declaration`.
4. The skills are project-agnostic. No file of either skill names a particular
   roadmap, refers to this repository's own workflow, or depends on a file of
   this repository. An example roadmap name in a skill is a placeholder such as
   `<roadmap>`, or a name that is evidently illustrative.
5. The **file set** of a skill is every regular file under its directory,
   recursively, excluding any path that has a component beginning with `.`.

## Scope Partition

The CLI surface the skills operate is partitioned between them. The partition is
closed: every command of the contract is assigned below, and none is assigned to
both skills.

| Skill | Commands in scope (every subcommand of each) |
|-------|----------------------------------------------|
| `roadmap-manager` | `roadmap`, `task`, `sprint`, `backlog`, `stats`, `audit` |
| `knowledge-authority` | `graph` (its subcommands are `serve`, the server, and `client`, the client) |

Three further rules complete the partition:

1. **Shared reference.** Both skills MAY invoke `rmp ai-help`, and MAY name
   `rmp --ai-help` and `--help`, as the source of truth for the CLI surface.
2. **Out of scope for both.** Neither skill invokes `rmp web`.
3. **Permitted invocations.** A skill's file set contains no invocation (see
   `§ Invocation`) of a command outside its own scope, other than `rmp ai-help`.
   In particular:
   - `roadmap-manager` contains no invocation of `rmp graph serve`,
     `rmp graph client`, or `rmp web`. It MAY state that the knowledge graph
     belongs to `knowledge-authority`, and MAY mention the command in a form that
     is not an invocation, such as `rmp graph …`.
   - `knowledge-authority` contains no invocation of `rmp roadmap`, `rmp task`,
     `rmp sprint`, `rmp backlog`, `rmp stats`, `rmp audit`, or `rmp web`, under
     any subcommand.

## Invocation

This section defines what counts as an invocation, so that the partition and the
alignment invariant can be checked mechanically.

### Where Invocations Are Recognised

- In a file of the file set whose name ends in `.md`, invocations are recognised
  only inside code: inside a fenced code block, and inside an inline code span
  outside a fenced code block. Prose is not scanned.
  - A fenced code block opens at a line that begins, after at most three spaces,
    with a run of three or more backticks or three or more tildes, and closes at
    the next line that begins, after at most three spaces, with a run of the same
    character at least as long. A block left open runs to the end of the file.
  - An inline code span is the text between a run of backticks and the next run
    of exactly as many backticks on the same line.
- In any other file of the file set, invocations are recognised in the whole
  text.

### What an Invocation Is

Within recognised text, an invocation is the following token sequence on one
line:

1. The word `rmp`: the three characters `rmp`, neither preceded nor followed by
   a letter, a digit, `_`, or `-`.
2. One or more spaces or tabs, then the **command token**: the longest run of
   characters matching `[a-z][a-z0-9-]*`.
3. The command token is resolved against the contract's `commands[]`, matching
   either an entry's `name` or one of its `aliases`.
   - When it resolves to a **single-action command**, the invocation is complete
     and is an invocation of `rmp <command>`. A single-action command is a
     `commands[]` entry whose `subcommands` array has exactly one element, whose
     `name` equals the command's own `name` (see
     `DATA_FORMATS.md § Single-action commands (no subcommands)`). These are
     `ai-help`, `stats`, and `web`.
   - Otherwise, the command token must be followed by one or more spaces or tabs
     and a **subcommand token**, the longest run of characters matching
     `[a-z][a-z0-9-]*`. The invocation is an invocation of
     `rmp <command> <subcommand>`.
4. Text that matches step 1 and step 2 but not the remainder of step 3 is a
   **command mention**, not an invocation. `rmp graph …`, `rmp graph`,
   `rmp task <subcommand>`, and `rmp task --help` are command mentions. Text in
   which `rmp` is followed by a token that does not begin with a lowercase letter
   is neither: `rmp --ai-help`, `rmp --help`, and `rmp <command>` are not
   invocations.

An invocation **resolves** when its command token names a `commands[]` entry, by
name or alias, and, for a command that is not single-action, its subcommand token
names one of that entry's `subcommands[]`, by name or alias. An invocation that
does not resolve names a command or subcommand the binary does not have.

Examples, against the current contract:

| Text inside a code span | Classification |
|-------------------------|----------------|
| `rmp task list -r <roadmap>` | Invocation of `rmp task list` |
| `rmp t ls -r <roadmap>` | Invocation of `rmp task list`, through aliases |
| `rmp stats -r <roadmap>` | Invocation of `rmp stats` (single-action) |
| `rmp graph client -r <roadmap> -q "MATCH (n) RETURN n"` | Invocation of `rmp graph client` |
| `rmp graph …` | Command mention |
| `rmp --ai-help` | Neither |
| `rmp graph execute` | Invocation that does not resolve |

## Alignment Invariant

The skills are always fully aligned with the binary built from the same commit.
Alignment is defined against the AI Agent Contract (`DATA_FORMATS.md § AI Agent Contract`):
the whole-CLI contract, whose `commands[]` lists every command, each with its
`subcommands[]`, and each subcommand with its `flags[]`, every flag carrying its
long form in `long`, including the `--` prefix.

### Rules

For each skill, with its scope as `§ Scope Partition` assigns it:

1. **Every subcommand is named.** For every command in the skill's scope and every
   element of that command's `subcommands[]`, the skill's file set contains an
   invocation of it written with canonical names: `rmp <command> <subcommand>`,
   or `rmp <command>` for a single-action command. An invocation written with an
   alias does not satisfy this rule.
2. **Every flag is named.** For every subcommand of every command in the skill's
   scope, every value of `flags[].long` appears in the skill's file set, anywhere
   in the text, prose included. A flag appears when its string occurs neither
   preceded nor followed by a letter, a digit, `_`, or `-`; `--type` is not
   satisfied by `--types`.
3. **Nothing absent is invoked.** Every invocation in the skill's file set
   resolves. A withdrawn or invented command or subcommand is never invoked.
4. **The partition holds.** The skill's file set contains no invocation that
   `§ Scope Partition` rule 3 forbids it.
5. **The version is declared and current.** The skill's `SKILL.md` carries the
   version declaration of `§ Version Declaration`, and the declared version
   equals the binary version: the string value of the `version` constant
   declared in `cmd/rmp/main.go`, which the contract publishes as
   `tool.binary_version`. Every change of the binary version therefore requires
   the skills to be reviewed against the contract of the new version and their
   declarations updated.

### Version Declaration

The declaration is a key of the free-form `metadata` map of the frontmatter,
which Claude Code accepts and ignores
(<https://code.claude.com/docs/en/skills>). Its fixed, machine-readable form is
two lines of the frontmatter:

```yaml
metadata:
  rmp-version: "1.0.0"
```

- The line `metadata:` stands at the start of a line, with no indentation.
- The line that declares the version follows it within the same `metadata` map,
  and matches exactly `^  rmp-version: "([0-9]+\.[0-9]+\.[0-9]+)"$`: two spaces,
  the key `rmp-version`, a colon, one space, and the version in double quotes,
  with no leading `v`.
- The frontmatter carries exactly one such line.

### Enforcement

A Go test in the package `internal/aihelp`, the package that builds the AI Agent
Contract, enforces every rule of this section and the structure of
`§ Location and Identity` items 1, 2, 3 and 5. `go test ./...` runs it, and
therefore so does `make check` and the `test` gate of `BUILD.md § Validation Gates`.

1. The test reads `skills/` from the repository, located relative to the package
   directory.
2. It obtains the commands, subcommands, aliases and flags from the whole-CLI
   contract the package itself generates, and it obtains the binary version by
   reading the `version` constant from the source of `cmd/rmp/main.go`.
3. It fails, with a message per violation, when:
   - `skills/` is missing, holds a skill directory other than the two, or lacks
     one of them;
   - a `SKILL.md` is missing, has no frontmatter, has a `name` that differs from
     its directory, has an empty `description`, or lacks a well-formed version
     declaration;
   - the declared version differs from the binary version;
   - any rule of `§ Rules` is violated.
4. Each message names the skill and its file set (`skills/<skill>/`), the rule
   violated, and the missing or forbidden item: the subcommand as
   `rmp <command> <subcommand>`, the long flag, the declared and expected
   versions, or, for a forbidden or unresolved invocation, the invocation text
   with the file and line where it occurs.
5. The test is not vacuous. It fails when the contract yields no command in
   either skill's scope, when it cannot read the binary version, and when a
   skill's file set is empty.

## Packaging

Each release publishes the skills as one archive and its checksum, alongside the
binary archives (`DEPLOY.md § Release Assets`).

1. **Name.** `rmp-skills-{version}.tar.gz`, where `{version}` is the release tag
   with its leading `v`, exactly as in the binary archive names
   (`DEPLOY.md § Binary Naming Convention`). Example: `rmp-skills-v1.0.0.tar.gz`.
2. **Format.** A gzip-compressed tar archive.
3. **Contents.** The archive is built from `skills/` of the tagged commit. Its
   top-level entries are exactly `roadmap-manager/` and `knowledge-authority/`,
   with no leading `./` and no other top-level entry. It holds the file set of
   each skill (see `§ Location and Identity` item 5) and the directories that
   contain it, and nothing else: no entry any of whose path components begins
   with `.`, no absolute path, no `..` component, and no entry that is not a
   directory or a regular file.
4. **Checksum.** The release publishes `rmp-skills-{version}.tar.gz.sha256`
   beside the archive, produced by `sha256sum` run from the directory that holds
   the archive, so the file has exactly the format `DEPLOY.md § What Is Published`
   specifies for a binary archive: the digest, two spaces, and the archive's base
   name with no directory component.
5. **Where it is built.** `.github/workflows/release.yml` builds the archive and
   its checksum into `dist/` in the job that creates the GitHub release, from that
   job's checkout of the tag, before the step that creates the release. The step
   fails the job when either skill directory is missing. Both files are then
   attached by the same step, and by the same rule, that attaches the binary
   archives and their checksums.

## Skills Installer

`install-skills.sh`, at the repository root, installs the two skills of the
latest release for the invoking user. Its documented invocation is:

```bash
curl -fsSL https://raw.githubusercontent.com/FlavioCFOliveira/Groadmap/main/install-skills.sh | bash
```

It is non-interactive, never invokes `sudo`, never writes outside the invoking
user's Claude Code configuration directory and its own staging directory, and
does not require `rmp` to be installed.

### Destination

Claude Code reads personal skills, available across all of a user's projects,
from `~/.claude/skills/<skill-name>/SKILL.md`
(<https://code.claude.com/docs/en/skills>). The environment variable
`$CLAUDE_CONFIG_DIR` overrides the configuration directory where Claude Code
stores, among other things, skills, and defaults to `~/.claude`
(<https://code.claude.com/docs/en/env-vars>). The installer therefore resolves:

1. The **configuration directory**: the value of `$CLAUDE_CONFIG_DIR` when it is
   set and not empty; otherwise `$HOME/.claude`.
2. The **skills directory**: `<configuration directory>/skills`.
3. The **skill paths**: `<skills directory>/roadmap-manager` and
   `<skills directory>/knowledge-authority`.

The installer honours `$CLAUDE_CONFIG_DIR` as it is set in the environment the
installer runs in. Claude Code reads the variable from the environment it is
started from, so skills installed under a relocated directory are visible to a
Claude Code started with the same value.

### Destination Rules

Before it requests any release asset, the installer checks the destination and
exits 1 through the `error` helper, writing nothing, when any of the following
holds:

| Condition | Why it is refused |
|-----------|-------------------|
| `$CLAUDE_CONFIG_DIR` is set, not empty, and not an absolute path | A relative value would install relative to whatever directory the script was started from, and a value beginning with `-` would reach `mkdir`, `ls` and `mv` as an option |
| `$CLAUDE_CONFIG_DIR` is unset or empty, and `$HOME` is unset, empty, or not an absolute path | There is no user configuration directory to install into |
| The configuration directory or the skills directory exists and is not a directory | There is nowhere to install |
| The skills directory exists and is not owned by the invoking user | It is not the user's own skills directory |
| The permissions of the skills directory cannot be read, or it is writable by every user and carries no sticky bit | Another local user could replace a skill, or the installer's work directory, inside it |
| A skill path exists and is a symbolic link | Replacing it would replace whatever the link points to, or leave the link in place |
| A skill path exists and is not a directory, or is not owned by the invoking user | It is not a skill directory the user owns, and the installer does not remove what it cannot attribute to the user |

The checks on the configuration directory and the skills directory follow a
symbolic link to a directory; the checks on a skill path do not. Permissions are
read with `ls -ld`, for the reasons `DEPLOY.md § Tools the Staging Directory Requires`
gives.

When the configuration directory or the skills directory does not exist, the
installer creates it, and every directory it creates has mode `0700`.

The installer reads and writes no entry of the skills directory other than the
two skill paths and its own work directory (see `§ Replacement Procedure`). Every
other skill in the directory is left untouched.

### Release Resolution and Download

1. **Latest release.** The installer resolves the latest release exactly as
   `install.sh` does: the `tag_name` of the GitHub API's latest-release response
   for the repository, fetched with `curl`, or with `wget` when `curl` is absent.
   A tag that cannot be fetched fails the run with exit 1.
2. **No version short-circuit.** The installer installs the latest release on
   every run. It does not compare any installed version with the latest release,
   and a run against skills already at the latest release replaces them all the
   same.
3. **Download.** The installer downloads
   `https://github.com/FlavioCFOliveira/Groadmap/releases/download/{version}/rmp-skills-{version}.tar.gz`
   and the checksum file at the same URL with `.sha256` appended.
4. **Staging.** Both files are staged in a private directory created and checked
   under exactly the rules of `DEPLOY.md § Staging Directory`, with the template
   `${TMPDIR:-/tmp}/rmp_skills_install.XXXXXXXXXX`. Every refusal of
   `DEPLOY.md § What the Script Refuses` applies unchanged, and so does
   `DEPLOY.md § A Refused Directory Is Left in Place`.
5. **Verification.** The archive is verified against its checksum file before it
   is read in any other way, under exactly the rules of
   `DEPLOY.md § How the Script Verifies`: the digest is taken from the line whose
   name field matches the archive, both digests are 64 hexadecimal characters, and
   the digest is computed with the first available tool of the three that section
   lists. There is no flag that disables verification, for the reasons
   `DEPLOY.md § Failure Modes` gives.

### Archive Validation

After verification and before any skill is replaced, the installer validates the
archive in two stages. Any failure exits 1 through the `error` helper, with the
installed skills unchanged.

1. **Before extraction**, from the list of entry names the archive reports:
   - no name begins with `/`;
   - no name has a `..` component;
   - every name lies under `roadmap-manager/` or `knowledge-authority/`, with no
     leading `./`.
2. **After extraction** into the work directory:
   - both `roadmap-manager/` and `knowledge-authority/` exist as directories, and
     each contains a regular file `SKILL.md`;
   - the extracted tree contains no symbolic link;
   - every extracted entry is a directory or a regular file, and no regular file
     has more than one link.

### Replacement Procedure

Every run replaces each of the two skills entirely with the version in the
latest release. After a successful run, no file of a previously installed
`roadmap-manager` or `knowledge-authority` remains under its skill path,
including a file the new version no longer ships. No step leaves a skill path
holding part of one version and part of another.

1. **Work directory.** After the archive is verified, the installer creates its
   work directory inside the skills directory, so that the work directory and the
   skill paths are on the same filesystem, with
   `mktemp -d "<skills directory>/.rmp-skills-install.XXXXXXXXXX"`. The work
   directory is checked by the same rules as the staging directory
   (`DEPLOY.md § What the Script Refuses`, last two rows): not a symbolic link,
   owned by the invoking user, and mode `rwx------`.
2. **Extraction.** The archive is extracted into `<work directory>/new/` and
   validated there (see `§ Archive Validation`).
3. **Swap.** For each skill in turn, `roadmap-manager` and then
   `knowledge-authority`:
   1. When the skill path exists, it is renamed to `<work directory>/old/<skill>`.
   2. The installer confirms that the skill path is now absent, and renames
      `<work directory>/new/<skill>` to the skill path.

   Each rename is a `mv` within one filesystem, so each moves a whole directory
   at once. Between steps 1 and 2 the skill path is briefly absent; it never
   holds a partial copy.
4. **Rollback.** When any step of the swap fails, the installer undoes every
   completed step in reverse order: a newly installed skill is renamed back into
   the work directory, and each previous copy is renamed back to its skill path.
   Both skills are therefore left at their previous versions, or absent where no
   previous version existed. The installer then exits 1 through the `error`
   helper.
5. **Failed rollback.** When an undo step itself fails, the installer reports
   each previous copy that could not be restored together with the path in the
   work directory that still holds it, and does not remove the work directory.
6. **Cleanup.** On every other path out of the script, the `EXIT` trap removes
   the staging directory and the work directory, the previous copies under
   `old/` included. The signal traps of `DEPLOY.md § When the Directory Is Removed`
   apply unchanged, and an interruption that arrives while a swap is in progress
   performs the rollback of item 4 before cleanup.

A failure at any point before step 3 leaves the installed skills unchanged.

### Tools Required

| Tool | Used for |
|------|----------|
| `curl` or `wget` | Resolving the latest release and downloading the archive and its checksum |
| `sha256sum`, `shasum`, or `openssl` | Verifying the archive (`DEPLOY.md § How the Script Verifies`) |
| `mktemp` | Creating the staging directory and the work directory |
| `ls` | Reading permissions and ownership checks (`DEPLOY.md § Tools the Staging Directory Requires`) |
| `tar` with gzip support | Listing and extracting the archive |
| `find` | Inspecting the extracted tree |
| `mkdir`, `mv`, `rm` | Creating the destination, swapping, and cleanup |

The absence of a downloader, of every SHA-256 tool, of `mktemp`, or of `tar`
fails the run before any release asset is requested.

### Diagnostic Output

The installer writes every diagnostic to standard error through the helpers
`info`, `success`, `warn`, and `error`, with the prefixes, the colouring, and both
rules of `DEPLOY.md § Diagnostic Output`. It writes no prompt.

- On success, the last line is the `success` message
  `Installed roadmap-manager and knowledge-authority {version} in {skills directory}`,
  where `{version}` is the release tag with its leading `v` and
  `{skills directory}` is the resolved skills directory. The installer then exits
  0.
- On every failure it detects, the installer exits 1. A signal ends the run with
  the exit code `DEPLOY.md § When the Directory Is Removed` assigns to it.

### Installer Failure Modes

Each of the following exits 1, reports through the `error` helper, and leaves
the installed skills unchanged, except where the last row states otherwise.

| Condition | When it is detected | Behaviour |
|-----------|---------------------|-----------|
| Neither `curl` nor `wget` is available | Before anything is fetched | Refused with the message `Neither curl nor wget is available. Please install one of them.` |
| No SHA-256 tool, no `mktemp`, or no `tar` | Before anything is fetched | Refused, naming the missing tool; for the SHA-256 case, naming all three accepted tools |
| An unsafe or unusable destination (`§ Destination Rules`) | Before anything is fetched | Refused, naming the path and the condition; nothing is created |
| The latest release cannot be resolved | Before any release asset is requested | Refused |
| A staging directory that cannot be created or trusted | Before any release asset is requested | Refused under `DEPLOY.md § What the Script Refuses` |
| The release publishes no skills archive, or the archive cannot be downloaded | At download | Refused with the message `release {version} publishes no downloadable skills archive rmp-skills-{version}.tar.gz. Nothing was installed.` |
| The checksum file cannot be downloaded, names no well-formed digest for the archive, or the digests differ | At verification | Refused under `DEPLOY.md § Failure Modes`; the archive is never extracted |
| The archive fails `§ Archive Validation` | Before the swap | Refused, naming the offending entry or the missing skill |
| A rename of the swap fails | During the swap | Rolled back under `§ Replacement Procedure` item 4, and the failed rename is named; when the rollback itself fails, item 5 applies and the work directory is kept |

## Acceptance Criteria

1. `skills/` at the repository root holds exactly the directories
   `roadmap-manager/` and `knowledge-authority/`, each with a `SKILL.md` whose
   frontmatter carries `name` equal to the directory name, a non-empty
   `description`, and exactly one version declaration in the form of
   `§ Version Declaration`.
2. Reading the file set of either skill shows no particular roadmap name and no
   reference to this repository's own workflow or files.
3. The test of `§ Enforcement` exists in the package `internal/aihelp`, runs under
   `go test ./...`, and passes on the tree.
4. Removing every invocation of `rmp sprint reorder` from `roadmap-manager` makes the
   test fail with a message naming `roadmap-manager`, `skills/roadmap-manager/`,
   and `rmp sprint reorder`.
5. Removing every occurrence of `--socket` from `knowledge-authority` makes the
   test fail with a message naming the skill and `--socket`.
6. Adding the inline code span `rmp graph execute` to either skill makes the test
   fail, naming the unresolved invocation with its file and line.
7. Adding the inline code span `rmp graph client -r <roadmap>` to
   `roadmap-manager`, or `rmp task list -r <roadmap>` to `knowledge-authority`,
   makes the test fail, naming the forbidden invocation; adding `rmp web` to
   either does the same.
8. Adding the text `rmp graph …` in a code span to `roadmap-manager`, or
   `rmp ai-help` to either skill, does not make the test fail.
9. Changing the `version` constant of `cmd/rmp/main.go` without changing the
   version declarations makes the test fail, naming both versions.
10. A published release carries `rmp-skills-{version}.tar.gz` and
    `rmp-skills-{version}.tar.gz.sha256`. `tar -tzf` on the archive lists entries
    under exactly `roadmap-manager/` and `knowledge-authority/`, with no `./`
    prefix, no component beginning with `.`, and no link; `sha256sum -c` on the
    checksum file, run beside the archive, prints `OK`.
11. With `$CLAUDE_CONFIG_DIR` unset, a successful run of `install-skills.sh`
    installs both skills under `$HOME/.claude/skills/`, exits 0, and writes the
    final `success` line of `§ Diagnostic Output`. With `$CLAUDE_CONFIG_DIR` set
    to an absolute directory, it installs them under that directory's `skills/`
    instead and writes nothing under `$HOME/.claude/`.
12. A file present under `<skills directory>/roadmap-manager/` before a run, and
    absent from the release's archive, is absent after the run. A sibling skill
    directory, such as `<skills directory>/other-skill/`, is byte-identical before
    and after the run.
13. A skill path that is a symbolic link, or a directory owned by another user, is
    refused with exit 1 before any release asset is requested, and is left
    exactly as it was found.
14. A checksum mismatch, a missing checksum file, or an archive carrying an entry
    with a `..` component, an absolute name, or a symbolic link exits 1 and leaves
    both installed skills byte-identical to their state before the run.
15. A run against a release that publishes no skills archive exits 1 with the
    message of `§ Installer Failure Modes` and installs nothing.
16. A failure injected at the second rename of the swap leaves both skills at
    their previous versions, and the run exits 1.
17. No exit path, including a refusal, a failure, and the `HUP`, `INT`, and `TERM`
    signals, leaves the staging directory or the work directory behind, except
    the failed rollback of `§ Replacement Procedure` item 5.
18. Reading `install-skills.sh` shows no invocation of `sudo`, no interactive
    prompt, and no flag that disables checksum verification.
