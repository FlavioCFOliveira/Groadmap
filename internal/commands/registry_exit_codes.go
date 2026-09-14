// Package commands — per-subcommand exit-code declaration helpers.
//
// SPEC/DATA_FORMATS.md § Field reference: per-subcommand exit code entry
// requires every code a subcommand can emit to be published together with
// the conditions that produce it, and forbids the one thing a bare list of
// integers invited: a sentence generic enough to be true of every
// subcommand. The conditions therefore live with each registry entry, in
// that entry's own words.
//
// Two things are shared here, and only two:
//
//  1. ec(), the constructor, so the declarations read as data rather than
//     as struct literals.
//  2. The conditions that are genuinely the SAME condition wherever they
//     appear, because a step the subcommand SHARES produces them and not
//     anything the subcommand does: the roadmap-resolution step every
//     roadmap-scoped subcommand runs before its own work, the one arity
//     point every invocation passes through, and the one flag parser every
//     subcommand with flags of its own builds. Sharing those is not the
//     defect the rule exists to remove: they are one condition published in
//     many places, not many conditions flattened into one sentence.
//     Everything that differs between subcommands is written out at the
//     subcommand, and a subcommand carries a shared condition only when it
//     can actually produce it — which was measured, one subcommand at a
//     time, against the compiled binary.
//
// The gate is TestGenerate_ExitCodeConditionsAreNotOneGenericSentence in
// internal/aihelp: a sentence that spreads across more than half the surface
// fails there unless it is named, with its reason, in that test's exemption
// list. Adding a constant below without adding it there is caught.
package commands

// ec builds one ExitCodeEntry. Conditions are supplied variadically so a
// registry entry reads as `ec(6, "…", "…")` rather than as a nested
// struct literal.
//
// It does not validate: the invariants (at least one condition, no empty
// condition, ascending order, a code declared once) are pinned by
// TestRegistry_ExitCodeEntriesAreWellFormed in registry_test.go, which
// sees every entry in the built registry rather than only the ones a
// constructor happened to be called for.
func ec(code int, conditions ...string) ExitCodeEntry {
	return ExitCodeEntry{Code: code, Conditions: conditions}
}

// The two conditions that are identical wherever they occur. Both are
// produced by the shared roadmap-resolution step every roadmap-scoped
// subcommand runs before its own work, so the same sentence is the true
// one in every case (SPEC/COMMANDS.md § Roadmap Selection (Always
// Required)).
const (
	// condNoRoadmap is exit code 3 on every subcommand that takes -r.
	condNoRoadmap = "Neither -r nor --roadmap was supplied."
	// condRoadmapNotFound is one of exit code 4's conditions on every
	// subcommand that takes -r.
	condRoadmapNotFound = "The roadmap named by -r/--roadmap does not exist."
)

// The conditions of exit code 2 that are the same condition wherever they
// occur, for the same reason the two above are: each is produced by a step
// shared by every subcommand that reaches it, not by anything a particular
// subcommand does. Three come from the argument surface the registry itself
// declares — the shared arity point (positional_arity.go) and the shared flag
// parser (flags.go) — and read the same on every subcommand that has that
// surface. Writing them out at each of the thirty-eight subcommands that
// gained code 2 would be one sentence restated thirty-eight times, which is
// the restatement SPEC/DATA_FORMATS.md § Field reference: per-subcommand exit
// code entry, rule 2, exists to prevent.
//
// A subcommand carries only the ones it can actually produce, which is not
// the same on all of them: `task get` cannot print the unknown-flag line
// because a token beginning with "-" written in its id position is read as
// the id, and `task next` can produce neither that line nor the malformed-id
// one because its single positional is optional and non-numeric values reach
// its own validation. Each assignment below was measured against the compiled
// binary, one subcommand at a time.
const (
	// condUnknownFlag is one of exit code 2's conditions on every
	// subcommand that refuses a token naming none of its own flags
	// (SPEC/COMMANDS.md § Positional Arguments, rule 5).
	condUnknownFlag = "A token beginning with - names none of this subcommand's flags."
	// condFlagWithoutValue is one of exit code 2's conditions on every
	// subcommand that declares a value-taking flag.
	condFlagWithoutValue = "A value-taking flag was written with nothing after it to be its value."
	// condNonIntegerFlagValue is one of exit code 2's conditions on every
	// subcommand that declares a flag whose value must be an integer. It is
	// distinct from a value that IS an integer but out of range, which is a
	// validation failure and exits 6.
	condNonIntegerFlagValue = "A flag whose value must be an integer carries a value that is not one."
	// condMalformedPositionalID is one of exit code 2's conditions on every
	// subcommand that reads a positional id. The two ways of producing it are
	// one condition, because the command reaches the same check on both: the
	// token written in the id's position is read as the id either way.
	condMalformedPositionalID = "A positional id is not an integer, which is also how a token beginning with - written in an id's position is refused: it is read as that id."
	// condMissingPositional is one of exit code 2's conditions on every
	// subcommand that declares a required positional argument.
	condMissingPositional = "A required positional argument was omitted."
	// condExcessPositional is one of exit code 2's conditions on every
	// subcommand, because the shared arity point refuses a surplus positional
	// argument on the only path that reaches a handler
	// (SPEC/COMMANDS.md § Positional Arguments).
	condExcessPositional = "More positional arguments were supplied than this subcommand accepts."
)
