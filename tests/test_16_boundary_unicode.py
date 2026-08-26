#!/usr/bin/env python3
"""
Test 16: Boundary Values and Unicode
Exhaustive tests for numeric field boundaries, max-length strings, Unicode
round-trips, and SQL-injection resilience.

The length caps are measured in CHARACTERS — Unicode code points — which is what
SPEC/MODELS.md states, what the refusal message names, and what
CHECK(length(<column>) <= N) enforces on the column. Two groups of tests cover
them, and the distinction matters:

  * TestBoundaryUnicode drives each cap with ASCII. Those cases pin the NUMBER
    (255, 4096, 2048) and nothing about the unit, because for ASCII the byte
    count and the character count are the same. They passed unchanged while the
    application counted bytes and refused a 102-character CJK title.
  * TestLengthCapsCountCharacters drives every cap in four scripts of one, two,
    three and four bytes per code point. Those cases pin the UNIT, and they are
    the regression test for rmp task 296.
"""

import sys
import os
import subprocess
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
            technical_requirements="Add nil guard before dereferencing the parent task pointer",
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
        """Task title at exactly 255 ASCII characters must be accepted.

        ASCII only, so this case says nothing about the UNIT the cap measures in:
        255 ASCII characters are also 255 bytes. The cases that separate the two
        are in TestLengthCapsCountCharacters below.
        """
        roadmap = self.test.create_roadmap()
        exact_title = "x" * 255
        task_id = self.test.create_task(
            roadmap,
            title=exact_title,
            functional_requirements="A title at the exact maximum must be stored without truncation",
            technical_requirements="The cap counts Unicode code points, matching CHECK(length(title) <= 255)",
            acceptance_criteria="Stored title matches input verbatim with no truncation",
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_title = result[0]["title"]
        assert stored_title == exact_title, (
            f"Stored title length {len(stored_title)} does not match input {len(exact_title)}"
        )

    def test_title_one_beyond_max_length_rejected(self):
        """Task title at 256 ASCII characters (max+1) must be rejected."""
        roadmap = self.test.create_roadmap()
        over_title = "x" * 256
        exit_code, _, stderr = self.test.run_cmd(
            [
                "task", "create", "-r", roadmap,
                "-t", over_title,
                "-fr", "A title one character over the maximum must be rejected",
                "-tr", "The create path enforces the 255-character title cap",
                "-ac", "Error returned and no task created",
            ],
            check=False,
        )
        assert exit_code != 0, "Title of 256 characters should be rejected"

    def test_functional_requirements_at_exact_max_length_accepted(self):
        """Functional requirements at exactly 4096 ASCII characters must be accepted."""
        roadmap = self.test.create_roadmap()
        exact_fr = "f" * 4096
        task_id = self.test.create_task(
            roadmap,
            title="Verify functional requirements max-length boundary",
            functional_requirements=exact_fr,
            technical_requirements="The cap counts Unicode code points, not UTF-8 bytes",
            acceptance_criteria="Stored functional requirements match input verbatim",
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_fr = result[0]["functional_requirements"]
        assert len(stored_fr) == 4096, (
            f"Stored FR length {len(stored_fr)} does not match expected 4096"
        )

    def test_functional_requirements_one_beyond_max_rejected(self):
        """Functional requirements at 4097 ASCII characters (max+1) must be rejected."""
        roadmap = self.test.create_roadmap()
        over_fr = "f" * 4097
        exit_code, _, _ = self.test.run_cmd(
            [
                "task", "create", "-r", roadmap,
                "-t", "Verify functional requirements over-max rejection",
                "-fr", over_fr,
                "-tr", "The create path enforces the 4096-character requirement cap",
                "-ac", "Error returned and no task created",
            ],
            check=False,
        )
        assert exit_code != 0, "Functional requirements of 4097 characters should be rejected"

    def test_technical_requirements_at_exact_max_length_accepted(self):
        """Technical requirements at exactly 4096 ASCII characters must be accepted."""
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
        """Acceptance criteria at exactly 4096 ASCII characters must be accepted."""
        roadmap = self.test.create_roadmap()
        exact_ac = "c" * 4096
        task_id = self.test.create_task(
            roadmap,
            title="Verify acceptance criteria max-length boundary",
            functional_requirements="Max-length acceptance criteria boundary test",
            technical_requirements="The cap counts Unicode code points, not UTF-8 bytes",
            acceptance_criteria=exact_ac,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        stored_ac = result[0]["acceptance_criteria"]
        assert len(stored_ac) == 4096, (
            f"Stored AC length {len(stored_ac)} does not match expected 4096"
        )

    def test_technical_requirements_one_beyond_max_rejected(self):
        """Technical requirements at 4097 ASCII characters (max+1) must be rejected."""
        roadmap = self.test.create_roadmap()
        over_tr = "t" * 4097
        exit_code, _, stderr = self.test.run_cmd(
            [
                "task", "create", "-r", roadmap,
                "-t", "Verify technical requirements over-max rejection",
                "-fr", "Validation should enforce the 4096-character limit",
                "-tr", over_tr,
                "-ac", "Error returned and no task created",
            ],
            check=False,
        )
        assert exit_code == 6, (
            f"TR of 4097 characters must exit 6 (validation); got {exit_code}, stderr={stderr}"
        )
        # stderr should reference the field or 4096 limit
        assert "technical" in stderr.lower() or "4096" in stderr, (
            f"stderr must reference the field or its limit; got {stderr!r}"
        )

    def test_acceptance_criteria_one_beyond_max_rejected(self):
        """Acceptance criteria at 4097 ASCII characters (max+1) must be rejected."""
        roadmap = self.test.create_roadmap()
        over_ac = "c" * 4097
        exit_code, _, stderr = self.test.run_cmd(
            [
                "task", "create", "-r", roadmap,
                "-t", "Verify acceptance criteria over-max rejection",
                "-fr", "Validation should enforce the 4096-character limit",
                "-tr", "AC oversize boundary test",
                "-ac", over_ac,
            ],
            check=False,
        )
        assert exit_code == 6, (
            f"AC of 4097 characters must exit 6 (validation); got {exit_code}, stderr={stderr}"
        )
        assert "acceptance" in stderr.lower() or "4096" in stderr, (
            f"stderr must reference the field or its limit; got {stderr!r}"
        )

    def test_fr_tr_ac_at_4095_accepted(self):
        """Each text field at 4095 ASCII characters (one below max) must be accepted."""
        roadmap = self.test.create_roadmap()
        under = "x" * 4095
        task_id = self.test.create_task(
            roadmap,
            title="Below-max boundary sanity check across all text fields",
            functional_requirements=under,
            technical_requirements=under,
            acceptance_criteria=under,
        )
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])[0]
        assert len(result["functional_requirements"]) == 4095
        assert len(result["technical_requirements"]) == 4095
        assert len(result["acceptance_criteria"]) == 4095

    def test_fr_oversize_via_edit_rejected(self):
        """`task edit` must reject an oversized functional_requirements update too,
        not just the initial create — boundaries apply to every write path."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Edit-path boundary check task",
            functional_requirements="small FR to start",
            technical_requirements="small TR",
            acceptance_criteria="small AC",
        )
        over_fr = "f" * 4097
        exit_code, _, stderr = self.test.run_cmd(
            ["task", "edit", "-r", roadmap, str(task_id), "-fr", over_fr],
            check=False,
        )
        assert exit_code == 6, (
            f"edit with FR>4096 must exit 6; got {exit_code}, stderr={stderr}"
        )

    def test_completion_summary_at_exact_max_accepted(self):
        """completion_summary at exactly 4096 ASCII characters must be accepted on COMPLETED transition."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Boundary check for completion_summary max length",
            functional_requirements="The summary field has its own 4096-character ceiling",
            technical_requirements="The set-status path accepts --summary at the limit",
            acceptance_criteria="Stored completion_summary length matches input verbatim",
        )
        sprint_id = self.test.create_sprint(roadmap, "Completion-summary boundary sprint")
        self.test.move_task_to_sprint(roadmap, task_id, sprint_id)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING", "--commit-open", "6c8064a"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])

        exact_summary = "s" * 4096
        self.test.run_cmd([
            "task", "stat", "-r", roadmap, str(task_id), "COMPLETED", "--commit-close", "4c4ccea",
            "--summary", exact_summary,
        ])
        stored = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])[0]
        assert stored["completion_summary"] is not None
        assert len(stored["completion_summary"]) == 4096

    # ==================== Unicode round-trips ====================

    def test_unicode_cjk_title_round_trip(self):
        """CJK characters in task title must be stored and retrieved verbatim."""
        roadmap = self.test.create_roadmap()
        # 50 CJK characters, well within the 255-CHARACTER title cap. The
        # boundary itself is exercised by TestLengthCapsCountCharacters.
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


    # ==================== UTF-8 encoding constraint ====================
    #
    # SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint and
    # SPEC/COMMANDS.md § UTF-8 Encoding Constraint (All Free-Text Fields).
    #
    # These drive the compiled binary with argv values that are NOT valid UTF-8.
    # Python puts arbitrary bytes into a child's argv through the surrogateescape
    # convention: a str holding lone surrogates is turned back into the original
    # bytes by os.fsencode, which is what subprocess applies to every argument.
    # That is the only way to reproduce from Python what a shell does natively
    # with `printf 'a\x80b'`, and the defect this covers was originally reported
    # exactly that way.
    #
    # The comment `body` is covered in test_50: it is the one free-text field
    # with a standard-input source, so it needs both paths rather than this one.

    # The malformed shapes SPEC/MODELS.md enumerates, as raw bytes. Kept as the
    # bytes themselves so the fixture states what reaches the process, not a
    # decoding of it.
    MALFORMED_UTF8 = [
        ("lone continuation byte", b"Ledger batch SEPA-20260815-004 \x80 failed to reconcile"),
        ("lone 0xFF", b"Settlement window closed at 17:00\xff before the last file arrived"),
        ("overlong encoding", b"Traversal probe in the upload path: ..\xc0\xaf..\xc0\xafetc/passwd"),
        ("lone surrogate", b"Imported from a UTF-16 feed carrying an unpaired \xed\xa0\x80 surrogate"),
        ("truncated sequence", b"Reconciliation summary for the medi\xc3"),
    ]

    UTF8_REFUSAL = "the value is not valid UTF-8"

    def _run_raw(self, args):
        """Run rmp with argv entries that may hold bytes which are not UTF-8.

        Returns (exit_code, stdout_bytes, stderr_bytes). Nothing is decoded here:
        the point of these tests is what the process was handed and what it wrote
        back, and a decode with a replacement policy would hide precisely the
        bytes under test.
        """
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        argv = [self.test.cli_path]
        for a in args:
            argv.append(a.decode("utf-8", "surrogateescape") if isinstance(a, bytes) else str(a))
        result = subprocess.run(argv, capture_output=True, env=env)
        return result.returncode, result.stdout, result.stderr

    def _assert_utf8_refusal(self, args, field, label):
        """Assert one malformed value is refused with exit 6 and the pinned line.

        Three things are checked, and each one closes a different way of being
        wrong: the exit code (6, the validation class), the exact message (a
        control-character refusal carries the same class and the same code, so
        only the wording distinguishes the rules), and the field name inside it
        (the published, underscored one).

        A fourth is checked as well, and it is the constraint's whole point: the
        refusal the process writes back must itself be valid UTF-8. A message
        that echoed the offending bytes would put them straight back on the
        caller's terminal, which is the class of defect this rule closes.
        """
        code, out, err = self._run_raw(args)
        assert code == 6, (
            f"{label}: expected exit 6, got {code}\n  stderr: {err!r}"
        )
        assert out == b"", f"{label}: a refused command wrote to stdout: {out!r}"
        try:
            text = err.decode("utf-8")
        except UnicodeDecodeError as exc:
            raise AssertionError(
                f"{label}: the refusal itself is not valid UTF-8 ({exc}); "
                f"the message echoed the malformed bytes back: {err!r}"
            ) from None
        expected = f"Error: validation error: {field}: {self.UTF8_REFUSAL}"
        assert expected in text, (
            f"{label}: stderr does not carry the pinned refusal\n"
            f"  expected: {expected!r}\n  got: {text!r}"
        )

    def test_task_create_refuses_invalid_utf8_in_every_free_text_flag(self):
        """`task create` refuses malformed UTF-8 in title, FR, TR and AC."""
        roadmap = self.test.create_roadmap()
        good = {
            "-t": "Reconcile the nightly settlement ledger",
            "-fr": "Every posting must be matched against the settlement file",
            "-tr": "One transaction per batch, with the cursor advanced only on commit",
            "-ac": "A regression test covers a batch that arrives twice",
        }
        fields = {"-t": "title", "-fr": "functional_requirements",
                  "-tr": "technical_requirements", "-ac": "acceptance_criteria"}

        for flag, field in fields.items():
            for label, bad in self.MALFORMED_UTF8:
                args = ["task", "create", "-r", roadmap]
                for f, v in good.items():
                    args += [f, bad if f == flag else v]
                self._assert_utf8_refusal(args, field, f"task create {flag} / {label}")

        assert self.test.list_tasks(roadmap) == [], (
            "a refused `task create` must leave no task behind"
        )

    def test_task_edit_refuses_invalid_utf8_and_changes_nothing(self):
        """`task edit` refuses malformed UTF-8 and leaves the task untouched."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Reconcile the nightly settlement ledger",
            functional_requirements="Every posting must be matched against the settlement file",
            technical_requirements="One transaction per batch, committed atomically",
            acceptance_criteria="A regression test covers a batch that arrives twice",
        )
        before = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])[0]

        for flag, field in (("-t", "title"), ("-fr", "functional_requirements"),
                            ("-tr", "technical_requirements"), ("-ac", "acceptance_criteria")):
            for label, bad in self.MALFORMED_UTF8:
                self._assert_utf8_refusal(
                    ["task", "edit", "-r", roadmap, str(task_id), flag, bad],
                    field, f"task edit {flag} / {label}",
                )

        after = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])[0]
        for column in ("title", "functional_requirements",
                       "technical_requirements", "acceptance_criteria"):
            assert after[column] == before[column], (
                f"a refused `task edit` modified {column}: "
                f"{before[column]!r} -> {after[column]!r}"
            )

    def test_sprint_create_and_update_refuse_invalid_utf8(self):
        """`sprint create` and `sprint update` refuse malformed UTF-8."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.create_sprint(
            roadmap,
            "Close the encoding gap in every free-text field.",
            title="Encoding hardening",
        )
        before = self.test.list_sprints(roadmap)

        for label, bad in self.MALFORMED_UTF8:
            self._assert_utf8_refusal(
                ["sprint", "create", "-r", roadmap, "-t", bad, "-d", "Any description"],
                "title", f"sprint create -t / {label}")
            self._assert_utf8_refusal(
                ["sprint", "create", "-r", roadmap, "-t", "Any title", "-d", bad],
                "description", f"sprint create -d / {label}")
            self._assert_utf8_refusal(
                ["sprint", "update", "-r", roadmap, str(sprint_id), "-t", bad],
                "title", f"sprint update -t / {label}")
            self._assert_utf8_refusal(
                ["sprint", "update", "-r", roadmap, str(sprint_id), "-d", bad],
                "description", f"sprint update -d / {label}")

        after = self.test.list_sprints(roadmap)
        assert len(after) == len(before), (
            f"a refused sprint write created a sprint: {len(before)} -> {len(after)}")
        assert after[0]["title"] == before[0]["title"], "a refused `sprint update` changed the title"
        assert after[0]["description"] == before[0]["description"], (
            "a refused `sprint update` changed the description")

    def test_completion_summary_refuses_invalid_utf8(self):
        """`task stat COMPLETED --summary` refuses malformed UTF-8.

        completion_summary is the eighth free-text field and the one with a
        single writer; it was missing from the original report of this defect and
        stored malformed bytes exactly like the other seven.
        """
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Reconcile the nightly settlement ledger",
            functional_requirements="Every posting must be matched against the settlement file",
            technical_requirements="One transaction per batch, committed atomically",
            acceptance_criteria="A regression test covers a batch that arrives twice",
        )
        sprint_id = self.test.create_sprint(roadmap, "Encoding hardening sprint")
        self.test.move_task_to_sprint(roadmap, task_id, sprint_id)
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING",
                           "--commit-open", "6c8064a"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])

        for label, bad in self.MALFORMED_UTF8:
            self._assert_utf8_refusal(
                ["task", "stat", "-r", roadmap, str(task_id), "COMPLETED",
                 "--commit-close", "4c4ccea", "--summary", bad],
                "completion_summary", f"task stat --summary / {label}")

        self.test.assert_task_status(roadmap, task_id, "TESTING")

        # And the same transition succeeds with a summary carrying accented
        # Latin, CJK and emoji: the rule refuses malformed BYTES, never
        # unfamiliar characters.
        summary = "Reconciliação concluída; 監査ログ verificado \U0001F680"
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED",
                           "--commit-close", "4c4ccea", "--summary", summary])
        stored = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])[0]
        assert stored["completion_summary"] == summary, (
            f"the summary did not round-trip: {stored['completion_summary']!r} != {summary!r}")

    def test_encoding_refusal_precedes_the_control_character_refusal(self):
        """A value breaking BOTH content rules reports the encoding one.

        SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint, "Order": the
        encoding check runs immediately before the control-character check. Both
        refusals are exit 6 and both carry `validation error:`, so only the
        wording can show which rule answered.
        """
        roadmap = self.test.create_roadmap()
        for label, bad in self.MALFORMED_UTF8:
            both = bad + b" \x1b[31m"
            self._assert_utf8_refusal(
                ["task", "create", "-r", roadmap, "-t", both,
                 "-fr", "x", "-tr", "x", "-ac", "x"],
                "title", f"encoding before control characters / {label}")

        # The control-character rule is still there: a well-formed value that
        # carries an ESC is refused by it, not by the encoding rule.
        code, _, err = self._run_raw(
            ["task", "create", "-r", roadmap, "-t", "Ledger \x1b[31mbatch\x1b[0m",
             "-fr", "x", "-tr", "x", "-ac", "x"])
        assert code == 6, f"an ESC must still be refused; got exit {code}"
        text = err.decode("utf-8")
        assert "title: control characters are not allowed" in text, (
            f"the control-character rule must still answer for well-formed input: {text!r}")
        assert self.UTF8_REFUSAL not in text, (
            f"a well-formed value must not be reported as an encoding failure: {text!r}")


class TestLengthCapsCountCharacters:
    """The unit every free-text length cap measures in: Unicode code points.

    The defect this class exists against (rmp task 296), reproduced against the
    compiled binary:

        rmp task create --title <102 CJK characters> ...
        Error: field exceeds maximum size: title exceeds maximum length of 255 characters

    The title was 102 characters. The cap measured its 306 BYTES while the
    message, SPEC/MODELS.md § Task Field Constraints and
    CHECK(length(title) <= 255) all named CHARACTERS, so every non-ASCII value
    was refused at roughly a third of its documented maximum. The comment body,
    alone among the eight fields, already counted characters: one codebase, two
    units, and which one applied depended on the field.

    An ASCII-only suite cannot see this. Every case below therefore runs in four
    scripts of one, two, three and four bytes per code point, and asserts the
    same two things of each cap: a value of exactly the maximum is accepted, and
    a value one code point over it is refused with exit 6 and the published
    message.
    """

    # The maximums, from SPEC/MODELS.md. They are spelled here rather than read
    # from the binary because a test that took the number from the thing under
    # test could not detect the number changing.
    TITLE_MAX = 255
    REQUIREMENT_MAX = 4096
    SPRINT_DESCRIPTION_MAX = 2048
    COMMENT_BODY_MAX = 4096

    # One seed per script, mirroring internal/testenv/lengthprobes.go so the Go
    # suite and this one exercise the same boundary. Each seed is free of
    # whitespace at either edge, so a cap measured on the trimmed value and one
    # measured on the value as supplied count the same thing.
    #
    # (name, bytes per code point, seed)
    SCRIPTS = [
        ("ascii", 1, "refuse-the-token-whose-expiry-is-the-current-second-"),
        ("accented latin", 2, "çãõáéíóúâêôàüñ"),
        ("cjk", 3, "資料庫遷移驗證任務標題"),
        ("emoji beyond the BMP", 4, "\U0001F680\U0001F5C4\U0001F9EA\U0001F9F1\U0001F52D"),
    ]

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    # ---------------- probe construction ----------------

    @classmethod
    def probe(cls, seed, n):
        """Return exactly n code points of seed, repeated and cut on a boundary.

        Python strings are sequences of code points, so len() here is the same
        count the application must make. That is what lets this module state the
        boundary in the unit under test without computing it a second way.
        """
        assert n > 0
        repeated = seed * (n // len(seed) + 1)
        value = repeated[:n]
        assert len(value) == n, f"probe is {len(value)} code points, want {n}"
        return value

    def probes(self, limit):
        """Yield (script_name, at_limit, one_over) for each of the four scripts.

        Each tuple is checked for the two properties that make the case
        non-vacuous before it is handed out: the value is exactly `limit` code
        points, and outside ASCII its byte count differs from its character
        count, so a byte-counting cap would behave differently on it.
        """
        for name, bytes_per_rune, seed in self.SCRIPTS:
            at = self.probe(seed, limit)
            over = self.probe(seed, limit + 1)
            encoded = len(at.encode("utf-8"))
            assert encoded == limit * bytes_per_rune, (
                f"the {name} probe is {encoded} bytes for {limit} characters, "
                f"want {limit * bytes_per_rune}")
            if bytes_per_rune > 1:
                assert encoded != len(at), (
                    f"the {name} probe counts the same in bytes and characters; "
                    "it cannot tell the two units apart")
            yield name, at, over

    # ---------------- shared assertions ----------------

    def assert_refused_for_length(self, args, field, limit, script, what):
        """The invocation is refused with exit 6 and the published message.

        SPEC/COMMANDS.md § Published Error Strings Are Exact publishes the line
        verbatim, so it is compared verbatim.
        """
        exit_code, stdout, stderr = self.test.run_cmd(args, check=False)
        expected = (f"Error: field exceeds maximum size: {field} exceeds "
                    f"maximum length of {limit} characters")
        assert exit_code == 6, (
            f"{what} with {limit + 1} {script} characters must exit 6; "
            f"got {exit_code}, stderr={stderr!r}")
        assert expected in stderr, (
            f"{what} with {limit + 1} {script} characters must report\n  {expected}\n"
            f"got\n  {stderr!r}")
        assert stdout == "", (
            f"{what} failed and still wrote to stdout: {stdout!r}")

    def assert_not_refused_for_length(self, args, field, limit, script, what):
        """The invocation is not refused for the field's LENGTH.

        The claim is deliberately narrower than "it succeeded": a command may
        decline for a reason of its own that has nothing to do with this rule.
        Refusal for length is exactly what the defect produced, so that is what
        is refused here.
        """
        _, _, stderr = self.test.run_cmd(args, check=False)
        refusal = f"{field} exceeds maximum length of {limit} characters"
        assert refusal not in stderr, (
            f"{what} refused a value of exactly {limit} {script} characters, "
            f"which is the documented maximum: {stderr!r}")

    # ---------------- task create: the four required fields ----------------

    def test_task_create_caps_every_field_in_characters(self):
        """`task create` accepts each field at its maximum and refuses one over.

        All four fields, all four scripts. Before rmp task 296 the CJK and emoji
        cases at the maximum were refused, because 255 CJK characters are 765
        bytes and 4096 emoji are 16384.
        """
        roadmap = self.test.create_roadmap()
        fields = [
            ("-t", "title", self.TITLE_MAX),
            ("-fr", "functional_requirements", self.REQUIREMENT_MAX),
            ("-tr", "technical_requirements", self.REQUIREMENT_MAX),
            ("-ac", "acceptance_criteria", self.REQUIREMENT_MAX),
        ]
        filler = {
            "-t": "Reconcile the settlement ledger against the clearing file",
            "-fr": "Every settlement line must match a clearing line or be reported",
            "-tr": "Match on the end-to-end identifier, then on amount and value date",
            "-ac": "A mismatched batch is reported and no partial reconciliation is written",
        }

        for flag, field, limit in fields:
            for script, at, over in self.probes(limit):
                args = ["task", "create", "-r", roadmap]
                for f, v in filler.items():
                    args.extend([f, at if f == flag else v])
                self.assert_not_refused_for_length(
                    args, field, limit, script, f"task create {flag}")

                args = ["task", "create", "-r", roadmap]
                for f, v in filler.items():
                    args.extend([f, over if f == flag else v])
                self.assert_refused_for_length(
                    args, field, limit, script, f"task create {flag}")

    def test_task_edit_caps_every_field_in_characters(self):
        """`task edit` measures the same unit as `task create` on all four fields."""
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Reconcile the settlement ledger against the clearing file",
            functional_requirements="Every settlement line must match a clearing line",
            technical_requirements="Match on the end-to-end identifier, then on amount",
            acceptance_criteria="A mismatched batch is reported and nothing partial is written",
        )
        for flag, field, limit in [
            ("-t", "title", self.TITLE_MAX),
            ("-fr", "functional_requirements", self.REQUIREMENT_MAX),
            ("-tr", "technical_requirements", self.REQUIREMENT_MAX),
            ("-ac", "acceptance_criteria", self.REQUIREMENT_MAX),
        ]:
            for script, at, over in self.probes(limit):
                base = ["task", "edit", "-r", roadmap, str(task_id), flag]
                self.assert_not_refused_for_length(
                    base + [at], field, limit, script, f"task edit {flag}")
                self.assert_refused_for_length(
                    base + [over], field, limit, script, f"task edit {flag}")

                stored = self.test.run_cmd_json(
                    ["task", "get", "-r", roadmap, str(task_id)])[0]
                column = {"-t": "title", "-fr": "functional_requirements",
                          "-tr": "technical_requirements",
                          "-ac": "acceptance_criteria"}[flag]
                assert stored[column] == at, (
                    f"task edit {flag} stored {len(stored[column])} characters "
                    f"after writing {limit} {script} ones, and the refused value "
                    "must have changed nothing")

    def test_completion_summary_caps_in_characters(self):
        """`task stat COMPLETED --summary` measures characters on every script."""
        roadmap = self.test.create_roadmap()
        for script, at, over in self.probes(self.REQUIREMENT_MAX):
            task_id = self.test.create_task(
                roadmap,
                title=f"Close the reconciliation run for the {script} ledger",
                functional_requirements="The completion summary records what was reconciled",
                technical_requirements="The summary is capped at 4096 characters",
                acceptance_criteria="A summary at the maximum is stored verbatim",
            )
            sprint_id = self.test.create_sprint(
                roadmap, f"Settlement reconciliation sprint for {script}")
            self.test.move_task_to_sprint(roadmap, task_id, sprint_id)
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id),
                               "DOING", "--commit-open", "6c8064a"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])

            self.assert_refused_for_length(
                ["task", "stat", "-r", roadmap, str(task_id), "COMPLETED",
                 "--commit-close", "4c4ccea", "--summary", over],
                "completion_summary", self.REQUIREMENT_MAX, script, "task stat --summary")
            self.test.assert_task_status(roadmap, task_id, "TESTING")

            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED",
                               "--commit-close", "4c4ccea", "--summary", at])
            stored = self.test.run_cmd_json(
                ["task", "get", "-r", roadmap, str(task_id)])[0]
            assert stored["completion_summary"] == at, (
                f"a summary of {self.REQUIREMENT_MAX} {script} characters did not "
                "round-trip verbatim")

    def test_sprint_create_and_update_cap_in_characters(self):
        """Both sprint free-text fields, on both writers, in all four scripts."""
        roadmap = self.test.create_roadmap()
        sprint_id = self.test.create_sprint(
            roadmap, "Settlement reconciliation hardening",
            title="Settlement reconciliation")

        for flag, field, limit in [
            ("-t", "title", self.TITLE_MAX),
            ("-d", "description", self.SPRINT_DESCRIPTION_MAX),
        ]:
            other = "-d" if flag == "-t" else "-t"
            other_value = ("Close every open reconciliation finding"
                           if other == "-d" else "Reconciliation hardening")
            for script, at, over in self.probes(limit):
                self.assert_refused_for_length(
                    ["sprint", "create", "-r", roadmap, flag, over, other, other_value],
                    field, limit, script, f"sprint create {flag}")
                self.assert_not_refused_for_length(
                    ["sprint", "create", "-r", roadmap, flag, at, other, other_value],
                    field, limit, script, f"sprint create {flag}")

                self.assert_refused_for_length(
                    ["sprint", "update", "-r", roadmap, str(sprint_id), flag, over],
                    field, limit, script, f"sprint update {flag}")
                self.test.run_cmd(
                    ["sprint", "update", "-r", roadmap, str(sprint_id), flag, at])
                # `sprint get` returns the sprint object itself, not a list
                # (SPEC/COMMANDS.md § Get Sprint), unlike `task get`.
                stored = self.test.run_cmd_json(
                    ["sprint", "get", "-r", roadmap, str(sprint_id)])
                column = "title" if flag == "-t" else "description"
                assert stored[column] == at, (
                    f"sprint update {flag} did not store {limit} {script} characters verbatim")

    def test_comment_body_caps_in_characters(self):
        """The comment body, the one field that always counted characters.

        It is swept with the other seven so the whole set is proved together: the
        agreement between them is the property rmp task 296 delivered, and a
        sweep that left out the field that was already right would not notice the
        other seven drifting back.
        """
        roadmap = self.test.create_roadmap()
        task_id = self.test.create_task(
            roadmap,
            title="Reconcile the settlement ledger against the clearing file",
            functional_requirements="Every settlement line must match a clearing line",
            technical_requirements="Match on the end-to-end identifier, then on amount",
            acceptance_criteria="A mismatched batch is reported",
        )
        sprint_id = self.test.create_sprint(roadmap, "Settlement reconciliation sprint")

        for script, at, over in self.probes(self.COMMENT_BODY_MAX):
            for args_prefix, what in [
                (["task", "comment-add", "-r", roadmap, str(task_id),
                  "--type", "DECISION", "--body"], "task comment-add"),
                (["sprint", "comment-add", "-r", roadmap, str(sprint_id),
                  "--type", "DECISION", "--body"], "sprint comment-add"),
            ]:
                self.assert_refused_for_length(
                    args_prefix + [over], "body", self.COMMENT_BODY_MAX, script, what)
                self.assert_not_refused_for_length(
                    args_prefix + [at], "body", self.COMMENT_BODY_MAX, script, what)

    # ---------------- the defect's own boundary ----------------

    def test_cjk_title_of_255_characters_accepted_and_256_refused(self):
        """A title of 255 CJK characters is accepted; 256 is refused.

        This is the acceptance criterion stated as the report stated it. 255 CJK
        characters occupy 765 bytes, so a byte-counting cap refuses them; the
        stored value is read back and compared, which also shows
        CHECK(length(title) <= 255) accepted those 765 bytes.
        """
        roadmap = self.test.create_roadmap()
        cjk_seed = next(seed for name, _, seed in self.SCRIPTS if name == "cjk")

        at = self.probe(cjk_seed, self.TITLE_MAX)
        assert len(at.encode("utf-8")) == 3 * self.TITLE_MAX, (
            "the probe is not three bytes per character; it cannot catch a byte-counting cap")

        task_id = self.test.create_task(
            roadmap,
            title=at,
            functional_requirements="A title at the documented maximum must be accepted",
            technical_requirements="The cap counts code points, so 255 CJK characters fit",
            acceptance_criteria="The stored title matches the supplied one exactly",
        )
        stored = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])[0]
        assert stored["title"] == at, (
            f"the stored title is {len(stored['title'])} characters, want {self.TITLE_MAX}")

        self.assert_refused_for_length(
            ["task", "create", "-r", roadmap,
             "-t", self.probe(cjk_seed, self.TITLE_MAX + 1),
             "-fr", "A title one character over the maximum must be refused",
             "-tr", "The cap counts code points",
             "-ac", "No task row is written"],
            "title", self.TITLE_MAX, "cjk", "task create -t")

    def test_the_refusal_names_the_unit_it_measured(self):
        """The number in the refusal is a number of CHARACTERS, in every script.

        The message is taken from the binary and its maximum parsed out of it,
        then a value of exactly that many characters is submitted. If the cap
        measured bytes, the message would name 255 while refusing anything past
        85 CJK characters, and this would fail.
        """
        roadmap = self.test.create_roadmap()
        for script, _, over in self.probes(self.TITLE_MAX):
            _, _, stderr = self.test.run_cmd(
                ["task", "create", "-r", roadmap, "-t", over,
                 "-fr", "Probe the published maximum",
                 "-tr", "Parse the number out of the refusal",
                 "-ac", "The number is a count of characters"],
                check=False)
            marker = "exceeds maximum length of "
            assert marker in stderr, f"not a too-large refusal: {stderr!r}"
            tail = stderr[stderr.index(marker) + len(marker):].split()
            assert len(tail) >= 2, f"the refusal names no maximum and no unit: {stderr!r}"
            assert tail[1].startswith("characters"), (
                f"the refusal names the unit {tail[1]!r}, but the cap measures characters: {stderr!r}")
            limit = int(tail[0])

            seed = next(s for name, _, s in self.SCRIPTS if name == script)
            accepted = self.probe(seed, limit)
            exit_code, _, err = self.test.run_cmd(
                ["task", "create", "-r", roadmap, "-t", accepted,
                 "-fr", "A title of exactly the published maximum must be accepted",
                 "-tr", "The published number is a count of characters",
                 "-ac", "The task is created"],
                check=False)
            assert exit_code == 0, (
                f"the refusal names a maximum of {limit} CHARACTERS, but a title of "
                f"exactly {limit} {script} characters ({len(accepted.encode('utf-8'))} bytes) "
                f"is refused: {err!r}")


def main():
    """Run all tests directly."""
    passed = 0
    failed = 0

    for cls in (TestBoundaryUnicode, TestLengthCapsCountCharacters):
        test = cls()
        for method_name in [m for m in dir(test) if m.startswith("test_")]:
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
