package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNoArgsPrintsHelp(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("")
	h.expect(code, out, 0)
	contains(t, out, "DSI Tools")
}

func TestVersionFlag(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "--version")
	h.expect(code, out, 0)
	contains(t, out, "test")
}

func TestHelpListsSubapps(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "--help")
	h.expect(code, out, 0)
	contains(t, out, "DSI Tools", "vCard processing", "feeds processing", "connections processing", "related to keys")
	for name, text := range map[string]string{"vcard": "vCard processing", "feeds": "feeds processing",
		"connections": "connections processing", "key": "related to keys"} {
		code, out := h.run("", name, "--help")
		h.expect(code, out, 0)
		contains(t, out, text)
	}
	_, out = h.run("", "feeds", "--help")
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, " add ") {
			line = l
		}
	}
	if !strings.Contains(line, "Create a new feed") {
		t.Errorf("feeds add help line: %q", line)
	}
	notContains(t, out, "publish") // publishing moved out of the core
}

func TestParseHelpMentionsJSON(t *testing.T) {
	h := newHarness(t)
	_, out := h.run("", "vcard", "parse", "--help")
	contains(t, out, "JSON")
	notContains(t, out, "human-readable")
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"feeds", "new"}, {"nope"}, {"vcard", "nope"}} {
		code, out := h.run("", args...)
		h.expect(code, out, 2)
		contains(t, out, "No such command")
	}
}

func TestUnknownFlagIsAUsageError(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "key", "create", "--nope")
	h.expect(code, out, 2)
	contains(t, out, "Usage:", "unknown flag")
}

func TestDebugFlag(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "--debug", "--help")
	h.expect(code, out, 0)
	contains(t, out, "--debug")
}

// --- the generic error handler (cmd_error_handler) ---

func runWrapped(t *testing.T, fn func(*cobra.Command, []string) error, env ...string) (int, string) {
	t.Helper()
	h := newHarness(t)
	for i := 0; i+1 < len(env); i += 2 {
		t.Setenv(env[i], env[i+1])
	}
	err := h.env.run("boom", fn)(&cobra.Command{}, nil)
	var ee *ExitError
	var ue *UsageError
	code := 0
	switch {
	case errors.As(err, &ee):
		code = ee.Code
	case errors.As(err, &ue):
		code = 2
	}
	return code, h.out.String()
}

func TestGenericErrorHasNoStackByDefault(t *testing.T) {
	code, out := runWrapped(t, func(*cobra.Command, []string) error { return errors.New("kaput") })
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	contains(t, out, "❌ Command 'boom' failed: kaput")
	notContains(t, out, "goroutine")
}

func TestGenericErrorDebugPrintsStack(t *testing.T) {
	code, out := runWrapped(t, func(*cobra.Command, []string) error { return errors.New("kaput") }, "DSI_DEBUG", "1")
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	contains(t, out, "failed: kaput", "goroutine")
}

func TestDebugFalseValuesDisableTheStack(t *testing.T) {
	boom := func(*cobra.Command, []string) error { return errors.New("kaput") }
	for _, v := range []string{"0", "false", "FALSE", " "} {
		_, out := runWrapped(t, boom, "DSI_DEBUG", v)
		notContains(t, out, "goroutine")
	}
	_, out := runWrapped(t, boom, "DSIPY_DEBUG", "yes") // legacy name
	contains(t, out, "goroutine")
}

func TestExitAndUsageErrorsPropagate(t *testing.T) {
	code, out := runWrapped(t, func(*cobra.Command, []string) error { return exit(3) })
	if code != 3 || strings.Contains(out, "failed") {
		t.Errorf("exit error: %d %q", code, out)
	}
	code, out = runWrapped(t, func(*cobra.Command, []string) error { return usagef("bad usage") })
	if code != 2 || strings.Contains(out, "failed") {
		t.Errorf("usage error: %d %q", code, out)
	}
}

func TestPromptAbortOnEOF(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "feeds", "add", "-i")
	h.expect(code, out, 1)
	contains(t, out, "Aborted.")
	notContains(t, out, "failed")
}

func TestGroupsWithoutArgumentsShowHelpAndExit2(t *testing.T) {
	h := newHarness(t)
	for _, group := range []string{"vcard", "feeds", "connections", "key", "plugin"} {
		code, out := h.run("", group)
		h.expect(code, out, 2)
		contains(t, out, "Usage:")
	}
	code, out := h.run("")
	h.expect(code, out, 0) // the bare command only shows the help
}

func TestFileErrorsUsePythonWording(t *testing.T) {
	h := newHarness(t)
	code, out := h.run("", "key", "create", "--priv", "nodir/p.pem", "--pub", "nodir/q.pem")
	h.expect(code, out, 1)
	contains(t, out, "[Errno 2] No such file or directory: 'nodir/p.pem'")
	code, out = h.run("", "vcard", "parse", "/nonexistent/x.vcf")
	h.expect(code, out, 1)
	contains(t, out, "The specified path is not a file: /nonexistent/x.vcf")
}
