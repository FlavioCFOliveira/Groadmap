#!/usr/bin/env python3
"""
Test 76: the user-visible behaviour of rmp tasks #577 to #590, driven against
the compiled binary in throwaway roadmaps under an isolated HOME.

Each class is one task, named by its rmp id, and asserts outcomes rather than
exit codes alone: what the roadmap holds afterwards, what the filesystem holds,
and the exact published line on stderr.

  #577  roadmap create is atomic under concurrent creators.
  #578  roadmap remove refuses while a graph server holds the store lock.
  #579  concurrent sprint openers receive the sequential refusals.
  #580  the task family help names SPRINT as the destination of a reopening.
  #581  task reopen refuses to take a sprint past its --max-tasks cap.
  #582  a database newer than the binary is refused, unchanged, by every command.
  #583  a zero-byte project.db is initialised; a non-SQLite file is refused.
  #584  a symbolically linked database companion is refused.
  #585  a repeated flag is refused with exit 2 on every surface.
  #586  a relative home directory is refused before anything is created.
  #587  roadmap list skips directories whose names no command could select.
  #588  audit list --entity-id prints the canonical format line.
  #590  every released schema version's fixture migrates through the binary.
  #593  concurrent sprint add-tasks processes all succeed against one sprint.

#589 (statement-scoped memory release in the graph server) has no case here:
SPEC/GRAPH.md § Statement-Scoped Memory Release publishes no figure, and
SPEC/BUILD.md forbids performance-measurement tests; its property is pinned by
internal/graphserve/memrelease_test.go.
"""

import inspect
import json
import os
import shutil
import sqlite3
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, REPO_ROOT, commit_flags_for  # noqa: E402

AI_HINT = "AI agents usage: run `rmp --ai-help` for a machine-readable command contract."

FR = "Finance operations must close the month without manual spreadsheets"
TR = "Extend the payments service behind its existing repository interface"
AC = "Covered by an integration test against the provider sandbox"


class LabBase:
    """Fixture plumbing shared by every class: a fresh HOME per test."""

    def setup_method(self):
        self.t = GroadmapTestBase()
        self.t.setup()

    def teardown_method(self):
        self.t.teardown()

    def env(self, home=None):
        env = os.environ.copy()
        env["HOME"] = str(self.t.home_dir) if home is None else home
        return env

    def run(self, args, home=None, cwd=None):
        result = subprocess.run([self.t.cli_path] + args, capture_output=True, text=True,
                                env=self.env(home), stdin=subprocess.DEVNULL, cwd=cwd, timeout=120)
        return result.returncode, result.stdout, result.stderr

    def run_many(self, argvs):
        """Start every argv at once and return their (rc, stdout, stderr)."""
        procs = [subprocess.Popen([self.t.cli_path] + a, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                  stdin=subprocess.DEVNULL, text=True, env=self.env()) for a in argvs]
        out = []
        for p in procs:
            o, e = p.communicate(timeout=120)
            out.append((p.returncode, o, e))
        return out

    def first_line(self, stderr):
        return stderr.splitlines()[0] if stderr else ""

    def expect(self, args, code, line, home=None, cwd=None):
        rc, out, err = self.run(args, home=home, cwd=cwd)
        assert rc == code, f"rmp {' '.join(args)}: exit {rc}, want {code}; stderr {err!r}"
        assert self.first_line(err) == line, f"rmp {' '.join(args)}: stderr {err!r}, want {line!r}"
        assert out == "", f"rmp {' '.join(args)}: a refusal wrote to stdout: {out!r}"

    def home_of(self, roadmap):
        return self.t.home_dir / ".roadmaps" / roadmap

    def db_of(self, roadmap):
        return self.home_of(roadmap) / "project.db"

    def task(self, roadmap, title, **kw):
        return self.t.create_task(roadmap, title, FR, TR, AC, **kw)

    def sprint(self, roadmap, title, description, *extra):
        return self.t.run_cmd_json(["sprint", "create", "-r", roadmap, "-t", title, "-d", description, *extra])["id"]

    def walk(self, roadmap, task_id, *statuses):
        for status in statuses:
            self.t.run_cmd(["task", "stat", "-r", roadmap, str(task_id), status] + commit_flags_for(status))


class TestTask577ConcurrentRoadmapCreate(LabBase):
    def test_exactly_one_concurrent_creator_wins(self):
        for round_ in range(3):
            name = f"payments-api-{round_}"
            results = self.run_many([["roadmap", "create", name]] * 8)
            winners = [r for r in results if r[0] == 0]
            assert len(winners) == 1, f"round {round_}: {len(winners)} creators exited 0: {results}"
            assert json.loads(winners[0][1]) == {"name": name}
            line = f'Error: resource already exists: roadmap "{name}" already exists'
            for rc, out, err in results:
                if rc == 0:
                    continue
                assert rc == 5 and self.first_line(err) == line and out == "", (
                    f"round {round_}: a loser exited {rc} with {err!r}")
            assert sorted(os.listdir(self.home_of(name))) == ["project.db"], (
                f"round {round_}: the home holds {os.listdir(self.home_of(name))}")
            # The published database is complete: it lists and takes a task.
            self.task(name, "Add idempotency keys to the refund endpoint")
            assert len(self.t.list_tasks(name)) == 1

    def test_a_symlinked_project_db_is_neither_existing_nor_claimable(self):
        name = "linked-ledger"
        home = self.home_of(name)
        home.mkdir(parents=True, mode=0o700)
        target = self.t.home_dir / "outside.db"
        link = home / "project.db"
        os.symlink(target, link)
        self.expect(["roadmap", "create", name], 1,
                    f"Error: database error: {link} is a symbolic link; refusing to use it as a roadmap database file")
        assert not target.exists() and link.is_symlink() and os.listdir(home) == ["project.db"]


class TestTask578RemoveUnderAGraphServer(LabBase):
    def test_removal_is_refused_while_a_server_runs_and_succeeds_after(self):
        name = self.t.create_roadmap("knowledge-base")
        self.task(name, "Map the payment components into the graph")
        server = self.t.start_graph_server(name)
        code, out, err = self.t.graph_client(name, "CREATE (:Component {key:'refunds'})")
        assert code == 0, err

        before = sorted(p.name for p in self.home_of(name).iterdir())
        self.expect(["roadmap", "remove", name], 6,
                    f'Error: validation error: cannot remove roadmap "{name}": a graph server is running '
                    f'for it; stop the server first')
        assert sorted(p.name for p in self.home_of(name).iterdir()) == before, "the refusal removed something"
        code, out, err = self.t.graph_client(name, "MATCH (c:Component) RETURN c.key AS key")
        assert code == 0 and "refunds" in out, f"the server stopped serving after the refusal: {err}"

        assert server.stop() == 0
        rc, out, err = self.run(["roadmap", "remove", name])
        assert rc == 0 and out == "", err
        assert not self.home_of(name).exists()

    def test_a_stale_socket_does_not_block_removal_and_serve_refuses_a_removed_roadmap(self):
        name = self.t.create_roadmap("stale-socket")
        (self.home_of(name) / "graph").mkdir(mode=0o700)
        (self.home_of(name) / "graph.sock").write_text("left by a killed server\n")
        rc, _, err = self.run(["roadmap", "remove", name])
        assert rc == 0, err
        assert not self.home_of(name).exists()
        self.expect(["graph", "serve", "-r", name], 4, f'Error: resource not found: roadmap "{name}" not found')
        assert not self.home_of(name).exists(), "graph serve recreated the removed roadmap's home"


class TestTask579ConcurrentSprintOpeners(LabBase):
    def test_losers_receive_the_sequential_refusals(self):
        name = self.t.create_roadmap("sprint-race")
        sprints = [self.sprint(name, title, "Quarterly payments work")
                   for title in ("Refund safety", "Dispute workflow", "Invoice performance", "Ledger exports")]
        results = self.run_many([["sprint", "start", "-r", name, str(s)] for s in sprints] +
                                [["sprint", "start", "-r", name, str(sprints[0])]])
        targets = sprints + [sprints[0]]
        winners = [targets[i] for i, r in enumerate(results) if r[0] == 0]
        assert len(winners) == 1, f"{len(winners)} sprints opened: {results}"
        winner = winners[0]
        for target, (rc, out, err) in zip(targets, results):
            if rc == 0:
                continue
            want = (f"Error: validation error: cannot start sprint with status OPEN" if target == winner
                    else f"Error: validation error: sprint #{winner} is already open — close it first")
            assert rc == 6 and self.first_line(err) == want, f"sprint {target}: exit {rc}, {err!r}"
            assert "constraint" not in err
        opened = [s for s in self.t.list_sprints(name) if s["status"] == "OPEN"]
        assert [s["id"] for s in opened] == [winner]
        audit = self.t.run_cmd_json(["audit", "list", "-r", name, "-o", "SPRINT_START"])
        assert len(audit) == 1, f"{len(audit)} SPRINT_START entries, want 1"


class TestTask580And581Reopen(LabBase):
    def test_family_help_names_sprint(self):
        rc, out, _ = self.run(["task", "--help"])
        line = next(l for l in out.splitlines() if l.strip().startswith("reopen <task-ids>"))
        assert "to SPRINT" in line and "BACKLOG" not in line, line

    def test_reopen_past_the_cap_is_refused_and_changes_nothing(self):
        name = self.t.create_roadmap("reopen-capacity")
        sprint = self.sprint(name, "Refund safety", "Make refunds safe to retry", "--max-tasks", "2")
        done = self.task(name, "Add idempotency keys to the refund endpoint")
        planned = self.task(name, "Retry provider webhooks with exponential backoff")
        doing = self.task(name, "Reconcile settlement totals against the daily report")
        self.t.run_cmd(["sprint", "add-tasks", "-r", name, str(sprint), str(done)])
        self.walk(name, done, "DOING", "TESTING", "COMPLETED")
        self.t.run_cmd(["sprint", "add-tasks", "-r", name, str(sprint), f"{planned},{doing}"])
        self.walk(name, doing, "DOING")

        before = self.t.run_cmd_json(["task", "get", "-r", name, str(done)])
        audit_before = len(self.t.run_cmd_json(["audit", "list", "-r", name, "-l", "500"]))
        self.expect(["task", "reopen", "-r", name, str(done)], 6,
                    f"Error: validation error: reopening 1 task(s) would exceed sprint #{sprint} capacity "
                    f"(2/2 tasks active)")
        assert self.t.run_cmd_json(["task", "get", "-r", name, str(done)]) == before
        assert len(self.t.run_cmd_json(["audit", "list", "-r", name, "-l", "500"])) == audit_before

        rc, _, err = self.run(["task", "reopen", "-r", name, str(doing)])
        assert rc == 0, err
        self.t.assert_task_status(name, doing, "SPRINT")


class TestTask582NewerSchema(LabBase):
    def test_every_command_refuses_and_nothing_is_written(self):
        name = self.t.create_roadmap("future-ledger")
        self.task(name, "Add idempotency keys to the refund endpoint")
        path = self.db_of(name)
        conn = sqlite3.connect(path)
        conn.execute("UPDATE _metadata SET value = '1.99.0' WHERE key = 'schema_version'")
        conn.commit()
        conn.close()
        before = path.read_bytes()
        supported = REPO_ROOT.joinpath("internal/db/schema.go").read_text().split('SchemaVersion = "')[1].split('"')[0]
        line = (f"Error: database error: {path} has schema version 1.99.0, newer than schema version "
                f"{supported} supported by this rmp; upgrade rmp to open it")
        for args in (["task", "list", "-r", name], ["stats", "-r", name],
                     ["task", "create", "-r", name, "-t", "Rotate the provider API credentials",
                      "-fr", FR, "-tr", TR, "-ac", AC],
                     ["audit", "list", "-r", name]):
            self.expect(args, 1, line)
        assert path.read_bytes() == before, "a refused command changed the newer database"


class TestTask583FileShapes(LabBase):
    def test_a_zero_byte_database_is_initialised_even_by_two_at_once(self):
        name = self.t.create_roadmap("emptied-ledger")
        for f in self.home_of(name).iterdir():
            f.unlink()
        self.db_of(name).write_bytes(b"")
        os.chmod(self.db_of(name), 0o600)
        results = self.run_many([["task", "list", "-r", name], ["sprint", "list", "-r", name]])
        for rc, out, err in results:
            assert rc == 0 and json.loads(out) == [] and err == "", f"exit {rc}: {err!r}"
        self.task(name, "Reconcile settlement totals against the daily report")
        assert [t["title"] for t in self.t.list_tasks(name)] == ["Reconcile settlement totals against the daily report"]
        conn = sqlite3.connect(self.db_of(name))
        assert conn.execute("SELECT COUNT(*) FROM _metadata").fetchone()[0] == 3
        conn.close()

    def test_a_file_that_is_not_sqlite_is_refused_and_left_as_it_is(self):
        name = self.t.create_roadmap("overwritten-ledger")
        for f in self.home_of(name).iterdir():
            f.unlink()
        content = b"\x89PNG\r\n\x1a\n" + b"\x00" * 64
        self.db_of(name).write_bytes(content)
        os.chmod(self.db_of(name), 0o600)
        self.expect(["task", "list", "-r", name], 1,
                    f"Error: database error: {self.db_of(name)} is not a valid roadmap database")
        assert self.db_of(name).read_bytes() == content
        assert sorted(os.listdir(self.home_of(name))) == ["project.db"], "the refusal created a sidecar"


class TestTask584SymlinkedCompanion(LabBase):
    def test_a_linked_wal_is_refused_and_its_target_untouched(self):
        name = self.t.create_roadmap("linked-wal")
        wal = self.home_of(name) / "project.db-wal"
        if wal.exists():
            wal.unlink()
        target = self.t.home_dir / "elsewhere.wal"
        target.write_text("someone else's file\n")
        os.chmod(target, 0o644)
        os.symlink(target, wal)
        self.expect(["task", "list", "-r", name], 1,
                    f"Error: database error: {wal} is a symbolic link; refusing to use it as a roadmap database file")
        assert target.read_text() == "someone else's file\n"
        assert (os.stat(target).st_mode & 0o777) == 0o644
        assert wal.is_symlink()


class TestTask585RepeatedFlags(LabBase):
    def test_refusals_and_precedence(self):
        name = self.t.create_roadmap("repeated-flags")
        self.expect(["task", "create", "-r", name, "-t", "Add idempotency keys", "--title",
                     "Retry provider webhooks", "-fr", FR, "-tr", TR, "-ac", AC], 2,
                    "Error: invalid input: repeated flag: --title")
        self.expect(["task", "list", "-r", name, "-p", "12", "-p", "3"], 2,
                    "Error: invalid input: repeated flag: -p")
        self.expect(["task", "list", "-r", name, "-r", "other-roadmap"], 2,
                    "Error: invalid input: repeated flag: -r")
        self.expect(["task", "list", "-r", name, "--roadmap", name], 2,
                    "Error: invalid input: repeated flag: --roadmap")
        sprint = self.sprint(name, "Refund safety", "Make refunds safe to retry")
        self.t.run_cmd(["sprint", "start", "-r", name, str(sprint)])
        self.expect(["sprint", "close", "-r", name, str(sprint), "--force", "--force"], 2,
                    "Error: invalid input: repeated flag: --force")
        self.t.assert_sprint_status(name, sprint, "OPEN")
        self.expect(["web", "--port", "70000", "--port", "8787"], 2,
                    "Error: invalid input: repeated flag: --port")
        assert self.t.list_tasks(name) == [], "a refused task create wrote a task"

        rc, out, err = self.run(["task", "create", "-r", name, "-t", "A", "-t", "B", "--help"])
        assert rc == 0 and "Usage: rmp task create" in out, err

    def test_the_contract_publishes_the_condition(self):
        rc, out, _ = self.run(["--ai-help"])
        assert rc == 0
        contract = json.loads(out)
        text = json.dumps(contract)
        assert "A flag was supplied more than once" in text, "the AI contract does not publish the repeated-flag condition"


class TestTask586RelativeHome(LabBase):
    def test_a_relative_home_is_refused_and_nothing_is_created(self):
        work = self.t.home_dir / "work"
        work.mkdir()
        self.expect(["roadmap", "list"], 1,
                    'Error: database error: home directory "relative/home" is not an absolute path; '
                    'refusing to locate the data directory under it', home="relative/home", cwd=str(work))
        self.expect(["roadmap", "create", "payments-api"], 1,
                    'Error: database error: home directory "relative/home" is not an absolute path; '
                    'refusing to locate the data directory under it', home="relative/home", cwd=str(work))
        assert os.listdir(work) == [], f"a relative home created {os.listdir(work)}"
        self.expect(["task", "list", "-r", "payments-api"], 1,
                    'Error: database error: home directory "relative/home" is not an absolute path; '
                    'refusing to locate the data directory under it', home="relative/home", cwd=str(work))
        # rmp web refuses before it binds, so the invocation ends rather than serving.
        self.expect(["web", "--port", "0", "--no-open"], 1,
                    'Error: database error: home directory "relative/home" is not an absolute path; '
                    'refusing to locate the data directory under it', home="relative/home", cwd=str(work))
        assert os.listdir(work) == [], f"a relative home created {os.listdir(work)}"
        # Help and the version line resolve no data directory, so they are served.
        rc, out, _ = self.run(["--version"], home="relative/home", cwd=str(work))
        assert rc == 0 and out.startswith("Groadmap version"), "the version line needs no data directory"
        rc, out, err = self.run(["task", "list", "--help"], home="relative/home", cwd=str(work))
        assert rc == 0 and "Usage: rmp task list" in out, f"help was refused under a relative home: {err!r}"
        assert os.listdir(work) == [], f"help under a relative home created {os.listdir(work)}"


class TestTask587ListSkipsInvalidNames(LabBase):
    def test_only_selectable_names_are_listed(self):
        self.t.create_roadmap("payments-api")
        for bad in ("help", "con", "Payments-Archive", "-leading", "a" * 51):
            d = self.t.roadmaps_dir / bad
            d.mkdir(mode=0o700)
            (d / "project.db").write_text("never read\n")
        rc, out, err = self.run(["roadmap", "list"])
        assert rc == 0 and err == ""
        assert [r["name"] for r in json.loads(out)] == ["payments-api"], out
        for bad in ("help", "con", "Payments-Archive", "-leading", "a" * 51):
            assert (self.t.roadmaps_dir / bad / "project.db").read_text() == "never read\n"


class TestTask588AuditEntityId(LabBase):
    def test_the_flag_and_the_positional_print_one_line(self):
        name = self.t.create_roadmap("audit-entity")
        line = 'Error: invalid input: invalid entity ID: "abc" (must be a positive integer)'
        self.expect(["audit", "list", "-r", name, "--entity-id", "abc"], 2, line)
        self.expect(["audit", "history", "-r", name, "TASK", "abc"], 2, line)


class TestTask590ReleasedSchemaFixtures(LabBase):
    FIXTURES = ("1.0.0", "1.1.0", "1.2.0", "1.6.0", "1.8.0", "1.12.0", "1.14.0")

    def test_every_fixture_migrates_and_keeps_its_data(self):
        for version in self.FIXTURES:
            name = "payments-" + version.replace(".", "-")
            self.home_of(name).mkdir(parents=True, mode=0o700)
            shutil.copyfile(REPO_ROOT / "internal" / "db" / "testdata" / "migration_chain" / f"schema-{version}.db",
                            self.db_of(name))
            os.chmod(self.db_of(name), 0o600)
            tasks = self.t.run_cmd_json(["task", "list", "-r", name, "-l", "100"])
            by_title = {t["title"]: t["status"] for t in tasks}
            assert len(tasks) == 9, f"{version}: {len(tasks)} tasks after migration"
            assert by_title["Add idempotency keys to the refund endpoint"] == "DOING", version
            assert by_title["Reconcile settlement totals against the provider daily report"] == "TESTING", version
            assert by_title["Migrate ledger exports to the columnar format"] == "BACKLOG", version
            statuses = sorted(s["status"] for s in self.t.list_sprints(name))
            assert statuses == ["CLOSED", "OPEN", "PENDING"], f"{version}: {statuses}"
            conn = sqlite3.connect(self.db_of(name))
            got = conn.execute("SELECT value FROM _metadata WHERE key = 'schema_version'").fetchone()[0]
            assert conn.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
            conn.close()
            schema = REPO_ROOT.joinpath("internal/db/schema.go").read_text().split('SchemaVersion = "')[1].split('"')[0]
            assert got == schema, f"{version}: migrated to {got}, want {schema}"


class TestTask593ConcurrentSprintAddTasks(LabBase):
    """Twenty rmp processes add one BACKLOG task each to the same sprint at once.

    Every read-write transaction is begun IMMEDIATE (SPEC/IMPLEMENTATION.md
    § Transaction Lock Mode), so the writers wait for one another under
    busy_timeout at BEGIN. Begun DEFERRED, a transaction that has read before it
    writes is refused SQLITE_BUSY at the upgrade without the busy handler, and
    several of the twenty used to exit 1 after exhausting the retry policy.
    """

    WRITERS = 20
    AREAS = ("refund", "dispute", "invoice", "ledger export", "payout", "chargeback")

    def test_every_writer_succeeds_and_the_sprint_holds_every_task(self):
        name = self.t.create_roadmap("payments-q4")
        sprint = self.sprint(name, "Settlement hardening", "Close the reconciliation gaps before the audit")
        backlog = [self.task(name, f"Reconcile {self.AREAS[i % len(self.AREAS)]} batch {i + 1} "
                                   f"against the provider daily report")
                   for i in range(60)]
        chosen = backlog[::3][:self.WRITERS]
        assert len(chosen) == self.WRITERS

        results = self.run_many([["sprint", "add-tasks", "-r", name, str(sprint), str(task_id)]
                                 for task_id in chosen])
        failures = [(task_id, rc, err) for task_id, (rc, _out, err) in zip(chosen, results) if rc != 0]
        assert not failures, f"{len(failures)} of {self.WRITERS} writers failed: {failures}"
        for task_id, (_rc, out, _err) in zip(chosen, results):
            assert out == "", f"task {task_id}: add-tasks wrote to stdout: {out!r}"

        members = self.t.run_cmd_json(["sprint", "tasks", "-r", name, str(sprint)])
        assert sorted(t["id"] for t in members) == sorted(chosen), (
            f"the sprint holds {sorted(t['id'] for t in members)}, want {sorted(chosen)}")
        assert {t["status"] for t in members} == {"SPRINT"}

        conn = sqlite3.connect(self.db_of(name))
        try:
            positions = [r[0] for r in conn.execute(
                "SELECT position FROM sprint_tasks WHERE sprint_id = ? ORDER BY position", (sprint,))]
            added = conn.execute("SELECT COUNT(*) FROM audit WHERE operation = 'SPRINT_ADD_TASK'").fetchone()[0]
            integrity = conn.execute("PRAGMA integrity_check").fetchone()[0]
        finally:
            conn.close()
        assert positions == list(range(self.WRITERS)), f"sprint positions {positions}, want 0..{self.WRITERS - 1}"
        assert added == self.WRITERS, f"{added} SPRINT_ADD_TASK audit entries, want {self.WRITERS}"
        assert integrity == "ok", f"integrity_check returned {integrity!r}"
        untouched = {t["id"] for t in self.t.list_tasks(name, status="BACKLOG")}
        assert untouched == set(backlog) - set(chosen)


def _run_all():
    passed = failed = 0
    failures = []
    classes = [obj for _n, obj in sorted(inspect.getmembers(sys.modules[__name__], inspect.isclass))
               if obj.__module__ == __name__ and _n.startswith("Test")]
    print(f"Discovered {len(classes)} test classes: "
          f"{', '.join(c.__name__ for c in classes)}")
    for cls in classes:
        for m in sorted(n for n in dir(cls) if n.startswith("test_")):
            label = f"{cls.__name__}.{m}"
            inst = cls()
            inst.setup_method()
            try:
                getattr(inst, m)()
                passed += 1
                print(f"✓ {label}")
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label} (error: {type(exc).__name__}: {exc})")
            finally:
                inst.teardown_method()
    print("\n" + "=" * 60)
    print(f"Correctness and security laboratory tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if _run_all() else 1)
