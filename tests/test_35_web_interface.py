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

        The default bind host is now 0.0.0.0 (all interfaces), which would
        expose a network-listening socket for the duration of every running-
        server scenario (SPEC/WEB.md § Bind Address and Port Selection). These
        route/lifecycle scenarios only need a reachable server, not network
        exposure, so unless the caller already pins --host we restrict the
        listener to loopback (the documented --host 127.0.0.1 opt-in). The
        default-host behaviour itself is asserted separately, on the printed
        URL, in test_default_host_is_all_interfaces.
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
    # AC1/AC2: startup URL object; all-interfaces default with loopback opt-in
    # ====================================================================

    def test_startup_prints_url_object_and_serves(self):
        # _start pins --host 127.0.0.1 (loopback opt-in) so the suite does not
        # leave an all-interfaces listener bound; the default-host value is
        # asserted on the printed URL in test_default_host_is_all_interfaces.
        proc, port = self._start(["--port", "0"])
        # AC1: the URL reflects the actual bind.
        status, _, _ = self._req(port, "/")
        assert status == 200
        # AC2: server is up without a browser (we passed --no-open); URL printed.
        assert port > 0

    def test_default_host_is_all_interfaces(self):
        """AC1: with no --host the printed URL host is 0.0.0.0 (all interfaces).

        We start the server with the default host but an explicit ephemeral
        --port 0 (so the test does not race the real 8787), read the printed
        startup URL, assert its host component, and immediately stop the
        server. The assertion is on the printed URL, and the all-interfaces
        listener exists only for the brief window before teardown signals the
        process, so the suite does not leave a network-listening socket bound.
        """
        # Bypass _start's loopback pin: launch directly with no --host so the
        # process resolves the real default. Reuse the same stdout-file polling.
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
        assert host == "0.0.0.0", (
            f"default bind host must be 0.0.0.0 (all interfaces); got {host!r} "
            f"from url {url!r}"
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

    def test_sprints_page_shows_sprints_not_full_task_table(self):
        """The sprints landing page renders the sprints (and the Actual tab's
        OPEN-sprint task list) but NOT the full task table, which now lives at
        /roadmaps/{name}/tasks (SPEC/WEB.md § Roadmap Sprints Page)."""
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")
        assert "authentication hardening sprint" in body.lower(), "must show sprints"
        # The OPEN sprint's member task surfaces under the Actual tab.
        assert "passwordless login" in body.lower(), (
            "Actual tab must show the OPEN sprint's task title"
        )
        # The full task table (its 15-column header) belongs to the tasks page,
        # not the sprints page. The <th>Specialists</th> header is unique to the
        # full table (the modal's "Specialists" datagrid title is not a <th>).
        assert "<th>Specialists</th>" not in body, (
            "the sprints page must NOT render the full task table"
        )
        # The PENDING-sprint task is not a member of any OPEN sprint, so it must
        # not appear on the sprints page (it appears on the tasks page).
        assert "add webauthn passkey support" not in body.lower(), (
            "non-OPEN-sprint tasks must not appear on the sprints page"
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

        # PENDING -> Próximos, OPEN -> Actual (with task statuses), CLOSED -> Concluídos.
        assert f"/sprints/{self.pending_sid}" in upcoming, "PENDING sprint not under Próximos"
        assert f"/sprints/{self.open_sid}" in current, "OPEN sprint not under Actual"
        assert f"/sprints/{self.closed_sid}" in closed, "CLOSED sprint not under Concluídos"

        # The Actual tab shows the OPEN sprint's task statuses.
        assert "passwordless login" in current.lower(), (
            "Actual tab must show the OPEN sprint's task title"
        )
        assert ">SPRINT</span>" in current, (
            "Actual tab must show each OPEN-sprint task's status"
        )

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

    # ====================================================================
    # AC14: read-only task detail modal — wiring, content, no edit control
    # ====================================================================

    def test_task_modal_wiring_and_content(self):
        proc, port = self._start(["--port", "0"])
        t1 = self.open_task_ids[0]
        for path in (
            f"/roadmaps/{ROADMAP}",
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
        # Mobile patent suits is the default selected option (exactly one preselected).
        assert re.search(
            r'<option value="patents"[^>]*\bselected\b', body, re.I
        ), "Mobile patent suits must be the preselected default layout"
        assert body.count("selected>") == 1, (
            "exactly one layout option must be preselected (the Mobile patent suits default)"
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
    # AC14: read-only - non-read methods rejected
    # ====================================================================

    def test_write_methods_return_405(self):
        proc, port = self._start(["--port", "0"])
        routes = [
            "/",
            f"/roadmaps/{ROADMAP}",
            f"/roadmaps/{ROADMAP}/tasks",
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
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/graph"):
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
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/graph"):
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
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/graph"):
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
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/graph"):
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
            f"/roadmaps/{ROADMAP}/graph",
        ):
            _, _, body = self._req(port, path)
            assert f'href="/roadmaps/{ROADMAP}"' in body, "sidebar must link the roadmap's Sprints (landing)"
            assert f'href="/roadmaps/{ROADMAP}/tasks"' in body, "sidebar must link the roadmap's Tasks"
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
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/graph"):
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
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/graph"):
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
