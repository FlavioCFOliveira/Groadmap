// Package commands — the `Exit codes:` block of a subcommand help, rendered
// from the registry.
//
// SPEC/HELP.md § Help structure template, block 8, and § Agreement with the
// contract fix both what the block of a subcommand help says and how it is laid
// out. What it says is the registry's exit-code entries for that subcommand —
// the entries the AI Agent Contract publishes as `exit_codes` — every code once,
// in the entries' order, each with its conditions in the contract's words and
// order. How it is laid out is one shape for every help, so that a gate can
// reproduce the block from the contract alone and compare it line for line.
//
// The block is therefore never written by hand. Every subcommand help printer
// calls exitCodesBlock, and that one function is the only place the shape is
// implemented: a help and the contract draw every code and every condition from
// one source, and cannot come to disagree.
package commands

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// exitCodesHeader is the line that opens the block.
	exitCodesHeader = "Exit codes:"
	// exitCodesIndent is the width of the column an entry's code occupies, its
	// two leading spaces included. Every line of an entry after its first
	// begins with this many spaces.
	exitCodesIndent = 8
	// exitCodesWidth is the greatest number of Unicode code points a line of
	// the block may hold, the indent included, unless a single word is longer
	// than the space after the indent.
	exitCodesWidth = 80
)

// exitCodeRemedy is text a help states after the last condition of one code's
// entry. The contract carries no remedy, so a remedy is the help's own text;
// SPEC/HELP.md § Agreement with the contract admits it on one entry alone, the
// exit-code-1 entry of `graph client`.
type exitCodeRemedy struct {
	// Remedies are laid out after the entry's last condition, each beginning on
	// a line of its own and laid out as a condition is.
	Remedies []string
	// Code is the exit code whose entry the remedies follow.
	Code int
}

// exitCodesBlock returns the `Exit codes:` block of the help of command's
// subcommand, rendered from that subcommand's registry exit-code entries and
// ended by the empty line that closes the block. subcommand is "" for a command
// that takes no subcommand, such as `stats` and `web`. remedies, when supplied,
// follow the last condition of the entry for their code.
//
// A name the registry does not hold renders a block with no entry, which every
// gate over the helps refuses; TestExitCodesBlock_EveryHelpCarriesItsRenderedBlock
// holds each printer to the entry it names.
func exitCodesBlock(command, subcommand string, remedies ...exitCodeRemedy) string {
	return renderExitCodesBlock(registeredExitCodes(command, subcommand), remedies)
}

// registeredExitCodes returns the exit-code entries the registry declares for
// command's subcommand, or nil when the registry holds no such subcommand.
func registeredExitCodes(command, subcommand string) []ExitCodeEntry {
	cmd := AppRegistry().FindCommand(command)
	if cmd == nil {
		return nil
	}
	if !cmd.HasSubcommand {
		if subcommand != "" || len(cmd.Subcommands) != 1 {
			return nil
		}
		return cmd.Subcommands[0].ExitCodes
	}
	sub := cmd.FindSubcommand(subcommand)
	if sub == nil {
		return nil
	}
	return sub.ExitCodes
}

// renderExitCodesBlock lays entries out in the shape SPEC/HELP.md § Agreement
// with the contract, "How the block is rendered", specifies:
//
//  1. the line `Exit codes:`, one entry per element in order, then an empty
//     line;
//  2. an entry's first line is two spaces, the code in decimal, and spaces to
//     column eight, followed by the start of its first condition; each further
//     condition begins on a line of its own, and every line after the first
//     begins with eight spaces;
//  3. a condition is laid out by words, each line taking as many words as fit
//     within 80 code points, a word longer than the space after the indent
//     standing alone on its line, and no line ending in a space;
//  4. the remedies for an entry's code follow its last condition, laid out as
//     conditions are.
func renderExitCodesBlock(entries []ExitCodeEntry, remedies []exitCodeRemedy) string {
	var b strings.Builder
	b.WriteString(exitCodesHeader)
	b.WriteByte('\n')

	continuation := strings.Repeat(" ", exitCodesIndent)
	for _, entry := range entries {
		first := "  " + strconv.Itoa(entry.Code)
		if pad := exitCodesIndent - len(first); pad > 0 {
			first += strings.Repeat(" ", pad)
		}

		prefix := first
		for _, condition := range entry.Conditions {
			layOutWords(&b, prefix, condition)
			prefix = continuation
		}
		for _, remedy := range remedies {
			if remedy.Code != entry.Code {
				continue
			}
			for _, text := range remedy.Remedies {
				layOutWords(&b, prefix, text)
				prefix = continuation
			}
		}
	}

	b.WriteByte('\n')
	return b.String()
}

// layOutWords writes text to b as one or more lines, the first opening with
// prefix and every other with the continuation indent. Words are placed greedily:
// a word goes on the current line when the line, a separating space and the word
// fit within exitCodesWidth code points, and otherwise starts the next line. A
// line holding no word yet always takes the next one, which is what places a word
// longer than the space after the indent alone on its line rather than breaking
// it. Text with no word writes the prefix alone, trimmed of its trailing spaces.
func layOutWords(b *strings.Builder, prefix, text string) {
	words := strings.Fields(text)
	if len(words) == 0 {
		b.WriteString(strings.TrimRight(prefix, " "))
		b.WriteByte('\n')
		return
	}

	b.WriteString(prefix)
	width := utf8.RuneCountInString(prefix)
	placed := 0
	for _, word := range words {
		n := utf8.RuneCountInString(word)
		if placed > 0 && width+1+n > exitCodesWidth {
			b.WriteByte('\n')
			for i := 0; i < exitCodesIndent; i++ {
				b.WriteByte(' ')
			}
			width = exitCodesIndent
			placed = 0
		}
		if placed > 0 {
			b.WriteByte(' ')
			width++
		}
		b.WriteString(word)
		width += n
		placed++
	}
	b.WriteByte('\n')
}
