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
  read-only indicator, and the off-canvas hamburger markup; the vendored
  Tabler CSS/JS and the Inter / Tabler Icons web fonts are served locally
  from /static/ (AC23/AC24, AC16/AC22).

The server is long-lived, so each scenario launches a fresh `rmp web`
process on an ephemeral port (--port 0), parses the startup URL from
stdout, drives it over raw http.client requests (no client-side path
normalisation, so the traversal guard is genuinely exercised), and then
terminates it. Roadmap data and a populated knowledge graph are built
through the real CLI so the pages render production-shaped content.
"""

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
        # The full task table (its 15-column header) belongs to the tasks page,
        # not the sprints page. The <th>Specialists</th> header is unique to the
        # full table (the modal's "Specialists" datagrid title is not a <th>).
        assert "<th>Specialists</th>" not in body, (
            "the sprints page must NOT render the full task table"
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

    def test_tasks_page_shows_full_table_read_only(self):
        """The tasks page renders the roadmap's full task table — every task of
        any status — with the read-only task detail modal on each row, and does
        NOT render the sprint tabs (SPEC/WEB.md § Roadmap Tasks Page)."""
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}/tasks")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")
        # The full 15-column task table is present.
        assert "<th>Specialists</th>" in body, "tasks page must render the full task table"
        # Every task, any status — including the PENDING-sprint task that the
        # sprints page does not show.
        low = body.lower()
        for title in (
            "implement passwordless login",
            "rate-limit the token endpoint",
            "add webauthn passkey support",
            "audit the session-cookie flags",
        ):
            assert title in low, f"tasks page must list task {title!r}"
        # A clickable row opens the read-only modal for the task.
        t1 = self.open_task_ids[0]
        assert f'data-bs-target="#task-modal-{t1}"' in body, "tasks page missing modal trigger"
        assert f'id="task-modal-{t1}"' in body, "tasks page missing modal element"
        # The tasks page does NOT render the sprint tabs/panes.
        assert 'id="tab-current"' not in body, "tasks page must not render the sprint tabs"
        # Read-only: no edit affordance.
        assert "<form" not in low, "tasks page must contain no form"
        assert "<input" not in low, "tasks page must contain no input"
        assert 'type="submit"' not in low, "tasks page must contain no submit"

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
        _, _, body = self._req(port, "/roadmaps/linebreaks_demo/tasks")
        assert "task-modal__text" in body, "task long-text must use the preserving class"
        assert multiline_fr in body, "task free-text must preserve the author's line breaks"

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
        # The task detail modal appears on the tasks page and the single sprint
        # page. The sprints landing page renders compact sprint cards only and
        # opens no task detail modal (SPEC/WEB.md § Task Detail Modal, § Shared
        # Sprint-Card Partial; Acceptance Criteria 8/15/38).
        for path in (
            f"/roadmaps/{ROADMAP}/tasks",
            f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}",
        ):
            _, _, body = self._req(port, path)
            # A clickable control toggles a Bootstrap modal targeting the task's modal.
            assert 'data-bs-toggle="modal"' in body, f"{path}: no modal-toggling control"
            assert f'data-bs-target="#task-modal-{t1}"' in body, (
                f"{path}: missing modal trigger for task #{t1}"
            )
            # The matching modal element exists.
            assert f'id="task-modal-{t1}"' in body, f"{path}: missing modal element for task #{t1}"
            # The modal shows the long free-text sections.
            for section in (
                "Functional requirements",
                "Technical requirements",
                "Acceptance criteria",
                "Completion summary",
            ):
                assert section in body, f"{path}: task modal missing section {section!r}"
            # The functional-requirement text of the OPEN-sprint task is present.
            assert "end users must authenticate without a stored password" in body.lower(), (
                f"{path}: task modal missing the task's functional-requirements text"
            )
            # Read-only: no form/input/submit.
            low = body.lower()
            assert "<form" not in low, f"{path}: modal page must contain no form"
            assert "<input" not in low, f"{path}: modal page must contain no input"
            assert 'type="submit"' not in low, f"{path}: modal page must contain no submit"

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
        header, and a top-navbar read-only indicator. The navbar-toggler +
        collapse markup is what Tabler's JS turns into an off-canvas hamburger
        menu on small viewports (AC24), so its presence is the structural
        proof of the responsive sidebar.
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
                "Read-only",         # top-navbar indicator
            ):
                assert marker in body, f"page {path} missing admin-shell marker {marker!r}"

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
        # which renders the full task table (SPEC/WEB.md § Roadmap Tasks Page).
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
