# Golden fixtures

`golden/` holds **frozen fixtures** that were generated from the original **Python implementation**
(`dsipy`, commit `f82f00e`, the last Python commit before the Go migration; library versions in
`golden/VERSIONS.txt`). They are the byte-level spec the Go port is tested against: every Go package
reads them through `internal/testutil`. Do not edit them by hand.

The generator (`testdata/generate.py`) and the Python/Go differential check (`testdata/parity/parity.py`,
209 steps, 0 unexpected differences when the port was finished) were removed together with the Python
code; they remain in the git history (any commit before "chore: remove the Python implementation").
To regenerate the fixtures, check out `f82f00e` (or that history) in a separate worktree and run the
generator there with the Python dependencies installed.


| Directory | Content | Used by |
|---|---|---|
| `crypto/` | strict Base64 decoding (Python's error messages), key loading errors, PEM conversion | crypto |
| `qr/` | QR module matrices and image size from `qrcode` (H level, box 10, border 4) | qr |
| `keys/` | Ed25519 test keys (seed = name padded to 32 bytes), PEM + base64 DER | crypto |
| `canonical/` | endorsement / feed-item canonical strings and signatures | crypto, feeds |
| `escaping/` | escape/unescape/split/fold/slugify/format_property cases | vcard |
| `vcards/<case>.vcf` | input cards | |
| `vcards/<case>.parse.json` | `asdict(Profile)` | parser |
| `vcards/<case>.normalized.vcf` / `.normalize_error.txt` | `normalize_vcard` | canonical |
| `vcards/<case>.validate.json` | `validate --json` | validator |
| `vcards/<case>.verify.json` | endorsement verification results | endorsements |
| `vcards/<case>.rebuilt.vcf` | `build_vcard_from_raw_lines` | serializer |
| `vcards/build_content.json` | `build_content` args → output | serializer |
| `lifecycle/` | revoke / rotate / add results (fixed `when`) | lifecycle |
| `resolver/` | `normalize_url`, `safe_filename`, `is_global`, `urljoin`, `_validate_url`, UTF-8 decode errors | core |
| `feeds/` | posts, `collect.json`, RSS (signed/unsigned/empty/tampered), verify results, templating inputs, `iso_dates.json` (`fromisoformat` incl. a seeded fuzz corpus), `markdown_cases.json` | feeds |
| `opml/` | OPML output and warnings | feeds |

Notes for the port:
- RSS from `rfeed` includes `<generator>rfeed v1.1.1</generator>` and a `<docs>`
  element, `xmlns:dc`, and empty-element style `<x></x>`; the golden files keep these.
- HTML items are wrapped in CDATA by `feeds build` (`_apply_templates`), not by `RSSFeed.build`.
- `*.pem` files in `golden/keys` are test-only keys from public seeds (un-ignored in `.gitignore`).

Known, deliberate differences from the Python implementation (all covered by tests):
- Markdown → HTML: goldmark follows CommonMark, Python-Markdown the original rules; `knownMarkdownDiffs`
  in `internal/feeds/feeds_test.go` lists the 17 of 49 snippets that differ (abbr, attr_list, md_in_html
  are unsupported).
- QR codes: same version/size and error correction as `qrcode`, always decodable, but module masks can
  differ (1 of 14 matrices is bit-identical); the logo is resized with Catmull-Rom instead of Lanczos.
- Validator: URLs that make Python's validator crash (`https://[::1/x`) are reported as invalid URLs;
  duplicate warnings are listed in order of occurrence (Python iterates an unordered set).
- cryptography's version-specific `Details: ...` suffix of DER errors is not reproduced.
- `lastBuildDate`/`pubDate` use UTC wall-clock time (Python used the local time labelled GMT).

## Parity check (historical)

Before the Python code was removed, `testdata/parity/parity.py` ran the same command lines through the
Python CLI and the Go binary and compared exit codes, stdout, stderr and the files left behind (about 200
steps, plus cross-verification of feeds, endorsements and keys between the two). The only differences
were the intentional ones below; the rest were identical.

- Expected differences: the key-parsing library's error text, Python's unordered confidence-level list,
  DNS error text and the rich progress bar. Help screens differ by design (typer/rich vs cobra).
- `dsi --version` and the `dsi plugin` commands are new in Go.
