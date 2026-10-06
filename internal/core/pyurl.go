package core

import (
	"errors"
	"net/netip"
	"regexp"
	"strings"

	"github.com/Desvelao/dsipy/internal/pyutil"
	"golang.org/x/text/unicode/norm"
)

// SplitResult mirrors the parts of Python's urllib.parse.urlsplit result.
type SplitResult struct {
	Scheme, Netloc, Path, Query, Fragment string
}

const schemeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-."

var ipvFuture = regexp.MustCompile(`^v[a-fA-F0-9]+\..+$`)

// SplitURL splits a URL like Python 3.12's urllib.parse.urlsplit, including the
// errors it raises for malformed bracketed hosts and netlocs.
func SplitURL(rawurl string) (SplitResult, error) {
	url := strings.TrimLeftFunc(rawurl, func(r rune) bool { return r <= 0x20 })
	url = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(url)

	var r SplitResult
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

// HasSpace reports whether the string contains any whitespace (Python isspace).
func HasSpace(s string) bool { return strings.IndexFunc(s, pyutil.IsSpace) >= 0 }
