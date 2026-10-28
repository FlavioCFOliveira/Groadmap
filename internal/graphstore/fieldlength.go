// Package graphstore — the snapshot format's field-length refusal, classified.
//
// A statement's labels, property keys and property values go into TWO durable
// formats, and each bounds how long a field it will carry: the write-ahead log at
// commit, and the snapshot at checkpoint. The bounds are the engine's, they are
// set independently, and neither is uniformly the stricter (SPEC/GRAPH.md § Field
// Length Limits).
//
// A refusal at commit is the statement's, and it reaches the caller across the
// server's Bolt connection under a failure code of its own, which the shared Bolt
// client recognises (rules 3 and 13); nothing in this package sees it. A refusal
// at checkpoint is this package's to recognise: graphstore is what the server's
// fold passes through, so "the snapshot format refused a committed field" is a
// question about the seam it owns. This file holds the predicate that answers it,
// and the one wording of the consequence.
//
// The predicate matches the engine's sentinel and never the engine's message
// text: the wording is the engine's to reword, and a match on it fails silently at
// the next version bump (rule 3).
package graphstore

import (
	"errors"

	"github.com/FlavioCFOliveira/GoGraph/store/snapshot"
)

// CheckpointRefusedFieldTooLong reports whether err is the snapshot format
// refusing a field the capture must write because its own reader would have to
// refuse it back.
//
// It is the CHECKPOINT half of SPEC/GRAPH.md § Field Length Limits, and it is a
// condition of its own rather than a variant of the first: the field it refuses
// is already COMMITTED graph state, so it is not a statement to correct and
// re-run. Every later capture captures the same state and every later checkpoint
// refuses for the same reason, until a statement removes or shortens the field
// (rule 6).
//
// The sentinel is the snapshot package's, and it is NOT the write-ahead log's
// field-length sentinel even though the two share a name in their own packages.
// Confusing them would report a commit-time refusal as an unhealable checkpoint
// condition, so this predicate matches the snapshot's and nothing else.
func CheckpointRefusedFieldTooLong(err error) bool {
	return errors.Is(err, snapshot.ErrFieldTooLong)
}

// FieldTooLongCheckpointDiagnostic words a checkpoint the snapshot format refused
// for an over-long committed field, for the surface that names its own
// checkpoint.
//
// checkpoint is the subject of the sentence — "the graph server's shutdown
// checkpoint", "an in-flight graph checkpoint" — and it is a parameter rather
// than a fixed phrase because the graph server reports this from both of its
// checkpoints on one stderr stream, and a reader must be able to tell which of
// the two was refused. What the two share is everything after the subject, which
// is what this function exists to keep single.
//
// # The four things it must say, and why each
//
// SPEC/GRAPH.md § Field Length Limits, rule 9, fixes the content and deliberately
// publishes no literal for it: this diagnostic is a log record on the graph
// server's own stderr, and the write whose commit made the fold owed has already
// been acknowledged to its client with its normal result and exit code 0, so it
// is not an error line and does not belong in the tables SPEC/COMMANDS.md
// § Published Error Strings Are Exact governs. What is fixed is the content, and
// it is these four:
//
//  1. That every acknowledged commit is still durable and recovery still restores
//     it. It goes FIRST because that is the question a durability diagnostic
//     raises, and leaving it unanswered would make this read as data loss. The
//     engine's guard fires while the capture is still being assembled, before any
//     snapshot file is written and before the log's prefix is truncated, so
//     nothing acknowledged is at risk (rule 7).
//  2. That the log was not folded, so it keeps growing and the next open replays
//     more of it. That is the real cost, it is unbounded, and it is the reason
//     the condition must be reported rather than absorbed.
//  3. That it will not clear itself. This is the clause that makes the condition
//     its own, and its absence is the defect this wording exists against: every
//     OTHER checkpoint failure may succeed next time, so the general diagnostic
//     is built on an expectation that is false here, and it would tell an
//     operator to wait for a reconciliation that will never come.
//  4. What to do — shorten or remove the offending field with a statement. It is
//     the only action that ends the condition, and it is an action on the GRAPH
//     rather than on the environment, which is what separates it from every other
//     checkpoint failure's remedy.
//
// The engine's own error is not folded into this text and is reported beside it
// by each caller, because the engine's is the half that names WHICH field and by
// how much, and no text written here could know that.
func FieldTooLongCheckpointDiagnostic(checkpoint string) string {
	return checkpoint + " was refused because a committed field is longer than the snapshot " +
		"format accepts; every acknowledged commit is still durable in the write-ahead log and " +
		"the next open recovers it, but the log was not folded into the snapshot, so it keeps " +
		"growing and the next open replays more of it. This does not clear itself: the field is " +
		"committed graph state, so every later checkpoint is refused for the same reason until a " +
		"statement shortens or removes the field the engine names"
}
