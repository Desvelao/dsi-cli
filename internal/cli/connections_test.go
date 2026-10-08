package cli

import (
	"strings"
	"testing"
)

const feedCard = "BEGIN:VCARD\nVERSION:4.0\nFN:Alice\nSOURCE:https://alice.example/dsi.vcf\nX-FEED:https://alice.example/f.rss\nEND:VCARD\n"

func TestConnectionsFeedStdout(t *testing.T) {
	h := newHarness(t)
	h.write("a.vcf", feedCard)
	code, out := h.run("", "connections", "feed", "a.vcf")
	h.expect(code, out, 0)
	contains(t, out, "<opml", "https://alice.example/f.rss", "Alice")
}

func TestConnectionsFeedOutputFile(t *testing.T) {
	h := newHarness(t)
	h.write("a.vcf", feedCard)
	code, out := h.run("", "connections", "feed", "a.vcf", "-o", "sub/out.opml")
	h.expect(code, out, 0)
	contains(t, out, "✅ OPML file generated: sub/out.opml")
	contains(t, h.read("sub/out.opml"), "https://alice.example/f.rss")
}

func TestConnectionsFeedNoFiles(t *testing.T) {
	h := newHarness(t)
	h.write("empty/readme.txt", "x")
	code, out := h.run("", "connections", "feed", "empty")
	h.expect(code, out, 1)
	contains(t, out, "No vCard files found")
}

func TestConnectionsFeedWarnings(t *testing.T) {
	h := newHarness(t)
	h.write("a.vcf", feedCard)
	h.write("bad.vcf", "BEGIN:VCARD\nFN:\xff\nEND:VCARD\n")
	code, out := h.run("", "connections", "feed", "a.vcf", "bad.vcf", "https://example.com/x.vcf")
	h.expect(code, out, 0)
	contains(t, out, "Ignoring URL input", "https://example.com/x.vcf", "Skipping ")

	code, out = h.run("", "connections", "feed", "https://example.com/x.vcf")
	h.expect(code, out, 1)
	contains(t, out, "Ignoring URL input", "No vCard files found")
}

func TestConnectionsFeedTitle(t *testing.T) {
	h := newHarness(t)
	h.write("a.vcf", feedCard)
	code, out := h.run("", "connections", "feed", "a.vcf")
	h.expect(code, out, 0)
	contains(t, out, `<?xml version="1.0" encoding="UTF-8"?>`, "<head><title>DSI connections</title></head>")
	code, out = h.run("", "connections", "feed", "a.vcf", "--title", "Me & <Friends>")
	h.expect(code, out, 0)
	contains(t, out, "<head><title>Me &amp; &lt;Friends&gt;</title></head>")
}

func TestConnectionsFeedDedupesAcrossCards(t *testing.T) {
	h := newHarness(t)
	h.write("a.vcf", feedCard)
	h.write("b.vcf", strings.Replace(feedCard, "FN:Alice", "FN:Alice Again", 1))
	code, out := h.run("", "connections", "feed", "a.vcf", "b.vcf")
	h.expect(code, out, 0)
	if n := strings.Count(out, "<outline "); n != 1 {
		t.Errorf("want 1 outline, got %d: %s", n, out)
	}
}
