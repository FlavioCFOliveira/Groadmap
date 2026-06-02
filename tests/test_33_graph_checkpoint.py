#!/usr/bin/env python3
"""
Test 33: rmp graph command and Synchronous Checkpoint on Write.

This suite is the end-to-end backstop for the knowledge-graph command
family and for its persistence contract, specified in
SPEC/GRAPH.md (functional requirements, guard rails, and
"§ Synchronous Checkpoint on Write" FR6/FR7) and
SPEC/IMPLEMENTATION.md § Synchronous Checkpoint on Write.

Persistence contract under test:
- Every successful write subcommand (create/update/delete) commits to
  the GoGraph write-ahead log and then, synchronously and before exit,
  writes a self-sufficient on-disk snapshot under
  ~/.roadmaps/<name>/graph/snapshot/ and truncates the WAL. The WAL
  therefore holds only post-snapshot transactions and never grows
  without bound.
- Startup/recovery reconstructs the graph from the snapshot plus any
  WAL tail (verified by reading back in a fresh process while the WAL
  is empty: the data can only come from the snapshot).
- FR7 failure policy: a checkpoint that fails AFTER the transaction has
  committed durably MUST NOT fail the user-visible write. The command
  still returns success (exit 0) with a diagnostic on stderr, the WAL
  is left intact (so recovery still works), and the next successful
  write reconciles the snapshot and truncates the WAL.
- Read subcommands (query/search) never checkpoint.

Coverage also includes baseline graph behaviour that had no prior E2E
test: create/query/update/delete/search round-trips, guard-rail
rejection, stdin input, and the error exit codes (no roadmap = 3,
unknown roadmap = 4, no query = 2, guard-rail mismatch = 6).
"""

import os
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXIT_OK = 0
EXIT_NO_QUERY = 2
EXIT_NO_ROADMAP = 3
EXIT_NOT_FOUND = 4
EXIT_GUARD_RAIL = 6


class TestGraphCheckpoint:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        # A failed assertion may leave the snapshot directory read-only
        # (0500) from a checkpoint-failure test; restore writable perms
        # on every directory so shutil.rmtree can clean up.
        try:
            root = self.test.roadmaps_dir
            if root and os.path.exists(root):
                for dirpath, _dirnames, _filenames in os.walk(root):
                    os.chmod(dirpath, 0o700)
        except OSError:
            pass
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def graph_dir(self) -> Path:
        return Path(self.test.roadmaps_dir) / self.roadmap / "graph"

    def snapshot_dir(self) -> Path:
        return self.graph_dir() / "snapshot"

    def wal_size(self):
        wal = self.graph_dir() / "wal"
        return wal.stat().st_size if wal.exists() else None

    def write(self, subcmd: str, query: str, check: bool = True):
        return self.test.run_cmd(["graph", subcmd, "-r", self.roadmap, "--query", query], check=check)

    def write_json(self, subcmd: str, query: str):
        return self.test.run_cmd_json(["graph", subcmd, "-r", self.roadmap, "--query", query])

    def query_json(self, query: str, subcmd: str = "query"):
        return self.test.run_cmd_json(["graph", subcmd, "-r", self.roadmap, "--query", query])

    @staticmethod
    def rows_by(result, key_col):
        """Index {columns, rows} output by the value of one column."""
        cols = result["columns"]
        idx = cols.index(key_col)
        return {row[idx]: row for row in result["rows"]}

    def seed_knowledge_graph(self):
        """A small, realistic project knowledge graph."""
        self.write("create",
                   "CREATE (s:Spec {key:'authentication', title:'User Authentication', status:'approved'})")
        self.write("create",
                   "CREATE (t:Task {key:'login-flow', title:'Implement login flow', status:'in_progress'})")
        self.write("create",
                   "CREATE (d:Decision {key:'use-jwt', rationale:'Stateless sessions scale horizontally'})")

    # ---- baseline graph behaviour ------------------------------------

    def test_create_and_query_roundtrip(self):
        ok = self.write_json("create",
                             "CREATE (s:Spec {key:'authentication', title:'User Authentication'})")
        assert ok == {"ok": True}, f"create without RETURN must emit {{'ok': true}}, got {ok!r}"
        result = self.query_json("MATCH (s:Spec) RETURN s.key, s.title")
        by_key = self.rows_by(result, "s.key")
        assert "authentication" in by_key, f"created Spec not found on read-back: {result!r}"
        title_idx = result["columns"].index("s.title")
        assert by_key["authentication"][title_idx] == "User Authentication"

    def test_update_mutates_property(self):
        self.write("create", "CREATE (t:Task {key:'login-flow', status:'in_progress'})")
        self.write("update", "MATCH (t:Task {key:'login-flow'}) SET t.status='done'")
        result = self.query_json("MATCH (t:Task {key:'login-flow'}) RETURN t.status")
        assert result["rows"] == [["done"]], f"update did not persist new status: {result!r}"

    def test_delete_removes_node(self):
        self.write("create", "CREATE (d:Decision {key:'use-jwt'})")
        before = self.query_json("MATCH (d:Decision) RETURN count(d)")["rows"][0][0]
        assert before == 1
        self.write("delete", "MATCH (d:Decision {key:'use-jwt'}) DETACH DELETE d")
        after = self.query_json("MATCH (d:Decision) RETURN count(d)")["rows"][0][0]
        assert after == 0, f"delete did not remove the node: count={after}"

    def test_search_variable_length_path(self):
        self.write("create",
                   "CREATE (s:Spec {key:'authentication'})-[:HAS_TASK]->(t:Task {key:'login-flow'})")
        result = self.query_json("MATCH p=(s:Spec)-[*1..2]-(b) RETURN b.key", subcmd="search")
        assert ["login-flow"] in result["rows"], f"variable-length search failed: {result!r}"

    def test_query_rejects_writing_clause(self):
        # graph query must reject a CREATE (guard-rail), exit code 6.
        self.test.assert_exit_code(
            ["graph", "query", "-r", self.roadmap, "--query", "CREATE (n:Spec {key:'x'})"],
            EXIT_GUARD_RAIL,
        )

    def test_create_rejects_delete_clause(self):
        self.test.assert_exit_code(
            ["graph", "create", "-r", self.roadmap, "--query", "MATCH (n) DETACH DELETE n"],
            EXIT_GUARD_RAIL,
        )

    def test_query_from_stdin(self):
        self.write("create", "CREATE (s:Spec {key:'authentication'})")
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        proc = subprocess.run(
            [self.test.cli_path, "graph", "query", "-r", self.roadmap],
            input="MATCH (n) RETURN count(n)",
            capture_output=True, text=True, env=env,
        )
        assert proc.returncode == EXIT_OK, f"stdin query failed: {proc.stderr}"
        assert '"count(n)"' in proc.stdout and "1" in proc.stdout, proc.stdout

    def test_missing_roadmap_flag_exits_3(self):
        self.test.assert_exit_code(
            ["graph", "query", "--query", "MATCH (n) RETURN n"], EXIT_NO_ROADMAP)

    def test_unknown_roadmap_exits_4(self):
        self.test.assert_exit_code(
            ["graph", "query", "-r", "no_such_roadmap_xyz", "--query", "MATCH (n) RETURN n"],
            EXIT_NOT_FOUND)

    def test_empty_query_exits_2(self):
        self.test.assert_exit_code(
            ["graph", "query", "-r", self.roadmap, "--query", ""], EXIT_NO_QUERY)

    # ---- synchronous checkpoint on write -----------------------------

    def test_snapshot_created_after_first_write(self):
        assert not self.snapshot_dir().exists(), "snapshot/ must not exist before any write"
        self.write("create", "CREATE (s:Spec {key:'authentication'})")
        manifest = self.snapshot_dir() / "manifest.json"
        mapper = self.snapshot_dir() / "mapper.bin"
        assert manifest.is_file(), "checkpoint must produce snapshot/manifest.json after a write"
        assert mapper.is_file(), (
            "snapshot must carry mapper.bin (self-sufficient) so the WAL can be truncated safely")

    def test_wal_truncated_after_write(self):
        self.write("create", "CREATE (s:Spec {key:'authentication'})")
        assert self.wal_size() == 0, (
            f"WAL must be truncated to 0 after the post-commit checkpoint, got {self.wal_size()}")

    def test_wal_stays_bounded_across_writes(self):
        # Without checkpointing the WAL would grow monotonically with
        # every write. With it, each write truncates back to 0.
        for i in range(1, 8):
            self.write("create", f"CREATE (t:Task {{key:'task-{i}'}})")
            assert self.wal_size() == 0, (
                f"WAL grew unbounded after write {i}: size={self.wal_size()} (checkpoint not truncating)")

    def test_durability_across_processes_from_snapshot(self):
        self.seed_knowledge_graph()
        self.write("update", "MATCH (s:Spec {key:'authentication'}) SET s.status='shipped'")
        # WAL is empty after the last checkpoint, so a read in a fresh
        # process can only reconstruct labels + properties from the snapshot.
        assert self.wal_size() == 0, "precondition: WAL truncated after last write"
        result = self.query_json(
            "MATCH (n) RETURN n.key, n.status, n.rationale, n.title ORDER BY n.key")
        by_key = self.rows_by(result, "n.key")
        assert set(by_key) == {"authentication", "login-flow", "use-jwt"}, (
            f"snapshot lost nodes: {sorted(by_key)}")
        status_idx = result["columns"].index("n.status")
        rationale_idx = result["columns"].index("n.rationale")
        assert by_key["authentication"][status_idx] == "shipped", "snapshot lost updated property"
        assert by_key["use-jwt"][rationale_idx] == "Stateless sessions scale horizontally", (
            "snapshot lost a string property")

    def test_delete_also_checkpoints(self):
        self.write("create", "CREATE (d:Decision {key:'use-jwt'})")
        self.write("delete", "MATCH (d:Decision {key:'use-jwt'}) DETACH DELETE d")
        assert self.wal_size() == 0, (
            f"delete must checkpoint and truncate the WAL, got {self.wal_size()}")

    def _run_with_unwritable_snapshot(self, subcmd, query):
        """Force a post-commit checkpoint failure by making snapshot/
        read-only, then restore perms. Returns (exit_code, stdout, stderr)."""
        snap = self.snapshot_dir()
        snap.chmod(0o500)
        try:
            return self.test.run_cmd(
                ["graph", subcmd, "-r", self.roadmap, "--query", query], check=False)
        finally:
            snap.chmod(0o700)

    def test_checkpoint_failure_is_non_fatal(self):
        # First successful write creates the snapshot dir.
        self.write("create", "CREATE (s:Spec {key:'authentication'})")
        assert self.wal_size() == 0
        # Now force the checkpoint to fail after a durable commit.
        code, stdout, stderr = self._run_with_unwritable_snapshot(
            "create", "CREATE (d:Decision {key:'use-jwt', rationale:'stateless'})")
        assert code == EXIT_OK, f"checkpoint failure must NOT fail the write (FR7); exit={code}"
        assert '"ok": true' in stdout, f"success JSON must still be emitted; stdout={stdout!r}"
        assert "checkpoint" in stderr.lower(), f"a checkpoint diagnostic must go to stderr; stderr={stderr!r}"
        # WAL was NOT truncated: the committed transaction is retained so
        # recovery still works.
        assert self.wal_size() and self.wal_size() > 0, (
            "WAL must stay intact when the checkpoint fails (durability via WAL)")
        # The committed node is recoverable from snapshot + WAL tail.
        result = self.query_json("MATCH (d:Decision {key:'use-jwt'}) RETURN d.key")
        assert result["rows"] == [["use-jwt"]], "committed node lost after a non-fatal checkpoint failure"

    def test_read_does_not_checkpoint(self):
        self.write("create", "CREATE (s:Spec {key:'authentication'})")
        self._run_with_unwritable_snapshot("create", "CREATE (d:Decision {key:'use-jwt'})")
        wal_after_failed_write = self.wal_size()
        assert wal_after_failed_write and wal_after_failed_write > 0, "precondition: WAL has the orphan tx"
        # A read must not checkpoint, so it must not truncate the WAL.
        self.query_json("MATCH (n) RETURN count(n)")
        assert self.wal_size() == wal_after_failed_write, (
            "a read subcommand must never checkpoint/truncate the WAL")

    def test_checkpoint_reconciles_after_failure(self):
        self.write("create", "CREATE (s:Spec {key:'authentication'})")
        # Orphan transaction left only in the WAL by a failed checkpoint.
        self._run_with_unwritable_snapshot("create", "CREATE (d:Decision {key:'use-jwt'})")
        assert self.wal_size() and self.wal_size() > 0
        # The next successful write reconciles: it rewrites the snapshot
        # (absorbing the orphan tx) and truncates the WAL back to 0.
        self.write("create", "CREATE (t:Task {key:'login-flow'})")
        assert self.wal_size() == 0, "a successful write after a failure must reconcile and truncate the WAL"
        # All three nodes survive with the WAL empty => all are in the snapshot.
        result = self.query_json("MATCH (n) RETURN n.key ORDER BY n.key")
        keys = {row[0] for row in result["rows"]}
        assert keys == {"authentication", "login-flow", "use-jwt"}, (
            f"reconciled snapshot is missing nodes: {sorted(keys)}")


def _run_all():
    instance_cls = TestGraphCheckpoint
    method_names = [m for m in dir(instance_cls) if m.startswith("test_")]
    passed = 0
    failed = 0
    failures = []
    for m in method_names:
        instance = instance_cls()
        instance.setup_method()
        try:
            getattr(instance, m)()
            passed += 1
            print(f"✓ {m}")
        except AssertionError as exc:
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m} (error)")
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Graph checkpoint tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
