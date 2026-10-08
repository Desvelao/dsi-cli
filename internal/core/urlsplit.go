package core

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/Desvelao/dsi-cli/internal/strutil"
	"golang.org/x/text/unicode/norm"
)

// SplitResult mirrors the parts of a urlsplit result.
type SplitResult struct {
	Scheme, Netloc, Path, Query, Fragment string
}

const schemeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-."

var ipvFuture = regexp.MustCompile(`^v[a-fA-F0-9]+\..+$`)

// SplitURL splits a URL like urllib's urlsplit, including the
// errors it raises for malformed bracketed hosts and netlocs.
func SplitURL(rawurl string) (SplitResult, error) { return splitURL(rawurl, "") }

// splitURL is SplitURL with a default scheme for URLs that have none.
func splitURL(rawurl, defaultScheme string) (SplitResult, error) {
	url := strings.TrimLeftFunc(rawurl, func(r rune) bool { return r <= 0x20 })
	url = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(url)

	r := SplitResult{Scheme: defaultScheme}
	if i := strings.Index(url, ":"); i > 0 && isASCIIAlpha(url[0]) {
		ok := true
		for _, c := range url[:i] {
			if !strings.ContainsRune(schemeChars, c) {
				ok = false
				break
			}
		}
		if ok {
			r.Scheme = strings.ToLower(url[:i])
			url = url[i+1:]
		}
	}
	if strings.HasPrefix(url, "//") {
		delim := len(url)
		for _, c := range "/?#" {
			if w := strings.IndexRune(url[2:], c); w >= 0 && w+2 < delim {
				delim = w + 2
			}
		}
		r.Netloc, url = url[2:delim], url[delim:]
		hasOpen, hasClose := strings.Contains(r.Netloc, "["), strings.Contains(r.Netloc, "]")
		if hasOpen != hasClose {
			return r, errors.New("Invalid IPv6 URL")
		}
		if hasOpen {
			host := r.Netloc[strings.Index(r.Netloc, "[")+1:]
			host = host[:strings.Index(host, "]")]
			if err := checkBracketedHost(host); err != nil {
				return r, err
			}
		}
	}
	if i := strings.Index(url, "#"); i >= 0 {
		url, r.Fragment = url[:i], url[i+1:]
	}
	if i := strings.Index(url, "?"); i >= 0 {
		url, r.Query = url[:i], url[i+1:]
	}
	r.Path = url
	if err := checkNetloc(r.Netloc); err != nil {
		return r, err
	}
	return r, nil
}

func isASCIIAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func checkBracketedHost(host string) error {
	if strings.HasPrefix(host, "v") {
		if !ipvFuture.MatchString(host) {
			return errors.New("IPvFuture address is invalid")
		}
		return nil
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return errors.New("'" + host + "' does not appear to be an IPv4 or IPv6 address")
	}
	if addr.Is4() {
		return errors.New("An IPv4 address cannot be in brackets")
	}
	return nil
}

// checkNetloc rejects non-ASCII netlocs whose NFKC form contains delimiters.
func checkNetloc(netloc string) error {
	ascii := true
	for i := 0; i < len(netloc); i++ {
		if netloc[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return nil
	}
	n := norm.NFKC.String(netloc)
	for _, c := range "/?#@:" {
		if strings.ContainsRune(n, c) {
			return errors.New("netloc '" + netloc + "' contains invalid characters under NFKC normalization")
		}
	}
	return nil
}

// HasSpace reports whether the string contains any whitespace (isspace).
func HasSpace(s string) bool { return strings.IndexFunc(s, strutil.IsSpace) >= 0 }

func (r SplitResult) userinfo() (info string, have bool, hostinfo string) {
	if i := strings.LastIndex(r.Netloc, "@"); i >= 0 {
		return r.Netloc[:i], true, r.Netloc[i+1:]
	}
	return "", false, r.Netloc
}

// Username is the user part of the netloc ("" when absent, like a falsy None).
func (r SplitResult) Username() string {
	info, have, _ := r.userinfo()
	if !have {
		return ""
	}
	user, _, _ := strings.Cut(info, ":")
	return user
}

// Password is the password part of the netloc.
func (r SplitResult) Password() string {
	info, have, _ := r.userinfo()
	if !have {
		return ""
	}
	_, pass, _ := strings.Cut(info, ":")
	return pass
}

func (r SplitResult) hostinfo() (host, port string) {
	_, _, hostinfo := r.userinfo()
	if _, bracketed, ok := strings.Cut(hostinfo, "["); ok {
		var rest string
		host, rest, _ = strings.Cut(bracketed, "]")
		_, port, _ = strings.Cut(rest, ":")
		return host, port
	}
	host, port, _ = strings.Cut(hostinfo, ":")
	return host, port
}

// Hostname is the lower-cased host without brackets or port ("" when absent).
func (r SplitResult) Hostname() string {
	host, _ := r.hostinfo()
	if host == "" {
		return ""
	}
	name, zone, found := strings.Cut(host, "%")
	if found {
		return strings.ToLower(name) + "%" + zone
	}
	return strings.ToLower(name)
}

// Port parses the port. It returns (0, false, nil) when there is none and an
// error with the standard messages when it is not a number in 0-65535.
func (r SplitResult) Port() (port int, present bool, err error) {
	_, p := r.hostinfo()
	if p == "" {
		return 0, false, nil
	}
	for i := 0; i < len(p); i++ {
		if p[i] < '0' || p[i] > '9' {
			return 0, false, fmt.Errorf("Port could not be cast to integer value as %s", strutil.Repr(p))
		}
	}
	n, convErr := strconv.Atoi(p)
	if convErr != nil || n > 65535 {
		return 0, false, errors.New("Port out of range 0-65535")
	}
	return n, true, nil
}

var usesNetloc = map[string]bool{
	"": true, "ftp": true, "http": true, "gopher": true, "nntp": true, "telnet": true, "imap": true,
	"wais": true, "file": true, "mms": true, "https": true, "shttp": true, "snews": true, "prospero": true,
	"rtsp": true, "rtspu": true, "rsync": true, "svn": true, "svn+ssh": true, "sftp": true, "nfs": true,
	"git": true, "git+ssh": true, "ws": true, "wss": true,
}

var usesRelative = map[string]bool{
	"": true, "ftp": true, "http": true, "gopher": true, "nntp": true, "imap": true, "wais": true,
	"file": true, "https": true, "shttp": true, "mms": true, "prospero": true, "rtsp": true, "rtspu": true,
	"sftp": true, "svn": true, "svn+ssh": true, "ws": true, "wss": true,
}

var usesParams = map[string]bool{
	"": true, "ftp": true, "hdl": true, "prospero": true, "http": true, "imap": true, "https": true,
	"shttp": true, "rtsp": true, "rtspu": true, "sip": true, "sips": true, "mms": true, "sftp": true, "tel": true,
}

// Unsplit joins the parts like urllib.parse.urlunsplit.
func Unsplit(scheme, netloc, path, query, fragment string) string {
	url := path
	switch {
	case netloc != "":
		if url != "" && url[0] != '/' {
			url = "/" + url
		}
		url = "//" + netloc + url
	case strings.HasPrefix(url, "//"):
		url = "//" + url
	case scheme != "" && usesNetloc[scheme] && (url == "" || url[0] == '/'):
		url = "//" + url
	}
	if scheme != "" {
		url = scheme + ":" + url
	}
	if query != "" {
		url += "?" + query
	}
	if fragment != "" {
		url += "#" + fragment
	}
	return url
}

// splitParams splits ";params" off the last path segment.
func splitParams(path string) (string, string) {
	var i int
	if strings.Contains(path, "/") {
		i = strings.Index(path[strings.LastIndex(path, "/"):], ";")
		if i < 0 {
			return path, ""
		}
		i += strings.LastIndex(path, "/")
	} else {
		i = strings.Index(path, ";")
		if i < 0 {
			return path, ""
		}
	}
	return path[:i], path[i+1:]
}

type parsedURL struct {
	SplitResult
	Params string
}

func urlParse(url, defaultScheme string) (parsedURL, error) {
	r, err := splitURL(url, defaultScheme)
	if err != nil {
		return parsedURL{}, err
	}
	p := parsedURL{SplitResult: r}
	if usesParams[r.Scheme] && strings.Contains(r.Path, ";") {
		p.Path, p.Params = splitParams(r.Path)
	}
	return p, nil
}

func (p parsedURL) unparse() string {
	path := p.Path
	if p.Params != "" {
		path += ";" + p.Params
	}
	return Unsplit(p.Scheme, p.Netloc, path, p.Query, p.Fragment)
}

// URLJoin resolves ref against base like urllib.parse.urljoin.
func URLJoin(base, ref string) (string, error) {
	if base == "" {
		return ref, nil
	}
	if ref == "" {
		return base, nil
	}
	b, err := urlParse(base, "")
	if err != nil {
		return "", err
	}
	u, err := urlParse(ref, b.Scheme)
	if err != nil {
		return "", err
	}
	if u.Scheme != b.Scheme || !usesRelative[u.Scheme] {
		return ref, nil
	}
	if usesNetloc[u.Scheme] {
		if u.Netloc != "" {
			return u.unparse(), nil
		}
		u.Netloc = b.Netloc
	}
	if u.Path == "" && u.Params == "" {
		u.Path, u.Params = b.Path, b.Params
		if u.Query == "" {
			u.Query = b.Query
		}
		return u.unparse(), nil
	}
	baseParts := strings.Split(b.Path, "/")
	if baseParts[len(baseParts)-1] != "" {
		baseParts = baseParts[:len(baseParts)-1]
	}
	var segments []string
	if strings.HasPrefix(u.Path, "/") {
		segments = strings.Split(u.Path, "/")
	} else {
		segments = append(append([]string(nil), baseParts...), strings.Split(u.Path, "/")...)
		// segments[1:-1] = filter(None, segments[1:-1])
		if len(segments) > 2 {
			kept := []string{segments[0]}
			for _, seg := range segments[1 : len(segments)-1] {
				if seg != "" {
					kept = append(kept, seg)
				}
			}
			segments = append(kept, segments[len(segments)-1])
		}
	}
	var resolved []string
	for _, seg := range segments {
		switch seg {
		case "..":
			if len(resolved) > 0 {
				resolved = resolved[:len(resolved)-1]
			}
		case ".":
		default:
			resolved = append(resolved, seg)
		}
	}
	if last := segments[len(segments)-1]; last == "." || last == ".." {
		resolved = append(resolved, "")
	}
	path := strings.Join(resolved, "/")
	if path == "" {
		path = "/"
	}
	u.Path = path
	return u.unparse(), nil
}
