// Package commands — fixtures shared by the task and sprint comment suites.
//
// The two families are served by one implementation (comments.go), so the
// behaviours their suites assert are the same behaviours read against two
// entities: the standard-input contract, the audit entry, the id arithmetic. The
// fixtures those assertions need therefore live here once, and each family's file
// holds only what is specific to it — its roadmap seed, its readers, and the
// per-family expectations.
package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// withStdin points os.Stdin at a real file holding content for the duration of
// fn and returns whatever was left unread afterwards. The leftover is the
// evidence for the precedence rules: a command that must NOT read standard input
// leaves the content intact, and one that reads it to EOF leaves nothing.
func withStdin(t *testing.T, content string, fn func()) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the stdin fixture: %v", err)
	}
	f, err := os.Open(path) // #nosec G304 -- path is this test's own TempDir file
	if err != nil {
		t.Fatalf("opening the stdin fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	restore := os.Stdin
	os.Stdin = f
	fn()
	os.Stdin = restore

	leftover, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("reading back the stdin fixture: %v", err)
	}
	return string(leftover)
}

// countCommentAudit counts the audit rows for one operation against one entity.
// Every audit assertion in both suites is phrased through it, because the contract
// it measures is the same on both sides: the operation, the entity TYPE and the
// entity ID must all name the PARENT of the comment, never the comment itself.
func countCommentAudit(t *testing.T, database *db.DB, op models.AuditOperation,
	entityType models.EntityType, entityID int) int {
	t.Helper()

	operation := string(op)
	entity := string(entityType)
	entries, err := database.GetAuditEntries(context.Background(), &db.AuditFilter{
		Operation:  &operation,
		EntityType: &entity,
		EntityID:   &entityID,
	})
	if err != nil {
		t.Fatalf("querying %s audit entries: %v", op, err)
	}
	return len(entries)
}

// itoa keeps the argument lists readable without pulling strconv into every call.
func itoa(n int) string {
	return strconv.Itoa(n)
}
