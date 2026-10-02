//go:build unix

package db

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestOpen_SymlinkedDatabaseFilesAreRefused pins SPEC/ARCHITECTURE.md
// § Directory Structure, location rule 10, for the database file and its
// companions: when project.db, project.db-wal, project.db-shm or
// project.db-journal is a symbolic link, dangling or not, every open refuses
// with the published line naming the link, before any mode is read or changed
// and before any connection exists. The target is never opened, written,
// created or chmod-ed, and the link is left as it was.
//
// Unix-only: creating a symbolic link on Windows needs a privilege the test
// process does not have.
func TestOpen_SymlinkedDatabaseFilesAreRefused(t *testing.T) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		for _, dangling := range []bool{false, true} {
			label := "project.db" + suffix
			if dangling {
				label += " dangling"
			}
			t.Run(label, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				const name = "linked-ledger"
				if err := CreateRoadmapDatabase(name); err != nil {
					t.Fatal(err)
				}
				dbPath, err := utils.GetRoadmapPath(name)
				if err != nil {
					t.Fatal(err)
				}
				link := dbPath + suffix
				if suffix == "" {
					if err := os.Remove(dbPath); err != nil {
						t.Fatal(err)
					}
				}

				outside := filepath.Join(t.TempDir(), "outside-target")
				content := []byte("payments ledger kept outside the data directory")
				if !dangling {
					if err := os.WriteFile(outside, content, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(outside, link); err != nil {
					t.Fatal(err)
				}

				for _, open := range []struct {
					name string
					fn   func(string) (*DB, error)
				}{{"Open", Open}, {"OpenExisting", OpenExisting}, {"OpenReadOnly", OpenReadOnly}} {
					database, err := open.fn(name)
					if err == nil {
						_ = database.Close()
						t.Fatalf("%s followed a symbolic link at %s", open.name, link)
					}
					if suffix == "" && dangling && open.name != "Open" {
						// A dangling project.db is no database at all to the
						// existence check these two run first: the roadmap does
						// not exist, and nothing is created.
						if !errors.Is(err, utils.ErrNotFound) {
							t.Errorf("%s: %v, want the roadmap-not-found refusal", open.name, err)
						}
						continue
					}
					want := "database error: " + link + " is a symbolic link; refusing to use it as a roadmap database file"
					if err.Error() != want || !errors.Is(err, utils.ErrDatabase) {
						t.Errorf("%s: %q, want %q (exit 1)", open.name, err.Error(), want)
					}
				}

				if dangling {
					if _, err := os.Lstat(outside); !os.IsNotExist(err) {
						t.Errorf("the dangling link's target was created: %v", err)
					}
				} else {
					info, err := os.Stat(outside)
					if err != nil || info.Mode().Perm() != 0o644 {
						t.Errorf("the target's mode changed: %v %v", info.Mode(), err)
					}
					if data, _ := os.ReadFile(outside); !bytes.Equal(data, content) {
						t.Errorf("the target's content changed")
					}
				}
				if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Errorf("the link was not left in place: %v", err)
				}
			})
		}
	}
}

// TestCreateRoadmapDatabase_UnderANarrowingUmask is the regression test for the
// temporary database `roadmap create` builds: os.CreateTemp asks for 0600 and a
// umask such as 0277 narrows it to 0400, which left SQLite unable to write the
// database it was building ("attempt to write a readonly database"). The file
// is brought to exactly 0600 before it is used, and the published project.db
// is 0600 whatever the umask.
func TestCreateRoadmapDatabase_UnderANarrowingUmask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	old := syscall.Umask(0o277)
	err := CreateRoadmapDatabase("narrow-umask")
	syscall.Umask(old)
	if err != nil {
		t.Fatalf("creating a roadmap under umask 0277: %v", err)
	}
	path, _ := utils.GetRoadmapPath("narrow-umask")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != utils.DBFilePerm {
		t.Fatalf("project.db mode %v (%v), want 0600", info.Mode().Perm(), err)
	}
	database, err := OpenExisting("narrow-umask")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if v, err := database.GetSchemaVersion(); err != nil || v != SchemaVersion {
		t.Errorf("schema version %q (%v)", v, err)
	}
}
