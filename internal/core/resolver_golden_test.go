package core

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/testutil"
)

func TestNormalizeURLGolden(t *testing.T) {
	var cases []struct {
		In    string
		Out   string
		Error string
	}
	testutil.GoldenJSON(t, "resolver/normalize_url.json", &cases)
	if len(cases) < 40 {
		t.Fatalf("few cases: %d", len(cases))
	}
	for _, c := range cases {
		got, err := NormalizeURL(c.In)
		if c.Error != "" {
			if err == nil || err.Error() != c.Error {
				t.Errorf("NormalizeURL(%q): err %v, want %q", c.In, err, c.Error)
			}
			continue
		}
		if err != nil || got != c.Out {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", c.In, got, err, c.Out)
		}
	}
}

func TestSafeFilenameGolden(t *testing.T) {
	var cases []struct {
		ContentDisposition string `json:"content_disposition"`
		URL                string
		Out                string
	}
	testutil.GoldenJSON(t, "resolver/safe_filename.json", &cases)
	for _, c := range cases {
		if got := SafeFilename(c.ContentDisposition, c.URL); got != c.Out {
			t.Errorf("SafeFilename(%q, %q) = %q, want %q", c.ContentDisposition, c.URL, got, c.Out)
		}
	}
}

func TestIsGlobalGolden(t *testing.T) {
	var cases []struct {
		IP     string
		Global bool
	}
	testutil.GoldenJSON(t, "resolver/is_global.json", &cases)
	if len(cases) < 60 {
		t.Fatalf("few cases: %d", len(cases))
	}
	for _, c := range cases {
		addr, err := netip.ParseAddr(strings.SplitN(c.IP, "%", 2)[0])
		if err != nil {
			t.Fatal(err)
		}
		if got := IsGlobal(addr); got != c.Global {
			t.Errorf("IsGlobal(%s) = %v, want %v", c.IP, got, c.Global)
		}
	}
}

func TestURLJoinGolden(t *testing.T) {
	var cases []struct{ Base, Location, Out string }
	testutil.GoldenJSON(t, "resolver/urljoin.json", &cases)
	for _, c := range cases {
		got, err := URLJoin(c.Base, c.Location)
		if err != nil || got != c.Out {
			t.Errorf("URLJoin(%q, %q) = %q, %v; want %q", c.Base, c.Location, got, err, c.Out)
		}
	}
}

func TestValidateURLGolden(t *testing.T) {
	var cases []struct {
		URL       string
		AllowHTTP bool `json:"allow_http"`
		Host      string
		Port      int
		Error     string
	}
	testutil.GoldenJSON(t, "resolver/validate_url.json", &cases)
	for _, c := range cases {
		host, port, err := validateURL(c.URL, c.AllowHTTP)
		if c.Error != "" {
			if err == nil || err.Error() != c.Error {
				t.Errorf("validateURL(%q, %v): err %v, want %q", c.URL, c.AllowHTTP, err, c.Error)
			}
			continue
		}
		if err != nil || host != c.Host || port != c.Port {
			t.Errorf("validateURL(%q, %v) = %q %d %v; want %q %d", c.URL, c.AllowHTTP, host, port, err, c.Host, c.Port)
		}
	}
}

func TestUTF8ErrorsGolden(t *testing.T) {
	var cases []struct {
		Hex   string
		Ok    bool
		Error string
	}
	testutil.GoldenJSON(t, "resolver/utf8_errors.json", &cases)
	for _, c := range cases {
		body := make([]byte, len(c.Hex)/2)
		for i := range body {
			var b byte
			for _, h := range c.Hex[2*i : 2*i+2] {
				b <<= 4
				switch {
				case h >= 'a':
					b |= byte(h-'a') + 10
				default:
					b |= byte(h - '0')
				}
			}
			body[i] = b
		}
		msg, bad := Utf8DecodeError(body)
		if c.Ok == bad || (bad && msg != c.Error) {
			t.Errorf("%s: got (%q, %v), want ok=%v %q", c.Hex, msg, bad, c.Ok, c.Error)
		}
	}
}

func TestSourceMatches(t *testing.T) {
	s := func(v string) *string { return &v }
	if !SourceMatches("https://e.com/%7Ea/./b%2f", s("https://e.com/~a/b%2F")) {
		t.Error("equivalent encodings must match")
	}
	for _, bad := range []string{"https://e.com:abc/a", "https://e.com:99999/a"} {
		if SourceMatches("https://e.com/a", s(bad)) {
			t.Errorf("invalid port %q must not match", bad)
		}
	}
	if !SourceMatches("https://example.com/alice.vcf", s("https://EXAMPLE.com:443/alice.vcf")) {
		t.Error("case and default port must be ignored")
	}
	if SourceMatches("https://alice.example/dsi.vcf", s("https://evil.example/bob.vcf")) || SourceMatches("https://example.com/a", nil) {
		t.Error("different or missing source must not match")
	}
}

func TestNormalizeURLKeepsUserinfo(t *testing.T) {
	got, err := NormalizeURL("HTTPS://User:pw@Example.com:443/a.vcf#f")
	if err != nil || got != "https://User:pw@example.com/a.vcf" {
		t.Errorf("got %q, %v", got, err)
	}
	s := func(v string) *string { return &v }
	for _, src := range []string{"https://user@example.com/a.vcf", "https://u:p@example.com/a.vcf", "https://@example.com/a.vcf"} {
		if SourceMatches("https://example.com/a.vcf", s(src)) || SourceMatches(src, s("https://example.com/a.vcf")) {
			t.Errorf("userinfo URL %q must not match the bare URL", src)
		}
	}
	if !SourceMatches("https://user@Example.com/a.vcf", s("https://user@example.com:443/a.vcf")) {
		t.Error("identical userinfo must still match")
	}
	if SourceMatches("https://user@example.com/a.vcf", s("https://other@example.com/a.vcf")) {
		t.Error("different userinfo must not match")
	}
}
