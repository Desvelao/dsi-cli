# dsi documentation

- [Getting started](getting-started.md): create a vCard and keys, validate, endorse, build a feed, host it.
- [Command reference](commands.md): every command and option, with examples.
- [Feed templates and publishing](publishing.md): `{{ }}` templates for `feeds build`, and where publishing went.
- [Plugins](plugins.md): extend `dsi` with `dsi-<name>` executables.
- [Hands-on testing guide](testing-guide.md): runnable commands with expected results to try and verify every feature yourself, in a throw-away folder.
- [Migrating from the Python `dsipy`](migrating-from-python.md): what changed in the Go rewrite.

## Install

Download the binary of your platform from the [Releases](https://github.com/Desvelao/dsi-cli/releases) page, verify it with `checksums.txt` and put it on your `PATH` (details in the [README](../README.md#install)). Then:

```sh
dsi --version
dsi --help
```

## Development

```sh
make test    # in the Docker dev container
make lint
make build   # bin/dsi
```
