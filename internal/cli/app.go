// Package cli wires the dsi command tree.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// debugEnabled mirrors the Python implementation: any value other than
// empty, "0" or "false" turns debug on. DSIPY_DEBUG is honored as a legacy alias.
func debugEnabled() bool {
	for _, name := range []string{"DSI_DEBUG", "DSIPY_DEBUG"} {
		switch os.Getenv(name) {
		case "", "0", "false":
		default:
			return true
		}
	}
	return false
}

// NewRootCmd builds the root command with IO streams injected (for tests).
func NewRootCmd(version string, in io.Reader, out, errOut io.Writer) *cobra.Command {
	var debug bool
	root := &cobra.Command{
		Use:           "dsi",
		Short:         "A CLI tool with subcommands for vCard, feeds, and connections processing",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if debug {
				os.Setenv("DSI_DEBUG", "1")
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug output (stack traces)")
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute(version string, args []string, in io.Reader, out, errOut io.Writer) int {
	root := NewRootCmd(version, in, out, errOut)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	return 0
}
