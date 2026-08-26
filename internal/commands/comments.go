// Package commands — the comment subcommands, and the input handling they share.
//
// The four comment subcommands of the `task` family and the four of the `sprint`
// family share one input contract (SPEC/COMMANDS.md § Comment Body Input Source
// and Precedence): a positional id, a `--type` drawn from the family's own
// comment-type subset, and a `--body` that falls back to standard input. This
// file holds the part of that contract which does not depend on which family is
// being served, so both families share one implementation instead of carrying a
// copy each: the argument parsing, the standard-input fallback, the validation
// dispatch and its ORDER, the transaction boundary, and the output shaping. What
// is genuinely per-family — the accepted type set, the table, the audit
// operation, the name the parent goes by — travels in a commentFamily value that
// task_comments.go and sprint_comments.go each declare once.
//
// Two rules govern everything here and are the reason the body is handled by
// hand rather than by the generic FlagParser:
//
//  1. `--type` is validated BEFORE the body is resolved, so a missing or invalid
//     type is reported immediately instead of leaving the command blocked on a
//     terminal waiting for a body it would reject anyway. Extraction is
//     therefore purely lexical: extractCommentBody records what it found and
//     decides nothing, and resolveCommentBody runs later, after the type verdict.
//  2. A body that never arrived is a MISSING PARAMETER (exit code 2), not a
//     validation failure (exit code 6). models.ValidateCommentBody reports an
//     empty body as the latter, so this layer decides absence itself, through
//     models.CommentBodyIsAbsent, and keeps the domain's exit-6 empty verdict
//     unreachable from the command line. It decides absence and nothing else:
//     the content rules that must run before that judgement stay in the domain,
//     which is what stops the trim from silently swallowing a leading or
//     trailing VT or FF.
package commands

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The body flag, spelled once. Both forms are accepted wherever a comment body
// is read (SPEC/COMMANDS.md § Add Task Comment).
const (
	commentBodyLong  = "--body"
	commentBodyShort = "-b"
)

// commentIDField is the field the positional id of `comment-edit` and
// `comment-remove` is reported as. Both subcommands take the COMMENT's own id in
// both families, so unlike the parent's field this one is not per-family.
//
// It carries both spellings the subcommands need — `comment_id` for the range
// refusal, `comment` for the format refusal and for the prose around it — so
// neither is written out here.
const commentIDField = utils.FieldCommentID

// commentTypeFlagDefs is the flag set the comment subcommands hand to the shared
// flag parser: `--type` only.
//
// `--body` is deliberately absent. Its value is consumed by extractCommentBody
// before the parser runs, because the parser refuses a valueless flag with its
// own "requires a value" message, while the SPEC pins "no comment body supplied"
// for that case and pins it as part of body resolution, which happens after the
// type has been validated.
var commentTypeFlagDefs = []FlagDef{
	{Name: "--type", Short: "-y", Field: "Type", Type: "string"},
}

// commentBody is what extractCommentBody found on the command line. It is a
// record of the argument list, not a verdict about it: present says the flag
// appeared, valueMissing says it appeared without a usable value token, and the
// meaning of both is decided later by resolveCommentBody.
type commentBody struct {
	value        string
	present      bool
	valueMissing bool
}

// errNoCommentBody is the refusal SPEC/COMMANDS.md pins when no body reached the
// command: `--body` present without a usable value, or `--body` absent with
// nothing usable on standard input under `comment-add`.
//
// It maps to exit code 2 (utils.ErrRequired). The domain reports an empty body as
// a validation error (exit code 6) instead, which is why emptiness is decided in
// this layer and never delegated to models.ValidateCommentBody.
func errNoCommentBody() error {
	return fmt.Errorf("%w: no comment body supplied", utils.ErrRequired)
}

// errNoCommentChange is the refusal SPEC/COMMANDS.md pins for `comment-edit`
// when nothing at all was requested: no `--type`, no `--body`, and no body on
// standard input. Exit code 2.
func errNoCommentChange() error {
	return fmt.Errorf("%w: at least one of --type or --body is required", utils.ErrRequired)
}

// requireCommentPositionalID parses the positional id of a comment subcommand and
// returns it with the arguments that follow it.
//
// field names what the id identifies, so the pinned messages name the right
// thing: utils.FieldTaskID / utils.FieldSprintID for the parent id `comment-add`
// and `comment-list` take, utils.FieldCommentID for the comment's own id
// `comment-edit` and `comment-remove` take.
//
// Every malformed id — non-numeric, non-positive, or beyond MaxInt32 — is exit
// code 2 here. utils.ValidateIDString classifies a non-positive or oversized
// value as a validation error (exit code 6); SPEC/COMMANDS.md pins exit code 2
// for the whole "positive integer" constraint on these subcommands, so the range
// class is supplied to the parser instead.
//
// It used to be re-classified by REBUILDING the message, and the rebuilt message
// drifted: these four subcommands announced an out-of-range id as
// `invalid comment ID: "0" (must be a positive integer no greater than
// 2147483647)` while every other surface announced the same verdict in one of two
// other sentences, and SPEC published neither of the comment ones. Handing the
// class to utils.ValidateIDStringAs keeps the exit code these subcommands publish
// and takes the sentence from the rule (rmp task 330).
func requireCommentPositionalID(args []string, field utils.RangedField) (int, []string, error) {
	if len(args) == 0 {
		return 0, nil, fmt.Errorf("%w: %s ID required", utils.ErrRequired, field.IDEntity())
	}

	id, err := utils.ValidateIDStringAs(args[0], field, utils.ErrInvalidInput)
	if err != nil {
		return 0, nil, err
	}
	return id, args[1:], nil
}

// extractCommentBody removes `-b` / `--body` and its value from args and reports
// what it found, without judging it.
//
// A value token is missing when there is no following token at all or when the
// following token is itself a flag: the command must neither swallow that flag
// as the body nor fall back to standard input (SPEC/COMMANDS.md § Comment Body
// Input Source and Precedence rule 4). isFlagLike draws the line where the graph
// subcommands draw it for `--query`, so "-1" is a body and "-y" is a flag.
//
// The inline forms `--body=<text>` and `-b=<text>` are accepted, matching the
// GNU-style splitting the shared flag parser applies to every other flag. A
// repeated flag follows the parser's rule too: the last occurrence wins, in full,
// so a valueless earlier occurrence does not poison a later valid one.
func extractCommentBody(args []string) ([]string, commentBody) {
	rest := make([]string, 0, len(args))
	var body commentBody

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == commentBodyLong || arg == commentBodyShort:
			body = commentBody{present: true}
			if i+1 >= len(args) || isFlagLike(args[i+1]) {
				body.valueMissing = true
				continue
			}
			body.value = args[i+1]
			i++

		case strings.HasPrefix(arg, commentBodyLong+"="), strings.HasPrefix(arg, commentBodyShort+"="):
			_, inline, _ := strings.Cut(arg, "=")
			body = commentBody{value: inline, present: true}

		default:
			rest = append(rest, arg)
		}
	}

	return rest, body
}

// resolveCommentBody decides the body text a comment subcommand was given. It
// returns the raw text (untrimmed, so the control-character rule still sees the
// input as supplied) and whether a body was supplied at all.
//
// The flag wins over standard input (precedence rule 1). Standard input is read
// only when the flag is absent AND stdinFallback is true — false on
// `comment-edit` when `--type` is present, which is what stops a type-only edit
// from blocking on a terminal (rule 2).
//
// A flag present but unusable — no value token, or a value that is empty or
// whitespace only — is an error in both subcommands and never a silent fallback
// to standard input (rule 4). An ABSENT body means different things to the two
// subcommands, so it is reported as (_, false, nil) and the caller supplies the
// pinned message: "no comment body supplied" on `comment-add`, "at least one of
// --type or --body is required" on `comment-edit`.
//
// Whether a body is absent is asked of models.CommentBodyIsAbsent rather than of
// models.NormalizeCommentBody, and on the value AS SUPPLIED, on both input paths
// alike. The difference is the ORDER SPEC/MODELS.md § Free-Text Emptiness and
// Trimming Constraint fixes: a body made only of VT or FF trims away to nothing,
// so asking the trim alone reported it as a body that never arrived and
// discarded the forbidden character in silence (CWE-150). Asking the domain
// makes the content rules answer first, so such a body is refused as a control
// character with exit code 6 and only genuinely empty whitespace reaches the
// missing-parameter verdict.
func resolveCommentBody(body commentBody, stdinFallback bool) (string, bool, error) {
	if body.present {
		if body.valueMissing {
			return "", false, errNoCommentBody()
		}
		absent, err := models.CommentBodyIsAbsent(body.value)
		if err != nil {
			return "", false, err
		}
		if absent {
			return "", false, errNoCommentBody()
		}
		return body.value, true, nil
	}

	if !stdinFallback {
		return "", false, nil
	}

	raw, err := readCommentBodyStdin()
	if err != nil {
		return "", false, err
	}
	absent, err := models.CommentBodyIsAbsent(raw)
	if err != nil {
		return "", false, err
	}
	if absent {
		return "", false, nil
	}
	return raw, true, nil
}

// readCommentBodyStdin reads the comment body from standard input under a
// BOUNDED memory budget and returns it in the form the body rules produce.
//
// The bound is the point: models.ReadCommentBody applies the length rule as the
// stream arrives and refuses the body the moment it cannot fit, instead of
// buffering everything a writer chooses to send and only then measuring it. The
// value it returns is the canonical stored form, plus the one forbidden
// whitespace rune it carries back when the body held one, and the callers still
// hand it to the domain — models.CommentBodyIsAbsent and then
// models.ValidateCommentBody — which reaches the same verdict on that form as on
// the whole stream. The domain, not this layer, remains the single owner of the
// rules.
//
// A read failure is an I/O failure of the process, not bad user input, so it maps
// to exit code 1 exactly as the graph subcommands' stdin read does.
func readCommentBodyStdin() (string, error) {
	return models.ReadCommentBody(os.Stdin)
}

// parseCommentArgs runs the shared flag parser over what is left of a comment
// subcommand's argument list once the positional id — and, where the subcommand
// accepts one, the body flag — have been consumed, and refuses everything that
// remains.
//
// This is the ONE place the Comment Positional Argument Contract
// (SPEC/COMMANDS.md) is enforced. All eight comment subcommands reach it,
// because the four shared bodies below are the only callers and each family
// supplies data alone: there is no second copy of the rule to drift from this
// one.
//
// Two refusals live here, both exit code 2 (utils.ErrInvalidInput):
//
//   - An unrecognised FLAG, reported by the parser itself. This already worked:
//     the parser refuses a token it cannot match to a definition.
//   - A leftover POSITIONAL argument, reported here. The parser COLLECTS
//     positionals into ParseResult.Args and returns them without complaint, so a
//     caller that ignored that slice silently ignored the extra token — which is
//     what `comment-remove <id> <stray>` did, deleting the comment named by the
//     id and exiting 0 (task #184).
//
// Only the FIRST leftover is named, in command-line order, and the parser
// preserves that order. The refusal is position-independent for the same reason:
// what is refused is whatever remains once the flags have been consumed, not a
// particular slot on the command line.
//
// A leftover beginning with "-" never reaches this check. The parser classifies
// every "-"-prefixed token as a flag, so "-1" here is an unknown flag and not an
// unexpected argument — deliberately unlike the graph family, which reads a
// negative numeric literal as a query value (SPEC/COMMANDS.md § Comment
// Positional Argument Contract rule 5). The one "-"-prefixed token that is a
// value, `--body -1`, is consumed by extractCommentBody before the parser runs.
func parseCommentArgs(defs []FlagDef, args []string) (*ParseResult, error) {
	result, err := NewFlagParser(defs).Parse(args)
	if err != nil {
		return nil, err
	}
	if len(result.Args) > 0 {
		return nil, fmt.Errorf("%w: unexpected argument %q", utils.ErrInvalidInput, result.Args[0])
	}
	return result, nil
}

// parseCommentTypeFlag runs the shared parse over what is left after the
// positional id and the body flag have been consumed, and reports the raw
// `--type` value together with whether the flag was present at all.
//
// The value is returned unparsed: the accepted set depends on the family, so the
// caller applies models.ParseTaskCommentType or models.ParseSprintCommentType.
//
// Every caller invokes this BEFORE validating the type value, before resolving
// the body, and before opening the database, so a malformed argument list is
// refused with exit code 2 ahead of the exit-6 type verdict, ahead of any wait
// on standard input, and ahead of the exit-4 not-found verdict. That order is
// behaviour the SPEC pins, not style.
func parseCommentTypeFlag(args []string) (string, bool, error) {
	result, err := parseCommentArgs(commentTypeFlagDefs, args)
	if err != nil {
		return "", false, err
	}
	raw, ok := result.Flags["Type"].(string)
	return raw, ok, nil
}

// rejectCommentLeftovers refuses any flag AND any positional argument left in
// args. It serves `comment-remove`, which takes no flag beyond the shared
// -r / -h and no positional beyond the comment id, and it goes through
// parseCommentArgs with an empty definition set so both refusals are worded by
// the same code every other subcommand uses rather than by a second copy of it.
func rejectCommentLeftovers(args []string) error {
	_, err := parseCommentArgs(nil, args)
	return err
}

// ==================== THE FOUR SUBCOMMAND BODIES ====================
//
// The `task` and `sprint` comment subcommands differ only in DATA: which type set
// is accepted, which table the row belongs to, which audit operation is written,
// and what the parent entity is called in a message. The steps they take — and
// above all the ORDER of those steps, which SPEC/COMMANDS.md pins as behaviour and
// not as style — are identical, so the steps exist once, here, and each family
// supplies a commentFamily value (SPEC/COMMANDS.md § Sprint Comments: the sprint
// subcommands "mirror the four task comment subcommands exactly").

// commentFamily is the per-family half of a comment subcommand: everything the
// four bodies below need in order to serve one entity, and nothing that is the
// same for both.
type commentFamily struct {
	// parseType applies the family's OWN accepted set — the seven task values or
	// the four sprint values — and produces the exit-6 refusal naming that set. It
	// is what makes HYPOTHESIS, TEST and NOTE accepted values on a task comment and
	// refusals on a sprint comment (SPEC/MODELS.md § Comment Type).
	parseType func(string) (models.CommentType, error)

	// parentExists reports utils.ErrNotFound when the parent entity is absent. Only
	// the verdict is used, never the entity itself, so a family supplies the
	// cheapest read that answers the question.
	parentExists func(context.Context, *db.DB, int) error

	// parentOf resolves a comment by its OWN id and returns the id of the entity it
	// belongs to. Every mutation calls it first: it reports an unknown comment id
	// before anything is written, and it supplies the parent id the audit entry
	// names. The two comment id spaces are per table, so this is also what stops
	// `sprint comment-edit 7` from reaching task_comments and vice versa.
	parentOf func(context.Context, *db.DB, int) (int, error)

	// insert, update and remove are the family's transactional writes. They take
	// the caller's *sql.Tx, so the row and its audit entry commit together.
	insert func(tx *sql.Tx, parentID int, commentType models.CommentType, body, createdAt string) (int, error)
	update func(tx *sql.Tx, commentID int, change *db.CommentUpdate, updatedAt string) error
	remove func(tx *sql.Tx, commentID int) error

	// list returns the family's comment slice, oldest first, optionally filtered by
	// type. The element type is erased because the two slices differ in nothing
	// else and utils.PrintJSON takes any: the value would be boxed at the print
	// call regardless, so erasing it here costs nothing and keeps one body.
	list func(context.Context, *db.DB, int, *models.CommentType) (any, error)

	// The three audit operations of the family (SPEC/DATABASE.md § audit Table).
	opCreate models.AuditOperation
	opUpdate models.AuditOperation
	opDelete models.AuditOperation

	// entityType is the audit entity every mutation is recorded against: TASK or
	// SPRINT, always the PARENT's type — a comment is never an audit entity of its
	// own (SPEC/DATA_FORMATS.md § Audit Entry).
	entityType models.EntityType

	// parentIDField is the field the positional parent id of `comment-add` and
	// `comment-list` is reported as: utils.FieldTaskID / utils.FieldSprintID.
	//
	// It replaces a bare label because it carries BOTH spellings the family
	// needs — `task_id` for the range refusal, `task` for the format refusal and
	// for the not-found prose — so the two can no longer be chosen separately.
	parentIDField utils.RangedField
}

// requireParent reports a missing parent as the exit-4 condition
// SPEC/COMMANDS.md pins for the comment subcommands: "task 42 not found",
// "sprint 7 not found".
//
// The parent readers word their own not-found message differently ("task 42",
// "sprint 7"), so the verdict is re-worded here rather than reused. Any other
// failure is passed through untouched, so a genuine database error stays a
// database error (exit code 1) instead of being reported as a missing parent.
func (f *commentFamily) requireParent(database *db.DB, parentID int) error {
	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	if err := f.parentExists(ctx, database, parentID); err != nil {
		if errors.Is(err, utils.ErrNotFound) {
			return fmt.Errorf("%w: %s %d not found", utils.ErrNotFound, f.parentIDField.IDEntity(), parentID)
		}
		return err
	}
	return nil
}

// logAudit writes the audit entry of one comment mutation. The entity is always
// the PARENT — its type and its id — never the comment's own id and never a new
// entity_type value invented for comments (SPEC/DATA_FORMATS.md § Audit Entry).
func (f *commentFamily) logAudit(tx *sql.Tx, op models.AuditOperation, parentID int, now string) error {
	return db.LogAuditTx(tx, op, f.entityType, parentID, now)
}

// commentAdd adds one comment to the entity named by the positional id.
//
// Usage:
//
//	rmp <family> comment-add -r <roadmap> <parent-id> --type <TYPE> [--body <text>]
//
// The validation order is the one SPEC/COMMANDS.md § Add Task Comment pins, and
// which § Add Sprint Comment adopts unchanged with the sprint in place of the
// task. The order is behaviour, not style: `--type` is checked for presence and
// for value BEFORE the body is resolved, so a missing or invalid type never
// leaves the command waiting on standard input for a body it is going to reject
// anyway.
//
//  1. roadmap                       exit 3
//  2. positional parent id          exit 2
//  3. --type present                exit 2
//  4. --type value, family's set    exit 6
//  5. body from --body or stdin     exit 2
//  6. parent exists                 exit 4
//  7. body length / control chars   exit 6
//  8. INSERT + the family's *_COMMENT_CREATE audit entry, one transaction
//
// Side effects: one row in the family's comment table and one audit entry against
// the PARENT, committed together. Prints {"id": <new-comment-id>} on success.
func commentAdd(f *commentFamily, args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	parentID, rest, err := requireCommentPositionalID(remaining, f.parentIDField)
	if err != nil {
		return err
	}

	// Lexical only: the body is resolved at step 5, after the type verdict.
	rest, body := extractCommentBody(rest)

	typeRaw, typePresent, err := parseCommentTypeFlag(rest)
	if err != nil {
		return err
	}
	if !typePresent {
		return fmt.Errorf("%w: --type", utils.ErrRequired)
	}
	commentType, err := f.parseType(typeRaw)
	if err != nil {
		return err
	}

	// On comment-add no other change is ever possible, so an absent --body always
	// means "read standard input" (SPEC precedence rule 2).
	raw, supplied, err := resolveCommentBody(body, true)
	if err != nil {
		return err
	}
	if !supplied {
		return errNoCommentBody()
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := f.requireParent(database, parentID); err != nil {
		return err
	}

	// The body as supplied, not the trimmed form: strings.TrimSpace strips VT and
	// FF, both forbidden, so trimming before this call would drop them instead of
	// rejecting them. The value to persist is the one this returns.
	stored, err := models.ValidateCommentBody(raw)
	if err != nil {
		return err
	}

	// One timestamp for the row and its audit entry, so the two agree exactly.
	now := utils.NowISO8601()

	var commentID int
	if err := database.WithTransaction(func(tx *sql.Tx) error {
		id, insertErr := f.insert(tx, parentID, commentType, stored, now)
		if insertErr != nil {
			return insertErr
		}
		commentID = id
		return f.logAudit(tx, f.opCreate, parentID, now)
	}); err != nil {
		return err
	}

	return utils.PrintJSON(map[string]int{"id": commentID})
}

// commentList returns one entity's comments, oldest first.
//
// Usage:
//
//	rmp <family> comment-list -r <roadmap> <parent-id> [--type <TYPE>]
//
// The order is the story the log tells, so it is created_at ascending with the
// comment id as the tie-breaker (SPEC/COMMANDS.md § List Task Comments, § List
// Sprint Comments). The result set is unbounded: there is no --limit, no --desc
// and no pagination.
//
// `--type` filters; its value MUST belong to the family's own set, so a value
// valid only on the other entity is rejected with exit code 6 rather than
// silently matching nothing. Writes no audit entry: listing is a read.
func commentList(f *commentFamily, args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	parentID, rest, err := requireCommentPositionalID(remaining, f.parentIDField)
	if err != nil {
		return err
	}

	typeRaw, typePresent, err := parseCommentTypeFlag(rest)
	if err != nil {
		return err
	}
	var filter *models.CommentType
	if typePresent {
		commentType, parseErr := f.parseType(typeRaw)
		if parseErr != nil {
			return parseErr
		}
		filter = &commentType
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := f.requireParent(database, parentID); err != nil {
		return err
	}

	ctx, cancel := db.WithDefaultTimeout()
	defer cancel()

	comments, err := f.list(ctx, database, parentID, filter)
	if err != nil {
		return err
	}

	// An empty log is an empty array, never null: the read layer returns an empty
	// slice for an entity with nothing recorded yet.
	return utils.PrintJSON(comments)
}

// commentEdit changes the type and/or the body of one existing comment.
//
// Usage:
//
//	rmp <family> comment-edit -r <roadmap> <comment-id> [--type <TYPE>] [--body <text>]
//
// The positional argument is the COMMENT's own id, not the id of the entity it
// belongs to, and the two id spaces are independent: an id that exists in the
// other family's table is a not-found condition here.
//
// At least one change is required, so unlike `task edit` this command does not
// succeed as a no-op. A change is requested by a `--type` value, by a `--body`
// value, or by a body arriving on standard input — which is why the flagless form
// `comment-edit <comment-id> < revised.txt` is a valid edit and the decision is
// made only after standard input has been resolved. Standard input is read ONLY
// when `--type` is absent as well, so a type-only edit never waits for input.
//
// The edit replaces the body in place and stamps updated_at; the previous text is
// not retained anywhere. Produces no output on success.
func commentEdit(f *commentFamily, args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	commentID, rest, err := requireCommentPositionalID(remaining, commentIDField)
	if err != nil {
		return err
	}

	rest, body := extractCommentBody(rest)

	typeRaw, typePresent, err := parseCommentTypeFlag(rest)
	if err != nil {
		return err
	}
	var newType *models.CommentType
	if typePresent {
		// Before standard input is considered, so an invalid type cannot leave the
		// command blocked on a terminal.
		commentType, parseErr := f.parseType(typeRaw)
		if parseErr != nil {
			return parseErr
		}
		newType = &commentType
	}

	raw, supplied, err := resolveCommentBody(body, !typePresent)
	if err != nil {
		return err
	}
	if !supplied && newType == nil {
		return errNoCommentChange()
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	// Resolving the comment reports a missing id before any write and supplies the
	// parent id the audit entry is written against.
	parentID, err := f.parentOf(ctx, database, commentID)
	if err != nil {
		return err
	}

	var newBody *string
	if supplied {
		stored, valErr := models.ValidateCommentBody(raw)
		if valErr != nil {
			return valErr
		}
		newBody = &stored
	}

	now := utils.NowISO8601()
	return database.WithTransaction(func(tx *sql.Tx) error {
		if updErr := f.update(tx, commentID, &db.CommentUpdate{Type: newType, Body: newBody}, now); updErr != nil {
			return updErr
		}
		return f.logAudit(tx, f.opUpdate, parentID, now)
	})
}

// commentRemove deletes one comment.
//
// Usage:
//
//	rmp <family> comment-remove -r <roadmap> <comment-id>
//
// The positional argument is the COMMENT's own id. Exactly one id is accepted: no
// comma-separated list, so the batch fail-fast rules of `task remove` do not
// apply here.
//
// The row is removed outright — no soft delete, no recovery — but the audit entry
// outlives it, so the parent's history still records that a comment existed and
// was removed. Produces no output on success.
func commentRemove(f *commentFamily, args []string) error {
	roadmapName, remaining, err := requireRoadmap(args)
	if err != nil {
		return err
	}

	commentID, rest, err := requireCommentPositionalID(remaining, commentIDField)
	if err != nil {
		return err
	}
	if err := rejectCommentLeftovers(rest); err != nil {
		return err
	}

	database, err := db.OpenExisting(roadmapName)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := db.WithQuickTimeout()
	defer cancel()

	parentID, err := f.parentOf(ctx, database, commentID)
	if err != nil {
		return err
	}

	now := utils.NowISO8601()
	return database.WithTransaction(func(tx *sql.Tx) error {
		if delErr := f.remove(tx, commentID); delErr != nil {
			return delErr
		}
		return f.logAudit(tx, f.opDelete, parentID, now)
	})
}

// commentTypeFlag is the registry declaration of `-y, --type` on a comment
// subcommand. enumName selects the family's own comment-type enum
// ("TaskCommentType" / "SprintCommentType"), which is what keeps the two sets —
// and, inside the task family, the unrelated TaskType carried by the same flag
// spelling on list/create/edit — from being conflated (SPEC/HELP.md § Comment
// subcommand help specifics item 1).
func commentTypeFlag(enumName, description string, required bool) Flag {
	return Flag{
		Long:        "--type",
		Short:       "-y",
		Type:        "enum",
		Enum:        enumName,
		Required:    required,
		Description: description,
	}
}

// commentBodyFlag is the registry declaration of `-b, --body` on a comment
// subcommand. StdinFallback is what publishes the standard-input source in the AI
// Agent Contract; the condition under which the fallback does not apply is
// family- and subcommand-specific, so it travels in description
// (SPEC/DATA_FORMATS.md § flags[] entry, stdin_fallback).
func commentBodyFlag(description string) Flag {
	return Flag{
		Long:          "--body",
		Short:         "-b",
		Type:          "string",
		MaxLength:     models.MaxCommentBody,
		Description:   description,
		StdinFallback: true,
	}
}
