package feeds

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/Desvelao/dsi-cli/internal/strutil"
)

func TestCDATAWithTerminatorRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const html = "<p>a[b[0]]>c</p>"
	st := &State{ID: "1", Title: "T", Date: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		Content: html, ContentType: "html", Metadata: strutil.NewOrderedMap()}
	states := []*State{st}
	ApplyTemplates(states, nil)
	if got := StripCDATA(st.Content); got != html {
		t.Fatalf("StripCDATA = %q, want %q", got, html)
	}
	out, err := BuildRSS("t", "http://x", "d", "n", "e@x", "en", time.Unix(0, 0).UTC(),
		states, &Signer{Key: priv, ID: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "]]]]><![CDATA[>") {
		t.Fatalf("terminator not split:\n%s", out)
	}
	res, err := VerifyFeedItems(out, map[string]ed25519.PublicKey{"k1": pub})
	if err != nil {
		t.Fatalf("not well-formed: %v", err)
	}
	if len(res) != 1 || res[0].Status != StatusValid {
		t.Fatalf("verification failed: %+v", res)
	}
}
