package cli

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Desvelao/dsipy/internal/canonical"
	"github.com/Desvelao/dsipy/internal/core"
	"github.com/Desvelao/dsipy/internal/crypto"
	"github.com/Desvelao/dsipy/internal/endorsements"
	"github.com/Desvelao/dsipy/internal/model"
	"github.com/Desvelao/dsipy/internal/pyutil"
	"github.com/Desvelao/dsipy/internal/vcard"
	"github.com/spf13/cobra"
)

var urlRe = regexp.MustCompile(`^https?://`)

// loadCard loads a vCard from a local path, a URL, or "-" (standard input).
func (e *Env) loadCard(source string, allowHTTP, verifySource bool) (*core.VCard, error) {
	if source == "-" {
		text, err := e.readAllStdin()
		if err != nil {
			return nil, err
		}
		return core.NewVCardFromText(text), nil
	}
	if urlRe.MatchString(source) {
		return e.fetcher().NewVCardFromURL(source, allowHTTP, verifySource)
	}
	return core.NewVCardFromPath(source)
}

func newVCardCmd(env *Env) *cobra.Command {
	group := subcommand("vcard", "Commands related to vCard processing")
	group.AddCommand(newCreateCmd(env), newFetchCmd(env), newParseCmd(env), newEndorseCmd(env),
		newQRCmd(env), newValidateCmd(env), newInspectCmd(env), newNormalizeCmd(env), newVerifyCmd(env))
	return group
}

// ---------------------------------------------------------------- parse

func newParseCmd(env *Env) *cobra.Command {
	var allowHTTP bool
	cmd := &cobra.Command{
		Use:   "parse [INPUT]",
		Short: "Parse a vCard (file, URL or stdin) and print its properties as JSON.",
		Args:  cobra.MaximumNArgs(1),
		RunE: env.run("parse", func(cmd *cobra.Command, args []string) error {
			input := ""
			haveInput := len(args) == 1
			if haveInput {
				input = args[0]
			} else if !env.StdinTTY {
				input, haveInput = "-", true
			}
			if !haveInput {
				env.secho(red, "❌ No input data provided for parsing.")
				return exit(1)
			}
			var card *core.VCard
			var jsonString string
			err := func() error {
				var err error
				if input == "-" {
					text, rerr := env.readAllStdin()
					if rerr != nil {
						return rerr
					}
					if text = pyutil.Strip(text); text != "" {
						card = core.NewVCardFromText(text)
					}
				} else if card, err = env.loadCard(input, allowHTTP, true); err != nil {
					return err
				}
				if card != nil {
					jsonString, err = card.ToJSON()
				}
				return err
			}()
			if err != nil {
				env.secho(red, "Failed to parse vCard: %s", pyutil.ErrText(err))
				return exit(1)
			}
			if card == nil {
				env.secho(red, "❌ No input data provided for parsing.")
				return exit(1)
			}
			env.echo("%s", jsonString)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&allowHTTP, "allow-http", false, "Allow plain HTTP URLs")
	return cmd
}

// ---------------------------------------------------------------- endorse

func newEndorseCmd(env *Env) *cobra.Command {
	var dest, priv, confidence string
	var write bool
	cmd := &cobra.Command{
		Use:   "endorse INPUTS...",
		Short: "Sign canonical endorsement strings for one or more vCard files",
		Args:  cobra.MinimumNArgs(1),
		RunE: env.run("endorse", func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("vcard") && !pathExists(dest) {
				return usagef("Invalid value for '--vcard' / '-v': File '%s' does not exist.", dest)
			}
			if !pathExists(priv) {
				return usagef("Invalid value for '--priv': File '%s' does not exist.", priv)
			}
			if confidence != "low" && confidence != "medium" && confidence != "high" {
				env.secho(red, "❌ Invalid confidence level '%s'. Must be one of: low, medium, high", confidence)
				return exit(1)
			}
			privData, err := os.ReadFile(priv)
			var key ed25519.PrivateKey
			if err == nil {
				key, err = crypto.LoadPrivateKeyPEM(privData)
			}
			if err != nil {
				env.secho(red, "Failed to load private key from '%s': %s", priv, pyutil.ErrText(err))
				return exit(1)
			}

			if write && !cmd.Flags().Changed("vcard") {
				env.secho(red, "❌ --write requires --vcard (the vCard where endorsements are added).")
				return exit(1)
			}
			inputs := newVCardInputs(args)
			for _, w := range inputs.warnings {
				env.secho(red, "%s", w)
			}
			if len(inputs.files) == 0 {
				env.secho(red, "❌ No valid vCard files found.")
				return exit(1)
			}
			var destination *core.VCard
			if write {
				if destination, err = core.NewVCardFromPath(dest); err != nil {
					return err
				}
			}
			failed := 0
			for _, path := range inputs.files {
				err := func() error {
					card, err := core.NewVCardFromPath(path)
					if err != nil {
						return err
					}
					preferred := card.PreferredKey()
					if preferred == nil {
						return fmt.Errorf("No valid KEY entries found in the vCard")
					}
					// Reject malformed or non-Ed25519 keys before endorsing them
					if _, err := crypto.LoadPublicKeyB64DER(preferred.KeyB64); err != nil {
						return err
					}
					sig := crypto.SignEndorsement(key, preferred.KeyB64)
					value := vcard.BuildEndorsementAttribute(preferred.KeyB64, sig,
						env.now().UTC().Format(model.DSIDateFormat), confidence, "b")
					if !write {
						env.echo("%s", value)
						return nil
					}
					if destination.HasEndorsementFor(preferred.KeyB64) {
						env.secho(yellow, "⚠️ Endorsement already exists for key %s in %s. Skipping write.", preferred.KeyB64, dest)
						return nil
					}
					if err := destination.AddLine(value); err != nil {
						return err
					}
					if err := destination.ToFile(""); err != nil {
						return err
					}
					env.secho(green, "✅ Endorsement added to %s: %s", destination.Path, value)
					return nil
				}()
				if err != nil {
					failed++
					env.secho(red, "Failed to endorse vCard '%s': %s", path, pyutil.ErrText(err))
				}
			}
			if failed > 0 {
				return exit(1)
			}
			return nil
		}),
	}
	f := cmd.Flags()
	f.StringVarP(&dest, "vcard", "v", "", "Path to the .vcf where the endorsement will be added")
	f.StringVar(&priv, "priv", "", "Path to the private key PEM used to sign endorsements.")
	cmd.MarkFlagRequired("priv")
	f.StringVarP(&confidence, "confidence", "c", "medium", "Confidence level for the endorsement (low, medium, high). This is just metadata and does not affect the signature.")
	f.BoolVar(&write, "write", false, "Whether to write the endorsement to the vCard file. If false, the endorsement will be generated and printed but not saved.")
	return cmd
}

// ---------------------------------------------------------------- qr

func newQRCmd(env *Env) *cobra.Command {
	var output, image, captionTop, captionBottom, font string
	cmd := &cobra.Command{
		Use:   "qr [INPUT]",
		Short: "Generate a QR code from the provided vCard file (support input piping) and save it to a file.",
		Args:  cobra.MaximumNArgs(1),
		RunE: env.run("qr", func(cmd *cobra.Command, args []string) error {
			data := ""
			if len(args) == 1 && args[0] != "" {
				if isFile(args[0]) {
					text, err := readText(args[0])
					if err != nil {
						return err
					}
					data = pyutil.Strip(text)
				} else {
					data = pyutil.Strip(args[0])
				}
			} else if !env.StdinTTY {
				text, err := env.readAllStdin()
				if err != nil {
					return err
				}
				data = pyutil.Strip(text)
			}
			if data == "" {
				env.secho(red, "❌ No input data provided for the QR code.")
				return exit(1)
			}
			if image != "" && !isFile(image) {
				env.secho(red, "❌ The specified image file does not exist: %s", image)
				return exit(1)
			}
			if output == "" {
				env.secho(red, "❌ Output file path is required to save the QR code image.")
				return exit(1)
			}
			if captionTop != "" || captionBottom != "" {
				if font == "" {
					env.secho(red, "❌ A font file must be specified when using captions.")
					return exit(1)
				} else if !isFile(font) {
					env.secho(red, "❌ The specified font file does not exist: %s", font)
					return exit(1)
				}
			}
			if err := vcard.GenerateQR(vcard.QROptions{
				Image: image, Output: output, Data: data, CaptionTop: captionTop, CaptionBottom: captionBottom, Font: font,
			}); err != nil {
				return err
			}
			env.echo("QR code generated!")
			return nil
		}),
	}
	f := cmd.Flags()
	f.StringVarP(&output, "output", "o", "", "Output file to save the QR code image")
	f.StringVarP(&image, "image", "i", "", "Path to an image to include in the QR code")
	f.StringVarP(&captionTop, "caption-top", "t", "", "Caption to display above the QR code")
	f.StringVarP(&captionBottom, "caption-bottom", "b", "", "Caption to display below the QR code")
	f.StringVarP(&font, "font", "f", "", "Path to a .ttf font file to use for captions (optional)")
	return cmd
}

// ---------------------------------------------------------------- validate / inspect / normalize / verify

func newValidateCmd(env *Env) *cobra.Command {
	var asJSON, strict, allowHTTP bool
	cmd := &cobra.Command{
		Use:   "validate SOURCE",
		Short: "Validate a DSI vCard (file, URL or '-' for stdin).",
		Args:  cobra.ExactArgs(1),
		RunE: env.run("validate", func(cmd *cobra.Command, args []string) error {
			source := args[0]
			card, err := env.loadCard(source, allowHTTP, true)
			if err != nil {
				if asJSON {
					res := &core.ValidationResult{}
					res.Errors = append(res.Errors, core.Issue{Code: "load", Message: pyutil.ErrText(err)})
					out, _ := pyutil.JSONDumps(res.ToDict())
					env.echo("%s", out)
				} else {
					env.secho(red, "❌ Cannot load '%s': %s", source, pyutil.ErrText(err))
				}
				return exit(1)
			}
			result := core.ValidateProfile(card.Profile)
			failed := !result.Valid() || (strict && len(result.Warnings) > 0)
			if asJSON {
				out, err := pyutil.JSONDumps(result.ToDict())
				if err != nil {
					return err
				}
				env.echo("%s", out)
			} else {
				for _, issue := range result.Errors {
					env.secho(red, "❌ [%s] %s", issue.Code, issue.Message)
				}
				for _, issue := range result.Warnings {
					env.secho(yellow, "⚠️  [%s] %s", issue.Code, issue.Message)
				}
				if failed {
					env.secho(red, "Invalid: %d error(s), %d warning(s)", len(result.Errors), len(result.Warnings))
				} else {
					env.secho(green, "✅ Valid (%d warning(s))", len(result.Warnings))
				}
			}
			if failed {
				return exit(1)
			}
			return nil
		}),
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the result as JSON ({valid, errors, warnings})")
	cmd.Flags().BoolVar(&strict, "strict", false, "Treat warnings as errors (exit code 1)")
	cmd.Flags().BoolVar(&allowHTTP, "allow-http", false, "Allow plain HTTP URLs")
	return cmd
}

func newInspectCmd(env *Env) *cobra.Command {
	var allowHTTP bool
	cmd := &cobra.Command{
		Use:   "inspect SOURCE",
		Short: "Show a read-only summary of a DSI vCard.",
		Args:  cobra.ExactArgs(1),
		RunE: env.run("inspect", func(cmd *cobra.Command, args []string) error {
			card, err := env.loadCard(args[0], allowHTTP, true)
			if err != nil {
				return err
			}
			p := card.Profile
			revoked := map[string]bool{}
			for _, r := range p.Revocations {
				revoked[r.KeyB64] = true
			}
			var preferred *model.PublicKey
			for i := range p.Keys {
				if p.Keys[i].Pref != nil && *p.Keys[i].Pref == 1 {
					preferred = &p.Keys[i]
					break
				}
			}
			orDash := func(s *string) string {
				if model.Str(s) == "" {
					return "-"
				}
				return *s
			}
			env.secho(bold, "Identity")
			env.echo("  Name:   %s", orDash(p.FN))
			env.echo("  SOURCE: %s", orDash(p.Source))
			env.secho(bold, "Keys")
			if preferred != nil {
				alg := preferred.Alg
				if alg == "" {
					alg = "unknown algorithm"
				}
				env.echo("  Preferred: %s", alg)
			} else {
				env.echo("  Preferred: none")
			}
			env.echo("  Keys: %d", len(p.Keys))
			env.echo("  Revoked: %d", len(revoked))
			env.secho(bold, "Feeds")
			for _, f := range p.Feeds {
				var detail []string
				for _, x := range []string{f.Language, f.Tags} {
					if x != "" {
						detail = append(detail, x)
					}
				}
				line := "  " + f.URL
				if len(detail) > 0 {
					line += " (" + strings.Join(detail, ", ") + ")"
				}
				env.echo("%s", line)
			}
			if len(p.Feeds) == 0 {
				env.echo("  -")
			}
			env.secho(bold, "Social")
			for _, s := range p.Social {
				env.echo("  %s: %s", s.Platform, s.Value)
			}
			if len(p.Social) == 0 {
				env.echo("  -")
			}
			env.secho(bold, "Endorsements")
			env.echo("  %d", len(p.Endorsements))
			return nil
		}),
	}
	cmd.Flags().BoolVar(&allowHTTP, "allow-http", false, "Allow plain HTTP URLs")
	return cmd
}

func newNormalizeCmd(env *Env) *cobra.Command {
	var write, allowHTTP bool
	cmd := &cobra.Command{
		Use:   "normalize SOURCE",
		Short: "Print the deterministic (normalized) form of a vCard.",
		Args:  cobra.ExactArgs(1),
		RunE: env.run("normalize", func(cmd *cobra.Command, args []string) error {
			source := args[0]
			if write && (source == "-" || urlRe.MatchString(source)) {
				env.secho(red, "❌ --write needs a local file.")
				return exit(1)
			}
			card, err := env.loadCard(source, allowHTTP, true)
			if err != nil {
				return err
			}
			text, err := canonical.NormalizeVCard(card.Profile)
			if err != nil {
				env.secho(red, "❌ %s", err)
				return exit(1)
			}
			if write {
				return os.WriteFile(source, []byte(text), 0o644)
			}
			fmt.Fprint(env.Out, text)
			return nil
		}),
	}
	cmd.Flags().BoolVar(&write, "write", false, "Overwrite the input file with the normalized form")
	cmd.Flags().BoolVar(&allowHTTP, "allow-http", false, "Allow plain HTTP URLs")
	return cmd
}

func newVerifyCmd(env *Env) *cobra.Command {
	var allowHTTP bool
	cmd := &cobra.Command{
		Use:   "verify SOURCE",
		Short: "Verify the X-ENDORSE signatures of a vCard against its own keys.",
		Args:  cobra.ExactArgs(1),
		RunE: env.run("verify", func(cmd *cobra.Command, args []string) error {
			card, err := env.loadCard(args[0], allowHTTP, true)
			if err != nil {
				return err
			}
			if len(card.Profile.Endorsements) == 0 {
				env.echo("No endorsements.")
				return nil
			}
			bad := 0
			for _, out := range endorsements.VerifyEndorsements(card.Profile) {
				key := out.Endorsement.EndorseeKeyB64
				if out.Status == endorsements.Valid {
					env.secho(green, "✅ valid    %s", key)
					continue
				}
				color := yellow
				if out.Status == endorsements.Invalid {
					bad++
					color = red
				}
				env.secho(color, "%-9s %s (%s)", out.Status, key, out.Reason)
			}
			if bad > 0 {
				return exit(1)
			}
			return nil
		}),
	}
	cmd.Flags().BoolVar(&allowHTTP, "allow-http", false, "Allow plain HTTP URLs")
	return cmd
}

// ---------------------------------------------------------------- fetch

func newFetchCmd(env *Env) *cobra.Command {
	var outputDir string
	var dryRun, backup, showDiff, allowHTTP, verifySource, noVerify bool
	cmd := &cobra.Command{
		Use:   "fetch INPUTS...",
		Short: "Fetch or update vCards from the remote vCard referenced in their SOURCE property.",
		Long: "Fetch or update vCards by fetching the remote vCard referenced in their SOURCE property.\n\n" +
			"Supports multiple files, directories, URLs, dry-run mode, backups, colored diffs\n" +
			"and a final summary report.",
		Args: cobra.MinimumNArgs(1),
		RunE: env.run("fetch", func(cmd *cobra.Command, args []string) error {
			if noVerify {
				verifySource = false
			}
			inputs := newVCardInputs(args)
			for _, w := range inputs.warnings {
				env.secho(red, "%s", w)
			}
			if len(inputs.files)+len(inputs.urls) == 0 {
				env.secho(red, "No valid .vcf files or URLs provided.")
				return exit(1)
			}
			if outputDir != "" {
				if err := os.MkdirAll(outputDir, 0o755); err != nil {
					return err
				}
			}
			var downloaded, updated, unchanged, skipped, failed int
			say := func(s style, format string, args ...any) { env.secho(s, format, args...) }

			for _, url := range inputs.urls {
				say(bold, "Processing URL: %s", url)
				err := func() error {
					card, err := env.fetcher().NewVCardFromURL(url, allowHTTP, verifySource)
					if err != nil {
						return err
					}
					destination := card.Path
					if outputDir != "" {
						destination = filepath.Join(outputDir, card.Path)
					}
					newText := card.String()
					oldText, haveOld := "", false
					if isFile(destination) {
						if oldText, err = readText(destination); err != nil {
							return err
						}
						haveOld = true
					}
					if haveOld && showDiff {
						env.showDiff(oldText, newText, destination, "(fetched)")
					}
					switch {
					case haveOld && oldText == newText:
						say(plain, "  Unchanged: %s (already up to date)", destination)
						unchanged++
					case dryRun:
						verb := "download"
						if haveOld {
							verb = "update"
							updated++
						} else {
							downloaded++
						}
						say(cyan, "  Dry-run: would %s %s; no changes written.", verb, destination)
					default:
						if haveOld && backup {
							backupPath := destination + ".bak"
							if err := os.WriteFile(backupPath, []byte(oldText), 0o644); err != nil {
								return err
							}
							say(blue, "  Backup created: %s", backupPath)
						}
						if err := card.ToFile(destination); err != nil {
							return err
						}
						if haveOld {
							say(green, "  Updated: %s", destination)
							updated++
						} else {
							say(green, "  Downloaded and saved to: %s -> %s", url, filepath.Base(destination))
							downloaded++
						}
					}
					return nil
				}()
				if err != nil {
					say(red, "  Failed to fetch URL %s: %s", url, pyutil.ErrText(err))
					failed++
				}
			}

			for _, file := range inputs.files {
				say(bold, "Processing: %s", file)
				card, err := core.NewVCardFromPath(file)
				if err != nil {
					say(red, "  Failed to read %s: %s", file, pyutil.ErrText(err))
					failed++
					continue
				}
				oldText := card.Profile.Raw
				source := model.Str(card.Profile.Source)
				if source == "" {
					say(yellow, "  No SOURCE property found. Skipping.")
					skipped++
					continue
				}
				say(plain, "  Fetching: %s", source)
				fetched, err := env.fetcher().NewVCardFromURL(source, allowHTTP, verifySource)
				if err != nil {
					say(red, "  Failed to fetch SOURCE: %s", pyutil.ErrText(err))
					failed++
					continue
				}
				newText := fetched.Profile.Raw
				if showDiff {
					env.showDiff(oldText, newText, file, "(fetched)")
				}
				outPath := file
				if outputDir != "" {
					outPath = filepath.Join(outputDir, filepath.Base(file))
				}
				current, haveCurrent := oldText, true
				if outputDir != "" {
					haveCurrent = isFile(outPath)
					current = ""
					if haveCurrent {
						if current, err = readText(outPath); err != nil {
							say(red, "  Failed to read %s: %s", outPath, err)
							failed++
							continue
						}
					}
				}
				if haveCurrent && current == newText {
					say(plain, "  Unchanged: %s (already up to date)", outPath)
					unchanged++
					continue
				}
				if dryRun {
					say(cyan, "  Dry-run: would update %s; no changes written.", outPath)
					updated++
					continue
				}
				if backup && pathExists(outPath) && outputDir == "" {
					backupPath := outPath + ".bak"
					if err := os.WriteFile(backupPath, []byte(oldText), 0o644); err != nil {
						say(red, "  Failed to write backup: %s", err)
						failed++
						continue
					}
					say(blue, "  Backup created: %s", backupPath)
				}
				if err := os.WriteFile(outPath, []byte(newText), 0o644); err != nil {
					say(red, "  Failed to write %s: %s", outPath, err)
					failed++
					continue
				}
				say(green, "  Updated: %s", outPath)
				updated++
			}

			env.secho(plain, "Summary:")
			pick := func(dry, real string) string {
				if dryRun {
					return dry
				}
				return real
			}
			env.secho(green, "  %s: %d", pick("Would download", "Downloaded"), downloaded)
			env.secho(green, "  %s: %d", pick("Would update", "Updated"), updated)
			env.secho(yellow, "  Unchanged: %d", unchanged)
			env.secho(yellow, "  Skipped: %d", skipped)
			env.secho(red, "  Failed: %d", failed)
			if failed > 0 {
				env.secho(red, "❌ %d item(s) failed.", failed)
				return exit(1)
			}
			env.secho(plain, "Done.")
			return nil
		}),
	}
	f := cmd.Flags()
	f.StringVarP(&outputDir, "output-dir", "o", "", "Directory to write updated vCards. Defaults to overwriting in place.")
	f.BoolVarP(&dryRun, "dry-run", "n", false, "Show what would be updated without writing any files.")
	f.BoolVarP(&backup, "backup", "b", false, "Create a .bak copy before overwriting.")
	f.BoolVarP(&showDiff, "diff", "d", false, "Show colored unified diff between old and new content.")
	f.BoolVar(&allowHTTP, "allow-http", false, "Allow plain HTTP URLs (HTTPS is required by default).")
	f.BoolVar(&verifySource, "verify-source", true, "Require the fetched vCard SOURCE to match the requested URL.")
	f.BoolVar(&noVerify, "no-verify-source", false, "Do not require the fetched vCard SOURCE to match the requested URL.")
	return cmd
}

// ---------------------------------------------------------------- create

// promptOrder is the order in which the main attributes are asked for in interactive mode.
var promptOrder = []string{"fn", "n", "nickname", "lang", "gender", "email", "categories", "bday", "anniversary",
	"kind", "adr", "tel", "impp", "photo", "note", "url", "source"}

const tempFile = "vcard_create.tmp"

// loadResumeFile loads saved prompt answers; it tolerates missing, legacy or malformed files.
func loadResumeFile(path string) *pyutil.OrderedMap {
	out := pyutil.NewOrderedMap()
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	text := string(data)
	var generic map[string]any
	if json.Unmarshal(data, &generic) == nil {
		dec := json.NewDecoder(strings.NewReader(text))
		dec.Token() // {
		for dec.More() {
			k, _ := dec.Token()
			var v any
			dec.Decode(&v)
			if s, ok := v.(string); ok {
				out.Set(fmt.Sprint(k), s)
			}
		}
		return out
	}
	// Legacy key=value format: skip lines that do not parse
	for _, line := range pyutil.SplitLines(text) {
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) != "" {
			out.Set(pyutil.Strip(k), v)
		}
	}
	return out
}

func saveResume(data *pyutil.OrderedMap) error {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range data.Keys() {
		if i > 0 {
			b.WriteString(", ")
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(data.Value(k))
		b.Write(kb)
		b.WriteString(": ")
		b.Write(vb)
	}
	b.WriteByte('}')
	part := tempFile + ".part"
	if err := os.WriteFile(part, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(part, tempFile)
}

func newCreateCmd(env *Env) *cobra.Command {
	var output string
	var interactive, generateKey, force, resume bool
	values := map[string]*string{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Generate a vCard by asking the user for information.",
		Args:  cobra.NoArgs,
		RunE: env.run("create", func(cmd *cobra.Command, args []string) error {
			attrs := pyutil.NewOrderedMap() // custom attributes
			if interactive {
				env.secho(cyan, "Let's create a new vCard!")
				temp := pyutil.NewOrderedMap()
				if resume {
					temp = loadResumeFile(tempFile)
				}
				promptWithTemp := func(key, description string, def *string) (string, error) {
					if v, ok := temp.Get(key); ok {
						def = &v
					}
					value, err := env.prompt(description, def)
					if err != nil {
						return "", err
					}
					temp.Set(key, value)
					return value, saveResume(temp)
				}
				for _, key := range promptOrder {
					a := model.MainAttributeByName(key)
					var def *string
					if a.HasDefault || cmd.Flags().Changed(key) {
						d := *values[key]
						def = &d
					}
					v, err := promptWithTemp(key, a.Description, def)
					if err != nil {
						return err
					}
					*values[key] = v
				}
				var err error
				if generateKey, err = env.confirm("Do you want to create new keys?", false); err != nil {
					return err
				}
				if err := promptFeeds(env, promptWithTemp, attrs); err != nil {
					return err
				}
				if err := promptSocial(env, promptWithTemp, attrs); err != nil {
					return err
				}
				if err := promptCustom(env, promptWithTemp, attrs); err != nil {
					return err
				}
			}

			var keys []vcard.KeySpec
			if generateKey {
				_, _, pubB64, err := crypto.ActionGenerateKeypair("vcard_private.pem", "vcard_public.pem", force)
				if err != nil {
					if crypto.IsExist(err) {
						env.secho(red, "❌ %s; refusing to overwrite a key file (use --force).", err)
						return exit(1)
					}
					return err
				}
				env.echoKeypairGenerated("vcard_private.pem", "vcard_public.pem", pubB64)
				one := int64(1)
				keys = []vcard.KeySpec{{Alg: "ed25519", KeyB64: pubB64, Pref: &one, Encoding: "b"}}
			}

			fields := vcard.Fields{
				FN: *values["fn"], Nickname: *values["nickname"], Lang: *values["lang"], Email: *values["email"], Kind: *values["kind"],
				N: vcard.Text(*values["n"]), Gender: vcard.Text(*values["gender"]), Categories: vcard.Text(*values["categories"]),
				Adr: vcard.Text(*values["adr"]), Bday: *values["bday"], Anniversary: *values["anniversary"],
				Tel: *values["tel"], Impp: *values["impp"], Photo: *values["photo"], Note: *values["note"],
				URL: *values["url"], Source: *values["source"], Keys: keys,
			}
			for _, k := range attrs.Keys() {
				fields.CustomAttributes = append(fields.CustomAttributes, vcard.Attribute{Name: k, Value: attrs.Value(k)})
			}
			content, err := vcard.BuildContent(fields)
			if err != nil {
				return err
			}

			env.secho(cyan, "\nSummary of the vCard:")
			env.echo("%s", content)
			env.echo("\n")

			if interactive {
				save, err := env.confirm("Do you want to save this vCard to the file?", true)
				if err != nil {
					return err
				}
				if !save {
					env.secho(red, "❌ Operation canceled. The vCard was not saved.")
					return exit(1)
				}
			}
			if dir := filepath.Dir(output); dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			if err := os.WriteFile(output, []byte(content), 0o644); err != nil {
				return err
			}
			env.secho(green, "✅ vCard generated and saved to %s", output)
			if *values["source"] == "" {
				env.secho(yellow, "⚠️  No SOURCE: the card will not pass `vcard validate` until you set one "+
					"(--source https://your.host/dsi.vcf).")
			}
			if interactive {
				// Saved successfully: the in-progress answers are no longer needed
				os.Remove(tempFile)
				os.Remove(tempFile + ".part")
			}
			return nil
		}),
	}
	f := cmd.Flags()
	f.StringVarP(&output, "output", "o", "dsi-card.vcf", "Output file to save the vCard")
	f.BoolVarP(&interactive, "interactive", "i", false, "Interactive prompts")
	for _, a := range model.MainAttributes {
		v := new(string)
		values[a.Name] = v
		f.StringVar(v, a.Name, a.Default, a.Description)
	}
	f.BoolVar(&generateKey, "generate-key", false,
		"Generate a new Ed25519 keypair and save to PEM files (vcard_private.pem and vcard_public.pem)")
	f.BoolVar(&force, "force", false, "With --generate-key, overwrite existing key files")
	f.BoolVar(&resume, "resume", false,
		"With --interactive, reuse the answers saved in vcard_create.tmp by a previous run that was cancelled "+
			"or interrupted as prompt defaults. The file is kept on cancel/abort so it can be resumed, ignored "+
			"without --resume, and deleted after the vCard is saved.")
	return cmd
}

type promptFn func(key, description string, def *string) (string, error)

func emptyDefault() *string { s := ""; return &s }

func promptFeeds(env *Env, prompt promptFn, attrs *pyutil.OrderedMap) error {
	add, err := env.confirm("Do you want to add a X-FEED attribute for an RSS feed?", false)
	if err != nil {
		return err
	}
	if add {
		url, err := prompt("x_feed", "Enter the RSS feed URL", emptyDefault())
		if err != nil {
			return err
		}
		attrs.Set("X-FEED", url)
	}
	env.echo("ℹ️ If you have feeds in different languages, add X-FEED;LANGUAGE:language-region to the vCard.")
	type langFeed struct{ language, url string }
	var custom []langFeed
	add, err = env.confirm("Do you want to add custom X-FEED;LANGUAGE:language-region entries?", false)
	if err != nil {
		return err
	}
	for add {
		language, err := prompt(fmt.Sprintf("x_feed_language_%d", len(custom)), "Enter the language-region (e.g., 'en-US', 'es-ES')", emptyDefault())
		if err != nil {
			return err
		}
		url, err := prompt(fmt.Sprintf("x_feed_url_%d", len(custom)), "Enter the feed URL", emptyDefault())
		if err != nil {
			return err
		}
		custom = append(custom, langFeed{pyutil.Strip(language), pyutil.Strip(url)})
		if add, err = env.confirm("Do you want to add another X-FEED;LANGUAGE entry?", false); err != nil {
			return err
		}
	}
	for _, c := range custom {
		attrs.Set("X-FEED;LANGUAGE="+c.language, c.url)
	}
	return nil
}

func promptSocial(env *Env, prompt promptFn, attrs *pyutil.OrderedMap) error {
	add, err := env.confirm("Do you want to add custom attributes for social media links?", false)
	if err != nil {
		return err
	}
	for add {
		platform, err := env.prompt("Enter the name of the social platform (it will be prefixed with 'X-SOCIAL;PLATFORM=')", nil)
		if err != nil {
			return err
		}
		name := vcard.BuildSocialPlatformAttribute(platform)
		value, err := prompt("x_"+name, "Enter the value for "+name, emptyDefault())
		if err != nil {
			return err
		}
		attrs.Set(name, value)
		if add, err = env.confirm("Do you want to add another social media link?", false); err != nil {
			return err
		}
	}
	return nil
}

func promptCustom(env *Env, prompt promptFn, attrs *pyutil.OrderedMap) error {
	add, err := env.confirm("Do you want to add custom attributes for other information?", false)
	if err != nil {
		return err
	}
	for add {
		raw, err := env.prompt("Enter the name of the custom attribute (it will be prefixed with 'X-')", nil)
		if err != nil {
			return err
		}
		name := vcard.BuildCustomAttribute(raw)
		value, err := prompt("x_"+name, "Enter the value for X-"+name, emptyDefault())
		if err != nil {
			return err
		}
		attrs.Set("X-"+name, value)
		if add, err = env.confirm("Do you want to add another custom attribute?", false); err != nil {
			return err
		}
	}
	return nil
}
