package web

import (
	"html"
	"regexp"
	"strings"
	"testing"
)

// This file is the served-HTML half of Acceptance Criterion 295: every
// table-responsive container the templates render is a named, focusable region
// (SPEC/WEB.md § UI Framework, rules 21 and 22). The browser half — Tab reaching
// the container, its accessible name, and the Right Arrow key scrolling it — and
// the contrast of its focus indicator, gated from the stylesheets by
// TestFocusIndicatorContrast, are outside this file.

// openTagRe captures the name and the attribute text of every opening tag.
var openTagRe = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)\b([^>]*)>`)

// attrRe captures one double-quoted attribute and its value.
var attrRe = regexp.MustCompile(`([a-zA-Z_:][-a-zA-Z0-9_:.]*)="([^"]*)"`)

// anyTagRe matches any tag, so an element's text can be read without its
// markup. Every id attribute of a page is read with markdown_test.go's idAttrRe.
var anyTagRe = regexp.MustCompile(`<[^>]*>`)

// openTag is one opening tag of a served page: its offset, its element name, and
// its attributes.
type openTag struct {
	at    int
	end   int
	name  string
	attrs map[string]string
}

// openTags returns every opening tag of body, in document order.
func openTags(body string) []openTag {
	matches := openTagRe.FindAllStringSubmatchIndex(body, -1)
	tags := make([]openTag, 0, len(matches))
	for _, m := range matches {
		attrs := map[string]string{}
		for _, a := range attrRe.FindAllStringSubmatch(body[m[4]:m[5]], -1) {
			attrs[a[1]] = html.UnescapeString(a[2])
		}
		tags = append(tags, openTag{at: m[0], end: m[1], name: body[m[2]:m[3]], attrs: attrs})
	}
	return tags
}

// hasClass reports whether the tag's class attribute carries class.
func (o openTag) hasClass(class string) bool {
	for _, c := range strings.Fields(o.attrs["class"]) {
		if c == class {
			return true
		}
	}
	return false
}

// assertTableRegions asserts the AC 295 contract on one served page: it renders
// exactly the table-responsive containers whose card titles are wantTitles, in
// that order; each carries tabindex="0", role="region", and an aria-labelledby
// naming the id of exactly one element, the card-title of the same card, whose
// text is that card's title; and no two elements of the page share an id.
func assertTableRegions(t *testing.T, page, body string, wantTitles []string) {
	t.Helper()

	ids := map[string]int{}
	for _, m := range idAttrRe.FindAllStringSubmatch(body, -1) {
		ids[m[1]]++
	}
	for id, n := range ids {
		if n > 1 {
			t.Errorf("%s: %d elements share the id %q", page, n, id)
		}
	}

	tags := openTags(body)
	byID := map[string]openTag{}
	for _, tag := range tags {
		if id, ok := tag.attrs["id"]; ok {
			byID[id] = tag
		}
	}

	var titles []string
	for i, tag := range tags {
		if !tag.hasClass("table-responsive") {
			continue
		}
		if got := tag.attrs["tabindex"]; got != "0" {
			t.Errorf("%s: a table-responsive container carries tabindex %q, want \"0\"", page, got)
		}
		if got := tag.attrs["role"]; got != "region" {
			t.Errorf("%s: a table-responsive container carries role %q, want \"region\"", page, got)
		}
		ref, ok := tag.attrs["aria-labelledby"]
		if !ok || ref == "" {
			t.Errorf("%s: a table-responsive container carries no aria-labelledby", page)
			continue
		}
		if ids[ref] != 1 {
			t.Errorf("%s: aria-labelledby %q names %d elements, want exactly 1", page, ref, ids[ref])
			continue
		}
		label := byID[ref]
		if !label.hasClass("card-title") {
			t.Errorf("%s: aria-labelledby %q names a %s carrying %q, not a card-title", page, ref, label.name, label.attrs["class"])
			continue
		}

		// The card-title belongs to the same card: the nearest card opened before
		// the container is also the nearest card opened before the title.
		card := -1
		for j := i - 1; j >= 0; j-- {
			if tags[j].hasClass("card") {
				card = tags[j].at
				break
			}
		}
		if card < 0 || label.at < card || label.at > tag.at {
			t.Errorf("%s: the card-title %q is not the title of the container's own card", page, ref)
			continue
		}

		closing := strings.Index(body[label.end:], "</"+label.name+">")
		if closing < 0 {
			t.Fatalf("%s: the card-title %q is never closed", page, ref)
		}
		text := strings.Join(strings.Fields(html.UnescapeString(anyTagRe.ReplaceAllString(body[label.end:label.end+closing], ""))), " ")
		titles = append(titles, text)
	}

	if strings.Join(titles, " | ") != strings.Join(wantTitles, " | ") {
		t.Errorf("%s: the table-responsive containers are named %q, want %q", page, titles, wantTitles)
	}
}

// TestScrollableTables_AreNamedFocusableRegions is Acceptance Criterion 295 on
// the served HTML of the three pages that render a table-responsive container:
// the audit log page with entries, the dashboard with rows in each of its four
// card-tables, and the tasks page with a task.
func TestScrollableTables_AreNamedFocusableRegions(t *testing.T) {
	t.Setenv("HOME", shortHome(t))

	f := seedDashFixture(t, "settlement-platform")
	seedRoadmapWithAudit(t, f.name, 3)
	mux := buildMux()

	assertTableRegions(t, "the audit log page", servePage(t, mux, "/roadmaps/"+f.name+"/audit"),
		[]string{"Audit log"})
	assertTableRegions(t, "the dashboard", dashboardBody(t, f.name, dashNow),
		[]string{"Work in progress", "Highest-severity open tasks", "Upcoming sprints", "Recent activity"})
	assertTableRegions(t, "the tasks page", servePage(t, mux, "/roadmaps/"+f.name+"/tasks"),
		[]string{"Task list"})
}

// TestScrollableTables_EmptyCardsRenderNoRegion asserts the other half of the
// criterion: a card that renders its empty state in place of its table renders
// no container, and therefore no focusable region. An empty roadmap's dashboard
// shows the empty state in every card-table.
func TestScrollableTables_EmptyCardsRenderNoRegion(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	openDashRoadmap(t, "empty-ledger")
	body := dashboardBody(t, "empty-ledger", dashNow)
	if !strings.Contains(body, `<p class="empty-title">No task in progress</p>`) {
		t.Fatalf("the empty roadmap's dashboard shows no empty state; the absence below would be vacuous")
	}
	assertTableRegions(t, "the empty dashboard", body, nil)
}
