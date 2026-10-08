package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/crypto"
	"github.com/Desvelao/dsi-cli/internal/feeds"
)

var fullOpts = []string{"--title", "T", "--link", "https://example.com/feed.rss", "--description", "D", "--author", "A", "--email", "a@example.com"}

func newBuildHarness(t *testing.T) *harness {
	h := newHarness(t)
	for _, p := range []struct{ name, day string }{{"a", "1"}, {"b", "2"}, {"c", "3"}} {
		h.write("feeds/"+p.name+".md", "---\ntitle: Post "+p.name+"\ndate: 2024-01-0"+p.day+"T00:00:00Z\n---\nBody "+p.name+"\n")
	}
	return h
}

func (h *harness) build(extra ...string) (int, string) {
	args := append([]string{"feeds", "build", "feeds", "-o", "out.rss"}, fullOpts...)
	return h.run("", append(args, extra...)...)
}

func TestBuildLimit(t *testing.T) {
	h := newBuildHarness(t)
	code, out := h.build("--limit", "2")
	h.expect(code, out, 0)
	xml := h.read("out.rss")
	if strings.Count(xml, "<item>") != 2 {
		t.Errorf("items: %d", strings.Count(xml, "<item>"))
	}
	contains(t, xml, "Post c", "Post b")
	notContains(t, xml, "Post a")

	h.build("--limit", "0")
	if strings.Count(h.read("out.rss"), "<item>") != 0 {
		t.Error("limit 0 must give an empty feed")
	}
	os.Remove("out.rss")
	code, out = h.build("--limit", "-1")
	h.expect(code, out, 2)
	if h.exists("out.rss") {
		t.Error("nothing written on usage errors")
	}
	h.build()
	if strings.Count(h.read("out.rss"), "<item>") != 3 {
		t.Error("no limit includes everything")
	}
}

func TestBuildVars(t *testing.T) {
	h := newBuildHarness(t)
	h.write("feeds/v.md", "---\ntitle: Hi {{ who }}\ndate: 2024-02-01T00:00:00Z\n---\n{{ site }} {{ who }}\n")
	h.write("vars.env", "# comment\n\nsite = https://file.example\nwho=file\n")
	code, out := h.build("--var-file", "vars.env", "--var", "who=cli", "--var", "x=a=b")
	h.expect(code, out, 0)
	xml := h.read("out.rss")
	contains(t, xml, "Hi cli", "https://file.example cli")
	notContains(t, xml, "{{ site }}")

	os.Remove("out.rss")
	code, out = h.build("--var", "foo")
	h.expect(code, out, 2)
	contains(t, out, "key=value")
	if h.exists("out.rss") {
		t.Error("nothing written on usage errors")
	}
	code, out = h.build("--var-file", "nope.env")
	h.expect(code, out, 1)
	contains(t, out, "Variable file not found")
}

func TestBuildHTMLContentIsWrappedInCDATA(t *testing.T) {
	h := newBuildHarness(t)
	h.write("feeds/h.md", "---\ntitle: Rich\ndate: 2024-03-01T00:00:00Z\nuse_html_content: true\n---\nSome **bold**\n")
	code, out := h.build()
	h.expect(code, out, 0)
	contains(t, h.read("out.rss"), "<![CDATA[<p>Some <strong>bold</strong></p>]]>")
}

func TestBuildSigningRoundTrip(t *testing.T) {
	h := newBuildHarness(t)
	priv, pub, b64, err := crypto.GenerateKeypair()
	_ = b64
	if err != nil {
		t.Fatal(err)
	}
	h.write("k.pem", string(priv))
	h.write("k.pub.pem", string(pub))
	code, out := h.build("--sign-priv", "k.pem", "--sign-pub", "k.pub.pem")
	h.expect(code, out, 0)
	if strings.Count(h.read("out.rss"), "<signature") != 3 {
		t.Error("every item must be signed")
	}
	code, out = h.run("", "feeds", "verify", "out.rss", "--pub", "k.pub.pem")
	h.expect(code, out, 0)
	contains(t, out, "3 item(s), 0 invalid", "✅ valid    Post a")

	h.write("out.rss", strings.Replace(h.read("out.rss"), "Body a", "Body X", 1))
	code, out = h.run("", "feeds", "verify", "out.rss", "--pub", "k.pub.pem")
	h.expect(code, out, 1)
	contains(t, out, "invalid", "bad signature", "1 invalid")
}

func TestBuildSigningNeedsBothKeys(t *testing.T) {
	h := newBuildHarness(t)
	priv, pub, _, _ := crypto.GenerateKeypair()
	h.write("k.pem", string(priv))
	h.write("k.pub.pem", string(pub))
	for _, opt := range [][]string{{"--sign-priv", "k.pem"}, {"--sign-pub", "k.pub.pem"}} {
		code, out := h.build(opt...)
		h.expect(code, out, 1)
		contains(t, out, "both --sign-priv and --sign-pub")
		if h.exists("out.rss") {
			t.Error("nothing written")
		}
	}
	h.build()
	notContains(t, h.read("out.rss"), "<signature")
}

func TestBuildSigningKeyNotFoundAndInlinePEM(t *testing.T) {
	h := newBuildHarness(t)
	priv, pub, _, _ := crypto.GenerateKeypair()
	h.write("pub.pem", string(pub))
	h.write("priv.pem", string(priv))
	code, out := h.build("--sign-priv", "nope.pem", "--sign-pub", "pub.pem")
	h.expect(code, out, 1)
	contains(t, out, "not found for --sign-priv")
	notContains(t, out, "MalformedFraming")
	code, out = h.build("--sign-priv", "priv.pem", "--sign-pub", "nope.pem")
	h.expect(code, out, 1)
	contains(t, out, "not found for --sign-pub")
	if h.exists("out.rss") {
		t.Error("nothing written")
	}
	// the PEM text itself (CI secrets)
	code, out = h.build("--sign-priv", string(priv), "--sign-pub", string(pub))
	h.expect(code, out, 0)
	contains(t, h.read("out.rss"), "<signature")
}

func TestBuildMissingDirectoryAndEmptyDirectory(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", append([]string{"feeds", "build", "nope", "-o", "out.rss"}, fullOpts...)...)
	h.expect(code, out, 1)
	contains(t, out, "Directory not found")
	if h.exists("out.rss") {
		t.Error("nothing written")
	}
	os.Mkdir("empty", 0o755)
	code, out = h.run("", append([]string{"feeds", "build", "empty", "-o", "out.rss"}, fullOpts...)...)
	h.expect(code, out, 0)
	notContains(t, h.read("out.rss"), "<item>")
}

func TestBuildChecksDirectoryBeforePrompts(t *testing.T) {
	h := newHarness(t)
	h.write("afile", "x")
	for name, msg := range map[string]string{"nope": "Directory not found: nope", "afile": "'afile' exists and is not a directory."} {
		code, out := h.run("T\nhttps://a\nd\nA\ne\n", "feeds", "build", name, "-i", "-o", "out.rss")
		h.expect(code, out, 1)
		contains(t, out, msg)
		notContains(t, out, "Provide the title")
		if h.exists("out.rss") {
			t.Error("nothing written")
		}
	}
}

func TestBuildRequiresOptionsUnlessInteractive(t *testing.T) {
	h := newBuildHarness(t)
	for _, missing := range []string{"title", "link", "description", "author", "email"} {
		var args []string
		for i := 0; i < len(fullOpts); i += 2 {
			if fullOpts[i] != "--"+missing {
				args = append(args, fullOpts[i], fullOpts[i+1])
			}
		}
		os.Remove("out.rss")
		code, out := h.run("", append([]string{"feeds", "build", "feeds", "-o", "out.rss"}, args...)...)
		h.expect(code, out, 1)
		contains(t, out, "The "+missing+" cannot be empty")
		if h.exists("out.rss") {
			t.Error("nothing written")
		}
	}
	code, out := h.run("", "feeds", "build", "feeds", "-o", "out.rss", "--title", "T", "--link", "https://a", "--description", "d", "--author", "A", "--email", "e", "-t", "T")
	h.expect(code, out, 0)
	contains(t, h.read("out.rss"), "<title>T</title>")
}

func TestBuildInteractivePromptsForMissingValue(t *testing.T) {
	h := newBuildHarness(t)
	args := []string{"feeds", "build", "feeds", "-o", "out.rss", "--link", "https://a", "--description", "d", "--author", "A", "--email", "e", "--interactive"}
	code, out := h.run("Prompted Title\n", args...)
	h.expect(code, out, 0)
	contains(t, out, "Provide the title for the RSS feed [My RSS Feed]:")
	contains(t, h.read("out.rss"), "Prompted Title")
	// an empty answer takes the default
	code, out = h.run("\n", args...)
	h.expect(code, out, 0)
	contains(t, h.read("out.rss"), "<title>My RSS Feed</title>")
}

func TestBuildWithoutOutputPrintsToStdout(t *testing.T) {
	h := newBuildHarness(t)
	code, out := h.run("", append([]string{"feeds", "build", "feeds", "-o", ""}, fullOpts...)...)
	h.expect(code, out, 0)
	contains(t, out, "<?xml", "</rss>")
}

func TestFeedsInit(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "feeds", "init", "feeds")
	h.expect(code, out, 0)
	contains(t, out, "Directory created: feeds", "dsi feeds build feeds")
	if names := listDir(t, "feeds"); len(names) != 1 || names[0] != "hello.md" {
		t.Errorf("files: %v", names)
	}
	contains(t, h.read("feeds/hello.md"), "title: Hello DSI")

	// running twice keeps edited files
	h.write("feeds/hello.md", "edited")
	code, out = h.run("", "feeds", "init", "feeds")
	h.expect(code, out, 0)
	contains(t, out, "already exists")
	if h.read("feeds/hello.md") != "edited" {
		t.Error("edited file was overwritten")
	}

	// the initialized directory builds
	h2 := newHarness(t)
	h2.run("", "feeds", "init")
	code, out = h2.run("", append([]string{"feeds", "build", "feeds", "-o", "out.rss"}, fullOpts...)...)
	h2.expect(code, out, 0)
	if strings.Count(h2.read("out.rss"), "<item>") != 1 {
		t.Error("expected one item")
	}
}

func TestFeedsInitVariants(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "feeds", "init", "a/b/feeds", "--no-sample")
	h.expect(code, out, 0)
	if names := listDir(t, "a/b/feeds"); len(names) != 1 || names[0] != ".gitkeep" {
		t.Errorf("files: %v", names)
	}
	os.MkdirAll("full", 0o755)
	h.write("full/mine.md", "x")
	h.run("", "feeds", "init", "full", "--no-sample")
	if names := listDir(t, "full"); len(names) != 1 || names[0] != "mine.md" {
		t.Errorf("non-empty directory changed: %v", names)
	}
	h.write("file", "x")
	code, out = h.run("", "feeds", "init", "file")
	h.expect(code, out, 1)
	if h.read("file") != "x" {
		t.Error("file was modified")
	}
	code, out = h.run("", "feeds", "init", "typed", "--type", "nope")
	h.expect(code, out, 1)
	if h.exists("typed") {
		t.Error("nothing created for an unsupported type")
	}
}

func TestFeedsAdd(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "feeds", "add", "--title", "T", "--message", "M", "--filename", filepath.Join(h.dir, "a.md"))
	h.expect(code, out, 0)
	contains(t, h.read("a.md"), "title: T", "date: 2026-03-01T12:00:00Z", "M\n")
	contains(t, out, "New post created", "Edit the file with a text editor")
	// parent folders are created
	code, out = h.run("", "feeds", "add", "-t", "T", "-m", "M", "-f", "new/dir/post.md")
	h.expect(code, out, 0)
	if !h.exists("new/dir/post.md") {
		t.Error("post not created")
	}
	// existing files are refused
	code, out = h.run("", "feeds", "add", "-t", "T", "-m", "M", "-f", "new/dir/post.md")
	h.expect(code, out, 1)
	contains(t, out, "already exists")
	// non-markdown names warn
	code, out = h.run("", "feeds", "add", "-t", "T", "-m", "M", "-f", "notes")
	h.expect(code, out, 0)
	contains(t, out, "does not end in .md")
	_, out = h.run("", "feeds", "add", "-t", "T", "-m", "M", "-f", "good.md")
	notContains(t, out, "does not end in .md")
	// missing values
	code, out = h.run("", "feeds", "add", "-f", "x.md")
	h.expect(code, out, 1)
	contains(t, out, "The title cannot be empty")
}

func TestFeedsAddInteractiveDefaults(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("\n\n\n", "feeds", "add", "-i")
	h.expect(code, out, 0)
	matches, _ := filepath.Glob("feeds/*.md")
	if len(matches) != 1 || matches[0] != "feeds/2026-03-01t12-00-00z.md" {
		t.Errorf("created: %v", matches)
	}
	contains(t, h.read(matches[0]), "title: My New Feed", "This is the content of my new feed item.")
}

func TestFeedsVerifyArguments(t *testing.T) {
	h := newHarness(t)
	h.write("f.rss", "<rss/>")
	code, out := h.run("", "feeds", "verify", "f.rss")
	h.expect(code, out, 1)
	contains(t, out, "Pass --vcard or --pub")
	code, out = h.run("", "feeds", "verify", "missing.rss", "--pub", "x")
	h.expect(code, out, 2)

	// verification keys from a vCard
	h2 := newBuildHarness(t)
	alice := keyNamed(t, "alice")
	h2.write("alice.pem", string(alice.privPEM(t)))
	h2.write("alice.pub", string(alice.pubPEM(t)))
	h2.write("alice.vcf", card("Alice", "https://alice.example/a.vcf", alice.b64))
	code, out = h2.build("--sign-priv", "alice.pem", "--sign-pub", "alice.pub")
	h2.expect(code, out, 0)
	code, out = h2.run("", "feeds", "verify", "out.rss", "--vcard", "alice.vcf")
	h2.expect(code, out, 0)
	contains(t, out, "3 item(s), 0 invalid")
	_ = feeds.StatusValid
}

func TestFeedsVerifyVCardRevokedKey(t *testing.T) {
	h := newBuildHarness(t)
	alice := keyNamed(t, "alice")
	bob := keyNamed(t, "bob")
	h.write("alice.pem", string(alice.privPEM(t)))
	h.write("alice.pub", string(alice.pubPEM(t)))
	h.write("bob.pem", string(bob.privPEM(t)))
	h.write("bob.pub", string(bob.pubPEM(t)))
	h.write("ok.vcf", card("A", "https://a.example/a.vcf", alice.b64))
	rev := func(reason string) string {
		return "REVKEY;TYPE=public;ALG=ed25519;REASON=" + reason + ";DATE=20260101T000000Z;ENCODING=b:" + alice.b64
	}
	h.write("rev.vcf", card("A", "https://a.example/a.vcf", bob.b64,
		"KEY;TYPE=public;ALG=ed25519;ENCODING=b:"+alice.b64, rev("compromised")))

	code, out := h.build("--sign-priv", "alice.pem", "--sign-pub", "alice.pub")
	h.expect(code, out, 0)
	code, out = h.run("", "feeds", "verify", "out.rss", "--vcard", "ok.vcf")
	h.expect(code, out, 0)

	// signed with a key later revoked as compromised
	code, out = h.run("", "feeds", "verify", "out.rss", "--vcard", "rev.vcf")
	h.expect(code, out, 1)
	contains(t, out, "signed with revoked key", "compromised", "3 invalid")

	// a revoked key does not stop a second valid key from validating
	code, out = h.build("--sign-priv", "bob.pem", "--sign-pub", "bob.pub")
	h.expect(code, out, 0)
	code, out = h.run("", "feeds", "verify", "out.rss", "--vcard", "rev.vcf")
	h.expect(code, out, 0)
	contains(t, out, "3 item(s), 0 invalid")
}

func TestFeedsInitSampleFlagsMutuallyExclusive(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "feeds", "init", "both", "--sample", "--no-sample")
	if code == 0 || !strings.Contains(out, "none of the others can be") {
		t.Errorf("expected mutual exclusion error, got code %d: %s", code, out)
	}
	if h.exists("both") {
		t.Error("directory must not be created")
	}
}
