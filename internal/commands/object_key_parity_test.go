// Package commands — parity between the object key lists the help surfaces
// PUBLISH and the objects the commands actually RETURN.
//
// # Why this file exists
//
// Several help surfaces publish, in prose, the key set of a JSON object the
// binary returns on stdout. SPEC/HELP.md § Family-help template mandates the
// "Object key list" line for exactly that reason: an agent or a script reads it
// to learn the shape it must consume.
//
// A published key list is a SECOND copy of a Go struct's shape, written by
// hand, and a second copy goes stale. It did. `models.Sprint` gained `title`
// and `order` while the list in `rmp sprint --help` went on naming the nine
// keys the object used to have, and `models.SprintShowResult` gained
// `sprint_title` while both surfaces that publish the `sprint show` shape went
// on naming eleven of its twelve keys. Nothing failed, because nothing compared
// the two.
//
// # How the guard works
//
// The expectation is never restated here. Every case DERIVES its key set by
// reflecting over the model's `json:"..."` tags, so a field added to a model
// reaches the expectation the moment it is declared, and any help that does not
// name it fails. Restating the list in this file would merely add a third copy
// to keep in step, which is the defect these tests exist to prevent.
//
// What a case supplies is only WHERE the list is and HOW it is written, because
// the surfaces spell their lists differently: a comma-separated run of names, an
// indented two-column block, a JSON document sketch with one member per line, or
// a sketch written inline. Each shape gets one small extractor, and no extractor
// knows what the keys are.
//
// # Scope
//
// Every help surface that spells out the key set of a Go struct this package can
// reflect over: the domain models in internal/models, plus the two result
// envelopes of the graph family. Two published shapes are deliberately absent
// because no struct backs them — `rmp roadmap create` and `rmp web` both emit a
// bare map literal, so there is nothing to hold them in parity WITH.
//
// The `Schema` strings of the machine-readable contract
// (internal/commands/registry_*.go) are held in parity too, but not here:
// registry_schema_parity_test.go covers them. They are deliberately
// heterogeneous — some name the object without enumerating it ("Array of task
// objects."), some spell its keys out ("{sprint_id, ...}") — so they are read
// from the registry against a classification of every entry in it, rather than
// from the help printers this file walks. The two files share the jsonObjectKeys
// authority and the extractor pattern, and neither restates a key set.
package commands

import (
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// ---------------------------------------------------------------------------
// The authority: keys derived from the model itself
// ---------------------------------------------------------------------------

// jsonObjectKeys returns, in declaration order, the JSON object keys a value of
// typ marshals to. It is the single authority every expectation in this file is
// derived from.
//
// An embedded field is rejected rather than guessed at: encoding/json inlines an
// untagged embedded struct's own keys, so counting it as one key would make the
// derived set silently wrong. No struct watched here embeds today; the failure
// exists so that introducing one is a visible decision rather than a quiet hole.
func jsonObjectKeys(t *testing.T, typ reflect.Type) []string {
	t.Helper()

	if typ.Kind() != reflect.Struct {
		t.Fatalf("jsonObjectKeys: %s is not a struct", typ)
	}

	keys := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)

		if field.PkgPath != "" {
			continue // unexported: never marshalled
		}
		if field.Anonymous {
			t.Fatalf("jsonObjectKeys: %s embeds %s; encoding/json inlines an "+
				"embedded struct's keys, so this helper must be taught to "+
				"flatten it before the derived key set can be trusted",
				typ, field.Type)
		}

		name, marshalled := jsonFieldName(&field)
		if !marshalled {
			continue
		}
		keys = append(keys, name)
	}
	return keys
}

// jsonFieldName reports the JSON key an exported field marshals to, and whether
// it is marshalled at all. A field tagged `json:"-"` is skipped; an untagged
// field marshals under its Go name.
//
// The field is taken by pointer because reflect.StructField is a wide value and
// this is called once per field of every watched struct.
func jsonFieldName(field *reflect.StructField) (string, bool) {
	tag, tagged := field.Tag.Lookup("json")
	if !tagged {
		return field.Name, true
	}
	name, _, _ := strings.Cut(tag, ",")
	switch name {
	case "-":
		return "", false
	case "":
		return field.Name, true
	default:
		return name, true
	}
}

// ---------------------------------------------------------------------------
// Reading a published list back out of help text
// ---------------------------------------------------------------------------

// keyExtractor reads the key list a help output publishes at marker.
type keyExtractor func(t *testing.T, help, marker string) []string

// parenthetical matches an aside such as "(array of int)" or "(map)" that a
// published list attaches to a key. The aside is prose, not a key, so it is
// removed before the run is split — and removed FIRST, so that a period inside
// one cannot be mistaken for the end of the run.
var parenthetical = regexp.MustCompile(`\([^)]*\)`)

// regionAfter returns the help text following marker, failing the test when the
// marker is absent. A missing marker means the surface stopped publishing its
// key list, or renamed the phrase that introduces it; either way the guard can
// no longer see what it is watching, so silence is not an acceptable outcome.
func regionAfter(t *testing.T, help, marker string) string {
	t.Helper()

	_, rest, found := strings.Cut(help, marker)
	if !found {
		t.Fatalf("the phrase %q that introduces this key list is no longer in "+
			"the help output; the guard cannot see the list it is watching", marker)
	}
	return rest
}

// commaList reads the comma-separated key run that follows marker.
//
// The run ends at the first sentence-ending period outside an aside, which is
// how each of these surfaces closes its list before resuming prose
// ("... commit_hash. The last two are null on ...").
func commaList(t *testing.T, help, marker string) []string {
	t.Helper()

	run := parenthetical.ReplaceAllString(regionAfter(t, help, marker), "")
	if end := strings.Index(run, "."); end >= 0 {
		run = run[:end]
	}
	return splitKeys(t, run, marker)
}

// bracedCommaList reads a key run written inside braces after marker, the form
// the `rmp roadmap` family help uses: `Array of objects { "name", "path", "size" }`.
func bracedCommaList(t *testing.T, help, marker string) []string {
	t.Helper()

	_, inside, opened := strings.Cut(regionAfter(t, help, marker), "{")
	if !opened {
		t.Fatalf("no opening brace after %q", marker)
	}
	inside, _, closed := strings.Cut(inside, "}")
	if !closed {
		t.Fatalf("no closing brace after %q", marker)
	}
	return splitKeys(t, inside, marker)
}

// indentedKeyRow matches one row of the two-column block `rmp sprint show
// --help` uses, where the key sits alone in the left column:
//
//	sprint_id                int
//
// The indent is pinned at exactly four spaces so that a wrapped continuation
// line, which is indented far deeper to sit under the right column, cannot be
// mistaken for a key.
var indentedKeyRow = regexp.MustCompile(`(?m)^ {4}([a-z][a-z0-9_]*) `)

// indentedKeyBlock reads the keys of the two-column block that follows marker,
// stopping at the blank line that closes it.
func indentedKeyBlock(t *testing.T, help, marker string) []string {
	t.Helper()

	block := untilBlankLine(regionAfter(t, help, marker))
	return matchedKeys(t, indentedKeyRow, block, marker, "indented key rows")
}

// jsonMemberAt builds the matcher for a JSON document sketch that writes one
// member per line at a fixed indent. Indent is what separates a top-level key
// from a nested one: in the `rmp stats` sketch the members of "sprints" and
// "tasks" are written on their own lines two spaces deeper, and counting those
// as top-level keys would make the comparison meaningless.
func jsonMemberAt(indent int) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^ {` + strconv.Itoa(indent) + `}"([a-z][a-z0-9_]*)"\s*:`)
}

var (
	jsonMemberIndent4 = jsonMemberAt(4)
	jsonMemberIndent6 = jsonMemberAt(6)
)

// jsonShapeKeys4 reads the top-level members of a JSON sketch indented four
// spaces.
func jsonShapeKeys4(t *testing.T, help, marker string) []string {
	t.Helper()

	block := untilBlankLine(regionAfter(t, help, marker))
	return matchedKeys(t, jsonMemberIndent4, block, marker, "JSON members at indent 4")
}

// jsonShapeKeys6 reads the members of a nested JSON sketch indented six spaces.
func jsonShapeKeys6(t *testing.T, help, marker string) []string {
	t.Helper()

	block := untilClosingBrace(regionAfter(t, help, marker))
	return matchedKeys(t, jsonMemberIndent6, block, marker, "JSON members at indent 6")
}

// inlineJSONMember matches a member of a JSON sketch written inline rather than
// one per line, the form `rmp audit list --help` and `rmp roadmap list --help`
// use. Position on the line carries no meaning in that form, so the key is
// recognised by its quoting and its colon alone.
var inlineJSONMember = regexp.MustCompile(`"([a-z][a-z0-9_]*)"\s*:`)

// inlineJSONKeys reads the members of the first inline JSON sketch after
// marker. The sketch is delimited by its own braces rather than by the end of
// the block, because a help that illustrates two alternative shapes writes them
// one after the other and only the first belongs to this case.
func inlineJSONKeys(t *testing.T, help, marker string) []string {
	t.Helper()

	sketch := firstBracedSketch(t, regionAfter(t, help, marker), marker)
	return dedupe(matchedKeys(t, inlineJSONMember, sketch, marker, "inline JSON members"))
}

// firstBracedSketch returns the contents of the first brace-delimited group in
// region. The sketches it reads carry no nested braces, so the first closing
// brace is the matching one.
func firstBracedSketch(t *testing.T, region, marker string) string {
	t.Helper()

	_, inside, opened := strings.Cut(region, "{")
	if !opened {
		t.Fatalf("no JSON sketch after %q", marker)
	}
	inside, _, closed := strings.Cut(inside, "}")
	if !closed {
		t.Fatalf("unterminated JSON sketch after %q", marker)
	}
	return inside
}

// untilBlankLine truncates a region at the blank line that closes it.
func untilBlankLine(region string) string {
	if end := strings.Index(region, "\n\n"); end >= 0 {
		return region[:end]
	}
	return region
}

// untilClosingBrace truncates a region at the line that closes the nested object
// it opened, so that a later sibling object's members stay out of the match.
func untilClosingBrace(region string) string {
	if end := strings.Index(region, "\n    }"); end >= 0 {
		return region[:end]
	}
	return untilBlankLine(region)
}

// matchedKeys applies a key matcher to a block, failing when it finds nothing —
// an empty match means the sketch was reformatted out from under the extractor,
// which would otherwise pass as a vacuous success.
func matchedKeys(t *testing.T, matcher *regexp.Regexp, block, marker, what string) []string {
	t.Helper()

	matches := matcher.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		t.Fatalf("no %s found after %q; the published shape was reformatted "+
			"and this extractor no longer reads it", what, marker)
	}
	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		keys = append(keys, m[1])
	}
	return keys
}

// splitKeys turns a comma-separated run into its keys, dropping the decoration a
// published list carries: surrounding quotes, a trailing "[]" marking an
// array-valued key, and the whitespace of a wrapped line.
func splitKeys(t *testing.T, run, marker string) []string {
	t.Helper()

	fields := strings.Split(run, ",")
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		key := strings.TrimSpace(field)
		key = strings.Trim(key, `"`)
		key = strings.TrimSpace(strings.TrimSuffix(key, "[]"))
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		t.Fatalf("no keys parsed from the run after %q", marker)
	}
	return keys
}

// dedupe removes repeats while preserving first-seen order. A sketch may show
// the same key twice (once per illustrated element); the published SET is what
// is being compared.
func dedupe(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	out := keys[:0:0]
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// The cases
// ---------------------------------------------------------------------------

// keyListCase is one published key list and the object it describes.
type keyListCase struct {
	// name identifies the surface in a failure message.
	name string
	// surface prints the help output that carries the list.
	surface func()
	// model is the struct whose JSON tags the list must name, exactly.
	model any
	// marker is the phrase in the help text that introduces the list.
	marker string
	// extract reads the published list back out, in the shape it is written.
	extract keyExtractor
}

// publishedKeyLists enumerates every help surface that spells out the key set of
// a struct this package can reflect over.
//
// Adding a surface that publishes a key list means adding a row. A row is cheap
// precisely because it carries no key names: it names the printer, the struct,
// the introducing phrase, and the shape the list is written in.
func publishedKeyLists() []keyListCase {
	modelSurfaces := []keyListCase{
		// --- rmp sprint --------------------------------------------------------
		{"sprint family help / Sprint object keys", printSprintHelp,
			models.Sprint{}, "Sprint object keys:", commaList},
		{"sprint family help / Comment object keys", printSprintHelp,
			models.SprintComment{}, "Comment object keys:", commaList},
		{"sprint family help / show flat object", printSprintHelp,
			models.SprintShowResult{}, "Flat object:", commaList},
		{"sprint family help / stats object", printSprintHelp,
			models.SprintStats{}, "SprintStats:", commaList},
		{"sprint show help / key block", printSprintShowHelp,
			models.SprintShowResult{}, "Flat object with these keys:", indentedKeyBlock},
		{"sprint stats help / JSON shape", printSprintStatsHelp,
			models.SprintStats{}, "Output (stdout JSON):", jsonShapeKeys4},
		{"sprint comment-list help / Keys", printSprintCommentListHelp,
			models.SprintComment{}, "Keys:", commaList},

		// --- rmp task ----------------------------------------------------------
		{"task family help / Task object keys", printTaskHelp,
			models.Task{}, "Task object keys:", commaList},
		{"task family help / Comment object keys", printTaskHelp,
			models.TaskComment{}, "Comment object keys:", commaList},
		{"task comment-list help / Keys", printTaskCommentListHelp,
			models.TaskComment{}, "Keys:", commaList},

		// --- rmp audit ---------------------------------------------------------
		{"audit family help / entry Keys", printAuditHelp,
			models.AuditEntry{}, "Keys:", commaList},
		{"audit family help / AuditStats", printAuditHelp,
			models.AuditStats{}, "AuditStats:", commaList},
		{"audit list help / entry sketch", printAuditListHelp,
			models.AuditEntry{}, "Array of audit entries.", inlineJSONKeys},
		{"audit stats help / JSON shape", printAuditStatsHelp,
			models.AuditStats{}, "Output (stdout JSON):", jsonShapeKeys4},

		// --- rmp stats ---------------------------------------------------------
		{"stats help / top-level JSON shape", printStatsHelp,
			models.RoadmapStats{}, "Output (stdout JSON):", jsonShapeKeys4},
		{"stats help / sprints sub-object", printStatsHelp,
			models.SprintStatsSummary{}, `"sprints": {`, jsonShapeKeys6},
		{"stats help / tasks sub-object", printStatsHelp,
			models.TaskStatsSummary{}, `"tasks": {`, jsonShapeKeys6},

		// --- rmp roadmap -------------------------------------------------------
		{"roadmap family help / list object", printRoadmapHelp,
			models.Roadmap{}, "Array of objects", bracedCommaList},
		{"roadmap list help / object sketch", printRoadmapListHelp,
			models.Roadmap{}, "Array of objects, one per roadmap:", inlineJSONKeys},
	}

	return slices.Concat(modelSurfaces, graphKeyLists())
}

// graphKeyLists covers the graph family, where every help surface publishes the
// same two result envelopes.
//
// Each surface introduces them with its own wording, so the phrase is given per
// surface. A read-only subcommand has no no-RETURN form to publish, which an
// empty okMarker records.
func graphKeyLists() []keyListCase {
	surfaces := []struct {
		name          string
		print         func()
		columnsMarker string
		okMarker      string
	}{
		{"graph family help", printGraphHelp,
			"Read subcommands and write subcommands with RETURN:",
			"Write subcommands without RETURN:"},
		{"graph create help", printGraphCreateHelp,
			"With a RETURN clause:", "Without a RETURN clause:"},
		{"graph query help", printGraphQueryHelp,
			"Output (stdout JSON):", ""},
		{"graph update help", printGraphUpdateHelp,
			"With a RETURN clause:", "Without a RETURN clause:"},
		{"graph delete help", printGraphDeleteHelp,
			"With a RETURN clause:", "Without a RETURN clause:"},
		{"graph search help", printGraphSearchHelp,
			"Output (stdout JSON):", ""},
	}

	cases := make([]keyListCase, 0, 2*len(surfaces))
	for _, s := range surfaces {
		cases = append(cases, keyListCase{
			name:    s.name + " / query result",
			surface: s.print,
			model:   graphQueryResult{},
			marker:  s.columnsMarker,
			extract: inlineJSONKeys,
		})
		if s.okMarker == "" {
			continue
		}
		cases = append(cases, keyListCase{
			name:    s.name + " / ok result",
			surface: s.print,
			model:   graphOKResult{},
			marker:  s.okMarker,
			extract: inlineJSONKeys,
		})
	}
	return cases
}

// TestPublishedKeyLists_MatchTheirObject is the parity guard.
//
// It fails when a published key list names a key its object does not carry, or
// omits one the object does. Adding a field to a model without publishing it on
// every surface that spells out that model's shape fails here.
func TestPublishedKeyLists_MatchTheirObject(t *testing.T) {
	for _, tc := range publishedKeyLists() {
		t.Run(tc.name, func(t *testing.T) {
			modelType := reflect.TypeOf(tc.model)
			want := jsonObjectKeys(t, modelType)
			got := tc.extract(t, captureStdout(t, tc.surface), tc.marker)

			missing, unknown := diffKeys(want, got)
			if len(missing) == 0 && len(unknown) == 0 {
				return
			}

			t.Errorf("the key list published by %s disagrees with %s\n"+
				"  published (%d): %s\n"+
				"  %s carries (%d): %s\n"+
				"  carried by the object but never published: %s\n"+
				"  published but not a key of the object: %s",
				tc.name, modelType,
				len(got), strings.Join(sortedCopy(got), ", "),
				modelType, len(want), strings.Join(sortedCopy(want), ", "),
				joinOrNone(missing), joinOrNone(unknown))
		})
	}
}

// TestPublishedKeyLists_CoverEveryHelpThatPublishesOne is the guard on the
// guard. A key list added to a help surface that no case claims would be
// unwatched, which is the state that let the sprint object drift in the first
// place.
//
// It walks every help output the registry can produce, decides whether the
// output publishes an object shape at all, and requires that output to be one a
// case already reads. Surfaces are compared by their rendered text, so a help
// that is renamed or re-registered stays matched, and one that starts publishing
// a shape does not.
func TestPublishedKeyLists_CoverEveryHelpThatPublishesOne(t *testing.T) {
	claimed := make(map[string]bool)
	for _, tc := range publishedKeyLists() {
		claimed[helpBody(captureStdout(t, tc.surface))] = true
	}

	for _, pair := range allHelpOutputs(t) {
		if !publishesAnObjectShape(pair.out) {
			continue
		}
		if claimed[helpBody(pair.out)] {
			continue
		}
		t.Errorf("the help for %q publishes an object shape that no case in "+
			"publishedKeyLists() reads, so nothing holds it in parity with the "+
			"object it describes. Add a row for it (or, when no struct backs "+
			"the shape, say so in this file's scope note).\n---\n%s---",
			pair.label, pair.out)
	}
}

// helpBody strips the AI agent banner so that a help printed on its own and the
// same help printed through the registry compare equal. The banner is prepended
// by the dispatch path, not by the printers, so it is the one difference between
// the two ways of obtaining the same page.
func helpBody(help string) string {
	return strings.TrimSpace(strings.ReplaceAll(help, AIBannerLine, ""))
}

// namedKeyList matches the phrases a help output uses to introduce a key set it
// spells out in prose.
var namedKeyList = regexp.MustCompile(`object keys:|Flat object|\bKeys:|SprintStats:|AuditStats:`)

// publishesAnObjectShape reports whether a help output spells out the keys of an
// object, as opposed to merely naming the object ("Array of task objects.").
//
// A JSON sketch counts only from its second member. The one-member envelopes —
// `{"id": <int>}` from every create, `{"name": "<name>"}` from `roadmap create`,
// `{"url": "..."}` from `web` — are emitted from bare map literals with no
// struct behind them, and a single key cannot drift out of step with a shape it
// wholly constitutes.
func publishesAnObjectShape(help string) bool {
	// Only the output section can publish a shape; a Cypher example elsewhere on
	// the page must not be mistaken for one.
	_, output, found := strings.Cut(help, "Output (stdout JSON):")
	if !found {
		return false
	}
	output = untilBlankLine(output)

	if namedKeyList.MatchString(output) {
		return true
	}
	return len(dedupe(inlineJSONMember.FindAllString(output, -1))) >= 2
}

// diffKeys compares the derived key set with the published one, returning the
// keys the object carries but the list omits, and the keys the list names but
// the object does not carry.
func diffKeys(want, got []string) (missing, unknown []string) {
	inGot := make(map[string]bool, len(got))
	for _, k := range got {
		inGot[k] = true
	}
	inWant := make(map[string]bool, len(want))
	for _, k := range want {
		inWant[k] = true
	}

	for _, k := range want {
		if !inGot[k] {
			missing = append(missing, k)
		}
	}
	for _, k := range got {
		if !inWant[k] {
			unknown = append(unknown, k)
		}
	}
	return missing, unknown
}

// sortedCopy returns keys in sorted order without disturbing the caller's slice.
// Failure messages compare two key sets, so a stable order makes the difference
// between them readable at a glance.
func sortedCopy(keys []string) []string {
	return slices.Sorted(slices.Values(keys))
}

// joinOrNone renders a key set for a failure message.
func joinOrNone(keys []string) string {
	if len(keys) == 0 {
		return "(none)"
	}
	return strings.Join(sortedCopy(keys), ", ")
}
