package testutil

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"testing"
)

type keyIndex map[string]struct {
	SeedHex   string `json:"seed_hex"`
	PublicB64 string `json:"public_b64_der"`
}

func keyFromSeed(t *testing.T, seedHex string) ed25519.PrivateKey {
	t.Helper()
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		t.Fatal(err)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// Go's stdlib must reproduce the golden keys byte for byte (DER, PEM, base64).
func TestKeysMatchGolden(t *testing.T) {
	var idx keyIndex
	GoldenJSON(t, "keys/index.json", &idx)
	if len(idx) != 4 {
		t.Fatalf("expected 4 keys, got %d", len(idx))
	}
	for name, k := range idx {
		priv := keyFromSeed(t, k.SeedHex)
		pubDER, err := x509.MarshalPKIXPublicKey(priv.Public())
		if err != nil {
			t.Fatal(err)
		}
		if got := base64.StdEncoding.EncodeToString(pubDER); got != k.PublicB64 {
			t.Errorf("%s: public b64 DER %q != %q", name, got, k.PublicB64)
		}
		pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
		if string(pubPEM) != GoldenString(t, "keys/"+name+".pub.pem") {
			t.Errorf("%s: public PEM differs", name)
		}
		privDER, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			t.Fatal(err)
		}
		privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
		if string(privPEM) != GoldenString(t, "keys/"+name+".priv.pem") {
			t.Errorf("%s: private PEM differs", name)
		}
	}
}

// Ed25519 is deterministic: Go must produce the same signatures as the golden ones.
func TestSignaturesMatchGolden(t *testing.T) {
	var idx keyIndex
	GoldenJSON(t, "keys/index.json", &idx)

	var endorsements []struct {
		Signer, Endorsee, Canonical string
		SignatureHex                string `json:"signature_hex"`
	}
	GoldenJSON(t, "canonical/endorsements.json", &endorsements)
	for _, c := range endorsements {
		priv := keyFromSeed(t, idx[c.Signer].SeedHex)
		want := "endorse:" + idx[c.Endorsee].PublicB64
		if c.Canonical != want {
			t.Errorf("canonical %q != %q", c.Canonical, want)
		}
		if got := hex.EncodeToString(ed25519.Sign(priv, []byte(c.Canonical))); got != c.SignatureHex {
			t.Errorf("endorsement %s->%s signature differs", c.Signer, c.Endorsee)
		}
	}

	var items []struct {
		Signer             string
		PubDate            string `json:"pub_date"`
		Title, Description string
		Canonical          string
		SignatureHex       string `json:"signature_hex"`
	}
	GoldenJSON(t, "canonical/feed_items.json", &items)
	for _, c := range items {
		priv := keyFromSeed(t, idx[c.Signer].SeedHex)
		canonical := c.PubDate + "\n" + c.Title + "\n" + c.Description
		if c.Canonical != canonical {
			t.Errorf("feed canonical %q != %q", c.Canonical, canonical)
		}
		if got := hex.EncodeToString(ed25519.Sign(priv, []byte(canonical))); got != c.SignatureHex {
			t.Errorf("feed item %q signature differs", c.Title)
		}
	}
}
