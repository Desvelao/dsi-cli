// Package strutil holds small string helpers whose semantics the
// DSI wire formats depend on (trimming, integer parsing, ...).
package strutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"unicode"
)

// IsSpace reports whether r is whitespace according to Unicode-aware whitespace rules.
// It differs from unicode.IsSpace by also including U+001C..U+001F.
func IsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// Strip trims whitespace on both sides.
func Strip(s string) string { return strings.TrimFunc(s, IsSpace) }

// RStrip trims trailing whitespace.
func RStrip(s string) string { return strings.TrimRightFunc(s, IsSpace) }

// ParseInt is a subset of integer syntax: surrounding whitespace, an
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

// Repr quotes a string for display: single quotes unless the text contains a
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

// OrderedMap is an insertion-ordered string map, like an ordered dict of strings.
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

// SplitLines splits on \n, \r\n, \r, \v, \f,
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

// OSErrorString formats file-system errors in the traditional OS error style
// ("[Errno 2] No such file or directory: 'path'"). ok is false for other errors.
func OSErrorString(err error) (string, bool) {
	var errno syscall.Errno
	capital := func(e syscall.Errno) string {
		msg := e.Error()
		if msg == "" {
			return msg
		}
		return strings.ToUpper(msg[:1]) + msg[1:]
	}
	var pe *fs.PathError
	if errors.As(err, &pe) && errors.As(pe.Err, &errno) {
		return fmt.Sprintf("[Errno %d] %s: %s", int(errno), capital(errno), Repr(pe.Path)), true
	}
	var le *os.LinkError
	if errors.As(err, &le) && errors.As(le.Err, &errno) {
		return fmt.Sprintf("[Errno %d] %s: %s -> %s", int(errno), capital(errno), Repr(le.Old), Repr(le.New)), true
	}
	return "", false
}

// ErrText is the user-facing text of an error: file-system errors use the traditional wording.
func ErrText(err error) string {
	if s, ok := OSErrorString(err); ok {
		return s
	}
	return err.Error()
}
