# Golden fixtures

`golden/` holds **frozen fixtures**: the byte-level spec the code is tested against. Every package
reads them through `internal/testutil`. Do not edit them by hand.

| Directory | Content | Used by |
|---|---|---|
| `crypto/` | strict Base64 decoding (exact error messages), key loading errors, PEM conversion | crypto |
| `qr/` | QR module matrices and image size (H level, box 10, border 4) | qr |
| `keys/` | Ed25519 test keys (seed = name padded to 32 bytes), PEM + base64 DER | crypto |
| `canonical/` | endorsement / feed-item canonical strings and signatures | crypto, feeds |
| `escaping/` | escape/unescape/split/fold/slugify/format_property cases | vcard |
| `vcards/<case>.vcf` | input cards | |
| `vcards/<case>.parse.json` | `asdict(Profile)` (`json.dumps(..., indent=2, sort_keys=True)`); compared structurally | parser, canonical |
| `vcards/<case>.parse.out` | the exact bytes of `json.dumps(asdict(profile), ensure_ascii=False)` (see below) | parser, core (`VCard.ToJSON`) |
| `vcards/<case>.normalized.vcf` / `.normalize_error.txt` | `normalize_vcard` | canonical |
| `vcards/<case>.validate.json` | `validate --json` result, pretty-printed; compared structurally | validator |
| `vcards/<case>.validate.out` | the exact bytes of `json.dumps(result.to_dict(), ensure_ascii=False)` (see below) | validator |
| `vcards/<case>.verify.json` | endorsement verification results | endorsements |
| `vcards/<case>.rebuilt.vcf` | `build_vcard_from_raw_lines` | serializer |
| `vcards/build_content.json` | `build_content` args → output | serializer |
| `lifecycle/` | revoke / rotate / add results (fixed `when`) | lifecycle |
| `resolver/` | `normalize_url`, `safe_filename`, `is_global`, `urljoin`, `_validate_url`, UTF-8 decode errors | core |
| `feeds/` | posts, `collect.json`, RSS (signed/unsigned/empty/tampered), verify results, templating inputs, `iso_dates.json` (`fromisoformat` incl. a seeded fuzz corpus), `markdown_cases.json` | feeds |
| `opml/` | OPML output and warnings | feeds |
| `cli/` | Goldens written by the CLI tests: `<case>.stdout`, `<case>.stderr`, `<case>.exitcode` and any frozen output file `<case>.<file>` of one CLI invocation (`vcard validate --json`, `feeds build/verify`, `key` commands) | cli (`TestCLIGolden`) |

### `.json` vs `.out`

`vcards/` holds 96 `.out` files: 49 `<case>.parse.out` and 47 `<case>.validate.out`. Each has a
`.json` sibling with the same data. The `.json` files are pretty-printed (`indent=2`, sorted keys); the
`.out` files hold the exact output bytes of `vcard parse` / `validate --json`. The tests use them differently:

- `.json`: decoded and compared as data (`reflect.DeepEqual` in `internal/vcard/parser_test.go`
  `TestParseVCardGolden`; `internal/core/validator_test.go` `TestValidateProfileGolden`;
  `internal/canonical/canonical_test.go`). Key order and whitespace do not matter.
- `.out`: compared as strings against `internal/strutil.JSONDumps`, to check the serialised text is
  byte-identical (`TestParseJSONMatchesGolden` in `internal/vcard/parser_test.go`,
  `TestValidateJSONMatchesGolden` in `internal/core/validator_test.go`, and `TestToJSONAndHasEndorsement`
  in `internal/core/identity_test.go`). The validator test skips cases whose expected text contains
  `-duplicate` or `Details: ` (known differences listed below).

The `.json` validate files may hold a `{"crash_note": ...}` entry for inputs that the reference
validator could not handle.

Notes:
- RSS fixtures use the `<generator>dsi</generator>` and `<docs>` elements, `xmlns:dc`, and empty-element
  style `<x></x>`.
- HTML items are wrapped in CDATA by `feeds build`.
- `*.pem` files in `golden/keys` are test-only keys from public seeds (un-ignored in `.gitignore`).

Known, deliberate differences from the reference outputs (all covered by tests):
- Markdown → HTML: goldmark follows CommonMark, the reference renderer the original rules; `knownMarkdownDiffs`
  in `internal/feeds/feeds_test.go` lists the 17 of 49 snippets that differ (abbr, attr_list, md_in_html
  are unsupported).
- QR codes: same version/size and error correction, always decodable, but module masks can
  differ (1 of 14 matrices is bit-identical); the logo is resized with Catmull-Rom instead of Lanczos.
- Validator: URLs that crash the reference validator (`https://[::1/x`) are reported as invalid URLs;
  duplicate warnings are listed in order of occurrence.
- The `Details: ...` suffix of DER errors is not reproduced.
- `lastBuildDate`/`pubDate` use UTC wall-clock time.

## CLI goldens and the orphan guard

`golden/cli/` is written by the Go code itself. `internal/cli/golden_test.go`
(`TestCLIGolden`) runs each case in a temp dir with a fixed clock (`2026-03-01T12:00:00Z`), no colour and
separate stdout/stderr buffers, replaces the temp dir by `<TMP>`, and compares with the files. Inputs are
copied from the other golden directories (signing uses `keys/alice.*.pem`). Cases that need random keys
(`key create`, `key rotate`) are deliberately not included. To add or refresh a case, edit
`cliGoldenCases` and run `UPDATE_GOLDEN=1 go test ./internal/cli -run TestCLIGolden`, then review the
`git diff` of `golden/cli/` by eye. The other fixtures stay frozen.

`internal/testutil/golden_guard_test.go` fails when a file under `golden/` is not read by any test (by path,
base name, a `GoldenNames(dir, suffix)` listing, a `"dir/"+name+".suffix"` read, or an entry in its
documented allowlists). Four known orphans are allowlisted until they get a test or are deleted:
`crypto/load_public_key_b64_der.json`, `crypto/pem_errors.json`, `opml/warnings.json`, `qr/image.json`.
