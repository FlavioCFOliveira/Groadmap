#!/usr/bin/env python3
"""
Groadmap CLI Test Runner

Runs all test suites and generates a summary report.

Usage:
    python run_tests.py              # Run standard tests
    python run_tests.py --stress     # Run stress tests only
    python run_tests.py --all        # Run all tests including stress
"""

import ast
import sys
import os
import re
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
    "test_07_concurrency",
    "test_08_complex_workflow",
    "test_10_task_next",
    "test_11_sprint_show",
    "test_12_sprint_stats",
    "test_13_sprint_task_ordering",
    "test_14_audit_date_filters",
    "test_15_roadmap_stats",
    "test_16_boundary_unicode",
    "test_17_task_type_flag",
    "test_18_cli_validation_data_integrity",
    "test_19_completion_summary",
    "test_20_task90_sprint_closed_guard",
    "test_21_task89_move_tasks_closed_guard",
    "test_22_task87_sprint_capacity",
    "test_23_backlog_management",
    "test_24_dependency_workflow",
    "test_25_completion_guards",
    "test_26_timing_realism",
    "test_27_exit_code_extremes",
    "test_28_command_aliases",
    "test_29_subprocess_concurrency",
    "test_30_aihelp_contract",
    "test_31_sprint_description_limit",
    "test_32_layout_migration",
    "test_33_graph_checkpoint",
    "test_34_graph_realistic_usage",
    "test_35_web_interface",
    "test_36_query_commands_correctness",
    "test_37_write_persistence_fidelity",
    "test_38_task_list_date_filters",
    "test_39_graph_guardrail_literals",
    "test_40_graph_notifications",
    "test_41_graph_concurrency_input",
    "test_42_security_audit",
    "test_43_sprint_order_field",
    "test_44_help_and_exitcode_contract",
    "test_45_audit_stats_keys",
    "test_46_graph_parallel_edge_predicates",
    "test_47_install_script_extraction",
    "test_48_graph_clause_surface",
    "test_49_install_platform_guards",
    "test_50_task_and_sprint_comments",
    "test_51_specialists_field_removal",
    "test_52_commit_tracking",
    "test_53_e2e_harness_binary_staleness",
    "test_54_audit_enrichment_e2e",
    "test_55_error_string_parity",
    "test_56_graph_read_direction",
    "test_57_positional_arity",
    "test_58_ai_contract_error_parity",
    "test_59_graph_property_value_content",
]

# Stress tests (run separately due to time/data volume)
STRESS_TEST_MODULES = [
    "test_09_stress_load",
]


def assert_no_dormant_modules() -> list[str]:
    """Guard against dormant tests: every tests/test_*.py on disk must be
    registered in TEST_MODULES or STRESS_TEST_MODULES. A test file that exists
    but is not registered never runs, providing a false sense of coverage.

    Returns the list of unregistered module names (empty when all are wired).
    """
    registered = set(TEST_MODULES) | set(STRESS_TEST_MODULES)
    on_disk = {
        p.stem
        for p in Path(__file__).parent.glob("test_*.py")
    }
    return sorted(on_disk - registered)


# Two idiom families let a module enumerate its own `Test*`/`*Tests` suite
# classes by introspecting its own module object at run time, instead of
# naming them in a fixed list: `inspect.getmembers`/`vars`/`dir` or `globals()`
# over the module's own namespace (`sys.modules[__name__]`, or `globals()`
# called from inside the module itself), filtered down to classes with
# `isinstance(obj, type)` / `inspect.isclass`. A runner built on either idiom
# picks up a class the moment it is defined, so it cannot go stale the way
# rmp task #303 describes -- see test_48_graph_clause_surface.py's `_run_all`
# for the canonical shape.
_DISCOVERY_NAMESPACE_MARKERS = ("sys.modules[__name__]", "globals()")
_DISCOVERY_CLASS_FILTER_MARKERS = ("isinstance(obj, type)", "inspect.isclass")


def _module_defines_dynamic_class_discovery(source: str) -> bool:
    """True when `source` enumerates its own test classes by introspecting
    its own module namespace rather than naming them in a fixed list."""
    return (
        any(marker in source for marker in _DISCOVERY_NAMESPACE_MARKERS)
        and any(marker in source for marker in _DISCOVERY_CLASS_FILTER_MARKERS)
    )


def _module_suite_classes(tree: ast.Module) -> list[str]:
    """Top-level classes in `tree` that look like test suites under this
    repository's two naming conventions (`Test...` / `...Tests`) and carry at
    least one `test_*` method -- a class matching the name convention with no
    test method is a fixture/helper, not a suite, and is not counted."""
    names = []
    for node in tree.body:
        if not isinstance(node, ast.ClassDef):
            continue
        if not (node.name.startswith("Test") or node.name.endswith("Tests")):
            continue
        has_test_method = any(
            isinstance(item, (ast.FunctionDef, ast.AsyncFunctionDef))
            and item.name.startswith("test_")
            for item in node.body
        )
        if has_test_method:
            names.append(node.name)
    return names


def _referenced_names(tree: ast.Module) -> set[str]:
    """Every bare identifier loaded anywhere in `tree`. Catches a class named
    in a hardcoded runner list in any shape this suite uses (a flat tuple, a
    list, a list of (label, class) pairs, ...) without needing to parse the
    exact shape of the loop that walks it."""
    return {
        node.id
        for node in ast.walk(tree)
        if isinstance(node, ast.Name) and isinstance(node.ctx, ast.Load)
    }


def assert_no_class_shortfall() -> list[str]:
    """Guard against a module executing fewer test classes than it defines
    (rmp task #303): a `Test*`/`*Tests` class appended to a registered module
    without also being wired into that module's runner never runs, yet the
    module still exits 0 with a smaller, misleadingly "passing" count -- see
    test_48_graph_clause_surface.py's docstring for the measured example (32
    passed before the fix, 39 after, with 7 tests that had never run before).

    A module built on the dynamic-discovery idiom (see
    `_module_defines_dynamic_class_discovery`) is exempt: it has no fixed list
    to fall behind the classes actually defined. Every other registered module
    must reference each of its own suite classes somewhere in its own source
    (its runner's list/tuple, however shaped) -- a class satisfying neither
    guarantee is reported here.

    Returns a list of "<module>: <ClassName>[, ClassName...]" strings, one per
    registered module with at least one apparently-unwired suite class (empty
    when every registered module accounts for all the classes it defines).
    """
    problems = []
    for module_name in TEST_MODULES + STRESS_TEST_MODULES:
        path = Path(__file__).parent / f"{module_name}.py"
        try:
            source = path.read_text(encoding="utf-8")
        except OSError:
            continue
        try:
            tree = ast.parse(source, filename=str(path))
        except SyntaxError:
            continue

        defined = _module_suite_classes(tree)
        if not defined:
            continue
        if _module_defines_dynamic_class_discovery(source):
            continue

        referenced = _referenced_names(tree)
        missing = [name for name in defined if name not in referenced]
        if missing:
            problems.append(f"{module_name}: {', '.join(missing)}")
    return problems


# A module row in the tests/README.md table:
#   | `test_01_basic_crud.py` | Roadmap and task CRUD: ... |
_README_MODULE_ROW = re.compile(r"^\|\s*`(test_[0-9A-Za-z_]+)\.py`\s*\|")

README_PATH = Path(__file__).parent / "README.md"


def assert_readme_documents_every_module() -> tuple[list[str], list[str], list[str]]:
    """Guard against a stale index: tests/README.md carries one table row per
    registered module saying what that module covers, and it is the only place
    the suite says so -- this file holds names, not meaning. A registered
    module with no row hides its coverage from every reader who consults the
    table; a row naming a module registered nowhere promises coverage that
    does not run; a module listed twice makes the table's own count wrong.

    Returns (missing, unknown, duplicated):
        missing    -- registered here, absent from the README table
        unknown    -- present in the README table, registered nowhere
        duplicated -- listed by the README table more than once

    A README that cannot be read reports every registered module as missing,
    so the check fails loudly instead of passing on an absent file.
    """
    registered = set(TEST_MODULES) | set(STRESS_TEST_MODULES)

    try:
        readme = README_PATH.read_text(encoding="utf-8")
    except OSError:
        readme = ""

    documented = [
        match.group(1)
        for match in (_README_MODULE_ROW.match(line) for line in readme.splitlines())
        if match
    ]

    missing = sorted(registered - set(documented))
    unknown = sorted(set(documented) - registered)
    duplicated = sorted({name for name in documented if documented.count(name) > 1})
    return missing, unknown, duplicated


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

    # Fail fast on dormant tests: a test_*.py file that exists but is not
    # registered would never run, masking a coverage gap.
    dormant = assert_no_dormant_modules()
    if dormant:
        print("=" * 60)
        print("ERROR: unregistered test modules detected (they never run):")
        for name in dormant:
            print(f"  - {name}")
        print("Add them to TEST_MODULES or STRESS_TEST_MODULES in run_tests.py.")
        print("=" * 60)
        return False

    # Fail fast on a class shortfall: a module that defines a suite class its
    # own runner never mentions would exit 0 while quietly never running that
    # class's tests (rmp task #303).
    shortfall = assert_no_class_shortfall()
    if shortfall:
        print("=" * 60)
        print("ERROR: a module defines test classes its runner never wires in:")
        for entry in shortfall:
            print(f"  - {entry}")
        print("Wire every Test*/*Tests class into the module's runner, or switch the")
        print("runner to introspect its own module's namespace (see")
        print("test_48_graph_clause_surface.py's _run_all for the model).")
        print("=" * 60)
        return False

    # Fail fast on a stale index: the README table is the suite's only record
    # of what each module covers, so a table that stopped tracking the registry
    # misinforms everyone who trusts it.
    missing, unknown, duplicated = assert_readme_documents_every_module()
    if missing or unknown or duplicated:
        print("=" * 60)
        print("ERROR: tests/README.md module table disagrees with the registry:")
        for name in missing:
            print(f"  - {name}.py: registered here, no row in the table")
        for name in unknown:
            print(f"  - {name}.py: row in the table, registered nowhere")
        for name in duplicated:
            print(f"  - {name}.py: listed by the table more than once")
        print("Give every registered module exactly one row in the '## Test Modules'")
        print("table of tests/README.md, stating what that module covers.")
        print("=" * 60)
        return False

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
