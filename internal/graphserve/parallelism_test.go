package graphserve

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The tests in this file change process-global state — the GOMAXPROCS setting
// and the GOMAXPROCS environment variable — so none of them calls t.Parallel,
// and each one restores what it changed (SPEC/GRAPH.md § Processor Parallelism).

// TestProcessorParallelism_Derivation pins every branch of the pure rule: an
// absent or empty variable reports the source as derived and the runtime
// default as the usable CPU count; any non-empty value, valid or not, reports
// the source as environment and no CPU count (rules 2, 3 and 4). The rule
// returns no value to apply, so no branch can change the setting.
func TestProcessorParallelism_Derivation(t *testing.T) {
	cases := []struct {
		name           string
		envValue       string
		envSet         bool
		runtimeDefault int
		wantSource     string
		wantCPUs       int
	}{
		{"unset, 1 CPU", "", false, 1, parallelismDerived, 1},
		{"unset, 2 CPUs", "", false, 2, parallelismDerived, 2},
		{"unset, 4 CPUs", "", false, 4, parallelismDerived, 4},
		{"unset, 512 CPUs", "", false, 512, parallelismDerived, 512},
		{"empty counts as unset", "", true, 4, parallelismDerived, 4},
		{"set to a valid value", "3", true, 4, parallelismEnvironment, 0},
		{"set to an invalid value", "abc", true, 4, parallelismEnvironment, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, cpus := processorParallelism(tc.envValue, tc.envSet, tc.runtimeDefault)
			if source != tc.wantSource || cpus != tc.wantCPUs {
				t.Errorf("processorParallelism(%q, %v, %d) = (%q, %d), want (%q, %d)",
					tc.envValue, tc.envSet, tc.runtimeDefault, source, cpus,
					tc.wantSource, tc.wantCPUs)
			}
		})
	}
}

// parallelismRecord matches the one processor-parallelism record, attribute by
// attribute and in the published order, after the leading time attribute.
var parallelismRecord = regexp.MustCompile(
	`^time=\S+ level=INFO msg="processor parallelism" source=(derived|environment) gomaxprocs=(\d+)(?: cpus=(\d+))?$`)

// parsedParallelism is one processor-parallelism record, decoded.
type parsedParallelism struct {
	source     string
	gomaxprocs int
	cpus       int
	hasCPUs    bool
}

// parseParallelismRecord decodes line, failing t when it is not a
// processor-parallelism record of the published shape.
func parseParallelismRecord(t *testing.T, line string) parsedParallelism {
	t.Helper()

	m := parallelismRecord.FindStringSubmatch(line)
	if m == nil {
		t.Fatalf("not a processor-parallelism record of the published shape: %q", line)
	}
	r := parsedParallelism{source: m[1]}
	r.gomaxprocs, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		r.hasCPUs = true
		r.cpus, _ = strconv.Atoi(m[3])
	}
	return r
}

// keepGOMAXPROCS restores this process's GOMAXPROCS setting when t ends.
func keepGOMAXPROCS(t *testing.T) {
	t.Helper()
	saved := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(saved) })
}

// unsetGOMAXPROCSEnv removes the GOMAXPROCS variable from this process's
// environment for the rest of t, restoring it when t ends. t.Setenv records the
// original value for the restore; the removal follows it.
func unsetGOMAXPROCSEnv(t *testing.T) {
	t.Helper()
	t.Setenv(gomaxprocsEnv, "")
	if err := os.Unsetenv(gomaxprocsEnv); err != nil {
		t.Fatalf("unsetting %s: %v", gomaxprocsEnv, err)
	}
}

// TestSettleProcessorParallelism_LeavesTheSettingAndRecords drives the
// side-effecting caller in this process: under every branch it must leave the
// GOMAXPROCS setting it found unchanged, and write exactly one record that
// states that setting. A start value of 0 selects the runtime's own default,
// the production case of rule 2; any other start value is a setting placed by
// the test, which the rule must not move either.
func TestSettleProcessorParallelism_LeavesTheSettingAndRecords(t *testing.T) {
	cases := []struct {
		name    string
		env     *string // nil: absent
		start   int     // 0: the runtime's default
		source  string
		hasCPUs bool
	}{
		{"absent, runtime default", nil, 0, parallelismDerived, true},
		{"absent, placed setting", nil, 3, parallelismDerived, true},
		{"empty, runtime default", ptr(""), 0, parallelismDerived, true},
		{"set", ptr("5"), 3, parallelismEnvironment, false},
		{"invalid", ptr("abc"), 3, parallelismEnvironment, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keepGOMAXPROCS(t)
			if tc.env == nil {
				unsetGOMAXPROCSEnv(t)
			} else {
				t.Setenv(gomaxprocsEnv, *tc.env)
			}
			// The runtime reads the variable only at process start, so the
			// setting the rule finds is the one placed here.
			if tc.start == 0 {
				runtime.SetDefaultGOMAXPROCS()
			} else {
				runtime.GOMAXPROCS(tc.start)
			}
			before := runtime.GOMAXPROCS(0)

			var captured syncBuffer
			settleProcessorParallelism(newLogger(&captured))

			if got := runtime.GOMAXPROCS(0); got != before {
				t.Errorf("GOMAXPROCS is %d after the rule ran, want the %d it found: the rule must not change it",
					got, before)
			}
			lines := strings.Split(strings.TrimRight(captured.String(), "\n"), "\n")
			if len(lines) != 1 {
				t.Fatalf("the rule wrote %d records, want exactly 1:\n%s", len(lines), captured.String())
			}
			r := parseParallelismRecord(t, lines[0])
			if r.source != tc.source || r.gomaxprocs != before || r.hasCPUs != tc.hasCPUs {
				t.Errorf("record %+v, want source=%s gomaxprocs=%d with cpus present=%v",
					r, tc.source, before, tc.hasCPUs)
			}
			if tc.hasCPUs && r.cpus != before {
				t.Errorf("record reports cpus=%d, want the setting the rule read, %d", r.cpus, before)
			}
		})
	}
}

// ptr returns a pointer to s, for a table field that distinguishes an absent
// value from an empty one.
func ptr(s string) *string { return &s }

// runtimeDefaultGOMAXPROCS returns the runtime's default GOMAXPROCS on this
// host, ignoring the environment, and leaves this process's setting as it found
// it. A child process started with no GOMAXPROCS variable starts with exactly
// this value: it shares this process's CPUs, affinity mask and cgroup quota.
func runtimeDefaultGOMAXPROCS(t *testing.T) int {
	t.Helper()
	keepGOMAXPROCS(t)
	runtime.SetDefaultGOMAXPROCS()
	return runtime.GOMAXPROCS(0)
}

// stderrAtAnnouncement waits for p to announce its socket and returns its
// stderr as it stood at that moment. The server flushes its startup records
// before it writes the announcement, so a record that is not here was written
// after the announcement.
func stderrAtAnnouncement(t *testing.T, p *serverProcess) string {
	t.Helper()

	deadline := time.Now().Add(serverStartDeadline)
	for {
		if out, err := os.ReadFile(p.stdout); err == nil && len(out) > 0 {
			return p.capturedStderr(t)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server announced nothing within %s.\nstderr:\n%s", serverStartDeadline, p.capturedStderr(t))
		}
		time.Sleep(time.Millisecond)
	}
}

// requireOneParallelismRecordFirst asserts that stderr holds exactly one
// processor-parallelism record, as its first line and ahead of the engine's two
// startup warnings, and returns it decoded.
func requireOneParallelismRecordFirst(t *testing.T, stderr string) parsedParallelism {
	t.Helper()

	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	if n := strings.Count(stderr, `msg="processor parallelism"`); n != 1 {
		t.Fatalf("stderr carries %d processor-parallelism records, want exactly 1:\n%s", n, stderr)
	}
	if len(lines) < 3 || strings.Count(stderr, "level=WARN") != 2 {
		t.Fatalf("stderr does not carry the record followed by the two startup warnings:\n%s", stderr)
	}
	return parseParallelismRecord(t, lines[0])
}

// TestServe_DerivesProcessorParallelismBeforeTheAnnouncement is acceptance
// criterion coverage for SPEC/GRAPH.md § Processor Parallelism, rules 1, 2 and
// 4, through the production entry point: a server started with no GOMAXPROCS
// variable runs at the runtime's default, which is the usable CPU count, leaves
// it unchanged, and states so in exactly one record that is on stderr before
// the socket is announced on stdout.
func TestServe_DerivesProcessorParallelismBeforeTheAnnouncement(t *testing.T) {
	cpus := runtimeDefaultGOMAXPROCS(t)
	unsetGOMAXPROCSEnv(t)

	p := launchServerProcess(t, graphRoot(t), "derived")
	early := stderrAtAnnouncement(t, p)
	p.waitUntilServed(t)
	p.requireAnnounced(t)

	r := requireOneParallelismRecordFirst(t, early)
	if r.source != parallelismDerived || !r.hasCPUs || r.cpus != cpus || r.gomaxprocs != cpus {
		t.Errorf("record %+v, want source=derived cpus=%d gomaxprocs=%d (the runtime default, unchanged)",
			r, cpus, cpus)
	}

	p.signalAndWait(t, syscall.SIGTERM)
	if n := strings.Count(p.capturedStderr(t), `msg="processor parallelism"`); n != 1 {
		t.Errorf("the whole run wrote %d processor-parallelism records, want exactly 1:\n%s",
			n, p.capturedStderr(t))
	}
}

// TestServe_LeavesAnEnvironmentParallelismUnchanged covers rule 3: a server
// started with GOMAXPROCS set keeps that value and records its source as the
// environment, with no CPU count.
func TestServe_LeavesAnEnvironmentParallelismUnchanged(t *testing.T) {
	t.Setenv(gomaxprocsEnv, "3")

	p := launchServerProcess(t, graphRoot(t), "environment")
	early := stderrAtAnnouncement(t, p)
	p.waitUntilServed(t)
	p.requireAnnounced(t)

	r := requireOneParallelismRecordFirst(t, early)
	if r.source != parallelismEnvironment || r.hasCPUs || r.gomaxprocs != 3 {
		t.Errorf("record %+v, want source=environment gomaxprocs=3 and no cpus attribute", r)
	}
	p.signalAndWait(t, syscall.SIGTERM)
}

// TestServe_ARefusedStartWritesNoParallelismRecord covers rule 4's last clause:
// a start refused before step 5 of § Server Startup — here at step 3, because a
// live server already answers on the socket — writes no processor-parallelism
// record.
func TestServe_ARefusedStartWritesNoParallelismRecord(t *testing.T) {
	root := graphRoot(t)
	stop := serveHandshakes(t, filepath.Join(root, "graph.sock"))
	defer stop()

	p := launchServerProcess(t, root, "refused")
	if code := waitForExit(t, p); code != 1 {
		t.Fatalf("the start against a socket another server answers on exited %d, want 1.\nstderr:\n%s",
			code, p.capturedStderr(t))
	}
	stderr := p.capturedStderr(t)
	if !strings.Contains(stderr, "already serving") {
		t.Fatalf("the start was not refused at step 3, so this test asserts nothing about it.\nstderr:\n%s", stderr)
	}
	if strings.Contains(stderr, processorParallelismMessage) {
		t.Errorf("a start refused before step 5 wrote a processor-parallelism record:\n%s", stderr)
	}
}
