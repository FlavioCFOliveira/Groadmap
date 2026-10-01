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
	// condInvalidRoadmapName is one of exit code 6's conditions on every
	// subcommand that takes -r: the name is judged by the roadmap name rules
	// at the step at which the subcommand resolves its roadmap, and a name
	// that breaks one is refused with that rule's line instead of the
	// not-found line (SPEC/COMMANDS.md § Roadmap Name Validation).
	condInvalidRoadmapName = "The name given to -r/--roadmap breaks a roadmap name rule; the line of the first rule it breaks is printed."
)

// withRoadmapNameCondition adds condInvalidRoadmapName to exit code 6 of every
// subcommand that takes -r, which is every subcommand whose exit code 3 carries
// condNoRoadmap. It is applied once, to the whole registry, because the
// condition is produced by the shared roadmap-resolution step and is the same
// on every one of them, including a subcommand that has no other cause of exit
// code 6 (SPEC/COMMANDS.md § Roadmap Name Validation). An exit code 6 entry is
// created in its ascending place when the subcommand declares none.
func withRoadmapNameCondition(reg *Registry) *Registry {
	for i := range reg.Commands {
		for j := range reg.Commands[i].Subcommands {
			sub := &reg.Commands[i].Subcommands[j]
			if !declaresCondition(sub.ExitCodes, 3, condNoRoadmap) {
				continue
			}
			sub.ExitCodes = addExitCondition(sub.ExitCodes, 6, condInvalidRoadmapName)
		}
	}
	return reg
}

// condRepeatedFlag is one of exit code 2's conditions on every subcommand that
// declares a flag other than the help tokens: no flag of any command is
// repeatable, and a second occurrence of one, in any spelling, is refused
// (SPEC/COMMANDS.md § Repeated Flags). It is the same condition everywhere,
// produced by the flag-reading step every such subcommand shares.
const condRepeatedFlag = "A flag was supplied more than once; no flag is repeatable, and every spelling of one flag counts as that flag."

// withRepeatedFlagCondition adds condRepeatedFlag to exit code 2 of every
// subcommand that declares at least one flag other than --help. --help and -h
// are help tokens rather than flags in the sense of the rule, and repeating
// one is not refused, so a subcommand that declares nothing else cannot
// produce the condition. It is applied once, to the whole registry, for the
// reason withRoadmapNameCondition is.
func withRepeatedFlagCondition(reg *Registry) *Registry {
	for i := range reg.Commands {
		for j := range reg.Commands[i].Subcommands {
			sub := &reg.Commands[i].Subcommands[j]
			if !declaresNonHelpFlag(sub) {
				continue
			}
			sub.ExitCodes = addExitCondition(sub.ExitCodes, 2, condRepeatedFlag)
		}
	}
	return reg
}

// declaresNonHelpFlag reports whether sub declares a flag other than --help.
func declaresNonHelpFlag(sub *Subcommand) bool {
	for i := range sub.Flags {
		if sub.Flags[i].Long != "--help" {
			return true
		}
	}
	return false
}

// declaresCondition reports whether codes carries condition under code.
func declaresCondition(codes []ExitCodeEntry, code int, condition string) bool {
	for _, entry := range codes {
		if entry.Code != code {
			continue
		}
		for _, c := range entry.Conditions {
			if c == condition {
				return true
			}
		}
	}
	return false
}

// addExitCondition returns codes with condition appended to the entry for code,
// inserting that entry in ascending order when codes declares none.
func addExitCondition(codes []ExitCodeEntry, code int, condition string) []ExitCodeEntry {
	for i := range codes {
		if codes[i].Code == code {
			codes[i].Conditions = append(codes[i].Conditions, condition)
			return codes
		}
		if codes[i].Code > code {
			out := make([]ExitCodeEntry, 0, len(codes)+1)
			out = append(out, codes[:i]...)
			out = append(out, ec(code, condition))
			return append(out, codes[i:]...)
		}
	}
	return append(codes, ec(code, condition))
}

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
// the same on all of them: `task next` cannot print the malformed-id line
// because its single positional is optional and non-numeric values reach its
// own validation. The unknown-flag line, by contrast, is produced by every
// subcommand that reads positional arguments, `task get` and `task next`
// included: a token beginning with "-" written in a positional slot is read as
// that slot's value and refused by that slot's check, but the same token
// written between two positional arguments or after the last of them stands in
// no slot and is refused as an unknown flag (SPEC/COMMANDS.md § Positional
// Arguments; rmp tasks 465 and 484). Each assignment below was measured against
// the compiled binary, one subcommand at a time.
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
