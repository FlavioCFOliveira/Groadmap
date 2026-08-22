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

# Download binary
download_binary() {
    local version="$1"
    local os="$2"
    local arch="$3"
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
    local tmp_file="/tmp/${BINARY_NAME}${ext}"
    local tmp_dir="/tmp/rmp_install_$$"

    info "Downloading ${BINARY_NAME} ${version} for ${os}/${arch}..."

    # Create temp directory for extraction
    mkdir -p "$tmp_dir"

    # Reported before anything is fetched, and the temp directory goes with it:
    # there is nothing to clean up later if the script cannot download at all.
    if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
        rm -rf "$tmp_dir"
        error "Neither curl nor wget is available. Please install one of them."
        exit 1
    fi

    if ! fetch_url "$download_url" "$tmp_dir/${archive_name}"; then
        rm -rf "$tmp_dir"
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
    if ! fetch_url "$checksum_url" "$tmp_dir/${archive_name}.sha256"; then
        rm -rf "$tmp_dir"
        error "Failed to download the checksum from ${checksum_url}"
        error "Every release publishes a .sha256 beside each archive, and this installer does not install an archive it cannot verify."
        error "To install anyway, follow the manual installation steps in SPEC/DEPLOY.md and check the archive yourself."
        exit 1
    fi

    if ! verify_archive_checksum "$tmp_dir/${archive_name}" "${archive_name}" "$tmp_dir/${archive_name}.sha256"; then
        rm -rf "$tmp_dir"
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
            rm -rf "$tmp_dir"
            error "unzip is required to install ${BINARY_NAME} on ${os}, but it was not found."
            error "Install unzip and run this script again, or download and extract the archive manually:"
            error "  ${download_url}"
            exit 1
        fi
        if ! unzip -oq "$tmp_dir/${archive_name}" -d "$tmp_dir" 2>/dev/null; then
            rm -rf "$tmp_dir"
            error "Failed to extract archive"
            exit 1
        fi
        # Mirror the tar.gz branch below: the binary sits at the archive root.
        mv "$tmp_dir/${BINARY_NAME}${ext}" "$tmp_file" 2>/dev/null || {
            rm -rf "$tmp_dir"
            error "Failed to extract binary from archive"
            exit 1
        }
    else
        # Extract tar.gz
        if ! tar -xzf "$tmp_dir/${archive_name}" -C "$tmp_dir" 2>/dev/null; then
            rm -rf "$tmp_dir"
            error "Failed to extract archive"
            exit 1
        fi
        # Move binary to tmp_file location
        mv "$tmp_dir/${BINARY_NAME}" "$tmp_file" 2>/dev/null || {
            rm -rf "$tmp_dir"
            error "Failed to extract binary from archive"
            exit 1
        }
    fi

    # Cleanup temp directory
    rm -rf "$tmp_dir"

    echo "$tmp_file"
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
                rm -f "$tmp_file"
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

    # Download and install
    local tmp_file
    tmp_file=$(download_binary "$latest_version" "$os" "$arch")
    install_binary "$tmp_file" "$install_dir" "$scope"

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

# Run main function
main "$@"
