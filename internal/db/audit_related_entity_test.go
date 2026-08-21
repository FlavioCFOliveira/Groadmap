// Package db — tests for the counterpart column of an audit row (rmp task #263).
//
// The rows themselves are asserted where the commands that write them live; what
// is pinned here is the writer that every one of those call sites goes through.
// Three properties belong to it and to nothing else:
//
//  1. The option reaches the column. A setter that built the row and never bound
//     it would leave every call site correct and every stored row NULL, and no
//     command-level count would notice.
//  2. The operation decides whether a counterpart is admissible at all, so the
//     table-wide invariant of SPEC/DATABASE.md § The Two Entities of a
//     Relational Operation is structure rather than call-site discipline.
//  3. The write belongs to the transaction that performs the mutation, so a
//     rolled-back change leaves no row claiming it happened.
package db

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// auditRelatedStamp is the timestamp the rows written here carry. A fixed value
// keeps the assertions on performed_at exact.
const auditRelatedStamp = "2026-08-21T08:30:00.000Z"

// readRelatedEntityID returns the stored related_entity_id of the newest row
// carrying op, and whether it is non-NULL.
func readRelatedEntityID(t *testing.T, db *DB, op models.AuditOperation) (int, bool) {
	t.Helper()

	var related sql.NullInt64
	if err := db.QueryRow(
		`SELECT related_entity_id FROM audit WHERE operation = ? ORDER BY id DESC LIMIT 1`,
		string(op),
	).Scan(&related); err != nil {
		t.Fatalf("reading the stored %s row: %v", op, err)
	}
	return int(related.Int64), related.Valid
}

// TestWithRelatedEntityReachesTheColumn pins the option end to end for one
// operation of each shape in the eight-case table: a sprint row naming a task, a
// task row naming a sprint, and a dependency row naming the other task.
//
// The stored value is read straight out of SQLite rather than through
// GetAuditEntries, because a writer that bound nothing and a reader that
// returned a constant would agree with each other and disagree with the table.
func TestWithRelatedEntityReachesTheColumn(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	cases := []struct {
		op         models.AuditOperation
		entityType models.EntityType
		entityID   int
		related    int
	}{
		{models.OpSprintAddTask, models.EntitySprint, 1, 42},
		{models.OpTaskStatusSprint, models.EntityTask, 42, 1},
		{models.OpSprintRemoveTask, models.EntitySprint, 1, 42},
		{models.OpTaskStatusBacklog, models.EntityTask, 42, 1},
		{models.OpSprintMoveTaskOut, models.EntitySprint, 1, 42},
		{models.OpSprintMoveTaskIn, models.EntitySprint, 2, 42},
		{models.OpTaskAddDep, models.EntityTask, 42, 43},
		{models.OpTaskRemoveDep, models.EntityTask, 43, 42},
	}

	for _, c := range cases {
		t.Run(string(c.op), func(t *testing.T) {
			if err := db.WithTransaction(func(tx *sql.Tx) error {
				return LogAuditTx(tx, c.op, c.entityType, c.entityID, auditRelatedStamp,
					WithRelatedEntity(c.related))
			}); err != nil {
				t.Fatalf("writing a %s row naming #%d: %v", c.op, c.related, err)
			}

			got, present := readRelatedEntityID(t, db, c.op)
			if !present {
				t.Fatalf("the stored %s row holds NULL in related_entity_id; the option was accepted and "+
					"never bound, so every call site would look correct and every row would say nothing",
					c.op)
			}
			if got != c.related {
				t.Errorf("the stored %s row names #%d, want #%d", c.op, got, c.related)
			}
		})
	}
}

// TestLogAuditTxRejectsACounterpartOnAnOperationThatHasNone is the structural
// half of the invariant. SPEC/DATABASE.md § The Two Entities of a Relational
// Operation admits related_entity_id on exactly eight operations and requires
// NULL on every other one; being the only audit writer, LogAuditTx enforces that
// at the point of the INSERT rather than leaving it to each call site.
//
// The sweep is exhaustive over the whole valid set, so an operation added later
// is covered without anyone remembering to extend this test.
func TestLogAuditTxRejectsACounterpartOnAnOperationThatHasNone(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	for _, op := range models.ValidAuditOperations {
		entityType := models.EntityTask
		if len(op) >= 6 && op[:6] == "SPRINT" {
			entityType = models.EntitySprint
		}

		err := db.WithTransaction(func(tx *sql.Tx) error {
			return LogAuditTx(tx, op, entityType, 7, auditRelatedStamp, WithRelatedEntity(9))
		})

		if models.OperationCarriesRelatedEntity(op) {
			if err != nil {
				t.Errorf("LogAuditTx refused a counterpart on %s: %v; the operation is one of the eight "+
					"that carry one", op, err)
			}
			continue
		}
		if !errors.Is(err, ErrAuditRelatedEntityNotAllowed) {
			t.Errorf("LogAuditTx accepted a counterpart on %s (error = %v); the operation has no second "+
				"entity party to it, so a non-NULL related_entity_id on it can only be a defect", op, err)
		}
	}

	// Nothing the loop refused reached the table.
	var stored int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM audit
		  WHERE related_entity_id IS NOT NULL
		    AND operation NOT IN ('SPRINT_ADD_TASK', 'TASK_STATUS_SPRINT', 'SPRINT_REMOVE_TASK',
		                          'TASK_STATUS_BACKLOG', 'SPRINT_MOVE_TASK_OUT', 'SPRINT_MOVE_TASK_IN',
		                          'TASK_ADD_DEP', 'TASK_REMOVE_DEP')`,
	).Scan(&stored); err != nil {
		t.Fatalf("checking the table-wide invariant: %v", err)
	}
	if stored != 0 {
		t.Errorf("%d rows carry a counterpart on an operation outside the eight; the invariant of "+
			"SPEC/DATABASE.md § The Two Entities of a Relational Operation, acceptance criterion 7, does "+
			"not hold", stored)
	}
}

// TestLogAuditTxCounterpartRollsBackWithItsTransaction covers the failure
// direction of the atomicity guarantee for the new column: a transaction that
// writes the rows and then fails leaves none of them behind.
//
// The rows are written first and the failure is raised afterwards, on purpose. A
// transaction that fails before the audit write proves nothing — no row was ever
// attempted — so the test would pass against an implementation that wrote its
// audit outside the transaction entirely, which is the defect the guarantee
// exists to prevent (SPEC/DATABASE.md § Transactional Atomicity Guarantees).
func TestLogAuditTxCounterpartRollsBackWithItsTransaction(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	pairs := []struct {
		op         models.AuditOperation
		entityType models.EntityType
		entityID   int
		related    int
	}{
		{models.OpSprintAddTask, models.EntitySprint, 1, 42},
		{models.OpTaskStatusSprint, models.EntityTask, 42, 1},
		{models.OpSprintMoveTaskOut, models.EntitySprint, 1, 42},
		{models.OpSprintMoveTaskIn, models.EntitySprint, 2, 42},
	}

	sentinel := errors.New("the mutation this audit describes failed")
	err := db.WithTransaction(func(tx *sql.Tx) error {
		for _, p := range pairs {
			if logErr := LogAuditTx(tx, p.op, p.entityType, p.entityID, auditRelatedStamp,
				WithRelatedEntity(p.related)); logErr != nil {
				return logErr
			}
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTransaction returned %v, want the sentinel; the rows must have been written before "+
			"the failure or this proves nothing", err)
	}

	for _, p := range pairs {
		if n := countAuditByOp(t, db, p.op); n != 0 {
			t.Errorf("%d %s rows survived a rolled-back transaction; the audit write happens inside the "+
				"transaction that performs the mutation, so it rolls back with it", n, p.op)
		}
	}

	// The rows really can be written by this sequence: repeating it without the
	// failure stores every one of them, so the assertion above is not vacuous.
	if err := db.WithTransaction(func(tx *sql.Tx) error {
		for _, p := range pairs {
			if logErr := LogAuditTx(tx, p.op, p.entityType, p.entityID, auditRelatedStamp,
				WithRelatedEntity(p.related)); logErr != nil {
				return logErr
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("writing the same rows without a failure: %v", err)
	}
	for _, p := range pairs {
		if n := countAuditByOp(t, db, p.op); n != 1 {
			t.Fatalf("the committed transaction stored %d %s rows, want 1; the rollback assertion above "+
				"passed because the rows are never written at all", n, p.op)
		}
	}
}

// TestAddTasksToSprint_WritesTheMirroredPair is the db-layer half of the
// membership-change contract: the method the `sprint add-tasks` command calls
// writes both sides of the pair, with transposed ids and one shared
// performed_at, inside the transaction that changes the membership.
//
// The command-level suite in internal/commands asserts the same invariant
// through the CLI; this one pins it at the boundary the command actually calls,
// so a regression is attributed to the method rather than to the command.
func TestAddTasksToSprint_WritesTheMirroredPair(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID, err := db.CreateSprint(testContext(), &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Transport hardening",
		Description: "Close the TLS and connection-pool findings raised by the review.",
		CreatedAt:   time.Now().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("seeding the sprint: %v", err)
	}

	taskIDs := make([]int, 0, 3)
	for _, title := range []string{
		"Pin the TLS cipher suite list to the approved set",
		"Expire idle database connections before the proxy does",
		"Emit a request id on every access log line",
	} {
		id, createErr := db.CreateTask(testContext(), &models.Task{
			Priority:               1,
			Severity:               1,
			Status:                 models.StatusBacklog,
			Title:                  title,
			FunctionalRequirements: "Stated in the roadmap entry for this task.",
			TechnicalRequirements:  "Stated in the roadmap entry for this task.",
			AcceptanceCriteria:     "Stated in the roadmap entry for this task.",
			CreatedAt:              time.Now().Format(time.RFC3339),
		})
		if createErr != nil {
			t.Fatalf("seeding task %q: %v", title, createErr)
		}
		taskIDs = append(taskIDs, id)
	}

	if err := db.AddTasksToSprint(testContext(), sprintID, taskIDs); err != nil {
		t.Fatalf("adding the tasks to the sprint: %v", err)
	}

	// Every pair, matched by the self-join that is the invariant itself.
	var mirrored int
	if err := db.QueryRow(
		`SELECT COUNT(*)
		   FROM audit a
		   JOIN audit b
		     ON b.operation         = ?
		    AND b.entity_id         = a.related_entity_id
		    AND b.related_entity_id = a.entity_id
		    AND b.performed_at      = a.performed_at
		  WHERE a.operation = ? AND a.entity_id = ?`,
		string(models.OpTaskStatusSprint), string(models.OpSprintAddTask), sprintID,
	).Scan(&mirrored); err != nil {
		t.Fatalf("joining the two sides of the membership change: %v", err)
	}
	if mirrored != len(taskIDs) {
		t.Errorf("%d of the %d %s rows have a mirrored %s row with transposed ids and a shared "+
			"performed_at, want all of them", mirrored, len(taskIDs),
			models.OpSprintAddTask, models.OpTaskStatusSprint)
	}

	// And the tasks named are the tasks added, each exactly once.
	var distinct int
	if err := db.QueryRow(
		`SELECT COUNT(DISTINCT related_entity_id) FROM audit WHERE operation = ?`,
		string(models.OpSprintAddTask),
	).Scan(&distinct); err != nil {
		t.Fatalf("counting the tasks named: %v", err)
	}
	if distinct != len(taskIDs) {
		t.Errorf("the %s rows name %d distinct tasks, want %d", models.OpSprintAddTask, distinct, len(taskIDs))
	}
}

// TestAddTasksToSprint_OnePerformedAtForTheWholeInvocation pins the second half
// of the mirror contract: every row one invocation writes carries the same
// performed_at, so the rows of one command are recognisable as one event and the
// self-join above has something to join on (SPEC/COMMANDS.md § Task Assignment).
//
// The batch is deliberately large. performed_at has millisecond resolution, so a
// writer that stamped each row as it wrote it would be indistinguishable from a
// correct one over three rows written in a few microseconds — the assertion
// would hold by accident rather than by construction. Several hundred rows take
// long enough to write that a per-row stamp lands in more than one millisecond,
// which is what makes this test able to fail at all.
//
// The tasks are seeded through the db layer rather than the CLI because the
// subject here is the timestamp the write path captures, not the command that
// calls it, and a batch this size through the CLI would cost seconds.
func TestAddTasksToSprint_OnePerformedAtForTheWholeInvocation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	sprintID, err := db.CreateSprint(testContext(), &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Observability rollout",
		Description: "Make a single request traceable end to end across the fleet.",
		CreatedAt:   time.Now().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("seeding the sprint: %v", err)
	}

	// Enough rows that writing them takes longer than the timestamp's
	// resolution; see the note above.
	const batch = 300
	taskIDs := make([]int, 0, batch)
	for i := 0; i < batch; i++ {
		id, createErr := db.CreateTask(testContext(), &models.Task{
			Priority:               1,
			Severity:               1,
			Status:                 models.StatusBacklog,
			Title:                  "Instrument the outbound HTTP client with the trace propagator",
			FunctionalRequirements: "An outbound call carries the caller's trace context.",
			TechnicalRequirements:  "Wrap the shared transport with the propagator middleware.",
			AcceptanceCriteria:     "The downstream span is a child of the caller's span.",
			CreatedAt:              time.Now().Format(time.RFC3339),
		})
		if createErr != nil {
			t.Fatalf("seeding task %d: %v", i, createErr)
		}
		taskIDs = append(taskIDs, id)
	}

	if err := db.AddTasksToSprint(testContext(), sprintID, taskIDs); err != nil {
		t.Fatalf("adding %d tasks to the sprint: %v", batch, err)
	}

	var rows, stamps int
	if err := db.QueryRow(
		`SELECT COUNT(*), COUNT(DISTINCT performed_at) FROM audit WHERE operation IN (?, ?)`,
		string(models.OpSprintAddTask), string(models.OpTaskStatusSprint),
	).Scan(&rows, &stamps); err != nil {
		t.Fatalf("counting the invocation's rows: %v", err)
	}
	if rows != 2*batch {
		t.Fatalf("the invocation wrote %d rows for %d tasks, want %d (a pair each)", rows, batch, 2*batch)
	}
	if stamps != 1 {
		t.Errorf("the %d rows of one invocation carry %d distinct performed_at values, want 1; the "+
			"timestamp is captured once for the whole command, not once per row", rows, stamps)
	}
}
