// Package web — the graph data endpoint when no graph server can be reached.
//
// # What this file fences
//
// The endpoint used to resolve the roadmap's socket and, on finding nothing
// answering there, open the store and read the graph itself. That fall back is
// withdrawn: the endpoint reaches a graph through the client mechanism and
// through nothing else, so a roadmap nothing is serving is reported as a graph
// that cannot be REACHED rather than read from disk
// (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 1).
//
// # Why the answer is 503 and the record is WARN
//
// A graph server is a dependency the operator starts. This web server is working
// correctly, the roadmap and its database are readable, every other route is
// served, and the one thing missing is a process `rmp graph serve` provides —
// which is what RFC 9110, Section 15.6.4, describes as a condition "which will
// likely be alleviated after some delay". 500 would assert that THIS server
// failed, and an ERROR on every page load of a roadmap whose server is not
// running trains an operator to ignore the level that means something is broken.
//
// # The split is the assertion, not either half
//
// One condition that also fails before a statement runs is deliberately NOT a
// 503: a derived socket path over the platform's bound, on which no server can
// EVER listen. Acceptance Criterion 165 drives both against ONE server and
// requires the 503 to produce exactly one WARN and ZERO ERROR records, because an
// implementation that merely logged "a record" would satisfy every check that
// looked for one while reintroducing exactly what the level split exists to
// prevent.
package web

import (
	"context"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/graphclient"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// TestHandleGraphData_NoServerIsServiceUnavailable covers SPEC/WEB.md Acceptance
// Criteria 149 and 164, and the halves are asserted together because neither is
// the criterion on its own.
//
// The status MUST be 503 and not merely a 5xx: 500 is this endpoint's answer to a
// different condition, and an assertion that accepted either would pass against
// an implementation that had confused the two. The level MUST be WARN and not
// merely "a record", by the same split.
//
// The body's silence is a security property and the record's content is what
// makes the condition diagnosable at all, so both are checked: the response body
// is the opaque text every other server-side failure carries and names no
// filesystem path, while the record carries the no-server line naming the socket
// that was probed — the same line `rmp graph client` writes for the same
// condition (SPEC/WEB.md § Record Content, rule 6; § Knowledge Graph from the
// GoGraph Store, rule 2).
func TestHandleGraphData_NoServerIsServiceUnavailable(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "backend-platform")

	socket, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path: %v", err)
	}

	buf := captureLog(t)
	rec := doGraphData(t, name, nil)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d. A roadmap with no graph server is a dependency the "+
			"operator starts, not a defect of this server, and the criterion requires 503 "+
			"specifically rather than a 5xx because 500 answers a different condition; body=%q",
			rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "internal server error") {
		t.Errorf("body = %q, want the opaque text every other server-side failure carries", body)
	}
	if strings.Contains(body, socket) || strings.Contains(body, ".roadmaps") {
		t.Errorf("the response body names a filesystem path: %q. The socket lives inside the "+
			"operator's home directory and never reaches an HTTP caller "+
			"(SPEC/WEB.md § Record Content, rule 6)", body)
	}
	if strings.Contains(body, "kind") {
		t.Errorf("the response body names a kind: %q. A kind belongs to the 400s and names a "+
			"fault in what the caller submitted; this request never reached a statement", body)
	}

	record := oneRecord(t, buf)
	if !strings.Contains(record, "level=WARN") {
		t.Errorf("the 503 was not recorded at WARN:\n%s\nRecording it at ERROR would put an "+
			"ERROR on every page load of a roadmap whose server is not running, which is exactly "+
			"what the level split exists to prevent (SPEC/WEB.md Acceptance Criterion 165)", record)
	}
	if strings.Contains(record, "level=ERROR") {
		t.Errorf("the 503 produced an ERROR record:\n%s", record)
	}
	for _, fragment := range []string{
		"method=GET",
		"path=/roadmaps/" + name + "/graph/data",
		"status=503",
		"no graph server is listening on " + socket,
	} {
		if !strings.Contains(record, fragment) {
			t.Errorf("the record is missing %q:\n%s", fragment, record)
		}
	}
	// The sentinel is the CLI's, because the failure is the CLI's failure met
	// through the same client (SPEC/WEB.md § Knowledge Graph from the GoGraph
	// Store, rule 2).
	_, unavailErr := resolveGraphServerForRequest(t.Context(), name)
	if unavailErr == nil {
		t.Fatal("resolution reported no error for a roadmap with no server listening")
	}
	if !utils.IsGraphServer(unavailErr) {
		t.Errorf("the no-server failure does not carry utils.ErrGraphServer: %v. The CLI and this "+
			"endpoint meet one condition through one client and MUST classify it identically",
			unavailErr)
	}
}

// TestHandleGraphData_TheTwo5xxAreRecordedAtDifferentLevels is SPEC/WEB.md
// Acceptance Criterion 165, and it is the sharp one.
//
// It drives BOTH 5xx answers against one captured log and counts records BY
// LEVEL rather than searching for one, because a level is only meaningful against
// the other levels the same server emits. **The zero is the assertion**: an
// implementation that recorded the 503 at ERROR would satisfy every check that
// merely looked for a record, and would break Acceptance Criterion 141's count of
// one ERROR per 500 in the same stroke — because the 503 produced no 500 at all.
func TestHandleGraphData_TheTwo5xxAreRecordedAtDifferentLevels(t *testing.T) {
	dir := bindDir(t)
	bound := measuredSocketPathBound(t, dir)
	const roadmap = "backend-platform"

	buf := captureLog(t)

	// First half: no server running, derived path inside the bound.
	t.Setenv("HOME", shortHome(t))
	unservedName := seedRoadmap(t, roadmap)
	unserved := doGraphData(t, unservedName, nil)
	if unserved.Code != http.StatusServiceUnavailable {
		t.Fatalf("the unserved roadmap answered %d, want 503; body=%q",
			unserved.Code, unserved.Body.String())
	}

	// Second half: a derived path over the bound, against the SAME server and the
	// same log. No server can ever listen there, so no delay alleviates it and it
	// keeps its 500.
	t.Setenv("HOME", deepHome(t, bound, roadmap))
	overBoundName := seedRoadmap(t, roadmap)
	assertDerivedPathIsOverTheBound(t, overBoundName, bound)
	overBound := doGraphData(t, overBoundName, nil)
	if overBound.Code != http.StatusInternalServerError {
		t.Fatalf("the over-bound roadmap answered %d, want 500. Answering 503 here would tell the "+
			"operator to start a server that can never bind; body=%q",
			overBound.Code, overBound.Body.String())
	}

	var warns, errors []string
	for _, line := range logLines(buf) {
		switch {
		case strings.Contains(line, "level=WARN"):
			warns = append(warns, line)
		case strings.Contains(line, "level=ERROR"):
			errors = append(errors, line)
		}
	}
	if len(warns) != 1 {
		t.Errorf("want exactly 1 WARN record over the two requests, got %d:\n%s",
			len(warns), strings.Join(warns, "\n"))
	}
	if len(errors) != 1 {
		t.Errorf("want exactly 1 ERROR record over the two requests, got %d:\n%s",
			len(errors), strings.Join(errors, "\n"))
	}
	if len(warns) == 1 && !strings.Contains(warns[0], "status=503") {
		t.Errorf("the WARN record does not carry status=503:\n%s", warns[0])
	}
	if len(errors) == 1 && !strings.Contains(errors[0], "status=500") {
		t.Errorf("the ERROR record does not carry status=500:\n%s", errors[0])
	}
	// Each record names the request and the underlying error (Criterion 165's
	// closing sentence, and Criterion 141's content requirement).
	for _, record := range append(append([]string{}, warns...), errors...) {
		for _, fragment := range []string{"method=GET", "path=/roadmaps/", "err="} {
			if !strings.Contains(record, fragment) {
				t.Errorf("a 5xx record is missing %q:\n%s", fragment, record)
			}
		}
	}
}

// TestHandleGraphData_ACancelledRequestIsNotReportedAsAnUnreachableServer is the
// regression test for a defect this task found and fixed.
//
// # The defect
//
// resolveGraphServerForRequest probes the socket under the REQUEST's context, so
// a client that disconnects before the probe completes fails the dial. Every
// other failed dial means "nothing is listening", and the endpoint classified
// this one the same way: 503, and a WARN record reading "graph server
// unavailable" that named the socket. A server may well have been listening
// throughout, and that WARN line is the one an operator is meant to act on
// (Acceptance Criteria 164 and 165). A page left by a user mid-load was therefore
// enough to report a healthy graph server as an absent one.
//
// # Why the assertion is the pair
//
// The roadmap here IS SERVED, which is what makes the old behaviour a lie rather
// than a wording preference: a 503 naming this socket would be false about a
// server that is running. And the record is counted BY LEVEL and read for the
// no-server line, because a check that merely looked for "a record" would pass
// against the defect — the defect emitted one.
//
// The classification it lands on instead is the one graphExecutionError already
// gives the same pair of causes one layer down: an execution failure, 400, no new
// kind and no new status. Nothing reads that body, because the caller has gone;
// what the classification decides is the record.
func TestHandleGraphData_ACancelledRequestIsNotReportedAsAnUnreachableServer(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := servedRoadmap(t, "backend-platform", `CREATE (s:Spec {key:'user-authentication'})`)

	socket, err := graphclient.SocketPath(name)
	if err != nil {
		t.Fatalf("deriving the socket path: %v", err)
	}

	// The control, first and against the same server: an ordinary request is
	// answered, so the 503 the defect produced could never have been about a
	// server that was missing.
	if rec := doGraphData(t, name, nil); rec.Code != http.StatusOK {
		t.Fatalf("the control request answered %d, want 200; the server this test needs is not "+
			"running and nothing below would be about a cancellation. body=%q",
			rec.Code, rec.Body.String())
	}

	buf := captureLog(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the client went away before the request could be served
	req := httptest.NewRequest(http.MethodGet, "/roadmaps/"+name+"/graph/data", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusServiceUnavailable {
		t.Errorf("a cancelled request against a SERVED roadmap answered 503, the status this " +
			"endpoint reserves for a graph server that cannot be reached. One was running; the " +
			"caller went away (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 1)")
	}

	var warns, errs []string
	for _, line := range logLines(buf) {
		switch {
		case strings.Contains(line, "level=WARN"):
			warns = append(warns, line)
		case strings.Contains(line, "level=ERROR"):
			errs = append(errs, line)
		}
	}
	if len(errs) != 0 {
		t.Errorf("a cancelled request produced %d ERROR record(s); a caller that disconnected is "+
			"not a fault of this server:\n%s", len(errs), strings.Join(errs, "\n"))
	}
	for _, record := range append(append([]string{}, warns...), errs...) {
		if strings.Contains(record, "graph server unavailable") {
			t.Errorf("a cancelled request was recorded as an unavailable graph server:\n%s\n"+
				"A server was listening on %s throughout; the record must not send an operator "+
				"looking for a process that is running", record, socket)
		}
		if strings.Contains(record, socket) {
			t.Errorf("the record names the socket %s, which is what the no-server line does. The "+
				"probe failed because the request was cancelled, not because of anything about "+
				"that path:\n%s", socket, record)
		}
	}
}

// TestHandleGraphData_NoRequestOpensAGraphStore is SPEC/WEB.md Acceptance
// Criterion 163, asserted over the store's OWN artefacts rather than over the
// source, because the source says what the code intends and the directory says
// what the process did.
//
// The write case is the one that matters most: an implementation that still
// opened the store would run GoGraph's recovery on the open and could change the
// directory's structure without changing its data — which is the effect this
// criterion exists to rule out. The fingerprint therefore compares names, lengths
// and content digests, not just the set of files.
func TestHandleGraphData_NoRequestOpensAGraphStore(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "backend-platform")

	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatalf("resolving the roadmap directory: %v", err)
	}
	graphDir := filepath.Join(roadmapDir, "graph")

	// A store with real content, so the fingerprint has something to protect and
	// a recovery run would have something to rearrange.
	seedGraph(t, name, graphSeedQueries()...)
	before := fingerprintDir(t, graphDir)
	if len(before) == 0 {
		t.Fatal("the seeded graph directory is empty; the fingerprint would be vacuous")
	}

	// No `q`, a read, and a write. Each is answered 503 because nothing is
	// serving, and each must leave the directory exactly as it found it.
	for _, statement := range []string{
		"",
		"MATCH (n) RETURN n",
		"CREATE (n:WebProbe {key:'w'})",
	} {
		params := graphParams(statement)
		rec := doGraphData(t, name, params)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("q=%q answered %d, want 503; body=%q", statement, rec.Code, rec.Body.String())
		}
		after := fingerprintDir(t, graphDir)
		assertSameFingerprint(t, before, after, statement)
	}
}

// TestHandleGraphData_UnservedRoadmapWithNoGraphCreatesNothing is the other half
// of Criterion 163: a roadmap that has never had a graph must not acquire one
// from a request, and in particular must not acquire the `graph/` directory the
// store's open used to create.
func TestHandleGraphData_UnservedRoadmapWithNoGraphCreatesNothing(t *testing.T) {
	t.Setenv("HOME", shortHome(t))
	name := seedRoadmap(t, "backend-platform")

	roadmapDir, err := utils.GetRoadmapDir(name)
	if err != nil {
		t.Fatalf("resolving the roadmap directory: %v", err)
	}
	graphDir := filepath.Join(roadmapDir, "graph")

	for _, statement := range []string{"", "MATCH (n) RETURN n", "CREATE (n:WebProbe {key:'w'})"} {
		rec := doGraphData(t, name, graphParams(statement))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("q=%q answered %d, want 503; body=%q", statement, rec.Code, rec.Body.String())
		}
	}

	if _, statErr := os.Stat(graphDir); !os.IsNotExist(statErr) {
		t.Errorf("%s exists after three requests against a roadmap that never had a graph "+
			"(stat error %v). This process opens no store, so it creates no directory "+
			"(SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rule 6)", graphDir, statErr)
	}
}

// TestWebPackage_SpawnsNoSubprocess is SPEC/WEB.md Acceptance Criterion 162.
//
// **The assertion is on the source rather than on a run**, and the criterion says
// why in as many words: an endpoint that shelled out to `rmp graph client` would
// satisfy every behavioural criterion in that file — the statuses, the read-backs
// and the timings would all hold — while making the endpoint's behaviour depend
// on which binary is on a path rather than on the code it was built from. No run
// distinguishes the two; a scan of the source does.
//
// # The one exemption, and why it is named rather than assumed
//
// Criterion 162 is worded as a universal — "No production file under
// internal/web constructs a subprocess" — and SPEC/WEB.md contradicts that
// universal itself. Step 6 of the command lifecycle REQUIRES `rmp web` to open
// the user's default browser at the served URL unless `--no-open` is given, and
// the paragraph below the steps says so in as many words: "step 6 spawns a
// browser, and a process spawn is far slower than anything else between the URL
// and the accept loop". A gate that enforced the universal literally would fail
// on a requirement of the same file.
//
// So the gate enforces what the criterion DEMONSTRABLY protects — the graph
// endpoint's route to a graph — and pins the browser launch as the single, named
// exemption. That keeps it sharp in both directions: a subprocess added to the
// graph path fails it, a subprocess added to any other file fails it, and a
// SECOND subprocess added to the exempted file fails it too. The exemption is
// asserted to be live rather than trusted, so a browser launch that moved
// elsewhere would be reported instead of silently widening the hole.
func TestWebPackage_SpawnsNoSubprocess(t *testing.T) {
	// The file SPEC/WEB.md § Server Lifecycle, step 6, requires to spawn a
	// process, and the only one.
	const browserLauncher = "server.go"

	// os/exec is the route to a child process from Go's standard library;
	// syscall reaches ForkExec and StartProcess directly, so it is named too and
	// is not a way round.
	forbidden := map[string]string{
		"os/exec": "constructs a child process (exec.Command, exec.CommandContext)",
		"syscall": "reaches ForkExec / StartProcess directly",
	}

	// The files that carry the graph endpoint. These may never import either,
	// exemption or not: this is the half of the criterion that is not in doubt.
	graphPath := map[string]bool{"data.go": true, "pages.go": true, "routes.go": true}

	fset := token.NewFileSet()
	scanned := 0
	spawners := []string{}
	for _, nameOf := range productionFiles(t) {
		file, parseErr := parser.ParseFile(fset, nameOf, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", nameOf, parseErr)
		}
		scanned++
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			why, bad := forbidden[path]
			if !bad {
				continue
			}
			spawners = append(spawners, nameOf)
			if graphPath[nameOf] {
				t.Errorf("%s imports %q, which %s. The graph data endpoint MUST reach a server "+
					"through internal/graphclient in this process and MUST NOT run "+
					"`rmp graph client` as a child: a subprocess would put a process boundary, an "+
					"argument-quoting layer, an exit code and a second copy of the output "+
					"serialisation between this endpoint and the answer it owes "+
					"(SPEC/WEB.md Acceptance Criterion 162; § Knowledge Graph from the GoGraph "+
					"Store, rule 3)", nameOf, path, why)
				continue
			}
			if nameOf != browserLauncher {
				t.Errorf("%s imports %q, which %s. The ONE production file of this package "+
					"permitted to spawn a process is %s, and only for the browser launch "+
					"SPEC/WEB.md § Server Lifecycle, step 6, requires. Anything else is a "+
					"subprocess this interface does not need (Acceptance Criterion 162)",
					nameOf, path, why, browserLauncher)
			}
		}
	}

	// The exemption must be exactly one file and it must be the named one. A
	// gate whose exemption had gone stale would stop protecting anything.
	if len(spawners) != 1 || spawners[0] != browserLauncher {
		t.Errorf("the process-spawning production files are %v, want exactly [%s]. The exemption "+
			"is named in this test because it is a requirement of SPEC/WEB.md § Command "+
			"Lifecycle, step 6; if the browser launch has moved, move the exemption with it "+
			"rather than widening it", spawners, browserLauncher)
	}

	if scanned < 5 {
		t.Fatalf("the gate scanned only %d production files; it is not reaching the package", scanned)
	}
	// And it must be able to SEE an import, or the sweep proves nothing about the
	// imports it did not find.
	if !packageImports(t, fset, "github.com/FlavioCFOliveira/Groadmap/internal/graphclient") {
		t.Error("the gate cannot find internal/graphclient among the package's imports, so its " +
			"failure to find os/exec establishes nothing. The endpoint's ONE route to a graph is " +
			"a call into that package (SPEC/WEB.md Acceptance Criterion 162)")
	}
}

// productionFiles lists the package's non-test Go files, so the two structural
// gates sweep one set rather than each deciding for itself what counts.
func productionFiles(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	var out []string
	for _, entry := range entries {
		nameOf := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(nameOf, ".go") || strings.HasSuffix(nameOf, "_test.go") {
			continue
		}
		out = append(out, nameOf)
	}
	return out
}

// packageImports reports whether any production file of the package imports path.
// It is the inverse assertion that keeps TestWebPackage_SpawnsNoSubprocess from
// passing vacuously.
func packageImports(t *testing.T, fset *token.FileSet, path string) bool {
	t.Helper()

	for _, nameOf := range productionFiles(t) {
		file, parseErr := parser.ParseFile(fset, nameOf, nil, parser.ImportsOnly)
		if parseErr != nil {
			continue
		}
		for _, spec := range file.Imports {
			if strings.Trim(spec.Path.Value, `"`) == path {
				return true
			}
		}
	}
	return false
}

// assertNoEngineConstruction is the structural half of "no web request opens the
// store": the package must construct no graph engine and open no store, so there
// is no second route in for a later change to reach for.
//
// It is an AST sweep rather than an import check because internal/graphstore is a
// legitimate import for a package that merely names its diagnostics; what must
// not appear is the CALL that opens a store.
func TestWebPackage_OpensNoGraphStore(t *testing.T) {
	// pkg.Func spellings that open a store or build an engine over one.
	forbidden := map[string]map[string]bool{
		"graphstore": {"Open": true},
		"recovery":   {"Open": true},
		"cypher":     {"NewEngineWithStore": true, "NewEngineWithStoreAndRecovery": true},
		"graphlock":  {"AcquireExclusive": true},
	}

	fset := token.NewFileSet()
	scanned := 0
	for _, nameOf := range productionFiles(t) {
		file, parseErr := parser.ParseFile(fset, nameOf, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", nameOf, parseErr)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			sel, isSel := call.Fun.(*ast.SelectorExpr)
			if !isSel {
				return true
			}
			ident, isIdent := sel.X.(*ast.Ident)
			if !isIdent {
				return true
			}
			if funcs, watched := forbidden[ident.Name]; watched && funcs[sel.Sel.Name] {
				t.Errorf("%s calls %s.%s. This process opens no graph store, takes no advisory "+
					"lock and constructs no engine; a statement runs in the graph server and "+
					"nowhere else (SPEC/WEB.md § Knowledge Graph from the GoGraph Store, rules 1 "+
					"and 7; Acceptance Criterion 163)", nameOf, ident.Name, sel.Sel.Name)
			}
			return true
		})
	}
	if scanned < 5 {
		t.Fatalf("the gate scanned only %d production files; it is not reaching the package", scanned)
	}
}

// fingerprintDir records every file under root by relative name, length and
// content digest.
//
// It walks the WHOLE graph directory rather than the two artefacts a checkpoint
// rewrites, because Criterion 163 turns on the structure as well as the data: a
// `write.lock` appearing where none was, a `snapshot.tmp` removed, and a
// `snapshot.bak` promoted are all effects of OPENING a store, and none of them
// changes the graph. A fingerprint narrower than the directory would miss exactly
// the class of change the criterion exists to rule out.
//
// A missing root fingerprints as the empty map rather than failing, so the helper
// is total over "the roadmap has no graph yet".
func fingerprintDir(t *testing.T, root string) map[string]string {
	t.Helper()

	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		key, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if entry.IsDir() {
			// Directories are recorded too: a staging directory that appeared or
			// vanished is a structural change even when it holds no file.
			out[key+"/"] = "dir"
			return nil
		}
		raw, readErr := os.ReadFile(path) //nolint:gosec // path derives from t.TempDir via HOME
		if readErr != nil {
			return readErr
		}
		out[key] = fmt.Sprintf("%d:%x", len(raw), sha256.Sum256(raw))
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("fingerprinting %s: %v", root, err)
	}
	return out
}

// assertSameFingerprint reports every difference between two fingerprints, naming
// the statement that was in flight, so a failure says WHICH file changed and how
// rather than that something did.
func assertSameFingerprint(t *testing.T, before, after map[string]string, statement string) {
	t.Helper()

	if maps.Equal(before, after) {
		return
	}
	for key, want := range before {
		got, present := after[key]
		switch {
		case !present:
			t.Errorf("q=%q removed %s from the graph directory", statement, key)
		case got != want:
			t.Errorf("q=%q changed %s\n before: %s\n after:  %s", statement, key, want, got)
		}
	}
	for key := range after {
		if _, present := before[key]; !present {
			t.Errorf("q=%q created %s in the graph directory. This process opens no store, so it "+
				"runs no recovery, creates no write.lock and creates no directory "+
				"(SPEC/WEB.md Acceptance Criterion 163)", statement, key)
		}
	}
}

// graphParams builds the endpoint's query values for a statement, and no values
// at all for the empty one, so "a request with no q" is driven as the endpoint
// really receives it rather than as `q=`.
func graphParams(statement string) url.Values {
	if statement == "" {
		return nil
	}
	return url.Values{"q": {statement}}
}
