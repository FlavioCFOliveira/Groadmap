package db

import (
	"errors"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestGetSprintBurndown_MissingSprintFailsLikeGetSprint pins the error of the
// burndown's started_at read (SPEC/IMPLEMENTATION.md, Performance Guidelines,
// item 8): for a sprint that does not exist it is utils.ErrNotFound, and its
// text is GetSprint's own, under the burndown's prefix, so `sprint stats`
// reports a missing sprint exactly as it did when the burndown read the whole
// sprint.
func TestGetSprintBurndown_MissingSprintFailsLikeGetSprint(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	const missing = 4242
	_, sprintErr := database.GetSprint(testContext(), missing)
	if sprintErr == nil {
		t.Fatalf("GetSprint(%d) succeeded; the sprint must not exist", missing)
	}
	_, err := database.GetSprintBurndown(testContext(), missing)
	if !errors.Is(err, utils.ErrNotFound) {
		t.Fatalf("GetSprintBurndown(%d) = %v, want utils.ErrNotFound", missing, err)
	}
	if want := "getting sprint for burndown: " + sprintErr.Error(); err.Error() != want {
		t.Errorf("GetSprintBurndown(%d) fails with %q, want %q", missing, err.Error(), want)
	}
}
