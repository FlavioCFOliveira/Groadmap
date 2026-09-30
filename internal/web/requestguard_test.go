package web

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// testHostPolicy is the listener every in-process request of this package's
// tests is addressed to: httptest.NewRequest gives a request built from a path
// the host example.com with no port, and this policy is the one that admits
// exactly that host — a specific bind host on port 80, the one bound port under
// which a host without a port is served (SPEC/WEB.md § Security and
// Constraints, rule 13).
var testHostPolicy = newHostPolicy("example.com", httpDefaultPort)

// handler is the production handler chain — security headers, request guard,
// router — as the in-process tests of this package drive it. It is newHandler
// over testHostPolicy and nothing else, so every test that serves through it
// passes through the real request guard rather than around it.
func handler() http.Handler {
	return newHandler(testHostPolicy)
}

// liveServer starts a real HTTP server on a loopback listener, serving the
// production handler chain under the policy of that very listener: bind host
// 127.0.0.1 and the port the listener was given. A client of it therefore sends
// the host 127.0.0.1:<port>, which the guard admits for the same reason it
// admits a browser that followed the URL `rmp web` printed.
//
// The listener is created before the handler, because the handler's policy
// needs the port the operating system chose. The caller closes the server.
func liveServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	srv.Config.Handler = newHandler(newHostPolicy("127.0.0.1", port))
	srv.Start()
	return srv
}

// guardPort is the bound port the guard tests describe. It is deliberately not
// 80, so a host without a port must be refused.
const guardPort = 8787

// hostPort renders a host part and a port as a request host.
func hostPort(part string, port int) string {
	return part + ":" + strconv.Itoa(port)
}

// serveWithHost drives one request through the handler with its Host field set
// to host (the empty string removes it, which is what an HTTP/1.0 request with
// no Host field looks like once parsed) and the given extra fields.
func serveWithHost(h http.Handler, method, target, host string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// levelCount counts the captured records of one level.
func levelCount(lines []string, level string) int {
	n := 0
	for _, line := range lines {
		if strings.Contains(line, " level="+level+" ") {
			n++
		}
	}
	return n
}

// TestHostPolicy_AllowsHost is SPEC/WEB.md § Security and Constraints, rule 13,
// over every row of its allowlist table and the port rule, asserted on the
// policy itself so each verdict is named on its own.
func TestHostPolicy_AllowsHost(t *testing.T) {
	cases := []struct {
		bindHost string
		port     int
		allowed  []string
		refused  []string
	}{
		{
			bindHost: "127.0.0.1", port: guardPort,
			allowed: []string{
				hostPort("127.0.0.1", guardPort), hostPort("localhost", guardPort),
				hostPort("LOCALHOST", guardPort), hostPort("[::1]", guardPort),
				hostPort("[0:0:0:0:0:0:0:1]", guardPort),
			},
			refused: []string{
				hostPort("attacker.example", guardPort), hostPort("127.0.0.1", guardPort+1),
				"127.0.0.1", "localhost", hostPort("localhost.", guardPort),
				hostPort("sub.localhost", guardPort), hostPort("127.0.0.2", guardPort),
				hostPort("0.0.0.0", guardPort), "", hostPort("::1", guardPort),
				"[::1", hostPort("127.0.0.1", 0) + "x", "127.0.0.1:", "127.0.0.1:+8787",
				hostPort("[127.0.0.1]", guardPort), hostPort("KKLOCALHOST", guardPort),
			},
		},
		{
			bindHost: "127.0.0.2", port: guardPort,
			allowed: []string{hostPort("127.0.0.2", guardPort), hostPort("127.0.0.1", guardPort), hostPort("localhost", guardPort)},
			refused: []string{hostPort("127.0.0.3", guardPort), hostPort("attacker.example", guardPort)},
		},
		{
			bindHost: "localhost", port: guardPort,
			allowed: []string{hostPort("localhost", guardPort), hostPort("127.0.0.1", guardPort), hostPort("[::1]", guardPort)},
			refused: []string{hostPort("localhost.", guardPort), hostPort("attacker.example", guardPort)},
		},
		{
			bindHost: "::1", port: guardPort,
			allowed: []string{hostPort("[::1]", guardPort), hostPort("127.0.0.1", guardPort)},
			refused: []string{hostPort("[::2]", guardPort)},
		},
		{
			bindHost: "192.0.2.10", port: guardPort,
			allowed: []string{hostPort("192.0.2.10", guardPort)},
			refused: []string{
				hostPort("localhost", guardPort), hostPort("127.0.0.1", guardPort), hostPort("[::1]", guardPort),
				hostPort("192.0.2.11", guardPort), hostPort("roadmaps.internal", guardPort),
				"192.0.2.10", hostPort("192.0.2.10", guardPort+1),
			},
		},
		{
			bindHost: "roadmaps.internal", port: guardPort,
			allowed: []string{hostPort("roadmaps.internal", guardPort), hostPort("Roadmaps.Internal", guardPort)},
			refused: []string{hostPort("roadmaps.internal.", guardPort), hostPort("localhost", guardPort), hostPort("192.0.2.10", guardPort)},
		},
		{
			bindHost: "0.0.0.0", port: guardPort,
			allowed: []string{
				hostPort("localhost", guardPort), hostPort("127.0.0.1", guardPort), hostPort("0.0.0.0", guardPort),
				hostPort("192.0.2.10", guardPort), hostPort("[::1]", guardPort), hostPort("[fe80::1]", guardPort),
			},
			refused: []string{
				hostPort("workstation.lan", guardPort), hostPort("attacker.example", guardPort),
				"127.0.0.1", hostPort("127.0.0.1", guardPort+1),
			},
		},
		{
			bindHost: "::", port: guardPort,
			allowed: []string{hostPort("localhost", guardPort), hostPort("192.0.2.10", guardPort)},
			refused: []string{hostPort("workstation.lan", guardPort)},
		},
		{
			bindHost: "", port: guardPort,
			allowed: []string{hostPort("localhost", guardPort), hostPort("[2001:db8::7]", guardPort)},
			refused: []string{hostPort("workstation.lan", guardPort)},
		},
		{
			// Port 80 is the one bound port under which a host without a port is
			// served, because it is the http default a browser omits.
			bindHost: "127.0.0.1", port: httpDefaultPort,
			allowed: []string{"127.0.0.1", "localhost", "[::1]", "127.0.0.1:80", "localhost:80"},
			refused: []string{"127.0.0.1:8080", "attacker.example"},
		},
	}

	for _, c := range cases {
		policy := newHostPolicy(c.bindHost, c.port)
		for _, host := range c.allowed {
			if !policy.allowsHost(host) {
				t.Errorf("bind %q port %d: host %q refused, want allowed", c.bindHost, c.port, host)
			}
		}
		for _, host := range c.refused {
			if policy.allowsHost(host) {
				t.Errorf("bind %q port %d: host %q allowed, want refused", c.bindHost, c.port, host)
			}
		}
	}
}

// TestRequestGuard_LoopbackBindServesOnlyTheLoopbackNames is SPEC/WEB.md
// Acceptance Criterion 258 in process, over the real handler chain.
//
// The refused hosts are driven against the index and against the graph data
// endpoint with a WRITING statement, and each refusal is proved to have run
// before any handler three ways: the graph the statement would have written is
// unchanged; the server's log holds one WARN per refusal and nothing else, where
// a request that had reached the roadmap would have been served or failed; and
// the control at the end sends the same statement with an allowed host and sees
// it land, so an unchanged graph is not an unobservable one.
func TestRequestGuard_LoopbackBindServesOnlyTheLoopbackNames(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := servedRoadmap(t, "identity-service", graphSeedQueries()...)
	h := newHandler(newHostPolicy("127.0.0.1", guardPort))

	for _, host := range []string{
		hostPort("127.0.0.1", guardPort), hostPort("localhost", guardPort),
		hostPort("LOCALHOST", guardPort), hostPort("[::1]", guardPort),
	} {
		if rec := serveWithHost(h, http.MethodGet, "/", host, nil); rec.Code != http.StatusOK {
			t.Errorf("GET / with host %q = %d, want 200", host, rec.Code)
		}
	}

	write := "/roadmaps/" + name + "/graph/data?q=" + url.QueryEscape("CREATE (:Intrusion {via:'host'})")
	refused := []string{
		hostPort("attacker.example", guardPort), hostPort("127.0.0.1", guardPort+1), "127.0.0.1",
		hostPort("localhost.", guardPort), hostPort("sub.localhost", guardPort),
		"", // an HTTP/1.0 request with no Host field
	}
	buf := captureLog(t)
	for _, host := range refused {
		for _, target := range []string{"/", write} {
			rec := serveWithHost(h, http.MethodGet, target, host, nil)
			if rec.Code != http.StatusForbidden || rec.Body.String() != refusedHostLine+"\n" {
				t.Errorf("GET %s with host %q = %d %q, want 403 %q", target, host, rec.Code, rec.Body.String(), refusedHostLine+"\n")
			}
		}
	}
	lines := logLines(buf)
	if want := 2 * len(refused); len(lines) != want || levelCount(lines, "WARN") != want {
		t.Errorf("want exactly %d WARN records and nothing else, one per refusal; got:\n%s", want, buf.String())
	}

	if n := countThroughTheServer(t, name, "MATCH (n:Intrusion) RETURN count(n)"); n != 0 {
		t.Fatalf("a refused request wrote %d node(s): the host check must run before any handler", n)
	}
	control := serveWithHost(h, http.MethodGet, write, hostPort("127.0.0.1", guardPort), nil)
	if control.Code != http.StatusOK {
		t.Fatalf("the control write with an allowed host = %d, want 200; body=%q", control.Code, control.Body.String())
	}
	if n := countThroughTheServer(t, name, "MATCH (n:Intrusion) RETURN count(n)"); n != 1 {
		t.Fatalf("the control write landed %d node(s), want 1: the unchanged graph above would prove nothing", n)
	}
}

// TestRequestGuard_ARefusedRequestReadsNoRoadmap proves the host check runs
// before any filesystem access. The data directory is a regular file, so every
// roadmap read fails with ENOTDIR and is answered 500 with an ERROR record; a
// refused request must instead be answered 403 with one WARN record and no
// ERROR, which it can only be if it touched nothing.
func TestRequestGuard_ARefusedRequestReadsNoRoadmap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".roadmaps"), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("planting the unreadable data directory: %v", err)
	}
	h := newHandler(newHostPolicy("127.0.0.1", guardPort))

	// The control: an allowed request does reach the filesystem and fails.
	buf := captureLog(t)
	if rec := serveWithHost(h, http.MethodGet, "/", hostPort("127.0.0.1", guardPort), nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("the control request = %d, want 500: the planted data directory must make every read fail", rec.Code)
	}
	buf.Reset()

	for _, target := range []string{"/", "/roadmaps/identity-service", "/roadmaps/identity-service/graph/data?q=MATCH%20(n)%20RETURN%20n"} {
		rec := serveWithHost(h, http.MethodGet, target, hostPort("attacker.example", guardPort), nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("GET %s with a foreign host = %d, want 403", target, rec.Code)
		}
	}
	lines := logLines(buf)
	if levelCount(lines, "ERROR") != 0 || levelCount(lines, "WARN") != 3 || len(lines) != 3 {
		t.Errorf("want exactly three WARN records and no ERROR: a refused request reads nothing; got:\n%s", buf.String())
	}
}

// TestRequestGuard_NonLoopbackBindsFollowTheirOwnRows is SPEC/WEB.md Acceptance
// Criterion 259, over the real handler chain.
func TestRequestGuard_NonLoopbackBindsFollowTheirOwnRows(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	seedRoadmap(t, "identity-service")

	specific := newHandler(newHostPolicy("192.0.2.10", guardPort))
	if rec := serveWithHost(specific, http.MethodGet, "/", hostPort("192.0.2.10", guardPort), nil); rec.Code != http.StatusOK {
		t.Errorf("specific bind: its own address = %d, want 200", rec.Code)
	}
	for _, host := range []string{
		hostPort("localhost", guardPort), hostPort("127.0.0.1", guardPort), hostPort("192.0.2.11", guardPort),
		hostPort("identity.example", guardPort), "192.0.2.10",
	} {
		if rec := serveWithHost(specific, http.MethodGet, "/", host, nil); rec.Code != http.StatusForbidden {
			t.Errorf("specific bind: host %q = %d, want 403", host, rec.Code)
		}
	}

	unspecified := newHandler(newHostPolicy("0.0.0.0", guardPort))
	for _, host := range []string{
		hostPort("localhost", guardPort), hostPort("127.0.0.1", guardPort), hostPort("0.0.0.0", guardPort),
		hostPort("192.0.2.10", guardPort), hostPort("[::1]", guardPort),
	} {
		if rec := serveWithHost(unspecified, http.MethodGet, "/", host, nil); rec.Code != http.StatusOK {
			t.Errorf("0.0.0.0 bind: host %q = %d, want 200", host, rec.Code)
		}
	}
	names := []string{"workstation.lan", "attacker.example"}
	if hostname, err := os.Hostname(); err == nil && hostname != "" && net.ParseIP(hostname) == nil {
		names = append(names, hostname)
	}
	for _, name := range names {
		if rec := serveWithHost(unspecified, http.MethodGet, "/", hostPort(name, guardPort), nil); rec.Code != http.StatusForbidden {
			t.Errorf("0.0.0.0 bind: name %q = %d, want 403", name, rec.Code)
		}
	}
	if rec := serveWithHost(unspecified, http.MethodGet, "/", "127.0.0.1", nil); rec.Code != http.StatusForbidden {
		t.Errorf("0.0.0.0 bind: a host without a port on port %d = %d, want 403", guardPort, rec.Code)
	}
}

// TestRequestGuard_CrossSiteRequestsAreRefused is SPEC/WEB.md Acceptance
// Criterion 260 over the real handler chain: every refusing value of either
// field, for GET and HEAD as well as for a write method, and every admitting
// value. A cross-site GET carrying a writing statement is then proved to leave
// the graph unchanged, and the graph page's own fetch to be served.
func TestRequestGuard_CrossSiteRequestsAreRefused(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := servedRoadmap(t, "identity-service", graphSeedQueries()...)
	h := newHandler(newHostPolicy("127.0.0.1", guardPort))
	own := hostPort("127.0.0.1", guardPort)

	refused := map[string]http.Header{
		"cross-site":             {"Sec-Fetch-Site": {"cross-site"}},
		"same-site":              {"Sec-Fetch-Site": {"same-site"}},
		"undefined value":        {"Sec-Fetch-Site": {"same-origin, none"}},
		"wrong case":             {"Sec-Fetch-Site": {"Same-Origin"}},
		"repeated fetch site":    {"Sec-Fetch-Site": {"same-origin", "same-origin"}},
		"null origin":            {"Origin": {"null"}},
		"other scheme":           {"Origin": {"https://" + own}},
		"other host":             {"Origin": {"http://" + hostPort("attacker.example", guardPort)}},
		"other port":             {"Origin": {"http://" + hostPort("127.0.0.1", guardPort+1)}},
		"localhost, other port":  {"Origin": {"http://" + hostPort("localhost", 3000)}},
		"no port on a non-80":    {"Origin": {"http://127.0.0.1"}},
		"repeated origin":        {"Origin": {"http://" + own, "http://" + own}},
		"origin with a path":     {"Origin": {"http://" + own + "/"}},
		"good origin, bad site":  {"Origin": {"http://" + own}, "Sec-Fetch-Site": {"cross-site"}},
		"good site, bad origin":  {"Origin": {"null"}, "Sec-Fetch-Site": {"same-origin"}},
		"empty fetch site value": {"Sec-Fetch-Site": {""}},
	}
	for label, header := range refused {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			rec := serveWithHost(h, method, "/", own, header)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: status %d, want 403", method, label, rec.Code)
			}
		}
	}

	served := map[string]http.Header{
		"same-origin":                  {"Sec-Fetch-Site": {"same-origin"}},
		"none":                         {"Sec-Fetch-Site": {"none"}},
		"own origin":                   {"Origin": {"http://" + own}},
		"own origin with fetch site":   {"Origin": {"http://" + own}, "Sec-Fetch-Site": {"same-origin"}},
		"neither field":                nil,
		"own origin, IPv6 equivalence": {"Origin": {"http://" + hostPort("[::ffff:127.0.0.1]", guardPort)}},
	}
	for label, header := range served {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			if rec := serveWithHost(h, method, "/", own, header); rec.Code != http.StatusOK {
				t.Errorf("%s %s: status %d, want 200", method, label, rec.Code)
			}
		}
	}
	// The Origin's host part is compared as the request's is, so a request that
	// names localhost is served an Origin naming localhost in another case.
	if rec := serveWithHost(h, http.MethodGet, "/", hostPort("localhost", guardPort),
		http.Header{"Origin": {"http://" + hostPort("LocalHost", guardPort)}}); rec.Code != http.StatusOK {
		t.Errorf("an Origin naming the request's own host in another case = %d, want 200", rec.Code)
	}

	write := "/roadmaps/" + name + "/graph/data?q=" + url.QueryEscape("CREATE (:Intrusion {via:'cross-site'})")
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := serveWithHost(h, method, write, own, http.Header{
			"Sec-Fetch-Site": {"cross-site"}, "Origin": {"http://attacker.example"},
		})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("a cross-site %s of a writing statement = %d, want 403", method, rec.Code)
		}
	}
	if n := countThroughTheServer(t, name, "MATCH (n:Intrusion) RETURN count(n)"); n != 0 {
		t.Fatalf("a cross-site GET wrote %d node(s) into the graph", n)
	}

	// What the graph page's own fetch sends: same-origin and no Origin.
	page := serveWithHost(h, http.MethodGet, write, own, http.Header{"Sec-Fetch-Site": {"same-origin"}})
	if page.Code != http.StatusOK {
		t.Fatalf("the graph page's own fetch = %d, want 200; body=%q", page.Code, page.Body.String())
	}
	if n := countThroughTheServer(t, name, "MATCH (n:Intrusion) RETURN count(n)"); n != 1 {
		t.Fatalf("the same-origin write landed %d node(s), want 1: the refusal above would prove nothing", n)
	}
}

// TestRequestGuard_RefusalShapeAndRecord is SPEC/WEB.md Acceptance Criterion
// 261: the status, the content type, the one-line body naming the failed check,
// no body on HEAD, the security headers and Cache-Control: no-store on every
// path — /static/... included — no Vary on the tasks route, and exactly one WARN
// record carrying the published attributes and no roadmap.
func TestRequestGuard_RefusalShapeAndRecord(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "identity-service")
	h := newHandler(newHostPolicy("127.0.0.1", guardPort))
	own := hostPort("127.0.0.1", guardPort)
	foreign := hostPort("attacker.example", guardPort)
	crossSite := http.Header{"Sec-Fetch-Site": {"cross-site", "same-site"}, "Origin": {"null"}}

	cases := []struct {
		label, method, path, host string
		header                    http.Header
		line                      string
	}{
		{"host, page", http.MethodGet, "/roadmaps/" + name, foreign, nil, refusedHostLine},
		{"host, static", http.MethodGet, "/static/app.css", foreign, nil, refusedHostLine},
		{"host, HEAD", http.MethodHead, "/roadmaps/" + name + "/tasks", foreign, nil, refusedHostLine},
		{"site, tasks", http.MethodGet, "/roadmaps/" + name + "/tasks", own, crossSite, refusedOriginLine},
		{"site, static", http.MethodGet, "/static/app.css", own, crossSite, refusedOriginLine},
		{"site, HEAD", http.MethodHead, "/", own, crossSite, refusedOriginLine},
		{"both fail", http.MethodGet, "/", foreign, crossSite, refusedHostLine},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			buf := captureLog(t)
			rec := serveWithHost(h, c.method, c.path, c.host, c.header)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", ct)
			}
			// The recorder keeps what the handler wrote, HEAD included: the server
			// discards a HEAD body on the wire, which
			// TestRequestGuard_HEADRefusalCarriesNoBodyOnTheWire asserts.
			wantBody := c.line + "\n"
			if got := rec.Body.String(); got != wantBody {
				t.Errorf("body = %q, want %q", got, wantBody)
			}
			for header, want := range map[string]string{
				"Content-Security-Policy": contentSecurityPolicy,
				"X-Content-Type-Options":  "nosniff",
				"X-Frame-Options":         "DENY",
				"Referrer-Policy":         "same-origin",
				"Cache-Control":           "no-store",
			} {
				if got := rec.Header().Values(header); len(got) != 1 || got[0] != want {
					t.Errorf("%s = %q, want exactly %q", header, got, want)
				}
			}
			if vary := rec.Header().Values("Vary"); len(vary) != 0 {
				t.Errorf("Vary = %q, want none: the refusal ran before any handler", vary)
			}
			if cookies := rec.Header().Values("Set-Cookie"); len(cookies) != 0 {
				t.Errorf("Set-Cookie = %q, want none", cookies)
			}

			record := oneRecord(t, buf)
			mustContainAll(t, record,
				" level=WARN ",
				`msg="request refused: `+c.line+`"`,
				"method="+c.method,
				"path="+c.path,
				"host="+c.host,
				" status=403 ",
				`err="`+c.line+`"`,
			)
			if c.header == nil {
				mustContainAll(t, record, `origin="" sec_fetch_site=""`)
			} else {
				mustContainAll(t, record, "origin=null", `sec_fetch_site="cross-site, same-site"`)
			}
			if strings.Contains(record, "roadmap=") {
				t.Errorf("a refusal carries no roadmap: no route was matched\nrecord: %s", record)
			}
		})
	}
}

// TestRequestGuard_HEADRefusalCarriesNoBodyOnTheWire drives a HEAD refusal over a
// real connection, because only the server decides what reaches the wire.
func TestRequestGuard_HEADRefusalCarriesNoBodyOnTheWire(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	srv := liveServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodHead, srv.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test response
	body := make([]byte, 1)
	n, _ := resp.Body.Read(body)
	if resp.StatusCode != http.StatusForbidden || n != 0 {
		t.Errorf("HEAD refusal = %d with %d body byte(s), want 403 and none", resp.StatusCode, n)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("HEAD refusal Content-Type = %q, want the GET's", ct)
	}
}

// TestRequestGuard_AbsoluteFormTargetTakesPrecedence pins that the host a
// request names is the authority of an absolute-form request target when it has
// one, and not the Host field (RFC 9112, Section 3.2.2).
func TestRequestGuard_AbsoluteFormTargetTakesPrecedence(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	h := newHandler(newHostPolicy("127.0.0.1", guardPort))

	foreign := httptest.NewRequest(http.MethodGet, "http://attacker.example:8787/", nil)
	foreign.Host = hostPort("127.0.0.1", guardPort)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, foreign)
	if rec.Code != http.StatusForbidden {
		t.Errorf("an absolute-form target naming a foreign host = %d, want 403 whatever the Host field says", rec.Code)
	}

	own := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/", nil)
	own.Host = hostPort("attacker.example", guardPort)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, own)
	if rec.Code != http.StatusOK {
		t.Errorf("an absolute-form target naming this listener = %d, want 200", rec.Code)
	}
}
