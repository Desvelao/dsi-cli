package core

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Desvelao/dsi-cli/internal/model"
	"github.com/Desvelao/dsi-cli/internal/strutil"
	"github.com/Desvelao/dsi-cli/internal/vcard"
)

var defaultPorts = map[string]int{"http": 80, "https": 443}

const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

var percentEscape = regexp.MustCompile(`%([0-9A-Fa-f]{2})`)

// normalizePercentEncoding uppercases percent-escapes and decodes those of
// unreserved characters.
func normalizePercentEncoding(component string) string {
	return percentEscape.ReplaceAllStringFunc(component, func(m string) string {
		n, _ := strconv.ParseUint(m[1:], 16, 8)
		if c := rune(n); strings.ContainsRune(unreserved, c) {
			return string(c)
		}
		return "%" + strings.ToUpper(m[1:])
	})
}

// removeDotSegments removes "." and ".." segments from a path (RFC 3986 §5.2.4).
func removeDotSegments(p string) string {
	var output []string
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		last := i == len(segments)-1
		switch seg {
		case ".":
			if last {
				output = append(output, "")
			}
		case "..":
			if len(output) > 1 {
				output = output[:len(output)-1]
			}
			if last {
				output = append(output, "")
			}
		default:
			output = append(output, seg)
		}
	}
	return strings.Join(output, "/")
}

// SourceMismatchError means the SOURCE of a fetched vCard does not match the
// URL it was fetched from.
type SourceMismatchError struct{ Msg string }

func (e *SourceMismatchError) Error() string { return e.Msg }

// NormalizeURL normalizes a URL for SOURCE comparison (RFC §5.1): lowercase
// scheme and host, default port and trailing host dot dropped, "/" for an empty
// path, normalized percent-encoding and dot segments, no fragment. Userinfo is
// preserved verbatim. It fails if
// the URL has an invalid port.
func NormalizeURL(rawurl string) (string, error) {
	parts, err := SplitURL(strutil.Strip(rawurl))
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(parts.Scheme)
	host := strings.TrimRight(strings.ToLower(parts.Hostname()), ".")
	if strings.Contains(host, ":") { // IPv6 literal
		host = "[" + host + "]"
	}
	port, present, err := parts.Port()
	if err != nil {
		return "", err
	}
	netloc := host
	if present && port != 0 && port != defaultPorts[scheme] {
		netloc = host + ":" + strconv.Itoa(port)
	}
	// Userinfo is kept verbatim so URLs differing only in credentials never
	// compare equal.
	if info, have, _ := parts.userinfo(); have {
		netloc = info + "@" + netloc
	}
	p := removeDotSegments(normalizePercentEncoding(parts.Path))
	if p == "" {
		p = "/"
	}
	return Unsplit(scheme, netloc, p, normalizePercentEncoding(parts.Query), ""), nil
}

// SourceMatches is true if the vCard SOURCE equals the requested URL after
// normalization.
func SourceMatches(requestedURL string, source *string) bool {
	if source == nil || *source == "" {
		return false
	}
	a, err := NormalizeURL(requestedURL)
	if err != nil {
		return false
	}
	b, err := NormalizeURL(*source)
	if err != nil {
		return false
	}
	return a == b
}

// pathName is PurePosixPath(p).name.
func pathName(p string) string {
	name := ""
	for _, seg := range strings.Split(p, "/") {
		if seg != "" && seg != "." {
			name = seg
		}
	}
	return name
}

// SafeFilename picks a file name that cannot escape the output directory.
func SafeFilename(contentDisposition, rawurl string) string {
	var candidates []string
	if contentDisposition != "" {
		candidates = append(candidates, dispositionFilename(contentDisposition))
	}
	if parts, err := SplitURL(rawurl); err == nil {
		candidates = append(candidates, pathName(parts.Path))
	} else {
		candidates = append(candidates, "")
	}
	for _, c := range candidates {
		name := pathName(strings.ReplaceAll(c, `\`, "/"))
		if name != "" && name != "." && name != ".." && !strings.HasPrefix(name, ".") {
			return name
		}
	}
	return "vcard.vcf"
}

// dispositionFilename extracts the filename parameter of a Content-Disposition
// header (first occurrence wins; filename*= RFC 2231 values are decoded).
func dispositionFilename(header string) string {
	var plain, extended string
	var havePlain, haveExtended bool
	for i, param := range splitHeaderParams(header) {
		if i == 0 {
			continue // the disposition type
		}
		name, value, ok := strings.Cut(param, "=")
		if !ok {
			continue
		}
		name = strings.ToLower(strutil.Strip(name))
		value = strutil.Strip(value)
		switch name {
		case "filename":
			if !havePlain {
				plain, havePlain = unquoteHeaderValue(value), true
			}
		case "filename*":
			if !haveExtended {
				extended, haveExtended = decodeRFC2231(value), true
			}
		}
	}
	if haveExtended {
		return strutil.Strip(extended)
	}
	return strutil.Strip(plain)
}

// splitHeaderParams splits on ';' outside double quotes.
func splitHeaderParams(s string) []string {
	var parts []string
	var cur strings.Builder
	inQuotes, escaped := false, false
	for _, r := range s {
		switch {
		case escaped:
			escaped = false
			cur.WriteRune(r)
		case r == '\\' && inQuotes:
			escaped = true
			cur.WriteRune(r)
		case r == '"':
			inQuotes = !inQuotes
			cur.WriteRune(r)
		case r == ';' && !inQuotes:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(parts, cur.String())
}

func unquoteHeaderValue(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
		v = strings.ReplaceAll(v, `\\`, `\`)
		v = strings.ReplaceAll(v, `\"`, `"`)
	}
	return v
}

// decodeRFC2231 decodes charset'language'percent-encoded values.
func decodeRFC2231(v string) string {
	v = unquoteHeaderValue(v)
	parts := strings.SplitN(v, "'", 3)
	if len(parts) != 3 {
		return v
	}
	decoded, err := url.PathUnescape(parts[2])
	if err != nil {
		return parts[2]
	}
	switch strings.ToLower(parts[0]) {
	case "", "utf-8", "us-ascii", "ascii":
		return decoded
	case "iso-8859-1", "latin-1", "latin1":
		runes := make([]rune, len(decoded))
		for i := 0; i < len(decoded); i++ {
			runes[i] = rune(decoded[i])
		}
		return string(runes)
	}
	return decoded
}

// FetchVCardFromURL fetches a vCard and returns (text, filename). It fails
// with a *FetchError if the resource cannot be fetched safely or is not a
// vCard, and with a *SourceMismatchError if verifySource is set and the
// returned SOURCE does not match the requested URL.
func (f *Fetcher) FetchVCardFromURL(rawurl string, allowHTTP, verifySource bool) (string, string, error) {
	opts := DefaultFetchOptions()
	opts.AllowHTTP = allowHTTP
	resp, err := f.FetchText(rawurl, opts)
	if err != nil {
		return "", "", err
	}
	text := resp.Text
	if !strings.HasPrefix(strutil.Strip(text), "BEGIN:VCARD") {
		return "", "", fetchErrorf("URL does not contain a valid vCard: %s", rawurl)
	}
	if verifySource {
		source := vcard.ParseVCard(text).Source
		if !SourceMatches(rawurl, source) {
			declared := "no SOURCE"
			if model.Str(source) != "" {
				declared = *source
			}
			return "", "", &SourceMismatchError{fmt.Sprintf("SOURCE mismatch for %s: the vCard declares %s", rawurl, declared)}
		}
	}
	return text, SafeFilename(resp.Header("Content-Disposition"), rawurl), nil
}

// FetchVCardFromURL fetches with the DefaultFetcher.
func FetchVCardFromURL(rawurl string, allowHTTP, verifySource bool) (string, string, error) {
	return DefaultFetcher.FetchVCardFromURL(rawurl, allowHTTP, verifySource)
}

// SaveVCardFromURL fetches a vCard from a URL and saves it into outputDir
// (the current directory when empty). It returns the destination path and the
// text. Without overwrite it fails with os.ErrExist if the file exists.
func (f *Fetcher) SaveVCardFromURL(rawurl, outputDir string, overwrite, allowHTTP, verifySource bool) (string, string, error) {
	text, filename, err := f.FetchVCardFromURL(rawurl, allowHTTP, verifySource)
	if err != nil {
		return "", "", err
	}
	if outputDir != "" {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return "", "", err
		}
	}
	if !FileIsVCardName(filename) {
		filename += ".vcf"
	}
	destination := filepath.Join(outputDir, filename)
	flags := os.O_WRONLY | os.O_CREATE
	if overwrite {
		// Never write through a pre-existing symlink: it could redirect the
		// write outside outputDir.
		if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("%s is a symlink; refusing to write through it", destination)
		}
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(destination, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", "", fmt.Errorf("%s already exists; refusing to overwrite it: %w", destination, os.ErrExist)
		}
		return "", "", err
	}
	defer file.Close()
	if _, err := file.WriteString(text); err != nil {
		return "", "", err
	}
	return destination, text, nil
}
