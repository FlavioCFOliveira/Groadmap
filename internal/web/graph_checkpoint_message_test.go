// Regression fence for the graph data endpoint's checkpoint diagnostic
// (rmp task #413).
//
// The endpoint answers 200 whether or not the checkpoint that follows a durable
// commit succeeded, which is the web analogue of the CLI's stderr diagnostic
// beside exit code 0. What this file fences is not the status — that is settled
// elsewhere — but the CHOICE between the two conditions a checkpoint can be in,
// because only one of them clears itself and the operator's next action differs.
package web

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/store/snapshot"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

func TestGraphCheckpointLogMessage(t *testing.T) {
	t.Run("an ordinary failure keeps the general message", func(t *testing.T) {
		err := errors.New("snapshot write: no space left on device")
		if got, want := graphCheckpointLogMessage(err), "graph checkpoint failed"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a commit-time refusal is not a checkpoint condition", func(t *testing.T) {
		// The log's sentinel reaches this endpoint on the STATEMENT, never on the
		// checkpoint, and it must not select the checkpoint wording if it ever
		// arrives here by another route.
		err := fmt.Errorf("cypher: commit WAL: %w: label is 65536 bytes, maximum 65535",
			txn.ErrFieldTooLong)
		if got, want := graphCheckpointLogMessage(err), "graph checkpoint failed"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a refused field selects the condition that cannot heal", func(t *testing.T) {
		err := fmt.Errorf("snapshot write: %w: property value is 2147483648 bytes, maximum 1073741824",
			snapshot.ErrFieldTooLong)
		got := graphCheckpointLogMessage(err)

		want := graphstore.FieldTooLongCheckpointDiagnostic("the graph checkpoint")
		if got != want {
			t.Errorf("the endpoint does not use the one shared wording\n got:  %q\n want: %q", got, want)
		}
		if got == "graph checkpoint failed" {
			t.Error("the unhealable condition was reported as the general failure")
		}
		// The engine's error is NOT folded into the message: it stays the "err"
		// attribute the caller already logs, which is where a reader of
		// structured output looks for which field is at fault.
		if strings.Contains(got, err.Error()) {
			t.Errorf("the engine's diagnostic was folded into the message text: %q", got)
		}
	})
}
