package vcard

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/pyutil"
	"github.com/Desvelao/dsi-cli/internal/testutil"
)

func decode(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func TestParseVCardGolden(t *testing.T) {
	names := testutil.GoldenNames(t, "vcards", ".parse.json")
	if len(names) < 25 {
		t.Fatalf("expected many golden cases, got %d", len(names))
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			profile := ParseVCard(testutil.GoldenString(t, "vcards/"+name+".vcf"))
			got, err := json.Marshal(profile)
			if err != nil {
				t.Fatal(err)
			}
			want := decode(t, testutil.Golden(t, "vcards/"+name+".parse.json"))
			if g := decode(t, got); !reflect.DeepEqual(g, want) {
				gj, _ := json.MarshalIndent(g, "", " ")
				wj, _ := json.MarshalIndent(want, "", " ")
				t.Errorf("parse mismatch\n--- got\n%s\n--- want\n%s", gj, wj)
			}
		})
	}
}

func TestParseKeepsRaw(t *testing.T) {
	text := "BEGIN:VCARD\r\nVERSION:4.0\r\nEND:VCARD\r\n"
	if p := ParseVCard(text); p.Raw != text {
		t.Errorf("raw not preserved: %q", p.Raw)
	}
}

func TestParseMultipleCards(t *testing.T) {
	p := ParseVCard("BEGIN:VCARD\nFN:A\nEND:VCARD\nBEGIN:VCARD\nFN:B\nEND:VCARD\n")
	if len(p.Errors) != 1 || p.Errors[0] != "multiple vCards found; only the first is parsed" {
		t.Errorf("errors: %v", p.Errors)
	}
	if *p.FN != "A" {
		t.Errorf("fn: %q", *p.FN)
	}
}

func TestParseBareSnippetHasNoFramingErrors(t *testing.T) {
	p := ParseVCard("FN:x\n")
	if len(p.Errors) != 0 {
		t.Errorf("errors: %v", p.Errors)
	}
}

func TestParseQuotedPrintableSoftBreak(t *testing.T) {
	p := ParseVCard("BEGIN:VCARD\nNOTE;ENCODING=QUOTED-PRINTABLE:abc=\ndef\nEND:VCARD\n")
	if len(p.Errors) != 0 {
		t.Fatalf("errors: %v", p.Errors)
	}
	if *p.Note != "abc=\ndef" {
		t.Errorf("note: %q", *p.Note)
	}
}

func TestParseJSONMatchesPythonDumps(t *testing.T) {
	for _, name := range testutil.GoldenNames(t, "vcards", ".parse.pyjson") {
		p := ParseVCard(testutil.GoldenString(t, "vcards/"+name+".vcf"))
		got, err := pyutil.JSONDumps(p)
		if err != nil {
			t.Fatal(err)
		}
		if want := testutil.GoldenString(t, "vcards/"+name+".parse.pyjson"); got != want {
			t.Errorf("%s: JSON differs from json.dumps\n got %.300s\nwant %.300s", name, got, want)
		}
	}
}
