#!/usr/bin/env python3
"""
Groadmap CLI Test Runner

Runs all test suites and generates a summary report.

Usage:
    python run_tests.py              # Run standard tests
    python run_tests.py --stress     # Run stress tests only
    python run_tests.py --all        # Run all tests including stress
"""

import sys
import os
import subprocess
import argparse
from pathlib import Path
from datetime import datetime

# Test modules
TEST_MODULES = [
    "test_01_basic_crud",
    "test_02_sprint_lifecycle",
    "test_03_task_state_machine",
    "test_04_sprint_task_management",
    "test_05_audit_reporting",
    "test_06_edge_cases_errors",
    "test_08_complex_workflow",
]

# Stress tests (run separately due to time/data volume)
STRESS_TEST_MODULES = [
    "test_09_stress_load",
]


def run_test_module(module_name: str) -> tuple[bool, str]:
    """
    Run a single test module.

    Returns:
        Tuple of (success, output)
    """
    print(f"\n{'='*60}")
    print(f"Running {module_name}...")
    print('='*60)

    module_path = Path(__file__).parent / f"{module_name}.py"

    if not module_path.exists():
        return False, f"Module {module_name} not found"

    result = subprocess.run(
        [sys.executable, str(module_path)],
        capture_output=True,
        text=True
    )

    success = result.returncode == 0
    output = result.stdout + result.stderr

    if success:
        print(f"✓ {module_name} PASSED")
    else:
        print(f"✗ {module_name} FAILED")

    return success, output


def run_tests(modules: list[str], title: str) -> tuple[int, int]:
    """Run a set of test modules and return results."""
    print("="*60)
    print(title)
    print("="*60)
    print(f"Started at: {datetime.now().isoformat()}")
    print()

    results = {}
    passed_count = 0
    failed_count = 0

    for module in modules:
        success, output = run_test_module(module)
        results[module] = {
            "success": success,
            "output": output
        }

        if success:
            passed_count += 1
        else:
            failed_count += 1

    # Print summary
    print("\n" + "="*60)
    print("TEST SUMMARY")
    print("="*60)
    print(f"Total: {len(modules)}")
    print(f"Passed: {passed_count}")
    print(f"Failed: {failed_count}")
    if len(modules) > 0:
        print(f"Success Rate: {passed_count/len(modules)*100:.1f}%")
    print("="*60)

    # Print failed tests details
    if failed_count > 0:
        print("\nFailed Tests:")
        for module, result in results.items():
            if not result["success"]:
                print(f"\n{module}:")
                print("-" * 40)
                print(result["output"])

    return passed_count, failed_count


def main():
    """Run tests based on command line arguments."""
    parser = argparse.ArgumentParser(description="Groadmap CLI Test Runner")
    parser.add_argument(
        "--stress",
        action="store_true",
        help="Run stress tests only"
    )
    parser.add_argument(
        "--all",
        action="store_true",
        help="Run all tests including stress tests"
    )
    parser.add_argument(
        "--quick",
        action="store_true",
        help="Run only quick tests (exclude stress tests)"
    )

    args = parser.parse_args()

    if args.stress:
        # Run only stress tests
        passed, failed = run_tests(STRESS_TEST_MODULES, "STRESS TESTS")
    elif args.all:
        # Run all tests
        passed1, failed1 = run_tests(TEST_MODULES, "STANDARD TESTS")
        passed2, failed2 = run_tests(STRESS_TEST_MODULES, "STRESS TESTS")
        passed = passed1 + passed2
        failed = failed1 + failed2

        print("\n" + "="*60)
        print("OVERALL SUMMARY")
        print("="*60)
        print(f"Total Passed: {passed}")
        print(f"Total Failed: {failed}")
        print("="*60)
    else:
        # Run standard tests by default
        passed, failed = run_tests(TEST_MODULES, "Groadmap CLI Test Suite")

    return failed == 0


if __name__ == "__main__":
    success = main()
    sys.exit(0 if success else 1)
