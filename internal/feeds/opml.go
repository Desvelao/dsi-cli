package feeds

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Desvelao/dsi-cli/internal/core"
	"github.com/Desvelao/dsi-cli/internal/pyutil"
	"github.com/Desvelao/dsi-cli/internal/vcard"
)

var cardRe = regexp.MustCompile(`(?is)BEGIN:VCARD.*?END:VCARD`)

// categories merges the feed category and tags (both comma-separated) into one
// deduplicated comma-separated list.
func categories(category, tags string) string {
	var items []string
	seen := map[string]bool{}
	for _, value := range []string{category, tags} {
		for _, item := range strings.Split(value, ",") {
			item = pyutil.Strip(item)
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

// GenerateOPMLFromVCards reads vCard files and generates an OPML document with
// one outline per X-FEED. The category attribute holds the feed category and
// tags (comma-separated) and language the feed language, both only when
// present. Cards that cannot be read or parsed are skipped and reported in
// warnings.
func GenerateOPMLFromVCards(files []string, warnings *[]string) (string, error) {
	warn := func(format string, args ...any) {
		if warnings != nil {
			*warnings = append(*warnings, fmt.Sprintf(format, args...))
		}
	}
	var outlines []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			warn("Skipping %s: %s", file, pyOSError(err, file))
			continue
		}
		if msg, bad := core.Utf8DecodeError(data); bad {
			warn("Skipping %s: %s", file, msg)
			continue
		}
		text := string(data)
		cards := cardRe.FindAllString(text, -1)
		if len(cards) == 0 {
			cards = []string{text}
		}
		for _, card := range cards {
			profile := vcard.ParseVCard(card)
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
				outlines = append(outlines, opmlOutline(name, categories(feed.Category, feed.Tags), feed.URL, feed.Language))
			}
		}
	}
	if len(outlines) == 0 {
		return "", errors.New("No valid vCards with feed URLs found in the provided files.")
	}
	return `<opml version="2.0"><body>` + strings.Join(outlines, "") + `</body></opml>`, nil
}

// pyOSError formats a file error like Python's OSError.
func pyOSError(err error, path string) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Sprintf("[Errno 2] No such file or directory: '%s'", path)
	case errors.Is(err, os.ErrPermission):
		return fmt.Sprintf("[Errno 13] Permission denied: '%s'", path)
	}
	return err.Error()
}
