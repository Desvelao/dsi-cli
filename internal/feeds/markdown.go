package feeds

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Desvelao/dsi-cli/internal/core"
	"github.com/Desvelao/dsi-cli/internal/pyutil"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// FeedFileError means a Markdown feed file is malformed; the message names the file.
type FeedFileError struct{ Msg string }

func (e *FeedFileError) Error() string { return e.Msg }

// State is one feed item parsed from a Markdown post.
type State struct {
	ID          string
	Title       string
	Date        time.Time // UTC
	Link        string    // "" when not set
	Image       string    // "" when not set
	Content     string
	ContentType string // "html" or "text"
	Metadata    *pyutil.OrderedMap
}

func unquote(value string) string {
	if len(value) >= 2 && value[0] == value[len(value)-1] && (value[0] == '"' || value[0] == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

// parseFrontMatter extracts the "key: value" front matter delimited by "---"
// and returns it with the remaining content lines.
func parseFrontMatter(lines []string) (*pyutil.OrderedMap, []string, error) {
	front := pyutil.NewOrderedMap()
	if len(lines) > 0 && pyutil.Strip(lines[0]) == "---" {
		i := 1
		for i < len(lines) && pyutil.Strip(lines[i]) != "---" {
			line := pyutil.Strip(lines[i])
			if key, value, ok := strings.Cut(line, ":"); ok {
				front.Set(pyutil.Strip(key), unquote(pyutil.Strip(value)))
			}
			i++
		}
		if i >= len(lines) {
			return nil, nil, fmt.Errorf("unterminated front matter (missing closing '---')")
		}
		return front, lines[i+1:], nil
	}
	return front, lines, nil
}

var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.DefinitionList, extension.Footnote),
	goldmark.WithRendererOptions(html.WithUnsafe(), html.WithXHTML()),
)

var (
	imgAttrOrder = regexp.MustCompile(`<img src="([^"]*)" alt="([^"]*)"`)
	cellAlign    = regexp.MustCompile(`<(th|td) align="(left|right|center)">`)
)

// RenderMarkdown converts Markdown to HTML. It approximates Python-Markdown
// with the "extra" extensions (the output has no trailing newline, img
// attributes are ordered alt/src/title and table cells use style="text-align").
// Python-Markdown follows the original Markdown rules and goldmark follows
// CommonMark, so edge cases differ (see the known differences in the tests);
// abbreviations, attribute lists and markdown="1" blocks are not supported.
func RenderMarkdown(text string) (string, error) {
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(text), &buf); err != nil {
		return "", err
	}
	out := strings.TrimRight(buf.String(), "\n")
	out = imgAttrOrder.ReplaceAllString(out, `<img alt="$2" src="$1"`)
	out = cellAlign.ReplaceAllString(out, `<$1 style="text-align: $2;">`)
	return out, nil
}

// pathSuffix mirrors PurePath.suffix.
func pathSuffix(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 || i >= len(name)-1 {
		return ""
	}
	return name[i:]
}

// ParseFile extracts the metadata and content of a Markdown post. The default
// item id is the slugified path relative to root (without extension); without
// root the file name is used.
func ParseFile(path, root string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if msg, bad := core.Utf8DecodeError(data); bad {
		return nil, fmt.Errorf("%s", msg)
	}
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	metadata, contentLines, err := parseFrontMatter(pyutil.SplitLines(text))
	if err != nil {
		return nil, &FeedFileError{fmt.Sprintf("%s: %v", path, err)}
	}

	base := filepath.Base(path)
	if metadata.Value("title") == "" {
		metadata.Set("title", strings.TrimSuffix(base, pathSuffix(base)))
	}
	if metadata.Value("date") == "" {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		metadata.Set("date", info.ModTime().UTC().Format("2006-01-02T15:04:05Z"))
	}
	date, err := ParseISODate(metadata.Value("date"))
	if err != nil {
		return nil, &FeedFileError{fmt.Sprintf("%s: invalid date '%s' (expected ISO 8601, e.g. 2025-01-31 or 2025-01-31T12:00:00Z)",
			path, metadata.Value("date"))}
	}

	metadata.Set("file_path", path)
	metadata.Set("file_name", base)
	metadata.Set("file_dir", pythonDirname(path))
	metadata.Set("file_ext", pathSuffix(base))

	content := strings.Join(contentLines, "\n")
	useHTML := false
	switch strings.ToLower(metadataOr(metadata, "use_html_content", "false")) {
	case "true", "yes":
		useHTML = true
	}
	contentType := "text"
	if useHTML {
		contentType = "html"
		if content, err = RenderMarkdown(content); err != nil {
			return nil, err
		}
	}

	defaultID := core.Slugify(strings.TrimSuffix(base, pathSuffix(base)))
	if root != "" {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		defaultID = core.Slugify(strings.TrimSuffix(rel, pathSuffix(rel)))
	}
	id := metadata.Value("id")
	if id == "" {
		id = defaultID
	}
	return &State{
		ID:          id,
		Title:       metadata.Value("title"),
		Date:        date,
		Link:        metadata.Value("link"),
		Image:       metadata.Value("image"),
		Content:     content,
		ContentType: contentType,
		Metadata:    metadata,
	}, nil
}

func metadataOr(m *pyutil.OrderedMap, key, def string) string {
	if v, ok := m.Get(key); ok {
		return v
	}
	return def
}

// pythonDirname is os.path.dirname: "" for a bare file name.
func pythonDirname(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return ""
	}
	head := path[:i+1]
	if strings.Trim(head, "/") != "" {
		head = strings.TrimRight(head, "/")
	}
	return head
}

// Collect parses the Markdown posts of a directory (or a single file), newest first.
func Collect(directory string) ([]*State, error) {
	info, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}
	var files []string
	root := directory
	if info.Mode().IsRegular() {
		files, root = []string{directory}, filepath.Dir(directory)
	} else {
		err = filepath.WalkDir(directory, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	states := make([]*State, 0, len(files))
	for _, f := range files {
		s, err := ParseFile(f, root)
		if err != nil {
			return nil, err
		}
		states = append(states, s)
	}
	sort.SliceStable(states, func(i, j int) bool { return states[i].Date.After(states[j].Date) })
	return states, nil
}

// CreateStateContent builds the content of a new post with front matter.
func CreateStateContent(title, message, date string) string {
	var attrs []string
	if title != "" {
		attrs = append(attrs, "title: "+title)
	}
	if date != "" {
		attrs = append(attrs, "date: "+date)
	}
	return "---\n" + strings.Join(attrs, "\n") + "\n---\n" + message + "\n"
}
