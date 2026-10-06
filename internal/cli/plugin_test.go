package cli

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Desvelao/dsipy/internal/plugin"
)

// pluginHarness installs fake plugins (shell scripts) in a temp plugin dir.
func pluginHarness(t *testing.T) (*harness, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("plugins are shell scripts")
	}
	h := newHarness(t)
	dir := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.env.Plugins = &plugin.Finder{Dirs: []string{dir}}
	return h, dir
}

func installPlugin(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, "dsi-"+name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const helloPlugin = `echo "args:$*"
echo "n=$#"
echo "version=$DSI_VERSION debug=${DSI_DEBUG:-unset} bin=${DSI_BIN:+set}"
if [ "$1" = "--stdin" ]; then cat; fi
if [ "$1" = "--fail" ]; then echo "oops" >&2; exit 7; fi
`

func TestPluginReceivesArgumentsAndEnvironment(t *testing.T) {
	h, dir := pluginHarness(t)
	installPlugin(t, dir, "hello", helloPlugin)
	code, out := h.run("", "hello", "a", "--flag", "b c", "-x")
	h.expect(code, out, 0)
	contains(t, out, "args:a --flag b c -x", "n=4", "version=test", "bin=set")
	notContains(t, out, "Usage:")

	// no arguments at all
	_, out = h.run("", "hello")
	contains(t, out, "n=0")
}

func TestPluginStdinStdoutStderrAndExitCode(t *testing.T) {
	h, dir := pluginHarness(t)
	installPlugin(t, dir, "hello", helloPlugin)
	code, out := h.run("piped input\n", "hello", "--stdin")
	h.expect(code, out, 0)
	contains(t, out, "piped input")

	code, out = h.run("", "hello", "--fail")
	h.expect(code, out, 7)
	contains(t, out, "oops")
	notContains(t, out, "failed") // the exit code is propagated without a wrapper message
}

func TestPluginDebugFlagBeforeNameIsForwarded(t *testing.T) {
	h, dir := pluginHarness(t)
	installPlugin(t, dir, "hello", helloPlugin)
	_, out := h.run("", "hello")
	contains(t, out, "debug=unset")
	_, out = h.run("", "--debug", "hello", "x")
	contains(t, out, "debug=1", "args:x")
	t.Setenv("DSI_DEBUG", "1")
	_, out = h.run("", "hello")
	contains(t, out, "debug=1")
}

func TestPluginsCannotShadowCoreCommands(t *testing.T) {
	h, dir := pluginHarness(t)
	installPlugin(t, dir, "key", `echo "plugin ran"`)
	installPlugin(t, dir, "help", `echo "plugin ran"`)
	installPlugin(t, dir, "plugin", `echo "plugin ran"`)
	for _, args := range [][]string{{"key", "--help"}, {"help"}, {"plugin", "list"}} {
		code, out := h.run("", args...)
		h.expect(code, out, 0)
		notContains(t, out, "plugin ran")
	}
	_, out := h.run("", "plugin", "list")
	contains(t, out, "key", "shadowed by the core command")
}

func TestUnknownCommandWithoutPluginStaysAUsageError(t *testing.T) {
	h, dir := pluginHarness(t)
	installPlugin(t, dir, "hello", helloPlugin)
	code, out := h.run("", "nope")
	h.expect(code, out, 2)
	contains(t, out, "No such command 'nope'")
}

func TestPluginNamesCannotEscapeTheDirectory(t *testing.T) {
	h, dir := pluginHarness(t)
	outside := filepath.Join(filepath.Dir(dir), "dsi-evil")
	os.WriteFile(outside, []byte("#!/bin/sh\necho evil ran\n"), 0o755)
	for _, name := range []string{"../evil", "./hello", "/bin/sh"} {
		code, out := h.run("", name)
		h.expect(code, out, 2)
		notContains(t, out, "evil ran")
	}
}

func TestNonExecutableAndBrokenPlugins(t *testing.T) {
	h, dir := pluginHarness(t)
	path := installPlugin(t, dir, "noexec", `echo ran`)
	os.Chmod(path, 0o644)
	code, out := h.run("", "noexec")
	h.expect(code, out, 2)

	os.WriteFile(filepath.Join(dir, "dsi-broken"), []byte("#!/nonexistent/interpreter\n"), 0o755)
	code, out = h.run("", "broken")
	h.expect(code, out, 1)
	contains(t, out, "Cannot run plugin")
}

func TestPluginList(t *testing.T) {
	h, dir := pluginHarness(t)
	code, out := h.run("", "plugin", "list")
	h.expect(code, out, 0)
	contains(t, out, "No plugins installed.", "dsi-<name>")

	hello := installPlugin(t, dir, "hello", helloPlugin)
	zed := installPlugin(t, dir, "zed", `echo z`)
	code, out = h.run("", "plugin", "list")
	h.expect(code, out, 0)
	contains(t, out, "NAME", "PATH", "hello", hello, "zed", zed)
	if strings.Index(out, "hello") > strings.Index(out, "zed") {
		t.Error("plugins are sorted by name")
	}
}

func TestRootHelpListsPlugins(t *testing.T) {
	h, dir := pluginHarness(t)
	_, out := h.run("", "--help")
	notContains(t, out, "Plugins (run with")
	installPlugin(t, dir, "hello", helloPlugin)
	code, out := h.run("", "--help")
	h.expect(code, out, 0)
	contains(t, out, "Plugins (run with `dsi <name>`):", "  hello", "plugin      Commands related to plugins")
	// subcommand help has no plugin section
	_, out = h.run("", "key", "--help")
	notContains(t, out, "Plugins (run with")
}

func TestDefaultFinderUsesPluginDirAndPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	h := newHarness(t) // env.Plugins is nil: the real lookup
	pluginDir, pathDir := t.TempDir(), t.TempDir()
	installPlugin(t, pluginDir, "fromdir", `echo "from plugin dir"`)
	installPlugin(t, pathDir, "frompath", `echo "from PATH"`)
	installPlugin(t, pathDir, "fromdir", `echo "from PATH (shadowed)"`)
	t.Setenv("DSI_PLUGIN_DIR", pluginDir)
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, out := h.run("", "fromdir")
	contains(t, out, "from plugin dir")
	notContains(t, out, "shadowed")
	_, out = h.run("", "frompath")
	contains(t, out, "from PATH")
}

// A plugin that ignores stdin must not block dsi when stdin never reaches EOF.
func TestPluginDoesNotWaitForStdinEOF(t *testing.T) {
	h, dir := pluginHarness(t)
	installPlugin(t, dir, "hello", `echo done`)
	r, w, err := os.Pipe() // an open pipe that is never written to or closed
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	defer r.Close()
	h.env.In = r
	h.env.reader = nil
	done := make(chan int, 1)
	go func() { done <- ExecuteEnv("test", []string{"hello"}, h.env) }()
	select {
	case code := <-done:
		if code != 0 || !strings.Contains(h.out.String(), "done") {
			t.Errorf("code %d output %q", code, h.out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dsi blocked waiting for stdin after the plugin exited")
	}

	// the same through a non-file reader that never ends
	pr, pw := io.Pipe()
	defer pw.Close()
	h.env.In = pr
	h.env.reader = nil
	h.out.Reset()
	go func() { done <- ExecuteEnv("test", []string{"hello"}, h.env) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("code %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("dsi blocked on a non-file stdin")
	}
}
