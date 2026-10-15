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

Links opened from a utena session land in that session's own Chrome window.

1. Run `task extension:install`, then do the one-time Load unpacked step it prints (`chrome://extensions` → Developer mode → Load unpacked). Install it in one Chrome profile only.
2. `eval "$(utena shell-init)"` sets `BROWSER` to `utena-open` inside tuios, so tools that open links route them through the daemon.
3. `utena open <url>...` opens links in the current session's window; when the browser extension is connected, the TUI's `o` switches to the selected session (if it isn't attached) and opens its PR in that session's window. Otherwise `o` just opens the PR.
4. If the daemon or extension is unavailable, links fall back to `/usr/bin/open`.

After upgrading utena, reload the extension in `chrome://extensions`. The extension now speaks protocol 2, and the daemon reports an older one as outdated.

Session windows outlive the session's time in the foreground:

- **Archiving** a session saves its windows' tabs and closes the windows. **Reactivating** it brings them back with the same tabs and the same active tab (pinned tabs come back unpinned).
- **Closing a window by hand** forgets it; it won't come back.
- **Quitting Chrome** keeps the windows. When Chrome restarts and restores its windows, utena reattaches them to their sessions by matching their tabs. Windows it can't match are restored from their saved tabs the next time the session is attached or a link is opened for it.
- **Deleting** a session closes its windows.

Limitations: tab changes made in the last ~2 seconds before quitting Chrome may not be saved. Archiving while the extension is disconnected saves the last known tabs and closes the windows when the extension reconnects; if Chrome restarts first, the windows Chrome restores stay open as personal windows and reactivating the session opens new copies.

With [AeroSpace](https://github.com/nikitabobko/AeroSpace), the daemon also parks windows: the attached session's Chrome windows sit on the browser workspace, and every other session's windows are moved to the park workspace. When you switch sessions, the windows swap quietly. Swapping never changes focus, and Chrome windows no session owns are never moved. Opening a link from the attached session (or from outside any session) focuses its window through AeroSpace, which switches to that window's workspace; links from background sessions open without changing focus. A window created for a background session may flash on the browser workspace for a moment before it is parked. A window that can't be matched to an AeroSpace window unambiguously stays where it is until a later tab change or swap resolves it.

| Flag | Env | Default | |
|---|---|---|---|
| `--wm` | `UTENA_WM` | `aerospace` | `aerospace` or `none`; `none` turns parking off |
| `--browser-workspace` | `UTENA_BROWSER_WORKSPACE` | `2` | workspace for the attached session's windows |
| `--park-workspace` | `UTENA_PARK_WORKSPACE` | `P` | workspace for every other session's windows |

The dotfiles AeroSpace config (`programs/aerospace/contents/aerospace.toml.tmpl`) binds `alt-b` to view workspace `P` and `alt-shift-b` to park the focused window on `P`. AeroSpace creates `P` on demand. A session-owned window you move by hand goes back to its workspace the next time placement runs.

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
