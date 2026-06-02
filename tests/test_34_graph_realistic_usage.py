#!/usr/bin/env python3
"""
Test 34: rmp graph realistic-usage, data-consistency and operation-reliability.

This suite drives the knowledge-graph command family (SPEC/GRAPH.md) the
way a real AI agent would: it builds a project knowledge graph for a
"platform-modernization" roadmap through a long sequence of distinct
`rmp graph` invocations -- creating and maintaining both nodes and edges
-- while interleaving read queries that must stay consistent with every
preceding write.

Methodology (why this is a strong test, not just a smoke test):
- A pure-Python `GraphModel` mirrors every structural mutation
  (node/edge creation, edge deletion, node DETACH DELETE, revival). After
  each phase the suite queries the real graph and asserts node counts,
  per-label counts, and the total edge count match the model exactly.
  The assertions are therefore derived from the same operation list that
  produced them -- there are no hand-tuned magic numbers, so the test
  fails the moment the engine's observable state diverges from the
  operations issued.
- Every `rmp graph` call is a fresh OS process, so each read also
  exercises GoGraph recovery (snapshot + WAL tail). Consistency across
  calls is, by construction, cross-process durability.
- The main scenario issues well over 100 distinct invocations and asserts
  that count, satisfying the "realistic usage, >= 100 different calls"
  requirement explicitly.

Reliability properties covered by focused tests:
- MERGE idempotency (identity-only create never duplicates).
- Property value-type round-trip (string/int/float/bool, and null after
  REMOVE).
- Edge lifecycle: deleting an edge keeps its endpoints; DETACH DELETE
  removes a node together with all its incident edges.
- Durable node deletion across a store reopen (GoGraph v3.0.1): a deleted
  node does not resurrect as a label-stripped ghost, and re-creating its
  key revives it cleanly.
- The simple-directed-graph invariant (at most one edge per ordered
  endpoint pair).
- Guard-rail rejection matrix: each write subcommand accepts only its own
  operation class (exit code 6 otherwise).

All output shapes asserted here were confirmed against the compiled
binary: a write without RETURN emits {"ok": true}; a RETURN emits
{"columns", "rows"}; labels(n) is a list; ints/floats/bools round-trip as
their JSON types; a removed property reads back as null. Node ids are
non-deterministic and are never asserted.
"""

import os
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


EXIT_OK = 0
EXIT_NO_QUERY = 2
EXIT_NO_ROADMAP = 3
EXIT_NOT_FOUND = 4
EXIT_GUARD_RAIL = 6


def cypher_lit(value) -> str:
    """Render a Python value as a Cypher literal.

    bool is checked before int because bool is a subclass of int. Strings
    are single-quoted; the test data deliberately contains no apostrophes,
    which this function asserts so a careless data edit fails loudly rather
    than producing a malformed query.
    """
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return repr(value)
    text = str(value)
    assert "'" not in text, f"test data must not contain apostrophes: {text!r}"
    return "'" + text + "'"


class GraphModel:
    """The expected structural state of the graph, maintained in lockstep
    with the operations the test issues against the real store."""

    def __init__(self):
        self.nodes = {}          # key -> label
        self.edges = set()       # (src_key, dst_key, edge_type)

    def add_node(self, label: str, key: str):
        self.nodes[key] = label

    def add_edge(self, src: str, dst: str, etype: str):
        assert src in self.nodes, f"edge source {src!r} not modelled"
        assert dst in self.nodes, f"edge target {dst!r} not modelled"
        # The backing LPG is a SIMPLE directed graph: at most one edge per
        # ordered endpoint pair. Guard the model against accidentally
        # modelling a second edge on the same pair, which the store would
        # silently collapse.
        assert not any(e[0] == src and e[1] == dst for e in self.edges), (
            f"simple-graph violation in test data: duplicate ordered pair {src} -> {dst}")
        self.edges.add((src, dst, etype))

    def remove_edge(self, src: str, dst: str):
        self.edges = {e for e in self.edges if not (e[0] == src and e[1] == dst)}

    def remove_node(self, key: str):
        self.nodes.pop(key, None)
        self.edges = {e for e in self.edges if e[0] != key and e[1] != key}

    def label_of(self, key: str) -> str:
        return self.nodes[key]

    def total_nodes(self) -> int:
        return len(self.nodes)

    def total_edges(self) -> int:
        return len(self.edges)

    def count_label(self, label: str) -> int:
        return sum(1 for lbl in self.nodes.values() if lbl == label)

    def labels(self):
        return set(self.nodes.values())

    def successors(self, src: str, etype: str):
        return {e[1] for e in self.edges if e[0] == src and e[2] == etype}

    def depends_on_closure(self, src: str):
        """Transitive closure over DEPENDS_ON edges (for search assertions)."""
        seen = set()
        stack = [src]
        while stack:
            cur = stack.pop()
            for nxt in self.successors(cur, "DEPENDS_ON"):
                if nxt not in seen:
                    seen.add(nxt)
                    stack.append(nxt)
        return seen


class TestGraphRealisticUsage:

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()
        self.roadmap = self.test.create_roadmap("platform-modernization")
        self.model = GraphModel()
        self.calls = 0

    def teardown_method(self):
        self.test.teardown()

    # ---- low-level call wrappers (each counts exactly one invocation) ----

    def _write(self, subcmd: str, query: str):
        self.calls += 1
        result = self.test.run_cmd_json(["graph", subcmd, "-r", self.roadmap, "--query", query])
        assert result == {"ok": True}, (
            f"write without RETURN must emit {{'ok': true}}; {subcmd} {query!r} -> {result!r}")

    def _read(self, query: str, subcmd: str = "query"):
        self.calls += 1
        return self.test.run_cmd_json(["graph", subcmd, "-r", self.roadmap, "--query", query])

    def scalar(self, query: str, col: str, subcmd: str = "query"):
        result = self._read(query, subcmd=subcmd)
        idx = result["columns"].index(col)
        assert len(result["rows"]) == 1, f"expected exactly one row for {query!r}: {result!r}"
        return result["rows"][0][idx]

    def col_set(self, query: str, col: str, subcmd: str = "query"):
        result = self._read(query, subcmd=subcmd)
        idx = result["columns"].index(col)
        return {row[idx] for row in result["rows"]}

    def graph_dir(self) -> Path:
        return Path(self.test.roadmaps_dir) / self.roadmap / "graph"

    def wal_size(self):
        wal = self.graph_dir() / "wal"
        return wal.stat().st_size if wal.exists() else None

    # ---- modelled mutations (real store + model kept in lockstep) ----

    def create_node(self, label: str, key: str, **props):
        body = ", ".join(f"{k}: {cypher_lit(v)}" for k, v in {"key": key, **props}.items())
        self._write("create", f"CREATE (n:{label} {{{body}}})")
        self.model.add_node(label, key)

    def merge_node(self, label: str, key: str):
        self._write("create", f"MERGE (n:{label} {{key: {cypher_lit(key)}}})")
        self.model.add_node(label, key)

    def set_props(self, key: str, **props):
        label = self.model.label_of(key)
        body = ", ".join(f"n.{k} = {cypher_lit(v)}" for k, v in props.items())
        self._write("update", f"MATCH (n:{label} {{key: {cypher_lit(key)}}}) SET {body}")

    def create_edge(self, src: str, dst: str, etype: str, **props):
        slabel, dlabel = self.model.label_of(src), self.model.label_of(dst)
        if props:
            pbody = ", ".join(f"{k}: {cypher_lit(v)}" for k, v in props.items())
            rel = f"[:{etype} {{{pbody}}}]"
        else:
            rel = f"[:{etype}]"
        self._write(
            "create",
            f"MATCH (a:{slabel} {{key: {cypher_lit(src)}}}), (b:{dlabel} {{key: {cypher_lit(dst)}}}) "
            f"CREATE (a)-{rel}->(b)")
        self.model.add_edge(src, dst, etype)

    def delete_edge(self, src: str, dst: str, etype: str):
        slabel, dlabel = self.model.label_of(src), self.model.label_of(dst)
        self._write(
            "delete",
            f"MATCH (a:{slabel} {{key: {cypher_lit(src)}}})-[r:{etype}]->(b:{dlabel} {{key: {cypher_lit(dst)}}}) "
            f"DELETE r")
        self.model.remove_edge(src, dst)

    def detach_delete_node(self, key: str):
        label = self.model.label_of(key)
        self._write("delete", f"MATCH (n:{label} {{key: {cypher_lit(key)}}}) DETACH DELETE n")
        self.model.remove_node(key)

    # ---- consistency oracle ----

    def assert_consistent(self, note: str):
        total_nodes = self.scalar("MATCH (n) RETURN count(n) AS c", "c")
        assert total_nodes == self.model.total_nodes(), (
            f"[{note}] total nodes: store={total_nodes} model={self.model.total_nodes()}")
        total_edges = self.scalar("MATCH ()-[r]->() RETURN count(r) AS c", "c")
        assert total_edges == self.model.total_edges(), (
            f"[{note}] total edges: store={total_edges} model={self.model.total_edges()}")
        for label in sorted(self.model.labels()):
            count = self.scalar(f"MATCH (x:{label}) RETURN count(x) AS c", "c")
            assert count == self.model.count_label(label), (
                f"[{note}] {label} count: store={count} model={self.model.count_label(label)}")

    # ================================================================
    #  Main realistic scenario: > 100 distinct calls.
    # ================================================================

    def test_realistic_project_graph_session(self):
        # ---------------------------------------------------------------
        # PHASE 1 - People. Each person is a distinct node created once
        # with its attributes inline. (CREATE is used rather than MERGE
        # for distinct identities because MERGE's idempotency is exercised
        # separately in test_merge_is_idempotent.)
        # ---------------------------------------------------------------
        people = [
            ("ana.reis@corp.io", "Ana Reis", "Tech Lead"),
            ("bruno.matos@corp.io", "Bruno Matos", "Backend Engineer"),
            ("carla.nunes@corp.io", "Carla Nunes", "Frontend Engineer"),
            ("diogo.lima@corp.io", "Diogo Lima", "Site Reliability Engineer"),
            ("elsa.faria@corp.io", "Elsa Faria", "Product Manager"),
            ("filipe.gomes@corp.io", "Filipe Gomes", "Data Engineer"),
            ("gabriela.sousa@corp.io", "Gabriela Sousa", "Security Engineer"),
            ("hugo.pereira@corp.io", "Hugo Pereira", "Quality Engineer"),
        ]
        for email, name, role in people:
            self.create_node("Person", email, name=name, role=role, active=True)
        assert self.scalar("MATCH (p:Person) RETURN count(p) AS c", "c") == len(people)

        # ---------------------------------------------------------------
        # PHASE 2 - Specifications + ownership edges.
        # ---------------------------------------------------------------
        specs = [
            ("spec-auth", "User Authentication", "approved"),
            ("spec-billing", "Subscription Billing", "approved"),
            ("spec-search", "Full-text Search", "draft"),
            ("spec-notifications", "Notification Delivery", "approved"),
            ("spec-audit", "Audit Logging", "review"),
            ("spec-reporting", "Usage Reporting", "draft"),
        ]
        for key, title, status in specs:
            self.create_node("Spec", key, title=title, status=status)
        owners = {
            "spec-auth": "ana.reis@corp.io",
            "spec-billing": "elsa.faria@corp.io",
            "spec-search": "bruno.matos@corp.io",
            "spec-notifications": "carla.nunes@corp.io",
            "spec-audit": "gabriela.sousa@corp.io",
            "spec-reporting": "filipe.gomes@corp.io",
        }
        for spec_key, owner in owners.items():
            self.create_edge(owner, spec_key, "OWNS")
        self.assert_consistent("after specs + ownership")

        # ---------------------------------------------------------------
        # PHASE 3 - Tasks, each implementing a spec and assigned to a
        # person (round-robin). IMPLEMENTS carries a coverage property.
        # ---------------------------------------------------------------
        tasks = [
            ("task-jwt", "Implement JWT issuance", "spec-auth", 8, 0.4),
            ("task-oauth", "Add OAuth providers", "spec-auth", 13, 0.0),
            ("task-session", "Session store", "spec-auth", 5, 0.0),
            ("task-invoice", "Invoice generation", "spec-billing", 8, 0.6),
            ("task-payments", "Payment gateway integration", "spec-billing", 13, 0.1),
            ("task-dunning", "Dunning e-mails", "spec-billing", 5, 0.0),
            ("task-index", "Search indexing pipeline", "spec-search", 8, 0.5),
            ("task-parser", "Query parser", "spec-search", 5, 0.2),
            ("task-push", "Push notifications", "spec-notifications", 8, 0.3),
            ("task-email", "E-mail channel", "spec-notifications", 5, 0.7),
            ("task-audit-store", "Audit event store", "spec-audit", 8, 0.0),
            ("task-audit-api", "Audit query API", "spec-audit", 5, 0.0),
            ("task-report-agg", "Report aggregation", "spec-reporting", 13, 0.0),
            ("task-report-export", "Report export", "spec-reporting", 5, 0.0),
        ]
        engineers = [
            "bruno.matos@corp.io", "carla.nunes@corp.io", "diogo.lima@corp.io",
            "filipe.gomes@corp.io", "gabriela.sousa@corp.io", "hugo.pereira@corp.io",
        ]
        for i, (key, title, spec_key, points, coverage) in enumerate(tasks):
            self.create_node("Task", key, title=title, status="todo", points=points)
            self.create_edge(key, spec_key, "IMPLEMENTS", coverage=coverage)
            self.create_edge(engineers[i % len(engineers)], key, "ASSIGNED_TO", since=2025)
        self.assert_consistent("after tasks + implements + assignments")

        # Consistency of a relationship projection: the tasks that
        # implement spec-auth must match the model exactly.
        auth_tasks = self.col_set(
            "MATCH (t:Task)-[:IMPLEMENTS]->(s:Spec {key:'spec-auth'}) RETURN t.key AS k", "k")
        assert auth_tasks == {e[0] for e in self.model.edges
                              if e[1] == "spec-auth" and e[2] == "IMPLEMENTS"}, auth_tasks

        # Aggregate consistency: total story points across all tasks.
        total_points = self.scalar("MATCH (t:Task) RETURN sum(t.points) AS s", "s")
        assert total_points == sum(p for _, _, _, p, _ in tasks), total_points

        # Edge-property round-trip on a specific IMPLEMENTS edge.
        cov = self.scalar(
            "MATCH (:Task {key:'task-email'})-[r:IMPLEMENTS]->(:Spec) RETURN r.coverage AS c", "c")
        assert cov == 0.7, f"edge property lost on read-back: {cov!r}"

        # ---------------------------------------------------------------
        # PHASE 4 - Infrastructure: components, services, and their
        # dependency / hosting relationships (all distinct ordered pairs).
        # ---------------------------------------------------------------
        components = [
            "comp-web", "comp-api-gateway", "comp-auth-svc", "comp-billing-svc",
            "comp-search-svc", "comp-notify-svc", "comp-postgres", "comp-redis",
        ]
        for key in components:
            self.create_node("Component", key, layer="infrastructure")
        services = ["svc-cron", "svc-worker", "svc-webhook", "svc-gateway-edge", "svc-metrics"]
        for key in services:
            self.create_node("Service", key, layer="runtime")

        depends_on = [
            ("comp-web", "comp-api-gateway"),
            ("comp-api-gateway", "comp-auth-svc"),
            ("comp-api-gateway", "comp-billing-svc"),
            ("comp-api-gateway", "comp-search-svc"),
            ("comp-auth-svc", "comp-postgres"),
            ("comp-billing-svc", "comp-postgres"),
            ("comp-billing-svc", "comp-redis"),
            ("comp-search-svc", "comp-redis"),
            ("comp-notify-svc", "comp-redis"),
        ]
        for src, dst in depends_on:
            self.create_edge(src, dst, "DEPENDS_ON")
        runs_on = [
            ("svc-cron", "comp-postgres"),
            ("svc-worker", "comp-redis"),
            ("svc-webhook", "comp-notify-svc"),
            ("svc-gateway-edge", "comp-api-gateway"),
            ("svc-metrics", "comp-redis"),
        ]
        for src, dst in runs_on:
            self.create_edge(src, dst, "RUNS_ON")
        self.assert_consistent("after infrastructure")

        # ---------------------------------------------------------------
        # PHASE 5 - Decisions (incl. superseded ones) and risks.
        # ---------------------------------------------------------------
        decisions = [
            ("dec-jwt", "Adopt stateless JWT sessions"),
            ("dec-stripe", "Use Stripe for payments"),
            ("dec-elastic", "Use Elasticsearch for search"),
            ("dec-postgres", "Postgres as the primary store"),
            ("dec-redis", "Redis for caching and queues"),
            ("dec-sessions", "Server-side sessions (superseded)"),
            ("dec-mysql", "MySQL as primary store (superseded)"),
        ]
        for key, rationale in decisions:
            self.create_node("Decision", key, rationale=rationale)
        for src, dst in [
            ("dec-jwt", "spec-auth"),
            ("dec-stripe", "spec-billing"),
            ("dec-elastic", "spec-search"),
            ("dec-redis", "spec-notifications"),
        ]:
            self.create_edge(src, dst, "MOTIVATES")
        self.create_edge("dec-jwt", "dec-sessions", "SUPERSEDES")
        self.create_edge("dec-postgres", "dec-mysql", "SUPERSEDES")

        risks = [
            ("risk-vendor-lockin", "Vendor lock-in on payment provider"),
            ("risk-data-loss", "Primary store data loss"),
            ("risk-latency", "Cache-tier latency spikes"),
            ("risk-cost-overrun", "Observability cost overrun"),
        ]
        for key, description in risks:
            self.create_node("Risk", key, description=description)
        for src, dst in [
            ("risk-data-loss", "comp-postgres"),
            ("risk-latency", "comp-redis"),
            ("risk-vendor-lockin", "comp-billing-svc"),
            ("risk-cost-overrun", "svc-metrics"),
        ]:
            self.create_edge(src, dst, "THREATENS")
        self.assert_consistent("after decisions + risks")

        # ---------------------------------------------------------------
        # PHASE 6 - Maintenance: progress tasks, update spec statuses,
        # and exercise REMOVE.
        # ---------------------------------------------------------------
        done_tasks = ["task-jwt", "task-invoice", "task-index", "task-email"]
        for key in done_tasks:
            self.set_props(key, status="done", closed=True)
        assert self.scalar("MATCH (t:Task {status:'done'}) RETURN count(t) AS c", "c") == len(done_tasks)

        for spec_key in ["spec-search", "spec-reporting", "spec-audit"]:
            self.set_props(spec_key, status="approved")
        approved = self.scalar("MATCH (s:Spec {status:'approved'}) RETURN count(s) AS c", "c")
        assert approved == 6, f"all six specs should be approved after maintenance, got {approved}"

        # People records evolve too: a role change and a departure flag.
        self.set_props("ana.reis@corp.io", role="Engineering Manager")
        self.set_props("diogo.lima@corp.io", active=False)
        assert self.scalar(
            "MATCH (p:Person {key:'ana.reis@corp.io'}) RETURN p.role AS r", "r") == "Engineering Manager"
        assert self.scalar(
            "MATCH (p:Person {key:'diogo.lima@corp.io'}) RETURN p.active AS a", "a") is False

        # SET then REMOVE a transient property; it must read back as null.
        self.set_props("task-oauth", review_flag="needs-design")
        assert self.scalar(
            "MATCH (t:Task {key:'task-oauth'}) RETURN t.review_flag AS f", "f") == "needs-design"
        self.calls += 1
        self.test.run_cmd_json(
            ["graph", "update", "-r", self.roadmap, "--query",
             "MATCH (t:Task {key:'task-oauth'}) REMOVE t.review_flag"])
        assert self.scalar(
            "MATCH (t:Task {key:'task-oauth'}) RETURN t.review_flag AS f", "f") is None

        # ---------------------------------------------------------------
        # PHASE 7 - Structural maintenance: reassign, delete, revive.
        # ---------------------------------------------------------------
        # Reassign task-dunning to Hugo: drop the existing ASSIGNED_TO edge
        # (whoever holds it) and create the new one. Net edge count: 0.
        current_assignee = self.scalar(
            "MATCH (p:Person)-[:ASSIGNED_TO]->(t:Task {key:'task-dunning'}) RETURN p.key AS k", "k")
        self.delete_edge(current_assignee, "task-dunning", "ASSIGNED_TO")
        self.create_edge("hugo.pereira@corp.io", "task-dunning", "ASSIGNED_TO", since=2026)
        assert self.col_set(
            "MATCH (p:Person)-[:ASSIGNED_TO]->(t:Task {key:'task-dunning'}) RETURN p.key AS k", "k") \
            == {"hugo.pereira@corp.io"}
        self.assert_consistent("after reassignment")

        # Cancel a task: DETACH DELETE removes the node and its incident
        # IMPLEMENTS + ASSIGNED_TO edges in one operation.
        edges_before = self.scalar("MATCH ()-[r]->() RETURN count(r) AS c", "c")
        self.detach_delete_node("task-report-export")
        edges_after = self.scalar("MATCH ()-[r]->() RETURN count(r) AS c", "c")
        assert edges_after == edges_before - 2, (
            f"DETACH DELETE should drop the node's 2 incident edges: {edges_before} -> {edges_after}")
        self.assert_consistent("after cancelling task-report-export")

        # Retire a superseded decision; its SUPERSEDES edge goes with it.
        self.detach_delete_node("dec-mysql")
        assert self.scalar(
            "MATCH (:Decision)-[r:SUPERSEDES]->(:Decision) RETURN count(r) AS c", "c") == 1
        self.assert_consistent("after retiring dec-mysql")

        # Revive the cancelled task under the same key and re-link it.
        self.create_node("Task", "task-report-export", title="Report export", status="todo", points=5)
        self.create_edge("task-report-export", "spec-reporting", "IMPLEMENTS", coverage=0.0)
        self.create_edge("hugo.pereira@corp.io", "task-report-export", "ASSIGNED_TO", since=2026)
        self.assert_consistent("after reviving task-report-export")

        # ---------------------------------------------------------------
        # PHASE 8 - Traversal via `search`: transitive dependency closure
        # from the web component must match the model's DEPENDS_ON closure.
        # ---------------------------------------------------------------
        reachable = self.col_set(
            "MATCH (c:Component {key:'comp-web'})-[:DEPENDS_ON*1..4]->(x:Component) RETURN x.key AS k",
            "k", subcmd="search")
        assert reachable == self.model.depends_on_closure("comp-web"), (
            f"transitive DEPENDS_ON closure mismatch: store={sorted(reachable)} "
            f"model={sorted(self.model.depends_on_closure('comp-web'))}")

        # Direct dependencies of the API gateway.
        direct = self.col_set(
            "MATCH (:Component {key:'comp-api-gateway'})-[:DEPENDS_ON]->(x:Component) RETURN x.key AS k", "k")
        assert direct == self.model.successors("comp-api-gateway", "DEPENDS_ON"), direct

        # ---------------------------------------------------------------
        # PHASE 9 - Final invariants: full consistency, bounded WAL after
        # the long write session, and the >= 100 distinct-call requirement.
        # ---------------------------------------------------------------
        self.assert_consistent("final")
        assert self.wal_size() == 0, (
            f"after the last write the checkpoint must truncate the WAL to 0; got {self.wal_size()}")
        assert self.calls >= 100, (
            f"realistic session must issue >= 100 distinct graph calls; issued {self.calls}")

    # ================================================================
    #  Focused reliability tests.
    # ================================================================

    def test_merge_is_idempotent(self):
        for _ in range(5):
            self.merge_node("Person", "repeat@corp.io")
        assert self.scalar("MATCH (p:Person) RETURN count(p) AS c", "c") == 1, (
            "MERGE on a stable identity must never create duplicates")
        # Attributes applied via update must persist on the single node.
        self.set_props("repeat@corp.io", name="Repeat Tester", visits=3)
        assert self.scalar("MATCH (p:Person {key:'repeat@corp.io'}) RETURN p.visits AS v", "v") == 3

    def test_property_value_types_roundtrip(self):
        self.create_node("Sample", "types", count=42, ratio=0.125, enabled=True, label_text="edge-case")
        result = self._read(
            "MATCH (n:Sample {key:'types'}) "
            "RETURN n.count AS count, n.ratio AS ratio, n.enabled AS enabled, n.label_text AS text")
        row = {col: result["rows"][0][i] for i, col in enumerate(result["columns"])}
        assert row["count"] == 42 and isinstance(row["count"], int), row
        assert row["ratio"] == 0.125 and isinstance(row["ratio"], float), row
        assert row["enabled"] is True, row
        assert row["text"] == "edge-case", row
        # REMOVE makes the property read back as null without dropping the node.
        self._write("update", "MATCH (n:Sample {key:'types'}) REMOVE n.enabled")
        assert self.scalar("MATCH (n:Sample {key:'types'}) RETURN n.enabled AS e", "e") is None
        assert self.scalar("MATCH (n:Sample) RETURN count(n) AS c", "c") == 1

    def test_edge_deletion_keeps_endpoints(self):
        self.create_node("Service", "svc-a")
        self.create_node("Service", "svc-b")
        self.create_edge("svc-a", "svc-b", "CALLS")
        assert self.scalar("MATCH ()-[r:CALLS]->() RETURN count(r) AS c", "c") == 1
        self.delete_edge("svc-a", "svc-b", "CALLS")
        assert self.scalar("MATCH ()-[r:CALLS]->() RETURN count(r) AS c", "c") == 0, "edge not removed"
        assert self.scalar("MATCH (s:Service) RETURN count(s) AS c", "c") == 2, (
            "deleting an edge must not delete its endpoints")

    def test_detach_delete_removes_incident_edges(self):
        # Hub node with three incident edges (two out, one in).
        self.create_node("Component", "hub")
        for nb in ["leaf-1", "leaf-2", "leaf-3"]:
            self.create_node("Component", nb)
        self.create_edge("hub", "leaf-1", "DEPENDS_ON")
        self.create_edge("hub", "leaf-2", "DEPENDS_ON")
        self.create_edge("leaf-3", "hub", "DEPENDS_ON")
        assert self.scalar("MATCH ()-[r]->() RETURN count(r) AS c", "c") == 3
        self.detach_delete_node("hub")
        assert self.scalar("MATCH ()-[r]->() RETURN count(r) AS c", "c") == 0, (
            "DETACH DELETE must remove every edge incident to the node")
        assert self.scalar("MATCH (c:Component) RETURN count(c) AS c", "c") == 3, (
            "the three neighbours must survive")

    def test_deleted_node_is_durable_no_ghost(self):
        # Regression guard for the GoGraph v3.0.1 tombstone-durability fix:
        # a deleted node must not resurrect as a label-stripped ghost in a
        # later (fresh-process) invocation.
        self.create_node("Decision", "dec-temp", rationale="placeholder")
        assert self.scalar("MATCH (n) RETURN count(n) AS c", "c") == 1
        self.detach_delete_node("dec-temp")
        # The checkpoint after the delete truncated the WAL, so the next
        # read reconstructs purely from the snapshot's tombstone set.
        assert self.wal_size() == 0
        assert self.scalar("MATCH (n) RETURN count(n) AS c", "c") == 0, (
            "deleted node resurrected (tombstone not durable)")
        ghosts = self._read("MATCH (n) RETURN labels(n) AS l, n.key AS k")
        assert ghosts["rows"] == [], f"label-stripped ghost survived the delete: {ghosts!r}"
        # Re-creating the same key revives the node cleanly with its label.
        self.create_node("Decision", "dec-temp", rationale="revived")
        revived = self._read("MATCH (n:Decision {key:'dec-temp'}) RETURN labels(n) AS l, n.rationale AS r")
        assert revived["rows"][0][revived["columns"].index("l")] == ["Decision"], revived
        assert revived["rows"][0][revived["columns"].index("r")] == "revived", revived

    def test_simple_graph_at_most_one_edge_per_ordered_pair(self):
        # The backing LPG is a simple directed graph: an ordered endpoint
        # pair holds at most one edge. A second edge created on the same
        # pair does not accumulate a parallel relationship.
        self.create_node("Component", "left")
        self.create_node("Component", "right")
        self._write("create",
                    "MATCH (a:Component {key:'left'}),(b:Component {key:'right'}) CREATE (a)-[:USES]->(b)")
        self._write("create",
                    "MATCH (a:Component {key:'left'}),(b:Component {key:'right'}) MERGE (a)-[:CALLS]->(b)")
        assert self.scalar(
            "MATCH (:Component {key:'left'})-[r]->(:Component {key:'right'}) RETURN count(r) AS c", "c") == 1, (
            "a simple directed graph must keep at most one edge per ordered pair")

    def test_guardrail_blocks_mismatched_operations(self):
        self.create_node("Spec", "spec-x", status="draft")
        # create rejects mutating / deleting / read-only queries.
        for query in [
            "MATCH (n:Spec {key:'spec-x'}) SET n.status='done'",
            "MATCH (n:Spec {key:'spec-x'}) DETACH DELETE n",
            "MATCH (n:Spec) RETURN n.key",
        ]:
            self.calls += 1
            self.test.assert_exit_code(
                ["graph", "create", "-r", self.roadmap, "--query", query], EXIT_GUARD_RAIL)
        # update rejects creating / deleting / read-only queries.
        for query in [
            "CREATE (n:Spec {key:'spec-y'})",
            "MATCH (n:Spec {key:'spec-x'}) DELETE n",
            "MATCH (n:Spec) RETURN n.key",
        ]:
            self.calls += 1
            self.test.assert_exit_code(
                ["graph", "update", "-r", self.roadmap, "--query", query], EXIT_GUARD_RAIL)
        # delete rejects creating / mutating queries.
        for query in [
            "CREATE (n:Spec {key:'spec-z'})",
            "MATCH (n:Spec {key:'spec-x'}) SET n.status='done'",
        ]:
            self.calls += 1
            self.test.assert_exit_code(
                ["graph", "delete", "-r", self.roadmap, "--query", query], EXIT_GUARD_RAIL)
        # query and search reject any writing clause.
        for subcmd in ["query", "search"]:
            self.calls += 1
            self.test.assert_exit_code(
                ["graph", subcmd, "-r", self.roadmap, "--query", "CREATE (n:Spec {key:'spec-w'})"],
                EXIT_GUARD_RAIL)
        # The rejected writes must have changed nothing: still exactly one Spec.
        assert self.scalar("MATCH (s:Spec) RETURN count(s) AS c", "c") == 1, (
            "a guard-rail rejection must never mutate the graph")


def _run_all():
    instance_cls = TestGraphRealisticUsage
    method_names = [m for m in dir(instance_cls) if m.startswith("test_")]
    passed = 0
    failed = 0
    failures = []
    for m in method_names:
        instance = instance_cls()
        instance.setup_method()
        try:
            getattr(instance, m)()
            passed += 1
            calls = getattr(instance, "calls", 0)
            print(f"✓ {m} ({calls} graph calls)")
        except AssertionError as exc:
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            failures.append((m, exc))
            print(f"✗ {m} (error)")
        finally:
            instance.teardown_method()
    print("\n" + "=" * 60)
    print(f"Graph realistic-usage tests: {passed} passed, {failed} failed")
    print("=" * 60)
    for name, exc in failures:
        print(f"\n✗ {name}\n  {exc}")
    return failed == 0


if __name__ == "__main__":
    ok = _run_all()
    sys.exit(0 if ok else 1)
