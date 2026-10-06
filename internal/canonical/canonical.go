// Package canonical builds the canonical byte strings that are signed in DSI
// (RFC sections 3 and 4) and the deterministic normal form of a vCard.
package canonical

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Desvelao/dsi-cli/internal/model"
	"github.com/Desvelao/dsi-cli/internal/vcard"
)

// EndorsementString is "endorse:<BASE64_DER_KEY>".
func EndorsementString(endorseeKeyB64 string) []byte {
	return []byte("endorse:" + endorseeKeyB64)
}

// FeedString is "<pubDate>\n<title>\n<description_plain>".
func FeedString(pubDate, title, descriptionPlain string) []byte {
	return []byte(pubDate + "\n" + title + "\n" + descriptionPlain)
}

var propertyOrder = []string{
	"FN", "N", "NICKNAME", "PHOTO", "BDAY", "ANNIVERSARY", "GENDER", "ADR", "TEL",
	"EMAIL", "IMPP", "LANG", "KIND", "CATEGORIES", "NOTE", "URL", "SOURCE", "KEY",
	"REVKEY", "X-DSI-VERSION", "X-FEED", "X-SOCIAL", "X-ENDORSE",
}

func rankOf(name string) int {
	for i, n := range propertyOrder {
		if n == name {
			return i
		}
	}
	return len(propertyOrder)
}

// NormalizeVCard returns the deterministic form of a parsed vCard: CRLF line
// endings, unfolded lines, upper-case property and parameter names, parameters
// sorted by name, and properties in a fixed order (known properties first, then
// the rest by name; equal properties keep their original order, except KEY and
// REVKEY, which are sorted by value).
//
// It fails if the vCard has malformed lines.
func NormalizeVCard(p *model.Profile) (string, error) {
	if len(p.Errors) > 0 {
		return "", fmt.Errorf("Cannot normalize a vCard with malformed lines: %s", strings.Join(p.Errors, "; "))
	}
	type entry struct {
		rank  int
		name  string
		tie   string
		index int
		line  string
	}
	var entries []entry
	for index, raw := range p.RawLines {
		name := *raw.Name
		if name == "BEGIN" || name == "END" || name == "VERSION" {
			continue
		}
		rank := rankOf(name)
		e := entry{rank: rank, index: index}
		if rank == len(propertyOrder) {
			e.name = name
		}
		if name == "KEY" || name == "REVKEY" {
			e.tie = *raw.RawValue
		}
		params := append([]model.Param(nil), raw.Params...)
		// Python sorts (name, value) tuples: by name, then value.
		sort.SliceStable(params, func(i, j int) bool {
			if params[i].Name != params[j].Name {
				return params[i].Name < params[j].Name
			}
			return params[i].Value < params[j].Value
		})
		line, err := vcard.FormatProperty(name, params, *raw.RawValue, raw.Group)
		if err != nil {
			return "", err
		}
		e.line = line
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.name != b.name {
			return a.name < b.name
		}
		if a.tie != b.tie {
			return a.tie < b.tie
		}
		return a.index < b.index
	})

	version := "4.0"
	if p.Version != nil && *p.Version != "" {
		version = *p.Version
	}
	lines := []string{"BEGIN:VCARD", "VERSION:" + version}
	for _, e := range entries {
		lines = append(lines, e.line)
	}
	lines = append(lines, "END:VCARD")
	return strings.Join(lines, "\r\n") + "\r\n", nil
}
