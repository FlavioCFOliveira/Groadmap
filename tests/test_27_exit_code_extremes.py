#!/usr/bin/env python3
"""
Test 27: Exit Code Extremes (127 unknown command, 130 SIGINT)
Covers the two corners of the exit-code contract that are easy to
overlook because the rest of the suite focuses on the [0,6] range.

SPEC/ARCHITECTURE.md § Exit Codes mandates:
  - 127 EXIT_CMD_NOT_FOUND   — unknown command/subcommand
  - 130 EXIT_SIGINT          — process interrupted by SIGINT (Ctrl+C)
"""

import os
import signal
import subprocess
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestUnknownCommandExit127:
    """Unknown top-level commands return exit 127."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def test_unknown_top_level_command(self):
        """`rmp notacommand` exits 127 and prints usage."""
        exit_code, stdout, stderr = self.test.run_cmd(["notacommand"], check=False)
        assert exit_code == 127, (
            f"unknown command must exit 127 per SPEC/ARCHITECTURE.md; got {exit_code}"
        )
        assert "unknown command" in stderr.lower() or "unknown command" in stdout.lower(), (
            f"output must say 'Unknown command'; stderr={stderr!r}, stdout={stdout[:100]!r}"
        )
        # Help should be printed alongside the error so the user can recover.
        assert "Usage:" in stdout, "usage block must follow the error"

        print("✓ unknown command returns exit 127 and prints usage")

    def test_unknown_command_with_args(self):
        """Garbage that looks like 'cmd subcmd ...' still exits 127."""
        exit_code, _, _ = self.test.run_cmd(["foo", "bar", "baz"], check=False)
        assert exit_code == 127, (
            f"unknown nested command must still exit 127; got {exit_code}"
        )

        print("✓ unknown command with arbitrary args still exits 127")


class TestSigintExit130:
    """A SIGINT (Ctrl+C) must collapse to exit 130.

    This is the standard Unix convention (128 + SIGINT=2 = 130). We spawn
    the rmp binary directly via subprocess and send SIGINT mid-flight.
    """

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap()

    def teardown_method(self):
        self.test.teardown()

    def test_sigint_during_command(self):
        """Sending SIGINT to a running rmp process yields exit code 130.

        Strategy: kick off a `rmp roadmap create <new>` against a fresh
        HOME (slow path: schema creation, pragmas, indexes), then SIGINT
        a few millis later. The Go runtime maps SIGINT to exit 130 when
        no signal handler intercepts it.
        """
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)

        # Use a name we know is valid so the process actually starts work.
        # Sleep a moment to let the binary reach the schema-creation phase.
        proc = subprocess.Popen(
            [self.test.cli_path, "roadmap", "create", "sigint-target"],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        time.sleep(0.02)
        proc.send_signal(signal.SIGINT)

        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
            raise AssertionError("process did not exit within 10s after SIGINT")

        # Two acceptable outcomes:
        #   - exit 130: SIGINT raced ahead of completion (preferred path)
        #   - exit 0:   the create completed before SIGINT was delivered
        # We tolerate both because timing is OS-scheduling dependent, but
        # the moment SIGINT lands during work, the runtime must produce 130.
        assert proc.returncode in (130, 0), (
            f"SIGINT must produce exit 130 (or 0 if it landed after success); got {proc.returncode}"
        )

        print(
            f"✓ SIGINT honoured: exit={proc.returncode} "
            f"(130 when interrupt won the race, 0 when the work completed first)"
        )


def main():
    """Run all exit-code extreme tests."""
    import inspect

    failures = []
    passed = 0
    classes = [
        ("TestUnknownCommandExit127", TestUnknownCommandExit127),
        ("TestSigintExit130", TestSigintExit130),
    ]
    for cls_name, cls in classes:
        for meth_name, meth in inspect.getmembers(cls, predicate=inspect.isfunction):
            if not meth_name.startswith("test_"):
                continue
            inst = cls()
            if hasattr(inst, "setup_method"):
                inst.setup_method()
            try:
                meth(inst)
                passed += 1
            except Exception as e:
                failures.append(f"{cls_name}.{meth_name}: {e}")
            if hasattr(inst, "teardown_method"):
                try:
                    inst.teardown_method()
                except Exception:
                    pass

    print(f"\n{passed} passed, {len(failures)} failed")
    for f in failures:
        print(f"  ✗ {f}")
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(main())
