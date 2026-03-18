package db_test

import (
    "context"
    "database/sql"
    "os"
    "path/filepath"
    "strings"
    "testing"

    _ "modernc.org/sqlite"
)

func TestCompositeIndexes(t *testing.T) {
    // Create temp database
    tmpDir, err := os.MkdirTemp("", "groadmap_index_test")
    if err != nil {
        t.Fatalf("Failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    dbPath := filepath.Join(tmpDir, "test.db")
    db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
    if err != nil {
        t.Fatalf("Failed to open db: %v", err)
    }
    defer db.Close()

    // Create schema with composite indexes
    schema := `
CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    priority INTEGER NOT NULL DEFAULT 0,
    severity INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'BACKLOG',
    description TEXT NOT NULL,
    specialists TEXT,
    action TEXT NOT NULL,
    expected_result TEXT NOT NULL,
    created_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
CREATE INDEX IF NOT EXISTS idx_tasks_status_priority ON tasks(status, priority DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_priority_created ON tasks(priority DESC, created_at ASC);

CREATE TABLE IF NOT EXISTS sprint_tasks (
    sprint_id INTEGER NOT NULL,
    task_id INTEGER NOT NULL UNIQUE,
    added_at TEXT NOT NULL,
    PRIMARY KEY (sprint_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_sprint_tasks_task_id ON sprint_tasks(task_id);
CREATE INDEX IF NOT EXISTS idx_sprint_tasks_lookup ON sprint_tasks(sprint_id, task_id);

CREATE TABLE IF NOT EXISTS audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id INTEGER NOT NULL,
    performed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_operation ON audit(operation);
CREATE INDEX IF NOT EXISTS idx_audit_performed_at ON audit(performed_at);
CREATE INDEX IF NOT EXISTS idx_audit_date ON audit(performed_at DESC);
`
    _, err = db.Exec(schema)
    if err != nil {
        t.Fatalf("Failed to create schema: %v", err)
    }

    // Insert test data
    for i := 0; i < 100; i++ {
        _, err = db.Exec("INSERT INTO tasks (priority, severity, status, description, action, expected_result, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
            i%10, i%5, "BACKLOG", "desc", "action", "result", "2026-03-18T10:00:00Z")
        if err != nil {
            t.Fatalf("Failed to insert: %v", err)
        }
    }

    // Test cases
    tests := []struct {
        name        string
        query       string
        indexNeeded string
    }{
        {
            name:        "ListTasks with status filter uses idx_tasks_status_priority",
            query:       "SELECT * FROM tasks WHERE status = 'BACKLOG' ORDER BY priority DESC",
            indexNeeded: "idx_tasks_status_priority",
        },
        {
            name:        "Priority filter with date ordering uses idx_tasks_priority_created",
            query:       "SELECT * FROM tasks WHERE priority >= 5 ORDER BY priority DESC, created_at ASC",
            indexNeeded: "idx_tasks_priority_created",
        },
        {
            name:        "Sprint tasks lookup uses idx_sprint_tasks_lookup",
            query:       "SELECT task_id FROM sprint_tasks WHERE sprint_id = 1",
            indexNeeded: "idx_sprint_tasks_lookup",
        },
        {
            name:        "Audit date range uses idx_audit_date",
            query:       "SELECT * FROM audit WHERE performed_at >= '2026-01-01' AND performed_at <= '2026-12-31' ORDER BY performed_at DESC",
            indexNeeded: "idx_audit_date",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            rows, err := db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+tt.query)
            if err != nil {
                t.Fatalf("EXPLAIN QUERY PLAN failed: %v", err)
            }
            defer rows.Close()

            var plan strings.Builder
            for rows.Next() {
                var id, parent, notused int
                var detail string
                if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
                    t.Fatalf("Failed to scan row: %v", err)
                }
                plan.WriteString(detail + " ")
            }

            planStr := plan.String()
            if !strings.Contains(planStr, tt.indexNeeded) {
                t.Errorf("Query plan does not use expected index %q\nPlan: %s", tt.indexNeeded, planStr)
            } else {
                t.Logf("✓ Uses index %s", tt.indexNeeded)
            }
        })
    }
}
