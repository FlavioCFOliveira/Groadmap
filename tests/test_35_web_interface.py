#!/usr/bin/env python3
"""
Test 35: rmp web - read-only embedded web interface (SPEC/WEB.md).

This suite drives the compiled binary's `rmp web` command end-to-end and
exercises every acceptance criterion AC1-AC22 of SPEC/WEB.md against the
running HTTP server:

- Process/CLI contract: flag validation and exit codes (AC1-AC5), the
  machine-readable {"url": ...} startup object, and graceful SIGINT/SIGTERM
  shutdown with exit 0 (AC17).
- Routes and pages: index with discovery + empty state (AC6-AC7), the
  read-only detail page (AC8) with no edit affordance and no audit-log
  growth (AC13), name validation / path-traversal guard (AC9), the
  knowledge-graph page (AC10) and its JSON data endpoint (AC11), and the
  read-only proof that graph reads create no snapshot/ directory (AC12).
- Read-only enforcement: non-read HTTP methods answered 405 (AC14).
- Self-contained delivery: static assets served only from /static/, a
  missing asset 404s (AC15), the vendored Cytoscape.js is served whole
  and locally, no page references any remote origin (AC16, AC18, AC19).
- Mobile-first: every page carries the responsive viewport meta tag and
  loads no remote CSS; the stylesheet uses min-width media queries (AC20-AC22).

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
CYTOSCAPE_BYTES = 373304  # vendored cytoscape@3.30.2 dist size


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
        """Build a realistic roadmap with tasks, a sprint and a knowledge graph."""
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
        sid = self.test.create_sprint(ROADMAP, "Authentication hardening sprint")
        self._run(["sprint", "add-tasks", "-r", ROADMAP, str(sid), str(t1), str(t2)])
        self.task_ids = (t1, t2)
        self.sprint_id = sid

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
        """
        args = [self.cli, "web", "--no-open"] + (extra_args or [])
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
                proc.send_signal(signal.SIGKILL)
                proc.wait(timeout=5)
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
    # AC1/AC2: startup URL object, loopback default
    # ====================================================================

    def test_startup_prints_url_object_and_serves(self):
        proc, port = self._start(["--port", "0"])
        # AC1: the URL reflects the actual loopback bind.
        status, _, _ = self._req(port, "/")
        assert status == 200
        # AC2: server is up without a browser (we passed --no-open); URL printed.
        assert port > 0

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
        assert f"/roadmaps/{ROADMAP}" in body, "index must link the detail page"
        assert f"/roadmaps/{ROADMAP}/graph" in body, "index must link the graph page"

    def test_index_empty_state_when_no_roadmaps(self):
        proc, port = self._start(["--port", "0"], home=self._fresh_home())
        status, _, body = self._req(port, "/")
        assert status == 200, "empty index must still be 200 (absence is not an error)"
        assert "roadmap create" in body.lower() or "no roadmap" in body.lower(), (
            "empty index must guide the user to create a roadmap via the CLI"
        )

    # ====================================================================
    # AC8/AC13: detail page is read-only and writes no audit entry
    # ====================================================================

    def test_detail_page_shows_tasks_and_sprints_read_only(self):
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")
        assert "passwordless login" in body.lower(), "detail must show task titles"
        assert "authentication hardening sprint" in body.lower(), "detail must show sprints"
        # No edit affordance: no form and no write-method submission.
        assert "<form" not in body.lower(), "detail page must contain no form"
        assert not re.search(r'method=["\']?(post|put|patch|delete)', body, re.I), (
            "detail page must not submit any change"
        )

    def test_serving_detail_writes_no_audit_entry(self):
        before = self._run(["audit", "stats", "-r", ROADMAP])[1]
        before_total = json.loads(before).get("total_entries")
        proc, port = self._start(["--port", "0"])
        for _ in range(4):
            assert self._req(port, f"/roadmaps/{ROADMAP}")[0] == 200
        after = self._run(["audit", "stats", "-r", ROADMAP])[1]
        after_total = json.loads(after).get("total_entries")
        assert before_total == after_total, (
            f"serving detail pages changed the audit log: {before_total} -> {after_total}"
        )

    # ====================================================================
    # AC9: name validation / path-traversal guard
    # ====================================================================

    def test_invalid_and_missing_names_return_404(self):
        proc, port = self._start(["--port", "0"])
        # Name violating ^[a-z0-9_-]+$ (uppercase) -> 404, never reaches FS.
        assert self._req(port, "/roadmaps/INVALID")[0] == 404
        # Encoded traversal attempt -> 404 (raw path, no client normalisation).
        assert self._req(port, "/roadmaps/..%2fetc")[0] == 404
        # Syntactically valid but non-existent roadmap -> 404.
        assert self._req(port, "/roadmaps/no_such_roadmap")[0] == 404
        assert self._req(port, "/roadmaps/no_such_roadmap/graph")[0] == 404
        assert self._req(port, "/roadmaps/no_such_roadmap/graph/data")[0] == 404

    # ====================================================================
    # AC10/AC11/AC12: graph page, data endpoint, read-only proof
    # ====================================================================

    def test_graph_page_loads_local_cytoscape(self):
        proc, port = self._start(["--port", "0"])
        status, headers, body = self._req(port, f"/roadmaps/{ROADMAP}/graph")
        assert status == 200
        assert headers.get("content-type", "").startswith("text/html")
        assert "/static/cytoscape.min.js" in body, "graph page must load vendored Cytoscape"
        # Cytoscape must not come from any remote origin.
        assert "cdn" not in body.lower() and "unpkg" not in body.lower()

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
        # The vendored Cytoscape bundle is served whole, locally.
        status, _, body = self._req(port, "/static/cytoscape.min.js")
        assert status == 200
        assert len(body.encode("utf-8")) == CYTOSCAPE_BYTES, (
            "the full vendored Cytoscape bundle must be served"
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
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/graph"):
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
        for path in ("/", f"/roadmaps/{ROADMAP}", f"/roadmaps/{ROADMAP}/graph"):
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

    def test_detail_text_is_html_escaped(self):
        # Output escaping (SPEC Security): roadmap-derived text cannot inject markup.
        self._run(["roadmap", "create", "escaping_demo"])
        self.test.create_task(
            "escaping_demo",
            "<script>alert(1)</script>",
            "why", "how", "verify",
        )
        proc, port = self._start(["--port", "0"])
        _, _, body = self._req(port, "/roadmaps/escaping_demo")
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
