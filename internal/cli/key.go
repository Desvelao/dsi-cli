package cli

import (
	"errors"
	"os"
	"strings"

	"github.com/Desvelao/dsi-cli/internal/core"
	"github.com/Desvelao/dsi-cli/internal/crypto"
	"github.com/Desvelao/dsi-cli/internal/pyutil"
	"github.com/Desvelao/dsi-cli/internal/vcard"
	"github.com/spf13/cobra"
)

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readText reads a file as UTF-8 text.
func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if msg, bad := core.Utf8DecodeError(data); bad {
		return "", errors.New(msg)
	}
	return string(data), nil
}

func (e *Env) echoKeypairGenerated(priv, pub, pubB64 string) {
	e.secho(green, "✅ Keypair generated and saved to '%s' and '%s'", priv, pub)
	e.secho(blue, "📋 Public key (Base64-encoded DER for vCard): %s", pubB64)
}

func newKeyCmd(env *Env) *cobra.Command {
	group := subcommand("key", "Commands related to keys")

	// create
	{
		var priv, pub string
		var force bool
		cmd := &cobra.Command{
			Use:   "create",
			Short: "Generate a new Ed25519 keypair and save to PEM files",
			Args:  cobra.NoArgs,
			RunE: env.run("create", func(cmd *cobra.Command, args []string) error {
				_, _, pubB64, err := crypto.ActionGenerateKeypair(priv, pub, force)
				if err != nil {
					if crypto.IsExist(err) {
						env.secho(red, "❌ %s; refusing to overwrite a key file (use --force).", err)
						return exit(1)
					}
					return err
				}
				env.echoKeypairGenerated(priv, pub, pubB64)
				return nil
			}),
		}
		cmd.Flags().StringVar(&priv, "priv", "private.pem", "Path to save the private key PEM file")
		cmd.Flags().StringVar(&pub, "pub", "public.pem", "Path to save the public key PEM file")
		cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing key files")
		group.AddCommand(cmd)
	}

	// pub-encode
	group.AddCommand(&cobra.Command{
		Use:   "pub-encode FILE",
		Short: "Convert a public key PEM file to Base64-encoded DER format for vCard use",
		Args:  cobra.ExactArgs(1),
		RunE: env.run("pub_encode", func(cmd *cobra.Command, args []string) error {
			file := args[0]
			if !isFile(file) {
				env.secho(red, "❌ '%s' is not a file.", file)
				return exit(1)
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			pub, err := crypto.LoadPublicKeyPEM(data)
			if err != nil {
				return err
			}
			b64, err := crypto.PublicKeyToB64DER(pub)
			if err != nil {
				return err
			}
			env.echo("%s", b64)
			return nil
		}),
	})

	// pub-decode
	group.AddCommand(&cobra.Command{
		Use:   "pub-decode [CONTENT]",
		Short: "Convert a Base64-encoded DER format for vCard use to public key PEM file",
		Args:  cobra.MaximumNArgs(1),
		RunE: env.run("pub_decode", func(cmd *cobra.Command, args []string) error {
			var content string
			if len(args) == 1 {
				content = args[0]
			} else {
				text, err := env.readAllStdin()
				if err != nil {
					return err
				}
				content = pyutil.Strip(text)
			}
			if content == "" {
				env.secho(red, "❌ No content provided.")
				return exit(1)
			}
			pem, err := crypto.B64DERToPublicKeyPEM(content)
			if err != nil {
				return err
			}
			env.echo("%s", pem)
			return nil
		}),
	})

	// rotate
	{
		var priv, pub, oldKey, reason, output string
		cmd := &cobra.Command{
			Use: "rotate VCARD",
			Short: "Rotate the preferred key of a vCard: generate a new keypair, make it the " +
				"preferred KEY and add a REVKEY for the old one.",
			Args: cobra.ExactArgs(1),
			RunE: env.run("rotate", func(cmd *cobra.Command, args []string) error {
				vcardPath := args[0]
				if !isFile(vcardPath) {
					env.secho(red, "❌ '%s' is not a file.", vcardPath)
					return exit(1)
				}
				for _, p := range []string{priv, pub} {
					if pathExists(p) {
						env.secho(red, "❌ '%s' already exists; refusing to overwrite a key file.", p)
						return exit(1)
					}
				}
				privPEM, pubPEM, newB64, err := crypto.GenerateKeypair()
				if err != nil {
					return err
				}
				text, err := readText(vcardPath)
				if err != nil {
					return err
				}
				updated, err := core.RotateKey(text, newB64, oldKey, reason, env.now())
				if err != nil {
					return err
				}
				if err := crypto.WritePrivateKey(priv, privPEM, false); err != nil {
					return err
				}
				if err := os.WriteFile(pub, pubPEM, 0o644); err != nil {
					return err
				}
				target := vcardPath
				if output != "" {
					target = output
				}
				if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
					return err
				}
				env.secho(green, "✅ Key rotated in '%s'", target)
				env.secho(blue, "   New private key: %s", priv)
				env.secho(blue, "   New public key (Base64 DER): %s", newB64)
				return nil
			}),
		}
		cmd.Flags().StringVar(&priv, "priv", "private.pem", "Where to save the new private key PEM")
		cmd.Flags().StringVar(&pub, "pub", "public.pem", "Where to save the new public key PEM")
		cmd.Flags().StringVar(&oldKey, "old-key", "", "Base64 DER of the key to revoke (default: the PREF=1 key)")
		cmd.Flags().StringVar(&reason, "reason", "rotated", "Revocation reason: rotated or superseded")
		cmd.Flags().StringVarP(&output, "output", "o", "", "Write the result here instead of updating the vCard")
		group.AddCommand(cmd)
	}

	// add
	{
		var priv, pub, publicKey, output string
		var pref bool
		cmd := &cobra.Command{
			Use: "add VCARD",
			Short: "Add a new key to a vCard without revoking anything (use it when every " +
				"key is revoked, or to add an extra key).",
			Args: cobra.ExactArgs(1),
			RunE: env.run("add", func(cmd *cobra.Command, args []string) error {
				vcardPath := args[0]
				if !isFile(vcardPath) {
					env.secho(red, "❌ '%s' is not a file.", vcardPath)
					return exit(1)
				}
				var privPEM, pubPEM []byte
				var newB64 string
				generated := !cmd.Flags().Changed("public-key")
				if generated {
					for _, p := range []string{priv, pub} {
						if pathExists(p) {
							env.secho(red, "❌ '%s' already exists; refusing to overwrite a key file.", p)
							return exit(1)
						}
					}
					var err error
					if privPEM, pubPEM, newB64, err = crypto.GenerateKeypair(); err != nil {
						return err
					}
				} else {
					newB64 = pyutil.Strip(publicKey)
				}
				text, err := readText(vcardPath)
				if err != nil {
					return err
				}
				updated, err := core.AddKey(text, newB64, pref)
				if err != nil {
					return err
				}
				if generated {
					if err := crypto.WritePrivateKey(priv, privPEM, false); err != nil {
						return err
					}
					if err := os.WriteFile(pub, pubPEM, 0o644); err != nil {
						return err
					}
				}
				target := vcardPath
				if output != "" {
					target = output
				}
				if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
					return err
				}
				env.secho(green, "✅ Key added to '%s'", target)
				if generated {
					env.secho(blue, "   New private key: %s", priv)
				}
				env.secho(blue, "   New public key (Base64 DER): %s", newB64)
				return nil
			}),
		}
		cmd.Flags().StringVar(&priv, "priv", "private.pem", "Where to save the new private key PEM")
		cmd.Flags().StringVar(&pub, "pub", "public.pem", "Where to save the new public key PEM")
		cmd.Flags().StringVar(&publicKey, "public-key", "", "Add this existing Base64 DER public key instead of generating a keypair")
		cmd.Flags().BoolVar(&pref, "pref", true, "Make the new key the preferred one (PREF=1); the other keys lose PREF")
		cmd.Flags().Bool("no-pref", false, "Do not make the new key the preferred one")
		cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
			if v, _ := cmd.Flags().GetBool("no-pref"); v {
				pref = false
			}
			return nil
		}
		cmd.Flags().StringVarP(&output, "output", "o", "", "Write the result here instead of updating the vCard")
		group.AddCommand(cmd)
	}

	// revoke
	{
		var key, pub, reason, output string
		cmd := &cobra.Command{
			Use:   "revoke VCARD",
			Short: "Revoke a key listed in a vCard by adding a REVKEY property.",
			Args:  cobra.ExactArgs(1),
			RunE: env.run("revoke", func(cmd *cobra.Command, args []string) error {
				vcardPath := args[0]
				if !isFile(vcardPath) {
					env.secho(red, "❌ '%s' is not a file.", vcardPath)
					return exit(1)
				}
				haveKey, havePub := cmd.Flags().Changed("key"), cmd.Flags().Changed("pub")
				if haveKey == havePub {
					env.secho(red, "❌ Pass exactly one of --key or --pub.")
					return exit(1)
				}
				keyB64 := key
				if !haveKey {
					data, err := os.ReadFile(pub)
					if err != nil {
						return err
					}
					pk, err := crypto.LoadPublicKeyPEM(data)
					if err != nil {
						return err
					}
					if keyB64, err = crypto.PublicKeyToB64DER(pk); err != nil {
						return err
					}
				}
				text, err := readText(vcardPath)
				if err != nil {
					return err
				}
				updated, err := core.RevokeKey(text, keyB64, reason, env.now())
				if err != nil {
					return err
				}
				target := vcardPath
				if output != "" {
					target = output
				}
				if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
					return err
				}
				env.secho(green, "✅ Key revoked (%s) in '%s'", reason, target)

				remaining := vcard.ParseVCard(updated)
				revoked := map[string]bool{}
				for _, r := range remaining.Revocations {
					revoked[r.KeyB64] = true
				}
				usable, preferred := false, false
				for _, k := range remaining.Keys {
					usable = usable || !revoked[k.KeyB64]
					preferred = preferred || (k.Pref != nil && *k.Pref == 1)
				}
				if !usable {
					env.secho(yellow, "⚠️ The vCard has no usable key left. Add one with `dsi key add`.")
				} else if reason != "deprecated" && !preferred {
					env.secho(yellow, "⚠️ No key is preferred (PREF=1) now. Use `dsi key add` or edit the vCard.")
				}
				return nil
			}),
		}
		cmd.Flags().StringVar(&key, "key", "", "Base64 DER of the key to revoke")
		cmd.Flags().StringVar(&pub, "pub", "", "Public key PEM of the key to revoke (alternative to --key)")
		cmd.Flags().StringVar(&reason, "reason", "", "compromised, rotated, superseded, retired, lost or deprecated")
		cmd.MarkFlagRequired("reason")
		cmd.Flags().StringVarP(&output, "output", "o", "", "Write the result here instead of updating the vCard")
		group.AddCommand(cmd)
	}
	_ = strings.TrimSpace
	return group
}
