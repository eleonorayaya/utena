# Utena

A workspace management system for [tuios](https://tuios.dev), consisting of a daemon API server and a TUI client.

## Documentation

### Architecture & Patterns

- [Architecture Overview](docs/architecture.md) - System components, module dependencies, and communication patterns
- [Module Structure](docs/module-structure.md) - Standard module layout, layer responsibilities, and lifecycle hooks
- [Event Bus Pattern](docs/event-bus.md) - When and how to use events for module communication

### Development

- [Adding Features](docs/adding-features.md) - Step-by-step guides for adding endpoints, events, and modules

### Project Setup

#### Install

```bash
task tui:install     # builds and copies to /usr/local/bin/utena
task daemon:install  # builds and installs daemon
```

#### tuios

The daemon talks to the tuios daemon over its control socket (`$TUIOS_SOCKET`, else `$XDG_RUNTIME_DIR/tuios/tuios.sock`, else `/tmp/tuios-<uid>/tuios.sock`) and follows its event stream to keep session and window state in sync.

Start your terminal with `utena attach` instead of `tuios attach`. The utena daemon never starts tuios itself (a launchd-started tuios would give every pane and popup launchd's minimal environment), so `utena attach` is also what brings the tuios daemon up. It runs `tuios attach` and reattaches to whatever session you pick in the utena TUI (tuios has no programmatic `switch-client`, so the wrapper restarts the client on the chosen session).

The tuios side (the `prefix+p` popup, the `custom/utena` dock cell running `utena status-line`, and an `after-attach` hook that keeps the attached session accurate however the tuios client switches sessions) is deployed from dotfiles: `programs/tuios/contents/config.toml`.

#### Resource usage

`utena top` shows live CPU, memory and process counts per session (`c`/`m`/`n` sort, `p` shows each session's busiest processes, `q` quits). `utena top --once` prints one tab-separated snapshot. Sessions tuios runs that utena doesn't manage are listed as `unmanaged`.

#### Shell initialization

Add to your `.zshrc` (or `.bashrc`):

```bash
eval "$(utena shell-init)"
```

This sets up environment variables needed for utena integrations (like Claude Code status tracking) when running inside tuios. It resolves `UTENA_SESSION_ID` from `$TUIOS_SESSION`, so panes you open by hand are attributed to the right session.

#### Browser windows

Links opened from a utena session land in that session's own browser window. The daemon drives one browser: Chrome or Firefox through the utena extension, or Chrome through AppleScript where extensions can't be installed:

| Flag | Env | Default | |
|---|---|---|---|
| `--browser` | `UTENA_BROWSER` | `chrome` | `chrome`, `firefox` or `chrome-applescript` |

Without the flag, the daemon reads the default from `~/.config/utena/daemon.json` (`{"browser": "firefox"}`); the env var overrides both. The dotfiles personal profile writes that file. Restart the daemon after changing it. Each driver keeps its own session windows: after switching, the other driver's windows are left alone (not moved, restored or reattached) and come back when you switch back. Deleting a session forgets its windows for every driver.

1. Install the extension in one browser profile only.
   - **Chrome:** run `task extension:install`, then do the one-time Load unpacked step it prints (`chrome://extensions` → Developer mode → Load unpacked).
   - **Firefox:** release Firefox only keeps signed add-ons, so the extension is signed as an unlisted add-on on addons.mozilla.org. Create API credentials at https://addons.mozilla.org/developers/addon/api/key/, export them as `WEB_EXT_API_KEY` and `WEB_EXT_API_SECRET`, and run `task extension:firefox:sign`. It signs the extension (Mozilla's automated review takes a few minutes) and opens the `.xpi` in Firefox to install. Bump the version in `extension/firefox/manifest.json` before signing a changed extension; AMO won't sign a version twice. `task extension:firefox:lint` checks it without signing.
   - **Chrome without the extension (`chrome-applescript`):** nothing to install. The daemon reads Chrome's windows and tabs through `osascript` about every 1.5s. The first time, macOS asks whether utena may control Google Chrome; if you missed it or denied it, allow it in System Settings → Privacy & Security → Automation (the TUI footer says so while it's denied). The permission survived a rebuilt daemon binary in testing. Chrome brings itself to the front whenever a page loads, so links from sessions that aren't attached wait as pending tabs (see below), and a window created when you attach a session briefly takes focus before utena gives it back.
2. `eval "$(utena shell-init)"` sets `BROWSER` to `utena-open` inside tuios, so tools that open links route them through the daemon.
3. `utena open <url>...` opens links in the current session's window; when the browser extension is connected, the TUI's `o` switches to the selected session (if it isn't attached) and opens its PR in that session's window. Otherwise `o` just opens the PR.
4. If the daemon or extension is unavailable, links fall back to `/usr/bin/open`.

After upgrading utena, reload the Chrome extension in `chrome://extensions`, or sign and install the new Firefox build. The extension speaks protocol 2, and the daemon reports an older one as outdated.

Session windows outlive the session's time in the foreground:

- **Archiving** a session saves its windows' tabs and closes the windows. **Reactivating** it brings them back with the same tabs and the same active tab (pinned tabs come back unpinned).
- **Closing a window by hand** forgets it; it won't come back.
- **Quitting the browser** keeps the windows. When it restarts and restores its windows, utena reattaches them to their sessions by matching their tabs. Windows it can't match are restored from their saved tabs the next time the session is attached or a link is opened for it.
- **Deleting** a session closes its windows.
- **Attaching** a session that has no browser window gives it one: its saved windows come back, or a new empty window opens, so there's always a window to add tabs to. A window you close by hand comes back the next time you attach the session.

The TUI shows each session's browser windows:

- **Session list:** a `◫` next to the attached marker means the session has browser windows. It is blue when a window is open, and muted when the windows are only saved (the browser quit, or the session was archived).
- **Session detail:** a **Browser** section, after Workspaces, shows the tab count and the best window state, then one line per tab (`●` marks a window's active tab), grouped under `window N` when there is more than one window. Saved tabs are muted. Pending tabs (`◌`) are links waiting to load: with a browser driver that can't load a page without taking focus, links for sessions that aren't attached wait in the session's window and load when the window gets focus. `utena open` says when a link is waiting.
- **Opening a tab:** `↓`/`j` moves from blockers into the open windows' tabs. `enter` on a tab activates it and focuses its window, switching to the session first if it isn't attached (the TUI then closes, as with `o`). With nothing selected, `enter` still switches to the session. Saved tabs can't be selected.
- **Outdated extension:** when an extension with an older protocol tries to connect and none is connected, the session list footer shows "browser extension outdated", followed by the driver's fix (Chrome: "reload it in chrome://extensions"), until a current extension connects.

Limitations: tab changes made in the last ~2 seconds before quitting the browser may not be saved. Archiving while the extension is disconnected saves the last known tabs and closes the windows when the extension reconnects; if the browser restarts first, the windows it restores stay open as personal windows and reactivating the session opens new copies.

With [AeroSpace](https://github.com/nikitabobko/AeroSpace), the daemon also parks windows: the attached session's browser windows sit on the browser workspace, and every other session's windows are moved to the park workspace. When you switch sessions, the windows swap quietly. Swapping never changes focus, and browser windows no session owns are never moved. Opening a link from the attached session (or from outside any session) focuses its window through AeroSpace, which switches to that window's workspace; links from background sessions open without changing focus. A window created for a background session may flash on the browser workspace for a moment before it is parked. A window that can't be matched to an AeroSpace window unambiguously stays where it is until a later tab change or swap resolves it.

| Flag | Env | Default | |
|---|---|---|---|
| `--wm` | `UTENA_WM` | `aerospace` | `aerospace` or `none`; `none` turns parking off |
| `--browser-workspace` | `UTENA_BROWSER_WORKSPACE` | `2` | workspace for the attached session's windows |
| `--park-workspace` | `UTENA_PARK_WORKSPACE` | `P` | workspace for every other session's windows |

The dotfiles AeroSpace config (`programs/aerospace/contents/aerospace.toml.tmpl`) binds `alt-b` to view workspace `P` and `alt-shift-b` to park the focused window on `P`. AeroSpace creates `P` on demand. A session-owned window you move by hand goes back to its workspace the next time placement runs.

#### Plugin bundles

Bundles turn groups of Claude Code plugins on per session. Define them in `~/.config/utena/daemon.json`:

```json
{
  "bundles": {
    "go": {"plugins": ["go@my-marketplace", "go-patterns@my-marketplace"]},
    "docs": {"plugins": ["pdf@my-marketplace"]}
  }
}
```

The daemon reads this file at startup, so restart it after changing it. A malformed file stops the daemon from starting. `GET /bundles` lists the catalog.

A workspace can pick default bundles in its `.utena/config.json`:

```json
{
  "default_bundles": ["go"]
}
```

A new session gets the default bundles of all its workspaces, unless the create request sets `bundles` itself (`"bundles": []` means none). Adding a workspace to a session adds that workspace's defaults. `PUT /sessions/{id}/bundles` with `{"bundles": [...]}` replaces a session's bundles. Unknown bundle names are kept and logged, not rejected.

utena writes the selected plugins as `true` under `enabledPlugins` in the session root's `.claude/settings.local.json`, and removes catalog plugins that aren't selected. Plugins outside the catalog are never touched. The file is rewritten when the session is set up, a workspace is added, the session is activated, or its bundles change. Run `/reload-plugins` in open Claude panes to pick up a change.

In the TUI, the new-session form has a Bundles field, pre-filled from the selected workspaces' defaults; press enter on it to pick bundles (space toggles). Until you change it, the daemon applies the workspace defaults. In session detail, the Bundles section lists the session's bundles and `p` edits them.

For bundles to stay off elsewhere, set each bundled plugin to `false` in `~/.claude/settings.json`; the dotfiles (shizuku) do this, so the session's local `true` turns them on.

Bundles can also turn MCP servers on. Define the servers once under `mcp_servers` and list them in bundles:

```json
{
  "mcp_servers": {
    "homeassistant": {"type": "http", "url": "https://ha.example/api/mcp",
                      "headers": {"Authorization": "Bearer ${HOMEASSISTANT_TOKEN}"}}
  },
  "bundles": {
    "homelab": {"mcp_servers": ["homeassistant"]}
  }
}
```

utena copies each selected definition as-is into `mcpServers` in the session root's `.mcp.json` and adds its name to `enabledMcpjsonServers` in `.claude/settings.local.json`, so Claude doesn't ask to approve it. Claude only honors those local approvals in a trusted folder, so utena also marks each session root as trusted in `~/.claude.json` (`projects[<root>].hasTrustDialogAccepted`, or `$CLAUDE_CONFIG_DIR/.claude.json` when that is set) when the session is set up or activated. This trusts the whole session folder, including the hooks and settings of the repos checked out in it. Catalog servers that aren't selected are removed from both; other servers are left alone. Keep secrets out of the file with `${VAR}` placeholders, which Claude Code expands from its environment. Remove catalog servers from user scope (`~/.claude.json`) so they only exist in sessions that select them; shizuku does this. Run `/mcp` (or restart Claude) to pick up a change.

#### Claude Code integration

Utena can track Claude Code session status (working, needs attention, completed) and display it in the TUI.

1. Install the `utena-claude` plugin in Claude Code — point it at this repo's `.claude-plugin/marketplace.json`
2. Ensure `eval "$(utena shell-init)"` is in your shell profile (see above)
3. Start the daemon and use Claude Code inside a tuios session managed by utena — the TUI will show Claude's status next to each session

## Theming

The TUI can be customized by creating a theme file at `~/.config/utena/theme.json`. All fields are optional — only include the colors you want to override. Values are hex color strings.

```json
{
  "primary": "#e6537a",
  "primary_variant": "#d16577",
  "secondary": "#d97c6e",
  "tertiary": "#9370b9",
  "surface_variant": "#dfd6cc",
  "surface_active": "#f0e8e0",
  "selection": "#ffd5d5",
  "text": "#4a4a4a",
  "text_emphasis": "#2a2a2a",
  "text_muted": "#8a7873",
  "text_on_primary": "#fef6f0",
  "accent_blue": "#6a9bc3",
  "accent_lavender": "#a87bb7",
  "accent_mint": "#5fafa5",
  "error": "#cc3333",
  "path": "#6a9bc3",
  "status_ready": "#5faf5f",
  "status_pending": "#808080",
  "status_active": "#d7a74f",
  "list_title": "#303030",
  "list_desc": "#6c6c6c",
  "list_selected_title": "#e6537a",
  "list_selected_desc": "#d16577",
  "list_selected_border": "#d16577"
}
```

| Key | Used for |
|-----|----------|
| `primary` | Main accent color (e.g. needs-attention indicator) |
| `primary_variant` | Variant of primary (e.g. badge background) |
| `secondary` | Secondary accent (e.g. working status) |
| `tertiary` | Tertiary accent (e.g. workspace name in sidebar) |
| `surface_variant` | Inactive surface background |
| `surface_active` | Background for attached sessions |
| `selection` | Background for the selected/focused item |
| `text` | Default text color |
| `text_emphasis` | Bold/emphasized text |
| `text_muted` | Dimmed text (timestamps, inactive items) |
| `text_on_primary` | Text rendered on primary-colored backgrounds |
| `accent_blue` | Active window indicator |
| `accent_lavender` | Working session accent bar |
| `accent_mint` | Ready-for-review indicator |
| `error` | Error messages and failed status |
| `path` | File/branch path highlights |
| `status_ready` | Ready/success status |
| `status_pending` | Pending/loading status, debug text |
| `status_active` | In-progress/active status |
| `list_title` | List item titles |
| `list_desc` | List item descriptions |
| `list_selected_title` | Selected list item title |
| `list_selected_desc` | Selected list item description |
| `list_selected_border` | Left border on selected list item |

The theme is loaded once at TUI startup. Changes require restarting the TUI.

## Libraries

### Bubble Tea
- [Examples](https://github.com/charmbracelet/bubbletea/tree/main/examples)

See [CLAUDE.md](CLAUDE.md) for build commands and development workflow.
