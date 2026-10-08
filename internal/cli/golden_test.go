package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Desvelao/dsi-cli/internal/testutil"
)

// updateCLIGolden rewrites the testdata/golden/cli files from the current
// output. Opt-in: UPDATE_GOLDEN=1 go test ./internal/cli -run TestCLIGolden
// Review the diff by eye afterwards.
var updateCLIGolden = os.Getenv("UPDATE_GOLDEN") != ""

// cliCase freezes the stdout, stderr and exit code of one CLI invocation (and
// optionally files it wrote) under testdata/golden/cli/<name>.*.
type cliCase struct {
	name  string
	stdin string
	// copy maps a destination in the temp dir to a path under testdata/golden
	// (a file or a directory).
	copy map[string]string
	args []string
	// outputs are files (relative to the temp dir) frozen as <name>.<file>.
	outputs []string
}

var cliGoldenCases = []cliCase{
	{name: "validate_json_ok", copy: map[string]string{"c.vcf": "vcards/complete_valid.vcf"},
		args: []string{"vcard", "validate", "--json", "c.vcf"}},
	{name: "validate_json_bad_key", copy: map[string]string{"c.vcf": "vcards/bad_key_b64.vcf"},
		args: []string{"vcard", "validate", "--json", "c.vcf"}},
	{name: "validate_json_empty", copy: map[string]string{"c.vcf": "vcards/empty.vcf"},
		args: []string{"vcard", "validate", "--json", "c.vcf"}},
	{name: "feeds_build_signed", copy: map[string]string{"posts": "feeds/posts", "alice.pem": "keys/alice.priv.pem", "alice.pub": "keys/alice.pub.pem"},
		args: []string{"feeds", "build", "posts", "-o", "out.rss", "--limit", "10", "--title", "T",
			"--link", "https://example.com/feed.rss", "--description", "D", "--author", "A",
			"--email", "a@example.com", "--sign-priv", "alice.pem", "--sign-pub", "alice.pub"},
		outputs: []string{"out.rss"}},
	{name: "feeds_build_bad_posts", copy: map[string]string{"posts": "feeds/bad"},
		args: []string{"feeds", "build", "posts", "-o", "out.rss", "--limit", "10", "--title", "T",
			"--link", "https://example.com/feed.rss", "--description", "D", "--author", "A",
			"--email", "a@example.com"}},
	{name: "feeds_verify_signed", copy: map[string]string{"f.rss": "feeds/feed_signed.rss", "alice.pub": "keys/alice.pub.pem"},
		args: []string{"feeds", "verify", "f.rss", "--pub", "alice.pub"}},
	{name: "feeds_verify_tampered", copy: map[string]string{"f.rss": "feeds/feed_tampered.rss", "alice.pub": "keys/alice.pub.pem"},
		args: []string{"feeds", "verify", "f.rss", "--pub", "alice.pub"}},
	{name: "key_pub_encode", copy: map[string]string{"alice.pub": "keys/alice.pub.pem"},
		args: []string{"key", "pub-encode", "alice.pub"}},
	{name: "key_pub_encode_missing", args: []string{"key", "pub-encode", "nope.pem"}},
	{name: "key_pub_decode_stdin", stdin: "MCowBQYDK2VwAyEAOAiOTCroL1xFxoCKYaZJDTxhLOHaI1cURm/HSPvEy7s=\n",
		args: []string{"key", "pub-decode"}},
	{name: "key_pub_decode_invalid", args: []string{"key", "pub-decode", "!!!not-base64"}},
	{name: "key_add_public_key", copy: map[string]string{"c.vcf": "vcards/complete_valid.vcf"},
		args: []string{"key", "add", "c.vcf", "--public-key",
			"MCowBQYDK2VwAyEA3XVgQP3VFF4r+YMtJk3QgOSz5zAWvfZXS0zYfqppf14=", "-o", "out.vcf"},
		outputs: []string{"out.vcf"}},
}

func copyGolden(t *testing.T, dst, rel string) {
	t.Helper()
	src := filepath.Join(testutil.GoldenDir(), filepath.FromSlash(rel))
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, r)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCLIGolden(t *testing.T) {
	for _, c := range cliGoldenCases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			var stdout, stderr bytes.Buffer
			h.env.Out, h.env.Err = &stdout, &stderr
			h.env.In = strings.NewReader(c.stdin)
			h.env.Color = false
			h.env.Now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }
			for dst, rel := range c.copy {
				copyGolden(t, filepath.Join(h.dir, dst), rel)
			}
			code := ExecuteEnv("test", c.args, h.env)

			norm := func(s string) string { return strings.ReplaceAll(s, h.dir, "<TMP>") }
			got := map[string]string{
				c.name + ".stdout":   norm(stdout.String()),
				c.name + ".stderr":   norm(stderr.String()),
				c.name + ".exitcode": fmt.Sprintf("%d\n", code),
			}
			for _, o := range c.outputs {
				if !h.exists(o) {
					t.Fatalf("%s not written; exit %d\nstdout: %s\nstderr: %s", o, code, stdout.String(), stderr.String())
				}
				got[c.name+"."+o] = h.read(o)
			}
			for name, content := range got {
				path := filepath.Join(testutil.GoldenDir(), "cli", name)
				if updateCLIGolden {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if want := testutil.GoldenString(t, "cli/"+name); want != content {
					t.Errorf("%s differs\n--- want\n%s\n--- got\n%s", name, want, content)
				}
			}
		})
	}
}
