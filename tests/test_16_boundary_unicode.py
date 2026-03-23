#!/usr/bin/env python3
"""
Test 16: Boundary Values and Unicode
Exhaustive tests for numeric field boundaries, max-length strings, Unicode
round-trips, and SQL-injection resilience.
"""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestBoundaryUnicode:
    """Boundary value and Unicode tests for Groadmap CLI."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    # ==================== Priority boundary values ====================

    def test_priority_min_boundary_accepted(self):
        """Priority 0 (minimum boundary) must be accepted on task create."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Implement rate-limiting for public REST API endpoints",
            functional_requirements="Prevent abuse by enforcing per-client request quotas",
            technical_requirements="Use token-bucket algorithm in Redis; configure per-route limits",
            acceptance_criteria="429 responses returned when quota exceeded; counters reset correctly",
            priority=0,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["priority"] == 0, f"Expected priority 0, got {result[0]['priority']}"

    def test_priority_max_boundary_accepted(self):
        """Priority 9 (maximum boundary) must be accepted on task create."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Resolve critical data-loss bug in export pipeline",
            functional_requirements="Exported CSV files must include all rows without truncation",
            technical_requirements="Fix off-by-one error in pagination logic within ExportService",
            acceptance_criteria="Export of 100 000 rows completes without missing rows",
            priority=9,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["priority"] == 9, f"Expected priority 9, got {result[0]['priority']}"

    def test_priority_below_min_rejected(self):
        """Priority -1 (below minimum) must be rejected."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Placeholder task for priority boundary test",
            functional_requirements="Task used to test priority validation",
            technical_requirements="No implementation; validation only",
            acceptance_criteria="Negative priority value is rejected with a non-zero exit code",
        )
        exit_code, _, _ = self.test.run_cmd(
            ["task", "prio", "-r", roadmap, str(task_id), "-1"],
            check=False,
        )
        assert exit_code != 0, "Priority -1 should be rejected (non-zero exit code)"

    def test_priority_above_max_rejected(self):
        """Priority 10 (above maximum) must be rejected."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Placeholder task for priority upper bound test",
            functional_requirements="Task used to test priority upper-bound validation",
            technical_requirements="No implementation; validation only",
            acceptance_criteria="Priority value 10 is rejected with a non-zero exit code",
        )
        exit_code, _, _ = self.test.run_cmd(
            ["task", "prio", "-r", roadmap, str(task_id), "10"],
            check=False,
        )
        assert exit_code != 0, "Priority 10 should be rejected (non-zero exit code)"

    def test_priority_boundary_prio_subcommand(self):
        """Priority subcommand (prio) must accept 0 and 9, reject -1 and 10."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Refactor authentication middleware to support multi-tenant routing",
            functional_requirements="Each tenant uses an isolated identity provider",
            technical_requirements="Extend middleware to resolve tenant from subdomain header",
            acceptance_criteria="All tenant logins succeed in integration environment",
            priority=5,
        )

        # Accept 0
        self.test.run_cmd(["task", "prio", "-r", roadmap, str(task_id), "0"])
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["priority"] == 0, f"Expected 0 after prio 0, got {result[0]['priority']}"

        # Accept 9
        self.test.run_cmd(["task", "prio", "-r", roadmap, str(task_id), "9"])
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["priority"] == 9, f"Expected 9 after prio 9, got {result[0]['priority']}"

        # Reject -1
        ec, _, _ = self.test.run_cmd(
            ["task", "prio", "-r", roadmap, str(task_id), "-1"], check=False
        )
        assert ec != 0, "prio -1 must be rejected"

        # Reject 10
        ec, _, _ = self.test.run_cmd(
            ["task", "prio", "-r", roadmap, str(task_id), "10"], check=False
        )
        assert ec != 0, "prio 10 must be rejected"

    # ==================== Severity boundary values ====================

    def test_severity_min_boundary_accepted(self):
        """Severity 0 (minimum boundary) must be accepted on task create."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Add pagination to roadmap listing endpoint",
            functional_requirements="Users can navigate roadmaps page-by-page in the UI",
            technical_requirements="Implement cursor-based pagination with page_size parameter",
            acceptance_criteria="Endpoint returns correct page slices without duplicate items",
            severity=0,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["severity"] == 0, f"Expected severity 0, got {result[0]['severity']}"

    def test_severity_max_boundary_accepted(self):
        """Severity 9 (maximum boundary) must be accepted on task create."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Fix production outage caused by nil dereference in task handler",
            functional_requirements="Task handler must not panic on missing optional fields",
            technical_requirements="Add nil guard before dereferencing specialists pointer",
            acceptance_criteria="No panics observed in production logs after deployment",
            severity=9,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["severity"] == 9, f"Expected severity 9, got {result[0]['severity']}"

    def test_severity_boundary_sev_subcommand(self):
        """Severity subcommand (sev) must accept 0 and 9, reject -1 and 10."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Upgrade Go runtime to latest stable release",
            functional_requirements="All services run on the latest Go patch release",
            technical_requirements="Update go.mod, rebuild Docker images, run full test suite",
            acceptance_criteria="CI pipeline passes with zero regressions on new runtime",
            severity=5,
        )

        # Accept 0
        self.test.run_cmd(["task", "sev", "-r", roadmap, str(task_id), "0"])
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["severity"] == 0

        # Accept 9
        self.test.run_cmd(["task", "sev", "-r", roadmap, str(task_id), "9"])
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["severity"] == 9

        # Reject -1
        ec, _, _ = self.test.run_cmd(
            ["task", "sev", "-r", roadmap, str(task_id), "-1"], check=False
        )
        assert ec != 0, "sev -1 must be rejected"

        # Reject 10
        ec, _, _ = self.test.run_cmd(
            ["task", "sev", "-r", roadmap, str(task_id), "10"], check=False
        )
        assert ec != 0, "sev 10 must be rejected"

    # ==================== Max-length string limits ====================

    def test_title_at_exact_max_length_accepted(self):
        """Task title at exactly 255 bytes must be accepted."""
        roadmap = self.test.create_roadmap()
        exact_title = "x" * 255
        task_id = self.test.create_task(
            roadmap,
            title=exact_title,
            functional_requirements="Title at exact byte boundary must be stored without truncation",
            technical_requirements="Validation uses len() which returns byte count in Go",
            acceptance_criteria="Stored title matches input verbatim with no truncation",
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_title = result[0]["title"]
        assert stored_title == exact_title, (
            f"Stored title length {len(stored_title)} does not match input {len(exact_title)}"
        )

    def test_title_one_beyond_max_length_rejected(self):
        """Task title at 256 bytes (max+1) must be rejected."""
        roadmap = self.test.create_roadmap()
        over_title = "x" * 256
        exit_code, _, stderr = self.test.run_cmd(
            [
                "task", "create", "-r", roadmap,
                "-t", over_title,
                "-fr", "Title one byte over max must be rejected",
                "-tr", "Validation enforces 255-byte limit",
                "-ac", "Error returned and no task created",
            ],
            check=False,
        )
        assert exit_code != 0, "Title of 256 bytes should be rejected"

    def test_functional_requirements_at_exact_max_length_accepted(self):
        """Functional requirements at exactly 4096 bytes must be accepted."""
        roadmap = self.test.create_roadmap()
        exact_fr = "f" * 4096
        task_id = self.test.create_task(
            roadmap,
            title="Verify functional requirements max-length boundary",
            functional_requirements=exact_fr,
            technical_requirements="Validation uses len() which returns byte count in Go",
            acceptance_criteria="Stored functional requirements match input verbatim",
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_fr = result[0]["functional_requirements"]
        assert len(stored_fr) == 4096, (
            f"Stored FR length {len(stored_fr)} does not match expected 4096"
        )

    def test_functional_requirements_one_beyond_max_rejected(self):
        """Functional requirements at 4097 bytes (max+1) must be rejected."""
        roadmap = self.test.create_roadmap()
        over_fr = "f" * 4097
        exit_code, _, _ = self.test.run_cmd(
            [
                "task", "create", "-r", roadmap,
                "-t", "Verify functional requirements over-max rejection",
                "-fr", over_fr,
                "-tr", "Validation enforces 4096-byte limit",
                "-ac", "Error returned and no task created",
            ],
            check=False,
        )
        assert exit_code != 0, "Functional requirements of 4097 bytes should be rejected"

    def test_technical_requirements_at_exact_max_length_accepted(self):
        """Technical requirements at exactly 4096 bytes must be accepted."""
        roadmap = self.test.create_roadmap()
        exact_tr = "t" * 4096
        task_id = self.test.create_task(
            roadmap,
            title="Verify technical requirements max-length boundary",
            functional_requirements="Max-length technical requirements boundary test",
            technical_requirements=exact_tr,
            acceptance_criteria="Stored technical requirements match input verbatim",
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_tr = result[0]["technical_requirements"]
        assert len(stored_tr) == 4096, (
            f"Stored TR length {len(stored_tr)} does not match expected 4096"
        )

    def test_acceptance_criteria_at_exact_max_length_accepted(self):
        """Acceptance criteria at exactly 4096 bytes must be accepted."""
        roadmap = self.test.create_roadmap()
        exact_ac = "c" * 4096
        task_id = self.test.create_task(
            roadmap,
            title="Verify acceptance criteria max-length boundary",
            functional_requirements="Max-length acceptance criteria boundary test",
            technical_requirements="Validation uses len() which returns byte count in Go",
            acceptance_criteria=exact_ac,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_ac = result[0]["acceptance_criteria"]
        assert len(stored_ac) == 4096, (
            f"Stored AC length {len(stored_ac)} does not match expected 4096"
        )

    # ==================== Unicode round-trips ====================

    def test_unicode_cjk_title_round_trip(self):
        """CJK characters in task title must be stored and retrieved verbatim."""
        roadmap = self.test.create_roadmap()
        # 50 CJK chars = 150 UTF-8 bytes — well within 255-byte limit
        cjk_title = "実装" * 25
        task_id = self.test.create_task(
            roadmap,
            title=cjk_title,
            functional_requirements="認証サービスのデプロイメント要件を定義する",
            technical_requirements="SQLite stores UTF-8 natively; no transcoding required",
            acceptance_criteria="Stored title matches input CJK characters verbatim",
            priority=7,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_title = result[0]["title"]
        assert stored_title == cjk_title, (
            f"CJK title round-trip failed: stored {stored_title!r}, want {cjk_title!r}"
        )

    def test_unicode_rtl_title_round_trip(self):
        """Arabic right-to-left text in task title must be stored and retrieved verbatim."""
        roadmap = self.test.create_roadmap()
        rtl_title = "تنفيذ بوابة الدفع الآمنة للعملاء"
        task_id = self.test.create_task(
            roadmap,
            title=rtl_title,
            functional_requirements="Enable secure payment processing for Arabic-locale customers",
            technical_requirements="Integrate Stripe SDK with locale-aware error messages",
            acceptance_criteria="Payment flow completes without data corruption for RTL inputs",
            priority=8,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_title = result[0]["title"]
        assert stored_title == rtl_title, (
            f"RTL title round-trip failed: stored {stored_title!r}, want {rtl_title!r}"
        )

    def test_unicode_accented_latin_title_round_trip(self):
        """Latin characters with diacritics in task title must be stored and retrieved verbatim."""
        roadmap = self.test.create_roadmap()
        accented_title = "Implementação do serviço de autenticação OAuth2"
        task_id = self.test.create_task(
            roadmap,
            title=accented_title,
            functional_requirements="Permitir autenticação via fornecedor SSO corporativo",
            technical_requirements="Integrar Auth0 SDK; actualizar middleware de sessão",
            acceptance_criteria="Utilizadores autenticam via SSO sem necessidade de reset de password",
            priority=6,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_title = result[0]["title"]
        assert stored_title == accented_title, (
            f"Accented title round-trip failed: stored {stored_title!r}, want {accented_title!r}"
        )

    def test_unicode_emoji_in_description_round_trip(self):
        """Emoji characters in functional requirements must be stored and retrieved verbatim."""
        roadmap = self.test.create_roadmap()
        fr_with_emoji = (
            "Automated deployment must complete within 10 minutes for every push to main branch"
        )
        task_id = self.test.create_task(
            roadmap,
            title="Migrate CI/CD pipeline to GitHub Actions",
            functional_requirements=fr_with_emoji,
            technical_requirements="Replace Jenkins runners; configure Go module caching",
            acceptance_criteria="All status checks pass on pull request and deployment succeeds",
            priority=5,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_fr = result[0]["functional_requirements"]
        assert stored_fr == fr_with_emoji, (
            f"FR round-trip failed: stored {stored_fr!r}, want {fr_with_emoji!r}"
        )

    def test_unicode_mixed_script_sprint_description_round_trip(self):
        """Sprint description with mixed scripts must be stored and retrieved verbatim."""
        roadmap = self.test.create_roadmap()
        mixed_desc = "Sprint 01: OAuth2 実装 — autenticação segura (português/日本語)"
        sprint_id = self.test.create_sprint(roadmap, mixed_desc)
        result = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        stored_desc = result["description"]
        assert stored_desc == mixed_desc, (
            f"Mixed-script sprint description round-trip failed: "
            f"stored {stored_desc!r}, want {mixed_desc!r}"
        )

    def test_unicode_cjk_title_edit_round_trip(self):
        """Editing a task title to CJK text must persist the new value verbatim."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Initial ASCII title before Unicode edit",
            functional_requirements="Task title will be updated to CJK characters via task edit",
            technical_requirements="Edit operation issues parameterized SQL UPDATE",
            acceptance_criteria="Subsequent get returns the CJK title without corruption",
        )
        new_cjk_title = "認証サービスのリファクタリング計画"
        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-t", new_cjk_title,
        ])
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_title = result[0]["title"]
        assert stored_title == new_cjk_title, (
            f"CJK edit round-trip failed: stored {stored_title!r}, want {new_cjk_title!r}"
        )

    # ==================== SQL-injection patterns ====================

    def test_sql_injection_in_task_title_stored_verbatim(self):
        """SQL-injection patterns in task title must be stored verbatim (not executed)."""
        roadmap = self.test.create_roadmap()
        injection_titles = [
            "'; DROP TABLE tasks; --",
            '" OR "1"="1',
            "Robert'); DROP TABLE tasks;--",
            "1; SELECT * FROM tasks WHERE 1=1--",
            "' UNION SELECT null,null,null--",
        ]
        for title in injection_titles:
            task_id = self.test.create_task(
                roadmap,
                title=title,
                functional_requirements="SQL injection test: functional requirements remain intact",
                technical_requirements="Parameterized queries prevent SQL injection by design",
                acceptance_criteria="Database schema is unmodified; title stored as literal string",
                priority=5,
            )
            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
            stored_title = result[0]["title"]
            assert stored_title == title, (
                f"SQL-injection title mutated: stored {stored_title!r}, want {title!r}"
            )

    def test_sql_injection_in_functional_requirements_stored_verbatim(self):
        """SQL-injection patterns in functional requirements must be stored verbatim."""
        roadmap = self.test.create_roadmap()
        injection_fr = "' OR '1'='1'; INSERT INTO tasks (title) VALUES ('hacked'); --"
        task_id = self.test.create_task(
            roadmap,
            title="Validate parameterized query security in functional requirements",
            functional_requirements=injection_fr,
            technical_requirements="All database queries use parameterized placeholders",
            acceptance_criteria="Schema is intact and no spurious rows appear after injection attempt",
            priority=8,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_fr = result[0]["functional_requirements"]
        assert stored_fr == injection_fr, (
            f"FR SQL-injection mutated: stored {stored_fr!r}, want {injection_fr!r}"
        )

    def test_sql_injection_in_sprint_description_stored_verbatim(self):
        """SQL-injection patterns in sprint description must be stored verbatim."""
        roadmap = self.test.create_roadmap()
        injection_desc = "Sprint Alpha'); DROP TABLE sprints; -- security test sprint"
        sprint_id = self.test.create_sprint(roadmap, injection_desc)
        result = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sprint_id)])
        stored_desc = result["description"]
        assert stored_desc == injection_desc, (
            f"Sprint description SQL-injection mutated: stored {stored_desc!r}, want {injection_desc!r}"
        )

    def test_sql_injection_schema_integrity_after_injections(self):
        """After multiple SQL-injection attempts the schema must remain intact."""
        roadmap = self.test.create_roadmap()
        # Create several tasks with injection patterns
        for i, payload in enumerate([
            "'; DROP TABLE tasks; --",
            "1 OR 1=1",
            "'; DELETE FROM tasks WHERE 1=1; --",
        ]):
            self.test.create_task(
                roadmap,
                title=payload,
                functional_requirements=f"Injection attempt {i+1} in title",
                technical_requirements="Schema integrity check after injection attempts",
                acceptance_criteria="task list still works and returns correct count after injections",
            )

        # The tasks table must still be functional
        tasks = self.test.list_tasks(roadmap)
        assert len(tasks) == 3, (
            f"Expected 3 tasks after injection attempts, got {len(tasks)}"
        )

    # ==================== Boundary values in task create flags ====================

    def test_create_with_priority_and_severity_boundaries(self):
        """Create task with both priority and severity at boundary values."""
        roadmap = self.test.create_roadmap()

        # Both at minimum
        task_id = self.test.create_task(
            roadmap,
            title="Configure database connection pooling for high-traffic workloads",
            functional_requirements="Service handles 10 000 concurrent database connections",
            technical_requirements="Tune pgbouncer pool_size and max_client_conn parameters",
            acceptance_criteria="P99 latency remains below 50ms at peak load in staging",
            priority=0,
            severity=0,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["priority"] == 0
        assert result[0]["severity"] == 0

        # Both at maximum
        task_id = self.test.create_task(
            roadmap,
            title="Resolve data corruption in distributed transaction coordinator",
            functional_requirements="Transactions must be atomic across all participating services",
            technical_requirements="Implement two-phase commit protocol with retry and rollback",
            acceptance_criteria="Zero data inconsistencies observed in chaos engineering tests",
            priority=9,
            severity=9,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["priority"] == 9
        assert result[0]["severity"] == 9

    def test_priority_create_flag_out_of_range_rejected(self):
        """Task create with priority -1 or 10 via -p flag must be rejected."""
        roadmap = self.test.create_roadmap()
        base_args = [
            "task", "create", "-r", roadmap,
            "-t", "Boundary test task",
            "-fr", "Functional",
            "-tr", "Technical",
            "-ac", "Criteria",
        ]

        ec, _, _ = self.test.run_cmd(base_args + ["-p", "-1"], check=False)
        assert ec != 0, "task create -p -1 should be rejected"

        ec, _, _ = self.test.run_cmd(base_args + ["-p", "10"], check=False)
        assert ec != 0, "task create -p 10 should be rejected"

    def test_severity_create_flag_out_of_range_rejected(self):
        """Task create with severity -1 or 10 via --severity flag must be rejected."""
        roadmap = self.test.create_roadmap()
        base_args = [
            "task", "create", "-r", roadmap,
            "-t", "Boundary test task",
            "-fr", "Functional",
            "-tr", "Technical",
            "-ac", "Criteria",
        ]

        ec, _, _ = self.test.run_cmd(base_args + ["--severity", "-1"], check=False)
        assert ec != 0, "task create --severity -1 should be rejected"

        ec, _, _ = self.test.run_cmd(base_args + ["--severity", "10"], check=False)
        assert ec != 0, "task create --severity 10 should be rejected"


def main():
    """Run all tests directly."""
    test = TestBoundaryUnicode()
    methods = [m for m in dir(test) if m.startswith("test_")]
    passed = 0
    failed = 0

    for method_name in methods:
        test.setup_method()
        try:
            getattr(test, method_name)()
            print(f"PASS  {method_name}")
            passed += 1
        except Exception as e:
            print(f"FAIL  {method_name}: {e}")
            failed += 1
        finally:
            test.teardown_method()

    print(f"\n{passed} passed, {failed} failed")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
