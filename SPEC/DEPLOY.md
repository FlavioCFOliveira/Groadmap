# Deployment and Installation Specification

## Overview

This specification defines the deployment process, installation methods, and platform detection for the Groadmap CLI.

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
- Installs to `/usr/local/bin` by default
- Supports custom installation directory

### 2. Manual Installation

Download binary from GitHub releases:
```bash
# Download for your platform
curl -LO https://github.com/FlavioCFOliveira/Groadmap/releases/download/v1.0.0/rmp-v1.0.0-linux-amd64.tar.gz

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

### Architecture Detection

The installation script detects architecture via `uname -m`:

| Output | Architecture | Binary Target |
|--------|--------------|---------------|
| x86_64, amd64 | amd64 | {goos}-amd64 |
| arm64, aarch64 | arm64 | {goos}-arm64 |
| armv6l, armv6 | armv6 | {goos}-armv6 |
| armv7l, armv7 | armv7 | {goos}-armv7 |
| i386, i686 | 386 | {goos}-386 |

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

### Functions

#### `detect_arch()`
Returns the architecture string for the current system.

**Returns:**
- `amd64` - x86_64 systems
- `arm64` - 64-bit ARM systems
- `armv6` - ARMv6 systems (Pi Zero/1)
- `armv7` - ARMv7 systems (Pi 2/3/4 32-bit)
- `386` - 32-bit x86 systems
- `unknown` - Unrecognized architecture

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

## Release Process

### Automated Release Creation

Releases are created automatically when a tag matching `v*` pattern is pushed:

1. Push git tag: `git tag -a v1.0.0 -m "Release v1.0.0"`
2. Push tag: `git push origin v1.0.0`
3. GitHub Actions workflow triggers automatically
4. Tests run, binaries are built for all platforms
5. GitHub Release is created automatically with all assets attached

### GitHub Actions Workflow

**File:** `.github/workflows/release.yml`

**Triggers:**
- Push of tags matching `v*` (e.g., `v1.0.0`, `v1.1.0`)

**Jobs:**

1. **Pre-release Tests**
   - Run on ubuntu-latest
   - Execute: `go fmt`, `go vet`, `go test -race`
   - Must pass before building

2. **Build Release Binaries**
   - Matrix builds for all platforms:
     - Linux: amd64, arm64, armv6, armv7
     - macOS: amd64, arm64
     - Windows: amd64, arm64
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
- Binary archives for all supported platforms (8 total)
- SHA256 checksums for each archive
- Automatic release notes generated from commits

### Release Checklist

- [ ] All binaries built successfully
- [ ] SHA256 checksums generated
- [ ] Release notes prepared
- [ ] Version updated in `cmd/rmp/main.go`
- [ ] Documentation updated (`SPEC/VERSION.md`, `SPEC/README.md`)

## Change History

| Date | Change | Description |
|------|--------|-------------|
| 2026-03-20 | Initial | Installation script with platform detection including Raspberry Pi |
| 2026-03-20 | Update | Added automated GitHub Release creation workflow triggered on tag push |

## Acceptance Criteria

### Installation Script
- [ ] Detects all supported architectures correctly
- [ ] Downloads correct binary for detected platform
- [ ] Installs binary with executable permissions
- [ ] Provides helpful error messages on failure

### Raspberry Pi Support
- [ ] Detects ARMv6 on Pi Zero/1
- [ ] Detects ARMv7 on Pi 2/3/4 (32-bit)
- [ ] Falls back to ARMv6 for generic ARM detection
- [ ] Can identify Raspberry Pi hardware

### Manual Installation
- [ ] Download URL format is correct
- [ ] Archives extract correctly
- [ ] Binary runs after manual installation
