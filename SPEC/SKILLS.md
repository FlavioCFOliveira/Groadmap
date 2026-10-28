# Agent Skills Specification

## Table of Contents

- [Overview](#overview)
- [Location and Identity](#location-and-identity)
- [Scope Partition](#scope-partition)
- [Invocation](#invocation)
- [Alignment Invariant](#alignment-invariant)
- [Packaging](#packaging)
- [Skills Installer](#skills-installer)
- [Skills Install Command](#skills-install-command)
- [Acceptance Criteria](#acceptance-criteria)

## Overview

The repository ships two Claude Code skills that teach an AI agent to operate
`rmp`. This specification defines where the skills live, which part of the CLI
each one operates, the invariant that keeps them aligned with the binary, how a
release publishes them, how `install-skills.sh` installs them from a release, and
how the command `rmp skills install` installs them from the repository's `main`
branch.

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
6. Each `SKILL.md` instructs the agent that reads it to run `rmp skills install`,
   and states when, in these terms:
   - after `rmp` has been installed or upgraded;
   - when the version the skill declares (`§ Version Declaration`) differs from
     the version `rmp --version` writes, which is the third whitespace-separated
     word of its line (`COMMANDS.md § Version`).

   It also states that the command is run once per such occasion. When the
   declared version still differs after a successful run, the cause is the
   installed binary, not the skills: the skills on `main` declare the version of
   the release `main` carries, so the agent reports that `rmp` itself must be
   upgraded and does not run the command again. `SKILL.md` carries the
   instruction as an invocation of `rmp skills install` written with canonical
   names (`§ Invocation`).

## Scope Partition

The CLI surface the skills operate is partitioned between them. The partition is
closed: every command of the contract is assigned below, and none is assigned to
both skills.

| Skill | Commands in scope (every subcommand of each) |
|-------|----------------------------------------------|
| `roadmap-manager` | `roadmap`, `task`, `sprint`, `backlog`, `stats`, `audit` |
| `knowledge-authority` | `graph` (its subcommands are `serve`, the server, and `client`, the client) |

Three further rules complete the partition:

1. **Shared references.** Two commands are shared by both skills and belong to
   neither one's scope:
   - `ai-help`: both skills MAY invoke `rmp ai-help`, and MAY name
     `rmp --ai-help` and `--help`, as the source of truth for the CLI surface.
   - `skills`: both skills MAY invoke `rmp skills install`, the command that
     installs them (`§ Skills Install Command`), and each `SKILL.md` invokes it
     as `§ Location and Identity` item 6 requires.

   A shared command is not in either skill's scope, so `§ Rules` rules 1 and 2
   do not require either skill to name its subcommands or its flags.
2. **Out of scope for both.** Neither skill invokes `rmp web`.
3. **Permitted invocations.** A skill's file set contains no invocation (see
   `§ Invocation`) of a command outside its own scope, other than `rmp ai-help`
   and `rmp skills install`.
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
| `rmp skills install` | Invocation of `rmp skills install` |
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
`§ Location and Identity` items 1, 2, 3, 5 and 6. `go test ./...` runs it, and
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
   - a `SKILL.md` contains no invocation of `rmp skills install` written with
     canonical names (`§ Location and Identity` item 6);
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
latest release for the invoking user. It is independent of the command
`rmp skills install`, which installs the skills of the `main` branch and is
specified in `§ Skills Install Command`; neither installer calls the other. Its
documented invocation is:

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

## Skills Install Command

`rmp skills install` installs the two skills from the repository's `main` branch.
The most current version of the skills is the one in the repository, so the
command fetches it from there rather than from a release, and every run replaces
the installed copy of each skill entirely. `COMMANDS.md § Skills Management` is
canonical for the command-line contract: its arguments, its output, its exit
codes, and the exact line it writes for each outcome. This section specifies the
behaviour behind that contract.

The command is non-interactive and unconditional:

1. It writes no prompt, reads nothing from standard input, and asks for no
   confirmation. It has no confirmation flag, such as `--yes` or `--force`; such
   a flag is an unknown flag and is refused as one.
2. It replaces the installed skills on every run, subject only to the refusals of
   `§ Tier Destination Rules`. It compares no installed version with the fetched
   one, and a run against skills already identical to the fetched ones replaces
   them all the same.
3. It never invokes `sudo` and never requests elevated privileges. It writes with
   the privileges it was started with, and a directory those privileges cannot
   write is a tier that fails (`§ Destination Tiers`).
4. It writes nothing outside the skills directory of each tier it attempts and its
   own staging directory (`§ Install Staging Directory`). Within a skills
   directory it reads and writes no entry other than the two skill paths and its
   own work directory, so every other skill in that directory is left untouched.
5. It does not require `-r` and does not accept it: it operates on no roadmap.

### Install Source

1. **The source is fixed.** The command fetches exactly one URL, the archive of
   the `main` branch:
   `https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz`.
   No flag and no environment variable changes it.
2. **The binary fetches it itself.** The request is made by the Go standard
   library's HTTP client inside the `rmp` binary. No external program is run:
   not `curl`, not `wget`, not `tar`.
3. **HTTPS only.** The request and every redirect it follows use the `https`
   scheme. A redirect to any other scheme is refused, and so is an eleventh
   redirect. Server certificates are verified against the system's trusted roots,
   and nothing disables that verification. The URL above answers with one
   redirect, to `codeload.github.com`, which serves the archive. The standard
   proxy environment variables are honoured, as the Go standard library's default
   transport honours them.
4. **Time bound.** The whole exchange — connection, redirects, and the reading of
   the whole response body — is bounded by 300 seconds. The archive is about
   6.3 MB, and 300 seconds still delivers the 64 MiB bound of item 6 over a link
   of 2 Mbit/s. A request that does not complete within the bound fails.
5. **Status.** Only a final response with HTTP status `200` is read. Any other
   final status fails the download.
6. **Size bounds.** Four bounds protect the disk and the process against an
   oversized or hostile response. Each leaves a wide margin over what the current
   archive needs, so ordinary growth of the repository does not reach it:

   | Bound | Value | Current archive |
   |-------|-------|-----------------|
   | Bytes of the response body (the compressed archive) | 64 MiB | about 6.3 MB |
   | Bytes of the decompressed tar stream | 256 MiB | about 23.4 MB |
   | Bytes of the regular files selected for extraction (`§ Install Archive Validation`) | 16 MiB | about 0.2 MB |
   | Entries selected for extraction | 1,000 | 17 |

   A response whose declared `Content-Length` exceeds the first bound is refused
   before its body is read. Every bound is also enforced on the bytes actually
   read, so a response that declares no length, or a false one, is stopped at the
   bound.
7. **No checksum exists.** GitHub publishes no checksum for a branch archive, and
   the archive of a branch changes with every commit to it, so no digest can be
   published beside it. The integrity of what is installed rests on two things
   only: the authenticated HTTPS connection to GitHub, and the structural
   validation of `§ Install Archive Validation`. The archive is read to its end,
   so the gzip trailer's CRC-32 and length checks also detect a truncated or
   corrupted stream; they authenticate nothing. `install-skills.sh`, which
   installs a release, verifies a published checksum
   (`§ Release Resolution and Download`); this command does not and cannot.

### Install Staging Directory

The archive is downloaded and validated once, before any tier is attempted, into
a private staging directory created for that one run. The validated tree is then
reused for each tier the run attempts.

1. **Where.** The staging directory is created inside the directory the Go
   standard library names as the default directory for temporary files: on a
   POSIX platform, the value of `TMPDIR` when it is set and not empty, and `/tmp`
   otherwise; on Windows, the directory the system's temporary-path lookup
   returns, which the `TMP`, `TEMP`, and `USERPROFILE` variables decide.
2. **Refusals before creation.** The run fails, with nothing downloaded and
   nothing installed, when that directory is not an absolute path, does not exist,
   or is not a directory, or, on a platform with POSIX file modes, when its
   permissions cannot be read or it is writable by every user and carries no
   sticky bit. These are the conditions of
   `DEPLOY.md § What the Script Refuses`, restated for a binary: another local
   user who could write to the parent could otherwise move the staging directory
   aside and put their own in its place.
3. **Creation.** The directory is created exclusively, with an unpredictable
   name that begins with `rmp-skills-install.`, so no other process can name it
   in advance and no existing directory is reused.
4. **Checks after creation.** On a platform with POSIX file modes, the created
   directory must not be a symbolic link, must be owned by the invoking user (the
   process's effective user id), and must have mode `0700` exactly. A created
   directory that fails a check is reported, left in place, and never removed, for
   the reason `DEPLOY.md § A Refused Directory Is Left in Place` gives.
5. **Removal.** The accepted staging directory is removed, with everything in
   it, on every way out of the command except a signal the process cannot handle.
   It holds only the fetched copy of the skills, never a previous one, so nothing
   in it needs to be kept after any outcome.

The archive itself is never written to disk. The response body is decompressed
and read as a tar stream as it arrives, and only the entries selected by
`§ Install Archive Validation` are written, into the staging directory. Nothing
leaves the staging directory until the whole stream has been read and has passed
every check, so a failure at any point of the download or of the validation
installs nothing at any tier.

### Install Archive Validation

The archive of `main` holds the whole repository under one top-level directory,
`Groadmap-main/`. The command extracts only the two skills from it.

1. **Global extended headers are skipped.** A pax global extended header (tar
   type `g`) is metadata about the archive, not an entry. GitHub writes one that
   carries the commit the archive was made from. It is neither validated as an
   entry nor extracted.
2. **Every entry name is checked.** For every other entry, whether or not item 3
   selects it, the run fails when the name begins with `/`, or when any of its
   `/`-separated components is `..`, `.`, or empty. The trailing `/` that ends a
   directory's name does not make an empty component. No archive of the
   repository that GitHub produces carries such a name, so its presence means the
   archive is not what the command expects.
3. **Selection.** An entry is selected when its name begins with
   `Groadmap-main/skills/roadmap-manager/` or
   `Groadmap-main/skills/knowledge-authority/`, or is one of those two names. Its
   **relative name** is its name with the prefix `Groadmap-main/skills/` removed.
   A selected entry any of whose relative components begins with `.` is not part
   of a skill's file set (`§ Location and Identity` item 5) and is skipped. Every
   entry that is not selected is ignored: it is read past, never written, and
   never type-checked, so a symbolic link elsewhere in the repository does not
   affect the run.
4. **Types.** A selected entry is a directory (tar type `5`) or a regular file
   (tar type `0`, or the legacy type written as a zero byte). Any other type — a
   symbolic link, a hard link, a character or block device, a FIFO, or any other
   type — fails the run.
5. **Bounds.** The selected entries are counted against the bounds of
   `§ Install Source` item 6.
6. **Extraction.** Each selected entry is written under the staging directory at
   its relative name. Every directory is created with mode `0700`, including a
   parent directory that a file's entry implies before the directory's own entry
   appears. Every regular file is created exclusively, without following a
   symbolic link, with mode `0600`, and its content is the entry's content. The
   modes are set explicitly, so the process umask does not alter them. The mode,
   owner, and timestamps the archive records for an entry are ignored. A selected
   name that names a file already written fails the run.
7. **Completeness.** After the stream has been read to its end, the staging
   directory holds a directory `roadmap-manager/` and a directory
   `knowledge-authority/`, and each holds a regular file `SKILL.md`. A missing
   skill or a missing `SKILL.md` fails the run.

Any failure in this section fails the whole run before any tier is attempted.

### Destination Tiers

Claude Code loads skills from several locations
(<https://code.claude.com/docs/en/skills>). The command tries three of them, in
this order, and stops at the first that installs both skills:

| Order | Tier | `scope` value | Skills directory |
|-------|------|---------------|------------------|
| 1 | Global | `global` | `<managed settings directory>/.claude/skills` |
| 2 | User | `user` | `<configuration directory>/skills`, resolved as `§ Destination` resolves it |
| 3 | Project | `project` | `<current working directory>/.claude/skills` |

1. **Global.** Claude Code loads enterprise skills, for every user of the
   machine, from `.claude/skills/<skill-name>/SKILL.md` inside the managed
   settings directory. That directory is fixed per operating system
   (<https://code.claude.com/docs/en/managed-settings>):

   | Platform | Managed settings directory |
   |----------|----------------------------|
   | Linux, including WSL | `/etc/claude-code` |
   | macOS | `/Library/Application Support/ClaudeCode` |
   | Windows | `C:\Program Files\ClaudeCode` |
   | FreeBSD, OpenBSD | None is documented |

   On a platform for which no managed settings directory is documented, the
   global tier fails without touching the filesystem, and the user tier is tried.
   Writing the managed settings directory normally requires the privileges of
   `root` or of an administrator. The command does not seek them: a run without
   them fails this tier at the first operation the operating system refuses, and
   moves on.
2. **User.** The skills directory of `§ Destination`, which `install-skills.sh`
   also uses: `$CLAUDE_CONFIG_DIR/skills` when `$CLAUDE_CONFIG_DIR` is set and not
   empty, and `<home directory>/.claude/skills` otherwise. The home directory is
   the one `ARCHITECTURE.md § Location Rules` rule 1 resolves: `HOME` on a POSIX
   platform and `USERPROFILE` on Windows. A home directory that is unset, empty,
   or not an absolute path never reaches the command, because that rule refuses
   the invocation at startup, before any tier is tried.
3. **Project.** Claude Code loads project skills from `.claude/skills/` in the
   directory a session is started in and in each parent directory up to the
   repository root. The project tier is that directory under the command's
   current working directory. A run started in a repository therefore writes into
   that repository's `.claude/skills/`, where the files are visible to version
   control.

**What makes a tier fail, and what follows.** A tier fails when its destination
is refused by `§ Tier Destination Rules`; when a directory the tier needs cannot
be examined, created, or written, for whatever reason the operating system gives;
or when its swap fails and the rollback of `§ Tier Replacement Procedure` item 5
restores the previous versions. A failed global or user tier is reported with a
one-line warning on standard error naming the tier and the reason, and the next
tier is tried. When the project tier fails as well, the run fails, and its error
line names the project tier's reason; the reasons of the other two are in the
warnings written before it. A tier whose rollback itself fails ends the run at
once, and no further tier is tried (`§ Tier Replacement Procedure` item 6).

**Precedence is Claude Code's, and the command does not act on it.** When two
locations hold a skill of the same name, Claude Code uses the enterprise copy over
the personal one, and the personal copy over the project one. The command never
removes or changes a copy at a tier it did not install into. A run that installs
into the user or the project tier therefore leaves in force any copy already
installed at a higher tier. The warnings, and the `skipped` member of the output
(`COMMANDS.md § Skills Install Output`), are how the reader learns that a higher
tier was not used.

### Tier Destination Rules

Before it creates anything at a tier, the command checks that tier's destination.
These are the rules of `§ Destination Rules`, restated for the binary, to which
the shell-specific parts of that section — the `error` helper and the reading of
permissions with `ls -ld` — do not apply. Each refusal fails the tier, with the
reason `COMMANDS.md § Skills Install Warnings` publishes for it, and leaves the
destination exactly as it was found.

The **configuration directory** of a tier is the directory that holds its skills
directory: `<managed settings directory>/.claude` for the global tier, the one
`§ Destination` resolves for the user tier, and
`<current working directory>/.claude` for the project tier. The **skill paths**
of a tier are `<skills directory>/roadmap-manager` and
`<skills directory>/knowledge-authority`. The **accepted owners** of a tier are
the invoking user, which is the process's effective user id, and, for the global
tier only, also `root` (user id `0`), because an enterprise skills directory is
normally owned by `root`.

1. For the user tier, `$CLAUDE_CONFIG_DIR` is set, not empty, and not an absolute
   path.
2. The configuration directory or the skills directory exists and is not a
   directory. These two checks follow a symbolic link to a directory.
3. The skills directory exists and its owner is not an accepted owner.
4. The skills directory exists and its permissions cannot be read, or it is
   writable by every user and carries no sticky bit.
5. A skill path exists and is a symbolic link. This check and the next do not
   follow a link.
6. A skill path exists and is not a directory, or its owner is not an accepted
   owner.

The ownership conditions of rules 3 and 6, and rule 4, apply on a platform with
POSIX file modes and owners. On Windows, which decides access by the
access-control list a file inherits from its directory rather than by mode bits
and a single owner, they are not checked, as for the graph store and its socket
(`GRAPH.md § Persistence Layout`, rule 4, and
`GRAPH.md § Socket Path and Permissions`); the operating system's own refusal of
a write is then what fails a tier the invoking user may not write.

When the configuration directory or the skills directory does not exist, the
command creates it; for the global tier, a missing managed settings directory is
created as well. Each directory it creates has the tier's directory mode
(`§ Tier Modes`). A directory created by an attempt that later fails is left in
place; it holds nothing the command wrote.

### Tier Modes

| Tier | Directories created | Files created |
|------|---------------------|---------------|
| Global | `0755` | `0644` |
| User | `0700` | `0600` |
| Project | `0700` | `0600` |

The global tier's skills are loaded for every user of the machine. A skill that
`root` installs there with mode `0700` or `0600` could be read by `root` alone,
and every other user's Claude Code would fail to load it, which would defeat the
tier. Its files are therefore readable by every user and writable by their owner
alone, and its directories are searchable by every user. The user and project
tiers serve one user, so their skills are private to that user, as
`§ Destination Rules` already requires of every directory `install-skills.sh`
creates. No tier makes a file executable: no file of either skill's file set is a
program.

The modes are applied to every directory and file the command writes into the
work directory of `§ Tier Replacement Procedure`, so a skill carries its tier's
modes from the moment it is moved into place. On Windows the modes are not
applied, for the reason `§ Tier Destination Rules` gives.

### Tier Replacement Procedure

For each tier it attempts, the command runs these steps. They are the steps of
`§ Replacement Procedure`, with the archive already extracted and validated in the
staging directory, so that each tier receives a copy of the same validated tree.

1. **Destination.** The destination is checked (`§ Tier Destination Rules`) and
   any missing directory is created.
2. **Work directory.** The command creates its work directory inside the skills
   directory, so that the work directory and the skill paths are on the same
   filesystem. It is created exclusively, with an unpredictable name that begins
   with `.rmp-skills-install.`, and mode `0700`. On a platform with POSIX file
   modes it must not be a symbolic link, must be owned by the invoking user, and
   must have mode `0700` exactly. A work directory that fails a check is left in
   place, never removed, and fails the tier.
3. **Copy.** The validated tree is copied from the staging directory into
   `<work directory>/new/`, with the tier's modes. Files are created exclusively
   and never through a symbolic link. The tree is copied rather than moved,
   because the staging directory may lie on another filesystem and a later tier
   may need it.
4. **Swap.** For each skill in turn, `roadmap-manager` and then
   `knowledge-authority`:
   1. When the skill path exists, it is renamed to `<work directory>/old/<skill>`.
      This is the removal of the previous version, made reversible.
   2. The command confirms that the skill path is now absent, and renames
      `<work directory>/new/<skill>` to the skill path.

   Each rename stays within one filesystem, so each moves a whole directory at
   once. Between steps 1 and 2 the skill path is briefly absent; it never holds
   part of one version and part of another. After both renames succeed, no file of
   a previously installed `roadmap-manager` or `knowledge-authority` remains under
   its skill path, including a file the new version no longer ships.
5. **Rollback.** When any step of the swap fails, the command undoes every
   completed step in reverse order: a newly installed skill is renamed back into
   the work directory, and each previous copy is renamed back to its skill path.
   Both skills at that tier are therefore left at their previous versions, or
   absent where no previous version existed. The tier has failed, its work
   directory is removed, and the next tier is tried.
6. **Failed rollback.** When an undo step itself fails, the run ends at once,
   with the error line `COMMANDS.md § Skills Install Error Cases` publishes for
   it. The line names the failed step and, for each previous copy that could not
   be restored, the path in the work directory that still holds it. The work
   directory is not removed, and no further tier is tried.
7. **Cleanup.** After both renames succeed, the command removes the work
   directory, the previous copies under `old/` included, then the staging
   directory, and then writes its output. On a tier that fails before its swap,
   the work directory, if one was created and accepted, is removed as well. A
   removal that fails is reported with a warning naming the directory, and does
   not change the outcome of the run.

A failure at any step before step 4 leaves the installed skills at that tier
unchanged.

### Install Interruption

The command handles `SIGINT` and `SIGTERM` itself for the part of its run that
holds state on disk, through the single signal registration of
`ARCHITECTURE.md § Modules and Responsibilities` item 11. It takes the action over
before it creates the staging directory and hands it back after the staging
directory has been removed.

1. A signal that arrives while the response is being read stops the download. A
   signal that arrives during a rename is acted on when that rename returns; a
   rename is never interrupted partway.
2. When a signal has been received, the command undoes every completed swap step
   of the tier in progress exactly as `§ Tier Replacement Procedure` item 5 does,
   removes the work directory and the staging directory, writes nothing to
   standard output, and exits with code `130`, the code an interrupted invocation
   of `rmp` exits with (`ARCHITECTURE.md § Exit Codes`). No further tier is tried.
3. When that undo fails, item 6 of the same section applies: the error line is
   written and the work directory is kept. The exit code is still `130`.
4. A signal that arrives after both renames of a tier have succeeded no longer
   undoes them: that tier's installation is complete. The command finishes its
   cleanup, writes nothing to standard output, and exits with code `130`.

A signal that arrives before the staging directory is created interrupts the
invocation as it interrupts any other short-lived `rmp` command.

### Install Verification

The command reaches the network and, run with sufficient privileges, writes the
machine's managed settings directory. No end-to-end test therefore runs the
compiled binary past the refusals that precede the download, such as an unknown
flag or a positional argument, and no published example that would reach the
network is executed (`DATA_FORMATS.md § Examples that cannot be run as published`).
The behaviour of this section is verified by Go tests of the package that
implements it. In place of the fixed source, the tests supply a local HTTPS server
serving archives the test builds; in place of the three tiers, directories the
test creates; and a point at which a chosen rename fails. These substitutions are
parameters of the package's own interface. The binary exposes none of them, and no
flag or environment variable of `rmp` changes the source or a tier.

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
19. Each `SKILL.md` contains an invocation of `rmp skills install` written with
    canonical names, and states the two occasions of `§ Location and Identity`
    item 6 and the rule that the command is not run again when the declared
    version still differs after a successful run. Removing that invocation from
    either `SKILL.md` makes the test of `§ Enforcement` fail, naming the skill and
    `rmp skills install`; adding `rmp skills install` to any file of either skill
    does not.
20. `rmp --ai-help` lists the command `skills` with its one subcommand
    `install`, and that entry's description states when an agent runs it
    (`HELP.md § Skills command help specifics`).
21. With a global skills directory the run may write, a run installs both skills
    under `<managed settings directory>/.claude/skills/`, exits `0`, writes the
    output of `COMMANDS.md § Skills Install Output` with `scope` equal to
    `global` and `skipped` empty, and leaves the user and project skills
    directories as they were. Each installed directory has mode `0755` and each
    installed file mode `0644`.
22. With a global skills directory the run may not write, and with
    `$CLAUDE_CONFIG_DIR` unset, a run installs both skills under
    `$HOME/.claude/skills/`, exits `0`, writes one warning naming the global tier,
    and reports `scope` equal to `user` with one `skipped` element whose `scope`
    is `global`. With `$CLAUDE_CONFIG_DIR` set to an absolute directory, it
    installs them under that directory's `skills/` instead and writes nothing
    under `$HOME/.claude/`. Each installed directory has mode `0700` and each
    installed file mode `0600`.
23. With the global tier unwritable and the user tier refused — for example a
    user skill path that is a symbolic link, or `$CLAUDE_CONFIG_DIR` set to a
    relative path — a run installs both skills under
    `<current working directory>/.claude/skills/`, exits `0`, writes the two
    warnings, and reports `scope` equal to `project` with two `skipped` elements,
    `global` then `user`. The refused user skill path is left exactly as it was
    found.
24. With all three tiers refused or unwritable, a run exits `1` with the
    all-tiers error line of `COMMANDS.md § Skills Install Error Cases`, after the
    two warnings, writes nothing to stdout, and leaves every installed skill at
    every tier byte-identical to its state before the run.
25. A download that fails — a refused connection, a final status other than
    `200`, an expired time bound, or a body over a bound of `§ Install Source`
    item 6 — and an archive that fails `§ Install Archive Validation` — an entry
    with a `..` component or an absolute name, a selected symbolic link or hard
    link, a missing `SKILL.md` — each exit `1`, attempt no tier, create no
    directory at any tier, and leave no staging directory behind.
26. A file present under `<skills directory>/roadmap-manager/` before a run, and
    absent from the fetched archive, is absent after a run that installs into
    that skills directory. A sibling skill directory, such as
    `<skills directory>/other-skill/`, is byte-identical before and after the run.
27. A failure injected at the second rename of a tier's swap leaves both skills at
    that tier at their previous versions, removes that tier's work directory, and
    tries the next tier. A failure injected in an undo step of the rollback exits
    `1` with the failed-rollback error line, which names the path that still
    holds each previous copy not restored; that work directory is kept, and no
    further tier is tried.
28. No way out of the command, including a refusal, a failure, a tier that falls
    through, and the `INT` and `TERM` signals, leaves the staging directory or a
    work directory behind, except the work directory of a failed rollback, a
    created directory that failed its checks, and a removal the operating system
    refused, which a warning names.
29. A signal delivered during a tier's swap leaves both skills at that tier at
    their previous versions and exits `130` with nothing on stdout.
30. A run with standard input closed, or attached to a terminal, completes without
    reading it and writes no prompt; `rmp skills install --yes` and
    `rmp skills install --force` each exit `2` with the unknown-flag line, before
    any request is made; a second successful run immediately after a first one
    replaces the skills again.
