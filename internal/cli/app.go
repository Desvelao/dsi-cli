// Package cli wires the dsi command tree.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/Desvelao/dsipy/internal/pyutil"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Desvelao/dsipy/internal/core"
	"github.com/Desvelao/dsipy/internal/plugin"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Env is the environment commands run in (injected so tests can capture it).
type Env struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	// Color enables ANSI colors (when Out is a terminal and NO_COLOR is unset).
	Color bool
	// StdinTTY is true when stdin is an interactive terminal.
	StdinTTY bool
	Fetcher  *core.Fetcher
	Now      func() time.Time
	// Plugins overrides where plugins are searched (nil: plugin dir and PATH).
	Plugins *plugin.Finder

	reader *bufio.Reader
}

// ExitError makes the process exit with Code (the message was already printed).
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

func exit(code int) error { return &ExitError{Code: code} }

// UsageError is a command-line usage problem (exit code 2).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

func usagef(format string, args ...any) error { return &UsageError{fmt.Sprintf(format, args...)} }

// debugEnabled mirrors the Python implementation: any value other than
// empty, "0" or "false" turns debug on. DSIPY_DEBUG is honored as a legacy alias.
func debugEnabled() bool {
	for _, name := range []string{"DSI_DEBUG", "DSIPY_DEBUG"} {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
		case "", "0", "false":
		default:
			return true
		}
	}
	return false
}

func (e *Env) reader_() *bufio.Reader {
	if e.reader == nil {
		e.reader = bufio.NewReader(e.In)
	}
	return e.reader
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Env) fetcher() *core.Fetcher {
	if e.Fetcher != nil {
		return e.Fetcher
	}
	return core.DefaultFetcher
}

// readAllStdin reads the rest of stdin.
func (e *Env) readAllStdin() (string, error) {
	b, err := io.ReadAll(e.reader_())
	return string(b), err
}

// style is an ANSI style for secho.
type style string

const (
	plain  style = ""
	red    style = "31"
	green  style = "32"
	yellow style = "33"
	blue   style = "34"
	cyan   style = "36"
	bold   style = "1"
)

func (e *Env) paint(s style, text string) string {
	if !e.Color || s == plain {
		return text
	}
	return "\x1b[" + string(s) + "m" + text + "\x1b[0m"
}

// secho prints a styled line to stdout (typer.secho).
func (e *Env) secho(s style, format string, args ...any) {
	fmt.Fprintln(e.Out, e.paint(s, fmt.Sprintf(format, args...)))
}

// echo prints a line to stdout (typer.echo).
func (e *Env) echo(format string, args ...any) { fmt.Fprintf(e.Out, format+"\n", args...) }

// prompt asks for a value like typer.prompt: an empty answer returns the
// default, or asks again when there is none. EOF aborts.
func (e *Env) prompt(text string, def *string) (string, error) {
	for {
		if def != nil {
			fmt.Fprintf(e.Out, "%s [%s]: ", text, *def)
		} else {
			fmt.Fprintf(e.Out, "%s: ", text)
		}
		line, err := e.reader_().ReadString('\n')
		if err != nil && line == "" {
			return "", e.abort()
		}
		line = strings.TrimRight(line, "\r\n")
		if line != "" {
			return line, nil
		}
		if def != nil {
			return *def, nil
		}
	}
}

// confirm asks a yes/no question like typer.confirm.
func (e *Env) confirm(text string, def bool) (bool, error) {
	suffix := "[y/N]"
	if def {
		suffix = "[Y/n]"
	}
	for {
		fmt.Fprintf(e.Out, "%s %s: ", text, suffix)
		line, err := e.reader_().ReadString('\n')
		if err != nil && line == "" {
			return false, e.abort()
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		case "":
			return def, nil
		}
		fmt.Fprintln(e.Err, "Error: invalid input")
	}
}

func (e *Env) abort() error {
	fmt.Fprintln(e.Err, "Aborted.")
	return exit(1)
}

// run wraps a command: unexpected errors are reported like the Python
// error handler ("❌ Command 'x' failed: ...") and exit with code 1.
func (e *Env) run(name string, fn func(cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := fn(cmd, args)
		if err == nil {
			return nil
		}
		var ee *ExitError
		var ue *UsageError
		if errors.As(err, &ee) || errors.As(err, &ue) {
			return err
		}
		e.secho(red, "❌ Command '%s' failed: %s", name, pyutil.ErrText(err))
		if debugEnabled() {
			fmt.Fprintf(e.Err, "%v\n%s\n", err, debug.Stack())
		}
		return exit(1)
	}
}

// NewRootCmd builds the command tree.
func NewRootCmd(version string, env *Env) *cobra.Command {
	var debugFlag bool
	root := &cobra.Command{
		Use:           "dsi",
		Short:         "DSI Tools: A collection of CLI tools for working with vCards, feeds, connections, and keys",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if debugFlag {
				os.Setenv("DSI_DEBUG", "1")
			}
		},
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.PersistentFlags().BoolVar(&debugFlag, "debug", false, "Print tracebacks on errors (or set DSI_DEBUG=1).")
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetIn(env.In)
	root.SetOut(env.Out)
	root.SetErr(env.Err)
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error { return &UsageError{err.Error()} })
	root.AddCommand(newKeyCmd(env), newConnectionsCmd(env), newFeedsCmd(env), newVCardCmd(env), newPluginCmd(env))
	addPluginHelp(root, env)
	return root
}

// subcommand returns a group command that prints its help without arguments.
func subcommand(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		// like click's no_args_is_help: show the help and exit with the usage code
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cmd.Help(); err != nil {
				return err
			}
			return exit(2)
		},
	}
}

// ExecuteEnv runs the CLI with an explicit environment and returns the exit code.
func ExecuteEnv(version string, args []string, env *Env) int {
	root := NewRootCmd(version, env)
	if code, ok := env.dispatchPlugin(root, version, args); ok {
		return code
	}
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	// anything else comes from argument/flag parsing: a usage error
	cmd, _, _ := root.Find(args)
	if cmd == nil {
		cmd = root
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "unknown command ") { // click's wording
		if parts := strings.SplitN(msg, `"`, 3); len(parts) >= 2 {
			msg = fmt.Sprintf("No such command '%s'.", parts[1])
		}
	}
	fmt.Fprintf(env.Err, "Usage: %s\nTry '%s --help' for help.\n\nError: %s\n", cmd.UseLine(), cmd.CommandPath(), msg)
	return 2
}

// Execute runs the CLI with the given streams.
func Execute(version string, args []string, in io.Reader, out, errOut io.Writer) int {
	env := &Env{In: in, Out: out, Err: errOut}
	return ExecuteEnv(version, args, env)
}

// NewEnvFromOS builds the environment of a real process: stdin/stdout/stderr,
// colors when stdout is a terminal (and NO_COLOR is unset).
func NewEnvFromOS() *Env {
	return &Env{
		In:       os.Stdin,
		Out:      os.Stdout,
		Err:      os.Stderr,
		Color:    term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == "",
		StdinTTY: term.IsTerminal(int(os.Stdin.Fd())),
	}
}
