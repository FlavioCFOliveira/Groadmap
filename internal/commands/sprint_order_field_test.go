package commands

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// createSprintForOrderTest creates a sprint via the CLI handler and returns its
// id. Optional extra args (e.g. --order) are appended.
func createSprintForOrderTest(t *testing.T, roadmap, title, desc string, extra ...string) int {
	t.Helper()
	args := append([]string{"create", "-r", roadmap, "-t", title, "-d", desc}, extra...)
	var out string
	out = captureOutput(t, func() {
		if err := HandleSprint(args); err != nil {
			t.Fatalf("sprintCreate(%v) error = %v", args, err)
		}
	})
	return int(parseJSONObject(t, out)["id"].(float64))
}

// TestSprintCreate_AutoAssignsOrder verifies that omitting --order auto-assigns
// MAX(order_index)+1, starting at 1 (SPEC/COMMANDS.md § Create Sprint).
func TestSprintCreate_AutoAssignsOrder(t *testing.T) {
	const roadmap = "testsprintcreateautoorder"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	id1 := createSprintForOrderTest(t, roadmap, "Sprint One", "first")
	id2 := createSprintForOrderTest(t, roadmap, "Sprint Two", "second")

	s1, err := database.GetSprint(context.Background(), id1)
	if err != nil {
		t.Fatalf("GetSprint #1: %v", err)
	}
	s2, err := database.GetSprint(context.Background(), id2)
	if err != nil {
		t.Fatalf("GetSprint #2: %v", err)
	}
	if s1.Order != 1 {
		t.Errorf("first sprint order = %d, want 1", s1.Order)
	}
	if s2.Order != 2 {
		t.Errorf("second sprint order = %d, want 2", s2.Order)
	}
}

// TestSprintCreate_ExplicitOrder verifies that an explicit --order is persisted.
func TestSprintCreate_ExplicitOrder(t *testing.T) {
	const roadmap = "testsprintcreateexplicitorder"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	id := createSprintForOrderTest(t, roadmap, "Sprint", "desc", "--order", "5")
	s, err := database.GetSprint(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if s.Order != 5 {
		t.Errorf("order = %d, want 5", s.Order)
	}
}

// TestSprintCreate_DuplicateOrderExit5 verifies that a colliding --order is
// rejected as ErrAlreadyExists (exit code 5).
func TestSprintCreate_DuplicateOrderExit5(t *testing.T) {
	const roadmap = "testsprintcreatedupeorder"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	_ = createSprintForOrderTest(t, roadmap, "Sprint A", "desc", "--order", "3")

	err := HandleSprint([]string{"create", "-r", roadmap, "-t", "Sprint B", "-d", "desc", "--order", "3"})
	if err == nil {
		t.Fatal("expected duplicate --order to fail, got nil")
	}
	if !errors.Is(err, utils.ErrAlreadyExists) {
		t.Errorf("error = %v, want ErrAlreadyExists (exit 5)", err)
	}
}

// TestSprintCreate_NonPositiveOrderExit6 verifies that --order <= 0 and
// non-integer --order are rejected as ErrValidation (exit code 6).
func TestSprintCreate_NonPositiveOrderExit6(t *testing.T) {
	const roadmap = "testsprintcreatebadorder"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	cases := []string{"0", "-1", "notanumber"}
	for _, v := range cases {
		err := HandleSprint([]string{"create", "-r", roadmap, "-t", "Sprint", "-d", "desc", "--order", v})
		if err == nil {
			t.Errorf("--order %q: expected error, got nil", v)
			continue
		}
		if !errors.Is(err, utils.ErrValidation) {
			t.Errorf("--order %q error = %v, want ErrValidation (exit 6)", v, err)
		}
	}
}

// TestSprintUpdate_OrderAllowedWhenPending verifies that --order can be changed
// while the sprint is PENDING, persists, and emits a SPRINT_UPDATE audit row.
func TestSprintUpdate_OrderAllowedWhenPending(t *testing.T) {
	const roadmap = "testsprintupdateorderpending"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	id := createSprintForOrderTest(t, roadmap, "Sprint", "desc") // order auto-assigned 1

	if err := HandleSprint([]string{"update", "-r", roadmap, strconv.Itoa(id), "--order", "9"}); err != nil {
		t.Fatalf("sprintUpdate --order error = %v", err)
	}

	s, err := database.GetSprint(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if s.Order != 9 {
		t.Errorf("order after update = %d, want 9", s.Order)
	}

	history, err := database.GetEntityHistory(context.Background(), string(models.EntitySprint), id)
	if err != nil {
		t.Fatalf("GetEntityHistory: %v", err)
	}
	var sawUpdate bool
	for i := range history {
		if history[i].Operation == string(models.OpSprintUpdate) {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Errorf("expected a %s audit entry after order change, got %+v", models.OpSprintUpdate, history)
	}
}

// TestSprintUpdate_OrderAllowedWhenOpen verifies that --order can be changed
// while the sprint is OPEN.
func TestSprintUpdate_OrderAllowedWhenOpen(t *testing.T) {
	const roadmap = "testsprintupdateorderopen"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	id := createSprintForOrderTest(t, roadmap, "Sprint", "desc")
	if err := HandleSprint([]string{"start", "-r", roadmap, strconv.Itoa(id)}); err != nil {
		t.Fatalf("sprintStart error = %v", err)
	}

	if err := HandleSprint([]string{"update", "-r", roadmap, strconv.Itoa(id), "--order", "4"}); err != nil {
		t.Fatalf("sprintUpdate --order on OPEN sprint error = %v", err)
	}

	s, err := database.GetSprint(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if s.Order != 4 {
		t.Errorf("order after OPEN update = %d, want 4", s.Order)
	}
}

// TestSprintUpdate_OrderRejectedWhenClosed verifies that changing --order on a
// CLOSED sprint is rejected as ErrValidation (exit code 6) and leaves the value
// unchanged (SPEC/STATE_MACHINE.md § Sprint Order Immutability).
func TestSprintUpdate_OrderRejectedWhenClosed(t *testing.T) {
	const roadmap = "testsprintupdateorderclosed"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	id := createSprintForOrderTest(t, roadmap, "Sprint", "desc") // order 1
	if err := HandleSprint([]string{"start", "-r", roadmap, strconv.Itoa(id)}); err != nil {
		t.Fatalf("sprintStart error = %v", err)
	}
	if err := HandleSprint([]string{"close", "-r", roadmap, strconv.Itoa(id)}); err != nil {
		t.Fatalf("sprintClose error = %v", err)
	}

	err := HandleSprint([]string{"update", "-r", roadmap, strconv.Itoa(id), "--order", "8"})
	if err == nil {
		t.Fatal("expected --order on CLOSED sprint to fail, got nil")
	}
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("error = %v, want ErrValidation (exit 6)", err)
	}

	// The order must be unchanged.
	s, err := database.GetSprint(context.Background(), id)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if s.Order != 1 {
		t.Errorf("order after rejected change = %d, want 1 (unchanged)", s.Order)
	}
}

// TestSprintUpdate_DuplicateOrderExit5 verifies that updating to an order already
// used by another sprint is rejected as ErrAlreadyExists (exit code 5).
func TestSprintUpdate_DuplicateOrderExit5(t *testing.T) {
	const roadmap = "testsprintupdatedupeorder"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	_ = createSprintForOrderTest(t, roadmap, "Sprint A", "desc")    // order 1
	id2 := createSprintForOrderTest(t, roadmap, "Sprint B", "desc") // order 2

	err := HandleSprint([]string{"update", "-r", roadmap, strconv.Itoa(id2), "--order", "1"})
	if err == nil {
		t.Fatal("expected duplicate --order update to fail, got nil")
	}
	if !errors.Is(err, utils.ErrAlreadyExists) {
		t.Errorf("error = %v, want ErrAlreadyExists (exit 5)", err)
	}
}

// TestSprintUpdate_NonPositiveOrderExit6 verifies that --order <= 0 / non-integer
// on update is rejected as ErrValidation (exit code 6).
func TestSprintUpdate_NonPositiveOrderExit6(t *testing.T) {
	const roadmap = "testsprintupdatebadorder"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	id := createSprintForOrderTest(t, roadmap, "Sprint", "desc")

	for _, v := range []string{"0", "-3", "abc"} {
		err := HandleSprint([]string{"update", "-r", roadmap, strconv.Itoa(id), "--order", v})
		if err == nil {
			t.Errorf("--order %q: expected error, got nil", v)
			continue
		}
		if !errors.Is(err, utils.ErrValidation) {
			t.Errorf("--order %q error = %v, want ErrValidation (exit 6)", v, err)
		}
	}
}
