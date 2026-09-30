package web

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// The two refusal lines of the request guard. Each is the whole body of the
// 403 it answers, followed by one line feed, and the err attribute of the WARN
// record that accompanies it (SPEC/WEB.md § Security and Constraints, rule 15;
// § Record Content, rule 3).
const (
	refusedHostLine   = "host not allowed"
	refusedOriginLine = "origin not allowed"
)

// errHostNotAllowed and errOriginNotAllowed are the two refusals as errors, so
// the record's err attribute is the body line the response carries: a refusal
// has no underlying error and withholds nothing (SPEC/WEB.md § Record Content,
// rule 3).
var (
	errHostNotAllowed   = errors.New(refusedHostLine)
	errOriginNotAllowed = errors.New(refusedOriginLine)
)

// httpDefaultPort is the default port of the http scheme (RFC 9110,
// Section 4.2.1). A browser omits the port from the host it sends for this port
// and for no other, so it is the one bound port under which a host without a
// port is admitted, and the port an Origin without one is read as
// (SPEC/WEB.md § Security and Constraints, rules 13 and 14).
const httpDefaultPort = 80

// bindClass is the classification of a bind host that decides which host parts
// a request may name. It is the classification the network-exposure warning
// makes, extended by the one distinction the warning does not need: an
// unspecified host binds every interface (SPEC/WEB.md § Security and
// Constraints, rule 13, the allowlist table).
type bindClass uint8

const (
	// bindLoopback is `localhost` or any loopback address.
	bindLoopback bindClass = iota + 1
	// bindUnspecified is `0.0.0.0`, `::`, or an empty host: every interface.
	bindUnspecified
	// bindSpecific is any other address or name.
	bindSpecific
)

// hostPolicy is what the request guard knows about the listener it protects:
// the value given to --host and the port the listener is actually bound to,
// which differs from the requested port under the ephemeral fallback and under
// --port 0. It is fixed once the listener is bound and never changes while the
// server runs.
type hostPolicy struct {
	// bindHost is the value given to --host, as given.
	bindHost string
	// port is the bound port, the one the startup URL reports.
	port int
	// class is bindHost's classification.
	class bindClass
}

// newHostPolicy classifies bindHost and records the bound port.
func newHostPolicy(bindHost string, port int) hostPolicy {
	class := bindSpecific
	switch {
	case isLoopbackHost(bindHost):
		class = bindLoopback
	case bindHost == "":
		class = bindUnspecified
	default:
		if ip := net.ParseIP(bindHost); ip != nil && ip.IsUnspecified() {
			class = bindUnspecified
		}
	}
	return hostPolicy{bindHost: bindHost, port: port, class: class}
}

// allowsHost reports whether a request naming host — its Host field, or the
// authority of an absolute-form request target — may be served (SPEC/WEB.md
// § Security and Constraints, rule 13).
//
// The port must be the bound port; a host without one is admitted only when the
// bound port is 80. The host part must then be one the bind class admits:
// loopback admits 127.0.0.1, localhost, [::1] and the bind host itself;
// unspecified admits localhost and any IP literal; any other bind admits the
// bind host itself and nothing else. An empty host — an HTTP/1.0 request with no
// Host field — names no host and is refused.
func (p hostPolicy) allowsHost(host string) bool {
	part, port, ok := splitRequestHost(host)
	if !ok {
		return false
	}
	if port != p.port {
		return false
	}

	switch p.class {
	case bindLoopback:
		return sameHostPart(part, "127.0.0.1") || sameHostPart(part, "localhost") ||
			sameHostPart(part, "[::1]") || sameHostPart(part, bindHostPart(p.bindHost))
	case bindUnspecified:
		if sameHostPart(part, "localhost") {
			return true
		}
		_, isLiteral := hostPartIP(part)
		return isLiteral
	default:
		return sameHostPart(part, bindHostPart(p.bindHost))
	}
}

// bindHostPart renders a --host value as the host part a request names for it:
// an IPv6 address is written in square brackets, as it appears in a URI's
// authority (RFC 3986, Section 3.2.2), and every other value unchanged.
func bindHostPart(bindHost string) string {
	if ip := net.ParseIP(bindHost); ip != nil && strings.Contains(bindHost, ":") {
		return "[" + bindHost + "]"
	}
	return bindHost
}

// splitRequestHost splits a request host into its host part and its port, an
// absent port reading as 80. It reports false for a value that is not a host
// with an optional decimal port: an empty value, an IPv6 address outside square
// brackets, an unterminated bracket, or a port that is empty or not a decimal
// number.
func splitRequestHost(host string) (part string, port int, ok bool) {
	if host == "" {
		return "", 0, false
	}

	rest := ""
	if strings.HasPrefix(host, "[") {
		end := strings.IndexByte(host, ']')
		if end < 0 {
			return "", 0, false
		}
		part, rest = host[:end+1], host[end+1:]
	} else {
		part = host
		if i := strings.LastIndexByte(host, ':'); i >= 0 {
			part, rest = host[:i], host[i:]
		}
		if part == "" || strings.Contains(part, ":") {
			return "", 0, false
		}
	}

	if rest == "" {
		return part, httpDefaultPort, true
	}
	digits, found := strings.CutPrefix(rest, ":")
	if !found || digits == "" || strings.Trim(digits, "0123456789") != "" || len(digits) > 5 {
		return "", 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return "", 0, false
	}
	return part, n, true
}

// hostPartIP parses a host part as an IP literal: a dotted-quad IPv4 address,
// or an IPv6 address in square brackets. A name, and an IPv4 address in square
// brackets, are not IP literals.
func hostPartIP(part string) (net.IP, bool) {
	if inner, found := strings.CutPrefix(part, "["); found {
		inner, closed := strings.CutSuffix(inner, "]")
		if !closed || !strings.Contains(inner, ":") {
			return nil, false
		}
		ip := net.ParseIP(inner)
		return ip, ip != nil
	}
	if strings.Contains(part, ":") {
		return nil, false
	}
	ip := net.ParseIP(part)
	return ip, ip != nil
}

// sameHostPart compares two host parts as SPEC/WEB.md § Security and
// Constraints, rule 13, compares them: two IP literals match when they denote
// the same address, two names match when they are equal ignoring ASCII case
// (RFC 3986, Section 3.2.2), and an IP literal never matches a name. A name with
// a trailing dot is a different name.
func sameHostPart(a, b string) bool {
	ipA, literalA := hostPartIP(a)
	ipB, literalB := hostPartIP(b)
	if literalA || literalB {
		return literalA && literalB && ipA.Equal(ipB)
	}
	return equalFoldASCII(a, b)
}

// equalFoldASCII reports whether a and b are equal under ASCII case folding
// only. strings.EqualFold folds by Unicode, which would equate a name carrying a
// non-ASCII letter with an ASCII one the server was told (for example the
// Kelvin sign with K), and a host name is compared ignoring ASCII case alone.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

// lowerASCII lowers one ASCII upper-case letter and leaves every other byte
// unchanged.
func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// allowsSite reports whether a request whose host has already been admitted
// was made by this interface's own pages or by the user directly, rather than
// by a browser on behalf of another site (SPEC/WEB.md § Security and
// Constraints, rule 14).
//
// Either field refuses the request. Sec-Fetch-Site is admitted only when it
// occurs once with the value same-origin or none. Origin is admitted only when
// it occurs once and names this interface's own origin for the request: the
// http scheme, the request's host part, and the request's port, an absent port
// reading as 80 on either side. A request carrying neither field is served.
//
// No method is exempt. net/http.CrossOriginProtection examines the same two
// fields but always admits GET, HEAD and OPTIONS, and the graph data endpoint
// writes on a GET, so that type does not satisfy the rule on its own.
func allowsSite(r *http.Request, requestHost string) bool {
	if values, present := r.Header["Sec-Fetch-Site"]; present {
		if len(values) != 1 || (values[0] != "same-origin" && values[0] != "none") {
			return false
		}
	}
	if values, present := r.Header["Origin"]; present {
		if len(values) != 1 || !sameOrigin(values[0], requestHost) {
			return false
		}
	}
	return true
}

// sameOrigin reports whether origin, the serialised value of an Origin field
// (RFC 6454, Section 7), names the http origin of requestHost. The opaque origin
// `null`, any other scheme, and any value carrying more than a scheme and an
// authority are refused.
func sameOrigin(origin, requestHost string) bool {
	authority, found := strings.CutPrefix(origin, "http://")
	if !found || strings.ContainsAny(authority, "/?#@") {
		return false
	}
	originPart, originPort, ok := splitRequestHost(authority)
	if !ok {
		return false
	}
	requestPart, requestPort, ok := splitRequestHost(requestHost)
	if !ok {
		return false
	}
	return originPort == requestPort && sameHostPart(originPart, requestPart)
}

// requestHost is the host a request names: the authority of an absolute-form
// request target, which takes precedence over the Host field (RFC 9112,
// Section 3.2.2), or else the Host field.
func requestHost(r *http.Request) string {
	if r.URL != nil && r.URL.Host != "" {
		return r.URL.Host
	}
	return r.Host
}

// guardRequests is the middleware that decides, before any route is matched,
// whether a request is served at all (SPEC/WEB.md § Security and Constraints,
// rules 13 to 15).
//
// It runs on every request, /static/... included, ahead of the router, the
// method check, the roadmap-name validation, and every filesystem, database, or
// socket access, so a refused request reaches no handler: it reads no roadmap,
// resolves no graph server, and sends no statement.
//
// The host check runs first, so a request that fails both is refused for its
// host. A refusal is answered 403 with a fixed one-line plain-text body naming
// the check that failed, carries Cache-Control: no-store whatever its path —
// the answer depends on request fields no cache keys on — and is recorded by
// exactly one WARN record.
func guardRequests(policy hostPolicy, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := requestHost(r)
		switch {
		case !policy.allowsHost(host):
			refuseRequest(w, r, host, errHostNotAllowed)
		case !allowsSite(r, host):
			refuseRequest(w, r, host, errOriginNotAllowed)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// refuseRequest answers a request the guard refused and records it.
//
// The record carries no roadmap and no route subject, because no route was
// matched. It carries the three values the checks examined, each as received:
// the request's host, the Origin field, and the Sec-Fetch-Site field, a field
// present more than once being recorded by its occurrences joined with ", "
// (SPEC/WEB.md § Record Content, rule 5). Each is a value the client chose, and
// the logger's quoting keeps it inside its own attribute (§ Log Integrity).
func refuseRequest(w http.ResponseWriter, r *http.Request, host string, refusal error) {
	line := refusal.Error()
	logClientWarn(r, "request refused: "+line, http.StatusForbidden, refusal,
		slog.String("host", host),
		slog.String("origin", strings.Join(r.Header["Origin"], ", ")),
		slog.String("sec_fetch_site", strings.Join(r.Header["Sec-Fetch-Site"], ", ")))

	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, line, http.StatusForbidden)
}
