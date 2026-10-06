package core

import (
	"strings"
	"testing"
	"time"

	"github.com/Desvelao/dsipy/internal/testutil"
)

type lifecycleCase struct {
	Ok    bool
	Out   string
	Error string
}

func TestLifecycleGolden(t *testing.T) {
	var keys map[string]struct {
		PublicB64 string `json:"public_b64_der"`
	}
	testutil.GoldenJSON(t, "keys/index.json", &keys)
	alice, bob, carol := keys["alice"].PublicB64, keys["bob"].PublicB64, keys["carol"].PublicB64
	base := testutil.GoldenString(t, "lifecycle/base.vcf")
	two := testutil.GoldenString(t, "lifecycle/two_keys.vcf")
	nokey := testutil.GoldenString(t, "lifecycle/no_key.vcf")
	when := time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)

	run := map[string]func() (string, error){
		"revoke_preferred":   func() (string, error) { return RevokeKey(base, alice, "lost", when) },
		"revoke_second":      func() (string, error) { return RevokeKey(two, carol, "compromised", when) },
		"revoke_unknown_key": func() (string, error) { return RevokeKey(base, bob, "lost", when) },
		"revoke_bad_reason":  func() (string, error) { return RevokeKey(base, alice, "bored", when) },
		"rotate_default_old": func() (string, error) { return RotateKey(base, bob, "", "rotated", when) },
		"rotate_superseded":  func() (string, error) { return RotateKey(two, bob, carol, "superseded", when) },
		"rotate_bad_reason":  func() (string, error) { return RotateKey(base, bob, "", "lost", when) },
		"rotate_ambiguous": func() (string, error) {
			return RotateKey(strings.Replace(two, "PREF=2", "PREF=1", 1), bob, "", "rotated", when)
		},
		"rotate_existing_new": func() (string, error) { return RotateKey(base, alice, "", "rotated", when) },
		"add_preferred":       func() (string, error) { return AddKey(base, bob, true) },
		"add_not_preferred":   func() (string, error) { return AddKey(base, bob, false) },
		"add_to_empty":        func() (string, error) { return AddKey(nokey, bob, true) },
		"add_existing":        func() (string, error) { return AddKey(base, alice, true) },
		"add_to_malformed": func() (string, error) {
			return AddKey(strings.Replace(base, "END:VCARD", "no colon here\r\nEND:VCARD", 1), bob, true)
		},
		"add_invalid_key": func() (string, error) { return AddKey(base, "@@@", true) },
	}
	var golden map[string]lifecycleCase
	testutil.GoldenJSON(t, "lifecycle/cases.json", &golden)
	if len(golden) != len(run) {
		t.Fatalf("golden has %d cases, test has %d", len(golden), len(run))
	}
	for name, fn := range run {
		t.Run(name, func(t *testing.T) {
			want := golden[name]
			got, err := fn()
			if want.Ok {
				if err != nil || got != want.Out {
					t.Errorf("got %q, %v\nwant %q", got, err, want.Out)
				}
				return
			}
			wantMsg := strings.ReplaceAll(want.Error, "dsipy key add", "dsi key add")
			if err == nil || err.Error() != wantMsg {
				t.Errorf("error %v, want %q", err, wantMsg)
			}
		})
	}
}
