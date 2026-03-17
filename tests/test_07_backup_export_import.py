#!/usr/bin/env python3
"""
Test 07: Backup, Export and Import
Tests backup/restore and export/import functionality.
"""

import sys
import os
import json
import tempfile
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestBackupExportImport:
    """Test backup, export, and import operations."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_backup_create_and_list(self):
        """Test creating and listing backups."""
        roadmap = self.test.create_roadmap("backup-test-project")

        # Create some tasks
        self.test.create_task(roadmap, "Task 1", "Action 1", "Result 1")
        self.test.create_task(roadmap, "Task 2", "Action 2", "Result 2")

        # Create sprint
        self.test.create_sprint(roadmap, "Sprint 1")

        # Create backup
        exit_code, stdout, _ = self.test.run_cmd(
            ["roadmap", "backup", "create", roadmap]
        )
        assert exit_code == 0
        assert "Backup created" in stdout

        # List backups
        exit_code, stdout, _ = self.test.run_cmd(
            ["roadmap", "backup", "list", roadmap]
        )
        assert exit_code == 0
        assert "backup-test-project" in stdout

        print("✓ Backup create and list test passed")

    def test_backup_restore(self):
        """Test backup restore functionality."""
        roadmap = self.test.create_roadmap("restore-test-project")

        # Create tasks
        task1 = self.test.create_task(roadmap, "Task 1", "Action 1", "Result 1")
        task2 = self.test.create_task(roadmap, "Task 2", "Action 2", "Result 2")

        # Create backup
        exit_code, stdout, _ = self.test.run_cmd(
            ["roadmap", "backup", "create", roadmap]
        )
        assert exit_code == 0

        # Remove original roadmap
        self.test.run_cmd(["roadmap", "remove", roadmap])

        # Extract backup filename from output
        backup_file = stdout.split(":")[-1].strip()

        # Restore from backup
        exit_code, stdout, _ = self.test.run_cmd(
            ["roadmap", "backup", "restore", roadmap, backup_file]
        )
        assert exit_code == 0
        assert "restored" in stdout

        # Verify tasks are restored
        result = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        assert len(result) == 2

        print("✓ Backup restore test passed")

    def test_export_roadmap(self):
        """Test exporting roadmap to JSON."""
        roadmap = self.test.create_roadmap("export-test-project")

        # Create tasks
        self.test.create_task(roadmap, "Task 1", "Action 1", "Result 1", priority=5)
        self.test.create_task(roadmap, "Task 2", "Action 2", "Result 2", severity=3)

        # Create sprint
        sprint_id = self.test.create_sprint(roadmap, "Sprint 1")

        # Export to specific file
        export_file = os.path.join(self.test.test_dir, "export.json")
        exit_code, stdout, _ = self.test.run_cmd(
            ["roadmap", "export", roadmap, export_file]
        )
        assert exit_code == 0
        assert "Exported" in stdout

        # Verify file exists
        assert os.path.exists(export_file)

        # Verify JSON structure
        with open(export_file, "r") as f:
            data = json.load(f)
            assert "roadmap" in data
            assert "tasks" in data
            assert "sprints" in data
            assert len(data["tasks"]) == 2
            assert len(data["sprints"]) == 1

        print("✓ Export roadmap test passed")

    def test_export_with_audit(self):
        """Test exporting roadmap with audit log."""
        roadmap = self.test.create_roadmap("export-audit-test")

        # Create task (generates audit entries)
        self.test.create_task(roadmap, "Task", "Action", "Result")

        # Export with audit
        export_file = os.path.join(self.test.test_dir, "export_audit.json")
        exit_code, stdout, _ = self.test.run_cmd(
            ["roadmap", "export", roadmap, export_file, "--audit"]
        )
        assert exit_code == 0

        # Verify audit is included
        with open(export_file, "r") as f:
            data = json.load(f)
            assert "audit" in data
            assert len(data["audit"]) > 0

        print("✓ Export with audit test passed")

    def test_import_roadmap(self):
        """Test importing roadmap from JSON."""
        source_roadmap = self.test.create_roadmap("import-source")

        # Create tasks
        self.test.create_task(source_roadmap, "Task 1", "Action 1", "Result 1")
        self.test.create_task(source_roadmap, "Task 2", "Action 2", "Result 2")

        # Create sprint
        self.test.create_sprint(source_roadmap, "Sprint 1")

        # Export
        export_file = os.path.join(self.test.test_dir, "import_test.json")
        self.test.run_cmd(["roadmap", "export", source_roadmap, export_file])

        # Remove source
        self.test.run_cmd(["roadmap", "remove", source_roadmap])

        # Import
        exit_code, stdout, _ = self.test.run_cmd(
            ["roadmap", "import", export_file]
        )
        assert exit_code == 0
        assert "Imported" in stdout

        # Verify imported data
        result = self.test.run_cmd_json(["task", "list", "-r", source_roadmap])
        assert len(result) == 2

        result = self.test.run_cmd_json(["sprint", "list", "-r", source_roadmap])
        assert len(result) == 1

        print("✓ Import roadmap test passed")

    def test_import_with_new_name(self):
        """Test importing roadmap with a new name."""
        source_roadmap = self.test.create_roadmap("original-name")

        # Create task
        self.test.create_task(source_roadmap, "Task", "Action", "Result")

        # Export
        export_file = os.path.join(self.test.test_dir, "rename_test.json")
        self.test.run_cmd(["roadmap", "export", source_roadmap, export_file])

        # Import with new name
        new_name = "renamed-project"
        exit_code, stdout, _ = self.test.run_cmd(
            ["roadmap", "import", export_file, new_name]
        )
        assert exit_code == 0

        # Verify new roadmap exists
        result = self.test.run_cmd_json(["roadmap", "list"])
        names = [r["name"] for r in result]
        assert new_name in names

        # Verify data
        result = self.test.run_cmd_json(["task", "list", "-r", new_name])
        assert len(result) == 1

        print("✓ Import with new name test passed")

    def test_import_duplicate_fails(self):
        """Test importing when target already exists fails."""
        source_roadmap = self.test.create_roadmap("duplicate-test")

        # Create task
        self.test.create_task(source_roadmap, "Task", "Action", "Result")

        # Export
        export_file = os.path.join(self.test.test_dir, "duplicate_test.json")
        self.test.run_cmd(["roadmap", "export", source_roadmap, export_file])

        # Try to import again (should fail)
        exit_code, _, _ = self.test.run_cmd(
            ["roadmap", "import", export_file],
            check=False
        )
        assert exit_code == 5, "Should fail with exit code 5 (already exists)"

        print("✓ Import duplicate fails test passed")

    def test_export_import_preserves_task_status(self):
        """Test that export/import preserves task statuses."""
        roadmap = self.test.create_roadmap("status-preservation")

        # Create tasks in different statuses
        task1 = self.test.create_task(roadmap, "Task 1", "Action", "Result")
        task2 = self.test.create_task(roadmap, "Task 2", "Action", "Result")

        sprint_id = self.test.create_sprint(roadmap, "Sprint")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task1)
        ])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task1), "TESTING"])

        # Export
        export_file = os.path.join(self.test.test_dir, "status_test.json")
        self.test.run_cmd(["roadmap", "export", roadmap, export_file])

        # Remove original
        self.test.run_cmd(["roadmap", "remove", roadmap])

        # Import
        self.test.run_cmd(["roadmap", "import", export_file])

        # Verify statuses preserved
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task1)])
        assert result[0]["status"] == "TESTING"

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task2)])
        assert result[0]["status"] == "BACKLOG"

        print("✓ Export/import preserves task status test passed")

    def test_export_import_preserves_priority_severity(self):
        """Test that export/import preserves priority and severity."""
        roadmap = self.test.create_roadmap("priority-preservation")

        # Create task with priority and severity
        task_id = self.test.create_task(
            roadmap, "Task", "Action", "Result", priority=7, severity=8
        )

        # Export
        export_file = os.path.join(self.test.test_dir, "priority_test.json")
        self.test.run_cmd(["roadmap", "export", roadmap, export_file])

        # Remove original
        self.test.run_cmd(["roadmap", "remove", roadmap])

        # Import
        self.test.run_cmd(["roadmap", "import", export_file])

        # Verify priority and severity preserved
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["priority"] == 7
        assert result[0]["severity"] == 8

        print("✓ Export/import preserves priority/severity test passed")

    def test_no_backups_message(self):
        """Test message when no backups exist."""
        roadmap = self.test.create_roadmap("no-backups")

        exit_code, stdout, _ = self.test.run_cmd(
            ["roadmap", "backup", "list", roadmap]
        )
        assert exit_code == 0
        assert "No backups" in stdout

        print("✓ No backups message test passed")


def main():
    """Run all tests."""
    test = TestBackupExportImport()

    methods = [m for m in dir(test) if m.startswith("test_")]
    passed = 0
    failed = 0

    for method_name in methods:
        test.setup_method()
        try:
            getattr(test, method_name)()
            passed += 1
        except Exception as e:
            print(f"✗ {method_name} failed: {e}")
            failed += 1
        finally:
            test.teardown_method()

    print(f"\n{passed} passed, {failed} failed")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
