# Command reference

Run `dsipy --help`, `dsipy <group> --help` and `dsipy <group> <command> --help` for the authoritative list. Global option: `--debug` (also `DSIPY_DEBUG=1`) prints tracebacks on errors. Commands exit `0` on success and `1` on failure.

Where `<source>` appears it is a file path, an HTTPS URL, or `-` for stdin. URL-reading commands accept `--allow-http`.

## vcard

| Command | Options |
|---|---|
| `vcard create` | `-o/--output` (default `dsi-card.vcf`), `-i/--interactive`, `--resume`, `--fn`, `--n`, `--nickname`, `--lang`, `--gender`, `--email`, `--categories`, `--note`, `--url`, `--source`, `--bday`, `--anniversary`, `--kind`, `--adr`, `--tel`, `--impp`, `--photo`, `--generate-key`, `--force` |
| `vcard validate <source>` | `--json`, `--strict`, `--allow-http` |
| `vcard inspect <source>` | `--allow-http` |
| `vcard normalize <source>` | `--write`, `--allow-http` |
| `vcard verify <source>` | `--allow-http` |
| `vcard endorse <vcards>...` | `--priv` (required), `-v/--vcard`, `-c/--confidence` (low, medium, high), `--write` |
| `vcard fetch <inputs>...` | `-o/--output-dir`, `-n/--dry-run`, `-b/--backup`, `-d/--diff`, `--allow-http`, `--verify-source/--no-verify-source` |
| `vcard parse [source]` | `--allow-http`; prints JSON; reads stdin when piped or `-` |
| `vcard qr [input]` | `-o/--output`, `-i/--image`, `-t/--caption-top`, `-b/--caption-bottom`, `-f/--font` (.ttf) |

```sh
dsipy vcard create -o dsi.vcf --fn "Alice" --source https://alice.example/dsi.vcf --generate-key
dsipy vcard create -i --resume          # reuse answers saved in vcard_create.tmp by a cancelled run
dsipy vcard validate dsi.vcf --json --strict
cat dsi.vcf | dsipy vcard parse
dsipy vcard fetch dsi.vcf --dry-run --diff
dsipy vcard fetch cards/ --backup       # writes <file>.bak before overwriting
dsipy vcard qr dsi.vcf -o qr.png -t "Alice" -b "alice.example" -f DejaVuSans.ttf
```

- `--generate-key` writes `vcard_private.pem` and `vcard_public.pem`; without `--force` it refuses to overwrite existing key files.
- `vcard fetch`: only HTTPS, and the fetched card's `SOURCE` must match the requested URL unless `--no-verify-source`. Exits `1` if any item failed.
- `vcard endorse` prints the `X-ENDORSE` lines; with `--vcard <dest> --write` it adds them to the destination.

## key

| Command | Options |
|---|---|
| `key create` | `--priv` (default `private.pem`), `--pub` (default `public.pem`), `--force` |
| `key add <vcard>` | `--priv`, `--pub`, `--public-key <base64-der>`, `--pref/--no-pref`, `-o/--output` |
| `key rotate <vcard>` | `--priv`, `--pub`, `--old-key`, `--reason` (rotated or superseded), `-o/--output` |
| `key revoke <vcard>` | `--key` or `--pub`, `--reason` (required: compromised, rotated, superseded, retired, lost, deprecated), `-o/--output` |
| `key pub-encode <pem>` | none; prints Base64 DER |
| `key pub-decode [base64]` | none; reads stdin if omitted; prints PEM |

`key create` refuses to overwrite existing files unless `--force` is given. `add`, `rotate` and `revoke` update the vCard in place unless `-o` is given.

## feeds

| Command | Options |
|---|---|
| `feeds init [directory]` | default `feeds`; `--sample/--no-sample`, `--type` |
| `feeds add` | `-t/--title`, `-m/--message`, `-f/--filename`, `-i/--interactive`, `--type` |
| `feeds build <directory>` | `-o/--output` (default `feed.rss`), `-l/--limit`, `-t/--title`, `-k/--link`, `-d/--description`, `-g/--language` (default `en-US`), `-a/--author`, `-e/--email`, `--type`, `-i/--interactive`, `--sign-priv`, `--sign-pub`, `--var`, `--var-file` |
| `feeds publish <inputs>...` | `--provider` (required: github, s3), `--arg key=value`, `--prefix`, `--diff`, `--dry-run` |
| `feeds verify <rss>` | `-v/--vcard`, `--pub` (one of them is required) |

`feeds build` requires title, link, description, author and email, either as options or via `--interactive`. See [templating and publishing](publishing.md).

```sh
dsipy feeds publish feeds.rss --provider s3 --arg bucket=my-bucket --dry-run --diff
```

`--dry-run` writes nothing, but it still reads the current remote copy of each file to decide between "would publish" and "unchanged", so it needs working provider credentials.

## connections

`dsipy connections feed <inputs>... [-o/--output <file>]` generates an OPML file from vCard files or directories (`.vcf`, `.vcard`). It emits one `<outline type="rss">` per `X-FEED`, named after the card's `FN`, with `xmlUrl`, plus `language` and `category` (feed category and tags, comma separated) when present. Unreadable or malformed cards are skipped with a warning; if no feed is found the command exits `1`. Without `-o` the OPML is printed to stdout.

```sh
dsipy connections feed friends/ bob.vcf -o following.opml
```
