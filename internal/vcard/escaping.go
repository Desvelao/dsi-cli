// Package vcard parses and serializes vCard 4.0 content (RFC 6350).
package vcard

import (
	"strings"
	"unicode/utf8"
)

// EscapeText escapes a TEXT value so it is safe inside a vCard property.
func EscapeText(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '\r':
			if i+1 < len(value) && value[i+1] == '\n' {
				i++
			}
			b.WriteString(`\n`)
		case '\n':
			b.WriteString(`\n`)
		case ',':
			b.WriteString(`\,`)
		case ';':
			b.WriteString(`\;`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// UnescapeText reverses EscapeText (also accepts \N for newline). A trailing
// lone backslash is kept.
func UnescapeText(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	runes := []rune(value)
	var b strings.Builder
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			i++
			if runes[i] == 'n' || runes[i] == 'N' {
				b.WriteByte('\n')
			} else {
				b.WriteRune(runes[i])
			}
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

// escapeBreaks escapes backslashes and line breaks only.
func escapeBreaks(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '\r':
			if i+1 < len(value) && value[i+1] == '\n' {
				i++
			}
			b.WriteString(`\n`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// JoinStructuredList builds a structured (";") or list (",") value from
// components that are escaped one by one. size < 0 means no padding; otherwise
// the result is padded with empty components up to size components.
func JoinStructuredList(parts []string, separator string, size int) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = EscapeText(p)
	}
	return joinPadded(out, separator, size)
}

// JoinStructuredText builds a structured or list value from text that is
// already structured: separator characters are kept as separators and only
// backslashes, line breaks and the other separator are escaped.
func JoinStructuredText(value, separator string, size int) string {
	text := escapeBreaks(value)
	other := ";"
	if separator == ";" {
		other = ","
	}
	text = strings.ReplaceAll(text, other, `\`+other)
	return joinPadded(strings.Split(text, separator), separator, size)
}

func joinPadded(parts []string, separator string, size int) string {
	for size >= 0 && len(parts) < size {
		parts = append(parts, "")
	}
	return strings.Join(parts, separator)
}

// SplitStructured splits a structured/list value on unescaped separators and
// unescapes each part. separator must be a single character.
func SplitStructured(value, separator string) []string {
	sep, _ := utf8.DecodeRuneInString(separator)
	runes := []rune(value)
	var parts []string
	var current []rune
	for i := 0; i < len(runes); {
		c := runes[i]
		if c == '\\' && i+1 < len(runes) {
			current = append(current, runes[i], runes[i+1])
			i += 2
			continue
		}
		if c == sep {
			parts = append(parts, string(current))
			current = nil
		} else {
			current = append(current, c)
		}
		i++
	}
	parts = append(parts, string(current))
	for i, p := range parts {
		parts[i] = UnescapeText(p)
	}
	return parts
}

// FoldLine folds a content line at limit octets (RFC 6350 section 3.2) on
// UTF-8 character boundaries. Continuation lines start with one space, which
// counts toward the limit. The result has no trailing CRLF.
func FoldLine(line string, limit int) string {
	if len(line) <= limit {
		return line
	}
	var chunks []string
	var current strings.Builder
	size := 0
	for _, r := range line {
		width := utf8.RuneLen(r)
		if width < 0 {
			width = 3 // U+FFFD, the encoding of an invalid byte
		}
		if size+width > limit {
			chunks = append(chunks, current.String())
			current.Reset()
			current.WriteByte(' ')
			size = 1
		}
		current.WriteRune(r)
		size += width
	}
	chunks = append(chunks, current.String())
	return strings.Join(chunks, "\r\n")
}
