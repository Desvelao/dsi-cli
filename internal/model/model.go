// Package model defines the parsed representation of a DSI vCard.
//
// Field order and JSON names are part of the output format, because
// `vcard parse` prints this structure as JSON.
package model

import (
	"bytes"
	"encoding/json"
)

// DSIDateFormat is the Go layout for the YYYYMMDDThhmmssZ dates used in DSI.
const DSIDateFormat = "20060102T150405Z"

// RevocationReasons are the accepted REVKEY REASON values.
var RevocationReasons = map[string]bool{
	"compromised": true, "rotated": true, "superseded": true,
	"retired": true, "lost": true, "deprecated": true,
}

type PublicKey struct {
	Alg    string `json:"alg"`
	KeyB64 string `json:"key_b64"`
	Pref   *int64 `json:"pref"`
}

type Endorsement struct {
	EndorseeKeyB64 string  `json:"endorsee_key_b64"`
	SignatureHex   string  `json:"signature_hex"`
	Date           *string `json:"date"`
	Confidence     *string `json:"confidence"`
}

type RevokedKey struct {
	KeyB64 string  `json:"key_b64"`
	Reason *string `json:"reason"`
	Date   *string `json:"date"`
}

type Feed struct {
	Language string `json:"language"`
	Category string `json:"category"`
	URL      string `json:"url"`
	Tags     string `json:"tags"`
}

type SocialIdentity struct {
	Platform string `json:"platform"`
	Value    string `json:"value"`
}

type DsiVersion struct {
	Revision string   `json:"revision"`
	Features []string `json:"features"`
}

// Param is one parsed parameter: NAME (upper-cased) and its values joined by ",".
type Param struct{ Name, Value string }

// MarshalJSON encodes a Param as the [name, value] pair.
func (p Param) MarshalJSON() ([]byte, error) { return json.Marshal([2]string{p.Name, p.Value}) }

// Attributes is an insertion-ordered {NAME: value} map (like an ordered dict).
type Attributes struct {
	keys []string
	vals map[string]string
}

// Set stores a value; a repeated name keeps its first position, last value wins.
func (a *Attributes) Set(name, value string) {
	if a.vals == nil {
		a.vals = map[string]string{}
	}
	if _, ok := a.vals[name]; !ok {
		a.keys = append(a.keys, name)
	}
	a.vals[name] = value
}

// Get returns the value and whether the parameter is present.
func (a Attributes) Get(name string) (string, bool) {
	v, ok := a.vals[name]
	return v, ok
}

// Value returns the parameter value, or "" when absent.
func (a Attributes) Value(name string) string { return a.vals[name] }

// Keys returns parameter names in insertion order.
func (a Attributes) Keys() []string { return a.keys }

func (a Attributes) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range a.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(a.vals[k])
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// RawLine is one logical content line with parsing metadata. Lines that failed
// to parse have only Line set (and no Group key in JSON).
type RawLine struct {
	Line      string
	AttrName  *string
	Value     *string
	Attrs     Attributes
	Name      *string
	Params    []Param
	RawValue  *string
	Group     *string
	Malformed bool
	// Edited marks lines created or changed by lifecycle operations; these are
	// re-rendered from Name/Params/RawValue instead of using Line.
	Edited bool
}

func (r RawLine) MarshalJSON() ([]byte, error) {
	params := r.Params
	if params == nil {
		params = []Param{}
	}
	type base struct {
		Line     string     `json:"line"`
		AttrName *string    `json:"attr_name"`
		Value    *string    `json:"value"`
		Attrs    Attributes `json:"attributes"`
		Name     *string    `json:"name"`
		Params   []Param    `json:"params"`
		RawValue *string    `json:"raw_value"`
	}
	b := base{r.Line, r.AttrName, r.Value, r.Attrs, r.Name, params, r.RawValue}
	if r.Malformed {
		return json.Marshal(b)
	}
	return json.Marshal(struct {
		base
		Group *string `json:"group"`
	}{b, r.Group})
}

// Profile is a parsed vCard.
type Profile struct {
	FN           *string          `json:"fn"`
	N            *string          `json:"n"`
	Nickname     *string          `json:"nickname"`
	Photo        *string          `json:"photo"`
	Lang         *string          `json:"lang"`
	Gender       *string          `json:"gender"`
	Email        *string          `json:"email"`
	Categories   *string          `json:"categories"`
	Bday         *string          `json:"bday"`
	Anniversary  *string          `json:"anniversary"`
	Kind         *string          `json:"kind"`
	Adr          *string          `json:"adr"`
	Tel          *string          `json:"tel"`
	Impp         *string          `json:"impp"`
	Note         *string          `json:"note"`
	URL          *string          `json:"url"`
	Source       *string          `json:"source"`
	Version      *string          `json:"version"`
	DsiVersion   *DsiVersion      `json:"dsi_version"`
	Keys         []PublicKey      `json:"keys"`
	Endorsements []Endorsement    `json:"endorsements"`
	Revocations  []RevokedKey     `json:"revocations"`
	Feeds        []Feed           `json:"feeds"`
	Social       []SocialIdentity `json:"social"`
	Errors       []string         `json:"errors"`
	Raw          string           `json:"raw"`
	RawLines     []RawLine        `json:"raw_lines"`
}

// NewProfile returns a Profile whose slices are non-nil (JSON [] not null).
func NewProfile(raw string) *Profile {
	return &Profile{
		Keys: []PublicKey{}, Endorsements: []Endorsement{}, Revocations: []RevokedKey{},
		Feeds: []Feed{}, Social: []SocialIdentity{}, Errors: []string{},
		RawLines: []RawLine{}, Raw: raw,
	}
}

// Str returns the value of an optional string, or "" when nil.
func Str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Ptr returns a pointer to s.
func Ptr(s string) *string { return &s }

// Field returns a pointer to the profile field for a lower-case main attribute
// name (fn, n, nickname, ...), or nil if the name is not a main attribute.
func (p *Profile) Field(name string) **string {
	switch name {
	case "fn":
		return &p.FN
	case "n":
		return &p.N
	case "nickname":
		return &p.Nickname
	case "photo":
		return &p.Photo
	case "lang":
		return &p.Lang
	case "gender":
		return &p.Gender
	case "email":
		return &p.Email
	case "categories":
		return &p.Categories
	case "bday":
		return &p.Bday
	case "anniversary":
		return &p.Anniversary
	case "kind":
		return &p.Kind
	case "adr":
		return &p.Adr
	case "tel":
		return &p.Tel
	case "impp":
		return &p.Impp
	case "note":
		return &p.Note
	case "url":
		return &p.URL
	case "source":
		return &p.Source
	}
	return nil
}

// MainAttribute describes a main vCard attribute offered by `vcard create`.
type MainAttribute struct {
	Name        string // lower-case option name
	Default     string
	HasDefault  bool // false: no default (unset)
	Description string
}

// MainAttributes are the main vCard attributes in option order.
var MainAttributes = []MainAttribute{
	{"fn", "", true, "Full Name (FN)"},
	{"n", "", true, "Name (N) in the format LastName;FirstName"},
	{"nickname", "", true, "Nickname (NICKNAME)"},
	{"lang", "en-US", true, "Language (LANG) in the format 'language-region' (e.g., 'en-US', 'es-ES')"},
	{"gender", "", true, "Gender (GENDER), e.g., 'M' for Male, 'F' for Female, or 'O' for Other"},
	{"email", "", true, "Email (EMAIL), e.g., 'example@mail.com'"},
	{"categories", "", true, "Categories (comma-separated, CATEGORIES), e.g., 'gamer,programmer'"},
	{"bday", "", true, "Birthday (BDAY) in the format YYYY-MM-DD"},
	{"anniversary", "", true, "Anniversary date (ANNIVERSARY) in the format YYYY-MM-DD"},
	{"kind", "individual", true, "Type of entity (KIND), e.g., 'individual' or 'org'"},
	{"adr", "", true, "Address (ADR) in the format ';;Street;City;State;PostalCode;Country'"},
	{"tel", "", true, "Telephone number (TEL), e.g., '+1234567890'"},
	{"impp", "", true, "Instant messaging protocol (IMPP), e.g., 'aim:exampleuser'"},
	{"photo", "", true, "URL to a photo (PHOTO), e.g., 'http://example.com/photo.jpg'"},
	{"note", "", true, "A short description about you (NOTE)"},
	{"url", "", true, "URL to public profile or personal web (URL), e.g., 'https://my.web.example.com/profile'"},
	{"source", "", false, "URL where the vCard will be hosted or can found (SOURCE)"},
}

// MainAttributeByName returns the attribute with the given name.
func MainAttributeByName(name string) MainAttribute {
	for _, a := range MainAttributes {
		if a.Name == name {
			return a
		}
	}
	return MainAttribute{Name: name}
}
