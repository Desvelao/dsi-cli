// Package core holds the DSI domain logic: validation, key lifecycle, URL
// resolution and safe fetching.
package core

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify folds text to lowercase ASCII with single hyphens between words.
func Slugify(text string) string {
	var ascii strings.Builder
	for _, r := range norm.NFKD.String(text) {
		if r < 128 {
			ascii.WriteRune(r)
		}
	}
	s := nonSlug.ReplaceAllString(strings.ToLower(ascii.String()), "-")
	return strings.Trim(s, "-")
}
