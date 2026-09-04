# Sesh for Herdr

A [Sesh](https://github.com/joshmedeski/sesh)-inspired workspace picker and
session manager for [Herdr](https://herdr.dev/).

`herdr-plugin-sesh` combines running Herdr workspaces, configured sessions, and
zoxide history in one searchable overlay. Selecting an item focuses its
existing workspace or creates a new one with the configured startup command and
tabs.

![Herdr Sesh picker demo](assets/picker-demo-42.gif)

See [VISION.md](VISION.md) for why this fork exists and the commitments it keeps.

## Features

- Search active Herdr workspaces, Sesh-style TOML sessions, and zoxide history
  from a native terminal picker.
- Focus an existing workspace or create one from a configured session or
  directory.
- Close active Herdr workspaces directly from the native picker.
- Apply startup commands, previews, and named Herdr tabs to new workspaces.
- Filter, sort, deduplicate, and optionally cache session results.
- Toggle between the current and previously selected workspace (`last`) or
  agent/tab (`last-agent`) as strict two-target switches.
- Clone a Git repository and connect to it in one command.
- Use the built-in picker by default or opt into the experimental fzf picker.

Sesh concepts map onto Herdr as follows:

| Sesh | Herdr |
| --- | --- |
| Session | Workspace |
| Window | Tab |
| Picker | Overlay pane |

## Requirements

- [Herdr](https://herdr.dev/docs/installation/) 0.8.0 or newer (plugin focus event hooks and workspace picker actions)
- Linux or macOS
- Git and Go 1.26.4 or newer for Herdr's source-based plugin installation
- Optional: `zoxide` for directory history and `eza` for the default preview
- Optional: `fzf` and `bat` for the experimental fzf picker

## Installation

Install the plugin directly from GitHub:

```bash
herdr plugin install RooseveltAdvisors/herdr-plugin-sesh
```

Herdr previews the plugin manifest and build command before installation. To
skip the confirmation in a non-interactive environment, add `--yes`. To pin a
release, add `--ref <release-tag>` using a tag from the
[GitHub releases](https://github.com/RooseveltAdvisors/herdr-plugin-sesh/releases) page.

This repository is also discoverable through the
[Herdr plugin marketplace](https://herdr.dev/plugins/) via the `herdr-plugin`
GitHub topic. Marketplace listings are automatic and are not endorsements or
security reviews.

## Quick start

Open the picker through the installed plugin action:

```bash
herdr plugin action invoke RooseveltAdvisors.herdr-sesh.open-picker
```

You can also open its overlay pane directly:

```bash
herdr plugin pane open \
  --plugin RooseveltAdvisors.herdr-sesh \
  --entrypoint picker \
  --placement overlay
```

See [Keybindings](docs/keybindings.md) to bind the picker, previous-workspace
(`last`), and previous-agent/tab (`last-agent`) two-target toggles.

## Configuration

Configuration is optional. Without a config file, the picker still includes
running Herdr workspaces and zoxide results when zoxide is available.

The plugin reads a supported subset of Sesh-style TOML from the first available
location:

1. `--config PATH`
2. `HERDR_SESH_CONFIG`
3. `${HERDR_PLUGIN_CONFIG_DIR}/sesh.toml`
4. `~/.config/sesh/sesh.toml` as a migration fallback

Ask Herdr for the managed configuration directory:

```bash
herdr plugin config-dir RooseveltAdvisors.herdr-sesh
```

See the [configuration reference](docs/config.md) for supported settings and a
complete example.

## CLI

The plugin binary also exposes its underlying operations directly:

| Command | Purpose |
| --- | --- |
| `herdr-sesh picker` | Open the native workspace picker. |
| `herdr-sesh picker --fzf` | Open the experimental fzf picker. |
| `herdr-sesh list --json` | List merged session sources as JSON. |
| `herdr-sesh connect TARGET` | Focus or create a workspace for a name, path, or ID. |
| `herdr-sesh preview TARGET` | Render the configured preview for a session. |
| `herdr-sesh clone REPOSITORY` | Clone a repository and connect to its workspace. |
| `herdr-sesh root --connect` | Connect to the current Git repository root. |
| `herdr-sesh last` | Toggle the previous workspace (strict two-target). |
| `herdr-sesh last-agent` | Toggle the previous agent/tab (strict two-target). |
| `herdr-sesh window [PATH]` | List tabs or create one for a path. |
| `herdr-sesh config path` | Print the resolved plugin config path. |
| `herdr-sesh config init` | Create a starter config if one does not exist. |

The binary lives inside Herdr's managed plugin checkout after installation; the
plugin actions are the normal entry points for day-to-day use.

## Local development

Tool versions are pinned in [`mise.toml`](mise.toml), and common tasks live in
the [`justfile`](justfile).

```bash
mise install
just check
just install-plugin
```

`just install-plugin` builds the binary and links the current checkout into
Herdr. Verify the local plugin with:

```bash
herdr plugin action list --plugin RooseveltAdvisors.herdr-sesh
herdr plugin log list --plugin RooseveltAdvisors.herdr-sesh
```

## Release

Release tags must begin with `v` and match `version` in
[`herdr-plugin.toml`](herdr-plugin.toml). Create and publish a release with:

```bash
just release vX.Y.Z
```

The recipe requires a clean working tree, validates the tag against the
manifest version, runs the repository checks and CLI smoke tests, creates an
annotated tag, and atomically pushes the current commit and tag.

## License

Licensed under the [MIT License](LICENSE).
