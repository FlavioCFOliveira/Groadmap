# Raspberry Pi Support Specification

## Overview

This specification defines the requirements for adding Raspberry Pi support to the Groadmap project through cross-compilation in GitHub Actions and enhanced platform detection in the installation script.

## Goals

1. Enable automated builds for Raspberry Pi architectures (ARMv6, ARMv7)
2. Ensure the installation script correctly detects Raspberry Pi devices
3. Provide clear documentation for Raspberry Pi users

## Supported Raspberry Pi Models

| Model | Architecture | GOARM | Target Name |
|-------|--------------|-------|-------------|
| Raspberry Pi Zero / Zero W | ARMv6 | 6 | linux-armv6 |
| Raspberry Pi 1 | ARMv6 | 6 | linux-armv6 |
| Raspberry Pi 2 | ARMv7 | 7 | linux-armv7 |
| Raspberry Pi 3 (32-bit OS) | ARMv7 | 7 | linux-armv7 |
| Raspberry Pi 3 (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |
| Raspberry Pi 4 (32-bit OS) | ARMv7 | 7 | linux-armv7 |
| Raspberry Pi 4 (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |
| Raspberry Pi 5 (64-bit OS) | ARMv8 | N/A (arm64) | linux-arm64 |

## Technical Requirements

### 1. GitHub Actions Workflow Changes

#### New Build Targets

Add the following matrix entries to the release workflow:

```yaml
- goos: linux
  goarch: arm
  goarm: 6
  target: linux-armv6
- goos: linux
  goarch: arm
  goarm: 7
  target: linux-armv7
```

#### Build Configuration

For ARM builds, the following environment variables must be set:

```yaml
env:
  GOOS: ${{ matrix.goos }}
  GOARCH: ${{ matrix.goarch }}
  GOARM: ${{ matrix.goarm }}
  CGO_ENABLED: 0
```

#### Archive Naming Convention

- ARMv6: `rmp-${version}-linux-armv6.tar.gz`
- ARMv7: `rmp-${version}-linux-armv7.tar.gz`

### 2. Installation Script Changes

#### Architecture Detection Enhancement

The `detect_arch()` function must be enhanced to detect ARM variants:

```bash
detect_arch() {
    local arch
    case "$(uname -m)" in
        x86_64|amd64)   arch="amd64" ;;
        arm64|aarch64)  arch="arm64" ;;
        armv6l|armv6)   arch="armv6" ;;
        armv7l|armv7)   arch="armv7" ;;
        arm*)           # Fallback for generic ARM
            # Try to determine ARM version from CPU info
            if [ -f /proc/cpuinfo ]; then
                if grep -q "ARMv7" /proc/cpuinfo 2>/dev/null; then
                    arch="armv7"
                elif grep -q "ARMv6" /proc/cpuinfo 2>/dev/null; then
                    arch="armv6"
                else
                    # Default to armv6 for compatibility (lowest common denominator)
                    arch="armv6"
                fi
            else
                arch="armv6"
            fi
            ;;
        i386|i686)      arch="386" ;;
        *)              arch="unknown" ;;
    esac
    echo "$arch"
}
```

#### Platform Detection for Raspberry Pi

Add a function to detect if running on Raspberry Pi:

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

### 3. Release Notes Template Update

The release notes must include the new ARM targets:

```markdown
| Plataforma | Arquitetura | Ficheiro |
|------------|-------------|----------|
| Linux | amd64 | rmp-${version}-linux-amd64.tar.gz |
| Linux | arm64 | rmp-${version}-linux-arm64.tar.gz |
| Linux | armv6 | rmp-${version}-linux-armv6.tar.gz |
| Linux | armv7 | rmp-${version}-linux-armv7.tar.gz |
| macOS | amd64 | rmp-${version}-darwin-amd64.tar.gz |
| macOS | arm64 | rmp-${version}-darwin-arm64.tar.gz |
| Windows | amd64 | rmp-${version}-windows-amd64.zip |
| Windows | arm64 | rmp-${version}-windows-arm64.zip |
```

## File Modifications

### Files to Modify

1. `.github/workflows/release.yml`
   - Add ARMv6 and ARMv7 to build matrix
   - Update archive creation logic for ARM variants
   - Update release notes template

2. `install.sh`
   - Enhance `detect_arch()` function
   - Add Raspberry Pi detection
   - Update download URL construction for ARM variants

## Acceptance Criteria

1. GitHub Actions successfully builds binaries for:
   - [ ] linux/armv6
   - [ ] linux/armv7

2. Installation script correctly:
   - [ ] Detects ARMv6 architecture (`armv6l`)
   - [ ] Detects ARMv7 architecture (`armv7l`)
   - [ ] Falls back to ARMv6 for generic ARM detection
   - [ ] Downloads correct binary for detected architecture

3. Release artifacts:
   - [ ] Include `rmp-{version}-linux-armv6.tar.gz`
   - [ ] Include `rmp-{version}-linux-armv7.tar.gz`
   - [ ] Include SHA256 checksums for both

4. Documentation:
   - [ ] Release notes list ARMv6 and ARMv7 binaries
   - [ ] Installation instructions mention Raspberry Pi support

## Testing Strategy

1. **Build Verification**: Confirm binaries are built without errors
2. **Architecture Verification**: Use `file` command to verify binary architecture
3. **Installation Testing**: Test install script on actual Raspberry Pi devices
4. **Runtime Testing**: Verify binary executes correctly on target hardware

## Compatibility Notes

- ARMv6 binaries are compatible with all Raspberry Pi models (backward compatible)
- ARMv7 binaries offer better performance on Pi 2/3/4 but won't run on Pi Zero/1
- ARMv8 (arm64) is already supported and should be used for 64-bit Raspberry Pi OS
