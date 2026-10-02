// Regression fence for the shutdown checkpoint's diagnostic (rmp task #413).
//
// Since rmp task #383 Groadmap holds the error of BOTH of the server's
// checkpoints — the shutdown checkpoint and the in-flight fold it now requests
// itself — so both classify an over-long field with errors.Is (SPEC/GRAPH.md
// § Field Length Limits, rule 8). The last sub-test below keeps the two reports
// apart: they share a stderr stream and the wording after their subjects.
package graphserve

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/store/snapshot"
	"github.com/FlavioCFOliveira/Groadmap/internal/graphstore"
)

func TestShutdownCheckpointMessage(t *testing.T) {
	t.Run("an ordinary failure keeps the general message", func(t *testing.T) {
		got := shutdownCheckpointMessage(errors.New("snapshot write: input/output error"))
		for _, fragment := range []string{
			"the graph server's shutdown checkpoint failed",
			"still durable in the write-ahead log",
			"was not folded into the snapshot",
		} {
			if !strings.Contains(got, fragment) {
				t.Errorf("the general message lost %q:\n%s", fragment, got)
			}
		}
	})

	t.Run("a refused field selects the condition that cannot heal", func(t *testing.T) {
		err := fmt.Errorf("capture: %w: property value is 2147483648 bytes, maximum 1073741824",
			snapshot.ErrFieldTooLong)
		got := shutdownCheckpointMessage(err)

		want := graphstore.FieldTooLongCheckpointDiagnostic("the graph server's shutdown checkpoint")
		if got != want {
			t.Errorf("the server does not use the one shared wording\n got:  %q\n want: %q", got, want)
		}
		if strings.Contains(got, "shutdown checkpoint failed") {
			t.Error("the unhealable condition was reported as the general failure")
		}
	})

	t.Run("both checkpoints name themselves", func(t *testing.T) {
		// The in-flight fold reports on the same stderr stream of the same
		// process, so a reader must be able to tell the two apart. Each branch
		// carries the shutdown subject, and neither may read as the other's.
		for _, got := range []string{
			shutdownCheckpointMessage(errors.New("snapshot write: input/output error")),
			shutdownCheckpointMessage(fmt.Errorf("capture: %w", snapshot.ErrFieldTooLong)),
		} {
			if !strings.Contains(got, "shutdown checkpoint") {
				t.Errorf("the message does not name the shutdown checkpoint:\n%s", got)
			}
			if strings.Contains(got, "in-flight") {
				t.Errorf("the shutdown message reads as the in-flight fold's:\n%s", got)
			}
		}
		// And the in-flight fold's two reports name the in-flight checkpoint,
		// and share the unhealable condition's wording with the shutdown's, so
		// the four things rule 9 requires are said once.
		for _, got := range []string{checkpointFailedMessage, inFlightFieldTooLongMessage()} {
			if !strings.Contains(got, "in-flight graph checkpoint") {
				t.Errorf("the in-flight report does not identify itself as the in-flight one:\n%s", got)
			}
		}
		if want := graphstore.FieldTooLongCheckpointDiagnostic("an in-flight graph checkpoint"); inFlightFieldTooLongMessage() != want {
			t.Errorf("the in-flight fold does not use the one shared wording\n got:  %q\n want: %q",
				inFlightFieldTooLongMessage(), want)
		}
	})
}
