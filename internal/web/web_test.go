package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

func TestParseArgs_Defaults(t *testing.T) {
	opts, showHelp, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if showHelp {
		t.Fatalf("showHelp = true, want false")
	}
	if opts.host != defaultHost {
		t.Errorf("host = %q, want %q", opts.host, defaultHost)
	}
	// With no --host the resolved default binds loopback (127.0.0.1), reachable
	// only from the local machine; exposing it on the network is the explicit
	// --host 0.0.0.0 opt-in (SPEC/WEB.md § Bind Address and Port Selection,
	// item 1). Asserting the literal here guards the default value itself, not
	// just that it equals the constant.
	if opts.host != "127.0.0.1" {
		t.Errorf("default host = %q, want 127.0.0.1 (loopback)", opts.host)
	}
	if opts.port != defaultPort {
		t.Errorf("port = %d, want %d", opts.port, defaultPort)
	}
	if opts.portExplicit {
		t.Errorf("portExplicit = true, want false (no --port given)")
	}
	if opts.noOpen {
		t.Errorf("noOpen = true, want false")
	}
}

func TestParseArgs_HelpTokens(t *testing.T) {
	for _, tok := range []string{"-h", "--help", "help"} {
		_, showHelp, err := parseArgs([]string{tok})
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tok, err)
		}
		if !showHelp {
			t.Errorf("%q: showHelp = false, want true", tok)
		}
	}
}

// TestParseArgs_HostAndPort exercises the host/port override path. It passes
// --host 0.0.0.0, the documented network-exposure opt-in that overrides the
// loopback default (127.0.0.1) to bind all interfaces (SPEC/WEB.md § Bind
// Address and Port Selection, item 2). Both the `--flag value` and
// `--flag=value` forms are covered.
func TestParseArgs_HostAndPort(t *testing.T) {
	cases := [][]string{
		{"--host", "0.0.0.0", "--port", "9000", "--no-open"},
		{"--host=0.0.0.0", "--port=9000", "--no-open"},
	}
	for _, args := range cases {
		opts, showHelp, err := parseArgs(args)
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", args, err)
		}
		if showHelp {
			t.Fatalf("%v: showHelp = true", args)
		}
		if opts.host != "0.0.0.0" {
			t.Errorf("%v: host = %q, want 0.0.0.0 (exposure opt-in)", args, opts.host)
		}
		if opts.port != 9000 || !opts.portExplicit {
			t.Errorf("%v: port = %d explicit=%v, want 9000 true", args, opts.port, opts.portExplicit)
		}
		if !opts.noOpen {
			t.Errorf("%v: noOpen = false, want true", args)
		}
	}
}

// TestIsLoopbackHost verifies the loopback classification that decides whether
// the network-exposure warning is printed (SPEC/WEB.md § Bind Address and Port
// Selection, item 3). Loopback addresses (incl. the whole 127.0.0.0/8 range,
// ::1, and the "localhost" literal) print no warning; 0.0.0.0, routable IPs,
// and bare hostnames are treated as network-exposed.
func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"::1", true},
		{"localhost", true},
		{"0.0.0.0", false},
		{"192.168.1.10", false},
		{"::", false},
		{"example.com", false},
	}
	for _, c := range cases {
		if got := isLoopbackHost(c.host); got != c.want {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestParseArgs_PortZeroIsExplicit(t *testing.T) {
	opts, _, err := parseArgs([]string{"--port", "0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.port != 0 || !opts.portExplicit {
		t.Errorf("port=%d explicit=%v, want 0 true", opts.port, opts.portExplicit)
	}
}

func TestParseArgs_PortErrors(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr error
	}{
		{"non-integer", []string{"--port", "notanumber"}, utils.ErrValidation},
		{"too high", []string{"--port", "70000"}, utils.ErrValidation},
		{"negative", []string{"--port", "-5"}, utils.ErrValidation},
		{"missing value", []string{"--port"}, utils.ErrRequired},
		{"host missing value", []string{"--host"}, utils.ErrRequired},
		{"unknown flag", []string{"--foo"}, utils.ErrInvalidInput},
		{"unexpected positional", []string{"roadmap"}, utils.ErrInvalidInput},
		{"no-open with value", []string{"--no-open=yes"}, utils.ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseArgs(tc.args)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want wrapping %v", err, tc.wantErr)
			}
		})
	}
}

// TestRoutes_NameGuard verifies the {name} path guard rejects invalid and
// non-existent roadmap names with 404, and that a non-read method on a known
// path yields 405. These tests use a temporary HOME so no real roadmap
// exists, exercising the validation and not-found paths without touching the
// developer's data directory.
func TestRoutes_NameGuard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mux := buildMux()

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"invalid name traversal", http.MethodGet, "/roadmaps/..%2fetc", http.StatusNotFound},
		{"invalid name uppercase", http.MethodGet, "/roadmaps/NotValid", http.StatusNotFound},
		{"valid but missing", http.MethodGet, "/roadmaps/nonexistent-roadmap", http.StatusNotFound},
		{"invalid name on tasks", http.MethodGet, "/roadmaps/NotValid/tasks", http.StatusNotFound},
		{"valid but missing tasks", http.MethodGet, "/roadmaps/nonexistent-roadmap/tasks", http.StatusNotFound},
		{"valid but missing graph", http.MethodGet, "/roadmaps/nonexistent-roadmap/graph", http.StatusNotFound},
		{"valid but missing graph data", http.MethodGet, "/roadmaps/nonexistent-roadmap/graph/data", http.StatusNotFound},
		{"post to sprints is 405", http.MethodPost, "/roadmaps/some-roadmap", http.StatusMethodNotAllowed},
		{"post to tasks is 405", http.MethodPost, "/roadmaps/some-roadmap/tasks", http.StatusMethodNotAllowed},
		{"put to tasks is 405", http.MethodPut, "/roadmaps/some-roadmap/tasks", http.StatusMethodNotAllowed},
		{"post to index is 405", http.MethodPost, "/", http.StatusMethodNotAllowed},
		{"delete to graph is 405", http.MethodDelete, "/roadmaps/some-roadmap/graph", http.StatusMethodNotAllowed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("%s %s: status = %d, want %d", tc.method, tc.path, rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestStatic_MissingAssetIs404 confirms the embedded static handler returns
// 404 for an asset not in the embedded set and 200 for one that is.
func TestStatic_MissingAssetIs404(t *testing.T) {
	mux := buildMux()

	missing := httptest.NewRequest(http.MethodGet, "/static/does-not-exist.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, missing)
	if rec.Code != http.StatusNotFound {
		t.Errorf("missing static asset: status = %d, want 404", rec.Code)
	}

	present := httptest.NewRequest(http.MethodGet, "/static/style.css", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, present)
	if rec.Code != http.StatusOK {
		t.Errorf("present static asset: status = %d, want 200", rec.Code)
	}

	// A non-read method on a static path must be rejected (405).
	post := httptest.NewRequest(http.MethodPost, "/static/style.css", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST to static: status = %d, want 405", rec.Code)
	}
}

// TestIndex_EmptyState confirms the index renders 200 with an empty state
// when no roadmaps exist.
func TestIndex_EmptyState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mux := buildMux()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeHTML {
		t.Errorf("content-type = %q, want %q", ct, contentTypeHTML)
	}
	body := rec.Body.String()
	if !contains(body, "No roadmaps found") {
		t.Errorf("empty-state message not found in body")
	}
}

// TestEmbeddedCSS_HiddenAttributeWins guards against a graph-rendering
// regression: the `hidden` attribute must override component display rules.
// A class rule such as `.graph-empty { display: flex }` otherwise beats the
// user-agent `[hidden] { display: none }` on specificity, leaving the
// empty-graph overlay painted over a populated graph and hiding it
// (SPEC/WEB.md § Empty graph). The global `[hidden] { display: none
// !important }` in style.css restores the attribute's semantics.
func TestEmbeddedCSS_HiddenAttributeWins(t *testing.T) {
	css, err := staticFS.ReadFile("static/style.css")
	if err != nil {
		t.Fatalf("reading embedded style.css: %v", err)
	}
	if !contains(stripSpace(string(css)), "[hidden]{display:none!important") {
		t.Errorf("style.css must contain a global `[hidden] { display: none !important }` rule so the hidden attribute overrides component display rules (e.g. .graph-empty); not found")
	}
}

// stripSpace removes all ASCII whitespace so a CSS-rule assertion is
// insensitive to source formatting.
func stripSpace(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			// skip whitespace
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
