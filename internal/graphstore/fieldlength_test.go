// Regression fence for the two field-length predicates and the checkpoint
// diagnostic they select (rmp task #413).
//
// The two sentinels this package matches share a NAME in their own packages —
// store/txn.ErrFieldTooLong and store/snapshot.ErrFieldTooLong — and are
// different values with opposite consequences. Confusing them would report a
// commit-time refusal, where nothing was written and the statement is the thing
// to correct, as an unhealable checkpoint condition, and an unhealable checkpoint
// condition as a statement to re-run. The cross assertions below are the fence
// against exactly that, and they are the reason each predicate is driven with
// BOTH sentinels rather than only its own.
package graphstore

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/GoGraph/store/snapshot"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"
	"github.com/FlavioCFOliveira/GoGraph/store/wal"
)

// TestFieldLengthPredicates drives each predicate with the sentinel it must
// match, the sentinel it must NOT match, and the neighbouring refusals
// SPEC/GRAPH.md § Field Length Limits, rule 10, keeps outside the class.
func TestFieldLengthPredicates(t *testing.T) {
	// The shapes the errors really arrive in: the guards format their own
	// message and the layer above wraps it (cypher/exectx.go and cypher/api.go
	// both wrap a commit failure as "cypher: commit WAL: %w").
	commitRefusal := fmt.Errorf("cypher: commit WAL: %w: label is 65536 bytes, maximum 65535",
		txn.ErrFieldTooLong)
	checkpointRefusal := fmt.Errorf("snapshot write: %w: property value is 2147483648 bytes, maximum 1073741824",
		snapshot.ErrFieldTooLong)

	cases := []struct {
		err            error
		name           string
		wantCommit     bool
		wantCheckpoint bool
	}{
		{name: "the log's refusal", err: commitRefusal, wantCommit: true},
		{name: "the snapshot's refusal", err: checkpointRefusal, wantCheckpoint: true},
		{name: "the bare log sentinel", err: txn.ErrFieldTooLong, wantCommit: true},
		{name: "the bare snapshot sentinel", err: snapshot.ErrFieldTooLong, wantCheckpoint: true},
		{
			name: "an over-large assembled frame",
			err:  fmt.Errorf("cypher: commit WAL: %w", wal.ErrFrameTooLarge),
		},
		{
			name: "an over-long node key, refused by the codec under no sentinel",
			err:  errors.New("stringCodec: key too long: 4294967296 bytes, maximum 4294967295"),
		},
		{
			name: "the engine's own words with no sentinel behind them",
			err: errors.New("cypher: commit WAL: txn: field too long for its WAL length " +
				"prefix: label is 65536 bytes, maximum 65535"),
		},
		{name: "an ordinary parse failure", err: errors.New("cypher: parse: unexpected token")},
		{name: "a full disk", err: errors.New("snapshot write: no space left on device")},
		{name: "no error at all", err: nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CommitRefusedFieldTooLong(c.err); got != c.wantCommit {
				t.Errorf("CommitRefusedFieldTooLong(%v) = %v, want %v", c.err, got, c.wantCommit)
			}
			if got := CheckpointRefusedFieldTooLong(c.err); got != c.wantCheckpoint {
				t.Errorf("CheckpointRefusedFieldTooLong(%v) = %v, want %v", c.err, got, c.wantCheckpoint)
			}
		})
	}

	// The two sentinels are distinct values, which is the whole reason both
	// predicates exist. If a future engine collapsed them into one, every caller
	// of these predicates would start reporting both conditions as one, and this
	// is where that would be caught.
	if errors.Is(txn.ErrFieldTooLong, snapshot.ErrFieldTooLong) ||
		errors.Is(snapshot.ErrFieldTooLong, txn.ErrFieldTooLong) {
		t.Fatal("the engine's two field-length sentinels now match each other; the commit " +
			"refusal and the checkpoint refusal can no longer be told apart")
	}
}

// TestFieldTooLongCheckpointDiagnostic fences the four things
// SPEC/GRAPH.md § Field Length Limits, rule 9, requires the diagnostic to carry.
//
// No literal is published for it — it accompanies a SUCCESSFUL invocation, so it
// is not an error line and does not belong in the tables
// SPEC/COMMANDS.md § Published Error Strings Are Exact governs — so what is
// asserted here is the content and not the wording. Each fragment below stands
// for one of the four; a rewording that keeps the meaning keeps these passing,
// and a rewrite that drops one of the four does not.
func TestFieldTooLongCheckpointDiagnostic(t *testing.T) {
	const subject = "the graph server's shutdown checkpoint"
	got := FieldTooLongCheckpointDiagnostic(subject)

	if !strings.HasPrefix(got, subject) {
		t.Errorf("the diagnostic does not name the checkpoint that failed: %q", got)
	}

	for _, required := range []struct{ why, fragment string }{
		{"every acknowledged commit is still durable", "still durable in the write-ahead log"},
		{"recovery still restores it", "the next open recovers it"},
		{"the log was not folded", "was not folded into the snapshot"},
		{"the log therefore grows", "keeps growing"},
		{"the next open replays more of it", "replays more of it"},
		{"the condition persists through every later checkpoint", "every later checkpoint"},
		{"it is committed state and not a statement to re-run", "committed graph state"},
		{"the remedy is a statement that shortens or removes the field", "shortens or removes the field"},
	} {
		if !strings.Contains(got, required.fragment) {
			t.Errorf("rule 9 requires the diagnostic to say %s; looked for %q in:\n%s",
				required.why, required.fragment, got)
		}
	}

	// The clause that makes this a condition of its own is the one that says it
	// will not clear. A diagnostic that promised a later reconciliation would be
	// telling the operator to wait for something that will never happen, which is
	// the defect this wording exists against (rule 6).
	for _, forbidden := range []string{
		"the next successful checkpoint",
		"reconciles the snapshot",
		"try again",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("the diagnostic promises a reconciliation that cannot come (%q):\n%s",
				forbidden, got)
		}
	}

	// It names no field kind and no figure of its own: the engine's error is
	// reported beside it by every caller and is the only half that knows which
	// field is at fault.
	for _, forbidden := range []string{"65535", "1073741824", "bytes, maximum"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("the diagnostic states a figure of its own (%q), which the engine owns:\n%s",
				forbidden, got)
		}
	}

	// The subject is a parameter because the surfaces do not share one name for
	// the thing that failed, and one of them shares a stream with a different
	// checkpoint whose report must stay distinguishable.
	other := FieldTooLongCheckpointDiagnostic("the graph checkpoint")
	if other == got {
		t.Error("the subject does not reach the diagnostic; two surfaces would report identically")
	}
	if strings.TrimPrefix(other, "the graph checkpoint") != strings.TrimPrefix(got, subject) {
		t.Error("the two subjects do not share one body; the four required things are said twice")
	}
}
