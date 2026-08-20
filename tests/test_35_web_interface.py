#!/usr/bin/env python3
"""
Test 35: rmp web - read-only embedded web interface (SPEC/WEB.md).

This suite drives the compiled binary's `rmp web` command end-to-end and
exercises every acceptance criterion AC1-AC24 of SPEC/WEB.md against the
running HTTP server:

- Process/CLI contract: flag validation and exit codes (AC1-AC5), the
  machine-readable {"url": ...} startup object, and graceful SIGINT/SIGTERM
  shutdown with exit 0 (AC17).
- Routes and pages: index with discovery + empty state, the read-only
  sprints landing page (GET /roadmaps/{name}: three sprint tabs, Actual
  active by default) and the separate tasks page (GET /roadmaps/{name}/tasks:
  the Kanban board of five fixed columns, narrowed by the header search input
  and the type, minimum-priority and minimum-severity filter dropdowns, which
  compose conjunctively and travel in the q, type, priority and severity query
  parameters) — both with no edit affordance and no audit-log
  growth, name validation / path-traversal guard, the knowledge-graph page
  and its JSON data endpoint, and the read-only proof that graph reads create
  no snapshot/ directory. Choosing a roadmap on the index lands the user on
  the sprints page with the current (OPEN) sprint selected by default.
- Read-only enforcement: non-read HTTP methods answered 405 (AC14).
- Self-contained delivery: static assets served only from /static/, a
  missing asset 404s (AC15), the vendored D3.js bundle and the d3-sankey
  plugin are served locally, no page references any remote origin
  (AC16, AC18, AC19).
- Mobile-first: every page carries the responsive viewport meta tag and
  loads no remote CSS; the stylesheet uses min-width media queries (AC20-AC22).
- Tabler admin-shell: every page renders in the dark theme
  (data-bs-theme="dark"), with a vertical sidebar, page wrapper/header, a
  top navbar naming the selected roadmap (AC108), and the off-canvas
  hamburger markup; the vendored Tabler CSS/JS and the Inter / Tabler Icons
  web fonts are served locally from /static/ (AC23/AC24, AC16/AC22).

The server is long-lived, so each scenario launches a fresh `rmp web`
process on an ephemeral port (--port 0), parses the startup URL from
stdout, drives it over raw http.client requests (no client-side path
normalisation, so the traversal guard is genuinely exercised), and then
terminates it. Roadmap data and a populated knowledge graph are built
through the real CLI so the pages render production-shaped content.
"""

import html as html_lib
import http.client
import json
import os
import re
import shutil
import signal
import sqlite3
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
from datetime import datetime, timedelta, timezone
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase, commit_flags_for

ROADMAP = "platform"

# The port `rmp web` binds when --port is not given (SPEC/WEB.md § Bind Address
# and Port Selection, item 2). The ephemeral fallback under test is reachable
# ONLY by omitting --port: any --port at all, including this very number passed
# explicitly, marks the port explicit and turns a bind failure into a fatal
# error instead (internal/web/server.go bindListener; that other half of the
# contract is test_explicit_busy_port_exits_1).
DEFAULT_WEB_PORT = 8787

# Unicode's White_Space property: the set SPEC/WEB.md Acceptance Criterion 121
# names as the board search's trim, and the set the server ships to the browser as
# SPACE_TABLE.
#
# It is written out here rather than taken from Python's str.strip(), whose set is
# Python's own — it also holds U+001C to U+001F — and rather than from the served
# asset, so that this module states the expectation INDEPENDENTLY. That it still
# equals what the binary ships is asserted, not assumed:
# test_tasks_page_search_trims_the_term_with_the_shipped_whitespace compares the
# two, so a drift on either side is named rather than absorbed.
#
# The two code points the JavaScript platform's own trimming disagrees about are
# the point of the whole rule: U+0085 is HERE and that platform keeps it, U+FEFF is
# NOT here and that platform removes it.
WHITE_SPACE = "".join(chr(cp) for cp in (
    0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020, 0x0085, 0x00A0, 0x1680,
    *range(0x2000, 0x200B), 0x2028, 0x2029, 0x202F, 0x205F, 0x3000,
))


class TestWebInterface:
    """End-to-end coverage of `rmp web` (SPEC/WEB.md AC1-AC22)."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.cli = self.test.cli_path
        self.home = str(self.test.home_dir)
        self._procs = []
        self._socks = []
        self._extra_homes = []
        self._populate()

    def teardown_method(self):
        for proc in self._procs:
            self._kill(proc)
        for sock in self._socks:
            try:
                sock.close()
            except OSError:
                pass
        for home in self._extra_homes:
            shutil.rmtree(home, ignore_errors=True)
        self.test.teardown()

    # ---- environment helpers -------------------------------------------

    def _env(self, home=None):
        env = os.environ.copy()
        env["HOME"] = home or self.home
        return env

    def _run(self, args, home=None, check=True):
        """Run a short-lived rmp command and return (code, stdout, stderr)."""
        result = subprocess.run(
            [self.cli] + args,
            capture_output=True,
            text=True,
            env=self._env(home),
        )
        if check and result.returncode != 0:
            raise AssertionError(
                f"command failed: rmp {' '.join(args)}\n"
                f"exit={result.returncode}\nstdout={result.stdout}\nstderr={result.stderr}"
            )
        return result.returncode, result.stdout, result.stderr

    def _populate(self):
        """Build a realistic roadmap with tasks, three sprints (one per status)
        and a knowledge graph.

        Sprint lifecycle is create -> start (PENDING->OPEN) -> close
        (OPEN->CLOSED), and at most one sprint may be OPEN at a time, so the
        CLOSED sprint is built (started then closed) before the OPEN one is
        started. The result is one PENDING, one OPEN, and one CLOSED sprint, so
        the sprints page's three tabs (Próximos / Actual / Concluídos) each have
        content (SPEC/WEB.md § Roadmap Sprints Page). The PENDING-sprint task
        and the BACKLOG/never-sprinted tasks appear only on the tasks page
        (SPEC/WEB.md § Roadmap Tasks Page), not on the sprints page.
        """
        self._run(["roadmap", "create", ROADMAP])
        t1 = self.test.create_task(
            ROADMAP,
            "Implement passwordless login",
            "End users must authenticate without a stored password",
            "Add a magic-link issuer and a one-time-token verifier",
            "A user receives a link and reaches an authenticated session",
            priority=8,
        )
        t2 = self.test.create_task(
            ROADMAP,
            "Rate-limit the token endpoint",
            "Brute-force attempts against tokens must be throttled",
            "Add a sliding-window limiter keyed by client address",
            "Excessive requests receive HTTP 429 within the window",
            priority=6,
        )
        # A dependency edge so the detail page has a relationship to render.
        self._run(["task", "add-dep", "-r", ROADMAP, str(t1), str(t2)], check=False)

        # CLOSED sprint: started, then force-closed (it carries active tasks).
        t_closed = self.test.create_task(
            ROADMAP,
            "Audit the session-cookie flags",
            "Session cookies must be Secure, HttpOnly and SameSite",
            "Set the cookie attributes in the session middleware",
            "Cookies inspected in the browser carry all three flags",
            priority=5,
        )
        closed_sid = self.test.create_sprint(ROADMAP, "Session cookie hardening sprint")
        self._run(["sprint", "add-tasks", "-r", ROADMAP, str(closed_sid), str(t_closed)])
        self._run(["sprint", "start", "-r", ROADMAP, str(closed_sid)])
        self._run(["sprint", "close", "-r", ROADMAP, str(closed_sid), "--force"])

        # OPEN sprint: the current/Actual sprint, started with two tasks.
        open_sid = self.test.create_sprint(ROADMAP, "Authentication hardening sprint")
        self._run(["sprint", "add-tasks", "-r", ROADMAP, str(open_sid), str(t1), str(t2)])
        self._run(["sprint", "start", "-r", ROADMAP, str(open_sid)])

        # PENDING sprint: planned, not started, under Próximos.
        t_pending = self.test.create_task(
            ROADMAP,
            "Add WebAuthn passkey support",
            "Users should be able to register a hardware passkey",
            "Integrate a FIDO2 server library and a registration ceremony",
            "A registered passkey authenticates without a magic link",
            priority=4,
        )
        pending_sid = self.test.create_sprint(ROADMAP, "Passkey enrolment sprint")
        self._run(["sprint", "add-tasks", "-r", ROADMAP, str(pending_sid), str(t_pending)])

        self.task_ids = (t1, t2)
        self.sprint_id = open_sid
        self.open_sid = open_sid
        self.pending_sid = pending_sid
        self.closed_sid = closed_sid
        self.open_task_ids = (t1, t2)
        self.pending_task_id = t_pending
        self.closed_task_id = t_closed

        # A small knowledge graph: two nodes and one relationship.
        self._run(["graph", "create", "-r", ROADMAP,
                   "--query", "CREATE (s:Spec {key:'passwordless-auth'})"])
        self._run(["graph", "create", "-r", ROADMAP,
                   "--query", "CREATE (c:Code {path:'internal/auth/magiclink.go'})"])
        self._run(["graph", "create", "-r", ROADMAP,
                   "--query",
                   "MATCH (s:Spec {key:'passwordless-auth'}), "
                   "(c:Code {path:'internal/auth/magiclink.go'}) "
                   "CREATE (s)-[:IMPLEMENTED_BY]->(c)"])

    def _fresh_home(self):
        """A separate empty HOME (no roadmaps) for empty-state tests."""
        home = tempfile.mkdtemp(prefix="groadmap_web_home_")
        self._extra_homes.append(home)
        return home

    # ---- server lifecycle helpers --------------------------------------

    def _start(self, extra_args=None, home=None, expect_ok=True):
        """Launch `rmp web` and return (proc, port). On expect_ok, parse the URL.

        stdout/stderr go to temporary files (not pipes): the server is
        long-lived and never closes its stdout, so a pipe + readline would
        deadlock. Polling a file for the pretty-printed {"url": ...} object
        is deterministic and EOF-independent.

        The default bind host is 127.0.0.1 (loopback), reachable only from the
        local machine (SPEC/WEB.md § Bind Address and Port Selection). These
        route/lifecycle scenarios only need a reachable server, so unless the
        caller already pins --host we pin the loopback default explicitly; this
        also avoids the network-exposure warning that a non-loopback bind would
        print. The default-host behaviour itself is asserted separately, on the
        printed URL, in test_default_host_is_loopback, and the warning in
        test_network_exposure_warns_on_stderr.
        """
        extra_args = list(extra_args or [])
        if not any(a == "--host" or a.startswith("--host=") for a in extra_args):
            extra_args = ["--host", "127.0.0.1"] + extra_args
        args = [self.cli, "web", "--no-open"] + extra_args
        out = tempfile.TemporaryFile(mode="w+")
        err = tempfile.TemporaryFile(mode="w+")
        proc = subprocess.Popen(
            args, stdout=out, stderr=err, text=True, env=self._env(home),
        )
        proc.out_file = out
        proc.err_file = err
        self._procs.append(proc)
        if not expect_ok:
            return proc, None
        url = self._read_startup_url(proc)
        assert url is not None, (
            "server did not print a startup URL; "
            f"exit={proc.poll()} stderr={self._drain(err)}"
        )
        assert url.startswith("http://"), f"unexpected url scheme: {url!r}"
        port = int(url.rsplit(":", 1)[1])
        self._wait_accepting(port)
        return proc, port

    @staticmethod
    def _read_startup_url(proc, timeout=10.0):
        """Poll the server's stdout file for the {"url": ...} startup object."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            proc.out_file.seek(0)
            content = proc.out_file.read()
            if content:
                try:
                    obj = json.loads(content)
                    if isinstance(obj, dict) and "url" in obj:
                        return obj["url"]
                except json.JSONDecodeError:
                    pass
            if proc.poll() is not None:
                return None  # exited without a parseable URL
            time.sleep(0.05)
        return None

    @staticmethod
    def _wait_accepting(port, host="127.0.0.1", timeout=5.0):
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                with socket.create_connection((host, port), timeout=0.5):
                    return
            except OSError:
                time.sleep(0.05)
        raise AssertionError(f"server on {host}:{port} never accepted connections")

    @staticmethod
    def _drain(stream):
        try:
            stream.seek(0)
            return stream.read()
        except Exception:  # noqa: BLE001
            return ""

    def _kill(self, proc):
        if proc.poll() is None:
            try:
                # Prefer a graceful SIGTERM: the server supports clean
                # SIGINT/SIGTERM shutdown (exit 0). A clean exit also lets a
                # coverage-instrumented binary flush its GOCOVERDIR data, which
                # an immediate SIGKILL would discard. Fall back to SIGKILL only
                # if the server fails to stop within the grace window.
                proc.send_signal(signal.SIGTERM)
                proc.wait(timeout=8)
            except subprocess.TimeoutExpired:
                try:
                    proc.send_signal(signal.SIGKILL)
                    proc.wait(timeout=5)
                except Exception:  # noqa: BLE001
                    pass
            except Exception:  # noqa: BLE001
                pass
        for attr in ("out_file", "err_file"):
            f = getattr(proc, attr, None)
            if f is not None:
                try:
                    f.close()
                except Exception:  # noqa: BLE001
                    pass

    def _stop(self, proc, sig):
        """Signal a running server and return its exit code (or None on timeout)."""
        proc.send_signal(sig)
        try:
            return proc.wait(timeout=8)
        except subprocess.TimeoutExpired:
            return None

    def _occupy(self, port=0, host="127.0.0.1"):
        """Bind and listen on a port so the next bind to it fails. Returns the port."""
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 0)
        sock.bind((host, port))
        sock.listen(1)
        self._socks.append(sock)
        return sock.getsockname()[1]

    # ---- HTTP helper (raw path, no client normalisation) ---------------

    @staticmethod
    def _req(port, path, method="GET", host="127.0.0.1", timeout=5):
        """GET (or METHOD) a raw path and return (status, headers, body).

        timeout is the client-side socket timeout in seconds. Five seconds is
        far more than any ordinary page or data response needs, so it stays the
        default. The graph query time budget scenario raises it deliberately:
        the response it waits for is the one the server's own 5-second budget
        produces, and a 5-second client would race the very deadline under test.
        """
        conn = http.client.HTTPConnection(host, port, timeout=timeout)
        try:
            conn.request(method, path)
            resp = conn.getresponse()
            body = resp.read().decode("utf-8", "replace")
            headers = {k.lower(): v for k, v in resp.getheaders()}
            return resp.status, headers, body
        finally:
            conn.close()

    @staticmethod
    def _asset_refs(html):
        """Return every <script src> / <link href> / <img src> target in the HTML."""
        refs = []
        refs += re.findall(r'<script[^>]*\bsrc=["\']([^"\']+)["\']', html, re.I)
        refs += re.findall(r'<link[^>]*\bhref=["\']([^"\']+)["\']', html, re.I)
        refs += re.findall(r'<img[^>]*\bsrc=["\']([^"\']+)["\']', html, re.I)
        return refs

    # ====================================================================
    # AC1-AC5, scaffold: CLI contract, flag validation, exit codes
    # ====================================================================

    def test_help_exits_zero_and_documents_command(self):
        code, out, _ = self._run(["web", "-h"])
        assert code == 0, f"web -h must exit 0, got {code}"
        for needle in ("rmp web", "--host", "--port", "--no-open"):
            assert needle in out, f"web help missing {needle!r}"
        # The help must make explicit that web takes no -r/--roadmap.
        assert "-r" in out or "roadmap" in out.lower()

    def test_port_out_of_range_exits_6(self):
        code, _, err = self._run(["web", "--port", "70000"], check=False)
        assert code == 6, f"--port 70000 must exit 6, got {code}; stderr={err}"

    def test_port_non_integer_exits_6(self):
        code, _, err = self._run(["web", "--port", "notanumber"], check=False)
        assert code == 6, f"--port notanumber must exit 6, got {code}; stderr={err}"

    def test_unknown_flag_exits_2(self):
        code, _, err = self._run(["web", "--definitely-not-a-flag"], check=False)
        assert code == 2, f"unknown flag must exit 2, got {code}; stderr={err}"

    def test_unexpected_positional_exits_2(self):
        code, _, err = self._run(["web", "stray-argument"], check=False)
        assert code == 2, f"unexpected positional must exit 2, got {code}; stderr={err}"

    def test_web_listed_in_ai_help_without_roadmap_flag(self):
        code, out, _ = self._run(["--ai-help"])
        assert code == 0
        contract = json.loads(out)
        names = {c.get("name") for c in contract.get("commands", [])}
        assert "web" in names, f"--ai-help must list web; got {sorted(names)}"
        web = next(c for c in contract["commands"] if c["name"] == "web")
        # web is the one command exempt from the always-required-roadmap rule:
        # it must not DECLARE -r/--roadmap (a textual mention in the description,
        # explaining that it does not take one, is expected and allowed).
        subs = web.get("subcommands") or [web]
        declared = {
            f.get("long") for s in subs for f in (s.get("flags") or [])
        } | {
            f.get("short") for s in subs for f in (s.get("flags") or [])
        }
        assert "--roadmap" not in declared and "-r" not in declared, (
            f"web must not declare the roadmap flag; declared={sorted(x for x in declared if x)}"
        )

    # ====================================================================
    # AC1/AC2: startup URL object; loopback default with network opt-in
    # ====================================================================

    def test_startup_prints_url_object_and_serves(self):
        # _start pins --host 127.0.0.1 (the loopback default) explicitly; the
        # default-host value is asserted on the printed URL in
        # test_default_host_is_loopback.
        proc, port = self._start(["--port", "0"])
        # AC1: the URL reflects the actual bind.
        status, _, _ = self._req(port, "/")
        assert status == 200
        # AC2: server is up without a browser (we passed --no-open); URL printed.
        assert port > 0

    def test_default_host_is_loopback(self):
        """AC1: with no --host the printed URL host is 127.0.0.1 (loopback).

        The default bind host is loopback, so the read-only interface is
        reachable only from the local machine; exposing it on the network is
        the explicit --host 0.0.0.0 opt-in (SPEC/WEB.md § Bind Address and Port
        Selection, item 1). We start with the default host but an explicit
        ephemeral --port 0 (so the test does not race the real 8787), read the
        printed startup URL, assert its host component, and confirm no
        network-exposure warning is printed on stderr for a loopback bind.
        """
        # Launch directly with no --host so the process resolves the real
        # default. Reuse the same stdout-file polling.
        out = tempfile.TemporaryFile(mode="w+")
        err = tempfile.TemporaryFile(mode="w+")
        proc = subprocess.Popen(
            [self.cli, "web", "--no-open", "--port", "0"],
            stdout=out, stderr=err, text=True, env=self._env(),
        )
        proc.out_file = out
        proc.err_file = err
        self._procs.append(proc)
        url = self._read_startup_url(proc)
        assert url is not None, (
            "server did not print a startup URL; "
            f"exit={proc.poll()} stderr={self._drain(err)}"
        )
        # url is http://<host>:<port>; the host is the default bind host.
        host = url[len("http://"):].rsplit(":", 1)[0]
        assert host == "127.0.0.1", (
            f"default bind host must be 127.0.0.1 (loopback); got {host!r} "
            f"from url {url!r}"
        )
        # A loopback bind prints no network-exposure warning on stderr.
        stderr = self._drain(err)
        assert "reachable from the network" not in stderr, (
            f"loopback default must NOT print a network-exposure warning; "
            f"stderr={stderr!r}"
        )
        code = self._stop(proc, signal.SIGTERM)
        assert code == 0, f"graceful SIGTERM shutdown must exit 0, got {code}"

    def test_network_exposure_warns_on_stderr(self):
        """AC: binding a non-loopback host prints a network-exposure warning to
        stderr while the startup URL object still goes to stdout (SPEC/WEB.md
        § Bind Address and Port Selection, item 3).

        We pass the explicit --host 0.0.0.0 opt-in with an ephemeral --port 0,
        so the all-interfaces listener exists only for the brief window before
        teardown signals the process.
        """
        out = tempfile.TemporaryFile(mode="w+")
        err = tempfile.TemporaryFile(mode="w+")
        proc = subprocess.Popen(
            [self.cli, "web", "--no-open", "--host", "0.0.0.0", "--port", "0"],
            stdout=out, stderr=err, text=True, env=self._env(),
        )
        proc.out_file = out
        proc.err_file = err
        self._procs.append(proc)
        url = self._read_startup_url(proc)
        assert url is not None, (
            "server did not print a startup URL; "
            f"exit={proc.poll()} stderr={self._drain(err)}"
        )
        # The startup URL object still goes to stdout, with the requested host.
        host = url[len("http://"):].rsplit(":", 1)[0]
        assert host == "0.0.0.0", (
            f"explicit --host 0.0.0.0 must be reflected in the URL; got {host!r}"
        )
        # The warning goes to stderr, naming the bound host.
        stderr = self._drain(err)
        assert "reachable from the network" in stderr, (
            f"non-loopback bind must print a network-exposure warning to stderr; "
            f"stderr={stderr!r}"
        )
        assert "0.0.0.0" in stderr, (
            f"warning must name the bound host; stderr={stderr!r}"
        )
        # Stop promptly so the all-interfaces listener is not left bound.
        code = self._stop(proc, signal.SIGTERM)
        assert code == 0, f"graceful SIGTERM shutdown must exit 0, got {code}"

    # ====================================================================
    # AC3/AC4: explicit-port bind failure vs default-port fallback
    # ====================================================================

    def test_explicit_busy_port_exits_1(self):
        busy = self._occupy()  # ephemeral, then demand it explicitly
        proc, _ = self._start(["--port", str(busy)], expect_ok=False)
        code = proc.wait(timeout=8)
        err = self._drain(proc.err_file)
        assert code == 1, f"explicit busy --port must exit 1, got {code}; stderr={err}"
        assert "bind" in err.lower(), f"bind error must name the failure; stderr={err}"

    def test_default_port_busy_falls_back_to_ephemeral(self):
        """AC4: when the DEFAULT port is busy and --port was not given, the server
        falls back to an OS-chosen ephemeral port and still starts, instead of
        failing the way an explicit busy --port does (SPEC/WEB.md § Bind Address
        and Port Selection, item 4).

        The fallback is reachable only by omitting --port, so the scenario needs
        DEFAULT_WEB_PORT to be busy — and whether it already is, is not this
        suite's to decide. The test therefore does not probe the port and hope
        the answer still holds a moment later. It starts one server with no
        --port and READS BACK the port that server actually took; that single
        observation both establishes which precondition is in force and is
        itself the first assertion.

          - The first server took the default port. Nothing else on this machine
            wanted it, so the contention is supplied by this fixture: a second
            server, also with no --port, must fall back off the port the first
            one is now provably holding.
          - The first server did NOT take the default port. Something outside
            this suite holds it, the contention already existed, and this very
            server is the one that fell back off it.

        Neither branch can pass for the other's reason, because each asserts
        something the other cannot reach: the fixture-held branch proves the
        held port is still served afterwards (the fallback took a different
        port rather than stealing the busy one), and the foreign-held branch
        proves a busy default is survivable rather than fatal — no bind error on
        stderr and a live process — which is exactly what separates it from the
        explicit-port contract. Every message names the branch that ran, so the
        output records which precondition was in force.

        This replaces a form that bound the default port itself and RETURNED
        without asserting when that bind failed, so on precisely the machines
        where the port was busy — the condition the test exists to exercise — it
        passed having executed no assertion at all. Nothing here can return
        without asserting.
        """
        first_proc, first_port = self._start()  # no --port: the default path

        # The port that server took is the whole of the branch decision, and it
        # is recorded before anything can fail, so the run's output names the
        # precondition that was in force whatever the outcome.
        fixture_held = first_port == DEFAULT_WEB_PORT
        branch = (
            f"fixture-held (nothing else on this machine wanted port "
            f"{DEFAULT_WEB_PORT}, so this test's own first server holds it)"
            if fixture_held else
            f"foreign-held (a process outside this suite holds port "
            f"{DEFAULT_WEB_PORT}, so the first server fell back off it)"
        )
        print(f"  (default-port contention {branch})")

        if fixture_held:
            # The first server is already accepting connections on the default
            # port (_start waits for that), so the second server's bind to it
            # cannot succeed. The contention is established, not assumed.
            fell_back_proc, fell_back_port = self._start()

            # Branch-specific: the fallback took a DIFFERENT port and left the
            # busy one alone. A server that had stolen or disturbed the held
            # port would still answer on its own port and pass every shared
            # check below.
            status, _, _ = self._req(first_port, "/")
            assert status == 200, (
                f"{branch}: the server holding port {DEFAULT_WEB_PORT} must "
                f"still serve after the second one fell back off it; got "
                f"{status}"
            )
        else:
            fell_back_proc, fell_back_port = first_proc, first_port

            # Branch-specific: a busy DEFAULT port is survivable, not fatal.
            # The explicit-port contract is the opposite — exit 1 with a bind
            # error named on stderr (test_explicit_busy_port_exits_1) — so the
            # absence of both is the whole difference between the two rules.
            assert fell_back_proc.poll() is None, (
                f"{branch}: a busy default port must not be fatal; the process "
                f"exited {fell_back_proc.poll()}"
            )
            stderr = self._drain(fell_back_proc.err_file)
            assert "bind" not in stderr.lower(), (
                f"{branch}: falling back is silent, not a reported bind "
                f"failure; stderr={stderr!r}"
            )

        # Shared contract, asserted for whichever server did the falling back.
        # "Fell back" means it bound a DIFFERENT port, never merely that it
        # started.
        assert fell_back_port != DEFAULT_WEB_PORT, (
            f"{branch}: the fallback must bind a port other than the busy "
            f"default; it bound {fell_back_port}"
        )
        assert fell_back_port > 0, (
            f"{branch}: the fallback must report the real OS-assigned port; "
            f"got {fell_back_port}"
        )
        status, _, _ = self._req(fell_back_port, "/")
        assert status == 200, (
            f"{branch}: the server must serve on its fallback port "
            f"{fell_back_port}; got {status}"
        )

    # ====================================================================
    # AC6/AC7: roadmap index + empty state
    # ====================================================================

    def test_index_lists_roadmaps_with_links(self):
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, "/")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")
        assert ROADMAP in body, "index must list the roadmap name"
        assert f"/roadmaps/{ROADMAP}" in body, "index must link the sprints landing page"
        assert f"/roadmaps/{ROADMAP}/graph" in body, "index must link the graph page"

    def test_choosing_roadmap_lands_on_sprints_page(self):
        """Selecting a roadmap on the index lands on the sprints page
        (GET /roadmaps/{name}) with the current (OPEN) sprint selected by
        default — the Actual tab is the active tab (SPEC/WEB.md § Roadmap Index
        Page and § Roadmap Sprints Page)."""
        proc, port = self._start(["--port", "0"])
        # The index card's primary link for the roadmap is the sprints landing
        # page (href="/roadmaps/{name}" exactly, not the tasks or graph URL).
        _, _, index = self._req(port, "/")
        assert f'href="/roadmaps/{ROADMAP}"' in index, (
            "index must link the roadmap to its sprints landing page"
        )
        # Following that link lands on the sprints page with Actual active.
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")
        assert re.search(
            r'href="#tab-current"[^>]*\bclass="nav-link active"[^>]*aria-selected="true">Actual',
            body,
        ), "landing must select the current (Actual/OPEN) sprint tab by default"

    def test_index_empty_state_when_no_roadmaps(self):
        proc, port = self._start(["--port", "0"], home=self._fresh_home())
        status, _, body = self._req(port, "/")
        assert status == 200, "empty index must still be 200 (absence is not an error)"
        assert "roadmap create" in body.lower() or "no roadmap" in body.lower(), (
            "empty index must guide the user to create a roadmap via the CLI"
        )

    # ====================================================================
    # Sprints page and tasks page are read-only and write no audit entry
    # ====================================================================

    def test_sprints_page_shows_sprint_cards_not_full_task_table(self):
        """The sprints landing page renders every sprint as a compact shared
        sprint card (header, description, task-count footer) but NOT the full
        task table and NOT any inline member-task list or modal — those live on
        the tasks page and the single sprint page (SPEC/WEB.md § Shared
        Sprint-Card Partial; Acceptance Criteria 8/38)."""
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")
        assert "authentication hardening sprint" in body.lower(), "must show sprint cards"
        # The shared sprint-card markup renders each sprint as a card link.
        assert 'class="card card-sm card-link text-reset"' in body, (
            "the sprints page must render sprints through the shared sprint-card partial"
        )
        # The OPEN sprint is shown as a card, NOT expanded: its member-task title
        # and any per-task modal trigger must be absent from the sprints page.
        assert "passwordless login" not in body.lower(), (
            "the OPEN sprint must not be expanded into an inline task list on the sprints page"
        )
        assert "data-bs-target=\"#task-modal-" not in body, (
            "the sprints page must render no per-task modal trigger"
        )
        # The sprints page renders no task presentation of its own: neither a
        # task table nor the tasks page's Kanban board.
        assert "<table" not in body, (
            "the sprints page must render no task table"
        )
        assert 'data-role="task-board"' not in body, (
            "the Kanban board belongs to the tasks page, not the sprints page"
        )
        # The PENDING-sprint task is not surfaced on the sprints page either.
        assert "add webauthn passkey support" not in body.lower(), (
            "member-task titles must not appear on the sprints page"
        )
        # No edit affordance: no form and no write-method submission.
        assert "<form" not in body.lower(), "sprints page must contain no form"
        assert not re.search(r'method=["\']?(post|put|patch|delete)', body, re.I), (
            "sprints page must not submit any change"
        )

    # The five fixed board columns, in the order of the task state machine's
    # flow (SPEC/WEB.md § Roadmap Tasks Page, Acceptance Criterion 81).
    BOARD_COLUMNS = ("BACKLOG", "SPRINT", "DOING", "TESTING", "COMPLETED")

    @staticmethod
    def _board_columns(body):
        """Return the markup of each board column, in the order rendered.

        The board lives inside <main class="page-body">; the task detail modals
        are rendered after it, outside the page wrapper, so slicing main keeps a
        modal's copy of a task's title from being mistaken for a card.
        """
        main = re.search(r'<main class="page-body">(.*?)</main>', body, re.S)
        assert main, 'the tasks page has no <main class="page-body"> region'
        region = main.group(1)
        assert 'data-role="task-board"' in region, "the tasks page renders no Kanban board"
        return region, region.split('data-role="task-board-column"')[1:]

    @staticmethod
    def _shows_empty_state(column):
        """Whether a column is SHOWING its in-column empty state.

        The element is always in the document — the server and the browser both
        express the state by toggling `hidden`, so a column emptied by a search
        reads exactly like a column the roadmap left empty — so presence alone
        says nothing.
        """
        m = re.search(r'<div class="empty" data-role="task-board-column-empty"([^>]*)>', column)
        assert m, "a board column carries no in-column empty state"
        return "hidden" not in m.group(1)

    # The authoritative task-status colour mapping, written out from
    # SPEC/WEB.md § Status, Priority, and Severity Badge Colours. Every
    # per-column count badge of the two boards is coloured through it
    # (Acceptance Criteria 61 and 140).
    TASK_STATUS_BADGE = {
        "BACKLOG": "bg-secondary-lt",
        "SPRINT": "bg-cyan-lt",
        "DOING": "bg-blue-lt",
        "TESTING": "bg-yellow-lt",
        "COMPLETED": "bg-green-lt",
    }

    # The canonical status of each column of the sprint's member-tasks board,
    # in board order. A column there groups a SET of statuses and writes none of
    # them, so its count badge takes the colour of the status a task is normally
    # in at that stage of the sprint: SPRINT for WAITING (a BACKLOG task inside a
    # sprint is the exceptional case), DOING for DOING, and COMPLETED for CLOSED,
    # which holds that status alone (SPEC/WEB.md § Sprint Detail Sub-Template,
    # rule 4, Column header; Acceptance Criterion 140).
    SPRINT_BOARD_CANONICAL = {
        "WAITING": "SPRINT",
        "DOING": "DOING",
        "CLOSED": "COMPLETED",
    }

    @staticmethod
    def _column_badge(column):
        """Return the (heading, badge colour variant, count) a column header shows.

        The variant is CAPTURED and not pinned here, because it is no longer one
        value across the columns of a board: each column's badge carries the
        semantic colour of the status it groups (Acceptance Criterion 140). A
        pattern that still demanded bg-secondary-lt would match the BACKLOG
        column alone and fail on every other one.
        """
        m = re.search(
            r'<h3 class="card-title">([A-Z]+) '
            r'<span class="badge (bg-[a-z]+-lt) ms-2">(\d+)</span></h3>',
            column,
        )
        assert m, "a board column has no Tabler card-title header with a count badge"
        return m.group(1), m.group(2), int(m.group(3))

    @classmethod
    def _column_header(cls, column):
        """Return the (status, count) a column header shows."""
        heading, _, count = cls._column_badge(column)
        return heading, count

    def test_tasks_page_renders_the_kanban_board_read_only(self):
        """The tasks page renders every task of the roadmap as a Kanban board of
        five fixed columns, each card opening the read-only task detail modal.

        It renders no task table, no sprint tabs, and no control that writes
        (SPEC/WEB.md § Roadmap Tasks Page, Acceptance Criteria 81 to 92)."""
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}/tasks")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")

        region, columns = self._board_columns(body)

        # Five columns, in the state machine's order, whatever the data holds.
        assert len(columns) == 5, f"the board renders {len(columns)} columns, want 5"
        for column, want in zip(columns, self.BOARD_COLUMNS):
            got, _ = self._column_header(column)
            assert got == want, f"column titled {got!r}, want {want!r}"

        # Every task the CLI reports is on the board exactly once, in the column
        # of its own status: the web view and the CLI agree on where the work
        # stands. The counts sum to the roadmap's task count.
        tasks = json.loads(self._run(["task", "list", "-r", ROADMAP])[1])
        assert tasks, "the fixture roadmap has no task to place on the board"
        by_column = dict(zip(self.BOARD_COLUMNS, columns))
        for task in tasks:
            marker = f'data-task-id="{task["id"]}"'
            assert region.count(marker) == 1, (
                f"task #{task['id']} has {region.count(marker)} cards on the board, want 1"
            )
            assert marker in by_column[task["status"]], (
                f"task #{task['id']} is not in the {task['status']} column, which is its status"
            )
        assert sum(self._column_header(c)[1] for c in columns) == len(tasks), (
            "the column counts do not sum to the roadmap's task count"
        )

        # Every task, any status — including the PENDING-sprint task that the
        # sprints page does not show.
        low = body.lower()
        for title in (
            "implement passwordless login",
            "rate-limit the token endpoint",
            "add webauthn passkey support",
            "audit the session-cookie flags",
        ):
            assert title in low, f"tasks page must show task {title!r} on the board"

        # A card opens the read-only modal of its own task.
        t1 = self.open_task_ids[0]
        assert f'data-task-id="{t1}"' in region, "board card missing the modal trigger"
        assert body.count('id="task-modal"') == 1, "the page must carry exactly one modal shell"
        assert '<button type="button" class="card card-sm task-card' in region, (
            "a board card must be a real button, so the keyboard can activate it"
        )

        # The card of a task in a sprint names that sprint, by title and id.
        assert f"Authentication hardening sprint (Sprint #{self.open_sid})" in region, (
            "the card of a sprinted task must name its sprint"
        )

        # The board replaced the table: no table, and no sprint tabs.
        assert "<table" not in region, "the board is the page's only task presentation"
        assert "<table" not in body, "the tasks page must render no task table anywhere on the page"
        assert 'id="tab-current"' not in body, "tasks page must not render the sprint tabs"

        # Read-only: no form, no submit, no drag-and-drop. The page carries exactly
        # one input — the header search box, which submits nothing and only changes
        # which of the already-read tasks are shown (SPEC/WEB.md § Roadmap Tasks
        # Page, Read-only).
        assert "<form" not in low, "tasks page must contain no form"
        assert 'type="submit"' not in low, "tasks page must contain no submit"
        assert low.count("<input") == 1, (
            f"tasks page carries {low.count('<input')} inputs, want exactly the search box"
        )
        assert "<input" not in region.lower(), "the board itself must carry no input"
        assert "draggable" not in region.lower(), "the board must offer no drag-and-drop"

    def test_tasks_page_board_places_each_status_in_its_own_column(self):
        """Tasks written through the CLI land in the board column of the status
        the CLI reports, one column per status.

        The shared fixture keeps every task in one status, so this test builds a
        roadmap of its own whose tasks sit in three DIFFERENT columns: a board
        that grouped by anything other than the task's status, or that dropped
        the grouping altogether, could not place all three correctly
        (SPEC/WEB.md § Roadmap Tasks Page, Acceptance Criterion 82)."""
        roadmap = "board_placement_demo"
        self._run(["roadmap", "create", roadmap])
        backlog = self.test.create_task(
            roadmap,
            "Publish the settlement reconciliation runbook",
            "Operators need a written procedure for a failed settlement window",
            "Write the runbook and link it from the on-call handbook",
            "An operator can follow the runbook without asking the team",
            priority=3,
        )
        sprinted = self.test.create_task(
            roadmap,
            "Alert on an unbalanced settlement window",
            "An unbalanced window must page the on-call engineer",
            "Emit a metric per window and alert on a non-zero residual",
            "A seeded imbalance pages within five minutes",
            priority=7,
        )
        doing = self.test.create_task(
            roadmap,
            "Reconcile the ledger against the acquirer report",
            "The ledger and the acquirer report must agree to the cent",
            "Match both sides by window and report the residual",
            "A day's windows reconcile with a zero residual",
            priority=9,
        )
        sprint_id = self.test.create_sprint(roadmap, "Settlement reconciliation sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(sprinted), str(doing)])
        self._run(["sprint", "start", "-r", roadmap, str(sprint_id)])
        self._run(["task", "stat", "-r", roadmap, str(doing), "DOING", "--commit-open", "24262f0"])

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/tasks")
        region, columns = self._board_columns(body)
        by_column = dict(zip(self.BOARD_COLUMNS, columns))

        for task_id, want in ((backlog, "BACKLOG"), (sprinted, "SPRINT"), (doing, "DOING")):
            marker = f'data-task-id="{task_id}"'
            assert region.count(marker) == 1, (
                f"task #{task_id} has {region.count(marker)} cards on the board, want 1"
            )
            assert marker in by_column[want], f"task #{task_id} is not in the {want} column"

        # The counts follow the placement, and the two untouched columns are empty.
        for status, want in (("BACKLOG", 1), ("SPRINT", 1), ("DOING", 1),
                             ("TESTING", 0), ("COMPLETED", 0)):
            _, count = self._column_header(by_column[status])
            assert count == want, f"column {status} shows the count {count}, want {want}"

        # The sprinted tasks name their sprint; the BACKLOG task names none.
        assert f"Settlement reconciliation sprint (Sprint #{sprint_id})" in region, (
            "a card of a sprinted task must name its sprint"
        )
        backlog_card = by_column["BACKLOG"]
        assert "Sprint #" not in backlog_card, (
            "the card of a task in no sprint must name no sprint"
        )

    def test_tasks_page_board_empty_states(self):
        """An empty column, and a roadmap with no task at all, each render the
        in-column empty state while keeping all five columns and their 0 counts
        (SPEC/WEB.md § Roadmap Tasks Page, Acceptance Criterion 88)."""
        self._run(["roadmap", "create", "board_empty_demo"])
        proc, port = self._start(["--port", "0"])

        # A roadmap with no task: five columns, five empty states, no card, and
        # the board itself still rendered.
        _, _, body = self._req(port, "/roadmaps/board_empty_demo/tasks")
        region, columns = self._board_columns(body)
        assert len(columns) == 5, "an empty roadmap must still render all five columns"
        for column, want in zip(columns, self.BOARD_COLUMNS):
            got, count = self._column_header(column)
            assert got == want, f"column titled {got!r}, want {want!r}"
            assert count == 0, f"the empty column {got} shows the count {count}, want 0"
            assert self._shows_empty_state(column), (
                f"the empty column {got} renders no empty state"
            )
        assert 'class="card card-sm task-card' not in region, (
            "an empty roadmap must render no card"
        )

        # A populated board keeps the empty state in the columns that hold no
        # task: the fixture roadmap has no task in DOING.
        _, _, populated = self._req(port, f"/roadmaps/{ROADMAP}/tasks")
        _, columns = self._board_columns(populated)
        for column in columns:
            status, count = self._column_header(column)
            empty = self._shows_empty_state(column)
            assert empty == (count == 0), (
                f"column {status} shows the count {count} and "
                f"{'an' if empty else 'no'} empty state"
            )

    # Elements the HTML activation behaviour lets the keyboard press: only these
    # turn Enter or Space into the click Bootstrap's modal data-api listens for.
    NATIVELY_ACTIVATABLE = ("button", "a", "input")

    def _task_detail(self, port, roadmap, task_id):
        """Return (status, parsed-JSON) of one task's detail endpoint.

        This is the read the modal performs when the user opens a task: the page
        carries one empty shell and the script fills it from here (SPEC/WEB.md
        § Task Detail Endpoint).
        """
        status, _, body = self._req(port, f"/roadmaps/{roadmap}/tasks/{task_id}/data")
        if status != 200:
            return status, None
        return status, json.loads(body)

    @staticmethod
    def _rendered_task_title(body, task_id):
        """Return a task's title exactly as the page rendered it.

        Read from the task's own modal heading, so an expected accessible name is
        composed from the page's own value rather than from a literal repeated in
        the test: Go's html/template escapes element text and attribute values
        with the same replacement table, which
        test_task_title_is_escaped_in_markup_and_in_the_accessible_name pins.
        """
        # The trigger of that task — the board card or the sprint row's title
        # button, both of which carry the task id followed by the accessible name
        # — and then its VISIBLE label. Reading the visible label rather than the
        # aria-label is what keeps an assertion about the accessible name
        # non-circular.
        # Attributes may sit between the task id and the accessible name (the board
        # card also carries its search corpus), so the pattern spans them.
        m = re.search(
            rf'data-task-id="{task_id}"[^>]*aria-label="[^"]*">(.*?)</button>', body, re.S
        )
        assert m, f"task #{task_id} has no trigger to read its title from"
        inner = re.search(r'data-role="task-card-title">(.*?)</span>', m.group(1), re.S)
        return inner.group(1) if inner else m.group(1)

    @staticmethod
    def _opening_tags(body):
        """Yield (tag, attributes) for every opening tag in a document."""
        return re.findall(r"<([a-zA-Z][a-zA-Z0-9]*)\b([^>]*)>", body)

    def test_every_task_has_a_keyboard_activatable_modal_trigger(self):
        """On both surfaces that show a clickable task, each task can be opened by
        a natively activatable control, and nothing that cannot be activated
        pretends to be one.

        The vendored Bootstrap binds the modal data-api on click alone
        (tabler.min.js: ge.on(document, On, '[data-bs-toggle="modal"]', ...) with
        On = 'click...'), so a div or tr carrying role="button" and tabindex="0"
        took focus, announced a button, and could never be pressed. The fix is
        markup, not script (SPEC/WEB.md § Roadmap Tasks Page, clickable card;
        § Sprint Detail Sub-Template)."""
        proc, port = self._start(["--port", "0"])

        for path in (
            f"/roadmaps/{ROADMAP}/tasks",
            f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}",
        ):
            _, _, body = self._req(port, path)
            tags = self._opening_tags(body)
            assert tags, f"{path}: no markup parsed; the extraction is broken"

            # Each task the page can open has at least one activatable trigger,
            # whose accessible name identifies the task.
            activatable_by_task = {}
            for tag, attrs in tags:
                if 'data-bs-toggle="modal"' not in attrs:
                    continue
                target = re.search(r'data-task-id="(\d+)"', attrs)
                if not target:
                    continue
                task_id = target.group(1)
                activatable_by_task.setdefault(task_id, False)
                if tag.lower() not in self.NATIVELY_ACTIVATABLE:
                    continue
                if tag.lower() == "button":
                    assert 'type="button"' in attrs, (
                        f"{path}: a modal trigger button carries no type=\"button\": {attrs}"
                    )
                # The accessible name names the task AND carries its title: a
                # control whose accessible name omits its visible label fails
                # WCAG 2.5.3 (Label in Name), and on the sprint page the visible
                # label of the trigger IS the title. The expectation is composed
                # from the page's own rendering of that title, so it stays exact
                # for a title carrying characters the template escapes.
                title = self._rendered_task_title(body, task_id)
                want = f'aria-label="Open details for task #{task_id}: {title}"'
                assert want in attrs, (
                    f"{path}: the trigger of task #{task_id} does not carry {want}: {attrs}"
                )
                activatable_by_task[task_id] = True

            assert activatable_by_task, f"{path}: no task modal trigger found"
            for task_id, activatable in activatable_by_task.items():
                assert activatable, (
                    f"{path}: task #{task_id} can be opened by pointer only — no trigger of it "
                    f"is a button, an anchor with href, or a form control, so Enter and Space "
                    f"do nothing"
                )

            # And no element fakes a button it cannot be.
            for tag, attrs in tags:
                if 'role="button"' in attrs and 'tabindex="0"' in attrs:
                    assert tag.lower() in self.NATIVELY_ACTIVATABLE, (
                        f"{path}: a <{tag}> carries role=\"button\" with tabindex=\"0\"; it takes "
                        f"focus and announces a button that cannot be pressed: {attrs}"
                    )
                if 'data-bs-toggle="modal"' in attrs:
                    for prop in ('role="button"', 'tabindex="0"'):
                        assert prop not in attrs, (
                            f"{path}: a modal trigger carries {prop}; a real button has it "
                            f"natively and anything else that needs it cannot be activated: {attrs}"
                        )

            # The fix added no script: the page still loads the one vendored
            # bundle, and the policy still forbids inline script.
            scripts = re.findall(r"<script\b([^>]*)>", body)
            srcs = {re.search(r'src="([^"]*)"', s).group(1) for s in scripts}
            want_srcs = {
                "/static/vendor/tabler/tabler.min.js",
                "/static/task-modal.js",
            }
            if path.endswith("/tasks"):
                want_srcs.add("/static/task-search.js")
            assert len(scripts) == len(want_srcs), (
                f"{path}: loads {len(scripts)} scripts, want {len(want_srcs)}"
            )
            assert srcs == want_srcs, f"{path}: unexpected scripts {srcs!r}"
            _, headers, _ = self._req(port, path)
            assert "script-src 'self'" in headers.get("content-security-policy", ""), (
                f"{path}: the Content-Security-Policy no longer restricts script to 'self'"
            )

    def test_task_modal_script_writes_values_as_text_only(self):
        """The script the binary serves writes every value as text.

        Moving the modal's values from html/template to JSON moved the escaping
        responsibility to the client, so this is the property that replaces the
        server's auto-escaping on that path: no markup-parsing sink appears in the
        script the browser actually receives (SPEC/WEB.md § Task Detail Modal,
        Client-side rendering is text-only; Acceptance Criterion 97)."""
        proc, port = self._start(["--port", "0"])
        status, headers, script = self._req(port, "/static/task-modal.js")
        assert status == 200, f"the modal script is not served: {status}"
        assert headers.get("content-type", "").startswith("text/javascript") or (
            "javascript" in headers.get("content-type", "")
        ), headers.get("content-type")

        # Comments are stripped first: the file's header names the sinks it must
        # never use, and naming them in prose is not using them.
        code = re.sub(r"/\*.*?\*/", "", script, flags=re.S)
        code = re.sub(r"^\s*//.*$", "", code, flags=re.M)

        for sink in (
            "innerHTML", "outerHTML", "insertAdjacentHTML", "document.write",
            "eval(", "new Function", "createContextualFragment",
        ):
            assert sink not in code, f"the modal script uses the markup sink {sink!r}"
        assert code.count("textContent") >= 10, (
            "the modal script must write its values through textContent"
        )
        assert "replaceChildren(" in code, (
            "the modal script must clear containers structurally, not by assigning markup"
        )

        # And the policy that makes "no inline script" enforceable is unchanged.
        _, page_headers, _ = self._req(port, f"/roadmaps/{ROADMAP}/tasks")
        csp = page_headers.get("content-security-policy", "")
        assert "script-src 'self'" in csp, csp
        assert "connect-src 'self'" in csp, csp

    def test_sprint_board_card_is_the_single_pointer_and_keyboard_trigger(self):
        """The member-tasks board replaced the six-column member-task table: the
        card itself IS the modal trigger, a real `<button type="button">`, so a
        pointer click, a touch tap, Enter and Space all reach the SAME target
        (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, The card is the
        trigger, and the trigger is a `<button>`; Acceptance Criterion 135).

        This replaces test_sprint_page_row_stays_clickable_by_pointer, whose
        markup — a clickable `<tr class="task-row">` plus a separate
        `task-row__trigger` `<button>` nested in one of its cells — no longer
        exists by design (the table became a board). What that test actually
        verified survives here: a member task is reachable by pointer AND by
        keyboard, and both still open that same task's modal. The property is
        now witnessed through ONE element instead of two, which is exactly what
        `data-task-id` appearing exactly ONCE per card proves: the old split
        trigger carried the id twice — once on the row, once on the button
        nested inside it — so "exactly once" is the signature that the
        two-target arrangement is gone, not merely that a card renders at all.
        """
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}")

        t1 = self.open_task_ids[0]
        marker = f'data-task-id="{t1}"'
        title = self._rendered_task_title(body, t1)

        want = (
            f'<button type="button" class="card card-sm task-card w-100 p-0 text-start" '
            f'data-bs-toggle="modal" data-bs-target="#task-modal" {marker} '
            f'aria-label="Open details for task #{t1}: {title}">'
        )
        assert want in body, (
            f"the member task's card must itself be the trigger; opening tag "
            f"{want!r} not found in the served sprint page"
        )

        card = self._sprint_board_card_html(self._sprint_board_region(body), t1)
        assert "tabindex" not in card, (
            "a <button> is natively focusable; tabindex on the card would be "
            "redundant and is exactly what the SPEC forbids on it"
        )
        assert 'role="button"' not in card, (
            'the card already IS a button; role="button" on it would announce '
            "nothing new and is the pattern this rule forbids on a real button"
        )

        # Exactly one target for this task: the old row+nested-button split
        # trigger carried data-task-id TWICE for one task (row and button); a
        # single <button> card carries it once.
        assert body.count(marker) == 1, (
            f"task #{t1}'s id appears {body.count(marker)} times on the sprint "
            f"page, want exactly 1 — more than one means more than one element "
            f"is wired as that task's modal trigger"
        )

        # No <tr> is a modal trigger on this page, and no row-based markup of
        # any kind remains.
        assert "<table" not in body, "the sprint page must render no member-tasks table"
        assert "task-row" not in body, (
            "no row-based trigger markup (task-row / task-row__trigger) may remain"
        )

    # ====================================================================
    # Header search: narrowing the board, the URL, and the two paths agreeing
    # (SPEC/WEB.md § Roadmap Tasks Page, Header search control)
    # ====================================================================

    @staticmethod
    def _board_cards(region):
        """Return every card of a board region as (id, corpus, shown)."""
        cards = []
        for tag in re.findall(
            r'<button type="button" class="card card-sm task-card[^>]*>', region
        ):
            task_id = re.search(r'data-task-id="(\d+)"', tag)
            corpus = re.search(r'data-search="([^"]*)"', tag)
            assert task_id and corpus, f"a board card carries no id or corpus: {tag}"
            cards.append(
                (int(task_id.group(1)), html_lib.unescape(corpus.group(1)), " hidden" not in tag)
            )
        return cards

    def _board_snapshot(self, port, roadmap, query=""):
        """Return what a served board shows: per column, the ids shown and the
        count, plus the no-match message state."""
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/tasks{query}")
        region, columns = self._board_columns(body)
        snapshot = {"columns": [], "message": False, "body": body, "region": region}
        for column in columns:
            status, count = self._column_header(column)
            cards = self._board_cards(column)
            snapshot["columns"].append({
                "status": status,
                "count": count,
                "shown": [c[0] for c in cards if c[2]],
                "cards": cards,
                "empty": self._shows_empty_state(column),
            })
        m = re.search(r'<div class="empty py-3" data-role="task-search-empty"([^>]*)>', region)
        assert m, "the board carries no no-match message element"
        snapshot["message"] = "hidden" not in m.group(1)
        return snapshot

    def test_tasks_page_header_has_search_and_no_graph_button(self):
        """The page header carries a labelled search input and no knowledge-graph
        button; the sidebar still reaches the graph (Acceptance Criterion 100)."""
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/tasks")

        header = body[body.index('<div class="page-header d-print-none">'):body.index('<main class="page-body">')]
        assert 'data-role="task-search"' in header, "the page header carries no search input"
        assert 'type="search"' in header and 'id="task-search"' in header, header
        assert '<label class="form-label mb-0" for="task-search">Search tasks</label>' in header, (
            "the search input has no associated label"
        )
        assert "placeholder" not in header, "a placeholder must not stand in for the label"
        assert "/graph" not in header, "the page header still links to the knowledge graph"
        assert "<form" not in header, "the search control must submit nothing"

        # The graph stays reachable from this page, through the sidebar.
        assert f'href="/roadmaps/{ROADMAP}/graph"' in body, (
            "the sidebar must still reach the graph from the tasks page"
        )

    def test_tasks_page_search_narrows_the_board_and_its_counts(self):
        """A q term narrows the shown cards and the counts follow, matching the
        title and the #id reference only (Acceptance Criteria 101, 102)."""
        proc, port = self._start(["--port", "0"])

        full = self._board_snapshot(port, ROADMAP)
        total = sum(c["count"] for c in full["columns"])
        assert total > 0, "the fixture roadmap shows no card"

        # A word from one title narrows to the tasks carrying it.
        narrowed = self._board_snapshot(port, ROADMAP, "?q=passkey")
        shown = [i for c in narrowed["columns"] for i in c["shown"]]
        assert shown == [self.pending_task_id], (
            f"the term 'passkey' shows {shown}, want just the passkey task"
        )
        for column in narrowed["columns"]:
            assert column["count"] == len(column["shown"]), (
                f"column {column['status']} counts {column['count']} and shows {len(column['shown'])}"
            )
            assert column["empty"] == (column["count"] == 0), (
                f"column {column['status']} empty-state disagrees with its count"
            )
        # Every card stays in the document, so the browser can widen with no round trip.
        assert sum(len(c["cards"]) for c in narrowed["columns"]) == total

        # Case-insensitive, and the same tasks for the upper-case term.
        upper = self._board_snapshot(port, ROADMAP, "?q=PASSKEY")
        assert [i for c in upper["columns"] for i in c["shown"]] == shown

        # The id and the #id reference both find the task.
        t1 = self.open_task_ids[0]
        for query in (f"?q={t1}", f"?q=%23{t1}"):
            by_ref = self._board_snapshot(port, ROADMAP, query)
            found = [i for c in by_ref["columns"] for i in c["shown"]]
            assert t1 in found, f"{query} does not find task #{t1}: {found}"

        # A term matching nothing empties the board AND says so.
        none = self._board_snapshot(port, ROADMAP, "?q=zzz-nothing-matches")
        assert all(c["count"] == 0 and c["empty"] for c in none["columns"])
        assert none["message"], "a search that matched nothing renders no message"
        assert len(none["columns"]) == 5, "searching dropped a column"

        # An empty term is no term: every card shows, and no message.
        blank = self._board_snapshot(port, ROADMAP, "?q=%20%20")
        assert sum(c["count"] for c in blank["columns"]) == total
        assert not blank["message"]

    def test_tasks_page_search_server_and_client_agree(self):
        """The board the server renders for a term and the board the browser
        produces by narrowing the unnarrowed page are the same (Acceptance
        Criterion 104).

        The browser's rule is re-expressed here from the served script's own
        contract: strip the term's ends by the trim rule's whitespace, fold what
        is left, then match it against the corpus the server folded into
        data-search, or against '#<id>'. The strip is NOT Python's str.strip():
        that removes Python's own set, which also holds U+001C-U+001F, so a
        harness using it would be re-expressing Python's rule rather than the one
        under test. The script is asserted to be this rule in
        test_tasks_page_search_script_is_text_only_and_locale_independent, and
        the whitespace set spelled out below is checked against the one the
        server actually ships in
        test_tasks_page_search_trims_the_term_with_the_shipped_whitespace."""
        proc, port = self._start(["--port", "0"])
        full = self._board_snapshot(port, ROADMAP)

        for term, query in (
            ("", ""),
            ("passkey", "?q=passkey"),
            ("PASSKEY", "?q=PASSKEY"),
            ("  passkey  ", "?q=%20%20passkey%20%20"),
            ("token endpoint", "?q=token%20endpoint"),
            ("#" + str(self.open_task_ids[0]), f"?q=%23{self.open_task_ids[0]}"),
            ("e", "?q=e"),
            ("zzz", "?q=zzz"),
            ("<b>x</b>", "?q=%3Cb%3Ex%3C%2Fb%3E"),
        ):
            server = self._board_snapshot(port, ROADMAP, query)
            folded = term.strip(WHITE_SPACE).lower()

            for column in full["columns"]:
                want = [
                    task_id for task_id, corpus, _ in column["cards"]
                    if folded == "" or folded in corpus or folded in f"#{task_id}"
                ]
                got = next(c for c in server["columns"] if c["status"] == column["status"])
                assert got["shown"] == want, (
                    f"term {term!r}: column {column['status']} shows {got['shown']} on the "
                    f"server and {want} in the browser"
                )
                assert got["count"] == len(want), (
                    f"term {term!r}: column {column['status']} counts {got['count']}, want {len(want)}"
                )
                assert got["empty"] == (len(want) == 0), (
                    f"term {term!r}: column {column['status']} empty-state disagrees"
                )

            shown_total = sum(len(c["shown"]) for c in server["columns"])
            assert server["message"] == (folded != "" and shown_total == 0), (
                f"term {term!r}: the no-match message disagrees with the shown set"
            )

    def test_tasks_page_search_term_is_escaped(self):
        """A term carrying markup is echoed as text into the input and the
        message, and introduces no element or script (Acceptance Criterion 106)."""
        hostile = '"><script>alert(1)</script>'
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(
            port, f"/roadmaps/{ROADMAP}/tasks?q=%22%3E%3Cscript%3Ealert(1)%3C%2Fscript%3E"
        )

        assert hostile not in body, "the raw term reached the page"
        assert "<script>alert(1)</script>" not in body, "the term became a script element"
        assert body.count("<script") == 3, (
            f"the page has {body.count('<script')} script elements, want 3"
        )
        assert body.count("</script>") == 3

        # The input echoes it as an attribute value that decodes back exactly.
        m = re.search(r'<input[^>]*data-role="task-search"[^>]*>', body)
        assert m, "the page renders no search input"
        value = re.search(r'value="([^"]*)"', m.group(0))
        assert value, f"the search input carries no value: {m.group(0)}"
        assert html_lib.unescape(value.group(1)) == hostile, (
            f"the input value decodes to {html_lib.unescape(value.group(1))!r}"
        )

        # And the no-match message names it as text.
        term = re.search(r'data-role="task-search-term">([^<]*)<', body)
        assert term and html_lib.unescape(term.group(1)) == hostile, (
            "the no-match message does not echo the term as text"
        )

    def test_tasks_page_search_script_is_text_only_and_locale_independent(self):
        """The narrowing script loads from /static/, writes the term only as
        text, folds it without a locale, and updates the URL in place
        (Acceptance Criteria 103, 106, 107)."""
        proc, port = self._start(["--port", "0"])
        status, headers, script = self._req(port, "/static/task-search.js")
        assert status == 200, f"the narrowing script is not served: {status}"

        code = re.sub(r"/\*.*?\*/", "", script, flags=re.S)
        code = re.sub(r"^\s*//.*$", "", code, flags=re.M)

        for sink in ("innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("):
            assert sink not in code, f"the search script uses the markup sink {sink!r}"
        assert "textContent" in code, "the search script must write the term as text"

        # The client folds the term with the mapping the SERVER ships it, and
        # calls no case conversion of the JavaScript platform at all: the
        # platform's is Unicode's Default Case Conversion rather than the folding
        # rule, and its tables are of whatever Unicode version the browser ships
        # (Acceptance Criteria 118 and 119).
        for conversion in ("toLowerCase", "toLocaleLowerCase"):
            assert conversion not in script, (
                f"the served script names {conversion}; the same term would then select "
                "different tasks on the two paths, and different tasks in two browsers "
                "of different Unicode versions"
            )
        assert "var FOLD_TABLE = [" in code, "the script ships no folding table"
        assert "codePointAt" in code and "fromCodePoint" in code, (
            "the script must fold the term by code point, not by UTF-16 code unit"
        )

        # And the same for the other half of the term's normalisation: the client
        # strips the term's ends with the whitespace set the SERVER ships, and
        # calls none of the platform's trimming functions — that platform's set
        # keeps U+0085 and removes U+FEFF, which the trim rule does the other way
        # round on both counts (Acceptance Criteria 121 and 122).
        for trimming in (".trim(", ".trimStart(", ".trimEnd(", ".trimLeft(", ".trimRight(",
                         '["trim"]', "['trim']"):
            assert trimming not in script, (
                f"the served script calls the platform's {trimming}; the same term would then "
                "lose different code points on the two paths"
            )
        assert "var SPACE_TABLE = [" in code, "the script ships no whitespace set"
        assert "function trimTerm(" in code and "function isSpaceCodePoint(" in code, (
            "the script must strip the term's ends through the shipped whitespace set"
        )
        # It matches the corpus the server folded, and the #id reference.
        assert 'getAttribute("data-search")' in code
        assert 'data-task-id' in code

        # The URL is updated in place, never stacked, and q is removed when empty.
        assert "replaceState" in code, "the script does not update the URL in place"
        assert "pushState" not in code, "the script stacks a history entry per keystroke"
        assert 'searchParams.delete("q")' in code, "the script leaves an empty q behind"

        # Narrowing reaches neither the network nor a navigation.
        for forbidden in ("fetch(", "XMLHttpRequest", "location.assign", "location.replace"):
            assert forbidden not in code, f"the search script does {forbidden!r}"

        # The policy that keeps every script out of the document is unchanged.
        _, page_headers, page = self._req(port, f"/roadmaps/{ROADMAP}/tasks")
        csp = page_headers.get("content-security-policy", "")
        assert "script-src 'self'" in csp, csp
        srcs = {re.search(r'src="([^"]*)"', s).group(1)
                for s in re.findall(r"<script\b([^>]*)>", page)}
        assert srcs == {
            "/static/vendor/tabler/tabler.min.js",
            "/static/task-modal.js",
            "/static/task-search.js",
        }, srcs

    # A second roadmap, whose titles carry the code points on which the
    # JavaScript platform's own case conversion differs from the folding rule
    # SPEC/WEB.md Acceptance Criterion 118 fixes, plus an ASCII control so a term
    # that narrows nothing at all cannot pass unnoticed. It is separate from the
    # fixture roadmap so no other scenario's totals move.
    FOLD_ROADMAP = "multilingual-settlement"
    FOLD_TITLES = (
        # A WORD-FINAL capital sigma: the rule folds it to U+03C3 in every
        # position, the full conversion would give U+03C2 here.
        ("greek_upper", "\u039f\u0394\u039f\u03a3 \u03a0\u039b\u0397\u03a1\u03a9\u039c\u03a9\u039d: "
                        "\u03b5\u03c0\u03b1\u03bd\u03b1\u03c3\u03c7\u03b5\u03b4\u03b9\u03b1\u03c3\u03bc"
                        "\u03cc\u03c2 \u03b4\u03c1\u03bf\u03bc\u03bf\u03bb\u03cc\u03b3\u03b7\u03c3\u03b7\u03c2"),
        # A LITERAL U+03C2 the author typed: already lower case, folds to itself,
        # and a post-fold rewrite of it would lose this card.
        ("greek_final", "\u03a7\u03b1\u03c1\u03c4\u03bf\u03b3\u03c1\u03ac\u03c6\u03b7\u03c3\u03b7 "
                        "\u03bf\u03b4\u03cc\u03c2 \u03c0\u03bb\u03b7\u03c1\u03c9\u03bc\u03ce\u03bd "
                        "\u03b1\u03bd\u03ac \u03c0\u03ac\u03c1\u03bf\u03c7\u03bf"),
        # U+0130, which the rule folds to U+0069 ALONE.
        ("dotted", "\u0130STANBUL nightly settlement reconciliation"),
        ("plain", "Istanbul acquirer report parser"),
        # Astral code points, which only a code-point walk folds.
        ("deseret", "\U00010400\U00010401 Deseret glossary pilot"),
        ("control", "Rotate the payment gateway signing keys"),
    )

    def _seed_fold_roadmap(self):
        """Create the divergence roadmap through the real CLI and return its
        task ids by key."""
        self._run(["roadmap", "create", self.FOLD_ROADMAP])
        ids = {}
        for key, title in self.FOLD_TITLES:
            ids[key] = self.test.create_task(
                self.FOLD_ROADMAP,
                title,
                "Operators must find this task by typing its title into the board search.",
                "The title is folded once by the server into the card's search corpus.",
                "The term selects the same cards typed as it does carried in the URL.",
            )
        return ids

    @staticmethod
    def _shipped_fold(script):
        """Return (runs, fold) built from the FOLD_TABLE the server ships in the
        narrowing script.

        fold is the MAPPING alone, with no trimming in it: normalising a term is
        two steps, the server ships one table for each, and composing them is the
        caller's job so that neither step can be taken from Python by accident.

        The client's rule is NOT re-expressed here: the table is the one the
        browser would run, and the binary search over it is the one the script
        performs. Python iterates a string by code point, which is the walk the
        script does with codePointAt."""
        m = re.search(r"var FOLD_TABLE = \[(.*?)\];", script, re.S)
        assert m, "the narrowing script ships no FOLD_TABLE"
        numbers = [int(n) for n in re.findall(r"-?\d+", m.group(1))]
        assert numbers, "the shipped FOLD_TABLE is empty"
        assert len(numbers) % 3 == 0, (
            f"the shipped FOLD_TABLE holds {len(numbers)} numbers, which is not whole "
            "triples of start, length, delta"
        )
        runs = [tuple(numbers[i:i + 3]) for i in range(0, len(numbers), 3)]
        for i in range(1, len(runs)):
            assert runs[i - 1][0] + runs[i - 1][1] <= runs[i][0], (
                f"the shipped runs {runs[i - 1]} and {runs[i]} overlap or are out of order"
            )

        def fold(raw):
            out = []
            for char in raw:
                cp = ord(char)
                lo, hi = 0, len(runs) - 1
                while lo <= hi:
                    mid = (lo + hi) // 2
                    start, length, delta = runs[mid]
                    if cp < start:
                        hi = mid - 1
                    elif cp >= start + length:
                        lo = mid + 1
                    else:
                        cp += delta
                        break
                out.append(chr(cp))
            return "".join(out)

        return runs, fold

    @staticmethod
    def _shipped_trim(script):
        """Return (spans, trim) built from the SPACE_TABLE the server ships in the
        narrowing script.

        The trim rule's whitespace is NOT re-expressed here either: the spans are
        the ones the browser would binary search, expanded into the set they
        stand for, and the stripping is the ends-only removal the script does.
        Python's own str.strip() is deliberately not what does the work — its set
        is Python's, not the server's."""
        m = re.search(r"var SPACE_TABLE = \[(.*?)\];", script, re.S)
        assert m, "the narrowing script ships no SPACE_TABLE"
        numbers = [int(n) for n in re.findall(r"-?\d+", m.group(1))]
        assert numbers, "the shipped SPACE_TABLE is empty; the client would trim nothing"
        assert len(numbers) % 2 == 0, (
            f"the shipped SPACE_TABLE holds {len(numbers)} numbers, which is not whole "
            "pairs of start, length"
        )
        spans = [tuple(numbers[i:i + 2]) for i in range(0, len(numbers), 2)]
        for i in range(1, len(spans)):
            assert spans[i - 1][0] + spans[i - 1][1] <= spans[i][0], (
                f"the shipped spans {spans[i - 1]} and {spans[i]} overlap or are out of order"
            )
        shipped = "".join(
            chr(cp) for start, length in spans for cp in range(start, start + length)
        )

        def trim(raw):
            return raw.strip(shipped)

        return spans, trim

    def test_tasks_page_search_folds_the_term_with_the_shipped_mapping(self):
        """Typing a term and opening the URL that carries it select the same cards
        for every term, the two code points included on which the JavaScript
        platform's own case conversion differs from the folding rule (Acceptance
        Criteria 104, 118 and 119).

        This is the defect the criterion exists to forbid: with the client folding
        through the platform's conversion, a term of U+039F U+0394 U+039F U+03A3
        found nothing while the same term carried in q found the card, because the
        platform gives the word-final sigma U+03C2 and the server gives U+03C3.

        The browser's rule is not re-expressed here. The term is folded through the
        FOLD_TABLE the server ships inside the narrowing script — the very table
        the script binary searches — so what is compared are the two real
        paths."""
        ids = self._seed_fold_roadmap()
        proc, port = self._start(["--port", "0"])

        status, _, script = self._req(port, "/static/task-search.js")
        assert status == 200, f"the narrowing script is not served: {status}"
        for conversion in ("toLowerCase", "toLocaleLowerCase"):
            assert conversion not in script, (
                f"the served script names {conversion}; it must fold the term with the "
                "server's shipped mapping and consult no case table of the browser's"
            )
        assert "codePointAt" in script and "fromCodePoint" in script, (
            "the script must fold the term by code point, so a surrogate pair is folded "
            "as the one character it is"
        )

        _, fold = self._shipped_fold(script)

        # The shipped mapping IS the rule Acceptance Criterion 118 fixes.
        assert fold("\u0130") == "\u0069", (
            "U+0130 must fold to U+0069 alone, never to U+0069 U+0307"
        )
        assert fold("\u03a3") == "\u03c3", (
            "U+03A3 must fold to U+03C3 in every position, word-final included"
        )
        assert fold("\u03c2") == "\u03c2", (
            "a literal U+03C2 is already lower case and must not be rewritten"
        )
        assert fold("A") == "a" and fold("\u00c1") == "\u00e1", (
            "ASCII and accented Latin must fold letter for letter"
        )

        full = self._board_snapshot(port, self.FOLD_ROADMAP)
        assert sum(c["count"] for c in full["columns"]) == len(self.FOLD_TITLES), (
            "the divergence roadmap did not render every seeded task"
        )

        for term, want in (
            # The defect, both ways round.
            ("\u039f\u0394\u039f\u03a3", [ids["greek_upper"]]),
            ("\u03bf\u03b4\u03bf\u03c3", [ids["greek_upper"]]),
            # And the regression a post-fold rewrite of U+03C2 would cause.
            ("\u03bf\u03b4\u03cc\u03c2", [ids["greek_final"]]),
            ("\u039f\u0394\u039f\u03a3 \u03a0\u039b\u0397\u03a1\u03a9\u039c\u03a9\u039d",
             [ids["greek_upper"]]),
            ("\u0130STANBUL", [ids["dotted"], ids["plain"]]),
            ("istanbul", [ids["dotted"], ids["plain"]]),
            ("\U00010400\U00010401", [ids["deseret"]]),
            ("SIGNING", [ids["control"]]),
        ):
            query = "?q=" + urllib.parse.quote(term, safe="")
            server = self._board_snapshot(port, self.FOLD_ROADMAP, query)
            shown = sorted(i for c in server["columns"] for i in c["shown"])

            folded = fold(term)
            client = sorted(
                task_id
                for column in full["columns"]
                for task_id, corpus, _ in column["cards"]
                if folded in corpus or folded in f"#{task_id}"
            )

            assert shown == sorted(want), (
                f"the SERVER shows {shown} for {term!r}, want {sorted(want)}"
            )
            assert client == sorted(want), (
                f"the BROWSER shows {client} for {term!r}, want {sorted(want)}"
            )
            assert shown == client, (
                f"the two paths disagree on {term!r}: the server shows {shown}, "
                f"the browser {client}"
            )
            assert server["message"] == (len(shown) == 0), (
                f"term {term!r}: the no-match message disagrees with the shown set"
            )

    def test_tasks_page_search_trims_the_term_with_the_shipped_whitespace(self):
        """Typing a term and opening the URL that carries it select the same cards
        for every term, the two code points included on which the JavaScript
        platform's own trimming differs from the trim rule (Acceptance Criteria
        104, 121 and 122).

        The two differ in OPPOSITE directions, which is why both are exercised:

          - U+0085 (NEXT LINE) carries Unicode's White_Space property, so it IS
            stripped from a term's ends. That platform's trimming keeps it, and
            with the client using that trimming a term of U+0085 + a word found
            nothing while the same term carried in q found the card.
          - U+FEFF (ZERO WIDTH NO-BREAK SPACE) does not carry the property, so it
            is NOT stripped and a term pasted with a byte-order mark matches
            nothing. That platform's trimming removes it, and with the client
            using that trimming the same term found the card while q found
            nothing. The empty result is not the defect; the disagreement was.

        The browser's rule is not re-expressed here. The term is stripped by the
        SPACE_TABLE and folded by the FOLD_TABLE the server ships inside the
        narrowing script — the very tables the script binary searches — so what
        is compared are the two real paths."""
        proc, port = self._start(["--port", "0"])

        status, _, script = self._req(port, "/static/task-search.js")
        assert status == 200, f"the narrowing script is not served: {status}"

        # The absence Acceptance Criterion 122 requires: the client calls none of
        # the platform's five trimming functions, the legacy aliases included.
        for trimming in (".trim(", ".trimStart(", ".trimEnd(", ".trimLeft(", ".trimRight(",
                         '["trim"]', "['trim']"):
            assert trimming not in script, (
                f"the served script calls the platform's {trimming}; it must strip the term's "
                "ends with the server's shipped whitespace set, so U+0085 and U+FEFF resolve "
                "the same way on both paths"
            )

        spans, trim = self._shipped_trim(script)
        _, fold = self._shipped_fold(script)
        shipped = {cp for start, length in spans for cp in range(start, start + length)}

        # The shipped set IS the property this module states, so the model used by
        # test_tasks_page_search_server_and_client_agree cannot rot unnoticed.
        assert shipped == set(map(ord, WHITE_SPACE)), (
            "the shipped SPACE_TABLE is not Unicode's White_Space property: "
            f"extra {sorted(shipped - set(map(ord, WHITE_SPACE)))}, "
            f"missing {sorted(set(map(ord, WHITE_SPACE)) - shipped)}"
        )
        assert 0x0085 in shipped, (
            "U+0085 carries White_Space and MUST be in the shipped set, though the platform's "
            "own trimming keeps it"
        )
        assert 0xFEFF not in shipped, (
            "U+FEFF does not carry White_Space and MUST NOT be in the shipped set, though the "
            "platform's own trimming removes it"
        )

        full = self._board_snapshot(port, ROADMAP)
        every = sorted(task_id for c in full["columns"] for task_id, _, _ in c["cards"])
        passkey = [self.pending_task_id]

        for term, want in (
            # The trim, first direction: U+0085 goes, and the word finds the card.
            ("\u0085passkey", passkey),
            ("passkey\u0085", passkey),
            ("\u0085PASSKEY\u0085", passkey),
            # The other direction: U+FEFF stays, and the term finds nothing.
            ("\ufeffpasskey", []),
            ("passkey\ufeff", []),
            # The ends only: whitespace inside a term is matched literally.
            ("pass\u0085key", []),
            # A term of the property alone is no term at all; one of U+FEFF is.
            ("\u0085", every),
            ("\ufeff", []),
            # And the code points both sides always agreed on still work, so the
            # two above are a difference rather than the whole rule.
            (" \t\r\n\u00a0\u2003PASSKEY\u3000\v\f ", passkey),
        ):
            query = "?q=" + urllib.parse.quote(term, safe="")
            server = self._board_snapshot(port, ROADMAP, query)
            shown = sorted(i for c in server["columns"] for i in c["shown"])

            folded = fold(trim(term))
            client = sorted(
                task_id
                for column in full["columns"]
                for task_id, corpus, _ in column["cards"]
                if folded == "" or folded in corpus or folded in f"#{task_id}"
            )

            assert shown == sorted(want), (
                f"the SERVER shows {shown} for {term!r}, want {sorted(want)}"
            )
            assert client == sorted(want), (
                f"the BROWSER shows {client} for {term!r}, want {sorted(want)}"
            )
            assert shown == client, (
                f"the two paths disagree on {term!r}: the server shows {shown}, "
                f"the browser {client}"
            )
            assert server["message"] == (folded != "" and len(shown) == 0), (
                f"term {term!r}: the no-match message disagrees with the shown set"
            )

    def test_tasks_page_search_never_errors(self):
        """No q value produces an error page, and an undecodable one is treated as
        absent (Acceptance Criterion 105)."""
        proc, port = self._start(["--port", "0"])
        full = self._board_snapshot(port, ROADMAP)
        total = sum(c["count"] for c in full["columns"])

        for query in ("?q=", "?q=%20", "?q=zzz", "?q=%zz", "?q=%", "?q=" + "x" * 3000,
                      "?q=%00", "?q=one&q=two"):
            status, _, body = self._req(port, f"/roadmaps/{ROADMAP}/tasks{query}")
            assert status == 200, f"{query} answered {status}, want 200"
            assert 'data-role="task-board"' in body, f"{query} rendered no board"

        # The undecodable one is treated as absent: the board is unnarrowed.
        undecodable = self._board_snapshot(port, ROADMAP, "?q=%zz")
        assert sum(c["count"] for c in undecodable["columns"]) == total, (
            "an undecodable q narrowed the board; it must be treated as absent"
        )

    # ---- tasks board header filters (AC112-AC117) -----------------------

    # The filter fixture, seeded on demand by _seed_filter_tasks. Every task
    # carries a distinct (type, priority, severity) triple, the ten TaskType
    # values are all present, and both ends of the threshold range are populated,
    # so no filter assertion below can pass by accident. The "cache" family is
    # built so that the specification's worked example
    # ?q=cache&type=BUG&priority=7 has a task excluded by EACH of its three
    # criteria and by no other, which makes dropping any one of them observable.
    FILTER_SEED = [
        ("Cache the acquirer settlement report", "BUG", 8, 7),
        ("Cache invalidation drops the refund receipt", "BUG", 7, 3),
        ("Cache warmup exceeds the deployment window", "BUG", 6, 9),
        ("Cache the merchant catalogue in the edge tier", "EPIC", 9, 2),
        ("Cache the currency conversion table", "TASK", 7, 7),
        ("Retire the legacy settlement cache", "BUG", 2, 9),
        ("Duplicate payout on a retried webhook", "BUG", 9, 5),
        ("Rotate the acquirer signing keys", "CHORE", 9, 8),
        ("Publish the acquirer onboarding runbook", "CHORE", 0, 0),
        ("Draft the payout reconciliation story", "USER_STORY", 5, 1),
        ("Split the ledger writer into its own package", "REFACTOR", 4, 0),
        ("Investigate the settlement latency spike", "SPIKE", 3, 6),
        ("Redesign the refund confirmation screen", "DESIGN_UX", 2, 2),
        ("Backfill the dispute evidence index", "SUB_TASK", 1, 5),
        ("Improve the payout scheduling heuristics", "IMPROVEMENT", 7, 4),
    ]

    def _seed_filter_tasks(self):
        """Create the filter fixture and return {id: (title, type, priority,
        severity)} for the tasks it created."""
        seeded = {}
        for title, task_type, priority, severity in self.FILTER_SEED:
            _, out, _ = self._run([
                "task", "create", "-r", ROADMAP,
                "-t", title,
                "-y", task_type,
                "-p", str(priority),
                "--severity", str(severity),
                "-fr", "Operators must be able to narrow the board to this work.",
                "-tr", "Read-only on the web side, over the roadmap database.",
                "-ac", "The board shows the task under every control that admits it.",
            ])
            seeded[json.loads(out)["id"]] = (title, task_type, priority, severity)
        return seeded

    @staticmethod
    def _card_dimensions(region):
        """Return {id: (type, priority, severity)} for every card in a region."""
        dimensions = {}
        for tag in re.findall(
            r'<button type="button" class="card card-sm task-card[^>]*>', region
        ):
            task_id = re.search(r'data-task-id="(\d+)"', tag)
            task_type = re.search(r'data-type="([^"]*)"', tag)
            priority = re.search(r'data-priority="([^"]*)"', tag)
            severity = re.search(r'data-severity="([^"]*)"', tag)
            assert task_id and task_type and priority and severity, (
                f"a board card carries no type, priority or severity: {tag}"
            )
            dimensions[int(task_id.group(1))] = (
                task_type.group(1), int(priority.group(1)), int(severity.group(1))
            )
        return dimensions

    @staticmethod
    def _read_select(body, control_id):
        """Return [(value, label, selected)] for the <select> carrying an id."""
        block = re.search(
            r'<select[^>]*\bid="' + re.escape(control_id) + r'"[^>]*>(.*?)</select>',
            body, re.S,
        )
        assert block, f"the page renders no <select id={control_id!r}>"
        return [
            (m.group(1), m.group(3), "selected" in m.group(2))
            for m in re.finditer(
                r'<option value="([^"]*)"([^>]*)>([^<]*)</option>', block.group(1)
            )
        ]

    @staticmethod
    def _selected(options, control_id):
        chosen = [value for value, _, is_selected in options if is_selected]
        assert len(chosen) == 1, (
            f"{control_id} has {len(chosen)} selected options ({chosen}), want exactly 1"
        )
        return chosen[0]

    def _shown_ids(self, snapshot):
        return {task_id for column in snapshot["columns"] for task_id in column["shown"]}

    @staticmethod
    def _keeps(term, task_type, priority, severity, task_id, seeded):
        """The conjunction, written from the specification and computed over the
        seeded values: substring over title or '#<id>', equality on type, '>=' on
        the two thresholds, and a control left empty contributing no criterion."""
        title, seed_type, seed_priority, seed_severity = seeded[task_id]
        folded = term.strip().lower()
        if folded and folded not in title.lower() and folded not in f"#{task_id}":
            return False
        if task_type and seed_type != task_type:
            return False
        if priority and seed_priority < int(priority):
            return False
        if severity and seed_severity < int(severity):
            return False
        return True

    FILTER_CONTROL_IDS = (
        ("task-filter-type", "type"),
        ("task-filter-priority", "priority"),
        ("task-filter-severity", "severity"),
    )

    def test_tasks_page_header_carries_the_three_filter_dropdowns(self):
        """The actions column carries exactly three labelled filter dropdowns
        beside the search input, offering the ten TaskType values and the
        thresholds 1-9, with a no-filter first option and no status filter
        (Acceptance Criterion 112)."""
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/tasks")

        assert len(re.findall(r"<select\b", body)) == 3, (
            "the tasks page must carry exactly three <select> controls"
        )
        assert 'data-role="task-search"' in body, "the search input the filters sit beside is gone"

        # Every control names its dimension through a real, associated label.
        for control_id in ("task-search", "task-filter-type",
                           "task-filter-priority", "task-filter-severity"):
            label = re.search(
                r'<label[^>]*\bfor="' + re.escape(control_id) + r'"[^>]*>([^<]+)</label>', body
            )
            assert label and label.group(1).strip(), (
                f"{control_id} carries no non-empty <label for=...>; a placeholder or a "
                f"first option may not stand in for one"
            )

        types = self._read_select(body, "task-filter-type")
        assert [value for value, _, _ in types] == [
            "", "USER_STORY", "TASK", "BUG", "SUB_TASK", "EPIC",
            "REFACTOR", "CHORE", "SPIKE", "DESIGN_UX", "IMPROVEMENT",
        ], f"the type filter offers {[v for v, _, _ in types]}"
        assert types[0][2], "the no-filter option is not selected on an unfiltered board"

        for control_id in ("task-filter-priority", "task-filter-severity"):
            options = self._read_select(body, control_id)
            assert [value for value, _, _ in options] == [""] + [str(n) for n in range(1, 10)], (
                f"{control_id} offers {[v for v, _, _ in options]}, want the no-filter option and 1-9"
            )
            assert options[0][2], f"{control_id} does not start on its no-filter option"

        # No status filter: the columns already are the status.
        for status in ("BACKLOG", "SPRINT", "DOING", "TESTING", "COMPLETED"):
            assert f'<option value="{status}"' not in body, (
                f"the header offers {status} as a filter value; the board offers no status filter"
            )
        assert 'id="task-filter-status"' not in body
        assert 'name="status"' not in body

    def test_tasks_page_filters_narrow_the_board_and_its_counts(self):
        """Each filter narrows by its own dimension, the type filter is an
        equality and the two thresholds are '>=', and every column count follows
        the narrowed set (Acceptance Criterion 113)."""
        seeded = self._seed_filter_tasks()
        proc, port = self._start(["--port", "0"])

        full = self._board_snapshot(port, ROADMAP)
        dimensions = self._card_dimensions(full["region"])
        assert set(seeded) <= set(dimensions), "the board dropped a seeded task"

        for task_id, (_, task_type, priority, severity) in seeded.items():
            assert dimensions[task_id] == (task_type, priority, severity), (
                f"task #{task_id} carries {dimensions[task_id]} on its card, "
                f"want {(task_type, priority, severity)}"
            )

        for task_type in ("USER_STORY", "TASK", "BUG", "SUB_TASK", "EPIC",
                          "REFACTOR", "CHORE", "SPIKE", "DESIGN_UX", "IMPROVEMENT"):
            snapshot = self._board_snapshot(port, ROADMAP, f"?type={task_type}")
            want = {tid for tid, dim in dimensions.items() if dim[0] == task_type}
            assert self._shown_ids(snapshot) == want, (
                f"?type={task_type} shows {self._shown_ids(snapshot)}, want {want}"
            )
            for column in snapshot["columns"]:
                assert column["count"] == len(column["shown"]), (
                    f"?type={task_type}: column {column['status']} counts "
                    f"{column['count']} while showing {len(column['shown'])} cards"
                )
                assert column["empty"] == (not column["shown"])
            assert len(snapshot["columns"]) == 5, "a filter dropped a column"

        for threshold in range(1, 10):
            for name, index in (("priority", 1), ("severity", 2)):
                snapshot = self._board_snapshot(port, ROADMAP, f"?{name}={threshold}")
                want = {tid for tid, dim in dimensions.items() if dim[index] >= threshold}
                assert self._shown_ids(snapshot) == want, (
                    f"?{name}={threshold} shows {self._shown_ids(snapshot)}, want {want}"
                )
                for column in snapshot["columns"]:
                    assert column["count"] == len(column["shown"])

        # The threshold is "at least", not "exactly": tasks ABOVE it are shown.
        above = {tid for tid, dim in dimensions.items() if dim[1] > 8}
        assert above, "the fixture holds no task above priority 8; the assertion would be vacuous"
        assert above <= self._shown_ids(self._board_snapshot(port, ROADMAP, "?priority=8"))

    def test_tasks_page_filters_compose_conjunctively_with_the_search(self):
        """The shown set is the conjunction of every active control, including
        the specification's worked example (Acceptance Criterion 114)."""
        seeded = self._seed_filter_tasks()
        proc, port = self._start(["--port", "0"])
        full = self._board_snapshot(port, ROADMAP)
        every = set(self._card_dimensions(full["region"]))

        worked = self._board_snapshot(port, ROADMAP, "?q=cache&type=BUG&priority=7")
        want = {tid for tid in every
                if tid in seeded and self._keeps("cache", "BUG", "7", "", tid, seeded)}
        assert len(want) == 2, f"the fixture makes the worked example select {len(want)} tasks, want 2"
        assert self._shown_ids(worked) == want, (
            f"?q=cache&type=BUG&priority=7 shows {self._shown_ids(worked)}, want {want}"
        )

        # Each of the three criteria is doing work: dropping any one widens the
        # board, so the assertion above cannot be satisfied by ignoring one.
        for query in ("?type=BUG&priority=7", "?q=cache&priority=7", "?q=cache&type=BUG"):
            wider = self._shown_ids(self._board_snapshot(port, ROADMAP, query))
            assert len(wider) > len(want), (
                f"{query} shows {len(wider)} cards, not more than the {len(want)} of the full "
                f"conjunction: that criterion selects nothing of its own"
            )

        for term, task_type, priority, severity in (
            ("", "", "", ""),
            ("cache", "", "", ""),
            ("", "BUG", "", ""),
            ("", "", "7", ""),
            ("", "", "", "6"),
            ("cache", "BUG", "", ""),
            ("cache", "", "7", ""),
            ("", "BUG", "7", ""),
            ("", "CHORE", "", "8"),
            ("", "", "5", "5"),
            ("cache", "BUG", "7", ""),
            ("cache", "BUG", "", "9"),
            ("the", "TASK", "6", "3"),
            ("settlement", "SPIKE", "1", "1"),
            ("", "DESIGN_UX", "9", ""),
            ("zzz-nothing-matches", "BUG", "3", "3"),
        ):
            query = "&".join(
                f"{name}={urllib.parse.quote(value)}"
                for name, value in (("q", term), ("type", task_type),
                                    ("priority", priority), ("severity", severity))
                if value
            )
            snapshot = self._board_snapshot(port, ROADMAP, f"?{query}" if query else "")
            # The expectation is computed over EVERY card the unnarrowed board
            # carries - the four tasks _populate created as well as the seeded
            # ones - from the card's own dimensions and the conjunction as the
            # specification states it, never from the narrowed page under test.
            want = set()
            dimensions = self._card_dimensions(full["region"])
            titles = self._board_titles(full["region"])
            assert set(dimensions) == every, "the card sweep lost a card"
            for tid, (card_type, card_priority, card_severity) in dimensions.items():
                folded = term.strip().lower()
                if folded and folded not in titles[tid].lower() and folded not in f"#{tid}":
                    continue
                if task_type and card_type != task_type:
                    continue
                if priority and card_priority < int(priority):
                    continue
                if severity and card_severity < int(severity):
                    continue
                want.add(tid)
            assert self._shown_ids(snapshot) == want, (
                f"?{query} shows {self._shown_ids(snapshot)}, want {want}"
            )
            for column in snapshot["columns"]:
                assert column["count"] == len(column["shown"]), (
                    f"?{query}: column {column['status']} miscounts"
                )
            assert snapshot["message"] == (
                bool(query) and not self._shown_ids(snapshot)
            ), f"?{query}: the no-match message disagrees with the shown set"

    @staticmethod
    def _board_titles(region):
        """Return {id: title} for every card in a board region, from the folded
        corpus the server wrote into the card."""
        titles = {}
        for tag in re.findall(
            r'<button type="button" class="card card-sm task-card[^>]*>', region
        ):
            task_id = int(re.search(r'data-task-id="(\d+)"', tag).group(1))
            titles[task_id] = html_lib.unescape(
                re.search(r'data-search="([^"]*)"', tag).group(1)
            )
        return titles

    def test_tasks_page_filters_never_error_and_are_independent(self):
        """A value a dimension does not accept applies no filter on that
        dimension, answers 200, and leaves the other dimensions applied
        (Acceptance Criterion 115)."""
        seeded = self._seed_filter_tasks()
        proc, port = self._start(["--port", "0"])
        full = self._board_snapshot(port, ROADMAP)
        total = sum(c["count"] for c in full["columns"])

        for query in (
            "type=NOT_A_TYPE", "type=bug", "type=Bug", "type=BUG,EPIC",
            "type=BUG%20EPIC", "type=%20BUG%20", "type=", "type=%zz",
            "priority=0", "priority=10", "priority=-1", "priority=%2B7",
            "priority=07", "priority=%207", "priority=high", "priority=7.0",
            "priority=", "priority=%zz",
            "severity=0", "severity=10", "severity=critical", "severity=", "severity=%zz",
            "status=DOING",
        ):
            status, _, body = self._req(port, f"/roadmaps/{ROADMAP}/tasks?{query}")
            assert status == 200, f"?{query} answered {status}, want 200"
            assert 'data-role="task-board"' in body, f"?{query} rendered no board"

            snapshot = self._board_snapshot(port, ROADMAP, f"?{query}")
            assert sum(c["count"] for c in snapshot["columns"]) == total, (
                f"?{query} narrowed the board; an unaccepted value applies no filter"
            )
            assert not snapshot["message"], (
                f"?{query}: the board says nothing matches while no control is in force"
            )
            for control_id, _ in self.FILTER_CONTROL_IDS:
                chosen = self._selected(self._read_select(body, control_id), control_id)
                assert chosen == "", (
                    f"?{query}: {control_id} is on {chosen!r}, want the no-filter option"
                )

        # The dimensions are independent: an unusable type leaves the accepted
        # priority and the term applying.
        mixed = self._board_snapshot(port, ROADMAP, "?q=cache&type=nope&priority=7")
        priced = self._board_snapshot(port, ROADMAP, "?q=cache&priority=7")
        assert self._shown_ids(mixed) == self._shown_ids(priced), (
            "an unusable type changed what the accepted priority and the term select"
        )
        assert self._shown_ids(mixed), "the independence assertion is vacuous: nothing is shown"

        # A repeated parameter is read as its FIRST occurrence.
        repeated = self._board_snapshot(port, ROADMAP, "?type=BUG&type=EPIC")
        bug = self._board_snapshot(port, ROADMAP, "?type=BUG")
        epic = self._board_snapshot(port, ROADMAP, "?type=EPIC")
        assert self._shown_ids(repeated) == self._shown_ids(bug), (
            "?type=BUG&type=EPIC is not the BUG board"
        )
        assert self._shown_ids(bug) != self._shown_ids(epic), (
            "the fixture makes BUG and EPIC select the same tasks; the assertion is vacuous"
        )

        # A comma-packed value is one string, names no TaskType, and is ignored.
        packed = self._board_snapshot(port, ROADMAP, "?type=BUG,EPIC")
        assert sum(c["count"] for c in packed["columns"]) == total, (
            "?type=BUG,EPIC narrowed the board; it must be ignored whole"
        )

        # The parameters do not depend on their order in the query string.
        forward = self._board_snapshot(port, ROADMAP, "?q=cache&type=BUG&priority=7")
        reverse = self._board_snapshot(port, ROADMAP, "?priority=7&type=BUG&q=cache")
        assert self._shown_ids(forward) == self._shown_ids(reverse), (
            "the board depends on the order of the query parameters"
        )
        assert seeded, "the fixture seeded nothing"

    def test_tasks_page_filters_round_trip_through_the_url(self):
        """A cold load of a URL carrying any combination renders that board with
        every control already showing the value that produced it, and clearing
        every control restores the full board and the bare URL (Acceptance
        Criterion 116)."""
        self._seed_filter_tasks()
        proc, port = self._start(["--port", "0"])
        full = self._board_snapshot(port, ROADMAP)
        total = sum(c["count"] for c in full["columns"])

        for term, task_type, priority, severity in (
            ("cache", "BUG", "7", ""),
            ("", "CHORE", "", "8"),
            ("the", "", "5", "5"),
            ("cache", "", "", ""),
            ("", "EPIC", "9", "1"),
        ):
            query = "&".join(
                f"{name}={urllib.parse.quote(value)}"
                for name, value in (("q", term), ("type", task_type),
                                    ("priority", priority), ("severity", severity))
                if value
            )
            _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/tasks?{query}")
            value = re.search(r'value="([^"]*)"', re.search(
                r'<input[^>]*data-role="task-search"[^>]*>', body).group(0))
            assert html_lib.unescape(value.group(1)) == term, (
                f"?{query}: the search input shows {value.group(1)!r}, want {term!r}"
            )
            for (control_id, _), want in zip(self.FILTER_CONTROL_IDS,
                                             (task_type, priority, severity)):
                chosen = self._selected(self._read_select(body, control_id), control_id)
                assert chosen == want, (
                    f"?{query}: {control_id} shows {chosen!r}, want {want!r}"
                )

        # Clearing every control restores the full board with its TRUE counts.
        bare = self._board_snapshot(port, ROADMAP)
        assert sum(c["count"] for c in bare["columns"]) == total
        assert not bare["message"]
        for control_id, _ in self.FILTER_CONTROL_IDS:
            assert self._selected(self._read_select(bare["body"], control_id), control_id) == ""

    def test_tasks_page_filter_script_applies_the_same_conjunction(self):
        """The dropdowns are applied by the SAME /static/ script that applies the
        term, as one conjunction, with the URL kept in place and no inline script
        or policy change (Acceptance Criteria 116 and 117)."""
        proc, port = self._start(["--port", "0"])
        status, _, script = self._req(port, "/static/task-search.js")
        assert status == 200, f"the narrowing script is not served: {status}"

        code = re.sub(r"/\*.*?\*/", "", script, flags=re.S)
        code = re.sub(r"^\s*//.*$", "", code, flags=re.M)

        for fragment in (
            'data-role="task-filter-type"',
            'data-role="task-filter-priority"',
            'data-role="task-filter-severity"',
            'attribute: "data-type"',
            'attribute: "data-priority"',
            'attribute: "data-severity"',
            'param: "type"', 'param: "priority"', 'param: "severity"',
        ):
            assert fragment in code, f"the narrowing script does not carry {fragment!r}"

        assert "return cardValue === filterValue" in code, "the type filter is not an equality"
        assert "return Number(cardValue) >= Number(filterValue)" in code, (
            "the threshold filters are not '>='"
        )
        assert "Number(cardValue) > Number(filterValue)" not in code

        # One conjunction, and one entry point for all four controls.
        assert 'addEventListener("change", narrow)' in code, (
            "the dropdowns are not wired to the same narrowing entry point as the search input"
        )
        assert 'input.addEventListener("input", narrow)' in code

        # The URL is kept in step in place, and a control on its no-filter option
        # removes its parameter.
        assert "replaceState" in code and "pushState" not in code
        assert "url.searchParams.delete(filters[i].param)" in code
        assert "url.searchParams.set(filters[i].param, state.filters[i])" in code

        for forbidden in ("fetch(", "XMLHttpRequest", "location.assign", "location.replace",
                          "innerHTML", "insertAdjacentHTML", "document.write", "eval("):
            assert forbidden not in code, f"the narrowing script does {forbidden!r}"

        # No new script and no policy change came in with the dropdowns.
        _, headers, page = self._req(port, f"/roadmaps/{ROADMAP}/tasks?type=BUG&priority=7")
        assert "script-src 'self'" in headers.get("content-security-policy", "")
        srcs = {re.search(r'src="([^"]*)"', s).group(1)
                for s in re.findall(r"<script\b([^>]*)>", page)}
        assert srcs == {
            "/static/vendor/tabler/tabler.min.js",
            "/static/task-modal.js",
            "/static/task-search.js",
        }, srcs
        assert "style=" not in page, "the filters introduced an inline style attribute"

    def test_tasks_page_filter_values_are_never_echoed_into_the_page(self):
        """A filter value never reaches the document: the options are the
        server's own enumeration and an unaccepted value selects the no-filter
        option (Acceptance Criterion 117)."""
        proc, port = self._start(["--port", "0"])
        hostile = '"><script>alert(1)</script>'
        encoded = urllib.parse.quote(hostile, safe="")
        status, _, body = self._req(
            port,
            f"/roadmaps/{ROADMAP}/tasks?type={encoded}&priority={encoded}&severity={encoded}",
        )

        assert status == 200, f"a hostile filter value answered {status}, want 200"
        assert hostile not in body, "a raw filter value reached the page"
        assert "alert(1)" not in body, "a filter value became script content"
        assert body.count("<script") == 3, (
            f"the page has {body.count('<script')} script elements, want 3"
        )
        for control_id, _ in self.FILTER_CONTROL_IDS:
            assert self._selected(self._read_select(body, control_id), control_id) == "", (
                f"{control_id} did not fall back to its no-filter option"
            )

    def test_no_page_carries_a_footer_or_the_read_only_notice(self):
        """No page ends with a footer band.

        Every page except the knowledge-graph one used to close with a footer
        whose entire content was the sentence below, restating a property the
        interface already demonstrates by having no control that writes. The
        element was removed - the element, not merely its text, so no empty band
        is left - and this is the guard that keeps it removed. It sweeps every
        page route, the graph page included, which never had one."""
        notice = "Read-only. The rmp CLI remains the sole write path."
        proc, port = self._start(["--port", "0"])

        for path in (
            "/",
            f"/roadmaps/{ROADMAP}",
            f"/roadmaps/{ROADMAP}/tasks",
            f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}",
            f"/roadmaps/{ROADMAP}/audit",
            f"/roadmaps/{ROADMAP}/graph",
        ):
            status, _, body = self._req(port, path)
            assert status == 200, f"{path}: status {status}"
            # A body that came back empty would satisfy every absence below
            # without proving anything.
            assert '<div class="page">' in body, f"{path}: no admin shell in the response"

            assert "<footer" not in body, f"{path}: renders a <footer> element"
            assert "</footer>" not in body, f"{path}: renders a closing </footer> tag"
            assert notice not in body, f"{path}: renders the read-only notice"
            assert "footer-transparent" not in body, (
                f"{path}: the page footer is back under another element"
            )

            # The shell itself is untouched: the sidebar, the top navbar, the page
            # header and the main landmark keep their places.
            assert '<aside class="navbar navbar-vertical' in body, f"{path}: lost its sidebar"
            assert '<main class="page-body">' in body, f"{path}: lost its main landmark"

    def test_serving_pages_writes_no_audit_entry(self):
        before = self._run(["audit", "stats", "-r", ROADMAP])[1]
        before_total = json.loads(before).get("total_entries")
        proc, port = self._start(["--port", "0"])
        for _ in range(4):
            assert self._req(port, f"/roadmaps/{ROADMAP}")[0] == 200
            assert self._req(port, f"/roadmaps/{ROADMAP}/tasks")[0] == 200
        after = self._run(["audit", "stats", "-r", ROADMAP])[1]
        after_total = json.loads(after).get("total_entries")
        assert before_total == after_total, (
            f"serving the sprints/tasks pages changed the audit log: {before_total} -> {after_total}"
        )

    # ====================================================================
    # Audit log page: full log, performed_at DESC, paginated, clamped
    # ====================================================================

    def test_audit_page_lists_entries_ordered_desc(self):
        """The audit log page renders the roadmap's full audit log as a read-only
        table with the AuditEntry columns (ID, Operation, Entity Type, Entity ID,
        Performed At), ordered by performed_at DESC, with no edit affordance
        (SPEC/WEB.md § Roadmap Audit Log Page)."""
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}/audit")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")

        # The five AuditEntry column headers are present.
        for col in ("Operation", "Entity Type", "Entity ID", "Performed At"):
            assert f"<th>{col}</th>" in body, f"audit table missing the {col!r} column"

        # The populated roadmap exercised create/update operations through the
        # CLI, so its audit log is non-empty: a real operation name appears.
        assert "TASK_CREATE" in body or "SPRINT_CREATE" in body, (
            "audit page must list the recorded CLI operations"
        )

        # Ordered performed_at DESC: every rendered timestamp is non-increasing.
        # The Performed At cell carries a unique class; extract them in document
        # order and assert monotonic non-increasing.
        stamps = re.findall(
            r'<td class="text-nowrap text-secondary">([^<]+)</td>', body
        )
        assert len(stamps) >= 2, "expected several audit rows in the populated roadmap"
        assert stamps == sorted(stamps, reverse=True), (
            f"audit rows must be ordered performed_at DESC; got {stamps}"
        )

        # Read-only: no form, no input, no write-method submission.
        low = body.lower()
        assert "<form" not in low, "audit page must contain no form"
        assert "<input" not in low, "audit page must contain no input"
        assert not re.search(r'method=["\']?(post|put|patch|delete)', body, re.I), (
            "audit page must not submit any change"
        )
        # Read-only: no clickable row / modal trigger on the audit table.
        assert 'data-bs-target="#task-modal-' not in body, (
            "audit page must render no clickable row / task modal"
        )

    def test_audit_page_pagination_and_clamping(self):
        """The audit page is paginated at 100 entries per page, selected by a
        1-based ?page= parameter, clamped (never 404) for out-of-range or garbage
        values, with a 'Page X of Y' indicator and Previous/Next controls bounded
        at the first/last page (SPEC/WEB.md § Roadmap Audit Log Page)."""
        # Build a roadmap with more than 100 audit entries. Each task create is
        # one audit operation; creating 130 tasks yields >= 130 audit rows, so the
        # log spans at least two 100-entry pages.
        self._run(["roadmap", "create", "audit_paging"])
        for i in range(130):
            self.test.create_task(
                "audit_paging",
                f"Harden subsystem component {i:03d}",
                "Eliminate an identified attack surface in the subsystem",
                "Apply the documented mitigation and add a regression test",
                "The mitigation holds under the regression test",
            )
        proc, port = self._start(["--port", "0"])

        # Page 1 of a multi-page log: a "Page 1 of N" (N >= 2) indicator, a Next
        # link, and no active Previous link.
        status, _, body = self._req(port, "/roadmaps/audit_paging/audit?page=1")
        assert status == 200
        m = re.search(r"Page 1 of (\d+)", body)
        assert m, "page 1 must show a 'Page 1 of N' indicator"
        total_pages = int(m.group(1))
        assert total_pages >= 2, f"expected >= 2 pages, got {total_pages}"
        assert 'href="?page=2"' in body, "page 1 must offer an active Next link"
        assert 'href="?page=0"' not in body, "page 1 must not offer an active Previous link"
        # Exactly 100 data rows on a full first page.
        rows = body.count('<td class="text-nowrap text-secondary">')
        assert rows == 100, f"a full first page must show 100 rows, got {rows}"

        # The last page: an active Previous link, no active Next link.
        status, _, last = self._req(
            port, f"/roadmaps/audit_paging/audit?page={total_pages}"
        )
        assert status == 200
        assert f"Page {total_pages} of {total_pages}" in last
        assert f'href="?page={total_pages - 1}"' in last, (
            "the last page must offer an active Previous link"
        )
        assert f'href="?page={total_pages + 1}"' not in last, (
            "the last page must not offer an active Next link"
        )

        # Clamping: page=0, a negative page, garbage, and a far-too-large page all
        # render 200 (never 404), clamped to the nearest valid page.
        for q, want in (
            ("page=0", "Page 1 of"),
            ("page=-5", "Page 1 of"),
            ("page=abc", "Page 1 of"),
            ("page=", "Page 1 of"),
            ("page=99999", f"Page {total_pages} of {total_pages}"),
        ):
            status, _, b = self._req(port, f"/roadmaps/audit_paging/audit?{q}")
            assert status == 200, f"{q!r} must clamp to 200, never 404; got {status}"
            assert want in b, f"{q!r} must clamp to {want!r}"

    def test_audit_page_empty_state(self):
        """A roadmap whose audit log is empty renders 200 with an empty-state
        message and 'Page 1 of 1', with no active pagination controls
        (SPEC/WEB.md § Roadmap Audit Log Page, empty state)."""
        # A brand-new roadmap, before any auditable operation, has an empty log.
        self._run(["roadmap", "create", "audit_blank"])
        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, "/roadmaps/audit_blank/audit")
        assert status == 200, "an empty audit log must render 200, not an error"
        assert "Page 1 of 1" in body, "empty audit log must show 'Page 1 of 1'"
        low = body.lower()
        assert "no audit" in low, "empty audit log must show a clear empty-state message"
        # No active prev/next pagination links on a single empty page.
        assert 'href="?page=' not in body, (
            "an empty single-page audit log must have no active pagination link"
        )

    def test_audit_page_name_guard_and_methods(self):
        """The audit route validates {name} (invalid/nonexistent -> 404) and is
        GET/HEAD only (a write method -> 405) (SPEC/WEB.md § Roadmap Audit Log
        Page, path parameters; Routes and Pages, status mapping)."""
        proc, port = self._start(["--port", "0"])
        # Invalid name (uppercase), encoded traversal, and nonexistent -> 404.
        assert self._req(port, "/roadmaps/INVALID/audit")[0] == 404
        assert self._req(port, "/roadmaps/..%2fetc/audit")[0] == 404
        assert self._req(port, "/roadmaps/no_such_roadmap/audit")[0] == 404
        # Non-read methods -> 405 on the registered audit route.
        for method in ("POST", "PUT", "PATCH", "DELETE"):
            status, _, _ = self._req(port, f"/roadmaps/{ROADMAP}/audit", method=method)
            assert status == 405, f"{method} audit route must be 405, got {status}"

    def test_audit_page_cache_control_no_store(self):
        """The audit response is data-derived, so it carries Cache-Control:
        no-store, ensuring a freshly read audit log is never served stale
        (SPEC/WEB.md § Cache Policy)."""
        proc, port = self._start(["--port", "0"])
        _, headers, _ = self._req(port, f"/roadmaps/{ROADMAP}/audit")
        assert headers.get("cache-control") == "no-store", (
            "the audit page must carry Cache-Control: no-store"
        )

    def test_audit_page_read_writes_no_audit_entry(self):
        """Reading the audit log writes no row and produces no new audit entry —
        a read is not a change (SPEC/WEB.md § Roadmap Audit Log Page,
        read-only)."""
        before = json.loads(self._run(["audit", "stats", "-r", ROADMAP])[1]).get(
            "total_entries"
        )
        proc, port = self._start(["--port", "0"])
        for page in (1, 2, 99999, 0):
            assert self._req(port, f"/roadmaps/{ROADMAP}/audit?page={page}")[0] == 200
        after = json.loads(self._run(["audit", "stats", "-r", ROADMAP])[1]).get(
            "total_entries"
        )
        assert before == after, (
            f"reading the audit page changed the audit log: {before} -> {after}"
        )

    # ====================================================================
    # AC10/AC11/AC12: sprint tabs, classification + ordering, sprint links
    # ====================================================================

    def test_detail_sprint_tabs_labels_and_default(self):
        """AC10: three tabs labelled Próximos / Actual / Concluídos, left to
        right, with Actual active by default on load."""
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}")

        # The exact Portuguese labels appear in the required left-to-right order.
        i_prox = body.find(">Próximos")
        i_actual = body.find(">Actual")
        i_concl = body.find(">Concluídos")
        assert -1 < i_prox < i_actual < i_concl, (
            "sprint tabs must read Próximos, Actual, Concluídos left-to-right; "
            f"offsets prox={i_prox} actual={i_actual} concl={i_concl}"
        )

        # Actual is the active/default tab: its link is the only one marked
        # active + aria-selected="true".
        assert re.search(
            r'href="#tab-current"[^>]*\bclass="nav-link active"[^>]*aria-selected="true">Actual',
            body,
        ), "the Actual tab must be active and aria-selected by default"
        assert body.count('aria-selected="true"') == 1, (
            "exactly one tab (Actual) may be aria-selected by default"
        )
        # And its pane is the shown/active pane.
        assert '<div id="tab-current" class="tab-pane active show"' in body, (
            "the Actual tab pane must be the active/shown pane by default"
        )

    def test_detail_sprint_classification_and_links(self):
        """AC11/AC12: sprints are classified by status into the right tab and
        each links to its own page."""
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}")

        # Every sprint links to its page.
        for sid in (self.pending_sid, self.open_sid, self.closed_sid):
            assert f"/roadmaps/{ROADMAP}/sprints/{sid}" in body, (
                f"detail page must link sprint #{sid} to its page"
            )

        # Slice the three panes apart so a link is asserted in the RIGHT pane.
        def pane(marker):
            start = body.index(marker)
            rest = body[start + len(marker):]
            nxt = rest.find('<div id="tab-')
            return rest if nxt < 0 else rest[:nxt]

        current = pane('<div id="tab-current"')
        upcoming = pane('<div id="tab-upcoming"')
        closed = pane('<div id="tab-closed"')

        # PENDING -> Próximos, OPEN -> Actual, CLOSED -> Concluídos, each rendered
        # through the shared sprint-card partial.
        assert f"/sprints/{self.pending_sid}" in upcoming, "PENDING sprint not under Próximos"
        assert f"/sprints/{self.open_sid}" in current, "OPEN sprint not under Actual"
        assert f"/sprints/{self.closed_sid}" in closed, "CLOSED sprint not under Concluídos"

        # The Actual tab shows the OPEN sprint as a card (header + task count),
        # NOT an expanded member-task list (SPEC/WEB.md § Shared Sprint-Card
        # Partial; Acceptance Criteria 8/12/38).
        assert 'class="card card-sm card-link text-reset"' in current, (
            "Actual tab must render the OPEN sprint through the shared sprint-card partial"
        )
        assert "passwordless login" not in current.lower(), (
            "Actual tab must not expand the OPEN sprint into an inline task list"
        )
        assert "data-bs-target=\"#task-modal-" not in current, (
            "Actual tab must render no per-task modal trigger"
        )
        assert "task(s)" in current, "Actual tab card must show the sprint's task count"

    # ====================================================================
    # AC13: sprint page — all details, task order, 404/405 rules
    # ====================================================================

    def test_sprint_page_shows_all_details_and_task_order(self):
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")

        # All sprint detail fields are present.
        for field in ("Status", "Capacity", "Created", "Started", "Closed", "Tasks"):
            assert field in body, f"sprint page missing field {field!r}"
        assert f"Sprint #{self.open_sid}" in body, "sprint page missing the sprint id"
        assert "authentication hardening sprint" in body.lower(), (
            "sprint page missing the sprint description"
        )

        # The member tasks are listed in sprint_tasks (execution) order: t1 was
        # added before t2.
        t1, t2 = self.open_task_ids
        low = body.lower()
        i1 = low.find("passwordless login")
        i2 = low.find("rate-limit the token endpoint")
        assert i1 != -1 and i2 != -1, "sprint page must list both member tasks"
        assert i1 < i2, (
            f"sprint page tasks out of execution order: task #{t1} must precede task #{t2}"
        )

        # Read-only: no edit affordance.
        assert "<form" not in body.lower(), "sprint page must contain no form"
        assert "<input" not in body.lower(), "sprint page must contain no input"
        assert not re.search(r'method=["\']?(post|put|patch|delete)', body, re.I), (
            "sprint page must not submit any change"
        )

    def test_sprint_page_not_found_and_method_rules(self):
        proc, port = self._start(["--port", "0"])
        # Non-integer id -> 404.
        assert self._req(port, f"/roadmaps/{ROADMAP}/sprints/abc")[0] == 404
        # Valid-but-nonexistent id -> 404.
        assert self._req(port, f"/roadmaps/{ROADMAP}/sprints/999999")[0] == 404
        # Invalid / nonexistent roadmap name -> 404.
        assert self._req(port, "/roadmaps/INVALID/sprints/1")[0] == 404
        assert self._req(port, "/roadmaps/no_such_roadmap/sprints/1")[0] == 404
        # Non-read method on the sprint route -> 405.
        for method in ("POST", "PUT", "PATCH", "DELETE"):
            status, _, _ = self._req(
                port, f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}", method=method
            )
            assert status == 405, f"{method} sprint route must be 405, got {status}"

    # ====================================================================
    # Sprint page member-tasks board: the three-column Kanban board
    # (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4; Acceptance
    # Criteria 130 to 139)
    # ====================================================================

    # The three fixed board columns, left to right, exactly as
    # SPEC/WEB.md § Sprint Detail Sub-Template, rule 4 spells them — the SAME
    # categorisation the sprint status summary line groups its P/A/C by
    # (Acceptance Criterion 130).
    SPRINT_BOARD_COLUMNS = ("WAITING", "DOING", "CLOSED")

    @staticmethod
    def _sprint_summary(body):
        """Parse the sprint status summary line into its five components.

        Read from data-role="sprint-summary" in the format
        `<pct>% - P:<p> A:<a> C:<c> - T:<t>` (SPEC/WEB.md § Sprint Detail
        Sub-Template, rule 3). Returns a dict of ints so a caller can compare
        the board's own column badges against these SAME numbers rather than
        against a count it derives independently (Acceptance Criterion 131).
        """
        m = re.search(
            r'<div class="h3 mb-3" data-role="sprint-summary">'
            r'(\d+)% - P:(\d+) A:(\d+) C:(\d+) - T:(\d+)</div>',
            body,
        )
        assert m, "the sprint page carries no sprint status summary line"
        pct, p, a, c, t = (int(x) for x in m.groups())
        return {"pct": pct, "p": p, "a": a, "c": c, "t": t}

    @staticmethod
    def _sprint_board_region(body):
        """Return the member-tasks board's own markup.

        Bounded between the board's opening tag and the Comments card that
        follows it directly (SPEC/WEB.md § Sprint Detail Sub-Template, rule 2:
        the board sits between the Sprint details card and the Comments card),
        the same start-marker-to-next-feature slicing idiom
        _slice_sprint_comments_card already uses, in reverse.
        """
        start = body.index('data-role="task-board">')
        end = body.index('<h3 class="card-title">Comments', start)
        return body[start:end]

    @classmethod
    def _sprint_board_columns(cls, body):
        """Return (region, columns): the board's own markup and its three
        columns, left to right, in rendered order (Acceptance Criterion 130,
        Every column is always rendered)."""
        region = cls._sprint_board_region(body)
        columns = region.split('data-role="task-board-column"')[1:]
        assert len(columns) == 3, (
            f"the sprint's member-tasks board renders {len(columns)} columns, want 3"
        )
        return region, columns

    @staticmethod
    def _sprint_column_shows_empty_state(column):
        """Whether a sprint-board column renders its in-column empty state.

        Unlike the tasks board's column-empty element, which is always present
        and toggled via a `hidden` attribute (because a client-side search can
        empty a column there), the sprint board carries no narrowing control of
        any kind: the element is rendered at all only when the column holds no
        card (SPEC/WEB.md § Sprint Detail Sub-Template, rule 4, Every column is
        always rendered).
        """
        return 'data-role="task-board-column-empty"' in column

    @staticmethod
    def _sprint_board_card_ids(column):
        """Return the task ids of a sprint-board column's cards, in card
        (= document) order."""
        return [
            int(m) for m in re.findall(
                r'<button type="button" class="card card-sm task-card[^>]*'
                r'data-task-id="(\d+)"',
                column,
            )
        ]

    @staticmethod
    def _sprint_board_card_html(region, task_id):
        """Return one board card's full markup, from its opening <button> to its
        closing </button>.

        Both Kanban boards emit the same card element, so this reads a card of
        either: the sprint's member-tasks board, and the roadmap tasks page's
        board when one is needed as a control.
        """
        m = re.search(
            rf'<button type="button" class="card card-sm task-card[^>]*'
            rf'data-task-id="{task_id}"[^>]*>.*?</button>',
            region, re.S,
        )
        assert m, f"no board card found for task #{task_id}"
        return m.group(0)

    @staticmethod
    def _span_with_role(html, role):
        """Return the whole <span> carrying data-role="<role>", children
        included, or None when the markup holds none.

        A card's parts are all <span> elements — a button's content model is
        phrasing content — and they NEST, so the slice is taken by balancing the
        tags. Matching the first `</span>` instead would cut a group after its
        first child and make every "the group holds X" assertion pass or fail by
        accident.
        """
        at = html.find(f'data-role="{role}"')
        if at < 0:
            return None
        start = html.rfind("<span", 0, at)
        assert start >= 0, f'the element carrying data-role="{role}" is not a span'

        rest, depth, i = html[start:], 0, 0
        while i < len(rest):
            if rest.startswith("<span", i):
                depth += 1
                i += len("<span")
            elif rest.startswith("</span>", i):
                depth -= 1
                i += len("</span>")
                if depth == 0:
                    return rest[:i]
            else:
                i += 1
        raise AssertionError(f'the element carrying data-role="{role}" is not closed')

    def test_sprint_board_groups_all_five_statuses_into_three_columns(self):
        """AC130/AC131: the member-tasks board renders exactly three columns —
        WAITING, DOING, CLOSED, left to right — each holding the sprint's own
        tasks of the statuses assigned to it, and each column's badge equals
        the summary line's own P/A/C.

        The fixture seeds one member task per TaskStatus value (BACKLOG,
        SPRINT, DOING, TESTING, COMPLETED) so the two-statuses-per-column
        grouping is actually exercised rather than merely assumed: a board that
        miscategorised even one status would print a count that disagrees with
        the summary line it is required to match.

        AC131 is explicit that the check compares the two renderings of ONE
        sprint against EACH OTHER, rather than each against a number this test
        computes on its own: the property under test is that the board and the
        summary line partition the sprint's tasks by the SAME categorisation,
        and a board that grouped the statuses differently could still print
        three counts that each looked plausible in isolation. So P/A/C/T are
        read from the summary line here, and the column badges are compared
        against THOSE values — never against a count this test derives
        independently from the tasks it created.
        """
        roadmap = "webhook_delivery_demo"
        self._run(["roadmap", "create", roadmap])

        def task(title, priority, severity):
            return self.test.create_task(
                roadmap, title,
                "Webhook subscribers must receive each event exactly once",
                "Retry failed deliveries with exponential backoff and a "
                "dead-letter queue after the retry budget is exhausted",
                "A subscriber outage of under ten minutes loses no event",
                priority=priority, severity=severity,
            )

        t_backlog = task("Design the dead-letter queue schema", 3, 2)
        t_sprint = task("Add exponential backoff to the retry worker", 5, 3)
        t_doing = task("Instrument delivery latency per subscriber", 6, 4)
        t_testing = task("Load-test the retry worker at ten times volume", 7, 5)
        t_completed = task("Cap the retry count at eight attempts", 4, 2)
        all_ids = [t_backlog, t_sprint, t_doing, t_testing, t_completed]

        sprint_id = self.test.create_sprint(roadmap, "Webhook reliability sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id)]
                  + [str(i) for i in all_ids])
        self._run(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # BACKLOG: a completed pipeline run reopened straight back to BACKLOG,
        # remaining a member of the sprint throughout (SPEC/STATE_MACHINE.md
        # § Manual Transitions, task stat BACKLOG is accepted from SPRINT).
        self._run(["task", "stat", "-r", roadmap, str(t_backlog), "BACKLOG"])
        # t_sprint is left untouched: SPRINT is its status by construction.
        self._run(["task", "stat", "-r", roadmap, str(t_doing), "DOING", "--commit-open", "6c8064a"])
        self._run(["task", "stat", "-r", roadmap, str(t_testing), "DOING", "--commit-open", "021fa2f"])
        self._run(["task", "stat", "-r", roadmap, str(t_testing), "TESTING"])
        self._run(["task", "stat", "-r", roadmap, str(t_completed), "DOING", "--commit-open", "abd481c"])
        self._run(["task", "stat", "-r", roadmap, str(t_completed), "TESTING"])
        self._run(["task", "stat", "-r", roadmap, str(t_completed), "COMPLETED", "--commit-close", "d1e8dec"])

        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        assert status == 200

        summary = self._sprint_summary(body)
        assert summary["t"] == 5, (
            f"the sprint's own summary line must count all 5 member tasks, got {summary}"
        )

        region, columns = self._sprint_board_columns(body)
        headings = [self._column_header(c)[0] for c in columns]
        assert headings == list(self.SPRINT_BOARD_COLUMNS), (
            f"the board's columns read {headings}, want "
            f"{list(self.SPRINT_BOARD_COLUMNS)} left to right"
        )

        waiting, doing, closed = columns
        waiting_count = self._column_header(waiting)[1]
        doing_count = self._column_header(doing)[1]
        closed_count = self._column_header(closed)[1]

        # AC131: the board's own badges against the summary line's own P/A/C —
        # the two renderings of one sprint compared against each other.
        assert waiting_count == summary["p"], (
            f"WAITING badge {waiting_count} must equal the summary line's P {summary['p']}"
        )
        assert doing_count == summary["a"], (
            f"DOING badge {doing_count} must equal the summary line's A {summary['a']}"
        )
        assert closed_count == summary["c"], (
            f"CLOSED badge {closed_count} must equal the summary line's C {summary['c']}"
        )
        assert waiting_count + doing_count + closed_count == summary["t"], (
            "the three column badges must sum to the summary line's T"
        )

        # Every member task appears on the board exactly once, in the column of
        # the bucket its OWN status maps to — never dropped, never duplicated.
        placement = {
            t_backlog: waiting, t_sprint: waiting,
            t_doing: doing, t_testing: doing,
            t_completed: closed,
        }
        for task_id, column in placement.items():
            marker = f'data-task-id="{task_id}"'
            assert region.count(marker) == 1, (
                f"task #{task_id} appears {region.count(marker)} times on the "
                f"board, want exactly 1"
            )
            assert marker in column, (
                f"task #{task_id} is not in the column its status maps to"
            )

        # The concrete numbers this fixture was built to produce: two WAITING
        # (BACKLOG + SPRINT), two DOING (DOING + TESTING), one CLOSED.
        assert (waiting_count, doing_count, closed_count) == (2, 2, 1), (
            f"got ({waiting_count}, {doing_count}, {closed_count}), want (2, 2, 1)"
        )

        # No table anywhere on this page, and no row-based markup of any kind.
        assert "<table" not in body, "the sprint page must render no task table"
        assert "task-row" not in body, "the sprint page must render no table-row markup"

    def test_sprint_board_each_column_orders_by_its_own_key(self):
        """AC132: the three columns of the sprint's member-tasks board do not
        share one order. WAITING follows the `sprint_tasks` position order (the
        plan), DOING follows `started_at` descending, and CLOSED follows
        `closed_at` descending. Cards of one column carrying the same ordering
        timestamp fall back to position ascending, and a card carrying none
        sorts last in its column.

        All three orders are asserted, and both halves of the split the
        criterion names are asserted too: reordering the sprint through the CLI
        reorders the WAITING column AND leaves DOING and CLOSED exactly as they
        were. Each half on its own is satisfied by a board that got the rule
        wrong — a board ordering all three columns by position satisfies the
        first, a board ordering all three by recency satisfies the second — so
        neither is evidence without the other.

        Nothing here can pass by coincidence. In every column the position
        order, the ordering-timestamp order and the task id order differ from
        one another, and the test states those alternatives explicitly and
        checks the expected order against them.

        Two of the cases are produced by the CLI itself. The TIE is a bulk
        `rmp task stat <id>,<id> DOING`, which stamps a whole batch alike — the
        reason the specification calls equal timestamps ordinary — and in both
        tied pairs the id order is the reverse of the position order, so a board
        tiebreaking on the id fails. The TESTING card carries the newest
        `tested_at` in the fixture, so a board that ordered it by `tested_at`
        would put it at the head of the DOING column instead of third.

        The ABSENT timestamp cannot come from the CLI: the task state machine
        stamps `started_at` on SPRINT -> DOING and `closed_at` on
        TESTING -> COMPLETED and offers no route to a DOING task without the
        first or a COMPLETED task without the second (SPEC/STATE_MACHINE.md
        § Date Tracking Fields). The two fields are nullable all the same
        (SPEC/MODELS.md § Task) and the specification states where a card
        carrying neither sorts, so the fixture writes those two NULLs, and the
        two older timestamps it needs, straight into the roadmap database with
        sqlite3 before the server is started. That is a fixture write and
        nothing else: every status change below travels the CLI.
        """
        roadmap = "checkout_latency_demo"
        self._run(["roadmap", "create", roadmap])

        def task(title, priority, severity):
            return self.test.create_task(
                roadmap, title,
                "Checkout must complete within the latency budget the "
                "merchants were promised",
                "Measured at the checkout endpoint and enforced in the "
                "storefront release gate",
                "The checkout endpoint stays inside its latency budget for a "
                "full trading day",
                priority=priority, severity=severity,
            )

        # Creation order fixes the ID order, and it interleaves the three
        # columns so no column's id order can coincide with its position order.
        w_alpha = task("Publish the checkout latency budget to the storefront team", 9, 2)
        d_alpha = task("Cache the merchant tax rules at the edge", 8, 6)
        c_alpha = task("Add a latency histogram to the checkout endpoint", 6, 1)
        w_beta = task("Backfill the latency history into the reliability warehouse", 5, 7)
        d_beta = task("Move the fraud check off the checkout critical path", 3, 9)
        c_beta = task("Retire the synchronous currency-rate lookup", 2, 4)
        w_gamma = task("Agree the checkout latency service level with the merchants", 4, 5)
        d_gamma = task("Batch the inventory reservation calls", 7, 3)
        c_gamma = task("Split the checkout database read replica", 1, 8)
        d_delta = task("Trim the checkout page's blocking script payload", 6, 6)
        c_delta = task("Compress the checkout API response payloads", 3, 2)

        sprint_id = self.test.create_sprint(
            roadmap, "Bring checkout back inside its latency budget")

        # Membership order fixes the POSITION order, which matches no column's
        # id order and, in DOING and CLOSED, no column's timestamp order.
        members = [w_gamma, d_delta, c_beta, w_alpha, d_beta, c_delta,
                   w_beta, d_alpha, c_alpha, d_gamma, c_gamma]
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id)]
                  + [str(i) for i in members])
        self._run(["sprint", "start", "-r", roadmap, str(sprint_id)])

        def stat(ids, status):
            self._run(["task", "stat", "-r", roadmap,
                       ",".join(str(i) for i in ids), status]
                      + commit_flags_for(status))

        # DOING column. The first call is the bulk one: d_alpha and d_beta enter
        # DOING in a single `task stat` and therefore carry one and the same
        # started_at, which is the latest in the column. d_gamma goes on to
        # TESTING last of all, so its tested_at is the newest timestamp in the
        # fixture while its started_at is rewritten to an older instant below.
        stat([d_alpha, d_beta], "DOING")
        stat([d_delta], "DOING")
        stat([d_gamma], "DOING")
        stat([d_gamma], "TESTING")

        # CLOSED column, through the full lifecycle. c_alpha and c_delta are
        # completed in one bulk call and share a closed_at.
        stat([c_alpha, c_beta, c_gamma, c_delta], "DOING")
        stat([c_alpha, c_beta, c_gamma, c_delta], "TESTING")
        stat([c_gamma], "COMPLETED")
        stat([c_beta], "COMPLETED")
        stat([c_alpha, c_delta], "COMPLETED")

        # The two older timestamps and the two absent ones, written directly
        # into the fixture database — see the docstring for why the CLI cannot
        # produce them.
        db_path = Path(self.home) / ".roadmaps" / roadmap / "project.db"
        connection = sqlite3.connect(str(db_path))
        try:
            cursor = connection.cursor()
            cursor.execute("UPDATE tasks SET started_at = ? WHERE id = ?",
                           ("2026-02-11T08:15:00.000Z", d_gamma))
            cursor.execute("UPDATE tasks SET started_at = NULL WHERE id = ?", (d_delta,))
            cursor.execute("UPDATE tasks SET closed_at = ? WHERE id = ?",
                           ("2026-01-20T17:40:00.000Z", c_gamma))
            cursor.execute("UPDATE tasks SET closed_at = NULL WHERE id = ?", (c_beta,))
            connection.commit()
        finally:
            connection.close()

        # What each column must render, and the two orders it must NOT render.
        want = [
            [w_gamma, w_alpha, w_beta],
            [d_beta, d_alpha, d_gamma, d_delta],
            [c_delta, c_alpha, c_gamma, c_beta],
        ]
        by_position = [
            [w_gamma, w_alpha, w_beta],
            [d_delta, d_beta, d_alpha, d_gamma],
            [c_beta, c_delta, c_alpha, c_gamma],
        ]
        by_id = [
            [w_alpha, w_beta, w_gamma],
            [d_alpha, d_beta, d_gamma, d_delta],
            [c_alpha, c_beta, c_gamma, c_delta],
        ]
        headings = ["WAITING", "DOING", "CLOSED"]

        # The controls. WAITING is specified to follow the position order, so it
        # is exempt from the first check and not from the second.
        for i in (1, 2):
            assert want[i] != by_position[i], (
                f"the fixture's {headings[i]} order {want[i]} is also its position "
                f"order; the assertion would pass on a board that ordered every "
                f"column by position"
            )
        for i in range(3):
            assert want[i] != by_id[i], (
                f"the fixture's {headings[i]} order {want[i]} is also its id order; "
                f"the assertion would pass on a board that lost the read's order"
            )

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        _, columns = self._sprint_board_columns(body)
        got = [self._sprint_board_card_ids(c) for c in columns]
        for i in range(3):
            assert got[i] == want[i], (
                f"as the sprint was planned, the {headings[i]} column renders "
                f"{got[i]}, want {want[i]}"
            )

        # The tie falls back to the plan, and the id order would invert it.
        assert got[1].index(d_beta) < got[1].index(d_alpha), (
            "the two DOING cards tied on started_at must fall back to position "
            "ascending, which puts the later-created d_beta above d_alpha"
        )
        assert got[2].index(c_delta) < got[2].index(c_alpha), (
            "the two CLOSED cards tied on closed_at must fall back to position "
            "ascending, which puts the later-created c_delta above c_alpha"
        )

        # A card whose ordering timestamp is absent sorts last, and neither of
        # the two is last by position, so "last" is the rule and not the plan
        # showing through.
        assert got[1][-1] == d_delta, (
            f"the DOING card carrying no started_at must sort last, got {got[1]}"
        )
        assert got[2][-1] == c_beta, (
            f"the CLOSED card carrying no closed_at must sort last, got {got[2]}"
        )
        assert by_position[1][-1] != d_delta and by_position[2][-1] != c_beta, (
            "the cards carrying no ordering timestamp must not be last by "
            "position either, or the assertion above proves nothing"
        )

        # tested_at orders nothing: the TESTING card carries the newest
        # timestamp in the fixture and still sits where started_at puts it.
        assert got[1][0] == d_beta, (
            f"the DOING column must be headed by the most recently STARTED task, "
            f"got #{got[1][0]}; a TESTING card takes its place from started_at "
            f"and never from tested_at"
        )

        # The split. The new plan moves every card in all three columns.
        reordered = [w_beta, d_beta, c_delta, w_alpha, d_delta, c_beta,
                     w_gamma, d_alpha, c_alpha, d_gamma, c_gamma]
        want_after = [
            [w_beta, w_alpha, w_gamma],
            [d_beta, d_alpha, d_gamma, d_delta],
            [c_delta, c_alpha, c_gamma, c_beta],
        ]
        by_position_after = [
            [w_beta, w_alpha, w_gamma],
            [d_beta, d_delta, d_alpha, d_gamma],
            [c_delta, c_beta, c_alpha, c_gamma],
        ]
        # The reorder must change what a position-ordered board would show in
        # DOING and CLOSED too, or "those two did not move" is a statement about
        # the reorder rather than about the board.
        for i in (1, 2):
            assert by_position_after[i] != by_position[i], (
                f"the reorder leaves the {headings[i]} column's position order "
                f"unchanged, so the assertion below would prove nothing"
            )
        assert want_after[0] != want[0], (
            "the reorder leaves the WAITING column unchanged, so the first half "
            "of the split would prove nothing"
        )

        self._run(["sprint", "reorder", "-r", roadmap, str(sprint_id),
                   ",".join(str(i) for i in reordered)])

        _, _, body2 = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        _, columns2 = self._sprint_board_columns(body2)
        got_after = [self._sprint_board_card_ids(c) for c in columns2]
        for i in range(3):
            assert got_after[i] == want_after[i], (
                f"after `sprint reorder`, the {headings[i]} column renders "
                f"{got_after[i]}, want {want_after[i]}"
            )

    def test_sprint_board_card_shows_six_data_points_in_order(self):
        """AC133: each card shows exactly six data points, on THREE lines, in
        this order: the task title leading the card, the `#<id>` reference as
        secondary text, and one line carrying the `P<n>` priority badge and the
        `S<n>` severity badge at its leading edge and the comment count followed
        by the subtask count at its trailing edge, each counter as an icon
        followed by its number.

        The COUNTER ORDER is asserted rather than left implicit, because the
        criterion requires it: a card showing the subtask count before the
        comment count satisfies every other clause. It is also the reverse of
        the roadmap tasks page's footer order, which that card keeps.

        The task carries real subtasks and real comments so both counters
        have something to render, and priority 7 / severity 6 fall
        in different colour bands (red / orange per badge.go's
        priorityBadge/severityBadge), so the badges are shown to carry the
        semantic mapping's own colours and not just the bare prefixed digits
        (SPEC/WEB.md § Roadmap Tasks Page, Card content, item 3 — the
        badge-prefix rule binds on both boards' cards; Acceptance Criterion
        133).
        """
        roadmap = "fraud_review_demo"
        self._run(["roadmap", "create", roadmap])
        parent = self.test.create_task(
            roadmap,
            "Escalate high-velocity card testing to manual review",
            "A burst of small authorizations from one card must page a "
            "fraud reviewer before the card is used for a large purchase",
            "Flag the card and route its next authorization to manual review",
            "A reviewer sees the flagged card within two minutes of the burst",
            priority=7, severity=6,
        )
        for sub_title in (
            "Define the burst-detection threshold",
            "Wire the flagged card into the manual-review queue",
        ):
            self._run(["task", "create", "-r", roadmap, "-t", sub_title,
                       "-fr", "Needed to detect and route a testing burst",
                       "-tr", "Implement as part of the fraud review pipeline",
                       "-ac", "The parent task's acceptance criteria are met",
                       "--parent", str(parent)])
        for body_text in (
            "Three authorizations under two dollars within ninety seconds "
            "from the same card.",
            "The reviewer queue currently has no SLA; adding one is out of "
            "scope for this task.",
            "Confirmed with the risk team: the threshold is three "
            "authorizations in one hundred twenty seconds.",
        ):
            self._run(["task", "comment-add", "-r", roadmap, str(parent),
                       "--type", "NOTE", "--body", body_text])

        sprint_id = self.test.create_sprint(roadmap, "Card-testing detection sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(parent)])

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        region, columns = self._sprint_board_columns(body)
        card = self._sprint_board_card_html(columns[0], parent)

        title = self._rendered_task_title(body, parent)
        pattern = (
            rf'data-role="task-card-title">{re.escape(title)}</span>\s*'
            rf'<span class="d-block small text-secondary mb-1" '
            rf'data-role="task-card-ref">#{parent}</span>\s*'
            r'<span class="d-flex flex-wrap align-items-center '
            r'justify-content-between gap-1" data-role="task-card-summary">\s*'
            r'<span class="d-flex flex-wrap gap-1" '
            r'data-role="task-card-badges">\s*'
            r'<span class="badge bg-red-lt">P7</span>\s*'
            r'<span class="badge bg-orange-lt">S6</span>\s*'
            r'</span>\s*'
            r'<span class="d-flex flex-wrap gap-2 small text-secondary" '
            r'data-role="task-card-counters">\s*'
            r'<span data-role="task-card-comments">'
            r'<i class="ti ti-message me-1"></i>3</span>\s*'
            r'<span data-role="task-card-subtasks">'
            r'<i class="ti ti-subtask me-1"></i>2</span>\s*'
            r'</span>\s*'
            r'</span>'
        )
        assert re.search(pattern, card, re.S), (
            f"the card's six data points are missing or out of the required "
            f"order: {card}"
        )

        # The order of the two counters, asserted on its own and not only
        # through the pattern above: the criterion singles it out because a card
        # showing the subtask count first satisfies every other clause, and a
        # pattern that drifted would take this with it.
        assert (card.index('data-role="task-card-comments"')
                < card.index('data-role="task-card-subtasks"')), (
            f"the sprint card's counters read subtask count first; the comment "
            f"count leads the pair on this board, which is the reverse of the "
            f"roadmap tasks page's footer order: {card}"
        )

        # Exactly six data points: no seventh. No status badge (the column
        # already states it), no type, no specialists, no dependency counts,
        # no sprint name.
        assert card.count('class="badge') == 2, (
            f"the card must carry exactly two badges (P and S); found "
            f"{card.count('class=\"badge')} in {card}"
        )
        for absent in ("task-card-sprint", "task-card-specialists",
                       "task-card-depends-on", "task-card-blocks"):
            assert absent not in card, (
                f"the sprint board's card must not render {absent!r}: {card}"
            )

    def test_sprint_board_card_merges_badges_and_counters_onto_one_line(self):
        """AC133: the badges and the counters share ONE line — the badges at its
        leading edge, the counters at its trailing edge — the line wraps inside
        the card instead of overflowing it on a narrow column, and the card
        renders no separate footer row for the counters.

        This is the card's SHAPE rather than its contents, which
        test_sprint_board_card_shows_six_data_points_in_order asserts. The
        layout is read from the utility classes the line carries, because those
        are what the browser resolves the behaviour from: justify-content-between
        puts the first flex item at the leading edge and the last at the
        trailing one, and flex-wrap turns "too narrow to hold both" into a wrap
        rather than an overflow — a wrapped flex line holding one item resolves
        space-between to flex-start, so the counters drop directly below the
        badges inside the same card.

        The roadmap tasks page's card is asserted UNCHANGED in the same test,
        because "this board renders no metadata footer" states nothing unless
        the other board still renders one: a template that had dropped the
        footer from both cards would satisfy every absence assertion here.
        """
        roadmap = "settlement_layout_demo"
        self._run(["roadmap", "create", roadmap])
        member = self.test.create_task(
            roadmap,
            "Reconcile the acquirer settlement file against the ledger",
            "Every settled line must be matched to a ledger entry before the "
            "nightly window closes",
            "Replay the acquirer file against the ledger and report residuals",
            "The nightly reconciliation reports no unexplained residual",
            priority=9, severity=2,
        )
        self._run(["task", "create", "-r", roadmap,
                   "-t", "Match settlement lines to ledger entries by reference",
                   "-fr", "The match must be reproducible line by line",
                   "-tr", "Implement inside the reconciliation pipeline",
                   "-ac", "The parent task's acceptance criteria are met",
                   "--parent", str(member)])
        self._run(["task", "comment-add", "-r", roadmap, str(member),
                   "--type", "DECISION",
                   "--body", "The ledger is authoritative; the acquirer file "
                             "is replayed against it, never the reverse."])

        sprint_id = self.test.create_sprint(roadmap, "Settlement reconciliation sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(member)])

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        region, columns = self._sprint_board_columns(body)
        card = self._sprint_board_card_html(columns[0], member)

        # The line's OWN class attribute, not its subtree: both inner groups are
        # flex containers that wrap as well, so a check against the whole card
        # would find d-flex and flex-wrap on a line that carried neither and
        # would pass on markup that overflows the card.
        m = re.search(r'<span class="([^"]*)" data-role="task-card-summary">', card)
        assert m, (
            f"the card renders no line carrying both the badges and the "
            f"counters: {card}"
        )
        line_classes = m.group(1).split()
        for cls, why in (
            ("d-flex", "the two groups share a line only inside a flex container"),
            ("flex-wrap", "without it the line overflows the card instead of wrapping"),
            ("justify-content-between", "it is what puts the counters at the trailing edge"),
            ("align-items-center", "the badges are taller than the counters"),
        ):
            assert cls in line_classes, (
                f"the card's badge-and-counter line does not itself carry "
                f"{cls!r}: {why}; it carries {line_classes}"
            )

        # Both groups are inside that one line, the badges leading it and the
        # counters closing it.
        line = self._span_with_role(card, "task-card-summary")
        assert line, f"the card's badge-and-counter line is not closed: {card}"
        for role in ("task-card-badges", "task-card-counters",
                     "task-card-comments", "task-card-subtasks"):
            assert f'data-role="{role}"' in line, (
                f"{role!r} does not sit on the card's badge-and-counter line: {line}"
            )
        assert (line.index('data-role="task-card-badges"')
                < line.index('data-role="task-card-counters"')), (
            f"the counters precede the badges on the card's line; the badges "
            f"lead it and the counters close it: {line}"
        )
        # Neither group leaks into the other: a single flat row of four spans
        # would satisfy every assertion above and would place nothing at either
        # edge, because justify-content-between spreads FOUR items across the
        # line instead of pinning two groups to its two ends.
        badges = self._span_with_role(line, "task-card-badges")
        counters = self._span_with_role(line, "task-card-counters")
        assert "task-card-comments" not in badges, (
            f"a counter sits inside the badge group: {badges}"
        )
        assert 'class="badge' not in counters, (
            f"a badge sits inside the counter group: {counters}"
        )
        assert (counters.index('data-role="task-card-comments"')
                < counters.index('data-role="task-card-subtasks"')), (
            f"the trailing group reads subtask count first; the comment count "
            f"leads the pair on this board: {counters}"
        )

        # No card of this board renders a separate footer row: not under the
        # tasks board's role, not with that row's trailing-edge alignment, and
        # not with the top margin that separated it from the badges. The board is
        # scanned CARD BY CARD so the guard cannot fail for something a column
        # header emits, and cannot pass because the one card examined is clean.
        for column in columns:
            for card_id in self._sprint_board_card_ids(column):
                each = self._sprint_board_card_html(column, card_id)
                for gone in ('data-role="task-card-meta"',
                             "justify-content-end", "mt-2"):
                    assert gone not in each, (
                        f"the card of task #{card_id} still renders {gone!r}; "
                        f"the counters share the badge line and the card has no "
                        f"separate footer row: {each}"
                    )

        # No inline style anywhere on the board (AC62 continues to hold), and no
        # page-level horizontal overflow is introduced by the merged line: the
        # card's own line is the only place the two groups can compete for
        # width, and it wraps.
        assert 'style="' not in region, (
            f"the member-tasks board carries an inline style attribute: {region}"
        )

        # The control: the roadmap tasks page's card is untouched. It still
        # renders its metadata footer, and that footer still lists the subtask
        # count BEFORE the comment count — the order this board reverses.
        _, _, tasks_body = self._req(port, f"/roadmaps/{roadmap}/tasks")
        tasks_region, _ = self._board_columns(tasks_body)
        assert 'data-role="task-card-meta"' in tasks_region, (
            "the roadmap tasks page's board renders no metadata footer at all, "
            "so asserting the sprint board has none proves nothing"
        )
        assert 'data-role="task-card-summary"' not in tasks_region, (
            "the roadmap tasks page's card grew the sprint card's merged line; "
            "that card keeps its separate metadata footer"
        )
        # The same member task, on the tasks board: one subtask and one comment,
        # so its footer renders both indicators and the order comparison below
        # has two positions to compare.
        tasks_card = self._sprint_board_card_html(tasks_region, member)
        footer = self._span_with_role(tasks_card, "task-card-meta")
        assert footer, f"the tasks board's control card renders no footer: {tasks_card}"
        for role in ("task-card-subtasks", "task-card-comments"):
            assert f'data-role="{role}"' in footer, (
                f"the tasks board's control card renders no {role!r}, so the "
                f"order comparison below is vacuous: {footer}"
            )
        assert (footer.index('data-role="task-card-subtasks"')
                < footer.index('data-role="task-card-comments"')), (
            f"the tasks board's metadata footer now lists the comment count "
            f"before the subtask count; that footer keeps its own order, and "
            f"the sprint card's reversed order is stated separately from it: "
            f"{footer}"
        )

    def test_sprint_board_card_always_renders_both_counters(self):
        """AC134: both counters are present on EVERY card of the member-tasks
        board, including when the number they carry is 0, so the trailing edge
        of every card's third line carries both numbers and every card is the
        same shape.

        The subject is a task with neither a subtask nor a comment, because that
        is the only card the criterion discriminates on: a card that has
        something to count renders the same markup whether the rule holds or
        not. A second task, with two subtasks and one comment, is seeded beside
        it so the two zeros cannot come from a counter group that prints 0
        whatever the task holds.

        Each counter is asserted as its whole indicator markup — the element that
        names it, its icon, and its number — rather than as the digit alone: a
        bare 0 with no icon, or an icon with nothing beside it, would satisfy a
        check for the digit and state nothing to the reader.
        """
        roadmap = "device_enrolment_demo"
        self._run(["roadmap", "create", roadmap])
        bare = self.test.create_task(
            roadmap,
            "Rotate the device-enrolment signing key",
            "The enrolment signing key must be rotated before its "
            "scheduled expiry so no device is locked out",
            "Generate a new key pair and publish the new public key",
            "Newly enrolled devices verify successfully against the "
            "rotated key",
            priority=3, severity=2,
        )
        counted = self.test.create_task(
            roadmap,
            "Publish the device-enrolment trust bundle to the CDN",
            "Enrolled devices must fetch the trust bundle from the edge "
            "rather than from the enrolment service",
            "Sign the bundle and upload it to the CDN origin on each rotation",
            "A freshly enrolled device validates its bundle from the CDN",
            priority=6, severity=4,
        )
        for subtask in (
            "Sign the trust bundle with the rotated enrolment key",
            "Invalidate the CDN cache for the previous trust bundle",
        ):
            self._run(["task", "create", "-r", roadmap, "-t", subtask,
                       "-fr", "The bundle publication must be verifiable step by step",
                       "-tr", "Implement as part of the enrolment trust pipeline",
                       "-ac", "The parent task's acceptance criteria are met",
                       "--parent", str(counted)])
        self._run(["task", "comment-add", "-r", roadmap, str(counted),
                   "--type", "DECISION",
                   "--body", "The bundle is served from the CDN origin, not "
                             "from the enrolment service."])

        sprint_id = self.test.create_sprint(roadmap, "Device trust maintenance sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id),
                   str(bare), str(counted)])

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        region, columns = self._sprint_board_columns(body)

        def counter(role, icon, n):
            return (f'<span data-role="{role}"><i class="ti {icon} me-1">'
                    f'</i>{n}</span>')

        # The card with nothing to count still carries both counters, each
        # showing 0, inside the counter group that closes its third line.
        card = self._sprint_board_card_html(columns[0], bare)
        assert 'data-role="task-card-counters"' in card, (
            f"a task with no subtasks and no comments must still render its "
            f"counter group: {card}"
        )
        for role, icon in (("task-card-comments", "ti-message"),
                           ("task-card-subtasks", "ti-subtask")):
            assert counter(role, icon, 0) in card, (
                f"the card of a task with nothing to count must render "
                f"{counter(role, icon, 0)!r}; a 0 states that the task has "
                f"none, where an absent counter leaves the reader unable to "
                f"tell 'no comments' from 'this card does not show "
                f"comments': {card}"
            )
        # And nothing stands in for the zero: no dash, no placeholder, no word.
        for absent in ("&mdash;", "Subtasks:", "Comments:"):
            assert absent not in card, (
                f"the counter-free card renders {absent!r} instead of the "
                f"number 0: {card}"
            )

        # The control: the second card's counters carry its own numbers, so the
        # zeros above are the data and not a constant.
        other = self._sprint_board_card_html(columns[0], counted)
        assert counter("task-card-comments", "ti-message", 1) in other, (
            f"the card of a commented task must render its own count: {other}"
        )
        assert counter("task-card-subtasks", "ti-subtask", 2) in other, (
            f"the card of a task with two subtasks must render its own count: {other}"
        )

        # Every card of the board carries the pair, which is the property the
        # criterion is written for and which no single card can establish.
        cards = region.count('<button type="button" class="card card-sm task-card')
        assert cards == 2, f"the board renders {cards} cards, want the 2 seeded"
        for role in ("task-card-counters", "task-card-comments", "task-card-subtasks"):
            assert region.count(f'data-role="{role}"') == cards, (
                f"the board renders {cards} cards and "
                f"{region.count(f'data-role=\"{role}\"')} {role!r} elements; "
                f"both counters are present on every card"
            )

    def test_board_column_count_badges_carry_the_colour_of_their_status(self):
        """AC140: every per-column count badge of the TWO Kanban boards carries
        the semantic colour of the status its column groups, while its text
        stays that column's task count.

        On the roadmap tasks page a column is exactly one task status, so its
        badge takes that status's own variant. On the sprint's member-tasks
        board a column groups a SET of statuses, so its badge takes the variant
        of the group's canonical status: SPRINT's for WAITING, DOING's for
        DOING, COMPLETED's for CLOSED.

        Both boards are asserted COLUMN BY COLUMN AND AS A WHOLE, exactly as
        AC120 asserts the three sprint tabs together. BACKLOG maps to
        bg-secondary-lt, which is also the neutral colour a badge carries when
        nothing colours it, so that one column renders identically whether the
        mapping was applied or not: what separates a conforming rendering from a
        non-conforming one is the other columns, and a rendering that gave every
        column of either board bg-secondary-lt must fail. The distinct-variant
        assertions below are that control.
        """
        roadmap = "settlement_recon_demo"
        self._run(["roadmap", "create", roadmap])

        def task(title, priority, severity):
            return self.test.create_task(
                roadmap, title,
                "Every acquirer settlement file must reconcile against the "
                "ledger before the books close",
                "Match settlement lines to ledger entries and raise a break "
                "for any residual",
                "A settlement file with no residual closes without manual work",
                priority=priority, severity=severity,
            )

        t_backlog = task("Document the settlement break escalation path", 3, 2)
        t_sprint = task("Match settlement lines against ledger entries", 8, 6)
        t_doing = task("Publish the daily reconciliation dashboard", 5, 3)
        t_testing = task("Replay a month of settlement files in staging", 6, 4)
        t_completed = task("Version the settlement export schema", 4, 2)
        all_ids = [t_backlog, t_sprint, t_doing, t_testing, t_completed]

        sprint_id = self.test.create_sprint(roadmap, "Settlement reconciliation sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id)]
                  + [str(i) for i in all_ids])
        self._run(["sprint", "start", "-r", roadmap, str(sprint_id)])

        self._run(["task", "stat", "-r", roadmap, str(t_backlog), "BACKLOG"])
        self._run(["task", "stat", "-r", roadmap, str(t_doing), "DOING", "--commit-open", "5d6a2cd"])
        self._run(["task", "stat", "-r", roadmap, str(t_testing), "DOING", "--commit-open", "5f93b51"])
        self._run(["task", "stat", "-r", roadmap, str(t_testing), "TESTING"])
        self._run(["task", "stat", "-r", roadmap, str(t_completed), "DOING", "--commit-open", "2578d18"])
        self._run(["task", "stat", "-r", roadmap, str(t_completed), "TESTING"])
        self._run(["task", "stat", "-r", roadmap, str(t_completed), "COMPLETED", "--commit-close", "4999725"])

        proc, port = self._start(["--port", "0"])

        # ---- the tasks board: one status per column ----
        status_code, _, tasks_body = self._req(port, f"/roadmaps/{roadmap}/tasks")
        assert status_code == 200
        _, tasks_columns = self._board_columns(tasks_body)
        assert len(tasks_columns) == 5

        tasks_variants = set()
        for column, want_status in zip(tasks_columns, self.BOARD_COLUMNS):
            heading, variant, count = self._column_badge(column)
            tasks_variants.add(variant)
            assert heading == want_status, (
                f"the tasks board's column reads {heading!r}, want {want_status!r}"
            )
            want = self.TASK_STATUS_BADGE[want_status]
            assert variant == want, (
                f"the tasks board's {heading} column carries the count badge "
                f"variant {variant!r}, want {want!r} — the variant the semantic "
                f"mapping assigns to that column's own status (AC140)"
            )
            assert count == 1, (
                f"the {heading} column shows the count {count}, want 1; the "
                f"colour changes the badge's variant and nothing about its text"
            )
        assert len(tasks_variants) == 5, (
            f"the tasks board's five column badges carry {len(tasks_variants)} "
            f"distinct variant(s) ({sorted(tasks_variants)}); the five statuses "
            f"map to five different colours, so fewer means the mapping was not "
            f"applied — and a board that gave every column bg-secondary-lt "
            f"conforms on none of them"
        )

        # ---- the sprint board: a set of statuses per column ----
        status_code, _, sprint_body = self._req(
            port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        assert status_code == 200
        _, sprint_columns = self._sprint_board_columns(sprint_body)

        sprint_variants = set()
        for column, heading_want in zip(sprint_columns, self.SPRINT_BOARD_COLUMNS):
            heading, variant, count = self._column_badge(column)
            sprint_variants.add(variant)
            assert heading == heading_want, (
                f"the sprint board's column reads {heading!r}, want {heading_want!r}"
            )
            canonical = self.SPRINT_BOARD_CANONICAL[heading_want]
            want = self.TASK_STATUS_BADGE[canonical]
            assert variant == want, (
                f"the sprint board's {heading} column carries the count badge "
                f"variant {variant!r}, want {want!r} — the variant assigned to "
                f"{canonical}, the canonical status of the group this column "
                f"holds (AC140)"
            )
        assert len(sprint_variants) == 3, (
            f"the sprint board's three column badges carry "
            f"{len(sprint_variants)} distinct variant(s) "
            f"({sorted(sprint_variants)}), want 3; the three canonical statuses "
            f"are three different statuses"
        )

        # The two boards agree where they overlap: the DOING column names the
        # same status on both, so it must carry the same colour on both.
        _, doing_on_tasks, _ = self._column_badge(tasks_columns[2])
        _, doing_on_sprint, _ = self._column_badge(sprint_columns[1])
        assert doing_on_tasks == doing_on_sprint, (
            f"the DOING column carries {doing_on_tasks!r} on the tasks board "
            f"and {doing_on_sprint!r} on the sprint board; both name one status "
            f"and both read one mapping"
        )

        # A narrowed board keeps every column's colour while its counts follow
        # the narrowing (AC101 and AC113 continue to hold).
        _, _, narrowed_body = self._req(
            port, f"/roadmaps/{roadmap}/tasks?q=settlement")
        _, narrowed_columns = self._board_columns(narrowed_body)
        narrowed_total = 0
        for column, want_status in zip(narrowed_columns, self.BOARD_COLUMNS):
            heading, variant, count = self._column_badge(column)
            narrowed_total += count
            assert variant == self.TASK_STATUS_BADGE[want_status], (
                f"the narrowed board's {heading} column carries {variant!r}, "
                f"want {self.TASK_STATUS_BADGE[want_status]!r}; narrowing "
                f"changes what a column counts, never what it stands for"
            )
        assert 0 < narrowed_total < 5, (
            f"the search left {narrowed_total} of 5 tasks showing; it must "
            f"narrow the board without emptying it for the assertion above to "
            f"be about a narrowed board at all"
        )

    def test_board_column_count_badge_keeps_its_colour_when_the_column_is_empty(self):
        """AC140: a column holding no task shows the count 0 and keeps the
        colour of its status, because the colour follows the COLUMN and not the
        cards in it.

        Both boards are checked with nothing in them at all — a roadmap with no
        task and a sprint with no member task — so every column of both is empty
        and there is no card anywhere for a colour to be read from.
        """
        roadmap = "clearing_house_empty_demo"
        self._run(["roadmap", "create", roadmap])
        sprint_id = self.test.create_sprint(roadmap, "Clearing window readiness sprint")

        proc, port = self._start(["--port", "0"])

        _, _, tasks_body = self._req(port, f"/roadmaps/{roadmap}/tasks")
        _, tasks_columns = self._board_columns(tasks_body)
        for column, want_status in zip(tasks_columns, self.BOARD_COLUMNS):
            heading, variant, count = self._column_badge(column)
            assert count == 0, (
                f"the {heading} column of an empty roadmap shows {count}, want 0"
            )
            assert variant == self.TASK_STATUS_BADGE[want_status], (
                f"the empty {heading} column carries {variant!r}, want "
                f"{self.TASK_STATUS_BADGE[want_status]!r}; the colour follows "
                f"the column and not the cards in it"
            )

        _, _, sprint_body = self._req(
            port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        _, sprint_columns = self._sprint_board_columns(sprint_body)
        for column, heading_want in zip(sprint_columns, self.SPRINT_BOARD_COLUMNS):
            heading, variant, count = self._column_badge(column)
            canonical = self.SPRINT_BOARD_CANONICAL[heading_want]
            assert count == 0, (
                f"the {heading} column of an empty sprint shows {count}, want 0"
            )
            assert variant == self.TASK_STATUS_BADGE[canonical], (
                f"the empty {heading} column carries {variant!r}, want "
                f"{self.TASK_STATUS_BADGE[canonical]!r}; a column holding no "
                f"task keeps the colour of the status it groups"
            )

    def test_sprint_page_comments_badge_stays_neutral_beside_a_coloured_board(self):
        """AC140: the boundary of the rule. The Comments card header count on
        the Roadmap Sprint Page counts comments, and a comment carries no status
        of any kind, so the semantic mapping has nothing to key on and the badge
        keeps the neutral bg-secondary-lt.

        It is asserted on the SAME page that carries the coloured board, because
        that is where the distinction lives: the column badges above the card are
        coloured by the status they group, and the card's own count badge below
        them is not. Without the control that at least one column badge on that
        page is NOT neutral, a page on which nothing was coloured at all would
        pass this test.
        """
        roadmap = "cross_border_payouts_demo"
        self._run(["roadmap", "create", roadmap])
        member = self.test.create_task(
            roadmap,
            "Route payouts through the local clearing scheme where available",
            "Cross-border payouts must use a local scheme when the corridor "
            "supports one, and fall back to correspondent banking otherwise",
            "Select the rail per corridor from the scheme availability table",
            "A payout to a supported corridor never leaves through "
            "correspondent banking",
            priority=7, severity=5,
        )
        sprint_id = self.test.create_sprint(roadmap, "Payout corridor coverage sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), str(member)])
        self._run(["sprint", "comment-add", "-r", roadmap, str(sprint_id),
                   "--type", "DECISION",
                   "--body", "Corridors without a local scheme keep the "
                             "correspondent rail until Q3."])

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")

        m = re.search(
            r'<h3 class="card-title">Comments '
            r'<span class="badge (bg-[a-z]+-lt) ms-2">(\d+)</span></h3>',
            body,
        )
        assert m, "the Comments card carries no count badge in the shared header idiom"
        assert m.group(1) == "bg-secondary-lt", (
            f"the Comments card's count badge carries {m.group(1)!r}, want the "
            f"neutral 'bg-secondary-lt'; it counts comments, and a comment has "
            f"no status for the semantic mapping to key on (SPEC/WEB.md "
            f"§ Status, Priority, and Severity Badge Colours, rule 2, The "
            f"discriminating test)"
        )
        assert m.group(2) == "1", (
            f"the Comments badge counts {m.group(2)}, want the sprint's 1 comment"
        )

        # The control: the board above it on the same page IS coloured.
        _, columns = self._sprint_board_columns(body)
        variants = {self._column_badge(c)[1] for c in columns}
        assert variants - {"bg-secondary-lt"}, (
            f"no column badge on this page carries a variant other than "
            f"bg-secondary-lt ({sorted(variants)}), so the Comments card "
            f"looking neutral says nothing about the boundary under test"
        )

    def test_sprint_board_empty_sprint_renders_all_three_columns_empty(self):
        """AC130: a sprint with no member task renders the member-tasks board
        with all three columns present, each showing its own `0` badge and its
        own in-column empty state — never a page-level empty state and never an
        absent board."""
        roadmap = "empty_sprint_demo"
        self._run(["roadmap", "create", roadmap])
        sprint_id = self.test.create_sprint(roadmap, "Not yet staffed sprint")

        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        assert status == 200

        summary = self._sprint_summary(body)
        assert summary == {"pct": 0, "p": 0, "a": 0, "c": 0, "t": 0}, (
            f"an empty sprint's summary line must read all zeros, got {summary}"
        )

        region, columns = self._sprint_board_columns(body)
        for column, heading in zip(columns, self.SPRINT_BOARD_COLUMNS):
            got, count = self._column_header(column)
            assert got == heading, f"column titled {got!r}, want {heading!r}"
            assert count == 0, f"the empty column {got} shows the count {count}, want 0"
            assert self._sprint_column_shows_empty_state(column), (
                f"the empty column {got} renders no in-column empty state"
            )
        assert 'class="card card-sm task-card' not in region, (
            "an empty sprint's board must render no card"
        )
        # The board is framed by the Sprint details card above and the
        # Comments card below, exactly as a populated sprint's is: this is not
        # a page-level empty state substituting for the board.
        assert "Sprint details" in body
        assert '<h3 class="card-title">Comments' in body

    def test_sprint_board_all_member_tasks_in_a_single_column(self):
        """A sprint whose member tasks sit entirely in one column still renders
        all three: the two untouched columns show their `0` badge and their own
        empty state, and the occupied column carries every member task."""
        roadmap = "latency_budget_demo"
        self._run(["roadmap", "create", roadmap])

        def task(title, priority):
            return self.test.create_task(
                roadmap, title,
                "The checkout API's p99 latency budget is 300 milliseconds",
                "Profile the request's hot path and cut its slowest span",
                "p99 latency measured under load stays under 300 milliseconds",
                priority=priority,
            )

        t1 = task("Profile the checkout API's p99 latency", 5)
        t2 = task("Cache the tax-rate lookup", 4)
        t3 = task("Move the fraud check off the request's hot path", 7)

        sprint_id = self.test.create_sprint(roadmap, "Checkout latency sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id),
                   str(t1), str(t2), str(t3)])
        self._run(["sprint", "start", "-r", roadmap, str(sprint_id)])
        for t in (t1, t2, t3):
            self._run(["task", "stat", "-r", roadmap, str(t), "DOING", "--commit-open", "391cff7"])

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        summary = self._sprint_summary(body)
        assert (summary["p"], summary["a"], summary["c"], summary["t"]) == (0, 3, 0, 3), (
            f"expected all 3 member tasks counted under A (DOING), got {summary}"
        )

        region, columns = self._sprint_board_columns(body)
        waiting, doing, closed = columns

        assert self._column_header(waiting)[1] == 0
        assert self._sprint_column_shows_empty_state(waiting)
        assert self._column_header(closed)[1] == 0
        assert self._sprint_column_shows_empty_state(closed)

        assert self._column_header(doing)[1] == 3
        assert not self._sprint_column_shows_empty_state(doing)
        assert set(self._sprint_board_card_ids(doing)) == {t1, t2, t3}, (
            "the DOING column must carry every member task when all three sit "
            "in that one status"
        )

    def test_sprint_description_preserves_line_breaks(self):
        """Authored multi-line sprint descriptions render preserving the line
        breaks (SPEC/WEB.md § Frontend Rules rule 6, Acceptance Criterion 32):
        the description <p> carries the sprint-description class, the served
        HTML keeps the author's newlines verbatim, and the stylesheet applies
        white-space: pre-wrap to that class (and to the task modal text)."""
        desc = "First objective line.\nSecond objective line.\nThird objective line."
        sid = self.test.create_sprint(ROADMAP, desc)
        proc, port = self._start(["--port", "0"])
        # The sprint detail page always shows the full description.
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/sprints/{sid}")
        assert "sprint-description" in body, (
            "the sprint description must carry the line-break-preserving class"
        )
        # html/template passes newlines through unchanged; CSS pre-wrap renders
        # them. The exact multi-line text must survive verbatim in the HTML.
        assert desc in body, "sprint description must preserve the author's line breaks"
        # The stylesheet preserves line breaks for the sprint description class.
        _, _, css = self._req(port, "/static/style.css")
        assert ".sprint-description" in css, "stylesheet must target .sprint-description"
        assert "white-space: pre-wrap" in css, (
            "the stylesheet must preserve authored line breaks (white-space: pre-wrap)"
        )

    def test_task_modal_text_preserves_line_breaks(self):
        """Authored multi-line task free-text renders preserving the line breaks
        in the detail modal (SPEC/WEB.md § Frontend Rules rule 6): the long
        fields sit in .task-modal__text, which the stylesheet renders with
        white-space: pre-wrap, and the served HTML keeps the newlines."""
        multiline_fr = "Step one of the rationale.\nStep two of the rationale."
        self._run(["roadmap", "create", "linebreaks_demo"])
        self.test.create_task(
            "linebreaks_demo",
            "Document the rollout rationale",
            multiline_fr,            # functional requirements: multi-line
            "how", "verify",
        )
        proc, port = self._start(["--port", "0"])

        # The value travels to the browser as JSON, with its newlines intact.
        tasks = json.loads(self._run(["task", "list", "-r", "linebreaks_demo"])[1])
        task_id = tasks[0]["id"]
        _, detail = self._task_detail(port, "linebreaks_demo", task_id)
        assert detail["task"]["functional_requirements"] == multiline_fr, (
            "the endpoint must carry the author's line breaks unchanged"
        )

        # The script writes it into a .task-modal__text block, which the
        # stylesheet renders with white-space: pre-wrap, so the line breaks
        # survive to the screen. The page itself carries no task text any more.
        _, _, script = self._req(port, "/static/task-modal.js")
        assert '"task-modal__text"' in script, (
            "the modal script must use the line-break-preserving class"
        )
        assert "textContent" in script, "the modal script must write values as text"
        _, _, css = self._req(port, "/static/style.css")
        assert ".task-modal__text" in css and "white-space: pre-wrap" in css
        _, _, body = self._req(port, "/roadmaps/linebreaks_demo/tasks")
        assert multiline_fr not in body, (
            "the task free-text must not travel in the page; the modal fetches it"
        )

    def test_graph_detail_panel_preserves_line_breaks(self):
        """The knowledge-graph detail panel preserves authored line breaks in the
        property values it shows (SPEC/WEB.md § Frontend Rules rule 6): the
        client script tags each value element with the detail-panel__value class
        (assigning the value through textContent, never as HTML), and the
        stylesheet renders that class with white-space: pre-wrap. The panel is
        populated by JavaScript, so the contract is verified on the served
        assets that wire it."""
        proc, port = self._start(["--port", "0"])
        _, _, js = self._req(port, "/static/graph.js")
        assert 'dd.className = "detail-panel__value"' in js, (
            "graph.js must tag each detail-panel value element with the "
            "line-break-preserving class"
        )
        assert "dd.textContent = value" in js, (
            "graph.js must assign the property value through textContent, never "
            "as raw HTML"
        )
        _, _, css = self._req(port, "/static/style.css")
        assert ".detail-panel__value" in css, "stylesheet must target .detail-panel__value"
        assert "white-space: pre-wrap" in css, (
            "the stylesheet must preserve authored line breaks (white-space: pre-wrap)"
        )

    # ====================================================================
    # AC14: read-only task detail modal — wiring, content, no edit control
    # ====================================================================

    def test_task_modal_wiring_and_content(self):
        proc, port = self._start(["--port", "0"])
        t1 = self.open_task_ids[0]
        # Both pages that show clickable tasks carry ONE modal shell and wire every
        # trigger to it by task id; the content comes from the task detail
        # endpoint when the user opens a task. The sprints landing page renders
        # compact sprint cards only and opens no task detail modal (SPEC/WEB.md
        # § Task Detail Modal, § Task Detail Endpoint; Acceptance Criteria
        # 8/15/38/96).
        for path in (
            f"/roadmaps/{ROADMAP}/tasks",
            f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}",
        ):
            _, _, body = self._req(port, path)
            assert 'data-bs-toggle="modal"' in body, f"{path}: no modal-toggling control"
            assert 'data-bs-target="#task-modal"' in body, (
                f"{path}: no trigger pointing at the modal shell"
            )
            assert f'data-task-id="{t1}"' in body, f"{path}: missing the trigger for task #{t1}"

            # Exactly one modal element, and no per-task modal.
            assert body.count('id="task-modal"') == 1, (
                f"{path}: carries {body.count('id=\"task-modal\"')} modal shells, want 1"
            )
            assert not re.search(r'id="task-modal-\d+"', body), (
                f"{path}: still renders a per-task modal"
            )
            # No task's modal content travels in the document.
            for absent in (
                "Functional requirements",
                "Technical requirements",
                "Acceptance criteria",
                "end users must authenticate without a stored password",
            ):
                assert absent.lower() not in body.lower(), (
                    f"{path}: carries the modal content {absent!r}; it must be fetched on demand"
                )
            # Read-only: no form and no submit. The tasks page carries exactly one
            # input, the header search box; the sprint page carries none.
            low = body.lower()
            assert "<form" not in low, f"{path}: modal page must contain no form"
            assert 'type="submit"' not in low, f"{path}: modal page must contain no submit"
            want_inputs = 1 if path.endswith("/tasks") else 0
            assert low.count("<input") == want_inputs, (
                f"{path}: carries {low.count('<input')} inputs, want {want_inputs}"
            )

        # And the shell is filled from the endpoint, which carries every field the
        # modal presents.
        status, detail = self._task_detail(port, ROADMAP, t1)
        assert status == 200, f"the task detail endpoint answered {status}"
        for field in (
            "id", "title", "status", "type", "priority", "severity",
            "functional_requirements", "technical_requirements", "acceptance_criteria",
            "completion_summary", "parent_task_id", "subtask_count",
            "depends_on", "blocks", "created_at", "started_at", "tested_at", "closed_at",
        ):
            assert field in detail["task"], f"the task detail carries no {field!r}"
        # The specialists field was removed from the task entity (rmp task
        # #246); the web task-detail endpoint must not carry it either.
        assert "specialists" not in detail["task"], detail["task"]
        assert "end users must authenticate without a stored password" in (
            detail["task"]["functional_requirements"].lower()
        )

    # ====================================================================
    # Comment log: the task detail modal timeline and the sprint Comments card
    # (SPEC/WEB.md § Task Detail Modal, comments timeline; § Sprint Detail
    # Sub-Template, Comments card)
    # ====================================================================

    def _seed_comments(self):
        """Attach a realistic comment log to the OPEN sprint and its first task.

        The bodies belong to this module's authentication domain, so the
        rendered page reads like the project it models. One task comment is
        edited, which is what makes the "edited" stamp reachable; the second
        member task is deliberately left without comments so the modal empty
        state is reachable on the same page.
        """
        t1, t2 = self.open_task_ids
        bodies = {
            "FINDING": "The magic-link token comparison used ==, so a token that "
                       "differed only in its final byte still took a measurable "
                       "amount of time longer to reject.",
            "HYPOTHESIS": "Suspect the token's expiry is compared with After() "
                          "rather than !Before(), which would accept the boundary "
                          "second.",
            "DECISION": "Decided to compare tokens with subtle.ConstantTimeCompare "
                        "and to store only their hashes, so a leaked database row "
                        "cannot be replayed as a login.",
        }
        ids = {}
        for ctype, body in bodies.items():
            ids[ctype] = json.loads(
                self._run(["task", "comment-add", "-r", ROADMAP, str(t1),
                           "--type", ctype, "--body", body])[1]
            )["id"]
        self.comment_bodies = bodies

        # An edit, so exactly one entry carries the edited stamp.
        self._run(["task", "comment-edit", "-r", ROADMAP, str(ids["HYPOTHESIS"]),
                   "--type", "HYPOTHESIS",
                   "--body", "Confirmed: the expiry used After(), so the boundary "
                             "second was accepted by the parser and refused by the "
                             "handler. Today's fix doesn't change the token lifetime."])
        self.edited_comment_body = (
            "Confirmed: the expiry used After(), so the boundary second was "
            "accepted by the parser and refused by the handler. Today's fix "
            "doesn't change the token lifetime."
        )

        self.sprint_comment_body = (
            "Decided to close the hardening sprint with the passkey work "
            "unstarted: the FIDO2 library review is still open and the remaining "
            "tasks carry cleanly into the next sprint."
        )
        self._run(["sprint", "comment-add", "-r", ROADMAP, str(self.open_sid),
                   "--type", "DECISION", "--body", self.sprint_comment_body])
        self.commented_task = t1
        self.uncommented_task = t2
        return ids

    @staticmethod
    def _slice_sprint_comments_card(html):
        """Return the sprint's own Comments card, up to the end of its timeline.

        The card precedes the page's task modals, so bounding it on its own
        first </ul> keeps a member task's comment log out of the slice — which
        is what lets the card be tested for the sprint's comments alone.
        """
        start = html.find('<h3 class="card-title">Comments')
        assert start != -1, "the sprint page carries no Comments card"
        end = html.find("</ul>", start)
        return html[start:end + 5] if end != -1 else html[start:]

    @staticmethod
    def _timeline(fragment):
        """Return the <ul class="timeline"> block of a fragment, or ''."""
        match = re.search(r'<ul class="timeline">.*?</ul>', fragment, re.S)
        return match.group(0) if match else ""

    @staticmethod
    def _timeline_types(timeline):
        return re.findall(r'<span class="badge bg-secondary-lt">(\w+)</span>', timeline)

    def test_task_detail_endpoint_serves_the_comment_log(self):
        """The log the modal shows travels through the task detail endpoint, in
        the order the CLI reports and complete (SPEC/WEB.md § Task Detail
        Endpoint; Acceptance Criterion 94)."""
        self._seed_comments()
        proc, port = self._start(["--port", "0"])

        status, detail = self._task_detail(port, ROADMAP, self.commented_task)
        assert status == 200, f"the task detail endpoint answered {status}"
        assert set(detail) == {"task", "comments"}, f"unexpected members {set(detail)!r}"
        assert detail["task"]["id"] == self.commented_task

        # Complete, and in the order `rmp task comment-list` returns.
        cli_log = json.loads(
            self._run(["task", "comment-list", "-r", ROADMAP,
                       str(self.commented_task)])[1]
        )
        assert [c["type"] for c in detail["comments"]] == [c["type"] for c in cli_log]
        assert [c["body"] for c in detail["comments"]] == [c["body"] for c in cli_log]
        created = [c["created_at"] for c in detail["comments"]]
        assert created == sorted(created), f"the log is not oldest first: {created}"

        # Exactly one entry was edited, and only that one carries updated_at.
        edited = [c for c in detail["comments"] if c.get("updated_at")]
        assert len(edited) == 1, f"{len(edited)} entries carry updated_at, want 1"

        # The page carries none of it: the modal is filled on demand.
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/tasks")
        for comment in detail["comments"]:
            assert comment["body"] not in body, (
                "a comment body reached the served page; the modal is filled from the endpoint"
            )
        assert '<ul class="timeline">' not in body, (
            "the tasks page renders no timeline: the modal builds it from the endpoint"
        )

    def test_task_detail_endpoint_without_comments_returns_an_empty_array(self):
        """A task with no comment yields [], never null, so the script renders
        its empty-state message (Acceptance Criterion 94)."""
        self._seed_comments()
        proc, port = self._start(["--port", "0"])

        status, detail = self._task_detail(port, ROADMAP, self.uncommented_task)
        assert status == 200
        assert detail["comments"] == [], f"want an empty array, got {detail['comments']!r}"

        # The message itself lives in the script, which renders the empty state.
        _, _, script = self._req(port, "/static/task-modal.js")
        assert "No comments have been recorded on this task yet." in script

        # The commented task, on the same server, does carry its log: the empty
        # array above is a fact about that task, not about the endpoint.
        _, populated = self._task_detail(port, ROADMAP, self.commented_task)
        assert len(populated["comments"]) == 3

    def test_task_detail_endpoint_rejects_unknown_names_ids_and_methods(self):
        """The endpoint enforces the roadmap-route discipline: 404 for an invalid
        or unknown roadmap, a non-integer id and a task of another roadmap; 405
        for any non-read method; and no-store on its response (Acceptance
        Criterion 95)."""
        self._seed_comments()
        self._run(["roadmap", "create", "endpoint_other"])
        other_task = self.test.create_task(
            "endpoint_other",
            "Rotate the acquirer API credentials",
            "Credentials must rotate quarterly",
            "Rotate through the secret manager",
            "The old credential stops authenticating",
        )
        proc, port = self._start(["--port", "0"])

        ok, headers, _ = self._req(port, f"/roadmaps/{ROADMAP}/tasks/{self.commented_task}/data")
        assert ok == 200
        assert headers.get("cache-control") == "no-store", headers.get("cache-control")

        for path in (
            "/roadmaps/INVALID/tasks/1/data",
            "/roadmaps/..%2fetc/tasks/1/data",
            "/roadmaps/no_such_roadmap/tasks/1/data",
            f"/roadmaps/{ROADMAP}/tasks/not-a-number/data",
            f"/roadmaps/{ROADMAP}/tasks/999999/data",
            # A task of ANOTHER roadmap is not reachable through this one.
            f"/roadmaps/{ROADMAP}/tasks/{other_task}/data"
            if other_task not in self.open_task_ids else
            f"/roadmaps/{ROADMAP}/tasks/999998/data",
            # The bare page path is not a route.
            f"/roadmaps/{ROADMAP}/tasks/{self.commented_task}",
        ):
            status, _, _ = self._req(port, path)
            assert status == 404, f"GET {path} answered {status}, want 404"

        path = f"/roadmaps/{ROADMAP}/tasks/{self.commented_task}/data"
        for method in ("POST", "PUT", "PATCH", "DELETE"):
            status, _, _ = self._req(port, path, method=method)
            assert status == 405, f"{method} {path} answered {status}, want 405"

    def test_sprint_page_comments_card_shows_only_the_sprints_own_comments(self):
        """The Comments card is the SPRINT's log, counted in its own badge.

        A member task's comments appear on the same page, inside that task's
        modal, so the card is sliced out and checked on its own.
        """
        self._seed_comments()
        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}")
        assert status == 200

        card = self._slice_sprint_comments_card(body)
        badge = re.search(
            r'<h3 class="card-title">Comments <span class="badge bg-secondary-lt ms-2">'
            r"(\d+)</span></h3>",
            card,
        )
        assert badge, f"the Comments card carries no count badge: {card[:200]!r}"
        assert badge.group(1) == "1", (
            f"the badge must count the sprint's own comments, got {badge.group(1)}"
        )
        assert "Oldest first" in card, "the card must state its ordering"

        assert self.sprint_comment_body[:50] in card, "the sprint's comment is missing"
        assert self._timeline_types(self._timeline(card)) == ["DECISION"]
        for task_body in self.comment_bodies.values():
            assert task_body[:50] not in card, (
                "a member task's comment leaked into the sprint's Comments card"
            )

        # The member task's own log is still reachable — from its own detail
        # endpoint, which is where the modal now gets it — and it is the task's
        # log, not the sprint's.
        _, detail = self._task_detail(port, ROADMAP, self.commented_task)
        assert self.comment_bodies["FINDING"] in [c["body"] for c in detail["comments"]]
        assert self.sprint_comment_body not in [c["body"] for c in detail["comments"]], (
            "a sprint comment leaked into a task's detail"
        )

    def test_sprint_page_comments_card_empty_state(self):
        """A sprint with no comments shows the Tabler empty panel and a zero badge."""
        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(
            port, f"/roadmaps/{ROADMAP}/sprints/{self.pending_sid}"
        )
        assert status == 200

        card = body[body.find('<h3 class="card-title">Comments'):]
        assert card, "the Comments card must be present even when empty"
        assert '<span class="badge bg-secondary-lt ms-2">0</span>' in card[:200], card[:200]
        assert "Nothing has been recorded on this sprint yet." in card
        assert '<ul class="timeline">' not in card, (
            "a sprint with no comments must render no timeline"
        )

    def test_roadmap_landing_page_renders_no_comment_log(self):
        """The sprints landing page is the negative control: no comment surface.

        It renders compact sprint cards only, so neither the timeline, nor the
        Comments card, nor any comment body may appear there.
        """
        self._seed_comments()
        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, f"/roadmaps/{ROADMAP}")
        assert status == 200

        assert '<ul class="timeline">' not in body
        assert '<li class="timeline-event">' not in body
        assert '<h3 class="card-title">Comments' not in body
        assert "No comments have been recorded on this task yet." not in body
        for task_body in self.comment_bodies.values():
            assert task_body[:50] not in body
        assert self.sprint_comment_body[:50] not in body

    def test_comment_surfaces_are_read_only_get_and_head(self):
        """The two comment-bearing routes answer GET/HEAD and write no audit entry."""
        self._seed_comments()
        proc, port = self._start(["--port", "0"])
        paths = (
            f"/roadmaps/{ROADMAP}/tasks",
            f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}",
        )

        before = json.loads(self._run(["audit", "stats", "-r", ROADMAP])[1])
        for path in paths:
            assert self._req(port, path)[0] == 200, path
            assert self._req(port, path, method="HEAD")[0] == 200, path
            for method in ("POST", "PUT", "PATCH", "DELETE"):
                status, _, _ = self._req(port, path, method=method)
                assert status == 405, f"{method} {path} must be 405, got {status}"
        after = json.loads(self._run(["audit", "stats", "-r", ROADMAP])[1])
        assert after["total_entries"] == before["total_entries"], (
            "rendering the comment surfaces wrote audit entries: "
            f"{before['total_entries']} -> {after['total_entries']}"
        )

    # ====================================================================
    # AC9: name validation / path-traversal guard
    # ====================================================================

    def test_invalid_and_missing_names_return_404(self):
        proc, port = self._start(["--port", "0"])
        # Name violating ^[a-z0-9_-]+$ (uppercase) -> 404, never reaches FS.
        assert self._req(port, "/roadmaps/INVALID")[0] == 404
        assert self._req(port, "/roadmaps/INVALID/tasks")[0] == 404
        # Encoded traversal attempt -> 404 (raw path, no client normalisation).
        assert self._req(port, "/roadmaps/..%2fetc")[0] == 404
        assert self._req(port, "/roadmaps/..%2fetc/tasks")[0] == 404
        # Syntactically valid but non-existent roadmap -> 404.
        assert self._req(port, "/roadmaps/no_such_roadmap")[0] == 404
        assert self._req(port, "/roadmaps/no_such_roadmap/tasks")[0] == 404
        assert self._req(port, "/roadmaps/no_such_roadmap/graph")[0] == 404
        assert self._req(port, "/roadmaps/no_such_roadmap/graph/data")[0] == 404

    # ====================================================================
    # AC10/AC11/AC12: graph page, data endpoint, read-only proof
    # ====================================================================

    def test_graph_page_loads_local_d3_and_layout_dropdown(self):
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}/graph")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")
        # The vendored D3.js bundle and the d3-sankey plugin load locally, in
        # the order d3 -> d3-sankey -> graph.js (d3-sankey augments the global
        # d3 and graph.js consumes both).
        assert "/static/vendor/d3/d3.min.js" in body, "graph page must load vendored D3.js"
        assert "/static/vendor/d3/d3-sankey.min.js" in body, "graph page must load the d3-sankey plugin"
        assert "/static/graph.js" in body, "graph page must load the local viewer script"
        i_d3 = body.index("/static/vendor/d3/d3.min.js")
        i_sankey = body.index("/static/vendor/d3/d3-sankey.min.js")
        i_viewer = body.index("/static/graph.js")
        assert i_d3 < i_sankey < i_viewer, (
            "script load order must be d3, then d3-sankey, then graph.js"
        )
        # Cytoscape is gone and not referenced anywhere on the page.
        assert "cytoscape" not in body.lower(), "graph page must no longer reference cytoscape"
        # Nothing comes from a remote origin.
        assert "cdn" not in body.lower() and "unpkg" not in body.lower()

        # The layout dropdown offers the complete set of nine Networks-section
        # layouts with Mobile patent suits preselected as the default (AC10).
        assert 'id="layout-select"' in body, "graph page must provide the layout dropdown"
        layouts = (
            ("force", "Force-directed graph"),
            ("disjoint", "Disjoint force-directed graph"),
            ("patents", "Mobile patent suits"),
            ("arc", "Arc diagram"),
            ("sankey", "Sankey diagram"),
            ("bundling", "Hierarchical edge bundling"),
            ("chord", "Chord diagram"),
            ("chord-directed", "Directed chord diagram"),
            ("chord-dependency", "Chord dependency diagram"),
        )
        for value, label in layouts:
            assert f'value="{value}"' in body, f"layout dropdown missing option {value!r}"
            assert label in body, f"layout dropdown missing label {label!r}"
        # The four new layouts added in this version are present by value.
        for value in ("patents", "chord", "chord-directed", "chord-dependency"):
            assert f'value="{value}"' in body, f"layout dropdown missing new option {value!r}"
        # The nine options appear in the required order.
        positions = [body.index(f'value="{value}"') for value, _ in layouts]
        assert positions == sorted(positions), (
            "layout dropdown options are out of the required order"
        )
        # Mobile patent suits is the default selected option (exactly one
        # layout option preselected). The page also carries the query bar's
        # node-limit dropdown, whose default value 100 is itself a preselected
        # <option ... selected> (AC45), so the page now has two preselected
        # options overall; scope the uniqueness check to the layout options.
        assert re.search(
            r'<option value="patents"[^>]*\bselected\b', body, re.I
        ), "Mobile patent suits must be the preselected default layout"
        layout_selected = sum(
            1 for value, _ in layouts
            if re.search(rf'<option value="{value}"[^>]*\bselected\b', body, re.I)
        )
        assert layout_selected == 1, (
            "exactly one layout option must be preselected (the Mobile patent suits default)"
        )

    def test_labels_sidebar_totals_and_collapse_control(self):
        """The labels sidebar renders, in each section header, an absolute total
        element the client populates, and a touch-friendly collapse/expand
        control at its top built with the page's Tabler icon font; the served
        assets carry the client logic that derives the totals (distinct-node
        total and edge total, kept distinct from the per-label sums) and toggles
        the sidebar without disturbing the highlight, layout, search, or detail
        panel (SPEC/WEB.md § Graph Labels Sidebar rules 11-12, Acceptance
        Criteria 43/51/52). The totals and the toggle act client-side, so the
        contract is verified on the page shell and the served scripts/styles."""
        proc, port = self._start(["--port", "0"])

        # The page shell carries the per-section total containers and the
        # collapse/expand control, which defaults to expanded on load.
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/graph")
        for marker in (
            'id="node-labels-total"',
            'id="edge-types-total"',
            'id="labels-toggle"',
            "ti-layout-sidebar-left-collapse",
            'aria-expanded="true"',
        ):
            assert marker in body, f"graph page missing labels-sidebar marker {marker!r}"
        # The collapse control sits at the top of the sidebar, before the section
        # headers, and each header is accompanied by its total element.
        i_sidebar = body.index('id="labels-sidebar"')
        i_toggle = body.index('id="labels-toggle"')
        i_node_total = body.index('id="node-labels-total"')
        i_edge_total = body.index('id="edge-types-total"')
        i_graph = body.index('id="graph"')
        assert i_sidebar < i_toggle < i_node_total < i_edge_total < i_graph, (
            "the collapse control and section totals are out of the required order"
        )

        # The viewer script derives the section totals client-side from the
        # fetched data: the node total is the distinct-node count (the deduped
        # node array length), NOT the sum of the per-label counts, and the edge
        # total is the edge count.
        _, _, js = self._req(port, "/static/graph.js")
        assert "nodeTotal: model.nodes.length" in js, (
            "graph.js node total must be the distinct-node count, not the per-label sum"
        )
        assert "typeTotal: model.links.length" in js, (
            "graph.js edge total must be the fetched-edge count"
        )
        assert 'getElementById("node-labels-total")' in js
        assert 'getElementById("edge-types-total")' in js

        # The collapse/expand toggle logic is wired and is a pure visibility
        # toggle: it must not run a search, reset the highlight selection, or
        # touch the detail panel / empty state.
        assert 'getElementById("labels-toggle")' in js
        assert "setSidebarCollapsed" in js
        assert "is-collapsed" in js
        assert "ti-layout-sidebar-left-expand" in js, (
            "the toggle icon must swap to the expand glyph when collapsed"
        )
        start = js.index("function setSidebarCollapsed(")
        end = js.index("\n  }\n", start)
        toggle_body = js[start:end]
        for forbidden in ("runSearch", "activeLabels = ", "activeTypes = ", "hidePanel", "showEmpty"):
            assert forbidden not in toggle_body, (
                f"setSidebarCollapsed() must not {forbidden!r}: collapsing changes only "
                "sidebar visibility and canvas width"
            )

        # The stylesheet carries the section-total badge and the collapsed-state
        # rules (hide the body; contract the column so the canvas takes the full
        # width on a wide viewport).
        _, _, css = self._req(port, "/static/style.css")
        for token in (
            ".labels-sidebar__total",
            ".labels-sidebar__toggle",
            ".labels-sidebar.is-collapsed .labels-sidebar__body",
            ".labels-sidebar.is-collapsed",
        ):
            assert token in css, f"style.css missing collapsed/total rule {token!r}"

    def test_neighbor_focus_on_node_selection(self):
        """Selecting a node puts the canvas into neighbor focus: the served viewer
        script carries a single focus state, an undirected first-degree
        neighbourhood computed client-side from the model's links (startId/endId
        mapped to source/target), one unified emphasis function that gives focus
        precedence over the labels highlight and reuses the same dim-not-remove
        mechanism, the consistent clear gestures (panel close, empty-canvas tap,
        re-select), and the layout/search coexistence (render reapplies the
        current emphasis; a search clears the focus). Neighbor focus is computed
        and applied entirely client-side, so the contract is verified on the
        served script, consistent with the existing server-side-only test
        approach for the graph page (SPEC/WEB.md § Roadmap Knowledge-Graph Page,
        "Neighbor focus on node selection"; § Graph Labels Sidebar rule 8;
        Acceptance Criteria 54-56)."""
        proc, port = self._start(["--port", "0"])
        _, _, js = self._req(port, "/static/graph.js")

        # Single module-level focus state plus the unified emphasis/neighbourhood
        # and clear/select helpers.
        for token in (
            "focusedNodeId",
            "function neighborSet(",
            "function applyEmphasis(",
            "function applyFocusDimming(",
            "function clearFocus(",
            "function onNodeSelected(",
            "function dismissSelection(",
            "data-node-id",
            "data-edge-source",
            "data-edge-target",
        ):
            assert token in js, f"graph.js missing neighbor-focus token {token!r}"

        # One source of truth: the focus state is declared exactly once.
        assert js.count("var focusedNodeId") == 1, (
            "graph.js must declare the focus state once (single dimming source of truth)"
        )

        # Focus reuses the SAME dim-not-remove mechanism the labels highlight uses.
        assert "is-dimmed" in js, "neighbor focus must dim with the .is-dimmed class, not remove elements"

        # applyEmphasis is the single dimming path: focus takes precedence,
        # otherwise it delegates to the labels highlight.
        emp = js[js.index("function applyEmphasis(") :]
        emp = emp[: emp.index("\n  }\n")]
        assert "focusedNodeId !== null" in emp, (
            "applyEmphasis() must branch on the focus state (focus precedence over labels)"
        )
        assert "applyFocusDimming" in emp and "applyHighlight()" in emp, (
            "applyEmphasis() must dim by neighbourhood when focused and delegate to "
            "applyHighlight() otherwise (one dimming path)"
        )

        # The neighbourhood is UNDIRECTED and derived from the model's links.
        nb = js[js.index("function neighborSet(") :]
        nb = nb[: nb.index("\n  }\n")]
        assert "s === nodeId" in nb and "t === nodeId" in nb, (
            "neighborSet() must be undirected: include a neighbour when the focused "
            "node is either the source OR the target of an edge"
        )
        assert "graphModel.links" in nb, (
            "neighborSet() must compute the neighbourhood client-side from graphModel.links"
        )

        # render() reapplies the CURRENT emphasis so a layout change preserves a
        # neighbor focus too.
        assert "applyEmphasis();" in js[js.index("function render(") :], (
            "render() must call applyEmphasis() to preserve the current focus/highlight"
        )

        # Unified clear gestures: closing the panel and tapping empty canvas both
        # clear the focus together with the panel.
        assert 'panelClose.addEventListener("click", dismissSelection)' in js, (
            "closing the detail panel must clear the neighbor focus too"
        )

        # A search clears the focus as part of rendering the new result.
        ad = js[js.index("function applyData(") :]
        ad = ad[: ad.index("\n  }\n")]
        assert "focusedNodeId = null" in ad, (
            "applyData() (the search re-render path) must clear the neighbor focus"
        )

    def test_graph_data_endpoint_shape(self):
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}/graph/data")
        assert status == 200
        assert headers.get("content-type", "").startswith("application/json")
        data = json.loads(body)
        assert set(data.keys()) == {"nodes", "edges"}, f"unexpected keys: {data.keys()}"
        assert isinstance(data["nodes"], list) and isinstance(data["edges"], list)
        assert len(data["nodes"]) >= 2, "the populated graph has at least two nodes"
        assert len(data["edges"]) >= 1, "the populated graph has at least one edge"
        node_ids = {n["id"] for n in data["nodes"]}
        for edge in data["edges"]:
            for key in ("id", "type", "startId", "endId", "properties"):
                assert key in edge, f"edge missing {key}"
            assert edge["startId"] in node_ids and edge["endId"] in node_ids, (
                "every edge endpoint must resolve to a node in the same response"
            )
        # Node shape per DATA_FORMATS Graph element mapping.
        for node in data["nodes"]:
            assert set(node.keys()) == {"id", "labels", "properties"}
            assert isinstance(node["labels"], list)

    def test_graph_reads_create_no_snapshot(self):
        graph_dir = Path(self.home) / ".roadmaps" / ROADMAP / "graph"
        snap = graph_dir / "snapshot"
        snap_existed = snap.exists()
        proc, port = self._start(["--port", "0"])
        for _ in range(5):
            assert self._req(port, f"/roadmaps/{ROADMAP}/graph/data")[0] == 200
        # A web read must not trigger a checkpoint: no snapshot newly created.
        if not snap_existed:
            assert not snap.exists(), "web graph reads must not create a snapshot/ dir"

    def test_empty_graph_returns_empty_and_creates_nothing(self):
        # A roadmap that never used `graph`: empty graph, no graph/ dir created.
        self._run(["roadmap", "create", "blankspace"])
        graph_dir = Path(self.home) / ".roadmaps" / "blankspace" / "graph"
        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, "/roadmaps/blankspace/graph/data")
        assert status == 200
        assert json.loads(body) == {"nodes": [], "edges": []}
        assert not graph_dir.exists(), "reading an absent graph must not create graph/"

    # ====================================================================
    # AC45-AC50: graph query bar (q / limit parameters on graph/data)
    # ====================================================================

    @staticmethod
    def _graph_data(port, q=None, limit=None, roadmap=ROADMAP):
        """Build /graph/data with URL-encoded q and limit, GET it, return JSON.

        roadmap defaults to the module fixture; the query time budget scenario
        overrides it, because the store that scenario needs is far larger than
        every other scenario wants to pay for.
        """
        params = {}
        if q is not None:
            params["q"] = q
        if limit is not None:
            params["limit"] = limit
        path = f"/roadmaps/{roadmap}/graph/data"
        if params:
            path += "?" + urllib.parse.urlencode(params)
        return path

    def test_query_bar_present_with_default_query_and_limits(self):
        """AC45: the graph page renders the query bar with the default query
        pre-filled, a Search button, and the six-value node-limit dropdown with
        100 selected by default."""
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/graph")
        assert 'id="query-input"' in body, "query bar must render the editable query box"
        # The default query sits in a <textarea> (RCDATA), where html/template
        # does not entity-escape '>', so it renders literally.
        assert "MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m" in body, (
            "query box must be pre-filled with the default query"
        )
        assert 'id="query-run"' in body, "query bar must render the Search button"
        assert 'id="limit-select"' in body, "query bar must render the node-limit dropdown"
        for value in ("50", "100", "250", "500", "1000", "3000"):
            assert f'value="{value}"' in body, f"limit dropdown missing option {value}"
        assert re.search(
            r'<option value="100"[^>]*\bselected\b', body, re.I
        ), "node limit 100 must be the preselected default"

    def test_query_bar_ctrl_enter_accelerator(self):
        """AC53: graph.js wires a Ctrl+Enter keyboard accelerator on the focused
        query box that triggers the search exactly as the Search button does,
        reusing the existing search path (the form submit) instead of
        duplicating the search logic; plain Enter is left untouched."""
        proc, port = self._start(["--port", "0"])
        _, _, js = self._req(port, "/static/graph.js")
        # The accelerator is wired as a keydown handler on the query box and
        # fires on the Ctrl+Enter chord, suppressing the default newline.
        assert 'queryInput.addEventListener("keydown"' in js, (
            "graph.js must wire a keydown accelerator on the query box"
        )
        assert "event.ctrlKey" in js, "the accelerator must trigger on Ctrl+Enter"
        assert 'event.key === "Enter"' in js, "the accelerator must gate on the Enter key"
        # It reuses the Search submit path (requestSubmit fires the same submit
        # event the type="submit" Search button does), never a duplicated fetch.
        keydown = js.index('queryInput.addEventListener("keydown"')
        handler = js[keydown:keydown + js[keydown:].index("\n    });")]
        assert "requestSubmit" in handler, (
            "Ctrl+Enter must reuse the Search submit path via requestSubmit(), "
            "not duplicate the search logic"
        )

    def test_query_bar_default_q_is_backward_compatible(self):
        """AC46: a request with no q runs the default query and returns the full
        graph, exactly as before the query bar existed."""
        proc, port = self._start(["--port", "0"])
        # No q parameter.
        status, _, body = self._req(port, self._graph_data(port))
        assert status == 200
        baseline = json.loads(body)
        # The explicit default query yields the same full-graph view.
        status, _, body = self._req(
            port,
            self._graph_data(port, q="MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m"),
        )
        assert status == 200
        explicit = json.loads(body)
        assert len(explicit["nodes"]) == len(baseline["nodes"])
        assert len(explicit["edges"]) == len(baseline["edges"])
        assert len(baseline["nodes"]) >= 2 and len(baseline["edges"]) >= 1

    def test_query_bar_rejects_write_and_ddl_without_executing(self):
        """AC47: a writing or DDL query is rejected (HTTP 400, kind
        not_read_only) before execution; the store is unchanged."""
        proc, port = self._start(["--port", "0"])
        before = json.loads(self._req(port, self._graph_data(port))[2])
        write_queries = [
            "MATCH (n) DELETE n",
            "MATCH (n) DETACH DELETE n",
            "CREATE (x:Spec {key:'injected-by-web'})",
            "MATCH (n:Spec) SET n.compromised = true",
            "CREATE INDEX ON :Spec(key)",
            "create   index spec_idx",  # non-canonical spacing/casing
        ]
        for q in write_queries:
            status, _, body = self._req(port, self._graph_data(port, q=q))
            assert status == 400, f"write query not rejected with 400: {q!r}"
            err = json.loads(body)
            assert err.get("kind") == "not_read_only", (
                f"query {q!r} not classified as not_read_only: {err}"
            )
        # The store is unchanged: the default read returns the same node count
        # and no injected node appeared.
        after = json.loads(self._req(port, self._graph_data(port))[2])
        assert len(after["nodes"]) == len(before["nodes"]), (
            "rejected write queries must not change the store"
        )
        for n in after["nodes"]:
            assert n["properties"].get("key") != "injected-by-web", (
                "a rejected CREATE must not have inserted a node"
            )

    def test_query_bar_literal_masking_not_falsely_rejected(self):
        """AC47: write keywords only inside a string literal are accepted as
        read-only and executed; a genuine DELETE is rejected."""
        proc, port = self._start(["--port", "0"])
        accepted = 'MATCH (m) WHERE m.key = "mentions delete and set and create" RETURN m'
        status, _, _ = self._req(port, self._graph_data(port, q=accepted))
        assert status == 200, "literal-only write keywords must be accepted as read-only"

        rejected = 'MATCH (m) WHERE m.key = "mentions delete" DELETE m'
        status, _, body = self._req(port, self._graph_data(port, q=rejected))
        assert status == 400
        assert json.loads(body).get("kind") == "not_read_only"

    def test_query_bar_limit_injection_and_invalid_limit(self):
        """AC48: an invalid limit is rejected (not clamped); allowed limits are
        accepted; a user LIMIT is respected over the dropdown value."""
        proc, port = self._start(["--port", "0"])
        for bad in ("7", "0", "5000", "abc"):
            status, _, body = self._req(port, self._graph_data(port, limit=bad))
            assert status == 400, f"invalid limit {bad!r} not rejected"
            assert json.loads(body).get("kind") == "invalid_limit"
        for ok in ("50", "100", "250", "500", "1000", "3000"):
            status, _, _ = self._req(port, self._graph_data(port, limit=ok))
            assert status == 200, f"allowed limit {ok!r} rejected"
        # A user-supplied LIMIT 1 is respected even with a larger dropdown value:
        # the result is capped at one returned row's worth of elements.
        status, _, body = self._req(
            port, self._graph_data(port, q="MATCH (n) RETURN n LIMIT 1", limit="3000")
        )
        assert status == 200
        assert len(json.loads(body)["nodes"]) == 1, "user LIMIT 1 must be respected"

    def test_query_bar_statements_admitting_no_limit_are_exempt(self):
        """AC111: the node-limit injection is suppressed for the statement forms
        that admit NO top-level LIMIT clause at all — the SHOW schema-
        introspection commands and standalone procedure calls — so a read the
        guard rail admits, and that `rmp graph query` runs, stays usable through
        the endpoint (SPEC/WEB.md § Graph Data Endpoint, Suppression 2).

        Appending a LIMIT to one of those bounds nothing: it makes the statement
        fail in the PARSER. The claim under test is therefore that each one
        EXECUTES rather than coming back as a 400 execution failure. Every one of
        them is a tabular result carrying no graph elements, so the specified
        outcome is the empty graph shape, and the body is asserted to equal it
        exactly rather than merely to be error-free: {"nodes": [], "edges": []}
        is the success shape, and a failure body would carry error and kind
        instead (SPEC/DATA_FORMATS.md § Graph View Data).

        The boundary is exactly the presence of a top-level RETURN, so the same
        test carries the control that sits on the other side of it. A PROJECTED
        call — a leading CALL that IS projected through a RETURN — parses as an
        ordinary query and DOES take the injected LIMIT. Asserting only that the
        six exempt forms succeed would be satisfied just as well by a broken
        endpoint that never injected a LIMIT into any CALL; asserting that the
        projected form is still CAPPED is what separates the two. The store is
        given more nodes than the cap so the difference is observable: capped at
        50 against a store of 62, and complete at 62 when the cap is raised
        beyond it.
        """
        # A store larger than the smallest allowed node limit, so a cap is
        # visible as a cap. The module fixture's own graph is two nodes, which
        # no limit in the allowed set could narrow. Each test method runs against
        # its own temporary HOME, so this widening is local to this scenario.
        self._run(["graph", "create", "-r", ROADMAP,
                   "--query", "UNWIND range(1,60) AS i CREATE (:Bulk {i:i})"])
        _, out, _ = self._run(["graph", "query", "-r", ROADMAP,
                               "--query", "MATCH (n) RETURN count(n)"])
        total = json.loads(out)["rows"][0][0]
        assert total > 50, (
            f"the control needs a store larger than the 50-node cap; got {total}"
        )

        proc, port = self._start(["--port", "0"])

        # The exempt forms: two bare SHOW commands, a SHOW carrying a YIELD tail
        # (a tail does not make a LIMIT injectable), a bare standalone call, a
        # standalone call that is not a pure read of the store, and a standalone
        # call carrying a YIELD.
        exempt = (
            "SHOW INDEXES",
            "SHOW CONSTRAINTS",
            "SHOW INDEXES YIELD name, state RETURN name",
            "CALL db.labels()",
            "CALL db.stats.refresh()",
            "CALL db.propertyKeys() YIELD propertyKey",
        )
        for query in exempt:
            status, _, body = self._req(
                port, self._graph_data(port, q=query, limit="100")
            )
            assert status == 200, (
                f"{query!r} must execute, not fail in the parser with an "
                f"injected LIMIT; got {status} {body!r}"
            )
            assert json.loads(body) == {"nodes": [], "edges": []}, (
                f"{query!r} is a tabular result carrying no graph elements, so "
                f"the response is the empty graph shape; got {body!r}"
            )

        # The control on the other side of the boundary: projected through a
        # RETURN, so the LIMIT is injected and the result IS capped.
        projected = "CALL db.labels() YIELD label MATCH (n) RETURN n"
        status, _, body = self._req(
            port, self._graph_data(port, q=projected, limit="50")
        )
        assert status == 200, (
            f"a projected call must execute with the injected LIMIT; got "
            f"{status} {body!r}"
        )
        capped = json.loads(body)["nodes"]
        assert 0 < len(capped) <= 50, (
            f"a projected call takes the injected LIMIT 50, so it returns at "
            f"most 50 of the {total} nodes; got {len(capped)}"
        )
        assert len(capped) < total, (
            f"the cap must be observable: {len(capped)} of {total} nodes came "
            "back, so nothing was capped and the injection was suppressed for a "
            "statement that admits a LIMIT"
        )

        # Raising the limit past the store size returns the whole store through
        # the same projected form, proving the shortfall above was the LIMIT and
        # not the query itself.
        status, _, body = self._req(
            port, self._graph_data(port, q=projected, limit="3000")
        )
        assert status == 200
        assert len(json.loads(body)["nodes"]) == total, (
            "the same projected call under a limit above the store size must "
            "return every node"
        )

    def test_query_bar_execution_failure_distinct_from_rejection(self):
        """AC50: a read-only query that fails in the engine (invalid syntax)
        surfaces kind=execution, distinct from a read-only rejection."""
        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, self._graph_data(port, q="MATCH (n) RETURN"))
        assert status == 400
        assert json.loads(body).get("kind") == "execution", (
            "an execution failure must be distinct from a read-only rejection"
        )

    def test_graph_query_is_bounded_by_the_query_time_budget(self):
        """AC110: the endpoint executes the caller's query under a per-request
        time budget, so a read the guard rail admits but whose WORK is unbounded
        cannot hold the endpoint open indefinitely (SPEC/WEB.md § Graph Query
        Time Budget).

        The node limit cannot stand in for the budget. The limit bounds the
        RESULT; an aggregate over a Cartesian product returns one row whatever
        the limit is, yet scans the whole product to produce it, so the budget is
        the only bound on that work (rule 3). Exhausting it is a query execution
        failure and nothing new: HTTP 400, kind=execution, the same class as
        invalid Cypher, distinguished only by the reason it gives (rules 4, 5).

        The five seconds this costs are inherent. The budget has no URL
        parameter, no flag and no environment variable: graphQueryBudget is
        assigned once, in the server process, and nothing outside it can move it
        (rule 8). The cost is confined instead — the store below belongs to this
        scenario alone, so the module's shared fixture stays two nodes and every
        other scenario stays fast.

        That store is sized from measurement. Unbounded — through
        `rmp graph query`, which has no budget — the three-way Cartesian product
        below costs 61.2s over these 799 nodes against a 5s budget: a twelvefold
        margin, so the query still cannot finish inside the budget on hardware an
        order of magnitude faster than the machine this was measured on. The
        margin is free: the request is cut at the budget whatever the store size,
        so a larger store buys robustness and costs no wall time. A 399-node
        store, for comparison, costs 7.5s unbounded — a 1.5x margin, which any
        machine 1.6x faster would turn into a silent false pass.

        Both time bounds are asserted and the LOWER one carries the weight: a
        regression that disabled the budget and failed the request for some other
        reason, instantly, would satisfy an upper bound on its own.
        """
        # 797 bulk nodes on top of the two-node, one-edge seed the module builds
        # for its fixture, giving a 799-node store. Seeding is one CLI call and
        # costs 0.03s.
        name = "telemetry"
        bulk = 797
        self._run(["roadmap", "create", name])
        self._run(["graph", "create", "-r", name,
                   "--query", "CREATE (s:Spec {key:'passwordless-auth'})"])
        self._run(["graph", "create", "-r", name,
                   "--query", "CREATE (c:Code {path:'internal/auth/magiclink.go'})"])
        self._run(["graph", "create", "-r", name,
                   "--query",
                   "MATCH (s:Spec {key:'passwordless-auth'}), "
                   "(c:Code {path:'internal/auth/magiclink.go'}) "
                   "CREATE (s)-[:IMPLEMENTED_BY]->(c)"])
        self._run(["graph", "create", "-r", name,
                   "--query",
                   "UNWIND range(1," + str(bulk) + ") AS i CREATE (:Bulk {i:i})"])

        # Ground truth for the store, read from the engine rather than assumed,
        # so the completeness assertion below cannot silently drift with the
        # seed. The reader limit must sit above it, or a capped read would be
        # mistaken for a complete one.
        _, out, _ = self._run(["graph", "query", "-r", name,
                               "--query", "MATCH (n) RETURN count(n)"])
        seeded = json.loads(out)["rows"][0][0]
        assert seeded == bulk + 2, (
            f"the seed must produce {bulk + 2} nodes; got {seeded}"
        )
        reader_limit = "1000"
        assert seeded < int(reader_limit), (
            f"the completeness read needs a limit above the {seeded}-node store"
        )

        proc, port = self._start(["--port", "0"])

        # The expensive read: 799**3 = 510 million tuples scanned to produce one
        # aggregate row, which no node limit can narrow.
        expensive = "MATCH (a),(b),(c) RETURN count(*)"
        # Six times the budget, so a client-side timeout is never the budget
        # firing late; it can only mean the budget did not fire at all. A server
        # that never cuts the query stops answering long before this, because its
        # own 30s WriteTimeout closes the connection unanswered — which arrives
        # here as a socket error, not as a response. Naming that outcome as the
        # failure it is keeps the diagnosis on the contract rather than on a
        # traceback from inside http.client.
        started = time.monotonic()
        try:
            status, _, body = self._req(
                port,
                self._graph_data(port, q=expensive, limit="100", roadmap=name),
                timeout=30,
            )
        except (OSError, http.client.HTTPException) as exc:
            waited = time.monotonic() - started
            raise AssertionError(
                f"the endpoint never answered the expensive query: it was still "
                f"running {waited:.2f}s in, and the connection failed with "
                f"{type(exc).__name__}: {exc}. The query time budget did not cut "
                "the query."
            ) from exc
        elapsed = time.monotonic() - started

        assert status == 400, (
            f"an exhausted budget is a query execution failure, answered 400; "
            f"got {status} after {elapsed:.2f}s: {body!r}"
        )
        err = json.loads(body)
        assert err.get("kind") == "execution", (
            f"exhausting the budget must reuse the execution-failure kind, not "
            f"introduce a new one: {err}"
        )
        assert "query time budget" in err.get("error", ""), (
            f"the reason must name the budget, so the user is not told the "
            f"query was cancelled or that the Cypher was wrong: {err}"
        )

        # The upper bound: the budget cut the work well before the server's
        # 30s WriteTimeout, so the failure was actually written to this client.
        assert elapsed < 15.0, (
            f"the budget must cut the query long before the 30s WriteTimeout; "
            f"the request took {elapsed:.2f}s"
        )
        # The lower bound, and the load-bearing half: the request really did run
        # until the budget stopped it. Without this, a regression that removed
        # the budget and failed the query instantly for any other reason would
        # pass the check above.
        assert elapsed > 3.0, (
            f"the request returned after only {elapsed:.2f}s, far short of the "
            "budget: the failure did not come from the budget expiring"
        )

        # The budget is per request and nothing outlives it: the SAME server
        # process still serves, and serves the whole store.
        status, _, body = self._req(
            port, self._graph_data(port, limit=reader_limit, roadmap=name)
        )
        assert status == 200, (
            f"the server must keep serving after a budget exhaustion; got "
            f"{status}: {body!r}"
        )
        view = json.loads(body)
        assert len(view["nodes"]) == seeded, (
            f"the ordinary read must return the whole {seeded}-node store; got "
            f"{len(view['nodes'])} nodes"
        )
        assert len(view["edges"]) == 1, (
            f"the ordinary read must return the seeded relationship; got "
            f"{len(view['edges'])} edges"
        )

    def test_query_bar_invalid_limit_outranks_not_read_only(self):
        """AC123: one request can be wrong in more than one way at once. The
        endpoint resolves the limit BEFORE it runs the read-only guard rail, so a
        request carrying both an invalid limit and a query that is not read-only
        is answered kind=invalid_limit, never kind=not_read_only. The order in
        which SPEC/WEB.md lists the three failure cases is an order of
        explanation, not an order of precedence (§ Query-Bar Error Handling,
        rule 6). The two single-fault controls keep the assertion honest: without
        them an endpoint that never classified anything as not_read_only would
        pass the combined case."""
        proc, port = self._start(["--port", "0"])
        before = json.loads(self._req(port, self._graph_data(port))[2])
        write_query = "MATCH (n) DELETE n"
        bad_limit = "7"

        # Control A: the query alone is classified not_read_only.
        status, _, body = self._req(
            port, self._graph_data(port, q=write_query, limit="100")
        )
        assert status == 400, "a write query with a valid limit must be rejected"
        assert json.loads(body).get("kind") == "not_read_only", (
            "the write query must reach the guard rail when the limit is valid"
        )

        # Control B: the limit alone is classified invalid_limit.
        status, _, body = self._req(port, self._graph_data(port, limit=bad_limit))
        assert status == 400
        assert json.loads(body).get("kind") == "invalid_limit"

        # The claim: both wrong at once resolves to invalid_limit.
        status, _, body = self._req(
            port, self._graph_data(port, q=write_query, limit=bad_limit)
        )
        assert status == 400, "a doubly invalid request must still be a 400"
        err = json.loads(body)
        assert err.get("kind") == "invalid_limit", (
            "the limit is resolved before the guard rail runs, so an invalid "
            f"limit outranks a query that is not read-only: {err}"
        )
        assert bad_limit in err.get("error", ""), (
            f"the invalid-limit message must name the rejected value: {err}"
        )

        # The query never ran: the DELETE would have emptied the store.
        after = json.loads(self._req(port, self._graph_data(port))[2])
        assert len(after["nodes"]) == len(before["nodes"]), (
            "the request must be rejected before the query runs, so the store "
            "is untouched"
        )

    def test_query_bar_error_body_carries_exactly_error_and_kind(self):
        """AC123: every query-bar failure is answered with a JSON body of exactly
        two string fields, error and kind, and never with the
        {"nodes": ..., "edges": ...} success shape
        (SPEC/DATA_FORMATS.md - Graph View Data, Error Shape, rule 1)."""
        proc, port = self._start(["--port", "0"])
        cases = (
            ("not_read_only", {"q": "MATCH (n) DELETE n"}),
            ("invalid_limit", {"limit": "7"}),
            ("execution", {"q": "MATCH (n) RETURN"}),
        )
        for want_kind, params in cases:
            status, _, body = self._req(port, self._graph_data(port, **params))
            assert status == 400, f"{want_kind}: status {status}, want 400"
            err = json.loads(body)
            assert set(err.keys()) == {"error", "kind"}, (
                f"{want_kind}: failure body fields {sorted(err)}, want exactly "
                "['error', 'kind']"
            )
            assert isinstance(err["error"], str) and err["error"], (
                f"{want_kind}: error must be a non-empty string: {err!r}"
            )
            assert isinstance(err["kind"], str) and err["kind"] == want_kind, (
                f"{want_kind}: kind must be the class name: {err!r}"
            )
            assert "nodes" not in err and "edges" not in err, (
                f"{want_kind}: a failure response carries neither nodes nor "
                f"edges: {err!r}"
            )

    def test_query_bar_extraction_dedup_and_orphan_drop(self):
        """AC49: every returned edge endpoint resolves to a node in the same
        response (orphan edges dropped, ids deduplicated)."""
        proc, port = self._start(["--port", "0"])
        # A query that returns nodes and relationships through a path, exercising
        # the recursive walk and dedup.
        status, _, body = self._req(
            port, self._graph_data(port, q="MATCH p=(a)-[r]->(b) RETURN p")
        )
        assert status == 200
        data = json.loads(body)
        node_ids = {n["id"] for n in data["nodes"]}
        for e in data["edges"]:
            assert e["startId"] in node_ids and e["endId"] in node_ids, (
                "every edge endpoint must resolve to a node in the same response"
            )
        # ids are unique (deduplicated).
        ids = [n["id"] for n in data["nodes"]]
        assert len(ids) == len(set(ids)), "node ids must be deduplicated"

    def test_query_bar_search_stays_get_only(self):
        """AC46: the query bar drives a GET; POST to the data endpoint is 405."""
        proc, port = self._start(["--port", "0"])
        status, _, _ = self._req(port, f"/roadmaps/{ROADMAP}/graph/data", method="POST")
        assert status == 405, "the graph data endpoint must remain GET/HEAD only"

    # ====================================================================
    # AC14: read-only - non-read methods rejected
    # ====================================================================

    def test_write_methods_return_405(self):
        proc, port = self._start(["--port", "0"])
        routes = [
            "/",
            f"/roadmaps/{ROADMAP}",
            f"/roadmaps/{ROADMAP}/tasks",
            f"/roadmaps/{ROADMAP}/audit",
            f"/roadmaps/{ROADMAP}/graph",
            f"/roadmaps/{ROADMAP}/graph/data",
            "/static/style.css",
        ]
        for path in routes:
            for method in ("POST", "PUT", "PATCH", "DELETE"):
                status, _, _ = self._req(port, path, method=method)
                assert status == 405, f"{method} {path} must be 405, got {status}"

    # ====================================================================
    # AC15/AC18/AC19: static assets, self-contained, missing -> 404
    # ====================================================================

    def test_static_assets_served_locally(self):
        proc, port = self._start(["--port", "0"])
        status, headers, _ = self._req(port, "/static/style.css")
        assert status == 200
        assert "css" in headers.get("content-type", "").lower()
        # The vendored D3.js bundle and the d3-sankey plugin are served locally
        # as JavaScript, with non-empty bodies.
        status, headers, body = self._req(port, "/static/vendor/d3/d3.min.js")
        assert status == 200, "the vendored D3.js bundle must be served"
        assert "javascript" in headers.get("content-type", "").lower()
        assert len(body) > 0, "the D3.js bundle must not be empty"
        status, headers, body = self._req(port, "/static/vendor/d3/d3-sankey.min.js")
        assert status == 200, "the vendored d3-sankey plugin must be served"
        assert "javascript" in headers.get("content-type", "").lower()
        assert len(body) > 0, "the d3-sankey plugin must not be empty"
        # The retired Cytoscape bundle is gone (404, not served).
        assert self._req(port, "/static/cytoscape.min.js")[0] == 404, (
            "the retired Cytoscape bundle must no longer be served"
        )
        assert self._req(port, "/static/graph.js")[0] == 200

    def test_missing_static_asset_returns_404(self):
        proc, port = self._start(["--port", "0"])
        assert self._req(port, "/static/does-not-exist.js")[0] == 404

    # ====================================================================
    # AC16/AC20/AC22: no remote origins, viewport meta, mobile-first CSS
    # ====================================================================

    def test_pages_reference_no_remote_origin(self):
        proc, port = self._start(["--port", "0"])
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/audit", f"/roadmaps/{ROADMAP}/graph"):
            _, _, body = self._req(port, path)
            for ref in self._asset_refs(body):
                assert not ref.startswith(("http://", "https://", "//")), (
                    f"page {path} references remote asset {ref!r}"
                )
                assert ref.startswith("/static/") or ref.startswith("/") or ref.startswith("."), (
                    f"page {path} asset {ref!r} is not served locally"
                )
            # No remote font/style host slips in via raw text.
            low = body.lower()
            for bad in ("fonts.googleapis", "cdnjs", "unpkg", "jsdelivr", "//cdn"):
                assert bad not in low, f"page {path} references remote origin {bad!r}"

    def test_every_page_has_viewport_meta(self):
        proc, port = self._start(["--port", "0"])
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/audit", f"/roadmaps/{ROADMAP}/graph"):
            _, _, body = self._req(port, path)
            assert re.search(r'<meta[^>]*name=["\']viewport["\']', body, re.I), (
                f"page {path} missing responsive viewport meta tag"
            )

    def test_stylesheet_is_mobile_first(self):
        proc, port = self._start(["--port", "0"])
        _, _, css = self._req(port, "/static/style.css")
        assert "@media" in css and "min-width" in css, (
            "stylesheet must progressively enhance via min-width media queries"
        )

    # ====================================================================
    # AC23/AC24: Tabler admin-shell layout in the dark theme
    # ====================================================================

    def test_every_page_is_dark_theme(self):
        """AC23: every page renders in Tabler's dark theme.

        Tabler 1.x sets the colour mode with data-bs-theme="dark" on the
        <html> element (Bootstrap 5.3 colour mode). The interface must render
        dark by default with no toggle, so every served page carries that
        attribute on its root element.
        """
        proc, port = self._start(["--port", "0"])
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/audit", f"/roadmaps/{ROADMAP}/graph"):
            _, _, body = self._req(port, path)
            assert re.search(
                r"<html[^>]*\bdata-bs-theme\s*=\s*[\"']dark[\"']", body, re.I
            ), f"page {path} is not in the dark theme (no data-bs-theme=\"dark\" on <html>)"

    def test_every_page_renders_admin_shell(self):
        """AC23/AC24: every page renders the Tabler admin-shell.

        The shell is a vertical navigation sidebar (listing Roadmaps and,
        within a roadmap, that roadmap's views), a page wrapper, a page
        header, and the top navbar. The navbar-toggler + collapse markup is
        what Tabler's JS turns into an off-canvas hamburger menu on small
        viewports (AC24), so its presence is the structural proof of the
        responsive sidebar.

        What the top navbar CARRIES is roadmap-dependent — the selected
        roadmap's name, and nothing at all on the roadmap index page — so it
        is covered by test_top_navbar_names_the_selected_roadmap rather than
        by this sweep, which asserts only what every page shares.
        """
        proc, port = self._start(["--port", "0"])
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/audit", f"/roadmaps/{ROADMAP}/graph"):
            _, _, body = self._req(port, path)
            for marker in (
                "navbar-vertical",   # vertical sidebar
                "page-wrapper",      # content wrapper
                "page-header",       # per-page header
                "navbar-toggler",    # hamburger / off-canvas toggle
                "Roadmaps",          # always-present sidebar link
                '<header class="navbar navbar-expand-md d-print-none">',  # top navbar
            ):
                assert marker in body, f"page {path} missing admin-shell marker {marker!r}"

    @staticmethod
    def _top_navbar(path, body):
        """Return the markup between the top navbar's <header> and </header>.

        Assertions about the navbar are made on this region, never on the whole
        document: the roadmap index page legitimately writes "Read-only view of
        the roadmaps under ~/.roadmaps/" in its page header, and the roadmap
        name appears in the sidebar and the page header of every roadmap-scoped
        page, so a document-wide check would prove nothing either way.
        """
        opening = '<header class="navbar navbar-expand-md d-print-none">'
        start = body.find(opening)
        assert start >= 0, f"page {path} renders no top navbar, so any assertion on it would be vacuous"
        rest = body[start + len(opening):]
        end = rest.find("</header>")
        assert end >= 0, f"page {path}: the top navbar is never closed"
        return rest[:end]

    @staticmethod
    def _page_header(path, body):
        """Return the page-header block, from its opening div to the <main>."""
        opening = '<div class="page-header d-print-none">'
        start = body.find(opening)
        assert start >= 0, f"page {path} renders no page header, so any assertion on it would be vacuous"
        rest = body[start:]
        end = rest.find('<main class="page-body">')
        assert end >= 0, f"page {path}: no page body follows the page header"
        return rest[:end]

    def test_page_header_is_uniform_across_pages(self):
        """AC109: one shared partial renders every page header's title column.

        The title names the VIEW, never the roadmap: the sidebar and the top
        navbar already state the roadmap, so a third statement would repeat
        what the user can see while leaving the view unnamed. Only a sprint's
        own page is hierarchical, with the pretitle Sprint #<id>. The actions
        column holds controls that act on the page, plus the sprint page's
        back link - never navigation the sidebar already carries.
        """
        proc, port = self._start(["--port", "0"])

        titles = {
            "/": "Roadmaps",
            f"/roadmaps/{ROADMAP}": "Sprints",
            f"/roadmaps/{ROADMAP}/tasks": "Tasks",
            f"/roadmaps/{ROADMAP}/audit": "Audit",
            f"/roadmaps/{ROADMAP}/graph": "Knowledge graph",
        }
        for path, title in titles.items():
            _, _, body = self._req(port, path)
            header = self._page_header(path, body)
            assert f'<h2 class="page-title">{title}</h2>' in header, (
                f"page {path}: header title is not {title!r}; header={header!r}"
            )
            assert "page-pretitle" not in header, (
                f"page {path}: renders a pretitle; only a sprint's own page carries one"
            )
            assert ROADMAP not in header, (
                f"page {path}: the header names the roadmap, which the sidebar and the "
                f"top navbar already state; header={header!r}"
            )

        # No header offers a second route to the knowledge graph, and the
        # retired "Tasks & sprints" label is gone from the graph page too.
        for path in list(titles):
            _, _, body = self._req(port, path)
            header = self._page_header(path, body)
            assert '/graph"' not in header, (
                f"page {path}: header links to the knowledge-graph page, duplicating the sidebar"
            )
            assert "Tasks &amp; sprints" not in header and "Tasks & sprints" not in header, (
                f"page {path}: header still shows the retired \"Tasks & sprints\" label"
            )

        # Only three headers carry an actions column, and each holds what it
        # should: a control, or the sprint page's hierarchical back link.
        _, _, tasks_body = self._req(port, f"/roadmaps/{ROADMAP}/tasks")
        assert 'data-role="task-search"' in self._page_header(f"/roadmaps/{ROADMAP}/tasks", tasks_body)
        _, _, graph_body = self._req(port, f"/roadmaps/{ROADMAP}/graph")
        assert 'id="layout-select"' in self._page_header(f"/roadmaps/{ROADMAP}/graph", graph_body)

        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/audit"):
            _, _, body = self._req(port, path)
            header = self._page_header(path, body)
            assert "col-auto ms-auto" not in header, (
                f"page {path}: header carries an actions column; it should have none; header={header!r}"
            )

    def test_top_navbar_names_the_selected_roadmap(self):
        """AC108: the top navbar names the roadmap the page belongs to.

        Every roadmap-scoped page shows that roadmap's name in the navbar,
        prominently (the Tabler `h3` type utility) and standing alone, with no
        glyph beside it; the roadmap index page belongs to no roadmap and
        renders the region empty. No page's navbar carries the badge that used to
        declare the interface read-only: that the server never writes is
        covered by the 405 and no-write-affordance scenarios, not by a label.
        """
        proc, port = self._start(["--port", "0"])

        named = f'<span class="h3 mb-0 text-truncate" data-role="active-roadmap">{ROADMAP}</span>'
        for path in (
            f"/roadmaps/{ROADMAP}",
            f"/roadmaps/{ROADMAP}/tasks",
            f"/roadmaps/{ROADMAP}/audit",
            f"/roadmaps/{ROADMAP}/graph",
        ):
            _, _, body = self._req(port, path)
            navbar = self._top_navbar(path, body)
            assert named in navbar, f"page {path}: top navbar does not name its roadmap; navbar={navbar!r}"
            assert "<i " not in navbar and "ti-" not in navbar, (
                f"page {path}: top navbar carries an icon beside the roadmap name; "
                f"the name stands alone; navbar={navbar!r}"
            )

        _, _, index_body = self._req(port, "/")
        index_navbar = self._top_navbar("/", index_body)
        for unwanted in ('data-role="active-roadmap"', ROADMAP, "<span", "<i "):
            assert unwanted not in index_navbar, (
                f"the roadmap index page belongs to no roadmap, so its top navbar must render "
                f"empty, but it carries {unwanted!r}; navbar={index_navbar!r}"
            )

        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/audit", f"/roadmaps/{ROADMAP}/graph"):
            _, _, body = self._req(port, path)
            navbar = self._top_navbar(path, body)
            for gone in ("Read-only", "read-only", "ti-lock", "badge"):
                assert gone not in navbar, (
                    f"page {path}: top navbar carries {gone!r}; the read-only indicator was "
                    f"replaced by the selected roadmap's name; navbar={navbar!r}"
                )

    def test_roadmap_pages_link_sprints_tasks_graph_in_sidebar(self):
        """A roadmap's pages surface its Sprints/Tasks/Graph in the sidebar, each
        resolving to its own endpoint (no #anchors on a combined page):
        Sprints -> /roadmaps/{name}, Tasks -> /roadmaps/{name}/tasks,
        Graph -> /roadmaps/{name}/graph (SPEC/WEB.md § UI Framework)."""
        proc, port = self._start(["--port", "0"])
        for path in (
            f"/roadmaps/{ROADMAP}",
            f"/roadmaps/{ROADMAP}/tasks",
            f"/roadmaps/{ROADMAP}/audit",
            f"/roadmaps/{ROADMAP}/graph",
        ):
            _, _, body = self._req(port, path)
            assert f'href="/roadmaps/{ROADMAP}"' in body, "sidebar must link the roadmap's Sprints (landing)"
            assert f'href="/roadmaps/{ROADMAP}/tasks"' in body, "sidebar must link the roadmap's Tasks"
            assert f'href="/roadmaps/{ROADMAP}/audit"' in body, "sidebar must link the roadmap's Audit"
            assert f'href="/roadmaps/{ROADMAP}/graph"' in body, "sidebar must link the roadmap's Graph"
            # The old combined-page anchors must be gone.
            assert f"/roadmaps/{ROADMAP}#tasks" not in body, "stale #tasks anchor must be removed"
            assert f"/roadmaps/{ROADMAP}#sprints" not in body, "stale #sprints anchor must be removed"

    def test_vendored_tabler_and_fonts_served_locally(self):
        """AC16/AC22: the vendored Tabler framework and fonts are served from /static/.

        The Tabler CSS framework is served with the correct text/css content
        type (so a nosniff client does not block it), the Tabler JS is served,
        and the Inter and Tabler Icons web fonts are served — all locally, no
        remote origin.
        """
        proc, port = self._start(["--port", "0"])

        status, headers, _ = self._req(port, "/static/vendor/tabler/tabler.min.css")
        assert status == 200, "vendored Tabler CSS must be served"
        assert "text/css" in headers.get("content-type", "").lower(), (
            "Tabler CSS must be served as text/css"
        )

        assert self._req(port, "/static/vendor/tabler/tabler.min.js")[0] == 200, (
            "vendored Tabler JS must be served"
        )
        assert self._req(port, "/static/vendor/tabler-icons/tabler-icons.min.css")[0] == 200
        assert self._req(port, "/static/vendor/inter/files/inter-latin-wght-normal.woff2")[0] == 200, (
            "the Inter web font must be served"
        )
        assert self._req(port, "/static/vendor/tabler-icons/fonts/tabler-icons.woff2")[0] == 200, (
            "the Tabler Icons web font must be served"
        )

    def test_pages_load_vendored_tabler_assets(self):
        """AC16/AC22: every page loads the vendored Tabler CSS/JS from /static/."""
        proc, port = self._start(["--port", "0"])
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/audit", f"/roadmaps/{ROADMAP}/graph"):
            _, _, body = self._req(port, path)
            assert "/static/vendor/tabler/tabler.min.css" in body, (
                f"page {path} must load the vendored Tabler CSS"
            )
            assert "/static/vendor/tabler/tabler.min.js" in body, (
                f"page {path} must load the vendored Tabler JS (for the off-canvas sidebar)"
            )
            assert "/static/vendor/inter/inter.css" in body, (
                f"page {path} must load the vendored Inter font CSS"
            )

    def test_stylesheet_links_are_local(self):
        """AC22: no page loads a CSS framework/reset from a remote origin.

        Every <link rel=stylesheet href> must be a same-origin /static/ URL.
        """
        proc, port = self._start(["--port", "0"])
        link_re = re.compile(
            r'<link[^>]*\brel=["\']?stylesheet["\']?[^>]*\bhref=["\']([^"\']+)["\']',
            re.I,
        )
        # Also catch href-before-rel ordering.
        link_re2 = re.compile(
            r'<link[^>]*\bhref=["\']([^"\']+)["\'][^>]*\brel=["\']?stylesheet["\']?',
            re.I,
        )
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/audit", f"/roadmaps/{ROADMAP}/graph"):
            _, _, body = self._req(port, path)
            hrefs = link_re.findall(body) + link_re2.findall(body)
            assert hrefs, f"page {path} declares no stylesheet link"
            for href in hrefs:
                assert href.startswith("/static/"), (
                    f"page {path} stylesheet {href!r} is not served from /static/"
                )

    def test_tasks_text_is_html_escaped(self):
        # Output escaping (SPEC Security): roadmap-derived text cannot inject markup.
        # The task is in BACKLOG (no sprint), so it surfaces on the tasks page,
        # as a card in the board's BACKLOG column (SPEC/WEB.md § Roadmap Tasks Page).
        self._run(["roadmap", "create", "escaping_demo"])
        self.test.create_task(
            "escaping_demo",
            "<script>alert(1)</script>",
            "why", "how", "verify",
        )
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, "/roadmaps/escaping_demo/tasks")
        assert "<script>alert(1)</script>" not in body, "task title must be escaped"
        assert "&lt;script&gt;" in body, "title must appear HTML-escaped"

        # The title also travels inside an ATTRIBUTE — the trigger's accessible
        # name — where an unescaped double quote would close the attribute and
        # turn the rest of the title into markup. A title is free text written
        # through the CLI, so every character with meaning in an attribute is
        # exercised here: the delimiter, the angle brackets, the ampersand, an
        # apostrophe, and an already-escaped entity that must not be decoded.
        hostile = "Reject \"quoted\" <b>bold</b> & O'Brien &amp; 100% > 50%"
        task_id = self.test.create_task(
            "escaping_demo", hostile, "why", "how", "verify",
        )
        sprint_id = self.test.create_sprint("escaping_demo", "Escaping regression sprint")
        self._run(["sprint", "add-tasks", "-r", "escaping_demo", str(sprint_id), str(task_id)])

        for path in (
            "/roadmaps/escaping_demo/tasks",
            f"/roadmaps/escaping_demo/sprints/{sprint_id}",
        ):
            _, _, body = self._req(port, path)

            # Nothing of the raw title reaches the page.
            assert hostile not in body, f"{path}: the raw title reached the page unescaped"
            for raw in ('<b>bold</b>', '"quoted"'):
                assert raw not in body, f"{path}: {raw!r} reached the page unescaped"

            # The accessible name is a well-formed attribute value that decodes
            # back to exactly what the user wrote. The extraction is bounded by
            # the quote characters, so a label that had swallowed a stray quote
            # would come back truncated and fail to decode.
            labels = [
                m for m in re.findall(r'aria-label="([^"]*)"', body)
                if m.startswith(f"Open details for task #{task_id}:")
            ]
            assert labels, f"{path}: no accessible name for task #{task_id} survived extraction"
            for label in labels:
                assert html_lib.unescape(label) == f"Open details for task #{task_id}: {hostile}", (
                    f"{path}: the accessible name decodes to {html_lib.unescape(label)!r}"
                )

            # And the markup kept its shape: the page carries exactly one modal
            # shell, not fragments produced by a broken attribute.
            assert body.count('id="task-modal"') == 1, (
                f"{path}: the hostile title broke the markup"
            )

    # ====================================================================
    # AC17: graceful shutdown on SIGINT / SIGTERM
    # ====================================================================

    def test_sigint_shuts_down_with_exit_0(self):
        proc, port = self._start(["--port", "0"])
        assert self._req(port, "/")[0] == 200
        code = self._stop(proc, signal.SIGINT)
        assert code == 0, f"SIGINT must exit 0 (graceful), got {code}"

    def test_sigterm_shuts_down_with_exit_0(self):
        proc, port = self._start(["--port", "0"])
        assert self._req(port, "/")[0] == 200
        code = self._stop(proc, signal.SIGTERM)
        assert code == 0, f"SIGTERM must exit 0 (graceful), got {code}"

    # ====================================================================
    # AC141-AC146: server logging on the console (SPEC/WEB.md § Server Logging)
    # ====================================================================

    # One slog TextHandler record: time=... level=LEVEL msg="..." key=value ...
    _LOG_RECORD = re.compile(
        r'^time=(?P<time>\S+) level=(?P<level>[A-Z]+) msg=(?P<msg>"[^"]*"|\S+)(?P<rest>.*)$'
    )
    _CANONICAL_STAMP = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")

    @classmethod
    def _log_records(cls, stderr):
        """Parse a server's stderr into slog records.

        Every record is one line by construction (SPEC/WEB.md § Log Integrity),
        so a line that does not parse is a line the server wrote outside the
        logger, which the caller can then assert about.
        """
        records = []
        for line in stderr.splitlines():
            if not line.strip():
                continue
            m = cls._LOG_RECORD.match(line)
            records.append(
                {
                    "raw": line,
                    "time": m.group("time") if m else None,
                    "level": m.group("level") if m else None,
                    "msg": m.group("msg").strip('"') if m else None,
                    "rest": m.group("rest") if m else "",
                }
            )
        return records

    @classmethod
    def _assert_canonical_utc(cls, record):
        """Every record's timestamp is UTC in YYYY-MM-DDTHH:mm:ss.sssZ (AC144)."""
        stamp = record["time"]
        assert stamp is not None, f"record is not a slog record: {record['raw']!r}"
        assert cls._CANONICAL_STAMP.match(stamp), (
            "log timestamp must be UTC as YYYY-MM-DDTHH:mm:ss.sssZ; "
            f"got {stamp!r} in record {record['raw']!r}"
        )

    def test_startup_network_warning_is_a_slog_warn_record(self):
        """AC146: the non-loopback bind warning is a WARN record, not an ad-hoc
        `warning: ` line, and it still names the bound host. AC141: stdout stays
        clean, carrying only the startup URL object.
        """
        out = tempfile.TemporaryFile(mode="w+")
        err = tempfile.TemporaryFile(mode="w+")
        proc = subprocess.Popen(
            [self.cli, "web", "--no-open", "--host", "0.0.0.0", "--port", "0"],
            stdout=out, stderr=err, text=True, env=self._env(),
        )
        proc.out_file = out
        proc.err_file = err
        self._procs.append(proc)

        url = self._read_startup_url(proc)
        assert url is not None, f"server did not start; stderr={self._drain(err)}"

        records = self._log_records(self._drain(err))
        exposure = [r for r in records
                    if r["msg"] == "web interface is reachable from the network"]
        assert len(exposure) == 1, (
            "exactly one network-exposure record expected; "
            f"got {len(exposure)} in {[r['raw'] for r in records]}"
        )
        rec = exposure[0]
        assert rec["level"] == "WARN", f"level = {rec['level']!r}, want WARN: {rec['raw']!r}"
        assert "host=0.0.0.0" in rec["rest"], f"record must name the bound host: {rec['raw']!r}"
        assert "warning:" not in rec["raw"], (
            f"the ad-hoc warning prefix must be gone: {rec['raw']!r}"
        )
        self._assert_canonical_utc(rec)

        # Stdout carries the startup object and nothing else: a caller reading
        # stdout for the URL must never receive a log record.
        stdout = self._drain(out)
        assert json.loads(stdout) == {"url": url}, (
            f"stdout must be exactly the startup URL object, got {stdout!r}"
        )

        code = self._stop(proc, signal.SIGTERM)
        assert code == 0, f"graceful SIGTERM shutdown must exit 0, got {code}"

    def test_server_error_is_logged_as_a_slog_error_record(self):
        """AC141: every HTTP 500 the running server returns is accompanied by
        exactly one ERROR record naming the request and the underlying error,
        while the response body itself stays opaque.

        The roadmap's database is replaced with non-database bytes AFTER the
        server has started, so the failure is genuinely a per-request read
        failure rather than anything the startup sweep could have reported.
        """
        proc, port = self._start(["--port", "0"])
        assert self._req(port, f"/roadmaps/{ROADMAP}")[0] == 200, "precondition: page serves"

        db_path = Path(self.home) / ".roadmaps" / ROADMAP / "project.db"
        db_path.write_bytes(b"these are not the bytes of a SQLite database")

        status, _, body = self._req(port, f"/roadmaps/{ROADMAP}")
        assert status == 500, f"a corrupt database must yield 500, got {status}"
        assert "internal server error" in body.lower(), (
            f"the response must stay opaque, got {body!r}"
        )
        # The detail belongs on the console, never in the response.
        assert "project.db" not in body and "SQLite" not in body, (
            f"the response leaked internal detail: {body!r}"
        )

        records = self._log_records(self._drain(proc.err_file))
        errors = [r for r in records if r["level"] == "ERROR"]
        assert len(errors) == 1, (
            f"exactly one ERROR record expected; got {[r['raw'] for r in records]}"
        )
        rec = errors[0]
        assert rec["msg"] == "sprints page load failed", f"msg = {rec['msg']!r}"
        for fragment in ("method=GET", f"path=/roadmaps/{ROADMAP}",
                         f"roadmap={ROADMAP}", "status=500", "err="):
            assert fragment in rec["raw"], (
                f"record is missing {fragment!r}: {rec['raw']!r}"
            )
        self._assert_canonical_utc(rec)

    def test_graph_query_bar_rejection_is_logged_as_a_slog_warn_record(self):
        """AC142: the graph data endpoint's 400 is a WARN, not an ERROR — the
        user's query failed, not the server — and the record carries the same
        failure kind the JSON response body carries.
        """
        proc, port = self._start(["--port", "0"])
        query = urllib.parse.quote("CREATE (n:Injected {via:'query bar'}) RETURN n")
        status, _, body = self._req(port, f"/roadmaps/{ROADMAP}/graph/data?q={query}")
        assert status == 400, f"a writing query must be rejected with 400, got {status}"
        payload = json.loads(body)
        assert payload["kind"] == "not_read_only", f"unexpected kind: {payload!r}"

        records = self._log_records(self._drain(proc.err_file))
        assert not [r for r in records if r["level"] == "ERROR"], (
            f"a rejected user query is not a server error: {[r['raw'] for r in records]}"
        )
        warns = [r for r in records if r["level"] == "WARN"]
        assert len(warns) == 1, (
            f"exactly one WARN record expected; got {[r['raw'] for r in records]}"
        )
        rec = warns[0]
        assert rec["msg"] == "graph query bar request failed", f"msg = {rec['msg']!r}"
        for fragment in ("method=GET", f"roadmap={ROADMAP}",
                         "kind=not_read_only", "status=400", "err="):
            assert fragment in rec["raw"], (
                f"record is missing {fragment!r}: {rec['raw']!r}"
            )
        self._assert_canonical_utc(rec)

        # Console and page agree on the classification.
        assert f'kind={payload["kind"]}' in rec["raw"]

    def test_ordinary_outcomes_leave_the_console_silent(self):
        """AC143: a successful request, a 404 and a 405 write no record at all.

        Logging them would bury the genuine failures under every mistyped URL
        and every browser probe for an asset the server does not serve.
        """
        proc, port = self._start(["--port", "0"])

        probes = [
            ("GET", "/", 200),
            ("GET", f"/roadmaps/{ROADMAP}", 200),
            ("GET", f"/roadmaps/{ROADMAP}/tasks", 200),
            ("GET", f"/roadmaps/{ROADMAP}/audit", 200),
            ("GET", "/roadmaps/no-such-roadmap", 404),
            ("GET", "/favicon.ico", 404),
            ("GET", "/no/such/page", 404),
            ("GET", f"/roadmaps/{ROADMAP}/sprints/999999", 404),
            ("GET", f"/roadmaps/{ROADMAP}/tasks/999999/data", 404),
            ("POST", f"/roadmaps/{ROADMAP}", 405),
            ("DELETE", "/", 405),
        ]
        for method, path, want in probes:
            status = self._req(port, path, method=method)[0]
            assert status == want, f"{method} {path} = {status}, want {want}"

        stderr = self._drain(proc.err_file)
        assert stderr.strip() == "", (
            "successful requests, 404s and 405s must leave the console silent; "
            f"stderr={stderr!r}"
        )

    def test_log_timestamps_are_utc_whatever_the_process_timezone(self):
        """AC144: the timestamp is the real UTC instant, not the local wall
        clock relabelled.

        The server runs under a fixed +09:00 zone, which slog's TextHandler
        would otherwise stamp as a local time with a numeric offset. The record
        must still be UTC, within a minute of this machine's own UTC clock.
        """
        env = self._env()
        env["TZ"] = "Asia/Tokyo"

        out = tempfile.TemporaryFile(mode="w+")
        err = tempfile.TemporaryFile(mode="w+")
        before = datetime.now(timezone.utc)
        proc = subprocess.Popen(
            [self.cli, "web", "--no-open", "--host", "0.0.0.0", "--port", "0"],
            stdout=out, stderr=err, text=True, env=env,
        )
        proc.out_file = out
        proc.err_file = err
        self._procs.append(proc)

        url = self._read_startup_url(proc)
        assert url is not None, f"server did not start; stderr={self._drain(err)}"
        after = datetime.now(timezone.utc)

        records = self._log_records(self._drain(err))
        assert records, "the non-loopback bind must have produced a record"
        rec = records[0]
        self._assert_canonical_utc(rec)

        stamp = datetime.strptime(rec["time"], "%Y-%m-%dT%H:%M:%S.%fZ").replace(
            tzinfo=timezone.utc
        )
        margin = timedelta(minutes=1)
        assert before - margin <= stamp <= after + margin, (
            "the timestamp is not the real UTC instant: under TZ=Asia/Tokyo a "
            f"local reading would be nine hours out. got {stamp.isoformat()}, "
            f"window [{before.isoformat()}, {after.isoformat()}]"
        )

        code = self._stop(proc, signal.SIGTERM)
        assert code == 0, f"graceful SIGTERM shutdown must exit 0, got {code}"


def _run_all():
    cls = TestWebInterface
    methods = sorted(m for m in dir(cls) if m.startswith("test_"))
    passed = 0
    failed = 0
    failures = []
    for name in methods:
        inst = cls()
        inst.setup_method()
        try:
            getattr(inst, name)()
            passed += 1
            print(f"✓ {name}")
        except AssertionError as exc:
            failed += 1
            failures.append((name, exc))
            print(f"✗ {name}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((name, exc))
            print(f"✗ {name} (error: {type(exc).__name__})")
        finally:
            inst.teardown_method()
    print("\n" + "=" * 60)
    print(f"Web interface tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
