package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// Statement counting for acceptance criterion 3 of rmp task #162: the grouped
// comment read must issue ONE statement for N parents, and that has to be
// measured, not asserted by reading the code.
//
// The measurement interposes on the physical connections database/sql opens, which
// is the pattern modernc.org/sqlite documents on NewConnector: the connector is
// wrapped, every connection it hands out is wrapped, and every statement those
// connections execute increments a counter. Counting at the driver boundary counts
// round trips to SQLite, which is exactly what the N+1 rule is about, and it is
// blind to how the query was built, so it cannot be satisfied by a query that
// merely looks like one statement.
//
// Both execution paths database/sql can take are counted, and each exactly once:
// the direct one (conn.QueryContext / conn.ExecContext) and the prepared one
// (conn.PrepareContext followed by stmt.Query / stmt.Exec, where only the stmt
// execution counts).

// stmtCounter counts the statements executed on the connections of one database.
// database/sql opens connections from several goroutines, so the counter is atomic.
type stmtCounter struct {
	n atomic.Int64
}

func (c *stmtCounter) add()       { c.n.Add(1) }
func (c *stmtCounter) reset()     { c.n.Store(0) }
func (c *stmtCounter) count() int { return int(c.n.Load()) }

// countingConnector wraps a driver.Connector so every connection it opens counts
// the statements it executes.
type countingConnector struct {
	driver.Connector
	counter *stmtCounter
}

func (c countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	inner, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: inner, counter: c.counter}, nil
}

// countingConn forwards every driver.Conn capability the SQLite driver implements
// and counts the statements that pass through it. The embedded Conn supplies the
// methods that are not overridden here (Close, Begin, ...).
type countingConn struct {
	driver.Conn
	counter *stmtCounter
}

func (c *countingConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{Stmt: stmt, counter: c.counter}, nil
}

func (c *countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	preparer, ok := c.Conn.(driver.ConnPrepareContext)
	if !ok {
		return c.Prepare(query)
	}
	stmt, err := preparer.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{Stmt: stmt, counter: c.counter}, nil
}

func (c *countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		// Reported as "not supported here"; database/sql then falls back to the
		// prepared path, which counts on the statement instead.
		return nil, driver.ErrSkip
	}
	c.counter.add()
	return queryer.QueryContext(ctx, query, args)
}

func (c *countingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.counter.add()
	return execer.ExecContext(ctx, query, args)
}

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	beginner, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return c.Conn.Begin()
	}
	return beginner.BeginTx(ctx, opts)
}

// countingStmt counts each execution of a prepared statement. Preparing is not an
// execution, so it is not counted: otherwise one query taking the prepared path
// would count twice and the "one statement" assertion would be meaningless.
type countingStmt struct {
	driver.Stmt
	counter *stmtCounter
}

func (s *countingStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.counter.add()
	return s.Stmt.Query(args) //nolint:staticcheck // the deprecated form is what the embedded driver.Stmt offers
}

func (s *countingStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.counter.add()
	return s.Stmt.Exec(args) //nolint:staticcheck // the deprecated form is what the embedded driver.Stmt offers
}

func (s *countingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.Stmt.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	s.counter.add()
	return queryer.QueryContext(ctx, args)
}

func (s *countingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := s.Stmt.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	s.counter.add()
	return execer.ExecContext(ctx, args)
}

// setupCountingDB opens a real on-disk database through the counting connector and
// creates the production schema in it. The DSN is the production one (dsnFor), so
// the connections carry the same PRAGMAs production uses, foreign keys included.
func setupCountingDB(t *testing.T) (*DB, *stmtCounter, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "project.db")
	base, err := sqlite.NewConnector(dsnFor(dbPath, false))
	if err != nil {
		t.Fatalf("building the connector for %s: %v", dbPath, err)
	}

	counter := &stmtCounter{}
	sqlDB := sql.OpenDB(countingConnector{Connector: base, counter: counter})

	database := &DB{
		DB:         sqlDB,
		queryCache: NewQueryCache(),
		batchProc:  NewBatchProcessor(100),
	}
	if err := database.CreateSchema(); err != nil {
		sqlDB.Close()
		t.Fatalf("creating the schema: %v", err)
	}

	return database, counter, func() { database.Close() }
}

// TestSingleParentCommentListingIsOneStatement pins the same property for the
// per-parent listing the CLI and the sprint page use: one statement, with or
// without the type filter, and one for a by-id read.
func TestSingleParentCommentListingIsOneStatement(t *testing.T) {
	db, counter, cleanup := setupCountingDB(t)
	defer cleanup()

	taskID := newTestTask(t, db, "Alert on any settlement window that fails to balance")
	sprintID := newTestSprintWithCap(t, db, "Settlement reconciliation", 0)
	commentID := addTaskComment(t, db, taskID, models.CommentFinding,
		"The alert fires on the staging ledger; production wiring is next.", "2026-08-17T07:00:00.000Z")
	addSprintComment(t, db, sprintID, models.CommentProgress,
		"Two of the six planned tasks are closed.", "2026-08-17T07:30:00.000Z")

	finding := models.CommentFinding
	reads := map[string]func() error{
		"task listing":                func() error { _, err := db.ListTaskComments(testContext(), taskID, nil); return err },
		"task listing, type filtered": func() error { _, err := db.ListTaskComments(testContext(), taskID, &finding); return err },
		"sprint listing":              func() error { _, err := db.ListSprintComments(testContext(), sprintID, nil); return err },
		"task comment by id":          func() error { _, err := db.GetTaskComment(testContext(), commentID); return err },
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			counter.reset()
			if err := read(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got := counter.count(); got != 1 {
				t.Errorf("%s issued %d statements, want exactly 1", name, got)
			}
		})
	}
}
