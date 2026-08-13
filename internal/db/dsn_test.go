package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// ==================== DSN CONSTRUCTION TESTS ====================

// TestDSNForProducesAFileURI pins the DSN shape both callers depend on: a
// file: URI, never dbPath+"?"+params. The plain form is what let a path
// containing '?' redirect the open and inject connection parameters.
func TestDSNForProducesAFileURI(t *testing.T) {
	tests := []struct {
		name     string
		readOnly bool
		wantHas  []string
		wantNot  []string
	}{
		{
			name:     "read-write",
			readOnly: false,
			wantHas:  []string{"file:///home/dev/.roadmaps/platform/project.db?", "foreign_keys", "busy_timeout"},
			wantNot:  []string{"query_only"},
		},
		{
			name:     "read-only",
			readOnly: true,
			wantHas:  []string{"file:///home/dev/.roadmaps/platform/project.db?", "foreign_keys", "busy_timeout", "query_only"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := dsnFor("/home/dev/.roadmaps/platform/project.db", tt.readOnly)

			if !strings.HasPrefix(dsn, "file:") {
				t.Fatalf("dsnFor(...) = %q, want a file: URI", dsn)
			}
			for _, want := range tt.wantHas {
				if !strings.Contains(dsn, want) {
					t.Errorf("dsnFor(...) = %q, want it to contain %q", dsn, want)
				}
			}
			for _, unwanted := range tt.wantNot {
				if strings.Contains(dsn, unwanted) {
					t.Errorf("dsnFor(...) = %q, want it NOT to contain %q", dsn, unwanted)
				}
			}
		})
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
