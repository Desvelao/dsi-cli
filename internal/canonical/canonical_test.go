package canonical

import (
	"strings"
	"testing"

	"github.com/Desvelao/dsipy/internal/testutil"
	"github.com/Desvelao/dsipy/internal/vcard"
)

func crlf(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

func normalize(t *testing.T, lines ...string) string {
	t.Helper()
	out, err := NormalizeVCard(vcard.ParseVCard(crlf(lines...)))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNormalizeGolden(t *testing.T) {
	ok, bad := 0, 0
	for _, name := range testutil.GoldenNames(t, "vcards", ".parse.json") {
		p := vcard.ParseVCard(testutil.GoldenString(t, "vcards/"+name+".vcf"))
		got, err := NormalizeVCard(p)
		wantErr := testutil.GoldenNames(t, "vcards", ".normalize_error.txt")
		isErr := false
		for _, n := range wantErr {
			if n == name {
				isErr = true
			}
		}
		if isErr {
			bad++
			want := strings.TrimSuffix(testutil.GoldenString(t, "vcards/"+name+".normalize_error.txt"), "\n")
			if err == nil || err.Error() != want {
				t.Errorf("%s: error %v, want %q", name, err, want)
			}
			continue
		}
		ok++
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if want := testutil.GoldenString(t, "vcards/"+name+".normalized.vcf"); got != want {
			t.Errorf("%s: mismatch\n got %q\nwant %q", name, got, want)
		}
	}
	if ok < 15 || bad < 3 {
		t.Fatalf("expected many cases, ok=%d bad=%d", ok, bad)
	}
}

func TestNormalizeOrdersPropertiesAndUsesCRLF(t *testing.T) {
	got := normalize(t, "BEGIN:VCARD", "VERSION:4.0", "SOURCE:https://a.example/x.vcf", "X-ZZZ:last", "FN:Alice", "X-AAA:first-unknown", "END:VCARD")
	want := crlf("BEGIN:VCARD", "VERSION:4.0", "FN:Alice", "SOURCE:https://a.example/x.vcf", "X-AAA:first-unknown", "X-ZZZ:last", "END:VCARD")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestNormalizeUppercasesNamesAndSortsParams(t *testing.T) {
	const k = "MCowBQYDK2VwAyEAOAiOTCroL1xFxoCKYaZJDTxhLOHaI1cURm/HSPvEy7s="
	out := normalize(t, "BEGIN:VCARD", "VERSION:4.0", "fn:Alice", "key;pref=1;alg=ed25519;type=public;encoding=b:"+k, "END:VCARD")
	if !strings.Contains(out, "KEY;ALG=ed25519;ENCODING=b;PREF=1;TYPE=public:"+k) || !strings.Contains(out, "FN:Alice\r\n") {
		t.Errorf("%q", out)
	}
}

func TestNormalizeUnfoldsAndIsIdempotent(t *testing.T) {
	if out := normalize(t, "BEGIN:VCARD", "VERSION:4.0", "NOTE:hello", " world", "END:VCARD"); !strings.Contains(out, "NOTE:helloworld\r\n") {
		t.Errorf("%q", out)
	}
	a := normalize(t, "BEGIN:VCARD", "VERSION:4.0", "FN:A", "NOTE:n", "END:VCARD")
	b := normalize(t, "BEGIN:VCARD", "VERSION:4.0", "NOTE:n", "FN:A", "END:VCARD")
	if a != b {
		t.Errorf("order dependent: %q vs %q", a, b)
	}
	again, err := NormalizeVCard(vcard.ParseVCard(a))
	if err != nil || again != a {
		t.Errorf("not idempotent: %q %v", again, err)
	}
}

func TestKeysSortedByValueOthersKeepOrder(t *testing.T) {
	out := normalize(t, "BEGIN:VCARD", "VERSION:4.0", "KEY;ENCODING=b:zzz", "KEY;ENCODING=b:aaa", "EMAIL:b@example.com", "EMAIL:a@example.com", "END:VCARD")
	idx := func(s string) int { return strings.Index(out, s) }
	if idx("KEY;ENCODING=b:aaa") > idx("KEY;ENCODING=b:zzz") || idx("EMAIL:b@") > idx("EMAIL:a@") {
		t.Errorf("%q", out)
	}
}

func TestMalformedRejected(t *testing.T) {
	if _, err := NormalizeVCard(vcard.ParseVCard(crlf("BEGIN:VCARD", "VERSION:4.0", "no colon here", "END:VCARD"))); err == nil {
		t.Error("expected error")
	}
}

func TestCanonicalStrings(t *testing.T) {
	if string(EndorsementString("QUJD")) != "endorse:QUJD" || string(FeedString("d", "t", "x")) != "d\nt\nx" {
		t.Error("canonical strings")
	}
}
