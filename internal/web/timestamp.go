package web

import (
	"html/template"
	"strings"
)

// This file is the single Go source of the web interface's date and time
// display form (SPEC/WEB.md § Date and Time Display). Every server-rendered
// surface that displays a timestamp formats it through timestampHTML, the one
// helper registered in the template FuncMap; no template and no handler composes
// the display form by any other means. The modal script, static/task-modal.js,
// carries the one client-side counterpart, formatTimestamp, which applies the
// same rule and is compared against canonicalTimestampDisplay value by value.

// canonicalTimestampLen is the length of a stored timestamp in the canonical
// format of SPEC/DATA_FORMATS.md § Dates - ISO 8601 with UTC:
// YYYY-MM-DDTHH:mm:ss.sssZ.
const canonicalTimestampLen = len("2006-01-02T15:04:05.000Z")

// timestampPlaceholder is the neutral placeholder of an unset timestamp, the em
// dash the surfaces already use for an absent value (rule 7).
const timestampPlaceholder template.HTML = "&mdash;"

// timeElement renders one displayed timestamp. html/template escapes both the
// datetime attribute value and the text content contextually, so the markup is
// never built by concatenating an unescaped value.
var timeElement = template.Must(template.New("time").Parse(
	`{{if .Canonical}}<time datetime="{{.Stored}}">{{.Display}}</time>{{else}}{{.Stored}}{{end}}`,
))

// timeElementData is the dot of timeElement.
type timeElementData struct {
	Stored    string
	Display   string
	Canonical bool
}

// canonicalTimestampDisplay returns the display form YYYY-MM-DD HH:mm:ss of a
// stored timestamp in the canonical format, and true. The display form carries
// exactly the stored year, month, day, hour, minute, and second: no zone
// conversion, no locale, and the fractional seconds truncated, never rounded
// (rules 1 to 4).
//
// It returns "" and false when the value is not in the canonical format: not
// exactly the shape YYYY-MM-DDTHH:mm:ss.sssZ in ASCII digits, or not a real
// instant (a year of 0000, a month outside 01-12, a day beyond the month's
// length in the proleptic Gregorian calendar, an hour above 23, a minute or a
// second above 59). Such a value is not a valid global date and time string for
// the datetime attribute of the HTML time element, so it is displayed as stored
// (rule 7). The check is spelled out rather than delegated to time.Parse so the
// modal script's formatTimestamp can apply the identical rule byte for byte.
func canonicalTimestampDisplay(stored string) (string, bool) {
	if len(stored) != canonicalTimestampLen {
		return "", false
	}
	for i := 0; i < canonicalTimestampLen; i++ {
		c := stored[i]
		var ok bool
		switch i {
		case 4, 7:
			ok = c == '-'
		case 10:
			ok = c == 'T'
		case 13, 16:
			ok = c == ':'
		case 19:
			ok = c == '.'
		case 23:
			ok = c == 'Z'
		default:
			ok = c >= '0' && c <= '9'
		}
		if !ok {
			return "", false
		}
	}

	year := digits(stored[0:4])
	month := digits(stored[5:7])
	day := digits(stored[8:10])
	if year < 1 || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return "", false
	}
	if digits(stored[11:13]) > 23 || digits(stored[14:16]) > 59 || digits(stored[17:19]) > 59 {
		return "", false
	}
	return stored[0:10] + " " + stored[11:19], true
}

// digits returns the value of a run of ASCII digits the caller has validated.
func digits(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// daysInMonth returns the length of month (1-12) of year in the proleptic
// Gregorian calendar.
func daysInMonth(year, month int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	default:
		return 31
	}
}

// timestampHTML renders one displayed timestamp for a server-rendered page
// (SPEC/WEB.md § Date and Time Display). v is a stored timestamp as a string or
// a *string, the two forms the models carry.
//
//   - Unset (a nil pointer, or an empty value): the em dash placeholder and no
//     time element (rule 7).
//   - Canonical: <time datetime="STORED">YYYY-MM-DD HH:mm:ss</time>, with the
//     stored value verbatim in the attribute (rule 6).
//   - Set but not canonical: the stored text unchanged, escaped, with no time
//     element; the page does not fail (rule 7).
//
// Any other argument type is a template wiring error and renders the
// placeholder rather than failing the page.
func timestampHTML(v any) template.HTML {
	var stored string
	switch value := v.(type) {
	case string:
		stored = value
	case *string:
		if value == nil {
			return timestampPlaceholder
		}
		stored = *value
	default:
		return timestampPlaceholder
	}
	if stored == "" {
		return timestampPlaceholder
	}

	display, canonical := canonicalTimestampDisplay(stored)
	var b strings.Builder
	if err := timeElement.Execute(&b, timeElementData{Stored: stored, Display: display, Canonical: canonical}); err != nil {
		// Executing into a strings.Builder cannot fail on the writer, and the
		// template is parsed at init; fall back to the escaped stored text so a
		// display defect can never fail the page.
		return template.HTML(template.HTMLEscapeString(stored)) // #nosec G203 -- the stored value is HTML-escaped on this line
	}
	return template.HTML(b.String()) // #nosec G203 -- html/template produced and escaped this markup
}

// timestampFuncMap exposes the display helper to the page templates.
var timestampFuncMap = map[string]any{
	"timestamp": timestampHTML,
}
