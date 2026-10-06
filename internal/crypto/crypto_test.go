package crypto

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Desvelao/dsipy/internal/testutil"
)

const (
	bobKey         = "MCowBQYDK2VwAyEA3XVgQP3VFF4r+YMtJk3QgOSz5zAWvfZXS0zYfqppf14="
	aliceSigForBob = "0c036138c11d467ad6518502d45dd0eeea4745b17ebea8dac01849cdd3946d67" +
		"9a8c6bd039caff7077abfbc2aff9a1d96bff16b20134c1368fc93a2b17f07e00"
)

func alice() ed25519.PrivateKey {
	seed := make([]byte, 32)
	copy(seed, "alice")
	return ed25519.NewKeyFromSeed(seed)
}

func TestSignEndorsementMatchesSpecExample(t *testing.T) {
	if got := SignEndorsement(alice(), bobKey); got != aliceSigForBob {
		t.Errorf("got %s", got)
	}
	pub := alice().Public().(ed25519.PublicKey)
	if !VerifyEndorsementSignature(pub, bobKey, aliceSigForBob) {
		t.Error("valid signature rejected")
	}
	if VerifyEndorsementSignature(pub, bobKey[:len(bobKey)-4]+"AAA=", aliceSigForBob) {
		t.Error("signature for another key accepted")
	}
}

func TestDecodeSignatureHex(t *testing.T) {
	for _, bad := range []string{"", "ABCD", "ab cd", "zz", "abc"} {
		if _, err := DecodeSignatureHex(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := DecodeSignatureHex("abc"); err == nil || !strings.Contains(err.Error(), "position 3") {
		t.Errorf("odd length message: %v", err)
	}
	if b, err := DecodeSignatureHex("00ff"); err != nil || len(b) != 2 {
		t.Errorf("%v %v", b, err)
	}
}

func TestDecodeB64StrictGolden(t *testing.T) {
	var cases []struct {
		In    string
		Ok    bool
		Hex   string
		Error string
	}
	testutil.GoldenJSON(t, "crypto/decode_b64_strict.json", &cases)
	if len(cases) < 40 {
		t.Fatalf("few cases: %d", len(cases))
	}
	for _, c := range cases {
		got, err := DecodeB64Strict(c.In)
		if c.Ok {
			if err != nil || hexOf(got) != c.Hex {
				t.Errorf("%q: got %x, %v; want %s", c.In, got, err, c.Hex)
			}
		} else if err == nil || err.Error() != c.Error {
			t.Errorf("%q: error %v; want %q", c.In, err, c.Error)
		}
	}
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

func TestLoadPublicKeyRejections(t *testing.T) {
	if _, err := LoadPublicKeyB64DER(bobKey); err != nil {
		t.Fatal(err)
	}
	rsaB64 := strings.TrimSpace(testutil.GoldenString(t, "crypto/rsa.pub.b64"))
	if _, err := LoadPublicKeyB64DER(rsaB64); err == nil || err.Error() != "Public key is not an Ed25519 key" {
		t.Errorf("rsa b64: %v", err)
	}
	if _, err := LoadPublicKeyPEM([]byte(testutil.GoldenString(t, "crypto/rsa.pub.pem"))); err == nil || err.Error() != "Public key is not an Ed25519 key" {
		t.Errorf("rsa pem: %v", err)
	}
	if _, err := LoadPrivateKeyPEM([]byte(testutil.GoldenString(t, "crypto/ec.priv.pem"))); err == nil || err.Error() != "Private key is not an Ed25519 key" {
		t.Errorf("ec: %v", err)
	}
	garbage := "Unable to load PEM file. See https://cryptography.io/en/latest/faq/#why-can-t-i-import-my-pem-file for more details. MalformedFraming"
	if _, err := LoadPublicKeyPEM([]byte("not a pem")); err == nil || err.Error() != garbage {
		t.Errorf("garbage pem: %v", err)
	}
	for _, in := range []string{"", "aGVsbG8="} { // empty and garbage DER
		_, err := LoadPublicKeyB64DER(in)
		if err == nil || !strings.HasPrefix(err.Error(), "Could not deserialize key data.") {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestKeyFilesMatchGolden(t *testing.T) {
	priv, err := LoadPrivateKeyPEM([]byte(testutil.GoldenString(t, "keys/alice.priv.pem")))
	if err != nil {
		t.Fatal(err)
	}
	if got := SignEndorsement(priv, bobKey); got != aliceSigForBob {
		t.Errorf("signature from PEM key differs: %s", got)
	}
	pub, err := LoadPublicKeyPEM([]byte(testutil.GoldenString(t, "keys/alice.pub.pem")))
	if err != nil || !pub.Equal(priv.Public()) {
		t.Errorf("public PEM mismatch: %v", err)
	}
	var conv struct{ In, Out string }
	testutil.GoldenJSON(t, "crypto/b64der_to_pem.json", &conv)
	if got, err := B64DERToPublicKeyPEM(conv.In); err != nil || got != conv.Out {
		t.Errorf("b64der_to_pem: %q %v", got, err)
	}
}

func TestActionGenerateKeypair(t *testing.T) {
	dir := t.TempDir()
	priv, pub := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	old := syscall.Umask(0o022)
	privPEM, pubPEM, b64, err := ActionGenerateKeypair(priv, pub, false)
	syscall.Umask(old)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(priv); string(got) != string(privPEM) {
		t.Error("private file differs from returned PEM")
	}
	if got, _ := os.ReadFile(pub); string(got) != string(pubPEM) {
		t.Error("public file differs from returned PEM")
	}
	if st, _ := os.Stat(priv); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
	key, err := LoadPrivateKeyPEM(privPEM)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPublicKeyB64DER(b64)
	if err != nil || !loaded.Equal(key.Public()) {
		t.Errorf("b64 does not match private key: %v", err)
	}
}

func TestRefusesToOverwriteWithoutForce(t *testing.T) {
	for _, existing := range []string{"private.pem", "public.pem"} {
		dir := t.TempDir()
		priv, pub := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
		os.WriteFile(filepath.Join(dir, existing), []byte("keep"), 0o644)
		if _, _, _, err := ActionGenerateKeypair(priv, pub, false); !IsExist(err) {
			t.Fatalf("%s: expected exists error, got %v", existing, err)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, existing)); string(got) != "keep" {
			t.Errorf("%s was modified", existing)
		}
		other := pub
		if existing == "public.pem" {
			other = priv
		}
		if _, err := os.Stat(other); err == nil {
			t.Errorf("%s was created", other)
		}
	}
}

func TestForceOverwritesAndTightensMode(t *testing.T) {
	dir := t.TempDir()
	priv, pub := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	os.WriteFile(priv, []byte("old"), 0o644)
	os.Chmod(priv, 0o644)
	privPEM, _, _, err := ActionGenerateKeypair(priv, pub, true)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(priv); string(got) != string(privPEM) {
		t.Error("not overwritten")
	}
	if st, _ := os.Stat(priv); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode().Perm())
	}
}
