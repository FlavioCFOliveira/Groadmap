# Application Version Specification

## Current Version

| Component | Version | File |
|-----------|---------|------|
| Application | v1.2.1 | `cmd/rmp/main.go` |
| Database Schema | v1.6.0 | `internal/db/schema.go` |

The SPEC itself is not versioned (see `SPEC/README.md` and `CLAUDE.md` § Versioning Policy). Git tags are the canonical record of past application and schema versions.

## Versioning Strategy

### Application Version

The application version is defined in `cmd/rmp/main.go`:

```go
const version = "1.2.1"
```

This version is:
- Compiled into the binary at build time
- Displayed via `rmp --version`
- Used for release artefact naming (e.g., `rmp-v1.2.1-linux-amd64.tar.gz`)

### Database Schema Version

The database schema version is managed separately via `internal/db/schema.go`:

```go
const SchemaVersion = "1.6.0"
```

- Used for database migrations
- Stored in database `_metadata` table
- Independent from application version

## Semantic Versioning

Groadmap follows Semantic Versioning (SemVer):

```
vMAJOR.MINOR.PATCH

Example: v1.2.1
```

### Version Components

- **MAJOR**: Incompatible API changes or major architectural changes
- **MINOR**: New functionality, backward compatible
- **PATCH**: Bug fixes, backward compatible

## Version Independence

The application version and schema version are independent:

- Application version follows release cadence and SemVer
- Schema version increments only when a migration is added in `internal/db/migrations.go`
- Schema updates can happen without application version bumps and vice versa

## Release Process

1. Update the version constant in `cmd/rmp/main.go`
2. Update the `Current Version` table in this file
3. Create the git tag: `git tag -a v<version> -m "Release v<version>"`
4. Push the tag: `git push origin v<version>`
5. The release workflow builds binaries and uploads artefacts

Past releases are discoverable via `git tag --list` and `git log v<previous>..v<current>` — no Version History table is kept here.
