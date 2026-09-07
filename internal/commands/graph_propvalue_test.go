package commands

// Tests for the two statement-content hazards published as SPEC/GRAPH.md § What
// Groadmap Does Not Check, items 2 and 3, and asserted by acceptance criterion
// 38's first two bullets.
//
// Groadmap used to refuse a statement whose bytes were not valid UTF-8, and a
// write whose property value carried a forbidden control character. Both
// refusals are withdrawn: between reading a statement and running it, Groadmap
// checks its length and nothing else about its content. What the engine then
// does with such a statement is the specified behaviour, and it is asserted here
// as an OUTCOME rather than as the absence of a check, because an absence cannot
// be tested and an outcome can.
//
// Each case drives the real command handlers against a real graph store and
// observes what the store ends up HOLDING. That is what makes them non-vacuous:
// the exit code is 0 either way, so only the stored value tells the specified
// behaviour apart from a reintroduced refusal or from an engine that changed.

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
)

// readMemoryBody returns the body property of the Memory node with the given
// key, and reports whether the node exists with a non-null body.
func readMemoryBody(t *testing.T, roadmap, key string) (string, bool) {
	t.Helper()
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphExecute([]string{"-r", roadmap, "--query",
			"MATCH (n:Memory {key:'" + key + "'}) RETURN n.body"}); err != nil {
			t.Fatalf("read back %q: %v", key, err)
		}
	})

	var parsed struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &parsed); err != nil {
		t.Fatalf("read back %q: stdout is not the columns/rows shape: %v\nstdout=%q", key, err, stdout)
	}
	if len(parsed.Rows) == 0 || parsed.Rows[0][0] == nil {
		return "", false
	}
	body, ok := parsed.Rows[0][0].(string)
	if !ok {
		t.Fatalf("read back %q: body is %T, not a string", key, parsed.Rows[0][0])
	}
	return body, true
}

// TestGraphWrite_MalformedUTF8ExecutesAndStoresReplacementChars is acceptance
// criterion 38's first bullet, over the corpus the UTF-8 rule is defined by
// (SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint) rather than over shapes
// invented here.
//
// The engine decodes the statement to characters before its grammar runs and
// replaces every byte that decodes to no character with U+FFFD, so the statement
// it executes is not the statement the caller wrote and no later point can
// recover the byte supplied. Four things are asserted for each shape: the
// command succeeds, the node exists, the stored value is not the one supplied,
// and it carries U+FFFD where the offending bytes were.
func TestGraphWrite_MalformedUTF8ExecutesAndStoresReplacementChars(t *testing.T) {
	const roadmap = "graph-propvalue-utf8"
	defer setupTestGraphRoadmap(t, roadmap)()

	corpus := testenv.MalformedUTF8Corpus()
	if len(corpus) < 4 {
		t.Fatalf("the malformed-UTF-8 corpus holds only %d shapes; it is not the corpus the rule is defined by", len(corpus))
	}

	for _, c := range corpus {
		t.Run(c.Name, func(t *testing.T) {
			key := "malformed-" + strings.ReplaceAll(c.Name, " ", "-")
			escaped := strings.ReplaceAll(c.Value, `\`, `\\`)
			escaped = strings.ReplaceAll(escaped, "'", `\'`)

			if err := runGraphExecute([]string{"-r", roadmap, "--query",
				"CREATE (n:Memory {key: '" + key + "', body: '" + escaped + "'})"}); err != nil {
				t.Fatalf("the statement must execute; Groadmap checks a statement's length and "+
					"nothing else about its content (SPEC/GRAPH.md § What Groadmap Does Not "+
					"Check, item 2). got %v\n  %s", err, c.Why)
			}

			body, present := readMemoryBody(t, roadmap, key)
			if !present {
				t.Fatal("the statement reported success but stored nothing")
			}
			if body == c.Value {
				t.Fatalf("the store holds the supplied bytes verbatim; the specified outcome is "+
					"that the engine replaced them. If the engine now preserves them, item 2 of "+
					"SPEC/GRAPH.md § What Groadmap Does Not Check is stale.\n  %s", c.Why)
			}
			if !strings.ContainsRune(body, utf8.RuneError) {
				t.Errorf("the stored value carries no U+FFFD: %q", body)
			}
			// The bytes the caller supplied are gone, which is the whole harm:
			// nothing downstream can recover them.
			if run := firstInvalidRun(c.Value); run != "" && strings.Contains(body, run) {
				t.Errorf("the stored value still carries the offending bytes %q: %q", run, body)
			}
			// The surrounding text survived, so the assertion above is about the
			// offending bytes and not about the value being mangled wholesale.
			if head, _, ok := strings.Cut(c.Value, firstInvalidRun(c.Value)); ok && !strings.Contains(body, head) {
				t.Errorf("the stored value lost the well-formed text before the offending bytes: %q", body)
			}
		})
	}
}

// firstInvalidRun returns the leading maximal run of bytes in s that begins no
// valid UTF-8 sequence, so the assertions above are about the bytes at fault
// rather than about the whole value. It returns "" when s is well-formed.
func firstInvalidRun(s string) string {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != utf8.RuneError || size != 1 {
			i += size
			continue
		}
		j := i
		for j < len(s) {
			r, size := utf8.DecodeRuneInString(s[j:])
			if r != utf8.RuneError || size != 1 {
				break
			}
			j++
		}
		return s[i:j]
	}
	return ""
}

// TestGraphWrite_CypherEscapeStoresARealControlCharacter is acceptance criterion
// 38's second bullet.
//
// The statement's own text is pure ASCII, which the test asserts rather than
// assumes. Cypher decodes \uXXXX inside a string literal, so the value that
// reaches the store carries a real U+001B, and every later surface that renders
// that value renders the control character with it. The free-text
// control-character constraint that governs task and sprint fields
// (SPEC/MODELS.md § Free-Text Control-Character Constraint) does not reach
// knowledge-graph property values.
func TestGraphWrite_CypherEscapeStoresARealControlCharacter(t *testing.T) {
	const roadmap = "graph-propvalue-control"
	defer setupTestGraphRoadmap(t, roadmap)()

	// A raw string literal: the six characters `\u001b` reach the engine, which is
	// what makes this an escape decoded by Cypher rather than a byte Go put here.
	const query = `CREATE (n:Memory {key: 'escape-decoded', body: 'deploy \u001b[31mFAILED'})`
	for _, r := range query {
		if r < 0x20 || r > 0x7e {
			t.Fatalf("the statement must be pure printable ASCII for this case to mean anything; it carries %U", r)
		}
	}

	if err := runGraphExecute([]string{"-r", roadmap, "--query", query}); err != nil {
		t.Fatalf("the statement must execute (SPEC/GRAPH.md § What Groadmap Does Not Check, "+
			"item 3); got %v", err)
	}

	body, present := readMemoryBody(t, roadmap, "escape-decoded")
	if !present {
		t.Fatal("the statement reported success but stored nothing")
	}
	const want = "deploy \x1b[31mFAILED"
	if body != want {
		t.Fatalf("the store holds %q, want %q: the specified outcome is that Cypher decodes the "+
			"escape and the real U+001B is stored. A run reporting the undecoded text instead "+
			"means the engine stopped decoding the escape and item 3 is stale", body, want)
	}

	// The store legitimately holds control characters, so a statement that names
	// one must reach the data that carries one.
	rows := graphQueryRows(t, roadmap,
		`MATCH (n:Memory {body: 'deploy \u001b[31mFAILED'}) RETURN n.key`)
	if len(rows) != 1 || rows[0][0] != "escape-decoded" {
		t.Errorf("a read matching on the stored control character did not reach the node: %v", rows)
	}
}

// TestGraphWrite_AcceptsLegitimateValues is the other side of the two hazards
// above, and the reason they read as hazards rather than as breakage: real
// knowledge-graph text is stored byte for byte.
func TestGraphWrite_AcceptsLegitimateValues(t *testing.T) {
	const roadmap = "graph-propvalue-accepted"
	defer setupTestGraphRoadmap(t, roadmap)()

	cases := []struct {
		name string
		key  string
		body string
	}{
		{"plain English prose", "sprint-38-scope", "Correctness sweep over twelve recorded defects."},
		{"accented Portuguese", "spec-graph", "Especificação do grafo de conhecimento — acentuação e cedilha."},
		{"CJK", "spec-cjk", "知識グラフのプロパティ値"},
		{"emoji", "release-note", "Sprint 38 shipped 🚀 and was measured 📊"},
		{"permitted whitespace controls", "commit-body", "subject line\n\nbody paragraph\twith a tab"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			escaped := strings.ReplaceAll(tc.body, `\`, `\\`)
			escaped = strings.ReplaceAll(escaped, "'", `\'`)
			escaped = strings.ReplaceAll(escaped, "\n", `\n`)
			escaped = strings.ReplaceAll(escaped, "\t", `\t`)

			if err := runGraphExecute([]string{"-r", roadmap, "--query",
				"CREATE (n:Memory {key: '" + tc.key + "', body: '" + escaped + "'})"}); err != nil {
				t.Fatalf("a legitimate value failed: %v", err)
			}
			body, present := readMemoryBody(t, roadmap, tc.key)
			if !present {
				t.Fatal("the write did not reach the store")
			}
			if body != tc.body {
				t.Errorf("the store holds %q, want %q: a stored value must be unchanged", body, tc.body)
			}
		})
	}
}
