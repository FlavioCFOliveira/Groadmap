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
    echo -e "${BLUE}INFO:${NC} $1"
}

success() {
    echo -e "${GREEN}SUCCESS:${NC} $1"
}

warn() {
    echo -e "${YELLOW}WARNING:${NC} $1"
}

error() {
    echo -e "${RED}ERROR:${NC} $1" >&2
}

prompt() {
    echo -e "${CYAN}PROMPT:${NC} $1"
}

# Detect OS
detect_os() {
    local os
    case "$(uname -s)" in
        Linux*)     os="linux" ;;
        Darwin*)    os="darwin" ;;
        CYGWIN*|MINGW*|MSYS*) os="windows" ;;
        *)          os="unknown" ;;
    esac
    echo "$os"
}

# Detect architecture
detect_arch() {
    local arch
    case "$(uname -m)" in
        x86_64|amd64)   arch="amd64" ;;
        arm64|aarch64)  arch="arm64" ;;
        i386|i686)      arch="386" ;;
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

    if [ "$os" = "windows" ]; then
        ext=".exe"
    fi

    local filename="${BINARY_NAME}_${version}_${os}_${arch}${ext}"
    local download_url="https://github.com/${REPO}/releases/download/${version}/${filename}"
    local tmp_file="/tmp/${BINARY_NAME}${ext}"

    info "Downloading ${BINARY_NAME} ${version} for ${os}/${arch}..."

    if command -v curl >/dev/null 2>&1; then
        if ! curl -fsSL -o "$tmp_file" "$download_url" 2>/dev/null; then
            error "Failed to download from ${download_url}"
            error "Please check that the release exists for your platform"
            exit 1
        fi
    elif command -v wget >/dev/null 2>&1; then
        if ! wget -qO "$tmp_file" "$download_url" 2>/dev/null; then
            error "Failed to download from ${download_url}"
            error "Please check that the release exists for your platform"
            exit 1
        fi
    else
        error "Neither curl nor wget is available. Please install one of them."
        exit 1
    fi

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

    if [ "$os" = "unknown" ]; then
        error "Unsupported operating system: $(uname -s)"
        exit 1
    fi

    if [ "$arch" = "unknown" ]; then
        error "Unsupported architecture: $(uname -m)"
        exit 1
    fi

    info "Detected platform: ${os}/${arch}"

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
