# Knowledge Model

This file is the authoritative description of the shape of the Groadmap Knowledge Graph.
The file and the live graph must mirror each other: no label, edge type or property may
exist in one and not in the other. Whenever the graph gains a new label, edge type or
property, this file is updated in the same commit.

The graph is a Label Property Graph stored by GoGraph at `~/.roadmaps/groadmap/graph/` and
is reached only through `rmp graph` (`execute`, `serve`, `client`). The five subcommands
`query`, `search`, `create`, `update` and `delete` were removed at commit 40d1b37 and exit 127;
`rmp graph execute -r <roadmap> [-q <cypher>]` replaces all of them and reads the statement from
standard input when `-q` is absent.
Groadmap models itself: the project described by the graph is this repository.

## Conventions

**Keys.** Every node carries a `key` property. The key is the natural identifier of the
artefact: a repository-relative file path for code, tests and specs, a package path for
components, a slug for requirements, releases and memories.

Every node's `key` is unique across the whole graph, so that `MATCH (n {key:'...'})`
without a label is unambiguous. That uniqueness is a **convention whoever writes to this
graph must honour, not a rule the product enforces**: no `rmp` command rejects, rewrites,
or reports a second node carrying a key that is already in use. `SPEC/GRAPH.md` section
Node Key Uniqueness is canonical for the invariant, for the comparison that decides it,
and for the audit that detects a violation; this file does not restate it. Two of its
consequences bear directly on writing a key here:

- Two keys are the same key when their Unicode NFC forms are equal, and the stored key is
  exactly the bytes supplied — normalisation is for comparison only. Writing one key in
  two normalisation forms therefore creates two nodes that render identically, and
  `MATCH (n {key:'...'})` binds whichever of the two the caller happened to spell.
- Because nothing enforces the convention, `MATCH (n {key:'...'})` is unambiguous only
  while the convention holds. Checking that it still holds is the audit's job, not the
  product's.

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

A stamp is a claim, not a formality: it asserts that somebody confirmed the element against
the repository at that commit. It follows that an element whose truth could not be
established MUST be left unstamped rather than given a plausible commit. **An absent
`last_commit` therefore means UNVERIFIED, and never "overlooked"**: it is the one honest
way the graph can say it does not know. Every node carries provenance; an edge may lack it,
and where one does, the reason is recorded in a `Memory` so the gap is a decision a reader
can find rather than a silence. Never bulk-stamp elements to make a completeness query come
out clean: that converts an admission of ignorance into a false assertion, which is worse
than the gap it hides.

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

Build and deployment artefacts are `CodeFile`s on the same terms as program source. The
`Makefile`, `install.sh`, the workflow files under `.github/workflows/`, and `.gitignore`
are each a file the project authored and maintains, each realises a requirement the SPEC states, and
each is verified by a test; nothing about them justifies a label of their own. Their
`package` is the directory that owns them -- `.` for a repository-root file, and
`.github/workflows` for a workflow -- rather than a Go import path, because `package` on
this label means "the component this file belongs to" and not "a compilation unit".

| Property | Required | Notes |
|---|---|---|
| `key` | yes | Repository-relative path. |
| `path` | yes | Same as `key`. |
| `file` | yes | Base name. |
| `package` | yes | Owning component's path. |
| `language` | yes | `Go`, `Python`, `HTML`, `CSS`, `JavaScript`, `SVG`, `Bash`, `YAML`, `Make` or `Gitignore`. The last four are the build and deployment artefacts described above. |
| `last_commit`, `last_commit_date` | yes | Provenance. |

### Spec

One specification document under `SPEC/`.

| Property | Required | Notes |
|---|---|---|
| `key` | yes | Repository-relative path, e.g. `SPEC/DATABASE.md`. |
| `path` | yes | Same as `key`. |
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
| `key` | yes | Slug prefixed `release-`, followed by `version`, e.g. `release-1.13.2`. |
| `version` | yes | Semantic version, without the tag's leading `v`, e.g. `1.13.2`. The `v` belongs to `tag`. |
| `tag` | yes | Git tag, e.g. `v1.13.2`. |
| `type` | yes | `major`, `minor` or `patch`. |
| `commit` | yes | Commit the release was cut from, and the commit the tag names. This is the release's own fact, not provenance. |
| `tag_commit` | no | Commit the tag resolves to. Recorded when a release was tagged more than once, so the node states which attempt shipped. |
| `merge_commit_develop` | no | Commit of the back-merge into `develop` that closed the release branch. |
| `date` | yes | Release date, `YYYY-MM-DD`. |
| `summary` | yes | What the release delivered. |
| `url` | no | Published release URL. |
| `published`, `published_at`, `assets` | no | Publication state. |
| `verified` | no | What was checked against the *published* artefacts after the release went out: checksums, archive contents, the version the shipped binary reports, and the release workflow run. The release's own fact, not provenance, and distinct from the validation gates that ran before the tag. |
| `last_commit`, `last_commit_date` | yes | Provenance. |

### Doc

A prose document that explains the project to a reader, as opposed to specifying it. The
criterion is the document's purpose, not its location: the per-command pages under
`DOCS/commands/`, the repository `README.md`, `CHANGELOG.md`, and `tests/README.md` are all
`Doc`s today. Distinct from `Spec`, which is the project's technical specification: the two
have different audiences and different owners (`doc-manager` versus
`specification-manager`). The key is the repository-relative path, the same rule every
file-anchored label follows.

`CHANGELOG.md` is a `Doc` and not a `Release`. A `Release` is one published version of the
binary; the changelog is the prose a reader consults to learn what changed, it carries the
`Unreleased` section that no release owns yet, and it is maintained by `doc-manager` along
with the rest of the user-facing documentation.

| Property | Required | Notes |
|---|---|---|
| `key` | yes | Repository-relative path, e.g. `DOCS/commands/task.md`. |
| `path` | yes | Same as `key`. |
| `file` | yes | Base name. |
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

Every edge carries `last_commit` and `last_commit_date`, except where the relationship could
not be verified; see Provenance above, where an absent stamp means unverified and its
reason is recorded in a `Memory`.

| Edge | From | To | Meaning |
|---|---|---|---|
| `PART_OF` | `CodeFile`, `Test` | `Component` | The file belongs to the component that owns its directory. Derived from the filesystem. A `contract` test has no file and therefore no `PART_OF`. |
| `DEPENDS_ON` | `Component` | `Component` | The component imports the other. Derived from the real import graph (`go list`). |
| `DEPENDS_ON` | `Requirement` | `Requirement` | The capability cannot work without the other. |
| `TESTS` | `Test` | `Component` | The test exercises the component. A `unit` test exercises the component it belongs to; an `e2e` test belongs to the `tests` harness component but exercises `cmd/rmp`, the binary it drives as a black box. |
| `SPECIFIES` | `Spec` | `Requirement` | The document specifies the capability. This is the single canonical direction: there is deliberately no inverse edge type, so a query never has to test both ways round. |
| `IMPLEMENTED_BY` | `Requirement` | `CodeFile`, `Doc` | The artefact implements the capability. |
| `VERIFIED_BY` | `Requirement` | `Test` | The test verifies the capability. |
| `FULFILS` | `Requirement` | `Memory` | The capability is the subject of the recorded memory. |
| `INCLUDES` | `Release` | `Memory` | The release is the subject of the recorded memory. |
| `NEXT_RELEASE` | `Release` | `Release` | Chronological succession of releases. |
| `SEE_ALSO` | `Memory` | `Memory`, `Spec`, `Component` | Cross-reference from a memory to related knowledge. A `Component` target records that the memory carries knowledge about that component which the component's own properties cannot hold, such as a pinning constraint no validation gate can check. |
| `SEE_ALSO` | `Requirement` | `Requirement` | Cross-reference between two capabilities that meet on the same surface, so that reading about one should lead to the other. It asserts no necessity: that is `DEPENDS_ON`, which states that a capability cannot work without the other, and the two must not be conflated. It is also not derivable from a shared `IMPLEMENTED_BY` target, because the shared artefacts are the large ones, and a join on them relates every capability that happens to touch the same file. |

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
