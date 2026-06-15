#!/usr/bin/env python3
"""
Test 37: Persistence fidelity of the state-mutating commands.

For every command that changes state (task/sprint/roadmap create, edit, status
transitions, reopen, priority/severity, assign/unassign, dependencies, remove;
sprint create/update/start/close/reopen, add/remove/move-tasks, ordering,
remove; roadmap create/remove) this suite issues the command with realistic
input and then reads the state back to assert that what was PERSISTED matches
what was REQUESTED.

Regression guards for write-side defects:
  - sprint move-tasks must PRESERVE task status (it used to reset to SPRINT)
  - capacity / membership / BACKLOG-only guards must reject atomically and
    leave state unchanged
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestWritePersistenceFidelity:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    # ---- helpers -----------------------------------------------------------

    def _mk(self, roadmap, title, ttype="TASK", priority=0, severity=0, parent=None,
            specialists=None):
        cmd = ["task", "create", "-r", roadmap, "-t", title,
               "-fr", "Why " + title, "-tr", "How " + title, "-ac", "Verify " + title,
               "-y", ttype, "-p", str(priority), "--severity", str(severity)]
        if parent is not None:
            cmd += ["--parent", str(parent)]
        if specialists is not None:
            cmd += ["-sp", specialists]
        return self.test.run_cmd_json(cmd)["id"]

    def _get(self, roadmap, tid):
        res = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(tid)])
        assert isinstance(res, list) and res, f"task {tid} not found"
        return res[0]

    def _sprint(self, roadmap, sid):
        return self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sid)])

    def _order(self, roadmap, sid):
        return self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sid)])["task_order"]

    # ---- task create -------------------------------------------------------

    def test_create_persists_all_fields(self):
        r = self.test.create_roadmap("payments_create")
        fr = "As a user I need secure login — ünïcode & symbols <>&\"'"
        tr = "authorization_code + PKCE; rotate refresh tokens"
        ac = "Pen-test passes; 100% auth e2e green"
        tid = self.test.run_cmd_json([
            "task", "create", "-r", r, "-t", "  OAuth2 PKCE flow  ",
            "-y", "BUG", "-p", "7", "--severity", "8",
            "-fr", fr, "-tr", tr, "-ac", ac, "-sp", "Alice Silva,Bob Costa",
        ])["id"]
        t = self._get(r, tid)
        assert t["title"] == "OAuth2 PKCE flow", repr(t["title"])  # trimmed
        assert t["type"] == "BUG" and t["priority"] == 7 and t["severity"] == 8, t
        assert t["functional_requirements"] == fr, t["functional_requirements"]
        assert t["technical_requirements"] == tr
        assert t["acceptance_criteria"] == ac
        assert t["specialists"] == "Alice Silva,Bob Costa", t["specialists"]
        assert t["status"] == "BACKLOG" and t["parent_task_id"] is None
        self.test.assert_task_shape(t)
        print("✓ create persists every requested field (with trimming)")

    def test_create_defaults(self):
        r = self.test.create_roadmap("defaults_create")
        tid = self._mk(r, "Default task")  # only required + neutral defaults
        # create with truly only required fields
        bare = self.test.run_cmd_json(["task", "create", "-r", r, "-t", "Bare",
                                       "-fr", "f", "-tr", "t", "-ac", "a"])["id"]
        b = self._get(r, bare)
        assert b["type"] == "TASK" and b["priority"] == 0 and b["severity"] == 0, b
        assert b["status"] == "BACKLOG" and b["specialists"] is None and b["parent_task_id"] is None, b
        print("✓ create applies documented defaults")

    def test_create_subtask_parent_link(self):
        r = self.test.create_roadmap("subtask_create")
        parent = self._mk(r, "Parent epic", "EPIC")
        child = self._mk(r, "Child task", "SUB_TASK", parent=parent)
        assert self._get(r, child)["parent_task_id"] == parent
        assert self._get(r, parent)["subtask_count"] == 1
        print("✓ create --parent persists parent linkage")

    # ---- task edit ---------------------------------------------------------

    def test_edit_partial_preserves_others(self):
        r = self.test.create_roadmap("edit_partial")
        tid = self._mk(r, "Original", "TASK", priority=2, severity=2)
        before = self._get(r, tid)
        self.test.run_cmd(["task", "edit", "-r", r, str(tid), "-p", "9"])
        after = self._get(r, tid)
        assert after["priority"] == 9, after["priority"]
        for k in ("title", "type", "severity", "functional_requirements",
                  "technical_requirements", "acceptance_criteria"):
            assert after[k] == before[k], f"{k} changed unexpectedly: {before[k]} -> {after[k]}"
        print("✓ edit changes only the requested field")

    def test_edit_all_fields(self):
        r = self.test.create_roadmap("edit_all")
        tid = self._mk(r, "Old")
        self.test.run_cmd(["task", "edit", "-r", r, str(tid),
                           "-t", "New title", "-y", "REFACTOR", "-p", "5", "--severity", "6",
                           "-fr", "new fr", "-tr", "new tr", "-ac", "new ac", "-sp", "Carol Dias"])
        t = self._get(r, tid)
        assert t["title"] == "New title" and t["type"] == "REFACTOR", t
        assert t["priority"] == 5 and t["severity"] == 6, t
        assert t["functional_requirements"] == "new fr" and t["technical_requirements"] == "new tr"
        assert t["acceptance_criteria"] == "new ac" and t["specialists"] == "Carol Dias"
        print("✓ edit persists every requested field")

    # ---- status transitions / lifecycle timestamps ------------------------

    def test_status_transitions_set_timestamps(self):
        r = self.test.create_roadmap("lifecycle")
        s = self.test.create_sprint(r, "Sprint")
        self.test.run_cmd(["sprint", "start", "-r", r, str(s)])
        tid = self._mk(r, "Lifecycle task")
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s), str(tid)])

        t = self._get(r, tid)
        assert t["status"] == "SPRINT" and t["started_at"] is None, t

        self.test.run_cmd(["task", "stat", "-r", r, str(tid), "DOING"])
        t = self._get(r, tid)
        assert t["status"] == "DOING" and t["started_at"] and t["tested_at"] is None, t

        self.test.run_cmd(["task", "stat", "-r", r, str(tid), "TESTING"])
        t = self._get(r, tid)
        assert t["status"] == "TESTING" and t["tested_at"] and t["closed_at"] is None, t

        self.test.run_cmd(["task", "stat", "-r", r, str(tid), "COMPLETED",
                           "-s", "Shipped in v2.1; all green"])
        t = self._get(r, tid)
        assert t["status"] == "COMPLETED" and t["closed_at"], t
        assert t["completion_summary"] == "Shipped in v2.1; all green", t["completion_summary"]
        # ordering of lifecycle timestamps
        assert t["started_at"] <= t["tested_at"] <= t["closed_at"], t
        print("✓ status transitions persist the correct lifecycle timestamps")

    def test_reopen_clears_lifecycle(self):
        r = self.test.create_roadmap("reopen")
        s = self.test.create_sprint(r, "Sprint")
        self.test.run_cmd(["sprint", "start", "-r", r, str(s)])
        tid = self._mk(r, "Reopen me")
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s), str(tid)])
        for st in ("DOING", "TESTING", "COMPLETED"):
            self.test.run_cmd(["task", "stat", "-r", r, str(tid), st])
        self.test.run_cmd(["task", "reopen", "-r", r, str(tid)])
        t = self._get(r, tid)
        assert t["status"] == "BACKLOG", t["status"]
        assert t["started_at"] is None and t["tested_at"] is None, t
        assert t["closed_at"] is None and t["completion_summary"] is None, t
        print("✓ reopen clears status and all lifecycle timestamps")

    # ---- bulk priority / severity -----------------------------------------

    def test_bulk_priority_severity(self):
        r = self.test.create_roadmap("bulk")
        a = self._mk(r, "A"); b = self._mk(r, "B"); c = self._mk(r, "C")
        self.test.run_cmd(["task", "prio", "-r", r, f"{a},{b},{c}", "6"])
        self.test.run_cmd(["task", "sev", "-r", r, f"{a},{b}", "4"])
        for tid in (a, b, c):
            assert self._get(r, tid)["priority"] == 6, tid
        assert self._get(r, a)["severity"] == 4 and self._get(r, b)["severity"] == 4
        assert self._get(r, c)["severity"] == 0, "c severity untouched"
        print("✓ bulk priority/severity persist for every listed task")

    # ---- assign / unassign -------------------------------------------------

    def test_assign_unassign(self):
        r = self.test.create_roadmap("assign")
        tid = self._mk(r, "Assignable")
        self.test.run_cmd(["task", "assign", "-r", r, str(tid), "Dev One"])
        self.test.run_cmd(["task", "assign", "-r", r, str(tid), "Dev Two"])
        self.test.run_cmd(["task", "assign", "-r", r, str(tid), "Dev One"], check=False)  # dup
        sp = self._get(r, tid)["specialists"]
        names = [n for n in sp.split(",")] if sp else []
        assert names.count("Dev One") == 1, f"assign must be idempotent: {sp}"
        assert "Dev Two" in names, sp
        self.test.run_cmd(["task", "unassign", "-r", r, str(tid), "Dev One"])
        sp2 = self._get(r, tid)["specialists"]
        assert "Dev One" not in (sp2 or ""), sp2
        assert "Dev Two" in (sp2 or ""), sp2
        print("✓ assign (idempotent) / unassign persist correctly")

    # ---- dependencies ------------------------------------------------------

    def test_dependencies(self):
        r = self.test.create_roadmap("deps")
        a = self._mk(r, "A"); b = self._mk(r, "B")
        self.test.run_cmd(["task", "add-dep", "-r", r, str(b), str(a)])  # b depends on a
        assert self._get(r, b)["depends_on"] == [a], self._get(r, b)
        assert self._get(r, a)["blocks"] == [b], self._get(r, a)
        # cycle rejected
        rc, _, _ = self.test.run_cmd(["task", "add-dep", "-r", r, str(a), str(b)], check=False)
        assert rc != 0, "a->b dependency would create a cycle and must be rejected"
        assert self._get(r, a)["depends_on"] == [], "rejected cycle must not persist"
        self.test.run_cmd(["task", "remove-dep", "-r", r, str(b), str(a)])
        assert self._get(r, b)["depends_on"] == [], self._get(r, b)
        print("✓ add-dep/remove-dep persist; cycles rejected without persisting")

    # ---- task remove -------------------------------------------------------

    def test_remove_backlog_only(self):
        r = self.test.create_roadmap("remove")
        a = self._mk(r, "Backlog removable")
        self.test.run_cmd(["task", "remove", "-r", r, str(a)])
        # task get now fail-fasts with exit 4 on the removed (unknown) ID (#44).
        exit_code, _, _ = self.test.run_cmd(["task", "get", "-r", r, str(a)], check=False)
        assert exit_code == 4, f"removed task get must exit 4, got {exit_code}"
        # non-BACKLOG cannot be removed
        s = self.test.create_sprint(r, "S")
        self.test.run_cmd(["sprint", "start", "-r", r, str(s)])
        b = self._mk(r, "In sprint")
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s), str(b)])
        rc, _, _ = self.test.run_cmd(["task", "remove", "-r", r, str(b)], check=False)
        assert rc == 6, f"removing a non-BACKLOG task must exit 6, got {rc}"
        assert self._get(r, b)["status"] == "SPRINT", "rejected remove must leave the task intact"
        print("✓ remove deletes BACKLOG tasks; rejects others without side effects")

    # ---- sprint create / update -------------------------------------------

    def test_sprint_create_update(self):
        r = self.test.create_roadmap("sprint_cu")
        sid = self.test.run_cmd_json(["sprint", "create", "-r", r,
                                      "-d", "Sprint Alpha", "--max-tasks", "10"])["id"]
        s = self._sprint(r, sid)
        assert s["description"] == "Sprint Alpha" and s["max_tasks"] == 10, s
        assert s["status"] == "PENDING", s
        self.test.assert_sprint_shape(s)
        self.test.run_cmd(["sprint", "update", "-r", r, str(sid),
                           "-d", "Sprint Alpha (revised)", "--max-tasks", "15"])
        s = self._sprint(r, sid)
        assert s["description"] == "Sprint Alpha (revised)" and s["max_tasks"] == 15, s
        print("✓ sprint create/update persist description and capacity")

    def test_sprint_lifecycle_timestamps(self):
        r = self.test.create_roadmap("sprint_life")
        sid = self.test.create_sprint(r, "S")
        assert self._sprint(r, sid)["started_at"] is None
        self.test.run_cmd(["sprint", "start", "-r", r, str(sid)])
        s = self._sprint(r, sid)
        assert s["status"] == "OPEN" and s["started_at"] and s["closed_at"] is None, s
        started = s["started_at"]
        self.test.run_cmd(["sprint", "close", "-r", r, str(sid)])
        s = self._sprint(r, sid)
        assert s["status"] == "CLOSED" and s["closed_at"], s
        self.test.run_cmd(["sprint", "reopen", "-r", r, str(sid)])
        s = self._sprint(r, sid)
        assert s["status"] == "OPEN" and s["closed_at"] is None, "reopen clears closed_at"
        assert s["started_at"] == started, "reopen preserves started_at"
        print("✓ sprint start/close/reopen persist the right timestamps")

    # ---- sprint membership -------------------------------------------------

    def test_sprint_add_remove_tasks(self):
        r = self.test.create_roadmap("sprint_member")
        sid = self.test.create_sprint(r, "S")
        self.test.run_cmd(["sprint", "start", "-r", r, str(sid)])
        a = self._mk(r, "A"); b = self._mk(r, "B")
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(sid), str(a), str(b)])
        assert self._get(r, a)["status"] == "SPRINT" and self._get(r, b)["status"] == "SPRINT"
        assert self._order(r, sid) == [a, b], self._order(r, sid)
        self.test.run_cmd(["sprint", "remove-tasks", "-r", r, str(sid), str(a)])
        assert self._get(r, a)["status"] == "BACKLOG", "remove-tasks reverts to BACKLOG"
        assert self._order(r, sid) == [b], self._order(r, sid)
        print("✓ add-tasks/remove-tasks persist membership, status, and order")

    def test_move_tasks_preserves_status(self):
        """Regression: moving tasks between sprints must preserve their status."""
        r = self.test.create_roadmap("move_tasks")
        s1 = self.test.create_sprint(r, "S1")
        s2 = self.test.create_sprint(r, "S2")  # stays PENDING
        self.test.run_cmd(["sprint", "start", "-r", r, str(s1)])
        a = self._mk(r, "A"); b = self._mk(r, "B"); c = self._mk(r, "C")
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(s1), str(a), str(b), str(c)])
        self.test.run_cmd(["task", "stat", "-r", r, str(b), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", r, str(c), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", r, str(c), "TESTING"])
        # a=SPRINT, b=DOING, c=TESTING
        self.test.run_cmd(["sprint", "move-tasks", "-r", r, str(s1), str(s2),
                           f"{a},{b},{c}"])
        assert self._get(r, a)["status"] == "SPRINT", "SPRINT status preserved"
        assert self._get(r, b)["status"] == "DOING", "DOING status must be preserved on move"
        assert self._get(r, c)["status"] == "TESTING", "TESTING status must be preserved on move"
        # membership moved
        assert set(self._order(r, s2)) == {a, b, c}, self._order(r, s2)
        assert self._order(r, s1) == [], self._order(r, s1)
        # moving a task not in the source sprint is rejected atomically (exit 6)
        d = self._mk(r, "D")  # backlog, not in s2
        rc, _, _ = self.test.run_cmd(["sprint", "move-tasks", "-r", r, str(s2), str(s1), str(d)],
                                     check=False)
        assert rc == 6, f"moving a non-member must exit 6, got {rc}"
        assert self._order(r, s1) == [], "rejected move must not change destination"
        print("✓ move-tasks preserves status and moves membership atomically")

    # ---- sprint ordering ---------------------------------------------------

    def test_sprint_ordering(self):
        r = self.test.create_roadmap("ordering")
        sid = self.test.create_sprint(r, "S")
        self.test.run_cmd(["sprint", "start", "-r", r, str(sid)])
        ids = [self._mk(r, f"T{i}") for i in range(5)]
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(sid)] + [str(i) for i in ids])
        assert self._order(r, sid) == ids
        # reorder (CSV) to reverse
        self.test.run_cmd(["sprint", "reorder", "-r", r, str(sid), ",".join(str(i) for i in reversed(ids))])
        assert self._order(r, sid) == list(reversed(ids)), self._order(r, sid)
        # top / bottom
        self.test.run_cmd(["sprint", "top", "-r", r, str(sid), str(ids[0])])
        assert self._order(r, sid)[0] == ids[0], self._order(r, sid)
        self.test.run_cmd(["sprint", "bottom", "-r", r, str(sid), str(ids[1])])
        assert self._order(r, sid)[-1] == ids[1], self._order(r, sid)
        # swap first and last
        order = self._order(r, sid)
        self.test.run_cmd(["sprint", "swap", "-r", r, str(sid), str(order[0]), str(order[-1])])
        new = self._order(r, sid)
        assert new[0] == order[-1] and new[-1] == order[0], new
        # move-to position 0
        self.test.run_cmd(["sprint", "move-to", "-r", r, str(sid), str(ids[2]), "0"])
        assert self._order(r, sid)[0] == ids[2], self._order(r, sid)
        print("✓ reorder/top/bottom/swap/move-to persist ordering")

    def test_sprint_remove_reverts_members(self):
        r = self.test.create_roadmap("sprint_remove")
        sid = self.test.create_sprint(r, "S")
        self.test.run_cmd(["sprint", "start", "-r", r, str(sid)])
        a = self._mk(r, "A")
        self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(sid), str(a)])
        self.test.run_cmd(["task", "stat", "-r", r, str(a), "DOING"])
        self.test.run_cmd(["sprint", "remove", "-r", r, str(sid)])
        assert self._get(r, a)["status"] == "BACKLOG", "sprint remove must revert members to BACKLOG"
        sprints = self.test.run_cmd_json(["sprint", "list", "-r", r])
        assert all(s["id"] != sid for s in (sprints or [])), "removed sprint must be gone"
        print("✓ sprint remove reverts members to BACKLOG and deletes the sprint")

    # ---- capacity guard ----------------------------------------------------

    def test_add_tasks_capacity_guard(self):
        r = self.test.create_roadmap("capacity")
        sid = self.test.run_cmd_json(["sprint", "create", "-r", r, "-d", "Capped",
                                      "--max-tasks", "2"])["id"]
        self.test.run_cmd(["sprint", "start", "-r", r, str(sid)])
        ids = [self._mk(r, f"C{i}") for i in range(3)]
        rc, _, _ = self.test.run_cmd(["sprint", "add-tasks", "-r", r, str(sid)] + [str(i) for i in ids],
                                     check=False)
        assert rc == 6, f"over-capacity add must exit 6, got {rc}"
        assert self._order(r, sid) == [], "rejected add must be atomic (no partial membership)"
        for tid in ids:
            assert self._get(r, tid)["status"] == "BACKLOG", "tasks must stay in BACKLOG"
        print("✓ add-tasks capacity guard rejects atomically")

    # ---- roadmap -----------------------------------------------------------

    def test_roadmap_create_remove(self):
        r = self.test.create_roadmap("rm_target")
        names = {x["name"] for x in self.test.run_cmd_json(["roadmap", "list"])}
        assert r in names, names
        self.test.run_cmd(["roadmap", "remove", r])
        names2 = {x["name"] for x in (self.test.run_cmd_json(["roadmap", "list"]) or [])}
        assert r not in names2, f"removed roadmap must be gone: {names2}"
        print("✓ roadmap create/remove persist")


def main():
    test = TestWritePersistenceFidelity()
    methods = sorted(m for m in dir(test) if m.startswith("test_"))
    passed = 0
    failed = 0
    for name in methods:
        test.setup_method()
        try:
            getattr(test, name)()
            passed += 1
        except Exception as e:
            print(f"✗ {name} failed: {e}")
            failed += 1
        finally:
            test.teardown_method()
    print(f"\n{passed} passed, {failed} failed")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
