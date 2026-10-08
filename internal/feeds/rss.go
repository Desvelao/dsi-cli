package feeds

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Desvelao/dsi-cli/internal/crypto"
	"github.com/Desvelao/dsi-cli/internal/strutil"
)

// Signer signs feed items with an Ed25519 key published under ID (the Base64
// DER of the public key).
type Signer struct {
	Key ed25519.PrivateKey
	ID  string
}

// StripCDATA returns a description without its CDATA wrapper (the form that is signed).
func StripCDATA(value string) string {
	if text, ok := cdataSections(value); ok {
		return text
	}
	if strings.HasPrefix(value, "<![CDATA[") && strings.HasSuffix(value, "]]>") {
		return value[len("<![CDATA[") : len(value)-len("]]>")]
	}
	return value
}

// cdataSections reports whether value consists solely of adjacent CDATA
// sections (as produced by wrapCDATA) and returns their concatenated text,
// which is what an XML parser reads back.
func cdataSections(value string) (string, bool) {
	const open, closing = "<![CDATA[", "]]>"
	if value == "" {
		return "", false
	}
	var out strings.Builder
	for value != "" {
		if !strings.HasPrefix(value, open) {
			return "", false
		}
		end := strings.Index(value[len(open):], closing)
		if end < 0 {
			return "", false
		}
		out.WriteString(value[len(open) : len(open)+end])
		value = value[len(open)+end+len(closing):]
	}
	return out.String(), true
}

// wrapCDATA wraps text in CDATA, splitting any inner "]]>" across sections.
func wrapCDATA(text string) string {
	return "<![CDATA[" + strings.ReplaceAll(text, "]]>", "]]]]><![CDATA[>") + "]]>"
}

// xmlWriter writes RSS XML with no short empty elements and text escaped
// for & < >.
type xmlWriter struct{ b strings.Builder }

var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

var attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\n", "&#10;", "\r", "&#13;", "\t", "&#9;")

// quoteAttr is saxutils.quoteattr.
func quoteAttr(v string) string {
	v = attrEscaper.Replace(stripIllegalXML(v))
	if strings.Contains(v, `"`) {
		if strings.Contains(v, "'") {
			return `"` + strings.ReplaceAll(v, `"`, "&quot;") + `"`
		}
		return "'" + v + "'"
	}
	return `"` + v + `"`
}

// stripIllegalXML removes characters that are not allowed in XML 1.0
// (control characters other than tab, LF and CR, U+FFFE and U+FFFF). Invalid
// UTF-8 bytes (e.g. lone surrogates) become U+FFFD.
func stripIllegalXML(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r', r >= 0x20 && r != 0xFFFE && r != 0xFFFF:
			return r
		}
		return -1
	}, s)
}

type attr struct{ name, value string }

func (w *xmlWriter) start(name string, attrs ...attr) {
	w.b.WriteString("<" + name)
	for _, a := range attrs {
		w.b.WriteString(" " + a.name + "=" + quoteAttr(a.value))
	}
	w.b.WriteString(">")
}

func (w *xmlWriter) end(name string) { w.b.WriteString("</" + name + ">") }

func (w *xmlWriter) text(s string) { w.b.WriteString(textEscaper.Replace(stripIllegalXML(s))) }

// element writes a text element.
func (w *xmlWriter) element(name, value string, attrs ...attr) {
	w.start(name, attrs...)
	w.text(value)
	w.end(name)
}

// description writes the description of an item; a CDATA section in the value
// is written raw instead of escaped.
func (w *xmlWriter) description(value string) {
	w.start("description")
	if _, ok := cdataSections(value); ok {
		w.b.WriteString(value)
		w.end("description")
		return
	}
	cs, ce := strings.Index(value, "<![CDATA["), strings.Index(value, "]]>")
	if cs > -1 && ce > -1 && cs < ce {
		w.text(value[:cs])
		w.b.WriteString(value[cs : ce+3])
		w.text(value[ce+3:])
	} else {
		w.text(value)
	}
	w.end("description")
}

var (
	weekdays = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	months   = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

// RFCDate formats a time as RFC 822: the wall-clock fields of t followed by "GMT".
func RFCDate(t time.Time) string {
	return fmt.Sprintf("%s, %02d %s %04d %02d:%02d:%02d GMT",
		weekdays[t.Weekday()], t.Day(), months[t.Month()-1], t.Year(), t.Hour(), t.Minute(), t.Second())
}

// mediaType maps the extension of an image URL (case-insensitive, ignoring
// any query string or fragment) to its MIME type and media:content medium.
func mediaType(rawURL string) (typ, medium string) {
	path := rawURL
	if u, err := neturl.Parse(rawURL); err == nil {
		path = u.Path
	} else if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	path = strings.ToLower(path)
	switch {
	case strings.HasSuffix(path, ".jpg"), strings.HasSuffix(path, ".jpeg"):
		return "image/jpeg", "image"
	case strings.HasSuffix(path, ".png"):
		return "image/png", "image"
	case strings.HasSuffix(path, ".gif"):
		return "image/gif", "image"
	case strings.HasSuffix(path, ".webp"):
		return "image/webp", "image"
	case strings.HasSuffix(path, ".svg"):
		return "image/svg+xml", "image"
	case strings.HasSuffix(path, ".avif"):
		return "image/avif", "image"
	case strings.HasSuffix(path, ".mp4"):
		return "video/mp4", "video"
	case strings.HasSuffix(path, ".webm"):
		return "video/webm", "video"
	}
	return "application/octet-stream", "unknown"
}

const (
	feedGenerator = "dsi"
	feedDocs      = "https://github.com/Desvelao/dsi-cli"
)

// BuildRSS generates an RSS 2.0 feed
// from the given items; items are signed when sign is set.
// buildDate is written with its wall-clock fields.
func BuildRSS(title, link, description, authorName, authorEmail, language string,
	buildDate time.Time, items []*State, sign *Signer) (string, error) {
	if sign != nil && (sign.Key == nil || sign.ID == "") {
		return "", errors.New("Signing requires both a private 'key' and a key 'id'")
	}
	w := &xmlWriter{}
	w.b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	w.start("rss",
		attr{"version", "2.0"},
		attr{"xmlns:dc", "http://purl.org/dc/elements/1.1/"},
		attr{"xmlns:atom", "http://www.w3.org/2005/Atom"},
		attr{"xmlns:media", "http://search.yahoo.com/mrss/"},
	)
	w.start("channel")
	w.element("title", title)
	w.element("link", link)
	w.element("description", description)
	w.element("language", language)
	w.element("lastBuildDate", RFCDate(buildDate))
	w.element("generator", feedGenerator)
	w.element("docs", feedDocs)
	w.element("atom:link", "", attr{"href", link}, attr{"rel", "self"})
	// atom:link has no text: written as <atom:link ...></atom:link>

	for _, item := range items {
		pubDate := RFCDate(item.Date)
		// Strip characters illegal in XML before signing so the signed bytes
		// match what is emitted (the writer's stripping is then a no-op).
		itemTitle := stripIllegalXML(item.Title)
		itemContent := stripIllegalXML(item.Content)
		var signature string
		if sign != nil {
			signature = crypto.SignFeedItem(sign.Key, pubDate, itemTitle, StripCDATA(itemContent))
		}
		itemLink := item.Link
		if itemLink == "" {
			itemLink = fmt.Sprintf("%s/feed/%s", link, item.ID)
		}
		w.start("item")
		w.element("title", itemTitle)
		w.element("link", itemLink)
		w.description(itemContent)
		w.element("author", fmt.Sprintf("%s (%s)", authorName, authorEmail))
		w.element("pubDate", pubDate)
		w.element("guid", item.ID, attr{"isPermaLink", "false"})
		if signature != "" {
			w.element("signature", signature, attr{"alg", "ed25519"}, attr{"key-id", sign.ID})
		}
		if item.Image != "" {
			typ, medium := mediaType(item.Image)
			w.start("media:content", attr{"url", item.Image}, attr{"type", typ}, attr{"medium", medium})
			w.end("media:content")
		}
		w.end("item")
	}
	w.end("channel")
	w.end("rss")
	return w.b.String(), nil
}

// ReplaceTemplateVariables replaces "{{ name }}" with values from metadata
// overridden by vars. Unknown variables are left unchanged.
func ReplaceTemplateVariables(content string, metadata, vars *strutil.OrderedMap) string {
	merged := strutil.NewOrderedMap()
	if metadata != nil {
		for _, k := range metadata.Keys() {
			merged.Set(k, metadata.Value(k))
		}
	}
	if vars != nil {
		for _, k := range vars.Keys() {
			merged.Set(k, vars.Value(k))
		}
	}
	// Single pass: substituted values are never re-expanded and the result
	// does not depend on key order.
	return templatePlaceholder.ReplaceAllStringFunc(content, func(m string) string {
		key := m[3 : len(m)-3]
		if v, ok := merged.Get(key); ok {
			return v
		}
		return m
	})
}

var templatePlaceholder = regexp.MustCompile(`\{\{ [^{}]+? \}\}`)

// ApplyTemplates replaces template variables in each state ({{ title }},
// {{ date }}, ...) and wraps HTML content in CDATA so it can be used as the
// RSS description.
func ApplyTemplates(states []*State, vars *strutil.OrderedMap) {
	for _, s := range states {
		for _, field := range []*string{&s.Title, &s.ID, &s.Link, &s.Image, &s.Content} {
			if *field != "" {
				*field = ReplaceTemplateVariables(*field, s.Metadata, vars)
			}
		}
		if s.Content != "" && s.ContentType == "html" {
			s.Content = wrapCDATA(s.Content)
		}
	}
}
