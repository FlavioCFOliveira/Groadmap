package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// The guards in this file cover ONE rule and its exclusions: on the card of
// either board, the priority badge writes `P` immediately followed by the task's
// priority and the severity badge writes `S` immediately followed by the task's
// severity, with no space and no separator, and no other badge in this interface
// takes a prefix (SPEC/WEB.md § Roadmap Tasks Page, Card content, item 3;
// Acceptance Criteria 85 and 133).
//
// Why the rule needs guards of its own, beyond the two boards' own card tests.
// The SPEC states it ONCE, for the card of both boards, precisely so the two
// cards cannot drift apart on it — and a rule stated once is not enforced by two
// tests that each assert a copy of it, because two copies can be changed one at a
// time and still both pass. So the checks here compare the two boards against
// EACH OTHER, the way the board layout guards compare their class-token sets
// rather than two expected lists that agree on the day they are written.
//
// The other half of the rule is the exclusion, and an exclusion is only enforced
// where it is asserted: the prefix earns its place on a card because a card
// carries no label naming either value, which is false of the task detail modal
// (its datagrid names every field it shows) and false of every status badge (its
// own text is the status name). Those are checked here too, so "only these two
// badges take a prefix" is a measured property of the served bytes rather than an
// assumption.

// ==================== ONE FORM FOR BOTH BOARDS ====================

// TestBadgePrefix_BothBoardsRenderOnePairForm is the gate for the clause that
// makes Acceptance Criteria 85 and 133 one criterion rather than two: the card of
// the sprint's member-tasks board renders the priority/severity pair EXACTLY as
// the tasks board's card does.
//
// The comparison is made between the two boards' rendered bytes, for the same
// task, so a change applied to one card and not to the other fails here whatever
// the change is — a dropped prefix, a separator, a swapped order, a different
// element. Both boards are then compared against the prefixed form built from the
// task's own priority and severity, so the two agreeing on the UNPREFIXED form
// cannot pass either.
func TestBadgePrefix_BothBoardsRenderOnePairForm(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	// Every member task of the sprint is also a task of the roadmap, so each one
	// has a card on BOTH boards: the same task, rendered twice, which is what
	// makes the two renderings comparable at all.
	tasksBoard := boardRegion(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks"))
	sprintBoard := memberBoardRegion(t, servePage(t, mux, f.path()))

	priorityVariants := map[string]bool{}
	severityVariants := map[string]bool{}

	for _, id := range sprintBoardMembers(&f) {
		task := decodeTaskDetail(t, mux, f.name, id).Task

		onTasks := cardBadgePair(t, cardMarkupOf(t, tasksBoard, id, "the tasks board"),
			id, "the tasks board")
		onSprint := cardBadgePair(t, cardMarkupOf(t, sprintBoard, id, "the sprint board"),
			id, "the sprint board")

		// One form, compared board against board.
		if onTasks != onSprint {
			t.Errorf("task #%d renders its priority/severity pair as\n  tasks board:  %s %s\n"+
				"  sprint board: %s %s\nThe rule is stated once for the card of both boards "+
				"(SPEC/WEB.md § Roadmap Tasks Page, Card content, item 3), so the two cards "+
				"render one form and a change to either is a change to both",
				id, onTasks.priority, onTasks.severity, onSprint.priority, onSprint.severity)
		}

		// And that one form is the prefixed one, built from the task's own values
		// and the mapping's own variants rather than written out here.
		want := badgePair{
			priority: `<span class="badge ` + priorityBadge(task.Priority) + `">P` +
				itoa(task.Priority) + `</span>`,
			severity: `<span class="badge ` + severityBadge(task.Severity) + `">S` +
				itoa(task.Severity) + `</span>`,
		}
		if onTasks != want {
			t.Errorf("task #%d (priority %d, severity %d) renders %s %s, want %s %s: each badge "+
				"names the value it carries with a one-letter prefix, with no space and no "+
				"separator (Acceptance Criteria 85 and 133)",
				id, task.Priority, task.Severity,
				onTasks.priority, onTasks.severity, want.priority, want.severity)
		}

		priorityVariants[priorityBadge(task.Priority)] = true
		severityVariants[severityBadge(task.Severity)] = true
	}

	// Falsifiability control. Six cards that all carried one priority band and one
	// severity band would let a card rendering a fixed pair satisfy every
	// comparison above, so the fixture is required to span more than one band on
	// each axis.
	if len(priorityVariants) < 2 || len(severityVariants) < 2 {
		t.Errorf("the fixture's member tasks span %d priority variant(s) %v and %d severity "+
			"variant(s) %v; with a single variant on either axis a card rendering one fixed "+
			"pair would satisfy this test",
			len(priorityVariants), sortedKeys(priorityVariants),
			len(severityVariants), sortedKeys(severityVariants))
	}
}

// ==================== THE COLOUR IS THE VALUE'S COLOUR ====================

// TestBadgePrefix_ColourFollowsTheValueNotThePrefixedText is the gate for the
// clause of Acceptance Criterion 85 that keeps Acceptance Criterion 61 in force
// through the change: the semantic mapping is applied to the VALUE alone and not
// to the prefixed text, so the prefix selects no colour, introduces no band, and
// changes no badge's variant (SPEC/WEB.md § Status, Priority, and Severity Badge
// Colours, rule 2).
//
// Two cases carry the weight, and the fixture is required to contain both:
//
//   - A task whose priority and severity fall in DIFFERENT bands must still get
//     the two different band colours. This is the case a mapping keyed on the
//     badge's text — "P9" and "S2" are two strings in no band at all — would fail,
//     by falling back to one variant for both.
//   - A task whose priority and severity fall in the SAME band is the case that
//     shows why the prefix exists: the two badges are then identical but for the
//     letter, and without it the card would show two same-coloured integers and
//     state nowhere which is the priority. The SPEC's own worked example, priority
//     5 and severity 3, is exactly this case.
//
// Both boards are checked, because both carry the pair.
func TestBadgePrefix_ColourFollowsTheValueNotThePrefixedText(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	boards := map[string]string{
		"the tasks board":  boardRegion(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks")),
		"the sprint board": memberBoardRegion(t, servePage(t, mux, f.path())),
	}

	var bandsDiffer, bandsAgree, tablesDiffer int

	for _, id := range sprintBoardMembers(&f) {
		task := decodeTaskDetail(t, mux, f.name, id).Task

		for where, region := range boards {
			pair := cardBadgePair(t, cardMarkupOf(t, region, id, where), id, where)

			// The class is the class the mapping assigns to the INTEGER, which the
			// helpers cannot see a prefix through: they take an int.
			if got, want := pair.priorityClasses(), "badge "+priorityBadge(task.Priority); got != want {
				t.Errorf("%s: task #%d's priority badge carries the classes %q, want %q — the "+
					"variant of the priority %d itself", where, id, got, want, task.Priority)
			}
			if got, want := pair.severityClasses(), "badge "+severityBadge(task.Severity); got != want {
				t.Errorf("%s: task #%d's severity badge carries the classes %q, want %q — the "+
					"variant of the severity %d itself", where, id, got, want, task.Severity)
			}

			// And the text is the value behind its letter, so the badge whose colour
			// was checked is the badge the reader is told the field of.
			if got, want := pair.priorityText(), "P"+itoa(task.Priority); got != want {
				t.Errorf("%s: task #%d's priority badge reads %q, want %q",
					where, id, got, want)
			}
			if got, want := pair.severityText(), "S"+itoa(task.Severity); got != want {
				t.Errorf("%s: task #%d's severity badge reads %q, want %q",
					where, id, got, want)
			}
		}

		if priorityBadge(task.Priority) != severityBadge(task.Severity) {
			bandsDiffer++
		} else {
			bandsAgree++
		}
		// A member whose severity resolves differently under the two tables, so a
		// card reading the severity through the priority table cannot pass. The
		// two tables agree on much of the 0-9 range and disagree on the rest; this
		// counts the members that fall in the part where they disagree.
		if priorityBadge(task.Severity) != severityBadge(task.Severity) {
			tablesDiffer++
		}
	}

	// The three falsifiability controls. Each names the class of defect it is the
	// only thing standing between; a fixture that stopped providing one would make
	// the corresponding assertion above vacuous without failing anything.
	if bandsDiffer == 0 {
		t.Errorf("no member task's priority and severity fall in different bands, so a card " +
			"colouring both badges from one value — or from the prefixed text, which is in no " +
			"band at all — would satisfy every assertion above")
	}
	if bandsAgree == 0 {
		t.Errorf("no member task's priority and severity share a badge variant, so the case the " +
			"prefix exists for — two identically coloured integers side by side, with nothing " +
			"but the letter to say which is the priority — is never exercised")
	}
	if tablesDiffer == 0 {
		t.Errorf("no member task's severity resolves to a different variant under the priority " +
			"table, so a card that coloured the severity badge through priorityBadge would " +
			"satisfy every assertion above")
	}
}

// ==================== THE MODAL TAKES NO PREFIX ====================

// TestBadgePrefix_TaskDetailModalRendersTheValuesBare is the gate for the
// exclusion SPEC/WEB.md § Task Detail Modal states and Acceptance Criterion 85
// repeats: the one-letter prefix belongs to the board card, and the same task's
// priority and severity in the modal render as the bare integer beside the field
// name that already names it (Acceptance Criterion 15 continues to hold).
//
// The modal is painted by /static/task-modal.js from the task detail endpoint, so
// the exclusion has two halves and both are asserted:
//
//   - the endpoint sends the raw integers, so nothing server-side formats them on
//     the way out;
//   - the script writes the field itself into the badge — the badge's text
//     argument is the bare `task.priority` expression, with no literal spliced in
//     front of it — and the datagrid item beside it carries the field's NAME,
//     which is the whole reason a prefix would state the same thing twice here.
//
// There is no browser in the Go suite and SPEC/BUILD.md rules out a JavaScript
// toolchain, so the script is read as source. That is enough for this rule: a
// prefix could only reach the modal's badge as a literal in the expression that
// builds it, and the check is that the expression is the bare field reference.
func TestBadgePrefix_TaskDetailModalRendersTheValuesBare(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := seedSprintBoardFixture(t, "settlement-platform")
	mux := buildMux()

	// The server half. The schema task carries the SPEC's own worked example,
	// priority 5 and severity 3, whose card reads P5 and S3 — so if the values
	// travelled prefixed, this is where it would show.
	status, body := fetchTaskDetail(t, mux, f.name, f.schema)
	if status != http.StatusOK {
		t.Fatalf("GET the detail of task #%d: status = %d, want 200; body=%q",
			f.schema, status, body)
	}
	// The values reach the modal as the integers they are. Decoding into the typed
	// view is the stronger half of that statement: a prefixed value is not an
	// integer and would not decode at all.
	task := decodeTaskDetail(t, mux, f.name, f.schema).Task
	if task.Priority != 5 || task.Severity != 3 {
		t.Fatalf("task #%d carries priority %d and severity %d, want the SPEC's worked example "+
			"5 and 3, which the assertions below are written against",
			f.schema, task.Priority, task.Severity)
	}
	for _, prefixed := range []string{"P5", "S3"} {
		if strings.Contains(body, prefixed) {
			t.Errorf("the task detail endpoint carries %q; the prefix is the board card's and is "+
				"applied where the card is rendered, never in the data\nbody: %s", prefixed, body)
		}
	}
	// The control that keeps the two absences above from being vacuous: the card
	// of that very task really does write those two strings.
	card := cardMarkupOf(t, boardRegion(t, servePage(t, mux, "/roadmaps/"+f.name+"/tasks")),
		f.schema, "the tasks board")
	for _, prefixed := range []string{">P5<", ">S3<"} {
		if !strings.Contains(card, prefixed) {
			t.Errorf("the card of task #%d does not render %s, so asserting the endpoint omits "+
				"the prefixed form proves nothing\ncard: %s", f.schema, prefixed, card)
		}
	}

	// The script half.
	script := stripJSComments(readEmbeddedAsset(t, "static/task-modal.js"))
	for label, field := range map[string]string{
		"Priority": "task.priority",
		"Severity": "task.severity",
	} {
		args := jsDatagridBadgeArgs(t, script, label)
		if len(args) != 3 {
			t.Fatalf("the modal's %s badge is built from el(%v), want three arguments "+
				"(tag, class, text)", label, args)
		}
		if args[2] != field {
			t.Errorf("the modal's %s badge is given the text %q, want exactly %q: the datagrid "+
				"item already writes the field's name beside the value, so the badge carries "+
				"the bare value and a prefix would state the same thing twice (SPEC/WEB.md "+
				"§ Task Detail Modal, No prefix on the modal's priority and severity badges)",
				label, args[2], field)
		}
	}

	// The modal's own status badge is filled with the bare status too. It is the
	// third badge the modal paints and the one a blanket "prefix the badges"
	// change would sweep up with the other two.
	if !strings.Contains(script, "statusEl.textContent = task.status;") {
		t.Errorf("the modal script does not set its status badge to the bare task.status; a " +
			"status badge is never ambiguous — its own text is the status name — so it takes " +
			"no prefix anywhere (SPEC/WEB.md § Roadmap Tasks Page, Card content, item 3, " +
			"Only these two badges take a prefix)")
	}
}

// ==================== AND NO OTHER BADGE ANYWHERE ====================

// TestBadgePrefix_NoOtherBadgeTakesAPrefix is the gate for the exclusive half of
// the rule: a prefix earns its place only where no label names the value, which
// is true of the board card's two badges and of no other badge in this interface
// (SPEC/WEB.md § Roadmap Tasks Page, Card content, item 3, Only these two badges
// take a prefix).
//
// The check is TOTAL rather than sampled. Every page of the interface is served,
// every badge it renders is read, and the badges are partitioned by whether they
// sit inside a board card. Each card must carry exactly its two prefixed badges,
// and no badge outside a card may carry a prefix — which covers the column count
// badges, the sprint tab count badges, the sprint status badges of the shared
// sprint card, the sprint page's header and datagrid, the comment type badges,
// and the graph sidebar's totals in one statement, with no list of them to keep
// up to date.
//
// The status badges are then checked directly as well, because they are the ones
// the SPEC singles out and the ones a reader would most expect to be prefixed by
// analogy: a badge whose text is a status name must render that name and nothing
// else, and no badge may render a letter followed by a status name.
func TestBadgePrefix_NoOtherBadgeTakesAPrefix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Two roadmaps, because no single one exercises every badge in the interface:
	// the board fixture supplies the cards and the sprint page's board, and the
	// sprint fixture supplies sprints in all three statuses, so the sprints page
	// renders a status badge of every variant the mapping defines.
	board := seedSprintBoardFixture(t, "settlement-platform")
	sprints := seedSprintFixture(t, "treasury-platform")
	mux := buildMux()

	paths := []string{
		"/",
		"/roadmaps/" + board.name,
		"/roadmaps/" + board.name + "/tasks",
		"/roadmaps/" + board.name + "/audit",
		"/roadmaps/" + board.name + "/graph",
		board.path(),
		"/roadmaps/" + sprints.name,
		"/roadmaps/" + sprints.name + "/tasks",
		"/roadmaps/" + sprints.name + "/sprints/" + itoa(sprints.openID),
		"/roadmaps/" + sprints.name + "/sprints/" + itoa(sprints.pendingID),
		"/roadmaps/" + sprints.name + "/sprints/" + itoa(sprints.closedLower),
	}

	statusNames := statusBadgeTexts()
	var (
		cardsSeen  int
		statusSeen = map[string]bool{}
	)

	for _, path := range paths {
		cards, outside := splitBoardCards(t, servePage(t, mux, path))

		// Inside a card: exactly the two prefixed badges, in that order, and no
		// third badge — the card shows no status badge, because its column already
		// states the status.
		for _, card := range cards {
			cardsSeen++
			found := pageBadges(card)
			if len(found) != 2 {
				t.Errorf("%s: a board card carries %d badges, want exactly 2 — the priority and "+
					"severity badges and no other\ncard: %s", path, len(found), card)
				continue
			}
			if !rePriorityBadgeText.MatchString(found[0].text) ||
				!reSeverityBadgeText.MatchString(found[1].text) {
				t.Errorf("%s: a board card's badges read %q and %q, want a priority badge reading "+
					"P then its value and a severity badge reading S then its value, in that "+
					"order\ncard: %s", path, found[0].text, found[1].text, card)
			}
		}

		// Outside a card: no prefix at all. Every badge here belongs to a surface
		// that names what it shows some other way.
		for _, b := range pageBadges(outside) {
			if rePrefixedBadgeText.MatchString(b.text) {
				t.Errorf("%s: a badge outside a board card reads %q; the one-letter prefix "+
					"belongs to the card, which carries no label naming either value, and to "+
					"nothing else in this interface", path, b.text)
			}
			// A status badge names itself: its text is the status, whole, with
			// nothing in front of it.
			if statusNames[b.text] {
				statusSeen[b.text] = true
				continue
			}
			for status := range statusNames {
				if len(b.text) == len(status)+1 && strings.HasSuffix(b.text, status) &&
					isASCIILetter(b.text[0]) {
					t.Errorf("%s: a badge reads %q, a one-letter prefix in front of the status "+
						"%q; a status badge is never ambiguous, because its own text is the "+
						"status name, so it takes no prefix wherever it is shown",
						path, b.text, status)
				}
			}
		}
	}

	// Falsifiability controls. Both halves of the partition must have had
	// something in them: a page set that rendered no card, or no status badge,
	// would satisfy every assertion above by rendering nothing to check.
	if cardsSeen == 0 {
		t.Errorf("the pages served rendered no board card at all, so asserting what a card "+
			"carries proves nothing (paths: %v)", paths)
	}
	for _, status := range models.ValidSprintStatuses {
		if !statusSeen[string(status)] {
			t.Errorf("no page rendered a badge carrying the sprint status %s, so asserting that "+
				"a status badge takes no prefix is vacuous for it", status)
		}
	}
}

// ==================== HELPERS ====================

// sprintBoardMembers returns the ids of the sprint fixture's member tasks, which
// are the tasks that have a card on BOTH boards and are therefore the ones the
// two renderings can be compared over.
func sprintBoardMembers(f *sprintBoardFixture) []int {
	columns := f.wantColumns()
	members := make([]int, 0, len(columns)*3)
	for _, column := range columns {
		members = append(members, column...)
	}
	return members
}

// badge is one rendered badge element: its full markup, the value of its class
// attribute, and the text it shows.
type badge struct {
	markup  string
	classes string
	text    string
}

// badgePair is the priority/severity pair of one board card, as the two elements
// the page actually renders, so two cards can be compared byte for byte.
type badgePair struct {
	priority string
	severity string
}

func (p badgePair) priorityClasses() string { return badgeClassesOf(p.priority) }
func (p badgePair) severityClasses() string { return badgeClassesOf(p.severity) }
func (p badgePair) priorityText() string    { return badgeTextOf(p.priority) }
func (p badgePair) severityText() string    { return badgeTextOf(p.severity) }

func badgeClassesOf(markup string) string {
	if m := classAttrRe.FindStringSubmatch(markup); m != nil {
		return m[1]
	}
	return ""
}

func badgeTextOf(markup string) string {
	if m := reLeafSpan.FindStringSubmatch(markup); m != nil {
		return m[2]
	}
	return ""
}

// reLeafSpan matches a <span> holding text and no element of its own, which is
// what every badge in this interface is. An outer span whose first child is an
// element cannot match — the text group admits no "<" — so nesting never confuses
// the scan and a card body is never mistaken for a badge.
var reLeafSpan = regexp.MustCompile(`<span\b([^>]*)>([^<]*)</span>`)

// rePriorityBadgeText, reSeverityBadgeText and rePrefixedBadgeText recognise the
// prefixed form. The value is an integer in 0-9 (MODELS.md § Task), and the
// pattern admits no space and no separator between the letter and the digits,
// which is the form Acceptance Criteria 85 and 133 fix.
var (
	rePriorityBadgeText = regexp.MustCompile(`^P[0-9]+$`)
	reSeverityBadgeText = regexp.MustCompile(`^S[0-9]+$`)
	rePrefixedBadgeText = regexp.MustCompile(`^[PS][0-9]+$`)
)

// pageBadges returns every badge element of a markup region, in document order.
func pageBadges(markup string) []badge {
	matches := reLeafSpan.FindAllStringSubmatch(markup, -1)
	badges := make([]badge, 0, len(matches))
	for _, m := range matches {
		attrs := classAttrRe.FindStringSubmatch(m[1])
		if attrs == nil || !hasClassToken(attrs[1], "badge") {
			continue
		}
		badges = append(badges, badge{markup: m[0], classes: attrs[1], text: m[2]})
	}
	return badges
}

// hasClassToken reports whether a class attribute carries a class as a whole
// token, so "labels-sidebar__total badge bg-secondary-lt" counts as a badge and a
// hypothetical "badge-like" class would not.
func hasClassToken(classes, want string) bool {
	for _, token := range strings.Fields(classes) {
		if token == want {
			return true
		}
	}
	return false
}

// cardMarkupOf returns the markup of ONE task's board card, bounded by the card's
// own closing tag.
//
// The bound matters: a card is the last element of its column, so a slice ending
// at the NEXT card would run through the column's closing markup and into the
// following column's header, whose count badge would then be read as part of the
// card.
func cardMarkupOf(t *testing.T, region string, taskID int, where string) string {
	t.Helper()

	const cardClose = "</button>"

	at := strings.Index(region, cardMarker(taskID))
	if at < 0 {
		t.Fatalf("%s renders no card for task #%d", where, taskID)
	}
	start := strings.LastIndex(region[:at], cardOpen)
	if start < 0 {
		t.Fatalf("%s: task #%d's card does not open with the Tabler card markup %s",
			where, taskID, cardOpen)
	}
	end := strings.Index(region[start:], cardClose)
	if end < 0 {
		t.Fatalf("%s: task #%d's card is never closed", where, taskID)
	}
	return region[start : start+end+len(cardClose)]
}

// cardBadgePair returns a card's two badges, in the order the card renders them.
// A card carrying any other number of badges is a failure in itself: the card
// shows a priority badge and a severity badge and no status badge at all
// (Acceptance Criteria 85 and 133).
func cardBadgePair(t *testing.T, card string, taskID int, where string) badgePair {
	t.Helper()

	found := pageBadges(card)
	if len(found) != 2 {
		t.Fatalf("%s: the card of task #%d carries %d badges, want exactly 2 — its priority "+
			"badge and its severity badge, and no status badge\ncard: %s",
			where, taskID, len(found), card)
	}
	return badgePair{priority: found[0].markup, severity: found[1].markup}
}

// splitBoardCards partitions a served page into the markup of every board card,
// in document order, and everything the page renders outside those cards.
//
// The split is what lets "only the card's two badges take a prefix" be asserted
// over the whole interface at once, rather than against a list of the surfaces
// that must not take one — a list that would silently stop covering a surface
// added later.
func splitBoardCards(t *testing.T, page string) ([]string, string) {
	t.Helper()

	const cardClose = "</button>"

	var (
		cards   []string
		outside strings.Builder
	)
	remaining := page
	for {
		at := strings.Index(remaining, cardOpen)
		if at < 0 {
			outside.WriteString(remaining)
			return cards, outside.String()
		}
		end := strings.Index(remaining[at:], cardClose)
		if end < 0 {
			t.Fatalf("a board card opens and is never closed; the page cannot be partitioned")
		}
		outside.WriteString(remaining[:at])
		cards = append(cards, remaining[at:at+end+len(cardClose)])
		remaining = remaining[at+end+len(cardClose):]
	}
}

// statusBadgeTexts returns every text a status badge may carry: the task status
// enum and the sprint status enum, which SPEC/WEB.md § Status, Priority, and
// Severity Badge Colours takes from MODELS.md § Enums rather than defining.
func statusBadgeTexts() map[string]bool {
	texts := make(map[string]bool, len(models.ValidTaskStatuses)+len(models.ValidSprintStatuses))
	for _, status := range models.ValidTaskStatuses {
		texts[string(status)] = true
	}
	for _, status := range models.ValidSprintStatuses {
		texts[string(status)] = true
	}
	return texts
}

// isASCIILetter reports whether a byte is an unaccented Latin letter, which is
// what a one-letter badge prefix is.
func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// jsDatagridBadgeArgs returns the arguments of the el(...) call that builds the
// badge of one named datagrid item in the modal script, so what the badge is
// given as its text can be read rather than guessed from the surrounding source.
func jsDatagridBadgeArgs(t *testing.T, script, label string) []string {
	t.Helper()

	open := regexp.MustCompile(`datagridItem\(\s*"` + label + `"\s*,\s*el\(`)
	loc := open.FindStringIndex(script)
	if loc == nil {
		t.Fatalf("the modal script builds no %q datagrid item holding a badge; either the "+
			"extraction is broken or the modal stopped naming the field beside the value, "+
			"which is the reason it carries no prefix", label)
	}
	return jsCallArguments(t, script[loc[1]:], label)
}

// jsCallArguments splits the top-level arguments of a JavaScript call whose
// opening parenthesis has already been consumed, ignoring commas nested inside
// parentheses, brackets, braces, and string literals.
func jsCallArguments(t *testing.T, rest, where string) []string {
	t.Helper()

	var (
		args  []string
		arg   strings.Builder
		depth = 1
		quote byte
	)
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if quote != 0 {
			arg.WriteByte(c)
			if c == '\\' && i+1 < len(rest) {
				i++
				arg.WriteByte(rest[i])
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
			arg.WriteByte(c)
		case '(', '[', '{':
			depth++
			arg.WriteByte(c)
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return append(args, strings.TrimSpace(arg.String()))
			}
			arg.WriteByte(c)
		case ',':
			if depth == 1 {
				args = append(args, strings.TrimSpace(arg.String()))
				arg.Reset()
				continue
			}
			arg.WriteByte(c)
		default:
			arg.WriteByte(c)
		}
	}
	t.Fatalf("the call built for %q is never closed in the modal script", where)
	return nil
}
