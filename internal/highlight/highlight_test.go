package highlight

import (
	"bytes"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// lexerName is the configured name of a lexer, or "" for none.
func lexerName(l chroma.Lexer) string {
	if l == nil {
		return ""
	}
	return l.Config().Name
}

// TestRegistryMatchesChromaRegistry is the registry half of Acceptance Criterion
// 255 of SPEC/WEB.md, rule 6: the generated registry holds the lexers chroma's
// global registry holds, in the same order, and resolves every name, every
// alias, each of them in upper case, and the extension of every file-name
// pattern to the same lexer chroma's registry resolves it to, and every MIME type
// to the same lexer chroma's registry matches it to.
func TestRegistryMatchesChromaRegistry(t *testing.T) {
	local, upstream := BuiltRegistry(), lexers.GlobalLexerRegistry

	if got, want := len(local.Lexers), len(upstream.Lexers); got != want {
		t.Fatalf("the generated registry holds %d lexers, chroma's holds %d", got, want)
	}
	for i := range upstream.Lexers {
		if got, want := lexerName(local.Lexers[i]), lexerName(upstream.Lexers[i]); got != want {
			t.Errorf("lexer %d: the generated registry holds %q, chroma's holds %q", i, got, want)
		}
	}

	queries := map[string]bool{}
	for _, l := range upstream.Lexers {
		cfg := l.Config()
		for _, name := range append([]string{cfg.Name}, cfg.Aliases...) {
			queries[name] = true
			queries[strings.ToUpper(name)] = true
		}
		for _, pattern := range append(append([]string{}, cfg.Filenames...), cfg.AliasFilenames...) {
			if ext := path.Ext(pattern); len(ext) > 1 {
				queries[ext[1:]] = true
			}
		}
		for _, mime := range cfg.MimeTypes {
			if got, want := lexerName(local.MatchMimeType(mime)), lexerName(upstream.MatchMimeType(mime)); got != want {
				t.Errorf("MIME type %q: the generated registry resolves %q, chroma's %q", mime, got, want)
			}
		}
	}
	for _, unknown := range []string{"no-such-language", "", "{.go}", "filename"} {
		queries[unknown] = true
	}
	for q := range queries {
		if got, want := lexerName(ResolveLexer(q)), lexerName(upstream.Get(q)); got != want {
			t.Errorf("%q: the generated registry resolves %q, chroma's %q", q, got, want)
		}
	}
}

// TestStyleMatchesChromaStyle: the carried style is chroma's github-dark: the
// same name, and the same CSS for every token type chroma's formatter writes.
func TestStyleMatchesChromaStyle(t *testing.T) {
	local, upstream := HighlightStyle(), styles.Get(StyleName)
	if local.Name != upstream.Name || upstream.Name != StyleName {
		t.Fatalf("the carried style is %q, chroma's %q is %q", local.Name, StyleName, upstream.Name)
	}
	var got, want bytes.Buffer
	formatter := chromahtml.New(chromahtml.WithClasses(true))
	if err := formatter.WriteCSS(&got, local); err != nil {
		t.Fatalf("writing the carried style's CSS: %v", err)
	}
	if err := formatter.WriteCSS(&want, upstream); err != nil {
		t.Fatalf("writing chroma's style's CSS: %v", err)
	}
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Error("the carried github-dark style writes different CSS from chroma's")
	}
}

// lazinessProbeEnv makes the test binary, re-executed by
// TestRegistryIsBuiltOnceOnFirstUse, run the probe in a fresh process: a process
// in which no test has touched the registry yet.
const lazinessProbeEnv = "GROADMAP_HIGHLIGHT_LAZINESS_PROBE"

// TestRegistryIsBuiltOnceOnFirstUse is the package half of Acceptance Criterion
// 256 of SPEC/WEB.md: in a fresh process the registry and the style are not built
// by initialising the package, and concurrent first uses build them exactly once.
func TestRegistryIsBuiltOnceOnFirstUse(t *testing.T) {
	if os.Getenv(lazinessProbeEnv) == "1" {
		probeLaziness(t)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^TestRegistryIsBuiltOnceOnFirstUse$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), lazinessProbeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: TestRegistryIsBuiltOnceOnFirstUse") {
		t.Fatalf("the fresh-process probe failed: %v\n%s", err, out)
	}
}

// probeLaziness runs in the fresh process.
func probeLaziness(t *testing.T) {
	if IsBuilt() || highlightBuilds.Load() != 0 {
		t.Fatal("the registry is built before any use: initialising the package built it")
	}
	if GlobalLexerRegistry != nil || Go != nil || HTML != nil || highlightStyle != nil {
		t.Fatal("a registry variable holds a value before any use: an initialiser constructed it")
	}

	const callers = 32
	var start, done sync.WaitGroup
	start.Add(1)
	resolved := make([]chroma.Lexer, callers)
	for i := range callers {
		done.Go(func() {
			start.Wait()
			if i%2 == 0 {
				resolved[i] = ResolveLexer("go")
			} else {
				_ = HighlightStyle()
				resolved[i] = ResolveLexer("golang")
			}
		})
	}
	start.Done()
	done.Wait()

	if n := highlightBuilds.Load(); n != 1 {
		t.Errorf("%d concurrent first uses built the registry %d times, want exactly 1", callers, n)
	}
	if !IsBuilt() {
		t.Error("the registry is not reported built after its first use")
	}
	for i, l := range resolved {
		if l == nil || l != resolved[0] {
			t.Errorf("caller %d resolved %v, want the one Go lexer every caller shares", i, l)
		}
	}
}
