package feeds

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Desvelao/dsi-cli/internal/strutil"
	"github.com/Desvelao/dsi-cli/internal/testutil"
)

// copyPosts copies the golden posts to a temp dir.
func copyPosts(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "posts")
	src := filepath.Join(testutil.GoldenDir(), "feeds", "posts")
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

type goldenState struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Date        string            `json:"date"`
	Link        *string           `json:"link"`
	Image       *string           `json:"image"`
	Content     string            `json:"content"`
	ContentType string            `json:"content_type"`
	Metadata    map[string]string `json:"metadata"`
}

func toGolden(s *State) goldenState {
	opt := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	meta := map[string]string{}
	for _, k := range s.Metadata.Keys() {
		if k == "file_path" || k == "file_dir" {
			continue
		}
		meta[k] = s.Metadata.Value(k)
	}
	return goldenState{s.ID, s.Title, isoFormat(s.Date), opt(s.Link), opt(s.Image), s.Content, s.ContentType, meta}
}

func TestCollectGolden(t *testing.T) {
	states, err := Collect(copyPosts(t))
	if err != nil {
		t.Fatal(err)
	}
	var want []goldenState
	testutil.GoldenJSON(t, "feeds/collect.json", &want)
	got := make([]goldenState, len(states))
	for i, s := range states {
		got[i] = toGolden(s)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d states, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			g, _ := json.MarshalIndent(got[i], "", " ")
			w, _ := json.MarshalIndent(want[i], "", " ")
			t.Errorf("state %d (%s) differs\n--- got\n%s\n--- want\n%s", i, want[i].ID, g, w)
		}
	}
}

func TestBadPostsGolden(t *testing.T) {
	var want map[string]string
	testutil.GoldenJSON(t, "feeds/bad_posts.json", &want)
	dir := filepath.Join(testutil.GoldenDir(), "feeds", "bad")
	for name, msg := range want {
		_, err := ParseFile(filepath.Join(dir, name), dir)
		var fe *FeedFileError
		if err == nil {
			t.Errorf("%s: expected error", name)
			continue
		}
		if ok := asFeedFileError(err, &fe); !ok {
			t.Errorf("%s: not a FeedFileError: %v", name, err)
			continue
		}
		// the golden message names the file relative to the bad dir
		got := strings.ReplaceAll(err.Error(), filepath.Join(dir, name), name)
		if got != msg {
			t.Errorf("%s: got %q want %q", name, got, msg)
		}
	}
}

func asFeedFileError(err error, target **FeedFileError) bool {
	fe, ok := err.(*FeedFileError)
	if ok {
		*target = fe
	}
	return ok
}

type feedInputs struct {
	Vars      map[string]string `json:"vars"`
	BuildDate string            `json:"build_date"`
	Signer    string            `json:"signer"`
}

func buildGoldenFeed(t *testing.T, items []*State, sign bool) string {
	t.Helper()
	var in feedInputs
	testutil.GoldenJSON(t, "feeds/feed_inputs.json", &in)
	vars := strutil.NewOrderedMap()
	for _, k := range []string{"site", "custom"} { // generator's dict order
		vars.Set(k, in.Vars[k])
	}
	ApplyTemplates(items, vars)
	var signer *Signer
	if sign {
		signer = goldenSigner(t, in.Signer)
	}
	bd, _ := time.Parse("2006-01-02T15:04:05", in.BuildDate)
	out, err := BuildRSS("Alice's Feed", "https://alice.example", "A <test> feed & more", "Alice",
		"alice@alice.example", "en-US", bd, items, signer)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func goldenSigner(t *testing.T, name string) *Signer {
	t.Helper()
	var idx map[string]struct {
		Seed   string `json:"seed_hex"`
		Public string `json:"public_b64_der"`
	}
	testutil.GoldenJSON(t, "keys/index.json", &idx)
	seed := make([]byte, 32)
	for i := range seed {
		var b byte
		for _, h := range idx[name].Seed[2*i : 2*i+2] {
			b <<= 4
			if h >= 'a' {
				b |= byte(h-'a') + 10
			} else {
				b |= byte(h - '0')
			}
		}
		seed[i] = b
	}
	return &Signer{Key: ed25519.NewKeyFromSeed(seed), ID: idx[name].Public}
}

func TestBuildRSSGolden(t *testing.T) {
	for _, tc := range []struct {
		file  string
		sign  bool
		empty bool
	}{{"feeds/feed_unsigned.rss", false, false}, {"feeds/feed_signed.rss", true, false}, {"feeds/feed_empty.rss", false, true}} {
		states, err := Collect(copyPosts(t))
		if err != nil {
			t.Fatal(err)
		}
		if tc.empty {
			states = nil
		}
		got := buildGoldenFeed(t, states, tc.sign)
		if want := testutil.GoldenString(t, tc.file); got != want {
			t.Errorf("%s differs\n--- got\n%s\n--- want\n%s", tc.file, got, want)
		}
	}
}

func publicKeys(t *testing.T, names ...string) map[string]ed25519.PublicKey {
	keys := map[string]ed25519.PublicKey{}
	for _, n := range names {
		s := goldenSigner(t, n)
		keys[s.ID] = s.Key.Public().(ed25519.PublicKey)
	}
	return keys
}

func TestVerifyFeedItemsGolden(t *testing.T) {
	type golden struct {
		Title  string  `json:"title"`
		Guid   *string `json:"guid"`
		Status string  `json:"status"`
		Reason string  `json:"reason"`
	}
	cases := []struct {
		feed, golden string
		keys         map[string]ed25519.PublicKey
	}{
		{"feeds/feed_signed.rss", "feeds/verify_signed_alice.json", publicKeys(t, "alice")},
		{"feeds/feed_signed.rss", "feeds/verify_signed_nokeys.json", nil},
		{"feeds/feed_unsigned.rss", "feeds/verify_unsigned.json", publicKeys(t, "alice")},
		{"feeds/feed_tampered.rss", "feeds/verify_tampered.json", publicKeys(t, "alice")},
	}
	// "wrong key": alice's key id mapped to bob's key
	wrong := map[string]ed25519.PublicKey{goldenSigner(t, "alice").ID: goldenSigner(t, "bob").Key.Public().(ed25519.PublicKey)}
	cases = append(cases, struct {
		feed, golden string
		keys         map[string]ed25519.PublicKey
	}{"feeds/feed_signed.rss", "feeds/verify_signed_wrong_key.json", wrong})

	for _, c := range cases {
		res, err := VerifyFeedItems(testutil.GoldenString(t, c.feed), c.keys)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]golden, len(res))
		for i, r := range res {
			got[i] = golden{r.Title, r.Guid, r.Status, r.Reason}
		}
		var want []golden
		testutil.GoldenJSON(t, c.golden, &want)
		if !reflect.DeepEqual(got, want) {
			g, _ := json.MarshalIndent(got, "", " ")
			w, _ := json.MarshalIndent(want, "", " ")
			t.Errorf("%s\n--- got\n%s\n--- want\n%s", c.golden, g, w)
		}
	}
}

func TestVerifyRejectsDTDAndGarbage(t *testing.T) {
	for _, doc := range []string{
		`<!DOCTYPE rss><rss/>`, `<!ENTITY x "y">`, `<!doctype rss><rss/>`, `<!DocType rss><rss/>`,
		`<!entity x "y"><rss/>`, `<?xml version="1.0"?><!DOCTYPE rss [<!ENTITY a "b">]><rss>&a;</rss>`,
		`<rss/><!DOCTYPE rss>`, `<!ELEMENT rss ANY><rss/>`,
	} {
		if _, err := VerifyFeedItems(doc, nil); err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("%q: %v", doc, err)
		}
	}
	if _, err := VerifyFeedItems(`<rss><item></rss>`, nil); err == nil || !strings.HasPrefix(err.Error(), "Invalid XML:") {
		t.Errorf("malformed: %v", err)
	}
	legacy := `<rss><channel><item><title>t</title><signature keyId="k" alg="rsa">aa</signature></item></channel></rss>`
	res, err := VerifyFeedItems(legacy, nil)
	if err != nil || len(res) != 1 || res[0].Reason != "unsupported alg 'rsa'" {
		t.Errorf("%+v %v", res, err)
	}
}

func TestVerifyAcceptsDoctypeTextInCDATA(t *testing.T) {
	doc := `<rss><channel><item><title><![CDATA[<!DOCTYPE x> <!ENTITY y "z">]]></title></item><item><title>&lt;!DOCTYPE x&gt;</title></item></channel></rss>`
	res, err := VerifyFeedItems(doc, nil)
	if err != nil || len(res) != 2 || res[0].Status != StatusUnsigned || res[0].Title != `<!DOCTYPE x> <!ENTITY y "z">` {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestGenerateOPMLGolden(t *testing.T) {
	dir := filepath.Join(testutil.GoldenDir(), "opml", "in")
	var warnings []string
	files := []string{"alice.vcf", "bob.vcf", "nofeeds.vcf", "carol.vcf", "missing.vcf"}
	for i := range files {
		files[i] = filepath.Join(dir, files[i])
	}
	got, err := GenerateOPMLFromVCards(files, "", &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if want := testutil.GoldenString(t, "opml/feeds.opml"); got != want {
		t.Errorf("OPML differs\n got %s\nwant %s", got, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "No such file or directory") {
		t.Errorf("warnings: %v", warnings)
	}
	if _, err := GenerateOPMLFromVCards([]string{files[2]}, "", nil); err == nil {
		t.Error("cards without feeds must fail")
	}
}

// knownMarkdownDiffs are inputs where goldmark (CommonMark) legitimately differs
// from the reference Markdown renderer (original Markdown rules + "extra"); everything else must match.
var knownMarkdownDiffs = map[string]string{
	"*em* **strong** ***both*** `code` ~~strike~~":         "nesting order of ***x***",
	"> quote\n> more\n\n> second":                          "blockquotes separated by a blank line are merged by Markdown.pl",
	"- a\n- b\n    - nested\n- c":                          "newline before a nested list",
	"<https://auto.example> and <me@x.example>":            "mailto links are entity-obfuscated by the reference renderer",
	"<div>raw <b>html</b></div>\n\ntext":                   "blank line after a raw HTML block",
	"A & B < C > D &copy; &amp; &#169;":                    "named/numeric entities are kept by the reference renderer",
	"Footnote ref[^1].\n\n[^1]: The note.":                 "footnote markup",
	"*[HTML]: Hyper Text Markup Language\n\nThe HTML spec": "abbr extension not supported",
	"## Heading {#custom-id .cls}\n":                       "attr_list extension not supported",
	"<div markdown=\"1\">\n*inside*\n</div>":               "md_in_html extension not supported",
	"Tab\there":                                            "tabs are expanded to spaces by the reference renderer",
	"<!-- comment -->\n\ntext":                             "blank line after an HTML comment",
	"1) paren list\n2) two":                                "')' list markers are CommonMark only",
	"Hard  \nbreak\\\nbackslash":                           "backslash line breaks are CommonMark only",
	"#NoSpace":                                             "ATX headings without a space are headings in Markdown.pl",
	"text\n- list right after":                             "lists may interrupt a paragraph in CommonMark",
	"![alt *em*](x.png)":                                   "alt text is plain text in CommonMark",
}

func TestRenderMarkdownGolden(t *testing.T) {
	var cases []struct{ In, Out string }
	testutil.GoldenJSON(t, "feeds/markdown_cases.json", &cases)
	matched := 0
	for _, c := range cases {
		got, err := RenderMarkdown(c.In)
		if err != nil {
			t.Fatal(err)
		}
		if reason, known := knownMarkdownDiffs[c.In]; known {
			if got == c.Out {
				t.Errorf("%q now matches the reference: remove it from knownMarkdownDiffs (%s)", c.In, reason)
			}
			continue
		}
		if got != c.Out {
			t.Errorf("unexpected difference for %q\n  go:     %q\n  want:   %q", c.In, got, c.Out)
			continue
		}
		matched++
	}
	if matched < 30 {
		t.Errorf("only %d markdown cases match the reference", matched)
	}
	t.Logf("%d/%d markdown cases identical, %d known differences", matched, len(cases), len(knownMarkdownDiffs))
}

func TestFrontMatterAndUnquote(t *testing.T) {
	var cases []struct{ In, Out string }
	testutil.GoldenJSON(t, "feeds/unquote.json", &cases)
	for _, c := range cases {
		if got := unquote(c.In); got != c.Out {
			t.Errorf("unquote(%q) = %q, want %q", c.In, got, c.Out)
		}
	}
}

func TestParseFileMissingDate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "undated.md")
	if err := os.WriteFile(path, []byte("---\ntitle: T\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ParseFile(path, "")
	var fe *FeedFileError
	if !asFeedFileError(err, &fe) {
		t.Fatalf("want FeedFileError, got %v", err)
	}
	if !strings.Contains(fe.Error(), path) || !strings.Contains(fe.Error(), "missing date") {
		t.Errorf("error should name the file and the missing date: %v", fe)
	}
}

func writePost(t *testing.T, path, front string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\ndate: 2025-01-01\n"+front+"---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectDuplicateIDs(t *testing.T) {
	check := func(t *testing.T, dir, id string) {
		t.Helper()
		_, err := Collect(dir)
		var fe *FeedFileError
		if !errors.As(err, &fe) {
			t.Fatalf("want FeedFileError, got %v", err)
		}
		for _, want := range []string{"duplicate id '" + id + "'", "a", "b"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q lacks %q", err, want)
			}
		}
	}
	t.Run("explicit", func(t *testing.T) {
		dir := t.TempDir()
		writePost(t, filepath.Join(dir, "a.md"), "id: same\n")
		writePost(t, filepath.Join(dir, "b.md"), "id: same\n")
		check(t, dir, "same")
	})
	t.Run("slug collision", func(t *testing.T) {
		dir := t.TempDir()
		writePost(t, filepath.Join(dir, "a b.md"), "")
		writePost(t, filepath.Join(dir, "a-b.md"), "")
		_, err := Collect(dir)
		var fe *FeedFileError
		if !errors.As(err, &fe) || !strings.Contains(err.Error(), "a b.md") || !strings.Contains(err.Error(), "a-b.md") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestCollectUnreadableSubdir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	dir := t.TempDir()
	writePost(t, filepath.Join(dir, "ok.md"), "")
	sub := filepath.Join(dir, "locked")
	writePost(t, filepath.Join(sub, "hidden.md"), "")
	if err := os.Chmod(sub, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(sub, 0o755) })
	if _, err := Collect(dir); err == nil {
		t.Fatal("expected an error for an unreadable subdirectory")
	}
}

func writeOPMLCard(t *testing.T, dir, name, fn string, urls ...string) string {
	t.Helper()
	body := "BEGIN:VCARD\nVERSION:4.0\nFN:" + fn + "\n"
	for _, u := range urls {
		body += "X-FEED:" + u + "\n"
	}
	body += "END:VCARD\n"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOPMLTitleAndDedupe(t *testing.T) {
	dir := t.TempDir()
	a := writeOPMLCard(t, dir, "a.vcf", "Alice", "https://x.example/f.rss", "https://a.example/a.rss")
	b := writeOPMLCard(t, dir, "b.vcf", "Bob", "https://x.example/f.rss", "https://b.example/b.rss")

	got, err := GenerateOPMLFromVCards([]string{a, b}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<opml version=\"2.0\"><head><title>DSI connections</title></head><body>") {
		t.Errorf("header: %s", got)
	}
	if n := strings.Count(got, "https://x.example/f.rss"); n != 1 {
		t.Errorf("duplicate URL emitted %d times: %s", n, got)
	}
	if !strings.Contains(got, `text="Alice"`) || strings.Contains(got, `text="Bob" type="rss" xmlUrl="https://x.example/f.rss"`) {
		t.Errorf("first-seen must win: %s", got)
	}
	if strings.Index(got, "a.example") > strings.Index(got, "b.example") {
		t.Errorf("order not preserved: %s", got)
	}

	got, err = GenerateOPMLFromVCards([]string{a}, `Tom & "Jerry" <1>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<head><title>Tom &amp; &quot;Jerry&quot; &lt;1&gt;</title></head>") {
		t.Errorf("title not escaped: %s", got)
	}
}
