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
  the full task table) — both with no edit affordance and no audit-log
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
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase

ROADMAP = "platform"


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
    def _req(port, path, method="GET", host="127.0.0.1"):
        conn = http.client.HTTPConnection(host, port, timeout=5)
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
        try:
            self._occupy(port=8787)
        except OSError:
            # 8787 already taken by something else in this environment.
            print("  (skipped: port 8787 not bindable here)")
            return
        proc, port = self._start()  # no --port -> default 8787 busy -> ephemeral
        assert port != 8787, "default-port fallback must choose a different port"
        status, _, _ = self._req(port, "/")
        assert status == 200

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
        assert "<th>Specialists</th>" not in body, (
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

    @staticmethod
    def _column_header(column):
        """Return the (status, count) a column header shows."""
        m = re.search(
            r'<h3 class="card-title">([A-Z]+) '
            r'<span class="badge bg-secondary-lt ms-2">(\d+)</span></h3>',
            column,
        )
        assert m, "a board column has no Tabler card-title header with a count badge"
        return m.group(1), int(m.group(2))

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
        assert "<th>Specialists</th>" not in body, "the tasks page must render no task table"
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
        self._run(["task", "stat", "-r", roadmap, str(doing), "DOING"])

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

    def test_sprint_page_row_stays_clickable_by_pointer(self):
        """Moving the keyboard trigger onto the task title left the row clickable
        by pointer: the row keeps the modal data attributes, and both it and the
        title button point at the same modal (SPEC/WEB.md § Sprint Detail
        Sub-Template)."""
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}")

        t1 = self.open_task_ids[0]
        marker = f'data-task-id="{t1}"'
        assert f'<tr class="task-row" data-bs-toggle="modal" data-bs-target="#task-modal" {marker}>' in body, (
            "the member-task row is no longer clickable by pointer"
        )
        title = self._rendered_task_title(body, t1)
        assert (
            f'<button type="button" class="task-row__trigger p-0 border-0 bg-transparent '
            f'text-reset text-start" data-bs-toggle="modal" data-bs-target="#task-modal" {marker} '
            f'aria-label="Open details for task #{t1}: {title}">{title}</button>'
        ) in body, "the member-task title is not the row's keyboard trigger"
        assert body.count(marker) == 2, (
            "the row and its title trigger must both carry the task id"
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
        contract: fold the term with trim()+toLowerCase(), then match it against
        the corpus the server folded into data-search, or against '#<id>'. The
        script is asserted to be that rule in
        test_tasks_page_search_script_is_text_only_and_locale_independent."""
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
            folded = term.strip().lower()

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

        # Locale-independent folding on the client, matching the server's.
        assert "toLowerCase()" in code, "the script does not fold the term"
        assert "toLocaleLowerCase" not in code, (
            "the script folds with a locale-sensitive conversion; the same term would "
            "then select different tasks for different viewers"
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
            "specialists", "completion_summary", "parent_task_id", "subtask_count",
            "depends_on", "blocks", "created_at", "started_at", "tested_at", "closed_at",
        ):
            assert field in detail["task"], f"the task detail carries no {field!r}"
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
    def _graph_data(port, q=None, limit=None):
        """Build /graph/data with URL-encoded q and limit, GET it, return JSON."""
        params = {}
        if q is not None:
            params["q"] = q
        if limit is not None:
            params["limit"] = limit
        path = f"/roadmaps/{ROADMAP}/graph/data"
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

    def test_query_bar_execution_failure_distinct_from_rejection(self):
        """AC50: a read-only query that fails in the engine (invalid syntax)
        surfaces kind=execution, distinct from a read-only rejection."""
        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, self._graph_data(port, q="MATCH (n) RETURN"))
        assert status == 400
        assert json.loads(body).get("kind") == "execution", (
            "an execution failure must be distinct from a read-only rejection"
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

    def test_top_navbar_names_the_selected_roadmap(self):
        """AC108: the top navbar names the roadmap the page belongs to.

        Every roadmap-scoped page shows that roadmap's name in the navbar,
        prominently (the Tabler `h3` type utility) and preceded by a Tabler
        Icons glyph; the roadmap index page belongs to no roadmap and renders
        the region empty. No page's navbar carries the badge that used to
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
            assert '<i class="ti ti-map me-2"></i>' in navbar, (
                f"page {path}: top navbar carries no roadmap glyph before the name; navbar={navbar!r}"
            )

        _, _, index_body = self._req(port, "/")
        index_navbar = self._top_navbar("/", index_body)
        for unwanted in ('data-role="active-roadmap"', "ti-map", ROADMAP, "<span", "<i "):
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
