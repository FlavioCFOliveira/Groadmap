#!/usr/bin/env bash
# Groadmap Installer Script
# Installs or updates the rmp binary from GitHub releases

set -e

REPO="FlavioCFOliveira/Groadmap"
BINARY_NAME="rmp"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Print functions
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

prompt() {
    echo -e "${CYAN}PROMPT:${NC} $1" >&2
}

# Detect OS
detect_os() {
    local os
    case "$(uname -s)" in
        Linux*)     os="linux" ;;
        Darwin*)    os="darwin" ;;
        FreeBSD*)   os="freebsd" ;;
        OpenBSD*)   os="openbsd" ;;
        CYGWIN*|MINGW*|MSYS*) os="windows" ;;
        *)          os="unknown" ;;
    esac
    echo "$os"
}

# Detect Raspberry Pi via /proc/device-tree/model or /proc/cpuinfo (BCM28xx SoC).
# Returns 0 if running on a Raspberry Pi, 1 otherwise.
# Per SPEC/DEPLOY.md § Raspberry Pi Detection.
is_raspberry_pi() {
    if [ -r /proc/device-tree/model ]; then
        if grep -qi "raspberry pi" /proc/device-tree/model 2>/dev/null; then
            return 0
        fi
    fi
    if [ -r /proc/cpuinfo ]; then
        if grep -qE "^Hardware\s*:\s*BCM(2708|2709|2710|2711|2712|283[0-9])" /proc/cpuinfo 2>/dev/null; then
            return 0
        fi
        if grep -qi "raspberry pi" /proc/cpuinfo 2>/dev/null; then
            return 0
        fi
    fi
    return 1
}

# Build the GitHub release download URL for a given version, OS and architecture.
# Output format: https://github.com/<REPO>/releases/download/<version>/rmp-<version>-<os>-<arch>.<ext>
# Per SPEC/DEPLOY.md § Download URL Format.
get_download_url() {
    local version="$1"
    local os="$2"
    local arch="$3"
    local archive_ext="tar.gz"

    if [ "$os" = "windows" ]; then
        archive_ext="zip"
    fi

    local archive_name="${BINARY_NAME}-${version}-${os}-${arch}.${archive_ext}"
    echo "https://github.com/${REPO}/releases/download/${version}/${archive_name}"
}

# Build the URL of the checksum file published beside a release archive.
# .github/workflows/release.yml (step "Generate checksum") runs
# `sha256sum <archive> > <archive>.sha256` from inside dist/, and the release
# job attaches every dist/*.sha256 as an asset of its own, so the checksum for
# an archive always sits at the archive's own URL with .sha256 appended.
# Per SPEC/DEPLOY.md Checksum Verification.
get_checksum_url() {
    local download_url="$1"
    echo "${download_url}.sha256"
}

# Fetch a URL into a destination path with whichever downloader is installed.
# Returns non-zero when the transfer fails and when neither tool is present;
# the caller reports the failure, because the message depends on what was being
# fetched. Both branches are quiet: the script speaks through its own helpers.
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
#
# Each accepted tool was verified to print the digest as the FIRST
# whitespace-separated field of its output:
#   sha256sum                 GNU coreutils on Linux, and the GNU-compatible
#                             sha256sum(1) FreeBSD carries in base.
#   shasum -a 256             The macOS base system (Perl's Digest::SHA).
#   openssl dgst -sha256 -r   OpenSSL and LibreSSL, which FreeBSD and OpenBSD
#                             carry in base. The -r flag is documented as
#                             "print the digest in coreutils format"; openssl's
#                             DEFAULT format is deliberately never parsed,
#                             because OpenSSL 3 prints "SHA2-256(file)= <hex>"
#                             where OpenSSL 1 printed "SHA256(file)= <hex>".
# Per SPEC/DEPLOY.md Checksum Verification.
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
# return 1 when the argument is not a well-formed SHA-256 digest. This is what
# stops a truncated, empty or non-hex field from ever being compared as though
# it were a digest -- an empty string compared against an empty string would
# otherwise "match". The fold uses shell parameter substitution so it needs no
# external tool and runs on the bash 3.2 macOS still ships.
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

# Print the SHA-256 digest of a file as 64 lowercase hex characters.
# Returns 1 when no hashing tool is available, when the tool fails, or when its
# output is not a digest.
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

# Print the digest the checksum file records for one archive name, and return 1
# when no line of that file names the archive with a well-formed digest.
#
# The published file is sha256sum's own output: the digest, two spaces, and the
# archive's base name. The workflow runs sha256sum from inside dist/, so the
# recorded name never carries a directory component. sha256sum's binary mode
# marks the name with a leading '*' instead of the second space, and that
# spelling is accepted too.
#
# The line is selected by NAME rather than simply read from the top of the file,
# so a checksum file that describes some other archive is rejected instead of
# being compared against this one.
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
# Returns 0 only when the two agree; every other outcome reports its reason
# through the error helper and returns 1. There is no third answer, so a caller
# that installs only on 0 installs only what was verified.
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

# Detect architecture
detect_arch() {
    local arch
    case "$(uname -m)" in
        x86_64|amd64)   arch="amd64" ;;
        arm64|aarch64)  arch="arm64" ;;
        armv6l|armv6)   arch="armv6" ;;
        armv7l|armv7)   arch="armv7" ;;
        arm*)           # Fallback for generic ARM - try to detect version
            if [ -f /proc/cpuinfo ]; then
                if grep -q "ARMv7" /proc/cpuinfo 2>/dev/null || \
                   grep -E "^CPU architecture:\s*7" /proc/cpuinfo 2>/dev/null; then
                    arch="armv7"
                elif grep -q "ARMv6" /proc/cpuinfo 2>/dev/null || \
                     grep -E "^CPU architecture:\s*6" /proc/cpuinfo 2>/dev/null; then
                    arch="armv6"
                else
                    # Default to armv6 for maximum compatibility (lowest common denominator)
                    arch="armv6"
                fi
            else
                arch="armv6"
            fi
            ;;
        # Recognised, but the build does not produce it: SPEC/BUILD.md ships no
        # 32-bit x86 target. Reported as unsupported rather than mapped to 386,
        # which would ask the release for an asset that does not exist and fail
        # late on the download instead of here.
        i386|i686)      arch="unsupported" ;;
        *)              arch="unknown" ;;
    esac
    echo "$arch"
}

# Get latest release version from GitHub
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

# Get current installed version
get_current_version() {
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        local version
        version=$($BINARY_NAME --version 2>/dev/null | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+' || echo "")
        echo "$version"
    else
        echo ""
    fi
}

# Ask user for installation scope
ask_install_scope() {
    local response
    local default_option="s"

    echo ""
    prompt "Select installation scope:"
    echo "  [s] System-wide (requires sudo, installs to /usr/local/bin)"
    echo "  [u] User only (no sudo required, installs to ~/.local/bin)"
    echo ""
    printf "Enter choice [s/u] (default: s): "
    read -r response

    # Default to system if empty
    if [ -z "$response" ]; then
        response="$default_option"
    fi

    case "$response" in
        [Ss]*)
            echo "system"
            ;;
        [Uu]*)
            echo "user"
            ;;
        *)
            warn "Invalid choice. Defaulting to system-wide installation."
            echo "system"
            ;;
    esac
}

# Determine install directory based on scope
get_install_dir() {
    local scope="$1"

    if [ "$scope" = "system" ]; then
        # Try /usr/local/bin first, fallback to /usr/bin
        if [ -d "/usr/local/bin" ]; then
            echo "/usr/local/bin"
        else
            echo "/usr/bin"
        fi
    else
        # User installation - use ~/.local/bin (XDG standard)
        local user_bin="${HOME}/.local/bin"
        mkdir -p "$user_bin"
        echo "$user_bin"
    fi
}

# Check if directory is in PATH
dir_in_path() {
    local dir="$1"
    case ":${PATH}:" in
        *:"$dir":*)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

# The private directory this run stages its download in, and the only path the
# cleanup has to know about. Empty until create_staging_dir succeeds.
STAGING_DIR=""

# The staged binary, set by download_binary and read by main. A global rather
# than a value echoed on standard output, because `x=$(download_binary ...)`
# would run the download in a subshell: a stray line on standard output would
# silently become the path, and the staging directory the EXIT trap owns would
# be managed from one shell and used from another.
STAGED_BINARY=""

# Remove the staging directory and everything inside it.
#
# Registered ONCE as a trap rather than repeated at every failure site. The
# shape this replaces carried thirteen separate `rm -rf "$tmp_dir"` calls, one
# per abort path -- thirteen chances for a fourteenth abort path to be added
# without one -- and it still covered none of the exits no `rm` can be written
# for: a Ctrl-C at the scope prompt, a SIGTERM, a SIGHUP when the terminal goes
# away, or `set -e` firing on a command nobody expected to fail. A trap covers
# all of them and cannot be forgotten.
#
# It only ever removes a directory create_staging_dir accepted -- created by
# mktemp -d, not a symlink, owned by us, mode 0700 -- so it can never be aimed
# at a path this script did not create.
cleanup_staging() {
    if [ -n "$STAGING_DIR" ] && [ -d "$STAGING_DIR" ]; then
        rm -rf "$STAGING_DIR" 2>/dev/null || :
    fi
    STAGING_DIR=""
}

# Print the nine permission characters of a path's mode -- `rwx------` for a
# private directory -- and return 1 when the path cannot be listed.
#
# Read from `ls -ld` because stat(1) is not portable: GNU spells this
# `stat -c %a` and the BSDs spell it `stat -f %Lp`. The long format of ls(1) IS
# specified: POSIX.1 ls, STDOUT, gives the first field as the file mode, one
# file-type character followed by nine permission characters. An ACL or SELinux
# marker ('+' or '.') sits after those ten and is left outside the substring.
path_permissions() {
    local listing
    listing=$(ls -ld "$1" 2>/dev/null) || return 1
    listing="${listing%% *}"
    if [ "${#listing}" -lt 10 ]; then
        return 1
    fi
    echo "${listing:1:9}"
}

# Create the private directory this run stages its download in, and print it.
# Returns 1, having reported the reason, when it cannot be created or cannot be
# trusted; the caller installs nothing in that case.
#
# WHY THIS EXISTS (rmp task #309; CWE-367 time-of-check-to-time-of-use, CWE-377
# insecure temporary file). The staging directory used to be
# `/tmp/rmp_install_$$`, created with `mkdir -p`. A PID is one of at most 32768
# values on a Linux host, so another local user could pre-create every one of
# them mode 0777 and wait -- and `mkdir -p` SUCCEEDS on a directory that already
# exists, so this script would then stage its download inside a directory
# belonging to the attacker. That defeats the checksum gate completely: the
# archive is verified at one path and extracted from that same path, two reads
# of one file, and whoever can write between them has the extraction take bytes
# the verification never saw. The payload then travels to /usr/local/bin under
# the sudo the documented install path already uses, so the window sits ON a
# privilege boundary rather than inside one trust domain.
#
# `mktemp -d` closes both halves of that. It creates the directory atomically
# with a name that cannot be predicted, it FAILS rather than succeeding when the
# name it tried already exists, and mkdtemp(3) creates it mode 0700 -- so even a
# guessed name cannot be written into, because no other user has search
# permission on it. Taking away the attacker's write access is what removes the
# race: a path nobody else can open for writing has no time-of-use to reach.
#
# THE ALTERNATIVE, AND WHY IT IS NOT TAKEN. The structural weakness is not the
# name of the path; it is that the archive is opened twice -- once by the
# hashing tool and once by tar or unzip. Verifying and extracting in a single
# pass, from one file descriptor the script never lets go of, would remove the
# second open and with it the window itself. In portable shell it cannot be
# done, for four reasons that hold at once:
#   * tar and unzip take a PATH, not a descriptor, so the second open happens
#     inside them and is not the script's to avoid;
#   * `/dev/fd/N` does not work around that portably. Linux re-opens the inode
#     through it, which would be exactly what is wanted, while FreeBSD's fdescfs
#     duplicates the descriptor instead, so the extractor would start reading at
#     the offset the hashing tool left it at -- end of file;
#   * the shell has no way to rewind a descriptor between the two readers, since
#     there is no lseek primitive in the language;
#   * teeing the download into the hasher and the extractor at the same time
#     would have tar and unzip consume bytes BEFORE the digest is known, which
#     inverts the gate rmp task #185 built rather than protecting it, and unzip
#     cannot read a non-seekable stream at all.
# So the window is closed the other way: not by holding one descriptor, but by
# making sure no other user can ever hold one. See SPEC/DEPLOY.md Staging
# Directory.
create_staging_dir() {
    local parent="${TMPDIR:-/tmp}"
    parent="${parent%/}"
    [ -n "$parent" ] || parent="/"

    # Absolute paths only. A relative TMPDIR would stage the download inside
    # whatever directory the script happens to have been started from, and a
    # value beginning with '-' would reach mktemp, ls and rm as an option
    # rather than as a path.
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

    # A private directory is only as private as the directory holding it. In a
    # world-writable parent WITHOUT the sticky bit, any local user can rename
    # our directory out of the way and put their own in its place after it was
    # created, which reopens the window this function exists to close. /tmp
    # carries mode 1777 on every system this script supports for exactly that
    # reason, so this refuses rather than staging where it cannot defend.
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

    # Ten X's: GNU mktemp requires at least three in the template and OpenBSD's
    # requires at least six in its last component, so ten satisfies every
    # implementation this script can meet.
    local dir=""
    dir=$(mktemp -d "${parent}/rmp_install.XXXXXXXXXX" 2>/dev/null) || dir=""
    if [ -z "$dir" ] || [ ! -d "$dir" ]; then
        error "failed to create a private staging directory under ${parent}. Nothing was downloaded."
        return 1
    fi

    # Verify rather than assume, exactly as the checksum path does. mktemp is
    # resolved through PATH like every other tool here, and every byte this
    # script downloads is written inside whatever it returns, so a mktemp
    # earlier on PATH that hands back an existing, shared, or symlinked
    # directory would reinstate the defect in full. These checks are what makes
    # that refusable instead of silently accepted, and they are the fingerprint
    # of the attack described above: a directory pre-created by another user
    # belongs to that user, and one pre-created for this script to write into
    # must be reachable by users other than its owner.
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

# Download binary
# Download the release archive into the staging directory, verify it against the
# checksum the release publishes, extract the binary from it, and report the
# staged binary in STAGED_BINARY.
#
# Every byte it writes stays inside the caller's staging directory: the archive,
# the checksum file, the extraction, and the binary itself. Nothing is written
# to a path another local user can name in advance -- the fixed `/tmp/rmp` this
# once staged the extracted binary at was a second copy of the same defect the
# staging directory fixes, predictable and reachable by anyone from the moment
# it appeared until the moment it was installed (rmp task #309).
#
# Failures exit; there is no partially successful return, and no failure site
# cleans up after itself, because the EXIT trap removes the whole staging
# directory on every path out of this script.
download_binary() {
    local version="$1"
    local os="$2"
    local arch="$3"
    local staging="$4"
    local ext=""
    local archive_ext=""

    if [ "$os" = "windows" ]; then
        ext=".exe"
        archive_ext="zip"
    else
        archive_ext="tar.gz"
    fi

    # Build archive name and URL via shared helper.
    local archive_name="${BINARY_NAME}-${version}-${os}-${arch}.${archive_ext}"
    local download_url
    download_url=$(get_download_url "$version" "$os" "$arch")
    local tmp_file="${staging}/${BINARY_NAME}${ext}"

    info "Downloading ${BINARY_NAME} ${version} for ${os}/${arch}..."

    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        error "Neither curl nor wget is available. Please install one of them."
        exit 1
    fi

    if ! fetch_url "$download_url" "${staging}/${archive_name}"; then
        error "Failed to download from ${download_url}"
        error "Please check that the release exists for your platform"
        exit 1
    fi

    # INTEGRITY GATE. The archive is checked against the digest the release
    # publishes BEFORE it is extracted, so a corrupted or substituted download
    # never reaches tar, unzip, chmod or the installation directory.
    #
    # A checksum that cannot be downloaded is a failure, not a licence to
    # proceed unverified: every release publishes a .sha256 beside every
    # archive (.github/workflows/release.yml, step "Generate checksum"), so its
    # absence means the release is not the one this installer knows how to
    # install. What this check does and does not defend against is stated in
    # SPEC/DEPLOY.md Checksum Verification, and it matters: the checksum is
    # served from the same origin as the archive.
    local checksum_url
    checksum_url=$(get_checksum_url "$download_url")

    info "Verifying checksum..."
    if ! fetch_url "$checksum_url" "${staging}/${archive_name}.sha256"; then
        error "Failed to download the checksum from ${checksum_url}"
        error "Every release publishes a .sha256 beside each archive, and this installer does not install an archive it cannot verify."
        error "To install anyway, follow the manual installation steps in SPEC/DEPLOY.md and check the archive yourself."
        exit 1
    fi

    if ! verify_archive_checksum "${staging}/${archive_name}" "${archive_name}" "${staging}/${archive_name}.sha256"; then
        error "Refusing to install ${archive_name}. Nothing was extracted and nothing was written to the installation directory."
        exit 1
    fi

    # Extract the binary from archive
    if [ "$os" = "windows" ]; then
        # Windows releases ship a .zip holding rmp.exe at the archive root
        # (see .github/workflows/release.yml). Extracting it needs unzip. If
        # unzip is missing, install nothing and say so: moving the archive into
        # place would leave a .zip file sitting under the binary's name, which
        # fails only later and for no obvious reason.
        if ! command -v unzip >/dev/null 2>&1; then
            error "unzip is required to install ${BINARY_NAME} on ${os}, but it was not found."
            error "Install unzip and run this script again, or download and extract the archive manually:"
            error "  ${download_url}"
            exit 1
        fi
        if ! unzip -oq "${staging}/${archive_name}" -d "$staging" 2>/dev/null; then
            error "Failed to extract archive"
            exit 1
        fi
    else
        # Extract tar.gz
        if ! tar -xzf "${staging}/${archive_name}" -C "$staging" 2>/dev/null; then
            error "Failed to extract archive"
            exit 1
        fi
    fi

    # Both branches place the binary at the archive root, which IS the staged
    # path now that the staging directory is the extraction directory: there is
    # no move out to a second location, because that second location was the
    # fixed `/tmp/rmp` this task removed. What used to be checked by the exit
    # status of that move is checked here instead, so an archive that carried
    # no binary at its root still fails with the same message rather than
    # installing whatever happens to sit at the path.
    if [ ! -f "$tmp_file" ]; then
        error "Failed to extract binary from archive"
        exit 1
    fi

    STAGED_BINARY="$tmp_file"
}

# Install binary
install_binary() {
    local tmp_file="$1"
    local install_dir="$2"
    local scope="$3"
    local target_path="${install_dir}/${BINARY_NAME}"

    # Make binary executable
    chmod +x "$tmp_file"

    # Check if we need sudo for system installation
    if [ "$scope" = "system" ]; then
        if [ -w "$install_dir" ]; then
            mv "$tmp_file" "$target_path"
        else
            info "Elevated permissions required to install to ${install_dir}"
            if command -v sudo >/dev/null 2>&1; then
                sudo mv "$tmp_file" "$target_path"
            else
                error "Cannot write to ${install_dir}. Please run with appropriate permissions."
                exit 1
            fi
        fi
    else
        # User installation - no sudo needed
        mv "$tmp_file" "$target_path"
    fi

    success "Installed ${BINARY_NAME} to ${target_path}"
}

# Main installation flow
main() {
    echo "========================================"
    echo "  Groadmap Installer"
    echo "========================================"
    echo ""

    # Detect platform
    local os
    os=$(detect_os)
    local arch
    arch=$(detect_arch)

    # Both guards reject before anything is downloaded, and both report the raw
    # uname output rather than the mapped name, because the mapping is precisely
    # what failed. Messages per SPEC/DEPLOY.md § Platform Detection; the ERROR:
    # prefix comes from the error helper (see § Diagnostic Output).
    if [ "$os" = "unknown" ]; then
        error "operating system $(uname -s) is not supported. Supported systems: linux, darwin, freebsd, openbsd, windows. See SPEC/BUILD.md for the build matrix."
        exit 1
    fi

    # "unsupported" is a recognised architecture the build does not produce;
    # "unknown" is one detect_arch does not recognise at all. Both stop here.
    if [ "$arch" = "unsupported" ] || [ "$arch" = "unknown" ]; then
        error "architecture $(uname -m) is not supported. Supported targets: amd64, arm64, armv6, armv7. See SPEC/BUILD.md for the build matrix."
        exit 1
    fi

    # Third guard of the same kind, and it fires for the same reason the other
    # two do: refuse before anything is fetched rather than late. Without a
    # SHA-256 tool the download cannot be verified, and an unverified install is
    # not a degraded outcome this script offers -- see SPEC/DEPLOY.md Checksum
    # Verification for why this fails closed rather than warning and carrying on.
    if [ -z "$(sha256_tool)" ]; then
        error "no SHA-256 tool is available, so the download cannot be verified. Install one of sha256sum (GNU coreutils), shasum (Perl Digest::SHA) or openssl, or follow the manual installation steps in SPEC/DEPLOY.md and verify the archive yourself."
        exit 1
    fi

    # Fourth guard of the same kind, refusing for the same reason. Everything
    # this script downloads is staged in a directory mktemp creates:
    # unpredictable, private, and refused rather than reused when the name it
    # tried already exists (see create_staging_dir, and rmp task #309 for the
    # defect that made that necessary). Without mktemp there is no safe place
    # to stage a download, and staging one at a predictable path is precisely
    # what this replaced -- so it fails closed rather than falling back.
    # mktemp is present wherever this script can run at all: GNU coreutils on
    # Linux, the base system on macOS, FreeBSD and OpenBSD, and the MSYS2
    # coreutils package behind Git for Windows.
    if ! command -v mktemp >/dev/null 2>&1; then
        error "mktemp is required to stage the download privately, but it was not found. Install it (GNU coreutils on Linux; it is part of the base system on macOS, FreeBSD and OpenBSD), or follow the manual installation steps in SPEC/DEPLOY.md."
        exit 1
    fi

    info "Detected platform: ${os}/${arch}"
    if is_raspberry_pi; then
        info "Raspberry Pi detected"
    fi

    # Get versions
    local latest_version
    latest_version=$(get_latest_version)
    local current_version
    current_version=$(get_current_version)

    if [ -n "$current_version" ]; then
        info "Current version: ${current_version}"
        if [ "$current_version" = "$latest_version" ]; then
            success "Already up to date (${latest_version})"
            exit 0
        fi
        warn "Updating from ${current_version} to ${latest_version}"
    else
        info "Latest version: ${latest_version}"
    fi

    # Ask for installation scope
    local scope
    scope=$(ask_install_scope)

    # Determine install directory
    local install_dir
    install_dir=$(get_install_dir "$scope")

    info "Installation directory: ${install_dir}"

    # Everything downloaded from here on is staged inside this directory, and
    # the EXIT trap removes it on every path out of the script. Created after
    # the version is known and before anything is fetched, so a host that
    # cannot stage privately is told so with no archive requested.
    if ! STAGING_DIR=$(create_staging_dir); then
        exit 1
    fi

    # Download and install
    download_binary "$latest_version" "$os" "$arch" "$STAGING_DIR"
    install_binary "$STAGED_BINARY" "$install_dir" "$scope"

    # Verify installation
    echo ""
    if command -v "$BINARY_NAME" >/dev/null 2>&1; then
        local installed_version
        installed_version=$(get_current_version)
        success "Installation complete! Version: ${installed_version}"
        echo ""
        info "Run '${BINARY_NAME} --help' to get started"
    else
        warn "${BINARY_NAME} is installed but not available in your current PATH"
        echo ""

        if ! dir_in_path "$install_dir"; then
            info "To use ${BINARY_NAME}, add the following to your shell configuration:"
            echo ""
            echo "  export PATH=\"${install_dir}:\$PATH\""
            echo ""
            info "Then restart your shell or run: source ~/.bashrc (or ~/.zshrc, etc.)"
        else
            info "Please restart your shell or open a new terminal to use ${BINARY_NAME}"
        fi
    fi
}

# The staging directory is removed on every exit: normal, failed, or
# interrupted. The three signal traps exist so that a Ctrl-C at the scope
# prompt, a SIGTERM, or the terminal going away reaches the EXIT trap instead
# of killing the shell outright and leaving a downloaded archive behind; their
# exit codes follow the 128+signal convention.
trap cleanup_staging EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# Run main function
main "$@"
