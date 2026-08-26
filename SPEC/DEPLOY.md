# Deployment and Installation Specification

## Table of Contents

- [Overview](#overview)
- [Data Location](#data-location)
- [Installation Methods](#installation-methods)
- [Platform Detection](#platform-detection)
- [Installation Script Reference](#installation-script-reference)
- [Checksum Verification](#checksum-verification)
- [Release Process](#release-process)
- [Acceptance Criteria](#acceptance-criteria)

## Overview

This specification defines the deployment process, installation methods, and platform detection for the Groadmap CLI.

## Data Location

The `rmp` binary installs to a system location (default `/usr/local/bin`). Its runtime data is stored separately, per user, under the data directory `~/.roadmaps/` (mode `0700`).

- Each roadmap occupies its own home directory `~/.roadmaps/<name>/` (mode `0700`), containing the SQLite database `project.db` (mode `0600`) and its sidecars.
- The data directory persists across binary upgrades and reinstalls; installing or removing the binary does not create, move, or delete roadmap data.
- On first run after upgrading from a build that used the legacy `~/.roadmaps/<name>.db` layout, the binary automatically migrates existing roadmaps to the current layout. The migration moves data in place and does not require user action. It is specified in `ARCHITECTURE.md § Filesystem Layout Migration`.

The full data directory layout and permission model are specified in `ARCHITECTURE.md § Directory Structure`.

The `rmp web` command (read-only web interface, see `WEB.md`) reads this same data and installs no new on-disk artefact: its HTML templates, static assets, and vendored graph library are embedded in the binary, so the runtime data footprint under `~/.roadmaps/` is unchanged. Serving a graph request may create the graph store's own lock file inside an existing `graph/` directory, which is part of the store rather than a deployment artefact (see `GRAPH.md § What a Read Changes on Disk`).

## Installation Methods

### 1. Automated Installation Script (Recommended)

**Location:** `install.sh` in repository root

**Usage:**
```bash
curl -fsSL https://raw.githubusercontent.com/FlavioCFOliveira/Groadmap/main/install.sh | bash
```

**Features:**
- Automatic platform detection
- Architecture detection (including ARM variants)
- Raspberry Pi detection
- Downloads latest release binary
- Verifies the downloaded archive against the SHA-256 checksum published beside
  it, before extracting anything (see Checksum Verification)
- Installs to `/usr/local/bin` by default
- Supports custom installation directory

### 2. Manual Installation

Download binary from GitHub releases. The checksum step is not optional
decoration: it is the same check the installation script performs, and it is
what this method would otherwise lack (see Checksum Verification).

```bash
# Download for your platform, and the checksum published beside it
curl -LO https://github.com/FlavioCFOliveira/Groadmap/releases/download/v1.0.0/rmp-v1.0.0-linux-amd64.tar.gz
curl -LO https://github.com/FlavioCFOliveira/Groadmap/releases/download/v1.0.0/rmp-v1.0.0-linux-amd64.tar.gz.sha256

# Verify before extracting. Stop here if this does not print OK.
sha256sum -c rmp-v1.0.0-linux-amd64.tar.gz.sha256      # Linux
shasum -a 256 -c rmp-v1.0.0-linux-amd64.tar.gz.sha256  # macOS

# Extract
tar -xzf rmp-v1.0.0-linux-amd64.tar.gz

# Install
sudo mv rmp /usr/local/bin/
```

### 3. Build from Source

```bash
git clone https://github.com/FlavioCFOliveira/Groadmap.git
cd Groadmap
go build -o rmp ./cmd/rmp
sudo mv rmp /usr/local/bin/
```

## Platform Detection

### Operating System Detection

The installation script detects the operating system via `uname -s` and maps it
to the `{goos}` half of an archive name:

| `uname -s` | `{goos}` |
|------------|----------|
| `Linux*` | linux |
| `Darwin*` | darwin |
| `FreeBSD*` | freebsd |
| `OpenBSD*` | openbsd |
| `CYGWIN*`, `MINGW*`, `MSYS*` | windows |

Every operating system in `BUILD.md § Supported Build Targets` MUST appear here.
A release that ships an archive the script cannot ask for is a release that
cannot be installed on the platform it was built for.

**Unsupported operating systems:** any `uname -s` not listed above is not a
platform the build produces. `detect_os()` returns `unknown` for it, and the
script rejects that value: it calls the `error` helper with the message

```
operating system {uname} is not supported. Supported systems: linux, darwin, freebsd, openbsd, windows. See SPEC/BUILD.md for the build matrix.
```

and exits with code 1. `{uname}` is the raw `uname -s` output for the host, the
string the machine reported, not a mapped name. The resulting line on standard
error is `ERROR: operating system {uname} is not supported. ...` (see
Diagnostic Output).

### Architecture Detection

The installation script detects architecture via `uname -m`:

| Output | Architecture | Binary Target |
|--------|--------------|---------------|
| x86_64, amd64 | amd64 | {goos}-amd64 |
| arm64, aarch64 | arm64 | {goos}-arm64 |
| armv6l, armv6 | armv6 | {goos}-armv6 |
| armv7l, armv7 | armv7 | {goos}-armv7 |

**Unsupported architectures:** 32-bit x86 (`i386`, `i686`) and any other
architecture not listed above are not produced by `BUILD.md`. `detect_arch()`
returns `unsupported` for an architecture it recognises but the build does not
produce, and `unknown` for one it does not recognise at all. **The script MUST
reject both values**, because neither can name an existing release asset. It
calls the `error` helper with the message

```
architecture {uname} is not supported. Supported targets: amd64, arm64, armv6, armv7. See SPEC/BUILD.md for the build matrix.
```

and exits with code 1. `{uname}` is the raw `uname -m` output for the host — the
string the machine reported, such as `i686` — and never the mapped architecture
name, because the mapping is precisely what failed. The resulting line on
standard error is `ERROR: architecture i686 is not supported. ...` (see
Diagnostic Output).

Rejecting these values is what keeps the failure early and legible. An
unsupported architecture that reaches the download step instead produces a
confusing failure fetching a release asset that was never built.

### ARM Variant Detection

For generic ARM (`arm*` fallback), the script attempts to determine the specific ARM version:

```bash
# Check /proc/cpuinfo for ARM version
if grep -q "ARMv7" /proc/cpuinfo 2>/dev/null; then
    arch="armv7"
elif grep -q "ARMv6" /proc/cpuinfo 2>/dev/null; then
    arch="armv6"
else
    # Default to armv6 for compatibility (lowest common denominator)
    arch="armv6"
fi
```

### Raspberry Pi Detection

The script can detect if running on a Raspberry Pi:

```bash
is_raspberry_pi() {
    if [ -f /proc/device-tree/model ]; then
        grep -q "Raspberry Pi" /proc/device-tree/model 2>/dev/null
        return $?
    elif [ -f /proc/cpuinfo ]; then
        grep -q "BCM28" /proc/cpuinfo 2>/dev/null
        return $?
    fi
    return 1
}
```

**Detection Methods:**
1. Check `/proc/device-tree/model` for "Raspberry Pi" string
2. Check `/proc/cpuinfo` for Broadcom BCM28xx chip

## Installation Script Reference

### Diagnostic Output

The script writes every diagnostic to standard error through one of five
helpers. Each helper prints a fixed uppercase level prefix, a colon, a space,
and then the message it was given:

| Helper | Line written to standard error |
|--------|--------------------------------|
| `info` | `INFO: {message}` |
| `success` | `SUCCESS: {message}` |
| `warn` | `WARNING: {message}` |
| `error` | `ERROR: {message}` |
| `prompt` | `PROMPT: {message}` |

Two rules follow, and they govern every message this specification quotes:

1. **No error path may bypass the `error` helper.** Every failure the script
   reports goes through it, so every error line on standard error begins with
   the same prefix.
2. **A quoted message is the `{message}` argument, never the whole line.** The
   helper supplies the `ERROR: ` prefix, so this specification never repeats it.
   A message specified as `architecture i686 is not supported.` reaches standard
   error as the line `ERROR: architecture i686 is not supported.`

The level prefix is wrapped in ANSI colour escape sequences, so the raw bytes on
standard error carry escape codes around the prefix. A test that asserts an exact
message MUST compare the message text rather than the raw prefix bytes.

### Functions

#### `detect_arch()`
Returns the architecture string for the current system.

**Returns:**
- `amd64` - x86_64 systems
- `arm64` - 64-bit ARM systems
- `armv6` - ARMv6 systems (Pi Zero/1)
- `armv7` - ARMv7 systems (Pi 2/3/4 32-bit)
- `unsupported` - Architecture detected but not produced by the build (e.g., `i386`, `i686`); the script rejects it and exits 1
- `unknown` - Unrecognized architecture string; the script rejects it and exits 1

Both values are rejected by the same guard, with the same message and the same
exit code (see Architecture Detection).

#### `is_raspberry_pi()`
Detects if running on Raspberry Pi hardware.

**Returns:**
- `0` (true) - Running on Raspberry Pi
- `1` (false) - Not running on Raspberry Pi

#### `get_download_url(version, arch)`
Constructs the download URL for a specific version and architecture.

**Parameters:**
- `version` - Release version (e.g., "v1.0.0")
- `arch` - Architecture string from `detect_arch()`

**Returns:** GitHub release asset URL

### Download URL Format

```
https://github.com/FlavioCFOliveira/Groadmap/releases/download/{version}/rmp-{version}-{os}-{arch}.{ext}
```

**Examples:**
- Linux AMD64: `rmp-v1.0.0-linux-amd64.tar.gz`
- macOS ARM64: `rmp-v1.0.0-darwin-arm64.tar.gz`
- Windows AMD64: `rmp-v1.0.0-windows-amd64.zip`
- Raspberry Pi ARMv6: `rmp-v1.0.0-linux-armv6.tar.gz`

## Checksum Verification

The installation script verifies every archive it downloads against the SHA-256
checksum the release publishes beside it, and it does so **before** the archive
is extracted. Nothing that fails verification reaches `tar`, `unzip`, `chmod`, or
the installation directory.

### What Is Published

`.github/workflows/release.yml`, step `Generate checksum`, runs
`sha256sum <archive> > <archive>.sha256` from inside `dist/`, and the release job
attaches every `dist/*.sha256` as a release asset of its own. The checksum for an
archive therefore always sits at the archive's own download URL with `.sha256`
appended:

```
https://github.com/FlavioCFOliveira/Groadmap/releases/download/{version}/rmp-{version}-{os}-{arch}.{ext}.sha256
```

The file holds GNU `sha256sum` text-mode output: 64 lowercase hexadecimal
characters, two spaces, and the archive's base name, followed by a newline.
Because the workflow runs `sha256sum` from inside `dist/`, the recorded name
never carries a directory component:

```
2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae  rmp-v1.0.0-linux-amd64.tar.gz
```

### How the Script Verifies

1. The archive is downloaded.
2. `<archive>.sha256` is downloaded from the same release.
3. The digest is read from the line of that file whose **name field matches the
   archive being installed**. The digest is never taken from a fixed position, so
   a checksum file describing some other archive is rejected rather than
   compared against this one. A leading `*` on the name, which `sha256sum` writes
   in binary mode, is accepted.
4. The digest of the downloaded file is computed and the two are compared.

Both digests must be exactly 64 hexadecimal characters. A field that is empty,
truncated, or not hexadecimal is rejected as malformed rather than compared, so
an error page served with a 200 status can never be read as a digest that
happens to match.

The script computes the digest with the first of these tools it finds:

| Order | Tool | Where it comes from |
|-------|------|---------------------|
| 1 | `sha256sum` | GNU coreutils on Linux; FreeBSD ships a GNU-compatible `sha256sum(1)` in base |
| 2 | `shasum -a 256` | The macOS base system (Perl's `Digest::SHA`) |
| 3 | `openssl dgst -sha256 -r` | OpenSSL and LibreSSL, carried in base by FreeBSD and OpenBSD |

`openssl` is invoked with `-r`, documented as printing the digest in coreutils
format. Its default output is never parsed, because OpenSSL 3 prints
`SHA2-256(file)= <digest>` where OpenSSL 1 printed `SHA256(file)= <digest>`.

### Failure Modes

Every one of the following exits 1, reports the reason through the `error`
helper, leaves no staging directory behind, and installs nothing:

| Condition | Behaviour |
|-----------|-----------|
| The digests differ | Refused, naming both digests |
| The checksum file cannot be downloaded | Refused. A missing checksum is a failure, never a reason to proceed unverified |
| The checksum file names no digest for this archive, or none that is well formed | Refused as malformed |
| The host has no SHA-256 tool | Refused **before anything is downloaded**, naming the three accepted tools |
| The host has no `mktemp` | Refused **before anything is downloaded**, naming the tool. Without it there is no private directory to stage the download in (see Staging Directory) |

There is no flag that disables verification. An installer whose documented
invocation is `curl ... | bash` into a privileged location is exactly where a
bypass switch does the most damage, and no legitimate installation needs one:
the verification path is driven end to end by the test suite against real
fixtures and real digests.

The absence of a hashing tool fails closed rather than warning and continuing.
The documented install path is non-interactive and its standard error routinely
scrolls past unread, so a warning would leave the control in place in name only,
and the party that benefits from a control failing open is the one who
substituted the archive. The failure is also one command away from being fixed by
the user, and the tool it asks for is already present wherever the script can run
at all: on Linux `sha256sum` belongs to the same coreutils package as the `mv`,
`chmod`, `mkdir`, and `rm` the script already invokes unconditionally, and macOS
ships `shasum` in its base system.

### Staging Directory

Everything the installation script downloads is staged inside a single directory
created for that one run: the archive, the `.sha256` file beside it, the
extraction, and the extracted binary. The script writes no downloaded byte to a
path another local user can name in advance, and it writes no fixed path at all.
In particular it never writes `/tmp/rmp` or `/tmp/rmp.exe`, the fixed paths the
extracted binary once passed through.

#### How the Directory Is Created

The script creates the directory with

```
mktemp -d "${TMPDIR:-/tmp}/rmp_install.XXXXXXXXXX"
```

`TMPDIR` is honoured deliberately, with `/tmp` as the fallback: a host that
confines temporary files to a particular filesystem keeps that confinement, and
the checks below are what make honouring it safe. The template gives the
directory the two properties the integrity gate depends on:

1. **The name is unpredictable.** The implementation replaces the ten `X`
   characters, so no other process can work the name out in advance. Ten
   satisfies every implementation the script can meet: GNU `mktemp` requires at
   least three, and OpenBSD's requires at least six in the last component.
2. **Creation is exclusive, and the result is private.** `mktemp -d` creates the
   directory or fails; it never reuses a directory that already exists. The
   directory it creates has mode `0700`, because `mkdtemp(3)` specifies that
   mode, and the process umask can only narrow those bits, never widen them.

Together those close the time-of-check-to-time-of-use window (CWE-367, and the
insecure temporary file of CWE-377) that the checksum cannot close on its own. The archive is still read twice, once by the hashing
tool and once by `tar` or `unzip`, but both reads happen inside a directory no
other user can open, so no other user has a moment in which to substitute the
file between them. The race is closed by taking the access away rather than by
shortening the interval: a path no other user can open offers no time of use to
reach.

The directory is created after the installation scope is chosen and before the
first release asset is requested, so a host that cannot stage privately is told
so with no archive downloaded.

#### What the Script Refuses

The script exits 1 through the `error` helper, downloads no release archive and
no checksum file, and installs nothing, when any of the following holds:

| Condition | Why it is refused |
|-----------|-------------------|
| `TMPDIR` is set to a value that is not an absolute path | A relative value stages the download under whatever directory the script was started from, and a value beginning with `-` reaches `mktemp`, `ls`, and `rm` as an option rather than as a path |
| The parent directory named by `TMPDIR` does not exist, or is not a directory | There is nowhere to stage |
| The permissions of that parent cannot be read | The check on the row below cannot be performed, so the parent cannot be trusted |
| The parent is writable by every user and carries no sticky bit | Any local user could then rename the staging directory out of the way and put their own in its place after it was created. `/tmp` carries mode `1777` on every system the script supports |
| `mktemp -d` fails | There is no directory to stage in, and no predictable path is used instead |
| The created directory is a symbolic link, or is not owned by the invoking user | It is not a directory this script created and owns |
| The created directory's permissions cannot be read, or are not `rwx------` | It is reachable by users other than its owner, which is the condition the staging directory exists to prevent |

The last two conditions check what `mktemp` returned rather than assuming it,
for the same reason the checksum path verifies the archive rather than assuming
it: `mktemp` is resolved through `PATH` like every other tool the script runs,
and every downloaded byte is written inside whatever it hands back. A `mktemp`
earlier on `PATH` that returned an existing, shared, or symlinked directory
would reinstate the defect in full, and these two checks are what make that
refusable instead of silently accepted.

#### A Refused Directory Is Left in Place

The script removes only a directory that passed every check above. A directory
that failed one of them is reported and left exactly as it was found, never
removed. A hostile `mktemp` earlier on `PATH` could print `/`, `/etc`, or a
directory belonging to another user, and a cleanup that trusted that output
would delete it; refusing without removing is what keeps that failure harmless.

#### When the Directory Is Removed

A single `EXIT` trap removes the accepted staging directory, and everything
inside it, on every path out of the script: success, every refusal, and every
failure. Three further traps turn a signal into an ordinary exit so that the
`EXIT` trap still runs, and each exits with 128 plus the number of the signal it
handles:

| Signal | Exit code |
|--------|-----------|
| `HUP` | 129 |
| `INT` | 130 |
| `TERM` | 143 |

No failure site cleans up after itself. Cleanup registered once as a trap cannot
be forgotten by a failure path added later, and it covers the exits no explicit
removal can be written for: an interrupt at the installation-scope prompt, a
terminal that goes away, and a command that fails where none was expected.

#### Tools the Staging Directory Requires

| Tool | Used for | If it is absent |
|------|----------|-----------------|
| `mktemp` | Creating the staging directory | The script exits 1 before any release asset is requested, and the message names the tool (see Failure Modes) |
| `ls` | Reading the permission bits of the staging directory and of its parent, through `ls -ld` | The permissions cannot be read, so the directory is refused and nothing is downloaded |

The script reads permissions with `ls -ld` rather than with `stat(1)` because
`stat` is not in POSIX and its spellings diverge: GNU spells the mode
`stat -c %a` and the BSDs spell it `stat -f %Lp`. The long output format of `ls`
is specified: the first field is the file mode, one file-type character followed
by nine permission characters. An access-control-list or SELinux marker, `+` or
`.`, sits after those ten characters and is not part of the mode the script
reads.

The absence of `mktemp` fails closed for the same reason the absence of a
hashing tool does. There is no fallback to a predictable path, because a
predictable path is the defect this replaced. `mktemp` is present wherever the
script can run at all: GNU coreutils on Linux, the base system on macOS,
FreeBSD, and OpenBSD, and the MSYS2 coreutils package behind Git for Windows.

### What This Protects, and What It Does Not

This section is normative. The guarantee is narrow, and stating it wider than it
is would be worse than not stating it at all.

**Verification detects:**

- A download corrupted in transit or at rest: a truncated transfer, a connection
  dropped mid-body, a damaged cache or mirror object, a short write. Before this
  check, a truncated archive could still extract and be installed.
- An archive replaced **asymmetrically**, where the attacker or the accident
  reached one of the two objects and not the other. The release job uploads with
  `gh release upload --clobber`, so an archive can be replaced in place while its
  `.sha256` is not; a caching layer can hold one object and not the other; an
  interception appliance that repackages executables can rewrite a large binary
  body while passing a small text file through untouched.
- Release-pipeline mistakes: an asset attached to the wrong tag, or a re-run that
  produced an archive the published checksum was not computed from.

**Verification does NOT detect:**

- An attacker who controls the release. A compromised repository account, a
  compromised Actions token, or a compromised workflow replaces the archive and
  its `.sha256` together. The checksum is served from the same origin as the
  archive and has no independent trust anchor.
- An attacker who controls the transport for both objects. A TLS-terminating
  proxy trusted by the client, or any position that can rewrite responses from
  `github.com`, rewrites both.
- An attacker who controls `install.sh` itself. The documented path fetches the
  script from `raw.githubusercontent.com` and pipes it straight into `bash`, so
  whoever can rewrite the script can delete this check. **The verification is
  never stronger than the trust already placed in the delivery of the script that
  performs it.**
- A malicious release that is authentic. Checksums attest to bytes, not to
  intent: a backdoor built and published by the pipeline verifies correctly.
- A local attacker who is already `root`, or who is already the user running the
  installer. Either can rewrite the installed binary directly, at the
  installation directory, so the staging step gives them nothing they do not
  already hold. The archive is still verified and extracted at the same path,
  two reads of one file, but that path now lies inside a directory created
  unpredictably and privately for the single run, which no other local user can
  open (see Staging Directory). Any other local attacker therefore has no moment
  in which to replace the archive between the two reads.

Closing the remaining gap needs a signature over the checksum, made with a key
the release pipeline does not hold and distributed out of band, or an attestation
anchored in a transparency log rather than in the same asset store. Neither is in
place today, and no document in this repository may describe the current state as
authenticity or provenance. It is integrity against corruption and against an
adversary who does not control the release origin.

## Release Process

### Automated Release Creation

Releases are created automatically when a tag matching `v*` pattern is pushed:

1. Push git tag: `git tag -a v1.0.0 -m "Release v1.0.0"`
2. Push tag: `git push origin v1.0.0`
3. GitHub Actions workflow triggers automatically
4. The workflow runs the complete validation gate set (see
   `BUILD.md § Validation Gates`); binaries are built for all platforms only
   after every gate passes
5. GitHub Release is created automatically with all assets attached

### GitHub Actions Workflow

**File:** `.github/workflows/release.yml`

**Triggers:**
- Push of tags matching `v*` (e.g., `v1.0.0`, `v1.1.0`)

**Jobs:**

1. **Pre-release Tests**
   - Run on ubuntu-latest
   - Execute every validation gate except `build`: `fmt`, `vet`, `lint`, `test`,
     and `security`. The gate set, the exact command each gate runs, and the rule
     that a missing tool fails the job instead of skipping the gate are specified
     once in `BUILD.md § Validation Gates`
   - Must pass before building

2. **Build Release Binaries**
   - Matrix builds for all platforms:
     - Linux: amd64, arm64, armv6, armv7
     - macOS: amd64, arm64
     - Windows: amd64, arm64
     - FreeBSD: amd64
     - OpenBSD: amd64, arm64
   - Build flags for production:
     - `-s -w`: Strip debug info and DWARF tables
     - `-trimpath`: Remove build paths for reproducible builds
     - `-extldflags '-static'`: Static linking on Linux
     - `-X main.version=${version}`: Embed version

3. **Create GitHub Release**
   - Runs after all builds complete
   - Creates release using `gh release create`
   - Generates release notes automatically
   - Attaches all built binaries and checksums

### Build Matrix

The release workflow builds one archive per platform. The authoritative list of
platforms is `BUILD.md § Supported Build Targets`; the table below restates it
with the archive format each platform ships.

| OS | Architecture | ARM Version | Output Format |
|----|--------------|-------------|---------------|
| Linux | amd64 | - | tar.gz |
| Linux | arm64 | - | tar.gz |
| Linux | arm | v6 | tar.gz |
| Linux | arm | v7 | tar.gz |
| macOS | amd64 | - | tar.gz |
| macOS | arm64 | - | tar.gz |
| Windows | amd64 | - | zip |
| Windows | arm64 | - | zip |
| FreeBSD | amd64 | - | tar.gz |
| OpenBSD | amd64 | - | tar.gz |
| OpenBSD | arm64 | - | tar.gz |

### Binary Naming Convention

```
rmp-{version}-{os}-{arch}.{ext}
```

**Examples:**
- `rmp-v1.0.0-linux-amd64.tar.gz`
- `rmp-v1.0.0-darwin-arm64.tar.gz`
- `rmp-v1.0.0-windows-amd64.zip`
- `rmp-v1.0.0-linux-armv6.tar.gz`

### Release Assets

Each release includes:
- Binary archives for all supported platforms (11 total)
- SHA256 checksums for each archive
- Automatic release notes generated from commits

### Release Checklist

- [ ] `govulncheck ./...` was run on the tree being released and its result acted on: no standard-library vulnerability is reachable from Groadmap's own code, and any reported-but-not-called vulnerability is recorded in the release notes (see `VERSION.md § Pre-Release Vulnerability Check`)
- [ ] Every validation gate ran and passed in the release workflow, and no gate is reported as skipped, waived, or not installed (see `BUILD.md § Validation Gates`)
- [ ] All binaries built successfully
- [ ] SHA256 checksums generated
- [ ] Release notes prepared
- [ ] Version updated in `cmd/rmp/main.go`
- [ ] Documentation updated (`SPEC/VERSION.md`, `SPEC/README.md`)

## Acceptance Criteria

### Installation Script
- [ ] Detects all supported architectures correctly
- [ ] Downloads correct binary for detected platform
- [ ] Installs binary with executable permissions
- [ ] Provides helpful error messages on failure
- [ ] An unsupported architecture fails before any download is attempted: on a host whose `uname -m` reports `i686`, the script exits 1 and standard error carries the line `ERROR: architecture i686 is not supported. Supported targets: amd64, arm64, armv6, armv7. See SPEC/BUILD.md for the build matrix.` No release asset is requested
- [ ] The architecture guard rejects both values `detect_arch()` can return for a host the build does not serve, `unsupported` and `unknown`, with that same message and exit code
- [ ] An unsupported operating system fails the same way: the script exits 1 and standard error carries the line `ERROR: operating system {uname} is not supported. Supported systems: linux, darwin, freebsd, openbsd, windows. See SPEC/BUILD.md for the build matrix.` with `{uname}` the raw `uname -s` output
- [ ] Every failure the script reports goes through the `error` helper, so every error line begins with the `ERROR: ` prefix and no path prints a bare message (see Diagnostic Output)
- [ ] The downloaded archive is verified against the published `<archive>.sha256` BEFORE it is extracted, on both the `.tar.gz` and the `.zip` branch. An archive whose digest does not match is refused: the script exits non-zero, reports the mismatch with both digests, removes its staging directory (see Staging Directory), and no file reaches the installation directory (see Checksum Verification)
- [ ] A checksum file that cannot be downloaded is a failure. The script does not fall back to an unverified installation, and it offers no flag that would
- [ ] A checksum file that carries no well-formed digest for the archive being installed is refused as malformed. The digest is matched by name, so a checksum file describing a different archive is rejected rather than accepted from a fixed position
- [ ] A host with none of `sha256sum`, `shasum`, or `openssl` is refused with exit 1 before any release asset is requested, and the message names all three
- [ ] `SPEC/DEPLOY.md § Checksum Verification` states both what the check detects and what it does not, and no document in the repository describes the checksum as proof of authenticity or provenance
- [ ] Every byte the script downloads is staged inside a directory created by `mktemp -d` from the template `${TMPDIR:-/tmp}/rmp_install.XXXXXXXXXX`. The directory the script uses differs between two runs on the same host, and `ls -ld` reports mode `drwx------` for it at the instant the verified archive is sitting in it (see Staging Directory)
- [ ] A staging directory the script did not create is refused, with no release archive downloaded, nothing installed, and the directory left exactly as it was found. All three shapes are covered: a directory of that name that already exists, one that belongs to another user, and one that is a symbolic link to somewhere else
- [ ] A `TMPDIR` that is writable by every user and carries no sticky bit is refused, and so is a staging directory that cannot be created. Each exits 1 through the `error` helper with no release archive and no checksum file downloaded
- [ ] A host without `mktemp` is refused with exit 1 before any release asset is requested, and the message names the tool
- [ ] No fixed staging path is written. Files already sitting at `/tmp/rmp` and `/tmp/rmp.exe` before the script runs are byte-identical after a successful installation
- [ ] No exit path leaves the accepted staging directory behind: not a refusal, not a failed extraction, and not a signal. The `EXIT` trap removes it, and the `HUP`, `INT`, and `TERM` traps route a signal through that trap, exiting 129, 130, and 143

### Raspberry Pi Support
- [ ] Detects ARMv6 on Pi Zero/1
- [ ] Detects ARMv7 on Pi 2/3/4 (32-bit)
- [ ] Falls back to ARMv6 for generic ARM detection
- [ ] Can identify Raspberry Pi hardware

### Manual Installation
- [ ] Download URL format is correct
- [ ] Archives extract correctly
- [ ] Binary runs after manual installation

### Release Process
- [ ] `govulncheck ./...` was run on the exact tree being released, and its output is available for the release record
- [ ] No standard-library vulnerability reachable from Groadmap's own code is present at the released commit. Should one be found, the Go floor is raised to the release that fixes it, in both `go.mod` and `BUILD.md § Go Toolchain`, and `govulncheck ./...` re-run clean before the tag is created (see `VERSION.md § Pre-Release Vulnerability Check`)
- [ ] The Go version in `go.mod` and the floor named in `BUILD.md § Go Toolchain` are the same version
- [ ] Any vulnerability reported but not called is recorded in the release notes rather than silently dropped
- [ ] `govulncheck` is run as a release step only. It is not added to `make check`, to `.github/workflows/ci.yml`, or to `.github/workflows/release.yml`, and the gate set stays at the six gates of `BUILD.md § Validation Gates`
