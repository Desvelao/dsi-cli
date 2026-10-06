# dsipy documentation

- [Getting started](getting-started.md): create a vCard and keys, validate, endorse, publish a feed, host it.
- [Command reference](commands.md): every command and option, with examples.
- [Publishing feeds and templates](publishing.md): `feeds publish` providers (GitHub, S3), `--arg`, `--diff`/`--dry-run`, conflicts, exit codes, and `{{ }}` templates for `feeds build`.
- [Hands-on testing guide](testing-guide.md): runnable commands with expected results to try and verify every feature yourself, in a throw-away folder.

## Install and run

Requires Python 3.12 or newer. From the repository root:

```sh
pip install .          # installs the `dsipy` command
pip install -e .       # editable install for development
dsipy --help
```

Without installing, `python -m dsipy` works from the `src` directory (`cd src && python -m dsipy --help`) once the dependencies are installed.

## Development

```sh
pip install -e ".[dev]"
python -m pytest -q
black src tests
```
