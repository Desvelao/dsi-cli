package cli

import (
	"bytes"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Execute("test", args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestNoArgsPrintsHelp(t *testing.T) {
	code, out, _ := run(t)
	if code != 0 || !strings.Contains(out, "dsi") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestVersionFlag(t *testing.T) {
	code, out, _ := run(t, "--version")
	if code != 0 || !strings.Contains(out, "test") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}
