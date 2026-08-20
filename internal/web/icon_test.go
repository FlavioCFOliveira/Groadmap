package web

import (
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Regression guard for the class of defect that let the knowledge-graph empty
// state ship with `ti-affiliate-off`, a Tabler Icons class no release of the
// webfont has ever defined. A missing glyph class fails silently: the browser
// paints nothing and no request errors, so neither the page tests nor the
// end-to-end suite noticed. This file closes that hole by gating every icon
// class the binary ships against the icon stylesheet the binary ships.
//
// The guard is also the safety net for upgrading the vendored webfont
// (SPEC/BUILD.md § Vendored Web Assets, rule 3): if a future Tabler Icons
// release renames or drops a glyph the interface uses, this test fails at the
// upgrade instead of leaving a blank space on a page nobody happens to open.

// usedIconClassRe matches a Tabler Icons glyph class token (`ti-<name>`) as it
// appears in a shipped asset. The leading word boundary keeps it from matching
// the tail of an unrelated hyphenated word (`multi-line`, `anti-pattern`), and
// the bare `ti` base class, which carries the font family rather than a glyph,
// is deliberately not matched.
var usedIconClassRe = regexp.MustCompile(`\bti-[a-z0-9]+(?:-[a-z0-9]+)*\b`)

// iconGlyphDefRe matches one glyph definition in the vendored Tabler Icons
// stylesheet, whose minified form is `.ti-<name>:before{content:"\<codepoint>"}`.
// The distribution uses one selector per rule — it groups no selectors with a
// comma — so a single-capture pattern maps the whole set.
var iconGlyphDefRe = regexp.MustCompile(`\.(ti-[a-z0-9-]+):before\{content:"\\([0-9a-fA-F]+)"\}`)

// vendoredIconStylesheet is the embedded path of the Tabler Icons stylesheet,
// relative to staticFS. It is the only file that defines what a glyph class
// means, so it is the only admissible authority for this test.
const vendoredIconStylesheet = "static/vendor/tabler-icons/tabler-icons.min.css"

// vendorPrefix marks the third-party assets. Files under it are excluded when
// collecting *used* classes: the icon stylesheet names all 5000-odd glyphs and
// the Tabler bundle references its own, none of which the interface ships as
// markup. Including them would make the assertion self-satisfying.
const vendorPrefix = "static/vendor/"

// TestEmbeddedAssets_EveryIconClassIsDefinedInTheVendoredWebfont asserts that
// every `ti-<name>` class referenced by an embedded template or a
// project-authored static asset resolves to a glyph in the embedded Tabler
// Icons stylesheet.
//
// Both sides are read through the go:embed filesystems rather than from the
// working directory, so the test gates exactly what the binary ships.
func TestEmbeddedAssets_EveryIconClassIsDefinedInTheVendoredWebfont(t *testing.T) {
	glyphs := embeddedIconGlyphs(t)
	used := shippedIconReferences(t)

	// Falsifiability control: an extraction that silently found nothing would
	// make the assertion below pass vacuously. Both sides must be populated,
	// and a glyph known to exist must be present on each.
	if len(glyphs) < 1000 {
		t.Fatalf("parsed only %d glyph definitions from %s; the stylesheet format changed and the guard is no longer reading it", len(glyphs), vendoredIconStylesheet)
	}
	if len(used) == 0 {
		t.Fatalf("found no ti-* class in any embedded asset; the extraction is broken and this test would pass vacuously")
	}
	if _, ok := used["ti-affiliate"]; !ok {
		t.Errorf("the extraction did not find ti-affiliate, which the sidebar and several page headers render; it is not reading the templates correctly")
	}

	for _, class := range sortedKeys(used) {
		if _, ok := glyphs[class]; !ok {
			t.Errorf("icon class %q is referenced by %s but is defined by no rule in %s, so the browser paints no glyph",
				class, strings.Join(used[class], ", "), vendoredIconStylesheet)
		}
	}
}

// TestEmbeddedAssets_IconGuardRejectsAnUndefinedClass proves the guard above
// discriminates instead of accepting anything. It takes the exact class the
// defect shipped — `ti-affiliate-off`, which mirrors the correct `ti-map` /
// `ti-map-off` pairing on the roadmap index but has no upstream counterpart —
// and asserts two things: the stylesheet really does not define it, and no
// shipped asset references it any more.
//
// Together these mean the guard fails if graph.html is reverted: the class
// would reappear in the used set while remaining absent from the glyph set.
func TestEmbeddedAssets_IconGuardRejectsAnUndefinedClass(t *testing.T) {
	glyphs := embeddedIconGlyphs(t)
	used := shippedIconReferences(t)

	const undefinedClass = "ti-affiliate-off"
	if _, ok := glyphs[undefinedClass]; ok {
		t.Fatalf("%s now defines %q; the premise of this control no longer holds and the guard needs a different undefined class", vendoredIconStylesheet, undefinedClass)
	}
	// The base glyph exists — it is the "-off" variant that upstream never
	// shipped. Asserting this separates "the class is missing" from "the
	// stylesheet failed to parse".
	if _, ok := glyphs["ti-affiliate"]; !ok {
		t.Fatalf("%s does not define ti-affiliate either; the stylesheet was not parsed", vendoredIconStylesheet)
	}
	if where, ok := used[undefinedClass]; ok {
		t.Errorf("%s references the undefined icon class %q, which paints no glyph; use an -off variant the webfont actually defines", strings.Join(where, ", "), undefinedClass)
	}
}

// embeddedIconGlyphs parses the embedded Tabler Icons stylesheet and returns
// each defined glyph class mapped to its codepoint.
func embeddedIconGlyphs(t *testing.T) map[string]string {
	t.Helper()

	css, err := staticFS.ReadFile(vendoredIconStylesheet)
	if err != nil {
		t.Fatalf("reading the embedded icon stylesheet %s: %v", vendoredIconStylesheet, err)
	}

	glyphs := make(map[string]string)
	for _, m := range iconGlyphDefRe.FindAllStringSubmatch(string(css), -1) {
		glyphs[m[1]] = m[2]
	}
	return glyphs
}

// shippedIconReferences collects every ti-* class referenced by an embedded
// template or a project-authored embedded static asset, mapped to the sorted
// list of files that reference it. Third-party assets under static/vendor/ are
// excluded: they are the definition side, not the usage side.
func shippedIconReferences(t *testing.T) map[string][]string {
	t.Helper()

	seen := make(map[string]map[string]bool)
	record := func(name, content string) {
		for _, class := range usedIconClassRe.FindAllString(content, -1) {
			if seen[class] == nil {
				seen[class] = make(map[string]bool)
			}
			seen[class][name] = true
		}
	}

	templates, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		t.Fatalf("globbing the embedded templates: %v", err)
	}
	if len(templates) == 0 {
		t.Fatalf("the embedded template set is empty; the guard has nothing to check")
	}
	for _, name := range templates {
		content, err := templatesFS.ReadFile(name)
		if err != nil {
			t.Fatalf("reading the embedded template %s: %v", name, err)
		}
		record(name, string(content))
	}

	err = fs.WalkDir(staticFS, "static", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip the whole vendored tree: it defines glyph classes rather
			// than referencing them as markup.
			if strings.HasPrefix(name+"/", vendorPrefix) {
				return fs.SkipDir
			}
			return nil
		}
		switch path.Ext(name) {
		case ".css", ".js", ".html", ".svg":
		default:
			return nil
		}
		content, readErr := staticFS.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		record(name, string(content))
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded static assets: %v", err)
	}

	used := make(map[string][]string, len(seen))
	for class, files := range seen {
		used[class] = sortedKeys(files)
	}
	return used
}

// sortedKeys returns the keys of m in a deterministic order, so failure
// messages read the same on every run.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
