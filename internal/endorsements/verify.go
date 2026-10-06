// Package endorsements verifies X-ENDORSE properties and applies the key
// revocation rules of RFC section 3.2.1.3.
package endorsements

import (
	"crypto/ed25519"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/Desvelao/dsipy/internal/crypto"
	"github.com/Desvelao/dsipy/internal/model"
)

// Verification statuses.
const (
	Valid        = "valid"
	Invalid      = "invalid"
	Unverifiable = "unverifiable"
)

// Result is the outcome of verifying one endorsement.
type Result struct {
	Endorsement  model.Endorsement
	Status       string
	SignerKeyB64 *string
	Reason       string
}

// dsiDateRe mirrors the regular expression Python's strptime builds for
// "%Y%m%dT%H%M%SZ" (case-insensitive, single-digit fields accepted).
var dsiDateRe = regexp.MustCompile(`(?i)^(\d{4})(1[0-2]|0[1-9]|[1-9])(3[01]|[12]\d|0[1-9]|[1-9]| [1-9])T(2[0-3]|[0-1]\d|\d)([0-5]\d|\d)(6[0-1]|[0-5]\d|\d)Z$`)

// ParseDSIDate parses YYYYMMDDThhmmssZ (UTC). It returns false if the value is
// missing or malformed.
func ParseDSIDate(value *string) (time.Time, bool) {
	if value == nil || *value == "" {
		return time.Time{}, false
	}
	m := dsiDateRe.FindStringSubmatch(*value)
	if m == nil {
		return time.Time{}, false
	}
	n := make([]int, 7)
	for i := 1; i <= 6; i++ {
		v, err := strconv.Atoi(trimSpace(m[i]))
		if err != nil {
			return time.Time{}, false
		}
		n[i] = v
	}
	if n[1] < 1 || n[6] > 59 {
		return time.Time{}, false
	}
	t := time.Date(n[1], time.Month(n[2]), n[3], n[4], n[5], n[6], 0, time.UTC)
	if t.Year() != n[1] || int(t.Month()) != n[2] || t.Day() != n[3] {
		return time.Time{}, false // e.g. February 30
	}
	return t, true
}

func trimSpace(s string) string {
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	return s
}

func revocationFor(p *model.Profile, keyB64 string) *model.RevokedKey {
	for i := range p.Revocations {
		if p.Revocations[i].KeyB64 == keyB64 {
			return &p.Revocations[i]
		}
	}
	return nil
}

// KeyAcceptsEndorsement applies the revocation rules of RFC section 3.2.1.3 to
// an endorsement signed by keyB64.
func KeyAcceptsEndorsement(p *model.Profile, keyB64 string, e model.Endorsement) (bool, string) {
	rev := revocationFor(p, keyB64)
	if rev == nil {
		return true, ""
	}
	switch model.Str(rev.Reason) {
	case "compromised":
		return false, "the signing key was revoked as compromised"
	case "deprecated":
		return true, ""
	}
	revokedAt, okRevoked := ParseDSIDate(rev.Date)
	if !okRevoked {
		return false, "the signing key was revoked without a valid DATE"
	}
	signedAt, okSigned := ParseDSIDate(e.Date)
	if !okSigned {
		return false, "the signing key was revoked and the endorsement has no DATE"
	}
	if !signedAt.Before(revokedAt) {
		return false, "the endorsement was signed after the key was revoked"
	}
	return true, ""
}

type usableKey struct {
	b64 string
	key ed25519.PublicKey
}

// VerifyEndorsements verifies each X-ENDORSE of the profile against the
// profile's own keys.
func VerifyEndorsements(p *model.Profile) []Result {
	keys := append([]model.PublicKey(nil), p.Keys...)
	sort.SliceStable(keys, func(i, j int) bool { return prefOrInf(keys[i]) < prefOrInf(keys[j]) })
	var usable []usableKey
	for _, k := range keys {
		if k.Alg != "" && k.Alg != "ed25519" {
			continue
		}
		pub, err := crypto.LoadPublicKeyB64DER(k.KeyB64)
		if err != nil {
			continue
		}
		usable = append(usable, usableKey{k.KeyB64, pub})
	}

	results := make([]Result, 0, len(p.Endorsements))
	for _, e := range p.Endorsements {
		if len(usable) == 0 {
			results = append(results, Result{Endorsement: e, Status: Unverifiable, Reason: "no usable KEY"})
			continue
		}
		if _, err := crypto.LoadPublicKeyB64DER(e.EndorseeKeyB64); err != nil {
			results = append(results, Result{Endorsement: e, Status: Invalid, Reason: err.Error()})
			continue
		}
		if _, err := crypto.DecodeSignatureHex(e.SignatureHex); err != nil {
			results = append(results, Result{Endorsement: e, Status: Invalid, Reason: err.Error()})
			continue
		}
		reason := "signature does not match any key"
		var verified *string
		for _, k := range usable {
			if !crypto.VerifyEndorsementSignature(k.key, e.EndorseeKeyB64, e.SignatureHex) {
				continue
			}
			if ok, why := KeyAcceptsEndorsement(p, k.b64, e); ok {
				b := k.b64
				verified = &b
				break
			} else {
				reason = why
			}
		}
		if verified != nil {
			results = append(results, Result{Endorsement: e, Status: Valid, SignerKeyB64: verified})
		} else {
			results = append(results, Result{Endorsement: e, Status: Invalid, Reason: reason})
		}
	}
	return results
}

func prefOrInf(k model.PublicKey) float64 {
	if k.Pref == nil {
		return 1e300
	}
	return float64(*k.Pref)
}
