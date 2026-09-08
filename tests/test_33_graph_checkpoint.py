#!/usr/bin/env python3
"""
Test 33: the graph command family, and the persistence contract of a served graph.

This suite is the end-to-end backstop for the knowledge-graph command family and
for its persistence contract, specified in SPEC/GRAPH.md (functional
requirements, "§ Synchronous Checkpoint on Write" FR6/FR7, and "§ Durability and
Checkpointing in a Long-Lived Process") and in
SPEC/IMPLEMENTATION.md § Graph Store Concurrency.

HOW A GRAPH IS REACHED
----------------------
Through a running `rmp graph serve`, and through nothing else. `rmp graph serve`
is the only process that opens the store, `rmp graph client` is the only
subcommand that runs a statement against it, and with nothing listening the
client fails rather than opening anything: there is no direct path and no fall
back at any surface. Starting a server against a roadmap that has never had a
graph is what CREATES one, which is why every test below starts the server first
and seeds through the client afterwards.

WHERE THE CHECKPOINT WENT, AND WHAT DID NOT MOVE WITH IT
--------------------------------------------------------
An invocation no longer checkpoints, because an invocation no longer opens a
store. The fold lives in the server: it runs on the server's own cadence and, at
the shutdown that matters to a test, when the write-ahead log has grown since it
was last folded. So "every write rewrites the snapshot and truncates the log" is
no longer a property of a write, and this suite does not assert it. What survives
is the guarantee that sentence existed to protect, and it is asserted here in the
terms the server model puts it in:

- A write is durable when it is ACKNOWLEDGED. The transaction is committed to the
  write-ahead log before the client is answered, and the log alone is enough to
  recover it.
- The durable state a graph carries from one server to the next is what a server
  LEFT BEHIND when it stopped. Every durability assertion here therefore runs the
  writes through one server, stops it -- whose shutdown checkpoint folds the log
  into the snapshot and truncates it -- and reads the data back through a FRESH
  server process.
- The snapshot is self-sufficient: it carries the node-identifier-to-key mapper,
  the deletion tombstones and the registered schema, so truncating the log after
  it loses nothing committed.
- A statement whose transaction appended nothing to the log owes no fold. Neither
  `snapshot/` nor `wal` is touched on its account, and a shutdown that owes no
  fold writes nothing at all -- both halves asserted here by fingerprinting those
  bytes and comparing them.
- FR7 failure policy: a checkpoint that fails AFTER the commits it would fold are
  already durable must not fail anything. The server still exits 0, the failure is
  reported on the server's own stderr rather than to the caller whose write
  already succeeded, the log is left intact so recovery still works, and the next
  successful checkpoint reconciles the snapshot.

Coverage also includes the baseline behaviour of the family: create / read /
update / delete round-trips through the client, a variable-length traversal,
schema DDL and its introspection, a statement read from standard input, the
retired subcommand names, and the error exit codes (no roadmap = 3, unknown
roadmap = 4, no query = 2, nothing listening = 1).
"""

import hashlib
import os
import shutil
import sys
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, assert_graph_write_shape


EXIT_OK = 0
EXIT_NO_SERVER = 1
EXIT_NO_QUERY = 2
EXIT_NO_ROADMAP = 3
EXIT_NOT_FOUND = 4

# Every name `rmp graph` has ever published and no longer does. `execute` joins
# the five that preceded it: it ran statements directly against the store, and
# the store is now opened by `serve` alone. The list is not trusted on its own --
# test_retired_subcommand_names_exit_127 checks each name against the
# subcommands the CLI itself publishes, so a name that came back would be caught
# rather than silently driven as though it were still gone.
RETIRED_SUBCOMMAND_NAMES = ("create", "query", "update", "delete", "search", "execute")


class TestGraphCheckpoint:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # The server is started here rather than in each test because starting
        # one is what creates the graph: the roadmap above has never had one, so
        # this serves it empty and the first statement writes its first node
        # (SPEC/GRAPH.md § Server Startup, step 1).
        self.server = self.test.start_graph_server(self.roadmap)

    def teardown_method(self):
        # A failed assertion may leave the snapshot staging directory read-only
        # (0500) from a checkpoint-failure test; restore writable perms on every
        # directory so shutil.rmtree can clean up.
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

    # `write` and `query_json` both send one statement to the running server
    # through `rmp graph client`, which is the only subcommand that runs one.
    # They are kept as two names because what each CALL means is still worth
    # reading off the call site: `write` issues a statement that changes the
    # graph, `query_json` one that does not. The invocation is the same, and
    # both fail loudly on a statement that did not succeed.

    def write(self, query: str):
        return self.test.graph_ok(self.roadmap, query=query)

    def query_json(self, query: str):
        return self.test.graph_ok(self.roadmap, query=query)

    def restart_server(self):
        """Stop the running server -- whose shutdown checkpoint folds the log
        into the snapshot when the log has grown -- and start a fresh one over
        what it left behind. Returns the new server.

        The stop is asserted rather than assumed: a server that failed to shut
        down cleanly has not necessarily written the snapshot the caller is
        about to read from.
        """
        code = self.server.stop()
        assert code == EXIT_OK, (
            f"the graph server must exit 0 on SIGINT; exit={code} "
            f"stderr={self.server.stderr_text()!r}")
        self.server = self.test.start_graph_server(self.roadmap)
        return self.server

    def durable_fingerprint(self):
        """The bytes of the durable graph state: the write-ahead log and every
        file under `snapshot/`, each by content hash.

        The two lock files the server keeps beside them are its own runtime
        state and not the graph's, so they are left out: what SPEC/GRAPH.md
        § Durability and Checkpointing in a Long-Lived Process, rule 4, promises
        to leave "byte for byte" is `snapshot/` and `wal`, and those are what
        this reads.
        """
        wal = self.graph_dir() / "wal"
        fingerprint = {
            "wal": hashlib.sha256(wal.read_bytes()).hexdigest() if wal.is_file() else None,
            "wal_size": wal.stat().st_size if wal.is_file() else None,
            "snapshot": None,
        }
        snapshot = self.snapshot_dir()
        if snapshot.is_dir():
            fingerprint["snapshot"] = {
                str(path.relative_to(snapshot)): hashlib.sha256(path.read_bytes()).hexdigest()
                for path in sorted(snapshot.rglob("*")) if path.is_file()
            }
        return fingerprint

    def staging_dir(self) -> Path:
        # GoGraph assembles each snapshot in a sibling "<snapshot>.tmp" staging
        # directory and publishes it with an atomic rename. Since GoGraph v0.3.0
        # the publish is a crash-atomic ".bak" swap
        # (Rename(snapshot, snapshot.bak) -> Rename(snapshot.tmp, snapshot)), so
        # it operates entirely at the graph/ (parent) level and never writes
        # inside snapshot/ itself.
        return self.graph_dir() / "snapshot.tmp"

    def block_snapshot_staging(self):
        """Make the next checkpoint fail, by blocking the staging directory it
        has to clear before it can assemble a snapshot.

        The directory is left non-empty and read-only (0500), so the
        checkpoint's RemoveAll of the stale staging directory fails with EACCES:
        it cannot unlink the child inside a directory it has no write permission
        on. The failure therefore happens AFTER the transactions it would fold
        are durably committed, which is the case FR7 governs. Making the live
        snapshot/ directory read-only does not work: the server chmods graph/
        back to 0700 when it opens the store, and the v0.3.0+ archive-swap
        publish renames at the graph/ (parent) level rather than writing inside
        snapshot/.
        """
        staging = self.staging_dir()
        if staging.exists():
            staging.chmod(0o700)
            shutil.rmtree(staging, ignore_errors=True)
        staging.mkdir(mode=0o700)
        (staging / "blocker").write_text("blocker")
        staging.chmod(0o500)

    def unblock_snapshot_staging(self):
        staging = self.staging_dir()
        if staging.exists():
            staging.chmod(0o700)
            shutil.rmtree(staging, ignore_errors=True)

    @staticmethod
    def rows_by(result, key_col):
        """Index {columns, rows} output by the value of one column."""
        cols = result["columns"]
        idx = cols.index(key_col)
        return {row[idx]: row for row in result["rows"]}

    def seed_knowledge_graph(self):
        """A small, realistic project knowledge graph."""
        self.write("CREATE (s:Spec {key:'authentication', title:'User Authentication', status:'approved'})")
        self.write("CREATE (t:Task {key:'login-flow', title:'Implement login flow', status:'in_progress'})")
        self.write("CREATE (d:Decision {key:'use-jwt', rationale:'Stateless sessions scale horizontally'})")

    # ---- baseline graph behaviour ------------------------------------

    def test_create_and_query_roundtrip(self):
        ok = self.write("CREATE (s:Spec {key:'authentication', title:'User Authentication'})")
        # One node, one label, two properties: the counters the CREATE applied,
        # beside the {"ok": true} that was the whole of this object before the
        # member existed.
        assert_graph_write_shape(
            ok, "create without RETURN",
            {"nodesCreated": 1, "propertiesWritten": 2, "labelsAdded": 1})
        result = self.query_json("MATCH (s:Spec) RETURN s.key, s.title")
        by_key = self.rows_by(result, "s.key")
        assert "authentication" in by_key, f"created Spec not found on read-back: {result!r}"
        title_idx = result["columns"].index("s.title")
        assert by_key["authentication"][title_idx] == "User Authentication"

    def test_update_mutates_property(self):
        self.write("CREATE (t:Task {key:'login-flow', status:'in_progress'})")
        self.write("MATCH (t:Task {key:'login-flow'}) SET t.status='done'")
        result = self.query_json("MATCH (t:Task {key:'login-flow'}) RETURN t.status")
        assert result["rows"] == [["done"]], f"update did not persist new status: {result!r}"

    def test_delete_removes_node(self):
        self.write("CREATE (d:Decision {key:'use-jwt'})")
        before = self.query_json("MATCH (d:Decision) RETURN count(d)")["rows"][0][0]
        assert before == 1
        self.write("MATCH (d:Decision {key:'use-jwt'}) DETACH DELETE d")
        after = self.query_json("MATCH (d:Decision) RETURN count(d)")["rows"][0][0]
        assert after == 0, f"delete did not remove the node: count={after}"

    def test_search_variable_length_path(self):
        self.write("CREATE (s:Spec {key:'authentication'})-[:HAS_TASK]->(t:Task {key:'login-flow'})")
        result = self.query_json("MATCH p=(s:Spec)-[*1..2]-(b) RETURN b.key")
        assert ["login-flow"] in result["rows"], f"variable-length search failed: {result!r}"

    def test_one_subcommand_runs_every_statement_class(self):
        """SPEC/GRAPH.md acceptance criterion 4.

        The two tests this replaces asserted the opposite: that `graph query`
        refused a CREATE and `graph create` refused a DETACH DELETE, each with
        exit code 6. There is no operation-class check any more and there is one
        statement-running subcommand, so what has to be proven is that the SAME
        invocation -- `rmp graph client`, against one running server -- runs
        every class, and that a read-back after each confirms the effect, since
        an exit code alone would pass against a command that did nothing.
        """
        self.write("CREATE (n:Spec {key:'class-matrix'})")
        assert self.query_json(
            "MATCH (n:Spec {key:'class-matrix'}) RETURN n.key")["rows"] == [["class-matrix"]]

        self.write("MATCH (n:Spec {key:'class-matrix'}) SET n.status = 'implemented'")
        assert self.query_json(
            "MATCH (n:Spec {key:'class-matrix'}) RETURN n.status")["rows"] == [["implemented"]]

        self.write("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        names = [row[0] for row in self.query_json("SHOW INDEXES")["rows"]]
        assert "spec_key" in names, f"the index was not registered: {names!r}"

        self.write("MATCH (n:Spec {key:'class-matrix'}) DETACH DELETE n")
        assert self.query_json(
            "MATCH (n:Spec {key:'class-matrix'}) RETURN n.key")["rows"] == []

    def test_retired_subcommand_names_exit_127(self):
        """SPEC/GRAPH.md acceptance criterion 5, extended by the withdrawal of
        `execute`.

        The names are checked against what the CLI PUBLISHES rather than against
        a number written down here: `rmp --ai-help` names the subcommands the
        graph family has, every historical name absent from that list is driven,
        and each must be an unresolved subcommand -- it must not run, it must not
        be answered with any of the graph family's own exit codes, and it must
        not reach the store.
        """
        contract = self.test.run_cmd_json(["--ai-help"])
        family = next(c for c in contract["commands"] if c["name"] == "graph")
        published = {sub["name"] for sub in family["subcommands"]}
        assert published == {"serve", "client"}, (
            f"the graph family publishes exactly serve and client; it publishes "
            f"{sorted(published)}. A name that came back needs this test's list "
            f"revisited, not this assertion relaxed.")
        retired = [name for name in RETIRED_SUBCOMMAND_NAMES if name not in published]
        assert retired == list(RETIRED_SUBCOMMAND_NAMES), (
            f"every name driven below must be one the CLI no longer publishes; "
            f"{sorted(set(RETIRED_SUBCOMMAND_NAMES) - set(retired))} is published again")

        self.write("CREATE (s:Spec {key:'authentication'})")
        for name in retired:
            rc, out, err = self.test.run_cmd(
                ["graph", name, "-r", self.roadmap, "--query",
                 "CREATE (n:Spec {key:'must-not-exist'})"],
                check=False)
            assert rc == 127, (
                f"`rmp graph {name}` exited {rc}, want 127: the name is not a "
                f"synonym of any surviving subcommand and none survives as an "
                f"alias (SPEC/COMMANDS.md § Graph Management); stderr={err!r}")
            assert out == "", f"`rmp graph {name}` wrote to stdout: {out!r}"
            assert f"unknown graph subcommand: {name}" in err, (
                f"`rmp graph {name}` must say which subcommand it could not "
                f"resolve; stderr={err!r}")

        assert self.query_json("MATCH (s:Spec) RETURN s.key")["rows"] == [["authentication"]], (
            "an unresolved subcommand must never reach the graph store")

    def test_query_from_stdin(self):
        self.write("CREATE (s:Spec {key:'authentication'})")
        rc, out, err = self.test.graph_client(
            self.roadmap, stdin_text="MATCH (n) RETURN count(n)")
        assert rc == EXIT_OK, f"stdin statement failed: exit={rc} stdout={out!r} stderr={err!r}"
        result = self.query_json("MATCH (n) RETURN count(n)")
        assert result["rows"] == [[1]], result
        assert '"count(n)"' in out and '1' in out, out

    def test_missing_roadmap_flag_exits_3(self):
        rc, out, err = self.test.run_cli(["graph", "client", "--query", "MATCH (n) RETURN n"])
        assert rc == EXIT_NO_ROADMAP, f"exit={rc} stdout={out!r} stderr={err!r}"
        assert "no roadmap selected" in err, err

    def test_unknown_roadmap_exits_4(self):
        rc, out, err = self.test.graph_client("no_such_roadmap_xyz", query="MATCH (n) RETURN n")
        assert rc == EXIT_NOT_FOUND, f"exit={rc} stdout={out!r} stderr={err!r}"
        assert 'roadmap "no_such_roadmap_xyz" not found' in err, err

    def test_empty_query_exits_2(self):
        rc, out, err = self.test.graph_client(self.roadmap, query="")
        assert rc == EXIT_NO_QUERY, f"exit={rc} stdout={out!r} stderr={err!r}"
        assert "no query supplied" in err, err

    # ---- the persistence contract ------------------------------------

    def test_snapshot_is_written_by_the_shutdown_checkpoint(self):
        assert not self.snapshot_dir().exists(), (
            "a graph served for the first time has no snapshot: nothing has folded yet")
        self.write("CREATE (s:Spec {key:'authentication'})")
        assert not self.snapshot_dir().exists(), (
            "no invocation checkpoints any more, so an acknowledged write leaves "
            "the snapshot directory alone")
        assert self.wal_size() > 0, (
            "the acknowledged write is durable in the write-ahead log before the "
            "client is answered, which is what makes the fold deferrable")

        code = self.server.stop()
        assert code == EXIT_OK, f"the server must exit 0 on SIGINT; exit={code}"
        manifest = self.snapshot_dir() / "manifest.json"
        mapper = self.snapshot_dir() / "mapper.bin"
        assert manifest.is_file(), (
            "the shutdown checkpoint must produce snapshot/manifest.json once the "
            "log has grown")
        assert mapper.is_file(), (
            "the snapshot must carry mapper.bin (self-sufficient) so the "
            "write-ahead log can be truncated safely")

    def test_wal_truncated_by_the_shutdown_checkpoint(self):
        self.write("CREATE (s:Spec {key:'authentication'})")
        assert self.wal_size() > 0, "precondition: the write is in the log"
        code = self.server.stop()
        assert code == EXIT_OK, f"the server must exit 0 on SIGINT; exit={code}"
        assert self.wal_size() == 0, (
            f"the shutdown checkpoint folds the log into the snapshot and then "
            f"truncates it; got {self.wal_size()}")

    def test_wal_is_bounded_by_each_servers_shutdown(self):
        """Without a fold the write-ahead log grows monotonically for the whole
        life of the graph. What bounds it is the checkpoint, and the checkpoint
        is the server's: inside one server's life the log GROWS with every
        write, and each server's shutdown takes it back to zero.
        """
        previous = self.wal_size()
        for i in range(1, 8):
            self.write(f"CREATE (t:Task {{key:'task-{i}'}})")
            size = self.wal_size()
            assert size > previous, (
                f"write {i} must have appended to the log: {previous} -> {size}")
            previous = size

        code = self.server.stop()
        assert code == EXIT_OK, f"the server must exit 0 on SIGINT; exit={code}"
        assert self.wal_size() == 0, (
            f"the first server's shutdown must fold and truncate; got {self.wal_size()}")

        self.server = self.test.start_graph_server(self.roadmap)
        assert self.wal_size() == 0, (
            "a server opened over a current snapshot replays nothing and appends "
            "nothing of its own")
        for i in range(8, 10):
            self.write(f"CREATE (t:Task {{key:'task-{i}'}})")
        assert self.wal_size() > 0, "the second server's writes are in its log"
        code = self.server.stop()
        assert code == EXIT_OK, f"the server must exit 0 on SIGINT; exit={code}"
        assert self.wal_size() == 0, (
            f"the second server's shutdown must fold and truncate too; got {self.wal_size()}")

        # The bound costs nothing: everything both servers wrote is in the
        # snapshot, which a third server serves with an empty log.
        self.server = self.test.start_graph_server(self.roadmap)
        keys = {row[0] for row in self.query_json("MATCH (t:Task) RETURN t.key")["rows"]}
        assert keys == {f"task-{i}" for i in range(1, 10)}, (
            f"folding the log must lose nothing committed: {sorted(keys)}")

    def test_durability_across_processes_from_snapshot(self):
        self.seed_knowledge_graph()
        self.write("MATCH (s:Spec {key:'authentication'}) SET s.status='shipped'")

        code = self.server.stop()
        assert code == EXIT_OK, f"the server must exit 0 on SIGINT; exit={code}"
        assert self.wal_size() == 0, (
            "the shutdown checkpoint folded the log into the snapshot and truncated it")
        assert self.snapshot_dir().is_dir(), "and left the snapshot behind"

        # With the server gone there is no other way in: no invocation opens the
        # store, so the durable state is unreadable until another server serves it.
        rc, out, err = self.test.graph_client(self.roadmap, query="MATCH (n) RETURN count(n)")
        assert rc == EXIT_NO_SERVER, f"exit={rc} stdout={out!r} stderr={err!r}"
        assert "no graph server is listening" in err, err
        assert out == "", f"a client that reached nothing must print nothing: {out!r}"

        # A FRESH process, over an empty log: what it serves can only have come
        # from the snapshot.
        self.server = self.test.start_graph_server(self.roadmap)
        assert self.wal_size() == 0, "precondition: there is no log tail to replay"
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

    def test_delete_is_durable_across_a_restart(self):
        self.write("CREATE (d:Decision {key:'use-jwt'})")
        self.write("CREATE (t:Task {key:'login-flow'})")
        self.write("MATCH (d:Decision {key:'use-jwt'}) DETACH DELETE d")

        self.restart_server()
        assert self.wal_size() == 0, (
            f"the shutdown checkpoint must have folded the deletion and truncated "
            f"the log; got {self.wal_size()}")
        keys = {row[0] for row in self.query_json("MATCH (n) RETURN n.key")["rows"]}
        assert keys == {"login-flow"}, (
            f"the snapshot carries the deletion tombstone, so the deleted node "
            f"must not come back on the next open: {sorted(keys)}")

    # Statements that change nothing, and what each answers. A read produces
    # result columns; a MERGE that matched and a DELETE that matched no row
    # produce none and answer with the write shape carrying no counters at all.
    # None of them appends to the write-ahead log, which is what makes a fold
    # not owed (SPEC/GRAPH.md § Synchronous Checkpoint on Write).
    STATEMENTS_THAT_CHANGE_NOTHING = (
        ("MATCH (n) RETURN count(n)", True),
        ("MATCH (s:Spec {key:'authentication'}) RETURN s.key", True),
        ("SHOW INDEXES", True),
        ("MERGE (s:Spec {key:'authentication'})", False),
        ("MATCH (d:Decision {key:'no-such-decision'}) DETACH DELETE d", False),
    )

    def test_statement_that_writes_nothing_leaves_snapshot_and_log_untouched(self):
        """SPEC/GRAPH.md acceptance criterion 17, and § Durability and
        Checkpointing in a Long-Lived Process, rule 4.

        What decides whether a fold is owed is the write-ahead log and not the
        statement: a transaction that appended nothing owes none, and neither
        the snapshot directory nor the log is touched on its account. Both
        halves are asserted against the BYTES, while one server runs and then
        across a shutdown, because a size alone would miss a rewrite that
        happened to land on the same length.

        The first phase runs against a deliberately NON-EMPTY log with no
        snapshot yet written: an implementation that folded on a read would
        truncate that log and publish a snapshot, and would be caught by either
        half of the fingerprint.
        """
        self.write("CREATE (s:Spec {key:'authentication'})")
        before = self.durable_fingerprint()
        assert before["wal_size"] > 0, "precondition: the log carries the write"
        assert before["snapshot"] is None, "precondition: nothing has folded yet"

        for statement, has_columns in self.STATEMENTS_THAT_CHANGE_NOTHING:
            result = self.query_json(statement)
            if has_columns:
                assert "columns" in result, (
                    f"{statement!r} produces result columns; got {result!r}")
            else:
                assert_graph_write_shape(result, statement, {})
            assert self.durable_fingerprint() == before, (
                f"{statement!r} appended nothing, so it owes no fold: neither "
                f"snapshot/ nor wal may change on its account (SPEC/GRAPH.md "
                f"§ What a Statement That Writes Nothing Changes on Disk, "
                f"rules 2 and 3)")

        # And a shutdown that owes no fold writes nothing at all. Fold what IS
        # owed first, so the next server opens over a current snapshot and an
        # empty log; then drive the same statements through it and stop it.
        self.restart_server()
        folded = self.durable_fingerprint()
        assert folded["wal_size"] == 0 and folded["snapshot"], (
            f"precondition: the first server's shutdown folded and truncated; got {folded!r}")

        for statement, _has_columns in self.STATEMENTS_THAT_CHANGE_NOTHING:
            self.query_json(statement)
        code = self.server.stop()
        assert code == EXIT_OK, f"the server must exit 0 on SIGINT; exit={code}"
        assert self.durable_fingerprint() == folded, (
            "a shutdown that owes no fold writes nothing at all: snapshot/ and "
            "wal are left byte for byte as the server found them")

    def test_checkpoint_failure_is_non_fatal(self):
        """FR7. The write is acknowledged before any fold is attempted, so a
        checkpoint that fails afterwards can only be a diagnostic: it must not
        change the shutdown's exit code, must not cost the committed data, and
        must be said on the server's own stderr rather than to the caller whose
        write already succeeded.
        """
        # A first server folds a snapshot, so what fails below is an attempt to
        # UPDATE one rather than to write the first.
        self.write("CREATE (s:Spec {key:'authentication'})")
        self.restart_server()
        assert self.wal_size() == 0, "precondition: the first fold succeeded"

        acknowledged = self.write(
            "CREATE (d:Decision {key:'use-jwt', rationale:'Stateless sessions scale horizontally'})")
        assert_graph_write_shape(
            acknowledged, "the write whose fold is about to fail",
            {"nodesCreated": 1, "propertiesWritten": 2, "labelsAdded": 1})
        committed = self.wal_size()
        assert committed > 0, "the commit is durable in the log before it is acknowledged"

        self.block_snapshot_staging()
        try:
            code = self.server.stop()
        finally:
            self.unblock_snapshot_staging()
        assert code == EXIT_OK, (
            f"a failed checkpoint after durable commits must not change the "
            f"server's exit code (FR7); exit={code}")

        self.server.drain_stderr_to_eof()
        stderr = self.server.stderr_text()
        assert "shutdown checkpoint" in stderr and "durable in the write-ahead log" in stderr, (
            f"the failure must be surfaced on the server's own stderr, saying "
            f"what is still safe; stderr={stderr!r}")
        assert self.wal_size() == committed, (
            f"the log must be left intact when the fold fails, since durability "
            f"now rests on it alone; {committed} -> {self.wal_size()}")

        # Degraded but correct: the snapshot is stale, and the next server
        # recovers the committed state from it plus the log tail.
        self.server = self.test.start_graph_server(self.roadmap)
        result = self.query_json("MATCH (n) RETURN n.key, n.rationale ORDER BY n.key")
        by_key = self.rows_by(result, "n.key")
        assert set(by_key) == {"authentication", "use-jwt"}, (
            f"nothing committed may be lost by a failed checkpoint: {sorted(by_key)}")
        rationale_idx = result["columns"].index("n.rationale")
        assert by_key["use-jwt"][rationale_idx] == "Stateless sessions scale horizontally"

    def test_checkpoint_reconciles_after_failure(self):
        self.write("CREATE (s:Spec {key:'authentication'})")
        self.block_snapshot_staging()
        try:
            code = self.server.stop()
        finally:
            self.unblock_snapshot_staging()
        assert code == EXIT_OK, f"exit={code} stderr={self.server.stderr_text()!r}"
        orphaned = self.wal_size()
        assert orphaned > 0, "the failed fold left the committed write in the log"
        assert not self.snapshot_dir().exists(), "and published no snapshot at all"

        # The next server recovers from that log and writes more of its own; its
        # shutdown checkpoint is the "next successful checkpoint" that reconciles.
        self.server = self.test.start_graph_server(self.roadmap)
        self.write("CREATE (t:Task {key:'login-flow'})")
        self.write("CREATE (d:Decision {key:'use-jwt'})")
        code = self.server.stop()
        assert code == EXIT_OK, f"exit={code} stderr={self.server.stderr_text()!r}"
        assert self.wal_size() == 0, (
            f"a successful checkpoint after a failed one must reconcile the "
            f"snapshot and truncate the log; got {self.wal_size()}")
        assert (self.snapshot_dir() / "manifest.json").is_file(), "and publish the snapshot"

        # All three nodes survive with the log empty, so all three are in the
        # snapshot -- the orphaned one included.
        self.server = self.test.start_graph_server(self.roadmap)
        keys = {row[0] for row in self.query_json("MATCH (n) RETURN n.key")["rows"]}
        assert keys == {"authentication", "login-flow", "use-jwt"}, (
            f"the reconciled snapshot is missing nodes: {sorted(keys)}")


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
