#!/usr/bin/env bash
# Groadmap Agent Skills Installer
# Installs the roadmap-manager and knowledge-authority Claude Code skills of the
# latest Groadmap release into the invoking user's personal skills directory.
# Per SPEC/SKILLS.md § Skills Installer.

set -e

REPO="FlavioCFOliveira/Groadmap"
SKILLS="roadmap-manager knowledge-authority"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print functions. Every diagnostic goes to standard error through one of these
# (SPEC/DEPLOY.md § Diagnostic Output). This script asks nothing, so it has no
# prompt helper.
info() {
    echo -e "${BLUE}INFO:${NC} $1" >&2
}

success() {
    echo -e "${GREEN}SUCCESS:${NC} $1" >&2
}

warn() {
    echo -e "${YELLOW}WARNING:${NC} $1" >&2
}

error() {
    echo -e "${RED}ERROR:${NC} $1" >&2
}

# Fetch a URL into a destination path with whichever downloader is installed.
# Returns non-zero when the transfer fails and when neither tool is present;
# the caller reports the failure. Identical to install.sh.
fetch_url() {
    local url="$1"
    local dest="$2"

    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$dest" "$url" 2>/dev/null
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$dest" "$url" 2>/dev/null
    else
        return 1
    fi
}

# Name the SHA-256 tool this host provides, or print nothing when it has none.
# Identical to install.sh; see SPEC/DEPLOY.md § How the Script Verifies for why
# each tool's output is read the way compute_sha256 reads it.
sha256_tool() {
    if command -v sha256sum >/dev/null 2>&1; then
        echo "sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        echo "shasum"
    elif command -v openssl >/dev/null 2>&1; then
        echo "openssl"
    fi
}

# Normalise a hex digest to 64 lowercase characters on standard output, and
# return 1 when the argument is not a well-formed SHA-256 digest. Identical to
# install.sh.
normalise_sha256() {
    local digest="$1"
    digest="${digest//A/a}"
    digest="${digest//B/b}"
    digest="${digest//C/c}"
    digest="${digest//D/d}"
    digest="${digest//E/e}"
    digest="${digest//F/f}"

    if [ "${#digest}" -ne 64 ]; then
        return 1
    fi
    case "$digest" in
        *[!0-9a-f]*) return 1 ;;
    esac

    echo "$digest"
}

# Print the SHA-256 digest of a file as 64 lowercase hex characters. Identical to
# install.sh.
compute_sha256() {
    local file="$1"
    local output=""

    case "$(sha256_tool)" in
        sha256sum) output=$(sha256sum "$file" 2>/dev/null) || return 1 ;;
        shasum)    output=$(shasum -a 256 "$file" 2>/dev/null) || return 1 ;;
        openssl)   output=$(openssl dgst -sha256 -r "$file" 2>/dev/null) || return 1 ;;
        *)         return 1 ;;
    esac

    normalise_sha256 "${output%% *}"
}

# Print the digest the checksum file records for one archive name, selected by
# NAME, and return 1 when no line names the archive with a well-formed digest.
# Identical to install.sh.
expected_sha256() {
    local checksum_file="$1"
    local archive_name="$2"
    local digest name rest

    while read -r digest name rest || [ -n "$digest" ]; do
        name="${name#\*}"
        if [ "$name" = "$archive_name" ]; then
            if normalise_sha256 "$digest"; then
                return 0
            fi
        fi
    done < "$checksum_file"

    return 1
}

# Verify a downloaded archive against the checksum file published beside it.
# Returns 0 only when the two agree. Identical to install.sh.
verify_archive_checksum() {
    local archive_path="$1"
    local archive_name="$2"
    local checksum_file="$3"

    local expected
    if ! expected=$(expected_sha256 "$checksum_file" "$archive_name"); then
        error "the checksum file published for ${archive_name} records no SHA-256 digest for it."
        error "It must carry a line of the form '<64 hex characters>  ${archive_name}'."
        return 1
    fi

    local actual
    if ! actual=$(compute_sha256 "$archive_path"); then
        error "failed to compute the SHA-256 digest of ${archive_name}."
        return 1
    fi

    if [ "$actual" != "$expected" ]; then
        error "checksum mismatch for ${archive_name}: the downloaded archive is not the one the release published."
        error "  expected: ${expected}"
        error "  actual:   ${actual}"
        return 1
    fi

    info "Checksum verified: SHA-256 ${actual}"
    return 0
}

# Get latest release version from GitHub. Identical to install.sh: the tag_name
# of the latest-release response, with curl or else wget.
get_latest_version() {
    local api_url="https://api.github.com/repos/${REPO}/releases/latest"
    local version

    if command -v curl >/dev/null 2>&1; then
        version=$(curl -fsSL "$api_url" 2>/dev/null | grep -o '"tag_name": "[^"]*"' | head -1 | sed 's/"tag_name": "//;s/"$//')
    elif command -v wget >/dev/null 2>&1; then
        version=$(wget -qO- "$api_url" 2>/dev/null | grep -o '"tag_name": "[^"]*"' | head -1 | sed 's/"tag_name": "//;s/"$//')
    fi

    if [ -z "$version" ]; then
        error "Failed to fetch latest version from GitHub"
        exit 1
    fi

    echo "$version"
}

# Print the nine permission characters of a path's mode, read from `ls -ld`
# rather than stat(1), which is not portable. Returns 1 when the path cannot be
# listed. Identical to install.sh (SPEC/DEPLOY.md § Tools the Staging Directory
# Requires). A caller that must follow a symbolic link to a directory passes the
# path with a trailing '/'.
path_permissions() {
    local listing
    listing=$(ls -ld "$1" 2>/dev/null) || return 1
    listing="${listing%% *}"
    if [ "${#listing}" -lt 10 ]; then
        return 1
    fi
    echo "${listing:1:9}"
}

# The private directory this run stages its download in. Empty until
# create_staging_dir has accepted it; only an accepted directory is removed.
STAGING_DIR=""

# The work directory inside the skills directory (SPEC/SKILLS.md § Replacement
# Procedure). Empty until create_work_dir has accepted it.
WORK_DIR=""

# Set when a rollback could not restore a previous copy: the work directory then
# still holds it and is kept (§ Replacement Procedure, item 5).
KEEP_WORK_DIR=0

# The resolved destination (SPEC/SKILLS.md § Destination).
CONFIG_DIR=""
SKILLS_DIR=""

# The swap's steps, oldest first, as "old:<skill>" (a previous copy moved into
# the work directory) and "new:<skill>" (a new copy moved into place). A step is
# recorded BEFORE its rename runs, and the rollback reads the filesystem to tell
# whether the rename happened, so an interruption that lands between a rename
# and the next statement is undone too. SWAP_ACTIVE is 1 while a swap is in
# progress, so an interruption knows to roll it back.
SWAP_STEPS=""
SWAP_ACTIVE=0

# Remove the staging directory and the work directory on every path out of the
# script, unless a failed rollback left a previous copy in the work directory.
cleanup() {
    if [ -n "$STAGING_DIR" ] && [ -d "$STAGING_DIR" ]; then
        rm -rf "$STAGING_DIR" 2>/dev/null || :
    fi
    STAGING_DIR=""
    if [ "$KEEP_WORK_DIR" -eq 0 ] && [ -n "$WORK_DIR" ] && [ -d "$WORK_DIR" ]; then
        rm -rf "$WORK_DIR" 2>/dev/null || :
    fi
    WORK_DIR=""
}

# Turn a signal into an ordinary exit with 128 plus its number, rolling back a
# swap that was in progress first, so the EXIT trap still runs
# (SPEC/DEPLOY.md § When the Directory Is Removed; SPEC/SKILLS.md § Replacement
# Procedure, item 6).
on_signal() {
    local code="$1"
    trap '' HUP INT TERM
    if [ "$SWAP_ACTIVE" -eq 1 ]; then
        rollback_swap || :
        SWAP_ACTIVE=0
    fi
    exit "$code"
}

# Create the private directory this run stages its download in, and print it.
# Returns 1, having reported the reason, when it cannot be created or trusted.
# The same rules as install.sh's create_staging_dir, with the template
# rmp_skills_install.XXXXXXXXXX (SPEC/SKILLS.md § Release Resolution and
# Download, item 4; SPEC/DEPLOY.md § Staging Directory).
create_staging_dir() {
    local parent="${TMPDIR:-/tmp}"
    parent="${parent%/}"
    [ -n "$parent" ] || parent="/"

    case "$parent" in
        /*) ;;
        *)
            error "cannot stage the download: TMPDIR must be an absolute path, and it is ${parent}."
            return 1
            ;;
    esac

    if [ ! -d "$parent" ]; then
        error "cannot stage the download: ${parent} is not a directory. Set TMPDIR to a writable directory and run this script again."
        return 1
    fi

    local parent_mode
    if ! parent_mode=$(path_permissions "$parent"); then
        error "cannot stage the download: the permissions of ${parent} could not be read."
        return 1
    fi
    if [ "${parent_mode:7:1}" = "w" ] && \
       [ "${parent_mode:8:1}" != "t" ] && [ "${parent_mode:8:1}" != "T" ]; then
        error "refusing to stage the download in ${parent}: it is writable by every user and carries no sticky bit, so another user could replace the staging directory after it is created."
        return 1
    fi

    local dir=""
    dir=$(mktemp -d "${parent}/rmp_skills_install.XXXXXXXXXX" 2>/dev/null) || dir=""
    if [ -z "$dir" ] || [ ! -d "$dir" ]; then
        error "failed to create a private staging directory under ${parent}. Nothing was downloaded."
        return 1
    fi

    if [ -L "$dir" ] || [ ! -O "$dir" ]; then
        error "refusing to stage the download in ${dir}: it is not a directory this script created and owns."
        return 1
    fi
    local mode
    if ! mode=$(path_permissions "$dir"); then
        error "refusing to stage the download in ${dir}: its permissions could not be read."
        return 1
    fi
    if [ "$mode" != "rwx------" ]; then
        error "refusing to stage the download in ${dir}: mode ${mode} leaves it reachable by other users, so the archive could be replaced between the checksum check and the extraction."
        return 1
    fi

    printf '%s\n' "$dir"
}

# Refuse when a required tool is absent, before anything is fetched
# (SPEC/SKILLS.md § Tools Required).
check_tools() {
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        error "Neither curl nor wget is available. Please install one of them."
        exit 1
    fi
    if [ -z "$(sha256_tool)" ]; then
        error "no SHA-256 tool is available, so the download cannot be verified. Install one of sha256sum (GNU coreutils), shasum (Perl Digest::SHA) or openssl."
        exit 1
    fi
    if ! command -v mktemp >/dev/null 2>&1; then
        error "mktemp is required to stage the download privately, but it was not found. Install it (GNU coreutils on Linux; it is part of the base system on macOS, FreeBSD and OpenBSD)."
        exit 1
    fi
    if ! command -v tar >/dev/null 2>&1; then
        error "tar is required to list and extract the skills archive, but it was not found. Install tar with gzip support and run this script again."
        exit 1
    fi
}

# Resolve the configuration directory, the skills directory and the skill paths,
# and refuse an unsafe or unusable destination before any release asset is
# requested. Writes nothing (SPEC/SKILLS.md § Destination and § Destination
# Rules).
resolve_destination() {
    if [ -n "${CLAUDE_CONFIG_DIR:-}" ]; then
        case "$CLAUDE_CONFIG_DIR" in
            /*) ;;
            *)
                error "CLAUDE_CONFIG_DIR must be an absolute path, and it is ${CLAUDE_CONFIG_DIR}. Nothing was installed."
                exit 1
                ;;
        esac
        CONFIG_DIR="${CLAUDE_CONFIG_DIR%/}"
        [ -n "$CONFIG_DIR" ] || CONFIG_DIR="/"
    else
        case "${HOME:-}" in
            /*) ;;
            *)
                error "HOME is unset, empty or not an absolute path, and CLAUDE_CONFIG_DIR is not set, so there is no Claude Code configuration directory to install into. Nothing was installed."
                exit 1
                ;;
        esac
        CONFIG_DIR="${HOME%/}/.claude"
    fi
    SKILLS_DIR="${CONFIG_DIR%/}/skills"

    local dir
    for dir in "$CONFIG_DIR" "$SKILLS_DIR"; do
        if { [ -e "$dir" ] || [ -L "$dir" ]; } && [ ! -d "$dir" ]; then
            error "${dir} exists and is not a directory, so there is nowhere to install the skills. Nothing was installed."
            exit 1
        fi
    done

    if [ -d "$SKILLS_DIR" ]; then
        if [ ! -O "$SKILLS_DIR" ]; then
            error "${SKILLS_DIR} is not owned by the invoking user, so it is not this user's skills directory. Nothing was installed."
            exit 1
        fi
        local mode
        if ! mode=$(path_permissions "${SKILLS_DIR}/"); then
            error "the permissions of ${SKILLS_DIR} could not be read. Nothing was installed."
            exit 1
        fi
        if [ "${mode:7:1}" = "w" ] && \
           [ "${mode:8:1}" != "t" ] && [ "${mode:8:1}" != "T" ]; then
            error "${SKILLS_DIR} is writable by every user and carries no sticky bit, so another user could replace a skill or this installer's work directory inside it. Nothing was installed."
            exit 1
        fi
    fi

    local skill path
    for skill in $SKILLS; do
        path="${SKILLS_DIR}/${skill}"
        if [ -L "$path" ]; then
            error "${path} is a symbolic link; replacing it would replace whatever it points to. Remove the link and run this script again. Nothing was installed."
            exit 1
        fi
        if [ -e "$path" ]; then
            if [ ! -d "$path" ]; then
                error "${path} exists and is not a directory, so it is not a skill directory this installer can replace. Nothing was installed."
                exit 1
            fi
            if [ ! -O "$path" ]; then
                error "${path} is not owned by the invoking user, so this installer does not replace it. Nothing was installed."
                exit 1
            fi
        fi
    done
}

# Create every missing component of a directory path with mode 0700, so that
# every directory this script creates is private (SPEC/SKILLS.md § Destination
# Rules).
make_private_dirs() {
    local target="$1"
    local path="" component rest="${target#/}"

    while [ -n "$rest" ]; do
        component="${rest%%/*}"
        if [ "$component" = "$rest" ]; then
            rest=""
        else
            rest="${rest#*/}"
        fi
        [ -n "$component" ] || continue
        path="${path}/${component}"
        if [ ! -d "$path" ]; then
            if ! mkdir -m 700 "$path" 2>/dev/null; then
                error "failed to create ${path}. Nothing was installed."
                return 1
            fi
        fi
    done
}

# Create the work directory inside the skills directory, on the same filesystem
# as the skill paths, and check it by the staging directory's rules
# (SPEC/SKILLS.md § Replacement Procedure, item 1). A directory that fails a
# check is reported and left in place.
create_work_dir() {
    local dir=""
    dir=$(mktemp -d "${SKILLS_DIR}/.rmp-skills-install.XXXXXXXXXX" 2>/dev/null) || dir=""
    if [ -z "$dir" ] || [ ! -d "$dir" ]; then
        error "failed to create a work directory under ${SKILLS_DIR}. Nothing was installed."
        return 1
    fi
    if [ -L "$dir" ] || [ ! -O "$dir" ]; then
        error "refusing to use ${dir} as the work directory: it is not a directory this script created and owns. Nothing was installed."
        return 1
    fi
    local mode
    if ! mode=$(path_permissions "$dir"); then
        error "refusing to use ${dir} as the work directory: its permissions could not be read. Nothing was installed."
        return 1
    fi
    if [ "$mode" != "rwx------" ]; then
        error "refusing to use ${dir} as the work directory: mode ${mode} leaves it reachable by other users. Nothing was installed."
        return 1
    fi
    printf '%s\n' "$dir"
}

# Validate the archive's entry names before extraction (SPEC/SKILLS.md § Archive
# Validation, item 1), and refuse any entry the listing marks as something other
# than a regular file or a directory, so a link is never extracted at all.
validate_archive_listing() {
    local archive="$1"
    local names listing name line

    if ! names=$(tar -tzf "$archive" 2>/dev/null); then
        error "the skills archive could not be listed. Nothing was installed."
        return 1
    fi
    if [ -z "$names" ]; then
        error "the skills archive is empty. Nothing was installed."
        return 1
    fi

    while IFS= read -r name; do
        case "$name" in
            /*)
                error "the skills archive carries an absolute entry name: ${name}. Nothing was installed."
                return 1
                ;;
        esac
        case "/${name}/" in
            */../*)
                error "the skills archive carries an entry with a '..' component: ${name}. Nothing was installed."
                return 1
                ;;
        esac
        case "$name" in
            roadmap-manager|roadmap-manager/*|knowledge-authority|knowledge-authority/*) ;;
            *)
                error "the skills archive carries an entry outside roadmap-manager/ and knowledge-authority/: ${name}. Nothing was installed."
                return 1
                ;;
        esac
    done <<< "$names"

    if ! listing=$(tar -tvzf "$archive" 2>/dev/null); then
        error "the skills archive could not be listed. Nothing was installed."
        return 1
    fi
    while IFS= read -r line; do
        case "$line" in
            -*|d*) ;;
            *)
                error "the skills archive carries an entry that is neither a regular file nor a directory: ${line}. Nothing was installed."
                return 1
                ;;
        esac
    done <<< "$listing"
}

# Validate the extracted tree (SPEC/SKILLS.md § Archive Validation, item 2).
validate_extracted_tree() {
    local root="$1"
    local skill found

    for skill in $SKILLS; do
        if [ -L "${root}/${skill}" ] || [ ! -d "${root}/${skill}" ]; then
            error "the skills archive does not contain the skill ${skill}/. Nothing was installed."
            return 1
        fi
        if [ -L "${root}/${skill}/SKILL.md" ] || [ ! -f "${root}/${skill}/SKILL.md" ]; then
            error "the skills archive does not contain ${skill}/SKILL.md. Nothing was installed."
            return 1
        fi
    done

    if ! found=$(find "$root" -type l 2>/dev/null); then
        error "the extracted skills could not be inspected. Nothing was installed."
        return 1
    fi
    if [ -n "$found" ]; then
        error "the skills archive carries a symbolic link: ${found%%$'\n'*}. Nothing was installed."
        return 1
    fi
    if ! found=$(find "$root" ! -type d ! -type f 2>/dev/null); then
        error "the extracted skills could not be inspected. Nothing was installed."
        return 1
    fi
    if [ -n "$found" ]; then
        error "the skills archive carries an entry that is neither a regular file nor a directory: ${found%%$'\n'*}. Nothing was installed."
        return 1
    fi
    if ! found=$(find "$root" -type f -links +1 2>/dev/null); then
        error "the extracted skills could not be inspected. Nothing was installed."
        return 1
    fi
    if [ -n "$found" ]; then
        error "the skills archive carries a file with more than one link: ${found%%$'\n'*}. Nothing was installed."
        return 1
    fi
}

# Undo every swap step in reverse order (SPEC/SKILLS.md § Replacement
# Procedure, items 4 and 5): a new copy that reached its skill path is moved back
# into the work directory, and a previous copy that was moved aside is renamed
# back to its skill path. A recorded step whose rename never happened is left
# alone. Returns 1 when an undo step failed; the work directory is then kept, and
# each previous copy that could not be restored is named with the path that
# still holds it.
rollback_swap() {
    local steps="" step kind skill path failed=0

    for step in $SWAP_STEPS; do
        steps="${step} ${steps}"
    done

    for step in $steps; do
        kind="${step%%:*}"
        skill="${step#*:}"
        path="${SKILLS_DIR}/${skill}"
        if [ "$kind" = "new" ]; then
            if [ -e "${WORK_DIR}/new/${skill}" ] || [ -L "${WORK_DIR}/new/${skill}" ]; then
                continue
            fi
            if ! mv "$path" "${WORK_DIR}/new/${skill}" 2>/dev/null; then
                error "rollback: the new copy of ${skill} could not be moved out of ${path}."
                failed=1
            fi
        else
            if [ ! -e "${WORK_DIR}/old/${skill}" ] && [ ! -L "${WORK_DIR}/old/${skill}" ]; then
                continue
            fi
            if [ -e "$path" ] || [ -L "$path" ] || ! mv "${WORK_DIR}/old/${skill}" "$path" 2>/dev/null; then
                error "rollback: the previous copy of ${skill} could not be restored to ${path}; it is still at ${WORK_DIR}/old/${skill}."
                failed=1
            fi
        fi
    done
    SWAP_STEPS=""

    if [ "$failed" -ne 0 ]; then
        KEEP_WORK_DIR=1
        error "the work directory ${WORK_DIR} has been kept because it still holds a previous copy."
        return 1
    fi
    return 0
}

# Abandon a swap: roll back what was done, then exit 1. Signals are ignored
# while the rollback runs, so it is never interrupted half-way.
fail_swap() {
    trap '' HUP INT TERM
    error "$1"
    rollback_swap || :
    SWAP_ACTIVE=0
    exit 1
}

# Replace each skill in turn with the new copy (SPEC/SKILLS.md § Replacement
# Procedure, item 3).
swap_skills() {
    local skill path

    if ! mkdir "${WORK_DIR}/old" 2>/dev/null; then
        error "failed to prepare the work directory ${WORK_DIR}. Nothing was installed."
        exit 1
    fi

    SWAP_ACTIVE=1
    for skill in $SKILLS; do
        path="${SKILLS_DIR}/${skill}"
        if [ -e "$path" ] || [ -L "$path" ]; then
            SWAP_STEPS="${SWAP_STEPS} old:${skill}"
            if ! mv "$path" "${WORK_DIR}/old/${skill}" 2>/dev/null; then
                fail_swap "failed to move the installed ${skill} from ${path} to ${WORK_DIR}/old/${skill}; both skills are left at their previous versions."
            fi
        fi
        if [ -e "$path" ] || [ -L "$path" ]; then
            fail_swap "${path} is still present after it was moved aside; both skills are left at their previous versions."
        fi
        SWAP_STEPS="${SWAP_STEPS} new:${skill}"
        if ! mv "${WORK_DIR}/new/${skill}" "$path" 2>/dev/null; then
            fail_swap "failed to move the new ${skill} from ${WORK_DIR}/new/${skill} to ${path}; both skills are left at their previous versions."
        fi
    done
    SWAP_ACTIVE=0
    SWAP_STEPS=""
}

# Main installation flow
main() {
    check_tools
    resolve_destination

    info "Skills directory: ${SKILLS_DIR}"

    local version
    if ! version=$(get_latest_version); then
        exit 1
    fi
    info "Latest release: ${version}"

    if ! STAGING_DIR=$(create_staging_dir); then
        STAGING_DIR=""
        exit 1
    fi

    local archive_name="rmp-skills-${version}.tar.gz"
    local archive_url="https://github.com/${REPO}/releases/download/${version}/${archive_name}"
    local archive="${STAGING_DIR}/${archive_name}"

    info "Downloading ${archive_name}..."
    if ! fetch_url "$archive_url" "$archive"; then
        error "release ${version} publishes no downloadable skills archive ${archive_name}. Nothing was installed."
        exit 1
    fi

    info "Verifying checksum..."
    if ! fetch_url "${archive_url}.sha256" "${archive}.sha256"; then
        error "Failed to download the checksum from ${archive_url}.sha256"
        error "Every release publishes a .sha256 beside each archive, and this installer does not install an archive it cannot verify. Nothing was installed."
        exit 1
    fi
    if ! verify_archive_checksum "$archive" "$archive_name" "${archive}.sha256"; then
        error "Refusing to install ${archive_name}. Nothing was extracted and nothing was installed."
        exit 1
    fi

    if ! validate_archive_listing "$archive"; then
        exit 1
    fi

    if ! make_private_dirs "$SKILLS_DIR"; then
        exit 1
    fi
    if ! WORK_DIR=$(create_work_dir); then
        WORK_DIR=""
        exit 1
    fi

    if ! mkdir "${WORK_DIR}/new" 2>/dev/null || \
       ! tar -xzf "$archive" -C "${WORK_DIR}/new" 2>/dev/null; then
        error "failed to extract ${archive_name}. Nothing was installed."
        exit 1
    fi
    if ! validate_extracted_tree "${WORK_DIR}/new"; then
        exit 1
    fi

    swap_skills

    success "Installed roadmap-manager and knowledge-authority ${version} in ${SKILLS_DIR}"
}

# The staging and work directories are removed on every exit: normal, failed,
# or interrupted. A signal rolls back a swap in progress, then exits with
# 128 plus its number so the EXIT trap runs.
trap cleanup EXIT
trap 'on_signal 129' HUP
trap 'on_signal 130' INT
trap 'on_signal 143' TERM

main "$@"
