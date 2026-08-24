#!/usr/bin/env python3
"""
Test 58: the AI contract's published example error strings vs. what the binary
prints (rmp task #317).

`rmp --ai-help` is the machine-readable contract this project tells AI agents
to consult so they need not guess. Every command and subcommand in it carries
`examples`, and a failing example publishes the stderr line it produces. Those
lines come from the `Example.Stderr` fields of the Go command registries
(internal/commands/registry_task.go, registry_sprint.go, registry_data.go,
registry_graph.go, registry_web.go, registry_aihelp.go) and reach the reader
verbatim. Nothing verified them against the binary until this module existed,
and eight of the sixty-eight had drifted:

  * five had silently dropped the sentinel the binary prints between
    `Error: ` and the detail (`validation error: `, `resource not found: `,
    `resource already exists: `);
  * two still carried a duplicated `: invalid task status` /
    `: invalid sprint status` tail that internal/commands' enum-message
    deduplication had already removed from the binary (see
    internal/commands/enum_message_dedup_test.go);
  * one published an unquoted enum value the binary quotes.

That is worse than a wording slip. The sentinel names the failure CLASS and
determines the exit code (SPEC/ARCHITECTURE.md § Sentinel Error Catalogue), so
a line missing it misrepresents what went wrong, not merely how it reads. An
agent that writes an assertion from a published example writes one that fails,
and the failure looks like a bug in the code under test rather than in the
contract.

WHY THIS GATE DRIVES THE COMPILED BINARY
----------------------------------------
The message body is assembled by `fmt.Errorf` calls scattered across
internal/commands; only the sentinel and the exit code can be derived
statically from the error values. The complete line exists nowhere but in the
process's own stderr, so the only way to know what the binary prints is to run
it. This module therefore builds one throwaway roadmap, replays every failing
example the contract publishes, and compares captured stderr against the
published string CHARACTER FOR CHARACTER, plus the captured exit code against
the published one.

The corpus is read from `rmp --ai-help` rather than by parsing the Go source.
That is deliberate: the published JSON is the surface an agent actually
consumes, it needs no Go string-literal unescaping to recover the intended
text, and a registry edit that fails to reach the contract is a defect this
module should see rather than compensate for.

ONE GATE OR TWO
---------------
This is a SECOND module rather than an extension of
tests/test_55_error_string_parity.py, which gates the OTHER published surface
(the strings SPEC/COMMANDS.md prints in its tables and fences, rmp task #277).
Both surfaces must equal the binary, but they are gated separately because:

  1. The derivation differs in kind. Markdown carries a string with no way to
     say which invocation produces it, so test_55 must hand-write an
     invocation per string and account for coverage by hand. The AI contract
     carries `cmd` NEXT TO `stderr`, so this module derives every invocation
     from the contract itself and hand-writes none. Merging would put two
     incompatible drivers, and two incompatible notions of "covered", in one
     file.
  2. Failure attribution stays legible. A red here says the Go registry is
     stale; a red in test_55 says the markdown is stale. Merged, a reader
     would have to work out which surface broke.
  3. test_55 is already ~1850 lines of per-string cases.

The two surfaces are still held to ONE convention, and this module checks that
directly: SPEC/COMMANDS.md § "Published Error Strings Are Exact" is the single
convention both obey (a published string is the complete line including the
`Error: ` prefix and the sentinel), and
test_sentinel_vocabulary_matches_the_other_published_surface below reads the
sentinel vocabulary out of that very section and holds the registry surface to
it. Because both surfaces are then pinned to the same binary, they agree with
each other transitively.

EXEMPTIONS: STRINGS WHOSE TAIL IS NOT rmp's
--------------------------------------------
Two loci exist where an rmp error line ends and text from another component
begins, and neither tail may be asserted as a fixed literal:

  * the Go standard library's `net.OpError` rendering after
    `cannot bind <addr>: ` (internal/web), which is platform-dependent;
  * the Cypher engine's own diagnostic after `graph query failed: `
    (internal/commands/graph.go), which SPEC/COMMANDS.md explicitly declines
    to specify.

EXTERNAL_TAIL_MARKERS names both, with the reason recorded beside each. A
published string carrying a marker is compared by its fixed PREFIX through the
marker and its exit code, never by full equality, and every such string is
counted and named in the coverage report rather than silently skipped. No
example the contract publishes today carries such a tail, so the exemption
path is currently taken zero times -- which is exactly why
test_external_tail_comparator_holds_the_fixed_prefix drives BOTH loci for
real, through the same comparator the corpus uses, so the machinery is proven
live rather than left as untested code that a future example would be the
first to exercise.

NON-VACUITY
-----------
test_comparator_rejects_a_drifted_string takes a real published string, drops
its sentinel exactly as the eight defects had, and requires the comparator to
report a mismatch -- so a comparator that had degenerated into "always true"
fails this module instead of certifying the contract. The coverage report
additionally enforces a floor on the number of strings actually driven, so an
extraction that silently found nothing cannot pass.
"""

import json
import os
import re
import shlex
import socket
import subprocess
import sys
import uuid

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, REPO_ROOT


SPEC_PATH = REPO_ROOT / "SPEC" / "COMMANDS.md"

# Every invocation this module runs is expected to FAIL within milliseconds.
# A generous ceiling still turns a hypothetical future example that blocks
# (a `rmp web` variant that reaches the listener, say) into a named failure
# rather than a suite that hangs forever.
RUN_TIMEOUT_SECONDS = 30

# The roadmap name the contract's own examples use as a stand-in for "a
# roadmap that exists". Substituted for this module's throwaway roadmap.
CONTRACT_ROADMAP_TOKEN = "myproject"

# Two further roadmap names the contract uses LITERALLY, because the scenario
# is about the name itself. They are not this module's invention and are not
# substituted: `existing` must exist for the duplicate-name example to fail as
# published, and `missing` must never be created.
CONTRACT_EXISTING_ROADMAP = "existing"
CONTRACT_MISSING_ROADMAP = "missing"


# ---------------------------------------------------------------------------
# Corpus: every published example that names a stderr line
# ---------------------------------------------------------------------------


class Case:
    """One published failing example: where it lives in the contract, the
    invocation it publishes, and the stderr line and exit code it promises."""

    def __init__(self, route, title, cmd, stderr, exit_code, stdout):
        self.route = route
        self.title = title
        self.cmd = cmd
        self.stderr = stderr
        self.exit_code = exit_code
        self.stdout = stdout

    @property
    def key(self):
        return (self.route, self.title)

    def __repr__(self):
        return f"<{self.route} / {self.title}>"


def load_contract(cli_path, home_dir):
    """Return the parsed `rmp --ai-help` document produced by the binary under
    test. Run with this module's own HOME so the contract can never depend on
    the invoking user's roadmaps."""
    env = os.environ.copy()
    env["HOME"] = str(home_dir)
    result = subprocess.run(
        [cli_path, "--ai-help"],
        capture_output=True,
        text=True,
        env=env,
        timeout=RUN_TIMEOUT_SECONDS,
    )
    assert result.returncode == 0, (
        f"rmp --ai-help exited {result.returncode}; stderr={result.stderr!r}"
    )
    return json.loads(result.stdout)


def extract_corpus(doc):
    """Return every published example that names a stderr line, from both loci
    the contract uses: a command's own `examples` (the commands that take no
    subcommand -- `web`, `ai-help`, `stats`) and each subcommand's."""
    cases = []

    def collect(route, examples):
        for ex in examples or []:
            if ex.get("stderr"):
                cases.append(
                    Case(
                        route=route,
                        title=ex.get("title", ""),
                        cmd=ex.get("cmd", ""),
                        stderr=ex["stderr"],
                        exit_code=ex.get("exit"),
                        stdout=ex.get("stdout", ""),
                    )
                )

    for command in doc.get("commands") or []:
        collect(command["name"], command.get("examples"))
        for sub in command.get("subcommands") or []:
            collect(f"{command['name']} {sub['name']}", sub.get("examples"))
    return cases


# ---------------------------------------------------------------------------
# Shell constructs the published `cmd` may use
# ---------------------------------------------------------------------------

# The contract writes its examples the way a reader would type them, so a `cmd`
# is not always a bare argv. Exactly one construct is recognised, and it is
# translated rather than executed: a trailing `< /dev/null`, which the contract
# uses to say "stdin is closed" on the comment subcommands that would otherwise
# read a body from a pipe. Everything else -- a pipe, an append, a background
# `&`, a command substitution -- is REFUSED loudly by to_argv() below instead
# of being guessed at, because guessing would either run something other than
# what the contract published or silently drop the case.
_STDIN_FROM_DEVNULL = "< /dev/null"
_SHELL_METACHARACTERS = ("|", ">", "<", "&", ";", "$(", "`", "\n")


def to_argv(cmd, roadmap):
    """Translate a published `cmd` string into (argv, stdin_text).

    `argv` excludes the leading `rmp` (the binary path is supplied by the test
    harness, never by the contract) and carries this module's throwaway
    roadmap name wherever the contract wrote its `myproject` stand-in.
    """
    stdin_text = ""
    text = cmd.strip()
    if text.endswith(_STDIN_FROM_DEVNULL):
        text = text[: -len(_STDIN_FROM_DEVNULL)].strip()
        stdin_text = ""

    found = [m for m in _SHELL_METACHARACTERS if m in text]
    assert not found, (
        f"published example uses an unrecognised shell construct {found} that "
        f"this module refuses to guess at: {cmd!r}. Teach to_argv() the "
        f"construct explicitly, or exempt the example by name -- never let it "
        f"run as something the contract did not publish."
    )

    tokens = shlex.split(text)
    assert tokens and tokens[0] == "rmp", (
        f"published example does not invoke rmp: {cmd!r}"
    )
    argv = [roadmap if t == CONTRACT_ROADMAP_TOKEN else t for t in tokens[1:]]
    assert CONTRACT_ROADMAP_TOKEN not in argv, (
        f"the contract's roadmap stand-in survived substitution in {cmd!r}"
    )
    return argv, stdin_text


# ---------------------------------------------------------------------------
# Exemptions: the two loci where an rmp line ends and another component's text
# begins. Each is named with its reason; a string carrying a marker is held to
# its fixed prefix through the marker, never to its tail.
# ---------------------------------------------------------------------------

EXTERNAL_TAIL_MARKERS = [
    (
        "graph query failed: ",
        "internal/commands/graph.go wraps the Cypher engine's own parse or "
        "execution diagnostic. SPEC/COMMANDS.md declines to specify it "
        '("what follows is the engine\'s own text and is not specified '
        'here"), it tracks the engine version rather than rmp, and it names '
        "grammar tokens no rmp source file owns. Only the fixed prefix "
        "through this marker is rmp's to promise.",
    ),
    (
        "cannot bind ",
        "internal/web renders the Go standard library's net.OpError after "
        "the address it failed to bind. The tail is the operating system's "
        "own text, reaching Go through syscall.Errno, and differs across "
        "platforms (Linux says 'address already in use', other systems word "
        "it differently), so only the prefix through the address is rmp's to "
        "promise.",
    ),
]


def classify(published):
    """Return (mode, prefix) for a published string: ("exact", None) when the
    whole line is rmp's to promise, or ("prefix", <fixed head>) when the line
    runs into text produced outside rmp."""
    for marker, _reason in EXTERNAL_TAIL_MARKERS:
        index = published.find(marker)
        if index != -1:
            return "prefix", published[: index + len(marker)]
    return "exact", None


def compare(published, actual):
    """The single comparator this module uses everywhere: full string equality
    against the whole published line, EXCEPT for a line whose tail is produced
    outside rmp, which is held to its fixed prefix (see EXTERNAL_TAIL_MARKERS).
    Returns (ok, mode, expectation_description)."""
    mode, prefix = classify(published)
    if mode == "prefix":
        return actual.startswith(prefix), mode, f"starts with {prefix!r}"
    return actual == published, mode, f"equals {published!r}"


# ---------------------------------------------------------------------------
# The fixture the contract's examples presuppose
# ---------------------------------------------------------------------------

# Ids the published examples name in a roadmap that is supposed to exist. They
# are read out of the contract's own `cmd` strings, so the fixture is built to
# satisfy the contract rather than the contract bent to fit the fixture.
FIXTURE_TASK_COUNT = 42       # `task comment-add ... 42` is the highest id named
FIXTURE_SPRINT_COUNT = 5      # `sprint tasks ... 5` is the highest id named
FIXTURE_TASK_COMMENTS = 12    # `task comment-edit ... 12` names comment id 12
FIXTURE_SPRINT_COMMENTS = 4   # `sprint comment-edit ... 4` names comment id 4
SPRINT_MEMBER_TASKS = "1,3,7,42"
COMMENTED_TASK_ID = "42"
COMMENTED_SPRINT_ID = "3"
# A real short hash shape, so the transition that needs one is refused for the
# reason the example names rather than for a malformed hash.
FIXTURE_COMMIT_OPEN = "4f9c1ab"


class ContractFixture:
    """One throwaway roadmap satisfying every precondition the published
    examples presuppose, built ONCE for the whole module.

    Sharing is safe because every case this module drives is a FAILING
    invocation: none of the sixty-eight exits 0, so none writes. That is not
    assumed -- test_failing_examples_do_not_mutate_the_fixture below reads the
    fixture back after the whole sweep and requires it unchanged.

    The states are the ones the examples name:
      * tasks 1, 3, 7 and 42 belong to a sprint, so `task remove 3` is refused
        for being in SPRINT and `task stat 1 SPRINT` is refused as a manual
        transition;
      * task 7 is in TESTING, the state COMPLETED follows, so
        `task stat 7 COMPLETED` is refused for the missing --commit-close
        rather than for an invalid transition;
      * no sprint is ever started, so `task next` finds none open;
      * task 42 carries 12 comments and sprint 3 carries 4, so the comment ids
        the examples edit exist.
    """

    FR = "Reject webhook deliveries whose HMAC signature does not match the shared secret"
    TR = "Verify the X-Signature header against a computed HMAC-SHA256 before parsing the body"
    AC = "An invalid signature returns HTTP 401 and the payload is never processed"

    def __init__(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = f"payments-platform-{uuid.uuid4().hex[:10]}"

    # -- process plumbing -------------------------------------------------

    def run(self, args, stdin_text=""):
        """Run the binary with HOME pinned to this fixture's temporary home and
        stdin ALWAYS under this module's control -- never the inherited stdin
        of the process running the suite, which would make a result depend on
        how the suite was invoked."""
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        result = subprocess.run(
            [self.test.cli_path] + args,
            input=stdin_text,
            capture_output=True,
            text=True,
            env=env,
            timeout=RUN_TIMEOUT_SECONDS,
        )
        return result.returncode, result.stdout, result.stderr

    def run_ok(self, args):
        rc, out, err = self.run(args)
        assert rc == 0, f"fixture setup failed: rmp {' '.join(args)} -> {rc}: {err!r}"
        return out

    # -- construction -----------------------------------------------------

    def build(self):
        self.run_ok(["roadmap", "create", CONTRACT_EXISTING_ROADMAP])
        self.run_ok(["roadmap", "create", self.roadmap])

        titles = [
            "Harden the payment webhook signature check",
            "Move settlement reconciliation onto the ledger service",
            "Retire the legacy card tokenisation endpoint",
            "Add idempotency keys to the refund API",
            "Backfill merchant category codes for 2024 settlements",
        ]
        for i in range(1, FIXTURE_TASK_COUNT + 1):
            self.run_ok([
                "task", "create", "-r", self.roadmap,
                "-t", f"{titles[(i - 1) % len(titles)]} (part {i})",
                "-fr", self.FR, "-tr", self.TR, "-ac", self.AC,
            ])

        for i in range(1, FIXTURE_SPRINT_COUNT + 1):
            self.run_ok([
                "sprint", "create", "-r", self.roadmap,
                "-t", f"Payment integrity hardening, phase {i}",
                "-d", "Deliver session-based authentication for every write command.",
            ])

        # Sprint membership. The sprint is never STARTED: `task next` must
        # find no open sprint, and membership does not require one.
        self.run_ok(["sprint", "add-tasks", "-r", self.roadmap, "1", SPRINT_MEMBER_TASKS])

        # Walk task 7 to TESTING, the state from which COMPLETED is the next
        # legal transition.
        self.run_ok([
            "task", "stat", "-r", self.roadmap, "7", "DOING",
            "--commit-open", FIXTURE_COMMIT_OPEN,
        ])
        self.run_ok(["task", "stat", "-r", self.roadmap, "7", "TESTING"])

        for i in range(1, FIXTURE_TASK_COMMENTS + 1):
            self.run_ok([
                "task", "comment-add", "-r", self.roadmap, COMMENTED_TASK_ID,
                "--type", "PROGRESS",
                "--body", f"Signature verification checkpoint {i}: replayed the "
                          f"captured webhook batch and compared digests.",
            ])
        for i in range(1, FIXTURE_SPRINT_COMMENTS + 1):
            self.run_ok([
                "sprint", "comment-add", "-r", self.roadmap, COMMENTED_SPRINT_ID,
                "--type", "PROGRESS",
                "--body", f"Sprint checkpoint {i}: reconciliation work is ahead "
                          f"of the tokenisation retirement.",
            ])

        # `missing` must not exist, ever: two examples depend on its absence.
        rc, _out, _err = self.run(["roadmap", "list"])
        assert rc == 0
        assert CONTRACT_MISSING_ROADMAP not in self.roadmap_names(), (
            f"the roadmap {CONTRACT_MISSING_ROADMAP!r} exists, but two published "
            f"examples depend on its absence"
        )
        return self

    # -- observation ------------------------------------------------------

    def roadmap_names(self):
        rc, out, err = self.run(["roadmap", "list"])
        assert rc == 0, err
        return sorted(entry["name"] for entry in json.loads(out or "[]"))

    def snapshot(self):
        """A readback of everything the sweep could plausibly disturb: every
        task's id/status/priority/severity, every sprint's id/status, and the
        comment counts on the two commented parents."""
        rc, tasks, err = self.run(["task", "list", "-r", self.roadmap, "-l", "100"])
        assert rc == 0, err
        rc, sprints, err = self.run(["sprint", "list", "-r", self.roadmap])
        assert rc == 0, err
        rc, task_comments, err = self.run(
            ["task", "comment-list", "-r", self.roadmap, COMMENTED_TASK_ID])
        assert rc == 0, err
        rc, sprint_comments, err = self.run(
            ["sprint", "comment-list", "-r", self.roadmap, COMMENTED_SPRINT_ID])
        assert rc == 0, err
        return {
            "roadmaps": self.roadmap_names(),
            "tasks": [
                (t["id"], t["status"], t["priority"], t["severity"])
                for t in sorted(json.loads(tasks or "[]"), key=lambda t: t["id"])
            ],
            "sprints": [
                (s["id"], s["status"])
                for s in sorted(json.loads(sprints or "[]"), key=lambda s: s["id"])
            ],
            "task_comments": len(json.loads(task_comments or "[]")),
            "sprint_comments": len(json.loads(sprint_comments or "[]")),
        }

    def teardown(self):
        self.test.teardown()


_FIXTURE = None


def shared_fixture():
    global _FIXTURE
    if _FIXTURE is None:
        _FIXTURE = ContractFixture().build()
    return _FIXTURE


def teardown_shared_fixture():
    global _FIXTURE
    if _FIXTURE is not None:
        _FIXTURE.teardown()
        _FIXTURE = None


# ---------------------------------------------------------------------------
# Sentinel vocabulary, read out of the OTHER published surface
# ---------------------------------------------------------------------------

# SPEC/COMMANDS.md § "Published Error Strings Are Exact" states the convention
# both published surfaces obey and enumerates the sentinel vocabulary inline.
# Reading the list from that sentence -- rather than restating it here -- is
# what makes this a CROSS-SURFACE check: the vocabulary is the markdown
# surface's own words, applied to the registry surface.
_SENTINEL_RULE_ANCHOR = "**The sentinel text is part of the string.**"
_SENTINEL_LIST_END = "The sentinel names the failure class"


def published_sentinels():
    text = SPEC_PATH.read_text(encoding="utf-8")
    line = next((l for l in text.split("\n") if _SENTINEL_RULE_ANCHOR in l), None)
    assert line is not None, (
        f"SPEC/COMMANDS.md no longer contains the sentinel rule anchored by "
        f"{_SENTINEL_RULE_ANCHOR!r}; re-anchor this check rather than dropping it."
    )
    assert _SENTINEL_LIST_END in line, (
        f"the sentinel rule sentence no longer contains {_SENTINEL_LIST_END!r}, "
        f"so the enumerated vocabulary can no longer be delimited."
    )
    head = line[line.index(_SENTINEL_RULE_ANCHOR): line.index(_SENTINEL_LIST_END)]
    sentinels = [s.strip() for s in re.findall(r"`([^`]*)`", head)]
    assert len(sentinels) >= 8, f"only {len(sentinels)} sentinels extracted: {sentinels}"
    return sentinels


# The convention (COMMANDS.md § Published Error Strings Are Exact, point 2)
# ends: "A message that carries no sentinel is published without one, because
# that is what the user sees." These are the published examples for which the
# binary genuinely prints no sentinel, each named with the reason, so that a
# NEWLY sentinel-free string is a failure here rather than a silent pass.
SENTINEL_FREE_EXAMPLES = {
    "Error: --commit-open is required when transitioning to DOING":
        "internal/commands/task_mutate.go refuses the transition before any "
        "sentinel-wrapped validation runs; the binary prints no sentinel and "
        "SPEC/COMMANDS.md publishes the line without one for the same reason.",
    "Error: --commit-close is required when transitioning to COMPLETED":
        "The --commit-open case's counterpart on the COMPLETED transition, "
        "sentinel-free in the binary and in SPEC/COMMANDS.md alike.",
    'Error: invalid commit hash for --commit-open: "zzzzzzz" (expected 7 to 64 hexadecimal characters)':
        "The commit-hash shape check reports its own message with no sentinel; "
        "SPEC/COMMANDS.md publishes it without one.",
    "Error: ai-help accepts no positional arguments or flags other than --help":
        "cmd/rmp rejects a malformed --ai-help invocation before the command "
        "table is entered, so no sentinel error value is involved.",
}


# ---------------------------------------------------------------------------
# Accounting, shared across test methods
# ---------------------------------------------------------------------------

DRIVEN = {}   # key -> mode ("exact" | "prefix")


class TestAIContractErrorParity:
    """Replays every failing example `rmp --ai-help` publishes against the
    compiled binary and compares the captured stderr line and exit code with
    what the contract promises."""

    def setup_method(self):
        self.fx = shared_fixture()
        self.corpus = extract_corpus(load_contract(self.fx.test.cli_path, self.fx.test.home_dir))

    def teardown_method(self):
        # The fixture is module-scoped on purpose (see ContractFixture): it is
        # torn down once, by _run_all, after every method has run.
        pass

    # ------------------------------------------------------------------
    # Structural properties of the published surface
    # ------------------------------------------------------------------

    def test_corpus_is_well_formed(self):
        """The contract publishes a substantial, well-formed set of failing
        examples: every one names a complete `Error: ` line, a non-zero exit,
        an invocation, and an empty stdout."""
        assert self.corpus, "rmp --ai-help published no failing example at all"

        # A floor below today's 68 but far above zero, so an extraction that
        # silently found nothing -- or a registry gutted by accident -- fails
        # here instead of passing vacuously.
        floor = 60
        assert len(self.corpus) >= floor, (
            f"only {len(self.corpus)} published failing examples found, below "
            f"the floor of {floor}; extraction or the registries regressed"
        )

        seen = {}
        for case in self.corpus:
            assert case.stderr.startswith("Error: "), (
                f"{case}: a published string must be the COMPLETE line, "
                f"`Error: ` prefix included (SPEC/COMMANDS.md § Published Error "
                f"Strings Are Exact, point 1); got {case.stderr!r}"
            )
            assert case.exit_code, (
                f"{case}: publishes a stderr line with exit code "
                f"{case.exit_code!r}; a failing example must name a non-zero code"
            )
            assert case.stdout == "", (
                f"{case}: a failing invocation writes zero bytes to stdout "
                f"(SPEC/COMMANDS.md § Failing Invocations Write Nothing to "
                f"Stdout), but the example publishes {case.stdout!r}"
            )
            assert case.cmd.strip(), f"{case}: publishes no invocation to reproduce it"
            duplicate = seen.get(case.key)
            assert duplicate is None, (
                f"{case}: two examples share the same route and title, so a "
                f"failure could not be attributed to one of them"
            )
            seen[case.key] = case
        print(f"  published failing examples: {len(self.corpus)}")

    def test_sentinel_vocabulary_matches_the_other_published_surface(self):
        """CROSS-SURFACE: every sentinel the registry surface uses is one
        SPEC/COMMANDS.md's convention section enumerates, and every string
        that uses none is named with its reason."""
        sentinels = published_sentinels()
        print(f"  sentinel vocabulary from SPEC/COMMANDS.md: {sentinels}")

        unexplained = []
        for case in self.corpus:
            if any(s in case.stderr for s in sentinels):
                continue
            if case.stderr in SENTINEL_FREE_EXAMPLES:
                continue
            unexplained.append(case)

        assert not unexplained, (
            "published example(s) carry no sentinel from SPEC/COMMANDS.md's "
            "vocabulary and are not named in SENTINEL_FREE_EXAMPLES. Either "
            "the string dropped the sentinel the binary prints (the rmp task "
            "#317 defect), or it is genuinely sentinel-free and must be named "
            "with its reason:\n" +
            "\n".join(f"  - {c}: {c.stderr!r}" for c in unexplained)
        )

        stale = sorted(
            text for text in SENTINEL_FREE_EXAMPLES
            if not any(c.stderr == text for c in self.corpus)
        )
        assert not stale, (
            f"SENTINEL_FREE_EXAMPLES names string(s) the contract no longer "
            f"publishes: {stale}"
        )
        print(f"  sentinel-free by design (named, reasoned): {len(SENTINEL_FREE_EXAMPLES)}")

    # ------------------------------------------------------------------
    # The gate itself
    # ------------------------------------------------------------------

    def test_every_published_stderr_matches_the_binary(self):
        """Replay every published failing example and require the captured
        first line of stderr and the captured exit code to be exactly what the
        contract promises."""
        failures = []
        for case in self.corpus:
            argv, stdin_text = to_argv(case.cmd, self.fx.roadmap)
            rc, out, err = self.fx.run(argv, stdin_text)
            actual = err.splitlines()[0] if err else ""
            ok, mode, expectation = compare(case.stderr, actual)

            if not ok:
                failures.append(
                    f"  {case} -- published string does not match the binary\n"
                    f"    invocation: rmp {' '.join(argv)}\n"
                    f"    published:  {case.stderr!r}\n"
                    f"    binary:     {actual!r}\n"
                    f"    expected the captured line to {expectation}"
                )
            elif rc != case.exit_code:
                failures.append(
                    f"  {case} -- published exit code does not match the binary\n"
                    f"    invocation: rmp {' '.join(argv)}\n"
                    f"    published:  {case.exit_code}\n"
                    f"    binary:     {rc}\n"
                    f"    stderr:     {err!r}"
                )
            elif out != "":
                failures.append(
                    f"  {case} -- a failing invocation wrote to stdout: {out!r}"
                )
            else:
                DRIVEN[case.key] = mode

        assert not failures, (
            f"{len(failures)} of {len(self.corpus)} published example(s) name a "
            f"line or an exit code the binary does not produce.\n"
            f"The binary is the observed truth: correct the published string in "
            f"internal/commands/registry_*.go, never the runtime message.\n"
            + "\n".join(failures)
        )
        print(f"  replayed against the binary and matched: {len(DRIVEN)}")

    # ------------------------------------------------------------------
    # The exemption machinery, driven for real
    # ------------------------------------------------------------------

    def test_external_tail_comparator_holds_the_fixed_prefix(self):
        """Both loci where an rmp line runs into text produced outside rmp are
        driven for real, through the SAME comparator the corpus uses, so the
        exemption path is proven live even while no published example takes it.

        Each is checked in both directions: the fixed prefix holds, and full
        equality against the prefix alone does NOT -- which is what makes the
        prefix comparison necessary rather than merely lenient."""

        # Locus 1: the Cypher engine's own parse diagnostic.
        rc, out, err = self.fx.run(
            ["graph", "query", "-r", self.fx.roadmap, "--query", "MATCH ("])
        actual = err.splitlines()[0] if err else ""
        published = "Error: database error: graph query failed: <engine diagnostic>"
        mode, prefix = classify(published)
        assert mode == "prefix", f"the engine-diagnostic marker no longer classifies: {published!r}"
        ok, _mode, expectation = compare(published, actual)
        assert ok, (
            f"the Cypher engine diagnostic no longer starts with the fixed "
            f"prefix rmp owns.\n  captured: {actual!r}\n  expected to {expectation}"
        )
        assert actual != prefix, (
            f"the engine appended nothing after {prefix!r}, so this check "
            f"proves nothing about a tail; captured {actual!r}"
        )
        assert rc != 0 and out == "", f"rc={rc} stdout={out!r}"
        print(f"  external tail (Cypher engine) held to prefix: {prefix!r}")

        # Locus 2: the operating system's bind failure. The port is occupied by
        # this module itself, so the failure is deterministic and hermetic
        # rather than dependent on what else happens to be listening.
        holder = socket.socket()
        holder.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        holder.bind(("127.0.0.1", 0))
        holder.listen(1)
        port = holder.getsockname()[1]
        try:
            rc, out, err = self.fx.run(["web", "--no-open", "--port", str(port)])
        finally:
            holder.close()
        actual = err.splitlines()[0] if err else ""
        published = f"Error: database error: cannot bind 127.0.0.1:{port}: <detail>"
        mode, prefix = classify(published)
        assert mode == "prefix", f"the bind marker no longer classifies: {published!r}"
        ok, _mode, expectation = compare(published, actual)
        assert ok, (
            f"the bind failure no longer starts with the fixed prefix rmp owns.\n"
            f"  captured: {actual!r}\n  expected to {expectation}"
        )
        assert actual != prefix, (
            f"the OS appended nothing after {prefix!r}, so this check proves "
            f"nothing about a tail; captured {actual!r}"
        )
        assert rc != 0 and out == "", f"rc={rc} stdout={out!r}"
        print(f"  external tail (OS bind error) held to prefix: {prefix!r}")

    # ------------------------------------------------------------------
    # Non-vacuity
    # ------------------------------------------------------------------

    def test_comparator_rejects_a_drifted_string(self):
        """The comparator must FAIL on exactly the drift rmp task #317 fixed.

        A real published string is degraded the way the eight defects had
        degraded -- the sentinel dropped, the enum value unquoted, a stale
        duplicated tail restored -- and the comparator is required to reject
        each one. Without this, a comparator that had degenerated into "always
        true" would certify the whole contract silently.
        """
        sample = next(
            (c for c in self.corpus
             if c.stderr.startswith("Error: validation error: ")),
            None,
        )
        assert sample is not None, (
            "no published example carries a `validation error: ` sentinel, so "
            "this proof has nothing to degrade"
        )
        truth = sample.stderr

        drifted = [
            ("sentinel dropped", truth.replace("Error: validation error: ", "Error: ", 1)),
            ("stale duplicated tail", truth + ": invalid task status"),
            ("prefix dropped", truth[len("Error: "):]),
        ]
        for label, mutant in drifted:
            assert mutant != truth, f"{label}: the mutation changed nothing"
            ok, _mode, _exp = compare(mutant, truth)
            assert not ok, (
                f"the comparator accepted a {label} mutation of a published "
                f"string, so it cannot detect the rmp task #317 defect.\n"
                f"  published (mutated): {mutant!r}\n  binary: {truth!r}"
            )

        # And the same comparator still accepts the truth, so it is not simply
        # rejecting everything.
        ok, mode, _exp = compare(truth, truth)
        assert ok and mode == "exact", (
            f"the comparator rejected an unmutated published string {truth!r}"
        )
        print(f"  comparator rejected {len(drifted)} drifted forms of {truth!r}")

    # ------------------------------------------------------------------
    # Shared-fixture safety
    # ------------------------------------------------------------------

    def test_failing_examples_do_not_mutate_the_fixture(self):
        """Every case this module drives is a failing invocation, so none may
        write. Replaying the whole corpus between two readbacks proves it --
        and with it, that sharing one fixture across the sweep is sound."""
        before = self.fx.snapshot()
        for case in self.corpus:
            argv, stdin_text = to_argv(case.cmd, self.fx.roadmap)
            self.fx.run(argv, stdin_text)
        after = self.fx.snapshot()
        assert before == after, (
            "replaying the published failing examples changed the fixture, so "
            "at least one of them is not the pure failure the contract "
            "publishes:\n"
            + "\n".join(
                f"  {field}: before={before[field]!r} after={after[field]!r}"
                for field in sorted(before)
                if before[field] != after[field]
            )
        )
        print(f"  fixture unchanged across {len(self.corpus)} failing invocations")

    # ------------------------------------------------------------------
    # Accounting. Named to sort last among this class's test_* methods, so it
    # runs after every other method has had the chance to mark its cases
    # driven (see _run_all, which iterates sorted(dir(cls))).
    # ------------------------------------------------------------------

    def test_zz_coverage_report(self):
        keys = {case.key for case in self.corpus}
        missing = sorted(keys - set(DRIVEN))
        by_mode = {}
        for mode in DRIVEN.values():
            by_mode[mode] = by_mode.get(mode, 0) + 1

        print(f"\nAI contract published failing examples: {len(keys)}")
        print(f"  replayed and matched exactly:                  {by_mode.get('exact', 0)}")
        print(f"  replayed and held to a fixed prefix (external tail): {by_mode.get('prefix', 0)}")
        for marker, reason in EXTERNAL_TAIL_MARKERS:
            print(f"  EXTERNAL TAIL MARKER: {marker!r}\n    reason: {reason}")

        assert not missing, (
            f"{len(missing)} published example(s) were never driven against the "
            f"binary -- a gap in this module's own coverage, not a silent pass:\n"
            + "\n".join(f"  - {route} / {title}" for route, title in missing)
        )

        floor = 60
        assert len(DRIVEN) >= floor, (
            f"only {len(DRIVEN)} published example(s) were driven, below the "
            f"floor of {floor}; extraction or the sweep regressed"
        )
        print(f"  floor: {floor} (met: {len(DRIVEN) >= floor})")


def _run_all():
    cls = TestAIContractErrorParity
    methods = sorted(m for m in dir(cls) if m.startswith("test_"))
    passed = 0
    failed = 0
    failures = []
    try:
        for name in methods:
            instance = cls()
            instance.setup_method()
            try:
                getattr(instance, name)()
                passed += 1
                print(f"PASS {name}")
            except AssertionError as exc:
                failed += 1
                failures.append((name, exc))
                print(f"FAIL {name}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((name, exc))
                print(f"FAIL {name} (error: {type(exc).__name__}: {exc})")
            finally:
                instance.teardown_method()
    finally:
        teardown_shared_fixture()

    print("\n" + "=" * 60)
    print(f"AI contract error parity tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\nFAIL {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
