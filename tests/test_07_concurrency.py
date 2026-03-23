#!/usr/bin/env python3
"""
Test 07: Concurrent Roadmap Operations
Tests concurrent access to the same roadmap by multiple processes.
Validates race condition handling on the .current file and database.
"""

import sys
import os
import subprocess
import tempfile
import time
import threading
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestConcurrency:
    """Test concurrent roadmap operations."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_concurrent_task_creation(self):
        """Test that multiple threads can safely create tasks concurrently."""
        roadmap = self.test.create_roadmap()

        errors = []
        created_tasks = []
        lock = threading.Lock()

        def create_task_worker(worker_id):
            try:
                for i in range(5):
                    task_id = self.test.create_task(
                        roadmap,
                        f"Task from worker {worker_id} attempt {i}",
                        f"Functional {worker_id}-{i}",
                        f"Technical {worker_id}-{i}",
                        f"Criteria {worker_id}-{i}"
                    )
                    with lock:
                        created_tasks.append(task_id)
                    time.sleep(0.01)  # Small delay to increase chance of overlap
            except Exception as e:
                with lock:
                    errors.append((worker_id, str(e)))

        # Spawn multiple threads
        threads = []
        for i in range(3):
            t = threading.Thread(target=create_task_worker, args=(i,))
            threads.append(t)
            t.start()

        # Wait for all threads to complete
        for t in threads:
            t.join()

        # Verify no errors occurred
        assert len(errors) == 0, f"Errors during concurrent task creation: {errors}"

        # Verify all tasks were created
        assert len(created_tasks) == 15, f"Expected 15 tasks, got {len(created_tasks)}"

        # Verify tasks are accessible
        result = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        assert len(result) == 15, f"Expected 15 tasks in list, got {len(result)}"

        print("✓ Concurrent task creation test passed")

    def test_concurrent_roadmap_switch(self):
        """Test that switching roadmaps concurrently doesn't corrupt .current file."""
        # Create multiple roadmaps
        roadmaps = [self.test.create_roadmap(f"roadmap_{i}") for i in range(3)]

        errors = []
        lock = threading.Lock()

        def switch_roadmap_worker(worker_id):
            try:
                for i in range(10):
                    roadmap = roadmaps[i % len(roadmaps)]
                    self.test.run_cmd(["roadmap", "use", roadmap])
                    time.sleep(0.005)  # Small delay to increase chance of overlap
            except Exception as e:
                with lock:
                    errors.append((worker_id, str(e)))

        # Spawn multiple threads
        threads = []
        for i in range(5):
            t = threading.Thread(target=switch_roadmap_worker, args=(i,))
            threads.append(t)
            t.start()

        # Wait for all threads to complete
        for t in threads:
            t.join()

        # Verify no errors occurred
        assert len(errors) == 0, f"Errors during concurrent roadmap switch: {errors}"

        # Verify .current file is still valid
        current_file = Path(self.test.home_dir) / ".roadmaps" / ".current"
        if current_file.exists():
            content = current_file.read_text().strip()
            assert content in roadmaps, f"Current roadmap {content} not in expected list"

        print("✓ Concurrent roadmap switch test passed")

    def test_concurrent_status_changes(self):
        """Test concurrent status changes on the same task."""
        roadmap = self.test.create_roadmap()

        # Create a sprint and add a task
        sprint_id = self.test.create_sprint(roadmap, "Test Sprint")
        task_id = self.test.create_task(
            roadmap, "Test task", "Functional", "Technical", "Criteria"
        )
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(task_id)
        ])

        errors = []
        lock = threading.Lock()

        def status_change_worker(worker_id):
            try:
                statuses = ["DOING", "TESTING", "COMPLETED", "BACKLOG"]
                for i in range(5):
                    status = statuses[i % len(statuses)]
                    try:
                        self.test.run_cmd([
                            "task", "stat", "-r", roadmap, str(task_id), status
                        ])
                    except AssertionError:
                        # Some transitions may fail (expected), ignore those
                        pass
                    time.sleep(0.01)
            except Exception as e:
                with lock:
                    errors.append((worker_id, str(e)))

        # Spawn multiple threads
        threads = []
        for i in range(3):
            t = threading.Thread(target=status_change_worker, args=(i,))
            threads.append(t)
            t.start()

        # Wait for all threads to complete
        for t in threads:
            t.join()

        # Verify no unexpected errors occurred
        # (some status transition errors are expected)
        unexpected_errors = [(w, e) for w, e in errors if "transition" not in e.lower()]
        assert len(unexpected_errors) == 0, f"Unexpected errors: {unexpected_errors}"

        # Verify task is still accessible and has a valid status
        # task get returns a JSON array even for a single ID (see SPEC/COMMANDS.md)
        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["status"] in ["BACKLOG", "SPRINT", "DOING", "TESTING", "COMPLETED"]

        print("✓ Concurrent status changes test passed")

    def test_concurrent_read_and_write(self):
        """Test concurrent read and write operations."""
        roadmap = self.test.create_roadmap()

        # Create initial tasks
        task_ids = []
        for i in range(5):
            task_id = self.test.create_task(
                roadmap, f"Task {i}", "Functional", "Technical", "Criteria"
            )
            task_ids.append(task_id)

        errors = []
        results = []
        lock = threading.Lock()

        def reader_worker(worker_id):
            try:
                for _ in range(10):
                    result = self.test.run_cmd_json(["task", "list", "-r", roadmap])
                    with lock:
                        results.append(len(result))
                    time.sleep(0.01)
            except Exception as e:
                with lock:
                    errors.append((f"reader-{worker_id}", str(e)))

        def writer_worker(worker_id):
            try:
                for i in range(3):
                    self.test.create_task(
                        roadmap, f"New task {worker_id}-{i}",
                        "Functional", "Technical", "Criteria"
                    )
                    time.sleep(0.02)
            except Exception as e:
                with lock:
                    errors.append((f"writer-{worker_id}", str(e)))

        # Spawn reader and writer threads
        threads = []
        for i in range(3):
            t = threading.Thread(target=reader_worker, args=(i,))
            threads.append(t)
            t.start()
        for i in range(2):
            t = threading.Thread(target=writer_worker, args=(i,))
            threads.append(t)
            t.start()

        # Wait for all threads to complete
        for t in threads:
            t.join()

        # Verify no errors occurred
        assert len(errors) == 0, f"Errors during concurrent read/write: {errors}"

        # Verify final state is consistent
        final_result = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        expected_count = 5 + 2 * 3  # Initial 5 + 2 writers * 3 tasks each
        assert len(final_result) == expected_count, \
            f"Expected {expected_count} tasks, got {len(final_result)}"

        print("✓ Concurrent read and write test passed")

    def test_subprocess_concurrent_access(self):
        """Test concurrent access from multiple subprocesses."""
        roadmap = self.test.create_roadmap()

        # Create a script that will be run by multiple subprocesses
        script = f"""
import subprocess
import json
import sys

# Create a task
result = subprocess.run(
    ["{self.test.cli_path}", "task", "create", "-r", "{roadmap}",
     "-t", "Subprocess task",
     "-fr", "Functional",
     "-tr", "Technical",
     "-ac", "Criteria"],
    capture_output=True,
    text=True
)
print(result.stdout)
print(result.stderr, file=sys.stderr)
sys.exit(result.returncode)
"""

        # Run multiple subprocesses concurrently
        processes = []
        for i in range(5):
            env = os.environ.copy()
            env["HOME"] = str(self.test.home_dir)
            p = subprocess.Popen(
                [sys.executable, "-c", script],
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                env=env
            )
            processes.append(p)

        # Wait for all processes to complete
        results = []
        for p in processes:
            stdout, stderr = p.communicate()
            results.append((p.returncode, stdout, stderr))

        # Check results
        for i, (returncode, stdout, stderr) in enumerate(results):
            if returncode != 0:
                print(f"Warning: Subprocess {i} failed with code {returncode}: {stderr}")

        # At least some should succeed (SQLite handles concurrent writes)
        success_count = sum(1 for r, _, _ in results if r == 0)
        assert success_count >= 3, f"Only {success_count}/5 subprocesses succeeded"

        # Verify tasks were created
        final_result = self.test.run_cmd_json(["task", "list", "-r", roadmap])
        assert len(final_result) >= 3, f"Expected at least 3 tasks, got {len(final_result)}"

        print("✓ Subprocess concurrent access test passed")


if __name__ == "__main__":
    import pytest
    pytest.main([__file__, "-v"])
