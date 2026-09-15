#!/usr/bin/env python3
"""
Test 74: help tokens only in a token position, and a flag's value in the joined
form (rmp tasks #476 and #477; SPEC/HELP.md § Help tokens, SPEC/COMMANDS.md
§ Positional Arguments rule 5, § Roadmap Name Validation, § Serve Options,
§ Client Options, SPEC/GRAPH.md § Cypher Input Source and Precedence rule 4).

## The defects

`--help`, `-h` and `help` were counted anywhere in the argument list, the value
of a flag included. `rmp task create -t help ...` wrote the help and created
nothing, `rmp sprint create -d help` likewise, and `rmp task list -r help` wrote
the help instead of refusing the roadmap name.

`rmp web` split every token on its first `=` before comparing it with the help
tokens, so `rmp web --help=1` served the help. `graph serve`, `graph client` and
`web` named an unknown flag with its whole token (`unknown flag: --zzz=1`) while
every other command strips the `=value`, and `graph serve --socket=<path>`,
`graph client --socket=<path>` and `graph client --query=<cypher>` were refused
outright.

## What this module holds

  - A help token in a token position still writes the help, byte for byte the
    help `rmp <command> <subcommand> --help` writes, over the roadmap selector,
    a free-text flag, an enum flag, a boolean flag and an undeclared flag.
  - The same word as the value of a flag is that value: a task and a sprint are
    created and read back, a status is refused as a status, and a roadmap name is
    refused as reserved, on a family subcommand and on `stats`.
  - `--help=<value>` and `-h=<value>` are unknown flags on a shared-parser
    command, on a positional command and on `web`, and a token written where a
    command or subcommand is expected is a dispatch failure.
  - The stripped unknown-flag line on `graph serve`, `graph client` and `web`.
  - The joined form accepted on the two graph commands against real servers:
    `serve --socket=<path>` binds that path, and `client --socket=<path>`,
    `--query=` and `-q=` reach it and write to the graph.
"""

import inspect
import json
import os
import signal
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, _StreamDrain


RESERVED_HELP_LINE = 'Error: validation error: "help": roadmap name is a reserved system name'


class HelpTokenBase:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    def rmp(self, args, timeout=20.0):
        return self.test.run_cli(args, timeout=timeout)

    def must_json(self, args):
        code, out, err = self.rmp(args)
        assert code == 0, f"rmp {' '.join(args)}: exit {code} stderr={err!r}"
        return json.loads(out)

    def first_line(self, err):
        return err.splitlines()[0] if err else ""

    def help_of(self, *words):
        code, out, err = self.rmp(list(words) + ["--help"])
        assert code == 0 and out, f"rmp {' '.join(words)} --help: exit {code} stderr={err!r}"
        return out

    def ledger(self):
        name = "treasury-ledger"
        self.test.create_roadmap(name)
        self.test.create_task(name, "Reconcile settlement batches nightly",
                              "Finance needs matched batches every morning",
                              "Compare the acquirer file with the ledger",
                              "Unmatched batches are listed by 07:00")
        self.test.create_sprint(name, "Deliver nightly settlement reconciliation",
                                "Settlement reconciliation")
        return name


class TestHelpTokenInATokenPosition(HelpTokenBase):
    """Where a help token still asks for help (rmp task #476)."""

    def test_every_token_position_writes_the_help_of_its_level(self):
        r = self.ledger()
        cases = [
            (["stats", "-r", r, "help"], ("stats",)),
            (["task", "list", "-r", r, "--help"], ("task", "list")),
            (["task", "list", "-r", r, "help"], ("task", "list")),
            (["task", "create", "-r", r, "-t", "--help"], ("task", "create")),
            (["task", "create", "-r", r, "-t", "Settle refunds", "help"], ("task", "create")),
            (["task", "list", "-r", r, "-s", "--help"], ("task", "list")),
            (["task", "list", "-r", r, "-s", "BACKLOG", "-h"], ("task", "list")),
            (["sprint", "tasks", "-r", r, "1", "--order-by-priority", "help"], ("sprint", "tasks")),
            (["task", "list", "-r", r, "--foo", "help"], ("task", "list")),
        ]
        problems = []
        for args, level in cases:
            code, out, err = self.rmp(args)
            if code != 0 or out != self.help_of(*level):
                problems.append(f"rmp {' '.join(args)}: exit {code}, stdout is "
                                f"{'the help' if out == self.help_of(*level) else repr(out[:120])}, "
                                f"stderr={err[:160]!r}")
        assert not problems, "\n".join(problems)

    def test_roadmap_create_and_remove_help_write_help_and_touch_nothing(self):
        for sub in ("create", "remove"):
            code, out, err = self.rmp(["roadmap", sub, "help"])
            assert code == 0 and out == self.help_of("roadmap", sub), (
                f"rmp roadmap {sub} help: exit {code} stdout={out[:120]!r} stderr={err!r}")
            assert not (self.test.roadmaps_dir / "help").exists(), (
                f"rmp roadmap {sub} help created a roadmap directory named help")


class TestHelpTokenAsAFlagValue(HelpTokenBase):
    """Where the same word is a flag's value (rmp task #476)."""

    def test_task_create_title_help_creates_the_task(self):
        r = self.ledger()
        for title in (["-t", "help"], ["--title=help"]):
            created = self.must_json(["task", "create", "-r", r] + title + [
                "-fr", "Operators need a task literally titled help",
                "-tr", "Store the title verbatim",
                "-ac", "The stored title reads help"])
            tasks = self.must_json(["task", "get", "-r", r, str(created["id"])])
            assert tasks[0]["title"] == "help", f"{title}: stored title {tasks[0]['title']!r}"

    def test_sprint_create_description_help_creates_the_sprint(self):
        r = self.ledger()
        created = self.must_json(["sprint", "create", "-r", r, "-t", "Help desk rollout", "-d", "help"])
        sprint = self.must_json(["sprint", "get", "-r", r, str(created["id"])])
        assert sprint["description"] == "help", f"stored description {sprint['description']!r}"

    def test_the_selector_value_help_is_a_reserved_roadmap_name(self):
        for args in (["task", "list", "-r", "help"], ["stats", "-r", "help"],
                     ["task", "get", "-r", "help", "1"]):
            code, out, err = self.rmp(args)
            assert (code, self.first_line(err), out) == (6, RESERVED_HELP_LINE, ""), (
                f"rmp {' '.join(args)}: exit={code} stdout={out[:120]!r} stderr={err!r}")

    def test_an_enum_value_help_is_refused_as_that_enum(self):
        r = self.ledger()
        code, out, err = self.rmp(["task", "list", "-r", r, "-s", "help"])
        assert (code, self.first_line(err), out) == (
            6, 'Error: validation error: invalid task status: "help"', ""), (
            f"rmp task list -s help: exit={code} stdout={out[:120]!r} stderr={err!r}")


class TestHelpTokenCarryingAValue(HelpTokenBase):
    """`--help=<value>` is an unknown flag, never help (rmp task #477)."""

    def test_it_is_refused_on_every_kind_of_command(self):
        r = self.ledger()
        cases = [
            (["task", "list", "-r", r, "--help=1"], "Error: invalid input: unknown flag: --help"),
            (["task", "list", "-r", r, "-h=1"], "Error: invalid input: unknown flag: -h"),
            (["task", "get", "-r", r, "1", "--help=1"], "Error: invalid input: unknown flag: --help"),
            (["task", "get", "-r", r, "1", "--help="], "Error: invalid input: unknown flag: --help"),
            (["web", "--no-open", "--help=1"], "Error: invalid input: unknown flag: --help"),
            (["web", "--no-open", "-h="], "Error: invalid input: unknown flag: -h"),
        ]
        problems = []
        for args, line in cases:
            code, out, err = self.rmp(args)
            if (code, self.first_line(err), out) != (2, line, ""):
                problems.append(f"rmp {' '.join(args)}: exit={code} first stderr line="
                                f"{self.first_line(err)!r} stdout={out[:120]!r}; want 2, {line!r}, empty")
        assert not problems, "\n".join(problems)

    def test_in_a_command_or_subcommand_position_it_is_a_dispatch_failure(self):
        for args in (["--help=1"], ["task", "--help=1"]):
            code, out, _err = self.rmp(args)
            assert code == 127 and out == "", f"rmp {' '.join(args)}: exit={code} stdout={out[:120]!r}"


class TestTheUnknownFlagLineIsStripped(HelpTokenBase):
    """`graph serve`, `graph client` and `web` name the flag without `=value` (rmp task #477)."""

    def test_graph_serve_graph_client_and_web(self):
        r = self.ledger()
        cases = [
            ["graph", "serve", "-r", r, "--zzz=1"],
            ["graph", "client", "-r", r, "--zzz=1"],
            ["graph", "client", "-r", r, "--query", "RETURN 1", "--zzz="],
            ["web", "--no-open", "--zzz=1"],
        ]
        problems = []
        for args in cases:
            code, out, err = self.rmp(args)
            want = (2, "Error: invalid input: unknown flag: --zzz", "")
            if (code, self.first_line(err), out) != want:
                problems.append(f"rmp {' '.join(args)}: exit={code} first stderr line="
                                f"{self.first_line(err)!r} stdout={out[:120]!r}")
        assert not problems, "\n".join(problems)


class TestJoinedFormOnTheGraphCommands(HelpTokenBase):
    """`--socket=`, `--query=` and `-q=` (rmp task #477)."""

    def test_serve_binds_the_joined_socket_and_client_reaches_it(self):
        r = self.ledger()
        socket_path = str(self.test.home_dir / "treasury.sock")
        env = os.environ.copy()
        env["HOME"] = str(self.test.home_dir)
        proc = subprocess.Popen([self.test.cli_path, "graph", "serve", "-r", r, f"--socket={socket_path}"],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
        out_drain, err_drain = _StreamDrain(proc.stdout), _StreamDrain(proc.stderr)
        try:
            announced = out_drain.wait_for_json_object(15.0)
            assert announced is not None, (
                f"graph serve --socket=<path> announced nothing (exit={proc.poll()}) "
                f"stderr={err_drain.text()!r}")
            assert announced["socket"] == socket_path, f"bound {announced['socket']!r}, want {socket_path!r}"

            code, out, err = self.rmp(["graph", "client", "-r", r, f"--socket={socket_path}",
                                       "-q=CREATE (n:Spec {key:'joined-form'}) RETURN n.key AS key"])
            assert code == 0, f"client --socket= -q=: exit {code} stderr={err!r}"
            code, out, err = self.rmp(["graph", "client", "-r", r, f"--socket={socket_path}",
                                       "--query=MATCH (n:Spec {key:'joined-form'}) RETURN n.key AS key"])
            assert code == 0 and "joined-form" in out, (
                f"client --socket= --query=: exit {code} stdout={out!r} stderr={err!r}")
        finally:
            if proc.poll() is None:
                proc.send_signal(signal.SIGINT)
                try:
                    proc.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=5)
        assert proc.returncode == 0, f"graph serve exited {proc.returncode} on SIGINT"

    def test_the_value_help_is_a_statement_not_a_help_request(self):
        r = self.ledger()
        self.test.start_graph_server(r)
        code, out, err = self.rmp(["graph", "client", "-r", r, "--query=help"])
        assert code == 1 and out == "" and err, (
            f"client --query=help: exit={code} stdout={out!r} stderr={err!r}; "
            f"the statement must reach the server and be refused as one that does not parse")

    def test_an_empty_joined_socket_is_the_missing_parameter(self):
        r = self.ledger()
        for sub, extra in (("serve", []), ("client", ["--query", "RETURN 1"])):
            separate = self.rmp(["graph", sub, "-r", r, "--socket", ""] + extra)
            joined = self.rmp(["graph", sub, "-r", r, "--socket="] + extra)
            assert separate[0] == 2 and joined[0] == separate[0], (
                f"graph {sub}: --socket= exit {joined[0]}, --socket '' exit {separate[0]}")
            assert self.first_line(joined[2]) == self.first_line(separate[2]) and joined[1] == "", (
                f"graph {sub}: --socket= wrote {joined[2]!r}, --socket '' wrote {separate[2]!r}")

    def test_the_selector_stays_separate_form_only(self):
        r = self.ledger()
        code, out, _err = self.rmp(["graph", "serve", f"--roadmap={r}"])
        assert code == 3 and out == "", f"graph serve --roadmap=<name>: exit={code} stdout={out!r}"


def _run_all():
    passed = failed = 0
    failures = []
    classes = [obj for _n, obj in sorted(inspect.getmembers(sys.modules[__name__], inspect.isclass))
               if obj.__module__ == __name__ and _n.startswith("Test")]
    print(f"Discovered {len(classes)} test classes: "
          f"{', '.join(c.__name__ for c in classes)}")
    for cls in classes:
        for m in sorted(n for n in dir(cls) if n.startswith("test_")):
            label = f"{cls.__name__}.{m}"
            inst = cls()
            inst.setup_method()
            try:
                getattr(inst, m)()
                passed += 1
                print(f"✓ {label}")
            except AssertionError as exc:
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label}")
            except Exception as exc:  # noqa: BLE001
                failed += 1
                failures.append((label, exc))
                print(f"✗ {label} (error)")
            finally:
                inst.teardown_method()
    print("\n" + "=" * 60)
    print(f"Help token and joined flag tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for label, exc in failures:
        print(f"\n✗ {label}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if _run_all() else 1)
