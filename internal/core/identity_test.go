package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/model"
	"github.com/Desvelao/dsi-cli/internal/testutil"
)

const (
	keyAlice = "MCowBQYDK2VwAyEAOAiOTCroL1xFxoCKYaZJDTxhLOHaI1cURm/HSPvEy7s="
	keyBob   = "MCowBQYDK2VwAyEA3XVgQP3VFF4r+YMtJk3QgOSz5zAWvfZXS0zYfqppf14="
)

func card(lines ...string) *VCard {
	all := append([]string{"BEGIN:VCARD", "VERSION:4.0", "FN:A"}, lines...)
	all = append(all, "END:VCARD")
	return NewVCardFromText(strings.Join(all, "\r\n") + "\r\n")
}

func TestPreferredKey(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{"lowest pref wins", []string{"KEY;TYPE=public;ALG=ed25519;PREF=2;ENCODING=b:" + keyAlice, "KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:" + keyBob}, keyBob},
		{"revoked skipped", []string{"KEY;ALG=ed25519;PREF=1:" + keyAlice, "KEY;ALG=ed25519;PREF=2:" + keyBob, "REVKEY;ALG=ed25519:" + keyAlice}, keyBob},
		{"only revoked", []string{"KEY;ALG=ed25519:" + keyAlice, "REVKEY;ALG=ed25519:" + keyAlice}, ""},
		{"non ed25519 skipped", []string{"KEY;ALG=rsa;PREF=1:" + keyAlice, "KEY;ALG=ed25519;PREF=2:" + keyBob}, keyBob},
		{"no pref sorts last", []string{"KEY;ALG=ed25519:" + keyAlice, "KEY;ALG=ed25519;PREF=9:" + keyBob}, keyBob},
	}
	for _, c := range cases {
		got := card(c.lines...).PreferredKey()
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: expected nil, got %v", c.name, got.KeyB64)
		case c.want != "" && (got == nil || got.KeyB64 != c.want):
			t.Errorf("%s: got %v want %s", c.name, got, c.want)
		}
	}
}

func TestAddLineKeepsCRLFAndUnknownProperties(t *testing.T) {
	ex := card("X-ACME-UNKNOWN;FOO=bar:keep me")
	before := ex.String()
	sig := strings.Repeat("0c", 64)
	if err := ex.AddLine("X-ENDORSE;SIG=" + sig + ";ENCODING=b:" + keyAlice); err != nil {
		t.Fatal(err)
	}
	text := ex.String()
	if !strings.HasPrefix(text, strings.TrimSuffix(before, "END:VCARD\r\n")) ||
		!strings.HasSuffix(text, "ENCODING=b:"+keyAlice+"\r\nEND:VCARD\r\n") ||
		strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") ||
		!strings.Contains(text, "X-ACME-UNKNOWN;FOO=bar:keep me\r\n") ||
		len(ex.Profile.Endorsements) != 1 {
		t.Errorf("unexpected result %q", text)
	}
	if err := ex.AddLine("X-FOO:a\r\nX-EVIL:b"); err == nil {
		t.Error("line breaks must be rejected")
	}
	if err := NewVCardFromText("BEGIN:VCARD\r\nFN:x\r\n").AddLine("X-A:b"); err == nil {
		t.Error("missing END:VCARD must be rejected")
	}
}

func TestFileRoundTripKeepsBytes(t *testing.T) {
	text := testutil.GoldenString(t, "vcards/full.vcf")
	path := filepath.Join(t.TempDir(), "alice.vcf")
	os.WriteFile(path, []byte(text), 0o644)
	v, err := NewVCardFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range ValidateProfile(v.Profile).Warnings {
		if w.Code == "line-endings" {
			t.Error("CRLF file must not warn about line endings")
		}
	}
	if err := v.ToFile(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != text {
		t.Error("file changed after round trip")
	}
	if _, err := NewVCardFromPath(filepath.Dir(path)); err == nil {
		t.Error("directory must be rejected")
	}
}

func TestToJSONAndHasEndorsement(t *testing.T) {
	v := NewVCardFromText(testutil.GoldenString(t, "vcards/endorse_valid.vcf"))
	js, err := v.ToJSON()
	if err != nil || js != testutil.GoldenString(t, "vcards/endorse_valid.parse.out") {
		t.Errorf("ToJSON mismatch: %v", err)
	}
	if !v.HasEndorsementFor(keyBob) || v.HasEndorsementFor(keyAlice) {
		t.Error("HasEndorsementFor")
	}
	_ = model.Str
}

func TestClassifyInputs(t *testing.T) {
	urls, paths := ClassifyInputs([]string{"https://a/x.vcf", "http://b", "HTTP://c", "dir/x.vcf", "ftp://d"})
	if strings.Join(urls, ",") != "https://a/x.vcf,http://b" || strings.Join(paths, ",") != "HTTP://c,dir/x.vcf,ftp://d" {
		t.Errorf("%v %v", urls, paths)
	}
}

func TestFileHelpers(t *testing.T) {
	for name, want := range map[string]bool{"a.vcf": true, "A.VCARD": true, "a.txt": false, ".vcf": true} {
		if FileIsVCardName(name) != want {
			t.Errorf("FileIsVCardName(%q) != %v", name, want)
		}
	}
	if FileIsVCardPath("dir/.vcf") || !FileIsVCardPath("dir/a.VCF") || FileIsVCardPath("dir/a.") {
		t.Error("FileIsVCardPath")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.vcf"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "sub", "b.vcard"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "sub", "c.txt"), nil, 0o644)
	var warnings []string
	files := LocalFilesFromInputs([]string{dir, filepath.Join(dir, "missing")}, FileIsVCardPath, &warnings)
	if len(files) != 2 || len(warnings) != 1 || !strings.Contains(warnings[0], "does not exist") {
		t.Errorf("files %v warnings %v", files, warnings)
	}
}

func TestPreferredKeyEdgeCases(t *testing.T) {
	if card().PreferredKey() != nil {
		t.Error("card without keys must have no preferred key")
	}
	// Equal PREF: the first listed key wins.
	got := card("KEY;ALG=ed25519;PREF=1:"+keyBob, "KEY;ALG=ed25519;PREF=1:"+keyAlice).PreferredKey()
	if got == nil || got.KeyB64 != keyBob {
		t.Errorf("tie: got %v", got)
	}
	// No PREF anywhere: the first listed key wins.
	got = card("KEY;ALG=ed25519:"+keyBob, "KEY;ALG=ed25519:"+keyAlice).PreferredKey()
	if got == nil || got.KeyB64 != keyBob {
		t.Errorf("no pref: got %v", got)
	}
	// A revoked lowest-PREF key is skipped even when its REVKEY comes first.
	got = card("REVKEY;ALG=ed25519:"+keyBob, "KEY;ALG=ed25519;PREF=1:"+keyBob, "KEY;ALG=ed25519;PREF=5:"+keyAlice).PreferredKey()
	if got == nil || got.KeyB64 != keyAlice {
		t.Errorf("revoked first: got %v", got)
	}
}

func TestAddLineLineEndings(t *testing.T) {
	t.Run("LF card stays LF", func(t *testing.T) {
		v := NewVCardFromText("BEGIN:VCARD\nVERSION:4.0\nFN:A\nEND:VCARD\n")
		if err := v.AddLine("X-A:b"); err != nil {
			t.Fatal(err)
		}
		if want := "BEGIN:VCARD\nVERSION:4.0\nFN:A\nX-A:b\nEND:VCARD\n"; v.String() != want {
			t.Errorf("got %q want %q", v.String(), want)
		}
	})
	t.Run("rejects bare CR and LF", func(t *testing.T) {
		for _, l := range []string{"X-A:b\n", "X-A:b\r", "\nX-A:b"} {
			if err := card().AddLine(l); err == nil {
				t.Errorf("%q must be rejected", l)
			}
		}
	})
	t.Run("trailing whitespace after END is tolerated", func(t *testing.T) {
		v := NewVCardFromText("BEGIN:VCARD\r\nVERSION:4.0\r\nFN:A\r\nEND:VCARD\r\n\r\n")
		if err := v.AddLine("X-A:b"); err != nil {
			t.Fatal(err)
		}
		if want := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:A\r\nX-A:b\r\nEND:VCARD\r\n\r\n"; v.String() != want {
			t.Errorf("got %q want %q", v.String(), want)
		}
	})
	t.Run("rejected input leaves the card unchanged", func(t *testing.T) {
		v := card()
		before := v.String()
		_ = v.AddLine("X-A:b\r\nX-B:c")
		if v.String() != before {
			t.Error("card changed after a rejected line")
		}
	})
}
