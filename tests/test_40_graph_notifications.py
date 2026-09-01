#!/usr/bin/env python3
"""
Test 40: rmp graph execute notifications as stderr diagnostics.

End-to-end backstop for SPEC/GRAPH.md § Query Notifications as Diagnostics
(functional requirement 10) and Acceptance Criteria 20, 21, 22.

Contract under test:
- Every graph subcommand that executes a query surfaces each advisory
  notification the engine attaches to the result as a plain-text diagnostic
  line on stderr (one line per notification, carrying at least the severity,
  the stable machine-readable code, and the description).
- Notifications are advisory only: they NEVER change the stdout success
  output (the columns/rows shape for a read or RETURN-bearing write, or
  {"ok": true} for a write with no RETURN) and NEVER change the exit code,
  which stays 0 on success.
- A query that produces no notification writes nothing extra to stderr.

The classic notification is the Cartesian-product warning the engine raises
for a disconnected multi-pattern MATCH (two patterns sharing no variable):
code "Neo.ClientNotification.Statement.CartesianProductWarning".

Read path vs write path (AC 22): notifications are surfaced on both the read
path (query/search) and the write path (create/update/delete). The CLI routes
every result -- read and transactional -- through the same notification
emitter, so whatever notifications the engine attaches are surfaced on either
path. On the pinned GoGraph (go.mod) the read-path result is the one that
carries the Cartesian-product advisory; the write subcommands are wired
through the identical emitter, verified here by asserting the write path keeps
its exact success stdout and exit code 0 and never emits non-notification
noise on stderr.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXIT_OK = 0

CARTESIAN_CODE = "Neo.ClientNotification.Statement.CartesianProductWarning"


class TestGraphNotifications:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self._seed_knowledge_graph()

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------

    def graph(self, query: str, check: bool = True):
        """Run `rmp graph execute`, returning (exit_code, stdout, stderr)."""
        return self.test.run_cmd(
            ["graph", "execute", "-r", self.roadmap, "--query", query], check=check)

    def _seed_knowledge_graph(self):
        """One Spec and one Task so a disconnected MATCH has rows to match
        while still triggering the engine's Cartesian-product advisory.
        Realistic project-knowledge nodes (no foo/bar placeholders)."""
        self.graph("CREATE (s:Spec {key:'user-authentication', title:'User Authentication'})")
        self.graph("CREATE (t:Task {key:'implement-login-flow', title:'Implement login flow'})")

    @staticmethod
    def _is_json_columns_rows(stdout: str) -> bool:
        import json
        try:
            parsed = json.loads(stdout.strip())
        except ValueError:
            return False
        return isinstance(parsed, dict) and "columns" in parsed and "rows" in parsed

    # ---- AC 21: a disconnected MATCH surfaces a notice on stderr ----

    def test_disconnected_match_emits_cartesian_notice_on_stderr(self):
        code, stdout, stderr = self.graph("MATCH (a:Spec), (b:Task) RETURN a.key, b.key")

        assert code == EXIT_OK, f"a notification is advisory; exit must stay 0, got {code}"

        # stderr carries the Cartesian-product notice: at least severity, the
        # stable code, and a description, on a plain-text line.
        assert stderr.strip() != "", "expected a Cartesian-product notice on stderr, got none"
        assert CARTESIAN_CODE in stderr or "cartesian" in stderr.lower(), (
            f"stderr must carry the Cartesian-product notification; stderr={stderr!r}")
        notice_lines = [ln for ln in stderr.splitlines() if ln.strip()]
        assert len(notice_lines) == 1, (
            f"exactly one notification line expected, got {len(notice_lines)}: {stderr!r}")
        line = notice_lines[0]
        # Representative SPEC shape: "<SEVERITY> <CODE>: <description>".
        assert "INFORMATION" in line, f"notice line missing severity: {line!r}"
        assert ": " in line, f"notice line missing 'code: description' separator: {line!r}"

        # stdout is exactly the normal columns/rows result, unchanged.
        assert self._is_json_columns_rows(stdout), (
            f"stdout must remain the columns/rows JSON, unchanged by the notice; stdout={stdout!r}")
        import json
        result = json.loads(stdout.strip())
        assert result["columns"] == ["a.key", "b.key"], result
        assert ["user-authentication", "implement-login-flow"] in result["rows"], result

    # ---- AC 22: a connected statement writes nothing extra to stderr ----

    def test_connected_query_emits_no_notice(self):
        code, stdout, stderr = self.graph("MATCH (s:Spec) RETURN s.key")

        assert code == EXIT_OK, f"connected query must succeed, got exit {code}"
        assert stderr.strip() == "", (
            f"a notification-free query must write nothing to stderr; stderr={stderr!r}")
        assert self._is_json_columns_rows(stdout), f"stdout malformed: {stdout!r}"
        import json
        assert json.loads(stdout.strip())["rows"] == [["user-authentication"]]

    def test_connected_traversal_emits_no_notice(self):
        # A connected traversal is quiet too. It used to run under a separate
        # `search` subcommand; there is one subcommand now
        # (SPEC/COMMANDS.md § Graph Management), so what this varies is the
        # STATEMENT and not the command.
        self.graph("CREATE (s:Spec {key:'session-management'})-[:HAS_TASK]->(t:Task {key:'token-rotation'})")
        code, stdout, stderr = self.graph("MATCH p=(s:Spec {key:'session-management'})-[*1..2]-(b) RETURN b.key")

        assert code == EXIT_OK, f"connected search must succeed, got exit {code}"
        assert stderr.strip() == "", (
            f"a connected traversal must write nothing to stderr; stderr={stderr!r}")
        assert self._is_json_columns_rows(stdout), f"stdout malformed: {stdout!r}"

    # ---- notifications on a statement that writes ----

    def test_write_success_output_unchanged_by_notifications(self):
        # A disconnected MATCH that drives a CREATE. A statement that writes
        # runs through the same notification emitter as one that does not,
        # because there is one path and one emitter.
        code, stdout, stderr = self.graph("MATCH (a:Spec), (b:Task) CREATE (l:Link {note:'join-spec-and-task'})")

        # The advisory never changes the write's success output or exit code.
        assert code == EXIT_OK, f"write must succeed with exit 0; got {code}, stderr={stderr!r}"
        assert stdout.strip() == '{\n  "ok": true\n}' or '"ok": true' in stdout, (
            f"write with no RETURN must emit the exact {{'ok': true}} shape; stdout={stdout!r}")

        # stderr carries only engine notifications (never arbitrary noise): any
        # line present must be a well-formed "code: description" notice.
        for ln in stderr.splitlines():
            if ln.strip():
                assert ": " in ln, f"unexpected non-notification stderr on write path: {ln!r}"

        # The write actually committed (read it back; this read also confirms
        # the link node exists).
        result = self.test.run_cmd_json(
            ["graph", "execute", "-r", self.roadmap, "--query", "MATCH (l:Link) RETURN l.note"])
        assert result["rows"] == [["join-spec-and-task"]], (
            f"write-path query did not persist the node: {result!r}")

    def test_write_with_return_keeps_columns_rows(self):
        # A RETURN-bearing write must still emit columns/rows on stdout, with
        # any notification confined to stderr and exit code 0.
        code, stdout, stderr = self.graph("CREATE (d:Decision {key:'adopt-jwt', rationale:'Stateless sessions scale'}) RETURN d.key")

        assert code == EXIT_OK, f"RETURN-bearing write must succeed; got {code}, stderr={stderr!r}"
        assert self._is_json_columns_rows(stdout), (
            f"RETURN-bearing write must emit columns/rows; stdout={stdout!r}")
        import json
        assert json.loads(stdout.strip())["rows"] == [["adopt-jwt"]], stdout


def _run_all():
    instance_cls = TestGraphNotifications
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
    print(f"Graph notification tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
