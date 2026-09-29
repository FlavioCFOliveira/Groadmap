package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/FlavioCFOliveira/Groadmap/internal/highlight"
)

// This file is the gate for SPEC/WEB.md § Markdown Rendering, rule 6, as
// Acceptance Criteria 255 and 256 state it: fenced code is highlighted through
// the generated lexer registry exactly as chroma's own registry highlights it,
// and that registry is built on the first fenced code block that declares a
// language, never at start-up.

// highlightCase is one fenced code block of the equivalence corpus.
type highlightCase struct {
	label    string
	language string
	code     string
}

// genericSamples are the code bodies every declared name and alias is rendered
// with: identifiers, keywords, an assignment, numbers, a call, and statement
// ends, over several lines. The richer inputs — strings, comments, operators,
// markup — come from chroma's own lexer test inputs (chromaTestdataCases), each
// run through the lexer chroma's tests run it through.
//
// The generic bodies are deliberately free of other punctuation. chroma's
// Jungle lexer does not terminate — it runs for minutes and was not seen to
// finish — on inputs as small as "a, b", "a - 1", "total / 4", "window[3] = 2",
// a quoted string, or a comment, while these bodies take it under a millisecond.
// That is a defect of the pinned chroma, outside this renderer, and it holds for
// chroma's own registry exactly as for the generated one; the corpus is chosen
// so the gate terminates, and every case also runs under a deadline that fails
// the test rather than hanging it (see withDeadline).
var genericSamples = []string{
	"residual = 42\n",
	"residual = 0.02;\nwindow = f(7);\nreturn total\n",
}

// goCommand prepares a run of the go command in dir. The package's TestMain points
// HOME at an empty temporary directory, and the go command derives its module
// and build caches from HOME, so the run gets the account's own home directory
// back: the pinned modules are then resolved from the module cache the build
// used, not downloaded again into an empty one.
func goCommand(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	account, err := user.Current()
	if err != nil || account.HomeDir == "" {
		t.Fatalf("resolving the account's home directory for the go command: %v", err)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+account.HomeDir)
	return cmd
}

// chromaModuleDir is the module directory of the chroma version go.mod pins.
func chromaModuleDir(t *testing.T) string {
	t.Helper()
	out, err := goCommand(t, ".", "list", "-m", "-json", "github.com/alecthomas/chroma/v2").Output()
	if err != nil {
		t.Fatalf("locating the pinned chroma module: %v", err)
	}
	var mod struct{ Dir string }
	if err := json.Unmarshal(out, &mod); err != nil || mod.Dir == "" {
		t.Fatalf("reading the pinned chroma module's directory: %v", err)
	}
	return mod.Dir
}

// chromaTestdataCases are chroma's own lexer test inputs, each declared with the
// language chroma's lexer tests resolve it by: the file's base name, or the name
// of the directory that holds it. They are the realistic samples chroma itself
// holds every lexer's output to.
func chromaTestdataCases(t *testing.T) []highlightCase {
	t.Helper()
	root := filepath.Join(chromaModuleDir(t), "lexers", "testdata")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading chroma's lexer test inputs: %v", err)
	}
	var cases []highlightCase
	add := func(language, file string) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		cases = append(cases, highlightCase{label: "chroma testdata " + filepath.Base(file), language: language, code: string(data)})
	}
	for _, e := range entries {
		switch {
		case e.Name() == "analysis":
			// Inputs for content analysis, which the renderer never performs.
		case e.IsDir():
			sub, err := filepath.Glob(filepath.Join(root, e.Name(), "*.actual"))
			if err != nil {
				t.Fatalf("listing %s: %v", e.Name(), err)
			}
			for _, file := range sub {
				add(e.Name(), file)
			}
		case strings.HasSuffix(e.Name(), ".actual"):
			base, _, _ := strings.Cut(strings.TrimSuffix(e.Name(), ".actual"), ".")
			add(base, filepath.Join(root, e.Name()))
		}
	}
	if len(cases) < 100 {
		t.Fatalf("chroma's lexer test inputs yield %d cases; the corpus is not being read", len(cases))
	}
	return cases
}

// fenced wraps code in a fenced code block declaring language, with a fence
// longer than any run of its character inside the code, so the code never closes
// the block early.
func fenced(language, code string) string {
	char := "`"
	if strings.Contains(language, "`") {
		char = "~"
	}
	longest := 0
	for run := 1; strings.Contains(code, strings.Repeat(char, run)); run++ {
		longest = run
	}
	fence := strings.Repeat(char, max(3, longest+1))
	if !strings.HasSuffix(code, "\n") {
		code += "\n"
	}
	return fence + language + "\n" + code + fence + "\n"
}

// escapeCode escapes a code block's text as the unhighlighted block writes it.
var escapeCode = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace

// referenceHighlight is the HTML SPEC/WEB.md § Markdown Rendering, rule 6
// requires for a fenced code block declaring language whose text is code, taken
// from chroma's own global registry and chroma's own github-dark style.
func referenceHighlight(language, code string) string {
	unhighlighted := "<pre><code>" + escapeCode(code) + "</code></pre>\n"
	lexer := lexers.Get(language)
	if lexer == nil {
		return unhighlighted
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return unhighlighted
	}
	var out bytes.Buffer
	if err := chromahtml.New(chromahtml.WithClasses(true)).Format(&out, styles.Get(highlightStyle), iterator); err != nil {
		return "reference formatting failed: " + err.Error()
	}
	return out.String()
}

// blockText is the text of the one fenced code block of source, as the parser
// hands it to the renderer: the concatenation of the block's lines. ok is false
// when source does not parse as exactly one fenced code block.
func blockText(source string) (string, bool) {
	src := []byte(source)
	doc := markdownRenderers[markdownInteractive].Parser().Parse(text.NewReader(src))
	block, ok := doc.FirstChild().(*ast.FencedCodeBlock)
	if !ok || doc.ChildCount() != 1 {
		return "", false
	}
	var b strings.Builder
	for i := range block.Lines().Len() {
		seg := block.Lines().At(i)
		b.Write(seg.Value(src))
	}
	return b.String(), true
}

// withDeadline runs fn, failing the test when it has not returned within limit,
// so a lexer that never terminates on an input fails the gate instead of hanging
// it.
func withDeadline(t *testing.T, label string, limit time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("%s: rendering did not finish within %v", label, limit)
	}
}

// TestMarkdown_HighlightMatchesChromaRegistry is Acceptance Criterion 255: a
// fenced code block declaring any name or alias of any lexer of chroma's global
// registry renders byte for byte the HTML chroma's own registry and style
// produce for it, and so does every one of chroma's own lexer test inputs, while
// a name neither registry resolves renders as the bare, class-free block.
//
// A lexer name holding a space cannot be declared — the declared language is
// the info string's first word — so such a name is covered through its aliases
// here and through the registry's lookup in package highlight.
func TestMarkdown_HighlightMatchesChromaRegistry(t *testing.T) {
	var cases []highlightCase
	for _, name := range lexers.Names(true) {
		if strings.ContainsAny(name, " \t") {
			continue
		}
		for i, code := range genericSamples {
			cases = append(cases, highlightCase{label: fmt.Sprintf("%q sample %d", name, i), language: name, code: code})
		}
	}
	for _, unknown := range []string{"ledgerscript", "not-a-language", "zz"} {
		if lexers.Get(unknown) != nil {
			t.Fatalf("%q was chosen as an unknown language, but chroma resolves it", unknown)
		}
		cases = append(cases, highlightCase{label: "unknown " + unknown, language: unknown,
			code: "if (total >= 100) { print('<balanced> & \"reconciled\"'); }\n"})
	}
	cases = append(cases, chromaTestdataCases(t)...)

	highlighted, unhighlighted := 0, 0
	for _, c := range cases {
		source := fenced(c.language, c.code)
		code, ok := blockText(source)
		if !ok {
			t.Fatalf("%s: the corpus case does not parse as one fenced code block", c.label)
		}
		var got, want string
		withDeadline(t, c.label, 2*time.Minute, func() {
			want = referenceHighlight(c.language, code)
			got = md(t, source)
		})
		if got != want {
			t.Errorf("%s: the rendered block differs from chroma's own registry's\n got: %.300q\nwant: %.300q",
				c.label, got, want)
			continue
		}
		if strings.HasPrefix(got, `<pre class="chroma">`) {
			highlighted++
		} else {
			unhighlighted++
			if strings.Contains(got, "class=") {
				t.Errorf("%s: an unhighlighted block carries a class:\n%.300s", c.label, got)
			}
		}
	}
	if highlighted < len(lexers.GlobalLexerRegistry.Lexers) || unhighlighted < 3 {
		t.Errorf("the corpus highlighted %d blocks and left %d unhighlighted; it does not exercise "+
			"every lexer and the unrecognised names", highlighted, unhighlighted)
	}
}

// registryProbeEnv makes the test binary, re-executed by
// TestMarkdown_HighlightRegistryBuiltOnFirstDeclaredLanguage, run the probe in a
// fresh process, in which nothing has rendered Markdown yet.
const registryProbeEnv = "GROADMAP_WEB_HIGHLIGHT_REGISTRY_PROBE"

// TestMarkdown_HighlightRegistryBuiltOnFirstDeclaredLanguage is the rendering
// half of Acceptance Criterion 256: after the web package is initialised the
// lexer registry and the style are not built; rendering Markdown with no fenced
// code block, or with only fenced code blocks that declare no language, leaves
// them unbuilt; the first fenced code block declaring a language builds them.
// (That concurrent first renderings build them exactly once is proved in
// package highlight, which counts the builds.)
func TestMarkdown_HighlightRegistryBuiltOnFirstDeclaredLanguage(t *testing.T) {
	if os.Getenv(registryProbeEnv) == "1" {
		if highlight.IsBuilt() {
			t.Fatal("the lexer registry is built once the web package is initialised")
		}
		for _, source := range []string{
			"# Settlement\n\nNo code here, only **prose** and a [link](https://example.com).",
			"```\nresidual = 42\n```",
			"```{.go}\nresidual = 42\n```",
			"    residual = 42\n",
			"`inline code` is not a block",
		} {
			md(t, source)
			if highlight.IsBuilt() {
				t.Fatalf("rendering %q built the lexer registry; it declares no language", source)
			}
		}
		md(t, "```go\nfunc Residual() int { return 0 }\n```")
		if !highlight.IsBuilt() {
			t.Fatal("the first fenced code block declaring a language did not build the registry")
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestMarkdown_HighlightRegistryBuiltOnFirstDeclaredLanguage$",
		"-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), registryProbeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: TestMarkdown_HighlightRegistryBuiltOnFirstDeclaredLanguage") {
		t.Fatalf("the fresh-process probe failed: %v\n%s", err, out)
	}
}

// TestRmpBinaryExcludesChromaRegistries is the build half of Acceptance
// Criterion 256 and SPEC/BUILD.md § Markdown Rendering Rules, rule 6: the
// packages the rmp binary is built from include chroma's root package and
// neither of chroma's registry packages, whose initialisers build every lexer and
// every style.
func TestRmpBinaryExcludesChromaRegistries(t *testing.T) {
	out, err := goCommand(t, filepath.Join("..", ".."), "list", "-deps", "./cmd/rmp").Output()
	if err != nil {
		t.Fatalf("go list -deps ./cmd/rmp: %v", err)
	}
	deps := strings.Fields(string(out))
	if !slices.Contains(deps, "github.com/alecthomas/chroma/v2") {
		t.Error("the rmp binary is not built from github.com/alecthomas/chroma/v2")
	}
	for _, banned := range []string{
		"github.com/alecthomas/chroma/v2/lexers",
		"github.com/alecthomas/chroma/v2/styles",
		"github.com/yuin/goldmark-highlighting/v2",
	} {
		if slices.Contains(deps, banned) {
			t.Errorf("the rmp binary is built from %s", banned)
		}
	}
}
