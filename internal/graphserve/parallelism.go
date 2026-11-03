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
	// parallelismDerived states that the server applied twice the usable CPU
	// count, because the environment fixed no value (rule 2).
	parallelismDerived = "derived"

	// parallelismEnvironment states that the server left the environment's value
	// in force and changed nothing (rule 3).
	parallelismEnvironment = "environment"
)

// processorParallelismMessage is the message of the one record a start writes to
// state the parallelism it settled (SPEC/GRAPH.md § Processor Parallelism,
// rule 4).
const processorParallelismMessage = "processor parallelism"

// processorParallelism decides the GOMAXPROCS setting a server start applies,
// from its inputs alone: the GOMAXPROCS environment variable's value, whether
// the variable is present at all, and the runtime's own default setting read
// before anything changed it. It touches no process state, which is what lets
// every branch of the rule be tested without changing the setting of the test
// process.
//
// The variable counts as not set when it is absent and when it is present with
// an empty value. In that case the result applies twice runtimeDefault, reports
// the source as derived, and reports runtimeDefault as the usable CPU count
// (rule 2): the runtime derives that default from the machine's logical CPUs,
// the process's CPU affinity mask and, on Linux, its cgroup CPU quota, so the
// count is the host's own and no value is fixed here.
//
// Any non-empty value — a valid one or not — is the operator's explicit choice,
// and the result applies nothing (apply is 0), reports the source as
// environment, and reports no CPU count (cpus is 0), because the runtime's
// derived default cannot be observed while an environment value is in force
// without changing the setting (rule 3 and rule 4).
func processorParallelism(envValue string, envSet bool, runtimeDefault int) (apply int, source string, cpus int) {
	if envSet && envValue != "" {
		return 0, parallelismEnvironment, 0
	}
	return 2 * runtimeDefault, parallelismDerived, runtimeDefault
}

// settleProcessorParallelism applies SPEC/GRAPH.md § Processor Parallelism to
// this process and writes the one record that states the result to log.
//
// It is called once per start, before the listener is bound and before the
// store is opened (§ Server Startup, step 5), so no connection is accepted and
// no engine work runs under any other value. The record is queued like every
// other startup record and reaches stderr at the flush that precedes the socket
// announcement.
//
// The decision is [processorParallelism]'s; this function only reads the
// environment and the runtime's current setting, applies the decision, and
// reports the setting in force afterwards.
func settleProcessorParallelism(log *slog.Logger) {
	value, set := os.LookupEnv(gomaxprocsEnv)
	// GOMAXPROCS(0) reads the setting without changing it. Nothing in this
	// process has changed it before this call, so it is the runtime's default.
	apply, source, cpus := processorParallelism(value, set, runtime.GOMAXPROCS(0))
	if apply > 0 {
		runtime.GOMAXPROCS(apply)
	}

	attrs := []slog.Attr{
		slog.String("source", source),
		slog.Int("gomaxprocs", runtime.GOMAXPROCS(0)),
	}
	if source == parallelismDerived {
		attrs = append(attrs, slog.Int("cpus", cpus))
	}
	log.LogAttrs(context.Background(), slog.LevelInfo, processorParallelismMessage, attrs...)
}
