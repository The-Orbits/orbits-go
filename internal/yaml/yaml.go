// Package yaml reads the subset of YAML that OpenAPI documents are written in: block mappings and sequences,
// flow mappings and sequences (which may span lines), plain, single- and double-quoted scalars, and literal and
// folded block scalars. It has no anchors, aliases, tags or multi-document streams, which the Orbits API spec does
// not use. Scalars resolve as in YAML 1.2's core schema: null, true and false, integers and floats; everything else
// is a string. Mapping keys are always strings, and a Map keeps them in document order.
//
// It exists so the SDK can read its vendored spec (to generate types and to check fixtures against it) without a
// dependency.
package yaml

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Map is a mapping that keeps its keys in document order.
type Map struct {
	Keys   []string
	Values map[string]any
}

// Get returns the value at key and whether it is present.
func (m *Map) Get(key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m.Values[key]
	return v, ok
}

func (m *Map) set(key string, v any) {
	if _, ok := m.Values[key]; !ok {
		m.Keys = append(m.Keys, key)
	}
	m.Values[key] = v
}

func newMap() *Map { return &Map{Values: map[string]any{}} }

// Unmarshal parses a document. The result is a *Map, []any, string, bool, int64, float64 or nil.
func Unmarshal(src []byte) (any, error) {
	p := &parser{}
	for i, raw := range strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n") {
		if lead := raw[:len(raw)-len(strings.TrimLeft(raw, " \t"))]; strings.Contains(lead, "\t") && strings.TrimSpace(raw) != "" {
			return nil, fmt.Errorf("yaml: line %d: tab in indentation", i+1)
		}
		p.lines = append(p.lines, line{num: i + 1, raw: raw})
	}
	p.skipBlank()
	if p.i >= len(p.lines) {
		return nil, nil
	}
	v, err := p.block(p.indent())
	if err != nil {
		return nil, err
	}
	p.skipBlank()
	if p.i < len(p.lines) {
		return nil, p.errf("unexpected content %q", strings.TrimSpace(p.lines[p.i].raw))
	}
	return v, nil
}

type line struct {
	num int
	raw string
}

type parser struct {
	lines []line
	i     int
}

func (p *parser) errf(format string, args ...any) error {
	n := 0
	if p.i < len(p.lines) {
		n = p.lines[p.i].num
	} else if len(p.lines) > 0 {
		n = p.lines[len(p.lines)-1].num
	}
	return fmt.Errorf("yaml: line %d: %s", n, fmt.Sprintf(format, args...))
}

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// blank reports whether a line is empty or only a comment.
func blank(s string) bool {
	t := strings.TrimSpace(s)
	return t == "" || strings.HasPrefix(t, "#")
}

func (p *parser) skipBlank() {
	for p.i < len(p.lines) && blank(p.lines[p.i].raw) {
		p.i++
	}
}

func (p *parser) indent() int { return indentOf(p.lines[p.i].raw) }

func (p *parser) text() string { return strings.TrimLeft(p.lines[p.i].raw, " ") }

// block parses the node starting at the current line, which is indented by ind.
func (p *parser) block(ind int) (any, error) {
	t := p.text()
	if t == "-" || strings.HasPrefix(t, "- ") {
		return p.sequence(ind)
	}
	if _, _, ok := splitKey(t); ok {
		return p.mapping(ind)
	}
	return p.inlineValue(ind-1, t)
}

func (p *parser) sequence(ind int) (any, error) {
	out := []any{}
	for {
		p.skipBlank()
		if p.i >= len(p.lines) || p.indent() != ind {
			break
		}
		t := p.text()
		if t != "-" && !strings.HasPrefix(t, "- ") {
			break
		}
		rest := strings.TrimLeft(strings.TrimPrefix(t, "-"), " ")
		if rest == "" || strings.HasPrefix(rest, "#") {
			p.i++
			p.skipBlank()
			if p.i >= len(p.lines) || p.indent() <= ind {
				out = append(out, nil)
				continue
			}
			v, err := p.block(p.indent())
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			continue
		}
		// "- key: value" starts a mapping indented to where "key" is: parse the line as if it began there.
		inner := ind + (len(t) - len(rest))
		p.lines[p.i].raw = strings.Repeat(" ", inner) + rest
		v, err := p.block(inner)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (p *parser) mapping(ind int) (any, error) {
	m := newMap()
	for {
		p.skipBlank()
		if p.i >= len(p.lines) || p.indent() != ind {
			break
		}
		t := p.text()
		if t == "-" || strings.HasPrefix(t, "- ") {
			break
		}
		rawKey, rest, ok := splitKey(t)
		if !ok {
			return nil, p.errf("expected a mapping key, found %q", t)
		}
		key, err := scalarKey(rawKey)
		if err != nil {
			return nil, p.errf("%v", err)
		}
		if _, dup := m.Values[key]; dup {
			return nil, p.errf("duplicate key %q", key)
		}
		rest = stripComment(rest)
		if rest == "" {
			p.i++
			p.skipBlank()
			switch {
			case p.i < len(p.lines) && p.indent() > ind:
				v, err := p.block(p.indent())
				if err != nil {
					return nil, err
				}
				m.set(key, v)
			case p.i < len(p.lines) && p.indent() == ind && (p.text() == "-" || strings.HasPrefix(p.text(), "- ")):
				// A sequence may sit at its key's own indentation.
				v, err := p.sequence(ind)
				if err != nil {
					return nil, err
				}
				m.set(key, v)
			default:
				m.set(key, nil)
			}
			continue
		}
		v, err := p.inlineValue(ind, rest)
		if err != nil {
			return nil, err
		}
		m.set(key, v)
	}
	return m, nil
}

// inlineValue parses a value that starts on the current line after its key (or dash), owned by a node indented by
// ind. It consumes the current line and any continuation lines.
func (p *parser) inlineValue(ind int, rest string) (any, error) {
	switch {
	case rest[0] == '|' || rest[0] == '>':
		return p.blockScalar(ind, rest)
	case rest[0] == '{' || rest[0] == '[':
		src := p.flowSource(rest)
		f := &flow{src: src}
		v, err := f.value()
		if err != nil {
			return nil, p.errf("%v", err)
		}
		f.ws()
		if f.pos != len(f.src) {
			return nil, p.errf("unexpected %q after a flow collection", f.src[f.pos:])
		}
		return v, nil
	case rest[0] == '"' || rest[0] == '\'':
		src := rest
		for !quoteClosed(src) && p.i+1 < len(p.lines) {
			p.i++
			src += "\n" + strings.TrimSpace(p.lines[p.i].raw)
		}
		p.i++
		f := &flow{src: src}
		s, err := f.quoted()
		if err != nil {
			return nil, p.errf("%v", err)
		}
		if tail := stripComment(f.src[f.pos:]); tail != "" {
			return nil, p.errf("unexpected %q after a quoted scalar", tail)
		}
		return s, nil
	}
	// A plain scalar, perhaps folded over more-indented lines.
	parts := []string{rest}
	p.i++
	for p.i < len(p.lines) {
		raw := p.lines[p.i].raw
		if strings.TrimSpace(raw) == "" {
			parts = append(parts, "")
			p.i++
			continue
		}
		if indentOf(raw) <= ind || strings.HasPrefix(strings.TrimSpace(raw), "#") {
			break
		}
		parts = append(parts, stripComment(strings.TrimSpace(raw)))
		p.i++
	}
	for len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 1 {
		return resolve(parts[0]), nil
	}
	return foldPlain(parts), nil
}

// foldPlain joins the lines of a multi-line plain scalar: a line break is a space, an empty line a newline.
func foldPlain(parts []string) string {
	var b strings.Builder
	for i, s := range parts {
		switch {
		case i == 0:
			b.WriteString(s)
		case s == "":
			b.WriteByte('\n')
		case parts[i-1] == "":
			b.WriteString(s)
		default:
			b.WriteByte(' ')
			b.WriteString(s)
		}
	}
	return b.String()
}

// flowSource gathers a flow collection that may continue over the following lines, without their comments.
func (p *parser) flowSource(first string) string {
	src := stripComment(first)
	p.i++
	for depth(src) > 0 && p.i < len(p.lines) {
		src += " " + stripComment(strings.TrimSpace(p.lines[p.i].raw))
		p.i++
	}
	return src
}

// depth is how many brackets are open at the end of s, outside quotes.
func depth(s string) int {
	d := 0
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '"':
			if c == '\\' {
				i++
			} else if c == '"' {
				q = 0
			}
		case q == '\'':
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					q = 0
				}
			}
		case c == '"' || c == '\'':
			if i == 0 || strings.ContainsRune(" ,[{:", rune(s[i-1])) {
				q = c
			}
		case c == '{' || c == '[':
			d++
		case c == '}' || c == ']':
			d--
		}
	}
	return d
}

func quoteClosed(s string) bool {
	f := &flow{src: s}
	_, err := f.quoted()
	return err == nil
}

func (p *parser) blockScalar(ind int, header string) (any, error) {
	header = stripComment(header)
	style := header[0]
	chomp := byte(0)
	explicit := 0
	for _, c := range header[1:] {
		switch {
		case c == '-' || c == '+':
			chomp = byte(c)
		case c >= '1' && c <= '9':
			explicit = int(c - '0')
		default:
			return nil, p.errf("bad block scalar header %q", header)
		}
	}
	p.i++
	var body []string
	contentIndent := -1
	if explicit > 0 {
		contentIndent = max(ind, 0) + explicit
	}
	for p.i < len(p.lines) {
		raw := p.lines[p.i].raw
		if strings.TrimSpace(raw) == "" {
			body = append(body, "")
			p.i++
			continue
		}
		in := indentOf(raw)
		if contentIndent < 0 {
			if in <= ind {
				break
			}
			contentIndent = in
		}
		if in < contentIndent {
			break
		}
		body = append(body, raw[contentIndent:])
		p.i++
	}
	// Trailing empty lines are subject to chomping.
	trailing := 0
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
		trailing++
	}
	var text string
	if style == '|' {
		text = strings.Join(body, "\n")
	} else {
		text = foldBlock(body)
	}
	if len(body) == 0 {
		return "", nil
	}
	switch chomp {
	case '-':
	case '+':
		text += "\n" + strings.Repeat("\n", trailing)
	default:
		text += "\n"
	}
	return text, nil
}

// foldBlock folds a ">" scalar: lines join with spaces, except around empty and more-indented lines.
func foldBlock(lines []string) string {
	var b strings.Builder
	for i, s := range lines {
		if i > 0 {
			prev := lines[i-1]
			switch {
			case s == "":
				b.WriteByte('\n')
			case prev == "":
			case strings.HasPrefix(s, " ") || strings.HasPrefix(prev, " "):
				b.WriteByte('\n')
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteString(s)
	}
	return b.String()
}

// splitKey splits "key: rest" (or "key:") at the first ": " outside quotes and brackets.
func splitKey(t string) (key, rest string, ok bool) {
	if t == "" || t[0] == '{' || t[0] == '[' || t[0] == '#' {
		return "", "", false
	}
	if t[0] == '"' || t[0] == '\'' {
		f := &flow{src: t}
		if _, err := f.quoted(); err != nil {
			return "", "", false
		}
		after := t[f.pos:]
		if after == ":" || strings.HasPrefix(after, ": ") {
			return t[:f.pos], strings.TrimSpace(after[1:]), true
		}
		return "", "", false
	}
	for i := 0; i < len(t); i++ {
		if t[i] == ':' && (i+1 == len(t) || t[i+1] == ' ') {
			return strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+1:]), true
		}
		if t[i] == '#' && i > 0 && t[i-1] == ' ' {
			return "", "", false
		}
	}
	return "", "", false
}

func scalarKey(raw string) (string, error) {
	if raw != "" && (raw[0] == '"' || raw[0] == '\'') {
		f := &flow{src: raw}
		return f.quoted()
	}
	return raw, nil
}

// stripComment removes a trailing " # comment" outside quotes, and surrounding spaces.
func stripComment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q == '"':
			if c == '\\' {
				i++
			} else if c == '"' {
				q = 0
			}
		case q == '\'':
			if c == '\'' {
				q = 0
			}
		case (c == '"' || c == '\'') && (i == 0 || strings.ContainsRune(" ,[{:", rune(s[i-1]))):
			q = c
		case c == '#' && (i == 0 || s[i-1] == ' '):
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

var (
	intRE   = regexp.MustCompile(`^[-+]?[0-9]+$`)
	floatRE = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)
)

// resolve gives a plain scalar its type, as YAML 1.2's core schema does.
func resolve(s string) any {
	switch s {
	case "", "~", "null", "Null", "NULL":
		return nil
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	case ".inf", ".Inf", ".INF", "+.inf":
		return math.Inf(1)
	case "-.inf", "-.Inf", "-.INF":
		return math.Inf(-1)
	}
	if intRE.MatchString(s) {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	if floatRE.MatchString(s) {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return s
}

// flow parses flow collections and quoted scalars from a single string.
type flow struct {
	src string
	pos int
}

func (f *flow) ws() {
	for f.pos < len(f.src) && (f.src[f.pos] == ' ' || f.src[f.pos] == '\n') {
		f.pos++
	}
}

func (f *flow) value() (any, error) {
	f.ws()
	if f.pos >= len(f.src) {
		return nil, nil
	}
	switch f.src[f.pos] {
	case '{':
		return f.mapping()
	case '[':
		return f.sequence()
	case '"', '\'':
		return f.quoted()
	}
	return resolve(f.plain()), nil
}

// plain reads a plain scalar in flow context: it ends at , [ ] { } or ": ".
func (f *flow) plain() string {
	start := f.pos
	for f.pos < len(f.src) {
		c := f.src[f.pos]
		if c == ',' || c == ']' || c == '}' || c == '[' || c == '{' {
			break
		}
		if c == ':' && (f.pos+1 == len(f.src) || strings.ContainsRune(" ,]}", rune(f.src[f.pos+1]))) {
			break
		}
		f.pos++
	}
	return strings.TrimSpace(f.src[start:f.pos])
}

func (f *flow) mapping() (any, error) {
	f.pos++ // {
	m := newMap()
	for {
		f.ws()
		if f.pos >= len(f.src) {
			return nil, fmt.Errorf("unterminated flow mapping")
		}
		if f.src[f.pos] == '}' {
			f.pos++
			return m, nil
		}
		var key string
		if c := f.src[f.pos]; c == '"' || c == '\'' {
			k, err := f.quoted()
			if err != nil {
				return nil, err
			}
			key = k
		} else {
			key = f.plain()
		}
		f.ws()
		var v any
		if f.pos < len(f.src) && f.src[f.pos] == ':' {
			f.pos++
			f.ws()
			if f.pos < len(f.src) && f.src[f.pos] != ',' && f.src[f.pos] != '}' {
				var err error
				if v, err = f.value(); err != nil {
					return nil, err
				}
			}
		}
		if _, dup := m.Values[key]; dup {
			return nil, fmt.Errorf("duplicate key %q in a flow mapping", key)
		}
		m.set(key, v)
		f.ws()
		if f.pos < len(f.src) && f.src[f.pos] == ',' {
			f.pos++
			continue
		}
		if f.pos < len(f.src) && f.src[f.pos] == '}' {
			continue
		}
		return nil, fmt.Errorf("expected , or } in a flow mapping at %q", f.rest())
	}
}

func (f *flow) sequence() (any, error) {
	f.pos++ // [
	out := []any{}
	for {
		f.ws()
		if f.pos >= len(f.src) {
			return nil, fmt.Errorf("unterminated flow sequence")
		}
		if f.src[f.pos] == ']' {
			f.pos++
			return out, nil
		}
		v, err := f.value()
		if err != nil {
			return nil, err
		}
		f.ws()
		// A single "key: value" pair inside a sequence is a one-entry mapping.
		if f.pos < len(f.src) && f.src[f.pos] == ':' {
			key := fmt.Sprint(v)
			f.pos++
			pv, err := f.value()
			if err != nil {
				return nil, err
			}
			m := newMap()
			m.set(key, pv)
			v = m
			f.ws()
		}
		out = append(out, v)
		if f.pos < len(f.src) && f.src[f.pos] == ',' {
			f.pos++
			continue
		}
		if f.pos < len(f.src) && f.src[f.pos] == ']' {
			continue
		}
		return nil, fmt.Errorf("expected , or ] in a flow sequence at %q", f.rest())
	}
}

func (f *flow) rest() string {
	r := f.src[f.pos:]
	if len(r) > 40 {
		r = r[:40] + "…"
	}
	return r
}

// quoted reads a single- or double-quoted scalar. Line breaks inside fold to spaces.
func (f *flow) quoted() (string, error) {
	q := f.src[f.pos]
	f.pos++
	var b strings.Builder
	for f.pos < len(f.src) {
		c := f.src[f.pos]
		switch {
		case c == q && q == '\'':
			if f.pos+1 < len(f.src) && f.src[f.pos+1] == '\'' {
				b.WriteByte('\'')
				f.pos += 2
				continue
			}
			f.pos++
			return b.String(), nil
		case c == q:
			f.pos++
			return b.String(), nil
		case c == '\n':
			// Fold: trailing spaces before and leading spaces after the break go; the break is a space.
			s := strings.TrimRight(b.String(), " ")
			b.Reset()
			b.WriteString(s)
			f.pos++
			for f.pos < len(f.src) && f.src[f.pos] == ' ' {
				f.pos++
			}
			b.WriteByte(' ')
		case c == '\\' && q == '"':
			if f.pos+1 >= len(f.src) {
				return "", fmt.Errorf("unterminated escape")
			}
			e := f.src[f.pos+1]
			f.pos += 2
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case '"', '\\', '/', ' ':
				b.WriteByte(e)
			case 'x', 'u', 'U':
				n := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
				if f.pos+n > len(f.src) {
					return "", fmt.Errorf("short \\%c escape", e)
				}
				r, err := strconv.ParseUint(f.src[f.pos:f.pos+n], 16, 32)
				if err != nil {
					return "", fmt.Errorf("bad \\%c escape", e)
				}
				b.WriteRune(rune(r))
				f.pos += n
			default:
				return "", fmt.Errorf("unknown escape \\%c", e)
			}
		default:
			b.WriteByte(c)
			f.pos++
		}
	}
	return "", fmt.Errorf("unterminated quoted scalar")
}
