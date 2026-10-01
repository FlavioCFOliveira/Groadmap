package utils

import (
	"fmt"
	"strings"
)

// RepeatedFlagError words the CLI-wide refusal of a flag supplied more than
// once in one invocation (SPEC/COMMANDS.md § Repeated Flags):
//
//	Error: invalid input: repeated flag: <flag>
//
// token is the refused occurrence — the second occurrence of the flag reading
// the command line from left to right — spelled as the command line spells it.
// A joined "=value" tail is removed, exactly as the unrecognised-flag refusal
// names its flag, so "--title=Second" is reported as "--title" and "-t" as
// "-t". The refusal carries ErrInvalidInput, exit code 2.
//
// The sentence exists once, here, because the flag parsers that must refuse a
// repetition live in two packages that cannot import each other:
// internal/commands, which parses every subcommand's flags, and internal/web,
// which parses the flags of `rmp web`.
func RepeatedFlagError(token string) error {
	name, _, _ := strings.Cut(token, "=")
	return fmt.Errorf("%w: repeated flag: %s", ErrInvalidInput, name)
}

// FlagOccurrences records which flags an invocation has supplied, so that a
// parser can refuse a second occurrence of any of them. Every spelling of one
// flag — the short form, the long form, the separate and the joined form —
// counts as that flag: callers record the flag under its canonical (long)
// name and report the token as written.
//
// The zero value is ready to use. It is a small slice rather than a map
// because an invocation supplies a handful of flags at most, and the slice
// costs no hashing and, for the common case of no flags at all, no
// allocation.
type FlagOccurrences struct {
	seen []string
}

// Note records one occurrence of the flag whose canonical name is canonical,
// written on the command line as token. It returns RepeatedFlagError(token)
// when the flag was already recorded, and nil otherwise.
func (f *FlagOccurrences) Note(canonical, token string) error {
	for _, s := range f.seen {
		if s == canonical {
			return RepeatedFlagError(token)
		}
	}
	f.seen = append(f.seen, canonical)
	return nil
}
