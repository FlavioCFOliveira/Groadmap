package web

import (
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// logger is the web server's diagnostic logger: the console counterpart of the
// deliberately opaque HTTP responses. Every failure the server absorbs into an
// HTTP status is discarded from the response body and stated here instead
// (SPEC/WEB.md § Server Logging).
//
// It writes to stderr, never stdout: stdout carries only the startup success
// object, so a caller that reads stdout for the served URL is never disturbed
// by a log record (SPEC/WEB.md § Logger Configuration, rule 2).
//
// It is a package-level variable rather than a constant construction so a test
// can redirect it and assert what was recorded, not merely that something was.
// The web tests do not run in parallel, so an unsynchronised swap is safe;
// use captureLog in the test package rather than assigning it directly.
var logger = newLogger(os.Stderr)

// newLogger builds the server's logger over w: a slog.TextHandler emitting one
// line of key=value pairs per record, with DEBUG suppressed and every timestamp
// rewritten to the project's canonical ISO 8601 UTC
// (SPEC/WEB.md § Logger Configuration, rules 1, 3 and 5).
//
// TextHandler is what makes the log safe to read as an account of what
// happened: it quotes any value containing whitespace, a quotation mark, or a
// control character, and escapes a newline as the two characters '\' and 'n'.
// A request path or an error text carrying a newline followed by a forged
// `level=ERROR msg="..."` therefore stays inside its own quoted value and
// cannot terminate the record, so a crafted request cannot write a second,
// invented record onto the operator's console (SPEC/WEB.md § Log Integrity).
//
// The timestamp hook is internal/utils' rather than this package's. It was this
// package's until rmp task #386, which found `rmp graph serve` stamping its own
// stderr in local time because the rule had been implemented HERE instead of
// where the format lives: internal/utils already owned ISO8601Format and
// FormatISO8601, and a second expression of one format is how two surfaces come
// to answer the same question differently. Both long-lived surfaces now install
// the same hook, and there is one definition of the format in the module.
func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: utils.SlogTimestampUTC,
	}))
}

// logServerError records the one ERROR that accompanies every HTTP 500 the
// running server returns: the request that failed, the status the client was
// given, and the underlying error the response body withholds. Callers pass any
// route-specific subject (the task or sprint id, the audit page, the template)
// as extra attributes (SPEC/WEB.md § Record Content).
//
// msg is a constant phrase naming the condition, never an interpolated string,
// so every record of one condition groups with the others regardless of which
// request produced it. What varies belongs in the attributes.
func logServerError(r *http.Request, msg string, err error, attrs ...slog.Attr) {
	logRequest(slog.LevelError, r, msg, http.StatusInternalServerError, err, attrs...)
}

// logClientWarn records the one WARN that accompanies a failure the client
// caused and the server survived — today, the HTTP 400 the graph data endpoint
// returns for a rejected or failing query-bar query. The server did not fail,
// but the operator still needs to see what was refused and why
// (SPEC/WEB.md § Levels).
func logClientWarn(r *http.Request, msg string, status int, err error, attrs ...slog.Attr) {
	logRequest(slog.LevelWarn, r, msg, status, err, attrs...)
}

// logRequest emits one request-scoped record. The attribute order is the order
// an operator reads the record in: which request (method, path), about what
// (the caller's roadmap and subject attributes), what the client got (status),
// and why (err).
func logRequest(level slog.Level, r *http.Request, msg string, status int, err error, attrs ...slog.Attr) {
	args := make([]any, 0, len(attrs)+4)
	args = append(args, slog.String("method", r.Method), slog.String("path", r.URL.Path))
	for _, a := range attrs {
		args = append(args, a)
	}
	args = append(args, slog.Int("status", status), slog.String("err", errText(err)))
	logger.Log(r.Context(), level, msg, args...)
}

// errText renders err for the err attribute. A nil error would mean a caller
// logged a failure it could not name; it is rendered explicitly rather than as
// an empty value, so the gap is visible in the log instead of looking like an
// error with no message.
func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
