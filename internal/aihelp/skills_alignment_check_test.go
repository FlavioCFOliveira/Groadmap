// Package aihelp — agent-skills alignment checker.
//
// The repository ships two Claude Code skills under skills/ that teach an
// agent to operate rmp. SPEC/SKILLS.md § Alignment Invariant requires them to
// stay aligned with the binary built from the same commit, judged against the
// AI Agent Contract this package generates. This file holds the checker; the
// tests that run it against the real tree and against fixtures live in
// skills_alignment_test.go.
//
// The checker is deliberately confined to _test.go files: it is a build-time
// gate, not part of the binary, so it adds nothing to the command surface.
//
// # What it enforces
//
//   - SPEC/SKILLS.md § Location and Identity items 1, 2, 3 and 5: the two skill
//     directories and nothing else, a SKILL.md with frontmatter whose name
//     matches the directory and whose description is non-empty, and a
//     non-empty file set.
//   - § Version Declaration: exactly one `  rmp-version: "X.Y.Z"` line, inside
//     the frontmatter's `metadata:` map, equal to the binary version.
//   - § Rules 1 to 4: every in-scope subcommand invoked with canonical names,
//     every in-scope long flag present, every invocation resolvable, and no
//     invocation outside the skill's scope other than `rmp ai-help`.
//
// # What counts as an invocation
//
// Exactly § Invocation: in a .md file only text inside fenced code blocks and
// inline code spans is scanned; in any other file the whole text is. The word
// `rmp` (not touching a letter, digit, `_` or `-`), whitespace, a command token
// matching [a-z][a-z0-9-]*, and — unless that token names a single-action
// command — whitespace and a subcommand token of the same shape.

package aihelp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// skillScope names one shipped skill and the commands SPEC/SKILLS.md
// § Scope Partition assigns to it.
type skillScope struct {
	name     string
	commands []string
}

// skillScopes is the partition of SPEC/SKILLS.md § Scope Partition, in the
// order the specification lists the skills.
var skillScopes = []skillScope{
	{name: "roadmap-manager", commands: []string{"roadmap", "task", "sprint", "backlog", "stats", "audit"}},
	{name: "knowledge-authority", commands: []string{"graph"}},
}

// skillSharedCommand is the one command both skills may invoke whatever their
// scope (SPEC/SKILLS.md § Scope Partition, rule 1).
const skillSharedCommand = "ai-help"

// skillVersionLine is the fixed, machine-readable form of the version
// declaration (SPEC/SKILLS.md § Version Declaration).
var skillVersionLine = regexp.MustCompile(`^  rmp-version: "([0-9]+\.[0-9]+\.[0-9]+)"$`)

// skillVersionKey matches any line that tries to declare the version, well
// formed or not, so a malformed or duplicated declaration is reported rather
// than overlooked.
var skillVersionKey = regexp.MustCompile(`^\s*rmp-version\s*:`)

// skillCLI indexes the whole-CLI contract by every spelling a command token may
// take: a command's canonical name and each of its aliases.
type skillCLI struct {
	byToken map[string]*CommandEntry
}

func newSkillCLI(c *Contract) *skillCLI {
	idx := &skillCLI{byToken: make(map[string]*CommandEntry, len(c.Commands)*2)}
	for i := range c.Commands {
		cmd := &c.Commands[i]
		idx.byToken[cmd.Name] = cmd
		for _, a := range cmd.Aliases {
			idx.byToken[a] = cmd
		}
	}
	return idx
}

// command returns the command named, canonically, by name, or nil.
func (x *skillCLI) command(name string) *CommandEntry {
	cmd := x.byToken[name]
	if cmd == nil || cmd.Name != name {
		return nil
	}
	return cmd
}

// isSingleAction reports whether cmd is a single-action command: exactly one
// subcommand, named as the command itself (SPEC/DATA_FORMATS.md § Single-action
// commands (no subcommands)).
func isSingleAction(cmd *CommandEntry) bool {
	return len(cmd.Subcommands) == 1 && cmd.Subcommands[0].Name == cmd.Name
}

// subcommandByToken resolves tok against cmd's subcommands by name or alias.
func subcommandByToken(cmd *CommandEntry, tok string) *SubcommandEntry {
	for i := range cmd.Subcommands {
		s := &cmd.Subcommands[i]
		if s.Name == tok {
			return s
		}
		for _, a := range s.Aliases {
			if a == tok {
				return s
			}
		}
	}
	return nil
}

// skillSegment is a run of recognised text: one line, or the inside of one
// inline code span, with the 1-based line it sits on.
type skillSegment struct {
	text string
	line int
}

// skillInvocation is one invocation found in a file of a skill's file set.
type skillInvocation struct {
	cmd     *CommandEntry    // nil when the command token resolves to nothing
	sub     *SubcommandEntry // nil when unresolved; the sole subcommand when single-action
	file    string           // path relative to the skills directory, slash-separated
	cmdTok  string
	subTok  string // empty for a single-action command
	line    int
	single  bool
	resolve bool
}

// text renders the invocation as it was written, normalised to single spaces.
func (inv *skillInvocation) text() string {
	if inv.subTok == "" {
		return "rmp " + inv.cmdTok
	}
	return "rmp " + inv.cmdTok + " " + inv.subTok
}

// canonical reports whether the invocation is written with the canonical name
// of its command and, unless single-action, of its subcommand.
func (inv *skillInvocation) canonical() bool {
	if !inv.resolve || inv.cmdTok != inv.cmd.Name {
		return false
	}
	return inv.single || inv.subTok == inv.sub.Name
}

// recognisedSegments returns the text of a file in which invocations are
// recognised (SPEC/SKILLS.md § Where Invocations Are Recognised).
func recognisedSegments(name, content string) []skillSegment {
	lines := strings.Split(content, "\n")
	segs := make([]skillSegment, 0, len(lines))
	if !strings.HasSuffix(name, ".md") {
		for i, l := range lines {
			segs = append(segs, skillSegment{text: l, line: i + 1})
		}
		return segs
	}

	var fenceChar byte
	fenceLen := 0
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if fenceLen > 0 {
			if c, n := skillFenceRun(l); c == fenceChar && n >= fenceLen {
				fenceLen = 0
				continue
			}
			segs = append(segs, skillSegment{text: l, line: i + 1})
			continue
		}
		if c, n := skillFenceRun(l); n >= 3 {
			fenceChar, fenceLen = c, n
			continue
		}
		for _, span := range skillInlineCodeSpans(l) {
			segs = append(segs, skillSegment{text: span, line: i + 1})
		}
	}
	return segs
}

// skillFenceRun returns the fence character and the length of the run that opens
// the line after at most three spaces, or a zero length when the line does not
// begin with a run of backticks or tildes.
func skillFenceRun(l string) (byte, int) {
	i := 0
	for i < len(l) && i < 3 && l[i] == ' ' {
		i++
	}
	if i >= len(l) || (l[i] != '`' && l[i] != '~') {
		return 0, 0
	}
	c := l[i]
	n := 0
	for i+n < len(l) && l[i+n] == c {
		n++
	}
	return c, n
}

// skillInlineCodeSpans returns the contents of every inline code span on a line: the
// text between a run of backticks and the next run of exactly as many backticks.
// A run with no closing partner is literal text.
func skillInlineCodeSpans(l string) []string {
	var spans []string
	i := 0
	for i < len(l) {
		if l[i] != '`' {
			i++
			continue
		}
		openEnd := skillBacktickRunEnd(l, i)
		n := openEnd - i
		j := openEnd
		closed := false
		for j < len(l) {
			if l[j] != '`' {
				j++
				continue
			}
			end := skillBacktickRunEnd(l, j)
			if end-j == n {
				spans = append(spans, l[openEnd:j])
				i = end
				closed = true
				break
			}
			j = end
		}
		if !closed {
			i = openEnd
		}
	}
	return spans
}

func skillBacktickRunEnd(l string, i int) int {
	for i < len(l) && l[i] == '`' {
		i++
	}
	return i
}

// isSkillWordRune reports whether r glues onto an adjacent word: a letter, a
// digit, '_' or '-' (SPEC/SKILLS.md § What an Invocation Is, step 1, and
// § Rules, rule 2).
func isSkillWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// standsAlone reports whether s[start:end] is neither preceded nor followed by
// a word rune.
func standsAlone(s string, start, end int) bool {
	if start > 0 {
		if r, _ := utf8.DecodeLastRuneInString(s[:start]); isSkillWordRune(r) {
			return false
		}
	}
	if end < len(s) {
		if r, _ := utf8.DecodeRuneInString(s[end:]); isSkillWordRune(r) {
			return false
		}
	}
	return true
}

// skipBlanks returns the index of the first byte at or after i that is not a
// space or a tab.
func skipBlanks(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

// skillToken returns the longest run at i matching [a-z][a-z0-9-]*.
func skillToken(s string, i int) string {
	if i >= len(s) || s[i] < 'a' || s[i] > 'z' {
		return ""
	}
	j := i + 1
	for j < len(s) {
		c := s[j]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			j++
			continue
		}
		break
	}
	return s[i:j]
}

// findInvocations returns every invocation in one recognised segment.
func findInvocations(cli *skillCLI, seg skillSegment, file string) []skillInvocation {
	var out []skillInvocation
	s := seg.text
	for i := 0; i < len(s); {
		k := strings.Index(s[i:], "rmp")
		if k < 0 {
			break
		}
		p := i + k
		i = p + 3
		if !standsAlone(s, p, p+3) {
			continue
		}
		c := skipBlanks(s, p+3)
		if c == p+3 {
			continue
		}
		cmdTok := skillToken(s, c)
		if cmdTok == "" {
			continue
		}
		inv := skillInvocation{file: file, line: seg.line, cmdTok: cmdTok, cmd: cli.byToken[cmdTok]}
		if inv.cmd != nil && isSingleAction(inv.cmd) {
			inv.single, inv.resolve = true, true
			inv.sub = &inv.cmd.Subcommands[0]
			out = append(out, inv)
			i = c + len(cmdTok)
			continue
		}
		after := c + len(cmdTok)
		sc := skipBlanks(s, after)
		if sc == after {
			continue // a command mention, not an invocation
		}
		subTok := skillToken(s, sc)
		if subTok == "" {
			continue // a command mention, not an invocation
		}
		inv.subTok = subTok
		if inv.cmd != nil {
			inv.sub = subcommandByToken(inv.cmd, subTok)
			inv.resolve = inv.sub != nil
		}
		out = append(out, inv)
		i = sc + len(subTok)
	}
	return out
}

// containsStandalone reports whether needle occurs in s neither preceded nor
// followed by a word rune (SPEC/SKILLS.md § Rules, rule 2).
func containsStandalone(s, needle string) bool {
	for i := 0; ; {
		k := strings.Index(s[i:], needle)
		if k < 0 {
			return false
		}
		p := i + k
		if standsAlone(s, p, p+len(needle)) {
			return true
		}
		i = p + 1
	}
}

// skillFile is one file of a skill's file set.
type skillFile struct {
	rel     string // relative to the skills directory, slash-separated
	content string
}

// readSkillFileSet returns every regular file under dir, recursively, excluding
// any path with a component beginning with '.' (SPEC/SKILLS.md § Location and
// Identity, item 5), sorted by path.
func readSkillFileSet(skillsDir, skill string) ([]skillFile, error) {
	root := filepath.Join(skillsDir, skill)
	var files []skillFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(skillsDir, path)
		if err != nil {
			return err
		}
		files = append(files, skillFile{rel: filepath.ToSlash(rel), content: string(b)})
		return nil
	})
	sort.Slice(files, func(a, b int) bool { return files[a].rel < files[b].rel })
	return files, err
}

// skillFrontmatter is what the checker reads from a SKILL.md frontmatter.
type skillFrontmatter struct {
	name        string
	description string
	version     string // the declared version, when the declaration is well formed
	versionErr  string // why the declaration is missing or malformed
	hasName     bool
}

// parseSkillFrontmatter reads the frontmatter of a SKILL.md: the first line is
// `---`, and it ends at the next line that is exactly `---`. It returns false
// when the file has no frontmatter.
func parseSkillFrontmatter(content string) (skillFrontmatter, bool) {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if len(lines) == 0 || lines[0] != "---" {
		return skillFrontmatter{}, false
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return skillFrontmatter{}, false
	}
	fm := lines[1:end]

	var out skillFrontmatter
	for i, l := range fm {
		switch {
		case strings.HasPrefix(l, "name:"):
			out.hasName = true
			out.name = unquoteYAMLScalar(strings.TrimSpace(strings.TrimPrefix(l, "name:")))
		case strings.HasPrefix(l, "description:"):
			out.description = yamlValue(strings.TrimSpace(strings.TrimPrefix(l, "description:")), fm[i+1:])
		}
	}
	out.version, out.versionErr = versionDeclaration(fm)
	return out, true
}

// yamlValue returns the value of a top-level key: the inline scalar, or, for a
// block scalar or an empty inline value, the indented lines that follow.
func yamlValue(inline string, rest []string) string {
	if inline != "" && inline[0] != '|' && inline[0] != '>' {
		return strings.TrimSpace(unquoteYAMLScalar(inline))
	}
	var b strings.Builder
	for _, l := range rest {
		if l != "" && l[0] != ' ' && l[0] != '\t' {
			break
		}
		b.WriteString(strings.TrimSpace(l))
		b.WriteByte(' ')
	}
	return strings.TrimSpace(b.String())
}

// unquoteYAMLScalar strips one pair of matching single or double quotes.
func unquoteYAMLScalar(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// versionDeclaration returns the declared version, or the reason the
// declaration is missing or malformed (SPEC/SKILLS.md § Version Declaration).
func versionDeclaration(fm []string) (version, problem string) {
	var keyLines []int
	metadata := -1
	metadataCount := 0
	for i, l := range fm {
		if l == "metadata:" {
			metadataCount++
			metadata = i
		}
		if skillVersionKey.MatchString(l) {
			keyLines = append(keyLines, i)
		}
	}
	switch {
	case len(keyLines) == 0:
		return "", "the frontmatter carries no rmp-version line"
	case len(keyLines) > 1:
		return "", fmt.Sprintf("the frontmatter carries %d rmp-version lines; exactly one is allowed", len(keyLines))
	case metadataCount != 1:
		return "", fmt.Sprintf("the frontmatter carries %d unindented `metadata:` lines; exactly one is required", metadataCount)
	}
	at := keyLines[0]
	m := skillVersionLine.FindStringSubmatch(fm[at])
	if m == nil {
		return "", fmt.Sprintf("the line %q does not match the form `  rmp-version: \"X.Y.Z\"`", fm[at])
	}
	if at < metadata {
		return "", "the rmp-version line precedes the `metadata:` line"
	}
	for _, l := range fm[metadata+1 : at] {
		if l != "" && l[0] != ' ' {
			return "", "the rmp-version line is not inside the `metadata:` map"
		}
	}
	return m[1], ""
}

// readBinaryVersion returns the string value of the `version` constant
// declared in the Go source file at path (cmd/rmp/main.go).
func readBinaryVersion(path string) (string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, n := range vs.Names {
				if n.Name != "version" || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return "", fmt.Errorf("%s: the version constant is not a string literal", path)
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					return "", fmt.Errorf("%s: unquote version: %w", path, err)
				}
				if v == "" {
					return "", fmt.Errorf("%s: the version constant is empty", path)
				}
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("%s: no `version` string constant is declared", path)
}

// checkSkillAlignment checks the skills directory against the contract and the
// binary version, and returns one message per violation (none when aligned).
func checkSkillAlignment(skillsDir string, contract *Contract, binaryVersion string) []string {
	var v []string
	report := func(skill, rule, format string, args ...any) {
		v = append(v, fmt.Sprintf("%s (skills/%s/): %s: %s", skill, skill, rule, fmt.Sprintf(format, args...)))
	}

	if binaryVersion == "" {
		v = append(v, "the binary version could not be read, so no declared version can be judged")
	}

	cli := newSkillCLI(contract)
	inScopeTotal := 0
	for _, sc := range skillScopes {
		for _, name := range sc.commands {
			if cli.command(name) != nil {
				inScopeTotal++
			}
		}
	}
	if inScopeTotal == 0 {
		v = append(v, "the contract yields no command in either skill's scope; the check would be vacuous")
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return append(v, fmt.Sprintf("skills/: Location and Identity item 1: the directory cannot be read: %v", err))
	}
	expected := make(map[string]bool, len(skillScopes))
	for _, sc := range skillScopes {
		expected[sc.name] = true
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || expected[e.Name()] {
			continue
		}
		v = append(v, fmt.Sprintf("skills/: Location and Identity item 1: %q is not one of the two skills (roadmap-manager, knowledge-authority)", e.Name()))
	}

	for _, sc := range skillScopes {
		checkOneSkill(skillsDir, sc, cli, binaryVersion, report)
	}
	return v
}

// checkOneSkill checks one skill and reports every violation through report.
func checkOneSkill(skillsDir string, sc skillScope, cli *skillCLI, binaryVersion string,
	report func(skill, rule, format string, args ...any)) {
	dir := filepath.Join(skillsDir, sc.name)
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		report(sc.name, "Location and Identity item 1", "the skill directory is missing")
		return
	}

	checkSkillLayout(dir, sc.name, report)

	files, err := readSkillFileSet(skillsDir, sc.name)
	if err != nil {
		report(sc.name, "Location and Identity item 5", "the file set cannot be read: %v", err)
		return
	}
	if len(files) == 0 {
		report(sc.name, "Location and Identity item 5", "the file set is empty")
		return
	}

	checkSkillFrontmatter(files, sc.name, binaryVersion, report)

	scope := make(map[string]bool, len(sc.commands))
	for _, c := range sc.commands {
		scope[c] = true
	}

	named := make(map[string]bool)
	for i := range files {
		f := &files[i]
		for _, seg := range recognisedSegments(f.rel, f.content) {
			for _, inv := range findInvocations(cli, seg, f.rel) {
				if inv.canonical() {
					named[inv.cmd.Name+" "+inv.sub.Name] = true
				}
				if !inv.resolve {
					report(sc.name, "rule 3 (nothing absent is invoked)",
						"%q at %s:%d names a command or subcommand the binary does not have", inv.text(), inv.file, inv.line)
				}
				if inv.cmd != nil && inv.cmd.Name != skillSharedCommand && !scope[inv.cmd.Name] {
					report(sc.name, "rule 4 (the partition holds)",
						"%q at %s:%d invokes a command outside this skill's scope", inv.text(), inv.file, inv.line)
				}
			}
		}
	}

	var all strings.Builder
	for i := range files {
		all.WriteString(files[i].content)
		all.WriteByte('\n')
	}
	text := all.String()

	for _, name := range sc.commands {
		cmd := cli.command(name)
		if cmd == nil {
			report(sc.name, "scope", "the contract has no command %q, which § Scope Partition assigns to this skill", name)
			continue
		}
		for i := range cmd.Subcommands {
			sub := &cmd.Subcommands[i]
			if !named[cmd.Name+" "+sub.Name] {
				want := "rmp " + cmd.Name + " " + sub.Name
				if isSingleAction(cmd) {
					want = "rmp " + cmd.Name
				}
				report(sc.name, "rule 1 (every subcommand is named)", "no invocation of %s written with canonical names", want)
			}
			for j := range sub.Flags {
				long := sub.Flags[j].Long
				if !containsStandalone(text, long) {
					report(sc.name, "rule 2 (every flag is named)", "the long flag %s of %s %s appears nowhere", long, cmd.Name, sub.Name)
				}
			}
		}
	}
}

// checkSkillLayout enforces SPEC/SKILLS.md § Location and Identity item 2: a
// SKILL.md, an optional references/ directory, and — for roadmap-manager only —
// an optional PDS.md; entries whose name begins with '.' are not read.
func checkSkillLayout(dir, skill string, report func(skill, rule, format string, args ...any)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		report(skill, "Location and Identity item 2", "the skill directory cannot be read: %v", err)
		return
	}
	hasSkillMD := false
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "."):
		case name == "SKILL.md" && e.Type().IsRegular():
			hasSkillMD = true
		case name == "references" && e.IsDir():
		case name == "PDS.md" && skill == "roadmap-manager" && e.Type().IsRegular():
		default:
			report(skill, "Location and Identity item 2", "%q is not an entry a skill may contain", name)
		}
	}
	if !hasSkillMD {
		report(skill, "Location and Identity item 2", "SKILL.md is missing")
	}
}

// checkSkillFrontmatter enforces SPEC/SKILLS.md § Location and Identity item 3
// and § Rules, rule 5.
func checkSkillFrontmatter(files []skillFile, skill, binaryVersion string,
	report func(skill, rule, format string, args ...any)) {
	var skillMD *skillFile
	for i := range files {
		if files[i].rel == skill+"/SKILL.md" {
			skillMD = &files[i]
		}
	}
	if skillMD == nil {
		return // reported by checkSkillLayout
	}
	fm, ok := parseSkillFrontmatter(skillMD.content)
	if !ok {
		report(skill, "Location and Identity item 3", "SKILL.md has no frontmatter")
		return
	}
	if !fm.hasName || fm.name != skill {
		report(skill, "Location and Identity item 3", "the frontmatter name is %q, not the directory name %q", fm.name, skill)
	}
	if fm.description == "" {
		report(skill, "Location and Identity item 3", "the frontmatter description is empty")
	}
	if fm.versionErr != "" {
		report(skill, "rule 5 (the version is declared and current)", "no well-formed version declaration: %s", fm.versionErr)
		return
	}
	if binaryVersion != "" && fm.version != binaryVersion {
		report(skill, "rule 5 (the version is declared and current)",
			"the declared version %s differs from the binary version %s", fm.version, binaryVersion)
	}
}
