package skillsinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// These tests exercise SPEC/SKILLS.md § Skills Install Command in-process: a
// local HTTPS server serves archives the tests build, the three tiers are
// directories under the test's temporary directory, and the rename, the
// removal, the owner reading and the signal channel are substituted where a
// test needs a failure or a delivery at a chosen point (§ Install
// Verification). No test reaches the network or a real skills directory.

// ---------------------------------------------------------------------------
// Archives
// ---------------------------------------------------------------------------

const top = "Groadmap-main/"

// entry is one tar entry of a test archive.
type entry struct {
	pax  map[string]string
	name string
	body string
	link string
	size int64 // when > 0, the body is that many 'x' bytes
	typ  byte
}

func dirE(name string) entry        { return entry{name: name, typ: tar.TypeDir} }
func fileE(name, body string) entry { return entry{name: name, typ: tar.TypeReg, body: body} }

const (
	rmSkillBody  = "---\nname: roadmap-manager\n---\n\n# Roadmap manager (fetched)\n"
	rmCLIBody    = "# CLI reference (fetched)\n"
	kaSkillBody  = "---\nname: knowledge-authority\n---\n\n# Knowledge authority (fetched)\n"
	readmeBody   = "# Groadmap\n"
	sha1OfCommit = "2f6a484c42df617cf48a1a5fd69890921a70e673"
)

// goodEntries is an archive shaped like GitHub's archive of main: a pax global
// header, one top-level directory, files outside the skills, a symbolic link
// outside the skills (which must be ignored), hidden entries inside a skill
// (which must be skipped), and the two skills.
func goodEntries() []entry {
	return []entry{
		{name: "pax_global_header", typ: tar.TypeXGlobalHeader, pax: map[string]string{"comment": sha1OfCommit}},
		dirE(top),
		fileE(top+"README.md", readmeBody),
		{name: top + "docs-link", typ: tar.TypeSymlink, link: "README.md"},
		dirE(top + "skills/"),
		dirE(top + "skills/roadmap-manager/"),
		fileE(top+"skills/roadmap-manager/SKILL.md", rmSkillBody),
		dirE(top + "skills/roadmap-manager/references/"),
		fileE(top+"skills/roadmap-manager/references/cli.md", rmCLIBody),
		fileE(top+"skills/roadmap-manager/.DS_Store", "finder metadata"),
		{name: top + "skills/roadmap-manager/.git/HEAD", typ: tar.TypeSymlink, link: "/etc/passwd"},
		dirE(top + "skills/knowledge-authority/"),
		fileE(top+"skills/knowledge-authority/SKILL.md", kaSkillBody),
	}
}

// wantInstalled is the file set the good archive installs, relative to the
// skills directory.
var wantInstalled = map[string]string{
	"roadmap-manager/SKILL.md":          rmSkillBody,
	"roadmap-manager/references/cli.md": rmCLIBody,
	"knowledge-authority/SKILL.md":      kaSkillBody,
}

func buildArchive(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		body := e.body
		if e.size > 0 {
			body = strings.Repeat("x", int(e.size))
		}
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644, Linkname: e.link, PAXRecords: e.pax, ModTime: time.Unix(1700000000, 0)}
		switch e.typ {
		case tar.TypeReg, regularTypeLegacy:
			hdr.Size = int64(len(body))
		case tar.TypeDir:
			hdr.Mode = 0o755
		case tar.TypeXGlobalHeader:
			hdr = &tar.Header{Name: e.name, Typeflag: e.typ, PAXRecords: e.pax, Format: tar.FormatPAX}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %q: %v", e.name, err)
		}
		if hdr.Size > 0 {
			if _, err := io.WriteString(tw, body); err != nil {
				t.Fatalf("write body %q: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withEntry returns goodEntries with the entry named name replaced by e, or
// e appended when nothing is named name.
func withEntry(name string, e entry) []entry {
	out := goodEntries()
	for i := range out {
		if out[i].name == name {
			out[i] = e
			return out
		}
	}
	return append(out, e)
}

func withoutEntry(name string) []entry {
	var out []entry
	for _, e := range goodEntries() {
		if e.name != name {
			out = append(out, e)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// errRenamed is returned by a rename hook that has performed the rename
// itself.
var errRenamed = errors.New("renamed by the hook")

type harness struct {
	t          *testing.T
	srv        *httptest.Server
	sigCh      chan os.Signal
	renameHook func(n int, oldpath, newpath string) error
	ownerHook  func(fi fs.FileInfo) (uint32, bool, bool)
	removeHook func(path string) error
	handler    http.HandlerFunc
	root       string
	tempDir    string
	archive    []byte
	warnings   []string
	renames    int
	released   int
	mu         sync.Mutex
}

func newHarness(t *testing.T, entries []entry) *harness {
	t.Helper()
	h := &harness{t: t, root: t.TempDir(), sigCh: make(chan os.Signal, 1)}
	h.tempDir = filepath.Join(h.root, "tmp")
	if err := os.Mkdir(h.tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	h.archive = buildArchive(t, entries)
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.handler != nil {
			h.handler(w, r)
			return
		}
		_, _ = w.Write(h.archive)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *harness) source() string { return h.srv.URL + "/archive/refs/heads/main.tar.gz" }

func (h *harness) options(tiers ...Tier) *Options {
	return &Options{
		Transport: h.srv.Client().Transport,
		Source:    h.source(),
		TempDir:   h.tempDir,
		Tiers:     tiers,
		Warn: func(line string) {
			h.mu.Lock()
			h.warnings = append(h.warnings, line)
			h.mu.Unlock()
		},
		takeOver: func() (<-chan os.Signal, func()) {
			return h.sigCh, func() { h.released++ }
		},
		rename: func(oldpath, newpath string) error {
			h.renames++
			if h.renameHook != nil {
				err := h.renameHook(h.renames, oldpath, newpath)
				if errors.Is(err, errRenamed) {
					return nil
				}
				if err != nil {
					return err
				}
			}
			return os.Rename(oldpath, newpath)
		},
		ownerOf: func(fi fs.FileInfo) (uint32, bool) {
			if h.ownerHook != nil {
				if uid, ok, override := h.ownerHook(fi); override {
					return uid, ok
				}
			}
			return fileOwner(fi)
		},
		removeAll: func(path string) error {
			if h.removeHook != nil {
				if err := h.removeHook(path); err != nil {
					return err
				}
			}
			return os.RemoveAll(path)
		},
	}
}

// tier returns a tier rooted under the harness, shaped as DefaultTiers shapes
// the tier of the same scope.
func (h *harness) tier(scope string) Tier {
	base := filepath.Join(h.root, scope)
	cfg := filepath.Join(base, ".claude")
	skills := filepath.Join(cfg, "skills")
	tr := Tier{Scope: scope, ConfigDir: cfg, SkillsDir: skills, Create: []string{cfg, skills}, DirMode: privateDirMode, FileMode: privateFileMode}
	if scope == ScopeGlobal {
		tr.Create = []string{base, cfg, skills}
		tr.DirMode, tr.FileMode, tr.AcceptRoot = sharedDirMode, sharedFileMode, true
	} else if err := os.MkdirAll(base, 0o700); err != nil {
		h.t.Fatal(err)
	}
	return tr
}

func (h *harness) three() (global, user, project Tier) {
	return h.tier(ScopeGlobal), h.tier(ScopeUser), h.tier(ScopeProject)
}

// blockGlobal makes the global tier fail without privileges: its managed
// settings directory is a regular file, so its configuration directory cannot
// be examined.
func (h *harness) blockGlobal(g *Tier) string {
	managed := filepath.Dir(g.ConfigDir)
	writeFile(h.t, managed, "not a directory", 0o600)
	return fmt.Sprintf("cannot examine %s: stat %s: not a directory", g.ConfigDir, g.ConfigDir)
}

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// installPrevious puts a previous version of both skills in skillsDir, with a
// file the fetched archive does not ship.
func installPrevious(t *testing.T, skillsDir string) {
	t.Helper()
	writeFile(t, filepath.Join(skillsDir, "roadmap-manager", "SKILL.md"), "previous roadmap-manager\n", 0o600)
	writeFile(t, filepath.Join(skillsDir, "roadmap-manager", "references", "stale.md"), "no longer shipped\n", 0o600)
	writeFile(t, filepath.Join(skillsDir, "knowledge-authority", "SKILL.md"), "previous knowledge-authority\n", 0o600)
}

// snapshot records every entry under dir: its type, mode and content, or link
// target, so two snapshots compare byte for byte.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return out
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		fi, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			out[rel] = "link " + target
		case fi.IsDir():
			out[rel] = "dir " + fi.Mode().String()
		default:
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[rel] = "file " + fi.Mode().String() + " " + string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func requireSame(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("%s changed:\nbefore: %v\nafter:  %v", what, before, after)
	}
}

// installedFiles returns the regular files under skillsDir's two skills with
// their content.
func installedFiles(t *testing.T, skillsDir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, skill := range skillNames {
		for rel, v := range snapshot(t, filepath.Join(skillsDir, skill)) {
			if strings.HasPrefix(v, "file ") {
				parts := strings.SplitN(v, " ", 3)
				out[filepath.ToSlash(filepath.Join(skill, rel))] = parts[2]
			}
		}
	}
	return out
}

func requireInstalled(t *testing.T, skillsDir string) {
	t.Helper()
	got := installedFiles(t, skillsDir)
	if fmt.Sprint(got) != fmt.Sprint(wantInstalled) {
		t.Fatalf("installed files under %s:\n got %v\nwant %v", skillsDir, got, wantInstalled)
	}
}

// requirePrevious asserts skillsDir holds exactly the previous version
// installPrevious wrote.
func requirePrevious(t *testing.T, skillsDir string) {
	t.Helper()
	want := map[string]string{
		"roadmap-manager/SKILL.md":            "previous roadmap-manager\n",
		"roadmap-manager/references/stale.md": "no longer shipped\n",
		"knowledge-authority/SKILL.md":        "previous knowledge-authority\n",
	}
	got := installedFiles(t, skillsDir)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("skills under %s are not the previous versions:\n got %v\nwant %v", skillsDir, got, want)
	}
}

// requireNoTemp asserts no staging directory and no work directory is left
// behind (SPEC/SKILLS.md acceptance criterion 28).
func (h *harness) requireNoTemp(skillsDirs ...string) {
	h.t.Helper()
	entries, err := os.ReadDir(h.tempDir)
	if err != nil {
		h.t.Fatal(err)
	}
	if len(entries) != 0 {
		h.t.Fatalf("the staging parent %s still holds %v", h.tempDir, entries)
	}
	for _, dir := range skillsDirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			h.t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), workPattern) {
				h.t.Fatalf("a work directory is left behind: %s", filepath.Join(dir, e.Name()))
			}
		}
	}
}

func requireAbsent(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s exists, and nothing should have created it (err %v)", p, err)
		}
	}
}

func requireErrorLine(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error; want %q", want)
	}
	if !errors.Is(err, utils.ErrIO) {
		t.Errorf("error %q does not carry utils.ErrIO", err)
	}
	if err.Error() != want {
		t.Fatalf("error line\n got %q\nwant %q", err.Error(), want)
	}
}

func requireModes(t *testing.T, skillsDir string, dirMode, fileMode os.FileMode) {
	t.Helper()
	for _, skill := range skillNames {
		err := filepath.WalkDir(filepath.Join(skillsDir, skill), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			fi, err := os.Lstat(path)
			if err != nil {
				return err
			}
			want := fileMode
			if fi.IsDir() {
				want = dirMode
			}
			if fi.Mode().Perm() != want {
				t.Errorf("%s has mode %v, want %v", path, fi.Mode().Perm(), want)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func resultJSON(t *testing.T, r *Result) string {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Tiers: success and fallback (acceptance criteria 21 to 24)
// ---------------------------------------------------------------------------

func TestInstall_GlobalTierInstallsWithSharedModes(t *testing.T) {
	h := newHarness(t, goodEntries())
	g, u, p := h.three()
	res, err := Install(h.options(g, u, p))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := fmt.Sprintf(`{"scope":"global","directory":%q,"skills":["roadmap-manager","knowledge-authority"],"source":%q,"skipped":[]}`, g.SkillsDir, h.source())
	if got := resultJSON(t, res); got != want {
		t.Fatalf("result\n got %s\nwant %s", got, want)
	}
	requireInstalled(t, g.SkillsDir)
	requireModes(t, g.SkillsDir, sharedDirMode, sharedFileMode)
	for _, dir := range g.Create {
		fi, err := os.Stat(dir)
		if err != nil || fi.Mode().Perm() != sharedDirMode {
			t.Errorf("created directory %s: %v, mode %v; want %v", dir, err, fi.Mode().Perm(), sharedDirMode)
		}
	}
	requireAbsent(t, u.ConfigDir, p.ConfigDir)
	if len(h.warnings) != 0 {
		t.Fatalf("warnings on a first-tier success: %q", h.warnings)
	}
	if h.released != 1 {
		t.Fatalf("the signal take-over was released %d times, want 1", h.released)
	}
	h.requireNoTemp(g.SkillsDir)
}

func TestInstall_FallsBackToUserTier(t *testing.T) {
	h := newHarness(t, goodEntries())
	g, u, p := h.three()
	reason := h.blockGlobal(&g)
	res, err := Install(h.options(g, u, p))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := fmt.Sprintf(`{"scope":"user","directory":%q,"skills":["roadmap-manager","knowledge-authority"],"source":%q,"skipped":[{"scope":"global","reason":%q}]}`,
		u.SkillsDir, h.source(), reason)
	if got := resultJSON(t, res); got != want {
		t.Fatalf("result\n got %s\nwant %s", got, want)
	}
	wantWarn := []string{"warning: the global skills directory was not used, trying the user skills directory: " + reason}
	if fmt.Sprint(h.warnings) != fmt.Sprint(wantWarn) {
		t.Fatalf("warnings\n got %q\nwant %q", h.warnings, wantWarn)
	}
	requireInstalled(t, u.SkillsDir)
	requireModes(t, u.SkillsDir, privateDirMode, privateFileMode)
	for _, dir := range u.Create {
		if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != privateDirMode {
			t.Errorf("created directory %s is not 0700 (%v)", dir, err)
		}
	}
	requireAbsent(t, p.ConfigDir)
	h.requireNoTemp(u.SkillsDir)
}

func TestInstall_FallsBackToProjectTierAndLeavesTheRefusedPathAsFound(t *testing.T) {
	h := newHarness(t, goodEntries())
	g, u, p := h.three()
	globalReason := h.blockGlobal(&g)
	target := filepath.Join(h.root, "elsewhere")
	writeFile(t, filepath.Join(target, "SKILL.md"), "someone else's skill\n", 0o600)
	if err := os.MkdirAll(u.SkillsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(u.SkillsDir, "roadmap-manager")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	beforeUser, beforeTarget := snapshot(t, u.SkillsDir), snapshot(t, target)

	res, err := Install(h.options(g, u, p))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	userReason := link + " is a symbolic link"
	want := fmt.Sprintf(`{"scope":"project","directory":%q,"skills":["roadmap-manager","knowledge-authority"],"source":%q,"skipped":[{"scope":"global","reason":%q},{"scope":"user","reason":%q}]}`,
		p.SkillsDir, h.source(), globalReason, userReason)
	if got := resultJSON(t, res); got != want {
		t.Fatalf("result\n got %s\nwant %s", got, want)
	}
	wantWarn := []string{
		"warning: the global skills directory was not used, trying the user skills directory: " + globalReason,
		"warning: the user skills directory was not used, trying the project skills directory: " + userReason,
	}
	if fmt.Sprint(h.warnings) != fmt.Sprint(wantWarn) {
		t.Fatalf("warnings\n got %q\nwant %q", h.warnings, wantWarn)
	}
	requireInstalled(t, p.SkillsDir)
	requireModes(t, p.SkillsDir, privateDirMode, privateFileMode)
	requireSame(t, "the refused user skills directory", beforeUser, snapshot(t, u.SkillsDir))
	requireSame(t, "the symbolic link's target", beforeTarget, snapshot(t, target))
	h.requireNoTemp(u.SkillsDir, p.SkillsDir)
}

func TestInstall_AllTiersRefusedFailsAndChangesNothing(t *testing.T) {
	h := newHarness(t, goodEntries())
	g, u, p := h.three()
	globalReason := h.blockGlobal(&g)
	installPrevious(t, u.SkillsDir)
	installPrevious(t, p.SkillsDir)
	if err := os.Chmod(u.SkillsDir, 0o777); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(p.SkillsDir, "knowledge-authority-file")
	writeFile(t, stray, "x", 0o600)
	if err := os.RemoveAll(filepath.Join(p.SkillsDir, "knowledge-authority")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stray, filepath.Join(p.SkillsDir, "knowledge-authority")); err != nil {
		t.Fatal(err)
	}
	beforeUser, beforeProject := snapshot(t, u.SkillsDir), snapshot(t, p.SkillsDir)

	res, err := Install(h.options(g, u, p))
	if res != nil {
		t.Fatalf("a result was returned: %+v", res)
	}
	userReason := u.SkillsDir + " is writable by every user and carries no sticky bit"
	projectReason := filepath.Join(p.SkillsDir, "knowledge-authority") + " is not a directory"
	requireErrorLine(t, err, "I/O error: the skills could not be installed in the global, user, or project skills directory; the project skills directory was not used: "+projectReason)
	wantWarn := []string{
		"warning: the global skills directory was not used, trying the user skills directory: " + globalReason,
		"warning: the user skills directory was not used, trying the project skills directory: " + userReason,
	}
	if fmt.Sprint(h.warnings) != fmt.Sprint(wantWarn) {
		t.Fatalf("warnings\n got %q\nwant %q", h.warnings, wantWarn)
	}
	requireSame(t, "the user skills directory", beforeUser, snapshot(t, u.SkillsDir))
	requireSame(t, "the project skills directory", beforeProject, snapshot(t, p.SkillsDir))
	h.requireNoTemp(u.SkillsDir, p.SkillsDir)
}

func TestInstall_UnavailableTierTouchesNothing(t *testing.T) {
	h := newHarness(t, goodEntries())
	g, u, p := h.three()
	g.Unavailable = "no managed settings directory is documented for this platform"
	res, err := Install(h.options(g, u, p))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Scope != ScopeUser || len(res.Skipped) != 1 || res.Skipped[0].Reason != g.Unavailable {
		t.Fatalf("result %+v", res)
	}
	requireAbsent(t, filepath.Dir(g.ConfigDir))
}

// ---------------------------------------------------------------------------
// Tier destination rules (SPEC/SKILLS.md § Tier Destination Rules)
// ---------------------------------------------------------------------------

func TestInstall_DestinationRefusals(t *testing.T) {
	cases := []struct {
		setup     func(h *harness, tr Tier) string // returns the expected reason
		ownerHook func(fi fs.FileInfo) (uint32, bool, bool)
		name      string
		scope     string
	}{
		{name: "configuration directory is a file", scope: ScopeUser, setup: func(h *harness, tr Tier) string {
			writeFile(h.t, tr.ConfigDir, "x", 0o600)
			return tr.ConfigDir + " is not a directory"
		}},
		{name: "skills directory is a file", scope: ScopeUser, setup: func(h *harness, tr Tier) string {
			writeFile(h.t, tr.SkillsDir, "x", 0o600)
			return tr.SkillsDir + " is not a directory"
		}},
		{name: "skill path is a file", scope: ScopeUser, setup: func(h *harness, tr Tier) string {
			path := filepath.Join(tr.SkillsDir, "knowledge-authority")
			writeFile(h.t, path, "x", 0o600)
			return path + " is not a directory"
		}},
		{name: "skill path is a symbolic link", scope: ScopeUser, setup: func(h *harness, tr Tier) string {
			path := filepath.Join(tr.SkillsDir, "knowledge-authority")
			if err := os.MkdirAll(tr.SkillsDir, 0o700); err != nil {
				h.t.Fatal(err)
			}
			if err := os.Symlink(h.root, path); err != nil {
				h.t.Fatal(err)
			}
			return path + " is a symbolic link"
		}},
		{name: "skills directory writable by every user", scope: ScopeProject, setup: func(h *harness, tr Tier) string {
			if err := os.MkdirAll(tr.SkillsDir, 0o700); err != nil {
				h.t.Fatal(err)
			}
			if err := os.Chmod(tr.SkillsDir, 0o777); err != nil {
				h.t.Fatal(err)
			}
			return tr.SkillsDir + " is writable by every user and carries no sticky bit"
		}},
		{name: "skill path owned by another user", scope: ScopeUser,
			ownerHook: func(fi fs.FileInfo) (uint32, bool, bool) {
				return 4242, true, fi.Name() == "roadmap-manager"
			},
			setup: func(h *harness, tr Tier) string {
				installPrevious(h.t, tr.SkillsDir)
				return filepath.Join(tr.SkillsDir, "roadmap-manager") + " is not owned by the invoking user"
			}},
		{name: "skills directory owned by another user", scope: ScopeProject,
			ownerHook: func(fi fs.FileInfo) (uint32, bool, bool) {
				return 4242, true, fi.Name() == "skills"
			},
			setup: func(h *harness, tr Tier) string {
				installPrevious(h.t, tr.SkillsDir)
				return tr.SkillsDir + " is not owned by the invoking user"
			}},
		{name: "global skill path owned neither by root nor by the invoking user", scope: ScopeGlobal,
			ownerHook: func(fi fs.FileInfo) (uint32, bool, bool) {
				return 4242, true, fi.Name() == "knowledge-authority"
			},
			setup: func(h *harness, tr Tier) string {
				installPrevious(h.t, tr.SkillsDir)
				return filepath.Join(tr.SkillsDir, "knowledge-authority") + " is owned neither by root nor by the invoking user"
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, goodEntries())
			h.ownerHook = c.ownerHook
			refused := h.tier(c.scope)
			reason := c.setup(h, refused)
			before := snapshot(t, filepath.Dir(refused.ConfigDir))
			next := h.tier("fallback")
			next.Scope = ScopeProject

			res, err := Install(h.options(refused, next))
			if err != nil {
				t.Fatalf("Install: %v", err)
			}
			if len(res.Skipped) != 1 || res.Skipped[0].Reason != reason {
				t.Fatalf("skipped %+v, want the reason %q", res.Skipped, reason)
			}
			requireSame(t, "the refused destination", before, snapshot(t, filepath.Dir(refused.ConfigDir)))
			requireInstalled(t, next.SkillsDir)
			h.requireNoTemp(next.SkillsDir)
		})
	}
}

func TestInstall_StickyWorldWritableAndRootOwnedGlobalAreAccepted(t *testing.T) {
	h := newHarness(t, goodEntries())
	g := h.tier(ScopeGlobal)
	if err := os.MkdirAll(g.SkillsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(g.SkillsDir, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	// The global skills directory reads as owned by root, which the global
	// tier accepts beside the invoking user.
	h.ownerHook = func(fi fs.FileInfo) (uint32, bool, bool) {
		return 0, true, fi.Name() == "skills"
	}
	res, err := Install(h.options(g))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.Scope != ScopeGlobal {
		t.Fatalf("scope %q", res.Scope)
	}
	requireInstalled(t, g.SkillsDir)
}

func TestOwnerReasonFor(t *testing.T) {
	cases := []struct {
		want       string
		uid        uint32
		euid       int
		acceptRoot bool
	}{
		{"", 1000, 1000, false},
		{"", 1000, 1000, true},
		{"", 0, 1000, true},
		{"/p is not owned by the invoking user", 0, 1000, false},
		{"/p is not owned by the invoking user", 1001, 1000, false},
		{"/p is owned neither by root nor by the invoking user", 1001, 1000, true},
		{"", 0, 0, false},
		{"/p is not owned by the invoking user", 0, -1, false},
	}
	for _, c := range cases {
		if got := ownerReasonFor(c.uid, "/p", c.euid, c.acceptRoot); got != c.want {
			t.Errorf("ownerReasonFor(%d, euid %d, root %v) = %q, want %q", c.uid, c.euid, c.acceptRoot, got, c.want)
		}
	}
}

func TestDefaultTiers(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home", "alice")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	configVar := "CLAUDE_CONFIG_DIR"

	t.Run("unset", func(t *testing.T) {
		t.Setenv(configVar, "")
		tiers := DefaultTiers(home)
		if len(tiers) != 3 || tiers[0].Scope != ScopeGlobal || tiers[1].Scope != ScopeUser || tiers[2].Scope != ScopeProject {
			t.Fatalf("tiers %+v", tiers)
		}
		g := tiers[0]
		if managed := managedSettingsDir(); managed == "" {
			if g.Unavailable != "no managed settings directory is documented for this platform" {
				t.Errorf("global tier %+v", g)
			}
		} else if g.SkillsDir != filepath.Join(managed, ".claude", "skills") || !g.AcceptRoot ||
			g.DirMode != 0o755 || g.FileMode != 0o644 || len(g.Create) != 3 || g.Create[0] != managed {
			t.Errorf("global tier %+v", g)
		}
		if u := tiers[1]; u.SkillsDir != filepath.Join(home, ".claude", "skills") || u.AcceptRoot ||
			u.DirMode != 0o700 || u.FileMode != 0o600 {
			t.Errorf("user tier %+v", u)
		}
		if p := tiers[2]; p.SkillsDir != filepath.Join(cwd, ".claude", "skills") || p.DirMode != 0o700 || p.FileMode != 0o600 {
			t.Errorf("project tier %+v", p)
		}
	})
	t.Run("absolute", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "relocated")
		t.Setenv(configVar, dir)
		u := DefaultTiers(home)[1]
		if u.ConfigDir != dir || u.SkillsDir != filepath.Join(dir, "skills") || u.Unavailable != "" {
			t.Errorf("user tier %+v", u)
		}
		if strings.HasPrefix(u.SkillsDir, home) {
			t.Errorf("a relocated configuration directory still installs under the home: %s", u.SkillsDir)
		}
	})
	t.Run("relative", func(t *testing.T) {
		t.Setenv(configVar, "relative/config")
		u := DefaultTiers(home)[1]
		want := configVar + ` "relative/config" is not an absolute path`
		if u.Unavailable != want || len(u.Create) != 0 {
			t.Errorf("user tier %+v, want unavailable with %q", u, want)
		}
	})
}

func TestManagedSettingsDirFor(t *testing.T) {
	cases := map[string]string{
		"linux":   "/etc/claude-code",
		"darwin":  "/Library/Application Support/ClaudeCode",
		"windows": `C:\Program Files\ClaudeCode`,
		"freebsd": "",
		"openbsd": "",
	}
	for goos, want := range cases {
		if got := managedSettingsDirFor(goos); got != want {
			t.Errorf("managedSettingsDirFor(%q) = %q, want %q", goos, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Full replacement (acceptance criteria 26 and 30)
// ---------------------------------------------------------------------------

func TestInstall_ReplacesEntirelyAndLeavesSiblingsByteIdentical(t *testing.T) {
	h := newHarness(t, goodEntries())
	u := h.tier(ScopeUser)
	installPrevious(t, u.SkillsDir)
	sibling := filepath.Join(u.SkillsDir, "other-skill")
	writeFile(t, filepath.Join(sibling, "SKILL.md"), "an unrelated skill\n", 0o640)
	writeFile(t, filepath.Join(sibling, "references", "notes.md"), "notes\n", 0o600)
	before := snapshot(t, sibling)

	if _, err := Install(h.options(u)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	requireInstalled(t, u.SkillsDir)
	requireAbsent(t, filepath.Join(u.SkillsDir, "roadmap-manager", "references", "stale.md"))
	requireSame(t, "the sibling skill", before, snapshot(t, sibling))
	h.requireNoTemp(u.SkillsDir)
}

func TestInstall_SecondRunReplacesAgainWithoutReadingStdin(t *testing.T) {
	h := newHarness(t, goodEntries())
	u := h.tier(ScopeUser)

	// Standard input is a pipe holding one byte. A run that read it would
	// consume the byte.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if _, err := w.WriteString("y"); err != nil {
		t.Fatal(err)
	}
	saved := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = saved }()

	if _, err := Install(h.options(u)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	writeFile(t, filepath.Join(u.SkillsDir, "roadmap-manager", "SKILL.md"), "edited locally\n", 0o600)
	writeFile(t, filepath.Join(u.SkillsDir, "knowledge-authority", "local.md"), "added locally\n", 0o600)
	if _, err := Install(h.options(u)); err != nil {
		t.Fatalf("second run: %v", err)
	}
	requireInstalled(t, u.SkillsDir)
	h.requireNoTemp(u.SkillsDir)

	os.Stdin = saved
	buf := make([]byte, 1)
	if n, err := r.Read(buf); n != 1 || err != nil || buf[0] != 'y' {
		t.Fatalf("standard input was read: %d %v %q", n, err, buf)
	}
}

// ---------------------------------------------------------------------------
// Download failures (acceptance criterion 25)
// ---------------------------------------------------------------------------

// requireNothingAttempted asserts a failed download created no directory at
// any tier and left no staging directory.
func (h *harness) requireNothingAttempted(tiers ...Tier) {
	h.t.Helper()
	for _, tr := range tiers {
		requireAbsent(h.t, tr.ConfigDir)
	}
	requireAbsent(h.t, filepath.Join(h.root, ScopeGlobal))
	h.requireNoTemp()
	if h.renames != 0 {
		h.t.Fatalf("%d renames ran after a failed download", h.renames)
	}
}

func TestInstall_DownloadFailures(t *testing.T) {
	zeros := func(w io.Writer, n int64) {
		chunk := make([]byte, 1<<20)
		for n > 0 {
			k := int64(len(chunk))
			if n < k {
				k = n
			}
			if _, err := w.Write(chunk[:k]); err != nil {
				return
			}
			n -= k
		}
	}

	cases := []struct {
		handler func(h *harness) http.HandlerFunc
		check   func(t *testing.T, h *harness, err error)
		name    string
		timeout time.Duration
	}{
		{name: "status other than 200",
			handler: func(h *harness) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "gone", http.StatusNotFound) }
			},
			check: func(t *testing.T, h *harness, err error) {
				requireErrorLine(t, err, "I/O error: cannot download the skills from "+h.source()+": HTTP status 404")
			}},
		{name: "time bound expires",
			timeout: 200 * time.Millisecond,
			handler: func(h *harness) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(h.archive[:10])
					w.(http.Flusher).Flush()
					select {
					case <-r.Context().Done():
					case <-time.After(10 * time.Second):
					}
				}
			},
			check: func(t *testing.T, h *harness, err error) {
				prefix := "I/O error: cannot download the skills from " + h.source() + ": "
				if err == nil || !strings.HasPrefix(err.Error(), prefix) || !errors.Is(err, utils.ErrIO) {
					t.Fatalf("error %v, want the download line", err)
				}
			}},
		{name: "redirect to a scheme other than https",
			handler: func(h *harness) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "http://127.0.0.1:1/archive.tar.gz", http.StatusFound)
				}
			},
			check: func(t *testing.T, h *harness, err error) {
				if err == nil || !strings.HasPrefix(err.Error(), "I/O error: cannot download the skills from "+h.source()+": ") ||
					!strings.Contains(err.Error(), errRedirectScheme.Error()) {
					t.Fatalf("error %v, want the refused redirect", err)
				}
			}},
		{name: "eleventh redirect",
			handler: func(h *harness) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					n, _ := strconv.Atoi(r.URL.Query().Get("n"))
					http.Redirect(w, r, "/again?n="+strconv.Itoa(n+1), http.StatusFound)
				}
			},
			check: func(t *testing.T, h *harness, err error) {
				if err == nil || !strings.Contains(err.Error(), errTooManyRedirects.Error()) {
					t.Fatalf("error %v, want the redirect bound", err)
				}
			}},
		{name: "declared length over the body bound",
			handler: func(h *harness) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Length", strconv.Itoa(maxBodyBytes+1))
					w.WriteHeader(http.StatusOK)
				}
			},
			check: func(t *testing.T, h *harness, err error) {
				requireErrorLine(t, err, "I/O error: the skills archive exceeds 64 MiB; nothing was installed")
			}},
		{name: "undeclared body over the body bound",
			handler: func(h *harness) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					gz, _ := gzip.NewWriterLevel(w, gzip.NoCompression)
					zeros(gz, maxBodyBytes+(1<<20))
					_ = gz.Close()
				}
			},
			check: func(t *testing.T, h *harness, err error) {
				requireErrorLine(t, err, "I/O error: the skills archive exceeds 64 MiB; nothing was installed")
			}},
		{name: "decompressed stream over its bound",
			handler: func(h *harness) http.HandlerFunc {
				bomb := compressedZeros(t, maxStreamBytes+(1<<20))
				return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bomb) }
			},
			check: func(t *testing.T, h *harness, err error) {
				requireErrorLine(t, err, "I/O error: the skills archive expands to more than 256 MiB; nothing was installed")
			}},
		{name: "not a gzip stream",
			handler: func(h *harness) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>not an archive</html>") }
			},
			check: func(t *testing.T, h *harness, err error) {
				requireErrorLine(t, err, "I/O error: the skills archive is not a valid gzip-compressed tar archive: gzip: invalid header")
			}},
		{name: "truncated stream",
			handler: func(h *harness) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(h.archive[:len(h.archive)/2]) }
			},
			check: func(t *testing.T, h *harness, err error) {
				requireErrorLine(t, err, "I/O error: the skills archive is not a valid gzip-compressed tar archive: unexpected EOF")
			}},
		{name: "corrupted trailer",
			handler: func(h *harness) http.HandlerFunc {
				bad := append([]byte(nil), h.archive...)
				bad[len(bad)-8] ^= 0xff // the CRC-32 of the trailer
				return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(bad) }
			},
			check: func(t *testing.T, h *harness, err error) {
				requireErrorLine(t, err, "I/O error: the skills archive is not a valid gzip-compressed tar archive: gzip: invalid checksum")
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, goodEntries())
			h.handler = c.handler(h)
			g, u, p := h.three()
			opts := h.options(g, u, p)
			opts.timeout = c.timeout
			res, err := Install(opts)
			if res != nil {
				t.Fatalf("a result was returned: %+v", res)
			}
			c.check(t, h, err)
			h.requireNothingAttempted(g, u, p)
		})
	}
}

var (
	bombOnce sync.Once
	bomb     []byte
)

// compressedZeros returns a gzip stream of n zero bytes, built once.
func compressedZeros(t *testing.T, n int64) []byte {
	t.Helper()
	bombOnce.Do(func() {
		var buf bytes.Buffer
		gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		chunk := make([]byte, 1<<20)
		for left := n; left > 0; left -= int64(len(chunk)) {
			if _, err := gz.Write(chunk); err != nil {
				t.Fatal(err)
			}
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		bomb = buf.Bytes()
	})
	return bomb
}

func TestInstall_RefusedConnection(t *testing.T) {
	h := newHarness(t, goodEntries())
	dead := httptest.NewTLSServer(http.NotFoundHandler())
	source := dead.URL + "/archive/refs/heads/main.tar.gz"
	transport := dead.Client().Transport
	dead.Close()
	g, u, p := h.three()
	opts := h.options(g, u, p)
	opts.Source, opts.Transport = source, transport
	_, err := Install(opts)
	prefix := "I/O error: cannot download the skills from " + source + ": "
	if err == nil || !strings.HasPrefix(err.Error(), prefix) || !errors.Is(err, utils.ErrIO) {
		t.Fatalf("error %v, want the download line", err)
	}
	h.requireNothingAttempted(g, u, p)
}

func TestInstall_SourceMustBeHTTPS(t *testing.T) {
	h := newHarness(t, goodEntries())
	u := h.tier(ScopeUser)
	opts := h.options(u)
	opts.Source = "http://127.0.0.1:1/archive.tar.gz"
	_, err := Install(opts)
	requireErrorLine(t, err, "I/O error: cannot download the skills from "+opts.Source+": "+errNotHTTPS.Error())
	h.requireNothingAttempted(u)
}

func TestInstall_OneRedirectIsFollowed(t *testing.T) {
	h := newHarness(t, goodEntries())
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/codeload/main.tar.gz" {
			http.Redirect(w, r, "/codeload/main.tar.gz", http.StatusFound)
			return
		}
		_, _ = w.Write(h.archive)
	}
	u := h.tier(ScopeUser)
	if _, err := Install(h.options(u)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	requireInstalled(t, u.SkillsDir)
}

// ---------------------------------------------------------------------------
// Archive validation (SPEC/SKILLS.md § Install Archive Validation)
// ---------------------------------------------------------------------------

func TestInstall_MalformedArchives(t *testing.T) {
	rm := top + "skills/roadmap-manager/"
	cases := []struct {
		name    string
		want    string
		entries []entry
	}{
		{"absolute name", `I/O error: the skills archive is malformed: entry "/etc/cron.d/job" has an absolute name`,
			withEntry("/etc/cron.d/job", fileE("/etc/cron.d/job", "x"))},
		{"dot-dot component outside the skills",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/docs/../../escape" has an empty, "." or ".." path component`,
			withEntry(top+"docs/../../escape", fileE(top+"docs/../../escape", "x"))},
		{"dot-dot component inside a skill",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/skills/roadmap-manager/../../x" has an empty, "." or ".." path component`,
			withEntry(rm+"../../x", fileE(rm+"../../x", "x"))},
		{"dot component",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/./skills" has an empty, "." or ".." path component`,
			withEntry(top+"./skills", fileE(top+"./skills", "x"))},
		{"empty component",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/skills//roadmap-manager/x.md" has an empty, "." or ".." path component`,
			withEntry(top+"skills//roadmap-manager/x.md", fileE(top+"skills//roadmap-manager/x.md", "x"))},
		{"selected symbolic link",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/skills/roadmap-manager/references/passwd" is neither a directory nor a regular file`,
			withEntry(rm+"references/passwd", entry{name: rm + "references/passwd", typ: tar.TypeSymlink, link: "/etc/passwd"})},
		{"selected hard link",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/skills/knowledge-authority/copy.md" is neither a directory nor a regular file`,
			withEntry(top+"skills/knowledge-authority/copy.md", entry{name: top + "skills/knowledge-authority/copy.md", typ: tar.TypeLink, link: rm + "SKILL.md"})},
		{"selected character device",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/skills/roadmap-manager/tty" is neither a directory nor a regular file`,
			withEntry(rm+"tty", entry{name: rm + "tty", typ: tar.TypeChar})},
		{"selected FIFO",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/skills/roadmap-manager/pipe" is neither a directory nor a regular file`,
			withEntry(rm+"pipe", entry{name: rm + "pipe", typ: tar.TypeFifo})},
		{"selected skill directory itself a symbolic link",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/skills/knowledge-authority" is neither a directory nor a regular file`,
			withEntry(top+"skills/knowledge-authority/", entry{name: top + "skills/knowledge-authority", typ: tar.TypeSymlink, link: "/tmp"})},
		{"duplicate file",
			`I/O error: the skills archive is malformed: entry "Groadmap-main/skills/roadmap-manager/SKILL.md" appears more than once`,
			append(goodEntries(), fileE(rm+"SKILL.md", "second copy"))},
		{"missing roadmap-manager SKILL.md",
			"I/O error: the skills archive is malformed: it carries no regular file skills/roadmap-manager/SKILL.md",
			withoutEntry(rm + "SKILL.md")},
		{"missing knowledge-authority SKILL.md",
			"I/O error: the skills archive is malformed: it carries no regular file skills/knowledge-authority/SKILL.md",
			withoutEntry(top + "skills/knowledge-authority/SKILL.md")},
		{"selected files over 16 MiB",
			"I/O error: the skills in the archive exceed 16 MiB; nothing was installed",
			withEntry(rm+"references/huge.md", entry{name: rm + "references/huge.md", typ: tar.TypeReg, size: maxSelectedBytes})},
		{"selected entries over 1,000", "I/O error: the skills in the archive exceed 1,000 entries; nothing was installed",
			manyEntries(maxSelectedEntries)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, c.entries)
			g, u, p := h.three()
			res, err := Install(h.options(g, u, p))
			if res != nil {
				t.Fatalf("a result was returned: %+v", res)
			}
			requireErrorLine(t, err, c.want)
			h.requireNothingAttempted(g, u, p)
		})
	}
}

// TestInstall_ExtractionFailureIsReported drives the extraction-failure line:
// an entry whose parent, inside the staging directory, is a regular file the
// archive already wrote, so the operating system cannot create a directory
// there.
func TestInstall_ExtractionFailureIsReported(t *testing.T) {
	rm := top + "skills/roadmap-manager/"
	h := newHarness(t, append(goodEntries(), fileE(rm+"SKILL.md/nested.md", "x")))
	g, u, p := h.three()
	_, err := Install(h.options(g, u, p))
	head := "I/O error: cannot extract the skills archive into the staging directory: "
	if err == nil || !errors.Is(err, utils.ErrIO) || !strings.HasPrefix(err.Error(), head) ||
		!strings.HasSuffix(err.Error(), filepath.Join("roadmap-manager", "SKILL.md")+" is not a directory") {
		t.Fatalf("error %v, want the extraction line", err)
	}
	h.requireNothingAttempted(g, u, p)
}

// manyEntries returns the good archive with n more directories under a skill,
// which puts the selected entries over the bound.
func manyEntries(n int) []entry {
	out := goodEntries()
	for i := range n {
		out = append(out, dirE(fmt.Sprintf("%sskills/roadmap-manager/references/d%04d/", top, i)))
	}
	return out
}

func TestInstall_LegacyRegularTypeAndBoundsAtTheLimitAreAccepted(t *testing.T) {
	entries := withEntry(top+"skills/roadmap-manager/references/cli.md",
		entry{name: top + "skills/roadmap-manager/references/cli.md", typ: regularTypeLegacy, body: rmCLIBody})
	h := newHarness(t, entries)
	u := h.tier(ScopeUser)
	if _, err := Install(h.options(u)); err != nil {
		t.Fatalf("Install: %v", err)
	}
	requireInstalled(t, u.SkillsDir)
}

// ---------------------------------------------------------------------------
// The staging directory (SPEC/SKILLS.md § Install Staging Directory)
// ---------------------------------------------------------------------------

func TestInstall_StagingRefusals(t *testing.T) {
	cases := []struct {
		setup func(h *harness) (tempDir, detail string)
		name  string
	}{
		{name: "relative", setup: func(h *harness) (string, string) {
			return "relative-tmp", `the temporary-file directory "relative-tmp" is not an absolute path`
		}},
		{name: "missing", setup: func(h *harness) (string, string) {
			dir := filepath.Join(h.root, "no-such-tmp")
			return dir, fmt.Sprintf("cannot examine %s: stat %s: no such file or directory", dir, dir)
		}},
		{name: "not a directory", setup: func(h *harness) (string, string) {
			dir := filepath.Join(h.root, "tmp-file")
			writeFile(h.t, dir, "x", 0o600)
			return dir, dir + " is not a directory"
		}},
		{name: "writable by every user without the sticky bit", setup: func(h *harness) (string, string) {
			dir := filepath.Join(h.root, "open-tmp")
			if err := os.Mkdir(dir, 0o700); err != nil {
				h.t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o777); err != nil {
				h.t.Fatal(err)
			}
			return dir, dir + " is writable by every user and carries no sticky bit"
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, goodEntries())
			requests := 0
			h.handler = func(w http.ResponseWriter, _ *http.Request) { requests++; _, _ = w.Write(h.archive) }
			dir, detail := c.setup(h)
			g, u, p := h.three()
			opts := h.options(g, u, p)
			opts.TempDir = dir
			_, err := Install(opts)
			requireErrorLine(t, err, "I/O error: cannot create a private staging directory: "+detail)
			if requests != 0 {
				t.Fatalf("%d requests were made after a staging refusal", requests)
			}
			h.requireNothingAttempted(g, u, p)
		})
	}
}

func TestInstall_StagingThatFailsItsChecksIsLeftInPlace(t *testing.T) {
	h := newHarness(t, goodEntries())
	h.ownerHook = func(fi fs.FileInfo) (uint32, bool, bool) {
		return 4242, true, strings.HasPrefix(fi.Name(), stagingPattern)
	}
	u := h.tier(ScopeUser)
	_, err := Install(h.options(u))
	entries, rerr := os.ReadDir(h.tempDir)
	if rerr != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), stagingPattern) {
		t.Fatalf("the refused staging directory is not left in place: %v %v", entries, rerr)
	}
	created := filepath.Join(h.tempDir, entries[0].Name())
	requireErrorLine(t, err, "I/O error: cannot create a private staging directory: "+created+" is not a private directory owned by the invoking user")
	requireAbsent(t, u.ConfigDir)
}

// ---------------------------------------------------------------------------
// The work directory and the cleanup warnings
// ---------------------------------------------------------------------------

func TestInstall_WorkDirectoryThatFailsItsChecksFailsTheTier(t *testing.T) {
	h := newHarness(t, goodEntries())
	first, second := h.tier(ScopeUser), h.tier(ScopeProject)
	// Only the first tier's work directory reads as foreign.
	calls := 0
	h.ownerHook = func(fi fs.FileInfo) (uint32, bool, bool) {
		if strings.HasPrefix(fi.Name(), workPattern) {
			calls++
			return 4242, true, calls == 1
		}
		return 0, false, false
	}
	res, err := Install(h.options(first, second))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	entries, _ := os.ReadDir(first.SkillsDir)
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), workPattern) {
		t.Fatalf("the refused work directory is not left in place: %v", entries)
	}
	work := filepath.Join(first.SkillsDir, entries[0].Name())
	if res.Scope != ScopeProject || len(res.Skipped) != 1 || res.Skipped[0].Reason != work+" is not a private directory owned by the invoking user" {
		t.Fatalf("result %+v", res)
	}
	requireInstalled(t, second.SkillsDir)
}

func TestInstall_RemovalRefusedIsWarnedAndDoesNotChangeTheOutcome(t *testing.T) {
	h := newHarness(t, goodEntries())
	u := h.tier(ScopeUser)
	refusal := errors.New("unlinkat: operation not permitted")
	var refusedWork string
	h.removeHook = func(path string) error {
		if strings.HasPrefix(filepath.Base(path), workPattern) {
			refusedWork = path
			return refusal
		}
		return nil
	}
	res, err := Install(h.options(u))
	if err != nil || res.Scope != ScopeUser {
		t.Fatalf("Install: %v, %+v", err, res)
	}
	want := []string{fmt.Sprintf("warning: the temporary directory %s could not be removed: %v", refusedWork, refusal)}
	if fmt.Sprint(h.warnings) != fmt.Sprint(want) {
		t.Fatalf("warnings\n got %q\nwant %q", h.warnings, want)
	}
	requireInstalled(t, u.SkillsDir)
}

// ---------------------------------------------------------------------------
// Swap, rollback and failed rollback (acceptance criterion 27)
// ---------------------------------------------------------------------------

// renameNumbers: with both skills present the swap is
// 1 roadmap-manager -> old, 2 new -> roadmap-manager,
// 3 knowledge-authority -> old, 4 new -> knowledge-authority.

func TestInstall_FailureAtTheSecondRenameRestoresAndTriesTheNextTier(t *testing.T) {
	for _, failAt := range []int{2, 3, 4} {
		t.Run("rename "+strconv.Itoa(failAt), func(t *testing.T) {
			h := newHarness(t, goodEntries())
			u, p := h.tier(ScopeUser), h.tier(ScopeProject)
			installPrevious(t, u.SkillsDir)
			injected := errors.New("injected rename failure")
			var failedPath string
			h.renameHook = func(n int, oldpath, newpath string) error {
				if n == failAt {
					if strings.Contains(oldpath, string(filepath.Separator)+"new"+string(filepath.Separator)) {
						failedPath = newpath
					} else {
						failedPath = oldpath
					}
					return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: injected}
				}
				return nil
			}
			res, err := Install(h.options(u, p))
			if err != nil {
				t.Fatalf("Install: %v", err)
			}
			requirePrevious(t, u.SkillsDir)
			if res.Scope != ScopeProject || len(res.Skipped) != 1 {
				t.Fatalf("result %+v", res)
			}
			wantPrefix := "cannot replace " + failedPath + ", and the previous skills were restored: rename "
			if !strings.HasPrefix(res.Skipped[0].Reason, wantPrefix) || !strings.HasSuffix(res.Skipped[0].Reason, injected.Error()) {
				t.Fatalf("reason %q, want prefix %q", res.Skipped[0].Reason, wantPrefix)
			}
			requireInstalled(t, p.SkillsDir)
			h.requireNoTemp(u.SkillsDir, p.SkillsDir)
		})
	}
}

func TestInstall_RollbackRestoresAbsenceWhereNoPreviousVersionExisted(t *testing.T) {
	h := newHarness(t, goodEntries())
	u, p := h.tier(ScopeUser), h.tier(ScopeProject)
	// No previous copy: the swap is 1 new -> roadmap-manager, 2 new ->
	// knowledge-authority.
	h.renameHook = func(n int, _, _ string) error {
		if n == 2 {
			return errors.New("injected")
		}
		return nil
	}
	res, err := Install(h.options(u, p))
	if err != nil || res.Scope != ScopeProject {
		t.Fatalf("Install: %v %+v", err, res)
	}
	requireAbsent(t, filepath.Join(u.SkillsDir, "roadmap-manager"), filepath.Join(u.SkillsDir, "knowledge-authority"))
	h.requireNoTemp(u.SkillsDir)
}

func TestInstall_SkillPathThatReappearsIsRolledBack(t *testing.T) {
	h := newHarness(t, goodEntries())
	u, p := h.tier(ScopeUser), h.tier(ScopeProject)
	installPrevious(t, u.SkillsDir)
	// After roadmap-manager is moved aside, something recreates it; it is gone
	// again by the time the rollback moves the previous copy back (rename 2).
	h.renameHook = func(n int, oldpath, newpath string) error {
		switch n {
		case 1:
			if err := os.Rename(oldpath, newpath); err != nil {
				return err
			}
			if err := os.Mkdir(oldpath, 0o700); err != nil {
				return err
			}
			return errRenamed
		case 2:
			return os.Remove(newpath)
		}
		return nil
	}
	res, err := Install(h.options(u, p))
	if err != nil || res.Scope != ScopeProject {
		t.Fatalf("Install: %v %+v", err, res)
	}
	path := filepath.Join(u.SkillsDir, "roadmap-manager")
	want := "cannot replace " + path + ", and the previous skills were restored: " + errReappeared.Error()
	if res.Skipped[0].Reason != want {
		t.Fatalf("reason %q, want %q", res.Skipped[0].Reason, want)
	}
	requirePrevious(t, u.SkillsDir)
	h.requireNoTemp(u.SkillsDir)
}

// A path that reappears and stays blocks the previous copy from being moved
// back, so the rollback itself fails: the previous copy is kept in the work
// directory rather than merged into whatever now occupies the skill path.
func TestInstall_SkillPathThatStaysReappearedFailsTheRollback(t *testing.T) {
	h := newHarness(t, goodEntries())
	u, p := h.tier(ScopeUser), h.tier(ScopeProject)
	installPrevious(t, u.SkillsDir)
	h.renameHook = func(n int, oldpath, newpath string) error {
		if n == 1 {
			if err := os.Rename(oldpath, newpath); err != nil {
				return err
			}
			if err := os.Mkdir(oldpath, 0o700); err != nil {
				return err
			}
			return errRenamed
		}
		return nil
	}
	_, err := Install(h.options(u, p))
	if err == nil || !strings.HasPrefix(err.Error(), "I/O error: the skills replacement failed and could not be rolled back: "+errReappeared.Error()+"; undoing it failed: ") ||
		!strings.Contains(err.Error(), "; the previous roadmap-manager is kept at ") {
		t.Fatalf("error %v", err)
	}
	requireAbsent(t, p.ConfigDir)
}

func TestInstall_FailedRollbackKeepsTheWorkDirectoryAndStops(t *testing.T) {
	h := newHarness(t, goodEntries())
	u, p := h.tier(ScopeUser), h.tier(ScopeProject)
	installPrevious(t, u.SkillsDir)
	// Rename 4 (new -> knowledge-authority) fails; the rollback then undoes
	// rename 3 (knowledge-authority restored), rename 2 (roadmap-manager back
	// into the work directory) which also fails, so the previous
	// roadmap-manager cannot be restored.
	h.renameHook = func(n int, _, _ string) error {
		switch n {
		case 4:
			return errors.New("injected swap failure")
		case 6:
			return errors.New("injected undo failure")
		}
		return nil
	}
	res, err := Install(h.options(u, p))
	if res != nil {
		t.Fatalf("a result was returned: %+v", res)
	}
	entries, _ := os.ReadDir(u.SkillsDir)
	var work string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), workPattern) {
			work = filepath.Join(u.SkillsDir, e.Name())
		}
	}
	if work == "" {
		t.Fatal("the work directory of a failed rollback was removed")
	}
	kept := filepath.Join(work, "old", "roadmap-manager")
	want := "I/O error: the skills replacement failed and could not be rolled back: injected swap failure; undoing it failed: injected undo failure; the previous roadmap-manager is kept at " + kept
	requireErrorLine(t, err, want)
	if b, rerr := os.ReadFile(filepath.Join(kept, "SKILL.md")); rerr != nil || string(b) != "previous roadmap-manager\n" {
		t.Fatalf("the kept copy does not hold the previous roadmap-manager: %q %v", b, rerr)
	}
	requireAbsent(t, p.ConfigDir) // no further tier was tried
	// The staging directory is still removed.
	if entries, _ := os.ReadDir(h.tempDir); len(entries) != 0 {
		t.Fatalf("the staging directory was left: %v", entries)
	}
}

// ---------------------------------------------------------------------------
// Interruption (acceptance criterion 29, SPEC/SKILLS.md § Install Interruption)
// ---------------------------------------------------------------------------

func requireInterrupted(t *testing.T, err error, wantRollbackErr bool) *InterruptedError {
	t.Helper()
	var intr *InterruptedError
	if !errors.As(err, &intr) {
		t.Fatalf("error %v is not an interruption", err)
	}
	if intr.Signal != syscall.SIGTERM {
		t.Fatalf("signal %v, want SIGTERM", intr.Signal)
	}
	if (intr.Err != nil) != wantRollbackErr {
		t.Fatalf("rollback error %v, want present=%v", intr.Err, wantRollbackErr)
	}
	return intr
}

func TestInstall_SignalDuringTheSwapRestoresThePreviousVersions(t *testing.T) {
	for _, at := range []int{1, 2, 3} {
		t.Run("after rename "+strconv.Itoa(at), func(t *testing.T) {
			h := newHarness(t, goodEntries())
			u, p := h.tier(ScopeUser), h.tier(ScopeProject)
			installPrevious(t, u.SkillsDir)
			h.renameHook = func(n int, _, _ string) error {
				if n == at {
					h.sigCh <- syscall.SIGTERM
				}
				return nil
			}
			res, err := Install(h.options(u, p))
			if res != nil {
				t.Fatalf("a result was returned: %+v", res)
			}
			requireInterrupted(t, err, false)
			requirePrevious(t, u.SkillsDir)
			requireAbsent(t, p.ConfigDir)
			h.requireNoTemp(u.SkillsDir)
			if h.released != 1 {
				t.Fatalf("released %d times", h.released)
			}
		})
	}
}

func TestInstall_SignalAfterBothRenamesKeepsTheInstallation(t *testing.T) {
	h := newHarness(t, goodEntries())
	u := h.tier(ScopeUser)
	installPrevious(t, u.SkillsDir)
	h.renameHook = func(n int, _, _ string) error {
		if n == 4 {
			h.sigCh <- syscall.SIGTERM
		}
		return nil
	}
	res, err := Install(h.options(u))
	if res != nil {
		t.Fatalf("a result was returned: %+v", res)
	}
	requireInterrupted(t, err, false)
	requireInstalled(t, u.SkillsDir)
	h.requireNoTemp(u.SkillsDir)
}

func TestInstall_SignalWithAFailedUndoKeepsTheWorkDirectory(t *testing.T) {
	h := newHarness(t, goodEntries())
	u := h.tier(ScopeUser)
	installPrevious(t, u.SkillsDir)
	h.renameHook = func(n int, _, _ string) error {
		switch n {
		case 2:
			h.sigCh <- syscall.SIGTERM
		case 3: // the undo of rename 2
			return errors.New("injected undo failure")
		}
		return nil
	}
	_, err := Install(h.options(u))
	intr := requireInterrupted(t, err, true)
	if !errors.Is(intr.Err, utils.ErrIO) || !strings.HasPrefix(intr.Err.Error(),
		"I/O error: the skills replacement failed and could not be rolled back: the run was interrupted by terminated; undoing it failed: injected undo failure; the previous roadmap-manager is kept at ") {
		t.Fatalf("rollback error %q", intr.Err)
	}
	found := false
	entries, _ := os.ReadDir(u.SkillsDir)
	for _, e := range entries {
		found = found || strings.HasPrefix(e.Name(), workPattern)
	}
	if !found {
		t.Fatal("the work directory of a failed undo was removed")
	}
}

func TestInstall_SignalDuringTheDownloadStopsIt(t *testing.T) {
	h := newHarness(t, goodEntries())
	started := make(chan struct{})
	h.handler = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(h.archive[:20])
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(20 * time.Second):
		}
	}
	g, u, p := h.three()
	go func() {
		<-started
		h.sigCh <- syscall.SIGTERM
	}()
	begin := time.Now()
	res, err := Install(h.options(g, u, p))
	if res != nil {
		t.Fatalf("a result was returned: %+v", res)
	}
	requireInterrupted(t, err, false)
	if elapsed := time.Since(begin); elapsed > 10*time.Second {
		t.Fatalf("the download was not stopped by the signal: %v", elapsed)
	}
	h.requireNothingAttempted(g, u, p)
}

// ---------------------------------------------------------------------------
// Output shape
// ---------------------------------------------------------------------------

func TestResult_KeyOrder(t *testing.T) {
	r := &Result{Scope: ScopeUser, Directory: "/home/alice/.claude/skills", Skills: SkillNames(), Source: SourceURL,
		Skipped: []Skipped{{Scope: ScopeGlobal, Reason: "cannot create /etc/claude-code: mkdir /etc/claude-code: permission denied"}}}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "scope": "user",
  "directory": "/home/alice/.claude/skills",
  "skills": [
    "roadmap-manager",
    "knowledge-authority"
  ],
  "source": "https://github.com/FlavioCFOliveira/Groadmap/archive/refs/heads/main.tar.gz",
  "skipped": [
    {
      "scope": "global",
      "reason": "cannot create /etc/claude-code: mkdir /etc/claude-code: permission denied"
    }
  ]
}`
	if string(b) != want {
		t.Fatalf("output\n%s\nwant\n%s", b, want)
	}
}
