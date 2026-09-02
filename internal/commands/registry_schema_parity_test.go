// Package commands — parity between the key sets the MACHINE-READABLE contract
// publishes and the objects the commands actually return.
//
// # Why this file exists
//
// `rmp --ai-help` emits, for every subcommand, a `stdout_on_success.schema`
// value taken verbatim from that subcommand's `Output.Schema` string in the
// registry (internal/commands/registry_*.go). Most of those strings merely NAME
// the object returned — "Array of task objects.", "Single sprint object." — but
// some ENUMERATE its keys, and an enumeration is a second, hand-written copy of
// a Go struct's JSON shape.
//
// Second copies go stale. This one did: the audit entry grew
// `related_entity_id` and `commit_hash` while the contract went on publishing
// the five keys the entry used to have, handing every agent that reads the
// contract a shape missing two of its members. Nothing failed, because nothing
// compared the two.
//
// The plain-text help surfaces have been held in parity with their models by
// object_key_parity_test.go, which deliberately left these strings alone. That
// left the drift possible on precisely the surface machines read.
//
// # How the guard works
//
// Nothing here restates a key set. Every case DERIVES its expectation by
// reflecting over the model's `json:"..."` tags, through the same jsonObjectKeys
// authority object_key_parity_test.go uses, so a field added to a model reaches
// the expectation the moment it is declared and a contract string that does not
// name it fails. What a case supplies is only WHERE the enumeration is and HOW
// it is written, because the strings spell their lists differently: bare names
// in braces, bare names in parentheses, a nested sub-object, or a quoted JSON
// sketch. Each shape gets one small extractor, and no extractor knows what the
// keys are.
//
// # Scope: total by construction
//
// Every registry entry is accounted for, and the accounting is the half that
// stops the surface growing an unchecked claim:
//
//   - An entry whose Schema ENUMERATES keys is in registrySchemaCases(), and its
//     enumeration is compared against the struct behind it.
//   - An entry whose Schema names an object without enumerating it is in
//     unguardedRegistrySchemas(), together with the reason it carries no
//     enumeration to compare. That acknowledgement is not taken on trust: an
//     acknowledged entry whose Schema later starts enumerating keys fails,
//     because the reason it was skipped for has stopped being true.
//   - An entry that publishes no Schema at all must be one whose Kind is
//     "empty": no payload, so no shape.
//
// An entry in none of the three fails TestRegistrySchemaClaims_AreAllClassified.
// A new subcommand that publishes a key list therefore cannot enter the contract
// unwatched, and a subcommand that is renamed or withdrawn cannot leave a stale
// exemption behind covering nothing.
//
// The free-text form of the unenumerated strings is left exactly as it is. They
// are a published contract; imposing a uniform shape on them would change it,
// which is a different decision from holding the existing enumerations honest.
package commands

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ---------------------------------------------------------------------------
// Addressing a registry entry
// ---------------------------------------------------------------------------

// registryKey names one registry entry the way the CLI is invoked, so that a
// failure message and the acknowledgement list both read as commands. A leaf
// command such as `stats` carries a single subcommand whose Name is empty, and
// is keyed by the family alone.
func registryKey(family, subcommand string) string {
	if subcommand == "" {
		return family
	}
	return family + " " + subcommand
}

// registrySchema returns the Schema string the registry publishes for one entry.
// It is read live from AppRegistry rather than restated here: the value under
// test must be the one the emitter serialises, not a copy of it.
func registrySchema(t *testing.T, family, subcommand string) string {
	t.Helper()

	cmd := AppRegistry().FindCommand(family)
	if cmd == nil {
		t.Fatalf("no %q command in the registry", family)
	}
	sub := cmd.FindSubcommand(subcommand)
	if sub == nil {
		t.Fatalf("no %q subcommand under %q in the registry", subcommand, family)
	}
	if sub.Output.Schema == "" {
		t.Fatalf("%s publishes no schema at all; the guard cannot see the key "+
			"list it is watching", registryKey(family, subcommand))
	}
	return sub.Output.Schema
}

// ---------------------------------------------------------------------------
// Reading a key list back out of a Schema string
// ---------------------------------------------------------------------------

// schemaExtractor reads the key set a Schema string enumerates. label names the
// case in a failure message.
type schemaExtractor func(t *testing.T, schema, label string) []string

// schemaAnnotation matches the gloss a bare-name enumeration may attach to a
// key, as the `stats` schema does in `current=OPEN sprint id or null`. The gloss
// is prose, not part of the key, and is removed before the run is split.
var schemaAnnotation = regexp.MustCompile(`=[^,}]*`)

// nestedBraceGroup matches an innermost brace group, so that repeated
// application flattens a nested enumeration from the inside out.
var nestedBraceGroup = regexp.MustCompile(`\{[^{}]*\}`)

// bracedGroup returns the contents of the nth top-level brace group of s,
// counting from zero. Depth is tracked rather than cutting at the first closing
// brace, because a top-level group may contain nested ones: the `stats` schema
// closes `sprints:{...}` long before it closes the object those keys belong to.
//
// A schema may also carry two alternative shapes one after the other, which is
// how the graph subcommands publish their with-RETURN and without-RETURN forms;
// n selects between them.
func bracedGroup(t *testing.T, s string, n int, label string) string {
	t.Helper()

	depth, start, seen := 0, 0, 0
	for i := range len(s) {
		switch s[i] {
		case '{':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case '}':
			if depth == 0 {
				t.Fatalf("%s: the Schema string closes a brace it never opened, "+
					"so its shape cannot be read: %s", label, s)
			}
			depth--
			if depth == 0 {
				if seen == n {
					return s[start:i]
				}
				seen++
			}
		}
	}
	t.Fatalf("%s: the Schema string has no brace group number %d; it was "+
		"rewritten and this extractor no longer reads it: %s", label, n, s)
	return ""
}

// bareNameKeys turns a run of undecorated key names into the key set it
// publishes. Nested groups are flattened away first, so that a sub-object
// contributes its own name to the enclosing set and not its members.
func bareNameKeys(t *testing.T, run, label string) []string {
	t.Helper()

	for nestedBraceGroup.MatchString(run) {
		run = nestedBraceGroup.ReplaceAllString(run, "")
	}
	run = schemaAnnotation.ReplaceAllString(run, "")
	// A key that introduced a sub-object keeps the colon that introduced it.
	run = strings.ReplaceAll(run, ":", "")
	return splitKeys(t, run, label)
}

// bracedNameList reads the bare-name enumeration written inside the schema's
// first brace group, the form `audit list`, `audit stats`, `sprint show`,
// `roadmap list` and `stats` use.
func bracedNameList(t *testing.T, schema, label string) []string {
	t.Helper()

	return bareNameKeys(t, bracedGroup(t, schema, 0, label), label)
}

// parenNameList reads the bare-name enumeration written inside the schema's
// first parenthesised group, the form both `comment-list` subcommands use:
// `Array of task-comment objects (id, task_id, ...)`.
func parenNameList(t *testing.T, schema, label string) []string {
	t.Helper()

	_, inside, opened := strings.Cut(schema, "(")
	if !opened {
		t.Fatalf("%s: no parenthesised key list in the Schema string: %s", label, schema)
	}
	inside, _, closed := strings.Cut(inside, ")")
	if !closed {
		t.Fatalf("%s: the parenthesised key list is unterminated: %s", label, schema)
	}
	return bareNameKeys(t, inside, label)
}

// nestedNameList builds an extractor for the sub-object a schema writes as
// `member:{...}`, which is how `stats` publishes its `sprints` and `tasks`
// counters. The enclosing object's own keys are read by bracedNameList, which
// sees `member` as one key; this reads what that key contains.
func nestedNameList(member string) schemaExtractor {
	return func(t *testing.T, schema, label string) []string {
		t.Helper()

		_, rest, found := strings.Cut(schema, member+":")
		if !found {
			t.Fatalf("%s: the Schema string no longer introduces a %q sub-object: %s",
				label, member, schema)
		}
		return bareNameKeys(t, bracedGroup(t, rest, 0, label), label)
	}
}

// schemaKeyName matches a token shaped like a JSON key of this CLI: lower snake
// case, no spaces. It is what separates an enumeration of keys from a
// parenthesised aside such as "(status BACKLOG, ordered priority DESC)", whose
// comma-separated parts are prose.
var schemaKeyName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// delimitedGroups returns the contents of every top-level group of s delimited
// by opening and closing. Unbalanced input yields the groups that did close,
// because this is a classifier and not an extractor: it reports what a string
// looks like, and must not fail on a string nothing else will read.
func delimitedGroups(s string, opening, closing byte) []string {
	groups := make([]string, 0, strings.Count(s, string(opening)))
	depth, start := 0, 0
	for i := range len(s) {
		switch s[i] {
		case opening:
			if depth == 0 {
				start = i + 1
			}
			depth++
		case closing:
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 {
				groups = append(groups, s[start:i])
			}
		}
	}
	return groups
}

// enumeratesKeys reports whether a Schema string spells out the keys of an
// object, as opposed to merely naming it.
//
// Two forms count: two or more quoted JSON members, and two or more
// comma-separated bare names that all look like keys. A single member never
// counts — the one-member envelopes are bare map literals with no struct behind
// them — and neither does a run whose parts read as prose.
//
// This is what keeps unguardedRegistrySchemas() honest. Without it, an
// acknowledged entry could grow an enumeration and go on being skipped on the
// strength of an acknowledgement that had become false.
func enumeratesKeys(schema string) bool {
	braced := delimitedGroups(schema, '{', '}')
	parenthesised := delimitedGroups(schema, '(', ')')

	for _, group := range slices.Concat(braced, parenthesised) {
		if len(dedupe(inlineJSONMember.FindAllString(group, -1))) >= 2 {
			return true
		}
		fields := strings.Split(group, ",")
		if len(fields) < 2 {
			continue
		}
		allNames := true
		for _, field := range fields {
			name := strings.TrimSuffix(strings.TrimSpace(field), ":")
			if !schemaKeyName.MatchString(name) {
				allNames = false
				break
			}
		}
		if allNames {
			return true
		}
	}
	return false
}

// quotedMembers builds an extractor for a quoted JSON sketch written as the nth
// brace group, the form every graph subcommand uses. inlineJSONMember, shared
// with object_key_parity_test.go, recognises a key by its quoting and its colon
// alone, which is what these one-line sketches need.
func quotedMembers(n int) schemaExtractor {
	return func(t *testing.T, schema, label string) []string {
		t.Helper()

		group := bracedGroup(t, schema, n, label)
		return dedupe(matchedKeys(t, inlineJSONMember, group, label, "quoted JSON members"))
	}
}

// ---------------------------------------------------------------------------
// The cases
// ---------------------------------------------------------------------------

// schemaCase is one key enumeration a registry Schema string publishes, and the
// object it enumerates.
type schemaCase struct {
	// family and subcommand address the registry entry that carries the string.
	family     string
	subcommand string
	// shape names which enumeration within that string this case reads, because
	// one string may publish more than one.
	shape string
	// model is the struct whose JSON tags the enumeration must name, exactly.
	model any
	// extract reads the enumeration back out, in the form it is written.
	extract schemaExtractor
}

func (c schemaCase) key() string  { return registryKey(c.family, c.subcommand) }
func (c schemaCase) name() string { return c.key() + " / " + c.shape }

// registrySchemaCases enumerates every registry Schema string that spells out
// the key set of a struct this package can reflect over.
//
// A row carries no key names: it names the entry, the struct, which enumeration
// within the string it reads, and the form that enumeration is written in.
func registrySchemaCases() []schemaCase {
	return []schemaCase{
		// --- bare names in braces ----------------------------------------------
		{"roadmap", "list", "roadmap object", models.Roadmap{}, bracedNameList},
		{"sprint", "show", "flat object", models.SprintShowResult{}, bracedNameList},
		{"audit", "list", "audit entry", models.AuditEntry{}, bracedNameList},
		{"audit", "stats", "stats object", models.AuditStats{}, bracedNameList},

		// --- bare names in parentheses -----------------------------------------
		{"task", "comment-list", "comment object", models.TaskComment{}, parenNameList},
		{"sprint", "comment-list", "comment object", models.SprintComment{}, parenNameList},

		// --- one enumeration nested inside another -----------------------------
		{"stats", "", "top-level object", models.RoadmapStats{}, bracedNameList},
		{"stats", "", "sprints sub-object", models.SprintStatsSummary{}, nestedNameList("sprints")},
		{"stats", "", "tasks sub-object", models.TaskStatsSummary{}, nestedNameList("tasks")},

		// --- quoted JSON sketches ----------------------------------------------
		//
		// `graph execute` publishes both result envelopes in one string, the
		// with-columns form first. There is one graph subcommand and it can
		// produce either envelope, so both are read off the same schema; the
		// five-subcommand table this replaces had a row per subcommand and a
		// group order that varied with whether the subcommand could carry a
		// RETURN clause at all.
		{"graph", "execute", "query result", graphQueryResult{}, quotedMembers(0)},
		{"graph", "execute", "ok result", graphOKResult{}, quotedMembers(1)},
		// `graph serve` publishes one envelope, written once at startup rather
		// than per statement: the socket the server bound. It is read here rather
		// than acknowledged as unguarded because a struct does back it, so the
		// published key and the emitted one can be compared instead of merely
		// described.
		{"graph", "serve", "startup object", graphServeResult{}, quotedMembers(0)},
	}
}

// unguardedRegistrySchemas lists every registry entry whose Schema string
// publishes no key enumeration, against the reason it publishes none.
//
// The list exists so that a reader can see exactly what was and was not
// compared, and so that an entry cannot quietly grow an enumeration nobody
// checks: a Schema string in neither this list nor registrySchemaCases() fails
// the classification test.
//
// These strings are free text by design and are left that way. Requiring them to
// enumerate would change what the contract publishes, which is a decision about
// the contract rather than about this guard.
func unguardedRegistrySchemas(t *testing.T) map[string]string {
	t.Helper()

	groups := []struct {
		reason  string
		entries []string
	}{
		{
			reason: "names the returned object without enumerating its keys; the " +
				"enumeration lives on the family help surface, which " +
				"object_key_parity_test.go holds in parity with the same struct",
			entries: []string{
				"task list", "task get", "task next", "task subtasks",
				"task blockers", "task blocking",
				"sprint list", "sprint get", "sprint tasks", "sprint open-tasks",
				"sprint stats",
				"backlog list", "backlog show-next",
			},
		},
		{
			reason: "a one-member envelope emitted from a bare map literal; no struct " +
				"backs it, and a single key cannot drift out of step with a shape it " +
				"wholly constitutes",
			entries: []string{
				"roadmap create", "task create", "sprint create",
				"task comment-add", "sprint comment-add", "web",
			},
		},
		{
			reason: "defers to another entry's enumeration instead of restating it " +
				"(\"same shape as audit list\"), which is the second copy this file exists " +
				"to prevent rather than one to check",
			entries: []string{"audit history"},
		},
		{
			reason:  "states that stdout carries no payload, so there is no shape to enumerate",
			entries: []string{"roadmap remove"},
		},
		{
			reason: "points at DATA_FORMATS.md for a document whose shape is specified " +
				"there; it is not the key list of a struct this package returns",
			entries: []string{"ai-help"},
		},
	}

	out := make(map[string]string, 32)
	for _, g := range groups {
		for _, entry := range g.entries {
			if _, dup := out[entry]; dup {
				t.Fatalf("%q is acknowledged twice with different reasons; the list "+
					"must say one thing about each entry", entry)
			}
			out[entry] = g.reason
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// The gates
// ---------------------------------------------------------------------------

// TestRegistrySchemaKeyLists_MatchTheirObject is the parity guard over the
// machine-readable contract.
//
// It fails when a Schema string names a key its object does not carry, or omits
// one the object does. Adding a field to a model without publishing it in every
// contract string that enumerates that model's shape fails here.
func TestRegistrySchemaKeyLists_MatchTheirObject(t *testing.T) {
	for _, tc := range registrySchemaCases() {
		t.Run(tc.name(), func(t *testing.T) {
			schema := registrySchema(t, tc.family, tc.subcommand)
			modelType := reflect.TypeOf(tc.model)
			want := jsonObjectKeys(t, modelType)
			got := tc.extract(t, schema, tc.name())

			missing, unknown := diffKeys(want, got)
			if len(missing) == 0 && len(unknown) == 0 {
				return
			}

			t.Errorf("the %s the AI Agent Contract publishes for %q disagrees with %s\n"+
				"  published (%d): %s\n"+
				"  %s carries (%d): %s\n"+
				"  carried by the object but never published: %s\n"+
				"  published but not a key of the object: %s\n"+
				"  the Schema string as the contract emits it:\n    %s",
				tc.shape, tc.key(), modelType,
				len(got), strings.Join(sortedCopy(got), ", "),
				modelType, len(want), strings.Join(sortedCopy(want), ", "),
				joinOrNone(missing), joinOrNone(unknown), schema)
		})
	}
}

// TestRegistrySchemaClaims_AreAllClassified is the guard on the guard.
//
// It walks every registry entry and requires each one to be classified: its
// Schema enumerates keys and a case reads it, or its Schema publishes no
// enumeration and the list says why, or it publishes no Schema at all and
// therefore declares an empty payload. An entry in none of the three is a claim
// about output shape that nothing holds in parity, which is the state that let
// the audit entry drift in the first place.
//
// It also fails on a classification that has outlived its entry, so a renamed or
// withdrawn subcommand cannot leave behind an acknowledgement that silently
// covers nothing.
func TestRegistrySchemaClaims_AreAllClassified(t *testing.T) {
	cases := registrySchemaCases()
	guarded := make(map[string]bool, len(cases))
	for _, tc := range cases {
		guarded[tc.key()] = true
	}
	unguarded := unguardedRegistrySchemas(t)

	present := make(map[string]bool, 64)
	reg := AppRegistry()
	for i := range reg.Commands {
		cmd := &reg.Commands[i]
		for j := range cmd.Subcommands {
			sub := &cmd.Subcommands[j]
			key := registryKey(cmd.Name, sub.Name)
			present[key] = true

			if sub.Output.Schema == "" {
				if sub.Output.Kind != "empty" {
					t.Errorf("%s returns stdout of kind %q but publishes no schema "+
						"describing it, so the contract says nothing an agent can "+
						"consume and nothing here can check", key, sub.Output.Kind)
				}
				continue
			}

			_, acknowledged := unguarded[key]
			switch {
			case guarded[key] && acknowledged:
				t.Errorf("%s is both read by registrySchemaCases() and acknowledged as "+
					"unguarded; one of the two is wrong about what its Schema publishes", key)
			case acknowledged && enumeratesKeys(sub.Output.Schema):
				t.Errorf("%s is acknowledged as publishing no key enumeration, but its "+
					"Schema now enumerates one, so the acknowledgement is no longer true "+
					"and the enumeration is unchecked. Move it to registrySchemaCases() "+
					"with the struct it describes.\n  schema: %s\n  acknowledged because: %s",
					key, sub.Output.Schema, unguarded[key])
			case !guarded[key] && !acknowledged:
				t.Errorf("%s publishes a schema that is in neither registrySchemaCases() "+
					"nor unguardedRegistrySchemas(), so nothing holds it in parity with "+
					"the object it describes. Add a case when it enumerates keys, or "+
					"acknowledge it with the reason it does not.\n  schema: %s",
					key, sub.Output.Schema)
			}
		}
	}

	for _, tc := range cases {
		if !present[tc.key()] {
			t.Errorf("registrySchemaCases() reads %q, which is no longer a registry "+
				"entry; the case checks nothing", tc.key())
		}
	}
	stale := make([]string, 0, len(unguarded))
	for key := range unguarded {
		if !present[key] {
			stale = append(stale, key)
		}
	}
	for _, key := range sortedCopy(stale) {
		t.Errorf("unguardedRegistrySchemas() acknowledges %q, which is no longer a "+
			"registry entry; the acknowledgement covers nothing", key)
	}
}
