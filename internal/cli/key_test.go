package cli

import (
	"crypto/ed25519"
	"os"
	"strings"
	"testing"

	"github.com/Desvelao/dsi-cli/internal/crypto"
	"github.com/Desvelao/dsi-cli/internal/vcard"
)

func TestKeyCreateAndRefuseOverwrite(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "key", "create")
	h.expect(code, out, 0)
	contains(t, out, "Keypair generated", "Public key (Base64-encoded DER for vCard)")
	info, err := os.Stat("private.pem")
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode: %v %v", info, err)
	}
	before := h.read("private.pem")
	code, out = h.run("", "key", "create")
	h.expect(code, out, 1)
	contains(t, out, "'private.pem' already exists", "use --force")
	if h.read("private.pem") != before {
		t.Error("key was overwritten without --force")
	}
	code, out = h.run("", "key", "create", "--force")
	h.expect(code, out, 0)
	if h.read("private.pem") == before {
		t.Error("--force must overwrite")
	}
}

func TestKeyCreateRefusesWhenOnlyPublicExists(t *testing.T) {
	h := newHarness(t)
	h.write("public.pem", "keep")
	code, out := h.run("", "key", "create")
	h.expect(code, out, 1)
	if h.exists("private.pem") || h.read("public.pem") != "keep" {
		t.Error("nothing may be written or modified")
	}
}

func TestKeyPubEncodeDecodeRoundTrip(t *testing.T) {
	h := newHarness(t)
	alice := keyNamed(t, "alice")
	h.write("alice.pub", string(alice.pubPEM(t)))
	code, out := h.run("", "key", "pub-encode", "alice.pub")
	h.expect(code, out, 0)
	if strings.TrimSpace(out) != alice.b64 {
		t.Errorf("pub-encode printed %q", out)
	}
	code, out = h.run("", "key", "pub-decode", alice.b64)
	h.expect(code, out, 0)
	if !strings.Contains(out, "BEGIN PUBLIC KEY") {
		t.Errorf("pub-decode output: %q", out)
	}
	code, out = h.run(alice.b64+"\n", "key", "pub-decode")
	h.expect(code, out, 0)
	contains(t, out, "BEGIN PUBLIC KEY")
}

func TestKeyPubEncodeErrors(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "key", "pub-encode", "/nonexistent/key.pem")
	h.expect(code, out, 1)
	contains(t, out, "is not a file")
	h.write("junk.pem", "not a key")
	code, out = h.run("", "key", "pub-encode", "junk.pem")
	h.expect(code, out, 1)
	contains(t, out, "Command 'pub_encode' failed")
}

func TestKeyPubDecodeWithoutContent(t *testing.T) {
	h := newHarness(t)
	for _, in := range []string{"\n", "   \n", ""} {
		code, out := h.run(in, "key", "pub-decode")
		h.expect(code, out, 1)
		contains(t, out, "No content provided.")
	}
}

func TestKeyRotateCommand(t *testing.T) {
	h := newHarness(t)
	alice := keyNamed(t, "alice")
	h.write("a.vcf", card("Alice", "https://alice.example/dsi.vcf", alice.b64))
	code, out := h.run("", "key", "rotate", "a.vcf", "--priv", "new.pem", "--pub", "new.pub.pem")
	h.expect(code, out, 0)
	contains(t, out, "Key rotated in 'a.vcf'", "New private key: new.pem")
	p := vcard.ParseVCard(h.read("a.vcf"))
	if len(p.Keys) != 2 || len(p.Revocations) != 1 || p.Revocations[0].KeyB64 != alice.b64 {
		t.Fatalf("rotated card: keys %+v revocations %+v", p.Keys, p.Revocations)
	}
	if *p.Revocations[0].Date != "20260301T120000Z" || *p.Revocations[0].Reason != "rotated" {
		t.Errorf("revocation: %+v", p.Revocations[0])
	}
	// the new private key matches the preferred KEY
	priv, err := crypto.LoadPrivateKeyPEM([]byte(h.read("new.pem")))
	if err != nil {
		t.Fatal(err)
	}
	newB64, _ := crypto.PublicKeyToB64DER(priv.Public().(ed25519.PublicKey))
	var preferred string
	for _, k := range p.Keys {
		if k.Pref != nil && *k.Pref == 1 {
			preferred = k.KeyB64
		}
	}
	if preferred != newB64 {
		t.Error("the new key must be the preferred one")
	}
	if info, _ := os.Stat("new.pem"); info.Mode().Perm() != 0o600 {
		t.Errorf("private key mode %v", info.Mode().Perm())
	}
	// validates cleanly after rotation
	code, out = h.run("", "vcard", "validate", "a.vcf")
	h.expect(code, out, 0)
	contains(t, out, "Valid (0 warning(s))")
}

func TestKeyRotateRefusesToOverwriteAndKeepsFilesOnFailure(t *testing.T) {
	h := newHarness(t)
	alice := keyNamed(t, "alice")
	original := card("Alice", "https://alice.example/dsi.vcf", alice.b64)
	h.write("a.vcf", original)
	h.write("new.pem", "keep")
	code, out := h.run("", "key", "rotate", "a.vcf", "--priv", "new.pem", "--pub", "new.pub.pem")
	h.expect(code, out, 1)
	contains(t, out, "already exists")
	if h.read("a.vcf") != original || h.exists("new.pub.pem") {
		t.Error("nothing may be written")
	}
	// a rotation that fails (bad reason) must not leave key files behind
	code, out = h.run("", "key", "rotate", "a.vcf", "--priv", "x.pem", "--pub", "x.pub", "--reason", "compromised")
	h.expect(code, out, 1)
	if h.exists("x.pem") || h.exists("x.pub") || h.read("a.vcf") != original {
		t.Error("failed rotation left files behind")
	}
	code, out = h.run("", "key", "rotate", "missing.vcf")
	h.expect(code, out, 1)
	contains(t, out, "is not a file")
}

func TestKeyRotateOutputFileLeavesOriginal(t *testing.T) {
	h := newHarness(t)
	original := card("Alice", "https://alice.example/dsi.vcf", keyNamed(t, "alice").b64)
	h.write("a.vcf", original)
	code, out := h.run("", "key", "rotate", "a.vcf", "-o", "out.vcf", "--priv", "n.pem", "--pub", "n.pub")
	h.expect(code, out, 0)
	if h.read("a.vcf") != original || !strings.Contains(h.read("out.vcf"), "REVKEY") {
		t.Error("-o must write the result elsewhere")
	}
}

func TestKeyRevokeCommand(t *testing.T) {
	h := newHarness(t)
	alice := keyNamed(t, "alice")
	h.write("a.vcf", card("Alice", "https://alice.example/dsi.vcf", alice.b64))
	code, out := h.run("", "key", "revoke", "a.vcf", "--key", alice.b64, "--reason", "compromised")
	h.expect(code, out, 0)
	contains(t, out, "Key revoked (compromised)", "no usable key left", "dsi key add")
	p := vcard.ParseVCard(h.read("a.vcf"))
	if len(p.Revocations) != 1 || p.Keys[0].Pref != nil {
		t.Errorf("revoked card: %+v %+v", p.Revocations, p.Keys)
	}
	code, out = h.run("", "key", "revoke", "a.vcf", "--key", alice.b64, "--reason", "lost")
	h.expect(code, out, 1)
	contains(t, out, "already revoked")
}

func TestKeyRevokeByPublicKeyFileAndArguments(t *testing.T) {
	h := newHarness(t)
	alice := keyNamed(t, "alice")
	h.write("a.vcf", card("Alice", "https://alice.example/dsi.vcf", alice.b64))
	h.write("alice.pub", string(alice.pubPEM(t)))
	code, out := h.run("", "key", "revoke", "a.vcf", "--pub", "alice.pub", "--reason", "deprecated")
	h.expect(code, out, 0)
	notContains(t, out, "No key is preferred") // deprecated keys keep their preference semantics
	code, out = h.run("", "key", "revoke", "a.vcf", "--reason", "lost")
	h.expect(code, out, 1)
	contains(t, out, "exactly one of --key or --pub")
	code, out = h.run("", "key", "revoke", "a.vcf", "--key", "x", "--pub", "y", "--reason", "lost")
	h.expect(code, out, 1)
	code, out = h.run("", "key", "revoke", "a.vcf", "--key", alice.b64)
	h.expect(code, out, 2) // --reason is required
}

func TestKeyAddCommand(t *testing.T) {
	h := newHarness(t)
	alice, bob := keyNamed(t, "alice"), keyNamed(t, "bob")
	h.write("a.vcf", card("Alice", "https://alice.example/dsi.vcf", alice.b64))

	// existing public key, not preferred
	code, out := h.run("", "key", "add", "a.vcf", "--public-key", bob.b64, "--no-pref")
	h.expect(code, out, 0)
	p := vcard.ParseVCard(h.read("a.vcf"))
	if len(p.Keys) != 2 || *p.Keys[0].Pref != 1 || p.Keys[1].Pref != nil {
		t.Errorf("--no-pref: %+v", p.Keys)
	}
	if h.exists("private.pem") {
		t.Error("no keypair must be generated with --public-key")
	}
	// generated key becomes the preferred one and the others lose PREF
	code, out = h.run("", "key", "add", "a.vcf")
	h.expect(code, out, 0)
	contains(t, out, "Key added to 'a.vcf'", "New private key: private.pem")
	p = vcard.ParseVCard(h.read("a.vcf"))
	prefs := 0
	for _, k := range p.Keys {
		if k.Pref != nil {
			prefs++
		}
	}
	if len(p.Keys) != 3 || prefs != 1 || p.Keys[2].Pref == nil {
		t.Errorf("preferred add: %+v", p.Keys)
	}
	// refuses to overwrite key files
	code, out = h.run("", "key", "add", "a.vcf")
	h.expect(code, out, 1)
	contains(t, out, "already exists")
}

func TestRevokeAllThenRotateFailsAndAddWorks(t *testing.T) {
	h := newHarness(t)
	alice := keyNamed(t, "alice")
	h.write("a.vcf", card("Alice", "https://alice.example/dsi.vcf", alice.b64))
	h.run("", "key", "revoke", "a.vcf", "--key", alice.b64, "--reason", "lost")
	code, out := h.run("", "key", "rotate", "a.vcf", "--priv", "n.pem", "--pub", "n.pub")
	h.expect(code, out, 1)
	contains(t, out, "dsi key add")
	if h.exists("n.pem") {
		t.Error("failed rotate must not write keys")
	}
	code, out = h.run("", "key", "add", "a.vcf", "--priv", "n.pem", "--pub", "n.pub")
	h.expect(code, out, 0)
	code, out = h.run("", "vcard", "validate", "a.vcf")
	h.expect(code, out, 0)
}

func TestConnectionsFeed(t *testing.T) {
	h := newHarness(t)
	h.write("cards/good.vcf", "BEGIN:VCARD\nVERSION:4.0\nFN:Good\nX-FEED:https://e.com/good.xml\nEND:VCARD\n")
	h.write("cards/bad.vcf", "BEGIN:VCARD\nVERSION:4.0\nFN:Bad\nnot a valid line\nX-FEED:https://e.com/bad.xml\nEND:VCARD\n")
	code, out := h.run("", "connections", "feed", "cards", "-o", "sub/out.opml")
	h.expect(code, out, 0)
	contains(t, out, "OPML file generated", "Skipping malformed vCard")
	text := h.read("sub/out.opml")
	contains(t, text, "https://e.com/good.xml")
	notContains(t, text, "bad.xml")

	code, out = h.run("", "connections", "feed", "cards/good.vcf")
	h.expect(code, out, 0)
	contains(t, out, "<opml", "good.xml")
}

func TestConnectionsFeedFailures(t *testing.T) {
	h := newHarness(t)
	if err := os.Mkdir("empty", 0o755); err != nil {
		t.Fatal(err)
	}
	code, out := h.run("", "connections", "feed", "empty")
	h.expect(code, out, 1)
	contains(t, out, "No vCard files found")
	h.write("nofeeds.vcf", "BEGIN:VCARD\nVERSION:4.0\nFN:X\nEND:VCARD\n")
	code, out = h.run("", "connections", "feed", "nofeeds.vcf")
	h.expect(code, out, 1)
	contains(t, out, "No valid vCards with feed URLs")
}

func TestKeyAddPrefFlagsMutuallyExclusive(t *testing.T) {
	h := newHarness(t)
	alice, bob := keyNamed(t, "alice"), keyNamed(t, "bob")
	h.write("a.vcf", card("Alice", "https://alice.example/dsi.vcf", alice.b64))
	before := h.read("a.vcf")
	code, out := h.run("", "key", "add", "a.vcf", "--public-key", bob.b64, "--pref", "--no-pref")
	if code == 0 || !strings.Contains(out, "none of the others can be") {
		t.Errorf("expected mutual exclusion error, got code %d: %s", code, out)
	}
	if h.read("a.vcf") != before {
		t.Error("vCard must not change")
	}
}

func TestKeyRotateAndAddVCardWriteFailureLeavesNoKeys(t *testing.T) {
	for _, cmd := range []string{"rotate", "add"} {
		h := newHarness(t)
		original := card("Alice", "https://alice.example/dsi.vcf", keyNamed(t, "alice").b64)
		h.write("a.vcf", original)
		code, out := h.run("", "key", cmd, "a.vcf", "-o", "nodir/out.vcf", "--priv", "n.pem", "--pub", "n.pub")
		h.expect(code, out, 1)
		if h.exists("n.pem") || h.exists("n.pub") || h.read("a.vcf") != original {
			t.Errorf("%s: failed vCard write left key files or changed the vCard", cmd)
		}
		// a rerun is not blocked by leftovers
		code, out = h.run("", "key", cmd, "a.vcf", "--priv", "n.pem", "--pub", "n.pub")
		h.expect(code, out, 0)
	}
}

func TestWriteFileAtomicPreservesMode(t *testing.T) {
	p := t.TempDir() + "/f.vcf"
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(p, []byte("new")); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	b, _ := os.ReadFile(p)
	if string(b) != "new" || info.Mode().Perm() != 0o600 {
		t.Errorf("got %q %v", b, info.Mode())
	}
}
