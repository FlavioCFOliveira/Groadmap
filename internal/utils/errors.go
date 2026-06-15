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

	// ErrNoRoadmap indicates no roadmap is selected.
	ErrNoRoadmap = errors.New("no roadmap selected")

	// ErrDatabase indicates a database error occurred.
	ErrDatabase = errors.New("database error")

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
