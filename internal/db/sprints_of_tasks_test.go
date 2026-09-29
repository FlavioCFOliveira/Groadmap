package db

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// TestSprintsOfTasksIsSetBasedAndChunked settles SPEC/DATABASE.md § Position
// Density Within a Sprint, "That read is one set-based statement over the whole
// set of task ids ... chunked like every other IN list": tasks in no sprint, in
// one sprint and spread over several, over a set larger than one chunk, are
// resolved to their sprints in ascending order with one statement per chunk.
func TestSprintsOfTasksIsSetBasedAndChunked(t *testing.T) {
	database, counter, cleanup := setupCountingDB(t)
	defer cleanup()
	ctx := testContext()

	const n = 250 // three chunks of at most sprintsOfTasksChunk ids
	ids := make([]int, n)
	for i := range ids {
		ids[i] = newTestTask(t, database, fmt.Sprintf("Harden the webhook retry policy for partner %d", i+1))
	}
	// Created in this order so the ids are not in the order they are asked
	// about: the latest-created sprint holds the earliest tasks.
	late := newTestSprintWithCap(t, database, "Webhook retries, partner wave C", 0)
	early := newTestSprintWithCap(t, database, "Webhook retries, partner wave A", 0)
	middle := newTestSprintWithCap(t, database, "Webhook retries, partner wave B", 0)
	unused := newTestSprintWithCap(t, database, "Webhook retries, partner wave D", 0)

	if err := database.AddTasksToSprint(ctx, late, ids[0:10]); err != nil {
		t.Fatalf("adding to the late sprint: %v", err)
	}
	if err := database.AddTasksToSprint(ctx, early, ids[150:161]); err != nil {
		t.Fatalf("adding to the early sprint: %v", err)
	}
	if err := database.AddTasksToSprint(ctx, middle, ids[249:250]); err != nil {
		t.Fatalf("adding to the middle sprint: %v", err)
	}

	resolve := func(taskIDs []int) []int {
		t.Helper()
		var got []int
		err := database.WithTransaction(func(tx *sql.Tx) error {
			var err error
			got, err = SprintsOfTasksTx(tx, taskIDs)
			return err
		})
		if err != nil {
			t.Fatalf("SprintsOfTasksTx: %v", err)
		}
		return got
	}

	shuffled := slices.Clone(ids)
	rand.New(rand.NewPCG(7, 11)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	query := func(size int) string { return sprintsOfTasksQuery(generatePlaceholders(size)) }

	counter.reset()
	want := []int{late, early, middle}
	slices.Sort(want)
	got := resolve(shuffled)
	if !slices.Equal(got, want) {
		t.Errorf("the sprints of every task = %v, want %v", got, want)
	}
	if slices.Contains(got, unused) {
		t.Errorf("sprint %d has no member, yet it is reported", unused)
	}
	if got := counter.executed(query(100)); got != 2 {
		t.Errorf("%d full-chunk statements for %d ids, want 2", got, n)
	}
	if got := counter.executed(query(50)); got != 1 {
		t.Errorf("%d statements for the last, partial chunk, want 1", got)
	}

	// Tasks in no sprint contribute nothing.
	if got := resolve(ids[20:140]); len(got) != 0 {
		t.Errorf("tasks in no sprint resolve to %v, want none", got)
	}
	// Tasks of one sprint resolve to it once.
	if got := resolve([]int{ids[151], ids[155], ids[30], ids[160]}); !slices.Equal(got, []int{early}) {
		t.Errorf("tasks of one sprint resolve to %v, want [%d]", got, early)
	}
	// No id, no statement.
	counter.reset()
	if got := resolve(nil); got != nil {
		t.Errorf("an empty id set resolves to %v, want nil", got)
	}
	if counter.count() != 0 {
		t.Errorf("an empty id set issued %d statements, want none", counter.count())
	}
}
