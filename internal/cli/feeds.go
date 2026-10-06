package cli

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Desvelao/dsipy/internal/core"
	"github.com/Desvelao/dsipy/internal/crypto"
	"github.com/Desvelao/dsipy/internal/feeds"
	"github.com/Desvelao/dsipy/internal/pyutil"
	"github.com/spf13/cobra"
)

func checkFeedType(feedType string) error {
	if feedType != "markdown" {
		return fmt.Errorf("Unsupported feed format: %s", feedType)
	}
	return nil
}

// optionValue returns the option value, prompting for it in interactive mode
// or failing when it is empty.
func (e *Env) optionValue(name, current, promptMessage, defaultValue string, interactive bool) (string, error) {
	if current != "" {
		return current, nil
	}
	if interactive {
		return e.prompt(promptMessage, &defaultValue)
	}
	e.secho(red, "❌ The %s cannot be empty. Please provide a %s using the --%s option.", name, name, name)
	return "", exit(1)
}

// createState writes a new Markdown post with front matter.
func (e *Env) createState(destination, title, message, date string) error {
	if pathExists(destination) {
		e.secho(red, "❌ A feed '%s' already exists.", destination)
		return exit(1)
	}
	if err := os.WriteFile(destination, []byte(feeds.CreateStateContent(title, message, date)), 0o644); err != nil {
		return err
	}
	e.secho(green, "✅ New post created: %s", destination)
	return nil
}

func nowUTC(e *Env) string { return e.now().UTC().Format("2006-01-02T15:04:05Z") }

// loadSigning returns the signer, or nil unless both keys are given. A value
// is either a key file or the PEM text itself (handy for CI secrets).
func (e *Env) loadSigning(privValue, pubValue string, privSet, pubSet bool) (*feeds.Signer, error) {
	if privSet != pubSet {
		e.secho(red, "❌ Signing needs both --sign-priv and --sign-pub; only one was given.")
		return nil, exit(1)
	}
	if !privSet {
		return nil, nil
	}
	read := func(value, option string) ([]byte, error) {
		if isFile(value) {
			return os.ReadFile(value)
		}
		if strings.Contains(value, "-----BEGIN") {
			return []byte(value), nil
		}
		e.secho(red, "❌ Signing key file not found for %s: %s", option, value)
		return nil, exit(1)
	}
	privData, err := read(privValue, "--sign-priv")
	if err != nil {
		return nil, err
	}
	pubData, err := read(pubValue, "--sign-pub")
	if err != nil {
		return nil, err
	}
	priv, err := crypto.LoadPrivateKeyPEM(privData)
	if err != nil {
		return nil, err
	}
	pub, err := crypto.LoadPublicKeyPEM(pubData)
	if err != nil {
		return nil, err
	}
	id, err := crypto.PublicKeyToB64DER(pub)
	if err != nil {
		return nil, err
	}
	return &feeds.Signer{Key: priv, ID: id}, nil
}

// parseTemplateVars merges --var-file and --var template variables (--var wins).
func (e *Env) parseTemplateVars(varFile string, varFileSet bool, vars []string) (*pyutil.OrderedMap, error) {
	out := pyutil.NewOrderedMap()
	if varFileSet {
		if !pathExists(varFile) {
			e.secho(red, "❌ Variable file not found: %s", varFile)
			return nil, exit(1)
		}
		text, err := readText(varFile)
		if err != nil {
			return nil, err
		}
		for _, line := range pyutil.SplitLines(text) {
			line = pyutil.Strip(line)
			if line != "" && strings.Contains(line, "=") && !strings.HasPrefix(line, "#") {
				k, v, _ := strings.Cut(line, "=")
				out.Set(pyutil.Strip(k), pyutil.Strip(v))
			}
		}
	}
	for _, arg := range vars {
		k, v, ok := strings.Cut(arg, "=")
		if !ok {
			return nil, usagef("Invalid value for '--var': Invalid --var '%s': expected key=value", arg)
		}
		out.Set(k, v)
	}
	return out, nil
}

func newFeedsCmd(env *Env) *cobra.Command {
	group := subcommand("feeds", "Commands related to feeds processing")

	// add
	{
		var title, message, filename, feedType string
		var interactive bool
		cmd := &cobra.Command{
			Use:   "add",
			Short: "Create a new feed file with an initial item",
			Args:  cobra.NoArgs,
			RunE: env.run("add", func(cmd *cobra.Command, args []string) error {
				if err := checkFeedType(feedType); err != nil {
					return err
				}
				now := nowUTC(env)
				var err error
				filename, err = env.optionValue("filename", filename, "Provide the filename for the new feed",
					fmt.Sprintf("feeds/%s.md", core.Slugify(now)), interactive)
				if err != nil {
					return err
				}
				if pathExists(filename) {
					env.secho(red, "❌ A feed file with the name '%s' already exists. Please choose a different name.", filename)
					return exit(1)
				}
				if !strings.HasSuffix(strings.ToLower(filename), ".md") {
					env.secho(yellow, "⚠️  '%s' does not end in .md: `dsi feeds build` only reads .md files.", filename)
				}
				if title, err = env.optionValue("title", title, "Provide the title for the new feed", "My New Feed", interactive); err != nil {
					return err
				}
				if message, err = env.optionValue("message", message, "Provide the message for the new feed",
					"This is the content of my new feed item.", interactive); err != nil {
					return err
				}
				if parent := filepath.Dir(filename); parent != "." {
					if err := os.MkdirAll(parent, 0o755); err != nil {
						return err
					}
				}
				if err := env.createState(filename, title, message, now); err != nil {
					return err
				}
				env.secho(plain, "ℹ️  Edit the file with a text editor: %s", filename)
				return nil
			}),
		}
		cmd.Flags().StringVarP(&title, "title", "t", "", "Title for the new feed")
		cmd.Flags().StringVarP(&message, "message", "m", "", "Content for the new feed item")
		cmd.Flags().StringVarP(&filename, "filename", "f", "", "Filename for the new feed")
		cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Interactive prompts")
		cmd.Flags().StringVar(&feedType, "type", "markdown", "Define the type of feed to create")
		group.AddCommand(cmd)
	}

	// init
	{
		var sample bool
		var noSample bool
		var feedType string
		cmd := &cobra.Command{
			Use:   "init [DIRECTORY]",
			Short: "Initialize the feeds source directory (creates it and a sample post).",
			Args:  cobra.MaximumNArgs(1),
			RunE: env.run("init", func(cmd *cobra.Command, args []string) error {
				if err := checkFeedType(feedType); err != nil {
					return err
				}
				if noSample {
					sample = false
				}
				directory := "feeds"
				if len(args) == 1 {
					directory = args[0]
				}
				if pathExists(directory) && !isDir(directory) {
					env.secho(red, "❌ '%s' exists and is not a directory.", directory)
					return exit(1)
				}
				already := isDir(directory)
				if err := os.MkdirAll(directory, 0o755); err != nil {
					return err
				}
				state := "created"
				if already {
					state = "already exists"
				}
				env.secho(plain, "ℹ️  Directory %s: %s", state, directory)

				samplePath := filepath.Join(directory, "hello.md")
				if sample && !pathExists(samplePath) {
					if err := env.createState(samplePath, "Hello DSI", "This is my first post. Edit or delete this file.", nowUTC(env)); err != nil {
						return err
					}
				} else if !sample {
					entries, err := os.ReadDir(directory)
					if err != nil {
						return err
					}
					if len(entries) == 0 {
						// keep the empty directory committable in git
						keep := filepath.Join(directory, ".gitkeep")
						if err := os.WriteFile(keep, nil, 0o644); err != nil {
							return err
						}
						env.secho(green, "✅ Created %s", keep)
					}
				}
				env.echo("Next: add posts with `dsi feeds add --filename %s/<name>.md` and build with `dsi feeds build %s`.", directory, directory)
				return nil
			}),
		}
		cmd.Flags().BoolVar(&sample, "sample", true, "Create a sample post (hello.md) when it does not exist")
		cmd.Flags().BoolVar(&noSample, "no-sample", false, "Do not create a sample post")
		cmd.Flags().StringVar(&feedType, "type", "markdown", "Define the type of feed to create")
		group.AddCommand(cmd)
	}

	// build
	{
		var output, title, link, description, language, author, email, feedType string
		var signPriv, signPub, varFile string
		var limit int
		var interactive bool
		var vars []string
		cmd := &cobra.Command{
			Use:   "build DIRECTORY",
			Short: "Build an RSS feed from a folder of Markdown posts",
			Args:  cobra.ExactArgs(1),
			RunE: env.run("build", func(cmd *cobra.Command, args []string) error {
				directory := args[0]
				flags := cmd.Flags()
				if flags.Changed("limit") && limit < 0 {
					return usagef("Invalid value for '--limit' / '-l': %d is not in the range x>=0.", limit)
				}
				if err := checkFeedType(feedType); err != nil {
					return err
				}
				var err error
				if title, err = env.optionValue("title", title, "Provide the title for the RSS feed", "My RSS Feed", interactive); err != nil {
					return err
				}
				if link, err = env.optionValue("link", link, "Provide the base link for the RSS feed items", "https://example.com", interactive); err != nil {
					return err
				}
				if description, err = env.optionValue("description", description, "Provide the description for the RSS feed", "This is my RSS feed", interactive); err != nil {
					return err
				}
				if author, err = env.optionValue("author", author, "Provide the author name for the RSS feed", "Author Name", interactive); err != nil {
					return err
				}
				if email, err = env.optionValue("email", email, "Provide the author email for the RSS feed", "author@email.com", interactive); err != nil {
					return err
				}
				signer, err := env.loadSigning(signPriv, signPub, signPriv != "", signPub != "")
				if err != nil {
					return err
				}
				varsMap, err := env.parseTemplateVars(varFile, flags.Changed("var-file"), vars)
				if err != nil {
					return err
				}
				if !pathExists(directory) {
					env.secho(red, "❌ Directory not found: %s", directory)
					return exit(1)
				}
				states, err := feeds.Collect(directory)
				if err != nil {
					return err
				}
				if flags.Changed("limit") && limit < len(states) {
					states = states[:limit]
				}
				feeds.ApplyTemplates(states, varsMap)

				content, err := feeds.BuildRSS(title, link, description, author, email, language, env.now().UTC(), states, signer)
				if err != nil {
					return err
				}
				if output != "" {
					if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
						return err
					}
					if err := os.WriteFile(output, []byte(content), 0o644); err != nil {
						return err
					}
					env.secho(green, "✅ RSS feed generated: %s", output)
				} else {
					env.echo("%s", content)
				}
				return nil
			}),
		}
		f := cmd.Flags()
		f.StringVarP(&output, "output", "o", "feed.rss", "Output RSS file")
		f.IntVarP(&limit, "limit", "l", 0, "Limit number of states to include in the feed (0 = empty feed)")
		f.StringVarP(&title, "title", "t", "", "RSS feed title")
		f.StringVarP(&link, "link", "k", "", "Base link for items")
		f.StringVarP(&description, "description", "d", "", "RSS feed description")
		f.StringVarP(&language, "language", "g", "en-US", "Language of the feed")
		f.StringVarP(&author, "author", "a", "", "Author information")
		f.StringVarP(&email, "email", "e", "", "Email of the author")
		f.StringVar(&feedType, "type", "markdown", "Define the type of states")
		f.BoolVarP(&interactive, "interactive", "i", false, "Interactive prompts")
		f.StringVar(&signPriv, "sign-priv", "", "Private key file to sign RSS items")
		f.StringVar(&signPub, "sign-pub", "", "Public key file to verify RSS item signatures")
		f.StringArrayVar(&vars, "var", nil, "Template variables as key=value pairs")
		f.StringVar(&varFile, "var-file", "", "File with template variables (one KEY=VALUE per line)")
		group.AddCommand(cmd)
	}

	// verify
	{
		var vcardPath, pubPath string
		cmd := &cobra.Command{
			Use:   "verify FEED",
			Short: "Verify the signed items of an RSS file",
			Args:  cobra.ExactArgs(1),
			RunE: env.run("verify", func(cmd *cobra.Command, args []string) error {
				feed := args[0]
				if !pathExists(feed) {
					return usagef("Invalid value for 'FEED': File '%s' does not exist.", feed)
				}
				keys := map[string]ed25519.PublicKey{}
				if vcardPath != "" {
					card, err := core.NewVCardFromPath(vcardPath)
					if err != nil {
						return err
					}
					for _, k := range card.Profile.Keys {
						if pub, err := crypto.LoadPublicKeyB64DER(k.KeyB64); err == nil {
							keys[k.KeyB64] = pub
						}
					}
				}
				if pubPath != "" {
					data, err := os.ReadFile(pubPath)
					if err != nil {
						return err
					}
					pub, err := crypto.LoadPublicKeyPEM(data)
					if err != nil {
						return err
					}
					id, err := crypto.PublicKeyToB64DER(pub)
					if err != nil {
						return err
					}
					keys[id] = pub
				}
				if len(keys) == 0 {
					env.secho(red, "❌ Pass --vcard or --pub to provide the verification keys.")
					return exit(1)
				}
				text, err := readText(feed)
				if err != nil {
					return err
				}
				results, err := feeds.VerifyFeedItems(text, keys)
				if err != nil {
					return err
				}
				invalid := 0
				for _, item := range results {
					label := item.Title
					if label == "" && item.Guid != nil {
						label = *item.Guid
					}
					if label == "" {
						label = "(untitled)"
					}
					switch item.Status {
					case feeds.StatusValid:
						env.secho(green, "✅ valid    %s", label)
					case feeds.StatusUnsigned:
						env.secho(yellow, "⚠️ unsigned %s", label)
					default:
						invalid++
						env.secho(red, "❌ invalid  %s (%s)", label, item.Reason)
					}
				}
				env.echo("%d item(s), %d invalid", len(results), invalid)
				if invalid > 0 {
					return exit(1)
				}
				return nil
			}),
		}
		cmd.Flags().StringVarP(&vcardPath, "vcard", "v", "", "vCard whose KEY properties may have signed the feed")
		cmd.Flags().StringVar(&pubPath, "pub", "", "Public key PEM that signed the feed (alternative)")
		group.AddCommand(cmd)
	}
	return group
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
