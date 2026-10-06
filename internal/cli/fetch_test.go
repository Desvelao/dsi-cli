package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Desvelao/dsipy/internal/core"
)

// remote serves vCards over TLS; the text can change between runs.
type remote struct {
	mu   sync.Mutex
	text string
	url  string
}

func (r *remote) set(text string) { r.mu.Lock(); r.text = text; r.mu.Unlock() }

func newRemote(t *testing.T, h *harness) *remote {
	r := &remote{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		w.Header().Set("Content-Type", "text/vcard")
		fmt.Fprint(w, r.text)
	}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	r.url = "https://example.com:" + port + "/a.vcf"
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	h.env.Fetcher = &core.Fetcher{
		LookupHost: func(context.Context, string) ([]string, error) { return []string{"93.184.216.34"}, nil },
		DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, n, srv.Listener.Addr().String())
		},
		TLSConfig: &tls.Config{RootCAs: pool},
	}
	return r
}

func (r *remote) card(name string) string {
	return fmt.Sprintf("BEGIN:VCARD\nVERSION:4.0\nFN:%s\nSOURCE:%s\nEND:VCARD\n", name, r.url)
}

func listDir(t *testing.T, dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestFetchURLDryRunNewDestinationWritesNothing(t *testing.T) {
	h := newHarness(t)
	r := newRemote(t, h)
	r.set(r.card("New"))
	code, out := h.run("", "vcard", "fetch", "--dry-run", r.url)
	h.expect(code, out, 0)
	if names := listDir(t, h.dir); len(names) != 0 {
		t.Errorf("dry run wrote %v", names)
	}
	contains(t, out, "Would download: 1", "Done.")
}

func TestFetchURLDryRunExistingDestinationUntouched(t *testing.T) {
	h := newHarness(t)
	r := newRemote(t, h)
	r.set(r.card("New"))
	h.write("a.vcf", r.card("Old"))
	code, out := h.run("", "vcard", "fetch", "--dry-run", "--backup", r.url)
	h.expect(code, out, 0)
	if h.read("a.vcf") != r.card("Old") || len(listDir(t, h.dir)) != 1 {
		t.Error("dry run must not touch anything")
	}
	contains(t, out, "Would update: 1")
}

func TestFetchFileInputDryRunUntouched(t *testing.T) {
	h := newHarness(t)
	r := newRemote(t, h)
	r.set(r.card("New"))
	h.write("b.vcf", r.card("Old"))
	code, out := h.run("", "vcard", "fetch", "--dry-run", "--backup", "b.vcf")
	h.expect(code, out, 0)
	if h.read("b.vcf") != r.card("Old") || len(listDir(t, h.dir)) != 1 {
		t.Error("dry run must not touch anything")
	}
	contains(t, out, "Would update: 1")
	notContains(t, out, "  Updated:")
}

func TestFetchUnchangedIsNotCountedOrBackedUp(t *testing.T) {
	h := newHarness(t)
	r := newRemote(t, h)
	r.set(r.card("Old"))
	h.write("b.vcf", r.card("Old"))
	code, out := h.run("", "vcard", "fetch", "--backup", "b.vcf")
	h.expect(code, out, 0)
	contains(t, out, "Updated: 0", "Unchanged: 1")
	if len(listDir(t, h.dir)) != 1 {
		t.Errorf("unexpected files %v", listDir(t, h.dir))
	}
	h.write("a.vcf", r.card("Old"))
	code, out = h.run("", "vcard", "fetch", "--backup", r.url)
	h.expect(code, out, 0)
	contains(t, out, "Updated: 0", "Downloaded: 0", "Unchanged: 1")
}

func TestFetchURLDiff(t *testing.T) {
	h := newHarness(t)
	r := newRemote(t, h)
	r.set(r.card("New"))
	h.write("a.vcf", r.card("Old"))
	code, out := h.run("", "vcard", "fetch", "--dry-run", "--diff", r.url)
	h.expect(code, out, 0)
	contains(t, out, "-FN:Old", "+FN:New", "--- a.vcf", "+++ (fetched)")
	code, out = h.run("", "vcard", "fetch", "--dry-run", "--diff", "--no-verify-source", r.url)
	h.expect(code, out, 0)
}

func TestFetchURLBackupAndUpdate(t *testing.T) {
	h := newHarness(t)
	r := newRemote(t, h)
	r.set(r.card("New"))
	h.write("a.vcf", r.card("Old"))
	code, out := h.run("", "vcard", "fetch", "--backup", r.url)
	h.expect(code, out, 0)
	if h.read("a.vcf") != r.card("New") || h.read("a.vcf.bak") != r.card("Old") {
		t.Error("expected updated file and backup")
	}
	contains(t, out, "Updated: 1")
}

func TestFetchURLNewDestinationIsDownloaded(t *testing.T) {
	h := newHarness(t)
	r := newRemote(t, h)
	r.set(r.card("New"))
	code, out := h.run("", "vcard", "fetch", r.url)
	h.expect(code, out, 0)
	if h.read("a.vcf") != r.card("New") {
		t.Error("not downloaded")
	}
	contains(t, out, "Downloaded: 1")
}

func TestFetchOutputDir(t *testing.T) {
	h := newHarness(t)
	r := newRemote(t, h)
	r.set(r.card("New"))
	h.write("b.vcf", r.card("Old"))
	code, out := h.run("", "vcard", "fetch", "-o", "out", "b.vcf", r.url)
	h.expect(code, out, 0)
	if h.read("out/b.vcf") != r.card("New") || h.read("out/a.vcf") != r.card("New") || h.read("b.vcf") != r.card("Old") {
		t.Errorf("output dir: %v", listDir(t, h.dir))
	}
}

func TestFetchFailureDoesNotSayDone(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "vcard", "fetch", "http://example.com/dsi.vcf", "--dry-run")
	h.expect(code, out, 1)
	notContains(t, out, "Done.")
	contains(t, out, "1 item(s) failed", "Failed: 1")
}

func TestFetchNoSourceIsSkipped(t *testing.T) {
	h := newHarness(t)
	h.write("nosource.vcf", "BEGIN:VCARD\nVERSION:4.0\nFN:No Source\nEND:VCARD\n")
	code, out := h.run("", "vcard", "fetch", "nosource.vcf", "-n")
	h.expect(code, out, 0)
	contains(t, out, "Done.", "Skipped: 1", "No SOURCE property found")
}

func TestFetchNothingValid(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "vcard", "fetch", "missing.vcf")
	h.expect(code, out, 1)
	contains(t, out, "No valid .vcf files or URLs provided.", "Input path does not exist")
}

func TestFetchSourceMismatchKeepsTheFile(t *testing.T) {
	h := newHarness(t)
	r := newRemote(t, h)
	r.set("BEGIN:VCARD\nVERSION:4.0\nFN:Evil\nSOURCE:https://evil.example/e.vcf\nEND:VCARD\n")
	original := r.card("A")
	h.write("a.vcf", original)
	code, out := h.run("", "vcard", "fetch", "a.vcf")
	h.expect(code, out, 1)
	contains(t, out, "Failed: 1", "SOURCE mismatch")
	if h.read("a.vcf") != original {
		t.Error("the file must not be overwritten on a source mismatch")
	}
}

func TestFetchUnreachableSource(t *testing.T) {
	h := newHarness(t)
	h.env.Fetcher = &core.Fetcher{LookupHost: func(context.Context, string) ([]string, error) {
		return nil, fmt.Errorf("boom")
	}}
	h.write("a.vcf", "BEGIN:VCARD\nVERSION:4.0\nFN:A\nSOURCE:https://a.example/a.vcf\nEND:VCARD\n")
	code, out := h.run("", "vcard", "fetch", "a.vcf")
	h.expect(code, out, 1)
	contains(t, out, "Failed: 1", "Cannot resolve host 'a.example'")
	_ = strings.TrimSpace
}
