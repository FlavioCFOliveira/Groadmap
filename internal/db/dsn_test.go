package db

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// ==================== DSN CONSTRUCTION TESTS ====================

// TestDSNForProducesAFileURI pins the whole DSN contract both callers depend
// on: a file: URI rather than dbPath+"?"+params, and the exact set of query
// parameters. The parameters are compared as a parsed key/value set, not by
// substring, because the key names overlap -- "_busy_timeout=" contains
// "_timeout=", so a substring check could not tell a primary key from its alias.
func TestDSNForProducesAFileURI(t *testing.T) {
	const dbPath = "/home/dev/.roadmaps/platform/project.db"

	tests := []struct {
		name     string
		readOnly bool
		want     map[string]string
	}{
		{
			name:     "read-write",
			readOnly: false,
			want: map[string]string{
				"_busy_timeout": "10000",
				"_foreign_keys": "1",
			},
		},
		{
			name:     "read-only",
			readOnly: true,
			want: map[string]string{
				"_busy_timeout": "10000",
				"_foreign_keys": "1",
				"_query_only":   "1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := dsnFor(dbPath, tt.readOnly)

			if !strings.HasPrefix(dsn, "file://") {
				t.Fatalf("dsnFor(...) = %q, want a file: URI with an empty authority", dsn)
			}

			parsed, err := url.Parse(dsn)
			if err != nil {
				t.Fatalf("dsnFor(...) = %q, which does not parse as a URI: %v", dsn, err)
			}
			if parsed.Path != dbPath {
				t.Errorf("DSN path = %q, want %q", parsed.Path, dbPath)
			}

			query, err := url.ParseQuery(parsed.RawQuery)
			if err != nil {
				t.Fatalf("DSN query %q does not parse: %v", parsed.RawQuery, err)
			}
			if len(query) != len(tt.want) {
				t.Errorf("DSN carries %d parameters (%v), want exactly %d (%v)", len(query), query, len(tt.want), tt.want)
			}
			for key, want := range tt.want {
				if got := query.Get(key); got != want {
					t.Errorf("DSN parameter %s = %q, want %q", key, got, want)
				}
			}

			// _pragma is the driver's execute-verbatim form: unvalidated, and
			// the one parameter class that can still fail partway through a
			// DSN. _fk and _timeout are aliases that win over the primary keys
			// when both are supplied, so carrying both would be a trap.
			for _, forbidden := range []string{"_pragma", "_fk", "_timeout", "_journal_mode", "_journal"} {
				if _, present := query[forbidden]; present {
					t.Errorf("DSN carries %s=%q; it must not", forbidden, query.Get(forbidden))
				}
			}
		})
	}
}

// TestDSNRejectsAnInvalidValueOutright pins the behaviour the shorthand keys are
// used for. The driver validates them against a fixed accepted set before
// applying any parameter, so a bad value fails the connection instead of leaving
// it half-configured. The verbatim _pragma form this replaced offered no such
// guarantee.
func TestDSNRejectsAnInvalidValueOutright(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "project.db")
	base := "file:" + uriPath(dbPath)

	// Positive control: the same DSN without the bad key must open, so a
	// failure below is attributable to the value and not to the path.
	good, err := sql.Open("sqlite", base+"?_foreign_keys=1&_busy_timeout=10000")
	if err != nil {
		t.Fatalf("opening the control database: %v", err)
	}
	defer good.Close()
	if err := good.Ping(); err != nil {
		t.Fatalf("control database does not connect: %v", err)
	}

	for _, bad := range []string{
		"_foreign_keys=yes_please",
		"_busy_timeout=10s",
		"_query_only=perhaps",
	} {
		t.Run(bad, func(t *testing.T) {
			database, err := sql.Open("sqlite", base+"?"+bad)
			if err != nil {
				return // rejected at construction, which is also acceptable
			}
			defer database.Close()
			if err := database.Ping(); err == nil {
				t.Errorf("a DSN carrying %s connected; the driver must reject the value", bad)
			}
		})
	}
}

// TestOpenLeavesJournalModeToTheDatabaseLevel guards the split the SPEC draws
// between connection-scoped PRAGMAs, which travel in the DSN, and
// database-level ones, which do not. journal_mode is recorded in the file
// header and applies to every connection, so it is set once by
// configureConnection; carrying it in the DSN would re-apply it on every
// connection for no gain.
func TestOpenLeavesJournalModeToTheDatabaseLevel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	database, err := Open("storage-layer")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	var mode string
	if err := database.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("reading journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	if query := dsnFor("/tmp/project.db", false); strings.Contains(query, "journal") {
		t.Errorf("the DSN carries a journal_mode parameter (%q); it is database-level", query)
	}
}

// TestURIPathEncodesHostileCharacters checks the path component against
// https://www.sqlite.org/uri.html. Every character that could terminate the
// path or introduce a parameter must come out percent-encoded.
func TestURIPathEncodesHostileCharacters(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "ordinary absolute path",
			path: "/home/dev/.roadmaps/platform/project.db",
			want: "///home/dev/.roadmaps/platform/project.db",
		},
		{
			name: "question mark is encoded, not left to split the DSN",
			path: "/home/de?v/project.db",
			want: "///home/de%3Fv/project.db",
		},
		{
			name: "hash is encoded so it cannot start a fragment",
			path: "/home/de#v/project.db",
			want: "///home/de%23v/project.db",
		},
		{
			name: "percent is encoded so it cannot start an escape",
			path: "/home/de%2Fv/project.db",
			want: "///home/de%252Fv/project.db",
		},
		{
			name: "space is encoded",
			path: "/home/my roadmaps/project.db",
			want: "///home/my%20roadmaps/project.db",
		},
		{
			name: "drive letter gets a leading slash",
			path: "C:/Users/dev/.roadmaps/platform/project.db",
			want: "///C:/Users/dev/.roadmaps/platform/project.db",
		},
		{
			name: "relative path keeps the opaque form, no empty authority",
			path: "roadmaps/project.db",
			want: "roadmaps/project.db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uriPath(tt.path); got != tt.want {
				t.Errorf("uriPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestOpenWithHostilePathOpensTheIntendedFile is the end-to-end regression for
// the DSN defect. Each home directory name below carries a character that the
// old string-concatenated DSN either truncated the path at or handed to the
// driver's parameter parser. The assertion is on the file SQLite actually
// opened, read back from the engine rather than inferred.
func TestOpenWithHostilePathOpensTheIntendedFile(t *testing.T) {
	homes := []struct {
		name string
		dir  string
	}{
		{"question mark", "ho?me"},
		{"hash", "ho#me"},
		{"percent", "ho%me"},
		{"ampersand", "ho&me"},
		{"space", "ho me"},
		{"equals sign", "ho=me"},
	}

	for _, h := range homes {
		t.Run(h.name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), h.dir)
			t.Setenv("HOME", home)

			const roadmap = "storage-layer"
			database, err := Open(roadmap)
			if err != nil {
				t.Fatalf("Open(%q) under home %q: %v", roadmap, home, err)
			}
			defer database.Close()

			var opened string
			if err := database.QueryRow("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&opened); err != nil {
				t.Fatalf("reading the opened filename: %v", err)
			}

			// SQLite reports the path it resolved, so both sides are resolved
			// before comparing: the temporary directory sits under a symlinked
			// root on some platforms (/var -> /private/var on macOS).
			want := filepath.Join(home, ".roadmaps", roadmap, "project.db")
			if resolve(t, opened) != resolve(t, want) {
				t.Errorf("opened %q, want %q", opened, want)
			}
		})
	}
}

// resolve expands every symbolic link in path so two spellings of the same file
// compare equal. A path that does not exist is returned unchanged, which keeps a
// mismatch reported as the mismatch it is rather than as a stat failure.
func resolve(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

// TestOpenWithParameterInjectingPathKeepsItsPragmas is the security half of the
// same regression. The home directory spells out driver parameters that would
// disable referential integrity and durability. Before the file: URI they were
// inert only because the driver did not yet recognise those keys; since driver
// v1.55.0 it does, so the path must not be able to reach the parameter parser
// at all.
func TestOpenWithParameterInjectingPathKeepsItsPragmas(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home?_foreign_keys=0&_synchronous=off&_query_only=1")
	t.Setenv("HOME", home)

	database, err := Open("storage-layer")
	if err != nil {
		t.Fatalf("Open under an injecting home path: %v", err)
	}
	defer database.Close()

	var foreignKeys, busyTimeout, synchronous, queryOnly int
	for _, probe := range []struct {
		pragma string
		into   *int
	}{
		{"foreign_keys", &foreignKeys},
		{"busy_timeout", &busyTimeout},
		{"synchronous", &synchronous},
		{"query_only", &queryOnly},
	} {
		if err := database.QueryRow("PRAGMA " + probe.pragma).Scan(probe.into); err != nil {
			t.Fatalf("reading PRAGMA %s: %v", probe.pragma, err)
		}
	}

	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1: the path disabled referential integrity", foreignKeys)
	}
	if busyTimeout != DefaultBusyTimeout {
		t.Errorf("busy_timeout = %d, want %d", busyTimeout, DefaultBusyTimeout)
	}
	if synchronous == 0 {
		t.Error("synchronous = 0 (OFF): the path downgraded durability")
	}
	if queryOnly != 0 {
		t.Errorf("query_only = %d, want 0: the path made a read-write connection read-only", queryOnly)
	}
}
