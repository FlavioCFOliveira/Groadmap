#!/usr/bin/env python3
"""
Test 09: Stress and Load Testing
Tests performance and behavior with large volumes of data:
- 10,000+ tasks
- 200+ sprints
- Bulk operations at scale
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

    def generate_random_description(self, length=50):
        """Generate random task description."""
        words = ['implement', 'create', 'fix', 'update', 'refactor', 'optimize',
                'design', 'develop', 'test', 'deploy', 'configure', 'integrate',
                'authentication', 'database', 'api', 'frontend', 'backend',
                'security', 'performance', 'monitoring', 'logging', 'cache',
                'user', 'admin', 'dashboard', 'report', 'export', 'import']
        return ' '.join(random.choice(words) for _ in range(5)) + ' ' + \
               ''.join(random.choices(string.ascii_lowercase, k=10))

    def test_create_10000_tasks(self):
        """Test creating 10,000 tasks and measure performance."""
        roadmap = self.test.create_roadmap("massive-load-test")

        print(f"\n  Creating 10,000 tasks...")
        start_time = time.time()

        task_ids = []
        batch_size = 100

        for batch in range(0, 10000, batch_size):
            batch_start = time.time()
            for i in range(batch_size):
                task_num = batch + i + 1
                task_id = self.test.create_task(
                    roadmap,
                    description=f"Task {task_num}: {self.generate_random_description()}",
                    action=f"Execute action for task {task_num}",
                    expected_result=f"Expected result for task {task_num}",
                    priority=random.randint(0, 9),
                    severity=random.randint(0, 9)
                )
                task_ids.append(task_id)

            batch_time = time.time() - batch_start
            if batch % 1000 == 0:
                print(f"    Created {len(task_ids)} tasks... (batch time: {batch_time:.2f}s)")

        total_time = time.time() - start_time
        tasks_per_second = 10000 / total_time

        print(f"  ✓ Created 10,000 tasks in {total_time:.2f}s ({tasks_per_second:.1f} tasks/s)")

        # Verify count
        result = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        assert len(result) == 10000, f"Expected 10000 tasks, got {len(result)}"

        print(f"  ✓ Verified 10,000 tasks exist")

    def test_create_200_sprints(self):
        """Test creating 200 sprints."""
        roadmap = self.test.create_roadmap("multi-sprint-project")

        print(f"\n  Creating 200 sprints...")
        start_time = time.time()

        sprint_ids = []
        for i in range(1, 201):
            sprint_id = self.test.create_sprint(
                roadmap,
                description=f"Sprint {i}: {random.choice(['Foundation', 'Development', 'Testing', 'Deployment', 'Optimization', 'Bugfix'])} Phase"
            )
            sprint_ids.append(sprint_id)
            if i % 50 == 0:
                print(f"    Created {i} sprints...")

        total_time = time.time() - start_time
        print(f"  ✓ Created 200 sprints in {total_time:.2f}s")

        # Verify count
        result = self.test.run_cmd_json(["sprint", "list", "-r", roadmap])
        assert len(result) == 200, f"Expected 200 sprints, got {len(result)}"

    def test_distribute_tasks_across_sprints(self):
        """Test distributing 1000 tasks across 50 sprints."""
        roadmap = self.test.create_roadmap("distributed-workload")

        print(f"\n  Creating 1000 tasks and 50 sprints...")

        # Create tasks
        task_ids = []
        for i in range(1, 1001):
            task_id = self.test.create_task(
                roadmap,
                description=f"Feature Task {i}",
                action=f"Implement feature {i}",
                expected_result=f"Feature {i} working",
                priority=random.randint(1, 9)
            )
            task_ids.append(task_id)

        # Create sprints
        sprint_ids = []
        for i in range(1, 51):
            sprint_id = self.test.create_sprint(roadmap, f"Sprint {i}")
            sprint_ids.append(sprint_id)

        print(f"  Distributing tasks across sprints...")
        start_time = time.time()

        # Distribute tasks (20 tasks per sprint)
        for sprint_idx, sprint_id in enumerate(sprint_ids):
            start_task = sprint_idx * 20
            end_task = start_task + 20
            sprint_tasks = task_ids[start_task:end_task]

            self.test.run_cmd([
                "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
                ",".join(map(str, sprint_tasks))
            ])

            if (sprint_idx + 1) % 10 == 0:
                print(f"    Distributed to {sprint_idx + 1} sprints...")

        total_time = time.time() - start_time
        print(f"  ✓ Distributed 1000 tasks across 50 sprints in {total_time:.2f}s")

        # Verify distribution
        for sprint_id in sprint_ids[:5]:  # Check first 5
            result = self.test.run_cmd_json(["sprint", "tasks", "-r", roadmap, str(sprint_id)])
            assert len(result) == 20, f"Expected 20 tasks in sprint, got {len(result)}"

    def test_bulk_status_update_1000_tasks(self):
        """Test updating status of 1000 tasks at once."""
        roadmap = self.test.create_roadmap("bulk-update-test")

        print(f"\n  Creating 1000 tasks for bulk update...")

        # Create tasks and add to sprint
        task_ids = []
        sprint_id = self.test.create_sprint(roadmap, "Bulk Sprint")

        for i in range(1, 1001):
            task_id = self.test.create_task(
                roadmap,
                description=f"Bulk Task {i}",
                action=f"Action {i}",
                expected_result=f"Result {i}"
            )
            task_ids.append(task_id)

        # Add all to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        print(f"  Updating status of 1000 tasks to DOING...")
        start_time = time.time()

        # Update all to DOING in batches of 100
        for i in range(0, 1000, 100):
            batch = task_ids[i:i+100]
            self.test.run_cmd([
                "task", "stat", "-r", roadmap,
                ",".join(map(str, batch)),
                "DOING"
            ])
            if (i + 100) % 200 == 0:
                print(f"    Updated {i + 100} tasks...")

        total_time = time.time() - start_time
        print(f"  ✓ Updated 1000 tasks in {total_time:.2f}s ({1000/total_time:.1f} tasks/s)")

        # Verify
        doing_tasks = self.test.list_tasks(roadmap, status="DOING")
        assert len(doing_tasks) == 1000, f"Expected 1000 DOING tasks, got {len(doing_tasks)}"

    def test_list_tasks_with_filters_large_dataset(self):
        """Test filtering large task lists."""
        roadmap = self.test.create_roadmap("filter-performance-test")

        print(f"\n  Creating 5000 tasks with varying priorities...")

        # Create tasks with known priority distribution
        priority_counts = {i: 0 for i in range(10)}

        for i in range(1, 5001):
            priority = random.randint(0, 9)
            priority_counts[priority] += 1
            self.test.create_task(
                roadmap,
                description=f"Filtered Task {i}",
                action=f"Action {i}",
                expected_result=f"Result {i}",
                priority=priority
            )

        print(f"  Testing filters on large dataset...")

        # Test priority filter
        start_time = time.time()
        high_priority = self.test.list_tasks(roadmap, priority=8)
        filter_time = time.time() - start_time

        expected_high = priority_counts[8] + priority_counts[9]
        assert len(high_priority) == expected_high, \
            f"Expected {expected_high} high priority tasks, got {len(high_priority)}"

        print(f"  ✓ Filtered high priority tasks in {filter_time:.3f}s")

        # Test limit
        start_time = time.time()
        limited = self.test.list_tasks(roadmap, limit=100)
        limit_time = time.time() - start_time

        assert len(limited) == 100, f"Expected 100 tasks with limit, got {len(limited)}"
        print(f"  ✓ Limited to 100 tasks in {limit_time:.3f}s")

    def test_sprint_statistics_with_many_tasks(self):
        """Test sprint stats calculation with many tasks."""
        roadmap = self.test.create_roadmap("stats-calculation-test")

        print(f"\n  Creating sprint with 500 tasks...")

        sprint_id = self.test.create_sprint(roadmap, "Large Sprint")
        task_ids = []

        # Create 500 tasks
        for i in range(1, 501):
            task_id = self.test.create_task(
                roadmap,
                description=f"Stats Task {i}",
                action=f"Action {i}",
                expected_result=f"Result {i}"
            )
            task_ids.append(task_id)

        # Add to sprint
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
            ",".join(map(str, task_ids))
        ])

        # Progress various percentages
        # 100 tasks to COMPLETED
        for task_id in task_ids[:100]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        # 50 tasks to TESTING
        for task_id in task_ids[100:150]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])

        # 50 tasks to DOING
        for task_id in task_ids[150:200]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])

        print(f"  Calculating sprint statistics...")
        start_time = time.time()

        result = self.test.run_cmd_json(["sprint", "stats", "-r", roadmap, str(sprint_id)])

        calc_time = time.time() - start_time

        assert result["total_tasks"] == 500
        assert result["completed_tasks"] == 100
        assert result["progress_percentage"] == 20.0
        assert result["status_distribution"]["COMPLETED"] == 100
        assert result["status_distribution"]["TESTING"] == 50
        assert result["status_distribution"]["DOING"] == 50
        assert result["status_distribution"]["SPRINT"] == 300

        print(f"  ✓ Calculated stats for 500 tasks in {calc_time:.3f}s")

    def test_concurrent_sprint_operations(self):
        """Test operations on multiple sprints simultaneously."""
        roadmap = self.test.create_roadmap("concurrent-sprints")

        print(f"\n  Creating 20 sprints with 50 tasks each...")

        all_sprints = []
        all_tasks = []

        # Create 20 sprints
        for i in range(1, 21):
            sprint_id = self.test.create_sprint(roadmap, f"Concurrent Sprint {i}")
            all_sprints.append(sprint_id)

        # Create 1000 tasks
        for i in range(1, 1001):
            task_id = self.test.create_task(
                roadmap,
                description=f"Concurrent Task {i}",
                action=f"Action {i}",
                expected_result=f"Result {i}"
            )
            all_tasks.append(task_id)

        # Distribute tasks (50 per sprint)
        print(f"  Distributing tasks to sprints...")
        for i, sprint_id in enumerate(all_sprints):
            start_idx = i * 50
            sprint_tasks = all_tasks[start_idx:start_idx + 50]
            self.test.run_cmd([
                "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
                ",".join(map(str, sprint_tasks))
            ])

        # Start all sprints
        print(f"  Starting all 20 sprints...")
        start_time = time.time()

        for sprint_id in all_sprints:
            self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_id)])

        start_time_total = time.time() - start_time
        print(f"  ✓ Started 20 sprints in {start_time_total:.2f}s")

        # Verify all are open
        open_sprints = self.test.list_sprints(roadmap, status="OPEN")
        assert len(open_sprints) == 20

    def test_audit_log_with_high_volume(self):
        """Test audit log with high volume of operations."""
        roadmap = self.test.create_roadmap("high-volume-audit")

        print(f"\n  Generating 500 audit entries...")

        # Create tasks and modify them to generate audit entries
        for i in range(1, 101):
            task_id = self.test.create_task(
                roadmap,
                description=f"Audit Task {i}",
                action=f"Action {i}",
                expected_result=f"Result {i}",
                priority=random.randint(0, 9)
            )

            # Generate multiple audit entries per task
            self.test.run_cmd(["task", "prio", "-r", roadmap, str(task_id), str(random.randint(0, 9))])
            self.test.run_cmd(["task", "sev", "-r", roadmap, str(task_id), str(random.randint(0, 9))])

        print(f"  Querying audit log...")
        start_time = time.time()

        result = self.test.run_cmd_json(["audit", "list", "-r", roadmap, "-l", "500"])

        query_time = time.time() - start_time

        assert len(result) >= 300, f"Expected at least 300 audit entries, got {len(result)}"
        print(f"  ✓ Queried {len(result)} audit entries in {query_time:.3f}s")

    def test_roadmap_with_mixed_entities(self):
        """Test roadmap with mix of tasks, sprints, and operations."""
        roadmap = self.test.create_roadmap("mixed-entities-roadmap")

        print(f"\n  Creating complex roadmap with multiple entity types...")

        # Create 1000 tasks
        task_ids = []
        for i in range(1, 1001):
            task_id = self.test.create_task(
                roadmap,
                description=f"Mixed Task {i}",
                action=f"Action {i}",
                expected_result=f"Result {i}",
                priority=random.randint(0, 9),
                severity=random.randint(0, 9)
            )
            task_ids.append(task_id)

        # Create 50 sprints
        sprint_ids = []
        for i in range(1, 51):
            sprint_id = self.test.create_sprint(roadmap, f"Mixed Sprint {i}")
            sprint_ids.append(sprint_id)

        # Distribute tasks
        for i, sprint_id in enumerate(sprint_ids):
            start_idx = i * 20
            sprint_tasks = task_ids[start_idx:start_idx + 20]
            self.test.run_cmd([
                "sprint", "add-tasks", "-r", roadmap, str(sprint_id),
                ",".join(map(str, sprint_tasks))
            ])

        # Progress some tasks in each sprint
        for sprint_idx, sprint_id in enumerate(sprint_ids[:10]):
            sprint_tasks = task_ids[sprint_idx*20:(sprint_idx+1)*20]

            # Complete 5 tasks
            for task_id in sprint_tasks[:5]:
                self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
                self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
                self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

            # Move 5 to DOING
            for task_id in sprint_tasks[5:10]:
                self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])

        # Verify final state
        print(f"  Verifying final state...")

        all_tasks = self.test.list_tasks(roadmap)
        assert len(all_tasks) == 1000

        all_sprints = self.test.list_sprints(roadmap)
        assert len(all_sprints) == 50

        completed_tasks = self.test.list_tasks(roadmap, status="COMPLETED")
        assert len(completed_tasks) == 50  # 5 per sprint * 10 sprints

        print(f"  ✓ Verified: 1000 tasks, 50 sprints, {len(completed_tasks)} completed")

    def test_memory_efficiency_large_dataset(self):
        """Test that operations remain efficient with large datasets."""
        roadmap = self.test.create_roadmap("memory-efficiency-test")

        print(f"\n  Creating 2000 tasks for memory test...")

        # Create tasks
        for i in range(1, 2001):
            self.test.create_task(
                roadmap,
                description=f"Memory Test Task {i}",
                action=f"Action {i}",
                expected_result=f"Result {i}"
            )

        # Test multiple list operations
        print(f"  Testing list operations...")

        operations = [
            ("List all", lambda: self.test.list_tasks(roadmap)),
            ("Filter by status", lambda: self.test.list_tasks(roadmap, status="BACKLOG")),
            ("Filter by priority", lambda: self.test.list_tasks(roadmap, priority=5)),
            ("Limit 100", lambda: self.test.list_tasks(roadmap, limit=100)),
        ]

        for name, operation in operations:
            start_time = time.time()
            result = operation()
            elapsed = time.time() - start_time
            print(f"    {name}: {len(result)} results in {elapsed:.3f}s")
            assert elapsed < 5.0, f"{name} took too long: {elapsed:.3f}s"

        print(f"  ✓ All operations completed efficiently")


def main():
    """Run all stress tests."""
    test = TestStressLoad()

    methods = [m for m in dir(test) if m.startswith("test_")]
    passed = 0
    failed = 0

    print("="*60)
    print("STRESS AND LOAD TESTS")
    print("="*60)
    print("Warning: These tests create large amounts of data")
    print("and may take several minutes to complete.")
    print("="*60)

    for method_name in methods:
        test.setup_method()
        try:
            getattr(test, method_name)()
            passed += 1
        except Exception as e:
            print(f"\n✗ {method_name} failed: {e}")
            failed += 1
        finally:
            test.teardown_method()

    print(f"\n{'='*60}")
    print(f"STRESS TEST SUMMARY: {passed} passed, {failed} failed")
    print(f"{'='*60}")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
