// Regression fence for the shutdown checkpoint's diagnostic (rmp task #413).
//
// This is the ONE checkpoint of the server whose error Groadmap holds: the
// in-flight cadence runs on the engine's own loop and exposes its last failure as
// a rendered STRING, which is not an error and which SPEC/GRAPH.md
// § Field Length Limits, rule 8, forbids matching. So the classification exists
// here and nowhere else in this package, and the assertion below that the
// in-flight message is untouched is part of the fence rather than an aside.
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

	t.Run("both branches name THIS checkpoint", func(t *testing.T) {
		// The in-flight watch reports on the same stderr stream of the same
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
				t.Errorf("the shutdown message reads as the in-flight watch's:\n%s", got)
			}
		}
		// And the in-flight watch is deliberately NOT classified: what it can
		// observe is a rendered string, and matching it is what rule 8 forbids
		// (SPEC/GRAPH.md § Field Length Limits, rule 8, states the limitation as
		// a limitation rather than closing it with a text match).
		if !strings.Contains(checkpointFailedMessage, "an in-flight graph checkpoint failed") {
			t.Error("the in-flight message no longer identifies itself as the in-flight one")
		}
		if strings.Contains(checkpointFailedMessage, "committed graph state") {
			t.Error("the in-flight watch now claims the unhealable condition, which it cannot " +
				"observe without matching the engine's text (rule 8)")
		}
	})
}
