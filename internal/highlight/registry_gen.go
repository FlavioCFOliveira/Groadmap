//go:build ignore

// Command registry_gen writes the generated part of package highlight: a copy of
// every lexer the pinned chroma module ships and of chroma's github-dark style,
// rewired so that nothing is constructed while the program initialises
// (SPEC/WEB.md § Markdown Rendering, rule 6; SPEC/BUILD.md § Markdown Rendering
// Rules, rules 5 and 6).
//
// The copy is mechanical. chroma's lexers package builds its registry in
// package-level variable initialisers and init functions. The generator
// type-checks that package from the module source of the chroma version go.mod
// pins, takes the initialisation order the Go specification gives those
// initialisers (go/types computes it exactly as the compiler does), and rewrites
// every initialised package-level variable into a declaration without a value
// plus a function that assigns the value. The init functions are renamed. A
// generated function then runs the assignments and the renamed init functions in
// chroma's own order, so the local registry receives every lexer in the order
// chroma's global registry does, and every lexer that looks another up while
// tokenising finds it in the local registry, because the copied code calls the
// copied Register and Get.
//
// Two further rewrites keep the copy inside the project's validation gates and
// change no behaviour: the package clause is renamed, and a composite literal of
// a struct type declared in another package that lists its values without field
// names gains the field names, which go vet requires.
//
// Run it with `go generate ./internal/highlight/` and commit the result: like
// the syntax-highlighting stylesheet, the registry is a generated but COMMITTED
// artefact, so `go build` runs no generation step. What keeps it equal to the
// pinned chroma is TestGeneratedRegistryIsCurrent (registry_stale_test.go),
// which runs this command into a temporary directory and fails when the
// committed files differ (SPEC/WEB.md Acceptance Criterion 257).
//
// Usage: go run registry_gen.go [-out DIR]
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
)

func main() {
	out := flag.String("out", ".", "the package directory to write into")
	flag.Parse()
	if err := Write(*out); err != nil {
		fmt.Fprintln(os.Stderr, "registry_gen:", err)
		os.Exit(1)
	}
}

// Module paths and names the generation reads.
const (
	chromaModule   = "github.com/alecthomas/chroma/v2"
	lexersPackage  = chromaModule + "/lexers"
	packageName    = "highlight"
	upstreamPkgDir = "lexers"
	// StyleName is the chroma style the generated data carries: the style the
	// syntax-highlighting stylesheet is generated from (SPEC/WEB.md § Markdown
	// Rendering, rule 7).
	StyleName = "github-dark"
	// StyleFile is the path, relative to the package directory, of the copied
	// style definition.
	StyleFile = "styles/" + StyleName + ".xml"
	// LicenseFile is the path, relative to the package directory, of the copy of
	// chroma's COPYING file.
	LicenseFile = "COPYING.chroma"
	// GoFilePrefix begins the name of every generated Go file.
	GoFilePrefix = "generated_"
	// LexerDefinitionDir is the directory, relative to the package directory, of
	// the copied XML lexer definitions. It keeps chroma's name, because the copied
	// Go source embeds and opens it by that name.
	LexerDefinitionDir = "embedded"
)

// gosecAccepted lists each gosec finding on the copied chroma source that is
// accepted, as the scoped directive appended to the line that ends the named
// package-level variable's declaration. Each is justified in .gosec.yaml, whose
// gate counts it. Generation fails when a named declaration no longer exists, so
// a chroma upgrade cannot silently drop or misplace one.
var gosecAccepted = []struct{ file, variable, directive string }{
	{"caddyfile.go", "caddyfileMatcherTokenRegexp",
		"#nosec G101 -- a regular expression matching Caddyfile matcher tokens, not a credential"},
}

// Result is the complete generated output: every file, keyed by its slash path
// relative to the package directory, and the chroma version it was made from.
type Result struct {
	Files         map[string][]byte
	ChromaVersion string
}

// IsGenerated reports whether a slash path relative to the package directory is
// one the generator owns: a stale file at such a path is removed by Write, and
// the staleness test compares every one of them.
func IsGenerated(rel string) bool {
	switch {
	case rel == LicenseFile:
		return true
	case strings.HasPrefix(rel, LexerDefinitionDir+"/"), strings.HasPrefix(rel, "styles/"):
		return true
	case !strings.Contains(rel, "/") && strings.HasPrefix(rel, GoFilePrefix) && strings.HasSuffix(rel, ".go"):
		return true
	}
	return false
}

// Generate produces the generated files from the chroma module go.mod pins. dir
// is any directory inside the Groadmap module; the go command run there resolves
// the pinned module.
func Generate(dir string) (*Result, error) {
	mod, err := goListModule(dir, chromaModule)
	if err != nil {
		return nil, err
	}
	pkg, err := goListPackage(dir, lexersPackage)
	if err != nil {
		return nil, err
	}
	exports, err := goListExports(dir, lexersPackage)
	if err != nil {
		return nil, err
	}

	notice, license, err := chromaNotice(mod.Dir)
	if err != nil {
		return nil, err
	}
	header := func(source string) string {
		return fmt.Sprintf("// Code generated by registry_gen.go from %s %s %s. DO NOT EDIT.\n\n%s\n",
			chromaModule, mod.Version, source, notice)
	}

	files := map[string][]byte{LicenseFile: license}

	lexerFiles, err := rewriteLexers(pkg, exports, header)
	if err != nil {
		return nil, err
	}
	for name, data := range lexerFiles {
		files[name] = data
	}

	for _, rel := range pkg.EmbedFiles {
		if !strings.HasPrefix(rel, LexerDefinitionDir+"/") {
			return nil, fmt.Errorf("chroma's lexers package embeds %s, outside %s/", rel, LexerDefinitionDir)
		}
		data, err := os.ReadFile(filepath.Join(pkg.Dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		files[rel] = data
	}

	styleSource, styleData, err := findStyle(filepath.Join(mod.Dir, "styles"))
	if err != nil {
		return nil, err
	}
	files[StyleFile] = styleData
	styleGo, err := format.Source([]byte(header("styles/"+styleSource) + styleGoSource))
	if err != nil {
		return nil, fmt.Errorf("formatting the style source: %w", err)
	}
	files[GoFilePrefix+"style.go"] = styleGo

	return &Result{Files: files, ChromaVersion: mod.Version}, nil
}

// Write generates the files, resolving the pinned chroma from the working
// directory, and writes them into dir, removing every generated file of an
// earlier run that the new output does not contain.
func Write(dir string) error {
	res, err := Generate(".")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { // committed source tree
		return err
	}
	existing, err := Existing(dir)
	if err != nil {
		return err
	}
	for rel := range existing {
		if _, keep := res.Files[rel]; !keep {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
				return err
			}
		}
	}
	for rel, data := range res.Files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil { // committed source tree
			return err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil { // committed, world-readable source file
			return err
		}
	}
	return nil
}

// Existing reads every generated file currently present in dir, the package
// directory, keyed by its slash path relative to dir.
func Existing(dir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !IsGenerated(rel) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = data
		return nil
	})
	return out, err
}

// styleGoSource is the Go source of the generated style accessor.
const styleGoSource = `package ` + packageName + `

import (
	"bytes"
	_ "embed" // the style definition is embedded

	"github.com/alecthomas/chroma/v2"
)

// generatedStyleXML is chroma's ` + StyleName + ` style definition.
//
//go:embed ` + StyleFile + `
var generatedStyleXML []byte

// generatedStyleName is the name the style is registered under in chroma.
const generatedStyleName = "` + StyleName + `"

// generatedNewStyle parses the style definition exactly as chroma's styles
// package does.
func generatedNewStyle() *chroma.Style {
	return chroma.MustNewXMLStyle(bytes.NewReader(generatedStyleXML))
}
`

// findStyle returns the name and content of the style file that chroma's styles
// registry resolves StyleName to. The registry is keyed by the lower-cased name
// each definition declares, filled in directory order, so the last file that
// declares the name wins.
func findStyle(stylesDir string) (string, []byte, error) {
	entries, err := os.ReadDir(stylesDir)
	if err != nil {
		return "", nil, err
	}
	var name string
	var data []byte
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".xml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(stylesDir, e.Name()))
		if err != nil {
			return "", nil, err
		}
		style, err := chroma.NewXMLStyle(bytes.NewReader(b))
		if err != nil {
			return "", nil, fmt.Errorf("parsing chroma style %s: %w", e.Name(), err)
		}
		if strings.ToLower(style.Name) == StyleName {
			name, data = e.Name(), b
		}
	}
	if data == nil {
		return "", nil, fmt.Errorf("chroma %s ships no %q style", stylesDir, StyleName)
	}
	return name, data, nil
}

// chromaNotice returns chroma's copyright and permission notice as a Go comment,
// and the COPYING file itself.
func chromaNotice(modDir string) (string, []byte, error) {
	license, err := os.ReadFile(filepath.Join(modDir, "COPYING"))
	if err != nil {
		return "", nil, err
	}
	mit, _, _ := strings.Cut(string(license), "\n\n\n")
	if !strings.HasPrefix(mit, "Copyright") || !strings.Contains(mit, "Permission is hereby granted") {
		return "", nil, errors.New("chroma's COPYING does not open with its copyright and permission notice")
	}
	var b strings.Builder
	b.WriteString("// This file is a copy, rewritten to build lazily, of chroma source. chroma's\n")
	b.WriteString("// copyright and permission notice follows; COPYING.chroma is its full COPYING.\n//\n")
	for line := range strings.SplitSeq(strings.TrimRight(mit, "\n"), "\n") {
		if line == "" {
			b.WriteString("//\n")
			continue
		}
		b.WriteString("// " + line + "\n")
	}
	return b.String(), license, nil
}

// ---------------------------------------------------------------------------
// The go command.

type listedModule struct {
	Path    string
	Version string
	Dir     string
}

type listedPackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	EmbedFiles []string
	Export     string
}

func goCommand(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return out, nil
}

func goListModule(dir, module string) (*listedModule, error) {
	out, err := goCommand(dir, "list", "-m", "-json", module)
	if err != nil {
		return nil, err
	}
	var m listedModule
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, err
	}
	if m.Dir == "" || m.Version == "" {
		return nil, fmt.Errorf("go list -m %s: no module directory or version", module)
	}
	return &m, nil
}

func goListPackage(dir, importPath string) (*listedPackage, error) {
	out, err := goCommand(dir, "list", "-json=ImportPath,Dir,GoFiles,EmbedFiles", importPath)
	if err != nil {
		return nil, err
	}
	var p listedPackage
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// goListExports maps every dependency of importPath to its compiled export data.
func goListExports(dir, importPath string) (map[string]string, error) {
	out, err := goCommand(dir, "list", "-export", "-deps", "-json=ImportPath,Export", importPath)
	if err != nil {
		return nil, err
	}
	exports := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		exports[p.ImportPath] = p.Export
	}
	return exports, nil
}

// ---------------------------------------------------------------------------
// The rewrite of chroma's lexers package.

// sourceFile is one parsed file of chroma's lexers package.
type sourceFile struct {
	name string
	src  []byte
	base int // token.File base, to turn a token.Pos into an offset
	ast  *ast.File
	// inserts are text insertions at an offset of src; removals replace a span
	// of src with text. Removals never overlap each other.
	inserts  []edit
	removals []edit
	// funcs are the functions appended to the file.
	funcs []string
}

type edit struct {
	start, end int
	text       string
}

func (f *sourceFile) off(p token.Pos) int { return int(p) - f.base }

// span returns src[start:end] with every insertion inside the span applied.
func (f *sourceFile) span(start, end int) string {
	var b strings.Builder
	at := start
	for _, ins := range f.inserts {
		if ins.start < start || ins.start >= end {
			continue
		}
		b.Write(f.src[at:ins.start])
		b.WriteString(ins.text)
		at = ins.start
	}
	b.Write(f.src[at:end])
	return b.String()
}

// render returns the rewritten file.
func (f *sourceFile) render() string {
	var b strings.Builder
	at := 0
	for _, r := range f.removals {
		b.WriteString(f.span(at, r.start))
		b.WriteString(r.text)
		at = r.end
	}
	b.WriteString(f.span(at, len(f.src)))
	for _, fn := range f.funcs {
		b.WriteString("\n" + fn)
	}
	return b.String()
}

func rewriteLexers(pkg *listedPackage, exports map[string]string, header func(string) string) (map[string][]byte, error) {
	fset := token.NewFileSet()
	var files []*sourceFile
	var astFiles []*ast.File
	for _, name := range pkg.GoFiles { // go list sorts them, the order the compiler receives
		src, err := os.ReadFile(filepath.Join(pkg.Dir, name))
		if err != nil {
			return nil, err
		}
		af, err := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, &sourceFile{name: name, src: src, base: fset.File(af.Pos()).Base(), ast: af})
		astFiles = append(astFiles, af)
	}

	lookup := func(importPath string) (io.ReadCloser, error) {
		p, ok := exports[importPath]
		if !ok || p == "" {
			return nil, fmt.Errorf("no export data for %s", importPath)
		}
		return os.Open(p)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}
	tpkg, err := conf.Check(pkg.ImportPath, fset, astFiles, info)
	if err != nil {
		return nil, fmt.Errorf("type-checking %s: %w", pkg.ImportPath, err)
	}

	fileOf := func(p token.Pos) *sourceFile {
		for _, f := range files {
			if f.ast.FileStart <= p && p <= f.ast.FileEnd {
				return f
			}
		}
		return nil
	}

	kept := map[ast.Expr]bool{} // the values of variables kept as chroma wrote them
	for _, f := range files {
		// The package clause.
		f.removals = append(f.removals, edit{f.off(f.ast.Name.Pos()), f.off(f.ast.Name.End()), packageName})
		// Field names for unkeyed composite literals of another package's struct.
		if err := keyCompositeLiterals(f, info, tpkg); err != nil {
			return nil, err
		}
		// Initialised package-level variables lose their values.
		if err := stripInitialisers(f, info, tpkg, kept); err != nil {
			return nil, err
		}
		// Accepted gosec findings carry their directive.
		if err := annotateAccepted(f); err != nil {
			return nil, err
		}
	}

	// The assignments, in the specification's initialisation order.
	var calls []string
	for _, init := range info.InitOrder {
		if kept[init.Rhs] {
			continue // kept in place as static data; see allConstant
		}
		f := fileOf(init.Rhs.Pos())
		if f == nil {
			return nil, fmt.Errorf("initialiser %v has no file", init)
		}
		var lhs []string
		for _, v := range init.Lhs {
			lhs = append(lhs, v.Name())
		}
		name := fmt.Sprintf("generatedInitVar%03d", len(calls))
		f.funcs = append(f.funcs, fmt.Sprintf("func %s() {\n\t%s = %s\n}\n",
			name, strings.Join(lhs, ", "), f.span(f.off(init.Rhs.Pos()), f.off(init.Rhs.End()))))
		calls = append(calls, name)
	}
	// Then the init functions, in file order and, within a file, source order.
	n := 0
	for _, f := range files {
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Name.Name != "init" {
				continue
			}
			name := fmt.Sprintf("generatedInitFunc%03d", n)
			n++
			f.removals = append(f.removals, edit{f.off(fd.Name.Pos()), f.off(fd.Name.End()), name})
			calls = append(calls, name)
		}
	}

	out := map[string][]byte{}
	for _, f := range files {
		sort.Slice(f.inserts, func(i, j int) bool { return f.inserts[i].start < f.inserts[j].start })
		sort.Slice(f.removals, func(i, j int) bool { return f.removals[i].start < f.removals[j].start })
		for i := 1; i < len(f.removals); i++ {
			if f.removals[i].start < f.removals[i-1].end {
				return nil, fmt.Errorf("%s: overlapping rewrites", f.name)
			}
		}
		text := header(path.Join(upstreamPkgDir, f.name)) + f.render()
		formatted, err := format.Source([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("formatting the copy of %s: %w", f.name, err)
		}
		out[GoFilePrefix+f.name] = formatted
	}

	var b strings.Builder
	b.WriteString(header("lexers (initialisation order)"))
	b.WriteString("package " + packageName + "\n\n")
	b.WriteString("// generatedBuildLexers runs, in the order chroma's lexers package initialises\n")
	b.WriteString("// them, the initialisers of that package's variables and then its init\n")
	b.WriteString("// functions, filling GlobalLexerRegistry. It MUST run exactly once.\n")
	b.WriteString("func generatedBuildLexers() {\n")
	for _, c := range calls {
		b.WriteString("\t" + c + "()\n")
	}
	b.WriteString("}\n")
	build, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, err
	}
	out[GoFilePrefix+"build.go"] = build
	return out, nil
}

// keyCompositeLiterals records, for every composite literal of a struct type
// declared in another package whose elements carry no field names, the insertion
// of each element's field name.
func keyCompositeLiterals(f *sourceFile, info *types.Info, self *types.Package) error {
	var err error
	ast.Inspect(f.ast, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || len(lit.Elts) == 0 {
			return true
		}
		if _, keyed := lit.Elts[0].(*ast.KeyValueExpr); keyed {
			return true
		}
		tv, ok := info.Types[lit]
		if !ok {
			return true
		}
		named, ok := types.Unalias(tv.Type).(*types.Named)
		if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg() == self {
			return true
		}
		st, ok := named.Underlying().(*types.Struct)
		if !ok {
			return true
		}
		if len(lit.Elts) != st.NumFields() {
			err = fmt.Errorf("%s: unkeyed %s literal with %d of %d fields", f.name, named, len(lit.Elts), st.NumFields())
			return false
		}
		for i, e := range lit.Elts {
			f.inserts = append(f.inserts, edit{start: f.off(e.Pos()), text: st.Field(i).Name() + ": "})
		}
		return true
	})
	return err
}

// stripInitialisers removes the value of every initialised package-level
// variable, declaring it with its type instead, except where every value of the
// declaration is a constant; those values are recorded in kept.
func stripInitialisers(f *sourceFile, info *types.Info, self *types.Package, kept map[ast.Expr]bool) error {
	for _, d := range f.ast.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec) // a var declaration holds only value specs
			if len(vs.Values) == 0 {
				continue
			}
			if allConstant(vs.Values, info) {
				for _, v := range vs.Values {
					kept[v] = true
				}
				continue
			}
			start := vs.Names[len(vs.Names)-1].End()
			text := ""
			if vs.Type != nil {
				start = vs.Type.End()
			} else {
				var typ types.Type
				for _, name := range vs.Names {
					obj := info.Defs[name]
					if obj == nil {
						return fmt.Errorf("%s: %s has no object", f.name, name.Name)
					}
					if typ != nil && !types.Identical(typ, obj.Type()) {
						return fmt.Errorf("%s: %s shares a declaration with a variable of another type", f.name, name.Name)
					}
					typ = obj.Type()
				}
				ts, err := typeString(f, typ, self)
				if err != nil {
					return err
				}
				text = " " + ts
			}
			f.removals = append(f.removals, edit{f.off(start), f.off(vs.Values[len(vs.Values)-1].End()), text})
		}
	}
	return nil
}

// annotateAccepted appends each gosecAccepted directive of the file to the end
// of its variable's declaration.
func annotateAccepted(f *sourceFile) error {
	for _, acc := range gosecAccepted {
		if acc.file != f.name {
			continue
		}
		found := false
		for _, d := range f.ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec) // a var declaration holds only value specs
				for _, name := range vs.Names {
					if name.Name == acc.variable {
						f.inserts = append(f.inserts, edit{start: f.off(vs.End()), text: " // " + acc.directive})
						found = true
					}
				}
			}
		}
		if !found {
			return fmt.Errorf("%s declares no variable %s to carry %q", f.name, acc.variable, acc.directive)
		}
	}
	return nil
}

// allConstant reports whether every value is a constant expression. A variable
// initialised with constants is laid out by the compiler as static data: its
// initialisation constructs, parses, and compiles nothing, so it is kept as
// chroma wrote it.
func allConstant(values []ast.Expr, info *types.Info) bool {
	for _, v := range values {
		if tv, ok := info.Types[v]; !ok || tv.Value == nil {
			return false
		}
	}
	return true
}

// typeString spells a type the way the file can name it.
func typeString(f *sourceFile, t types.Type, self *types.Package) (string, error) {
	var missing string
	s := types.TypeString(t, func(p *types.Package) string {
		if p == self {
			return ""
		}
		for _, imp := range f.ast.Imports {
			ip, _ := strconv.Unquote(imp.Path.Value) // the parser accepted it as a string literal
			if ip != p.Path() {
				continue
			}
			if imp.Name == nil {
				return p.Name()
			}
			if imp.Name.Name == "." {
				return ""
			}
			return imp.Name.Name
		}
		missing = p.Path()
		return p.Name()
	})
	if missing != "" {
		return "", fmt.Errorf("%s: type %s names %s, which the file does not import", f.name, s, missing)
	}
	return s, nil
}
