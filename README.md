# dsipy

This repository provides the `dsipy` tool that allows creating, validating, signing and publishing some resources for the decentralized social identity, see [dsi-spec](https://github.com/Desvelao/dsi-spec) for more information.

Based on https://nfraprado.net/post/vcard-rss-as-an-alternative-to-social-media.html

New here? Read the short [getting started guide](docs/getting-started.md). More: [command reference](docs/commands.md), [publishing feeds and templates](docs/publishing.md), [hands-on testing guide](docs/testing-guide.md) (try every feature yourself).

# Join to the network

1. Create a vCard with your info according to the [dsi-spec](https://github.com/Desvelao/dsi-spec) (`dsipy vcard create --generate-key`)
2. Validate it (`dsipy vcard validate dsi-card.vcf`)
3. Optionally, create a rss or atom file for your feeds.
4. Host the files in a public-accesible web:
    - vCard
    - feeds (rss and resources referenced by the states)
5. Share the link to your vCard to your connections

See [dsi-publish-template-github-pages](https://github.com/Desvelao/dsi-publish-template-github-pages) for a way to publish the feeds with GitHub Pages.

# Commands

| Command | Purpose |
|---|---|
| `dsipy vcard create` | Create a vCard (`--interactive`, `--resume`, `--generate-key`, `--force`) |
| `dsipy vcard validate <file\|url\|->` | Validate a vCard against the specification (`--json`, `--strict`) |
| `dsipy vcard inspect <file\|url\|->` | Read-only summary: identity, keys, feeds, social, endorsements |
| `dsipy vcard normalize <file\|url\|->` | Print the deterministic form of a vCard (`--write` overwrites a local file) |
| `dsipy vcard verify <file\|url\|->` | Verify the `X-ENDORSE` signatures against the vCard's own keys |
| `dsipy vcard endorse` | Sign endorsements for other vCards (`--vcard <dest> --write` adds them) |
| `dsipy vcard fetch` | Update vCards from their `SOURCE`, or download from URLs (`--dry-run`, `--diff`, `--backup`) |
| `dsipy vcard parse [file\|url\|-]` | Print the parsed profile as JSON (reads stdin when piped) |
| `dsipy vcard qr` | Generate a QR code (`-o`, `-i`, `-t`, `-b`, `-f`) |
| `dsipy key create` | Create an Ed25519 keypair (PEM); `--force` to overwrite |
| `dsipy key add <vcard>` | Add a new key (works when every key is revoked); `--public-key` adds an existing one |
| `dsipy key rotate <vcard>` | New preferred key + `REVKEY` for the old one |
| `dsipy key revoke <vcard>` | Add a `REVKEY` (`--reason compromised\|rotated\|superseded\|retired\|lost\|deprecated`) |
| `dsipy key pub-encode` / `pub-decode` | Convert a public key between PEM and Base64 DER |
| `dsipy feeds init` / `add` / `build` | Initialize the source directory, add posts, build (optionally signed) RSS feeds; `build` supports `{{ }}` templates (`--var`, `--var-file`) and `--limit` |
| `dsipy feeds publish <files\|dirs>` | Publish feeds to `--provider github` or `s3` (`--arg key=value`, `--prefix`, `--diff`, `--dry-run`); see [publishing](docs/publishing.md) |
| `dsipy feeds verify <rss>` | Verify signed items (`--vcard` or `--pub`) |
| `dsipy connections feed <files\|dirs>` | Generate an OPML file (one outline per `X-FEED`) from vCards (`-o`) |

Full option lists: [docs/commands.md](docs/commands.md) or `dsipy <group> <command> --help`. Add `--debug` (or `DSIPY_DEBUG=1`) before the group to print tracebacks on errors.

## Exit codes

`0` on success. `1` when a command fails, an input cannot be processed, or validation finds errors (also `fetch`/`endorse` when any item failed, and `validate --strict` when there are warnings).

## `validate --json`

```json
{"valid": false, "errors": [{"code": "source-missing", "message": "SOURCE property is required"}], "warnings": []}
```

Use it as a gate in CI: `dsipy vcard validate dsi.vcf` exits with `1` if the vCard is invalid.

## Remote fetching

`vcard fetch` and the commands that accept a URL only fetch over HTTPS (`--allow-http` to relax it), follow at most 5 redirects, refuse hosts that resolve to non-public addresses (loopback, private ranges, link-local), limit the response to 1 MB and check the content type. After fetching, the `SOURCE` of the returned vCard must match the requested URL (`--no-verify-source` disables the check); otherwise nothing is written.

Limitation: the host is checked just before connecting, so DNS rebinding with a very short TTL is not fully excluded; run the tool in a network that cannot reach internal services if that matters.

# Project layout

```
src/dsipy/
  core/          model, identity (VCard), validator, canonical strings + normalization,
                 key lifecycle, secure HTTP fetch and SOURCE-verifying resolver
  vcard/         parser (vobject line parsing), serializer, TEXT escaping, QR
  crypto/        Ed25519 keys, signatures, verification
  endorsements/  endorsement model and verification
  feeds/         RSS building, item signing/verification, markdown sources, OPML
  publishing/    GitHub and S3 publishers
  cli/           Typer apps (the only package that prints or exits)
```

The library packages do not depend on `cli`.

# Development

Python is run in the dev container:

```sh
docker compose -f dev/docker-compose.yml up -d
docker compose -f dev/docker-compose.yml exec dev sh -c "cd /app && pip install -e '.[dev]' && python -m pytest -q"
docker compose -f dev/docker-compose.yml exec dev sh -c "cd /app && black src tests"
```

# License

This project is licensed under the [MIT License](https://opensource.org/licenses/MIT).
