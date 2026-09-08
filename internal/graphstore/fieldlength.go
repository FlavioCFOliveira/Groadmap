// Package graphstore — the engine's two field-length refusals, classified.
//
// A statement's labels, property keys and property values go into TWO durable
// formats, and each bounds how long a field it will carry. The write-ahead log
// bounds one at commit, by the capacity of the length prefix its frame reserves;
// the snapshot bounds one at checkpoint, by what its own reader is required to
// accept. The bounds are the engine's, they are set independently, and neither is
// uniformly the stricter (SPEC/GRAPH.md § Field Length Limits).
//
// This file holds the two predicates that say WHICH format refused, and the one
// wording of the consequence a refused checkpoint carries. It holds no wording
// for a refused COMMIT: that one reaches the caller as a published error line,
// and the line belongs to the surface that publishes it
// (SPEC/COMMANDS.md § Graph Management).
//
// # Why the predicates live here rather than at their call sites
//
// Because the question they answer is this package's. graphstore is what opens
// the write-ahead log and what writes the snapshot, so "which of the two durable
// formats refused this field" is a question about the seam it owns, and an
// answer written at a call site would be a second opinion about a file this
// package is the one to have opened.
//
// It also stops the one mistake the specification names twice: recognising the
// condition by matching the engine's message text. The engine's wording is the
// engine's to reword, a match on it fails silently at the next version bump, and
// the whole point of a sentinel is that it survives the wording
// (SPEC/GRAPH.md § Field Length Limits, rule 3). A predicate named for the
// condition, with the errors.Is inside it, is what a caller reaches for instead.
package graphstore

import (
	"errors"

	"github.com/FlavioCFOliveira/GoGraph/store/snapshot"
	"github.com/FlavioCFOliveira/GoGraph/store/txn"
)

// CommitRefusedFieldTooLong reports whether err is the write-ahead log refusing a
// field the statement writes as longer than its length prefix can express.
//
// It is the COMMIT half of SPEC/GRAPH.md § Field Length Limits. The engine wraps
// store/txn.ErrFieldTooLong around every such refusal and formats it with the
// field kind and both figures; nothing was written when it fires — the refused
// transaction consumes a sequence number and applies nothing, so the graph holds
// no part of the statement, not even the elements it created before the over-long
// field (rule 5).
//
// Two neighbouring refusals are deliberately OUTSIDE what this reports, and the
// specification requires that they stay outside (rule 10). A node key over the
// log's unsigned 32-bit prefix is refused by the engine's node-key codec, which
// wraps no sentinel at all, and an assembled frame over the log's frame ceiling —
// which one list property of individually-legal elements can reach — is refused
// under store/wal.ErrFrameTooLarge. Both are length refusals in spirit; neither
// is this class, because the class is the sentinel a caller matches and not the
// shape of the complaint. Matching the sentinel and nothing else is what keeps
// them out, which is why this function has no second clause.
func CommitRefusedFieldTooLong(err error) bool {
	return errors.Is(err, txn.ErrFieldTooLong)
}

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
// The sentinel is the snapshot package's, and it is NOT the same value
// CommitRefusedFieldTooLong matches even though the two share a name in their own
// packages. Confusing them would report a commit-time refusal as an unhealable
// checkpoint condition, and an unhealable checkpoint condition as a statement to
// re-run, so each predicate matches exactly one.
func CheckpointRefusedFieldTooLong(err error) bool {
	return errors.Is(err, snapshot.ErrFieldTooLong)
}

// FieldTooLongCheckpointDiagnostic words a checkpoint the snapshot format refused
// for an over-long committed field, for the surface that names its own
// checkpoint.
//
// checkpoint is the subject of the sentence — "the graph checkpoint", "the graph
// server's shutdown checkpoint" — and it is a parameter rather than a fixed
// phrase because the surfaces that report this do not share one name for the
// thing that failed, and one of them shares a stderr stream with a DIFFERENT
// checkpoint whose report must stay distinguishable from this one
// (internal/graphserve's in-flight watch). What they do share is everything after
// the subject, which is what this function exists to keep single.
//
// # The four things it must say, and why each
//
// SPEC/GRAPH.md § Field Length Limits, rule 9, fixes the content and deliberately
// publishes no literal for it: this diagnostic accompanies a SUCCESSFUL
// invocation — exit code 0 on the short-lived surfaces, a log record on the
// server — so it is not an error line and does not belong in the tables
// SPEC/COMMANDS.md § Published Error Strings Are Exact governs. What is fixed is
// the content, and it is these four:
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
