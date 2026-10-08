package core

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/strutil"
	"github.com/Desvelao/dsi-cli/internal/testutil"
	"github.com/Desvelao/dsi-cli/internal/vcard"
)

type goldenValidation struct {
	Valid         bool    `json:"valid"`
	Errors        []Issue `json:"errors"`
	Warnings      []Issue `json:"warnings"`
	ExpectedCrash string  `json:"crash_note"`
}

// cryptography appends version-specific ASN.1 details to deserialization errors.
func trimDetails(issues []Issue) []Issue {
	out := make([]Issue, len(issues))
	for i, is := range issues {
		if k := strings.Index(is.Message, " Details: "); k >= 0 {
			is.Message = is.Message[:k]
		}
		out[i] = is
	}
	return out
}

// The reference reports duplicates from an unordered set: compare them as sets.
func splitDuplicates(issues []Issue) (ordered, dups []Issue) {
	for _, is := range issues {
		if strings.HasSuffix(is.Code, "-duplicate") && is.Code != "version-duplicate" && is.Code != "source-duplicate" && is.Code != "dsi-version-duplicate" {
			dups = append(dups, is)
		} else {
			ordered = append(ordered, is)
		}
	}
	sort.Slice(dups, func(i, j int) bool { return dups[i].Message < dups[j].Message })
	return
}

func sameIssues(t *testing.T, label string, got, want []Issue) {
	t.Helper()
	got, want = trimDetails(got), trimDetails(want)
	go1, gd := splitDuplicates(got)
	wo, wd := splitDuplicates(want)
	if len(go1) == 0 {
		go1 = nil
	}
	if len(wo) == 0 {
		wo = nil
	}
	if !reflect.DeepEqual(go1, wo) || !reflect.DeepEqual(gd, wd) {
		g, _ := json.MarshalIndent(got, "", " ")
		w, _ := json.MarshalIndent(want, "", " ")
		t.Errorf("%s mismatch\n--- got\n%s\n--- want\n%s", label, g, w)
	}
}

func TestValidateProfileGolden(t *testing.T) {
	names := testutil.GoldenNames(t, "vcards", ".validate.json")
	if len(names) < 40 {
		t.Fatalf("few cases: %d", len(names))
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			var want goldenValidation
			testutil.GoldenJSON(t, "vcards/"+name+".validate.json", &want)
			res := ValidateProfile(vcard.ParseVCard(testutil.GoldenString(t, "vcards/"+name+".vcf")))
			if want.ExpectedCrash != "" {
				// The reference crashes on these inputs; Go reports an invalid URL instead.
				found := false
				for _, e := range res.Errors {
					found = found || strings.HasSuffix(e.Code, "-invalid")
				}
				if !found {
					t.Errorf("expected an *-invalid error for malformed URL, got %v", res.Errors)
				}
				return
			}
			if res.Valid() != want.Valid {
				t.Errorf("valid = %v, want %v", res.Valid(), want.Valid)
			}
			sameIssues(t, "errors", res.Errors, want.Errors)
			sameIssues(t, "warnings", res.Warnings, want.Warnings)
		})
	}
}

func TestValidationDictShape(t *testing.T) {
	res := ValidateProfile(vcard.ParseVCard(testutil.GoldenString(t, "vcards/complete_valid.vcf")))
	b, _ := json.Marshal(res.ToDict())
	if string(b) != `{"valid":true,"errors":[],"warnings":[]}` {
		t.Errorf("json shape: %s", b)
	}
}

func TestSplitURL(t *testing.T) {
	cases := []struct {
		in           string
		scheme, host string
		err          bool
	}{
		{"https://a.example/x?q#f", "https", "a.example", false},
		{"HTTP://A.example", "http", "A.example", false},
		{"//a.example/x", "", "a.example", false},
		{"mailto:a@b.example", "mailto", "", false},
		{"relative/path", "", "", false},
		{"1http://a.example", "", "", false},
		{"https://[::1/x", "https", "", true},
		{"https://[1.2.3.4]/x", "https", "", true},
		{"  \thttps://a.example", "https", "a.example", false},
		{"https://a.exa\nmple/", "https", "a.example", false},
	}
	for _, c := range cases {
		r, err := SplitURL(c.in)
		if (err != nil) != c.err {
			t.Errorf("%q: err = %v", c.in, err)
			continue
		}
		if err == nil && (r.Scheme != c.scheme || r.Netloc != c.host) {
			t.Errorf("%q: got %q %q, want %q %q", c.in, r.Scheme, r.Netloc, c.scheme, c.host)
		}
	}
}

func TestValidateJSONMatchesGolden(t *testing.T) {
	names := testutil.GoldenNames(t, "vcards", ".validate.out")
	if len(names) < 40 {
		t.Fatalf("few cases: %d", len(names))
	}
	for _, name := range names {
		res := ValidateProfile(vcard.ParseVCard(testutil.GoldenString(t, "vcards/"+name+".vcf")))
		got, err := strutil.JSONDumps(res.ToDict())
		if err != nil {
			t.Fatal(err)
		}
		want := testutil.GoldenString(t, "vcards/"+name+".validate.out")
		// set-ordered duplicate warnings and cryptography's version-specific details differ
		if strings.Contains(want, "-duplicate") || strings.Contains(want, "Details: ") {
			continue
		}
		if got != want {
			t.Errorf("%s: got %s\nwant %s", name, got, want)
		}
	}
}
