#!/usr/bin/env python3
"""
Test 08: Complex Multi-Sprint Workflow
Tests realistic development scenarios with multiple sprints,
task carryover, rollback patterns, ordering, and state transitions.
"""

import sys
import os
import time
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.base_test import GroadmapTestBase


class TestComplexWorkflow:
    """Test complex multi-sprint workflow scenarios."""

    def setup_method(self):
        self.test = GroadmapTestBase()
        self.test.setup()

    def teardown_method(self):
        self.test.teardown()

    # ==================== Full Lifecycle ====================

    def test_full_development_cycle(self):
        """Simulate a full product development cycle across two sprints."""
        roadmap = self.test.create_roadmap("ecommerce-platform")
        self.test.use_roadmap(roadmap)

        # Sprint 1: Core infrastructure
        infra_tasks = {
            "db_schema": self.test.create_task(
                roadmap,
                "Design and implement database schema",
                "Application requires a persistent data store for orders and users",
                "Create PostgreSQL-compatible schema with foreign keys and indexes",
                "Schema passes all migration tests and supports required queries",
                priority=9, severity=8
            ),
            "auth": self.test.create_task(
                roadmap,
                "Implement JWT authentication service",
                "Users need secure, stateless authentication",
                "Build token issuance, validation, and refresh endpoints",
                "Auth tokens are accepted by all protected endpoints",
                priority=9, severity=9
            ),
            "api_gateway": self.test.create_task(
                roadmap,
                "Configure API gateway and routing",
                "Services must be reachable through a single entry point",
                "Deploy and configure Kong or nginx reverse proxy",
                "All services respond through gateway with correct headers",
                priority=8, severity=7
            ),
        }

        sprint1 = self.test.create_sprint(roadmap, "Sprint 1: Core Infrastructure")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint1),
            str(infra_tasks["db_schema"]), str(infra_tasks["auth"]), str(infra_tasks["api_gateway"])
        ])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint1)])

        # Advance all tasks through the pipeline
        for task_id in infra_tasks.values():
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint1)])
        self.test.assert_sprint_status(roadmap, sprint1, "CLOSED")

        # Verify all infra tasks completed
        for task_id in infra_tasks.values():
            self.test.assert_task_status(roadmap, task_id, "COMPLETED")

        # Sprint 2: Feature development
        feature_tasks = {
            "product_catalog": self.test.create_task(
                roadmap,
                "Build product catalog service",
                "Users need to browse and search products",
                "Implement REST API for product CRUD with search and pagination",
                "Product listing returns paginated results under 200ms",
                priority=8, severity=6
            ),
            "shopping_cart": self.test.create_task(
                roadmap,
                "Implement shopping cart functionality",
                "Users must be able to add, remove, and update cart items",
                "Build cart service with session persistence and inventory checks",
                "Cart operations are idempotent and session-safe",
                priority=8, severity=7
            ),
            "checkout": self.test.create_task(
                roadmap,
                "Integrate payment processing and checkout flow",
                "Users must be able to complete purchases",
                "Integrate Stripe SDK and implement order creation on success",
                "Checkout creates an order and sends confirmation email",
                priority=9, severity=9
            ),
        }

        sprint2 = self.test.create_sprint(roadmap, "Sprint 2: Feature Development")
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap, str(sprint2),
            str(feature_tasks["product_catalog"]),
            str(feature_tasks["shopping_cart"]),
            str(feature_tasks["checkout"]),
        ])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint2)])

        # Partially complete sprint 2
        for key in ["product_catalog", "shopping_cart"]:
            task_id = feature_tasks[key]
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        # Checkout is still in DOING
        checkout_id = feature_tasks["checkout"]
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(checkout_id), "DOING"])
        self.test.assert_task_status(roadmap, checkout_id, "DOING")

        # Verify sprint summary
        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["completed"] == 5, f"Expected 5 completed, got {stats['tasks']['completed']}"
        assert stats["tasks"]["doing"] == 1, f"Expected 1 doing, got {stats['tasks']['doing']}"

    # ==================== Task Carryover ====================

    def test_task_carryover_between_sprints(self):
        """Incomplete tasks from a closed sprint carry over to the next sprint."""
        roadmap = self.test.create_roadmap("cloud-migration")

        tasks = [
            self.test.create_task(
                roadmap,
                f"Migrate service {name} to Kubernetes",
                f"Service {name} must run on the new infrastructure",
                f"Containerise {name}, create Helm chart, deploy to cluster",
                f"{name} responds with 200 after rollout",
            )
            for name in ["auth-service", "billing-service", "notification-service", "analytics-service"]
        ]

        sprint1 = self.test.create_sprint(roadmap, "Migration Sprint 1")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint1)] + [str(t) for t in tasks])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint1)])

        # Complete only the first two tasks
        for task_id in tasks[:2]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        # Close sprint with incomplete tasks — tasks[2:] are still SPRINT, so --force is required
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint1), "--force"])
        self.test.assert_sprint_status(roadmap, sprint1, "CLOSED")

        # Remaining tasks are still in SPRINT status (sprint is CLOSED, tasks were never started)
        for task_id in tasks[2:]:
            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
            assert result[0]["status"] == "SPRINT", \
                f"Task {task_id} should remain SPRINT after close, got {result[0]['status']}"

        # Carry them over to sprint 2.
        # move-tasks from a CLOSED sprint is blocked (Task #89), so the correct carryover
        # workflow is: remove-tasks (→ BACKLOG) then add-tasks to the new sprint.
        sprint2 = self.test.create_sprint(roadmap, "Migration Sprint 2")
        self.test.run_cmd([
            "sprint", "remove-tasks", "-r", roadmap,
            str(sprint1), ",".join(str(t) for t in tasks[2:])
        ])
        self.test.run_cmd([
            "sprint", "add-tasks", "-r", roadmap,
            str(sprint2), ",".join(str(t) for t in tasks[2:])
        ])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint2)])

        # Verify sprint 2 has the remaining tasks
        sprint2_data = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint2)])
        assert sprint2_data["summary"]["total_tasks"] == 2, \
            f"Sprint 2 should have 2 tasks, got {sprint2_data['summary']['total_tasks']}"

        # Complete them in sprint 2
        for task_id in tasks[2:]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint2)])
        self.test.assert_sprint_status(roadmap, sprint2, "CLOSED")

    # ==================== Sprint Reopen (Rollback) ====================

    def test_sprint_reopen_and_recover(self):
        """Closed sprint can be reopened to address unfinished work."""
        roadmap = self.test.create_roadmap("data-pipeline")

        tasks = [
            self.test.create_task(
                roadmap,
                "Implement Kafka consumer for events",
                "Events from upstream systems must be consumed",
                "Build consumer group with dead-letter queue support",
                "Consumer processes 10k events/sec without lag",
                priority=8
            ),
            self.test.create_task(
                roadmap,
                "Build transformation layer",
                "Raw events must be enriched before storage",
                "Implement stream processing with Apache Flink",
                "Transformation latency under 50ms at 10k events/sec",
                priority=7
            ),
        ]

        sprint = self.test.create_sprint(roadmap, "Data Pipeline Sprint")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint)] + [str(t) for t in tasks])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])

        # Complete first task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[0]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[0]), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[0]), "COMPLETED"])

        # Close sprint prematurely — tasks[1] is still SPRINT (never started), --force required
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint), "--force"])
        self.test.assert_sprint_status(roadmap, sprint, "CLOSED")

        # Reopen to recover unfinished work
        self.test.run_cmd(["sprint", "reopen", "-r", roadmap, str(sprint)])
        self.test.assert_sprint_status(roadmap, sprint, "OPEN")

        # Complete the second task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[1]), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[1]), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(tasks[1]), "COMPLETED"])

        # Now properly close
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint)])
        self.test.assert_sprint_status(roadmap, sprint, "CLOSED")

        for task_id in tasks:
            self.test.assert_task_status(roadmap, task_id, "COMPLETED")

    # ==================== Task Ordering Within Sprint ====================

    def test_sprint_task_ordering_workflow(self):
        """Task ordering within a sprint reflects correct execution priority."""
        roadmap = self.test.create_roadmap("devops-sprint")

        task_titles = [
            "Provision production Kubernetes cluster",
            "Configure Terraform state backend in S3",
            "Deploy monitoring stack (Prometheus + Grafana)",
            "Set up alerting rules and PagerDuty integration",
            "Configure auto-scaling policies for web tier",
        ]

        task_ids = [
            self.test.create_task(
                roadmap,
                title,
                f"Operations require: {title}",
                f"Implementation approach for: {title}",
                f"Verified working: {title}",
            )
            for title in task_titles
        ]

        sprint = self.test.create_sprint(roadmap, "DevOps Sprint")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint)] + [str(t) for t in task_ids])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])

        # Reorder: Terraform first (prerequisite), then cluster, then rest
        ordered = [task_ids[1], task_ids[0], task_ids[2], task_ids[3], task_ids[4]]
        self.test.run_cmd([
            "sprint", "reorder", "-r", roadmap, str(sprint),
            ",".join(str(t) for t in ordered)
        ])

        # Verify order via sprint show
        sprint_data = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint)])
        assert sprint_data["task_order"] == ordered, \
            f"Expected order {ordered}, got {sprint_data['task_order']}"

        # Move monitoring (index 2) to top — it became urgent
        self.test.run_cmd(["sprint", "top", "-r", roadmap, str(sprint), str(task_ids[2])])
        sprint_data = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint)])
        assert sprint_data["task_order"][0] == task_ids[2], \
            f"Expected task {task_ids[2]} at top, got {sprint_data['task_order'][0]}"

        # Swap alerting and auto-scaling
        self.test.run_cmd(["sprint", "swap", "-r", roadmap, str(sprint), str(task_ids[3]), str(task_ids[4])])

        # Complete tasks in sprint order
        sprint_data = self.test.run_cmd_json(["sprint", "show", "-r", roadmap, str(sprint)])
        for task_id in sprint_data["task_order"]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint)])
        self.test.assert_sprint_status(roadmap, sprint, "CLOSED")

    # ==================== Parallel Sprint Management ====================

    def test_parallel_sprint_management(self):
        """Sequential sprint management: complete one team's sprint before starting the next."""
        roadmap = self.test.create_roadmap("multi-team-platform")

        team_a_tasks = [
            self.test.create_task(
                roadmap,
                f"Team Alpha — {task}",
                "Platform capability needed by team alpha",
                "Implementation owned by team alpha",
                "Accepted by team alpha tech lead",
            )
            for task in [
                "Build user notification preferences API",
                "Implement email template engine",
                "Add push notification support",
            ]
        ]

        team_b_tasks = [
            self.test.create_task(
                roadmap,
                f"Team Beta — {task}",
                "Platform capability needed by team beta",
                "Implementation owned by team beta",
                "Accepted by team beta tech lead",
            )
            for task in [
                "Create analytics event pipeline",
                "Build real-time dashboard backend",
                "Implement cohort analysis queries",
            ]
        ]

        sprint_a = self.test.create_sprint(roadmap, "Team Alpha Sprint")
        sprint_b = self.test.create_sprint(roadmap, "Team Beta Sprint")

        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_a)] + [str(t) for t in team_a_tasks])
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint_b)] + [str(t) for t in team_b_tasks])

        # Only one sprint can be OPEN at a time (sequential enforcement).
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_a)])

        # Verify exactly one open sprint
        open_sprints = self.test.run_cmd_json(["sprint", "list", "-r", roadmap, "--status", "OPEN"])
        assert len(open_sprints) == 1, f"Expected 1 open sprint, got {len(open_sprints)}"
        assert open_sprints[0]["id"] == sprint_a

        # Team A completes all tasks
        for task_id in team_a_tasks:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_a)])

        # No open sprints after team A
        open_sprints = self.test.run_cmd_json(["sprint", "list", "-r", roadmap, "--status", "OPEN"])
        assert len(open_sprints) == 0, f"Expected 0 open sprints after team A done, got {len(open_sprints)}"

        # Team B starts
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint_b)])

        # Team B completes
        for task_id in team_b_tasks:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint_b)])

        # All sprints closed
        all_sprints = self.test.run_cmd_json(["sprint", "list", "-r", roadmap])
        assert all(s["status"] == "CLOSED" for s in all_sprints), \
            "All sprints should be CLOSED"

    # ==================== Task Type Diversity ====================

    def test_multi_type_task_workflow(self):
        """Sprint with diverse task types: BUG, IMPROVEMENT, SPIKE, DOCUMENTATION."""
        roadmap = self.test.create_roadmap("legacy-modernisation")

        type_cases = [
            ("BUG",         "Fix memory leak in connection pool",
             "Connection pool leaks memory under sustained load",
             "Profile allocations with pprof, fix goroutine leak in pool cleanup",
             "Memory usage stable at 512MB under 1k concurrent connections"),
            ("IMPROVEMENT", "Optimise database query performance",
             "Slow queries degrade user experience during peak hours",
             "Add indexes, rewrite N+1 queries, enable query plan caching",
             "P99 query latency under 50ms at 5k QPS"),
            ("SPIKE",       "Evaluate gRPC migration feasibility",
             "REST overhead limits throughput between internal services",
             "Build prototype, benchmark vs REST, document trade-offs",
             "Report with benchmarks and go/no-go recommendation"),
            ("CHORE",       "Document internal API contract",
             "New engineers lack onboarding context for internal APIs",
             "Write OpenAPI spec, add usage examples to Confluence",
             "API spec passes openapi-lint with zero errors"),
        ]

        task_ids = []
        for task_type, title, fr, tr, ac in type_cases:
            exit_code, stdout, _ = self.test.run_cmd(
                ["task", "create", "-r", roadmap,
                 "-t", title, "-fr", fr, "-tr", tr, "-ac", ac,
                 "-y", task_type],
                check=False
            )
            assert exit_code == 0, f"Failed to create {task_type} task"
            import json as _json
            task_ids.append(_json.loads(stdout)["id"])

        # Verify types persisted
        for i, (task_type, _, _, _, _) in enumerate(type_cases):
            result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_ids[i])])
            assert result[0]["type"] == task_type, \
                f"Expected type {task_type}, got {result[0]['type']}"

        # Add all to sprint and complete
        sprint = self.test.create_sprint(roadmap, "Legacy Modernisation Sprint")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint)] + [str(t) for t in task_ids])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])

        for task_id in task_ids:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint)])

        # Stats reflect completion
        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["completed"] == 4
        assert stats["tasks"]["backlog"] == 0

    # ==================== State Machine Enforcement ====================

    def test_invalid_state_transitions_rejected(self):
        """Invalid state transitions are rejected without corrupting task state."""
        roadmap = self.test.create_roadmap("state-machine-test")

        task_id = self.test.create_task(
            roadmap,
            "Implement rate limiting middleware",
            "API must reject requests exceeding rate limits",
            "Implement token-bucket algorithm in middleware layer",
            "Rate limiter rejects requests exceeding 100 req/s per client",
        )

        # BACKLOG -> DOING (invalid: must go through SPRINT first)
        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "DOING"], check=False
        )
        assert exit_code != 0, "BACKLOG -> DOING should be rejected"
        self.test.assert_task_status(roadmap, task_id, "BACKLOG")

        # BACKLOG -> TESTING (invalid)
        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "TESTING"], check=False
        )
        assert exit_code != 0, "BACKLOG -> TESTING should be rejected"
        self.test.assert_task_status(roadmap, task_id, "BACKLOG")

        # Valid path: BACKLOG -> SPRINT
        sprint = self.test.create_sprint(roadmap, "Rate Limiting Sprint")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint), str(task_id)])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])
        self.test.assert_task_status(roadmap, task_id, "SPRINT")

        # SPRINT -> TESTING (invalid: must go through DOING)
        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "TESTING"], check=False
        )
        assert exit_code != 0, "SPRINT -> TESTING should be rejected"
        self.test.assert_task_status(roadmap, task_id, "SPRINT")

        # Valid: SPRINT -> DOING -> TESTING -> COMPLETED
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])

        # COMPLETED -> DOING (invalid: terminal state)
        exit_code, _, _ = self.test.run_cmd(
            ["task", "stat", "-r", roadmap, str(task_id), "DOING"], check=False
        )
        assert exit_code != 0, "COMPLETED -> DOING should be rejected"
        self.test.assert_task_status(roadmap, task_id, "COMPLETED")

    # ==================== Bulk Operations ====================

    def test_bulk_task_status_transition(self):
        """Multiple tasks can be transitioned simultaneously with comma-separated IDs."""
        roadmap = self.test.create_roadmap("bulk-ops-project")

        services = [
            "User profile service",
            "Notification dispatch service",
            "Audit logging service",
            "Feature flag service",
            "Configuration management service",
        ]

        task_ids = [
            self.test.create_task(
                roadmap,
                f"Deploy {service} to production",
                f"{service} must be available in production",
                f"Package {service} as Docker image and deploy to cluster",
                f"{service} health check returns 200 in production",
            )
            for service in services
        ]

        sprint = self.test.create_sprint(roadmap, "Production Deployment Sprint")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint)] + [str(t) for t in task_ids])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])

        # Bulk transition all to DOING
        ids_str = ",".join(str(t) for t in task_ids)
        self.test.run_cmd(["task", "stat", "-r", roadmap, ids_str, "DOING"])
        for task_id in task_ids:
            self.test.assert_task_status(roadmap, task_id, "DOING")

        # Bulk transition all to TESTING
        self.test.run_cmd(["task", "stat", "-r", roadmap, ids_str, "TESTING"])
        for task_id in task_ids:
            self.test.assert_task_status(roadmap, task_id, "TESTING")

        # Bulk transition all to COMPLETED
        self.test.run_cmd(["task", "stat", "-r", roadmap, ids_str, "COMPLETED"])
        for task_id in task_ids:
            self.test.assert_task_status(roadmap, task_id, "COMPLETED")

        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint)])

        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["completed"] == 5
        assert stats["tasks"]["doing"] == 0

    # ==================== Task Edit During Sprint ====================

    def test_task_edit_during_active_sprint(self):
        """Tasks can be edited while they are active in a sprint."""
        roadmap = self.test.create_roadmap("live-editing-test")

        task_id = self.test.create_task(
            roadmap,
            "Implement search indexing service",
            "Users need full-text search across all content",
            "Build Elasticsearch indexer with change data capture",
            "Search returns results in under 100ms for 95th percentile",
            priority=5
        )

        sprint = self.test.create_sprint(roadmap, "Search Sprint")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint), str(task_id)])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])

        # Edit title and priority mid-sprint
        self.test.run_cmd([
            "task", "edit", "-r", roadmap, str(task_id),
            "-t", "Implement Elasticsearch indexing with real-time sync",
            "-p", "9"
        ])

        result = self.test.run_cmd_json(["task", "get", "-r", roadmap, str(task_id)])
        assert result[0]["title"] == "Implement Elasticsearch indexing with real-time sync"
        assert result[0]["priority"] == 9
        assert result[0]["status"] == "DOING", "Edit should not change status"

        # Complete the edited task
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "COMPLETED"])
        self.test.run_cmd(["sprint", "close", "-r", roadmap, str(sprint)])

    # ==================== Roadmap Stats Accuracy ====================

    def test_stats_reflect_sprint_lifecycle(self):
        """Stats accurately reflect task distribution across sprint lifecycle."""
        roadmap = self.test.create_roadmap("stats-accuracy-test")

        # Create 10 tasks
        task_ids = [
            self.test.create_task(
                roadmap,
                f"Implement feature {i:02d}: {desc}",
                "Business requirement for the platform",
                "Technical implementation details",
                "Acceptance criteria for delivery",
            )
            for i, desc in enumerate([
                "user onboarding flow",
                "email verification",
                "password reset",
                "two-factor authentication",
                "session management",
                "account deletion",
                "profile photo upload",
                "notification preferences",
                "activity audit log",
                "API rate limiting",
            ])
        ]

        # Initially all backlog
        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["backlog"] == 10

        sprint = self.test.create_sprint(roadmap, "Feature Sprint")
        self.test.run_cmd(["sprint", "add-tasks", "-r", roadmap, str(sprint)] + [str(t) for t in task_ids[:8]])
        self.test.run_cmd(["sprint", "start", "-r", roadmap, str(sprint)])

        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["backlog"] == 2
        assert stats["tasks"]["sprint"] == 8

        # Advance 3 tasks to DOING
        for task_id in task_ids[:3]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "DOING"])
        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["sprint"] == 5
        assert stats["tasks"]["doing"] == 3

        # Advance 2 to TESTING
        for task_id in task_ids[:2]:
            self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_id), "TESTING"])
        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["doing"] == 1
        assert stats["tasks"]["testing"] == 2

        # Complete 1
        self.test.run_cmd(["task", "stat", "-r", roadmap, str(task_ids[0]), "COMPLETED"])
        stats = self.test.run_cmd_json(["stats", "-r", roadmap])
        assert stats["tasks"]["completed"] == 1
        assert stats["tasks"]["testing"] == 1

        # Verify total sums to 10
        total = (stats["tasks"]["backlog"] + stats["tasks"]["sprint"] +
                 stats["tasks"]["doing"] + stats["tasks"]["testing"] +
                 stats["tasks"]["completed"])
        assert total == 10, f"Total tasks should be 10, got {total}"


def main():
    """Run all tests."""
    test = TestComplexWorkflow()
    methods = [m for m in dir(test) if m.startswith("test_")]
    passed = 0
    failed = 0

    for method_name in methods:
        test.setup_method()
        try:
            getattr(test, method_name)()
            passed += 1
        except Exception as e:
            print(f"✗ {method_name} failed: {e}")
            failed += 1
        finally:
            test.teardown_method()

    print(f"\n{passed} passed, {failed} failed")
    return failed == 0


if __name__ == "__main__":
    sys.exit(0 if main() else 1)
