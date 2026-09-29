package utils_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// indentingEncoder is the encoding PrintJSON produced before it became single
// pass, and the reference its bytes are held to (SPEC/IMPLEMENTATION.md
// § Performance Considerations, item 6): encoding/json's Encoder with HTML
// escaping off and a two-space indent, which encodes the value and then
// re-indents the encoded bytes.
func indentingEncoder(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func ptr[T any](v T) *T { return &v }

// jsonPayloads are the representative command results the golden test runs:
// every field kind the models carry (pointers set and nil, nil and empty
// slices, maps, floats, nested structs) with realistic values, plus the string
// edge cases the SPEC names.
func jsonPayloads() map[string]any {
	task := models.Task{
		ParentTaskID:           ptr(118),
		CompletionSummary:      ptr("Replaced the per-card comment reads with one grouped count; <5 ms> on 4,000 cards & counting."),
		CommitOpen:             ptr("4c7153ef489a76d17a1ad4e67c8424350e6feb85"),
		CommitClose:            nil,
		TestedAt:               ptr("2026-09-28T16:04:11.512Z"),
		ClosedAt:               nil,
		StartedAt:              ptr("2026-09-27T09:12:00.000Z"),
		AcceptanceCriteria:     "- The sprint page issues **one** count query\n- HTML is byte-identical\u2028(line separator)",
		CreatedAt:              "2026-09-20T08:00:00.000Z",
		Status:                 models.StatusTesting,
		TechnicalRequirements:  "Use `task_id IN (SELECT task_id FROM sprint_tasks WHERE sprint_id = ?)`; tab\there.",
		FunctionalRequirements: "Show the comment count on each card \u2029 without reading bodies. Quote: \"ok\" \\ backslash",
		Type:                   models.TypeTask,
		Title:                  "Count sprint comments by sprint id — naïve 日本語 ✓",
		DependsOn:              []int{101, 102, 117},
		Blocks:                 []int{},
		ID:                     546,
		Priority:               8,
		Severity:               3,
		SubtaskCount:           0,
	}
	bare := models.Task{ID: 1, Title: "Seed roadmap", Status: models.StatusBacklog, Type: models.TypeTask}

	sprint := models.Sprint{
		StartedAt:   ptr("2026-09-21T07:30:00.000Z"),
		ClosedAt:    nil,
		MaxTasks:    ptr(40),
		Title:       "Sprint 50 — response time",
		Description: "Cut the fixed per-invocation cost: <chroma> & JSON.",
		CreatedAt:   "2026-09-20T07:00:00.000Z",
		Status:      models.SprintOpen,
		Tasks:       []int{536, 537, 544, 545, 546},
		ID:          50,
		TaskCount:   5,
		Order:       50,
	}
	emptySprint := models.Sprint{ID: 51, Title: "Sprint 51", Tasks: []int{}, Status: models.SprintPending, Order: 51}

	stats := models.RoadmapStats{
		Roadmap:         "groadmap",
		Sprints:         models.SprintStatsSummary{Current: ptr(50), Total: 51, Completed: 49, Pending: 1},
		Tasks:           models.TaskStatsSummary{Backlog: 12, Sprint: 3, Doing: 1, Testing: 1, Completed: 529},
		AverageVelocity: 1.4285714285714286,
	}
	sprintStats := models.SprintStats{
		StatusDistribution: map[string]int{"TESTING": 1, "COMPLETED": 2, "DOING": 1, "SPRINT": 1},
		DaysElapsed:        ptr(8),
		DaysRemaining:      nil,
		TaskOrder:          []int{536, 537, 544, 545, 546},
		Burndown:           []models.BurndownEntry{{Date: "2026-09-21", TasksRemaining: 5}, {Date: "2026-09-28", TasksRemaining: 3}},
		SprintID:           50,
		TotalTasks:         5,
		CompletedTasks:     2,
		ProgressPercentage: 40,
		Velocity:           0.25,
	}
	auditStats := models.AuditStats{
		ByOperation:  map[string]int{},
		ByEntityType: nil,
		FirstEntryAt: nil,
		LastEntryAt:  ptr("2026-09-29T15:01:32.000Z"),
		TotalEntries: 0,
	}

	return map[string]any{
		"task":              task,
		"task pointer":      &task,
		"bare task":         bare,
		"task list":         []models.Task{task, bare},
		"empty task list":   []models.Task{},
		"nil task list":     []models.Task(nil),
		"sprint":            sprint,
		"empty sprint":      emptySprint,
		"sprint list":       []models.Sprint{sprint, emptySprint},
		"roadmap stats":     stats,
		"sprint stats":      sprintStats,
		"audit stats":       auditStats,
		"comment":           models.TaskComment{Type: "DECISION", Body: "Adopt the <sub-select> & bind one ?", CreatedAt: "2026-09-29T10:00:00.000Z", ID: 9, TaskID: 546},
		"created id":        map[string]int{"id": 546},
		"reorder result":    map[string]any{"sprint_id": 50, "task_order": []int{546, 545}, "moved": true, "note": nil},
		"graph rows":        map[string]any{"columns": []string{"n.key"}, "rows": []any{[]any{"a"}, []any{}, map[string]any{}}, "counters": map[string]any{"nodesCreated": 1, "ratio": 0.1}},
		"floats":            []float64{0, math.Copysign(0, -1), 0.1, 1e20, 1e21, 1.5e-7, 123456789.125, math.MaxFloat64, math.SmallestNonzeroFloat64},
		"integers":          []int64{0, -1, math.MaxInt64, math.MinInt64},
		"html characters":   "<script>alert('x')</script> & Tom & Jerry > less",
		"line separators":   "a\u2028b\u2029c",
		"invalid utf-8":     "valid\xffinvalid\xc3(\xed\xa0\x80)end",
		"control chars":     "nul\x00bell\x07esc\x1bdel\x7f",
		"empty containers":  map[string]any{"a": []int{}, "o": map[string]int{}, "s": "", "n": nil, "z": struct{}{}},
		"nested empty":      []any{[]any{[]any{}}, map[string]any{"k": []any{map[string]any{}}}},
		"invalid utf-8 key": map[string]int{"k\xffey": 1},
		"top-level string":  "plain",
		"top-level number":  42,
		"top-level null":    nil,
	}
}

// TestPrintJSON_ByteIdenticalToIndentingEncoder is the golden test of
// SPEC/IMPLEMENTATION.md § Performance Considerations, item 6: for every
// representative command result and edge case, the single-pass encoding writes
// exactly the bytes the encode-then-indent encoding wrote.
func TestPrintJSON_ByteIdenticalToIndentingEncoder(t *testing.T) {
	for name, v := range jsonPayloads() {
		want, err := indentingEncoder(v)
		if err != nil {
			t.Fatalf("%s: reference encoding: %v", name, err)
		}
		var got bytes.Buffer
		if err := utils.WriteJSON(&got, v); err != nil {
			t.Fatalf("%s: WriteJSON: %v", name, err)
		}
		if !bytes.Equal(got.Bytes(), want) {
			t.Errorf("%s: the single-pass output differs from the indenting encoder's\n got: %q\nwant: %q",
				name, got.Bytes(), want)
		}
	}
}

// TestPrintJSON_Properties pins each property the SPEC names on literal bytes, so
// the golden test cannot pass by the reference drifting with the new encoding.
func TestPrintJSON_Properties(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    any
		want string
	}{
		{"two-space indent, no prefix, trailing newline",
			map[string]any{"a": map[string]int{"b": 1}, "c": []int{2, 3}},
			"{\n  \"a\": {\n    \"b\": 1\n  },\n  \"c\": [\n    2,\n    3\n  ]\n}\n"},
		{"<, > and & written literally", "<a href=\"x&y\">", "\"<a href=\\\"x&y\\\">\"\n"},
		{"U+2028 and U+2029 escaped", "a\u2028b\u2029c", "\"a\\u2028b\\u2029c\"\n"},
		{"invalid UTF-8 replaced by U+FFFD", "a\xffb", "\"a\ufffdb\"\n"},
		{"empty array", map[string][]int{"tasks": {}}, "{\n  \"tasks\": []\n}\n"},
		{"empty object", map[string]map[string]int{"by": {}}, "{\n  \"by\": {}\n}\n"},
		{"nil slice is null", map[string][]int{"tasks": nil}, "{\n  \"tasks\": null\n}\n"},
	} {
		var got bytes.Buffer
		if err := utils.WriteJSON(&got, tc.v); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.String() != tc.want {
			t.Errorf("%s:\n got: %q\nwant: %q", tc.name, got.String(), tc.want)
		}
	}
}

// TestPrintJSON_ErrorsMatchAndWriteNothing: a value that cannot be encoded fails
// with the error the indenting encoder reported, and nothing is written.
func TestPrintJSON_ErrorsMatchAndWriteNothing(t *testing.T) {
	type cycle struct {
		Next *cycle `json:"next"`
	}
	loop := &cycle{}
	loop.Next = loop
	for name, v := range map[string]any{
		"channel":          make(chan int),
		"function":         map[string]any{"f": func() {}},
		"NaN":              []float64{1, math.NaN()},
		"infinity":         map[string]float64{"x": math.Inf(1)},
		"cycle":            loop,
		"unsupported deep": models.SprintStats{StatusDistribution: map[string]int{}, Velocity: math.Inf(-1)},
	} {
		_, wantErr := indentingEncoder(v)
		if wantErr == nil {
			t.Fatalf("%s: the reference encoder accepted the value", name)
		}
		var out bytes.Buffer
		err := utils.WriteJSON(&out, v)
		if err == nil {
			t.Errorf("%s: WriteJSON accepted a value the indenting encoder refused", name)
			continue
		}
		if got, want := err.Error(), "encoding JSON: "+wantErr.Error(); got != want {
			t.Errorf("%s: error %q, want %q", name, got, want)
		}
		if out.Len() != 0 {
			t.Errorf("%s: %d bytes written for a value that failed to encode", name, out.Len())
		}
	}
}

// failingWriter refuses every write.
type failingWriter struct{}

var errWriteRefused = errors.New("write refused")

func (failingWriter) Write([]byte) (int, error) { return 0, errWriteRefused }

// TestPrintJSON_WriteErrorIsReturned: a failed write is reported, wrapped as the
// encoding error it always was.
func TestPrintJSON_WriteErrorIsReturned(t *testing.T) {
	err := utils.WriteJSON(failingWriter{}, map[string]int{"id": 1})
	if !errors.Is(err, errWriteRefused) || !strings.HasPrefix(err.Error(), "encoding JSON: ") {
		t.Errorf("WriteJSON to a failing writer returned %v, want the wrapped write error", err)
	}
}
