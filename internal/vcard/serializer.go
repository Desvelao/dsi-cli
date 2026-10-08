package vcard

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Desvelao/dsi-cli/internal/model"
	"github.com/Desvelao/dsi-cli/internal/strutil"
)

const (
	// DefaultKeyEncoding is the ENCODING of KEY values (Base64).
	DefaultKeyEncoding = "b"
	// DefaultNoteLanguage is the LANGUAGE of NOTE when the card has no lang.
	DefaultNoteLanguage = "en-US"
)

// Value is a structured (";") or list (",") field value: either text whose
// separators are kept, or a list of components that are escaped one by one.
type Value struct {
	Text   string
	List   []string
	IsList bool
}

// Text returns a Value holding already-structured text.
func Text(s string) Value { return Value{Text: s} }

// List returns a Value holding components.
func List(parts ...string) Value { return Value{List: parts, IsList: true} }

func (v Value) empty() bool {
	if v.IsList {
		return len(v.List) == 0
	}
	return v.Text == ""
}

func (v Value) join(separator string, size int) string {
	if v.IsList {
		return JoinStructuredList(v.List, separator, size)
	}
	return JoinStructuredText(v.Text, separator, size)
}

// KeySpec is a public key entry for BuildContent. A nil Pref means PREF=1.
type KeySpec struct {
	Alg      string
	KeyB64   string
	Pref     *int64
	Encoding string
}

// Attribute is a custom "NAME:value" line.
type Attribute struct{ Name, Value string }

// Fields are the inputs of BuildContent; empty fields are omitted.
type Fields struct {
	FN, Nickname, Lang, Email, Kind     string
	N, Gender, Categories, Adr          Value
	Bday, Anniversary                   string
	Tel, Impp, Photo, Note, URL, Source string
	CustomAttributes                    []Attribute
	Keys                                []KeySpec
}

// BuildContent builds vCard content from the given fields. Text fields are
// escaped; values written raw (URIs, dates...) may not contain line breaks.
func BuildContent(f Fields) (string, error) {
	// Values written raw must not be able to inject new lines.
	for _, c := range []struct{ name, value string }{
		{"bday", f.Bday}, {"anniversary", f.Anniversary}, {"tel", f.Tel},
		{"impp", f.Impp}, {"photo", f.Photo}, {"url", f.URL}, {"source", f.Source},
	} {
		if strings.ContainsAny(c.value, "\r\n") {
			return "", fmt.Errorf("Line breaks are not allowed in '%s'", c.name)
		}
	}
	for _, a := range f.CustomAttributes {
		if strings.ContainsAny(a.Name+a.Value, "\r\n") {
			return "", fmt.Errorf("Line breaks are not allowed in '%s'", a.Name)
		}
	}

	lines := []string{"BEGIN:VCARD", "VERSION:4.0"}
	add := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }

	// N, ADR and GENDER are structured (";"), CATEGORIES is a list (",").
	if f.FN != "" {
		add("FN:%s", EscapeText(f.FN))
	}
	if !f.N.empty() {
		add("N:%s", f.N.join(";", 5))
	}
	if f.Nickname != "" {
		add("NICKNAME:%s", EscapeText(f.Nickname))
	}
	if f.Lang != "" {
		add("LANG:%s", EscapeText(f.Lang))
	}
	if !f.Gender.empty() {
		add("GENDER:%s", f.Gender.join(";", -1))
	}
	if f.Email != "" {
		add("EMAIL:%s", EscapeText(f.Email))
	}
	if !f.Categories.empty() {
		add("CATEGORIES:%s", f.Categories.join(",", -1))
	}
	if f.Bday != "" {
		add("BDAY:%s", f.Bday)
	}
	if f.Anniversary != "" {
		add("ANNIVERSARY:%s", f.Anniversary)
	}
	if f.Kind != "" {
		add("KIND:%s", EscapeText(f.Kind))
	}
	if !f.Adr.empty() {
		add("ADR:%s", f.Adr.join(";", 7))
	}
	if f.Tel != "" {
		add("TEL:%s", f.Tel)
	}
	if f.Impp != "" {
		add("IMPP:%s", f.Impp)
	}
	if f.Photo != "" {
		add("PHOTO:%s", f.Photo)
	}
	if f.Note != "" {
		lang := f.Lang
		if lang == "" {
			lang = DefaultNoteLanguage
		}
		lang = strings.NewReplacer("\r", "", "\n", "").Replace(lang)
		noteLang, err := FormatParamValue(lang)
		if err != nil {
			return "", err
		}
		add("NOTE;LANGUAGE=%s:%s", noteLang, EscapeText(f.Note))
	}
	if f.URL != "" {
		add("URL:%s", f.URL)
	}
	if f.Source != "" {
		add("SOURCE:%s", f.Source)
	}
	for _, k := range f.Keys {
		if k.Alg == "" {
			return "", fmt.Errorf("Key entry is missing required field 'alg'")
		}
		if k.KeyB64 == "" {
			return "", fmt.Errorf("Key entry is missing required field 'key_b64'")
		}
		for _, c := range []struct{ name, value string }{
			{"alg", k.Alg}, {"key_b64", k.KeyB64}, {"encoding", k.Encoding},
		} {
			if strings.ContainsAny(c.value, "\r\n") {
				return "", fmt.Errorf("Line breaks are not allowed in key '%s'", c.name)
			}
		}
		for _, c := range []struct{ name, value string }{{"alg", k.Alg}, {"encoding", k.Encoding}} {
			if strings.ContainsAny(c.value, ";:\",") {
				return "", fmt.Errorf("Invalid character in key '%s'", c.name)
			}
		}
		encoding := k.Encoding
		if encoding == "" {
			encoding = DefaultKeyEncoding
		}
		pref := int64(1)
		if k.Pref != nil {
			pref = *k.Pref
		}
		add("KEY;TYPE=public;ALG=%s;PREF=%s;ENCODING=%s:%s",
			strings.ToLower(k.Alg), strconv.FormatInt(pref, 10), encoding, k.KeyB64)
	}
	for _, a := range f.CustomAttributes {
		add("%s:%s", a.Name, a.Value)
	}
	lines = append(lines, "END:VCARD")

	var b strings.Builder
	for _, l := range lines {
		b.WriteString(FoldLine(l, 75))
		b.WriteString("\r\n")
	}
	return b.String(), nil
}

// BuildVCardFromRawLines rebuilds a vCard from the raw lines of a profile,
// keeping every property byte for byte (lines are intentionally not folded).
// VERSION is always rewritten as 4.0.
func BuildVCardFromRawLines(p *model.Profile) string {
	lines := []string{"BEGIN:VCARD", "VERSION:4.0"}
	for _, raw := range p.RawLines {
		line := raw.Line
		if line != "" && !isFramingLine(raw) {
			lines = append(lines, line)
		}
	}
	lines = append(lines, "END:VCARD")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\r\n")
	}
	return b.String()
}

// FormatParamValue quotes a parameter value when it contains separators
// (RFC 6350 section 3.3). A double quote or line break cannot be represented
// inside a quoted value and is rejected.
func FormatParamValue(value string) (string, error) {
	if strings.ContainsAny(value, "\"\r\n") {
		return "", fmt.Errorf("Invalid character in parameter value: %s", strutil.Repr(value))
	}
	if strings.ContainsAny(value, `;:"`) {
		return `"` + value + `"`, nil
	}
	return value, nil
}

// FormatProperty builds one unfolded property line from its parts.
func FormatProperty(name string, params []model.Param, value string, group *string) (string, error) {
	var b strings.Builder
	if group != nil && *group != "" {
		b.WriteString(*group)
		b.WriteByte('.')
	}
	b.WriteString(name)
	for _, p := range params {
		v, err := FormatParamValue(p.Value)
		if err != nil {
			return "", err
		}
		b.WriteString(";" + p.Name + "=" + v)
	}
	b.WriteByte(':')
	b.WriteString(value)
	return b.String(), nil
}

// BuildEndorsementAttribute builds an X-ENDORSE line (without CRLF).
func BuildEndorsementAttribute(canonicalValue, signatureHex, date, confidence, encoding string) string {
	params := []string{"SIG=" + signatureHex}
	if date != "" {
		params = append(params, "DATE="+date)
	}
	if confidence != "" {
		params = append(params, "CONFIDENCE="+confidence)
	}
	if encoding == "" {
		encoding = "b"
	}
	params = append(params, "ENCODING="+encoding)
	return "X-ENDORSE;" + strings.Join(params, ";") + ":" + canonicalValue
}

// BuildSocialPlatformAttribute is the attribute name for a social platform.
func BuildSocialPlatformAttribute(name string) string {
	return "X-SOCIAL;PLATFORM=" + strings.ToLower(strutil.Strip(name))
}

// BuildCustomAttribute is the attribute name used by interactive vcard create.
func BuildCustomAttribute(name string) string {
	return strings.ToUpper(strutil.Strip(name)) + "="
}

// isFramingLine reports whether a raw line is a BEGIN/END/VERSION property,
// compared case-insensitively and ignoring any group prefix, like the parser.
func isFramingLine(raw model.RawLine) bool {
	var name string
	if raw.Name != nil {
		name = *raw.Name
	} else {
		name = raw.Line
		if i := strings.IndexAny(name, ":;"); i >= 0 {
			name = name[:i]
		}
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
	}
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "BEGIN", "END", "VERSION":
		return true
	}
	return false
}
