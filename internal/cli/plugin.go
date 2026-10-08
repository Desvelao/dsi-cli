package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"text/tabwriter"

	"github.com/Desvelao/dsi-cli/internal/plugin"
	"github.com/spf13/cobra"
)

// finder returns the plugin finder (overridable in tests).
func (e *Env) finder() plugin.Finder {
	if e.Plugins != nil {
		return *e.Plugins
	}
	return plugin.DefaultFinder()
}

// dispatchPlugin runs an external plugin when the first argument is not a core
// command: `dsi foo a b` runs `dsi-foo a b`. Core commands are never shadowed.
// The returned bool is false when args do not name a plugin.
func (e *Env) dispatchPlugin(root *cobra.Command, version string, args []string) (int, bool) {
	idx := -1
	for i, a := range args {
		if a == "--" || !strings.HasPrefix(a, "-") {
			if a != "--" {
				idx = i
			}
			break
		}
		if a != "--debug" { // any other flag: leave it to cobra
			return 0, false
		}
	}
	if idx < 0 {
		return 0, false
	}
	name := args[idx]
	if name == "help" || isCoreCommand(root, name) {
		return 0, false
	}
	path, ok := e.finder().Find(name)
	if !ok {
		return 0, false
	}
	debug := e.debugEnabled()
	for _, a := range args[:idx] {
		debug = debug || a == "--debug"
	}
	return e.runPlugin(path, args[idx+1:], version, debug), true
}

func isCoreCommand(root *cobra.Command, name string) bool {
	for _, c := range root.Commands() {
		if c.Name() == name {
			return true
		}
		for _, alias := range c.Aliases {
			if alias == name {
				return true
			}
		}
	}
	return false
}

// runPlugin executes a plugin with the standard streams and returns its exit
// code. The plugin learns about its host through the environment: DSI_VERSION,
// DSI_BIN (this executable) and DSI_DEBUG.
func (e *Env) runPlugin(path string, args []string, version string, debug bool) int {
	cmd := exec.Command(path, args...)
	var stopped atomic.Bool
	if f, ok := e.In.(*os.File); ok && e.reader == nil {
		// hand the real stdin to the child
		cmd.Stdin = f
	} else {
		// exec cannot interrupt a goroutine blocked reading a non-file stdin and
		// would make Wait hang after the plugin exited, so feed the child
		// through our own pipe
		pr, pw, err := os.Pipe()
		if err != nil {
			e.secho(red, "❌ Cannot run plugin '%s': %s", path, err)
			return 1
		}
		defer pr.Close()
		defer pw.Close()
		defer stopped.Store(true)
		cmd.Stdin = pr
		src := e.reader_()
		// A Read blocked on a non-file stdin cannot be interrupted, so that
		// goroutine ends when the read returns; stopped makes sure it never
		// reads again (and never consumes input meant for dsi) once the plugin
		// is gone.
		go func() {
			defer pw.Close()
			buf := make([]byte, 32*1024)
			for !stopped.Load() {
				n, err := src.Read(buf)
				if n > 0 && !stopped.Load() {
					if _, werr := pw.Write(buf[:n]); werr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()
	}
	cmd.Stdout = e.Out
	cmd.Stderr = e.Err
	cmd.Env = append(os.Environ(), "DSI_VERSION="+version)
	if self, err := os.Executable(); err == nil {
		cmd.Env = append(cmd.Env, "DSI_BIN="+self)
	}
	if debug {
		cmd.Env = append(cmd.Env, "DSI_DEBUG=1")
	}
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
		return 128 + 1 // terminated by a signal
	}
	e.secho(red, "❌ Cannot run plugin '%s': %s", path, err)
	return 1
}

func newPluginCmd(env *Env) *cobra.Command {
	group := subcommand("plugin", "Commands related to plugins (external dsi-<name> executables)")
	group.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the installed plugins",
		Args:  cobra.NoArgs,
		RunE: env.run("list", func(cmd *cobra.Command, args []string) error {
			plugins := env.finder().List()
			if len(plugins) == 0 {
				env.echo("No plugins installed.")
				env.echo("A plugin is an executable named dsi-<name> in %s or in your PATH; `dsi <name>` runs it.", plugin.PluginDir())
				return nil
			}
			w := tabwriter.NewWriter(env.Out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPATH")
			root := cmd.Root()
			for _, p := range plugins {
				note := ""
				if isCoreCommand(root, p.Name) {
					note = "  (shadowed by the core command, cannot be run)"
				}
				fmt.Fprintf(w, "%s\t%s%s\n", p.Name, p.Path, note)
			}
			return w.Flush()
		}),
	})
	return group
}

// addPluginHelp lists the installed plugins after the root help. (A custom
// usage template function would make the linker keep much more code.)
func addPluginHelp(root *cobra.Command, env *Env) {
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		defaultHelp(cmd, args)
		if cmd != root {
			return
		}
		var names []string
		for _, p := range env.finder().List() {
			if !isCoreCommand(root, p.Name) {
				names = append(names, "  "+p.Name)
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(env.Out, "\nPlugins (run with `dsi <name>`):\n%s\n", strings.Join(names, "\n"))
		}
	})
}
