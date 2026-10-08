package vcard

import (
	"strings"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/model"
)

func TestBuildVCardFromRawLinesFiltersFramingCaseInsensitively(t *testing.T) {
	text := "begin:vcard\r\nversion:3.0\r\nitem1.VERSION:3.0\r\nFN:A\r\nend:vcard\r\n"
	p := ParseVCard(text)
	out := BuildVCardFromRawLines(p)
	if n := strings.Count(strings.ToUpper(out), "VERSION"); n != 1 {
		t.Fatalf("want 1 VERSION line, got %d:\n%s", n, out)
	}
	want := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:A\r\nEND:VCARD\r\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
	// Lines without a parsed Name fall back to the line text.
	q := &model.Profile{RawLines: []model.RawLine{{Line: "g.version:3.0"}, {Line: "FN:B"}}}
	if got := BuildVCardFromRawLines(q); got != "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:B\r\nEND:VCARD\r\n" {
		t.Fatalf("got %q", got)
	}
}
