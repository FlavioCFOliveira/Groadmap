package graphserve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkpointSectionHeading is the heading line of the SPEC/GRAPH.md section
// that states where a checkpoint fold runs.
const checkpointSectionHeading = "### Synchronous Checkpoint on Write"

// TestSpecCheckpointFoldRunsOnTheEngineGoroutine holds SPEC/GRAPH.md
// § Synchronous Checkpoint on Write to the code fact it describes: the server
// never runs a fold itself. foldGate.fold hands it to the engine's checkpointer
// through TriggerCtx, the checkpointer runs it on its own goroutine, and the
// caller waits for the outcome while writers keep committing. The section once
// claimed the opposite — that a checkpoint ran "never as a background goroutine
// racing either" — and rmp task #595 removed that claim. This test fails if it
// returns, and fails if the statement that replaced it is dropped.
//
// Both phrases are matched with case folded and every run of whitespace
// collapsed to one space, so re-wrapping the paragraph cannot hide either.
func TestSpecCheckpointFoldRunsOnTheEngineGoroutine(t *testing.T) {
	section := normaliseSpecProse(specGraphSection(t, checkpointSectionHeading))

	// The withdrawn claim: a fold never runs as a background goroutine.
	const forbidden = "never as a background goroutine"
	if strings.Contains(section, forbidden) {
		t.Errorf("SPEC/GRAPH.md %s states a checkpoint runs %q; the fold runs on the "+
			"engine's checkpoint goroutine and the server only triggers it and waits "+
			"(foldGate.fold -> TriggerCtx; rmp task #595)",
			checkpointSectionHeading, forbidden)
	}

	// The contract that replaced it: the requester hands the fold to the
	// engine's checkpoint routine, which runs it on its own goroutine, and the
	// requester waits for the fold's outcome.
	const required = "hands the fold to the engine's checkpoint routine, which runs it " +
		"on a goroutine of its own, and waits until the fold has finished"
	if !strings.Contains(section, required) {
		t.Errorf("SPEC/GRAPH.md %s no longer states that the requester %q; that is "+
			"what foldGate.fold does (it calls TriggerCtx and waits; rmp task #595)",
			checkpointSectionHeading, required)
	}
}

// specGraphSection returns the body of the SPEC/GRAPH.md section whose heading
// line is heading, up to the next heading of level two or three. The heading is
// matched as a whole line, so a cross-reference cannot redirect the scan.
func specGraphSection(t *testing.T, heading string) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s; this test assumes internal/graphserve sits two levels below the module root: %v", root, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "SPEC", "GRAPH.md"))
	if err != nil {
		t.Fatalf("reading SPEC/GRAPH.md: %v", err)
	}
	spec := string(raw)

	anchor := "\n" + heading + "\n"
	start := strings.Index(spec, anchor)
	if start < 0 {
		t.Fatalf("SPEC/GRAPH.md has no %q section; this test cannot verify itself", heading)
	}
	body := spec[start+len(anchor):]
	for _, next := range []string{"\n## ", "\n### "} {
		if end := strings.Index(body, next); end >= 0 {
			body = body[:end]
		}
	}
	return body
}

// normaliseSpecProse folds s to lower case and collapses every run of
// whitespace, line breaks included, to a single space.
func normaliseSpecProse(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}
