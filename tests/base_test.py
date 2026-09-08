#!/usr/bin/env python3
"""
Base test class for Groadmap CLI tests.
Provides common utilities and setup/teardown functionality.
"""

import re
import queue
import signal
import subprocess
import json
import os
import socket as socketlib
import tempfile
import threading
import time
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
        """Clean up test environment.

        Any graph server this fixture started is killed FIRST. A server holds
        its roadmap's store open for its whole lifetime, so removing the
        temporary HOME out from under a live one leaves a process writing into
        a directory that no longer exists -- and, worse, leaves it running for
        the rest of the suite holding a lock nothing else can take.
        """
        self.stop_graph_servers()
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

    # ----------------------------------------------------------------------
    # The graph server, and reaching a graph through it
    #
    # `rmp graph execute` is withdrawn. The graph is reachable only through a
    # running `rmp graph serve`, spoken to by `rmp graph client`
    # (SPEC/GRAPH.md "The Dedicated Graph Server"), so every module that
    # touches a graph now needs a server before it can assert anything about
    # one. The three things that takes -- start it, wait for it to bind, stop
    # it -- live here rather than in each module, because fifteen copies of a
    # process lifecycle is fifteen places for a leaked child to hide.
    #
    # The wait is on the STARTUP OBJECT the server prints, never on a sleep.
    # SPEC/GRAPH.md "Server Startup" step 7 writes that line only after the
    # store has been opened and the socket bound, so reading it is observing
    # the whole startup sequence complete; a sleep is a guess about a machine
    # that passes on a fast one and flakes on a loaded one.
    # ----------------------------------------------------------------------

    def _graph_servers(self) -> List["GraphServeProcess"]:
        """The servers this fixture has started, created on first use so a
        subclass that does not call setup() still tears down cleanly."""
        if not hasattr(self, "_servers"):
            self._servers = []
        return self._servers

    def start_graph_server(self, roadmap: str, socket_path: Optional[str] = None,
                           timeout: float = 15.0) -> "GraphServeProcess":
        """Start `rmp graph serve` for `roadmap`, block until it has announced
        its socket, track it for teardown, and return the handle.

        The roadmap must exist; its GRAPH need not. Starting a server against
        a roadmap that has never had a graph is what CREATES one
        (SPEC/GRAPH.md "Server Startup", step 1), which is why nothing has to
        materialise a store first any more.
        """
        server = GraphServeProcess(self, roadmap, socket_path=socket_path)
        self._graph_servers().append(server)
        server.start(timeout=timeout)
        return server

    def stop_graph_servers(self):
        """Force-kill every server this fixture started that is still running.

        It is a KILL and not a signal: this runs in teardown, where the point
        is that nothing outlives the test, and a server wedged badly enough to
        ignore SIGINT is exactly the case a graceful stop would hang on. A
        test whose subject is the graceful shutdown drives it itself.
        """
        for server in self._graph_servers():
            try:
                if server.is_alive():
                    server.kill_dash_9()
            except Exception:
                pass
        self._servers = []

    def default_socket_path(self, roadmap: str) -> str:
        """The socket `rmp graph serve` and `rmp graph client` both derive for
        `roadmap` under this fixture's HOME, when neither is given --socket."""
        return str(self.home_dir / ".roadmaps" / roadmap / "graph.sock")

    def run_cli(self, args: List[str], stdin_text: Optional[str] = None,
                timeout: float = 20.0) -> Tuple[int, str, str]:
        """One `rmp` invocation against this fixture's HOME, returning
        (exit_code, stdout, stderr).

        It is distinct from run_cmd, which every non-graph module uses: this
        NEVER raises on a non-zero exit -- a graph module inspects the code
        itself in almost every case -- it can feed standard input, which the
        graph family reads a statement from, and it is bounded by a timeout so
        a wedged invocation fails the test instead of the run.
        """
        env = os.environ.copy()
        env["HOME"] = str(self.home_dir)
        result = subprocess.run(
            [self.cli_path] + args,
            input=stdin_text,
            capture_output=True,
            text=True,
            env=env,
            timeout=timeout,
        )
        return result.returncode, result.stdout, result.stderr

    def graph_client(self, roadmap: str, query: Optional[str] = None,
                     stdin_text: Optional[str] = None, socket: Optional[str] = None,
                     extra: Optional[List[str]] = None,
                     timeout: float = 20.0) -> Tuple[int, str, str]:
        """Send one Cypher statement to the roadmap's graph server through
        `rmp graph client`, returning (exit_code, stdout, stderr).

        `query` supplies it through --query and `stdin_text` through standard
        input; SPEC/GRAPH.md "Cypher Input Source and Precedence" makes the two
        equally valid, and a caller with a very long statement wants the second
        because Linux caps a single argv entry at 128 KiB. Supplying neither is
        itself a case some modules drive, so it is permitted here.
        """
        args = ["graph", "client", "-r", roadmap]
        if socket is not None:
            args += ["--socket", socket]
        if query is not None:
            args += ["--query", query]
        if extra:
            args += extra
        return self.run_cli(args, stdin_text=stdin_text, timeout=timeout)

    def graph_ok(self, roadmap: str, query: Optional[str] = None,
                 stdin_text: Optional[str] = None, socket: Optional[str] = None,
                 timeout: float = 20.0) -> Any:
        """Run a statement that must succeed and return its parsed stdout.

        It fails loudly with the invocation's own streams rather than returning
        a value a caller would then assert about, because a statement that did
        not run is not a result worth comparing.
        """
        rc, out, err = self.graph_client(roadmap, query=query, stdin_text=stdin_text,
                                         socket=socket, timeout=timeout)
        statement = query if query is not None else stdin_text
        assert rc == 0, (
            f"the statement {statement!r} failed against roadmap {roadmap!r}: "
            f"exit={rc} stdout={out!r} stderr={err!r}"
        )
        try:
            return json.loads(out)
        except json.JSONDecodeError as exc:
            raise AssertionError(
                f"the statement {statement!r} wrote non-JSON to stdout: {out!r} ({exc})"
            ) from exc

    def served_roadmap(self, name: str, *seed_queries: str,
                       socket_path: Optional[str] = None) -> "GraphServeProcess":
        """Create `name`, start a server for it, run every seed statement
        through the client, and return the running server.

        This is the whole fixture most graph cases need. The server is started
        BEFORE the seeds because it is what creates the graph store: nothing
        else does, and a roadmap that has never had a graph is served an empty
        one whose first node the first seed creates.
        """
        self.create_roadmap(name)
        server = self.start_graph_server(name, socket_path=socket_path)
        for statement in seed_queries:
            rc, out, err = self.graph_client(name, query=statement, socket=socket_path)
            assert rc == 0, (
                f"seeding {name!r} with {statement!r} failed: "
                f"exit={rc} stdout={out!r} stderr={err!r}"
            )
        return server


class _StreamDrain:
    """Reads one subprocess pipe (stdout or stderr) on a background thread so
    the writer never blocks on a full OS pipe buffer, and hands the reader a
    thread-safe, timeout-capable view of what has arrived.

    A graph server's own stdout carries exactly one line (the startup JSON)
    and then nothing until it exits; its stderr carries the two engine
    warnings early and nothing else on the happy path. Both must be drained
    continuously regardless, because a client-under-test can print to either
    at any point in the process's life, and an un-drained pipe backs up and
    wedges the child the moment its buffer fills.
    """

    def __init__(self, stream):
        self._lines = []
        self._lock = threading.Lock()
        self._queue: "queue.Queue[str]" = queue.Queue()
        self._thread = threading.Thread(target=self._run, args=(stream,), daemon=True)
        self._thread.start()

    def _run(self, stream):
        try:
            for line in iter(stream.readline, ""):
                with self._lock:
                    self._lines.append(line)
                self._queue.put(line)
        finally:
            # A sentinel so a blocked waiter (wait_for / wait_for_json_object)
            # unblocks the instant the pipe reaches EOF -- typically because
            # the child has exited -- instead of sitting out its whole
            # timeout for a line that will never arrive.
            self._queue.put(None)
            try:
                stream.close()
            except OSError:
                pass

    def snapshot(self):
        with self._lock:
            return list(self._lines)

    def text(self):
        return "".join(self.snapshot())

    def wait_for(self, predicate, timeout):
        """Block until a line already collected -- or a new one -- satisfies
        predicate, or timeout elapses. Returns the matching line, or None.
        """
        deadline = time.monotonic() + timeout
        for line in self.snapshot():
            if predicate(line):
                return line
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return None
            try:
                line = self._queue.get(timeout=remaining)
            except queue.Empty:
                return None
            if line is None:
                return None
            if predicate(line):
                return line

    def wait_for_json_object(self, timeout):
        """Block until the lines collected so far (from the start of the
        stream) parse as one JSON value, or timeout elapses.

        `rmp graph serve`'s startup object is pretty-printed across several
        lines (two-space indentation, per DATA_FORMATS.md "Implementation
        Notes"), so a single-line read never sees a complete object; this
        accumulates lines and re-attempts the parse after each one, which
        works for a pretty-printed object of any number of lines without
        this harness hardcoding how many `rmp` happens to emit today.
        """
        deadline = time.monotonic() + timeout
        buf = "".join(self.snapshot())
        while True:
            candidate = buf.strip()
            if candidate:
                try:
                    return json.loads(candidate)
                except json.JSONDecodeError:
                    pass
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return None
            try:
                line = self._queue.get(timeout=remaining)
            except queue.Empty:
                return None
            if line is None:
                return None
            buf += line


class GraphServeProcess:
    """One `rmp graph serve` child process, spawned and torn down by hand.

    This is the harness every module that needs a live server builds on: it
    owns the subprocess, the two drained pipes, parsing the startup JSON off
    stdout, and sending a signal or a kill with a bounded wait for the exit
    that must follow. Nothing here talks Bolt -- the tests reach the server
    exclusively through `rmp graph client` -- which is what makes this an
    end-to-end suite for the CLI contract rather than a second, private
    protocol client.

    It lives in base_test.py rather than in the module that first needed it
    because fifteen modules need it now, and a process lifecycle copied
    fifteen times is fifteen places for a leaked child holding a store lock to
    hide.
    """

    def __init__(self, harness: "GroadmapTestBase", roadmap: str, socket_path: str = None):
        self.harness = harness
        self.roadmap = roadmap
        self.socket_path_flag = socket_path
        self.proc = None
        self.socket = None
        self._out = None
        self._err = None

    def start(self, timeout: float = 15.0):
        """Launch the server and block until its startup JSON line has been
        read off stdout (SPEC/GRAPH.md "Server Startup" step 7: the line is
        written only after the store has been opened, so seeing it is seeing
        the whole startup sequence complete, not merely the socket bound).

        Raises AssertionError, with the process's own stdout/stderr attached,
        on early exit or on a startup that never announces within `timeout`.
        """
        args = [self.harness.cli_path, "graph", "serve", "-r", self.roadmap]
        if self.socket_path_flag is not None:
            args += ["--socket", self.socket_path_flag]

        env = os.environ.copy()
        env["HOME"] = str(self.harness.home_dir)

        self.proc = subprocess.Popen(
            args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env,
        )
        self._out = _StreamDrain(self.proc.stdout)
        self._err = _StreamDrain(self.proc.stderr)

        obj = self._out.wait_for_json_object(timeout)
        if obj is None:
            self.proc.poll()
            self._finish_teardown_if_dead()
            raise AssertionError(
                f"rmp graph serve -r {self.roadmap} printed no complete startup "
                f"object within {timeout}s (exited={self.proc.returncode}); "
                f"stdout={self._out.text()!r} stderr={self._err.text()!r}"
            )
        self.socket = obj["socket"]
        return obj

    def _finish_teardown_if_dead(self):
        if self.proc.poll() is not None:
            return
        try:
            self.proc.wait(timeout=1.0)
        except subprocess.TimeoutExpired:
            pass

    def stop(self, sig=signal.SIGINT, timeout: float = 15.0) -> int:
        """Signal the server and block for its exit. Returns the exit code.

        Raises AssertionError, rather than leaving a wedged child behind, if
        the process does not exit inside `timeout`.
        """
        assert self.proc is not None, "start() was never called"
        if self.proc.poll() is not None:
            return self.proc.returncode
        self.proc.send_signal(sig)
        try:
            self.proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5.0)
            raise AssertionError(
                f"rmp graph serve -r {self.roadmap} did not exit within {timeout}s "
                f"of signal {sig!r}; it was force-killed. "
                f"stdout={self._out.text()!r} stderr={self._err.text()!r}"
            )
        return self.proc.returncode

    def kill_dash_9(self, timeout: float = 10.0):
        """SIGKILL the server -- uncatchable, no drain, no checkpoint -- and
        wait for the process table entry to clear. Used by every scenario
        that needs a stale socket or a genuinely severed connection rather
        than a graceful stop, and by the fixture's own teardown.
        """
        assert self.proc is not None, "start() was never called"
        if self.proc.poll() is None:
            self.proc.kill()
        self.proc.wait(timeout=timeout)
        return self.proc.returncode

    def pause(self, timeout: float = 5.0) -> str:
        """SIGSTOP the server, wait for the stop to be OBSERVABLE, and return
        the process state last seen.

        The signal is uncatchable and unmaskable, so the process leaves the run
        queue wherever it happens to be -- including inside an engine call --
        and cannot answer anything at all until it is resumed. What it does NOT
        touch is the connection: nothing is closed, no FIN or RST reaches the
        peer, and the kernel goes on holding both ends of the socket. That is
        the difference between "the server is alive and not answering" and "the
        connection was lost", and it is the difference the two published lines
        for those two states are told apart by.

        DELIVERY IS NOT ARRIVAL, and reading the state once is not enough. The
        kernel marks the signal pending and the thread group leaves the run
        queue when its threads are next scheduled, so /proc still reports 'R'
        for a short while after os.kill returns: measured against a server busy
        executing a write, the stop became observable between 0.108 ms and
        2.183 ms later (median 0.146 ms, 30 samples), and reading the state
        once instead of polling for it reported 'R' on two of the first three
        runs of that case. Polling until 'T' is what turns "the signal was
        sent" into "the process is stopped", which is the property a caller
        asserting on a server that cannot answer actually needs. The wait is
        bounded so that a server which never stops fails the caller's assertion
        with the state it was really in, rather than hanging here.
        """
        assert self.proc is not None, "start() was never called"
        assert self.is_alive(), (
            f"rmp graph serve -r {self.roadmap} had already exited "
            f"({self.proc.returncode}) before it could be frozen; "
            f"stderr={self.stderr_text()!r}"
        )
        os.kill(self.proc.pid, signal.SIGSTOP)
        deadline = time.monotonic() + timeout
        while True:
            state = self.state()
            if state == "T" or time.monotonic() >= deadline:
                return state
            time.sleep(0.001)

    def resume(self) -> str:
        """SIGCONT a frozen server, putting it back on the run queue with its
        statement, its session and its socket exactly as it left them.
        """
        assert self.proc is not None, "start() was never called"
        os.kill(self.proc.pid, signal.SIGCONT)
        return self.state()

    def state(self) -> str:
        """The process state character Linux publishes as field 3 of
        /proc/<pid>/stat: 'R' running, 'S' sleeping, 'D' in uninterruptible
        sleep, 'T' stopped by a signal, 'Z' exited and not yet reaped.

        Read from the LAST ')' rather than by splitting the whole line, because
        field 2 is the executable name in parentheses and a name may itself
        contain a space or a ')'. Returns 'Z' for a process that has exited and
        whose entry Popen has not yet collected, and raises FileNotFoundError
        for one that has been reaped -- both of which are states a caller here
        wants to see fail loudly rather than be smoothed over.
        """
        assert self.proc is not None, "start() was never called"
        with open(f"/proc/{self.proc.pid}/stat", "rb") as fh:
            raw = fh.read()
        return raw[raw.rindex(b")") + 2:].split(b" ", 1)[0].decode()

    def is_alive(self) -> bool:
        return self.proc is not None and self.proc.poll() is None

    def drain_stderr_to_eof(self, timeout: float = 5.0):
        """Block until the server's stderr pipe reaches EOF, so a caller that
        has already seen the process exit can read the WHOLE stream rather
        than whatever the draining thread happened to have collected.

        The drain runs on its own thread, so `proc.wait()` returning does not
        mean the last line has been appended. Every assertion that compares the
        COMPLETE stderr -- rather than searching it for a fragment -- has to
        close that window first, or it races the thread and fails
        intermittently on a line that did arrive.

        `_StreamDrain._run` puts a `None` sentinel on the queue at EOF, so
        waiting for a predicate nothing satisfies returns exactly when the pipe
        closes (or when `timeout` elapses, which a caller that has already
        observed the exit can treat as a drained stream).
        """
        if self._err is not None:
            self._err.wait_for(lambda _line: False, timeout)

    def stderr_text(self) -> str:
        return self._err.text() if self._err else ""

    def stdout_text(self) -> str:
        return self._out.text() if self._out else ""

    def wait_for_exit(self, timeout: float):
        """Block for a NATURAL exit -- no signal sent -- returning the exit
        code, or None if it is still running when `timeout` elapses.
        """
        try:
            return self.proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            return None


# --------------------------------------------------------------------------
# The graph write-result shape
#
# `rmp graph client` answers a statement that produces no
# result columns with {"ok": true}, and a statement that CHANGED the graph adds
# one further member, `counters`, naming what it changed
# (SPEC/DATA_FORMATS.md § Graph Write Result and § Graph Query Counters;
# SPEC/GRAPH.md § Write Counters: What a Statement Changed).
#
# The member is additive, so `ok` keeps its meaning, its value and its position.
# What it broke is the IDIOM these suites used to assert that shape:
# `result == {"ok": True}` compares the whole object and therefore fails on a
# correct result the moment the statement changed something. Relaxing each site
# to `result["ok"] is True` would fix the failure and lose what the comparison
# was worth -- it would stop noticing a stray third member, which is exactly the
# drift a whole-object comparison exists to catch.
#
# This is the one place the rule is written, so the suites cannot come to
# disagree about what the shape is: `ok` is true, and the only other member the
# object may carry is `counters`, whose value is asserted when the caller knows
# what the statement changed.
# --------------------------------------------------------------------------


def assert_graph_write_shape(result: Any, context: str = "",
                             counters: Optional[Dict[str, int]] = None):
    """Assert the {"ok": true} write shape, with its optional counters member.

    counters, when given, is the COMPLETE expected block: every counter the
    statement produced, and no other, because a zero is omitted rather than
    published. Pass {} to require that the statement changed nothing and
    therefore carries no `counters` key at all.
    """
    where = f"{context}: " if context else ""
    assert isinstance(result, dict), f"{where}the write shape is a JSON object; got {result!r}"
    assert result.get("ok") is True, (
        f"{where}a statement that produces no result columns returns "
        f'{{"ok": true}}; got {result!r}')
    extra = set(result) - {"ok", "counters"}
    assert not extra, (
        f"{where}the only member additive to the write shape is 'counters'; "
        f"got the unexpected {sorted(extra)!r} in {result!r}")
    if counters is None:
        return
    if counters == {}:
        assert "counters" not in result, (
            f"{where}a statement that changed nothing carries no 'counters' key at "
            f"all, and produces exactly the bytes it produced before the member "
            f"existed; got {result!r}")
        return
    assert result.get("counters") == counters, (
        f"{where}expected the counters {counters!r} -- a zero is omitted, not "
        f"published; got {result.get('counters')!r}")


# --------------------------------------------------------------------------
# The platform's socket-path bound
#
# A Unix domain socket path is bounded by the size of the kernel's sun_path
# field, and the bound is NOT the same everywhere: across the nine targets
# SPEC/BUILD.md declares it is 107 bytes on Linux and Windows and 103 on
# macOS, FreeBSD and OpenBSD. A test that writes either figure down is wrong
# on the other platforms, and wrong in the PERMISSIVE direction on three of
# the five -- it would let a harness build a path the kernel refuses and then
# fail with the bare "bind: invalid argument" that SPEC/GRAPH.md
# "Socket Path Length" exists to replace.
#
# So the bound is MEASURED, once, here, by binding real sockets until one is
# refused, and every module that needs it reads this one function. It lives in
# base_test rather than in the module that first needed it because two modules
# need it, and importing one test module from another merely to share a number
# makes the two a cycle.
# --------------------------------------------------------------------------


def _bind_at(directory: str, length: int) -> bool:
    """Bind a real AF_UNIX listener whose absolute path is exactly `length`
    bytes, and report whether the platform accepted it.

    The socket is created and immediately removed, so the measurement leaves
    nothing behind. A successful return is a real kernel bind -- the half that
    could not be faked by any check running before the syscall.
    """
    padding = length - len(directory) - 1
    assert padding >= 1, (
        f"the measurement directory {directory!r} is {len(directory)} bytes, "
        f"too long to build a path of {length} bytes inside it. This module "
        f"needs a short $TMPDIR."
    )
    path = os.path.join(directory, "b" * padding)
    assert len(path) == length, (len(path), length)

    sock = socketlib.socket(socketlib.AF_UNIX, socketlib.SOCK_STREAM)
    try:
        sock.bind(path)
    except OSError:
        return False
    finally:
        sock.close()
    os.unlink(path)
    return True


def measure_socket_path_bound(directory: str, floor: int = 64, ceiling: int = 400) -> int:
    """Return the greatest socket-path length this platform will bind, found by
    binding real sockets at increasing lengths until one is refused.

    Acceptance Criterion 67 requires exactly this and forbids the alternative:
    a criterion asserting the literal 107 would confirm the defect on the three
    operating systems whose bound is 103, and would confirm it silently.

    The figure returned is the largest length that SUCCEEDED, which is a pure
    kernel observation. The refusals above it are corroboration, and four of
    them are required so a single anomalous length cannot be mistaken for the
    boundary.
    """
    largest_bound = None
    for length in range(floor, ceiling + 1):
        if _bind_at(directory, length):
            largest_bound = length
            continue

        assert largest_bound is not None, (
            f"no socket path bound at any length from {floor} bytes upward; the "
            f"measurement directory {directory!r} is unusable"
        )
        assert largest_bound == length - 1, (
            f"the boundary is not contiguous: the largest length that bound was "
            f"{largest_bound} but {length} is the first refused"
        )
        for over in range(length, min(length + 4, ceiling) + 1):
            assert not _bind_at(directory, over), (
                f"a path of {over} bytes bound after {length} was refused, so the "
                f"refusal at {length} was not the platform's bound"
            )
        return largest_bound

    raise AssertionError(
        f"no socket path was refused at any length up to {ceiling} bytes; this "
        f"platform appears to have no sun_path bound, which contradicts every "
        f"target SPEC/BUILD.md declares"
    )
