#!/usr/bin/env python3
"""
Test 43: Sprint `order` field — exhaustive E2E coverage.

The sprint `order` field (JSON key "order"; DB column "order_index") is a
positive integer (> 0), unique across all sprints in a roadmap, that records
the intended execution sequence. This test module validates every path
specified in SPEC/COMMANDS.md § Create Sprint / § Update Sprint /
§ List Sprints (Result Ordering) and SPEC/STATE_MACHINE.md § Sprint Order
Immutability.

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
10. audit history shows SPRINT_ORDER_CHANGE after an order change.
11. Help text: sprint create --help and sprint update --help mention --order;
    sprint create --ai-help flags array contains --order.
12. `sprint list` result ordering (rmp task #281): the array is ordered by
    `order` ASCENDING even when creation order disagrees with it; `--status`
    narrows the array as a subsequence without reordering it; two
    consecutive reads over unchanged data are identical; both help surfaces
    (`sprint list --help` and its `--ai-help` contract entry) state the
    guarantee.
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

    def _list_sprints(self, status: str = None) -> list:
        cmd = ["sprint", "list", "-r", self.roadmap]
        if status is not None:
            cmd.extend(["--status", status])
        return self.test.run_cmd_json(cmd)

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
    def test_audit_history_shows_sprint_order_change_after_order_change(self):
        """After an --order update, audit history for the sprint must contain
        at least one SPRINT_ORDER_CHANGE entry.

        The entry names the field the invocation supplied. The generic
        SPRINT_UPDATE it replaced is LEGACY and no command writes it
        (SPEC/COMMANDS.md § Update Sprint)."""
        sid = self._create_sprint(
            "Contract Testing Sprint",
            "Add provider-side Pact contracts for every consumer integration",
        )

        # Confirm there is no SPRINT_ORDER_CHANGE entry before the update
        history_before = self.test.run_cmd_json(
            ["audit", "history", "-r", self.roadmap, "SPRINT", str(sid)]
        )
        ops_before = [e["operation"] for e in history_before]
        assert "SPRINT_ORDER_CHANGE" not in ops_before, (
            f"unexpected SPRINT_ORDER_CHANGE before order change: {ops_before}"
        )

        # Perform the order change
        code, _, _ = self._update_sprint(sid, order=15)
        assert code == EXIT_OK, f"order update must succeed, got {code}"

        # Verify SPRINT_ORDER_CHANGE appears in audit history
        history_after = self.test.run_cmd_json(
            ["audit", "history", "-r", self.roadmap, "SPRINT", str(sid)]
        )
        ops_after = [e["operation"] for e in history_after]
        assert "SPRINT_ORDER_CHANGE" in ops_after, (
            f"SPRINT_ORDER_CHANGE not found in audit history after order change; got: {ops_after}"
        )
        assert "SPRINT_UPDATE" not in ops_after, (
            f"SPRINT_UPDATE is LEGACY and must never be written; got: {ops_after}"
        )

        # Verify the audit entry references the correct entity type and id
        update_entries = [
            e for e in history_after
            if e["operation"] == "SPRINT_ORDER_CHANGE"
        ]
        assert any(e["entity_id"] == sid for e in update_entries), (
            f"no SPRINT_ORDER_CHANGE entry references sprint id {sid}; entries: {update_entries}"
        )
        assert all(e["entity_type"] == "SPRINT" for e in update_entries), (
            f"SPRINT_ORDER_CHANGE entry has wrong entity_type: {update_entries}"
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


    # ================================================================ Result Ordering (rmp task #281)
    #
    # SPEC/COMMANDS.md § List Sprints "Result Ordering" makes the following
    # a published guarantee, not an artefact of the query plan:
    #   - the array is ordered by `order` ASCENDING (lowest first);
    #   - the ordering is TOTAL (order is NOT NULL and unique per roadmap),
    #     so the sequence is deterministic and needs no tie-break;
    #   - `--status` narrows the array; it never reorders it.

    def test_list_returns_ascending_order_when_creation_order_disagrees(self):
        """`sprint list` must return sprints ordered by ascending `order`, not
        by creation order. The fixture creates four sprints whose creation
        sequence deliberately DISAGREES with their `order` values, so a
        listing that (incorrectly) fell back to creation order — e.g. the
        prior `created_at DESC` behaviour, or plain insertion order — would
        produce a visibly different sequence and this test would catch it,
        rather than passing by accident."""
        # Created 1st, but given the HIGHEST order (40) — must land LAST.
        id_zeta = self._create_sprint(
            "Zeta Release Hardening",
            "Stabilise the release pipeline ahead of the v4 cut",
            order=40,
        )
        # Created 2nd, but given the LOWEST order (10) — must land FIRST.
        id_alpha = self._create_sprint(
            "Alpha Authentication Rework",
            "Replace session cookies with rotating short-lived tokens",
            order=10,
        )
        # Created 3rd, given order 30 — must land 3rd.
        id_gamma = self._create_sprint(
            "Gamma Data Warehouse Sync",
            "Build the incremental CDC pipeline into the analytics warehouse",
            order=30,
        )
        # Created 4th, given order 20 — must land 2nd.
        id_beta = self._create_sprint(
            "Beta Checkout Latency",
            "Cut p99 checkout latency below 300ms under Black Friday load",
            order=20,
        )

        creation_sequence = [id_zeta, id_alpha, id_gamma, id_beta]
        expected_order_sequence = [id_alpha, id_beta, id_gamma, id_zeta]

        # Fixture sanity: creation order and order-value order must genuinely
        # disagree, otherwise the assertion below would pass whether or not
        # `sprint list` actually honours `order`, proving nothing.
        assert creation_sequence != expected_order_sequence, (
            "fixture bug: creation sequence accidentally matches the expected "
            f"order-ascending sequence ({creation_sequence}); this test would "
            "be vacuous — scramble the --order values relative to creation "
            "order so the two disagree"
        )

        sprints = self._list_sprints()
        returned_sequence = [s["id"] for s in sprints]

        assert returned_sequence == expected_order_sequence, (
            "sprint list must return sprints ordered by 'order' ascending "
            f"({expected_order_sequence}, i.e. order 10, 20, 30, 40); got "
            f"{returned_sequence} (creation sequence was {creation_sequence})"
        )
        # And, explicitly, the listing must NOT merely coincide with
        # creation order — the two sequences are different by construction,
        # so recovering creation order here would itself be a failure.
        assert returned_sequence != creation_sequence, (
            f"sprint list returned creation order {returned_sequence} instead "
            "of order-ascending order — it is ignoring the 'order' field"
        )

    def test_status_filter_narrows_without_reordering(self):
        """`--status` must narrow the array without changing the relative
        sequence of the sprints it keeps: the filtered sequence must be the
        unfiltered sequence with non-matching sprints simply removed, checked
        as a subsequence rather than merely as a set (so a filter that
        happened to also re-sort its matches would still be caught).

        Only one sprint per roadmap may be OPEN at a time (task #77), so the
        fixture drives S1 and S5 through OPEN and back to CLOSED before
        opening S2 (which is left OPEN), giving a two-member CLOSED group,
        a two-member PENDING group, and a one-member OPEN group — enough
        for the subsequence check to be meaningful on the multi-member
        groups while still covering the single-member case."""
        # Creation order deliberately disagrees with `order` here too, so
        # this fixture also cannot pass by falling back to insertion order.
        id_s1 = self._create_sprint(
            "S1 Legacy API Sunset",
            "Retire the v1 REST endpoints after the v2 migration completes",
            order=50,
        )
        id_s2 = self._create_sprint(
            "S2 Real-Time Notifications",
            "Move push notifications from polling to a WebSocket channel",
            order=10,
        )
        id_s3 = self._create_sprint(
            "S3 Onboarding Funnel Redesign",
            "Rebuild the first-run onboarding flow to cut drop-off by half",
            order=40,
        )
        id_s4 = self._create_sprint(
            "S4 Billing Reconciliation",
            "Automate nightly reconciliation between Stripe and the ledger",
            order=20,
        )
        id_s5 = self._create_sprint(
            "S5 Search Relevance Tuning",
            "Retrain the ranking model on six months of click-through data",
            order=30,
        )

        # S1 and S5 pass through OPEN and land on CLOSED; S3 and S4 stay
        # PENDING (auto-assigned status on create); S2 is opened last and
        # left OPEN — never more than one sprint OPEN at once.
        self._start_sprint(id_s1)
        self._close_sprint(id_s1)
        self._start_sprint(id_s5)
        self._close_sprint(id_s5)
        self._start_sprint(id_s2)

        # Order-ascending across ALL statuses: S2(10), S4(20), S5(30), S3(40), S1(50).
        expected_unfiltered = [id_s2, id_s4, id_s5, id_s3, id_s1]

        unfiltered = self._list_sprints()
        unfiltered_sequence = [s["id"] for s in unfiltered]
        assert unfiltered_sequence == expected_unfiltered, (
            f"unfiltered listing expected {expected_unfiltered} (order-ascending), "
            f"got {unfiltered_sequence}"
        )

        for status, expected_members in (
            ("OPEN", [id_s2]),
            ("CLOSED", [id_s5, id_s1]),
            ("PENDING", [id_s4, id_s3]),
        ):
            filtered = self._list_sprints(status=status)
            filtered_sequence = [s["id"] for s in filtered]

            assert set(filtered_sequence) == set(expected_members), (
                f"--status {status} must select exactly {expected_members}, "
                f"got {filtered_sequence}"
            )

            # Subsequence check: the filtered sequence must equal the
            # unfiltered sequence with non-matching ids simply dropped, so
            # the relative order among the survivors is provably unchanged
            # rather than merely coincidentally right for a 2-element result.
            subsequence_of_unfiltered = [
                sid for sid in unfiltered_sequence if sid in expected_members
            ]
            assert filtered_sequence == subsequence_of_unfiltered, (
                f"--status {status} must narrow without reordering: expected "
                f"the unfiltered sequence with non-matches removed "
                f"({subsequence_of_unfiltered}), got {filtered_sequence}"
            )

    def test_list_is_deterministic_across_repeated_reads(self):
        """Two consecutive `sprint list` reads over unchanged data must
        return the identical sequence: the ordering is total (order is
        NOT NULL and unique per roadmap), so repeating the read cannot
        change the result. The fixture again scrambles creation order
        against `order` so the query's ORDER BY is genuinely exercised
        rather than the check happening to hold under insertion order too."""
        id_first_created = self._create_sprint(
            "Edge Caching Rollout",
            "Push static asset caching to the CDN edge in every region",
            order=25,
        )
        id_second_created = self._create_sprint(
            "Feature Flag Consolidation",
            "Collapse three competing flagging systems into one service",
            order=5,
        )
        id_third_created = self._create_sprint(
            "Mobile Crash Rate Reduction",
            "Drive the iOS crash-free-sessions rate above 99.7 percent",
            order=15,
        )

        creation_sequence = [id_first_created, id_second_created, id_third_created]
        expected_order_sequence = [id_second_created, id_third_created, id_first_created]
        assert creation_sequence != expected_order_sequence, (
            "fixture bug: creation sequence accidentally matches the expected "
            "order-ascending sequence; this determinism test would then hold "
            "trivially under insertion order too, whether or not the query "
            "sorts by 'order' at all"
        )

        first_read = self._list_sprints()
        second_read = self._list_sprints()

        first_sequence = [s["id"] for s in first_read]
        second_sequence = [s["id"] for s in second_read]

        assert first_sequence == expected_order_sequence, (
            f"first read expected order-ascending sequence {expected_order_sequence}, "
            f"got {first_sequence}"
        )
        assert first_sequence == second_sequence, (
            "two consecutive 'sprint list' reads over unchanged data must "
            f"return the same id sequence; got {first_sequence} then "
            f"{second_sequence}"
        )
        assert first_read == second_read, (
            "two consecutive 'sprint list' reads over unchanged data must "
            "return identical Sprint objects, not merely the same ids in the "
            f"same order; first={first_read!r}\nsecond={second_read!r}"
        )

    def test_sprint_list_help_documents_ascending_order_guarantee(self):
        """`sprint list --help` must state the ascending-`order` guarantee and
        that --status narrows without reordering, so the help cannot drift
        from the published contract in SPEC/COMMANDS.md § List Sprints."""
        _, stdout, stderr = self.test.run_cmd(["sprint", "list", "--help"], check=False)
        combined = (stdout + stderr).lower()

        assert "order" in combined, (
            f"'sprint list --help' does not mention 'order' at all;\n  stdout={stdout!r}"
        )
        assert "asc" in combined, (
            f"'sprint list --help' does not state the ascending direction;\n  stdout={stdout!r}"
        )
        assert "lowest" in combined, (
            f"'sprint list --help' does not state that the lowest 'order' value comes first;\n  stdout={stdout!r}"
        )
        assert "reorder" in combined, (
            f"'sprint list --help' does not state that --status never reorders the result;\n  stdout={stdout!r}"
        )

    def test_sprint_list_ai_help_describes_ascending_order_guarantee(self):
        """The machine-readable `sprint list` contract entry (`--ai-help`)
        must describe the same ascending-`order` guarantee as the human help
        and SPEC/COMMANDS.md, so an AI agent driving the CLI can rely on it
        without reading the SPEC."""
        _, stdout, _ = self.test.run_cmd(["sprint", "list", "--ai-help"], check=False)
        try:
            contract = json.loads(stdout)
        except json.JSONDecodeError as exc:
            raise AssertionError(
                f"sprint list --ai-help did not return valid JSON: {exc}\n  stdout={stdout[:400]!r}"
            ) from exc

        sprint_list_cmd = None
        for cmd in contract.get("commands", []):
            if cmd.get("name") == "sprint":
                for sub in cmd.get("subcommands", []):
                    if sub.get("name") == "list":
                        sprint_list_cmd = sub
                        break

        assert sprint_list_cmd is not None, "Could not find sprint > list in --ai-help JSON"

        description = sprint_list_cmd.get("description", "").lower()
        assert "order asc" in description, (
            f"sprint list --ai-help description does not state ascending 'order' "
            f"ordering; got: {description!r}"
        )
        assert "lowest" in description, (
            f"sprint list --ai-help description does not state the lowest-order-first "
            f"rule; got: {description!r}"
        )
        assert "never reorders" in description, (
            f"sprint list --ai-help description does not state that --status never "
            f"reorders the result; got: {description!r}"
        )


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
