package vcard

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Desvelao/dsipy/internal/model"
	"github.com/Desvelao/dsipy/internal/testutil"
)

const (
	alice = "MCowBQYDK2VwAyEAOAiOTCroL1xFxoCKYaZJDTxhLOHaI1cURm/HSPvEy7s="
	bob   = "MCowBQYDK2VwAyEA3XVgQP3VFF4r+YMtJk3QgOSz5zAWvfZXS0zYfqppf14="
)

func crlf(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

// fieldsFromJSON converts the golden build_content args (Python kwargs).
func fieldsFromJSON(t *testing.T, args map[string]any) Fields {
	t.Helper()
	s := func(k string) string {
		v, _ := args[k].(string)
		return v
	}
	f := Fields{
		FN: s("fn"), Nickname: s("nickname"), Lang: s("lang"), Email: s("email"), Kind: s("kind"),
		N: Text(s("n")), Gender: Text(s("gender")), Categories: Text(s("categories")), Adr: Text(s("adr")),
		Bday: s("bday"), Anniversary: s("anniversary"), Tel: s("tel"), Impp: s("impp"),
		Photo: s("photo"), Note: s("note"), URL: s("url"), Source: s("source"),
	}
	if ca, ok := args["custom_attributes"].([]any); ok {
		for _, kv := range ca {
			pair := kv.([]any)
			f.CustomAttributes = append(f.CustomAttributes, Attribute{pair[0].(string), pair[1].(string)})
		}
	}
	if keys, ok := args["keys"].([]any); ok {
		for _, k := range keys {
			m := k.(map[string]any)
			spec := KeySpec{}
			spec.Alg, _ = m["alg"].(string)
			spec.KeyB64, _ = m["key_b64"].(string)
			if p, ok := m["pref"].(float64); ok {
				n := int64(p)
				spec.Pref = &n
			}
			f.Keys = append(f.Keys, spec)
		}
	}
	return f
}

func TestBuildContentGolden(t *testing.T) {
	var cases map[string]struct {
		Args map[string]any
		Out  string
	}
	testutil.GoldenJSON(t, "vcards/build_content.json", &cases)
	n := 0
	for name, c := range cases {
		if name == "__errors__" {
			continue
		}
		n++
		t.Run(name, func(t *testing.T) {
			got, err := BuildContent(fieldsFromJSON(t, c.Args))
			if err != nil {
				t.Fatal(err)
			}
			if got != c.Out {
				t.Errorf("mismatch\n got %q\nwant %q", got, c.Out)
			}
		})
	}
	if n < 3 {
		t.Fatalf("expected golden build cases, got %d", n)
	}
}

func TestBuildContentErrorsGolden(t *testing.T) {
	var cases map[string]struct{ Error *string }
	testutil.GoldenJSON(t, "vcards/build_content.json", &cases)
	inputs := map[string]Fields{
		"break_in_tel":    {Tel: "1\nFN:evil"},
		"break_in_url":    {URL: "https://e.com\r\nX-EVIL:1"},
		"break_in_custom": {CustomAttributes: []Attribute{{"X-A", "v\r\nFN:evil"}}},
		"key_missing_alg": {Keys: []KeySpec{{KeyB64: alice}}},
		"key_missing_b64": {Keys: []KeySpec{{Alg: "ed25519"}}},
		"quote_in_lang":   {Lang: `en"US`, Note: "x"},
	}
	var errs map[string]struct{ Error *string }
	raw := testutil.Golden(t, "vcards/build_content.json")
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(all["__errors__"], &errs); err != nil {
		t.Fatal(err)
	}
	for name, in := range inputs {
		_, err := BuildContent(in)
		want := errs[name].Error
		if want == nil || err == nil || err.Error() != *want {
			t.Errorf("%s: got err %v, want %v", name, err, want)
		}
	}
}

func TestRebuiltGolden(t *testing.T) {
	for _, name := range testutil.GoldenNames(t, "vcards", ".rebuilt.vcf") {
		t.Run(name, func(t *testing.T) {
			p := ParseVCard(testutil.GoldenString(t, "vcards/"+name+".vcf"))
			if got, want := BuildVCardFromRawLines(p), testutil.GoldenString(t, "vcards/"+name+".rebuilt.vcf"); got != want {
				t.Errorf("got %q want %q", got, want)
			}
		})
	}
}

func TestFormatPropertyGolden(t *testing.T) {
	var cases []struct {
		Name   string
		Params [][2]string
		Value  string
		Group  *string
		Out    string
	}
	testutil.GoldenJSON(t, "escaping/format_property.json", &cases)
	for _, c := range cases {
		var params []model.Param
		for _, p := range c.Params {
			params = append(params, model.Param{Name: p[0], Value: p[1]})
		}
		got, err := FormatProperty(c.Name, params, c.Value, c.Group)
		if err != nil || got != c.Out {
			t.Errorf("FormatProperty(%s) = %q, %v; want %q", c.Name, got, err, c.Out)
		}
	}
}

func TestFormatParamValue(t *testing.T) {
	if v, _ := FormatParamValue("plain"); v != "plain" {
		t.Error(v)
	}
	if v, _ := FormatParamValue("a;b"); v != `"a;b"` {
		t.Error(v)
	}
	for _, bad := range []string{`say "hi"`, "a\nb"} {
		if _, err := FormatParamValue(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

// --- ported from tests/test_parser.py and tests/test_vcard_serializer.py ---

func example() string {
	sig := strings.Repeat("0c", 64)
	return crlf(
		"BEGIN:VCARD", "VERSION:4.0", "FN:Alice Example",
		"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:"+alice,
		"REVKEY;TYPE=public;ALG=ed25519;REASON=rotated;DATE=20260221T110230Z;ENCODING=b:"+bob,
		"SOURCE:https://example.com/alice.vcf",
		"X-DSI-VERSION;FEATURES=tags,endorse:00",
		"X-FEED:https://example.com/alice/feed.xml",
		"X-FEED;LANGUAGE=es-ES;TAGS=nature,travel:https://example.com/alice/es.xml",
		"X-SOCIAL;PLATFORM=github:alice",
		"X-SOCIAL;PLATFORM=activitypub:https://social.example/@alice",
		"X-ENDORSE;SIG="+sig+";DATE=20260210T120000Z;CONFIDENCE=high;ENCODING=b:"+bob,
		"X-ACME-UNKNOWN;FOO=bar:keep me",
		"END:VCARD",
	)
}

func TestParseDSIProperties(t *testing.T) {
	p := ParseVCard(example())
	if model.Str(p.Version) != "4.0" || model.Str(p.FN) != "Alice Example" || model.Str(p.Source) != "https://example.com/alice.vcf" {
		t.Errorf("basic fields: %+v", p)
	}
	if len(p.Keys) != 1 || p.Keys[0].Alg != "ed25519" || p.Keys[0].KeyB64 != alice || *p.Keys[0].Pref != 1 {
		t.Errorf("keys: %+v", p.Keys)
	}
	r := p.Revocations[0]
	if r.KeyB64 != bob || *r.Reason != "rotated" || *r.Date != "20260221T110230Z" {
		t.Errorf("revocation: %+v", r)
	}
	e := p.Endorsements[0]
	if e.EndorseeKeyB64 != bob || e.SignatureHex != strings.Repeat("0c", 64) || *e.Date != "20260210T120000Z" || *e.Confidence != "high" {
		t.Errorf("endorsement: %+v", e)
	}
	if p.DsiVersion.Revision != "00" || strings.Join(p.DsiVersion.Features, "|") != "tags|endorse" {
		t.Errorf("dsi version: %+v", p.DsiVersion)
	}
	if len(p.Social) != 2 || p.Social[1].Platform != "activitypub" || len(p.Errors) != 0 {
		t.Errorf("social/errors: %+v %v", p.Social, p.Errors)
	}
	if len(p.Feeds) != 2 || p.Feeds[0].Language != "" || p.Feeds[1].Language != "es-ES" || p.Feeds[1].Tags != "nature,travel" {
		t.Errorf("feeds: %+v", p.Feeds)
	}
}

func TestParseCaseInsensitiveNames(t *testing.T) {
	p := ParseVCard(crlf("BEGIN:VCARD", "key;type=public;alg=ED25519;pref=2:"+alice, "END:VCARD"))
	if p.Keys[0].Alg != "ed25519" || *p.Keys[0].Pref != 2 {
		t.Errorf("%+v", p.Keys[0])
	}
}

func TestParseKeyWithoutParams(t *testing.T) {
	p := ParseVCard(crlf("BEGIN:VCARD", "KEY:"+alice, "END:VCARD"))
	if p.Keys[0].KeyB64 != alice || p.Keys[0].Alg != "" {
		t.Errorf("%+v", p.Keys[0])
	}
}

func TestParseFoldedLines(t *testing.T) {
	p := ParseVCard("BEGIN:VCARD\r\nFN:Alice\r\nNOTE:a very long\r\n  folded note\r\n" +
		"KEY;ALG=ed25519:" + alice[:20] + "\r\n " + alice[20:] + "\r\nEND:VCARD\r\n")
	if model.Str(p.Note) != "a very long folded note" || p.Keys[0].KeyB64 != alice {
		t.Errorf("note %q key %q", model.Str(p.Note), p.Keys[0].KeyB64)
	}
}

func TestParseTextUnescapeAndStructuredRaw(t *testing.T) {
	p := ParseVCard(crlf("BEGIN:VCARD", `FN:Alice\, Example\; Jr`, `NOTE:one\ntwo \\ end`, `N:Example;Alice;;;`, "END:VCARD"))
	if model.Str(p.FN) != "Alice, Example; Jr" || model.Str(p.Note) != "one\ntwo \\ end" || model.Str(p.N) != "Example;Alice;;;" {
		t.Errorf("%q %q %q", model.Str(p.FN), model.Str(p.Note), model.Str(p.N))
	}
}

func TestParseValueWithColonAndQuotedParams(t *testing.T) {
	p := ParseVCard(crlf("BEGIN:VCARD", "NOTE;LANGUAGE=en-US:hello: world", `X-FEED;LANGUAGE="en;US":https://e.com/a`, "END:VCARD"))
	if model.Str(p.Note) != "hello: world" || p.Feeds[0].Language != "en;US" || p.Feeds[0].URL != "https://e.com/a" {
		t.Errorf("%q %+v", model.Str(p.Note), p.Feeds)
	}
}

func TestParseInvalidPrefAndMalformedAreNotFatal(t *testing.T) {
	p := ParseVCard(crlf("BEGIN:VCARD", "KEY;PREF=high:"+alice, "END:VCARD"))
	if p.Keys[0].Pref != nil || len(p.Errors) != 1 {
		t.Errorf("%+v %v", p.Keys[0], p.Errors)
	}
	p = ParseVCard(crlf("BEGIN:VCARD", "FN:Alice", "garbage line", "END:VCARD"))
	if model.Str(p.FN) != "Alice" || len(p.Errors) != 1 {
		t.Errorf("%v", p.Errors)
	}
}

func TestParseRoundTripRebuild(t *testing.T) {
	ex := example()
	if got := BuildVCardFromRawLines(ParseVCard(ex)); got != ex {
		t.Errorf("rebuild differs:\n%q\n%q", got, ex)
	}
}

func TestParseFraming(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"valid", example(), nil},
		{"missing end", crlf("BEGIN:VCARD", "VERSION:4.0", "FN:A"), []string{"missing END:VCARD"}},
		{"missing begin", crlf("VERSION:4.0", "FN:A", "END:VCARD"), []string{"missing BEGIN:VCARD"}},
		{"unframed", crlf("FN:A"), nil},
		{"unterminated then second", crlf("BEGIN:VCARD", "VERSION:4.0", "FN:First", "BEGIN:VCARD", "FN:Second"),
			[]string{"multiple vCards found; only the first is parsed", "missing END:VCARD"}},
	}
	for _, c := range cases {
		p := ParseVCard(c.text)
		if strings.Join(p.Errors, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: errors %v, want %v", c.name, p.Errors, c.want)
		}
	}
}

func TestBuildContentEscapesAndRoundTrips(t *testing.T) {
	text, err := BuildContent(Fields{FN: "Alice, Example; Jr", Note: "line1\nline2", Source: "https://e.com/a.vcf"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(text, "END:VCARD\r\n") || strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Errorf("line endings: %q", text)
	}
	if !strings.Contains(text, `FN:Alice\, Example\; Jr`) || !strings.Contains(text, `line1\nline2`) {
		t.Errorf("escaping: %q", text)
	}
	p := ParseVCard(text)
	if model.Str(p.FN) != "Alice, Example; Jr" || model.Str(p.Note) != "line1\nline2" || len(p.Errors) != 0 {
		t.Errorf("round trip: %q %q %v", model.Str(p.FN), model.Str(p.Note), p.Errors)
	}
}

func TestBuildContentCannotInject(t *testing.T) {
	text, err := BuildContent(Fields{FN: "Alice\r\nX-EVIL:1"})
	if err != nil || strings.Contains(text, "\r\nX-EVIL") {
		t.Errorf("injection: %q %v", text, err)
	}
	text, _ = BuildContent(Fields{N: Text("Doe\nFN:evil"), Categories: Text("a\r\nFN:evil")})
	p := ParseVCard(text)
	if p.FN != nil || len(p.Errors) != 0 {
		t.Errorf("fn %v errors %v", p.FN, p.Errors)
	}
}

func TestBuildContentFieldForms(t *testing.T) {
	get := func(f Fields, prefix string) string {
		text, err := BuildContent(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n ", ""), "\r\n") {
			if strings.HasPrefix(l, prefix) {
				return l
			}
		}
		return ""
	}
	if got := get(Fields{Adr: Text(";;Street 1;City")}, "ADR:"); got != "ADR:;;Street 1;City;;;" {
		t.Error(got)
	}
	if got := get(Fields{Adr: List("", "", "1; Main, St", "City")}, "ADR:"); got != `ADR:;;1\; Main\, St;City;;;` {
		t.Error(got)
	}
	if got := get(Fields{Categories: List("a,b", "c;d", "e")}, "CATEGORIES:"); got != `CATEGORIES:a\,b,c\;d,e` {
		t.Error(got)
	}
	if got := get(Fields{Gender: Text("O;non, binary")}, "GENDER:"); got != `GENDER:O;non\, binary` {
		t.Error(got)
	}
	if got := get(Fields{Tel: "tel:+1;ext=5"}, "TEL:"); got != "TEL:tel:+1;ext=5" {
		t.Error(got)
	}
	if got := get(Fields{Keys: []KeySpec{{Alg: "ED25519", KeyB64: alice}}}, "KEY"); got != "KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:"+alice {
		t.Error(got)
	}
	if got := get(Fields{Lang: "es-ES", Note: "hola"}, "NOTE"); got != "NOTE;LANGUAGE=es-ES:hola" {
		t.Error(got)
	}
	if got := get(Fields{Note: "hi"}, "NOTE"); got != "NOTE;LANGUAGE=en-US:hi" {
		t.Error(got)
	}
}

func TestLongKeyFoldedAndParsesBack(t *testing.T) {
	long := strings.Repeat("A", 300)
	content, err := BuildContent(Fields{FN: "X", Note: strings.Repeat("ñ", 100), Keys: []KeySpec{{Alg: "ed25519", KeyB64: long, Encoding: "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, phys := range strings.Split(content, "\r\n") {
		if len(phys) > 75 {
			t.Errorf("physical line too long (%d): %q", len(phys), phys)
		}
	}
	p := ParseVCard(content)
	if p.Keys[0].KeyB64 != long || model.Str(p.Note) != strings.Repeat("ñ", 100) {
		t.Error("folded values did not round trip")
	}
}
