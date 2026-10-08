package vcard

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/strutil"
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

func TestParseJSONMatchesGolden(t *testing.T) {
	for _, name := range testutil.GoldenNames(t, "vcards", ".parse.out") {
		p := ParseVCard(testutil.GoldenString(t, "vcards/"+name+".vcf"))
		got, err := strutil.JSONDumps(p)
		if err != nil {
			t.Fatal(err)
		}
		if want := testutil.GoldenString(t, "vcards/"+name+".parse.out"); got != want {
			t.Errorf("%s: JSON differs from json.dumps\n got %.300s\nwant %.300s", name, got, want)
		}
	}
}

func TestParseVCards(t *testing.T) {
	text := "junk\r\nBEGIN:VCARD\r\nVERSION:4.0\r\nFN:A\r\nEND:VCARD\r\nmid\r\nBEGIN:VCARD\r\nVERSION:4.0\r\nFN:B\r\nEND:VCARD\r\n"
	ps := ParseVCards(text)
	if len(ps) != 2 {
		t.Fatalf("got %d profiles", len(ps))
	}
	if ps[0].FN == nil || *ps[0].FN != "A" || ps[1].FN == nil || *ps[1].FN != "B" {
		t.Errorf("unexpected FNs: %v %v", ps[0].FN, ps[1].FN)
	}
	for _, p := range ps {
		if len(p.Errors) != 0 {
			t.Errorf("unexpected errors: %v", p.Errors)
		}
	}
	// No complete block: one profile, framing error reported.
	ps = ParseVCards("BEGIN:VCARD\r\nFN:X\r\n")
	if len(ps) != 1 || len(ps[0].Errors) == 0 {
		t.Errorf("expected single errored profile, got %v", ps)
	}
	// ParseVCard itself still reports multiple cards as an error.
	if p := ParseVCard(text); len(p.Errors) == 0 {
		t.Errorf("ParseVCard should flag multiple cards")
	}
}

func TestLogicalLinesQuotedPrintable(t *testing.T) {
	got := logicalLines("NOTE;ENCODING=QUOTED-PRINTABLE:abc=\r\ndef=\r\nghi\r\nFN:x\r\n")
	if len(got) != 2 {
		t.Fatalf("got %d lines: %#v", len(got), got)
	}
	if want := "NOTE;ENCODING=QUOTED-PRINTABLE:abc=\ndef=\nghi"; got[0].text != want {
		t.Errorf("got %q want %q", got[0].text, want)
	}
	if got[1].text != "FN:x" {
		t.Errorf("got %q", got[1].text)
	}
	// "=" without a quoted-printable marker is not a soft break.
	got = logicalLines("NOTE:abc=\r\nFN:x\r\n")
	if len(got) != 2 {
		t.Errorf("non-QP '=' joined lines: %#v", got)
	}
}
