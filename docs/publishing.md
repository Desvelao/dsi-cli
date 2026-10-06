# Publishing feeds

`dsipy feeds publish` uploads feed files (`.xml`, `.rss`, `.json`) to a provider. Only the feeds are published this way; host your `dsi.vcf` yourself (see [getting started](getting-started.md)).

```sh
dsipy feeds publish <file-or-dir>... --provider <github|s3> [--arg key=value]... \
  [--prefix <path>] [--diff] [--dry-run]
```

- `inputs`: files or directories. A directory is searched recursively and each file is published under its path relative to that directory (`out/2025/a.rss` with input `out` becomes `2025/a.rss`). A file given directly is published under its file name.
- `--provider`: `github` or `s3` (required). Any other value fails with `Unknown provider type`. There are no `webdav` or `local` providers.
- `--prefix`: path prepended to every remote path (`/` is appended if missing).
- `--arg key=value`: provider-specific setting, repeatable. Split at the first `=`, so values may contain `=`. An argument without `=` is rejected. Unknown keys are ignored.

## Providers

### github

Writes through the GitHub contents API (one commit per changed file) on a branch.

| `--arg` | Required | Meaning |
|---|---|---|
| `owner` | yes | User or organization |
| `repo` | yes | Repository name |
| `branch` | yes | Branch to commit to (for GitHub Pages, usually `gh-pages`) |
| `token` | yes | Personal access token with write access to the repository contents |

```sh
dsipy feeds publish out/ --provider github \
  --arg owner=alice --arg repo=alice.github.io --arg branch=gh-pages \
  --arg token="$GITHUB_TOKEN" --prefix feeds
```

The token is only read from `--arg token=...`; `dsipy` does not read any environment variable for it, so pass it from your shell or CI secret as above. Beware that command lines may be visible in process lists and shell history. `--prefix` is the directory inside the repository.

### s3

| `--arg` | Required | Meaning |
|---|---|---|
| `bucket` | yes | Bucket name |
| `prefix` | no | Key prefix (slashes at both ends are trimmed) |
| `region` | no | AWS region for the client |

Credentials are not passed to `dsipy`: boto3's default credential chain is used (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN`, `AWS_PROFILE`, `~/.aws`, instance or role credentials). The key is `<--arg prefix>/<--prefix>/<relative path>`; in practice use one of them. Content type is `application/xml` for `.xml`, `.rss` and `.opml`.

```sh
AWS_PROFILE=dsi dsipy feeds publish feeds.rss --provider s3 \
  --arg bucket=my-bucket --arg region=eu-west-1 --prefix public
```

## What happens for each file

1. The local file is read as UTF-8 and the remote copy is fetched (this needs working credentials even with `--dry-run`).
2. `--diff`: when a remote copy exists, a unified diff (remote vs local) is printed, or `No differences.`
3. If the contents are identical the file is reported `Unchanged` and skipped.
4. `--dry-run`: the file is reported as one that would be published and nothing is written.
5. Otherwise it is uploaded.

The summary shows `Published` (or `Would publish` with `--dry-run`), `Unchanged` and `Failed`.

## Conflicts

Uploads are conditional on the version that was read in step 1 (GitHub `sha`, S3 `ETag`):

- File changed remotely in the meantime: `Conflict (remote changed since it was read)`; the file counts as failed. Run the command again.
- S3 file that did not exist when read: it is created only if it still does not exist, so a file created by someone else meanwhile is never overwritten (this needs a bucket and region that support S3 conditional writes).

## Exit codes

`0` when every file was published, unchanged or (dry-run) would be published. `1` when no feed file is found, or when any file failed (unreadable, remote fetch error, conflict, upload error); the other files are still processed. A bad `--arg` or a missing required provider argument is reported before anything is uploaded (the latter as an error message from the provider selection).

Use `dsipy --debug ...` (or `DSIPY_DEBUG=1`) to get full tracebacks.

# Feed templates

`dsipy feeds build` replaces `{{ name }}` placeholders in these item fields: title, id, link, image and content. Other fields (such as `date`) are not templated.

```markdown
---
title: Release {{ version }}
date: 2025-03-01
link: {{ site }}/posts/{{ file_name }}
---
Read more at {{ site }}.
```

```sh
dsipy feeds build feeds -o feeds.rss --title "Blog" --link https://alice.example \
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
- Signing (`--sign-priv` and `--sign-pub`, both required, PEM files; giving only one exits 1) is applied to the items after templating.
