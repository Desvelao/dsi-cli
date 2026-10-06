// Package pyutil holds small helpers that reproduce Python string semantics the
// DSI wire formats depend on (str.strip, int(), ...).
package pyutil

import (
	"fmt"
	"strings"
	"unicode"
)

// IsSpace reports whether r is whitespace according to Python's str.isspace.
// It differs from unicode.IsSpace by also including U+001C..U+001F.
func IsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// Strip is Python's str.strip().
func Strip(s string) string { return strings.TrimFunc(s, IsSpace) }

// RStrip is Python's str.rstrip().
func RStrip(s string) string { return strings.TrimRightFunc(s, IsSpace) }

// ParseInt is a subset of Python's int(str): surrounding whitespace, an
// optional sign and ASCII digits with single underscores between digits.
// Values that do not fit in int64 are reported as invalid.
func ParseInt(s string) (int64, bool) {
	s = Strip(s)
	if s == "" {
		return 0, false
	}
	neg := false
	if s[0] == '+' || s[0] == '-' {
		neg = s[0] == '-'
		s = s[1:]
	}
	if s == "" || s[0] == '_' || s[len(s)-1] == '_' {
		return 0, false
	}
	var n int64
	prevUnderscore := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			if prevUnderscore {
				return 0, false
			}
			prevUnderscore = true
			continue
		}
		prevUnderscore = false
		if c < '0' || c > '9' {
			return 0, false
		}
		d := int64(c - '0')
		if n > (1<<63-1-d)/10 {
			return 0, false
		}
		n = n*10 + d
	}
	if neg {
		n = -n
	}
	return n, true
}

// Repr is Python's repr() for a str: single quotes unless the text contains a
// single quote and no double quote; control and non-printable characters are
// escaped.
func Repr(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range s {
		switch {
		case r == quote || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x7f || unicode.IsPrint(r):
			b.WriteRune(r)
		case r <= 0xff:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xffff:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(quote)
	return b.String()
}

// OrderedMap is an insertion-ordered string map, like a Python dict[str, str].
type OrderedMap struct {
	keys []string
	vals map[string]string
}

// NewOrderedMap returns an empty map.
func NewOrderedMap() *OrderedMap { return &OrderedMap{vals: map[string]string{}} }

// Set stores a value; an existing key keeps its position.
func (m *OrderedMap) Set(key, value string) {
	if _, ok := m.vals[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.vals[key] = value
}

// Get returns the value and whether the key is present.
func (m *OrderedMap) Get(key string) (string, bool) {
	v, ok := m.vals[key]
	return v, ok
}

// Value returns the value or "" when absent.
func (m *OrderedMap) Value(key string) string { return m.vals[key] }

// Keys returns the keys in insertion order.
func (m *OrderedMap) Keys() []string { return append([]string(nil), m.keys...) }

// Len is the number of entries.
func (m *OrderedMap) Len() int { return len(m.keys) }

// Clone returns a copy.
func (m *OrderedMap) Clone() *OrderedMap {
	c := NewOrderedMap()
	for _, k := range m.keys {
		c.Set(k, m.vals[k])
	}
	return c
}

// SplitLines is Python's str.splitlines(): it splits on \n, \r\n, \r, \v, \f,
// \x1c-\x1e, \x85,   and   and drops the separators.
func SplitLines(s string) []string {
	var lines []string
	runes := []rune(s)
	pos := 0
	var cur []rune
	for pos < len(runes) {
		r := runes[pos]
		isBreak := false
		switch r {
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			isBreak = true
		case '\r':
			isBreak = true
			if pos+1 < len(runes) && runes[pos+1] == '\n' {
				pos++
			}
		}
		if isBreak {
			lines = append(lines, string(cur))
			cur = nil
		} else {
			cur = append(cur, r)
		}
		pos++
	}
	if len(cur) > 0 {
		lines = append(lines, string(cur))
	}
	return lines
}
