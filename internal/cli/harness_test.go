package cli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Desvelao/dsipy/internal/crypto"
)

// harness runs the CLI in a fresh temp directory, capturing stdout+stderr
// together (like click's CliRunner).
type harness struct {
	t   *testing.T
	env *Env
	out *bytes.Buffer
	dir string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("DSI_DEBUG", "")
	t.Setenv("DSIPY_DEBUG", "")
	out := &bytes.Buffer{}
	h := &harness{t: t, out: out, dir: dir}
	h.env = &Env{In: strings.NewReader(""), Out: out, Err: out,
		Now: func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }}
	return h
}

// run executes the CLI with the given stdin and returns the exit code and output.
func (h *harness) run(stdin string, args ...string) (int, string) {
	h.t.Helper()
	h.out.Reset()
	h.env.In = strings.NewReader(stdin)
	h.env.reader = nil
	code := ExecuteEnv("test", args, h.env)
	return code, h.out.String()
}

func (h *harness) write(name, content string) string {
	h.t.Helper()
	path := filepath.Join(h.dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return path
}

func (h *harness) read(name string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.dir, name))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

func (h *harness) exists(name string) bool {
	_, err := os.Stat(filepath.Join(h.dir, name))
	return err == nil
}

func (h *harness) expect(code int, out string, wantCode int) {
	h.t.Helper()
	if code != wantCode {
		h.t.Fatalf("exit code %d, want %d\noutput:\n%s", code, wantCode, out)
	}
}

func contains(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(out, p) {
			t.Errorf("output does not contain %q:\n%s", p, out)
		}
	}
}

func notContains(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(out, p) {
			t.Errorf("output must not contain %q:\n%s", p, out)
		}
	}
}

// testKey is a deterministic Ed25519 key (seed = name padded with zeros).
type testKey struct {
	priv ed25519.PrivateKey
	b64  string
}

func keyNamed(t *testing.T, name string) testKey {
	t.Helper()
	seed := make([]byte, 32)
	copy(seed, name)
	priv := ed25519.NewKeyFromSeed(seed)
	b64, err := crypto.PublicKeyToB64DER(priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	return testKey{priv, b64}
}

func (k testKey) privPEM(t *testing.T) []byte {
	der, err := x509.MarshalPKCS8PrivateKey(k.priv)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func (k testKey) pubPEM(t *testing.T) []byte {
	der, err := x509.MarshalPKIXPublicKey(k.priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// card builds a CRLF DSI vCard for tests.
func card(name, source, keyB64 string, extra ...string) string {
	lines := []string{"BEGIN:VCARD", "VERSION:4.0", "FN:" + name}
	if source != "" {
		lines = append(lines, "SOURCE:"+source)
	}
	if keyB64 != "" {
		lines = append(lines, "KEY;TYPE=public;ALG=ed25519;PREF=1;ENCODING=b:"+keyB64)
	}
	lines = append(lines, extra...)
	lines = append(lines, "END:VCARD")
	return strings.Join(lines, "\r\n") + "\r\n"
}
