# Application Version Specification

## Current Version

| Component | Version | File |
|-----------|---------|------|
| Application | v1.1.0 | `cmd/rmp/main.go` |
| Database Schema | v1.0.0 | `internal/db/schema.go` |
| Specification | v1.1.0 | `SPEC/` |

## Versioning Strategy

### Application Version

The application version is defined in `cmd/rmp/main.go`:

```go
const version = "1.0.0"
```

This version is:
- Compiled into the binary at build time
- Displayed via `rmp --version`
- Used for release artifact naming (e.g., `rmp-v1.0.0-linux-amd64.tar.gz`)

### Database Schema Version

The database schema version is managed separately via `internal/db/schema.go`:

```go
const SchemaVersion = "1.0.0"
```

- Used for database migrations
- Stored in database `_metadata` table
- Independent from application version

### Specification Version

The technical specification has its own versioning:
- Defined in `SPEC/README.md`
- Tracks changes to the specification documents

## Semantic Versioning

Groadmap follows Semantic Versioning (SemVer):

```
vMAJOR.MINOR.PATCH

Example: v1.0.0
```

### Version Components

- **MAJOR**: Incompatible API changes or major architectural changes
- **MINOR**: New functionality, backward compatible
- **PATCH**: Bug fixes, backward compatible

## Version Independence

The three version numbers (application, schema, specification) are independent:

- Application can be v1.0.0 while Schema is v1.0.0
- Schema changes follow database migration requirements
- Specification changes track documentation evolution

This independence allows:
- Schema updates without application version bumps
- Documentation updates without code changes
- Clear separation of concerns

## Release Process

1. Update version constant in `cmd/rmp/main.go`
2. Update `SPEC/VERSION.md` with new version information
3. Update `SPEC/README.md` version and date
4. Create git tag: `git tag -a v1.0.0 -m "Release v1.0.0"`
5. Push tag: `git push origin v1.0.0`
6. Workflow builds binaries and uploads artifacts

## Version History

| Date | Application | Schema | Description |
|------|-------------|--------|-------------|
| 2026-03-20 | v1.1.0 | v1.0.0 | Added Claude Code skills documentation |
| 2026-03-20 | v1.0.0 | v1.0.0 | Initial release |
