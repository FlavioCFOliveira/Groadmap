#!/usr/bin/env python3
"""
Test 70: the AI Agent Contract's published claims are executed, not inspected.

Two gates live here, on two SPEC sections, over one fixture.

1. SPEC/DATA_FORMATS.md § Published Examples Are Executed.
   Every invocation the contract publishes -- 148 subcommand examples, 34
   workflow steps and 40 pitfall examples -- is run against the compiled
   binary and asserted to produce what the contract published for it: the
   exit code, the stderr line when it fails, and stdout of the declared shape
   when it succeeds. An invocation the gate cannot run is exempted BY NAME
   with its reason, and the exemption list is itself checked for staleness.

2. SPEC/DATA_FORMATS.md § Field reference: per-subcommand exit code entry,
   rule 5: a subcommand's `exit_codes` array is exhaustive over the
   conditions a caller controls, and does not enumerate faults in the
   environment. Every one of the 56 subcommands is driven into every code it
   declares, and swept with a closed family of command-line probes; a code the
   binary emits that the subcommand does not declare fails the gate, and so
   does a declared code nothing can drive.

# Why the corpus is read from the binary

Both gates obtain the contract by running `rmp --ai-help` against the compiled
binary. Neither parses the Go source that builds it: a gate that reads the
source agrees with a generator that is itself wrong, which is the failure they
exist to detect.

# What bounds the exhaustiveness sweep, and what does not

Rule 5 draws the boundary, and this gate takes it from the SPEC rather than
drawing one of its own: the array is exhaustive over the conditions a caller
controls -- the command line and the state of the roadmap -- and does not
enumerate faults in the environment. The rule is the source; what follows is
only how this gate applies it.

The sweep of gate 2 is over the COMMAND LINE: an unrecognised flag, a
malformed positional id, a missing or surplus positional argument, a flag
written with no value, a value outside its range, a value outside its enum, an
absent roadmap selector, and a roadmap that does not exist.

It is NOT a sweep over the environment, because rule 5 does not ask for one: a
code a subcommand emits only on an environment fault, and does not declare, is
not a defect under the rule. Where a subcommand DOES declare a code for such a
fault, rule 5 requires its condition to name the fault and this gate to
reproduce that fault rather than a generic one, and the drivers below do: a
corrupted database for the subcommands that add, edit and remove a comment, a
bound port for `web`, a socket nothing listens on for `graph client`, and a
live server for a second `graph serve`.

# Isolation

Everything runs under a throwaway HOME created below /tmp and removed at the
end. Each invocation gets its own copy of a pre-built fixture, so a published
invocation that mutates the roadmap is run and its mutation kept, without the
next invocation inheriting it. `rmp` on PATH is a symlink to the binary under
test, so an example beginning with the bare word `rmp` cannot silently reach an
installed copy elsewhere on the machine -- which it did, and which made the
first draft of this gate pass an example the binary under test refuses.

The temporary root is short so the graph socket path derived under it stays
inside the platform's limit (SPEC/GRAPH.md § Socket Path Length); past that
limit every graph invocation would be refused for the length of the gate's own
directory instead of for the reason its example publishes.
"""

import json
import os
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import resolve_cli


# ---------------------------------------------------------------------------
# The roadmap stand-in
# ---------------------------------------------------------------------------

# The one token the gate substitutes rather than taking literally
# (SPEC/DATA_FORMATS.md § State, isolation and side effects). Every other token
# in a subcommand or pitfall example is passed to the binary unchanged.
STAND_IN = "myproject"

# The throwaway roadmap the stand-in resolves to. Deliberately NOT "myproject":
# substituting a different name is what proves no published example depends on
# the literal one.
FIXTURE_ROADMAP = "gatefix"

# Two commit hashes the corpus uses as literals.
OPEN_HASH = "5f93b51"
CLOSE_HASH = "2578d18"


# ---------------------------------------------------------------------------
# Exemptions: examples the gate cannot run, each named with its reason
# ---------------------------------------------------------------------------
#
# SPEC/DATA_FORMATS.md § Examples that cannot be run as published: no silent
# skip, one example per entry, a published reason, and a staleness check that
# fails when an entry names an example the contract no longer publishes.

EXEMPT_EXAMPLES = {
    "rmp graph serve -r myproject --socket /run/user/1000/myproject-graph.sock":
        "names an absolute socket path outside the temporary home this gate "
        "isolates. Running it would create a socket the gate does not own, "
        "under a directory whose existence depends on the running user's id "
        "being 1000; on a machine where it is not, the invocation would fail "
        "for the absence of /run/user/1000 rather than for anything rmp did.",
    "rmp graph client -r myproject --socket /run/user/1000/myproject-graph.sock --query \"SHOW INDEXES\"":
        "the client half of the same pair, and unrunnable for the same reason: "
        "it can only succeed against a server bound to that same path outside "
        "the gate's isolation.",
}


# ---------------------------------------------------------------------------
# Server invocations: started, asserted to have started, and stopped
# ---------------------------------------------------------------------------
#
# These are executed, not exempted. SPEC/DATA_FORMATS.md § State, isolation and
# side effects: "An invocation that starts a long-running process ... is
# started by the gate ..., asserted to have started, and stopped before the run
# continues." The oracle is the startup line the process writes to stdout, and
# the published exit code for each of them is 0.

SERVER_INVOCATIONS = {
    "rmp graph serve -r myproject": "socket",
    "rmp web": "url",
    "rmp web --no-open": "url",
}

SERVER_START_TIMEOUT = 20.0


# ---------------------------------------------------------------------------
# The placeholder vocabulary of workflow steps
# ---------------------------------------------------------------------------
#
# SPEC/DATA_FORMATS.md § Published Examples Are Executed: "A step carrying a
# token that is not in the vocabulary fails the gate, which is what keeps the
# vocabulary closed and the steps runnable."

WORKFLOW_PLACEHOLDERS = {
    "<name>",
    "<title>",
    "<functional-requirements>",
    "<technical-requirements>",
    "<acceptance-criteria>",
    "<TYPE>",
    "<0-9>",
    "<macro-goal-description>",
    "<n>",
    "<sprint-id>",
    "<task-id>",
    "<task-id-1,task-id-2,...>",
    "<open-sprint-id>",
    "<next-pending-sprint-id>",
    "<from-sprint-id>",
    "<to-sprint-id>",
    "<new-priority>",
    "<comment-id>",
    "<hash>",
    "<one-paragraph completion summary>",
    "<the proposition about to be tested>",
    "<the test or verification that was run, and what it showed>",
    "<the behaviour observed, the measurement taken, or the cause identified>",
    "<the decision taken and the reasoning behind it>",
    "<key>",
    "<from>",
    "<to>",
}


# ---------------------------------------------------------------------------
# Placeholders the gate resolves inside a published stderr line
# ---------------------------------------------------------------------------
#
# SPEC/COMMANDS.md § Published Error Strings Are Exact declares the closed set;
# this is the subset a gate running against its own fixture can resolve. The
# whole line is compared: the text before the placeholder and the text after it
# character for character, and the placeholder's own span against the value the
# gate resolved for it.

RESOLVABLE_STDERR_PLACEHOLDERS = ("<socket>",)


# ---------------------------------------------------------------------------
# The workspace
# ---------------------------------------------------------------------------


class Workspace:
    """A throwaway HOME with a pre-built fixture, copied per invocation."""

    _root = None
    _template = None
    _bindir = None
    _contract = None

    @classmethod
    def root(cls):
        if cls._root is None:
            # Short on purpose: the graph socket path is derived under it.
            cls._root = tempfile.mkdtemp(prefix="rmpg", dir="/tmp")
        return cls._root

    @classmethod
    def bindir(cls):
        """A PATH entry holding `rmp` (the binary under test) and a no-op
        `xdg-open`.

        The first is what makes an example beginning with the bare word `rmp`
        a claim about THIS binary; without it the shell resolves `rmp` from the
        developer's own PATH, and the gate measures whatever is installed there.

        The second keeps `rmp web`, which is published without --no-open, from
        launching the machine's browser: the process it starts is outside the
        gate's isolation and outside its power to stop.
        """
        if cls._bindir is None:
            path = os.path.join(cls.root(), "bin")
            os.makedirs(path, exist_ok=True)
            os.symlink(resolve_cli(), os.path.join(path, "rmp"))
            for name in ("xdg-open", "open", "www-browser"):
                stub = os.path.join(path, name)
                with open(stub, "w", encoding="utf-8") as fh:
                    fh.write("#!/bin/sh\nexit 0\n")
                os.chmod(stub, 0o755)
            cls._bindir = path
        return cls._bindir

    @classmethod
    def template(cls):
        if cls._template is None:
            path = os.path.join(cls.root(), "t")
            cls._build(path)
            cls._template = path
        return cls._template

    # -- fixture -----------------------------------------------------------

    @classmethod
    def _rmp(cls, args, home, check=True):
        env = os.environ.copy()
        env["HOME"] = home
        env["PATH"] = cls.bindir() + os.pathsep + env.get("PATH", "")
        proc = subprocess.run(
            [resolve_cli()] + args, capture_output=True, text=True,
            env=env, input="", timeout=60,
        )
        if check and proc.returncode != 0:
            raise AssertionError(
                "the gate could not build its fixture: rmp "
                + " ".join(args)
                + f"\n  exit={proc.returncode} stderr={proc.stderr.strip()!r}"
            )
        return proc.returncode, proc.stdout, proc.stderr

    @classmethod
    def _build(cls, home):
        """Build the fixture the published examples act on.

        SPEC/DATA_FORMATS.md § State, isolation and side effects: the fixture is
        built to fit the examples, not the examples trimmed to fit the fixture.
        Every id and every status below is there because a published invocation
        names it.
        """
        os.makedirs(home)
        r = FIXTURE_ROADMAP

        cls._rmp(["roadmap", "create", r], home)
        # `rmp roadmap create existing` publishes exit 5, so `existing` exists.
        cls._rmp(["roadmap", "create", "existing"], home)
        # `rmp roadmap remove mobile-app` publishes exit 0, so it exists too.
        cls._rmp(["roadmap", "create", "mobile-app"], home)

        # Ids up to 46: the pitfall catalogue reorders sprint 7 over
        # 42,43,44,45,46, and the examples name 42 and 43 directly.
        for i in range(1, 47):
            cls._rmp([
                "task", "create", "-r", r,
                "-t", f"Fixture task {i}",
                "-fr", "An agent can drive the published example that names this task.",
                "-tr", "Created by the published-examples gate before the corpus runs.",
                "-ac", "The example naming this id reproduces its published outcome.",
                "--type", "TASK", "--priority", "5",
            ], home)

        # Eight sprints: the examples name 5 and the pitfalls name 3, 7 and 8.
        for i in range(1, 9):
            cls._rmp([
                "sprint", "create", "-r", r,
                "-t", f"Fixture sprint {i}",
                "-d", "Deliver the fixture state the published examples are run against.",
            ], home)

        # Task 2 depends on task 1: the dependency-guard pitfall needs both.
        cls._rmp(["task", "add-dep", "-r", r, "2", "1"], home)

        # Sprint 5 holds exactly 1,2,3,7 -- `sprint reorder 5 3,1,7,2` names
        # the complete set, and swap/move-to/top/bottom name 3 and 7.
        cls._rmp(["sprint", "add-tasks", "-r", r, "5", "1,2,3,7"], home)
        # Sprint 7 holds exactly 42..46 -- the partial_reorder pitfall's
        # correct example names all five.
        cls._rmp(["sprint", "add-tasks", "-r", r, "7", "42,43,44,45,46"], home)

        # Tasks 1 and 2 in TESTING: COMPLETED is legal from there and nowhere
        # else, and the dependency-guard pitfall is reached only from there.
        for task in ("1", "2"):
            cls._rmp(["task", "stat", "-r", r, task, "DOING", "--commit-open", OPEN_HASH], home)
            cls._rmp(["task", "stat", "-r", r, task, "TESTING"], home)

        # Task 7 back to BACKLOG so `task remove 7` succeeds as published. The
        # BACKLOG transition keeps the sprint_tasks row, so 7 stays a member of
        # sprint 5 for the ordering examples (SPEC/STATE_MACHINE.md § Sprint
        # Membership and the BACKLOG Status).
        cls._rmp(["task", "stat", "-r", r, "7", "BACKLOG"], home)

        # Sprint 3 CLOSED: `sprint add-tasks 3 42,43` publishes the CLOSED
        # refusal. Closing it leaves no sprint OPEN, which is what the
        # `task next` failure example and the `sprint start` examples need.
        cls._rmp(["sprint", "start", "-r", r, "3"], home)
        cls._rmp(["sprint", "close", "-r", r, "3"], home)

        # Task comment 12 and sprint comment 4 are named by their ids.
        for i in range(1, 13):
            cls._rmp(["task", "comment-add", "-r", r, "42", "--type", "NOTE",
                      "--body", f"Fixture task comment {i} on task 42."], home)
        for i in range(1, 5):
            cls._rmp(["sprint", "comment-add", "-r", r, "3", "--type", "FINDING",
                      "--body", f"Fixture sprint comment {i} on sprint 3."], home)

        # Files three published examples read from the working directory.
        for name, text in (
            ("decision.txt", "Chose the bounded retry ladder over an unbounded one.\n"),
            ("revised.txt", "The revised body of the comment, supplied on standard input.\n"),
            ("sprint-retro.txt", "The sprint delivered every task it planned.\n"),
        ):
            with open(os.path.join(home, name), "w", encoding="utf-8") as fh:
                fh.write(text)

    # -- per-invocation copies --------------------------------------------

    _counter = 0

    @classmethod
    def fresh(cls):
        cls._counter += 1
        work = os.path.join(cls.root(), f"w{cls._counter}")
        shutil.copytree(cls.template(), work)
        return work

    @classmethod
    def contract(cls):
        if cls._contract is None:
            _, out, _ = cls._rmp(["--ai-help"], cls.template())
            cls._contract = json.loads(out)
        return cls._contract

    @classmethod
    def cleanup(cls):
        if cls._root and os.path.isdir(cls._root):
            shutil.rmtree(cls._root, ignore_errors=True)
        cls._root = cls._template = cls._bindir = cls._contract = None


# ---------------------------------------------------------------------------
# Running one invocation
# ---------------------------------------------------------------------------


def _env_for(home):
    env = os.environ.copy()
    env["HOME"] = home
    env["PATH"] = Workspace.bindir() + os.pathsep + env.get("PATH", "")
    # Isolation is asserted, not assumed: a gate that reached a real roadmap
    # would be destroying the developer's data (SPEC/DATA_FORMATS.md § State,
    # isolation and side effects).
    assert home.startswith(Workspace.root() + os.sep), (
        f"the gate refused to run: HOME {home!r} is outside its own temporary "
        f"root {Workspace.root()!r}"
    )
    return env


def run_line(line, home, timeout=40.0):
    """Run one published invocation as a shell command line.

    The corpus carries pipelines, redirections and `&&` chains, so the line is
    handed to a shell rather than split. `&&` already gives the shell the
    semantics the SPEC asks of a published sequence: each element must exit 0
    for the next to run, and the line's own status is non-zero if any did not.

    The process gets its own group, and the group is killed afterwards, so an
    invocation that backgrounds a server leaves nothing running.
    """
    env = _env_for(home)
    # The two streams go to FILES, not pipes. A published invocation may
    # background a server (`rmp graph serve -r <name> & ...`), and a
    # backgrounded child inherits the pipe's write end: reading the pipe to
    # end-of-file would then wait for the SERVER to exit rather than for the
    # command line to finish, and the gate would report a timeout on a line
    # that completed in a second.
    out_path = os.path.join(home, ".gate-stdout")
    err_path = os.path.join(home, ".gate-stderr")
    with open(out_path, "w", encoding="utf-8") as out_fh, \
            open(err_path, "w", encoding="utf-8") as err_fh:
        proc = subprocess.Popen(
            ["bash", "-c", line], stdout=out_fh, stderr=err_fh,
            stdin=subprocess.DEVNULL, text=True, env=env, cwd=home,
            start_new_session=True,
        )
        try:
            code = proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            code = "TIMEOUT"
        finally:
            _kill_group(proc)
    with open(out_path, encoding="utf-8", errors="replace") as fh:
        out = fh.read()
    with open(err_path, encoding="utf-8", errors="replace") as fh:
        err = fh.read()
    return code, out, err


def _kill_group(proc):
    try:
        os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
    except (ProcessLookupError, PermissionError):
        pass


def start_server(line, home, key, timeout=SERVER_START_TIMEOUT):
    """Start a long-running published invocation, assert it started, stop it.

    The oracle is the JSON object the process writes to stdout at startup --
    {"socket": ...} for the graph server, {"url": ...} for the web server --
    which is what the subcommand's own stdout_on_success declares.
    """
    env = _env_for(home)
    proc = subprocess.Popen(
        ["bash", "-c", line], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        stdin=subprocess.DEVNULL, text=True, env=env, cwd=home,
        start_new_session=True,
    )
    deadline = time.time() + timeout
    buffered = ""
    started = None
    while time.time() < deadline:
        if proc.poll() is not None:
            break
        line_read = proc.stdout.readline()
        if not line_read:
            time.sleep(0.02)
            continue
        buffered += line_read
        try:
            obj = json.loads(buffered)
        except json.JSONDecodeError:
            continue
        if isinstance(obj, dict) and key in obj:
            started = obj
            break
    return started, buffered, proc


def stop_server(proc):
    _kill_group(proc)
    try:
        proc.communicate(timeout=10)
    except subprocess.TimeoutExpired:
        pass


# ---------------------------------------------------------------------------
# Comparing what came back
# ---------------------------------------------------------------------------


def first_line(text):
    stripped = text.strip()
    return stripped.splitlines()[0] if stripped else ""


def stderr_matches(published, observed, resolved):
    """Compare an observed first line of stderr against a published one.

    `resolved` maps a declared placeholder to the value the gate resolved for
    it from its own fixture. Where the published line carries one, the whole
    line is still compared: the text before the placeholder and the text after
    it character for character, and the placeholder's own span against the
    resolved value. Nothing is skipped and nothing is matched loosely.
    """
    for token in RESOLVABLE_STDERR_PLACEHOLDERS:
        if token not in published:
            continue
        value = resolved.get(token)
        if value is None:
            return False
        head, _, tail = published.partition(token)
        return observed == head + value + tail
    return observed == published


def stdout_schema_keys(schema):
    """The keys a `stdout_on_success.schema` names, when it names them in a
    form a machine can read: one brace group whose depth-1 entries are all
    plain identifiers, optionally quoted, optionally followed by a value.

    A schema that is prose about the shape rather than a listing of it -- the
    `graph client` one, whose entries carry "with EXPLAIN" and "when the
    statement changed the graph" -- yields no keys, and only the JSON kind is
    then asserted. Guessing keys out of prose would produce a gate that fails
    on documents that are correct.
    """
    if not schema:
        return set()
    start = schema.find("{")
    if start < 0:
        return set()
    depth = 0
    end = -1
    for i in range(start, len(schema)):
        if schema[i] == "{":
            depth += 1
        elif schema[i] == "}":
            depth -= 1
            if depth == 0:
                end = i
                break
    if end < 0:
        return set()

    body = schema[start + 1:end]
    entries, buf, depth = [], "", 0
    for ch in body:
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
        if ch == "," and depth == 0:
            entries.append(buf)
            buf = ""
            continue
        buf += ch
    entries.append(buf)

    keys = set()
    for entry in entries:
        name = entry.split(":", 1)[0].strip().strip('"')
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", name):
            return set()
        keys.add(name)
    return keys


def assert_stdout_shape(label, cmd, kind, schema, observed):
    """SPEC/DATA_FORMATS.md § What the gate asserts, point 3."""
    keys = stdout_schema_keys(schema)
    if kind == "empty":
        assert observed.strip() == "", (
            f"{label}: `{cmd}` declares an empty stdout on success and wrote "
            f"{observed[:200]!r}"
        )
        return
    try:
        doc = json.loads(observed)
    except json.JSONDecodeError as exc:
        raise AssertionError(
            f"{label}: `{cmd}` declares stdout_on_success.kind={kind!r} and wrote "
            f"something that is not JSON ({exc}): {observed[:200]!r}"
        ) from exc
    if kind == "object":
        assert isinstance(doc, dict), (
            f"{label}: `{cmd}` declares an object on success and wrote a "
            f"{type(doc).__name__}"
        )
        missing = keys - set(doc)
        assert not missing, (
            f"{label}: `{cmd}` wrote an object without the key(s) its schema "
            f"names: {sorted(missing)}; schema={schema!r}"
        )
    elif kind == "array":
        assert isinstance(doc, list), (
            f"{label}: `{cmd}` declares an array on success and wrote a "
            f"{type(doc).__name__}"
        )
        for element in doc:
            if not isinstance(element, dict):
                continue
            missing = keys - set(element)
            assert not missing, (
                f"{label}: `{cmd}` wrote an array element without the key(s) "
                f"its schema names: {sorted(missing)}; schema={schema!r}"
            )
    else:
        raise AssertionError(f"{label}: unknown stdout_on_success.kind {kind!r}")


# ---------------------------------------------------------------------------
# Preparations: the state a particular example requires
# ---------------------------------------------------------------------------
#
# SPEC/DATA_FORMATS.md § State, isolation and side effects: "the gate creates
# entities until those ids exist, and puts the entities an example acts on into
# the status that example requires". The base fixture satisfies most of the
# corpus; the entries below name the examples whose own precondition differs
# from it, and there is no way to satisfy all of them at once -- `task stat 42
# DOING` needs task 42 in SPRINT and `task stat 42 COMPLETED` needs it in
# TESTING, and both are published.

# Drive a task from the fixture's SPRINT status into TESTING.
def _to_testing(*task_ids):
    steps = []
    for task in task_ids:
        steps.append(["task", "stat", "-r", FIXTURE_ROADMAP, task, "DOING",
                      "--commit-open", OPEN_HASH])
        steps.append(["task", "stat", "-r", FIXTURE_ROADMAP, task, "TESTING"])
    return steps


# Keyed by (subcommand label, example title): a title is what distinguishes the
# two `rmp task next -r myproject` examples, which publish different exit codes
# for the same command line under different preconditions.
EXAMPLE_PREPARATIONS = {
    ("roadmap create", "Create a roadmap"): {
        "rmp": [["roadmap", "remove", "mobile-app"]],
    },
    ("task next", "Next task"): {
        "rmp": [["sprint", "start", "-r", FIXTURE_ROADMAP, "5"]],
    },
    ("task next", "Next 5"): {
        "rmp": [["sprint", "start", "-r", FIXTURE_ROADMAP, "5"]],
    },
    ("task stat", "Complete with summary"): {
        "rmp": [["sprint", "add-tasks", "-r", FIXTURE_ROADMAP, "5", "7"]] + _to_testing("7"),
    },
    ("task remove-dep", "Remove dep"): {
        "rmp": [["task", "add-dep", "-r", FIXTURE_ROADMAP, "10", "7"]],
    },
    ("sprint close", "Close clean"): {
        "rmp": [
            ["sprint", "start", "-r", FIXTURE_ROADMAP, "5"],
            ["sprint", "remove-tasks", "-r", FIXTURE_ROADMAP, "5", "1,2,3,7"],
        ],
    },
    ("sprint close", "Force close"): {
        "rmp": [["sprint", "start", "-r", FIXTURE_ROADMAP, "5"]],
    },
    ("sprint reopen", "Reopen"): {
        "rmp": [
            ["sprint", "start", "-r", FIXTURE_ROADMAP, "5"],
            ["sprint", "close", "-r", FIXTURE_ROADMAP, "5", "--force"],
        ],
    },
}

# Examples that need a graph server answering on the roadmap's default socket.
EXAMPLES_NEEDING_A_SERVER = {
    'rmp graph client -r myproject --query "MATCH (n:Spec) RETURN n.key"',
    'rmp graph client -r myproject --query "CREATE (n:Spec {key:\'auth\'})"',
    'echo "MATCH (n) RETURN count(n)" | rmp graph client -r myproject',
}

# Pitfall preparations, keyed by (pitfall id, "wrong"|"correct").
PITFALL_PREPARATIONS = {
    ("summary_on_non_completed_transition", "correct"): {"rmp": _to_testing("42")},
    ("assume_partial_batch_success", "correct"): {"rmp": _to_testing("42", "43")},
}

PITFALLS_NEEDING_A_SERVER = {
    ("graph_statement_is_not_checked", "wrong"),
    ("graph_statement_is_not_checked", "correct"),
    ("graph_schema_two_statements_in_one_query", "wrong"),
    ("graph_schema_two_statements_in_one_query", "correct"),
    ("graph_schema_failure_exit_code", "wrong"),
    ("graph_schema_failure_exit_code", "correct"),
    ("graph_missing_query", "correct"),
}


def apply_preparation(prep, home):
    for argv in prep.get("rmp", []):
        code, _, err = Workspace._rmp(argv, home, check=False)
        assert code == 0, (
            "a preparation step failed, so the example it prepares was never "
            f"put in the state it requires: rmp {' '.join(argv)} -> exit={code} "
            f"stderr={err.strip()!r}"
        )


# ---------------------------------------------------------------------------
# Gate 1: published examples are executed
# ---------------------------------------------------------------------------


class TestPublishedExamplesAreExecuted:
    """SPEC/DATA_FORMATS.md § Published Examples Are Executed."""

    def setup_method(self):
        self.contract = Workspace.contract()
        self.executed = []
        self.exempted = []

    def teardown_method(self):
        pass

    # -- helpers -----------------------------------------------------------

    def _substitute(self, cmd):
        return cmd.replace(STAND_IN, FIXTURE_ROADMAP)

    def _run_published(self, label, cmd, home, needs_server, server_key=None):
        """Run one published invocation, starting a graph server first where
        the invocation needs one to reach the behaviour it publishes."""
        line = self._substitute(cmd)
        server = None
        if needs_server:
            server = self._start_graph_server(home)
        try:
            if server_key:
                started, buffered, proc = start_server(line, home, server_key)
                assert started is not None, (
                    f"{label}: `{cmd}` publishes exit 0 and a startup line, and "
                    f"none arrived within {SERVER_START_TIMEOUT}s; stdout so far "
                    f"was {buffered[:300]!r}"
                )
                stop_server(proc)
                return 0, json.dumps(started), ""
            return run_line(line, home)
        finally:
            if server is not None:
                stop_server(server)

    @staticmethod
    def _start_graph_server(home):
        """Start the server a `graph client` example needs to reach the
        behaviour it publishes, and return the process so the caller can stop
        it. Starting one is fixture work, not a published invocation: the
        published `graph serve` examples are run and asserted on their own."""
        started, buffered, proc = start_server(
            f"rmp graph serve -r {FIXTURE_ROADMAP}", home, "socket")
        assert started is not None, (
            "the gate could not start the graph server its fixture needs; "
            f"stdout was {buffered[:300]!r}"
        )
        return proc

    # -- the three surfaces ------------------------------------------------

    def test_every_subcommand_example_runs_as_published(self):
        checked = 0
        for cmd_entry in self.contract["commands"]:
            for sub in cmd_entry["subcommands"]:
                label = (cmd_entry["name"] + " " + sub["name"]).strip()
                titles = [ex["title"] for ex in sub["examples"]]
                assert len(titles) == len(set(titles)), (
                    f"{label}: two examples share a title, so a preparation "
                    f"keyed by title cannot name one of them: {titles}"
                )
                for ex in sub["examples"]:
                    cmd = ex["cmd"]
                    if cmd in EXEMPT_EXAMPLES:
                        self.exempted.append(cmd)
                        continue
                    home = Workspace.fresh()
                    try:
                        prep = EXAMPLE_PREPARATIONS.get((label, ex["title"]))
                        if prep:
                            apply_preparation(prep, home)
                        code, out, err = self._run_published(
                            label, cmd, home,
                            needs_server=cmd in EXAMPLES_NEEDING_A_SERVER,
                            server_key=SERVER_INVOCATIONS.get(cmd),
                        )
                        self._assert_example(label, ex, sub, code, out, err, home)
                        self.executed.append(cmd)
                        checked += 1
                    finally:
                        shutil.rmtree(home, ignore_errors=True)

        assert checked >= 120, (
            f"only {checked} subcommand examples were executed; the corpus "
            f"traversal is broken and every assertion above is vacuous"
        )

    def _assert_example(self, label, ex, sub, code, out, err, home):
        want = ex["exit"]
        assert code == want, (
            f"{label}: `{ex['cmd']}` publishes exit {want} and produced {code}\n"
            f"  stdout: {out[:300]!r}\n  stderr: {err[:300]!r}"
        )
        if want != 0:
            assert out.strip() == "", (
                f"{label}: `{ex['cmd']}` publishes exit {want} and wrote to "
                f"stdout: {out[:200]!r} (SPEC/COMMANDS.md § Failing Invocations "
                f"Write Nothing to Stdout)"
            )
            published = ex.get("stderr") or ""
            if published:
                resolved = {"<socket>": self._socket_path(home)}
                observed = first_line(err)
                assert stderr_matches(self._substitute(published), observed, resolved), (
                    f"{label}: `{ex['cmd']}` publishes\n    {published!r}\n"
                    f"  and produced\n    {observed!r}"
                )
            return
        assert_stdout_shape(
            label, ex["cmd"], sub["stdout_on_success"]["kind"],
            sub["stdout_on_success"]["schema"], out,
        )
        published_stdout = ex.get("stdout")
        if published_stdout:
            assert_stdout_shape(
                label + " (published stdout)", ex["cmd"],
                sub["stdout_on_success"]["kind"],
                sub["stdout_on_success"]["schema"], published_stdout,
            )

    @staticmethod
    def _socket_path(home):
        return os.path.join(home, ".roadmaps", FIXTURE_ROADMAP, "graph.sock")

    def test_every_workflow_step_carries_a_declared_placeholder(self):
        seen = set()
        for workflow in self.contract["common_workflows"]:
            for step in workflow["steps"]:
                for token in re.findall(r"<[^>]+>", step["command"]):
                    seen.add(token)
                    assert token in WORKFLOW_PLACEHOLDERS, (
                        f"workflow {workflow['name']}: the step "
                        f"`{step['command']}` carries {token!r}, which is not in "
                        f"the declared placeholder vocabulary; the gate cannot "
                        f"fill it, so the step is not runnable"
                    )
        assert len(seen) >= 20, (
            f"only {len(seen)} distinct placeholders were seen across the "
            f"workflows; the traversal is broken"
        )
        stale = WORKFLOW_PLACEHOLDERS - seen
        assert not stale, (
            f"the vocabulary declares placeholders no workflow step uses: "
            f"{sorted(stale)}; remove them rather than leaving them to cover "
            f"nothing"
        )

    def test_every_workflow_runs_end_to_end(self):
        ran = 0
        for workflow in self.contract["common_workflows"]:
            home = Workspace.fresh()
            server = None
            try:
                plan = WORKFLOW_PLANS.get(workflow["name"])
                assert plan is not None, (
                    f"the contract publishes a workflow the gate has no plan "
                    f"for: {workflow['name']!r}. A workflow reaches a reader as "
                    f"a recipe to follow, so it is run or it is nothing."
                )
                apply_preparation({"rmp": plan.get("prerequisites", [])}, home)
                for index, step in enumerate(workflow["steps"]):
                    line = fill_placeholders(step["command"], plan, home, index)
                    key = SERVER_INVOCATIONS.get(strip_fixture(line))
                    if key:
                        started, buffered, proc = start_server(line, home, key)
                        assert started is not None, (
                            f"workflow {workflow['name']} step {index} "
                            f"(`{line}`) starts a server that never announced "
                            f"itself; stdout was {buffered[:300]!r}"
                        )
                        server = proc
                        # Started and asserted to have started, which is what
                        # this surface's oracle is; it counts as executed.
                        self.executed.append(step["command"])
                        continue
                    code, out, err = run_line(line, home)
                    assert code == 0, (
                        f"workflow {workflow['name']} step {index} was refused: "
                        f"`{line}`\n  exit={code}\n  stderr={err.strip()[:400]!r}\n"
                        f"  An agent following this recipe line by line is never "
                        f"to be refused by the binary."
                    )
                    self.executed.append(step["command"])
                self._assert_workflow_outcome(workflow["name"], plan, home)
                ran += 1
            finally:
                if server is not None:
                    stop_server(server)
                shutil.rmtree(home, ignore_errors=True)
        assert ran >= 8, f"only {ran} workflows ran; the traversal is broken"

    @staticmethod
    def _assert_workflow_outcome(name, plan, home):
        """A workflow that closes a task is asserted to have closed it.

        Running every step to exit 0 shows the recipe is accepted; it does not
        show the recipe achieves what its name says. `record_task_working_log`
        exists because its closing step lost `--commit-close` and the workflow
        stopped completing anything, so the outcome of exactly that step is the
        one worth reading back (rmp task 417).
        """
        expect = plan.get("completes_task")
        if expect is None:
            return
        _, out, _ = Workspace._rmp(
            ["task", "get", "-r", FIXTURE_ROADMAP, str(expect)], home, check=False)
        try:
            tasks = json.loads(out or "[]")
        except json.JSONDecodeError:
            tasks = []
        statuses = [t.get("status") for t in tasks]
        assert statuses == ["COMPLETED"], (
            f"workflow {name} ran every step to exit 0 but did not complete task "
            f"{expect}: its status is {statuses}. Every step being accepted is "
            f"not the same as the recipe achieving what it is named for."
        )

    def test_every_pitfall_example_reproduces_its_oracle(self):
        checked = 0
        for pitfall in self.contract["pitfalls"]:
            pid = pitfall["id"]
            for role, field, oracle in (
                ("wrong", "wrong_example", pitfall["wrong_exit"]),
                ("correct", "correct_example", 0),
            ):
                cmd = pitfall[field]
                home = Workspace.fresh()
                server = None
                try:
                    prep = PITFALL_PREPARATIONS.get((pid, role))
                    if prep:
                        apply_preparation(prep, home)
                    if (pid, role) in PITFALLS_NEEDING_A_SERVER:
                        started, buffered, proc = start_server(
                            f"rmp graph serve -r {FIXTURE_ROADMAP}", home, "socket")
                        assert started is not None, (
                            f"pitfall {pid} {role}: the gate could not start the "
                            f"graph server the example needs; stdout was "
                            f"{buffered[:300]!r}"
                        )
                        server = proc
                    code, out, err = run_line(self._substitute(cmd), home)
                    assert code == oracle, (
                        f"pitfall {pid} {field} publishes exit {oracle} and "
                        f"produced {code}\n  command: {cmd}\n"
                        f"  stderr: {err.strip()[:400]!r}"
                    )
                    if role == "wrong":
                        published = pitfall["wrong_stderr"]
                        resolved = {"<socket>": self._socket_path(home)}
                        observed = first_line(err)
                        if published == "":
                            assert observed == "", (
                                f"pitfall {pid} publishes an empty wrong_stderr "
                                f"-- its lesson is that nothing refuses the "
                                f"invocation -- and stderr carried {observed!r}"
                            )
                        else:
                            assert stderr_matches(
                                self._substitute(published), observed, resolved), (
                                f"pitfall {pid} publishes\n    {published!r}\n"
                                f"  and produced\n    {observed!r}"
                            )
                    self.executed.append(cmd)
                    checked += 1
                finally:
                    if server is not None:
                        stop_server(server)
                    shutil.rmtree(home, ignore_errors=True)
        assert checked >= 38, (
            f"only {checked} pitfall examples were executed; the traversal is "
            f"broken"
        )

    # -- the gate's own guarantees ----------------------------------------

    def test_the_exemption_list_carries_no_stale_entry(self):
        published = set()
        for cmd_entry in self.contract["commands"]:
            for sub in cmd_entry["subcommands"]:
                for ex in sub["examples"]:
                    published.add(ex["cmd"])
        for cmd, why in EXEMPT_EXAMPLES.items():
            assert cmd in published, (
                f"the exemption for `{cmd}` ({why}) names an example the "
                f"contract no longer publishes; remove it rather than leaving "
                f"it to cover nothing"
            )
            assert why.strip(), f"the exemption for `{cmd}` states no reason"

    def test_the_comparator_rejects_a_degraded_string(self):
        """SPEC/DATA_FORMATS.md § Examples that cannot be run as published,
        rule 5: the gate proves it can fail.

        A comparator that has silently become permissive reports a contract
        that has silently become wrong, so it is shown here to accept a real
        published line and to reject three degradations of it: the sentinel
        removed, the `Error: ` prefix removed, and -- for a line carrying a
        placeholder -- a span that is not the value the gate resolved.
        """
        published = 'Error: resource not found: roadmap "missing"'
        assert stderr_matches(published, published, {})

        without_sentinel = published.replace("resource not found: ", "")
        assert not stderr_matches(published, without_sentinel, {}), (
            "the comparator accepted a line with its sentinel removed"
        )
        without_prefix = published.replace("Error: ", "")
        assert not stderr_matches(published, without_prefix, {}), (
            "the comparator accepted a line with its `Error: ` prefix removed"
        )

        placeheld = "Error: graph server error: no graph server is listening on <socket>"
        resolved = {"<socket>": "/tmp/rmpg000/w1/.roadmaps/gatefix/graph.sock"}
        good = placeheld.replace("<socket>", resolved["<socket>"])
        assert stderr_matches(placeheld, good, resolved)
        wrong_span = placeheld.replace("<socket>", "/home/user/.roadmaps/myproject/graph.sock")
        assert not stderr_matches(placeheld, wrong_span, resolved), (
            "the comparator accepted a line whose placeholder span is not the "
            "value the gate resolved for it, which is what would let an "
            "invented literal pass as an exact string"
        )
        assert not stderr_matches(placeheld, good, {}), (
            "the comparator accepted a placeholder it could not resolve; such "
            "a line must be exempted by name instead"
        )

        # The shape assertions must reject as well as accept.
        try:
            assert_stdout_shape("self-test", "x", "object", '{"id": <int>}', "[]")
        except AssertionError:
            pass
        else:
            raise AssertionError("the stdout comparator accepted an array where "
                                 "the schema declares an object")
        try:
            assert_stdout_shape("self-test", "x", "object", '{"id": <int>}', '{"name":1}')
        except AssertionError:
            pass
        else:
            raise AssertionError("the stdout comparator accepted an object "
                                 "without the key its schema names")

    def test_zz_coverage_report(self):
        """SPEC/DATA_FORMATS.md § Examples that cannot be run as published,
        rule 4: the gate counts what it executed against what the contract
        published and fails on a shortfall. A gate that passes because it
        found nothing to run is a gate that has stopped working."""
        published = []
        for cmd_entry in self.contract["commands"]:
            for sub in cmd_entry["subcommands"]:
                for ex in sub["examples"]:
                    published.append(ex["cmd"])
        for workflow in self.contract["common_workflows"]:
            for step in workflow["steps"]:
                published.append(step["command"])
        for pitfall in self.contract["pitfalls"]:
            published.append(pitfall["wrong_example"])
            published.append(pitfall["correct_example"])

        # Re-run the three surfaces so this report counts what THIS method
        # observed rather than what an earlier method left in an attribute.
        self.test_every_subcommand_example_runs_as_published()
        self.test_every_workflow_runs_end_to_end()
        self.test_every_pitfall_example_reproduces_its_oracle()

        accounted = len(self.executed) + len(self.exempted)
        print()
        print("published invocations:", len(published))
        print("  executed :", len(self.executed))
        print("  exempted :", len(self.exempted))
        print("  accounted:", accounted)
        for cmd, why in sorted(EXEMPT_EXAMPLES.items()):
            print(f"  EXEMPT  {cmd}")
            print(f"          {why}")

        assert accounted == len(published), (
            f"{len(published) - accounted} published invocation(s) are neither "
            f"executed nor exempted; every one of them is a claim about the "
            f"binary that nothing checks"
        )


# ---------------------------------------------------------------------------
# Workflow plans
# ---------------------------------------------------------------------------
#
# One plan per published workflow: the prerequisites the workflow states, and
# the value each placeholder takes. A value is either a literal or a callable
# resolved against the live roadmap at the moment the step runs, which is how
# `<comment-id>` names the comment an earlier step of the same workflow created.


def _sprint_ids(home, status=None):
    args = ["sprint", "list", "-r", FIXTURE_ROADMAP]
    if status:
        args += ["--status", status]
    _, out, _ = Workspace._rmp(args, home, check=False)
    try:
        return [s["id"] for s in json.loads(out or "[]")]
    except (json.JSONDecodeError, TypeError):
        return []


def _latest_task_comment_id(home, task_id):
    _, out, _ = Workspace._rmp(
        ["task", "comment-list", "-r", FIXTURE_ROADMAP, str(task_id)], home, check=False)
    try:
        rows = json.loads(out or "[]")
    except json.JSONDecodeError:
        return None
    return max((row["id"] for row in rows), default=None)


TITLE = "Harden the write path"
MACRO_GOAL = "Deliver a single audited write path for every mutating command."
FR = "Every mutating command records who changed what and when."
TR = "Route all writes through one transaction helper."
AC = "The audit log carries one entry per mutating invocation."
SUMMARY = ("Routed every mutating command through the shared transaction helper "
           "and covered the change with an end-to-end audit assertion.")

WORKFLOW_PLANS = {
    "bootstrap_new_project": {
        "prerequisites": [],
        "values": {
            "<name>": "newproject",
            "<title>": TITLE,
            "<functional-requirements>": FR,
            "<technical-requirements>": TR,
            "<acceptance-criteria>": AC,
            "<TYPE>": "TASK",
            "<0-9>": "7",
            "<macro-goal-description>": MACRO_GOAL,
            "<n>": "10",
            # A roadmap created by step 0 issues ids from 1 upward.
            "<sprint-id>": "1",
            "<task-id-1,task-id-2,...>": "1",
        },
    },
    "plan_next_sprint": {
        "prerequisites": [],
        "values": {
            "<name>": FIXTURE_ROADMAP,
            "<title>": TITLE,
            "<macro-goal-description>": MACRO_GOAL,
            "<n>": "10",
            "<sprint-id>": lambda home: str(max(_sprint_ids(home))),
            "<task-id-1,task-id-2,...>": "4,5,6",
        },
    },
    "close_active_sprint_and_open_next": {
        "prerequisites": [["sprint", "start", "-r", FIXTURE_ROADMAP, "5"]],
        "values": {
            "<name>": FIXTURE_ROADMAP,
            "<open-sprint-id>": "5",
            "<task-id-1,task-id-2,...>": "1,2,3,7",
            "<next-pending-sprint-id>": "6",
        },
    },
    "reprioritise_backlog": {
        "prerequisites": [],
        "values": {
            "<name>": FIXTURE_ROADMAP,
            "<task-id-1,task-id-2,...>": "4,5,6",
            "<new-priority>": "8",
        },
    },
    "move_task_between_sprints": {
        "prerequisites": [],
        "values": {
            "<name>": FIXTURE_ROADMAP,
            "<from-sprint-id>": "5",
            "<to-sprint-id>": "6",
            "<task-id-1,task-id-2,...>": "1,2",
        },
    },
    "complete_task_with_summary": {
        "prerequisites": [],
        "completes_task": 3,
        "values": {
            "<name>": FIXTURE_ROADMAP,
            "<task-id>": "3",
            "<hash>": OPEN_HASH,
            "<one-paragraph completion summary>": SUMMARY,
        },
    },
    "record_task_working_log": {
        # The closing step transitions the task to COMPLETED, which is legal
        # from TESTING alone; the workflow states the precondition and the
        # gate establishes it.
        "prerequisites": _to_testing("3"),
        "completes_task": 3,
        "values": {
            "<name>": FIXTURE_ROADMAP,
            "<task-id>": "3",
            "<hash>": CLOSE_HASH,
            "<comment-id>": lambda home: str(_latest_task_comment_id(home, 3)),
            "<one-paragraph completion summary>": SUMMARY,
            "<the proposition about to be tested>":
                "The retry ladder is what recovers the contended write.",
            "<the test or verification that was run, and what it showed>":
                "Ran the contended write 2000 times with the ladder disabled: "
                "0.15 per cent succeeded.",
            "<the behaviour observed, the measurement taken, or the cause identified>":
                "With the ladder enabled the same run succeeds 79.9 per cent of "
                "the time, so the delay is what buys the recovery.",
            "<the decision taken and the reasoning behind it>":
                "Kept the ladder and published its shape, because a caller that "
                "reimplements it has to know the delays.",
        },
    },
    "build_knowledge_graph": {
        "prerequisites": [],
        "values": {
            "<name>": FIXTURE_ROADMAP,
            "<key>": "auth",
            "<title>": TITLE,
            "<from>": "auth",
            "<to>": "internal/auth",
        },
    },
}


def fill_placeholders(command, plan, home, index):
    """Replace every declared placeholder in a workflow step with the value the
    plan gives it, resolving a callable against the live roadmap."""
    line = command
    for token in sorted(WORKFLOW_PLACEHOLDERS, key=len, reverse=True):
        if token not in line:
            continue
        assert token in plan["values"], (
            f"step {index} carries {token!r} and the plan gives it no value"
        )
        value = plan["values"][token]
        if callable(value):
            value = value(home)
        assert value and value != "None", (
            f"step {index}: the plan resolved {token!r} to {value!r}"
        )
        line = line.replace(token, value)
    return line


def strip_fixture(line):
    """The published form of a filled line, for looking it up in
    SERVER_INVOCATIONS."""
    return line.replace(FIXTURE_ROADMAP, STAND_IN)


# ---------------------------------------------------------------------------
# Gate 2: the per-subcommand exit-code array is exhaustive
# ---------------------------------------------------------------------------
#
# SPEC/DATA_FORMATS.md § Field reference: per-subcommand exit code entry,
# rule 5.

# Valid positional arguments per subcommand, for the probe sweep and for the
# generic drivers. Keyed by the subcommand's contract label.
VALID_POSITIONALS = {
    "roadmap list": [], "roadmap create": ["brandnew"], "roadmap remove": ["mobile-app"],
    "task list": [], "task create": [], "task get": ["1"], "task next": ["1"],
    "task edit": ["1"], "task remove": ["9"], "task stat": ["1", "COMPLETED"],
    "task reopen": ["1"], "task prio": ["1", "5"], "task sev": ["1", "5"],
    "task subtasks": ["1"], "task add-dep": ["4", "5"], "task remove-dep": ["4", "5"],
    "task blockers": ["1"], "task blocking": ["1"], "task comment-add": ["1"],
    "task comment-list": ["1"], "task comment-edit": ["1"], "task comment-remove": ["1"],
    "sprint list": [], "sprint create": [], "sprint get": ["1"], "sprint show": ["1"],
    "sprint update": ["1"], "sprint remove": ["4"], "sprint start": ["1"],
    "sprint close": ["1"], "sprint reopen": ["1"], "sprint tasks": ["1"],
    "sprint open-tasks": ["1"], "sprint stats": ["1"], "sprint add-tasks": ["1", "4"],
    "sprint remove-tasks": ["5", "3"], "sprint move-tasks": ["5", "6", "3"],
    "sprint reorder": ["5", "1,2,3,7"], "sprint move-to": ["5", "3", "0"],
    "sprint swap": ["5", "3", "7"], "sprint top": ["5", "3"], "sprint bottom": ["5", "3"],
    "sprint comment-add": ["1"], "sprint comment-list": ["1"],
    "sprint comment-edit": ["1"], "sprint comment-remove": ["1"],
    "backlog list": [], "backlog show-next": ["5"], "audit list": [],
    "audit history": ["TASK", "1"], "audit stats": [], "stats": [],
    "graph serve": [], "graph client": [], "web": [], "ai-help": [],
}

# The flags a subcommand needs before its positional arguments are meaningful.
EXTRA_FLAGS = {
    "task create": ["-t", "T", "-fr", "f", "-tr", "t", "-ac", "a"],
    "sprint create": ["-t", "T", "-d", "Deliver the fixture state."],
    "task comment-add": ["--type", "NOTE", "--body", "b"],
    "sprint comment-add": ["--type", "FINDING", "--body", "b"],
    "task comment-edit": ["--body", "b"],
    "sprint comment-edit": ["--body", "b"],
    "sprint update": ["-t", "Renamed sprint"],
    "graph client": ["--query", "MATCH (n) RETURN n"],
}

# Subcommands that take no roadmap selector.
NO_ROADMAP = {"roadmap list", "roadmap create", "roadmap remove", "web", "ai-help"}

# Subcommands whose invocation blocks; the probe sweep leaves them to the
# drivers, which start and stop them deliberately.
BLOCKING_SUBCOMMANDS = {"graph serve", "web"}


def subcommand_label(cmd_entry, sub):
    """The label and argv prefix of one subcommand. A leaf command (`stats`,
    `web`, `ai-help`) publishes a subcommand whose name repeats the family's,
    and its argv carries the family name alone."""
    if sub["name"] == cmd_entry["name"]:
        return cmd_entry["name"], [cmd_entry["name"]]
    return (cmd_entry["name"] + " " + sub["name"]), [cmd_entry["name"], sub["name"]]


def _corrupt_database(home):
    """Reproduce, exactly, the fault the comment subcommands' own condition for
    exit 1 names: "The roadmap database could not be read or written." """
    path = os.path.join(home, ".roadmaps", FIXTURE_ROADMAP, "project.db")
    with open(path, "wb") as fh:
        fh.write(b"this is not a SQLite database")


# Drivers for the codes no generic probe and no published example reaches.
# Each entry is (label, code) -> a callable taking the working home and
# returning the observed exit code.


def _driver_corrupt_db(argv):
    def run(home):
        _corrupt_database(home)
        code, _, _ = Workspace._rmp(argv, home, check=False)
        return code
    return run


def _driver_plain(argv, prep=()):
    def run(home):
        for step in prep:
            Workspace._rmp(step, home, check=False)
        code, _, _ = Workspace._rmp(argv, home, check=False)
        return code
    return run


def _driver_second_graph_server(home):
    started, buffered, proc = start_server(
        f"rmp graph serve -r {FIXTURE_ROADMAP}", home, "socket")
    assert started is not None, (
        f"the gate could not start the first graph server; stdout {buffered[:200]!r}")
    try:
        code, _, _ = Workspace._rmp(["graph", "serve", "-r", FIXTURE_ROADMAP], home,
                                    check=False)
        return code
    finally:
        stop_server(proc)


def _driver_web_port_in_use(home):
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        sock.listen(1)
        port = sock.getsockname()[1]
        code, _, _ = Workspace._rmp(["web", "--port", str(port), "--no-open"], home,
                                    check=False)
        return code


def _driver_graph_client_oversized(home):
    started, buffered, proc = start_server(
        f"rmp graph serve -r {FIXTURE_ROADMAP}", home, "socket")
    assert started is not None
    try:
        env = _env_for(home)
        # Past the published 1048576-byte cap, supplied on standard input
        # because a single argv entry is capped well below it.
        statement = "RETURN 1 // " + ("x" * 1_048_600)
        proc2 = subprocess.run(
            [resolve_cli(), "graph", "client", "-r", FIXTURE_ROADMAP],
            input=statement, capture_output=True, text=True, env=env, timeout=60)
        return proc2.returncode
    finally:
        stop_server(proc)


RESIDUE_DRIVERS = {
    ("task comment-add", 1): _driver_corrupt_db(
        ["task", "comment-add", "-r", FIXTURE_ROADMAP, "1", "--type", "NOTE", "--body", "b"]),
    ("task comment-edit", 1): _driver_corrupt_db(
        ["task", "comment-edit", "-r", FIXTURE_ROADMAP, "1", "--body", "b"]),
    ("task comment-remove", 1): _driver_corrupt_db(
        ["task", "comment-remove", "-r", FIXTURE_ROADMAP, "1"]),
    ("sprint comment-add", 1): _driver_corrupt_db(
        ["sprint", "comment-add", "-r", FIXTURE_ROADMAP, "1", "--type", "FINDING", "--body", "b"]),
    ("sprint comment-edit", 1): _driver_corrupt_db(
        ["sprint", "comment-edit", "-r", FIXTURE_ROADMAP, "1", "--body", "b"]),
    ("sprint comment-remove", 1): _driver_corrupt_db(
        ["sprint", "comment-remove", "-r", FIXTURE_ROADMAP, "1"]),
    ("graph serve", 1): _driver_second_graph_server,
    ("graph client", 1): _driver_plain(
        ["graph", "client", "-r", FIXTURE_ROADMAP, "--query", "RETURN 1"]),
    ("graph client", 6): _driver_graph_client_oversized,
    ("web", 1): _driver_web_port_in_use,
    ("sprint create", 5): _driver_plain(
        ["sprint", "create", "-r", FIXTURE_ROADMAP, "-t", "Duplicate order",
         "-d", "Deliver the duplicate-order refusal.", "--order", "1"]),
    ("sprint update", 5): _driver_plain(
        ["sprint", "update", "-r", FIXTURE_ROADMAP, "2", "--order", "1"]),
    ("sprint remove-tasks", 6): _driver_plain(
        ["sprint", "remove-tasks", "-r", FIXTURE_ROADMAP, "5", "44"]),
    ("sprint top", 6): _driver_plain(["sprint", "top", "-r", FIXTURE_ROADMAP, "5", "44"]),
    ("sprint bottom", 6): _driver_plain(["sprint", "bottom", "-r", FIXTURE_ROADMAP, "5", "44"]),
    ("sprint move-tasks", 6): _driver_plain(
        ["sprint", "move-tasks", "-r", FIXTURE_ROADMAP, "5", "6", "44"]),
    ("sprint reorder", 6): _driver_plain(
        ["sprint", "reorder", "-r", FIXTURE_ROADMAP, "5", "1,2,3"]),
    ("sprint move-to", 6): _driver_plain(
        ["sprint", "move-to", "-r", FIXTURE_ROADMAP, "5", "44", "0"]),
    ("sprint swap", 6): _driver_plain(
        ["sprint", "swap", "-r", FIXTURE_ROADMAP, "5", "3", "3"]),
    ("sprint add-tasks", 6): _driver_plain(
        ["sprint", "add-tasks", "-r", FIXTURE_ROADMAP, "3", "9"]),
    ("sprint start", 6): _driver_plain(
        ["sprint", "start", "-r", FIXTURE_ROADMAP, "3"]),
    ("sprint close", 6): _driver_plain(
        ["sprint", "close", "-r", FIXTURE_ROADMAP, "5"]),
    ("sprint reopen", 6): _driver_plain(
        ["sprint", "reopen", "-r", FIXTURE_ROADMAP, "5"]),
    ("sprint tasks", 6): _driver_plain(
        ["sprint", "tasks", "-r", FIXTURE_ROADMAP, "5", "-s", "NOT_A_STATUS"]),
    ("sprint comment-list", 6): _driver_plain(
        ["sprint", "comment-list", "-r", FIXTURE_ROADMAP, "3", "--type", "NOT_A_TYPE"]),
    ("sprint comment-edit", 6): _driver_plain(
        ["sprint", "comment-edit", "-r", FIXTURE_ROADMAP, "1", "--type", "NOT_A_TYPE"]),
    ("task comment-list", 6): _driver_plain(
        ["task", "comment-list", "-r", FIXTURE_ROADMAP, "42", "--type", "NOT_A_TYPE"]),
    ("task comment-edit", 6): _driver_plain(
        ["task", "comment-edit", "-r", FIXTURE_ROADMAP, "1", "--type", "NOT_A_TYPE"]),
    ("task get", 6): _driver_plain(
        ["task", "get", "-r", FIXTURE_ROADMAP, "2147483648"]),
    ("task next", 6): _driver_plain(["task", "next", "-r", FIXTURE_ROADMAP, "0"]),
    ("task edit", 6): _driver_plain(
        ["task", "edit", "-r", FIXTURE_ROADMAP, "1", "-p", "99"]),
    ("task reopen", 6): _driver_plain(
        ["task", "reopen", "-r", FIXTURE_ROADMAP, "2147483648"]),
    ("task prio", 6): _driver_plain(["task", "prio", "-r", FIXTURE_ROADMAP, "1", "99"]),
    ("task sev", 6): _driver_plain(["task", "sev", "-r", FIXTURE_ROADMAP, "1", "99"]),
    ("task add-dep", 6): _driver_plain(
        ["task", "add-dep", "-r", FIXTURE_ROADMAP, "4", "4"]),
    ("task create", 6): _driver_plain(
        ["task", "create", "-r", FIXTURE_ROADMAP, "-t", "T", "-fr", "f", "-tr", "t",
         "-ac", "a", "--priority", "99"]),
    ("task create", 4): _driver_plain(
        ["task", "create", "-r", FIXTURE_ROADMAP, "-t", "T", "-fr", "f", "-tr", "t",
         "-ac", "a", "--parent", "99999"]),
    ("task stat", 4): _driver_plain(
        ["task", "stat", "-r", FIXTURE_ROADMAP, "99999", "TESTING"]),
    ("task comment-add", 4): _driver_plain(
        ["task", "comment-add", "-r", FIXTURE_ROADMAP, "99999", "--type", "NOTE",
         "--body", "b"]),
    ("sprint comment-add", 4): _driver_plain(
        ["sprint", "comment-add", "-r", FIXTURE_ROADMAP, "99999", "--type", "FINDING",
         "--body", "b"]),
    ("task remove", 4): _driver_plain(
        ["task", "remove", "-r", FIXTURE_ROADMAP, "99999"]),
    ("task list", 4): _driver_plain(["task", "list", "-r", "nosuchroadmap"]),
    ("sprint list", 4): _driver_plain(["sprint", "list", "-r", "nosuchroadmap"]),
    ("sprint create", 4): _driver_plain(
        ["sprint", "create", "-r", "nosuchroadmap", "-t", "T", "-d", "Deliver it."]),
    ("backlog list", 4): _driver_plain(["backlog", "list", "-r", "nosuchroadmap"]),
    ("backlog show-next", 4): _driver_plain(["backlog", "show-next", "-r", "nosuchroadmap"]),
    ("audit list", 4): _driver_plain(["audit", "list", "-r", "nosuchroadmap"]),
    ("audit history", 4): _driver_plain(
        ["audit", "history", "-r", "nosuchroadmap", "TASK", "1"]),
    ("audit stats", 4): _driver_plain(["audit", "stats", "-r", "nosuchroadmap"]),
    ("roadmap list", 2): _driver_plain(["roadmap", "list", "--zzz-unknown"]),
    # The generic exit-2 driver reads a "-"-prefixed token as a positional id,
    # which these five do not do: `roadmap create notanumber` creates a roadmap
    # of that name, `task next -r <n> notanumber` refuses the count with exit 6,
    # and `audit history -r <n> notanumber 1` refuses the entity type the same
    # way. Each is driven instead by the condition it really has for the code.
    ("roadmap create", 2): _driver_plain(["roadmap", "create"]),
    ("roadmap remove", 2): _driver_plain(["roadmap", "remove"]),
    ("task next", 2): _driver_plain(["task", "next", "-r", FIXTURE_ROADMAP, "1", "surplus"]),
    ("backlog show-next", 2): _driver_plain(
        ["backlog", "show-next", "-r", FIXTURE_ROADMAP, "5", "surplus"]),
    ("audit history", 2): _driver_plain(["audit", "history", "-r", FIXTURE_ROADMAP]),
    ("stats", 2): _driver_plain(["stats", "-r", FIXTURE_ROADMAP, "--zzz-unknown"]),
    ("graph serve", 2): _driver_plain(["graph", "serve", "-r", FIXTURE_ROADMAP, "--zzz"]),
    ("web", 2): _driver_plain(["web", "--zzz-unknown"]),
    ("web", 6): _driver_plain(["web", "--port", "70000"]),
    ("ai-help", 2): _driver_plain(["ai-help", "stray"]),
    ("roadmap create", 6): _driver_plain(["roadmap", "create", "My Project!"]),
    ("roadmap remove", 6): _driver_plain(["roadmap", "remove", "My Project!"]),
    ("sprint create", 6): _driver_plain(
        ["sprint", "create", "-r", FIXTURE_ROADMAP, "-t", "T",
         "-d", "Deliver the range refusal.", "--max-tasks", "0"]),
    ("sprint update", 6): _driver_plain(
        ["sprint", "update", "-r", FIXTURE_ROADMAP, "2", "--max-tasks", "0"]),
    ("graph serve", 0): lambda home: _server_starts(home, "graph serve"),
    ("web", 0): lambda home: _server_starts(home, "web"),
}


def _server_starts(home, which):
    line = (f"rmp graph serve -r {FIXTURE_ROADMAP}" if which == "graph serve"
            else "rmp web --no-open")
    key = "socket" if which == "graph serve" else "url"
    started, buffered, proc = start_server(line, home, key)
    try:
        return 0 if started is not None else "NO-STARTUP-LINE"
    finally:
        stop_server(proc)


class TestSubcommandExitCodesAreExhaustive:
    """SPEC/DATA_FORMATS.md § Field reference: per-subcommand exit code entry,
    rule 5: the array is exhaustive over the conditions a caller controls. A
    code such a condition produces and the array omits is a defect, and so is
    a code the array publishes that the subcommand cannot emit. The rule, not
    this docstring, draws the boundary with the environment; the module
    docstring says how this gate applies it."""

    def setup_method(self):
        self.contract = Workspace.contract()

    def teardown_method(self):
        pass

    def _probes(self, label, argv, sub):
        """The closed family of command-line probes, derived from the
        subcommand's own contract entry."""
        positionals = VALID_POSITIONALS[label]
        extra = EXTRA_FLAGS.get(label, [])
        selector = [] if label in NO_ROADMAP else ["-r", FIXTURE_ROADMAP]
        probes = []
        if label not in NO_ROADMAP:
            probes.append(("no roadmap selector", argv + extra + positionals))
            probes.append(("roadmap absent",
                           argv + ["-r", "nosuchroadmap"] + extra + positionals))
        probes.append(("unknown flag after the arguments",
                       argv + selector + extra + positionals + ["--zzz-unknown"]))
        probes.append(("surplus positional argument",
                       argv + selector + extra + positionals + ["surplus"]))
        if sub.get("positional_arguments"):
            probes.append(("unknown flag in the first positional slot",
                           argv + selector + extra + ["--zzz-unknown"]))
            probes.append(("malformed positional id",
                           argv + selector + extra + ["notanumber"] + positionals[1:]))
            probes.append(("no positional argument at all", argv + selector + extra))
        for flag in sub["flags"]:
            long = flag["long"]
            if long in ("--help", "--roadmap"):
                continue
            if flag["type"] != "boolean":
                probes.append((f"{long} written with no value",
                               argv + selector + extra + positionals + [long]))
            if flag["type"] == "enum":
                probes.append((f"{long} outside its enum",
                               argv + selector + extra + positionals + [long, "NOT_A_MEMBER"]))
            if flag["type"] == "integer":
                probes.append((f"{long} not an integer",
                               argv + selector + extra + positionals + [long, "abc"]))
                probes.append((f"{long} out of range",
                               argv + selector + extra + positionals + [long, "999999"]))
            if flag["type"] == "date":
                probes.append((f"{long} not a date",
                               argv + selector + extra + positionals + [long, "not-a-date"]))
        return probes

    def test_no_subcommand_emits_a_code_it_does_not_declare(self):
        probes_run = 0
        problems = []
        for cmd_entry in self.contract["commands"]:
            for sub in cmd_entry["subcommands"]:
                label, argv = subcommand_label(cmd_entry, sub)
                if label in BLOCKING_SUBCOMMANDS:
                    # Their success path blocks, so a probe that happens to
                    # succeed would hang; they are driven, not swept.
                    continue
                declared = {e["code"] for e in sub["exit_codes"]}
                for why, probe in self._probes(label, argv, sub):
                    home = Workspace.fresh()
                    try:
                        code, _, err = Workspace._rmp(probe, home, check=False)
                    finally:
                        shutil.rmtree(home, ignore_errors=True)
                    probes_run += 1
                    if code not in declared:
                        problems.append(
                            f"{label}: `rmp {' '.join(probe)}` ({why}) exits "
                            f"{code}, which its exit_codes array does not "
                            f"declare (declared: {sorted(declared)}); stderr: "
                            f"{first_line(err)!r}"
                        )
        assert probes_run >= 350, (
            f"only {probes_run} probes ran; the sweep is broken and every "
            f"comparison above is vacuous"
        )
        assert not problems, (
            f"{len(problems)} subcommand(s) emit an undeclared exit code:\n  "
            + "\n  ".join(problems)
        )

    def test_every_declared_code_can_be_driven(self):
        driven = 0
        problems = []
        undriven = []
        for cmd_entry in self.contract["commands"]:
            for sub in cmd_entry["subcommands"]:
                label, argv = subcommand_label(cmd_entry, sub)
                for entry in sub["exit_codes"]:
                    code = entry["code"]
                    driver = self._driver_for(label, argv, sub, code)
                    if driver is None:
                        undriven.append(f"{label}: exit {code}")
                        continue
                    home = Workspace.fresh()
                    try:
                        observed = driver(home)
                    finally:
                        shutil.rmtree(home, ignore_errors=True)
                    driven += 1
                    if observed != code:
                        problems.append(
                            f"{label}: exit {code} is declared, and the driver "
                            f"for it produced {observed}"
                        )
        assert not undriven, (
            f"{len(undriven)} declared exit code(s) have no driver, so nothing "
            f"shows the subcommand can produce them:\n  " + "\n  ".join(undriven)
        )
        assert driven >= 200, (
            f"only {driven} declared codes were driven; the traversal is broken"
        )
        assert not problems, (
            f"{len(problems)} declared exit code(s) were not produced by the "
            f"invocation that is meant to produce them:\n  " + "\n  ".join(problems)
        )

    def _driver_for(self, label, argv, sub, code):
        """A driver for one (subcommand, code) pair.

        The generic ones come from the shared conditions the registry itself
        publishes, which is what keeps this table from being 262 hand-written
        invocations. Everything else is named in RESIDUE_DRIVERS.
        """
        if (label, code) in RESIDUE_DRIVERS:
            return RESIDUE_DRIVERS[(label, code)]

        from_example = self._example_driver(label, sub, code)
        if from_example is not None:
            return from_example

        positionals = VALID_POSITIONALS[label]
        extra = EXTRA_FLAGS.get(label, [])
        selector = [] if label in NO_ROADMAP else ["-r", FIXTURE_ROADMAP]

        if code == 3 and label not in NO_ROADMAP:
            return _driver_plain(argv + extra + positionals)
        if code == 4 and label not in NO_ROADMAP:
            return _driver_plain(argv + ["-r", "nosuchroadmap"] + extra + positionals)
        if code == 2:
            if sub.get("positional_arguments"):
                return _driver_plain(argv + selector + extra + ["notanumber"] + positionals[1:])
            return _driver_plain(argv + selector + extra + positionals + ["--zzz-unknown"])
        return None

    def _example_driver(self, label, sub, code):
        """A code is driven by one of the subcommand's own published examples
        wherever it publishes one for that code.

        Reusing the example is deliberate: the invocation the contract
        advertises for a failure is exactly the invocation a reader will run,
        so driving the declared code with it asserts the two together. Gate 1
        above asserts the same invocation's stderr line; this leg asserts only
        that the code the subcommand declares is the code it produces.
        """
        for ex in sub["examples"]:
            if ex["exit"] != code:
                continue
            if ex["cmd"] in EXEMPT_EXAMPLES:
                continue
            cmd = ex["cmd"]
            prep = EXAMPLE_PREPARATIONS.get((label, ex["title"]), {})
            needs_server = cmd in EXAMPLES_NEEDING_A_SERVER

            def run(home, cmd=cmd, prep=prep, needs_server=needs_server):
                apply_preparation(prep, home)
                server = None
                if needs_server:
                    started, buffered, proc = start_server(
                        f"rmp graph serve -r {FIXTURE_ROADMAP}", home, "socket")
                    assert started is not None
                    server = proc
                try:
                    code, _, _ = run_line(cmd.replace(STAND_IN, FIXTURE_ROADMAP), home)
                    return code
                finally:
                    if server is not None:
                        stop_server(server)
            return run
        return None

    def test_zz_exit_code_coverage_report(self):
        total = 0
        per_code = {}
        for cmd_entry in self.contract["commands"]:
            for sub in cmd_entry["subcommands"]:
                for entry in sub["exit_codes"]:
                    total += 1
                    per_code[entry["code"]] = per_code.get(entry["code"], 0) + 1
        print()
        print("per-subcommand exit code entries:", total)
        for code in sorted(per_code):
            print(f"  code {code}: declared by {per_code[code]} subcommand(s)")
        print("  residue drivers hand-written:", len(RESIDUE_DRIVERS))
        assert total >= 250, (
            f"only {total} per-subcommand exit code entries were read; the "
            f"traversal is broken"
        )


# ---------------------------------------------------------------------------
# Runner
# ---------------------------------------------------------------------------


def _run_all():
    module = sys.modules[__name__]
    classes = [
        obj for name, obj in vars(module).items()
        if isinstance(obj, type) and (name.startswith("Test") or name.endswith("Tests"))
        and any(m.startswith("test_") for m in dir(obj))
    ]
    passed = failed = 0
    failures = []
    for cls in classes:
        for method in sorted(m for m in dir(cls) if m.startswith("test_")):
            instance = cls()
            instance.setup_method()
            try:
                getattr(instance, method)()
                passed += 1
                print(f"✓ {cls.__name__}.{method}")
            except AssertionError as exc:
                failed += 1
                failures.append((f"{cls.__name__}.{method}", exc))
                print(f"✗ {cls.__name__}.{method}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((f"{cls.__name__}.{method}", exc))
                print(f"✗ {cls.__name__}.{method} (error)")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Published contract execution tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    try:
        ok = _run_all()
    finally:
        Workspace.cleanup()
    sys.exit(0 if ok else 1)
