package feeds

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/Desvelao/dsi-cli/internal/strutil"
)

func TestStripIllegalXML(t *testing.T) {
	in := "a\x00b\x01\x08\x0b\x0c\x0e\x1f\tc\nd\re￾f￿g\xedhé"
	want := "ab\tc\nd\refg�hé"
	want = strings.Replace(want, "efg", "efg", 1)
	if got := stripIllegalXML(in); got != "ab\tc\nd\refg�hé" {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildRSSIllegalCharsSignedAndWellFormed(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, ct := range []string{"", "html"} {
		st := &State{ID: "1\x02", Title: "T\x00itle", Date: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			Content: "<p>bo\x0bdy\x1f</p>", ContentType: ct, Link: "http://x/\x01", Metadata: strutil.NewOrderedMap()}
		states := []*State{st}
		ApplyTemplates(states, nil)
		out, err := BuildRSS("t\x03", "http://x", "d", "n", "e@x", "en", time.Unix(0, 0).UTC(),
			states, &Signer{Key: priv, ID: "k1"})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range out {
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				t.Fatalf("illegal char %q in output", r)
			}
		}
		res, err := VerifyFeedItems(out, map[string]ed25519.PublicKey{"k1": pub})
		if err != nil {
			t.Fatalf("not well-formed: %v", err)
		}
		if len(res) != 1 || res[0].Status != StatusValid {
			t.Fatalf("verification failed: %+v", res)
		}
	}
}
