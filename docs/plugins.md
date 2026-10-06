# Plugins

`dsi` can be extended with external plugins, like `git` or `gh`. A plugin is **any
executable named `dsi-<name>`**. Running `dsi <name> [args...]` runs it, so plugins can be
written in any language and a crashing plugin cannot take `dsi` down.

## Where plugins are found

1. The plugin directory: `$DSI_PLUGIN_DIR`, or `$XDG_DATA_HOME/dsi/plugins`
   (default `~/.local/share/dsi/plugins`).
2. The directories of `$PATH` (empty entries are ignored).

The first match wins. Plugins must be executable files; on Windows they are `dsi-<name>.exe`.
Names may contain letters, digits, `.`, `_` and `-`, and cannot start with `-` or `.`
(so `dsi ../x` never runs anything).

**Core commands cannot be shadowed**: `dsi key`, `dsi vcard`, `dsi feeds`,
`dsi connections`, `dsi plugin` and `dsi help` always run the built-in commands.

## Running a plugin

- Everything after the plugin name is passed through unchanged (flags included).
- stdin, stdout and stderr are connected to the plugin.
- The exit code of the plugin is the exit code of `dsi`.
- `dsi --debug <name> ...` runs the plugin with `DSI_DEBUG=1`.

Environment given to the plugin (in addition to the inherited one):

| Variable | Value |
|---|---|
| `DSI_VERSION` | version of the `dsi` that launched the plugin |
| `DSI_BIN` | path of that `dsi` executable, to call core commands back (`"$DSI_BIN" vcard validate ...`) |
| `DSI_DEBUG` | `1` when debug output was requested |

## Managing plugins

There is no installer yet: drop the executable into the plugin directory.

```sh
mkdir -p ~/.local/share/dsi/plugins
install -m 0755 dsi-hello ~/.local/share/dsi/plugins/
dsi plugin list      # NAME / PATH of every installed plugin
dsi hello world      # runs dsi-hello world
```

`dsi --help` also lists the installed plugins.

## Example

```sh
#!/bin/sh
# ~/.local/share/dsi/plugins/dsi-count
# Usage: dsi count FILE...   (counts the X-FEED entries of vCards)
for f in "$@"; do
  printf '%s: ' "$f"
  "$DSI_BIN" vcard parse "$f" | grep -o '"feeds": \[[^]]*\]' | grep -o '"url"' | wc -l
done
```

## Publishing

`feeds publish` (GitHub Pages and S3) is **not part of the core**: it is meant to become the
`dsi-publish` plugin, which keeps the AWS SDK out of the core binary.
