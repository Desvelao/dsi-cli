# Golden fixtures

`golden/` is generated from the **Python reference implementation** and is the
byte-level spec for the Go port. Do not edit it by hand.

    make golden      # regenerate (runs testdata/generate.py in the `py` container)

Generation is deterministic (fixed key seeds and dates, no dependence on the
clock or cwd); re-running must produce no diff unless the Python code changed.
`golden/VERSIONS.txt` records the library versions used (rfeed, opyml, markdown…).

| Directory | Content | Used by |
|---|---|---|
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
| `resolver/` | `normalize_url`, `safe_filename` | core |
| `feeds/` | posts, `collect.json`, RSS (signed/unsigned/empty/tampered), verify results, templating inputs | feeds |
| `opml/` | OPML output and warnings | feeds |

Notes for the port:
- RSS from `rfeed` includes `<generator>rfeed v1.1.1</generator>` and a `<docs>`
  element, `xmlns:dc`, and empty-element style `<x></x>`; the golden files keep these.
- HTML items are wrapped in CDATA by `feeds build` (`_apply_templates`), not by `RSSFeed.build`.
- `*.pem` files in `golden/keys` are test-only keys from public seeds (un-ignored in `.gitignore`).
