# Feed templates and publishing

## Publishing

`dsi` builds and verifies feeds; it does not upload them. Publishing (GitHub Pages, S3 providers) is **not part of the core**, so that the binary stays small and free of cloud SDKs. It is meant to be provided by an external `dsi-publish` [plugin](plugins.md) (not released yet): once installed it would run as `dsi publish ...`.

Until then, host the generated files with any tool you like (for GitHub Pages see [dsi-publish-template-github-pages](https://github.com/Desvelao/dsi-publish-template-github-pages), or use the [reusable workflow](#reusable-workflow) `.github/workflows/gha-build-feeds.yml` to build the feed in CI).

## Reusable workflow

`.github/workflows/gha-build-feeds.yml` is a reusable workflow (`workflow_call`) that builds (and optionally signs) a feed in CI. It checks out the caller repository, downloads the `dsi_linux_amd64` binary from a release of `Desvelao/dsi-cli`, verifies it against the release `checksums.txt`, runs `dsi feeds build` and uploads the generated file as an artifact (retention 1 day). It does not publish anything: deploy the artifact with a later job (for example GitHub Pages).

### Example caller

```yaml
name: Build feeds
on:
  push:
    branches: [main]

jobs:
  feeds:
    uses: Desvelao/dsi-cli/.github/workflows/gha-build-feeds.yml@<tag>
    with:
      dsi_tag: v1.0.0            # pin a release (required for pre-releases)
      source_dir: feeds
      feeds_file: feeds.rss
      feeds_limit: "50"
      feeds_vars: |
        site=https://alice.example
    secrets:
      # feeds_author / feeds_email may be passed here or as `with:` inputs
      feeds_author: ${{ secrets.FEEDS_AUTHOR }}
      feeds_email: ${{ secrets.FEEDS_EMAIL }}
      sign_private_key: ${{ secrets.SIGN_PRIVATE_KEY }}
      sign_public_key: ${{ secrets.SIGN_PUBLIC_KEY }}

  deploy:
    needs: feeds
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4
        with:
          name: ${{ needs.feeds.outputs.artifact_name }}
      # ... publish ${{ needs.feeds.outputs.feeds_file }} where you host it
```

Replace `<tag>` with a tag or branch of this repository. The reusable workflow itself only needs `contents: read`.

### Inputs (`with:`)

All inputs are optional strings.

| Input | Default | Description |
|-------|---------|-------------|
| `dsi_tag` | empty | Release tag of `Desvelao/dsi-cli` to download the binary from. Empty means the latest **stable** release; this only works once a stable release with the `dsi_linux_amd64` asset and `checksums.txt` exists. Pre-releases are never picked automatically: pin them with an explicit tag. |
| `source_dir` | `feeds` | Directory passed to `dsi feeds build`. |
| `feeds_limit` | unset | Keep only the N newest items (`--limit`). Empty means unlimited. |
| `feeds_file` | `feeds.rss` | Output file name (`--output`), also the file uploaded in the artifact. |
| `feeds_title` | `DSI Feed` | Feed title. |
| `feeds_description` | `DSI feed updates` | Feed description. |
| `feeds_author` | unset | Feed author (see "Secrets and inputs with the same name"). |
| `feeds_email` | unset | Feed author email (see below). |
| `feed_link` | empty | Feed URL. When empty it is `https://<owner>.github.io/<repo>/<feeds_file>` (owner and repo lowercased). |
| `feeds_vars` | unset | Extra template variables, `key=value`, one per line (see [Feed templates](#feed-templates)). |
| `command_args` | unset | Extra arguments appended to `dsi feeds build`, split on whitespace (no quoting). |
| `artifact_name` | `feeds-file` | Name of the uploaded artifact. |

### Secrets (`secrets:`)

| Secret | Required | Description |
|--------|----------|-------------|
| `feeds_author` | no | Feed author. May be passed as the `feeds_author` input instead. |
| `feeds_email` | no | Feed author email. May be passed as the `feeds_email` input instead. |
| `dsi_repo_token` | no | Token used by `gh release` to read the `dsi-cli` releases; falls back to the workflow `github.token`. |
| `sign_private_key` | no | Private key (PEM) used to sign the feed items. |
| `sign_public_key` | no | Public key (PEM). Must be set together with `sign_private_key`, otherwise the job fails. |
| `feeds_title`, `feeds_description`, `feed_link`, `feeds_vars` | no | Secret variants of the inputs of the same name. |

### Secrets and inputs with the same name

`feeds_title`, `feeds_description`, `feeds_author`, `feeds_email`, `feed_link` and `feeds_vars` exist both as a secret and as an input.

- Scalars (`feeds_title`, `feeds_description`, `feeds_author`, `feeds_email`, `feed_link`): the value is `secrets.X || inputs.X`, so a **non-empty secret wins**; the input (or its default) is used only when the secret is empty or not passed.
- `feeds_vars`: both are used. Secret lines are written first and input lines after them, and when a key appears twice the later line wins, so the input overrides the secret. These lines are also written after the built-in variables (`gh_owner`, `gh_repo`, `gh_pages_base_url`, `gh_repo_source_base_url`, `author`, `email`), so they can override them.
- `feeds_author` and `feeds_email` are optional secrets: a caller may pass them under `secrets:`, under `with:` as inputs, or both (a non-empty secret wins). Author and email are not sensitive, so inputs are the natural choice.
- Title, description, author, email and link must be single-line values; the job fails otherwise.

The workflow runs only on GitHub Actions and cannot be run locally.


# Feed templates

`dsi feeds build` replaces `{{ name }}` placeholders in these item fields: title, id, link, image and content. Other fields (such as `date`) are not templated.

```markdown
---
title: Release {{ version }}
date: 2025-03-01
link: {{ site }}/posts/{{ file_name }}
---
Read more at {{ site }}.
```

```sh
dsi feeds build feeds -o feeds.rss --title "Blog" --link https://alice.example \
  --description "Notes" --author Alice --email alice@example.com \
  --var site=https://alice.example --var version=1.2 --var-file vars.env
```

- Syntax: exactly `{{ name }}` with one space inside each brace pair (`{{name}}` is not replaced). Unknown names are left as they are.
- `--var key=value` (repeatable, split at the first `=`) and `--var-file` (one `KEY=VALUE` per line; blank lines and lines starting with `#` are ignored; a missing file exits 1) define variables. Command-line `--var` wins over `--var-file`, and both win over metadata of the same name.
- Metadata available: every `key: value` pair in the file's front matter (for example `title`, `date`, `link`, `image`, `id`, `use_html_content` and your own keys, as text) plus `file_path`, `file_name`, `file_dir` and `file_ext`. `title` is filled with the file name when missing. `date` is required: a post without it makes the build fail with an error naming the file (file modification times are not used, because they change between checkouts).
- Front matter lines are `key: value`; surrounding matching quotes are removed. `date` must be ISO 8601 (`2025-01-31`, `2025-01-31T12:00:00Z`), otherwise the build fails naming the file.
- `use_html_content: true` (or `yes`) renders the body as Markdown to HTML.
- The item id (`guid`) is the front matter `id`, or the slug of the file path relative to the feed directory without extension. Ids must be unique: two posts with the same id (for example `a b.md` and `a-b.md`) make the build fail naming both files.
- `--limit N` keeps the N newest items (items are sorted by date, newest first); `--limit 0` produces an empty feed and a negative value is rejected.
- In the reusable workflow (`gha-build-feeds.yml`) the `feeds_limit` input is optional: when empty or unset, `--limit` is not passed and the feed is unlimited.
- Signing (`--sign-priv` and `--sign-pub`, both required, PEM files; giving only one exits 1) is applied to the items after templating.
