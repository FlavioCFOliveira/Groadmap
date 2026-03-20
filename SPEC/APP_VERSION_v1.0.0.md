# Application Version Update Specification

## Overview

This specification defines the requirements for updating the application version identifier across the Groadmap project to v1.0.0.

## Current State

The application version is defined in the following locations:

1. **Source Code:** `cmd/rmp/main.go` - Line 39
   - Current value: `version = "1.0.0"` (already correct)

2. **Documentation:** `SPEC/README.md` - Line 35
   - Current value: "1.1.0 (Migrated to Go)" (needs update)

## Versioning Scheme

Groadmap follows **Semantic Versioning (SemVer)**:
- Format: `MAJOR.MINOR.PATCH`
- Example: `v1.0.0`

### Version Components

- **MAJOR**: Incompatible API changes
- **MINOR**: New functionality, backward compatible
- **PATCH**: Bug fixes, backward compatible

## Target Version

**v1.0.0** - First stable release of Groadmap CLI

## Changes Required

### 1. Source Code Verification

**File:** `cmd/rmp/main.go`

```go
const (
    version = "1.0.0"
    appName = "Groadmap"
)
```

**Action:** Verify the version is set to "1.0.0" (already correct)

### 2. Documentation Update

**File:** `SPEC/README.md` - Line 35

**Current:**
```markdown
- Current version: 1.1.0 (Migrated to Go)
- Date: 2026-03-16
```

**New:**
```markdown
- Current version: 1.0.0
- Date: 2026-03-20
```

### 3. Version Specification Document

**File:** `SPEC/VERSION.md` (new file)

Create a new specification file documenting the versioning strategy:

```markdown
# Application Version Specification

## Current Version

- Application: v1.0.0
- Schema: v1.2.0 (see SPEC/DATABASE.md)
- Specification: v1.1.0 (see SPEC/README.md)

## Versioning Strategy

### Application Version

The application version is defined in `cmd/rmp/main.go`:

```go
const version = "1.0.0"
```

This version is:
- Compiled into the binary at build time
- Displayed via `rmp --version`
- Used for release artifacts naming

### Schema Version

The database schema version is managed separately via `internal/db/schema.go`:
- Current: SchemaVersion = "1.2.0"
- Used for database migrations

### Specification Version

The technical specification has its own versioning:
- Current: 1.1.0 (as defined in SPEC/README.md)

## Release Process

1. Update version in `cmd/rmp/main.go`
2. Update SPEC/VERSION.md
3. Tag release: `git tag -a v1.0.0 -m "Release v1.0.0"`
4. Push tag: `git push origin v1.0.0`
```

## Files to Modify

| File | Change Type | Description |
|------|-------------|-------------|
| `cmd/rmp/main.go` | Verify | Confirm version is "1.0.0" |
| `SPEC/README.md` | Update | Change version from 1.1.0 to 1.0.0 |
| `SPEC/README.md` | Update | Update date to 2026-03-20 |
| `SPEC/VERSION.md` | Create | New version specification document |

## Acceptance Criteria

1. [ ] `cmd/rmp/main.go` defines version as "1.0.0"
2. [ ] `rmp --version` outputs "Groadmap version 1.0.0"
3. [ ] `SPEC/README.md` references version 1.0.0
4. [ ] `SPEC/VERSION.md` exists with complete versioning documentation
5. [ ] All references to application version are consistent

## Notes

- Schema version (1.2.0) and application version (1.0.0) are independent
- The application can be at v1.0.0 while the database schema is at v1.2.0
- This is intentional as schema changes follow a different lifecycle than application releases
