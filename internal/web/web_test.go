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
			t.Errorf("%v: host = %q, want 0.0.0.0", args, opts.host)
		}
		if opts.port != 9000 || !opts.portExplicit {
			t.Errorf("%v: port = %d explicit=%v, want 9000 true", args, opts.port, opts.portExplicit)
		}
		if !opts.noOpen {
			t.Errorf("%v: noOpen = false, want true", args)
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
		{"valid but missing graph", http.MethodGet, "/roadmaps/nonexistent-roadmap/graph", http.StatusNotFound},
		{"valid but missing graph data", http.MethodGet, "/roadmaps/nonexistent-roadmap/graph/data", http.StatusNotFound},
		{"post to detail is 405", http.MethodPost, "/roadmaps/some-roadmap", http.StatusMethodNotAllowed},
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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
