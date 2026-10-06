package cli

import (
	"crypto/ed25519"
	"encoding/json"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/core"
	"github.com/Desvelao/dsi-cli/internal/crypto"
	"github.com/Desvelao/dsi-cli/internal/endorsements"
	"golang.org/x/image/font/gofont/goregular"
)

const sampleCard = "BEGIN:VCARD\nVERSION:4.0\nFN:Alice\nSOURCE:https://alice.example/dsi.vcf\nEND:VCARD\n"

// ---------------------------------------------------------------- create

func TestCreateNonInteractive(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "vcard", "create", "-o", "c.vcf", "--fn", "Alice, Ex", "--source", "https://x.example/dsi.vcf",
		"--note", "line1\nline2", "--n", "Doe;Jane", "--categories", "a,b")
	h.expect(code, out, 0)
	contains(t, out, "Summary of the vCard:", "vCard generated and saved to c.vcf")
	notContains(t, out, "No SOURCE")
	text := h.read("c.vcf")
	contains(t, text, `FN:Alice\, Ex`, "N:Doe;Jane;;;", "CATEGORIES:a,b", `NOTE;LANGUAGE=en-US:line1\nline2`, "LANG:en-US", "KIND:individual")
	if !strings.HasSuffix(text, "END:VCARD\r\n") {
		t.Error("vCards use CRLF")
	}
	code, out = h.run("", "vcard", "validate", "c.vcf")
	h.expect(code, out, 0)
}

func TestCreateWithoutSourceWarns(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "vcard", "create", "-o", "c.vcf", "--fn", "No Source")
	h.expect(code, out, 0)
	contains(t, out, "No SOURCE")
	if !h.exists("c.vcf") {
		t.Error("card not written")
	}
}

func TestCreateGenerateKey(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "vcard", "create", "-o", "c.vcf", "--fn", "A", "--source", "https://a.example/dsi.vcf", "--generate-key")
	h.expect(code, out, 0)
	contains(t, out, "Keypair generated and saved to 'vcard_private.pem' and 'vcard_public.pem'")
	priv, err := crypto.LoadPrivateKeyPEM([]byte(h.read("vcard_private.pem")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.read("c.vcf"), "KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:") {
		t.Error("no KEY in the card")
	}
	_ = priv
	// second run refuses without --force
	code, out = h.run("", "vcard", "create", "-o", "d.vcf", "--fn", "A", "--generate-key")
	h.expect(code, out, 1)
	contains(t, out, "use --force")
	if h.exists("d.vcf") {
		t.Error("card written despite the refusal")
	}
	code, out = h.run("", "vcard", "create", "-o", "d.vcf", "--fn", "A", "--generate-key", "--force")
	h.expect(code, out, 0)
}

func TestCreateRejectsLineBreakInjection(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "vcard", "create", "-o", "c.vcf", "--tel", "1\nFN:evil")
	h.expect(code, out, 1)
	contains(t, out, "Line breaks are not allowed in 'tel'")
	if h.exists("c.vcf") {
		t.Error("card must not be written")
	}
}

const (
	nFields = 17 // fn .. source prompts
	tail    = "n\nn\nn\nn\nn\n"
)

// answers builds the stdin of an interactive `vcard create` run.
func answers(fn, save string) string {
	lines := []string{fn}
	for i := 0; i < nFields-2; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, "https://example.com/card.vcf")
	return strings.Join(lines, "\n") + "\n" + tail + save + "\n"
}

func TestCreateInteractiveRemovesTempFile(t *testing.T) {
	h := newHarness(t)
	code, out := h.run(answers("Alice", "y"), "vcard", "create", "-i")
	h.expect(code, out, 0)
	contains(t, h.read("dsi-card.vcf"), "FN:Alice")
	if h.exists("vcard_create.tmp") || h.exists("vcard_create.tmp.part") {
		t.Error("temp files must be removed after saving")
	}
}

func TestCreateTempKeptOnCancelAndResumable(t *testing.T) {
	h := newHarness(t)
	code, out := h.run(answers("Alice", "n"), "vcard", "create", "-i")
	h.expect(code, out, 1)
	contains(t, out, "Operation canceled")
	var saved map[string]string
	if err := json.Unmarshal([]byte(h.read("vcard_create.tmp")), &saved); err != nil || saved["fn"] != "Alice" {
		t.Errorf("temp file: %v %v", saved, err)
	}
	if h.exists("dsi-card.vcf") {
		t.Error("cancelled card must not be written")
	}
	// resume: an empty answer keeps the saved value
	code, out = h.run(answers("", "y"), "vcard", "create", "-i", "--resume")
	h.expect(code, out, 0)
	contains(t, h.read("dsi-card.vcf"), "FN:Alice")
}

func TestCreateStaleTempIgnoredWithoutResume(t *testing.T) {
	h := newHarness(t)
	h.write("vcard_create.tmp", `{"fn": "Stale"}`)
	code, out := h.run(answers("", "y"), "vcard", "create", "-i")
	h.expect(code, out, 0)
	notContains(t, h.read("dsi-card.vcf"), "Stale")
	if h.exists("vcard_create.tmp") {
		t.Error("temp file must be removed")
	}
}

func TestCreateResumeUsesValuesAsDefaults(t *testing.T) {
	h := newHarness(t)
	h.write("vcard_create.tmp", `{"fn": "Resumed"}`)
	code, out := h.run(answers("", "y"), "vcard", "create", "-i", "--resume")
	h.expect(code, out, 0)
	contains(t, h.read("dsi-card.vcf"), "FN:Resumed")
}

func TestCreateValuesWithEqualsRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.run(answers("a=b", "n"), "vcard", "create", "-i")
	code, out := h.run(answers("", "y"), "vcard", "create", "-i", "--resume")
	h.expect(code, out, 0)
	contains(t, h.read("dsi-card.vcf"), "FN:a=b")
}

func TestCreateMalformedTempDoesNotCrash(t *testing.T) {
	for _, content := range []string{"garbage without equals\nfn=Legacy\n=x\n", "{not json", "[1, 2]", "\x00\n"} {
		h := newHarness(t)
		h.write("vcard_create.tmp", content)
		code, out := h.run(answers("", "y"), "vcard", "create", "-i", "--resume")
		h.expect(code, out, 0)
	}
}

func TestCreateLegacyTempLinesAreLoaded(t *testing.T) {
	h := newHarness(t)
	h.write("vcard_create.tmp", "junk\nfn=Legacy\n")
	code, out := h.run(answers("", "y"), "vcard", "create", "-i", "--resume")
	h.expect(code, out, 0)
	contains(t, h.read("dsi-card.vcf"), "FN:Legacy")
}

func TestCreateInteractiveFeedsSocialAndCustom(t *testing.T) {
	h := newHarness(t)
	lines := []string{"Alice"}
	for i := 0; i < nFields-2; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, "https://example.com/card.vcf",
		"y",                                   // create keys
		"y", "https://alice.example/feed.xml", // X-FEED
		"y", "es-ES", "https://alice.example/es.xml", "n", // X-FEED;LANGUAGE
		"y", "Mastodon", "@alice@example.social", "n", // social
		"y", "Pronouns", "she/her", "n", // custom
		"y", // save
	)
	code, out := h.run(strings.Join(lines, "\n")+"\n", "vcard", "create", "-i")
	h.expect(code, out, 0)
	text := h.read("dsi-card.vcf")
	contains(t, text, "X-FEED:https://alice.example/feed.xml", "X-FEED;LANGUAGE=es-ES:https://alice.example/es.xml",
		"X-SOCIAL;PLATFORM=mastodon:@alice@example.social", "X-PRONOUNS=:she/her", "KEY;TYPE=public")
	if !h.exists("vcard_private.pem") {
		t.Error("keys must be generated")
	}
}

// ---------------------------------------------------------------- parse

func TestParse(t *testing.T) {
	h := newHarness(t)
	h.write("a.vcf", sampleCard)
	for name, args := range map[string][]string{"file": {"vcard", "parse", "a.vcf"}, "dash": {"vcard", "parse", "-"}, "implicit stdin": {"vcard", "parse"}} {
		stdin := ""
		if name != "file" {
			stdin = sampleCard
		}
		code, out := h.run(stdin, args...)
		h.expect(code, out, 0)
		var v map[string]any
		if err := json.Unmarshal([]byte(out), &v); err != nil || v["fn"] != "Alice" {
			t.Errorf("%s: %v %q", name, err, out)
		}
	}
}

func TestParseWithoutInput(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"vcard", "parse"}, {"vcard", "parse", "-"}} {
		code, out := h.run("", args...)
		h.expect(code, out, 1)
		if strings.Count(out, "No input data provided") != 1 {
			t.Errorf("%v: %q", args, out)
		}
		notContains(t, out, "Failed to parse")
	}
	code, out := h.run("", "vcard", "parse", "/nonexistent/x.vcf")
	h.expect(code, out, 1)
	if strings.Count(out, "Failed to parse vCard") != 1 {
		t.Errorf("%q", out)
	}
	h.env.StdinTTY = true
	code, out = h.run("", "vcard", "parse")
	h.expect(code, out, 1)
	contains(t, out, "No input data provided")
}

// ---------------------------------------------------------------- endorse

type endorseFixture struct {
	h                *harness
	alice, bob       testKey
	bobCard, aliceVC string
}

func newEndorseFixture(t *testing.T) *endorseFixture {
	h := newHarness(t)
	f := &endorseFixture{h: h, alice: keyNamed(t, "alice"), bob: keyNamed(t, "bob")}
	h.write("alice.pem", string(f.alice.privPEM(t)))
	f.bobCard = h.write("bob.vcf", card("Bob", "https://bob.example/bob.vcf", f.bob.b64))
	f.aliceVC = h.write("alice.vcf", card("Alice", "https://alice.example/alice.vcf", f.alice.b64))
	return f
}

func (f *endorseFixture) endorse(args ...string) (int, string) {
	return f.h.run("", append([]string{"vcard", "endorse"}, append(args, "--priv", "alice.pem")...)...)
}

func TestEndorsePrintsAValidEndorsement(t *testing.T) {
	f := newEndorseFixture(t)
	code, out := f.endorse("bob.vcf")
	f.h.expect(code, out, 0)
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "X-ENDORSE") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no endorsement printed: %q", out)
	}
	sig := strings.Split(strings.Split(line, "SIG=")[1], ";")[0]
	if len(sig) != 128 || !crypto.VerifyEndorsementSignature(f.alice.priv.Public().(ed25519.PublicKey), f.bob.b64, sig) {
		t.Errorf("invalid signature in %q", line)
	}
	contains(t, line, "DATE=20260301T120000Z", "CONFIDENCE=medium", ":"+f.bob.b64)
}

func TestEndorseWriteRequiresDestinationAndAddsOnce(t *testing.T) {
	f := newEndorseFixture(t)
	code, out := f.endorse("bob.vcf", "--write")
	f.h.expect(code, out, 1)
	contains(t, out, "--vcard")

	for i, wantExists := range []bool{false, true} {
		code, out = f.endorse("bob.vcf", "--vcard", "alice.vcf", "--write")
		f.h.expect(code, out, 0)
		if strings.Contains(out, "already exists") != wantExists {
			t.Errorf("run %d: %q", i, out)
		}
		p := core.NewVCardFromText(f.h.read("alice.vcf")).Profile
		if len(p.Endorsements) != 1 || p.Endorsements[0].EndorseeKeyB64 != f.bob.b64 {
			t.Errorf("run %d: endorsements %+v", i, p.Endorsements)
		}
		if res := endorsements.VerifyEndorsements(p); len(res) != 1 || res[0].Status != endorsements.Valid {
			t.Errorf("endorsement does not verify: %+v", res)
		}
	}
	if strings.Contains(f.h.read("bob.vcf"), "X-ENDORSE") {
		t.Error("the endorsed card must not be modified")
	}
}

func TestEndorseFailures(t *testing.T) {
	f := newEndorseFixture(t)
	f.h.write("nokey.vcf", card("NoKey", "https://n.example/n.vcf", ""))
	code, out := f.endorse("nokey.vcf")
	f.h.expect(code, out, 1)
	contains(t, out, "No valid KEY entries found")

	f.h.write("bad.vcf", card("Bad", "https://b.example/b.vcf", "bm90LWEta2V5"))
	code, out = f.endorse("bad.vcf")
	f.h.expect(code, out, 1)
	notContains(t, out, "X-ENDORSE")

	f.h.write("broken.pem", "not a key")
	code, out = f.h.run("", "vcard", "endorse", "bob.vcf", "--priv", "broken.pem")
	f.h.expect(code, out, 1)
	contains(t, out, "Failed to load private key")

	code, out = f.h.run("", "vcard", "endorse", "bob.vcf", "--priv", "alice.pem", "-c", "huge")
	f.h.expect(code, out, 1)
	contains(t, out, "Invalid confidence level 'huge'")

	code, out = f.h.run("", "vcard", "endorse", "bob.vcf", "--priv", "missing.pem")
	f.h.expect(code, out, 2)
	code, out = f.h.run("", "vcard", "endorse", "bob.vcf")
	f.h.expect(code, out, 2)
}

func TestEndorseMissingInputs(t *testing.T) {
	f := newEndorseFixture(t)
	code, out := f.endorse("missing.vcf")
	f.h.expect(code, out, 1)
	contains(t, out, "No valid vCard files found")
	code, out = f.endorse("bob.vcf", "missing.vcf")
	f.h.expect(code, out, 0)
	contains(t, out, "X-ENDORSE", "Input path does not exist")
}

// ---------------------------------------------------------------- qr

func TestQRCommand(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "vcard", "qr", "hello world", "-o", "q.png")
	h.expect(code, out, 0)
	contains(t, out, "QR code generated!")
	f, err := os.Open("q.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if cfg, err := png.DecodeConfig(f); err != nil || cfg.Width < 100 {
		t.Errorf("png: %v %v", cfg, err)
	}
	h.write("c.vcf", "BEGIN:VCARD\nVERSION:4.0\nFN:A\nEND:VCARD\n")
	code, out = h.run("", "vcard", "qr", "c.vcf", "-o", "q2.png")
	h.expect(code, out, 0)
	code, out = h.run("piped data\n", "vcard", "qr", "-o", "q3.png")
	h.expect(code, out, 0)
}

func TestQRCommandCaptionsAndErrors(t *testing.T) {
	h := newHarness(t)
	h.write("font.ttf", string(goregular.TTF))
	h.run("", "vcard", "qr", "data", "-o", "plain.png")
	code, out := h.run("", "vcard", "qr", "data", "-o", "cap.png", "--caption-top", "Hi", "--font", "font.ttf")
	h.expect(code, out, 0)
	if h.read("cap.png") == h.read("plain.png") {
		t.Error("captions must change the image")
	}
	code, out = h.run("", "vcard", "qr", "data", "-o", "x.png", "--caption-top", "Hi", "--font", "nope.ttf")
	h.expect(code, out, 1)
	contains(t, out, "font file does not exist")
	if h.exists("x.png") {
		t.Error("no image on failure")
	}
	code, out = h.run("", "vcard", "qr", "data", "-o", "x.png", "-t", "Hi")
	h.expect(code, out, 1)
	contains(t, out, "font file must be specified")
	code, out = h.run("", "vcard", "qr", "data")
	h.expect(code, out, 1)
	contains(t, out, "Output file path is required")
	code, out = h.run("", "vcard", "qr", "data", "-o", "x.png", "-i", "nope.png")
	h.expect(code, out, 1)
	contains(t, out, "image file does not exist")
	code, out = h.run("", "vcard", "qr", "-o", "x.png")
	h.expect(code, out, 1)
	contains(t, out, "No input data provided")
}

// ---------------------------------------------------------------- validate / inspect / normalize / verify

func TestValidateCommand(t *testing.T) {
	h := newHarness(t)
	alice := keyNamed(t, "alice")
	h.write("ok.vcf", card("Alice", "https://alice.example/dsi.vcf", alice.b64))
	code, out := h.run("", "vcard", "validate", "ok.vcf", "--json")
	h.expect(code, out, 0)
	if strings.TrimSpace(out) != `{"valid": true, "errors": [], "warnings": []}` {
		t.Errorf("json: %q", out)
	}
	code, out = h.run("", "vcard", "validate", "ok.vcf")
	h.expect(code, out, 0)
	contains(t, out, "✅ Valid (0 warning(s))")

	h.write("bad.vcf", "BEGIN:VCARD\r\nEND:VCARD\r\n")
	code, out = h.run("", "vcard", "validate", "bad.vcf")
	h.expect(code, out, 1)
	contains(t, out, "[version-missing]", "[fn-missing]", "[source-missing]", "Invalid:")

	h.write("warn.vcf", card("W", "https://w.example/w.vcf", ""))
	code, out = h.run("", "vcard", "validate", "warn.vcf")
	h.expect(code, out, 0)
	contains(t, out, "[key-missing]")
	code, out = h.run("", "vcard", "validate", "warn.vcf", "--strict")
	h.expect(code, out, 1)
}

func TestValidateMissingFileAndStdin(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "vcard", "validate", "missing.vcf")
	h.expect(code, out, 1)
	contains(t, out, "Cannot load 'missing.vcf'")
	code, out = h.run("", "vcard", "validate", "missing.vcf", "--json")
	h.expect(code, out, 1)
	contains(t, out, `"code": "load"`)
	code, out = h.run(card("S", "https://s.example/s.vcf", keyNamed(t, "s").b64), "vcard", "validate", "-")
	h.expect(code, out, 0)
}

func TestInspectDoesNotModifyAndNormalize(t *testing.T) {
	h := newHarness(t)
	alice := keyNamed(t, "alice")
	messy := "BEGIN:VCARD\r\nVERSION:4.0\r\nSOURCE:https://a.example/a.vcf\r\nX-FEED;LANGUAGE=en-US:https://a.example/f.rss\r\nfn:Alice\r\n" +
		"KEY;pref=1;alg=ed25519:" + alice.b64 + "\r\nX-SOCIAL;PLATFORM=github:alice\r\nEND:VCARD\r\n"
	h.write("a.vcf", messy)
	code, out := h.run("", "vcard", "inspect", "a.vcf")
	h.expect(code, out, 0)
	contains(t, out, "Name:   Alice", "Preferred: ed25519", "Keys: 1", "https://a.example/f.rss (en-US)", "github: alice", "Endorsements")
	if h.read("a.vcf") != messy {
		t.Error("inspect must not modify the file")
	}
	code, out = h.run("", "vcard", "normalize", "a.vcf")
	h.expect(code, out, 0)
	if !strings.HasPrefix(out, "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Alice\r\n") || h.read("a.vcf") != messy {
		t.Errorf("normalize output: %q", out)
	}
	code, out = h.run("", "vcard", "normalize", "a.vcf", "--write")
	h.expect(code, out, 0)
	if !strings.HasPrefix(h.read("a.vcf"), "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Alice\r\n") {
		t.Error("--write must rewrite the file")
	}
	code, out = h.run(messy, "vcard", "normalize", "-", "--write")
	h.expect(code, out, 1)
	contains(t, out, "--write needs a local file")
	h.write("m.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nno colon\r\nEND:VCARD\r\n")
	code, out = h.run("", "vcard", "normalize", "m.vcf")
	h.expect(code, out, 1)
	contains(t, out, "malformed lines")
}

func TestVerifyCommand(t *testing.T) {
	h := newHarness(t)
	alice, bob := keyNamed(t, "alice"), keyNamed(t, "bob")
	h.write("none.vcf", card("A", "https://a.example/a.vcf", alice.b64))
	code, out := h.run("", "vcard", "verify", "none.vcf")
	h.expect(code, out, 0)
	contains(t, out, "No endorsements.")

	sig := crypto.SignEndorsement(alice.priv, bob.b64)
	good := "X-ENDORSE;SIG=" + sig + ";DATE=20250101T000000Z;ENCODING=b:" + bob.b64
	h.write("good.vcf", card("A", "https://a.example/a.vcf", alice.b64, good))
	code, out = h.run("", "vcard", "verify", "good.vcf")
	h.expect(code, out, 0)
	contains(t, out, "✅ valid    "+bob.b64)

	h.write("bad.vcf", card("A", "https://a.example/a.vcf", alice.b64, "X-ENDORSE;SIG="+strings.Repeat("0", 128)+";ENCODING=b:"+bob.b64))
	code, out = h.run("", "vcard", "verify", "bad.vcf")
	h.expect(code, out, 1)
	contains(t, out, "invalid   "+bob.b64, "signature does not match any key")
}
