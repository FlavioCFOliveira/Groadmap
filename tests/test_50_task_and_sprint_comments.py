#!/usr/bin/env python3
"""
Test 50: comments on tasks and sprints (SPEC/COMMANDS.md § Task Comments,
§ Sprint Comments; SPEC/MODELS.md § Comment Type; SPEC/DATABASE.md
§ task_comments Table, § sprint_comments Table; SPEC/WEB.md).

End-to-end coverage of the eight comment subcommands against the compiled
./bin/rmp. A comment is a durable, typed, timestamped entry in the work log of
one task or one sprint; the log is append-oriented, it never gates a state
transition, and it is surfaced read-only on the web interface.

What this module pins, and why each group is here rather than in a Go unit test
(the Go tests cover the layers in isolation; only the binary shows the layers
composed, the process boundary, and the exit code):

  - The eight subcommands, both families, on the success path: the exact stdout
    shape of each one, the stored record, and the four aliases per family.
  - The three body input sources — the flag, a heredoc, and a pipe — plus the
    rule that a bad --type never leaves the command blocked on standard input.
    That rule is proved with a FIFO held open by a live writer that never
    sends data: a bad --type must still return promptly, and the same FIFO
    with a good --type must block, which is what rules out standard input
    having simply been at EOF.
  - The per-entity type enum in both directions: every value each family
    accepts, every value it refuses, and the refusal naming that family's own
    valid set. Including the two cross-enum confusions, since -y, --type
    carries an unrelated TaskType elsewhere in the task family.
  - Body validation at its boundaries: empty, whitespace only, exactly 4096
    characters, one character over, 70000 bytes arriving on standard input,
    and the forbidden control characters — among them VT and FF, which
    strings.TrimSpace also strips, so they are only caught when the rule is
    applied to the body AS SUPPLIED.
  - comment-edit with type only, body only, both, and neither; and that an
    edit carrying values identical to the stored ones still stamps updated_at,
    because only the ABSENCE of a change is refused, never value equality.
  - Ordering and filtering asserted on the returned records, not on a count.
  - The lifecycle interactions: comments in all five task statuses and all
    three sprint statuses, survival across task reopen, the cascade when a
    parent is removed, and the comment-neutrality of sprint membership
    changes.
  - The audit trail of all six mutations, including that entity_id is always
    the PARENT's id, that an edit touches only the parent of the comment
    edited, and that a failed mutation writes nothing.
  - The exit codes 2, 3, 4 and 6 on their respective paths, each asserted
    together with the message the SPEC pins.
  - Two robustness properties: eight concurrent writers, and the 1.9.0
    migration recreating the comment tables on a roadmap that predates them.

Every scenario runs against a scratch HOME created per test, so the developer's
real ~/.roadmaps is never touched.
"""

import fcntl
import json
import os
import re
import sqlite3
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


# Exit codes (SPEC/ARCHITECTURE.md § Exit Codes).
EXIT_OK = 0
EXIT_MISUSE = 2
EXIT_NO_ROADMAP = 3
EXIT_NOT_FOUND = 4
EXIT_VALIDATION = 6

# ---------------------------------------------------------------------------
# Domain data: a SEPA batch-settlement reconciliation service. Every body below
# is a sentence a reviewer of that service could have written, so a failure
# message reads like a real log entry and never like a fixture.
# ---------------------------------------------------------------------------

ROADMAP = "ledger-settlement"
ROADMAP_OTHER = "treasury-reporting"

SPRINT_TITLE = "August settlement close"
SPRINT_GOAL = (
    "Close out the August SEPA reconciliation cycle before month-end reporting."
)
SPRINT_TITLE_NEXT = "September settlement close"
SPRINT_GOAL_NEXT = (
    "Carry the idempotency work into the September cycle and verify the retry path."
)
SPRINT_TITLE_THIRD = "October settlement close"
SPRINT_GOAL_THIRD = (
    "Prepare the clearing-feed volume increase ahead of the October cycle."
)

# The three tasks, each with the four fields `task create` requires.
TASK_RECONCILE = {
    "title": "Reconcile SEPA batch settlement mismatches against ledger postings",
    "fr": "Operations must be able to explain every SEPA settlement mismatch "
          "against the ledger before month-end reporting closes.",
    "tr": "Compare each batch line against its ledger posting and report the "
          "unmatched set together with the cause of each mismatch.",
    "ac": "Every mismatch in the August cycle is either matched to a posting or "
          "carries a recorded cause.",
}
TASK_RETRIES = {
    "title": "Automate SEPA batch ingestion retries without duplicate postings",
    "fr": "A failed SEPA batch ingestion must be retried automatically without "
          "creating a second ledger posting for the same movement.",
    "tr": "Derive an idempotency key from (batch_id, sequence_number) and refuse "
          "a second commit under a key already recorded.",
    "ac": "Replaying an ingestion that already committed produces no additional "
          "posting and no error.",
}
TASK_DUPLICATES = {
    "title": "Investigate duplicate SEPA batch IDs from the upstream clearing feed",
    "fr": "The upstream clearing feed sometimes re-sends a batch under an "
          "unchanged id, and the cause must be established before the retry "
          "logic is changed.",
    "tr": "Capture the raw feed for a re-sent batch and compare the two payloads "
          "byte for byte, including the transport headers.",
    "ac": "The reason the upstream re-sends a batch under an unchanged id is "
          "documented with captured evidence.",
}

# One body per comment type. The seven task types plus the sprint decision.
BODY = {
    "FINDING": "Batch ID SEPA-20260815-004 appeared twice in the morning feed; "
               "upstream re-sent after a timeout without changing the batch id.",
    "HYPOTHESIS": "Suspect the ingestion worker retries on a 30s HTTP timeout "
                  "without checking whether the first attempt actually committed.",
    "TEST": "Ran the reconciliation job against a staging replay of 50,000 "
            "transactions; zero unexplained deltas, matches production behaviour.",
    "DECISION": "Decided to add an idempotency key derived from (batch_id, "
                "sequence_number) rather than deduplicating by batch_id alone, "
                "since legitimate re-sends reuse the batch id.",
    "PROGRESS": "Idempotency key added to the ingestion worker; staging tests "
                "green; production rollout scheduled for the next deploy window.",
    "UPDATE": "Widened acceptance criteria to cover the duplicate-batch-id case "
              "discovered during reconciliation testing.",
    "NOTE": "Filed a follow-up ticket for Q4 batch volume increase; today's fix "
            "does not address throughput, only correctness.",
}

SPRINT_DECISION_BODY = (
    "Decided to close the settlement sprint one day early: the upstream clearing "
    "feed had a scheduled outage that blocked further verification, and remaining "
    "tasks carry cleanly into the next sprint."
)

# The canonical accepted sets and the exact list the refusal message renders
# (SPEC/COMMANDS.md § Comment Field Constraints).
TASK_TYPES = ["FINDING", "HYPOTHESIS", "TEST", "DECISION", "PROGRESS", "UPDATE", "NOTE"]
SPRINT_TYPES = ["FINDING", "DECISION", "PROGRESS", "UPDATE"]
TASK_TYPE_LIST = "FINDING, HYPOTHESIS, TEST, DECISION, PROGRESS, UPDATE, NOTE"
SPRINT_TYPE_LIST = "FINDING, DECISION, PROGRESS, UPDATE"

# The JSON shape of one comment record (SPEC/DATA_FORMATS.md § Task Comment,
# § Sprint Comment). Asserted exactly, so a field added or dropped without a
# SPEC decision fails here.
TASK_COMMENT_KEYS = frozenset(
    {"id", "task_id", "type", "body", "created_at", "updated_at"}
)
SPRINT_COMMENT_KEYS = frozenset(
    {"id", "sprint_id", "type", "body", "created_at", "updated_at"}
)

ISO_8601 = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")


class TestTaskAndSprintComments:
    """End-to-end coverage of the eight comment subcommands."""

    # ==================================================================
    # fixture
    # ==================================================================

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self._procs = []
        self._fifo_fds = []
        self.run(["roadmap", "create", ROADMAP])
        self.task = self.create_task(TASK_RECONCILE)
        self.sprint = self.create_sprint(SPRINT_TITLE, SPRINT_GOAL)

    def teardown_method(self):
        for proc in self._procs:
            if proc.poll() is None:
                # Only ever a process this test started, addressed by its own PID.
                proc.kill()
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    pass
            for stream in (proc.stdout, proc.stderr):
                if stream is not None and not stream.closed:
                    stream.close()
        for fd in self._fifo_fds:
            try:
                os.close(fd)
            except OSError:
                pass
        self.test.teardown()

    # ==================================================================
    # command helpers
    # ==================================================================

    def run(self, args, stdin_text=None, stdin_bytes=None, expect=EXIT_OK):
        """Run rmp under the scratch HOME and assert the exit code.

        stdin_bytes exists because some bodies (a NUL byte, a lone BOM) are not
        expressible as text the way subprocess would encode it safely.
        """
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        if stdin_bytes is not None:
            result = subprocess.run(
                [self.cli] + args, capture_output=True, env=env, input=stdin_bytes
            )
            code = result.returncode
            out = result.stdout.decode("utf-8", "replace")
            err = result.stderr.decode("utf-8", "replace")
        else:
            result = subprocess.run(
                [self.cli] + args, capture_output=True, text=True, env=env,
                input=stdin_text,
            )
            code, out, err = result.returncode, result.stdout, result.stderr
        if expect is not None:
            assert code == expect, (
                f"rmp {' '.join(args)}\n"
                f"  expected exit {expect}, got {code}\n"
                f"  stdout: {out!r}\n  stderr: {err!r}"
            )
        return code, out, err

    def run_json(self, args, stdin_text=None):
        _, out, _ = self.run(args, stdin_text=stdin_text)
        return json.loads(out)

    def run_shell(self, script, args=None):
        """Run a bash snippet so real heredocs, pipes and redirects are exercised.

        A parent-created pipe (subprocess input=) is only one of the descriptor
        kinds standard input can be; a heredoc hands the process a seekable
        regular file and a pipeline hands it a pipe from a sibling process. The
        binary is reached through $RMP and the roadmap, ids and paths through
        positional parameters, so nothing is interpolated into the script text.
        """
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        env["RMP"] = self.cli
        result = subprocess.run(
            ["bash", "-c", script, "bash"] + [str(a) for a in (args or [])],
            capture_output=True, text=True, env=env,
        )
        return result.returncode, result.stdout, result.stderr

    def assert_failure(self, args, code, message, stdin_text=None, stdin_bytes=None):
        """Assert a failing invocation: the exit code AND the pinned message.

        Also asserts the failure produced nothing on stdout, since a machine
        consumer parses stdout and must never find a half-written object there.
        """
        actual, out, err = self.run(
            args, stdin_text=stdin_text, stdin_bytes=stdin_bytes, expect=None
        )
        assert actual == code, (
            f"rmp {' '.join(args)}\n  expected exit {code}, got {actual}\n"
            f"  stderr: {err!r}"
        )
        assert message in err, (
            f"rmp {' '.join(args)}\n  expected stderr to contain {message!r}\n"
            f"  got: {err!r}"
        )
        assert out == "", f"a failing command wrote to stdout: {out!r}"
        return err

    # ---- roadmap fixture builders --------------------------------------

    def create_task(self, spec, roadmap=ROADMAP):
        return self.run_json([
            "task", "create", "-r", roadmap,
            "-t", spec["title"], "-fr", spec["fr"],
            "-tr", spec["tr"], "-ac", spec["ac"],
        ])["id"]

    def create_sprint(self, title, goal, roadmap=ROADMAP):
        return self.run_json([
            "sprint", "create", "-r", roadmap, "-t", title, "-d", goal,
        ])["id"]

    # ---- comment helpers -----------------------------------------------

    def add_task_comment(self, ctype, body=None, task=None, roadmap=ROADMAP):
        return self.run_json([
            "task", "comment-add", "-r", roadmap,
            str(self.task if task is None else task),
            "--type", ctype, "--body", BODY[ctype] if body is None else body,
        ])["id"]

    def add_sprint_comment(self, ctype, body=None, sprint=None, roadmap=ROADMAP):
        return self.run_json([
            "sprint", "comment-add", "-r", roadmap,
            str(self.sprint if sprint is None else sprint),
            "--type", ctype, "--body", BODY[ctype] if body is None else body,
        ])["id"]

    def task_comments(self, task=None, ctype=None, roadmap=ROADMAP):
        args = ["task", "comment-list", "-r", roadmap,
                str(self.task if task is None else task)]
        if ctype is not None:
            args += ["--type", ctype]
        return self.run_json(args)

    def sprint_comments(self, sprint=None, ctype=None, roadmap=ROADMAP):
        args = ["sprint", "comment-list", "-r", roadmap,
                str(self.sprint if sprint is None else sprint)]
        if ctype is not None:
            args += ["--type", ctype]
        return self.run_json(args)

    # ---- audit helpers -------------------------------------------------

    def audit_history(self, entity_type, entity_id, roadmap=ROADMAP):
        return self.run_json([
            "audit", "history", "-r", roadmap, entity_type, str(entity_id),
        ])

    def audit_operations(self, entity_type, entity_id, roadmap=ROADMAP):
        return [e["operation"] for e in self.audit_history(entity_type, entity_id, roadmap)]

    def audit_total(self, roadmap=ROADMAP):
        return self.run_json(["audit", "stats", "-r", roadmap])["total_entries"]

    def audit_by_operation(self, roadmap=ROADMAP):
        return self.run_json(["audit", "stats", "-r", roadmap])["by_operation"]

    # ---- a FIFO whose writer never sends anything -----------------------

    def open_blocking_fifo(self):
        """Return a read fd on a FIFO held open by a live writer that never writes.

        A read on this fd blocks indefinitely: the writer holds the write end
        open, so the reader never sees EOF. That is what distinguishes "the
        command did not read standard input" from "standard input happened to
        be empty".

        The read end is opened non-blocking first and then switched back to
        blocking, so this helper can never hang the suite waiting for a writer
        that failed to start; the writer is only confirmed afterwards, through
        the READY marker it prints once it holds the FIFO open.
        """
        path = os.path.join(self.test.test_dir, "comment_body.fifo")
        if os.path.exists(path):
            os.remove(path)
        os.mkfifo(path, 0o600)

        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK)
        self._fifo_fds.append(fd)
        flags = fcntl.fcntl(fd, fcntl.F_GETFL)
        fcntl.fcntl(fd, fcntl.F_SETFL, flags & ~os.O_NONBLOCK)

        writer = subprocess.Popen(
            [sys.executable, "-c",
             "import sys, time\n"
             "handle = open(sys.argv[1], 'w')\n"
             "sys.stdout.write('READY\\n')\n"
             "sys.stdout.flush()\n"
             "time.sleep(120)\n",
             path],
            stdout=subprocess.PIPE, text=True,
        )
        self._procs.append(writer)
        marker = writer.stdout.readline()
        assert marker.strip() == "READY", (
            f"the FIFO writer never confirmed it opened the pipe: {marker!r}"
        )
        return fd

    def popen_with_stdin(self, args, fd):
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        proc = subprocess.Popen(
            [self.cli] + args, stdin=fd,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env,
        )
        self._procs.append(proc)
        return proc

    # ---- shared assertions ---------------------------------------------

    @staticmethod
    def assert_comment_shape(comment, keys, parent_field, parent_id):
        got = set(comment.keys())
        missing, extra = keys - got, got - keys
        assert not missing and not extra, (
            "comment JSON shape diverges from SPEC/DATA_FORMATS.md:\n"
            f"  missing: {sorted(missing)}\n  extra:   {sorted(extra)}"
        )
        assert comment[parent_field] == parent_id, (
            f"comment {comment['id']} reports {parent_field}={comment[parent_field]}, "
            f"expected {parent_id}"
        )
        assert isinstance(comment["id"], int) and comment["id"] > 0
        assert ISO_8601.match(comment["created_at"]), comment["created_at"]

    @staticmethod
    def bodies(comments):
        return [c["body"] for c in comments]

    @staticmethod
    def types(comments):
        return [c["type"] for c in comments]

    @staticmethod
    def ids(comments):
        return [c["id"] for c in comments]

    # ==================================================================
    # A. the eight subcommands on the success path
    # ==================================================================

    def test_task_comment_add_prints_only_the_new_id(self):
        """comment-add writes exactly {"id": n} to stdout, and nothing else."""
        code, out, err = self.run([
            "task", "comment-add", "-r", ROADMAP, str(self.task),
            "--type", "FINDING", "--body", BODY["FINDING"],
        ])
        assert code == EXIT_OK
        assert err == "", f"a successful add wrote to stderr: {err!r}"
        payload = json.loads(out)
        assert payload == {"id": 1}, payload
        # Nothing framing the object: the whole of stdout IS the object.
        assert out.strip().startswith("{") and out.strip().endswith("}"), out

        stored = self.task_comments()
        assert len(stored) == 1
        self.assert_comment_shape(stored[0], TASK_COMMENT_KEYS, "task_id", self.task)
        assert stored[0]["type"] == "FINDING"
        assert stored[0]["body"] == BODY["FINDING"]
        assert stored[0]["updated_at"] is None, (
            "updated_at must start null: nothing has edited this comment yet"
        )
        print("✓ task comment-add prints only the new id and stores the record")

    def test_sprint_comment_add_prints_only_the_new_id(self):
        """The sprint family mirrors the task family's add contract."""
        code, out, err = self.run([
            "sprint", "comment-add", "-r", ROADMAP, str(self.sprint),
            "--type", "DECISION", "--body", SPRINT_DECISION_BODY,
        ])
        assert code == EXIT_OK and err == ""
        assert json.loads(out) == {"id": 1}, out

        stored = self.sprint_comments()
        assert len(stored) == 1
        self.assert_comment_shape(stored[0], SPRINT_COMMENT_KEYS, "sprint_id", self.sprint)
        assert stored[0]["type"] == "DECISION"
        assert stored[0]["body"] == SPRINT_DECISION_BODY
        assert stored[0]["updated_at"] is None
        print("✓ sprint comment-add prints only the new id and stores the record")

    def test_task_comment_edit_prints_nothing_and_applies_both_fields(self):
        """comment-edit is silent on stdout and replaces type and body in place."""
        cid = self.add_task_comment("HYPOTHESIS")
        code, out, err = self.run([
            "task", "comment-edit", "-r", ROADMAP, str(cid),
            "--type", "FINDING", "--body", BODY["FINDING"],
        ])
        assert code == EXIT_OK
        assert out == "", f"comment-edit must print nothing, got {out!r}"
        assert err == ""

        stored = self.task_comments()[0]
        assert stored["type"] == "FINDING"
        assert stored["body"] == BODY["FINDING"]
        assert stored["updated_at"] is not None and ISO_8601.match(stored["updated_at"])
        assert stored["updated_at"] >= stored["created_at"]
        print("✓ task comment-edit prints nothing and applies both fields")

    def test_sprint_comment_edit_prints_nothing_and_applies_both_fields(self):
        cid = self.add_sprint_comment("PROGRESS")
        code, out, err = self.run([
            "sprint", "comment-edit", "-r", ROADMAP, str(cid),
            "--type", "DECISION", "--body", SPRINT_DECISION_BODY,
        ])
        assert code == EXIT_OK and out == "" and err == ""

        stored = self.sprint_comments()[0]
        assert stored["type"] == "DECISION"
        assert stored["body"] == SPRINT_DECISION_BODY
        assert stored["updated_at"] is not None
        print("✓ sprint comment-edit prints nothing and applies both fields")

    def test_task_comment_remove_prints_nothing_and_drops_the_row(self):
        """comment-remove is silent, removes exactly one row, and leaves the rest."""
        keep = self.add_task_comment("FINDING")
        drop = self.add_task_comment("NOTE")
        code, out, err = self.run([
            "task", "comment-remove", "-r", ROADMAP, str(drop),
        ])
        assert code == EXIT_OK
        assert out == "", f"comment-remove must print nothing, got {out!r}"
        assert err == ""

        remaining = self.task_comments()
        assert self.ids(remaining) == [keep], self.ids(remaining)
        print("✓ task comment-remove prints nothing and drops exactly one row")

    def test_sprint_comment_remove_prints_nothing_and_drops_the_row(self):
        keep = self.add_sprint_comment("FINDING")
        drop = self.add_sprint_comment("UPDATE")
        code, out, err = self.run([
            "sprint", "comment-remove", "-r", ROADMAP, str(drop),
        ])
        assert code == EXIT_OK and out == "" and err == ""
        assert self.ids(self.sprint_comments()) == [keep]
        print("✓ sprint comment-remove prints nothing and drops exactly one row")

    def test_comment_list_returns_every_stored_field(self):
        """comment-list is the read surface for both families; the shape is pinned."""
        self.add_task_comment("FINDING")
        self.add_sprint_comment("DECISION", SPRINT_DECISION_BODY)

        task_log = self.task_comments()
        assert len(task_log) == 1
        self.assert_comment_shape(task_log[0], TASK_COMMENT_KEYS, "task_id", self.task)

        sprint_log = self.sprint_comments()
        assert len(sprint_log) == 1
        self.assert_comment_shape(
            sprint_log[0], SPRINT_COMMENT_KEYS, "sprint_id", self.sprint
        )
        print("✓ comment-list returns every stored field on both families")

    def test_every_comment_subcommand_alias_resolves(self):
        """The four aliases per family address the same subcommands.

        Asserted through the effect, not through the help text: an alias that
        resolved to the wrong handler would still exit 0 on --help.
        """
        # task: c-add, c-ls, c-edit, c-rm
        created = self.run_json([
            "task", "c-add", "-r", ROADMAP, str(self.task),
            "--type", "PROGRESS", "--body", BODY["PROGRESS"],
        ])["id"]
        listed = self.run_json(["task", "c-ls", "-r", ROADMAP, str(self.task)])
        assert self.ids(listed) == [created]
        self.run(["task", "c-edit", "-r", ROADMAP, str(created), "--type", "UPDATE"])
        assert self.types(self.task_comments()) == ["UPDATE"]
        self.run(["task", "c-rm", "-r", ROADMAP, str(created)])
        assert self.task_comments() == []

        # sprint: the same four spellings, over sprint_comments.
        s_created = self.run_json([
            "sprint", "c-add", "-r", ROADMAP, str(self.sprint),
            "--type", "PROGRESS", "--body", BODY["PROGRESS"],
        ])["id"]
        s_listed = self.run_json(["sprint", "c-ls", "-r", ROADMAP, str(self.sprint)])
        assert self.ids(s_listed) == [s_created]
        self.run(["sprint", "c-edit", "-r", ROADMAP, str(s_created), "--type", "UPDATE"])
        assert self.types(self.sprint_comments()) == ["UPDATE"]
        self.run(["sprint", "c-rm", "-r", ROADMAP, str(s_created)])
        assert self.sprint_comments() == []
        print("✓ all eight comment aliases resolve to their subcommand")

    # ==================================================================
    # B. body input sources
    # ==================================================================

    def test_body_accepted_from_every_flag_spelling(self):
        """--body, -b, --body=<text> and -b=<text> all supply the same body."""
        spellings = [
            ["--body", BODY["FINDING"]],
            ["-b", BODY["HYPOTHESIS"]],
            [f"--body={BODY['TEST']}"],
            [f"-b={BODY['DECISION']}"],
        ]
        expected = [BODY["FINDING"], BODY["HYPOTHESIS"], BODY["TEST"], BODY["DECISION"]]
        for spelling in spellings:
            self.run([
                "task", "comment-add", "-r", ROADMAP, str(self.task),
                "--type", "FINDING",
            ] + spelling)
        assert self.bodies(self.task_comments()) == expected
        print("✓ every --body / -b spelling, inline and separated, supplies the body")

    def test_body_from_heredoc_preserves_interior_line_breaks(self):
        """A real shell heredoc: standard input is a seekable regular file.

        Driven through bash rather than through a parent-created pipe, because
        the two are different kinds of descriptor and the reader must handle
        both. The quoted delimiter stops the shell expanding the body.
        """
        script = (
            '"$RMP" task comment-add -r "$1" "$2" --type HYPOTHESIS <<\'RMPBODY\'\n'
            "Suspect the ingestion worker retries on a 30s HTTP timeout\n"
            "without checking whether the first attempt actually committed.\n"
            "\n"
            "Next step: capture the raw feed for batch SEPA-20260815-004.\n"
            "RMPBODY\n"
        )
        code, out, err = self.run_shell(script, [ROADMAP, self.task])
        assert code == EXIT_OK, f"stdout={out!r} stderr={err!r}"

        stored = self.task_comments()[0]["body"]
        expected = (
            "Suspect the ingestion worker retries on a 30s HTTP timeout\n"
            "without checking whether the first attempt actually committed.\n"
            "\n"
            "Next step: capture the raw feed for batch SEPA-20260815-004."
        )
        # Trailing newline trimmed, interior structure — including the blank
        # line — preserved (SPEC/COMMANDS.md precedence rule 5).
        assert stored == expected, repr(stored)
        assert stored.count("\n") == 3, stored
        assert "\n\n" in stored, "the blank line between paragraphs was lost"
        print("✓ a heredoc body keeps its interior line breaks and loses its edges")

    def test_body_from_a_shell_pipe_is_read_to_eof(self):
        """A real shell pipeline: standard input is a pipe from another process."""
        source = os.path.join(self.test.test_dir, "reconciliation-test.txt")
        with open(source, "w", encoding="utf-8") as handle:
            handle.write(BODY["TEST"] + "\n")

        code, out, err = self.run_shell(
            'cat "$3" | "$RMP" task comment-add -r "$1" "$2" --type TEST',
            [ROADMAP, self.task, source],
        )
        assert code == EXIT_OK, f"stdout={out!r} stderr={err!r}"
        assert self.bodies(self.task_comments()) == [BODY["TEST"]]
        print("✓ a body arriving down a shell pipe is read to EOF")

    def test_body_from_a_file_redirect_is_read_to_eof(self):
        """The `< finding.txt` form the SPEC documents, on both families."""
        finding = os.path.join(self.test.test_dir, "finding.txt")
        with open(finding, "w", encoding="utf-8") as handle:
            handle.write(BODY["FINDING"] + "\n")
        decision = os.path.join(self.test.test_dir, "decision.txt")
        with open(decision, "w", encoding="utf-8") as handle:
            handle.write(SPRINT_DECISION_BODY + "\n")

        code, _, err = self.run_shell(
            '"$RMP" task comment-add -r "$1" "$2" --type FINDING < "$3"',
            [ROADMAP, self.task, finding],
        )
        assert code == EXIT_OK, err
        assert self.bodies(self.task_comments()) == [BODY["FINDING"]]

        code, _, err = self.run_shell(
            '"$RMP" sprint comment-add -r "$1" "$2" --type DECISION < "$3"',
            [ROADMAP, self.sprint, decision],
        )
        assert code == EXIT_OK, err
        assert self.bodies(self.sprint_comments()) == [SPRINT_DECISION_BODY]
        print("✓ the documented `< file` form supplies the body on both families")

    def test_body_edges_are_trimmed_before_storage(self):
        """Leading and trailing whitespace never reaches the stored record."""
        self.run([
            "task", "comment-add", "-r", ROADMAP, str(self.task),
            "--type", "FINDING", "--body", "   \t\n" + BODY["FINDING"] + "  \n\n",
        ])
        assert self.bodies(self.task_comments()) == [BODY["FINDING"]]
        print("✓ the stored body is the trimmed form")

    def test_comment_edit_reads_the_new_body_from_stdin_when_type_is_absent(self):
        """The flagless `comment-edit <id> < file` form is a valid edit."""
        cid = self.add_task_comment("FINDING")
        revised = (
            "Batch ID SEPA-20260815-004 appeared twice in the morning feed; the "
            "upstream re-send carried an identical payload, confirmed byte for byte."
        )
        self.run(["task", "comment-edit", "-r", ROADMAP, str(cid)], stdin_text=revised)

        stored = self.task_comments()[0]
        assert stored["body"] == revised
        assert stored["type"] == "FINDING", "a body-only edit must not change the type"
        assert stored["updated_at"] is not None
        print("✓ comment-edit takes the new body from standard input when --type is absent")

    def test_invalid_type_is_refused_without_reading_standard_input(self):
        """A bad --type must not leave the command blocked on standard input.

        Both halves matter. Over a FIFO whose writer is alive and silent, a bad
        --type must still return exit 6 promptly; the SAME FIFO with a good
        --type must NOT return, which is what proves standard input was
        genuinely blocking rather than simply at EOF
        (SPEC/COMMANDS.md § Comment Body ... Validation order).
        """
        fd = self.open_blocking_fifo()

        started = time.monotonic()
        bad = self.popen_with_stdin(
            ["task", "comment-add", "-r", ROADMAP, str(self.task), "--type", "BOGUS"], fd
        )
        try:
            _, bad_err = bad.communicate(timeout=15)
        except subprocess.TimeoutExpired:
            bad.kill()
            raise AssertionError(
                "a bad --type blocked on standard input instead of failing at once"
            )
        elapsed = time.monotonic() - started
        assert bad.returncode == EXIT_VALIDATION, bad.returncode
        assert f'invalid comment type "BOGUS" for a task comment' in bad_err, bad_err
        assert elapsed < 5.0, (
            f"the type verdict took {elapsed:.2f}s; it must not wait on standard input"
        )

        # The control: a GOOD type over the same still-open FIFO must block.
        good = self.popen_with_stdin(
            ["task", "comment-add", "-r", ROADMAP, str(self.task), "--type", "FINDING"], fd
        )
        try:
            good.wait(timeout=3)
            raise AssertionError(
                "a good --type returned without a body, so standard input was not "
                "blocking and the fast refusal above proves nothing"
            )
        except subprocess.TimeoutExpired:
            pass  # still waiting for a body, exactly as specified
        good.kill()
        good.wait(timeout=5)
        assert self.task_comments() == [], "no comment should have been written"
        print("✓ a bad --type is refused without reading standard input")

    def test_type_only_edit_does_not_wait_on_standard_input(self):
        """`comment-edit --type X` must not read standard input at all."""
        cid = self.add_task_comment("FINDING")
        fd = self.open_blocking_fifo()

        proc = self.popen_with_stdin(
            ["task", "comment-edit", "-r", ROADMAP, str(cid), "--type", "DECISION"], fd
        )
        try:
            _, err = proc.communicate(timeout=15)
        except subprocess.TimeoutExpired:
            proc.kill()
            raise AssertionError("a type-only edit blocked waiting for a body")
        assert proc.returncode == EXIT_OK, f"exit={proc.returncode} stderr={err!r}"

        stored = self.task_comments()[0]
        assert stored["type"] == "DECISION"
        assert stored["body"] == BODY["FINDING"], "the body must be left unchanged"
        print("✓ a type-only edit never waits on standard input")

    # ==================================================================
    # C. the per-entity type enum
    # ==================================================================

    def test_every_task_comment_type_is_accepted(self):
        """All seven task values are accepted and stored as given."""
        for ctype in TASK_TYPES:
            self.add_task_comment(ctype)
        assert self.types(self.task_comments()) == TASK_TYPES
        print("✓ all seven task comment types are accepted")

    def test_every_sprint_comment_type_is_accepted(self):
        """All four sprint values are accepted and stored as given."""
        for ctype in SPRINT_TYPES:
            self.add_sprint_comment(ctype)
        assert self.types(self.sprint_comments()) == SPRINT_TYPES
        print("✓ all four sprint comment types are accepted")

    def test_task_only_types_are_refused_on_a_sprint_comment(self):
        """HYPOTHESIS, TEST and NOTE are refused on a sprint, naming the four values."""
        for ctype in ["HYPOTHESIS", "TEST", "NOTE"]:
            err = self.assert_failure(
                ["sprint", "comment-add", "-r", ROADMAP, str(self.sprint),
                 "--type", ctype, "--body", BODY[ctype]],
                EXIT_VALIDATION,
                f'invalid comment type "{ctype}" for a sprint comment; '
                f"valid types: {SPRINT_TYPE_LIST}",
            )
            # The refusal must name the SPRINT set, never leak the task set.
            assert "HYPOTHESIS" not in err.split("valid types:")[1], err
        assert self.sprint_comments() == []
        print("✓ the three task-only types are refused on a sprint comment")

    def test_type_outside_the_enum_is_refused_on_both_families(self):
        """A value in neither set is refused, each family naming its own set."""
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task),
             "--type", "BLOCKER", "--body", BODY["FINDING"]],
            EXIT_VALIDATION,
            f'invalid comment type "BLOCKER" for a task comment; '
            f"valid types: {TASK_TYPE_LIST}",
        )
        self.assert_failure(
            ["sprint", "comment-add", "-r", ROADMAP, str(self.sprint),
             "--type", "BLOCKER", "--body", SPRINT_DECISION_BODY],
            EXIT_VALIDATION,
            f'invalid comment type "BLOCKER" for a sprint comment; '
            f"valid types: {SPRINT_TYPE_LIST}",
        )
        print("✓ a value outside the enum is refused on both families")

    def test_lowercase_type_is_refused(self):
        """The enum is case-sensitive: `finding` is not `FINDING`."""
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task),
             "--type", "finding", "--body", BODY["FINDING"]],
            EXIT_VALIDATION,
            'invalid comment type "finding" for a task comment',
        )
        print("✓ a lowercase type value is refused")

    def test_empty_type_is_a_validation_error_not_a_missing_parameter(self):
        """`--type ""` is a present-but-invalid value: exit 6, not exit 2."""
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task),
             "--type", "", "--body", BODY["FINDING"]],
            EXIT_VALIDATION,
            'invalid comment type "" for a task comment; '
            f"valid types: {TASK_TYPE_LIST}",
        )
        print("✓ an empty --type is a validation error, not a missing parameter")

    def test_type_is_required_on_comment_add(self):
        """comment-add without --type is a missing parameter on both families."""
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task),
             "--body", BODY["FINDING"]],
            EXIT_MISUSE, "required parameter missing: --type",
        )
        self.assert_failure(
            ["sprint", "comment-add", "-r", ROADMAP, str(self.sprint),
             "--body", SPRINT_DECISION_BODY],
            EXIT_MISUSE, "required parameter missing: --type",
        )
        print("✓ --type is required on comment-add")

    def test_the_two_type_enums_are_never_interchangeable(self):
        """-y, --type carries two unrelated enums; each rejects the other's values.

        Both directions, because the flag spelling is shared inside the task
        family (SPEC/COMMANDS.md § Task Comments, third bullet).
        """
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task),
             "--type", "BUG", "--body", BODY["FINDING"]],
            EXIT_VALIDATION,
            f'invalid comment type "BUG" for a task comment; valid types: {TASK_TYPE_LIST}',
        )
        self.assert_failure(
            ["task", "create", "-r", ROADMAP, "-t", TASK_RETRIES["title"],
             "-fr", TASK_RETRIES["fr"], "-tr", TASK_RETRIES["tr"],
             "-ac", TASK_RETRIES["ac"], "--type", "FINDING"],
            EXIT_VALIDATION, 'invalid task type: "FINDING"',
        )
        print("✓ the TaskType and comment-type enums reject each other's values")

    def test_repeated_type_flag_lets_the_last_occurrence_win(self):
        """A repeated --type is last-wins in full: not a merge, not first-wins."""
        self.run([
            "task", "comment-add", "-r", ROADMAP, str(self.task),
            "--type", "BOGUS", "--type", "NOTE", "--body", BODY["NOTE"],
        ])
        stored = self.task_comments()
        assert self.types(stored) == ["NOTE"], self.types(stored)
        assert self.bodies(stored) == [BODY["NOTE"]]
        print("✓ a repeated --type resolves to the last occurrence")

    def test_comment_list_type_filter_refuses_the_other_family_value(self):
        """The filter is validated against the family's own set, not silently empty.

        A second, independent code path from comment-add's check: the filter is
        parsed by comment-list itself.
        """
        self.add_sprint_comment("DECISION", SPRINT_DECISION_BODY)
        self.assert_failure(
            ["sprint", "comment-list", "-r", ROADMAP, str(self.sprint), "--type", "NOTE"],
            EXIT_VALIDATION,
            f'invalid comment type "NOTE" for a sprint comment; '
            f"valid types: {SPRINT_TYPE_LIST}",
        )
        self.assert_failure(
            ["task", "comment-list", "-r", ROADMAP, str(self.task), "--type", "BUG"],
            EXIT_VALIDATION,
            f'invalid comment type "BUG" for a task comment; valid types: {TASK_TYPE_LIST}',
        )
        print("✓ comment-list refuses a filter value the family does not accept")

    # ==================================================================
    # D. body validation at its boundaries
    # ==================================================================

    def test_empty_and_whitespace_bodies_are_missing_parameters(self):
        """An unusable body is a MISSING parameter (exit 2), never a validation error.

        Whitespace only is the interesting case: the domain would call it a
        validation failure, and the command layer deliberately overrides that
        so the exit code says "you gave me nothing" (SPEC/COMMANDS.md rule 4).
        """
        for body in ["", "   ", "\t\t", "\n\n"]:
            self.assert_failure(
                ["task", "comment-add", "-r", ROADMAP, str(self.task),
                 "--type", "FINDING", "--body", body],
                EXIT_MISUSE, "required parameter missing: no comment body supplied",
            )
        # The same over standard input, where the body simply never arrives.
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task), "--type", "FINDING"],
            EXIT_MISUSE, "required parameter missing: no comment body supplied",
            stdin_text="   \n \t \n",
        )
        assert self.task_comments() == []
        print("✓ an empty or whitespace-only body is a missing parameter")

    def test_body_at_the_character_limit_is_accepted_and_one_over_is_refused(self):
        """4096 CHARACTERS, not bytes: a multi-byte body proves the unit.

        A CJK body of 4096 characters is 12288 bytes. It must be accepted, and
        4097 characters must be refused, which is only true if this layer and
        SQLite's length() agree that the unit is characters
        (SPEC/MODELS.md MaxCommentBody).
        """
        at_limit = "決" * 4096
        self.run([
            "task", "comment-add", "-r", ROADMAP, str(self.task),
            "--type", "FINDING", "--body", at_limit,
        ])
        stored = self.task_comments()[0]["body"]
        assert len(stored) == 4096, len(stored)
        assert stored == at_limit
        assert len(at_limit.encode("utf-8")) == 12288, "the fixture must be multi-byte"

        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task),
             "--type", "FINDING", "--body", "決" * 4097],
            EXIT_VALIDATION, "body exceeds maximum length of 4096 characters",
        )
        assert len(self.task_comments()) == 1, "the oversized body must not be stored"
        print("✓ the 4096-character limit is counted in characters, not bytes")

    def test_oversized_stdin_body_is_refused_and_never_truncated(self):
        """A 70000-byte body on standard input is refused, never truncated.

        The refusal is the point: a reader that silently kept the first 4096
        characters would store a comment the author never wrote. The read itself
        is bounded (see the test below), so the verdict comes from a retained
        prefix that is already over the cap rather than from draining the stream.
        """
        oversized = (
            "Reconciliation replay log for the August cycle. " * 1500
        )
        assert len(oversized) > 70000, len(oversized)
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task), "--type", "TEST"],
            EXIT_VALIDATION, "body exceeds maximum length of 4096 characters",
            stdin_text=oversized,
        )
        assert self.task_comments() == []
        print("✓ a 70000-byte body on standard input is refused, not truncated")

    def test_oversized_stdin_body_is_refused_without_draining_the_writer(self):
        """The body read is BOUNDED: an oversized stream is refused after a few
        kilobytes instead of being buffered in full.

        A security audit measured the unbounded read this replaces: 64 MiB of
        input produced 246 MB of peak RSS, 256 MiB produced 868 MB and 512 MiB
        produced 1.27 GB, all for a body that was always going to be refused —
        so any pipeline feeding rmp from an untrusted source could drive the
        machine into swap through a command whose largest acceptable input is
        about 16 KiB.

        The writer here keeps sending until the pipe breaks and reports how much
        it managed to send. The bound asserted is deliberately generous (a pipe
        buffer is 64 KiB, and the reader needs at most a couple of chunks), so
        the test is stable while still failing outright if the reader ever goes
        back to draining whatever it is offered.
        """
        chunk = b"a" * (64 * 1024)
        offered = 64 * 1024 * 1024
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        proc = subprocess.Popen(
            [self.cli, "task", "comment-add", "-r", ROADMAP, str(self.task),
             "--type", "TEST"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            env=env,
        )
        self._procs.append(proc)

        sent = 0
        try:
            while sent < offered:
                proc.stdin.write(chunk)
                proc.stdin.flush()
                sent += len(chunk)
        except (BrokenPipeError, OSError, ValueError):
            # BrokenPipeError once rmp has exited; ValueError from a flush on the
            # writer Python closed when the pipe broke. Both mean the same thing:
            # the reader stopped before the writer ran out of data.
            pass
        finally:
            try:
                proc.stdin.close()
            except (BrokenPipeError, OSError, ValueError):
                pass

        # communicate() would flush the writer Python already closed on the
        # broken pipe (ValueError), so the streams are drained directly. Both are
        # small — an error line on stderr, nothing on stdout — so a read to EOF
        # cannot deadlock.
        _stdout = proc.stdout.read()
        stderr = proc.stderr.read()
        proc.wait(timeout=30)
        assert proc.returncode == EXIT_VALIDATION, (
            f"an oversized body must be refused with exit {EXIT_VALIDATION}; "
            f"got {proc.returncode}, stderr={stderr!r}")
        assert b"body exceeds maximum length of 4096 characters" in stderr, stderr
        assert sent < 8 * 1024 * 1024, (
            f"the reader consumed at least {sent} bytes of the {offered} offered: "
            f"the standard-input body read is no longer bounded")
        assert self.task_comments() == [], "nothing may be stored"
        print("✓ an oversized standard-input body is refused after "
              f"{sent} bytes, not after draining the stream")

    def test_vertical_tab_and_form_feed_bodies_are_refused(self):
        """VT and FF are forbidden AND stripped by trimming, so the rule must
        apply to the body AS SUPPLIED.

        strings.TrimSpace removes VT (0x0B) and FF (0x0C). Validating the
        trimmed form would accept a body that contains them at either edge,
        reporting nothing; validating the raw form rejects it. These are the
        only forbidden characters trimming also removes, so this is the one
        case that separates the two implementations.
        """
        for char in ["\x0b", "\x0c"]:
            body = char + "Found a race in the settlement worker pool" + char
            # Trimming would leave a body that is valid on every rule, so a
            # validator working on the trimmed form would accept this silently.
            assert body.strip() == "Found a race in the settlement worker pool", body
            self.assert_failure(
                ["task", "comment-add", "-r", ROADMAP, str(self.task),
                 "--type", "FINDING", "--body", body],
                EXIT_VALIDATION, "body: control characters are not allowed",
            )
        # And in the interior, where trimming was never a factor.
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task), "--type", "FINDING",
             "--body", "Found a race\x0bin the settlement worker pool"],
            EXIT_VALIDATION, "body: control characters are not allowed",
        )
        assert self.task_comments() == []
        print("✓ VT and FF bodies are refused even though trimming would hide them")

    def test_nul_byte_body_is_refused(self):
        """A NUL arriving on standard input is a control character, not a terminator."""
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task), "--type", "FINDING"],
            EXIT_VALIDATION, "body: control characters are not allowed",
            stdin_bytes="Ledger posting \x00 mismatch".encode("utf-8"),
        )
        assert self.task_comments() == []
        print("✓ a NUL byte in the body is refused")

    def test_trojan_source_bodies_are_refused(self):
        """A bidirectional override or a byte-order mark is refused.

        U+202E can make a stored comment read differently from the bytes it
        contains; a leading U+FEFF is invisible. Both are rejected as control
        characters (SPEC/COMMANDS.md § Control Character Validation).
        """
        bodies = [
            "Ledger posting reversed ‮gnitsop regdeL",
            "﻿Batch ID SEPA-20260815-004 appeared twice in the morning feed.",
        ]
        for body in bodies:
            self.assert_failure(
                ["task", "comment-add", "-r", ROADMAP, str(self.task),
                 "--type", "FINDING"],
                EXIT_VALIDATION, "body: control characters are not allowed",
                stdin_bytes=body.encode("utf-8"),
            )
        assert self.task_comments() == []
        print("✓ trojan-source bodies (U+202E, U+FEFF) are refused")

    # ==================================================================
    # E. comment-edit semantics
    # ==================================================================

    def test_comment_edit_with_type_only_leaves_the_body_untouched(self):
        cid = self.add_task_comment("HYPOTHESIS")
        self.run(["task", "comment-edit", "-r", ROADMAP, str(cid), "--type", "FINDING"])
        stored = self.task_comments()[0]
        assert stored["type"] == "FINDING"
        assert stored["body"] == BODY["HYPOTHESIS"]
        assert stored["updated_at"] is not None
        print("✓ a type-only edit leaves the body untouched")

    def test_comment_edit_with_body_only_leaves_the_type_untouched(self):
        cid = self.add_task_comment("FINDING")
        revised = "Batch ID SEPA-20260815-004 was re-sent twice, not once."
        self.run(["task", "comment-edit", "-r", ROADMAP, str(cid), "--body", revised])
        stored = self.task_comments()[0]
        assert stored["type"] == "FINDING"
        assert stored["body"] == revised
        assert stored["updated_at"] is not None
        print("✓ a body-only edit leaves the type untouched")

    def test_comment_edit_with_neither_change_is_refused(self):
        """No --type, no --body and nothing on standard input requests no change."""
        cid = self.add_task_comment("FINDING")
        self.assert_failure(
            ["task", "comment-edit", "-r", ROADMAP, str(cid)],
            EXIT_MISUSE,
            "required parameter missing: at least one of --type or --body is required",
            stdin_text="",
        )
        # Unchanged, and specifically never stamped as edited.
        stored = self.task_comments()[0]
        assert stored["type"] == "FINDING"
        assert stored["body"] == BODY["FINDING"]
        assert stored["updated_at"] is None, "a refused edit must not stamp updated_at"

        self.assert_failure(
            ["sprint", "comment-edit", "-r", ROADMAP,
             str(self.add_sprint_comment("PROGRESS"))],
            EXIT_MISUSE,
            "required parameter missing: at least one of --type or --body is required",
            stdin_text="   \n",
        )
        print("✓ comment-edit refuses an edit that requests no change")

    def test_comment_edit_with_identical_values_still_stamps_updated_at(self):
        """Only the ABSENCE of a change is refused, never value equality.

        Re-submitting the stored type and the stored body is a change request
        that happens to be idempotent in content; the SPEC makes updated_at the
        record that an edit happened, so it must be stamped.
        """
        cid = self.add_task_comment("DECISION")
        before = self.task_comments()[0]
        assert before["updated_at"] is None

        self.run([
            "task", "comment-edit", "-r", ROADMAP, str(cid),
            "--type", "DECISION", "--body", BODY["DECISION"],
        ])

        after = self.task_comments()[0]
        assert after["type"] == before["type"]
        assert after["body"] == before["body"]
        assert after["created_at"] == before["created_at"], "created_at is immutable"
        assert after["updated_at"] is not None, (
            "an edit with identical values must still stamp updated_at"
        )
        assert self.audit_operations("TASK", self.task).count("TASK_COMMENT_UPDATE") == 1
        print("✓ an edit with identical values still stamps updated_at")

    # ==================================================================
    # F. ordering and filtering, asserted on the returned data
    # ==================================================================

    def test_comments_are_returned_oldest_first(self):
        """The log reads as one story: created_at ascending, id as tie-breaker.

        Asserted on the returned bodies and ids, not on the count: a reversed
        listing has exactly the same length.
        """
        order = ["FINDING", "HYPOTHESIS", "TEST", "DECISION", "PROGRESS"]
        created = [self.add_task_comment(t) for t in order]

        log = self.task_comments()
        assert self.ids(log) == created, self.ids(log)
        assert self.types(log) == order, self.types(log)
        assert self.bodies(log) == [BODY[t] for t in order]
        stamps = [c["created_at"] for c in log]
        assert stamps == sorted(stamps), stamps

        s_order = ["FINDING", "PROGRESS", "DECISION", "UPDATE"]
        s_created = [self.add_sprint_comment(t) for t in s_order]
        s_log = self.sprint_comments()
        assert self.ids(s_log) == s_created
        assert self.types(s_log) == s_order
        print("✓ both families return their comments oldest first")

    def test_editing_a_comment_does_not_reorder_the_log(self):
        """The order is the order the work happened, so an edit never moves an entry."""
        first = self.add_task_comment("FINDING")
        second = self.add_task_comment("HYPOTHESIS")
        third = self.add_task_comment("TEST")

        self.run(["task", "comment-edit", "-r", ROADMAP, str(first),
                  "--body", "Batch SEPA-20260815-004 re-sent after a 30s timeout."])

        assert self.ids(self.task_comments()) == [first, second, third]
        print("✓ an edit does not reorder the log")

    def test_type_filter_returns_only_that_type_in_order(self):
        """The filter narrows the log without changing its order."""
        self.add_task_comment("FINDING")
        second_finding = "A second re-send of SEPA-20260815-004 arrived at 11:42Z."
        self.add_task_comment("HYPOTHESIS")
        self.add_task_comment("FINDING", second_finding)
        self.add_task_comment("NOTE")

        findings = self.task_comments(ctype="FINDING")
        assert self.types(findings) == ["FINDING", "FINDING"], self.types(findings)
        assert self.bodies(findings) == [BODY["FINDING"], second_finding]
        ids = self.ids(findings)
        assert ids == sorted(ids), ids

        assert self.bodies(self.task_comments(ctype="NOTE")) == [BODY["NOTE"]]
        print("✓ the type filter returns only that type, in order")

    def test_type_filter_with_no_match_returns_an_empty_array(self):
        """A filter matching nothing is an empty array, never an error."""
        self.add_task_comment("FINDING")
        code, out, _ = self.run(
            ["task", "comment-list", "-r", ROADMAP, str(self.task), "--type", "DECISION"]
        )
        assert code == EXIT_OK
        assert out.strip() == "[]", out
        print("✓ a filter that matches nothing returns an empty array")

    def test_an_empty_log_is_a_literal_empty_array(self):
        """Asserted on the RAW stdout: a Go nil slice would serialise as `null`,
        which json.loads() accepts, so parsing first would hide the difference.
        """
        _, out, _ = self.run(["task", "comment-list", "-r", ROADMAP, str(self.task)])
        assert out.strip() == "[]", f"expected a literal empty array, got {out!r}"
        _, s_out, _ = self.run(
            ["sprint", "comment-list", "-r", ROADMAP, str(self.sprint)]
        )
        assert s_out.strip() == "[]", f"expected a literal empty array, got {s_out!r}"
        print("✓ an empty comment log is a literal []")

    # ==================================================================
    # G. lifecycle interaction
    # ==================================================================

    def test_comments_are_accepted_in_every_task_status(self):
        """All five statuses accept a comment, and none of them is changed by it.

        SPRINT is reachable only through `sprint add-tasks`; `task stat <id>
        SPRINT` is refused (SPEC/STATE_MACHINE.md), so the walk goes through
        the sprint.
        """
        seen = {}

        self.test.assert_task_status(ROADMAP, self.task, "BACKLOG")
        seen["BACKLOG"] = self.add_task_comment("NOTE")

        self.run(["sprint", "add-tasks", "-r", ROADMAP, str(self.sprint), str(self.task)])
        self.test.assert_task_status(ROADMAP, self.task, "SPRINT")
        seen["SPRINT"] = self.add_task_comment("UPDATE")

        self.run(["sprint", "start", "-r", ROADMAP, str(self.sprint)])
        self.run(["task", "stat", "-r", ROADMAP, str(self.task), "DOING"])
        self.test.assert_task_status(ROADMAP, self.task, "DOING")
        seen["DOING"] = self.add_task_comment("HYPOTHESIS")

        self.run(["task", "stat", "-r", ROADMAP, str(self.task), "TESTING"])
        self.test.assert_task_status(ROADMAP, self.task, "TESTING")
        seen["TESTING"] = self.add_task_comment("TEST")

        self.run(["task", "stat", "-r", ROADMAP, str(self.task), "COMPLETED",
                  "--summary", "Idempotency key deployed; August cycle reconciled."])
        self.test.assert_task_status(ROADMAP, self.task, "COMPLETED")
        seen["COMPLETED"] = self.add_task_comment("DECISION")

        # The comment never moved the task: it is still COMPLETED.
        self.test.assert_task_status(ROADMAP, self.task, "COMPLETED")
        assert self.ids(self.task_comments()) == sorted(seen.values())
        assert len(seen) == 5
        print("✓ comments are accepted in all five task statuses")

    def test_comments_are_accepted_in_every_sprint_status(self):
        """PENDING, OPEN and CLOSED all accept a comment, and none is changed."""
        self.test.assert_sprint_status(ROADMAP, self.sprint, "PENDING")
        pending = self.add_sprint_comment("UPDATE")

        self.run(["sprint", "start", "-r", ROADMAP, str(self.sprint)])
        self.test.assert_sprint_status(ROADMAP, self.sprint, "OPEN")
        open_ = self.add_sprint_comment("PROGRESS")

        self.run(["sprint", "close", "-r", ROADMAP, str(self.sprint), "--force"])
        self.test.assert_sprint_status(ROADMAP, self.sprint, "CLOSED")
        closed = self.add_sprint_comment("DECISION", SPRINT_DECISION_BODY)

        self.test.assert_sprint_status(ROADMAP, self.sprint, "CLOSED")
        assert self.ids(self.sprint_comments()) == [pending, open_, closed]
        assert self.bodies(self.sprint_comments())[2] == SPRINT_DECISION_BODY
        print("✓ comments are accepted in all three sprint statuses")

    def test_comment_survives_task_reopen_untouched(self):
        """`task reopen` clears four task fields and touches no comment.

        The comment record must come through byte-identical, updated_at
        included: reopen rewrites the task's own timestamps, and a cascade
        written into that statement would be invisible to a count-only check.
        """
        self.run(["sprint", "add-tasks", "-r", ROADMAP, str(self.sprint), str(self.task)])
        self.run(["sprint", "start", "-r", ROADMAP, str(self.sprint)])
        self.run(["task", "stat", "-r", ROADMAP, str(self.task), "DOING"])
        self.run(["task", "stat", "-r", ROADMAP, str(self.task), "TESTING"])
        self.run(["task", "stat", "-r", ROADMAP, str(self.task), "COMPLETED",
                  "--summary", "Idempotency key deployed; August cycle reconciled."])

        cid = self.add_task_comment("PROGRESS")
        self.run(["task", "comment-edit", "-r", ROADMAP, str(cid), "--type", "UPDATE"])
        before = self.task_comments()
        assert before[0]["updated_at"] is not None

        completed = self.run_json(["task", "get", "-r", ROADMAP, str(self.task)])
        completed = completed[0] if isinstance(completed, list) else completed
        for field in ("started_at", "tested_at", "closed_at", "completion_summary"):
            assert completed[field] is not None, f"{field} should be set before reopen"

        self.run(["task", "reopen", "-r", ROADMAP, str(self.task)])

        reopened = self.run_json(["task", "get", "-r", ROADMAP, str(self.task)])
        reopened = reopened[0] if isinstance(reopened, list) else reopened
        for field in ("started_at", "tested_at", "closed_at", "completion_summary"):
            assert reopened[field] is None, f"reopen must clear {field}"
        assert reopened["status"] == "BACKLOG", reopened["status"]

        after = self.task_comments()
        assert after == before, (
            "reopen altered the comment log:\n"
            f"  before: {before}\n  after:  {after}"
        )
        print("✓ a comment survives task reopen byte-identically")

    def test_task_remove_deletes_its_comments_and_logs_only_task_delete(self):
        """The cascade removes the rows; the audit records the parent delete only.

        No per-comment TASK_COMMENT_DELETE is written: the cascade is a
        database rule, not a comment operation, and the audit entry that
        matters is the one for the task.
        """
        doomed = self.create_task(TASK_DUPLICATES)
        first = self.add_task_comment("FINDING", task=doomed)
        second = self.add_task_comment("HYPOTHESIS", task=doomed)
        assert self.ids(self.task_comments(task=doomed)) == [first, second]

        self.run(["task", "remove", "-r", ROADMAP, str(doomed)])

        # The rows are gone: the comment ids no longer resolve.
        for cid in (first, second):
            self.assert_failure(
                ["task", "comment-edit", "-r", ROADMAP, str(cid), "--type", "NOTE"],
                EXIT_NOT_FOUND, f"task comment {cid} not found",
            )
        # And the parent itself is gone.
        self.assert_failure(
            ["task", "comment-list", "-r", ROADMAP, str(doomed)],
            EXIT_NOT_FOUND, f"task {doomed} not found",
        )

        ops = self.audit_operations("TASK", doomed)
        assert "TASK_DELETE" in ops, ops
        assert "TASK_COMMENT_DELETE" not in ops, (
            f"the cascade must not write a per-comment delete entry: {ops}"
        )
        assert ops.count("TASK_COMMENT_CREATE") == 2, ops
        print("✓ task remove cascades the comments and logs only TASK_DELETE")

    def test_sprint_remove_deletes_its_own_comments_and_logs_only_sprint_delete(self):
        """The sprint cascade behaves exactly as the task cascade does."""
        member = self.task
        self.run(["sprint", "add-tasks", "-r", ROADMAP, str(self.sprint), str(member)])
        sprint_comment = self.add_sprint_comment("DECISION", SPRINT_DECISION_BODY)

        self.run(["sprint", "remove", "-r", ROADMAP, str(self.sprint)])

        self.assert_failure(
            ["sprint", "comment-edit", "-r", ROADMAP, str(sprint_comment),
             "--type", "PROGRESS"],
            EXIT_NOT_FOUND, f"sprint comment {sprint_comment} not found",
        )
        ops = self.audit_operations("SPRINT", self.sprint)
        assert "SPRINT_DELETE" in ops, ops
        assert "SPRINT_COMMENT_DELETE" not in ops, (
            f"the cascade must not write a per-comment delete entry: {ops}"
        )
        assert "SPRINT_COMMENT_CREATE" in ops, ops
        print("✓ sprint remove cascades its own comments and logs only SPRINT_DELETE")

    def test_sprint_remove_does_not_touch_a_member_tasks_comments(self):
        """A task's comments belong to the task, so deleting the sprint keeps them."""
        self.run(["sprint", "add-tasks", "-r", ROADMAP, str(self.sprint), str(self.task)])
        for ctype in ["FINDING", "HYPOTHESIS", "TEST"]:
            self.add_task_comment(ctype)
        self.add_sprint_comment("DECISION", SPRINT_DECISION_BODY)

        before = self.task_comments()
        assert len(before) == 3

        self.run(["sprint", "remove", "-r", ROADMAP, str(self.sprint)])

        after = self.task_comments()
        assert after == before, (
            "sprint remove altered a member task's comments:\n"
            f"  before: {self.ids(before)}\n  after:  {self.ids(after)}"
        )
        print("✓ sprint remove leaves a member task's comments untouched")

    def test_sprint_membership_changes_are_comment_neutral(self):
        """move-tasks and remove-tasks touch no comment in any of three places.

        Asserted in all three dimensions at once: the moved task keeps its own
        comments, the source sprint keeps its own, and the destination's count
        is unaffected.
        """
        destination = self.create_sprint(SPRINT_TITLE_NEXT, SPRINT_GOAL_NEXT)
        self.run(["sprint", "add-tasks", "-r", ROADMAP, str(self.sprint), str(self.task)])

        self.add_task_comment("HYPOTHESIS")
        self.add_task_comment("TEST")
        self.add_sprint_comment("PROGRESS")
        self.add_sprint_comment("DECISION", SPRINT_DECISION_BODY, sprint=destination)

        task_before = self.task_comments()
        source_before = self.sprint_comments()
        dest_before = self.sprint_comments(sprint=destination)
        assert (len(task_before), len(source_before), len(dest_before)) == (2, 1, 1)

        self.run(["sprint", "move-tasks", "-r", ROADMAP,
                  str(self.sprint), str(destination), str(self.task)])
        # The move genuinely happened, so the neutrality claim is not vacuous.
        moved = self.run_json(["sprint", "tasks", "-r", ROADMAP, str(destination)])
        assert [t["id"] for t in moved] == [self.task], moved
        assert self.run_json(["sprint", "tasks", "-r", ROADMAP, str(self.sprint)]) == []

        assert self.task_comments() == task_before
        assert self.sprint_comments() == source_before
        assert self.sprint_comments(sprint=destination) == dest_before

        self.run(["sprint", "remove-tasks", "-r", ROADMAP,
                  str(destination), str(self.task)])
        assert self.run_json(["sprint", "tasks", "-r", ROADMAP, str(destination)]) == []
        assert self.task_comments() == task_before
        assert self.sprint_comments(sprint=destination) == dest_before
        print("✓ move-tasks and remove-tasks are comment-neutral")

    # ==================================================================
    # H. comment ids
    # ==================================================================

    def test_comment_ids_are_independent_per_family(self):
        """`task comment-edit 1` and `sprint comment-edit 1` are unrelated rows.

        Proved before any sprint comment exists: task comment 1 is there, and
        sprint comment 1 must still be a not-found condition.
        """
        task_cid = self.add_task_comment("FINDING")
        assert task_cid == 1, "the fixture needs the first task comment to be id 1"
        assert self.sprint_comments() == []

        self.assert_failure(
            ["sprint", "comment-edit", "-r", ROADMAP, str(task_cid),
             "--type", "PROGRESS", "--body", BODY["PROGRESS"]],
            EXIT_NOT_FOUND, f"sprint comment {task_cid} not found",
        )
        self.assert_failure(
            ["sprint", "comment-remove", "-r", ROADMAP, str(task_cid)],
            EXIT_NOT_FOUND, f"sprint comment {task_cid} not found",
        )
        # The task comment is intact: nothing crossed over.
        assert self.bodies(self.task_comments()) == [BODY["FINDING"]]

        # And the mirror image: a sprint comment id that task_comments lacks.
        for _ in range(2):
            self.add_sprint_comment("PROGRESS")
        second_sprint_cid = self.ids(self.sprint_comments())[1]
        self.assert_failure(
            ["task", "comment-edit", "-r", ROADMAP, str(second_sprint_cid),
             "--type", "NOTE"],
            EXIT_NOT_FOUND, f"task comment {second_sprint_cid} not found",
        )
        print("✓ task and sprint comment ids are independent id spaces")

    def test_comment_ids_are_scoped_to_their_roadmap(self):
        """A comment id from one roadmap does not resolve in another."""
        cid = self.add_task_comment("FINDING")
        self.run(["roadmap", "create", ROADMAP_OTHER])
        self.create_task(TASK_RETRIES, roadmap=ROADMAP_OTHER)

        self.assert_failure(
            ["task", "comment-edit", "-r", ROADMAP_OTHER, str(cid), "--type", "NOTE"],
            EXIT_NOT_FOUND, f"task comment {cid} not found",
        )
        # Untouched in its own roadmap.
        assert self.types(self.task_comments()) == ["FINDING"]
        print("✓ comment ids do not cross roadmaps")

    def test_duplicate_add_creates_two_distinct_rows(self):
        """The log is append-oriented: an identical add never upserts."""
        first = self.add_task_comment("FINDING")
        second = self.add_task_comment("FINDING")
        assert first != second, (first, second)

        log = self.task_comments()
        assert self.ids(log) == [first, second]
        assert self.bodies(log) == [BODY["FINDING"], BODY["FINDING"]]
        print("✓ a byte-identical add creates a second, distinct row")

    def test_malformed_comment_ids_are_format_errors(self):
        """Every malformed id is exit 2 with the format message, never exit 6.

        The comment subcommands deliberately re-classify what the shared id
        validator would report as a validation error, because SPEC/COMMANDS.md
        pins the whole "positive integer" constraint at exit code 2 here.
        """
        for raw in ["0", "-1", "2147483648", "1.0", "+1", "1,2", "one", "1 2", "12abc"]:
            for family, subcommand in [
                ("task", "comment-edit"), ("task", "comment-remove"),
                ("sprint", "comment-edit"), ("sprint", "comment-remove"),
            ]:
                args = [family, subcommand, "-r", ROADMAP, raw]
                if subcommand == "comment-edit":
                    args += ["--type", "FINDING"]
                err = self.assert_failure(args, EXIT_MISUSE, "invalid comment ID")
                assert "must be a positive integer" in err, err
        print("✓ every malformed comment id is a format error at exit 2")

    def test_surrounding_whitespace_in_an_id_is_tolerated(self):
        """An id is trimmed before parsing, as every id in the CLI is.

        utils.ValidateIDString trims deliberately, so the comment subcommands
        inherit the convention. Pinned here so the tolerance is a decision on
        record rather than an accident: a future tightening would have to be a
        deliberate, SPEC-level change across every id, not a silent one.
        """
        cid = self.add_task_comment("FINDING")
        self.run(["task", "comment-edit", "-r", ROADMAP, f" {cid} ", "--type", "NOTE"])
        assert self.types(self.task_comments()) == ["NOTE"]
        self.run(["task", "comment-remove", "-r", ROADMAP, f"\t{cid}\n"])
        assert self.task_comments() == []
        print("✓ surrounding whitespace in a comment id is trimmed, not refused")

    def test_malformed_parent_ids_are_format_errors(self):
        """The parent id is validated the same way, under the parent's own name."""
        for raw in ["0", "-1", "2147483648", "1.0"]:
            err = self.assert_failure(
                ["task", "comment-add", "-r", ROADMAP, raw,
                 "--type", "FINDING", "--body", BODY["FINDING"]],
                EXIT_MISUSE, "invalid task ID",
            )
            assert "must be a positive integer" in err, err
            self.assert_failure(
                ["sprint", "comment-list", "-r", ROADMAP, raw],
                EXIT_MISUSE, "invalid sprint ID",
            )
        print("✓ a malformed parent id is a format error naming the parent")

    def test_the_two_missing_id_paths_report_differently(self):
        """Omitting the id and putting a flag where it belongs are distinct faults.

        A test that only omits everything never reaches the first path, because
        the id slot is then empty rather than occupied by a flag.
        """
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, "--type", "FINDING",
             "--body", BODY["FINDING"]],
            EXIT_MISUSE, 'invalid task ID: "--type" (must be a positive integer)',
        )
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP],
            EXIT_MISUSE, "required parameter missing: task ID required",
        )
        self.assert_failure(
            ["sprint", "comment-add", "-r", ROADMAP, "--type", "DECISION",
             "--body", SPRINT_DECISION_BODY],
            EXIT_MISUSE, 'invalid sprint ID: "--type" (must be a positive integer)',
        )
        self.assert_failure(
            ["sprint", "comment-remove", "-r", ROADMAP],
            EXIT_MISUSE, "required parameter missing: comment ID required",
        )
        print("✓ the two missing-id paths report different messages")

    def test_removing_the_same_comment_twice_is_not_found(self):
        """The second remove is a clean exit 4, not a success and not a crash."""
        cid = self.add_task_comment("NOTE")
        self.run(["task", "comment-remove", "-r", ROADMAP, str(cid)])
        self.assert_failure(
            ["task", "comment-remove", "-r", ROADMAP, str(cid)],
            EXIT_NOT_FOUND, f"task comment {cid} not found",
        )
        print("✓ removing a comment twice is a not-found condition")

    # ==================================================================
    # I. the audit trail
    # ==================================================================

    def test_each_mutation_writes_its_own_audit_operation(self):
        """The six comment operations, one per mutation, and none for a read."""
        task_cid = self.add_task_comment("FINDING")
        self.run(["task", "comment-edit", "-r", ROADMAP, str(task_cid), "--type", "NOTE"])
        self.run(["task", "comment-remove", "-r", ROADMAP, str(task_cid)])
        task_ops = self.audit_operations("TASK", self.task)
        for op in ["TASK_COMMENT_CREATE", "TASK_COMMENT_UPDATE", "TASK_COMMENT_DELETE"]:
            assert task_ops.count(op) == 1, f"{op} in {task_ops}"

        sprint_cid = self.add_sprint_comment("PROGRESS")
        self.run(["sprint", "comment-edit", "-r", ROADMAP, str(sprint_cid),
                  "--type", "DECISION"])
        self.run(["sprint", "comment-remove", "-r", ROADMAP, str(sprint_cid)])
        sprint_ops = self.audit_operations("SPRINT", self.sprint)
        for op in ["SPRINT_COMMENT_CREATE", "SPRINT_COMMENT_UPDATE",
                   "SPRINT_COMMENT_DELETE"]:
            assert sprint_ops.count(op) == 1, f"{op} in {sprint_ops}"
        print("✓ each of the six comment mutations writes its own audit operation")

    def test_comment_list_writes_no_audit_entry(self):
        """Listing is a read: it must leave the audit log exactly as it was."""
        self.add_task_comment("FINDING")
        self.add_sprint_comment("PROGRESS")
        before = self.audit_total()

        self.task_comments()
        self.task_comments(ctype="FINDING")
        self.sprint_comments()
        self.sprint_comments(ctype="PROGRESS")

        assert self.audit_total() == before, (
            f"comment-list wrote audit entries: {before} -> {self.audit_total()}"
        )
        print("✓ comment-list writes no audit entry")

    def test_audit_entity_id_is_always_the_parent(self):
        """entity_id names the PARENT, never the comment's own id.

        The comment ids under test are deliberately pushed clear of the parent
        id first, by spending ids 1-3 on another task. With the parent at id 1
        and the comments at 4, 5 and 6, an entry that logged the comment's own
        id is distinguishable from one that logged the parent's; had the
        comments been ids 1-3 the two would coincide and the assertion would
        hold either way.
        """
        decoy = self.create_task(TASK_RETRIES)
        for ctype in ["FINDING", "HYPOTHESIS", "TEST"]:
            self.add_task_comment(ctype, task=decoy)

        under_test = [self.add_task_comment(t) for t in ["FINDING", "DECISION", "NOTE"]]
        assert under_test == [4, 5, 6], (
            f"the fixture needs comment ids clear of parent {self.task}: {under_test}"
        )

        self.run(["task", "comment-edit", "-r", ROADMAP, str(under_test[1]),
                  "--type", "PROGRESS"])
        self.run(["task", "comment-remove", "-r", ROADMAP, str(under_test[2])])

        entries = [
            e for e in self.audit_history("TASK", self.task)
            if e["operation"].startswith("TASK_COMMENT_")
        ]
        assert len(entries) == 5, entries
        for entry in entries:
            assert entry["entity_type"] == "TASK", entry
            assert entry["entity_id"] == self.task, (
                f"entity_id must be the parent task {self.task}, got {entry}"
            )
            assert entry["entity_id"] not in under_test, (
                f"entity_id carries a COMMENT id, not the parent's: {entry}"
            )
        # The decoy's own comment entries stayed with the decoy.
        decoy_entries = [
            e for e in self.audit_history("TASK", decoy)
            if e["operation"].startswith("TASK_COMMENT_")
        ]
        assert len(decoy_entries) == 3, decoy_entries
        assert all(e["entity_id"] == decoy for e in decoy_entries), decoy_entries

        # The sprint family, with the sprint comment id likewise clear of the
        # parent sprint id.
        decoy_sprint = self.create_sprint(SPRINT_TITLE_NEXT, SPRINT_GOAL_NEXT)
        for ctype in ["FINDING", "PROGRESS", "UPDATE"]:
            self.add_sprint_comment(ctype, sprint=decoy_sprint)
        sprint_cid = self.add_sprint_comment("PROGRESS")
        assert sprint_cid == 4, sprint_cid

        self.run(["sprint", "comment-edit", "-r", ROADMAP, str(sprint_cid),
                  "--type", "DECISION"])
        self.run(["sprint", "comment-remove", "-r", ROADMAP, str(sprint_cid)])
        s_entries = [
            e for e in self.audit_history("SPRINT", self.sprint)
            if e["operation"].startswith("SPRINT_COMMENT_")
        ]
        assert len(s_entries) == 3, s_entries
        for entry in s_entries:
            assert entry["entity_type"] == "SPRINT", entry
            assert entry["entity_id"] == self.sprint, (
                f"entity_id must be the parent sprint {self.sprint}, got {entry}"
            )
            assert entry["entity_id"] != sprint_cid, entry
        print("✓ every comment audit entry names the parent as its entity")

    def test_an_edit_touches_only_the_audited_history_of_its_own_parent(self):
        """Two parents, one edit: the other parent's history must not grow.

        A single-parent test cannot see a parent id leaking between calls of
        the shared closure, because there is only one value it could be.
        """
        other = self.create_task(TASK_RETRIES)
        mine = self.add_task_comment("FINDING")
        theirs = self.add_task_comment("HYPOTHESIS", task=other)

        before_mine = len(self.audit_history("TASK", self.task))
        before_theirs = len(self.audit_history("TASK", other))

        self.run(["task", "comment-edit", "-r", ROADMAP, str(mine), "--type", "DECISION"])

        after_mine = self.audit_history("TASK", self.task)
        after_theirs = self.audit_history("TASK", other)
        assert len(after_mine) == before_mine + 1, (before_mine, len(after_mine))
        assert len(after_theirs) == before_theirs, (
            f"the edit polluted task {other}'s history: "
            f"{before_theirs} -> {len(after_theirs)}"
        )
        assert after_mine[0]["operation"] == "TASK_COMMENT_UPDATE", after_mine[0]
        assert after_mine[0]["entity_id"] == self.task

        # And the same in reverse, so neither direction is privileged.
        self.run(["task", "comment-edit", "-r", ROADMAP, str(theirs), "--type", "NOTE"])
        assert len(self.audit_history("TASK", self.task)) == before_mine + 1
        assert len(self.audit_history("TASK", other)) == before_theirs + 1
        print("✓ an edit writes only against its own comment's parent")

    def test_a_failed_mutation_writes_no_audit_entry(self):
        """A refusal must leave the audit log untouched, whatever the exit code."""
        self.add_task_comment("FINDING")
        before = self.audit_total()

        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, "99999",
             "--type", "NOTE", "--body", BODY["NOTE"]],
            EXIT_NOT_FOUND, "task 99999 not found",
        )
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task),
             "--type", "NOTE", "--body", "決" * 4097],
            EXIT_VALIDATION, "body exceeds maximum length of 4096 characters",
        )
        self.assert_failure(
            ["task", "comment-edit", "-r", ROADMAP, "99999", "--type", "NOTE"],
            EXIT_NOT_FOUND, "task comment 99999 not found",
        )
        self.assert_failure(
            ["task", "comment-remove", "-r", ROADMAP, "99999"],
            EXIT_NOT_FOUND, "task comment 99999 not found",
        )

        assert self.audit_total() == before, (
            f"a failed mutation wrote an audit entry: {before} -> {self.audit_total()}"
        )
        print("✓ a failed mutation writes no audit entry")

    def test_audit_stats_surfaces_every_comment_operation_with_exact_counts(self):
        """All six keys appear in by_operation with the counts the run produced."""
        t_first = self.add_task_comment("FINDING")
        t_second = self.add_task_comment("HYPOTHESIS")
        self.run(["task", "comment-edit", "-r", ROADMAP, str(t_first), "--type", "NOTE"])
        self.run(["task", "comment-edit", "-r", ROADMAP, str(t_second), "--type", "TEST"])
        self.run(["task", "comment-remove", "-r", ROADMAP, str(t_second)])

        s_first = self.add_sprint_comment("PROGRESS")
        self.add_sprint_comment("DECISION", SPRINT_DECISION_BODY)
        self.add_sprint_comment("UPDATE")
        self.run(["sprint", "comment-edit", "-r", ROADMAP, str(s_first),
                  "--type", "FINDING"])
        self.run(["sprint", "comment-remove", "-r", ROADMAP, str(s_first)])

        by_op = self.audit_by_operation()
        expected = {
            "TASK_COMMENT_CREATE": 2,
            "TASK_COMMENT_UPDATE": 2,
            "TASK_COMMENT_DELETE": 1,
            "SPRINT_COMMENT_CREATE": 3,
            "SPRINT_COMMENT_UPDATE": 1,
            "SPRINT_COMMENT_DELETE": 1,
        }
        for op, count in expected.items():
            assert by_op.get(op) == count, (
                f"audit stats reports {op}={by_op.get(op)}, expected {count}"
            )
        print("✓ audit stats surfaces all six comment operations with exact counts")

    # ==================================================================
    # J. flags, help and exit codes
    # ==================================================================

    def test_unknown_flags_are_refused_on_the_flagless_subcommands(self):
        """comment-remove takes no --type; comment-list takes no --body."""
        cid = self.add_task_comment("NOTE")
        self.assert_failure(
            ["task", "comment-remove", "-r", ROADMAP, str(cid), "--type", "NOTE"],
            EXIT_MISUSE, "unknown flag: --type",
        )
        self.assert_failure(
            ["task", "comment-list", "-r", ROADMAP, str(self.task), "--body", "x"],
            EXIT_MISUSE, "unknown flag: --body",
        )
        self.assert_failure(
            ["sprint", "comment-remove", "-r", ROADMAP,
             str(self.add_sprint_comment("PROGRESS")), "-y", "DECISION"],
            EXIT_MISUSE, "unknown flag: -y",
        )
        # The refused remove changed nothing.
        assert self.ids(self.task_comments()) == [cid]
        print("✓ an unknown flag is refused on comment-remove and comment-list")

    def test_body_flag_without_a_usable_value_is_refused(self):
        """No following token, or a following flag: never a silent stdin fallback.

        The second form is the dangerous one — the flag must not be swallowed
        as body text — and neither may fall through to standard input, which is
        why a valid body is offered there and must be ignored.
        """
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task),
             "--type", "NOTE", "--body"],
            EXIT_MISUSE, "required parameter missing: no comment body supplied",
            stdin_text=BODY["NOTE"],
        )
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, str(self.task),
             "--body", "--type", "NOTE"],
            EXIT_MISUSE, "required parameter missing: no comment body supplied",
            stdin_text=BODY["NOTE"],
        )
        self.assert_failure(
            ["sprint", "comment-add", "-r", ROADMAP, str(self.sprint),
             "--type", "PROGRESS", "-b"],
            EXIT_MISUSE, "required parameter missing: no comment body supplied",
            stdin_text=BODY["PROGRESS"],
        )
        assert self.task_comments() == [] and self.sprint_comments() == []
        print("✓ a --body without a usable value is refused, never a stdin fallback")

    def test_a_negative_looking_body_is_a_body_and_not_a_flag(self):
        """`-1` is a value, not a flag: the flag-like test is precise."""
        self.run([
            "task", "comment-add", "-r", ROADMAP, str(self.task),
            "--type", "TEST", "--body", "-1 unexplained delta after the replay",
        ])
        assert self.bodies(self.task_comments()) == [
            "-1 unexplained delta after the replay"
        ]
        print("✓ a body beginning with -1 is accepted as a body")

    def test_missing_roadmap_is_exit_3_on_every_comment_subcommand(self):
        """Exit code 3 is the no-roadmap-selected condition, checked first."""
        cases = [
            ["task", "comment-add", "1", "--type", "FINDING", "--body", BODY["FINDING"]],
            ["task", "comment-list", "1"],
            ["task", "comment-edit", "1", "--type", "FINDING"],
            ["task", "comment-remove", "1"],
            ["sprint", "comment-add", "1", "--type", "DECISION",
             "--body", SPRINT_DECISION_BODY],
            ["sprint", "comment-list", "1"],
            ["sprint", "comment-edit", "1", "--type", "DECISION"],
            ["sprint", "comment-remove", "1"],
        ]
        for args in cases:
            self.assert_failure(
                args, EXIT_NO_ROADMAP,
                "no roadmap selected: use -r <name> or --roadmap <name>",
            )
        # An empty -r value is the same condition, not a lookup of "".
        self.assert_failure(
            ["task", "comment-list", "-r", "", "1"], EXIT_NO_ROADMAP,
            "no roadmap selected",
        )
        print("✓ every comment subcommand reports a missing roadmap as exit 3")

    def test_unknown_roadmap_and_unknown_parent_are_exit_4(self):
        """Exit code 4 covers both the roadmap and the parent entity."""
        self.assert_failure(
            ["task", "comment-add", "-r", "no-such-roadmap", "1",
             "--type", "FINDING", "--body", BODY["FINDING"]],
            EXIT_NOT_FOUND, 'roadmap "no-such-roadmap"',
        )
        self.assert_failure(
            ["task", "comment-add", "-r", ROADMAP, "99999",
             "--type", "FINDING", "--body", BODY["FINDING"]],
            EXIT_NOT_FOUND, "task 99999 not found",
        )
        self.assert_failure(
            ["task", "comment-list", "-r", ROADMAP, "99999"],
            EXIT_NOT_FOUND, "task 99999 not found",
        )
        self.assert_failure(
            ["sprint", "comment-add", "-r", ROADMAP, "99999",
             "--type", "DECISION", "--body", SPRINT_DECISION_BODY],
            EXIT_NOT_FOUND, "sprint 99999 not found",
        )
        self.assert_failure(
            ["sprint", "comment-list", "-r", ROADMAP, "99999"],
            EXIT_NOT_FOUND, "sprint 99999 not found",
        )
        print("✓ an unknown roadmap and an unknown parent are both exit 4")

    def test_help_for_every_comment_subcommand_exits_zero(self):
        """All eight --help surfaces render and document their own usage."""
        for family in ["task", "sprint"]:
            for subcommand in ["comment-add", "comment-list", "comment-edit",
                               "comment-remove"]:
                code, out, err = self.run([family, subcommand, "--help"], expect=None)
                text = out + err
                assert code == EXIT_OK, f"{family} {subcommand} --help exit={code}"
                assert f"rmp {family} {subcommand}" in text, text[:400]
        print("✓ every comment subcommand renders help and exits 0")

    def test_task_family_help_keeps_the_two_type_enums_apart(self):
        """Two distinct blocks, never one merged list of seventeen values.

        -y, --type carries a TaskType on list/create/edit and a comment type on
        the comment subcommands, inside one family (SPEC/HELP.md § Comment
        subcommand help specifics, item 1).
        """
        _, out, err = self.run(["task", "--help"])
        text = out + err

        task_block = re.search(
            r"Valid task types \(for -y, --type on ([^)]*)\):\n\s*([^\n]+)", text
        )
        comment_block = re.search(
            r"Valid comment types \(for -y, --type on ([^)]*)\):\n\s*([^\n]+)",
            text, re.S,
        )
        assert task_block, "the task-type block is missing from `task --help`"
        assert comment_block, "the comment-type block is missing from `task --help`"

        assert task_block.group(2).strip().startswith("USER_STORY, TASK, BUG"), \
            task_block.group(2)
        assert comment_block.group(2).strip() == TASK_TYPE_LIST, comment_block.group(2)
        # Each block names the subcommands it governs.
        assert "'list'" in task_block.group(1) and "'create'" in task_block.group(1)
        assert "comment-add" in comment_block.group(1), comment_block.group(1)
        # Never merged: no single line carries a value from both enums.
        for line in text.splitlines():
            assert not ("USER_STORY" in line and "HYPOTHESIS" in line), line
        # And the overload is stated, not left for the reader to discover.
        assert "two unrelated enums" in text, text
        print("✓ `task --help` keeps the TaskType and comment-type blocks apart")

    def test_sprint_family_help_shows_only_the_sprint_comment_types(self):
        """One block, the four sprint values, and no TaskType block at all.

        The task-only values may be NAMED in the sprint help — it explains that
        they are rejected here — so their mere presence proves nothing. What
        must hold is that no ENUM LISTING in this family offers them: the
        comment-type block renders exactly four values, the seven-value list
        never appears, and there is no TaskType block at all.
        """
        for args in [["sprint", "--help"], ["sprint", "comment-add", "--help"]]:
            label = f"rmp {' '.join(args)}"
            _, out, err = self.run(args)
            text = out + err
            block = re.search(
                r"Valid comment types \(for -y, --type on [^)]*\):\n\s*([^\n]+)",
                text, re.S,
            )
            assert block, f"the comment-type block is missing from `{label}`"
            assert block.group(1).strip() == SPRINT_TYPE_LIST, block.group(1)
            assert "Valid task types" not in text, (
                f"`{label}` carries a TaskType block, which has no meaning here"
            )
            assert TASK_TYPE_LIST not in text, (
                f"`{label}` renders the seven-value task comment list"
            )
            # No comma-separated enum listing mixes the sprint values with a
            # task-only one, which is what a merged set would look like.
            for line in text.splitlines():
                tokens = [t.strip() for t in line.strip().split(",")]
                if "FINDING" not in tokens:
                    continue
                assert set(tokens) == set(SPRINT_TYPES), (
                    f"`{label}` renders a comment-type listing that is not the "
                    f"sprint set: {line!r}"
                )
        print("✓ the sprint family help offers only the four sprint comment types")

    def test_ai_help_publishes_both_comment_enums_as_distinct_keys(self):
        """TaskCommentType and SprintCommentType are separate, and neither is TaskType."""
        contract = self.run_json(["--ai-help"])
        enums = contract["enums"]

        assert "TaskCommentType" in enums and "SprintCommentType" in enums, sorted(enums)
        task_values = [v["value"] for v in enums["TaskCommentType"]["values"]]
        sprint_values = [v["value"] for v in enums["SprintCommentType"]["values"]]
        assert task_values == TASK_TYPES, task_values
        assert sprint_values == SPRINT_TYPES, sprint_values

        # Never aliased into TaskType, whose ten values are unrelated.
        task_type_values = [v["value"] for v in enums["TaskType"]["values"]]
        assert set(task_type_values).isdisjoint(set(task_values)), task_type_values
        assert len(task_type_values) == 10, task_type_values

        # The comment subcommands reference their own enum, per family.
        commands = {c["name"]: c for c in contract["commands"]}
        for family, expected_enum in [("task", "TaskCommentType"),
                                      ("sprint", "SprintCommentType")]:
            subs = {s["name"]: s for s in commands[family]["subcommands"]}
            for name in ["comment-add", "comment-list", "comment-edit"]:
                flags = {f["long"]: f for f in subs[name]["flags"]}
                assert flags["--type"]["enum"] == expected_enum, (
                    f"{family} {name} --type points at {flags['--type']['enum']}"
                )
            # comment-remove carries no --type at all.
            remove_flags = {f["long"] for f in subs["comment-remove"]["flags"]}
            assert "--type" not in remove_flags, remove_flags
        print("✓ --ai-help publishes both comment enums as distinct keys")

    def test_ai_help_publishes_the_body_stdin_fallback(self):
        """The standard-input source is part of the machine-readable contract."""
        contract = self.run_json(["--ai-help"])
        commands = {c["name"]: c for c in contract["commands"]}
        for family in ["task", "sprint"]:
            subs = {s["name"]: s for s in commands[family]["subcommands"]}
            for name in ["comment-add", "comment-edit"]:
                flags = {f["long"]: f for f in subs[name]["flags"]}
                body = flags["--body"]
                assert body.get("stdin_fallback") is True, (
                    f"{family} {name} --body does not publish stdin_fallback: {body}"
                )
                assert body.get("max_length") == 4096, body
        print("✓ --ai-help publishes the --body standard-input fallback")

    def test_errors_go_to_stderr_and_never_to_stdout(self):
        """A machine consumer parses stdout; a refusal must leave it empty."""
        failures = [
            (["task", "comment-add", "-r", ROADMAP, str(self.task), "--type", "BUG",
              "--body", BODY["FINDING"]], EXIT_VALIDATION),
            (["task", "comment-edit", "-r", ROADMAP, "99999", "--type", "NOTE"],
             EXIT_NOT_FOUND),
            (["task", "comment-remove", "-r", ROADMAP, "abc"], EXIT_MISUSE),
            (["sprint", "comment-list", "-r", ROADMAP, "99999"], EXIT_NOT_FOUND),
        ]
        for args, expected in failures:
            code, out, err = self.run(args, expect=None)
            assert code == expected, (args, code)
            assert out == "", f"{args} wrote to stdout: {out!r}"
            assert err.startswith("Error: "), err
        print("✓ comment errors go to stderr and never to stdout")

    # ==================================================================
    # K. robustness
    # ==================================================================

    def test_concurrent_comment_adds_all_succeed_with_distinct_ids(self):
        """Eight writers on one task: all exit 0, all ids distinct, all rows there.

        Contention must be absorbed by the busy timeout, not surfaced as a
        failure: an add that returned exit 1 under SQLITE_BUSY would lose a
        record the caller believes it wrote.
        """
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        procs = []
        for index in range(8):
            procs.append(subprocess.Popen(
                [self.cli, "task", "comment-add", "-r", ROADMAP, str(self.task),
                 "--type", "PROGRESS",
                 "--body", f"Settlement batch worker {index} reported a clean pass "
                           "over the August replay."],
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env,
            ))

        ids = []
        for index, proc in enumerate(procs):
            out, err = proc.communicate(timeout=60)
            assert proc.returncode == EXIT_OK, (
                f"writer {index} failed: exit={proc.returncode} stderr={err!r}"
            )
            ids.append(json.loads(out)["id"])

        assert len(set(ids)) == 8, f"ids collided: {sorted(ids)}"
        stored = self.task_comments()
        assert len(stored) == 8, f"expected 8 rows, got {len(stored)}"
        assert sorted(self.ids(stored)) == sorted(ids)
        assert self.audit_by_operation()["TASK_COMMENT_CREATE"] == 8
        print("✓ eight concurrent comment-add processes all succeed with distinct ids")

    def test_schema_migration_recreates_the_comment_tables_on_reopen(self):
        """A roadmap predating 1.9.0 gains the comment tables on the next command.

        This is the shape of every roadmap that existed before the feature, and
        a create-then-comment test structurally cannot reach it: the tables are
        always already there. The roadmap is downgraded in place — the version
        rolled back and both tables dropped — and the next command must migrate
        it forward, through every later migration (not stopping at 1.9.0; a
        roadmap this old also loses tasks.specialists on the way to CURRENT),
        and accept a comment immediately.

        The expected post-migration version is read from the live database
        BEFORE the downgrade, rather than hardcoded, so this test does not go
        stale the next time SPEC/VERSION.md adds a migration: whatever version
        the binary stamps on a freshly created roadmap today is CURRENT, and a
        pre-1.9.0 database must land on that same CURRENT version, not on 1.9.0
        specifically.
        """
        db_path = os.path.join(
            str(self.test.home_dir), ".roadmaps", ROADMAP, "project.db"
        )
        assert os.path.exists(db_path), db_path

        connection = sqlite3.connect(db_path)
        try:
            current_version = connection.execute(
                "SELECT value FROM _metadata WHERE key = 'schema_version'"
            ).fetchone()[0]
            connection.execute("DROP TABLE task_comments")
            connection.execute("DROP TABLE sprint_comments")
            connection.execute(
                "UPDATE _metadata SET value = '1.8.0' WHERE key = 'schema_version'"
            )
            connection.commit()
            tables = [
                row[0] for row in connection.execute(
                    "SELECT name FROM sqlite_master WHERE type = 'table' "
                    "AND name LIKE '%_comments'"
                )
            ]
            assert tables == [], f"the downgrade left comment tables behind: {tables}"
        finally:
            connection.close()

        # Any command reopens the database and runs the pending migration.
        self.run(["task", "list", "-r", ROADMAP])

        connection = sqlite3.connect(db_path)
        try:
            tables = sorted(
                row[0] for row in connection.execute(
                    "SELECT name FROM sqlite_master WHERE type = 'table' "
                    "AND name LIKE '%_comments'"
                )
            )
            version = connection.execute(
                "SELECT value FROM _metadata WHERE key = 'schema_version'"
            ).fetchone()[0]
        finally:
            connection.close()

        assert tables == ["sprint_comments", "task_comments"], tables
        assert version == current_version, (
            f"a pre-1.9.0 database must migrate all the way to CURRENT "
            f"({current_version!r}), not stop partway; got {version!r}"
        )

        # And the feature works at once, on both families.
        task_cid = self.add_task_comment("FINDING")
        sprint_cid = self.add_sprint_comment("DECISION", SPRINT_DECISION_BODY)
        assert self.ids(self.task_comments()) == [task_cid]
        assert self.ids(self.sprint_comments()) == [sprint_cid]
        print("✓ the 1.9.0 migration recreates the comment tables on reopen, "
              f"landing on CURRENT ({current_version})")


def _run_all():
    cls = TestTaskAndSprintComments
    methods = sorted(m for m in dir(cls) if m.startswith("test_"))
    passed = 0
    failed = 0
    failures = []
    for name in methods:
        instance = cls()
        instance.setup_method()
        try:
            getattr(instance, name)()
            passed += 1
        except AssertionError as exc:
            failed += 1
            failures.append((name, exc))
            print(f"✗ {name}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((name, exc))
            print(f"✗ {name} (error: {type(exc).__name__}: {exc})")
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Task and sprint comment tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
