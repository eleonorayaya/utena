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

## Libraries

### Bubble Tea
- [Examples](https://github.com/charmbracelet/bubbletea/tree/main/examples)

See [CLAUDE.md](CLAUDE.md) for build commands and development workflow.
