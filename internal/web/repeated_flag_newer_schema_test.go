package web

import (
	"bytes"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestParseArgs_RepeatedFlagIsRefused pins SPEC/COMMANDS.md § Repeated Flags
// for `rmp web`: every one of its three flags takes one occurrence, in either
// form, the refusal names the second occurrence as written, no value of a
// repeated flag is validated, and a help token is still served.
func TestParseArgs_RepeatedFlagIsRefused(t *testing.T) {
	refused := []struct {
		args  []string
		named string
	}{
		{[]string{"--port", "8787", "--port", "9000"}, "--port"},
		{[]string{"--port=8787", "--port", "8787"}, "--port"},
		{[]string{"--host", "127.0.0.1", "--host=0.0.0.0"}, "--host"},
		{[]string{"--no-open", "--port", "9000", "--no-open"}, "--no-open"},
		// The first value is out of range, and is never examined.
		{[]string{"--port", "70000", "--port", "9000"}, "--port"},
	}
	for _, tc := range refused {
		_, help, err := parseArgs(tc.args)
		want := "invalid input: repeated flag: " + tc.named
		if help || err == nil || err.Error() != want || !errors.Is(err, utils.ErrInvalidInput) {
			t.Errorf("parseArgs(%q) = help %v, err %v; want %q (exit 2)", tc.args, help, err, want)
		}
	}

	_, help, err := parseArgs([]string{"--port", "8787", "--port", "9000", "--help"})
	if !help || err != nil {
		t.Errorf("a help token after a repeated flag: help %v, err %v; want the help", help, err)
	}
	opts, help, err := parseArgs([]string{"--host", "127.0.0.1", "--port", "9000", "--no-open"})
	if help || err != nil || opts.port != 9000 || !opts.noOpen {
		t.Errorf("each flag once: %+v, %v, %v", opts, help, err)
	}
}

// TestNewerSchema_StartupSkipsItAndRequestsFailRead pins SPEC/VERSION.md
// § Database Schema Newer Than the Binary for the web interface: the startup
// migration meets the refusal like any other open and treats the roadmap as one
// that could not be migrated, writing nothing to it, and a request that reads
// it is a read failure on its route.
func TestNewerSchema_StartupSkipsItAndRequestsFailRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const name = "future-ledger"
	if err := db.CreateRoadmapDatabase(name); err != nil {
		t.Fatal(err)
	}
	path, err := utils.GetRoadmapPath(name)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("UPDATE _metadata SET value = '9.0.0' WHERE key = 'schema_version'"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	buf := captureLog(t)
	migrateRoadmapsAtStartup()
	mustContainAll(t, oneRecord(t, buf),
		"level=WARN",
		`msg="startup schema migration skipped for roadmap"`,
		"roadmap="+name,
		"has schema version 9.0.0, newer than schema version "+db.SchemaVersion,
	)
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
		t.Errorf("the startup migration changed a database newer than the binary (%v)", err)
	}

	rec := httptest.NewRecorder()
	buildMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name, nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("reading a newer database: status %d, want 500", rec.Code)
	}
}

// TestIndex_SkipsDirectoriesWithInvalidNames pins SPEC/WEB.md § Roadmap Index
// Page: the index enumerates under the CLI's one rule, so a directory whose name
// no command could select is not offered.
func TestIndex_SkipsDirectoriesWithInvalidNames(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := db.CreateRoadmapDatabase("payments-api"); err != nil {
		t.Fatal(err)
	}
	dataDir, err := utils.GetDataDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"help", "Payments-Archive"} {
		if err := os.Mkdir(filepath.Join(dataDir, n), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, n, utils.DBFileName), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	names, err := loadRoadmapNames()
	if err != nil || strings.Join(names, ",") != "payments-api" {
		t.Errorf("the index enumerates %v (%v), want only payments-api", names, err)
	}

	rec := httptest.NewRecorder()
	buildMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "payments-api") ||
		strings.Contains(body, "Payments-Archive") || strings.Contains(body, "/roadmaps/help") {
		t.Errorf("the index page offers an invalid name (status %d)", rec.Code)
	}
}
