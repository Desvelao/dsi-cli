package strutil

import "testing"

func TestJSONDumps(t *testing.T) {
	got, err := JSONDumps(map[string]any{"a": []any{1, "x,y: z", nil}, "b": "<&>é "})
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\"a\": [1, \"x,y: z\", null], \"b\": \"<&>é \"}"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestParseInt(t *testing.T) {
	ok := map[string]int64{"1": 1, " 2 ": 2, "+3": 3, "-4": -4, "1_0": 10, "007": 7}
	for in, want := range ok {
		if got, valid := ParseInt(in); !valid || got != want {
			t.Errorf("ParseInt(%q) = %d, %v", in, got, valid)
		}
	}
	for _, in := range []string{"", "x", "1x", "_1", "1_", "1__0", "+", "1.5", "high", "99999999999999999999"} {
		if _, valid := ParseInt(in); valid {
			t.Errorf("ParseInt(%q) should be invalid", in)
		}
	}
}

func TestStripAndRepr(t *testing.T) {
	if Strip("\x1c a  ") != "a" {
		t.Error("Strip must drop Unicode whitespace")
	}
	for in, want := range map[string]string{
		`en"US`: `'en"US'`, "it's": `"it's"`, "a\nb": `'a\nb'`, `both'"`: `'both\'"'`, "\x00": `'\x00'`, "é": `'é'`,
	} {
		if got := Repr(in); got != want {
			t.Errorf("Repr(%q) = %s, want %s", in, got, want)
		}
	}
}
