#!/usr/bin/env python3
"""
Test 28: Command Aliases
SPEC/COMMANDS.md and the per-handler routers document a number of short
aliases for top-level commands and subcommands. Beyond a handful of
ad-hoc uses in the suite, the canonical short forms were not
systematically verified. A future refactor of the switch tables could
drop or rename an alias without any test catching it.

This module table-drives the alias coverage so each alias gets at
least one happy-path assertion.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestTopLevelAliases:
    """Top-level command aliases: t, s, bl, aud, road."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        # One seeded task so that list-style commands produce non-empty output.
        self.task_id = self.test.create_task(
            self.roadmap,
            title="Verify command alias routing in CLI",
            functional_requirements="The router must dispatch short aliases to the same handler as the full name.",
            technical_requirements="Use the same setup as other suites and call each alias once.",
            acceptance_criteria="Each alias returns the same JSON shape as the long form.",
            priority=5,
        )

    def teardown_method(self):
        self.test.teardown()

    def test_task_alias_t(self):
        """`rmp t list` is equivalent to `rmp task list`."""
        long = self.test.run_cmd_json(["task", "list", "-r", self.roadmap])
        short = self.test.run_cmd_json(["t", "list", "-r", self.roadmap])
        assert sorted(x["id"] for x in long) == sorted(x["id"] for x in short)
        print("✓ top-level alias 't' = 'task'")

    def test_sprint_alias_s(self):
        """`rmp s list` is equivalent to `rmp sprint list`."""
        long = self.test.run_cmd_json(["sprint", "list", "-r", self.roadmap])
        short = self.test.run_cmd_json(["s", "list", "-r", self.roadmap])
        # Both empty initially; what matters is that the alias is accepted.
        assert long == short
        print("✓ top-level alias 's' = 'sprint'")

    def test_backlog_alias_bl(self):
        """`rmp bl list` is equivalent to `rmp backlog list`."""
        long = self.test.run_cmd_json(["backlog", "list", "-r", self.roadmap])
        short = self.test.run_cmd_json(["bl", "list", "-r", self.roadmap])
        assert sorted(x["id"] for x in long) == sorted(x["id"] for x in short)
        print("✓ top-level alias 'bl' = 'backlog'")

    def test_audit_alias_aud(self):
        """`rmp aud list` is equivalent to `rmp audit list`."""
        long = self.test.run_cmd_json(["audit", "list", "-r", self.roadmap])
        short = self.test.run_cmd_json(["aud", "list", "-r", self.roadmap])
        assert len(long) == len(short)
        print("✓ top-level alias 'aud' = 'audit'")

    def test_roadmap_alias_road(self):
        """`rmp road list` is equivalent to `rmp roadmap list`."""
        long = self.test.run_cmd_json(["roadmap", "list"])
        short = self.test.run_cmd_json(["road", "list"])
        assert sorted(r["name"] for r in long) == sorted(r["name"] for r in short)
        print("✓ top-level alias 'road' = 'roadmap'")


class TestSubcommandAliases:
    """Subcommand aliases: ls, rm, new, hist, upd, mvto, btm, prio, sev, stat, ..."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()
        self.task_id = self.test.create_task(
            self.roadmap,
            title="Subcommand alias coverage probe task",
            functional_requirements="Drive every subcommand alias once.",
            technical_requirements="Table-driven equivalence between long and short subcommand forms.",
            acceptance_criteria="Each alias accepted without error or with the documented error.",
            priority=4,
        )

    def teardown_method(self):
        self.test.teardown()

    def test_list_ls_alias_across_modules(self):
        """`ls` works wherever `list` works (task, sprint, audit, backlog, roadmap)."""
        for module in ("task", "sprint", "audit", "backlog"):
            long = self.test.run_cmd_json([module, "list", "-r", self.roadmap])
            short = self.test.run_cmd_json([module, "ls", "-r", self.roadmap])
            assert long == short or len(long) == len(short), (
                f"{module} ls must mirror {module} list"
            )
        # Roadmap also accepts ls and does NOT take -r
        long_rm = self.test.run_cmd_json(["roadmap", "list"])
        short_rm = self.test.run_cmd_json(["roadmap", "ls"])
        assert long_rm == short_rm
        print("✓ 'ls' alias works for task / sprint / audit / backlog / roadmap")

    def test_remove_rm_alias_task(self):
        """`task rm` is equivalent to `task remove`."""
        # We need a fresh BACKLOG task to remove; the seeded one will do.
        # Use rm via a second task to avoid affecting other tests.
        rmtask = self.test.create_task(
            self.roadmap,
            title="One-shot probe to be removed via the rm alias",
            functional_requirements="Verifying alias 'rm'.",
            technical_requirements="Single create+remove cycle.",
            acceptance_criteria="task get returns [] after rm.",
        )
        self.test.run_cmd(["task", "rm", "-r", self.roadmap, str(rmtask)])
        result = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(rmtask)])
        assert result == [], f"rm alias should delete; got {result}"
        print("✓ 'rm' alias = 'remove' (task)")

    def test_remove_aliases_roadmap(self):
        """Roadmap accepts both rm and delete aliases."""
        # Each alias removes a distinct roadmap so the test stays deterministic.
        for alias in ("rm", "delete"):
            tmp = self.test.create_roadmap()
            self.test.run_cmd(["roadmap", alias, tmp])
            remaining = self.test.run_cmd_json(["roadmap", "list"])
            names = {r["name"] for r in remaining}
            assert tmp not in names, f"roadmap {alias} should remove {tmp}; remaining={names}"
        print("✓ roadmap 'rm' and 'delete' aliases both remove the roadmap")

    def test_create_new_alias_sprint(self):
        """`sprint new` is equivalent to `sprint create`."""
        result = self.test.run_cmd_json([
            "sprint", "new", "-r", self.roadmap,
            "-d", "Sprint created through the new alias",
        ])
        assert "id" in result and result["id"] > 0
        print("✓ 'new' alias = 'create' (sprint)")

    def test_history_hist_alias_audit(self):
        """`audit hist` is equivalent to `audit history`."""
        long = self.test.run_cmd_json([
            "audit", "history", "-r", self.roadmap, "TASK", str(self.task_id),
        ])
        short = self.test.run_cmd_json([
            "audit", "hist", "-r", self.roadmap, "TASK", str(self.task_id),
        ])
        assert len(long) == len(short)
        print("✓ 'hist' alias = 'history' (audit)")

    def test_update_upd_alias_sprint(self):
        """`sprint upd` is equivalent to `sprint update`."""
        sprint = self.test.run_cmd_json([
            "sprint", "create", "-r", self.roadmap, "-d", "Initial description",
        ])
        self.test.run_cmd([
            "sprint", "upd", "-r", self.roadmap, str(sprint["id"]),
            "-d", "Updated via upd alias",
        ])
        got = self.test.run_cmd_json(["sprint", "get", "-r", self.roadmap, str(sprint["id"])])
        assert got["description"] == "Updated via upd alias"
        print("✓ 'upd' alias = 'update' (sprint)")

    def test_task_status_stat_alias(self):
        """`task stat` is equivalent to `task set-status`. Both must reach DOING via sprint flow."""
        sprint = self.test.create_sprint(self.roadmap, "Status alias coverage sprint")
        self.test.move_task_to_sprint(self.roadmap, self.task_id, sprint)
        # Drive to DOING using stat (short alias); set-status would also work.
        self.test.run_cmd(["task", "stat", "-r", self.roadmap, str(self.task_id), "DOING"])
        self.test.assert_task_status(self.roadmap, self.task_id, "DOING")
        print("✓ 'stat' alias = 'set-status' (task)")

    def test_priority_prio_alias(self):
        """`task prio` is equivalent to `task set-priority`."""
        self.test.run_cmd(["task", "prio", "-r", self.roadmap, str(self.task_id), "7"])
        got = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.task_id)])[0]
        assert got["priority"] == 7
        print("✓ 'prio' alias = 'set-priority' (task)")

    def test_severity_sev_alias(self):
        """`task sev` is equivalent to `task set-severity`."""
        self.test.run_cmd(["task", "sev", "-r", self.roadmap, str(self.task_id), "6"])
        got = self.test.run_cmd_json(["task", "get", "-r", self.roadmap, str(self.task_id)])[0]
        assert got["severity"] == 6
        print("✓ 'sev' alias = 'set-severity' (task)")

    def test_sprint_task_aliases_add_rm_mv(self):
        """`add`, `rm-tasks`, `mv-tasks` are documented sprint subcommand aliases.

        Argument shapes per SPEC/COMMANDS.md:
          sprint add-tasks <sprint-id> <task-ids-csv>
          sprint move-tasks <from-sprint> <to-sprint> <task-ids-csv>
          sprint remove-tasks <sprint-id> <task-ids-csv>
        """
        sprint_a = self.test.create_sprint(self.roadmap, "Sprint A for alias add")
        sprint_b = self.test.create_sprint(self.roadmap, "Sprint B for alias mv")
        # add (alias of add-tasks): task IDs as CSV
        self.test.run_cmd([
            "sprint", "add", "-r", self.roadmap, str(sprint_a), str(self.task_id),
        ])
        self.test.assert_task_status(self.roadmap, self.task_id, "SPRINT")
        # mv-tasks (alias of move-tasks): from, to, csv
        self.test.run_cmd([
            "sprint", "mv-tasks", "-r", self.roadmap,
            str(sprint_a), str(sprint_b), str(self.task_id),
        ])
        # rm-tasks (alias of remove-tasks): sprint ID + csv
        self.test.run_cmd([
            "sprint", "rm-tasks", "-r", self.roadmap, str(sprint_b), str(self.task_id),
        ])
        self.test.assert_task_status(self.roadmap, self.task_id, "BACKLOG")
        print("✓ sprint aliases 'add', 'mv-tasks', 'rm-tasks' route to long forms")

    def test_sprint_position_aliases_mvto_btm_order(self):
        """`mvto`, `btm`, `order` are documented position-management aliases."""
        sprint = self.test.create_sprint(self.roadmap, "Position alias sprint")
        # Create 3 tasks and add them via a CSV arg
        ids = []
        for i in range(3):
            tid = self.test.create_task(
                self.roadmap,
                title=f"Position-alias probe task #{i+1}",
                functional_requirements=f"Need #{i+1}",
                technical_requirements=f"Tech #{i+1}",
                acceptance_criteria=f"AC #{i+1}",
            )
            ids.append(tid)
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", self.roadmap, str(sprint),
            ",".join(str(i) for i in ids),
        ])

        # order (alias of reorder): task IDs come as a CSV in a single argument
        self.test.run_cmd([
            "sprint", "order", "-r", self.roadmap, str(sprint),
            ",".join(str(i) for i in reversed(ids)),
        ])
        # mvto (alias of move-to): move first id to position 0
        self.test.run_cmd([
            "sprint", "mvto", "-r", self.roadmap, str(sprint), str(ids[0]), "0",
        ])
        # btm (alias of bottom): push ids[0] to the bottom
        self.test.run_cmd([
            "sprint", "btm", "-r", self.roadmap, str(sprint), str(ids[0]),
        ])

        # Final ordering: ids[0] must be last.
        stats = self.test.run_cmd_json(["sprint", "stats", "-r", self.roadmap, str(sprint)])
        assert stats["task_order"][-1] == ids[0], (
            f"after btm, ids[0]={ids[0]} should be last; got order={stats['task_order']}"
        )
        print("✓ sprint position aliases 'order', 'mvto', 'btm' route to long forms")


def main():
    """Run all alias coverage tests."""
    import inspect

    failures = []
    passed = 0
    classes = [
        ("TestTopLevelAliases", TestTopLevelAliases),
        ("TestSubcommandAliases", TestSubcommandAliases),
    ]
    for cls_name, cls in classes:
        for meth_name, meth in inspect.getmembers(cls, predicate=inspect.isfunction):
            if not meth_name.startswith("test_"):
                continue
            inst = cls()
            if hasattr(inst, "setup_method"):
                inst.setup_method()
            try:
                meth(inst)
                passed += 1
            except Exception as e:
                failures.append(f"{cls_name}.{meth_name}: {e}")
            if hasattr(inst, "teardown_method"):
                try:
                    inst.teardown_method()
                except Exception:
                    pass

    print(f"\n{passed} passed, {len(failures)} failed")
    for f in failures:
        print(f"  ✗ {f}")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
