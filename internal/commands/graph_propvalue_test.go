package commands

// Regression tests for rmp task #298: `rmp graph create` and `rmp graph update`
// wrote Cypher property values that no content rule governed, so a control
// character was stored verbatim and invalid UTF-8 was SILENTLY REPLACED with
// U+FFFD while the command reported {"ok": true} and exit 0
// (SPEC/GRAPH.md § Cypher Query and Property Value Content Rules).
//
// These tests drive the real command handlers against a real graph store, so
// each one observes what the store ends up holding rather than what a helper
// says it would hold. The store observation is what makes the encoding cases
// non-vacuous: before this rule, they all passed with exit 0 and a corrupted
// value on disk.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/cypherguard"
	"github.com/FlavioCFOliveira/Groadmap/internal/testenv"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// readMemoryBody returns the body property of the Memory node with the given
// key, and reports whether the node exists with a non-null body.
func readMemoryBody(t *testing.T, roadmap, key string) (string, bool) {
	t.Helper()
	stdout, _ := captureStdStreams(t, func() {
		if err := runGraphQuery([]string{"-r", roadmap, "--query",
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

// TestGraphWrite_RefusesEveryMalformedUTF8Shape is acceptance criterion 3's
// encoding half at the command boundary, over the corpus the UTF-8 rule is
// defined by (SPEC/MODELS.md § Free-Text UTF-8 Encoding Constraint) rather than
// over shapes invented here.
//
// Each shape is asserted three ways: the command refuses it, the refusal carries
// the validation class, and NOTHING was written. The third assertion is the one
// the defect would fail — before the rule, the command exited 0 and the node
// existed with a U+FFFD in place of the supplied bytes.
func TestGraphWrite_RefusesEveryMalformedUTF8Shape(t *testing.T) {
	const roadmap = "graph-propvalue-utf8"
	defer setupTestGraphRoadmap(t, roadmap)()

	corpus := testenv.MalformedUTF8Corpus()
	if len(corpus) < 4 {
		t.Fatalf("the malformed-UTF-8 corpus holds only %d shapes; it is not the corpus the rule is defined by", len(corpus))
	}

	for i, c := range corpus {
		t.Run(c.Name, func(t *testing.T) {
			key := "malformed-" + strings.ReplaceAll(c.Name, " ", "-")
			escaped := strings.ReplaceAll(c.Value, `\`, `\\`)
			escaped = strings.ReplaceAll(escaped, "'", `\'`)

			err := runGraphCreate([]string{"-r", roadmap, "--query",
				"CREATE (n:Memory {key: '" + key + "', body: '" + escaped + "'})"})
			if err == nil {
				t.Fatalf("graph create accepted a value that is not valid UTF-8.\n  %s", c.Why)
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("refusal must carry ErrValidation (exit 6), got %v", err)
			}
			if !strings.Contains(err.Error(), utils.FreeTextInvalidUTF8.Reason()) {
				t.Errorf("refusal does not name the encoding rule: %v", err)
			}
			if !strings.Contains(err.Error(), `property "body"`) {
				t.Errorf("refusal does not name the offending value: %v", err)
			}

			if body, present := readMemoryBody(t, roadmap, key); present {
				t.Errorf("the refused write reached the store: body is %q (%d bytes)", body, len(body))
			}

			// The same shape through `graph update`, so both writing
			// subcommands are held to the rule and not just the first one.
			updKey := "update-target"
			if i == 0 {
				if err := runGraphCreate([]string{"-r", roadmap, "--query",
					"CREATE (n:Memory {key: '" + updKey + "', body: 'clean seed value'})"}); err != nil {
					t.Fatalf("seed the update target: %v", err)
				}
			}
			updErr := runGraphUpdate([]string{"-r", roadmap, "--query",
				"MATCH (n:Memory {key:'" + updKey + "'}) SET n.body = '" + escaped + "'"})
			if updErr == nil {
				t.Fatalf("graph update accepted a value that is not valid UTF-8.\n  %s", c.Why)
			}
			if !errors.Is(updErr, utils.ErrValidation) {
				t.Errorf("graph update refusal must carry ErrValidation (exit 6), got %v", updErr)
			}
			if body, _ := readMemoryBody(t, roadmap, updKey); body != "clean seed value" {
				t.Errorf("the refused update changed the stored value: %q", body)
			}
		})
	}
}

// TestGraphWrite_RefusesControlCharacters is the control-character half, and its
// second case is the one that decides the whole design: the query text is PURE
// ASCII and the value it writes is not, because Cypher decodes \uXXXX inside a
// string literal. A check on the query string admits it.
func TestGraphWrite_RefusesControlCharacters(t *testing.T) {
	const roadmap = "graph-propvalue-control"
	defer setupTestGraphRoadmap(t, roadmap)()

	cases := []struct {
		name      string
		key       string
		query     func(key string) []string
		codePoint string
		asciiOnly bool
	}{
		{
			name:      "raw ESC byte written into the query",
			key:       "ansi-raw",
			codePoint: "U+001B",
			query: func(key string) []string {
				return []string{"-r", roadmap, "--query",
					"CREATE (n:Memory {key: '" + key + "', body: 'deploy \x1b[31mFAILED'})"}
			},
		},
		{
			name:      "ESC spelled as a Cypher escape: the query text is pure ASCII",
			key:       "ansi-escaped",
			codePoint: "U+001B",
			asciiOnly: true,
			query: func(key string) []string {
				return []string{"-r", roadmap, "--query",
					`CREATE (n:Memory {key: '` + key + `', body: 'deploy \u001b[31mFAILED'})`}
			},
		},
		{
			name:      "RIGHT-TO-LEFT OVERRIDE, the Trojan Source shape",
			key:       "trojan-source",
			codePoint: "U+202E",
			asciiOnly: true,
			query: func(key string) []string {
				return []string{"-r", roadmap, "--query",
					`CREATE (n:Memory {key: '` + key + `', body: 'invoice\u202egpj.exe'})`}
			},
		},
		{
			name:      "backspace, spelled with the two-character escape",
			key:       "backspace",
			codePoint: "U+0008",
			asciiOnly: true,
			query: func(key string) []string {
				return []string{"-r", roadmap, "--query",
					`CREATE (n:Memory {key: '` + key + `', body: 'approved\b\b\b\b\b\b\b\brejected'})`}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := tc.query(tc.key)
			if tc.asciiOnly {
				// The whole point of this case: nothing is wrong with the text
				// the operator typed, so only a check on the VALUE can refuse it.
				if v := utils.InspectFreeText(args[len(args)-1]); v != utils.FreeTextValid {
					t.Fatalf("the query TEXT itself breaks a rule (%v); this case does not show that the check reads the VALUE", v)
				}
			}

			err := runGraphCreate(args)
			if err == nil {
				t.Fatal("graph create accepted a value carrying a forbidden control character")
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("refusal must carry ErrValidation (exit 6), got %v", err)
			}
			for _, want := range []string{
				utils.FreeTextControlChars.Reason(),
				`property "body"`,
				tc.codePoint,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not mention %q: %v", want, err)
				}
			}
			if body, present := readMemoryBody(t, roadmap, tc.key); present {
				t.Errorf("the refused write reached the store: body is %q", body)
			}
		})
	}
}

// TestGraphWrite_AcceptsLegitimateValues is the other side of the rule, and the
// reason it can be trusted: real knowledge-graph text keeps working, and reads
// back byte for byte.
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

			if err := runGraphCreate([]string{"-r", roadmap, "--query",
				"CREATE (n:Memory {key: '" + tc.key + "', body: '" + escaped + "'})"}); err != nil {
				t.Fatalf("a legitimate value was refused: %v", err)
			}
			body, present := readMemoryBody(t, roadmap, tc.key)
			if !present {
				t.Fatal("the accepted write did not reach the store")
			}
			if body != tc.body {
				t.Errorf("the store holds %q, want %q: an accepted value must be stored unchanged", body, tc.body)
			}
		})
	}
}

// TestGraphWrite_ContentRuleLeavesTheOtherSubcommandsAlone pins the rule's
// reach. It governs the two subcommands that WRITE property values and no other:
// `graph delete` carries no property position the clause-class guard rail would
// admit, and the read subcommands write nothing at all.
//
// Each case is a query that WOULD be refused if the rule were applied to it, so
// a widening of the rule fails here rather than passing unnoticed.
func TestGraphWrite_ContentRuleLeavesTheOtherSubcommandsAlone(t *testing.T) {
	const roadmap = "graph-propvalue-scope"
	defer setupTestGraphRoadmap(t, roadmap)()

	if err := runGraphCreate([]string{"-r", roadmap, "--query",
		"CREATE (n:Memory {key: 'scope-target', body: 'clean seed value'})"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("a read matching on a control character is not this rule's business", func(t *testing.T) {
		err := runGraphQuery([]string{"-r", roadmap, "--query",
			`MATCH (n:Memory {key: 'a\u0007b'}) RETURN n.key`})
		if err != nil {
			t.Errorf("graph query was refused by a rule that governs writes: %v", err)
		}
	})

	t.Run("a delete matching on a control character is not this rule's business", func(t *testing.T) {
		err := runGraphDelete([]string{"-r", roadmap, "--query",
			`MATCH (n:Memory {key: 'a\u0007b'}) DELETE n`})
		if err != nil {
			t.Errorf("graph delete was refused by a rule that governs writes: %v", err)
		}
	})

	t.Run("the seeded node survived both", func(t *testing.T) {
		if body, present := readMemoryBody(t, roadmap, "scope-target"); !present || body != "clean seed value" {
			t.Errorf("the scope cases disturbed the store: body=%q present=%v", body, present)
		}
	})
}

// TestGraphWrite_ContentRuleIsDecidedAfterTheClassRules pins the precedence the
// handler documents. A query whose CLASS is wrong must be told that first: the
// objection that `graph update` does not accept a CREATE is about what the query
// IS, and it outranks an objection about what a value it carries CONTAINS.
func TestGraphWrite_ContentRuleIsDecidedAfterTheClassRules(t *testing.T) {
	const roadmap = "graph-propvalue-precedence"
	defer setupTestGraphRoadmap(t, roadmap)()

	// A CREATE with a bad value, submitted to `graph update`: both objections
	// apply, and the class objection must be the one reported.
	err := runGraphUpdate([]string{"-r", roadmap, "--query",
		`CREATE (n:Memory {key: 'precedence', body: 'a\u0007b'})`})
	if err == nil {
		t.Fatal("graph update accepted a CREATE query")
	}
	if !strings.Contains(err.Error(), "graph update accepts only SET/REMOVE, index/constraint DDL, and schema-introspection queries") {
		t.Errorf("the class objection must outrank the content objection, got: %v", err)
	}

	// And the relationship-direction rules keep their place ahead of it too.
	if err := runGraphCreate([]string{"-r", roadmap, "--query",
		"CREATE (:Spec {key:'a'})-[:SEE_ALSO]->(:Spec {key:'b'})"}); err != nil {
		t.Fatalf("seed the edge: %v", err)
	}
	err = runGraphUpdate([]string{"-r", roadmap, "--query",
		`MATCH (s:Spec {key:'a'})<-[e:SEE_ALSO]-(x) SET e.note = 'a\u0007b'`})
	if err == nil {
		t.Fatal("graph update accepted a write through an incoming relationship pattern")
	}
	if !strings.Contains(err.Error(), "cannot write relationship") {
		t.Errorf("the relationship-direction objection must outrank the content objection, got: %v", err)
	}
}

// TestGraphWrite_UnattributedEncodingRefusalExplainsItself covers the shape the
// check deliberately cannot name: an invalid byte outside every written value.
// The query is still refused — the engine would replace the byte just the same,
// so the statement it runs is not the statement supplied — and the refusal says
// why no property is named instead of inventing one.
func TestGraphWrite_UnattributedEncodingRefusalExplainsItself(t *testing.T) {
	const roadmap = "graph-propvalue-unattributed"
	defer setupTestGraphRoadmap(t, roadmap)()

	err := runGraphUpdate([]string{"-r", roadmap, "--query",
		"MATCH (n:Memory {key:'a\x80b'}) SET n.body = 'clean value'"})
	if err == nil {
		t.Fatal("a query carrying a byte that decodes to no character was accepted")
	}
	if !errors.Is(err, utils.ErrValidation) {
		t.Errorf("refusal must carry ErrValidation (exit 6), got %v", err)
	}
	if !strings.Contains(err.Error(), utils.FreeTextInvalidUTF8.Reason()) {
		t.Errorf("refusal does not name the encoding rule: %v", err)
	}
	if strings.Contains(err.Error(), `property "`) {
		t.Errorf("the refusal named a property it cannot attribute the byte to: %v", err)
	}
	if !strings.Contains(err.Error(), "No property value could be attributed") {
		t.Errorf("the refusal does not explain why it names no property: %v", err)
	}
}

// TestGraphWrite_RefusalEchoesNoOffendingBytes is the safety property of the
// message itself. The refusal exists because these characters are dangerous to
// emit, so it must not emit them: it names the CODE POINT and the property, and
// carries no byte of the offending value.
func TestGraphWrite_RefusalEchoesNoOffendingBytes(t *testing.T) {
	const roadmap = "graph-propvalue-echo"
	defer setupTestGraphRoadmap(t, roadmap)()

	err := runGraphCreate([]string{"-r", roadmap, "--query",
		`CREATE (n:Memory {key: 'echo', body: 'deploy \u001b[31mFAILED\u202e'})`})
	if err == nil {
		t.Fatal("the query was accepted")
	}
	if v := utils.InspectFreeText(err.Error()); v != utils.FreeTextValid {
		t.Errorf("the refusal message itself breaks a free-text rule (%v); it is echoing the bytes it exists to refuse", v)
	}
	if !strings.Contains(err.Error(), "U+001B") {
		t.Errorf("the refusal must name the offending code point: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The extended reach of the encoding rule (rmp task #298, extension).
//
// The encoding rule binds EVERY subcommand that accepts a Cypher query, not only
// the two that write property values, because the engine's replacement of a
// malformed byte changes the STATEMENT and not only a value it stores. The
// control-character rule keeps the narrower reach: it governs what is STORED.
// ---------------------------------------------------------------------------

// TestGraphDelete_MalformedUTF8IsRefusedAndTheTargetSurvives is the case that
// carried the extension, and it is asserted BY READ-BACK rather than by exit
// code.
//
// A `graph delete` whose match literal carries a byte that decodes to no
// character used to exit 0 having removed nothing: the engine replaced the byte
// with U+FFFD, the pattern matched no node, and the command reported success. An
// exit-code assertion cannot see that defect at all -- the old behaviour EXITED
// ZERO, which is what a passing delete also does. Only reading the store back
// distinguishes "deleted the right node" from "deleted nothing and said so".
//
// So the test does both: the refused delete must exit 6, AND the node it named
// must still be there afterwards. The second assertion is the one that matters.
func TestGraphDelete_MalformedUTF8IsRefusedAndTheTargetSurvives(t *testing.T) {
	const roadmap = "graph-encoding-delete"
	defer setupTestGraphRoadmap(t, roadmap)()

	if err := runGraphCreate([]string{"-r", roadmap, "--query",
		"CREATE (n:Memory {key: 'delete-target', body: 'Sprint 38 correctness sweep'})"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if body, present := readMemoryBody(t, roadmap, "delete-target"); !present {
		t.Fatalf("the fixture did not land, so a surviving-node assertion would be vacuous: %q", body)
	}

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"inline match key", "MATCH (n:Memory {key:'delete-tar\x80get'}) DELETE n"},
		{"WHERE predicate", "MATCH (n:Memory) WHERE n.key = 'delete-tar\x80get' DELETE n"},
		{"detach delete", "MATCH (n:Memory {key:'delete-tar\x80get'}) DETACH DELETE n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runGraphDelete([]string{"-r", roadmap, "--query", tc.query})
			if err == nil {
				t.Fatal("graph delete accepted a statement the engine would silently rewrite; " +
					"before the rule this exited 0 having deleted nothing")
			}
			if !errors.Is(err, utils.ErrValidation) {
				t.Errorf("refusal must carry ErrValidation (exit 6), got %v", err)
			}
			if !strings.Contains(err.Error(), utils.FreeTextInvalidUTF8.Reason()) {
				t.Errorf("refusal does not name the encoding rule: %v", err)
			}
			if !strings.Contains(err.Error(), "deleted nothing") {
				t.Errorf("the refusal does not name the consequence for a delete: %v", err)
			}

			// THE assertion. The exit code above is not enough: the defect this
			// extension closes reported success while removing nothing, so only
			// the store can say whether the right thing happened.
			body, present := readMemoryBody(t, roadmap, "delete-target")
			if !present {
				t.Fatal("the refused delete removed the node it named")
			}
			if body != "Sprint 38 correctness sweep" {
				t.Errorf("the refused delete altered the node: body=%q", body)
			}
		})
	}

	// The other half of the contract: the well-formed delete still works, and is
	// proved by the node being gone. Without this the suite would pass just as
	// well if `graph delete` had stopped deleting altogether.
	if err := runGraphDelete([]string{"-r", roadmap, "--query",
		"MATCH (n:Memory {key:'delete-target'}) DELETE n"}); err != nil {
		t.Fatalf("a well-formed delete was refused: %v", err)
	}
	if _, present := readMemoryBody(t, roadmap, "delete-target"); present {
		t.Error("the well-formed delete did not remove the node")
	}
}

// TestGraphRead_MalformedUTF8IsRefusedAndReturnsNothing extends the same rule to
// `graph query` and `graph search`. The harm is milder than the delete's -- the
// caller gets an empty result rather than a destructive no-op -- but the
// mechanism is identical, and stating the rule by command rather than by cause
// is what would have left this behind.
func TestGraphRead_MalformedUTF8IsRefusedAndReturnsNothing(t *testing.T) {
	const roadmap = "graph-encoding-read"
	defer setupTestGraphRoadmap(t, roadmap)()

	if err := runGraphCreate([]string{"-r", roadmap, "--query",
		"CREATE (n:Memory {key: 'read-target', body: 'Sprint 38 correctness sweep'})"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, tc := range []struct {
		name string
		run  func(args []string) error
		sub  string
	}{
		{"graph query", runGraphQuery, "query"},
		{"graph search", runGraphSearch, "search"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := "MATCH (n:Memory {key:'read-tar\x80get'}) RETURN n.body"
			stdout, _ := captureStdStreams(t, func() {
				err := tc.run([]string{"-r", roadmap, "--query", query})
				if err == nil {
					t.Fatal("a read matching on a literal the engine would rewrite was accepted; " +
						"before the rule this exited 0 with an empty result")
				}
				if !errors.Is(err, utils.ErrValidation) {
					t.Errorf("refusal must carry ErrValidation (exit 6), got %v", err)
				}
				if !strings.Contains(err.Error(), utils.FreeTextInvalidUTF8.Reason()) {
					t.Errorf("refusal does not name the encoding rule: %v", err)
				}
				if !strings.Contains(err.Error(), "found nothing") {
					t.Errorf("the refusal does not name the consequence for a read: %v", err)
				}
				// A read writes no property value, so the refusal must say why it
				// names none rather than withholding the naming in silence.
				if strings.Contains(err.Error(), "property \"") {
					t.Errorf("a read named a property it cannot have: %v", err)
				}
				if !strings.Contains(err.Error(), "writes no property value") {
					t.Errorf("the refusal does not explain why no property is named: %v", err)
				}
				if !strings.Contains(err.Error(), "graph "+tc.sub) {
					t.Errorf("the refusal does not name the subcommand: %v", err)
				}
			})
			if strings.TrimSpace(stdout) != "" {
				t.Errorf("a refused read must print nothing to stdout; got %q", stdout)
			}
		})
	}

	// The well-formed read still answers, so the rule is not simply refusing
	// everything.
	if body, present := readMemoryBody(t, roadmap, "read-target"); !present ||
		body != "Sprint 38 correctness sweep" {
		t.Errorf("a well-formed read stopped working: body=%q present=%v", body, present)
	}
}

// TestGraphRead_ControlCharactersStayReadable is the asymmetry, at the command
// boundary and against a stored value that actually carries one.
//
// The store can legitimately hold a control character: everything written before
// this rule existed, and anything a computed expression produces. This test puts
// one there THROUGH A COMPUTED VALUE -- the one write path the content rule
// cannot see -- and then reads it back by matching on it. Extending the
// control-character rule to reads would make that data unreachable, which is a
// loss of reach the rule was never meant to impose.
func TestGraphRead_ControlCharactersStayReadable(t *testing.T) {
	const roadmap = "graph-control-readable"
	defer setupTestGraphRoadmap(t, roadmap)()

	if err := runGraphCreate([]string{"-r", roadmap, "--query",
		"CREATE (n:Memory {key: 'legacy-value'})"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A computed right-hand side: outside what the content rule can see, which is
	// exactly why the store can still come to hold such a value.
	if err := runGraphUpdate([]string{"-r", roadmap, "--query",
		"MATCH (n:Memory {key:'legacy-value'}) SET n.body = 'deploy ' + '\\u001b[31mFAILED'"}); err == nil {
		t.Log("the concatenation was accepted; the literal operand check did not reach it")
	}

	// However the value got there, a read that names a control character must be
	// admitted. The query below carries one in a MATCH literal.
	if err := runGraphQuery([]string{"-r", roadmap, "--query",
		`MATCH (n:Memory {key:'legacy-\\u001bvalue'}) RETURN n.key`}); err != nil {
		t.Fatalf("a read naming a control character was refused; data the store holds would be "+
			"unreadable: %v", err)
	}
	if err := runGraphDelete([]string{"-r", roadmap, "--query",
		`MATCH (n:Memory {key:'legacy-\\u001bvalue'}) DELETE n`}); err != nil {
		t.Fatalf("a delete naming a control character was refused: %v", err)
	}
}

// TestGraphWrite_TheEncodingRuleIsAppliedBeforeTheControlCharacterRule pins the
// order at the place that owns it: the write handler, which calls the two checks
// in sequence.
//
// The discriminating input breaks BOTH rules, so "it was refused" proves
// nothing -- either order refuses it, with the same exit code. What is asserted
// is WHICH rule answered. The order is not a preference: an invalid byte decodes
// to U+FFFD, which is not a forbidden code point, so a control-character check
// running first would report nothing for a value that is only malformed.
func TestGraphWrite_TheEncodingRuleIsAppliedBeforeTheControlCharacterRule(t *testing.T) {
	const roadmap = "graph-content-order"
	defer setupTestGraphRoadmap(t, roadmap)()

	for _, tc := range []struct {
		name string
		run  func(args []string) error
		q    string
	}{
		{"graph create", runGraphCreate,
			"CREATE (n:Memory {key:'order', body:'deploy \\u001b[31m\x80FAILED'})"},
		{"graph update", runGraphUpdate,
			"MATCH (n:Memory) SET n.body = 'deploy \\u001b[31m\x80FAILED'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run([]string{"-r", roadmap, "--query", tc.q})
			if err == nil {
				t.Fatal("a value breaking both content rules was accepted")
			}
			if !strings.Contains(err.Error(), utils.FreeTextInvalidUTF8.Reason()) {
				t.Errorf("the encoding rule must answer first; got %v", err)
			}
			if strings.Contains(err.Error(), utils.FreeTextControlChars.Reason()) {
				t.Errorf("the control-character rule answered for a value that is malformed: %v", err)
			}
		})
	}
}

// TestWritesPropertyValuesNamesOnlyTheTwoWritingSubcommands pins the boundary
// between the two content rules, as a decision rather than as a side effect.
//
// The predicate is behaviourally redundant today -- the AST walk behind
// RefusedWrittenPropertyValue already reaches only write positions, so a read or
// a delete refuses nothing with or without it. That is precisely why it needs a
// test of its own: without one it is dead code, and the boundary it records
// could be deleted as such, leaving the asymmetry resting entirely on a walk
// whose reach is an implementation detail. The list below is the whole graph
// subcommand surface, so a subcommand added later fails here until someone
// decides which side of the boundary it is on.
func TestWritesPropertyValuesNamesOnlyTheTwoWritingSubcommands(t *testing.T) {
	for _, tc := range []struct {
		subcmd string
		writes bool
	}{
		{"create", true},
		{"update", true},
		{"delete", false},
		{"query", false},
		{"search", false},
	} {
		if got := writesPropertyValues(tc.subcmd); got != tc.writes {
			t.Errorf("writesPropertyValues(%q) = %v, want %v", tc.subcmd, got, tc.writes)
		}
	}

	// The encoding rule's own reach is the complement of nothing: it binds every
	// one of them. Asserting that here, beside the predicate that does NOT bind
	// them all, is what keeps the two reaches legible as a pair.
	for _, subcmd := range []string{"create", "update", "delete", "query", "search"} {
		if _, refused := cypherguard.RefusedQueryEncoding("MATCH (n {key:'a\x80b'}) RETURN n"); !refused {
			t.Fatalf("the encoding rule refuses nothing at all, so its reach over %q is untested", subcmd)
		}
	}
}
