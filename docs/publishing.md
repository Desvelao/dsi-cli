# Feed templates and publishing

## Publishing

`dsi` builds and verifies feeds; it does not upload them. The previous Python tool had `feeds publish` (GitHub Pages and S3 providers); in the Go rewrite that is **not part of the core**, so that the binary stays small and free of cloud SDKs. It is meant to be provided by an external `dsi-publish` [plugin](plugins.md) (not released yet): once installed it would run as `dsi publish ...`.

Until then, host the generated files with any tool you like (for GitHub Pages see [dsi-publish-template-github-pages](https://github.com/Desvelao/dsi-publish-template-github-pages), or use the reusable workflow `.github/workflows/gha-build-feeds.yml` to build the feed in CI).

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
- Metadata available: every `key: value` pair in the file's front matter (for example `title`, `date`, `link`, `image`, `id`, `use_html_content` and your own keys, as text) plus `file_path`, `file_name`, `file_dir` and `file_ext`. `title` and `date` are filled with the file name and its modification time when missing.
- Front matter lines are `key: value`; surrounding matching quotes are removed. `date` must be ISO 8601 (`2025-01-31`, `2025-01-31T12:00:00Z`), otherwise the build fails naming the file.
- `use_html_content: true` (or `yes`) renders the body as Markdown to HTML.
- The item id (`guid`) is the front matter `id`, or the slug of the file path relative to the feed directory without extension.
- `--limit N` keeps the N newest items (items are sorted by date, newest first); `--limit 0` produces an empty feed and a negative value is rejected.
- In the reusable workflow (`gha-build-feeds.yml`) the `feeds_limit` input is optional: when empty or unset, `--limit` is not passed and the feed is unlimited.
- Signing (`--sign-priv` and `--sign-pub`, both required, PEM files; giving only one exits 1) is applied to the items after templating.
