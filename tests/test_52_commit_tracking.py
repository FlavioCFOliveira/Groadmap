#!/usr/bin/env python3
"""
Test 52: Commit tracking on `task stat` — E2E coverage.

The task entity carries two commit hashes, `commit_open` and `commit_close`,
supplied through the mandatory `--commit-open` / `-co` and `--commit-close` /
`-cc` flags of `task stat` (rmp task #254; SPEC/COMMANDS.md § Change Status
(stat); SPEC/STATE_MACHINE.md § Commit Tracking Fields). Groadmap never derives
a hash: it runs no git command and validates the FORMAT alone.

Every other module in this suite now passes the flags because the states it
needs cannot be reached without them. This module is the single place that
proves the FEATURE, against the shipped binary.

Coverage matrix
----------------
Mandatory where the SPEC says, rejected everywhere else (each case checks the
EXACT exit code and the EXACT stderr line, and re-reads the task to prove
nothing moved):
  1.  `DOING` without `--commit-open`                      -> exit 6
  2.  `COMPLETED` without `--commit-close`                 -> exit 6
  3.  `--commit-open` on BACKLOG / TESTING / COMPLETED     -> exit 6
  4.  `--commit-close` on BACKLOG / DOING / TESTING        -> exit 6
  5.  a malformed hash (6 chars, 65 chars, empty, non-hex) -> exit 6
  6.  a flag written with no value after it                -> exit 2

What gets written:
  7.  a valid transition stores the hash, lowercased, and `task get` shows it
  8.  `TESTING -> DOING` REPLACES `commit_open`
  9.  every task of a multi-ID batch receives the same hash
  10. the short forms `-co` / `-cc` behave as the long ones

The asymmetric clearing — all four routes back to BACKLOG clear `commit_close`
and PRESERVE `commit_open`:
  11. `task stat <ids> BACKLOG`
  12. `task reopen`
  13. `sprint remove-tasks`
  14. `sprint remove`

Atomicity:
  15. a multi-ID batch in which one ID would fail leaves EVERY task unchanged.

Scope boundary:
  16. `task create` and `task edit` accept neither flag (exit 2, unknown flag).

Hash shapes actually seen in the wild:
  17. an abbreviated hash, a full 40-character SHA-1, and a full 64-character
      SHA-256 (`git init --object-format=sha256`) all round-trip unchanged.

Schema migration (SPEC/VERSION.md § Migration 1.10.0 -> 1.11.0):
  18. a 1.10.0-shape database gains both columns on the next open, its existing
      tasks survive with both fields null, and the migrated table enforces the
      same commit rule a freshly created one does.
"""

import os
import sqlite3
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase  # noqa: E402


EXIT_OK = 0
EXIT_MISUSE = 2       # flag written with no value after it
EXIT_INVALID = 6      # rejected transition / malformed hash

# Real short hashes from this project's history, so the fixtures read like a
# roadmap someone actually worked through.
OPEN_FIRST = "4f5ba9b"
OPEN_SECOND = "66a0b1b"
CLOSE_HASH = "4c4ccea"

SIX_CHARS = "a" * 6
SIXTY_FIVE_CHARS = "a" * 65


def _first_line(text: str) -> str:
    return text.strip().splitlines()[0] if text.strip() else ""


class TestCommitTracking:
    """`task stat` commit-flag behaviour against the shipped binary."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("session-store-migration")
        self.sprint_id = self.test.create_sprint(
            self.roadmap,
            "Move session tokens to the encrypted store and close the audit finding",
            title="Session storage compliance",
        )

    def teardown_method(self):
        self.test.teardown()

    # -- fixture helpers ---------------------------------------------------

    def _new_task(self, title: str) -> int:
        """Create a task, put it in the sprint, and return its id."""
        task_id = self.test.create_task(
            self.roadmap,
            title=title,
            functional_requirements="Session tokens are never written in clear text.",
            technical_requirements="Route every write through the compliance store.",
            acceptance_criteria="No clear-text token remains in the sessions table.",
        )
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)
        ])
        return task_id

    def _start_sprint(self):
        """Open the sprint. Called exactly once per test, after the tasks are in
        it, so a non-zero exit here is a real failure and is not tolerated."""
        self.test.run_cmd(["sprint", "start", "-r", self.roadmap, str(self.sprint_id)])

    def _stat(self, ids: str, status: str, *extra: str, check: bool = True):
        return self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap, ids, status] + list(extra), check=check
        )

    def _task(self, task_id: int) -> dict:
        return self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(task_id)])[0]

    def _commits(self, task_id: int):
        task = self._task(task_id)
        return task["commit_open"], task["commit_close"]

    def _snapshot(self, *task_ids: int) -> dict:
        """Every field a status transition may touch, for the no-change checks."""
        fields = ("status", "started_at", "tested_at", "closed_at",
                  "completion_summary", "commit_open", "commit_close")
        return {
            task_id: {f: self._task(task_id)[f] for f in fields}
            for task_id in task_ids
        }

    def _walk_to_completed(self, task_id: int):
        self._stat(str(task_id), "DOING", "--commit-open", OPEN_FIRST)
        self._stat(str(task_id), "TESTING")
        self._stat(str(task_id), "COMPLETED", "--commit-close", CLOSE_HASH,
                   "--summary", "Every session row now round-trips through the store.")

    def _assert_rejected(self, args, want_code, want_line, unchanged_ids):
        """Run a rejected `task stat` and prove it changed nothing."""
        before = self._snapshot(*unchanged_ids)
        exit_code, stdout, stderr = self.test.run_cmd(
            ["task", "stat", "-r", self.roadmap] + args, check=False
        )
        assert exit_code == want_code, (
            f"rmp task stat {' '.join(args)}: exit {exit_code}, want {want_code}\n"
            f"stderr: {stderr}"
        )
        assert _first_line(stderr) == want_line, (
            f"rmp task stat {' '.join(args)}:\n  got  {_first_line(stderr)!r}\n"
            f"  want {want_line!r} (SPEC/COMMANDS.md § Change Status (stat), error table)"
        )
        assert stdout.strip() == "", f"a rejected command wrote to stdout: {stdout!r}"
        after = self._snapshot(*unchanged_ids)
        assert after == before, (
            f"rmp task stat {' '.join(args)} changed state despite being rejected\n"
            f"  before: {before}\n  after:  {after}"
        )

    # -- 1 & 2: the flag is mandatory --------------------------------------

    def test_doing_without_commit_open_is_rejected(self):
        task_id = self._new_task("Rotate the session signing secret")
        self._start_sprint()

        self._assert_rejected(
            [str(task_id), "DOING"], EXIT_INVALID,
            "Error: --commit-open is required when transitioning to DOING", [task_id],
        )

        # The re-entry route is policed too, not only the first entry.
        self._stat(str(task_id), "DOING", "--commit-open", OPEN_FIRST)
        self._stat(str(task_id), "TESTING")
        self._assert_rejected(
            [str(task_id), "DOING"], EXIT_INVALID,
            "Error: --commit-open is required when transitioning to DOING", [task_id],
        )
        print("✓ every transition into DOING requires --commit-open")

    def test_completed_without_commit_close_is_rejected(self):
        task_id = self._new_task("Backfill the encrypted session rows")
        self._start_sprint()
        self._stat(str(task_id), "DOING", "--commit-open", OPEN_FIRST)
        self._stat(str(task_id), "TESTING")

        self._assert_rejected(
            [str(task_id), "COMPLETED"], EXIT_INVALID,
            "Error: --commit-close is required when transitioning to COMPLETED", [task_id],
        )
        # A summary does not stand in for the hash.
        self._assert_rejected(
            [str(task_id), "COMPLETED", "--summary", "Backfill finished."], EXIT_INVALID,
            "Error: --commit-close is required when transitioning to COMPLETED", [task_id],
        )
        print("✓ the transition into COMPLETED requires --commit-close")

    # -- 3 & 4: rejected on any other target state -------------------------

    def test_commit_flag_on_the_wrong_target_state_is_rejected(self):
        in_sprint = self._new_task("Publish the rotation runbook")
        in_doing = self._new_task("Wire the compliance store client")
        in_testing = self._new_task("Verify the migration on a restored dump")
        self._start_sprint()
        self._stat(str(in_doing), "DOING", "--commit-open", OPEN_FIRST)
        self._stat(str(in_testing), "DOING", "--commit-open", OPEN_FIRST)
        self._stat(str(in_testing), "TESTING")

        open_line = "Error: --commit-open flag is only allowed when transitioning to DOING"
        close_line = "Error: --commit-close flag is only allowed when transitioning to COMPLETED"

        for task_id, target, flag, line in [
            (in_sprint, "BACKLOG", "--commit-open", open_line),
            (in_doing, "TESTING", "--commit-open", open_line),
            (in_testing, "COMPLETED", "--commit-open", open_line),
            (in_doing, "TESTING", "-co", open_line),
            (in_sprint, "BACKLOG", "--commit-close", close_line),
            (in_sprint, "DOING", "--commit-close", close_line),
            (in_doing, "TESTING", "--commit-close", close_line),
            (in_sprint, "DOING", "-cc", close_line),
        ]:
            self._assert_rejected(
                [str(task_id), target, flag, OPEN_FIRST], EXIT_INVALID, line, [task_id],
            )
        print("✓ each commit flag is refused on every target state but its own")

    # -- 5: format validation ----------------------------------------------

    def test_malformed_commit_hash_is_rejected(self):
        in_sprint = self._new_task("Drop the legacy clear-text column")
        in_testing = self._new_task("Re-point the session reader at the store")
        self._start_sprint()
        self._stat(str(in_testing), "DOING", "--commit-open", OPEN_FIRST)
        self._stat(str(in_testing), "TESTING")

        for value in (SIX_CHARS, SIXTY_FIVE_CHARS, "", "zzzzzzz", "refs/heads/main", " 4f5ba9b"):
            for task_id, target, flag in (
                (in_sprint, "DOING", "--commit-open"),
                (in_testing, "COMPLETED", "--commit-close"),
            ):
                self._assert_rejected(
                    [str(task_id), target, flag, value], EXIT_INVALID,
                    f'Error: invalid commit hash for {flag}: "{value}" '
                    f"(expected 7 to 64 hexadecimal characters)",
                    [task_id],
                )

        # The bounds themselves are inclusive: 7 and 64 characters are accepted.
        self._stat(str(in_sprint), "DOING", "--commit-open", "a" * 7)
        assert self._commits(in_sprint)[0] == "a" * 7
        self._stat(str(in_sprint), "TESTING")
        self._stat(str(in_sprint), "DOING", "--commit-open", "b" * 64)
        assert self._commits(in_sprint)[0] == "b" * 64
        print("✓ a malformed hash is refused and the 7..64 bounds are inclusive")

    # -- 6: no value after the flag ----------------------------------------

    def test_commit_flag_with_no_value_is_a_misuse(self):
        in_sprint = self._new_task("Retire the dual-write shim")
        in_testing = self._new_task("Confirm the read path never falls back")
        self._start_sprint()
        self._stat(str(in_testing), "DOING", "--commit-open", OPEN_FIRST)
        self._stat(str(in_testing), "TESTING")

        for task_id, target, flag, line in [
            (in_sprint, "DOING", "--commit-open", "Error: --commit-open requires a value"),
            (in_sprint, "DOING", "-co", "Error: --commit-open requires a value"),
            (in_testing, "COMPLETED", "--commit-close", "Error: --commit-close requires a value"),
            (in_testing, "COMPLETED", "-cc", "Error: --commit-close requires a value"),
        ]:
            self._assert_rejected([str(task_id), target, flag], EXIT_MISUSE, line, [task_id])
        print("✓ a commit flag with no value after it is exit 2, not exit 6")

    # -- 7 to 10: what gets written ----------------------------------------

    def test_the_stored_hash_is_lowercased(self):
        task_id = self._new_task("Rotate the acquirer webhook secret")
        self._start_sprint()

        self._stat(str(task_id), "DOING", "--commit-open", "4F5BA9BCDEF")
        assert self._commits(task_id) == ("4f5ba9bcdef", None), self._commits(task_id)

        self._stat(str(task_id), "TESTING")
        self._stat(str(task_id), "COMPLETED", "--commit-close", "4C4CCEA")
        assert self._commits(task_id) == ("4f5ba9bcdef", "4c4ccea"), self._commits(task_id)
        print("✓ an uppercase hash is stored lowercase in both columns")

    def test_re_entry_into_doing_replaces_commit_open(self):
        task_id = self._new_task("Fix the token expiry boundary")
        self._start_sprint()

        self._stat(str(task_id), "DOING", "--commit-open", OPEN_FIRST)
        assert self._commits(task_id)[0] == OPEN_FIRST
        self._stat(str(task_id), "TESTING")
        self._stat(str(task_id), "DOING", "-co", OPEN_SECOND)

        assert self._commits(task_id) == (OPEN_SECOND, None), (
            "TESTING -> DOING must REPLACE commit_open "
            "(SPEC/STATE_MACHINE.md § Commit Tracking Fields, rule 2)"
        )
        print("✓ a re-entry into DOING replaces the previous commit_open")

    def test_one_hash_applies_to_the_whole_batch(self):
        ids = [self._new_task(f"Shard the session table, phase {i}") for i in (1, 2, 3)]
        self._start_sprint()

        self._stat(",".join(str(i) for i in ids), "DOING", "--commit-open", OPEN_FIRST)
        for task_id in ids:
            assert self._commits(task_id) == (OPEN_FIRST, None), (
                f"task {task_id} did not receive the batch hash"
            )

        for task_id in ids:
            self._stat(str(task_id), "TESTING")
        self._stat(",".join(str(i) for i in ids), "COMPLETED", "-cc", CLOSE_HASH)
        for task_id in ids:
            assert self._commits(task_id) == (OPEN_FIRST, CLOSE_HASH)
        print("✓ every task of a batch receives the same supplied hash")

    # -- 11 to 14: the asymmetric clearing, all four routes ----------------

    def _assert_cleared_asymmetrically(self, task_id: int, route: str):
        task = self._task(task_id)
        assert task["status"] == "BACKLOG", f"{route}: status {task['status']}, want BACKLOG"
        assert task["commit_close"] is None, (
            f"{route}: commit_close = {task['commit_close']!r}, want null; every return to "
            "BACKLOG clears it (SPEC/STATE_MACHINE.md § Commit Tracking Fields, rule 4)"
        )
        assert task["commit_open"] == OPEN_FIRST, (
            f"{route}: commit_open = {task['commit_open']!r}, want {OPEN_FIRST!r} preserved; no "
            "route back to BACKLOG clears it (rule 5) — this is where it parts company with the "
            "lifecycle timestamps"
        )
        for field in ("started_at", "tested_at", "closed_at", "completion_summary"):
            assert task[field] is None, f"{route}: {field} = {task[field]!r}, want null"

    def test_task_stat_backlog_clears_only_commit_close(self):
        task_id = self._new_task("Reopen the storage finding for rework")
        self._start_sprint()
        self._walk_to_completed(task_id)

        self._stat(str(task_id), "BACKLOG")
        self._assert_cleared_asymmetrically(task_id, "task stat <id> BACKLOG")
        print("✓ task stat BACKLOG clears commit_close and preserves commit_open")

    def test_task_reopen_clears_only_commit_close(self):
        completed = self._new_task("Re-run the migration on the replica")
        doing = self._new_task("Trace the residual clear-text writes")
        self._start_sprint()
        self._walk_to_completed(completed)
        self._stat(str(doing), "DOING", "--commit-open", OPEN_FIRST)

        self.test.run_cmd(["task", "reopen", "-r", self.roadmap, f"{completed},{doing}"])
        self._assert_cleared_asymmetrically(completed, "task reopen (from COMPLETED)")
        self._assert_cleared_asymmetrically(doing, "task reopen (from DOING)")
        print("✓ task reopen clears commit_close and preserves commit_open")

    def test_sprint_remove_tasks_clears_only_commit_close(self):
        completed = self._new_task("Archive the pre-migration dump")
        testing = self._new_task("Check the replica lag during the backfill")
        self._start_sprint()
        self._walk_to_completed(completed)
        self._stat(str(testing), "DOING", "--commit-open", OPEN_FIRST)
        self._stat(str(testing), "TESTING")

        self.test.run_cmd([
            "sprint", "remove-tasks", "-r", self.roadmap, str(self.sprint_id),
            f"{completed},{testing}",
        ])
        self._assert_cleared_asymmetrically(completed, "sprint remove-tasks (from COMPLETED)")
        self._assert_cleared_asymmetrically(testing, "sprint remove-tasks (from TESTING)")
        print("✓ sprint remove-tasks clears commit_close and preserves commit_open")

    def test_sprint_remove_clears_only_commit_close(self):
        completed = self._new_task("Decommission the legacy session cache")
        testing = self._new_task("Measure the store's p99 read latency")
        doing = self._new_task("Document the rollback procedure")
        self._start_sprint()
        self._walk_to_completed(completed)
        self._stat(str(testing), "DOING", "--commit-open", OPEN_FIRST)
        self._stat(str(testing), "TESTING")
        self._stat(str(doing), "DOING", "--commit-open", OPEN_FIRST)

        self.test.run_cmd(["sprint", "remove", "-r", self.roadmap, str(self.sprint_id)])
        self._assert_cleared_asymmetrically(completed, "sprint remove (from COMPLETED)")
        self._assert_cleared_asymmetrically(testing, "sprint remove (from TESTING)")
        self._assert_cleared_asymmetrically(doing, "sprint remove (from DOING)")
        print("✓ sprint remove clears commit_close and preserves commit_open")

    # -- 15: batch atomicity -----------------------------------------------

    def test_a_failing_id_leaves_the_whole_batch_unchanged(self):
        eligible = [self._new_task(f"Encrypt the token column, step {i}") for i in (1, 2, 3)]
        ineligible = self._new_task("Draft the compliance sign-off note")
        self._start_sprint()
        # Send the fourth task back to BACKLOG, from where DOING is refused.
        self._stat(str(ineligible), "BACKLOG")

        every_id = eligible + [ineligible]
        csv_eligible = ",".join(str(i) for i in eligible)
        csv_every = ",".join(str(i) for i in every_id)

        # Rejected at step 4, before the database is opened.
        self._assert_rejected(
            [csv_eligible, "DOING"], EXIT_INVALID,
            "Error: --commit-open is required when transitioning to DOING", eligible,
        )
        self._assert_rejected(
            [csv_eligible, "DOING", "--commit-open", "not-a-hash"], EXIT_INVALID,
            'Error: invalid commit hash for --commit-open: "not-a-hash" '
            "(expected 7 to 64 hexadecimal characters)", eligible,
        )

        # Rejected at step 6, with the database open and the batch already read.
        before = self._snapshot(*every_id)
        exit_code, _, stderr = self._stat(csv_every, "DOING", "--commit-open", OPEN_FIRST,
                                          check=False)
        assert exit_code == EXIT_INVALID, f"exit {exit_code}, want {EXIT_INVALID}: {stderr}"
        assert self._snapshot(*every_id) == before, (
            "one ineligible ID must leave EVERY task of the batch untouched, including the "
            "eligible ones (SPEC/COMMANDS.md § Change Status (stat), Validation Order)"
        )

        # Control: the same batch minus the ineligible ID does move, so the
        # assertions above are not passing merely because nothing ever moves.
        self._stat(csv_eligible, "DOING", "--commit-open", OPEN_FIRST)
        for task_id in eligible:
            assert self._commits(task_id) == (OPEN_FIRST, None)
        print("✓ a batch that would partly fail changes nothing at all")

    # -- 16: scope boundary ------------------------------------------------

    def test_create_and_edit_accept_neither_flag(self):
        task_id = self._new_task("Confirm the flags are stat-only")

        for args in (
            ["task", "create", "-r", self.roadmap, "-t", "Ad-hoc", "-fr", "f", "-tr", "t",
             "-ac", "a", "--commit-open", OPEN_FIRST],
            ["task", "create", "-r", self.roadmap, "-t", "Ad-hoc", "-fr", "f", "-tr", "t",
             "-ac", "a", "--commit-close", CLOSE_HASH],
            ["task", "edit", "-r", self.roadmap, str(task_id), "--commit-open", OPEN_FIRST],
            ["task", "edit", "-r", self.roadmap, str(task_id), "--commit-close", CLOSE_HASH],
        ):
            exit_code, _, stderr = self.test.run_cmd(args, check=False)
            assert exit_code == EXIT_MISUSE, (
                f"rmp {' '.join(args)}: exit {exit_code}, want {EXIT_MISUSE}; neither flag exists "
                "on create or edit (SPEC/COMMANDS.md § Change Status (stat), Commit Tracking "
                f"Behavior)\nstderr: {stderr}"
            )
        print("✓ task create and task edit accept neither commit flag")




# tasksDDL110 is the `tasks` table exactly as schema 1.10.0 declared it: the
# shape shipped immediately before the two commit columns existed, taken
# verbatim from internal/db/schema.go at commit 1d0f66a. It is reproduced here
# rather than derived from the current schema, because a fixture computed from
# today's code cannot prove yesterday's database still opens.
TASKS_DDL_1_10_0 = """
CREATE TABLE tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL CHECK(length(title) <= 255),
    status TEXT NOT NULL DEFAULT 'BACKLOG' CHECK(status IN ('BACKLOG', 'SPRINT', 'DOING', 'TESTING', 'COMPLETED')),
    type TEXT NOT NULL DEFAULT 'TASK' CHECK(type IN ('USER_STORY', 'TASK', 'BUG', 'SUB_TASK', 'EPIC', 'REFACTOR', 'CHORE', 'SPIKE', 'DESIGN_UX', 'IMPROVEMENT')),
    functional_requirements TEXT NOT NULL CHECK(length(functional_requirements) <= 4096),
    technical_requirements TEXT NOT NULL CHECK(length(technical_requirements) <= 4096),
    acceptance_criteria TEXT NOT NULL CHECK(length(acceptance_criteria) <= 4096),
    created_at TEXT NOT NULL,
    started_at TEXT,
    tested_at TEXT,
    closed_at TEXT,
    completion_summary TEXT CHECK(completion_summary IS NULL OR length(completion_summary) <= 4096),
    parent_task_id INTEGER REFERENCES tasks(id),
    priority INTEGER NOT NULL DEFAULT 0 CHECK(priority >= 0 AND priority <= 9),
    severity INTEGER NOT NULL DEFAULT 0 CHECK(severity >= 0 AND severity <= 9)
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_type ON tasks(type);
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
CREATE INDEX IF NOT EXISTS idx_tasks_status_priority ON tasks(status, priority DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_priority_created ON tasks(priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_parent_task_id ON tasks(parent_task_id);
"""

# Hash shapes a real repository produces. The abbreviated and SHA-1 forms come
# from this project's own history; the SHA-256 form is the length a repository
# created with `git init --object-format=sha256` writes, which the 7..64 bound
# exists to admit.
ABBREVIATED_HASH = "391cff7"
SHA1_HASH = "5f93b518375f7f65df4f275f1ee9b2b2e2fd17f0"
SHA256_HASH = "9f2c" + "a1b3" * 14 + "7e5d"


class TestCommitHashShapes:
    """A hash is accepted at every length git actually emits, and comes back
    byte for byte. The 7..64 bound is not an arbitrary range: it spans the
    conventional abbreviation through a full SHA-256 object name."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("acquirer-settlement")
        self.sprint_id = self.test.create_sprint(
            self.roadmap,
            "Reconcile every settlement window against the acquirer report",
            title="Settlement reconciliation",
        )

    def teardown_method(self):
        self.test.teardown()

    def test_every_hash_length_git_emits_round_trips(self):
        assert len(SHA256_HASH) == 64, "the SHA-256 fixture must be a full object name"

        for label, opening, closing in [
            ("abbreviated", ABBREVIATED_HASH, "2578d18"),
            ("full SHA-1", SHA1_HASH, "6742fc4a81de9ce610fd9d0803344bf8f4ceb86d"),
            ("full SHA-256", SHA256_HASH, SHA256_HASH),
        ]:
            task_id = self.test.create_task(
                self.roadmap,
                title=f"Reconcile the {label} settlement window",
                functional_requirements="Every settlement window balances against the acquirer report.",
                technical_requirements="Compare both ledgers per window and record the delta.",
                acceptance_criteria="An unbalanced window raises an alert within the hour.",
            )
            self.test.run_cmd([
                "sprint", "add-tasks", "-r", self.roadmap, str(self.sprint_id), str(task_id)
            ])
            if label == "abbreviated":
                self.test.run_cmd(["sprint", "start", "-r", self.roadmap, str(self.sprint_id)])

            self.test.run_cmd([
                "task", "stat", "-r", self.roadmap, str(task_id), "DOING", "--commit-open", opening])
            self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(task_id), "TESTING"])
            self.test.run_cmd([
                "task", "stat", "-r", self.roadmap, str(task_id), "COMPLETED", "--commit-close", closing])

            task = self.test.run_cmd_json(
                ["task", "get", "-r", self.roadmap, str(task_id)])[0]
            assert task["commit_open"] == opening, (
                f"{label}: commit_open came back as {task['commit_open']!r}, not the {len(opening)}-character "
                f"hash that was written")
            assert task["commit_close"] == closing, (
                f"{label}: commit_close came back as {task['commit_close']!r}, not the {len(closing)}-character "
                f"hash that was written")
        print("✓ abbreviated, full SHA-1 and full SHA-256 hashes all round-trip unchanged")


class TestCommitColumnsMigration:
    """A database written by the previous release opens, gains both columns and
    keeps its rows (SPEC/VERSION.md § Migration 1.10.0 -> 1.11.0).

    The property that matters is not that the migration runs — it is that a
    MIGRATED database and a FRESH one behave identically afterwards. `ALTER
    TABLE ... ADD COLUMN` can only append, so the two tables declare the columns
    in different positions and their DDL text will never match; what must match
    is what they accept and reject."""

    ROADMAP = "regional-clearing-ledger"

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def _db_path(self, roadmap):
        return self.test.roadmaps_dir / roadmap / "project.db"

    def _schema_version(self, roadmap):
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            row = con.execute(
                "SELECT value FROM _metadata WHERE key = 'schema_version'").fetchone()
            return row[0] if row else None
        finally:
            con.close()

    def _build_1_10_0_fixture(self, roadmap):
        """Create a roadmap through the CLI, then rebuild `tasks` to its verbatim
        1.10.0 shape, seed it with rows the old binary could have written, and
        roll the recorded schema_version back. Returns the seeded ids."""
        self.test.create_roadmap(roadmap)
        db_path = self._db_path(roadmap)
        assert db_path.exists(), f"precondition: the CLI must have created {db_path}"

        con = sqlite3.connect(str(db_path))
        try:
            # The table is still empty, so the drop cannot cascade.
            con.execute("DROP TABLE tasks")
            con.executescript(TASKS_DDL_1_10_0)

            now = "2026-02-17T11:20:00.000Z"
            insert = (
                "INSERT INTO tasks (title, status, type, functional_requirements, "
                "technical_requirements, acceptance_criteria, created_at, started_at, "
                "closed_at, completion_summary, priority, severity) "
                "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
            )

            def seed(title, status, started, closed, summary, priority):
                cur = con.execute(insert, (
                    title, status, "TASK",
                    "Clearing batches reconcile against the central ledger to the cent.",
                    "Stream each batch and compare running totals against the ledger.",
                    "A divergent batch is quarantined before settlement.",
                    now, started, closed, summary, priority, 0,
                ))
                return cur.lastrowid

            backlog_id = seed(
                "Quarantine divergent clearing batches before settlement",
                "BACKLOG", None, None, None, 7)
            # A task the old binary had already carried to COMPLETED. It is the
            # row that proves the migration preserves data it cannot backfill:
            # the work was done at some commit, but nothing recorded which.
            completed_id = seed(
                "Stream the nightly clearing batch instead of buffering it",
                "COMPLETED", now, now, "Replaced the buffer with a streaming reader.", 5)

            con.execute("UPDATE _metadata SET value = '1.10.0' WHERE key = 'schema_version'")
            con.commit()
        finally:
            con.close()

        return backlog_id, completed_id

    def test_a_1_10_0_database_gains_both_columns_and_keeps_its_rows(self):
        backlog_id, completed_id = self._build_1_10_0_fixture(self.ROADMAP)
        assert self._schema_version(self.ROADMAP) == "1.10.0", "fixture must start at 1.10.0"

        # The next command against the roadmap runs the migration chain.
        tasks = self.test.run_cmd_json(["task", "list", "-r", self.ROADMAP])

        # Compared against a freshly created roadmap rather than a literal:
        # opening a 1.10.0 database runs the WHOLE chain, so it lands on
        # whatever the newest version is. What must hold is that a migrated
        # database and a fresh one agree — a claim that does not rot on the
        # next schema bump.
        fresh = self.test.create_roadmap()
        assert self._schema_version(self.ROADMAP) == self._schema_version(fresh), (
            "a migrated database must reach the same schema version a fresh one is stamped with")

        by_id = {t["id"]: t for t in tasks}
        assert set(by_id) == {backlog_id, completed_id}, (
            f"the migration lost or invented rows: {sorted(by_id)}")
        for task_id in (backlog_id, completed_id):
            assert by_id[task_id]["commit_open"] is None, (
                f"task {task_id} must migrate with commit_open null; there is nothing to backfill it from")
            assert by_id[task_id]["commit_close"] is None, (
                f"task {task_id} must migrate with commit_close null")
        assert by_id[completed_id]["completion_summary"] == (
            "Replaced the buffer with a streaming reader."), "the migration must not disturb existing data"
        print("✓ a 1.10.0 database migrates, gains both columns null, and keeps every row")

    def test_the_migrated_table_enforces_the_same_commit_rule_as_a_fresh_one(self):
        self._build_1_10_0_fixture(self.ROADMAP)
        self.test.run_cmd(["task", "list", "-r", self.ROADMAP])  # triggers the migration
        fresh = self.test.create_roadmap()

        # Both databases are driven through the SAME command with the SAME
        # values, and must answer identically. This half exercises the COMMAND
        # layer, which validates before any write; the storage CHECK is probed
        # separately below, because a command-level assertion alone would pass
        # even on a migration that added the columns without their constraint.
        for roadmap in (self.ROADMAP, fresh):
            task_id = self.test.create_task(
                roadmap,
                title="Verify the clearing reconciliation after migration",
                functional_requirements="The clearing ledger reconciles after a schema migration.",
                technical_requirements="Re-run the reconciliation against the migrated database.",
                acceptance_criteria="The totals match the pre-migration report.",
            )
            sprint_id = self.test.create_sprint(
                roadmap, "Confirm the ledger survives the schema migration",
                title="Post-migration verification")
            self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)])
            self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

            exit_code, _, stderr = self.test.run_cmd(
                ["task", "stat", "-r", roadmap, str(task_id), "DOING", "--commit-open", "nothex!"],
                check=False)
            assert exit_code == EXIT_INVALID, (
                f"{roadmap}: a malformed hash must be rejected with exit {EXIT_INVALID}, "
                f"got {exit_code}")
            assert _first_line(stderr) == (
                'Error: invalid commit hash for --commit-open: "nothex!" '
                "(expected 7 to 64 hexadecimal characters)"), (
                f"{roadmap}: unexpected rejection message: {_first_line(stderr)!r}")

            self.test.run_cmd([
                "task", "stat", "-r", roadmap, str(task_id), "DOING", "--commit-open", SHA1_HASH])
            task = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])[0]
            assert task["commit_open"] == SHA1_HASH, (
                f"{roadmap}: a valid hash must be stored; got {task['commit_open']!r}")

            # The storage backstop, reached under the CLI. ALTER TABLE ADD COLUMN
            # accepts a CHECK clause, but nothing forces a migration to include
            # one, and a migration that omitted it would leave the constraint on
            # fresh databases only. Every assertion above would still pass.
            self._assert_check_rejects(roadmap, task_id)
        print("✓ a migrated table and a fresh one accept and reject exactly the same hashes, "
              "at the command layer and at the storage layer")

    def _assert_check_rejects(self, roadmap, task_id):
        """Write straight to SQLite, under the binary, and require the column
        constraint itself to refuse the values the SPEC puts out of bounds."""
        con = sqlite3.connect(str(self._db_path(roadmap)))
        try:
            for column in ("commit_open", "commit_close"):
                for value in ("nothex!", "A" * 40, "a" * 6, "a" * 65):
                    try:
                        con.execute(
                            f"UPDATE tasks SET {column} = ? WHERE id = ?", (value, task_id))
                    except sqlite3.IntegrityError:
                        continue
                    raise AssertionError(
                        f"{roadmap}: the {column} CHECK accepted {value!r}; the migrated and the "
                        f"fresh schema must both carry the constraint")
            con.rollback()
        finally:
            con.close()


def _run_all():
    passed = 0
    failed = 0
    failures = []
    # Every class in the module, so a suite added below the runner is not
    # silently skipped — which is exactly what happened when the migration and
    # hash-shape suites were first appended.
    for cls in (TestCommitTracking, TestCommitHashShapes, TestCommitColumnsMigration):
        for name in sorted(m for m in dir(cls) if m.startswith("test_")):
            label = f"{cls.__name__}.{name}"
            instance = cls()
            instance.setup_method()
            try:
                getattr(instance, name)()
                passed += 1
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label} (error)")
            finally:
                instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Commit tracking tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
