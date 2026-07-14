# Knowledge Model

This file is the authoritative description of the shape of the Groadmap Knowledge Graph.
The file and the live graph must mirror each other: no label, edge type or property may
exist in one and not in the other. Whenever the graph gains a new label, edge type or
property, this file is updated in the same commit.

The graph is a Label Property Graph stored by GoGraph at `~/.roadmaps/groadmap/graph/` and
is reached only through `rmp graph` (`query`, `search`, `create`, `update`, `delete`).
Groadmap models itself: the project described by the graph is this repository.

## Conventions

**Keys.** Every node carries a `key` property that is globally unique across the whole
graph, so `MATCH (n {key:'...'})` without a label is unambiguous. The key is the natural
identifier of the artefact: a repository-relative file path for code, tests and specs, a
package path for components, a slug for requirements, releases and memories.

**Provenance.** Every node and every edge carries the commit at which it was last
confirmed to be true:

| Property | Type | Meaning |
|---|---|---|
| `last_commit` | string | Full 40-character SHA of the commit at which the element was last confirmed. |
| `last_commit_date` | string | Calendar date of that commit, ISO 8601, `YYYY-MM-DD`. |

For nodes backed by a file (`CodeFile`, `Test`, `Spec`) the confirmed commit is the last
commit that touched the file, as reported by `git log -1 -- <path>`. For `Component` it is
the last commit that touched the package directory. For `Requirement` it is the most recent
commit among the artefacts the requirement is linked to. For an edge it is the commit at
which the relationship itself was last verified to hold.

Provenance is distinct from an artefact's own facts. A `Release` legitimately owns the
commit it was cut from, and a `Memory` owns the commit at which its content was recorded;
those are stored under their own property names (`commit`/`date` and
`source_commit`/`source_date`) and never under `last_commit`.

## Node labels

### Component

A unit of the architecture: a Go package of this module, an external Go module the project
depends on, or a third-party web asset vendored into the binary.

| Property | Required | Notes |
|---|---|---|
| `key` | yes | Package path (`internal/db`), module path (`github.com/FlavioCFOliveira/GoGraph`), or vendored-project name (`tabler`). |
| `path` | yes | Package path, module path, or the repository path of the vendored asset. |
| `kind` | yes | `package` or `external-dependency`. |
| `language` | yes | `Go` for packages and Go modules; for vendored web assets, the comma-separated languages they ship (`CSS,JavaScript`, `CSS,Webfont`, `JavaScript`). |
| `version` | no | Pinned version. Omitted when upstream declares none, as the Inter webfont does; never inferred. |
| `licence` | no | Upstream licence of an `external-dependency`, as recorded in `internal/web/static/vendor/LICENSES.md`. |
| `summary` | no | What the component is and what it owns. |
| `released_in` | no | Release tag that first shipped the pinned version. |
| `release_commit`, `release_date` | no | Commit and date at which the pinned version was adopted. The dependency's own facts, not provenance. |
| `last_commit`, `last_commit_date` | yes | Provenance. |

Third-party code is never a `CodeFile`. The files vendored under
`internal/web/static/vendor/` (Tabler, Tabler Icons, Inter, D3, d3-sankey) are modelled as
`external-dependency` components that `internal/web` depends on, which keeps project-authored
source and third-party source distinguishable. See `SPEC/BUILD.md` section Vendored Web Assets.

### CodeFile

A non-test source file authored by the project. Test sources are `Test` nodes, never
`CodeFile`; vendored third-party files are `Component`s, never `CodeFile`.

| Property | Required | Notes |
|---|---|---|
| `key` | yes | Repository-relative path. |
| `path` | yes | Same as `key`. |
| `file` | yes | Base name. |
| `package` | yes | Owning component's path. |
| `language` | yes | `Go`, `Python`, `HTML`, `CSS`, `JavaScript` or `SVG`. |
| `last_commit`, `last_commit_date` | yes | Provenance. |

### Spec

One specification document under `SPEC/`.

| Property | Required | Notes |
|---|---|---|
| `key` | yes | File name, e.g. `DATABASE.md`. |
| `path` | yes | Repository-relative path. |
| `area` | yes | Functional area the document owns, per CLAUDE.md section 2. |
| `summary` | yes | One-line description of what the document specifies. |
| `last_commit`, `last_commit_date` | yes | Provenance. |

### Test

An executable check of the project's behaviour, or a named contract that a set of checks
enforces.

| Property | Required | Notes |
|---|---|---|
| `key` | yes | Repository-relative path, or a slug for a contract test. |
| `kind` | yes | `unit` (Go `*_test.go`), `e2e` (Python module under `tests/`), or `contract` (a named invariant enforced across several checks). |
| `path` | no | Present for file-backed tests; absent for `contract` tests. |
| `name` | no | Base name, for file-backed tests. |
| `summary` | no | What the test asserts. Expected on `contract` tests, which have no file to read. |
| `runner_registered` | no | `e2e` only: `true` when the module is registered in `tests/run_tests.py`, which `assert_no_dormant_modules` enforces. |
| `last_commit`, `last_commit_date` | yes | Provenance. |

### Requirement

A capability the project provides. Requirements are the hinge of traceability: they are
specified by a `Spec`, implemented by `CodeFile`s and verified by `Test`s.

| Property | Required | Notes |
|---|---|---|
| `key` | yes | Slug, e.g. `graph-guardrail`. |
| `title` | yes | Human-readable name of the capability. |
| `status` | yes | `implemented` or `planned`. |
| `area` | no | Functional area, matching a `Spec.area`. |
| `summary` | no | Longer description of the capability. |
| `rmp_task` | no | Integer id of the `rmp` task that delivered the capability. The task itself lives in the roadmap database, not in the graph. |
| `last_commit`, `last_commit_date` | yes | Provenance. |

### Release

A published version of the binary.

| Property | Required | Notes |
|---|---|---|
| `version` | yes | Semantic version, e.g. `v1.13.2`. |
| `tag` | yes | Git tag. |
| `type` | yes | `major`, `minor` or `patch`. |
| `commit` | yes | Commit the release was cut from. This is the release's own fact, not provenance. |
| `date` | yes | Release date, `YYYY-MM-DD`. |
| `summary` | yes | What the release delivered. |
| `url` | no | Published release URL. |
| `published`, `published_at`, `assets` | no | Publication state. |
| `last_commit`, `last_commit_date` | yes | Provenance. |

### Memory

The project's prose memory: a durable, non-obvious fact that would otherwise have to be
rediscovered. Per CLAUDE.md section 5 this layer is the only memory the project keeps.

| Property | Required | Notes |
|---|---|---|
| `key` | yes | Slug prefixed `mem-`. |
| `summary` | yes | One-line statement of the fact, used to judge relevance during recall. |
| `body` | yes | The fact in full. |
| `title` | no | Human-readable name. |
| `type` | no | Category of the memory. |
| `source_commit`, `source_date` | no | Commit and date at which the fact was recorded. The memory's own fact, not provenance. |
| `last_commit`, `last_commit_date` | yes | Provenance. |

## Edge types

Every edge carries `last_commit` and `last_commit_date`.

| Edge | From | To | Meaning |
|---|---|---|---|
| `PART_OF` | `CodeFile`, `Test` | `Component` | The file belongs to the component that owns its directory. Derived from the filesystem. A `contract` test has no file and therefore no `PART_OF`. |
| `DEPENDS_ON` | `Component` | `Component` | The component imports the other. Derived from the real import graph (`go list`). |
| `DEPENDS_ON` | `Requirement` | `Requirement` | The capability cannot work without the other. |
| `TESTS` | `Test` | `Component` | The test exercises the component. A `unit` test exercises the component it belongs to; an `e2e` test belongs to the `tests` harness component but exercises `cmd/rmp`, the binary it drives as a black box. |
| `SPECIFIES` | `Spec` | `Requirement` | The document specifies the capability. |
| `IMPLEMENTED_BY` | `Requirement` | `CodeFile` | The file implements the capability. |
| `VERIFIED_BY` | `Requirement` | `Test` | The test verifies the capability. |
| `SPECIFIED_BY` | `Requirement` | `Spec` | Inverse of `SPECIFIES`, used where the requirement is the natural starting point. |
| `FULFILS` | `Requirement` | `Memory` | The capability is the subject of the recorded memory. |
| `INCLUDES` | `Release` | `Memory` | The release is the subject of the recorded memory. |
| `NEXT_RELEASE` | `Release` | `Release` | Chronological succession of releases. |
| `SEE_ALSO` | `Memory` | `Memory`, `Spec` | Cross-reference from a memory to related knowledge. |

## Core traceability chain

```
Spec -[:SPECIFIES]-> Requirement -[:IMPLEMENTED_BY]-> CodeFile -[:PART_OF]-> Component
                     Requirement -[:VERIFIED_BY]---> Test     -[:PART_OF]-> Component
```

A requirement with no `IMPLEMENTED_BY` edge is not implemented; a requirement with no
`VERIFIED_BY` edge is not tested. Both are defects in the graph or in the project, and the
graph is expected to make them visible rather than hide them.

## Maintenance

The graph is maintained incrementally: there is no rebuild generator. After every commit,
the nodes and edges the commit touched have their provenance updated, new artefacts are
merged in, and removed artefacts are detached and deleted. Only facts that are requested,
defined or verified are stored; the graph never records inferences presented as facts.
