package core

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Desvelao/dsi-cli/internal/crypto"
	"github.com/Desvelao/dsi-cli/internal/model"
	"github.com/Desvelao/dsi-cli/internal/strutil"
	"github.com/Desvelao/dsi-cli/internal/vcard"
)

// lifecycleLines returns the logical lines of the profile without BEGIN/END.
func lifecycleLines(p *model.Profile) []model.RawLine {
	var out []model.RawLine
	for _, l := range p.RawLines {
		if l.Name == nil || *l.Name == "BEGIN" || *l.Name == "END" {
			continue
		}
		out = append(out, l)
	}
	return out
}

func render(lines []model.RawLine) (string, error) {
	text := []string{"BEGIN:VCARD"}
	for _, l := range lines {
		if l.Edited {
			s, err := vcard.FormatProperty(*l.Name, l.Params, *l.RawValue, l.Group)
			if err != nil {
				return "", err
			}
			text = append(text, s)
		} else {
			text = append(text, l.Line)
		}
	}
	text = append(text, "END:VCARD")
	return strings.Join(text, "\r\n") + "\r\n", nil
}

func withoutPref(l model.RawLine) model.RawLine {
	var params []model.Param
	for _, p := range l.Params {
		if p.Name != "PREF" {
			params = append(params, p)
		}
	}
	l.Params = params
	l.Edited = true
	return l
}

func newLine(name string, params []model.Param, value string) model.RawLine {
	return model.RawLine{Name: &name, Params: params, RawValue: &value, Edited: true}
}

func revkeyLine(keyB64, reason string, when time.Time, alg string) model.RawLine {
	if alg == "" {
		alg = "ed25519"
	}
	return newLine("REVKEY", []model.Param{
		{Name: "TYPE", Value: "public"},
		{Name: "ALG", Value: alg},
		{Name: "REASON", Value: reason},
		{Name: "DATE", Value: when.UTC().Format(model.DSIDateFormat)},
		{Name: "ENCODING", Value: "b"},
	}, keyB64)
}

func keyLine(keyB64 string, pref *int64) model.RawLine {
	params := []model.Param{{Name: "TYPE", Value: "public"}, {Name: "ALG", Value: "ed25519"}}
	if pref != nil {
		params = append(params, model.Param{Name: "PREF", Value: fmt.Sprint(*pref)})
	}
	params = append(params, model.Param{Name: "ENCODING", Value: "b"})
	return newLine("KEY", params, keyB64)
}

func checkWellFormed(p *model.Profile) error {
	if len(p.Errors) > 0 {
		return fmt.Errorf("The vCard has malformed lines: %s", strings.Join(p.Errors, "; "))
	}
	return nil
}

func hasKey(p *model.Profile, keyB64 string) bool {
	for _, k := range p.Keys {
		if k.KeyB64 == keyB64 {
			return true
		}
	}
	return false
}

func hasRevocation(p *model.Profile, keyB64 string) bool {
	for _, r := range p.Revocations {
		if r.KeyB64 == keyB64 {
			return true
		}
	}
	return false
}

// RevokeKey adds a REVKEY for a key listed in the vCard and returns the new
// vCard text. If the revoked key was the preferred one it loses its PREF
// parameter, so the vCard has no preferred key until a new one is added. A zero
// `when` means now.
func RevokeKey(text, keyB64, reason string, when time.Time) (string, error) {
	if !model.RevocationReasons[reason] {
		return "", fmt.Errorf("Unknown reason '%s' (expected one of %s)", reason, sortedReasons())
	}
	if _, err := crypto.LoadPublicKeyB64DER(keyB64); err != nil {
		return "", err
	}
	if when.IsZero() {
		when = time.Now().UTC()
	}
	p := vcard.ParseVCard(text)
	if err := checkWellFormed(p); err != nil {
		return "", err
	}
	if !hasKey(p, keyB64) {
		return "", errors.New("The key is not listed as KEY in the vCard")
	}
	if hasRevocation(p, keyB64) {
		return "", errors.New("The key is already revoked in the vCard")
	}

	var lines []model.RawLine
	alg := "ed25519"
	for _, l := range lifecycleLines(p) {
		if *l.Name == "KEY" && strutil.Strip(*l.RawValue) == keyB64 {
			for _, prm := range l.Params {
				if prm.Name == "ALG" && prm.Value != "" {
					alg = prm.Value
					break
				}
			}
			l = withoutPref(l)
		}
		lines = append(lines, l)
	}
	lines = append(lines, revkeyLine(keyB64, reason, when, alg))
	return render(lines)
}

// RotateKey makes a new key the preferred one and revokes the old one. The old
// key defaults to the current preferred key (PREF=1) when oldKeyB64 is empty.
func RotateKey(text, newKeyB64, oldKeyB64, reason string, when time.Time) (string, error) {
	if reason != "rotated" && reason != "superseded" {
		return "", errors.New("A rotation reason must be 'rotated' or 'superseded'")
	}
	if _, err := crypto.LoadPublicKeyB64DER(newKeyB64); err != nil {
		return "", err
	}
	if when.IsZero() {
		when = time.Now().UTC()
	}
	p := vcard.ParseVCard(text)
	if err := checkWellFormed(p); err != nil {
		return "", err
	}
	if hasKey(p, newKeyB64) {
		return "", errors.New("The new key is already listed as KEY")
	}
	if hasRevocation(p, newKeyB64) {
		return "", errors.New("The new key is revoked in the vCard and cannot be added again")
	}
	if oldKeyB64 == "" {
		var preferred []model.PublicKey
		for _, k := range p.Keys {
			if k.Pref != nil && *k.Pref == 1 {
				preferred = append(preferred, k)
			}
		}
		if len(preferred) != 1 {
			return "", errors.New("Cannot tell which key to rotate: the vCard must have exactly one " +
				"KEY with PREF=1 (or pass the old key explicitly). If every key is " +
				"revoked, use `dsi key add` to add a new one")
		}
		oldKeyB64 = preferred[0].KeyB64
	}
	if !hasKey(p, oldKeyB64) {
		return "", errors.New("The old key is not listed as KEY in the vCard")
	}
	if hasRevocation(p, oldKeyB64) {
		return "", errors.New("The old key is already revoked in the vCard")
	}

	var lines []model.RawLine
	lastKey := -1
	alg := "ed25519"
	for _, l := range lifecycleLines(p) {
		if *l.Name == "KEY" {
			if strutil.Strip(*l.RawValue) == oldKeyB64 {
				for _, prm := range l.Params {
					if prm.Name == "ALG" && prm.Value != "" {
						alg = prm.Value
						break
					}
				}
			}
			l = withoutPref(l)
			lastKey = len(lines)
		}
		lines = append(lines, l)
	}
	insertAt := lastKey + 1
	if lastKey < 0 {
		insertAt = len(lines)
	}
	one := int64(1)
	lines = insertLine(lines, insertAt, keyLine(newKeyB64, &one))
	lines = append(lines, revkeyLine(oldKeyB64, reason, when, alg))
	return render(lines)
}

// AddKey adds a KEY to the vCard without revoking anything. It works when the
// vCard has no key or when every key is revoked. With preferred the new key
// gets PREF=1 and the other keys lose PREF.
func AddKey(text, newKeyB64 string, preferred bool) (string, error) {
	if _, err := crypto.LoadPublicKeyB64DER(newKeyB64); err != nil {
		return "", err
	}
	p := vcard.ParseVCard(text)
	if err := checkWellFormed(p); err != nil {
		return "", err
	}
	if hasKey(p, newKeyB64) {
		return "", errors.New("The key is already listed as KEY")
	}
	if hasRevocation(p, newKeyB64) {
		return "", errors.New("The key is revoked in the vCard and cannot be added again")
	}

	var lines []model.RawLine
	lastKey := -1
	for _, l := range lifecycleLines(p) {
		if *l.Name == "KEY" {
			if preferred {
				l = withoutPref(l)
			}
			lastKey = len(lines)
		}
		lines = append(lines, l)
	}
	insertAt := lastKey + 1
	if lastKey < 0 {
		insertAt = len(lines)
	}
	var pref *int64
	if preferred {
		one := int64(1)
		pref = &one
	}
	lines = insertLine(lines, insertAt, keyLine(newKeyB64, pref))
	return render(lines)
}

func insertLine(lines []model.RawLine, at int, l model.RawLine) []model.RawLine {
	lines = append(lines, model.RawLine{})
	copy(lines[at+1:], lines[at:])
	lines[at] = l
	return lines
}
