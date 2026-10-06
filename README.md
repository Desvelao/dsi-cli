# dsi-cli

`dsi` is a command line tool for creating, validating, signing and publishing the resources of a decentralized social identity (a vCard, signed RSS feeds and OPML lists). See [dsi-spec](https://github.com/Desvelao/dsi-spec) for more information about the specification.

It is distributed as **a single static binary** (no runtime, no dependencies) and can be extended with [plugins](docs/plugins.md).

Based on https://nfraprado.net/post/vcard-rss-as-an-alternative-to-social-media.html

New here? Read the short [getting started guide](docs/getting-started.md). More: [command reference](docs/commands.md), [feed templates](docs/publishing.md), [plugins](docs/plugins.md), [hands-on testing guide](docs/testing-guide.md), [migrating from the Python `dsipy`](docs/migrating-from-python.md).

# Install

Download the binary for your platform from the [Releases](https://github.com/Desvelao/dsi-cli/releases) page (`dsi_linux_amd64`, `dsi_linux_arm64`, `dsi_darwin_amd64`, `dsi_darwin_arm64`, `dsi_windows_amd64.exe`, `dsi_windows_arm64.exe`), check it against `checksums.txt` and put it on your `PATH`:

```sh
tag=v0.1.0-alpha1
curl -fsSLO "https://github.com/Desvelao/dsi-cli/releases/download/$tag/dsi_linux_amd64"
curl -fsSLO "https://github.com/Desvelao/dsi-cli/releases/download/$tag/checksums.txt"
sha256sum -c checksums.txt --ignore-missing
install -m 0755 dsi_linux_amd64 ~/.local/bin/dsi
dsi --version
```

With a Go toolchain you can also build it from source: `go install github.com/Desvelao/dsi-cli/cmd/dsi@v0.1.0-alpha1`.

> The first releases (`v0.1.0-alpha*`) are pre-releases. The previous Python tool was called `dsipy`; the command is now `dsi` and there is no `dsipy` alias.

# Join to the network

1. Create a vCard with your info according to the [dsi-spec](https://github.com/Desvelao/dsi-spec) (`dsi vcard create --generate-key`)
2. Validate it (`dsi vcard validate dsi-card.vcf`)
3. Optionally, create a rss or atom file for your feeds.
4. Host the files in a public-accesible web:
    - vCard
    - feeds (rss and resources referenced by the states)
5. Share the link to your vCard to your connections

See [dsi-publish-template-github-pages](https://github.com/Desvelao/dsi-publish-template-github-pages) for a way to publish the feeds with GitHub Pages. The reusable workflow `.github/workflows/gha-build-feeds.yml` of this repository builds (and optionally signs) the feed in CI.

# Commands

| Command | Purpose |
|---|---|
| `dsi vcard create` | Create a vCard (`--interactive`, `--resume`, `--generate-key`, `--force`) |
| `dsi vcard validate <file\|url\|->` | Validate a vCard against the specification (`--json`, `--strict`) |
| `dsi vcard inspect <file\|url\|->` | Read-only summary: identity, keys, feeds, social, endorsements |
| `dsi vcard normalize <file\|url\|->` | Print the deterministic form of a vCard (`--write` overwrites a local file) |
| `dsi vcard verify <file\|url\|->` | Verify the `X-ENDORSE` signatures against the vCard's own keys |
| `dsi vcard endorse` | Sign endorsements for other vCards (`--vcard <dest> --write` adds them) |
| `dsi vcard fetch` | Update vCards from their `SOURCE`, or download from URLs (`--dry-run`, `--diff`, `--backup`) |
| `dsi vcard parse [file\|url\|-]` | Print the parsed profile as JSON (reads stdin when piped) |
| `dsi vcard qr` | Generate a QR code (`-o`, `-i`, `-t`, `-b`, `-f`) |
| `dsi key create` | Create an Ed25519 keypair (PEM); `--force` to overwrite |
| `dsi key add <vcard>` | Add a new key (works when every key is revoked); `--public-key` adds an existing one |
| `dsi key rotate <vcard>` | New preferred key + `REVKEY` for the old one |
| `dsi key revoke <vcard>` | Add a `REVKEY` (`--reason compromised\|rotated\|superseded\|retired\|lost\|deprecated`) |
| `dsi key pub-encode` / `pub-decode` | Convert a public key between PEM and Base64 DER |
| `dsi feeds init` / `add` / `build` | Initialize the source directory, add posts, build (optionally signed) RSS feeds; `build` supports `{{ }}` templates (`--var`, `--var-file`) and `--limit` |
| `dsi feeds verify <rss>` | Verify signed items (`--vcard` or `--pub`) |
| `dsi connections feed <files\|dirs>` | Generate an OPML file (one outline per `X-FEED`) from vCards (`-o`) |
| `dsi plugin list` | List the installed [plugins](docs/plugins.md); `dsi <name>` runs the plugin `dsi-<name>` |

Full option lists: [docs/commands.md](docs/commands.md) or `dsi <group> <command> --help`. Add `--debug` (or `DSI_DEBUG=1`) before the group to print stack traces on errors.

Publishing feeds to GitHub or S3 is not part of the core: it is meant to be provided by the `dsi-publish` plugin (see [docs/publishing.md](docs/publishing.md)).

## Exit codes

`0` on success. `1` when a command fails, an input cannot be processed, or validation finds errors (also `fetch`/`endorse` when any item failed, and `validate --strict` when there are warnings). `2` for usage errors (unknown command or flag, missing argument) and for a command group run without a subcommand.

## `validate --json`

```json
{"valid": false, "errors": [{"code": "source-missing", "message": "SOURCE property is required"}], "warnings": []}
```

Use it as a gate in CI: `dsi vcard validate dsi.vcf` exits with `1` if the vCard is invalid.

## Remote fetching

`vcard fetch` and the commands that accept a URL only fetch over HTTPS (`--allow-http` to relax it), follow at most 5 redirects, refuse hosts that resolve to non-public addresses (loopback, private ranges, link-local), limit the response to 1 MB with connect/read timeouts, accept only text-like content types and ignore proxy environment variables. The connection is pinned to the address that was validated (TLS still checks the original host name), so DNS rebinding cannot swap the address between the check and the connection. URLs with embedded credentials are rejected, and `vcard fetch` requires the fetched card's `SOURCE` to match the requested URL unless `--no-verify-source` is given.

# Project layout

```
cmd/dsi/              main (version is injected at build time)
internal/
  cli/                cobra commands (the only package that prints or exits) and plugin dispatch
  core/               validator, VCard, key lifecycle, secure HTTP fetch, URL resolution, file helpers
  vcard/              parser, serializer, TEXT escaping, QR
  model/              Profile and related types
  canonical/          canonical strings and vCard normalization
  crypto/             Ed25519 keys, signatures, verification
  endorsements/       endorsement verification and revocation rules
  feeds/              RSS building, item signing/verification, markdown sources, OPML
  plugin/             plugin discovery
  pyutil/             helpers reproducing the string semantics of the original Python tool
  testutil/           loader of the golden fixtures
testdata/golden/      frozen fixtures that the Go tests compare against (see testdata/README.md)
```

The library packages do not depend on `cli`.

# Development

Go is run in a Docker dev container, so no local Go toolchain is needed:

```sh
make test     # go test ./...
make lint     # go vet + gofmt check
make build    # static binary at bin/dsi
make fmt      # gofmt -w
make shell    # a shell in the dev container
```

Release binaries are built by `.github/workflows/release.yml` when a `v*` tag is pushed.

# License

This project is licensed under the [MIT License](https://opensource.org/licenses/MIT).
