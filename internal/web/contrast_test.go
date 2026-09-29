package web

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file is the gate for the dark-theme contrast of the tasks list and the
// sprint board, computed from the stylesheets the server serves by the steps of
// css_cascade_test.go (SPEC/WEB.md § Status, Priority, and Severity Badge
// Colours, rule 5; § UI Framework, rule 21; § Roadmap Tasks Page, Labels are
// programmatic, not visible, and The active size is not shown by colour alone;
// Acceptance Criteria 245, 246, and 247).

// contrastWidths are the viewport widths the rules are resolved at, so a colour
// declared inside a media query is judged at every breakpoint the criteria name.
var contrastWidths = []float64{375, 576, 992, 1440}

// servedStylesheets are the two stylesheets the contrast criteria read, in the
// order every page loads them.
var servedStylesheets = []string{"/static/vendor/tabler/tabler.min.css", "/static/style.css"}

// servedCascadeRules fetches the two stylesheets through the server and parses
// them, in load order, into one list of rules.
func servedCascadeRules(t *testing.T) []cascadeRule {
	t.Helper()
	mux := buildMux()
	var rules []cascadeRule
	for _, path := range servedStylesheets {
		before := len(rules)
		var err error
		if rules, err = parseCascadeSheet(servePage(t, mux, path), rules); err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		// Falsifiability: a sheet read as empty would pass every ratio vacuously.
		if len(rules)-before < 20 {
			t.Fatalf("%s parsed to %d rules; the ratios over it would be vacuous", path, len(rules)-before)
		}
	}
	return rules
}

// htmlDark is the root element of every page.
const htmlDark = "html[data-bs-theme=dark][lang=en]"

// pagePath is the admin-shell path down to the page body's container.
var pagePath = []string{htmlDark, "body.layout-fluid", "div.page", "div.page-wrapper", "main.page-body", "div.container-xl"}

// under returns pagePath followed by the given descriptions.
func under(path ...string) []string {
	return append(append([]string(nil), pagePath...), path...)
}

// badgeContexts are the places a badge is rendered on the tasks list and on the
// sprint board: a cell of the list, the badge line of a board card, the count
// badge of a board column's heading, and the count badge of a Comments card.
var badgeContexts = map[string][]string{
	"tasks list cell": under("div.card", "div.table-responsive", "table.table.table-vcenter.card-table", "tbody", "tr", "td"),
	"board card": under("div.task-board.task-board--bounded.mb-3", "div.card.task-board__column",
		"div.card-body.task-board__cards", "a.card.card-sm.card-link.text-reset.task-card[href=/roadmaps/payments/tasks/7]",
		"span.card-body.d-block", "span.d-flex.flex-wrap.align-items-center.justify-content-between.gap-1",
		"span.d-flex.flex-wrap.gap-1"),
	"board column heading": under("div.task-board.task-board--bounded.mb-3", "div.card.task-board__column",
		"div.card-header", "h3.card-title"),
	"comments card heading": under("div.card", "div.card-header", "h3.card-title"),
}

// badgeVariant is one badge variant the interface renders: its classes, and the
// values the mapping tables give it, for messages.
type badgeVariant struct {
	classes string
	values  []string
}

// renderedBadgeVariants derives every variant the interface renders from the
// mapping helpers themselves — every task status, sprint status, task type,
// priority, and severity value — plus the neutral bg-secondary-lt and the id
// badge's bg-black text-white, so a variant the mapping gains is judged too
// (SPEC/WEB.md § Status, Priority, and Severity Badge Colours, rule 5).
func renderedBadgeVariants() []badgeVariant {
	byClass := map[string][]string{}
	add := func(class, value string) { byClass[class] = append(byClass[class], value) }
	for _, s := range models.ValidTaskStatuses {
		add(taskStatusBadge(s), string(s))
	}
	for _, s := range []models.SprintStatus{models.SprintPending, models.SprintOpen, models.SprintClosed} {
		add(sprintStatusBadge(s), "sprint "+string(s))
	}
	for _, tt := range models.ValidTaskTypes {
		add(taskTypeBadge(tt), string(tt))
	}
	for v := 0; v <= 9; v++ {
		add(priorityBadge(v), fmt.Sprintf("P%d", v))
		add(severityBadge(v), fmt.Sprintf("S%d", v))
	}
	add(badgeSecondary, "comment type / neutral count")
	add("bg-black text-white", "id badge")
	variants := make([]badgeVariant, 0, len(byClass))
	for class, values := range byClass {
		variants = append(variants, badgeVariant{classes: class, values: values})
	}
	sort.Slice(variants, func(i, j int) bool { return variants[i].classes < variants[j].classes })
	return variants
}

// badgeElement describes a badge of the given variant classes in a context.
func badgeElement(context []string, classes string) *cssElement {
	return describe(append(append([]string(nil), context...), "span.badge."+strings.ReplaceAll(classes, " ", "."))...)
}

// textContrast is the ratio of a translucent text colour to a translucent fill,
// the fill composited over the surface and the text over the result.
func textContrast(text, fill, surface rgba) float64 {
	background := over(fill, surface)
	return rgbaContrast(over(text, background), background)
}

// TestBadgeContrast_EveryVariantReadsAtFourAndAHalf is the gate for Acceptance
// Criterion 246's badges: for every badge variant the interface renders, in every
// context of the tasks list and the sprint board, at every viewport width, the
// winning color and background-color resolved from the served stylesheets give
// a text contrast of at least 4.5:1 over --tblr-bg-surface and over
// --tblr-body-bg. A value the resolver cannot resolve fails the variant.
func TestBadgeContrast_EveryVariantReadsAtFourAndAHalf(t *testing.T) {
	rules := servedCascadeRules(t)
	variants := renderedBadgeVariants()
	if len(variants) < 13 {
		t.Fatalf("the mapping helpers yield %d variants; the mapping tables assign 12 plus the id badge", len(variants))
	}

	for _, v := range variants {
		worst, worstAt := math.Inf(1), ""
		for _, width := range contrastWidths {
			c := newCSSCascade(rules, width)
			for name, context := range badgeContexts {
				el := badgeElement(context, v.classes)
				text, err := c.colour(el, "", "color")
				if err != nil {
					t.Fatalf("%s (%v): color: %v", v.classes, v.values, err)
				}
				fill, err := c.colour(el, "", "background-color")
				if err != nil {
					t.Fatalf("%s (%v): background-color: %v", v.classes, v.values, err)
				}
				for _, surfaceName := range []string{"--tblr-bg-surface", "--tblr-body-bg"} {
					surface, err := c.customColour(el, surfaceName)
					if err != nil {
						t.Fatalf("%s: %s: %v", v.classes, surfaceName, err)
					}
					if surface.a != 1 {
						t.Fatalf("%s resolves to a translucent %s", surfaceName, surface.hex())
					}
					ratio := textContrast(text, fill, surface)
					if ratio < worst {
						worst = ratio
						worstAt = fmt.Sprintf("%s over %s at %.0fpx: text %s, fill %s", name, surfaceName, width, text.hex(), fill.hex())
					}
				}
			}
		}
		t.Logf("%-22s %5.2f:1 (lowest: %s) %v", v.classes, worst, worstAt, v.values)
		if worst < 4.5 {
			t.Errorf("the %s badge (%v) reads at %.2f:1, below 4.5:1: %s", v.classes, v.values, worst, worstAt)
		}
	}
}

// filterBarPath is the path from the page body down to a filter-bar control's
// column in the tasks list card's header.
var filterBarPath = under("div.card", "div.card-header.flex-wrap.gap-2", "div.card-actions",
	"form.row.g-2.align-items-end.justify-content-end[method=get][action=/roadmaps/payments/tasks]",
	"div.col-12.col-sm-auto")

// footerPath is the path from the page body down to the tasks list card's footer
// row.
var footerPath = under("div.card", "div.card-footer", "div.row.g-2.align-items-center")

// sizeLink describes a rows-per-page link, active or not, with optional states.
func sizeLink(active bool, states string) *cssElement {
	link := "a.btn.btn-sm[href=/roadmaps/payments/tasks?size=50]"
	if active {
		link = "a.btn.btn-sm.active[href=/roadmaps/payments/tasks?size=50][aria-current=true]"
	}
	return describe(append(append([]string(nil), footerPath...), "div.col-auto",
		"div.btn-group[role=group][aria-labelledby=task-list-size-label]", link+states)...)
}

// TestSearchPlaceholderContrast is the gate for the placeholder half of
// Acceptance Criterion 246: the colour of the winning ::placeholder declaration
// for the tasks page's search input — an input carrying form-control and
// form-control-sm — reads at 4.5:1 or more against that input's background
// composited over --tblr-bg-surface, the card surface it sits on, at every
// viewport width. The placeholder is the input's only visible label, so its text
// is judged as text (SPEC/WEB.md § Roadmap Tasks Page, Labels are programmatic,
// not visible).
func TestSearchPlaceholderContrast(t *testing.T) {
	rules := servedCascadeRules(t)
	input := describe(append(append([]string(nil), filterBarPath...),
		"input.form-control.form-control-sm#task-filter-q[type=search][name=q][placeholder=Search]:placeholder-shown")...)
	for _, width := range contrastWidths {
		c := newCSSCascade(rules, width)
		text, err := c.colour(input, "placeholder", "color")
		if err != nil {
			t.Fatalf("the search placeholder colour: %v", err)
		}
		fill, err := c.colour(input, "", "background-color")
		if err != nil {
			t.Fatalf("the search input background: %v", err)
		}
		surface, err := c.customColour(input, "--tblr-bg-surface")
		if err != nil {
			t.Fatalf("--tblr-bg-surface: %v", err)
		}
		ratio := textContrast(text, fill, surface)
		if width == 1440 {
			t.Logf("search placeholder %.2f:1 (text %s on %s over %s)", ratio, text.hex(), fill.hex(), surface.hex())
		}
		if ratio < 4.5 {
			t.Errorf("at %.0fpx the search placeholder %s reads at %.2f:1 on the input's %s over %s, below 4.5:1",
				width, text.hex(), ratio, fill.hex(), surface.hex())
		}
	}
}

// TestRowsPerPageActiveFill is the gate for Acceptance Criterion 245, resolved
// from the served stylesheets by the steps of Acceptance Criterion 246: the
// active rows-per-page link's background is opaque and differs from an inactive
// link's; that fill reaches 3:1 against the card footer's background and against
// an inactive link's composited fill; and the active link's text reaches 4.5:1
// against the fill. The served markup's active and aria-current="true" on the
// active link alone are gated by TestTaskList_PaginationBarAndSizeSelector.
func TestRowsPerPageActiveFill(t *testing.T) {
	rules := servedCascadeRules(t)
	active, inactive := sizeLink(true, ""), sizeLink(false, "")
	footer := active.parent.parent.parent.parent
	if !footer.hasClass("card-footer") {
		t.Fatalf("the described path does not reach the card footer: %v", footer.classes)
	}
	for _, width := range contrastWidths {
		c := newCSSCascade(rules, width)
		fill, err := c.colour(active, "", "background-color")
		if err != nil {
			t.Fatalf("the active link's background: %v", err)
		}
		text, err := c.colour(active, "", "color")
		if err != nil {
			t.Fatalf("the active link's colour: %v", err)
		}
		inactiveBg, err := c.colour(inactive, "", "background-color")
		if err != nil {
			t.Fatalf("an inactive link's background: %v", err)
		}
		footerOwn, err := c.colour(footer, "", "background-color")
		if err != nil {
			t.Fatalf("the card footer's background: %v", err)
		}
		footerBase, err := c.backdrop(footer)
		if err != nil {
			t.Fatalf("the card footer's backdrop: %v", err)
		}
		inactiveBase, err := c.backdrop(inactive)
		if err != nil {
			t.Fatalf("an inactive link's backdrop: %v", err)
		}
		footerBg, inactiveFill := over(footerOwn, footerBase), over(inactiveBg, inactiveBase)

		if fill.a != 1 {
			t.Errorf("at %.0fpx the active link's fill %s is not opaque", width, fill.hex())
			continue
		}
		if fill == inactiveBg {
			t.Errorf("at %.0fpx the active and inactive links share the background %s", width, fill.hex())
		}
		againstFooter, againstInactive := rgbaContrast(fill, footerBg), rgbaContrast(fill, inactiveFill)
		textRatio := rgbaContrast(over(text, fill), fill)
		if width == 1440 {
			t.Logf("active fill %s: %.2f:1 vs footer %s, %.2f:1 vs inactive %s; text %s %.2f:1",
				fill.hex(), againstFooter, footerBg.hex(), againstInactive, inactiveFill.hex(), text.hex(), textRatio)
		}
		if againstFooter < 3 || againstInactive < 3 || textRatio < 4.5 {
			t.Errorf("at %.0fpx the active fill %s reaches %.2f:1 against the footer %s and %.2f:1 against an inactive link %s "+
				"(want 3:1 each), and its text %s %.2f:1 (want 4.5:1)",
				width, fill.hex(), againstFooter, footerBg.hex(), againstInactive, inactiveFill.hex(), text.hex(), textRatio)
		}
	}
}

// focusTarget is one kind of element UI Framework rule 21 names on the tasks page
// and the sprint board, described with keyboard focus.
type focusTarget struct {
	name string
	el   *cssElement
}

// focusTargets describes every kind of focusable element of the tasks list and
// the sprint board, each in the state keyboard focus gives it: :focus and
// :focus-visible.
func focusTargets() []focusTarget {
	const kb = ":focus:focus-visible"
	control := func(leaf string) *cssElement {
		return describe(append(append([]string(nil), filterBarPath...), leaf+kb)...)
	}
	row := under("div.card", "div.table-responsive", "table.table.table-vcenter.card-table", "tbody", "tr")
	board := under("div.task-board.task-board--bounded.mb-3", "div.card.task-board__column")
	return []focusTarget{
		{"search input", control("input.form-control.form-control-sm#task-filter-q[type=search][name=q][placeholder=Search]:placeholder-shown")},
		{"sprint select", control("select.form-select.form-select-sm.task-list__sprint-select#task-filter-sprint[name=sprint]")},
		{"status select", control("select.form-select.form-select-sm#task-filter-status[name=status]")},
		{"Apply button", control("button.btn.btn-primary.btn-sm[type=submit]")},
		{"row title link", describe(append(row, "td.task-list__title", "a[href=/roadmaps/payments/tasks/7]"+kb)...)},
		{"pagination link", describe(append(append([]string(nil), footerPath...), "div.col-auto.ms-auto",
			"nav[aria-label=Task list pages]", "ul.pagination.m-0", "li.page-item", "a.page-link[href=/roadmaps/payments/tasks?page=2]"+kb)...)},
		{"active rows-per-page link", sizeLink(true, kb)},
		{"inactive rows-per-page link", sizeLink(false, kb)},
		{"empty-state Reset link", describe(under("div.card", "div.card-body", "div.empty", "div.empty-action",
			"a.btn[href=/roadmaps/payments/tasks]"+kb)...)},
		{"board card", describe(append(board, "div.card-body.task-board__cards",
			"a.card.card-sm.card-link.text-reset.task-card[href=/roadmaps/payments/tasks/7]"+kb)...)},
		{"column toggle", describe(append(board, "div.card-header", "div.card-actions",
			"button.btn-action[type=button][aria-expanded=true]"+kb)...)},
	}
}

// TestFocusIndicatorContrast is the gate for Acceptance Criterion 247, resolved
// from the served stylesheets by the steps of Acceptance Criterion 246: for every
// kind of focusable element of the tasks list and the sprint board, in the state
// keyboard focus gives it, the winning outline is solid and at least 2px wide,
// and its colour reaches 3:1 against the element's own background and against
// its container's — the page background with every ancestor's background
// composited over it — at every viewport width (SPEC/WEB.md § UI Framework,
// rule 21).
func TestFocusIndicatorContrast(t *testing.T) {
	rules := servedCascadeRules(t)
	for _, target := range focusTargets() {
		for _, width := range contrastWidths {
			c := newCSSCascade(rules, width)
			style, _, err := c.resolved(target.el, "", "outline-style")
			if err != nil {
				t.Fatalf("%s: outline-style: %v", target.name, err)
			}
			widthValue, _, err := c.resolved(target.el, "", "outline-width")
			if err != nil {
				t.Fatalf("%s: outline-width: %v", target.name, err)
			}
			px, err := cssLengthPx(widthValue)
			if style != "solid" || err != nil || px < 2 {
				t.Errorf("at %.0fpx the %s's focus outline is %q %q, want solid and at least 2px", width, target.name, style, widthValue)
				continue
			}
			outline, err := c.colour(target.el, "", "outline-color")
			if err != nil {
				t.Fatalf("%s: outline-color: %v", target.name, err)
			}
			container, err := c.backdrop(target.el)
			if err != nil {
				t.Fatalf("%s: container background: %v", target.name, err)
			}
			own, err := c.colour(target.el, "", "background-color")
			if err != nil {
				t.Fatalf("%s: background-color: %v", target.name, err)
			}
			element := over(own, container)
			vsContainer := rgbaContrast(over(outline, container), container)
			vsElement := rgbaContrast(over(outline, element), element)
			if width == 1440 {
				t.Logf("%-28s outline %s: %.2f:1 vs container %s, %.2f:1 vs element %s",
					target.name, outline.hex(), vsContainer, container.hex(), vsElement, element.hex())
			}
			if vsContainer < 3 || vsElement < 3 {
				t.Errorf("at %.0fpx the %s's focus outline %s reaches %.2f:1 against its container %s and %.2f:1 against "+
					"its own background %s; want 3:1 against each", width, target.name, outline.hex(),
					vsContainer, container.hex(), vsElement, element.hex())
			}
		}
	}
}

// TestCascadeResolver_ConvertsFaithfullyAndRefusesWhatItCannotResolve proves the
// resolver the contrast gates stand on. Its colour conversion reproduces the sRGB
// triples the vendored stylesheet itself declares for its primary and secondary
// colours; it composites by source-over; its cascade ranks importance above
// specificity above order; and it fails — rather than measuring — a value outside
// the steps of Acceptance Criterion 246: a two-colour color-mix(), relative
// colour syntax, an undeclared var() with no fallback, an unknown colour name, an
// unknown pseudo-class, and an unknown at-rule. Last, it shows the gates can
// fail: against the vendored stylesheet alone, without the project overrides,
// the DOING badge, the active rows-per-page fill, and the board card's focus
// outline fall short.
func TestCascadeResolver_ConvertsFaithfullyAndRefusesWhatItCannotResolve(t *testing.T) {
	rules := servedCascadeRules(t)
	c := newCSSCascade(rules, 1440)
	root := describe(htmlDark)
	// The first declaration of each colour in the vendored sheet is its literal
	// OKLCH value, and the sheet declares the sRGB triple of that same colour.
	firstDecl := func(name string) string {
		for _, rule := range rules {
			for _, d := range rule.decls {
				if d.prop == name {
					return d.value
				}
			}
		}
		t.Fatalf("the served stylesheets never declare %s", name)
		return ""
	}
	for _, pair := range [][2]string{{"--tblr-primary", "--tblr-primary-rgb"}, {"--tblr-secondary", "--tblr-secondary-rgb"}} {
		got, err := parseColour(firstDecl(pair[0]), nil)
		if err != nil {
			t.Fatalf("%s: %v", pair[0], err)
		}
		want, err := parseColour("rgb("+firstDecl(pair[1])+")", nil)
		if err != nil {
			t.Fatalf("%s: %v", pair[1], err)
		}
		if got.hex() != want.hex() {
			t.Errorf("%s = %s converts to %s, but the stylesheet declares %s = %s",
				pair[0], firstDecl(pair[0]), got.hex(), pair[1], want.hex())
		}
	}
	if got := over(rgba{1, 1, 1, 0.5}, rgba{0, 0, 0, 1}); math.Abs(got.r-0.5) > 1e-9 || got.a != 1 {
		t.Errorf("white at 50%% over black composites to %+v, want 0.5 grey", got)
	}
	if got := rgbaContrast(rgba{1, 1, 1, 1}, rgba{0, 0, 0, 1}); math.Abs(got-21) > 1e-9 {
		t.Errorf("white on black is %.4f:1, want 21:1", got)
	}

	ranked, err := parseCascadeSheet(`
		.x.y { color: #111111; }
		.x { color: #222222 !important; }
		.x { color: #333333 !important; }
		span.x.y.z { color: #444444; }`, nil)
	if err != nil {
		t.Fatalf("parsing the ranking sheet: %v", err)
	}
	el := describe("html", "span.x.y.z")
	got, err := newCSSCascade(ranked, 1440).colour(el, "", "color")
	if err != nil || got.hex() != "#333333" {
		t.Errorf("the cascade picks %s (%v), want the later of the two important declarations, #333333", got.hex(), err)
	}

	for _, value := range []string{
		"color-mix(in oklab, #ffffff 40%, #066fd1)",
		"oklch(from #066fd1 calc(l - 0.06) c h)",
		"rebeccapurple",
	} {
		if _, err := parseColour(value, nil); err == nil {
			t.Errorf("the resolver measured %q instead of failing", value)
		}
	}
	if _, err := c.substitute(root, "var(--tblr-undeclared-for-the-gate)", 0); err == nil {
		t.Errorf("an undeclared var() with no fallback resolved instead of failing")
	}
	if _, err := parseCascadeSheet(`@layer base { .x { color: red; } }`, nil); err == nil {
		t.Errorf("an unknown at-rule was parsed instead of failing")
	}
	if unknown, err := parseCascadeSheet(`.x:unheard-of { color: #111111; }`, nil); err != nil {
		t.Fatalf("parsing: %v", err)
	} else if _, err := newCSSCascade(unknown, 1440).colour(describe("html", "span.x"), "", "color"); err == nil {
		t.Errorf("an unknown pseudo-class matched or mismatched silently instead of failing")
	}

	// The gates can fail: the vendored stylesheet alone.
	vendored, err := parseCascadeSheet(servePage(t, buildMux(), servedStylesheets[0]), nil)
	if err != nil {
		t.Fatalf("parsing the vendored stylesheet: %v", err)
	}
	v := newCSSCascade(vendored, 1440)
	badge := badgeElement(badgeContexts["tasks list cell"], badgeBlue)
	text, errText := v.colour(badge, "", "color")
	fill, errFill := v.colour(badge, "", "background-color")
	surface, errSurface := v.customColour(badge, "--tblr-bg-surface")
	if err := errors.Join(errText, errFill, errSurface); err != nil {
		t.Fatalf("resolving the vendored DOING badge: %v", err)
	}
	if ratio := textContrast(text, fill, surface); ratio >= 4.5 {
		t.Errorf("without the overrides the DOING badge reads at %.2f:1; the badge gate would pass vacuously", ratio)
	}
	if activeFill, err := v.colour(sizeLink(true, ""), "", "background-color"); err != nil || activeFill.a == 1 {
		t.Errorf("without the override the active rows-per-page fill is %s (%v); the fill gate would pass vacuously", activeFill.hex(), err)
	}
	var card focusTarget
	for _, target := range focusTargets() {
		if target.name == "board card" {
			card = target
		}
	}
	if card.el == nil {
		t.Fatalf("no focus target describes the board card")
	}
	if style, _, err := v.resolved(card.el, "", "outline-style"); err == nil && style == "solid" {
		t.Errorf("without the override the board card still carries a solid focus outline; the focus gate would pass vacuously")
	}
}

// TestFilterBarLayoutRules is the stylesheet half of Acceptance Criterion 243,
// resolved by the cascade the contrast gates use: at every viewport width the
// bar's three small selects, its small search input, and its Apply button resolve
// to one line height, and the search input to the vendored small height, so the
// controls render at one height (the browser measurement confirms it); the sprint
// select is capped at 16rem from 576px up and uncapped below; and no rule of the
// project stylesheet that matches the list card's card-actions container
// declares a margin, so the container keeps Tabler's own position.
func TestFilterBarLayoutRules(t *testing.T) {
	rules := servedCascadeRules(t)
	vendoredRules, err := parseCascadeSheet(servePage(t, buildMux(), servedStylesheets[0]), nil)
	if err != nil {
		t.Fatalf("parsing the vendored stylesheet: %v", err)
	}
	firstProjectRule := len(vendoredRules)
	controls := map[string]*cssElement{
		"search input":  describe(append(append([]string(nil), filterBarPath...), "input.form-control.form-control-sm#task-filter-q[type=search][name=q][placeholder=Search]")...),
		"sprint select": describe(append(append([]string(nil), filterBarPath...), "select.form-select.form-select-sm.task-list__sprint-select#task-filter-sprint[name=sprint]")...),
		"status select": describe(append(append([]string(nil), filterBarPath...), "select.form-select.form-select-sm#task-filter-status[name=status]")...),
		"type select":   describe(append(append([]string(nil), filterBarPath...), "select.form-select.form-select-sm#task-filter-type[name=type]")...),
		"Apply button":  describe(append(append([]string(nil), filterBarPath...), "button.btn.btn-primary.btn-sm[type=submit]")...),
	}
	for _, width := range contrastWidths {
		c := newCSSCascade(rules, width)
		heights := map[string]string{}
		for name, el := range controls {
			win, ok, err := c.winning(el, "", "line-height")
			if err != nil || !ok {
				t.Fatalf("at %.0fpx the %s has no line-height (%v)", width, name, err)
			}
			value, err := c.substitute(el, win.value, 0)
			if err != nil {
				t.Fatalf("at %.0fpx the %s's line-height: %v", width, name, err)
			}
			heights[name] = value
		}
		for name, value := range heights {
			if value != heights["Apply button"] {
				t.Errorf("at %.0fpx the %s's line-height is %q and the Apply button's %q; the controls must share one height",
					width, name, value, heights["Apply button"])
			}
		}
		if win, ok, err := c.winning(controls["search input"], "", "height"); err != nil || !ok ||
			win.value != "calc(1rem + .625rem + calc(var(--tblr-border-width) * 2))" {
			t.Errorf("at %.0fpx the search input's height is %+v (%v), want the vendored small height", width, win, err)
		}

		capped, ok, err := c.winning(controls["sprint select"], "", "max-width")
		if err != nil {
			t.Fatalf("the sprint select's max-width: %v", err)
		}
		if wantCap := width >= 576; (ok && capped.value == "16rem") != wantCap {
			t.Errorf("at %.0fpx the sprint select's max-width is %+v (set=%v); want 16rem exactly from 576px up", width, capped, ok)
		}

		actions := controls["Apply button"].parent.parent.parent
		if !actions.hasClass("card-actions") {
			t.Fatalf("the described path does not reach the card-actions container: %v", actions.classes)
		}
		matches, err := c.matching(actions, "")
		if err != nil {
			t.Fatalf("matching the card-actions container: %v", err)
		}
		for _, m := range matches {
			if m.rule.order < firstProjectRule {
				continue
			}
			for _, d := range m.rule.decls {
				if strings.HasPrefix(d.prop, "margin") {
					t.Errorf("at %.0fpx a project rule declares %s: %s on the list card's card-actions", width, d.prop, d.value)
				}
			}
		}
	}
}
