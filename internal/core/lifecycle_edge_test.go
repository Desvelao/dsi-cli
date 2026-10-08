package core

import (
	"strings"
	"testing"
	"time"

	"github.com/Desvelao/dsi-cli/internal/testutil"
)

func lifecycleKeys(t *testing.T) (alice, bob, carol string) {
	t.Helper()
	var keys map[string]struct {
		PublicB64 string `json:"public_b64_der"`
	}
	testutil.GoldenJSON(t, "keys/index.json", &keys)
	return keys["alice"].PublicB64, keys["bob"].PublicB64, keys["carol"].PublicB64
}

var lifecycleWhen = time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)

func vc(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

func TestRevokeKeyKeepsPrefOfOtherKeys(t *testing.T) {
	alice, bob, _ := lifecycleKeys(t)
	// Two keys share PREF=1: revoking one must not touch the other's PREF.
	text := vc("BEGIN:VCARD", "VERSION:4.0", "FN:A",
		"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:"+alice,
		"KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:"+bob, "END:VCARD")
	out, err := RevokeKey(text, alice, "lost", lifecycleWhen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:"+bob+"\r\n") {
		t.Errorf("other key lost its PREF: %q", out)
	}
	if strings.Contains(out, "PREF=1;ENCODING=b:"+alice) || !strings.Contains(out, "KEY;TYPE=public;ALG=ed25519;ENCODING=b:"+alice+"\r\n") {
		t.Errorf("revoked key kept PREF: %q", out)
	}
	if !strings.Contains(out, "REVKEY;TYPE=public;ALG=ed25519;REASON=lost;DATE=20250304T050607Z;ENCODING=b:"+alice+"\r\n") {
		t.Errorf("missing REVKEY: %q", out)
	}
	v := NewVCardFromText(out)
	if k := v.PreferredKey(); k == nil || k.KeyB64 != bob {
		t.Errorf("preferred key after revoke: %v", k)
	}
}

func TestRevokeKeyLineEndings(t *testing.T) {
	alice, _, _ := lifecycleKeys(t)
	crlf := testutil.GoldenString(t, "lifecycle/base.vcf")
	lf := strings.ReplaceAll(crlf, "\r\n", "\n")
	fromCRLF, err := RevokeKey(crlf, alice, "lost", lifecycleWhen)
	if err != nil {
		t.Fatal(err)
	}
	fromLF, err := RevokeKey(lf, alice, "lost", lifecycleWhen)
	if err != nil {
		t.Fatal(err)
	}
	if fromLF != fromCRLF {
		t.Errorf("LF input differs from CRLF input:\n%q\n%q", fromLF, fromCRLF)
	}
	if strings.Contains(strings.ReplaceAll(fromLF, "\r\n", ""), "\n") || !strings.HasSuffix(fromLF, "END:VCARD\r\n") {
		t.Errorf("output must be CRLF only: %q", fromLF)
	}
}

func TestRotateKeyLineEndings(t *testing.T) {
	_, bob, _ := lifecycleKeys(t)
	crlf := testutil.GoldenString(t, "lifecycle/base.vcf")
	lf := strings.ReplaceAll(crlf, "\r\n", "\n")
	a, errA := RotateKey(crlf, bob, "", "rotated", lifecycleWhen)
	b, errB := RotateKey(lf, bob, "", "rotated", lifecycleWhen)
	if errA != nil || errB != nil || a != b {
		t.Errorf("CRLF vs LF: %v %v\n%q\n%q", errA, errB, a, b)
	}
	if strings.Contains(strings.ReplaceAll(b, "\r\n", ""), "\n") {
		t.Errorf("output must be CRLF only: %q", b)
	}
}

func TestRevokeKeyUsesKeyAlgorithm(t *testing.T) {
	alice, _, _ := lifecycleKeys(t)
	cases := []struct{ name, keyParams, wantAlg string }{
		{"non-ed25519 ALG is copied", "TYPE=public;ALG=rsa;PREF=1;ENCODING=b", "rsa"},
		{"ed25519 ALG", "TYPE=public;ALG=ed25519;ENCODING=b", "ed25519"},
		{"missing ALG defaults to ed25519", "TYPE=public;ENCODING=b", "ed25519"},
		{"empty ALG defaults to ed25519", "TYPE=public;ALG=;ENCODING=b", "ed25519"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := vc("BEGIN:VCARD", "VERSION:4.0", "FN:A", "KEY;"+c.keyParams+":"+alice, "END:VCARD")
			out, err := RevokeKey(text, alice, "lost", lifecycleWhen)
			if err != nil {
				t.Fatal(err)
			}
			want := "REVKEY;TYPE=public;ALG=" + c.wantAlg + ";REASON=lost;DATE=20250304T050607Z;ENCODING=b:" + alice + "\r\n"
			if !strings.Contains(out, want) {
				t.Errorf("want %q in %q", want, out)
			}
		})
	}
}

func TestRevokeKeyAlreadyRevokedAndTwice(t *testing.T) {
	alice, _, _ := lifecycleKeys(t)
	base := testutil.GoldenString(t, "lifecycle/base.vcf")
	out, err := RevokeKey(base, alice, "lost", lifecycleWhen)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RevokeKey(out, alice, "lost", lifecycleWhen); err == nil || err.Error() != "The key is already revoked in the vCard" {
		t.Errorf("second revoke: %v", err)
	}
}

// Rotating to an already revoked key is covered by the rotate_revoked_new golden case.

func TestRotateKeyUsesOldKeyAlgorithm(t *testing.T) {
	alice, bob, carol := lifecycleKeys(t)
	text := vc("BEGIN:VCARD", "VERSION:4.0", "FN:A",
		"KEY;TYPE=public;ALG=ed25519;ENCODING=b:"+carol,
		"KEY;TYPE=public;ALG=rsa;PREF=1;ENCODING=b:"+alice, "END:VCARD")
	out, err := RotateKey(text, bob, "", "rotated", lifecycleWhen)
	if err != nil {
		t.Fatal(err)
	}
	want := "REVKEY;TYPE=public;ALG=rsa;REASON=rotated;DATE=20250304T050607Z;ENCODING=b:" + alice + "\r\n"
	if !strings.Contains(out, want) {
		t.Errorf("want %q in %q", want, out)
	}
}
