#!/usr/bin/env python3
"""
Test 09: Stress and Load Testing
Tests performance and behavior with large volumes of data.
"""

import sys
import os
import time
import random
import string
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestStressLoad:
    """Test performance with large data volumes."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def generate_random_title(self, length=30):
        """Generate random task title."""
        words = ['implement', 'create', 'fix', 'update', 'refactor', 'optimize',
                'design', 'develop', 'test', 'deploy', 'configure', 'integrate']
        return ' '.join(random.choice(words) for _ in range(3)) + ' ' + \
               ''.join(random.choices(string.ascii_lowercase, k=5))

    def test_create_1000_tasks(self):
        """Test creating 1000 tasks and measure performance."""
        roadmap = self.test.create_roadmap("stress-test")

        print(f"\n  Creating 1000 tasks...")
        start_time = time.time()

        task_ids = []
        for i in range(1000):
            task_id = self.test.create_task(
                roadmap,
                title=f"Task {i}: {self.generate_random_title()}",
                functional_requirements=f"Functional requirements for task {i}",
                technical_requirements=f"Technical requirements for task {i}",
                acceptance_criteria=f"Acceptance criteria for task {i}",
                priority=random.randint(0, 9),
                severity=random.randint(0, 9)
            )
            task_ids.append(task_id)
            if i % 200 == 0:
                print(f"    Created {i} tasks...")

        total_time = time.time() - start_time
        print(f"  Created 1000 tasks in {total_time:.2f}s")

        # Verify count
        result = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        assert len(result) == 1000, f"Expected 1000 tasks, got {len(result)}"

        print(f"  Verified 1000 tasks exist")

    def test_create_50_sprints(self):
        """Test creating 50 sprints."""
        roadmap = self.test.create_roadmap("multi-sprint-project")

        print(f"\n  Creating 50 sprints...")
        start_time = time.time()

        sprint_ids = []
        for i in range(1, 51):
            sprint_id = self.test.create_sprint(
                roadmap,
                description=f"Sprint {i}: Development Phase"
            )
            sprint_ids.append(sprint_id)
            if i % 10 == 0:
                print(f"    Created {i} sprints...")

        total_time = time.time() - start_time
        print(f"  Created 50 sprints in {total_time:.2f}s")

        # Verify count
        result = self.test.run_cmd_json(["sprint", "list", "-r", roadmap])
        assert len(result) == 50


def main():
    """Run all tests."""
    test = TestStressLoad()
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
