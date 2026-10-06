# Migrating from the Python `dsipy`

`dsi` is a rewrite of the Python tool `dsipy` as a single static Go binary. The file formats, the
cryptography (Ed25519 keys, signatures, canonical strings), the command tree and the flag names are the
same, and the output of the two tools was compared on about 200 command lines (feeds and endorsements
signed by one verify with the other). This page lists what changed.

## Renames

| Python | Go |
|---|---|
| command `dsipy` (`pip install`, needs Python ≥ 3.12 and its dependencies) | command **`dsi`** (one binary from the Releases page; no `dsipy` alias is provided) |
| `DSIPY_DEBUG=1` | `DSI_DEBUG=1` (`DSIPY_DEBUG` is still honored) |
| `dsipy feeds publish` (GitHub Pages, S3) | not in the core; meant to be the `dsi-publish` [plugin](plugins.md) |
| repository `Desvelao/dsipy`, Python package `dsipy` | repository `Desvelao/dsi-cli`, Go module `github.com/Desvelao/dsi-cli` |
| `.github/workflows/gha-build-feeds.yml` inputs `wheel_tag`, secret `wheel_repo_token` | `dsi_tag` (default `v0.1.0-alpha1`), secret `dsi_repo_token`; the workflow downloads the `dsi_linux_amd64` binary and verifies it against `checksums.txt` |

Callers of the reusable workflow that move to the new version must rename those two inputs; the flags of
`dsi feeds build` used by the workflow did not change.

## New

- `dsi --version`.
- `dsi plugin list` and the plugin mechanism: `dsi <name>` runs `dsi-<name>` ([plugins](plugins.md)).
- Release binaries for linux, macOS and Windows (amd64 and arm64) with `checksums.txt`.

## Behaviour differences

- A command group run without a subcommand (`dsi vcard`) shows its help and exits with `2` (as before for
  `dsipy vcard`); `dsi` alone shows the help and exits `0`. Usage errors exit `2`.
- Help screens are laid out by cobra instead of typer/rich (same commands, options and descriptions);
  there is no progress bar in `vcard fetch` (the per-item lines are the same).
- `feeds build`: Markdown posts with `use_html_content` are rendered with goldmark (CommonMark) instead of
  Python-Markdown. Common Markdown renders the same; some edge cases differ (nested/adjacent lists,
  `1)` lists, entity handling, footnotes, and the `abbr`, `attr_list` and `md_in_html` extensions, which
  are not supported). If you sign feeds, the signature covers the HTML that was produced, so a feed built
  by one tool verifies with the other as long as the file itself is unchanged.
- `lastBuildDate` is written in UTC (Python wrote the local time and labelled it GMT).
- `vcard qr`: same size and error correction level and always decodable, but the module mask may differ
  from Python's image, and logos are resized with Catmull-Rom instead of Lanczos. Output formats: PNG,
  JPEG and GIF.
- `vcard validate`: URLs that crashed the Python validator (for example `https://[::1/x`) are reported as
  invalid URLs; duplicate warnings appear in order of occurrence.
- Library error texts (for example for a malformed private key) come from Go's libraries.
- The confidence levels in the `vcard endorse -c` error are listed in a fixed order (`low, medium, high`).
- Files in directory inputs are visited in lexical order.

The full list of deliberate differences, with the tests that pin them, is in
[`testdata/README.md`](../testdata/README.md).
