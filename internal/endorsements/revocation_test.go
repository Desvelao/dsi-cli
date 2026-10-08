package endorsements

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/crypto"
	"github.com/Desvelao/dsi-cli/internal/model"
)

func sp(s string) *string { return &s }
func ip(i int64) *int64   { return &i }

type testKey struct {
	priv ed25519.PrivateKey
	b64  string
}

func newTestKey(t *testing.T) testKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := crypto.PublicKeyToB64DER(pub)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{priv, b}
}

func (k testKey) endorse(t *testing.T, endorsee testKey, date *string) model.Endorsement {
	t.Helper()
	return model.Endorsement{
		EndorseeKeyB64: endorsee.b64,
		SignatureHex:   crypto.SignEndorsement(k.priv, endorsee.b64),
		Date:           date,
	}
}

func TestKeyAcceptsEndorsement(t *testing.T) {
	const key = "KEY"
	cases := []struct {
		name   string
		rev    *model.RevokedKey
		date   *string
		want   bool
		reason string
	}{
		{"not revoked", nil, nil, true, ""},
		{"other key revoked", &model.RevokedKey{KeyB64: "OTHER", Reason: sp("compromised")}, nil, true, ""},
		{"compromised", &model.RevokedKey{KeyB64: key, Reason: sp("compromised")}, sp("20240101T000000Z"), false, "compromised"},
		{"deprecated ignores dates", &model.RevokedKey{KeyB64: key, Reason: sp("deprecated")}, nil, true, ""},
		{"no reason, no revocation date", &model.RevokedKey{KeyB64: key}, sp("20240101T000000Z"), false, "without a valid DATE"},
		{"invalid revocation date", &model.RevokedKey{KeyB64: key, Date: sp("garbage")}, sp("20240101T000000Z"), false, "without a valid DATE"},
		{"missing endorsement date", &model.RevokedKey{KeyB64: key, Date: sp("20240101T000000Z")}, nil, false, "no DATE"},
		{"invalid endorsement date", &model.RevokedKey{KeyB64: key, Date: sp("20240101T000000Z")}, sp("20240230T000000Z"), false, "no DATE"},
		{"signed before revocation", &model.RevokedKey{KeyB64: key, Date: sp("20240101T000000Z")}, sp("20231231T235959Z"), true, ""},
		{"signed exactly at revocation", &model.RevokedKey{KeyB64: key, Date: sp("20240101T000000Z")}, sp("20240101T000000Z"), false, "after the key was revoked"},
		{"signed after revocation", &model.RevokedKey{KeyB64: key, Date: sp("20240101T000000Z")}, sp("20240101T000001Z"), false, "after the key was revoked"},
		{"unknown reason uses dates", &model.RevokedKey{KeyB64: key, Reason: sp("lost"), Date: sp("20240101T000000Z")}, sp("20230101T000000Z"), true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &model.Profile{}
			if c.rev != nil {
				p.Revocations = []model.RevokedKey{*c.rev}
			}
			ok, why := KeyAcceptsEndorsement(p, key, model.Endorsement{Date: c.date})
			if ok != c.want {
				t.Fatalf("ok = %v (%q), want %v", ok, why, c.want)
			}
			if c.want && why != "" || !strings.Contains(why, c.reason) {
				t.Errorf("reason = %q, want containing %q", why, c.reason)
			}
		})
	}
}

func TestVerifyEndorsementsRevocation(t *testing.T) {
	signer, endorsee := newTestKey(t), newTestKey(t)
	build := func(rev model.RevokedKey, date *string) Result {
		p := &model.Profile{
			Keys:         []model.PublicKey{{Alg: "ed25519", KeyB64: signer.b64}},
			Endorsements: []model.Endorsement{signer.endorse(t, endorsee, date)},
			Revocations:  []model.RevokedKey{rev},
		}
		rs := VerifyEndorsements(p)
		if len(rs) != 1 {
			t.Fatalf("got %d results", len(rs))
		}
		return rs[0]
	}
	r := build(model.RevokedKey{KeyB64: signer.b64, Reason: sp("compromised")}, nil)
	if r.Status != Invalid || !strings.Contains(r.Reason, "compromised") || r.SignerKeyB64 != nil {
		t.Errorf("compromised: %+v", r)
	}
	r = build(model.RevokedKey{KeyB64: signer.b64, Reason: sp("deprecated")}, nil)
	if r.Status != Valid || r.SignerKeyB64 == nil || *r.SignerKeyB64 != signer.b64 {
		t.Errorf("deprecated: %+v", r)
	}
	rev := model.RevokedKey{KeyB64: signer.b64, Date: sp("20240101T000000Z")}
	if r = build(rev, sp("20230101T000000Z")); r.Status != Valid {
		t.Errorf("before: %+v", r)
	}
	if r = build(rev, sp("20240101T000000Z")); r.Status != Invalid {
		t.Errorf("at: %+v", r)
	}
}

func TestVerifyEndorsementsMultiKeyPref(t *testing.T) {
	k1, k2, endorsee := newTestKey(t), newTestKey(t), newTestKey(t)
	e := k2.endorse(t, endorsee, nil)

	// Signer is found regardless of PREF order or missing PREF.
	for name, keys := range map[string][]model.PublicKey{
		"k2 preferred":  {{KeyB64: k1.b64, Pref: ip(2)}, {KeyB64: k2.b64, Pref: ip(1)}},
		"k1 preferred":  {{KeyB64: k1.b64, Pref: ip(1)}, {KeyB64: k2.b64, Pref: ip(2)}},
		"no prefs":      {{KeyB64: k1.b64}, {KeyB64: k2.b64}},
		"nil pref last": {{KeyB64: k1.b64}, {KeyB64: k2.b64, Pref: ip(5)}},
		"skips non-ed":  {{Alg: "rsa", KeyB64: k2.b64, Pref: ip(1)}, {KeyB64: k1.b64}},
	} {
		p := &model.Profile{Keys: keys, Endorsements: []model.Endorsement{e}}
		r := VerifyEndorsements(p)[0]
		if name == "skips non-ed" {
			if r.Status != Invalid || r.Reason != "signature does not match any key" {
				t.Errorf("%s: %+v", name, r)
			}
			continue
		}
		if r.Status != Valid || r.SignerKeyB64 == nil || *r.SignerKeyB64 != k2.b64 {
			t.Errorf("%s: %+v", name, r)
		}
	}

	// Revoked first key does not stop a later key from verifying; the
	// original profile key order must not be mutated.
	p := &model.Profile{
		Keys:         []model.PublicKey{{KeyB64: k1.b64, Pref: ip(2)}, {KeyB64: k2.b64, Pref: ip(1)}},
		Endorsements: []model.Endorsement{e, k1.endorse(t, endorsee, nil)},
		Revocations:  []model.RevokedKey{{KeyB64: k1.b64, Reason: sp("compromised")}},
	}
	rs := VerifyEndorsements(p)
	if rs[0].Status != Valid || rs[1].Status != Invalid || !strings.Contains(rs[1].Reason, "compromised") {
		t.Errorf("results: %+v", rs)
	}
	if p.Keys[0].KeyB64 != k1.b64 {
		t.Error("Profile.Keys was reordered")
	}
}

func TestVerifyEndorsementsErrorBranches(t *testing.T) {
	signer, endorsee := newTestKey(t), newTestKey(t)
	good := signer.endorse(t, endorsee, nil)
	goodKeys := []model.PublicKey{{KeyB64: signer.b64}}

	run := func(keys []model.PublicKey, e model.Endorsement) Result {
		return VerifyEndorsements(&model.Profile{Keys: keys, Endorsements: []model.Endorsement{e}})[0]
	}

	if rs := VerifyEndorsements(&model.Profile{Keys: goodKeys}); len(rs) != 0 {
		t.Errorf("no endorsements: %+v", rs)
	}

	for name, keys := range map[string][]model.PublicKey{
		"no keys":     nil,
		"non-ed25519": {{Alg: "rsa", KeyB64: signer.b64}},
		"invalid b64": {{KeyB64: "!!!notbase64"}},
		"invalid DER": {{KeyB64: "AAAA"}},
	} {
		r := run(keys, good)
		if r.Status != Unverifiable || r.Reason != "no usable KEY" {
			t.Errorf("%s: %+v", name, r)
		}
	}

	bad := good
	bad.EndorseeKeyB64 = "AAAA"
	if r := run(goodKeys, bad); r.Status != Invalid || r.Reason == "" {
		t.Errorf("invalid endorsee: %+v", r)
	}
	// Usable-key check comes first.
	if r := run(nil, bad); r.Status != Unverifiable {
		t.Errorf("no key + invalid endorsee: %+v", r)
	}

	for name, sig := range map[string]string{"empty": "", "uppercase": strings.ToUpper(good.SignatureHex), "odd": "abc"} {
		e := good
		e.SignatureHex = sig
		if r := run(goodKeys, e); r.Status != Invalid || r.Reason == "" {
			t.Errorf("signature %s: %+v", name, r)
		}
	}

	other := newTestKey(t)
	e := other.endorse(t, endorsee, nil)
	if r := run(goodKeys, e); r.Status != Invalid || r.Reason != "signature does not match any key" {
		t.Errorf("wrong signer: %+v", r)
	}
}
