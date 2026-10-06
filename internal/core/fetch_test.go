package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const publicIP = "93.184.216.34"

// fakeNet wires a Fetcher to local test servers: DNS answers come from a map
// and every connection is redirected to the server, while the address the
// fetcher asked for (the pinned one) is recorded.
type fakeNet struct {
	mu      sync.Mutex
	dns     map[string][]string
	dialed  []string
	lookups []string
	target  string // real address of the test server
}

func (n *fakeNet) fetcher(srv *httptest.Server) *Fetcher {
	n.target = srv.Listener.Addr().String()
	pool := x509.NewCertPool()
	if srv.Certificate() != nil {
		pool.AddCert(srv.Certificate())
	}
	return &Fetcher{
		LookupHost: func(_ context.Context, host string) ([]string, error) {
			n.mu.Lock()
			defer n.mu.Unlock()
			n.lookups = append(n.lookups, host)
			if ips, ok := n.dns[host]; ok {
				return ips, nil
			}
			return []string{publicIP}, nil
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			n.mu.Lock()
			n.dialed = append(n.dialed, addr)
			n.mu.Unlock()
			var d net.Dialer
			return d.DialContext(ctx, network, n.target)
		},
		TLSConfig: &tls.Config{RootCAs: pool},
	}
}

func hostPort(srv *httptest.Server) string {
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	return "example.com:" + port
}

func newTLS(t *testing.T, h http.HandlerFunc) *httptest.Server {
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func textHandler(body string, headers map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		fmt.Fprint(w, body)
	}
}

func isFetchError(err error) bool {
	var fe *FetchError
	return errors.As(err, &fe)
}

func TestFetchHTTPSAndHTTP(t *testing.T) {
	srv := newTLS(t, textHandler("hello", map[string]string{"Content-Type": "text/plain"}))
	n := &fakeNet{}
	f := n.fetcher(srv)
	resp, err := f.FetchText("https://"+hostPort(srv)+"/a", DefaultFetchOptions())
	if err != nil || resp.Text != "hello" {
		t.Fatalf("%v %v", resp, err)
	}

	plain := httptest.NewServer(textHandler("x", nil))
	defer plain.Close()
	n2 := &fakeNet{}
	f2 := n2.fetcher(plain)
	opts := DefaultFetchOptions()
	if _, err := f2.FetchText("http://"+hostPort(plain)+"/a", opts); !isFetchError(err) {
		t.Errorf("plain http must be refused by default, got %v", err)
	}
	if len(n2.dialed) != 0 {
		t.Error("refused request must not connect")
	}
	opts.AllowHTTP = true
	if resp, err := f2.FetchText("http://"+hostPort(plain)+"/a", opts); err != nil || resp.Text != "x" {
		t.Errorf("allowed http: %v %v", resp, err)
	}
}

func TestOtherSchemesAndCredentialsRefused(t *testing.T) {
	f := &Fetcher{LookupHost: func(context.Context, string) ([]string, error) { return []string{publicIP}, nil }}
	opts := DefaultFetchOptions()
	opts.AllowHTTP = true
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/a", "gopher://x/", "https://user:pass@example.com/a"} {
		if _, err := f.FetchText(u, opts); !isFetchError(err) {
			t.Errorf("%s: expected FetchError, got %v", u, err)
		}
	}
}

func TestPrivateAddressesRefused(t *testing.T) {
	for _, addrs := range [][]string{
		{"127.0.0.1"}, {"10.0.0.5"}, {"192.168.1.1"}, {"169.254.169.254"}, {"::1"},
		{publicIP, "10.0.0.5"}, // one private address is enough to refuse
	} {
		dialed := false
		f := &Fetcher{
			LookupHost:  func(context.Context, string) ([]string, error) { return addrs, nil },
			DialContext: func(context.Context, string, string) (net.Conn, error) { dialed = true; return nil, errors.New("no") },
		}
		if _, err := f.FetchText("https://internal.example/a", DefaultFetchOptions()); !isFetchError(err) {
			t.Errorf("%v: expected FetchError, got %v", addrs, err)
		}
		if dialed {
			t.Errorf("%v: must not connect", addrs)
		}
	}
}

func TestRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = newTLS(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusMovedPermanently)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/private":
			http.Redirect(w, r, "https://internal.example/x", http.StatusFound)
		case "/noloc":
			w.WriteHeader(http.StatusFound)
		default:
			fmt.Fprint(w, "done")
		}
	})
	n := &fakeNet{dns: map[string][]string{"internal.example": {"127.0.0.1"}}}
	f := n.fetcher(srv)
	base := "https://" + hostPort(srv)

	resp, err := f.FetchText(base+"/a", DefaultFetchOptions())
	if err != nil || resp.URL != base+"/b" || resp.Text != "done" {
		t.Errorf("relative redirect: %+v %v", resp, err)
	}

	n.dialed = nil
	if _, err := f.FetchText(base+"/private", DefaultFetchOptions()); !isFetchError(err) || !strings.Contains(err.Error(), "non-public") {
		t.Errorf("redirect to private: %v", err)
	}
	if len(n.dialed) != 1 {
		t.Errorf("only the first hop may connect, dialed %v", n.dialed)
	}

	opts := DefaultFetchOptions()
	opts.MaxRedirects = 3
	n.dialed = nil
	if _, err := f.FetchText(base+"/loop", opts); !isFetchError(err) || !strings.Contains(err.Error(), "Too many redirects") {
		t.Errorf("redirect limit: %v", err)
	}
	if len(n.dialed) != 4 {
		t.Errorf("expected 4 hops, dialed %d", len(n.dialed))
	}
	if _, err := f.FetchText(base+"/noloc", DefaultFetchOptions()); !isFetchError(err) {
		t.Errorf("redirect without Location: %v", err)
	}
	_ = srv
}

func TestLimitsAndContentType(t *testing.T) {
	srv := newTLS(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Write([]byte(strings.Repeat("a", 5000)))
		case "/declared":
			w.Header().Set("Content-Length", "999999")
			w.Write([]byte("a"))
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>"))
		case "/vcard":
			w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
			w.Write([]byte("ok"))
		case "/binary":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte{0xff, 0xfe})
		case "/missing":
			http.NotFound(w, r)
		}
	})
	n := &fakeNet{}
	f := n.fetcher(srv)
	base := "https://" + hostPort(srv)
	opts := DefaultFetchOptions()
	opts.MaxBytes = 1000
	for _, p := range []string{"/big", "/missing"} {
		if _, err := f.FetchText(base+p, opts); !isFetchError(err) {
			t.Errorf("%s: %v", p, err)
		}
	}
	// A declared Content-Length over the limit is refused. net/http's server
	// rejects the mismatching body, so only the error type is asserted.
	if _, err := f.FetchText(base+"/declared", opts); err == nil {
		t.Error("declared length over limit must fail")
	}
	strict := DefaultFetchOptions()
	strict.AcceptedTypes = []string{"text/vcard"}
	if _, err := f.FetchText(base+"/html", strict); !isFetchError(err) {
		t.Errorf("html: %v", err)
	}
	if _, err := f.FetchText(base+"/vcard", strict); err != nil {
		t.Errorf("text/vcard with charset: %v", err)
	}
	if _, err := f.FetchText(base+"/binary", DefaultFetchOptions()); !isFetchError(err) || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Errorf("invalid utf-8: %v", err)
	}
}

func TestConnectionIsPinnedToValidatedIP(t *testing.T) {
	var gotHost, serverName string
	var mu sync.Mutex
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotHost = r.Host
		mu.Unlock()
		fmt.Fprint(w, "ok")
	}))
	srv.EnableHTTP2 = false
	srv.TLS = &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		serverName = h.ServerName
		mu.Unlock()
		return nil, nil
	}}
	srv.StartTLS()
	defer srv.Close()

	// Public at check time, loopback on any later lookup (DNS rebinding).
	calls := 0
	n := &fakeNet{}
	f := n.fetcher(srv)
	f.LookupHost = func(context.Context, string) ([]string, error) {
		calls++
		if calls == 1 {
			return []string{publicIP}, nil
		}
		return []string{"127.0.0.1"}, nil
	}
	// Proxy variables must be ignored.
	t.Setenv("HTTPS_PROXY", "http://proxy.internal:3128")
	t.Setenv("https_proxy", "http://proxy.internal:3128")

	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	if _, err := f.FetchText("https://example.com:"+port+"/a", DefaultFetchOptions()); err != nil {
		t.Fatal(err)
	}
	if len(n.dialed) != 1 || n.dialed[0] != net.JoinHostPort(publicIP, port) {
		t.Errorf("connection must use the validated IP, dialed %v", n.dialed)
	}
	if calls != 1 {
		t.Errorf("host must be resolved once per hop, got %d lookups", calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotHost != "example.com:"+port {
		t.Errorf("Host header = %q", gotHost)
	}
	if serverName != "example.com" {
		t.Errorf("SNI = %q", serverName)
	}
}

func vcardText(source string) string {
	return "BEGIN:VCARD\nVERSION:4.0\nFN:Alice\nSOURCE:" + source + "\nEND:VCARD\n"
}

func vcardServer(t *testing.T, body func(self string) string, headers map[string]string) (*httptest.Server, *Fetcher, string) {
	var url string
	srv := newTLS(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/vcard")
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		fmt.Fprint(w, body(url))
	})
	n := &fakeNet{}
	f := n.fetcher(srv)
	url = "https://" + hostPort(srv) + "/dsi.vcf"
	return srv, f, url
}

func TestFetchVCardSourceVerification(t *testing.T) {
	_, f, url := vcardServer(t, func(self string) string { return vcardText(self) }, nil)
	text, filename, err := f.FetchVCardFromURL(url, false, true)
	if err != nil || !strings.Contains(text, "FN:Alice") || filename != "dsi.vcf" {
		t.Errorf("matching source: %q %q %v", text, filename, err)
	}

	for name, body := range map[string]string{
		"substitution": vcardText("https://evil.example/bob.vcf"),
		"bad port":     vcardText("https://alice.example:abc/dsi.vcf"),
		"port range":   vcardText("https://alice.example:99999/x"),
		"missing":      "BEGIN:VCARD\nVERSION:4.0\nFN:A\nEND:VCARD\n",
	} {
		body := body
		_, f, url := vcardServer(t, func(string) string { return body }, nil)
		_, _, err := f.FetchVCardFromURL(url, false, true)
		var mismatch *SourceMismatchError
		if !errors.As(err, &mismatch) {
			t.Errorf("%s: expected SourceMismatchError, got %v", name, err)
		}
		if _, _, err := f.FetchVCardFromURL(url, false, false); err != nil {
			t.Errorf("%s: verification disabled must accept, got %v", name, err)
		}
	}

	_, f, url = vcardServer(t, func(string) string { return "<html></html>" }, nil)
	if _, _, err := f.FetchVCardFromURL(url, false, true); !isFetchError(err) {
		t.Errorf("non-vcard: %v", err)
	}
}

func TestFetchVCardFilenameFromContentDisposition(t *testing.T) {
	_, f, url := vcardServer(t, func(self string) string { return vcardText(self) },
		map[string]string{"Content-Disposition": `attachment; filename="../../etc/cron.d/x"`})
	_, filename, err := f.FetchVCardFromURL(url, false, true)
	if err != nil || filename != "x" {
		t.Errorf("filename %q err %v", filename, err)
	}
}

func TestSaveVCardFromURL(t *testing.T) {
	_, f, url := vcardServer(t, func(self string) string { return vcardText(self) }, nil)
	dir := t.TempDir()
	dest, text, err := f.SaveVCardFromURL(url, dir, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != text || filepath.Base(dest) != "dsi.vcf" {
		t.Errorf("saved %q to %s", got, dest)
	}
	if _, _, err := f.SaveVCardFromURL(url, dir, false, false, true); !errors.Is(err, os.ErrExist) {
		t.Errorf("existing file must be refused, got %v", err)
	}
	os.WriteFile(dest, []byte("old"), 0o644)
	if _, text, err := f.SaveVCardFromURL(url, dir, true, false, true); err != nil {
		t.Errorf("overwrite: %v", err)
	} else if got, _ := os.ReadFile(dest); string(got) != text {
		t.Errorf("not overwritten: %q", got)
	}
	if _, _, err := f.SaveVCardFromURL(url, dir, false, false, true); !errors.Is(err, os.ErrExist) {
		t.Error("expected exists after overwrite")
	}
}
