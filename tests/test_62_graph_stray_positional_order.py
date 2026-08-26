#!/usr/bin/env python3
"""
Test 62: where the `graph` family's stray-positional refusal lands in the
subcommand's order, measured against the compiled ./bin/rmp (rmp task #291).

SPEC/GRAPH.md § No Positional Query: A Stray Token Is Refused is canonical. Rule
5 of that section places the refusal precisely, and acceptance criterion 59
requires the placement to be MEASURED rather than reasoned about:

    roadmap selection
      -> THE REFUSAL
        -> the graph store is opened
        -> standard input is read
        -> the maximum-length check
        -> the guard rail and the two content rules

Each neighbour in that order carries a DIFFERENT exit code, which is what makes
the placement observable from outside the process at all:

    exit 3   no roadmap named and none selected      (precedes the refusal)
    exit 2   the refusal itself
    exit 4   the roadmap named does not exist        (follows it)
    exit 6   the query is of the wrong class         (follows it)

Every case below therefore drives an invocation that carries BOTH the stray
token and a second fault, and asserts which of the two verdicts comes out. A
control invocation proves the second fault really does produce its own verdict
on its own, so each assertion distinguishes two live outcomes rather than one.

Standard input is the fourth neighbour and needs a different instrument: there
is no exit code for "the stream was read", so the case drives a producer that
keeps writing and measures how much the command consumed before exiting. The
SPEC notes why the neighbouring maximum-length check is exercised in that form
too: a `--query` value above 1 MiB cannot reach the binary through a shell on
Linux, where MAX_ARG_STRLEN caps a single argument at 128 KiB, so the
stray-beats-the-limit ordering is written as a standard-input case.

What every case asserts, beyond the exit code:

  * stdout is empty. A refused invocation writes zero bytes to it.
  * stderr carries the error line and the AI-agent hint and NOTHING else. An
    excess positional argument is not a dispatch failure, so no help body
    follows it (SPEC/HELP.md § Recovery help after a dispatch failure).
  * the roadmap's `graph/` directory -- its snapshot directory and its
    write-ahead log -- is byte-identical before and after. This is the half that
    makes the module about the ordering rather than about the message: a refusal
    that had moved to AFTER the store open would still exit 2 and would still
    print the right line, and only the bytes on disk would say so.

The module also carries the end-to-end half of acceptance criteria 57, 58 and
60. The five subcommands' lines are compared against each other and against the
CANONICAL line plus this family's HINT, and the comment subcommands' line is
compared against that same canonical line without the hint. The two constants
below are written once and every expectation is derived from them, so no
expectation in this file is a second copy of the other family's wording.
"""

import hashlib
import inspect
import os
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXIT_OK = 0
EXIT_MISUSE = 2
EXIT_NO_ROADMAP = 3
EXIT_NOT_FOUND = 4
EXIT_VALIDATION = 6

# The refusal, in the two pieces SPEC/GRAPH.md acceptance criterion 60 names:
# the line SPEC/COMMANDS.md § Positional Arguments publishes for the WHOLE CLI,
# and the hint the `graph` family appends to it. Neither family's line is
# written out anywhere below; both are DERIVED from these two, so the
# relationship between them is what this module asserts and a change to either
# has to be made here, once, in the open.
CANONICAL_REFUSAL = 'Error: invalid input: unexpected argument "{token}"'
GRAPH_HINT = " (graph queries use --query or stdin)"

# The AI-agent hint that closes stderr on every failing invocation
# (SPEC/HELP.md § Stderr part order, part 4).
AI_HINT = "AI agents: run `rmp --ai-help` for a machine-readable command contract."

# The roadmap-selection refusal that PRECEDES the stray-token one
# (SPEC/COMMANDS.md; the angle brackets are literal characters the binary
# prints, not placeholders).
NO_ROADMAP_LINE = "Error: no roadmap selected: use -r <name> or --roadmap <name>"

# The offending token every case supplies. A plausible report name a caller
# might really append by mistake, carrying no character any parser treats
# specially.
STRAY = "reconciliation-report"

# How long the command may take to refuse a stray token while a producer is
# still writing to standard input. The contract is that it does not read the
# stream at all, so the honest budget is milliseconds; thirty seconds is chosen
# only so a loaded machine cannot produce a false failure.
NO_WAIT_BUDGET_SECONDS = 30.0

# The most a NON-READING command can absorb: whatever the operating system's
# pipe buffer holds before the writer blocks. A command that performed the
# bounded standard-input read would have consumed 1 MiB plus one byte, so
# anything at or below this ceiling proves the read never happened.
UNREAD_PIPE_CEILING_BYTES = 512 * 1024


def canonical_line(token):
    """The refusal SPEC/COMMANDS.md § Positional Arguments publishes CLI-wide."""
    return CANONICAL_REFUSAL.format(token=token)


def graph_line(token):
    """The `graph` family's line: the canonical line with this family's hint."""
    return canonical_line(token) + GRAPH_HINT


def comment_line(token):
    """The comment subcommands' line: the canonical line, without a hint.

    A comment body comes from `--body` or from standard input and never from
    `--query`, so the `graph` family's hint would be false here
    (SPEC/COMMANDS.md § Comment Positional Argument Contract, "The other family
    that publishes this refusal").
    """
    return canonical_line(token)


# The five subcommands, each with a query of its own operation class, so every
# invocation driven below is one that would SUCCEED were the stray token
# removed. The queries act on different nodes of the same seeded graph, so the
# controls do not undo one another.
GRAPH_SUBCOMMANDS = [
    ("create", "CREATE (:Spec {key:'chargeback-handling'})"),
    ("query", "MATCH (s:Spec) RETURN s.key ORDER BY s.key"),
    ("update", "MATCH (s:Spec {key:'payment-capture'}) SET s.status = 'ready'"),
    ("delete", "MATCH (s:Spec {key:'refund-flow'}) DETACH DELETE s"),
    ("search", "MATCH p=(a:Spec)-[*1..3]-(b:Spec) RETURN p"),
]

# A query of the WRONG class for `graph query`: it writes, and a read
# subcommand refuses it with exit 6. Used to prove the refusal precedes the
# guard rail.
WRONG_CLASS_QUERY = "CREATE (:Spec {key:'settlement-reconciliation'})"


def stderr_parts(stderr):
    """The non-blank lines of stderr, in order.

    SPEC/HELP.md § Stderr part order separates the parts with blank lines, so
    dropping the blanks leaves exactly the parts. A help body would show up here
    as dozens of extra lines, which is how "no help follows the refusal" is
    asserted without matching on any particular help text.
    """
    return [line for line in stderr.splitlines() if line.strip()]


class GraphStrayBase:
    """Fixture shared by every class in the module: one roadmap with a real
    graph, and the helpers that drive the binary and fingerprint the store."""

    ROADMAP = "settlement-platform"

    SEED_QUERIES = [
        "CREATE (:Spec {key:'payment-capture'})-[:DEPENDS_ON]->(:Spec {key:'ledger-posting'})",
        "CREATE (:Spec {key:'refund-flow'})",
    ]

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap(self.ROADMAP)
        for query in self.SEED_QUERIES:
            self.test.run_cmd(["graph", "create", "-r", self.roadmap, "--query", query])

    def teardown_method(self):
        self.test.teardown()

    # ---- driving the binary -------------------------------------------

    def env(self):
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        return env

    def run(self, args):
        """Run the binary with standard input closed.

        DEVNULL rather than an inherited stream: a case here must never be able
        to block on the terminal the suite happens to run under, and the one
        case that is ABOUT standard input opens its own pipe instead.
        """
        result = subprocess.run(
            [self.test.cli_path] + args,
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
            env=self.env(),
        )
        return result.returncode, result.stdout, result.stderr

    # ---- fingerprinting the store -------------------------------------

    def graph_dir(self):
        return self.test.home_dir / ".roadmaps" / self.roadmap / "graph"

    def graph_fingerprint(self):
        """A digest of every file under the roadmap's `graph/` directory.

        Content, not mtimes: the criterion says byte-identical, and a checkpoint
        that rewrote a snapshot with identical bytes would be a checkpoint that
        changed nothing. Relative paths are included so a file appearing or
        disappearing is caught as well as one changing.
        """
        root = self.graph_dir()
        prints = {}
        for dirpath, dirnames, filenames in os.walk(root):
            dirnames.sort()
            for name in sorted(filenames):
                path = os.path.join(dirpath, name)
                with open(path, "rb") as handle:
                    prints[os.path.relpath(path, root)] = hashlib.sha256(handle.read()).hexdigest()
        return prints

    def assert_store_untouched(self, before, label):
        after = self.graph_fingerprint()
        assert after == before, (
            f"{label}: the roadmap's graph/ directory changed across a refused invocation.\n"
            f"  before: {before}\n"
            f"  after:  {after}\n"
            f"A refused invocation opens no store: it creates, changes and deletes nothing, and "
            f"leaves the snapshot directory and the write-ahead log exactly as they were.")

    def assert_refused_cleanly(self, code, stdout, stderr, want_code, want_line, label):
        """The whole contract of a refused invocation, in one place."""
        assert code == want_code, (
            f"{label}: exit={code}, want {want_code}; stdout={stdout!r} stderr={stderr!r}")
        assert stdout == "", (
            f"{label}: a refused invocation writes zero bytes to stdout; got {stdout!r}")

        parts = stderr_parts(stderr)
        assert parts, f"{label}: stderr carried no error line at all"

        # The wording first, on its own, so a drifted line is reported as a
        # drifted line rather than as a stderr-shape failure.
        assert parts[0] == want_line, (
            f"{label}: stderr line = {parts[0]!r}\n"
            f"{' ' * len(label)}  want {want_line!r}")

        # Then the shape: the error line and the AI-agent hint and nothing
        # else. A help body would show up here as dozens of extra parts.
        assert parts == [want_line, AI_HINT], (
            f"{label}: stderr carries {parts!r}; SPEC/HELP.md § Stderr part order gives a refusal "
            f"the error line and the AI-agent hint and nothing else, and an excess positional "
            f"argument is not a dispatch failure, so no help body may follow it.\n"
            f"  want: {[want_line, AI_HINT]!r}")
        assert "Usage:" not in stderr, (
            f"{label}: a help body followed the refusal: {stderr!r}")


class TestGraphStrayRefusalOrder(GraphStrayBase):
    """Acceptance criterion 59: the refusal precedes every other check the
    subcommand performs, and roadmap selection precedes the refusal."""

    def test_the_named_roadmap_not_existing_alone_exits_four(self):
        """The control for the case below: without the stray token, a roadmap
        that does not exist really is an exit-4 verdict."""
        code, stdout, stderr = self.run(
            ["graph", "query", "-r", "roadmap-that-does-not-exist",
             "--query", "MATCH (s:Spec) RETURN s.key"])
        assert code == EXIT_NOT_FOUND, (
            f"a missing roadmap alone exits {code}, want {EXIT_NOT_FOUND}; the ordering case below "
            f"would otherwise be distinguishing one live outcome from a dead one; stderr={stderr!r}")
        assert stdout == ""

    def test_a_stray_beats_a_roadmap_that_does_not_exist(self):
        """The refusal precedes opening the graph store, so exit 2 and not 4."""
        before = self.graph_fingerprint()
        code, stdout, stderr = self.run(
            ["graph", "query", STRAY, "-r", "roadmap-that-does-not-exist",
             "--query", "MATCH (s:Spec) RETURN s.key"])

        self.assert_refused_cleanly(
            code, stdout, stderr, EXIT_MISUSE, graph_line(STRAY),
            "a stray token on a roadmap that does not exist")
        assert code != EXIT_NOT_FOUND, (
            "exit 4 means the store open ran first; the refusal must precede it")
        assert not (self.test.home_dir / ".roadmaps" / "roadmap-that-does-not-exist").exists(), (
            "a refused invocation created the roadmap directory it named")
        self.assert_store_untouched(before, "a stray token on a roadmap that does not exist")

    def test_roadmap_selection_precedes_the_refusal(self):
        """The one check that comes FIRST. With no `-r` and no roadmap selected
        the same invocation exits 3, not 2: the stray token is still there and
        is still refusable, and the roadmap verdict wins because it is reached
        earlier."""
        before = self.graph_fingerprint()
        roadmaps_before = sorted(os.listdir(self.test.home_dir / ".roadmaps"))

        code, stdout, stderr = self.run(
            ["graph", "query", STRAY, "--query", "MATCH (s:Spec) RETURN s.key"])

        self.assert_refused_cleanly(
            code, stdout, stderr, EXIT_NO_ROADMAP, NO_ROADMAP_LINE,
            "a stray token with no roadmap named and none selected")
        assert graph_line(STRAY) not in stderr, (
            f"the stray-token refusal was reported instead of the roadmap one: {stderr!r}; "
            f"roadmap selection runs first, so its verdict is the one the caller reads")
        assert sorted(os.listdir(self.test.home_dir / ".roadmaps")) == roadmaps_before, (
            "a refused invocation changed the set of roadmaps")
        self.assert_store_untouched(before, "a stray token with no roadmap selected")

    def test_a_query_of_the_wrong_class_alone_exits_six(self):
        """The control for the case below: without the stray token, a writing
        query under `graph query` really is an exit-6 verdict."""
        before = self.graph_fingerprint()
        code, stdout, stderr = self.run(
            ["graph", "query", "-r", self.roadmap, "--query", WRONG_CLASS_QUERY])
        assert code == EXIT_VALIDATION, (
            f"a wrong-class query alone exits {code}, want {EXIT_VALIDATION}; stderr={stderr!r}")
        assert stdout == ""
        self.assert_store_untouched(before, "a wrong-class query alone")

    def test_a_stray_beats_a_query_of_the_wrong_class(self):
        """The refusal precedes the guard rail, so exit 2 and not 6."""
        before = self.graph_fingerprint()
        code, stdout, stderr = self.run(
            ["graph", "query", "-r", self.roadmap, "--query", WRONG_CLASS_QUERY, STRAY])

        self.assert_refused_cleanly(
            code, stdout, stderr, EXIT_MISUSE, graph_line(STRAY),
            "a stray token beside a query of the wrong operation class")
        assert code != EXIT_VALIDATION, (
            "exit 6 means the guard rail classified the query first; the refusal must precede it")
        self.assert_store_untouched(before, "a stray token beside a wrong-class query")

    def test_a_stray_refuses_before_standard_input_is_read(self):
        """The refusal precedes the standard-input read, and precedes the
        maximum-length check that read feeds.

        There is no exit code for "the stream was read", so the instrument is
        the producer: it keeps writing until the pipe breaks and reports how
        much it managed to send. A command that had performed the bounded read
        would have consumed 1 MiB plus one byte before deciding anything; a
        command that never reads absorbs at most whatever the operating
        system's pipe buffer holds, and the writer then blocks until the
        process exits and the pipe breaks.

        The same case covers the ordering against the maximum-length check.
        The offered stream is far larger than the 1 MiB maximum, so a command
        that read it would exit 6; exit 2 says the stray token was refused
        first. This form is the one the SPEC prescribes for that ordering,
        because a `--query` value above 1 MiB cannot reach the binary through a
        shell on Linux at all: MAX_ARG_STRLEN caps a single argument at 128 KiB.
        """
        before = self.graph_fingerprint()

        started = time.time()
        proc = subprocess.Popen(
            [self.test.cli_path, "graph", "query", "-r", self.roadmap, STRAY],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=self.env(),
        )

        chunk = b"MATCH (s:Spec) RETURN s.key\n" * 2048
        offered = 8 * 1024 * 1024
        sent = 0
        try:
            while sent < offered:
                proc.stdin.write(chunk)
                proc.stdin.flush()
                sent += len(chunk)
        except (BrokenPipeError, OSError, ValueError):
            # BrokenPipeError once rmp has exited; ValueError from a flush on
            # the writer Python closed when the pipe broke. Both mean the
            # command stopped before the writer ran out of data.
            pass
        finally:
            try:
                proc.stdin.close()
            except (BrokenPipeError, OSError, ValueError):
                pass

        # communicate() would flush the writer Python already closed on the
        # broken pipe, so the streams are drained directly. Both are small --
        # an error line on stderr, nothing on stdout -- so reading them to end
        # of stream cannot deadlock.
        stdout = proc.stdout.read().decode()
        stderr = proc.stderr.read().decode()
        proc.wait(timeout=NO_WAIT_BUDGET_SECONDS)
        elapsed = time.time() - started

        self.assert_refused_cleanly(
            proc.returncode, stdout, stderr, EXIT_MISUSE, graph_line(STRAY),
            "a stray token with a producer still writing to standard input")
        assert proc.returncode != EXIT_VALIDATION, (
            "exit 6 means the stream was read and measured against the maximum; the refusal must "
            "precede both")
        assert sent <= UNREAD_PIPE_CEILING_BYTES, (
            f"the command absorbed {sent} bytes of the {offered} offered, which is more than an "
            f"unread pipe can hold: the stray token was refused only AFTER standard input was "
            f"read, so a subcommand given no --query would block on, and consume, a stream a "
            f"producer is still writing to")
        assert elapsed < NO_WAIT_BUDGET_SECONDS, (
            f"the refusal took {elapsed:.3f}s; it must not wait on standard input at all")
        self.assert_store_untouched(before, "a stray token with a producer writing to standard input")
        print(f"  the stray token was refused after {sent} bytes, in {elapsed:.3f}s")


class TestGraphStrayRefusalWording(GraphStrayBase):
    """Acceptance criteria 57 and 58 at binary level: all five subcommands, one
    wording, and the classification of a `-`-prefixed token in both directions."""

    def test_all_five_subcommands_would_succeed_without_the_stray_token(self):
        """The control half of criterion 57. Each query is of its subcommand's
        own operation class, so every refusal asserted below is caused by the
        stray token and not by a query the guard rail was going to reject."""
        for subcommand, query in GRAPH_SUBCOMMANDS:
            code, stdout, stderr = self.run(
                ["graph", subcommand, "-r", self.roadmap, "--query", query])
            assert code == EXIT_OK, (
                f"`graph {subcommand}` was refused its own class of query: exit={code} "
                f"stderr={stderr!r}")
            assert stdout.strip() != "", (
                f"`graph {subcommand}` wrote nothing to stdout, so the control did not run")

    def test_all_five_subcommands_refuse_with_one_wording(self):
        """Criterion 57. The whole line, the parenthetical included, identical
        across the five -- and the five compared against EACH OTHER, because a
        wording that drifts on one subcommand satisfies every assertion made
        about that subcommand alone."""
        before = self.graph_fingerprint()
        want = graph_line(STRAY)
        produced = {}

        for subcommand, query in GRAPH_SUBCOMMANDS:
            code, stdout, stderr = self.run(
                ["graph", subcommand, "-r", self.roadmap, "--query", query, STRAY])
            self.assert_refused_cleanly(
                code, stdout, stderr, EXIT_MISUSE, want, f"graph {subcommand}")
            produced[subcommand] = stderr_parts(stderr)[0]

        distinct = {}
        for subcommand, line in produced.items():
            distinct.setdefault(line, []).append(subcommand)
        assert len(distinct) == 1, (
            f"the family no longer has one wording; the five subcommands share one argument "
            f"parser, so a divergence here is a divergence in that parser: {distinct!r}")

        self.assert_store_untouched(before, "the five refusals")

    def test_hyphen_prefixed_tokens_are_classified_in_both_directions(self):
        """Criterion 58. On this family a `-` followed by a digit or a decimal
        point is a query value and not a flag, so a stray `-1` and a stray bare
        `-` are unexpected arguments, while a long flag the family does not
        define is an unknown flag.

        Both refusals carry exit code 2, so nothing but the wording tells them
        apart and only an assertion on the wording can hold the difference. The
        comment subcommands classify `-1` the other way, which
        test_the_comment_subcommands_classify_minus_one_as_a_flag pins.
        """
        unexpected_tokens = ["-1", "-0.5", "-"]
        flag_tokens = ["--include-archived", "-x"]

        for subcommand, query in GRAPH_SUBCOMMANDS:
            for token in unexpected_tokens:
                code, stdout, stderr = self.run(
                    ["graph", subcommand, "-r", self.roadmap, "--query", query, token])
                self.assert_refused_cleanly(
                    code, stdout, stderr, EXIT_MISUSE, graph_line(token),
                    f"graph {subcommand} with the stray token {token!r}")

            for token in flag_tokens:
                code, stdout, stderr = self.run(
                    ["graph", subcommand, "-r", self.roadmap, "--query", query, token])
                assert code == EXIT_MISUSE, (
                    f"graph {subcommand} {token}: exit={code}, want {EXIT_MISUSE}; stderr={stderr!r}")
                assert stdout == ""
                first = stderr_parts(stderr)[0]
                assert first == f"Error: invalid input: unknown flag: {token}", (
                    f"graph {subcommand}: a genuine flag must be reported as an unknown flag; "
                    f"got {first!r}")
                assert "unexpected argument" not in first, (
                    f"graph {subcommand}: a genuine flag must not be reported as a positional "
                    f"argument; got {first!r}")

    def test_only_the_first_stray_token_is_named(self):
        """Criterion 58's closing half. The tokens are examined left to right,
        the first positional argument ends the invocation, and the position of
        the stray on the command line does not change which one is named."""
        first, second = STRAY, "settlement-summary"
        want = graph_line(first)

        for subcommand, query in GRAPH_SUBCOMMANDS:
            layouts = [
                ("both strays after the flags",
                 ["graph", subcommand, "-r", self.roadmap, "--query", query, first, second]),
                ("the first stray written before the flags",
                 ["graph", subcommand, first, "-r", self.roadmap, "--query", query, second]),
            ]
            for label, args in layouts:
                code, stdout, stderr = self.run(args)
                self.assert_refused_cleanly(
                    code, stdout, stderr, EXIT_MISUSE, want, f"graph {subcommand}: {label}")
                assert second not in stderr, (
                    f"graph {subcommand} ({label}): stderr names the second stray token as well: "
                    f"{stderr!r}; only the first offending token may be named")


class TestGraphStrayRefusalAcrossFamilies(GraphStrayBase):
    """Acceptance criterion 60 at binary level: one rule, two families.

    The `graph` line and the comment line are both derived from CANONICAL_REFUSAL
    in this module, so what is asserted below is the RELATION between them --
    the `graph` line is the canonical CLI-wide line with this family's hint
    appended, and the comment line is that same line without a hint. Nothing
    here is a second literal copy of the other family's wording, which is what
    the criterion forbids: two copies drift one at a time and nothing objects.
    """

    def setup_method(self):
        super().setup_method()
        self.task_id = self.test.create_task(
            self.roadmap,
            "Reconcile settlement batches nightly",
            "Finance cannot close the day until every batch is reconciled",
            "Add a nightly job that walks the settlement ledger and reports gaps",
            "A deliberately unbalanced batch is reported within one run")

    def test_the_canonical_line_is_what_the_shared_enforcement_point_emits(self):
        """The line the other two are measured against. `roadmap list` belongs
        to neither family, so it reaches the shared enforcement point and emits
        the CLI-wide wording unchanged."""
        code, stdout, stderr = self.run(["roadmap", "list", STRAY])
        self.assert_refused_cleanly(
            code, stdout, stderr, EXIT_MISUSE, canonical_line(STRAY), "roadmap list")

    def test_the_graph_line_is_the_canonical_line_plus_this_familys_hint(self):
        """One direction of the relation, measured on all five subcommands."""
        for subcommand, query in GRAPH_SUBCOMMANDS:
            code, stdout, stderr = self.run(
                ["graph", subcommand, "-r", self.roadmap, "--query", query, STRAY])
            first = stderr_parts(stderr)[0]
            assert code == EXIT_MISUSE, f"graph {subcommand}: exit={code}; stderr={stderr!r}"
            assert first == canonical_line(STRAY) + GRAPH_HINT, (
                f"`graph {subcommand}` emits {first!r}; the canonical CLI-wide line plus this "
                f"family's hint is {canonical_line(STRAY) + GRAPH_HINT!r}. The shared half of the "
                f"two families' lines must stay shared character for character.")
            assert stdout == ""

    def test_the_comment_line_is_that_same_line_without_a_hint(self):
        """The reciprocal direction, so neither family can be edited alone.

        Without this half the module would pin the `graph` line and say nothing
        about the family that shares its first half, which is exactly how two
        copies come to be maintained separately.
        """
        subcommands = [
            (["task", "comment-add", "-r", self.roadmap, str(self.task_id), STRAY,
              "--type", "NOTE", "--body", "Recorded while the settlement ledger was reviewed."]),
            (["task", "comment-list", "-r", self.roadmap, str(self.task_id), STRAY]),
            (["sprint", "comment-list", "-r", self.roadmap, "1", STRAY]),
        ]
        for args in subcommands:
            code, stdout, stderr = self.run(args)
            first = stderr_parts(stderr)[0]
            assert code == EXIT_MISUSE, f"rmp {' '.join(args)}: exit={code}; stderr={stderr!r}"
            assert first == comment_line(STRAY), (
                f"rmp {' '.join(args)} emits {first!r}; the comment subcommands publish the "
                f"canonical CLI-wide line unchanged: {comment_line(STRAY)!r}")
            assert GRAPH_HINT not in first, (
                f"rmp {' '.join(args)} emits the `graph` family's hint {GRAPH_HINT!r}. The hint "
                f"names the two sources of a Cypher query; a comment body comes from --body or "
                f"standard input and never from --query, so the hint would be false here.")
            assert stdout == ""

    def test_the_comment_subcommands_classify_minus_one_as_a_flag(self):
        """The one point on which the two families deliberately disagree about
        the SAME token. On a comment subcommand `-1` is an unknown flag; on a
        `graph` subcommand it is an unexpected argument
        (test_hyphen_prefixed_tokens_are_classified_in_both_directions).

        Both refusals carry exit code 2, so this difference is invisible to any
        assertion that reads only the exit code, and an "alignment" of the two
        families would pass unnoticed without both halves asserted.
        """
        code, stdout, stderr = self.run(
            ["task", "comment-add", "-r", self.roadmap, str(self.task_id), "-1",
             "--type", "NOTE", "--body", "Recorded while the settlement ledger was reviewed."])
        first = stderr_parts(stderr)[0]
        assert code == EXIT_MISUSE, f"exit={code}, want {EXIT_MISUSE}; stderr={stderr!r}"
        assert first == "Error: invalid input: unknown flag: -1", (
            f"on the comment subcommands every '-'-prefixed token is a flag, digits included; "
            f"got {first!r}")
        assert "unexpected argument" not in first, (
            f"on the comment subcommands `-1` must NOT be reported as a positional argument; "
            f"got {first!r}")
        assert stdout == ""

        # And the same token on the `graph` family, so the divergence is read as
        # a divergence rather than as two unrelated facts.
        code, stdout, stderr = self.run(
            ["graph", "query", "-r", self.roadmap, "--query", "MATCH (s:Spec) RETURN s.key", "-1"])
        first = stderr_parts(stderr)[0]
        assert code == EXIT_MISUSE, f"exit={code}, want {EXIT_MISUSE}; stderr={stderr!r}"
        assert first == graph_line("-1"), (
            f"on the `graph` family `-1` is a negative numeric literal and therefore a stray "
            f"positional argument, not a flag; got {first!r}")


def _run_all():
    passed = 0
    failed = 0
    failures = []
    # Classes are DISCOVERED by inspecting this module, never listed, so a
    # suite added later cannot be silently skipped.
    classes = [
        obj for _name, obj in sorted(inspect.getmembers(sys.modules[__name__], inspect.isclass))
        if obj.__module__ == __name__ and _name.startswith("Test")
    ]
    print(f"Discovered {len(classes)} test classes: "
          f"{', '.join(cls.__name__ for cls in classes)}")
    for cls in classes:
        for m in sorted(name for name in dir(cls) if name.startswith("test_")):
            label = f"{cls.__name__}.{m}"
            instance = cls()
            instance.setup_method()
            try:
                getattr(instance, m)()
                passed += 1
                print(f"PASS {label}")
            except Exception as exc:  # noqa: BLE001 - the harness reports every failure
                failed += 1
                failures.append((label, exc))
                print(f"FAIL {label}: {exc}")
            finally:
                instance.teardown_method()

    print(f"\n{passed} passed, {failed} failed")
    if failures:
        for label, exc in failures:
            print(f"\n--- {label} ---\n{exc}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(_run_all())
