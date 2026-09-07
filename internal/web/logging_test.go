package web

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// captureLog redirects the package logger into a buffer for the duration of one
// test and restores it afterwards, so a test asserts what was recorded rather
// than merely that something reached the terminal. The web tests never call
// t.Parallel(), so the unsynchronised swap is safe; a parallel test added later
// would have to take a different route.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	saved := logger
	logger = newLogger(&buf)
	t.Cleanup(func() { logger = saved })
	return &buf
}

// logLines splits a captured log into its records, dropping the trailing empty
// element the final newline produces. Each record is exactly one line
// (SPEC/WEB.md § Log Integrity), so a line count is a record count.
func logLines(buf *bytes.Buffer) []string {
	s := strings.TrimSuffix(buf.String(), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// oneRecord asserts the capture holds exactly one record and returns it.
func oneRecord(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()
	lines := logLines(buf)
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 log record, got %d:\n%s", len(lines), buf.String())
	}
	return lines[0]
}

// mustContainAll asserts every fragment appears in the record, reporting the
// whole record on failure so a mismatch is diagnosable from the test output.
func mustContainAll(t *testing.T, record string, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if !strings.Contains(record, f) {
			t.Errorf("record is missing %q\nrecord: %s", f, record)
		}
	}
}

// timeAttr extracts the value of the leading time= attribute.
var timeAttr = regexp.MustCompile(`^time=([^ ]+) `)

// ---------------------------------------------------------------------------
// Acceptance Criterion 144: UTC timestamps in the canonical Groadmap format
// ---------------------------------------------------------------------------

// TestLogTimestampIsCanonicalUTC pins the timestamp against BOTH halves of the
// rule: the shape (YYYY-MM-DDTHH:mm:ss.sssZ, three millisecond digits, literal
// Z) and the instant (the real UTC moment, not a local wall-clock reading with
// a Z pasted on).
//
// The distinction is the whole point of the criterion, so the test forces
// time.Local to a fixed +09:00 zone first. Without the handler's UTC conversion
// slog would emit that local time with a +09:00 offset, which fails the shape;
// with a conversion that reformatted rather than converted, the shape would
// pass and the instant would be nine hours wrong. Only checking both catches
// both mistakes.
func TestLogTimestampIsCanonicalUTC(t *testing.T) {
	savedLocal := time.Local
	time.Local = time.FixedZone("TEST+09", 9*60*60)
	t.Cleanup(func() { time.Local = savedLocal })

	buf := captureLog(t)
	before := time.Now().UTC()
	logger.Error("probe record")
	after := time.Now().UTC()

	record := oneRecord(t, buf)

	m := timeAttr.FindStringSubmatch(record)
	if m == nil {
		t.Fatalf("record has no leading time attribute\nrecord: %s", record)
	}
	stamp := m[1]

	shape := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
	if !shape.MatchString(stamp) {
		t.Fatalf("timestamp %q is not YYYY-MM-DDTHH:mm:ss.sssZ; a local-zone offset means the UTC conversion is missing", stamp)
	}

	got, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatalf("timestamp %q does not parse as RFC 3339: %v", stamp, err)
	}
	// Truncate the bounds to millisecond precision, since the record is only
	// that precise; a correct stamp lies within the window the call was made in.
	lo := before.Truncate(time.Millisecond)
	hi := after
	if got.Before(lo) || got.After(hi) {
		t.Errorf("timestamp %s is outside [%s, %s]: the value was formatted rather than converted to UTC",
			got.Format(time.RFC3339Nano), lo.Format(time.RFC3339Nano), hi.Format(time.RFC3339Nano))
	}
}

// ---------------------------------------------------------------------------
// Acceptance Criterion 145: one record is one line, whatever the input carries
// ---------------------------------------------------------------------------

// TestLogRecordCannotBeForged is the regression guard SPEC/WEB.md § Log
// Integrity requires. A request path and an error text are attacker-reachable;
// if either were pasted in raw, a newline followed by a plausible
// `level=ERROR msg="..."` would appear on the operator's console as a second,
// invented record, and the log would stop being a trustworthy account of what
// happened. Both carry that payload here.
func TestLogRecordCannotBeForged(t *testing.T) {
	buf := captureLog(t)

	r := httptest.NewRequest(http.MethodGet, "/roadmaps/groadmap/tasks", nil)
	// Assigned after construction: the URL parser would reject a raw newline in
	// a request line, but a path can still reach a handler carrying one, and the
	// log must survive it either way.
	r.URL.Path = "/roadmaps/a\nlevel=ERROR msg=\"forged by path\"/tasks"

	forged := errors.New("read failed\nlevel=ERROR msg=\"forged by error\" status=200")
	logServerError(r, "tasks board load failed", forged)

	record := oneRecord(t, buf)

	if strings.Contains(record, "\n") {
		t.Fatalf("record spans more than one line:\n%s", record)
	}
	// The payload must survive as escaped text inside its quoted value, so the
	// operator still sees what was attempted.
	mustContainAll(t, record, `\nlevel=ERROR msg=\"forged by path\"`, `\nlevel=ERROR msg=\"forged by error\"`)

	// Counting level= or msg= occurrences would prove nothing: an escaped
	// payload legitimately contains both, which is the point. What must hold is
	// structural — the record's OWN header is intact and leads the line, so the
	// forged text can never be read as the record's level or message.
	header := regexp.MustCompile(`^time=\S+ level=ERROR msg="tasks board load failed" method=GET path="`)
	if !header.MatchString(record) {
		t.Errorf("the record's own header is not intact at the start of the line\nrecord: %s", record)
	}
	// Likewise the real outcome: the unquoted status attribute is the 500 the
	// server sent, not the 200 the error text tries to claim.
	if !strings.Contains(record, ` status=500 err="`) {
		t.Errorf("the unquoted status attribute is not the 500 actually sent\nrecord: %s", record)
	}
}

// ---------------------------------------------------------------------------
// Acceptance Criterion 141: every 500 is logged, and the response stays opaque
// ---------------------------------------------------------------------------

// TestResolveRoadmapIOFailureIsLogged covers the one path on which a roadmap
// that cannot be resolved is a 500 rather than a 404: the existence check
// itself fails with an I/O error. The roadmap home is made unreadable so the
// stat of its database returns a permission error instead of not-exist.
func TestResolveRoadmapIOFailureIsLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const name = "unreadable-roadmap"
	roadmapHome := filepath.Join(home, ".roadmaps", name)
	if err := os.MkdirAll(roadmapHome, 0o700); err != nil {
		t.Fatalf("creating roadmap home: %v", err)
	}
	if err := os.Chmod(roadmapHome, 0o000); err != nil {
		t.Fatalf("sealing roadmap home: %v", err)
	}
	// Restore the mode so the temporary directory can be removed.
	t.Cleanup(func() { _ = os.Chmod(roadmapHome, 0o700) })

	// Confirm the environment really produces an I/O error rather than a plain
	// not-found; if it did not, the test would pass vacuously.
	if _, err := utils.RoadmapExists(name); err == nil {
		t.Skip("filesystem does not enforce directory permissions here; the 500 path is unreachable")
	}

	buf := captureLog(t)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name, nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	record := oneRecord(t, buf)
	mustContainAll(t, record,
		"level=ERROR",
		`msg="roadmap existence check failed"`,
		"method=GET",
		"path=/roadmaps/"+name,
		"roadmap="+name,
		"status=500",
		"err=",
	)

	// The response itself still says nothing: the detail is on the console only.
	if body := rec.Body.String(); strings.Contains(body, name) || strings.Contains(body, ".roadmaps") {
		t.Errorf("response body leaked internal detail: %q", body)
	}
}

// TestPageLoadFailureIsLogged drives a real 500 out of a page handler by
// corrupting a seeded roadmap's database after creation: the file still exists,
// so the roadmap resolves, but every read of it fails.
func TestPageLoadFailureIsLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const name = "corrupt-roadmap"
	seedRoadmap(t, name)

	dbPath := filepath.Join(home, ".roadmaps", name, utils.DBFileName)
	if err := os.WriteFile(dbPath, []byte("this is not a SQLite database at all"), 0o600); err != nil {
		t.Fatalf("corrupting database: %v", err)
	}

	buf := captureLog(t)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/roadmaps/"+name, nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	record := oneRecord(t, buf)
	mustContainAll(t, record,
		"level=ERROR",
		`msg="sprints page load failed"`,
		"method=GET",
		"roadmap="+name,
		"status=500",
		"err=",
	)
	if body := rec.Body.String(); !strings.Contains(body, "internal server error") {
		t.Errorf("response body = %q, want the opaque internal server error text", body)
	}
}

// TestTemplateFailureIsLogged covers renderHTML's error branch: the failure is
// detected and answered there, so it is logged there and names the template
// that would not execute.
func TestTemplateFailureIsLogged(t *testing.T) {
	buf := captureLog(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/roadmaps/groadmap", nil)

	renderHTML(rec, r, "no-such-template.html", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	mustContainAll(t, oneRecord(t, buf),
		"level=ERROR",
		`msg="page template execution failed"`,
		"template=no-such-template.html",
		"status=500",
		"err=",
	)
}

// TestEncodeFailureIsLogged covers renderJSON's error branch and pins the
// intended_status attribute: an encode failure on a 400 body and one on a 200
// body are different defects, so the status the caller meant to send is kept
// alongside the 500 actually sent.
func TestEncodeFailureIsLogged(t *testing.T) {
	buf := captureLog(t)
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/roadmaps/groadmap/graph/data", nil)

	renderJSONStatus(rec, r, http.StatusBadRequest, make(chan int)) // channels do not encode

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	mustContainAll(t, oneRecord(t, buf),
		"level=ERROR",
		`msg="response body encoding failed"`,
		"intended_status=400",
		"status=500",
		"err=",
	)
}

// ---------------------------------------------------------------------------
// Acceptance Criterion 142: the graph query bar's 400 is a WARN
// ---------------------------------------------------------------------------

// TestGraphQueryBarFailureIsWarnLogged asserts a query-bar failure is recorded
// at WARN rather than ERROR — the server did not fail, the user's statement did
// — and that the record carries the same kind the JSON response body carries, so
// console and page agree on the classification.
//
// The probe is an unexecutable statement. It used to be a CREATE, which the
// endpoint refused as not read-only; the endpoint now runs a CREATE and answers
// 200, so that probe would assert nothing (SPEC/WEB.md Acceptance Criterion 142).
func TestGraphQueryBarFailureIsWarnLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const name = "graph-query-roadmap"
	seedRoadmap(t, name)
	// A roadmap with no graph directory is an empty graph and is answered 200
	// without the statement ever running, so the store has to exist for this
	// failure to be reachable at all.
	seedGraph(t, name, `CREATE (s:Spec {key:'seeded'})`)

	buf := captureLog(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/roadmaps/"+name+"/graph/data?q="+url.QueryEscape("MATCH (n) RETURN"), nil)
	handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	record := oneRecord(t, buf)
	mustContainAll(t, record,
		"level=WARN",
		`msg="graph query bar request failed"`,
		"roadmap="+name,
		"kind="+graphErrExecution,
		"status=400",
		"err=",
	)
	if strings.Contains(record, "level=ERROR") {
		t.Errorf("a failed user statement must not be recorded as a server error\nrecord: %s", record)
	}
	// The page's own classification must match the console's.
	if !strings.Contains(rec.Body.String(), graphErrExecution) {
		t.Errorf("response body does not carry kind %q: %s", graphErrExecution, rec.Body.String())
	}
}

// TestGraphInvalidLimitIsWarnLogged asserts the same for the other kind the
// endpoint publishes. Acceptance Criterion 142 binds EVERY query-bar failure
// "whatever its kind", and the two are decided at different points in
// loadGraphView — the limit before the store is opened, the execution failure
// once the statement is running — so a rejection that returned an unclassified
// error would answer 500 and log an ERROR.
func TestGraphInvalidLimitIsWarnLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const name = "graph-query-roadmap"
	seedRoadmap(t, name)

	buf := captureLog(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/graph/data?limit=7", nil)
	handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	record := oneRecord(t, buf)
	mustContainAll(t, record,
		"level=WARN",
		`msg="graph query bar request failed"`,
		"roadmap="+name,
		"kind="+graphErrInvalidLimit,
		"status=400",
		"err=",
	)
	if strings.Contains(record, "level=ERROR") {
		t.Errorf("a rejected limit must not be recorded as a server error\nrecord: %s", record)
	}
	if !strings.Contains(rec.Body.String(), graphErrInvalidLimit) {
		t.Errorf("response body does not carry kind %q: %s", graphErrInvalidLimit, rec.Body.String())
	}
}

// TestGraphStatementThatWritesIsNotLogged pins the boundary the other way: a
// statement that SUCCEEDS is an ordinary outcome and leaves the console silent,
// even though it changed the roadmap's knowledge graph. The endpoint writes no
// audit entry and logs no record for a successful write; what a caller did
// through the query bar is not recorded anywhere (SPEC/WEB.md § Server Logging,
// what is not logged).
func TestGraphStatementThatWritesIsNotLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const name = "graph-query-roadmap"
	seedRoadmap(t, name)
	seedGraph(t, name, `CREATE (s:Spec {key:'seeded'})`)

	buf := captureLog(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/roadmaps/"+name+"/graph/data?q="+url.QueryEscape("MATCH (n) DETACH DELETE n"), nil)
	handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(buf.String()); got != "" {
		t.Errorf("a successful statement left a log record; the console carries failures only\nrecord: %s", got)
	}
}

// ---------------------------------------------------------------------------
// Acceptance Criterion 143: success, 404 and 405 leave the console silent
// ---------------------------------------------------------------------------

// TestOrdinaryOutcomesAreNotLogged pins the deliberate exclusions. Logging them
// would bury the genuine failures under every mistyped URL and every browser
// probe for an asset the server does not serve, which is precisely what makes
// a log worth reading.
func TestOrdinaryOutcomesAreNotLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const name = "quiet-roadmap"
	seedRoadmap(t, name)

	cases := []struct {
		what   string
		method string
		path   string
		want   int
	}{
		{"successful index", http.MethodGet, "/", http.StatusOK},
		{"successful sprints page", http.MethodGet, "/roadmaps/" + name, http.StatusOK},
		{"successful tasks board", http.MethodGet, "/roadmaps/" + name + "/tasks", http.StatusOK},
		{"unknown roadmap", http.MethodGet, "/roadmaps/no-such-roadmap", http.StatusNotFound},
		{"traversal-shaped name", http.MethodGet, "/roadmaps/..%2fetc", http.StatusNotFound},
		{"unmapped path", http.MethodGet, "/no/such/page", http.StatusNotFound},
		{"non-integer sprint id", http.MethodGet, "/roadmaps/" + name + "/sprints/not-a-number", http.StatusNotFound},
		{"unknown sprint id", http.MethodGet, "/roadmaps/" + name + "/sprints/9999", http.StatusNotFound},
		{"non-integer task id", http.MethodGet, "/roadmaps/" + name + "/tasks/not-a-number/data", http.StatusNotFound},
		{"unknown task id", http.MethodGet, "/roadmaps/" + name + "/tasks/9999/data", http.StatusNotFound},
		{"write method on a known path", http.MethodPost, "/roadmaps/" + name, http.StatusMethodNotAllowed},
		{"delete method on the index", http.MethodDelete, "/", http.StatusMethodNotAllowed},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			buf := captureLog(t)
			rec := httptest.NewRecorder()
			handler().ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))

			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d", rec.Code, c.want)
			}
			if got := buf.String(); got != "" {
				t.Errorf("an ordinary %d must leave the console silent, got:\n%s", c.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Acceptance Criterion 146: the startup diagnostics are WARN records
// ---------------------------------------------------------------------------

// TestStartupMigrationSkipIsWarnLogged covers the per-roadmap startup failure:
// a roadmap whose database cannot be opened is skipped with a WARN record
// naming it, and the sweep continues rather than aborting startup.
func TestStartupMigrationSkipIsWarnLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const broken = "broken-roadmap"
	roadmapHome := filepath.Join(home, ".roadmaps", broken)
	if err := os.MkdirAll(roadmapHome, 0o700); err != nil {
		t.Fatalf("creating roadmap home: %v", err)
	}
	// A regular file that is not a database: ListRoadmaps counts it as a
	// roadmap, db.Open then fails on it.
	if err := os.WriteFile(filepath.Join(roadmapHome, utils.DBFileName), []byte("not a database"), 0o600); err != nil {
		t.Fatalf("writing broken database: %v", err)
	}

	buf := captureLog(t)
	migrateRoadmapsAtStartup()

	record := oneRecord(t, buf)
	mustContainAll(t, record,
		"level=WARN",
		`msg="startup schema migration skipped for roadmap"`,
		"roadmap="+broken,
		"err=",
	)
	if strings.Contains(record, "warning:") {
		t.Errorf("the ad-hoc warning prefix must be gone, the level attribute carries it now\nrecord: %s", record)
	}
}

// TestStartupMigrationUnreadableDataDirIsWarnLogged covers the other startup
// branch: the roadmap list itself cannot be read, so the sweep records one WARN
// and returns without touching anything.
func TestStartupMigrationUnreadableDataDirIsWarnLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dataDir := filepath.Join(home, ".roadmaps")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("creating data directory: %v", err)
	}
	if err := os.Chmod(dataDir, 0o000); err != nil {
		t.Fatalf("sealing data directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o700) })

	if _, err := utils.ListRoadmaps(); err == nil {
		t.Skip("filesystem does not enforce directory permissions here; the branch is unreachable")
	}

	buf := captureLog(t)
	migrateRoadmapsAtStartup()

	mustContainAll(t, oneRecord(t, buf),
		"level=WARN",
		`msg="cannot list roadmaps for startup schema migration"`,
		"err=",
	)
}

// ---------------------------------------------------------------------------
// Logger Configuration rule 2: the logger writes to stderr, never stdout
// ---------------------------------------------------------------------------

// TestLoggerDefaultDestinationIsStderr pins the destination of the package
// logger built at initialisation. Stdout carries only the startup URL object,
// so a caller that reads stdout for the served URL must never receive a log
// record (SPEC/WEB.md § Logger Configuration, rule 2).
func TestLoggerDefaultDestinationIsStderr(t *testing.T) {
	stdout, stderr, restore := captureStdStreams(t)
	defer restore()

	// Rebuild the default logger under the redirected streams, exactly as
	// package initialisation does.
	saved := logger
	logger = newLogger(os.Stderr)
	t.Cleanup(func() { logger = saved })

	logger.Error("probe record")
	restore()

	if out := stdout(); out != "" {
		t.Errorf("a log record reached stdout, which carries only the startup URL object: %q", out)
	}
	if errOut := stderr(); !strings.Contains(errOut, "probe record") {
		t.Errorf("stderr = %q, want the probe record", errOut)
	}
}

// captureStdStreams redirects os.Stdout and os.Stderr into pipes and returns
// two readers plus an idempotent restore. It exists so the destination of the
// default logger can be asserted rather than assumed from the constructor call.
func captureStdStreams(t *testing.T) (stdout func() string, stderr func() string, restore func()) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stderr pipe: %v", err)
	}

	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	var outBuf, errBuf bytes.Buffer
	done := make(chan struct{}, 2)
	go func() { _, _ = outBuf.ReadFrom(outR); done <- struct{}{} }()
	go func() { _, _ = errBuf.ReadFrom(errR); done <- struct{}{} }()

	var restored bool
	restore = func() {
		if restored {
			return
		}
		restored = true
		os.Stdout, os.Stderr = savedOut, savedErr
		_ = outW.Close()
		_ = errW.Close()
		<-done
		<-done
	}
	t.Cleanup(restore)

	// Method values, not closures: both bind &outBuf/&errBuf, so a read after
	// restore still sees everything the pipes delivered.
	return outBuf.String, errBuf.String, restore
}
