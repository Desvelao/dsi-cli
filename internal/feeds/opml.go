package feeds

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Desvelao/dsi-cli/internal/core"
	"github.com/Desvelao/dsi-cli/internal/strutil"
	"github.com/Desvelao/dsi-cli/internal/vcard"
)

// categories merges the feed category and tags (both comma-separated) into one
// deduplicated comma-separated list.
func categories(category, tags string) string {
	var items []string
	seen := map[string]bool{}
	for _, value := range []string{category, tags} {
		for _, item := range strings.Split(value, ",") {
			item = strutil.Strip(item)
			if item != "" && !seen[item] {
				seen[item] = true
				items = append(items, item)
			}
		}
	}
	return strings.Join(items, ",")
}

var opmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\r", "&#13;", "\n", "&#10;", "\t", "&#09;")

func opmlOutline(text, category, url, language string) string {
	var b strings.Builder
	b.WriteString(`<outline text="` + opmlEscaper.Replace(text) + `" type="rss"`)
	if category != "" {
		b.WriteString(` category="` + opmlEscaper.Replace(category) + `"`)
	}
	b.WriteString(` xmlUrl="` + opmlEscaper.Replace(url) + `"`)
	if language != "" {
		b.WriteString(` language="` + opmlEscaper.Replace(language) + `"`)
	}
	b.WriteString(` title="` + opmlEscaper.Replace(text) + `" />`)
	return b.String()
}

// DefaultOPMLTitle is the <head><title> used when no title is given.
const DefaultOPMLTitle = "DSI connections"

// GenerateOPMLFromVCards reads vCard files and generates an OPML document with
// one outline per X-FEED. The category attribute holds the feed category and
// tags (comma-separated) and language the feed language, both only when
// present. Cards that cannot be read or parsed are skipped and reported in
// warnings. Outlines with the same feed URL are emitted once (first seen wins).
// The document starts with an XML declaration and a <head><title>; title is
// DefaultOPMLTitle when empty.
func GenerateOPMLFromVCards(files []string, title string, warnings *[]string) (string, error) {
	warn := func(format string, args ...any) {
		if warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf(format, args...))
		}
	}
	var outlines []string
	seenURLs := map[string]bool{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			warn("Skipping %s: %s", file, osErrorString(err, file))
			continue
		}
		if msg, bad := core.Utf8DecodeError(data); bad {
			warn("Skipping %s: %s", file, msg)
			continue
		}
		for _, profile := range vcard.ParseVCards(string(data)) {
			if len(profile.Errors) > 0 {
				warn("Skipping malformed vCard in %s: %s", file, profile.Errors[0])
				continue
			}
			name := "Unknown"
			if profile.FN != nil && *profile.FN != "" {
				name = *profile.FN
			}
			for _, feed := range profile.Feeds {
				if feed.URL == "" {
					continue
				}
				if seenURLs[feed.URL] {
					continue
				}
				seenURLs[feed.URL] = true
				outlines = append(outlines, opmlOutline(name, categories(feed.Category, feed.Tags), feed.URL, feed.Language))
			}
		}
	}
	if len(outlines) == 0 {
		return "", errors.New("No valid vCards with feed URLs found in the provided files.")
	}
	if title == "" {
		title = DefaultOPMLTitle
	}
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<opml version="2.0"><head><title>` + opmlEscaper.Replace(title) + `</title></head><body>` + strings.Join(outlines, "") + `</body></opml>`, nil
}

// osErrorString formats a file error in the traditional OS error style.
func osErrorString(err error, path string) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Sprintf("[Errno 2] No such file or directory: '%s'", path)
	case errors.Is(err, os.ErrPermission):
		return fmt.Sprintf("[Errno 13] Permission denied: '%s'", path)
	}
	return err.Error()
}
