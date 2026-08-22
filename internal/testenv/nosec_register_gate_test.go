package testenv

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file is the gate task #141 exists for.
//
// The defect it closes: .gosec.yaml is the reviewer's register of every #nosec
// suppression the source carries, and nothing compared it with the source. It
// could not: gosec applies a configuration file only when -conf names one, the
// invocation in SPEC/BUILD.md § Security Scan passes none, and the register is
// comments only, so the scanner never reads it. A reviewer does, which is what
// makes a stale register worse than none. It drifted twice — once into task
// #141, and again within a day of the release that brought it up to date without
// adding a check, when three suppressions landed unlisted.
//
// So the gate reads BOTH sides and compares them, and neither side repeats the
// other's answer: the register is parsed out of .gosec.yaml, and the
// suppressions are swept out of the tree with go/ast. A test that restated the
// register in Go would pin a copy of it rather than the correspondence between
// the register and the code, and would stay green through exactly the drift it
// exists to catch.
//
// It fails in both directions, which is the property the task requires: a
// suppression the register does not account for, and a register line claiming a
// count the file no longer carries.
//
// Why the sweep mirrors gosec rather than grepping. `grep -c '#nosec'` over this
// tree answers 57 where the scanner sees 56: one hit is the string inside a
// sentence in cmd/rmp/workflow_gates_test.go, and gosec honours the directive
// only at the start of a comment group or at the start of a line within one
// (gosec v2.28.0 analyzer.go, findNoSecTag). The register used to carry a
// footnote apologising for that difference. Reading comment groups the way the
// scanner reads them settles it instead, and it also sees the second suppression
// syntax, //gosec:disable, which a #nosec grep is blind to and which would
// otherwise be a way past this gate.
//
// Why the gate is a Go test rather than a Makefile step. The security gate must
// behave identically in the three places that enforce the validation gates
// (SPEC/BUILD.md § Where the gate runs). Neither .github/workflows/ci.yml nor
// .github/workflows/release.yml invokes make: both run `go test ./...` and the
// gosec command directly. A Makefile-only check would therefore be a local
// courtesy, absent from both workflows — and it would have to reimplement the
// comment-group rule above in shell, which is the reimplementation that produced
// the footnote in the first place.
//
// Why the file lives in internal/testenv. The sweep spans every package in the
// module plus a file at its root, so no audited package is its home, and this
// package already holds the module-wide AST gates whose repository walk and skip
// list it reuses. internal/testenv also has no first-party dependencies, so this
// audit still compiles and runs against a tree in which the audited packages do
// not — which is the tree a bad edit produces, and the moment the answer matters
// most.
//
// Known limits, stated rather than papered over:
//
//   - The sweep reads every .go file in the module, whatever its build
//     constraints, so its total is the module's and does not move with GOOS. The
//     scanner's own figure does: a platform-gated file such as pty_linux.go is
//     not scanned off Linux. The register states the module-wide count and the
//     rule that derives the scanner's from it, rather than pinning a figure that
//     changes with the host.
//   - Accounting is per file, not per line. A suppression that moves within a
//     file passes, which is deliberate: line numbers churn on edits that have
//     nothing to do with security, and a register that had to be touched by
//     every such edit would be maintained mechanically — the transcription habit
//     this gate exists to prevent. Every ADDED, REMOVED or RELOCATED-BETWEEN-
//     FILES suppression fails.

// gosecRegisterRelPath is the register, relative to the module root.
const gosecRegisterRelPath = ".gosec.yaml"

// The two suppression syntaxes gosec honours (gosec v2.28.0 analyzer.go).
const (
	noSecTag           = "#nosec"
	gosecDisablePrefix = "//gosec:disable"
)

// suppression is one directive found in the source.
type suppression struct {
	file  string // slash-separated path relative to the module root
	line  int
	rules []string // rule ids named by the directive, deduplicated, in order
	// justified reports whether the directive carried a `-- reason` with text
	// after the delimiter.
	justified bool
}

// ruleFile keys the comparison: one rule suppressed in one file.
type ruleFile struct {
	rule string
	file string
}

// registerEntry is what .gosec.yaml declares for one rule.
type registerEntry struct {
	declaredCount int            // the [N] on the rule's heading line
	locations     map[string]int // file -> count
	reason        string
	headingLine   int
}

// gosecRegister is the parsed register.
type gosecRegister struct {
	rules          map[string]*registerEntry
	totalAll       int
	totalNonTest   int
	totalsDeclared bool
}

// Register line shapes. A line that is inside a rule block and matches none of
// them fails the parse: a register the gate cannot read in full is a register it
// cannot vouch for.
var (
	registerTotalsRe   = regexp.MustCompile(`^ totals: (\d+) suppressions, (\d+) outside _test\.go$`)
	registerHeadingRe  = regexp.MustCompile(`^ (G\d{3}) .*\[(\d+)\]$`)
	registerLocationRe = regexp.MustCompile(`^   (\S+\.go) +(\d+)$`)
	registerReasonRe   = regexp.MustCompile(`^   Reason: (\S.*)$`)
	registerContinueRe = regexp.MustCompile(`^     (\S.*)$`)
)

// TestNosecRegisterAccountsForEverySuppressionInTheSource is the gate. It fails
// when the register and the tree disagree in either direction.
func TestNosecRegisterAccountsForEverySuppressionInTheSource(t *testing.T) {
	root := repoRoot(t)

	found := sweepSuppressions(t, root)
	register := parseGosecRegister(t, filepath.Join(root, gosecRegisterRelPath))

	// The source side, folded to (rule, file) counts.
	inSource := make(map[ruleFile]int)
	evidence := make(map[ruleFile][]string)
	totalAll, totalNonTest := 0, 0
	for _, s := range found {
		for _, rule := range s.rules {
			key := ruleFile{rule: rule, file: s.file}
			inSource[key]++
			evidence[key] = append(evidence[key], fmt.Sprintf("%s:%d", s.file, s.line))
			totalAll++
			if !strings.HasSuffix(s.file, "_test.go") {
				totalNonTest++
			}
		}
	}

	// The register side, folded the same way.
	inRegister := make(map[ruleFile]int)
	for rule, entry := range register.rules {
		for file, count := range entry.locations {
			inRegister[ruleFile{rule: rule, file: file}] = count
		}
	}

	var unlisted, stale, miscounted []string
	for key, count := range inSource {
		declared, listed := inRegister[key]
		switch {
		case !listed:
			unlisted = append(unlisted, fmt.Sprintf(
				"%s in %s (%d: %s) is suppressed in the source and absent from the register",
				key.rule, key.file, count, strings.Join(evidence[key], ", ")))
		case declared != count:
			miscounted = append(miscounted, fmt.Sprintf(
				"%s in %s: the register declares %d, the source carries %d (%s)",
				key.rule, key.file, declared, count, strings.Join(evidence[key], ", ")))
		}
	}
	for key, declared := range inRegister {
		if _, present := inSource[key]; !present {
			stale = append(stale, fmt.Sprintf(
				"%s in %s: the register declares %d, the source carries none",
				key.rule, key.file, declared))
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	sort.Strings(miscounted)

	if len(unlisted) > 0 {
		t.Errorf("suppressions the register does not account for.\n"+
			"A suppression that cannot be justified on review is a defect to fix, not a line to add.\n"+
			"Justify it and record it in %s, or remove it:\n%s",
			gosecRegisterRelPath, indentEvidence(unlisted))
	}
	if len(stale) > 0 {
		t.Errorf("register entries the source no longer carries.\n"+
			"Remove them from %s, so the register keeps describing the code:\n%s",
			gosecRegisterRelPath, indentEvidence(stale))
	}
	if len(miscounted) > 0 {
		t.Errorf("register counts that disagree with the source.\n"+
			"Bring %s to the counts below, justifying anything newly suppressed:\n%s",
			gosecRegisterRelPath, indentEvidence(miscounted))
	}

	// Internal consistency of the register, and its two totals.
	for _, rule := range sortedRegisterRules(register) {
		entry := register.rules[rule]
		sum := 0
		for _, count := range entry.locations {
			sum += count
		}
		if sum != entry.declaredCount {
			t.Errorf("%s line %d: %s declares [%d] but its locations add up to %d",
				gosecRegisterRelPath, entry.headingLine, rule, entry.declaredCount, sum)
		}
		if entry.reason == "" {
			t.Errorf("%s line %d: %s carries no Reason. Every accepted rule states what makes it safe",
				gosecRegisterRelPath, entry.headingLine, rule)
		}
	}
	if !register.totalsDeclared {
		t.Fatalf("%s declares no totals line. Expected: `# totals: <n> suppressions, <n> outside _test.go`",
			gosecRegisterRelPath)
	}
	if register.totalAll != totalAll {
		t.Errorf("%s declares %d suppressions in the module; the sweep finds %d",
			gosecRegisterRelPath, register.totalAll, totalAll)
	}
	if register.totalNonTest != totalNonTest {
		t.Errorf("%s declares %d suppressions outside _test.go; the sweep finds %d.\n"+
			"That is the figure gosec's summary prints as `Nosec:` and the release notes quote, "+
			"because gosec does not scan test files without -tests",
			gosecRegisterRelPath, register.totalNonTest, totalNonTest)
	}
}

// TestEveryNosecDirectiveNamesARuleAndCarriesAJustification refuses the two
// directive shapes the register cannot vouch for.
//
// A naked #nosec — one that names no rule — suppresses EVERY rule on its node,
// not the one the author had in mind, and would keep suppressing a finding of a
// class nobody reviewed. A directive with no `-- reason` leaves a reviewer with
// nothing to check. gosec can enforce both itself, through the
// -nosec-require-rules and -nosec-require-justification flags of v2.28.0, but
// the project's invocation passes neither; enforcing them here keeps the rule in
// force under the invocation SPEC/BUILD.md actually defines.
func TestEveryNosecDirectiveNamesARuleAndCarriesAJustification(t *testing.T) {
	root := repoRoot(t)

	var naked, unjustified []string
	for _, s := range sweepSuppressions(t, root) {
		if len(s.rules) == 0 {
			naked = append(naked, fmt.Sprintf("%s:%d", s.file, s.line))
			continue
		}
		if !s.justified {
			unjustified = append(unjustified, fmt.Sprintf("%s:%d (%s)", s.file, s.line, strings.Join(s.rules, " ")))
		}
	}
	sort.Strings(naked)
	sort.Strings(unjustified)

	if len(naked) > 0 {
		t.Errorf("suppressions that name no rule, and therefore suppress every rule on their node.\n"+
			"Name the rule the site accepts, e.g. `#nosec G304 -- reason`:\n%s", indentEvidence(naked))
	}
	if len(unjustified) > 0 {
		t.Errorf("suppressions with no justification at the site.\n"+
			"Add `-- <reason>` after the rule, so the reason travels with the code:\n%s",
			indentEvidence(unjustified))
	}
}

// sweepSuppressions returns every gosec suppression directive in the module, in
// file then line order.
func sweepSuppressions(t *testing.T, root string) []suppression {
	t.Helper()

	fset := token.NewFileSet()
	var found []suppression

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && skipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", path, parseErr)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		for _, group := range file.Comments {
			directive, ok := findSuppressionDirective(group)
			if !ok {
				continue
			}
			rules, justified := parseDirective(directive)
			found = append(found, suppression{
				file:      filepath.ToSlash(rel),
				line:      directiveLine(fset, group),
				rules:     rules,
				justified: justified,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sweeping the repository for suppressions: %v", err)
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].file != found[j].file {
			return found[i].file < found[j].file
		}
		return found[i].line < found[j].line
	})
	return found
}

// findSuppressionDirective reports whether a comment group carries a suppression
// directive and returns everything after the tag.
//
// It reproduces gosec v2.28.0's findNoSecDirective/findNoSecTag: the tag counts
// at the start of the group's text or at the start of a line within it, so the
// same string in the middle of a sentence is prose, exactly as the scanner reads
// it. //gosec:disable is then looked for on each comment of the group, which is
// where the scanner looks for it.
func findSuppressionDirective(group *ast.CommentGroup) (string, bool) {
	text := strings.TrimSpace(group.Text())
	if text != "" {
		if strings.HasPrefix(text, noSecTag) {
			return text[len(noSecTag):], true
		}
		if idx := strings.Index(text, noSecTag); idx > 0 {
			for i := idx - 1; i >= 0; i-- {
				if text[i] == '\n' {
					return text[idx+len(noSecTag):], true
				}
				if text[i] != ' ' && text[i] != '\t' {
					break
				}
			}
		}
	}
	for _, comment := range group.List {
		if after, ok := strings.CutPrefix(comment.Text, gosecDisablePrefix); ok {
			if len(after) == 0 || after[0] == ' ' {
				return strings.TrimSpace(after), true
			}
		}
	}
	return "", false
}

// parseDirective splits a directive into the rule ids it names and whether it
// carries a justification, by gosec's own rules: the justification is whatever
// follows the first "--", and a rule id is a 'G' followed by exactly three
// digits in the part before it.
func parseDirective(args string) (rules []string, justified bool) {
	if idx := strings.Index(args, "--"); idx > -1 {
		justified = strings.TrimSpace(strings.TrimLeft(args[idx+2:], "-")) != ""
		args = args[:idx]
	}

	directive := strings.TrimSpace(args)
	if directive == "" || directive == "block" {
		return nil, justified
	}

	seen := make(map[string]bool)
	for i := 0; i < len(directive); {
		if directive[i] == 'G' && i+4 <= len(directive) {
			id := directive[i : i+4]
			valid := true
			for j := 1; j < 4; j++ {
				if directive[i+j] < '0' || directive[i+j] > '9' {
					valid = false
					break
				}
			}
			if valid {
				if !seen[id] {
					seen[id] = true
					rules = append(rules, id)
				}
				i += 4
				continue
			}
		}
		i++
	}
	return rules, justified
}

// directiveLine is the line the directive itself sits on, which is not the start
// of the group when the group opens with prose.
func directiveLine(fset *token.FileSet, group *ast.CommentGroup) int {
	for _, comment := range group.List {
		if strings.Contains(comment.Text, noSecTag) || strings.HasPrefix(comment.Text, gosecDisablePrefix) {
			return fset.Position(comment.Pos()).Line
		}
	}
	return fset.Position(group.Pos()).Line
}

// parseGosecRegister reads .gosec.yaml. It is strict on purpose: a line inside a
// rule block that it cannot read fails the test rather than being skipped, since
// a silently ignored line is how a register stops describing the code.
func parseGosecRegister(t *testing.T, path string) *gosecRegister {
	t.Helper()

	// No #nosec here: the project scans without -tests, so gosec never reads this
	// file, and a suppression that suppresses nothing is one more line the register
	// would have to carry for no gain.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", gosecRegisterRelPath, err)
	}

	register := &gosecRegister{rules: make(map[string]*registerEntry)}
	var current *registerEntry
	var currentRule string
	inReason := false

	for i, line := range strings.Split(string(raw), "\n") {
		number := i + 1
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			t.Fatalf("%s line %d is not a comment: %q.\n"+
				"The register is comments only, which is what keeps it inert as scanner input "+
				"(SPEC/BUILD.md § Security Scan, Accepted findings)", gosecRegisterRelPath, number, line)
		}
		content := strings.TrimRight(line[1:], " \t")

		if content == "" { // a blank comment line closes the block it follows
			current, currentRule, inReason = nil, "", false
			continue
		}

		if m := registerTotalsRe.FindStringSubmatch(content); m != nil {
			if register.totalsDeclared {
				t.Fatalf("%s line %d declares a second totals line", gosecRegisterRelPath, number)
			}
			register.totalAll = mustAtoi(t, m[1], number)
			register.totalNonTest = mustAtoi(t, m[2], number)
			register.totalsDeclared = true
			continue
		}

		if m := registerHeadingRe.FindStringSubmatch(content); m != nil {
			rule := m[1]
			if _, duplicate := register.rules[rule]; duplicate {
				t.Fatalf("%s line %d: %s is declared twice", gosecRegisterRelPath, number, rule)
			}
			current = &registerEntry{
				declaredCount: mustAtoi(t, m[2], number),
				locations:     make(map[string]int),
				headingLine:   number,
			}
			currentRule = rule
			register.rules[rule] = current
			inReason = false
			continue
		}

		if current == nil { // free prose outside any rule block
			continue
		}

		if m := registerReasonRe.FindStringSubmatch(content); m != nil {
			current.reason = m[1]
			inReason = true
			continue
		}
		if inReason {
			if m := registerContinueRe.FindStringSubmatch(content); m != nil {
				current.reason += " " + m[1]
				continue
			}
			t.Fatalf("%s line %d, inside the reason for %s, is neither a continuation "+
				"(five spaces then text) nor the end of the block (a blank comment line): %q",
				gosecRegisterRelPath, number, currentRule, content)
		}
		if m := registerLocationRe.FindStringSubmatch(content); m != nil {
			file := filepath.ToSlash(m[1])
			if _, duplicate := current.locations[file]; duplicate {
				t.Fatalf("%s line %d: %s lists %s twice", gosecRegisterRelPath, number, currentRule, file)
			}
			current.locations[file] = mustAtoi(t, m[2], number)
			continue
		}

		t.Fatalf("%s line %d, inside the %s block, is neither a location "+
			"(three spaces, path, count) nor a Reason: %q",
			gosecRegisterRelPath, number, currentRule, content)
	}

	if len(register.rules) == 0 {
		t.Fatalf("%s declares no rules at all, so it vouches for nothing", gosecRegisterRelPath)
	}
	return register
}

func mustAtoi(t *testing.T, s string, line int) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("%s line %d: %q is not a number: %v", gosecRegisterRelPath, line, s, err)
	}
	return n
}

func sortedRegisterRules(register *gosecRegister) []string {
	rules := make([]string, 0, len(register.rules))
	for rule := range register.rules {
		rules = append(rules, rule)
	}
	sort.Strings(rules)
	return rules
}
