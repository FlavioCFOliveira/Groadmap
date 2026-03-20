#!/usr/bin/env python3
"""
Test 14: Audit Date Filters
Tests audit list --since and --until date filters.
"""

import sys
import os
from datetime import datetime, timedelta, timezone
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestAuditDateFilters:
    """Test audit date filtering functionality."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_audit_since_filter(self):
        """Test --since filter for audit list."""
        roadmap = self.test.create_roadmap()

        # Record current time
        before = datetime.now(timezone.utc).isoformat()

        # Create first task (this will have audit entry)
        task1_id = self.test.create_task(roadmap, "Task before filter", "Functional", "Technical", "Criteria")

        # Wait briefly to ensure time separation
        time.sleep(0.1)
        mid_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create second task
        task2_id = self.test.create_task(roadmap, "Task after filter", "Functional", "Technical", "Criteria")

        after = datetime.now(timezone.utc).isoformat()

        # Test --since filter (should only show entries after mid_time)
        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "--since", mid_time])

        # Should contain TASK_CREATE for task2 but not task1
        task_creates = [e for e in result if e["operation"] == "TASK_CREATE"]
        entity_ids = [e["entity_id"] for e in task_creates]

        assert task2_id in entity_ids, "Should include task created after since filter"
        assert task1_id not in entity_ids, "Should exclude task created before since filter"

        print("✓ Audit --since filter test passed")

    def test_audit_until_filter(self):
        """Test --until filter for audit list."""
        roadmap = self.test.create_roadmap()

        # Record start time
        start_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create first task
        task1_id = self.test.create_task(roadmap, "Task before cutoff", "Functional", "Technical", "Criteria")

        time.sleep(0.1)
        cutoff_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create second task
        task2_id = self.test.create_task(roadmap, "Task after cutoff", "Functional", "Technical", "Criteria")

        # Test --until filter (should only show entries before cutoff_time)
        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "--until", cutoff_time])

        # Should contain TASK_CREATE for task1 but not task2
        task_creates = [e for e in result if e["operation"] == "TASK_CREATE"]
        entity_ids = [e["entity_id"] for e in task_creates]

        assert task1_id in entity_ids, "Should include task created before until filter"
        assert task2_id not in entity_ids, "Should exclude task created after until filter"

        print("✓ Audit --until filter test passed")

    def test_audit_combined_date_range_filter(self):
        """Test combined --since and --until filters."""
        roadmap = self.test.create_roadmap()

        # Record start time
        start_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create first task (before range)
        task1_id = self.test.create_task(roadmap, "Task before range", "Functional", "Technical", "Criteria")

        time.sleep(0.1)
        range_start = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create second task (within range)
        task2_id = self.test.create_task(roadmap, "Task in range", "Functional", "Technical", "Criteria")

        time.sleep(0.1)
        range_end = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create third task (after range)
        task3_id = self.test.create_task(roadmap, "Task after range", "Functional", "Technical", "Criteria")

        # Test combined filters
        result = self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "--since", range_start,
            "--until", range_end
        ])

        task_creates = [e for e in result if e["operation"] == "TASK_CREATE"]
        entity_ids = [e["entity_id"] for e in task_creates]

        assert task1_id not in entity_ids, "Should exclude task before range"
        assert task2_id in entity_ids, "Should include task within range"
        assert task3_id not in entity_ids, "Should exclude task after range"

        print("✓ Audit combined date range filter test passed")

    def test_audit_date_filter_with_other_filters(self):
        """Test date filters combined with other filters."""
        roadmap = self.test.create_roadmap()

        start_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Create task
        task_id = self.test.create_task(roadmap, "Test task", "Functional", "Technical", "Criteria")

        time.sleep(0.1)
        mid_time = datetime.now(timezone.utc).isoformat()
        time.sleep(0.1)

        # Edit task
        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-t", "Updated title"
        ])

        # Test since filter combined with operation filter
        result = self.test.run_cmd_json([
            "audit", "list", "-r", roadmap,
            "--since", mid_time,
            "-o", "TASK_UPDATE"
        ])

        # Should only show TASK_UPDATE operations after mid_time
        assert len(result) >= 1
        for entry in result:
            assert entry["operation"] == "TASK_UPDATE"
            assert entry["entity_id"] == task_id

        print("✓ Audit date filter with other filters test passed")


if __name__ == "__main__":
    import pytest
    pytest.main([__file__, "-v"])
