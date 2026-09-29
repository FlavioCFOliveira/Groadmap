package db

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The tests in this file pin the write discipline of the position routines
// (SPEC/DATABASE.md § Compact Sprint Positions, § Reorder Sprint Tasks (Set
// Exact Order) and § Move Task to Position): each per-member statement is
// prepared once per transaction, a member whose position does not change is
// not written, and a move-to writes only the affected range.

// largeSprintSize is the member count of the fixture sprint: large enough that
// a routine rewriting the whole sprint and one writing only what changes differ
// by an order of magnitude in the statements they execute.
const largeSprintSize = 120

// seedLargeSprint creates a sprint with n member tasks, added in one batch so
// they hold the dense run 0..n-1 in id order, and returns the sprint id and the
// member ids in position order.
func seedLargeSprint(t *testing.T, database *DB, n int) (int, []int) {
	t.Helper()
	sprintID := newTestSprintWithCap(t, database, "Card-network certification backlog", 0)
	ids := make([]int, n)
	for i := range ids {
		ids[i] = newTestTask(t, database, fmt.Sprintf("Certify the card-network message flow %d", i+1))
	}
	if err := database.AddTasksToSprint(testContext(), sprintID, ids); err != nil {
		t.Fatalf("adding %d tasks to the sprint: %v", n, err)
	}
	return sprintID, ids
}

// positionsOf reads every member of the sprint with its position, in position
// order.
func positionsOf(t *testing.T, database *DB, sprintID int) []sprintMember {
	t.Helper()
	var members []sprintMember
	err := database.WithTransaction(func(tx *sql.Tx) error {
		var err error
		members, err = sprintMembersInOrderTx(tx, sprintID)
		return err
	})
	if err != nil {
		t.Fatalf("reading the sprint positions: %v", err)
	}
	return members
}

// orderOf returns the member ids in position order and fails the test unless
// the positions are the dense run 0..N-1.
func orderOf(t *testing.T, database *DB, sprintID int) []int {
	t.Helper()
	members := positionsOf(t, database, sprintID)
	ids := make([]int, len(members))
	for i, m := range members {
		if m.position != i {
			t.Fatalf("member %d holds position %d at rank %d; the run is not dense", m.taskID, m.position, i)
		}
		ids[i] = m.taskID
	}
	return ids
}

// moved returns order with the element at from lifted out and re-inserted at to.
func moved(order []int, from, to int) []int {
	out := slices.Delete(slices.Clone(order), from, from+1)
	return slices.Insert(out, to, order[from])
}

// TestCompactionWritesOnlyTheMembersThatMove removes one member from the middle
// of a large sprint and compacts: only the members after the gap are written,
// through one statement prepared once. Compacting a sprint that is already
// dense writes nothing and prepares nothing.
func TestCompactionWritesOnlyTheMembersThatMove(t *testing.T) {
	database, counter, cleanup := setupCountingDB(t)
	defer cleanup()
	sprintID, ids := seedLargeSprint(t, database, largeSprintSize)

	const removedRank = 30
	counter.reset()
	err := database.WithTransaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM sprint_tasks WHERE sprint_id = ? AND task_id = ?", sprintID, ids[removedRank]); err != nil {
			return err
		}
		return CompactSprintPositionsTx(tx, sprintID)
	})
	if err != nil {
		t.Fatalf("removing and compacting: %v", err)
	}
	if got, want := counter.executed(assignPositionSQL), largeSprintSize-1-removedRank; got != want {
		t.Errorf("compaction wrote %d rows, want %d: only the members after the gap move", got, want)
	}
	if got := counter.prepared(assignPositionSQL); got != 1 {
		t.Errorf("compaction prepared its statement %d times, want once", got)
	}
	if got, want := orderOf(t, database, sprintID), slices.Delete(slices.Clone(ids), removedRank, removedRank+1); !slices.Equal(got, want) {
		t.Errorf("compaction changed the order of the survivors:\n got %v\nwant %v", got, want)
	}

	counter.reset()
	if err := database.WithTransaction(func(tx *sql.Tx) error { return CompactSprintPositionsTx(tx, sprintID) }); err != nil {
		t.Fatalf("compacting a dense sprint: %v", err)
	}
	if got := counter.executed(assignPositionSQL) + counter.prepared(assignPositionSQL); got != 0 {
		t.Errorf("compacting a dense sprint prepared or wrote %d times, want nothing", got)
	}
}

// TestMoveToPositionWritesOnlyTheAffectedRange moves a task down, up, to its own
// rank and past the end of a sprint of more than a hundred members, and checks
// the resulting order, that every member outside the affected range kept its
// position, and that the range is parked and assigned through one statement
// each, prepared once.
func TestMoveToPositionWritesOnlyTheAffectedRange(t *testing.T) {
	database, counter, cleanup := setupCountingDB(t)
	defer cleanup()
	sprintID, ids := seedLargeSprint(t, database, largeSprintSize)

	cases := []struct {
		name     string
		from, to int // ranks; target passed to MoveTaskToPosition
		wantRank int // the rank the task must end at
	}{
		{name: "down the sprint", from: 10, to: 90, wantRank: 90},
		{name: "up the sprint", from: 95, to: 3, wantRank: 3},
		{name: "to the rank it holds", from: 40, to: 40, wantRank: 40},
		{name: "past the end", from: 7, to: 5000, wantRank: largeSprintSize - 1},
		{name: "to the top", from: largeSprintSize - 1, to: 0, wantRank: 0},
	}
	order := ids
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := positionsOf(t, database, sprintID)
			taskID := order[c.from]

			counter.reset()
			if err := database.MoveTaskToPosition(sprintID, taskID, c.to); err != nil {
				t.Fatalf("moving task %d to %d: %v", taskID, c.to, err)
			}

			want := moved(order, c.from, c.wantRank)
			got := orderOf(t, database, sprintID)
			if !slices.Equal(got, want) {
				t.Fatalf("order after the move:\n got %v\nwant %v", got, want)
			}
			order = got

			lo, hi := min(c.from, c.wantRank), max(c.from, c.wantRank)
			after := positionsOf(t, database, sprintID)
			for rank, m := range before {
				if rank >= lo && rank <= hi {
					continue
				}
				if after[rank] != m {
					t.Errorf("member %d outside the range [%d, %d] changed: %+v, then %+v", m.taskID, lo, hi, m, after[rank])
				}
			}

			wantWrites := hi - lo + 1
			wantPrepares := 1
			if lo == hi {
				wantWrites, wantPrepares = 0, 0
			}
			if got := counter.executed(parkPositionSQL); got != wantWrites {
				t.Errorf("the move parked %d rows, want %d (the affected range only)", got, wantWrites)
			}
			if got := counter.executed(assignPositionSQL); got != wantWrites {
				t.Errorf("the move assigned %d rows, want %d (the affected range only)", got, wantWrites)
			}
			if got := counter.prepared(parkPositionSQL); got != wantPrepares {
				t.Errorf("the park statement was prepared %d times, want %d", got, wantPrepares)
			}
			if got := counter.prepared(assignPositionSQL); got != wantPrepares {
				t.Errorf("the assign statement was prepared %d times, want %d", got, wantPrepares)
			}
		})
	}

	// Every invocation wrote its audit entry, the no-op included
	// (SPEC/COMMANDS.md § Audit of the ordering commands).
	var entries int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit WHERE operation = ? AND entity_id = ?`,
		models.OpSprintTaskMovePosition, sprintID).Scan(&entries); err != nil {
		t.Fatalf("counting the move entries: %v", err)
	}
	if entries != len(cases) {
		t.Errorf("%d SPRINT_TASK_MOVE_POSITION entries, want %d", entries, len(cases))
	}
}

// TestReorderWritesOnlyTheMembersThatChange reorders a large sprint by
// exchanging two members, then by naming the current order: the first writes
// exactly the two members, the second writes nothing, and both write their
// audit entry.
func TestReorderWritesOnlyTheMembersThatChange(t *testing.T) {
	database, counter, cleanup := setupCountingDB(t)
	defer cleanup()
	sprintID, ids := seedLargeSprint(t, database, largeSprintSize)

	swapped := slices.Clone(ids)
	swapped[12], swapped[101] = swapped[101], swapped[12]

	counter.reset()
	if err := database.ReorderSprintTasks(sprintID, swapped); err != nil {
		t.Fatalf("reordering: %v", err)
	}
	if got := orderOf(t, database, sprintID); !slices.Equal(got, swapped) {
		t.Fatalf("order after the reorder:\n got %v\nwant %v", got, swapped)
	}
	for _, stmt := range []string{parkPositionSQL, assignPositionSQL} {
		if got := counter.executed(stmt); got != 2 {
			t.Errorf("%q executed %d times, want 2 (the two members that change)", stmt, got)
		}
		if got := counter.prepared(stmt); got != 1 {
			t.Errorf("%q prepared %d times, want once", stmt, got)
		}
	}

	counter.reset()
	if err := database.ReorderSprintTasks(sprintID, swapped); err != nil {
		t.Fatalf("reordering to the current order: %v", err)
	}
	if got := orderOf(t, database, sprintID); !slices.Equal(got, swapped) {
		t.Fatalf("a reorder to the current order changed it:\n got %v\nwant %v", got, swapped)
	}
	for _, stmt := range []string{parkPositionSQL, assignPositionSQL} {
		if got := counter.executed(stmt) + counter.prepared(stmt); got != 0 {
			t.Errorf("a reorder to the current order touched %q %d times, want none", stmt, got)
		}
	}

	var entries int
	if err := database.QueryRow(`SELECT COUNT(*) FROM audit WHERE operation = ? AND entity_id = ?`,
		models.OpSprintReorderTasks, sprintID).Scan(&entries); err != nil {
		t.Fatalf("counting the reorder entries: %v", err)
	}
	if entries != 2 {
		t.Errorf("%d SPRINT_REORDER_TASKS entries, want 2", entries)
	}
}

// TestMoveToRankChanges pins the permutation moveToRankChanges computes on a
// sparse run, where positions are not ranks: the range receives the values it
// held, and nothing outside it is touched.
func TestMoveToRankChanges(t *testing.T) {
	members := []sprintMember{{11, 0}, {12, 2}, {13, 5}, {14, 6}, {15, 9}}

	got := moveToRankChanges(members, 12, 3)
	want := []positionAssignment{{13, 2}, {14, 5}, {12, 6}}
	if !slices.Equal(got, want) {
		t.Errorf("moving 12 down to rank 3 = %v, want %v", got, want)
	}

	got = moveToRankChanges(members, 15, 1)
	want = []positionAssignment{{15, 2}, {12, 5}, {13, 6}, {14, 9}}
	if !slices.Equal(got, want) {
		t.Errorf("moving 15 up to rank 1 = %v, want %v", got, want)
	}

	if got := moveToRankChanges(members, 13, 2); got != nil {
		t.Errorf("moving 13 to its own rank = %v, want no write", got)
	}
	if got := moveToRankChanges(members, 99, 0); got != nil {
		t.Errorf("moving a non-member = %v, want no write", got)
	}
}
