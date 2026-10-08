package feeds

import "testing"

func TestMediaType(t *testing.T) {
	cases := []struct{ url, typ, medium string }{
		{"https://x/a.jpg", "image/jpeg", "image"},
		{"https://x/photo.JPG", "image/jpeg", "image"},
		{"https://x/a.jpeg", "image/jpeg", "image"},
		{"https://x/x.png?v=2", "image/png", "image"},
		{"https://x/x.gif#frag", "image/gif", "image"},
		{"https://x/x.webp?a=b#c", "image/webp", "image"},
		{"https://x/x.svg", "image/svg+xml", "image"},
		{"https://x/x.AVIF", "image/avif", "image"},
		{"https://x/x.mp4", "video/mp4", "video"},
		{"https://x/x.webm?t=1", "video/webm", "video"},
		{"https://x/noext", "application/octet-stream", "unknown"},
		{"https://x/x.txt", "application/octet-stream", "unknown"},
		{"https://x/dir.png/", "application/octet-stream", "unknown"},
		{"https://x/a b%.PNG?q=1", "image/png", "image"}, // url.Parse fails: raw fallback
	}
	for _, c := range cases {
		typ, medium := mediaType(c.url)
		if typ != c.typ || medium != c.medium {
			t.Errorf("mediaType(%q) = %q,%q; want %q,%q", c.url, typ, medium, c.typ, c.medium)
		}
	}
}
