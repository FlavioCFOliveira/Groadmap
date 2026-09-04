// Package aihelp — documented-example invocation gate.
//
// Every `rmp ...` command line printed as an example in SPEC/, DOCS/ or
// README.md is a promise: a reader — a person or an agent — is expected to be
// able to type it. This gate resolves each of them against the AI Agent
// Contract, the machine-readable description the binary generates from its own
// command registry, so an example that names a command, a subcommand or a flag
// the CLI does not have fails `make check` instead of failing the reader.
//
// The defect that produced this gate stood in SPEC/ARCHITECTURE.md § Usage in
// Shell Scripts for an entire release. It read
//
//	rmp task add -r myproject -d "New task"   # Exits 3 if no roadmap specified
//
// and it was wrong three times over: `task add` does not exist (exit 127), `-d`
// is not a flag of any task subcommand (exit 2), and the line passed `-r` while
// claiming to demonstrate the exit code for a MISSING roadmap, which it could
// therefore never reach. Nothing in the build noticed, because until now nothing
// in the build read an example.
//
// # The oracle
//
// The contract, obtained by calling Generate directly rather than by shelling
// out to a built binary: the emitter walks the same registry the CLI dispatches
// from, so an example is checked against the command surface as it exists in
// this working tree rather than in whatever binary happens to sit in ./bin.
//
// # What is checked, and what deliberately is not
//
// Command names, subcommand names — both resolved THROUGH their aliases, which
// 65 invocations in the corpus depend on — and flag SPELLINGS per subcommand.
// Nothing else. Not argument values, not flag values, not required-flag
// presence, not positional arity.
//
// That restriction is not caution for its own sake, and it is not a guess: it is
// measured. The contract publishes 153 examples of its own, of which 68 declare
// a non-zero exit, and every one of those 68 fails on a VALUE or a MISSING FLAG
// — `--sort foo`, `task get 99999`, a `task create` with no `--title` — with a
// single deliberate exception, `rmp web --foo`, which demonstrates an unknown
// FLAG. Not one fails on an unknown command or subcommand NAME. A checker that
// also judged values or required flags would therefore start reporting correct,
// deliberately-failing examples as defects.
// TestExampleInvocations_ContractExamplesFailOnValuesRatherThanNames keeps that
// asymmetry true, because the whole rule rests on it.
//
// # Recognition
//
// Measured against the corpus before it was written; every clause below earns
// its place:
//
//   - Only lines inside fenced code blocks whose language is shell-ish or absent
//     ("", bash, sh, shell, console). The untagged fences are not optional: the
//     six prompted transcripts live in them.
//   - A leading "$ " marks a transcript and is stripped; see the skip classes.
//   - A trailing " #" comment is cut, outside quotes.
//   - A leading if / elif / while / until and a trailing "; then" or "; do" are
//     peeled, which is what makes SPEC/ARCHITECTURE.md's
//     `if rmp task list -r myproject > /dev/null 2>&1; then` readable.
//   - Placeholders — <...> and [...] — are MASKED BEFORE redirections are
//     stripped. The order is load-bearing and is pinned by its own test: a
//     redirection stripper reads the `<` of `rmp <command> --ai-help` as an
//     input redirection and eats the rest of the line with it. Reversing the two
//     steps manufactures false positives on correct synopsis lines.
//   - The line is then split on |, ||, && and ; — outside quotes, because three
//     `--body` values in the corpus contain a semicolon.
//   - A segment is an invocation when its first token is EXACTLY `rmp`, or a
//     path ending in /rmp. Never a prefix match on ^rmp\b:
//     `rmp-{version}-{target}.tar.gz` is a line of SPEC/BUILD.md and
//     `rmp-{version}-{os}-{arch}.{ext}` a line of SPEC/DEPLOY.md, and a
//     word-boundary match swallows both.
//
// Quote awareness runs through all of it. No documented value today contains a
// word that opens with a dash, so a naive whitespace split happens to agree with
// this one on the current tree — but the day a `--body "use --verbose instead"`
// is written, a naive split reads `--verbose` as a flag of the subcommand and
// reports a correct example as a defect. Respecting quotes costs one state
// machine and removes that failure mode permanently; it also removes the
// hand-written exception for the bare `-` that a naive split produces three
// times from README.md's `-fr "Functional requirements - Why build it?"`.
//
// # Continuation lines
//
// A logical line is assembled per fenced block: a physical line ending in a
// backslash continues into the next. The task premise recorded that no rmp line
// in the corpus ends in a backslash and that this machinery was therefore
// unnecessary; reproduction disagrees. Thirty physical lines inside shell fences
// end in a backslash, 21 of them continuing an rmp invocation, and they carry
// the flags that matter — `-t`, `-fr`, `-tr`, `-ac`, `--type`, `--parent`,
// `--query`. Without joining, the invocation count is unchanged at 409 but 37
// flag spellings on the continuation lines are never read at all, and the
// acceptance criterion asks for the FULL example set. So the joining is done,
// and minJoinedContinuations keeps it exercised.
//
// # Three traps
//
// Each of these broke a prototype of this gate, and each has a test:
//
//  1. Flat commands. `stats`, `web` and `ai-help` each publish exactly one
//     subcommand whose name equals the command's, and `rmp stats -r x` carries
//     no subcommand token at all. A gate that always expects `cmd sub` fails all
//     three.
//  2. The `rmp-` archive-name token, above.
//  3. The placeholder / redirection ordering, above.
//
// # Two skip classes, enumerated rather than guessed
//
// Both are counted exactly, so neither can grow without a person looking at what
// was added:
//
//   - SYNOPSIS lines whose command or subcommand slot is a placeholder:
//     `rmp <command> --ai-help`, `rmp audit <subcommand> ...`,
//     `rmp task [subcommand] ...`. Six of each. The slot holds no name, so there
//     is no name to resolve — but everything to the LEFT of the placeholder is
//     still resolved, which is why a bogus command in a synopsis still fails.
//   - "$"-PROMPTED transcripts: exactly six, each in an untagged fence directly
//     above its own output. Three of them are invalid BY NAME on purpose, to
//     document exit 127. Exempting the prompt is mechanical, needs no new
//     annotation in the SPEC, and is proven to hide nothing: the gate asserts
//     that exactly three of the six fail name resolution and that each of those
//     three is one of the deliberate `nadadisto` transcripts.
//
// # Why the gate cannot quietly stop working
//
// A scanner that recognises nothing resolves nothing and passes. Every count
// this gate depends on therefore carries a floor: corpus files, documents
// holding an invocation, invocations, invocations resolved to a
// command-and-subcommand pair, flag spellings checked, distinct subcommands
// reached, alias resolutions, and joined continuations. The distinct-subcommand
// floor is the strongest of them — the corpus exercises all 59 subcommands the
// contract declares — because it cannot be satisfied by a scanner that has
// fallen back to recognising one shape.
//
// This file lives in internal/aihelp because that is where the contract emitter
// is. internal/models, which hosts the specification-side gates this one is
// modelled on, cannot reach Generate: aihelp imports models, so the edge cannot
// run the other way.
package aihelp

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Corpus definition and floors
// ---------------------------------------------------------------------------

// exampleCorpusReadme is the one corpus member that is not under a directory.
const exampleCorpusReadme = "README.md"

// exampleCorpusDirs are walked recursively for *.md. DOCS/ nests one level
// (DOCS/commands/*.md), so a glob would miss most of it.
var exampleCorpusDirs = [...]string{"SPEC", "DOCS"}

// exampleShellLanguages are the fence info strings whose contents are read as
// shell. The empty string is the important one: every "$"-prompted transcript in
// the corpus sits in an untagged fence.
var exampleShellLanguages = map[string]bool{
	"": true, "bash": true, "sh": true, "shell": true, "console": true,
}

// Floors. Their job is not to describe the documentation, it is to make a gate
// that has stopped recognising invocations fail rather than pass with nothing to
// do. Each sits far enough below its measurement that ordinary editing cannot
// trip it, and far enough above zero that a gate measuring nothing cannot report
// success. Measured on this tree: 24 corpus files, 13 of them holding an
// invocation, 409 invocations, 375 resolved to a command-and-subcommand pair,
// 560 flag spellings checked, all 59 subcommands reached, 65 alias resolutions,
// 30 joined continuation lines and 12 bare global-flag invocations.
const (
	minExampleCorpusFiles     = 20
	minExampleDocuments       = 10
	minExampleInvocations     = 350
	minExampleResolved        = 320
	minExampleFlagSpellings   = 450
	minExampleSubcommands     = 50
	minExampleAliasUses       = 30
	minJoinedContinuations    = 15
	minExampleGlobalFlagLines = 8
)

// The skip classes are counted exactly rather than floored. A skipped line is an
// unchecked example, so the set may not grow without someone confirming that
// what was added really is a synopsis or a transcript.
const (
	exampleCommandSlotSynopses = 6
	// Five, not six: DOCS/commands/graph.md's synopsis names its subcommand
	// (`rmp graph execute ...`) rather than leaving the slot a placeholder,
	// because the family has exactly one subcommand to name
	// (SPEC/COMMANDS.md § Graph Management). That line is therefore CHECKED
	// rather than skipped, which is the direction this count wants to move in.
	exampleSubcommandSlotSynopses = 5
	examplePromptedTranscripts    = 6
	// Three of the six prompted transcripts document exit 127 and are invalid
	// by name on purpose.
	examplePromptedInvalidByName = 3
	// The marker those three share. It is not a word of any language; it exists
	// in the SPEC precisely to be an unresolvable name.
	exampleDeliberateUnknownName = "nadadisto"
)

// minArchiveNameLines keeps trap 2 exercised from the corpus side: at least this
// many lines inside shell fences begin with a token that starts with "rmp" and
// is not an invocation. Two do — SPEC/BUILD.md's `rmp-{version}-{target}.tar.gz`
// and SPEC/DEPLOY.md's `rmp-{version}-{os}-{arch}.{ext}` — and if the last of
// them ever leaves the tree, the exact-match rule stops being exercised by
// anything but its unit test.
const minArchiveNameLines = 2

// shellPlaceholder is what a masked <...> or [...] becomes. It is a single token
// with no dash, so it is never mistaken for a flag, and it is spelled in capitals
// so it cannot collide with a real command or subcommand name.
const shellPlaceholder = "PLACEHOLDER"

// ---------------------------------------------------------------------------
// Reading the corpus
// ---------------------------------------------------------------------------

// exampleCorpusFiles returns every repository-relative Markdown path of the
// corpus, sorted, so failures come out in a stable order.
func exampleCorpusFiles(t *testing.T) []string {
	t.Helper()

	root := repoRoot(t)
	files := make([]string, 0, 32)
	for _, dir := range exampleCorpusDirs {
		walkRoot := filepath.Join(root, dir)
		err := filepath.WalkDir(walkRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr != nil {
				return relErr
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s for the example corpus: %v", dir, err)
		}
	}
	files = append(files, exampleCorpusReadme)
	sort.Strings(files)
	return files
}

// shellLine is one logical shell line: a physical line, or a run of physical
// lines joined across trailing backslashes, taken from inside one fenced block.
type shellLine struct {
	file string
	text string
	line int // 1-based line of the first physical line
	// joined counts the continuation lines folded into text. Summed over the
	// corpus it is what minJoinedContinuations checks.
	joined int
}

// fenceMarkerAt reports the fence marker run and the info string of a fence line,
// or ok=false when the line is not one. Written by hand rather than as a regexp
// because it runs over every line of every corpus file.
func fenceMarkerAt(line string) (marker byte, width int, info string, ok bool) {
	i := 0
	for i < len(line) && i < 4 && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) || i > 3 || (line[i] != '`' && line[i] != '~') {
		return 0, 0, "", false
	}
	marker = line[i]
	start := i
	for i < len(line) && line[i] == marker {
		i++
	}
	if i-start < 3 {
		return 0, 0, "", false
	}
	return marker, i - start, strings.ToLower(strings.TrimSpace(line[i:])), true
}

// shellLinesOf returns the logical shell lines of one document.
//
// Continuation joining is scoped to the fenced block: a trailing backslash on
// the last line of a block does not reach into the next one. README.md holds
// exactly that — a `--type USER_STORY --priority 7 --severity 3 \` immediately
// above a closing fence — and without the scoping it would splice two unrelated
// examples into one invocation.
func shellLinesOf(file, raw string) []shellLine {
	physical := strings.Split(raw, "\n")
	out := make([]shellLine, 0, 32)

	inFence, shellish := false, false
	var marker byte
	var width int

	var buf strings.Builder
	open, start, joined := false, 0, 0

	flush := func() {
		if !open {
			return
		}
		out = append(out, shellLine{file: file, text: buf.String(), line: start, joined: joined})
		buf.Reset()
		open, joined = false, 0
	}

	for i, line := range physical {
		m, w, info, isFence := fenceMarkerAt(line)
		if !inFence {
			if isFence {
				inFence, marker, width = true, m, w
				shellish = exampleShellLanguages[info]
			}
			continue
		}
		if isFence && m == marker && w >= width {
			flush()
			inFence = false
			continue
		}
		if !shellish {
			continue
		}

		if open {
			buf.WriteByte(' ')
			buf.WriteString(strings.TrimSpace(line))
			joined++
		} else {
			open, start = true, i+1
			buf.WriteString(strings.TrimSpace(line))
		}

		joinedText := strings.TrimRight(buf.String(), " \t")
		if strings.HasSuffix(joinedText, `\`) {
			buf.Reset()
			buf.WriteString(strings.TrimRight(strings.TrimSuffix(joinedText, `\`), " \t"))
			continue
		}
		flush()
	}
	flush()
	return out
}

// ---------------------------------------------------------------------------
// The recognition pipeline
// ---------------------------------------------------------------------------

// shellQuotedMask reports, byte by byte, whether a position of s lies inside a
// quoted region; the quote characters themselves count as inside. Working byte
// by byte is safe because every character it reasons about is ASCII and no UTF-8
// continuation byte can equal one.
func shellQuotedMask(s string) []bool {
	mask := make([]bool, len(s))
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote == 0:
			if ch == '\'' || ch == '"' {
				quote, mask[i] = ch, true
			}
		case ch == '\\' && quote == '"' && i+1 < len(s):
			mask[i], mask[i+1] = true, true
			i++
		case ch == quote:
			quote, mask[i] = 0, true
		default:
			mask[i] = true
		}
	}
	return mask
}

// stripShellPrompt removes an interactive "$ " prompt and reports that it was
// there. A prompted line is a transcript of a session rather than a line meant
// to be copied, and the six in the corpus are exempted wholesale; see the
// package comment.
func stripShellPrompt(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "$" {
		return "", true
	}
	if rest, found := strings.CutPrefix(trimmed, "$ "); found {
		return strings.TrimSpace(rest), true
	}
	return trimmed, false
}

// stripShellComment cuts a trailing comment. The hash must open a word and must
// not be quoted, so a `--body "issue #42"` keeps its text.
func stripShellComment(s string) string {
	mask := shellQuotedMask(s)
	for i := 0; i < len(s); i++ {
		if s[i] != '#' || mask[i] {
			continue
		}
		if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
			return strings.TrimRight(s[:i], " \t")
		}
	}
	return s
}

var (
	shellControlPrefixes = [...]string{"if ", "elif ", "while ", "until "}
	shellControlSuffixes = [...]string{"; then", ";then", "; do", ";do"}
)

// peelShellControl removes the shell control syntax that wraps a command inside
// a condition, so the command itself can be read.
func peelShellControl(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range shellControlPrefixes {
		if rest, found := strings.CutPrefix(s, prefix); found {
			s = strings.TrimSpace(rest)
			break
		}
	}
	for _, suffix := range shellControlSuffixes {
		if rest, found := strings.CutSuffix(s, suffix); found {
			s = strings.TrimSpace(rest)
			break
		}
	}
	return s
}

// maskShellPlaceholders replaces every unquoted <...> and [...] with a single
// placeholder token.
//
// A placeholder that is not closed before the next quote, or before another
// opener of the same kind, is left alone: the opener is then an ordinary
// character — most often the `<` of an input redirection, which the next stage
// removes.
func maskShellPlaceholders(s string) string {
	mask := shellQuotedMask(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		var closer byte
		switch {
		case mask[i]:
			b.WriteByte(s[i])
			i++
			continue
		case s[i] == '<':
			closer = '>'
		case s[i] == '[':
			closer = ']'
		default:
			b.WriteByte(s[i])
			i++
			continue
		}

		end := -1
		for j := i + 1; j < len(s); j++ {
			if mask[j] || s[j] == s[i] {
				break
			}
			if s[j] == closer {
				end = j
				break
			}
		}
		if end < 0 {
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteString(shellPlaceholder)
		i = end + 1
	}
	return b.String()
}

// stripShellRedirections removes an unquoted redirection and its target.
//
// A redirection starts a word: `2>&1` and `> /dev/null` are redirections,
// `a->b` inside a Cypher query is not. The target runs to the next whitespace or
// segment separator, which is what `\S+` means for a filename and what keeps a
// removed target from swallowing the `;` that ends the command.
func stripShellRedirections(s string) string {
	mask := shellQuotedMask(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if mask[i] || !shellWordStart(s, mask, i) {
			b.WriteByte(s[i])
			i++
			continue
		}
		end, hasTarget, ok := shellRedirectionAt(s, mask, i)
		if !ok {
			b.WriteByte(s[i])
			i++
			continue
		}
		i = end
		if hasTarget {
			for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
				i++
			}
			for i < len(s) && !mask[i] && !shellSeparator(s[i]) {
				i++
			}
		}
		// A space in place of what was removed, so the tokens either side of a
		// redirection cannot be glued into one.
		b.WriteByte(' ')
	}
	return b.String()
}

// shellWordStart reports whether position i opens a word: the start of the
// string, or a position preceded by unquoted whitespace.
func shellWordStart(s string, mask []bool, i int) bool {
	if i == 0 {
		return true
	}
	return !mask[i-1] && (s[i-1] == ' ' || s[i-1] == '\t')
}

// shellSeparator reports the characters that end a redirection target: the ones
// a shell would not read as part of a filename.
func shellSeparator(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == ';' || ch == '|' || ch == '&'
}

// shellRedirectionAt reads a redirection operator at i — an optional file
// descriptor number, then < or > or >>, then an optional &N duplication — and
// returns the offset just past it and whether a separate target word follows.
func shellRedirectionAt(s string, mask []bool, i int) (end int, hasTarget, ok bool) {
	j := i
	for j < len(s) && !mask[j] && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j >= len(s) || mask[j] || (s[j] != '<' && s[j] != '>') {
		return 0, false, false
	}
	j++
	if j < len(s) && !mask[j] && s[j] == '>' && s[j-1] == '>' {
		j++
	}
	if j < len(s) && !mask[j] && s[j] == '&' {
		k := j + 1
		for k < len(s) && !mask[k] && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j+1 {
			return k, false, true
		}
	}
	return j, true, true
}

// prepareShellLine applies the recognition pipeline to one logical line and
// reports whether the line carried an interactive prompt.
//
// The order of the last two steps is the subject of
// TestExampleInvocations_PlaceholdersAreMaskedBeforeRedirectionsAreStripped and
// must not be swapped: a redirection stripper reads the `<` of
// `rmp <command> --ai-help` as an input redirection and removes the rest of the
// line with it.
func prepareShellLine(text string) (body string, prompted bool) {
	body, prompted = stripShellPrompt(text)
	body = stripShellComment(body)
	body = peelShellControl(body)
	body = maskShellPlaceholders(body)
	body = stripShellRedirections(body)
	return body, prompted
}

// ---------------------------------------------------------------------------
// Tokenisation
// ---------------------------------------------------------------------------

// shellToken is one word of a command line. quoted records whether the word
// OPENED inside quotes, which is what separates a flag from a value that merely
// starts with a dash.
type shellToken struct {
	text   string
	quoted bool
}

// isPlaceholder reports whether a token is a masked <...> or [...].
func (t shellToken) isPlaceholder() bool { return !t.quoted && t.text == shellPlaceholder }

// isFlag reports whether a token is a flag rather than a value. A lone "-" is
// not a flag; it is the conventional name for standard input, and README.md
// produces three of them from prose inside a quoted value.
func (t shellToken) isFlag() bool {
	return !t.quoted && len(t.text) > 1 && t.text[0] == '-'
}

// spelling is the part of a flag token that names it: everything before an "="
// in the --flag=value form.
func (t shellToken) spelling() string {
	if i := strings.IndexByte(t.text, '='); i >= 0 {
		return t.text[:i]
	}
	return t.text
}

// isEndOfFlags reports the "--" separator, after which nothing is a flag.
func (t shellToken) isEndOfFlags() bool { return !t.quoted && t.text == "--" }

// splitShellSegments splits a prepared line into segments at the unquoted
// operators |, ||, && and ;, and each segment into tokens at unquoted
// whitespace.
func splitShellSegments(s string) [][]shellToken {
	segments := make([][]shellToken, 0, 2)
	current := make([]shellToken, 0, 8)

	var tok strings.Builder
	open, quotedStart := false, false
	var quote byte

	endToken := func() {
		if !open {
			return
		}
		current = append(current, shellToken{text: tok.String(), quoted: quotedStart})
		tok.Reset()
		open, quotedStart = false, false
	}
	endSegment := func() {
		endToken()
		if len(current) > 0 {
			segments = append(segments, current)
			current = make([]shellToken, 0, 8)
		}
	}

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			switch {
			case ch == '\\' && quote == '"' && i+1 < len(s):
				tok.WriteByte(s[i+1])
				i++
			case ch == quote:
				quote = 0
			default:
				tok.WriteByte(ch)
			}
			continue
		}
		switch {
		case ch == '\'' || ch == '"':
			if !open {
				open, quotedStart = true, true
			}
			quote = ch
		case ch == ' ' || ch == '\t':
			endToken()
		case (ch == '|' || ch == '&') && i+1 < len(s) && s[i+1] == ch:
			endSegment()
			i++
		case ch == '|' || ch == ';':
			endSegment()
		default:
			open = true
			tok.WriteByte(ch)
		}
	}
	endSegment()
	return segments
}

// isRmpInvocation reports whether a segment's first token names the binary. The
// match is exact, or a path whose final element is exactly "rmp"; a prefix match
// would take `rmp-{version}-{target}.tar.gz` for a command line.
func isRmpInvocation(seg []shellToken) bool {
	if len(seg) == 0 || seg[0].quoted {
		return false
	}
	return seg[0].text == "rmp" || strings.HasSuffix(seg[0].text, "/rmp")
}

// startsWithRmpWord reports whether a segment's first token begins with "rmp"
// without being an invocation — the archive names of trap 2.
func startsWithRmpWord(seg []shellToken) bool {
	if len(seg) == 0 || seg[0].quoted || isRmpInvocation(seg) {
		return false
	}
	return strings.HasPrefix(seg[0].text, "rmp")
}

// ---------------------------------------------------------------------------
// The oracle: the AI Agent Contract
// ---------------------------------------------------------------------------

// exampleSubcommand is one leaf of the command tree, indexed by flag spelling.
type exampleSubcommand struct {
	name       string
	flags      map[string]bool
	flagsOrder []string
}

// exampleCommand is one command family. subs is keyed by canonical name AND by
// every alias, because 65 invocations in the corpus reach a name through an
// alias. flat is non-nil for the families that take no subcommand token.
type exampleCommand struct {
	name     string
	flat     *exampleSubcommand
	subs     map[string]*exampleSubcommand
	subOrder []string
}

// exampleOracle is the whole command surface, in the shape this gate resolves
// against.
type exampleOracle struct {
	commands     map[string]*exampleCommand
	commandOrder []string
	// global holds the top-level flag spellings. They are legal at every level
	// of the tree: SPEC/COMMANDS.md § AI Help states it of --ai-help
	// explicitly, and the corpus uses `rmp task create --ai-help` twice.
	global      map[string]bool
	globalOrder []string
	subcommands int
}

// contractShape is the subtree of the contract this gate reads. `short` is null
// on 24 of the 182 flags, so it is a pointer.
type contractShape struct {
	GlobalFlags []struct {
		Long  string  `json:"long"`
		Short *string `json:"short"`
	} `json:"global_flags"`
	Commands []struct {
		Name        string   `json:"name"`
		Aliases     []string `json:"aliases"`
		Subcommands []struct {
			Name    string   `json:"name"`
			Aliases []string `json:"aliases"`
			Flags   []struct {
				Long  string  `json:"long"`
				Short *string `json:"short"`
			} `json:"flags"`
		} `json:"subcommands"`
	} `json:"commands"`
	SchemaVersion string `json:"schema_version"`
}

// loadExampleOracle generates the contract in-process and indexes it.
func loadExampleOracle(t *testing.T) *exampleOracle {
	t.Helper()

	out, err := Generate(ScopeAll(), testInfo())
	if err != nil {
		t.Fatalf("Generate(ScopeAll()) returned error: %v", err)
	}
	var shape contractShape
	if err := json.Unmarshal(out, &shape); err != nil {
		t.Fatalf("the contract this gate resolves against is not readable as JSON: %v", err)
	}
	if shape.SchemaVersion != SchemaVersion {
		t.Fatalf("the contract declares schema_version %q, this gate was written against %q; re-read the "+
			"shape before trusting anything it reports", shape.SchemaVersion, SchemaVersion)
	}

	oracle := &exampleOracle{
		commands:     make(map[string]*exampleCommand, len(shape.Commands)*2),
		commandOrder: make([]string, 0, len(shape.Commands)),
		global:       make(map[string]bool, len(shape.GlobalFlags)*2),
		globalOrder:  make([]string, 0, len(shape.GlobalFlags)*2),
	}
	for _, f := range shape.GlobalFlags {
		oracle.global[f.Long] = true
		oracle.globalOrder = append(oracle.globalOrder, f.Long)
		if f.Short != nil && *f.Short != "" {
			oracle.global[*f.Short] = true
			oracle.globalOrder = append(oracle.globalOrder, *f.Short)
		}
	}
	sort.Strings(oracle.globalOrder)

	for _, c := range shape.Commands {
		cmd := &exampleCommand{
			name:     c.Name,
			subs:     make(map[string]*exampleSubcommand, len(c.Subcommands)*2),
			subOrder: make([]string, 0, len(c.Subcommands)),
		}
		oracle.commandOrder = append(oracle.commandOrder, c.Name)
		for i := range c.Subcommands {
			s := &c.Subcommands[i]
			sub := &exampleSubcommand{
				name:       s.Name,
				flags:      make(map[string]bool, len(s.Flags)*2),
				flagsOrder: make([]string, 0, len(s.Flags)*2),
			}
			for _, f := range s.Flags {
				sub.flags[f.Long] = true
				sub.flagsOrder = append(sub.flagsOrder, f.Long)
				if f.Short != nil && *f.Short != "" {
					sub.flags[*f.Short] = true
					sub.flagsOrder = append(sub.flagsOrder, *f.Short)
				}
			}
			sort.Strings(sub.flagsOrder)
			cmd.subOrder = append(cmd.subOrder, s.Name)
			cmd.subs[s.Name] = sub
			for _, alias := range s.Aliases {
				cmd.subs[alias] = sub
			}
			oracle.subcommands++
		}
		// A family whose only subcommand carries the family's own name takes no
		// subcommand token: `rmp stats -r x`, `rmp web`, `rmp ai-help`.
		if len(c.Subcommands) == 1 && c.Subcommands[0].Name == c.Name {
			cmd.flat = cmd.subs[c.Name]
		}
		oracle.commands[c.Name] = cmd
		for _, alias := range c.Aliases {
			oracle.commands[alias] = cmd
		}
	}
	return oracle
}

// canonicalNames returns the canonical command names, sorted, for a failure that
// has to tell a reader what does exist.
func (o *exampleOracle) canonicalNames() []string {
	names := make([]string, len(o.commandOrder))
	copy(names, o.commandOrder)
	sort.Strings(names)
	return names
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

// exampleOutcome records how far an invocation was resolved. Everything but
// outcomeResolved describes a line whose flags could not be attributed to a
// subcommand, and each of those has to be accounted for by a floor or an exact
// count.
type exampleOutcome int

const (
	// outcomeResolved: a command and a subcommand were named, so the flags were
	// checked against that subcommand.
	outcomeResolved exampleOutcome = iota
	// outcomeGlobalFlags: `rmp --ai-help`, or `rmp task --help`. There is no
	// subcommand, and the flags are checked against the global set.
	outcomeGlobalFlags
	// outcomeCommandSlotSynopsis: `rmp <command> --ai-help`.
	outcomeCommandSlotSynopsis
	// outcomeSubcommandSlotSynopsis: `rmp audit <subcommand> ...`.
	outcomeSubcommandSlotSynopsis
	// outcomeCommandOnly: `rmp task` with nothing after it.
	outcomeCommandOnly
	// outcomeBare: `rmp` alone.
	outcomeBare
	// outcomeUnresolvedName: a command or subcommand name did not resolve, and a
	// finding says which.
	outcomeUnresolvedName
)

// Finding kinds. A reader of a failure must not have to work out which of the
// three classes fired.
const (
	findingCommand    = "command"
	findingSubcommand = "subcommand"
	findingFlag       = "flag"
)

// exampleFinding is one thing an invocation named that the CLI does not have.
type exampleFinding struct {
	kind string // findingCommand, findingSubcommand or findingFlag
	name string // the token that did not resolve
	on   string // the surface it was resolved against, e.g. "task create"
	// known is the set it had to belong to, sorted, so the failure can show what
	// the writer could have meant.
	known []string
}

// resolve reads one invocation and reports what it named.
func (o *exampleOracle) resolve(seg []shellToken) (exampleOutcome, []exampleFinding) {
	rest := seg[1:]
	if len(rest) == 0 {
		return outcomeBare, nil
	}
	if rest[0].isPlaceholder() {
		return outcomeCommandSlotSynopsis, nil
	}
	if rest[0].isFlag() {
		return outcomeGlobalFlags, o.checkFlags(rest, o.global, o.globalOrder, "rmp")
	}

	cmd, ok := o.commands[rest[0].text]
	if !ok {
		return outcomeUnresolvedName, []exampleFinding{{
			kind:  findingCommand,
			name:  rest[0].text,
			on:    "rmp",
			known: o.canonicalNames(),
		}}
	}
	rest = rest[1:]

	sub := cmd.flat
	if sub == nil {
		switch {
		case len(rest) == 0:
			return outcomeCommandOnly, nil
		case rest[0].isPlaceholder():
			return outcomeSubcommandSlotSynopsis, nil
		case rest[0].isFlag():
			return outcomeGlobalFlags, o.checkFlags(rest, o.global, o.globalOrder, "rmp "+cmd.name)
		}
		s, found := cmd.subs[rest[0].text]
		if !found {
			return outcomeUnresolvedName, []exampleFinding{{
				kind:  findingSubcommand,
				name:  rest[0].text,
				on:    "rmp " + cmd.name,
				known: cmd.subOrder,
			}}
		}
		sub, rest = s, rest[1:]
	}

	surface := "rmp " + cmd.name
	if sub.name != cmd.name {
		surface += " " + sub.name
	}
	return outcomeResolved, o.checkFlags(rest, sub.flags, sub.flagsOrder, surface)
}

// checkFlags reports every flag spelling of a token run that the given set does
// not hold. The global flags are always admitted: SPEC/COMMANDS.md § AI Help
// states that --ai-help is recognised at every level of the command tree, and
// --help is declared on every subcommand anyway. Admitting three extra spellings
// can only make this gate report less, never report a correct example as wrong.
func (o *exampleOracle) checkFlags(toks []shellToken, known map[string]bool, order []string, surface string) []exampleFinding {
	findings := make([]exampleFinding, 0, 1)
	for _, tok := range toks {
		if tok.isEndOfFlags() {
			break
		}
		if !tok.isFlag() {
			continue
		}
		spelling := tok.spelling()
		if known[spelling] || o.global[spelling] {
			continue
		}
		findings = append(findings, exampleFinding{
			kind:  findingFlag,
			name:  spelling,
			on:    surface,
			known: order,
		})
	}
	if len(findings) == 0 {
		return nil
	}
	return findings
}

// countedFlags returns how many flag spellings of a token run this gate actually
// resolved, which is what minExampleFlagSpellings floors.
func countedFlags(toks []shellToken) int {
	n := 0
	for _, tok := range toks {
		if tok.isEndOfFlags() {
			break
		}
		if tok.isFlag() {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// The scan
// ---------------------------------------------------------------------------

// exampleInvocation is one recognised `rmp ...` command line.
type exampleInvocation struct {
	file     string
	text     string // the logical line as written, whitespace flattened
	seg      []shellToken
	line     int
	joined   int
	prompted bool
}

// scanExampleInvocations reads the whole corpus and returns every invocation,
// together with the count of files walked and of continuation lines joined.
func scanExampleInvocations(t *testing.T) (invocations []exampleInvocation, files, joined int) {
	t.Helper()

	paths := exampleCorpusFiles(t)
	invocations = make([]exampleInvocation, 0, 512)
	for _, rel := range paths {
		for _, line := range shellLinesOf(rel, readRepoFile(t, rel)) {
			joined += line.joined
			if !strings.Contains(line.text, "rmp") {
				continue
			}
			body, prompted := prepareShellLine(line.text)
			for _, seg := range splitShellSegments(body) {
				if !isRmpInvocation(seg) {
					continue
				}
				invocations = append(invocations, exampleInvocation{
					file:     line.file,
					text:     strings.Join(strings.Fields(line.text), " "),
					seg:      seg,
					line:     line.line,
					joined:   line.joined,
					prompted: prompted,
				})
			}
		}
	}
	return invocations, len(paths), joined
}

// ---------------------------------------------------------------------------
// 1. Every documented invocation names a command, a subcommand and flags that
//    exist.
// ---------------------------------------------------------------------------

// TestExampleInvocations_NameOnlyThingsTheCLIHas is the gate proper.
func TestExampleInvocations_NameOnlyThingsTheCLIHas(t *testing.T) {
	oracle := loadExampleOracle(t)
	invocations, files, joined := scanExampleInvocations(t)

	if files < minExampleCorpusFiles {
		t.Fatalf("only %d Markdown files were found across %v and %s, want at least %d; the corpus is not "+
			"where this gate assumes it is, so it is about to check nothing and report success",
			files, exampleCorpusDirs, exampleCorpusReadme, minExampleCorpusFiles)
	}

	documents := make(map[string]bool, files)
	reached := make(map[string]bool, oracle.subcommands)
	counts := make(map[exampleOutcome]int, 7)
	flagsChecked, aliasUses := 0, 0

	for i := range invocations {
		inv := &invocations[i]
		documents[inv.file] = true

		// The "$"-prompted transcripts are exempt; the test below proves the
		// exemption hides nothing.
		if inv.prompted {
			continue
		}

		outcome, findings := oracle.resolve(inv.seg)
		counts[outcome]++
		if outcome == outcomeResolved {
			cmd, sub, alias := oracle.surfaceOf(inv.seg)
			reached[cmd+" "+sub] = true
			aliasUses += alias
			flagsChecked += countedFlags(inv.seg[1:])
		}
		for _, f := range findings {
			reportExampleFinding(t, inv, f)
		}
	}

	// Floors. Every one of them fails a gate whose recognition has stopped
	// working, in place of the silent pass such a gate would otherwise produce.
	if len(documents) < minExampleDocuments {
		t.Errorf("invocations were found in only %d documents, want at least %d; the fenced-block scan is "+
			"no longer reading most of the corpus", len(documents), minExampleDocuments)
	}
	if len(invocations) < minExampleInvocations {
		t.Errorf("only %d invocations were recognised across %d files, want at least %d; recognition has "+
			"stopped working and this gate is now checking almost nothing", len(invocations), files,
			minExampleInvocations)
	}
	if counts[outcomeResolved] < minExampleResolved {
		t.Errorf("only %d invocations resolved to a command and a subcommand, want at least %d; the "+
			"remaining %d were classified as something whose flags are never checked\noutcomes: %s",
			counts[outcomeResolved], minExampleResolved, len(invocations)-counts[outcomeResolved],
			outcomeCounts(counts))
	}
	if flagsChecked < minExampleFlagSpellings {
		t.Errorf("only %d flag spellings were checked against a subcommand, want at least %d; the "+
			"tokeniser has stopped producing flags, so an example may now name any flag at all",
			flagsChecked, minExampleFlagSpellings)
	}
	if len(reached) < minExampleSubcommands {
		t.Errorf("the documented examples reached only %d of the %d subcommands the contract declares, "+
			"want at least %d; a scan that has collapsed onto one shape cannot clear this floor",
			len(reached), oracle.subcommands, minExampleSubcommands)
	}
	if aliasUses < minExampleAliasUses {
		t.Errorf("only %d names resolved through an alias, want at least %d; alias resolution is no longer "+
			"exercised, and an example written with an alias would then be reported as a defect",
			aliasUses, minExampleAliasUses)
	}
	if joined < minJoinedContinuations {
		t.Errorf("only %d continuation lines were joined, want at least %d; multi-line examples are no "+
			"longer being assembled, so the flags on their continuation lines are read by nothing",
			joined, minJoinedContinuations)
	}
	if counts[outcomeGlobalFlags] < minExampleGlobalFlagLines {
		t.Errorf("only %d invocations were read as carrying a global flag instead of a subcommand, want at "+
			"least %d; `rmp --ai-help` and `rmp task --help` are no longer recognised as the shapes they are",
			counts[outcomeGlobalFlags], minExampleGlobalFlagLines)
	}

	t.Logf("checked %d invocations over %d of %d files: %s; %d flag spellings, %d subcommands reached, "+
		"%d alias resolutions, %d continuation lines joined",
		len(invocations), len(documents), files, outcomeCounts(counts), flagsChecked, len(reached),
		aliasUses, joined)
}

// surfaceOf re-reads a resolved invocation to report the canonical command and
// subcommand it named and how many of the two names were written as an alias.
func (o *exampleOracle) surfaceOf(seg []shellToken) (command, subcommand string, aliases int) {
	rest := seg[1:]
	cmd := o.commands[rest[0].text]
	if cmd.name != rest[0].text {
		aliases++
	}
	rest = rest[1:]
	if cmd.flat != nil {
		return cmd.name, cmd.flat.name, aliases
	}
	sub := cmd.subs[rest[0].text]
	if sub.name != rest[0].text {
		aliases++
	}
	return cmd.name, sub.name, aliases
}

// reportExampleFinding writes one failure. It names the file, the line, the
// invocation as written, which of the three classes did not resolve, and the set
// the name had to belong to.
func reportExampleFinding(t *testing.T, inv *exampleInvocation, f exampleFinding) {
	t.Helper()

	var b strings.Builder
	b.WriteString("names a ")
	b.WriteString(f.kind)
	b.WriteString(" the CLI does not have.\n  invocation : ")
	b.WriteString(inv.text)
	b.WriteString("\n  unresolved : ")
	b.WriteString(f.kind)
	b.WriteString(" ")
	b.WriteString(strconv.Quote(f.name))
	b.WriteString("\n  resolved as: ")
	b.WriteString(f.on)
	b.WriteString("\n  accepted   : ")
	b.WriteString(strings.Join(f.known, " "))
	b.WriteString("\nEvery `rmp` line printed as an example must be one a reader can type. Fix the ")
	b.WriteString(f.kind)
	b.WriteString(", or add it to the CLI.")
	if inv.joined > 0 {
		b.WriteString("\n  note       : this invocation spans ")
		b.WriteString(strconv.Itoa(inv.joined + 1))
		b.WriteString(" lines joined at a trailing backslash; the line number is the first of them.")
	}
	t.Errorf("%s:%d %s", inv.file, inv.line, b.String())
}

// outcomeCounts renders the outcome tally for a failure message.
func outcomeCounts(counts map[exampleOutcome]int) string {
	labels := [...]struct {
		outcome exampleOutcome
		name    string
	}{
		{outcomeResolved, "resolved"},
		{outcomeGlobalFlags, "global-flag"},
		{outcomeCommandSlotSynopsis, "command-slot synopsis"},
		{outcomeSubcommandSlotSynopsis, "subcommand-slot synopsis"},
		{outcomeCommandOnly, "command only"},
		{outcomeBare, "bare rmp"},
		{outcomeUnresolvedName, "unresolved name"},
	}
	parts := make([]string, 0, len(labels))
	for _, l := range labels {
		if counts[l.outcome] > 0 {
			parts = append(parts, l.name+"="+strconv.Itoa(counts[l.outcome]))
		}
	}
	return strings.Join(parts, ", ")
}

// ---------------------------------------------------------------------------
// 2. The skipped classes are enumerated, and hide nothing.
// ---------------------------------------------------------------------------

// TestExampleInvocations_SkippedClassesHideNoDefect counts the two classes the
// gate does not resolve and proves that neither is a place a defect can sit.
//
// The counts are exact rather than floored on purpose. A skipped line is an
// unchecked example, so the sets may not grow without someone confirming that
// what was added really is a synopsis or a transcript.
func TestExampleInvocations_SkippedClassesHideNoDefect(t *testing.T) {
	oracle := loadExampleOracle(t)
	invocations, _, _ := scanExampleInvocations(t)

	commandSlot := make([]string, 0, exampleCommandSlotSynopses)
	subcommandSlot := make([]string, 0, exampleSubcommandSlotSynopses)
	prompted := make([]string, 0, examplePromptedTranscripts)
	promptedInvalid := make([]string, 0, examplePromptedInvalidByName)

	for i := range invocations {
		inv := &invocations[i]
		where := inv.file + ":" + strconv.Itoa(inv.line) + "  " + inv.text
		if inv.prompted {
			prompted = append(prompted, where)
			if outcome, _ := oracle.resolve(inv.seg); outcome == outcomeUnresolvedName {
				promptedInvalid = append(promptedInvalid, where)
			}
			continue
		}
		switch outcome, _ := oracle.resolve(inv.seg); outcome {
		case outcomeCommandSlotSynopsis:
			commandSlot = append(commandSlot, where)
		case outcomeSubcommandSlotSynopsis:
			subcommandSlot = append(subcommandSlot, where)
		}
	}

	assertExampleClass(t, "synopsis lines whose COMMAND slot is a placeholder", commandSlot,
		exampleCommandSlotSynopses,
		"The command slot holds no name, so there is nothing to resolve. Everything to the left of the "+
			"placeholder is still resolved, so a bogus name in a synopsis is still reported.")
	assertExampleClass(t, "synopsis lines whose SUBCOMMAND slot is a placeholder", subcommandSlot,
		exampleSubcommandSlotSynopses,
		"The command name of each of these IS resolved; only the subcommand slot is skipped. A flat "+
			"command such as `rmp web [options]` is not one of these: it has no subcommand slot, so the "+
			"bracket is an ordinary argument and the line is checked in full.")
	assertExampleClass(t, `"$"-prompted transcripts`, prompted, examplePromptedTranscripts,
		"A prompted line is a transcript of a session printed above its own output, not a line meant to "+
			"be copied. Exempting the prompt is mechanical and needs no annotation in the SPEC.")

	// The exemption that could hide something is the prompted one, because three
	// of those six are invalid BY NAME on purpose, to document exit 127. Pin
	// exactly which: if a fourth prompted line ever fails to resolve, it is a
	// defect that the exemption would otherwise swallow.
	if len(promptedInvalid) != examplePromptedInvalidByName {
		t.Errorf("%d of the %d prompted transcripts fail name resolution, want exactly %d.\n%s\n"+
			"Exactly the three deliberate exit-127 transcripts may fail. A fourth means the prompt "+
			"exemption is now hiding a real defect.",
			len(promptedInvalid), len(prompted), examplePromptedInvalidByName,
			strings.Join(promptedInvalid, "\n"))
	}
	for _, line := range promptedInvalid {
		if !strings.Contains(line, exampleDeliberateUnknownName) {
			t.Errorf("a prompted transcript fails name resolution without being one of the deliberate "+
				"%q examples, so the prompt exemption is hiding a real defect:\n%s",
				exampleDeliberateUnknownName, line)
		}
	}

	t.Logf("skipped %d command-slot synopses, %d subcommand-slot synopses and %d prompted transcripts "+
		"(%d of them invalid by name on purpose)",
		len(commandSlot), len(subcommandSlot), len(prompted), len(promptedInvalid))
}

// assertExampleClass pins the size of one skipped class and names its members
// when the size is wrong, so the reader can judge what changed.
func assertExampleClass(t *testing.T, label string, members []string, want int, why string) {
	t.Helper()
	if len(members) == want {
		return
	}
	t.Errorf("found %d %s, want exactly %d.\n%s\n%s\nEach line in this class is an example this gate does "+
		"NOT check. If the new line is genuinely one of these, update the constant; if it is not, it is a "+
		"defect the count has just caught.",
		len(members), label, want, strings.Join(members, "\n"), why)
}

// ---------------------------------------------------------------------------
// 3. Trap 1: the flat commands.
// ---------------------------------------------------------------------------

// TestExampleInvocations_FlatCommandsTakeNoSubcommandToken pins the three
// families whose only subcommand carries the family's own name. `rmp stats -r x`
// has no subcommand token, and a resolver that always expects `cmd sub` reports
// `-r` as an unknown subcommand on all three.
func TestExampleInvocations_FlatCommandsTakeNoSubcommandToken(t *testing.T) {
	oracle := loadExampleOracle(t)

	flat := make([]string, 0, 3)
	for _, name := range oracle.commandOrder {
		if oracle.commands[name].flat != nil {
			flat = append(flat, name)
		}
	}
	sort.Strings(flat)
	want := []string{"ai-help", "stats", "web"}
	if strings.Join(flat, " ") != strings.Join(want, " ") {
		t.Fatalf("the contract's flat command families are %v, this gate was written for %v; a family that "+
			"has gained or lost a subcommand changes how its examples must be read", flat, want)
	}

	cases := [...]struct {
		line string
		want exampleOutcome
	}{
		{"rmp stats -r backend-platform", outcomeResolved},
		{"rmp web --host 127.0.0.1 --port 8080 --no-open", outcomeResolved},
		{"rmp ai-help", outcomeResolved},
		{"rmp web [options]", outcomeResolved},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			outcome, findings := resolveExampleLine(t, oracle, tc.line)
			if outcome != tc.want || len(findings) > 0 {
				t.Errorf("%q resolved as outcome %d with %d findings, want outcome %d and none.\n%s\n"+
					"A flat command carries no subcommand token; the first argument after the family name "+
					"is already a flag or an argument.",
					tc.line, outcome, len(findings), tc.want, describeFindings(findings))
			}
		})
	}

	// And the negative: a flat family still rejects a flag it does not have.
	if _, findings := resolveExampleLine(t, oracle, "rmp stats --nonexistent"); len(findings) != 1 ||
		findings[0].kind != findingFlag {
		t.Errorf("`rmp stats --nonexistent` produced %d findings (%s), want exactly one flag finding; "+
			"the flat-command path must still check flags", len(findings), describeFindings(findings))
	}
}

// resolveExampleLine runs one literal line through the whole pipeline, so a test
// exercises the same code the corpus scan does rather than a shortcut past it.
func resolveExampleLine(t *testing.T, oracle *exampleOracle, line string) (exampleOutcome, []exampleFinding) {
	t.Helper()
	body, _ := prepareShellLine(line)
	for _, seg := range splitShellSegments(body) {
		if isRmpInvocation(seg) {
			return oracle.resolve(seg)
		}
	}
	t.Fatalf("%q was not recognised as an rmp invocation at all", line)
	return outcomeBare, nil
}

// describeFindings renders findings compactly for a failure message.
func describeFindings(findings []exampleFinding) string {
	if len(findings) == 0 {
		return "  (no findings)"
	}
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, "  "+f.kind+" "+strconv.Quote(f.name)+" on "+f.on)
	}
	return strings.Join(parts, "\n")
}

// ---------------------------------------------------------------------------
// 4. Trap 2: the archive name is not an invocation.
// ---------------------------------------------------------------------------

// TestExampleInvocations_ArchiveNamesAreNotInvocations pins the exact-match rule
// from both sides: the unit cases below, and a floor on the corpus lines that
// would be swallowed by a prefix match.
func TestExampleInvocations_ArchiveNamesAreNotInvocations(t *testing.T) {
	cases := [...]struct {
		line string
		want bool
	}{
		{"rmp task list -r backend-platform", true},
		{"./bin/rmp task list -r backend-platform", true},
		{"/usr/local/bin/rmp --version", true},
		{"rmp-{version}-{target}.tar.gz", false},
		{"rmp-{version}-{os}-{arch}.{ext}", false},
		{"rmp-v1.0.0-linux-amd64.tar.gz", false},
		{"./bin/rmp-linux-amd64 task list", false},
		{"rmpx task list", false},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			body, _ := prepareShellLine(tc.line)
			got := false
			for _, seg := range splitShellSegments(body) {
				if isRmpInvocation(seg) {
					got = true
				}
			}
			if got != tc.want {
				t.Errorf("isRmpInvocation(%q) = %v, want %v\nThe first token must be exactly `rmp` or a "+
					"path whose last element is exactly `rmp`. A word-boundary match on ^rmp swallows the "+
					"release archive names, and a released-artefact line then has to resolve as a command.",
					tc.line, got, tc.want)
			}
		})
	}

	// The corpus side: the archive names really are there to be swallowed.
	paths := exampleCorpusFiles(t)
	nearMisses := make([]string, 0, minArchiveNameLines)
	for _, rel := range paths {
		for _, line := range shellLinesOf(rel, readRepoFile(t, rel)) {
			if !strings.Contains(line.text, "rmp") {
				continue
			}
			body, _ := prepareShellLine(line.text)
			for _, seg := range splitShellSegments(body) {
				if startsWithRmpWord(seg) {
					nearMisses = append(nearMisses, rel+":"+strconv.Itoa(line.line)+"  "+seg[0].text)
				}
			}
		}
	}
	if len(nearMisses) < minArchiveNameLines {
		t.Errorf("only %d lines inside shell fences open with a token that starts with \"rmp\" without "+
			"being an invocation, want at least %d; with none of them left in the tree the exact-match "+
			"rule is exercised by nothing but the cases above\nfound: %v",
			len(nearMisses), minArchiveNameLines, nearMisses)
	}
	t.Logf("rejected %d rmp-prefixed non-invocations: %v", len(nearMisses), nearMisses)
}

// ---------------------------------------------------------------------------
// 5. Trap 3: placeholders are masked before redirections are stripped.
// ---------------------------------------------------------------------------

// TestExampleInvocations_PlaceholdersAreMaskedBeforeRedirectionsAreStripped pins
// the order of the last two stages of the pipeline.
//
// This is the trap that matters most, because getting it wrong does not weaken
// the gate — it makes the gate report CORRECT synopsis lines as defects. A
// redirection stripper reads the `<` of `rmp <command> --ai-help` as an input
// redirection and takes the rest of the line with it; what is left of
// `rmp <family> <sub> -r <roadmap> [...]` is `rmp -r ...`, whose first token is
// then a flag that no global-flag set contains.
func TestExampleInvocations_PlaceholdersAreMaskedBeforeRedirectionsAreStripped(t *testing.T) {
	cases := [...]struct {
		name string
		line string
		want string
	}{
		{
			name: "an angle placeholder is a placeholder, not a redirection",
			line: "rmp <command> --ai-help",
			want: "rmp PLACEHOLDER --ai-help",
		},
		{
			name: "and so is every one of them on a synopsis line",
			line: "rmp <family> <sub> -r <roadmap> [...]",
			want: "rmp PLACEHOLDER PLACEHOLDER -r PLACEHOLDER PLACEHOLDER",
		},
		{
			name: "a real input redirection is still removed",
			line: "rmp task comment-add -r project1 42 --type DECISION < decision.txt",
			want: "rmp task comment-add -r project1 42 --type DECISION",
		},
		{
			name: "a placeholder and a redirection on the same line",
			line: "rmp task comment-add -r <name> <task-id> --type FINDING < finding.txt",
			want: "rmp task comment-add -r PLACEHOLDER PLACEHOLDER --type FINDING",
		},
		{
			name: "output redirection and descriptor duplication, after the control peel",
			line: "if rmp task list -r myproject > /dev/null 2>&1; then",
			want: "rmp task list -r myproject",
		},
		{
			name: "a bracket placeholder needs no masking order but must still mask",
			line: "rmp task [subcommand] [arguments] [flags]",
			want: "rmp task PLACEHOLDER PLACEHOLDER PLACEHOLDER",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := prepareShellLine(tc.line)
			if strings.Join(strings.Fields(got), " ") != tc.want {
				t.Errorf("prepareShellLine(%q) = %q, want %q\nPlaceholders must be masked BEFORE "+
					"redirections are stripped. Swapping the two stages makes a correct synopsis line "+
					"resolve to something the CLI does not have, which is the one failure mode this gate "+
					"must never produce.", tc.line, got, tc.want)
			}
		})
	}

	// The same claim from the other direction: with the order reversed the
	// synopsis line loses its names. Reversing it here proves the order is doing
	// work, rather than being an accident that happens to hold.
	reversed := stripShellRedirections(maskShellPlaceholders("rmp <family> <sub> -r <roadmap> [...]"))
	wrongWay := maskShellPlaceholders(stripShellRedirections("rmp <family> <sub> -r <roadmap> [...]"))
	if strings.Join(strings.Fields(reversed), " ") == strings.Join(strings.Fields(wrongWay), " ") {
		t.Error("masking and redirection-stripping commute on `rmp <family> <sub> -r <roadmap> [...]`, " +
			"which they must not; if they did, the ordering pinned above would be pinning nothing")
	}
}

// ---------------------------------------------------------------------------
// 6. The recognition rules themselves.
// ---------------------------------------------------------------------------

// TestExampleInvocations_RecognitionRules pins each clause of the recognition
// rule with a case taken from, or shaped like, the corpus.
func TestExampleInvocations_RecognitionRules(t *testing.T) {
	t.Run("prompt", func(t *testing.T) {
		body, prompted := prepareShellLine("$ rmp task nadadisto -r project1")
		if !prompted || body != "rmp task nadadisto -r project1" {
			t.Errorf(`prepareShellLine("$ rmp ...") = (%q, %v), want the line without the prompt and true`,
				body, prompted)
		}
		if _, prompted := prepareShellLine("rmp task list -r project1"); prompted {
			t.Error("an unprompted line was reported as prompted; the exemption would then swallow the corpus")
		}
	})

	t.Run("comment", func(t *testing.T) {
		cases := [...]struct{ line, want string }{
			{"rmp task create -t \"New task\"   # Exits 3: no roadmap given", `rmp task create -t "New task"`},
			{"rmp task list -r p --body \"issue #42\"", `rmp task list -r p --body "issue #42"`},
			{"# a whole-line comment", ""},
			{"rmp task list -r sharp#name", "rmp task list -r sharp#name"},
		}
		for _, tc := range cases {
			body, _ := prepareShellLine(tc.line)
			if body != tc.want {
				t.Errorf("stripping the comment of %q gave %q, want %q; a hash must open a word and must "+
					"not be quoted", tc.line, body, tc.want)
			}
		}
	})

	t.Run("segments", func(t *testing.T) {
		cases := [...]struct {
			line  string
			first string
			count int
		}{
			{`rmp --ai-help | jq '.pitfalls[] | .id'`, "rmp", 2},
			{"rmp task list -r p && rmp sprint list -r p", "rmp", 2},
			{"rmp task list -r p; rmp stats -r p", "rmp", 2},
			{`rmp sprint c-edit -r p 4 -b "Dropped both; they move on."`, "rmp", 1},
		}
		for _, tc := range cases {
			body, _ := prepareShellLine(tc.line)
			segs := splitShellSegments(body)
			if len(segs) != tc.count || len(segs[0]) == 0 || segs[0][0].text != tc.first {
				t.Errorf("splitting %q gave %d segments (first token %q), want %d starting with %q; the "+
					"split must respect quotes, or a value carrying a semicolon truncates its invocation",
					tc.line, len(segs), segmentHead(segs), tc.count, tc.first)
			}
		}
	})

	t.Run("quoted values are not flags", func(t *testing.T) {
		body, _ := prepareShellLine(`rmp task create -r p -t "use --verbose instead" -fr "a - b"`)
		segs := splitShellSegments(body)
		if len(segs) != 1 {
			t.Fatalf("expected one segment, got %d", len(segs))
		}
		flags := make([]string, 0, 4)
		for _, tok := range segs[0] {
			if tok.isFlag() {
				flags = append(flags, tok.text)
			}
		}
		want := "-r -t -fr"
		if strings.Join(flags, " ") != want {
			t.Errorf("the flags of a line whose values contain dashes were read as %v, want %s\nA word "+
				"inside quotes is a value. Reading `--verbose` there as a flag would report a correct "+
				"example as a defect, and a lone `-` inside prose would be reported as an unknown flag.",
				flags, want)
		}
	})

	t.Run("flag spelling stops at =", func(t *testing.T) {
		tok := shellToken{text: "--roadmap=backend-platform"}
		if got := tok.spelling(); got != "--roadmap" {
			t.Errorf("spelling of %q = %q, want %q", tok.text, got, "--roadmap")
		}
	})

	t.Run("fenced blocks", func(t *testing.T) {
		doc := "prose\n" +
			"```bash\n" +
			"rmp task list -r p\n" +
			"```\n" +
			"```json\n" +
			"rmp not-a-command\n" +
			"```\n" +
			"```\n" +
			"$ rmp stats -r p\n" +
			"```\n" +
			"rmp outside-a-fence\n" +
			"```sh\n" +
			"rmp task create -r p \\\n" +
			"  -t \"Title\" \\\n" +
			"  -fr \"Why\"\n" +
			"```\n"
		lines := shellLinesOf("synthetic.md", doc)
		got := make([]string, 0, len(lines))
		joined := 0
		for _, l := range lines {
			got = append(got, strconv.Itoa(l.line)+":"+strings.Join(strings.Fields(l.text), " "))
			joined += l.joined
		}
		want := []string{
			"3:rmp task list -r p",
			"9:$ rmp stats -r p",
			`13:rmp task create -r p -t "Title" -fr "Why"`,
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("shellLinesOf read %v, want %v\nOnly shell-ish and untagged fences are read, prose "+
				"outside a fence is not a command line, and a trailing backslash continues the line.",
				got, want)
		}
		if joined != 2 {
			t.Errorf("the continuation join folded %d lines, want 2", joined)
		}
	})

	t.Run("a continuation does not cross a fence", func(t *testing.T) {
		doc := "```bash\n" +
			"rmp task create -r p -t \"Title\" \\\n" +
			"```\n" +
			"```bash\n" +
			"rmp task get -r p 5\n" +
			"```\n"
		lines := shellLinesOf("synthetic.md", doc)
		if len(lines) != 2 {
			t.Fatalf("a dangling backslash before a closing fence produced %d logical lines, want 2; it "+
				"must not splice the next block's first example onto the end of this one: %v", len(lines), lines)
		}
		if strings.Join(strings.Fields(lines[1].text), " ") != "rmp task get -r p 5" {
			t.Errorf("the second block's example was read as %q, want it intact", lines[1].text)
		}
	})
}

// segmentHead renders the first token of the first segment, for a failure
// message that must not panic on an empty split.
func segmentHead(segs [][]shellToken) string {
	if len(segs) == 0 || len(segs[0]) == 0 {
		return ""
	}
	return segs[0][0].text
}

// ---------------------------------------------------------------------------
// 7. The oracle has the shape this gate was written against.
// ---------------------------------------------------------------------------

// TestExampleInvocations_OracleShape pins what the gate assumes about the
// contract. A gate that resolved against an empty oracle would report every
// example as a defect; one that resolved against an oracle it had misread would
// report the wrong ones.
func TestExampleInvocations_OracleShape(t *testing.T) {
	oracle := loadExampleOracle(t)

	const (
		wantCommands = 9
		// 57: 59 minus the five `graph` subcommands that collapsed onto one, plus
		// the three the family publishes now — `graph execute`, `graph serve` and
		// `graph client` (SPEC/COMMANDS.md § Graph Management).
		wantSubcommands = 57
	)
	if len(oracle.commandOrder) != wantCommands {
		t.Errorf("the contract declares %d command families, this gate was written against %d: %v",
			len(oracle.commandOrder), wantCommands, oracle.commandOrder)
	}
	if oracle.subcommands != wantSubcommands {
		t.Errorf("the contract declares %d subcommands, this gate was written against %d",
			oracle.subcommands, wantSubcommands)
	}
	if !oracle.global["--ai-help"] {
		t.Error("--ai-help is not among the contract's global flags. It is what makes " +
			"`rmp task create --ai-help` a valid documented example, and SPEC/COMMANDS.md § AI Help " +
			"states that it is recognised at every level of the command tree")
	}

	// Aliases must index the same subcommand as the canonical name, because 65
	// documented invocations arrive through one.
	task, ok := oracle.commands["t"]
	if !ok || task.name != "task" {
		t.Fatalf("the command alias `t` does not resolve to `task`; alias resolution is not wired")
	}
	if task.subs["ls"] == nil || task.subs["ls"] != task.subs["list"] {
		t.Error("the subcommand alias `ls` does not resolve to the same subcommand as `list`")
	}

	// Flag indexing, from both sides. `short` is null on 24 of the 182 flags, so
	// a gate that read it blindly would either panic or invent a spelling.
	spellings := 0
	for _, name := range oracle.commandOrder {
		for _, subName := range oracle.commands[name].subOrder {
			spellings += len(oracle.commands[name].subs[subName].flagsOrder)
		}
	}
	const minFlagSpellings = 120
	if spellings < minFlagSpellings {
		t.Errorf("only %d flag spellings were indexed across the whole surface, want at least %d; with an "+
			"empty index every documented flag would be reported as unknown", spellings, minFlagSpellings)
	}
	create := oracle.commands["task"].subs["create"]
	if !create.flags["--functional-requirements"] || !create.flags["-fr"] {
		t.Error("`task create` did not index both spellings of --functional-requirements/-fr; short " +
			"spellings are what most of the documented examples are written with")
	}
	if !create.flags["--severity"] || create.flags["-s"] {
		t.Error("`task create` declares --severity with a null short form. The long spelling must be " +
			"indexed and no short one invented; a `-s` accepted here would let a wrong example pass")
	}
	if oracle.commands["task"].flat != nil {
		t.Error("`task` was classified as a flat command; only families whose single subcommand carries " +
			"the family name are flat")
	}
}

// ---------------------------------------------------------------------------
// 8. The asymmetry the whole rule rests on.
// ---------------------------------------------------------------------------

// TestExampleInvocations_ContractExamplesFailOnValuesRatherThanNames keeps the
// justification for the name-and-flag rule honest.
//
// The rule checks names and flag spellings and nothing else. That is safe only
// because a documented FAILURE example fails on a value or a missing flag rather
// than on a name — otherwise a gate that checked more would report deliberate,
// correct examples as defects, and a gate that checked names would report the
// failure examples themselves. The contract's own 153 examples are the largest
// body of evidence for that, and this test re-derives it on every run instead of
// trusting the measurement that produced the rule.
func TestExampleInvocations_ContractExamplesFailOnValuesRatherThanNames(t *testing.T) {
	oracle := loadExampleOracle(t)

	out, err := Generate(ScopeAll(), testInfo())
	if err != nil {
		t.Fatalf("Generate(ScopeAll()) returned error: %v", err)
	}
	var shape struct {
		Commands []struct {
			Subcommands []struct {
				Examples []struct {
					Title string `json:"title"`
					Cmd   string `json:"cmd"`
					Exit  int    `json:"exit"`
				} `json:"examples"`
			} `json:"subcommands"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(out, &shape); err != nil {
		t.Fatalf("reading the contract's examples: %v", err)
	}

	const (
		minContractExamples = 120
		minFailureExamples  = 50
		// `rmp web --foo`, titled "Unknown flag", is the single documented
		// failure that fails by a NAME. It demonstrates the exit code for an
		// unknown flag, so it is the one example whose whole point is the thing
		// this gate reports.
		wantNameFailures = 1
	)

	total, failures := 0, 0
	nameFailures := make([]string, 0, wantNameFailures)
	for _, cmd := range shape.Commands {
		for _, sub := range cmd.Subcommands {
			for _, ex := range sub.Examples {
				total++
				if ex.Exit != 0 {
					failures++
				}
				body, _ := prepareShellLine(ex.Cmd)
				for _, seg := range splitShellSegments(body) {
					if !isRmpInvocation(seg) {
						continue
					}
					outcome, findings := oracle.resolve(seg)
					if outcome == outcomeUnresolvedName || len(findings) > 0 {
						nameFailures = append(nameFailures,
							ex.Cmd+"  (exit "+strconv.Itoa(ex.Exit)+", "+ex.Title+")"+
								"\n"+describeFindings(findings))
					}
				}
			}
		}
	}

	if total < minContractExamples {
		t.Fatalf("only %d contract examples were read, want at least %d; the evidence this rule rests on "+
			"is no longer being gathered", total, minContractExamples)
	}
	if failures < minFailureExamples {
		t.Errorf("only %d of the %d contract examples declare a non-zero exit, want at least %d; without "+
			"them this test proves nothing about what a documented failure fails on",
			failures, total, minFailureExamples)
	}
	if len(nameFailures) != wantNameFailures {
		t.Errorf("%d of the %d contract examples name a command, subcommand or flag the CLI does not "+
			"have, want exactly %d:\n%s\nEvery documented failure must fail on a VALUE or a MISSING FLAG. "+
			"A new one that fails on a NAME means the rule this gate implements — check names and flag "+
			"spellings, nothing else — has stopped being safe for the documentation corpus too.",
			len(nameFailures), total, wantNameFailures, strings.Join(nameFailures, "\n"))
	}

	t.Logf("read %d contract examples, %d of them declaring a non-zero exit; %d fail by name",
		total, failures, len(nameFailures))
}
