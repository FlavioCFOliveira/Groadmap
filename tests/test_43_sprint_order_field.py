#!/usr/bin/env python3
"""
Test 43: Sprint `order` field — exhaustive E2E coverage.

The sprint `order` field (JSON key "order"; DB column "order_index") is a
positive integer (> 0), unique across all sprints in a roadmap, that records
the intended execution sequence. This test module validates every path
specified in SPEC/COMMANDS.md § Create Sprint / § Update Sprint and
SPEC/STATE_MACHINE.md § Sprint Order Immutability.

Coverage matrix
---------------
1.  Auto-assignment on successive creates → 1, 2, 3 (MAX+1 rule).
2.  Explicit --order on create → stored verbatim.
3.  Explicit --order duplicate on create → exit 5.
4.  Invalid --order on create: 0, -3, abc → exit 6.
5.  --order update on PENDING sprint → succeeds, value persists in sprint get.
6.  --order update on OPEN sprint → succeeds, value persists.
7.  --order update on CLOSED sprint → exit 6, value unchanged.
8.  --order update to value already used by another sprint → exit 5.
9.  sprint get / sprint list include "order" with correct values.
10. audit history shows SPRINT_UPDATE after an order change.
11. Help text: sprint create --help and sprint update --help mention --order;
    sprint create --ai-help flags array contains --order.
"""

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase  # noqa: E402


EXIT_OK = 0
EXIT_EXISTS = 5   # ErrAlreadyExists — duplicate order
EXIT_INVALID = 6  # ErrValidation — bad order value or CLOSED state


class TestSprintOrderField:
    """End-to-end tests for the sprint execution order field."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    # ------------------------------------------------------------------ helpers

    def _create_sprint(self, title: str, description: str, **extra_flags) -> int:
        """Create a sprint and return its id. Extra keyword args become CLI flags
        (e.g. order=5 → ["--order", "5"])."""
        cmd = [
            "sprint", "create",
            "-r", self.roadmap,
            "-t", title,
            "-d", description,
        ]
        for flag, value in extra_flags.items():
            cmd.extend([f"--{flag}", str(value)])
        result = self.test.run_cmd_json(cmd)
        return result["id"]

    def _get_sprint(self, sprint_id: int) -> dict:
        return self.test.run_cmd_json(["sprint", "get", "-r", self.roadmap, str(sprint_id)])

    def _list_sprints(self) -> list:
        return self.test.run_cmd_json(["sprint", "list", "-r", self.roadmap])

    def _update_sprint(self, sprint_id: int, **flags):
        """Run sprint update with arbitrary flags; returns (exit_code, stdout, stderr)."""
        cmd = ["sprint", "update", "-r", self.roadmap, str(sprint_id)]
        for flag, value in flags.items():
            cmd.extend([f"--{flag}", str(value)])
        return self.test.run_cmd(cmd, check=False)

    def _start_sprint(self, sprint_id: int):
        self.test.run_cmd(["sprint", "start", "-r", self.roadmap, str(sprint_id)])

    def _close_sprint(self, sprint_id: int):
        # Close requires no active (non-COMPLETED) tasks; force-close is safe here.
        self.test.run_cmd(["sprint", "close", "-r", self.roadmap, str(sprint_id)])

    # ================================================================ Test 1
    def test_auto_assign_order_successive_creates(self):
        """Successive sprint creates without --order auto-assign 1, 2, 3."""
        id1 = self._create_sprint(
            "Authentication Hardening",
            "Implement MFA and session token rotation",
        )
        id2 = self._create_sprint(
            "Observability Foundation",
            "Integrate structured logging and distributed tracing",
        )
        id3 = self._create_sprint(
            "Performance Optimisation",
            "Profile hot paths and reduce p99 latency",
        )

        # Verify via sprint get
        assert self._get_sprint(id1)["order"] == 1, (
            f"first sprint order expected 1, got {self._get_sprint(id1)['order']}"
        )
        assert self._get_sprint(id2)["order"] == 2, (
            f"second sprint order expected 2, got {self._get_sprint(id2)['order']}"
        )
        assert self._get_sprint(id3)["order"] == 3, (
            f"third sprint order expected 3, got {self._get_sprint(id3)['order']}"
        )

    # ================================================================ Test 2
    def test_explicit_order_on_create_is_stored(self):
        """An explicit --order value on create is stored verbatim and readable
        from sprint get."""
        sid = self._create_sprint(
            "Security Audit Remediation",
            "Address all findings from the external security review",
            order=7,
        )
        sprint = self._get_sprint(sid)
        assert sprint["order"] == 7, (
            f"explicit --order 7 not stored; got {sprint['order']}"
        )

    # ================================================================ Test 3
    def test_duplicate_order_on_create_exits_5(self):
        """Creating a sprint with an --order already in use by another sprint
        must fail with exit 5 (ErrAlreadyExists) and an informative stderr message."""
        self._create_sprint(
            "Data Migration Sprint",
            "Migrate legacy user table to the new schema",
            order=3,
        )

        code, _, stderr = self.test.run_cmd(
            [
                "sprint", "create",
                "-r", self.roadmap,
                "-t", "Conflicting Sprint",
                "-d", "Attempt to claim the same execution slot",
                "--order", "3",
            ],
            check=False,
        )
        assert code == EXIT_EXISTS, (
            f"duplicate --order 3 must exit 5, got {code}"
        )
        assert "already in use" in stderr.lower() or "already exists" in stderr.lower(), (
            f"stderr should mention the collision; got: {stderr!r}"
        )

    # ================================================================ Test 4a
    def test_order_zero_on_create_exits_6(self):
        """--order 0 on create must be rejected with exit 6 (ErrValidation)."""
        code, _, stderr = self.test.run_cmd(
            [
                "sprint", "create",
                "-r", self.roadmap,
                "-t", "Invalid Zero Order",
                "-d", "Should be rejected — order 0 is not a positive integer",
                "--order", "0",
            ],
            check=False,
        )
        assert code == EXIT_INVALID, f"--order 0 must exit 6, got {code}"
        assert "positive" in stderr.lower() or "greater than zero" in stderr.lower(), (
            f"stderr should mention positive-integer constraint; got: {stderr!r}"
        )

    # ================================================================ Test 4b
    def test_order_negative_on_create_exits_6(self):
        """--order -3 on create must be rejected with exit 6 (ErrValidation)."""
        code, _, stderr = self.test.run_cmd(
            [
                "sprint", "create",
                "-r", self.roadmap,
                "-t", "Negative Order Sprint",
                "-d", "Should be rejected — negative order is invalid",
                "--order", "-3",
            ],
            check=False,
        )
        assert code == EXIT_INVALID, f"--order -3 must exit 6, got {code}"

    # ================================================================ Test 4c
    def test_order_non_integer_on_create_exits_6(self):
        """--order abc on create must be rejected with exit 6 (ErrValidation)."""
        code, _, stderr = self.test.run_cmd(
            [
                "sprint", "create",
                "-r", self.roadmap,
                "-t", "Alpha Order Sprint",
                "-d", "Should be rejected — non-integer order is invalid",
                "--order", "abc",
            ],
            check=False,
        )
        assert code == EXIT_INVALID, f"--order abc must exit 6, got {code}"
        assert "positive" in stderr.lower() or "integer" in stderr.lower(), (
            f"stderr should mention integer requirement; got: {stderr!r}"
        )

    # ================================================================ Test 5
    def test_order_update_on_pending_sprint_succeeds(self):
        """Updating --order on a PENDING sprint succeeds and the new value is
        immediately visible in sprint get."""
        sid = self._create_sprint(
            "Reliability Sprint",
            "Add circuit breakers and retry budgets to all outbound calls",
        )
        assert self._get_sprint(sid)["order"] == 1, "auto-assigned order should be 1"

        code, _, _ = self._update_sprint(sid, order=9)
        assert code == EXIT_OK, f"order update on PENDING sprint must succeed (exit 0), got {code}"

        updated = self._get_sprint(sid)
        assert updated["order"] == 9, (
            f"order after PENDING update expected 9, got {updated['order']}"
        )

    # ================================================================ Test 6
    def test_order_update_on_open_sprint_succeeds(self):
        """Updating --order on an OPEN sprint succeeds and the new value persists."""
        sid = self._create_sprint(
            "API Versioning Sprint",
            "Introduce v2 endpoints while keeping v1 stable",
        )
        self._start_sprint(sid)

        code, _, _ = self._update_sprint(sid, order=4)
        assert code == EXIT_OK, f"order update on OPEN sprint must succeed (exit 0), got {code}"

        updated = self._get_sprint(sid)
        assert updated["order"] == 4, (
            f"order after OPEN update expected 4, got {updated['order']}"
        )

    # ================================================================ Test 7
    def test_order_update_on_closed_sprint_exits_6(self):
        """Updating --order on a CLOSED sprint must be rejected with exit 6 and
        the order value must remain unchanged."""
        sid = self._create_sprint(
            "Infrastructure Hardening",
            "Upgrade all runtime dependencies and patch known CVEs",
        )
        original_order = self._get_sprint(sid)["order"]

        self._start_sprint(sid)
        self._close_sprint(sid)

        code, _, stderr = self._update_sprint(sid, order=8)
        assert code == EXIT_INVALID, (
            f"order update on CLOSED sprint must exit 6, got {code}"
        )
        assert "closed" in stderr.lower(), (
            f"stderr should mention CLOSED state; got: {stderr!r}"
        )

        # Order must be unchanged
        after = self._get_sprint(sid)
        assert after["order"] == original_order, (
            f"order changed after rejected update: expected {original_order}, got {after['order']}"
        )

    # ================================================================ Test 7b — reopen restores editability
    def test_order_update_allowed_after_reopen(self):
        """A CLOSED sprint that is reopened (→ OPEN) becomes editable again;
        --order update must then succeed."""
        sid = self._create_sprint(
            "Compliance Hardening",
            "Implement GDPR data-deletion pipeline and audit trail",
        )
        self._start_sprint(sid)
        self._close_sprint(sid)

        # Reopen — sprint becomes OPEN again
        self.test.run_cmd(["sprint", "reopen", "-r", self.roadmap, str(sid)])

        code, _, _ = self._update_sprint(sid, order=6)
        assert code == EXIT_OK, (
            f"order update on reopened (OPEN) sprint must succeed, got {code}"
        )
        assert self._get_sprint(sid)["order"] == 6, "order not updated after reopen"

    # ================================================================ Test 8
    def test_order_update_duplicate_exits_5(self):
        """Updating --order to a value already used by another sprint must fail
        with exit 5 (ErrAlreadyExists)."""
        sid1 = self._create_sprint(
            "Cache Invalidation Rework",
            "Replace TTL-based cache with event-driven invalidation",
        )
        sid2 = self._create_sprint(
            "Search Index Rebuild",
            "Re-index the product catalogue with the new tokeniser",
        )

        # sprint 1 has order 1; try to update sprint 2 to the same value
        code, _, stderr = self._update_sprint(sid2, order=self._get_sprint(sid1)["order"])
        assert code == EXIT_EXISTS, (
            f"duplicate --order update must exit 5, got {code}"
        )
        assert "already in use" in stderr.lower() or "already exists" in stderr.lower(), (
            f"stderr should mention collision; got: {stderr!r}"
        )

    # ================================================================ Test 9a
    def test_sprint_get_includes_order_field(self):
        """sprint get output must include the 'order' key with the correct value."""
        sid = self._create_sprint(
            "Deployment Pipeline Overhaul",
            "Migrate from Jenkins to GitHub Actions with canary deployments",
            order=5,
        )
        sprint = self._get_sprint(sid)
        assert "order" in sprint, f"sprint get JSON missing 'order' key; keys: {list(sprint.keys())}"
        assert sprint["order"] == 5, f"sprint get 'order' expected 5, got {sprint['order']}"

    # ================================================================ Test 9b
    def test_sprint_list_includes_order_field_with_correct_values(self):
        """sprint list output must include 'order' in each sprint object with
        the values assigned at creation time (or updated)."""
        id_a = self._create_sprint(
            "Rate Limiting Rollout",
            "Enforce per-client rate limits on all public API routes",
            order=10,
        )
        id_b = self._create_sprint(
            "Webhook Delivery Guarantees",
            "Add retry + dead-letter queue for failed webhook dispatches",
            order=20,
        )
        id_c = self._create_sprint(
            "Multi-Region Failover",
            "Configure active-passive failover between us-east-1 and eu-west-1",
            order=30,
        )

        sprints = self._list_sprints()
        by_id = {s["id"]: s for s in sprints}

        assert id_a in by_id, "sprint A missing from list"
        assert id_b in by_id, "sprint B missing from list"
        assert id_c in by_id, "sprint C missing from list"

        assert by_id[id_a]["order"] == 10, f"sprint A order expected 10, got {by_id[id_a]['order']}"
        assert by_id[id_b]["order"] == 20, f"sprint B order expected 20, got {by_id[id_b]['order']}"
        assert by_id[id_c]["order"] == 30, f"sprint C order expected 30, got {by_id[id_c]['order']}"

        # Validate the full sprint shape now that both 'title' and 'order' are present
        for sid, sprint in by_id.items():
            self.test.assert_sprint_shape(sprint)

    # ================================================================ Test 10
    def test_audit_history_shows_sprint_update_after_order_change(self):
        """After an --order update, audit history for the sprint must contain
        at least one SPRINT_UPDATE entry."""
        sid = self._create_sprint(
            "Contract Testing Sprint",
            "Add provider-side Pact contracts for every consumer integration",
        )

        # Confirm there is no SPRINT_UPDATE entry before the update
        history_before = self.test.run_cmd_json(
            ["audit", "history", "-r", self.roadmap, "SPRINT", str(sid)]
        )
        ops_before = [e["operation"] for e in history_before]
        assert "SPRINT_UPDATE" not in ops_before, (
            f"unexpected SPRINT_UPDATE before order change: {ops_before}"
        )

        # Perform the order change
        code, _, _ = self._update_sprint(sid, order=15)
        assert code == EXIT_OK, f"order update must succeed, got {code}"

        # Verify SPRINT_UPDATE appears in audit history
        history_after = self.test.run_cmd_json(
            ["audit", "history", "-r", self.roadmap, "SPRINT", str(sid)]
        )
        ops_after = [e["operation"] for e in history_after]
        assert "SPRINT_UPDATE" in ops_after, (
            f"SPRINT_UPDATE not found in audit history after order change; got: {ops_after}"
        )

        # Verify the audit entry references the correct entity type and id
        update_entries = [
            e for e in history_after
            if e["operation"] == "SPRINT_UPDATE"
        ]
        assert any(e["entity_id"] == sid for e in update_entries), (
            f"no SPRINT_UPDATE entry references sprint id {sid}; entries: {update_entries}"
        )
        assert all(e["entity_type"] == "SPRINT" for e in update_entries), (
            f"SPRINT_UPDATE entry has wrong entity_type: {update_entries}"
        )

    # ================================================================ Test 11a
    def test_sprint_create_help_mentions_order(self):
        """sprint create --help must document --order in its output."""
        _, stdout, stderr = self.test.run_cmd(
            ["sprint", "create", "--help"], check=False
        )
        combined = (stdout + stderr).lower()
        assert "--order" in combined, (
            f"'sprint create --help' does not mention --order;\n  stdout={stdout!r}\n  stderr={stderr!r}"
        )
        # Must describe the positive-integer constraint
        assert "positive" in combined or "> 0" in combined or "greater than zero" in combined, (
            f"'sprint create --help' does not describe the positive-integer constraint for --order"
        )

    # ================================================================ Test 11b
    def test_sprint_update_help_mentions_order(self):
        """sprint update --help must document --order and state immutability on CLOSED."""
        _, stdout, stderr = self.test.run_cmd(
            ["sprint", "update", "--help"], check=False
        )
        combined = (stdout + stderr).lower()
        assert "--order" in combined, (
            f"'sprint update --help' does not mention --order;\n  stdout={stdout!r}\n  stderr={stderr!r}"
        )
        assert "closed" in combined, (
            f"'sprint update --help' does not mention CLOSED immutability for --order"
        )

    # ================================================================ Test 11c
    def test_sprint_create_ai_help_includes_order_flag(self):
        """sprint create --ai-help must list --order as a flag in the JSON
        contract with type 'integer' and required=false."""
        _, stdout, _ = self.test.run_cmd(
            ["sprint", "create", "--ai-help"], check=False
        )
        # --ai-help returns the global JSON contract; parse it and find sprint create
        try:
            contract = json.loads(stdout)
        except json.JSONDecodeError as exc:
            raise AssertionError(
                f"sprint create --ai-help did not return valid JSON: {exc}\n  stdout={stdout[:400]!r}"
            ) from exc

        # Navigate: commands[] → name=sprint → subcommands[] → name=create
        sprint_create_cmd = None
        for cmd in contract.get("commands", []):
            if cmd.get("name") == "sprint":
                for sub in cmd.get("subcommands", []):
                    if sub.get("name") == "create":
                        sprint_create_cmd = sub
                        break

        assert sprint_create_cmd is not None, (
            "Could not find sprint > create in --ai-help JSON"
        )

        flags = sprint_create_cmd.get("flags", [])
        order_flags = [f for f in flags if f.get("long") == "--order"]
        assert order_flags, (
            f"--order flag not found in sprint create --ai-help flags; flags present: "
            f"{[f.get('long') for f in flags]}"
        )
        order_flag = order_flags[0]
        assert order_flag.get("required") is False, (
            f"--order must be optional (required=false); got {order_flag.get('required')}"
        )
        assert order_flag.get("type") == "integer", (
            f"--order must have type 'integer'; got {order_flag.get('type')!r}"
        )

    # ================================================================ Additional edge cases

    def test_auto_assign_skips_explicit_orders(self):
        """When explicit orders are used, auto-assignment uses MAX(existing)+1
        so it never collides, even with gaps in the sequence."""
        # Create sprint at order 5 explicitly
        self._create_sprint(
            "Database Schema Migration",
            "Apply pending Flyway migrations to production database",
            order=5,
        )
        # Next auto-assigned sprint should get order 6 (MAX=5, so MAX+1=6)
        id_auto = self._create_sprint(
            "Post-Migration Smoke Tests",
            "Run full regression suite against the migrated schema",
        )
        assert self._get_sprint(id_auto)["order"] == 6, (
            f"auto-assigned order after explicit order 5 should be 6, "
            f"got {self._get_sprint(id_auto)['order']}"
        )

    def test_order_field_present_after_update(self):
        """After a successful --order update, the sprint get response still
        includes the 'order' key (field not accidentally dropped on update)."""
        sid = self._create_sprint(
            "Incident Response Runbooks",
            "Document and automate all runbooks in the on-call rotation",
        )
        code, _, _ = self._update_sprint(sid, order=12)
        assert code == EXIT_OK

        sprint = self._get_sprint(sid)
        assert "order" in sprint, "order field missing from sprint get after update"
        assert sprint["order"] == 12

    def test_update_order_invalid_zero_on_open_sprint_exits_6(self):
        """--order 0 on update is rejected (exit 6) regardless of sprint status."""
        sid = self._create_sprint(
            "Load Testing Campaign",
            "Stress-test all critical endpoints at 10x expected peak traffic",
        )
        self._start_sprint(sid)

        code, _, _ = self._update_sprint(sid, order=0)
        assert code == EXIT_INVALID, (
            f"--order 0 update on OPEN sprint must exit 6, got {code}"
        )

    def test_order_remains_after_title_only_update(self):
        """Updating only --title must not alter the sprint's order value."""
        sid = self._create_sprint(
            "Initial Title",
            "Sprint description covering observability work",
            order=3,
        )
        # Update title only — order must not change
        code, _, _ = self.test.run_cmd(
            [
                "sprint", "update",
                "-r", self.roadmap,
                str(sid),
                "-t", "Revised Observability Sprint",
            ],
            check=False,
        )
        assert code == EXIT_OK, f"title-only update must succeed, got {code}"

        sprint = self._get_sprint(sid)
        assert sprint["order"] == 3, (
            f"order changed after title-only update; expected 3, got {sprint['order']}"
        )
        assert sprint["title"] == "Revised Observability Sprint", "title not updated"


# ------------------------------------------------------------------- runner

def _run_all():
    cls = TestSprintOrderField
    methods = sorted(m for m in dir(cls) if m.startswith("test_"))
    passed = 0
    failed = 0
    failures = []
    for m in methods:
        instance = cls()
        instance.setup_method()
        try:
            getattr(instance, m)()
            passed += 1
            print(f"  PASS  {m}")
        except AssertionError as exc:
            failed += 1
            failures.append((m, exc))
            print(f"  FAIL  {m}: {exc}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((m, exc))
            print(f"  ERROR {m}: {exc}")
        finally:
            instance.teardown_method()

    print()
    print("=" * 60)
    print(f"Sprint order field tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n  FAIL  {name}")
        print(f"        {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
