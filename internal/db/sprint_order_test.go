package db

import (
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestCreateSprintAutoAssignsSequentialOrder verifies that CreateSprint
// auto-assigns MAX(order_index)+1 when the caller omits an explicit order: the
// first sprint receives 1 and each subsequent sprint the next value
// (SPEC/DATABASE.md § Create Sprint).
func TestCreateSprintAutoAssignsSequentialOrder(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	for i := 1; i <= 3; i++ {
		s := &models.Sprint{
			Status:      models.SprintPending,
			Title:       "Sprint",
			Description: "desc",
			CreatedAt:   utils.NowISO8601(),
		}
		id, err := seedSprint(db, s)
		if err != nil {
			t.Fatalf("seeding sprint #%d: %v", i, err)
		}
		if s.Order != i {
			t.Errorf("auto-assigned order for sprint #%d = %d, want %d", i, s.Order, i)
		}

		got, err := db.GetSprint(ctx, id)
		if err != nil {
			t.Fatalf("GetSprint #%d: %v", id, err)
		}
		if got.Order != i {
			t.Errorf("persisted order for sprint #%d = %d, want %d", id, got.Order, i)
		}
	}
}

// TestCreateSprintExplicitOrderRespected verifies that an explicit positive
// order is used verbatim rather than auto-assigned.
func TestCreateSprintExplicitOrderRespected(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := testContext()

	s := &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Sprint",
		Description: "desc",
		CreatedAt:   utils.NowISO8601(),
		Order:       42,
	}
	id, err := seedSprint(db, s)
	if err != nil {
		t.Fatalf("seeding the sprint: %v", err)
	}
	got, err := db.GetSprint(ctx, id)
	if err != nil {
		t.Fatalf("GetSprint: %v", err)
	}
	if got.Order != 42 {
		t.Errorf("order = %d, want 42", got.Order)
	}

	// A later auto-assigned sprint must continue from MAX+1 = 43.
	s2 := &models.Sprint{Status: models.SprintPending, Title: "S2", Description: "d", CreatedAt: utils.NowISO8601()}
	if _, err := seedSprint(db, s2); err != nil {
		t.Fatalf("seeding the second sprint: %v", err)
	}
	if s2.Order != 43 {
		t.Errorf("auto order after explicit 42 = %d, want 43", s2.Order)
	}
}

// TestCreateSprintDuplicateOrderRejected verifies the two halves of the
// duplicate-order refusal this layer owns: idx_sprints_order rejects the second
// insert, and IsUniqueConstraintErr recognises the refusal.
//
// Those are exactly what `sprint create` depends on to answer exit code 5 — it
// calls IsUniqueConstraintErr on the insert error and re-dresses it as
// utils.ErrAlreadyExists (SPEC/DATABASE.md § Create Sprint). The exit code
// itself is asserted where it is produced: against the binary, in
// tests/test_43_sprint_order_field.py. Asserting it here would have required
// the insert helper to carry the command's error mapping, which is how the db
// layer grew the duplicate write methods this package has just retired.
func TestCreateSprintDuplicateOrderRejected(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	first := &models.Sprint{Status: models.SprintPending, Title: "A", Description: "d", CreatedAt: utils.NowISO8601(), Order: 7}
	if _, err := seedSprint(db, first); err != nil {
		t.Fatalf("creating the first sprint: %v", err)
	}

	dup := &models.Sprint{Status: models.SprintPending, Title: "B", Description: "d", CreatedAt: utils.NowISO8601(), Order: 7}
	_, err := seedSprint(db, dup)
	if err == nil {
		t.Fatal("expected duplicate-order create to fail, got nil")
	}
	if !IsUniqueConstraintErr(err) {
		t.Errorf("duplicate order error = %v, want one IsUniqueConstraintErr recognises", err)
	}
}

// TestMigrateV1_7_0_toV1_8_0Backfill is the regression gate for the order_index
// migration: it must add the column, backfill a unique, positive, deterministic
// 1..N sequence ordered by created_at ASC then id ASC, create the unique index,
// and be idempotent (SPEC/VERSION.md § Migration 1.7.0 → 1.8.0).
func TestMigrateV1_7_0_toV1_8_0Backfill(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	// Build a pre-1.8.0 sprints table (no order_index) and seed rows whose
	// created_at order differs from id order, so the backfill ordering is tested.
	if _, err := sqlDB.Exec(`CREATE TABLE sprints (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		status TEXT NOT NULL DEFAULT 'PENDING',
		title TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL,
		created_at TEXT NOT NULL,
		started_at TEXT,
		closed_at TEXT,
		max_tasks INTEGER
	)`); err != nil {
		t.Fatalf("create legacy sprints: %v", err)
	}

	// id 1 created latest, id 2 earliest, id 3 middle → expected order: 2,3,1.
	seed := []struct {
		id        int
		createdAt string
	}{
		{1, "2026-03-03T00:00:00.000Z"},
		{2, "2026-03-01T00:00:00.000Z"},
		{3, "2026-03-02T00:00:00.000Z"},
	}
	for _, s := range seed {
		if _, err := sqlDB.Exec(
			"INSERT INTO sprints (id, status, description, created_at) VALUES (?, 'PENDING', 'd', ?)",
			s.id, s.createdAt,
		); err != nil {
			t.Fatalf("seed sprint %d: %v", s.id, err)
		}
	}

	applyMigration := func() {
		tx, err := sqlDB.Begin()
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if err := migrateV1_7_0_toV1_8_0(tx); err != nil {
			tx.Rollback() //nolint:errcheck
			t.Fatalf("migrateV1_7_0_toV1_8_0: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	applyMigration()

	want := map[int]int{2: 1, 3: 2, 1: 3} // id -> order_index
	for id, wantOrder := range want {
		var got int
		if err := sqlDB.QueryRow("SELECT order_index FROM sprints WHERE id = ?", id).Scan(&got); err != nil {
			t.Fatalf("read order_index for id %d: %v", id, err)
		}
		if got != wantOrder {
			t.Errorf("backfilled order_index for id %d = %d, want %d", id, got, wantOrder)
		}
	}

	// Uniqueness: the unique index must reject a duplicate order_index.
	if _, err := sqlDB.Exec(
		"INSERT INTO sprints (status, description, created_at, order_index) VALUES ('PENDING', 'd', '2026-03-04T00:00:00.000Z', 1)",
	); err == nil {
		t.Error("expected duplicate order_index insert to be rejected by idx_sprints_order, got nil")
	}

	// Idempotent: a second application must not error and must preserve the values.
	applyMigration()
	var orderForID2 int
	if err := sqlDB.QueryRow("SELECT order_index FROM sprints WHERE id = 2").Scan(&orderForID2); err != nil {
		t.Fatalf("re-read order_index: %v", err)
	}
	if orderForID2 != 1 {
		t.Errorf("order_index for id 2 after idempotent re-run = %d, want 1", orderForID2)
	}
}

// ============================================================================
// rmp task #281: the listing order of `rmp sprint list`
// ============================================================================
//
// SPEC/COMMANDS.md § List Sprints, Result Ordering, publishes the order of this
// command: `order` ASCENDING, lowest first — the roadmap's planned execution
// order. It is a guarantee a caller may rely on, not an incidental property of
// the query that happens to produce the result.
//
// The defect these gates exist for: ListSprints ordered by `created_at DESC` and
// ignored `order_index` entirely — the one column a sprint carries to express
// execution sequence, and the one the web sprints page already honours. A
// roadmap planned 1, 2, 3, 4 and created in that sequence was handed back
// 4, 3, 2, 1: the plan exactly reversed.
//
// The fixtures below are built so that creation order and `order` value
// deliberately DISAGREE. That is what makes the assertions falsifiable: if the
// two agreed, an `ORDER BY created_at ASC`, an `ORDER BY id`, and no ORDER BY at
// all would every one of them satisfy the expectation, and the test would prove
// nothing. Each fixture is checked against the alternatives in a comment naming
// the sequence that alternative would produce.
//
// `created_at` is set explicitly to distinct, increasing values rather than left
// to a clock: the helper timestamps have millisecond resolution, so sprints
// created inside one loop can share a timestamp and the ordering the defect used
// would be indeterminate rather than wrong in a fixed way. Distinct values make
// the reverted behaviour deterministic, so the fail-then-pass proof is exact.

// plannedSprint is one fixture row: the title, the execution order it is planned
// at, and the status it is created in.
type plannedSprint struct {
	title  string
	order  int
	status models.SprintStatus
}

// seedPlannedSprints creates the fixture sprints in slice order — which is the
// creation order — stamping each with a distinct, increasing created_at so the
// creation sequence is total and observable. It returns the sprint ids, indexed
// as the input slice.
func seedPlannedSprints(t *testing.T, db *DB, planned []plannedSprint) []int {
	t.Helper()

	// A fixed base instant keeps the fixture independent of the wall clock; the
	// one-minute step is far wider than the format's millisecond resolution, so
	// no two rows can collide.
	base := time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC)

	ids := make([]int, len(planned))
	for i := range planned {
		sprint := &models.Sprint{
			Status:      planned[i].status,
			Title:       planned[i].title,
			Description: planned[i].title + " — planned work for this sprint.",
			CreatedAt:   utils.FormatISO8601(base.Add(time.Duration(i) * time.Minute)),
			Order:       planned[i].order,
		}
		id, err := seedSprint(db, sprint)
		if err != nil {
			t.Fatalf("creating fixture sprint %q at order %d: %v",
				planned[i].title, planned[i].order, err)
		}
		ids[i] = id
	}
	return ids
}

// listedOrders runs the production listing and returns the `order` value of each
// sprint in the sequence the listing returned them, which is the property under
// test.
func listedOrders(t *testing.T, db *DB, status *models.SprintStatus) []int {
	t.Helper()

	sprints, err := db.ListSprints(testContext(), status)
	if err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	orders := make([]int, len(sprints))
	for i := range sprints {
		orders[i] = sprints[i].Order
	}
	return orders
}

// listedIDs runs the production listing and returns the ids in returned
// sequence.
func listedIDs(t *testing.T, db *DB, status *models.SprintStatus) []int {
	t.Helper()

	sprints, err := db.ListSprints(testContext(), status)
	if err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	ids := make([]int, len(sprints))
	for i := range sprints {
		ids[i] = sprints[i].ID
	}
	return ids
}

// TestSprintListingReturnsThePlannedOrder is the regression gate for rmp task
// #281: the listing returns sprints by `order` ascending — lowest first, the
// roadmap's planned execution order (SPEC/COMMANDS.md § List Sprints, Result
// Ordering).
//
// The fixture is built so creation order and `order` disagree. Creating at
// orders 3, 1, 4, 2 (ids 1..4 in that creation sequence) separates the specified
// order from every alternative a listing could plausibly use:
//
//	ORDER BY order_index ASC  -> 1, 2, 3, 4  (specified; ids 2, 4, 1, 3)
//	ORDER BY created_at DESC  -> 2, 4, 1, 3  (the defect; ids 4, 3, 2, 1)
//	ORDER BY created_at ASC   -> 3, 1, 4, 2  (ids 1, 2, 3, 4)
//	ORDER BY id / no ORDER BY -> 3, 1, 4, 2  (rowid order)
//
// No two of those four sequences are equal, so the assertion cannot pass by
// accident: removing the clause, reversing it, or ordering by any of the other
// candidate columns all produce a sequence this test rejects.
func TestSprintListingReturnsThePlannedOrder(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	planned := []plannedSprint{
		{"Settlement reconciliation", 3, models.SprintPending},
		{"Authentication hardening", 1, models.SprintPending},
		{"Observability rollout", 4, models.SprintPending},
		{"Ledger archival", 2, models.SprintPending},
	}
	ids := seedPlannedSprints(t, db, planned)

	// The fixture is only useful if creation order really does disagree with the
	// planned order. Asserting that here means a later edit that quietly makes
	// them agree fails loudly instead of silently making every assertion below
	// vacuous.
	creationOrders := make([]int, len(planned))
	for i := range planned {
		creationOrders[i] = planned[i].order
	}
	if slices.IsSorted(creationOrders) {
		t.Fatalf("the fixture creates sprints in ascending planned order %v, so creation order and "+
			"planned order agree and the assertions below would hold for orderings this test "+
			"exists to reject", creationOrders)
	}

	// The listing, by the value the SPEC names.
	gotOrders := listedOrders(t, db, nil)
	wantOrders := []int{1, 2, 3, 4}
	if !slices.Equal(gotOrders, wantOrders) {
		t.Errorf("sprint listing returned orders %v, want %v (ascending `order`, the planned "+
			"execution order; SPEC/COMMANDS.md § List Sprints, Result Ordering)",
			gotOrders, wantOrders)
	}

	// The same statement expressed over identities, so a future change that kept
	// the order values ascending while returning the wrong rows is caught too.
	gotIDs := listedIDs(t, db, nil)
	wantIDs := []int{ids[1], ids[3], ids[0], ids[2]}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Errorf("sprint listing returned ids %v, want %v (the sprints at orders 1, 2, 3, 4)",
			gotIDs, wantIDs)
	}

	// The sequence the defect produced, named explicitly. It must NOT be what the
	// listing returns; asserting against it directly documents the regression this
	// gate exists for and fails the moment `created_at DESC` comes back.
	defectOrders := []int{2, 4, 1, 3}
	if slices.Equal(gotOrders, defectOrders) {
		t.Errorf("sprint listing returned %v, which is the creation-time-descending sequence of "+
			"rmp task #281 — the roadmap's plan handed back in the wrong sequence", defectOrders)
	}

	// Repeating the read over unchanged data returns the same sequence: the
	// ordering is total, so the result is deterministic and no tie-break is
	// needed (SPEC/COMMANDS.md § List Sprints).
	for read := 2; read <= 4; read++ {
		if again := listedOrders(t, db, nil); !slices.Equal(again, gotOrders) {
			t.Errorf("read %d of unchanged data returned %v, want %v; the ordering is not total",
				read, again, gotOrders)
		}
	}
}

// TestSprintListingStatusFilterNarrowsWithoutReordering verifies that --status
// selects WHICH sprints the result contains and never changes the sequence of
// the ones it keeps: a filtered listing is the unfiltered listing with the
// excluded entries removed, and nothing else (SPEC/COMMANDS.md § List Sprints,
// "The --status filter narrows the result; it never reorders it").
//
// The fixture interleaves statuses against the planned order so that, within
// EVERY status group, creation order and `order` still disagree — otherwise the
// filtered assertions would hold for a listing that reordered on filtering:
//
//	PENDING ids 1(order 5), 3(order 6)            -> planned 1, 3 / creation-desc 3, 1
//	CLOSED  ids 2(order 2), 4(order 1), 6(order 3) -> planned 4, 2, 6 / creation-desc 6, 4, 2
//
// The relation is asserted structurally — the filtered ids must equal the
// unfiltered ids restricted to that status — so the test states the property
// itself rather than a second hand-written copy of the expected sequence, which
// could go stale independently.
func TestSprintListingStatusFilterNarrowsWithoutReordering(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	planned := []plannedSprint{
		{"Acquirer feed migration", 5, models.SprintPending},
		{"Settlement reconciliation", 2, models.SprintClosed},
		{"Ledger archival", 6, models.SprintPending},
		{"Authentication hardening", 1, models.SprintClosed},
		{"Observability rollout", 4, models.SprintOpen},
		{"Chargeback automation", 3, models.SprintClosed},
	}
	seedPlannedSprints(t, db, planned)

	// The unfiltered listing, which the filtered ones are measured against.
	unfiltered, err := db.ListSprints(testContext(), nil)
	if err != nil {
		t.Fatalf("unfiltered ListSprints: %v", err)
	}
	if got := len(unfiltered); got != len(planned) {
		t.Fatalf("unfiltered listing returned %d sprints, want %d", got, len(planned))
	}
	gotOrders := make([]int, len(unfiltered))
	for i := range unfiltered {
		gotOrders[i] = unfiltered[i].Order
	}
	if want := []int{1, 2, 3, 4, 5, 6}; !slices.Equal(gotOrders, want) {
		t.Fatalf("unfiltered listing returned orders %v, want %v; the filter assertions below "+
			"are measured against this sequence and mean nothing until it is right",
			gotOrders, want)
	}

	for _, status := range []models.SprintStatus{
		models.SprintPending, models.SprintOpen, models.SprintClosed,
	} {
		// What the unfiltered listing holds for this status, in the sequence the
		// unfiltered listing holds it.
		var wantIDs []int
		for i := range unfiltered {
			if unfiltered[i].Status == status {
				wantIDs = append(wantIDs, unfiltered[i].ID)
			}
		}
		if len(wantIDs) == 0 {
			t.Fatalf("the fixture holds no %s sprint, so the filter assertion for it is vacuous",
				status)
		}

		gotIDs := listedIDs(t, db, sprintStatusPtr(status))
		if !slices.Equal(gotIDs, wantIDs) {
			t.Errorf("--status %s returned ids %v, want %v: the filter must narrow the result "+
				"to the sprints of that status and leave their relative sequence untouched",
				status, gotIDs, wantIDs)
		}

		// The filtered sequence is ascending by `order` for the same reason the
		// unfiltered one is: filtering removes entries and changes nothing else.
		filteredOrders := listedOrders(t, db, sprintStatusPtr(status))
		if !slices.IsSorted(filteredOrders) {
			t.Errorf("--status %s returned orders %v, which is not ascending; the filter "+
				"reordered the result", status, filteredOrders)
		}
	}
}

// TestSprintOrderingIsTotalByConstruction pins the schema facts the published
// ordering rests on. SPEC/COMMANDS.md § List Sprints states NO tie-break rule,
// and is entitled to only because no tie can occur: `order_index` is NOT NULL
// and unique across the roadmap, so ordering by it alone places every sprint at
// exactly one position.
//
// Without this gate, a future migration could drop the unique index or relax the
// NOT NULL and leave the listing silently non-deterministic — the ordering would
// still look right on any fixture that happened to have distinct values, and the
// published guarantee would quietly stop being true. This asserts the guarantee's
// foundation directly, against the live schema rather than against a fixture.
func TestSprintOrderingIsTotalByConstruction(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// NOT NULL: SQLite's unique index treats NULLs as distinct, so a nullable
	// column would admit many sprints with no position at all and the ordering
	// among them would be undefined.
	var notNull int
	err := db.QueryRowContext(testContext(),
		`SELECT "notnull" FROM pragma_table_info('sprints') WHERE name = 'order_index'`,
	).Scan(&notNull)
	if err != nil {
		t.Fatalf("reading the sprints.order_index column definition: %v", err)
	}
	if notNull != 1 {
		t.Error("sprints.order_index is nullable; a sprint with no position makes the listing " +
			"order non-total, and SPEC/COMMANDS.md § List Sprints specifies no tie-break")
	}

	// Unique: two sprints at one position would tie, and no tie-break is
	// specified because none is supposed to be reachable.
	var uniqueIndexes int
	err = db.QueryRowContext(testContext(), `
		SELECT COUNT(*) FROM pragma_index_list('sprints') il
		WHERE il."unique" = 1
		  AND EXISTS (
		      SELECT 1 FROM pragma_index_info(il.name) ii
		      WHERE ii.name = 'order_index'
		  )`,
	).Scan(&uniqueIndexes)
	if err != nil {
		t.Fatalf("reading the unique indexes over sprints.order_index: %v", err)
	}
	if uniqueIndexes < 1 {
		t.Error("no unique index covers sprints.order_index; two sprints could share a position " +
			"and the published listing order would stop being total")
	}

	// The constraint is not merely declared, it is enforced: a second sprint at a
	// position already taken must be rejected. This is what makes the two reads
	// above a statement about behaviour rather than about pragma output.
	first := &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Acquirer feed migration",
		Description: "Move the acquirer feed onto the new ingest path.",
		CreatedAt:   utils.NowISO8601(),
		Order:       7,
	}
	if _, err := seedSprint(db, first); err != nil {
		t.Fatalf("creating the first sprint at order 7: %v", err)
	}
	duplicate := &models.Sprint{
		Status:      models.SprintPending,
		Title:       "Chargeback automation",
		Description: "Automate the chargeback representment flow.",
		CreatedAt:   utils.NowISO8601(),
		Order:       7,
	}
	if _, err := seedSprint(db, duplicate); err == nil {
		t.Error("a second sprint was accepted at an order already taken; the listing order is " +
			"then not total and the SPEC's no-tie-break claim is false")
	}
}
