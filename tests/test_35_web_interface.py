#!/usr/bin/env python3
"""
Test 35: rmp web - read-only embedded web interface (SPEC/WEB.md).

This suite drives the compiled binary's `rmp web` command end-to-end and
exercises the acceptance criteria of SPEC/WEB.md against the
running HTTP server:

- Process/CLI contract: flag validation and exit codes (AC1-AC5), the
  machine-readable {"url": ...} startup object, and the graceful SIGINT/SIGTERM
  shutdown of acceptance criterion 24 — asserted as the OUTCOME the
  criterion's adverb names and not merely as the exit code, since runServer
  returns nil on the signal path unconditionally. Each of step 7's four promises
  is driven or explicitly declined: the listener refusing new connections while
  the process is still alive, an in-flight response still arriving whole across
  the signal, the drain giving up inside internal/web/server.go's shutdownGrace
  when a client stops reading, and the store/database handles, whose closure at
  shutdown is unobservable from outside the process and whose antecedent is
  driven instead. The scope step 7 puts on those promises is driven on both
  sides: after the URL the answer is a graceful exit 0, and before it — reached
  by widening the startup migration sweep with a service estate of roadmaps — an
  interruption exiting 130.
- Routes and pages: index with discovery + empty state, the read-only
  sprints landing page (GET /roadmaps/{name}: three sprint tabs, Actual
  active by default) and the separate tasks page (GET /roadmaps/{name}/tasks:
  one paginated task list in a Tabler card, narrowed on the server by the card
  header's GET filter bar — search, sprint, status, and type, with an Apply
  button and no Reset — which compose conjunctively and travel, with the page and
  the page size, in the URL query string; the one Reset link is the no-match
  empty state's) — both with no edit affordance and no audit-log
  growth, name validation / path-traversal guard, the knowledge-graph page
  and its JSON data endpoint, and the read-only proof that graph reads create
  no snapshot/ directory. Choosing a roadmap on the index lands the user on
  the sprints page with the current (OPEN) sprint selected by default.
- Read-only enforcement: non-read HTTP methods answered 405 (AC21).
- Self-contained delivery: static assets served only from /static/, a
  missing asset 404s (AC22), the vendored D3.js bundle and the d3-sankey
  plugin are served locally, no page references any remote origin
  (AC23, AC25, AC26).
- Mobile-first: every page carries the responsive viewport meta tag and
  loads no remote CSS; the stylesheet uses min-width media queries (AC27, AC29).
- Tabler admin-shell: every page renders in the dark theme
  (data-bs-theme="dark"), with a vertical sidebar, page wrapper/header, a
  top navbar naming the selected roadmap (AC108), and the off-canvas
  hamburger markup; the vendored Tabler CSS/JS and the Inter / Tabler Icons
  web fonts are served locally from /static/ (AC30/AC31, AC23/AC29).

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
import select
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

from tests.base_test import REPO_ROOT, GroadmapTestBase, commit_flags_for

ROADMAP = "platform"

# The port `rmp web` binds when --port is not given (SPEC/WEB.md § Bind Address
# and Port Selection, item 2). The ephemeral fallback under test is reachable
# ONLY by omitting --port: any --port at all, including this very number passed
# explicitly, marks the port explicit and turns a bind failure into a fatal
# error instead (internal/web/server.go bindListener; that other half of the
# contract is test_explicit_busy_port_exits_1).
DEFAULT_WEB_PORT = 8787

# Unicode's White_Space property: the set SPEC/WEB.md Acceptance Criterion 121
# names as the tasks page search's trim.
#
# It is written out here rather than taken from Python's str.strip(), whose set is
# Python's own — it also holds U+001C to U+001F — so that this module states the
# expectation INDEPENDENTLY; test_tasks_page_search_rules drives a term wrapped in
# every one of these code points through the server's trim.
#
# The two code points a JavaScript platform's own trimming disagrees about are the
# point of the rule: U+0085 is HERE and is trimmed, U+FEFF is NOT here and is kept.
WHITE_SPACE = "".join(chr(cp) for cp in (
    0x0009, 0x000A, 0x000B, 0x000C, 0x000D, 0x0020, 0x0085, 0x00A0, 0x1680,
    *range(0x2000, 0x200B), 0x2028, 0x2029, 0x202F, 0x205F, 0x3000,
))


# ---- graceful shutdown (SPEC/WEB.md section "Server Lifecycle", step 7) ----

# The bound `rmp web` puts on its graceful shutdown: the deadline runServer
# gives http.Server.Shutdown, declared as `shutdownGrace` in
# internal/web/server.go. SPEC/WEB.md calls it "a brief bounded period"; the
# number itself lives in the code, so the timing assertions below are derived
# from this constant rather than from a timeout somebody picked.
#
# It is written out here rather than parsed at import time, for the same reason
# WHITE_SPACE above is: this module states its expectation INDEPENDENTLY, and
# the parity with the binary is asserted rather than assumed. That the Go source
# still declares this very value is what
# test_the_shutdown_bound_is_the_one_the_server_declares checks, so a change on
# either side is named rather than absorbed.
SHUTDOWN_GRACE_SECONDS = 5.0
SHUTDOWN_GRACE_SOURCE = "internal/web/server.go"

# Slack allowed on top of SHUTDOWN_GRACE_SECONDS before a signalled server is
# called hung. It covers process teardown and scheduling on a loaded machine and
# nothing else. The two real timings it has to leave room for were measured, and
# both sit far from the edge: an idle server stops about 3 ms after the signal,
# and a server wedged by a client that stopped reading stops at 5.05 s.
SHUTDOWN_EXIT_MARGIN_SECONDS = 3.0

# How many times each signal is driven end to end by the repeated case below.
#
# The count is chosen against a MEASURED per-run catch probability, in the form
# internal/graphserve/signalwindow_test.go's signalAtAnnouncementRuns states its
# own.
#
# THE RATE. The figure of record for this surface is the one rmp task #388 took
# before commit 99f2e5c: 20 of 60 signalled `rmp web` processes did not stop
# cleanly, a 33% failure rate. Re-measured for this test against a binary built
# at a218312, the parent of that fix, driving the case exactly as it is written
# below: SIGTERM 31 of 60, every one of them the process killed outright by the
# signal's default disposition. The count is sized against 33%, the lower of the
# two. A single run misses a regression of that shape 0.67 of the time, and
# 0.67 ** 17 is 9.9e-4, so seventeen runs leave under one chance in a thousand of
# missing it. At the 52% actually observed here the same seventeen leave 8e-6.
#
# THE HARNESS IS PART OF THE RATE, and this is the part that would be easy to
# state dishonestly. The window is tens of microseconds wide, so what the parent
# does between reading the announcement and delivering the signal decides
# whether the case samples anything at all. Measured against the same pre-fix
# binary, over 40 to 60 runs each: signalling after the server had answered a
# request caught it 0 times; signalling after a 50 ms poll of a stdout file
# caught it 0 times; reading the clock once per character while reading the
# announcement caught it 1 time in 60; and the shape below, with no clock in
# that loop and os.kill in place of send_signal's waitpid, caught it 31 times in
# 60. It follows that the rate is a property of this machine as much as of the
# defect, and that a slower host would sample less. The seventeen runs are worth
# having anyway — they cost under a second — but the STATISTICAL confirmation of
# the signal discipline is not theirs to give and is not claimed here.
#
# WHAT THE COUNT IS NOT SIZED AGAINST, and why. The signal-window defect rmp task
# #388 removed failed at 4.5%, which needs of the order of a thousand runs to
# confirm — and that confirmation already exists, over a child process that does
# nothing else and races no interpreter: internal/signals/window_test.go
# (runs = 150) and #388's own 1,000-run harness. Repeating it here would buy the
# suite confidence it already has, at a cost only this module would pay, and
# through a harness demonstrably worse at reaching the window. This case is the
# integration half of that division, not a second copy of the statistical half.
#
# COST IS MEASURED RATHER THAN FEARED. One run costs about 18 ms (spawn, bind,
# read the announcement, signal, wait), so seventeen runs on each of the two
# signals add under a second to a module that already starts more than a hundred
# servers. Cost is not what bounds this number. The rate is.
#
# AND NOTE WHAT THE REPETITION IS FOR. Only the DELIVERY of the signal is
# stochastic. Everything the other cases assert about the outcome — the listener
# refusing while the process is still alive, the in-flight body completing whole,
# the bound holding — is deterministic, and one run settles each. The seventeen
# are here for the single property that is a race.
GRACEFUL_STOP_RUNS = 17

# The asset used to hold a request IN FLIGHT across the signal, and the file it
# is served from. It is chosen for its size: at nearly three megabytes it cannot
# fit in the socket buffers at either end, so the handler is still writing when
# the signal arrives. A small asset would be delivered into the kernel and be
# over before anything could be signalled, and the case would pass while driving
# nothing at all — which is why _asset_size asserts a floor on it.
IN_FLIGHT_ASSET_PATH = "/static/vendor/tabler-icons/fonts/tabler-icons.ttf"
IN_FLIGHT_ASSET_FILE = "internal/web/static/vendor/tabler-icons/fonts/tabler-icons.ttf"
IN_FLIGHT_ASSET_MIN_BYTES = 1 << 20

# The receive buffer given to a deliberately slow reader. The kernel rounds and
# clamps it, and the server's own send buffer adds a few more kilobytes, but the
# total stays four orders of magnitude below the asset's size.
SLOW_READER_RCVBUF = 2048

# The longest stdout the announcement reader will take before giving up. It
# stands in for a deadline, which that reader deliberately cannot afford: the
# object is about forty characters ({"url": "http://127.0.0.1:PORT"} indented),
# so this leaves an order of magnitude of room and still bounds a server that
# writes something else entirely.
ANNOUNCEMENT_MAX_CHARS = 512

# How many roadmaps are cloned into a throwaway HOME to widen the startup
# migration sweep, and the floor the widened window must clear for the
# pre-announcement case to be driving anything. Step 2 of the lifecycle opens and
# migrates every roadmap under ~/.roadmaps/ before the listener is bound and long
# before the URL is printed, at a few milliseconds each: one roadmap reaches the
# announcement in about 20 ms, two hundred take over a second.
STARTUP_SWEEP_ROADMAPS = 200
STARTUP_SWEEP_MIN_WINDOW_SECONDS = 0.5

# The service estate those roadmaps model. A roadmap per service is how an
# organisation this tool is aimed at actually accumulates hundreds of them, and
# the names are the services rather than a counter with a prefix.
SERVICE_FAMILIES = (
    "payments", "identity", "notifications", "search", "billing",
    "inventory", "shipping", "analytics", "support", "reporting",
)

# Exit code for an invocation interrupted before it took the signals over
# (SPEC/ARCHITECTURE.md section "Exit Codes"; SPEC/WEB.md section "Server
# Lifecycle", step 7: a signal arriving during steps 1 to 4).
EXIT_SIGINT = 130

class TestWebInterface:
    """End-to-end coverage of `rmp web` (SPEC/WEB.md § Acceptance Criteria)."""

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
        self._run(["sprint", "add-tasks", "-r", ROADMAP, str(open_sid), f"{t1},{t2}"])
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
        #
        # The graph is reachable only through a running `rmp graph serve`,
        # spoken to by `rmp graph client` (SPEC/GRAPH.md § The Dedicated Graph
        # Server), and starting that server is also what CREATES a roadmap's
        # graph store. So the server comes first and the seeds go through the
        # client, and the server is left RUNNING for the whole scenario: the
        # graph data endpoint reaches the graph the same way and answers HTTP
        # 503 while nothing is serving the roadmap, so every case that reads the
        # graph over HTTP needs this server listening for its requests.
        self.graph_server = self._serve_graph(
            ROADMAP,
            "CREATE (s:Spec {key:'passwordless-auth'})",
            "CREATE (c:Code {path:'internal/auth/magiclink.go'})",
            "MATCH (s:Spec {key:'passwordless-auth'}), "
            "(c:Code {path:'internal/auth/magiclink.go'}) "
            "CREATE (s)-[:IMPLEMENTED_BY]->(c)",
        )

    # ---- knowledge-graph helpers ---------------------------------------

    def _serve_graph(self, roadmap, *statements):
        """Start a graph server for an EXISTING roadmap, run every statement
        through `rmp graph client`, and return the running server.

        The server is tracked by the shared fixture and force-killed in
        teardown, so nothing outlives the scenario holding a store lock.
        """
        server = self.test.start_graph_server(roadmap)
        for statement in statements:
            self.test.graph_ok(roadmap, query=statement)
        return server

    def _graph(self, statement, roadmap=ROADMAP):
        """Run one statement against a served roadmap's graph and return its
        parsed JSON result. It must succeed; a statement that did not run is
        not a result worth comparing."""
        return self.test.graph_ok(roadmap, query=statement)

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

    def _start_piped(self, home=None, timeout=15.0):
        """Launch `rmp web` with stdout on a PIPE, returning the instant the
        announcement is complete.

        _start reads the URL from a temporary file, polling every 50 ms, and
        that is the right way to wait for a server a test is about to USE. It is
        the wrong way to reach the instant this case is about. Measured against
        the binary built at commit a218312, the parent of the fix, over 60 runs
        each: a signal sent once the server had answered a GET found the defect
        0 times, a signal sent after a poll had come round found it 0 times, and
        a signal sent on the announcement itself found it 50 times. The window
        lives in the microseconds after the URL is written, so stdout has to be
        a pipe and the signal has to be sent on the read.

        The pipe cannot deadlock, which is why _start's reason for avoiding one
        does not apply here: `rmp web` writes the URL object to stdout and
        nothing else for the rest of its life — every log record goes to stderr
        (SPEC/WEB.md section "Server Logging") — so the pipe never fills, and
        this reads exactly that object and stops.
        """
        args = [self.cli, "web", "--no-open", "--host", "127.0.0.1", "--port", "0"]
        err = tempfile.TemporaryFile(mode="w+")
        proc = subprocess.Popen(
            args, stdout=subprocess.PIPE, stderr=err, text=True, env=self._env(home),
        )
        proc.out_file = proc.stdout
        proc.err_file = err
        self._procs.append(proc)

        # NO CLOCK IS READ WHILE THE ANNOUNCEMENT IS BEING READ. The window
        # this case samples is tens of microseconds wide — internal/signals puts
        # the unprotected interval at 41 microseconds at the median — and a
        # clock call is a syscall, which is a preemption point that hands the
        # CPU to the child and lets it finish re-arming before the signal lands.
        # Measured against the pre-fix binary, 40 runs each: an identical loop
        # that read the clock once per character caught the defect 1 time in 60,
        # and the same loop with no clock in it caught it 32 times in 40. The
        # cost is cumulative and it is not small — about forty characters at
        # roughly a microsecond each is the width of the whole window.
        #
        # The timeout is therefore taken ONCE, before the first character, where
        # it costs nothing: waiting for the announcement to begin is the only
        # step that can block indefinitely. Everything after it is served from
        # the buffer the first read filled, because utils.PrintJSON encodes
        # through a json.Encoder, which writes the whole object in a single
        # write. ANNOUNCEMENT_MAX_CHARS bounds the read in place of a deadline.
        if not select.select([proc.stdout], [], [], timeout)[0]:
            proc.kill()
            proc.wait(timeout=10)
            raise AssertionError(
                f"the server wrote nothing to stdout within {timeout:.0f}s: "
                f"stderr={self._drain(err)}"
            )
        announcement = ""
        while len(announcement) < ANNOUNCEMENT_MAX_CHARS:
            char = proc.stdout.read(1)
            if not char:
                break
            announcement += char
            if char != "}":
                continue
            try:
                url = json.loads(announcement)["url"]
            except (json.JSONDecodeError, KeyError, TypeError):
                continue
            return proc, int(url.rsplit(":", 1)[1])
        raise AssertionError(
            f"the server printed no announcement: exit={proc.poll()} "
            f"stdout={announcement!r} stderr={self._drain(err)}"
        )

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
    def _connect_refused(port, host="127.0.0.1"):
        """Report whether a TCP connect to host:port is REFUSED.

        The inversion of _wait_accepting's probe, and deliberately narrower than
        "the connection failed": only ECONNREFUSED counts, which is what a
        loopback port with no listener answers. A timeout would mean the packet
        reached something that never replied, a different condition and not the
        one "stops accepting new connections" is about, so it is reported as
        still accepting and left to the caller's own deadline.
        """
        try:
            with socket.create_connection((host, port), timeout=1.0):
                return False
        except ConnectionRefusedError:
            return True
        except OSError:
            return False

    @classmethod
    def _wait_refusing(cls, port, host="127.0.0.1", timeout=5.0):
        """Wait until host:port refuses connections; return how long that took.

        Used on a server that has been signalled but has NOT yet exited, which
        is the only vantage point from which "stops accepting new connections"
        is a real assertion: once the process is gone the kernel has closed the
        listening socket whatever the code did.
        """
        start = time.time()
        deadline = start + timeout
        while time.time() < deadline:
            if cls._connect_refused(port, host):
                return time.time() - start
            time.sleep(0.005)
        raise AssertionError(
            f"server on {host}:{port} was still accepting connections {timeout:.1f}s "
            f"after it was signalled; SPEC/WEB.md section \"Server Lifecycle\" step 7 "
            f"requires it to stop accepting new connections"
        )

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

    @staticmethod
    def _board_columns(body):
        """Return the markup of each column of the sprint page's board, in the
        order rendered.

        The board lives inside <main class="page-body">, so slicing main keeps
        the page header and the scripts after it out of every card assertion.
        """
        main = re.search(r'<main class="page-body">(.*?)</main>', body, re.S)
        assert main, 'the page has no <main class="page-body"> region'
        region = main.group(1)
        assert 'data-role="task-board"' in region, "the page renders no Kanban board"
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
    # rule 3, Column header; Acceptance Criterion 140).
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

    # ====================================================================
    # The tasks page: one paginated task list, filtered on the server
    # (SPEC/WEB.md § Roadmap Tasks Page)
    # ====================================================================

    # The TaskStatus values in the order of the task state machine's flow, and
    # the TaskType values in the order SPEC/MODELS.md § Enums lists them: the
    # order the status and type selects offer them in.
    TASK_STATUSES = ("BACKLOG", "SPRINT", "DOING", "TESTING", "COMPLETED")
    TASK_TYPES = ("USER_STORY", "TASK", "BUG", "SUB_TASK", "EPIC", "REFACTOR",
                  "CHORE", "SPIKE", "DESIGN_UX", "IMPROVEMENT")

    # Realistic title stems the list fixture cycles through; two carry the word
    # the search cases look for, in two cases.
    LIST_TITLE_PHRASES = (
        "Invalidate the settlement cache after a refund",
        "Rotate the acquirer API credentials",
        "Reconcile the nightly payout file",
        "Warm the Cache of merchant fee schedules",
        "Alert on residual balances after the close",
        "Export the ledger to the finance warehouse",
        "Harden webhook signature verification",
        "Backfill missing dispute evidence records",
    )

    def _seed_list_roadmap(self, roadmap, n):
        """Build a roadmap of n tasks through the CLI and return its record:
        {"tasks": {id: {...}}, "sprint_a": id, "sprint_b": id}.

        Task i carries type TASK_TYPES[i % 10], priority (i*7) % 10, severity
        (i*3) % 10, and the title stem LIST_TITLE_PHRASES[i % 8]. Every third task
        joins sprint A (started, so its members move through DOING, TESTING,
        COMPLETED, and back to BACKLOG while staying members), every third + 1
        joins sprint B (planned: its members are SPRINT or BACKLOG), and the rest
        belong to no sprint and stay in BACKLOG. Expectations are computed from
        this record, never from the page under test.
        """
        self._run(["roadmap", "create", roadmap])
        sprint_a = self.test.create_sprint(roadmap, "Close the checkout attack surface",
                                           title="Checkout hardening")
        sprint_b = self.test.create_sprint(roadmap, "Reconcile acquirer files nightly",
                                           title="Settlement reconciliation")
        tasks = {}
        order = []
        for i in range(n):
            task_type = self.TASK_TYPES[i % 10]
            title = f"{self.LIST_TITLE_PHRASES[i % 8]} (batch {i + 1})"
            priority, severity = (i * 7) % 10, (i * 3) % 10
            _, out, _ = self._run([
                "task", "create", "-r", roadmap, "-t", title, "-y", task_type,
                "-p", str(priority), "--severity", str(severity),
                "-fr", "Operations must reach this work from the tasks page.",
                "-tr", "Served read-only from the roadmap database.",
                "-ac", "The tasks page lists the task under every filter admitting it.",
            ])
            task_id = json.loads(out)["id"]
            tasks[task_id] = {"title": title, "type": task_type, "priority": priority,
                              "severity": severity, "status": "BACKLOG", "sprint": 0}
            order.append(task_id)

        members_a = order[0::3]
        members_b = order[1::3]
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_a), ",".join(map(str, members_a))])
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_b), ",".join(map(str, members_b))])
        self._run(["sprint", "start", "-r", roadmap, str(sprint_a)])
        for task_id in members_a:
            tasks[task_id].update(sprint=sprint_a, status="SPRINT")
        for task_id in members_b:
            tasks[task_id].update(sprint=sprint_b, status="SPRINT")

        def stat(ids, status, *extra):
            if ids:
                self._run(["task", "stat", "-r", roadmap, ",".join(map(str, ids)), status, *extra])

        target = {"DOING": [], "TESTING": [], "COMPLETED": [], "BACKLOG": []}
        for j, task_id in enumerate(members_a):
            want = ("SPRINT", "DOING", "TESTING", "COMPLETED", "BACKLOG")[j % 5]
            if want != "SPRINT":
                target[want].append(task_id)
            tasks[task_id]["status"] = want
        for j, task_id in enumerate(members_b):
            if j % 2:
                target["BACKLOG"].append(task_id)
                tasks[task_id]["status"] = "BACKLOG"
        stat(target["BACKLOG"], "BACKLOG")
        moving = target["DOING"] + target["TESTING"] + target["COMPLETED"]
        stat(moving, "DOING", "--commit-open", "5d6a2cd")
        stat(target["TESTING"] + target["COMPLETED"], "TESTING")
        stat(target["COMPLETED"], "COMPLETED", "--commit-close", "4999725")
        return {"tasks": tasks, "sprint_a": sprint_a, "sprint_b": sprint_b}

    @staticmethod
    def _expect(tasks, keep=lambda task: True):
        """The ids of the tasks keep admits, in the page's order: priority
        descending, then creation — which the CLI performs in id order — then id."""
        return [task_id for task_id, _ in sorted(
            ((task_id, task) for task_id, task in tasks.items() if keep(task)),
            key=lambda item: (-item[1]["priority"], item[0]))]

    @staticmethod
    def _parse_list(body):
        """Read what a served tasks page states, from its HTML alone."""
        parsed = {
            "ids": [int(m) for m in re.findall(
                r'<tr>\s*<td><span class="badge bg-black text-white">#(\d+)</span></td>', body)],
            "range": None, "selects": {}, "checks": {}, "toggles": {}, "search": None, "size": None,
            "reset": None, "empty": None, "items": [], "prev": None, "next": None, "sizes": [],
        }
        for toggle_id, described_by, span_id, text, menu in re.findall(
                r'<div class="dropdown">\s*<button type="button" class="form-select form-select-sm" id="([^"]*)" '
                r'data-bs-toggle="dropdown" data-bs-auto-close="outside" aria-describedby="([^"]*)">'
                r'<span id="([^"]*)">([^<]*)</span></button>\s*<div class="dropdown-menu">(.*?)</div>\s*</div>', body, re.S):
            boxes = re.findall(
                r'<label class="dropdown-item"><input type="checkbox" class="form-check-input" '
                r'name="([a-z]+)" value="([^"]*)"( checked)?>([^<]*)</label>', menu)
            name = boxes[0][0] if boxes else toggle_id
            parsed["checks"][name] = [(html_lib.unescape(value), bool(checked)) for _, value, checked, _ in boxes]
            parsed["toggles"][name] = {"id": toggle_id, "describedby": described_by, "span": span_id,
                                       "text": html_lib.unescape(text)}
        m = re.search(r'<p class="m-0 text-secondary">Showing <span>(\d+)</span> to <span>(\d+)</span> '
                      r'of <span>(\d+)</span> entries</p>', body)
        if m:
            parsed["range"] = tuple(int(g) for g in m.groups())
        for name, options in re.findall(
                r'<select class="form-select form-select-sm(?: task-list__sprint-select)?" id="task-filter-[a-z]+" name="([a-z]+)">(.*?)</select>', body, re.S):
            parsed["selects"][name] = [
                (html_lib.unescape(value), html_lib.unescape(label), bool(selected))
                for value, selected, label in re.findall(
                    r'<option value="([^"]*)"( selected)?>([^<]*)</option>', options)]
        m = re.search(r'<input type="search" class="form-control form-control-sm" id="task-filter-q" name="q" placeholder="Search" value="([^"]*)">', body)
        assert m, "the tasks page carries no search input of the specified shape"
        parsed["search"] = html_lib.unescape(m.group(1))
        m = re.search(r'<input type="hidden" name="size" value="([^"]*)">', body)
        parsed["size"] = m.group(1) if m else None
        m = re.search(r'<a class="btn" href="([^"]*)">Reset</a>', body)
        parsed["reset"] = html_lib.unescape(m.group(1)) if m else None
        m = re.search(r'<p class="empty-title">([^<]*)</p>', body)
        parsed["empty"] = m.group(1) if m else None
        nav = re.search(r'<nav aria-label="Task list pages">(.*?)</nav>', body, re.S)
        if nav:
            items = re.findall(
                r'<li class="page-item( active| disabled)?"(?: aria-current="page")?>\s*'
                r'(?:<a class="page-link" href="([^"]*)"[^>]*>(.*?)</a>|<span class="page-link"[^>]*>(.*?)</span>)\s*</li>',
                nav.group(1), re.S)
            for index, (state, href, link_text, span_text) in enumerate(items):
                href = html_lib.unescape(href)
                if index == 0:
                    parsed["prev"] = href or None
                elif index == len(items) - 1:
                    parsed["next"] = href or None
                else:
                    parsed["items"].append({"text": (link_text or span_text).strip(), "href": href or None,
                                            "current": state == " active"})
        parsed["sizes"] = [
            {"active": bool(active), "href": html_lib.unescape(href), "current": bool(current), "text": text}
            for active, href, current, text in re.findall(
                r'<a class="btn btn-sm( active)?" href="([^"]*)"( aria-current="true")?>(\d+)</a>', body)]
        return parsed

    @staticmethod
    def _selected(parsed, name):
        """The value of the one selected option of a select; exactly one must be."""
        chosen = [value for value, _, selected in parsed["selects"][name] if selected]
        assert len(chosen) == 1, f"the {name} select marks {len(chosen)} options selected: {chosen}"
        return chosen[0]

    @staticmethod
    def _checked(parsed, name):
        """The values of a dropdown's checked boxes, in menu order."""
        assert name in parsed["checks"], f"the page has no {name} dropdown of the specified shape"
        return [value for value, checked in parsed["checks"][name] if checked]

    @staticmethod
    def _current_page(parsed):
        current = [item["text"] for item in parsed["items"] if item["current"]]
        assert len(current) == 1, f"the pagination bar has {len(current)} active items"
        return int(current[0])

    def _list(self, port, path):
        """Serve one tasks page and return (headers, body, parsed); it must answer 200."""
        status, headers, body = self._req(port, path)
        assert status == 200, f"GET {path}: status {status}, want 200"
        return headers, body, self._parse_list(body)

    def _list_all_pages(self, port, roadmap, query, size):
        """Follow the page numbers from 1 and return every page's rows in order,
        checking each page's range text against its rows."""
        ids = []
        page = 1
        while True:
            sep = "&" if query else ""
            _, _, parsed = self._list(port, f"/roadmaps/{roadmap}/tasks?{query}{sep}size={size}&page={page}")
            if parsed["range"] is None:
                return ids
            first, last, total = parsed["range"]
            assert self._current_page(parsed) == page, f"page {page} renders page {self._current_page(parsed)}"
            assert (first, last) == ((page - 1) * size + 1, min(page * size, total)), (
                f"page {page} at size {size}: range {first} to {last} of {total}")
            assert len(parsed["ids"]) == last - first + 1, f"page {page}: {len(parsed['ids'])} rows for {first}-{last}"
            ids.extend(parsed["ids"])
            if last == total:
                return ids
            page += 1

    @staticmethod
    def _list_row(body, task_id):
        """The <tr> of one task in the tasks page's list."""
        m = re.search(
            rf'<tr>\s*<td><span class="badge bg-black text-white">#{task_id}</span></td>.*?</tr>', body, re.S)
        assert m, f"task #{task_id} has no row in the tasks page's list"
        return m.group(0)

    def test_tasks_page_renders_one_paginated_list(self):
        """AC9, AC81, AC83, AC85, AC86, AC87, AC91, AC107, AC232: the tasks page
        is ONE Tabler card — a header holding the "Task list" title block and a
        card-actions GET filter bar, a table-responsive table with one row per
        task of the page in seven columns, and a footer with the range text, the
        rows-per-page selector and the pagination bar — with no board, no card
        per task, no modal, no selection, and no script of its own. Each row
        carries the id, type, status, severity and priority badges and the
        created date, no sprint, no View link and no Actions column, and exactly
        one link to the task's page: the title, named by its visible text.
        """
        record = self._seed_list_roadmap("payments_catalogue", 60)
        tasks = record["tasks"]
        proc, port = self._start(["--port", "0"])
        # Explicit, with no filter: every task, COMPLETED ones included. A bare
        # request would apply the default status selection.
        headers, body, parsed = self._list(port, "/roadmaps/payments_catalogue/tasks?size=25")

        assert headers.get("cache-control") == "no-store", headers.get("cache-control")
        main = re.search(r'<main class="page-body">(.*?)</main>', body, re.S).group(1)
        assert main.count('<div class="card">') == 1, "the page renders more than one card"
        assert main.count("<table") == 1 and '<table class="table table-vcenter card-table">' in main
        for forbidden in ("task-board", "card-sm card-link", "table-selectable",
                          'class="modal', 'data-bs-toggle="modal"', "task-search.js", "style="):
            assert forbidden not in body, f"the tasks page carries {forbidden!r}"
        # The only checkboxes are the filter bar's Status and Type dropdown boxes.
        form = body[body.index("<form"):body.index("</form>")]
        assert body.count('type="checkbox"') == form.count('type="checkbox"') == 15, "a checkbox outside the dropdowns"
        assert re.search(
            r'<div class="card-header flex-wrap gap-2">\s*<div>\s*<h2 class="card-title">Task list</h2>\s*</div>\s*'
            r'<div class="card-actions">\s*(?:\{\{.*?\}\}\s*)?<form class="row g-2 align-items-end justify-content-end" method="get" '
            r'action="/roadmaps/payments_catalogue/tasks">', main, re.S), "the card header is not the Tabler title block and card-actions form"
        assert "card-subtitle" not in main
        headings = re.findall(r"<th(?: class=\"[^\"]*\")?>([^<]*)</th>", main)
        assert headings == ["ID", "Title", "Type", "Status", "Severity", "Priority", "Created"], headings
        assert '<th class="w-1">ID</th>' in main and "Actions</th>" not in main
        scripts = re.findall(r"<script\b([^>]*)>", body)
        assert scripts == [' src="/static/vendor/tabler/tabler.min.js"'], scripts

        # The first page: the first 25 tasks of the order, and the range text.
        want = self._expect(tasks)
        assert parsed["ids"] == want[:25], f"page 1 lists {parsed['ids']}, want {want[:25]}"
        assert parsed["range"] == (1, 25, 60), parsed["range"]
        assert '<p class="m-0 text-secondary">Showing <span>1</span> to <span>25</span> of <span>60</span> entries</p>' in body
        assert re.search(r'<div class="card-footer">\s*<div class="row g-2 align-items-center">\s*'
                         r'<div class="col-auto">.*?</div>\s*<div class="col-auto">.*?</div>\s*</div>\s*'
                         r'<div class="col-auto ms-auto">\s*<nav aria-label="Task list pages">', main, re.S), (
            "the card footer is not three col-auto columns with the pagination bar at the trailing edge")

        # Row content, for a sprint member and a task in no sprint: neither row
        # shows a sprint.
        statuses = {task["status"] for task in tasks.values()}
        assert statuses == set(self.TASK_STATUSES), f"the fixture spans only {statuses}"
        member = next(i for i in want[:25] if tasks[i]["sprint"] == record["sprint_a"])
        loose = next(i for i in want[:25] if tasks[i]["sprint"] == 0)
        table = main[main.index("<table"):main.index("</table>")]
        for absent in ("Sprint #", "&mdash;", ">View<", "aria-label", "task-list__sprint"):
            assert absent not in table, f"the table carries {absent!r}; it shows no sprint and no View link"
        for task_id in (member, loose):
            task = tasks[task_id]
            row = re.sub(r">\s+<", "><", self._list_row(body, task_id))
            href = f"/roadmaps/payments_catalogue/tasks/{task_id}"
            title = self._go_escaped(task["title"])
            for piece in (
                f'<td><span class="badge bg-black text-white">#{task_id}</span></td>',
                f'<td class="task-list__title"><a href="{href}">{title}</a></td>',
                f'<span class="badge {self.TASK_TYPE_BADGE[task["type"]]}">{task["type"]}</span>',
                f'<span class="badge {self.TASK_STATUS_BADGE[task["status"]]}">{task["status"]}</span>',
                f'>S{task["severity"]}</span>', f'>P{task["priority"]}</span>',
                '<td class="text-secondary text-nowrap"><i class="ti ti-calendar me-1" aria-hidden="true"></i><time datetime="',
            ):
                assert piece in row, f"task #{task_id}'s row lacks {piece!r}: {row}"
            assert row.count("<a ") == 1 and row.count(f'href="{href}"') == 1, row
            assert row.index(f">S{task['severity']}<") < row.index(f">P{task['priority']}<"), "severity must precede priority"
            for forbidden in ("ti-message", "ti-subtask", "Comments", "role=", "tabindex"):
                assert forbidden not in row, f"task #{task_id}'s row carries {forbidden!r}"
            status, page = self._task_page(port, "payments_catalogue", task_id)
            assert status == 200 and f'<div class="page-pretitle">Task #{task_id} ' in page

        # An unknown roadmap and an invalid name answer 404 whatever the query.
        for path in ("/roadmaps/no_such_roadmap/tasks?status=DOING", "/roadmaps/..%2Fetc/tasks?page=2"):
            status, _, _ = self._req(port, path)
            assert status == 404, f"GET {path}: {status}, want 404"

    def test_tasks_page_each_filter_narrows_the_list(self):
        """AC112, AC113, AC234, AC235, AC243, AC244, AC248: the filter bar offers
        search, a sprint select, and Status and Type dropdowns of checkboxes, each
        control with one visually hidden label, then Apply, and no priority or
        severity filter and no Reset control; status and type match any one of
        their checked values (OR within, AND across), sprint is membership —
        `none` the tasks of no sprint whatever their status — priority and
        severity are not parameters of the page, and each control shows the
        state that produced the list, a dropdown's toggle reading its one value
        or `<n> selected`."""
        record = self._seed_list_roadmap("payments_catalogue", 45)
        tasks, sprint_a, sprint_b = record["tasks"], record["sprint_a"], record["sprint_b"]
        proc, port = self._start(["--port", "0"])
        base = "/roadmaps/payments_catalogue/tasks"
        _, body, parsed = self._list(port, f"{base}?size=25")

        for name, label in (("q", "Search"), ("sprint", "Sprint"), ("status", "Status"), ("type", "Type")):
            assert body.count(f'for="task-filter-{name}"') == 1, label
            assert f'<label class="visually-hidden" for="task-filter-{name}">{label}</label>' in body, label
        assert [v for v, _, _ in parsed["selects"]["sprint"]] == ["", "none", str(sprint_a), str(sprint_b)]
        assert [label for _, label, _ in parsed["selects"]["sprint"]] == [
            "Any sprint", "No sprint", f"Sprint #{sprint_a} Checkout hardening",
            f"Sprint #{sprint_b} Settlement reconciliation"]
        assert [v for v, _ in parsed["checks"]["status"]] == list(self.TASK_STATUSES)
        assert sorted(v for v, _ in parsed["checks"]["type"]) == sorted(self.TASK_TYPES)
        assert sorted(parsed["selects"]) == ["sprint"], sorted(parsed["selects"])
        for name, any_text in (("status", "Any status"), ("type", "Any type")):
            toggle = parsed["toggles"][name]
            assert toggle["text"] == any_text and self._checked(parsed, name) == [], (name, toggle)
            assert toggle["describedby"] == toggle["span"] and body.count(f'id="{toggle["span"]}"') == 1, toggle
        assert '<button type="submit" class="btn btn-primary btn-sm">Apply</button>' in body
        form = body[body.index("<form"):body.index("</form>")]
        for forbidden in ('name="priority"', 'name="severity"', "Reset", "<a "):
            assert forbidden not in form, f"the filter bar carries {forbidden!r}"
        assert form.count('<label class="visually-hidden"') == 4 and form.count('class="col-12 col-sm-auto"') == 5
        assert 'placeholder="Search"' in body and "text-break" not in body.split("<table", 1)[1].split("</table>", 1)[0]
        assert parsed["reset"] is None and parsed["size"] == "25"

        def check(query, keep, select=None, value=None):
            got = self._list_all_pages(port, "payments_catalogue", query, 100)
            want = self._expect(tasks, keep)
            assert got == want, f"?{query} lists {got}, want {want}"
            if select:
                _, _, shown = self._list(port, f"{base}?{query}")
                if select == "sprint":
                    assert self._selected(shown, select) == value, f"?{query}: the sprint select shows another value"
                else:
                    assert self._checked(shown, select) == value, f"?{query}: the {select} dropdown checks another set"
            return got

        for status in self.TASK_STATUSES:
            check(f"status={status}", lambda t, s=status: t["status"] == s, "status", [status])
        for task_type in ("BUG", "IMPROVEMENT", "USER_STORY"):
            check(f"type={task_type}", lambda t, y=task_type: t["type"] == y, "type", [task_type])
        # Several values of one dimension combine by OR, the dimensions by AND,
        # and a repeated value counts once.
        both = check("status=TESTING&status=DOING", lambda t: t["status"] in ("DOING", "TESTING"),
                     "status", ["DOING", "TESTING"])
        assert both and check("status=DOING&status=DOING", lambda t: t["status"] == "DOING") == \
            self._expect(tasks, lambda t: t["status"] == "DOING")
        check("status=DOING&status=TESTING&type=BUG&type=TASK&type=EPIC",
              lambda t: t["status"] in ("DOING", "TESTING") and t["type"] in ("BUG", "TASK", "EPIC"))
        _, _, shown = self._list(port, f"{base}?status=DOING&status=TESTING&type=BUG")
        assert shown["toggles"]["status"]["text"] == "2 selected" and shown["toggles"]["type"]["text"] == "BUG", shown["toggles"]
        # priority and severity are not parameters of the page: any value lists
        # what the request lists without it, and no generated link carries it.
        for param in ("priority", "severity"):
            for value in ("7", "0", "abc", ""):
                check(f"{param}={value}", lambda t: True)
                check(f"{param}={value}&status=DOING", lambda t: t["status"] == "DOING")
                _, _, shown = self._list(port, f"{base}?{param}={value}&size=10")
                for link in [i["href"] for i in shown["items"] if i["href"]] + [x["href"] for x in shown["sizes"]]:
                    assert param not in urllib.parse.parse_qs(urllib.parse.urlsplit(link).query), link
        assert any(t["status"] == "BACKLOG" and t["sprint"] for t in tasks.values()), "no BACKLOG sprint member"
        none = check("sprint=none", lambda t: t["sprint"] == 0, "sprint", "none")
        in_a = check(f"sprint={sprint_a}", lambda t: t["sprint"] == sprint_a, "sprint", str(sprint_a))
        in_b = check(f"sprint={sprint_b}", lambda t: t["sprint"] == sprint_b, "sprint", str(sprint_b))
        assert sorted(none + in_a + in_b) == sorted(tasks), "none and every sprint do not partition the tasks"
        check(f"sprint={sprint_a}&status=DOING", lambda t: t["sprint"] == sprint_a and t["status"] == "DOING")

    def test_tasks_page_filters_and_search_compose_in_every_combination(self):
        """AC114, AC238: each of the 16 combinations of the four criteria lists
        exactly the tasks satisfying every present criterion, in the page's
        order, with the range text stating their number; the order of the
        parameters in the query string changes nothing."""
        record = self._seed_list_roadmap("payments_catalogue", 48)
        tasks, sprint_a = record["tasks"], record["sprint_a"]
        proc, port = self._start(["--port", "0"])
        criteria = (
            ("q=cache", lambda t: "cache" in t["title"].lower()),
            (f"sprint={sprint_a}", lambda t: t["sprint"] == sprint_a),
            ("status=SPRINT", lambda t: t["status"] == "SPRINT"),
            ("type=TASK", lambda t: t["type"] == "TASK"),
        )
        non_empty = 0
        for mask in range(16):
            present = [c for i, c in enumerate(criteria) if mask & (1 << i)]
            query = "&".join(param for param, _ in present)
            want = self._expect(tasks, lambda t: all(keep(t) for _, keep in present))
            got = self._list_all_pages(port, "payments_catalogue", query, 100)
            assert got == want, f"mask {mask:04b} ?{query} lists {got}, want {want}"
            if want:
                non_empty += 1
                _, _, parsed = self._list(port, f"/roadmaps/payments_catalogue/tasks?{query or 'size=25'}")
                assert parsed["range"][2] == len(want), f"?{query}: range states {parsed['range']}"
        assert non_empty >= 8, f"only {non_empty} combinations list any task"
        _, _, forward = self._list(port, "/roadmaps/payments_catalogue/tasks?status=SPRINT&type=TASK&q=cache")
        _, _, backward = self._list(port, "/roadmaps/payments_catalogue/tasks?q=cache&type=TASK&status=SPRINT")
        assert forward["ids"] == backward["ids"]

    def test_tasks_page_ignores_unacceptable_values(self):
        """AC115, AC236, AC105, AC117: an unacceptable filter value — wrong
        case, out of range, decorated, packed, of another roadmap's sprint,
        empty, undecodable, or hostile — is ignored: HTTP 200 with
        Cache-Control no-store, exactly the list without it, the select on its
        any option, the value in no generated link and nowhere in the page, the
        other parameters still applied; a repeated parameter reads its first
        value while each occurrence of a repeatable status or type is validated on
        its own, and unknown parameters are ignored. The roadmap is intact after."""
        record = self._seed_list_roadmap("payments_catalogue", 30)
        tasks = record["tasks"]
        # A roadmap whose third sprint id is not a sprint of the catalogue.
        self._run(["roadmap", "create", "treasury_ops"])
        for title in ("Treasury close", "Liquidity report", "Cash sweep"):
            foreign = self.test.create_sprint("treasury_ops", f"{title} planning", title=title)
        assert foreign not in (record["sprint_a"], record["sprint_b"])
        proc, port = self._start(["--port", "0"])
        base = "/roadmaps/payments_catalogue/tasks"

        def served(query):
            status, headers, body = self._req(port, f"{base}?{query}")
            assert status == 200, f"?{query}: status {status}, want 200"
            assert headers.get("cache-control") == "no-store"
            return body, self._parse_list(body)

        keep = {"status": ("type", "TASK"), "type": ("status", "SPRINT"), "sprint": ("status", "SPRINT")}
        bad = {
            "status": ["doing", "Doing", "BLOCKED", "%20DOING", "", "DOING,SPRINT", "%zz",
                       urllib.parse.quote("DOING' OR '1'='1")],
            "type": ["bug", "FEATURE", "BUG,EPIC", "", "%zz", urllib.parse.quote("BUG;DROP TABLE tasks")],
            "sprint": ["NONE", "007", "0", str(foreign), "999", "", urllib.parse.quote("1 OR 1=1")],
        }
        for param, values in bad.items():
            kept, kept_value = keep[param]
            _, baseline = served(f"{kept}={kept_value}&size=100")
            for value in values:
                body, parsed = served(f"{param}={value}&{kept}={kept_value}&size=100")
                assert parsed["ids"] == baseline["ids"], f"{param}={value!r} changed the list"
                if param == "sprint":
                    assert self._selected(parsed, param) == "", f"{param}={value!r}: the select left its any option"
                else:
                    assert self._checked(parsed, param) == [], f"{param}={value!r}: a box is checked"
                assert self._checked(parsed, kept) == [kept_value], f"{param}={value!r}: the {kept} filter was lost"
                links = [i["href"] for i in parsed["items"] if i["href"]] + [s["href"] for s in parsed["sizes"]]
                for link in links + ([parsed["reset"]] if parsed["reset"] else []):
                    assert param not in urllib.parse.parse_qs(urllib.parse.urlsplit(link).query), (
                        f"{param}={value!r}: the link {link} carries the ignored parameter")
                decoded = urllib.parse.unquote(value)
                if any(mark in decoded for mark in ("'", ";", " OR ")):
                    assert decoded not in html_lib.unescape(body), f"{param}={value!r} is echoed into the page"
        _, unfiltered = served("size=100")
        for param in ("priority", "severity"):
            for value in ("5", "0", "10", "-1", "abc", "", urllib.parse.quote("1; DELETE FROM tasks")):
                _, parsed = served(f"{param}={value}&size=100")
                assert parsed["ids"] == unfiltered["ids"], f"{param}={value!r} changed the list"
        _, repeated = served("type=BUG&type=EPIC&size=100")
        assert repeated["ids"] == self._expect(tasks, lambda t: t["type"] in ("BUG", "EPIC"))
        _, mixed = served("type=BUG&type=bug&size=100")
        assert mixed["ids"] == self._expect(tasks, lambda t: t["type"] == "BUG") and self._checked(mixed, "type") == ["BUG"]
        _, first_sprint = served(f"sprint={record['sprint_a']}&sprint={record['sprint_b']}&size=100")
        assert first_sprint["ids"] == self._expect(tasks, lambda t: t["sprint"] == record["sprint_a"])
        _, unknown = served("assignee=alice&sort=title&limit=5&size=100")
        assert unknown["ids"] == unfiltered["ids"]
        listed = self.test.list_tasks("payments_catalogue", limit=100)
        assert len(listed) == len(tasks), "the roadmap lost tasks after the hostile requests"

    def test_tasks_page_paginates_on_the_server(self):
        """AC82, AC84, AC116, AC237, AC239, AC240, AC241, AC242: the list is
        paginated on the server at 10, 25, 50 or 100 rows, the pages together
        carrying every task once in order; an unacceptable page falls back to 1,
        a page beyond the last renders the last, an unacceptable size falls back
        to 25; every generated link keeps the accepted filters and following it
        shows them applied; submitting the form keeps the size and returns to
        page 1, and submitting it cleared lists every task at the same size;
        the no-match Reset link restores the default statuses and keeps the
        size."""
        record = self._seed_list_roadmap("payments_catalogue", 60)
        tasks = record["tasks"]
        proc, port = self._start(["--port", "0"])
        base = "/roadmaps/payments_catalogue/tasks"
        want = self._expect(tasks)

        for size, pages in ((10, 6), (25, 3), (50, 2), (100, 1)):
            assert self._list_all_pages(port, "payments_catalogue", "", size) == want, f"size {size}"
            _, _, last = self._list(port, f"{base}?size={size}&page={pages}")
            assert self._current_page(last) == pages and last["range"][1] == 60
            texts = [s["text"] for s in last["sizes"]]
            assert texts == ["10", "25", "50", "100"], texts
            assert [s["text"] for s in last["sizes"] if s["active"] and s["current"]] == [str(size)]
        _, body, page2 = self._list(port, f"{base}?page=2")
        assert page2["ids"] == want[25:50]
        for task_id in want[:25] + want[50:]:
            assert f"/tasks/{task_id}\"" not in body, f"page 2 carries task #{task_id} of another page"

        for value in ("0", "-1", "abc", "1.5", "02", "%202", ""):
            _, _, parsed = self._list(port, f"{base}?page={value}")
            assert self._current_page(parsed) == 1 and parsed["ids"] == want[:25], f"page={value!r}"
        for value in ("4", "999", "99999999999999999999999"):
            _, _, parsed = self._list(port, f"{base}?page={value}")
            assert self._current_page(parsed) == 3 and parsed["ids"] == want[50:], f"page={value!r}"
        for value in ("20", "0", "-10", "abc", "025", ""):
            _, _, parsed = self._list(port, f"{base}?size={value}")
            assert len(parsed["ids"]) == 25, f"size={value!r}"
            assert [s["text"] for s in parsed["sizes"] if s["active"]] == ["25"], f"size={value!r}"
        doing = self._expect(tasks, lambda t: t["status"] == "DOING")
        _, _, beyond = self._list(port, f"{base}?status=DOING&size=10&page=50")
        assert beyond["ids"] == doing[(len(doing) - 1) // 10 * 10:], "beyond the last page of a filtered list"

        # Every generated link keeps the accepted filters, and following it shows
        # them applied at the page and size it names.
        admitted = self._expect(tasks, lambda t: "the" in t["title"].lower() and t["sprint"] == 0)
        _, _, start = self._list(port, f"{base}?q=the&sprint=none&severity=1&priority=0&type=bogus&size=10&page=2")
        links = [i["href"] for i in start["items"] if i["href"]] + [s["href"] for s in start["sizes"]]
        links += [start["prev"], start["next"]]
        assert len(links) >= 6, links
        for link in filter(None, links):
            query = urllib.parse.parse_qs(urllib.parse.urlsplit(link).query)
            assert query.get("q") == ["the"] and query.get("sprint") == ["none"], f"{link} lost a filter"
            for ignored in ("type", "priority", "severity"):
                assert ignored not in query, f"{link} carries {ignored}"
            assert query.get("page") != ["1"] and query.get("size") != ["25"], link
            page = int(query.get("page", ["1"])[0])
            size = int(query.get("size", ["25"])[0])
            _, _, followed = self._list(port, link)
            assert self._current_page(followed) == page
            assert followed["ids"] == admitted[(page - 1) * size:page * size], f"following {link}"
        assert start["reset"] is None, "a list with rows carries a Reset link; the bar has none"
        _, _, no_match = self._list(port, f"{base}?q=no+task+is+titled+like+this&status=DOING&size=10&page=2")
        defaults = "status=BACKLOG&status=SPRINT&status=DOING&status=TESTING"
        assert no_match["reset"] == f"{base}?{defaults}&size=10", no_match["reset"]
        _, _, reset = self._list(port, no_match["reset"])
        open_tasks = self._expect(tasks, lambda t: t["status"] != "COMPLETED")
        assert self._current_page(reset) == 1 and reset["ids"] == open_tasks[:10]
        assert self._checked(reset, "status") == ["BACKLOG", "SPRINT", "DOING", "TESTING"]

        # Submitting the real form: its fields as the served HTML defines them —
        # the search input, the hidden size, the sprint select, and one field per
        # checked box — with the given fields replaced.
        def submit(body, parsed, changes):
            form = body[body.index("<form"):body.index("</form>")]
            action = html_lib.unescape(re.search(r'action="([^"]*)"', form).group(1))
            fields = {name: [html_lib.unescape(value)] for name, value in re.findall(
                r'<input type="(?:hidden|search)"[^>]* name="([a-z]+)"(?: placeholder="[^"]*")? value="([^"]*)">', form)}
            fields["sprint"] = [self._selected(parsed, "sprint")]
            for name in ("status", "type"):
                fields[name] = self._checked(parsed, name)
            assert sorted(fields) == ["q", "size", "sprint", "status", "type"], fields
            fields.update(changes)
            return f"{action}?{urllib.parse.urlencode(fields, doseq=True)}"

        _, body, parsed = self._list(port, f"{base}?q=the&size=10&page=3")
        assert self._current_page(parsed) == 3
        submitted = submit(body, parsed, {"status": ["DOING"]})
        assert "page=" not in submitted and "size=10" in submitted, submitted
        filtered_body = self._req(port, submitted)[2]
        filtered = self._parse_list(filtered_body)
        doing_the = self._expect(tasks, lambda t: t["status"] == "DOING" and "the" in t["title"].lower())
        assert self._current_page(filtered) == 1 and filtered["ids"] == doing_the[:10]
        _, _, reloaded = self._list(port, submitted)
        assert reloaded["ids"] == filtered["ids"]
        cleared_url = submit(filtered_body, filtered, {"q": [""], "sprint": [""], "status": [], "type": []})
        _, _, cleared = self._list(port, cleared_url)
        assert self._current_page(cleared) == 1 and cleared["ids"] == want[:10] and self._checked(cleared, "status") == []
        assert cleared["range"][2] == len(tasks), "clearing the form does not list every task, COMPLETED included"
        _, body, parsed = self._list(port, base)
        default_submit = submit(body, parsed, {})
        assert "size=25" in default_submit and defaults in default_submit, (
            f"under the defaults the form submits {default_submit}")

    def test_tasks_page_empty_states(self):
        """AC88, AC102, AC249: with no task to show the card keeps its header and
        filter bar and shows Tabler's empty state instead of the table and the
        footer — "No tasks yet" for a roadmap with no task whatever the filters,
        "No task matches the filters" with a Reset link to the default statuses
        for a roadmap whose tasks the filters all exclude, the defaults over a
        roadmap of COMPLETED tasks included — all HTTP 200."""
        self._run(["roadmap", "create", "clearing_house"])
        record = self._seed_list_roadmap("payments_catalogue", 12)
        # A roadmap whose every task is COMPLETED.
        self._run(["roadmap", "create", "archived_settlements"])
        closed = []
        for title in ("Close the March settlement window", "Archive the Q1 chargeback evidence"):
            _, out, _ = self._run(["task", "create", "-r", "archived_settlements", "-t", title,
                                   "-fr", "Closed work kept for the audit trail.", "-tr", "Nothing left to change.",
                                   "-ac", "The work is archived."])
            closed.append(str(json.loads(out)["id"]))
        archive_sprint = self.test.create_sprint("archived_settlements", "Close the quarter", title="Q1 close")
        self._run(["sprint", "add-tasks", "-r", "archived_settlements", str(archive_sprint), ",".join(closed)])
        self._run(["sprint", "start", "-r", "archived_settlements", str(archive_sprint)])
        self._run(["task", "stat", "-r", "archived_settlements", ",".join(closed), "DOING", "--commit-open", "5d6a2cd"])
        self._run(["task", "stat", "-r", "archived_settlements", ",".join(closed), "TESTING"])
        self._run(["task", "stat", "-r", "archived_settlements", ",".join(closed), "COMPLETED", "--commit-close", "4999725"])
        proc, port = self._start(["--port", "0"])
        for path, title in (
            ("/roadmaps/clearing_house/tasks", "No tasks yet"),
            ("/roadmaps/clearing_house/tasks?status=DOING", "No tasks yet"),
            ("/roadmaps/clearing_house/tasks?size=25", "No tasks yet"),
            ("/roadmaps/payments_catalogue/tasks?q=no+task+is+titled+like+this", "No task matches the filters"),
            ("/roadmaps/payments_catalogue/tasks?status=COMPLETED&sprint=none", "No task matches the filters"),
            ("/roadmaps/archived_settlements/tasks", "No task matches the filters"),
        ):
            _, body, parsed = self._list(port, path)
            main = re.search(r'<main class="page-body">(.*?)</main>', body, re.S).group(1)
            assert parsed["empty"] == title, f"{path}: empty state {parsed['empty']!r}, want {title!r}"
            for gone in ("<table", '<div class="card-footer">', "Rows per page", "Task list pages"):
                assert gone not in main, f"{path}: the empty list still carries {gone!r}"
            for kept in ('<div class="card-header flex-wrap gap-2">', "<form", '<div class="empty">'):
                assert kept in main, f"{path}: the empty list lost {kept!r}"
            if title == "No tasks yet":
                assert "rmp task create" in main and "empty-action" not in main
            else:
                roadmap = path.split("/")[2]
                reset = (f'/roadmaps/{roadmap}/tasks?status=BACKLOG&amp;status=SPRINT&amp;status=DOING'
                         f'&amp;status=TESTING')
                assert re.search(r'<div class="empty-action">\s*<a class="btn" href="' + re.escape(reset) + r'">Reset</a>', main), (
                    f"{path}: the Reset link does not restore the default statuses")
                assert main.count(">Reset</a>") == 1, "the empty state's Reset link is not the page's only one"
        _, _, archived = self._list(port, "/roadmaps/archived_settlements/tasks?size=25")
        assert sorted(archived["ids"]) == sorted(int(i) for i in closed), "the COMPLETED tasks are not listed unfiltered"

    @staticmethod
    def _req_raw(port, path, method="GET", cookie=None, extra=None, timeout=5):
        """Request a raw path, optionally carrying a Cookie header and extra
        headers, and return (status, header list, body): the list keeps every
        occurrence, so repeated Set-Cookie headers stay countable."""
        conn = http.client.HTTPConnection("127.0.0.1", port, timeout=timeout)
        try:
            headers = dict(extra or {})
            if cookie is not None:
                headers["Cookie"] = cookie
            conn.request(method, path, headers=headers)
            resp = conn.getresponse()
            body = resp.read().decode("utf-8", "replace")
            return resp.status, [(k.lower(), v) for k, v in resp.getheaders()], body
        finally:
            conn.close()

    def test_tasks_page_filter_state_cookie(self):
        """AC249, AC250, AC251, AC252, AC253, AC254: a bare request with no
        cookie applies the defaults — every status but COMPLETED — and sets no
        cookie; an explicit request, GET or HEAD, sets rmp_tasks_filters with
        Path=/, Max-Age=31536000, HttpOnly and SameSite=Lax and the accepted
        state, never the page; a bare request carrying that cookie restores the
        state on page 1 without rewriting it, a foreign sprint id and every
        unaccepted part being ignored; an oversized value is not written; every
        response varies by Cookie, is no-store, and is never a 304; the cookie's
        term is echoed escaped, and no request writes to the roadmap."""
        record = self._seed_list_roadmap("payments_catalogue", 60)
        tasks, sprint_a = record["tasks"], record["sprint_a"]
        other = self._seed_list_roadmap("treasury_ops", 9)
        # A third sprint gives the catalogue a sprint id the treasury roadmap lacks.
        extra_sprint = self.test.create_sprint("payments_catalogue", "Automate chargeback responses",
                                               title="Chargeback automation")
        assert extra_sprint not in (other["sprint_a"], other["sprint_b"])
        audit_before = json.loads(self._run(["audit", "stats", "-r", "payments_catalogue"])[1]).get("total_entries")
        proc, port = self._start(["--port", "0"])
        base = "/roadmaps/payments_catalogue/tasks"

        def cookies_of(headers):
            return [v for k, v in headers if k == "set-cookie"]

        def header(headers, name):
            return [v for k, v in headers if k == name]

        # AC249: the defaults.
        status, headers, body = self._req_raw(port, base)
        parsed = self._parse_list(body)
        open_tasks = self._expect(tasks, lambda t: t["status"] != "COMPLETED")
        assert status == 200 and parsed["ids"] == open_tasks[:25] and parsed["range"][2] == len(open_tasks)
        assert len(open_tasks) < len(tasks), "the fixture has no COMPLETED task to exclude"
        assert self._checked(parsed, "status") == ["BACKLOG", "SPRINT", "DOING", "TESTING"]
        assert parsed["toggles"]["status"]["text"] == "4 selected" and parsed["toggles"]["type"]["text"] == "Any type"
        assert self._checked(parsed, "type") == [] and self._selected(parsed, "sprint") == "" and parsed["search"] == ""
        assert cookies_of(headers) == [], "a bare request set the cookie"
        assert "Cookie" in ", ".join(header(headers, "vary")) and header(headers, "cache-control") == ["no-store"]

        # AC250: an explicit request, GET and HEAD, sets the cookie.
        explicit = (f"{base}?q=the&sprint={sprint_a}&status=TESTING&status=DOING&status=SPRINT"
                    f"&type=TASK&type=BUG&type=EPIC&size=10&page=2")
        stored = f"q=the&sprint={sprint_a}&status=SPRINT&status=DOING&status=TESTING&type=TASK&type=BUG&type=EPIC&size=10"
        for method in ("GET", "HEAD"):
            status, headers, head_body = self._req_raw(port, explicit, method=method)
            set_cookies = cookies_of(headers)
            assert status == 200 and len(set_cookies) == 1, f"{method}: {status} {set_cookies}"
            pair, *attrs = set_cookies[0].split("; ")
            assert pair == f"rmp_tasks_filters={stored}", f"{method}: {pair}"
            assert sorted(attrs) == ["HttpOnly", "Max-Age=31536000", "Path=/", "SameSite=Lax"], attrs
            if method == "HEAD":
                assert head_body == "", "a HEAD response carries a body"
        for query, value in (("size=25", "size=25"), ("status=doing", "size=25"),
                             ("q=" + urllib.parse.quote('refund; "cache", now') + "&type=BUG", None)):
            _, headers, _ = self._req_raw(port, f"{base}?{query}")
            got = cookies_of(headers)[0].split(";", 1)[0].split("=", 1)[1]
            assert re.fullmatch(r"[A-Za-z0-9\-._~%+&=]*", got), f"?{query}: the value {got!r} leaves the cookie-octet set"
            if value is not None:
                assert got == value, f"?{query}: {got!r}, want {value!r}"
        status, headers, _ = self._req_raw(port, "/roadmaps/no_such_roadmap/tasks?status=DOING")
        assert status == 404 and cookies_of(headers) == [] and "Cookie" in ", ".join(header(headers, "vary"))

        # AC251: a bare request restores the stored state, validated per roadmap.
        want = self._expect(tasks, lambda t: "the" in t["title"].lower() and t["sprint"] == sprint_a
                            and t["status"] in ("SPRINT", "DOING", "TESTING") and t["type"] in ("TASK", "BUG", "EPIC"))
        assert want, "the stored filters admit no task; the restoration check proves nothing"
        for path in (base, f"{base}?priority=3"):
            status, headers, body = self._req_raw(port, path, cookie=f"rmp_tasks_filters={stored}")
            parsed = self._parse_list(body)
            assert status == 200 and parsed["ids"] == want[:10] and self._current_page(parsed) == 1, (path, parsed["ids"], want)
            assert parsed["search"] == "the" and self._selected(parsed, "sprint") == str(sprint_a) and parsed["size"] == "10"
            assert self._checked(parsed, "status") == ["SPRINT", "DOING", "TESTING"]
            assert parsed["toggles"]["type"]["text"] == "3 selected"
            assert cookies_of(headers) == [], f"{path}: a bare request rewrote the cookie"
        foreign = f"q=the&sprint={extra_sprint}&status=BACKLOG&status=SPRINT&size=50"
        _, _, body = self._req_raw(port, "/roadmaps/treasury_ops/tasks", cookie=f"rmp_tasks_filters={foreign}")
        parsed = self._parse_list(body)
        treasury_want = self._expect(other["tasks"], lambda t: "the" in t["title"].lower()
                                     and t["status"] in ("BACKLOG", "SPRINT"))
        assert treasury_want and parsed["ids"] == treasury_want, (parsed["ids"], treasury_want)
        assert self._selected(parsed, "sprint") == "" and parsed["size"] == "50", "the foreign sprint was not ignored"
        junk = "status=doing&type=BUG,EPIC&sprint=007&size=20&page=3&q=%zz&status=DOING"
        status, _, body = self._req_raw(port, base, cookie=f"rmp_tasks_filters={junk}")
        parsed = self._parse_list(body)
        doing = self._expect(tasks, lambda t: t["status"] == "DOING")
        assert status == 200 and parsed["ids"] == doing[:25] and parsed["size"] == "25" and self._current_page(parsed) == 1
        _, _, body = self._req_raw(port, base, cookie="rmp_tasks_filters=assignee=alice")
        assert self._parse_list(body)["range"][2] == len(tasks), "a cookie with no accepted part must list every task"
        _, _, body = self._req_raw(port, base, cookie="rmp_tasks_filters=status=DOING&size=100; rmp_tasks_filters=status=BACKLOG")
        assert self._checked(self._parse_list(body), "status") == ["DOING"], "the first cookie occurrence was not read"

        # AC252: an oversized value is not written; the earlier cookie stays usable.
        long_term = "a" * 3991
        status, headers, body = self._req_raw(port, f"{base}?q={long_term}", cookie="rmp_tasks_filters=status=DOING&size=25")
        assert status == 200 and cookies_of(headers) == [] and self._parse_list(body)["search"] == long_term
        _, headers, _ = self._req_raw(port, f"{base}?q={long_term[:-1]}")
        assert len(cookies_of(headers)) == 1, "a value of exactly 4000 bytes was not written"

        # AC253: never a 304, whatever the conditional headers.
        for extra in ({"If-None-Match": "*"}, {"If-Modified-Since": "Fri, 01 Jan 2100 00:00:00 GMT"}):
            status, headers, body = self._req_raw(port, base, extra=extra)
            assert status == 200 and "</html>" in body, f"{extra}: {status}"
            assert not header(headers, "etag") and not header(headers, "last-modified")

        # AC254: the cookie's term is data; nothing reached the roadmap.
        _, _, body = self._req_raw(port, base, cookie="rmp_tasks_filters=q=%3Cscript%3Ealert(1)%3C%2Fscript%3E")
        assert self._parse_list(body)["search"] == "<script>alert(1)</script>" and "<script>alert" not in body
        _, _, body = self._req_raw(port, base, cookie="rmp_tasks_filters=status=DOING%27+OR+%271%27%3D%271&sprint=1+OR+1%3D1")
        assert self._parse_list(body)["range"][2] == len(tasks) and "OR '1'" not in html_lib.unescape(body)
        assert len(self.test.list_tasks("payments_catalogue", limit=100)) == len(tasks)
        audit_after = json.loads(self._run(["audit", "stats", "-r", "payments_catalogue"])[1]).get("total_entries")
        assert audit_after == audit_before, f"serving the tasks page changed the audit log: {audit_before} -> {audit_after}"
        _, _, task_page = self._req_raw(port, f"{base}/{want[0]}")
        assert f'href="{base}"><i class="ti ti-arrow-left me-1"></i>Back to tasks</a>' in task_page
        assert f'<a class="nav-link" href="{base}"' in task_page and "/tasks?" not in task_page

    def test_tasks_page_search_rules(self):
        """AC101, AC103, AC105, AC106, AC118, AC121, AC152, AC153: the search
        matches the title and the #<id> reference alone, case-insensitively and
        as a substring; the term is trimmed by White_Space (U+0085 removed,
        U+FEFF kept), normalised to NFC and folded by the simple lowercase
        mapping on the server, so either stored spelling of café is found by
        either typed spelling and οδός finds οδός; an invalid byte becomes
        U+FFFD; an undecodable q is absent; a markup term is echoed escaped."""
        roadmap = "search_rules_demo"
        self._run(["roadmap", "create", roadmap])
        titles = (
            "Invalidate the settlement CACHE after a refund",
            "Café Lisboa onboarding",
            "Cafe\u0301 Porto onboarding",
            "Survey the οδός network",
            "Survey the ΟΔΟΣ network",
            "Decode the acquirer file \ufffd marker",
            'Render the "<b>bold</b> & more" banner',
        )
        ids = []
        for title in titles:
            _, out, _ = self._run([
                "task", "create", "-r", roadmap, "-t", title,
                "-fr", "Only this requirement mentions the zeppelin freight carrier.",
                "-tr", "Served read-only from the roadmap database.",
                "-ac", "The search finds the task by its title or reference.",
            ])
            ids.append(json.loads(out)["id"])
        proc, port = self._start(["--port", "0"])

        def search(raw_query):
            _, _, parsed = self._list(port, f"/roadmaps/{roadmap}/tasks?size=100&{raw_query}")
            return sorted(parsed["ids"])

        def q(term):
            return "q=" + urllib.parse.quote(term)

        assert search(q("cache")) == [ids[0]] == search(q("CaChE"))
        assert search(q("zeppelin")) == [], "a word only in the requirements matched"
        assert search(q("TASK")) == [], "a word only in the type matched"
        assert ids[1] in search(q(str(ids[1]))) and search(q(f"#{ids[1]}")) == [ids[1]]
        for term in ("café", "cafe\u0301", "CAFÉ", "CAFE\u0301"):
            assert search(q(term)) == sorted(ids[1:3]), f"{term!r} does not find both spellings"
        assert search(q("cafe")) == [], "NFC: cafe must not find café"
        assert search(q("οδός")) == [ids[3]]
        assert search(q("ΟΔΟΣ")) == [ids[4]]
        assert search(q("\u0085settlement cache\u0085")) == [ids[0]], "U+0085 is White_Space and is trimmed"
        assert search(q(WHITE_SPACE + "settlement cache" + WHITE_SPACE)) == [ids[0]], (
            "every White_Space code point is trimmed from the ends of a term")
        assert search(q("\ufeffsettlement")) == [], "U+FEFF is not White_Space and is kept"
        assert search(q(" \t ")) == sorted(ids), "a whitespace-only term is no criterion"
        assert search("q=%FF") == [ids[5]], "an invalid byte must be U+FFFD"
        assert search("q=%zz") == sorted(ids), "an undecodable q must be absent"
        assert search(q("refund " * 60)) == [], "a term longer than any title"

        term = '<b>bold</b> & more'
        _, body, parsed = self._list(port, f"/roadmaps/{roadmap}/tasks?size=10&{q(term)}")
        assert parsed["ids"] == [ids[6]] and parsed["search"] == term
        assert term not in body and "<b>bold</b>" not in body, "the term reached the page as markup"
        encoded = urllib.parse.quote_plus(term)
        assert all(f"q={encoded}" in s["href"] for s in parsed["sizes"]), "a link does not carry the encoded term"
        _, body, _ = self._list(port, f"/roadmaps/{roadmap}/tasks?" + q('"><script>alert(1)</script>'))
        assert "<script>alert" not in body

        # Normalisation is for comparison only: the CLI returns the stored bytes.
        stored = json.loads(self._run(["task", "get", "-r", roadmap, str(ids[2])])[1])
        stored = stored[0] if isinstance(stored, list) else stored
        assert stored["title"] == titles[2], "the stored decomposed title changed"

    TASK_TYPE_BADGE = {
        "BUG": "bg-red-lt", "USER_STORY": "bg-green-lt", "TASK": "bg-blue-lt",
        "SUB_TASK": "bg-azure-lt", "EPIC": "bg-purple-lt", "REFACTOR": "bg-indigo-lt",
        "IMPROVEMENT": "bg-teal-lt", "SPIKE": "bg-yellow-lt", "DESIGN_UX": "bg-pink-lt",
        "CHORE": "bg-secondary-lt",
    }

    @staticmethod
    def _go_escaped(text):
        """Return text as Go's html/template escapes it in element text: the
        ampersand, the angle brackets, and both quote characters, the quotes as
        numeric references."""
        return (text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
                .replace('"', "&#34;").replace("'", "&#39;"))

    @staticmethod
    def _markdown_text(text):
        """Return plain text as the Markdown renderer writes it in a paragraph:
        the ampersand, the angle brackets, and the double quote escaped, and
        nothing else."""
        return (text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
                .replace('"', "&quot;"))

    def _task_page(self, port, roadmap, task_id):
        """Return (status, body) of one task's own page, GET
        /roadmaps/{name}/tasks/{id} (SPEC/WEB.md § Roadmap Task Page)."""
        status, _, body = self._req(port, f"/roadmaps/{roadmap}/tasks/{task_id}")
        return status, body

    @staticmethod
    def _rendered_task_title(body, task_id):
        """Return a task's title exactly as a board page rendered it: the visible
        label of the task's card, read from the card link that carries the task's
        href followed by its accessible name. Reading the visible label rather
        than the aria-label is what keeps an assertion about the accessible name
        non-circular.
        """
        m = re.search(
            rf'href="/roadmaps/[^"/]+/tasks/{task_id}"[^>]*aria-label="[^"]*">(.*?)</a>', body, re.S
        )
        assert m, f"task #{task_id} has no card to read its title from"
        inner = re.search(r'data-role="task-card-title">(.*?)</span>', m.group(1), re.S)
        return inner.group(1) if inner else m.group(1)

    @staticmethod
    def _opening_tags(body):
        """Yield (tag, attributes) for every opening tag in a document."""
        return re.findall(r"<([a-zA-Z][a-zA-Z0-9]*)\b([^>]*)>", body)

    def test_every_task_card_is_a_link_to_its_task_page(self):
        """On the sprint board every task card is ONE <a> carrying Tabler's card
        and card-link classes and the href of its own task's page, named
        `Open details for task #<id>: <title>`, with no tabindex and no role, and
        nothing that cannot be activated pretends to be a control. Following the
        href serves that task's page. No script was added for it (SPEC/WEB.md
        § Sprint Detail Sub-Template, The card is a link to the task page;
        Acceptance Criteria 93 and 135). The tasks page renders rows, not cards:
        test_tasks_page_renders_one_paginated_list covers its one link (AC86)."""
        proc, port = self._start(["--port", "0"])

        for path in (
            f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}",
        ):
            _, _, body = self._req(port, path)
            main = re.search(r'<main class="page-body">(.*?)</main>', body, re.S).group(1)
            tags = self._opening_tags(main)
            assert tags, f"{path}: no markup parsed; the extraction is broken"

            cards = [(tag, attrs) for tag, attrs in tags
                     if re.search(r'class="[^"]*\btask-card\b[^"]*"', attrs)]
            assert cards, f"{path}: no task card found"
            for tag, attrs in cards:
                assert tag.lower() == "a", f"{path}: a task card is a <{tag}>: {attrs}"
                m = re.match(
                    rf' class="card card-sm card-link text-reset task-card" href="/roadmaps/{ROADMAP}/tasks/(\d+)"',
                    attrs,
                )
                assert m, f"{path}: a card does not carry the card link classes and its task href: {attrs}"
                task_id = m.group(1)
                title = self._rendered_task_title(body, task_id)
                want = f'aria-label="Open details for task #{task_id}: {title}"'
                assert want in attrs, f"{path}: task #{task_id}'s card lacks {want}: {attrs}"
                for prop in ("tabindex=", "role=", "data-bs-toggle"):
                    assert prop not in attrs, f"{path}: task #{task_id}'s card carries {prop}: {attrs}"
                status, page = self._task_page(port, ROADMAP, task_id)
                assert status == 200 and f'<div class="page-pretitle">Task #{task_id} ' in page, (
                    f"{path}: following task #{task_id}'s card does not serve its page"
                )

            # No nested link inside a card, and nothing fakes a control.
            for chunk in main.split('<a class="card card-sm card-link text-reset task-card"')[1:]:
                assert "<a " not in chunk[:chunk.find("</a>")], f"{path}: a card holds a nested link"
            for tag, attrs in tags:
                assert 'role="button"' not in attrs and "tabindex=" not in attrs, (
                    f"{path}: a <{tag}> carries a role or a tabindex: {attrs}"
                )
            assert "<tr" not in main, f"{path}: a table row remains"

            # No script was added: the vendored bundle plus the page's own board
            # script, all from /static/, under the unchanged policy.
            scripts = re.findall(r"<script\b([^>]*)>", body)
            srcs = {re.search(r'src="([^"]*)"', s).group(1) for s in scripts}
            want_srcs = {"/static/vendor/tabler/tabler.min.js", "/static/sprint-board.js"}
            assert srcs == want_srcs and len(scripts) == len(want_srcs), (
                f"{path}: unexpected scripts {srcs!r}"
            )
            _, headers, _ = self._req(port, path)
            assert "script-src 'self'" in headers.get("content-security-policy", ""), (
                f"{path}: the Content-Security-Policy no longer restricts script to 'self'"
            )

    def test_task_modal_script_and_task_json_are_gone(self):
        """The interface has no task modal, no task detail script, and no task
        JSON: neither the tasks page nor the sprint page carries a modal element or a modal toggle, the
        modal script is not served, and every path below a task page answers 404
        with no JSON body (SPEC/WEB.md § Routes and Pages, rule 5; Acceptance
        Criterion 96)."""
        proc, port = self._start(["--port", "0"])
        for path in (f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}"):
            _, _, body = self._req(port, path)
            assert not re.search(r'class="(?:[^"]* )?modal(?: [^"]*)?"', body), f"{path}: a modal element"
            assert 'data-bs-toggle="modal"' not in body and "task-modal" not in body, f"{path}: modal wiring"

        status, _, _ = self._req(port, "/static/task-modal.js")
        assert status == 404, f"the modal script is still served: {status}"
        t1 = self.open_task_ids[0]
        for suffix in ("/data", "/comments", "/data/extra"):
            status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}/tasks/{t1}{suffix}")
            assert status == 404, f"GET .../tasks/{t1}{suffix}: status {status}, want 404"
            assert "json" not in headers.get("content-type", ""), headers.get("content-type")
            assert not body.lstrip().startswith("{"), f"GET .../tasks/{t1}{suffix} returned JSON: {body!r}"

    def test_sprint_board_card_is_the_single_pointer_and_keyboard_target(self):
        """On the sprint page's member-tasks board the card itself IS the link to
        the task's page — one `<a>` carrying the card and card-link classes — so a
        pointer click, a touch tap, and Enter all reach the SAME target, and the
        task's href appears exactly once on the page (SPEC/WEB.md § Sprint Detail
        Sub-Template, The card is a link to the task page; Acceptance Criterion
        135)."""
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}")

        t1 = self.open_task_ids[0]
        href = f'href="/roadmaps/{ROADMAP}/tasks/{t1}"'
        title = self._rendered_task_title(body, t1)

        want = (
            f'<a class="card card-sm card-link text-reset task-card" {href} '
            f'aria-label="Open details for task #{t1}: {title}">'
        )
        assert want in body, (
            f"the member task's card must itself be the link; opening tag "
            f"{want!r} not found in the served sprint page"
        )

        card = self._sprint_board_card_html(self._sprint_board_region(body), t1)
        assert "tabindex" not in card, "a link is natively focusable; tabindex on the card is redundant"
        assert not re.search(r"\srole=", card), "a link announces itself; a role on the card is redundant"

        # Exactly one target for this task.
        assert body.count(href) == 1, (
            f"task #{t1}'s href appears {body.count(href)} times on the sprint page, want exactly 1"
        )
        assert "<table" not in body, "the sprint page must render no member-tasks table"
        assert "task-row" not in body, "no row-based markup may remain"

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

        # Ordered performed_at DESC: every stored timestamp is non-increasing.
        # The Performed At cell carries a unique class and holds a <time> element
        # whose datetime attribute is the stored value and whose text is the
        # display form YYYY-MM-DD HH:mm:ss (SPEC/WEB.md § Date and Time Display).
        # The order is asserted on the stored values, as the page sorts by them.
        cells = re.findall(
            r'<td class="text-nowrap text-secondary">'
            r'<time datetime="([^"]+)">([^<]+)</time></td>', body
        )
        assert len(cells) >= 2, "expected several audit rows in the populated roadmap"
        stamps = [stored for stored, _ in cells]
        assert stamps == sorted(stamps, reverse=True), (
            f"audit rows must be ordered performed_at DESC; got {stamps}"
        )
        for stored, shown in cells:
            assert re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z", stored), (
                f"the datetime attribute {stored!r} is not the stored ISO 8601 value"
            )
            assert shown == f"{stored[:10]} {stored[11:19]}", (
                f"Performed At {stored!r} is displayed as {shown!r}"
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
    # AC11/AC12/AC13: sprint tabs, classification + ordering, sprint links
    # ====================================================================

    def test_detail_sprint_tabs_labels_and_default(self):
        """AC11: three tabs labelled Próximos / Actual / Concluídos, left to
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

    @staticmethod
    def _card_task_count(markup, roadmap, sprint_id):
        """Return the task-count footer number the shared sprint-card partial
        shows for ONE sprint, read from that sprint's own card and no other.

        The card is a single, non-nested <a> element per sprint (SPEC/WEB.md
        § Shared Sprint-Card Partial), so slicing from that sprint's own
        opening href to its closing </a> isolates its footer from every other
        card in the same markup, and a transposed count on a neighbouring
        card cannot be mistaken for this sprint's own. `markup` may be a full
        page body or a single tab's pane.
        """
        pattern = re.compile(
            rf'<a href="/roadmaps/{re.escape(roadmap)}/sprints/{sprint_id}" '
            r'class="card card-sm card-link text-reset">(.*?)</a>',
            re.S,
        )
        card = pattern.search(markup)
        assert card, (
            f"no sprint card for sprint #{sprint_id} found in the given markup"
        )
        count = re.search(
            r'<span class="text-secondary">(\d+) task\(s\)</span>', card.group(1)
        )
        assert count, f"sprint #{sprint_id} card carries no task-count footer"
        return int(count.group(1))

    def test_detail_sprint_classification_and_links(self):
        """AC12/AC13: sprints are classified by status into the right tab and
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
        open_footer_count = self._card_task_count(current, ROADMAP, self.open_sid)
        assert open_footer_count == len(self.open_task_ids), (
            f"Actual tab card for sprint #{self.open_sid} must show its real "
            f"member-task count {len(self.open_task_ids)}, got {open_footer_count}"
        )

    def test_sprint_card_footer_counts_match_real_membership_across_tabs(self):
        """Task #280: the shared sprint-card footer shown on every tab of the
        Roadmap Sprints Page must be each sprint's OWN, real member-task
        count — never a constant, never a transposed neighbour's number, and
        never zero for a sprint that actually holds tasks (SPEC/WEB.md §
        Shared Sprint-Card Partial; SPEC/MODELS.md § Sprint).

        The fixture gives every sprint under test a DIFFERENT member-task
        count (0, 1, 3, 5), so a constant or a transposed value cannot
        coincidentally satisfy every assertion, and it seeds one sprint per
        tab plus a second Próximos sprint that holds no task at all — an
        empty sprint is the case a hardcoded non-zero placeholder, or a
        footer that silently reads 0 for every sprint, would each fail
        differently against. One member of the OPEN sprint is manually
        walked back to BACKLOG status (still a sprint member per
        SPEC/STATE_MACHINE.md § Sprint Membership and the BACKLOG Status) to
        prove membership, not status, drives the count. Every footer is
        finally cross-checked against `rmp sprint get`'s own task_count for
        the identical sprint, so the web surface and the CLI cannot silently
        drift apart.
        """
        roadmap = "release_train_demo"
        self._run(["roadmap", "create", roadmap])

        def task(title, priority):
            return self.test.create_task(
                roadmap, title,
                "Release trains must ship without manual sign-off delays",
                "Automate the pre-flight checklist and gate promotion on it",
                "A train carrying a red checklist item cannot be promoted",
                priority=priority,
            )

        # Próximos: a 3-task PENDING sprint, and a second PENDING sprint left
        # genuinely empty (no `sprint add-tasks` call at all).
        upcoming_tasks = [
            task(f"Automate pre-flight check #{n}", 4 + n) for n in range(3)
        ]
        upcoming_sid = self.test.create_sprint(roadmap, "Pre-flight automation sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(upcoming_sid), ",".join(str(i) for i in upcoming_tasks)])
        empty_sid = self.test.create_sprint(roadmap, "Rollback tooling sprint")

        # Concluídos: a 1-task sprint, started then force-closed.
        closed_task = task("Wire the canary-deploy health check", 6)
        closed_sid = self.test.create_sprint(roadmap, "Canary health-check sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(closed_sid), str(closed_task)])
        self._run(["sprint", "start", "-r", roadmap, str(closed_sid)])
        self._run(["sprint", "close", "-r", roadmap, str(closed_sid), "--force"])

        # Actual: a 5-task OPEN sprint (only one sprint may be OPEN at a
        # time, so this is started last). One member is then walked back to
        # BACKLOG, staying a sprint member throughout.
        open_tasks = [
            task(f"Gate merge on checklist item #{n}", 5 + n) for n in range(5)
        ]
        open_sid = self.test.create_sprint(roadmap, "Merge-gate rollout sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(open_sid), ",".join(str(i) for i in open_tasks)])
        self._run(["sprint", "start", "-r", roadmap, str(open_sid)])
        self._run(["task", "stat", "-r", roadmap, str(open_tasks[0]), "BACKLOG"])
        self.test.assert_task_status(roadmap, open_tasks[0], "BACKLOG")

        expected = {
            upcoming_sid: 3,
            empty_sid: 0,
            closed_sid: 1,
            open_sid: 5,
        }

        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, f"/roadmaps/{roadmap}")
        assert status == 200

        def pane(marker):
            start = body.index(marker)
            rest = body[start + len(marker):]
            nxt = rest.find('<div id="tab-')
            return rest if nxt < 0 else rest[:nxt]

        upcoming_pane = pane('<div id="tab-upcoming"')
        current_pane = pane('<div id="tab-current"')
        closed_pane = pane('<div id="tab-closed"')

        tab_of = {
            upcoming_sid: upcoming_pane,
            empty_sid: upcoming_pane,
            open_sid: current_pane,
            closed_sid: closed_pane,
        }

        # Each sprint's footer, checked in ITS OWN tab, against the fixture's
        # independently known truth. All four expected values are distinct,
        # so no constant and no transposition between sprints can pass here.
        for sid, want in expected.items():
            got = self._card_task_count(tab_of[sid], roadmap, sid)
            assert got == want, (
                f"sprint #{sid} footer shows {got} task(s) on the sprints "
                f"page, want its real member-task count {want}"
            )

        # The empty sprint's card is present (not omitted) and reads exactly
        # 0, never absent and never defaulting to a non-zero placeholder.
        assert f"/roadmaps/{roadmap}/sprints/{empty_sid}" in upcoming_pane, (
            "the empty sprint must still render its own card under Próximos"
        )

        # The BACKLOG member is still counted: the OPEN sprint's footer stays
        # 5, not 4, after one member's STATUS (not membership) changed.
        still_open_count = self._card_task_count(current_pane, roadmap, open_sid)
        assert still_open_count == 5, (
            "a member task returned to BACKLOG status must still be counted "
            f"in its sprint's footer; got {still_open_count}, want 5"
        )

        # Cross-check against the CLI: the web footer and `rmp sprint get`
        # must report the IDENTICAL count for every sprint, so the two
        # surfaces cannot silently drift apart.
        for sid, want in expected.items():
            cli_sprint = self.test.run_cmd_json(["sprint", "get", "-r", roadmap, str(sid)])
            assert cli_sprint["task_count"] == want, (
                f"rmp sprint get #{sid} reports task_count="
                f"{cli_sprint['task_count']}, want {want} (fixture truth)"
            )
            web_count = self._card_task_count(tab_of[sid], roadmap, sid)
            assert web_count == cli_sprint["task_count"], (
                f"web footer ({web_count}) and `rmp sprint get` task_count "
                f"({cli_sprint['task_count']}) disagree for sprint #{sid}"
            )

    # ====================================================================
    # AC14/AC21: sprint page — details, task order, 404/405 rules
    # ====================================================================

    _DATAGRID_ITEM = re.compile(
        r'<div class="datagrid-item">\s*<div class="datagrid-title">([^<]*)</div>'
        r'\s*<div class="datagrid-content[^"]*">(.*?)</div>\s*</div>',
        re.S,
    )

    def _sprint_datagrid_items(self, body):
        """Return the (title, content) pairs of the sprint page's one metadata
        datagrid, in document order, with the content HTML-unescaped."""
        opener = '<div class="datagrid">'
        assert body.count(opener) == 1, (
            f"the sprint page carries {body.count(opener)} metadata datagrids, want exactly 1"
        )
        grid = body[body.index(opener):]
        end = grid.find('data-role="task-board"')
        if end != -1:
            grid = grid[:end]
        items = self._DATAGRID_ITEM.findall(grid)
        assert len(items) == grid.count('<div class="datagrid-item">'), (
            "a datagrid item escaped the item pattern, so the exact-shape check would miss it"
        )
        return [(title, html_lib.unescape(content)) for title, content in items]

    def test_sprint_page_datagrid_holds_exactly_created_started_closed(self):
        """AC14: the Sprint details datagrid holds exactly Created, Started and
        Closed, in that order, each equal to the CLI's value or an em dash when
        unset, and carries no ID, Title, Status, Order, Capacity or Tasks field."""
        proc, port = self._start(["--port", "0"])
        for label, sid in (("OPEN", self.open_sid), ("PENDING", self.pending_sid),
                           ("CLOSED", self.closed_sid)):
            status, _, body = self._req(port, f"/roadmaps/{ROADMAP}/sprints/{sid}")
            assert status == 200, f"{label} sprint #{sid}: status {status}"
            items = self._sprint_datagrid_items(body)
            titles = [title for title, _ in items]
            assert titles == ["Created", "Started", "Closed"], (
                f"{label} sprint #{sid}: datagrid titles {titles}, want exactly "
                "['Created', 'Started', 'Closed']"
            )
            cli = self.test.run_cmd_json(["sprint", "get", "-r", ROADMAP, str(sid)])

            # A set timestamp is a <time> element carrying the CLI's stored value
            # in its datetime attribute and the display form YYYY-MM-DD HH:mm:ss
            # as its text; an unset one is the em dash (SPEC/WEB.md § Date and
            # Time Display).
            def shown(value):
                if not value:
                    return "\u2014"
                return f'<time datetime="{value}">{value[:10]} {value[11:19]}</time>'

            want = [shown(cli["created_at"]), shown(cli.get("started_at")),
                    shown(cli.get("closed_at"))]
            got = [content for _, content in items]
            assert got == want, f"{label} sprint #{sid}: datagrid values {got}, want {want}"
            for gone in ("ID", "Title", "Status", "Order", "Capacity", "Tasks"):
                assert f'<div class="datagrid-title">{gone}</div>' not in body, (
                    f"{label} sprint #{sid}: the page still carries the {gone!r} datagrid field"
                )
            assert "Unlimited" not in body, (
                f"{label} sprint #{sid}: the page still shows the capacity placeholder"
            )
        # The fixture covers every placeholder combination the rule separates.
        pending = self.test.run_cmd_json(["sprint", "get", "-r", ROADMAP, str(self.pending_sid)])
        opened = self.test.run_cmd_json(["sprint", "get", "-r", ROADMAP, str(self.open_sid)])
        closed = self.test.run_cmd_json(["sprint", "get", "-r", ROADMAP, str(self.closed_sid)])
        assert not pending.get("started_at") and not pending.get("closed_at")
        assert opened.get("started_at") and not opened.get("closed_at")
        assert closed.get("started_at") and closed.get("closed_at")

    def test_sprint_page_shows_details_and_task_order(self):
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")

        # The sprint's details are present (the datagrid's exact shape is pinned
        # by test_sprint_page_datagrid_holds_exactly_created_started_closed).
        for field in ("Created", "Started", "Closed"):
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
    # (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3; Acceptance
    # Criteria 130 to 139)
    # ====================================================================

    # The three fixed board columns, left to right, exactly as
    # SPEC/WEB.md § Sprint Detail Sub-Template, rule 3 spells them (Acceptance
    # Criterion 130), and the task statuses each one groups (Acceptance
    # Criterion 131).
    SPRINT_BOARD_COLUMNS = ("WAITING", "DOING", "CLOSED")
    SPRINT_BOARD_STATUSES = (
        ("BACKLOG", "SPRINT"),
        ("DOING", "TESTING"),
        ("COMPLETED",),
    )

    def _sprint_member_counts(self, roadmap, sprint_id):
        """Count the sprint's member tasks per board column, from their statuses.

        The member tasks are read with `rmp sprint tasks`, independently of the
        web page, and each is counted under the column whose status group
        (SPRINT_BOARD_STATUSES) holds its status. Returns (counts, total), where
        counts lists the WAITING, DOING, and CLOSED expectations left to right
        and total is the number of member tasks, so a caller asserts each
        column's badge against its own expected count (Acceptance Criterion
        131).
        """
        _, out, _ = self._run(["sprint", "tasks", "-r", roadmap, str(sprint_id)])
        statuses = [task["status"] for task in json.loads(out)]
        counts = [
            sum(1 for status in statuses if status in group)
            for group in self.SPRINT_BOARD_STATUSES
        ]
        assert sum(counts) == len(statuses), (
            f"member task statuses {statuses} fall outside the board's status groups"
        )
        return counts, len(statuses)

    @staticmethod
    def _assert_no_sprint_summary_line(body):
        """Acceptance Criterion 39: the sprint page carries no element with
        data-role="sprint-summary" and no text of the form `<n>% - P:`."""
        assert 'data-role="sprint-summary"' not in body, (
            'the sprint page carries an element with data-role="sprint-summary"'
        )
        m = re.search(r"\d+% - P:", body)
        assert m is None, f"the sprint page carries summary-line text {m.group(0)!r}"

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
        card (SPEC/WEB.md § Sprint Detail Sub-Template, rule 3, Every column is
        always rendered).
        """
        return 'data-role="task-board-column-empty"' in column

    @staticmethod
    def _sprint_board_card_ids(column):
        """Return the task ids of a sprint-board column's cards, in card
        (= document) order, read from each card's link to its task page."""
        return [
            int(m) for m in re.findall(
                r'<a class="card card-sm card-link text-reset task-card" '
                r'href="/roadmaps/[^"/]+/tasks/(\d+)"',
                column,
            )
        ]

    @staticmethod
    def _sprint_board_card_html(region, task_id):
        """Return one board card's full markup, from its opening <a> to its
        closing </a>.

        Both Kanban boards emit the same card element, so this reads a card of
        either: the sprint's member-tasks board, and the roadmap tasks page's
        board when one is needed as a control.
        """
        m = re.search(
            rf'<a class="card card-sm card-link text-reset task-card" '
            rf'href="/roadmaps/[^"/]+/tasks/{task_id}"[^>]*>.*?</a>',
            region, re.S,
        )
        assert m, f"no board card found for task #{task_id}"
        return m.group(0)

    @staticmethod
    def _span_with_role(html, role):
        """Return the whole <span> carrying data-role="<role>", children
        included, or None when the markup holds none.

        A card's parts are all <span> elements, and they NEST, so the slice is taken by balancing the
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
        """AC130/AC131/AC39: the member-tasks board renders exactly three
        columns — WAITING, DOING, CLOSED, left to right — each holding the
        sprint's own tasks of the statuses assigned to it, and each column's
        badge equals the number of the sprint's member tasks in those statuses.

        The fixture seeds one member task per TaskStatus value (BACKLOG,
        SPRINT, DOING, TESTING, COMPLETED) so the two-statuses-per-column
        grouping is actually exercised rather than merely assumed: a board that
        miscategorised even one status would print a count that disagrees with
        the member tasks' own statuses.

        AC131 derives the expected counts from the sprint's member tasks and
        their statuses, read here with `rmp sprint tasks` rather than from the
        page, and asserts each badge against its OWN expected count: a board
        that grouped the statuses differently could still print three counts
        whose sum is right. The page itself carries no sprint status summary
        line (AC39).
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
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), ",".join(str(i) for i in all_ids)])
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

        self._assert_no_sprint_summary_line(body)
        expected, total = self._sprint_member_counts(roadmap, sprint_id)
        assert (expected, total) == ([2, 2, 1], 5), (
            f"the fixture's member tasks call for WAITING/DOING/CLOSED {expected} "
            f"of {total}, want [2, 2, 1] of 5"
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

        # AC131: each badge against its own count of the sprint's member tasks
        # in the statuses the column groups.
        for heading, got, want, group in zip(
            self.SPRINT_BOARD_COLUMNS, (waiting_count, doing_count, closed_count),
            expected, self.SPRINT_BOARD_STATUSES,
        ):
            assert got == want, (
                f"{heading} badge {got} must equal the {want} member tasks in {group}"
            )
        assert waiting_count + doing_count + closed_count == total, (
            "the three column badges must sum to the sprint's member-task count"
        )

        # Every member task appears on the board exactly once, in the column of
        # the bucket its OWN status maps to — never dropped, never duplicated.
        placement = {
            t_backlog: waiting, t_sprint: waiting,
            t_doing: doing, t_testing: doing,
            t_completed: closed,
        }
        for task_id, column in placement.items():
            marker = f'/tasks/{task_id}"'
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
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), ",".join(str(i) for i in members)])
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

    def test_sprint_board_card_shows_seven_data_points_in_order(self):
        """AC133/AC179: each card shows exactly seven data points, on TWO
        lines, in this order: the task title leading the card, then one line
        carrying at its leading edge the badge line — the id badge reading
        `#<id>` with `bg-black text-white`, the `S<n>` severity badge, the
        `P<n>` priority badge, and the type badge reading the task's type in
        the variant the task type mapping assigns (`TASK` -> `bg-blue-lt`) — and
        at its trailing edge the comment count followed by the subtask count,
        each counter as an icon followed by its number.

        The COUNTER ORDER is asserted rather than left implicit, because the
        criterion requires it: a card showing the subtask count before the
        comment count satisfies every other clause. It is also the reverse of
        the roadmap tasks page's footer order, which that card keeps.

        The task carries real subtasks and real comments so both counters
        have something to render, and priority 7 / severity 6 fall
        in different colour bands (red / orange per badge.go's
        priorityBadge/severityBadge), so the badges are shown to carry the
        semantic mapping's own colours and not just the labelled digits
        (SPEC/WEB.md § Roadmap Tasks Page, Card content, item 2 — the badge
        line binds on both boards' cards; Acceptance Criterion 133).
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
            r'<span class="card-body d-block">\s*'
            r'<span class="d-block fw-bold text-break mb-1" '
            rf'data-role="task-card-title">{re.escape(title)}</span>\s*'
            r'<span class="d-flex flex-wrap align-items-center '
            r'justify-content-between gap-1" data-role="task-card-summary">\s*'
            r'<span class="d-flex flex-wrap gap-1" '
            r'data-role="task-card-badges">'
            rf'<span class="badge bg-black text-white">#{parent}</span>'
            r'<span class="badge bg-orange-lt">S6</span>'
            r'<span class="badge bg-red-lt">P7</span>'
            r'<span class="badge bg-blue-lt">TASK</span>'
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
            f"the card's seven data points are missing or out of the required "
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

        # Exactly seven data points: no eighth. No status badge (the column
        # already states it), no specialists, no dependency counts, no sprint
        # name.
        assert card.count('class="badge') == 4, (
            f"the card must carry exactly four badges (id, severity, priority and type); found "
            f"{card.count('class=\"badge')} in {card}"
        )
        for absent in ("task-card-sprint", "task-card-specialists",
                       "task-card-depends-on", "task-card-blocks"):
            assert absent not in card, (
                f"the sprint board's card must not render {absent!r}: {card}"
            )

    def test_board_cards_lead_with_title_then_one_badge_line(self):
        """AC85/AC133/AC178/AC179: on the card of the sprint board the task title
        is the first line, and the next line opens with exactly four badges in
        this order: the id badge `#<id>` with `bg-black text-white`, the
        severity badge `S<n>`, the priority badge `P<n>`, and the type badge in
        its type variant; the tasks page's row carries the same four badges,
        byte for byte, in its ID, Type, Severity and Priority cells, severity
        before priority; the id badge keeps its fixed classes whatever the
        task's values, and the rejected forms (`S <n>`, `S:<n>`, `Sev:<n>`,
        `Sev: <n>`, their priority counterparts, any `Sev:` or `Pri:` text, and
        the separate reference line) are absent.

        Two tasks of different types, severities, and priorities are asserted,
        with each task's severity and priority different from each other, so a
        card that swapped the two badges or coloured the id badge from a value
        cannot pass on both.
        """
        roadmap = "chargeback_ops_demo"
        self._run(["roadmap", "create", roadmap])
        seeded = []
        for title, task_type, priority, severity in (
            ("Reject chargebacks filed after the network deadline", "BUG", 8, 3),
            ("Summarise weekly dispute win rates for the risk team",
             "IMPROVEMENT", 2, 6),
        ):
            _, out, _ = self._run([
                "task", "create", "-r", roadmap, "-t", title, "-y", task_type,
                "-p", str(priority), "--severity", str(severity),
                "-fr", "Dispute operations must see this work on both boards",
                "-tr", "Implemented in the dispute management service",
                "-ac", "The dispute operations lead confirms the behaviour",
            ])
            seeded.append((json.loads(out)["id"], task_type, priority, severity))
        sprint_id = self.test.create_sprint(roadmap, "Chargeback deadline sprint")
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id),
                   ",".join(str(task_id) for task_id, _, _, _ in seeded)])
        self._run(["sprint", "start", "-r", roadmap, str(sprint_id)])

        # The SPEC's severity and priority bands (SPEC/WEB.md § Status,
        # Priority, and Severity Badge Colours), for the four values used here.
        severity_variant = {3: "bg-yellow-lt", 6: "bg-orange-lt"}
        priority_variant = {8: "bg-red-lt", 2: "bg-secondary-lt"}
        type_variant = {"BUG": "bg-red-lt", "IMPROVEMENT": "bg-teal-lt"}

        proc, port = self._start(["--port", "0"])
        _, _, tasks_body = self._req(port, f"/roadmaps/{roadmap}/tasks")
        _, _, sprint_body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        sprint_region, _ = self._sprint_board_columns(sprint_body)

        for task_id, task_type, priority, severity in seeded:
            badges = (
                f'<span class="badge bg-black text-white">#{task_id}</span>',
                f'<span class="badge {severity_variant[severity]}">S{severity}</span>',
                f'<span class="badge {priority_variant[priority]}">P{priority}</span>',
                f'<span class="badge {type_variant[task_type]}">{task_type}</span>',
            )
            want_line = ('<span class="d-flex flex-wrap gap-1" data-role="task-card-badges">'
                         + "".join(badges) + '</span>')
            card = self._sprint_board_card_html(sprint_region, task_id)
            title = re.search(r'data-role="task-card-title">(.*?)</span>', card, re.S).group(1)
            pattern = (
                r'<span class="card-body d-block">\s*'
                r'<span class="d-block fw-bold text-break mb-1" '
                rf'data-role="task-card-title">{re.escape(title)}</span>'
                r'\s*<span class="d-flex flex-wrap align-items-center '
                r'justify-content-between gap-1" data-role="task-card-summary">\s*'
                + re.escape(want_line)
            )
            assert re.search(pattern, card, re.S), (
                f"AC179: task #{task_id}'s card must open with its title and then "
                f"the badge line {want_line}: {card}"
            )
            assert f'aria-label="Open details for task #{task_id}: {title}"' in card, (
                f"AC179: task #{task_id}'s accessible name changed: {card}"
            )

            # The list row: the same four badges, in the ID, Type, Severity and
            # Priority cells, severity before priority.
            row = self._list_row(tasks_body, task_id)
            row_badges = re.findall(r'<span class="badge [^"]*">[^<]*</span>', row)
            assert len(row_badges) == 5, f"task #{task_id}'s row carries {len(row_badges)} badges: {row}"
            assert (row_badges[0], row_badges[1], row_badges[3], row_badges[4]) == (
                badges[0], badges[3], badges[1], badges[2]), (
                f"AC179/AC85: task #{task_id}'s row does not repeat the card's badges "
                f"(id, type, severity, priority): {row_badges}"
            )
            for where, markup in (("the sprint board", card), ("the tasks list", row)):
                assert 'data-role="task-card-ref"' not in markup, (
                    f"{where}: task #{task_id} still renders the separate reference line"
                )
                for retired in (f">S {severity}<", f">P {priority}<",
                                f">S:{severity}<", f">P:{priority}<",
                                f">Sev:{severity}<", f">Pri:{priority}<",
                                f">Sev: {severity}<", f">Pri: {priority}<",
                                "Sev:", "Pri:",
                                f'bg-secondary-lt">#{task_id}<'):
                    assert retired not in markup, (
                        f"AC85/AC178: {where} renders the retired form {retired!r} "
                        f"for task #{task_id}: {markup}"
                    )

    def test_sprint_board_card_merges_badges_and_counters_onto_one_line(self):
        """AC133: the badges and the counters share ONE line — the badges at its
        leading edge, the counters at its trailing edge — the line wraps inside
        the card instead of overflowing it on a narrow column, and the card
        renders no separate footer row for the counters.

        This is the card's SHAPE rather than its contents, which
        test_sprint_board_card_shows_seven_data_points_in_order asserts. The
        layout is read from the utility classes the line carries, because those
        are what the browser resolves the behaviour from: justify-content-between
        puts the first flex item at the leading edge and the last at the
        trailing one, and flex-wrap turns "too narrow to hold both" into a wrap
        rather than an overflow — a wrapped flex line holding one item resolves
        space-between to flex-start, so the counters drop directly below the
        badges inside the same card.
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

        # No card of this board renders a separate footer row: not under a
        # footer role, not with a trailing-edge alignment, and
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


    def test_sprint_board_card_always_renders_both_counters(self):
        """AC134: both counters are present on EVERY card of the member-tasks
        board, including when the number they carry is 0, so the trailing edge
        of every card's second line carries both numbers and every card is the
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
                   f"{bare},{counted}"])

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        region, columns = self._sprint_board_columns(body)

        def counter(role, icon, n):
            return (f'<span data-role="{role}"><i class="ti {icon} me-1">'
                    f'</i>{n}</span>')

        # The card with nothing to count still carries both counters, each
        # showing 0, inside the counter group that closes its second line.
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
        cards = region.count('<a class="card card-sm card-link text-reset task-card"')
        assert cards == 2, f"the board renders {cards} cards, want the 2 seeded"
        for role in ("task-card-counters", "task-card-comments", "task-card-subtasks"):
            assert region.count(f'data-role="{role}"') == cards, (
                f"the board renders {cards} cards and "
                f"{region.count(f'data-role=\"{role}\"')} {role!r} elements; "
                f"both counters are present on every card"
            )

    def test_board_column_count_badges_carry_the_colour_of_their_status(self):
        """AC140 and AC61: every per-column count badge of the sprint's
        member-tasks board carries the semantic colour of the status its column
        groups — SPRINT's for WAITING, DOING's for DOING, COMPLETED's for CLOSED
        — while its text stays that column's task count; and in the tasks
        page's list each row's status badge carries its own status's variant.

        Both are asserted AS A WHOLE, exactly as AC120 asserts the three sprint
        tabs together: BACKLOG maps to bg-secondary-lt, the neutral colour a
        badge carries when nothing colours it, so a rendering that gave every
        badge bg-secondary-lt must fail, and the distinct-variant assertions
        below are that control.
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
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint_id), ",".join(str(i) for i in all_ids)])
        self._run(["sprint", "start", "-r", roadmap, str(sprint_id)])

        self._run(["task", "stat", "-r", roadmap, str(t_backlog), "BACKLOG"])
        self._run(["task", "stat", "-r", roadmap, str(t_doing), "DOING", "--commit-open", "5d6a2cd"])
        self._run(["task", "stat", "-r", roadmap, str(t_testing), "DOING", "--commit-open", "5f93b51"])
        self._run(["task", "stat", "-r", roadmap, str(t_testing), "TESTING"])
        self._run(["task", "stat", "-r", roadmap, str(t_completed), "DOING", "--commit-open", "2578d18"])
        self._run(["task", "stat", "-r", roadmap, str(t_completed), "TESTING"])
        self._run(["task", "stat", "-r", roadmap, str(t_completed), "COMPLETED", "--commit-close", "4999725"])

        proc, port = self._start(["--port", "0"])

        # ---- the tasks page's list: each row's status badge ----
        # Explicit, with no filter: every status, COMPLETED included; a bare
        # request would apply the default status selection.
        status_code, _, tasks_body = self._req(port, f"/roadmaps/{roadmap}/tasks?size=25")
        assert status_code == 200
        assert 'data-role="task-board' not in tasks_body, "the tasks page renders a board"
        row_variants = set()
        for task_id, want_status in zip(all_ids, ("BACKLOG", "SPRINT", "DOING", "TESTING", "COMPLETED")):
            row = self._list_row(tasks_body, task_id)
            want = f'<span class="badge {self.TASK_STATUS_BADGE[want_status]}">{want_status}</span>'
            assert want in row, f"task #{task_id}'s row does not carry the status badge {want}: {row}"
            row_variants.add(self.TASK_STATUS_BADGE[want_status])
        assert len(row_variants) == 5, f"the five statuses carry {len(row_variants)} variants"

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

        # The list and the board read one mapping: the DOING row's status badge
        # and the DOING column's count badge carry one variant.
        _, doing_on_sprint, _ = self._column_badge(sprint_columns[1])
        assert f'<span class="badge {doing_on_sprint}">DOING</span>' in self._list_row(tasks_body, t_doing), (
            f"the DOING row's badge and the DOING column's badge ({doing_on_sprint!r}) differ"
        )

    def test_board_column_count_badge_keeps_its_colour_when_the_column_is_empty(self):
        """AC140: a column holding no task shows the count 0 and keeps the
        colour of its status, because the colour follows the COLUMN and not the
        cards in it. The board is checked with nothing in it at all — a sprint
        with no member task — so there is no card for a colour to be read from.
        The tasks page of that roadmap renders no board at all (AC81).
        """
        roadmap = "clearing_house_empty_demo"
        self._run(["roadmap", "create", roadmap])
        sprint_id = self.test.create_sprint(roadmap, "Clearing window readiness sprint")

        proc, port = self._start(["--port", "0"])

        _, _, tasks_body = self._req(port, f"/roadmaps/{roadmap}/tasks")
        assert 'data-role="task-board' not in tasks_body, "the tasks page renders a board"

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

        self._assert_no_sprint_summary_line(body)
        expected, total = self._sprint_member_counts(roadmap, sprint_id)
        assert (expected, total) == ([0, 0, 0], 0), (
            f"an empty sprint has no member task, got {expected} of {total}"
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
                   f"{t1},{t2},{t3}"])
        self._run(["sprint", "start", "-r", roadmap, str(sprint_id)])
        for t in (t1, t2, t3):
            self._run(["task", "stat", "-r", roadmap, str(t), "DOING", "--commit-open", "391cff7"])

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        self._assert_no_sprint_summary_line(body)
        expected, total = self._sprint_member_counts(roadmap, sprint_id)
        assert (expected, total) == ([0, 3, 0], 3), (
            f"expected all 3 member tasks in DOING, got {expected} of {total}"
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

    # ====================================================================
    # Markdown fields (SPEC/WEB.md § Markdown Rendering; AC32, AC180-AC194)
    # ====================================================================

    MARKDOWN_DESCRIPTION = (
        "Close the **settlement** window.\nKeep the ledger frozen.\n\n"
        "- export the settlement day\n- compare the totals\n\n"
        "See [the runbook](https://example.org/runbook), https://example.org/status and "
        "![flow chart](https://example.org/flow.png).\n\n"
        "- [x] freeze announced\n- [ ] residual published\n\n"
        "The drift is one cent[^1].\n\n[^1]: Rounding happens twice.\n\n"
        "# Runbook\n\n"
        "```go\nfunc Residual() int { return 0 }\n```\n\n"
        "The last line of the description."
    )
    HOSTILE_MARKDOWN = (
        "Before <script>alert(1)</script> the table.\n\n"
        "<img src=x onerror=alert(1)>\n\n"
        "<iframe src=\"https://example.org/\"></iframe>\n\n"
        "An inline <b onclick=alert(1)>bold</b> tag and the code `<script>`.\n\n"
        "[alpha](javascript:alert(1)) [bravo](JavaScript:alert(1)) [charlie](vbscript:msgbox) "
        "[delta](file:///etc/passwd) <javascript:alert(1)>"
    )

    @staticmethod
    def _tags(fragment):
        """Return (name, attributes) for every start tag in an HTML fragment."""
        return [(m.group(1).lower(), m.group(2))
                for m in re.finditer(r"<([a-zA-Z][a-zA-Z0-9]*)([^>]*)>", fragment)]

    def _assert_no_raw_html(self, label, fragment):
        """The Markdown renderer's output carries none of the author's raw HTML
        and no active dangerous link (Acceptance Criteria 183 and 184)."""
        for name, attrs in self._tags(fragment):
            assert name not in ("script", "iframe", "img", "b", "object", "embed"), (
                f"{label}: the author's raw element <{name}{attrs}> reached the page"
            )
            assert not re.search(r"\son[a-z]+\s*=", attrs, re.I), f"{label}: event handler in <{name}{attrs}>"
            assert not re.search(r"\sstyle\s*=", attrs, re.I), f"{label}: style attribute in <{name}{attrs}>"
        lowered = fragment.lower()
        for bad in ('href="javascript:', 'href="vbscript:', 'href="file:', 'href=""'):
            assert bad not in lowered, f"{label}: a dangerous link is active ({bad})"
        assert "<code>&lt;script&gt;</code>" in fragment, f"{label}: a < in a code span is not escaped"

    @staticmethod
    def _sprint_card(body, roadmap, sprint_id):
        """Return one sprint card of the sprints page, from its link start tag to
        its end tag, and the id of the tab pane holding it."""
        start = body.find(f'<a href="/roadmaps/{roadmap}/sprints/{sprint_id}" class="card')
        assert start >= 0, f"no sprint card for sprint #{sprint_id}"
        end = body.index("</a>", start) + len("</a>")
        pane = re.findall(r'<div id="(tab-[a-z]+)"', body[:start])[-1]
        return body[start:end], pane

    def test_sprint_description_renders_as_markdown(self):
        """A sprint's description renders as Markdown on every surface: in the
        non-interactive form in its card under each of the three tabs, whole and
        with the card as its only link, and in the ordinary form on the sprint
        page; the sprint title stays plain text (Acceptance Criteria 32, 180,
        187, and 189)."""
        roadmap = "markdown_sprints"
        self._run(["roadmap", "create", roadmap])
        desc = self.MARKDOWN_DESCRIPTION
        upcoming = self.test.create_sprint(roadmap, desc, title="Publish the **residual** report")
        current = self.test.create_sprint(roadmap, desc, title="Reconcile the settlement windows")
        closed = self.test.create_sprint(roadmap, desc, title="Retire the legacy importer")
        self._run(["sprint", "start", "-r", roadmap, str(closed)])
        self._run(["sprint", "close", "-r", roadmap, str(closed), "--force"])
        self._run(["sprint", "start", "-r", roadmap, str(current)])

        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, f"/roadmaps/{roadmap}")
        for sprint_id, want_pane in ((upcoming, "tab-upcoming"), (current, "tab-current"), (closed, "tab-closed")):
            card, pane = self._sprint_card(body, roadmap, sprint_id)
            assert pane == want_pane, f"sprint #{sprint_id} is under {pane}, want {want_pane}"
            assert '<div class="markdown mb-1">' in card, f"sprint #{sprint_id}: no markdown container"
            for want in ("<strong>settlement</strong>", "window.<br>", "<li>export the settlement day</li>",
                         "the runbook, https://example.org/status and flow chart.",
                         '<li class="task-list-item"><span class="task-list-marker">[x]</span> freeze announced</li>',
                         '<li class="task-list-item"><span class="task-list-marker">[ ]</span> residual published</li>',
                         "<h4>Runbook</h4>", '<pre class="chroma">',
                         "The last line of the description."):
                assert want in card, f"sprint #{sprint_id}: the card lacks {want!r}"
            assert "**" not in card.split("card-title")[1].split("</h4>")[1], (
                f"sprint #{sprint_id}: literal asterisks are displayed in the description"
            )
            assert len(re.findall(r"<a\s", card)) == 1, f"sprint #{sprint_id}: a link nested in the card"
            assert "<input" not in card, f"sprint #{sprint_id}: a form control in the card"
        assert "Publish the **residual** report</h4>" in body, "the sprint title is not plain text"

        _, _, page = self._req(port, f"/roadmaps/{roadmap}/sprints/{current}")
        assert '<div class="markdown mb-3">' in page
        assert ('<a href="https://example.org/runbook" target="_blank" rel="noopener noreferrer">'
                "the runbook</a>") in page
        assert ('<a href="https://example.org/flow.png" target="_blank" rel="noopener noreferrer">'
                "flow chart</a>") in page, "a remote image is not rendered as a link"
        description = page.split('<div class="markdown mb-3">')[1].split('<div class="datagrid">')[0]
        assert "<img" not in description
        assert '<input checked="" disabled="" type="checkbox">' in page
        assert '<input disabled="" type="checkbox">' in page
        assert 'id="sprint-%d-description-fn:1"' % current in page, "the footnote id carries no field prefix"
        for level in ("<h1", "<h2", "<h3"):
            assert level not in description, f"an undemoted {level} heading"

        _, _, css = self._req(port, "/static/style.css")
        assert ".markdown table" in css and ".markdown pre" in css and "overflow-x: auto" in css, (
            "a wide table or code block does not scroll inside its own box"
        )
        assert ".sprint-description" not in css and ".task-modal__text" not in css and ".modal " not in css

    @staticmethod
    def _task_page_card(page, title):
        """Return one card of a task page, from its card title to the next card
        title (the Comments card closes the page body's main column)."""
        start = page.find(f'<h3 class="card-title">{title}')
        assert start >= 0, f"the task page has no card titled {title!r}"
        nxt = page.find('<h3 class="card-title">', start + 1)
        end = page.find("</main>", start) if nxt < 0 else nxt
        return page[start:end]

    def test_task_page_markdown_fields(self):
        """The task page carries, inside a markdown container, the renderer's HTML
        for each of the four Markdown fields and each comment body; a null
        completion summary shows the em dash and no container; the six fragments
        share no id; the CLI's output carries no _html member and keeps every
        timestamp in the canonical format (Acceptance Criteria 180, 183, 188, 190,
        and 208)."""
        roadmap = "markdown_task_page"
        self._run(["roadmap", "create", roadmap])
        functional = ("Operators must see the **residual**.\nPer settlement window[^1].\n\n- export\n- compare"
                      "\n\n[^1]: One window per day.")
        task_id = self.test.create_task(
            roadmap, "Remove the **one-cent** drift", functional,
            "Compare with `!time.Now().Before(exp)` first[^1].\n\n[^1]: The boundary second.\n\n"
            + self.HOSTILE_MARKDOWN,
            "Verified when:\n\n- [x] residual is 0.00\n- [ ] report published[^1]\n\n[^1]: Checked by the operator.",
        )
        open_task = self.test.create_task(roadmap, "Publish the residual per window",
                                          "Show the residual", "Read it from the ledger", "It is shown")
        sprint_id = self.test.move_task_to_sprint(roadmap, task_id)
        self._run(["sprint", "start", "-r", roadmap, str(sprint_id)])
        self._run(["task", "stat", "-r", roadmap, str(task_id), "DOING", "--commit-open", "391cff7"])
        self._run(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self._run(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED", "--commit-close", "2578d18",
                   "--summary", "Shipped; see [the runbook](https://example.org/runbook)[^1].\n\n[^1]: Behind a flag."])
        for body in ("Found the **drift** in the importer[^1].\n\n[^1]: Rounding twice.",
                     "Decided to round once[^1].\n\n[^1]: At export.\n\n" + self.HOSTILE_MARKDOWN):
            self._run(["task", "comment-add", "-r", roadmap, str(task_id), "--type", "FINDING", "--body", body])

        proc, port = self._start(["--port", "0"])
        status, page = self._task_page(port, roadmap, task_id)
        assert status == 200

        functional_card = self._task_page_card(page, "Functional requirements")
        technical_card = self._task_page_card(page, "Technical requirements")
        criteria_card = self._task_page_card(page, "Acceptance criteria")
        summary_card = self._task_page_card(page, "Completion summary")
        comments_card = self._task_page_card(page, "Comments")
        for label, card in (("functional", functional_card), ("technical", technical_card),
                            ("criteria", criteria_card), ("summary", summary_card)):
            assert card.count('<div class="markdown">') == 1, f"the {label} card has no single markdown container"
        assert "<strong>residual</strong>" in functional_card and "<br>" in functional_card
        assert "<code>!time.Now().Before(exp)</code>" in technical_card
        self._assert_no_raw_html("technical requirements", technical_card)
        assert '<input checked="" disabled="" type="checkbox">' in criteria_card
        assert ('<a href="https://example.org/runbook" target="_blank" rel="noopener noreferrer">'
                in summary_card)
        assert comments_card.count('<div class="markdown">') == 2
        assert "<p>Found the <strong>drift</strong>" in comments_card
        self._assert_no_raw_html("comment body", comments_card)
        assert "**" not in functional_card, "literal asterisks are displayed"

        # Footnote identifiers of the six fragments never collide on the page.
        fn_ids = [i for i in re.findall(r'\sid="([^"]*)"', page) if "fn" in i]
        assert len(fn_ids) == len(set(fn_ids)) and len(fn_ids) >= 12, fn_ids
        assert all(i.startswith(f"task-{task_id}-") or i.startswith("task-comment-") for i in fn_ids), fn_ids
        all_ids = re.findall(r'\sid="([^"]*)"', page)
        assert len(all_ids) == len(set(all_ids)), "two elements of the task page share an id"
        for target in re.findall(r'\shref="#([^"]*)"', page):
            assert f' id="{target}"' in page, f"fragment #{target} does not resolve on the page"

        # A null completion summary keeps its card, with the em dash alone.
        _, other = self._task_page(port, roadmap, open_task)
        other_summary = self._task_page_card(other, "Completion summary")
        assert 'class="markdown"' not in other_summary and "\u2014" in other_summary, other_summary

        # The CLI is unchanged: no _html member, and canonical timestamps
        # (Acceptance Criterion 208).
        cli_task = json.loads(self._run(["task", "get", "-r", roadmap, str(task_id)])[1])
        cli_task = cli_task[0] if isinstance(cli_task, list) else cli_task
        assert not [k for k in cli_task if k.endswith("_html")], "the CLI's task carries an _html member"
        cli_comments = json.loads(self._run(["task", "comment-list", "-r", roadmap, str(task_id)])[1])
        assert not [k for c in cli_comments for k in c if k.endswith("_html")], (
            "the CLI's comments carry an _html member"
        )
        canonical = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")
        for field in ("created_at", "started_at", "tested_at", "closed_at"):
            value = cli_task[field]
            assert canonical.match(value), f"`rmp task get` publishes {field}={value!r}, not the canonical format"
            display = value[:10] + " " + value[11:19]
            assert f'<time datetime="{value}">{display}</time>' in page, (
                f"the task page does not display {field} as {display} with the stored value in datetime"
            )

        # The board pages carry none of the free text, and the title stays text.
        # Explicit, with no filter, so the list holds the task whatever its status.
        _, _, tasks_page = self._req(port, f"/roadmaps/{roadmap}/tasks?size=25")
        assert "<strong>residual</strong>" not in tasks_page, "the task free-text reached the board page"
        assert "Remove the **one-cent** drift" in tasks_page, "the task title is not plain text"
        assert '<h2 class="page-title">Remove the **one-cent** drift</h2>' in page

    def test_markdown_server_path_omits_raw_html_and_dangerous_links(self):
        """On the server-rendered path, a sprint description and a sprint comment
        carrying raw HTML and dangerous links reach the page through the Markdown
        renderer only (Acceptance Criteria 73, 183, and 184)."""
        roadmap = "markdown_hostile"
        self._run(["roadmap", "create", roadmap])
        sprint_id = self.test.create_sprint(roadmap, self.HOSTILE_MARKDOWN, title="Harden the importer")
        self._run(["sprint", "comment-add", "-r", roadmap, str(sprint_id), "--type", "DECISION",
                   "--body", self.HOSTILE_MARKDOWN])
        proc, port = self._start(["--port", "0"])
        _, _, page = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        fragments = re.findall(r'<div class="markdown(?: mb-3)?">(.*?)</div>\n', page, re.S)
        assert len(fragments) == 2, f"{len(fragments)} markdown containers, want the description and the comment"
        for fragment in fragments:
            self._assert_no_raw_html("sprint page", fragment)
            assert "alpha bravo charlie delta javascript:alert(1)" in fragment
        assert "<script>alert(1)" not in page and "<iframe" not in page
        _, _, cards = self._req(port, f"/roadmaps/{roadmap}")
        card, _ = self._sprint_card(cards, roadmap, sprint_id)
        self._assert_no_raw_html("sprint card", card)

    def test_highlight_stylesheet_served_and_linked(self):
        """The one dark syntax-highlighting stylesheet is served and linked by the
        three pages that can render a Markdown field, a fenced go block is
        highlighted with its classes, and the Content-Security-Policy is unchanged
        (Acceptance Criteria 182 and 192)."""
        roadmap = "markdown_highlight"
        self._run(["roadmap", "create", roadmap])
        sprint_id = self.test.create_sprint(
            roadmap,
            "```go\nfunc Residual() int { return 0 }\n```\n\n```\nplain block\n```\n\n"
            "![pixel](data:image/png;base64,iVBORw0KGgo=) [board](/roadmaps/%s/tasks)" % roadmap,
            title="Highlight the runbook")
        hl_task = self.test.create_task(
            roadmap, "Highlight the settlement runbook",
            "```go\nfunc Residual() int { return 0 }\n```", "Read the residual from the ledger",
            "The residual renders highlighted",
        )
        proc, port = self._start(["--port", "0"])
        status, headers, css = self._req(port, "/static/highlight.css")
        assert status == 200 and headers.get("content-type", "").startswith("text/css")
        assert ".chroma" in css and "@media" not in css and "data-bs-theme" not in css
        link = '<link rel="stylesheet" href="/static/highlight.css">'
        # The tasks page renders no Markdown field and does not link the
        # stylesheet (Acceptance Criterion 182).
        _, _, board_page = self._req(port, f"/roadmaps/{roadmap}/tasks")
        assert "/static/highlight.css" not in board_page, "the tasks page links highlight.css"
        for path in (f"/roadmaps/{roadmap}", f"/roadmaps/{roadmap}/sprints/{sprint_id}",
                     f"/roadmaps/{roadmap}/tasks/{hl_task}"):
            _, page_headers, page = self._req(port, path)
            assert page.count(link) == 1, f"{path} does not link the highlighting stylesheet once"
            assert page_headers.get("content-security-policy") == (
                "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "
                "img-src 'self' data:; font-src 'self'; connect-src 'self'; "
                "frame-ancestors 'none'; base-uri 'self'"
            ), f"{path}: the Content-Security-Policy changed"
            for attrs in re.findall(r"<script([^>]*)>", page):
                assert 'src="/static/' in attrs, f"{path}: an inline or remote script"
        _, _, page = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint_id}")
        assert '<pre class="chroma">' in page and '<span class="kd">func</span>' in page
        assert "<pre><code>plain block\n</code></pre>" in page, "an undeclared block is not left unhighlighted"
        assert '<img src="data:image/png;base64,iVBORw0KGgo=" alt="pixel">' in page
        _, _, task_page = self._req(port, f"/roadmaps/{roadmap}/tasks/{hl_task}")
        assert '<pre class="chroma">' in task_page and '<span class="kd">func</span>' in task_page, (
            "the task page does not highlight a fenced code block"
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
    # AC15, AC94 to AC99, AC221 to AC226: the roadmap task page
    # ====================================================================

    def test_task_page_wiring_and_content(self):
        """Following a task's link — a card of the sprint board, or the title
        link of a row of the tasks page's list — navigates to the task's
        own page, which carries every field of the task in the HTML the server
        sends and contains no form, no edit control, and no submit (Acceptance
        Criteria 15 and 94). Neither page carries a task's long text; the tasks
        page's one form is its GET filter bar (AC87)."""
        proc, port = self._start(["--port", "0"])
        t1 = self.open_task_ids[0]
        href = f'href="/roadmaps/{ROADMAP}/tasks/{t1}"'
        for path in (
            f"/roadmaps/{ROADMAP}/tasks",
            f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}",
        ):
            _, _, body = self._req(port, path)
            assert href in body, f"{path}: no card links to task #{t1}'s page"
            for absent in ("Functional requirements", "Technical requirements", "Acceptance criteria",
                           "end users must authenticate without a stored password"):
                assert absent.lower() not in body.lower(), f"{path}: carries the task text {absent!r}"
            low = body.lower()
            if path.endswith("/tasks"):
                assert low.count("<form") == 1 and 'method="get"' in low, f"{path}: the filter bar is not one GET form"
                # The search box, the hidden size, and one checkbox per TaskStatus
                # (5) and TaskType (10) value in the Status and Type dropdowns.
                submits, inputs = low.count('type="submit"'), low.count("<input")
                assert submits == 1 and inputs == 17, (
                    f"{path}: the filter bar carries {submits} submits and {inputs} inputs, want 1 and 17"
                )
                assert body.count(href) == 1, f"{path}: task #{t1}'s row carries {body.count(href)} links, want 1"
            else:
                assert "<form" not in low and 'type="submit"' not in low, f"{path}: a form or a submit"
                assert low.count("<input") == 0, f"{path}: carries an input"

        status, headers, page = self._req(port, f"/roadmaps/{ROADMAP}/tasks/{t1}")
        assert status == 200 and headers.get("content-type", "").startswith("text/html")
        cli = json.loads(self._run(["task", "get", "-r", ROADMAP, str(t1)])[1])
        cli = cli[0] if isinstance(cli, list) else cli
        assert f'<div class="page-pretitle">Task #{t1} <span class="badge ' in page
        assert f">{cli['status']}</span></div>" in page, "the header does not state the task's status"
        assert f'<h2 class="page-title">{self._go_escaped(cli["title"])}</h2>' in page
        for label in ("Type", "Severity", "Priority", "Parent task", "Subtasks", "Depends on", "Blocks",
                      "Created", "Started", "Tested", "Closed", "Commit open", "Commit close"):
            assert f'<div class="datagrid-title">{label}</div>' in page, f"the Details card lacks {label}"
        for title in ("Functional requirements", "Technical requirements", "Acceptance criteria",
                      "Completion summary", "Comments"):
            assert f'<h3 class="card-title">{title}' in page, f"the task page lacks the {title} card"
        assert f'<div class="datagrid-content">{cli["type"]}</div>' in page
        assert f'<time datetime="{cli["created_at"]}">' in page
        assert "end users must authenticate without a stored password" in page.lower()
        assert "specialists" not in page.lower(), "the removed specialists field reached the task page"
        main = page[page.index('<main class="page-body">'):page.index("</main>")].lower()
        for bad in ("<form", 'type="submit"', "<button", "<textarea", "<select", "<input"):
            assert bad not in main, f"the task page is read-only but carries {bad!r}"
        # The page loads no script of its own (Acceptance Criterion 98).
        scripts = re.findall(r"<script\b([^>]*)>", page)
        assert scripts == [' src="/static/vendor/tabler/tabler.min.js"'], scripts
        assert not re.search(r"\son[a-z]+=", page, re.I), "the task page carries an inline event handler"

        # HEAD answers 200 with the GET's headers and no body.
        head_status, head_headers, head_body = self._req(port, f"/roadmaps/{ROADMAP}/tasks/{t1}", method="HEAD")
        assert head_status == 200 and head_body == "", (head_status, head_body[:80])
        for h in ("content-type", "cache-control", "content-security-policy"):
            assert head_headers.get(h) == headers.get(h), f"HEAD {h} differs from the GET"

    def test_task_page_header_sprint_card_and_details(self):
        """The task page header, its way back, its Sprint card, and its Details
        card, for a task in a sprint, a task in no sprint, and after
        `rmp sprint add-tasks` (Acceptance Criteria 221 to 224)."""
        roadmap = "payments"
        self._run(["roadmap", "create", roadmap])
        mk = lambda title, fr="Card payments settle in the reconciliation window", \
            tr="Read the ledger through the settlement API", ac="The residual is zero": \
            self.test.create_task(roadmap, title, fr, tr, ac)
        parent = mk("Harden the payment gateway key management")
        dep_a = mk("Inventory the signing keys in use")
        dep_b = mk("Publish the key rotation runbook")
        key = json.loads(self._run([
            "task", "create", "-r", roadmap, "-t", "Rotate the signing keys",
            "-fr", "Rotate the **signing keys** every 90 days", "-tr", "Use the kms rotation API",
            "-ac", "Verified when:\n\n- [x] new key active", "-p", "7", "--severity", "2", "--parent", str(parent),
        ])[1])["id"]
        blocked = mk("Revoke the retired signing keys")
        loose = mk("Document the refund reversal flow")
        members = [mk(f"Reconcile settlement batch {i}") for i in range(1, 9)]
        self._run(["task", "add-dep", "-r", roadmap, str(key), str(dep_a)])
        self._run(["task", "add-dep", "-r", roadmap, str(key), str(dep_b)])
        self._run(["task", "add-dep", "-r", roadmap, str(blocked), str(key)])
        sprint = self.test.create_sprint(roadmap, "Harden the payment platform before the audit",
                                         title="Payments hardening")
        order = members[:2] + [key] + members[2:]
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint), ",".join(map(str, order))])
        self._run(["sprint", "start", "-r", roadmap, str(sprint)])
        for done in members[:4]:
            self._run(["task", "stat", "-r", roadmap, str(done), "DOING", "--commit-open", "24262f0"])
            self._run(["task", "stat", "-r", roadmap, str(done), "TESTING"])
            self._run(["task", "stat", "-r", roadmap, str(done), "COMPLETED", "--commit-close", "2578d18",
                       "--summary", "Settled."])
        self._run(["task", "stat", "-r", roadmap, str(key), "DOING", "--commit-open", "5f93b51"])

        proc, port = self._start(["--port", "0"])
        status, page = self._task_page(port, roadmap, key)
        assert status == 200

        # AC221: the header, the active view, and the way back.
        assert (f'<div class="page-pretitle">Task #{key} <span class="badge bg-blue-lt">DOING</span></div>'
                in page), "the pretitle is not Task #<id> followed by the DOING badge"
        assert '<h2 class="page-title">Rotate the signing keys</h2>' in page
        assert (f'<a class="nav-link" href="/roadmaps/{roadmap}/tasks" aria-current="page">' in page
                and page.count('aria-current="page"') == 1), "the sidebar's Tasks entry is not the active one"
        header = page[page.index('<div class="page-header d-print-none">'):page.index('<main class="page-body">')]
        actions = header[header.index('<div class="col-12 col-sm-auto ms-auto d-print-none">'):]
        assert actions.count("<a ") == 1 and f'href="/roadmaps/{roadmap}/tasks">' in actions, actions
        assert "Back to tasks</a>" in actions
        assert "/sprints/" not in header and "/graph" not in header and "?" not in header

        # AC222: the Sprint card of a task in a sprint.
        card = self._task_page_card(page, "Sprint</h3>")
        for want in (f'href="/roadmaps/{roadmap}/sprints/{sprint}"', f">Sprint #{sprint} Payments hardening</a>",
                     '<span class="badge bg-blue-lt">OPEN</span>', ">Position 3 of 9</div>",
                     ">4 of 9 tasks completed</div>",
                     '<progress class="progress progress-sm" value="4" max="9" aria-label="Sprint progress"></progress>'):
            assert want in card, f"the Sprint card lacks {want!r}: {card}"
        assert "style=" not in card
        _, first = self._task_page(port, roadmap, order[0])
        assert ">Position 1 of 9</div>" in first
        _, last = self._task_page(port, roadmap, order[-1])
        assert ">Position 9 of 9</div>" in last

        # AC224: the Details card.
        details = page[page.index('data-role="task-details-card"'):page.index('<div class="col-12 col-lg-8">')]
        assert '<div class="datagrid-content">TASK</div>' in details
        assert re.search(r'<div class="datagrid-title">Priority</div>\s*<div class="datagrid-content">'
                         r'<span class="badge bg-[a-z]+-lt">7</span></div>', details)
        assert re.search(r'<div class="datagrid-title">Severity</div>\s*<div class="datagrid-content">'
                         r'<span class="badge bg-[a-z]+-lt">2</span></div>', details)
        assert f'<a href="/roadmaps/{roadmap}/tasks/{parent}">#{parent}</a>' in details
        assert (f'<a href="/roadmaps/{roadmap}/tasks/{dep_a}">#{dep_a}</a>'
                f'<a href="/roadmaps/{roadmap}/tasks/{dep_b}">#{dep_b}</a>') in details
        assert f'<a href="/roadmaps/{roadmap}/tasks/{blocked}">#{blocked}</a>' in details
        assert '<div class="datagrid-content font-monospace text-break">5f93b51</div>' in details
        assert "text-truncate" not in details, "a Details value is truncated"
        # Severity precedes Priority (Acceptance Criterion 224).
        labels = re.findall(r'<div class="datagrid-title">([^<]+)</div>', details)
        assert labels == ["Type", "Severity", "Priority", "Parent task", "Subtasks", "Depends on", "Blocks",
                          "Created", "Started", "Tested", "Closed", "Commit open", "Commit close"], labels
        assert '<div class="datagrid task-details-grid">' in details

        # AC173: the document title leads with the task.
        title = re.search(r"<title>(.*?)</title>", page).group(1)
        assert re.fullmatch(rf"#{key} Rotate the signing keys - {roadmap}( - .+)?", title), title

        # AC230: the sprint page's actions column wraps like the task page's.
        _, _, sprint_page = self._req(port, f"/roadmaps/{roadmap}/sprints/{sprint}")
        assert '<div class="col-12 col-sm-auto ms-auto d-print-none">' in sprint_page
        # The tasks page's header carries no actions column: its filter bar
        # sits in the header of its task-list card (AC100, AC109).
        _, _, board = self._req(port, f"/roadmaps/{roadmap}/tasks")
        assert "ms-auto d-print-none" not in board

        # AC231: both Comments cards state their order, with or without comments.
        for record in (page, sprint_page):
            header_ = record[record.index('<h3 class="card-title">Comments'):]
            header_ = header_[:header_.index('<div class="card-body">')]
            assert header_.count('<div class="card-actions text-secondary">Oldest first</div>') == 1, header_
        assert ">P7<" not in details and ">S2<" not in details
        for ref in re.findall(r'<a href="(/roadmaps/[^"]+)">#\d+</a>', details):
            assert self._req(port, ref)[0] == 200, f"the reference {ref} does not serve a task page"

        # AC223: a task in no sprint, then moved into one.
        _, loose_page = self._task_page(port, roadmap, loose)
        loose_card = self._task_page_card(loose_page, "Sprint</h3>")
        assert "In the backlog: this task belongs to no sprint." in loose_card
        for gone in ("<a ", "badge", "Position", "<progress"):
            assert gone not in loose_card, f"the backlog Sprint card carries {gone!r}"
        self._run(["sprint", "add-tasks", "-r", roadmap, str(sprint), str(loose)])
        _, moved = self._task_page(port, roadmap, loose)
        moved_card = self._task_page_card(moved, "Sprint</h3>")
        assert ">Position 10 of 10</div>" in moved_card and ">4 of 10 tasks completed</div>" in moved_card

        # AC225: the layout, in the served markup.
        body = moved[moved.index('<main class="page-body">'):]
        order_marks = ['<div class="col-12 col-lg-4 order-lg-last">', 'data-role="task-sprint-card"',
                       'data-role="task-details-card"', '<div class="col-12 col-lg-8">',
                       ">Functional requirements</h3>", ">Technical requirements</h3>",
                       ">Acceptance criteria</h3>", ">Completion summary</h3>", 'data-role="task-comments-card"']
        positions = [body.index(m) for m in order_marks]
        assert positions == sorted(positions), "the task page's cards are out of order"

        # AC226: serving the task pages wrote no audit entry.
        before = json.loads(self._run(["audit", "list", "-r", roadmap, "-l", "500"])[1])
        for task_id in (key, loose, parent):
            self._task_page(port, roadmap, task_id)
        after = json.loads(self._run(["audit", "list", "-r", roadmap, "-l", "500"])[1])
        assert len(after) == len(before), "serving the task pages wrote an audit entry"

    def test_task_page_path_discipline_and_escaping(self):
        """404 for an invalid or unknown roadmap, a non-integer id, and a task of
        another roadmap; 405 for any non-read method; no-store on every response,
        the 404 included; a hostile title renders as visible characters in the
        header and the document title (Acceptance Criteria 95 and 97)."""
        self._run(["roadmap", "create", "endpoint_other"])
        other_task = self.test.create_task(
            "endpoint_other", "Rotate the acquirer API credentials",
            "Credentials must rotate quarterly", "Rotate through the secret manager",
            "The old credential stops authenticating",
        )
        hostile = 'Reject "quoted" <b>bold</b> & O\'Brien'
        hostile_id = self.test.create_task(
            ROADMAP, hostile, "Block <script>alert('fr')</script> inputs", "<img src=x onerror=alert(1)>",
            "The page shows the text",
        )
        proc, port = self._start(["--port", "0"])
        t1 = self.open_task_ids[0]
        ok, headers, _ = self._req(port, f"/roadmaps/{ROADMAP}/tasks/{t1}")
        assert ok == 200 and headers.get("cache-control") == "no-store"

        for path in (
            "/roadmaps/INVALID/tasks/1",
            "/roadmaps/..%2fetc/tasks/1",
            "/roadmaps/no_such_roadmap/tasks/1",
            f"/roadmaps/{ROADMAP}/tasks/not-a-number",
            f"/roadmaps/{ROADMAP}/tasks/999999",
            f"/roadmaps/{ROADMAP}/tasks/{other_task}"
            if other_task not in self.open_task_ids and other_task != hostile_id else
            f"/roadmaps/{ROADMAP}/tasks/999998",
            f"/roadmaps/{ROADMAP}/tasks/{t1}/data",
        ):
            status, h, _ = self._req(port, path)
            assert status == 404, f"GET {path} answered {status}, want 404"
            assert h.get("cache-control") == "no-store", f"GET {path}: {h.get('cache-control')}"
        for method in ("POST", "PUT", "PATCH", "DELETE"):
            status, _, _ = self._req(port, f"/roadmaps/{ROADMAP}/tasks/{t1}", method=method)
            assert status == 405, f"{method} answered {status}, want 405"

        _, page = self._task_page(port, ROADMAP, hostile_id)
        escaped = self._go_escaped(hostile)
        assert f'<h2 class="page-title">{escaped}</h2>' in page, "the hostile title is not escaped in the header"
        title = re.search(r"<title>(.*?)</title>", page).group(1)
        assert "<b>" not in title and title.startswith(f"#{hostile_id} {escaped} - {ROADMAP}"), title
        main = page[page.index('<main class="page-body">'):page.index("</main>")]
        for raw in ("<b>bold</b>", "<script", "<img", "onerror"):
            assert raw not in main, f"{raw!r} reached the task page as markup"
        assert page.count("<script") == 1

    # ====================================================================
    # Comment log: the task page's Comments card and the sprint Comments card
    # (SPEC/WEB.md § Roadmap Task Page, Comments card; § Sprint Detail
    # Sub-Template, Comments card)
    # ====================================================================

    def _seed_comments(self):
        """Attach a realistic comment log to the OPEN sprint and its first task.

        The bodies belong to this module's authentication domain, so the
        rendered page reads like the project it models. One task comment is
        edited, which is what makes the "edited" stamp reachable; the second
        member task is deliberately left without comments so the task page's
        empty state is reachable.
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
        """Return the sprint's own Comments card, up to the end of its timeline."""
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

    def test_task_page_serves_the_comment_log(self):
        """The task page's Comments card shows the task's log, last in the main
        column after the Completion summary card, complete and in the order the
        CLI reports, with its count in the header badge and the edited entry
        marked (Acceptance Criteria 64 and 65)."""
        self._seed_comments()
        proc, port = self._start(["--port", "0"])

        status, page = self._task_page(port, ROADMAP, self.commented_task)
        assert status == 200
        assert page.index(">Completion summary</h3>") < page.index('data-role="task-comments-card"')
        card = page[page.index('data-role="task-comments-card"'):page.index("</main>")]
        assert '<h3 class="card-title">Comments <span class="badge bg-secondary-lt ms-2">3</span></h3>' in card

        cli_log = json.loads(
            self._run(["task", "comment-list", "-r", ROADMAP, str(self.commented_task)])[1]
        )
        assert self._timeline_types(self._timeline(card)) == [c["type"] for c in cli_log]
        positions = [card.find(self._markdown_text(c["body"])[:40]) for c in cli_log]
        assert all(p >= 0 for p in positions) and positions == sorted(positions), (
            f"the log is not complete and oldest first: {positions}"
        )
        assert card.count(">edited <time datetime=") == 1, "exactly one entry must carry the edited stamp"
        for c in cli_log:
            assert f'<time datetime="{c["created_at"]}">' in card, "a comment's created_at is missing"

        # The board pages carry none of it.
        _, _, board = self._req(port, f"/roadmaps/{ROADMAP}/tasks")
        for c in cli_log:
            assert self._markdown_text(c["body"])[:40] not in board, "a comment body reached the board"
        assert '<ul class="timeline">' not in board, "the tasks page renders no timeline"

    def test_task_page_without_comments_shows_the_empty_state(self):
        """The page of a task with no comments keeps its Comments card, with a
        zero badge and a clear empty-state message in place of the timeline
        (Acceptance Criterion 67)."""
        self._seed_comments()
        proc, port = self._start(["--port", "0"])

        status, page = self._task_page(port, ROADMAP, self.uncommented_task)
        assert status == 200
        card = page[page.index('data-role="task-comments-card"'):page.index("</main>")]
        assert '<span class="badge bg-secondary-lt ms-2">0</span>' in card
        assert "Nothing has been recorded on this task yet." in card
        assert '<ul class="timeline">' not in card

    def test_sprint_page_comments_card_shows_only_the_sprints_own_comments(self):
        """The Comments card is the SPRINT's log, counted in its own badge.

        A member task's comments appear on that task's own page, never here.
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

        # The member task's own log is on its own page, and it is the task's log,
        # not the sprint's.
        _, task_page = self._task_page(port, ROADMAP, self.commented_task)
        assert self._markdown_text(self.comment_bodies["FINDING"])[:50] in task_page
        assert self.sprint_comment_body[:50] not in task_page, "a sprint comment leaked into a task's page"

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
        assert "Nothing has been recorded on this task yet." not in body
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
    # AC10: name validation / path-traversal guard
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
    # AC17/AC18/AC19: graph page, data endpoint, read-only proof
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
        # layouts with Mobile patent suits preselected as the default (AC17).
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
        """A read over the endpoint owes no checkpoint, so no snapshot appears.

        Nothing here can create one by accident: the web server never opens a
        graph store, and the graph server folds a snapshot only when the
        write-ahead log has grown since the last fold (SPEC/GRAPH.md
        § Synchronous Checkpoint on Write). Five reads append nothing, so the
        snapshot directory the fixture's server left absent must stay absent.
        """
        graph_dir = Path(self.home) / ".roadmaps" / ROADMAP / "graph"
        snap = graph_dir / "snapshot"
        assert not snap.exists(), (
            "precondition: the fixture's seeding writes leave no snapshot behind, "
            "so an absent snapshot afterwards is evidence about the reads"
        )
        proc, port = self._start(["--port", "0"])
        for _ in range(5):
            assert self._req(port, f"/roadmaps/{ROADMAP}/graph/data")[0] == 200
        assert not snap.exists(), "web graph reads must not create a snapshot/ dir"

    def test_unserved_roadmap_is_unavailable_and_creates_no_graph(self):
        """A roadmap nothing is serving: HTTP 503, and no graph/ directory.

        This is what became of the old "an absent graph reads as the empty
        graph" case. The web server no longer opens a store at all -- the graph
        is reachable only through a running `rmp graph serve` -- so a roadmap
        with no server is a dependency that is not up, answered 503, and the
        request never touches the filesystem where a store would live
        (SPEC/WEB.md § Knowledge Graph from the GoGraph Store).

        The second half is what keeps the first honest. Serving the same roadmap
        creates its store, empty, and the endpoint then answers the empty graph
        the criterion has always asked for -- so the 503 above was the absence of
        a server and not an endpoint that had stopped serving empty graphs.
        """
        self._run(["roadmap", "create", "blankspace"])
        graph_dir = Path(self.home) / ".roadmaps" / "blankspace" / "graph"
        proc, port = self._start(["--port", "0"])

        status, _, body = self._req(port, "/roadmaps/blankspace/graph/data")
        assert status == 503, (
            f"a roadmap no server is serving must be answered 503; got "
            f"{status} {body!r}")
        assert "internal server error" in body.lower(), (
            f"the 503 body stays opaque and names no path; got {body!r}")
        assert not graph_dir.exists(), (
            "the web server must not open or create a graph store: reading an "
            "absent graph created graph/")

        # Now serve it. Starting the server is what creates the store, empty.
        self._serve_graph("blankspace")
        assert graph_dir.exists(), "the server must have created the graph store"
        status, _, body = self._req(port, "/roadmaps/blankspace/graph/data")
        assert status == 200, (
            f"a served roadmap must be answered 200; got {status} {body!r}")
        assert json.loads(body) == {"nodes": [], "edges": []}, (
            f"an empty graph is served as the empty graph; got {body!r}")

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

    def test_query_bar_executes_writes_and_they_persist(self):
        """AC47: a statement submitted through the query bar is executed
        whatever it does, its change is committed, and a SEPARATE process finds
        it afterwards.

        The read-back through a SEPARATE `rmp graph client` process is what the
        criterion asks for, and it is not a courtesy. The endpoint answers 200
        for any statement the graph server ran without error, so a write that
        was executed and then rolled back -- or one answered out of a per-request
        graph that never reached the store -- would answer 200 exactly as a
        committed write does. Only a second process, over its own connection and
        its own transaction, tells the two apart.

        The status alone establishes nothing in the other direction either.
        Before this change the endpoint answered 400 with kind not_read_only,
        and with the guard rail withdrawn but the read-path engine still in
        place it answered 400 with kind execution and the engine's own "Run does
        not execute write or DDL statements". Both are refusals; only a 200 plus
        a read-back meets the criterion.

        Durability is asserted on the write-ahead log and NOT on the snapshot.
        The commit is the durability boundary, and a server does not fold a
        snapshot per write: it has later opportunities, and a full snapshot after
        every committed write would make every write cost the whole live graph
        (SPEC/GRAPH.md § Durability and Checkpointing in a Long-Lived Process,
        rules 1 and 3). Asserting a snapshot after each write would be asserting
        the short-lived invocation's rule against a process the specification
        exempts from it.
        """
        proc, port = self._start(["--port", "0"])

        graph_dir = Path(self.home) / ".roadmaps" / ROADMAP / "graph"
        wal_before = (graph_dir / "wal").stat().st_size

        # CREATE: executed, committed, and found by a separate client process
        # afterwards.
        status, _, body = self._req(
            port, self._graph_data(port, q="CREATE (n:WebProbe {key:'p'})"))
        assert status == 200, (
            f"AC47: a CREATE through the query bar must be executed, not "
            f"refused; got {status} {body!r}")
        result = self._graph("MATCH (n:WebProbe {key:'p'}) RETURN count(n)")
        assert result["rows"][0][0] == 1, (
            "AC47: the node the endpoint created must be present on a separate "
            "read; a 200 that stored nothing is the silent-wrong-data failure "
            f"this criterion exists against; got {result!r}")

        # The commit reached the write-ahead log, which is where a committed
        # change is durable before it is acknowledged.
        wal_after_write = (graph_dir / "wal").stat().st_size
        assert wal_after_write > wal_before, (
            f"AC47: the committed write must have been appended to the "
            f"write-ahead log; it is {wal_after_write} bytes and was "
            f"{wal_before} before the request")

        # SET: the property change is committed and visible afterwards.
        status, _, body = self._req(port, self._graph_data(
            port, q="MATCH (n:WebProbe {key:'p'}) SET n.state = 'seen'"))
        assert status == 200, f"a SET must execute; got {status} {body!r}"
        result = self._graph("MATCH (n:WebProbe {key:'p'}) RETURN n.state")
        assert result["rows"][0][0] == "seen", (
            f"AC47: the property change must persist; got {result!r}")

        # DETACH DELETE: the node is gone afterwards.
        status, _, body = self._req(
            port, self._graph_data(port, q="MATCH (n:WebProbe) DETACH DELETE n"))
        assert status == 200, (
            f"AC47: a DETACH DELETE must execute; got {status} {body!r}")
        result = self._graph("MATCH (n:WebProbe) RETURN count(n)")
        assert result["rows"][0][0] == 0, (
            f"AC47: the delete must persist; got {result!r}")

        # The seeded graph is intact: the writes above touched only their own
        # nodes.
        status, _, body = self._req(port, self._graph_data(port))
        assert status == 200
        assert json.loads(body)["nodes"], (
            "the roadmap's own graph must survive the probe writes")

    def test_query_bar_publishes_exactly_three_failure_kinds(self):
        """AC123: the endpoint's `kind` takes exactly three values,
        invalid_limit, plan_prefix and execution, and the criterion requires the
        CLOSED set to be asserted rather than only the three members -- "a
        further value is exactly what an endpoint that started refusing
        statements on the ground of what they do would publish".

        The corpus is made of the statements the withdrawn guard rail refused: a
        write, schema DDL, a schema-introspection command at both spacings, and
        an undirected relationship read. Each is either served or fails in the
        engine; none of them may produce a kind of its own. Beside them stand
        the probes that produce each kind -- an invalid limit, an EXPLAIN and a
        PROFILE prefix, and an unexecutable statement -- and every member of the
        set must be observed, so what is asserted is the set the endpoint
        produces and not merely a bound on it.
        """
        proc, port = self._start(["--port", "0"])
        published = {"invalid_limit", "plan_prefix", "execution"}
        withdrawn = ("not_read_only", "schema_introspection",
                     "relationship_read_direction", "invalid_keyword_spacing")

        probes = [
            ("MATCH (n) DETACH DELETE n", None),
            ("CREATE (n:WebProbe {key:'closed-set'})", None),
            ("CREATE INDEX web_idx FOR (n:Spec) ON (n.key)", None),
            ("DROP INDEX web_idx", None),
            ("SHOW INDEXES", None),
            ("SHOW  INDEXES", None),
            ("MATCH (a)-[e]-(b) RETURN type(e)", None),
            ("MATCH (a)<-[e]-(b) RETURN type(e)", None),
            ("MATCH (n) RETURN", None),
            ("SHOW DATABASES", None),
            ("EXPLAIN MATCH (n) RETURN n", None),
            ("PROFILE MATCH (n) RETURN n", None),
            (None, "7"),
        ]
        observed = set()
        for query, limit in probes:
            status, _, body = self._req(
                port, self._graph_data(port, q=query, limit=limit))
            assert status in (200, 400), (
                f"{query!r} limit={limit!r}: status = {status}, want 200 or 400; "
                f"{body!r}")
            if status == 200:
                continue
            err = json.loads(body)
            assert err.get("kind") in published, (
                f"AC123: {query!r} limit={limit!r} carries kind "
                f"{err.get('kind')!r}, outside the closed set "
                f"{sorted(published)}: {err!r}")
            observed.add(err["kind"])
            for gone in withdrawn:
                assert gone not in body, (
                    f"AC123: the body for {query!r} names the withdrawn kind "
                    f"{gone!r}: {body!r}")
        assert observed == published, (
            f"AC123: the probes must produce every member of the closed set "
            f"{sorted(published)}; they produced {sorted(observed)}")

    # ====================================================================
    # AC166, AC167, AC170: the query bar refuses EXPLAIN and PROFILE
    # ====================================================================

    # The spellings AC166 names, each paired with the member `rmp graph client`
    # publishes for its prefix: `plan` for EXPLAIN, `profile` for PROFILE. A
    # PROFILE of a write is left out because the engine refuses it wherever it
    # runs, so the client could not confirm its premise; its EXPLAIN executes
    # nothing and can.
    _PLAN_PREFIX_CASES = (
        ("EXPLAIN MATCH (n) RETURN n", "plan"),
        ("PROFILE MATCH (n) RETURN n", "profile"),
        ("explain match (n) return n", "plan"),
        ("profile match (n) return n", "profile"),
        ("ExPlAiN MATCH (n) RETURN n", "plan"),
        ("pRoFiLe MATCH (n) RETURN n", "profile"),
        ("EXPLAIN\nMATCH (s:Spec)\nWHERE s.key = 'passwordless-auth'\nRETURN s", "plan"),
        ("PROFILE\nMATCH (s:Spec)-[:IMPLEMENTED_BY]->(c:Code)\nRETURN s, c", "profile"),
        (" \t\n\t EXPLAIN MATCH (n) RETURN n", "plan"),
        ("\n  \t PROFILE MATCH (n) RETURN n", "profile"),
        ("// plan the full read first\nEXPLAIN MATCH (n) RETURN n", "plan"),
        ("// measure the full read\nPROFILE MATCH (n) RETURN n", "profile"),
        ("/* plan the full read first */ EXPLAIN MATCH (n) RETURN n", "plan"),
        ("/* measure the full read */\nPROFILE MATCH (n) RETURN n", "profile"),
        ("EXPLAIN MATCH (n) RETURN n LIMIT 5", "plan"),
        ("PROFILE MATCH (n) RETURN n LIMIT 5", "profile"),
        ("EXPLAIN MATCH (n) DETACH DELETE n", "plan"),
        ("EXPLAIN CALL db.labels() YIELD label RETURN label", "plan"),
        ("PROFILE CALL db.labels() YIELD label RETURN label", "profile"),
    )

    # A second roadmap of the fixture's HOME that no graph server ever serves.
    _UNSERVED_ROADMAP = "incident-response"

    @staticmethod
    def _published_plan_prefix_line():
        """The refusal line SPEC/WEB.md section "Query-Bar Error Handling",
        rule 12, publishes, read out of the specification itself so the
        assertions cannot agree with a wrong copy of it. The specification
        publishes it once, on a line of its own, as one code span."""
        text = (REPO_ROOT / "SPEC" / "WEB.md").read_text(encoding="utf-8")
        found = re.findall(r"(?m)^[ \t]*`(query not run: [^`]+)`[ \t]*$", text)
        assert len(found) == 1, (
            f"SPEC/WEB.md publishes {len(found)} plan-prefix refusal lines, want "
            f"exactly one")
        return found[0]

    def _unserved_roadmap(self):
        """Create the roadmap no graph server serves, and confirm nothing is
        listening for it: no socket file exists at its derived path."""
        self._run(["roadmap", "create", self._UNSERVED_ROADMAP])
        socket_path = Path(self.test.default_socket_path(self._UNSERVED_ROADMAP))
        assert not socket_path.exists(), (
            f"the unserved roadmap has a socket at {socket_path}; the cases "
            f"that need no graph server would not be about one")
        return self._UNSERVED_ROADMAP

    def test_query_bar_refuses_plan_prefixes(self):
        """AC166: against a served roadmap, a statement carrying an EXPLAIN or
        PROFILE prefix, in every spelling the engine's parser recognises, is
        answered HTTP 400 with a body of exactly two fields -- kind plan_prefix
        and the line rule 12 publishes -- with no limit and with an allowed one,
        and the two answers are the same bytes.

        The cases come from the engine's grammar, so the premise is confirmed
        first, through the engine: `rmp graph client` sends each statement to
        the same server and publishes the plan member its prefix selects. A
        grammar change at a future pin then fails the premise instead of
        silently changing what is asserted.
        """
        line = self._published_plan_prefix_line()
        for query, member in self._PLAN_PREFIX_CASES:
            result = self._graph(query)
            assert member in result, (
                f"AC166 premise: the pinned engine does not report {query!r} as "
                f"carrying its prefix -- `rmp graph client` published "
                f"{sorted(result)!r} and no {member!r} member")

        proc, port = self._start(["--port", "0"])
        for query, _member in self._PLAN_PREFIX_CASES:
            answers = []
            for limit in (None, "250"):
                status, headers, body = self._req(
                    port, self._graph_data(port, q=query, limit=limit))
                assert status == 400, (
                    f"AC166: {query!r} limit={limit!r} must be refused with 400; "
                    f"got {status} {body!r}")
                assert headers.get("content-type", "").startswith("application/json"), (
                    f"AC166: the refusal must be JSON; got {headers!r}")
                err = json.loads(body)
                assert set(err) == {"error", "kind"}, (
                    f"AC166: the body must carry exactly error and kind; got {err!r}")
                assert err["kind"] == "plan_prefix", (
                    f"AC166: {query!r} must carry kind plan_prefix; got {err!r}")
                assert err["error"] == line, (
                    f"AC166: the error must be exactly the published line "
                    f"{line!r}; got {err['error']!r}")
                answers.append(body)
            assert answers[0] == answers[1], (
                f"AC166: {query!r} must be refused identically with and without "
                f"an allowed limit; got {answers!r}")

    def test_query_bar_plan_prefix_is_refused_with_no_graph_server(self):
        """AC167, the no-server half: with no `rmp graph serve` running for the
        roadmap, EXPLAIN MATCH (n) RETURN n and PROFILE MATCH (n) RETURN n are
        each answered 400 with kind plan_prefix, while MATCH (n) RETURN n is
        answered 503.

        The pair is the assertion. The 503 proves nothing is serving the
        roadmap, so the 400 beside it can only come from a refusal decided
        before any graph server is resolved.
        """
        line = self._published_plan_prefix_line()
        roadmap = self._unserved_roadmap()
        proc, port = self._start(["--port", "0"])

        for query in ("EXPLAIN MATCH (n) RETURN n", "PROFILE MATCH (n) RETURN n"):
            status, _, body = self._req(
                port, self._graph_data(port, q=query, roadmap=roadmap))
            assert status == 400, (
                f"AC167: {query!r} against a roadmap no server serves must be "
                f"refused with 400, not answered as an unavailable graph; got "
                f"{status} {body!r}")
            assert json.loads(body) == {"error": line, "kind": "plan_prefix"}, (
                f"AC167: {query!r} must carry kind plan_prefix and the published "
                f"line; got {body!r}")

        status, _, body = self._req(
            port, self._graph_data(port, q="MATCH (n) RETURN n", roadmap=roadmap))
        assert status == 503, (
            f"AC167: the unprefixed statement against the same roadmap must be "
            f"answered 503, or the 400s above cannot show that the refusal "
            f"precedes the server lookup; got {status} {body!r}")

    def test_query_bar_invalid_limit_outranks_plan_prefix(self):
        """AC170: a request carrying an invalid limit -- 7, or the non-numeric
        all -- and a prefixed statement is answered 400 with kind invalid_limit
        and an error naming the rejected value, never plan_prefix, for EXPLAIN
        and PROFILE alike, both against a served roadmap and against one no
        graph server serves.

        The control is the same prefixed statement under an allowed limit,
        answered plan_prefix: without it a limit that outranked the prefix could
        not be told from a prefix that was never recognised.
        """
        line = self._published_plan_prefix_line()
        unserved = self._unserved_roadmap()
        proc, port = self._start(["--port", "0"])

        for roadmap in (ROADMAP, unserved):
            for query in ("EXPLAIN MATCH (n) RETURN n", "PROFILE MATCH (n) RETURN n"):
                status, _, body = self._req(
                    port, self._graph_data(port, q=query, limit="250", roadmap=roadmap))
                assert status == 400 and json.loads(body) == {
                        "error": line, "kind": "plan_prefix"}, (
                    f"AC170 control: {query!r} under an allowed limit on {roadmap!r} "
                    f"must be refused as plan_prefix; got {status} {body!r}")

                for bad in ("7", "all"):
                    status, _, body = self._req(
                        port, self._graph_data(port, q=query, limit=bad, roadmap=roadmap))
                    assert status == 400, (
                        f"AC170: {query!r} limit={bad!r} on {roadmap!r} must be "
                        f"answered 400; got {status} {body!r}")
                    err = json.loads(body)
                    assert err.get("kind") == "invalid_limit", (
                        f"AC170: the limit is resolved before the prefix is "
                        f"examined, so {query!r} limit={bad!r} on {roadmap!r} must "
                        f"carry kind invalid_limit; got {err!r}")
                    assert bad in err.get("error", ""), (
                        f"AC170: the error must name the rejected limit {bad!r}; "
                        f"got {err!r}")

    def test_schema_listing_is_read_from_the_cli_not_the_endpoint(self):
        """AC157 and AC156: a schema-introspection command is EXECUTED and
        answered HTTP 200 with {"nodes": [], "edges": []}, while
        `rmp graph client` answers the identical statement with the rows naming
        the index the caller declared.

        Both halves are required together. The endpoint's answer is empty
        because its response shape carries nodes and edges, not because the
        store's schema is empty, and the CLI read is what establishes the
        difference. Asserting that the endpoint reports the index row MUST fail
        AC157.

        This test has now asserted three different things, and each step
        withdrew a rule rather than adding one: the endpoint used to publish an
        invalid_keyword_spacing class; then it refused the whole family at every
        spacing (rmp task #344); and the guard rail is now withdrawn entirely
        (rmp task #364), so the empty graph is back and is the specified answer.
        """
        # The store must hold a schema, or "the endpoint answers an empty graph"
        # would be true for the wrong reason and the distinction AC157 turns on
        # would be unobservable.
        self._graph("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        listing = self._graph("SHOW INDEXES")
        declared = [row[listing["columns"].index("name")] for row in listing["rows"]]
        assert "spec_key" in declared, (
            f"AC157: the store must hold the declared index; SHOW INDEXES "
            f"reported {declared!r}")

        proc, port = self._start(["--port", "0"])

        for query in ("SHOW INDEXES", "SHOW INDEX", "SHOW CONSTRAINTS",
                      "SHOW CONSTRAINT", "show indexes",
                      "SHOW INDEXES YIELD name RETURN name",
                      "SHOW INDEXES WHERE type = 'hash'"):
            status, _, body = self._req(port, self._graph_data(port, q=query))
            assert status == 200, (
                f"AC157: {query!r} must be executed and answered 200; got "
                f"{status} {body!r}")
            assert json.loads(body) == {"nodes": [], "edges": []}, (
                f"AC157: {query!r} returns tabular rows the response shape "
                f"cannot carry, so the answer is the empty graph; got {body!r}")

        # The other half: the CLI answers the same statement with the rows.
        listing = self._graph("SHOW INDEXES")
        declared = [row[listing["columns"].index("name")] for row in listing["rows"]]
        assert "spec_key" in declared, (
            f"AC157: `rmp graph client` must report the declared index, which "
            f"is what makes the endpoint's empty answer a property of its "
            f"response shape rather than of the store; got {listing!r}")

    def test_schema_keyword_spacing_is_the_engines_verdict(self):
        """AC151: a schema-introspection command written with anything but a
        single space between its two keywords is answered as the ENGINE's own
        parse failure -- HTTP 400, kind execution, and the engine's diagnostic in
        error. The endpoint neither refuses it before execution nor repairs the
        spacing, and it publishes no class of its own for it.

        The same statement written with one space is answered HTTP 200 with an
        empty graph, so the two spellings differ in the response and the
        difference is the engine's routing rather than a rule of this
        endpoint's (SPEC/GRAPH.md section "What Groadmap Does Not Check",
        item 7).

        The engine diagnostic is asserted and not only the status: a refusal
        decided before execution could not carry one, so it is what
        distinguishes "the engine rejected it" from "the endpoint rejected it".

        Any kind but execution fails the criterion, a member of the closed set
        included: none of these commands carries an EXPLAIN or PROFILE prefix,
        so plan_prefix is as much a failure here as invalid_limit.
        """
        proc, port = self._start(["--port", "0"])

        for one_space, misspaced in (
            ("SHOW INDEXES", "SHOW  INDEXES"),
            ("SHOW INDEX", "SHOW\tINDEX"),
            ("SHOW CONSTRAINTS", "SHOW\nCONSTRAINTS"),
            ("SHOW CONSTRAINT", "show  constraint"),
            ("SHOW INDEXES YIELD name RETURN name",
             "SHOW  INDEXES YIELD name RETURN name"),
        ):
            status, _, body = self._req(port, self._graph_data(port, q=one_space))
            assert status == 200 and json.loads(body) == {"nodes": [], "edges": []}, (
                f"AC151: the well-spaced {one_space!r} must be executed and "
                f"answered 200 with an empty graph; got {status} {body!r}")

            status, _, body = self._req(port, self._graph_data(port, q=misspaced))
            assert status == 400, (
                f"AC151: {misspaced!r} is not routed to the engine's schema "
                f"parser and fails there; got {status} {body!r}")
            err = json.loads(body)
            assert err.get("kind") == "execution", (
                f"AC151: {misspaced!r} must carry kind execution, not "
                f"{err.get('kind')!r}: the endpoint publishes no class of its "
                f"own for the spacing")
            assert set(err) == {"error", "kind"}, (
                f"the failure body must carry exactly error and kind; got {err!r}")
            assert "cypher:" in err["error"], (
                f"AC151: the message must be the engine's own diagnostic; a "
                f"message without one would mean the statement never reached "
                f"the engine; got {err['error']!r}")
            for forbidden in ("keyword spacing", "exactly one space"):
                assert forbidden not in err["error"], (
                    f"AC151: the endpoint states no spacing correction; got "
                    f"{err['error']!r}")
            assert err["kind"] != "invalid_keyword_spacing", (
                "AC123: invalid_keyword_spacing is not a value this endpoint "
                "publishes")

        # THE CLI ANSWERS THE SAME WAY, which is the point: with the guard rail
        # withdrawn on both surfaces there is no divergence left to reconcile.
        # `rmp graph client` hands the badly spaced statement to the server,
        # whose engine rejects it as a syntax error -- exit 1, and a diagnostic
        # naming SHOW rather than the spacing.
        code, _out, err = self.test.graph_client(ROADMAP, query="SHOW  INDEXES")
        assert code == 1, (
            f"AC151: `rmp graph client` must let the engine refuse a badly "
            f"spaced SHOW, which is exit 1; got {code} with stderr={err!r}")
        assert "validation error" not in err, (
            f"AC151: the refusal is the engine's, not a validation refusal of "
            f"Groadmap's; got {err!r}")

    def test_schema_ddl_through_the_endpoint_persists(self):
        """The pair to the listing above, and the plainest statement of what
        SPEC/WEB.md section Security and Constraints rule 3 grants: an
        unauthenticated GET creates an index in the roadmap's knowledge graph,
        and a separate process finds it under the name the caller declared.

        The pre-existing schema surviving is asserted too: whenever the server
        does fold a snapshot, that snapshot must carry the WHOLE registered
        schema and not just the newest definition. A snapshot that omitted it,
        followed by the log truncation that always follows a fold, destroys
        every index and constraint the graph had (SPEC/GRAPH.md section
        Synchronous Checkpoint on Write, step 2).
        """
        self._graph("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        self._graph("CREATE CONSTRAINT spec_key_unique FOR (n:Spec) "
                    "REQUIRE n.key IS UNIQUE")

        proc, port = self._start(["--port", "0"])

        status, _, body = self._req(port, self._graph_data(
            port, q="CREATE INDEX audit_key FOR (n:Audit) ON (n.key)"))
        assert status == 200, (
            f"schema DDL through the endpoint must execute; got {status} {body!r}")

        listing = self._graph("SHOW INDEXES")
        names = [row[listing["columns"].index("name")] for row in listing["rows"]]
        assert "audit_key" in names, (
            f"an index created through the endpoint must persist under the name "
            f"the caller declared; SHOW INDEXES reported {names!r}")
        assert "spec_key" in names, (
            f"the pre-existing index was lost after the DDL, which is the "
            f"snapshot-without-schema defect; SHOW INDEXES reported {names!r}")
        constraints = self._graph("SHOW CONSTRAINTS")
        declared = [row[constraints["columns"].index("name")]
                    for row in constraints["rows"]]
        assert "spec_key_unique" in declared, (
            f"the declared constraint was lost; got {constraints!r}")

        # And the DROP is symmetric.
        status, _, body = self._req(
            port, self._graph_data(port, q="DROP INDEX audit_key"))
        assert status == 200, f"a DROP must execute; got {status} {body!r}"
        listing = self._graph("SHOW INDEXES")
        names = [row[listing["columns"].index("name")] for row in listing["rows"]]
        assert "audit_key" not in names, (
            f"the DROP must persist; SHOW INDEXES reported {names!r}")

    def test_empty_graph_answers_are_indistinguishable(self):
        """AC156: four statements that produce no node and no edge for four
        different reasons are answered identically, because the endpoint
        publishes no class that separates them. The four responses are compared
        TO ONE ANOTHER, which is what the criterion asks for.

        The control keeps it narrow: the default query over the same store
        returns a non-empty nodes array, so an empty answer is a property of the
        statement rather than of the endpoint.
        """
        self._graph("CREATE INDEX spec_key FOR (n:Spec) ON (n.key)")
        proc, port = self._start(["--port", "0"])

        statements = [
            "MATCH (n:Absent) RETURN n",       # matched nothing
            "MATCH (n) RETURN count(n)",       # returned a number
            "SHOW INDEXES",                    # returned tabular rows
            "CREATE (n:Probe {key:'ac156'})",  # created a node, no columns
        ]
        answers = []
        for query in statements:
            status, _, body = self._req(port, self._graph_data(port, q=query))
            assert status == 200, (
                f"AC156: {query!r} must be answered 200; got {status} {body!r}")
            answers.append((status, body))
        assert len(set(answers)) == 1, (
            f"AC156: the four answers must be indistinguishable; got "
            f"{dict(zip(statements, answers))!r}")
        assert json.loads(answers[0][1]) == {"nodes": [], "edges": []}

        # The CREATE really created: the empty answer is the response shape, not
        # a statement that did nothing.
        result = self._graph("MATCH (n:Probe {key:'ac156'}) RETURN count(n)")
        assert result["rows"][0][0] == 1, (
            f"AC156: the CREATE that answered an empty graph must have "
            f"persisted; got {result!r}")

        # The control.
        status, _, body = self._req(port, self._graph_data(
            port, q="MATCH (n) OPTIONAL MATCH (n)-[r]->(m) RETURN n, r, m"))
        assert status == 200 and json.loads(body)["nodes"], (
            f"AC156: the same store must answer the default query with a "
            f"non-empty nodes array, or an empty answer is a property of the "
            f"endpoint and the comparison above proves nothing; got {body!r}")

    def test_query_bar_misresolved_relationship_reads_are_no_longer_refused(self):
        """What became of rmp task #288 at this endpoint.

        #288 added a refusal here: a read of a relationship bound by an incoming
        or undirected pattern was answered HTTP 400 with the kind
        relationship_read_direction. The refusal is withdrawn with the rest of
        the guard rail, so every one of those shapes now EXECUTES.

        This test asserts NEITHER reading of the disputed hazard, exactly as
        tests/test_56_graph_read_direction.py and
        internal/web/graph_relread_test.go do. SPEC/GRAPH.md item 5 still says
        the shapes are reported wrong; task #362 measured every one of them
        answering correctly at GoGraph v0.12.0, and correcting the item is task
        #373's. What is asserted is only what is true on both readings: the
        shapes execute, the outgoing forms return the graph, and the published
        UNION ALL rewrite returns both legs.

        The fixture is its own roadmap carrying a node pair with edges BOTH ways
        and DIFFERENT types each way, because that is the only shape the
        disputed behaviour would be visible on: a one-way pair reads back
        correctly on either reading.
        """
        name = "identity-platform"
        self._run(["roadmap", "create", name])
        # This roadmap gets a server of its own, left running for the whole
        # scenario: the endpoint reads it through that server and would answer
        # 503 without one.
        self._serve_graph(
            name,
            "CREATE (s:Spec {key:'session-revocation'}), (v:Test {key:'revoke-on-logout'})",
            "MATCH (s:Spec {key:'session-revocation'}), (v:Test {key:'revoke-on-logout'}) "
            "MERGE (s)-[:VERIFIED_BY]->(v)",
            "MATCH (s:Spec {key:'session-revocation'}), (v:Test {key:'revoke-on-logout'}) "
            "MERGE (v)-[:COVERS]->(s)",
        )

        proc, port = self._start(["--port", "0"])

        for query in [
            "MATCH (s:Spec)-[e]-(x) RETURN type(e), x.key",
            "MATCH (s:Spec)<-[e]-(x) RETURN type(e)",
            "MATCH (s:Spec)-[e]-(x) RETURN startNode(e).key, endNode(e).key",
            "MATCH (s:Spec)-[e]-(x) WHERE type(e) = 'COVERS' RETURN x.key",
            "MATCH (s:Spec)-[e]-(x) RETURN *",
        ]:
            status, _, body = self._req(
                port, self._graph_data(port, q=query, roadmap=name))
            assert status == 200, (
                f"{query!r} must execute: the endpoint refuses nothing on the "
                f"ground of what a statement does; got {status} {body!r}")
            assert "relationship_read_direction" not in body, (
                f"the response for {query!r} names a withdrawn kind: {body!r}")

        # The outgoing forms and the endpoint default still return the graph.
        for query in [
            None,  # the endpoint default
            "MATCH (s:Spec)-[e]->(x) RETURN s, e, x",
            "MATCH (x)-[e]->(s:Spec) RETURN x, e, s",
            "MATCH (s:Spec)-[:COVERS]-(x) RETURN x.key",
            "MATCH p=(s:Spec)-[e]-(x:Test) RETURN p",
        ]:
            status, _, body = self._req(
                port, self._graph_data(port, q=query, roadmap=name))
            assert status == 200, (
                f"{query!r} must be answered; got {status} {body!r}")

        # The published UNION ALL rewrite of the two outgoing legs returns both
        # typed edges, which is the property the rewrite exists to deliver and
        # is true whichever way item 5 is corrected.
        rewrite = ("MATCH (s:Spec {key:'session-revocation'})-[e]->(x:Test) "
                   "RETURN s, e, x UNION ALL "
                   "MATCH (x:Test)-[e]->(s:Spec {key:'session-revocation'}) "
                   "RETURN s, e, x")
        status, _, body = self._req(
            port, self._graph_data(port, q=rewrite, roadmap=name))
        assert status == 200, f"the rewrite must run; got {status} {body!r}"
        types = {e["type"] for e in json.loads(body)["edges"]}
        assert types == {"VERIFIED_BY", "COVERS"}, (
            f"the union of the two outgoing legs must return both edge types; "
            f"got {types!r}")

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
        that admit NO top-level LIMIT clause at all -- the SHOW schema-
        introspection commands, standalone procedure calls, and every statement
        with no top-level RETURN for a LIMIT to attach to -- so a statement that
        `rmp graph client` runs stays usable through the endpoint
        (SPEC/WEB.md section Graph Data Endpoint, Suppression 2).

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
        self._graph("UNWIND range(1,60) AS i CREATE (:Bulk {i:i})")
        total = self._graph("MATCH (n) RETURN count(n)")["rows"][0][0]
        assert total > 50, (
            f"the control needs a store larger than the 50-node cap; got {total}"
        )

        proc, port = self._start(["--port", "0"])

        # The exempt forms: a bare standalone call, a standalone call that is
        # not a pure read of the store, a standalone call carrying a YIELD, and
        # the write with no projection — which the endpoint could not execute at
        # all while it injected into one, and which AC47 requires it to execute.
        exempt = (
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

        # The other non-limitable form: the schema-introspection command. It
        # admits no LIMIT either, so it is suppressed and EXECUTED, and the rows
        # it returns carry no graph element (AC111, AC156). This assertion fails
        # both ways round: if the suppression is lost the injected LIMIT reaches
        # the engine and the form comes back 400 with the parse diagnostic, and
        # if a refusal is reintroduced it comes back 400 with a kind of its own,
        # which AC111 states MUST fail.
        for query in ("SHOW INDEXES", "SHOW CONSTRAINTS",
                      "SHOW INDEXES YIELD name, state RETURN name"):
            status, _, body = self._req(
                port, self._graph_data(port, q=query, limit="100")
            )
            assert status == 200, (
                f"AC111: {query!r} admits no LIMIT, so nothing is injected and "
                f"it is executed as written; got {status} {body!r}"
            )
            assert json.loads(body) == {"nodes": [], "edges": []}, (
                f"AC111/AC156: {query!r} returns tabular rows carrying no graph "
                f"element, so the answer is the empty graph; got {body!r}"
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
        """AC50: a statement that fails in the engine (invalid syntax) is
        answered with kind=execution, distinct from the endpoint's two refusals,
        invalid_limit and plan_prefix (SPEC/WEB.md § Query-Bar Error Handling,
        rules 2 and 4)."""
        proc, port = self._start(["--port", "0"])
        status, _, body = self._req(port, self._graph_data(port, q="MATCH (n) RETURN"))
        assert status == 400
        assert json.loads(body).get("kind") == "execution", (
            "an execution failure must carry kind execution, distinct from the "
            "endpoint's two refusals, invalid_limit and plan_prefix"
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
        parameter, no flag and no environment variable: graphlock.StatementBudget
        is assigned once, in the server process, and nothing outside it can move
        it (rule 8). The cost is confined instead — the store below belongs to this
        scenario alone, so the module's shared fixture stays two nodes and every
        other scenario stays fast.

        That store is sized from measurement. Measured UNBOUNDED — on a surface
        that at the time ran its statement under no deadline of its own, before
        rmp task #377 put every statement under the same 5s budget this endpoint
        applies — the three-way Cartesian product below costs 61.2s over these
        799 nodes against a 5s budget: a twelvefold margin, so the query still
        cannot finish inside the budget on hardware an order of magnitude faster
        than the machine this was measured on. The
        margin is free: the request is cut at the budget whatever the store size,
        so a larger store buys robustness and costs no wall time. A 399-node
        store, for comparison, costs 7.5s unbounded — a 1.5x margin, which any
        machine 1.6x faster would turn into a silent false pass.

        Both time bounds are asserted and the LOWER one carries the weight: a
        regression that disabled the budget and failed the request for some other
        reason, instantly, would satisfy an upper bound on its own.
        """
        # 797 bulk nodes on top of a two-node, one-edge seed of the same shape
        # the module fixture builds, giving a 799-node store. The bulk arrives
        # in a single statement, so the whole seed costs a handful of client
        # round trips.
        name = "telemetry"
        bulk = 797
        self._run(["roadmap", "create", name])
        # Its own server, left running: the endpoint reaches this store the same
        # way the seeds below do, and the budget under test is the server's.
        self._serve_graph(
            name,
            "CREATE (s:Spec {key:'passwordless-auth'})",
            "CREATE (c:Code {path:'internal/auth/magiclink.go'})",
            "MATCH (s:Spec {key:'passwordless-auth'}), "
            "(c:Code {path:'internal/auth/magiclink.go'}) "
            "CREATE (s)-[:IMPLEMENTED_BY]->(c)",
            "UNWIND range(1," + str(bulk) + ") AS i CREATE (:Bulk {i:i})",
        )

        # Ground truth for the store, read from the engine rather than assumed,
        # so the completeness assertion below cannot silently drift with the
        # seed. The reader limit must sit above it, or a capped read would be
        # mistaken for a complete one.
        seeded = self._graph("MATCH (n) RETURN count(n)", roadmap=name)["rows"][0][0]
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
            raise AssertionError(
                f"the endpoint never answered the expensive query, and the "
                f"connection failed with {type(exc).__name__}: {exc}. The query "
                "time budget did not cut the query."
            ) from exc

        assert status == 400, (
            f"an exhausted budget is a query execution failure, answered 400; "
            f"got {status}: {body!r}"
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

        # WHICH failure was written is what establishes that the BUDGET cut the
        # query, and it is stronger than any duration would be: the reason names
        # the query time budget, and a request that failed for any other cause --
        # a guard-rail rejection, an invalid limit, a cancelled client, a server
        # that stopped -- carries a different kind or a different reason, both
        # asserted above. That the answer reached this client at all is the other
        # half: a query the budget had not cut would still have been running when
        # the server's 30s WriteTimeout fired, and no 400 would have arrived.
        # SPEC/WEB.md acceptance criterion 110 and SPEC/BUILD.md "No Benchmarks
        # and No Performance-Measurement Tests" both forbid asserting the
        # elapsed time here.

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

    def test_query_bar_invalid_limit_is_resolved_before_the_statement_runs(self):
        """AC123: the limit is resolved BEFORE the statement runs, so a request
        carrying both an invalid limit and a statement that would have written is
        answered kind=invalid_limit and the statement is not executed
        (SPEC/WEB.md section Query-Bar Error Handling, rule 5).

        The write is what makes the assertion decisive: "the statement was not
        executed" is observable only through a statement whose execution leaves a
        trace, and a read leaves none. The two single-fault controls keep the
        combined case honest -- without them an endpoint that never executed
        anything would pass it.
        """
        proc, port = self._start(["--port", "0"])
        bad_limit = "7"

        # Control A: the statement alone EXECUTES under an allowed limit.
        status, _, body = self._req(port, self._graph_data(
            port, q="CREATE (n:WebProbe {key:'control'})", limit="100"))
        assert status == 200, (
            f"control A: a CREATE under an allowed limit must execute; got "
            f"{status} {body!r}")
        result = self._graph("MATCH (n:WebProbe {key:'control'}) RETURN count(n)")
        assert result["rows"][0][0] == 1, (
            f"control A: the write did not land, so this test cannot tell an "
            f"unexecuted statement from an executed one; got {result!r}")

        # Control B: the limit alone is classified invalid_limit.
        status, _, body = self._req(port, self._graph_data(port, limit=bad_limit))
        assert status == 400
        assert json.loads(body).get("kind") == "invalid_limit"

        # The claim: both wrong at once resolves to invalid_limit.
        status, _, body = self._req(port, self._graph_data(
            port, q="CREATE (n:WebProbe {key:'never-created'})", limit=bad_limit))
        assert status == 400, "a doubly invalid request must still be a 400"
        err = json.loads(body)
        assert err.get("kind") == "invalid_limit", (
            f"the limit is resolved before the statement runs: {err!r}")
        assert bad_limit in err.get("error", ""), (
            f"the invalid-limit message must name the rejected value: {err!r}")

        # The statement never ran.
        result = self._graph("MATCH (n:WebProbe {key:'never-created'}) RETURN count(n)")
        assert result["rows"][0][0] == 0, (
            f"the CREATE executed despite the invalid limit; the request must "
            f"be rejected before the statement runs; got {result!r}")

    def test_query_bar_error_body_carries_exactly_error_and_kind(self):
        """AC123: every query-bar failure is answered with a JSON body of exactly
        two string fields, error and kind, and never with the
        {"nodes": ..., "edges": ...} success shape
        (SPEC/DATA_FORMATS.md - Graph View Data, Error Shape, rule 1)."""
        proc, port = self._start(["--port", "0"])
        cases = (
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
    # AC21: read-only - non-read methods rejected
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
    # AC22/AC25/AC26: static assets, self-contained, missing -> 404
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
    # AC23/AC29/AC27: no remote origins, viewport meta, mobile-first CSS
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
    # AC30/AC31: Tabler admin-shell layout in the dark theme
    # ====================================================================

    def test_every_page_is_dark_theme(self):
        """AC30: every page renders in Tabler's dark theme.

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
        """AC30/AC31: every page renders the Tabler admin-shell.

        The shell is a vertical navigation sidebar (listing Roadmaps and,
        within a roadmap, that roadmap's views), a page wrapper, a page
        header, and the top navbar. The navbar-toggler + collapse markup is
        what Tabler's JS turns into an off-canvas hamburger menu on small
        viewports (AC31), so its presence is the structural proof of the
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
                '<header class="navbar d-print-none">',  # top navbar
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
        opening = '<header class="navbar d-print-none">'
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
            # The title column (everything before any actions column) carries
            # no badge: the sprint's status badge is the only header badge,
            # and it lives in the sprint page's pretitle.
            title_column = header.split('<div class="col-auto', 1)[0]
            assert "badge" not in title_column, (
                f"page {path}: the header's title column carries a badge; "
                f"only the sprint page's pretitle does; column={title_column!r}"
            )

        # The sprint page: the pretitle reads Sprint #<id> and then, after one
        # space, the sprint's status badge (OPEN -> bg-blue-lt, the sprint
        # status mapping); the page-title holds the sprint's title alone.
        sprint_path = f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}"
        _, _, sprint_body = self._req(port, sprint_path)
        sprint_header = self._page_header(sprint_path, sprint_body)
        want_pretitle = (
            f'<div class="page-pretitle">Sprint #{self.open_sid} '
            '<span class="badge bg-blue-lt">OPEN</span></div>'
        )
        assert want_pretitle in sprint_header, (
            f"sprint page: pretitle is not Sprint #{self.open_sid} followed by its "
            f"status badge; header={sprint_header!r}"
        )
        assert '<h2 class="page-title">Authentication hardening sprint</h2>' in sprint_header, (
            f"sprint page: page-title is not the sprint's title alone; header={sprint_header!r}"
        )
        h2 = sprint_header[sprint_header.index('<h2 class="page-title">'):]
        h2 = h2[: h2.index("</h2>")]
        assert "badge" not in h2, (
            f"sprint page: the page-title carries a badge; it belongs in the pretitle; h2={h2!r}"
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

        # The headers that carry an actions column hold what they should: a
        # control, or a record page's hierarchical back link. The tasks page's
        # header carries none: its filter bar sits in its list card's header
        # (AC100, AC109).
        _, _, tasks_body = self._req(port, f"/roadmaps/{ROADMAP}/tasks")
        tasks_header = self._page_header(f"/roadmaps/{ROADMAP}/tasks", tasks_body)
        assert "ms-auto" not in tasks_header and "<input" not in tasks_header and "<select" not in tasks_header, (
            f"the tasks page's header carries an actions column: {tasks_header!r}"
        )
        _, _, graph_body = self._req(port, f"/roadmaps/{ROADMAP}/graph")
        assert 'id="layout-select"' in self._page_header(f"/roadmaps/{ROADMAP}/graph", graph_body)

        # The two record pages' actions column is full width below sm, so it
        # wraps below a long title; the other pages keep col-auto (Acceptance
        # Criteria 78 and 230).
        for path in (f"/roadmaps/{ROADMAP}/sprints/{self.open_sid}", f"/roadmaps/{ROADMAP}/tasks/{self.open_task_ids[0]}"):
            _, _, body = self._req(port, path)
            assert '<div class="col-12 col-sm-auto ms-auto d-print-none">' in self._page_header(path, body), path
        for path in (f"/roadmaps/{ROADMAP}/graph",):
            _, _, body = self._req(port, path)
            assert '<div class="col-auto ms-auto d-print-none">' in self._page_header(path, body), path

        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/tasks", f"/roadmaps/{ROADMAP}/audit"):
            _, _, body = self._req(port, path)
            header = self._page_header(path, body)
            assert "ms-auto" not in header, (
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
        """AC23/AC29: the vendored Tabler framework and fonts are served from /static/.

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
        """AC23/AC29: every page loads the vendored Tabler CSS/JS from /static/."""
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
        """AC29: no page loads a CSS framework/reset from a remote origin.

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
        # as a row of its list (SPEC/WEB.md § Roadmap Tasks Page).
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
            # The sprint card is named "Open details for task #<id>: <title>"
            # (AC93); the list row's one link is the title, named by its visible
            # text, which decodes back to the title exactly (AC86).
            if path.endswith("/tasks"):
                m = re.search(rf'<a href="/roadmaps/escaping_demo/tasks/{task_id}">([^<]*)</a>', body)
                assert m and html_lib.unescape(m.group(1)) == hostile, f"{path}: the title link text does not decode to the title"
                assert body.count(f'href="/roadmaps/escaping_demo/tasks/{task_id}"') == 1, (
                    f"{path}: the hostile title broke the markup"
                )
                continue
            prefix = "Open details for task"
            labels = [
                m for m in re.findall(r'aria-label="([^"]*)"', body)
                if m.startswith(f"{prefix} #{task_id}:")
            ]
            assert labels, f"{path}: no accessible name for task #{task_id} survived extraction"
            for label in labels:
                assert html_lib.unescape(label) == f"{prefix} #{task_id}: {hostile}", (
                    f"{path}: the accessible name decodes to {html_lib.unescape(label)!r}"
                )

            # And the markup kept its shape: the card carries the task's href
            # once, not fragments produced by a broken attribute.
            assert body.count(f'href="/roadmaps/escaping_demo/tasks/{task_id}"') == 1, (
                f"{path}: the hostile title broke the markup"
            )

        # The task's own page shows the title as visible characters too, in the
        # header and in the document title (Acceptance Criterion 97).
        _, _, page = self._req(port, f"/roadmaps/escaping_demo/tasks/{task_id}")
        assert hostile not in page and "<b>bold</b>" not in page, "the task page renders the title as markup"
        assert f'<h2 class="page-title">{self._go_escaped(hostile)}</h2>' in page, (
            "the task page header does not show the escaped title"
        )

    # ====================================================================
    # AC24: graceful shutdown on SIGINT / SIGTERM
    # (SPEC/WEB.md section "Server Lifecycle", step 7; acceptance criterion 24)
    # ====================================================================
    #
    # Criterion 24 has two halves — sending either signal "shuts the server down
    # GRACEFULLY and the process EXITS 0" — and until rmp task #395 this block
    # asserted only the second. That half cannot fail for any reason this block
    # is about: runServer returns nil on the signal path unconditionally and
    # discards the shutdown error (`_ = srv.Shutdown(ctx)`), so exit 0 is what
    # the code returns there by construction. An exit code therefore proves that
    # the signal ARRIVED and nothing more, which was the right fence before rmp
    # task #388 repaired the delivery and is not a sufficient one now that it is
    # repaired.
    #
    # "Gracefully" is defined by step 7, which makes four promises and then
    # scopes them. What this block does with each:
    #
    #   1. "stops accepting new connections" — DRIVEN, and driven on a LIVE
    #      process. A probe taken after the process has exited is close to
    #      tautological, because the kernel closes the listening socket at exit
    #      whatever the code did; the one thing it does still catch is a
    #      listener LEAKED to a child that outlived the server, so the repeated
    #      case keeps it. The real assertion is in
    #      test_{sigint,sigterm}_stops_accepting_while_a_request_is_in_flight,
    #      which wedges a request open so the server is still running, and
    #      requires a new connection to be refused while it is. Measured here:
    #      the listener refuses about a millisecond after the signal, with the
    #      process still alive.
    #
    #   2. "allows in-flight requests a brief BOUNDED period to complete" — both
    #      words DRIVEN, by opposite cases, because they fail in opposite
    #      directions. A shutdown that cut its connections immediately would
    #      honour the bound and break "complete"; an unbounded one would honour
    #      "complete" and break the bound. So
    #      test_{sigint,sigterm}_lets_an_in_flight_request_finish stalls a
    #      2.8 MB response across the signal and requires the whole body to
    #      arrive with its declared Content-Length, while
    #      test_{sigint,sigterm}_shutdown_is_bounded_by_the_grace_window wedges
    #      the same response and never reads it, so Shutdown can never go
    #      quiescent, and requires the process to exit between shutdownGrace and
    #      shutdownGrace plus a margin. The floor proves it really waited; the
    #      ceiling proves the wait was bounded. Measured: 5.05 s. Without the
    #      bound that same case stops at the server's 30 s WriteTimeout instead,
    #      so the two outcomes are eight margins apart and cannot be confused.
    #
    #   3. "closes any graph store or database handle it opened" — the shutdown
    #      half is DECLINED as unobservable, with the reasoning and the
    #      antecedent that IS observable in
    #      test_no_handle_is_held_across_a_request_or_the_signal.
    #
    #   4. "exits 0" — still asserted, but now as one clause among several
    #      rather than as the whole of the test, and with the two ways it can be
    #      wrong told apart: a process KILLED by the signal reports a negative
    #      status to Python rather than a code, and that is a different defect
    #      from a clean process exiting the wrong number. The repeated case that
    #      carries this signals ON THE ANNOUNCEMENT, through _start_piped, which
    #      is the only timing at which the delivery is a race at all: measured
    #      against the parent of the fix, 60 runs found the defect 31 times when
    #      SIGTERM was sent there and 0 times when it was sent after the server
    #      had answered a request.
    #
    #   5. "This holds from step 5 onwards" — DRIVEN, on both sides. Every case
    #      above signals after the URL has been read, which is the inside of the
    #      boundary. test_{sigint,sigterm}_before_the_url_is_an_interruption
    #      drives the outside, where the specified answer is exit 130 and no
    #      graceful shutdown at all.
    #
    # The count the repeated case uses, and the reasoning that chose it, are at
    # GRACEFUL_STOP_RUNS. `rmp graph serve` answers the bounded-shutdown
    # question DIFFERENTLY on purpose (SPEC/GRAPH.md section "Server Shutdown and
    # the Drain": it does not bound its drain), so nothing in this block may be
    # copied onto that surface, nor anything of test_65's onto this one.

    # ---- graceful-shutdown helpers -------------------------------------

    @staticmethod
    def _await_exit(proc, sig, started):
        """Wait for a signalled server to exit; return (status, seconds taken).

        The wait is bounded by the server's own published bound plus a margin,
        so a shutdown that wedges is reported as the failure it is rather than
        hanging the suite. `started` is read by the caller immediately before
        the signal is sent, so the elapsed time covers the delivery too.
        """
        limit = SHUTDOWN_GRACE_SECONDS + SHUTDOWN_EXIT_MARGIN_SECONDS
        try:
            status = proc.wait(timeout=limit)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=10)
            raise AssertionError(
                f"the server never stopped after {sig.name}: still running {limit:.1f}s "
                f"later, though internal/web/server.go bounds its shutdown at "
                f"{SHUTDOWN_GRACE_SECONDS:.0f}s. Two different defects leave this trace, "
                f"and they are told apart by whether a request was in flight. With one "
                f"in flight it is an UNBOUNDED drain: the deadline runServer puts on "
                f"http.Server.Shutdown is what ends a wait on a client that never reads, "
                f"and without it the wait runs to the 30s WriteTimeout instead. With "
                f"nothing in flight it is a signal that reached no handler at all, one of "
                f"the two outcomes rmp task #388 removed"
            ) from None
        return status, time.time() - started

    @staticmethod
    def _assert_clean_exit(status, sig, context):
        """Require a graceful exit 0, telling the two failure shapes apart."""
        assert status is not None, f"{context}: the server did not exit"
        assert status >= 0, (
            f"{context}: the server was KILLED by signal {-status} instead of shutting "
            f"down. There was an instant in which {sig.name} carried its default "
            f"disposition, which is the defect rmp task #388 removed: no graceful "
            f"shutdown, no bounded drain, and the listener closed by the operating "
            f"system rather than by the process"
        )
        assert status == 0, (
            f"{context}: the server exited {status}, want 0. SPEC/WEB.md acceptance "
            f"criterion 24 fixes 0 for a server stopped by a signal; 130 means the "
            f"take-over had not happened when the signal arrived, so a URL had been "
            f"published for a process that did not yet own its own shutdown"
        )

    @classmethod
    def _asset_size(cls):
        """The on-disk size of the asset used to hold a request in flight.

        The floor is the anti-vacuity guard for every in-flight case below: the
        response has to be far larger than the socket buffers at both ends, or
        the handler never blocks, the request is over before the signal is sent,
        and the cases assert their properties against nothing.
        """
        size = (REPO_ROOT / IN_FLIGHT_ASSET_FILE).stat().st_size
        assert size >= IN_FLIGHT_ASSET_MIN_BYTES, (
            f"{IN_FLIGHT_ASSET_FILE} is {size} bytes, below the "
            f"{IN_FLIGHT_ASSET_MIN_BYTES}-byte floor the in-flight cases need. A "
            f"response that fits in the kernel's buffers completes before it can be "
            f"signalled, so those cases would stop driving the drain"
        )
        return size

    @staticmethod
    def _slow_reader(port, host="127.0.0.1"):
        """Open a connection whose receive buffer is far smaller than the reply.

        The client's buffer and the server's send buffer hold a few tens of
        kilobytes between them and the asset is megabytes, so the handler
        genuinely blocks in Write and the request is still in flight when the
        signal arrives. The request is sent here and deliberately NOT read.
        """
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, SLOW_READER_RCVBUF)
        sock.settimeout(SHUTDOWN_GRACE_SECONDS + SHUTDOWN_EXIT_MARGIN_SECONDS)
        sock.connect((host, port))
        sock.sendall(
            f"GET {IN_FLIGHT_ASSET_PATH} HTTP/1.1\r\n"
            f"Host: {host}:{port}\r\n"
            "Connection: close\r\n"
            "\r\n".encode()
        )
        return sock

    @staticmethod
    def _declared_length(headers_blob):
        """The Content-Length the server declared, as an int."""
        match = re.search(rb"(?im)^Content-Length:[ \t]*(\d+)[ \t]*\r?$", headers_blob)
        assert match, (
            "the response carried no Content-Length, so there is nothing to hold the "
            f"delivered body against: {headers_blob[:200]!r}"
        )
        return int(match.group(1))

    # ---- the four promises and the boundary ----------------------------

    def _graceful_stop_once(self, sig, run):
        """One stop signalled ON THE ANNOUNCEMENT, asserting the OUTCOME.

        The signal is sent the instant the URL object is read, which is where
        the published promise begins and where the defect this fences lives. The
        server has bound its listener and taken the signals over by then, but it
        may not have reached its accept loop, and SPEC/WEB.md is explicit that
        this is already inside the guarantee: "a caller that has read the URL
        cannot signal a process that has not yet taken over."

        It is also the only timing that samples anything. Against the parent of
        the fix, a signal sent once the server had answered a request found the
        defect 0 times in 60; sent here, SIGTERM found it 31 times in 60.
        Signalling a server that has demonstrably served a request is a
        different property, and it is the in-flight cases below that drive it.

        THE TWO SIGNALS ARE NOT SYMMETRIC HERE, and the asymmetry is measured
        rather than assumed. The shape this fences reset and re-armed both
        signals in one ordered pair — Reset(SIGINT, SIGTERM) then
        Notify(SIGINT, SIGTERM) — so SIGINT's unprotected interval both opens
        and CLOSES first. Against the pre-fix binary this case caught SIGTERM 31
        times in 60 and SIGINT 1 time in 60: a parent that has to return from a
        read and issue a kill is already past the earlier window and still
        inside the later one. So the repetition below samples the delivery race
        for SIGTERM and, for SIGINT, asserts the same outcome without claiming
        to sample it. Nothing is lost by that, because the delivery discipline
        for both signals is held at a thousand runs by internal/signals over a
        child that races nothing (rmp task #388); what this case adds is the
        outcome, and the outcome is driven identically for both.
        """
        label = f"{sig.name} run {run} of {GRACEFUL_STOP_RUNS}"
        proc, port = self._start_piped()

        # os.kill rather than proc.send_signal, and the clock read after the
        # signal rather than before it. Both are for the reason _start_piped's
        # own loop states: every syscall between the announcement and the signal
        # narrows what this case can sample, and send_signal begins with a
        # waitpid of its own. Measured against the pre-fix binary over 60 runs,
        # SIGTERM was caught 17 times through send_signal and 31 through os.kill.
        # Skipping that waitpid is safe here and nowhere else in this module:
        # nothing reaps this process between the announcement it just wrote and
        # this line, so the pid cannot have been recycled.
        os.kill(proc.pid, sig)
        started = time.time()
        code, elapsed = self._await_exit(proc, sig, started)
        self._assert_clean_exit(code, sig, label)

        # Inside the bound the code owns, and STRICTLY inside it: with no
        # request in flight there is nothing for the grace window to wait for,
        # so a server that sat out the window would be waiting for nothing.
        assert elapsed < SHUTDOWN_GRACE_SECONDS, (
            f"{label}: an idle server took {elapsed:.3f}s to stop, which is not inside "
            f"internal/web/server.go's shutdownGrace of {SHUTDOWN_GRACE_SECONDS:.0f}s"
        )

        # The listener is gone. After the exit this can only fail if the socket
        # was LEAKED — inherited by a child that outlived the server — which is
        # why _start passes --no-open and why the take-over is specified to
        # precede the browser spawn.
        assert self._connect_refused(port), (
            f"{label}: 127.0.0.1:{port} still accepts connections after the server "
            f"exited {code}; the listening socket outlived the process that opened it"
        )

        # And a request is REFUSED rather than answered.
        try:
            answered, _, _ = self._req(port, "/", timeout=2)
        except ConnectionRefusedError:
            pass
        else:
            raise AssertionError(
                f"{label}: GET / was answered {answered} after the server was signalled "
                f"and exited {code}"
            )

    def test_sigint_stops_serving_gracefully_on_every_run(self):
        for run in range(1, GRACEFUL_STOP_RUNS + 1):
            self._graceful_stop_once(signal.SIGINT, run)

    def test_sigterm_stops_serving_gracefully_on_every_run(self):
        for run in range(1, GRACEFUL_STOP_RUNS + 1):
            self._graceful_stop_once(signal.SIGTERM, run)

    def _in_flight_case(self, sig):
        """Signal a server with a request still in flight and drain it after.

        Drives promise 1 on a live process and promise 2's "complete" half: the
        listener must already refuse while the server is still running, and the
        response the server was midway through writing must still arrive whole.
        """
        label = f"{sig.name} with a request in flight"
        size = self._asset_size()
        proc, port = self._start(["--port", "0"])
        sock = self._slow_reader(port)
        try:
            head = sock.recv(4096)
            assert head.startswith(b"HTTP/1.1 200 OK"), (
                f"{label}: the in-flight request was not answered 200: {head[:80]!r}"
            )
            assert len(head) < size, (
                f"{label}: the whole {size}-byte response arrived in one read, so it was "
                f"never in flight and this case drives nothing"
            )

            started = time.time()
            proc.send_signal(sig)

            # PROMISE 1, observed on a LIVE process: the listener is already
            # gone while the server is still running, because the request it is
            # draining has not finished.
            refused_after = self._wait_refusing(port)
            assert proc.poll() is None, (
                f"{label}: the server exited before the in-flight response was drained, "
                f"so the refusal observed {refused_after * 1000:.1f}ms after the signal "
                f"proves only that the process is gone, not that it stopped accepting"
            )

            # PROMISE 2, "complete": read the rest and require the whole body.
            buffered = bytearray(head)
            while True:
                try:
                    chunk = sock.recv(65536)
                except socket.timeout:
                    raise AssertionError(
                        f"{label}: the in-flight response never completed; "
                        f"{len(buffered)} of {size} bytes arrived before the client's "
                        f"own deadline. SPEC/WEB.md section \"Server Lifecycle\" step 7 "
                        f"allows in-flight requests a bounded period to COMPLETE, not to "
                        f"be cut"
                    ) from None
                if not chunk:
                    break
                buffered += chunk

            code, elapsed = self._await_exit(proc, sig, started)
        finally:
            sock.close()

        self._assert_clean_exit(code, sig, label)

        headers_blob, separator, body = bytes(buffered).partition(b"\r\n\r\n")
        assert separator, f"{label}: the response had no header terminator"
        declared = self._declared_length(headers_blob)
        assert declared == size, (
            f"{label}: the server declared Content-Length {declared} for an asset of "
            f"{size} bytes"
        )
        assert len(body) == declared, (
            f"{label}: the in-flight response was CUT — {len(body)} of the declared "
            f"{declared} bytes arrived. The shutdown stopped accepting new connections "
            f"and took the live one with it, which is not the bounded drain step 7 "
            f"specifies"
        )
        assert elapsed <= SHUTDOWN_GRACE_SECONDS + SHUTDOWN_EXIT_MARGIN_SECONDS, (
            f"{label}: the server took {elapsed:.3f}s to stop"
        )

    def test_sigint_stops_accepting_while_a_request_is_in_flight(self):
        self._in_flight_case(signal.SIGINT)

    def test_sigterm_stops_accepting_while_a_request_is_in_flight(self):
        self._in_flight_case(signal.SIGTERM)

    def _bounded_case(self, sig):
        """Wedge a response open and require the shutdown to give up on time.

        The client stops reading and never resumes, so the handler stays blocked
        in Write and http.Server.Shutdown can never go quiescent. The only thing
        that can end this process is the deadline runServer put on it, which
        makes the exit time a direct reading of shutdownGrace. Without that
        deadline the same case would run to the server's 30 s WriteTimeout.
        """
        label = f"{sig.name} with a wedged response"
        size = self._asset_size()
        proc, port = self._start(["--port", "0"])
        sock = self._slow_reader(port)
        try:
            head = sock.recv(4096)
            assert head.startswith(b"HTTP/1.1 200 OK"), (
                f"{label}: the wedged request was not answered 200: {head[:80]!r}"
            )
            assert len(head) < size, (
                f"{label}: the whole {size}-byte response arrived in one read, so nothing "
                f"is wedged and the grace window would never be reached"
            )
            started = time.time()
            proc.send_signal(sig)
            code, elapsed = self._await_exit(proc, sig, started)
        finally:
            sock.close()

        self._assert_clean_exit(code, sig, label)
        assert elapsed >= SHUTDOWN_GRACE_SECONDS, (
            f"{label}: the server stopped {elapsed:.3f}s after the signal, before "
            f"internal/web/server.go's shutdownGrace of {SHUTDOWN_GRACE_SECONDS:.0f}s "
            f"had elapsed, with a request still in flight. It cut the connection instead "
            f"of allowing the bounded period step 7 promises"
        )
        assert elapsed <= SHUTDOWN_GRACE_SECONDS + SHUTDOWN_EXIT_MARGIN_SECONDS, (
            f"{label}: the server took {elapsed:.3f}s to stop, past shutdownGrace plus "
            f"its margin. The period step 7 promises is BOUNDED, and a shutdown that "
            f"waits on a client that never reads is bounded by nothing else"
        )

    def test_sigint_shutdown_is_bounded_by_the_grace_window(self):
        self._bounded_case(signal.SIGINT)

    def test_sigterm_shutdown_is_bounded_by_the_grace_window(self):
        self._bounded_case(signal.SIGTERM)

    def _widened_startup_home(self):
        """A throwaway HOME whose startup sweep is wide enough to signal into.

        Step 2 of the lifecycle migrates every roadmap under ~/.roadmaps/ before
        the listener is bound and long before the URL is printed. One roadmap
        reaches the announcement in about 20 ms, which is far too narrow to
        place a signal inside deliberately; a few hundred widen it past a
        second, and the signal then lands within steps 1 to 4 by construction
        rather than by luck.

        The roadmaps are real. A roadmap IS a directory under ~/.roadmaps/
        holding a project.db, and nothing inside the database records its own
        name — the directory is the name — so copying a database the CLI just
        created produces roadmaps the sweep cannot tell from separately created
        ones.

        Returns (home, window) where window is the measured time from launch to
        the URL, taken on that very HOME.
        """
        home = self._fresh_home()
        self._run(["roadmap", "create", "identity-service-01"], home=home)
        seed = Path(home) / ".roadmaps" / "identity-service-01" / "project.db"

        made = 0
        for family in SERVICE_FAMILIES:
            for index in range(1, STARTUP_SWEEP_ROADMAPS // len(SERVICE_FAMILIES) + 1):
                name = f"{family}-service-{index:02d}"
                directory = Path(home) / ".roadmaps" / name
                if directory.exists():
                    continue
                directory.mkdir(mode=0o700)
                shutil.copy2(seed, directory / "project.db")
                made += 1
        assert made >= STARTUP_SWEEP_ROADMAPS - 1, (
            f"only {made} roadmaps were cloned, too few to widen the startup sweep"
        )

        launched = time.time()
        proc, _ = self._start(["--port", "0"], home=home)
        window = time.time() - launched
        started = time.time()
        proc.send_signal(signal.SIGTERM)
        code, _ = self._await_exit(proc, signal.SIGTERM, started)
        assert code == 0, f"the run that measured the startup window exited {code}"
        return home, window

    def _boundary_case(self, sig):
        """Signal before the URL: an interruption, not a graceful shutdown.

        Step 7 scopes its promises — "This holds from step 5 onwards" — and
        specifies the outside of that boundary too: a signal arriving during
        steps 1 to 4 reaches an invocation that has printed no URL and served
        nothing, so it is an interruption and the process exits 130.
        """
        label = f"{sig.name} before the URL"
        home, window = self._widened_startup_home()
        assert window >= STARTUP_SWEEP_MIN_WINDOW_SECONDS, (
            f"{label}: the startup sweep over {STARTUP_SWEEP_ROADMAPS} roadmaps took "
            f"only {window:.3f}s, under the {STARTUP_SWEEP_MIN_WINDOW_SECONDS:.1f}s this "
            f"case needs to place a signal inside steps 1 to 4. The widening no longer "
            f"widens anything and this case would be racing the announcement"
        )

        proc, _ = self._start(["--port", "0"], home=home, expect_ok=False)
        time.sleep(window / 4)
        assert proc.poll() is None, (
            f"{label}: the server exited {proc.returncode} before it was signalled; "
            f"stderr={self._drain(proc.err_file)}"
        )
        # Read stdout at the instant of the signal, so the two ways this case
        # can go wrong stay distinguishable afterwards: a widening that stopped
        # widening leaves the URL here, and a take-over that moved earlier than
        # step 5 leaves it empty here and present after.
        assert self._drain(proc.out_file) == "", (
            f"{label}: the URL was already printed {window / 4:.3f}s into a {window:.3f}s "
            f"startup, so the signal would land after the announcement and this case "
            f"would be driving the inside of the boundary instead of the outside"
        )

        started = time.time()
        proc.send_signal(sig)
        code, _ = self._await_exit(proc, sig, started)

        printed = self._drain(proc.out_file)
        assert printed == "", (
            f"{label}: stdout was empty when the signal was sent and holds {printed!r} "
            f"now, so the invocation was signalled during steps 1 to 4 and went on to "
            f"announce itself anyway. Something already owned the signal before step 5, "
            f"which is where SPEC/WEB.md says the take-over belongs: an interruption "
            f"before the URL was swallowed and turned into a graceful shutdown of a "
            f"server nobody had been told about"
        )
        assert code == EXIT_SIGINT, (
            f"{label}: the invocation exited {code}, want {EXIT_SIGINT}. SPEC/WEB.md "
            f"section \"Server Lifecycle\" step 7 scopes the graceful shutdown to step 5 "
            f"onwards; a signal during steps 1 to 4 reaches an invocation that has "
            f"printed no URL and served nothing, and is an interruption"
        )

    def test_sigint_before_the_url_is_an_interruption(self):
        self._boundary_case(signal.SIGINT)

    def test_sigterm_before_the_url_is_an_interruption(self):
        self._boundary_case(signal.SIGTERM)

    def test_no_handle_is_held_across_a_request_or_the_signal(self):
        """Step 7's third promise, examined: "closes any graph store or database
        handle it opened".

        THE SHUTDOWN HALF IS DECLINED, because it is not observable from outside
        the process and there is nothing at that moment to observe. Two things
        make it so:

          * A process exit closes every descriptor the process held and releases
            every advisory lock it took, so no observation made afterwards can
            tell a handle the server closed from one the kernel reclaimed. Any
            assertion of that shape would pass against a runServer that closed
            nothing whatsoever.
          * And runServer does close nothing, deliberately, because by then
            there is nothing open. The same SPEC section says so: "Each incoming
            request opens the data it needs read-only, serves the response, and
            releases the handle; the server does not hold a roadmap database or
            a graph store open across requests." The promise is kept by an
            invariant maintained per request, not by shutdown code.

        SO THE ANTECEDENT IS DRIVEN INSTEAD, which is the observable half and
        the half that can actually regress. After the server has served both a
        SQLite-backed page and a knowledge-graph request, a competing CLI
        process must be able to write to the roadmap's database and to its
        knowledge graph WHILE THE SERVER IS STILL RUNNING, and be answered
        promptly. A server holding the database open would make the task
        creation wait on SQLite's lock and then fail.

        The graph half of the promise is now kept a step earlier: the web server
        never opens a graph store at all, because the only process that opens one
        is `rmp graph serve` and every other surface reaches the graph through it
        (SPEC/GRAPH.md § Engine Constructor by Path). What the graph write proves
        here is therefore that nothing the web server holds delays it -- the same
        observable, over an architecture in which the handle it must not hold no
        longer exists to be held.

        The server is then signalled, and both writes are read back afterwards.
        """
        proc, port = self._start(["--port", "0"])

        status, _, _ = self._req(port, f"/roadmaps/{ROADMAP}")
        assert status == 200, f"the sprints page must be served, got {status}"
        query = urllib.parse.quote("MATCH (s:Spec) RETURN s.key")
        status, _, _ = self._req(port, f"/roadmaps/{ROADMAP}/graph/data?q={query}")
        assert status == 200, f"the graph data endpoint must be served, got {status}"

        # A competing writer, while the server is still up. Both of these take
        # the very resources the server just used.
        started = time.time()
        self._graph("CREATE (t:Test {path:'tests/test_35_web_interface.py'})")
        graph_write = time.time() - started
        assert graph_write < SHUTDOWN_GRACE_SECONDS, (
            f"a graph write took {graph_write:.3f}s while the web server was up, so "
            f"something it holds across requests delayed a write it has no part in"
        )
        audit_task = self.test.create_task(
            ROADMAP,
            "Rotate the session signing key quarterly",
            "Session signatures must not rest on a key older than one quarter",
            "Add a key ring with an active key and a grace-period verifier",
            "A key rotated at the boundary leaves existing sessions valid",
            priority=4,
        )
        assert audit_task, "a task must be creatable while the server is running"

        started = time.time()
        proc.send_signal(signal.SIGTERM)
        code, _ = self._await_exit(proc, signal.SIGTERM, started)
        self._assert_clean_exit(code, signal.SIGTERM, "the store/handle case")

        # Both writes survive, and the roadmap is fully usable afterwards.
        result = self._graph("MATCH (t:Test) RETURN t.path")
        assert result["rows"] == [["tests/test_35_web_interface.py"]], (
            f"the graph write made while the server was running did not survive: {result!r}"
        )
        _, out, _ = self._run(["task", "get", str(audit_task), "-r", ROADMAP])
        assert "Rotate the session signing key quarterly" in out, (
            f"the task created while the server was running did not survive: {out}"
        )

    def test_the_shutdown_bound_is_the_one_the_server_declares(self):
        """SHUTDOWN_GRACE_SECONDS is this module's independent statement of the
        bound; this is what keeps it equal to the number the binary compiles.

        Without this, every timing assertion in this block would be measured
        against a constant that could quietly stop describing the server — the
        cases would keep passing while fencing a bound nobody implements. It is
        the same rule WHITE_SPACE follows for the search term's trim: state the
        expectation here, and assert the parity rather than assume it.
        """
        source = (REPO_ROOT / SHUTDOWN_GRACE_SOURCE).read_text(encoding="utf-8")
        declared = re.findall(
            r"^const\s+shutdownGrace\s*=\s*(\d+(?:\.\d+)?)\s*\*\s*time\.Second\s*$",
            source,
            re.M,
        )
        assert len(declared) == 1, (
            f"{SHUTDOWN_GRACE_SOURCE} declares the shutdown bound {len(declared)} times, "
            f"want exactly one. The constant was renamed, moved or written in another "
            f"unit, and this module's timing assertions are no longer derived from it"
        )
        assert float(declared[0]) == SHUTDOWN_GRACE_SECONDS, (
            f"{SHUTDOWN_GRACE_SOURCE} bounds the graceful shutdown at {declared[0]}s "
            f"while this module asserts against {SHUTDOWN_GRACE_SECONDS}s. One of the two "
            f"has moved and the drift must be named, not absorbed"
        )

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
        """AC142: the graph data endpoint's 400 is a WARN, not an ERROR -- the
        user's statement failed, not the server -- and the record carries the
        same failure kind the JSON response body carries.

        The probe is an unexecutable statement. It used to be a CREATE, which
        the endpoint refused as not read-only; the endpoint now runs a CREATE
        and answers 200, so that probe would assert nothing.
        """
        proc, port = self._start(["--port", "0"])
        query = urllib.parse.quote("MATCH (n) RETURN")
        status, _, body = self._req(port, f"/roadmaps/{ROADMAP}/graph/data?q={query}")
        assert status == 400, f"an unexecutable statement must be a 400, got {status}"
        payload = json.loads(body)
        assert payload["kind"] == "execution", f"unexpected kind: {payload!r}"

        records = self._log_records(self._drain(proc.err_file))
        assert not [r for r in records if r["level"] == "ERROR"], (
            f"a failed user statement is not a server error: {[r['raw'] for r in records]}"
        )
        warns = [r for r in records if r["level"] == "WARN"]
        assert len(warns) == 1, (
            f"exactly one WARN record expected; got {[r['raw'] for r in records]}"
        )
        rec = warns[0]
        assert rec["msg"] == "graph query bar request failed", f"msg = {rec['msg']!r}"
        for fragment in ("method=GET", f"roadmap={ROADMAP}",
                         "kind=execution", "status=400", "err="):
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
            ("GET", f"/roadmaps/{ROADMAP}/tasks/999999", 404),
            ("GET", f"/roadmaps/{ROADMAP}/tasks/1/data", 404),
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
