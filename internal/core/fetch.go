package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

// Hardened HTTP fetching for remote DSI resources.
//
// Protections: HTTPS by default, redirect limit (each hop re-validated),
// response-size cap on the decoded body, connect/read timeouts, content-type
// filtering and refusal of non-public addresses (SSRF).
//
// The host is resolved once and checked; the connection is then restricted to
// that validated set of IPs, tried in order until one connects (TLS SNI and
// certificate checks still use the original hostname and the Host header is
// preserved). Nothing is re-resolved, so DNS rebinding cannot swap an address.
// Proxy environment variables are ignored: a local DNS check says nothing about
// where a proxy connects.

// Fetch defaults.
const (
	DefaultMaxBytes     = 1_000_000
	DefaultMaxRedirects = 5
	ConnectTimeout      = 5 * time.Second
	ReadTimeout         = 10 * time.Second
	// totalTimeout bounds a whole fetch (requests has no overall timeout).
	totalTimeout = 60 * time.Second
)

// DefaultAcceptedTypes are the content-type prefixes accepted by default.
var DefaultAcceptedTypes = []string{"text/", "application/octet-stream", "application/vcard"}

// FetchError is returned when a remote resource cannot be fetched safely.
type FetchError struct{ Msg string }

func (e *FetchError) Error() string { return e.Msg }

func fetchErrorf(format string, args ...any) error {
	return &FetchError{fmt.Sprintf(format, args...)}
}

// FetchedResponse is a fetched text resource.
type FetchedResponse struct {
	Text    string
	URL     string // final URL after redirects
	Headers http.Header
}

// Header returns a response header value.
func (r *FetchedResponse) Header(name string) string { return r.Headers.Get(name) }

// FetchOptions tunes FetchText. Use DefaultFetchOptions for the defaults.
type FetchOptions struct {
	AllowHTTP     bool
	MaxBytes      int
	MaxRedirects  int
	AcceptedTypes []string // nil accepts everything
}

// DefaultFetchOptions returns the default limits.
func DefaultFetchOptions() FetchOptions {
	return FetchOptions{MaxBytes: DefaultMaxBytes, MaxRedirects: DefaultMaxRedirects, AcceptedTypes: DefaultAcceptedTypes}
}

// Fetcher fetches remote text resources. The zero value uses the system
// resolver and dialer; tests replace the hooks.
type Fetcher struct {
	// LookupHost resolves a host to IP address strings.
	LookupHost func(ctx context.Context, host string) ([]string, error)
	// DialContext connects to a network address (always a validated IP).
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// TLSConfig is the base TLS configuration (nil uses the system roots).
	TLSConfig *tls.Config
}

// DefaultFetcher is used by the package-level helpers.
var DefaultFetcher = &Fetcher{}

func (f *Fetcher) lookup(ctx context.Context, host string) ([]string, error) {
	if f.LookupHost != nil {
		return f.LookupHost(ctx, host)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = a.String()
	}
	return out, nil
}

func (f *Fetcher) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if f.DialContext != nil {
		return f.DialContext(ctx, network, addr)
	}
	d := net.Dialer{Timeout: ConnectTimeout}
	return d.DialContext(ctx, network, addr)
}

// AssertPublicHost fails unless every address of the host is globally
// routable. It returns the validated addresses.
func (f *Fetcher) AssertPublicHost(ctx context.Context, host string) ([]string, error) {
	addresses, err := f.lookup(ctx, host)
	if err != nil {
		return nil, fetchErrorf("Cannot resolve host '%s': %v", host, err)
	}
	if len(addresses) == 0 {
		return nil, fetchErrorf("Host '%s' did not resolve to any address", host)
	}
	for _, address := range addresses {
		ip, err := netip.ParseAddr(strings.SplitN(address, "%", 2)[0])
		if err != nil {
			return nil, fetchErrorf("Cannot resolve host '%s': invalid address %q", host, address)
		}
		if !IsGlobal(ip) {
			return nil, fetchErrorf("Refusing to fetch '%s': it resolves to a non-public address (%s)", host, ip)
		}
	}
	return addresses, nil
}

// validateURL checks the scheme, host and credentials and returns the host and port.
func validateURL(url string, allowHTTP bool) (host string, port int, err error) {
	parts, err := SplitURL(url)
	if err != nil {
		return "", 0, err
	}
	allowed := []string{"https"}
	if allowHTTP {
		allowed = []string{"https", "http"}
	}
	ok := false
	for _, a := range allowed {
		ok = ok || parts.Scheme == a
	}
	if !ok {
		return "", 0, fetchErrorf("Unsupported URL scheme '%s' in %s (allowed: %s)", parts.Scheme, url, strings.Join(allowed, ", "))
	}
	host = parts.Hostname()
	if host == "" {
		return "", 0, fetchErrorf("URL has no host: %s", url)
	}
	if parts.Username() != "" || parts.Password() != "" {
		return "", 0, fetchErrorf("URLs with embedded credentials are not allowed")
	}
	port, _, err = parts.Port()
	if err != nil {
		return "", 0, err
	}
	if port == 0 { // no port: use the default
		port = 443
		if parts.Scheme == "http" {
			port = 80
		}
	}
	return host, port, nil
}

// deadlineConn refreshes the read deadline before every read, so the read
// timeout applies per socket read like in the requests library.
type deadlineConn struct {
	net.Conn
	timeout time.Duration
}

func (c deadlineConn) Read(b []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.timeout))
	return c.Conn.Read(b)
}

var redirectStatuses = map[int]bool{301: true, 302: true, 303: true, 307: true, 308: true}

// FetchText fetches a text resource following the safety rules above.
func (f *Fetcher) FetchText(url string, opts FetchOptions) (*FetchedResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()
	current := url
	for i := 0; i <= opts.MaxRedirects; i++ {
		host, port, err := validateURL(current, opts.AllowHTTP)
		if err != nil {
			return nil, err
		}
		addresses, err := f.AssertPublicHost(ctx, host)
		if err != nil {
			return nil, err
		}
		pinned := make([]string, len(addresses))
		for i, a := range addresses {
			pinned[i] = net.JoinHostPort(a, strconv.Itoa(port))
		}

		resp, closeFn, err := f.get(ctx, current, host, pinned)
		if err != nil {
			return nil, fetchErrorf("Failed to fetch %s: %v", current, err)
		}
		next, result, err := f.handle(resp, current, url, opts)
		closeFn()
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
		current = next
	}
	return nil, fetchErrorf("Too many redirects (limit %d) fetching %s", opts.MaxRedirects, url)
}

// get performs one request without following redirects, connecting to the
// validated addresses (in order, first success wins) while keeping the
// original host for Host/SNI/TLS checks.
func (f *Fetcher) get(ctx context.Context, url, host string, pinned []string) (*http.Response, func(), error) {
	tlsConfig := &tls.Config{}
	if f.TLSConfig != nil {
		tlsConfig = f.TLSConfig.Clone()
	}
	tlsConfig.ServerName = host
	transport := &http.Transport{
		Proxy: nil, // ignore proxy environment variables
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var lastErr error
			for _, addr := range pinned {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				conn, err := f.dial(ctx, network, addr)
				if err == nil {
					return deadlineConn{conn, ReadTimeout}, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		TLSClientConfig:     tlsConfig,
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: ConnectTimeout,
	}
	client := &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "dsi")
	req.Header.Set("Accept", "*/*")
	resp, err := client.Do(req)
	if err != nil {
		transport.CloseIdleConnections()
		var urlErr *neturl.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // the request URL is already part of the message
		}
		return nil, nil, err
	}
	return resp, func() { resp.Body.Close(); transport.CloseIdleConnections() }, nil
}

// handle interprets a response: it returns the next URL for a redirect, or the
// final result.
func (f *Fetcher) handle(resp *http.Response, current, original string, opts FetchOptions) (string, *FetchedResponse, error) {
	if redirectStatuses[resp.StatusCode] {
		location := resp.Header.Get("Location")
		if location == "" {
			return "", nil, fetchErrorf("Redirect without Location from %s", current)
		}
		next, err := URLJoin(current, location)
		if err != nil {
			return "", nil, fetchErrorf("Invalid redirect location %q from %s: %v", location, current, err)
		}
		return next, nil, nil
	}
	if resp.StatusCode >= 400 {
		return "", nil, fetchErrorf("HTTP %d fetching %s", resp.StatusCode, current)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	if len(opts.AcceptedTypes) > 0 && contentType != "" {
		accepted := false
		for _, prefix := range opts.AcceptedTypes {
			accepted = accepted || strings.HasPrefix(contentType, prefix)
		}
		if !accepted {
			return "", nil, fetchErrorf("Unexpected content type '%s'", contentType)
		}
	}
	body, err := readLimited(resp, opts.MaxBytes)
	if err != nil {
		return "", nil, err
	}
	if msg, bad := Utf8DecodeError(body); bad {
		return "", nil, fetchErrorf("Response is not valid UTF-8: %s", msg)
	}
	return "", &FetchedResponse{Text: string(body), URL: current, Headers: resp.Header}, nil
}

func readLimited(resp *http.Response, maxBytes int) ([]byte, error) {
	if declared := resp.Header.Get("Content-Length"); declared != "" && isDigits(declared) {
		if n, err := strconv.Atoi(declared); err != nil || n > maxBytes {
			return nil, fetchErrorf("Response too large (%s bytes, limit %d)", declared, maxBytes)
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return nil, fetchErrorf("Failed to read response: %v", err)
	}
	if len(body) > maxBytes {
		return nil, fetchErrorf("Response exceeds the %d byte limit", maxBytes)
	}
	return body, nil
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// FetchText fetches with the DefaultFetcher.
func FetchText(url string, opts FetchOptions) (*FetchedResponse, error) {
	return DefaultFetcher.FetchText(url, opts)
}

// Utf8DecodeError reports why body is not valid UTF-8, using the standard
// UnicodeDecodeError wording. It returns ("", false) for valid UTF-8.
func Utf8DecodeError(body []byte) (string, bool) {
	for i := 0; i < len(body); {
		b := body[i]
		if b < 0x80 {
			i++
			continue
		}
		need := 0
		switch {
		case b >= 0xC2 && b <= 0xDF:
			need = 1
		case b >= 0xE0 && b <= 0xEF:
			need = 2
		case b >= 0xF0 && b <= 0xF4:
			need = 3
		default:
			return utf8Message(body, i, i, "invalid start byte"), true
		}
		for j := 1; j <= need; j++ {
			if i+j >= len(body) {
				return utf8Message(body, i, len(body)-1, "unexpected end of data"), true
			}
			lo, hi := byte(0x80), byte(0xBF)
			if j == 1 {
				switch b {
				case 0xE0:
					lo = 0xA0
				case 0xED:
					hi = 0x9F
				case 0xF0:
					lo = 0x90
				case 0xF4:
					hi = 0x8F
				}
			}
			if c := body[i+j]; c < lo || c > hi {
				return utf8Message(body, i, i+j-1, "invalid continuation byte"), true
			}
		}
		i += need + 1
	}
	return "", false
}

func utf8Message(body []byte, start, end int, reason string) string {
	if start == end {
		return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s", body[start], start, reason)
	}
	return fmt.Sprintf("'utf-8' codec can't decode bytes in position %d-%d: %s", start, end, reason)
}
