package web

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// This file is the small CSS cascade resolver behind the contrast gates of the
// tasks list and the sprint board (SPEC/WEB.md § Acceptance Criteria 245, 246,
// and 247). It reads the two stylesheets every page loads, in the order the pages
// load them, and answers one question: which colour does an element of a
// described shape resolve to in the dark theme, and against which background.
//
// It implements exactly the steps Acceptance Criterion 246 fixes, and fails —
// it never guesses — when a value lies outside them:
//
//  1. The winning declaration of a property is chosen by the CSS cascade:
//     importance, then specificity, then order of appearance. Selectors are
//     matched against a described element and its ancestors; a rule inside a
//     media query applies when its condition holds at the viewport width being
//     evaluated.
//  2. var() is substituted from the custom properties the element inherits —
//     its own winning declaration, else its parent's, up to the root element —
//     using the reference's fallback where the property is undeclared. An
//     opacity property the badge does not set therefore resolves to the
//     fallback of 1 the vendored rules write.
//  3. light-dark(<light>, <dark>) resolves to <dark>.
//  4. color-mix(in oklab, <colour> <p>%, transparent) is <colour> with an alpha
//     of <p>/100; oklch(), oklab(), rgb(), and hexadecimal colours are
//     converted to sRGB by CSS Color 4.
//  5. A translucent colour is composited over what lies beneath it by
//     source-over alpha compositing in sRGB.
//  6. The contrast ratio is WCAG 2.2's.
//
// Anything else — a two-colour color-mix(), relative colour syntax, a named
// colour other than transparent, white, and black, an unknown pseudo-class, an
// at-rule the resolver does not know — is an error, so a contrast gate fails
// loudly instead of measuring a value it did not resolve.

// ==================== THE STYLESHEET MODEL ====================

// cascadeDecl is one declaration of a style rule.
type cascadeDecl struct {
	prop      string
	value     string
	important bool
}

// cascadeRule is one style rule: its selector list, its declarations, the media
// conditions of the at-rules enclosing it (all of which must hold), and its
// position in the concatenated stylesheets.
type cascadeRule struct {
	selectors []cascadeSelector
	decls     []cascadeDecl
	media     []string
	order     int
}

// cascadeSelector is one complex selector: its compounds from left to right and
// the combinator joining each compound to the next (' ', '>', '+', '~').
type cascadeSelector struct {
	compounds     []cascadeCompound
	combinators   []byte
	spec          [3]int
	pseudoElement string
}

// cascadeCompound is one compound selector. anchor, when set, matches that one
// element and nothing else: it stands for the subject of a :has() argument.
type cascadeCompound struct {
	anchor        *cssElement
	tag           string
	pseudoElement string
	ids           []string
	classes       []string
	attrs         []cascadeAttr
	pseudos       []cascadePseudo
}

// cascadeAttr is one attribute selector.
type cascadeAttr struct {
	name, op, value string
}

// cascadePseudo is one pseudo-class, with its argument text and, for the
// pseudo-classes that take a selector list, that list parsed. relative marks a
// :has() argument, whose selectors begin with an implied or written combinator.
type cascadePseudo struct {
	name     string
	args     string
	list     []cascadeSelector
	relative bool
}

// cssElement is a described element: a tag, an id, classes, attributes, its
// parent, its one child on the described path, and its state. The structural
// position (pos of count siblings) is 1 of 1 unless the description says
// otherwise, and preceding siblings are not modelled: a sibling combinator
// matches nothing.
type cssElement struct {
	parent  *cssElement
	child   *cssElement
	attrs   map[string]string
	state   map[string]bool
	tag     string
	id      string
	classes []string
	pos     int
	count   int
}

// ==================== LEXICAL HELPERS ====================

// cascadeStripComments removes every comment, leaving quoted strings intact, so
// a comment marker inside a string cannot cut the sheet.
func cascadeStripComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += 2 + end + 1
		case c == '"' || c == '\'':
			j := i + 1
			for ; j < len(src) && src[j] != c; j++ {
				if src[j] == '\\' {
					j++
				}
			}
			if j >= len(src) {
				j = len(src) - 1
			}
			b.WriteString(src[i : j+1])
			i = j
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// cascadeScan returns the index of the first byte of src at or after from that is
// one of stops and lies outside every string, parenthesis, and bracket, or -1.
func cascadeScan(src string, from int, stops string) int {
	depth := 0
	for i := from; i < len(src); i++ {
		c := src[i]
		switch {
		case depth == 0 && strings.IndexByte(stops, c) >= 0:
			return i
		case c == '"' || c == '\'':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' {
					i++
				}
			}
		case c == '\\':
			i++
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		}
	}
	return -1
}

// cascadeBlockEnd returns the index of the brace closing the block opened at
// open, skipping strings, or -1.
func cascadeBlockEnd(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		c := src[i]
		switch c {
		case '"', '\'':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' {
					i++
				}
			}
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// cascadeSplit splits s on sep where sep lies outside every string, parenthesis,
// and bracket.
func cascadeSplit(s string, sep byte) []string {
	var out []string
	for {
		at := cascadeScan(s, 0, string(sep))
		if at < 0 {
			return append(out, s)
		}
		out = append(out, s[:at])
		s = s[at+1:]
	}
}

// cascadeFields splits a value into its top-level whitespace-separated tokens,
// keeping a function call with its parenthesised arguments as one token.
func cascadeFields(s string) []string {
	var (
		out   []string
		depth int
		start = -1
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '(':
			depth++
		case c == ')':
			depth--
		}
		space := c == ' ' || c == '\t' || c == '\n' || c == '\r'
		switch {
		case space && depth == 0:
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		case start < 0:
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// ==================== PARSING THE STYLESHEETS ====================

// parseCascadeSheet appends the style rules of one stylesheet to rules, each
// numbered after the ones already there, so concatenating the sheets in load
// order gives the order of appearance the cascade compares.
func parseCascadeSheet(src string, rules []cascadeRule) ([]cascadeRule, error) {
	return parseCascadeBlock(cascadeStripComments(src), nil, rules)
}

// parseCascadeBlock parses a list of rules, as found at the top level of a sheet
// or inside a conditional group rule.
func parseCascadeBlock(src string, media []string, rules []cascadeRule) ([]cascadeRule, error) {
	for i := 0; i < len(src); {
		for i < len(src) && strings.IndexByte(" \t\r\n", src[i]) >= 0 {
			i++
		}
		if i >= len(src) {
			break
		}
		stop := cascadeScan(src, i, "{;}")
		if stop < 0 {
			return nil, fmt.Errorf("unterminated rule at %q", clip(src[i:]))
		}
		prelude := strings.TrimSpace(src[i:stop])
		switch src[stop] {
		case '}':
			return nil, fmt.Errorf("unbalanced closing brace after %q", clip(prelude))
		case ';':
			if !strings.HasPrefix(prelude, "@charset") {
				return nil, fmt.Errorf("unsupported statement %q", clip(prelude))
			}
			i = stop + 1
			continue
		}
		end := cascadeBlockEnd(src, stop)
		if end < 0 {
			return nil, fmt.Errorf("unterminated block after %q", clip(prelude))
		}
		body := src[stop+1 : end]
		i = end + 1

		if strings.HasPrefix(prelude, "@") {
			name, cond, _ := strings.Cut(prelude[1:], " ")
			if paren := strings.IndexByte(name, '('); paren >= 0 {
				name, cond = name[:paren], name[paren:]+" "+cond
			}
			switch strings.ToLower(name) {
			case "media":
				inner := append(append([]string(nil), media...), strings.TrimSpace(cond))
				var err error
				if rules, err = parseCascadeBlock(body, inner, rules); err != nil {
					return nil, err
				}
			case "keyframes", "-webkit-keyframes", "property", "font-face", "page":
				// Not style rules: they declare no property of an element.
			default:
				return nil, fmt.Errorf("unsupported at-rule @%s", name)
			}
			continue
		}

		selectors, dropped, err := parseCascadeSelectorList(prelude)
		if err != nil {
			return nil, fmt.Errorf("selector %q: %w", clip(prelude), err)
		}
		if dropped {
			// A selector list naming a pseudo-class or pseudo-element another
			// engine alone defines is invalid in Chromium, and an invalid
			// selector list drops the whole rule.
			continue
		}
		decls, err := parseCascadeDecls(body)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", clip(prelude), err)
		}
		rules = append(rules, cascadeRule{selectors: selectors, decls: decls, media: media, order: len(rules)})
	}
	return rules, nil
}

// parseCascadeDecls parses a declaration block.
func parseCascadeDecls(body string) ([]cascadeDecl, error) {
	var decls []cascadeDecl
	for _, raw := range cascadeSplit(body, ';') {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if cascadeScan(raw, 0, "{") >= 0 {
			return nil, fmt.Errorf("nested rule in %q", clip(raw))
		}
		name, value, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, fmt.Errorf("declaration %q has no colon", clip(raw))
		}
		d := cascadeDecl{prop: strings.ToLower(strings.TrimSpace(name)), value: strings.TrimSpace(value)}
		if bang := strings.LastIndexByte(d.value, '!'); bang >= 0 &&
			strings.EqualFold(strings.TrimSpace(d.value[bang+1:]), "important") {
			d.important = true
			d.value = strings.TrimSpace(d.value[:bang])
		}
		decls = append(decls, d)
	}
	return decls, nil
}

// clip shortens a fragment for an error message.
func clip(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// ==================== SELECTORS ====================

// legacyPseudoElements are the pseudo-elements CSS 2 wrote with one colon.
var legacyPseudoElements = map[string]bool{"before": true, "after": true, "first-line": true, "first-letter": true}

// parseCascadeSelectorList parses a selector list. dropped reports a list that
// Chromium rejects as a whole because one of its selectors uses a pseudo-class
// or pseudo-element prefixed for another engine.
func parseCascadeSelectorList(s string) (list []cascadeSelector, dropped bool, err error) {
	for _, part := range cascadeSplit(s, ',') {
		sel, drop, err := parseCascadeSelector(strings.TrimSpace(part), false)
		if err != nil {
			return nil, false, err
		}
		if drop {
			dropped = true
		}
		list = append(list, sel)
	}
	return list, dropped, nil
}

// parseCascadeSelector parses one complex selector. relative admits a leading
// combinator, as a :has() argument writes it; the implied leading compound is
// then left empty for the caller to anchor.
func parseCascadeSelector(s string, relative bool) (sel cascadeSelector, dropped bool, err error) {
	if s == "" {
		return sel, false, errors.New("empty selector")
	}
	if relative {
		sel.compounds = append(sel.compounds, cascadeCompound{})
		comb := byte(' ')
		if c := s[0]; c == '>' || c == '+' || c == '~' {
			comb = c
			s = strings.TrimSpace(s[1:])
		}
		sel.combinators = append(sel.combinators, comb)
	}
	i := 0
	for i < len(s) {
		compound, next, drop, err := parseCascadeCompound(s, i)
		if err != nil {
			return sel, false, err
		}
		dropped = dropped || drop
		sel.compounds = append(sel.compounds, compound)
		i = next
		spaced := false
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
			spaced = true
			i++
		}
		if i >= len(s) {
			break
		}
		comb := byte(' ')
		if c := s[i]; c == '>' || c == '+' || c == '~' {
			comb = c
			i++
			for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
				i++
			}
		} else if !spaced {
			return sel, false, fmt.Errorf("unexpected %q", s[i:])
		}
		sel.combinators = append(sel.combinators, comb)
	}
	if len(sel.combinators) != len(sel.compounds)-1 {
		return sel, false, errors.New("dangling combinator")
	}
	for i := range sel.compounds {
		a, b, cc := sel.compounds[i].specificity()
		sel.spec[0] += a
		sel.spec[1] += b
		sel.spec[2] += cc
	}
	sel.pseudoElement = sel.compounds[len(sel.compounds)-1].pseudoElement
	return sel, dropped, nil
}

// specificity is the compound's contribution to its selector's specificity.
func (c *cascadeCompound) specificity() (a, b, cc int) {
	a = len(c.ids)
	b = len(c.classes) + len(c.attrs)
	if c.tag != "" && c.tag != "*" {
		cc++
	}
	if c.pseudoElement != "" {
		cc++
	}
	for _, p := range c.pseudos {
		switch p.name {
		case "where":
		case "is", "not", "has", "matches", "-webkit-any":
			best := [3]int{}
			for _, s := range p.list {
				if cascadeSpecLess(best, s.spec) {
					best = s.spec
				}
			}
			a, b, cc = a+best[0], b+best[1], cc+best[2]
		default:
			b++
		}
	}
	return a, b, cc
}

// cascadeSpecLess reports whether specificity x is lower than y.
func cascadeSpecLess(x, y [3]int) bool {
	for i := range x {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}

// parseCascadeIdent reads an identifier at i, resolving escapes.
func parseCascadeIdent(s string, i int) (string, int) {
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			j := i + 1
			for j < len(s) && j < i+7 && isHexDigit(s[j]) {
				j++
			}
			if j > i+1 {
				r, _ := strconv.ParseUint(s[i+1:j], 16, 32)
				b.WriteRune(rune(r))
				if j < len(s) && s[j] == ' ' {
					j++
				}
				i = j
				continue
			}
			b.WriteByte(s[i+1])
			i += 2
		case c == '-' || c == '_' || c >= 0x80 || (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'z'):
			b.WriteByte(c)
			i++
		default:
			return b.String(), i
		}
	}
	return b.String(), i
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'f')
}

// parseCascadeCompound parses the compound selector at i.
func parseCascadeCompound(s string, i int) (c cascadeCompound, next int, dropped bool, err error) {
	start := i
	for i < len(s) {
		ch := s[i]
		switch {
		case ch == '*':
			c.tag = "*"
			i++
		case ch == '.':
			var name string
			name, i = parseCascadeIdent(s, i+1)
			c.classes = append(c.classes, name)
		case ch == '#':
			var name string
			name, i = parseCascadeIdent(s, i+1)
			c.ids = append(c.ids, name)
		case ch == '[':
			end := cascadeScan(s, i+1, "]")
			if end < 0 {
				return c, 0, false, errors.New("unterminated attribute selector")
			}
			c.attrs = append(c.attrs, parseCascadeAttr(s[i+1:end]))
			i = end + 1
		case ch == ':':
			element := i+1 < len(s) && s[i+1] == ':'
			if element {
				i++
			}
			var name string
			name, i = parseCascadeIdent(s, i+1)
			name = strings.ToLower(name)
			if strings.HasPrefix(name, "-moz-") || strings.HasPrefix(name, "-ms-") {
				dropped = true
			}
			var args string
			if i < len(s) && s[i] == '(' {
				end := cascadeScan(s, i+1, ")")
				if end < 0 {
					return c, 0, false, errors.New("unterminated pseudo-class argument")
				}
				args = strings.TrimSpace(s[i+1 : end])
				i = end + 1
			}
			if element || legacyPseudoElements[name] {
				c.pseudoElement = name
				continue
			}
			p := cascadePseudo{name: name, args: args}
			switch name {
			case "not", "is", "where", "matches", "-webkit-any", "has":
				p.relative = name == "has"
				for _, part := range cascadeSplit(args, ',') {
					sel, drop, err := parseCascadeSelector(strings.TrimSpace(part), p.relative)
					if err != nil {
						return c, 0, false, fmt.Errorf(":%s(%s): %w", name, args, err)
					}
					dropped = dropped || drop
					p.list = append(p.list, sel)
				}
			}
			c.pseudos = append(c.pseudos, p)
		case ch == '-' || ch == '_' || ch == '\\' || ch >= 0x80 || (ch|0x20 >= 'a' && ch|0x20 <= 'z'):
			if i != start {
				return c, 0, false, fmt.Errorf("type selector inside a compound at %q", s[i:])
			}
			c.tag, i = parseCascadeIdent(s, i)
			c.tag = strings.ToLower(c.tag)
		default:
			if i == start {
				return c, 0, false, fmt.Errorf("unexpected %q", s[i:])
			}
			return c, i, dropped, nil
		}
	}
	return c, i, dropped, nil
}

// parseCascadeAttr parses the inside of an attribute selector.
func parseCascadeAttr(s string) cascadeAttr {
	s = strings.TrimSpace(s)
	for _, op := range []string{"~=", "|=", "^=", "$=", "*=", "="} {
		if at := strings.Index(s, op); at >= 0 {
			value := strings.TrimSpace(s[at+len(op):])
			if f := cascadeFields(value); len(f) == 2 && (f[1] == "i" || f[1] == "s") {
				value = f[0]
			}
			value = strings.Trim(value, `"'`)
			return cascadeAttr{name: strings.ToLower(strings.TrimSpace(s[:at])), op: op, value: value}
		}
	}
	return cascadeAttr{name: strings.ToLower(s)}
}

// ==================== MATCHING ====================

// matches reports whether sel matches el, or el's pseudo-element when pseudo is
// not empty.
func (sel *cascadeSelector) matches(el *cssElement, pseudo string) (bool, error) {
	if sel.pseudoElement != pseudo {
		return false, nil
	}
	return sel.matchFrom(len(sel.compounds)-1, el)
}

func (sel *cascadeSelector) matchFrom(idx int, el *cssElement) (bool, error) {
	ok, err := sel.compounds[idx].matches(el)
	if err != nil || !ok {
		return false, err
	}
	if idx == 0 {
		return true, nil
	}
	switch sel.combinators[idx-1] {
	case '>':
		if el.parent == nil {
			return false, nil
		}
		return sel.matchFrom(idx-1, el.parent)
	case ' ':
		for p := el.parent; p != nil; p = p.parent {
			ok, err := sel.matchFrom(idx-1, p)
			if err != nil || ok {
				return ok, err
			}
		}
	}
	// Sibling combinators: preceding siblings are not modelled.
	return false, nil
}

// matches reports whether the compound matches el.
func (c *cascadeCompound) matches(el *cssElement) (bool, error) {
	if c.anchor != nil && c.anchor != el {
		return false, nil
	}
	if c.tag != "" && c.tag != "*" && c.tag != el.tag {
		return false, nil
	}
	for _, id := range c.ids {
		if el.id != id {
			return false, nil
		}
	}
	for _, class := range c.classes {
		if !el.hasClass(class) {
			return false, nil
		}
	}
	for _, a := range c.attrs {
		if !el.matchesAttr(a) {
			return false, nil
		}
	}
	for i := range c.pseudos {
		ok, err := el.matchesPseudo(&c.pseudos[i])
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func (el *cssElement) hasClass(class string) bool {
	for _, c := range el.classes {
		if c == class {
			return true
		}
	}
	return false
}

func (el *cssElement) attr(name string) (string, bool) {
	switch name {
	case "class":
		return strings.Join(el.classes, " "), len(el.classes) > 0
	case "id":
		return el.id, el.id != ""
	}
	v, ok := el.attrs[name]
	return v, ok
}

func (el *cssElement) matchesAttr(a cascadeAttr) bool {
	v, ok := el.attr(a.name)
	if !ok {
		return false
	}
	switch a.op {
	case "":
		return true
	case "=":
		return v == a.value
	case "~=":
		for _, f := range strings.Fields(v) {
			if f == a.value {
				return true
			}
		}
		return false
	case "|=":
		return v == a.value || strings.HasPrefix(v, a.value+"-")
	case "^=":
		return a.value != "" && strings.HasPrefix(v, a.value)
	case "$=":
		return a.value != "" && strings.HasSuffix(v, a.value)
	case "*=":
		return a.value != "" && strings.Contains(v, a.value)
	}
	return false
}

// isFormControl reports whether el is an input, a select, or a textarea.
func (el *cssElement) isFormControl() bool {
	return el.tag == "input" || el.tag == "select" || el.tag == "textarea" || el.tag == "button"
}

// focusWithin reports whether el or an element below it on the path has focus.
func (el *cssElement) focusWithin() bool {
	for e := el; e != nil; e = e.child {
		if e.state["focus"] {
			return true
		}
	}
	return false
}

// matchesPseudo evaluates one pseudo-class against el. A pseudo-class this
// resolver does not know is an error, never a silent mismatch.
func (el *cssElement) matchesPseudo(p *cascadePseudo) (bool, error) {
	anyOf := func() (bool, error) {
		for i := range p.list {
			ok, err := p.list[i].matches(el, "")
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	}
	pos, count := max(el.pos, 1), max(el.count, 1)
	switch p.name {
	case "root", "scope":
		return el.parent == nil, nil
	case "not":
		ok, err := anyOf()
		return !ok, err
	case "is", "where", "matches", "-webkit-any":
		return anyOf()
	case "has":
		for i := range p.list {
			sel := p.list[i]
			sel.compounds = append([]cascadeCompound(nil), sel.compounds...)
			sel.compounds[0] = cascadeCompound{anchor: el}
			for d := el.child; d != nil; d = d.child {
				ok, err := sel.matchFrom(len(sel.compounds)-1, d)
				if err != nil || ok {
					return ok, err
				}
			}
		}
		return false, nil
	case "focus", "focus-visible", "placeholder-shown", "empty":
		return el.state[p.name], nil
	case "focus-within":
		return el.focusWithin(), nil
	case "hover", "active", "visited", "target", "checked", "indeterminate", "invalid",
		"user-invalid", "user-valid", "autofill", "default", "in-range", "out-of-range",
		"modal", "popover-open", "fullscreen", "open", "closed", "host", "host-context",
		"target-within", "playing", "paused", "blank":
		return false, nil
	case "link", "any-link":
		_, href := el.attrs["href"]
		return el.tag == "a" && href, nil
	case "disabled":
		_, disabled := el.attrs["disabled"]
		return el.isFormControl() && disabled, nil
	case "enabled":
		_, disabled := el.attrs["disabled"]
		return el.isFormControl() && !disabled, nil
	case "valid":
		return el.isFormControl(), nil
	case "required":
		_, required := el.attrs["required"]
		return required, nil
	case "optional":
		_, required := el.attrs["required"]
		return el.isFormControl() && !required, nil
	case "read-only":
		return !(el.tag == "input" || el.tag == "textarea"), nil
	case "read-write":
		return el.tag == "input" || el.tag == "textarea", nil
	case "defined":
		return true, nil
	case "dir":
		return p.args == "ltr", nil
	case "lang":
		return strings.HasPrefix(p.args, "en"), nil
	case "first-child", "first-of-type":
		return pos == 1, nil
	case "last-child", "last-of-type":
		return pos == count, nil
	case "only-child", "only-of-type":
		return count == 1, nil
	case "nth-child", "nth-of-type":
		return nthMatches(p.args, pos)
	case "nth-last-child", "nth-last-of-type":
		return nthMatches(p.args, count-pos+1)
	}
	if strings.HasPrefix(p.name, "-webkit-") {
		return false, nil
	}
	return false, fmt.Errorf("unsupported pseudo-class :%s", p.name)
}

// nthMatches evaluates an An+B argument at a 1-based position.
func nthMatches(arg string, pos int) (bool, error) {
	arg = strings.ReplaceAll(strings.ToLower(arg), " ", "")
	switch arg {
	case "odd":
		arg = "2n+1"
	case "even":
		arg = "2n"
	}
	if strings.Contains(arg, "of") {
		return false, fmt.Errorf("unsupported :nth-*(%s)", arg)
	}
	a, b := 0, 0
	if n := strings.IndexByte(arg, 'n'); n >= 0 {
		switch coef := arg[:n]; coef {
		case "", "+":
			a = 1
		case "-":
			a = -1
		default:
			v, err := strconv.Atoi(coef)
			if err != nil {
				return false, fmt.Errorf("unsupported :nth-*(%s)", arg)
			}
			a = v
		}
		if rest := arg[n+1:]; rest != "" {
			v, err := strconv.Atoi(rest)
			if err != nil {
				return false, fmt.Errorf("unsupported :nth-*(%s)", arg)
			}
			b = v
		}
	} else {
		v, err := strconv.Atoi(arg)
		if err != nil {
			return false, fmt.Errorf("unsupported :nth-*(%s)", arg)
		}
		b = v
	}
	if a == 0 {
		return pos == b, nil
	}
	k := pos - b
	return k%a == 0 && k/a >= 0, nil
}

// ==================== MEDIA CONDITIONS ====================

// mediaHolds evaluates a media query list for a screen of the given width, with
// no reduced-motion preference, no forced colours, and a dark colour-scheme
// preference. A feature it does not know is an error.
func mediaHolds(query string, width float64) (bool, error) {
	for _, alt := range cascadeSplit(query, ',') {
		ok, err := mediaQueryHolds(strings.ToLower(strings.TrimSpace(alt)), width)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func mediaQueryHolds(q string, width float64) (bool, error) {
	negate := false
	if rest, ok := strings.CutPrefix(q, "not "); ok {
		negate, q = true, rest
	}
	q = strings.TrimPrefix(q, "only ")
	holds := true
	for _, part := range strings.Split(q, " and ") {
		part = strings.TrimSpace(part)
		var ok bool
		switch {
		case part == "screen" || part == "all":
			ok = true
		case part == "print":
			ok = false
		case strings.HasPrefix(part, "(") && strings.HasSuffix(part, ")"):
			name, value, _ := strings.Cut(part[1:len(part)-1], ":")
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
			switch name {
			case "min-width", "max-width":
				px, err := strconv.ParseFloat(strings.TrimSuffix(value, "px"), 64)
				if err != nil || !strings.HasSuffix(value, "px") {
					return false, fmt.Errorf("unsupported media feature %s", part)
				}
				ok = (name == "min-width" && width >= px) || (name == "max-width" && width <= px)
			case "prefers-reduced-motion":
				ok = value == "no-preference"
			case "forced-colors":
				ok = value == "none"
			case "prefers-color-scheme":
				ok = value == "dark"
			case "hover":
				ok = value == "hover"
			case "pointer", "any-pointer":
				ok = value == "fine"
			default:
				return false, fmt.Errorf("unsupported media feature %s", part)
			}
		default:
			return false, fmt.Errorf("unsupported media query %q", q)
		}
		holds = holds && ok
	}
	return holds != negate, nil
}

// ==================== THE CASCADE ====================

// cssCascade resolves the style of described elements at one viewport width.
type cssCascade struct {
	rules   []cascadeRule
	matched map[cascadeKey][]cascadeMatch
	width   float64
}

type cascadeKey struct {
	el     *cssElement
	pseudo string
}

// cascadeMatch is a rule that matches an element, with the specificity of the
// most specific of its selectors that matches.
type cascadeMatch struct {
	rule *cascadeRule
	spec [3]int
}

// cascadeWinner is a winning declaration.
type cascadeWinner struct {
	prop  string
	value string
}

func newCSSCascade(rules []cascadeRule, width float64) *cssCascade {
	return &cssCascade{rules: rules, width: width, matched: map[cascadeKey][]cascadeMatch{}}
}

// matching returns the rules that match el (or its pseudo-element) and whose
// media conditions hold.
func (c *cssCascade) matching(el *cssElement, pseudo string) ([]cascadeMatch, error) {
	key := cascadeKey{el, pseudo}
	if m, ok := c.matched[key]; ok {
		return m, nil
	}
	var out []cascadeMatch
	for i := range c.rules {
		rule := &c.rules[i]
		holds := true
		for _, m := range rule.media {
			ok, err := mediaHolds(m, c.width)
			if err != nil {
				return nil, err
			}
			holds = holds && ok
		}
		if !holds {
			continue
		}
		var best [3]int
		found := false
		for j := range rule.selectors {
			ok, err := rule.selectors[j].matches(el, pseudo)
			if err != nil {
				return nil, fmt.Errorf("matching %v: %w", rule.selectors[j].compounds, err)
			}
			if ok && (!found || cascadeSpecLess(best, rule.selectors[j].spec)) {
				best, found = rule.selectors[j].spec, true
			}
		}
		if found {
			out = append(out, cascadeMatch{rule: rule, spec: best})
		}
	}
	c.matched[key] = out
	return out, nil
}

// winning returns the declaration that wins the cascade among the declarations
// of any of props on el: importance first, then specificity, then order of
// appearance.
func (c *cssCascade) winning(el *cssElement, pseudo string, props ...string) (cascadeWinner, bool, error) {
	matches, err := c.matching(el, pseudo)
	if err != nil {
		return cascadeWinner{}, false, err
	}
	var (
		win       cascadeWinner
		found     bool
		wImp      bool
		wSpec     [3]int
		wOrder    int
		wDeclSlot int
	)
	for _, m := range matches {
		for d, decl := range m.rule.decls {
			if !containsString(props, decl.prop) {
				continue
			}
			better := !found ||
				(decl.important && !wImp) ||
				(decl.important == wImp && (cascadeSpecLess(wSpec, m.spec) ||
					(wSpec == m.spec && (m.rule.order > wOrder || (m.rule.order == wOrder && d > wDeclSlot)))))
			if better {
				win, found = cascadeWinner{prop: decl.prop, value: decl.value}, true
				wImp, wSpec, wOrder, wDeclSlot = decl.important, m.spec, m.rule.order, d
			}
		}
	}
	return win, found, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// customProperty returns the inherited value of a custom property on el, with
// its own var() references substituted where it is declared.
func (c *cssCascade) customProperty(el *cssElement, name string, depth int) (string, bool, error) {
	for e := el; e != nil; e = e.parent {
		win, ok, err := c.winning(e, "", name)
		if err != nil {
			return "", false, err
		}
		if ok {
			v, err := c.substitute(e, win.value, depth+1)
			return v, true, err
		}
	}
	return "", false, nil
}

// substitute replaces every var() reference in value.
func (c *cssCascade) substitute(el *cssElement, value string, depth int) (string, error) {
	if depth > 64 {
		return "", fmt.Errorf("var() recursion too deep in %q", clip(value))
	}
	for {
		at := strings.Index(value, "var(")
		if at < 0 {
			return value, nil
		}
		end := cascadeScan(value, at+4, ")")
		if end < 0 {
			return "", fmt.Errorf("unterminated var() in %q", clip(value))
		}
		name, fallback, hasFallback := strings.Cut(value[at+4:end], ",")
		name = strings.TrimSpace(name)
		replacement, ok, err := c.customProperty(el, name, depth)
		if err != nil {
			return "", err
		}
		if !ok {
			if !hasFallback {
				return "", fmt.Errorf("var(%s) is undeclared and has no fallback", name)
			}
			if replacement, err = c.substitute(el, strings.TrimSpace(fallback), depth+1); err != nil {
				return "", err
			}
		}
		value = value[:at] + replacement + value[end+1:]
	}
}

// ==================== COLOURS ====================

// rgba is a colour in gamma-encoded sRGB, each channel and the alpha in [0, 1].
type rgba struct{ r, g, b, a float64 }

// over composites top over bottom by source-over alpha compositing in sRGB.
func over(top, bottom rgba) rgba {
	a := top.a + bottom.a*(1-top.a)
	if a == 0 {
		return rgba{}
	}
	mix := func(t, b float64) float64 { return (t*top.a + b*bottom.a*(1-top.a)) / a }
	return rgba{mix(top.r, bottom.r), mix(top.g, bottom.g), mix(top.b, bottom.b), a}
}

// rgbaContrast is WCAG 2.2's contrast ratio of two opaque colours, by the
// relative luminance and contrast ratio helpers the typography gates share.
func rgbaContrast(x, y rgba) float64 {
	return contrastRatio(relativeLuminance(x.r*255, x.g*255, x.b*255), relativeLuminance(y.r*255, y.g*255, y.b*255))
}

// hex renders an opaque colour as #rrggbb, for messages.
func (c rgba) hex() string {
	ch := func(v float64) int { return int(math.Round(math.Max(0, math.Min(1, v)) * 255)) }
	s := fmt.Sprintf("#%02x%02x%02x", ch(c.r), ch(c.g), ch(c.b))
	if c.a < 1 {
		s += fmt.Sprintf("/%.3f", c.a)
	}
	return s
}

// oklabToSRGB converts an OKLab colour to gamma-encoded sRGB by CSS Color 4:
// OKLab to LMS, LMS to linear sRGB (the two matrices CSS Color 4's OKLab
// conversion composes through XYZ D65), then the sRGB transfer function. A
// channel outside the sRGB gamut is clipped into [0, 1].
func oklabToSRGB(l, a, b float64) (float64, float64, float64) {
	lp := l + 0.3963377773761749*a + 0.2158037573099136*b
	mp := l - 0.1055613458156586*a - 0.0638541728258133*b
	sp := l - 0.0894841775298119*a - 1.2914855480194092*b
	L, M, S := lp*lp*lp, mp*mp*mp, sp*sp*sp
	lr := 4.0767416621*L - 3.3077115913*M + 0.2309699292*S
	lg := -1.2684380046*L + 2.6097574011*M - 0.3413193965*S
	lb := -0.0041960863*L - 0.7034186147*M + 1.7076147010*S
	enc := func(v float64) float64 {
		v = math.Max(0, math.Min(1, v))
		if v <= 0.0031308 {
			return 12.92 * v
		}
		return 1.055*math.Pow(v, 1/2.4) - 0.055
	}
	return enc(lr), enc(lg), enc(lb)
}

// parseColour parses a colour value whose var() references are already
// substituted. current resolves currentcolor.
func parseColour(s string, current func() (rgba, error)) (rgba, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	fn, args, isFn := cutFunction(s)
	if isFn {
		switch fn {
		case "light-dark":
			parts := cascadeSplit(args, ',')
			if len(parts) != 2 {
				return rgba{}, fmt.Errorf("light-dark() needs two arguments: %q", clip(s))
			}
			return parseColour(parts[1], current)
		case "color-mix":
			return parseColorMix(args, current)
		case "oklch", "oklab":
			return parseOK(fn, args)
		case "rgb", "rgba":
			return parseRGB(args)
		}
		return rgba{}, fmt.Errorf("unsupported colour function %s()", fn)
	}
	switch {
	case s == "transparent":
		return rgba{}, nil
	case s == "white":
		return rgba{1, 1, 1, 1}, nil
	case s == "black":
		return rgba{0, 0, 0, 1}, nil
	case s == "currentcolor":
		if current == nil {
			return rgba{}, errors.New("currentcolor has nothing to resolve against")
		}
		return current()
	case strings.HasPrefix(s, "#"):
		return parseHexColour(s[1:])
	}
	return rgba{}, fmt.Errorf("unsupported colour %q", clip(s))
}

// cutFunction splits a whole-value function call into its name and arguments.
func cutFunction(s string) (name, args string, ok bool) {
	open := strings.IndexByte(s, '(')
	if open <= 0 || !strings.HasSuffix(s, ")") || cascadeScan(s, open+1, ")") != len(s)-1 {
		return "", "", false
	}
	return s[:open], s[open+1 : len(s)-1], true
}

func parseHexColour(h string) (rgba, error) {
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for i := range len(h) {
			b.WriteByte(h[i])
			b.WriteByte(h[i])
		}
		h = b.String()
	}
	if len(h) != 6 && len(h) != 8 {
		return rgba{}, fmt.Errorf("bad hex colour #%s", h)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return rgba{}, fmt.Errorf("bad hex colour #%s", h)
	}
	if len(h) == 6 {
		v = v<<8 | 0xff
	}
	return rgba{float64(v>>24&0xff) / 255, float64(v>>16&0xff) / 255, float64(v>>8&0xff) / 255, float64(v&0xff) / 255}, nil
}

// cssNumber evaluates a number, a percentage, or a calc() of products and sums
// of those. percent reports a percentage result.
func cssNumber(s string) (value float64, percent bool, err error) {
	s = strings.TrimSpace(s)
	if name, args, ok := cutFunction(s); ok && name == "calc" {
		return cssCalc(args)
	}
	if strings.HasSuffix(s, "%") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		return v, true, err
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "deg"), 64)
	return v, false, err
}

// cssCalc evaluates the inside of a calc() made of numbers and percentages
// joined by * and /, and those products joined by + and -.
func cssCalc(expr string) (float64, bool, error) {
	fields := cascadeFields(expr)
	total, percent := 0.0, false
	sign := 1.0
	for i := 0; i < len(fields); {
		term, termPercent, err := cssNumber(fields[i])
		if err != nil {
			return 0, false, fmt.Errorf("calc(%s): %w", expr, err)
		}
		i++
		for i+1 < len(fields) && (fields[i] == "*" || fields[i] == "/") {
			v, p, err := cssNumber(fields[i+1])
			if err != nil {
				return 0, false, fmt.Errorf("calc(%s): %w", expr, err)
			}
			if fields[i] == "*" {
				term *= v
			} else {
				term /= v
			}
			termPercent = termPercent || p
			i += 2
		}
		total += sign * term
		percent = percent || termPercent
		if i < len(fields) {
			switch fields[i] {
			case "+":
				sign = 1
			case "-":
				sign = -1
			default:
				return 0, false, fmt.Errorf("calc(%s): unexpected %q", expr, fields[i])
			}
			i++
		}
	}
	return total, percent, nil
}

// parseColorMix evaluates color-mix(in oklab, <colour> <p>%, transparent), in
// either argument order: <colour> with an alpha of <p>/100 of its own. A mix of
// two colours neither of which is transparent lies outside the resolver's steps.
func parseColorMix(args string, current func() (rgba, error)) (rgba, error) {
	parts := cascadeSplit(args, ',')
	if len(parts) != 3 || strings.TrimSpace(parts[0]) != "in oklab" {
		return rgba{}, fmt.Errorf("unsupported color-mix(%s)", clip(args))
	}
	type side struct {
		colour      string
		p           float64
		transparent bool
		given       bool
	}
	var sides [2]side
	for i, part := range parts[1:] {
		var colour []string
		for _, f := range cascadeFields(strings.TrimSpace(part)) {
			if strings.HasSuffix(f, "%") || strings.HasPrefix(f, "calc(") {
				v, _, err := cssNumber(f)
				if err != nil {
					return rgba{}, err
				}
				sides[i].p, sides[i].given = v, true
				continue
			}
			colour = append(colour, f)
		}
		sides[i].colour = strings.Join(colour, " ")
		sides[i].transparent = sides[i].colour == "transparent"
	}
	switch {
	case !sides[0].given && !sides[1].given:
		sides[0].p, sides[1].p = 50, 50
	case !sides[0].given:
		sides[0].p = 100 - sides[1].p
	case !sides[1].given:
		sides[1].p = 100 - sides[0].p
	}
	var colourSide side
	switch {
	case sides[1].transparent:
		colourSide = sides[0]
	case sides[0].transparent:
		colourSide = sides[1]
	default:
		return rgba{}, fmt.Errorf("color-mix() of two colours, neither transparent: %q", clip(args))
	}
	sum := sides[0].p + sides[1].p
	if sum <= 0 {
		return rgba{}, fmt.Errorf("color-mix() percentages sum to %v", sum)
	}
	c, err := parseColour(colourSide.colour, current)
	if err != nil {
		return rgba{}, err
	}
	c.a *= colourSide.p / sum * math.Min(1, sum/100)
	return c, nil
}

// splitAlpha separates the channels of a colour function from its "/ alpha".
func splitAlpha(args string) (channels []string, alpha float64, err error) {
	alpha = 1
	body, a, hasAlpha := strings.Cut(args, "/")
	if hasAlpha {
		v, p, err := cssNumber(a)
		if err != nil {
			return nil, 0, err
		}
		if p {
			v /= 100
		}
		alpha = v
	}
	return cascadeFields(body), alpha, nil
}

// parseOK parses oklch() and oklab().
func parseOK(fn, args string) (rgba, error) {
	if strings.HasPrefix(strings.TrimSpace(args), "from ") {
		return rgba{}, fmt.Errorf("relative colour syntax %s(%s) is outside the steps", fn, clip(args))
	}
	ch, alpha, err := splitAlpha(args)
	if err != nil || len(ch) != 3 {
		return rgba{}, fmt.Errorf("bad %s(%s)", fn, args)
	}
	vals := [3]float64{}
	for i, c := range ch {
		if c == "none" {
			continue
		}
		v, p, err := cssNumber(c)
		if err != nil {
			return rgba{}, fmt.Errorf("bad %s(%s): %w", fn, args, err)
		}
		if p {
			switch i {
			case 0:
				v /= 100
			default:
				v = v / 100 * 0.4
			}
		}
		vals[i] = v
	}
	l, a, b := vals[0], vals[1], vals[2]
	if fn == "oklch" {
		h := vals[2] * math.Pi / 180
		a, b = vals[1]*math.Cos(h), vals[1]*math.Sin(h)
	}
	r, g, bl := oklabToSRGB(l, a, b)
	return rgba{r, g, bl, alpha}, nil
}

// parseRGB parses rgb() and rgba(), legacy comma or modern space syntax.
func parseRGB(args string) (rgba, error) {
	var ch []string
	alpha := 1.0
	if strings.Contains(args, ",") {
		parts := cascadeSplit(args, ',')
		if len(parts) == 4 {
			v, p, err := cssNumber(parts[3])
			if err != nil {
				return rgba{}, err
			}
			if p {
				v /= 100
			}
			alpha, parts = v, parts[:3]
		}
		ch = parts
	} else {
		var err error
		if ch, alpha, err = splitAlpha(args); err != nil {
			return rgba{}, err
		}
	}
	if len(ch) != 3 {
		return rgba{}, fmt.Errorf("bad rgb(%s)", args)
	}
	var out [3]float64
	for i, c := range ch {
		v, p, err := cssNumber(c)
		if err != nil {
			return rgba{}, fmt.Errorf("bad rgb(%s): %w", args, err)
		}
		if p {
			out[i] = v / 100
		} else {
			out[i] = v / 255
		}
	}
	return rgba{out[0], out[1], out[2], alpha}, nil
}

// ==================== RESOLVED PROPERTIES ====================

// shorthandOf lists the declarations a longhand colour property is set by.
var shorthandOf = map[string][]string{
	"color":            {"color"},
	"background-color": {"background-color", "background"},
	"outline-color":    {"outline-color", "outline"},
	"outline-style":    {"outline-style", "outline"},
	"outline-width":    {"outline-width", "outline"},
}

var outlineStyles = map[string]bool{
	"none": true, "hidden": true, "dotted": true, "dashed": true, "solid": true, "double": true,
	"groove": true, "ridge": true, "inset": true, "outset": true, "auto": true,
}

// backgroundNonColour reports a background shorthand token that is not a colour.
func backgroundNonColour(tok string) bool {
	switch tok {
	case "none", "repeat", "repeat-x", "repeat-y", "no-repeat", "space", "round", "center", "left", "right",
		"top", "bottom", "fixed", "scroll", "local", "border-box", "padding-box", "content-box",
		"text", "cover", "contain", "auto", "/":
		return true
	}
	if strings.HasPrefix(tok, "url(") || strings.Contains(tok, "gradient(") || strings.HasPrefix(tok, "image-set(") {
		return true
	}
	if _, _, err := cssNumber(strings.TrimRight(tok, "pxremvhw")); err == nil && tok != "" &&
		(tok[0] == '.' || tok[0] == '-' || (tok[0] >= '0' && tok[0] <= '9')) {
		return true
	}
	return false
}

// component returns the part of a winning declaration that sets prop: the whole
// value for a longhand, the matching token of a shorthand, and ok=false where a
// shorthand leaves prop at its initial value.
func component(win cascadeWinner, prop, value string) (string, bool) {
	if win.prop == prop {
		return value, true
	}
	switch win.prop {
	case "background":
		layers := cascadeSplit(value, ',')
		for _, tok := range cascadeFields(layers[len(layers)-1]) {
			if !backgroundNonColour(tok) {
				return tok, true
			}
		}
		return "", false
	case "outline":
		for _, tok := range cascadeFields(value) {
			isStyle := outlineStyles[tok]
			isWidth := tok == "thin" || tok == "medium" || tok == "thick" || cssLengthOK(tok)
			switch prop {
			case "outline-style":
				if isStyle {
					return tok, true
				}
			case "outline-width":
				if isWidth {
					return tok, true
				}
			case "outline-color":
				if !isStyle && !isWidth {
					return tok, true
				}
			}
		}
		return "", false
	}
	return value, true
}

// cssLengthOK reports whether tok is a length in px or rem, or 0.
func cssLengthOK(tok string) bool {
	_, err := cssLengthPx(tok)
	return err == nil
}

// cssLengthPx converts a px or rem length, or 0, to pixels at a 16px root.
func cssLengthPx(tok string) (float64, error) {
	switch {
	case tok == "0":
		return 0, nil
	case strings.HasSuffix(tok, "rem"):
		v, err := strconv.ParseFloat(strings.TrimSuffix(tok, "rem"), 64)
		return v * 16, err
	case strings.HasSuffix(tok, "px"):
		return strconv.ParseFloat(strings.TrimSuffix(tok, "px"), 64)
	}
	if name, args, ok := cutFunction(tok); ok && name == "calc" {
		fields := cascadeFields(args)
		if len(fields) == 3 && fields[1] == "*" {
			a, errA := cssLengthPx(fields[0])
			b, _, errB := cssNumber(fields[2])
			if errA == nil && errB == nil {
				return a * b, nil
			}
		}
	}
	return 0, fmt.Errorf("unsupported length %q", tok)
}

// resolved returns the substituted value of a property on el (or its
// pseudo-element), and ok=false where no declaration sets it.
func (c *cssCascade) resolved(el *cssElement, pseudo, prop string) (string, bool, error) {
	win, ok, err := c.winning(el, pseudo, shorthandOf[prop]...)
	if err != nil || !ok {
		return "", false, err
	}
	value, err := c.substitute(el, win.value, 0)
	if err != nil {
		return "", false, fmt.Errorf("%s: %s: %w", prop, win.value, err)
	}
	v, set := component(win, prop, strings.TrimSpace(value))
	return v, set, nil
}

// colour resolves a colour property of el, or of its pseudo-element, to sRGB.
// color inherits and background-color does not; a ::placeholder colour must be
// declared by a rule, since the criterion measures the winning declaration.
func (c *cssCascade) colour(el *cssElement, pseudo, prop string) (rgba, error) {
	value, ok, err := c.resolved(el, pseudo, prop)
	if err != nil {
		return rgba{}, err
	}
	current := func() (rgba, error) { return c.colour(el, pseudo, "color") }
	switch {
	case !ok && prop == "background-color":
		return rgba{}, nil
	case !ok && prop == "outline-color":
		return current()
	case !ok && pseudo != "":
		return rgba{}, fmt.Errorf("no rule declares the ::%s %s", pseudo, prop)
	case !ok || value == "inherit" || (prop == "color" && value == "unset"):
		if el.parent == nil {
			return rgba{}, fmt.Errorf("the root element declares no %s", prop)
		}
		return c.colour(el.parent, "", prop)
	case value == "initial" || value == "unset":
		if prop == "background-color" {
			return rgba{}, nil
		}
		return rgba{}, fmt.Errorf("%s: %s is outside the steps", prop, value)
	}
	col, err := parseColour(value, current)
	if err != nil {
		return rgba{}, fmt.Errorf("%s of <%s class=%q>: %w", prop, el.tag, strings.Join(el.classes, " "), err)
	}
	return col, nil
}

// customColour resolves a custom property of el, such as --tblr-bg-surface, to
// sRGB.
func (c *cssCascade) customColour(el *cssElement, name string) (rgba, error) {
	value, ok, err := c.customProperty(el, name, 0)
	if err != nil {
		return rgba{}, err
	}
	if !ok {
		return rgba{}, fmt.Errorf("%s is undeclared", name)
	}
	return parseColour(value, nil)
}

// backdrop is what lies beneath el: the page background, --tblr-body-bg, with the
// background of every ancestor of el composited over it in turn, from the root
// down to el's parent.
func (c *cssCascade) backdrop(el *cssElement) (rgba, error) {
	root := el
	for root.parent != nil {
		root = root.parent
	}
	base, err := c.customColour(root, "--tblr-body-bg")
	if err != nil {
		return rgba{}, err
	}
	var chain []*cssElement
	for e := el.parent; e != nil; e = e.parent {
		chain = append(chain, e)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		bg, err := c.colour(chain[i], "", "background-color")
		if err != nil {
			return rgba{}, err
		}
		base = over(bg, base)
	}
	return base, nil
}

// ==================== DESCRIBING ELEMENTS ====================

// describe builds a path of elements from the root down, one description per
// element, and returns the last. A description is a tag followed by any number
// of .class, #id, [attr], and [attr=value] parts, then optional states written
// as :focus, :focus-visible, :placeholder-shown, or :empty.
func describe(path ...string) *cssElement {
	var parent *cssElement
	for _, d := range path {
		el := &cssElement{attrs: map[string]string{}, state: map[string]bool{}, parent: parent}
		i := 0
		for i < len(d) && d[i] != '.' && d[i] != '#' && d[i] != '[' && d[i] != ':' {
			i++
		}
		el.tag = d[:i]
		for i < len(d) {
			kind := d[i]
			j := i + 1
			if kind == '[' {
				j = strings.IndexByte(d[i:], ']') + i
				name, value, _ := strings.Cut(d[i+1:j], "=")
				el.attrs[name] = strings.Trim(value, `"`)
				i = j + 1
				continue
			}
			for j < len(d) && d[j] != '.' && d[j] != '#' && d[j] != '[' && d[j] != ':' {
				j++
			}
			switch kind {
			case '.':
				el.classes = append(el.classes, d[i+1:j])
			case '#':
				el.id = d[i+1 : j]
			case ':':
				el.state[d[i+1:j]] = true
			}
			i = j
		}
		if parent != nil {
			parent.child = el
		}
		parent = el
	}
	return parent
}
