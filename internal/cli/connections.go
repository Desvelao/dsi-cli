package cli

import (
	"os"
	"path/filepath"

	"github.com/Desvelao/dsi-cli/internal/core"
	"github.com/Desvelao/dsi-cli/internal/feeds"
	"github.com/Desvelao/dsi-cli/internal/strutil"
	"github.com/spf13/cobra"
)

func newConnectionsCmd(env *Env) *cobra.Command {
	group := subcommand("connections", "Commands related to connections processing")
	var output, title string
	cmd := &cobra.Command{
		Use:   "feed INPUTS...",
		Short: "Generate an OPML file from vCard files (files, directories)",
		Args:  cobra.MinimumNArgs(1),
		RunE: env.run("feed", func(cmd *cobra.Command, args []string) error {
			inputs := newVCardInputs(args)
			for _, w := range inputs.warnings {
				env.secho(red, "%s", w)
			}
			for _, u := range inputs.urls {
				env.secho(red, "Ignoring URL input (only local vCard files are supported): %s", u)
			}
			if len(inputs.files) == 0 {
				env.secho(red, "❌ No vCard files found in the specified directory or files: %s", pyList(args))
				return exit(1)
			}
			var opmlWarnings []string
			opml, err := feeds.GenerateOPMLFromVCards(inputs.files, title, &opmlWarnings)
			for _, w := range opmlWarnings {
				env.secho(red, "%s", w)
			}
			if err != nil {
				env.secho(red, "❌ %s", err)
				return exit(1)
			}
			if output != "" {
				if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(output, []byte(opml), 0o644); err != nil {
					return err
				}
				env.secho(plain, "✅ OPML file generated: %s", output)
			} else {
				env.echo("%s", opml)
			}
			return nil
		}),
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output OPML file")
	cmd.Flags().StringVar(&title, "title", "", "OPML title (default \""+feeds.DefaultOPMLTitle+"\")")
	group.AddCommand(cmd)
	return group
}

// vcardInputs classifies command inputs into local vCard files and URLs.
type vcardInputs struct {
	files    []string
	urls     []string
	warnings []string
}

func newVCardInputs(inputs []string) *vcardInputs {
	urls, paths := core.ClassifyInputs(inputs)
	v := &vcardInputs{urls: urls}
	v.files = core.LocalFilesFromInputs(paths, core.FileIsVCardPath, &v.warnings)
	return v
}

// pyList formats a list as [a, b] with single-quoted items.
func pyList(items []string) string {
	out := "["
	for i, s := range items {
		if i > 0 {
			out += ", "
		}
		out += strutil.Repr(s)
	}
	return out + "]"
}
