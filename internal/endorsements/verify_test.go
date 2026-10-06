package endorsements

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Desvelao/dsipy/internal/testutil"
	"github.com/Desvelao/dsipy/internal/vcard"
)

type goldenResult struct {
	Endorsee string  `json:"endorsee"`
	Status   string  `json:"status"`
	Signer   *string `json:"signer"`
	Reason   string  `json:"reason"`
}

func TestVerifyEndorsementsGolden(t *testing.T) {
	names := testutil.GoldenNames(t, "vcards", ".verify.json")
	if len(names) < 30 {
		t.Fatalf("few cases: %d", len(names))
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			p := vcard.ParseVCard(testutil.GoldenString(t, "vcards/"+name+".vcf"))
			got := []goldenResult{}
			for _, r := range VerifyEndorsements(p) {
				got = append(got, goldenResult{r.Endorsement.EndorseeKeyB64, r.Status, r.SignerKeyB64, r.Reason})
			}
			var want []goldenResult
			testutil.GoldenJSON(t, "vcards/"+name+".verify.json", &want)
			if want == nil {
				want = []goldenResult{}
			}
			if !reflect.DeepEqual(got, want) {
				g, _ := json.MarshalIndent(got, "", " ")
				w, _ := json.MarshalIndent(want, "", " ")
				t.Errorf("got\n%s\nwant\n%s", g, w)
			}
		})
	}
}

func TestParseDSIDate(t *testing.T) {
	cases := map[string]bool{
		"20250101T000000Z":  true,
		"2025011T000000Z":   true, // strptime accepts single-digit day
		"202511T000000Z":    true,
		"20250101t000000z":  true,
		"20250230T000000Z":  false,
		"20250101T000060Z":  false,
		"20250101T000059Z":  true,
		"20250101T240000Z":  false,
		"00000101T000000Z":  false,
		"20250101T000000":   false,
		" 20250101T000000Z": false,
		"20240229T000000Z":  true,
		"20250229T000000Z":  false,
		"":                  false,
	}
	for in, want := range cases {
		v := in
		if _, ok := ParseDSIDate(&v); ok != want {
			t.Errorf("ParseDSIDate(%q) = %v, want %v", in, ok, want)
		}
	}
	if _, ok := ParseDSIDate(nil); ok {
		t.Error("nil must not parse")
	}
}
