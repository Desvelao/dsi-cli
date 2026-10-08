package core

import (
	"strings"
	"testing"
)

// Expected values below were checked against urllib.parse.

func TestSplitURLTable(t *testing.T) {
	for _, c := range []struct {
		in   string
		want SplitResult
	}{
		{"http://[::1]:8080/p?q#f", SplitResult{"http", "[::1]:8080", "/p", "q", "f"}},
		{"http://user:pw@Host.COM:99/a", SplitResult{"http", "user:pw@Host.COM:99", "/a", "", ""}},
		{"//h/p", SplitResult{"", "h", "/p", "", ""}},
		{"/p/q", SplitResult{"", "", "/p/q", "", ""}},
		{"HTTP://H/", SplitResult{"http", "H", "/", "", ""}},
		{"  \thttp://a\n/b", SplitResult{"http", "a", "/b", "", ""}},
		{"http://[v1.x]/", SplitResult{"http", "[v1.x]", "/", "", ""}},
		{"http://café/x", SplitResult{"http", "café", "/x", "", ""}},
		{"mailto:a@b?s=1", SplitResult{"mailto", "", "a@b", "s=1", ""}},
		{"a:b", SplitResult{"a", "", "b", "", ""}},
		{"1a:b", SplitResult{"", "", "1a:b", "", ""}},
		{"http://a/p;x?q", SplitResult{"http", "a", "/p;x", "q", ""}},
	} {
		got, err := SplitURL(c.in)
		if err != nil {
			t.Errorf("SplitURL(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("SplitURL(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSplitURLErrors(t *testing.T) {
	for in, want := range map[string]string{
		"http://[::1":       "Invalid IPv6 URL",
		"http://::1]/":      "Invalid IPv6 URL",
		"http://[1.2.3.4]/": "An IPv4 address cannot be in brackets",
		"http://[zz]/":      "'zz' does not appear to be an IPv4 or IPv6 address",
		"http://[v1]/":      "IPvFuture address is invalid",
		"http://a／b/":       "contains invalid characters under NFKC normalization",
		"http://a＠b/":       "contains invalid characters under NFKC normalization",
	} {
		_, err := SplitURL(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("SplitURL(%q) error = %v, want containing %q", in, err, want)
		}
	}
}

func TestSplitResultAccessors(t *testing.T) {
	r, err := SplitURL("http://us:pw@[::1]:8080/x")
	if err != nil {
		t.Fatal(err)
	}
	if r.Username() != "us" || r.Password() != "pw" || r.Hostname() != "::1" {
		t.Errorf("got user=%q pass=%q host=%q", r.Username(), r.Password(), r.Hostname())
	}
	if p, ok, err := r.Port(); p != 8080 || !ok || err != nil {
		t.Errorf("Port() = %d, %v, %v", p, ok, err)
	}
	r, _ = SplitURL("http://Example.COM/")
	if r.Hostname() != "example.com" || r.Username() != "" {
		t.Errorf("host=%q user=%q", r.Hostname(), r.Username())
	}
	if _, ok, err := r.Port(); ok || err != nil {
		t.Errorf("Port() without port = %v, %v", ok, err)
	}
	for _, in := range []string{"http://h:abc/", "http://h:99999/"} {
		r, _ := SplitURL(in)
		if _, _, err := r.Port(); err == nil {
			t.Errorf("Port() for %q: expected error", in)
		}
	}
}

func TestUnsplitTable(t *testing.T) {
	for _, c := range []struct {
		parts [5]string
		want  string
	}{
		{[5]string{"http", "", "//x", "", ""}, "http:////x"},
		{[5]string{"http", "", "/p", "", ""}, "http:///p"},
		{[5]string{"", "", "//p", "", ""}, "////p"},
		{[5]string{"file", "", "/p", "", ""}, "file:///p"},
		{[5]string{"mailto", "", "a", "", ""}, "mailto:a"},
		{[5]string{"", "h", "p", "q", "f"}, "//h/p?q#f"},
		{[5]string{"http", "h", "", "", ""}, "http://h"},
	} {
		p := c.parts
		if got := Unsplit(p[0], p[1], p[2], p[3], p[4]); got != c.want {
			t.Errorf("Unsplit(%q) = %q, want %q", p, got, c.want)
		}
	}
}

func TestSplitParams(t *testing.T) {
	for _, c := range []struct{ in, path, params string }{
		{"/a/b;p", "/a/b", "p"},
		{"/a;x/b", "/a;x/b", ""},
		{"b;p;q", "b", "p;q"},
		{"nosemi", "nosemi", ""},
		{"/a/b;", "/a/b", ""},
	} {
		if p, q := splitParams(c.in); p != c.path || q != c.params {
			t.Errorf("splitParams(%q) = %q, %q; want %q, %q", c.in, p, q, c.path, c.params)
		}
	}
}

func TestURLJoinTable(t *testing.T) {
	const base = "http://a/b/c/d;p?q"
	for _, c := range []struct{ base, ref, want string }{
		{base, "g", "http://a/b/c/g"},
		{base, "../../../g", "http://a/g"},
		{base, "//x/y", "http://x/y"},
		{base, "?y", "http://a/b/c/d;p?y"},
		{base, "#s", "http://a/b/c/d;p?q#s"},
		{base, "/g", "http://a/g"},
		{base, "..", "http://a/b/"},
		{base, ".", "http://a/b/c/"},
		{base, "g;x?y#s", "http://a/b/c/g;x?y#s"},
		{base, "../..", "http://a/"},
		{base, "./g/.", "http://a/b/c/g/"},
		{base, "g/../h", "http://a/b/c/h"},
		{base, "https://z/", "https://z/"},
		{base, "mailto:x@y", "mailto:x@y"},
		{"http://u:p@[::1]:80/a/b", "c", "http://u:p@[::1]:80/a/c"},
		{"http://a", "b", "http://a/b"},
		{"http://a/b/", "../../../x", "http://a/x"},
		{"http://a/b//c/d", "e//f", "http://a/b/c/e/f"},
		{"http://a/b", "/../x", "http://a/x"},
		{"", "x", "x"},
		{"http://a/b", "", "http://a/b"},
		{"http://a/b/c", "g:h", "g:h"},
		{"http://a/b/c", "HTTP://q/r", "http://q/r"},
		{"http://a/b/c", "http:g", "http://a/b/g"},
		{"http://a/b/c", "http:", "http://a/b/c"},
	} {
		got, err := URLJoin(c.base, c.ref)
		if err != nil {
			t.Errorf("URLJoin(%q, %q) error: %v", c.base, c.ref, err)
			continue
		}
		if got != c.want {
			t.Errorf("URLJoin(%q, %q) = %q, want %q", c.base, c.ref, got, c.want)
		}
	}
}

func TestURLJoinErrors(t *testing.T) {
	if _, err := URLJoin("http://[::1", "x"); err == nil {
		t.Error("expected error for malformed base")
	}
	if _, err := URLJoin("http://a/", "//[zz]/x"); err == nil {
		t.Error("expected error for malformed ref")
	}
	if _, err := URLJoin("http://a/", "//b＠c/"); err == nil {
		t.Error("expected NFKC netloc error for ref")
	}
}
