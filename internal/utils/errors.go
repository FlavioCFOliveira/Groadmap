// Package utils provides utility functions for the Groadmap CLI application.
package utils

import (
	"errors"
	"fmt"
)

// MessageError carries a SPEC-mandated, human-readable message while still
// chaining one or more sentinel errors for errors.Is-based exit-code mapping.
// Its Error() returns ONLY the message (no sentinel prefix), so messages the
// SPEC specifies verbatim render exactly as documented — e.g.
// "Error: Roadmap name must not exceed 50 characters (got 60)" rather than
// "Error: validation error: ...: roadmap name too long". Unwrap returns the
// full sentinel chain (Go 1.20+ multi-error semantics), so both
// errors.Is(err, ErrValidation) and errors.Is(err, ErrRoadmapNameTooLong) hold.
type MessageError struct {
	Msg       string
	Sentinels []error
}

func (e *MessageError) Error() string   { return e.Msg }
func (e *MessageError) Unwrap() []error { return e.Sentinels }

// ValidationMessage builds a MessageError that wraps ErrValidation (exit 6)
// plus any additional sentinels, rendering exactly the provided message.
func ValidationMessage(msg string, extra ...error) error {
	return &MessageError{Msg: msg, Sentinels: append([]error{ErrValidation}, extra...)}
}

// ValidationMessagef is the fmt.Sprintf-style variant of ValidationMessage.
// (Note: it takes no extra sentinels; use ValidationMessage for those.)
func ValidationMessagef(format string, a ...any) error {
	return &MessageError{Msg: fmt.Sprintf(format, a...), Sentinels: []error{ErrValidation}}
}

// Sentinel errors for common error conditions.
// These errors can be used with errors.Is for reliable error checking.
var (
	// ErrNotFound indicates a resource was not found.
	ErrNotFound = errors.New("resource not found")

	// ErrAlreadyExists indicates a resource already exists.
	ErrAlreadyExists = errors.New("resource already exists")

	// ErrInvalidInput indicates invalid input was provided.
	ErrInvalidInput = errors.New("invalid input")

	// ErrRequired indicates a required field or parameter is missing.
	ErrRequired = errors.New("required parameter missing")

	// ErrUnknownCommand indicates a dispatch failure: a name the CLI was
	// given does not resolve to a command, or does not resolve to a
	// subcommand of the command that did resolve. It maps to exit code
	// 127 (EXIT_CMD_NOT_FOUND).
	//
	// A dispatch failure MUST be carried by this sentinel and MUST NOT be
	// wrapped in ErrInvalidInput. The two are distinct classes:
	// ErrInvalidInput covers a malformed flag or argument supplied to a
	// command that WAS resolved (exit 2); ErrUnknownCommand covers a name
	// that could not be resolved at all (exit 127). Wrapping the second in
	// the first is what used to make an unresolved subcommand exit 2, and
	// it also prefixed the message with "invalid input: ", misreporting the
	// class to the reader (SPEC/ARCHITECTURE.md § Sentinel Error Catalogue).
	ErrUnknownCommand = errors.New("unknown command")

	// ErrNoRoadmap indicates no roadmap is selected.
	ErrNoRoadmap = errors.New("no roadmap selected")

	// ErrDatabase indicates a database error occurred.
	//
	// Its scope is a roadmap's project.db and nothing else. No graph failure
	// carries it: the graph store holds GoGraph's snapshot, WAL and lock file,
	// and SPEC/GRAPH.md § Constraints forbids a graph operation from touching
	// project.db at all, so the word never described what failed there.
	ErrDatabase = errors.New("database error")

	// The three graph sentinels. All map to exit code 1, exactly as ErrDatabase
	// does, and the split exists for the reader rather than for the code: the
	// sentinel is the first thing an agent reads, and these three select three
	// different ACTIONS (SPEC/ARCHITECTURE.md § Sentinel Error Catalogue).
	//
	// One sentinel would not do it. "graph error" in front of both "fix your
	// Cypher" and "start a server" reproduces one level down the defect the
	// split exists to close: it still fails to say what to do next.

	// ErrGraphEngine indicates the statement reached the graph engine and did
	// not complete there -- a parse failure, a refusal, an exhausted time
	// budget, a lost write conflict. ACT ON THE STATEMENT.
	ErrGraphEngine = errors.New("graph engine error")

	// ErrGraphStore indicates the graph store itself, its directory, or its
	// exclusive advisory lock. ACT ON THE FILESYSTEM OR THE LOCK HOLDER.
	ErrGraphStore = errors.New("graph store error")

	// ErrGraphServer indicates the server, its socket, or the connection to it.
	// ACT ON THE SERVER OR ON --socket.
	ErrGraphServer = errors.New("graph server error")

	// ErrIO indicates a stream, socket, file or directory the CLI reads or
	// writes, and that is NOT a roadmap's database. It maps to exit code 1.
	//
	// The sentinel names the ARTEFACT rather than the layer that reported the
	// failure. Moving a legacy project.db, or failing to secure it to 0600,
	// therefore stays ErrDatabase even though both are file operations: the
	// artefact is the database. A failed read of the process's own standard
	// input, or a listener that cannot bind, is neither.
	//
	// "I/O" is written in the case the specification already used for this class
	// before the sentinel existed. It is an acronym whose capitals are its
	// spelling, not emphasis, which is why it sits beside twelve lower-case
	// sentinels without breaking the rule they follow
	// (SPEC/ARCHITECTURE.md § Sentinel Error Catalogue, rule 5).
	ErrIO = errors.New("I/O error")

	// ErrValidation indicates a validation error.
	ErrValidation = errors.New("validation error")

	// ErrFieldTooLarge indicates a field exceeds the maximum allowed size.
	ErrFieldTooLarge = errors.New("field exceeds maximum size")

	// ErrInvalidUpdate indicates an attempt to update non-whitelisted fields.
	ErrInvalidUpdate = errors.New("invalid field update")
)

// IsNotFound checks if an error is ErrNotFound or wraps it.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsAlreadyExists checks if an error is ErrAlreadyExists or wraps it.
func IsAlreadyExists(err error) bool {
	return errors.Is(err, ErrAlreadyExists)
}

// IsInvalidInput checks if an error is ErrInvalidInput or wraps it.
func IsInvalidInput(err error) bool {
	return errors.Is(err, ErrInvalidInput)
}

// IsRequired checks if an error is ErrRequired or wraps it.
func IsRequired(err error) bool {
	return errors.Is(err, ErrRequired)
}

// IsUnknownCommand checks if an error is ErrUnknownCommand or wraps it.
func IsUnknownCommand(err error) bool {
	return errors.Is(err, ErrUnknownCommand)
}

// IsNoRoadmap checks if an error is ErrNoRoadmap or wraps it.
func IsNoRoadmap(err error) bool {
	return errors.Is(err, ErrNoRoadmap)
}

// IsDatabase checks if an error is ErrDatabase or wraps it.
func IsDatabase(err error) bool {
	return errors.Is(err, ErrDatabase)
}

// IsValidation checks if an error is ErrValidation or wraps it.
func IsValidation(err error) bool {
	return errors.Is(err, ErrValidation)
}

// IsFieldTooLarge checks if an error is ErrFieldTooLarge or wraps it.
func IsFieldTooLarge(err error) bool {
	return errors.Is(err, ErrFieldTooLarge)
}

// IsInvalidUpdate checks if an error is ErrInvalidUpdate or wraps it.
func IsInvalidUpdate(err error) bool {
	return errors.Is(err, ErrInvalidUpdate)
}

// IsGraphEngine checks if an error is ErrGraphEngine or wraps it.
func IsGraphEngine(err error) bool {
	return errors.Is(err, ErrGraphEngine)
}

// IsGraphStore checks if an error is ErrGraphStore or wraps it.
func IsGraphStore(err error) bool {
	return errors.Is(err, ErrGraphStore)
}

// IsGraphServer checks if an error is ErrGraphServer or wraps it.
func IsGraphServer(err error) bool {
	return errors.Is(err, ErrGraphServer)
}

// IsIO checks if an error is ErrIO or wraps it.
func IsIO(err error) bool {
	return errors.Is(err, ErrIO)
}
