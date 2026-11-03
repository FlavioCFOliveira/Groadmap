package graphserve

import (
	"context"
	"log/slog"
	"os"
	"runtime"
)

// gomaxprocsEnv is the environment variable through which an operator fixes the
// Go runtime's GOMAXPROCS setting. Its presence with a non-empty value is what
// selects rule 3 of SPEC/GRAPH.md § Processor Parallelism.
const gomaxprocsEnv = "GOMAXPROCS"

// The two values of the processor-parallelism record's source attribute
// (SPEC/GRAPH.md § Processor Parallelism, rule 4).
const (
	// parallelismDerived states that the server left the runtime's default in
	// force and changed nothing, because the environment fixed no value
	// (rule 2).
	parallelismDerived = "derived"

	// parallelismEnvironment states that the server left the environment's value
	// in force and changed nothing (rule 3).
	parallelismEnvironment = "environment"
)

// processorParallelismMessage is the message of the one record a start writes to
// state the parallelism it settled (SPEC/GRAPH.md § Processor Parallelism,
// rule 4).
const processorParallelismMessage = "processor parallelism"

// processorParallelism classifies the GOMAXPROCS setting a server start finds in
// force, from its inputs alone: the GOMAXPROCS environment variable's value,
// whether the variable is present at all, and the runtime's setting read before
// anything changed it. It touches no process state, which is what lets every
// branch of the rule be tested without changing the setting of the test
// process. Under no branch does the rule change the setting, so it returns no
// value to apply.
//
// The variable counts as not set when it is absent and when it is present with
// an empty value. In that case the setting in force is the runtime's default,
// and the result reports the source as derived and runtimeDefault as the usable
// CPU count (rule 2): the runtime derives that default from the machine's
// logical CPUs, the process's CPU affinity mask and, on Linux, its cgroup CPU
// quota, so the count is the host's own and no value is fixed here.
//
// Any non-empty value — a valid one or not — is the operator's explicit choice,
// and the result reports the source as environment and no CPU count (cpus is
// 0), because the runtime's derived default cannot be observed while an
// environment value is in force without changing the setting (rule 3 and
// rule 4).
func processorParallelism(envValue string, envSet bool, runtimeDefault int) (source string, cpus int) {
	if envSet && envValue != "" {
		return parallelismEnvironment, 0
	}
	return parallelismDerived, runtimeDefault
}

// settleProcessorParallelism applies SPEC/GRAPH.md § Processor Parallelism to
// this process and writes the one record that states the result to log.
//
// It never sets GOMAXPROCS: under rule 2 the runtime's default stays in force,
// so the runtime's automatic updates of that default stay active while the
// server serves (rule 5), and under rule 3 the environment's value stays in
// force. It only reads the setting, which runtime.GOMAXPROCS(0) does without
// changing it.
//
// It is called once per start, before the listener is bound and before the
// store is opened (§ Server Startup, step 5), so the setting is established
// before any connection is accepted and before any engine work runs. The record
// is queued like every other startup record and reaches stderr at the flush
// that precedes the socket announcement.
//
// The classification is [processorParallelism]'s; this function only reads the
// environment and the runtime's current setting and reports them.
func settleProcessorParallelism(log *slog.Logger) {
	value, set := os.LookupEnv(gomaxprocsEnv)
	// GOMAXPROCS(0) reads the setting without changing it. Nothing in this
	// process changes it, so it is the setting in force, and under rule 2 the
	// runtime's default.
	gomaxprocs := runtime.GOMAXPROCS(0)
	source, cpus := processorParallelism(value, set, gomaxprocs)

	attrs := []slog.Attr{
		slog.String("source", source),
		slog.Int("gomaxprocs", gomaxprocs),
	}
	if source == parallelismDerived {
		attrs = append(attrs, slog.Int("cpus", cpus))
	}
	log.LogAttrs(context.Background(), slog.LevelInfo, processorParallelismMessage, attrs...)
}
