package feeds

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Desvelao/dsi-cli/internal/crypto"
	"github.com/Desvelao/dsi-cli/internal/pyutil"
)

// Signer signs feed items with an Ed25519 key published under ID (the Base64
// DER of the public key).
type Signer struct {
	Key ed25519.PrivateKey
	ID  string
}

// StripCDATA returns a description without its CDATA wrapper (the form that is signed).
func StripCDATA(value string) string {
	if strings.HasPrefix(value, "<![CDATA[") && strings.HasSuffix(value, "]]>") {
		return value[len("<![CDATA[") : len(value)-len("]]>")]
	}
	return value
}

// xmlWriter reproduces the output of Python's xml.sax.saxutils.XMLGenerator
// (as used by rfeed): no short empty elements, text escaped for & < >.
type xmlWriter struct{ b strings.Builder }

var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

var attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\n", "&#10;", "\r", "&#13;", "\t", "&#9;")

// quoteAttr is saxutils.quoteattr.
func quoteAttr(v string) string {
	v = attrEscaper.Replace(v)
	if strings.Contains(v, `"`) {
		if strings.Contains(v, "'") {
			return `"` + strings.ReplaceAll(v, `"`, "&quot;") + `"`
		}
		return "'" + v + "'"
	}
	return `"` + v + `"`
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

func (w *xmlWriter) text(s string) { w.b.WriteString(textEscaper.Replace(s)) }

// element is rfeed's _write_element for a non-None value.
func (w *xmlWriter) element(name, value string, attrs ...attr) {
	w.start(name, attrs...)
	w.text(value)
	w.end(name)
}

// description writes the description of an item; a CDATA section in the value
// is written raw instead of escaped.
func (w *xmlWriter) description(value string) {
	w.start("description")
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

// RFCDate formats a time like rfeed does: the wall-clock fields of t followed by "GMT".
func RFCDate(t time.Time) string {
	return fmt.Sprintf("%s, %02d %s %04d %02d:%02d:%02d GMT",
		weekdays[t.Weekday()], t.Day(), months[t.Month()-1], t.Year(), t.Hour(), t.Minute(), t.Second())
}

func mediaType(url string) (typ, medium string) {
	switch {
	case strings.HasSuffix(url, ".jpg"), strings.HasSuffix(url, ".jpeg"):
		return "image/jpeg", "image"
	case strings.HasSuffix(url, ".png"):
		return "image/png", "image"
	case strings.HasSuffix(url, ".gif"):
		return "image/gif", "image"
	case strings.HasSuffix(url, ".webp"):
		return "image/webp", "image"
	case strings.HasSuffix(url, ".mp4"):
		return "video/mp4", "video"
	}
	return "application/octet-stream", "unknown"
}

const (
	rfeedGenerator = "rfeed v1.1.1"
	rfeedDocs      = "https://github.com/svpino/rfeed/blob/master/README.md"
)

// BuildRSS generates an RSS 2.0 feed (byte-compatible with the Python
// implementation) from the given items; items are signed when sign is set.
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
	w.element("generator", rfeedGenerator)
	w.element("docs", rfeedDocs)
	w.element("atom:link", "", attr{"href", link}, attr{"rel", "self"})
	// atom:link has no text: rfeed writes <atom:link ...></atom:link>

	for _, item := range items {
		pubDate := RFCDate(item.Date)
		var signature string
		if sign != nil {
			signature = crypto.SignFeedItem(sign.Key, pubDate, item.Title, StripCDATA(item.Content))
		}
		itemLink := item.Link
		if itemLink == "" {
			itemLink = fmt.Sprintf("%s/feed/%s", link, item.ID)
		}
		w.start("item")
		w.element("title", item.Title)
		w.element("link", itemLink)
		w.description(item.Content)
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
func ReplaceTemplateVariables(content string, metadata, vars *pyutil.OrderedMap) string {
	merged := pyutil.NewOrderedMap()
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
	for _, k := range merged.Keys() {
		content = strings.ReplaceAll(content, "{{ "+k+" }}", merged.Value(k))
	}
	return content
}

// ApplyTemplates replaces template variables in each state ({{ title }},
// {{ date }}, ...) and wraps HTML content in CDATA so it can be used as the
// RSS description.
func ApplyTemplates(states []*State, vars *pyutil.OrderedMap) {
	for _, s := range states {
		for _, field := range []*string{&s.Title, &s.ID, &s.Link, &s.Image, &s.Content} {
			if *field != "" {
				*field = ReplaceTemplateVariables(*field, s.Metadata, vars)
			}
		}
		if s.Content != "" && s.ContentType == "html" {
			s.Content = "<![CDATA[" + s.Content + "]]>"
		}
	}
}
