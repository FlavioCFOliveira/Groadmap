package db

import (
	"fmt"
	"reflect"
	"testing"
)

// TestSprintTaskCommentCountsQueryBindsOneParameter is the gate of SPEC/DATABASE.md
// § Count Comments for the Member Tasks of One Sprint (Grouped):
// the statement the sprint page counts its cards' comments with binds exactly
// one parameter, whatever the number of members. The number is SQLite's own: the
// statement's bytecode reads its parameters through Variable instructions, whose
// first operand is the parameter's index, and the driver refuses to run the
// statement with no argument because index 1 is then unbound.
func TestSprintTaskCommentCountsQueryBindsOneParameter(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	rows, err := database.QueryContext(testContext(), "EXPLAIN "+sprintTaskCommentCountsQuery, 1)
	if err != nil {
		t.Fatalf("explaining the sprint comment-count statement: %v", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("reading the EXPLAIN columns: %v", err)
	}
	indexes := map[int64]bool{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scanning an EXPLAIN row: %v", err)
		}
		// The columns are addr, opcode, p1, p2, p3, p4, p5, comment.
		if fmt.Sprint(vals[1]) == "Variable" {
			p1, ok := vals[2].(int64)
			if !ok {
				t.Fatalf("a Variable instruction carries operand %#v, want an integer", vals[2])
			}
			indexes[p1] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the EXPLAIN rows: %v", err)
	}
	if !reflect.DeepEqual(indexes, map[int64]bool{1: true}) {
		t.Errorf("the sprint comment-count statement reads parameters %v, want exactly parameter 1: "+
			"the sprint id, whatever the number of member tasks", indexes)
	}

	unbound, err := database.QueryContext(testContext(), sprintTaskCommentCountsQuery)
	if err == nil {
		_ = unbound.Close() // the failure is already decided; the close cannot change it
		t.Error("the sprint comment-count statement ran with no argument; it must bind the sprint id")
	}
}
