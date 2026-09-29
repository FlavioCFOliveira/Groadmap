package highlight

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// packagePath is this package's directory relative to the module root, in slash
// form, which is how the root .gitattributes names its files.
const packagePath = "internal/highlight"

// textState is the state a .gitattributes line gives the text attribute.
type textState int

const (
	textUnspecified textState = iota
	textSet
	textUnset
	textValue
)

// String renders the state as git check-attr prints it.
func (s textState) String() string {
	switch s {
	case textSet:
		return "set"
	case textUnset:
		return "unset"
	case textValue:
		return "a value"
	}
	return "unspecified"
}

// attrRule is one .gitattributes line that assigns the text attribute.
type attrRule struct {
	line    int
	pattern string
	state   textState
}

// moduleRoot walks up from the working directory to the directory holding
// go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolving the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// parseTextRules reads the text-attribute assignments of a .gitattributes file,
// in file order. Macro definitions are skipped; the built-in binary macro
// counts as -text, which it includes.
func parseTextRules(data []byte) []attrRule {
	var rules []attrRule
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") || strings.HasPrefix(fields[0], "[attr]") {
			continue
		}
		state, found := textUnspecified, false
		for _, attr := range fields[1:] {
			switch {
			case attr == "text":
				state, found = textSet, true
			case attr == "-text", attr == "binary":
				state, found = textUnset, true
			case attr == "!text":
				state, found = textUnspecified, true
			case strings.HasPrefix(attr, "text="):
				state, found = textValue, true
			}
		}
		if found {
			rules = append(rules, attrRule{line: n, pattern: fields[0], state: state})
		}
	}
	return rules
}

// matchAttrPattern reports whether a .gitattributes pattern at the repository
// root matches the slash path name, following gitignore(5) as gitattributes(5)
// prescribes: a pattern without a slash matches the base name at any depth; any
// other pattern is anchored at the root, where "*" and "?" stop at a slash and a
// "**" segment spans zero or more directories.
func matchAttrPattern(pattern, name string) bool {
	pattern = strings.TrimPrefix(pattern, "/")
	if !strings.Contains(pattern, "/") {
		ok, err := path.Match(pattern, path.Base(name))
		return err == nil && ok
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// matchSegments matches path segments against pattern segments.
func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], name[0])
	return err == nil && ok && matchSegments(pat[1:], name[1:])
}

// effectiveText is the text state the last matching rule gives name, which is
// the one git applies.
func effectiveText(rules []attrRule, name string) (textState, int) {
	state, line := textUnspecified, 0
	for _, r := range rules {
		if matchAttrPattern(r.pattern, name) {
			state, line = r.state, r.line
		}
	}
	return state, line
}

// TestGeneratedFilesAreStoredByteExact pins the .gitattributes rules that keep
// git from converting the line endings of the generated files: every file the
// generator owns must have the text attribute unset, or a checkout stores a
// CRLF lexer definition (dax.xml, modelica.xml) with LF, or — under
// core.autocrlf=true — an LF file with CRLF, and TestGeneratedRegistryIsCurrent
// fails on that checkout. The root .gitattributes is parsed on every run, so the
// guarantee holds in an exported tree with no git; inside a git work tree the
// verdict of git check-attr is asserted as well.
func TestGeneratedFilesAreStoredByteExact(t *testing.T) {
	root := moduleRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatalf("reading the root .gitattributes: %v", err)
	}
	rules := parseTextRules(data)

	committed := generatedFiles(t, ".")
	names := make([]string, 0, len(committed))
	for rel := range committed {
		names = append(names, packagePath+"/"+rel)
	}
	slices.Sort(names)
	if !slices.Contains(names, packagePath+"/embedded/dax.xml") {
		t.Fatal("the generated file set lacks embedded/dax.xml, the CRLF definition this test exists for")
	}

	for _, name := range names {
		if state, line := effectiveText(rules, name); state != textUnset {
			where := "no rule matches it"
			if line > 0 {
				where = "the last matching rule is line " + strconv.Itoa(line)
			}
			t.Errorf(".gitattributes leaves the text attribute of %s %s, not unset (%s)", name, state, where)
		}
	}

	checkAttrWithGit(t, root, names)
}

// checkAttrWithGit asserts git's own verdict when git is installed and root is a
// git work tree; otherwise the parsed verdict above is the whole assertion.
func checkAttrWithGit(t *testing.T, root string, names []string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Logf("git is not installed; the parsed .gitattributes verdict stands alone")
		return
	}
	probe := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel")
	top, err := probe.Output()
	if err != nil {
		t.Logf("%s is not a git work tree; the parsed .gitattributes verdict stands alone", root)
		return
	}
	if !sameDir(strings.TrimSpace(string(top)), root) {
		t.Logf("the git work tree %s is not the module root %s; the parsed verdict stands alone",
			strings.TrimSpace(string(top)), root)
		return
	}

	cmd := exec.Command("git", "-C", root, "check-attr", "--stdin", "-z", "text")
	cmd.Stdin = strings.NewReader(strings.Join(names, "\x00") + "\x00")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git check-attr: %v\n%s", err, stderr.String())
	}
	// With -z the output is a sequence of NUL-terminated triples: path,
	// attribute, value.
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(fields) != 3*len(names) {
		t.Fatalf("git check-attr printed %d fields for %d paths", len(fields), len(names))
	}
	for i := 0; i < len(fields); i += 3 {
		if fields[i+2] != "unset" {
			t.Errorf("git check-attr reports text=%s for %s, want unset", fields[i+2], fields[i])
		}
	}
}

// sameDir reports whether two paths name the same directory.
func sameDir(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if err := errors.Join(errA, errB); err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// TestMatchAttrPattern pins the gitignore(5) pattern subset the parsed verdict
// relies on, so a matcher defect cannot pass a file the rules do not cover.
func TestMatchAttrPattern(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"internal/highlight/embedded/**", "internal/highlight/embedded/dax.xml", true},
		{"internal/highlight/embedded/**", "internal/highlight/embeddedx/dax.xml", false},
		{"internal/highlight/embedded/**", "other/internal/highlight/embedded/dax.xml", false},
		{"/internal/highlight/styles/**", "internal/highlight/styles/github-dark.xml", true},
		{"internal/highlight/generated_*.go", "internal/highlight/generated_go.go", true},
		{"internal/highlight/generated_*.go", "internal/highlight/sub/generated_go.go", false},
		{"internal/highlight/generated_*.go", "internal/highlight/highlight.go", false},
		{"**/*.xml", "internal/highlight/embedded/dax.xml", true},
		{"*.xml", "internal/highlight/embedded/dax.xml", true},
		{"*", "internal/highlight/COPYING.chroma", true},
		{"*.go", "internal/highlight/embedded/dax.xml", false},
	}
	for _, c := range cases {
		if got := matchAttrPattern(c.pattern, c.name); got != c.want {
			t.Errorf("matchAttrPattern(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

// TestEffectiveTextLastRuleWins pins that a later rule overrides an earlier
// one, as in git, so a trailing "* text=auto" is caught.
func TestEffectiveTextLastRuleWins(t *testing.T) {
	rules := parseTextRules([]byte("# comment\n" +
		"[attr]mine -text\n" +
		"internal/highlight/embedded/** -text\n" +
		"*.md diff=markdown\n" +
		"* text=auto eol=lf\n"))
	if len(rules) != 2 {
		t.Fatalf("parsed %d text rules, want 2: %+v", len(rules), rules)
	}
	if state, line := effectiveText(rules, "internal/highlight/embedded/dax.xml"); state != textValue || line != 5 {
		t.Errorf("effective state = %s from line %d, want a value from line 5", state, line)
	}
	rules = parseTextRules([]byte("* text\ninternal/highlight/** binary\n"))
	if state, _ := effectiveText(rules, "internal/highlight/embedded/dax.xml"); state != textUnset {
		t.Errorf("binary after text gives %s, want unset", state)
	}
}
