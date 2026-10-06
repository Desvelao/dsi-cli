# Getting started with DSI

A DSI identity is one vCard (`dsi.vcf`) that you host at a public HTTPS URL, plus optional signed feeds. The spec is [dsi-spec](https://github.com/Desvelao/dsi-spec); `dsi` is the tool.

## 0. Install

Download the `dsi` binary of your platform from the [Releases](https://github.com/Desvelao/dsi-cli/releases) page, check it against `checksums.txt`, make it executable and put it on your `PATH` (see the [README](../README.md#install)):

```sh
dsi --version
dsi --help
```

## 1. Create your vCard and keys

```sh
dsi vcard create -o dsi.vcf --fn "Alice Example" \
  --source https://alice.example/dsi.vcf --generate-key
```

- `--source` is the URL where you will host the file and your canonical address. The command still creates a card without it (and warns), but `dsi vcard validate` rejects a card with no `SOURCE`, so always set it.
- `--generate-key` writes `vcard_private.pem` / `vcard_public.pem` and adds the public key to the vCard. **Keep the private key secret and out of git.**
- `-i` asks for each field interactively. Keys alone: `dsi key create`.

Add links and accounts by editing `dsi.vcf` (one property per line):

```
X-SOCIAL;PLATFORM=github:alice
X-SOCIAL;PLATFORM=activitypub:https://social.example/@alice
X-FEED;LANGUAGE=en-US:https://alice.example/feeds.rss
```

## 2. Validate

```sh
dsi vcard validate dsi.vcf          # exits 1 if there are errors
dsi vcard validate dsi.vcf --json   # {"valid", "errors", "warnings"}, handy in CI
dsi vcard validate dsi.vcf --strict # warnings also fail
dsi vcard inspect dsi.vcf           # read-only summary
```

## 3. Endorse other people

Endorsing signs someone's public key with yours. Get their vCard (`dsi vcard fetch https://bob.example/dsi.vcf`), then:

```sh
dsi vcard endorse bob.vcf --priv vcard_private.pem   # print the X-ENDORSE line
dsi vcard endorse bob.vcf --priv vcard_private.pem --vcard dsi.vcf --write  # add it to your vCard
dsi vcard verify dsi.vcf                              # check your endorsements
```

Endorsements are public: they show who you vouch for.

## 4. Publish a feed

Create the source directory once, then add one markdown file per post (`--filename` is the full path):

```sh
dsi feeds init feeds                     # creates feeds/ with a sample post (hello.md)
dsi feeds add --title "Second" --message "More news" --filename feeds/second.md
dsi feeds build feeds -o feeds.rss \
  --title "Alice's blog" --link https://alice.example --description "Notes" \
  --author Alice --email alice@example.com \
  --sign-priv vcard_private.pem --sign-pub vcard_public.pem
dsi feeds verify feeds.rss --vcard dsi.vcf
```

Each item's id (the RSS `guid`) is derived from the file's path relative to the feed directory, without extension (`feeds/2025/news.md` becomes `2025-news`), so it does not change if you move the directory or run from another folder. Renaming or moving a file inside the feed directory changes its id; to pin it, set `id: my-stable-id` in the file's front matter.

Signing is optional; signed items can be checked by anyone with your vCard. Make sure the `X-FEED` URL in your vCard points to the published `feeds.rss`.

## 5. Host it

Put `dsi.vcf` at the exact URL you used in `--source`, and `feeds.rss` where `X-FEED` points, both over HTTPS. Then check what others will see:

```sh
dsi vcard validate https://alice.example/dsi.vcf   # also checks that SOURCE matches the URL
```

For `{{ }}` templates in posts, see [feed templates](publishing.md). Uploading feeds from the command line (GitHub or S3) is meant to be done by a `dsi-publish` [plugin](plugins.md). With GitHub Pages, use [dsi-publish-template-github-pages](https://github.com/Desvelao/dsi-publish-template-github-pages): its workflow builds and publishes `feeds.rss`; you commit `dsi.vcf` and images to the `gh-pages` branch yourself. A custom domain is set with the `PAGES_CNAME` variable.

## 6. Keep it healthy

```sh
dsi key rotate dsi.vcf --priv new_private.pem --pub new_public.pem   # new preferred key, old one revoked
dsi key revoke dsi.vcf --key <BASE64_DER> --reason compromised       # or lost, retired, ...
dsi key add dsi.vcf --priv new_private.pem --pub new_public.pem      # new key without revoking; use it when every key is revoked
dsi vcard fetch dsi.vcf                                              # refresh from SOURCE
```

After a rotation, re-sign your feeds with the new key and tell people who endorsed you to endorse the new one. If a key is compromised, revoke it right away.

## Cheatsheet

| Goal | Command |
|---|---|
| Create vCard + key | `vcard create --generate-key --source <url>` |
| Check a vCard | `vcard validate <file\|url>` |
| Summary | `vcard inspect <file>` |
| Endorse | `vcard endorse <vcard> --priv <pem> [--vcard <dest> --write]` |
| Check endorsements | `vcard verify <file>` |
| Init feeds dir / add post / build | `feeds init` / `feeds add` / `feeds build <dir> -o feeds.rss` |
| Check feed signatures | `feeds verify <rss> --vcard <file>` |
| Rotate / revoke / add key | `key rotate <vcard>` / `key revoke <vcard> --key ... --reason ...` / `key add <vcard>` |

Details: [command reference](commands.md), or `dsi <group> <command> --help`.
