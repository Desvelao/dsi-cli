package vcard

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Desvelao/dsipy/internal/model"
	"github.com/Desvelao/dsipy/internal/pyutil"
)

// The line grammar is a port of vobject's (0.9.x) regular expressions, which the
// Python implementation relied on: content-line, parameter and value patterns.
const (
	reName  = `[a-zA-Z0-9_-]+`
	rePV    = `"[^"]*"|[^";:,]*`
	reParam = `;` + reName + `(?:(?:=(?:` + rePV + `))?(?:,(?:` + rePV + `))*)*`
)

var (
	lineRe = regexp.MustCompile(`(?s)^(?:(` + reName + `)\.)?(` + reName + `)(;?(?:` + reParam + `)*):(.*)$`)
	// Same pattern with the parameter name and value list captured.
	paramsRe      = regexp.MustCompile(`;(` + reName + `)(?:=((?:` + rePV + `)?(?:,(?:` + rePV + `))*))?`)
	paramValuesRe = regexp.MustCompile(`"([^"]*)"|([^";:,]+)`)
)

// textAttributes are the TEXT properties whose value is unescaped in the profile.
var textAttributes = map[string]bool{
	"fn": true, "nickname": true, "note": true, "email": true,
	"gender": true, "kind": true, "lang": true,
}

type logicalLine struct {
	text string
	num  int
}

// logicalLines unfolds content lines like vobject.getLogicalLines (allowQP):
// physical lines end at "\n"; a line starting with space/tab continues the
// previous one; blank lines separate lines; quoted-printable soft breaks
// ("=" at the end of a line) join the next line with "\n". Line numbers follow
// vobject's accounting (the first logical line is reported as line 0).
func logicalLines(text string) []logicalLine {
	var out []logicalLine
	var buf strings.Builder
	quotedPrintable := false
	lineNumber, lineStart := 0, 0

	flush := func() {
		out = append(out, logicalLine{buf.String(), lineStart})
	}
	for _, phys := range strings.SplitAfter(text, "\n") {
		if phys == "" {
			continue
		}
		line := strings.TrimRight(phys, "\r\n")
		lineNumber++
		if pyutil.RStrip(line) == "" {
			if buf.Len() > 0 {
				flush()
			}
			lineStart = lineNumber
			buf.Reset()
			quotedPrintable = false
			continue
		}
		switch {
		case quotedPrintable:
			buf.WriteByte('\n')
			buf.WriteString(line)
			quotedPrintable = false
		case line[0] == ' ' || line[0] == '\t':
			buf.WriteString(line[1:])
		case buf.Len() > 0:
			flush()
			lineStart = lineNumber
			buf.Reset()
			buf.WriteString(line)
		default:
			buf.Reset()
			buf.WriteString(line)
		}
		val := buf.String()
		if val != "" && val[len(val)-1] == '=' && strings.Contains(asciiLower(val), "quoted-printable") {
			quotedPrintable = true
		}
	}
	if buf.Len() > 0 {
		flush()
	}
	return out
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// parsedLine is the result of splitting one content line.
type parsedLine struct {
	name   string
	params [][]string // [name, value...]
	value  string
	group  *string
}

func parseLine(line string, lineNumber int) (*parsedLine, error) {
	m := lineRe.FindStringSubmatchIndex(line)
	if m == nil {
		return nil, fmt.Errorf("At line %d: Failed to parse line: %s", lineNumber, line)
	}
	sub := func(i int) string { return line[m[2*i]:m[2*i+1]] }
	p := &parsedLine{
		// Underscores are replaced with dash to work around Lotus Notes
		name:  strings.ReplaceAll(sub(2), "_", "-"),
		value: sub(4),
	}
	if m[2] >= 0 {
		g := sub(1)
		p.group = &g
	}
	for _, pm := range paramsRe.FindAllStringSubmatch(sub(3), -1) {
		entry := []string{pm[1]}
		for _, vm := range paramValuesRe.FindAllStringSubmatch(pm[2], -1) {
			if vm[1] != "" {
				entry = append(entry, vm[1])
			} else {
				entry = append(entry, vm[2])
			}
		}
		p.params = append(p.params, entry)
	}
	return p, nil
}

func joinParam(p []string) string { return strings.Join(p[1:], ",") }

// ParseVCard parses the text of a vCard into a Profile.
//
// Property values are kept as written (the Base64 values of KEY, REVKEY and
// X-ENDORSE are not decoded) and only TEXT properties are unescaped. Malformed
// lines are reported in Profile.Errors instead of aborting the parse. The
// original text is kept in Profile.Raw.
func ParseVCard(text string) *model.Profile {
	profile := model.NewProfile(text)
	beginCount := 0
	endSeen := false
	stopped := false // first card is over: only look for further BEGIN lines

	for _, ll := range logicalLines(text) {
		line := pyutil.Strip(ll.text)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if upper == "BEGIN:VCARD" {
			beginCount++
			if beginCount > 1 {
				if beginCount == 2 {
					profile.Errors = append(profile.Errors, "multiple vCards found; only the first is parsed")
				}
				stopped = true
				continue
			}
		}
		if stopped {
			continue
		}
		if upper == "END:VCARD" {
			endSeen, stopped = true, true
		}

		parsed, err := parseLine(line, ll.num)
		if err != nil {
			profile.Errors = append(profile.Errors, err.Error())
			profile.RawLines = append(profile.RawLines, model.RawLine{Line: line, Malformed: true})
			continue
		}

		name := strings.ToUpper(parsed.name)
		var attrs model.Attributes
		params := make([]model.Param, 0, len(parsed.params))
		for _, p := range parsed.params {
			attrs.Set(strings.ToUpper(p[0]), joinParam(p))
			params = append(params, model.Param{Name: strings.ToUpper(p[0]), Value: joinParam(p)})
		}
		value := parsed.value
		var attrName *string
		parsedValue := &value

		set := func(n string) { attrName = &n }
		switch {
		case name == "KEY":
			set("key")
			profile.Keys = append(profile.Keys, model.PublicKey{
				Alg:    strings.ToLower(attrs.Value("ALG")),
				KeyB64: pyutil.Strip(value),
				Pref:   parsePref(attrs, profile, ll.num),
			})
		case name == "REVKEY":
			set("revkey")
			profile.Revocations = append(profile.Revocations, model.RevokedKey{
				KeyB64: pyutil.Strip(value),
				Reason: optAttr(attrs, "REASON"),
				Date:   optAttr(attrs, "DATE"),
			})
		case name == "X-ENDORSE":
			set("x-endorse")
			profile.Endorsements = append(profile.Endorsements, model.Endorsement{
				EndorseeKeyB64: pyutil.Strip(value),
				SignatureHex:   attrs.Value("SIG"),
				Date:           optAttr(attrs, "DATE"),
				Confidence:     optAttr(attrs, "CONFIDENCE"),
			})
		case name == "X-FEED":
			set("x-feed")
			tags := attrs.Value("TAGS")
			category := attrs.Value("CATEGORY")
			if category == "" {
				category = tags
			}
			profile.Feeds = append(profile.Feeds, model.Feed{
				Language: attrs.Value("LANGUAGE"),
				Category: category,
				URL:      pyutil.Strip(value),
				Tags:     tags,
			})
		case name == "X-SOCIAL":
			set("x-social")
			profile.Social = append(profile.Social, model.SocialIdentity{
				Platform: strings.ToLower(attrs.Value("PLATFORM")),
				Value:    pyutil.Strip(value),
			})
		case name == "X-DSI-VERSION":
			set("x-dsi-version")
			features := []string{}
			for _, f := range strings.Split(attrs.Value("FEATURES"), ",") {
				if f != "" {
					features = append(features, f)
				}
			}
			profile.DsiVersion = &model.DsiVersion{Revision: pyutil.Strip(value), Features: features}
		case name == "VERSION":
			set("version")
			profile.Version = model.Ptr(pyutil.Strip(value))
		default:
			lower := strings.ToLower(name)
			if field := profile.Field(lower); field != nil {
				set(lower)
				v := value
				if textAttributes[lower] {
					v = UnescapeText(value)
				}
				parsedValue = &v
				*field = &v
			}
		}

		raw := model.RawLine{
			Line:     line,
			AttrName: attrName,
			Attrs:    attrs,
			Name:     &name,
			Params:   params,
			RawValue: &value,
			Group:    parsed.group,
		}
		if attrName != nil {
			raw.Value = parsedValue
		}
		profile.RawLines = append(profile.RawLines, raw)
	}

	// Framing is only checked when the text has BEGIN/END at all, so bare
	// property snippets are still accepted. A missing VERSION is reported by the
	// validator (version-missing), not here.
	if beginCount > 0 || endSeen {
		if beginCount == 0 {
			profile.Errors = append(profile.Errors, "missing BEGIN:VCARD")
		}
		if !endSeen {
			profile.Errors = append(profile.Errors, "missing END:VCARD")
		}
	}
	return profile
}

func optAttr(attrs model.Attributes, name string) *string {
	if v, ok := attrs.Get(name); ok {
		return &v
	}
	return nil
}

func parsePref(attrs model.Attributes, profile *model.Profile, lineNumber int) *int64 {
	raw, ok := attrs.Get("PREF")
	if !ok {
		return nil
	}
	n, valid := pyutil.ParseInt(raw)
	if !valid {
		profile.Errors = append(profile.Errors, fmt.Sprintf("At line %d: invalid PREF value '%s'", lineNumber, raw))
		return nil
	}
	return &n
}
