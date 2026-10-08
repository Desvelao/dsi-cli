package vcard

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Desvelao/dsi-cli/internal/testutil"
)

func TestEscapeTextGolden(t *testing.T) {
	var cases []struct{ In, Out string }
	testutil.GoldenJSON(t, "escaping/escape_text.json", &cases)
	for _, c := range cases {
		if got := EscapeText(c.In); got != c.Out {
			t.Errorf("EscapeText(%q) = %q, want %q", c.In, got, c.Out)
		}
	}
}

func TestUnescapeTextGolden(t *testing.T) {
	var cases []struct{ In, Out string }
	testutil.GoldenJSON(t, "escaping/unescape_text.json", &cases)
	for _, c := range cases {
		if got := UnescapeText(c.In); got != c.Out {
			t.Errorf("UnescapeText(%q) = %q, want %q", c.In, got, c.Out)
		}
	}
}

func TestSplitStructuredGolden(t *testing.T) {
	var cases []struct {
		In  string
		Sep string
		Out []string
	}
	testutil.GoldenJSON(t, "escaping/split_structured.json", &cases)
	for _, c := range cases {
		if got := SplitStructured(c.In, c.Sep); !reflect.DeepEqual(got, c.Out) {
			t.Errorf("SplitStructured(%q,%q) = %q, want %q", c.In, c.Sep, got, c.Out)
		}
	}
}

func TestFoldLineGolden(t *testing.T) {
	var cases []struct{ In, Out string }
	testutil.GoldenJSON(t, "escaping/fold_line.json", &cases)
	for _, c := range cases {
		if got := FoldLine(c.In, 75); got != c.Out {
			t.Errorf("FoldLine(%.20q…) mismatch:\n got %q\nwant %q", c.In, got, c.Out)
		}
	}
}

func TestJoinStructured(t *testing.T) {
	cases := []struct{ got, want string }{
		{JoinStructuredText("Doe;John", ";", 5), "Doe;John;;;"},
		{JoinStructuredText("a,b;c", ";", -1), `a\,b;c`},
		{JoinStructuredText("a;b,c", ",", -1), `a\;b,c`},
		{JoinStructuredList([]string{"Doe; Jr", "John, A", "", "Dr", ""}, ";", 5), `Doe\; Jr;John\, A;;Dr;`},
		{JoinStructuredList([]string{"x"}, ",", 3), "x,,"},
		{JoinStructuredText("l1\r\nl2\\", ";", -1), `l1\nl2\\`},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q want %q", i, c.got, c.want)
		}
	}
}

func TestFoldLineMultiByteAtLimit(t *testing.T) {
	// 3-byte runes: limit 6 fits exactly two per chunk.
	got := FoldLine("€€€€", 6)
	if want := "€€\r\n €\r\n €"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	for i, chunk := range strings.Split(got, "\r\n") {
		if len(chunk) > 6 || !utf8.ValidString(chunk) {
			t.Errorf("chunk %d %q invalid", i, chunk)
		}
		if i > 0 && chunk[0] != ' ' {
			t.Errorf("chunk %d lacks leading space", i)
		}
	}
	if un := strings.ReplaceAll(got, "\r\n ", ""); un != "€€€€" {
		t.Errorf("unfold mismatch %q", un)
	}
	// Exactly at the limit: untouched.
	if s := "€€"; FoldLine(s, 6) != s {
		t.Errorf("line at limit was folded")
	}
	// One byte over with a multi-byte rune: rune is never split.
	if got := FoldLine("aaaaa€", 6); got != "aaaaa\r\n €" {
		t.Errorf("got %q", got)
	}
}
