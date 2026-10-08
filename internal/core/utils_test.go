package core

import (
	"testing"

	"github.com/Desvelao/dsi-cli/internal/testutil"
)

func TestSlugifyGolden(t *testing.T) {
	var cases []struct{ In, Out string }
	testutil.GoldenJSON(t, "escaping/slugify.json", &cases)
	for _, c := range cases {
		if got := Slugify(c.In); got != c.Out {
			t.Errorf("Slugify(%q) = %q, want %q", c.In, got, c.Out)
		}
	}
}

func TestSlugifyTable(t *testing.T) {
	for in, want := range map[string]string{
		"Hello World":      "hello-world",
		"Árbol Ñandú Über": "arbol-nandu-uber",
		"  --a  b--  ":     "a-b",
		"a_b!!c":           "a-b-c",
		"":                 "",
		"日本語":              "",
		"../../etc/passwd": "etc-passwd",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
