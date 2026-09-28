package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/db"
	"github.com/FlavioCFOliveira/Groadmap/internal/models"
)

// This file gates the document title every HTML page carries (SPEC/WEB.md
// § Document Title; Acceptance Criteria 173-176).

// titleFixtureRoadmap and titleFixtureSprint are the roadmap and sprint the
// acceptance criteria name: a roadmap called payments holding sprint 7.
const (
	titleFixtureRoadmap = "payments"
	titleFixtureSprint  = 7
)

// titleElement captures the text of every <title> element on a page. The
// template renders the element on one line, so a non-greedy match is exact.
var titleElement = regexp.MustCompile(`<title>(.*?)</title>`)

// navbarBrandClass matches an element whose class list carries the exact token
// navbar-brand; the brand's companion token navbar-brand-autodark does not
// count as a second brand.
var navbarBrandClass = regexp.MustCompile(`class="(?:[^"]*\s)?navbar-brand(?:\s[^"]*)?"`)

// seedTitleFixture creates the payments roadmap with sprints 1 through 7, so
// sprint 7 exists and its page renders. Every sprint goes through seedSprint,
// the statement `sprint create` runs.
func seedTitleFixture(t *testing.T) {
	t.Helper()
	database, err := db.Open(titleFixtureRoadmap)
	if err != nil {
		t.Fatalf("opening roadmap %q: %v", titleFixtureRoadmap, err)
	}
	defer database.Close() //nolint:errcheck // test cleanup

	now := time.Now().UTC().Format(time.RFC3339)
	for i := 1; i <= titleFixtureSprint; i++ {
		id, err := seedSprint(database, &models.Sprint{
			Status:      models.SprintPending,
			Title:       fmt.Sprintf("Card settlement increment %d", i),
			Description: fmt.Sprintf("Settle card payments in batch %d of the rollout", i),
			CreatedAt:   now,
		})
		if err != nil {
			t.Fatalf("creating sprint %d: %v", i, err)
		}
		if id != i {
			t.Fatalf("sprint id = %d, want %d", id, i)
		}
	}
}

// setServerHostname installs hostname as the startup hostname for one test and
// restores the previous value afterwards. The package runs no test with
// t.Parallel, so the unsynchronised swap is safe.
func setServerHostname(t *testing.T, hostname string) {
	t.Helper()
	previous := serverHostname
	t.Cleanup(func() { serverHostname = previous })
	serverHostname = hostname
}

// pageTitle requests path through the full handler chain with the given Host
// header, requires HTTP 200, and returns the text of the page's single <title>
// element, failing the test when the page carries zero or several.
func pageTitle(t *testing.T, h http.Handler, path, host string) (title, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200; body=%q", path, rec.Code, rec.Body.String())
	}
	body = rec.Body.String()
	matches := titleElement.FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		t.Fatalf("GET %s: %d <title> elements, want exactly 1", path, len(matches))
	}
	return matches[0][1], body
}

// titleCase is one page of the acceptance criteria and the document title it
// must carry for a given hostname.
type titleCase struct {
	path string
	want string
}

// titleCases returns the six pages of criterion 173 with their expected titles
// for hostname; an empty hostname yields the titles of criterion 176.
func titleCases(hostname string) []titleCase {
	suffix := ""
	if hostname != "" {
		suffix = " - " + hostname
	}
	base := "/roadmaps/" + titleFixtureRoadmap
	rdm := titleFixtureRoadmap + " - "
	return []titleCase{
		{"/", "Roadmaps" + suffix},
		{base, rdm + "Sprints" + suffix},
		{base + "/tasks", rdm + "Tasks" + suffix},
		{base + "/audit", rdm + "Audit" + suffix},
		{base + "/graph", rdm + "Knowledge graph" + suffix},
		{fmt.Sprintf("%s/sprints/%d", base, titleFixtureSprint), fmt.Sprintf("%sSprint #%d%s", rdm, titleFixtureSprint, suffix)},
	}
}

// TestDocumentTitle_EveryPage asserts the exact <title> of all six pages with a
// known startup hostname, that no title names the product in any letter case,
// and that the sidebar brand is unaffected (SPEC/WEB.md § Document Title, rules
// 1-3; Acceptance Criteria 173 and 174).
func TestDocumentTitle_EveryPage(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTitleFixture(t)
	setServerHostname(t, "thinkpad")

	h := handler()
	for _, tc := range titleCases("thinkpad") {
		got, body := pageTitle(t, h, tc.path, "127.0.0.1:7171")
		if got != tc.want {
			t.Errorf("GET %s: <title> = %q, want %q", tc.path, got, tc.want)
		}
		if strings.Contains(strings.ToLower(got), "groadmap") {
			t.Errorf("GET %s: <title> %q names the product", tc.path, got)
		}
		if n := len(navbarBrandClass.FindAllString(body, -1)); n != 1 {
			t.Errorf("GET %s: %d navbar-brand elements, want exactly 1", tc.path, n)
		}
	}
}

// TestDocumentTitle_IgnoresHostHeader asserts a request naming another host in
// its Host header gets the same title as one naming the served host: the last
// segment is the startup hostname, never a request field (SPEC/WEB.md
// § Document Title, rule 4; Acceptance Criterion 175).
func TestDocumentTitle_IgnoresHostHeader(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTitleFixture(t)
	setServerHostname(t, "thinkpad")

	h := handler()
	for _, tc := range titleCases("thinkpad") {
		served, _ := pageTitle(t, h, tc.path, "127.0.0.1:7171")
		foreign, _ := pageTitle(t, h, tc.path, "example.org")
		if foreign != served {
			t.Errorf("GET %s with Host: example.org: <title> = %q, want %q", tc.path, foreign, served)
		}
		if foreign != tc.want {
			t.Errorf("GET %s with Host: example.org: <title> = %q, want %q", tc.path, foreign, tc.want)
		}
	}
}

// TestDocumentTitle_UnavailableHostnameDropsSegment asserts that an empty
// startup hostname drops the hostname segment and its separator from every
// title, every page still answering 200, and no title ending in a separator
// (SPEC/WEB.md § Document Title, rule 5; Acceptance Criterion 176).
func TestDocumentTitle_UnavailableHostnameDropsSegment(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedTitleFixture(t)
	setServerHostname(t, "")

	h := handler()
	for _, tc := range titleCases("") {
		got, _ := pageTitle(t, h, tc.path, "127.0.0.1:7171")
		if got != tc.want {
			t.Errorf("GET %s: <title> = %q, want %q", tc.path, got, tc.want)
		}
		if strings.HasSuffix(got, " -") || strings.HasSuffix(got, "- ") || strings.HasSuffix(got, "-") {
			t.Errorf("GET %s: <title> %q ends in a separator", tc.path, got)
		}
	}
}

// TestDocumentTitle_HostnameIsEscaped asserts the hostname is rendered as
// escaped text, like every other value the interface shows (SPEC/WEB.md
// § Document Title, rule 6).
func TestDocumentTitle_HostnameIsEscaped(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	setServerHostname(t, `lab<b>&"rack"`)

	got, _ := pageTitle(t, handler(), "/", "127.0.0.1:7171")
	want := "Roadmaps - lab&lt;b&gt;&amp;&#34;rack&#34;"
	if got != want {
		t.Errorf("<title> = %q, want %q", got, want)
	}
}

// TestDocumentTitle_Format pins the helper every page uses: segment order, the
// separator, and the dropping of an empty roadmap or hostname segment together
// with its separator (SPEC/WEB.md § Document Title, rules 1, 2 and 5).
func TestDocumentTitle_Format(t *testing.T) {
	cases := []struct {
		roadmap, area, hostname, want string
	}{
		{"payments", "Sprint #7", "thinkpad", "payments - Sprint #7 - thinkpad"},
		{"payments", "Knowledge graph", "thinkpad", "payments - Knowledge graph - thinkpad"},
		{"", "Roadmaps", "thinkpad", "Roadmaps - thinkpad"},
		{"payments", "Tasks", "", "payments - Tasks"},
		{"", "Roadmaps", "", "Roadmaps"},
	}
	for _, tc := range cases {
		if got := documentTitle(tc.roadmap, tc.area, tc.hostname); got != tc.want {
			t.Errorf("documentTitle(%q, %q, %q) = %q, want %q",
				tc.roadmap, tc.area, tc.hostname, got, tc.want)
		}
	}
}

// TestReadHostname asserts the startup lookup: the reported hostname is kept,
// and a failed lookup yields the empty string that drops the segment rather
// than an error (SPEC/WEB.md § Document Title, rules 4 and 5).
func TestReadHostname(t *testing.T) {
	cases := []struct {
		name   string
		lookup func() (string, error)
		want   string
	}{
		{"reported", func() (string, error) { return "thinkpad", nil }, "thinkpad"},
		{"empty", func() (string, error) { return "", nil }, ""},
		{"failed", func() (string, error) { return "", errors.New("uname: no hostname") }, ""},
		{"failed with partial value", func() (string, error) { return "thinkpad", errors.New("uname: truncated") }, ""},
	}
	for _, tc := range cases {
		if got := readHostname(tc.lookup); got != tc.want {
			t.Errorf("%s: readHostname = %q, want %q", tc.name, got, tc.want)
		}
	}
}
