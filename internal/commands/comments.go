// Package commands — input handling shared by the comment subcommands.
//
// The four comment subcommands of the `task` family and the four of the `sprint`
// family share one input contract (SPEC/COMMANDS.md § Comment Body Input Source
// and Precedence): a positional id, a `--type` drawn from the family's own
// comment-type subset, and a `--body` that falls back to standard input. This
// file holds the part of that contract which does not depend on which family is
// being served, so both families share one implementation instead of carrying a
// copy each.
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
//     empty body as the latter, so this layer decides emptiness itself with
//     models.NormalizeCommentBody and keeps the domain's exit-6 verdict
//     unreachable from the command line.
package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// The body flag, spelled once. Both forms are accepted wherever a comment body
// is read (SPEC/COMMANDS.md § Add Task Comment).
const (
	commentBodyLong  = "--body"
	commentBodyShort = "-b"
)

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
// entity names what the id identifies, so the pinned messages name the right
// thing: "task" / "sprint" for the parent id `comment-add` and `comment-list`
// take, "comment" for the comment's own id `comment-edit` and `comment-remove`
// take.
//
// Every malformed id — non-numeric, non-positive, or beyond MaxInt32 — is exit
// code 2 here. utils.ValidateIDString classifies a non-positive or oversized
// value as a validation error (exit code 6); SPEC/COMMANDS.md pins exit code 2
// for the whole "positive integer" constraint on these subcommands, so the
// verdict is re-classified rather than the parsing re-implemented.
func requireCommentPositionalID(args []string, entity string) (int, []string, error) {
	if len(args) == 0 {
		return 0, nil, fmt.Errorf("%w: %s ID required", utils.ErrRequired, entity)
	}

	raw := args[0]
	id, err := utils.ValidateIDString(raw, entity)
	if err == nil {
		return id, args[1:], nil
	}
	if errors.Is(err, utils.ErrInvalidInput) {
		// Non-numeric token: already the class and the wording SPEC pins.
		return 0, nil, err
	}
	return 0, nil, fmt.Errorf("%w: invalid %s ID: %q (must be a positive integer no greater than %d)",
		utils.ErrInvalidInput, entity, raw, utils.MaxInt32)
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
func resolveCommentBody(body commentBody, stdinFallback bool) (string, bool, error) {
	if body.present {
		if body.valueMissing || models.NormalizeCommentBody(body.value) == "" {
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
	if models.NormalizeCommentBody(raw) == "" {
		return "", false, nil
	}
	return raw, true, nil
}

// readCommentBodyStdin reads standard input to EOF as the comment body.
//
// A read failure is an I/O failure of the process, not bad user input, so it maps
// to exit code 1 exactly as the graph subcommands' stdin read does. The
// underlying error is interpolated with %v rather than wrapped, so a sentinel
// hiding inside it cannot silently re-map the exit code.
func readCommentBodyStdin() (string, error) {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("%w: reading the comment body from standard input: %v", utils.ErrDatabase, err)
	}
	return string(raw), nil
}

// parseCommentTypeFlag runs the shared flag parser over what is left after the
// positional id and the body flag have been consumed, and reports the raw
// `--type` value together with whether the flag was present at all.
//
// The value is returned unparsed: the accepted set depends on the family, so the
// caller applies models.ParseTaskCommentType or models.ParseSprintCommentType.
func parseCommentTypeFlag(args []string) (string, bool, error) {
	result, err := NewFlagParser(commentTypeFlagDefs).Parse(args)
	if err != nil {
		return "", false, err
	}
	raw, ok := result.Flags["Type"].(string)
	return raw, ok, nil
}

// rejectUnknownFlags refuses any flag left in args. It serves `comment-remove`,
// which takes no flag beyond the shared -r / -h, and it runs the shared flag
// parser with an empty definition set so the "unknown flag" wording is the one
// every other subcommand produces rather than a second copy of it.
func rejectUnknownFlags(args []string) error {
	_, err := NewFlagParser(nil).Parse(args)
	return err
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
