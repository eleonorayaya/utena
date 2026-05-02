# Utena

A workspace management system for tmux, consisting of a daemon API server, TUI client, and TPM plugin.

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

#### TPM Plugin

Add to your `.tmux.conf`:

```
set -g @plugin 'path/to/utena/plugins/utena-tmux'
```

This registers tmux hooks for session state sync and binds `prefix + p` to open the utena TUI in a popup.

#### Shell initialization

Add to your `.zshrc` (or `.bashrc`):

```bash
eval "$(utena shell-init)"
```

This sets up environment variables needed for utena integrations (like Claude Code status tracking) when running inside tmux.

#### Claude Code integration

Utena can track Claude Code session status (working, needs attention, completed) and display it in the TUI.

1. Install the `utena-claude` plugin in Claude Code — point it at this repo's `.claude-plugin/marketplace.json`
2. Ensure `eval "$(utena shell-init)"` is in your shell profile (see above)
3. Start the daemon and use Claude Code inside a tmux session managed by utena — the TUI will show Claude's status next to each session

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

<!-- test pr review prompt -->
