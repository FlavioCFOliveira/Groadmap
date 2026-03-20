# CI/CD Workflow Simplification Specification

## Overview

This specification defines the requirements for simplifying the GitHub Actions release workflow by removing the automatic GitHub Release creation while maintaining the build and artifact upload capabilities.

## Goals

1. Remove automatic GitHub Release creation from the release workflow
2. Preserve the build matrix for all supported platforms
3. Maintain artifact upload for manual download and workflow reuse
4. Ensure the workflow remains functional for CI/CD validation

## Changes Required

### File: `.github/workflows/release.yml`

#### Remove the `release` Job

The following job shall be removed from the workflow:

- Job name: `release`
- Job ID: `release`
- Purpose: Creates GitHub Release with built binaries

#### Keep the Following Jobs

1. **Job: `test`**
   - Purpose: Run pre-release validation (fmt, vet, tests)
   - Keep: All steps

2. **Job: `build`**
   - Purpose: Build binaries for all target platforms
   - Keep: All steps including artifact upload
   - Artifact naming: `release-{goos}-{goarch}{goarm}`

#### Workflow Trigger

Keep existing trigger:
```yaml
on:
  push:
    tags:
      - 'v*'
```

#### Permissions

Update permissions to remove unnecessary write permissions:

```yaml
permissions:
  contents: read
```

(Previously required `write` for release creation)

### Files NOT Affected

The following files remain unchanged:
- `.github/workflows/ci.yml` - CI workflow for pull requests
- `install.sh` - Installation script (continues to reference GitHub releases)
- All source code files

## Rationale

1. **Manual Release Control**: Releases will be created manually when needed, providing better control over release timing and notes
2. **Reduced Complexity**: Fewer automated steps reduce potential failure points
3. **Security**: Reduced permissions (no `contents: write` needed)
4. **Flexibility**: Artifacts remain available for download and can be used in other workflows or for manual distribution

## Accessing Built Binaries

After workflow completion, binaries will be available as:

1. **GitHub Actions Artifacts**: Download from the Actions tab for each workflow run
2. **Direct Download**: Via GitHub API for workflow artifacts
3. **Manual Release**: Can be attached to manually created releases

## Workflow State After Changes

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: read

jobs:
  test:
    # ... existing test job ...

  build:
    # ... existing build job ...
```

## Acceptance Criteria

1. [ ] Job `release` removed from `.github/workflows/release.yml`
2. [ ] Job `test` preserved and functional
3. [ ] Job `build` preserved with artifact upload
4. [ ] Permissions reduced to `contents: read`
5. [ ] Workflow triggers on tag push remain functional
6. [ ] All build targets (linux/amd64, linux/arm64, linux/armv6, linux/armv7, darwin/amd64, darwin/arm64, windows/amd64, windows/arm64) remain in matrix
7. [ ] YAML syntax validated
8. [ ] Workflow runs successfully on next tag push (build artifacts only, no release created)

## Migration Notes

- Existing releases in the repository are NOT affected
- The workflow will continue to trigger on tag pushes
- Manual releases can be created via GitHub UI or CLI (`gh release create`)
- Artifacts from workflow runs can be downloaded and manually attached to releases

## Related Documentation

- `SPEC/RASPBERRY_PI_SUPPORT.md` - Build matrix specification for ARM targets
- GitHub Actions documentation: https://docs.github.com/en/actions
