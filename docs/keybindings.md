# Keybindings

Prerequisite: build and link this checkout, or install a published release with
`herdr plugin install RooseveltAdvisors/herdr-plugin-sesh --ref <release-tag>` using a
tag from the [GitHub releases](https://github.com/RooseveltAdvisors/herdr-plugin-sesh/releases)
page.

Example Herdr keybinding once the plugin is linked:

```toml
[keys]
# If using "prefix+shift+t" to open the herdr-sesh plugin picker, the rename_tab keybind needs to be changed.
rename_tab = "prefix+shift+,"

# Unbind stock cycling actions when replacing them with two-target toggles.
previous_tab = ""
previous_workspace = ""

[[keys.command]]
key = "prefix+shift+t"
type = "plugin_action"
command = "RooseveltAdvisors.herdr-sesh.open-picker"
description = "open Sesh picker"

[[keys.command]]
key = "prefix+l"
type = "plugin_action"
command = "RooseveltAdvisors.herdr-sesh.last-agent"
description = "toggle previous agent/tab"

[[keys.command]]
key = "prefix+shift+l"
type = "plugin_action"
command = "RooseveltAdvisors.herdr-sesh.last"
description = "toggle previous workspace"
```

Bindings must use the `RooseveltAdvisors.herdr-sesh.*` action ids (not
`fullerzz.sesh.*`, and not stock `previous_tab` / `previous_workspace`).
Stock `previous_tab` is an N-item cycle; `fullerzz.sesh.last` fails with
`custom command failed` when only this fork is installed.

`last` and `last-agent` are strict two-target toggles (tmux-style). They alternate
only between the current target and the immediately previously selected target.
Agent/tab and workspace recency are tracked independently via plugin focus event
hooks, so same-workspace tab changes do not overwrite the workspace destination.

Manual picker open:

```bash
herdr plugin pane open --plugin RooseveltAdvisors.herdr-sesh --entrypoint picker --placement overlay
```
