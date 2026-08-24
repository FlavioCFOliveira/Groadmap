#!/usr/bin/env python3
"""
Base test class for Groadmap CLI tests.
Provides common utilities and setup/teardown functionality.
"""

import re
import subprocess
import json
import os
import tempfile
import shutil
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import List, Optional, Dict, Any, Tuple, Union


# --------------------------------------------------------------------------
# CLI binary resolution (module scope)
#
# The suite must drive the binary that matches the source tree it exercises,
# never a stale or foreign one. Resolution therefore happens ONCE per
# interpreter process, at module scope rather than per GroadmapTestBase
# instance: GroadmapTestBase.__init__ runs once per test CLASS (a module
# typically defines several), so instance-scoped resolution would silently
# repeat the staleness check -- and the startup banner -- many times per
# module run. resolve_cli() memoizes into _RESOLVED_CLI and every
# GroadmapTestBase instance in the process reuses that one result.
REPO_ROOT = Path(__file__).resolve().parent.parent

# Directories that never hold anything compiled into the binary, so a file
# changed under them must not be mistaken for a reason to rebuild: .git and
# .claude are tooling (the latter includes agent scratch worktrees), bin/ is
# build OUTPUT, and tests/ is this Python suite, not Go source or an embedded
# asset.
_EXCLUDED_SOURCE_DIR_NAMES = frozenset({".git", ".claude", "bin", "tests", "vendor", "node_modules"})

# A //go:embed directive comment, anchored to the start of its line (the
# grammar gofmt always produces for a directive immediately above a
# declaration) so a line that merely MENTIONS "//go:embed" inside prose --
# such as the docstrings in this very file -- is never mistaken for one.
_GO_EMBED_DIRECTIVE = re.compile(r"^//go:embed[ \t]+(.+?)\s*$", re.MULTILINE)

_RESOLVED_CLI: Optional[str] = None


def _embedded_roots(repo_root: Path) -> List[Path]:
    """Discover the directories //go:embed compiles into the binary.

    internal/web/embed.go embeds `templates/*.html` and `static` (its whole
    tree, vendored JS/CSS included) into the rmp binary via html/template and
    http.FileServer; editing one of those files changes the binary's runtime
    behaviour while leaving every .go file's mtime untouched, so a .go-only
    staleness check would certify a binary that no longer matches the page
    it renders.

    This derives the embedded roots instead of hardcoding them, because
    Go's own //go:embed grammar is simple enough that a general parser is
    not needed to read it correctly: a directive is one or more
    whitespace-separated patterns with no quoting mechanism at all (see the
    `embed` package docs), so splitting on whitespace is not a heuristic,
    it is the grammar. For each pattern, the root handed back is everything
    before the first glob metacharacter (*, ?, [), resolved against the
    directory the directive lives in. Walking that whole root -- rather than
    replicating the glob -- is a deliberately conservative approximation: it
    can make an unrelated sibling file trigger a rebuild, but it can never
    miss a real one, and an extra rebuild is a cost this test harness can
    always afford. Should a future directive turn out not to reduce this
    cleanly (a glob spanning a "/", an exotic quoting need CPython's
    tokenizer can't approximate), the fix is to special-case that one
    pattern here, not to grow this into a general go:embed parser.

    Best-effort throughout: a pattern that cannot be read, or whose target
    no longer exists, is skipped rather than raising. A diagnostic mtime
    scan must never be the reason the suite cannot run; the worst case is
    silently falling back to the .go-only comparison for that one root,
    exactly the coverage this suite had before this function existed.
    """
    roots: List[Path] = []
    for go_file in repo_root.rglob("*.go"):
        if any(
            part in _EXCLUDED_SOURCE_DIR_NAMES
            for part in go_file.relative_to(repo_root).parts[:-1]
        ):
            continue
        try:
            text = go_file.read_text(encoding="utf-8", errors="ignore")
        except OSError:
            continue

        for match in _GO_EMBED_DIRECTIVE.finditer(text):
            for pattern in match.group(1).split():
                if pattern.startswith("all:"):
                    pattern = pattern[len("all:"):]
                glob_pos = next((i for i, c in enumerate(pattern) if c in "*?["), None)
                prefix = pattern if glob_pos is None else pattern[:glob_pos]
                if glob_pos is not None and "/" not in prefix:
                    # The wildcard sits in the pattern's first path segment
                    # with no directory prefix (e.g. a bare "*.html"): there
                    # is no distinct subdirectory to derive from it, so this
                    # pattern is skipped rather than mis-deriving a fake one.
                    continue
                literal = prefix if glob_pos is None else prefix.rsplit("/", 1)[0]
                if not literal:
                    continue
                root = (go_file.parent / literal).resolve()
                if root.exists():
                    roots.append(root)
    return roots


def _newest_source_mtime(repo_root: Path) -> float:
    """Return the mtime of the most recently modified file compiled into the
    binary: every .go file in the module, plus every file under a directory
    a //go:embed directive pulls in (see _embedded_roots()).

    Walks the whole repository (skipping _EXCLUDED_SOURCE_DIR_NAMES) rather
    than hardcoding cmd/ and internal/, so a future Go source directory is
    covered automatically without another change here. Returns 0.0 when
    nothing is found, which can never make a binary look stale by itself.
    """
    newest = 0.0

    for path in repo_root.rglob("*.go"):
        if any(part in _EXCLUDED_SOURCE_DIR_NAMES for part in path.relative_to(repo_root).parts[:-1]):
            continue
        try:
            mtime = path.stat().st_mtime
        except OSError:
            continue
        if mtime > newest:
            newest = mtime

    for root in _embedded_roots(repo_root):
        candidates = [root] if root.is_file() else list(root.rglob("*"))
        for path in candidates:
            if not path.is_file():
                continue
            try:
                mtime = path.stat().st_mtime
            except OSError:
                continue
            if mtime > newest:
                newest = mtime

    return newest


def _build_cli(repo_root: Path) -> str:
    """Build bin/rmp from the module root and return its absolute path.

    This is the single build mechanism resolve_cli() uses, whether no
    candidate binary exists at all or an existing one is stale -- one
    mechanism, reused, rather than a second one added for the stale case.
    Raises RuntimeError, naming `go build`'s own stderr, on failure: the
    caller must let that propagate and fail the run rather than falling back
    to a stale or absent binary.
    """
    result = subprocess.run(
        ["go", "build", "-o", "./bin/rmp", "./cmd/rmp"],
        cwd=repo_root,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(
            "Could not build the CLI binary under test "
            f"(go build -o ./bin/rmp ./cmd/rmp, cwd={repo_root}):\n{result.stderr}"
        )
    return str((repo_root / "bin" / "rmp").resolve())


def _binary_identity(resolved_path: str) -> str:
    """Describe the build the suite is about to certify: `--version` output
    plus the binary's own mtime. Never raises: a binary that cannot even
    report --version still gets a startup line, so the failure is visible
    instead of aborting resolution over a diagnostic nicety.
    """
    try:
        proc = subprocess.run(
            [resolved_path, "--version"], capture_output=True, text=True, timeout=10
        )
        version = (proc.stdout.strip() or proc.stderr.strip()) or "<no --version output>"
    except (OSError, subprocess.TimeoutExpired) as exc:
        version = f"<--version failed: {exc}>"

    try:
        mtime = datetime.fromtimestamp(
            Path(resolved_path).stat().st_mtime, tz=timezone.utc
        ).isoformat()
    except OSError:
        mtime = "<unknown mtime>"

    return f"{version} (built {mtime})"


def resolve_cli(repo_root: Optional[Path] = None, memoize: bool = True) -> str:
    """Resolve the CLI binary this suite is about to drive.

    Candidates are restricted to the repository root -- bin/rmp, then rmp --
    never the current working directory. A binary that happens to sit in
    whatever directory the suite is launched from must never be mistaken for
    the artefact under test.

    When a candidate exists but its mtime predates the newest source file
    compiled into it -- a .go file OR a //go:embed'd template/static asset,
    see _newest_source_mtime() -- it is treated exactly like a missing
    binary: rebuilt via _build_cli() before use. This is the staleness check
    task #271 exists to add; the acceptance regression
    (tests/test_53_e2e_harness_binary_staleness.py) proves each branch fires
    by neutralising it and observing the corresponding test fail.

    Resolution is memoized into the module-level _RESOLVED_CLI by default, so
    it runs (and prints its result) once per interpreter process no matter how
    many callers ask. Pass memoize=False to force a fresh resolution, which
    the regression test uses to probe each scenario without process restarts.
    """
    global _RESOLVED_CLI
    if memoize and _RESOLVED_CLI is not None:
        return _RESOLVED_CLI

    root = (repo_root or REPO_ROOT).resolve()
    newest_source = _newest_source_mtime(root)

    chosen: Optional[Path] = None
    for candidate in (root / "bin" / "rmp", root / "rmp"):
        if candidate.exists():
            chosen = candidate
            break

    if chosen is None:
        resolved = _build_cli(root)
        reason = f"no candidate binary at {root / 'bin' / 'rmp'} or {root / 'rmp'}; built from source"
    else:
        binary_mtime = chosen.stat().st_mtime
        if binary_mtime < newest_source:
            resolved = _build_cli(root)
            reason = (
                f"{chosen} was STALE (binary mtime {binary_mtime:.3f} < "
                f"newest source mtime {newest_source:.3f}); rebuilt"
            )
        else:
            resolved = str(chosen.resolve())
            reason = (
                f"{chosen} is current (binary mtime {binary_mtime:.3f} >= "
                f"newest source mtime {newest_source:.3f})"
            )

    if memoize:
        _RESOLVED_CLI = resolved

    print(
        "[groadmap-tests] resolved CLI binary: "
        f"{resolved}\n[groadmap-tests] resolution: {reason}\n"
        f"[groadmap-tests] identity: {_binary_identity(resolved)}"
    )

    return resolved


# Commit hashes for the two mandatory commit-tracking flags of `task stat`.
# --commit-open is required on every transition into DOING and --commit-close on
# the transition into COMPLETED (SPEC/COMMANDS.md § Change Status (stat)).
# Groadmap validates the FORMAT of a hash only: it runs no git command and never
# resolves the value against a repository, so any well-formed hash serves. These
# two are real short hashes from this project's history.
COMMIT_OPEN_HASH = "8007175"
COMMIT_CLOSE_HASH = "8a82583"


def commit_flags_for(status: str) -> List[str]:
    """Return the commit flag `task stat` makes mandatory for a target status.

    Call sites that build the target status dynamically use this so the flag
    follows the status automatically; a call site with a literal status spells
    the flag out instead.
    """
    if status == "DOING":
        return ["--commit-open", COMMIT_OPEN_HASH]
    if status == "COMPLETED":
        return ["--commit-close", COMMIT_CLOSE_HASH]
    return []


class GroadmapTestBase:
    """Base class for all Groadmap CLI tests."""

    def __init__(self):
        self.cli_path = self._find_cli()
        self.test_dir = None
        self.home_dir = None
        self.roadmaps_dir = None

    def _find_cli(self) -> str:
        """Return the path to the CLI binary under test.

        Delegates to the module-level resolve_cli(), which restricts
        candidates to the repository root, rebuilds a stale binary against
        the newest source compiled into it (.go files and //go:embed'd
        assets alike), and memoizes the result for the process (see the
        module-level docstring above the resolve_cli() definition).
        """
        return resolve_cli()

    def setup(self):
        """Set up test environment."""
        # Create temporary directory for test isolation
        self.test_dir = tempfile.mkdtemp(prefix="groadmap_test_")
        self.home_dir = Path(self.test_dir) / "home"
        self.home_dir.mkdir()
        self.roadmaps_dir = self.home_dir / ".roadmaps"

    def teardown(self):
        """Clean up test environment."""
        if self.test_dir and os.path.exists(self.test_dir):
            shutil.rmtree(self.test_dir)

    def run_cmd(self, args: List[str], check: bool = True) -> Tuple[int, str, str]:
        """
        Run a CLI command and return (exit_code, stdout, stderr).

        Args:
            args: Command arguments (without the binary name)
            check: If True, raise AssertionError on non-zero exit

        Returns:
            Tuple of (exit_code, stdout, stderr)
        """
        env = os.environ.copy()
        env["HOME"] = str(self.home_dir)

        result = subprocess.run(
            [self.cli_path] + args,
            capture_output=True,
            text=True,
            env=env
        )

        if check and result.returncode != 0:
            raise AssertionError(
                f"Command failed: rmp {' '.join(args)}\n"
                f"Exit code: {result.returncode}\n"
                f"Stdout: {result.stdout}\n"
                f"Stderr: {result.stderr}"
            )

        return result.returncode, result.stdout, result.stderr

    def run_cmd_json(self, args: List[str], check: bool = True) -> Any:
        """Run a command and parse JSON output."""
        exit_code, stdout, stderr = self.run_cmd(args, check=check)
        if not stdout.strip():
            return {}
        try:
            result = json.loads(stdout)
            # Convert null (None) to empty list for list operations
            if result is None:
                return []
            return result
        except json.JSONDecodeError as e:
            raise AssertionError(
                f"Failed to parse JSON output: {e}\n"
                f"Output was: {stdout}"
            )

    def generate_roadmap_name(self) -> str:
        """Generate a unique roadmap name for testing."""
        return f"test_roadmap_{uuid.uuid4().hex[:8]}"

    def create_roadmap(self, name: Optional[str] = None) -> str:
        """Create a new roadmap and return its name."""
        if name is None:
            name = self.generate_roadmap_name()
        self.run_cmd(["roadmap", "create", name])
        return name

    def create_task(self, roadmap: str, title: str, functional_requirements: str,
                    technical_requirements: str, acceptance_criteria: str, **kwargs) -> int:
        """
        Create a task and return its ID.

        Args:
            roadmap: Roadmap name
            title: Task title
            functional_requirements: Functional requirements (Why?)
            technical_requirements: Technical requirements (How?)
            acceptance_criteria: Acceptance criteria (How to verify?)
            **kwargs: Optional fields (priority, severity)
        """
        cmd = [
            "task", "create",
            "-r", roadmap,
            "-t", title,
            "-fr", functional_requirements,
            "-tr", technical_requirements,
            "-ac", acceptance_criteria
        ]

        if "priority" in kwargs:
            cmd.extend(["-p", str(kwargs["priority"])])
        if "severity" in kwargs:
            cmd.extend(["--severity", str(kwargs["severity"])])

        result = self.run_cmd_json(cmd)
        return result["id"]

    def create_sprint(self, roadmap: str, description: str, title: str = "") -> int:
        """Create a sprint and return its ID.

        Args:
            roadmap: Roadmap name
            description: Sprint description (also used as title when title is omitted)
            title: Sprint title; defaults to description when not supplied
        """
        sprint_title = title if title else description
        result = self.run_cmd_json([
            "sprint", "create",
            "-r", roadmap,
            "-t", sprint_title,
            "-d", description
        ])
        return result["id"]

    def move_task_to_sprint(self, roadmap: str, task_id: int, sprint_id: Optional[int] = None) -> int:
        """Move a task from BACKLOG to SPRINT via `sprint add-tasks`.

        Manual `task stat <id> SPRINT` is rejected per SPEC/STATE_MACHINE.md;
        SPRINT is an automatic transition triggered only by sprint assignment.
        Creates a new sprint if sprint_id is not provided. Returns the sprint_id used.
        """
        if sprint_id is None:
            sprint_id = self.create_sprint(roadmap, f"Test sprint for task {task_id}")
        self.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)])
        return sprint_id

    def assert_task_status(self, roadmap: str, task_id: int, expected_status: str):
        """Assert that a task has the expected status."""
        result = self.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        if isinstance(result, list) and len(result) > 0:
            actual_status = result[0].get("status")
        else:
            actual_status = result.get("status")
        assert actual_status == expected_status, (
            f"Task {task_id} expected status {expected_status}, got {actual_status}"
        )

    def assert_sprint_status(self, roadmap: str, sprint_id: int, expected_status: str):
        """Assert that a sprint has the expected status."""
        result = self.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        actual_status = result.get("status")
        assert actual_status == expected_status, (
            f"Sprint {sprint_id} expected status {expected_status}, got {actual_status}"
        )

    def assert_exit_code(self, args: List[str], expected_code: int):
        """Assert that a command returns the expected exit code."""
        exit_code, _, _ = self.run_cmd(args, check=False)
        assert exit_code == expected_code, (
            f"Expected exit code {expected_code}, got {exit_code}"
        )

    # Canonical JSON shapes per SPEC/MODELS.md. Used by assert_task_shape /
    # assert_sprint_shape to catch fields that disappear or are added
    # without an explicit decision.
    TASK_KEYS = frozenset([
        "id", "title", "status", "type",
        "functional_requirements", "technical_requirements", "acceptance_criteria",
        "created_at",
        "started_at", "tested_at", "closed_at", "completion_summary",
        "commit_open", "commit_close",
        "parent_task_id", "priority", "severity",
        "subtask_count", "depends_on", "blocks",
    ])
    SPRINT_KEYS = frozenset([
        "id", "title", "status", "description",
        "created_at", "started_at", "closed_at",
        "max_tasks", "tasks", "task_count",
        "order",
    ])

    @classmethod
    def assert_task_shape(cls, task: Dict[str, Any]):
        """Validate that a task JSON object carries exactly the SPEC-defined keys.

        Catches both regressions (a field silently dropped) and accidental
        additions (a field introduced without a SPEC update).
        """
        got = set(task.keys())
        missing = cls.TASK_KEYS - got
        extra = got - cls.TASK_KEYS
        assert not missing and not extra, (
            f"task JSON shape diverges from SPEC:\n  missing: {sorted(missing)}\n  extra:   {sorted(extra)}"
        )

    @classmethod
    def assert_sprint_shape(cls, sprint: Dict[str, Any]):
        """Validate that a sprint JSON object carries exactly the SPEC-defined keys."""
        got = set(sprint.keys())
        missing = cls.SPRINT_KEYS - got
        extra = got - cls.SPRINT_KEYS
        assert not missing and not extra, (
            f"sprint JSON shape diverges from SPEC:\n  missing: {sorted(missing)}\n  extra:   {sorted(extra)}"
        )

    def list_tasks(self, roadmap: str, **filters) -> List[Dict[str, Any]]:
        """List tasks with optional filters."""
        cmd = ["task", "list", "-r", roadmap]
        if "status" in filters:
            cmd.extend(["-s", filters["status"]])
        if "priority" in filters:
            cmd.extend(["-p", str(filters["priority"])])
        if "severity" in filters:
            cmd.extend(["--severity", str(filters["severity"])])
        if "limit" in filters:
            cmd.extend(["-l", str(filters["limit"])])
        return self.run_cmd_json(cmd)

    def list_sprints(self, roadmap: str, status: Optional[str] = None) -> List[Dict[str, Any]]:
        """List sprints with optional status filter."""
        cmd = ["sprint", "list", "-r", roadmap]
        if status:
            cmd.extend(["--status", status])
        return self.run_cmd_json(cmd)
