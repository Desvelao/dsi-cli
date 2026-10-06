package vcard

import (
	"reflect"
	"testing"

	"github.com/Desvelao/dsipy/internal/testutil"
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
