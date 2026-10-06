package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Desvelao/dsi-cli/internal/crypto"
	"github.com/Desvelao/dsi-cli/internal/endorsements"
	"github.com/Desvelao/dsi-cli/internal/model"
	"github.com/Desvelao/dsi-cli/internal/pyutil"
)

// Python's `$` also matches before a trailing newline, hence `\n?$`.
var (
	signatureRe = regexp.MustCompile(`^[0-9a-f]{128}\n?$`)
	languageRe  = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*\n?$`)
	tagRe       = regexp.MustCompile(`^[A-Za-z0-9]+(,[A-Za-z0-9]+)*\n?$`)
	platformRe  = regexp.MustCompile(`^[a-z]+\n?$`)
	revisionRe  = regexp.MustCompile(`^\p{Nd}{2}\n?$`)
	featureRe   = regexp.MustCompile(`^[A-Za-z0-9-]+\n?$`)
)

var confidenceLevels = map[string]bool{"low": true, "medium": true, "high": true}

// Issue is one validation finding.
type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidationResult is the outcome of validating a profile.
type ValidationResult struct {
	Errors   []Issue
	Warnings []Issue
}

// Valid reports whether there are no errors.
func (r *ValidationResult) Valid() bool { return len(r.Errors) == 0 }

func (r *ValidationResult) errorf(code, format string, args ...any) {
	r.Errors = append(r.Errors, Issue{code, fmt.Sprintf(format, args...)})
}

func (r *ValidationResult) warnf(code, format string, args ...any) {
	r.Warnings = append(r.Warnings, Issue{code, fmt.Sprintf(format, args...)})
}

// ToDict returns the {valid, errors, warnings} structure printed by `validate --json`.
func (r *ValidationResult) ToDict() ValidationDict {
	d := ValidationDict{Valid: r.Valid(), Errors: r.Errors, Warnings: r.Warnings}
	if d.Errors == nil {
		d.Errors = []Issue{}
	}
	if d.Warnings == nil {
		d.Warnings = []Issue{}
	}
	return d
}

// ValidationDict is the JSON form of a ValidationResult (field order matters).
type ValidationDict struct {
	Valid    bool    `json:"valid"`
	Errors   []Issue `json:"errors"`
	Warnings []Issue `json:"warnings"`
}

func checkURL(r *ValidationResult, code, label, value string, requireHTTP bool) {
	parts, err := SplitURL(value)
	if err != nil || parts.Scheme == "" || parts.Netloc == "" || HasSpace(value) {
		r.errorf(code+"-invalid", "%s is not an absolute URL: '%s'", label, value)
		return
	}
	if requireHTTP && parts.Scheme != "http" && parts.Scheme != "https" {
		r.errorf(code+"-scheme", "%s must use http or https, found '%s'", label, parts.Scheme)
	} else if parts.Scheme == "http" {
		r.warnf(code+"-http", "%s does not use HTTPS: %s", label, value)
	}
}

func countAttr(p *model.Profile, name string) int {
	n := 0
	for _, l := range p.RawLines {
		if l.AttrName != nil && *l.AttrName == name {
			n++
		}
	}
	return n
}

// duplicates returns the values that occur more than once, in order of their
// second occurrence (Python returns an unordered set).
func duplicates[T comparable](values []T) []T {
	seen := map[T]bool{}
	reported := map[T]bool{}
	var out []T
	for _, v := range values {
		if seen[v] && !reported[v] {
			out = append(out, v)
			reported[v] = true
		}
		seen[v] = true
	}
	return out
}

func prefix16(s string) string {
	r := []rune(s)
	if len(r) > 16 {
		r = r[:16]
	}
	return string(r)
}

func sortedReasons() string {
	var reasons []string
	for r := range model.RevocationReasons {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	return strings.Join(reasons, ", ")
}

// ValidateProfile validates a parsed vCard against the specification.
func ValidateProfile(p *model.Profile) *ValidationResult {
	r := &ValidationResult{}
	raw := p.Raw

	// --- structure -------------------------------------------------------
	stripped := pyutil.Strip(raw)
	if !strings.HasPrefix(stripped, "BEGIN:VCARD") || !strings.HasSuffix(stripped, "END:VCARD") {
		r.errorf("structure", "Content must start with BEGIN:VCARD and end with END:VCARD")
	}
	if strings.Contains(strings.ReplaceAll(raw, "\r\n", ""), "\n") {
		r.warnf("line-endings", "Lines are not terminated with CRLF (RFC 6350)")
	}
	for _, m := range p.Errors {
		r.errorf("malformed-line", "%s", m)
	}

	switch {
	case p.Version == nil:
		r.errorf("version-missing", "VERSION property is missing")
	case *p.Version != "4.0":
		r.errorf("version", "VERSION must be 4.0, found '%s'", *p.Version)
	}
	if countAttr(p, "version") > 1 {
		r.errorf("version-duplicate", "VERSION appears more than once")
	}
	if model.Str(p.FN) == "" {
		r.errorf("fn-missing", "FN property is missing or empty")
	}

	// --- SOURCE ----------------------------------------------------------
	if model.Str(p.Source) == "" {
		r.errorf("source-missing", "SOURCE property is required")
	} else {
		checkURL(r, "source", "SOURCE", *p.Source, true)
		if countAttr(p, "source") > 1 {
			r.warnf("source-duplicate", "SOURCE appears more than once")
		}
	}
	for _, c := range []struct {
		attr, label string
		value       *string
	}{{"url", "URL", p.URL}, {"photo", "PHOTO", p.Photo}} {
		if v := model.Str(c.value); v != "" && !strings.HasPrefix(v, "data:") {
			checkURL(r, c.attr, c.label, v, false)
		}
	}

	// --- keys ------------------------------------------------------------
	for i, key := range p.Keys {
		label := fmt.Sprintf("KEY #%d", i+1)
		if key.Alg != "ed25519" {
			alg := key.Alg
			if alg == "" {
				alg = "none"
			}
			r.errorf("key-alg", "%s: ALG must be ed25519, found '%s'", label, alg)
			continue
		}
		if _, err := crypto.LoadPublicKeyB64DER(key.KeyB64); err != nil {
			r.errorf("key-invalid", "%s: %s", label, err)
		}
	}
	if len(p.Keys) == 0 {
		r.warnf("key-missing", "No KEY: endorsements and feed signatures cannot be verified")
	}
	keyValues := make([]string, len(p.Keys))
	for i, k := range p.Keys {
		keyValues[i] = k.KeyB64
	}
	for _, d := range duplicates(keyValues) {
		r.warnf("key-duplicate", "KEY appears more than once: %s...", prefix16(d))
	}
	var preferred []model.PublicKey
	for _, k := range p.Keys {
		if k.Pref != nil && *k.Pref == 1 {
			preferred = append(preferred, k)
		}
	}
	if len(preferred) > 1 {
		r.errorf("key-pref", "More than one KEY is marked PREF=1")
	} else if len(p.Keys) > 0 && len(preferred) == 0 {
		r.warnf("key-pref-missing", "No KEY is marked PREF=1")
	}

	// --- revoked keys ----------------------------------------------------
	revoked := map[string]bool{}
	for i, rev := range p.Revocations {
		label := fmt.Sprintf("REVKEY #%d", i+1)
		revoked[rev.KeyB64] = true
		if _, err := crypto.LoadPublicKeyB64DER(rev.KeyB64); err != nil {
			r.errorf("revkey-invalid", "%s: %s", label, err)
		}
		switch {
		case rev.Reason == nil:
			r.warnf("revkey-reason-missing", "%s: REASON is missing", label)
		case !model.RevocationReasons[*rev.Reason]:
			r.errorf("revkey-reason", "%s: unknown REASON '%s' (expected one of %s)", label, *rev.Reason, sortedReasons())
		}
		if rev.Date == nil {
			r.warnf("revkey-date-missing", "%s: DATE is missing", label)
		} else if _, ok := endorsements.ParseDSIDate(rev.Date); !ok {
			r.errorf("revkey-date", "%s: DATE must use YYYYMMDDThhmmssZ, found '%s'", label, *rev.Date)
		}
	}
	for _, k := range preferred {
		if revoked[k.KeyB64] {
			r.errorf("key-revoked", "The preferred KEY is also listed as revoked")
		}
	}

	// --- endorsements ----------------------------------------------------
	for i, e := range p.Endorsements {
		label := fmt.Sprintf("X-ENDORSE #%d", i+1)
		if !signatureRe.MatchString(e.SignatureHex) {
			r.errorf("endorse-sig-format", "%s: SIG must be 128 lowercase hexadecimal characters", label)
		}
		if d := model.Str(e.Date); d != "" {
			if _, ok := endorsements.ParseDSIDate(e.Date); !ok {
				r.errorf("endorse-date", "%s: DATE must use YYYYMMDDThhmmssZ, found '%s'", label, d)
			}
		}
		if c := model.Str(e.Confidence); c != "" && !confidenceLevels[c] {
			r.errorf("endorse-confidence", "%s: CONFIDENCE must be low, medium or high", label)
		}
	}
	endorsed := make([]string, len(p.Endorsements))
	for i, e := range p.Endorsements {
		endorsed[i] = e.EndorseeKeyB64
	}
	for _, d := range duplicates(endorsed) {
		r.warnf("endorse-duplicate", "Key endorsed more than once: %s...", prefix16(d))
	}
	for i, out := range endorsements.VerifyEndorsements(p) {
		label := fmt.Sprintf("X-ENDORSE #%d", i+1)
		if out.Status == endorsements.Invalid && signatureRe.MatchString(out.Endorsement.SignatureHex) {
			r.errorf("endorse-signature", "%s: %s", label, out.Reason)
		} else if out.Status == endorsements.Unverifiable {
			r.warnf("endorse-unverifiable", "%s: %s", label, out.Reason)
		}
	}

	// --- feeds -----------------------------------------------------------
	for i, f := range p.Feeds {
		label := fmt.Sprintf("X-FEED #%d", i+1)
		checkURL(r, "feed", label, f.URL, true)
		if f.Language != "" && !languageRe.MatchString(f.Language) {
			r.errorf("feed-language", "%s: LANGUAGE is not a BCP 47 tag: '%s'", label, f.Language)
		}
		if f.Tags != "" && !tagRe.MatchString(f.Tags) {
			r.errorf("feed-tags", "%s: TAGS must be comma-separated alphanumerics", label)
		}
	}
	feedURLs := make([]string, len(p.Feeds))
	for i, f := range p.Feeds {
		feedURLs[i] = f.URL
	}
	for _, d := range duplicates(feedURLs) {
		r.warnf("feed-duplicate", "Feed URL appears more than once: %s", d)
	}

	// --- social identifiers ----------------------------------------------
	for i, s := range p.Social {
		label := fmt.Sprintf("X-SOCIAL #%d", i+1)
		if !platformRe.MatchString(s.Platform) {
			r.errorf("social-platform", "%s: PLATFORM must be lowercase letters a-z, found '%s'", label, s.Platform)
		}
		if s.Value == "" {
			r.errorf("social-value", "%s: value is empty", label)
		}
	}
	socials := make([][2]string, len(p.Social))
	for i, s := range p.Social {
		socials[i] = [2]string{s.Platform, s.Value}
	}
	for _, d := range duplicates(socials) {
		r.warnf("social-duplicate", "Duplicate X-SOCIAL: %s:%s", d[0], d[1])
	}

	// --- X-DSI-VERSION ---------------------------------------------------
	if v := p.DsiVersion; v != nil {
		if !revisionRe.MatchString(v.Revision) {
			r.errorf("dsi-version", "X-DSI-VERSION must be a two-digit revision, found '%s'", v.Revision)
		}
		for _, f := range v.Features {
			if !featureRe.MatchString(f) {
				r.errorf("dsi-version-feature", "Invalid FEATURES token '%s'", f)
			}
		}
	}
	if countAttr(p, "x-dsi-version") > 1 {
		r.warnf("dsi-version-duplicate", "X-DSI-VERSION appears more than once")
	}
	return r
}
