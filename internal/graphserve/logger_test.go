// Package graphserve — the regression suite for rmp task #386: every timestamp
// this server writes to stderr is the project's canonical ISO 8601 UTC.
//
// # What was wrong
//
// The handler was built without a ReplaceAttr hook, so slog.TextHandler stamped
// records in the machine's LOCAL zone with a numeric offset —
// `time=2026-09-03T11:51:05.221+01:00`. SPEC/DATA_FORMATS.md § Dates - ISO 8601
// with UTC requires `YYYY-MM-DDTHH:mm:ss.sssZ` of every Groadmap timestamp, so
// the product was emitting, on its own stderr, a shape its own specification
// forbids. `rmp web` had had the rule, the implementation and a test since
// SPEC/WEB.md Acceptance Criterion 144; this surface had none of the three.
//
// # Why the engine's records are in scope
//
// The two startup warnings are the ENGINE's — it calls Warn with the message —
// but the engine supplies no timestamp: slog builds the time attribute inside
// the HANDLER, and the handler is Groadmap's. So `rmp` does not relay those
// lines, it writes them, and the rule binds them.
package graphserve

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// canonicalStamp is the shape SPEC/DATA_FORMATS.md § Dates - ISO 8601 with UTC
// fixes: a date, T, a time with exactly three digits of milliseconds, and a
// literal Z. Anchored at both ends so a local offset cannot satisfy it.
var canonicalStamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// recordStamp extracts the value of a record's leading time attribute.
var recordStamp = regexp.MustCompile(`^time=(\S+) `)

// TestLogger_TimestampIsCanonicalUTC pins the timestamp against BOTH halves of
// the rule: the SHAPE, and the INSTANT.
//
// The distinction is the whole point. time.Local is forced to a fixed +09:00
// zone first, so that: a handler with no hook at all emits `+09:00` and fails
// the shape; and a hook that REFORMATTED the local reading rather than
// converting it would pass the shape while being nine hours wrong, which only
// the instant check catches. Checking one without the other would admit one of
// the two mistakes.
//
// It is deliberately the same test internal/web has for the same rule
// (TestLogTimestampIsCanonicalUTC), because the point of task #386 is that the
// two long-lived surfaces answer this question identically.
func TestLogger_TimestampIsCanonicalUTC(t *testing.T) {
	savedLocal := time.Local
	time.Local = time.FixedZone("TEST+09", 9*60*60)
	t.Cleanup(func() { time.Local = savedLocal })

	var captured syncBuffer
	probe := newLogger(&captured)

	before := time.Now().UTC()
	probe.Error("probe record")
	after := time.Now().UTC()

	record := strings.TrimSpace(captured.String())
	if record == "" {
		t.Fatalf("the logger wrote nothing")
	}

	match := recordStamp.FindStringSubmatch(record)
	if match == nil {
		t.Fatalf("the record has no leading time attribute\nrecord: %s", record)
	}
	stamp := match[1]

	if !canonicalStamp.MatchString(stamp) {
		t.Fatalf("timestamp %q is not YYYY-MM-DDTHH:mm:ss.sssZ. A local-zone offset here means the "+
			"handler has no UTC hook, which is the defect of rmp task #386 "+
			"(SPEC/DATA_FORMATS.md § Dates - ISO 8601 with UTC)", stamp)
	}

	got, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatalf("timestamp %q does not parse as RFC 3339: %v", stamp, err)
	}
	// The record is millisecond-precise, so truncate the lower bound to match.
	low := before.Truncate(time.Millisecond)
	if got.Before(low) || got.After(after) {
		t.Errorf("timestamp %s is outside [%s, %s]: the local reading was REFORMATTED with a Z "+
			"rather than converted to UTC, so the shape is right and the instant is wrong by the "+
			"machine's offset",
			got.Format(time.RFC3339Nano), low.Format(time.RFC3339Nano), after.Format(time.RFC3339Nano))
	}
}

// TestLogger_TheEnginesStartupWarningsAreStampedByThisPackage drives a REAL
// server and requires that the two warnings the engine emits at construction
// arrive, and arrive in the project's timestamp format.
//
// This is the assertion the unit test above cannot make. That one proves the
// handler stamps correctly; this one proves the handler is what stamps the
// records Groadmap does not author — which is the finding the task turned on,
// and the reason "scope the rule to rmp's own emissions" was not the cheap
// option it looked like.
//
// The local zone is forced for the same reason as above: without it a machine
// already running in UTC would pass this test whether or not the hook exists.
func TestLogger_TheEnginesStartupWarningsAreStampedByThisPackage(t *testing.T) {
	savedLocal := time.Local
	time.Local = time.FixedZone("TEST+09", 9*60*60)
	t.Cleanup(func() { time.Local = savedLocal })

	var captured syncBuffer
	_, _, stop := startRealServerLogging(t, productionCadence(), newLogger(&captured))
	stop()

	records := strings.Split(strings.TrimRight(captured.String(), "\n"), "\n")
	if len(records) == 1 && records[0] == "" {
		t.Fatalf("the server wrote no records at all; the two startup warnings are expected " +
			"(SPEC/GRAPH.md § Socket Path and Permissions, rule 6)")
	}

	// 1. Every record carries a canonical stamp, whoever produced it.
	for _, record := range records {
		match := recordStamp.FindStringSubmatch(record)
		if match == nil {
			t.Errorf("a record has no leading time attribute\nrecord: %s", record)
			continue
		}
		if !canonicalStamp.MatchString(match[1]) {
			t.Errorf("timestamp %q is not YYYY-MM-DDTHH:mm:ss.sssZ. This record is the ENGINE's, and "+
				"it is stamped by THIS package's handler: the hook is missing or is not reaching "+
				"records rmp did not author (rmp task #386)\nrecord: %s", match[1], record)
		}
	}

	// 2. The two warnings are still there. They are the engine's words, and a
	//    reword upstream is a legitimate reason for this half to fail — but a
	//    silent DISAPPEARANCE is not, and that is what it guards.
	var warnings []string
	for _, record := range records {
		if strings.Contains(record, "level=WARN") {
			warnings = append(warnings, record)
		}
	}
	if len(warnings) != 2 {
		t.Fatalf("got %d WARN records at startup, want the 2 the engine emits — one for no "+
			"authentication and one for no TLS. Both are expected and neither is a failure "+
			"(SPEC/GRAPH.md § Socket Path and Permissions, rule 6)\nrecords:\n%s",
			len(warnings), strings.Join(records, "\n"))
	}
	joined := strings.Join(warnings, "\n")
	for _, subject := range []string{"no authentication", "no TLS"} {
		if !strings.Contains(joined, subject) {
			t.Errorf("neither startup warning mentions %q. The two conditions are deliberate and "+
				"are published as expected output, so an operator must still be told about both.\n"+
				"warnings:\n%s", subject, joined)
		}
	}
}
