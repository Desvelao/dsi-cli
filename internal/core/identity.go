package core

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Desvelao/dsipy/internal/model"
	"github.com/Desvelao/dsipy/internal/pyutil"
	"github.com/Desvelao/dsipy/internal/vcard"
)

// VCard is a parsed vCard with the place it was loaded from.
type VCard struct {
	Profile *model.Profile
	// Path is where the card was read from, or the suggested file name for
	// cards fetched from a URL.
	Path string
	// URL is set for cards fetched from a URL.
	URL string
}

// NewVCardFromText parses vCard text.
func NewVCardFromText(text string) *VCard {
	return &VCard{Profile: vcard.ParseVCard(text)}
}

// NewVCardFromPath reads and parses a vCard file.
func NewVCardFromPath(path string) (*VCard, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("The specified path is not a file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if msg, bad := Utf8DecodeError(data); bad {
		return nil, errors.New(msg)
	}
	return &VCard{Profile: vcard.ParseVCard(string(data)), Path: path}, nil
}

// NewVCardFromURL fetches and parses a vCard from a URL.
func (f *Fetcher) NewVCardFromURL(url string, allowHTTP, verifySource bool) (*VCard, error) {
	text, filename, err := f.FetchVCardFromURL(url, allowHTTP, verifySource)
	if err != nil {
		return nil, err
	}
	if !FileIsVCardName(filename) {
		filename += ".vcf"
	}
	return &VCard{Profile: vcard.ParseVCard(text), URL: url, Path: filename}, nil
}

// Parse parses text and replaces the profile.
func (v *VCard) Parse(text string) *model.Profile {
	v.Profile = vcard.ParseVCard(text)
	return v.Profile
}

// Build rebuilds the vCard content from the current profile.
func (v *VCard) Build() string { return vcard.BuildVCardFromRawLines(v.Profile) }

// AddLine inserts a line before END:VCARD, keeping the existing line endings.
func (v *VCard) AddLine(line string) error {
	if strings.ContainsAny(line, "\r\n") {
		return errors.New("A vCard line cannot contain line breaks")
	}
	content := v.Profile.Raw
	if content == "" {
		content = v.Build()
	}
	const end = "END:VCARD"
	if !strings.HasSuffix(pyutil.Strip(content), end) {
		return fmt.Errorf("Invalid vCard format: missing %s", end)
	}
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	pos := strings.LastIndex(content, end)
	v.Parse(content[:pos] + line + newline + content[pos:])
	return nil
}

// String returns the vCard text (the original text when it was parsed).
func (v *VCard) String() string {
	if v.Profile.Raw != "" {
		return v.Profile.Raw
	}
	return v.Build()
}

// ToFile saves the vCard byte for byte (no newline translation). An empty path
// uses the card's own Path.
func (v *VCard) ToFile(path string) error {
	if path == "" {
		path = v.Path
	}
	if path == "" {
		return errors.New("No path specified for saving the vCard.")
	}
	return os.WriteFile(path, []byte(v.String()), 0o644)
}

// ToJSON returns the profile as JSON, formatted like Python's json.dumps.
func (v *VCard) ToJSON() (string, error) { return pyutil.JSONDumps(v.Profile) }

// PreferredKey returns the preferred usable public key: the lowest PREF among
// ed25519 keys that are not revoked, or nil if none remains.
func (v *VCard) PreferredKey() *model.PublicKey {
	revoked := map[string]bool{}
	for _, r := range v.Profile.Revocations {
		revoked[r.KeyB64] = true
	}
	var best *model.PublicKey
	for i := range v.Profile.Keys {
		k := &v.Profile.Keys[i]
		if revoked[k.KeyB64] || k.Alg != "ed25519" {
			continue
		}
		if best == nil || prefLess(k.Pref, best.Pref) {
			best = k
		}
	}
	return best
}

func prefLess(a, b *int64) bool {
	if a == nil {
		return false
	}
	return b == nil || *a < *b
}

// HasEndorsementFor reports whether the card already endorses the given key.
func (v *VCard) HasEndorsementFor(endorseeKeyB64 string) bool {
	for _, e := range v.Profile.Endorsements {
		if e.EndorseeKeyB64 == endorseeKeyB64 {
			return true
		}
	}
	return false
}

// ClassifyInputs splits inputs into URLs (http:// or https://) and local paths.
func ClassifyInputs(inputs []string) (urls, paths []string) {
	for _, in := range inputs {
		if strings.HasPrefix(in, "http://") || strings.HasPrefix(in, "https://") {
			urls = append(urls, in)
		} else {
			paths = append(paths, in)
		}
	}
	return urls, paths
}
